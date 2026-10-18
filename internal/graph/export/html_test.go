package export

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"html"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/athos/gitview/internal/graph/model"
)

func sample() *model.Graph {
	g := &model.Graph{Schema: model.Schema, Repository: "repo", Generated: "2026-01-01T00:00:00Z"}
	g.Nodes = []*model.Node{
		{ID: "package:m/a", Type: model.KindPackage, Name: "m/a", Group: "package:m/a"},
		{ID: "func:m/a.F", Type: model.KindFunc, Name: "</script><script>alert(1)</script>", Group: "package:m/a", Parent: "package:m/a",
			Metadata: map[string]any{"sql": "SELECT '<!--' "}},
	}
	g.Edges = []*model.Edge{{ID: model.EdgeID(model.EdgeContains, "package:m/a", "func:m/a.F"), Source: "package:m/a", Target: "func:m/a.F",
		Type: model.EdgeContains, Confidence: model.Confirmed}}
	return g
}

var scriptRe = regexp.MustCompile(`(?s)<script([^>]*)>(.*?)</script>`)

func TestHTMLIsOfflineAndPinned(t *testing.T) {
	var buf bytes.Buffer
	if err := HTML(&buf, sample(), HTMLOptions{}); err != nil {
		t.Fatal(err)
	}
	page := buf.String()
	m := regexp.MustCompile(`http-equiv="Content-Security-Policy" content="([^"]+)"`).FindStringSubmatch(page)
	if m == nil {
		t.Fatal("no CSP meta tag")
	}
	csp := html.UnescapeString(m[1])
	for _, want := range []string{"default-src 'none'", "base-uri 'none'", "form-action 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP missing %q: %s", want, csp)
		}
	}
	if strings.Contains(csp, "connect-src") || strings.Contains(csp, "unsafe-eval") || strings.Contains(csp, "http") || strings.Contains(csp, "worker-src") {
		t.Errorf("CSP allows more than inline pinned scripts: %s", csp)
	}
	var code, data, inert int
	for _, s := range scriptRe.FindAllStringSubmatch(page, -1) {
		switch {
		case strings.Contains(s[1], `type="application/json"`):
			data++
			var v map[string]any
			if err := json.Unmarshal([]byte(s[2]), &v); err != nil {
				t.Fatalf("data block is not JSON: %v", err)
			}
			if n := len(v["nodes"].([]any)); n != 2 {
				t.Errorf("embedded %d nodes", n)
			}
		case strings.Contains(s[1], `type="application/gzip+base64"`):
			inert++
			if !regexp.MustCompile(`^[A-Za-z0-9+/=]*$`).MatchString(s[2]) {
				t.Error("the source block is not plain base64")
			}
		default:
			code++
			if !strings.Contains(csp, scriptHash(s[2])) {
				t.Errorf("inline script %d is not pinned by the CSP", code)
			}
		}
	}
	if want := len(jsFiles) + 1; code != want || data != 1 || inert != 1 {
		t.Errorf("found %d scripts, %d data blocks and %d source blocks; want %d, 1 and 1 (the injected name must not create a script)", code, data, inert, want)
	}
	if regexp.MustCompile(`(?i)<(link|iframe|img|object|embed|base)\b`).MatchString(page) {
		t.Error("page loads external resources")
	}
	if regexp.MustCompile(`(?i)\s(src|href|action)\s*=\s*"(https?:)?//`).MatchString(page) {
		t.Error("page references a remote URL")
	}
	if regexp.MustCompile(`(?i)url\(\s*['"]?(https?:)?//`).MatchString(pageCSS) || strings.Contains(pageCSS, "@import") {
		t.Error("the styles load a remote resource")
	}
}

// Every part embedded in the binary is used, in a fixed order, and none of
// them can end the script or style element it is inlined in.
func TestPagePartsAreListed(t *testing.T) {
	check := func(dir string, entries []os.DirEntry, listed []string) {
		var files []string
		for _, e := range entries {
			files = append(files, e.Name())
		}
		want := append([]string(nil), listed...)
		sort.Strings(files)
		sort.Strings(want)
		if strings.Join(files, ",") != strings.Join(want, ",") {
			t.Errorf("%s: embedded %v, listed %v", dir, files, want)
		}
	}
	js, _ := jsFS.ReadDir("assets/js")
	css, _ := cssFS.ReadDir("assets/css")
	check("assets/js", js, jsFiles)
	check("assets/css", css, cssFiles)
	for _, s := range scripts {
		if strings.Contains(strings.ToLower(s), "</script") {
			t.Error("a script contains </script")
		}
	}
	if strings.Contains(strings.ToLower(pageCSS), "</style") {
		t.Error("the styles contain </style")
	}
	// The vendored ELK build is the unmodified npm file (assets/README.md).
	sum := sha256.Sum256([]byte(elkJS))
	if got := hex.EncodeToString(sum[:]); got != "cbf61b0182e9085d36dcd5b392f57cc816273169ac40bde80b52b808444c5cf8" {
		t.Errorf("elk-0.11.0.bundled.js changed: sha256 %s", got)
	}
}

func TestCompactKeepsReferences(t *testing.T) {
	g := sample()
	c := compact(g, HTMLOptions{Level: LevelFunctions})
	nodes := c["nodes"].([][]any)
	edges := c["edges"].([][]any)
	if nodes[1][cParent].(int) != 0 || nodes[1][cGroup].(int) != 0 { // parent and group → index of package:m/a
		t.Errorf("parent/group = %v %v", nodes[1][cParent], nodes[1][cGroup])
	}
	if nodes[1][cName] != g.Nodes[1].Name || len(nodes[1]) != cMeta+1 {
		t.Errorf("node layout changed: %v", nodes[1])
	}
	if edges[0][0].(int) != 0 || edges[0][1].(int) != 1 {
		t.Errorf("edge endpoints = %v → %v", edges[0][0], edges[0][1])
	}
	if c["initial"].(map[string]any)["level"] != LevelFunctions {
		t.Error("initial level lost")
	}
}

func decodeBundle(t *testing.T, enc string) map[string]string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	js, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	var files map[string]string
	if err := json.Unmarshal(js, &files); err != nil {
		t.Fatal(err)
	}
	return files
}

// The source bundle holds the repository's Go files the graph has file
// nodes for, nothing else: no path outside the root, no symlink, no other
// file type.
func TestSourceBundle(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	write := func(dir, name, content string) {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(root, "a.go", "package a\n\nfunc A() {}\n")
	write(root, "sub/b.go", "package sub\n")
	write(root, "notes.txt", "secret")
	write(root, "bad.go", "\xff\xfe")
	write(outside, "x.go", "package x // outside\n")
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	g := &model.Graph{Schema: model.Schema, Repository: "repo", Root: root}
	for _, f := range []string{"a.go", "sub/b.go", "notes.txt", "bad.go", "link/x.go", "../x.go", "missing.go"} {
		g.Nodes = append(g.Nodes, &model.Node{ID: "file:" + f, Type: model.KindFile, Name: filepath.Base(f), File: f})
	}
	g.Nodes = append(g.Nodes, &model.Node{ID: "func:a.A", Type: model.KindFunc, Name: "A", File: "a.go", Line: 3})
	b, err := sources(g)
	if err != nil {
		t.Fatal(err)
	}
	files := decodeBundle(t, b.Encoded)
	if len(files) != 2 || files["a.go"] != "package a\n\nfunc A() {}\n" || files["sub/b.go"] != "package sub\n" {
		t.Errorf("bundle = %v", files)
	}
	if b.Files != 2 || len(b.Skipped) != 5 {
		t.Errorf("files %d, skipped %v", b.Files, b.Skipped)
	}
	again, _ := sources(g)
	if again.Encoded != b.Encoded {
		t.Error("the bundle is not deterministic")
	}

	var on, off bytes.Buffer
	if err := HTML(&on, g, HTMLOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := HTML(&off, g, HTMLOptions{NoSource: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(on.String(), b.Encoded) {
		t.Error("the page does not embed the bundle")
	}
	m := regexp.MustCompile(`(?s)<script type="application/gzip\+base64" id="gitview-sources">(.*?)</script>`).FindStringSubmatch(off.String())
	if m == nil || m[1] != "" || !strings.Contains(off.String(), `"disabled":true`) || strings.Contains(off.String(), "func A()") {
		t.Error("NoSource still embeds source")
	}
}
