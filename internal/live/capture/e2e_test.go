package capture_test

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/live/capture"
	"github.com/athos/gitview/internal/live/goprof"
	"github.com/athos/gitview/internal/live/proc"
	"github.com/athos/gitview/internal/live/report"
	"github.com/athos/gitview/internal/live/srcmap"
)

var (
	buildOnce sync.Once
	appBin    string
	buildErr  error
)

func appDir() string {
	_, file, _, _ := runtime.Caller(0)
	d, _ := filepath.Abs(filepath.Join(filepath.Dir(file), "..", "testdata", "app"))
	return d
}

// buildApp compiles the fixture server once per test run.
func buildApp(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("builds and runs a server")
	}
	if proc.Supported() != nil {
		t.Skip(proc.Supported())
	}
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "gitview-liveapp-")
		if err != nil {
			buildErr = err
			return
		}
		appBin = filepath.Join(dir, "liveapp")
		cmd := exec.Command("go", "build", "-o", appBin, ".")
		cmd.Dir = appDir()
		cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOCACHE="+goBuildCache)
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("go build: %v\n%s", err, out)
		}
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return appBin
}

func TestMain(m *testing.M) {
	code := m.Run()
	if appBin != "" {
		os.RemoveAll(filepath.Dir(appBin))
	}
	os.Exit(code)
}

type app struct {
	cmd       *exec.Cmd
	pid       int
	addr      string
	collector string
	pprof     string
}

func startApp(t *testing.T, args ...string) *app {
	t.Helper()
	bin := buildApp(t)
	cmd := exec.Command(bin, args...)
	cmd.Env = os.Environ()
	out, _ := cmd.StdoutPipe()
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	a := &app{cmd: cmd, pid: cmd.Process.Pid}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	sc := bufio.NewScanner(out)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) != 2 {
			continue
		}
		switch f[0] {
		case "COLLECTOR":
			a.collector = f[1]
		case "PPROF":
			a.pprof = f[1]
		case "LISTEN":
			a.addr = f[1]
			go io.Copy(io.Discard, out)
			return a
		}
	}
	t.Fatal("the fixture server did not start")
	return nil
}

func env(t *testing.T) *capture.Store {
	t.Helper()
	t.Setenv("GITVIEW_CACHE_DIR", t.TempDir())
	privateRuntimeDir(t)
	st, err := capture.StoreFor("/repo")
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// waitActive waits for the active phase.
func waitActive(t *testing.T, s *capture.Session) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if s.Live(1).Phase == capture.PhaseActive {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("capture never became active")
}

func send(t *testing.T, base string, n int, path string) {
	t.Helper()
	for i := 0; i < n; i++ {
		resp, err := http.Get("http://" + base + path)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

func TestCaptureByPIDOnly(t *testing.T) {
	st := env(t)
	a := startApp(t)
	tg, err := capture.Prepare(context.Background(), proc.Default, a.pid)
	if err != nil {
		t.Fatal(err)
	}
	if !tg.Process.Go || len(tg.Endpoints) != 0 || tg.Collector != nil {
		t.Fatalf("target: go=%v endpoints=%v collector=%v", tg.Process.Go, tg.Endpoints, tg.Collector)
	}
	if !strings.Contains(strings.Join(tg.Notes, "\n"), "No profiling endpoint") {
		t.Errorf("notes: %v", tg.Notes)
	}
	s, err := capture.Start(context.Background(), st, tg, capture.Config{Label: "pid only", Interval: 50 * time.Millisecond, Baseline: 300 * time.Millisecond, Cooldown: 200 * time.Millisecond, Proxy: true})
	if err != nil {
		t.Fatal(err)
	}
	live := s.Live(5)
	if live.Proxy == "" || live.Target != a.addr {
		t.Fatalf("live: %+v", live)
	}
	waitActive(t, s)
	send(t, live.Proxy, 8, "/clients/7")
	send(t, live.Proxy, 2, "/health")
	time.Sleep(100 * time.Millisecond)
	s.Stop()
	c, err := s.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != capture.StatusComplete || len(c.Samples) < 8 || len(c.Requests) != 10 {
		t.Fatalf("capture: status=%s samples=%d requests=%d", c.Status, len(c.Samples), len(c.Requests))
	}
	an, err := report.Analyze(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(an.Routes) != 2 || an.Routes[0].Key != "GET /clients/{n}" || an.Routes[0].Count != 8 || an.Routes[0].KeyBasis != "normalized path" {
		t.Fatalf("routes: %+v", an.Routes)
	}
	r := an.Routes[0]
	if r.Latency.P50 < 8*time.Millisecond || !strings.HasPrefix(r.LatencyBasis, "client side") || r.Status[200] != 8 {
		t.Errorf("latency: %+v %s", r.Latency, r.LatencyBasis)
	}
	act := an.Process.Phases[1]
	if !act.HasCPU || act.CPUTime <= 0 || !act.HasMem || act.RSSMax == 0 || !act.HasIO {
		t.Errorf("active phase: %+v", act)
	}
	lim := strings.Join(an.Limitations, "\n")
	for _, want := range []string{"Network bytes", "No profile was collected", "Without GitView's collector"} {
		if !strings.Contains(lim, want) {
			t.Errorf("limitations missing %q:\n%s", want, lim)
		}
	}
	for _, m := range r.Metrics {
		if m.Basis != report.Unavailable && m.Scope == report.ScopeRequest {
			t.Errorf("request metric without a source of attribution: %+v", m)
		}
	}
	list, _ := st.List()
	if len(list) != 1 || list[0].ID != c.ID || list[0].Requests != 10 {
		t.Errorf("history: %+v", list)
	}
	again, err := st.Load(c.ID[:15])
	if err != nil || again.ID != c.ID || len(again.Requests) != 10 {
		t.Errorf("reload: %v", err)
	}
}

func TestCaptureWithCollectorAndProfiles(t *testing.T) {
	st := env(t)
	a := startApp(t, "-collector", "-pprof")
	tg, err := capture.Prepare(context.Background(), proc.Default, a.pid)
	if err != nil {
		t.Fatal(err)
	}
	if tg.Collector == nil || len(tg.Endpoints) != 2 || tg.Endpoint().Kind != "collector" {
		t.Fatalf("target: collector=%v endpoints=%d notes=%v", tg.Collector, len(tg.Endpoints), tg.Notes)
	}
	if tg.DefaultTarget() != a.addr {
		t.Errorf("default target %s, want %s", tg.DefaultTarget(), a.addr)
	}
	plan := goprof.Plan{CPU: true, CPUWindow: time.Second, Heap: true, HeapGC: true, Goroutine: true, Mutex: true, Block: true, SetRates: true, Trace: true, TraceSeconds: 1, Runtime: true, RuntimeEvery: 200 * time.Millisecond}
	s, err := capture.Start(context.Background(), st, tg, capture.Config{Interval: 50 * time.Millisecond, Baseline: 300 * time.Millisecond, Cooldown: 300 * time.Millisecond, Proxy: true, Collector: true, Profile: plan})
	if err != nil {
		t.Fatal(err)
	}
	waitActive(t, s)
	send(t, s.Live(1).Proxy, 12, "/clients/42")
	time.Sleep(300 * time.Millisecond)
	s.Stop()
	c, err := s.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Profiles.Errors) != 0 {
		t.Errorf("profiling errors: %v", c.Profiles.Errors)
	}
	p, err := flow.Load(context.Background(), appDir(), appDir())
	if err != nil {
		t.Fatal(err)
	}
	an, err := report.Analyze(c, srcmap.New(appDir(), []*flow.Program{p}))
	if err != nil {
		t.Fatal(err)
	}
	if len(an.Routes) != 1 {
		t.Fatalf("routes: %+v", an.Routes)
	}
	r := an.Routes[0]
	if r.Key != "GET /clients/{id}" || r.KeyBasis != "route pattern" || r.Count != 12 || r.Sources["proxy"] != 12 || r.Sources["collector"] != 12 {
		t.Errorf("route: key=%s basis=%s count=%d sources=%v", r.Key, r.KeyBasis, r.Count, r.Sources)
	}
	if !strings.HasPrefix(r.LatencyBasis, "server side") || r.Proxy == nil || r.Proxy.P50 < r.Latency.P50 {
		t.Errorf("latency: %s server=%v proxy=%v", r.LatencyBasis, r.Latency, r.Proxy)
	}
	if r.Route == nil || r.Route.Conf != "exact" || r.Route.Handler == nil || !strings.HasSuffix(r.Route.Handler.Name, "getClient") {
		t.Errorf("route mapping: %+v", r.Route)
	}
	if len(r.Statements) != 1 {
		t.Fatalf("statements: %+v", r.Statements)
	}
	sq := r.Statements[0]
	if sq.Query != "SELECT name FROM clients WHERE id = $1 AND status = ?" || sq.PerRequest != 1 || sq.Match == nil || sq.Match.Conf != "exact" || !strings.HasSuffix(sq.Match.Func.Name, "ClientName") {
		t.Errorf("statement: %+v match=%+v", sq, sq.Match)
	}
	var cpuMetric *report.Metric
	for i := range r.Metrics {
		if r.Metrics[i].Name == "CPU per request" {
			cpuMetric = &r.Metrics[i]
		}
	}
	if cpuMetric == nil || cpuMetric.Basis != report.Sampled || cpuMetric.Value <= 0 {
		t.Errorf("cpu per request: %+v", cpuMetric)
	}
	if r.Flame == nil || len(r.HotPath) == 0 {
		t.Errorf("no flame/hot path for the route")
	} else {
		found := false
		for _, st := range r.HotPath {
			found = found || strings.HasSuffix(st.Name, "main.burn")
		}
		if !found {
			t.Errorf("hot path: %+v", r.HotPath)
		}
	}
	if an.Runtime == nil || len(an.Runtime.Metrics) == 0 {
		t.Errorf("runtime metrics missing")
	}
	if an.Trace == nil || len(an.Trace.Requests) == 0 || r.Trace == nil || r.Trace.RunningNs <= 0 {
		t.Errorf("trace: %+v route trace %+v", an.Trace, r.Trace)
	}
	if an.Profiles == nil || an.Profiles.Allocs == nil || an.Profiles.Goroutines == nil || an.Profiles.Mutex == nil || an.Profiles.Block == nil {
		t.Errorf("profiles: %+v", an.Profiles)
	}
	// The collector was disarmed and the rates restored.
	if info, err := tg.Collector.Info(context.Background()); err != nil || info.Armed {
		t.Errorf("collector still armed: %+v %v", info, err)
	}
}

func capture1(t *testing.T, st *capture.Store, extra string, n int) *report.Analysis {
	t.Helper()
	args := []string{}
	if extra != "" {
		args = append(args, "-extra", extra)
	}
	a := startApp(t, args...)
	tg, err := capture.Prepare(context.Background(), proc.Default, a.pid)
	if err != nil {
		t.Fatal(err)
	}
	s, err := capture.Start(context.Background(), st, tg, capture.Config{Interval: 100 * time.Millisecond, Baseline: 300 * time.Millisecond, Cooldown: 100 * time.Millisecond, Proxy: true})
	if err != nil {
		t.Fatal(err)
	}
	waitActive(t, s)
	send(t, s.Live(1).Proxy, n, "/clients/1")
	s.Stop()
	c, err := s.Wait()
	if err != nil {
		t.Fatal(err)
	}
	an, err := report.Analyze(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	return an
}

func TestCompareRuns(t *testing.T) {
	st := env(t)
	before := capture1(t, st, "", 10)
	after := capture1(t, st, "25ms", 10)
	cmp := report.Compare(before, after)
	if !cmp.Comparable || len(cmp.Routes) != 1 {
		t.Fatalf("comparison: %+v", cmp)
	}
	rd := cmp.Routes[0]
	if rd.Verdict != report.Slower || float64(rd.P) >= 0.05 || len(cmp.Regressions) != 1 {
		t.Errorf("verdict %s p=%v change=%v reasons=%v", rd.Verdict, rd.P, rd.Change, rd.Reasons)
	}
	few := capture1(t, st, "", 3)
	cmp = report.Compare(before, few)
	if v := cmp.Routes[0]; v.Verdict != report.Inconclusive || !strings.Contains(strings.Join(v.Reasons, ";"), "too few requests") {
		t.Errorf("small sample: %+v", v)
	}
}

func TestProcessExitDuringCapture(t *testing.T) {
	st := env(t)
	a := startApp(t)
	tg, err := capture.Prepare(context.Background(), proc.Default, a.pid)
	if err != nil {
		t.Fatal(err)
	}
	s, err := capture.Start(context.Background(), st, tg, capture.Config{Interval: 50 * time.Millisecond, Baseline: 100 * time.Millisecond, Proxy: true})
	if err != nil {
		t.Fatal(err)
	}
	waitActive(t, s)
	send(t, s.Live(1).Proxy, 2, "/health")
	a.cmd.Process.Kill()
	a.cmd.Wait()
	select {
	case <-s.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("capture did not end when the process exited")
	}
	c, err := s.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != capture.StatusExited || !strings.Contains(c.StatusNote, "exited") || len(c.Requests) != 2 {
		t.Errorf("status %q note %q requests %d", c.Status, c.StatusNote, len(c.Requests))
	}
}

func TestCancelSavesNothing(t *testing.T) {
	st := env(t)
	a := startApp(t)
	tg, _ := capture.Prepare(context.Background(), proc.Default, a.pid)
	s, err := capture.Start(context.Background(), st, tg, capture.Config{Baseline: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	s.Cancel()
	if _, err := s.Wait(); err == nil {
		t.Error("cancelled capture returned")
	}
	if list, _ := st.List(); len(list) != 0 {
		t.Errorf("history after cancel: %+v", list)
	}
}

func TestCaptureWithOpenTelemetrySpans(t *testing.T) {
	st := env(t)
	a := startApp(t)
	tg, err := capture.Prepare(context.Background(), proc.Default, a.pid)
	if err != nil {
		t.Fatal(err)
	}
	s, err := capture.Start(context.Background(), st, tg, capture.Config{Interval: 50 * time.Millisecond, Baseline: 200 * time.Millisecond, Cooldown: 100 * time.Millisecond, OTLP: true, OTLPListen: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	waitActive(t, s)
	otlp := s.Live(1).OTLP
	now := time.Now()
	ns := func(t time.Time) string { return fmt.Sprint(t.UnixNano()) }
	body := fmt.Sprintf(`{"resourceSpans":[{"resource":{"attributes":[{"key":"process.pid","value":{"intValue":"%d"}}]},"scopeSpans":[{"spans":[
	 {"traceId":"0af7651916cd43dd8448eb211c80319c","spanId":"b7ad6b7169203331","name":"GET /clients/{id}","kind":2,"startTimeUnixNano":"%s","endTimeUnixNano":"%s",
	  "attributes":[{"key":"http.request.method","value":{"stringValue":"GET"}},{"key":"http.route","value":{"stringValue":"/clients/{id}"}},{"key":"url.path","value":{"stringValue":"/clients/9"}},{"key":"http.response.status_code","value":{"intValue":"200"}}]},
	 {"traceId":"0af7651916cd43dd8448eb211c80319c","spanId":"c7ad6b7169203331","parentSpanId":"b7ad6b7169203331","name":"SELECT","kind":3,"startTimeUnixNano":"%s","endTimeUnixNano":"%s",
	  "attributes":[{"key":"db.query.text","value":{"stringValue":"SELECT name FROM clients WHERE id = 9"}}]}]}]},
	 {"resource":{"attributes":[{"key":"process.pid","value":{"intValue":"1"}}]},"scopeSpans":[{"spans":[
	 {"traceId":"1af7651916cd43dd8448eb211c80319c","spanId":"d7ad6b7169203331","name":"GET /other","kind":2,"startTimeUnixNano":"%s","endTimeUnixNano":"%s",
	  "attributes":[{"key":"http.request.method","value":{"stringValue":"GET"}}]}]}]}]}`,
		a.pid, ns(now), ns(now.Add(12*time.Millisecond)), ns(now.Add(2*time.Millisecond)), ns(now.Add(7*time.Millisecond)), ns(now), ns(now.Add(time.Millisecond)))
	resp, err := http.Post("http://"+otlp+"/v1/traces", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	s.Stop()
	c, err := s.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Requests) != 1 || c.OTelSpans != 3 {
		t.Fatalf("requests %+v spans %d", c.Requests, c.OTelSpans)
	}
	r := c.Requests[0]
	if r.Key != "GET /clients/{id}" || r.ServerDur != 12*time.Millisecond || len(r.SQL) != 1 || r.SQL[0].Query != "SELECT name FROM clients WHERE id = ?" || r.Sources[0] != "otel" {
		t.Errorf("request: %+v", r)
	}
	an, _ := report.Analyze(c, nil)
	if len(an.Routes) != 1 || !strings.Contains(an.Routes[0].LatencyBasis, "OpenTelemetry") {
		t.Errorf("route: %+v", an.Routes)
	}
}

// privateRuntimeDir points XDG_RUNTIME_DIR at a private (0700) temporary
// directory and the user cache at another, so that collector announcements
// never reach the real home directory.
func privateRuntimeDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	os.Chmod(dir, 0o700)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("GOCACHE", goBuildCache) // the go tool keeps its real cache
	return dir
}

// goBuildCache is the Go build cache as it was before the tests redirected
// the user cache directory (a cold cache would rebuild the standard library).
var goBuildCache = func() string {
	if c := os.Getenv("GOCACHE"); c != "" {
		return c
	}
	d, err := os.UserCacheDir()
	if err != nil {
		return os.TempDir()
	}
	return filepath.Join(d, "go-build")
}()

func TestContainerProcessNeedsExplicitTarget(t *testing.T) {
	st := env(t)
	a := startApp(t)
	tg, err := capture.Prepare(context.Background(), proc.Default, a.pid)
	if err != nil {
		t.Fatal(err)
	}
	tg.Process.OwnNetNS = true
	if tg.DefaultTarget() != "" {
		t.Error("a port of another network namespace was chosen")
	}
	if _, err := capture.Start(context.Background(), st, tg, capture.Config{Proxy: true}); err == nil || !strings.Contains(err.Error(), "--target") {
		t.Errorf("start: %v", err)
	}
}

// TestTimedCaptureWithCollector ends by its duration (not Stop): the
// collector polling must stop before the final read (no race, no
// duplicated pages).
func TestTimedCaptureWithCollector(t *testing.T) {
	st := env(t)
	a := startApp(t, "-collector")
	tg, err := capture.Prepare(context.Background(), proc.Default, a.pid)
	if err != nil || tg.Collector == nil {
		t.Fatalf("prepare: %v", err)
	}
	s, err := capture.Start(context.Background(), st, tg, capture.Config{Interval: 50 * time.Millisecond, Baseline: 100 * time.Millisecond, Duration: 900 * time.Millisecond, Cooldown: 100 * time.Millisecond, Collector: true})
	if err != nil {
		t.Fatal(err)
	}
	waitActive(t, s)
	send(t, a.addr, 5, "/clients/3")
	c, err := s.Wait()
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, r := range c.Requests {
		if ids[r.ID] {
			t.Errorf("request %s twice", r.ID)
		}
		ids[r.ID] = true
	}
	if len(c.Requests) != 5 {
		t.Errorf("requests %d", len(c.Requests))
	}
}
