package study

import (
	"sort"
	"strings"

	"github.com/athos/gitview/internal/evidence"
	"github.com/athos/gitview/internal/inspect"
	"github.com/athos/gitview/internal/inspect/dataflow"
)

// Roles of a data journey step.
const (
	RoleSource    = "SOURCE"
	RoleTransform = "TRANSFORMATION"
	RoleDecision  = "DECISION"
	RoleSink      = "SINK"
)

// JourneyStep is one value on a data journey.
type JourneyStep struct {
	Role     string `json:"role"`
	Kind     string `json:"kind"` // Data Flow node kind
	Label    string `json:"label"`
	Var      string `json:"var,omitempty"`
	Type     string `json:"type,omitempty"`
	Func     string `json:"func,omitempty"` // symbol ID of the enclosing function
	FuncName string `json:"funcName,omitempty"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Code     string `json:"code,omitempty"`
	// How the value got here from the previous step (Data Flow edge).
	Via     string `json:"via,omitempty"` // assign | arg | return | store | load | sink | ...
	ViaText string `json:"viaText,omitempty"`
	Conf    string `json:"conf,omitempty"`
}

// DataJourney is a path a value takes, from where it enters to where it
// ends, through one variable of a function: a didactic view of a Data Flow
// result (intermediate SSA values hidden).
type DataJourney struct {
	Func     string        `json:"func"`
	FuncName string        `json:"funcName"`
	Variable string        `json:"variable"` // Data Flow key (name@line)
	Steps    []JourneyStep `json:"steps"`
	Complete bool          `json:"complete"` // from a source to a sink
	Partial  []string      `json:"partial,omitempty"`
	Evidence Evidence      `json:"evidence"`
}

const (
	maxJourneysPerFunc = 3
	maxJourneys        = 40
)

// dataJourneys turns the Data Flow results of the report into journeys,
// in the order of the functions given.
func dataJourneys(bundles []*inspect.Bundle, order []string) []DataJourney {
	sorted := orderBundles(bundles, order)
	var out []DataJourney
	seen := map[string]bool{} // the same path found through several variables is shown once
	for _, b := range sorted {
		if len(out) >= maxJourneys {
			break
		}
		type scored struct {
			j     DataJourney
			score int
		}
		var cands []scored
		keys := make([]string, 0, len(b.DataFlow))
		for k := range b.DataFlow {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			j, score, ok := journeyOf(b, k, b.DataFlow[k])
			if ok {
				cands = append(cands, scored{j, score})
			}
		}
		sort.SliceStable(cands, func(i, j int) bool { return cands[i].score > cands[j].score })
		n := 0
		for _, c := range cands {
			sig := journeySig(c.j)
			if seen[sig] || n >= maxJourneysPerFunc {
				continue
			}
			seen[sig] = true
			out = append(out, c.j)
			n++
		}
	}
	return out
}

func journeySig(j DataJourney) string {
	var parts []string
	for _, s := range j.Steps {
		parts = append(parts, s.Label+"@"+itoa(s.Line))
	}
	return strings.Join(parts, "|")
}

var sourceScore = map[string]int{dataflow.KindSource: 4, dataflow.KindParam: 2, dataflow.KindGlobal: 2, dataflow.KindCall: 1, dataflow.KindConst: 1}
var sinkScore = map[string]int{dataflow.KindSQL: 4, dataflow.KindExternal: 3, dataflow.KindReturn: 2, dataflow.KindCheck: 1}

// journeyOf finds, in one Data Flow result, the most informative path
// from an origin leaf through the selected value to a destination leaf.
func journeyOf(b *inspect.Bundle, key string, full *dataflow.Result) (DataJourney, int, bool) {
	if full == nil || len(full.Roots) == 0 {
		return DataJourney{}, 0, false
	}
	r := full.HideIntermediate()
	root := r.Roots[0]
	// Paths towards the origins (backwards) and the destinations.
	back := bfs(r, root, true)
	fwd := bfs(r, root, false)
	leaves := func(paths map[int][]*dataflow.Edge, origin bool) []int {
		var out []int
		for id := range paths {
			if !hasMore(r, id, origin) {
				out = append(out, id)
			}
		}
		sort.Ints(out)
		return out
	}
	bestScore, bestO, bestD := -1, root, root
	for _, o := range leaves(back, true) {
		for _, d := range leaves(fwd, false) {
			on, dn := r.Node(o), r.Node(d)
			score := sourceScore[on.Kind]*3 + sinkScore[dn.Kind]*3 + min(len(back[o])+len(fwd[d]), 6)
			if score > bestScore {
				bestScore, bestO, bestD = score, o, d
			}
		}
	}
	if bestO == root && bestD == root {
		return DataJourney{}, 0, false
	}
	// Assemble: origin … root … destination.
	var edges []*dataflow.Edge
	bp := back[bestO]
	for i := len(bp) - 1; i >= 0; i-- {
		edges = append(edges, bp[i])
	}
	edges = append(edges, fwd[bestD]...)
	ids := []int{bestO}
	for _, e := range edges {
		ids = append(ids, e.To)
	}
	j := DataJourney{Func: b.Symbol.ID, FuncName: b.Symbol.Qualified, Variable: key,
		Evidence: Evidence{Basis: Static, Text: "Data Flow (SSA def-use) of " + key + " in " + b.Symbol.Qualified, File: b.Symbol.File, Line: b.Symbol.Line, Ref: "inspect:" + b.Symbol.ID + ":dataflow"}}
	for i, id := range ids {
		n := r.Node(id)
		st := JourneyStep{Kind: n.Kind, Label: n.Label, Var: n.Var, Type: n.Type, Func: n.Func, FuncName: n.FuncName, File: n.File, Line: n.Line, Code: n.Code, Role: RoleTransform}
		if i > 0 {
			e := edges[i-1]
			st.Via, st.ViaText, st.Conf = e.Kind, e.Label, e.Conf
		}
		switch {
		case i == 0 && (sourceScore[n.Kind] > 0 || n.Kind == dataflow.KindMerge || n.Kind == dataflow.KindValue):
			st.Role = RoleSource
		case n.Kind == dataflow.KindCheck:
			st.Role = RoleDecision
		case i == len(ids)-1 && (sinkScore[n.Kind] > 0 || st.Via == dataflow.EdgeSink):
			st.Role = RoleSink
		}
		j.Steps = append(j.Steps, st)
	}
	first, last := j.Steps[0], j.Steps[len(j.Steps)-1]
	j.Complete = first.Role == RoleSource && (last.Role == RoleSink || last.Role == RoleDecision)
	if r.Status.Status != evidence.Complete {
		j.Partial = append(j.Partial, r.Status.Reasons...)
	}
	return j, bestScore, true
}

// bfs returns, for every node reachable from root (backwards over origin
// edges, or forwards over destination edges), the shortest edge path.
func bfs(r *dataflow.Result, root int, origin bool) map[int][]*dataflow.Edge {
	paths := map[int][]*dataflow.Edge{root: nil}
	queue := []int{root}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, e := range next(r, id, origin) {
			other := e.To
			if origin {
				other = e.From
			}
			if _, seen := paths[other]; seen {
				continue
			}
			p := append(append([]*dataflow.Edge(nil), paths[id]...), e)
			paths[other] = p
			queue = append(queue, other)
		}
	}
	return paths
}

func next(r *dataflow.Result, id int, origin bool) []*dataflow.Edge {
	var out []*dataflow.Edge
	if origin {
		for _, e := range r.In(id) {
			if e.Dir == string(dataflow.Origin) {
				out = append(out, e)
			}
		}
	} else {
		for _, e := range r.Out(id) {
			if e.Dir == string(dataflow.Destination) {
				out = append(out, e)
			}
		}
	}
	return out
}

func hasMore(r *dataflow.Result, id int, origin bool) bool { return len(next(r, id, origin)) > 0 }
