package export

import (
	"encoding/json"
	"html"
	"regexp"
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
	for _, want := range []string{`id="inspect-data"`, `href="#inspect"`, `id="inspect-app"`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q", want)
		}
	}
	csp := regexp.MustCompile(`Content-Security-Policy" content="([^"]+)"`).FindStringSubmatch(s)
	if len(csp) == 2 {
		csp[1] = html.UnescapeString(csp[1])
	}
	if len(csp) < 2 || !strings.Contains(csp[1], "default-src 'none'") || strings.Count(csp[1], "sha256-") != 5 {
		t.Fatalf("CSP %v", csp)
	}
	// Every inline script is allowed by hash, so nothing else can run.
	for _, m := range regexp.MustCompile(`(?s)<script>(.*?)</script>`).FindAllStringSubmatch(s, -1) {
		if !strings.Contains(csp[1], scriptHash(m[1])) {
			t.Errorf("inline script not pinned by the CSP: %.60q", m[1])
		}
	}
	// The data block holds valid JSON with the original text.
	data := regexp.MustCompile(`(?s)<script type="application/json" id="inspect-data">(.*?)</script>`).FindStringSubmatch(s)
	var d struct {
		Symbols []struct {
			Symbol evidence.SymbolRef `json:"symbol"`
		} `json:"symbols"`
	}
	if err := json.Unmarshal([]byte(data[1]), &d); err != nil || d.Symbols[0].Symbol.Qualified != "x."+hostile {
		t.Errorf("data block: %v %+v", err, d)
	}
	// The script inserts repository text with textContent only.
	if strings.Contains(inspectJS, "innerHTML") || strings.Contains(inspectJS, "eval(") || strings.Contains(inspectJS, "insertAdjacentHTML") {
		t.Error("inspector script uses an HTML sink")
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
	if strings.Contains(string(plain), `id="inspect-data"`) || strings.Contains(string(plain), `href="#inspect"`) {
		t.Error("a report without inspector data must not pretend to have it")
	}
}
