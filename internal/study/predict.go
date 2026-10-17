package study

import (
	"sort"

	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/inspect/xtrace"
)

// Candidate is a function the call graph says an entry point may call.
type Candidate struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Layer string `json:"layer"`
	Depth int    `json:"depth"` // calls from the entry point
	File  string `json:"file,omitempty"`
	Line  int    `json:"line,omitempty"`
}

// PredictEntry is a starting point the reader can predict the execution
// of.
type PredictEntry struct {
	ID         string      `json:"id"`
	Title      string      `json:"title"`
	Layer      string      `json:"layer"`
	File       string      `json:"file,omitempty"`
	Line       int         `json:"line,omitempty"`
	Candidates []Candidate `json:"candidates"`
	// StaticOrder is the order in which the calls appear in the code, depth
	// first from the entry (a possible order, not an observed one).
	StaticOrder []string `json:"staticOrder"`
	// Observed is the order in which the entry and the functions below it
	// first appear in the trace (only stacks where they run under the
	// entry); empty without a trace.
	Observed []ObservedCall `json:"observed,omitempty"`
}

// ObservedCall is a module function seen in the execution trace.
type ObservedCall struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	FirstNs int64  `json:"firstNs"` // first time it appears, from the start of the trace
	How     string `json:"how"`     // region (measured span) | stack (stack of a trace event)
	// Seq is the position in the observed order; calls first seen at the
	// same instant and stack depth share it (their relative order is not
	// known).
	Seq int `json:"seq"`
}

// ObservedRun describes the execution trace the entries were compared
// with.
type ObservedRun struct {
	Source    string `json:"source"`
	Functions int    `json:"functions"` // module functions seen anywhere in the trace
	Note      string `json:"note"`
}

// Predict is the "predict the execution" exercise.
type Predict struct {
	Entries  []PredictEntry `json:"entries"`
	Observed *ObservedRun   `json:"observed,omitempty"`
}

const (
	maxPredictEntries = 8
	maxCandidates     = 30
	maxPredictDepth   = 5
)

// predict lists the entry points of the reading order with the functions
// the call graph says they may call, and the order the trace observed.
func predict(x *graph, a *arch, steps []Step, trace *xtrace.Timeline) Predict {
	var p Predict
	done := map[string]bool{}
	for _, s := range steps {
		if len(p.Entries) >= maxPredictEntries {
			break
		}
		if s.Kind != "route" && !(s.Kind == "function" && isEntry(s)) {
			continue
		}
		start := s.ID
		if s.Kind == "route" {
			// The route's final handler.
			for _, e := range x.out[s.ID] {
				if h := x.nodes[e.To]; e.Kind == flow.EdgeHandler && h != nil && !hasTag(h, "middleware") {
					start = h.ID
				}
			}
		}
		if done[start] {
			continue
		}
		done[start] = true
		en := PredictEntry{ID: start, Title: s.Title, Layer: s.Layer, File: s.File, Line: s.Line}
		depth := map[string]int{start: 0}
		queue := []string{start}
		for len(queue) > 0 && len(en.Candidates) < maxCandidates {
			id := queue[0]
			queue = queue[1:]
			if depth[id] >= maxPredictDepth {
				continue
			}
			for _, e := range x.callees(id) {
				if !x.isModuleFunc(e.To) {
					continue
				}
				if _, seen := depth[e.To]; seen {
					continue
				}
				depth[e.To] = depth[id] + 1
				n := x.nodes[e.To]
				l, _ := a.layerOf(n.ID)
				en.Candidates = append(en.Candidates, Candidate{ID: n.ID, Name: n.Name, Layer: l, Depth: depth[e.To], File: n.File, Line: n.Line})
				queue = append(queue, e.To)
				if len(en.Candidates) >= maxCandidates {
					break
				}
			}
		}
		// Depth-first order of the calls in the code.
		seen := map[string]bool{start: true}
		var dfs func(id string, d int)
		dfs = func(id string, d int) {
			en.StaticOrder = append(en.StaticOrder, id)
			if d >= maxPredictDepth {
				return
			}
			for _, e := range x.callees(id) {
				if x.isModuleFunc(e.To) && !seen[e.To] && depth[e.To] > 0 {
					seen[e.To] = true
					dfs(e.To, d+1)
				}
			}
		}
		dfs(start, 0)
		if trace != nil {
			en.Observed = observed(x, trace, start)
		}
		if len(en.Candidates) > 0 {
			p.Entries = append(p.Entries, en)
		}
	}
	if trace != nil {
		p.Observed = &ObservedRun{Source: trace.Artifact.Name, Functions: len(observed(x, trace, "")),
			Note: "A trace records stacks only at scheduling events (blocking, system calls, goroutine creation) and in regions: a function that ran without such an event does not appear, so \"not observed\" does not mean \"did not run\". Observed functions did run. Only stacks where a function runs under the entry point are counted for it."}
	}
	return p
}

func isEntry(s Step) bool {
	for _, w := range s.Why {
		if len(w.Text) >= 12 && w.Text[:12] == "entry point:" {
			return true
		}
	}
	return false
}

// observed orders the module functions by their first appearance in the
// trace, in stacks that run under entry (any stack when entry is ""): in
// the stack of a trace event, or as a measured region (temporary
// instrumentation or a user region named after the function). At the same
// instant, outer frames come before inner ones.
func observed(x *graph, t *xtrace.Timeline, entry string) []ObservedCall {
	type first struct {
		key int64
		ns  int64
		how string
	}
	best := map[string]first{}
	see := func(id string, key, ns int64, how string) {
		if f, ok := best[id]; !ok || key < f.key || (key == f.key && how == "region") {
			best[id] = first{key, ns, how}
		}
	}
	// frames returns the module functions of stack k from the outermost
	// frame, starting at the entry; ok is false when the entry is not in it.
	frames := func(k int) ([]string, bool) {
		if k < 0 || k >= len(t.Stacks) {
			return nil, entry == ""
		}
		var ids []string
		fs := t.Stacks[k]
		in := entry == ""
		for d := len(fs) - 1; d >= 0; d-- {
			id := fs[d].Symbol
			if !in && id == entry {
				in = true
			}
			if in && id != "" && x.isModuleFunc(id) {
				ids = append(ids, id)
			}
		}
		return ids, in
	}
	seeStack := func(ns int64, k int) {
		ids, ok := frames(k)
		if !ok {
			return
		}
		for d, id := range ids {
			see(id, ns*64+int64(min(d, 62)), ns, "stack")
		}
	}
	for _, r := range t.Regions {
		ids, ok := frames(r.Stack)
		if !ok {
			continue
		}
		seeStack(r.Start, r.Stack)
		if r.Symbol != "" && x.isModuleFunc(r.Symbol) && (entry == "" || r.Symbol == entry || len(ids) > 0) {
			d := 63
			for i, id := range ids {
				if id == r.Symbol {
					d = min(i, 62)
					break
				}
			}
			see(r.Symbol, r.Start*64+int64(d), r.Start, "region")
		}
	}
	for _, e := range t.Events {
		seeStack(e.T, e.Stack)
	}
	for _, g := range t.Goroutines {
		for _, s := range g.Segments {
			seeStack(s.Start, s.Stack)
		}
		if g.CreatedAt >= 0 {
			seeStack(max(g.Created, 0), g.CreatedAt)
		}
	}
	var calls []ObservedCall
	for id, f := range best {
		calls = append(calls, ObservedCall{ID: id, Name: x.nodes[id].Name, FirstNs: f.ns, How: f.how})
	}
	sort.Slice(calls, func(i, j int) bool {
		a, b := best[calls[i].ID], best[calls[j].ID]
		if a.key != b.key {
			return a.key < b.key
		}
		return calls[i].ID < calls[j].ID
	})
	for i := range calls {
		calls[i].Seq = i
		if i > 0 && best[calls[i].ID].key == best[calls[i-1].ID].key {
			calls[i].Seq = calls[i-1].Seq
		}
	}
	return calls
}
