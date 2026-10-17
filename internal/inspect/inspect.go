// Package inspect is the Go Deep Inspector: it gathers, for one Go symbol,
// what every analysis knows about it — Code Flow (call graph), Data Flow,
// Execution Trace, Branch Explorer, Performance and SQL Explorer — under the
// same symbol identity (flow.Symbol.ID / evidence.SymbolRef).
//
// Static results are computed on demand and cached per code version.
// Runtime results (traces, profiles, benchmarks, compiler diagnostics,
// coverage, query plans) exist only after the user captured or imported
// them; without them the bundle says so instead of inventing data.
package inspect

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/athos/gitview/internal/evidence"
	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/inspect/branch"
	"github.com/athos/gitview/internal/inspect/dataflow"
	"github.com/athos/gitview/internal/inspect/perf"
	"github.com/athos/gitview/internal/inspect/pgexplain"
	"github.com/athos/gitview/internal/inspect/sqlops"
	"github.com/athos/gitview/internal/inspect/xtrace"
	"golang.org/x/tools/go/ssa"
)

// Messages shown when data is missing (never replaced by simulated data).
const (
	NoTrace       = "No runtime trace available"
	NoProfile     = "No profiling data available"
	NoSQL         = "No database operations found"
	SQLUnresolved = "SQL query not statically resolved"
	NoSymbol      = "No matching symbol found"
	NoCoverage    = "No coverage data available"
)

// Session holds the analysed programs and the runtime data loaded so far.
// When a session is shared between goroutines (the TUI), runtime fields are
// changed with Update and read with Snapshot.
type Session struct {
	mu    sync.Mutex
	Root  string
	progs []*flow.Program

	Coverage *branch.Profile
	CPU      *perf.Report
	Mem      *perf.Report
	Benches  []*perf.BenchRun // oldest first
	Escape   *perf.EscapeReport
	Trace    *xtrace.Timeline
	Plans    []*pgexplain.Plan

	cache map[cacheKey]any
}

type cacheKey struct {
	version, symbol, kind, opts string
}

// Runtime is a consistent copy of the runtime data of a session.
type Runtime struct {
	Coverage *branch.Profile
	CPU      *perf.Report
	Mem      *perf.Report
	Benches  []*perf.BenchRun
	Escape   *perf.EscapeReport
	Trace    *xtrace.Timeline
	Plans    []*pgexplain.Plan
}

// Snapshot copies the runtime data under the session lock.
func (s *Session) Snapshot() Runtime {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Runtime{Coverage: s.Coverage, CPU: s.CPU, Mem: s.Mem, Benches: append([]*perf.BenchRun(nil), s.Benches...),
		Escape: s.Escape, Trace: s.Trace, Plans: append([]*pgexplain.Plan(nil), s.Plans...)}
}

// Update changes runtime data under the session lock.
func (s *Session) Update(fn func(s *Session)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(s)
}

func NewSession(root string, progs []*flow.Program) *Session {
	return &Session{Root: root, progs: progs, cache: map[cacheKey]any{}}
}

// SetPrograms replaces the analysed programs (after a reload). Cached
// results of other code versions are dropped.
func (s *Session) SetPrograms(progs []*flow.Program) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.progs = progs
	live := map[string]bool{}
	for _, p := range progs {
		live[p.Version] = true
	}
	for k := range s.cache {
		if !live[k.version] {
			delete(s.cache, k)
		}
	}
}

func (s *Session) Programs() []*flow.Program {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*flow.Program(nil), s.progs...)
}

// Resolved is a symbol located in one of the programs.
type Resolved struct {
	Prog *flow.Program
	Fn   *ssa.Function
	Ref  evidence.SymbolRef
}

// Resolve finds exactly one function by ID, qualified name, file:line or
// bare name. Several matches yield a flow.AmbiguityError.
func (s *Session) Resolve(query string) (*Resolved, error) {
	var found []Resolved
	var syms []flow.Symbol
	for _, p := range s.Programs() {
		if fn := p.FuncByID(query); fn != nil {
			return &Resolved{p, fn, p.Ref(fn)}, nil
		}
		for _, sym := range p.Find(query) {
			if fn := p.FuncByID(sym.ID); fn != nil {
				found = append(found, Resolved{p, fn, p.Ref(fn)})
				syms = append(syms, sym)
			}
		}
	}
	switch len(found) {
	case 0:
		return nil, fmt.Errorf("%s: %q", NoSymbol, query)
	case 1:
		return &found[0], nil
	}
	return nil, &flow.AmbiguityError{Query: query, Candidates: syms}
}

func (s *Session) cached(p *flow.Program, id, kind, opts string, build func() (any, error)) (any, error) {
	k := cacheKey{p.Version, id, kind, opts}
	s.mu.Lock()
	if v, ok := s.cache[k]; ok {
		s.mu.Unlock()
		return v, nil
	}
	s.mu.Unlock()
	v, err := build()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.cache[k] = v
	s.mu.Unlock()
	return v, nil
}

// Branches returns the CFG of a symbol (with the coverage overlay when a
// coverage profile is loaded).
func (s *Session) Branches(r *Resolved) (*branch.Graph, error) {
	s.mu.Lock()
	cov := s.Coverage
	s.mu.Unlock()
	opts := ""
	if cov != nil {
		opts = fmt.Sprintf("%s@%d", cov.Name, cov.ModTime.UnixNano())
	}
	v, err := s.cached(r.Prog, r.Ref.ID, "branches", opts, func() (any, error) {
		return branch.Build(r.Prog, r.Fn, branch.Options{Coverage: cov})
	})
	if err != nil {
		return nil, err
	}
	return v.(*branch.Graph), nil
}

// DataFlow traces a variable of a symbol.
func (s *Session) DataFlow(r *Resolved, q dataflow.Query, opt dataflow.Options) (*dataflow.Result, error) {
	key := fmt.Sprintf("%+v|%+v", q, opt)
	v, err := s.cached(r.Prog, r.Ref.ID, "dataflow", key, func() (any, error) {
		return dataflow.Trace(r.Prog, r.Fn, q, opt)
	})
	if err != nil {
		return nil, err
	}
	return v.(*dataflow.Result), nil
}

// SQL lists the database operations of a symbol, with parameter origins.
func (s *Session) SQL(r *Resolved, reachable bool, depth int) *sqlops.Result {
	v, _ := s.cached(r.Prog, r.Ref.ID, "sql", fmt.Sprint(reachable, depth), func() (any, error) {
		res := sqlops.ForFunction(r.Prog, r.Fn, sqlops.Options{Reachable: reachable, Depth: depth})
		for _, op := range res.Ops {
			op.TraceParams(r.Prog, dataflow.Options{MaxDepth: 4, MaxNodes: 200})
		}
		return res, nil
	})
	return v.(*sqlops.Result)
}

// PerfView is what the loaded performance data says about a symbol.
type PerfView struct {
	CPU        *perf.SymbolPerf  `json:"cpu,omitempty"`
	Mem        *perf.SymbolPerf  `json:"mem,omitempty"`
	CPUProfile string            `json:"cpuProfile,omitempty"`
	MemProfile string            `json:"memProfile,omitempty"`
	MemType    string            `json:"memType,omitempty"`
	Escape     []perf.Diagnostic `json:"escape,omitempty"`
	EscapeRun  bool              `json:"escapeRun"`
	Benchmarks []perf.BenchStat  `json:"benchmarks,omitempty"` // of the symbol's package
	Message    string            `json:"message,omitempty"`
}

// Performance relates the loaded profiles and diagnostics to a symbol.
func (s *Session) Performance(r *Resolved) *PerfView {
	s.mu.Lock()
	cpu, mem, esc := s.CPU, s.Mem, s.Escape
	var bench *perf.BenchRun
	if len(s.Benches) > 0 {
		bench = s.Benches[len(s.Benches)-1]
	}
	s.mu.Unlock()
	v := &PerfView{}
	if cpu != nil {
		sp := cpu.ForSymbol(r.Ref.ID)
		v.CPU, v.CPUProfile = &sp, cpu.Artifact.Name
	}
	if mem != nil {
		sp := mem.ForSymbol(r.Ref.ID)
		v.Mem, v.MemProfile, v.MemType = &sp, mem.Artifact.Name, mem.SampleType
	}
	if esc != nil {
		v.EscapeRun = true
		v.Escape = esc.ForSymbol(r.Ref.ID)
	}
	if bench != nil {
		for _, st := range bench.Stats() {
			if st.Pkg == r.Ref.Pkg || st.Pkg == "" {
				v.Benchmarks = append(v.Benchmarks, st)
			}
		}
	}
	if cpu == nil && mem == nil && esc == nil && bench == nil {
		v.Message = NoProfile
	}
	return v
}

// TraceView is what the loaded trace shows about a symbol.
type TraceView struct {
	Loaded  bool                `json:"loaded"`
	Trace   string              `json:"trace,omitempty"`
	Symbol  *xtrace.SymbolTrace `json:"symbol,omitempty"`
	Message string              `json:"message,omitempty"`
}

func (s *Session) TraceFor(r *Resolved) *TraceView {
	s.mu.Lock()
	t := s.Trace
	s.mu.Unlock()
	if t == nil {
		return &TraceView{Message: NoTrace}
	}
	st := t.ForSymbol(r.Ref.ID)
	v := &TraceView{Loaded: true, Trace: t.Artifact.Name, Symbol: &st}
	if !st.Found {
		v.Message = "The loaded trace has no events attributed to this function (no goroutine started in it, no region measuring it, and no blocking stack through it)."
	}
	return v
}

// Bundle is everything known about one symbol.
type Bundle struct {
	Symbol    evidence.SymbolRef          `json:"symbol"`
	Code      string                      `json:"code,omitempty"` // source of the function (bounded)
	Branches  *branch.Graph               `json:"branches,omitempty"`
	Variables []dataflow.VarInfo          `json:"variables"`
	DataFlow  map[string]*dataflow.Result `json:"dataflow,omitempty"` // by "name@line"
	SQL       *sqlops.Result              `json:"sql"`
	Perf      *PerfView                   `json:"performance"`
	Trace     *TraceView                  `json:"trace"`
	Errors    map[string]string           `json:"errors,omitempty"`
}

type BundleOptions struct {
	DataFlow      bool // trace every variable (bounded by MaxVariables)
	MaxVariables  int  // default 25
	DataFlowDepth int  // default 3
	DataFlowNodes int  // default 150
	SQLDepth      int  // default 4
}

// Bundle computes every view for one symbol.
func (s *Session) Bundle(r *Resolved, opt BundleOptions) *Bundle {
	if opt.MaxVariables <= 0 {
		opt.MaxVariables = 25
	}
	if opt.DataFlowDepth <= 0 {
		opt.DataFlowDepth = 3
	}
	if opt.DataFlowNodes <= 0 {
		opt.DataFlowNodes = 150
	}
	if opt.SQLDepth <= 0 {
		opt.SQLDepth = 4
	}
	b := &Bundle{Symbol: r.Ref, Errors: map[string]string{}}
	if r.Ref.File != "" {
		b.Code = r.Prog.Source(r.Ref.File, r.Ref.Line, min(r.Ref.EndLine, r.Ref.Line+400))
	}
	if g, err := s.Branches(r); err == nil {
		b.Branches = g
	} else {
		b.Errors["branches"] = err.Error()
	}
	b.Variables = dataflow.Variables(r.Prog, r.Fn)
	if opt.DataFlow {
		b.DataFlow = map[string]*dataflow.Result{}
		for i, v := range b.Variables {
			if i >= opt.MaxVariables {
				b.Errors["dataflow"] = fmt.Sprintf("only the first %d of %d variables were traced", opt.MaxVariables, len(b.Variables))
				break
			}
			res, err := s.DataFlow(r, dataflow.Query{Variable: v.Name, Line: v.Line, Return: -1},
				dataflow.Options{Direction: dataflow.Both, MaxDepth: opt.DataFlowDepth, MaxNodes: opt.DataFlowNodes})
			if err != nil {
				var amb *dataflow.AmbiguousError
				if !errors.As(err, &amb) {
					b.Errors["dataflow:"+v.Name] = err.Error()
				}
				continue
			}
			b.DataFlow[fmt.Sprintf("%s@%d", v.Name, v.Line)] = res
		}
	}
	b.SQL = s.SQL(r, true, opt.SQLDepth)
	b.Perf = s.Performance(r)
	b.Trace = s.TraceFor(r)
	if len(b.Errors) == 0 {
		b.Errors = nil
	}
	return b
}

// AddBench records a benchmark run (kept in order for comparisons).
func (s *Session) AddBench(r *perf.BenchRun) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Benches = append(s.Benches, r)
	if len(s.Benches) > 10 {
		s.Benches = s.Benches[len(s.Benches)-10:]
	}
}

// AddPlan records an EXPLAIN result.
func (s *Session) AddPlan(p *pgexplain.Plan) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Plans = append(s.Plans, p)
}

// Changed returns the module functions touched by the working tree diff,
// as starting points for reports.
func (s *Session) Changed(changed map[string]map[int]bool) []*Resolved {
	var out []*Resolved
	for _, p := range s.Programs() {
		for _, c := range p.ChangedFunctions(changed) {
			if fn := p.FuncByID(c.ID); fn != nil {
				out = append(out, &Resolved{p, fn, p.Ref(fn)})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Ref.Qualified < out[j].Ref.Qualified })
	return out
}
