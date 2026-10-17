// Package study builds the Study Mode of GitView: a learning layer over the
// results the existing analyzers already produced (Code Flow call graph,
// routes, CFGs, Data Flow, SQL, coverage, traces, profiles, git diff).
//
// Nothing here interprets code: every item is derived deterministically
// from those results and carries the evidence it rests on, with a badge
// saying where it comes from (STATIC, RUNTIME, COVERAGE, SQL, GIT,
// HEURISTIC). What cannot be determined is reported as Unknown. The same
// inputs always produce the same model (no clock, no randomness), whose
// fingerprint identifies it.
package study

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path"
	"sort"
	"strings"

	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/inspect"
	"github.com/athos/gitview/internal/inspect/perf"
	"github.com/athos/gitview/internal/inspect/pgexplain"
	"github.com/athos/gitview/internal/inspect/xtrace"
)

// Sources of a fact (the badges shown next to it).
const (
	Static    = "STATIC"    // static analysis: SSA, call graph, CFG, types
	Runtime   = "RUNTIME"   // observed in a trace, a profile or a test run
	Coverage  = "COVERAGE"  // observed by a coverage profile
	SQLBasis  = "SQL"       // read from SQL text (queries, migrations)
	Git       = "GIT"       // from the working-tree diff
	Heuristic = "HEURISTIC" // from names, paths or extensions
)

// Evidence is what a fact rests on. Ref is a navigation target the report
// understands: "codeflow:<node>", "inspect:<symbol>:<tab>",
// "diff:<file>:<line>", "source:<symbol>", "sql:<op>".
type Evidence struct {
	Basis string `json:"basis"`
	Text  string `json:"text"`
	File  string `json:"file,omitempty"`
	Line  int    `json:"line,omitempty"`
	Ref   string `json:"ref,omitempty"`
}

// Layer names, in the order the report shows them.
const (
	LayerRoute      = "HTTP Route"
	LayerMiddleware = "Middleware"
	LayerHandler    = "Handler"
	LayerService    = "Service"
	LayerRepository = "Repository"
	LayerDatabase   = "Database"
	LayerExternal   = "External API"
	LayerModel      = "Model"
	LayerEntrypoint = "Entrypoint"
	LayerWorker     = "Worker"
	LayerConfig     = "Config"
	LayerTests      = "Tests"
	LayerFrontend   = "Frontend"
	LayerDocs       = "Docs"
	LayerDeps       = "Dependencies"
	LayerUnknown    = "Unknown"
)

// Layers is the display order of the layers.
var Layers = []string{LayerRoute, LayerMiddleware, LayerHandler, LayerService, LayerRepository, LayerDatabase, LayerExternal,
	LayerModel, LayerEntrypoint, LayerWorker, LayerConfig, LayerTests, LayerFrontend, LayerDocs, LayerDeps, LayerUnknown}

func layerRank(l string) int {
	for i, x := range Layers {
		if x == l {
			return i
		}
	}
	return len(Layers)
}

// FileChange is a file of the working-tree diff.
type FileChange struct {
	Path    string
	Status  string // A, M, D, R, ?
	Added   int
	Deleted int
}

// TestResult is the outcome of a test in the last test run.
type TestResult struct {
	Package string
	Name    string
	Status  string // passed | failed | skipped | ...
}

// Inputs are the results of the existing analyzers.
type Inputs struct {
	Root     string
	Programs []*flow.Program
	Graph    *flow.Graph // the Code Flow index of the report
	Changed  map[string]map[int]bool
	Files    []FileChange
	Bundles  []*inspect.Bundle // Deep Inspector results (CFG, Data Flow, SQL)
	Trace    *xtrace.Timeline
	CPU      *perf.Report
	Mem      *perf.Report
	Benches  []*perf.BenchRun
	Escape   *perf.EscapeReport
	Plans    []*pgexplain.Plan
	Tests    []TestResult
	// Coverage names the coverage profile the CFGs were overlaid with ("" when none).
	Coverage string
}

// Summary describes what the model was built from.
type Summary struct {
	Modules          []string `json:"modules"`
	Packages         int      `json:"packages"`
	Functions        int      `json:"functions"`
	Routes           int      `json:"routes"`
	ChangedFiles     int      `json:"changedFiles"`
	ChangedFunctions int      `json:"changedFunctions"`
	TestFunctions    int      `json:"testFunctions"`
	HasCoverage      bool     `json:"hasCoverage"`
	CoverageProfile  string   `json:"coverageProfile,omitempty"`
	HasTrace         bool     `json:"hasTrace"`
	HasProfile       bool     `json:"hasProfile"`
	HasTestRun       bool     `json:"hasTestRun"`
}

// Model is the Study Mode of a report.
type Model struct {
	Version      int             `json:"version"`
	Fingerprint  string          `json:"fingerprint"`
	Summary      Summary         `json:"summary"`
	Layers       []string        `json:"layers"`
	Architecture Architecture    `json:"architecture"`
	Changes      []ChangeGroup   `json:"changes"`
	ReadingOrder []Step          `json:"readingOrder"`
	Tests        []TestFunc      `json:"tests,omitempty"`
	DataJourneys []DataJourney   `json:"dataJourneys,omitempty"`
	Decisions    []FuncDecisions `json:"decisions,omitempty"`
	TestMatrix   []MatrixFunc    `json:"testMatrix,omitempty"`
	Predict      Predict         `json:"predict"`
	SQL          SQLStudy        `json:"sql"`
	Performance  PerfStudy       `json:"performance"`
	Exercises    []Exercise      `json:"exercises,omitempty"`
	Explain      []ExplainFunc   `json:"explain,omitempty"`
	Notes        []string        `json:"notes,omitempty"`
}

// graph indexes the Code Flow index for the transformations.
type graph struct {
	g      *flow.Graph
	nodes  map[string]*flow.Node
	out    map[string][]*flow.Edge
	in     map[string][]*flow.Edge
	funcs  []*flow.Node // module functions (not closures), sorted by file and line
	byPkg  map[string][]*flow.Node
	byFile map[string][]*flow.Node
}

func newGraph(g *flow.Graph) *graph {
	x := &graph{g: g, nodes: map[string]*flow.Node{}, out: map[string][]*flow.Edge{}, in: map[string][]*flow.Edge{},
		byPkg: map[string][]*flow.Node{}, byFile: map[string][]*flow.Node{}}
	if g == nil {
		return x
	}
	for _, n := range g.Nodes {
		x.nodes[n.ID] = n
		if n.Kind == flow.KindFunc || n.Kind == flow.KindClosure {
			x.byFile[n.File] = append(x.byFile[n.File], n)
			if n.Kind == flow.KindFunc {
				x.funcs = append(x.funcs, n)
				x.byPkg[n.Pkg] = append(x.byPkg[n.Pkg], n)
			}
		}
	}
	seen := map[string]bool{}
	for _, e := range g.Edges {
		k := e.From + "\x00" + e.To + "\x00" + e.Kind + "\x00" + e.File + "\x00" + itoa(e.Line)
		if seen[k] {
			continue
		}
		seen[k] = true
		x.out[e.From] = append(x.out[e.From], e)
		x.in[e.To] = append(x.in[e.To], e)
	}
	for _, m := range []map[string][]*flow.Edge{x.out, x.in} {
		for _, es := range m {
			sort.SliceStable(es, func(i, j int) bool { return edgeLess(es[i], es[j]) })
		}
	}
	sortNodes(x.funcs)
	for _, l := range x.byPkg {
		sortNodes(l)
	}
	for _, l := range x.byFile {
		sortNodes(l)
	}
	return x
}

func edgeLess(a, b *flow.Edge) bool {
	if a.Order != b.Order {
		return a.Order < b.Order
	}
	if a.File != b.File {
		return a.File < b.File
	}
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	if a.To != b.To {
		return a.To < b.To
	}
	return a.From < b.From
}

func sortNodes(ns []*flow.Node) {
	sort.SliceStable(ns, func(i, j int) bool {
		if ns[i].File != ns[j].File {
			return ns[i].File < ns[j].File
		}
		if ns[i].Line != ns[j].Line {
			return ns[i].Line < ns[j].Line
		}
		return ns[i].ID < ns[j].ID
	})
}

// nodeList returns the nodes of the index (none without a call graph).
func (x *graph) nodeList() []*flow.Node {
	if x.g == nil {
		return nil
	}
	return x.g.Nodes
}

// isModuleFunc reports whether a node is a function of the analysed module.
func (x *graph) isModuleFunc(id string) bool {
	n := x.nodes[id]
	return n != nil && (n.Kind == flow.KindFunc || n.Kind == flow.KindClosure)
}

// callees are the module functions (and db/external nodes) called by id,
// by call edges, in call-site order.
func (x *graph) callees(id string) []*flow.Edge {
	var out []*flow.Edge
	for _, e := range x.out[id] {
		if e.Kind != flow.EdgeHandler {
			out = append(out, e)
		}
	}
	return out
}

func (x *graph) callers(id string) []*flow.Edge {
	var out []*flow.Edge
	for _, e := range x.in[id] {
		if e.Kind != flow.EdgeHandler {
			out = append(out, e)
		}
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func loc(file string, line int) string {
	if line > 0 {
		return file + ":" + itoa(line)
	}
	return file
}

func dirOf(file string) string { return path.Dir(file) }

func hasTag(n *flow.Node, tag string) bool {
	for _, t := range n.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// Build derives the Study model from the analyzers' results.
func Build(in Inputs) *Model {
	m := &Model{Version: 1, Layers: Layers}
	x := newGraph(in.Graph)
	m.Summary = summary(in, x)
	arch := classify(in, x)
	m.Architecture = arch.model()
	m.Tests = scanTests(in, x)
	m.Summary.TestFunctions = len(m.Tests)
	m.Changes = changeJourney(in, x, arch)
	m.ReadingOrder = readingOrder(in, x, arch, m.Tests)
	var order []string
	for _, s := range m.ReadingOrder {
		if s.Kind == "function" {
			order = append(order, s.ID)
		}
	}
	m.DataJourneys = dataJourneys(in.Bundles, order)
	m.Decisions = decisions(in.Bundles, order)
	m.TestMatrix = testMatrix(in.Bundles, order, m.Tests, x)
	m.Predict = predict(x, arch, m.ReadingOrder, in.Trace)
	m.SQL = sqlStudy(in, x, order)
	m.Performance = perfStudy(in, x, order)
	m.Exercises = append(quizzes(m, x, in, order), challenges(m, x, in, order)...)
	m.Explain = explain(m, x, arch, order)
	if in.Graph == nil {
		m.Notes = append(m.Notes, "The Go call graph is not available (no Go module, or the analysis failed): architecture, reading order and journeys are empty.")
	}
	m.Fingerprint = fingerprint(m)
	return m
}

func summary(in Inputs, x *graph) Summary {
	s := Summary{ChangedFiles: len(in.Files), Functions: len(x.funcs), HasTrace: in.Trace != nil,
		HasProfile: in.CPU != nil || in.Mem != nil || len(in.Benches) > 0, HasTestRun: len(in.Tests) > 0,
		HasCoverage: in.Coverage != "", CoverageProfile: in.Coverage}
	for _, p := range in.Programs {
		s.Modules = append(s.Modules, p.Module)
		s.Packages += len(p.Packages())
	}
	sort.Strings(s.Modules)
	for _, n := range x.nodes {
		switch {
		case n.Kind == flow.KindRoute:
			s.Routes++
		case n.Kind == flow.KindFunc && n.Changed:
			s.ChangedFunctions++
		}
	}
	return s
}

// fingerprint hashes the model without its fingerprint: equal inputs give
// equal fingerprints, which also key the reader's progress.
func fingerprint(m *Model) string {
	c := *m
	c.Fingerprint = ""
	data, _ := json.Marshal(c)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}

// short returns the last element of a package path.
func short(pkg string) string {
	if i := strings.LastIndexByte(pkg, '/'); i >= 0 {
		return pkg[i+1:]
	}
	return pkg
}
