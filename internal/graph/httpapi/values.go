package httpapi

import (
	"go/constant"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"github.com/athos/gitview/internal/flow"
	"golang.org/x/tools/go/ssa"
)

// frame is one function being analysed for a route: the handler or a
// middleware, or a helper it calls with HTTP values. env binds the
// function's parameters and free variables to the caller's values, so a
// status or a body passed to a helper resolves to what the caller wrote.
type frame struct {
	fn     *ssa.Function
	env    map[ssa.Value]ssa.Value
	parent *frame
	site   ssa.CallInstruction // the call in parent that entered fn
	owner  *chainItem
	conf   string // confirmed, or weaker when entered through dynamic dispatch
	depth  int

	events []event // net/http writes, paired when the route is done
}

func (fr *frame) via() []string {
	var out []string
	for f := fr; f != nil && f.parent != nil; f = f.parent {
		out = append([]string{shortFn(f.fn)}, out...)
	}
	return out
}

func (fr *frame) inChain(fn *ssa.Function) bool {
	for f := fr; f != nil; f = f.parent {
		if f.fn == fn {
			return true
		}
	}
	return false
}

// bind follows parameters and free variables to the value the caller
// passed, as far as frames are known.
func bind(v ssa.Value, fr *frame) (ssa.Value, *frame) {
	for i := 0; i < 16 && fr != nil; i++ {
		x, ok := fr.env[v]
		if !ok {
			return v, fr
		}
		v, fr = x, fr.parent
	}
	return v, fr
}

// ints is a set of possible integer values; complete is false when some
// path yields a value that is not a known constant. A complete empty set
// means no path yields a value (a field read through a nil pointer, which
// would panic instead of answering).
type ints struct {
	vals     []int64
	complete bool
}

func (a *ints) add(b ints) {
	for _, v := range b.vals {
		found := false
		for _, x := range a.vals {
			if x == v {
				found = true
			}
		}
		if !found {
			a.vals = append(a.vals, v)
		}
	}
	a.complete = a.complete && b.complete
	sort.Slice(a.vals, func(i, j int) bool { return a.vals[i] < a.vals[j] })
}

// evaluator resolves values of the analysed program.
type evaluator struct{}

func newEvaluator(*ssa.Program) *evaluator { return &evaluator{} }

// intValues resolves the possible values of an integer expression: a
// constant, a parameter bound by the caller, a field set by a constructor
// (NewError(http.StatusNotFound, ...)), a package variable initialised
// with such a call, or a function that returns constants.
func (e *evaluator) intValues(v ssa.Value, fr *frame, depth int) ints {
	if depth > 10 {
		return ints{complete: false}
	}
	v, fr = bind(v, fr)
	switch x := v.(type) {
	case *ssa.Const:
		if x.Value != nil && x.Value.Kind() == constant.Int {
			if n, ok := constant.Int64Val(x.Value); ok {
				return ints{vals: []int64{n}, complete: true}
			}
		}
	case *ssa.Convert:
		return e.intValues(x.X, fr, depth+1)
	case *ssa.ChangeType:
		return e.intValues(x.X, fr, depth+1)
	case *ssa.Phi:
		out := ints{complete: true}
		for _, ed := range x.Edges {
			out.add(e.intValues(ed, fr, depth+1))
		}
		return out
	case *ssa.UnOp:
		if x.Op == token.MUL {
			if fa, ok := x.X.(*ssa.FieldAddr); ok {
				return e.fieldInts(fa.X, fa.Field, fr, depth+1)
			}
		}
	case *ssa.Field:
		return e.fieldInts(x.X, x.Field, fr, depth+1)
	case *ssa.Call:
		out := ints{complete: true}
		rets := returns(x, fr)
		if len(rets) == 0 {
			return ints{}
		}
		for _, r := range rets {
			out.add(e.intValues(r.v, r.fr, depth+1))
		}
		return out
	}
	return ints{}
}

type valueIn struct {
	v  ssa.Value
	fr *frame
}

// returns lists the first result of every return of a module callee, in a
// frame binding its parameters to the call's arguments.
func returns(call *ssa.Call, fr *frame) []valueIn {
	fn := call.Common().StaticCallee()
	if fn == nil || len(fn.Blocks) == 0 || fn.Pkg == nil {
		return nil
	}
	if strings.HasPrefix(fn.Pkg.Pkg.Path(), "net/") || fn.Synthetic != "" {
		return nil
	}
	callee := &frame{fn: fn, env: argEnv(fn, call.Common()), parent: fr, site: call}
	var out []valueIn
	for _, b := range fn.Blocks {
		for _, ins := range b.Instrs {
			if ret, ok := ins.(*ssa.Return); ok && len(ret.Results) > 0 {
				out = append(out, valueIn{ret.Results[0], callee})
			}
		}
	}
	return out
}

func argEnv(fn *ssa.Function, c *ssa.CallCommon) map[ssa.Value]ssa.Value {
	env := map[ssa.Value]ssa.Value{}
	args := c.Args
	if c.IsInvoke() {
		// fn is a concrete method: Params[0] is the receiver.
		if len(fn.Params) > 0 {
			env[fn.Params[0]] = c.Value
		}
		for i, a := range args {
			if i+1 < len(fn.Params) {
				env[fn.Params[i+1]] = a
			}
		}
	} else {
		for i, a := range args {
			if i < len(fn.Params) {
				env[fn.Params[i]] = a
			}
		}
	}
	if mc, ok := c.Value.(*ssa.MakeClosure); ok {
		for i, fv := range fn.FreeVars {
			if i < len(mc.Bindings) {
				env[fv] = mc.Bindings[i]
			}
		}
	}
	return env
}

// fieldInts resolves field idx of the struct obj points to (or is).
func (e *evaluator) fieldInts(obj ssa.Value, idx int, fr *frame, depth int) ints {
	if depth > 10 {
		return ints{}
	}
	obj, fr = bind(obj, fr)
	switch x := obj.(type) {
	case *ssa.Const:
		if x.IsNil() {
			return ints{complete: true} // reading a field of nil panics: no response on this path
		}
	case *ssa.Alloc:
		return e.storedInts(x, idx, fr, depth+1)
	case *ssa.Call:
		out := ints{complete: true}
		rets := returns(x, fr)
		if len(rets) == 0 {
			return ints{}
		}
		for _, r := range rets {
			out.add(e.fieldInts(r.v, idx, r.fr, depth+1))
		}
		return out
	case *ssa.UnOp:
		if x.Op == token.MUL {
			if g, ok := x.X.(*ssa.Global); ok {
				out := ints{complete: true}
				vals := globalStores(g)
				if len(vals) == 0 {
					return ints{}
				}
				for _, v := range vals {
					out.add(e.fieldInts(v, idx, nil, depth+1))
				}
				return out
			}
			// A struct value loaded from a pointer (c := *e): its field
			// is the pointee's.
			return e.fieldInts(x.X, idx, fr, depth+1)
		}
	case *ssa.Phi:
		out := ints{complete: true}
		for _, ed := range x.Edges {
			out.add(e.fieldInts(ed, idx, fr, depth+1))
		}
		return out
	case *ssa.ChangeType:
		return e.fieldInts(x.X, idx, fr, depth+1)
	case *ssa.MakeInterface:
		return e.fieldInts(x.X, idx, fr, depth+1)
	}
	return ints{}
}

// storedInts resolves the values stored into field idx of an allocation
// (a composite literal &T{Status: s}).
func (e *evaluator) storedInts(a *ssa.Alloc, idx int, fr *frame, depth int) ints {
	out := ints{complete: true}
	found := false
	for _, r := range *a.Referrers() {
		if st, ok := r.(*ssa.Store); ok && st.Addr == a {
			// The whole struct is copied in (c := *e; return &c).
			found = true
			out.add(e.fieldInts(st.Val, idx, fr, depth+1))
			continue
		}
		fa, ok := r.(*ssa.FieldAddr)
		if !ok || fa.Field != idx {
			continue
		}
		for _, r2 := range *fa.Referrers() {
			if st, ok := r2.(*ssa.Store); ok && st.Addr == fa {
				found = true
				out.add(e.intValues(st.Val, fr, depth+1))
			}
		}
	}
	if !found {
		return ints{}
	}
	return out
}

// globalStores lists the values stored into a package variable by its
// package's initialisation.
func globalStores(g *ssa.Global) []ssa.Value {
	if g.Pkg == nil {
		return nil
	}
	var out []ssa.Value
	if init := g.Pkg.Func("init"); init != nil {
		for _, b := range init.Blocks {
			for _, ins := range b.Instrs {
				if st, ok := ins.(*ssa.Store); ok && st.Addr == g {
					out = append(out, st.Val)
				}
			}
		}
	}
	return out
}

// stringValue resolves a constant string (a header name, a query key, a
// content type), through bound parameters.
func stringValue(v ssa.Value, fr *frame) (string, bool) {
	v, _ = bind(v, fr)
	switch x := v.(type) {
	case *ssa.Const:
		if x.Value != nil && x.Value.Kind() == constant.String {
			return constant.StringVal(x.Value), true
		}
	case *ssa.BinOp:
		if x.Op == token.ADD {
			a, ok1 := stringValue(x.X, fr)
			b, ok2 := stringValue(x.Y, fr)
			if ok1 && ok2 {
				return a + b, true
			}
		}
	}
	return "", false
}

// stringPrefix is the constant prefix of a string expression (":" + port).
func stringPrefix(v ssa.Value, fr *frame) string {
	v, fr = bind(v, fr)
	if b, ok := v.(*ssa.BinOp); ok && b.Op == token.ADD {
		if s, ok := stringValue(b.X, fr); ok {
			return s
		}
		return stringPrefix(b.X, fr)
	}
	return ""
}

// bodyValue is what a response or request body expression resolves to: a
// concrete type, or a map literal whose keys are constants.
type bodyValue struct {
	t       types.Type
	keys    []mapKey
	partial string // why more keys may be present
	known   bool
	fr      *frame
}

type mapKey struct {
	name        string
	t           types.Type
	conditional bool
}

// bodyOf resolves the dynamic value behind an interface argument
// (c.JSON(status, v), Encode(v)), through helpers' parameters.
func bodyOf(v ssa.Value, fr *frame, use ssa.Instruction, depth int) bodyValue {
	if depth > 10 {
		return bodyValue{}
	}
	v, fr = bind(v, fr)
	switch x := v.(type) {
	case *ssa.MakeInterface:
		return concreteBody(x.X, fr, use, depth+1)
	case *ssa.ChangeInterface:
		return bodyOf(x.X, fr, use, depth+1)
	case *ssa.Phi:
		// Several values: only describe it when all are the same type.
		var first bodyValue
		for i, ed := range x.Edges {
			b := bodyOf(ed, fr, use, depth+1)
			if !b.known {
				return bodyValue{}
			}
			if i == 0 {
				first = b
			} else if !types.Identical(b.t, first.t) {
				return bodyValue{}
			}
		}
		return first
	}
	if _, isIface := v.Type().Underlying().(*types.Interface); isIface {
		return bodyValue{}
	}
	return concreteBody(v, fr, use, depth+1)
}

func concreteBody(x ssa.Value, fr *frame, use ssa.Instruction, depth int) bodyValue {
	x, fr = bind(x, fr)
	if _, isMap := x.Type().Underlying().(*types.Map); isMap {
		if m, ok := x.(*ssa.MakeMap); ok {
			return mapLiteral(m, fr, use, depth)
		}
	}
	if _, isIface := x.Type().Underlying().(*types.Interface); isIface {
		return bodyOf(x, fr, use, depth+1)
	}
	return bodyValue{t: x.Type(), known: true, fr: fr}
}

// mapLiteral reads the constant keys written into a map (gin.H{"error":
// msg}, body["code"] = c) and whether anything else may add keys.
func mapLiteral(m *ssa.MakeMap, fr *frame, use ssa.Instruction, depth int) bodyValue {
	out := bodyValue{t: m.Type(), known: true, fr: fr}
	seen := map[string]int{}
	for _, r := range *m.Referrers() {
		switch u := r.(type) {
		case *ssa.MapUpdate:
			name, ok := stringValue(u.Key, fr)
			if !ok {
				out.partial = "a key is computed at runtime"
				continue
			}
			var vt types.Type
			if mi, ok := u.Value.(*ssa.MakeInterface); ok {
				vt = mi.X.Type()
			} else {
				vt = u.Value.Type()
			}
			cond := use != nil && use.Block() != nil && u.Block() != m.Block() && !u.Block().Dominates(use.Block())
			if i, dup := seen[name]; dup {
				out.keys[i].conditional = out.keys[i].conditional && cond
				continue
			}
			seen[name] = len(out.keys)
			out.keys = append(out.keys, mapKey{name: name, t: vt, conditional: cond})
		case *ssa.MakeInterface, *ssa.Lookup, *ssa.Range, *ssa.DebugRef:
			// reading or passing on the map
		case ssa.CallInstruction:
			if callsName(u.Common(), "maps", "Copy") {
				out.partial = "keys copied from another map (maps.Copy)"
			} else {
				out.partial = "the map is passed to " + calleeName(u.Common()) + ", which may add keys"
			}
		}
	}
	sort.SliceStable(out.keys, func(i, j int) bool { return out.keys[i].name < out.keys[j].name })
	return out
}

func callsName(c *ssa.CallCommon, pkg, name string) bool {
	if fn := c.StaticCallee(); fn != nil && fn.Pkg != nil {
		return fn.Pkg.Pkg.Path() == pkg && fn.Name() == name
	}
	if fn := c.StaticCallee(); fn != nil && fn.Object() != nil && fn.Object().Pkg() != nil {
		return fn.Object().Pkg().Path() == pkg && fn.Name() == name
	}
	return false
}

func calleeName(c *ssa.CallCommon) string {
	if c.IsInvoke() {
		return c.Method.Name()
	}
	if fn := c.StaticCallee(); fn != nil {
		return shortFn(fn)
	}
	return "a function value"
}

func shortFn(fn *ssa.Function) string { return flow.ShortName(fn) }
