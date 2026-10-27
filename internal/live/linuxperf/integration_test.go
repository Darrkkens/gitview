package linuxperf

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// busyProgram is a Go program that burns CPU in a known function.
const busyProgram = `package main

import "crypto/sha256"

//go:noinline
func hotLoop(b []byte) (s [32]byte) {
	for i := 0; i < 200; i++ {
		s = sha256.Sum256(append(b, s[:]...))
	}
	return s
}

func main() {
	b := make([]byte, 4096)
	for {
		hotLoop(b)
	}
}
`

// startBusy builds and starts busyProgram (stripped of its ELF symbols
// when strip is set); the process is killed at the end of the test.
func startBusy(t *testing.T, strip bool) *exec.Cmd {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(busyProgram), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module busy\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	args := []string{"build", "-o", "busy"}
	if strip {
		args = append(args, "-ldflags=-s -w")
	}
	build := exec.Command("go", append(args, ".")...)
	build.Dir = dir
	build.Env = append(os.Environ(), "GOFLAGS=", "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	cmd := exec.Command(filepath.Join(dir, "busy"))
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})
	return cmd
}

// realPerf skips the test when perf cannot sample this user's processes.
func realPerf(t *testing.T) Capability {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("perf is Linux only")
	}
	if testing.Short() {
		t.Skip("short mode")
	}
	c := Detect(context.Background(), Env{}, os.Geteuid())
	if !c.Available {
		t.Skip("perf unavailable: " + c.Reason)
	}
	return c
}

func recordBusy(t *testing.T, c Capability, strip bool) (*Data, Result) {
	t.Helper()
	cmd := startBusy(t, strip)
	time.Sleep(200 * time.Millisecond)
	dir := t.TempDir()
	tmp := t.TempDir()
	t0 := Monotonic()
	s, err := Start(context.Background(), Options{Cap: c, PID: cmd.Process.Pid, Freq: 199, T0: t0, Dir: dir, TempDir: tmp}, 0)
	if errors.Is(err, ErrPermission) {
		t.Skip("perf is not allowed here: ", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond)
	if s.Samples() == 0 {
		t.Error("no sample counted while recording")
	}
	res := s.Stop(context.Background(), 1500*time.Millisecond)
	if res.Error != "" {
		t.Fatalf("perf: %s (notes %v)", res.Error, res.Notes)
	}
	if left, _ := os.ReadDir(tmp); len(left) != 0 {
		t.Errorf("perf's temporary directory was not removed: %v", left)
	}
	d, err := Load(dir, res.File)
	if err != nil {
		t.Fatal(err)
	}
	return d, res
}

func TestRecordGoProcess(t *testing.T) {
	c := realPerf(t)
	d, res := recordBusy(t, c, false)
	if res.Samples < 50 || len(d.T) < 50 {
		t.Fatalf("too few samples: recorded %d, processed %d", res.Samples, len(d.T))
	}
	for i := 1; i < len(d.T); i++ {
		if d.T[i] < d.T[i-1] {
			t.Fatal("samples out of order")
		}
	}
	if d.T[0] < 0 || time.Duration(d.T[len(d.T)-1]) > 10*time.Second {
		t.Errorf("sample times are not offsets in the capture: %v … %v", time.Duration(d.T[0]), time.Duration(d.T[len(d.T)-1]))
	}
	rep, err := Analyze(&res, d, nil, 250*time.Millisecond, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range rep.CPU.Funcs {
		if f.Name == "main.hotLoop" {
			found = true
			if !strings.HasSuffix(f.File, "main.go") {
				t.Errorf("main.hotLoop has no source file: %+v", f)
			}
		}
	}
	if !found {
		t.Errorf("main.hotLoop is not among the functions: %+v", rep.CPU.Funcs[:min(5, len(rep.CPU.Funcs))])
	}
	if rep.SampledCPU <= 0 || len(rep.Timeline) == 0 {
		t.Errorf("no sampled CPU or timeline: %+v", rep)
	}
}

// A binary stripped of its ELF symbols: perf sees [unknown], GitView names
// the frames from the pclntab Go keeps.
func TestRecordStrippedGoProcess(t *testing.T) {
	c := realPerf(t)
	d, res := recordBusy(t, c, true)
	rep, err := Analyze(&res, d, nil, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.Stats.ByPclntab == 0 {
		t.Fatalf("no frame was named from the pclntab: %+v", d.Stats)
	}
	found := false
	for _, f := range rep.CPU.Funcs {
		found = found || f.Name == "main.hotLoop"
	}
	if !found {
		t.Errorf("main.hotLoop not found in a stripped binary")
	}
}

// perf exits by itself when the process exits; the samples taken until
// then are kept.
func TestRecordProcessExits(t *testing.T) {
	c := realPerf(t)
	cmd := startBusy(t, false)
	time.Sleep(200 * time.Millisecond)
	dir := t.TempDir()
	s, err := Start(context.Background(), Options{Cap: c, PID: cmd.Process.Pid, T0: Monotonic(), Dir: dir, TempDir: t.TempDir()}, 0)
	if errors.Is(err, ErrPermission) {
		t.Skip("perf is not allowed here: ", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(700 * time.Millisecond)
	cmd.Process.Kill()
	select {
	case <-s.Exited():
	case <-time.After(10 * time.Second):
		t.Fatal("perf did not exit with the process")
	}
	res := s.Stop(context.Background(), time.Second)
	if !res.Early {
		t.Error("the early end is not reported")
	}
	if res.Error != "" || res.File == "" {
		t.Fatalf("samples before the exit were lost: %+v", res)
	}
}
