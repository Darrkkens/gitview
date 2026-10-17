package flow

import (
	"fmt"
	"sort"
	"strings"

	"golang.org/x/tools/go/ssa"
)

// Symbol is an entry of the module's function index. Identity is the SSA
// function (ID = its fully qualified name), never a textual match.
type Symbol struct {
	ID        string `json:"id"`        // stable: full package path + receiver + name
	Name      string `json:"name"`      // bare name, e.g. GetClientByID
	Qualified string `json:"qualified"` // pkg.(*T).Name
	Pkg       string `json:"pkg"`       // import path
	Recv      string `json:"recv,omitempty"`
	Signature string `json:"sig"`
	File      string `json:"file"`
	Line      int    `json:"line"`
	Closure   bool   `json:"closure,omitempty"`
}

// Symbols returns the index of every function and method declared in the
// module (closures excluded), sorted by qualified name.
func (p *Program) Symbols() []Symbol {
	out := make([]Symbol, 0, len(p.funcs))
	for _, fn := range p.funcs {
		if fn.Parent() != nil {
			continue
		}
		out = append(out, p.symbol(fn))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Qualified != out[j].Qualified {
			return out[i].Qualified < out[j].Qualified
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (p *Program) symbol(fn *ssa.Function) Symbol {
	file, line, _ := p.position(fn)
	s := Symbol{ID: fn.String(), Name: fn.Name(), Qualified: shortName(fn), Pkg: pkgPath(fn),
		Signature: signature(fn), File: file, Line: line, Closure: fn.Parent() != nil}
	if r := fn.Signature.Recv(); r != nil {
		s.Recv = strings.TrimPrefix(strings.SplitN(shortName(fn), ".", 2)[1], "")
		if i := strings.LastIndexByte(s.Recv, '.'); i >= 0 {
			s.Recv = s.Recv[:i]
		}
	}
	return s
}

// Find resolves a query to symbols. Accepted forms, from most to least
// specific: the full ID ("example.com/m/svc.(*S).Get"), a qualified name
// ("svc.(*S).Get", "(*S).Get", "S.Get"), file:line, or a bare name ("Get").
// Several results mean the query is ambiguous; callers must not pick one.
func (p *Program) Find(query string) []Symbol {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil
	}
	if _, _, ok := parseFileLine(q); ok {
		var out []Symbol
		for _, fn := range p.Lookup(q) {
			out = append(out, p.symbol(fn))
		}
		return out
	}
	syms := p.Symbols()
	for _, s := range syms {
		if s.ID == q {
			return []Symbol{s}
		}
	}
	norm := func(s string) string { return strings.NewReplacer("(*", "", "(", "", ")", "", "*", "").Replace(s) }
	var exact, suffix, bare []Symbol
	nq := norm(q)
	for _, s := range syms {
		qual := norm(s.Qualified)
		switch {
		case qual == nq || norm(s.Pkg+"."+strings.SplitN(s.Qualified, ".", 2)[1]) == nq:
			exact = append(exact, s)
		case strings.Contains(q, ".") && strings.HasSuffix(qual, "."+nq):
			suffix = append(suffix, s)
		case s.Name == q:
			bare = append(bare, s)
		}
	}
	switch {
	case len(exact) > 0:
		return exact
	case len(suffix) > 0:
		return suffix
	}
	return bare
}

// AmbiguityError lists candidates when a query matches several symbols.
type AmbiguityError struct {
	Query      string
	Candidates []Symbol
}

func (e *AmbiguityError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%q is ambiguous: %d symbols found; use a qualified name or file:line\n", e.Query, len(e.Candidates))
	for _, c := range e.Candidates {
		fmt.Fprintf(&b, "  %-50s %s:%d\n", c.Qualified, c.File, c.Line)
	}
	return strings.TrimRight(b.String(), "\n")
}

// Resolve returns exactly one function for a query, or an error listing
// the candidates.
func (p *Program) Resolve(query string) (*ssa.Function, error) {
	found := p.Find(query)
	switch len(found) {
	case 0:
		return nil, fmt.Errorf("no function matching %q in module %s", query, p.Module)
	case 1:
		return p.funcByID(found[0].ID), nil
	}
	return nil, &AmbiguityError{Query: query, Candidates: found}
}

func (p *Program) funcByID(id string) *ssa.Function {
	for _, fn := range p.funcs {
		if fn.String() == id {
			return fn
		}
	}
	return nil
}

// Directions for Query.
const (
	Callers = "callers"
	Callees = "callees"
	Both    = "both"
)

// Query builds the graph centred on one function. The depth limit applies
// independently to each direction.
func (p *Program) Query(fn *ssa.Function, direction string, opt Options) (*Graph, error) {
	if fn == nil {
		return nil, fmt.Errorf("no function selected")
	}
	if direction == "" {
		direction = Both
	}
	if direction != Callers && direction != Callees && direction != Both {
		return nil, fmt.Errorf("invalid direction %q (callers, callees or both)", direction)
	}
	b := p.newBuilder(opt)
	b.g.MaxDepth = b.opt.Depth
	b.noRouteRoots = true
	root := b.funcNode(fn)
	if root == nil {
		return nil, fmt.Errorf("cannot build graph for %s", shortName(fn))
	}
	b.g.Roots = []Root{{ID: root.ID, Label: root.Name, Kind: "selected"}}
	if direction != Callers {
		b.expand([]*ssa.Function{origin(fn)})
	}
	if direction != Callees {
		b.expandCallers(origin(fn))
	}
	b.finish()
	if direction != Callees {
		b.g.Notes = append(b.g.Notes, "Callers reached only through function values stored in variables, reflection or code outside the module cannot be found statically.")
	}
	return b.g, nil
}

// expandCallers walks callers breadth-first up to the depth limit, adding
// the HTTP routes that register any function on the way.
func (b *builder) expandCallers(fn *ssa.Function) {
	type item struct {
		fn    *ssa.Function
		depth int
	}
	seen := map[*ssa.Function]bool{fn: true}
	queue := []item{{fn, 0}}
	for len(queue) > 0 {
		it := queue[0]
		queue = queue[1:]
		to := b.funcNode(it.fn)
		if to == nil {
			continue
		}
		for _, r := range b.p.handler[it.fn] {
			b.addRoute(r)
		}
		refs := b.p.callers[it.fn]
		if it.depth >= b.opt.Depth {
			if len(refs) > 0 {
				to.Note = strings.TrimSpace(to.Note + " Callers beyond the depth limit were not explored.")
				b.g.Truncated = true
			}
			continue
		}
		for _, ref := range refs {
			c := origin(ref.caller)
			from := b.funcNode(c)
			if from == nil {
				continue
			}
			b.edge(&Edge{From: from.ID, To: to.ID, Kind: b.kindOf(ref), Confidence: ref.conf, File: ref.file, Line: ref.line})
			if !seen[c] {
				seen[c] = true
				queue = append(queue, item{c, it.depth + 1})
			}
		}
	}
}

// kindOf recovers the edge kind (call/go/defer) of a caller reference.
func (b *builder) kindOf(ref callerRef) string {
	for _, cs := range b.p.calls[ref.caller] {
		if cs.file == ref.file && cs.pos == ref.line && cs.kind != EdgeCall {
			return cs.kind
		}
	}
	return EdgeCall
}

// Index builds the whole-module call graph: every function, route, database
// operation and resolved/unresolved call. It is what the HTML report embeds
// so the browser can navigate any function without further analysis.
func (p *Program) Index(opt Options) *Graph {
	opt.External = true
	if opt.MaxNodes <= 0 {
		opt.MaxNodes = 20000
	}
	if opt.Depth <= 0 {
		opt.Depth = 1 << 20 // the index is complete; depth applies when browsing
	}
	b := p.newBuilder(opt)
	b.snippetLines = 40
	for _, r := range p.routes {
		b.addRoute(r)
	}
	var all []*ssa.Function
	for _, fn := range p.funcs {
		all = append(all, fn)
	}
	b.expand(all)
	b.g.Roots = nil
	b.finish()
	return b.g
}
