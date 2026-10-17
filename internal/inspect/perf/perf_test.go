package perf

import (
	"context"
	"math"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/inspect/inspecttest"
	"github.com/athos/gitview/internal/inspect/run"
)

func TestInvalidProfiles(t *testing.T) {
	for name, data := range map[string][]byte{
		"empty":     {},
		"text":      []byte("goroutine profile: total 1\n"),
		"gzip junk": {0x1f, 0x8b, 0, 1, 2, 3},
		"bad proto": {0xff, 0xff, 0xff},
	} {
		if _, err := Parse(data); err == nil {
			t.Errorf("%s accepted as a profile", name)
		}
	}
	dir := t.TempDir()
	os.WriteFile(dir+"/x.prof", []byte("nope"), 0o644)
	if _, err := Load(dir+"/x.prof", nil, Options{}); err == nil || !strings.Contains(err.Error(), "x.prof") {
		t.Errorf("load error should name the file: %v", err)
	}
}

const oldBench = `goos: linux
goarch: amd64
pkg: example.com/deep/perf
cpu: Test CPU A
BenchmarkJoin-8   	 1000000	       100.0 ns/op	      16 B/op	       1 allocs/op
BenchmarkJoin-8   	 1000000	       102.0 ns/op	      16 B/op	       1 allocs/op
BenchmarkJoin-8   	 1000000	       101.0 ns/op	      16 B/op	       1 allocs/op
BenchmarkJoin-8   	 1000000	        99.0 ns/op	      16 B/op	       1 allocs/op
BenchmarkJoin-8   	 1000000	       100.5 ns/op	      16 B/op	       1 allocs/op
BenchmarkGone-8   	 1000	       5 ns/op
PASS
ok  	example.com/deep/perf	1.0s
`

const newBench = `goos: linux
goarch: amd64
pkg: example.com/deep/perf
cpu: Test CPU B
BenchmarkJoin-8   	 2000000	        60.0 ns/op	       8 B/op	       1 allocs/op
BenchmarkJoin-8   	 2000000	        61.0 ns/op	       8 B/op	       1 allocs/op
BenchmarkJoin-8   	 2000000	        59.5 ns/op	       8 B/op	       1 allocs/op
BenchmarkJoin-8   	 2000000	        60.5 ns/op	       8 B/op	       1 allocs/op
BenchmarkJoin-8   	 2000000	        60.2 ns/op	       8 B/op	       1 allocs/op
BenchmarkNew/sub-8	 10	       7.0 ns/op
`

func TestBenchmarksParseAndCompare(t *testing.T) {
	o, err := ParseBench(strings.NewReader(oldBench))
	if err != nil {
		t.Fatal(err)
	}
	n, _ := ParseBench(strings.NewReader(newBench))
	if len(o.Results) != 6 || o.Results[0].Name != "BenchmarkJoin" || o.Results[0].Procs != 8 || o.Results[0].Metrics["B/op"] != 16 || o.Config["cpu"] != "Test CPU A" {
		t.Fatalf("parse %+v", o.Results[0])
	}
	if n.Results[5].Name != "BenchmarkNew/sub" {
		t.Errorf("sub-benchmark name %q", n.Results[5].Name)
	}
	var join BenchStat
	for _, s := range o.Stats() {
		if s.Name == "BenchmarkJoin" && s.Unit == "ns/op" {
			join = s
		}
	}
	if join.N != 5 || join.Min != 99 || join.Max != 102 || join.Median != 100.5 || math.Abs(join.Mean-100.5) > 1e-9 {
		t.Errorf("stats %+v", join)
	}
	c := Compare(o, n)
	if len(c.ConfigDiffs) != 1 || !strings.Contains(c.ConfigDiffs[0], "cpu") {
		t.Errorf("config differences %v", c.ConfigDiffs)
	}
	rows := map[string]CompareRow{}
	for _, r := range c.Rows {
		rows[r.Name+" "+r.Unit] = r
	}
	ns := rows["BenchmarkJoin ns/op"]
	if !ns.Significant || ns.P >= 0.05 || ns.DeltaPct > -39 || ns.DeltaPct < -41 {
		t.Errorf("ns/op row %+v", ns)
	}
	if a := rows["BenchmarkJoin allocs/op"]; a.Significant || a.DeltaPct != 0 {
		t.Errorf("identical allocs must not be significant: %+v", a)
	}
	if rows["BenchmarkGone ns/op"].Note != "only in the old run" || rows["BenchmarkNew/sub ns/op"].Note != "only in the new run" {
		t.Errorf("unmatched rows: %+v", c.Rows)
	}
	if _, err := ParseBench(strings.NewReader("PASS\n")); err == nil {
		t.Error("output without results accepted")
	}
}

func TestMannWhitneyExact(t *testing.T) {
	if p := mannWhitney([]float64{1, 2, 3, 4}, []float64{5, 6, 7, 8}); math.Abs(p-2.0/70) > 1e-12 {
		t.Errorf("p = %v, want 2/70", p)
	}
	if p := mannWhitney([]float64{1, 5, 2, 6}, []float64{3, 4, 7, 8}); p < 0.3 {
		t.Errorf("interleaved samples p = %v", p)
	}
}

var (
	capOnce sync.Once
	capture *BenchCapture
)

// benchCapture runs the fixture's benchmarks once with CPU and memory profiles.
func benchCapture(t *testing.T) (*BenchCapture, *flow.Program) {
	t.Helper()
	if testing.Short() {
		t.Skip("runs go test -bench on the fixture")
	}
	p := inspecttest.Deep(t)
	capOnce.Do(func() {
		dir, err := os.MkdirTemp("", "gitview-perf-test-*")
		if err != nil {
			t.Fatal(err)
		}
		capture = RunBench(context.Background(), []*flow.Program{p}, inspecttest.Dir(), "./perf",
			BenchOptions{Bench: ".", Count: 1, Benchtime: "200ms", Profiles: true, OutDir: dir}, 3*time.Minute)
	})
	if len(capture.Errors) > 0 {
		t.Fatalf("capture: %v\n%s", capture.Errors, capture.Result.Stderr)
	}
	return capture, p
}

func TestBenchmarkCaptureWithProfiles(t *testing.T) {
	c, p := benchCapture(t)
	if !strings.Contains(c.Command, "-cpuprofile=") || !strings.Contains(c.Command, "-o ") {
		t.Errorf("command %s", c.Command)
	}
	names := map[string]bool{}
	for _, r := range c.Run.Results {
		names[r.Name] = true
		if _, ok := r.Metrics["allocs/op"]; !ok {
			t.Errorf("-benchmem metrics missing: %+v", r)
		}
	}
	if !names["BenchmarkBusy"] || !names["BenchmarkJoin"] || !names["BenchmarkNewPoint"] || c.Run.Config["pkg"] != "example.com/deep/perf" {
		t.Errorf("results %v config %v", names, c.Run.Config)
	}
	if _, err := os.Stat(c.Files.Binary); err == nil {
		t.Error("test binary left behind")
	}
	if _, err := os.Stat(inspecttest.Dir() + "/perf/perf.test"); err == nil {
		t.Error("test binary written into the module")
	}

	cpu := c.CPU
	if cpu == nil || cpu.Kind != "cpu" || cpu.Samples == 0 || cpu.Total == 0 || cpu.Evidence[0].Basis != "estimated" {
		t.Fatalf("cpu report %+v", cpu)
	}
	busy := inspecttest.Func(t, p, "perf.Busy")
	sp := cpu.ForSymbol(busy.String())
	if !sp.Found || sp.Cum == 0 || sp.CumPct <= 0 {
		t.Fatalf("perf.Busy not found in the CPU profile (mapped by symbol ID): %+v", sp)
	}
	if len(sp.Lines) == 0 || sp.Lines[0].File != "perf/perf.go" {
		t.Errorf("hot lines %+v", sp.Lines)
	}
	if cpu.Flame == nil || cpu.Flame.Value != cpu.Total || len(cpu.Flame.Children) == 0 {
		t.Errorf("flame graph %+v", cpu.Flame)
	}
	callerFound := false
	for _, e := range sp.Callers {
		if strings.HasSuffix(e.From, "BenchmarkBusy") {
			callerFound = true
		}
	}
	if !callerFound {
		t.Errorf("profile call graph: callers of Busy %+v", sp.Callers)
	}

	mem := c.Mem
	if mem == nil || mem.Kind != "heap" || mem.SampleType != "alloc_space/bytes" || !strings.Contains(mem.Meaning, "cumulative") {
		t.Fatalf("mem report %+v", mem)
	}
	if !strings.Contains(strings.Join(mem.SampleTypes, ","), "inuse_space") {
		t.Errorf("memory profile sample types %v", mem.SampleTypes)
	}
	// The same file analysed for memory in use.
	inuse, err := Load(c.Files.Mem, []*flow.Program{p}, Options{SampleType: "inuse_space"})
	if err != nil || inuse.SampleType != "inuse_space/bytes" {
		t.Errorf("inuse view: %v %+v", err, inuse)
	}
	if _, err := Load(c.Files.Mem, nil, Options{SampleType: "nope"}); err == nil || !strings.Contains(err.Error(), "available") {
		t.Errorf("unknown sample type: %v", err)
	}
}

func TestEscapeAnalysisDiagnostics(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go build")
	}
	p := inspecttest.Deep(t)
	spec := EscapeSpec(inspecttest.Dir(), "./perf")
	res := run.Run(context.Background(), spec)
	if res.Err != nil || res.ExitCode != 0 {
		t.Fatalf("%v %s", res.Err, res.Stderr)
	}
	r := ParseEscape(res.Stderr, []*flow.Program{p})
	if len(r.Errors) > 0 || len(r.Packages) != 1 || r.Packages[0] != "example.com/deep/perf" {
		t.Errorf("packages %v errors %v", r.Packages, r.Errors)
	}
	np := r.ForSymbol(inspecttest.Func(t, p, "perf.NewPoint").String())
	var kinds []string
	escapes := 0
	for _, d := range np {
		kinds = append(kinds, d.Kind+": "+d.Message)
		if d.Kind == DiagEscapes && strings.HasPrefix(d.Message, "&Point{...} escapes to heap") {
			escapes++
			if len(d.Flow) == 0 {
				t.Errorf("-m=2 explanation not attached: %+v", d)
			}
		}
	}
	if escapes != 1 {
		t.Errorf("NewPoint escape diagnostics (want exactly one, merged): %v", kinds)
	}
	sum := r.ForSymbol(inspecttest.Func(t, p, "perf.Sum").String())
	found := false
	for _, d := range sum {
		if d.Kind == DiagNoEscape && d.Message == "xs does not escape" {
			found = true
		}
	}
	if !found {
		t.Errorf("Sum diagnostics %+v", sum)
	}
	busy := r.ForSymbol(inspecttest.Func(t, p, "perf.Busy").String())
	cannot := false
	for _, d := range busy {
		cannot = cannot || d.Kind == DiagCannot
	}
	if !cannot || r.Counts()[DiagCanInline] < 2 {
		t.Errorf("inlining decisions: %+v counts %v", busy, r.Counts())
	}
	if r.Evidence[0].Kind != "compiler" {
		t.Error("evidence kind")
	}
}

func TestZeroBaselineIsNotHidden(t *testing.T) {
	old := "BenchmarkA-8 1 10 ns/op 0 allocs/op\n"
	cur := "BenchmarkA-8 1 10 ns/op 5 allocs/op\n"
	o, _ := ParseBench(strings.NewReader(strings.Repeat(old, 5)))
	n, _ := ParseBench(strings.NewReader(strings.Repeat(cur, 5)))
	for _, r := range Compare(o, n).Rows {
		if r.Unit == "allocs/op" && (!r.DeltaUndefined || !strings.Contains(r.Note, "was 0, now 5")) {
			t.Errorf("regression from zero hidden: %+v", r)
		}
	}
	b, _ := ParseBench(strings.NewReader("panic: boom\nBenchmarkA-8 1 10 ns/op\n"))
	if _, ok := b.Config["panic"]; ok {
		t.Error("panic line taken as configuration")
	}
}

func TestEscapeOutputClassification(t *testing.T) {
	out := []byte(`# example.com/x
<autogenerated>:1: inlining call to sync.(*Mutex).Lock
./a.go:3:2: x escapes to heap:
./a.go:3:2:   flow: {heap} = &x:
./a.go:3:2: moved to heap: x
./a.go:9:5: undefined: nope
`)
	r := ParseEscape(out, nil)
	if len(r.Errors) != 0 {
		t.Errorf("autogenerated lines reported as errors: %v", r.Errors)
	}
	moved := 0
	for _, d := range r.Diags {
		if d.Line == 3 {
			moved++
			if d.Kind != DiagMoved || len(d.Flow) != 1 {
				t.Errorf("merged diagnostic %+v", d)
			}
		}
	}
	if moved != 1 {
		t.Errorf("moved variable counted %d times", moved)
	}
	r.BuildFailed()
	if len(r.Errors) != 1 || !strings.Contains(r.Errors[0], "undefined: nope") {
		t.Errorf("compile error not reported as such: %v", r.Errors)
	}
}
