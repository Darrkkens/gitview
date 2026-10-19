package main

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tr "github.com/athos/gitview/internal/testrunner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func init() { lipgloss.SetColorProfile(termenv.TrueColor) }

func fileDiff(path string, added ...string) FileChange {
	var b strings.Builder
	b.WriteString("diff --git a/" + path + " b/" + path + "\n--- a/" + path + "\n+++ b/" + path + "\n")
	b.WriteString("@@ -1,1 +1," + itoa(len(added)+1) + " @@\n ctx\n")
	for _, a := range added {
		b.WriteString("+" + a + "\n")
	}
	return FileChange{Path: path, Status: "M", Added: len(added), Diff: b.String()}
}

func newTestModel(t *testing.T, files ...FileChange) *model {
	t.Helper()
	m := newModel(&Repo{Root: t.TempDir()}, nil, options{interval: time.Hour, exportDir: t.TempDir()})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.load(files...)
	return m
}

func (m *model) load(files ...FileChange) {
	m.Update(refreshMsg{files: files, parsed: m.parseCache.update(files), branch: "FATUR-44"})
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEscape}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}
	}
	if a, ok := strings.CutPrefix(s, "alt+"); ok {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(a), Alt: true}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func (m *model) press(keys ...string) tea.Cmd {
	var cmd tea.Cmd
	for _, k := range keys {
		_, cmd = m.Update(key(k))
	}
	return cmd
}

func (m *model) typeSearch(text string) {
	m.press("/")
	for _, r := range text {
		m.press(string(r))
	}
	m.Update(searchDebounceMsg{seq: m.search.seq})
}

const (
	hitBg = "48;2;88;91;112"
	curBg = "48;2;249;226;175"
)

func sampleFiles() []FileChange {
	return []FileChange{
		fileDiff("auth/handler.go", "a := token_x", "b := 2"),
		fileDiff("auth/service.go", "func token_new() {}", "// token_ again", "x"),
		fileDiff("telecom/telecom.go", "type Meta struct{}"),
	}
}

func TestGlobalSearchFindsCollapsedFilesAndExpandsOnlyTarget(t *testing.T) {
	m := newTestModel(t, sampleFiles()...)
	m.expanded["telecom/telecom.go"] = true // pre-existing state must survive
	m.rebuild()

	m.typeSearch("token_")
	if m.screen != screenSearch {
		t.Fatal("search panel not open")
	}
	r := m.search.res
	if len(r.Matches) != 3 || len(r.Files) != 2 {
		t.Fatalf("matches=%d files=%d", len(r.Matches), len(r.Files))
	}
	view := ansi.Strip(m.View())
	for _, want := range []string{"3 resultados · 2 arquivos", "auth/handler.go", "2 ocorrências"} {
		if !strings.Contains(view, want) {
			t.Errorf("search view missing %q:\n%s", want, view)
		}
	}

	m.press("down", "enter")
	if m.screen != screenDiff || m.cursor != "auth/service.go" {
		t.Fatalf("screen=%v cursor=%q", m.screen, m.cursor)
	}
	if !m.expanded["auth/service.go"] || m.expanded["auth/handler.go"] || !m.expanded["telecom/telecom.go"] {
		t.Fatalf("expanded state changed unexpectedly: %v", m.expanded)
	}
	if m.search.cur != r.Files[1].First {
		t.Fatalf("current match %d", m.search.cur)
	}
	if v := m.View(); !strings.Contains(v, curBg) || !strings.Contains(v, hitBg) {
		t.Error("current and other hits must be highlighted")
	}
}

func TestSearchNextPrevWraps(t *testing.T) {
	m := newTestModel(t, sampleFiles()...)
	m.typeSearch("token_")
	m.press("enter") // first match
	if m.search.cur != 0 {
		t.Fatalf("cur=%d", m.search.cur)
	}
	m.press("N")
	if m.search.cur != 2 || m.cursor != "auth/service.go" {
		t.Fatalf("N should wrap to last: cur=%d cursor=%s", m.search.cur, m.cursor)
	}
	m.press("n")
	if m.search.cur != 0 || m.cursor != "auth/handler.go" {
		t.Fatalf("n should wrap to first: cur=%d", m.search.cur)
	}
	if !strings.Contains(ansi.Strip(m.footerView()), "1/3") {
		t.Errorf("footer position: %q", ansi.Strip(m.footerView()))
	}
	m.press("esc")
	if !m.search.query().Empty() || strings.Contains(m.View(), curBg) {
		t.Error("esc must clear the search")
	}
}

func TestSearchInvalidRegexAndNoResults(t *testing.T) {
	m := newTestModel(t, sampleFiles()...)
	m.press("/", "alt+r")
	m.typeSearch("token_(")
	if !strings.Contains(ansi.Strip(m.View()), "regex inválida") {
		t.Errorf("invalid regex not reported:\n%s", ansi.Strip(m.View()))
	}
	m.press("enter") // must not crash nor navigate
	if m.screen != screenSearch {
		t.Fatal("enter with invalid regex should keep the panel open")
	}
	m.press("esc")

	m.typeSearch("nada-aqui")
	if !strings.Contains(ansi.Strip(m.View()), "Nenhuma ocorrência") {
		t.Errorf("no-results message missing")
	}
}

func TestSearchCaseToggle(t *testing.T) {
	m := newTestModel(t, sampleFiles()...)
	m.typeSearch("META")
	if len(m.search.res.Matches) != 1 {
		t.Fatalf("insensitive: %d", len(m.search.res.Matches))
	}
	m.press("alt+c")
	if len(m.search.res.Matches) != 0 {
		t.Fatalf("sensitive: %d", len(m.search.res.Matches))
	}
}

func TestSearchSurvivesRefresh(t *testing.T) {
	files := sampleFiles()
	m := newTestModel(t, files...)
	m.typeSearch("token_")
	m.press("down", "enter")
	cur := m.search.res.Matches[m.search.cur]

	// The agent edits another file: results update, the current match stays.
	files = append([]FileChange{}, files...) // a refresh delivers a fresh slice
	files[2] = fileDiff("telecom/telecom.go", "token_ := 1", "type Meta struct{}")
	m.load(files...)
	if n := len(m.search.res.Matches); n != 4 {
		t.Fatalf("matches after refresh = %d", n)
	}
	now := m.search.res.Matches[m.search.cur]
	if now.Path != cur.Path || now.Line != cur.Line {
		t.Fatalf("current match moved from %+v to %+v", cur, now)
	}
	if m.search.input.Value() != "token_" {
		t.Fatal("query lost on refresh")
	}
}

func runCmd(m *model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		for _, c := range b {
			runCmd(m, c)
		}
		return
	}
	m.Update(msg)
}

func TestExportFlowWithOverwriteProtection(t *testing.T) {
	m := newTestModel(t, sampleFiles()...)
	m.press("x")
	if m.screen != screenExport {
		t.Fatal("export dialog not open")
	}
	view := ansi.Strip(m.View())
	for _, want := range []string{"Exportar alterações", "Markdown", "HTML", "JSON", "Todos os arquivos", "gitview-report.md"} {
		if !strings.Contains(view, want) {
			t.Errorf("dialog missing %q", want)
		}
	}
	dest := m.export.dest.Value()
	runCmd(m, m.press("enter"))
	if m.screen != screenDiff || !strings.Contains(m.toast, filepath.Base(dest)) {
		t.Fatalf("export did not finish: screen=%v toast=%q msg=%q", m.screen, m.toast, m.export.msg)
	}
	first, _ := os.ReadFile(dest)
	if !strings.Contains(string(first), "auth/service.go") {
		t.Fatal("markdown content missing")
	}

	os.WriteFile(dest, []byte("keep me"), 0o644)
	m.press("x")
	runCmd(m, m.press("enter"))
	if !m.export.confirmOverwrite || m.screen != screenExport {
		t.Fatal("existing file must require confirmation")
	}
	if b, _ := os.ReadFile(dest); string(b) != "keep me" {
		t.Fatal("file overwritten without confirmation")
	}
	runCmd(m, m.press("enter"))
	if b, _ := os.ReadFile(dest); string(b) == "keep me" {
		t.Fatal("confirmed overwrite did not happen")
	}
}

func TestExportFilteredScopeJSON(t *testing.T) {
	m := newTestModel(t, sampleFiles()...)
	m.typeSearch("token_")
	m.press("enter")
	m.press("x", "down", "down") // format: JSON
	m.press("tab", "down", "down")
	if m.export.scope != scopeFiltered || !strings.HasSuffix(m.export.dest.Value(), ".json") {
		t.Fatalf("scope=%v dest=%s", m.export.scope, m.export.dest.Value())
	}
	runCmd(m, m.press("enter"))
	data, err := os.ReadFile(filepath.Join(m.export.dir, "gitview-report.json"))
	if err != nil {
		t.Fatal(err, m.export.msg)
	}
	var doc struct {
		Query string
		Files []struct{ Path string }
		Tests struct{ Ran bool }
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Query != "token_" || len(doc.Files) != 2 || doc.Tests.Ran {
		t.Fatalf("%+v", doc)
	}
}

func TestExportSensitiveNeedsExplicitOptIn(t *testing.T) {
	env := FileChange{Path: ".env", Status: "A", Added: 1, Diff: "@@ -0,0 +1 @@\n+DB_PASSWORD=\"hunter2-very-secret\"\n"}
	m := newTestModel(t, env, fileDiff("main.go", "x"))
	m.press("x")
	if len(m.export.sensitive) != 1 || !strings.Contains(ansi.Strip(m.View()), "possíveis segredos") {
		t.Fatal("sensitive warning missing")
	}
	runCmd(m, m.press("enter"))
	data, _ := os.ReadFile(filepath.Join(m.export.dir, "gitview-report.md"))
	if strings.Contains(string(data), "hunter2") {
		t.Fatal("secret exported without opt-in")
	}
	m.press("x", "tab", "tab", "tab", "tab", "space")
	if !m.export.includeSensitive {
		t.Fatal("opt-in toggle failed")
	}
}

// TestTUIStaysResponsiveWhileTestsRun runs a real slow `go test` and checks
// that key handling and git refreshes are never blocked by it.
func TestTUIStaysResponsiveWhileTestsRun(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not installed")
	}
	m := newTestModel(t, sampleFiles()...)
	write := func(name, body string) {
		os.MkdirAll(filepath.Dir(filepath.Join(m.repo.Root, name)), 0o755)
		os.WriteFile(filepath.Join(m.repo.Root, name), []byte(body), 0o644)
	}
	write("go.mod", "module example.com/slow\n\ngo 1.21\n")
	write("s/s_test.go", "package s\nimport (\"testing\"; \"time\")\nfunc TestSlow(t *testing.T) { time.Sleep(30 * time.Second) }\n")
	defer m.tests.runner.Cancel()

	runCmd(m, m.press("t"))
	if len(m.tests.targets) == 0 {
		t.Fatal("no targets discovered")
	}
	m.press("enter")
	if m.tests.pending == nil || !strings.Contains(ansi.Strip(m.View()), "roda código do repositório") {
		t.Fatal("execution must be acknowledged first")
	}
	m.press("enter")
	if !m.tests.runner.Running() {
		t.Fatal("run did not start")
	}

	for _, k := range []string{"j", "k", "l", "esc", "a", "esc", "j", "/", "esc", "t"} {
		start := time.Now()
		m.press(k)
		m.View()
		if d := time.Since(start); d > 100*time.Millisecond {
			t.Fatalf("key %q blocked for %s", k, d)
		}
	}
	start := time.Now()
	m.load(sampleFiles()[:2]...)
	if d := time.Since(start); d > 100*time.Millisecond || len(m.files) != 2 {
		t.Fatalf("refresh blocked for %s", d)
	}

	m.press("c")
	m.tests.runner.Wait()
	if s := m.tests.runner.Snapshot(); s.Status != tr.Cancelled {
		t.Fatalf("status after cancel: %v", s.Status)
	}
}

// 'w' serves the web UI of the repository and opens the browser once.
func TestWebKeyStartsTheLocalServer(t *testing.T) {
	t.Setenv("GITVIEW_CACHE_DIR", t.TempDir())
	opened := []string{}
	old := startBrowser
	startBrowser = func(u string) error { opened = append(opened, u); return nil }
	defer func() { startBrowser = old }()
	m := newTestModel(t)
	runCmd(m, m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")}))
	if m.web == nil || !strings.HasPrefix(m.web.url, "http://127.0.0.1:") || len(opened) != 1 {
		t.Fatalf("web UI not started: %+v %v (%s)", m.web, opened, m.toast)
	}
	defer m.webStop()
	resp, err := http.Get(m.web.url)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("page: %v %v", err, resp)
	}
	resp.Body.Close()
	runCmd(m, m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")}))
	if len(opened) != 2 {
		t.Error("a second press must reopen the same page")
	}
}
