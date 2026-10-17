// Package flow derives call graphs ("code flows") from Go source with
// go/packages, go/types and SSA. Every edge comes from a call site found in
// the code; dynamic calls are resolved conservatively and labelled with a
// confidence level instead of being guessed from names.
//
// The analysis is static: an edge means a call that may happen on some
// execution path, not one observed at runtime.
package flow

// Node kinds.
const (
	KindFunc       = "func"       // function or method in the analysed module
	KindClosure    = "closure"    // anonymous function
	KindRoute      = "route"      // HTTP route registration
	KindExternal   = "external"   // function outside the module (stdlib or dependency)
	KindDB         = "db"         // database operation (pgx, database/sql)
	KindUnresolved = "unresolved" // dynamic call whose target could not be determined
)

// Edge kinds.
const (
	EdgeCall    = "call"
	EdgeGo      = "go"      // started as a goroutine
	EdgeDefer   = "defer"   // deferred call
	EdgeHandler = "handler" // route → handler or middleware
)

// Confidence of an edge.
const (
	Confirmed  = "confirmed"  // static call, or a function value known at the call site
	Likely     = "likely"     // dynamic call with a single possible target in the module
	Possible   = "possible"   // one of several possible targets (interface dispatch)
	Unresolved = "unresolved" // dynamic call with no target found
)

type Node struct {
	ID        string   `json:"id"`
	Kind      string   `json:"kind"`
	Name      string   `json:"name"`           // short display name, e.g. (*Service).Create
	Pkg       string   `json:"pkg,omitempty"`  // package path
	Signature string   `json:"sig,omitempty"`  // full signature
	File      string   `json:"file,omitempty"` // path relative to the repository root
	Line      int      `json:"line,omitempty"` // definition line
	EndLine   int      `json:"end,omitempty"`  // last line of the body
	Snippet   string   `json:"code,omitempty"` // source of the definition (bounded)
	Changed   bool     `json:"changed,omitempty"`
	Recursive bool     `json:"recursive,omitempty"`
	Truncated bool     `json:"truncated,omitempty"` // callees not explored (depth or size limit)
	Tags      []string `json:"tags,omitempty"`      // evidence-based roles, e.g. "http handler"

	// Database operations.
	DBOp       string `json:"dbOp,omitempty"`  // Query, Exec, Begin...
	DBLib      string `json:"dbLib,omitempty"` // pgx, database/sql
	SQL        string `json:"sql,omitempty"`
	SQLDynamic bool   `json:"sqlDynamic,omitempty"`

	// Routes.
	Method  string `json:"method,omitempty"`
	Path    string `json:"path,omitempty"`
	Router  string `json:"router,omitempty"` // net/http, gin, chi, echo
	Partial bool   `json:"partial,omitempty"`

	Note    string `json:"note,omitempty"`
	Explain string `json:"explain,omitempty"`
}

type Edge struct {
	From       string `json:"from"`
	To         string `json:"to"`
	Kind       string `json:"kind"`
	Confidence string `json:"conf"`
	File       string `json:"file,omitempty"` // call site
	Line       int    `json:"line,omitempty"`
	Order      int    `json:"order,omitempty"` // position for route handler chains
	Note       string `json:"note,omitempty"`
}

type Root struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind"` // changed | route | selected
}

// Graph is the result of a flow query.
type Graph struct {
	Module    string   `json:"module"`
	Roots     []Root   `json:"roots"`
	Nodes     []*Node  `json:"nodes"`
	Edges     []*Edge  `json:"edges"`
	Notes     []string `json:"notes,omitempty"`
	Truncated bool     `json:"truncated,omitempty"`
	MaxDepth  int      `json:"maxDepth"`
}

// Merge appends another graph (e.g. from a second module) into g.
func (g *Graph) Merge(o *Graph) {
	if o == nil {
		return
	}
	seen := map[string]bool{}
	for _, n := range g.Nodes {
		seen[n.ID] = true
	}
	for _, n := range o.Nodes {
		if !seen[n.ID] {
			g.Nodes = append(g.Nodes, n)
		}
	}
	g.Edges = append(g.Edges, o.Edges...)
	g.Roots = append(g.Roots, o.Roots...)
	g.Notes = append(g.Notes, o.Notes...)
	g.Truncated = g.Truncated || o.Truncated
	if g.Module == "" {
		g.Module = o.Module
	} else if o.Module != "" && o.Module != g.Module {
		g.Module += ", " + o.Module
	}
	g.MaxDepth = max(g.MaxDepth, o.MaxDepth)
}

// Node returns the node with the given id.
func (g *Graph) Node(id string) *Node {
	for _, n := range g.Nodes {
		if n.ID == id {
			return n
		}
	}
	return nil
}

// Out returns edges leaving id, in discovery order.
func (g *Graph) Out(id string) []*Edge {
	var out []*Edge
	for _, e := range g.Edges {
		if e.From == id {
			out = append(out, e)
		}
	}
	return out
}

// In returns edges entering id.
func (g *Graph) In(id string) []*Edge {
	var out []*Edge
	for _, e := range g.Edges {
		if e.To == id {
			out = append(out, e)
		}
	}
	return out
}
