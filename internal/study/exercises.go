package study

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"

	"github.com/athos/gitview/internal/flow"
)

// Exercise is a quiz question or a challenge, built from a fixed template
// and structured facts only. The answer is the option at Answer; Evidence
// points to what proves it.
type Exercise struct {
	ID         string   `json:"id"`
	Kind       string   `json:"kind"` // quiz | challenge
	Type       string   `json:"type"` // signature | callgraph | cfg | sql | dataflow | route | architecture | branch | trace
	Prompt     string   `json:"prompt"`
	Code       string   `json:"code,omitempty"`  // context to read (signature, condition, query)
	Given      []string `json:"given,omitempty"` // values of a branch challenge
	Options    []string `json:"options"`
	Answer     int      `json:"answer"`
	SourceFile string   `json:"sourceFile,omitempty"`
	SourceLine int      `json:"sourceLine,omitempty"`
	SourceType string   `json:"sourceType"` // AST | CFG | CALLGRAPH | SQL | DATAFLOW | TRACE | ARCHITECTURE
	Evidence   Evidence `json:"evidence"`
}

// options orders the correct answer and the distractors deterministically
// (by a hash of the exercise and the option), returning the answer index.
// Distractors equal to the answer are dropped; at least two distractors
// are required, else ok is false.
func options(id, answer string, distractors []string, max int) ([]string, int, bool) {
	seen := map[string]bool{answer: true}
	var ds []string
	for _, d := range distractors {
		if d != "" && !seen[d] {
			seen[d] = true
			ds = append(ds, d)
		}
	}
	sort.Slice(ds, func(i, j int) bool { return hash(id+ds[i]) < hash(id+ds[j]) })
	if len(ds) < 2 {
		return nil, 0, false
	}
	if len(ds) > max-1 {
		ds = ds[:max-1]
	}
	all := append(ds, answer)
	sort.Slice(all, func(i, j int) bool { return hash(id+"#"+all[i]) < hash(id+"#"+all[j]) })
	for i, o := range all {
		if o == answer {
			return all, i, true
		}
	}
	return nil, 0, false
}

func hash(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}

const maxTraceChallenges = 6

// traceChallenges asks which function the trace shows next under an entry
// point, among the functions the call graph says it may reach.
func traceChallenges(p Predict) []Exercise {
	if p.Observed == nil {
		return nil
	}
	var out []Exercise
	for _, en := range p.Entries {
		names := map[string]string{en.ID: en.Title}
		for _, c := range en.Candidates {
			names[c.ID] = c.Name
		}
		var rel []ObservedCall
		for _, c := range en.Observed {
			if _, ok := names[c.ID]; ok {
				rel = append(rel, c)
			}
		}
		for i := 0; i+1 < len(rel) && len(out) < maxTraceChallenges; i++ {
			next := rel[i+1]
			// Skip when the order is not known: ties with the previous call
			// or with another candidate.
			if next.Seq == rel[i].Seq || (i+2 < len(rel) && rel[i+2].Seq == next.Seq) {
				continue
			}
			var ds []string
			for _, c := range en.Candidates {
				if c.ID != next.ID && c.ID != rel[i].ID {
					ds = append(ds, c.Name)
				}
			}
			id := "trace:" + en.ID + ":" + itoa(i)
			opts, ans, ok := options(id, next.Name, ds, 4)
			if !ok {
				continue
			}
			out = append(out, Exercise{ID: id, Kind: "challenge", Type: "trace", SourceType: "TRACE",
				Prompt:  "In the execution trace (order of first appearance), which function reached from " + en.Title + " appears right after " + rel[i].Name + "?",
				Options: opts, Answer: ans,
				Evidence: Evidence{Basis: Runtime, Text: next.Name + " first appears at " + fmtNs(next.FirstNs) + " (" + next.How + ") in " + p.Observed.Source, Ref: "inspect:" + next.ID + ":trace"}})
		}
	}
	return out
}

func fmtNs(ns int64) string {
	switch {
	case ns >= 1e9:
		return strconv.FormatFloat(float64(ns)/1e9, 'f', 2, 64) + " s"
	case ns >= 1e6:
		return strconv.FormatFloat(float64(ns)/1e6, 'f', 2, 64) + " ms"
	case ns >= 1e3:
		return strconv.FormatFloat(float64(ns)/1e3, 'f', 1, 64) + " µs"
	}
	return itoa(int(ns)) + " ns"
}

const (
	maxPerTemplate = 4
	maxQuizzes     = 24
	maxChallenges  = 24
)

// sigParts splits a function signature as printed by the call graph
// ("func (r *T) Name(params) results") into its parameter and result
// lists, types only.
func sigParts(sig string) (params []string, results string, ok bool) {
	f, err := parser.ParseFile(token.NewFileSet(), "", "package p\n"+sig+" {}", 0)
	if err != nil || len(f.Decls) != 1 {
		return nil, "", false
	}
	fd, isFunc := f.Decls[0].(*ast.FuncDecl)
	if !isFunc {
		return nil, "", false
	}
	typ := func(e ast.Expr) string {
		var b strings.Builder
		printer.Fprint(&b, token.NewFileSet(), e)
		return b.String()
	}
	for _, fl := range fd.Type.Params.List {
		n := max(len(fl.Names), 1)
		for i := 0; i < n; i++ {
			name := ""
			if i < len(fl.Names) {
				name = fl.Names[i].Name + " "
			}
			params = append(params, name+typ(fl.Type))
		}
	}
	var rs []string
	if fd.Type.Results != nil {
		for _, fl := range fd.Type.Results.List {
			for i := 0; i < max(len(fl.Names), 1); i++ {
				rs = append(rs, typ(fl.Type))
			}
		}
	}
	switch len(rs) {
	case 0:
		results = "nothing (no results)"
	case 1:
		results = rs[0]
	default:
		results = "(" + strings.Join(rs, ", ") + ")"
	}
	return params, results, true
}

// withoutResults returns the signature up to its parameter list.
func withoutResults(sig string) string {
	depth := 0
	seenName := false
	for i, c := range sig {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 && seenName {
				return sig[:i+1]
			}
		default:
			if depth == 0 && c != ' ' && i > 4 {
				seenName = true
			}
		}
	}
	return sig
}

// ends lists the labels of the origins (or destinations) Data Flow finds
// for a variable.
func ends(in Inputs, fn, key string, origin bool) map[string]bool {
	out := map[string]bool{}
	for _, b := range in.Bundles {
		if b.Symbol.ID != fn || b.DataFlow[key] == nil || len(b.DataFlow[key].Roots) == 0 {
			continue
		}
		r := b.DataFlow[key].HideIntermediate()
		for id := range bfs(r, r.Roots[0], origin) {
			if !hasMore(r, id, origin) {
				out[r.Node(id).Label] = true
			}
		}
	}
	return out
}

type exBuilder struct {
	out   []Exercise
	count map[string]int
	limit int
	kind  string
}

func (b *exBuilder) full(typ string) bool {
	n := 0
	for _, e := range b.out {
		if e.Kind == b.kind {
			n++
		}
	}
	return n >= b.limit || b.count[typ] >= maxPerTemplate
}

// add records an exercise when there are enough reliable alternatives.
func (b *exBuilder) add(e Exercise, answer string, distractors []string, minDistractors int) {
	if b.full(e.Type) {
		return
	}
	e.Kind = b.kind
	opts, ans, ok := options(e.ID, answer, distractors, 4)
	if !ok && minDistractors == 1 {
		opts, ans, ok = options1(e.ID, answer, distractors)
	}
	if !ok {
		return
	}
	e.Options, e.Answer = opts, ans
	b.out = append(b.out, e)
	b.count[e.Type]++
}

// options1 is options for binary questions: one distractor is enough.
func options1(id, answer string, distractors []string) ([]string, int, bool) {
	for _, d := range distractors {
		if d != "" && d != answer {
			all := []string{answer, d}
			sort.Slice(all, func(i, j int) bool { return hash(id+"#"+all[i]) < hash(id+"#"+all[j]) })
			if all[0] == answer {
				return all, 0, true
			}
			return all, 1, true
		}
	}
	return nil, 0, false
}

// quizzes builds questions with one correct answer from fixed templates.
func quizzes(m *Model, x *graph, in Inputs, order []string) []Exercise {
	b := &exBuilder{count: map[string]int{}, limit: maxQuizzes, kind: "quiz"}
	fns := orderedFuncs(x, order)

	// Signature: what a function returns (AST of the signature).
	var allResults []string
	for _, n := range x.funcs {
		if _, r, ok := sigParts(n.Signature); ok {
			allResults = append(allResults, r)
		}
	}
	for _, n := range fns {
		_, res, ok := sigParts(n.Signature)
		if !ok {
			continue
		}
		b.add(Exercise{ID: "sig:" + n.ID, Type: "signature", SourceType: "AST", SourceFile: n.File, SourceLine: n.Line,
			Prompt: "What does " + n.Name + " return?", Code: withoutResults(n.Signature) + " …",
			Evidence: Evidence{Basis: Static, Text: "signature: " + n.Signature, File: n.File, Line: n.Line, Ref: "codeflow:" + n.ID}},
			res, allResults, 2)
	}

	// Call graph: which function is called at a call site.
	for _, n := range fns {
		callees := map[string]bool{}
		byLine := map[int][]string{}
		for _, e := range x.callees(n.ID) {
			callees[e.To] = true
			if x.isModuleFunc(e.To) {
				byLine[e.Line] = append(byLine[e.Line], e.To)
			}
		}
		var lines []int
		for l, ids := range byLine {
			if len(ids) == 1 {
				lines = append(lines, l)
			}
		}
		sort.Ints(lines)
		var ds []string
		for _, o := range x.funcs {
			if !callees[o.ID] && o.ID != n.ID {
				ds = append(ds, o.Name)
			}
		}
		for _, l := range lines[:min(len(lines), 1)] {
			to := x.nodes[byLine[l][0]]
			var edge = x.callees(n.ID)[0]
			for _, e := range x.callees(n.ID) {
				if e.To == to.ID && e.Line == l {
					edge = e
				}
			}
			if edge.Confidence != flow.Confirmed {
				continue
			}
			b.add(Exercise{ID: "call:" + n.ID + ":" + itoa(l), Type: "callgraph", SourceType: "CALLGRAPH", SourceFile: edge.File, SourceLine: l,
				Prompt:   "Which function of the module does " + n.Name + " call at " + loc(edge.File, l) + "?",
				Evidence: Evidence{Basis: Static, Text: n.Name + " → " + to.Name + " (call graph, confirmed)", File: edge.File, Line: l, Ref: "codeflow:" + n.ID}},
				to.Name, ds, 2)
		}
	}

	// CFG: the first statement of a decision outcome.
	for _, f := range m.Decisions {
		var firsts []string
		for _, d := range f.Decisions {
			for _, o := range d.Outcomes {
				firsts = append(firsts, o.First)
			}
		}
		for _, d := range f.Decisions {
			if d.Kind != "if" || len(d.Outcomes) != 2 || b.full("cfg") {
				continue
			}
			o := d.Outcomes[0]
			if o.First == "" || o.First == d.Outcomes[1].First {
				continue
			}
			b.add(Exercise{ID: "cfg:" + f.Func + ":" + itoa(d.Block), Type: "cfg", SourceType: "CFG", SourceFile: d.File, SourceLine: d.Line,
				Prompt: "In " + f.FuncName + ", at line " + itoa(d.Line) + ", when the condition is " + o.Label + ", which statement runs first?", Code: "if " + d.Cond,
				Evidence: Evidence{Basis: Static, Text: "CFG: " + d.Cond + " → " + o.Label + " leads to " + o.First, File: d.File, Line: d.Line, Ref: d.Evidence.Ref}},
				o.First, firsts, 2)
			break // one per function
		}
	}

	// SQL: the table a statement reads or writes.
	known := m.SQL.Tables
	for _, q := range m.SQL.Queries {
		s := q.Structure
		if s == nil || s.Partial || len(s.Tables) == 0 {
			continue
		}
		t := s.Tables[0]
		if t.Depth != 0 {
			continue
		}
		in := map[string]bool{}
		for _, x := range s.Tables {
			in[normName(x.Name)] = true
		}
		var ds []string
		for _, k := range known {
			if !in[normName(k)] {
				ds = append(ds, k)
			}
		}
		verb := map[string]string{"from": "is in the FROM clause of", "insert": "does the query insert into, in", "update": "does the query update, in", "delete": "does the query delete from, in"}[t.Role]
		if verb == "" {
			continue
		}
		b.add(Exercise{ID: "sqltable:" + q.ID, Type: "sql", SourceType: "SQL", SourceFile: q.File, SourceLine: q.Line,
			Prompt: "Which table " + verb + " the query of " + q.FuncName + "?", Code: q.SQL,
			Evidence: Evidence{Basis: SQLBasis, Text: "table " + t.Name + " (" + t.Role + ") read from the SQL text", File: q.File, Line: q.Line, Ref: "inspect:" + q.Func + ":sql"}},
			t.Name, ds, 2)
	}

	// Data Flow: where a value comes from.
	var sources []string
	for _, j := range m.DataJourneys {
		if len(j.Steps) > 0 && j.Steps[0].Role == RoleSource {
			sources = append(sources, j.Steps[0].Label)
		}
	}
	for _, j := range m.DataJourneys {
		if !j.Complete || len(j.Steps) == 0 || j.Steps[0].Role != RoleSource {
			continue
		}
		origins := ends(in, j.Func, j.Variable, true)
		if len(origins) != 1 { // a single origin, so there is one right answer
			continue
		}
		var ds []string
		for _, s := range sources {
			if !origins[s] {
				ds = append(ds, s)
			}
		}
		st := j.Steps[0]
		b.add(Exercise{ID: "origin:" + j.Func + ":" + j.Variable, Type: "dataflow", SourceType: "DATAFLOW", SourceFile: st.File, SourceLine: st.Line,
			Prompt:   "Where does the value of " + varName(j.Variable) + " in " + j.FuncName + " come from?",
			Evidence: Evidence{Basis: Static, Text: "Data Flow: " + st.Label + " → … → " + varName(j.Variable), File: st.File, Line: st.Line, Ref: j.Evidence.Ref}},
			st.Label, ds, 2)
	}

	// Routes: the handler of a route.
	for _, n := range x.nodeList() {
		if n.Kind != flow.KindRoute {
			continue
		}
		var handler *flow.Edge
		for _, e := range x.out[n.ID] {
			if h := x.nodes[e.To]; e.Kind == flow.EdgeHandler && h != nil && !hasTag(h, "middleware") {
				handler = e
			}
		}
		if handler == nil {
			continue
		}
		var ds []string
		for _, o := range x.funcs {
			if o.ID != handler.To {
				ds = append(ds, o.Name)
			}
		}
		b.add(Exercise{ID: "route:" + n.ID, Type: "route", SourceType: "CALLGRAPH", SourceFile: n.File, SourceLine: n.Line,
			Prompt:   "Which function handles " + n.Name + "?",
			Evidence: Evidence{Basis: Static, Text: "route registered with " + n.Router + " and handler " + x.nodes[handler.To].Name, File: handler.File, Line: handler.Line, Ref: "codeflow:" + n.ID}},
			x.nodes[handler.To].Name, ds, 2)
	}

	// Architecture: the layer of a package classified from code facts.
	for _, p := range m.Architecture.Packages {
		if p.Basis != Static || p.Layer == LayerUnknown {
			continue
		}
		var ds []string
		for _, l := range Layers {
			if l != p.Layer && l != LayerUnknown {
				ds = append(ds, l)
			}
		}
		ev := Evidence{Basis: Static, Text: "layer " + p.Layer}
		if len(p.Evidence) > 0 {
			ev = p.Evidence[0]
		}
		b.add(Exercise{ID: "layer:" + p.Path, Type: "architecture", SourceType: "ARCHITECTURE", SourceFile: p.Dir,
			Prompt: "Which layer does package " + p.Path + " belong to, by what its code does?", Evidence: ev},
			p.Layer, ds, 2)
	}
	return b.out
}

func varName(key string) string {
	if i := strings.LastIndexByte(key, '@'); i > 0 {
		return key[:i] + " (line " + key[i+1:] + ")"
	}
	return key
}

func orderedFuncs(x *graph, order []string) []*flow.Node {
	var out []*flow.Node
	for _, id := range order {
		if n := x.nodes[id]; n != nil && n.Kind == flow.KindFunc {
			out = append(out, n)
		}
	}
	return out
}

// challenges builds exercises that ask the reader to follow the code.
func challenges(m *Model, x *graph, in Inputs, order []string) []Exercise {
	b := &exBuilder{count: map[string]int{}, limit: maxChallenges, kind: "challenge"}
	b.out = append(b.out, traceChallenges(m.Predict)...)
	for _, e := range b.out {
		b.count[e.Type]++
	}

	// Branch: which outcome runs for given values (evaluated, not guessed).
	for _, f := range m.Decisions {
		for _, d := range f.Decisions {
			if d.Kind != "if" || len(d.Outcomes) != 2 || b.full("branch") {
				continue
			}
			c, ok := condOf(in.Programs, f.Func, d.Cond, d.Line)
			if !ok {
				continue
			}
			describe := func(o Outcome) string {
				s := o.Label + " → "
				switch {
				case o.First != "":
					s += o.First
				case o.Terminal != "":
					s += o.Terminal
				default:
					s += "next block"
				}
				return s
			}
			t, fl := describe(d.Outcomes[0]), describe(d.Outcomes[1])
			if d.Outcomes[0].Label != "true" {
				t, fl = fl, t
			}
			yes, no, given := c.scenarios(f.Func + d.Cond)
			for i, a := range []*assignment{yes, no} {
				if a == nil {
					continue
				}
				ans, other := t, fl
				if i == 1 {
					ans, other = fl, t
				}
				b.add(Exercise{ID: "branch:" + f.Func + ":" + itoa(d.Block) + ":" + itoa(i), Type: "branch", SourceType: "CFG", SourceFile: d.File, SourceLine: d.Line,
					Prompt: "In " + f.FuncName + ", at line " + itoa(d.Line) + ", with these values, which way does the condition go?", Code: "if " + d.Cond, Given: given[a],
					Evidence: Evidence{Basis: Static, Text: "the condition evaluates to " + strings.SplitN(ans, " ", 2)[0] + " with " + strings.Join(given[a], ", ") + "; CFG outcomes at " + loc(d.File, d.Line), File: d.File, Line: d.Line, Ref: d.Evidence.Ref}},
					ans, []string{other}, 1)
			}
		}
	}

	// Data Flow: where a value ends.
	var sinks []string
	for _, j := range m.DataJourneys {
		if n := len(j.Steps); n > 0 && j.Steps[n-1].Role == RoleSink {
			sinks = append(sinks, j.Steps[n-1].Label)
		}
	}
	for _, j := range m.DataJourneys {
		n := len(j.Steps)
		if !j.Complete || n == 0 || j.Steps[n-1].Role != RoleSink {
			continue
		}
		dests := ends(in, j.Func, j.Variable, false)
		if len(dests) != 1 {
			continue
		}
		var ds []string
		for _, s := range sinks {
			if !dests[s] {
				ds = append(ds, s)
			}
		}
		st := j.Steps[n-1]
		b.add(Exercise{ID: "sink:" + j.Func + ":" + j.Variable, Type: "dataflow", SourceType: "DATAFLOW", SourceFile: st.File, SourceLine: st.Line,
			Prompt:   "Follow " + varName(j.Variable) + " from " + j.FuncName + ": where does the value end up?",
			Evidence: Evidence{Basis: Static, Text: "Data Flow: " + varName(j.Variable) + " → … → " + st.Label, File: st.File, Line: st.Line, Ref: j.Evidence.Ref}},
			st.Label, ds, 2)
	}

	// SQL: the Go expression bound to a placeholder.
	var exprs []string
	for _, q := range m.SQL.Queries {
		for _, p := range q.Params {
			exprs = append(exprs, paramExpr(p))
		}
	}
	for _, q := range m.SQL.Queries {
		for i, p := range q.Params {
			if i > 0 {
				break
			}
			// Any other expression is not the one sent as this placeholder.
			var ds []string
			for _, e := range exprs {
				if e != paramExpr(p) {
					ds = append(ds, e)
				}
			}
			ph := strings.SplitN(p, " ", 2)[0]
			b.add(Exercise{ID: "param:" + q.ID + ":" + ph, Type: "sql", SourceType: "SQL", SourceFile: q.File, SourceLine: q.Line,
				Prompt: "In the query of " + q.FuncName + ", which Go expression is sent as " + ph + "?", Code: q.SQL,
				Evidence: Evidence{Basis: Static, Text: "argument " + p + " of the call at " + loc(q.File, q.Line), File: q.File, Line: q.Line, Ref: "inspect:" + q.Func + ":sql"}},
				paramExpr(p), ds, 2)
		}
	}

	// Architecture: the function of a route chain that calls the database.
	noDB := []string{}
	for _, n := range x.funcs {
		direct := false
		for _, e := range x.out[n.ID] {
			if t := x.nodes[e.To]; t != nil && t.Kind == flow.KindDB {
				direct = true
			}
		}
		if !direct {
			noDB = append(noDB, n.Name)
		}
	}
	for _, c := range m.Architecture.Chains {
		for i, s := range c.Steps {
			if s.Layer != LayerDatabase || i == 0 {
				continue
			}
			prev := c.Steps[i-1]
			b.add(Exercise{ID: "chain:" + c.Route, Type: "architecture", SourceType: "ARCHITECTURE", SourceFile: prev.File, SourceLine: prev.Line,
				Prompt:   "On the way from " + c.Title + " to the database, which function calls the database directly?",
				Evidence: Evidence{Basis: Static, Text: prev.Name + " → " + s.Name + " (call graph)", File: s.File, Line: s.Line, Ref: "codeflow:" + prev.ID}},
				prev.Name, noDB, 2)
			break
		}
	}
	return b.out
}

// paramExpr returns the expression of a "$N = expr (type)" parameter.
func paramExpr(p string) string {
	s := p
	if i := strings.Index(s, " = "); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.LastIndex(s, " ("); i > 0 && strings.HasSuffix(s, ")") {
		s = s[:i]
	}
	return s
}
