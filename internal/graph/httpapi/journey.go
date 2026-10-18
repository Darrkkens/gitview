package httpapi

import (
	"sort"

	"github.com/athos/gitview/internal/graph/model"
)

// Index is an adjacency index of a dataset, built once so that route
// subgraphs never scan every edge.
type Index struct {
	Nodes map[string]*model.Node
	Out   map[string][]*model.Edge
	In    map[string][]*model.Edge
}

func NewIndex(g *model.Graph) *Index {
	ix := &Index{Nodes: g.Index(), Out: map[string][]*model.Edge{}, In: map[string][]*model.Edge{}}
	for _, e := range g.Edges {
		ix.Out[e.Source] = append(ix.Out[e.Source], e)
		ix.In[e.Target] = append(ix.In[e.Target], e)
	}
	return ix
}

// Relations followed from a route's handler chain.
var flowEdges = map[string]bool{model.EdgeCalls: true, model.EdgeGo: true, model.EdgeDefer: true, model.EdgeQueries: true}

// Step is a node of a route's execution graph.
type Step struct {
	Node  *model.Node
	Depth int    // 0 for the route, 1 for its handler chain
	Role  string // route | middleware | handler | call
	Order int    // position in the handler chain
}

// Journey is the subgraph of the code a route may run: its handler chain
// and what the chain calls, up to a depth. Edges are the dataset's own; a
// call is a possible path, not one a given request takes.
type Journey struct {
	Route      *model.Node
	Steps      []Step
	Edges      []*model.Edge
	SQL        []*model.Node
	Unresolved int // calls with no static target, within the depth
	Recursive  bool
	Truncated  bool // the depth or node limit cut some calls
	Depth      int
}

// Journey builds the execution graph of a route. depth counts calls from
// the handler chain; maxNodes bounds the result.
func (ix *Index) Journey(routeID string, depth, maxNodes int) *Journey {
	root := ix.Nodes[routeID]
	if root == nil || root.Type != model.KindRoute {
		return nil
	}
	j := &Journey{Route: root, Depth: depth}
	at := map[string]int{routeID: 0}
	j.Steps = append(j.Steps, Step{Node: root, Role: "route"})
	chain := append([]*model.Edge{}, ix.Out[routeID]...)
	sort.SliceStable(chain, func(a, b int) bool { return chainOrder(chain[a]) < chainOrder(chain[b]) })
	var frontier []string
	for _, e := range chain {
		if e.Type != model.EdgeHandles {
			continue
		}
		n := ix.Nodes[e.Target]
		if _, ok := at[n.ID]; ok {
			continue
		}
		role := "handler"
		if r, _ := n.Metadata["role"].(string); r == "middleware" {
			role = "middleware"
		}
		at[n.ID] = 1
		j.Steps = append(j.Steps, Step{Node: n, Depth: 1, Role: role, Order: chainOrder(e)})
		frontier = append(frontier, n.ID)
	}
	for d := 2; d <= depth+1 && len(frontier) > 0; d++ {
		var next []string
		for _, id := range frontier {
			for _, e := range ix.Out[id] {
				if !flowEdges[e.Type] {
					continue
				}
				if _, ok := at[e.Target]; ok {
					continue
				}
				if len(j.Steps) >= maxNodes {
					j.Truncated = true
					continue
				}
				at[e.Target] = d
				j.Steps = append(j.Steps, Step{Node: ix.Nodes[e.Target], Depth: d, Role: "call"})
				next = append(next, e.Target)
			}
		}
		frontier = next
	}
	// Calls left out at the last level.
	for _, id := range frontier {
		for _, e := range ix.Out[id] {
			if flowEdges[e.Type] {
				if _, ok := at[e.Target]; !ok {
					j.Truncated = true
				}
			}
		}
	}
	for _, s := range j.Steps {
		for _, e := range ix.Out[s.Node.ID] {
			if _, ok := at[e.Target]; !ok || !(flowEdges[e.Type] || e.Type == model.EdgeHandles) {
				continue
			}
			j.Edges = append(j.Edges, e)
			if e.Confidence == model.Unresolved || ix.Nodes[e.Target].Type == model.KindUnresolved {
				j.Unresolved++
			}
		}
		if s.Node.Type == model.KindDB {
			j.SQL = append(j.SQL, s.Node)
		}
	}
	j.Recursive = hasCycle(j.Edges)
	return j
}

// hasCycle reports whether the calls of a subgraph contain a cycle
// (recursion, direct or mutual).
func hasCycle(edges []*model.Edge) bool {
	out := map[string][]string{}
	for _, e := range edges {
		if e.Type != model.EdgeHandles {
			out[e.Source] = append(out[e.Source], e.Target)
		}
	}
	state := map[string]int{} // 1 visiting, 2 done
	var visit func(string) bool
	visit = func(n string) bool {
		state[n] = 1
		for _, m := range out[n] {
			if state[m] == 1 || state[m] == 0 && visit(m) {
				return true
			}
		}
		state[n] = 2
		return false
	}
	keys := make([]string, 0, len(out))
	for k := range out {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if state[k] == 0 && visit(k) {
			return true
		}
	}
	return false
}

func chainOrder(e *model.Edge) int {
	if o, ok := e.Metadata["order"].(int); ok {
		return o
	}
	if o, ok := e.Metadata["order"].(float64); ok {
		return int(o)
	}
	return 0
}
