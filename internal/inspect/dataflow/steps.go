package dataflow

import (
	"fmt"
	"go/constant"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"github.com/athos/gitview/internal/evidence"
	"github.com/athos/gitview/internal/flow"
	"golang.org/x/tools/go/ssa"
)

// --- origin (backwards) ----------------------------------------------------------

func (t *tracer) preds(it item) []step {
	if it.mem {
		return t.memPreds(it.v, nil)
	}
	switch x := it.v.(type) {
	case *ssa.Parameter:
		return t.paramPreds(x, it)
	case *ssa.FreeVar:
		return t.freeVarPreds(x)
	case *ssa.Const:
		n := t.event(KindSource, constText(x), "constant", x.Parent(), t.valuePos(x, false), "constant")
		if x.Parent() == nil {
			return nil
		}
		return []step{{node: n, kind: EdgeSource, conf: evidence.Exact, stop: true}}
	case *ssa.Global:
		return []step{{node: t.event(KindUnresolved, "package variable "+x.Name(), "writes to package variables are not tracked", nil, x.Pos()),
			kind: EdgeLoad, conf: evidence.Unresolved, stop: true}}
	case *ssa.Function, *ssa.MakeClosure:
		return nil
	case *ssa.Call:
		return t.callPreds(x, x.Common(), -1, it)
	case *ssa.Extract:
		if call, ok := x.Tuple.(*ssa.Call); ok {
			return t.callPreds(call, call.Common(), x.Index, it)
		}
		return []step{{v: x.Tuple, kind: EdgeDerived, conf: evidence.Exact, label: fmt.Sprintf("result #%d", x.Index), fromCtx: true}}
	case *ssa.Phi:
		var out []step
		for i, e := range x.Edges {
			label := ""
			if i < len(x.Block().Preds) {
				if l := lastLine(t.p, x.Block().Preds[i]); l > 0 {
					label = "on the path through line " + strconv.Itoa(l)
				}
			}
			out = append(out, step{v: e, kind: EdgeMerge, conf: evidence.Exact, label: label, fromCtx: true})
		}
		return out
	case *ssa.UnOp:
		switch x.Op {
		case token.MUL:
			return t.memPreds(x.X, x)
		case token.ARROW:
			return t.chanPreds(x)
		}
		return []step{{v: x.X, kind: EdgeDerived, conf: evidence.Derived, label: x.Op.String(), pos: x.Pos(), fromCtx: true}}
	case *ssa.BinOp:
		kind := EdgeDerived
		if b, ok := x.X.Type().Underlying().(*types.Basic); ok && b.Info()&types.IsString != 0 && x.Op == token.ADD {
			kind = EdgeConcat
		}
		return []step{
			{v: x.X, kind: kind, conf: evidence.Derived, label: x.Op.String(), pos: x.Pos(), fromCtx: true},
			{v: x.Y, kind: kind, conf: evidence.Derived, label: x.Op.String(), pos: x.Pos(), fromCtx: true},
		}
	case *ssa.Convert:
		return []step{{v: x.X, kind: EdgeConvert, conf: evidence.Exact, label: "conversion", pos: x.Pos(), fromCtx: true}}
	case *ssa.ChangeType:
		return []step{{v: x.X, kind: EdgeConvert, conf: evidence.Exact, pos: x.Pos(), fromCtx: true}}
	case *ssa.ChangeInterface:
		return []step{{v: x.X, kind: EdgeConvert, conf: evidence.Exact, fromCtx: true}}
	case *ssa.MakeInterface:
		return []step{{v: x.X, kind: EdgeConvert, conf: evidence.Exact, label: "to " + types.TypeString(x.Type(), qualifier), fromCtx: true}}
	case *ssa.TypeAssert:
		return []step{{v: x.X, kind: EdgeConvert, conf: evidence.Exact, label: "type assertion", pos: x.Pos(), fromCtx: true}}
	case *ssa.SliceToArrayPointer:
		return []step{{v: x.X, kind: EdgeConvert, conf: evidence.Exact, pos: x.Pos(), fromCtx: true}}
	case *ssa.MultiConvert:
		return []step{{v: x.X, kind: EdgeConvert, conf: evidence.Exact, pos: x.Pos(), fromCtx: true}}
	case *ssa.Field:
		return []step{{v: x.X, kind: EdgeDerived, conf: evidence.Derived, label: "field " + fieldName(x.X.Type(), x.Field), pos: x.Pos(), fromCtx: true}}
	case *ssa.Index:
		return []step{{v: x.X, kind: EdgeDerived, conf: evidence.Derived, label: "element", pos: x.Pos(), fromCtx: true}}
	case *ssa.Lookup:
		return []step{{v: x.X, kind: EdgeDerived, conf: evidence.Derived, label: "map lookup", pos: x.Pos(), fromCtx: true}}
	case *ssa.Slice:
		return []step{{v: x.X, kind: EdgeDerived, conf: evidence.Derived, label: "slice", pos: x.Pos(), fromCtx: true}}
	case *ssa.Next:
		return []step{{v: x.Iter, kind: EdgeDerived, conf: evidence.Derived, label: "range element", fromCtx: true}}
	case *ssa.Range:
		return []step{{v: x.X, kind: EdgeDerived, conf: evidence.Derived, label: "range", fromCtx: true}}
	case *ssa.Alloc, *ssa.FieldAddr, *ssa.IndexAddr:
		// The value is an address: where it points is its identity.
		return nil
	}
	return []step{{node: t.event(KindUnresolved, ssaDescription(it.v), "origin not followed for this kind of value", it.v.Parent(), t.valuePos(it.v, false)),
		kind: EdgeDerived, conf: evidence.Unresolved, stop: true}}
}

// paramIndex maps a call's arguments to a callee's parameters.
func argForParam(c *ssa.CallCommon, callee *ssa.Function, idx int) ssa.Value {
	if c.IsInvoke() {
		if idx == 0 {
			return c.Value
		}
		if idx-1 < len(c.Args) {
			return c.Args[idx-1]
		}
		return nil
	}
	shift := 0
	if len(callee.Params) == len(c.Args)+1 {
		shift = 1 // bound method: the receiver is captured, not passed
		if idx == 0 {
			if mc, ok := c.Value.(*ssa.MakeClosure); ok && len(mc.Bindings) > 0 {
				return mc.Bindings[0]
			}
			return nil
		}
	}
	if j := idx - shift; j >= 0 && j < len(c.Args) {
		return c.Args[j]
	}
	return nil
}

func paramIndex(fn *ssa.Function, prm *ssa.Parameter) int {
	for i, p := range fn.Params {
		if p == prm {
			return i
		}
	}
	return -1
}

func (t *tracer) paramPreds(prm *ssa.Parameter, it item) []step {
	fn := prm.Parent()
	idx := paramIndex(fn, prm)
	if src, ok := httpParamSource(prm); ok {
		n := t.event(KindSource, src, "parameter of an HTTP handler", fn, prm.Pos(), "http-input")
		return []step{{node: n, kind: EdgeSource, conf: evidence.Exact, stop: true}}
	}
	var out []step
	sites := t.p.CallersOf(fn)
	if top := it.top(); top != nil {
		// We came down from this call: only its argument flows here.
		file, line, _ := t.p.Pos(top.Pos())
		sites = []flow.CallerSite{{Caller: top.Parent(), Instr: top, File: file, Line: line, Conf: flow.Confirmed}}
	}
	for _, s := range sites {
		if s.Callback {
			out = append(out, step{node: t.event(KindUnresolved, "called through a function value",
				"passed to "+calleeName(s.Instr.Common())+"; the code that calls it and its arguments are outside the analysis", s.Caller, s.Instr.Pos()),
				kind: EdgeArg, conf: evidence.Unresolved, stop: true})
			continue
		}
		a := argForParam(s.Instr.Common(), fn, idx)
		if a == nil {
			continue
		}
		out = append(out, step{v: a, kind: EdgeArg, conf: confOf(s.Conf), pos: s.Instr.Pos(), leave: true,
			label: fmt.Sprintf("argument of %s at %s:%d", flow.ShortName(fn), s.File, s.Line)})
	}
	if len(out) == 0 {
		n := t.event(KindUnresolved, "no callers of "+flow.ShortName(fn)+" in the module",
			"an entry point, a test target, or called by code outside the module", fn, prm.Pos())
		out = append(out, step{node: n, kind: EdgeArg, conf: evidence.Unresolved, stop: true})
	}
	return out
}

func confOf(flowConf string) string {
	switch flowConf {
	case flow.Confirmed:
		return evidence.Exact
	case flow.Likely:
		return evidence.Likely
	case flow.Possible:
		return evidence.Possible
	}
	return evidence.Unresolved
}

// httpParamSource recognises the *http.Request parameter of a handler.
func httpParamSource(prm *ssa.Parameter) (string, bool) {
	if isRequestType(prm.Type()) {
		return "HTTP request (" + prm.Name() + ")", true
	}
	return "", false
}

func isRequestType(t types.Type) bool {
	s := types.TypeString(t, nil)
	return s == "*net/http.Request" || s == "net/http.Request"
}

func (t *tracer) freeVarPreds(fv *ssa.FreeVar) []step {
	fn := fv.Parent()
	idx := -1
	for i, f := range fn.FreeVars {
		if f == fv {
			idx = i
		}
	}
	var out []step
	if par := fn.Parent(); par != nil && idx >= 0 {
		for _, b := range par.Blocks {
			for _, ins := range b.Instrs {
				if mc, ok := ins.(*ssa.MakeClosure); ok && mc.Fn == fn && idx < len(mc.Bindings) {
					out = append(out, step{v: mc.Bindings[idx], kind: EdgeCapture, conf: evidence.Exact, label: "captured by closure", pos: mc.Pos(), cross: true})
				}
			}
		}
	}
	return out
}

// callPreds explains a call result: the callee's returns for module code,
// the call itself as a source for known inputs, or a derived value.
func (t *tracer) callPreds(call *ssa.Call, c *ssa.CallCommon, idx int, it item) []step {
	if src, ok := t.inputSource(c); ok {
		return []step{{node: t.event(KindSource, src.label, src.detail, call.Parent(), call.Pos(), src.tags...), kind: EdgeSource, conf: evidence.Exact, stop: true}}
	}
	var out []step
	resultIdx := max(idx, 0)
	for _, ce := range t.p.CalleesOf(call) {
		if ce.Fn != nil && t.p.InModule(ce.Fn) {
			callee := flow.Origin(ce.Fn)
			for _, b := range callee.Blocks {
				for _, ins := range b.Instrs {
					if ret, ok := ins.(*ssa.Return); ok && resultIdx < len(ret.Results) {
						out = append(out, step{v: ret.Results[resultIdx], kind: EdgeReturn, conf: confOf(ce.Conf), pos: ret.Pos(),
							ctx: call, enter: true, label: "returned by " + flow.ShortName(callee)})
					}
				}
			}
		}
	}
	if len(out) > 0 {
		return out
	}
	// Outside the module (or unresolved): the result is derived from the
	// arguments as far as the analysis can tell.
	name := calleeName(c)
	conf := evidence.Derived
	if c.StaticCallee() == nil && !c.IsInvoke() {
		conf = evidence.Unresolved
	}
	if c.IsInvoke() {
		out = append(out, step{v: c.Value, kind: EdgeDerived, conf: conf, label: "receiver of " + name, pos: call.Pos(), fromCtx: true})
	}
	// Literal arguments next to data arguments configure the call (a base,
	// a bit size, a format) rather than carry data: they are not origins.
	data := 0
	for _, a := range c.Args {
		if _, isConst := a.(*ssa.Const); !isConst && !isContext(a.Type()) {
			data++
		}
	}
	for _, a := range c.Args {
		if _, isConst := a.(*ssa.Const); isContext(a.Type()) || isConst && data > 0 {
			continue
		}
		out = append(out, step{v: a, kind: EdgeDerived, conf: conf, label: "via " + name, pos: call.Pos(), fromCtx: true})
	}
	if len(out) == 0 {
		out = append(out, step{node: t.event(KindExternal, name+"(…)", "result of a call with no traceable inputs", call.Parent(), call.Pos()),
			kind: EdgeDerived, conf: conf, stop: true})
	}
	return out
}

func isContext(t types.Type) bool { return types.TypeString(t, nil) == "context.Context" }

func calleeName(c *ssa.CallCommon) string {
	if c.IsInvoke() {
		return types.TypeString(c.Value.Type(), qualifier) + "." + c.Method.Name()
	}
	if fn := c.StaticCallee(); fn != nil {
		return flow.ShortName(flow.Unwrap(fn))
	}
	if b, ok := c.Value.(*ssa.Builtin); ok {
		return b.Name()
	}
	return "function value"
}

func calleeFullName(c *ssa.CallCommon) string {
	if c.IsInvoke() {
		return c.Method.FullName()
	}
	if fn := c.StaticCallee(); fn != nil {
		if obj, ok := fn.Object().(*types.Func); ok {
			return obj.FullName()
		}
		return fn.String()
	}
	return ""
}

type sourceKind struct {
	what     string
	keyArg   int  // index (in the API's own parameters) of the key argument, -1 if none
	needsReq bool // the receiver must come from an *http.Request
}

var inputAPIs = map[string]sourceKind{
	"github.com/go-chi/chi/v5.URLParam":                {"path parameter", 1, false},
	"github.com/go-chi/chi.URLParam":                   {"path parameter", 1, false},
	"(*net/http.Request).FormValue":                    {"form value", 0, false},
	"(*net/http.Request).PostFormValue":                {"form value", 0, false},
	"(*net/http.Request).PathValue":                    {"path parameter", 0, false},
	"(*net/http.Request).Cookie":                       {"cookie", 0, false},
	"(*net/http.Request).UserAgent":                    {"User-Agent header", -1, false},
	"(*net/http.Request).Referer":                      {"Referer header", -1, false},
	"(net/url.Values).Get":                             {"query parameter", 0, true},
	"(net/http.Header).Get":                            {"request header", 0, true},
	"(*github.com/gin-gonic/gin.Context).Param":        {"path parameter", 0, false},
	"(*github.com/gin-gonic/gin.Context).Query":        {"query parameter", 0, false},
	"(*github.com/gin-gonic/gin.Context).DefaultQuery": {"query parameter", 0, false},
	"(*github.com/gin-gonic/gin.Context).PostForm":     {"form value", 0, false},
	"(*github.com/gin-gonic/gin.Context).GetHeader":    {"request header", 0, false},
	"(github.com/labstack/echo/v4.Context).Param":      {"path parameter", 0, false},
	"(github.com/labstack/echo/v4.Context).QueryParam": {"query parameter", 0, false},
	"(github.com/labstack/echo/v4.Context).FormValue":  {"form value", 0, false},
	"os.Getenv":    {"environment variable", 0, false},
	"os.LookupEnv": {"environment variable", 0, false},
}

type source struct {
	label, detail string
	tags          []string
}

// inputSource recognises calls that read external input.
func (t *tracer) inputSource(c *ssa.CallCommon) (source, bool) {
	name := calleeFullName(c)
	k, ok := inputAPIs[name]
	if !ok {
		return source{}, false
	}
	args := c.Args
	var recv ssa.Value
	if c.IsInvoke() {
		recv = c.Value
	} else if fn := c.StaticCallee(); fn != nil && fn.Signature.Recv() != nil && len(args) > 0 {
		recv, args = args[0], args[1:]
	}
	if k.needsReq && (recv == nil || !fromRequest(recv, 0)) {
		return source{}, false
	}
	label := k.what
	if k.keyArg >= 0 && k.keyArg < len(args) {
		if kc, ok := args[k.keyArg].(*ssa.Const); ok && kc.Value != nil && kc.Value.Kind() == constant.String {
			label += " " + strconv.Quote(constant.StringVal(kc.Value))
		}
	}
	tag := "http-input"
	if strings.HasPrefix(name, "os.") {
		tag = "environment"
	} else {
		label = "HTTP " + label
	}
	return source{label: label, detail: "read by " + calleeName(c), tags: []string{tag}}, true
}

// fromRequest reports whether v is derived from an *http.Request through
// field reads and method calls.
func fromRequest(v ssa.Value, depth int) bool {
	if v == nil || depth > 6 {
		return false
	}
	if isRequestType(v.Type()) {
		return true
	}
	switch x := v.(type) {
	case *ssa.Call:
		c := x.Common()
		if c.IsInvoke() {
			return fromRequest(c.Value, depth+1)
		}
		for _, a := range c.Args {
			if fromRequest(a, depth+1) {
				return true
			}
		}
	case *ssa.UnOp:
		return fromRequest(x.X, depth+1)
	case *ssa.FieldAddr:
		return fromRequest(x.X, depth+1)
	case *ssa.Field:
		return fromRequest(x.X, depth+1)
	case *ssa.Extract:
		return fromRequest(x.Tuple, depth+1)
	case *ssa.ChangeType:
		return fromRequest(x.X, depth+1)
	case *ssa.MakeInterface:
		return fromRequest(x.X, depth+1)
	}
	return false
}

func (t *tracer) chanPreds(recv *ssa.UnOp) []step {
	var out []step
	fn := recv.Parent()
	for _, b := range fn.Blocks {
		for _, ins := range b.Instrs {
			if s, ok := ins.(*ssa.Send); ok && s.Chan == recv.X {
				out = append(out, step{v: s.X, kind: EdgeDerived, conf: evidence.Possible, label: "sent on the same channel", pos: s.Pos(), fromCtx: true})
			}
		}
	}
	out = append(out, step{node: t.event(KindUnresolved, "channel receive", "sends from other functions or goroutines are not tracked", fn, recv.Pos()),
		kind: EdgeDerived, conf: evidence.Unresolved, stop: true})
	return out
}

// --- destination (forwards) ------------------------------------------------------

func (t *tracer) succs(it item) []step {
	if it.mem {
		return t.memSuccs(it.v, it.store)
	}
	v := it.v
	refs := v.Referrers()
	if refs == nil {
		return nil
	}
	var out []step
	for _, r := range *refs {
		switch x := r.(type) {
		case *ssa.DebugRef:
		case ssa.CallInstruction:
			out = append(out, t.argSuccs(x, v, it)...)
		case *ssa.Store:
			if x.Val != v {
				continue // v is the address being written: not a flow of v
			}
			if call, k, ok := varargOf(x); ok {
				out = append(out, t.argSuccsAt(call, v, -1, k, it)...)
				continue
			}
			out = append(out, step{v: x.Addr, mem: true, store: x, kind: EdgeStore, conf: evidence.Exact, label: "stored", pos: x.Pos(), fromCtx: true})
		case *ssa.Phi:
			out = append(out, step{v: x, kind: EdgeMerge, conf: evidence.Exact, fromCtx: true})
		case *ssa.BinOp:
			if isComparison(x.Op) {
				out = append(out, t.checkSteps(x, x)...)
				continue
			}
			kind := EdgeDerived
			if b, ok := x.Type().Underlying().(*types.Basic); ok && b.Info()&types.IsString != 0 && x.Op == token.ADD {
				kind = EdgeConcat
			}
			out = append(out, step{v: x, kind: kind, conf: evidence.Derived, label: x.Op.String(), pos: x.Pos(), fromCtx: true})
		case *ssa.UnOp:
			switch x.Op {
			case token.MUL:
				out = append(out, step{v: x, kind: EdgeLoad, conf: evidence.Derived, label: "dereference", pos: x.Pos(), fromCtx: true})
			case token.NOT:
				out = append(out, t.checkSteps(x, x)...)
			case token.ARROW:
			default:
				out = append(out, step{v: x, kind: EdgeDerived, conf: evidence.Derived, label: x.Op.String(), pos: x.Pos(), fromCtx: true})
			}
		case *ssa.Convert:
			out = append(out, step{v: x, kind: EdgeConvert, conf: evidence.Exact, label: "conversion", pos: x.Pos(), fromCtx: true})
		case *ssa.ChangeType, *ssa.ChangeInterface, *ssa.SliceToArrayPointer, *ssa.MultiConvert:
			out = append(out, step{v: x.(ssa.Value), kind: EdgeConvert, conf: evidence.Exact, fromCtx: true})
		case *ssa.MakeInterface:
			out = append(out, step{v: x, kind: EdgeConvert, conf: evidence.Exact, label: "to " + types.TypeString(x.Type(), qualifier), fromCtx: true})
		case *ssa.TypeAssert:
			out = append(out, step{v: x, kind: EdgeConvert, conf: evidence.Exact, label: "type assertion", pos: x.Pos(), fromCtx: true})
		case *ssa.Field:
			out = append(out, step{v: x, kind: EdgeDerived, conf: evidence.Derived, label: "field " + fieldName(v.Type(), x.Field), pos: x.Pos(), fromCtx: true})
		case *ssa.FieldAddr:
			if x.X == v {
				out = append(out, step{v: x, mem: true, kind: EdgeDerived, conf: evidence.Derived, label: "field " + fieldName(v.Type(), x.Field), pos: x.Pos(), fromCtx: true})
			}
		case *ssa.Index, *ssa.Lookup, *ssa.Slice, *ssa.Range, *ssa.Next:
			out = append(out, step{v: x.(ssa.Value), kind: EdgeDerived, conf: evidence.Derived, label: "element", pos: x.Pos(), fromCtx: true})
		case *ssa.Extract:
			out = append(out, step{v: x, kind: EdgeDerived, conf: evidence.Exact, label: fmt.Sprintf("result #%d", x.Index), fromCtx: true})
		case *ssa.Return:
			for i, res := range x.Results {
				if res == v {
					out = append(out, t.returnSteps(it, x, i)...)
				}
			}
		case *ssa.If:
			out = append(out, t.checkSteps(v, nil)...)
		case *ssa.MakeClosure:
			for i, b := range x.Bindings {
				if b == v {
					if cl, ok := x.Fn.(*ssa.Function); ok && i < len(cl.FreeVars) {
						out = append(out, step{v: cl.FreeVars[i], kind: EdgeCapture, conf: evidence.Exact, label: "captured by closure", pos: x.Pos(), cross: true})
					}
				}
			}
		case *ssa.MapUpdate:
			if x.Value == v || x.Key == v {
				out = append(out, step{node: t.event(KindUnresolved, "stored in a map", "map contents are not tracked", x.Parent(), x.Pos()),
					kind: EdgeStore, conf: evidence.Unresolved, stop: true})
			}
		case *ssa.Send:
			if x.X == v {
				out = append(out, step{node: t.event(KindUnresolved, "sent on a channel", "receivers in other goroutines are not tracked", x.Parent(), x.Pos()),
					kind: EdgeSink, conf: evidence.Unresolved, stop: true})
			}
		case *ssa.Panic:
			out = append(out, step{node: t.event(KindExternal, "panic value", "", x.Parent(), x.Pos()), kind: EdgeSink, conf: evidence.Exact, stop: true})
		}
	}
	return out
}

func isComparison(op token.Token) bool {
	switch op {
	case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
		return true
	}
	return false
}

// checkSteps records a condition derived from the traced value: if it
// decides a branch it is a validation; otherwise the boolean flows on.
func (t *tracer) checkSteps(cond ssa.Value, expr ssa.Value) []step {
	decides := false
	if refs := cond.Referrers(); refs != nil {
		for _, r := range *refs {
			if _, ok := r.(*ssa.If); ok {
				decides = true
			}
		}
	}
	if !decides {
		if expr != nil {
			return []step{{v: cond, kind: EdgeDerived, conf: evidence.Derived, label: "condition", pos: cond.Pos(), fromCtx: true}}
		}
		return nil
	}
	text := t.exprText(cond)
	kind := "comparison"
	if _, ok := cond.(*ssa.Call); ok {
		kind = "predicate"
	}
	if text == "" {
		text = ssaDescription(cond)
	}
	if b, ok := cond.(*ssa.BinOp); ok {
		if k, ok := b.Y.(*ssa.Const); ok && k.IsNil() && isErrorType(b.X.Type()) {
			kind = "error check"
		}
	}
	n := t.event(KindCheck, oneLine(text, 80), kind, cond.Parent(), t.valuePos(cond, false), "validation")
	return []step{{node: n, kind: EdgeSink, conf: evidence.Exact, label: "decides a branch", stop: true}}
}

var errorIface = types.Universe.Lookup("error").Type().Underlying().(*types.Interface)

func isErrorType(t types.Type) bool {
	return t != nil && types.Implements(t, errorIface) && types.IsInterface(t)
}

// returnSteps follows a returned value to the call sites that receive it.
func (t *tracer) returnSteps(it item, ret *ssa.Return, idx int) []step {
	fn := ret.Parent()
	sites := []flow.CallerSite{}
	conf := evidence.Exact
	if top := it.top(); top != nil {
		sites = append(sites, flow.CallerSite{Caller: top.Parent(), Instr: top, Conf: flow.Confirmed})
	} else {
		for _, s := range t.p.CallersOf(fn) {
			if !s.Callback {
				sites = append(sites, s)
			}
		}
	}
	var out []step
	for _, s := range sites {
		call, ok := s.Instr.(*ssa.Call)
		if !ok {
			continue // go/defer discard results
		}
		c := confOf(s.Conf)
		if it.top() != nil {
			c = conf
		}
		label := "returned to " + flow.ShortName(flow.Origin(s.Caller))
		if fn.Signature.Results().Len() == 1 {
			out = append(out, step{v: call, kind: EdgeReturn, conf: c, label: label, pos: ret.Pos(), leave: true})
			continue
		}
		if refs := call.Referrers(); refs != nil {
			for _, r := range *refs {
				if ex, ok := r.(*ssa.Extract); ok && ex.Index == idx {
					out = append(out, step{v: ex, kind: EdgeReturn, conf: c, label: label, pos: ret.Pos(), leave: true})
				}
			}
		}
	}
	if len(out) == 0 {
		what := "no callers in the module"
		if fn.Parent() != nil {
			what = "closure called from code the analysis does not resolve"
		}
		out = append(out, step{node: t.event(KindReturn, "returned by "+flow.ShortName(fn), what, fn, ret.Pos()),
			kind: EdgeReturn, conf: evidence.Exact, stop: true})
	}
	return out
}

// varargOf recognises a store into the array built for a variadic call
// (f(a, b...) packs b into a slice): it returns the call and the element.
func varargOf(st *ssa.Store) (ssa.CallInstruction, int, bool) {
	ia, ok := st.Addr.(*ssa.IndexAddr)
	if !ok {
		return nil, 0, false
	}
	alloc, ok := ia.X.(*ssa.Alloc)
	if !ok {
		return nil, 0, false
	}
	idx := -1
	if k, ok := ia.Index.(*ssa.Const); ok && k.Value != nil {
		if n, exact := constant.Int64Val(k.Value); exact {
			idx = int(n)
		}
	}
	for _, r := range *alloc.Referrers() {
		sl, ok := r.(*ssa.Slice)
		if !ok || sl.X != alloc {
			continue
		}
		for _, r2 := range *sl.Referrers() {
			if ci, ok := r2.(ssa.CallInstruction); ok {
				c := ci.Common()
				if len(c.Args) > 0 && c.Args[len(c.Args)-1] == sl && c.Signature().Variadic() {
					return ci, idx, true
				}
			}
		}
	}
	return nil, 0, false
}

// argSuccs follows v into the calls that receive it.
func (t *tracer) argSuccs(ci ssa.CallInstruction, v ssa.Value, it item) []step {
	c := ci.Common()
	var out []step
	if c.IsInvoke() && c.Value == v {
		out = append(out, t.argSuccsAt(ci, v, -1, -1, it)...) // receiver
	}
	for i, a := range c.Args {
		if a == v {
			out = append(out, t.argSuccsAt(ci, v, i, -1, it)...)
		}
	}
	if c.Value == v && !c.IsInvoke() {
		if _, isFn := v.(*ssa.Function); !isFn {
			out = append(out, step{node: t.event(KindUnresolved, "called as a function value", "", ci.Parent(), ci.Pos()), kind: EdgeSink, conf: evidence.Unresolved, stop: true})
		}
	}
	return out
}

// argSuccsAt handles v passed as argument argIdx (index in c.Args; -1 for
// the receiver of an interface call) or as element k of the variadic slice.
func (t *tracer) argSuccsAt(ci ssa.CallInstruction, v ssa.Value, argIdx, k int, it item) []step {
	c := ci.Common()
	if k >= 0 {
		argIdx = len(c.Args) - 1
	}
	var out []step
	// Database operations: SQL text or a query parameter.
	for _, ce := range t.p.CalleesOf(ci) {
		if ce.DB == nil {
			continue
		}
		sqlIdx := flow.SQLArgIndex(c)
		var n *Node
		switch {
		case argIdx == sqlIdx && sqlIdx >= 0:
			what := "SQL text of " + ce.DB.Lib + " " + ce.DB.Op
			n = t.event(KindSQL, what, "the value is part of the SQL statement itself", ci.Parent(), ci.Pos(), "sql-text")
		case sqlIdx >= 0 && argIdx > sqlIdx:
			ph := argIdx - sqlIdx
			if k >= 0 {
				ph = argIdx - sqlIdx + k
			}
			label := fmt.Sprintf("SQL parameter $%d of %s %s", ph, ce.DB.Lib, ce.DB.Op)
			detail := ""
			if ce.DB.SQL != "" {
				detail = oneLine(ce.DB.SQL, 120)
			} else if ce.DB.Dynamic {
				detail = "dynamic SQL"
			}
			n = t.event(KindSQL, label, detail, ci.Parent(), ci.Pos(), "sql-param")
		default:
			continue
		}
		out = append(out, step{node: n, kind: EdgeSink, conf: confOf(ce.Conf), pos: ci.Pos(), stop: true})
		return out
	}
	targets := t.p.CalleesOf(ci)
	followed := false
	for _, ce := range targets {
		if ce.Fn == nil || !t.p.InModule(ce.Fn) {
			continue
		}
		callee := flow.Origin(ce.Fn)
		var prm ssa.Value
		switch {
		case ce.Note != "" && strings.HasPrefix(ce.Note, "function value passed to"):
			continue // callback: v is not its argument
		case argIdx < 0:
			if len(callee.Params) > 0 {
				prm = callee.Params[0]
			}
		case c.IsInvoke():
			if argIdx+1 < len(callee.Params) {
				prm = callee.Params[argIdx+1]
			}
		default:
			shift := 0
			if len(callee.Params) == len(c.Args)+1 {
				shift = 1
			}
			if argIdx+shift < len(callee.Params) {
				prm = callee.Params[argIdx+shift]
			}
		}
		if prm == nil {
			continue
		}
		followed = true
		label := fmt.Sprintf("argument to %s", flow.ShortName(callee))
		if p, ok := prm.(*ssa.Parameter); ok {
			label += " (param " + p.Name() + ")"
		}
		if k >= 0 {
			// Variadic element: the callee receives the slice.
			label += fmt.Sprintf(", variadic element %d", k)
		}
		out = append(out, step{v: prm, kind: EdgeArg, conf: confOf(ce.Conf), label: label, pos: ci.Pos(), ctx: ci, enter: true})
	}
	if followed {
		return out
	}
	// Outside the module: the call receives the value, and its results are
	// derived from it as far as the analysis can tell.
	name := calleeName(c)
	conf := evidence.Derived
	builtin, isBuiltin := c.Value.(*ssa.Builtin)
	call, isCall := ci.(*ssa.Call)
	switch {
	case isBuiltin:
		if builtin.Name() != "append" {
			return out // len, cap, copy, delete...: the value does not flow on
		}
		if isCall {
			out = append(out, step{v: call, kind: EdgeDerived, conf: evidence.Exact, label: "appended", pos: call.Pos(), fromCtx: true})
		}
		return out
	case c.StaticCallee() == nil && !c.IsInvoke():
		out = append(out, step{node: t.event(KindUnresolved, "passed to a function value", "the called function is not known statically", ci.Parent(), ci.Pos()),
			kind: EdgeSink, conf: evidence.Unresolved, pos: ci.Pos(), stop: true})
		return out
	}
	ext := t.event(KindExternal, "passed to "+name, "", ci.Parent(), ci.Pos())
	out = append(out, step{node: ext, kind: EdgeSink, conf: evidence.Exact, pos: ci.Pos(), stop: true})
	if !isCall {
		return out
	}
	sig := c.Signature()
	switch sig.Results().Len() {
	case 0:
	case 1:
		if !isErrorType(sig.Results().At(0).Type()) {
			out = append(out, step{v: call, kind: EdgeDerived, conf: conf, label: "via " + name, pos: call.Pos(), fromCtx: true})
		} else {
			out = append(out, t.errorChecks(call, name)...)
		}
	default:
		if refs := call.Referrers(); refs != nil {
			for _, r := range *refs {
				ex, ok := r.(*ssa.Extract)
				if !ok {
					continue
				}
				if isErrorType(ex.Type()) {
					out = append(out, t.errorChecks(ex, name)...)
					continue
				}
				out = append(out, step{v: ex, kind: EdgeDerived, conf: conf, label: "via " + name, pos: call.Pos(), fromCtx: true})
			}
		}
	}
	return out
}

// errorChecks records `if err != nil` checks on the error a call returns.
func (t *tracer) errorChecks(errVal ssa.Value, name string) []step {
	var out []step
	refs := errVal.Referrers()
	if refs == nil {
		return nil
	}
	for _, r := range *refs {
		b, ok := r.(*ssa.BinOp)
		if !ok || (b.Op != token.NEQ && b.Op != token.EQL) {
			continue
		}
		decides := false
		if br := b.Referrers(); br != nil {
			for _, x := range *br {
				if _, ok := x.(*ssa.If); ok {
					decides = true
				}
			}
		}
		if !decides {
			continue
		}
		text := t.exprText(b)
		n := t.event(KindCheck, oneLine(text, 80), "error check", b.Parent(), t.valuePos(b, false), "validation")
		n.Detail = "error check"
		n.Tags = append(n.Tags, "error from "+name)
		out = append(out, step{node: n, kind: EdgeSink, conf: evidence.Exact, label: "error of " + name + " checked", stop: true})
	}
	return out
}

// --- memory -----------------------------------------------------------------------

// loc abstracts an address: a base value and a field/element path.
type loc struct {
	base ssa.Value
	path string
}

func locOf(addr ssa.Value) loc {
	path := ""
	for i := 0; i < 10; i++ {
		switch a := addr.(type) {
		case *ssa.FieldAddr:
			path = "." + fieldName(a.X.Type(), a.Field) + path
			addr = a.X
			continue
		case *ssa.IndexAddr:
			path = "[*]" + path
			addr = a.X
			continue
		}
		break
	}
	return loc{addr, path}
}

// related reports how a store at s relates to a load at l: same location,
// s is a prefix of l (whole object written) or l is a prefix of s.
func related(s, l loc) (bool, string) {
	if s.base != l.base {
		return false, ""
	}
	switch {
	case s.path == l.path:
		return true, evidence.Exact
	case strings.HasPrefix(l.path, s.path):
		return true, evidence.Derived
	case strings.HasPrefix(s.path, l.path):
		return true, evidence.Derived
	}
	return false, ""
}

// memScopes returns the functions where a location's base is visible:
// the declaring function and the closures that capture it.
func (t *tracer) memScopes(base ssa.Value) ([]*ssa.Function, map[ssa.Value]ssa.Value, bool) {
	alias := map[ssa.Value]ssa.Value{base: base}
	switch b := base.(type) {
	case *ssa.Alloc:
		fns := []*ssa.Function{b.Parent()}
		for _, r := range *b.Referrers() {
			if mc, ok := r.(*ssa.MakeClosure); ok {
				if cl, ok := mc.Fn.(*ssa.Function); ok {
					for i, bd := range mc.Bindings {
						if bd == b && i < len(cl.FreeVars) {
							alias[cl.FreeVars[i]] = base
							fns = append(fns, cl)
						}
					}
				}
			}
		}
		return fns, alias, true
	case *ssa.FreeVar:
		fn := b.Parent()
		fns := []*ssa.Function{fn}
		if par := fn.Parent(); par != nil {
			idx := -1
			for i, f := range fn.FreeVars {
				if f == b {
					idx = i
				}
			}
			for _, blk := range par.Blocks {
				for _, ins := range blk.Instrs {
					if mc, ok := ins.(*ssa.MakeClosure); ok && mc.Fn == fn && idx >= 0 && idx < len(mc.Bindings) {
						alias[mc.Bindings[idx]] = b
						fns = append(fns, par)
					}
				}
			}
		}
		return fns, alias, true
	case *ssa.Global:
		return t.p.Funcs(), alias, false
	}
	var fns []*ssa.Function
	if base.Parent() != nil {
		fns = append(fns, base.Parent())
	}
	return fns, alias, false
}

func normLoc(l loc, alias map[ssa.Value]ssa.Value) loc {
	if a, ok := alias[l.base]; ok {
		l.base = a
	}
	return l
}

// escapes reports whether an allocation's address leaves the instructions
// that only read and write it.
func escapes(base ssa.Value) bool {
	alloc, ok := base.(*ssa.Alloc)
	if !ok {
		return true
	}
	var visit func(v ssa.Value, depth int) bool
	visit = func(v ssa.Value, depth int) bool {
		refs := v.Referrers()
		if refs == nil || depth > 6 {
			return false
		}
		for _, r := range *refs {
			switch x := r.(type) {
			case *ssa.DebugRef, *ssa.MakeClosure:
				// Closures capturing the variable are searched like the
				// declaring function (memScopes).
			case *ssa.UnOp:
			case *ssa.Store:
				if x.Val == v {
					return true
				}
			case *ssa.FieldAddr:
				if visit(x, depth+1) {
					return true
				}
			case *ssa.IndexAddr:
				if visit(x, depth+1) {
					return true
				}
			default:
				return true
			}
		}
		return false
	}
	return visit(alloc, 0)
}

func (t *tracer) reaches(from, to ssa.Instruction) bool {
	bf, bt := from.Block(), to.Block()
	if bf.Parent() != bt.Parent() {
		return true // different functions (closure and parent): order unknown
	}
	if bf == bt && index(from) < index(to) {
		return true
	}
	// Later in the same block, or another block: through the CFG (a loop
	// back edge makes a block reach itself).
	return t.blockReach(bf)[bt]
}

// blockReach returns the blocks reachable from b's successors.
func (t *tracer) blockReach(b *ssa.BasicBlock) map[*ssa.BasicBlock]bool {
	fi := t.info(b.Parent())
	if fi.reach == nil {
		fi.reach = map[*ssa.BasicBlock]map[*ssa.BasicBlock]bool{}
	}
	if r, ok := fi.reach[b]; ok {
		return r
	}
	r := map[*ssa.BasicBlock]bool{}
	queue := append([]*ssa.BasicBlock(nil), b.Succs...)
	for len(queue) > 0 {
		x := queue[0]
		queue = queue[1:]
		if r[x] {
			continue
		}
		r[x] = true
		queue = append(queue, x.Succs...)
	}
	fi.reach[b] = r
	return r
}

func index(ins ssa.Instruction) int {
	for i, x := range ins.Block().Instrs {
		if x == ins {
			return i
		}
	}
	return -1
}

// killed reports whether another store to the same location certainly
// overwrites st before the load: in st's block after st, or in the load's
// block before the load.
func killed(st *ssa.Store, load ssa.Instruction, l loc, alias map[ssa.Value]ssa.Value) bool {
	check := func(b *ssa.BasicBlock, from, to int) bool {
		for i := from; i < to && i < len(b.Instrs); i++ {
			if s2, ok := b.Instrs[i].(*ssa.Store); ok && s2 != st && normLoc(locOf(s2.Addr), alias) == l {
				return true
			}
		}
		return false
	}
	if st.Block() == load.Block() && index(st) < index(load) {
		return check(st.Block(), index(st)+1, index(load))
	}
	return check(st.Block(), index(st)+1, len(st.Block().Instrs)) || check(load.Block(), 0, index(load))
}

// memPreds finds the writes that can reach a read of addr (load is nil
// when the whole variable is traced, without a particular read).
func (t *tracer) memPreds(addr ssa.Value, load ssa.Value) []step {
	l0 := locOf(addr)
	fns, alias, local := t.memScopes(l0.base)
	l := normLoc(l0, alias)
	esc := escapes(l.base)
	var loadIns ssa.Instruction
	at := addr.Pos()
	if load != nil {
		loadIns, _ = load.(ssa.Instruction)
		at = load.Pos()
	}
	var stores []step
	for _, fn := range fns {
		for _, b := range fn.Blocks {
			for _, ins := range b.Instrs {
				st, ok := ins.(*ssa.Store)
				if !ok {
					continue
				}
				rel, conf := related(normLoc(locOf(st.Addr), alias), l)
				if !rel {
					continue
				}
				if loadIns != nil && st.Parent() == loadIns.Parent() && (!t.reaches(st, loadIns) || killed(st, loadIns, l, alias)) {
					continue
				}
				if esc || !local {
					conf = weaker(conf, evidence.Possible)
				}
				stores = append(stores, step{v: st.Val, kind: EdgeStore, conf: conf, label: "written at line " + lineOf(t.p, st.Pos()), pos: st.Pos(), fromCtx: st.Parent() == addr.Parent()})
			}
		}
	}
	if len(stores) > 1 {
		for i := range stores {
			if stores[i].conf == evidence.Exact {
				stores[i].conf = evidence.Possible // several writes may reach: some may be overwritten
			}
		}
	}
	// Calls that receive the address (or the object containing it) may write it.
	for _, w := range t.writers(l, fns, alias) {
		stores = append(stores, w)
	}
	switch b := l.base.(type) {
	case *ssa.Parameter:
		stores = append(stores, step{node: t.event(KindUnresolved, "memory reachable from param "+b.Name()+strings.TrimPrefix(l.path, ""),
			"the caller may have written it before the call; writes outside this function are not tracked", b.Parent(), b.Pos()),
			kind: EdgeLoad, conf: evidence.Unresolved, stop: true})
	case *ssa.Global:
		if len(stores) == 0 {
			stores = append(stores, step{node: t.event(KindGlobal, "package variable "+b.Name(), "initialised outside the functions analysed", nil, b.Pos()),
				kind: EdgeLoad, conf: evidence.Possible, stop: true})
		}
	}
	if len(stores) == 0 {
		stores = append(stores, step{node: t.event(KindUnresolved, "no write found", "the memory is written where the analysis cannot see it", addr.Parent(), at),
			kind: EdgeLoad, conf: evidence.Unresolved, stop: true})
	}
	return stores
}

// lastLine returns the last source line executed in a block.
func lastLine(p *flow.Program, b *ssa.BasicBlock) int {
	for i := len(b.Instrs) - 1; i >= 0; i-- {
		if pos := b.Instrs[i].Pos(); pos.IsValid() {
			_, l, _ := p.Pos(pos)
			return l
		}
	}
	return 0
}

func lineOf(p *flow.Program, pos token.Pos) string {
	_, l, _ := p.Pos(pos)
	return strconv.Itoa(l)
}

// writers lists calls that receive the address of the location (or of the
// object containing it), which may write it.
func (t *tracer) writers(l loc, fns []*ssa.Function, alias map[ssa.Value]ssa.Value) []step {
	var out []step
	seen := map[ssa.Instruction]bool{}
	for _, fn := range fns {
		for _, b := range fn.Blocks {
			for _, ins := range b.Instrs {
				ci, ok := ins.(ssa.CallInstruction)
				if !ok || seen[ins] {
					continue
				}
				for _, a := range callAddrArgs(ci) {
					if rel, _ := related(normLoc(locOf(a), alias), l); !rel {
						continue
					}
					seen[ins] = true
					out = append(out, t.writerStep(ci, a))
					break
				}
			}
		}
	}
	return out
}

// callAddrArgs returns the pointer arguments of a call, unpacking variadic
// slices and interface conversions (Scan(&c.ID, &c.Name)).
func callAddrArgs(ci ssa.CallInstruction) []ssa.Value {
	var out []ssa.Value
	var add func(v ssa.Value, depth int)
	add = func(v ssa.Value, depth int) {
		if depth > 3 {
			return
		}
		switch x := v.(type) {
		case *ssa.MakeInterface:
			add(x.X, depth+1)
		case *ssa.ChangeType:
			add(x.X, depth+1)
		case *ssa.Slice:
			if alloc, ok := x.X.(*ssa.Alloc); ok {
				for _, r := range *alloc.Referrers() {
					if ia, ok := r.(*ssa.IndexAddr); ok {
						for _, r2 := range *ia.Referrers() {
							if st, ok := r2.(*ssa.Store); ok && st.Addr == ia {
								add(st.Val, depth+1)
							}
						}
					}
				}
			}
		default:
			if _, isPtr := v.Type().Underlying().(*types.Pointer); isPtr {
				out = append(out, v)
			}
		}
	}
	for _, a := range ci.Common().Args {
		add(a, 0)
	}
	return out
}

func (t *tracer) writerStep(ci ssa.CallInstruction, addr ssa.Value) step {
	c := ci.Common()
	name := calleeName(c)
	full := calleeFullName(c)
	method := ""
	if c.IsInvoke() {
		method = c.Method.Name()
	} else if fn := c.StaticCallee(); fn != nil {
		method = fn.Name()
	}
	recvPkg := ""
	if c.IsInvoke() {
		recvPkg = typePkg(c.Value.Type())
	} else if fn := c.StaticCallee(); fn != nil && fn.Pkg != nil {
		recvPkg = fn.Pkg.Pkg.Path()
	}
	switch {
	case method == "Scan" && (strings.HasPrefix(recvPkg, "github.com/jackc/pgx") || recvPkg == "database/sql" || strings.HasPrefix(recvPkg, "github.com/jmoiron/sqlx")):
		n := t.event(KindSource, "database row ("+name+")", "column value written by Scan", ci.Parent(), ci.Pos(), "db-result")
		return step{node: n, kind: EdgeSource, conf: evidence.Exact, pos: ci.Pos(), stop: true}
	case full == "(*encoding/json.Decoder).Decode":
		recv := c.Args[0]
		if c.IsInvoke() {
			recv = c.Value
		}
		if fromRequest(recv, 0) {
			n := t.event(KindSource, "HTTP JSON request body", "decoded by json.Decoder.Decode", ci.Parent(), ci.Pos(), "http-input")
			return step{node: n, kind: EdgeSource, conf: evidence.Exact, pos: ci.Pos(), stop: true}
		}
	}
	n := t.event(KindUnresolved, "may be written by "+name, "the call receives the address; what it writes is not analysed", ci.Parent(), ci.Pos())
	return step{node: n, kind: EdgeStore, conf: evidence.Possible, pos: ci.Pos(), stop: true}
}

func typePkg(t types.Type) string {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if n, ok := t.(*types.Named); ok && n.Obj().Pkg() != nil {
		return n.Obj().Pkg().Path()
	}
	return ""
}

// memSuccs finds the reads that a write to addr can reach (only those
// reached by the write st, when given).
func (t *tracer) memSuccs(addr ssa.Value, st *ssa.Store) []step {
	l0 := locOf(addr)
	fns, alias, local := t.memScopes(l0.base)
	l := normLoc(l0, alias)
	esc := escapes(l.base)
	var stores []*ssa.Store
	if st != nil {
		stores = append(stores, st)
	}
	var out []step
	for _, fn := range fns {
		for _, b := range fn.Blocks {
			for _, ins := range b.Instrs {
				ld, ok := ins.(*ssa.UnOp)
				if !ok || ld.Op != token.MUL {
					continue
				}
				rel, conf := related(normLoc(locOf(ld.X), alias), l)
				if !rel {
					continue
				}
				if len(stores) > 0 {
					reach := false
					for _, s := range stores {
						if s.Parent() != ld.Parent() || t.reaches(s, ld) && !killed(s, ld, l, alias) {
							reach = true
						}
					}
					if !reach {
						continue
					}
				}
				if esc || !local {
					conf = weaker(conf, evidence.Possible)
				}
				out = append(out, step{v: ld, kind: EdgeLoad, conf: conf, label: "read at line " + lineOf(t.p, ld.Pos()), pos: ld.Pos(), fromCtx: ld.Parent() == addr.Parent()})
			}
		}
	}
	// The address itself passed to calls: they may read the value.
	for _, fn := range fns {
		for _, b := range fn.Blocks {
			for _, ins := range b.Instrs {
				ci, ok := ins.(ssa.CallInstruction)
				if !ok {
					continue
				}
				for _, a := range callAddrArgs(ci) {
					if rel, _ := related(normLoc(locOf(a), alias), l); rel {
						out = append(out, step{node: t.event(KindExternal, "address passed to "+calleeName(ci.Common()), "the callee may read the value through the pointer", ci.Parent(), ci.Pos()),
							kind: EdgeSink, conf: evidence.Possible, pos: ci.Pos(), stop: true})
						break
					}
				}
			}
		}
	}
	if st == nil && l.path == "" {
		// The whole variable: its address may leave the function.
		if refs := addr.Referrers(); refs != nil {
			for _, r := range *refs {
				if ret, ok := r.(*ssa.Return); ok {
					for i, res := range ret.Results {
						if res == addr {
							for _, s := range t.returnSteps(item{v: addr}, ret, i) {
								s.label = joinLabels("returned as a pointer", s.label)
								out = append(out, s)
							}
						}
					}
				}
			}
		}
	}
	switch b := l.base.(type) {
	case *ssa.Parameter:
		out = append(out, step{node: t.event(KindUnresolved, "visible to callers through param "+b.Name(), "reads after the call returns are not tracked", b.Parent(), b.Pos()),
			kind: EdgeStore, conf: evidence.Possible, stop: true})
	case *ssa.Global:
		if len(out) == 0 {
			out = append(out, step{node: t.event(KindGlobal, "package variable "+b.Name(), "read elsewhere", nil, b.Pos()), kind: EdgeStore, conf: evidence.Possible, stop: true})
		}
	}
	return out
}
