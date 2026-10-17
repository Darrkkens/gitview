package study

import (
	"github.com/athos/gitview/internal/inspect"
	"github.com/athos/gitview/internal/inspect/branch"
)

// Status of a branch in the coverage profile.
const (
	Observed    = "OBSERVED"     // the coverage profile shows the outcome ran
	NotObserved = "NOT OBSERVED" // the profile covers the code and shows it did not run
	NoData      = "NO DATA"      // no coverage profile, or not usable for this code
)

// MatrixTest is a test that reaches a function. Which branch a test takes
// cannot be known statically, and the coverage profile is aggregated for
// the whole test run: tests and branch status are reported separately.
type MatrixTest struct {
	Name   string `json:"name"`
	File   string `json:"file"`
	Line   int    `json:"line"`
	Basis  string `json:"basis"` // STATIC | HEURISTIC (how the test refers to the code)
	Hops   int    `json:"hops"`  // calls from the test's direct reference
	Via    string `json:"via"`
	Status string `json:"status,omitempty"` // last test run (RUNTIME)
}

// MatrixRow is one outcome of a decision.
type MatrixRow struct {
	Scenario string `json:"scenario"` // condition and outcome
	Cond     string `json:"cond"`
	Outcome  string `json:"outcome"`
	Block    int    `json:"block"` // decision block
	To       int    `json:"to"`    // block the outcome leads to
	File     string `json:"file"`
	Line     int    `json:"line"`
	Status   string `json:"status"`          // OBSERVED | NOT OBSERVED | NO DATA
	Count    int64  `json:"count,omitempty"` // executions, in count/atomic profiles
}

// MatrixFunc is the branch/test matrix of a function.
type MatrixFunc struct {
	Func        string       `json:"func"`
	FuncName    string       `json:"funcName"`
	File        string       `json:"file"`
	Line        int          `json:"line"`
	Coverage    string       `json:"coverage,omitempty"` // profile name
	Stale       bool         `json:"stale,omitempty"`
	Tests       []MatrixTest `json:"tests"`
	Rows        []MatrixRow  `json:"rows"`
	Cyclomatic  int          `json:"cyclomatic"`
	Observed    int          `json:"observed"`
	NotObserved int          `json:"notObserved"`
	NoData      int          `json:"noData"`
	// Unobserved lists, for the outcomes the profile shows did not run, a
	// path of decisions from the entry to them.
	Unobserved []UnobservedBranch `json:"unobserved,omitempty"`
	Limited    string             `json:"limited,omitempty"`
}

// UnobservedBranch is an outcome no test executed, and how to reach it.
type UnobservedBranch struct {
	Scenario string   `json:"scenario"`
	Line     int      `json:"line"`
	Path     []string `json:"path"` // ENTRY, decisions taken, then the outcome
}

// Bound on the CFGs walked for paths.
const maxPathBlocks = 400

func coverageStatus(b *branch.Block) (string, int64) {
	switch b.Coverage {
	case "covered", "partial":
		return Observed, b.CoverCount
	case "uncovered":
		return NotObserved, 0
	}
	return NoData, 0
}

// testMatrix crosses the CFG outcomes of each function in the report with
// the tests that reach it (syntax + call graph) and the coverage profile.
func testMatrix(bundles []*inspect.Bundle, order []string, tests []TestFunc, x *graph) []MatrixFunc {
	reach := reachByTests(tests, x, 4)
	var out []MatrixFunc
	for _, b := range orderBundles(bundles, order) {
		g := b.Branches
		if g == nil {
			continue
		}
		mf := MatrixFunc{Func: b.Symbol.ID, FuncName: b.Symbol.Qualified, File: b.Symbol.File, Line: b.Symbol.Line, Cyclomatic: g.Metrics.Cyclomatic}
		if g.Coverage != nil {
			mf.Coverage, mf.Stale = g.Coverage.Profile, g.Coverage.Stale
		}
		for _, r := range reach[b.Symbol.ID] {
			t := tests[r.Test]
			mf.Tests = append(mf.Tests, MatrixTest{Name: t.Name, File: t.File, Line: t.Line, Basis: r.Basis, Hops: r.Hops, Via: r.Via.Call, Status: t.Status})
		}
		blocks := map[int]*branch.Block{}
		for _, bl := range g.Blocks {
			blocks[bl.ID] = bl
		}
		for i, e := range g.Edges {
			from, to := blocks[e.From], blocks[e.To]
			if e.Kind != branch.EdgeBranch || from == nil || to == nil || !from.Reachable {
				continue
			}
			line := from.CondLine
			if line == 0 {
				line = from.Line
			}
			st, n := coverageStatus(to)
			if mf.Coverage == "" || mf.Stale {
				st, n = NoData, 0
			}
			row := MatrixRow{Scenario: from.Cond + " → " + e.Label, Cond: from.Cond, Outcome: e.Label, Block: from.ID, To: to.ID, File: from.File, Line: line, Status: st, Count: n}
			switch st {
			case Observed:
				mf.Observed++
			case NotObserved:
				mf.NotObserved++
				if len(g.Blocks) <= maxPathBlocks {
					mf.Unobserved = append(mf.Unobserved, UnobservedBranch{Scenario: row.Scenario, Line: line, Path: pathTo(g, blocks, i)})
				}
			default:
				mf.NoData++
			}
			mf.Rows = append(mf.Rows, row)
		}
		if mf.NotObserved > 0 && len(g.Blocks) > maxPathBlocks {
			mf.Limited = "the function has more than 400 blocks: paths to unobserved branches are not listed"
		}
		if len(mf.Rows) > 0 || len(mf.Tests) > 0 {
			out = append(out, mf)
		}
	}
	return out
}

// pathTo finds the shortest path of CFG edges from the entry to the
// decision of edge target, and describes it as the decisions taken.
func pathTo(g *branch.Graph, blocks map[int]*branch.Block, target int) []string {
	e := g.Edges[target]
	prev := map[int]int{g.EntryID: -1} // block → edge index that reached it
	queue := []int{g.EntryID}
	out := map[int][]int{}
	for i, x := range g.Edges {
		if x.Kind != branch.EdgeLoop {
			out[x.From] = append(out[x.From], i)
		}
	}
	for len(queue) > 0 && queue[0] != e.From {
		id := queue[0]
		queue = queue[1:]
		for _, ei := range out[id] {
			to := g.Edges[ei].To
			if _, seen := prev[to]; !seen {
				prev[to] = ei
				queue = append(queue, to)
			}
		}
	}
	var steps []string
	for cur := e.From; ; {
		ei, ok := prev[cur]
		if !ok || ei < 0 {
			break
		}
		x := g.Edges[ei]
		if x.Kind == branch.EdgeBranch {
			if b := blocks[x.From]; b != nil {
				steps = append(steps, b.Cond+" → "+x.Label)
			}
		}
		cur = x.From
	}
	// Reverse: entry first.
	for i, j := 0, len(steps)-1; i < j; i, j = i+1, j-1 {
		steps[i], steps[j] = steps[j], steps[i]
	}
	path := append([]string{"ENTRY"}, steps...)
	from := blocks[e.From]
	path = append(path, from.Cond+" → "+e.Label)
	if to := blocks[e.To]; to != nil {
		o := Outcome{}
		describe(&o, to, nil, blocks)
		switch {
		case o.First != "":
			path = append(path, o.First)
		}
		if o.Terminal != "" {
			path = append(path, upper(o.Terminal))
		}
	}
	return path
}

func upper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}
