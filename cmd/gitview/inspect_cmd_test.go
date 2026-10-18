package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/athos/gitview/internal/inspect/branch"
	"github.com/athos/gitview/internal/inspect/dataflow"
	"github.com/athos/gitview/internal/inspect/sqlops"
)

const deepFixture = "../../internal/inspect/testdata/deep"

func runInspectCLI(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var out, errb bytes.Buffer
	code := runInspectIO(append(args, deepFixture), &out, &errb, strings.NewReader(""))
	return out.String(), errb.String(), code
}

func TestInspectSummaryCoversEveryView(t *testing.T) {
	out, errs, code := runInspectCLI(t, "--function", "service.(*ClientService).GetClientByID", "--non-interactive")
	if code != exitOK {
		t.Fatalf("code %d: %s", code, errs)
	}
	for _, want := range []string{"Go Deep Inspector", "(*example.com/deep/service.ClientService).GetClientByID",
		"[Code Flow]", "chamadores: api.(*Handler).GetClient", "[Data Flow]", "param id (int64",
		"[Execution Trace]", "No runtime trace available", "[Branch Explorer]", "if id <= 0",
		"[Performance]", "No profiling data available", "[SQL Explorer]", "SELECT id, name FROM clients WHERE id = $1"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q:\n%s", want, out)
		}
	}
}

func TestInspectHomonymsAndMissingSymbols(t *testing.T) {
	_, errs, code := runInspectCLI(t, "--function", "Count", "--non-interactive")
	if code != exitAmbiguous || !strings.Contains(errs, "repository.(*ClientRepository).Count") || !strings.Contains(errs, "repository.(*AuditRepository).Count") {
		t.Errorf("ambiguity: code %d\n%s", code, errs)
	}
	_, errs, code = runInspectCLI(t, "branches", "--function", "NoSuchThing", "--non-interactive")
	if code != exitErr || !strings.Contains(errs, "nenhuma função") {
		t.Errorf("missing symbol: code %d %s", code, errs)
	}
}

func TestInspectDataFlowCLI(t *testing.T) {
	_, errs, code := runInspectCLI(t, "dataflow", "--function", "api.(*Handler).GetClient")
	if code != exitUsage || !strings.Contains(errs, "clientID") {
		t.Errorf("without a variable the CLI lists them: code %d\n%s", code, errs)
	}
	out, errs, code := runInspectCLI(t, "dataflow", "--function", "api.(*Handler).GetClient", "--variable", "clientID", "--json")
	if code != exitOK {
		t.Fatalf("code %d %s", code, errs)
	}
	var r dataflow.Result
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}
	sql := false
	for _, n := range r.Nodes {
		sql = sql || n.Kind == dataflow.KindSQL && strings.Contains(n.Label, "$1")
	}
	if !sql || r.Symbol.ID != "(*example.com/deep/api.Handler).GetClient" {
		t.Errorf("JSON trace: sql=%v symbol=%s", sql, r.Symbol.ID)
	}
	out, _, _ = runInspectCLI(t, "dataflow", "--function", "dataflow.Shadow", "--variable", "v", "--non-interactive")
	if out != "" {
		t.Errorf("ambiguous variable printed a result")
	}
	_, errs, code = runInspectCLI(t, "dataflow", "--function", "dataflow.Shadow", "--variable", "v")
	if code != exitAmbiguous || !strings.Contains(errs, "--line") {
		t.Errorf("shadowed variable: %d %s", code, errs)
	}
	if _, _, code := runInspectCLI(t, "dataflow", "--function", "x", "--variable", "y", "--direction", "up"); code != exitUsage {
		t.Errorf("invalid direction accepted")
	}
}

func TestInspectBranchesAndSQLCLI(t *testing.T) {
	out, _, code := runInspectCLI(t, "branches", "--function", "branches.GetClient", "--dot")
	if code != exitOK || !strings.Contains(out, "digraph") || !strings.Contains(out, `label="true"`) {
		t.Errorf("dot: %d\n%s", code, out)
	}
	out, _, _ = runInspectCLI(t, "branches", "--function", "branches.GetClient", "--json")
	var g branch.Graph
	if err := json.Unmarshal([]byte(out), &g); err != nil || g.Metrics.Paths != 3 {
		t.Errorf("json: %v %+v", err, g.Metrics)
	}
	out, _, code = runInspectCLI(t, "sql", "--all", "--json")
	var res sqlops.Result
	if err := json.Unmarshal([]byte(out), &res); err != nil || code != exitOK || len(res.Ops) < 15 {
		t.Fatalf("sql --all: %v code %d, %d ops", err, code, len(res.Ops))
	}
	out, _, _ = runInspectCLI(t, "sql", "--function", "repository.(*ClientRepository).Search")
	if !strings.Contains(out, "SQL query not statically resolved") || !strings.Contains(out, "⟨name⟩") {
		t.Errorf("dynamic SQL:\n%s", out)
	}
}

func TestInspectRefusesToRunWithoutConsent(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("GITVIEW_CACHE_DIR", cache)
	before, _ := filepath.Glob(filepath.Join(cache, "*", "bench-*"))
	_, errs, code := runInspectCLI(t, "performance", "--bench", ".", "--package", "./perf")
	if code != exitConsent || !strings.Contains(errs, "go test") || !strings.Contains(errs, "confirme com --yes") {
		t.Errorf("benchmarks ran without consent: code %d\n%s", code, errs)
	}
	after, _ := filepath.Glob(filepath.Join(cache, "*", "bench-*"))
	if len(after) != len(before) {
		t.Errorf("an artifact directory was created before consent")
	}
	_, errs, code = runInspectCLI(t, "trace", "--test", "TestPipeline", "--package", "./traced")
	if code != exitConsent {
		t.Errorf("trace capture without consent: %d %s", code, errs)
	}
	_, errs, code = runInspectCLI(t, "branches", "--function", "branches.GetClient", "--run-coverage")
	if code != exitConsent {
		t.Errorf("coverage run without consent: %d %s", code, errs)
	}
	_, errs, code = runInspectCLI(t, "sql", "--function", "FindByID", "--explain", "--dsn", "postgres://u:hunter2@127.0.0.1:1/dev")
	if code != exitConsent || strings.Contains(errs, "hunter2") || !strings.Contains(errs, "NÃO é executada") {
		t.Errorf("explain without consent: %d\n%s", code, errs)
	}
	_, errs, code = runInspectCLI(t, "sql", "--function", "FindByID", "--explain", "--yes", "--dsn", "postgres://u:hunter2@db.example.com/prod")
	if code != exitErr || strings.Contains(errs, "hunter2") || !strings.Contains(errs, "não é um servidor local") {
		t.Errorf("remote explain: %d\n%s", code, errs)
	}
	_, errs, code = runInspectCLI(t, "sql", "--function", "repository.(*ClientRepository).Mismatch", "--explain", "--analyze", "--param", "1", "--param", "x", "--yes", "--dsn", "postgres://u@127.0.0.1:1/dev")
	if code != exitErr || !strings.Contains(errs, "explicit permission to run a write") {
		t.Errorf("analyze of a DELETE without --allow-write: %d\n%s", code, errs)
	}
}

func TestInspectImportsAndReport(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.prof")
	os.WriteFile(bad, []byte("garbage"), 0o644)
	if _, errs, code := runInspectCLI(t, "performance", "--cpu", bad); code != exitErr || !strings.Contains(errs, "bad.prof") {
		t.Errorf("invalid profile: %d %s", code, errs)
	}
	if _, errs, code := runInspectCLI(t, "trace", "--import", bad); code != exitErr || !strings.Contains(errs, "bad.prof") {
		t.Errorf("invalid trace: %d %s", code, errs)
	}
	html := filepath.Join(dir, "inspect.html")
	_, errs, code := runInspectCLI(t, "report", "-o", html, "--function", "service.(*ClientService).GetClientByID,branches.Parse")
	if code != exitOK {
		t.Fatalf("report: %d %s", code, errs)
	}
	data, _ := os.ReadFile(html)
	s := string(data)
	var rep struct {
		Inspect, Flow json.RawMessage
	}
	if err := json.Unmarshal(pageSections(t, s)["report"], &rep); err != nil || rep.Inspect == nil || rep.Flow == nil {
		t.Errorf("report without the inspector or Code Flow: %v", err)
	}
	for _, want := range []string{"Go Deep Inspector", "(*example.com/deep/service.ClientService).GetClientByID"} {
		if !strings.Contains(s, want) {
			t.Errorf("report missing %q", want)
		}
	}
	if _, _, code := runInspectCLI(t, "report", "-o", html, "--function", "branches.Parse"); code != exitErr {
		t.Error("existing report overwritten without --overwrite")
	}
	js := filepath.Join(dir, "inspect.json")
	if _, errs, code := runInspectCLI(t, "report", "-o", js, "--function", "branches.Parse"); code != exitOK {
		t.Fatalf("json report: %s", errs)
	}
	var doc map[string]any
	raw, _ := os.ReadFile(js)
	if err := json.Unmarshal(raw, &doc); err != nil || doc["inspector"] == nil {
		t.Errorf("json report without inspector section: %v", err)
	}
}
