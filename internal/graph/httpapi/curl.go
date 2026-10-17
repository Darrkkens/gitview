package httpapi

import (
	"bytes"
	"encoding/json"
	"net/url"
	"sort"
	"strings"

	"github.com/athos/gitview/internal/graph/model"
)

// Request is a request the user configures from a contract: what a cURL
// command or a Postman item sends. Nothing here is executed.
type Request struct {
	Method  string
	BaseURL string
	Path    string            // OpenAPI template, {name} placeholders
	PathVal map[string]string // values of the placeholders
	Query   [][2]string
	Headers [][2]string
	Body    string // sent as is
}

// DefaultRequest builds the request the contract suggests: path parameters
// with example values, required query parameters, the content type of the
// body, and placeholders for recognised credentials.
func DefaultRequest(r *model.Route, baseURL string) Request {
	q := Request{Method: r.Method, BaseURL: baseURL, Path: r.Template, PathVal: map[string]string{}}
	if q.Method == "ANY" || q.Method == "?" {
		q.Method = "GET"
	}
	for _, p := range r.Params {
		switch {
		case p.In == "path":
			q.PathVal[p.Name] = ExampleParam(p)
		case p.In == "query" && (p.Required == "yes" || p.Default != ""):
			q.Query = append(q.Query, [2]string{p.Name, ExampleParam(p)})
		case p.In == "header" && p.Required == "yes":
			q.Headers = append(q.Headers, [2]string{p.Name, "<" + strings.ToLower(p.Name) + ">"})
		}
	}
	if a := r.Auth; a != nil && a.Recognized {
		switch a.Scheme {
		case "bearer":
			q.Headers = append(q.Headers, [2]string{"Authorization", "Bearer <token>"})
		case "basic":
			q.Headers = append(q.Headers, [2]string{"Authorization", "Basic <credentials>"})
		case "cookie":
			name := strings.TrimPrefix(a.Credential, "cookie ")
			if strings.Contains(name, " ") {
				name = "<cookie>"
			}
			q.Headers = append(q.Headers, [2]string{"Cookie", name + "=<value>"})
		}
	}
	if b := r.Body; b != nil && r.Example != nil && r.Example.Body != "" {
		ct := "application/json"
		if len(b.ContentTypes) > 0 {
			ct = b.ContentTypes[0]
		}
		q.Headers = append(q.Headers, [2]string{"Content-Type", ct})
		q.Body = compact(r.Example.Body)
	}
	return q
}

func compact(s string) string {
	var b bytes.Buffer
	if json.Compact(&b, []byte(s)) != nil {
		return s
	}
	return b.String()
}

// URL renders the request URL.
func (q Request) URL() string {
	path := q.Path
	keys := make([]string, 0, len(q.PathVal))
	for k := range q.PathVal {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		path = strings.ReplaceAll(path, "{"+k+"}", url.PathEscape(q.PathVal[k]))
	}
	u := strings.TrimSuffix(q.BaseURL, "/") + path
	if len(q.Query) > 0 {
		var parts []string
		for _, kv := range q.Query {
			parts = append(parts, url.QueryEscape(kv[0])+"="+url.QueryEscape(kv[1]))
		}
		u += "?" + strings.Join(parts, "&")
	}
	return u
}

// ShellQuote quotes s for POSIX shells.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Curl renders the request as a cURL command.
func Curl(q Request) string {
	var b strings.Builder
	b.WriteString("curl")
	if q.Method != "GET" {
		b.WriteString(" -X " + q.Method)
	}
	b.WriteString(" " + ShellQuote(q.URL()))
	for _, h := range q.Headers {
		b.WriteString(" \\\n  -H " + ShellQuote(h[0]+": "+h[1]))
	}
	if q.Body != "" {
		b.WriteString(" \\\n  --data-raw " + ShellQuote(q.Body))
	}
	return b.String()
}
