package export

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/athos/gitview/internal/graph/httpapi"
	"github.com/athos/gitview/internal/graph/model"
)

const postmanSchema = "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"

// Postman writes the HTTP catalog as a Postman Collection v2.1: one folder
// per catalog group, one request per route with its path variables, the
// query parameters and headers the code reads (optional ones disabled) and
// the synthetic body. Credentials are variables left empty.
func Postman(w io.Writer, g *model.Graph) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(postmanDoc(g))
}

// stableUUID derives a UUID-shaped identifier from text, so the collection
// keeps its identity across exports of the same code.
func stableUUID(s string) string {
	h := sha256.Sum256([]byte(s))
	x := hex.EncodeToString(h[:16])
	return x[0:8] + "-" + x[8:12] + "-4" + x[13:16] + "-8" + x[17:20] + "-" + x[20:32]
}

func postmanDoc(g *model.Graph) obj {
	h := g.HTTP
	coll := obj{
		"info": obj{
			"_postman_id": stableUUID("gitview:" + g.Repository),
			"name":        g.Repository + " (GitView)",
			"description": "Requests built by GitView from the Go source by static analysis. Bodies and parameter values are synthetic examples; nothing was executed and no credential is included.",
			"schema":      postmanSchema,
		},
	}
	base := "http://localhost:8080"
	baseDesc := "placeholder: no constant listen address was found"
	needToken := false
	var folders []any
	if h != nil {
		if len(h.Routes) > 0 && h.Routes[0].Example != nil {
			base, baseDesc = h.Routes[0].Example.BaseURL, h.Routes[0].Example.BaseURLSrc
		}
		byGroup := map[string][]any{}
		var groupNames []string
		for _, r := range h.Routes {
			if r.Method == "?" {
				continue
			}
			if _, ok := byGroup[r.Group]; !ok {
				groupNames = append(groupNames, r.Group)
			}
			item, token := postmanItem(r)
			needToken = needToken || token
			byGroup[r.Group] = append(byGroup[r.Group], item)
		}
		sort.Strings(groupNames)
		for _, gname := range groupNames {
			folders = append(folders, obj{"name": gname, "item": byGroup[gname]})
		}
	}
	if folders == nil {
		folders = []any{}
	}
	coll["item"] = folders
	vars := []any{obj{"key": "baseUrl", "value": base, "type": "string", "description": baseDesc}}
	if needToken {
		vars = append(vars, obj{"key": "token", "value": "", "type": "string", "description": "fill in a token for your environment; GitView never exports credentials"})
	}
	coll["variable"] = vars
	return coll
}

func postmanItem(r *model.Route) (obj, bool) {
	q := httpapi.DefaultRequest(r, "{{baseUrl}}")
	path := strings.Trim(r.Template, "/")
	var segs []string
	var vars []any
	for _, s := range strings.Split(path, "/") {
		if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
			name := s[1 : len(s)-1]
			segs = append(segs, ":"+name)
			v := obj{"key": name, "value": q.PathVal[name]}
			if name == "dynamic" {
				v["description"] = "the path is computed at runtime; fill in the real segment"
			}
			vars = append(vars, v)
			continue
		}
		if s != "" {
			segs = append(segs, s)
		}
	}
	var query []any
	var headers []any
	for _, p := range r.Params {
		desc := "read with " + p.Source
		if p.Note != "" {
			desc += "; " + p.Note
		}
		switch p.In {
		case "query":
			query = append(query, obj{"key": p.Name, "value": httpapi.ExampleParam(p), "description": desc, "disabled": p.Required != "yes" && p.Default == ""})
		case "header":
			if strings.EqualFold(p.Name, "Authorization") || strings.EqualFold(p.Name, "Content-Type") {
				continue
			}
			headers = append(headers, obj{"key": p.Name, "value": "<" + strings.ToLower(p.Name) + ">", "description": desc, "disabled": p.Required != "yes"})
		}
	}
	token := false
	var auth obj
	for _, hd := range q.Headers {
		switch {
		case hd[0] == "Authorization" && strings.HasPrefix(hd[1], "Bearer"):
			auth = obj{"type": "bearer", "bearer": []any{obj{"key": "token", "value": "{{token}}", "type": "string"}}}
			token = true
		case hd[0] == "Authorization":
			headers = append(headers, obj{"key": hd[0], "value": hd[1]})
		default:
			headers = append(headers, obj{"key": hd[0], "value": hd[1]})
		}
	}
	raw := "{{baseUrl}}"
	if len(segs) > 0 {
		raw += "/" + strings.Join(segs, "/")
	}
	if strings.HasSuffix(r.Template, "/") && len(segs) > 0 {
		raw += "/"
	}
	url := obj{"raw": raw, "host": []string{"{{baseUrl}}"}, "path": segs}
	if len(vars) > 0 {
		url["variable"] = vars
	}
	if len(query) > 0 {
		url["query"] = query
		var on []string
		for _, x := range query {
			if !x.(obj)["disabled"].(bool) {
				on = append(on, x.(obj)["key"].(string)+"="+x.(obj)["value"].(string))
			}
		}
		if len(on) > 0 {
			url["raw"] = raw + "?" + strings.Join(on, "&")
		}
	}
	method := q.Method
	req := obj{"method": method, "header": nonNil(headers), "url": url}
	if auth != nil {
		req["auth"] = auth
	}
	if r.Body != nil {
		switch {
		case q.Body != "":
			lang := "json"
			if len(r.Body.ContentTypes) > 0 && strings.Contains(r.Body.ContentTypes[0], "xml") {
				lang = "xml"
			}
			if r.Example != nil && r.Example.Body != "" {
				req["body"] = obj{"mode": "raw", "raw": r.Example.Body, "options": obj{"raw": obj{"language": lang}}}
			}
		case len(r.Body.ContentTypes) > 0 && strings.Contains(strings.Join(r.Body.ContentTypes, ","), "form"):
			var fields []any
			for _, p := range r.Params {
				if p.In != "form" {
					continue
				}
				f := obj{"key": p.Name, "description": "read with " + p.Source}
				if p.Schema != nil && p.Schema.Format == "binary" {
					f["type"] = "file"
				} else {
					f["type"] = "text"
					f["value"] = httpapi.ExampleParam(p)
				}
				fields = append(fields, f)
			}
			mode := "urlencoded"
			if r.Body.ContentTypes[0] == "multipart/form-data" {
				mode = "formdata"
			}
			req["body"] = obj{"mode": mode, mode: nonNil(fields)}
		}
	}
	desc := []string{fmt.Sprintf("Registered at %s:%d (%s).", r.File, r.Line, r.Framework)}
	if r.Handler != nil {
		desc = append(desc, fmt.Sprintf("Handler %s (%s:%d).", r.Handler.Name, r.Handler.File, r.Handler.Line))
	}
	if len(r.Responses) > 0 {
		var st []string
		for _, x := range r.Responses {
			s := "unknown"
			if x.Status > 0 {
				s = fmt.Sprint(x.Status)
			}
			st = append(st, s)
		}
		desc = append(desc, "Statuses identified statically: "+strings.Join(unique(st), ", ")+".")
	}
	if r.Status != model.StatusComplete {
		desc = append(desc, "Contract "+r.Status+": "+strings.Join(r.Reasons, "; ")+".")
	}
	if r.Example != nil && r.Example.Valid != "compatible" && r.Example.Body != "" {
		desc = append(desc, "The example body may not satisfy every rule: "+strings.Join(r.Example.Notes, "; ")+".")
	}
	req["description"] = strings.Join(desc, " ")
	return obj{"name": r.Key, "request": req, "response": []any{}}, token
}

func nonNil(x []any) []any {
	if x == nil {
		return []any{}
	}
	return x
}
