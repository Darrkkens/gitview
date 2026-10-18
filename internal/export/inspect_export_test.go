package export

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/athos/gitview/internal/evidence"
	"github.com/athos/gitview/internal/inspect"
	"github.com/athos/gitview/internal/inspect/branch"
	"github.com/athos/gitview/internal/inspect/sqlops"
)

const hostile = `</script><script>alert(1)</script><img src=x onerror=alert(2)>`

func hostileReport() Report {
	b := &inspect.Bundle{
		Symbol:   evidence.SymbolRef{ID: "example.com/x." + hostile, Qualified: "x." + hostile, File: "x.go", Line: 1},
		Code:     "func f() { s := \"" + hostile + "\" }",
		Branches: &branch.Graph{Blocks: []*branch.Block{{ID: 0, Stmts: []branch.Stmt{{Line: 1, Text: hostile}}, Reachable: true, Entry: true}}},
		SQL:      &sqlops.Result{Ops: []*sqlops.Op{{ID: "db:x.go:1:Exec", Lib: "pgx", Op: "Exec", SQL: sqlops.SQLText{Kind: "literal", Text: "SELECT '" + hostile + "'"}}}},
		Perf:     &inspect.PerfView{Message: inspect.NoProfile},
		Trace:    &inspect.TraceView{Message: inspect.NoTrace},
	}
	return Report{Project: "p", GeneratedAt: time.Unix(0, 0), Inspect: &InspectData{Symbols: []*inspect.Bundle{b}}}
}

func TestInspectorSectionIsEscapedAndPinnedByCSP(t *testing.T) {
	out, err := Render(HTML, hostileReport())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Count(s, "<script>alert(1)</script>") > 0 || strings.Contains(s, "<img src=x") {
		t.Fatal("repository content broke out of the data block")
	}
	// The section holds the original text.
	var d struct {
		Inspect struct {
			Symbols []struct {
				Symbol evidence.SymbolRef `json:"symbol"`
			} `json:"symbols"`
		} `json:"inspect"`
	}
	pageSection(t, s, "report", &d)
	if len(d.Inspect.Symbols) != 1 || d.Inspect.Symbols[0].Symbol.Qualified != "x."+hostile {
		t.Errorf("section: %+v", d)
	}
}

func TestInspectorInJSONAndMarkdown(t *testing.T) {
	out, err := Render(JSON, hostileReport())
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil || doc["inspector"] == nil {
		t.Fatalf("json: %v", err)
	}
	md := string(renderMarkdown(hostileReport()))
	if !strings.Contains(md, "## Go Deep Inspector") || !strings.Contains(md, inspect.NoTrace) {
		t.Errorf("markdown:\n%s", md)
	}
	plain, _ := Render(HTML, Report{Project: "p", GeneratedAt: time.Unix(0, 0)})
	var none ReportSection
	pageSection(t, string(plain), "report", &none)
	if none.Inspect != nil {
		t.Error("a report without inspector data must not pretend to have it")
	}
}
