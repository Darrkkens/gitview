package flow

import (
	"fmt"
	"sort"
	"strings"

	"golang.org/x/tools/go/ssa"
)

// Options bound the size of a flow graph.
type Options struct {
	Depth    int  // levels of calls followed from each root (default 6)
	MaxNodes int  // hard cap on nodes (default 400)
	External bool // include calls leaving the module as leaf nodes
	Routes   bool // walk callers upwards to find HTTP routes reaching the roots
	// Changed holds, per repository-relative file, the new-side line numbers
	// touched by the working tree diff.
	Changed map[string]map[int]bool
}

func (o Options) withDefaults() Options {
	if o.Depth <= 0 {
		o.Depth = 6
	}
	if o.MaxNodes <= 0 {
		o.MaxNodes = 400
	}
	return o
}

const maxSnippetLines = 80

// Func describes a module function, for pickers and selectors.
type Func struct {
	ID      string
	Name    string
	File    string
	Line    int
	EndLine int
	Changed bool
}

// Functions lists module functions, optionally only those declared in file.
func (p *Program) Functions(file string) []Func {
	var out []Func
	for _, fn := range p.funcs {
		f, line, end := p.position(fn)
		if file != "" && f != file {
			continue
		}
		out = append(out, Func{ID: origin(fn).String(), Name: shortName(fn), File: f, Line: line, EndLine: end})
	}
	return out
}

// ChangedFunctions returns the top-level functions and methods whose bodies
// contain a changed line. Closures are reported through their parents.
func (p *Program) ChangedFunctions(changed map[string]map[int]bool) []Func {
	var out []Func
	for _, fn := range p.funcs {
		if fn.Parent() != nil {
			continue
		}
		file, line, end := p.position(fn)
		lines := changed[file]
		for l := range lines {
			if l >= line && l <= end {
				out = append(out, Func{ID: fn.String(), Name: shortName(fn), File: file, Line: line, EndLine: end, Changed: true})
				break
			}
		}
	}
	return out
}

// Lookup finds functions by id, short name (pkg.F, pkg.(*T).M, (*T).M, F)
// or file:line.
func (p *Program) Lookup(q string) []*ssa.Function {
	var out []*ssa.Function
	if file, line, ok := parseFileLine(q); ok {
		var best *ssa.Function
		for _, fn := range p.funcs {
			f, l, e := p.position(fn)
			if (f == file || strings.HasSuffix(f, "/"+file)) && line >= l && line <= e {
				if best == nil || l >= mustLine(p, best) {
					best = fn // innermost
				}
			}
		}
		if best != nil {
			out = append(out, best)
		}
		return out
	}
	for _, fn := range p.funcs {
		name := shortName(fn)
		short := name
		if i := strings.IndexByte(name, '.'); i >= 0 {
			short = name[i+1:]
		}
		if fn.String() == q || name == q || short == q || strings.TrimPrefix(short, "(*") == strings.TrimPrefix(q, "(*") && strings.Contains(q, ".") {
			out = append(out, fn)
		}
	}
	return out
}

func mustLine(p *Program, fn *ssa.Function) int { _, l, _ := p.position(fn); return l }

func parseFileLine(q string) (string, int, bool) {
	i := strings.LastIndexByte(q, ':')
	if i <= 0 {
		return "", 0, false
	}
	var line int
	if _, err := fmt.Sscanf(q[i+1:], "%d", &line); err != nil || line <= 0 {
		return "", 0, false
	}
	return q[:i], line, true
}

// RouteCount reports how many route registrations were found.
func (p *Program) RouteCount() int { return len(p.routes) }

// builder accumulates a Graph.
type builder struct {
	p     *Program
	opt   Options
	g     *Graph
	nodes map[string]*Node
	edges map[string]bool
	fnOf  map[string]*ssa.Function
	full  bool

	snippetLines int
	noRouteRoots bool // routes found while walking callers are not roots
}

func (p *Program) newBuilder(opt Options) *builder {
	return &builder{p: p, opt: opt.withDefaults(), g: &Graph{Module: p.Module}, nodes: map[string]*Node{}, edges: map[string]bool{}, fnOf: map[string]*ssa.Function{}}
}

func (b *builder) add(n *Node) *Node {
	if old, ok := b.nodes[n.ID]; ok {
		return old
	}
	if len(b.nodes) >= b.opt.MaxNodes {
		b.full, b.g.Truncated = true, true
		return nil
	}
	b.nodes[n.ID] = n
	b.g.Nodes = append(b.g.Nodes, n)
	return n
}

func (b *builder) edge(e *Edge) {
	key := e.From + "\x00" + e.To + "\x00" + e.Kind + "\x00" + fmt.Sprint(e.Line, "#", e.Order)
	if b.edges[key] {
		return
	}
	b.edges[key] = true
	b.g.Edges = append(b.g.Edges, e)
}

func (b *builder) funcNode(fn *ssa.Function) *Node {
	fn = origin(fn)
	id := fn.String()
	if n, ok := b.nodes[id]; ok {
		return n
	}
	if !b.p.inModule(fn) {
		return b.externalNode(shortName(fn), pkgPath(fn), signature(fn))
	}
	file, line, end := b.p.position(fn)
	n := &Node{ID: id, Kind: KindFunc, Name: shortName(fn), Signature: signature(fn), File: file, Line: line, EndLine: end}
	if fn.Pkg != nil {
		n.Pkg = fn.Pkg.Pkg.Path()
	}
	if fn.Parent() != nil {
		n.Kind = KindClosure
	}
	limit := maxSnippetLines
	if b.snippetLines > 0 {
		limit = b.snippetLines
	}
	n.Snippet = b.p.source(file, line, min(end, line+limit-1))
	if end-line+1 > limit {
		n.Snippet += "\n…"
	}
	for l := range b.opt.Changed[file] {
		if l >= line && l <= end {
			n.Changed = true
			break
		}
	}
	if rs := b.p.handler[fn]; len(rs) > 0 {
		n.Tags = append(n.Tags, "http handler")
	}
	if n = b.add(n); n != nil {
		b.fnOf[id] = fn
	}
	return n
}

func pkgPath(fn *ssa.Function) string {
	if fn.Pkg != nil {
		return fn.Pkg.Pkg.Path()
	}
	if fn.Object() != nil && fn.Object().Pkg() != nil {
		return fn.Object().Pkg().Path()
	}
	return ""
}

func (b *builder) externalNode(name, pkg, sig string) *Node {
	if !b.opt.External {
		return nil
	}
	return b.add(&Node{ID: "ext:" + pkg + ":" + name, Kind: KindExternal, Name: name, Pkg: pkg, Signature: sig})
}

// expand explores calls breadth-first from the given functions.
func (b *builder) expand(roots []*ssa.Function) {
	type item struct {
		fn    *ssa.Function
		depth int
	}
	var queue []item
	seen := map[*ssa.Function]bool{}
	for _, r := range roots {
		if !seen[r] {
			seen[r] = true
			queue = append(queue, item{r, 0})
		}
	}
	for len(queue) > 0 {
		it := queue[0]
		queue = queue[1:]
		from := b.funcNode(it.fn)
		if from == nil {
			continue
		}
		sites := b.p.calls[it.fn]
		if it.depth >= b.opt.Depth {
			if len(sites) > 0 {
				from.Truncated = true
				b.g.Truncated = true
			}
			continue
		}
		for _, cs := range sites {
			for _, t := range cs.targets {
				to := b.targetNode(cs, t)
				if to == nil {
					if b.full {
						from.Truncated = true
					}
					continue
				}
				b.edge(&Edge{From: from.ID, To: to.ID, Kind: cs.kind, Confidence: t.conf, File: cs.file, Line: cs.pos, Note: t.note})
				if t.fn != nil && b.p.inModule(t.fn) {
					fn := origin(t.fn)
					if !seen[fn] {
						seen[fn] = true
						queue = append(queue, item{fn, it.depth + 1})
					}
				}
			}
		}
	}
}

func (b *builder) targetNode(cs callSite, t target) *Node {
	switch {
	case t.db != nil:
		name := t.db.lib + " " + t.db.op
		n := &Node{ID: DBNodeID(cs.file, cs.pos, t.db.op), Kind: KindDB, Name: name, Pkg: t.db.lib,
			File: cs.file, Line: cs.pos, DBOp: t.db.op, DBLib: t.db.lib, SQL: t.db.sql, SQLDynamic: t.db.dynamic}
		if t.extName != "" {
			n.Signature = t.extName
		} else if t.fn != nil {
			n.Signature = shortName(t.fn)
		}
		if t.db.implType != "" {
			n.Note = "through a module interface implemented by " + t.db.implType
		}
		return b.add(n)
	case t.fn != nil:
		return b.funcNode(t.fn)
	case t.extName != "":
		return b.externalNode(t.extName, t.extPkg, "")
	default:
		return b.add(&Node{ID: fmt.Sprintf("unresolved:%s:%d", cs.file, cs.pos), Kind: KindUnresolved,
			Name: "unresolved call", File: cs.file, Line: cs.pos, Note: t.note})
	}
}

// routesTo walks callers upwards from fn and adds the HTTP routes whose
// handler chain reaches it, with the shortest caller path for each.
func (b *builder) routesTo(fn *ssa.Function) {
	type step struct {
		fn   *ssa.Function
		next *step // towards the root function
		ref  callerRef
	}
	visited := map[*ssa.Function]bool{fn: true}
	frontier := []*step{{fn: fn}}
	found := 0
	for depth := 0; depth <= b.opt.Depth && len(frontier) > 0 && found < 8; depth++ {
		var next []*step
		for _, s := range frontier {
			for _, r := range b.p.handler[s.fn] {
				b.addRoute(r)
				// Chain: route → handler → ... → fn
				for cur := s; cur.next != nil; cur = cur.next {
					from, to := b.funcNode(cur.fn), b.funcNode(cur.next.fn)
					if from != nil && to != nil {
						b.edge(&Edge{From: from.ID, To: to.ID, Kind: EdgeCall, Confidence: cur.ref.conf, File: cur.ref.file, Line: cur.ref.line})
					}
				}
				found++
			}
			for _, ref := range b.p.callers[s.fn] {
				c := origin(ref.caller)
				if visited[c] || ref.conf == Unresolved {
					continue
				}
				visited[c] = true
				next = append(next, &step{fn: c, next: s, ref: ref})
			}
		}
		frontier = next
	}
}

func (b *builder) addRoute(r *route) {
	id := fmt.Sprintf("route:%s %s@%s:%d", r.method, r.path, r.file, r.line)
	if _, ok := b.nodes[id]; ok {
		return
	}
	method := r.method
	if method == "" {
		method = "ANY"
	}
	n := b.add(&Node{ID: id, Kind: KindRoute, Name: method + " " + r.path, Method: method, Path: r.path,
		Router: r.router, File: r.file, Line: r.line, Partial: r.partial, Note: strings.Join(r.notes, "; ")})
	if n == nil {
		return
	}
	n.Snippet = b.p.source(r.file, r.line, r.line)
	if !b.noRouteRoots {
		b.g.Roots = append(b.g.Roots, Root{ID: id, Label: n.Name, Kind: "route"})
	}
	for i, h := range r.handlers {
		var to *Node
		if h.fn != nil {
			to = b.funcNode(h.fn)
			if to != nil && h.middleware && !hasTag(to, "middleware") {
				to.Tags = append(to.Tags, "middleware")
			}
		} else {
			to = b.add(&Node{ID: fmt.Sprintf("unresolved:%s:%d:h%d", r.file, r.line, i), Kind: KindUnresolved,
				Name: "unresolved handler", File: r.file, Line: r.line, Note: h.note})
		}
		if to != nil {
			b.edge(&Edge{From: id, To: to.ID, Kind: EdgeHandler, Confidence: h.conf, File: r.file, Line: r.line, Order: i + 1, Note: h.note})
		}
	}
}

func hasTag(n *Node, t string) bool {
	for _, x := range n.Tags {
		if x == t {
			return true
		}
	}
	return false
}

// Flow builds the graph for the given root functions.
func (p *Program) Flow(roots []*ssa.Function, rootKind string, opt Options) *Graph {
	b := p.newBuilder(opt)
	b.g.MaxDepth = b.opt.Depth
	for _, r := range roots {
		if n := b.funcNode(r); n != nil {
			b.g.Roots = append(b.g.Roots, Root{ID: n.ID, Label: n.Name, Kind: rootKind})
		}
	}
	b.expand(roots)
	if b.opt.Routes {
		for _, r := range roots {
			b.routesTo(origin(r))
		}
		// Expand the handler chains of discovered routes too, so middleware
		// and handlers show their own calls.
		var more []*ssa.Function
		for id, fn := range b.fnOf {
			if hasTag(b.nodes[id], "http handler") || hasTag(b.nodes[id], "middleware") {
				more = append(more, fn)
			}
		}
		sort.Slice(more, func(i, j int) bool { return more[i].String() < more[j].String() })
		b.expand(more)
	}
	b.finish()
	return b.g
}

// RoutesFlow builds the graph of every discovered route.
func (p *Program) RoutesFlow(opt Options) *Graph {
	b := p.newBuilder(opt)
	b.g.MaxDepth = b.opt.Depth
	var handlers []*ssa.Function
	for _, r := range p.routes {
		b.addRoute(r)
		for _, h := range r.handlers {
			if h.fn != nil {
				handlers = append(handlers, origin(h.fn))
			}
		}
	}
	b.expand(handlers)
	b.finish()
	return b.g
}

// finish marks recursion, tags database access and writes explanations.
func (b *builder) finish() {
	g := b.g
	out := map[string][]string{}
	for _, e := range g.Edges {
		out[e.From] = append(out[e.From], e.To)
	}
	for _, scc := range tarjan(g.Nodes, out) {
		if len(scc) > 1 {
			for _, id := range scc {
				b.nodes[id].Recursive = true
			}
		}
	}
	for _, e := range g.Edges {
		if e.From == e.To {
			b.nodes[e.From].Recursive = true
		}
		if to := b.nodes[e.To]; to != nil && to.Kind == KindDB {
			if from := b.nodes[e.From]; from != nil && !hasTag(from, "database access") {
				from.Tags = append(from.Tags, "database access")
			}
		}
		if e.Kind == EdgeGo {
			if to := b.nodes[e.To]; to != nil && !hasTag(to, "goroutine") {
				to.Tags = append(to.Tags, "goroutine")
			}
		}
	}
	if b.full {
		g.Notes = append(g.Notes, fmt.Sprintf("Graph limited to %d nodes; some calls were not explored.", b.opt.MaxNodes))
	}
	for _, n := range g.Nodes {
		n.Note = strings.TrimSpace(n.Note)
		b.explain(n)
	}
}

func tarjan(nodes []*Node, out map[string][]string) [][]string {
	index := map[string]int{}
	low := map[string]int{}
	on := map[string]bool{}
	var stack []string
	var res [][]string
	i := 0
	var strong func(v string)
	strong = func(v string) {
		index[v], low[v] = i, i
		i++
		stack = append(stack, v)
		on[v] = true
		for _, w := range out[v] {
			if _, ok := index[w]; !ok {
				strong(w)
				low[v] = min(low[v], low[w])
			} else if on[w] {
				low[v] = min(low[v], index[w])
			}
		}
		if low[v] == index[v] {
			var scc []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				on[w] = false
				scc = append(scc, w)
				if w == v {
					break
				}
			}
			res = append(res, scc)
		}
	}
	for _, n := range nodes {
		if _, ok := index[n.ID]; !ok {
			strong(n.ID)
		}
	}
	return res
}

// Callers lists every known caller of a node across the whole module, not
// only those inside the graph.
func (p *Program) Callers(id string) []Func {
	for _, fn := range p.funcs {
		if fn.String() != id {
			continue
		}
		var out []Func
		for _, ref := range p.callers[fn] {
			out = append(out, Func{ID: origin(ref.caller).String(), Name: shortName(ref.caller), File: ref.file, Line: ref.line})
		}
		return out
	}
	return nil
}
