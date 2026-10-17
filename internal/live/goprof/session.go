package goprof

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/athos/gitview/internal/live/httpcol"
)

// Plan says what the profiler collects during a capture. Everything it
// does in the process is listed by Actions, for the user's consent.
type Plan struct {
	Endpoint *Endpoint `json:"-"`

	CPU       bool          `json:"cpu"`
	CPUWindow time.Duration `json:"cpuWindow"` // consecutive CPU profiles of this length (default 2 s)
	Heap      bool          `json:"heap"`      // heap/allocation snapshots before and after
	HeapGC    bool          `json:"heapGC"`    // run a GC before each heap snapshot (accurate allocation counts)
	Goroutine bool          `json:"goroutine"` // goroutine profile at the end of the active phase
	Mutex     bool          `json:"mutex"`
	Block     bool          `json:"block"`
	// SetRates enables mutex and block profiling for the capture (collector
	// only; restored when the capture ends).
	SetRates      bool          `json:"setRates"`
	MutexFraction int           `json:"mutexFraction"`
	BlockRate     int           `json:"blockRate"` // nanoseconds
	Trace         bool          `json:"trace"`
	TraceSeconds  int           `json:"traceSeconds"`
	Runtime       bool          `json:"runtime"` // poll runtime/metrics (collector only)
	RuntimeEvery  time.Duration `json:"runtimeEvery"`
	// MaxArtifact bounds each profile or trace (bytes, default 512 MiB).
	MaxArtifact int64 `json:"maxArtifact,omitempty"`
}

// Defaults fills unset parameters.
func (pl *Plan) Defaults() {
	if pl.CPUWindow <= 0 {
		pl.CPUWindow = 2 * time.Second
	}
	pl.CPUWindow = min(max(pl.CPUWindow, time.Second), 30*time.Second).Round(time.Second)
	if pl.TraceSeconds <= 0 {
		pl.TraceSeconds = 5
	}
	pl.TraceSeconds = min(pl.TraceSeconds, 30)
	if pl.MutexFraction <= 0 {
		pl.MutexFraction = 5
	}
	if pl.BlockRate <= 0 {
		pl.BlockRate = 10000
	}
	if pl.RuntimeEvery <= 0 {
		pl.RuntimeEvery = time.Second
	}
	if pl.MaxArtifact <= 0 || pl.MaxArtifact > 512<<20 {
		pl.MaxArtifact = 512 << 20
	}
}

// Empty reports whether the plan collects nothing.
func (pl *Plan) Empty() bool {
	return !pl.CPU && !pl.Heap && !pl.Goroutine && !pl.Mutex && !pl.Block && !pl.Trace && !pl.Runtime
}

// Validate checks the plan against its endpoint.
func (pl *Plan) Validate() error {
	if pl.Empty() {
		return nil
	}
	if pl.Endpoint == nil {
		return errors.New("no profiling endpoint: the process serves neither net/http/pprof nor GitView's collector")
	}
	if pl.Endpoint.coll == nil {
		if pl.SetRates {
			return errors.New("mutex and block profiling rates can only be changed through GitView's collector (net/http/pprof cannot change them)")
		}
		if pl.Runtime {
			return errors.New("runtime/metrics readings need GitView's collector")
		}
	}
	return nil
}

// Action is one thing the profiler will do in the process.
type Action struct {
	What    string `json:"what"`
	Request string `json:"request"`
	Effect  string `json:"effect"`
}

func (pl *Plan) path(name string) string {
	if pl.Endpoint != nil && pl.Endpoint.coll != nil {
		return "/debug/gitview/profile/" + name
	}
	if name == CPU {
		name = "profile"
	}
	return "/debug/pprof/" + name
}

// Actions lists what the plan does in the process, with its cost.
func (pl *Plan) Actions() []Action {
	var out []Action
	if pl.SetRates {
		out = append(out, Action{"Enable mutex and block profiling", fmt.Sprintf("POST /debug/gitview/profile/rates?mutex=%d&block=%d", pl.MutexFraction, pl.BlockRate),
			"every lock contention (1 in " + fmt.Sprint(pl.MutexFraction) + ") and blocking event above " + fmt.Sprint(pl.BlockRate) + " ns is recorded, which slows them; restored when the capture ends"})
	}
	if pl.Heap {
		eff := "reads the sampled heap profile (no stop-the-world)"
		q := ""
		if pl.HeapGC {
			eff = "runs a garbage collection in the process before each snapshot (outside the measured window), then reads the heap profile"
			q = "?gc=1"
		}
		out = append(out, Action{"Heap and allocation snapshots, before and after", "GET " + pl.path(Heap) + q, eff})
	}
	for _, k := range []string{Mutex, Block} {
		if k == Mutex && pl.Mutex || k == Block && pl.Block {
			out = append(out, Action{k + " profile snapshots, before and after", "GET " + pl.path(k), "reads the accumulated profile"})
		}
	}
	if pl.CPU {
		q := fmt.Sprintf("?seconds=%d", int(pl.CPUWindow/time.Second))
		out = append(out, Action{"CPU profile during the whole active phase", "GET " + pl.path(CPU) + q + ", repeated back to back",
			"samples every thread at 100 Hz (typically 1–5 % CPU overhead); a few milliseconds between windows are not sampled"})
	}
	if pl.Trace {
		out = append(out, Action{"Execution trace", fmt.Sprintf("GET %s?seconds=%d, at the start of the active phase", pl.path(Trace), pl.TraceSeconds),
			"records every scheduling, GC and blocking event (a few % overhead, large output)"})
	}
	if pl.Goroutine {
		out = append(out, Action{"Goroutine profile at the end of the active phase", "GET " + pl.path(Goroutine), "briefly stops the world, proportionally to the number of goroutines"})
	}
	if pl.Runtime {
		out = append(out, Action{"runtime/metrics readings", fmt.Sprintf("GET /debug/gitview/runtime every %s", pl.RuntimeEvery), "no stop-the-world; negligible"})
	}
	return out
}

// Warnings lists conflicts and caveats of the plan.
func (pl *Plan) Warnings() []string {
	var w []string
	if pl.CPU && pl.Trace {
		w = append(w, "CPU profile and execution trace run at the same time: their overheads add up, and the trace then also records the CPU samples. Run them in separate captures to measure latency without both.")
	}
	if (pl.Mutex || pl.Block) && !pl.SetRates {
		w = append(w, "Mutex and block profiles only contain data when the application enabled them (runtime.SetMutexProfileFraction / runtime.SetBlockProfileRate); an empty profile then means \"not enabled\", not \"no contention\".")
	}
	if pl.Heap && !pl.HeapGC {
		w = append(w, "Without a GC before each snapshot, the heap profile reflects allocations up to the last completed GC: the allocation delta may miss part of the capture.")
	}
	if pl.Endpoint != nil && pl.Endpoint.Exposed {
		w = append(w, "The pprof endpoint is reachable from other machines ("+pl.Endpoint.Listen+").")
	}
	return w
}

// Window is a time interval on the capture timeline.
type Window struct {
	Start time.Duration `json:"start"`
	End   time.Duration `json:"end"`
	File  string        `json:"file,omitempty"`
	Err   string        `json:"err,omitempty"`
}

// RuntimePoint is a runtime/metrics reading.
type RuntimePoint struct {
	T    time.Duration           `json:"t"`
	Snap httpcol.RuntimeSnapshot `json:"snap"`
}

// Result lists what was collected; profiles are files in the session's
// directory.
type Result struct {
	Endpoint    string         `json:"endpoint,omitempty"`
	Plan        Plan           `json:"plan"`
	CPU         []Window       `json:"cpu,omitempty"`
	HeapStart   *Window        `json:"heapStart,omitempty"`
	HeapEnd     *Window        `json:"heapEnd,omitempty"`
	MutexStart  *Window        `json:"mutexStart,omitempty"`
	MutexEnd    *Window        `json:"mutexEnd,omitempty"`
	BlockStart  *Window        `json:"blockStart,omitempty"`
	BlockEnd    *Window        `json:"blockEnd,omitempty"`
	Goroutine   *Window        `json:"goroutine,omitempty"`
	Trace       *Window        `json:"trace,omitempty"`
	Runtime     []RuntimePoint `json:"runtime,omitempty"`
	RuntimeErrs int            `json:"runtimeErrs,omitempty"`
	Errors      []string       `json:"errors,omitempty"`
}

// Session collects profiles during a capture.
type Session struct {
	pl     Plan
	dir    string
	t0     time.Time
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.Mutex
	res    Result
	stop   chan struct{}
	paused bool
	resume chan struct{}
	seq    int
}

// Start begins collecting: snapshots before the active phase, then the
// CPU windows, the trace and the runtime readings in the background.
// Files are written to dir; offsets are measured from t0.
func Start(ctx context.Context, pl Plan, dir string, t0 time.Time) (*Session, error) {
	pl.Defaults()
	if err := pl.Validate(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Session{pl: pl, dir: dir, t0: t0, stop: make(chan struct{})}
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.res.Plan = pl
	if pl.Empty() {
		return s, nil
	}
	s.res.Endpoint = pl.Endpoint.Label()
	if pl.SetRates {
		if err := pl.Endpoint.coll.Rates(s.ctx, pl.MutexFraction, pl.BlockRate); err != nil {
			s.fail("enabling mutex/block profiling: " + err.Error())
		}
	}
	if pl.Heap {
		s.res.HeapStart = s.snapshot(Heap, "heap-start.pb.gz", pl.HeapGC)
	}
	if pl.Mutex {
		s.res.MutexStart = s.snapshot(Mutex, "mutex-start.pb.gz", false)
	}
	if pl.Block {
		s.res.BlockStart = s.snapshot(Block, "block-start.pb.gz", false)
	}
	if pl.CPU {
		s.wg.Add(1)
		go s.cpuLoop()
	}
	if pl.Trace {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			w := s.fetch(Trace, pl.TraceSeconds, false, "trace.out")
			s.mu.Lock()
			s.res.Trace = w
			s.mu.Unlock()
		}()
	}
	if pl.Runtime {
		s.wg.Add(1)
		go s.runtimeLoop()
	}
	return s, nil
}

func (s *Session) now() time.Duration { return time.Since(s.t0) }

func (s *Session) fail(msg string) {
	s.mu.Lock()
	s.res.Errors = append(s.res.Errors, msg)
	s.mu.Unlock()
}

// fetch collects one profile into a file.
func (s *Session) fetch(kind string, seconds int, gc bool, name string) *Window {
	w := &Window{Start: s.now()}
	data, err := s.pl.Endpoint.Fetch(s.ctx, kind, seconds, gc, s.pl.MaxArtifact)
	w.End = s.now()
	if err == nil && len(data) == 0 {
		err = errors.New("empty reply")
	}
	if err == nil {
		err = os.WriteFile(filepath.Join(s.dir, name), data, 0o600)
	}
	if err != nil {
		w.Err = err.Error()
		s.fail(kind + ": " + err.Error())
		return w
	}
	w.File = name
	return w
}

func (s *Session) snapshot(kind, name string, gc bool) *Window { return s.fetch(kind, 0, gc, name) }

func (s *Session) cpuLoop() {
	defer s.wg.Done()
	secs := int(s.pl.CPUWindow / time.Second)
	for {
		select {
		case <-s.stop:
			return
		default:
		}
		s.mu.Lock()
		resume := s.resume
		s.mu.Unlock()
		if resume != nil {
			select {
			case <-resume:
			case <-s.stop:
				return
			}
		}
		s.mu.Lock()
		s.seq++
		name := fmt.Sprintf("cpu-%03d.pb.gz", s.seq)
		s.mu.Unlock()
		w := s.fetch(CPU, secs, false, name)
		s.mu.Lock()
		s.res.CPU = append(s.res.CPU, *w)
		s.mu.Unlock()
		if w.Err != "" {
			// A conflict or a dead endpoint does not get better by retrying
			// every window.
			return
		}
	}
}

func (s *Session) runtimeLoop() {
	defer s.wg.Done()
	t := time.NewTicker(s.pl.RuntimeEvery)
	defer t.Stop()
	read := func() {
		ctx, cancel := context.WithTimeout(s.ctx, 2*time.Second)
		snap, err := s.pl.Endpoint.coll.Runtime(ctx)
		cancel()
		s.mu.Lock()
		defer s.mu.Unlock()
		if err != nil {
			s.res.RuntimeErrs++
			return
		}
		s.res.Runtime = append(s.res.Runtime, RuntimePoint{T: s.now(), Snap: snap})
	}
	read()
	for {
		select {
		case <-s.stop:
			read()
			return
		case <-t.C:
			read()
		}
	}
}

// Pause stops starting new CPU windows (a window in progress completes).
func (s *Session) Pause() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.resume == nil {
		s.resume = make(chan struct{})
	}
}

// Resume undoes Pause.
func (s *Session) Resume() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.resume != nil {
		close(s.resume)
		s.resume = nil
	}
}

// Stop ends the active phase: it waits for the CPU window and the trace in
// progress, then takes the final snapshots. Cancelling ctx abandons what
// is still running.
func (s *Session) Stop(ctx context.Context) Result {
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
	s.Resume()
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		s.cancel()
		<-done
	}
	if !s.pl.Empty() && ctx.Err() == nil {
		if s.pl.Goroutine {
			s.res.Goroutine = s.snapshot(Goroutine, "goroutine.pb.gz", false)
		}
		if s.pl.Heap {
			s.res.HeapEnd = s.snapshot(Heap, "heap-end.pb.gz", s.pl.HeapGC)
		}
		if s.pl.Mutex {
			s.res.MutexEnd = s.snapshot(Mutex, "mutex-end.pb.gz", false)
		}
		if s.pl.Block {
			s.res.BlockEnd = s.snapshot(Block, "block-end.pb.gz", false)
		}
	}
	s.cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.res
}
