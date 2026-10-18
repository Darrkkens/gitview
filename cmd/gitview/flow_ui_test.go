package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/athos/gitview/internal/flow"
	"github.com/charmbracelet/x/ansi"
)

// flowRepo copies the flow fixture (module + stub dependencies) into a
// temporary repository root.
func flowRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	src := filepath.Join("..", "..", "internal", "flow", "testdata")
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if strings.HasPrefix(rel, "broken") {
			return filepath.SkipDir
		}
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

func flowModel(t *testing.T) *model {
	t.Helper()
	m := newTestModel(t, fileDiff("app/client/service.go", "x"))
	m.repo.Root = flowRepo(t)
	return m
}

func TestFlowScreenFindFunctionWithHomonyms(t *testing.T) {
	m := flowModel(t)
	runCmd(m, m.press("f"))
	if m.screen != screenFlow || m.flow.loading || m.flow.err != "" {
		t.Fatalf("flow screen not ready: loading=%v err=%q", m.flow.loading, m.flow.err)
	}
	for _, r := range "GetClientByID" {
		m.press(string(r))
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "3 símbolos com esse nome") {
		t.Fatalf("homonyms not announced:\n%s", view)
	}
	for _, want := range []string{"catalog.GetClientByID", "client.(*ClientService).GetClientByID", "httpapi.(*ClientHandler).GetClientByID"} {
		if !strings.Contains(view, want) {
			t.Errorf("candidate %s missing", want)
		}
	}
	// Choose the service method explicitly.
	for i, c := range m.flow.cands {
		if c.sym.Qualified == "client.(*ClientService).GetClientByID" {
			m.flow.csel = i
		}
	}
	m.press("enter")
	if m.flow.phase != phaseGraph {
		t.Fatal("graph not built")
	}
	view = ansi.Strip(m.View())
	for _, want := range []string{"Callers", "Callees", "← httpapi.(*ClientHandler).GetClientByID", "GET /api/clients/:id",
		"→ client.ValidateClient", "client.(*PGRepo).Find  likely", "pgx QueryRow", "both · profundidade 5"} {
		if !strings.Contains(view, want) {
			t.Errorf("graph view missing %q:\n%s", want, view)
		}
	}

	m.press("tab") // callers
	if v := ansi.Strip(m.View()); strings.Contains(v, "→ client.ValidateClient") || !strings.Contains(v, "callers") {
		t.Errorf("callers-only view still shows callees")
	}
	m.press("tab", "tab", "-", "-") // both, depth 3
	if m.flow.dir != flow.Both || m.flow.depth != 3 {
		t.Fatalf("dir=%s depth=%d", m.flow.dir, m.flow.depth)
	}

	// Set as root and go back.
	for i, r := range m.flow.rows {
		if r.id != "" && m.flow.graph.Node(r.id).Name == "client.ValidateClient" {
			m.flow.rsel = i
		}
	}
	m.press("enter")
	if !strings.Contains(m.flow.rootID, "ValidateClient") {
		t.Fatalf("set as root failed: %s", m.flow.rootID)
	}
	m.press("backspace")
	if !strings.Contains(m.flow.rootID, "GetClientByID") {
		t.Fatalf("history back failed: %s", m.flow.rootID)
	}
}

func TestExportHTMLIncludesCodeFlowIndex(t *testing.T) {
	m := flowModel(t)
	m.press("x", "down") // HTML
	runCmd(m, m.press("enter"))
	data, err := os.ReadFile(filepath.Join(m.export.dir, "gitview-report.html"))
	if err != nil {
		t.Fatal(err, m.export.msg)
	}
	var rep struct {
		Flow *struct {
			Nodes []struct{ ID, Name, Kind string }
			Edges []struct{ From, To, Conf string }
		} `json:"flow"`
	}
	if err := json.Unmarshal(pageSections(t, string(data))["report"], &rep); err != nil || rep.Flow == nil {
		t.Fatalf("Code Flow section missing: %v", err)
	}
	g := rep.Flow
	found := 0
	for _, n := range g.Nodes {
		if strings.HasSuffix(n.ID, "GetClientByID") {
			found++
		}
	}
	if found != 3 || len(g.Edges) == 0 {
		t.Fatalf("index incomplete: %d GetClientByID symbols, %d edges", found, len(g.Edges))
	}
}
