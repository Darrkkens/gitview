package report

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/athos/gitview/internal/inspect/perf"
)

// Condition is one check of whether two captures can be compared.
type Condition struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	// Blocking conditions make every verdict inconclusive; the others are
	// reported next to the results.
	Blocking bool `json:"blocking,omitempty"`
}

// Delta compares one number between the captures.
type Delta struct {
	Name   string  `json:"name"`
	A      float64 `json:"a"`
	B      float64 `json:"b"`
	Unit   string  `json:"unit"`
	Change Num     `json:"change"` // (B-A)/A in %; NaN (null) when A is 0
	Basis  string  `json:"basis"`
	Scope  string  `json:"scope"`
	Note   string  `json:"note,omitempty"`
}

// Verdicts of a route comparison.
const (
	Slower       = "slower"
	Faster       = "faster"
	NoChange     = "no significant change"
	Inconclusive = "inconclusive"
)

// RouteDelta compares the requests of one endpoint.
type RouteDelta struct {
	Key      string   `json:"key"`
	NA       int      `json:"na"`
	NB       int      `json:"nb"`
	A        Latency  `json:"a"`
	B        Latency  `json:"b"`
	Change   Num      `json:"change"` // median, %
	P        Num      `json:"p"`      // Mann–Whitney two-sided p-value; NaN (null) when not computed
	Verdict  string   `json:"verdict"`
	Reasons  []string `json:"reasons,omitempty"`
	Deltas   []Delta  `json:"deltas,omitempty"`
	SQLAdded []string `json:"sqlAdded,omitempty"` // statements only in B
	SQLGone  []string `json:"sqlGone,omitempty"`  // statements only in A
}

// Comparison compares capture B (usually after a change) with A.
type Comparison struct {
	A          *Analysis    `json:"-"`
	B          *Analysis    `json:"-"`
	AID        string       `json:"a"`
	BID        string       `json:"b"`
	Conditions []Condition  `json:"conditions"`
	Comparable bool         `json:"comparable"`
	Routes     []RouteDelta `json:"routes"`
	OnlyA      []string     `json:"onlyA,omitempty"`
	OnlyB      []string     `json:"onlyB,omitempty"`
	Process    []Delta      `json:"process"`
	Functions  []FuncDelta  `json:"functions,omitempty"`
	// Regressions lists the significant slowdowns.
	Regressions []string `json:"regressions,omitempty"`
	Notes       []string `json:"notes,omitempty"`
}

// FuncDelta compares a function's share of the CPU profile.
type FuncDelta struct {
	Name   string  `json:"name"`
	Symbol string  `json:"symbol,omitempty"`
	A      float64 `json:"a"` // % of samples
	B      float64 `json:"b"`
}

// Minimum requests per route for a verdict.
const minRequests = 5

// Num is a number that may be undefined (NaN), written as null in JSON.
type Num float64

// MarshalJSON writes NaN and infinities as null.
func (n Num) MarshalJSON() ([]byte, error) {
	f := float64(n)
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return []byte("null"), nil
	}
	return []byte(strconv.FormatFloat(f, 'g', -1, 64)), nil
}

// UnmarshalJSON reads null as NaN.
func (n *Num) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*n = Num(math.NaN())
		return nil
	}
	f, err := strconv.ParseFloat(string(b), 64)
	*n = Num(f)
	return err
}

// Defined reports whether n is a number.
func (n Num) Defined() bool { return !math.IsNaN(float64(n)) && !math.IsInf(float64(n), 0) }

func change(a, b float64) Num {
	if a == 0 {
		return Num(math.NaN())
	}
	return Num(100 * (b - a) / a)
}

func activePhase(a *Analysis) PhaseStats {
	for _, p := range a.Process.Phases {
		if p.Name == "active" {
			return p
		}
	}
	return PhaseStats{}
}

func baselinePhase(a *Analysis) PhaseStats { return a.Process.Phases[0] }

// Compare compares two analyses.
func Compare(a, b *Analysis) *Comparison {
	c := &Comparison{A: a, B: b, AID: a.ID, BID: b.ID}
	cond := func(name string, ok bool, blocking bool, detail string) {
		c.Conditions = append(c.Conditions, Condition{Name: name, OK: ok, Blocking: blocking, Detail: detail})
	}
	// Same program.
	sameExe := a.ProcessInfo.Exe == b.ProcessInfo.Exe || a.ProcessInfo.MainModule != "" && a.ProcessInfo.MainModule == b.ProcessInfo.MainModule
	cond("Same program", sameExe, true, fmt.Sprintf("A: %s (%s), B: %s (%s)", a.ProcessInfo.Exe, a.ProcessInfo.MainModule, b.ProcessInfo.Exe, b.ProcessInfo.MainModule))
	sameBuild := a.ProcessInfo.VCSRevision == b.ProcessInfo.VCSRevision && a.ProcessInfo.VCSModified == b.ProcessInfo.VCSModified && a.ProcessInfo.StartTicks == b.ProcessInfo.StartTicks
	buildDetail := "same process instance"
	if !sameBuild {
		buildDetail = "different process instances or builds (expected when comparing before and after a change)"
	}
	cond("Same build", true, false, buildDetail)
	cond("Same Go version", a.ProcessInfo.GoVersion == b.ProcessInfo.GoVersion, false, a.ProcessInfo.GoVersion+" / "+b.ProcessInfo.GoVersion)
	cond("Same machine size", a.ProcessInfo.CPUs == b.ProcessInfo.CPUs, true, fmt.Sprintf("%d / %d CPUs", a.ProcessInfo.CPUs, b.ProcessInfo.CPUs))
	sameInstr := a.Settings.Profile.CPU == b.Settings.Profile.CPU && a.Settings.Profile.Trace == b.Settings.Profile.Trace &&
		a.Settings.Profile.SetRates == b.Settings.Profile.SetRates && (a.Settings.Collector != "") == (b.Settings.Collector != "")
	cond("Same instrumentation", sameInstr, true, "profiling, tracing and the collector add overhead: they must be the same in both captures")
	ba, bb := baselinePhase(a), baselinePhase(b)
	if !ba.HasCPU || !bb.HasCPU {
		cond("Quiet and similar baseline", false, true, "the process CPU before the active phase was not measured in both captures (a baseline shorter than the sampling interval has no CPU sample)")
	} else {
		quiet := math.Abs(ba.CPUAvg-bb.CPUAvg) <= 20 && ba.CPUAvg < 50 && bb.CPUAvg < 50
		cond("Quiet and similar baseline", quiet, true, fmt.Sprintf("process CPU before the active phase: %.1f %% / %.1f %% of one core", ba.CPUAvg, bb.CPUAvg))
	}
	c.Comparable = true
	for _, x := range c.Conditions {
		if x.Blocking && !x.OK {
			c.Comparable = false
		}
	}

	// Routes.
	ra := map[string]*RouteStats{}
	for i := range a.Routes {
		ra[a.Routes[i].Key] = &a.Routes[i]
	}
	rb := map[string]*RouteStats{}
	for i := range b.Routes {
		rb[b.Routes[i].Key] = &b.Routes[i]
	}
	var keys []string
	for k := range ra {
		if _, ok := rb[k]; ok {
			keys = append(keys, k)
		} else if ra[k].Count > 0 {
			c.OnlyA = append(c.OnlyA, k)
		}
	}
	for k := range rb {
		if _, ok := ra[k]; !ok && rb[k].Count > 0 {
			c.OnlyB = append(c.OnlyB, k)
		}
	}
	sort.Strings(keys)
	sort.Strings(c.OnlyA)
	sort.Strings(c.OnlyB)
	for _, k := range keys {
		x, y := ra[k], rb[k]
		if x.Count == 0 && y.Count == 0 {
			continue
		}
		c.Routes = append(c.Routes, compareRoute(c, x, y))
	}
	holm(c.Routes)
	for _, rd := range c.Routes {
		if rd.Verdict == Slower {
			c.Regressions = append(c.Regressions, fmt.Sprintf("%s: median %s → %s (%+.1f %%, p = %.3g)", rd.Key, fmtDur(rd.A.P50), fmtDur(rd.B.P50), float64(rd.Change), float64(rd.P)))
		}
	}

	// Process-wide numbers of the active phases.
	pa, pb := activePhase(a), activePhase(b)
	if pa.HasCPU && pb.HasCPU {
		c.Process = append(c.Process, Delta{Name: "Process CPU, active phase (average)", A: pa.CPUAvg, B: pb.CPUAvg, Unit: "%", Change: change(pa.CPUAvg, pb.CPUAvg), Basis: Measured, Scope: ScopeProcess,
			Note: "includes everything the process did, not only the requests"})
	}
	if pa.HasMem && pb.HasMem {
		c.Process = append(c.Process, Delta{Name: "Peak RSS, active phase", A: float64(pa.RSSMax), B: float64(pb.RSSMax), Unit: "bytes", Change: change(float64(pa.RSSMax), float64(pb.RSSMax)), Basis: Measured, Scope: ScopeProcess})
	}
	if a.Runtime != nil && b.Runtime != nil {
		for _, ma := range a.Runtime.Metrics {
			for _, mb := range b.Runtime.Metrics {
				if ma.Name == mb.Name && ma.Basis != Unavailable && mb.Basis != Unavailable {
					c.Process = append(c.Process, Delta{Name: ma.Name, A: ma.Value, B: mb.Value, Unit: ma.Unit, Change: change(ma.Value, mb.Value), Basis: ma.Basis, Scope: ma.Scope, Note: ma.Note})
				}
			}
		}
	}
	if a.Profiles != nil && b.Profiles != nil && a.Profiles.CPU != nil && b.Profiles.CPU != nil {
		c.Functions = funcDeltas(a.Profiles.CPU, b.Profiles.CPU)
	}
	if !c.Comparable {
		c.Notes = append(c.Notes, "The captures were not made under comparable conditions: every verdict is inconclusive. The numbers are shown for reference.")
	}
	return c
}

func compareRoute(c *Comparison, x, y *RouteStats) RouteDelta {
	rd := RouteDelta{Key: x.Key, NA: x.Count, NB: y.Count, A: x.Latency, B: y.Latency, P: Num(math.NaN())}
	rd.Change = change(float64(x.Latency.P50), float64(y.Latency.P50))
	reasons := []string{}
	if !c.Comparable {
		reasons = append(reasons, "capture conditions differ (see the conditions)")
	}
	if x.Count < minRequests || y.Count < minRequests {
		reasons = append(reasons, fmt.Sprintf("too few requests (%d and %d; at least %d each are needed)", x.Count, y.Count, minRequests))
	}
	if x.LatencyBasis != y.LatencyBasis {
		reasons = append(reasons, "latency measured differently (server side vs proxy)")
	}
	switch {
	case x.MaxInFlight == 0 || y.MaxInFlight == 0:
		reasons = append(reasons, "concurrency not measured in both captures (the proxy or the collector measures it)")
	case x.MaxInFlight > 1 || y.MaxInFlight > 1:
		// Requests overlapped: the load must be comparable too.
		if !within(float64(x.MaxInFlight), float64(y.MaxInFlight), 1.5) || !within(x.MeanInFlight, y.MeanInFlight, 1.5) {
			reasons = append(reasons, fmt.Sprintf("different concurrency (max %d vs %d, mean %.1f vs %.1f requests in flight)", x.MaxInFlight, y.MaxInFlight, x.MeanInFlight, y.MeanInFlight))
		}
		if !within(x.Rate, y.Rate, 1.5) {
			reasons = append(reasons, fmt.Sprintf("different request rates under concurrency (%.1f vs %.1f per second)", x.Rate, y.Rate))
		}
	}
	if x.Errors*2 > x.Count || y.Errors*2 > y.Count {
		reasons = append(reasons, "most requests failed in one of the captures")
	}
	if len(x.Durations) >= 2 && len(y.Durations) >= 2 {
		rd.P = Num(perf.MannWhitney(x.Durations, y.Durations))
	}
	switch {
	case len(reasons) > 0:
		rd.Verdict = Inconclusive
	case !rd.P.Defined() || !rd.Change.Defined():
		rd.Verdict = Inconclusive
		reasons = append(reasons, "not enough data for a test")
	case rd.P < 0.05 && math.Abs(float64(rd.Change)) >= 5:
		// Provisional: holm() keeps it only if it survives the correction
		// for comparing several routes at once.
		rd.Verdict = Faster
		if rd.Change > 0 {
			rd.Verdict = Slower
		}
	default:
		rd.Verdict = NoChange
		if cv(x.Durations) > 0.5 || cv(y.Durations) > 0.5 {
			reasons = append(reasons, "durations vary a lot within each capture: a small change may be hidden by the noise")
		}
	}
	rd.Reasons = reasons
	// Other numbers of the route.
	for _, ma := range x.Metrics {
		for _, mb := range y.Metrics {
			if ma.Name == mb.Name && ma.Basis != Unavailable && mb.Basis != Unavailable {
				rd.Deltas = append(rd.Deltas, Delta{Name: ma.Name, A: ma.Value, B: mb.Value, Unit: ma.Unit, Change: change(ma.Value, mb.Value), Basis: ma.Basis, Scope: ma.Scope, Note: ma.Note})
			}
		}
	}
	sqlA, sqlB := perReq(x), perReq(y)
	if !x.SQLObserved || !y.SQLObserved {
		if len(x.Statements) > 0 || len(y.Statements) > 0 {
			rd.Reasons = append(rd.Reasons, "SQL not compared: one of the captures had no source that observes SQL for this route")
		}
	} else if x.Count > 0 && y.Count > 0 && (len(x.Statements) > 0 || len(y.Statements) > 0) {
		rd.Deltas = append(rd.Deltas, Delta{Name: "SQL statements per request", A: sqlA, B: sqlB, Unit: "count", Change: change(sqlA, sqlB), Basis: Measured, Scope: ScopeRequest})
		ta, tb := sqlTime(x), sqlTime(y)
		rd.Deltas = append(rd.Deltas, Delta{Name: "SQL time per request", A: ta, B: tb, Unit: "ns", Change: change(ta, tb), Basis: Measured, Scope: ScopeRequest})
	}
	if !x.SQLObserved || !y.SQLObserved {
		return rd
	}
	qa, qb := map[string]bool{}, map[string]bool{}
	for _, s := range x.Statements {
		qa[normQuery(s.Query)] = true
	}
	for _, s := range y.Statements {
		qb[normQuery(s.Query)] = true
	}
	for _, s := range y.Statements {
		if n := normQuery(s.Query); !qa[n] {
			rd.SQLAdded = append(rd.SQLAdded, s.Query)
			qa[n] = true
		}
	}
	for _, s := range x.Statements {
		if n := normQuery(s.Query); !qb[n] {
			rd.SQLGone = append(rd.SQLGone, s.Query)
			qb[n] = true
		}
	}
	return rd
}

// within reports whether a and b differ by at most the factor f.
func within(a, b, f float64) bool {
	if a <= 0 || b <= 0 {
		return a == b
	}
	return a/b <= f && b/a <= f
}

// holm applies the Holm–Bonferroni correction to the routes with a test:
// a difference is kept only if its p-value survives comparing all the
// routes at once (family-wise error 0.05).
func holm(routes []RouteDelta) {
	var idx []int
	for i, r := range routes {
		if (r.Verdict == Slower || r.Verdict == Faster || r.Verdict == NoChange) && r.P.Defined() {
			idx = append(idx, i)
		}
	}
	sort.Slice(idx, func(a, b int) bool { return routes[idx[a]].P < routes[idx[b]].P })
	m := len(idx)
	rejecting := true
	for k, i := range idx {
		r := &routes[i]
		rejecting = rejecting && float64(r.P) <= 0.05/float64(m-k)
		if (r.Verdict == Slower || r.Verdict == Faster) && !rejecting {
			r.Verdict = NoChange
			r.Reasons = append(r.Reasons, fmt.Sprintf("p = %.3g is not significant once the %d routes compared are taken into account (Holm correction)", float64(r.P), m))
		}
	}
}

func normQuery(q string) string { return strings.ToLower(strings.Join(strings.Fields(q), " ")) }

func perReq(r *RouteStats) float64 {
	if r.Count == 0 {
		return 0
	}
	n := 0
	for _, s := range r.Statements {
		n += s.Count
	}
	return round(float64(n)/float64(r.Count), 2)
}

func sqlTime(r *RouteStats) float64 {
	if r.Count == 0 {
		return 0
	}
	var t time.Duration
	for _, s := range r.Statements {
		t += s.Total
	}
	return float64(t) / float64(r.Count)
}

// cv is the coefficient of variation.
func cv(xs []float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	mean := 0.0
	for _, x := range xs {
		mean += x
	}
	mean /= float64(len(xs))
	if mean == 0 {
		return 0
	}
	v := 0.0
	for _, x := range xs {
		v += (x - mean) * (x - mean)
	}
	return math.Sqrt(v/float64(len(xs)-1)) / mean
}

// funcDeltas lists the module functions whose share of CPU samples
// changed the most.
func funcDeltas(a, b *perf.Report) []FuncDelta {
	type v struct {
		sym  string
		a, b float64
	}
	m := map[string]*v{}
	for _, f := range a.Funcs {
		if f.Symbol != "" {
			m[f.Name] = &v{sym: f.Symbol, a: f.CumPct}
		}
	}
	for _, f := range b.Funcs {
		if f.Symbol == "" {
			continue
		}
		if x := m[f.Name]; x != nil {
			x.b = f.CumPct
		} else {
			m[f.Name] = &v{sym: f.Symbol, b: f.CumPct}
		}
	}
	var out []FuncDelta
	for name, x := range m {
		if math.Abs(x.b-x.a) >= 1 {
			out = append(out, FuncDelta{Name: name, Symbol: x.sym, A: round(x.a, 1), B: round(x.b, 1)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return math.Abs(out[i].B-out[i].A) > math.Abs(out[j].B-out[j].A) })
	if len(out) > 20 {
		out = out[:20]
	}
	return out
}

func fmtDur(d time.Duration) string {
	switch {
	case d >= time.Second:
		return fmt.Sprintf("%.2f s", d.Seconds())
	case d >= time.Millisecond:
		return fmt.Sprintf("%.2f ms", float64(d)/1e6)
	}
	return fmt.Sprintf("%.0f µs", float64(d)/1e3)
}
