package study

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/athos/gitview/internal/flow"
)

// Step is one entry of the recommended reading order.
type Step struct {
	N       int        `json:"n"`
	Kind    string     `json:"kind"` // route | function | sql | test | file
	Title   string     `json:"title"`
	Detail  string     `json:"detail,omitempty"`
	ID      string     `json:"id,omitempty"` // Code Flow node / symbol
	File    string     `json:"file,omitempty"`
	Line    int        `json:"line,omitempty"`
	Layer   string     `json:"layer"`
	Changed bool       `json:"changed,omitempty"`
	Why     []Evidence `json:"why"`
}

// Bounds of the reading order.
const (
	maxReadFuncs = 25
	maxReadTests = 8
	maxReadFiles = 12
)

// Plan returns the functions of the reading order, so that the Deep
// Inspector can include them in the report (CFG, Data Flow, SQL) before
// the model is built.
func Plan(in Inputs) []string {
	x := newGraph(in.Graph)
	a := classify(in, x)
	var ids []string
	for _, s := range readingOrder(in, x, a, scanTests(in, x)) {
		if s.Kind == "function" {
			ids = append(ids, s.ID)
		}
	}
	return ids
}

// readingOrder answers "where should I start reading this change?":
//
//  1. entry points that lead to the changed code (routes, then main and
//     other roots), each followed depth first through the calls that lead
//     to a changed function;
//  2. changed functions not reached that way;
//  3. data access and external calls of the changed functions;
//  4. changed SQL files;
//  5. tests that reach the changed functions;
//  6. the other changed files.
//
// Ties are broken by file, line and ID, so the order is reproducible.
func readingOrder(in Inputs, x *graph, a *arch, tests []TestFunc) []Step {
	var changed []*flow.Node
	for _, n := range x.funcs {
		if n.Changed {
			changed = append(changed, n)
		}
	}
	// Functions from which a changed function can be reached.
	leads := map[string]bool{}
	queue := []string{}
	for _, n := range changed {
		leads[n.ID] = true
		queue = append(queue, n.ID)
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, e := range x.callers(id) {
			if !leads[e.From] && x.isModuleFunc(e.From) {
				leads[e.From] = true
				queue = append(queue, e.From)
			}
		}
	}
	var steps []Step
	listed := map[string]bool{}
	funcs := 0
	add := func(s Step) {
		if s.ID != "" {
			if listed[s.ID] {
				return
			}
			listed[s.ID] = true
		}
		steps = append(steps, s)
	}
	funcStep := func(n *flow.Node, why ...Evidence) bool {
		if listed[n.ID] || funcs >= maxReadFuncs {
			return false
		}
		funcs++
		l, b := a.layerOf(n.ID)
		why = append([]Evidence{{Basis: b, Text: "layer: " + l}}, why...)
		if n.Changed {
			why = append(why, Evidence{Basis: Git, Text: "changed in the working tree", File: n.File, Line: n.Line, Ref: "diff:" + n.File + ":" + itoa(n.Line)})
		}
		add(Step{Kind: "function", Title: n.Name, Detail: n.Signature, ID: n.ID, File: n.File, Line: n.Line, Layer: l, Changed: n.Changed, Why: why})
		return true
	}
	// visit follows calls depth first, only towards changed code.
	var visit func(id string, depth int)
	visit = func(id string, depth int) {
		if depth > 12 {
			return
		}
		for _, e := range x.callees(id) {
			t := x.nodes[e.To]
			if t == nil || !x.isModuleFunc(t.ID) || !leads[t.ID] || listed[t.ID] {
				continue
			}
			why := Evidence{Basis: Static, Text: "called by " + x.nodes[id].Name, File: e.File, Line: e.Line, Ref: "codeflow:" + id}
			if t.Changed {
				why.Text += " (leads to the change)"
			} else {
				why.Text += " (on the way to the changed code)"
			}
			if funcStep(t, why) {
				visit(t.ID, depth+1)
			}
		}
	}

	// 1. Entry points.
	type entry struct {
		prio  int
		title string
		node  *flow.Node
	}
	var entries []entry
	for _, n := range x.nodes {
		if n.Kind != flow.KindRoute {
			continue
		}
		reaches, changedHandler := false, false
		for _, e := range x.out[n.ID] {
			if e.Kind == flow.EdgeHandler && leads[e.To] {
				reaches = true
				changedHandler = changedHandler || x.nodes[e.To] != nil && x.nodes[e.To].Changed
			}
		}
		routeChanged := in.Changed[n.File][n.Line]
		if !reaches && !routeChanged {
			continue
		}
		prio := 1
		if routeChanged || changedHandler {
			prio = 0
		}
		entries = append(entries, entry{prio, strings.TrimSpace(n.Method + " " + n.Path), n})
	}
	for _, n := range x.funcs {
		if !leads[n.ID] {
			continue
		}
		if r := a.own[n.ID]; r != nil && r.Layer == LayerEntrypoint {
			entries = append(entries, entry{2, n.Name, n})
			continue
		}
		hasCaller := false
		for _, e := range x.callers(n.ID) {
			if x.isModuleFunc(e.From) {
				hasCaller = true
				break
			}
		}
		if !hasCaller && n.Changed && !hasTag(n, "http handler") {
			entries = append(entries, entry{1, n.Name, n})
		}
	}
	// func main is an entry point of its own only when no route leads to
	// the change (otherwise it is the wiring of those routes).
	hasRoute := false
	for _, en := range entries {
		hasRoute = hasRoute || en.node.Kind == flow.KindRoute
	}
	if hasRoute {
		kept := entries[:0]
		for _, en := range entries {
			if en.prio != 2 {
				kept = append(kept, en)
			}
		}
		entries = kept
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].prio != entries[j].prio {
			return entries[i].prio < entries[j].prio
		}
		if entries[i].title != entries[j].title {
			return entries[i].title < entries[j].title
		}
		return entries[i].node.ID < entries[j].node.ID
	})
	for _, en := range entries {
		n := en.node
		if n.Kind == flow.KindRoute {
			why := []Evidence{{Basis: Static, Text: "HTTP route registered with " + n.Router, File: n.File, Line: n.Line, Ref: "codeflow:" + n.ID}}
			if in.Changed[n.File][n.Line] {
				why = append(why, Evidence{Basis: Git, Text: "registration changed in the working tree", File: n.File, Line: n.Line, Ref: "diff:" + n.File + ":" + itoa(n.Line)})
			}
			why = append(why, Evidence{Basis: Static, Text: "entry point: its handler leads to changed code"})
			add(Step{Kind: "route", Title: en.title, Detail: path.Base(n.File), ID: n.ID, File: n.File, Line: n.Line, Layer: LayerRoute, Why: why})
			for _, e := range x.out[n.ID] {
				h := x.nodes[e.To]
				if e.Kind != flow.EdgeHandler || h == nil {
					continue
				}
				if funcStep(h, Evidence{Basis: Static, Text: "handler of " + en.title, File: e.File, Line: e.Line, Ref: "codeflow:" + n.ID}) {
					visit(h.ID, 1)
				}
			}
			continue
		}
		why := Evidence{Basis: Static, Text: "entry point: no function of the module calls it"}
		if r := a.own[n.ID]; r != nil && r.Layer == LayerEntrypoint {
			why = Evidence{Basis: Static, Text: "entry point: func main", File: n.File, Line: n.Line}
		}
		if funcStep(n, why) {
			visit(n.ID, 1)
		}
	}
	// 2. Changed functions not reached from an entry point.
	for _, n := range changed {
		if funcStep(n, Evidence{Basis: Git, Text: "changed function not reached from an entry point of the change"}) {
			visit(n.ID, 1)
		}
	}
	// 3. Data access and external calls of the changed functions.
	for _, n := range changed {
		for _, e := range x.callees(n.ID) {
			t := x.nodes[e.To]
			if t == nil || !x.isModuleFunc(t.ID) {
				continue
			}
			if l, _ := a.layerOf(t.ID); l == LayerRepository || l == LayerExternal {
				funcStep(t, Evidence{Basis: Static, Text: "persistence or external call used by " + n.Name, File: e.File, Line: e.Line, Ref: "codeflow:" + n.ID})
			}
		}
	}
	// 4. Changed SQL files; 6. the other changed files (after the tests).
	var sqlFiles, otherFiles []FileChange
	listedFile := map[string]bool{}
	for _, s := range steps {
		listedFile[s.File] = true
	}
	for _, f := range in.Files {
		switch {
		case strings.EqualFold(path.Ext(f.Path), ".sql"):
			sqlFiles = append(sqlFiles, f)
		case strings.HasSuffix(f.Path, "_test.go"):
		case !listedFile[f.Path]:
			otherFiles = append(otherFiles, f)
		}
	}
	sort.Slice(sqlFiles, func(i, j int) bool { return sqlFiles[i].Path < sqlFiles[j].Path })
	for i, f := range sqlFiles {
		if i >= maxReadFiles {
			break
		}
		add(Step{Kind: "sql", Title: path.Base(f.Path), Detail: f.Path, File: f.Path, Layer: LayerDatabase, Changed: true,
			Why: []Evidence{{Basis: Git, Text: fmt.Sprintf("changed SQL file (+%d −%d)", f.Added, f.Deleted), File: f.Path, Ref: "diff:" + f.Path + ":0"}}})
	}
	// 5. Tests reaching the changed functions.
	reach := reachByTests(tests, x, 3)
	type cand struct {
		test int
		hops int
		why  Evidence
	}
	best := map[int]cand{}
	for _, n := range changed {
		for _, r := range reach[n.ID] {
			t := tests[r.Test]
			why := Evidence{Basis: r.Basis, Text: fmt.Sprintf("calls %s", r.Via.Call), File: t.File, Line: r.Via.Line, Ref: "codeflow:" + r.Via.ID}
			if r.Hops > 0 {
				why.Text += fmt.Sprintf(", which reaches the changed %s in %d call(s)", n.Name, r.Hops)
			} else {
				why.Text += " (changed)"
			}
			if c, ok := best[r.Test]; !ok || r.Hops < c.hops {
				best[r.Test] = cand{r.Test, r.Hops, why}
			}
		}
	}
	var cands []cand
	for _, c := range best {
		cands = append(cands, c)
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].hops != cands[j].hops {
			return cands[i].hops < cands[j].hops
		}
		ti, tj := tests[cands[i].test], tests[cands[j].test]
		if ti.File != tj.File {
			return ti.File < tj.File
		}
		return ti.Line < tj.Line
	})
	testFiles := map[string]bool{}
	for i, c := range cands {
		if i >= maxReadTests {
			break
		}
		t := tests[c.test]
		testFiles[t.File] = true
		why := []Evidence{c.why}
		if t.Status != "" {
			why = append(why, Evidence{Basis: Runtime, Text: "last test run: " + t.Status})
		}
		add(Step{Kind: "test", Title: t.Name, Detail: t.File, ID: "test:" + t.Pkg + "." + t.Name, File: t.File, Line: t.Line, Layer: LayerTests, Why: why})
	}
	for _, f := range in.Files {
		if strings.HasSuffix(f.Path, "_test.go") && !testFiles[f.Path] {
			otherFiles = append(otherFiles, f)
		}
	}
	sort.Slice(otherFiles, func(i, j int) bool { return otherFiles[i].Path < otherFiles[j].Path })
	for i, f := range otherFiles {
		if i >= maxReadFiles {
			break
		}
		l, _, ev := a.fileLayerOf(f, nil)
		why := append(ev, Evidence{Basis: Git, Text: fmt.Sprintf("changed file (+%d −%d)", f.Added, f.Deleted), File: f.Path, Ref: "diff:" + f.Path + ":0"})
		add(Step{Kind: "file", Title: path.Base(f.Path), Detail: f.Path, File: f.Path, Layer: l, Changed: true, Why: why})
	}
	for i := range steps {
		steps[i].N = i + 1
	}
	return steps
}
