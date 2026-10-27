package capture

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/live/goprof"
	"github.com/athos/gitview/internal/live/httpcol"
	"github.com/athos/gitview/internal/live/linuxperf"
	"github.com/athos/gitview/internal/live/proc"
)

// EnvDisable set to "off" disables the runtime inspector.
const EnvDisable = "GITVIEW_RUNTIME"

// Disabled reports whether the runtime inspector was turned off.
func Disabled() bool { return os.Getenv(EnvDisable) == "off" }

// Target is a process prepared for capture: what it is and which
// collectors it offers.
type Target struct {
	FS        proc.FS
	Process   *proc.Process
	Collector *httpcol.Collector
	CollInfo  httpcol.CollectorInfo
	Endpoints []*goprof.Endpoint
	// Perf says whether Linux perf can sample the process's CPU from the
	// outside (no endpoint needed).
	Perf  linuxperf.Capability
	Notes []string
}

// Prepare inspects a process: /proc, the collector's announcement and
// net/http/pprof on its own ports (GET /debug/pprof/ and its cmdline, to
// verify the endpoint belongs to the process).
func Prepare(ctx context.Context, fs proc.FS, pid int) (*Target, error) {
	if err := proc.Supported(); err != nil {
		return nil, err
	}
	p, err := fs.Inspect(pid)
	if err != nil {
		if errors.Is(err, proc.ErrNoProcess) {
			return nil, fmt.Errorf("no process with PID %d", pid)
		}
		return nil, err
	}
	t := &Target{FS: fs, Process: p}
	if !p.Go && p.GoKnown {
		t.Notes = append(t.Notes, "The executable has no Go build information: it is not a Go program (or it was stripped); process metrics still work.")
	}
	if !p.GoKnown {
		t.Notes = append(t.Notes, "The executable cannot be read (another user's process): Go detection, ports and some metrics are unavailable.")
	}
	d := httpcol.FindCollector(ctx, p)
	t.Collector, t.CollInfo = d.Collector, d.Info
	t.Notes = append(t.Notes, d.Notes...)
	eps, notes := goprof.Discover(ctx, fs, p, t.Collector)
	t.Endpoints = eps
	t.Notes = append(t.Notes, notes...)
	t.Perf = linuxperf.Detect(ctx, linuxperf.Env{}, p.UID)
	return t, nil
}

// Endpoint returns the preferred profiling endpoint (the collector's,
// which can also set profiling rates), or nil.
func (t *Target) Endpoint() *goprof.Endpoint { return t.EndpointFor(true) }

// EndpointFor returns the profiling endpoint to use: the collector's only
// when useCollector is set, else net/http/pprof; nil when there is none.
func (t *Target) EndpointFor(useCollector bool) *goprof.Endpoint {
	if useCollector {
		for _, e := range t.Endpoints {
			if e.Kind == "collector" {
				return e
			}
		}
	}
	for _, e := range t.Endpoints {
		if e.Kind == "pprof" {
			return e
		}
	}
	return nil
}

// DefaultTarget picks the application port for the proxy: the first port
// that is neither the collector's own listener nor a port serving only
// net/http/pprof; failing that, the first port.
func (t *Target) DefaultTarget() string {
	if t.Process.OwnNetNS {
		// Its ports live in another network namespace: 127.0.0.1:port here
		// is another process (or nothing). The user must give an address.
		return ""
	}
	skip := map[string]bool{}
	if t.Collector != nil && !t.Collector.Mounted {
		skip[t.Collector.Addr] = true
	}
	for _, e := range t.Endpoints {
		if e.Kind == "pprof" {
			skip[e.Addr] = true
		}
	}
	for _, pt := range t.Process.Ports {
		if !skip[pt.Dial()] {
			return pt.Dial()
		}
	}
	if len(t.Process.Ports) > 0 {
		return t.Process.Ports[0].Dial()
	}
	return ""
}

// Config is what the user chose for a capture.
type Config struct {
	Label    string
	Interval time.Duration // process sampling (default 250 ms)
	Baseline time.Duration // before the active phase (default 2 s)
	Duration time.Duration // active phase; 0 = until Stop
	Cooldown time.Duration // after the active phase (default 2 s)

	Proxy       bool
	ProxyListen string // default 127.0.0.1:0
	Target      string // application address (default Target.DefaultTarget)
	Collector   bool   // use the collector when the process has one
	OTLP        bool
	OTLPListen  string
	// OTLPAnyProcess accepts spans whose resource does not name the process
	// (no process.pid): they may come from another local service.
	OTLPAnyProcess bool
	Endpoint       string // route of interest (request key), informative

	Profile goprof.Plan // Endpoint set by the caller
	// Perf samples the CPU with Linux perf during the whole capture when
	// the target allows it (Target.Perf); otherwise the capture goes on
	// without it, with a note.
	Perf     bool
	PerfFreq int    // samples per second per thread (default 99)
	Repo     string // repository root, recorded for display
}

// Phase names.
const (
	PhaseBaseline = "baseline"
	PhaseActive   = "active"
	PhasePaused   = "paused"
	PhaseCooldown = "cooldown"
	PhaseDone     = "done"
)

// maxRequests bounds the requests kept per source.
const maxRequests = 50000

// Session is a capture in progress.
type Session struct {
	store  *Store
	tg     *Target
	cfg    Config
	t0     time.Time
	ctx    context.Context
	cancel context.CancelFunc

	mu          sync.Mutex
	cap         *Capture
	phase       string
	pauseStart  time.Duration
	proxyReqs   []httpcol.Request
	collReqs    []httpcol.Request
	otelSpans   []httpcol.OTelSpan
	dropped     int
	proxy       *httpcol.Proxy
	otlp        *httpcol.OTLPReceiver
	prof        *goprof.Session
	perf        *linuxperf.Session
	perfT0      int64 // CLOCK_MONOTONIC at t0
	perfNote    string
	sampler     *proc.Sampler
	exitErr     error
	collLast    int64
	orphLast    int64
	pageMax     int
	pollErrs    int
	pollErr     string
	pollStop    chan struct{}
	pollDone    chan struct{}
	lastSpan    time.Time
	stopCh      chan struct{}
	stopOnce    sync.Once
	cancelled   bool
	done        chan struct{}
	err         error
	activeTimer *time.Timer
}

func clampDur(d, def, lo, hi time.Duration) time.Duration {
	if d <= 0 {
		d = def
	}
	return min(max(d, lo), hi)
}

// Start begins a capture: it starts sampling the process, the proxy, the
// collector's recording and the OTLP receiver at once (so the user can
// send requests), and the active phase after the baseline.
func Start(ctx context.Context, store *Store, tg *Target, cfg Config) (*Session, error) {
	if Disabled() {
		return nil, errors.New("the runtime inspector is disabled (" + EnvDisable + "=off)")
	}
	lim := store.Limits
	cfg.Interval = clampDur(cfg.Interval, lim.DefaultEvery, lim.MinInterval, lim.MaxInterval)
	cfg.Baseline = clampDur(cfg.Baseline, 2*time.Second, 0, time.Minute)
	cfg.Cooldown = clampDur(cfg.Cooldown, 2*time.Second, 0, time.Minute)
	if cfg.Duration > lim.MaxDuration {
		cfg.Duration = lim.MaxDuration
	}
	if cfg.Profile.Endpoint == nil && !cfg.Profile.Empty() {
		cfg.Profile.Endpoint = tg.EndpointFor(cfg.Collector)
	}
	// A profile or trace may take at most a quarter of the history's space.
	cfg.Profile.MaxArtifact = min(int64(512<<20), lim.MaxBytes/4)
	cfg.Profile.Defaults()
	if err := cfg.Profile.Validate(); err != nil {
		return nil, err
	}
	if cfg.Proxy && cfg.Target == "" {
		cfg.Target = tg.DefaultTarget()
		if cfg.Target == "" {
			if tg.Process.OwnNetNS {
				return nil, errors.New("the process runs in its own network namespace (a container): give the address that reaches it from here (--target)")
			}
			return nil, errors.New("the process listens on no TCP port: there is no application address for the proxy")
		}
	}
	now := time.Now()
	perfT0 := linuxperf.Monotonic()
	id, dir, err := store.NewDir(now)
	if err != nil {
		return nil, err
	}
	p := tg.Process
	s := &Session{store: store, tg: tg, cfg: cfg, t0: now, perfT0: perfT0, phase: PhaseBaseline, stopCh: make(chan struct{}), done: make(chan struct{}),
		pollStop: make(chan struct{}), pollDone: make(chan struct{}), pageMax: 200}
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.cap = &Capture{
		Version: FormatVersion, ID: id, Label: cfg.Label, Created: now.Round(0), Dir: dir, Repo: flow.DisplayPath(cfg.Repo),
		Process: ProcessInfo{PID: p.PID, Comm: p.Comm, Exe: flow.DisplayPath(p.Exe), ExeDeleted: p.ExeDeleted, StartTicks: p.StartTicks, Started: p.Started,
			Go: p.Go, GoVersion: p.GoVersion, MainModule: p.MainModule, MainVersion: p.MainVersion, VCSRevision: p.VCSRevision, VCSModified: p.VCSModified,
			Ports: p.Ports, Unavailable: p.Access.Reasons, CPUs: runtime.NumCPU()},
		Settings: Settings{Interval: cfg.Interval, Baseline: cfg.Baseline, Duration: cfg.Duration, Cooldown: cfg.Cooldown, MaxDuration: lim.MaxDuration, Endpoint: cfg.Endpoint, Profile: cfg.Profile, Perf: onOff(cfg.Perf)},
		Sources:  []string{"proc"},
		Notes:    append([]string(nil), tg.Notes...),
	}
	s.sampler = tg.FS.NewSampler(p, now)
	if smp, err := s.sampler.Sample(time.Now()); err != nil {
		os.RemoveAll(dir)
		return nil, fmt.Errorf("process %d: %w", p.PID, err)
	} else {
		s.cap.Samples = append(s.cap.Samples, smp)
	}
	fail := func(err error) (*Session, error) {
		s.shutdownCollectors()
		os.RemoveAll(dir)
		return nil, err
	}
	if cfg.Proxy {
		px, err := httpcol.StartProxy(httpcol.ProxyOptions{Listen: cfg.ProxyListen, Target: cfg.Target, Traceparent: cfg.OTLP, Sink: s.addProxy})
		if err != nil {
			return fail(fmt.Errorf("proxy: %w", err))
		}
		s.proxy = px
		s.cap.Settings.Proxy, s.cap.Settings.Target = px.Addr(), cfg.Target
		s.cap.Sources = append(s.cap.Sources, string(httpcol.SourceProxy))
	}
	if cfg.Collector && tg.Collector != nil {
		ttl := cfg.Baseline + lim.MaxDuration + cfg.Cooldown + time.Minute
		info, err := tg.Collector.Arm(s.ctx, httpcol.ArmOptions{TTL: ttl, Labels: true, Tasks: true, Runtime: true})
		if err != nil {
			return fail(fmt.Errorf("arming the collector: %w", err))
		}
		s.collLast = info.Last
		s.cap.Settings.Collector = tg.Collector.Addr
		s.cap.Sources = append(s.cap.Sources, string(httpcol.SourceCollector))
	}
	if cfg.OTLP {
		o, err := httpcol.StartOTLP(cfg.OTLPListen, s.addSpans)
		if err != nil {
			return fail(fmt.Errorf("OTLP receiver: %w", err))
		}
		s.otlp = o
		s.cap.Settings.OTLP = o.Addr()
		s.cap.Sources = append(s.cap.Sources, string(httpcol.SourceOTel))
	}
	if cfg.Perf {
		s.startPerf()
	}
	go s.sampleLoop()
	if s.cap.Settings.Collector != "" {
		go s.pollLoop()
	} else {
		close(s.pollDone)
	}
	go s.run()
	return s, nil
}

func (s *Session) now() time.Duration { return time.Since(s.t0) }

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// startPerf attaches Linux perf to the process for the whole capture. A
// perf that is missing or not allowed is noted; the capture goes on.
func (s *Session) startPerf() {
	tg := s.tg
	if !tg.Perf.Available {
		s.perfNote = "unavailable: " + tg.Perf.Reason
		s.cap.Notes = append(s.cap.Notes, "Linux perf was not used: "+tg.Perf.Reason+".")
		return
	}
	p := tg.Process
	ps, err := linuxperf.Start(s.ctx, linuxperf.Options{Cap: tg.Perf, PID: p.PID, Freq: s.cfg.PerfFreq,
		Verify: func() error { return tg.FS.Verify(p) }, T0: s.perfT0, Dir: s.cap.Dir, Proc: tg.FS.Root}, s.now())
	if err != nil {
		s.perfNote = "not started: " + err.Error()
		s.cap.Warnings = append(s.cap.Warnings, "Linux perf could not sample the process: "+err.Error()+".")
		return
	}
	s.perf = ps
	s.perfNote = "recording"
	s.cap.Warnings = append(s.cap.Warnings, tg.Perf.Warnings...)
	s.cap.Sources = append(s.cap.Sources, "perf")
}

// stopPerf ends perf and processes its samples (perf script).
func (s *Session) stopPerf() {
	s.mu.Lock()
	ps := s.perf
	s.perfNote = "processing the samples"
	s.mu.Unlock()
	if ps == nil {
		return
	}
	res := ps.Stop(context.Background(), s.now())
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cap.Perf = &res
	s.perfNote = "done"
	if res.Error != "" {
		s.cap.Warnings = append(s.cap.Warnings, "Linux perf: "+res.Error+".")
	}
}

func (s *Session) addProxy(r httpcol.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.proxyReqs) >= maxRequests {
		s.dropped++
		return
	}
	s.proxyReqs = append(s.proxyReqs, r)
}

func (s *Session) addSpans(sp []httpcol.OTelSpan) {
	drift := s.wallDrift()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastSpan = time.Now()
	if len(s.otelSpans)+len(sp) > 20*maxRequests {
		s.dropped += len(sp)
		return
	}
	for _, x := range sp {
		x.Start, x.End = x.Start.Add(-drift), x.End.Add(-drift)
		s.otelSpans = append(s.otelSpans, x)
	}
}

// wallDrift is how far the wall clock moved from the monotonic clock since
// the capture started (an NTP step, for instance). Times read from the
// application (collector, OpenTelemetry) are wall-clock times: shifting
// them by the drift places them on the capture's monotonic timeline.
func (s *Session) wallDrift() time.Duration {
	now := time.Now()
	return now.Round(0).Sub(s.t0.Round(0)) - now.Sub(s.t0)
}

func (s *Session) sampleLoop() {
	t := time.NewTicker(s.cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-s.ctx.Done():
			return
		case now := <-t.C:
			smp, err := s.sampler.Sample(now)
			s.mu.Lock()
			if err != nil {
				if s.exitErr == nil && (errors.Is(err, proc.ErrExited) || errors.Is(err, proc.ErrRestarted)) {
					s.exitErr = err
					s.mu.Unlock()
					s.Stop()
					return
				}
				s.mu.Unlock()
				continue
			}
			if s.phase != PhaseDone {
				s.cap.Samples = append(s.cap.Samples, smp)
			}
			s.mu.Unlock()
		}
	}
}

// poll reads the requests the collector completed since the last read,
// page after page. A reply too large to read makes the pages smaller.
func (s *Session) poll(ctx context.Context) {
	for i := 0; i < 50; i++ {
		s.mu.Lock()
		since, osince, max := s.collLast, s.orphLast, s.pageMax
		s.mu.Unlock()
		pg, err := s.tg.Collector.Requests(ctx, since, osince, max)
		if err != nil {
			s.mu.Lock()
			s.pollErrs++
			s.pollErr = err.Error()
			if s.pageMax > 10 {
				s.pageMax /= 2
			}
			s.mu.Unlock()
			return
		}
		drift := s.wallDrift()
		s.mu.Lock()
		s.collLast, s.orphLast = pg.Last, pg.OrphansLast
		s.cap.Lost += pg.Lost
		for _, r := range pg.Requests {
			r.Start = r.Start.Add(-drift)
			if len(s.collReqs) < maxRequests {
				s.collReqs = append(s.collReqs, r)
			} else {
				s.dropped++
			}
		}
		for _, o := range pg.Orphans {
			o.Start = o.Start.Add(-drift)
			if len(s.cap.Orphans) < maxRequests {
				s.cap.Orphans = append(s.cap.Orphans, o)
			}
		}
		s.mu.Unlock()
		if !pg.More {
			return
		}
	}
}

// pollLoop reads the collector every 500 ms until the capture finishes:
// after the active phase too, while the profiles end and the cooldown runs,
// so that no backlog builds up.
func (s *Session) pollLoop() {
	defer close(s.pollDone)
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-s.pollStop:
			return
		case <-s.ctx.Done():
			return
		case <-t.C:
			ctx, cancel := context.WithTimeout(s.ctx, 3*time.Second)
			s.poll(ctx)
			cancel()
		}
	}
}

func (s *Session) wait(d time.Duration) bool {
	if d <= 0 {
		return true
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-s.stopCh:
		return false
	case <-s.ctx.Done():
		return false
	}
}

func (s *Session) setPhase(ph string) {
	s.mu.Lock()
	s.phase = ph
	s.mu.Unlock()
}

func (s *Session) run() {
	defer close(s.done)
	stopped := !s.wait(s.cfg.Baseline)

	// Active phase. Snapshots taken by the profiler at its start (and at
	// its end) stay outside the measured window.
	if !s.cfg.Profile.Empty() && !stopped {
		pr, err := goprof.Start(s.ctx, s.cfg.Profile, s.cap.Dir, s.t0)
		if err != nil {
			s.note("profiling not started: " + err.Error())
		} else {
			s.mu.Lock()
			s.prof = pr
			s.mu.Unlock()
		}
	}
	s.mu.Lock()
	s.phase = PhaseActive
	s.cap.Phases.ActiveStart = s.now()
	s.mu.Unlock()
	if !stopped {
		limit := s.store.Limits.MaxDuration
		if s.cfg.Duration > 0 {
			limit = s.cfg.Duration
		}
		if s.wait(limit) && s.cfg.Duration == 0 {
			s.note(fmt.Sprintf("Stopped at the maximum capture duration (%s).", limit))
		}
	}
	s.mu.Lock()
	if s.phase == PhasePaused {
		s.cap.Phases.Paused = append(s.cap.Phases.Paused, goprof.Window{Start: s.pauseStart, End: s.now()})
	}
	s.cap.Phases.ActiveEnd = s.now()
	s.phase = PhaseCooldown
	prof := s.prof
	exited := s.exitErr != nil
	s.mu.Unlock()

	if prof != nil {
		ctx, cancel := context.WithTimeout(s.ctx, 45*time.Second)
		if exited {
			cancel()
		}
		res := prof.Stop(ctx)
		cancel()
		s.mu.Lock()
		s.cap.Profiles = &res
		if res.Trace != nil {
			s.cap.TraceAt = s.t0.Add(res.Trace.Start).Round(0)
		}
		s.mu.Unlock()
	}
	if !exited && !s.isCancelled() {
		// The process keeps being sampled after the active phase.
		t := time.NewTimer(s.cfg.Cooldown)
		select {
		case <-t.C:
		case <-s.ctx.Done():
		}
		t.Stop()
	}
	s.finish()
}

func (s *Session) isCancelled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancelled
}

func (s *Session) note(n string) {
	s.mu.Lock()
	s.cap.Notes = append(s.cap.Notes, n)
	s.mu.Unlock()
}

func (s *Session) shutdownCollectors() {
	if s.proxy != nil {
		s.proxy.Close()
	}
	if s.otlp != nil {
		s.otlp.Close()
	}
	if s.tg.Collector != nil && s.cap != nil && s.cap.Settings.Collector != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		s.tg.Collector.Disarm(ctx)
		cancel()
	}
}

func (s *Session) finish() {
	close(s.pollStop)
	<-s.pollDone
	if s.isCancelled() {
		// Nothing is kept: disarm the collector and close the listeners at once.
		if s.perf != nil {
			s.perf.Abort()
		}
		s.shutdownCollectors()
		s.mu.Lock()
		defer s.mu.Unlock()
		s.phase = PhaseDone
		os.RemoveAll(s.cap.Dir)
		s.err = errors.New("capture cancelled; nothing was saved")
		return
	}
	if s.cap.Settings.Collector != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		s.poll(ctx)
		cancel()
	}
	if s.otlp != nil {
		// OpenTelemetry SDKs export in batches (every 5 s by default): wait
		// until a batch arrived after the active phase and a second passed
		// without spans, at most 6 s after the active phase.
		s.mu.Lock()
		activeEnd := s.t0.Add(s.cap.Phases.ActiveEnd)
		s.mu.Unlock()
		for time.Since(activeEnd) < 6*time.Second && s.ctx.Err() == nil {
			s.mu.Lock()
			last := s.lastSpan
			s.mu.Unlock()
			if last.After(activeEnd) && time.Since(last) > time.Second {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	s.stopPerf()
	s.shutdownCollectors()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.phase = PhaseDone
	s.cap.Phases.End = s.now()
	otel, unidentified := httpcol.AssembleOTel(s.otelSpans, s.tg.Process.PID, s.cfg.OTLPAnyProcess)
	s.cap.OTelSpans = len(s.otelSpans)
	if unidentified > 0 {
		s.cap.Warnings = append(s.cap.Warnings, fmt.Sprintf("%d OpenTelemetry spans did not say which process sent them (resource attribute process.pid) and were ignored: they may come from another service. Add the process resource (resource.WithProcessPID) or accept them explicitly.", unidentified))
	}
	if s.otlp != nil {
		s.cap.Notes = append(s.cap.Notes, "Spans the application exported after the receiver closed are not in the capture.")
	}
	reqs := httpcol.Merge(s.proxyReqs, s.collReqs, otel)
	for i := range reqs {
		reqs[i].Off = reqs[i].Start.Sub(s.t0)
		reqs[i].Start = reqs[i].Start.Round(0)
	}
	s.cap.Requests = reqs
	if s.dropped > 0 {
		s.cap.Warnings = append(s.cap.Warnings, fmt.Sprintf("%d requests or spans were dropped (more than %d per source).", s.dropped, maxRequests))
	}
	if s.cap.Lost > 0 {
		s.cap.Warnings = append(s.cap.Warnings, fmt.Sprintf("The collector's buffer overflowed: %d requests were lost before GitView read them.", s.cap.Lost))
	}
	if s.pollErrs > 0 {
		s.cap.Warnings = append(s.cap.Warnings, fmt.Sprintf("%d reads of the collector failed (last: %s): requests may be missing.", s.pollErrs, s.pollErr))
	}
	s.cap.Status = StatusComplete
	switch {
	case errors.Is(s.exitErr, proc.ErrExited):
		s.cap.Status = StatusExited
		s.cap.StatusNote = fmt.Sprintf("Process %d exited %.1f s into the capture.", s.tg.Process.PID, s.cap.Samples[len(s.cap.Samples)-1].T.Seconds())
		if succ := s.tg.FS.FindSuccessor(s.tg.Process); succ != nil {
			s.cap.Status = StatusRestarted
			s.cap.StatusNote += fmt.Sprintf(" The same executable is running again as PID %d.", succ.PID)
		}
	case errors.Is(s.exitErr, proc.ErrRestarted):
		s.cap.Status = StatusRestarted
		s.cap.StatusNote = fmt.Sprintf("PID %d now belongs to another process (the original exited).", s.tg.Process.PID)
	}
	if dropped := enforceSize(s.cap, s.store.Limits.MaxBytes); len(dropped) > 0 {
		s.cap.Warnings = append(s.cap.Warnings, fmt.Sprintf("The capture exceeded the storage limit (%d MiB): %s removed.", s.store.Limits.MaxBytes>>20, strings.Join(dropped, ", ")))
	}
	if err := s.store.Save(s.cap); err != nil {
		s.err = err
	}
}

// Pause suspends the active phase: samples and requests keep being
// recorded but are excluded from the analysis; no new CPU window starts.
func (s *Session) Pause() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.phase != PhaseActive {
		return false
	}
	s.phase, s.pauseStart = PhasePaused, s.now()
	if s.prof != nil {
		s.prof.Pause()
	}
	return true
}

// Resume ends a pause.
func (s *Session) Resume() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.phase != PhasePaused {
		return false
	}
	s.cap.Phases.Paused = append(s.cap.Phases.Paused, goprof.Window{Start: s.pauseStart, End: s.now()})
	s.phase = PhaseActive
	if s.prof != nil {
		s.prof.Resume()
	}
	return true
}

// Stop ends the active phase (or the baseline); the cooldown follows.
func (s *Session) Stop() { s.stopOnce.Do(func() { close(s.stopCh) }) }

// Cancel abandons the capture; nothing is saved.
func (s *Session) Cancel() {
	s.mu.Lock()
	s.cancelled = true
	s.mu.Unlock()
	s.Stop()
	s.cancel()
}

// Done is closed when the capture is finished and saved.
func (s *Session) Done() <-chan struct{} { return s.done }

// Wait blocks until the capture is saved and returns it.
func (s *Session) Wait() (*Capture, error) {
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	return s.cap, nil
}

// Live is the state of a capture in progress, for the interface.
type Live struct {
	ID        string            `json:"id"`
	Phase     string            `json:"phase"`
	Elapsed   time.Duration     `json:"elapsed"`
	Active    time.Duration     `json:"active"` // time spent in the active phase so far
	Last      proc.Sample       `json:"last"`
	Samples   []proc.Sample     `json:"-"` // the most recent samples
	Requests  int               `json:"requests"`
	Recent    []httpcol.Request `json:"recent"`
	Proxy     string            `json:"proxy,omitempty"`
	Target    string            `json:"target,omitempty"`
	Collector string            `json:"collector,omitempty"`
	OTLP      string            `json:"otlp,omitempty"`
	Profiling bool              `json:"profiling"`
	// PerfSamples counts the CPU samples Linux perf wrote so far (exact);
	// Perf is perf's state ("" when not requested).
	PerfSamples int64  `json:"perfSamples"`
	Perf        string `json:"perf,omitempty"`
	Exited      string `json:"exited,omitempty"`
}

// Live returns the current state; it keeps the last n samples and
// requests.
func (s *Session) Live(n int) Live {
	s.mu.Lock()
	defer s.mu.Unlock()
	l := Live{ID: s.cap.ID, Phase: s.phase, Elapsed: s.now(), Proxy: s.cap.Settings.Proxy, Target: s.cap.Settings.Target,
		Collector: s.cap.Settings.Collector, OTLP: s.cap.Settings.OTLP, Profiling: s.prof != nil, Perf: s.perfNote}
	if s.perf != nil {
		l.PerfSamples = s.perf.Samples()
	}
	if s.phase == PhaseActive || s.phase == PhasePaused {
		l.Active = s.now() - s.cap.Phases.ActiveStart
	} else if s.cap.Phases.ActiveEnd > 0 {
		l.Active = s.cap.Phases.ActiveEnd - s.cap.Phases.ActiveStart
	}
	if k := len(s.cap.Samples); k > 0 {
		l.Last = s.cap.Samples[k-1]
		l.Samples = append([]proc.Sample(nil), s.cap.Samples[max(0, k-n):]...)
	}
	// Requests seen by any source, by ID.
	seen := map[string]bool{}
	var recent []httpcol.Request
	for _, list := range [][]httpcol.Request{s.proxyReqs, s.collReqs} {
		for _, r := range list {
			if r.ID != "" && seen[r.ID] {
				continue
			}
			seen[r.ID] = true
			recent = append(recent, r)
		}
	}
	l.Requests = len(recent)
	if len(recent) > n {
		recent = recent[len(recent)-n:]
	}
	for i := range recent {
		recent[i].Off = recent[i].Start.Sub(s.t0)
	}
	l.Recent = recent
	if s.exitErr != nil {
		l.Exited = s.exitErr.Error()
	}
	return l
}
