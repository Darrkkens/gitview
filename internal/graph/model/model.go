// Package model is the versioned dataset of the Repository Graph: nodes and
// edges describing a Go repository, each edge carrying the evidence that
// produced it. Nothing here analyses code; the analyzer fills the model and
// the exporters render it.
//
// Identity: a node ID is derived only from the symbol it represents (kind
// prefix + import path + receiver + name, or repository-relative path), never
// from its position in a layout, so IDs are stable across runs as long as the
// code declaring them does not change. Go has no overloading, so package,
// receiver and name identify a function; closures carry the SSA suffix
// (.func1, .func1.2) of their parent.
package model

import (
	"sort"

	"github.com/athos/gitview/internal/evidence"
)

// Schema identifies the dataset format. Readers must check it: fields may be
// added within a version, never renamed or given another meaning.
const Schema = "gitview.graph/v1"

// Node kinds.
const (
	KindModule     = "module"
	KindPackage    = "package"
	KindFile       = "file"
	KindStruct     = "struct"
	KindInterface  = "interface"
	KindType       = "type" // named type that is neither a struct nor an interface
	KindFunc       = "func"
	KindMethod     = "method"
	KindClosure    = "closure"
	KindRoute      = "route"
	KindDB         = "db"         // database operation at a call site
	KindExternal   = "external"   // function declared outside the analysed modules
	KindUnresolved = "unresolved" // dynamic call or handler whose target is unknown
)

// Edge kinds.
const (
	EdgeContains   = "contains"   // structural: module → package → file → declaration, function → closure
	EdgeMethod     = "method"     // named type → its method
	EdgeImports    = "imports"    // package → imported package
	EdgeCalls      = "calls"      // function → callee
	EdgeGo         = "go"         // function → callee started as a goroutine
	EdgeDefer      = "defer"      // function → deferred callee
	EdgeHandles    = "handles"    // route → handler or middleware
	EdgeQueries    = "queries"    // function → database operation
	EdgeImplements = "implements" // concrete type → module interface it satisfies
	EdgeEmbeds     = "embeds"     // struct or interface → embedded type
	EdgeReferences = "references" // declaration → module type used in a field or signature
	// EdgeDepends only appears in the files level of an export: a file → file
	// edge summarising the relations between their declarations (derived).
	EdgeDepends = "depends"
)

// Confidence of an edge. Code Flow's levels are kept as they are for calls;
// structural and type relations read from go/types are confirmed.
const (
	Confirmed  = "confirmed"
	Likely     = "likely"
	Possible   = "possible"
	Unresolved = "unresolved"
)

type Node struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Name      string `json:"name"`                // display name: F, (*T).M, T, file.go, import path
	Qualified string `json:"qualified,omitempty"` // pkg.(*T).M, pkg.T
	Package   string `json:"package,omitempty"`   // import path of the declaring package
	Module    string `json:"module,omitempty"`
	File      string `json:"file,omitempty"` // repository-relative
	Line      int    `json:"line,omitempty"`
	EndLine   int    `json:"endLine,omitempty"`
	Signature string `json:"signature,omitempty"`
	External  bool   `json:"external,omitempty"` // declared outside the analysed modules

	// Parent is the node that structurally contains this one (the package of
	// a file, the file of a declaration, the parent function of a closure).
	Parent string `json:"parent,omitempty"`
	// Group is the package node the node is drawn with; for packages it is
	// the node itself.
	Group string `json:"group,omitempty"`

	Community int     `json:"community"` // -1 when not clustered
	Degree    int     `json:"degree"`
	InDegree  int     `json:"inDegree"`
	OutDegree int     `json:"outDegree"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`

	Metadata map[string]any `json:"metadata,omitempty"`
	// Annotations are attached by later analyses (for example security
	// findings); the graph builder never writes them.
	Annotations []Annotation `json:"annotations,omitempty"`
}

type Edge struct {
	ID         string             `json:"id"`
	Source     string             `json:"source"`
	Target     string             `json:"target"`
	Type       string             `json:"type"`
	Confidence string             `json:"confidence"`
	Evidence   *evidence.Evidence `json:"evidence,omitempty"`
	Note       string             `json:"note,omitempty"`
	Weight     int                `json:"weight,omitempty"` // number of call sites or references merged
	Metadata   map[string]any     `json:"metadata,omitempty"`

	Annotations []Annotation `json:"annotations,omitempty"`
}

// Annotation attaches the result of another analysis to a node or an edge,
// with the evidence that supports it. Reserved for future modules (for
// example an audit that marks a path from an HTTP input to a query).
type Annotation struct {
	Kind     string              `json:"kind"`
	Ref      string              `json:"ref,omitempty"` // ID of what it comes from, e.g. a CodeAudit finding
	Severity string              `json:"severity,omitempty"`
	Title    string              `json:"title"`
	Details  string              `json:"details,omitempty"`
	Evidence []evidence.Evidence `json:"evidence,omitempty"`
}

// Community is a group found by the community detection.
type Community struct {
	ID      int    `json:"id"`
	Level   string `json:"level"` // package | symbol
	Label   string `json:"label"` // name of its most connected member
	Size    int    `json:"size"`
	Members int    `json:"members"` // nodes drawn with it (symbols for package communities)
}

// Stats are the counts and timings of a build.
type Stats struct {
	Modules          int            `json:"modules"`
	Packages         int            `json:"packages"`
	ExternalPackages int            `json:"externalPackages"`
	Files            int            `json:"files"`
	Functions        int            `json:"functions"` // functions, methods and closures
	Types            int            `json:"types"`
	Nodes            int            `json:"nodes"`
	Edges            int            `json:"edges"`
	Communities      int            `json:"communities"`
	Routes           int            `json:"routes"`
	Modularity       float64        `json:"modularity"`
	NodesByType      map[string]int `json:"nodesByType"`
	EdgesByType      map[string]int `json:"edgesByType"`
	EdgesByConf      map[string]int `json:"edgesByConfidence"`
	AnalysisMS       int64          `json:"analysisMs"`
	BuildMS          int64          `json:"buildMs"`
	LayoutMS         int64          `json:"layoutMs"`
}

// Graph is the dataset.
type Graph struct {
	Schema      string      `json:"schema"`
	Repository  string      `json:"repository"`
	Root        string      `json:"-"` // absolute path; never exported
	Modules     []string    `json:"modules"`
	Version     string      `json:"version,omitempty"` // fingerprint of the analysed sources
	Generated   string      `json:"generated"`
	Nodes       []*Node     `json:"nodes"`
	Edges       []*Edge     `json:"edges"`
	Communities []Community `json:"communities"`
	// HTTP is the HTTP catalog (routes and contracts); nil when the
	// analysis found no route or it was not requested.
	HTTP  *HTTP `json:"http,omitempty"`
	Stats Stats `json:"stats"`
	// Completeness lists what limited the analysis (load errors, packages
	// that did not type-check, unresolved calls).
	Completeness evidence.Completeness `json:"completeness"`
	Method       []string              `json:"method"` // how each relation and the communities were computed
}

// Sort orders nodes and edges by ID so that two builds of the same code
// produce byte-identical datasets.
func (g *Graph) Sort() {
	sort.Slice(g.Nodes, func(i, j int) bool { return g.Nodes[i].ID < g.Nodes[j].ID })
	sort.Slice(g.Edges, func(i, j int) bool { return g.Edges[i].ID < g.Edges[j].ID })
}

// Index maps node IDs to nodes.
func (g *Graph) Index() map[string]*Node {
	m := make(map[string]*Node, len(g.Nodes))
	for _, n := range g.Nodes {
		m[n.ID] = n
	}
	return m
}

// EdgeID is the identity of an edge: endpoints and kind. Several call sites
// between the same functions merge into one edge with a Weight.
func EdgeID(typ, source, target string) string {
	return typ + "|" + source + "|" + target
}
