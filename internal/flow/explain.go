package flow

import (
	"fmt"
	"sort"
	"strings"
)

// Explanation is attached to each node: a short, factual description built
// only from the relations found by the analysis.
func (b *builder) explain(n *Node) {
	g := b.g
	var s []string
	switch n.Kind {
	case KindRoute:
		s = append(s, fmt.Sprintf("HTTP route %s registered with %s at %s:%d.", n.Name, n.Router, n.File, n.Line))
		var chain []string
		for _, e := range g.Out(n.ID) {
			if to := b.nodes[e.To]; to != nil {
				chain = append(chain, to.Name)
			}
		}
		if len(chain) > 0 {
			s = append(s, "Requests run, in order: "+strings.Join(chain, " → ")+".")
		}
		if n.Partial {
			s = append(s, "Resolution is partial: "+n.Note+".")
		}
	case KindDB:
		s = append(s, fmt.Sprintf("Database operation %s through %s at %s:%d.", n.DBOp, n.DBLib, n.File, n.Line))
		switch {
		case n.SQL != "":
			s = append(s, "Uses a SQL literal written in the code.")
		case n.SQLDynamic:
			s = append(s, "The SQL is built at runtime (dynamic SQL), so its text is not known statically.")
		}
		s = append(s, "This is a possible execution path found statically, not an observed query.")
	case KindExternal:
		s = append(s, fmt.Sprintf("%s belongs to %s, outside the analysed module; its internals are not explored.", n.Name, n.Pkg))
	case KindUnresolved:
		s = append(s, "A dynamic call whose target cannot be determined statically.")
		if n.Note != "" {
			s = append(s, upperFirst(n.Note)+".")
		}
	default:
		what := "Function"
		if n.Kind == KindClosure {
			what = "Anonymous function"
		} else if strings.Contains(n.Name, ").") || strings.Count(n.Name, ".") > 1 {
			what = "Method"
		}
		s = append(s, fmt.Sprintf("%s %s in package %s, defined at %s:%d.", what, n.Name, n.Pkg, n.File, n.Line))
		if n.Changed {
			s = append(s, "It was modified in the working tree.")
		}
		counts := map[string][]string{}
		var goes, defers, dbs []string
		for _, e := range g.Out(n.ID) {
			to := b.nodes[e.To]
			if to == nil {
				continue
			}
			switch {
			case to.Kind == KindDB:
				dbs = append(dbs, to.DBOp)
			case e.Kind == EdgeGo:
				goes = append(goes, to.Name)
			case e.Kind == EdgeDefer:
				defers = append(defers, to.Name)
			default:
				counts[e.Confidence] = append(counts[e.Confidence], to.Name)
			}
		}
		if c := counts[Confirmed]; len(c) > 0 {
			s = append(s, fmt.Sprintf("It calls %s.", list(c)))
		}
		if c := counts[Likely]; len(c) > 0 {
			s = append(s, fmt.Sprintf("Through an interface with a single implementation in the module, it likely calls %s.", list(c)))
		}
		if c := counts[Possible]; len(c) > 0 {
			s = append(s, fmt.Sprintf("It may call %s (interface dispatch or callbacks with more than one possible target).", list(c)))
		}
		if c := counts[Unresolved]; len(c) > 0 {
			s = append(s, fmt.Sprintf("%d call(s) could not be resolved statically.", len(c)))
		}
		if len(goes) > 0 {
			s = append(s, fmt.Sprintf("It starts goroutine(s) running %s.", list(goes)))
		}
		if len(defers) > 0 {
			s = append(s, fmt.Sprintf("It defers %s.", list(defers)))
		}
		if len(dbs) > 0 {
			s = append(s, fmt.Sprintf("It performs database operation(s): %s.", list(dbs)))
		}
		var callers []string
		for _, e := range g.In(n.ID) {
			if from := b.nodes[e.From]; from != nil {
				callers = append(callers, from.Name)
			}
		}
		if fn := b.fnOf[n.ID]; fn != nil {
			total := len(b.p.callers[fn])
			switch {
			case total == 0:
				s = append(s, "No callers were found in the module (it may be an entry point, a test target or called via reflection).")
			case len(callers) > 0:
				s = append(s, fmt.Sprintf("Called by %s; %d call site(s) in the module overall.", list(callers), total))
			default:
				s = append(s, fmt.Sprintf("It has %d call site(s) in the module.", total))
			}
		}
		if n.Recursive {
			s = append(s, "It is part of a recursive cycle.")
		}
		if n.Truncated {
			s = append(s, "Its calls were not explored further because of the depth or size limit.")
		}
	}
	n.Explain = strings.Join(s, " ")
}

func list(items []string) string {
	seen := map[string]bool{}
	var u []string
	for _, i := range items {
		if !seen[i] {
			seen[i] = true
			u = append(u, i)
		}
	}
	sort.Strings(u)
	if len(u) > 6 {
		return strings.Join(u[:6], ", ") + fmt.Sprintf(" and %d more", len(u)-6)
	}
	return strings.Join(u, ", ")
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
