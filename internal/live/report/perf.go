package report

import (
	"fmt"
	"time"

	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/live/capture"
	"github.com/athos/gitview/internal/live/linuxperf"
	"github.com/athos/gitview/internal/live/srcmap"
)

// Correlation compares the process while requests were in flight with the
// process while none was, over the active phase. Intervals are those of
// the /proc samples; an interval is busy when any request overlapped it.
type Correlation struct {
	BusySecs float64 `json:"busySecs"`
	IdleSecs float64 `json:"idleSecs"`
	// Process CPU from /proc (measured), % of one core.
	BusyCPU float64 `json:"busyCPU"`
	IdleCPU float64 `json:"idleCPU"`
	// The same intervals in the perf samples (sampled), % of one core.
	HasPerf  bool    `json:"hasPerf"`
	BusyPerf float64 `json:"busyPerf"`
	IdlePerf float64 `json:"idlePerf"`
	// RSS growth over the busy and the idle intervals (measured).
	BusyRSS int64  `json:"busyRSS"`
	IdleRSS int64  `json:"idleRSS"`
	Note    string `json:"note"`
}

// analyzePerf reads the samples of Linux perf; nil when there are none.
func (a *Analysis) analyzePerf(c *capture.Capture, progs []*flow.Program) *linuxperf.Data {
	pr := c.Perf
	if pr == nil || pr.File == "" {
		return nil
	}
	d, err := linuxperf.Load(c.Dir, pr.File)
	if err != nil {
		a.Warnings = append(a.Warnings, "perf samples: "+err.Error())
		return nil
	}
	bucket := max(c.Settings.Interval, 500*time.Millisecond)
	r, err := linuxperf.Analyze(pr, d, c.Phases.InActive, bucket, progs)
	if err != nil {
		a.Warnings = append(a.Warnings, "perf samples: "+err.Error())
		return nil
	}
	a.Perf = r
	a.Warnings = append(a.Warnings, r.Warnings...)
	if len(a.HotPath) == 0 && r.CPU != nil {
		// Without a Go CPU profile, the critical path comes from perf.
		a.HotPath = srcmap.HotPath(r.CPU.Flame)
	}
	return d
}

// correlate places the requests on the process's timeline: the perf
// samples taken during each request (process-wide) and the CPU of the
// process with and without requests in flight.
func (a *Analysis) correlate(c *capture.Capture, d *linuxperf.Data) {
	for i := range a.Requests {
		a.Requests[i].PerfSamples = -1
		if d != nil {
			r := &a.Requests[i]
			r.PerfSamples, _ = d.Between(r.Off, r.Off+r.Dur)
		}
	}
	if len(c.Requests) == 0 || len(c.Samples) < 2 {
		return
	}
	type span struct{ s, e time.Duration }
	var spans []span
	for _, r := range c.Requests {
		if c.Phases.InActive(r.Off) || c.Phases.InActive(r.End()) {
			spans = append(spans, span{r.Off, r.End()})
		}
	}
	if len(spans) == 0 {
		return
	}
	busyAt := func(s, e time.Duration) bool {
		for _, x := range spans {
			if x.s < e && s < x.e {
				return true
			}
		}
		return false
	}
	co := &Correlation{HasPerf: d != nil}
	var busyT, idleT time.Duration
	var busyCPU, idleCPU, busyPerf, idlePerf int64
	for i := 1; i < len(c.Samples); i++ {
		p, s := c.Samples[i-1], c.Samples[i]
		if !c.Phases.InActive(s.T) || !c.Phases.InActive(p.T) || !p.HasCPU || !s.HasCPU || s.T <= p.T {
			continue
		}
		dt := s.T - p.T
		cpu := int64(s.CPUNanos - p.CPUNanos)
		var perfCPU int64
		if d != nil {
			_, perfCPU = d.Between(p.T, s.T)
		}
		var drss int64
		if p.HasMem && s.HasMem {
			drss = int64(s.RSS) - int64(p.RSS)
		}
		if busyAt(p.T, s.T) {
			busyT += dt
			busyCPU += cpu
			busyPerf += perfCPU
			co.BusyRSS += drss
		} else {
			idleT += dt
			idleCPU += cpu
			idlePerf += perfCPU
			co.IdleRSS += drss
		}
	}
	pct := func(cpu int64, t time.Duration) float64 {
		if t <= 0 {
			return 0
		}
		return round(float64(cpu)/float64(t)*100, 1)
	}
	co.BusySecs, co.IdleSecs = round(busyT.Seconds(), 2), round(idleT.Seconds(), 2)
	co.BusyCPU, co.IdleCPU = pct(busyCPU, busyT), pct(idleCPU, idleT)
	if d != nil {
		co.BusyPerf, co.IdlePerf = pct(busyPerf, busyT), pct(idlePerf, idleT)
	}
	co.Note = fmt.Sprintf("Whole-process figures over the /proc sampling intervals (every %s) of the active phase: an interval counts as busy when any request overlapped it, even partly. They show how the process behaves while serving requests, not what one request costs.", c.Settings.Interval)
	a.Correlation = co
}

// cpuViews states the three different CPU figures of a capture, which
// must not be confused: the process total, the CPU sampled by function and
// the CPU attributable to requests.
func (a *Analysis) cpuViews(c *capture.Capture) {
	act := a.Process.Phases[1]
	total := Metric{Name: "CPU of the whole process (active phase)", Unit: "ns", Scope: ScopeProcess}
	if act.HasCPU {
		total.Value, total.Basis = float64(act.CPUTime), Measured
		total.Note = "Scheduler time of every thread, read from /proc: the reference total."
	} else {
		total.Basis, total.Note = Unavailable, "/proc CPU counters could not be read."
	}
	a.CPUViews = append(a.CPUViews, total)

	byFunc := Metric{Name: "CPU sampled by function (active phase)", Unit: "ns", Scope: ScopeProcess, Basis: Unavailable,
		Note: "No CPU profile: neither Linux perf nor a Go CPU profile (net/http/pprof or the collector) was available."}
	switch {
	case a.Profiles != nil && a.Profiles.CPU != nil:
		byFunc.Value, byFunc.Basis = float64(a.Profiles.CPU.Total), Sampled
		byFunc.Note = fmt.Sprintf("Go CPU profile: samples × 10 ms, covering %.0f%% of the active phase.", 100*a.Profiles.CPUCoverage)
		if a.Perf != nil && a.Perf.CPU != nil {
			byFunc.Note += fmt.Sprintf(" Linux perf sampled %s over the same phase.", time.Duration(a.Perf.SampledCPU).Round(time.Millisecond))
		}
	case a.Perf != nil && a.Perf.CPU != nil:
		byFunc.Value, byFunc.Basis = float64(a.Perf.SampledCPU), Sampled
		byFunc.Note = fmt.Sprintf("Linux perf: %d samples × the sampling period (%d Hz per thread).", a.Perf.Samples, a.Perf.Freq)
		if a.Perf.UserOnly {
			byFunc.Note += " User space only: lower than the process total by the time spent in the kernel."
		}
	}
	a.CPUViews = append(a.CPUViews, byFunc)

	byReq := Metric{Name: "CPU attributable to requests (active phase)", Unit: "ns", Scope: ScopeRequest, Basis: Unavailable,
		Note: "Only the Go CPU profile with GitView's collector (pprof labels per request) attributes CPU to requests; /proc and Linux perf see the whole process."}
	if a.Profiles != nil && a.Profiles.CPULabelled > 0 {
		byReq.Value, byReq.Basis = float64(a.Profiles.CPULabelled), Sampled
		byReq.Note = fmt.Sprintf("Samples labelled with a request by the collector; %s of the sampled CPU had no request label (background work, GC, idle connections).", time.Duration(a.Profiles.CPUUnlabelled).Round(time.Millisecond))
	}
	a.CPUViews = append(a.CPUViews, byReq)
}
