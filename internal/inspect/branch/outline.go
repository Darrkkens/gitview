package branch

import (
	"fmt"
	"math/bits"
	"strings"
)

// OutlineRow is one line of the structured text rendering of a graph.
type OutlineRow struct {
	Depth int    `json:"depth"`
	Block int    `json:"block"`
	Label string `json:"label,omitempty"` // edge label leading to this row
	Ref   bool   `json:"ref,omitempty"`   // block already shown: this row only points to it
	Loop  bool   `json:"loop,omitempty"`  // back edge closing a loop
	Join  bool   `json:"join,omitempty"`  // branch that goes straight to the merge point
	Error bool   `json:"error,omitempty"` // edge taken when an error check finds an error
}

// Outline renders the graph as nested branches: each decision lists its
// outcomes one level deeper, and the code after the decision (its immediate
// post-dominator, where the branches merge) continues at the same level.
func (g *Graph) Outline() []OutlineRow {
	if len(g.Blocks) == 0 {
		return nil
	}
	ipdom := g.postDominators()
	shown := map[int]bool{}
	var rows []OutlineRow
	var render func(id, depth, stop int, label string, errEdge bool)
	render = func(id, depth, stop int, label string, errEdge bool) {
		for id >= 0 && id != stop {
			if shown[id] {
				rows = append(rows, OutlineRow{Depth: depth, Block: id, Label: label, Ref: true, Error: errEdge})
				return
			}
			shown[id] = true
			rows = append(rows, OutlineRow{Depth: depth, Block: id, Label: label, Error: errEdge})
			label, errEdge = "", false
			var fwd []*Edge
			for _, e := range g.Out(id) {
				if e.Kind == EdgeLoop {
					rows = append(rows, OutlineRow{Depth: depth + 1, Block: e.To, Label: e.Label, Ref: true, Loop: true})
					continue
				}
				fwd = append(fwd, e)
			}
			switch len(fwd) {
			case 0:
				return
			case 1:
				label, errEdge = fwd[0].Label, fwd[0].Error
				id = fwd[0].To
				continue
			}
			join := ipdom[id]
			for _, e := range fwd {
				if e.To == join {
					rows = append(rows, OutlineRow{Depth: depth + 1, Block: e.To, Label: e.Label, Ref: true, Join: true, Error: e.Error})
					continue
				}
				render(e.To, depth+1, join, e.Label, e.Error)
			}
			id = join
		}
		if id >= 0 && id == stop && label != "" {
			rows = append(rows, OutlineRow{Depth: depth, Block: id, Label: label, Ref: true, Join: true, Error: errEdge})
		}
	}
	render(g.EntryID, 0, -1, "", false)
	// Dead code is listed after the reachable graph.
	for _, b := range g.Blocks {
		if !shown[b.ID] && !b.Reachable {
			render(b.ID, 0, -1, "", false)
		}
	}
	return rows
}

// postDominators returns the immediate post-dominator of each reachable
// block (-1 when control leaves the function or never reaches an exit).
func (g *Graph) postDominators() map[int]int {
	n := len(g.Blocks)
	exit := n // virtual exit node
	succ := make([][]int, n+1)
	for _, e := range g.Edges {
		succ[e.From] = append(succ[e.From], e.To)
	}
	for _, b := range g.Blocks {
		if b.Terminal != "" {
			succ[b.ID] = append(succ[b.ID], exit)
		}
	}
	words := (n + 1 + 63) / 64
	full := func() []uint64 {
		s := make([]uint64, words)
		for i := 0; i <= n; i++ {
			s[i/64] |= 1 << (i % 64)
		}
		return s
	}
	pdom := make([][]uint64, n+1)
	for i := range pdom {
		pdom[i] = full()
	}
	pdom[exit] = make([]uint64, words)
	pdom[exit][exit/64] |= 1 << (exit % 64)
	for changed := true; changed; {
		changed = false
		for i := n - 1; i >= 0; i-- {
			next := make([]uint64, words)
			if len(succ[i]) == 0 {
				// No way out: post-dominated only by itself.
			} else {
				copy(next, pdom[succ[i][0]])
				for _, s := range succ[i][1:] {
					for w := range next {
						next[w] &= pdom[s][w]
					}
				}
			}
			next[i/64] |= 1 << (i % 64)
			for w := range next {
				if next[w] != pdom[i][w] {
					pdom[i] = next
					changed = true
					break
				}
			}
		}
	}
	has := func(set []uint64, j int) bool { return set[j/64]&(1<<(j%64)) != 0 }
	size := make([]int, n+1)
	for i := 0; i <= n; i++ {
		for _, w := range pdom[i] {
			size[i] += bits.OnesCount64(w)
		}
	}
	out := map[int]int{}
	for i := 0; i < n; i++ {
		// The strict post-dominators of i form a chain; the immediate one is
		// post-dominated by all the others, so its own set is the largest.
		out[i] = -1
		best := -1
		for j := 0; j < n; j++ {
			if j != i && has(pdom[i], j) && size[j] > best {
				best, out[i] = size[j], j
			}
		}
	}
	return out
}

// Summary is a one-line description of a block for lists and graph boxes.
func (b *Block) Summary() string {
	switch {
	case b.Decision != "":
		switch b.Decision {
		case DecIf:
			return "if " + b.Cond
		case DecFor:
			return "for " + b.Cond
		case DecRange:
			return "for " + b.Cond
		case DecSwitch:
			if b.Cond == "true" {
				return "switch"
			}
			return "switch " + b.Cond
		case DecTypeSwitch:
			return "switch " + b.Cond
		}
		return b.Decision
	case len(b.Stmts) > 0:
		last := b.Stmts[len(b.Stmts)-1]
		if len(b.Stmts) == 1 {
			return last.Text
		}
		if b.Terminal == TermImplicit {
			return fmt.Sprintf("%s  … (end of function)", b.Stmts[0].Text)
		}
		if b.Terminal != "" {
			return fmt.Sprintf("%s  (+%d stmt)", last.Text, len(b.Stmts)-1)
		}
		return fmt.Sprintf("%s  (+%d stmt)", b.Stmts[0].Text, len(b.Stmts)-1)
	case b.Entry:
		return "entry"
	case b.LoopHead:
		return "loop"
	}
	return "(empty)"
}

// Dot renders the graph in Graphviz format.
func (g *Graph) Dot() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "digraph %q {\n  node [shape=box fontname=monospace fontsize=10];\n", g.Symbol.Qualified)
	for _, b := range g.Blocks {
		label := fmt.Sprintf("B%d  %s:%d\\n", b.ID, b.File, b.Line)
		for _, s := range b.Stmts {
			label += dotEscape(s.Text) + "\\l"
		}
		if b.Decision != "" {
			label += dotEscape(b.Summary()) + "?\\l"
		}
		attrs := ""
		switch {
		case !b.Reachable:
			attrs = " style=dashed color=gray"
		case b.Terminal == TermPanic || b.Terminal == TermExit:
			attrs = " color=red"
		case b.Terminal != "":
			attrs = " peripheries=2"
		case b.Decision != "":
			attrs = " shape=diamond"
		}
		fmt.Fprintf(&sb, "  b%d [label=\"%s\"%s];\n", b.ID, label, attrs)
	}
	for _, e := range g.Edges {
		attrs := ""
		if e.Kind == EdgeLoop {
			attrs = " style=dashed"
		}
		fmt.Fprintf(&sb, "  b%d -> b%d [label=\"%s\"%s];\n", e.From, e.To, dotEscape(e.Label), attrs)
	}
	sb.WriteString("}\n")
	return sb.String()
}

func dotEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(s)
}
