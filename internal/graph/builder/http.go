package builder

import (
	"fmt"
	"sort"
	"strings"

	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/graph/analyzer"
	"github.com/athos/gitview/internal/graph/httpapi"
	"github.com/athos/gitview/internal/graph/model"
	"github.com/athos/gitview/internal/study"
)

// Depth of the route execution graph used for the unresolved-call count.
const journeyDepth = 6

// roles labels functions and packages with the layer Study Mode's
// architecture assigns them (handler, service, repository...), with the
// basis of each classification: STATIC from the code's structure,
// HEURISTIC from directory names.
func roles(g *model.Graph, progs []*flow.Program, index []*flow.Graph) {
	fg := &flow.Graph{}
	for _, x := range index {
		fg.Merge(x)
	}
	arch := study.Architect(progs, fg)
	idx := g.Index()
	for _, fr := range arch.Functions {
		n := idx[analyzer.FuncID(fr.ID)]
		if n == nil {
			continue
		}
		setMeta(n, "layer", fr.Layer)
		setMeta(n, "layerBasis", fr.Basis)
		if len(fr.Evidence) > 0 {
			ev := fr.Evidence[0]
			setMeta(n, "layerEvidence", fmt.Sprintf("%s (%s:%d)", ev.Text, ev.File, ev.Line))
		}
	}
	for _, pk := range arch.Packages {
		n := idx[analyzer.PackageID(pk.Path)]
		if n == nil || pk.Layer == "" || pk.Layer == study.LayerUnknown {
			continue
		}
		setMeta(n, "layer", pk.Layer)
		setMeta(n, "layerBasis", pk.Basis)
		if len(pk.Evidence) > 0 {
			setMeta(n, "layerEvidence", pk.Evidence[0].Text)
		}
	}
}

func setMeta(n *model.Node, k string, v any) {
	if n.Metadata == nil {
		n.Metadata = map[string]any{}
	}
	n.Metadata[k] = v
}

// httpCatalog reads the contracts of every route and completes them with
// what needs the whole dataset: graph IDs, execution-graph counts,
// catalog groups, examples.
func httpCatalog(g *model.Graph, progs []*flow.Program) *model.HTTP {
	h := &model.HTTP{Schemas: map[string]*model.Shape{}}
	for _, p := range progs {
		h.Routes = append(h.Routes, httpapi.Analyze(p, h.Schemas, httpapi.Options{FuncID: analyzer.FuncID})...)
		h.Servers = append(h.Servers, httpapi.Servers(p)...)
	}
	if len(h.Routes) == 0 {
		return nil
	}
	ix := httpapi.NewIndex(g)
	base, src := baseURL(h.Servers)
	if src == "placeholder" {
		h.Notes = append(h.Notes, "no constant listen address was found: examples use "+base+" (change it in the request builder)")
	}
	groups(h.Routes)
	examples := map[string]string{}
	for _, r := range h.Routes {
		if r.Handler != nil && ix.Nodes[r.Handler.ID] == nil {
			r.Handler.ID = ""
		}
		for i := range r.Middlewares {
			if ix.Nodes[r.Middlewares[i].ID] == nil {
				r.Middlewares[i].ID = ""
			}
		}
		if j := ix.Journey(r.ID, journeyDepth, 5000); j != nil {
			r.UnresolvedCalls = j.Unresolved
		}
		ex := &model.Example{BaseURL: base, BaseURLSrc: src, Valid: "compatible"}
		if r.Body != nil && r.Body.Schema != nil {
			v, valid, notes := httpapi.ExampleValue(h.Schemas, r.Body.Schema, r.Body.Enforces)
			ex.Body, ex.Valid, ex.Notes = httpapi.Indent(v), valid, notes
		}
		r.Example = ex
		ex.Curl = httpapi.Curl(httpapi.DefaultRequest(r, base))
		for i := range r.Responses {
			x := &r.Responses[i]
			if x.Schema == nil || x.Schema.Type == model.TypeUnknown || !strings.Contains(x.ContentType, "json") {
				continue
			}
			key := x.Schema.Ref
			if key == "" {
				key = fmt.Sprintf("%p", x.Schema)
			}
			if cached, ok := examples[key]; ok {
				x.Example = cached
				continue
			}
			v, _, _ := httpapi.ExampleValue(h.Schemas, x.Schema, nil)
			x.Example = httpapi.Indent(v)
			examples[key] = x.Example
		}
	}
	return h
}

// baseURL chooses the base URL of the examples: the only constant listen
// address, else the first one, else a placeholder.
func baseURL(servers []model.Server) (string, string) {
	var urls []model.Server
	for _, s := range servers {
		if s.URL != "" {
			urls = append(urls, s)
		}
	}
	sort.SliceStable(urls, func(i, j int) bool { return urls[i].Evidence.File < urls[j].Evidence.File })
	switch {
	case len(urls) == 1:
		return urls[0].URL, fmt.Sprintf("%s (%s:%d)", urls[0].Evidence.Details, urls[0].Evidence.File, urls[0].Evidence.Line)
	case len(urls) > 1:
		return urls[0].URL, fmt.Sprintf("%s (%s:%d), first of %d listen addresses", urls[0].Evidence.Details, urls[0].Evidence.File, urls[0].Evidence.Line, len(urls))
	}
	return "http://localhost:8080", "placeholder"
}

// groups assigns each route a catalog group: the first path segment after
// the prefix shared by most routes (for /api/users/:id with every route
// under /api, "/api/users"). It only uses the registered paths.
func groups(routes []*model.Route) {
	segs := make([][]string, len(routes))
	for i, r := range routes {
		for _, s := range strings.Split(strings.Trim(r.Path, "/"), "/") {
			if s == "" || strings.ContainsAny(s, ":*{") {
				break
			}
			segs[i] = append(segs[i], s)
		}
	}
	var prefix []string
	for {
		count := map[string]int{}
		for _, s := range segs {
			if len(s) > len(prefix)+1 && hasPrefix(s, prefix) {
				count[s[len(prefix)]]++
			}
		}
		best, n := "", 0
		for k, c := range count {
			if c > n || c == n && k < best {
				best, n = k, c
			}
		}
		if n*5 < len(routes)*4 { // descend only while one segment holds 80% of the routes
			break
		}
		prefix = append(prefix, best)
	}
	for i, r := range routes {
		s := segs[i]
		switch {
		case hasPrefix(s, prefix) && len(s) > len(prefix):
			r.Group = "/" + strings.Join(s[:len(prefix)+1], "/")
		case len(s) > 0:
			r.Group = "/" + s[0]
		default:
			r.Group = "/"
		}
	}
}

func hasPrefix(s, p []string) bool {
	if len(s) < len(p) {
		return false
	}
	for i := range p {
		if s[i] != p[i] {
			return false
		}
	}
	return true
}
