package branch

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/athos/gitview/internal/inspect/inspecttest"
)

func build(t *testing.T, name string) *Graph {
	t.Helper()
	p := inspecttest.Deep(t)
	g, err := Build(p, inspecttest.Func(t, p, name), Options{})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// labels returns "summary -[label]-> summary" for every edge.
func labels(g *Graph) []string {
	var out []string
	for _, e := range g.Edges {
		out = append(out, g.Block(e.From).Summary()+" -["+e.Label+"]-> "+g.Block(e.To).Summary())
	}
	return out
}

func hasEdge(t *testing.T, g *Graph, want string) {
	t.Helper()
	for _, l := range labels(g) {
		if l == want {
			return
		}
	}
	t.Errorf("missing edge %q in:\n%s", want, strings.Join(labels(g), "\n"))
}

func TestEarlyReturnsMatchTheSpecExample(t *testing.T) {
	g := build(t, "branches.GetClient")
	hasEdge(t, g, "if id <= 0 -[true]-> return ErrInvalidID")
	hasEdge(t, g, "if id <= 0 -[false]-> if !exists(id)")
	hasEdge(t, g, "if !exists(id) -[true]-> return ErrNotFound")
	hasEdge(t, g, "if !exists(id) -[false]-> return nil")
	m := g.Metrics
	if m.Blocks != 5 || m.DecisionPoints != 2 || m.Returns != 3 || m.Cyclomatic != 3 || m.Paths != 3 || m.Loops != 0 {
		t.Errorf("metrics %+v", m)
	}
	errPaths := 0
	for _, p := range g.Paths {
		if p.Error {
			errPaths++
		}
	}
	if errPaths != 2 {
		t.Errorf("error paths = %d, want 2 (ErrInvalidID, ErrNotFound)", errPaths)
	}
	if g.Symbol.ID != "example.com/deep/branches.GetClient" || g.Symbol.File != "branches/branches.go" || g.Status.Status != "complete" {
		t.Errorf("symbol/status: %+v %v", g.Symbol, g.Status)
	}
	if b := g.Block(1); b.File != "branches/branches.go" || b.Line != 20 || b.Terminal != TermReturn || !b.ReturnsError {
		t.Errorf("block 1 location/terminal: %+v", b)
	}
}

func TestIfElseMerges(t *testing.T) {
	g := build(t, "branches.IfElse")
	hasEdge(t, g, `if x > 10 -[true]-> s = "big"`)
	hasEdge(t, g, `if x > 10 -[false]-> s = "small"`)
	hasEdge(t, g, `s = "big" -[]-> return s`)
	rows := g.Outline()
	last := rows[len(rows)-1]
	if g.Block(last.Block).Summary() != "return s" || last.Depth != 0 {
		t.Errorf("merge point should continue at depth 0: %+v", rows)
	}
}

func TestSwitchesBecomeMultiWayDecisions(t *testing.T) {
	g := build(t, "branches.Grade")
	hasEdge(t, g, `switch n -[case 10, 9]-> return "A"`)
	hasEdge(t, g, `switch n -[case 8]-> return "B"`)
	hasEdge(t, g, `switch n -[default]-> return "C"`)
	if g.Metrics.Decisions != 2 || g.Metrics.Cyclomatic != 3 {
		t.Errorf("metrics %+v", g.Metrics)
	}
	ts := build(t, "branches.Kind")
	hasEdge(t, ts, `switch v.(type) -[case int, int64]-> return "integer"`)
	hasEdge(t, ts, `switch v.(type) -[case string]-> return "text"`)
	hasEdge(t, ts, `switch v.(type) -[no case matched]-> return "other"`)
}

func TestLoopsBreakContinue(t *testing.T) {
	g := build(t, "branches.Sum")
	hasEdge(t, g, "if xs[i] < 0 -[true, continue]-> i++")
	hasEdge(t, g, "if xs[i] > 100 -[true, break]-> return total")
	hasEdge(t, g, "for i < len(xs) -[false]-> return total")
	loopEdges := 0
	for _, e := range g.Edges {
		if e.Kind == EdgeLoop {
			loopEdges++
			if !g.Block(e.To).LoopHead || g.Block(e.To).Summary() != "for i < len(xs)" {
				t.Errorf("back edge to %s", g.Block(e.To).Summary())
			}
		}
	}
	if loopEdges != 1 || g.Metrics.Loops != 1 {
		t.Errorf("loops: edges=%d metrics=%+v", loopEdges, g.Metrics)
	}
	// One path enters the body once, repeats the loop and leaves it.
	found := false
	for _, p := range g.Paths {
		if strings.Contains(strings.Join(p.Steps, "|"), "↻ B1|i < len(xs) → false") {
			found = true
		}
	}
	if !found {
		t.Errorf("no path repeating the loop once: %+v", g.Paths)
	}

	r := build(t, "branches.Each")
	hasEdge(t, r, "for _, v := range m -[next]-> n += v")
	hasEdge(t, r, "for _, v := range m -[done]-> return")

	f := build(t, "branches.Forever")
	hasEdge(t, f, "if !ok -[true, break loop]-> return count")
	if f.Metrics.Loops != 1 {
		t.Errorf("labelled loop: %+v", f.Metrics)
	}
}

func TestSelect(t *testing.T) {
	g := build(t, "branches.Wait")
	hasEdge(t, g, "select -[case v := <-a]-> return v")
	hasEdge(t, g, "select -[case <-b]-> return -1")
	if len(g.Out(0)) != 2 {
		t.Errorf("select without default must not fall through: %v", labels(g))
	}
	p := build(t, "branches.Poll")
	hasEdge(t, p, "select -[case v := <-a]-> return v, true")
	hasEdge(t, p, "select -[default]-> return 0, false")
}

func TestDeferPanicExitAndErrorHandling(t *testing.T) {
	g := build(t, "branches.Parse")
	if len(g.Defers) != 1 || !strings.HasPrefix(g.Defers[0].Text, "defer fmt.Println") || g.Metrics.Defers != 1 {
		t.Errorf("defers: %+v", g.Defers)
	}
	entry := g.Block(0)
	if !entry.ErrorCheck || entry.Cond != "err != nil" {
		t.Errorf("error check not detected: %+v", entry)
	}
	errEdge := false
	for _, e := range g.Out(0) {
		if e.Label == "true" && e.Error {
			errEdge = true
		}
	}
	if !errEdge {
		t.Error("err != nil → true should be the error edge")
	}
	hasEdge(t, g, `if n < 0 -[true]-> panic("negative")`)
	if g.Metrics.Panics != 1 || g.Metrics.Returns != 2 {
		t.Errorf("metrics %+v", g.Metrics)
	}
	x := build(t, "branches.Exit")
	hasEdge(t, x, "if code != 0 -[true]-> os.Exit(code)")
	for _, b := range x.Blocks {
		if b.Summary() == "os.Exit(code)" && b.Terminal != TermExit {
			t.Errorf("os.Exit terminal = %q", b.Terminal)
		}
	}
}

func TestUnreachableCode(t *testing.T) {
	g := build(t, "branches.Unreachable")
	if g.Metrics.Unreachable != 1 || g.Metrics.Blocks != 1 {
		t.Fatalf("metrics %+v", g.Metrics)
	}
	dead := g.Block(1)
	if dead.Reachable || len(dead.Stmts) != 2 || dead.Line != 131 {
		t.Errorf("dead block: %+v", dead)
	}
}

func TestOutlineAndDot(t *testing.T) {
	g := build(t, "branches.GetClient")
	rows := g.Outline()
	if len(rows) != 5 || rows[1].Label != "true" || rows[1].Depth != 1 || rows[3].Depth != 2 {
		t.Errorf("outline: %+v", rows)
	}
	dot := g.Dot()
	if !strings.Contains(dot, `b0 -> b1 [label="true"]`) || !strings.Contains(dot, "shape=diamond") {
		t.Errorf("dot:\n%s", dot)
	}
}

func TestCoverageProfileParsing(t *testing.T) {
	if _, err := ParseProfile(strings.NewReader("not a profile\n")); err == nil {
		t.Error("invalid profile accepted")
	}
	if _, err := ParseProfile(strings.NewReader("mode: set\nfoo.go:1.2,3.4 x y\n")); err == nil {
		t.Error("malformed block accepted")
	}
	pr, err := ParseProfile(strings.NewReader("mode: count\nexample.com/deep/branches/branches.go:19.2,19.13 1 4\n"))
	if err != nil || pr.Mode != "count" || len(pr.Files()) != 1 {
		t.Fatalf("%v %+v", err, pr)
	}
}

// TestCoverageOverlay runs the fixture's tests with -coverprofile: they reach
// the invalid-id and success paths of GetClient but not ErrNotFound.
func TestCoverageOverlay(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test on the fixture")
	}
	out := filepath.Join(t.TempDir(), "cover.out")
	args := CoverArgs(out, "./branches")
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = inspecttest.Dir()
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	pr, err := LoadProfile(out)
	if err != nil {
		t.Fatal(err)
	}
	p := inspecttest.Deep(t)
	g, err := Build(p, inspecttest.Func(t, p, "branches.GetClient"), Options{Coverage: pr})
	if err != nil {
		t.Fatal(err)
	}
	state := map[string]string{}
	for _, b := range g.Blocks {
		state[b.Summary()] = b.Coverage
	}
	want := map[string]string{
		"if id <= 0":          Covered,
		"return ErrInvalidID": Covered,
		"if !exists(id)":      Covered,
		"return ErrNotFound":  Uncovered,
		"return nil":          Covered,
	}
	for k, v := range want {
		if state[k] != v {
			t.Errorf("%s: coverage %q, want %q", k, state[k], v)
		}
	}
	if g.Coverage == nil || g.Coverage.Covered != 4 || g.Coverage.Uncovered != 1 || g.Coverage.Stale {
		t.Errorf("summary %+v", g.Coverage)
	}
	// A profile older than the source is stale: everything becomes unknown.
	// The age is relative to the source file, not to the clock, so the test
	// does not depend on when the fixture was last edited.
	src, err := os.Stat(filepath.Join(p.RepoRoot, g.Symbol.File))
	if err != nil {
		t.Fatal(err)
	}
	old := src.ModTime().Add(-time.Hour)
	os.Chtimes(out, old, old)
	pr, _ = LoadProfile(out)
	g, _ = Build(p, inspecttest.Func(t, p, "branches.GetClient"), Options{Coverage: pr})
	if !g.Coverage.Stale || g.Coverage.Unknown != 5 {
		t.Errorf("stale profile: %+v", g.Coverage)
	}
}

func TestNestedSwitchAndSelectStaySeparate(t *testing.T) {
	g := build(t, "branches.NestedSwitch")
	hasEdge(t, g, `switch v.(type) -[case string]-> return "s"`)
	hasEdge(t, g, `switch v.(type) -[default]-> switch y`)
	hasEdge(t, g, `switch y -[case 1]-> return "one"`)
	hasEdge(t, g, `switch y -[no case matched]-> return "other"`)
	for _, l := range labels(g) {
		if strings.HasPrefix(l, "switch v.(type) -[case 1]") {
			t.Errorf("inner case merged into the outer switch: %s", l)
		}
	}
	s := build(t, "branches.NestedSelect")
	hasEdge(t, s, "select -[case v := <-a]-> return v")
	if len(s.Out(0)) != 2 {
		t.Errorf("outer select outcomes: %v", labels(s))
	}
	for _, b := range s.Blocks {
		if b.Terminal == TermBlocked {
			t.Errorf("phantom blocking exit: %+v", b)
		}
	}
	// The inner select has one case and no default: it waits for that case,
	// so it is not a branching decision.
	hasEdge(t, s, "select -[default]-> select")
	hasEdge(t, s, "select -[case v := <-b]-> return v")
	if s.Metrics.DecisionPoints != 1 || s.Metrics.Cyclomatic != 2 || s.Metrics.Blocks != 4 {
		t.Errorf("metrics %+v", s.Metrics)
	}
}

func TestCompoundErrorConditionHasNoPolarity(t *testing.T) {
	g := build(t, "branches.CompoundErr")
	if !g.Block(0).ErrorCheck {
		t.Error("the condition tests an error")
	}
	for _, e := range g.Edges {
		if e.Error {
			t.Errorf("compound condition must not mark an error edge: %+v", e)
		}
	}
	for _, p := range g.Paths {
		if g.Block(p.End).Summary() == "return n, nil" && p.Error {
			t.Errorf("success path marked as error")
		}
	}
}
