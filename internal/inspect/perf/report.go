// Package perf is the Performance Inspector: it reads CPU and memory
// profiles (pprof), benchmark output and compiler escape-analysis
// diagnostics, and relates them to the module's functions.
//
// Profiles are sampled: their numbers are statistical estimates. Benchmark
// results are measurements of the runs that produced them. Compiler
// diagnostics are reported verbatim, without added interpretation.
package perf

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/athos/gitview/internal/evidence"
	"github.com/athos/gitview/internal/flow"
)

type FuncStat struct {
	Name    string  `json:"name"`
	File    string  `json:"file,omitempty"`
	Line    int     `json:"line,omitempty"`
	Flat    int64   `json:"flat"`
	Cum     int64   `json:"cum"`
	FlatPct float64 `json:"flatPct"`
	CumPct  float64 `json:"cumPct"`
	Symbol  string  `json:"symbol,omitempty"` // module symbol ID, when the function belongs to the module
}

type LineStat struct {
	Func   string `json:"func"`
	File   string `json:"file"`
	Line   int    `json:"line"`
	Flat   int64  `json:"flat"`
	Cum    int64  `json:"cum"`
	Symbol string `json:"symbol,omitempty"`
}

type CallEdge struct {
	From   string `json:"from"` // caller function name
	To     string `json:"to"`
	Weight int64  `json:"weight"`
}

type FlameNode struct {
	Name     string       `json:"name"`
	Value    int64        `json:"value"`
	Self     int64        `json:"self,omitempty"`
	Symbol   string       `json:"symbol,omitempty"`
	Children []*FlameNode `json:"children,omitempty"`
}

type Report struct {
	Kind        string                `json:"kind"` // cpu | heap | block | mutex | goroutine | other
	SampleType  string                `json:"sampleType"`
	SampleTypes []string              `json:"sampleTypes"`
	Unit        string                `json:"unit"`
	Total       int64                 `json:"total"`
	Samples     int                   `json:"samples"`
	DurationMs  float64               `json:"durationMs,omitempty"`
	Period      string                `json:"period,omitempty"`
	Meaning     string                `json:"meaning"`
	Funcs       []FuncStat            `json:"funcs"`
	FuncCount   int                   `json:"funcCount"`
	Lines       []LineStat            `json:"lines"`
	Edges       []CallEdge            `json:"edges"`
	Flame       *FlameNode            `json:"flame"`
	Notes       []string              `json:"notes,omitempty"`
	Artifact    evidence.Artifact     `json:"artifact"`
	Evidence    []evidence.Evidence   `json:"evidence"`
	Status      evidence.Completeness `json:"status"`
}

type Options struct {
	SampleType string  // e.g. alloc_space; default: the profile's default
	MaxFuncs   int     // default 500
	MaxLines   int     // default 300
	MaxEdges   int     // default 2000
	MinFlame   float64 // flame nodes below this fraction of the total are folded (default 0.001)
}

// Load reads and analyses a profile file.
func Load(path string, progs []*flow.Program, opt Options) (*Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	p, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	r, err := Analyze(p, progs, opt)
	if err != nil {
		return nil, err
	}
	r.Artifact = evidence.Artifact{Kind: r.Kind, Name: filepath.Base(path), Path: path, Imported: true}
	if st, err := os.Stat(path); err == nil {
		r.Artifact.CapturedAt = st.ModTime()
	}
	r.Evidence[0].Source = filepath.Base(path)
	return r, nil
}

func kindOf(p *Profile) string {
	types := map[string]bool{}
	for _, t := range p.SampleTypes {
		types[t.Type] = true
	}
	switch {
	case types["cpu"]:
		return "cpu"
	case types["alloc_space"] || types["inuse_space"]:
		return "heap"
	case types["contentions"] || types["delay"]:
		// Block and mutex profiles have the same format: they cannot be told apart.
		return "contention"
	case types["goroutine"]:
		return "goroutine"
	}
	return "other"
}

var meanings = map[string]string{
	"cpu":           "CPU time sampled while the program ran (statistical estimate)",
	"samples":       "number of CPU samples (statistical estimate)",
	"alloc_space":   "bytes allocated since the program started (cumulative; sampled allocations scaled to estimates)",
	"alloc_objects": "objects allocated since the program started (cumulative; sampled allocations scaled to estimates)",
	"inuse_space":   "bytes still in use at the last garbage collection before the profile was written (sampled estimate)",
	"inuse_objects": "objects still in use at the last garbage collection before the profile was written (sampled estimate)",
	"contentions":   "number of blocking or contention events (sampled)",
	"delay":         "time spent blocked or waiting for locks (sampled)",
	"goroutine":     "goroutines with this stack when the profile was written",
}

// Analyze aggregates a profile by function, line and call edge.
func Analyze(p *Profile, progs []*flow.Program, opt Options) (*Report, error) {
	if opt.MaxFuncs <= 0 {
		opt.MaxFuncs = 500
	}
	if opt.MaxLines <= 0 {
		opt.MaxLines = 300
	}
	if opt.MaxEdges <= 0 {
		opt.MaxEdges = 2000
	}
	if opt.MinFlame <= 0 {
		opt.MinFlame = 0.001
	}
	idx := -1
	want := opt.SampleType
	if want == "" {
		want = p.DefaultType
	}
	for i, t := range p.SampleTypes {
		if t.Type == want {
			idx = i
		}
	}
	if idx < 0 {
		if opt.SampleType != "" {
			var names []string
			for _, t := range p.SampleTypes {
				names = append(names, t.Type)
			}
			return nil, fmt.Errorf("sample type %q not in profile (available: %s)", opt.SampleType, strings.Join(names, ", "))
		}
		idx = len(p.SampleTypes) - 1
	}
	st := p.SampleTypes[idx]
	r := &Report{Kind: kindOf(p), SampleType: st.Type + "/" + st.Unit, Unit: st.Unit, Samples: len(p.Samples), Meaning: meanings[st.Type]}
	for _, t := range p.SampleTypes {
		r.SampleTypes = append(r.SampleTypes, t.Type)
	}
	if p.DurationNanos > 0 {
		r.DurationMs = float64(p.DurationNanos) / 1e6
	}
	if p.Period > 0 && p.PeriodType.Type != "" {
		r.Period = fmt.Sprintf("%s every %s", p.PeriodType.Type, formatValue(p.Period, p.PeriodType.Unit))
	}
	m := newMapper(progs)

	type fkey struct{ name, file string }
	funcs := map[fkey]*FuncStat{}
	lines := map[string]*LineStat{}
	edges := map[[2]string]int64{}
	root := &FlameNode{Name: "root"}
	for _, s := range p.Samples {
		v := s.Values[idx]
		if v == 0 {
			continue
		}
		r.Total += v
		frames := expand(p, s)
		if len(frames) == 0 {
			continue
		}
		seenF := map[fkey]bool{}
		seenL := map[string]bool{}
		seenE := map[[2]string]bool{}
		for i, f := range frames {
			k := fkey{f.name, f.file}
			fs := funcs[k]
			if fs == nil {
				fs = &FuncStat{Name: f.name, File: m.rel(f.file), Line: int(f.start), Symbol: m.symbol(f.name, f.file, int(f.start))}
				funcs[k] = fs
			}
			if i == 0 {
				fs.Flat += v
			}
			if !seenF[k] {
				seenF[k] = true
				fs.Cum += v
			}
			lk := fmt.Sprintf("%s:%d", f.file, f.line)
			ls := lines[lk]
			if ls == nil {
				ls = &LineStat{Func: f.name, File: m.rel(f.file), Line: int(f.line), Symbol: m.symbol(f.name, f.file, int(f.line))}
				lines[lk] = ls
			}
			if i == 0 {
				ls.Flat += v
			}
			if !seenL[lk] {
				seenL[lk] = true
				ls.Cum += v
			}
			if i+1 < len(frames) {
				e := [2]string{frames[i+1].name, f.name}
				if !seenE[e] {
					seenE[e] = true
					edges[e] += v
				}
			}
		}
		node := root
		node.Value += v
		for i := len(frames) - 1; i >= 0; i-- {
			f := frames[i]
			var child *FlameNode
			for _, c := range node.Children {
				if c.Name == f.name {
					child = c
					break
				}
			}
			if child == nil {
				child = &FlameNode{Name: f.name, Symbol: m.symbol(f.name, f.file, int(f.start))}
				node.Children = append(node.Children, child)
			}
			child.Value += v
			if i == 0 {
				child.Self += v
			}
			node = child
		}
	}
	for _, fs := range funcs {
		if r.Total != 0 {
			fs.FlatPct = 100 * float64(fs.Flat) / float64(r.Total)
			fs.CumPct = 100 * float64(fs.Cum) / float64(r.Total)
		}
		r.Funcs = append(r.Funcs, *fs)
	}
	sort.Slice(r.Funcs, func(i, j int) bool {
		if r.Funcs[i].Flat != r.Funcs[j].Flat {
			return r.Funcs[i].Flat > r.Funcs[j].Flat
		}
		if r.Funcs[i].Cum != r.Funcs[j].Cum {
			return r.Funcs[i].Cum > r.Funcs[j].Cum
		}
		return r.Funcs[i].Name < r.Funcs[j].Name
	})
	r.FuncCount = len(r.Funcs)
	if len(r.Funcs) > opt.MaxFuncs {
		// Keep the top by flat, plus every module function.
		keep := r.Funcs[:opt.MaxFuncs]
		for _, f := range r.Funcs[opt.MaxFuncs:] {
			if f.Symbol != "" {
				keep = append(keep, f)
			}
		}
		r.Funcs = keep
		r.Status.Limit(fmt.Sprintf("%d of %d functions listed", len(r.Funcs), r.FuncCount))
	}
	for _, ls := range lines {
		r.Lines = append(r.Lines, *ls)
	}
	sort.Slice(r.Lines, func(i, j int) bool {
		if r.Lines[i].Flat != r.Lines[j].Flat {
			return r.Lines[i].Flat > r.Lines[j].Flat
		}
		return r.Lines[i].Cum > r.Lines[j].Cum
	})
	if len(r.Lines) > opt.MaxLines {
		keep := r.Lines[:opt.MaxLines]
		for _, l := range r.Lines[opt.MaxLines:] {
			if l.Symbol != "" && l.Cum > 0 {
				keep = append(keep, l)
			}
		}
		r.Lines = keep
	}
	for e, w := range edges {
		r.Edges = append(r.Edges, CallEdge{From: e[0], To: e[1], Weight: w})
	}
	sort.Slice(r.Edges, func(i, j int) bool {
		if r.Edges[i].Weight != r.Edges[j].Weight {
			return r.Edges[i].Weight > r.Edges[j].Weight
		}
		return r.Edges[i].From+r.Edges[i].To < r.Edges[j].From+r.Edges[j].To
	})
	if len(r.Edges) > opt.MaxEdges {
		r.Edges = r.Edges[:opt.MaxEdges]
		r.Status.Limit(fmt.Sprintf("call graph limited to the %d heaviest edges", opt.MaxEdges))
	}
	if folded := fold(root, int64(opt.MinFlame*float64(r.Total))); folded > 0 {
		r.Notes = append(r.Notes, fmt.Sprintf("Flame graph: %d small frames (below %.1f%% of the total) are folded into \"(other)\".", folded, opt.MinFlame*100))
	}
	sortFlame(root)
	r.Flame = root
	if r.Total == 0 {
		r.Notes = append(r.Notes, "The profile has no samples for "+r.SampleType+".")
	}
	if r.Kind == "cpu" && r.Samples < 100 {
		r.Notes = append(r.Notes, fmt.Sprintf("Only %d samples: small percentages are statistically unreliable; profile a longer run.", r.Samples))
	}
	r.Evidence = []evidence.Evidence{{Kind: evidence.Profile, Basis: evidence.BasisEstimated, Source: "pprof",
		Details: "sampled profile: values are statistical estimates (" + r.SampleType + ")"}}
	if r.Kind == "goroutine" {
		r.Evidence[0].Basis = evidence.BasisObserved
	}
	r.Status.Done()
	return r, nil
}

type frame struct {
	name, file  string
	line, start int64
}

// expand turns a sample into frames, leaf first, expanding inlined calls.
func expand(p *Profile, s Sample) []frame {
	var out []frame
	for _, id := range s.Locs {
		loc := p.Locations[id]
		if loc == nil {
			continue
		}
		for _, ln := range loc.Lines {
			if ln.Func == nil {
				continue
			}
			out = append(out, frame{name: ln.Func.Name, file: ln.Func.File, line: ln.Line, start: ln.Func.StartLine})
		}
		if len(loc.Lines) == 0 {
			out = append(out, frame{name: fmt.Sprintf("0x%x", id)})
		}
	}
	return out
}

// fold merges small children into "(other)" nodes and returns how many
// frames were folded.
func fold(n *FlameNode, min int64) int {
	folded := 0
	var keep []*FlameNode
	var other *FlameNode
	for _, c := range n.Children {
		if c.Value < min {
			if other == nil {
				other = &FlameNode{Name: "(other)"}
			}
			other.Value += c.Value
			other.Self += c.Value
			folded += count(c)
			continue
		}
		folded += fold(c, min)
		keep = append(keep, c)
	}
	if other != nil {
		keep = append(keep, other)
	}
	n.Children = keep
	return folded
}

func count(n *FlameNode) int {
	c := 1
	for _, k := range n.Children {
		c += count(k)
	}
	return c
}

func sortFlame(n *FlameNode) {
	sort.SliceStable(n.Children, func(i, j int) bool { return n.Children[i].Value > n.Children[j].Value })
	for _, c := range n.Children {
		sortFlame(c)
	}
}

// mapper relates runtime frames to module symbols.
type mapper struct {
	progs []*flow.Program
	cache map[string]string
}

func newMapper(progs []*flow.Program) *mapper {
	return &mapper{progs: progs, cache: map[string]string{}}
}

func (m *mapper) symbol(name, file string, line int) string {
	key := fmt.Sprintf("%s\x00%s\x00%d", name, file, line)
	if s, ok := m.cache[key]; ok {
		return s
	}
	s := ""
	for _, p := range m.progs {
		if fn := p.FuncForFrame(name, file, line); fn != nil {
			s = flow.Origin(fn).String()
			break
		}
	}
	m.cache[key] = s
	return s
}

func (m *mapper) rel(file string) string {
	for _, p := range m.progs {
		if r := p.NormFile(file); r != "" && !filepath.IsAbs(r) && !strings.HasPrefix(r, "..") {
			return r
		}
	}
	return flow.DisplayPath(file)
}

// SymbolPerf is what a profile says about one module function.
type SymbolPerf struct {
	Symbol  string     `json:"symbol"`
	Found   bool       `json:"found"`
	Flat    int64      `json:"flat"`
	Cum     int64      `json:"cum"`
	FlatPct float64    `json:"flatPct"`
	CumPct  float64    `json:"cumPct"`
	Lines   []LineStat `json:"lines,omitempty"`
	Callers []CallEdge `json:"callers,omitempty"`
	Callees []CallEdge `json:"callees,omitempty"`
}

// ForSymbol extracts the costs attributed to a module function.
func (r *Report) ForSymbol(id string) SymbolPerf {
	sp := SymbolPerf{Symbol: id}
	var names []string
	for _, f := range r.Funcs {
		if f.Symbol == id {
			sp.Found = true
			sp.Flat += f.Flat
			sp.Cum = max(sp.Cum, f.Cum)
			names = append(names, f.Name)
		}
	}
	if r.Total > 0 {
		sp.FlatPct = 100 * float64(sp.Flat) / float64(r.Total)
		sp.CumPct = 100 * float64(sp.Cum) / float64(r.Total)
	}
	for _, l := range r.Lines {
		if l.Symbol == id && (l.Flat > 0 || l.Cum > 0) {
			sp.Lines = append(sp.Lines, l)
		}
	}
	sort.Slice(sp.Lines, func(i, j int) bool { return sp.Lines[i].Line < sp.Lines[j].Line })
	for _, e := range r.Edges {
		for _, n := range names {
			if e.To == n && e.From != n {
				sp.Callers = append(sp.Callers, e)
			}
			if e.From == n && e.To != n {
				sp.Callees = append(sp.Callees, e)
			}
		}
	}
	return sp
}

// FormatValue renders a profile value in its unit.
func (r *Report) FormatValue(v int64) string { return formatValue(v, r.Unit) }

func formatValue(v int64, unit string) string {
	switch unit {
	case "nanoseconds":
		return time.Duration(v).Round(time.Microsecond).String()
	case "bytes":
		f := float64(v)
		switch {
		case f >= 1<<30:
			return fmt.Sprintf("%.2fGB", f/(1<<30))
		case f >= 1<<20:
			return fmt.Sprintf("%.2fMB", f/(1<<20))
		case f >= 1<<10:
			return fmt.Sprintf("%.1fkB", f/(1<<10))
		}
		return fmt.Sprintf("%dB", v)
	}
	return fmt.Sprintf("%d", v)
}
