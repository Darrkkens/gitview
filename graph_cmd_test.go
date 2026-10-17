package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gmodel "github.com/athos/gitview/internal/graph/model"
	"github.com/charmbracelet/x/ansi"
)

const graphFixture = "internal/graph/testdata/repo"

func runGraphCLI(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var out, errb bytes.Buffer
	code := runGraph(args, &out, &errb)
	return out.String(), errb.String(), code
}

func TestGraphCLIJSONToStdout(t *testing.T) {
	out, errs, code := runGraphCLI(t, "export", "--format", "json", "--module", graphFixture, ".")
	if code != 0 {
		t.Fatalf("code %d: %s", code, errs)
	}
	var g gmodel.Graph
	if err := json.Unmarshal([]byte(out), &g); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if err := g.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Packages analyzed:", "Functions discovered:", "Communities detected:", "Analysis duration:"} {
		if !strings.Contains(errs, want) {
			t.Errorf("metrics missing %q:\n%s", want, errs)
		}
	}
	if g.Stats.Packages != 6 {
		t.Errorf("packages = %d", g.Stats.Packages)
	}
}

func TestGraphCLILevelPackages(t *testing.T) {
	out, _, code := runGraphCLI(t, "export", "--format", "json", "--level", "packages", "--module", graphFixture, ".")
	if code != 0 {
		t.Fatal(code)
	}
	var g gmodel.Graph
	json.Unmarshal([]byte(out), &g)
	for _, n := range g.Nodes {
		if n.Type != gmodel.KindPackage && n.Type != gmodel.KindModule {
			t.Errorf("packages level contains %s %s", n.Type, n.ID)
		}
	}
	for _, e := range g.Edges {
		if e.Type != gmodel.EdgeImports && e.Type != gmodel.EdgeContains {
			t.Errorf("packages level contains edge %s", e.ID)
		}
	}
}

func TestGraphCLIHTMLAndOverwrite(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "architecture.html")
	_, errs, code := runGraphCLI(t, "export", "--output", dst, "--level", "functions", "--module", graphFixture, ".")
	if code != 0 {
		t.Fatalf("code %d: %s", code, errs)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, want := range []string{"Content-Security-Policy", "default-src &#39;none&#39;", `id="graph-data"`, `"level":"functions"`} {
		if !strings.Contains(html, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
	_, errs, code = runGraphCLI(t, "export", "--output", dst, "--module", graphFixture, ".")
	if code == 0 || !strings.Contains(errs, "--overwrite") {
		t.Fatalf("existing file replaced without --overwrite (code %d): %s", code, errs)
	}
	if _, _, code = runGraphCLI(t, "export", "--output", dst, "--overwrite", "--module", graphFixture, "."); code != 0 {
		t.Fatal("--overwrite failed")
	}
}

func TestGraphCLIBadArgs(t *testing.T) {
	for _, args := range [][]string{{}, {"draw"}, {"export", "--level", "galaxy"}, {"export", "--format", "svg"}} {
		if _, _, code := runGraphCLI(t, args...); code != 2 {
			t.Errorf("%v: code %d, want 2", args, code)
		}
	}
}

func TestGraphCLIStats(t *testing.T) {
	out, _, code := runGraphCLI(t, "stats", "--module", graphFixture, ".")
	if code != 0 || !strings.Contains(out, "Nodes generated:") || !strings.Contains(out, "Calls by confidence:") {
		t.Fatalf("code %d:\n%s", code, out)
	}
}

// graphRepo copies the graph fixture into a temporary repository root.
func graphRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	err := filepath.Walk(graphFixture, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(graphFixture, p)
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

func TestGraphScreenAnalyzeExportOpen(t *testing.T) {
	m := newTestModel(t)
	m.repo.Root = graphRepo(t)
	before, _ := os.ReadDir(m.repo.Root)

	m.press("a")
	if m.screen != screenGraph || len(m.graph.scopes) != 1 {
		t.Fatalf("screen=%v scopes=%v", m.screen, m.graph.scopes)
	}
	m.press("o")
	if !m.graph.msgErr {
		t.Error("opening before an export must explain that there is nothing to open")
	}
	runCmd(m, m.press("enter"))
	if m.graph.g == nil || m.graph.err != "" {
		t.Fatalf("analysis failed: %q", m.graph.err)
	}
	view := ansi.Strip(m.View())
	for _, want := range []string{"Packages analyzed", "Functions discovered", "Communities detected", "x Exportar HTML"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
	runCmd(m, m.press("x"))
	if m.graph.exported == "" || !strings.HasPrefix(m.graph.exported, m.export.dir) {
		t.Fatalf("export: %q (%s)", m.graph.exported, m.graph.msg)
	}
	if _, err := os.Stat(m.graph.exported); err != nil {
		t.Fatal(err)
	}
	var opened string
	defer func(orig func(string) error) { startBrowser = orig }(startBrowser)
	startBrowser = func(abs string) error { opened = abs; return nil }
	runCmd(m, m.press("o"))
	if opened != m.graph.exported {
		t.Errorf("opened %q, want %q", opened, m.graph.exported)
	}
	// The analysed repository is never modified.
	after, _ := os.ReadDir(m.repo.Root)
	if len(after) != len(before) {
		t.Errorf("repository changed: %d → %d entries", len(before), len(after))
	}
	m.press("esc")
	if m.screen != screenDiff {
		t.Error("Esc did not go back")
	}
}
