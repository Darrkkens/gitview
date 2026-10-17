package model

import "fmt"

var (
	nodeKinds = map[string]bool{KindModule: true, KindPackage: true, KindFile: true, KindStruct: true, KindInterface: true,
		KindType: true, KindFunc: true, KindMethod: true, KindClosure: true, KindRoute: true, KindDB: true,
		KindExternal: true, KindUnresolved: true}
	edgeKinds = map[string]bool{EdgeContains: true, EdgeMethod: true, EdgeImports: true, EdgeCalls: true, EdgeGo: true,
		EdgeDefer: true, EdgeHandles: true, EdgeQueries: true, EdgeImplements: true, EdgeEmbeds: true, EdgeReferences: true, EdgeDepends: true}
	confidences = map[string]bool{Confirmed: true, Likely: true, Possible: true, Unresolved: true}
)

// Validate checks the referential integrity of the dataset: unique IDs,
// edges and parents pointing to existing nodes, known kinds and confidences.
func (g *Graph) Validate() error {
	if g.Schema != Schema {
		return fmt.Errorf("schema %q, want %q", g.Schema, Schema)
	}
	nodes := make(map[string]*Node, len(g.Nodes))
	for _, n := range g.Nodes {
		if n.ID == "" {
			return fmt.Errorf("node without id: %+v", n)
		}
		if nodes[n.ID] != nil {
			return fmt.Errorf("duplicate node %s", n.ID)
		}
		if !nodeKinds[n.Type] {
			return fmt.Errorf("node %s: unknown type %q", n.ID, n.Type)
		}
		nodes[n.ID] = n
	}
	for _, n := range g.Nodes {
		if n.Parent != "" && nodes[n.Parent] == nil {
			return fmt.Errorf("node %s: parent %s does not exist", n.ID, n.Parent)
		}
		if n.Group != "" && nodes[n.Group] == nil {
			return fmt.Errorf("node %s: group %s does not exist", n.ID, n.Group)
		}
	}
	edges := make(map[string]bool, len(g.Edges))
	for _, e := range g.Edges {
		if edges[e.ID] {
			return fmt.Errorf("duplicate edge %s", e.ID)
		}
		edges[e.ID] = true
		if e.ID != EdgeID(e.Type, e.Source, e.Target) {
			return fmt.Errorf("edge %s: id does not match its endpoints", e.ID)
		}
		if nodes[e.Source] == nil || nodes[e.Target] == nil {
			return fmt.Errorf("edge %s: missing endpoint", e.ID)
		}
		if !edgeKinds[e.Type] {
			return fmt.Errorf("edge %s: unknown type %q", e.ID, e.Type)
		}
		if !confidences[e.Confidence] {
			return fmt.Errorf("edge %s: unknown confidence %q", e.ID, e.Confidence)
		}
	}
	for _, c := range g.Communities {
		if c.Level != "package" && c.Level != "symbol" {
			return fmt.Errorf("community %d: unknown level %q", c.ID, c.Level)
		}
	}
	return nil
}
