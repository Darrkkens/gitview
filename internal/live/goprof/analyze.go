package goprof

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/inspect/perf"
)

// LabelTraceID is the pprof label the collector puts on the goroutines
// serving a request (collector.LabelTraceID).
const LabelTraceID = "gitview_trace_id"

func load(dir string, w *Window) (*perf.Profile, error) {
	if w == nil || w.File == "" {
		return nil, nil
	}
	data, err := os.ReadFile(filepath.Join(dir, w.File))
	if err != nil {
		return nil, err
	}
	return perf.Parse(data)
}

// CPUProfile merges the CPU windows of a capture into one profile; nil
// when none was collected.
func CPUProfile(dir string, res *Result) (*perf.Profile, error) {
	var parts []*perf.Profile
	for i := range res.CPU {
		p, err := load(dir, &res.CPU[i])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", res.CPU[i].File, err)
		}
		if p != nil {
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 {
		return nil, nil
	}
	return perf.Combine(parts, nil)
}

// delta subtracts the start snapshot from the end one.
func delta(dir string, start, end *Window) (*perf.Profile, error) {
	a, err := load(dir, start)
	if err != nil {
		return nil, err
	}
	b, err := load(dir, end)
	if err != nil || a == nil || b == nil {
		return nil, err
	}
	return perf.Combine([]*perf.Profile{b, a}, []int64{1, -1})
}

// Profiles are the analysed profiles of a capture.
type Profiles struct {
	CPU *perf.Report `json:"cpu,omitempty"`
	// CPUCoverage is the part of the active phase covered by CPU windows.
	CPUCoverage float64 `json:"cpuCoverage,omitempty"`
	// CPUByTrace is the sampled CPU time of each request, from the pprof
	// labels the collector sets (nanoseconds).
	CPUByTrace    map[string]int64 `json:"cpuByTrace,omitempty"`
	CPULabelled   int64            `json:"cpuLabelled,omitempty"`
	CPUUnlabelled int64            `json:"cpuUnlabelled,omitempty"`
	Allocs        *perf.Report     `json:"allocs,omitempty"`  // allocated during the capture
	Objects       int64            `json:"objects,omitempty"` // objects allocated during the capture
	Inuse         *perf.Report     `json:"inuse,omitempty"`   // live heap at the end
	Mutex         *perf.Report     `json:"mutex,omitempty"`   // contention during the capture
	Block         *perf.Report     `json:"block,omitempty"`   // blocking during the capture
	Goroutines    *perf.Report     `json:"goroutines,omitempty"`
	// GoroutinesByTrace counts the goroutines of each in-flight request in
	// the goroutine profile.
	GoroutinesByTrace map[string]int `json:"goroutinesByTrace,omitempty"`
	Notes             []string       `json:"notes,omitempty"`
}

// Active is the active phase of a capture: its bounds and its pauses.
type Active struct {
	Start, End time.Duration
	Paused     []Window
}

// overlap is the part of [a, b] inside the active phase, pauses excluded.
func (ac Active) overlap(a, b time.Duration) time.Duration {
	clip := func(a, b, x, y time.Duration) time.Duration {
		return max(min(b, y)-max(a, x), 0)
	}
	d := clip(a, b, ac.Start, ac.End)
	for _, p := range ac.Paused {
		lo, hi := max(a, ac.Start, p.Start), min(b, ac.End, p.End)
		if hi > lo {
			d -= hi - lo
		}
	}
	return max(d, 0)
}

// Covered reports whether [a, b] lies entirely inside a successful CPU
// window (its samples are then all in the profile).
func (r *Result) Covered(a, b time.Duration) bool {
	for _, w := range r.CPU {
		if w.File != "" && w.Start <= a && b <= w.End {
			return true
		}
	}
	return false
}

// Analyze reads the profiles of a capture.
func Analyze(dir string, res *Result, ac Active, progs []*flow.Program) (*Profiles, error) {
	out := &Profiles{}
	note := func(format string, args ...any) { out.Notes = append(out.Notes, fmt.Sprintf(format, args...)) }
	cpu, err := CPUProfile(dir, res)
	if err != nil {
		return nil, err
	}
	if cpu != nil {
		r, err := perf.Analyze(cpu, progs, perf.Options{SampleType: "cpu"})
		if err != nil {
			return nil, err
		}
		out.CPU = r
		var covered, outside time.Duration
		for _, w := range res.CPU {
			if w.File != "" {
				in := ac.overlap(w.Start, w.End)
				covered += in
				outside += w.End - w.Start - in
			}
		}
		if active := ac.overlap(ac.Start, ac.End); active > 0 {
			out.CPUCoverage = min(1, float64(covered)/float64(active))
		}
		if outside > 100*time.Millisecond {
			note("The CPU profile also covers %.1f s outside the active phase (the last window finishes after the stop, or a pause): those samples are in the process totals.", outside.Seconds())
		}
		idx := sampleIndex(cpu, "cpu")
		for _, s := range cpu.Samples {
			if idx < 0 {
				break
			}
			v := s.Values[idx]
			if id := s.Labels[LabelTraceID]; id != "" {
				if out.CPUByTrace == nil {
					out.CPUByTrace = map[string]int64{}
				}
				out.CPUByTrace[id] += v
				out.CPULabelled += v
			} else {
				out.CPUUnlabelled += v
			}
		}
	}
	if allocs, err := delta(dir, res.HeapStart, res.HeapEnd); err != nil {
		note("allocation delta: %v", err)
	} else if allocs != nil {
		if r, err := perf.Analyze(allocs, progs, perf.Options{SampleType: "alloc_space"}); err == nil {
			r.Meaning = "bytes allocated between the two heap snapshots (sampled allocations scaled to estimates)"
			out.Allocs = r
		}
		if r, err := perf.Analyze(allocs, progs, perf.Options{SampleType: "alloc_objects"}); err == nil {
			out.Objects = r.Total
		}
		if !res.Plan.HeapGC {
			note("Heap snapshots were taken without a garbage collection: the allocation delta covers the GC cycles completed between them, not exactly the capture.")
		}
	}
	if end, err := load(dir, res.HeapEnd); err == nil && end != nil {
		if r, err := perf.Analyze(end, progs, perf.Options{SampleType: "inuse_space"}); err == nil {
			out.Inuse = r
		}
	}
	for _, c := range []struct {
		start, end *Window
		dst        **perf.Report
		name       string
	}{{res.MutexStart, res.MutexEnd, &out.Mutex, "mutex"}, {res.BlockStart, res.BlockEnd, &out.Block, "block"}} {
		d, err := delta(dir, c.start, c.end)
		if err != nil {
			note("%s delta: %v", c.name, err)
			continue
		}
		if d == nil {
			continue
		}
		r, err := perf.Analyze(d, progs, perf.Options{SampleType: "delay"})
		if err != nil {
			continue
		}
		r.Kind = c.name
		if r.Total == 0 && !res.Plan.SetRates {
			note("The %s profile recorded nothing during the capture: either there was none, or %s profiling is not enabled in the application.", c.name, c.name)
		}
		*c.dst = r
	}
	if g, err := load(dir, res.Goroutine); err == nil && g != nil {
		if r, err := perf.Analyze(g, progs, perf.Options{}); err == nil {
			out.Goroutines = r
		}
		for _, s := range g.Samples {
			if id := s.Labels[LabelTraceID]; id != "" && len(s.Values) > 0 {
				if out.GoroutinesByTrace == nil {
					out.GoroutinesByTrace = map[string]int{}
				}
				out.GoroutinesByTrace[id] += int(s.Values[0])
			}
		}
	}
	return out, nil
}

func sampleIndex(p *perf.Profile, typ string) int {
	for i, t := range p.SampleTypes {
		if t.Type == typ {
			return i
		}
	}
	return -1
}

// ForTraces analyses the CPU samples of the given requests only.
func ForTraces(cpu *perf.Profile, ids map[string]bool, progs []*flow.Program) (*perf.Report, error) {
	sub := cpu.Filter(func(s perf.Sample) bool { return ids[s.Labels[LabelTraceID]] })
	return perf.Analyze(sub, progs, perf.Options{SampleType: "cpu"})
}
