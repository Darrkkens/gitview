// Package xtrace is the Execution Trace module: it reads Go execution traces
// (runtime/trace, `go test -trace`) and turns the recorded events into a
// timeline of goroutine states, GC phases, tasks, regions and logs.
//
// Everything shown comes from events present in the trace. runtime/trace
// does not record every function call: per-function durations exist only
// for regions, either written by the program itself or added by GitView's
// optional, temporary instrumentation (see Capture), and are labelled so.
package xtrace

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/athos/gitview/internal/evidence"
	"github.com/athos/gitview/internal/flow"
	"golang.org/x/exp/trace"
)

// RegionPrefix marks regions added by GitView's instrumentation.
const RegionPrefix = "gitview: "

type Frame struct {
	Func   string `json:"func"`
	File   string `json:"file,omitempty"`
	Line   int    `json:"line,omitempty"`
	Symbol string `json:"symbol,omitempty"`
}

// Goroutine states in segments.
const (
	Running  = "running"
	Runnable = "runnable"
	Waiting  = "waiting"
	Syscall  = "syscall"
)

type Segment struct {
	Start  int64  `json:"s"`
	End    int64  `json:"e"`
	State  string `json:"st"`
	Reason string `json:"r,omitempty"`
	Stack  int    `json:"k"` // index into Timeline.Stacks, -1 when none
	Proc   int64  `json:"p,omitempty"`
}

type Goroutine struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"` // start function
	Symbol     string    `json:"symbol,omitempty"`
	Created    int64     `json:"created"` // -1: existed when the trace started
	Ended      int64     `json:"ended"`   // -1: still alive when the trace ended
	Creator    int64     `json:"creator,omitempty"`
	CreatedAt  int       `json:"createStack"` // stack of the go statement, -1 when none
	Segments   []Segment `json:"segments"`
	RunningNs  int64     `json:"runningNs"`
	RunnableNs int64     `json:"runnableNs"`
	WaitingNs  int64     `json:"waitingNs"`
	SyscallNs  int64     `json:"syscallNs"`
	System     bool      `json:"system,omitempty"` // started in the runtime
}

type Span struct {
	Name  string `json:"name"`
	Start int64  `json:"s"`
	End   int64  `json:"e"`
	G     int64  `json:"g,omitempty"` // goroutine-scoped ranges (mark assist, sweep)
}

type Task struct {
	ID         uint64  `json:"id"`
	Parent     uint64  `json:"parent,omitempty"`
	Type       string  `json:"type"`
	Start      int64   `json:"s"`
	End        int64   `json:"e"` // -1 when it did not end in the trace
	Goroutines []int64 `json:"goroutines,omitempty"`
}

type Region struct {
	G            int64  `json:"g"`
	Task         uint64 `json:"task,omitempty"`
	Type         string `json:"type"`
	Start        int64  `json:"s"`
	End          int64  `json:"e"` // -1 when it did not end in the trace
	Stack        int    `json:"k"`
	Instrumented bool   `json:"instrumented,omitempty"`
	Symbol       string `json:"symbol,omitempty"`
}

type Log struct {
	T        int64  `json:"t"`
	G        int64  `json:"g"`
	Task     uint64 `json:"task,omitempty"`
	Category string `json:"category,omitempty"`
	Message  string `json:"message"`
	Stack    int    `json:"k"`
}

// Event is a notable instant: blocking, unblocking, goroutine creation.
type Event struct {
	T      int64  `json:"t"`
	G      int64  `json:"g"`
	Kind   string `json:"kind"` // block | unblock | create | syscall | end
	Detail string `json:"detail,omitempty"`
	Dur    int64  `json:"dur,omitempty"` // for blocks: how long the goroutine waited
	Stack  int    `json:"k"`
	Other  int64  `json:"other,omitempty"` // goroutine created or unblocked
}

type MetricPoint struct {
	T int64  `json:"t"`
	V uint64 `json:"v"`
}

type Stats struct {
	Events         int              `json:"events"`
	Goroutines     int              `json:"goroutines"`
	UserGoroutines int              `json:"userGoroutines"`
	GCCycles       int              `json:"gcCycles"`
	GCNs           int64            `json:"gcNs"`
	STWNs          int64            `json:"stwNs"`
	BlockedNs      map[string]int64 `json:"blockedNs"` // by reason
	Procs          int              `json:"procs"`
	StackSamples   int              `json:"stackSamples,omitempty"`
}

type Timeline struct {
	DurationNs int64                    `json:"durationNs"`
	StartWall  time.Time                `json:"startWall,omitempty"` // wall clock at offset 0, when the trace records it
	Goroutines []*Goroutine             `json:"goroutines"`
	GC         []Span                   `json:"gc"`
	Ranges     []Span                   `json:"ranges,omitempty"`
	Tasks      []*Task                  `json:"tasks,omitempty"`
	Regions    []*Region                `json:"regions,omitempty"`
	Logs       []Log                    `json:"logs,omitempty"`
	Events     []Event                  `json:"events,omitempty"`
	Metrics    map[string][]MetricPoint `json:"metrics,omitempty"`
	Stacks     [][]Frame                `json:"stacks"`
	Stats      Stats                    `json:"stats"`
	Truncated  bool                     `json:"truncated,omitempty"`
	Notes      []string                 `json:"notes,omitempty"`
	Artifact   evidence.Artifact        `json:"artifact"`
	Evidence   []evidence.Evidence      `json:"evidence"`
	Status     evidence.Completeness    `json:"status"`
}

// Limits keep the timeline small enough for the TUI and HTML reports.
type Limits struct {
	Segments int // default 200000
	Events   int // default 50000
	Regions  int // default 50000
	Metric   int // points per metric, default 2000
}

func (l Limits) withDefaults() Limits {
	if l.Segments <= 0 {
		l.Segments = 200000
	}
	if l.Events <= 0 {
		l.Events = 50000
	}
	if l.Regions <= 0 {
		l.Regions = 50000
	}
	if l.Metric <= 0 {
		l.Metric = 2000
	}
	return l
}

// Load reads a trace file.
func Load(path string, progs []*flow.Program, lim Limits) (*Timeline, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	t, err := Read(f, progs, lim)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	t.Artifact = evidence.Artifact{Kind: "trace", Name: filepath.Base(path), Path: path, Imported: true}
	if st, err := f.Stat(); err == nil {
		t.Artifact.CapturedAt = st.ModTime()
	}
	return t, nil
}

var metricNames = map[string]bool{
	"/memory/classes/heap/objects:bytes": true,
	"/gc/heap/goal:bytes":                true,
	"/sched/gomaxprocs:threads":          true,
	"/gc/heap/live:bytes":                true,
}

type builder struct {
	t       *Timeline
	lim     Limits
	m       *mapper
	base    trace.Time
	gs      map[int64]*Goroutine
	open    map[int64]*Segment // current segment per goroutine
	tasks   map[uint64]*Task
	regions map[int64][]*Region // open regions per goroutine (stack)
	ranges  map[string]*Span    // open ranges keyed by name+scope
	blockAt map[int64]int       // index in Events of each goroutine's open block event
	stacks  map[string]int
	segs    int
	last    int64
}

// Read parses a trace. Traces are untrusted input: malformed data yields
// an error, never a crash.
func Read(r io.Reader, progs []*flow.Program, lim Limits) (t *Timeline, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			t, err = nil, fmt.Errorf("invalid trace: %v", rec)
		}
	}()
	rd, err := trace.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("not a Go execution trace (Go 1.11+ format expected): %w", err)
	}
	b := &builder{t: &Timeline{Metrics: map[string][]MetricPoint{}, Stats: Stats{BlockedNs: map[string]int64{}}},
		lim: lim.withDefaults(), m: newMapper(progs), gs: map[int64]*Goroutine{}, open: map[int64]*Segment{},
		tasks: map[uint64]*Task{}, regions: map[int64][]*Region{}, ranges: map[string]*Span{}, stacks: map[string]int{}, blockAt: map[int64]int{}}
	first := true
	recorded := 0
	for {
		ev, err := rd.ReadEvent()
		if err == io.EOF {
			break
		}
		if err != nil {
			if b.t.Stats.Events == 0 {
				return nil, fmt.Errorf("invalid trace: %w", err)
			}
			b.t.Status.Limit("the trace ends with unreadable data: " + err.Error())
			break
		}
		if first {
			b.base, first = ev.Time(), false
		}
		if ev.Kind() != trace.EventSync {
			recorded++
		}
		b.event(ev)
	}
	if recorded == 0 {
		return nil, errors.New("the trace contains no recorded events")
	}
	b.finish()
	return b.t, nil
}

func (b *builder) ts(ev trace.Event) int64 {
	t := int64(ev.Time() - b.base)
	if t > b.last {
		b.last = t
	}
	return t
}

func (b *builder) stack(s trace.Stack) int {
	var frames []Frame
	for f := range s.Frames() {
		frames = append(frames, Frame{Func: f.Func, File: b.m.rel(f.File), Line: int(f.Line), Symbol: b.m.symbol(f.Func, f.File, int(f.Line))})
		if len(frames) == 32 {
			break
		}
	}
	if len(frames) == 0 {
		return -1
	}
	var key strings.Builder
	for _, f := range frames {
		fmt.Fprintf(&key, "%s:%s:%d|", f.Func, f.File, f.Line)
	}
	if i, ok := b.stacks[key.String()]; ok {
		return i
	}
	b.t.Stacks = append(b.t.Stacks, frames)
	b.stacks[key.String()] = len(b.t.Stacks) - 1
	return len(b.t.Stacks) - 1
}

func (b *builder) g(id int64) *Goroutine {
	g := b.gs[id]
	if g == nil {
		g = &Goroutine{ID: id, Created: -1, Ended: -1, CreatedAt: -1}
		b.gs[id] = g
	}
	return g
}

func (b *builder) addEvent(e Event) int {
	if len(b.t.Events) >= b.lim.Events {
		b.t.Truncated = true
		return -1
	}
	b.t.Events = append(b.t.Events, e)
	return len(b.t.Events) - 1
}

func stateName(s trace.GoState) string {
	switch s {
	case trace.GoRunning:
		return Running
	case trace.GoRunnable:
		return Runnable
	case trace.GoWaiting:
		return Waiting
	case trace.GoSyscall:
		return Syscall
	}
	return ""
}

func (b *builder) event(ev trace.Event) {
	b.t.Stats.Events++
	now := b.ts(ev)
	switch ev.Kind() {
	case trace.EventSync:
		// Go 1.25+ traces carry a wall-clock reading, which places the
		// trace on other timelines.
		if cs := ev.Sync().ClockSnapshot; cs != nil && b.t.StartWall.IsZero() {
			b.t.StartWall = cs.Wall.Add(-time.Duration(int64(cs.Trace) - int64(b.base)))
		}
	case trace.EventStateTransition:
		st := ev.StateTransition()
		switch st.Resource.Kind {
		case trace.ResourceGoroutine:
			b.goTransition(ev, st, now)
		case trace.ResourceProc:
			if p := int(st.Resource.Proc()) + 1; p > b.t.Stats.Procs {
				b.t.Stats.Procs = p
			}
		}
	case trace.EventRangeBegin, trace.EventRangeActive:
		r := ev.Range()
		key := r.Name + "|" + r.Scope.String()
		if _, open := b.ranges[key]; open {
			break // RangeActive repeats an open range at generation boundaries
		}
		sp := &Span{Name: r.Name, Start: now}
		if r.Scope.Kind == trace.ResourceGoroutine {
			sp.G = int64(r.Scope.Goroutine())
		}
		b.ranges[key] = sp
	case trace.EventRangeEnd:
		r := ev.Range()
		key := r.Name + "|" + r.Scope.String()
		sp := b.ranges[key]
		if sp == nil {
			sp = &Span{Name: r.Name, Start: 0}
			if r.Scope.Kind == trace.ResourceGoroutine {
				sp.G = int64(r.Scope.Goroutine())
			}
		}
		delete(b.ranges, key)
		sp.End = now
		b.closeRange(*sp)
	case trace.EventTaskBegin:
		tk := ev.Task()
		t := &Task{ID: uint64(tk.ID), Type: tk.Type, Start: now, End: -1}
		if tk.Parent != trace.NoTask && tk.Parent != trace.BackgroundTask {
			t.Parent = uint64(tk.Parent)
		}
		b.tasks[t.ID] = t
		b.t.Tasks = append(b.t.Tasks, t)
		b.taskGoroutine(t.ID, int64(ev.Goroutine()))
	case trace.EventTaskEnd:
		tk := ev.Task()
		t := b.tasks[uint64(tk.ID)]
		if t == nil {
			t = &Task{ID: uint64(tk.ID), Type: tk.Type, Start: 0}
			b.tasks[t.ID] = t
			b.t.Tasks = append(b.t.Tasks, t)
		}
		t.End = now
	case trace.EventRegionBegin:
		rg := ev.Region()
		g := int64(ev.Goroutine())
		reg := &Region{G: g, Task: uint64(rg.Task), Type: rg.Type, Start: now, End: -1, Stack: b.stack(ev.Stack())}
		if strings.HasPrefix(rg.Type, RegionPrefix) {
			reg.Instrumented = true
			reg.Symbol = strings.TrimPrefix(rg.Type, RegionPrefix)
		}
		b.regions[g] = append(b.regions[g], reg)
		if len(b.t.Regions) < b.lim.Regions {
			b.t.Regions = append(b.t.Regions, reg)
		} else {
			b.t.Truncated = true
		}
		if reg.Task != 0 {
			b.taskGoroutine(reg.Task, g)
		}
	case trace.EventRegionEnd:
		rg := ev.Region()
		g := int64(ev.Goroutine())
		open := b.regions[g]
		for i := len(open) - 1; i >= 0; i-- {
			if open[i].Type == rg.Type {
				open[i].End = now
				b.regions[g] = append(open[:i], open[i+1:]...)
				break
			}
		}
	case trace.EventLog:
		lg := ev.Log()
		if len(b.t.Logs) < b.lim.Events {
			b.t.Logs = append(b.t.Logs, Log{T: now, G: int64(ev.Goroutine()), Task: uint64(lg.Task), Category: lg.Category, Message: lg.Message, Stack: b.stack(ev.Stack())})
		}
	case trace.EventMetric:
		mt := ev.Metric()
		if metricNames[mt.Name] && mt.Value.Kind() == trace.ValueUint64 {
			pts := b.t.Metrics[mt.Name]
			if len(pts) < b.lim.Metric {
				b.t.Metrics[mt.Name] = append(pts, MetricPoint{T: now, V: mt.Value.Uint64()})
			}
		}
	case trace.EventStackSample:
		b.t.Stats.StackSamples++
	}
}

func (b *builder) taskGoroutine(task uint64, g int64) {
	t := b.tasks[task]
	if t == nil || g < 0 {
		return
	}
	for _, x := range t.Goroutines {
		if x == g {
			return
		}
	}
	t.Goroutines = append(t.Goroutines, g)
}

func (b *builder) closeRange(sp Span) {
	switch {
	case strings.HasPrefix(sp.Name, "stop-the-world"):
		// Scoped to the goroutine that stopped the world, but global in effect.
		b.t.GC = append(b.t.GC, sp)
		b.t.Stats.STWNs += sp.End - sp.Start
	case sp.G != 0:
		b.t.Ranges = append(b.t.Ranges, sp)
	default:
		b.t.GC = append(b.t.GC, sp)
		if strings.Contains(sp.Name, "GC") {
			b.t.Stats.GCNs += sp.End - sp.Start
			if strings.Contains(sp.Name, "mark") {
				b.t.Stats.GCCycles++
			}
		}
	}
}

func (b *builder) goTransition(ev trace.Event, st trace.StateTransition, now int64) {
	id := int64(st.Resource.Goroutine())
	from, to := st.Goroutine()
	g := b.g(id)
	// Status events at generation boundaries repeat the current state: they
	// are not transitions.
	if seg := b.open[id]; seg != nil && (from == to || from == trace.GoUndetermined) && seg.State == stateName(to) {
		return
	}
	// Close the current segment.
	if seg := b.open[id]; seg != nil {
		seg.End = now
		b.account(g, seg)
		if seg.State == Waiting {
			// Blocking event: record how long it lasted.
			if i, ok := b.blockAt[id]; ok {
				b.t.Events[i].Dur = now - b.t.Events[i].T
				delete(b.blockAt, id)
			}
		}
		delete(b.open, id)
	}
	switch {
	case from == trace.GoNotExist && to != trace.GoNotExist:
		g.Created = now
		g.Creator = int64(ev.Goroutine())
		g.CreatedAt = b.stack(ev.Stack())
		if s := b.stack(st.Stack); s >= 0 {
			top := b.t.Stacks[s][0]
			g.Name, g.Symbol = top.Func, top.Symbol
		}
		b.addEvent(Event{T: now, G: g.Creator, Kind: "create", Other: id, Stack: g.CreatedAt, Detail: "goroutine " + fmt.Sprint(id)})
	case to == trace.GoNotExist:
		g.Ended = now
	}
	name := stateName(to)
	if name == "" {
		return
	}
	seg := &Segment{Start: now, End: -1, State: name, Stack: -1}
	if to == trace.GoRunning {
		seg.Proc = int64(ev.Proc())
	}
	switch {
	case to == trace.GoWaiting:
		seg.Reason = st.Reason
		seg.Stack = b.stack(st.Stack)
		if seg.Stack < 0 {
			seg.Stack = b.stack(ev.Stack())
		}
		if i := b.addEvent(Event{T: now, G: id, Kind: "block", Detail: st.Reason, Stack: seg.Stack}); i >= 0 {
			b.blockAt[id] = i
		}
	case to == trace.GoRunnable && from == trace.GoWaiting:
		other := int64(ev.Goroutine())
		b.addEvent(Event{T: now, G: other, Kind: "unblock", Other: id, Stack: b.stack(ev.Stack()), Detail: "unblocks goroutine " + fmt.Sprint(id)})
	case to == trace.GoSyscall:
		seg.Stack = b.stack(ev.Stack())
		b.addEvent(Event{T: now, G: id, Kind: "syscall", Stack: seg.Stack})
	}
	if g.Name == "" && from == trace.GoUndetermined {
		// Goroutines alive when the trace started: name them by the
		// outermost frame of their current stack.
		g.Name, g.Symbol = outermost(b.t.Stacks, b.stack(st.Stack))
	}
	b.open[id] = seg
}

func (b *builder) account(g *Goroutine, seg *Segment) {
	d := seg.End - seg.Start
	switch seg.State {
	case Running:
		g.RunningNs += d
	case Runnable:
		g.RunnableNs += d
	case Waiting:
		g.WaitingNs += d
		reason := seg.Reason
		if reason == "" {
			reason = "(no reason recorded)"
		}
		b.t.Stats.BlockedNs[reason] += d
	case Syscall:
		g.SyscallNs += d
	}
	if b.segs >= b.lim.Segments {
		b.t.Truncated = true
		return
	}
	b.segs++
	g.Segments = append(g.Segments, *seg)
}

func (b *builder) finish() {
	t := b.t
	t.DurationNs = b.last
	for id, seg := range b.open {
		seg.End = b.last
		b.account(b.g(id), seg)
	}
	for _, sp := range b.ranges {
		sp.End = b.last
		b.closeRange(*sp)
	}
	for _, g := range b.gs {
		if g.Name == "" {
			// Existed before tracing started: name it by the outermost frame
			// of a stack recorded for it later.
			for _, seg := range g.Segments {
				if name, sym := outermost(t.Stacks, seg.Stack); name != "" {
					g.Name, g.Symbol = name, sym
					break
				}
			}
		}
		if g.Name == "" {
			g.Name = fmt.Sprintf("goroutine %d", g.ID)
		}
		g.System = strings.HasPrefix(g.Name, "runtime.") || strings.HasPrefix(g.Name, "runtime/") || strings.HasPrefix(g.Name, "internal/")
		t.Goroutines = append(t.Goroutines, g)
	}
	sort.Slice(t.Goroutines, func(i, j int) bool { return t.Goroutines[i].ID < t.Goroutines[j].ID })
	t.Stats.Goroutines = len(t.Goroutines)
	for _, g := range t.Goroutines {
		if !g.System {
			t.Stats.UserGoroutines++
		}
	}
	sort.Slice(t.GC, func(i, j int) bool { return t.GC[i].Start < t.GC[j].Start })
	if t.Truncated {
		t.Status.Limit("size limits reached: some segments, events or regions were dropped")
	}
	instrumented := false
	for _, r := range t.Regions {
		instrumented = instrumented || r.Instrumented
	}
	t.Evidence = []evidence.Evidence{{Kind: evidence.Runtime, Basis: evidence.BasisObserved, Source: "runtime/trace",
		Details: "events recorded by the Go runtime during this execution"}}
	t.Notes = append(t.Notes, "The trace records scheduling, blocking, GC, tasks, regions and logs; it does not record every function call.")
	if instrumented {
		t.Notes = append(t.Notes, "Regions named \""+strings.TrimSpace(RegionPrefix)+" …\" were added temporarily by GitView to measure function spans; their durations include the instrumentation overhead.")
		t.Evidence = append(t.Evidence, evidence.Evidence{Kind: evidence.Runtime, Basis: evidence.BasisObserved, Source: "GitView overlay instrumentation",
			Details: "trace.StartRegion inserted at the start of selected functions through go test -overlay; no file of the project was modified"})
	}
	t.Status.Done()
}

// outermost returns the outermost meaningful frame of a stack (skipping
// runtime.goexit and runtime.main, which start every goroutine or main).
func outermost(stacks [][]Frame, i int) (string, string) {
	if i < 0 || i >= len(stacks) {
		return "", ""
	}
	frames := stacks[i]
	for j := len(frames) - 1; j >= 0; j-- {
		if f := frames[j].Func; f != "runtime.goexit" && f != "runtime.main" {
			return f, frames[j].Symbol
		}
	}
	return "", ""
}

// mapper relates frames to module symbols (shared with Performance).
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

// SymbolTrace is what a trace shows about one module function.
type SymbolTrace struct {
	Symbol     string    `json:"symbol"`
	Goroutines []int64   `json:"goroutines,omitempty"` // started in the function
	Spans      []*Region `json:"spans,omitempty"`      // regions measuring the function
	SpanCount  int       `json:"spanCount"`
	SpanTotal  int64     `json:"spanTotalNs"`
	SpanMin    int64     `json:"spanMinNs"`
	SpanMax    int64     `json:"spanMaxNs"`
	Blocks     []Event   `json:"blocks,omitempty"` // blocking events whose stack includes the function
	Logs       []Log     `json:"logs,omitempty"`
	Found      bool      `json:"found"`
}

func (t *Timeline) stackHas(i int, id string) bool {
	if i < 0 || i >= len(t.Stacks) {
		return false
	}
	for _, f := range t.Stacks[i] {
		if f.Symbol == id {
			return true
		}
	}
	return false
}

// ForSymbol correlates trace events with a module function.
func (t *Timeline) ForSymbol(id string) SymbolTrace {
	st := SymbolTrace{Symbol: id}
	for _, g := range t.Goroutines {
		if g.Symbol == id {
			st.Goroutines = append(st.Goroutines, g.ID)
		}
	}
	for _, r := range t.Regions {
		if r.Symbol == id && r.End >= 0 {
			st.Spans = append(st.Spans, r)
			d := r.End - r.Start
			st.SpanTotal += d
			if st.SpanCount == 0 || d < st.SpanMin {
				st.SpanMin = d
			}
			st.SpanMax = max(st.SpanMax, d)
			st.SpanCount++
		}
	}
	for _, e := range t.Events {
		if e.Kind == "block" && t.stackHas(e.Stack, id) {
			st.Blocks = append(st.Blocks, e)
		}
	}
	for _, l := range t.Logs {
		if t.stackHas(l.Stack, id) {
			st.Logs = append(st.Logs, l)
		}
	}
	sort.Slice(st.Blocks, func(i, j int) bool { return st.Blocks[i].Dur > st.Blocks[j].Dur })
	st.Found = len(st.Goroutines) > 0 || st.SpanCount > 0 || len(st.Blocks) > 0 || len(st.Logs) > 0
	return st
}

// LongestBlocks returns the longest observed waits of non-runtime goroutines.
func (t *Timeline) LongestBlocks(n int) []Event {
	system := map[int64]bool{}
	for _, g := range t.Goroutines {
		system[g.ID] = g.System
	}
	var out []Event
	for _, e := range t.Events {
		if e.Kind == "block" && e.Dur > 0 && !system[e.G] {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dur > out[j].Dur })
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// Goroutine returns the goroutine with id.
func (t *Timeline) Goroutine(id int64) *Goroutine {
	for _, g := range t.Goroutines {
		if g.ID == id {
			return g
		}
	}
	return nil
}

// Compact returns a copy small enough to embed in a report: segments of
// runtime goroutines are dropped (their totals are kept), at most
// maxSegments segments and maxEvents events remain, and the cut is noted.
func (t *Timeline) Compact(maxSegments, maxEvents int) *Timeline {
	c := *t
	c.Goroutines = nil
	total := 0
	dropped := false
	for _, g := range t.Goroutines {
		cg := *g
		if g.System || total+len(g.Segments) > maxSegments {
			if len(g.Segments) > 0 {
				dropped = true
			}
			cg.Segments = nil
		}
		total += len(cg.Segments)
		c.Goroutines = append(c.Goroutines, &cg)
	}
	if len(t.Events) > maxEvents {
		c.Events = append([]Event(nil), t.Events[:maxEvents]...)
		dropped = true
	}
	if dropped {
		c.Truncated = true
		c.Notes = append(append([]string(nil), t.Notes...), "Embedded copy: segments of runtime goroutines and events beyond the size limit were omitted; totals are kept.")
	}
	return &c
}
