package export

import (
	"encoding/json"
	"errors"
	stdhtml "html"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/athos/gitview/internal/gitdiff"
)

const goDiff = "diff --git a/svc/ação.go b/svc/ação.go\n--- a/svc/ação.go\n+++ b/svc/ação.go\n" +
	"@@ -1,2 +1,3 @@ package svc\n func A() {}\n-// old ```fence```\n+// novo: <script>alert(\"x\")</script> & ü 🚀\n+var s = `raw`\n"

func sampleReport(t *testing.T) Report {
	t.Helper()
	hunks, _ := gitdiff.Parse(goDiff)
	add, del := gitdiff.Count(hunks)
	return Report{
		Project: "gitview", Branch: "feat/<x>", Root: "/tmp/repo",
		GeneratedAt: time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC),
		Scope:       "Todos os arquivos",
		Files: []gitdiff.File{
			{Path: "svc/ação.go", Status: "M", Added: add, Deleted: del, Hunks: hunks, Raw: goDiff},
			{Path: "img/logo.png", Status: "A", Binary: true},
		},
		Tests: &TestReport{Ran: false},
	}
}

func TestMarkdown(t *testing.T) {
	md := string(renderMarkdown(sampleReport(t)))
	for _, want := range []string{
		"# GitView · gitview", "- **Branch:** `feat/<x>`", "2026-10-14 12:00:00",
		"| M | `svc/ação.go` | 2 | 1 |", "## Testes", "_Testes não executados nesta sessão._",
		"\n````diff\n@@ -1,2 +1,3 @@ package svc\n", "+// novo: <script>", "_Arquivo binário._",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q\n%s", want, md)
		}
	}
	// The fence must be longer than any backtick run inside the diff.
	if strings.Contains(md, "\n```diff") {
		t.Error("fence too short for content with ```")
	}
}

func renderHTMLString(t *testing.T, r Report) string {
	t.Helper()
	out, err := renderHTML(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestHTMLEscaping(t *testing.T) {
	r := sampleReport(t)
	r.Files[0].Path = `svc/"><img src=x onerror=alert(1)>.go`
	html := renderHTMLString(t, r)
	for _, bad := range []string{"<script>alert", "feat/<x>", "<img src=x", `"><img`} {
		if strings.Contains(html, bad) {
			t.Fatalf("unescaped content %q in HTML", bad)
		}
	}
	for _, want := range []string{"&lt;script&gt;", "feat/&lt;x&gt;", "🚀", "&lt;img src=x onerror=alert(1)&gt;"} {
		if !strings.Contains(html, want) {
			t.Errorf("html missing %q", want)
		}
	}
}

func TestHTMLStandaloneAndCSP(t *testing.T) {
	html := renderHTMLString(t, sampleReport(t))
	// The SVG namespace URI is an identifier, not a resource.
	scan := strings.ReplaceAll(html, `"http://www.w3.org/2000/svg"`, "")
	for _, ext := range []string{"http://", "https://", "<link", "@import", "src=\""} {
		if strings.Contains(scan, ext) {
			t.Errorf("report must be standalone, found %q", ext)
		}
	}
	// Every inline script must be allowed by hash, and no other script may exist.
	scripts := regexp.MustCompile(`(?s)<script>(.*?)</script>`).FindAllStringSubmatch(html, -1)
	if len(scripts) != 3 {
		t.Fatalf("expected 3 scripts, got %d", len(scripts))
	}
	csp := stdhtml.UnescapeString(regexp.MustCompile(`http-equiv="Content-Security-Policy" content="([^"]*)"`).FindStringSubmatch(html)[1])
	for _, s := range scripts {
		if h := scriptHash(s[1]); !strings.Contains(csp, "'"+h+"'") {
			t.Errorf("script hash %s missing from CSP", h)
		}
	}
	if !strings.HasPrefix(csp, "default-src 'none';") {
		t.Error("CSP must block network access")
	}
}

func TestHTMLStructure(t *testing.T) {
	html := stdhtml.UnescapeString(renderHTMLString(t, sampleReport(t)))
	for _, want := range []string{
		`<nav class="tabs"`, `href="#overview"`, `href="#files"`, `href="#tests"`,
		`<a class="frow" role="listitem" href="#file-1" data-path="svc/ação.go">`,
		`<details class="file" id="file-1" data-path="svc/ação.go" open>`,
		`data-action="copy-path" data-path="svc/ação.go"`, `data-action="copy-hunk"`,
		`data-action="expand-all"`, `data-action="theme"`, `id="file-filter"`,
		`<span class="range">@@ -1,2 +1,3 @@</span> <span class="section">package svc</span>`,
		"Binary file not shown.", "Tests were not run in this session.", "Tests not run",
		"<b>2</b> files changed", "<b>2</b> additions", "<b>1</b> deletion<",
		"@media print", "@media screen and (max-width:760px)",
		`:root[data-theme="light"] .cd .k`, `:root[data-theme="dark"] .cd .k`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("html missing %q", want)
		}
	}
}

func TestHTMLDiffLines(t *testing.T) {
	r := sampleReport(t)
	tabbed := "@@ -1,2 +1,3 @@\n \tkeep\n-\told\n+\t\tnew  trailing  \n+\n"
	hunks, _ := gitdiff.Parse(tabbed)
	r.Files = []gitdiff.File{{Path: "a.go", Status: "M", Added: 2, Deleted: 1, Hunks: hunks}}
	html := strings.ReplaceAll(renderHTMLString(t, r), "&#43;", "+")
	rowRe := regexp.MustCompile(`<tr class="l ([a-z ]+)"(?: id="[^"]+")?><td class="ln old">(\d*)</td><td class="ln new">(\d*)</td><td class="sg">(.)</td><td class="cd">(.*?)</td></tr>`)
	rows := rowRe.FindAllStringSubmatch(html, -1)
	if len(rows) != 4 {
		t.Fatalf("rows = %d", len(rows))
	}
	strip := regexp.MustCompile(`<[^>]+>`)
	want := [][4]string{
		{"ctx", "1", "1", "\tkeep"},
		{"del", "2", "", "\told"},
		{"add", "", "2", "\t\tnew  trailing  "},
		{"add empty", "", "3", ""},
	}
	for i, w := range want {
		got := rows[i]
		if got[1] != w[0] || got[2] != w[1] || got[3] != w[2] {
			t.Errorf("row %d = kind %q old %q new %q, want %v", i, got[1], got[2], got[3], w)
		}
		if text := stdhtml.UnescapeString(strip.ReplaceAllString(got[5], "")); text != w[3] {
			t.Errorf("row %d text %q, want %q (whitespace must be preserved)", i, text, w[3])
		}
	}
}

func TestHTMLTests(t *testing.T) {
	r := sampleReport(t)
	r.Tests = &TestReport{
		Ran: true, Status: "failed", Structured: true, Passed: 1, Failed: 1, ExitCode: 1,
		Started: time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC), Finished: time.Date(2026, 10, 14, 12, 0, 2, 0, time.UTC),
		Commands: []TestCommand{{Line: "go test -json ./...", Dir: "backend"}},
		Cases: []TestCase{
			{Kind: "test", Package: "example.com/auth", Name: "TestToken", Status: "passed", ElapsedMs: 24},
			{Kind: "test", Package: "example.com/auth", Name: "TestAuthentication", Status: "failed", ElapsedMs: 42, Output: "Expected: 200\nReceived: <401>"},
			{Kind: "package", Package: "example.com/broken", Name: "(falha de compilação)", Status: "failed", Output: "x.go:1: syntax error"},
		},
		Log: []string{"line <1>", "line 2"}, LogTotal: 10,
	}
	html := renderHTMLString(t, r)
	for _, want := range []string{
		`<span class="ok-text">1 passed</span>`, `<span class="bad-text">1 failed</span>`,
		"Tests failed", "go test -json ./...", "in backend", "2s",
		`<span class="tname mono">TestToken</span>`, "24 ms",
		`Build failed <span class="tag">package result</span>`,
		"Received: &lt;401&gt;", "Full log", "2 lines, last of 10", "line &lt;1&gt;",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("tests section missing %q", want)
		}
	}
	if strings.Contains(html, `<span class="tname mono">(falha de compilação)</span>`) {
		t.Error("package-level result rendered as an individual test")
	}

	r.Tests = &TestReport{Ran: true, Status: "failed", Structured: false, ExitCode: 3}
	html = renderHTMLString(t, r)
	if !strings.Contains(html, "exit code 3") || !strings.Contains(html, "individual test results are not available") {
		t.Error("unstructured run must not show individual results")
	}
}

func TestHTMLLargeFilesStartCollapsed(t *testing.T) {
	var b strings.Builder
	b.WriteString("@@ -0,0 +1,1000 @@\n")
	for i := 0; i < 1000; i++ {
		b.WriteString("+" + strings.Repeat("x", 300) + "\n")
	}
	hunks, _ := gitdiff.Parse(b.String())
	r := sampleReport(t)
	r.Files = append(r.Files, gitdiff.File{Path: "big.txt", Status: "A", Added: 1000, Hunks: hunks})
	html := renderHTMLString(t, r)
	if !strings.Contains(html, `id="file-3" data-path="big.txt">`) {
		t.Error("large file should start collapsed")
	}
	if strings.Count(html, `<tr class="l add`) < 1000 {
		t.Error("collapsed files must still contain their full diff")
	}
}

func TestHTMLDeterministic(t *testing.T) {
	a := renderHTMLString(t, sampleReport(t))
	b := renderHTMLString(t, sampleReport(t))
	if a != b {
		t.Fatal("same report must render identically")
	}
}

func TestDiffstatBar(t *testing.T) {
	for _, c := range []struct {
		add, del int
		want     string
	}{
		{0, 0, "none none none none none"}, {10, 0, "add add add add add"}, {1, 1, "add del none none none"},
		{100, 1, "add add add add del"}, {3, 7, "add add del del del"},
	} {
		if got := strings.Join(diffstatBar(c.add, c.del), " "); got != c.want {
			t.Errorf("bar(%d,%d) = %s, want %s", c.add, c.del, got, c.want)
		}
	}
}

func TestJSONValidAndUnicode(t *testing.T) {
	r := sampleReport(t)
	r.Tests = &TestReport{Ran: true, Status: "failed", Structured: true, Failed: 1,
		Cases: []TestCase{{Name: "TestA", Status: "failed", Output: "want \"1\"\n"}}}
	out, err := renderJSON(r)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		SchemaVersion int
		Repository    struct{ Branch string }
		Summary       struct{ Files, Added, Deleted int }
		Files         []struct {
			Path  string
			Hunks []struct {
				Lines []struct {
					Type string
					Old  int
					New  int
					Text string
				}
			}
		}
		Tests struct {
			Ran   bool
			Tests []struct{ Name, Status, Output string }
		}
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if doc.SchemaVersion != 1 || doc.Repository.Branch != "feat/<x>" || doc.Summary.Added != 2 {
		t.Errorf("metadata %+v", doc)
	}
	f := doc.Files[0]
	if f.Path != "svc/ação.go" || len(f.Hunks) != 1 {
		t.Fatalf("file %+v", f)
	}
	ln := f.Hunks[0].Lines[2]
	if ln.Type != "added" || ln.New != 2 || !strings.Contains(ln.Text, "🚀") {
		t.Errorf("line %+v", ln)
	}
	if !doc.Tests.Ran || doc.Tests.Tests[0].Output != "want \"1\"\n" {
		t.Errorf("tests %+v", doc.Tests)
	}
}

func TestSensitiveRedaction(t *testing.T) {
	secretDiff := "@@ -0,0 +1 @@\n+AWS_KEY=AKIAABCDEFGHIJKLMNOP\n"
	hunks, _ := gitdiff.Parse(secretDiff)
	files := []gitdiff.File{
		{Path: ".env", Status: "A", Hunks: hunks},
		{Path: "config/app.go", Status: "M", Hunks: hunks},
		{Path: "README.md", Status: "M"},
	}
	out, sens := Redact(files, false)
	if strings.Join(sens, ",") != ".env,config/app.go" {
		t.Fatalf("sensitive = %v", sens)
	}
	if out[0].Hunks != nil || out[1].Hunks != nil {
		t.Fatal("content of sensitive files must be omitted by default")
	}
	r := Report{Project: "p", Files: out, Omitted: sens, GeneratedAt: time.Now()}
	for _, f := range Formats {
		data, err := Render(f, r)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "AKIAABCDEFGHIJKLMNOP") {
			t.Errorf("%s export leaked a secret", f)
		}
	}
	kept, _ := Redact(files, true)
	if kept[0].Hunks == nil {
		t.Error("explicit inclusion must keep content")
	}
}

func TestWriteFileProtectsExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "report.md")
	if err := WriteFile(path, []byte("one"), false); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(path, []byte("two"), false); !errors.Is(err, ErrExists) {
		t.Fatalf("expected ErrExists, got %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "one" {
		t.Fatalf("file overwritten without confirmation: %q", b)
	}
	if err := WriteFile(path, []byte("two"), true); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "two" {
		t.Fatalf("overwrite failed: %q", b)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("temporary files left behind: %v", entries)
	}
}

func TestWriteFileFailure(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	os.WriteFile(blocker, nil, 0o644)
	if err := WriteFile(filepath.Join(blocker, "report.md"), []byte("x"), false); err == nil {
		t.Fatal("expected error writing under a regular file")
	}
	if os.Getuid() != 0 {
		ro := filepath.Join(dir, "ro")
		os.Mkdir(ro, 0o555)
		if err := WriteFile(filepath.Join(ro, "r.md"), []byte("x"), false); err == nil {
			t.Fatal("expected error writing into read-only directory")
		}
	}
}
