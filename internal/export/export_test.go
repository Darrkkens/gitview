package export

import (
	"crypto/sha256"
	"encoding/base64"
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

// pageSection decodes one section of a page's envelope into v.
func pageSection(t *testing.T, page, name string, v any) {
	t.Helper()
	m := regexp.MustCompile(`(?s)<script type="application/json" id="gitview-data">(.*?)</script>`).FindStringSubmatch(page)
	if m == nil {
		t.Fatal("no data block")
	}
	var env struct {
		Sections map[string]json.RawMessage `json:"sections"`
	}
	if err := json.Unmarshal([]byte(m[1]), &env); err != nil {
		t.Fatalf("data block is not JSON: %v", err)
	}
	if err := json.Unmarshal(env.Sections[name], v); err != nil {
		t.Fatalf("section %s: %v", name, err)
	}
}

func reportOf(t *testing.T, r Report) (ReportSection, string) {
	t.Helper()
	page := renderHTMLString(t, r)
	var s ReportSection
	pageSection(t, page, "report", &s)
	return s, page
}

func spanText(spans []Span) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	return b.String()
}

func TestHTMLEscaping(t *testing.T) {
	r := sampleReport(t)
	r.Files[0].Path = `svc/"><img src=x onerror=alert(1)>.go`
	s, page := reportOf(t, r)
	for _, bad := range []string{"<script>alert", "feat/<x>", "<img src=x", `"><img`} {
		if strings.Contains(page, bad) {
			t.Fatalf("unescaped content %q in HTML", bad)
		}
	}
	// The data round-trips exactly: escaping is only the JSON encoding.
	if s.Branch != "feat/<x>" || s.Files[0].Path != r.Files[0].Path {
		t.Errorf("branch %q, path %q", s.Branch, s.Files[0].Path)
	}
	if got := spanText(s.Files[0].Hunks[0].Lines[2].Spans); got != "// novo: <script>alert(\"x\")</script> & ü 🚀" {
		t.Errorf("line text %q", got)
	}
}

func TestHTMLStandaloneAndCSP(t *testing.T) {
	html := renderHTMLString(t, sampleReport(t))
	// The SVG namespace URI is an identifier, not a resource.
	scan := strings.ReplaceAll(html, `"http://www.w3.org/2000/svg"`, "")
	scan = strings.ReplaceAll(scan, `'http://www.w3.org/2000/svg'`, "")
	for _, ext := range []string{`src="http`, `href="http`, "@import", "url(http"} {
		if strings.Contains(scan, ext) {
			t.Errorf("report must be standalone, found %q", ext)
		}
	}
	// Every inline script must be allowed by hash, and no other script may exist.
	scripts := regexp.MustCompile(`(?s)<script type="module"[^>]*>(.*?)</script>`).FindAllStringSubmatch(html, -1)
	if len(scripts) != 1 || strings.Count(html, "</script>") != 2 { // the app and the data block
		t.Fatalf("expected the app script and one data block, got %d / %d", len(scripts), strings.Count(html, "</script>"))
	}
	csp := stdhtml.UnescapeString(regexp.MustCompile(`http-equiv="Content-Security-Policy" content="([^"]*)"`).FindStringSubmatch(html)[1])
	if h := "sha256-" + scriptHashB64(scripts[0][1]); !strings.Contains(csp, "'"+h+"'") {
		t.Errorf("script hash %s missing from CSP", h)
	}
	if !strings.HasPrefix(csp, "default-src 'none';") || strings.Contains(csp, "connect-src") {
		t.Error("CSP must block network access")
	}
}

func TestHTMLStructure(t *testing.T) {
	s, page := reportOf(t, sampleReport(t))
	if !strings.Contains(page, "<title>GitView · gitview · Todos os arquivos</title>") {
		t.Error("title")
	}
	if len(s.Files) != 2 || s.Added != 2 || s.Deleted != 1 {
		t.Fatalf("files %d, +%d -%d", len(s.Files), s.Added, s.Deleted)
	}
	f := s.Files[0]
	if f.ID != "file-1" || f.Path != "svc/ação.go" || f.Status != "M" || f.StatusName != "Modified" || !f.Open {
		t.Errorf("file %+v", f)
	}
	if h := f.Hunks[0]; h.Range != "@@ -1,2 +1,3 @@" || h.Section != "package svc" {
		t.Errorf("hunk %q %q", h.Range, h.Section)
	}
	if b := s.Files[1]; !b.Binary || b.Status != "A" || len(b.Hunks) != 0 {
		t.Errorf("binary %+v", b)
	}
	if s.Tests == nil || s.Tests.Ran {
		t.Error("a session without a test run reports ran=false")
	}
	if s.Flow != nil || s.Inspect != nil || s.Study != nil {
		t.Error("sections that were not requested must be absent")
	}
	// Chroma classes for the page's palette.
	var classes []string
	for _, sp := range f.Hunks[0].Lines[0].Spans {
		classes = append(classes, sp.Class)
	}
	if !strings.Contains(strings.Join(classes, " "), "k") {
		t.Errorf("Go keywords are not classified: %v", classes)
	}
}

func TestHTMLDiffLines(t *testing.T) {
	r := sampleReport(t)
	tabbed := "@@ -1,2 +1,3 @@\n \tkeep\n-\told\n+\t\tnew  trailing  \n+\n"
	hunks, _ := gitdiff.Parse(tabbed)
	r.Files = []gitdiff.File{{Path: "a.go", Status: "M", Added: 2, Deleted: 1, Hunks: hunks}}
	s, _ := reportOf(t, r)
	lines := s.Files[0].Hunks[0].Lines
	if len(lines) != 4 {
		t.Fatalf("lines = %d", len(lines))
	}
	want := []struct {
		kind     string
		old, new int
		text     string
	}{
		{"ctx", 1, 1, "\tkeep"},
		{"del", 2, 0, "\told"},
		{"add", 0, 2, "\t\tnew  trailing  "},
		{"add", 0, 3, ""},
	}
	for i, w := range want {
		got := lines[i]
		if got.Kind != w.kind || got.Old != w.old || got.New != w.new {
			t.Errorf("line %d = %s %d %d, want %v", i, got.Kind, got.Old, got.New, w)
		}
		if text := spanText(got.Spans); text != w.text {
			t.Errorf("line %d text %q, want %q (whitespace must be preserved)", i, text, w.text)
		}
	}
}

func TestHTMLTests(t *testing.T) {
	r := sampleReport(t)
	r.Tests = &TestReport{
		Ran: true, Status: "failed", Structured: true, Passed: 1, Failed: 1, ExitCode: 1,
		Commands: []TestCommand{{Line: "go test -json ./...", Dir: "backend"}},
		Cases: []TestCase{
			{Kind: "test", Package: "example.com/auth", Name: "TestAuthentication", Status: "failed", ElapsedMs: 42, Output: "Received: <401>"},
		},
		Log: []string{"line <1>"}, LogTotal: 10,
	}
	s, page := reportOf(t, r)
	if s.Tests == nil || s.Tests.Failed != 1 || s.Tests.Commands[0].Line != "go test -json ./..." || s.Tests.Cases[0].Output != "Received: <401>" || s.Tests.LogTotal != 10 {
		t.Errorf("tests %+v", s.Tests)
	}
	if strings.Contains(page, "<401>") {
		t.Error("test output not escaped in the page")
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
	s, _ := reportOf(t, r)
	big := s.Files[2]
	if big.ID != "file-3" || big.Open {
		t.Error("large file should start collapsed")
	}
	if big.LineCount != 1000 || len(big.Hunks[0].Lines) != 1000 {
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

func scriptHashB64(js string) string {
	sum := sha256.Sum256([]byte(js))
	return base64.StdEncoding.EncodeToString(sum[:])
}

// Minified lines stay plain text: highlighting them token by token would
// multiply the page's size.
func TestLongLinesAreNotHighlighted(t *testing.T) {
	long := strings.Repeat("a=1;", 1000)
	hunks, _ := gitdiff.Parse("@@ -0,0 +1,2 @@\n+" + long + "\n+package x\n")
	r := sampleReport(t)
	r.Files = []gitdiff.File{{Path: "dist/app.js", Status: "A", Added: 2, Hunks: hunks}}
	s, _ := reportOf(t, r)
	lines := s.Files[0].Hunks[0].Lines
	if len(lines[0].Spans) != 1 || lines[0].Spans[0].Class != "" || lines[0].Spans[0].Text != long {
		t.Errorf("long line: %d spans", len(lines[0].Spans))
	}
}
