package goprof

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"path/filepath"
	rpprof "runtime/pprof"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/athos/gitview/internal/live/proc"
)

// pprofServer serves net/http/pprof from this test process on addr.
func pprofServer(t *testing.T, addr string) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return ln.Addr().String()
}

func self(t *testing.T) *proc.Process {
	t.Helper()
	if proc.Supported() != nil {
		t.Skip(proc.Supported())
	}
	p, err := proc.Default.Inspect(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func burn(stop <-chan struct{}) {
	x := 0
	for {
		select {
		case <-stop:
			return
		default:
			for i := 0; i < 1e5; i++ {
				x += i
			}
			_ = make([]byte, 4096)
		}
	}
}

func TestDiscoverAndCollect(t *testing.T) {
	addr := pprofServer(t, "127.0.0.1:0")
	p := self(t)
	eps, notes := Discover(context.Background(), proc.Default, p, nil)
	var ep *Endpoint
	for _, e := range eps {
		if e.Addr == addr {
			ep = e
		}
	}
	if ep == nil || ep.Kind != "pprof" || ep.Exposed {
		t.Fatalf("endpoint not found: %+v %v", eps, notes)
	}

	pl := Plan{Endpoint: ep, CPU: true, CPUWindow: time.Second, Heap: true, HeapGC: true, Goroutine: true, Trace: true, TraceSeconds: 1}
	if w := pl.Warnings(); len(w) == 0 || !strings.Contains(w[0], "CPU profile and execution trace") {
		t.Errorf("warnings: %v", w)
	}
	acts := pl.Actions()
	if len(acts) != 4 || !strings.Contains(acts[0].Request, "/debug/pprof/heap?gc=1") || !strings.Contains(acts[1].Request, "/debug/pprof/profile?seconds=1") {
		t.Errorf("actions: %+v", acts)
	}
	dir := t.TempDir()
	t0 := time.Now()
	stop := make(chan struct{})
	go burn(stop)
	s, err := Start(context.Background(), pl, dir, t0)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond)
	res := s.Stop(context.Background())
	close(stop)
	if len(res.Errors) != 0 {
		t.Fatalf("errors: %v", res.Errors)
	}
	if len(res.CPU) != 2 || res.CPU[0].File != "cpu-001.pb.gz" || res.CPU[1].Start < res.CPU[0].End {
		t.Errorf("cpu windows: %+v", res.CPU)
	}
	for _, w := range []*Window{res.HeapStart, res.HeapEnd, res.Goroutine, res.Trace} {
		if w == nil || w.File == "" {
			t.Fatalf("missing artifact: %+v", res)
		}
		if _, err := os.Stat(filepath.Join(dir, w.File)); err != nil {
			t.Error(err)
		}
	}
	prof, err := Analyze(dir, &res, Active{Start: 0, End: time.Since(t0)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if prof.CPU == nil || prof.CPU.Total == 0 || prof.CPUCoverage < 0.5 {
		t.Errorf("cpu: %+v coverage %.2f", prof.CPU, prof.CPUCoverage)
	}
	found := false
	for _, f := range prof.CPU.Funcs {
		found = found || strings.HasSuffix(f.Name, "goprof.burn")
	}
	if !found {
		t.Error("burn not in the CPU profile")
	}
	if prof.Allocs == nil || prof.Allocs.Total <= 0 || prof.Inuse == nil || prof.Goroutines == nil {
		t.Errorf("heap/goroutines: allocs=%v inuse=%v g=%v", prof.Allocs, prof.Inuse, prof.Goroutines)
	}
}

func TestLabelledCPUIsAttributed(t *testing.T) {
	addr := pprofServer(t, "127.0.0.1:0")
	p := self(t)
	eps, _ := Discover(context.Background(), proc.Default, p, nil)
	var ep *Endpoint
	for _, e := range eps {
		if e.Addr == addr {
			ep = e
		}
	}
	dir := t.TempDir()
	stop := make(chan struct{})
	go rpprof.Do(context.Background(), rpprof.Labels(LabelTraceID, "req-1"), func(context.Context) { burn(stop) })
	s, err := Start(context.Background(), Plan{Endpoint: ep, CPU: true, CPUWindow: time.Second}, dir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	res := s.Stop(context.Background())
	close(stop)
	prof, err := Analyze(dir, &res, Active{End: time.Second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if prof.CPUByTrace["req-1"] < int64(200*time.Millisecond) || prof.CPULabelled != prof.CPUByTrace["req-1"] {
		t.Errorf("labelled cpu: %v (labelled %d, unlabelled %d)", prof.CPUByTrace, prof.CPULabelled, prof.CPUUnlabelled)
	}
	cpu, _ := CPUProfile(dir, &res)
	r, err := ForTraces(cpu, map[string]bool{"req-1": true}, nil)
	if err != nil || r.Total != prof.CPUByTrace["req-1"] {
		t.Errorf("filtered report: %v %v", r, err)
	}
}

func TestConflictingCPUProfile(t *testing.T) {
	addr := pprofServer(t, "127.0.0.1:0")
	ep := &Endpoint{Kind: "pprof", Addr: addr, hc: newClient()}
	if err := rpprof.StartCPUProfile(io.Discard); err != nil {
		t.Skip("CPU profiler busy:", err)
	}
	_, err := ep.Fetch(context.Background(), CPU, 1, false, 1<<20)
	rpprof.StopCPUProfile()
	if !errors.Is(err, ErrConflict) {
		t.Errorf("conflict not detected: %v", err)
	}
}

func TestPprofOfAnotherProcessIsRefused(t *testing.T) {
	addr := pprofServer(t, "127.0.0.1:0")
	_, port, _ := net.SplitHostPort(addr)
	p := self(t)
	// A fake procfs whose process has another command line but owns the
	// port: the endpoint must not be used.
	root := t.TempDir()
	pid := os.Getpid()
	dir := filepath.Join(root, strconv.Itoa(pid))
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "cmdline"), []byte("other\x00"), 0o644)
	q := *p
	q.Ports = []proc.Port{{Proto: "tcp", Addr: "127.0.0.1", Port: mustAtoi(port), Loopback: true}}
	eps, notes := Discover(context.Background(), proc.FS{Root: root}, &q, nil)
	if len(eps) != 0 || !strings.Contains(strings.Join(notes, "\n"), "answers for another process") {
		t.Errorf("endpoints %+v notes %v", eps, notes)
	}
}

func TestPlanValidation(t *testing.T) {
	ep := &Endpoint{Kind: "pprof", Addr: "127.0.0.1:1"}
	if err := (&Plan{Endpoint: ep, Mutex: true, SetRates: true}).Validate(); err == nil {
		t.Error("rates through pprof accepted")
	}
	if err := (&Plan{Endpoint: ep, Runtime: true}).Validate(); err == nil {
		t.Error("runtime metrics through pprof accepted")
	}
	if err := (&Plan{CPU: true}).Validate(); err == nil {
		t.Error("plan without endpoint accepted")
	}
	pl := Plan{Endpoint: ep, Mutex: true, Heap: true}
	w := strings.Join(pl.Warnings(), "\n")
	if !strings.Contains(w, "not enabled") || !strings.Contains(w, "last completed GC") {
		t.Errorf("warnings: %s", w)
	}
	exposed := &Endpoint{Kind: "pprof", Addr: "127.0.0.1:6060", Exposed: true, Listen: "0.0.0.0:6060"}
	if w := (&Plan{Endpoint: exposed, CPU: true}).Warnings(); len(w) != 1 || !strings.Contains(w[0], "0.0.0.0:6060") {
		t.Errorf("exposure warning: %v", w)
	}
}

func mustAtoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func TestCoverageIsClippedToTheActivePhase(t *testing.T) {
	ac := Active{Start: 10 * time.Second, End: 20 * time.Second, Paused: []Window{{Start: 12 * time.Second, End: 14 * time.Second}}}
	if got := ac.overlap(8*time.Second, 22*time.Second); got != 8*time.Second {
		t.Errorf("overlap %v, want 8s (10 s active minus a 2 s pause)", got)
	}
	r := &Result{CPU: []Window{{Start: 10 * time.Second, End: 12 * time.Second, File: "cpu-001.pb.gz"}, {Start: 12 * time.Second, End: 14 * time.Second, Err: "conflict"}}}
	if !r.Covered(10500*time.Millisecond, 11*time.Second) || r.Covered(11500*time.Millisecond, 12500*time.Millisecond) || r.Covered(12500*time.Millisecond, 13*time.Second) {
		t.Error("Covered")
	}
}
