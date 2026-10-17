package srcmap

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/inspect/inspecttest"
	"github.com/athos/gitview/internal/inspect/perf"
)

func mapper(t *testing.T) *Mapper {
	p := inspecttest.Deep(t)
	dir, _ := filepath.Abs(inspecttest.Dir())
	return New(dir, []*flow.Program{p})
}

func TestFrame(t *testing.T) {
	m := mapper(t)
	dir, _ := filepath.Abs(inspecttest.Dir())
	l, ok := m.Frame("example.com/deep/repository.(*ClientRepository).FindByID", filepath.Join(dir, "repository", "pg.go"), 22)
	if !ok || l.Conf != "exact" || l.Name != "repository.(*ClientRepository).FindByID" || l.File != "repository/pg.go" {
		t.Errorf("by position: %+v", l)
	}
	// A binary built elsewhere (or with -trimpath): by runtime name.
	l, ok = m.Frame("example.com/deep/service.(*ClientService).GetClientByID", "/build/other/service/client.go", 9999)
	if !ok || l.Conf != "likely" || l.Symbol != "(*example.com/deep/service.ClientService).GetClientByID" {
		t.Errorf("by name: %+v", l)
	}
	if _, ok := m.Frame("net/http.(*conn).serve", "/go/src/net/http/server.go", 2000); ok {
		t.Error("standard library frame mapped")
	}
}

func TestSQL(t *testing.T) {
	m := mapper(t)
	dir, _ := filepath.Abs(inspecttest.Dir())
	file := filepath.Join(dir, "repository", "pg.go")
	sm, ok := m.SQL("SELECT id, name FROM clients WHERE id = $1", file, 22, "")
	if !ok || sm.Conf != "exact" || sm.Op.Line != 22 || sm.Func.Name != "repository.(*ClientRepository).FindByID" {
		t.Errorf("call site: %+v", sm)
	}
	sm, ok = m.SQL("SELECT id, name FROM clients WHERE id = $1", file, 23, "")
	if !ok || sm.Conf != "likely" || sm.Op.Line != 22 {
		t.Errorf("near call site: %+v", sm)
	}
	// Without a call site (proxy or OpenTelemetry without code attributes):
	// by text, literals ignored.
	sm, ok = m.SQL("select id,  name from clients where id = $1", "", 0, "")
	if !ok || sm.Conf != "likely" || sm.Op.Line != 22 || sm.Func.Symbol == "" {
		t.Errorf("by text: %+v", sm)
	}
	if sm, ok := m.SQL("SELECT 42 FROM nowhere", "", 0, ""); ok {
		t.Errorf("unknown statement matched: %+v", sm)
	}
}

func TestRoute(t *testing.T) {
	m := mapper(t)
	rm, ok := m.Route("GET", "/clients/{id}", "/clients/7")
	if !ok || rm.Conf != "exact" || rm.Router != "chi" || rm.Handler == nil || rm.Handler.Name != "api.(*Handler).GetClient" {
		t.Errorf("by pattern: %+v", rm)
	}
	rm, ok = m.Route("GET", "", "/clients/7")
	if !ok || rm.Conf != "likely" || rm.Path != "/clients/{id}" {
		t.Errorf("by path: %+v", rm)
	}
	rm, ok = m.Route("GET", "", "/clients")
	if !ok || rm.Path != "/clients" || rm.Handler.Name != "api.(*Handler).SearchClients" {
		t.Errorf("literal path: %+v", rm)
	}
	if _, ok := m.Route("POST", "", "/clients/7"); ok {
		t.Error("method mismatch matched")
	}
	if !samePattern("/u/:id/x", "/u/{uid}/x") || samePattern("/u/{id}", "/u/me") {
		t.Error("samePattern")
	}
}

func TestHotPath(t *testing.T) {
	root := &perf.FlameNode{Name: "root", Value: 100, Children: []*perf.FlameNode{
		{Name: "main.a", Value: 70, Symbol: "a", Children: []*perf.FlameNode{
			{Name: "main.b", Value: 20, Symbol: "b"},
			{Name: "runtime.mallocgc", Value: 45, Self: 45},
		}},
		{Name: "main.c", Value: 30},
	}}
	path := HotPath(root)
	if len(path) != 2 || path[0].Name != "main.a" || path[1].Name != "runtime.mallocgc" || path[1].Pct != 45 {
		t.Errorf("hot path: %+v", path)
	}
}

func TestStaleFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), nil, 0o644)
	os.WriteFile(filepath.Join(dir, "b.go"), nil, 0o644)
	old := time.Now().Add(-time.Hour)
	os.Chtimes(filepath.Join(dir, "b.go"), old, old)
	m := New(dir, nil)
	got := m.StaleFiles(time.Now().Add(-time.Minute), []string{"a.go", "b.go", "a.go", "missing.go"})
	if len(got) != 1 || got[0] != "a.go" {
		t.Errorf("stale: %v", got)
	}
}
