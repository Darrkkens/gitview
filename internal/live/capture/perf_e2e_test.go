package capture_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/athos/gitview/internal/live/capture"
	"github.com/athos/gitview/internal/live/linuxperf"
	"github.com/athos/gitview/internal/live/proc"
	"github.com/athos/gitview/internal/live/report"
)

// perfTarget prepares the fixture server (burning CPU in main.burn on each
// request) and skips the test when perf cannot sample it here.
func perfTarget(t *testing.T) (*app, *capture.Target) {
	t.Helper()
	a := startApp(t, "-extra", "4ms")
	tg, err := capture.Prepare(context.Background(), proc.Default, a.pid)
	if err != nil {
		t.Fatal(err)
	}
	if !tg.Perf.Available {
		t.Skip("Linux perf unavailable: " + tg.Perf.Reason)
	}
	return a, tg
}

// tempDir points os.TempDir at a fresh directory, to see what perf leaves
// (after the fixture server is built: it is built in os.TempDir).
func tempDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	t.Setenv("TMPDIR", d)
	return d
}

func leftovers(t *testing.T, dir string) {
	t.Helper()
	if m, _ := filepath.Glob(filepath.Join(dir, "gitview-perf-*")); len(m) != 0 {
		t.Errorf("perf's temporary directories were not removed: %v", m)
	}
}

// /proc and perf sample the same process at the same time, on one timeline.
func TestCaptureWithPerf(t *testing.T) {
	st := env(t)
	_, tg := perfTarget(t)
	tmp := tempDir(t)
	s, err := capture.Start(context.Background(), st, tg, capture.Config{Interval: 100 * time.Millisecond, Baseline: 300 * time.Millisecond,
		Cooldown: 200 * time.Millisecond, Proxy: true, Perf: true, PerfFreq: 499})
	if err != nil {
		t.Fatal(err)
	}
	waitActive(t, s)
	send(t, s.Live(1).Proxy, 60, "/clients/3")
	if l := s.Live(1); l.PerfSamples == 0 || l.Perf != "recording" {
		t.Errorf("live perf state: %q, %d samples", l.Perf, l.PerfSamples)
	}
	s.Stop()
	c, err := s.Wait()
	if err != nil {
		t.Fatal(err)
	}
	leftovers(t, tmp)
	if c.Perf == nil || c.Perf.File == "" || c.Perf.Error != "" || !slices.Contains(c.Sources, "perf") {
		t.Fatalf("perf result %+v, sources %v", c.Perf, c.Sources)
	}
	if c.Settings.Perf != "on" || len(c.Samples) < 5 {
		t.Errorf("settings %q, %d /proc samples", c.Settings.Perf, len(c.Samples))
	}
	an, err := report.Analyze(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	if an.Perf == nil || an.Perf.CPU == nil || an.Perf.Samples == 0 {
		t.Fatalf("perf report %+v", an.Perf)
	}
	burn := false
	for _, f := range an.Perf.CPU.Funcs {
		burn = burn || f.Name == "main.burn" && strings.HasSuffix(f.File, "main.go")
	}
	if !burn {
		t.Errorf("main.burn (with its file) not among the sampled functions")
	}
	// The perf samples fall in the same places as the requests on the
	// capture's timeline: most of them while requests were in flight.
	co := an.Correlation
	if co == nil || !co.HasPerf || co.BusyPerf <= co.IdlePerf {
		t.Errorf("correlation %+v", co)
	}
	with := 0
	for _, r := range an.Requests {
		if r.PerfSamples < 0 {
			t.Fatalf("request without perf correlation: %+v", r)
		}
		with += r.PerfSamples
	}
	if with == 0 {
		t.Error("no perf sample during any request")
	}
	if len(an.CPUViews) != 3 || an.CPUViews[0].Basis != report.Measured || an.CPUViews[1].Basis != report.Sampled || an.CPUViews[2].Basis != report.Unavailable {
		t.Errorf("CPU views %+v", an.CPUViews)
	}
	if !strings.Contains(strings.Join(an.Limitations, "\n"), "CPU by function comes from Linux perf") {
		t.Errorf("limitations: %v", an.Limitations)
	}
}

// perf missing or forbidden: the capture goes on with /proc and says why.
func TestCapturePerfUnavailable(t *testing.T) {
	st := env(t)
	a := startApp(t)
	tg, err := capture.Prepare(context.Background(), proc.Default, a.pid)
	if err != nil {
		t.Fatal(err)
	}
	tg.Perf = linuxperf.Capability{Reason: "Linux perf is not installed"}
	s, err := capture.Start(context.Background(), st, tg, capture.Config{Interval: 50 * time.Millisecond, Baseline: 100 * time.Millisecond, Cooldown: 50 * time.Millisecond, Perf: true})
	if err != nil {
		t.Fatal(err)
	}
	waitActive(t, s)
	if l := s.Live(1); !strings.HasPrefix(l.Perf, "unavailable") {
		t.Errorf("live perf state %q", l.Perf)
	}
	s.Stop()
	c, err := s.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != capture.StatusComplete || c.Perf != nil || slices.Contains(c.Sources, "perf") {
		t.Fatalf("status %s perf %+v sources %v", c.Status, c.Perf, c.Sources)
	}
	if !strings.Contains(strings.Join(c.Notes, "\n"), "Linux perf was not used: Linux perf is not installed") {
		t.Errorf("notes %v", c.Notes)
	}
}

// Ctrl+C twice: nothing is kept, perf's files included.
func TestCancelWithPerf(t *testing.T) {
	st := env(t)
	_, tg := perfTarget(t)
	tmp := tempDir(t)
	s, err := capture.Start(context.Background(), st, tg, capture.Config{Baseline: time.Second, Perf: true})
	if err != nil {
		t.Fatal(err)
	}
	s.Cancel()
	if _, err := s.Wait(); err == nil {
		t.Error("cancelled capture returned")
	}
	leftovers(t, tmp)
	if list, _ := st.List(); len(list) != 0 {
		t.Errorf("history after cancel: %+v", list)
	}
}

// The process exits during the capture: perf stops with it and its
// samples are kept.
func TestProcessExitWithPerf(t *testing.T) {
	st := env(t)
	a, tg := perfTarget(t)
	tmp := tempDir(t)
	s, err := capture.Start(context.Background(), st, tg, capture.Config{Interval: 50 * time.Millisecond, Baseline: 100 * time.Millisecond, Proxy: true, Perf: true, PerfFreq: 499})
	if err != nil {
		t.Fatal(err)
	}
	waitActive(t, s)
	send(t, s.Live(1).Proxy, 20, "/clients/1")
	a.cmd.Process.Kill()
	a.cmd.Wait()
	select {
	case <-s.Done():
	case <-time.After(20 * time.Second):
		t.Fatal("capture did not end when the process exited")
	}
	c, err := s.Wait()
	if err != nil {
		t.Fatal(err)
	}
	leftovers(t, tmp)
	if c.Status != capture.StatusExited || c.Perf == nil || !c.Perf.Early || c.Perf.File == "" {
		t.Fatalf("status %s perf %+v", c.Status, c.Perf)
	}
	if _, err := os.Stat(filepath.Join(c.Dir, linuxperf.DataFile)); err != nil {
		t.Error(err)
	}
}
