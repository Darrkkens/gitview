package export

import (
	"bytes"
	"html"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/athos/gitview/internal/inspect/perf"
	"github.com/athos/gitview/internal/live/capture"
	"github.com/athos/gitview/internal/live/linuxperf"
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

// The page carries the perf samples: flame graph and functions, the
// timeline, the three CPU figures and the correlation with the requests;
// the per-line statistics it does not show are dropped.
func TestRuntimeHTMLCarriesPerf(t *testing.T) {
	a := hostileAnalysis("P1")
	cpu := &perf.Report{Kind: "cpu", Unit: "nanoseconds", Total: 30e6, Samples: 3,
		Funcs: []perf.FuncStat{{Name: "main.burn", File: "main.go", Line: 32, Flat: 30e6, Cum: 30e6}},
		Lines: []perf.LineStat{{Func: "main.burn", File: "main.go", Line: 33}},
		Flame: &perf.FlameNode{Name: "root", Value: 30e6, Children: []*perf.FlameNode{{Name: "main.burn", Value: 30e6, Self: 30e6}}}}
	a.Perf = &linuxperf.Report{CPU: cpu, Event: "cpu-clock:u", Freq: 99, Samples: 3, Recorded: 4, SampledCPU: 30e6,
		Timeline: []linuxperf.Bucket{{T: 0, Samples: 1, CPU: 2}, {T: 0.5, Samples: 3, CPU: 6}}}
	a.CPUViews = []report.Metric{{Name: "CPU of the whole process (active phase)", Value: 4e7, Unit: "ns", Basis: report.Measured, Scope: report.ScopeProcess}}
	a.Correlation = &report.Correlation{BusySecs: 1, IdleSecs: 2, BusyCPU: 40, HasPerf: true, BusyPerf: 30}
	a.Requests[0].PerfSamples = 2
	var buf bytes.Buffer
	if err := RuntimeHTML(&buf, a, nil); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		A *report.Analysis `json:"a"`
	}
	pageSection(t, buf.String(), "runtime", &payload)
	p := payload.A.Perf
	if p == nil || p.CPU == nil || len(p.CPU.Funcs) != 1 || p.CPU.Flame == nil || len(p.Timeline) != 2 {
		t.Fatalf("perf in the page: %+v", p)
	}
	if len(p.CPU.Lines) != 0 {
		t.Error("per-line statistics were not dropped")
	}
	if len(a.Perf.CPU.Lines) != 1 {
		t.Error("slimming modified the analysis")
	}
	if payload.A.Correlation == nil || payload.A.Correlation.BusyPerf != 30 || len(payload.A.CPUViews) != 1 || payload.A.Requests[0].PerfSamples != 2 {
		t.Errorf("correlation %+v, CPU views %+v", payload.A.Correlation, payload.A.CPUViews)
	}
}
