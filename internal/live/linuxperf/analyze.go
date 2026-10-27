package linuxperf

import (
	"fmt"
	"time"

	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/inspect/perf"
)

// Result is what a capture records about perf (in capture.json).
type Result struct {
	Bin      string        `json:"bin,omitempty"`
	Version  string        `json:"version,omitempty"`
	Event    string        `json:"event"`
	Freq     int           `json:"freq"`
	UserOnly bool          `json:"userOnly,omitempty"`
	Start    time.Duration `json:"start"` // offsets in the capture
	End      time.Duration `json:"end"`
	File     string        `json:"file,omitempty"` // DataFile, when samples were kept
	Samples  int64         `json:"samples"`        // written by perf record
	Lost     int64         `json:"lost,omitempty"` // events perf dropped (buffer overflow)
	Inlined  bool          `json:"inlined,omitempty"`
	Early    bool          `json:"early,omitempty"` // perf stopped with the process
	Error    string        `json:"error,omitempty"`
	Notes    []string      `json:"notes,omitempty"`
}

// Bucket is the sampled CPU of one slice of the timeline.
type Bucket struct {
	T       float64 `json:"t"` // seconds, start of the slice
	Samples int     `json:"samples"`
	CPU     float64 `json:"cpu"` // sampled CPU, % of one core
}

// Report is the analysed perf data of a capture.
type Report struct {
	CPU      *perf.Report `json:"cpu,omitempty"`
	Event    string       `json:"event"`
	Freq     int          `json:"freq"`
	UserOnly bool         `json:"userOnly,omitempty"`
	Recorded int          `json:"recorded"` // samples in the recording
	Samples  int          `json:"samples"`  // samples in the active phase
	// SampledCPU is samples × period in the active phase (ns): an estimate
	// of the CPU time, not the measured total of /proc.
	SampledCPU int64    `json:"sampledCPU"`
	Stats      Stats    `json:"stats"`
	BucketSecs float64  `json:"bucketSecs"`
	Timeline   []Bucket `json:"timeline"`
	Notes      []string `json:"notes,omitempty"`
	Warnings   []string `json:"warnings,omitempty"`
}

// Profile converts the samples kept by keep into a pprof-shaped profile
// (sample types samples/count and cpu/nanoseconds).
func (d *Data) Profile(keep func(t time.Duration) bool) *perf.Profile {
	p := &perf.Profile{
		SampleTypes: []perf.ValueType{{Type: "samples", Unit: "count"}, {Type: "cpu", Unit: "nanoseconds"}},
		DefaultType: "cpu",
		Locations:   map[uint64]*perf.Location{},
		Functions:   map[uint64]*perf.Function{},
		PeriodType:  perf.ValueType{Type: "cpu", Unit: "nanoseconds"},
	}
	fns := map[string]*perf.Function{}
	for i, f := range d.Frames {
		fn := fns[f.Func+"\x00"+f.File]
		if fn == nil {
			fn = &perf.Function{ID: uint64(len(fns) + 1), Name: f.Func, SystemName: f.Func, File: f.File, StartLine: int64(f.Start)}
			fns[f.Func+"\x00"+f.File] = fn
			p.Functions[fn.ID] = fn
		}
		p.Locations[uint64(i+1)] = &perf.Location{ID: uint64(i + 1), Lines: []perf.Line{{Func: fn, Line: int64(f.Line)}}}
	}
	agg := map[int32]int{}
	for i, t := range d.T {
		if keep != nil && !keep(time.Duration(t)) {
			continue
		}
		k, ok := agg[d.S[i]]
		if !ok {
			st := d.Stacks[d.S[i]]
			locs := make([]uint64, len(st))
			for j, f := range st {
				locs[j] = uint64(f) + 1
			}
			k = len(p.Samples)
			agg[d.S[i]] = k
			p.Samples = append(p.Samples, perf.Sample{Locs: locs, Values: []int64{0, 0}})
		}
		p.Samples[k].Values[0]++
		p.Samples[k].Values[1] += d.P[i]
		if p.Period == 0 {
			p.Period = d.P[i]
		}
	}
	if len(d.T) > 0 {
		p.DurationNanos = d.T[len(d.T)-1] - d.T[0]
	}
	return p
}

// Analyze reports the samples of the active phase (keep), with a timeline
// of every sample in buckets of the given width.
func Analyze(res *Result, d *Data, keep func(time.Duration) bool, bucket time.Duration, progs []*flow.Program) (*Report, error) {
	r := &Report{Event: res.Event, Freq: res.Freq, UserOnly: res.UserOnly, Recorded: len(d.T), Stats: d.Stats}
	prof := d.Profile(keep)
	for _, s := range prof.Samples {
		r.Samples += int(s.Values[0])
		r.SampledCPU += s.Values[1]
	}
	if r.Samples > 0 {
		rep, err := perf.Analyze(prof, progs, perf.Options{SampleType: "cpu"})
		if err != nil {
			return nil, err
		}
		rep.Kind = "cpu"
		rep.Meaning = "CPU time sampled by Linux perf (" + res.Event + fmt.Sprintf(" at %d Hz", res.Freq) + "): samples × sampling period, a statistical estimate"
		if res.Freq > 0 {
			rep.Period = fmt.Sprintf("%d Hz per thread", res.Freq)
		}
		r.CPU = rep
	}
	if bucket <= 0 {
		bucket = 500 * time.Millisecond
	}
	r.BucketSecs = bucket.Seconds()
	if len(d.T) > 0 {
		from := time.Duration(d.T[0]).Truncate(bucket)
		if res.Start > 0 && res.Start < from {
			from = res.Start.Truncate(bucket)
		}
		end := max(time.Duration(d.T[len(d.T)-1]), res.End)
		for t := from; t <= end; t += bucket {
			n, cpu := d.Between(t, t+bucket)
			r.Timeline = append(r.Timeline, Bucket{T: t.Seconds(), Samples: n, CPU: float64(cpu) / float64(bucket) * 100})
			if len(r.Timeline) > 20000 {
				break
			}
		}
	}
	st := d.Stats
	pct := func(n int) float64 { return 100 * float64(n) / float64(max(st.Samples, 1)) }
	if st.Samples > 0 && st.UnknownLeaf > 0 {
		msg := fmt.Sprintf("%.0f%% of the samples (%d) are in code perf could not name ([unknown]).", pct(st.UnknownLeaf), st.UnknownLeaf)
		if pct(st.UnknownLeaf) >= 20 {
			msg += " The executable may have been rebuilt or removed since the process started, or it lacks symbols."
			r.Warnings = append(r.Warnings, msg)
		} else {
			r.Notes = append(r.Notes, msg)
		}
	}
	if st.ByPclntab > 0 {
		r.Notes = append(r.Notes, fmt.Sprintf("%d frames had no ELF symbol (a stripped binary?) and were named from Go's own table (pclntab).", st.ByPclntab))
	}
	if st.Broken > 0 {
		r.Notes = append(r.Notes, fmt.Sprintf("%.0f%% of the call stacks are cut short: frames in assembly or C code without frame pointers end the chain perf can follow; their callers are missing from the flame graph.", pct(st.Broken)))
	}
	if st.Foreign > 0 {
		r.Notes = append(r.Notes, fmt.Sprintf("%.0f%% of the samples are in other objects (C libraries, vdso): cgo or system code.", pct(st.Foreign)))
	}
	if res.UserOnly {
		r.Notes = append(r.Notes, "Only user-space CPU was sampled (cpu-clock:u): time in the kernel is in the /proc CPU of the process but not in these samples.")
	}
	if res.Lost > 0 {
		r.Warnings = append(r.Warnings, fmt.Sprintf("perf dropped %d events (its buffer overflowed): the samples undercount the CPU.", res.Lost))
	}
	if res.Early {
		r.Notes = append(r.Notes, "perf stopped before the end of the active phase: the process exited.")
	}
	r.Notes = append(r.Notes, res.Notes...)
	return r, nil
}
