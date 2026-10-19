package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/athos/gitview/internal/inspect/xtrace"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// drain runs a command and keeps following the commands produced by the
// inspector's own messages (analysis results, runs, loads).
func drain(m *model, cmd tea.Cmd) {
	for i := 0; cmd != nil && i < 20; i++ {
		msg := cmd()
		if b, ok := msg.(tea.BatchMsg); ok {
			for _, c := range b {
				drain(m, c)
			}
			return
		}
		_, next := m.Update(msg)
		switch msg.(type) {
		case inspDocMsg, inspRunMsg, flowLoadedMsg:
			cmd = next
		default:
			return
		}
	}
}

// inspectRepo copies the Deep Inspector fixtures into a repository root.
func inspectRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	src := filepath.Join("..", "..", "internal", "inspect", "testdata")
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		dst := filepath.Join(root, rel)
		if info.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func inspectModel(t *testing.T) *model {
	t.Helper()
	t.Setenv("GITVIEW_CACHE_DIR", t.TempDir())
	m := newTestModel(t, fileDiff("deep/service/client.go", "x"))
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m.repo.Root = inspectRepo(t)
	return m
}

// pick opens the inspector and chooses a function through Find Function.
func pick(t *testing.T, m *model, query, qualified string) {
	t.Helper()
	drain(m, m.press("i"))
	if m.screen != screenFlow || m.flow.loading || m.flow.err != "" {
		t.Fatalf("inspector not ready: loading=%v err=%q", m.flow.loading, m.flow.err)
	}
	if m.flow.phase != phasePick {
		m.press("/")
	}
	for _, r := range query {
		m.press(string(r))
	}
	found := false
	for i, c := range m.flow.cands {
		if c.sym.Qualified == qualified {
			m.flow.csel, found = i, true
		}
	}
	if !found {
		t.Fatalf("%s not offered", qualified)
	}
	drain(m, m.press("enter"))
}

func docText(m *model) string { return m.insp.doc.plain() }

func TestDeepInspectorTabsShareTheSymbol(t *testing.T) {
	m := inspectModel(t)
	pick(t, m, "GetClientByID", "service.(*ClientService).GetClientByID")
	if m.insp.tab != tabBranches {
		t.Fatalf("inspector should open on Branch Explorer, got tab %d", m.insp.tab)
	}
	view := ansi.Strip(m.View())
	for _, want := range []string{"4 Branch Explorer", "service.(*ClientService).GetClientByID", "if id <= 0", "true → B"} {
		if !strings.Contains(view, want) {
			t.Errorf("branch view missing %q:\n%s", want, view)
		}
	}

	drain(m, m.press("2"))
	if m.insp.dfVar != "id@27" {
		t.Errorf("default variable %q", m.insp.dfVar)
	}
	if d := docText(m); !strings.Contains(d, "SQL parameter $1 of pgx QueryRow") || !strings.Contains(d, "id <= 0") {
		t.Errorf("data flow:\n%s", d)
	}
	drain(m, m.press("o"))
	if d := docText(m); !strings.Contains(d, `HTTP path parameter "id"`) || strings.Contains(d, "Destino —") {
		t.Errorf("origin only:\n%s", d)
	}
	// Selecting a node shows the Go code responsible below the list.
	m.insp.sel = m.insp.step(1, 0)
	if v := ansi.Strip(m.View()); !strings.Contains(v, "func (s *ClientService) GetClientByID") {
		t.Errorf("code of the selected node not shown:\n%s", v)
	}

	drain(m, m.press("6"))
	if d := docText(m); !strings.Contains(d, "SELECT id, name") || !strings.Contains(d, "repository.(*ClientRepository).FindByID [likely]") {
		t.Errorf("sql:\n%s", d)
	}
	drain(m, m.press("5"))
	if d := docText(m); !strings.Contains(d, "No profiling data available") {
		t.Errorf("performance without data:\n%s", d)
	}
	drain(m, m.press("3"))
	if d := docText(m); !strings.Contains(d, "No runtime trace available") {
		t.Errorf("trace without data:\n%s", d)
	}

	// Code Flow is the first tab; setting another root moves every tab.
	drain(m, m.press("1"))
	if v := ansi.Strip(m.View()); !strings.Contains(v, "Callers") || !strings.Contains(v, "1 Code Flow") {
		t.Fatalf("code flow tab:\n%s", v)
	}
	for i, r := range m.flow.rows {
		if r.id != "" && m.flow.graph.Node(r.id).Name == "repository.(*ClientRepository).FindByID" {
			m.flow.rsel = i
		}
	}
	m.press("enter")
	drain(m, m.press("4"))
	if d := docText(m); !strings.Contains(d, "repository.(*ClientRepository).FindByID") || !strings.Contains(d, "if err != nil") {
		t.Errorf("branches did not follow the new root:\n%s", d)
	}
}

func TestDeepInspectorAsksBeforeRunning(t *testing.T) {
	m := inspectModel(t)
	pick(t, m, "Busy", "perf.Busy")
	drain(m, m.press("5"))
	m.press("b")
	if m.insp.confirm == nil {
		t.Fatal("no confirmation before running benchmarks")
	}
	v := ansi.Strip(m.View())
	for _, want := range []string{"Os benchmarks do pacote serão executados", "go test", "-bench=.", "./perf", "Enter executa"} {
		if !strings.Contains(v, want) {
			t.Errorf("confirmation missing %q:\n%s", want, v)
		}
	}
	m.press("esc")
	if m.insp.confirm != nil || !strings.Contains(m.insp.msg, "nada foi executado") || m.inspSession().CPU != nil {
		t.Errorf("cancel: confirm=%v msg=%q", m.insp.confirm, m.insp.msg)
	}
	// EXPLAIN without an explicit connection is refused.
	t.Setenv("GITVIEW_PG_DSN", "")
	pick(t, m, "FindByID", "repository.(*ClientRepository).FindByID")
	drain(m, m.press("6"))
	m.press("x")
	if m.insp.confirm != nil || !strings.Contains(m.insp.msg, "GITVIEW_PG_DSN") {
		t.Errorf("EXPLAIN without DSN: %q", m.insp.msg)
	}
	t.Setenv("GITVIEW_PG_DSN", "postgres://u:secret@db.example.com/prod")
	m.press("x")
	if m.insp.confirm != nil || !strings.Contains(m.insp.msg, "não é um servidor local") || strings.Contains(m.insp.msg, "secret") {
		t.Errorf("remote DSN: %q", m.insp.msg)
	}
}

// TestDeepInspectorCapturesTrace runs the fixture's tests with a trace and
// temporary instrumentation of the selected function, from the TUI.
func TestDeepInspectorCapturesTrace(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test -trace")
	}
	m := inspectModel(t)
	pick(t, m, "Square", "traced.Square")
	drain(m, m.press("3"))
	m.press("T")
	if m.insp.confirm == nil || !strings.Contains(strings.Join(m.insp.confirm.lines, "\n"), "região temporária em example.com/deep/traced.Square") {
		t.Fatalf("confirmation: %+v (msg %q)", m.insp.confirm, m.insp.msg)
	}
	drain(m, m.press("enter"))
	if m.insp.msgErr {
		t.Fatalf("capture failed: %s", m.insp.msg)
	}
	tl := m.inspSession().Trace
	if tl == nil {
		t.Fatal("no trace loaded")
	}
	if st := tl.ForSymbol(m.inspSymbol().Ref.ID); st.SpanCount != 20 {
		t.Errorf("spans of Square: %d", st.SpanCount)
	}
	d := docText(m)
	for _, want := range []string{"Execution Trace", "spans medidos (regiões): 20", "traced.worker", "pipeline (#"} {
		if !strings.Contains(d, want) {
			t.Errorf("trace view missing %q:\n%s", want, d)
		}
	}
	// Zoom keeps a valid range.
	drain(m, m.press("+"))
	if m.insp.trTo <= m.insp.trFrom {
		t.Errorf("zoom range %d..%d", m.insp.trFrom, m.insp.trTo)
	}
	// The project file was not modified by the instrumentation.
	src, _ := os.ReadFile(filepath.Join(m.repo.Root, "deep", "traced", "traced.go"))
	if strings.Contains(string(src), xtrace.RegionPrefix) {
		t.Error("instrumentation leaked into the project")
	}
}

func TestInspectorModalStatesOwnTheKeyboard(t *testing.T) {
	m := inspectModel(t)
	pick(t, m, "Busy", "perf.Busy")
	drain(m, m.press("5"))
	m.press("I")
	for _, r := range "/tmp/cpu2.prof" {
		m.press(string(r))
	}
	if m.insp.tab != tabPerf || m.insp.input.Value() != "/tmp/cpu2.prof" {
		t.Fatalf("digits typed in the import box switched tabs: tab=%d value=%q", m.insp.tab, m.insp.input.Value())
	}
	m.press("esc")
	// A confirmation survives no tab switch, and shows its keys on a small screen.
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 14})
	m.press("b")
	m.press("1")
	if m.insp.tab != tabPerf || m.insp.confirm == nil {
		t.Fatalf("tab key bypassed the confirmation: tab=%d", m.insp.tab)
	}
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "Enter executa") || !strings.Contains(v, "Esc cancela") {
		t.Errorf("confirmation keys hidden on a small terminal:\n%s", v)
	}
	m.press("esc")
}

func TestControlCharactersAreNeutralised(t *testing.T) {
	d := doc{{spans: []tspan{sp("SELECT '\x1b]52;c;ZXZpbA==\x07'")}, loc: "x.go:1"}}
	out := d.plain()
	if strings.ContainsAny(out, "\x1b\x07") || !strings.Contains(out, `\x1b]52`) {
		t.Errorf("plain: %q", out)
	}
	if styled := d[0].styled(80, false); strings.Contains(styled, "\x1b]52") || strings.Contains(styled, "\x07") {
		t.Errorf("styled output carries the raw escape: %q", styled)
	}
}

func TestCleanTextNeutralisesBidiAndInvalidBytes(t *testing.T) {
	in := "ok ‮evil‬ \x9b31m done \xff"
	out := cleanText(in)
	for _, bad := range []string{"‮", "‬", "\x9b", "\xff"} {
		if strings.Contains(out, bad) {
			t.Errorf("%q survived: %q", bad, out)
		}
	}
	if !strings.Contains(out, `\u{202e}`) || !strings.Contains(out, `\x9b`) || !strings.Contains(out, `\xff`) || !strings.HasPrefix(out, "ok ") {
		t.Errorf("cleaned: %q", out)
	}
	if cleanText("plain café") != "plain café" {
		t.Error("ordinary text changed")
	}
}
