package audit

import (
	"fmt"

	"github.com/athos/gitview/internal/graph/model"
)

// GraphPath is a path between two nodes of the Repository Graph, with the
// findings whose evidence lies on it and the unresolved calls next to it.
type GraphPath struct {
	Steps      []PathStep
	Related    []*Finding
	Unresolved []string
}

// PathBetween finds the shortest path from one node to another following
// the relations of the graph (a route's chain, calls, queries). It only
// uses edges the analysis found; when there is none it says so.
func PathBetween(g *model.Graph, a *Audit, from, to string) (*GraphPath, error) {
	nodes := g.Index()
	if nodes[from] == nil {
		return nil, fmt.Errorf("node %q is not in the graph", from)
	}
	if nodes[to] == nil {
		return nil, fmt.Errorf("node %q is not in the graph", to)
	}
	out := map[string][]*model.Edge{}
	for _, e := range g.Edges {
		if flowEdge[e.Type] {
			out[e.Source] = append(out[e.Source], e)
		}
	}
	prev := map[string]*model.Edge{}
	seen := map[string]bool{from: true}
	queue := []string{from}
	for len(queue) > 0 && !seen[to] {
		x := queue[0]
		queue = queue[1:]
		for _, e := range out[x] {
			if !seen[e.Target] {
				seen[e.Target] = true
				prev[e.Target] = e
				queue = append(queue, e.Target)
			}
		}
	}
	if !seen[to] {
		return nil, fmt.Errorf("the analysis found no path from %s to %s (calls through function values or interfaces it could not resolve may exist)", nodes[from].Name, nodes[to].Name)
	}
	var ids []string
	var confs []string
	for x := to; x != from; x = prev[x].Source {
		ids = append([]string{x}, ids...)
		confs = append([]string{prev[x].Confidence}, confs...)
	}
	ids = append([]string{from}, ids...)
	confs = append([]string{""}, confs...)
	var routes map[string]*model.Route
	if g.HTTP != nil {
		routes = map[string]*model.Route{}
		for _, r := range g.HTTP.Routes {
			routes[r.ID] = r
		}
	}
	p := &GraphPath{}
	onPath := map[string]bool{}
	s := &scanner{routes: routes}
	for k, id := range ids {
		n := nodes[id]
		onPath[id] = true
		st := PathStep{ID: id, Name: n.Name, Role: "function", File: n.File, Line: n.Line, Conf: confs[k]}
		switch n.Type {
		case model.KindRoute:
			st.Role = "route"
		case model.KindDB:
			st.Role = "sink"
		}
		if k > 0 && routes != nil {
			if r := routes[ids[0]]; r != nil {
				st.Check = s.chainCheck(r, nameOf(r, id))
				if r.Handler != nil && r.Handler.ID == id {
					st.Role = "handler"
				} else if k == 1 {
					st.Role = "middleware"
				}
			}
		}
		p.Steps = append(p.Steps, st)
		for _, e := range out[id] {
			if t := nodes[e.Target]; t != nil && t.Type == model.KindUnresolved {
				p.Unresolved = append(p.Unresolved, fmt.Sprintf("%s: %s (%s:%d)", n.Name, t.Name, t.File, t.Line))
			}
		}
	}
	if a != nil {
		for _, f := range a.Findings {
			if onPath[f.SymbolID] {
				p.Related = append(p.Related, f)
			}
		}
	}
	return p, nil
}
