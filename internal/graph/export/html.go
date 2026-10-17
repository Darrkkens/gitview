package export

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"sort"

	"github.com/athos/gitview/internal/graph/model"
)

// The page embeds two vendored libraries (see assets/README.md) and its own
// script. Every script is inline and pinned by its SHA-256 in the
// Content-Security-Policy; default-src 'none' blocks network requests, so
// the file works from file:// and never contacts a server.
var (
	//go:embed assets/graphology-0.26.0.umd.min.js
	graphologyJS string
	//go:embed assets/sigma-3.0.2.min.js
	sigmaJS string
	//go:embed assets/graph.js
	appJS string
	//go:embed assets/graph.css
	appCSS string
)

func scriptHash(js string) string {
	sum := sha256.Sum256([]byte(js))
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}

// CSP is the page's Content-Security-Policy.
var CSP = fmt.Sprintf("default-src 'none'; script-src %s %s %s; style-src 'unsafe-inline'; img-src data:; base-uri 'none'; form-action 'none'",
	scriptHash(graphologyJS), scriptHash(sigmaJS), scriptHash(appJS))

// HTMLOptions configure the initial view of the page.
type HTMLOptions struct {
	Level    string // initial Overview level (packages by default)
	MaxNodes int    // nodes drawn at once before the view keeps the most connected
	MaxEdges int
}

// HTML writes the interactive page. The whole dataset is embedded (the
// views aggregate and filter it in the browser), in a compact form: string
// tables for kinds and node indices instead of IDs in edges.
func HTML(w io.Writer, g *model.Graph, opt HTMLOptions) error {
	if opt.Level == "" {
		opt.Level = LevelPackages
	}
	if opt.MaxNodes <= 0 {
		opt.MaxNodes = 5000
	}
	if opt.MaxEdges <= 0 {
		opt.MaxEdges = 25000
	}
	data, err := json.Marshal(compact(g, opt))
	if err != nil {
		return err
	}
	title := "Repository Graph · " + g.Repository
	var buf bytes.Buffer
	err = pageTmpl.Execute(&buf, struct {
		Title                     string
		CSP                       string
		CSS                       template.CSS
		Graphology, Sigma, App    template.JS
		Data                      template.JS
		Repository, Generated     string
		Nodes, Edges, Communities int
	}{title, CSP, template.CSS(appCSS), template.JS(graphologyJS), template.JS(sigmaJS), template.JS(appJS), template.JS(data),
		g.Repository, g.Generated, len(g.Nodes), len(g.Edges), len(g.Communities)})
	if err != nil {
		return err
	}
	_, err = w.Write(buf.Bytes())
	return err
}

type table struct {
	idx  map[string]int
	list []string
}

func (t *table) id(s string) int {
	if i, ok := t.idx[s]; ok {
		return i
	}
	if t.idx == nil {
		t.idx = map[string]int{}
	}
	t.idx[s] = len(t.list)
	t.list = append(t.list, s)
	return t.idx[s]
}

func compact(g *model.Graph, opt HTMLOptions) map[string]any {
	var kinds, etypes, confs, esrc table
	pos := make(map[string]int, len(g.Nodes))
	for i, n := range g.Nodes {
		pos[n.ID] = i
	}
	ref := func(id string) int {
		if i, ok := pos[id]; ok && id != "" {
			return i
		}
		return -1
	}
	b2i := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}
	nodes := make([][]any, len(g.Nodes))
	for i, n := range g.Nodes {
		var meta any
		if len(n.Metadata) > 0 || len(n.Annotations) > 0 {
			m := map[string]any{}
			for k, v := range n.Metadata {
				m[k] = v
			}
			if len(n.Annotations) > 0 {
				m["annotations"] = n.Annotations
			}
			meta = m
		}
		nodes[i] = []any{n.ID, kinds.id(n.Type), n.Name, n.Qualified, n.Package, n.File, n.Line, n.EndLine, n.Signature,
			b2i(n.External), ref(n.Parent), ref(n.Group), n.Community, n.Degree, n.X, n.Y, meta}
	}
	edges := make([][]any, len(g.Edges))
	for i, e := range g.Edges {
		file, line, src := "", 0, ""
		if e.Evidence != nil {
			file, line, src = e.Evidence.File, e.Evidence.Line, e.Evidence.Source
		}
		var meta any
		if len(e.Metadata) > 0 || len(e.Annotations) > 0 {
			m := map[string]any{}
			for k, v := range e.Metadata {
				m[k] = v
			}
			if len(e.Annotations) > 0 {
				m["annotations"] = e.Annotations
			}
			meta = m
		}
		edges[i] = []any{pos[e.Source], pos[e.Target], etypes.id(e.Type), confs.id(e.Confidence), e.Weight, file, line, e.Note, meta, esrc.id(src)}
	}
	comms := make([][]any, len(g.Communities))
	for i, c := range g.Communities {
		comms[i] = []any{c.ID, c.Level, c.Label, c.Size, c.Members}
	}
	byType := make([]string, 0, len(g.Stats.NodesByType))
	for k := range g.Stats.NodesByType {
		byType = append(byType, k)
	}
	sort.Strings(byType)
	return map[string]any{
		"schema": g.Schema, "repository": g.Repository, "version": g.Version, "generated": g.Generated,
		"modules": g.Modules, "stats": g.Stats, "completeness": g.Completeness, "method": g.Method,
		"initial": map[string]any{"level": opt.Level, "maxNodes": opt.MaxNodes, "maxEdges": opt.MaxEdges},
		"kinds":   kinds.list, "etypes": etypes.list, "confs": confs.list, "esrc": esrc.list,
		"communities": comms, "nodes": nodes, "edges": edges,
	}
}

var pageTmpl = template.Must(template.New("graph").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="Content-Security-Policy" content="{{.CSP}}">
<meta name="referrer" content="no-referrer">
<title>{{.Title}}</title>
<style>{{.CSS}}</style>
</head>
<body>
<header id="bar">
  <div class="brand"><strong>Repository Graph</strong><span class="repo" id="repo">{{.Repository}}</span></div>
  <nav class="crumbs" id="crumbs" aria-label="Navigation"></nav>
  <div class="counts" id="counts">{{.Nodes}} nodes · {{.Edges}} edges · {{.Communities}} communities</div>
  <div class="tools" id="tools"></div>
</header>
<main id="app">
  <section id="stage" aria-label="Graph">
    <div id="graph"></div>
    <div id="msg" class="msg" role="status"></div>
    <div id="legend" class="legend"></div>
    <div class="zoom"><button id="zin" title="Zoom in (+)">+</button><button id="zout" title="Zoom out (-)">−</button><button id="fit" title="Center the graph (f)">⤢</button></div>
  </section>
  <aside id="side">
    <div class="search"><input id="q" type="search" placeholder="Search functions, types, packages, files…  ( / )" autocomplete="off" spellcheck="false" aria-label="Search nodes"><ul id="suggest" role="listbox" hidden></ul></div>
    <div id="panel"><p class="muted">This report needs JavaScript to display the graph.</p></div>
  </aside>
</main>
<footer id="foot">Generated by GitView on {{.Generated}} · offline file, no network requests · static analysis: an edge is a relation found in the code, not an execution observed at runtime.</footer>
<script type="application/json" id="graph-data">{{.Data}}</script>
<script>{{.Graphology}}</script>
<script>{{.Sigma}}</script>
<script>{{.App}}</script>
</body>
</html>
`))
