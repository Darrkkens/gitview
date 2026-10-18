package audit

import (
	"strconv"
	"strings"

	"github.com/athos/gitview/internal/evidence"
	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/inspect/dataflow"
	"golang.org/x/tools/go/ssa"
)

// taint is what Data Flow says about where a value of a sensitive
// operation comes from.
type taint struct {
	res        *dataflow.Result
	untrusted  []taintPath // paths from HTTP input
	unresolved []string    // places where the trace stopped
	validated  []dataflow.Validation
	partial    bool
}

// taintPath is one path from an untrusted source to the value.
type taintPath struct {
	source    *dataflow.Node
	steps     []FlowStep
	conf      string // weakest confidence of the edges
	sanitizer string // a recognised neutralisation on the path, if any
}

// Sanitizers whose effect on the value is known, by sink class. Only
// functions with that semantics count: a transformation is not a
// sanitization by itself.
var (
	numericParsers = map[string]bool{"strconv.Atoi": true, "strconv.ParseInt": true, "strconv.ParseUint": true, "strconv.ParseFloat": true,
		"strconv.ParseBool": true, "uuid.Parse": true, "uuid.MustParse": true}
	pathSanitizers = map[string]bool{"filepath.Base": true, "path.Base": true}
	sqlSanitizers  = map[string]bool{"pq.QuoteIdentifier": true, "pq.QuoteLiteral": true, "(pgx.Identifier).Sanitize": true}
)

// trace follows v (an argument of a sensitive call in fn) back to its
// origins. Results are cached by value.
func (s *scanner) trace(p *flow.Program, fn *ssa.Function, v ssa.Value, what string) *taint {
	if t, ok := s.traces[v]; ok {
		return t
	}
	res := dataflow.TraceValue(p, fn, v, dataflow.Selection{Kind: "value", Name: what}, dataflow.Options{Direction: dataflow.Origin, MaxDepth: s.opt.MaxDepth, MaxNodes: 500})
	t := &taint{res: res, validated: res.Validations, partial: res.Status.Status != evidence.Complete}
	// Edges point from the origin side to the selection.
	out := map[int][]*dataflow.Edge{}
	hasIn := map[int]bool{}
	for _, e := range res.Edges {
		if e.Dir == string(dataflow.Origin) {
			out[e.From] = append(out[e.From], e)
			hasIn[e.To] = true
		}
	}
	roots := map[int]bool{}
	for _, r := range res.Roots {
		roots[r] = true
	}
	for _, n := range res.Nodes {
		if hasIn[n.ID] || roots[n.ID] {
			continue
		}
		switch {
		case n.Kind == dataflow.KindSource && hasTag(n, "http-input"):
			if path, ok := shortestPath(res, out, n.ID, roots); ok {
				t.untrusted = append(t.untrusted, path)
			}
		case n.Kind == dataflow.KindUnresolved:
			t.unresolved = append(t.unresolved, n.Label+locText(n))
		}
	}
	s.traces[v] = t
	return t
}

func hasTag(n *dataflow.Node, tag string) bool {
	for _, t := range n.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

func locText(n *dataflow.Node) string {
	if n.File == "" {
		return ""
	}
	return " (" + n.File + ":" + strconv.Itoa(n.Line) + ")"
}

// shortestPath finds the path with fewest edges from src to a root and
// describes each step.
func shortestPath(res *dataflow.Result, out map[int][]*dataflow.Edge, src int, roots map[int]bool) (taintPath, bool) {
	prev := map[int]*dataflow.Edge{}
	seen := map[int]bool{src: true}
	queue := []int{src}
	end := -1
	for len(queue) > 0 && end < 0 {
		x := queue[0]
		queue = queue[1:]
		if roots[x] {
			end = x
			break
		}
		for _, e := range out[x] {
			if !seen[e.To] {
				seen[e.To] = true
				prev[e.To] = e
				queue = append(queue, e.To)
			}
		}
	}
	if end < 0 {
		return taintPath{}, false
	}
	var edges []*dataflow.Edge
	for x := end; x != src; x = prev[x].From {
		edges = append([]*dataflow.Edge{prev[x]}, edges...)
	}
	srcNode := res.Node(src)
	tp := taintPath{source: srcNode, conf: evidence.Exact}
	tp.steps = append(tp.steps, FlowStep{Role: "source", Label: srcNode.Label, Func: srcNode.FuncName, File: srcNode.File, Line: srcNode.Line, Code: Redact(srcNode.Code)})
	for _, e := range edges {
		n := res.Node(e.To)
		role := "propagation"
		switch e.Kind {
		case dataflow.EdgeConvert, dataflow.EdgeDerived, dataflow.EdgeConcat:
			role = "transformation"
		}
		via := strings.TrimPrefix(e.Label, "via ")
		if strings.HasPrefix(e.Label, "via ") && (numericParsers[via] || pathSanitizers[via] || sqlSanitizers[via]) {
			role = "sanitizer"
			if tp.sanitizer == "" {
				tp.sanitizer = via
			}
		} else if numericType(n.Type) && tp.sanitizer == "" {
			role = "sanitizer"
			tp.sanitizer = "conversion to " + n.Type
		}
		tp.conf = weakerConf(tp.conf, e.Conf)
		tp.steps = append(tp.steps, FlowStep{Role: role, Label: n.Label, Func: n.FuncName, File: n.File, Line: n.Line, Code: Redact(n.Code), Conf: e.Conf, Via: e.Label})
	}
	return tp, true
}

// numericType: a number, a boolean or a UUID cannot carry SQL, a path or
// a command, whatever the input was.
func numericType(t string) bool {
	switch t {
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "float32", "float64", "bool", "uuid.UUID":
		return true
	}
	return false
}

var confOrder = map[string]int{evidence.Exact: 4, evidence.Derived: 3, evidence.Likely: 2, evidence.Possible: 1, evidence.Unresolved: 0}

func weakerConf(a, b string) string {
	if b == "" {
		return a
	}
	if confOrder[b] < confOrder[a] {
		return b
	}
	return a
}

// exploitable returns the untrusted paths that no recognised sanitizer of
// the sink's class (sql, path, command, url) neutralises.
func (t *taint) exploitable(class string) []taintPath {
	var out []taintPath
	for _, p := range t.untrusted {
		if !p.neutralizedFor(class) {
			out = append(out, p)
		}
	}
	return out
}

func (p taintPath) neutralizedFor(class string) bool {
	s := p.sanitizer
	switch {
	case s == "":
		return false
	case numericParsers[s] || strings.HasPrefix(s, "conversion to "):
		return true // a number, boolean or UUID carries no syntax
	case pathSanitizers[s]:
		return class == "path"
	case sqlSanitizers[s]:
		return class == "sql"
	}
	return false
}

// findingConfidence maps the weakest edge of a data path to the finding's
// confidence: exact and derived steps are demonstrated by SSA.
func findingConfidence(paths []taintPath) string {
	best := ConfPossible
	for _, p := range paths {
		switch p.conf {
		case evidence.Exact, evidence.Derived:
			return ConfConfirmed
		case evidence.Likely:
			best = ConfLikely
		}
	}
	return best
}

// addFlow records the data path of the first exploitable path, and the
// rest of what the trace found, as evidence of f.
func (s *scanner) addFlow(f *Finding, t *taint, paths []taintPath) {
	if len(paths) > 0 {
		p := paths[0]
		f.Flow = append(f.Flow, p.steps...)
		src := p.steps[0]
		f.ev(Evidence{Kind: EvSource, Text: "untrusted input: " + src.Label, File: src.File, Line: src.Line, Code: src.Code, Confidence: evidence.Exact, Source: "dataflow"})
		for _, st := range p.steps[1:] {
			kind := EvPropagation
			if st.Role == "transformation" {
				kind = EvTransform
			}
			f.ev(Evidence{Kind: kind, Text: strings.TrimSpace(st.Via + ": " + st.Label), File: st.File, Line: st.Line, Code: st.Code, Confidence: st.Conf, Source: "dataflow"})
		}
		if len(paths) > 1 {
			var more []string
			for _, q := range paths[1:] {
				more = append(more, q.steps[0].Label)
			}
			f.ev(Evidence{Kind: EvSource, Text: "other untrusted inputs reach the same value: " + strings.Join(dedupe(more), "; "), Source: "dataflow", Confidence: evidence.Exact})
		}
	}
	for _, v := range t.validated {
		f.ev(Evidence{Kind: EvValidation, Text: v.Kind + ": " + v.Text + " (its sufficiency is not established)", File: v.File, Line: v.Line, Source: "dataflow"})
		f.Checks = append(f.Checks, Check{Kind: "validation", Label: v.Text, File: v.File, Line: v.Line, Basis: "static"})
	}
	for _, u := range t.unresolved {
		f.Unresolved = append(f.Unresolved, u)
	}
	if t.partial {
		for _, r := range t.res.Status.Reasons {
			f.limit("data flow: " + r)
		}
	}
}

func dedupe(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

// guards lists the conditions that decide whether a sensitive call runs
// and look at its value: an if that dominates the call, whose condition
// calls a predicate with the value (sites.Allowed(origin),
// strings.HasPrefix(p, base)) or compares it with something other than
// an empty or nil value. Within the function of the call only.
func guards(p *flow.Program, at ssa.Instruction, v ssa.Value) []Check {
	if at == nil || at.Block() == nil {
		return nil
	}
	slice := map[ssa.Value]bool{}
	var back func(x ssa.Value, d int)
	back = func(x ssa.Value, d int) {
		if x == nil || d > 6 || slice[x] {
			return
		}
		slice[x] = true
		switch y := x.(type) {
		case *ssa.BinOp:
			back(y.X, d+1)
			back(y.Y, d+1)
		case *ssa.UnOp:
			back(y.X, d+1)
		case *ssa.Convert:
			back(y.X, d+1)
		case *ssa.ChangeType:
			back(y.X, d+1)
		case *ssa.MakeInterface:
			back(y.X, d+1)
		case *ssa.Extract:
			back(y.Tuple, d+1)
		case *ssa.Phi:
			for _, e := range y.Edges {
				back(e, d+1)
			}
		case *ssa.Slice:
			back(y.X, d+1)
		case *ssa.Call:
			// through functions (filepath.Join, strings.TrimSpace…), not
			// through the receiver of a method
			if fn := y.Call.StaticCallee(); fn != nil && fn.Signature.Recv() == nil {
				for _, a := range y.Call.Args {
					back(a, d+1)
				}
			}
		}
	}
	back(v, 0)
	trivial := func(x ssa.Value) bool {
		k, ok := x.(*ssa.Const)
		if !ok {
			return false
		}
		return k.IsNil() || k.Value != nil && (k.Value.ExactString() == `""` || k.Value.ExactString() == "0")
	}
	var meaningful func(x ssa.Value, d int) bool
	meaningful = func(x ssa.Value, d int) bool {
		if x == nil || d > 5 {
			return false
		}
		switch y := x.(type) {
		case *ssa.UnOp:
			return meaningful(y.X, d+1)
		case *ssa.BinOp:
			switch y.Op.String() {
			case "&&", "||":
				return meaningful(y.X, d+1) || meaningful(y.Y, d+1)
			}
			return slice[y.X] && !trivial(y.Y) || slice[y.Y] && !trivial(y.X)
		case *ssa.Call:
			for _, a := range y.Call.Args {
				if slice[a] {
					return true
				}
				if mi, ok := a.(*ssa.MakeInterface); ok && slice[mi.X] {
					return true
				}
			}
		case *ssa.Extract:
			return meaningful(y.Tuple, d+1)
		case *ssa.Lookup:
			return slice[y.Index] // membership in a map: an allow-list or a deny-list
		}
		return false
	}
	var out []Check
	for b := at.Block().Idom(); b != nil; b = b.Idom() {
		iff, ok := b.Instrs[len(b.Instrs)-1].(*ssa.If)
		if !ok || !meaningful(iff.Cond, 0) {
			continue
		}
		pos := iff.Cond.Pos()
		if !pos.IsValid() {
			if ins, ok := iff.Cond.(ssa.Instruction); ok {
				pos = ins.Pos()
			}
		}
		file, line, _ := p.Pos(pos)
		out = append(out, Check{Kind: "validation", Label: "condition on the value: " + strings.TrimSpace(p.Source(file, line, line)), File: file, Line: line, Basis: "static"})
	}
	return out
}

// guarded downgrades a finding whose sink is behind a condition on its
// value: the condition is evidence of a check whose sufficiency the
// analysis cannot establish.
func guarded(f *Finding, checks []Check) {
	if len(checks) == 0 {
		return
	}
	for _, c := range checks {
		f.ev(Evidence{Kind: EvValidation, Text: c.Label + " (its sufficiency is not established)", File: c.File, Line: c.Line, Source: "go/ssa"})
		f.Checks = append(f.Checks, c)
	}
	f.Verdict, f.Confidence = Review, ConfPossible
	f.Message += " The operation runs only after a condition on the value (" + checks[0].Label[len("condition on the value: "):] + "); review whether it is sufficient."
}
