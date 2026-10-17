package export

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"html"
	"regexp"
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
	if strings.Contains(csp, "connect-src") || strings.Contains(csp, "unsafe-eval") || strings.Contains(csp, "http") {
		t.Errorf("CSP allows more than inline pinned scripts: %s", csp)
	}
	scripts := scriptRe.FindAllStringSubmatch(page, -1)
	var code, data int
	for _, s := range scripts {
		if strings.Contains(s[1], `type="application/json"`) {
			data++
			var v map[string]any
			if err := json.Unmarshal([]byte(s[2]), &v); err != nil {
				t.Fatalf("data block is not JSON: %v", err)
			}
			if n := len(v["nodes"].([]any)); n != 2 {
				t.Errorf("embedded %d nodes", n)
			}
			continue
		}
		code++
		sum := sha256.Sum256([]byte(s[2]))
		h := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
		if !strings.Contains(csp, h) {
			t.Errorf("inline script %d is not pinned by the CSP", code)
		}
	}
	if code != 3 || data != 1 {
		t.Errorf("found %d scripts and %d data blocks; want 3 and 1 (the injected name must not create a script)", code, data)
	}
	if regexp.MustCompile(`(?i)<(link|iframe|img|object|embed)\b`).MatchString(page) {
		t.Error("page loads external resources")
	}
	if regexp.MustCompile(`(?i)\s(src|href)\s*=\s*"(https?:)?//`).MatchString(page) {
		t.Error("page references a remote URL")
	}
}

func TestCompactKeepsReferences(t *testing.T) {
	g := sample()
	c := compact(g, HTMLOptions{Level: LevelFunctions})
	nodes := c["nodes"].([][]any)
	edges := c["edges"].([][]any)
	if nodes[1][10].(int) != 0 || nodes[1][11].(int) != 0 { // parent and group → index of package:m/a
		t.Errorf("parent/group = %v %v", nodes[1][10], nodes[1][11])
	}
	if edges[0][0].(int) != 0 || edges[0][1].(int) != 1 {
		t.Errorf("edge endpoints = %v → %v", edges[0][0], edges[0][1])
	}
	if c["initial"].(map[string]any)["level"] != LevelFunctions {
		t.Error("initial level lost")
	}
}
