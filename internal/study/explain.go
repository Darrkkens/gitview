package study

import (
	"github.com/athos/gitview/internal/flow"
)

// ExplainItem is one point of the "explain it yourself" checklist, with
// the facts the analysis found, shown only when the reader asks.
type ExplainItem struct {
	Key    string   `json:"key"`
	Prompt string   `json:"prompt"`
	Facts  []string `json:"facts"`
	Basis  string   `json:"basis"`
	Ref    string   `json:"ref,omitempty"`
}

// ExplainFunc is the checklist of one function of the reading order.
type ExplainFunc struct {
	Func     string        `json:"func"`
	FuncName string        `json:"funcName"`
	File     string        `json:"file"`
	Line     int           `json:"line"`
	Layer    string        `json:"layer"`
	Items    []ExplainItem `json:"items"`
}

const maxExplainFacts = 12

func explain(m *Model, x *graph, a *arch, order []string) []ExplainFunc {
	var out []ExplainFunc
	for _, n := range orderedFuncs(x, order) {
		l, _ := a.layerOf(n.ID)
		ef := ExplainFunc{Func: n.ID, FuncName: n.Name, File: n.File, Line: n.Line, Layer: l}
		item := func(key, prompt, basis, ref string, facts []string) {
			if len(facts) > maxExplainFacts {
				facts = append(facts[:maxExplainFacts], "… and "+itoa(len(facts)-maxExplainFacts)+" more")
			}
			ef.Items = append(ef.Items, ExplainItem{Key: key, Prompt: prompt, Facts: facts, Basis: basis, Ref: ref})
		}

		var callers []string
		for _, e := range x.in[n.ID] {
			if f := x.nodes[e.From]; f != nil {
				if e.Kind == flow.EdgeHandler {
					callers = append(callers, "handles "+f.Name+" ("+loc(e.File, e.Line)+")")
				} else {
					callers = append(callers, f.Name+" at "+loc(e.File, e.Line))
				}
			}
		}
		item("callers", "I can say who calls it, or how execution reaches it.", Static, "codeflow:"+n.ID, callers)

		params, results, ok := sigParts(n.Signature)
		if ok {
			item("inputs", "I can list what it receives.", Static, "codeflow:"+n.ID, params)
		}

		var calls []string
		seen := map[string]bool{}
		for _, e := range x.callees(n.ID) {
			t := x.nodes[e.To]
			if t == nil || seen[t.ID] {
				continue
			}
			seen[t.ID] = true
			what := t.Name
			switch t.Kind {
			case flow.KindDB:
				what += " (database)"
			case flow.KindExternal:
				what += " (outside the module)"
			}
			if e.Confidence != flow.Confirmed {
				what += " — " + e.Confidence
			}
			calls = append(calls, what+" at "+loc(e.File, e.Line))
		}
		item("calls", "I can name what it calls, in order.", Static, "codeflow:"+n.ID, calls)

		var decs []string
		for _, f := range m.Decisions {
			if f.Func != n.ID {
				continue
			}
			for _, d := range f.Decisions {
				s := "line " + itoa(d.Line) + ": " + d.Kind + " " + d.Cond
				for _, o := range d.Outcomes {
					s += " · " + o.Label + " → "
					switch {
					case o.First != "":
						s += o.First
					case o.Terminal != "":
						s += o.Terminal
					default:
						s += "continues"
					}
				}
				decs = append(decs, s)
			}
		}
		item("decisions", "I can say what each condition decides.", Static, "inspect:"+n.ID+":branches", decs)

		var data []string
		for _, j := range m.DataJourneys {
			if j.Func != n.ID || len(j.Steps) == 0 {
				continue
			}
			first, last := j.Steps[0], j.Steps[len(j.Steps)-1]
			data = append(data, varName(j.Variable)+": "+first.Label+" → … → "+last.Label)
		}
		item("data", "I can say where its data comes from and where it goes.", Static, "inspect:"+n.ID+":dataflow", data)

		var sql []string
		for _, q := range m.SQL.Queries {
			if q.Func != n.ID {
				continue
			}
			s := q.Statement + " via " + q.Method + " at line " + itoa(q.Line)
			if q.Structure != nil {
				for i, t := range q.Structure.Tables {
					if i == 0 {
						s += ": "
					} else {
						s += ", "
					}
					s += t.Name
				}
			}
			sql = append(sql, s)
		}
		item("sql", "I can say what it reads from or writes to the database.", SQLBasis, "inspect:"+n.ID+":sql", sql)

		if ok {
			item("returns", "I can say what it returns.", Static, "codeflow:"+n.ID, []string{results})
		}

		var tests []string
		for _, f := range m.TestMatrix {
			if f.Func != n.ID {
				continue
			}
			for _, t := range f.Tests {
				s := t.Name + " (" + t.Basis
				if t.Hops > 0 {
					s += ", " + itoa(t.Hops) + " call(s) away"
				}
				s += ")"
				if t.Status != "" {
					s += " · last run: " + t.Status
				}
				tests = append(tests, s)
			}
		}
		item("tests", "I can name the tests that exercise it.", Static, "inspect:"+n.ID+":branches", tests)
		out = append(out, ef)
	}
	return out
}
