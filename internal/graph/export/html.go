package export

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"strings"

	"github.com/athos/gitview/internal/graph/model"
)

// The page is assembled from reusable parts:
//
//	page/*.html     html/template partials (header, sidebar, canvas, inspector, overlays)
//	assets/css/*    the design system and the styles of each component, in cssFiles order
//	assets/js/*     one module per component, in jsFiles order, sharing the GV namespace
//	assets/vendor/  ELK.js, the layout engine (see assets/README.md)
//
// Every script is inline and pinned by its SHA-256 in the
// Content-Security-Policy; default-src 'none' blocks network requests, so
// the file works from file:// and never contacts a server.
var (
	//go:embed page/*.html
	pageFS embed.FS
	//go:embed assets/css/*.css
	cssFS embed.FS
	//go:embed assets/js/*.js
	jsFS embed.FS
	//go:embed assets/vendor/elk-0.11.0.bundled.js
	elkJS string
)

// cssFiles and jsFiles fix the order the parts are concatenated in; a test
// checks that every embedded file is listed.
var (
	cssFiles = []string{"tokens.css", "base.css", "layout.css", "components.css", "canvas.css", "sidebar.css", "inspector.css", "code.css", "http.css", "security.css"}
	jsFiles  = []string{"core.js", "data.js", "source.js", "layout.js", "render.js", "arch.js", "focus.js", "requests.js", "http.js",
		"security.js", "tree.js", "inspector.js", "search.js", "filters.js", "app.js"}
)

var (
	pageTmpl = template.Must(template.New("page").ParseFS(pageFS, "page/*.html"))
	pageCSS  = readParts(cssFS, "assets/css/", cssFiles, "")
	scripts  = append([]string{elkJS}, moduleScripts()...)
	// CSP is the page's Content-Security-Policy.
	CSP = csp(false)
)

// csp pins every script by hash. A page served by gitview audit serve may
// also call back the local server that served it ('self'), nothing else.
func csp(live bool) string {
	hashes := make([]string, len(scripts))
	for i, s := range scripts {
		hashes[i] = scriptHash(s)
	}
	connect := ""
	if live {
		connect = " connect-src 'self';"
	}
	return fmt.Sprintf("default-src 'none'; script-src %s;%s style-src 'unsafe-inline'; img-src data:; base-uri 'none'; form-action 'none'", strings.Join(hashes, " "), connect)
}

func readParts(fs embed.FS, dir string, names []string, sep string) string {
	var b strings.Builder
	for _, n := range names {
		data, err := fs.ReadFile(dir + n)
		if err != nil {
			panic("export: missing page part " + dir + n)
		}
		b.Write(data)
		b.WriteString(sep)
	}
	return b.String()
}

// moduleScripts returns one script per module; the sourceURL comment names
// it in the browser's developer tools (it loads nothing).
func moduleScripts() []string {
	out := make([]string, len(jsFiles))
	for i, n := range jsFiles {
		data, err := jsFS.ReadFile("assets/js/" + n)
		if err != nil {
			panic("export: missing page module " + n)
		}
		out[i] = string(data) + "\n//# sourceURL=gitview/" + n + "\n"
	}
	return out
}

// inertJSON re-encodes JSON so that it cannot close the script element it
// is placed in (<, > and & become \u003c…).
func inertJSON(raw json.RawMessage) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

func scriptHash(js string) string {
	sum := sha256.Sum256([]byte(js))
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}

// HTMLOptions configure the page.
type HTMLOptions struct {
	// Level is the initial exploration level of the Architecture canvas:
	// "" or "modules" (modules and their top-level directories), packages,
	// files, or functions/types/full (files expanded in the package view).
	Level    string
	MaxNodes int // elements drawn at once; beyond it the canvas asks before expanding
	MaxEdges int // relations drawn at once
	// NoSource leaves the Go source out of the page (no Source tab content).
	NoSource bool
	// Security is the CodeAudit page data (audit.Page as JSON): the page
	// then opens the Security Journey.
	Security json.RawMessage
	// Live is set when the page is served by gitview audit serve: the page
	// may then call that server (and only it) with the token.
	Live *Live
}

// Live configures a page served locally.
type Live struct {
	Token string `json:"token"`
}

// HTML writes the interactive page. The whole dataset is embedded (the
// views aggregate and filter it in the browser), plus, unless NoSource, the
// repository's Go files the graph describes, compressed.
func HTML(w io.Writer, g *model.Graph, opt HTMLOptions) error {
	if opt.MaxNodes <= 0 {
		opt.MaxNodes = 5000
	}
	if opt.MaxEdges <= 0 {
		opt.MaxEdges = 25000
	}
	c := compact(g, opt)
	var src SourceBundle
	if !opt.NoSource {
		var err error
		if src, err = sources(g); err != nil {
			return err
		}
	}
	c["sources"] = map[string]any{"embedded": src.Encoded != "", "files": src.Files, "bytes": src.Bytes, "skipped": src.Skipped, "disabled": opt.NoSource}
	data, err := json.Marshal(c) // escapes <, > and & as <…, so the block cannot close early
	if err != nil {
		return err
	}
	var tail strings.Builder
	tail.WriteString(`<script type="application/json" id="gitview-data">`)
	tail.Write(data)
	tail.WriteString("</script>\n")
	if len(opt.Security) > 0 {
		sec, err := inertJSON(opt.Security)
		if err != nil {
			return err
		}
		tail.WriteString(`<script type="application/json" id="gitview-security">`)
		tail.Write(sec)
		tail.WriteString("</script>\n")
	}
	if opt.Live != nil {
		live, _ := json.Marshal(opt.Live)
		tail.WriteString(`<script type="application/json" id="gitview-live">`)
		tail.Write(live)
		tail.WriteString("</script>\n")
	}
	// Base64 only uses [A-Za-z0-9+/=]: inert inside a non-script type.
	tail.WriteString(`<script type="application/gzip+base64" id="gitview-sources">`)
	tail.WriteString(src.Encoded)
	tail.WriteString("</script>\n")
	for _, s := range scripts {
		tail.WriteString("<script>")
		tail.WriteString(s)
		tail.WriteString("</script>\n")
	}
	title := "GitView · " + g.Repository
	if len(opt.Security) > 0 {
		title += " · Security Journey"
	}
	var buf bytes.Buffer
	err = pageTmpl.ExecuteTemplate(&buf, "page", struct {
		Title, CSP, Repository, Generated string
		CSS                               template.CSS
		Nodes, Edges, Routes              int
		Security                          bool
		Scripts                           template.HTML
	}{title, csp(opt.Live != nil), g.Repository, g.Generated, template.CSS(pageCSS),
		len(g.Nodes), len(g.Edges), g.Stats.Routes, len(opt.Security) > 0, template.HTML(tail.String())})
	if err != nil {
		return err
	}
	_, err = w.Write(buf.Bytes())
	return err
}
