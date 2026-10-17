package study

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/athos/gitview/internal/flow"
)

// PackageInfo is a package of the module and its layer.
type PackageInfo struct {
	Path      string     `json:"path"`
	Name      string     `json:"name"`
	Dir       string     `json:"dir"`
	Layer     string     `json:"layer"`
	Basis     string     `json:"basis"`
	Evidence  []Evidence `json:"evidence"`
	Functions int        `json:"functions"`
	Files     []string   `json:"files"`
	Imports   []string   `json:"imports,omitempty"` // module packages it imports
}

// FuncRole is the layer of a function and why.
type FuncRole struct {
	ID       string     `json:"id"` // Code Flow node / symbol ID
	Name     string     `json:"name"`
	Pkg      string     `json:"pkg"`
	File     string     `json:"file"`
	Line     int        `json:"line"`
	Layer    string     `json:"layer"`
	Basis    string     `json:"basis"`
	Evidence []Evidence `json:"evidence"`
	Changed  bool       `json:"changed,omitempty"`
}

// ChainStep is one element of an architecture path.
type ChainStep struct {
	Layer string    `json:"layer"`
	ID    string    `json:"id,omitempty"` // Code Flow node
	Name  string    `json:"name"`
	File  string    `json:"file,omitempty"`
	Line  int       `json:"line,omitempty"`
	Basis string    `json:"basis"`         // of the layer
	Via   *Evidence `json:"via,omitempty"` // how the previous step reaches this one
}

// Chain is the path from a route to the data it reaches, found in the call
// graph.
type Chain struct {
	Title string      `json:"title"`
	Route string      `json:"route"`
	Steps []ChainStep `json:"steps"`
	Note  string      `json:"note,omitempty"`
}

// LayerEdge counts the calls from one layer to another.
type LayerEdge struct {
	From    string   `json:"from"`
	To      string   `json:"to"`
	Count   int      `json:"count"`
	Example Evidence `json:"example"`
}

// Architecture is the architecture map.
type Architecture struct {
	Packages   []PackageInfo `json:"packages"`
	Functions  []FuncRole    `json:"functions"`
	Chains     []Chain       `json:"chains"`
	LayerEdges []LayerEdge   `json:"layerEdges"`
	Notes      []string      `json:"notes,omitempty"`
}

// arch holds the classification while the model is built.
type arch struct {
	in     Inputs
	x      *graph
	own    map[string]*FuncRole // roles from the function's own structure
	final  map[string]*FuncRole // every module function's layer
	pkgs   map[string]*PackageInfo
	order  []string // package paths, sorted
	model_ Architecture
}

// Path segments and the layer they suggest (HEURISTIC).
var dirLayers = map[string]string{
	"handler": LayerHandler, "handlers": LayerHandler, "controller": LayerHandler, "controllers": LayerHandler,
	"api": LayerHandler, "httpapi": LayerHandler, "rest": LayerHandler, "transport": LayerHandler, "endpoint": LayerHandler, "endpoints": LayerHandler,
	"routes": LayerRoute, "router": LayerRoute, "routing": LayerRoute,
	"service": LayerService, "services": LayerService, "usecase": LayerService, "usecases": LayerService, "business": LayerService, "logic": LayerService,
	"repository": LayerRepository, "repositories": LayerRepository, "repo": LayerRepository, "repos": LayerRepository,
	"store": LayerRepository, "stores": LayerRepository, "storage": LayerRepository, "dao": LayerRepository,
	"db": LayerRepository, "database": LayerRepository, "persistence": LayerRepository, "postgres": LayerRepository,
	"model": LayerModel, "models": LayerModel, "entity": LayerModel, "entities": LayerModel, "dto": LayerModel, "dtos": LayerModel, "schema": LayerModel,
	"middleware": LayerMiddleware, "middlewares": LayerMiddleware,
	"config": LayerConfig, "configs": LayerConfig, "settings": LayerConfig,
	"worker": LayerWorker, "workers": LayerWorker, "job": LayerWorker, "jobs": LayerWorker, "cron": LayerWorker,
	"consumer": LayerWorker, "consumers": LayerWorker, "queue": LayerWorker, "scheduler": LayerWorker,
	"client": LayerExternal, "clients": LayerExternal, "integration": LayerExternal, "integrations": LayerExternal, "gateway": LayerExternal,
	"cmd": LayerEntrypoint,
}

// File-name suffixes and the layer they suggest (HEURISTIC).
var fileLayers = []struct{ suffix, layer string }{
	{"_handler.go", LayerHandler}, {"_handlers.go", LayerHandler}, {"_controller.go", LayerHandler},
	{"_routes.go", LayerRoute}, {"_router.go", LayerRoute},
	{"_service.go", LayerService}, {"_usecase.go", LayerService},
	{"_repository.go", LayerRepository}, {"_repo.go", LayerRepository}, {"_store.go", LayerRepository},
	{"_model.go", LayerModel}, {"_entity.go", LayerModel},
	{"_middleware.go", LayerMiddleware}, {"_client.go", LayerExternal}, {"_worker.go", LayerWorker}, {"_job.go", LayerWorker},
}

// Signature fragments of HTTP handlers of the common routers (signatures
// are written with package names: "w http.ResponseWriter").
var handlerSignatures = []struct{ frag, label string }{
	{" *gin.Context", "*gin.Context"},
	{" echo.Context", "echo.Context"},
	{" *fiber.Ctx", "*fiber.Ctx"},
}

// isHTTPHandlerSig reports whether a signature takes an HTTP request.
func isHTTPHandlerSig(sig string) (string, bool) {
	params := sig
	if i := strings.Index(sig, ")"); i >= 0 && strings.HasPrefix(sig, "func (") {
		params = sig[i+1:] // skip the receiver
	}
	if strings.Contains(params, " http.ResponseWriter") && strings.Contains(params, " *http.Request") {
		return "http.ResponseWriter and *http.Request", true
	}
	for _, h := range handlerSignatures {
		if strings.Contains(params, h.frag) {
			return h.label, true
		}
	}
	return "", false
}

// isHTTPClientCall reports whether an external node is an outbound HTTP
// call of net/http.
func isHTTPClientCall(n *flow.Node) bool {
	if n == nil || n.Kind != flow.KindExternal {
		return false
	}
	if n.Pkg != "net/http" {
		return false
	}
	name := strings.TrimPrefix(n.Name, "http.")
	switch name {
	case "(*Client).Do", "(*Client).Get", "(*Client).Post", "(*Client).PostForm", "(*Client).Head", "Get", "Post", "PostForm", "Head":
		return true
	}
	return false
}

func classify(in Inputs, x *graph) *arch {
	a := &arch{in: in, x: x, own: map[string]*FuncRole{}, final: map[string]*FuncRole{}, pkgs: map[string]*PackageInfo{}}
	for _, p := range in.Programs {
		for _, pi := range p.Packages() {
			a.pkgs[pi.Path] = &PackageInfo{Path: pi.Path, Name: pi.Name, Dir: pi.Dir, Files: pi.Files, Imports: pi.Imports}
			a.order = append(a.order, pi.Path)
		}
	}
	sort.Strings(a.order)
	for _, n := range x.funcs {
		if pi := a.pkgs[n.Pkg]; pi != nil {
			pi.Functions++
		}
	}
	a.ownRoles()
	a.positionRoles()
	a.packageLayers()
	a.finalRoles()
	a.chains()
	a.layerEdges()
	return a
}

func (a *arch) role(n *flow.Node, layer, basis string, ev ...Evidence) {
	a.own[n.ID] = &FuncRole{ID: n.ID, Name: n.Name, Pkg: n.Pkg, File: n.File, Line: n.Line, Layer: layer, Basis: basis, Evidence: ev, Changed: n.Changed}
}

// ownRoles classifies functions from their own structure (STATIC).
func (a *arch) ownRoles() {
	x := a.x
	for _, n := range x.funcs {
		switch {
		case hasTag(n, "middleware"):
			a.role(n, LayerMiddleware, Static, Evidence{Basis: Static, Text: "registered as middleware of a route", File: n.File, Line: n.Line, Ref: "codeflow:" + n.ID})
		case hasTag(n, "http handler"):
			ev := Evidence{Basis: Static, Text: "registered as the handler of a route", File: n.File, Line: n.Line, Ref: "codeflow:" + n.ID}
			for _, e := range x.in[n.ID] {
				if r := x.nodes[e.From]; e.Kind == flow.EdgeHandler && r != nil && r.Kind == flow.KindRoute {
					ev = Evidence{Basis: Static, Text: fmt.Sprintf("registered as the handler of %s %s (%s)", r.Method, r.Path, r.Router), File: r.File, Line: r.Line, Ref: "codeflow:" + r.ID}
					break
				}
			}
			a.role(n, LayerHandler, Static, ev)
		default:
			if what, ok := isHTTPHandlerSig(n.Signature); ok {
				a.role(n, LayerHandler, Static, Evidence{Basis: Static, Text: "its signature takes " + what, File: n.File, Line: n.Line, Ref: "codeflow:" + n.ID})
				continue
			}
			if pi := a.pkgs[n.Pkg]; pi != nil && pi.Name == "main" && n.ID == n.Pkg+".main" {
				a.role(n, LayerEntrypoint, Static, Evidence{Basis: Static, Text: "func main of package main", File: n.File, Line: n.Line, Ref: "codeflow:" + n.ID})
				continue
			}
			var db, ext *flow.Edge
			for _, e := range x.callees(n.ID) {
				t := x.nodes[e.To]
				switch {
				case t != nil && t.Kind == flow.KindDB && db == nil:
					db = e
				case isHTTPClientCall(t) && ext == nil:
					ext = e
				}
			}
			switch {
			case db != nil:
				t := x.nodes[db.To]
				a.role(n, LayerRepository, Static, Evidence{Basis: Static, Text: fmt.Sprintf("executes a database operation (%s %s)", t.DBLib, t.DBOp), File: db.File, Line: db.Line, Ref: "codeflow:" + n.ID})
			case ext != nil:
				t := x.nodes[ext.To]
				a.role(n, LayerExternal, Static, Evidence{Basis: Static, Text: "makes an outbound HTTP call (" + t.Name + ")", File: ext.File, Line: ext.Line, Ref: "codeflow:" + n.ID})
			}
		}
	}
}

// positionRoles classifies, by their place in the call graph, functions
// called from handlers or services that call data access, external APIs
// or services (STATIC: the call edges). Repeated until nothing changes.
func (a *arch) positionRoles() {
	x := a.x
	for changed := true; changed; {
		changed = false
		for _, n := range x.funcs {
			if a.own[n.ID] != nil {
				continue
			}
			var from, to *flow.Edge
			for _, e := range x.callers(n.ID) {
				if r := a.own[e.From]; r != nil && (r.Layer == LayerHandler || r.Layer == LayerService || r.Layer == LayerMiddleware) {
					from = e
					break
				}
			}
			if from == nil {
				continue
			}
			for _, e := range x.callees(n.ID) {
				if r := a.own[e.To]; r != nil && (r.Layer == LayerRepository || r.Layer == LayerExternal || r.Layer == LayerService) {
					to = e
					break
				}
			}
			if to == nil {
				continue
			}
			c, t := x.nodes[from.From], x.nodes[to.To]
			a.role(n, LayerService, Static,
				Evidence{Basis: Static, Text: "called by " + c.Name + " (" + a.own[c.ID].Layer + ")", File: from.File, Line: from.Line, Ref: "codeflow:" + n.ID},
				Evidence{Basis: Static, Text: "calls " + t.Name + " (" + a.own[t.ID].Layer + ")", File: to.File, Line: to.Line, Ref: "codeflow:" + t.ID})
			changed = true
		}
	}
}

// packageLayers gives each package the layer most of its classified
// functions have (STATIC), else the one its path suggests (HEURISTIC),
// else Unknown.
func (a *arch) packageLayers() {
	x := a.x
	routesByDir := map[string]int{}
	for _, n := range x.nodes {
		if n.Kind == flow.KindRoute {
			routesByDir[dirOf(n.File)]++
		}
	}
	for _, path := range a.order {
		pi := a.pkgs[path]
		counts := map[string]int{}
		total := 0
		for _, n := range x.byPkg[path] {
			if r := a.own[n.ID]; r != nil {
				counts[r.Layer]++
				total++
			}
		}
		if c := routesByDir[pi.Dir]; c > 0 && pi.Dir != "" {
			counts[LayerRoute] += c
			total += c
		}
		if best, n := mostFrequent(counts); n > 0 {
			pi.Layer, pi.Basis = best, Static
			pi.Evidence = append(pi.Evidence, Evidence{Basis: Static, Text: fmt.Sprintf("%s: %d of the %d elements of the package with a structural role", best, n, total)})
			continue
		}
		if l, ev, ok := pathLayer(pi.Dir, pi.Files); ok {
			pi.Layer, pi.Basis, pi.Evidence = l, Heuristic, []Evidence{ev}
			continue
		}
		pi.Layer, pi.Basis = LayerUnknown, Heuristic
		pi.Evidence = []Evidence{{Basis: Heuristic, Text: "no structural signal (routes, handlers, database or HTTP calls) and no conventional directory or file name"}}
	}
}

func mostFrequent(counts map[string]int) (string, int) {
	best, n := "", 0
	for l, c := range counts {
		if c > n || c == n && layerRank(l) < layerRank(best) {
			best, n = l, c
		}
	}
	return best, n
}

// pathLayer suggests a layer from directory names (innermost first) and
// then file names.
func pathLayer(dir string, files []string) (string, Evidence, bool) {
	segs := strings.Split(dir, "/")
	for i := len(segs) - 1; i >= 0; i-- {
		if l, ok := dirLayers[strings.ToLower(segs[i])]; ok {
			return l, Evidence{Basis: Heuristic, Text: fmt.Sprintf("directory %q is conventionally used for %s code", segs[i], l), File: dir}, true
		}
	}
	counts := map[string]int{}
	var example string
	for _, f := range files {
		if l, ok := fileLayer(f); ok {
			counts[l]++
			if example == "" {
				example = f
			}
		}
	}
	if l, n := mostFrequent(counts); n > 0 {
		return l, Evidence{Basis: Heuristic, Text: fmt.Sprintf("file names such as %s are conventionally used for %s code", path.Base(example), l), File: example}, true
	}
	return "", Evidence{}, false
}

func fileLayer(file string) (string, bool) {
	base := strings.ToLower(path.Base(file))
	for _, f := range fileLayers {
		if strings.HasSuffix(base, f.suffix) {
			return f.layer, true
		}
	}
	return "", false
}

// finalRoles gives every module function a layer: its own, else its
// package's.
func (a *arch) finalRoles() {
	for _, n := range a.x.funcs {
		if r := a.own[n.ID]; r != nil {
			a.final[n.ID] = r
			continue
		}
		r := &FuncRole{ID: n.ID, Name: n.Name, Pkg: n.Pkg, File: n.File, Line: n.Line, Layer: LayerUnknown, Basis: Heuristic, Changed: n.Changed}
		if pi := a.pkgs[n.Pkg]; pi != nil {
			r.Layer, r.Basis = pi.Layer, pi.Basis
			r.Evidence = []Evidence{{Basis: pi.Basis, Text: "declared in package " + pi.Name + ", classified as " + pi.Layer, File: pi.Dir}}
		}
		if r.Basis == Static {
			// A package's layer says nothing certain about each function.
			r.Basis = Heuristic
			r.Evidence[0].Basis = Heuristic
		}
		a.final[n.ID] = r
	}
}

// layerOf returns the layer of a module function (Unknown when not known).
func (a *arch) layerOf(id string) (string, string) {
	if r := a.final[id]; r != nil {
		return r.Layer, r.Basis
	}
	n := a.x.nodes[id]
	switch {
	case n == nil:
		return LayerUnknown, Heuristic
	case n.Kind == flow.KindRoute:
		return LayerRoute, Static
	case n.Kind == flow.KindDB:
		return LayerDatabase, Static
	case isHTTPClientCall(n):
		return LayerExternal, Static
	}
	return LayerUnknown, Heuristic
}

// chains follows each route to the database or external API it reaches.
func (a *arch) chains() {
	x := a.x
	var routes []*flow.Node
	for _, n := range x.nodes {
		if n.Kind == flow.KindRoute {
			routes = append(routes, n)
		}
	}
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Path != routes[j].Path {
			return routes[i].Path < routes[j].Path
		}
		return routes[i].Method < routes[j].Method
	})
	for _, r := range routes {
		title := strings.TrimSpace(r.Method + " " + r.Path)
		ch := Chain{Title: title, Route: r.ID}
		ch.Steps = append(ch.Steps, ChainStep{Layer: LayerRoute, ID: r.ID, Name: title, File: r.File, Line: r.Line, Basis: Static,
			Via: &Evidence{Basis: Static, Text: "registered with " + r.Router, File: r.File, Line: r.Line, Ref: "codeflow:" + r.ID}})
		var handler *flow.Node
		for _, e := range x.out[r.ID] {
			if e.Kind != flow.EdgeHandler {
				continue
			}
			h := x.nodes[e.To]
			if h == nil {
				continue
			}
			l, b := a.layerOf(h.ID)
			ch.Steps = append(ch.Steps, ChainStep{Layer: l, ID: h.ID, Name: h.Name, File: h.File, Line: h.Line, Basis: b,
				Via: &Evidence{Basis: Static, Text: "handler chain of the route", File: e.File, Line: e.Line, Ref: "codeflow:" + h.ID}})
			if !hasTag(h, "middleware") {
				handler = h
			}
		}
		if handler == nil {
			ch.Note = "No handler found for this route."
			a.model_.Chains = append(a.model_.Chains, ch)
			continue
		}
		path, end := a.shortestPath(handler.ID, func(n *flow.Node) bool { return n.Kind == flow.KindDB })
		if end == nil {
			path, end = a.shortestPath(handler.ID, isHTTPClientCall)
		}
		if end == nil {
			ch.Note = "No database operation or outbound HTTP call is reachable from the handler in the call graph."
		}
		for _, e := range path {
			t := x.nodes[e.To]
			l, b := a.layerOf(t.ID)
			name := t.Name
			if t.Kind == flow.KindDB {
				name = t.DBOp
				if t.SQL != "" {
					name += ": " + oneLine(t.SQL, 80)
				}
			}
			ch.Steps = append(ch.Steps, ChainStep{Layer: l, ID: t.ID, Name: name, File: t.File, Line: t.Line, Basis: b,
				Via: &Evidence{Basis: Static, Text: "call (" + e.Confidence + ")", File: e.File, Line: e.Line, Ref: "codeflow:" + e.From}})
		}
		a.model_.Chains = append(a.model_.Chains, ch)
	}
}

// shortestPath finds, breadth first over call edges between module
// functions, the nearest node accepted by target. Ties are broken by
// call-site order, so the result is reproducible.
func (a *arch) shortestPath(from string, target func(*flow.Node) bool) ([]*flow.Edge, *flow.Node) {
	x := a.x
	prev := map[string]*flow.Edge{from: nil}
	queue := []string{from}
	for depth := 0; len(queue) > 0 && depth < 12; depth++ {
		var next []string
		for _, id := range queue {
			for _, e := range x.callees(id) {
				if _, seen := prev[e.To]; seen {
					continue
				}
				t := x.nodes[e.To]
				if t == nil {
					continue
				}
				prev[e.To] = e
				if target(t) {
					var path []*flow.Edge
					for cur := e; cur != nil; cur = prev[cur.From] {
						path = append([]*flow.Edge{cur}, path...)
					}
					return path, t
				}
				if x.isModuleFunc(t.ID) {
					next = append(next, t.ID)
				}
			}
		}
		queue = next
	}
	return nil, nil
}

// layerEdges counts the calls between layers of module functions.
func (a *arch) layerEdges() {
	type key struct{ from, to string }
	counts := map[key]int{}
	examples := map[key]Evidence{}
	x := a.x
	for _, n := range x.funcs {
		lf, _ := a.layerOf(n.ID)
		for _, e := range x.callees(n.ID) {
			if !x.isModuleFunc(e.To) && x.nodes[e.To] != nil && x.nodes[e.To].Kind != flow.KindDB && !isHTTPClientCall(x.nodes[e.To]) {
				continue
			}
			lt, _ := a.layerOf(e.To)
			if lf == lt || lf == LayerUnknown || lt == LayerUnknown {
				continue
			}
			k := key{lf, lt}
			if counts[k] == 0 {
				examples[k] = Evidence{Basis: Static, Text: n.Name + " calls " + x.nodes[e.To].Name, File: e.File, Line: e.Line, Ref: "codeflow:" + n.ID}
			}
			counts[k]++
		}
	}
	for k, c := range counts {
		a.model_.LayerEdges = append(a.model_.LayerEdges, LayerEdge{From: k.from, To: k.to, Count: c, Example: examples[k]})
	}
	sort.Slice(a.model_.LayerEdges, func(i, j int) bool {
		x, y := a.model_.LayerEdges[i], a.model_.LayerEdges[j]
		if layerRank(x.From) != layerRank(y.From) {
			return layerRank(x.From) < layerRank(y.From)
		}
		return layerRank(x.To) < layerRank(y.To)
	})
}

// model returns the architecture: every package, and the functions whose
// layer comes from their own structure or that the diff changed.
func (a *arch) model() Architecture {
	out := a.model_
	for _, p := range a.order {
		out.Packages = append(out.Packages, *a.pkgs[p])
	}
	for _, n := range a.x.funcs {
		r := a.final[n.ID]
		if a.own[n.ID] != nil || n.Changed {
			out.Functions = append(out.Functions, *r)
		}
	}
	return out
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		return s[:n-1] + "…"
	}
	return s
}
