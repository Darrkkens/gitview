package httpapi

import (
	"go/constant"
	"go/token"
	"go/types"
	"net/http"
	"strconv"
	"strings"

	"github.com/athos/gitview/internal/evidence"
	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/graph/model"
	"golang.org/x/tools/go/ssa"
)

const (
	pkgGin      = "github.com/gin-gonic/gin"
	pkgChi      = "github.com/go-chi/chi/v5"
	pkgChiOld   = "github.com/go-chi/chi"
	pkgEcho     = "github.com/labstack/echo/v4"
	pkgEchoOld  = "github.com/labstack/echo"
	pkgHTTP     = "net/http"
	pkgJSON     = "encoding/json"
	pkgValidate = "github.com/go-playground/validator/v10"
)

// call describes a call target by package, receiver type and name, for
// static calls and interface invocations alike.
type call struct {
	pkg, typ, name string
	recv           ssa.Value
	args           []ssa.Value
}

func describe(c *ssa.CallCommon) (call, bool) {
	if c.IsInvoke() {
		m := c.Method
		if m.Pkg() == nil {
			return call{}, false
		}
		typ := ""
		if n, ok := types.Unalias(c.Value.Type()).(*types.Named); ok {
			typ = n.Obj().Name()
		}
		return call{pkg: m.Pkg().Path(), typ: typ, name: m.Name(), recv: c.Value, args: c.Args}, true
	}
	fn := c.StaticCallee()
	if fn == nil {
		return call{}, false
	}
	pkg := ""
	if fn.Pkg != nil {
		pkg = fn.Pkg.Pkg.Path()
	} else if fn.Object() != nil && fn.Object().Pkg() != nil {
		pkg = fn.Object().Pkg().Path()
	}
	if fn.Signature.Recv() != nil && len(c.Args) > 0 {
		t := fn.Signature.Recv().Type()
		if p, ok := t.(*types.Pointer); ok {
			t = p.Elem()
		}
		typ := ""
		if n, ok := types.Unalias(t).(*types.Named); ok {
			typ = n.Obj().Name()
		}
		return call{pkg: pkg, typ: typ, name: fn.Name(), recv: c.Args[0], args: c.Args[1:]}, true
	}
	return call{pkg: pkg, name: fn.Name(), args: c.Args}, true
}

// event is a net/http write (status, body or content type) waiting to be
// paired: WriteHeader followed by Encode is one response.
type event struct {
	kind   string // header | body | ctype
	ins    ssa.Instruction
	status ints
	shape  *model.Shape
	ct     string
	source string
	used   bool
}

var ginJSONLike = map[string]string{
	"JSON": "application/json", "IndentedJSON": "application/json", "SecureJSON": "application/json", "AsciiJSON": "application/json",
	"PureJSON": "application/json", "JSONP": "application/javascript", "XML": "application/xml", "YAML": "application/yaml",
	"TOML": "application/toml", "ProtoBuf": "application/x-protobuf", "AbortWithStatusJSON": "application/json",
}

var echoBodyLike = map[string]string{
	"JSON": "application/json", "JSONPretty": "application/json", "JSONBlob": "application/json", "XML": "application/xml",
	"XMLPretty": "application/xml", "String": "text/plain", "HTML": "text/html",
}

// recognize records the facts of a framework call. It returns true when the
// call was understood (or belongs to a framework), so it is not entered.
func (st *routeState) recognize(fr *frame, ci ssa.CallInstruction) bool {
	c, ok := describe(ci.Common())
	if !ok {
		return false
	}
	switch {
	case c.pkg == pkgGin && c.typ == "Context":
		st.gin(fr, ci, c)
		return true
	case (c.pkg == pkgEcho || c.pkg == pkgEchoOld) && c.typ == "Context":
		st.echo(fr, ci, c)
		return true
	case (c.pkg == pkgChi || c.pkg == pkgChiOld) && (c.name == "URLParam" || c.name == "URLParamFromCtx") && len(c.args) == 2:
		if name, ok := stringValue(c.args[1], fr); ok {
			st.pathParam(name, "chi.URLParam", fr, st.evAt(fr, ci, "chi.URLParam(\""+name+"\")")[0], fr.conf)
			st.hint(fr, ci, "path", name)
		}
		return true
	case c.pkg == pkgHTTP || c.pkg == "net/url":
		return st.nethttp(fr, ci, c)
	case c.pkg == pkgJSON:
		st.json(fr, ci, c)
		return true
	case c.pkg == pkgValidate && (c.name == "Struct" || c.name == "StructCtx"):
		for _, b := range st.bodies {
			b.Enforces = appendOnce(b.Enforces, "validate")
		}
		st.validates = true
		return true
	}
	return false
}

func appendOnce(list []string, s string) []string {
	for _, x := range list {
		if x == s {
			return list
		}
	}
	return append(list, s)
}

func (st *routeState) shapeOf(v ssa.Value, fr *frame, ins ssa.Instruction, tag string) *model.Shape {
	bv := bodyOf(v, fr, ins, 0)
	if !bv.known {
		return &model.Shape{Type: model.TypeUnknown, Note: "value not determinable statically"}
	}
	if bv.keys != nil || bv.partial != "" {
		sh := &model.Shape{Type: model.TypeObject, GoType: shortType(bv.t), Partial: bv.partial != "", Note: bv.partial}
		for _, k := range bv.keys {
			sh.Fields = append(sh.Fields, model.Field{Name: k.name, Schema: st.a.shapes.of(k.t, tag), Conditional: k.conditional})
		}
		return sh
	}
	t := bv.t
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	return st.a.shapes.of(t, tag)
}

// boundType is the type behind a bind target (&req): the pointer's element.
func boundType(v ssa.Value, fr *frame) (types.Type, bool) {
	bv := bodyOf(v, fr, nil, 0)
	if !bv.known {
		return nil, false
	}
	t := bv.t
	if p, ok := t.(*types.Pointer); ok {
		return p.Elem(), true
	}
	return t, true
}

func (st *routeState) bindBody(fr *frame, ci ssa.CallInstruction, target ssa.Value, source string, cts []string, tag string, enforces []string, note string) {
	t, ok := boundType(target, fr)
	b := &model.Body{ContentTypes: cts, Source: source, Enforces: enforces, Confidence: fr.conf, Evidence: st.evAt(fr, ci, source)}
	if ok {
		b.Schema = st.a.shapes.of(t, tag)
	} else {
		b.Schema = &model.Shape{Type: model.TypeUnknown, Note: "decoded into a value whose type is not determinable statically"}
	}
	if note != "" {
		b.Notes = append(b.Notes, note)
	}
	if st.validates {
		b.Enforces = appendOnce(b.Enforces, "validate")
	}
	st.body(b)
}

// bindParams turns the fields of a struct bound from the query, the path
// or the headers into parameters.
func (st *routeState) bindParams(fr *frame, ci ssa.CallInstruction, target ssa.Value, in, tag, source string, enforces []string) {
	t, ok := boundType(target, fr)
	if !ok {
		st.limit(source + " binds a value whose type is not determinable statically")
		return
	}
	sh := resolve(st.a.shapes.defs, st.a.shapes.of(t, tag))
	for _, f := range sh.Fields {
		req := "no"
		if f.Required(enforces) {
			req = "yes"
		}
		if in == "path" {
			st.pathParam(f.Name, source, fr, st.evAt(fr, ci, source+" field "+f.GoName)[0], fr.conf)
			continue
		}
		st.param(f.Name, in, source, fr, ci, f.Schema, req, "", "field "+f.GoName+" of "+shortType(t))
	}
}

func (st *routeState) keyParam(fr *frame, ci ssa.CallInstruction, c call, in, source string, required, note string) {
	if len(c.args) == 0 {
		return
	}
	name, ok := stringValue(c.args[0], fr)
	if !ok {
		if in == "cookie" {
			// A session cookie with a computed name: described by the
			// authentication (credential), not a gap of the contract.
			return
		}
		st.limit(source + " reads a " + in + " parameter whose name is computed at runtime")
		return
	}
	def := ""
	if (c.name == "DefaultQuery" || c.name == "DefaultPostForm") && len(c.args) > 1 {
		def, _ = stringValue(c.args[1], fr)
		required = "no"
	}
	if in == "path" {
		st.pathParam(name, source, fr, st.evAt(fr, ci, source+"(\""+name+"\")")[0], fr.conf)
		st.hint(fr, ci, in, name)
		return
	}
	st.param(name, in, source, fr, ci, nil, required, def, note)
	st.hint(fr, ci, in, name)
	st.credential(fr, ci, in, name)
}

// credential notes what may authenticate the request.
func (st *routeState) credential(fr *frame, ci ssa.CallInstruction, in, name string) {
	it := fr.owner
	switch {
	case in == "header" && strings.EqualFold(name, "Authorization"):
		it.credential = "Authorization header"
		it.scheme = schemeIn(fr.fn)
	case in == "cookie" && it.credential == "":
		it.credential = "cookie " + name
		it.scheme = "cookie"
	default:
		return
	}
	it.credEv = append(it.credEv, st.evAt(fr, ci, "reads the "+it.credential)...)
}

// schemeIn looks for the scheme prefix compared with the header value.
func schemeIn(fn *ssa.Function) string {
	scheme := "header"
	for _, b := range fn.Blocks {
		for _, ins := range b.Instrs {
			for _, op := range ins.Operands(nil) {
				if k, ok := (*op).(*ssa.Const); ok && k.Value != nil && k.Value.Kind() == constant.String {
					s := strings.ToLower(strings.TrimSpace(constant.StringVal(k.Value)))
					switch s {
					case "bearer":
						return "bearer"
					case "basic":
						scheme = "basic"
					}
				}
			}
		}
	}
	return scheme
}

// hint refines a parameter's type from a direct conversion of the value
// (strconv.Atoi(c.Param("id"))).
func (st *routeState) hint(fr *frame, ci ssa.CallInstruction, in, name string) {
	v, ok := ci.(*ssa.Call)
	if !ok {
		return
	}
	var val ssa.Value = v
	if _, tuple := v.Type().(*types.Tuple); tuple {
		for _, r := range *v.Referrers() {
			if ex, ok := r.(*ssa.Extract); ok && ex.Index == 0 {
				val = ex
			}
		}
	}
	refs := val.Referrers()
	if refs == nil {
		return
	}
	for _, r := range *refs {
		rc, ok := r.(ssa.CallInstruction)
		if !ok {
			continue
		}
		d, ok := describe(rc.Common())
		if !ok {
			continue
		}
		var sh *model.Shape
		switch d.pkg + "." + d.name {
		case "strconv.Atoi", "strconv.ParseInt", "strconv.ParseUint":
			sh = &model.Shape{Type: model.TypeInteger}
		case "strconv.ParseFloat":
			sh = &model.Shape{Type: model.TypeNumber}
		case "strconv.ParseBool":
			sh = &model.Shape{Type: model.TypeBoolean}
		case "github.com/google/uuid.Parse", "github.com/google/uuid.MustParse":
			sh = &model.Shape{Type: model.TypeString, Format: "uuid"}
		default:
			continue
		}
		file, line := st.site(rc)
		sh.Note = "converted with " + d.name + " at " + file + ":" + itoa(line)
		if p := st.params[in+":"+name]; p != nil {
			p.Schema = sh
		}
		return
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func (st *routeState) gin(fr *frame, ci ssa.CallInstruction, c call) {
	enforce := []string{"binding"}
	switch c.name {
	case "ShouldBindJSON", "BindJSON", "ShouldBindBodyWithJSON":
		if len(c.args) > 0 {
			st.bindBody(fr, ci, c.args[0], "c."+c.name, []string{"application/json"}, "json", enforce, "")
		}
	case "ShouldBindXML", "BindXML", "ShouldBindBodyWithXML":
		if len(c.args) > 0 {
			st.bindBody(fr, ci, c.args[0], "c."+c.name, []string{"application/xml"}, "xml", enforce, "")
		}
	case "ShouldBindYAML", "BindYAML":
		if len(c.args) > 0 {
			st.bindBody(fr, ci, c.args[0], "c."+c.name, []string{"application/yaml"}, "yaml", enforce, "")
		}
	case "ShouldBind", "Bind":
		if len(c.args) == 0 {
			return
		}
		if st.method == "GET" {
			// gin binds the query string (form tags) for GET requests.
			st.bindParams(fr, ci, c.args[0], "query", "form", "c."+c.name, enforce)
			return
		}
		st.bindBody(fr, ci, c.args[0], "c."+c.name, []string{"application/json", "application/xml", "application/x-www-form-urlencoded", "multipart/form-data"},
			"json", enforce, "gin chooses the decoder from the request Content-Type; field names shown for JSON")
	case "ShouldBindWith", "BindWith", "ShouldBindBodyWith", "MustBindWith":
		if len(c.args) < 2 {
			return
		}
		switch bindingName(c.args[1]) {
		case "JSON":
			st.bindBody(fr, ci, c.args[0], "c."+c.name+"(binding.JSON)", []string{"application/json"}, "json", enforce, "")
		case "XML":
			st.bindBody(fr, ci, c.args[0], "c."+c.name+"(binding.XML)", []string{"application/xml"}, "xml", enforce, "")
		case "Query":
			st.bindParams(fr, ci, c.args[0], "query", "form", "c."+c.name+"(binding.Query)", enforce)
		case "Form", "FormPost", "FormMultipart":
			st.bindBody(fr, ci, c.args[0], "c."+c.name+"(binding.Form)", []string{"application/x-www-form-urlencoded", "multipart/form-data"}, "form", enforce, "")
		case "Uri":
			st.bindParams(fr, ci, c.args[0], "path", "uri", "c."+c.name+"(binding.Uri)", enforce)
		case "Header":
			st.bindParams(fr, ci, c.args[0], "header", "header", "c."+c.name+"(binding.Header)", enforce)
		default:
			st.bindBody(fr, ci, c.args[0], "c."+c.name, nil, "json", enforce, "binding chosen at runtime")
			st.limit("a body binding is chosen at runtime")
		}
	case "ShouldBindQuery", "BindQuery":
		if len(c.args) > 0 {
			st.bindParams(fr, ci, c.args[0], "query", "form", "c."+c.name, enforce)
		}
	case "ShouldBindUri", "BindUri":
		if len(c.args) > 0 {
			st.bindParams(fr, ci, c.args[0], "path", "uri", "c."+c.name, enforce)
		}
	case "ShouldBindHeader", "BindHeader":
		if len(c.args) > 0 {
			st.bindParams(fr, ci, c.args[0], "header", "header", "c."+c.name, enforce)
		}
	case "Param":
		st.keyParam(fr, ci, c, "path", "c.Param", "yes", "")
	case "Query", "GetQuery", "QueryArray", "GetQueryArray", "QueryMap", "GetQueryMap", "DefaultQuery":
		note := ""
		if strings.Contains(c.name, "Array") {
			note = "may be repeated"
		}
		st.keyParam(fr, ci, c, "query", "c."+c.name, "unknown", note)
	case "PostForm", "GetPostForm", "PostFormArray", "GetPostFormArray", "PostFormMap", "GetPostFormMap", "DefaultPostForm":
		st.keyParam(fr, ci, c, "form", "c."+c.name, "unknown", "")
		st.formBody(fr, ci, "c."+c.name, false)
	case "FormFile":
		st.keyParam(fr, ci, c, "form", "c.FormFile", "unknown", "file upload")
		if p := st.lastParam("form"); p != nil {
			p.Schema = &model.Shape{Type: model.TypeString, Format: "binary"}
		}
		st.formBody(fr, ci, "c.FormFile", true)
	case "MultipartForm":
		st.formBody(fr, ci, "c.MultipartForm", true)
	case "GetHeader":
		st.keyParam(fr, ci, c, "header", "c.GetHeader", "unknown", "")
	case "Cookie":
		st.keyParam(fr, ci, c, "cookie", "c.Cookie", "unknown", "")
	case "Header":
		if len(c.args) == 2 {
			if k, ok := stringValue(c.args[0], fr); ok && strings.EqualFold(k, "Content-Type") {
				ct, _ := stringValue(c.args[1], fr)
				fr.events = append(fr.events, event{kind: "ctype", ins: ci, ct: ct})
			}
		}
	case "String", "HTML":
		if len(c.args) > 0 {
			ct := "text/plain"
			if c.name == "HTML" {
				ct = "text/html"
			}
			st.respond(fr, ci, st.a.ev.intValues(c.args[0], fr, 0), ct, &model.Shape{Type: model.TypeString}, "c."+c.name, false, "")
		}
	case "Data":
		if len(c.args) == 3 {
			ct, _ := stringValue(c.args[1], fr)
			st.respond(fr, ci, st.a.ev.intValues(c.args[0], fr, 0), ct, &model.Shape{Type: model.TypeString, Format: "binary"}, "c.Data", false, "")
		}
	case "Redirect":
		if len(c.args) > 0 {
			st.respond(fr, ci, st.a.ev.intValues(c.args[0], fr, 0), "", nil, "c.Redirect", false, "")
		}
	case "Status", "AbortWithStatus", "AbortWithError":
		if len(c.args) > 0 {
			st.respond(fr, ci, st.a.ev.intValues(c.args[0], fr, 0), "", nil, "c."+c.name, false, "")
		}
	case "File", "FileAttachment", "FileFromFS":
		st.respond(fr, ci, ints{vals: []int64{http.StatusOK}, complete: false}, "", &model.Shape{Type: model.TypeString, Format: "binary"}, "c."+c.name, true,
			"serves a file; http.ServeFile may also answer 304, 404 or 416")
	default:
		if ct, ok := ginJSONLike[c.name]; ok && len(c.args) >= 2 {
			st.respond(fr, ci, st.a.ev.intValues(c.args[0], fr, 0), ct, st.shapeOf(c.args[1], fr, ci, "json"), "c."+c.name, false, "")
		}
	}
}

func (st *routeState) lastParam(in string) *model.Param {
	for i := len(st.order) - 1; i >= 0; i-- {
		if p := st.params[st.order[i]]; p.In == in {
			return p
		}
	}
	return nil
}

func (st *routeState) formBody(fr *frame, ci ssa.CallInstruction, source string, multipart bool) {
	cts := []string{"application/x-www-form-urlencoded", "multipart/form-data"}
	if multipart {
		cts = []string{"multipart/form-data"}
	}
	for _, b := range st.bodies {
		if b.Schema == nil && strings.Contains(strings.Join(b.ContentTypes, ","), "form") {
			return
		}
	}
	st.body(&model.Body{ContentTypes: cts, Source: source, Confidence: fr.conf, Evidence: st.evAt(fr, ci, source),
		Notes: []string{"form fields are listed as parameters with in=form"}})
}

// bindingName reads binding.JSON, binding.Query... passed to ShouldBindWith.
func bindingName(v ssa.Value) string {
	if u, ok := v.(*ssa.UnOp); ok && u.Op == token.MUL {
		if g, ok := u.X.(*ssa.Global); ok && g.Pkg != nil && strings.HasSuffix(g.Pkg.Pkg.Path(), "gin/binding") {
			return g.Name()
		}
	}
	return ""
}

func (st *routeState) echo(fr *frame, ci ssa.CallInstruction, c call) {
	switch c.name {
	case "Bind":
		if len(c.args) > 0 {
			st.bindBody(fr, ci, c.args[0], "c.Bind", []string{"application/json", "application/xml", "application/x-www-form-urlencoded"}, "json", nil,
				"echo chooses the decoder from the request Content-Type and also binds path (param tags) and query (query tags) values")
		}
	case "Param":
		st.keyParam(fr, ci, c, "path", "c.Param", "yes", "")
	case "QueryParam":
		st.keyParam(fr, ci, c, "query", "c.QueryParam", "unknown", "")
	case "FormValue":
		st.keyParam(fr, ci, c, "form", "c.FormValue", "unknown", "")
		st.formBody(fr, ci, "c.FormValue", false)
	case "FormFile":
		st.keyParam(fr, ci, c, "form", "c.FormFile", "unknown", "file upload")
		st.formBody(fr, ci, "c.FormFile", true)
	case "Cookie":
		st.keyParam(fr, ci, c, "cookie", "c.Cookie", "unknown", "")
	case "NoContent", "Redirect":
		if len(c.args) > 0 {
			st.respond(fr, ci, st.a.ev.intValues(c.args[0], fr, 0), "", nil, "c."+c.name, false, "")
		}
	case "Blob", "Stream":
		if len(c.args) >= 2 {
			ct, _ := stringValue(c.args[1], fr)
			st.respond(fr, ci, st.a.ev.intValues(c.args[0], fr, 0), ct, &model.Shape{Type: model.TypeString, Format: "binary"}, "c."+c.name, false, "")
		}
	case "File", "Attachment", "Inline":
		st.respond(fr, ci, ints{vals: []int64{http.StatusOK}}, "", &model.Shape{Type: model.TypeString, Format: "binary"}, "c."+c.name, true, "serves a file")
	default:
		if ct, ok := echoBodyLike[c.name]; ok && len(c.args) >= 2 {
			sh := &model.Shape{Type: model.TypeString}
			if strings.Contains(ct, "json") || strings.Contains(ct, "xml") {
				sh = st.shapeOf(c.args[1], fr, ci, "json")
			}
			st.respond(fr, ci, st.a.ev.intValues(c.args[0], fr, 0), ct, sh, "c."+c.name, false, "")
		}
	}
}

// requestField reports whether v is field name of an *http.Request.
func requestField(v ssa.Value, fr *frame, name string) bool {
	v, fr = bind(v, fr)
	switch x := v.(type) {
	case *ssa.UnOp:
		if fa, ok := x.X.(*ssa.FieldAddr); ok && x.Op == token.MUL {
			return fieldIs(fa.X.Type(), fa.Field, name)
		}
	case *ssa.Field:
		return fieldIs(x.X.Type(), x.Field, name)
	}
	return false
}

func fieldIs(t types.Type, idx int, name string) bool {
	if namedPath(t) != "net/http.Request" {
		return false
	}
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	st, ok := t.Underlying().(*types.Struct)
	return ok && idx < st.NumFields() && st.Field(idx).Name() == name
}

func isWriter(t types.Type) bool {
	ms := types.NewMethodSet(t)
	n := 0
	for i := 0; i < ms.Len(); i++ {
		switch ms.At(i).Obj().Name() {
		case "Header", "Write", "WriteHeader":
			n++
		}
	}
	return n == 3
}

// responseHeader reports whether v is w.Header() of a response writer.
func responseHeader(v ssa.Value, fr *frame) bool {
	v, _ = bind(v, fr)
	if c, ok := v.(*ssa.Call); ok {
		if d, ok := describe(c.Common()); ok && d.pkg == pkgHTTP && d.name == "Header" && d.recv != nil && isWriter(d.recv.Type()) {
			return true
		}
	}
	return false
}

func (st *routeState) nethttp(fr *frame, ci ssa.CallInstruction, c call) bool {
	switch {
	case c.name == "WriteHeader" && c.recv != nil && len(c.args) == 1:
		fr.events = append(fr.events, event{kind: "header", ins: ci, status: st.a.ev.intValues(c.args[0], fr, 0), source: "WriteHeader"})
	case c.name == "Write" && c.recv != nil && isWriter(c.recv.Type()):
		fr.events = append(fr.events, event{kind: "body", ins: ci, shape: &model.Shape{Type: model.TypeString, Note: "bytes written with Write"}, source: "Write"})
	case c.typ == "Header" && (c.name == "Set" || c.name == "Add") && len(c.args) == 2:
		if responseHeader(c.recv, fr) {
			if k, ok := stringValue(c.args[0], fr); ok && strings.EqualFold(k, "Content-Type") {
				ct, _ := stringValue(c.args[1], fr)
				fr.events = append(fr.events, event{kind: "ctype", ins: ci, ct: ct})
			}
		}
	case c.typ == "Header" && (c.name == "Get" || c.name == "Values") && len(c.args) == 1:
		if requestField(c.recv, fr, "Header") {
			st.keyParam(fr, ci, c, "header", "r.Header."+c.name, "unknown", "")
		}
	case c.typ == "Request" && c.name == "PathValue":
		st.keyParam(fr, ci, c, "path", "r.PathValue", "yes", "")
	case c.typ == "Request" && c.name == "FormValue":
		st.keyParam(fr, ci, c, "query", "r.FormValue", "unknown", "or a form field of the body")
	case c.typ == "Request" && c.name == "PostFormValue":
		st.keyParam(fr, ci, c, "form", "r.PostFormValue", "unknown", "")
		st.formBody(fr, ci, "r.PostFormValue", false)
	case c.typ == "Request" && c.name == "FormFile":
		st.keyParam(fr, ci, c, "form", "r.FormFile", "unknown", "file upload")
		st.formBody(fr, ci, "r.FormFile", true)
	case c.typ == "Request" && c.name == "Cookie":
		st.keyParam(fr, ci, c, "cookie", "r.Cookie", "unknown", "")
		if len(c.args) == 1 {
			if _, ok := stringValue(c.args[0], fr); !ok && fr.owner.credential == "" {
				fr.owner.credential, fr.owner.scheme = "a cookie whose name is computed at runtime", "cookie"
				fr.owner.credEv = append(fr.owner.credEv, st.evAt(fr, ci, "r.Cookie")...)
			}
		}
	case c.typ == "" && c.name == "Error" && len(c.args) == 3:
		st.respond(fr, ci, st.a.ev.intValues(c.args[2], fr, 0), "text/plain", &model.Shape{Type: model.TypeString}, "http.Error", false, "")
	case c.typ == "" && c.name == "NotFound" && len(c.args) == 2:
		st.respond(fr, ci, ints{vals: []int64{404}, complete: true}, "text/plain", &model.Shape{Type: model.TypeString}, "http.NotFound", false, "")
	case c.typ == "" && c.name == "Redirect" && len(c.args) == 4:
		st.respond(fr, ci, st.a.ev.intValues(c.args[3], fr, 0), "", nil, "http.Redirect", false, "")
	case c.typ == "" && (c.name == "ServeFile" || c.name == "ServeContent"):
		st.respond(fr, ci, ints{vals: []int64{200}}, "", &model.Shape{Type: model.TypeString, Format: "binary"}, "http."+c.name, true, "serves a file; may also answer 304, 404 or 416")
	case c.typ == "URL" || c.typ == "Values":
		// r.URL.Query().Get("x")
		if c.typ == "Values" && c.name == "Get" && len(c.args) == 1 && queryOfRequest(c.recv, fr) {
			st.keyParam(fr, ci, c, "query", "r.URL.Query().Get", "unknown", "")
		}
	default:
		return c.recv != nil && isHTTPType(c.recv.Type())
	}
	return true
}

// queryOfRequest reports whether v is r.URL.Query() of a request.
func queryOfRequest(v ssa.Value, fr *frame) bool {
	v, fr = bind(v, fr)
	c, ok := v.(*ssa.Call)
	if !ok {
		return false
	}
	d, ok := describe(c.Common())
	return ok && d.typ == "URL" && d.name == "Query" && d.recv != nil && requestField(d.recv, fr, "URL")
}

func (st *routeState) json(fr *frame, ci ssa.CallInstruction, c call) {
	switch {
	case c.typ == "Decoder" && c.name == "Decode" && len(c.args) == 1:
		src, sfr := decoderSource(c.recv, fr)
		if src == nil || !requestField(src, sfr, "Body") {
			return
		}
		st.bindBody(fr, ci, c.args[0], "json.NewDecoder(r.Body).Decode", []string{"application/json"}, "json", nil, "")
		if decoderDisallows(c.recv, fr) {
			st.bodies[len(st.bodies)-1].DisallowUnknown = true
		}
	case c.typ == "Encoder" && c.name == "Encode" && len(c.args) == 1:
		src, _ := decoderSource(c.recv, fr)
		if src == nil || !isWriter(src.Type()) {
			return
		}
		fr.events = append(fr.events, event{kind: "body", ins: ci, ct: "application/json", shape: st.shapeOf(c.args[0], fr, ci, "json"), source: "json.NewEncoder(w).Encode"})
	case c.typ == "" && c.name == "Unmarshal" && len(c.args) == 2:
		if readsBody(c.args[0], fr) {
			st.bindBody(fr, ci, c.args[1], "io.ReadAll(r.Body) + json.Unmarshal", []string{"application/json"}, "json", nil, "")
		}
	}
}

// decoderSource returns the argument of the json.NewDecoder / NewEncoder
// call that created a decoder or encoder.
func decoderSource(v ssa.Value, fr *frame) (ssa.Value, *frame) {
	v, fr = bind(v, fr)
	c, ok := v.(*ssa.Call)
	if !ok {
		return nil, fr
	}
	if d, ok := describe(c.Common()); ok && d.pkg == pkgJSON && (d.name == "NewDecoder" || d.name == "NewEncoder") && len(d.args) == 1 {
		return unwrapIface(d.args[0], fr)
	}
	return nil, fr
}

// unwrapIface strips interface conversions (http.ResponseWriter passed as
// io.Writer, io.ReadCloser as io.Reader), through bound parameters.
func unwrapIface(v ssa.Value, fr *frame) (ssa.Value, *frame) {
	for i := 0; i < 8; i++ {
		v, fr = bind(v, fr)
		switch x := v.(type) {
		case *ssa.MakeInterface:
			v = x.X
		case *ssa.ChangeInterface:
			v = x.X
		default:
			return v, fr
		}
	}
	return v, fr
}

func decoderDisallows(dec ssa.Value, fr *frame) bool {
	dec, _ = bind(dec, fr)
	refs := dec.Referrers()
	if refs == nil {
		return false
	}
	for _, r := range *refs {
		if ci, ok := r.(ssa.CallInstruction); ok {
			if d, ok := describe(ci.Common()); ok && d.pkg == pkgJSON && d.name == "DisallowUnknownFields" {
				return true
			}
		}
	}
	return false
}

// readsBody reports whether data comes from io.ReadAll(r.Body).
func readsBody(data ssa.Value, fr *frame) bool {
	data, fr = bind(data, fr)
	ex, ok := data.(*ssa.Extract)
	if !ok {
		return false
	}
	c, ok := ex.Tuple.(*ssa.Call)
	if !ok {
		return false
	}
	d, ok := describe(c.Common())
	if !ok || d.name != "ReadAll" || (d.pkg != "io" && d.pkg != "io/ioutil") || len(d.args) != 1 {
		return false
	}
	a, afr := unwrapIface(d.args[0], fr)
	return requestField(a, afr, "Body")
}

// order reports whether instruction a runs before b (-1), after it (1) or
// whether that depends on the path (0).
func order(a, b ssa.Instruction) int {
	if a.Parent() != b.Parent() {
		return 0
	}
	ba, bb := a.Block(), b.Block()
	if ba == bb {
		for _, ins := range ba.Instrs {
			switch ins {
			case a:
				return -1
			case b:
				return 1
			}
		}
		return 0
	}
	switch {
	case ba.Dominates(bb):
		return -1
	case bb.Dominates(ba):
		return 1
	}
	return 0
}

// preceding finds the latest event of a kind that runs before ins in fr, or
// before the call that entered fr in its callers.
func preceding(fr *frame, ins ssa.Instruction, kind string) *event {
	for f, at := fr, ins; f != nil; at, f = f.site, f.parent {
		var best *event
		for i := range f.events {
			e := &f.events[i]
			if e.kind != kind || order(e.ins, at) != -1 {
				continue
			}
			if best == nil || order(best.ins, e.ins) == -1 {
				best = e
			}
		}
		if best != nil {
			return best
		}
		if f.site == nil {
			break
		}
	}
	return nil
}

// pair turns the net/http writes of a frame into responses: a body write
// takes the status of the WriteHeader that precedes it, or net/http's
// implicit 200.
func (st *routeState) pair(fr *frame) {
	for i := range fr.events {
		e := &fr.events[i]
		if e.kind != "body" {
			continue
		}
		ct := e.ct
		if c := preceding(fr, e.ins, "ctype"); c != nil && c.ct != "" {
			ct = c.ct
		}
		if h := preceding(fr, e.ins, "header"); h != nil {
			h.used = true
			st.respond(fr, e.ins, h.status, ct, e.shape, "WriteHeader + "+e.source, false, "")
			continue
		}
		st.respond(fr, e.ins, ints{vals: []int64{200}, complete: true}, ct, e.shape, e.source, true,
			"no WriteHeader before the write: net/http answers 200 unless something wrote the header earlier")
	}
}

// pairHeaders adds the WriteHeader calls not followed by a body.
func (st *routeState) pairHeaders(fr *frame) {
	for i := range fr.events {
		e := &fr.events[i]
		if e.kind == "header" && !e.used {
			st.respond(fr, e.ins, e.status, "", nil, "WriteHeader", false, "")
		}
	}
}

// Servers finds listen addresses in the program: http.ListenAndServe,
// (*http.Server).ListenAndServe with a constant Addr, gin's Run and echo's
// Start.
func Servers(p *flow.Program) []model.Server {
	var out []model.Server
	seen := map[string]bool{}
	add := func(addr string, dynamic bool, ins ssa.Instruction, details string) {
		file, line, _ := p.Pos(ins.Pos())
		key := addr + "@" + file + ":" + itoa(line)
		if seen[key] {
			return
		}
		seen[key] = true
		s := model.Server{Addr: addr, Dynamic: dynamic, Evidence: evidence.Evidence{Kind: evidence.Static, Basis: evidence.BasisStatic,
			Source: "go/ssa", File: file, Line: line, Details: details}}
		if !dynamic {
			host, port, _ := strings.Cut(addr, ":")
			if port != "" {
				if host == "" || host == "0.0.0.0" {
					host = "localhost"
				}
				s.URL = "http://" + host + ":" + port
			}
		}
		out = append(out, s)
	}
	addrOf := func(v ssa.Value, ins ssa.Instruction, details string) {
		if s, ok := stringValue(v, nil); ok {
			add(s, false, ins, details)
			return
		}
		add(stringPrefix(v, nil)+"{dynamic}", true, ins, details+" (address computed at runtime)")
	}
	for _, fn := range p.Funcs() {
		for _, b := range fn.Blocks {
			for _, ins := range b.Instrs {
				switch x := ins.(type) {
				case ssa.CallInstruction:
					d, ok := describe(x.Common())
					if !ok {
						continue
					}
					switch {
					case d.pkg == pkgHTTP && d.typ == "" && (d.name == "ListenAndServe" || d.name == "ListenAndServeTLS") && len(d.args) > 0:
						addrOf(d.args[0], ins, "http."+d.name)
					case d.pkg == pkgGin && d.typ == "Engine" && d.name == "Run":
						if len(d.args) == 1 {
							if elems, ok := flow.VariadicElems(d.args[0]); ok {
								if len(elems) == 0 {
									add(":8080", false, ins, "gin Run() without an address: :8080 unless the PORT variable is set")
								} else {
									addrOf(elems[0], ins, "(*gin.Engine).Run")
								}
							}
						}
					case (d.pkg == pkgEcho || d.pkg == pkgEchoOld) && d.typ == "Echo" && d.name == "Start" && len(d.args) == 1:
						addrOf(d.args[0], ins, "(*echo.Echo).Start")
					}
				case *ssa.Store:
					// http.Server{Addr: ...}
					if fa, ok := x.Addr.(*ssa.FieldAddr); ok && namedPath(fa.X.Type()) == "net/http.Server" {
						if st, ok := fa.X.Type().(*types.Pointer).Elem().Underlying().(*types.Struct); ok && st.Field(fa.Field).Name() == "Addr" {
							addrOf(x.Val, ins, "http.Server{Addr: ...}")
						}
					}
				}
			}
		}
	}
	return out
}
