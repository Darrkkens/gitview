// Package report is the Report Engine of the Runtime Performance
// Inspector: it turns a stored capture into an analysis — process
// behaviour per phase, requests per route, SQL, profiles, runtime metrics —
// and compares two captures.
//
// Every number says how it was obtained: measured (read from the kernel or
// the Go runtime), sampled (statistical profiles), estimated (derived with
// a stated assumption) or unavailable (with the reason). Process-wide
// numbers are never presented as a request's.
package report

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/inspect/perf"
	"github.com/athos/gitview/internal/inspect/xtrace"
	"github.com/athos/gitview/internal/live/capture"
	"github.com/athos/gitview/internal/live/goprof"
	"github.com/athos/gitview/internal/live/httpcol"
	"github.com/athos/gitview/internal/live/linuxperf"
	"github.com/athos/gitview/internal/live/proc"
	"github.com/athos/gitview/internal/live/srcmap"
	"github.com/athos/gitview/internal/live/tracean"
)

// Bases of a number.
const (
	Measured    = "measured"
	Sampled     = "sampled"
	Estimated   = "estimated"
	Unavailable = "unavailable"
)

// Scopes of a number.
const (
	ScopeProcess = "process"
	ScopeRequest = "request"
)

// Metric is one number of the report.
type Metric struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"` // % | bytes | ns | count | bytes/s
	Basis string  `json:"basis"`
	Scope string  `json:"scope"`
	Note  string  `json:"note,omitempty"`
}

// PhaseStats summarises the process samples of one phase.
type PhaseStats struct {
	Name     string        `json:"name"`
	From     time.Duration `json:"from"`
	To       time.Duration `json:"to"`
	Samples  int           `json:"samples"`
	HasCPU   bool          `json:"hasCPU"`
	CPUAvg   float64       `json:"cpuAvg"` // % of one core
	CPUMax   float64       `json:"cpuMax"`
	CPUTime  int64         `json:"cpuTime"` // ns
	HasMem   bool          `json:"hasMem"`
	RSSStart uint64        `json:"rssStart"`
	RSSEnd   uint64        `json:"rssEnd"`
	RSSMin   uint64        `json:"rssMin"`
	RSSMax   uint64        `json:"rssMax"`
	Threads  int           `json:"threads"` // maximum
	HasCtx   bool          `json:"hasCtx"`
	VolCtx   uint64        `json:"volCtx"`
	InvolCtx uint64        `json:"involCtx"`
	HasIO    bool          `json:"hasIO"`
	Read     uint64        `json:"read"`  // storage
	Write    uint64        `json:"write"` // storage
	RChar    uint64        `json:"rchar"` // read() including sockets
	WChar    uint64        `json:"wchar"`
	HasFDs   bool          `json:"hasFDs"`
	FDs      int           `json:"fds"` // maximum
	HasNet   bool          `json:"hasNet"`
	NetRx    uint64        `json:"netRx"`
	NetTx    uint64        `json:"netTx"`
}

// Point is one sample for the charts.
type Point struct {
	T       float64 `json:"t"` // seconds
	CPU     float64 `json:"cpu"`
	RSS     uint64  `json:"rss"`
	VSZ     uint64  `json:"vsz,omitempty"`
	Threads int     `json:"threads"`
	FDs     int     `json:"fds"`
}

// Process is the process-wide part of the report.
type Process struct {
	Phases        []PhaseStats      `json:"phases"`
	Series        []Point           `json:"series"`
	CPUResolution string            `json:"cpuResolution"`
	Unavailable   map[string]string `json:"unavailable,omitempty"`
}

// Latency summarises request durations.
type Latency struct {
	N    int           `json:"n"`
	Min  time.Duration `json:"min"`
	P50  time.Duration `json:"p50"`
	P90  time.Duration `json:"p90"`
	P99  time.Duration `json:"p99"`
	Max  time.Duration `json:"max"`
	Mean time.Duration `json:"mean"`
}

// StatementStats summarises one SQL statement.
type StatementStats struct {
	Query      string           `json:"query"`
	Count      int              `json:"count"`
	PerRequest float64          `json:"perRequest,omitempty"`
	Total      time.Duration    `json:"total"`
	Mean       time.Duration    `json:"mean"`
	Max        time.Duration    `json:"max"`
	Errors     int              `json:"errors,omitempty"`
	Rows       float64          `json:"rows,omitempty"` // mean, when reported
	Site       string           `json:"site,omitempty"` // observed call site file:line
	Match      *srcmap.SQLMatch `json:"match,omitempty"`
	Sources    []httpcol.Source `json:"sources,omitempty"`
	Unfinished int              `json:"unfinished,omitempty"`
}

// TraceStats averages what the execution trace shows for a route.
type TraceStats struct {
	Requests   int              `json:"requests"`
	RunningNs  int64            `json:"runningNs"`
	RunnableNs int64            `json:"runnableNs"`
	WaitingNs  int64            `json:"waitingNs"`
	SyscallNs  int64            `json:"syscallNs"`
	GCNs       int64            `json:"gcNs"`
	STWNs      int64            `json:"stwNs"`
	Waits      map[string]int64 `json:"waits,omitempty"`
}

// RouteStats is everything about the requests of one endpoint.
type RouteStats struct {
	Key          string             `json:"key"`
	KeyBasis     string             `json:"keyBasis"`
	Count        int                `json:"count"`   // requests in the active phase
	Outside      int                `json:"outside"` // in the baseline, a pause or the cooldown
	Status       map[int]int        `json:"status"`
	Errors       int                `json:"errors"`
	Latency      Latency            `json:"latency"`
	LatencyBasis string             `json:"latencyBasis"`
	Proxy        *Latency           `json:"proxy,omitempty"` // client side, when the server side is known too
	TTFB         *Latency           `json:"ttfb,omitempty"`
	BytesIn      float64            `json:"bytesIn"`
	BytesOut     float64            `json:"bytesOut"`
	MaxInFlight  int64              `json:"maxInFlight"`
	MeanInFlight float64            `json:"meanInFlight"` // 0 when no source measured it
	Rate         float64            `json:"rate"`         // requests per second of active phase
	Isolated     int                `json:"isolated"`     // requests that overlapped no other
	Metrics      []Metric           `json:"metrics"`
	Statements   []StatementStats   `json:"statements,omitempty"`
	SQLShare     float64            `json:"sqlShare,omitempty"` // SQL time / request time
	Route        *srcmap.RouteMatch `json:"route,omitempty"`
	HotPath      []srcmap.PathStep  `json:"hotPath,omitempty"`
	Flame        *perf.FlameNode    `json:"flame,omitempty"`
	TopFuncs     []perf.FuncStat    `json:"topFuncs,omitempty"`
	Trace        *TraceStats        `json:"trace,omitempty"`
	Durations    []float64          `json:"durations"` // ms, active phase, for comparison
	Labelled     bool               `json:"labelled,omitempty"`
	// SQLObserved: every request came through a source that sees SQL (the
	// collector, or OpenTelemetry when the application exports database
	// spans); otherwise statement counts are unknown, not zero.
	SQLObserved bool                   `json:"sqlObserved"`
	Sources     map[httpcol.Source]int `json:"sources"`
}

// RequestRow is one request in the list.
type RequestRow struct {
	ID      string        `json:"id"`
	Key     string        `json:"key"`
	Method  string        `json:"method"`
	Path    string        `json:"path"`
	Status  int           `json:"status"`
	Off     time.Duration `json:"off"`
	Dur     time.Duration `json:"dur"`
	Proxy   time.Duration `json:"proxy,omitempty"`
	Phase   string        `json:"phase"`
	SQL     int           `json:"sql"` // -1: no source of this request can see SQL
	SQLTime time.Duration `json:"sqlTime"`
	CPU     int64         `json:"cpu"` // sampled ns, -1 when unknown
	// PerfSamples counts the Linux perf samples of the whole process taken
	// while the request ran (-1: no perf data); they are not the request's.
	PerfSamples int      `json:"perfSamples"`
	InFlight    int64    `json:"inFlight,omitempty"`
	Sources     []string `json:"sources"`
	Error       string   `json:"error,omitempty"`
	Panic       bool     `json:"panic,omitempty"`
}

// Runtime summarises the runtime/metrics readings of the active phase.
type Runtime struct {
	Metrics []Metric `json:"metrics"`
	Series  []struct {
		T          float64 `json:"t"`
		HeapLive   uint64  `json:"heapLive"`
		Goroutines uint64  `json:"goroutines"`
		GCCycles   uint64  `json:"gcCycles"`
	} `json:"series"`
}

// Analysis is the report of one capture.
type Analysis struct {
	ID          string              `json:"id"`
	Label       string              `json:"label,omitempty"`
	Created     time.Time           `json:"created"`
	Status      string              `json:"status"`
	StatusNote  string              `json:"statusNote,omitempty"`
	Repo        string              `json:"repo,omitempty"`
	ProcessInfo capture.ProcessInfo `json:"processInfo"`
	Settings    capture.Settings    `json:"settings"`
	Phases      capture.Phases      `json:"phases"`
	Sources     []string            `json:"sources"`
	Process     Process             `json:"process"`
	Routes      []RouteStats        `json:"routes"`
	Requests    []RequestRow        `json:"requests"`
	Background  []StatementStats    `json:"background,omitempty"` // SQL outside requests
	Profiles    *goprof.Profiles    `json:"profiles,omitempty"`
	Perf        *linuxperf.Report   `json:"perf,omitempty"`
	// CPUViews are the three CPU figures: process total (measured), CPU
	// sampled by function, CPU attributable to requests.
	CPUViews    []Metric          `json:"cpuViews"`
	Correlation *Correlation      `json:"correlation,omitempty"`
	HotPath     []srcmap.PathStep `json:"hotPath,omitempty"`
	Runtime     *Runtime          `json:"runtime,omitempty"`
	Trace       *tracean.Analysis `json:"trace,omitempty"`
	Stale       []string          `json:"stale,omitempty"`
	Limitations []string          `json:"limitations"`
	Warnings    []string          `json:"warnings,omitempty"`
	Notes       []string          `json:"notes,omitempty"`
}

// Analyze builds the report of a capture. mapper may be nil (no source
// code available).
func Analyze(c *capture.Capture, mapper *srcmap.Mapper) (*Analysis, error) {
	a := &Analysis{ID: c.ID, Label: c.Label, Created: c.Created, Status: c.Status, StatusNote: c.StatusNote, Repo: c.Repo,
		ProcessInfo: c.Process, Settings: c.Settings, Phases: c.Phases, Sources: c.Sources,
		Warnings: append([]string(nil), c.Warnings...), Notes: append([]string(nil), c.Notes...)}
	a.Process = processStats(c)
	var progs = mapperProgs(mapper)

	// Profiles.
	var cpu *perf.Profile
	if c.Profiles != nil {
		prof, err := goprof.Analyze(c.Dir, c.Profiles, goprof.Active{Start: c.Phases.ActiveStart, End: c.Phases.ActiveEnd, Paused: c.Phases.Paused}, progs)
		if err != nil {
			a.Warnings = append(a.Warnings, "profiles: "+err.Error())
		} else {
			a.Profiles = prof
			if prof.CPU != nil {
				a.HotPath = srcmap.HotPath(prof.CPU.Flame)
			}
			a.Notes = append(a.Notes, prof.Notes...)
		}
		cpu, _ = goprof.CPUProfile(c.Dir, c.Profiles)
		a.Warnings = append(a.Warnings, c.Profiles.Errors...)
		if c.Profiles.Trace != nil && c.Profiles.Trace.File != "" {
			tl, err := xtrace.Load(filepath.Join(c.Dir, c.Profiles.Trace.File), progs, xtrace.Limits{})
			if err != nil {
				a.Warnings = append(a.Warnings, "trace: "+err.Error())
			} else {
				a.Trace = tracean.Analyze(tl, c.TraceAt)
			}
		}
		a.Runtime = runtimeStats(c)
	}
	perfData := a.analyzePerf(c, progs)
	a.analyzeRequests(c, cpu, mapper)
	a.correlate(c, perfData)
	a.cpuViews(c)
	a.Background = statements(orphanSpans(c), nil, 0, mapper)
	a.limitations(c)
	if mapper != nil {
		var files []string
		for _, r := range a.Routes {
			if r.Route != nil && r.Route.Handler != nil {
				files = append(files, r.Route.Handler.File)
			}
			for _, s := range r.Statements {
				if s.Match != nil {
					files = append(files, s.Match.Func.File)
				}
			}
		}
		if a.Profiles != nil && a.Profiles.CPU != nil {
			for _, f := range a.Profiles.CPU.Funcs {
				if f.Symbol != "" {
					files = append(files, f.File)
				}
			}
		}
		if a.Perf != nil && a.Perf.CPU != nil {
			for _, f := range a.Perf.CPU.Funcs {
				if f.Symbol != "" {
					files = append(files, f.File)
				}
			}
		}
		a.Stale = mapper.StaleFiles(c.Process.Started, files)
		if len(a.Stale) > 0 {
			a.Warnings = append(a.Warnings, fmt.Sprintf("%d source files changed after the process started: their line numbers may not match the running code (%s).", len(a.Stale), strings.Join(first(a.Stale, 5), ", ")))
		}
	}
	return a, nil
}

func first(s []string, n int) []string {
	if len(s) > n {
		return append(s[:n:n], "…")
	}
	return s
}

func mapperProgs(m *srcmap.Mapper) []*flow.Program {
	if m == nil {
		return nil
	}
	return m.Programs()
}

func processStats(c *capture.Capture) Process {
	p := Process{Unavailable: c.Process.Unavailable, CPUResolution: "scheduler statistics (nanoseconds per thread)"}
	exact := true
	for _, s := range c.Samples {
		if s.HasCPU && !s.CPUExact && s.CPUPct >= 0 {
			exact = false
		}
	}
	if !exact {
		p.CPUResolution = "partly from /proc/<pid>/stat ticks (10 ms resolution) when threads exited or schedstat was unreadable"
	}
	ph := c.Phases
	end := ph.End
	if len(c.Samples) > 0 && c.Samples[len(c.Samples)-1].T > end {
		end = c.Samples[len(c.Samples)-1].T
	}
	p.Phases = []PhaseStats{
		phase(c.Samples, "baseline", 0, ph.ActiveStart, nil),
		phase(c.Samples, "active", ph.ActiveStart, ph.ActiveEnd, ph.Paused),
		phase(c.Samples, "cooldown", ph.ActiveEnd, end, nil),
	}
	step := 1
	if len(c.Samples) > 2000 {
		step = (len(c.Samples) + 1999) / 2000
	}
	for i := 0; i < len(c.Samples); i += step {
		s := c.Samples[i]
		cpuPct := s.CPUPct
		if cpuPct < 0 {
			cpuPct = 0
		}
		p.Series = append(p.Series, Point{T: s.T.Seconds(), CPU: round(cpuPct, 1), RSS: s.RSS, VSZ: s.VSZ, Threads: s.Threads, FDs: s.FDs})
	}
	return p
}

// phase summarises the samples in (from, to]: a sample describes the
// interval that ends with it.
func phase(samples []proc.Sample, name string, from, to time.Duration, paused []goprofWindow) PhaseStats {
	ps := PhaseStats{Name: name, From: from, To: to}
	// Counters are summed over the intervals between consecutive samples,
	// skipping the intervals that overlap a pause.
	inPause := func(a, b time.Duration) bool {
		for _, w := range paused {
			if a < w.End && b > w.Start {
				return true
			}
		}
		return false
	}
	var prev *proc.Sample
	var firstS, lastS *proc.Sample
	var cpuSum float64
	var cpuN int
	ps.HasCPU, ps.HasCtx, ps.HasIO, ps.HasNet = true, true, true, true
	intervals := 0
	for i := range samples {
		s := &samples[i]
		if s.T <= from {
			prev = s
			continue
		}
		if s.T > to {
			break
		}
		if prev == nil {
			prev = s
		}
		paused := inPause(prev.T, s.T)
		if firstS == nil && !paused {
			firstS = s
		}
		if !paused {
			lastS = s
			ps.Samples++
			if s.HasCPU && s.CPUPct >= 0 {
				cpuSum += s.CPUPct
				cpuN++
				ps.CPUMax = math.Max(ps.CPUMax, s.CPUPct)
			}
			if s.HasMem {
				if !ps.HasMem || s.RSS < ps.RSSMin {
					ps.RSSMin = s.RSS
				}
				ps.RSSMax = max(ps.RSSMax, s.RSS)
				ps.HasMem = true
			}
			ps.Threads = max(ps.Threads, s.Threads)
			if s.HasFDs {
				ps.HasFDs, ps.FDs = true, max(ps.FDs, s.FDs)
			}
			if s != prev {
				intervals++
				ps.HasCPU = ps.HasCPU && s.HasCPU && prev.HasCPU
				if s.CPUNanos >= prev.CPUNanos {
					ps.CPUTime += int64(s.CPUNanos - prev.CPUNanos)
				}
				ps.HasCtx = ps.HasCtx && s.HasCtx && prev.HasCtx
				ps.VolCtx += sub(s.VolCtx, prev.VolCtx)
				ps.InvolCtx += sub(s.InvolCtx, prev.InvolCtx)
				ps.HasIO = ps.HasIO && s.HasIO && prev.HasIO
				ps.Read += sub(s.ReadBytes, prev.ReadBytes)
				ps.Write += sub(s.WriteBytes, prev.WriteBytes)
				ps.RChar += sub(s.RChar, prev.RChar)
				ps.WChar += sub(s.WChar, prev.WChar)
				ps.HasNet = ps.HasNet && s.HasNet && prev.HasNet
				ps.NetRx += sub(s.NetRx, prev.NetRx)
				ps.NetTx += sub(s.NetTx, prev.NetTx)
			}
		}
		prev = s
	}
	if intervals == 0 {
		ps.HasCPU, ps.HasCtx, ps.HasIO, ps.HasNet = false, false, false, false
		ps.CPUTime = 0
	}
	if firstS == nil {
		ps.HasCPU = false
		return ps
	}
	ps.HasCPU = ps.HasCPU && cpuN > 0
	if cpuN > 0 {
		ps.CPUAvg = round(cpuSum/float64(cpuN), 1)
	}
	if !ps.HasCPU {
		ps.CPUTime = 0
	}
	if firstS.HasMem && lastS.HasMem {
		ps.RSSStart, ps.RSSEnd = firstS.RSS, lastS.RSS
	}
	return ps
}

func sub(a, b uint64) uint64 {
	if a < b {
		return 0
	}
	return a - b
}

func round(v float64, digits int) float64 {
	p := math.Pow(10, float64(digits))
	return math.Round(v*p) / p
}

// percentile uses the nearest-rank method on sorted values.
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	k := int(math.Ceil(p/100*float64(len(sorted)))) - 1
	return sorted[min(max(k, 0), len(sorted)-1)]
}

func latency(ds []time.Duration) Latency {
	if len(ds) == 0 {
		return Latency{}
	}
	s := append([]time.Duration(nil), ds...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	var sum time.Duration
	for _, d := range s {
		sum += d
	}
	return Latency{N: len(s), Min: s[0], P50: percentile(s, 50), P90: percentile(s, 90), P99: percentile(s, 99), Max: s[len(s)-1], Mean: sum / time.Duration(len(s))}
}

func phaseOf(ph capture.Phases, t time.Duration) string {
	switch {
	case t < ph.ActiveStart:
		return "baseline"
	case t > ph.ActiveEnd:
		return "cooldown"
	case !ph.InActive(t):
		return "paused"
	}
	return "active"
}

// cpuAt interpolates the process CPU counter at t.
func cpuAt(samples []proc.Sample, t time.Duration) (float64, bool) {
	i := sort.Search(len(samples), func(i int) bool { return samples[i].T >= t })
	if i == 0 || i == len(samples) {
		return 0, false
	}
	a, b := samples[i-1], samples[i]
	if !a.HasCPU || !b.HasCPU || b.T == a.T {
		return 0, false
	}
	f := float64(t-a.T) / float64(b.T-a.T)
	return float64(a.CPUNanos) + f*float64(b.CPUNanos-a.CPUNanos), true
}

func (a *Analysis) analyzeRequests(c *capture.Capture, cpu *perf.Profile, mapper *srcmap.Mapper) {
	ph := c.Phases
	var cpuByTrace map[string]int64
	if a.Profiles != nil {
		cpuByTrace = a.Profiles.CPUByTrace
	}
	traceByID := map[string]*tracean.RequestTrace{}
	if a.Trace != nil {
		traceByID = a.Trace.ByID()
	}
	// Overlaps: a request is isolated when no other request was in flight
	// during any part of it.
	type span struct{ s, e time.Duration }
	spans := make([]span, len(c.Requests))
	for i, r := range c.Requests {
		spans[i] = span{r.Off, r.End()}
	}
	isolated := func(i int) bool {
		for j, o := range spans {
			if j != i && o.s < spans[i].e && spans[i].s < o.e {
				return false
			}
		}
		return true
	}
	baseline := a.Process.Phases[0]
	byKey := map[string]*RouteStats{}
	var order []string
	reqsByKey := map[string][]int{}
	for i, r := range c.Requests {
		phase := phaseOf(ph, r.Off)
		row := RequestRow{ID: r.ID, Key: r.Key, Method: r.Method, Path: r.Path, Status: r.Status, Off: r.Off, Dur: r.Dur(), Phase: phase,
			SQL: len(r.SQL), CPU: -1, InFlight: r.InFlight, Error: r.Error, Panic: r.Panic}
		if r.HasProxy && r.HasServer {
			row.Proxy = r.ProxyDur
		}
		for _, s := range r.Sources {
			row.Sources = append(row.Sources, string(s))
		}
		for _, s := range r.SQL {
			row.SQLTime += s.Dur
		}
		// Sampled CPU of a request is known only when the request lay
		// entirely inside a CPU profile window; otherwise it is unknown,
		// not zero.
		if r.Labelled && cpu != nil && c.Profiles.Covered(r.Off, r.End()) {
			row.CPU = cpuByTrace[r.ID]
		}
		if !sqlCapable(r) {
			row.SQL = -1
		}
		if len(a.Requests) < 5000 {
			a.Requests = append(a.Requests, row)
		}
		rs := byKey[r.Key]
		if rs == nil {
			rs = &RouteStats{Key: r.Key, KeyBasis: r.KeyBasis, Status: map[int]int{}, Sources: map[httpcol.Source]int{}}
			byKey[r.Key] = rs
			order = append(order, r.Key)
		}
		if phase != "active" {
			rs.Outside++
			continue
		}
		reqsByKey[r.Key] = append(reqsByKey[r.Key], i)
	}
	for _, key := range order {
		rs := byKey[key]
		idx := reqsByKey[key]
		rs.Count = len(idx)
		var server, proxy, ttfb []time.Duration
		var sqlTime, reqTime time.Duration
		var cpuTotal int64
		cpuKnown, labelled := 0, 0
		var attrib []float64
		attribSkipped := 0
		sqlBlind := 0
		var spansOf []httpcol.SQLSpan
		var tr TraceStats
		ids := map[string]bool{}
		for _, i := range idx {
			r := c.Requests[i]
			ids[r.ID] = true
			for _, s := range r.Sources {
				rs.Sources[s]++
			}
			rs.Status[r.Status]++
			if r.Status >= 500 || r.Error != "" || r.Panic {
				rs.Errors++
			}
			if r.HasServer {
				server = append(server, r.ServerDur)
			}
			if r.HasProxy {
				proxy = append(proxy, r.ProxyDur)
				if r.HasTTFB {
					ttfb = append(ttfb, r.TTFB)
				}
			}
			rs.Durations = append(rs.Durations, float64(r.Dur())/1e6)
			rs.BytesIn += float64(r.BytesIn)
			rs.BytesOut += float64(r.BytesOut)
			rs.MaxInFlight = max(rs.MaxInFlight, r.InFlight)
			reqTime += r.Dur()
			for _, s := range r.SQL {
				sqlTime += s.Dur
			}
			spansOf = append(spansOf, r.SQL...)
			if r.Labelled {
				labelled++
				if cpu != nil && c.Profiles.Covered(r.Off, r.End()) {
					cpuTotal += cpuByTrace[r.ID]
					cpuKnown++
				}
			}
			if !sqlCapable(r) {
				sqlBlind++
			}
			iso := isolated(i)
			if iso {
				rs.Isolated++
			}
			// Process CPU in the request's window, minus the baseline rate.
			if iso && r.Dur() >= 2*c.Settings.Interval && baseline.HasCPU {
				s0, ok0 := cpuAt(c.Samples, r.Off)
				s1, ok1 := cpuAt(c.Samples, r.End())
				if ok0 && ok1 {
					v := (s1 - s0) - baseline.CPUAvg/100*float64(r.Dur())
					attrib = append(attrib, math.Max(v, 0))
				}
			} else {
				attribSkipped++
			}
			if t := traceByID[r.ID]; t != nil {
				tr.Requests++
				tr.RunningNs += t.RunningNs
				tr.RunnableNs += t.RunnableNs
				tr.WaitingNs += t.WaitingNs
				tr.SyscallNs += t.SyscallNs
				tr.GCNs += t.GCNs
				tr.STWNs += t.STWNs
				for k, v := range t.Waits {
					if tr.Waits == nil {
						tr.Waits = map[string]int64{}
					}
					tr.Waits[k] += v
				}
			}
		}
		if rs.Count > 0 {
			rs.BytesIn /= float64(rs.Count)
			rs.BytesOut /= float64(rs.Count)
			var inflight int64
			known := 0
			for _, i := range idx {
				if v := c.Requests[i].InFlight; v > 0 {
					inflight += v
					known++
				}
			}
			if known == len(idx) {
				rs.MeanInFlight = round(float64(inflight)/float64(known), 2)
			} else {
				rs.MaxInFlight = 0 // not measured for every request: unknown
			}
			if act := c.Phases.Active(); act > 0 {
				rs.Rate = round(float64(rs.Count)/act.Seconds(), 2)
			}
		}
		if len(server) == len(idx) && len(server) > 0 {
			rs.Latency, rs.LatencyBasis = latency(server), "server side (time in the handler, measured by the "+serverSource(rs.Sources)+")"
			if len(proxy) > 0 {
				l := latency(proxy)
				rs.Proxy = &l
			}
		} else {
			all := make([]time.Duration, 0, len(idx))
			for _, i := range idx {
				all = append(all, c.Requests[i].Dur())
			}
			rs.Latency, rs.LatencyBasis = latency(all), "client side (measured at GitView's proxy: includes the local hop and the client reading the response)"
			if len(server) > 0 {
				rs.LatencyBasis = "mixed: server side where the collector saw the request, proxy otherwise"
			}
		}
		if len(ttfb) > 0 {
			l := latency(ttfb)
			rs.TTFB = &l
		}
		if reqTime > 0 && len(spansOf) > 0 {
			rs.SQLShare = round(float64(sqlTime)/float64(reqTime), 3)
		}
		rs.Statements = statements(spansOf, nil, rs.Count, mapper)
		rs.Labelled = labelled > 0
		// Request-attributable numbers.
		switch {
		case cpuKnown > 0:
			n := cpuKnown
			period := int64(10 * time.Millisecond)
			if cpu != nil && cpu.Period > 0 {
				period = cpu.Period
			}
			note := fmt.Sprintf("CPU profile samples labelled with the request's ID (each sample stands for about %s of CPU), averaged over the %d labelled requests that lay entirely inside a CPU profile window", time.Duration(period), n)
			if n < labelled {
				note += fmt.Sprintf(" (%d others fell between windows or after the profile stopped: unknown)", labelled-n)
			}
			switch samples := cpuTotal / period; {
			case samples == 0:
				note += "; no sample fell in these requests: their CPU time is below the sampling resolution"
			case samples < 30:
				note += fmt.Sprintf("; only %d samples: low precision (send more or longer requests)", samples)
			}
			rs.Metrics = append(rs.Metrics, Metric{Name: "CPU per request", Value: float64(cpuTotal) / float64(n), Unit: "ns", Basis: Sampled, Scope: ScopeRequest, Note: note})
		case labelled > 0 && cpu != nil:
			rs.Metrics = append(rs.Metrics, Metric{Name: "CPU per request", Basis: Unavailable, Scope: ScopeRequest,
				Note: "no labelled request lay entirely inside a CPU profile window"})
		case cpu != nil:
			rs.Metrics = append(rs.Metrics, Metric{Name: "CPU per request", Basis: Unavailable, Scope: ScopeRequest,
				Note: "the CPU profile is process-wide; attributing samples to requests needs GitView's collector (pprof labels)"})
		}
		rs.SQLObserved = sqlBlind == 0
		if len(attrib) > 0 {
			sum := 0.0
			for _, v := range attrib {
				sum += v
			}
			rs.Metrics = append(rs.Metrics, Metric{Name: "Process CPU during an isolated request, above baseline", Value: sum / float64(len(attrib)), Unit: "ns", Basis: Estimated, Scope: ScopeRequest,
				Note: fmt.Sprintf("process CPU counter interpolated over %d requests that overlapped no other request, minus the baseline CPU rate; assumes background work stayed at its baseline level", len(attrib))})
		} else if rs.Count > 0 {
			reason := "no request lasted two sampling intervals (" + c.Settings.Interval.String() + ") without overlapping another"
			if !baseline.HasCPU {
				reason = "no baseline CPU measurement"
			}
			rs.Metrics = append(rs.Metrics, Metric{Name: "Process CPU during an isolated request, above baseline", Basis: Unavailable, Scope: ScopeRequest, Note: reason})
		}
		if tr.Requests > 0 {
			d := int64(tr.Requests)
			tr.RunningNs, tr.RunnableNs, tr.WaitingNs, tr.SyscallNs, tr.GCNs, tr.STWNs = tr.RunningNs/d, tr.RunnableNs/d, tr.WaitingNs/d, tr.SyscallNs/d, tr.GCNs/d, tr.STWNs/d
			for k := range tr.Waits {
				tr.Waits[k] /= d
			}
			rs.Trace = &tr
		}
		if mapper != nil {
			method, _, _ := strings.Cut(rs.Key, " ")
			route, path := "", ""
			for _, i := range idx {
				route, path = c.Requests[i].Route, c.Requests[i].Path
				break
			}
			if rs.Count == 0 {
				for _, r := range c.Requests {
					if r.Key == rs.Key {
						route, path = r.Route, r.Path
						break
					}
				}
			}
			if m, ok := mapper.Route(method, route, path); ok {
				rs.Route = &m
			}
		}
		if cpu != nil && labelled > 0 {
			if r, err := goprof.ForTraces(cpu, ids, mapperProgs(mapper)); err == nil && r.Total > 0 {
				rs.Flame = r.Flame
				rs.HotPath = srcmap.HotPath(r.Flame)
				rs.TopFuncs = topFuncs(r.Funcs, 10)
			}
		}
		a.Routes = append(a.Routes, *rs)
	}
	sort.SliceStable(a.Routes, func(i, j int) bool { return a.Routes[i].Count > a.Routes[j].Count })
}

// sqlCapable reports whether a source of the request can observe SQL.
func sqlCapable(r httpcol.Request) bool {
	for _, s := range r.Sources {
		if s == httpcol.SourceCollector || s == httpcol.SourceOTel {
			return true
		}
	}
	return false
}

func serverSource(src map[httpcol.Source]int) string {
	if src[httpcol.SourceCollector] > 0 {
		return "collector"
	}
	return "OpenTelemetry spans"
}

// topFuncs lists module functions first.
func topFuncs(fs []perf.FuncStat, n int) []perf.FuncStat {
	var mod, other []perf.FuncStat
	for _, f := range fs {
		if f.Symbol != "" {
			mod = append(mod, f)
		} else {
			other = append(other, f)
		}
	}
	sort.SliceStable(mod, func(i, j int) bool { return mod[i].Cum > mod[j].Cum })
	out := append(mod, other...)
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func orphanSpans(c *capture.Capture) []httpcol.SQLSpan {
	var out []httpcol.SQLSpan
	for _, o := range c.Orphans {
		if c.Phases.InActive(o.Start.Sub(c.Created)) {
			out = append(out, o.SQLSpan)
		}
	}
	return out
}

// statements groups SQL spans by statement.
func statements(spans []httpcol.SQLSpan, _ map[string]bool, requests int, mapper *srcmap.Mapper) []StatementStats {
	type key struct{ q, site string }
	byKey := map[key]*StatementStats{}
	var order []key
	rows := map[key][2]float64{}
	for _, s := range spans {
		site := ""
		if s.File != "" && s.Line > 0 {
			site = fmt.Sprintf("%s:%d", s.File, s.Line)
		}
		k := key{s.Query, site}
		st := byKey[k]
		if st == nil {
			st = &StatementStats{Query: s.Query, Site: displaySite(s.File, s.Line)}
			byKey[k] = st
			order = append(order, k)
			if mapper != nil {
				if m, ok := mapper.SQL(s.Query, s.File, s.Line, s.Func); ok {
					st.Match = &m
				}
			}
		}
		st.Count++
		st.Total += s.Dur
		st.Max = max(st.Max, s.Dur)
		if s.Err != "" {
			st.Errors++
		}
		if s.Unfinished {
			st.Unfinished++
		}
		if s.HasRows {
			r := rows[k]
			rows[k] = [2]float64{r[0] + float64(s.Rows), r[1] + 1}
		}
		if !hasSrc(st.Sources, s.Source) {
			st.Sources = append(st.Sources, s.Source)
		}
	}
	out := make([]StatementStats, 0, len(order))
	for _, k := range order {
		st := byKey[k]
		st.Mean = st.Total / time.Duration(st.Count)
		if requests > 0 {
			st.PerRequest = round(float64(st.Count)/float64(requests), 2)
		}
		if r := rows[k]; r[1] > 0 {
			st.Rows = round(r[0]/r[1], 1)
		}
		out = append(out, *st)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Total > out[j].Total })
	return out
}

func hasSrc(l []httpcol.Source, s httpcol.Source) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func displaySite(file string, line int) string {
	if file == "" || line == 0 {
		return ""
	}
	return fmt.Sprintf("%s:%d", displayPath(file), line)
}

func displayPath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(p, home+string(os.PathSeparator)) {
		return "~" + p[len(home):]
	}
	return p
}

// runtimeStats summarises the collector's runtime/metrics readings.
func runtimeStats(c *capture.Capture) *Runtime {
	pts := c.Profiles.Runtime
	if len(pts) < 2 {
		return nil
	}
	// The readings closest to the active phase's bounds.
	var a, b *goprof.RuntimePoint
	for i := range pts {
		p := &pts[i]
		if p.T <= c.Phases.ActiveStart || a == nil {
			a = p
		}
		if b == nil || p.T <= c.Phases.ActiveEnd+c.Profiles.Plan.RuntimeEvery || b.T < c.Phases.ActiveEnd {
			b = p
		}
	}
	if a == nil || b == nil || b.T <= a.T {
		return nil
	}
	rt := &Runtime{}
	win := (b.T - a.T).Seconds()
	u := func(name string) (uint64, bool) {
		x, ok1 := a.Snap.Uint[name]
		y, ok2 := b.Snap.Uint[name]
		if !ok1 || !ok2 || y < x {
			return 0, false
		}
		return y - x, true
	}
	f := func(name string) (float64, bool) {
		x, ok1 := a.Snap.Float[name]
		y, ok2 := b.Snap.Float[name]
		return y - x, ok1 && ok2
	}
	window := fmt.Sprintf("between the runtime readings at %.1f s and %.1f s (the active phase is %.1f–%.1f s)", a.T.Seconds(), b.T.Seconds(), c.Phases.ActiveStart.Seconds(), c.Phases.ActiveEnd.Seconds())
	if len(c.Phases.Paused) > 0 {
		window += ", pauses included"
	}
	if v, ok := u("/gc/cycles/total:gc-cycles"); ok {
		rt.Metrics = append(rt.Metrics, Metric{Name: "GC cycles", Value: float64(v), Unit: "count", Basis: Measured, Scope: ScopeProcess, Note: window})
	}
	if v, ok := u("/gc/heap/allocs:bytes"); ok {
		rt.Metrics = append(rt.Metrics, Metric{Name: "Heap allocation rate", Value: float64(v) / win, Unit: "bytes/s", Basis: Measured, Scope: ScopeProcess, Note: "runtime counter, " + window})
		n := 0
		for _, r := range c.Requests {
			if c.Phases.InActive(r.Off) {
				n++
			}
		}
		if n > 0 {
			rt.Metrics = append(rt.Metrics, Metric{Name: "Heap allocated per request", Value: float64(v) / float64(n), Unit: "bytes", Basis: Estimated, Scope: ScopeRequest,
				Note: fmt.Sprintf("process total %s divided by the %d requests of the active phase: includes background work", window, n)})
		}
	}
	if v, ok := f("/sync/mutex/wait/total:seconds"); ok {
		rt.Metrics = append(rt.Metrics, Metric{Name: "Time goroutines waited on sync.Mutex/RWMutex", Value: v * 1e9, Unit: "ns", Basis: Estimated, Scope: ScopeProcess, Note: "runtime approximation, " + window})
	}
	gc, ok1 := f("/cpu/classes/gc/total:cpu-seconds")
	tot, ok2 := f("/cpu/classes/total:cpu-seconds")
	if ok1 && ok2 && tot > 0 {
		rt.Metrics = append(rt.Metrics, Metric{Name: "GC share of the CPU available to Go", Value: 100 * gc / tot, Unit: "%", Basis: Estimated, Scope: ScopeProcess, Note: "runtime/metrics CPU classes are estimates (the runtime documents them as such)"})
	}
	pauseName := "/sched/pauses/total/gc:seconds"
	if _, ok := b.Snap.Histograms[pauseName]; !ok {
		pauseName = "/gc/pauses:seconds"
	}
	if n, total, p99, ok := histDelta(a.Snap.Histograms[pauseName], b.Snap.Histograms[pauseName]); ok && n > 0 {
		rt.Metrics = append(rt.Metrics,
			Metric{Name: "GC stop-the-world pauses", Value: float64(n), Unit: "count", Basis: Measured, Scope: ScopeProcess, Note: window},
			Metric{Name: "GC pause time (total)", Value: total * 1e9, Unit: "ns", Basis: Estimated, Scope: ScopeProcess, Note: "from the runtime's pause histogram (bucket midpoints)"},
			Metric{Name: "GC pause p99", Value: p99 * 1e9, Unit: "ns", Basis: Estimated, Scope: ScopeProcess, Note: "upper bound of the histogram bucket"})
	}
	if n, _, p99, ok := histDelta(a.Snap.Histograms["/sched/latencies:seconds"], b.Snap.Histograms["/sched/latencies:seconds"]); ok && n > 0 {
		rt.Metrics = append(rt.Metrics, Metric{Name: "Scheduling latency p99", Value: p99 * 1e9, Unit: "ns", Basis: Estimated, Scope: ScopeProcess, Note: "time goroutines waited to run once ready; upper bound of the histogram bucket"})
	}
	for _, p := range pts {
		rt.Series = append(rt.Series, struct {
			T          float64 `json:"t"`
			HeapLive   uint64  `json:"heapLive"`
			Goroutines uint64  `json:"goroutines"`
			GCCycles   uint64  `json:"gcCycles"`
		}{p.T.Seconds(), p.Snap.Uint["/gc/heap/live:bytes"], p.Snap.Uint["/sched/goroutines:goroutines"], p.Snap.Uint["/gc/cycles/total:gc-cycles"]})
	}
	return rt
}

// histDelta returns the count, approximate sum and approximate p99 of the
// observations added between two readings of a histogram.
func histDelta(a, b httpcol.Histogram) (n uint64, sum, p99 float64, ok bool) {
	if len(b.Counts) == 0 || len(a.Counts) != len(b.Counts) || len(b.Buckets) != len(b.Counts)+1 {
		return 0, 0, 0, false
	}
	d := make([]uint64, len(b.Counts))
	for i := range d {
		if b.Counts[i] >= a.Counts[i] {
			d[i] = b.Counts[i] - a.Counts[i]
		}
		n += d[i]
	}
	if n == 0 {
		return 0, 0, 0, true
	}
	var acc uint64
	target := uint64(math.Ceil(0.99 * float64(n)))
	p99set := false
	for i, c := range d {
		lo, hi := b.Buckets[i], b.Buckets[i+1]
		if lo < -1e300 {
			lo = 0
		}
		if hi > 1e300 {
			hi = lo
		}
		sum += float64(c) * (lo + hi) / 2
		acc += c
		if !p99set && acc >= target {
			p99, p99set = hi, true
		}
	}
	return n, sum, p99, true
}

// limitations lists what this capture could not measure, and why.
func (a *Analysis) limitations(c *capture.Capture) {
	l := &a.Limitations
	add := func(s string) { *l = append(*l, s) }
	if r, ok := c.Process.Unavailable[proc.MetricNet]; ok {
		add("Network bytes of the process: " + r + ".")
	}
	for _, m := range []string{proc.MetricIO, proc.MetricFDs, proc.MetricMemory} {
		if r, ok := c.Process.Unavailable[m]; ok {
			add(strings.ToUpper(m[:1]) + m[1:] + ": " + r + ".")
		}
	}
	hasHTTP := false
	for _, s := range c.Sources {
		hasHTTP = hasHTTP || s != "proc"
	}
	if !hasHTTP {
		add("No HTTP source (proxy, collector or OpenTelemetry) was enabled: requests could not be observed, only the process.")
	}
	hasPerf := c.Perf != nil && c.Perf.File != ""
	switch {
	case (c.Profiles == nil || c.Profiles.Plan.Empty()) && hasPerf:
		add("No Go profile was collected (net/http/pprof or GitView's collector): CPU by function comes from Linux perf; Go heap, goroutine and contention profiles and the execution trace need one of those endpoints (perf does not see the Go heap).")
	case c.Profiles == nil || c.Profiles.Plan.Empty():
		add("No profile was collected: CPU, memory, goroutine and contention details by function are not available.")
	}
	if hasPerf {
		add("Linux perf samples the whole process: its samples cannot be split by request, only placed on the timeline next to them.")
	}
	collector := false
	for _, s := range c.Sources {
		collector = collector || s == string(httpcol.SourceCollector)
	}
	if !collector {
		add("Without GitView's collector, CPU samples cannot be attributed to requests and the execution trace cannot be split by request.")
	}
	add("Per-request process metrics (CPU, memory, I/O) do not exist in Linux: process counters cover everything the process does; request-level numbers are labelled as sampled or estimated.")
}

type goprofWindow = goprof.Window
