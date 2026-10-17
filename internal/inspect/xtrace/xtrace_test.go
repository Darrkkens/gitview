package xtrace

import (
	"bytes"
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/inspect/inspecttest"
)

var (
	capOnce sync.Once
	cap     *Capture
	capDir  string
)

// capture runs the fixture's TestPipeline once with a trace and temporary
// instrumentation of traced.Square and traced.Pipeline.
func capture(t *testing.T) (*Capture, *flow.Program) {
	t.Helper()
	if testing.Short() {
		t.Skip("runs go test -trace on the fixture")
	}
	p := inspecttest.Deep(t)
	capOnce.Do(func() {
		capDir, _ = os.MkdirTemp("", "gitview-trace-test-*")
		capDir = capDir + "/out" // created by RunCapture, after planning
		pl, err := PlanCapture(p, CaptureOptions{Pkg: "./traced", Run: "^TestPipeline$", OutDir: capDir, Timeout: 3 * time.Minute,
			Instrument: []string{inspecttest.Func(t, p, "traced.Square").String(), inspecttest.Func(t, p, "traced.Pipeline").String(), "example.com/deep/nope.X"}})
		if err != nil {
			t.Fatal(err)
		}
		cap = RunCapture(context.Background(), pl, []*flow.Program{p})
	})
	if cap.Err != nil {
		t.Fatalf("%v\n%s", cap.Err, cap.Result.Stderr)
	}
	return cap, p
}

func TestCapturePlanIsSafe(t *testing.T) {
	c, p := capture(t)
	pl := c.Plan
	cmd := pl.Spec.CommandLine()
	for _, want := range []string{"go test", "-trace=", "-overlay=", "-o ", "./traced", "^TestPipeline$"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("command %q missing %q", cmd, want)
		}
	}
	if pl.Spec.Dir != p.Dir {
		t.Errorf("runs in %s, want the module directory", pl.Spec.Dir)
	}
	if len(pl.Instrumented) != 2 || len(pl.Skipped) != 1 || !strings.Contains(pl.Skipped[0], "not found") {
		t.Errorf("instrumented %v skipped %v", pl.Instrumented, pl.Skipped)
	}
	// The project file is untouched and the copy keeps every line number.
	orig := inspecttest.Dir() + "/traced/traced.go"
	src, _ := os.ReadFile(orig)
	if bytes.Contains(src, []byte("__gitview")) {
		t.Fatal("instrumentation leaked into the project file")
	}
	inst := pl.Contents(orig)
	if bytes.Count(src, []byte("\n")) != bytes.Count(inst, []byte("\n")) {
		t.Error("instrumentation changed line numbers")
	}
	if !bytes.Contains(inst, []byte(`"gitview: example.com/deep/traced.Square"`)) {
		t.Errorf("region missing in instrumented copy")
	}
	if _, err := os.Stat(capDir + "/trace.test"); err == nil {
		t.Error("test binary left behind")
	}
}

func TestTimelineFromRealTrace(t *testing.T) {
	c, p := capture(t)
	tl := c.Timeline
	if tl.DurationNs <= 0 || tl.Stats.Events == 0 || tl.Artifact.Imported || tl.Artifact.Command == "" || tl.Evidence[0].Basis != "observed" {
		t.Fatalf("timeline %+v", tl.Stats)
	}
	if len(tl.Tasks) != 1 || tl.Tasks[0].Type != "pipeline" || tl.Tasks[0].End < tl.Tasks[0].Start || len(tl.Tasks[0].Goroutines) < 4 {
		t.Errorf("tasks %+v", tl.Tasks)
	}
	if len(tl.Logs) != 1 || tl.Logs[0].Category != "result" || tl.Logs[0].Message != "collected" {
		t.Errorf("logs %+v", tl.Logs)
	}
	user := map[string]int{}
	for _, r := range tl.Regions {
		if !r.Instrumented {
			user[r.Type]++
		}
	}
	if user["square"] != 20 || user["collect"] != 1 {
		t.Errorf("user regions %v", user)
	}
	if tl.Stats.GCCycles == 0 || len(tl.GC) == 0 {
		t.Errorf("GC not observed: %+v", tl.Stats)
	}
	if tl.Stats.BlockedNs["chan receive"] == 0 || tl.Stats.BlockedNs["sleep"] == 0 {
		t.Errorf("blocking reasons %v", tl.Stats.BlockedNs)
	}
	if tl.Stats.GCNs > tl.DurationNs || tl.Stats.STWNs > tl.DurationNs {
		t.Errorf("GC time exceeds the trace: gc %d stw %d duration %d", tl.Stats.GCNs, tl.Stats.STWNs, tl.DurationNs)
	}
	for _, e := range tl.Events {
		if e.Kind == "block" && e.Dur < 0 {
			t.Errorf("negative block duration %+v", e)
		}
	}
	// The workers sleep 20 times in total (once per value): no fabricated
	// blocks. Runtime goroutines (the background scavenger) also park with
	// the "sleep" reason, depending on the machine's load: not counted.
	workers := map[int64]bool{}
	for _, g := range tl.Goroutines {
		if strings.HasSuffix(g.Name, "traced.worker") {
			workers[g.ID] = true
		}
	}
	sleeps := 0
	for _, e := range tl.Events {
		if e.Kind == "block" && e.Detail == "sleep" && workers[e.G] {
			sleeps++
			if e.Dur == 0 {
				t.Errorf("sleep without duration: %+v", e)
			}
		}
	}
	if sleeps != 20 {
		t.Errorf("sleep blocks = %d, want 20 (one per time.Sleep)", sleeps)
	}
	// Segments are ordered and do not overlap within a goroutine.
	for _, g := range tl.Goroutines {
		for i := 1; i < len(g.Segments); i++ {
			if g.Segments[i].Start < g.Segments[i-1].End {
				t.Fatalf("G%d segments overlap: %+v %+v", g.ID, g.Segments[i-1], g.Segments[i])
			}
		}
	}
	// Correlation with the module's symbols.
	sq := tl.ForSymbol(inspecttest.Func(t, p, "traced.Square").String())
	if sq.SpanCount != 20 || sq.SpanMin <= 0 || sq.SpanMax < sq.SpanMin || sq.SpanTotal < sq.SpanMax {
		t.Errorf("instrumented spans of Square: %+v", sq)
	}
	w := tl.ForSymbol(inspecttest.Func(t, p, "traced.worker").String())
	if len(w.Goroutines) != 3 || len(w.Blocks) == 0 {
		t.Errorf("worker goroutines %v blocks %d", w.Goroutines, len(w.Blocks))
	}
	for _, b := range w.Blocks {
		if b.Detail == "" {
			t.Errorf("block without reason %+v", b)
		}
	}
	if blocks := tl.LongestBlocks(3); len(blocks) == 0 || tl.Goroutine(blocks[0].G).System {
		t.Errorf("longest blocks %+v", blocks)
	}
	foundNote := false
	for _, n := range tl.Notes {
		foundNote = foundNote || strings.Contains(n, "instrumentation overhead")
	}
	if !foundNote {
		t.Errorf("instrumented spans must be labelled: %v", tl.Notes)
	}
}

func TestImportAndInvalidTraces(t *testing.T) {
	c, p := capture(t)
	tl, err := Load(c.Plan.TraceFile, []*flow.Program{p}, Limits{})
	if err != nil || !tl.Artifact.Imported || tl.Artifact.Name != "trace.out" {
		t.Fatalf("import: %v %+v", err, tl)
	}
	small, err := Load(c.Plan.TraceFile, nil, Limits{Segments: 10, Events: 5})
	if err != nil || !small.Truncated || small.Status.Status != "partial" {
		t.Errorf("limits not reported: %v truncated=%v %v", err, small.Truncated, small.Status)
	}
	data, _ := os.ReadFile(c.Plan.TraceFile)
	for name, bad := range map[string][]byte{
		"empty":   {},
		"garbage": []byte("this is not a trace at all"),
		"header":  []byte("go 1.25 trace\x00\x00\x00"),
	} {
		if _, err := Read(bytes.NewReader(bad), nil, Limits{}); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	// A truncated trace either yields what could be read, marked partial, or an error.
	if tr, err := Read(bytes.NewReader(data[:len(data)/2]), nil, Limits{}); err == nil && tr.Status.Status == "complete" && tr.Stats.Events == tl.Stats.Events {
		t.Error("truncated trace reported as complete")
	}
}

func TestInstrumentationRefusesChangedFiles(t *testing.T) {
	p := inspecttest.Deep(t)
	// A copy of the fixture whose file changes after the analysis.
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(inspecttest.TestdataDir())); err != nil {
		t.Fatal(err)
	}
	dir := root + "/deep"
	q, err := flow.Load(context.Background(), dir, dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = p
	os.WriteFile(dir+"/traced/traced.go", []byte("package traced\n"), 0o644)
	sq, _ := q.Resolve("traced.Square")
	_, err = PlanCapture(q, CaptureOptions{Pkg: "./traced", Instrument: []string{sq.String()}, OutDir: t.TempDir() + "/x"})
	if err == nil || !strings.Contains(err.Error(), "changed since it was analysed") {
		t.Fatalf("stale instrumentation not refused: %v", err)
	}
}
