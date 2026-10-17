// Package dataflow traces where a Go value comes from (origin) and where it
// may go (destination) using SSA def-use chains, across function
// boundaries up to a configurable depth.
//
// Every edge states how it was established: "exact" for SSA def-use,
// parameter binding and returns; "derived" when the value is computed from
// the source (conversion, arithmetic, an opaque library call); "likely" or
// "possible" through interface dispatch or memory that may alias; and
// "unresolved" where the analysis cannot follow the value. A relation the
// analysis cannot demonstrate is never reported as exact.
package dataflow

import (
	"github.com/athos/gitview/internal/evidence"
)

type Direction string

const (
	Origin      Direction = "origin"
	Destination Direction = "destination"
	Both        Direction = "both"
)

// Node kinds.
const (
	KindValue      = "value"      // an SSA value (variable, expression result)
	KindParam      = "param"      // function parameter
	KindCall       = "call"       // result of a call
	KindConst      = "const"      // literal or constant
	KindMerge      = "merge"      // φ: one of several assignments, depending on the path
	KindField      = "field"      // struct field in memory
	KindSource     = "source"     // where data enters the program (HTTP input, DB row, environment)
	KindSQL        = "sql"        // SQL text or parameter of a database operation
	KindCheck      = "check"      // condition that decides a branch (validation)
	KindExternal   = "external"   // passed to a function outside the module
	KindReturn     = "return"     // leaves the function with no caller found
	KindUnresolved = "unresolved" // the analysis cannot follow the value further
	KindGlobal     = "global"     // package-level variable
)

// Edge kinds.
const (
	EdgeAssign  = "assign"  // same SSA value bound to a variable or copied
	EdgeConvert = "convert" // conversion or type assertion
	EdgeDerived = "derived" // computed from the value (arithmetic, external call)
	EdgeConcat  = "concat"  // string concatenation
	EdgeArg     = "arg"     // argument → parameter
	EdgeReturn  = "return"  // returned value → call result
	EdgeMerge   = "merge"   // incoming value of a φ
	EdgeStore   = "store"   // written to memory
	EdgeLoad    = "load"    // read from memory
	EdgeCapture = "capture" // captured by a closure
	EdgeSource  = "source"  // produced by an input source
	EdgeSink    = "sink"    // reaches a SQL operation, a check or an external call
)

type Node struct {
	ID           int      `json:"id"`
	Kind         string   `json:"kind"`
	Label        string   `json:"label"`
	Var          string   `json:"var,omitempty"` // variable(s) holding the value at this point
	Type         string   `json:"type,omitempty"`
	Func         string   `json:"func,omitempty"` // symbol ID of the enclosing function
	FuncName     string   `json:"funcName,omitempty"`
	File         string   `json:"file,omitempty"`
	Line         int      `json:"line,omitempty"`
	Code         string   `json:"code,omitempty"` // the source line
	Detail       string   `json:"detail,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	Dir          string   `json:"dir"`   // selected | origin | destination
	Level        int      `json:"level"` // call levels from the selection (callees > 0, callers < 0)
	Hops         int      `json:"hops"`  // edges from the selection
	Conf         string   `json:"conf"`  // weakest confidence on the path from the selection
	Intermediate bool     `json:"intermediate,omitempty"`
}

type Edge struct {
	From  int    `json:"from"`
	To    int    `json:"to"`
	Kind  string `json:"kind"`
	Conf  string `json:"conf"`
	Label string `json:"label,omitempty"`
	File  string `json:"file,omitempty"`
	Line  int    `json:"line,omitempty"`
	Dir   string `json:"dir"` // origin | destination
}

// Selection describes what was traced.
type Selection struct {
	Kind string `json:"kind"` // variable | param | return | expr | value
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
	Line int    `json:"line,omitempty"`
}

// Validation is a condition applied to the traced value (or to a value
// derived from it) that decides a branch.
type Validation struct {
	Node int    `json:"node"`
	Text string `json:"text"`
	Kind string `json:"kind"` // comparison | predicate | error check
	Func string `json:"func"`
	File string `json:"file"`
	Line int    `json:"line"`
}

type Result struct {
	Symbol      evidence.SymbolRef    `json:"symbol"`
	Selection   Selection             `json:"selection"`
	Direction   Direction             `json:"direction"`
	Nodes       []*Node               `json:"nodes"`
	Edges       []*Edge               `json:"edges"`
	Roots       []int                 `json:"roots"`
	Sources     []int                 `json:"sources,omitempty"`
	Sinks       []int                 `json:"sinks,omitempty"`
	Validations []Validation          `json:"validations,omitempty"`
	Status      evidence.Completeness `json:"status"`
	Notes       []string              `json:"notes,omitempty"`
	Evidence    []evidence.Evidence   `json:"evidence"`
	MaxDepth    int                   `json:"maxDepth"`
}

// Node returns the node with the given id.
func (r *Result) Node(id int) *Node {
	if id >= 0 && id < len(r.Nodes) {
		return r.Nodes[id]
	}
	return nil
}

// Out returns the edges leaving id; In those entering it.
func (r *Result) Out(id int) []*Edge {
	var out []*Edge
	for _, e := range r.Edges {
		if e.From == id {
			out = append(out, e)
		}
	}
	return out
}

func (r *Result) In(id int) []*Edge {
	var out []*Edge
	for _, e := range r.Edges {
		if e.To == id {
			out = append(out, e)
		}
	}
	return out
}

var confRank = map[string]int{evidence.Exact: 4, evidence.Derived: 3, evidence.Likely: 2, evidence.Possible: 1, evidence.Unresolved: 0}

// weaker returns the weaker of two confidences.
func weaker(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	if confRank[a] <= confRank[b] {
		return a
	}
	return b
}

// TreeRow is one line of the text rendering of a trace.
type TreeRow struct {
	Depth int   `json:"depth"`
	Node  int   `json:"node"`
	Edge  *Edge `json:"edge,omitempty"`
	Ref   bool  `json:"ref,omitempty"` // node already listed above
}

// Tree renders the trace from the selected nodes: origins (walking edges
// backwards) or destinations (forwards).
func (r *Result) Tree(dir Direction) []TreeRow {
	shown := map[int]bool{}
	var rows []TreeRow
	var walk func(id, depth int)
	walk = func(id, depth int) {
		var edges []*Edge
		if dir == Origin {
			for _, e := range r.In(id) {
				if e.Dir == string(Origin) {
					edges = append(edges, e)
				}
			}
		} else {
			for _, e := range r.Out(id) {
				if e.Dir == string(Destination) {
					edges = append(edges, e)
				}
			}
		}
		for _, e := range edges {
			other := e.To
			if dir == Origin {
				other = e.From
			}
			if shown[other] {
				rows = append(rows, TreeRow{Depth: depth, Node: other, Edge: e, Ref: true})
				continue
			}
			shown[other] = true
			rows = append(rows, TreeRow{Depth: depth, Node: other, Edge: e})
			walk(other, depth+1)
		}
	}
	for _, root := range r.Roots {
		if shown[root] {
			continue
		}
		shown[root] = true
		rows = append(rows, TreeRow{Depth: 0, Node: root})
		walk(root, 1)
	}
	return rows
}

// HideIntermediate returns a copy of r without intermediate SSA values,
// connecting their neighbours directly (keeping the weakest confidence).
func (r *Result) HideIntermediate() *Result {
	out := *r
	hidden := map[int]bool{}
	for _, n := range r.Nodes {
		if n.Intermediate && n.Dir != "selected" {
			hidden[n.ID] = true
		}
	}
	if len(hidden) == 0 {
		return &out
	}
	edges := append([]*Edge(nil), r.Edges...)
	for id := range hidden {
		var in, outE, rest []*Edge
		for _, e := range edges {
			switch {
			case e.To == id && e.From != id:
				in = append(in, e)
			case e.From == id && e.To != id:
				outE = append(outE, e)
			case e.From != id && e.To != id:
				rest = append(rest, e)
			}
		}
		for _, a := range in {
			for _, b := range outE {
				kind := b.Kind
				if a.Kind != EdgeAssign && b.Kind == EdgeAssign {
					kind = a.Kind
				}
				rest = append(rest, &Edge{From: a.From, To: b.To, Kind: kind, Conf: weaker(a.Conf, b.Conf),
					Label: joinLabels(a.Label, b.Label), File: b.File, Line: b.Line, Dir: b.Dir})
			}
		}
		edges = rest
	}
	out.Edges = edges
	return &out
}

func joinLabels(a, b string) string {
	switch {
	case a == "" || a == b:
		return b
	case b == "":
		return a
	}
	return a + " · " + b
}
