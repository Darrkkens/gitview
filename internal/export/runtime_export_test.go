package export

import (
	"bytes"
	"html"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/athos/gitview/internal/live/capture"
	"github.com/athos/gitview/internal/live/report"
)

func hostileAnalysis(id string) *report.Analysis {
	evil := `</script><script>alert(1)</script><img src=x onerror=alert(2)>`
	return &report.Analysis{
		ID: id, Label: evil, Created: time.Unix(1000, 0), Status: capture.StatusComplete,
		ProcessInfo: capture.ProcessInfo{PID: 7, Exe: evil},
		Process:     report.Process{Phases: []report.PhaseStats{{Name: "baseline"}, {Name: "active"}, {Name: "cooldown"}}},
		Routes: []report.RouteStats{{Key: "GET " + evil, Count: 1, Status: map[int]int{200: 1},
			Statements: []report.StatementStats{{Query: "SELECT '" + evil + "'", Count: 1}}}},
		Requests:    []report.RequestRow{{ID: "r1", Key: "GET " + evil, Path: evil, Status: 200, Phase: "active"}},
		Limitations: []string{evil},
	}
}

func TestRuntimeHTMLIsInertAndPinned(t *testing.T) {
	a, b := hostileAnalysis("A1"), hostileAnalysis("B2")
	var buf bytes.Buffer
	if err := RuntimeHTML(&buf, b, report.Compare(a, b)); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	if strings.Contains(s, "<script>alert") || strings.Contains(s, "<img src=x") {
		t.Fatal("hostile markup reached the page")
	}
	csp := regexp.MustCompile(`Content-Security-Policy" content="([^"]+)"`).FindStringSubmatch(s)
	if len(csp) < 2 {
		t.Fatal("no CSP")
	}
	if policy := html.UnescapeString(csp[1]); !strings.Contains(policy, "default-src 'none'") || strings.Count(policy, "sha256-") != 1 || strings.Contains(policy, "connect-src") {
		t.Errorf("CSP: %s", policy)
	}
	var payload struct {
		A          *report.Analysis   `json:"a"`
		Base       *report.Analysis   `json:"base"`
		Comparison *report.Comparison `json:"comparison"`
	}
	pageSection(t, s, "runtime", &payload)
	if payload.A == nil || payload.A.ID != "B2" || payload.Comparison == nil || payload.Base == nil || payload.Base.ID != "A1" {
		t.Fatalf("payload: %+v", payload)
	}
	if !strings.Contains(payload.A.Label, "<script>") {
		t.Error("the label should survive as data")
	}
	// No network access: nothing is loaded from elsewhere.
	if regexp.MustCompile(`(src|href)="(https?:)?//`).MatchString(s) {
		t.Error("external resource referenced")
	}
}
