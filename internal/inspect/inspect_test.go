package inspect

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/inspect/inspecttest"
	"github.com/athos/gitview/internal/inspect/perf"
	"github.com/athos/gitview/internal/inspect/run"
	"github.com/athos/gitview/internal/inspect/xtrace"
)

func session(t *testing.T) (*Session, *flow.Program) {
	p := inspecttest.Deep(t)
	return NewSession(p.RepoRoot, []*flow.Program{p}), p
}

// Every view of a symbol refers to the same identity, and the relations
// between views (SQL operations in Code Flow, data flow into SQL
// parameters) line up.
func TestViewsShareTheSymbolIdentity(t *testing.T) {
	s, p := session(t)
	r, err := s.Resolve("service.(*ClientService).GetClientByID")
	if err != nil {
		t.Fatal(err)
	}
	b := s.Bundle(r, BundleOptions{DataFlow: true})
	id := r.Ref.ID
	if b.Symbol.ID != id || b.Branches.Symbol.ID != id || b.SQL.Symbol.ID != id {
		t.Fatalf("identities differ: bundle %s branches %s sql %s", b.Symbol.ID, b.Branches.Symbol.ID, b.SQL.Symbol.ID)
	}
	if len(b.DataFlow) == 0 {
		t.Fatal("no data flow traces")
	}
	for k, df := range b.DataFlow {
		if df.Symbol.ID != id {
			t.Errorf("data flow %s symbol %s", k, df.Symbol.ID)
		}
		for _, n := range df.Nodes {
			if n.Func != "" && p.FuncByID(n.Func) == nil {
				t.Errorf("data flow node in unknown function %s", n.Func)
			}
		}
	}
	g, err := p.Query(r.Fn, flow.Both, flow.Options{Depth: 4, External: true})
	if err != nil {
		t.Fatal(err)
	}
	if g.Node(id) == nil {
		t.Fatal("Code Flow does not know the symbol")
	}
	for _, op := range b.SQL.Ops {
		if g.Node(op.ID) == nil {
			t.Errorf("SQL op %s is not a Code Flow node", op.ID)
		}
	}
	idFlow := b.DataFlow["id@27"]
	if idFlow == nil {
		t.Fatalf("no trace for param id: %v", keys(b.DataFlow))
	}
	reached := false
	for _, n := range idFlow.Nodes {
		if n.Kind == "sql" && n.File == b.SQL.Ops[0].File && n.Line == b.SQL.Ops[0].Line {
			reached = true
		}
	}
	if !reached {
		t.Error("Data Flow does not reach the SQL Explorer operation")
	}
	if b.Perf.Message != NoProfile || b.Trace.Message != NoTrace || b.Trace.Loaded {
		t.Errorf("runtime views must say there is no data: %+v %+v", b.Perf, b.Trace)
	}
	// Cached per code version.
	g1, _ := s.Branches(r)
	g2, _ := s.Branches(r)
	if g1 != g2 {
		t.Error("branches not cached")
	}
	s.SetPrograms(nil)
	if len(s.cache) != 0 {
		t.Error("cache not invalidated when programs change")
	}
}

func keys[V any](m map[string]V) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestResolveAmbiguity(t *testing.T) {
	s, _ := session(t)
	if _, err := s.Resolve("Count"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("Count should be ambiguous: %v", err)
	}
	if _, err := s.Resolve("Nope"); err == nil || !strings.Contains(err.Error(), NoSymbol) {
		t.Errorf("missing: %v", err)
	}
}

// Runtime data (profiles, compiler diagnostics, traces) is attributed to
// the same symbol IDs as the static views.
func TestRuntimeDataUsesTheSameIdentity(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test and go build on the fixture")
	}
	s, p := session(t)
	progs := []*flow.Program{p}
	dir := t.TempDir()
	c := perf.RunBench(context.Background(), progs, p.Dir, "./perf", perf.BenchOptions{Bench: "Busy", Count: 1, Benchtime: "200ms", Profiles: true, OutDir: dir}, 3*time.Minute)
	if c.CPU == nil {
		t.Fatalf("no CPU profile: %v", c.Errors)
	}
	s.CPU, s.Mem = c.CPU, c.Mem
	s.AddBench(c.Run)
	res := run.Run(context.Background(), perf.EscapeSpec(p.Dir, "./perf"))
	s.Escape = perf.ParseEscape(res.Stderr, progs)

	busy, _ := s.Resolve("perf.Busy")
	b := s.Bundle(busy, BundleOptions{})
	if b.Perf.CPU == nil || !b.Perf.CPU.Found || b.Perf.CPU.Symbol != busy.Ref.ID || len(b.Perf.Benchmarks) == 0 {
		t.Errorf("performance view of perf.Busy: %+v", b.Perf)
	}
	np, _ := s.Resolve("perf.NewPoint")
	if e := s.Bundle(np, BundleOptions{}).Perf.Escape; len(e) == 0 || e[0].Symbol != np.Ref.ID {
		t.Errorf("escape diagnostics of NewPoint: %+v", e)
	}

	sq, _ := s.Resolve("traced.Square")
	tdir, _ := os.MkdirTemp("", "gitview-inspect-trace-*")
	defer os.RemoveAll(tdir)
	pl, err := xtrace.PlanCapture(p, xtrace.CaptureOptions{Pkg: "./traced", Run: "TestPipeline", Instrument: []string{sq.Ref.ID}, OutDir: tdir, Timeout: 3 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	cap := xtrace.RunCapture(context.Background(), pl, progs)
	if cap.Err != nil {
		t.Fatal(cap.Err)
	}
	s.Trace = cap.Timeline
	tv := s.Bundle(sq, BundleOptions{}).Trace
	if !tv.Loaded || tv.Symbol == nil || tv.Symbol.SpanCount != 20 || tv.Symbol.Symbol != sq.Ref.ID {
		t.Errorf("trace view of Square: %+v %+v", tv, tv.Symbol)
	}
}
