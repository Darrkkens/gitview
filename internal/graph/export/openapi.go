package export

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/athos/gitview/internal/graph/model"
)

// OpenAPI writes the HTTP catalog as an OpenAPI 3.1 document. Only what the
// analysis identified is described; the rest is kept in x-gitview-*
// extensions (unresolved routes, unknown statuses, evidence) instead of
// being filled in.
func OpenAPI(w io.Writer, g *model.Graph) error {
	doc := openAPIDoc(g)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}

type obj = map[string]any

var nonName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// componentNames gives each named schema a readable, unique component name.
func componentNames(defs map[string]*model.Shape) map[string]string {
	keys := make([]string, 0, len(defs))
	for k := range defs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := map[string]string{}
	used := map[string]bool{}
	for _, k := range keys {
		name := nonName.ReplaceAllString(defs[k].Name, "_")
		if strings.Contains(k, "#") {
			name += "_" + k[strings.LastIndexByte(k, '#')+1:]
		}
		if used[name] {
			name = nonName.ReplaceAllString(k, "_")
		}
		used[name] = true
		out[k] = name
	}
	return out
}

// enforcement lists, for each named schema, the tag families applied by
// every request body that uses it (directly or nested): a field is
// required in the document only when all of them require it.
func enforcement(h *model.HTTP) map[string][]string {
	out := map[string][]string{}
	seen := map[string]bool{}
	var mark func(sh *model.Shape, enf []string, depth int)
	mark = func(sh *model.Shape, enf []string, depth int) {
		if sh == nil || depth > 12 {
			return
		}
		if sh.Ref != "" {
			if prev, ok := out[sh.Ref]; ok {
				out[sh.Ref] = intersect(prev, enf)
			} else {
				out[sh.Ref] = enf
			}
			key := sh.Ref + "|" + strings.Join(enf, ",")
			if seen[key] {
				return
			}
			seen[key] = true
			sh = h.Schemas[sh.Ref]
			if sh == nil {
				return
			}
		}
		for _, f := range sh.Fields {
			mark(f.Schema, enf, depth+1)
		}
		mark(sh.Items, enf, depth+1)
		mark(sh.Values, enf, depth+1)
	}
	for _, r := range h.Routes {
		if r.Body != nil {
			mark(r.Body.Schema, r.Body.Enforces, 0)
		}
	}
	return out
}

func intersect(a, b []string) []string {
	var out []string
	for _, x := range a {
		for _, y := range b {
			if x == y {
				out = append(out, x)
			}
		}
	}
	return out
}

type schemaConv struct {
	names map[string]string
	enf   map[string][]string
}

func (c schemaConv) schema(sh *model.Shape, enforced []string) obj {
	if sh == nil {
		return obj{}
	}
	if sh.Ref != "" {
		return obj{"$ref": "#/components/schemas/" + c.names[sh.Ref]}
	}
	out := obj{}
	var desc []string
	switch sh.Type {
	case model.TypeObject:
		out["type"] = "object"
		if len(sh.Fields) > 0 {
			props := obj{}
			var req []string
			for _, f := range sh.Fields {
				fs := c.schema(f.Schema, enforced)
				if d := fieldDesc(f, enforced); d != "" {
					fs = withDesc(fs, d)
				}
				c.applyRules(fs, f, enforced)
				props[f.Name] = fs
				if f.Required(enforced) {
					req = append(req, f.Name)
				}
			}
			out["properties"] = props
			if len(req) > 0 {
				out["required"] = req
			}
		}
		if sh.Values != nil {
			out["additionalProperties"] = c.schema(sh.Values, enforced)
		}
	case model.TypeArray:
		out["type"] = "array"
		out["items"] = c.schema(sh.Items, enforced)
	case model.TypeString, model.TypeInteger, model.TypeNumber, model.TypeBoolean:
		out["type"] = sh.Type
	default:
		desc = append(desc, "type not determinable statically")
	}
	if sh.Format != "" && sh.Type != model.TypeUnknown {
		out["format"] = sh.Format
	}
	if sh.Nullable {
		if t, ok := out["type"].(string); ok {
			out["type"] = []string{t, "null"}
		}
	}
	if sh.GoType != "" {
		out["x-go-type"] = sh.GoType
	}
	if sh.Note != "" {
		desc = append(desc, sh.Note)
	}
	if sh.Partial {
		out["x-gitview-partial"] = true
	}
	if len(desc) > 0 {
		out["description"] = strings.Join(desc, "; ")
	}
	return out
}

func withDesc(s obj, d string) obj {
	if _, isRef := s["$ref"]; isRef {
		// Sibling keywords of $ref are allowed in OpenAPI 3.1.
		s = obj{"$ref": s["$ref"]}
	}
	if old, ok := s["description"].(string); ok && old != "" {
		d = old + "; " + d
	}
	s["description"] = d
	return s
}

func fieldDesc(f model.Field, enforced []string) string {
	var parts []string
	if f.Embedded != "" {
		parts = append(parts, "promoted from the embedded "+f.Embedded)
	}
	if f.Conditional {
		parts = append(parts, "only present on some paths")
	}
	if len(f.Rules) > 0 {
		var rs []string
		for _, r := range f.Rules {
			s := r.Name
			if r.Param != "" {
				s += "=" + r.Param
			}
			rs = append(rs, s)
		}
		applied := "declared"
		for _, r := range f.Rules {
			for _, e := range enforced {
				if r.Source == e {
					applied = "applied"
				}
			}
		}
		parts = append(parts, fmt.Sprintf("%s tag rules (%s): %s", f.Rules[0].Source, applied, strings.Join(rs, ",")))
	}
	return strings.Join(parts, "; ")
}

// applyRules maps enforced validation rules to JSON Schema keywords.
func (c schemaConv) applyRules(s obj, f model.Field, enforced []string) {
	if _, isRef := s["$ref"]; isRef {
		return
	}
	isEnforced := func(r model.Rule) bool {
		for _, e := range enforced {
			if r.Source == e {
				return true
			}
		}
		return false
	}
	str := f.Schema != nil && f.Schema.Type == model.TypeString
	num := f.Schema != nil && (f.Schema.Type == model.TypeInteger || f.Schema.Type == model.TypeNumber)
	arr := f.Schema != nil && f.Schema.Type == model.TypeArray
	for _, r := range f.Rules {
		if !isEnforced(r) {
			continue
		}
		n, nerr := strconv.ParseFloat(r.Param, 64)
		switch {
		case r.Name == "email" && str:
			s["format"] = "email"
		case (r.Name == "url" || r.Name == "uri") && str:
			s["format"] = "uri"
		case (r.Name == "uuid" || r.Name == "uuid4") && str:
			s["format"] = "uuid"
		case r.Name == "oneof" && r.Param != "":
			var vals []any
			for _, v := range strings.Fields(r.Param) {
				if num {
					if x, err := strconv.ParseFloat(v, 64); err == nil {
						vals = append(vals, x)
						continue
					}
				}
				vals = append(vals, v)
			}
			s["enum"] = vals
		case nerr != nil:
		case (r.Name == "min" || r.Name == "gte") && str:
			s["minLength"] = int(n)
		case (r.Name == "max" || r.Name == "lte") && str:
			s["maxLength"] = int(n)
		case r.Name == "len" && str:
			s["minLength"], s["maxLength"] = int(n), int(n)
		case (r.Name == "min" || r.Name == "gte") && num:
			s["minimum"] = n
		case (r.Name == "max" || r.Name == "lte") && num:
			s["maximum"] = n
		case r.Name == "gt" && num:
			s["exclusiveMinimum"] = n
		case r.Name == "lt" && num:
			s["exclusiveMaximum"] = n
		case r.Name == "min" && arr:
			s["minItems"] = int(n)
		case r.Name == "max" && arr:
			s["maxItems"] = int(n)
		}
	}
}

var anyMethods = []string{"get", "post", "put", "patch", "delete"}

func operationID(r *model.Route, used map[string]bool) string {
	base := "route"
	if r.Handler != nil && r.Handler.Name != "" && r.Handler.Name != "unresolved" {
		base = nonName.ReplaceAllString(strings.NewReplacer("(*", "", "(", "", ")", "", ".", "_").Replace(r.Handler.Name), "_")
	}
	id := base
	for i := 2; used[id]; i++ {
		id = fmt.Sprintf("%s_%d", base, i)
	}
	used[id] = true
	return id
}

func openAPIDoc(g *model.Graph) obj {
	h := g.HTTP
	doc := obj{"openapi": "3.1.0"}
	info := obj{
		"title":   g.Repository + " API (GitView static analysis)",
		"version": g.Version,
		"description": "Generated by GitView from the Go source by static analysis: routes, parameters, bodies and responses " +
			"identified in the handler chains, with their evidence in x-gitview-* extensions. Nothing was executed; " +
			"statuses and fields that could not be determined are marked, not guessed.",
	}
	if g.Version == "" {
		info["version"] = "0"
	}
	doc["info"] = info
	if h == nil {
		doc["paths"] = obj{}
		return doc
	}
	names := componentNames(h.Schemas)
	conv := schemaConv{names: names, enf: enforcement(h)}
	var servers []any
	seenURL := map[string]bool{}
	for _, s := range h.Servers {
		if s.URL != "" && !seenURL[s.URL] {
			seenURL[s.URL] = true
			servers = append(servers, obj{"url": s.URL, "description": fmt.Sprintf("%s (%s:%d)", s.Evidence.Details, s.Evidence.File, s.Evidence.Line)})
		}
	}
	if len(servers) > 0 {
		doc["servers"] = servers
	}
	var dynamicServers []any
	for _, s := range h.Servers {
		if s.Dynamic {
			dynamicServers = append(dynamicServers, obj{"addr": s.Addr, "file": s.Evidence.File, "line": s.Evidence.Line, "details": s.Evidence.Details})
		}
	}
	if len(dynamicServers) > 0 {
		doc["x-gitview-dynamic-servers"] = dynamicServers
	}

	paths := obj{}
	schemes := obj{}
	var unresolved []any
	used := map[string]bool{}
	for _, r := range h.Routes {
		if strings.Contains(r.Template, "{dynamic}") || strings.Contains(r.Template, "{unknown}") || r.Method == "?" {
			unresolved = append(unresolved, obj{"method": r.Method, "path": r.Path, "file": r.File, "line": r.Line,
				"reasons": r.Reasons, "id": r.ID})
			continue
		}
		item, _ := paths[r.Template].(obj)
		if item == nil {
			item = obj{}
			paths[r.Template] = item
		}
		methods := []string{strings.ToLower(r.Method)}
		if r.Method == "ANY" {
			methods = anyMethods
		}
		for _, m := range methods {
			if _, dup := item[m]; dup {
				ops, _ := item["x-gitview-duplicates"].([]any)
				item["x-gitview-duplicates"] = append(ops, obj{"method": r.Method, "id": r.ID, "file": r.File, "line": r.Line})
				continue
			}
			item[m] = operation(r, conv, h, schemes, used)
		}
	}
	doc["paths"] = paths
	comps := obj{}
	keys := make([]string, 0, len(h.Schemas))
	for k := range h.Schemas {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		cs := obj{}
		for _, k := range keys {
			s := conv.schema(&model.Shape{Type: h.Schemas[k].Type, Fields: h.Schemas[k].Fields, Partial: h.Schemas[k].Partial,
				Note: h.Schemas[k].Note, GoType: h.Schemas[k].GoType}, conv.enf[k])
			if d := h.Schemas[k]; d.File != "" {
				s["x-gitview-declared"] = fmt.Sprintf("%s:%d", d.File, d.Line)
			}
			cs[names[k]] = s
		}
		comps["schemas"] = cs
	}
	if len(schemes) > 0 {
		comps["securitySchemes"] = schemes
	}
	if len(comps) > 0 {
		doc["components"] = comps
	}
	if len(unresolved) > 0 {
		doc["x-gitview-unresolved-routes"] = unresolved
	}
	doc["x-gitview"] = obj{"schema": g.Schema, "generated": g.Generated, "completeness": g.Completeness, "notes": h.Notes}
	return doc
}

func operation(r *model.Route, conv schemaConv, h *model.HTTP, schemes obj, used map[string]bool) obj {
	op := obj{"operationId": operationID(r, used), "summary": r.Key, "tags": []string{r.Group}}
	var desc []string
	if r.Handler != nil {
		desc = append(desc, fmt.Sprintf("Handler `%s` (%s:%d).", r.Handler.Name, r.Handler.File, r.Handler.Line))
	}
	if len(r.Middlewares) > 0 {
		var ms []string
		for _, m := range r.Middlewares {
			ms = append(ms, m.Name)
		}
		desc = append(desc, "Middleware: "+strings.Join(ms, ", ")+".")
	}
	desc = append(desc, fmt.Sprintf("Registered at %s:%d (%s).", r.File, r.Line, r.Framework))
	op["description"] = strings.Join(desc, " ")

	var params []any
	formProps := obj{}
	var formFile bool
	for _, p := range r.Params {
		if p.In == "form" {
			fs := conv.schema(p.Schema, nil)
			if p.Schema != nil && p.Schema.Format == "binary" {
				formFile = true
			}
			formProps[p.Name] = withDesc(fs, "read with "+p.Source)
			continue
		}
		po := obj{"name": p.Name, "in": p.In, "schema": conv.schema(p.Schema, nil)}
		d := "read with " + p.Source
		if p.From != "" && r.Handler != nil && p.From != r.Handler.Name {
			d += " by " + p.From
		}
		if p.Note != "" {
			d += "; " + p.Note
		}
		po["description"] = d
		switch {
		case p.In == "path":
			po["required"] = true
		case p.Required == "yes":
			po["required"] = true
		case p.Required == "unknown":
			po["x-gitview-required"] = "unknown"
		}
		if p.Default != "" {
			po["schema"] = withDefault(po["schema"].(obj), p.Default)
		}
		params = append(params, po)
	}
	if len(params) > 0 {
		op["parameters"] = params
	}
	if b := r.Body; b != nil {
		content := obj{}
		for _, ct := range b.ContentTypes {
			mt := obj{}
			if b.Schema != nil {
				mt["schema"] = conv.schema(b.Schema, b.Enforces)
			} else if len(formProps) > 0 {
				mt["schema"] = obj{"type": "object", "properties": formProps}
			}
			if r.Example != nil && r.Example.Body != "" && strings.Contains(ct, "json") {
				var ex any
				if json.Unmarshal([]byte(r.Example.Body), &ex) == nil {
					mt["example"] = ex
				}
			}
			content[ct] = mt
		}
		if len(content) > 0 {
			rb := obj{"content": content, "x-gitview-source": b.Source}
			if len(b.Notes) > 0 {
				rb["description"] = strings.Join(b.Notes, "; ")
			}
			op["requestBody"] = rb
		}
	} else if len(formProps) > 0 {
		ct := "application/x-www-form-urlencoded"
		if formFile {
			ct = "multipart/form-data"
		}
		op["requestBody"] = obj{"content": obj{ct: obj{"schema": obj{"type": "object", "properties": formProps}}}}
	}

	resps := obj{}
	byStatus := map[string][]model.Response{}
	var order []string
	for _, x := range r.Responses {
		k := strconv.Itoa(x.Status)
		if x.Status == 0 {
			k = "default"
		}
		if _, ok := byStatus[k]; !ok {
			order = append(order, k)
		}
		byStatus[k] = append(byStatus[k], x)
	}
	for _, k := range order {
		list := byStatus[k]
		var from []string
		content := obj{}
		var ev []any
		for _, x := range list {
			from = append(from, x.From)
			ev = append(ev, obj{"source": x.Source, "from": x.From, "via": x.Via, "basis": x.Basis, "confidence": x.Confidence,
				"file": firstFile(x), "line": firstLine(x), "note": x.Note})
			if x.Schema == nil {
				continue
			}
			ct := x.ContentType
			if ct == "" {
				ct = "application/octet-stream"
			}
			s := conv.schema(x.Schema, nil)
			if prev, ok := content[ct].(obj); ok {
				ps := prev["schema"].(obj)
				if alts, ok := ps["oneOf"].([]any); ok {
					if !containsSchema(alts, s) {
						ps["oneOf"] = append(alts, s)
					}
				} else if !sameJSON(ps, s) {
					prev["schema"] = obj{"oneOf": []any{ps, s}}
				}
				continue
			}
			content[ct] = obj{"schema": s}
		}
		d := strings.TrimSpace(list[0].StatusText)
		if k == "default" {
			d = "Status not determinable statically"
		}
		ro := obj{"description": d + " — from " + strings.Join(unique(from), ", "), "x-gitview-evidence": ev}
		if len(content) > 0 {
			ro["content"] = content
		}
		resps[k] = ro
	}
	if len(resps) == 0 {
		resps["default"] = obj{"description": "No response identified statically", "x-gitview-unknown": true}
	}
	op["responses"] = resps

	if a := r.Auth; a != nil && a.Recognized {
		name := ""
		switch a.Scheme {
		case "bearer":
			name = "bearerAuth"
			schemes[name] = obj{"type": "http", "scheme": "bearer"}
		case "basic":
			name = "basicAuth"
			schemes[name] = obj{"type": "http", "scheme": "basic"}
		case "cookie":
			if c := strings.TrimPrefix(a.Credential, "cookie "); !strings.Contains(c, " ") {
				name = "cookie_" + nonName.ReplaceAllString(c, "_")
				schemes[name] = obj{"type": "apiKey", "in": "cookie", "name": c}
			}
		}
		if name != "" {
			op["security"] = []any{obj{name: []string{}}}
		}
		op["x-gitview-auth"] = obj{"by": a.By, "credential": a.Credential, "scheme": a.Scheme, "note": a.Note}
	}
	op["x-gitview-evidence"] = obj{"route": r.ID, "file": r.File, "line": r.Line, "framework": r.Framework, "status": r.Status,
		"reasons": r.Reasons, "unresolvedCalls": r.UnresolvedCalls}
	return op
}

func withDefault(s obj, def string) obj {
	if _, isRef := s["$ref"]; isRef {
		return s
	}
	switch s["type"] {
	case "integer":
		if n, err := strconv.ParseInt(def, 10, 64); err == nil {
			s["default"] = n
			return s
		}
	case "number":
		if n, err := strconv.ParseFloat(def, 64); err == nil {
			s["default"] = n
			return s
		}
	case "string":
		s["default"] = def
		return s
	}
	s["description"] = "default " + strconv.Quote(def)
	return s
}

func firstFile(x model.Response) string {
	if len(x.Evidence) > 0 {
		return x.Evidence[0].File
	}
	return ""
}

func firstLine(x model.Response) int {
	if len(x.Evidence) > 0 {
		return x.Evidence[0].Line
	}
	return 0
}

func unique(list []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range list {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func sameJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func containsSchema(list []any, s any) bool {
	for _, x := range list {
		if sameJSON(x, s) {
			return true
		}
	}
	return false
}
