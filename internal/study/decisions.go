package study

import (
	"regexp"
	"sort"
	"strings"

	"github.com/athos/gitview/internal/inspect"
	"github.com/athos/gitview/internal/inspect/branch"
)

// Outcome is one branch of a decision and what the CFG says happens there.
type Outcome struct {
	Label        string   `json:"label"` // true | false | case … | default | next | done
	To           int      `json:"to"`    // CFG block
	First        string   `json:"first,omitempty"`
	FirstLine    int      `json:"firstLine,omitempty"`
	Terminal     string   `json:"terminal,omitempty"` // return | panic | exit | …
	ReturnsError bool     `json:"returnsError,omitempty"`
	Jump         string   `json:"jump,omitempty"` // break | continue | goto …
	Assigns      []string `json:"assigns,omitempty"`
	ErrorPath    bool     `json:"errorPath,omitempty"` // taken when an error is present
	Coverage     string   `json:"coverage,omitempty"`  // covered | uncovered | partial | unknown (COVERAGE)
}

// Decision is a decision point of a function's CFG.
type Decision struct {
	Block      int       `json:"block"`
	Kind       string    `json:"kind"` // if | for | range | switch | type switch | select
	Cond       string    `json:"cond"`
	File       string    `json:"file"`
	Line       int       `json:"line"`
	ErrorCheck bool      `json:"errorCheck,omitempty"`
	Preds      []int     `json:"preds,omitempty"`
	Outcomes   []Outcome `json:"outcomes"`
	Evidence   Evidence  `json:"evidence"`
}

// FuncDecisions are the decisions of one function.
type FuncDecisions struct {
	Func       string     `json:"func"`
	FuncName   string     `json:"funcName"`
	File       string     `json:"file"`
	Line       int        `json:"line"`
	Cyclomatic int        `json:"cyclomatic"`
	Coverage   string     `json:"coverage,omitempty"` // profile the outcomes were overlaid with
	Decisions  []Decision `json:"decisions"`
}

var assignLHS = regexp.MustCompile(`^\s*([A-Za-z_][\w.\[\]]*(?:\s*,\s*[A-Za-z_][\w.\[\]]*)*)\s*(?::=|=|\+=|-=|\*=|/=|%=|\|=|&=|<<=|>>=|\^=|\+\+|--)`)

// assigned extracts the targets of an assignment statement's text.
func assigned(text string) []string {
	m := assignLHS.FindStringSubmatch(text)
	if m == nil {
		return nil
	}
	var out []string
	for _, v := range strings.Split(m[1], ",") {
		if v = strings.TrimSpace(v); v != "" && v != "_" {
			out = append(out, v)
		}
	}
	return out
}

// decisions reads the decision points of the report's CFGs (Branch
// Explorer), in the order of the functions given.
func decisions(bundles []*inspect.Bundle, order []string) []FuncDecisions {
	var out []FuncDecisions
	for _, b := range orderBundles(bundles, order) {
		g := b.Branches
		if g == nil {
			continue
		}
		blocks := map[int]*branch.Block{}
		for _, bl := range g.Blocks {
			blocks[bl.ID] = bl
		}
		outEdges := map[int][]*branch.Edge{}
		preds := map[int][]int{}
		for _, e := range g.Edges {
			outEdges[e.From] = append(outEdges[e.From], e)
			preds[e.To] = append(preds[e.To], e.From)
		}
		fd := FuncDecisions{Func: b.Symbol.ID, FuncName: b.Symbol.Qualified, File: b.Symbol.File, Line: b.Symbol.Line, Cyclomatic: g.Metrics.Cyclomatic}
		if g.Coverage != nil {
			fd.Coverage = g.Coverage.Profile
		}
		for _, bl := range g.Blocks {
			if bl.Decision == "" || !bl.Reachable {
				continue
			}
			line := bl.CondLine
			if line == 0 {
				line = bl.Line
			}
			d := Decision{Block: bl.ID, Kind: bl.Decision, Cond: bl.Cond, File: bl.File, Line: line, ErrorCheck: bl.ErrorCheck,
				Evidence: Evidence{Basis: Static, Text: "decision of the control-flow graph (go/cfg) of " + b.Symbol.Qualified, File: bl.File, Line: line, Ref: "inspect:" + b.Symbol.ID + ":branches"}}
			d.Preds = append(d.Preds, preds[bl.ID]...)
			sort.Ints(d.Preds)
			for _, e := range outEdges[bl.ID] {
				if e.Kind != branch.EdgeBranch {
					continue
				}
				o := Outcome{Label: e.Label, To: e.To, ErrorPath: e.Error}
				if t := blocks[e.To]; t != nil {
					describe(&o, t, outEdges, blocks)
				}
				d.Outcomes = append(d.Outcomes, o)
			}
			if len(d.Outcomes) > 0 {
				fd.Decisions = append(fd.Decisions, d)
			}
		}
		if len(fd.Decisions) > 0 {
			out = append(out, fd)
		}
	}
	return out
}

// describe fills what happens in the block an outcome leads to: its first
// statement, how it ends and what it assigns (following blocks that only
// jump, at most a few).
func describe(o *Outcome, t *branch.Block, outEdges map[int][]*branch.Edge, blocks map[int]*branch.Block) {
	o.Coverage = t.Coverage
	for hops := 0; t != nil && hops < 4; hops++ {
		if len(t.Stmts) == 0 && t.Decision != "" && o.First == "" {
			// Straight into another decision.
			o.First, o.FirstLine = t.Decision+" "+t.Cond, t.CondLine
			return
		}
		for _, st := range t.Stmts {
			if o.First == "" {
				o.First, o.FirstLine = st.Text, st.Line
			}
			if st.Kind == "assign" || st.Kind == "var" {
				o.Assigns = append(o.Assigns, assigned(st.Text)...)
			}
		}
		if t.Terminal != "" {
			o.Terminal, o.ReturnsError = t.Terminal, t.ReturnsError
			return
		}
		es := outEdges[t.ID]
		if len(es) == 1 && es[0].Kind == branch.EdgeJump && es[0].Label != "" {
			o.Jump = es[0].Label
			return
		}
		if len(t.Stmts) > 0 || len(es) != 1 {
			return
		}
		t = blocks[es[0].To]
	}
}

// orderBundles sorts bundles by the given order, the others after by ID.
func orderBundles(bundles []*inspect.Bundle, order []string) []*inspect.Bundle {
	rank := map[string]int{}
	for i, id := range order {
		rank[id] = i
	}
	out := append([]*inspect.Bundle(nil), bundles...)
	sort.SliceStable(out, func(i, j int) bool {
		ri, iok := rank[out[i].Symbol.ID]
		rj, jok := rank[out[j].Symbol.ID]
		if iok != jok {
			return iok
		}
		if ri != rj {
			return ri < rj
		}
		return out[i].Symbol.ID < out[j].Symbol.ID
	})
	return out
}
