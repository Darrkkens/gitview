package study

import (
	"slices"
	"sort"
	"strings"

	"github.com/athos/gitview/internal/inspect/perf"
	"github.com/athos/gitview/internal/inspect/xtrace"
)

// PerfFunc is the cost a profile attributes to a function.
type PerfFunc struct {
	ID      string  `json:"id,omitempty"` // module symbol, when known
	Name    string  `json:"name"`
	File    string  `json:"file,omitempty"`
	Line    int     `json:"line,omitempty"`
	Found   bool    `json:"found"` // the profile has samples in it
	Flat    int64   `json:"flat"`
	Cum     int64   `json:"cum"`
	FlatPct float64 `json:"flatPct"`
	CumPct  float64 `json:"cumPct"`
}

// ProfileStudy is what one loaded profile observed.
type ProfileStudy struct {
	Source     string     `json:"source"`
	Kind       string     `json:"kind"`
	SampleType string     `json:"sampleType"`
	Unit       string     `json:"unit"`
	Total      int64      `json:"total"`
	Samples    int        `json:"samples"`
	DurationMs float64    `json:"durationMs,omitempty"`
	Meaning    string     `json:"meaning"`
	Top        []PerfFunc `json:"top"`    // highest flat cost, any function
	Report     []PerfFunc `json:"report"` // the functions of the reading order
	Evidence   Evidence   `json:"evidence"`
}

// RegionStat summarises the trace regions of one type.
type RegionStat struct {
	Type         string `json:"type"`
	Symbol       string `json:"symbol,omitempty"`
	Instrumented bool   `json:"instrumented,omitempty"`
	Count        int    `json:"count"`
	Unfinished   int    `json:"unfinished,omitempty"`
	TotalNs      int64  `json:"totalNs"`
	MinNs        int64  `json:"minNs"`
	MaxNs        int64  `json:"maxNs"`
}

// PerfObserved holds measurements only (RUNTIME).
type PerfObserved struct {
	CPU        *ProfileStudy    `json:"cpu,omitempty"`
	Mem        *ProfileStudy    `json:"mem,omitempty"`
	Benchmarks []perf.BenchStat `json:"benchmarks,omitempty"`
	BenchFile  string           `json:"benchFile,omitempty"`
	Regions    []RegionStat     `json:"regions,omitempty"`
	TraceFile  string           `json:"traceFile,omitempty"`
	Missing    []string         `json:"missing,omitempty"`
}

// StaticPerf is what the code and the compiler say about a function
// (STATIC), with no measurement.
type StaticPerf struct {
	Func       string   `json:"func"`
	FuncName   string   `json:"funcName"`
	File       string   `json:"file"`
	Line       int      `json:"line"`
	Loops      int      `json:"loops"`
	Cyclomatic int      `json:"cyclomatic"`
	DBOps      int      `json:"dbOps"`             // database operations it performs directly
	DBReach    int      `json:"dbReach"`           // ... and through the functions it calls
	Escapes    []string `json:"escapes,omitempty"` // "line: message" of escapes to heap / moved to heap
	Inline     string   `json:"inline,omitempty"`  // the compiler's inlining decision
	EscapeRun  bool     `json:"escapeRun"`
}

// PerfStudy keeps observed and static facts apart.
type PerfStudy struct {
	Observed PerfObserved `json:"observed"`
	Static   []StaticPerf `json:"static,omitempty"`
	Notes    []string     `json:"notes,omitempty"`
}

const (
	maxPerfTop     = 10
	maxRegionTypes = 30
	maxEscapes     = 12
)

func perfStudy(in Inputs, x *graph, order []string) PerfStudy {
	var ps PerfStudy
	bundles := orderBundles(in.Bundles, order)
	profile := func(r *perf.Report, mem bool) *ProfileStudy {
		p := &ProfileStudy{Source: r.Artifact.Name, Kind: r.Kind, SampleType: r.SampleType, Unit: r.Unit, Total: r.Total, Samples: r.Samples, DurationMs: r.DurationMs, Meaning: r.Meaning,
			Evidence: Evidence{Basis: Runtime, Text: "profile " + r.Artifact.Name + " (" + r.SampleType + ", sampled)"}}
		funcs := append([]perf.FuncStat(nil), r.Funcs...)
		sort.SliceStable(funcs, func(i, j int) bool { return funcs[i].Flat > funcs[j].Flat })
		for _, f := range funcs {
			if len(p.Top) >= maxPerfTop || f.Flat <= 0 {
				break
			}
			p.Top = append(p.Top, PerfFunc{ID: f.Symbol, Name: f.Name, File: f.File, Line: f.Line, Found: true, Flat: f.Flat, Cum: f.Cum, FlatPct: f.FlatPct, CumPct: f.CumPct})
		}
		for _, b := range bundles {
			var sp *perf.SymbolPerf
			if b.Perf != nil {
				sp = b.Perf.CPU
				if mem {
					sp = b.Perf.Mem
				}
			}
			if sp == nil {
				s := r.ForSymbol(b.Symbol.ID)
				sp = &s
			}
			p.Report = append(p.Report, PerfFunc{ID: b.Symbol.ID, Name: b.Symbol.Qualified, File: b.Symbol.File, Line: b.Symbol.Line, Found: sp.Found, Flat: sp.Flat, Cum: sp.Cum, FlatPct: sp.FlatPct, CumPct: sp.CumPct})
		}
		return p
	}
	o := &ps.Observed
	if in.CPU != nil {
		o.CPU = profile(in.CPU, false)
	} else {
		o.Missing = append(o.Missing, "No CPU profile: gitview study --cpu cpu.out (from go test -cpuprofile, or a capture of the Runtime Inspector).")
	}
	if in.Mem != nil {
		o.Mem = profile(in.Mem, true)
	} else {
		o.Missing = append(o.Missing, "No memory profile: gitview study --memory mem.out (from go test -memprofile).")
	}
	if n := len(in.Benches); n > 0 {
		run := in.Benches[n-1]
		o.Benchmarks, o.BenchFile = run.Stats(), run.Artifact.Name
	} else {
		o.Missing = append(o.Missing, "No benchmark results: gitview study --bench-file bench.txt (saved output of go test -bench).")
	}
	if in.Trace != nil {
		o.Regions, o.TraceFile = regionStats(in.Trace), in.Trace.Artifact.Name
		if len(o.Regions) == 0 {
			o.Missing = append(o.Missing, "The execution trace has no regions: durations per function exist only for code measured with regions (gitview inspect trace --instrument).")
		}
	} else {
		o.Missing = append(o.Missing, "No execution trace: gitview study --trace trace.out.")
	}

	for _, b := range bundles {
		sp := StaticPerf{Func: b.Symbol.ID, FuncName: b.Symbol.Qualified, File: b.Symbol.File, Line: b.Symbol.Line}
		if n := x.nodes[b.Symbol.ID]; n != nil {
			sp.FuncName = n.Name
		}
		if g := b.Branches; g != nil {
			sp.Loops, sp.Cyclomatic = g.Metrics.Loops, g.Metrics.Cyclomatic
		}
		if b.SQL != nil {
			for _, op := range b.SQL.Ops {
				if op.Func.ID == b.Symbol.ID {
					sp.DBOps++
				}
				sp.DBReach++
			}
		}
		if b.Perf != nil && b.Perf.EscapeRun {
			sp.EscapeRun = true
			for _, d := range b.Perf.Escape {
				switch d.Kind {
				case perf.DiagEscapes, perf.DiagMoved:
					// -m=2 repeats a diagnostic with " in <func>" before
					// its explanation.
					e := itoa(d.Line) + ": " + trimIn(d.Message)
					if len(sp.Escapes) < maxEscapes && !slices.Contains(sp.Escapes, e) {
						sp.Escapes = append(sp.Escapes, e)
					}
				case perf.DiagCanInline, perf.DiagCannot:
					if sp.Inline == "" {
						sp.Inline = d.Message
						if i := strings.Index(sp.Inline, " as: "); i > 0 {
							sp.Inline = sp.Inline[:i] // drop the inlined body
						}
					}
				}
			}
		}
		ps.Static = append(ps.Static, sp)
	}
	if in.Escape == nil {
		ps.Notes = append(ps.Notes, "Compiler diagnostics (escape analysis, inlining) not loaded: gitview study --escape-log build.log (saved output of go build -gcflags=-m=2).")
	}
	return ps
}

func trimIn(msg string) string {
	if i := strings.LastIndex(msg, " in "); i > 0 && !strings.Contains(msg[i+4:], " ") {
		return msg[:i]
	}
	return msg
}

func regionStats(t *xtrace.Timeline) []RegionStat {
	by := map[string]*RegionStat{}
	var keys []string
	for _, r := range t.Regions {
		s := by[r.Type]
		if s == nil {
			s = &RegionStat{Type: r.Type, Symbol: r.Symbol, Instrumented: r.Instrumented, MinNs: -1}
			by[r.Type] = s
			keys = append(keys, r.Type)
		}
		if r.End < 0 {
			s.Unfinished++
			continue
		}
		d := r.End - r.Start
		s.Count++
		s.TotalNs += d
		if s.MinNs < 0 || d < s.MinNs {
			s.MinNs = d
		}
		s.MaxNs = max(s.MaxNs, d)
	}
	out := make([]RegionStat, 0, len(keys))
	for _, k := range keys {
		s := by[k]
		if s.MinNs < 0 {
			s.MinNs = 0
		}
		out = append(out, *s)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].TotalNs != out[j].TotalNs {
			return out[i].TotalNs > out[j].TotalNs
		}
		return out[i].Type < out[j].Type
	})
	if len(out) > maxRegionTypes {
		out = out[:maxRegionTypes]
	}
	return out
}
