// Package httpapi reads HTTP contracts from Go code by static analysis: the
// parameters, body and responses of every route found by Code Flow, from
// its middleware and handler and the helpers they pass HTTP values to.
//
// The analysis interprets the SSA of those functions: framework calls
// (gin, net/http, chi, echo, encoding/json) are recognised by package and
// method, and statuses and body types are resolved through parameters,
// struct fields, package variables and returned values. It never descends
// into services (functions that receive no HTTP value), never runs code,
// and marks what it cannot resolve instead of guessing it.
package httpapi

import (
	"fmt"
	"go/types"
	"net/http"
	"sort"
	"strings"

	"github.com/athos/gitview/internal/evidence"
	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/graph/model"
	"golang.org/x/tools/go/ssa"
)

// Limits of the analysis of one route.
const (
	maxDepth  = 6   // helper calls followed from a handler or middleware
	maxFrames = 400 // functions analysed per route
)

// Options of the analysis.
type Options struct {
	// FuncID maps a Code Flow function ID to its graph node ID.
	FuncID func(flowID string) string
}

// Analyze reads the contracts of every route of the program. defs receives
// the named schemas (shared by every program of the dataset).
func Analyze(p *flow.Program, defs map[string]*model.Shape, opt Options) []*model.Route {
	if opt.FuncID == nil {
		opt.FuncID = func(id string) string { return id }
	}
	a := &analyzer{p: p, shapes: newShapes(p, defs), opt: opt, ev: newEvaluator(p.SSA())}
	var out []*model.Route
	for _, ri := range p.Routes() {
		out = append(out, a.route(ri))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		if out[i].Method != out[j].Method {
			return out[i].Method < out[j].Method
		}
		return out[i].ID < out[j].ID
	})
	return out
}

type analyzer struct {
	p      *flow.Program
	shapes *shapes
	opt    Options
	ev     *evaluator
}

// chainItem is a middleware or handler being analysed.
type chainItem struct {
	entry      model.ChainEntry
	middleware bool
	credential string // what it reads that may be a credential
	scheme     string
	credEv     []evidence.Evidence
	answers401 bool
	reads      []string // headers and cookies it reads
}

// routeState collects the facts of one route.
type routeState struct {
	a      *analyzer
	r      *model.Route
	frames []*frame
	params map[string]*model.Param
	order  []string
	bodies []*model.Body
	resps  []*model.Response
	items  []*chainItem
	limits map[string]bool
	method string
	// validates is set when the chain calls a go-playground validator, so
	// validate tags are applied.
	validates bool
}

func (a *analyzer) ev2(file string, line int, symbol, details string) evidence.Evidence {
	return evidence.Evidence{Kind: evidence.Static, Basis: evidence.BasisStatic, Source: "go/ssa", File: file, Line: line, SymbolID: symbol, Details: details}
}

func (a *analyzer) route(ri flow.RouteInfo) *model.Route {
	method := ri.Method
	if method == "" {
		method = "ANY"
	}
	r := &model.Route{ID: ri.ID, Key: method + " " + ri.Path, Method: method, Path: ri.Path, Template: Template(ri.Path),
		Framework: ri.Router, File: ri.File, Line: ri.Line, Status: model.StatusComplete}
	st := &routeState{a: a, r: r, params: map[string]*model.Param{}, limits: map[string]bool{}, method: method}
	if ri.Partial {
		for _, n := range ri.Notes {
			st.limit("route: " + n)
		}
	}
	for _, h := range ri.Chain {
		ce := model.ChainEntry{Confidence: h.Conf, Scope: h.Scope, AddedFile: h.File, AddedLine: h.Line, Note: h.Note}
		if h.Fn != nil {
			fn := flow.Origin(h.Fn)
			ce.ID = a.opt.FuncID(fn.String())
			ce.Name = flow.ShortName(fn)
			ce.File, ce.Line, _ = a.p.Range(fn)
		} else {
			ce.Name = "unresolved"
			st.limit("a handler of the chain could not be resolved: " + h.Note)
		}
		item := &chainItem{entry: ce, middleware: h.Middleware}
		st.items = append(st.items, item)
		if h.Middleware {
			r.Middlewares = append(r.Middlewares, ce)
		} else {
			c := ce
			r.Handler = &c
		}
		if strings.Contains(h.Note, "BasicAuth") {
			item.credential, item.scheme, item.answers401 = "Authorization header", "basic", true
			item.credEv = append(item.credEv, a.ev2(h.File, h.Line, "", h.Note))
		}
		if h.Fn != nil {
			conf := model.Confirmed
			if h.Conf != flow.Confirmed {
				conf = h.Conf
			}
			st.walk(&frame{fn: flow.Origin(h.Fn), env: map[ssa.Value]ssa.Value{}, owner: item, conf: conf})
		}
	}
	// Path parameters come from the pattern itself.
	for _, name := range PathParams(ri.Path) {
		st.pathParam(name, "route pattern", nil, a.ev2(ri.File, ri.Line, "", "placeholder in the registered pattern "+ri.Path), model.Confirmed)
	}
	for _, fr := range st.frames {
		st.pair(fr)
	}
	for _, fr := range st.frames {
		st.pairHeaders(fr)
	}
	st.finish()
	return r
}

func (st *routeState) limit(reason string) {
	if !st.limits[reason] {
		st.limits[reason] = true
		st.r.Reasons = append(st.r.Reasons, reason)
	}
}

func weaker(a, b string) string {
	rank := map[string]int{model.Confirmed: 4, model.Likely: 3, model.Possible: 2, model.Unresolved: 1}
	if rank[b] < rank[a] {
		return b
	}
	return a
}

// walk analyses one function of the chain.
func (st *routeState) walk(fr *frame) {
	if len(st.frames) >= maxFrames {
		st.limit(fmt.Sprintf("analysis stopped after %d functions", maxFrames))
		return
	}
	st.frames = append(st.frames, fr)
	for _, b := range fr.fn.Blocks {
		for _, ins := range b.Instrs {
			ci, ok := ins.(ssa.CallInstruction)
			if !ok {
				continue
			}
			if st.recognize(fr, ci) {
				continue
			}
			st.descend(fr, ci)
		}
	}
}

// descend follows a call into a module function that receives an HTTP
// value (a helper that answers or decodes); services are not entered.
func (st *routeState) descend(fr *frame, ci ssa.CallInstruction) {
	c := ci.Common()
	type target struct {
		fn   *ssa.Function
		conf string
	}
	var targets []target
	if fn := c.StaticCallee(); fn != nil {
		targets = append(targets, target{flow.Unwrap(fn), model.Confirmed})
	} else {
		for _, t := range st.a.p.CalleesOf(ci) {
			if t.Fn != nil {
				targets = append(targets, target{t.Fn, t.Conf})
			}
		}
	}
	for _, t := range targets {
		fn := flow.Origin(t.fn)
		if fn == nil || !st.a.p.InModule(fn) || !httpRelated(fn, c) || fr.inChain(fn) {
			continue
		}
		if fr.depth+1 > maxDepth {
			st.limit(fmt.Sprintf("helpers deeper than %d calls were not followed", maxDepth))
			continue
		}
		st.walk(&frame{fn: fn, env: argEnv(fn, c), parent: fr, site: ci, owner: fr.owner, conf: weaker(fr.conf, t.conf), depth: fr.depth + 1})
	}
}

// httpRelated reports whether a call passes an HTTP value (request,
// response writer, framework context) to fn, or fn is a closure of the
// handler itself.
func httpRelated(fn *ssa.Function, c *ssa.CallCommon) bool {
	if fn.Parent() != nil {
		return true
	}
	vals := append([]ssa.Value{}, c.Args...)
	if c.IsInvoke() {
		vals = append(vals, c.Value)
	}
	for _, v := range vals {
		if isHTTPType(v.Type()) {
			return true
		}
	}
	return false
}

func namedPath(t types.Type) string {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if n, ok := types.Unalias(t).(*types.Named); ok && n.Obj().Pkg() != nil {
		return n.Obj().Pkg().Path() + "." + n.Obj().Name()
	}
	return ""
}

func isHTTPType(t types.Type) bool {
	switch namedPath(t) {
	case "net/http.Request", "net/http.ResponseWriter", "github.com/gin-gonic/gin.Context", "github.com/gin-gonic/gin.ResponseWriter",
		"github.com/labstack/echo/v4.Context", "github.com/labstack/echo.Context":
		return true
	}
	return false
}

func (st *routeState) site(ins ssa.Instruction) (string, int) {
	f, l, _ := st.a.p.Pos(ins.Pos())
	return f, l
}

func (st *routeState) evAt(fr *frame, ins ssa.Instruction, details string) []evidence.Evidence {
	file, line := st.site(ins)
	out := []evidence.Evidence{st.a.ev2(file, line, st.a.opt.FuncID(fr.fn.String()), details)}
	for f := fr; f != nil && f.site != nil; f = f.parent {
		file, line := st.site(f.site)
		out = append(out, st.a.ev2(file, line, st.a.opt.FuncID(f.parent.fn.String()), "calls "+flow.ShortName(f.fn)))
	}
	return out
}

func (st *routeState) param(name, in, source string, fr *frame, ins ssa.Instruction, sh *model.Shape, required, def, note string) {
	key := in + ":" + name
	ev := st.evAt(fr, ins, source+"(\""+name+"\")")
	if p := st.params[key]; p != nil {
		if p.Schema == nil || p.Schema.Type == model.TypeString && sh != nil && sh.Type != model.TypeString {
			p.Schema = sh
		}
		if required == "yes" {
			p.Required = "yes"
		}
		if p.Source == "route pattern" {
			p.Source = source
		}
		if p.Default == "" {
			p.Default = def
		}
		if len(p.Evidence) < 8 {
			p.Evidence = append(p.Evidence, ev...)
		}
		return
	}
	if sh == nil {
		sh = &model.Shape{Type: model.TypeString}
	}
	st.params[key] = &model.Param{Name: name, In: in, Required: required, Schema: sh, Default: def, Source: source,
		From: fr.owner.entry.Name, Confidence: fr.conf, Evidence: ev, Note: note}
	if in == "header" || in == "cookie" {
		fr.owner.reads = appendOnce(fr.owner.reads, in+" "+name)
	}
	st.order = append(st.order, key)
}

func (st *routeState) pathParam(name, source string, fr *frame, ev evidence.Evidence, conf string) {
	key := "path:" + name
	in := false
	for _, n := range PathParams(st.r.Path) {
		if n == name {
			in = true
		}
	}
	if p := st.params[key]; p != nil {
		if source != "route pattern" && p.Source == "route pattern" {
			p.Source = source
		}
		p.Evidence = append(p.Evidence, ev)
		return
	}
	note := ""
	if !in {
		note = "read by the code but not a placeholder of the route pattern"
	}
	st.params[key] = &model.Param{Name: name, In: "path", Required: "yes", Schema: &model.Shape{Type: model.TypeString},
		Source: source, InPattern: &in, Confidence: conf, Evidence: []evidence.Evidence{ev}, Note: note}
	st.order = append(st.order, key)
}

func (st *routeState) body(b *model.Body) {
	for _, x := range st.bodies {
		if x.Source == b.Source && sameShape(x.Schema, b.Schema) {
			x.Evidence = append(x.Evidence, b.Evidence...)
			return
		}
	}
	st.bodies = append(st.bodies, b)
}

func sameShape(a, b *model.Shape) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Ref == b.Ref && a.Type == b.Type && a.GoType == b.GoType
}

func (st *routeState) respond(fr *frame, ins ssa.Instruction, status ints, ct string, sh *model.Shape, source string, implicit bool, note string) {
	from := fr.owner.entry
	vals := status.vals
	if len(vals) == 0 {
		if status.complete {
			return // no path reaches this write with a value (see ints)
		}
		vals = []int64{0}
	}
	for _, code := range vals {
		resp := &model.Response{Status: int(code), ContentType: ct, Schema: sh, Source: source, From: from.Name, FromID: from.ID,
			Via: fr.via(), Confidence: fr.conf, Evidence: st.evAt(fr, ins, source), Implicit: implicit, Note: note}
		if code > 0 {
			resp.StatusText = http.StatusText(int(code))
		}
		switch {
		case code == 0 && sh == nil:
			resp.Basis = model.BasisUnknown
		case code == 0 || !status.complete || sh != nil && sh.Type == model.TypeUnknown:
			resp.Basis = model.BasisPartial
		default:
			resp.Basis = model.BasisStatic
		}
		if code == 0 {
			resp.Note = strings.TrimPrefix(resp.Note+"; status not determinable statically", "; ")
		} else if !status.complete {
			resp.Note = strings.TrimPrefix(resp.Note+"; other statuses may be computed at runtime", "; ")
		}
		if implicit {
			resp.Confidence = weaker(resp.Confidence, model.Possible)
		}
		if code == http.StatusUnauthorized {
			fr.owner.answers401 = true
		}
		st.addResponse(resp)
	}
}

func (st *routeState) addResponse(r *model.Response) {
	for _, x := range st.resps {
		if x.Status == r.Status && x.ContentType == r.ContentType && sameShape(x.Schema, r.Schema) && x.From == r.From && x.Source == r.Source && x.Implicit == r.Implicit {
			if len(x.Evidence) < 12 {
				x.Evidence = append(x.Evidence, r.Evidence...)
			}
			if len(r.Via) < len(x.Via) {
				x.Via = r.Via
			}
			return
		}
	}
	st.resps = append(st.resps, r)
}

// finish orders the facts and decides the analysis status.
func (st *routeState) finish() {
	r := st.r
	for _, k := range st.order {
		r.Params = append(r.Params, *st.params[k])
	}
	inOrder := map[string]int{"path": 0, "query": 1, "header": 2, "cookie": 3, "form": 4}
	sort.SliceStable(r.Params, func(i, j int) bool { return inOrder[r.Params[i].In] < inOrder[r.Params[j].In] })
	if len(st.bodies) > 0 {
		r.Body = st.bodies[0]
		for _, b := range st.bodies[1:] {
			r.Body.Notes = append(r.Body.Notes, "the chain also decodes "+describeShape(b.Schema)+" with "+b.Source+" ("+loc(b.Evidence)+")")
			st.limit("more than one request body decoding")
		}
	}
	for _, x := range st.resps {
		r.Responses = append(r.Responses, *x)
	}
	sort.SliceStable(r.Responses, func(i, j int) bool {
		a, b := r.Responses[i], r.Responses[j]
		if (a.Status == 0) != (b.Status == 0) {
			return b.Status == 0
		}
		if a.Status != b.Status {
			return a.Status < b.Status
		}
		return a.From < b.From
	})
	// Authentication: a chain element that reads a credential and can
	// answer 401.
	for _, it := range st.items {
		if it.credential == "" && !it.answers401 {
			continue
		}
		au := &model.Auth{Credential: it.credential, Scheme: it.scheme, By: it.entry.Name, Evidence: it.credEv}
		switch {
		case it.credential != "" && it.answers401:
			au.Recognized = true
		case it.answers401:
			au.Recognized = true
			au.Note = "answers 401 Unauthorized; the credential it checks was not identified"
			if len(it.reads) > 0 {
				au.Note = "answers 401 Unauthorized after reading " + strings.Join(it.reads, ", ")
			}
		default:
			au.Note = "reads " + it.credential + " but no 401 response was identified"
		}
		if r.Auth == nil || au.Recognized && !r.Auth.Recognized {
			r.Auth = au
		}
	}
	if len(r.Responses) == 0 {
		st.limit("no response identified")
	}
	for _, x := range r.Responses {
		if x.Basis != model.BasisStatic {
			st.limit("some responses are only partially identified")
			break
		}
	}
	if r.Handler == nil || r.Handler.ID == "" {
		r.Status = model.StatusUnknown
	} else if len(r.Reasons) > 0 {
		r.Status = model.StatusPartial
	}
}

func loc(ev []evidence.Evidence) string {
	if len(ev) == 0 {
		return ""
	}
	return fmt.Sprintf("%s:%d", ev[0].File, ev[0].Line)
}

func describeShape(sh *model.Shape) string {
	if sh == nil {
		return "a body"
	}
	if sh.Name != "" {
		return sh.Name
	}
	if sh.GoType != "" {
		return sh.GoType
	}
	return sh.Type
}
