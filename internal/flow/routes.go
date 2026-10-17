package flow

import (
	"go/constant"
	"go/types"
	"strings"

	"golang.org/x/tools/go/ssa"
)

// route is one HTTP route registration found in the code.
type route struct {
	router   string
	method   string
	path     string
	partial  bool
	notes    []string
	file     string
	line     int
	handlers []routeHandler // in execution order: middlewares, then the handler
}

type routeHandler struct {
	fn         *ssa.Function
	conf       string
	middleware bool
	note       string
}

type routerLib struct {
	name    string
	pkgs    []string
	methods map[string]string // registration method → HTTP method ("" = any, "*" = taken from an argument)
	group   map[string]bool   // methods returning a sub-router with a path prefix
	use     map[string]bool   // methods adding middleware to a router
}

var httpVerbs = map[string]string{
	"GET": "GET", "POST": "POST", "PUT": "PUT", "DELETE": "DELETE", "PATCH": "PATCH",
	"HEAD": "HEAD", "OPTIONS": "OPTIONS", "CONNECT": "CONNECT", "TRACE": "TRACE",
}

var routerLibs = []routerLib{
	{
		name: "gin", pkgs: []string{"github.com/gin-gonic/gin"},
		methods: merge(httpVerbs, map[string]string{"Any": "", "Handle": "*"}),
		group:   map[string]bool{"Group": true}, use: map[string]bool{"Use": true},
	},
	{
		name: "echo", pkgs: []string{"github.com/labstack/echo/v4", "github.com/labstack/echo"},
		methods: merge(httpVerbs, map[string]string{"Any": "", "Add": "*"}),
		group:   map[string]bool{"Group": true}, use: map[string]bool{"Use": true},
	},
	{
		name: "chi", pkgs: []string{"github.com/go-chi/chi/v5", "github.com/go-chi/chi"},
		methods: map[string]string{
			"Get": "GET", "Post": "POST", "Put": "PUT", "Delete": "DELETE", "Patch": "PATCH", "Head": "HEAD",
			"Options": "OPTIONS", "Connect": "CONNECT", "Trace": "TRACE", "Handle": "", "HandleFunc": "",
			"Method": "*", "MethodFunc": "*", "Mount": "",
		},
		group: map[string]bool{"Route": true}, use: map[string]bool{"Use": true},
	},
	{
		name: "net/http", pkgs: []string{"net/http"},
		methods: map[string]string{"Handle": "", "HandleFunc": ""},
	},
}

func merge(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

// callee returns the package path and name of a call's target, for static
// calls and interface invocations alike.
func calleeOf(c *ssa.CallCommon) (pkg, name string, recv ssa.Value, args []ssa.Value) {
	if c.IsInvoke() {
		return pkgOf(c.Value.Type()), c.Method.Name(), c.Value, c.Args
	}
	fn := c.StaticCallee()
	if fn == nil || fn.Pkg == nil {
		if fn != nil && fn.Object() != nil && fn.Object().Pkg() != nil {
			return fn.Object().Pkg().Path(), fn.Name(), nil, c.Args
		}
		return "", "", nil, nil
	}
	pkg, name = fn.Pkg.Pkg.Path(), fn.Name()
	if fn.Signature.Recv() != nil && len(c.Args) > 0 {
		return pkg, name, c.Args[0], c.Args[1:]
	}
	return pkg, name, nil, c.Args
}

func libFor(pkg string) *routerLib {
	for i := range routerLibs {
		for _, p := range routerLibs[i].pkgs {
			if pkg == p {
				return &routerLibs[i]
			}
		}
	}
	return nil
}

// routeScan holds state shared while scanning registrations.
type routeScan struct {
	p *Program
	// prefix contexts for chi-style callbacks: Route("/x", func(r Router){...})
	fnPrefix map[*ssa.Function][]string
	// middleware registered with Use, keyed by the router value
	uses map[ssa.Value][]routeHandler
}

func (p *Program) findRoutes() {
	s := &routeScan{p: p, fnPrefix: map[*ssa.Function][]string{}, uses: map[ssa.Value][]routeHandler{}}
	// First pass: chi Route/Group callbacks and Use calls.
	for _, fn := range p.funcs {
		forEachCall(fn, func(c *ssa.CallCommon, ins ssa.CallInstruction) {
			pkg, name, recv, args := calleeOf(c)
			lib := libFor(pkg)
			if lib == nil || recv == nil {
				return
			}
			switch {
			case lib.use[name]:
				key := routerKey(recv)
				s.uses[key] = append(s.uses[key], s.handlerArgs(args, true)...)
			case lib.name == "chi" && (name == "Route" || name == "Group"):
				prefixes := s.prefixes(recv, 0)
				if name == "Route" && len(args) > 0 {
					seg, ok := constString(args[0])
					for i := range prefixes {
						if ok {
							prefixes[i] = joinPath(prefixes[i], seg)
						} else {
							prefixes[i] = joinPath(prefixes[i], "{dynamic}")
						}
					}
				}
				if len(args) > 0 {
					if cb := funcValue(args[len(args)-1]); cb != nil {
						s.fnPrefix[cb] = append(s.fnPrefix[cb], prefixes...)
					}
				}
			}
		})
	}
	// Second pass: route registrations.
	for _, fn := range p.funcs {
		forEachCall(fn, func(c *ssa.CallCommon, ins ssa.CallInstruction) {
			pkg, name, recv, args := calleeOf(c)
			lib := libFor(pkg)
			if lib == nil {
				return
			}
			verb, ok := lib.methods[name]
			if !ok || (lib.name != "net/http" && recv == nil) {
				return
			}
			if lib.name == "net/http" && recv != nil && !isServeMux(recv.Type()) {
				return
			}
			s.register(lib, name, verb, recv, args, ins)
		})
	}
}

func isServeMux(t types.Type) bool {
	return strings.Contains(types.TypeString(t, nil), "net/http.ServeMux")
}

func forEachCall(fn *ssa.Function, f func(*ssa.CallCommon, ssa.CallInstruction)) {
	for _, b := range fn.Blocks {
		for _, ins := range b.Instrs {
			if ci, ok := ins.(ssa.CallInstruction); ok {
				f(ci.Common(), ci)
			}
		}
	}
}

func (s *routeScan) register(lib *routerLib, name, verb string, recv ssa.Value, args []ssa.Value, ins ssa.CallInstruction) {
	file, line := s.p.site(ins.Pos())
	r := &route{router: lib.name, method: verb, file: file, line: line}
	rest := args
	if verb == "*" { // Handle(method, path, ...), Add(method, path, ...), Method(method, path, h)
		if len(rest) == 0 {
			return
		}
		if m, ok := constString(rest[0]); ok {
			r.method = strings.ToUpper(m)
		} else {
			r.method, r.partial = "?", true
			r.notes = append(r.notes, "HTTP method is computed at runtime")
		}
		rest = rest[1:]
	}
	if len(rest) == 0 {
		return
	}
	pattern, ok := constString(rest[0])
	if !ok {
		pattern, r.partial = "{dynamic}", true
		r.notes = append(r.notes, "path is computed at runtime")
	}
	if lib.name == "net/http" {
		if m, p, found := strings.Cut(pattern, " "); found && httpVerbs[strings.ToUpper(m)] != "" {
			r.method, pattern = strings.ToUpper(m), strings.TrimSpace(p)
		}
	}
	rest = rest[1:]

	var prefixes []string
	if recv != nil {
		prefixes = s.prefixes(recv, 0)
	} else {
		prefixes = []string{""}
	}
	for _, pre := range prefixes {
		if strings.Contains(pre, "{unknown}") {
			r.partial = true
			r.notes = append(r.notes, "router prefix could not be resolved statically")
			break
		}
	}

	// Middleware from Use on this router and its parents runs first.
	var chain []routeHandler
	if recv != nil {
		chain = append(chain, s.middlewareOf(recv, 0)...)
	}
	hs := s.handlerArgs(rest, false)
	switch lib.name {
	case "gin": // handlers...: all but the last are middleware
		for i := range hs {
			hs[i].middleware = i < len(hs)-1
		}
		chain = append(chain, hs...)
	case "echo": // h, middleware...: middleware wraps the handler
		if len(hs) > 0 {
			for _, m := range hs[1:] {
				m.middleware = true
				chain = append(chain, m)
			}
			chain = append(chain, hs[0])
		}
	default:
		chain = append(chain, hs...)
	}
	if len(hs) == 0 {
		r.partial = true
		r.notes = append(r.notes, "handler could not be resolved statically")
	}
	r.handlers = chain
	for _, pre := range prefixes {
		rr := *r
		rr.path = joinPath(strings.ReplaceAll(pre, "{unknown}", ""), pattern)
		if rr.path == "" {
			rr.path = "/"
		}
		s.p.routes = append(s.p.routes, &rr)
		for _, h := range rr.handlers {
			if h.fn != nil && !h.middleware {
				s.p.handler[h.fn] = append(s.p.handler[h.fn], &rr)
			}
		}
	}
}

// prefixes resolves the path prefixes a router value carries: sub-routers
// created with Group("/x"), chi callbacks, and routers passed as parameters
// (resolved through every call site of the enclosing function).
func (s *routeScan) prefixes(v ssa.Value, depth int) []string {
	if depth > 6 {
		return []string{"{unknown}"}
	}
	switch v := v.(type) {
	case *ssa.Call:
		pkg, name, recv, args := calleeOf(v.Common())
		if lib := libFor(pkg); lib != nil && lib.group[name] && recv != nil && len(args) > 0 {
			seg, ok := constString(args[0])
			if !ok {
				seg = "{unknown}"
			}
			var out []string
			for _, pre := range s.prefixes(recv, depth+1) {
				out = append(out, joinPath(pre, seg))
			}
			return out
		}
		if lib := libFor(pkg); lib != nil {
			return []string{""} // New(), Default(), NewRouter()...
		}
		// A module function returning a router: use what it returns.
		var out []string
		seen := map[string]bool{}
		for _, r := range s.p.returnedValues(v) {
			for _, pre := range s.prefixes(r, depth+1) {
				if !seen[pre] {
					seen[pre] = true
					out = append(out, pre)
				}
			}
		}
		if len(out) > 0 {
			return out
		}
		return []string{"{unknown}"}
	case *ssa.ChangeType:
		return s.prefixes(v.X, depth+1)
	case *ssa.MakeInterface:
		return s.prefixes(v.X, depth+1)
	case *ssa.FieldAddr: // promoted method of an embedded router (gin.Engine → RouterGroup)
		return s.prefixes(v.X, depth+1)
	case *ssa.Parameter:
		fn := v.Parent()
		if pre, ok := s.fnPrefix[fn]; ok {
			return pre
		}
		idx := -1
		for i, prm := range fn.Params {
			if prm == v {
				idx = i
			}
		}
		var out []string
		seen := map[string]bool{}
		for _, ref := range s.p.callers[fn] {
			for _, cs := range s.p.calls[ref.caller] {
				if cs.instr.Common().StaticCallee() == nil || unwrap(cs.instr.Common().StaticCallee()) != fn {
					continue
				}
				args := cs.instr.Common().Args
				if idx < 0 || idx >= len(args) {
					continue
				}
				for _, pre := range s.prefixes(args[idx], depth+1) {
					if !seen[pre] {
						seen[pre] = true
						out = append(out, pre)
					}
				}
			}
		}
		if len(out) == 0 {
			return []string{"{unknown}"}
		}
		return out
	}
	return []string{"{unknown}"}
}

// middlewareOf collects middleware registered with Use on a router value and
// on the routers it was derived from.
func (s *routeScan) middlewareOf(v ssa.Value, depth int) []routeHandler {
	if depth > 6 {
		return nil
	}
	var parent []routeHandler
	switch x := v.(type) {
	case *ssa.Call:
		pkg, name, recv, _ := calleeOf(x.Common())
		if lib := libFor(pkg); lib != nil && lib.group[name] && recv != nil {
			parent = s.middlewareOf(recv, depth+1)
		} else if libFor(pkg) == nil {
			for _, r := range s.p.returnedValues(x) {
				parent = append(parent, s.middlewareOf(r, depth+1)...)
			}
		}
	case *ssa.ChangeType:
		parent = s.middlewareOf(x.X, depth+1)
	case *ssa.MakeInterface:
		parent = s.middlewareOf(x.X, depth+1)
	case *ssa.FieldAddr:
		return s.middlewareOf(x.X, depth+1)
	case *ssa.Parameter: // router received as argument: middleware of every caller's router
		fn := x.Parent()
		idx := -1
		for i, prm := range fn.Params {
			if prm == x {
				idx = i
			}
		}
		seen := map[*ssa.Function]bool{}
		for _, ref := range s.p.callers[fn] {
			for _, cs := range s.p.calls[ref.caller] {
				sc := cs.instr.Common().StaticCallee()
				if sc == nil || unwrap(sc) != fn || idx < 0 || idx >= len(cs.instr.Common().Args) {
					continue
				}
				for _, m := range s.middlewareOf(cs.instr.Common().Args[idx], depth+1) {
					if m.fn == nil || !seen[m.fn] {
						if m.fn != nil {
							seen[m.fn] = true
						}
						parent = append(parent, m)
					}
				}
			}
		}
	}
	out := append([]routeHandler{}, parent...)
	for _, m := range s.uses[routerKey(v)] {
		m.middleware = true
		out = append(out, m)
	}
	return out
}

// handlerArgs resolves handler arguments, expanding variadic slices.
func (s *routeScan) handlerArgs(args []ssa.Value, middleware bool) []routeHandler {
	var out []routeHandler
	for _, a := range args {
		for _, h := range s.p.expandHandlers(a, nil, 0) {
			h.middleware = middleware
			out = append(out, h)
		}
	}
	return out
}

// env maps a callee's parameters and free variables to the caller's values,
// so handlers built inside helper functions resolve to the real arguments.
type env struct {
	vals   map[ssa.Value]ssa.Value
	parent *env
}

func (e *env) lookup(v ssa.Value) (ssa.Value, *env, bool) {
	if e == nil {
		return nil, nil, false
	}
	if x, ok := e.vals[v]; ok {
		return x, e.parent, true
	}
	return nil, nil, false
}

// expandHandlers resolves a handler argument that may be a single handler, a
// variadic list, or a slice returned by a helper (e.g. chain(h)...).
func (p *Program) expandHandlers(v ssa.Value, e *env, depth int) []routeHandler {
	if depth > 5 {
		return []routeHandler{{conf: Unresolved, note: "handler list too indirect to resolve"}}
	}
	if x, outer, ok := e.lookup(v); ok {
		return p.expandHandlers(x, outer, depth+1)
	}
	if k, ok := v.(*ssa.Const); ok && k.IsNil() {
		return nil // empty variadic list
	}
	if t, ok := v.Type().Underlying().(*types.Basic); ok && t.Kind() == types.String {
		return nil
	}
	if _, isSlice := v.Type().Underlying().(*types.Slice); isSlice {
		if call, ok := v.(*ssa.Call); ok {
			if b, ok := call.Common().Value.(*ssa.Builtin); ok && b.Name() == "append" {
				var out []routeHandler
				for _, a := range call.Common().Args { // base slice, then the appended elements
					out = append(out, p.expandHandlers(a, e, depth+1)...)
				}
				return out
			}
			var out []routeHandler
			for _, r := range p.returnedValues(call) {
				out = append(out, p.expandHandlers(r, calleeEnv(call, e), depth+1)...)
			}
			if len(out) > 0 {
				return out
			}
			return []routeHandler{{conf: Unresolved, note: "handler list returned by a call that could not be followed"}}
		}
		elems := variadic(v)
		if len(elems) == 1 && elems[0] == v {
			return []routeHandler{{conf: Unresolved, note: "handler list not determinable statically"}}
		}
		var out []routeHandler
		for _, el := range elems {
			out = append(out, p.expandHandlers(el, e, depth+1)...)
		}
		return out
	}
	return []routeHandler{p.resolveHandlerIn(v, e, 0)}
}

// calleeEnv binds the callee's parameters and free variables for a call.
func calleeEnv(call *ssa.Call, parent *env) *env {
	c := call.Common()
	fn := c.StaticCallee()
	if fn == nil {
		return parent
	}
	e := &env{vals: map[ssa.Value]ssa.Value{}, parent: parent}
	for i, prm := range fn.Params {
		if i < len(c.Args) {
			e.vals[prm] = c.Args[i]
		}
	}
	if mc, ok := c.Value.(*ssa.MakeClosure); ok {
		for i, fv := range fn.FreeVars {
			if i < len(mc.Bindings) {
				e.vals[fv] = mc.Bindings[i]
			}
		}
	}
	return e
}

// returnedValues lists the first result of every return of a module callee.
func (p *Program) returnedValues(call *ssa.Call) []ssa.Value {
	fn := call.Common().StaticCallee()
	if fn == nil || !p.inModule(unwrap(fn)) {
		return nil
	}
	var out []ssa.Value
	for _, b := range fn.Blocks {
		for _, ins := range b.Instrs {
			if ret, ok := ins.(*ssa.Return); ok && len(ret.Results) > 0 {
				out = append(out, ret.Results[0])
			}
		}
	}
	return out
}

// variadic expands a slice built for a variadic call into its elements.
func variadic(v ssa.Value) []ssa.Value {
	if k, ok := v.(*ssa.Const); ok && k.IsNil() {
		return nil
	}
	sl, ok := v.(*ssa.Slice)
	if !ok {
		return []ssa.Value{v}
	}
	alloc, ok := sl.X.(*ssa.Alloc)
	if !ok {
		return []ssa.Value{v}
	}
	type elem struct {
		idx int64
		v   ssa.Value
	}
	var elems []elem
	for _, ref := range *alloc.Referrers() {
		ia, ok := ref.(*ssa.IndexAddr)
		if !ok {
			continue
		}
		idx := int64(-1)
		if k, ok := ia.Index.(*ssa.Const); ok && k.Value != nil {
			idx, _ = constant.Int64Val(k.Value)
		}
		for _, r2 := range *ia.Referrers() {
			if st, ok := r2.(*ssa.Store); ok && st.Addr == ia {
				elems = append(elems, elem{idx, st.Val})
			}
		}
	}
	for i := 1; i < len(elems); i++ {
		for j := i; j > 0 && elems[j].idx < elems[j-1].idx; j-- {
			elems[j], elems[j-1] = elems[j-1], elems[j]
		}
	}
	out := make([]ssa.Value, len(elems))
	for i, e := range elems {
		out[i] = e.v
	}
	return out
}

// resolveHandler determines the function a handler value denotes.
func (p *Program) resolveHandler(v ssa.Value, depth int) routeHandler {
	return p.resolveHandlerIn(v, nil, depth)
}

func (p *Program) resolveHandlerIn(v ssa.Value, e *env, depth int) routeHandler {
	if depth > 4 {
		return routeHandler{conf: Unresolved, note: "handler too indirect to resolve"}
	}
	if x, outer, ok := e.lookup(v); ok {
		return p.resolveHandlerIn(x, outer, depth+1)
	}
	if fn := funcValue(v); fn != nil {
		return routeHandler{fn: fn, conf: Confirmed}
	}
	switch x := v.(type) {
	case *ssa.MakeInterface: // a value implementing http.Handler
		if fn := funcValue(x.X); fn != nil {
			return routeHandler{fn: fn, conf: Confirmed}
		}
		if m := p.prog.LookupMethod(x.X.Type(), nil, "ServeHTTP"); m != nil && p.inModule(unwrap(m)) {
			return routeHandler{fn: unwrap(m), conf: Confirmed, note: "ServeHTTP of " + types.TypeString(x.X.Type(), shortQualifier)}
		}
		if inner, ok := x.X.(*ssa.Call); ok {
			return p.resolveHandlerIn(inner, e, depth+1)
		}
	case *ssa.Call: // factory or middleware wrapper: handler := mw(next) / NewHandler(svc)
		callee := x.Common().StaticCallee()
		if callee == nil {
			break
		}
		callee = unwrap(callee)
		if !p.inModule(callee) {
			return routeHandler{conf: Unresolved, note: "handler returned by external " + shortName(callee)}
		}
		// The handler is whatever function the factory returns.
		var returned []*ssa.Function
		for _, b := range callee.Blocks {
			for _, ins := range b.Instrs {
				if ret, ok := ins.(*ssa.Return); ok && len(ret.Results) > 0 {
					if fn := funcValue(ret.Results[0]); fn != nil {
						returned = append(returned, fn)
					} else if mi, ok := ret.Results[0].(*ssa.MakeInterface); ok {
						if fn := funcValue(mi.X); fn != nil {
							returned = append(returned, fn)
						}
					}
				}
			}
		}
		if len(returned) == 1 {
			return routeHandler{fn: returned[0], conf: Likely, note: "returned by " + shortName(callee)}
		}
		return routeHandler{fn: callee, conf: Confirmed, note: "handler built by " + shortName(callee)}
	case *ssa.UnOp:
		if name := valueName(x); name != "" {
			return routeHandler{conf: Unresolved, note: "handler stored in " + name}
		}
	}
	return routeHandler{conf: Unresolved, note: "handler not determinable statically"}
}

// routerKey identifies a router value regardless of embedded-field access
// or interface conversion, so Use and route calls on it can be matched.
func routerKey(v ssa.Value) ssa.Value {
	for i := 0; i < 8; i++ {
		switch x := v.(type) {
		case *ssa.FieldAddr:
			v = x.X
		case *ssa.ChangeType:
			v = x.X
		case *ssa.MakeInterface:
			v = x.X
		default:
			return v
		}
	}
	return v
}

func constString(v ssa.Value) (string, bool) {
	if k, ok := v.(*ssa.Const); ok && k.Value != nil && k.Value.Kind() == constant.String {
		return constant.StringVal(k.Value), true
	}
	return "", false
}

func joinPath(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return strings.TrimSuffix(a, "/") + "/" + strings.TrimPrefix(b, "/")
}
