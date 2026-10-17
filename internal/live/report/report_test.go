package report

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/athos/gitview/internal/live/capture"
	"github.com/athos/gitview/internal/live/httpcol"
	"github.com/athos/gitview/internal/live/proc"
)

const ms = time.Millisecond

// synthetic builds a capture: 1 s baseline at 10 % CPU, active phase from
// 1 s to 4 s, cooldown to 5 s; samples every 100 ms.
func synthetic(durs []time.Duration, cpus int) *capture.Capture {
	c := &capture.Capture{ID: "c1", Created: time.Unix(1000, 0), Status: capture.StatusComplete,
		Process:  capture.ProcessInfo{PID: 7, Exe: "/bin/app", MainModule: "example.com/app", CPUs: cpus},
		Settings: capture.Settings{Interval: 100 * ms},
		Phases:   capture.Phases{ActiveStart: time.Second, ActiveEnd: 4 * time.Second, End: 5 * time.Second},
		Sources:  []string{"proc", "proxy"}}
	var cpu uint64
	for t := time.Duration(0); t <= 5*time.Second; t += 100 * ms {
		rate := 0.10
		if t > time.Second && t <= 4*time.Second {
			rate = 0.50
		}
		if t > 0 {
			cpu += uint64(rate * float64(100*ms))
		}
		c.Samples = append(c.Samples, proc.Sample{T: t, HasCPU: true, CPUNanos: cpu, CPUPct: rate * 100, CPUExact: true,
			HasMem: true, RSS: uint64(10<<20 + int(t/ms)*1024), Threads: 8})
	}
	c.Samples[0].CPUPct = -1
	off := 1200 * ms
	for i, d := range durs {
		r := httpcol.Request{ID: string(rune('a' + i)), Sources: []httpcol.Source{httpcol.SourceProxy}, Method: "GET", Path: "/x/1", Status: 200,
			Start: c.Created.Add(off), Off: off, HasProxy: true, ProxyDur: d, InFlight: 1}
		r.SetKey()
		c.Requests = append(c.Requests, r)
		off += d + 50*ms
	}
	return c
}

func TestPhasesAndLatency(t *testing.T) {
	c := synthetic([]time.Duration{10 * ms, 20 * ms, 30 * ms, 40 * ms, 300 * ms}, 4)
	// A request in the cooldown is not part of the route's statistics.
	late := c.Requests[0]
	late.ID, late.Off = "late", 4500*ms
	c.Requests = append(c.Requests, late)
	a, err := Analyze(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	base, act := a.Process.Phases[0], a.Process.Phases[1]
	if math.Abs(base.CPUAvg-10) > 0.1 || math.Abs(act.CPUAvg-50) > 0.1 || act.CPUTime != int64(1500*ms) {
		t.Errorf("phases: baseline %.1f active %.1f time %d", base.CPUAvg, act.CPUAvg, act.CPUTime)
	}
	r := a.Routes[0]
	if r.Key != "GET /x/{n}" || r.Count != 5 || r.Outside != 1 {
		t.Fatalf("route: %+v", r)
	}
	l := r.Latency
	if l.Min != 10*ms || l.P50 != 30*ms || l.P90 != 300*ms || l.Max != 300*ms || l.Mean != 80*ms {
		t.Errorf("latency (nearest rank): %+v", l)
	}
	// Only the 300 ms request lasts two sampling intervals: its CPU above
	// the baseline rate is estimated, 300 ms × (50 % − 10 %) = 120 ms.
	var est *Metric
	for i := range r.Metrics {
		if r.Metrics[i].Basis == Estimated {
			est = &r.Metrics[i]
		}
	}
	if est == nil || math.Abs(est.Value-float64(120*ms)) > float64(2*ms) || est.Scope != ScopeRequest {
		t.Errorf("estimate: %+v", est)
	}
	for _, m := range r.Metrics {
		if m.Basis == Sampled {
			t.Errorf("sampled metric without a profile: %+v", m)
		}
	}
	if !strings.Contains(strings.Join(a.Limitations, "\n"), "No profile was collected") {
		t.Errorf("limitations: %v", a.Limitations)
	}
}

func TestOverlappingRequestsAreNotAttributed(t *testing.T) {
	c := synthetic([]time.Duration{400 * ms, 400 * ms}, 4)
	c.Requests[1].Off = c.Requests[0].Off + 100*ms // overlaps the first
	a, _ := Analyze(c, nil)
	r := a.Routes[0]
	if r.Isolated != 0 {
		t.Errorf("isolated: %d", r.Isolated)
	}
	for _, m := range r.Metrics {
		if m.Basis == Estimated {
			t.Errorf("estimate from overlapping requests: %+v", m)
		}
	}
}

func durations(base time.Duration, n int, step time.Duration) []time.Duration {
	var out []time.Duration
	for i := 0; i < n; i++ {
		out = append(out, base+time.Duration(i%3)*step)
	}
	return out
}

func TestCompareVerdicts(t *testing.T) {
	a, _ := Analyze(synthetic(durations(20*ms, 10, ms), 4), nil)
	b, _ := Analyze(synthetic(durations(30*ms, 10, ms), 4), nil)
	c := Compare(a, b)
	if !c.Comparable || len(c.Routes) != 1 || c.Routes[0].Verdict != Slower || len(c.Regressions) != 1 {
		t.Fatalf("slower: %+v", c.Routes)
	}
	same, _ := Analyze(synthetic(durations(20*ms, 10, ms), 4), nil)
	if v := Compare(a, same).Routes[0].Verdict; v != NoChange {
		t.Errorf("same durations: %s", v)
	}
	// Different machines: every verdict is inconclusive.
	other, _ := Analyze(synthetic(durations(30*ms, 10, ms), 8), nil)
	c = Compare(a, other)
	if c.Comparable || c.Routes[0].Verdict != Inconclusive || len(c.Regressions) != 0 {
		t.Errorf("different machines: comparable=%v %+v", c.Comparable, c.Routes[0])
	}
	// Few requests.
	few, _ := Analyze(synthetic(durations(30*ms, 3, ms), 4), nil)
	if r := Compare(a, few).Routes[0]; r.Verdict != Inconclusive || !strings.Contains(strings.Join(r.Reasons, ";"), "too few") {
		t.Errorf("few requests: %+v", r)
	}
	// Different concurrency.
	busy := synthetic(durations(30*ms, 10, ms), 4)
	busy.Requests[3].InFlight = 4
	bz, _ := Analyze(busy, nil)
	if r := Compare(a, bz).Routes[0]; r.Verdict != Inconclusive || !strings.Contains(strings.Join(r.Reasons, ";"), "concurrency") {
		t.Errorf("concurrency: %+v", r)
	}
	// Undefined numbers are null in JSON.
	data, err := json.Marshal(Compare(a, few))
	if err != nil || strings.Contains(string(data), "NaN") {
		t.Errorf("json: %v %s", err, data)
	}
	var back Comparison
	if err := json.Unmarshal(data, &back); err != nil {
		t.Errorf("json round trip: %v", err)
	}
}

func TestHistDelta(t *testing.T) {
	a := httpcol.Histogram{Counts: []uint64{1, 0, 0}, Buckets: []float64{-1e308, 0.001, 0.01, 1e308}}
	b := httpcol.Histogram{Counts: []uint64{1, 98, 2}, Buckets: a.Buckets}
	n, sum, p99, ok := histDelta(a, b)
	if !ok || n != 100 || p99 != 0.01 || sum <= 0 {
		t.Errorf("histDelta: n=%d sum=%g p99=%g ok=%v", n, sum, p99, ok)
	}
	if _, _, _, ok := histDelta(a, httpcol.Histogram{Counts: []uint64{1}}); ok {
		t.Error("mismatched histograms accepted")
	}
}

func TestHolmAndUnknownConcurrency(t *testing.T) {
	routes := []RouteDelta{{Key: "a", P: 0.03, Change: 20, Verdict: Slower}, {Key: "b", P: 0.04, Change: 20, Verdict: Slower}}
	holm(routes)
	for _, r := range routes {
		if r.Verdict != NoChange || !strings.Contains(strings.Join(r.Reasons, ";"), "Holm") {
			t.Errorf("not corrected: %+v", r)
		}
	}
	strong := []RouteDelta{{Key: "a", P: 0.001, Change: 20, Verdict: Slower}, {Key: "b", P: 0.5, Verdict: NoChange}}
	holm(strong)
	if strong[0].Verdict != Slower {
		t.Errorf("strong difference dropped: %+v", strong[0])
	}
	a, _ := Analyze(synthetic(durations(20*ms, 10, ms), 4), nil)
	otelOnly := synthetic(durations(30*ms, 10, ms), 4)
	for i := range otelOnly.Requests {
		otelOnly.Requests[i].InFlight = 0
	}
	b, _ := Analyze(otelOnly, nil)
	if r := Compare(a, b).Routes[0]; r.Verdict != Inconclusive || !strings.Contains(strings.Join(r.Reasons, ";"), "concurrency not measured") {
		t.Errorf("unknown concurrency: %+v", r)
	}
	// 2 vs 64 requests in flight is not comparable.
	busy := synthetic(durations(30*ms, 10, ms), 4)
	for i := range busy.Requests {
		busy.Requests[i].InFlight = 64
	}
	two := synthetic(durations(20*ms, 10, ms), 4)
	for i := range two.Requests {
		two.Requests[i].InFlight = 2
	}
	x, _ := Analyze(two, nil)
	y, _ := Analyze(busy, nil)
	if r := Compare(x, y).Routes[0]; r.Verdict != Inconclusive {
		t.Errorf("2 vs 64 in flight: %+v", r)
	}
	// Proxy-only requests: SQL unknown, not zero.
	if !strings.Contains(fmt.Sprint(a.Routes[0].SQLObserved), "false") || a.Requests[0].SQL != -1 {
		t.Errorf("SQL observability: %v %d", a.Routes[0].SQLObserved, a.Requests[0].SQL)
	}
}
