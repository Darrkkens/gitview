package perf

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/athos/gitview/internal/evidence"
)

// BenchResult is one line of `go test -bench` output.
type BenchResult struct {
	Pkg        string             `json:"pkg,omitempty"`
	Name       string             `json:"name"` // without the -GOMAXPROCS suffix
	Procs      int                `json:"procs,omitempty"`
	Iterations int64              `json:"iterations"`
	Metrics    map[string]float64 `json:"metrics"` // unit → value: ns/op, B/op, allocs/op, custom
	Units      []string           `json:"units"`   // in output order
}

// BenchRun is a parsed benchmark output: configuration and results.
type BenchRun struct {
	Config   map[string]string   `json:"config"` // goos, goarch, pkg, cpu...
	Results  []BenchResult       `json:"results"`
	Failures []string            `json:"failures,omitempty"`
	Artifact evidence.Artifact   `json:"artifact"`
	Evidence []evidence.Evidence `json:"evidence"`
}

var (
	benchLine  = regexp.MustCompile(`^(Benchmark\S*)\s+(\d+)\s+(.+)$`)
	configLine = regexp.MustCompile(`^([a-z][^:\s]*):\s*(.*)$`)
	procSuffix = regexp.MustCompile(`-(\d+)$`)
)

// ParseBench reads the standard Go benchmark format.
func ParseBench(r io.Reader) (*BenchRun, error) {
	run := &BenchRun{Config: map[string]string{}}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 4<<20)
	pkg := ""
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if m := configLine.FindStringSubmatch(line); m != nil && !strings.HasPrefix(line, "Benchmark") && m[1] != "panic" && m[1] != "ok" {
			if m[1] == "pkg" {
				pkg = m[2]
			}
			if _, ok := run.Config[m[1]]; !ok || m[1] != "pkg" {
				run.Config[m[1]] = m[2]
			}
			continue
		}
		if strings.HasPrefix(line, "--- FAIL") || strings.HasPrefix(line, "FAIL") {
			run.Failures = append(run.Failures, line)
			continue
		}
		m := benchLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		fields := strings.Fields(m[3])
		if len(fields) < 2 || len(fields)%2 != 0 {
			continue
		}
		res := BenchResult{Pkg: pkg, Name: m[1], Metrics: map[string]float64{}}
		if pm := procSuffix.FindStringSubmatch(res.Name); pm != nil {
			res.Procs, _ = strconv.Atoi(pm[1])
			res.Name = strings.TrimSuffix(res.Name, pm[0])
		}
		res.Iterations, _ = strconv.ParseInt(m[2], 10, 64)
		ok := true
		for i := 0; i < len(fields); i += 2 {
			v, err := strconv.ParseFloat(fields[i], 64)
			if err != nil {
				ok = false
				break
			}
			res.Metrics[fields[i+1]] = v
			res.Units = append(res.Units, fields[i+1])
		}
		if ok {
			run.Results = append(run.Results, res)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(run.Results) == 0 {
		return run, fmt.Errorf("no benchmark results found (expected lines like \"BenchmarkX-8  1000  123 ns/op\")")
	}
	run.Evidence = []evidence.Evidence{{Kind: evidence.Benchmark, Basis: evidence.BasisObserved, Source: "go test -bench",
		Details: "measured by the benchmark harness; ns/op varies with machine load, B/op and allocs/op are averages per operation"}}
	return run, nil
}

// LoadBench reads a benchmark output file.
func LoadBench(path string) (*BenchRun, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	run, err := ParseBench(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	run.Artifact = evidence.Artifact{Kind: "bench", Name: filepath.Base(path), Path: path, Imported: true}
	if st, err := f.Stat(); err == nil {
		run.Artifact.CapturedAt = st.ModTime()
	}
	return run, nil
}

// BenchStat summarises the samples (-count) of one benchmark and unit.
type BenchStat struct {
	Pkg    string    `json:"pkg,omitempty"`
	Name   string    `json:"name"`
	Procs  int       `json:"procs,omitempty"`
	Unit   string    `json:"unit"`
	N      int       `json:"n"`
	Mean   float64   `json:"mean"`
	Median float64   `json:"median"`
	Min    float64   `json:"min"`
	Max    float64   `json:"max"`
	StdDev float64   `json:"stddev"`
	Values []float64 `json:"values"`
	Iters  int64     `json:"iterations"` // of the last sample
}

func (s BenchStat) key() string {
	return fmt.Sprintf("%s\x00%s\x00%d\x00%s", s.Pkg, s.Name, s.Procs, s.Unit)
}

// Stats groups results by benchmark and unit.
func (run *BenchRun) Stats() []BenchStat {
	idx := map[string]int{}
	var out []BenchStat
	for _, r := range run.Results {
		for _, u := range r.Units {
			s := BenchStat{Pkg: r.Pkg, Name: r.Name, Procs: r.Procs, Unit: u}
			k := s.key()
			i, ok := idx[k]
			if !ok {
				i = len(out)
				idx[k] = i
				out = append(out, s)
			}
			out[i].Values = append(out[i].Values, r.Metrics[u])
			out[i].Iters = r.Iterations
		}
	}
	for i := range out {
		s := &out[i]
		v := append([]float64(nil), s.Values...)
		sort.Float64s(v)
		s.N, s.Min, s.Max = len(v), v[0], v[len(v)-1]
		sum := 0.0
		for _, x := range v {
			sum += x
		}
		s.Mean = sum / float64(len(v))
		if len(v)%2 == 1 {
			s.Median = v[len(v)/2]
		} else {
			s.Median = (v[len(v)/2-1] + v[len(v)/2]) / 2
		}
		if len(v) > 1 {
			ss := 0.0
			for _, x := range v {
				ss += (x - s.Mean) * (x - s.Mean)
			}
			s.StdDev = math.Sqrt(ss / float64(len(v)-1))
		}
	}
	return out
}

type CompareRow struct {
	Pkg            string     `json:"pkg,omitempty"`
	Name           string     `json:"name"`
	Unit           string     `json:"unit"`
	Old            *BenchStat `json:"old,omitempty"`
	New            *BenchStat `json:"new,omitempty"`
	DeltaPct       float64    `json:"deltaPct"`
	DeltaUndefined bool       `json:"deltaUndefined,omitempty"` // old median is 0: relative change undefined
	P              float64    `json:"p"`                        // Mann–Whitney U, two-sided; -1 when not computable
	Significant    bool       `json:"significant"`
	Note           string     `json:"note,omitempty"`
}

type Comparison struct {
	Rows        []CompareRow `json:"rows"`
	ConfigDiffs []string     `json:"configDiffs,omitempty"`
	Method      string       `json:"method"`
	Benchstat   string       `json:"benchstat,omitempty"` // benchstat output, when it was run
	Alpha       float64      `json:"alpha"`
}

// Compare contrasts two runs (before → after) per benchmark and unit, with
// the same test benchstat uses (Mann–Whitney U, α = 0.05).
func Compare(old, cur *BenchRun) *Comparison {
	c := &Comparison{Method: "Mann–Whitney U test (two-sided, α = 0.05), as used by benchstat; Δ compares medians", Alpha: 0.05}
	keys := map[string]bool{}
	for k := range old.Config {
		keys[k] = true
	}
	for k := range cur.Config {
		keys[k] = true
	}
	var ks []string
	for k := range keys {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		if old.Config[k] != cur.Config[k] {
			c.ConfigDiffs = append(c.ConfigDiffs, fmt.Sprintf("%s: %q → %q", k, old.Config[k], cur.Config[k]))
		}
	}
	olds := map[string]BenchStat{}
	for _, s := range old.Stats() {
		olds[s.key()] = s
	}
	seen := map[string]bool{}
	for _, s := range cur.Stats() {
		ns := s
		row := CompareRow{Pkg: s.Pkg, Name: s.Name, Unit: s.Unit, New: &ns, P: -1}
		if o, ok := olds[s.key()]; ok {
			oc := o
			row.Old = &oc
			seen[s.key()] = true
			switch {
			case o.Median != 0:
				row.DeltaPct = 100 * (s.Median - o.Median) / o.Median
			case s.Median != 0:
				row.DeltaUndefined = true
			}
			if o.N < 4 || s.N < 4 {
				row.Note = fmt.Sprintf("too few samples for a significance test (%d vs %d; use -count=6 or more)", o.N, s.N)
			} else {
				row.P = mannWhitney(o.Values, s.Values)
				row.Significant = row.P < c.Alpha
				if !row.Significant {
					row.Note = "no statistically significant difference"
				}
			}
			if row.DeltaUndefined {
				row.Note = strings.TrimPrefix(row.Note+"; ", "; ") + fmt.Sprintf("was 0, now %.4g", s.Median)
			}
		} else {
			row.Note = "only in the new run"
		}
		c.Rows = append(c.Rows, row)
	}
	for _, s := range old.Stats() {
		if !seen[s.key()] {
			o := s
			c.Rows = append(c.Rows, CompareRow{Pkg: s.Pkg, Name: s.Name, Unit: s.Unit, Old: &o, P: -1, Note: "only in the old run"})
		}
	}
	return c
}

// mannWhitney returns the two-sided p-value of the Mann–Whitney U test:
// exact without ties for small samples, normal approximation otherwise.
func mannWhitney(a, b []float64) float64 {
	n, m := len(a), len(b)
	type obs struct {
		v float64
		g int
	}
	all := make([]obs, 0, n+m)
	for _, x := range a {
		all = append(all, obs{x, 0})
	}
	for _, x := range b {
		all = append(all, obs{x, 1})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].v < all[j].v })
	ranks := make([]float64, len(all))
	ties := false
	tieTerm := 0.0
	for i := 0; i < len(all); {
		j := i
		for j+1 < len(all) && all[j+1].v == all[i].v {
			j++
		}
		r := float64(i+j)/2 + 1
		for k := i; k <= j; k++ {
			ranks[k] = r
		}
		if t := float64(j - i + 1); t > 1 {
			ties = true
			tieTerm += t*t*t - t
		}
		i = j + 1
	}
	ra := 0.0
	for i, o := range all {
		if o.g == 0 {
			ra += ranks[i]
		}
	}
	u := ra - float64(n*(n+1))/2
	if !ties && n <= 30 && m <= 30 {
		// Exact distribution of U by dynamic programming.
		counts := uDistribution(n, m)
		total := 0.0
		for _, c := range counts {
			total += c
		}
		ui := int(math.Round(u))
		le, ge := 0.0, 0.0
		for k, c := range counts {
			if k <= ui {
				le += c
			}
			if k >= ui {
				ge += c
			}
		}
		return math.Min(1, 2*math.Min(le, ge)/total)
	}
	nm := float64(n * m)
	mean := nm / 2
	N := float64(n + m)
	sd := math.Sqrt(nm / 12 * ((N + 1) - tieTerm/(N*(N-1))))
	if sd == 0 {
		return 1
	}
	z := (math.Abs(u-mean) - 0.5) / sd
	return math.Min(1, math.Erfc(z/math.Sqrt2))
}

// uDistribution counts the arrangements giving each value of U.
func uDistribution(n, m int) []float64 {
	// f[i][j][u]: arrangements of i and j elements with statistic u.
	memo := map[[3]int]float64{}
	var f func(i, j, u int) float64
	f = func(i, j, u int) float64 {
		if u < 0 {
			return 0
		}
		if i == 0 || j == 0 {
			if u == 0 {
				return 1
			}
			return 0
		}
		k := [3]int{i, j, u}
		if v, ok := memo[k]; ok {
			return v
		}
		v := f(i-1, j, u-j) + f(i, j-1, u)
		memo[k] = v
		return v
	}
	out := make([]float64, n*m+1)
	for u := range out {
		out[u] = f(n, m, u)
	}
	return out
}

// MannWhitney returns the two-sided p-value of the Mann–Whitney U test
// comparing two samples (exact for small samples without ties).
func MannWhitney(a, b []float64) float64 { return mannWhitney(a, b) }
