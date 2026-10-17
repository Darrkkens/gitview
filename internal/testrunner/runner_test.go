package testrunner

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

const gomod = "module example.com/fx\n\ngo 1.21\n"

func TestDiscoverProjects(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"go.mod":                          gomod,
		"a/a.go":                          "package a\n",
		"a/a_test.go":                     "package a\n",
		"b/b.go":                          "package b\n",
		"testdata/x_test.go":              "package x\n",
		"tools/go.mod":                    "module example.com/tools\n",
		"tools/t_test.go":                 "package tools\n",
		"web/package.json":                `{"name":"web","devDependencies":{"vitest":"^1"},"scripts":{"test":"vitest"}}`,
		"web/node_modules/x/package.json": `{"name":"dep","scripts":{"test":"jest"}}`,
		"api/package.json":                `{"name":"api","devDependencies":{"jest":"^29"}}`,
		"empty/package.json":              `{"name":"empty","scripts":{"test":"echo \"Error: no test specified\" && exit 1"}}`,
		"scripted/package.json":           `{"name":"scripted","scripts":{"test":"node test.js"}}`,
		"scripted/yarn.lock":              "",
	})
	projects, err := Discover(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]*Project{}
	for _, p := range projects {
		got[p.Name] = p
	}
	if len(projects) != 5 {
		t.Fatalf("projects = %d: %+v", len(projects), got)
	}
	fx := got["example.com/fx"]
	if fx == nil || strings.Join(fx.Packages, ",") != "./a" {
		t.Fatalf("root module packages: %+v", fx)
	}
	if tools := got["example.com/tools"]; tools == nil || strings.Join(tools.Packages, ",") != "." {
		t.Fatalf("nested module: %+v", tools)
	}
	if got["web"].Framework != "vitest" || got["api"].Framework != "jest" {
		t.Errorf("node frameworks: web=%s api=%s", got["web"].Framework, got["api"].Framework)
	}
	if s := got["scripted"]; s.Framework != "script" || strings.Join(s.Command, " ") != "yarn test" {
		t.Errorf("script project: %+v", s)
	}
	if got["empty"] != nil || got["dep"] != nil {
		t.Error("default npm script and node_modules must be ignored")
	}
	if got["web"].Bin[0] != "npx" || got["web"].Bin[1] != "--no-install" {
		t.Errorf("node bin must never download: %v", got["web"].Bin)
	}

	targets := Targets(root, projects, "a/a_test.go")
	if targets[0].Label != "Todos os testes" || !strings.HasPrefix(targets[1].Label, "Arquivo atual") {
		t.Fatalf("targets: %v", labels(targets))
	}
}

func labels(ts []Target) []string {
	var out []string
	for _, t := range ts {
		out = append(out, t.Label)
	}
	return out
}

func TestRelatedTargetGo(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"go.mod":      gomod,
		"a/a.go":      "package a\n",
		"a/a_test.go": "package a\n\nfunc TestOne(t *testing.T) {}\nfunc TestTwo(t *testing.T) {}\nfunc helper() {}\n",
	})
	projects, _ := Discover(root, nil)
	tg, ok := RelatedTarget(root, projects, "a/a_test.go")
	if !ok || tg.Units[0].Run != "^(TestOne|TestTwo)$" || tg.Units[0].Packages[0] != "./a" {
		t.Fatalf("related test file: %+v", tg)
	}
	tg, ok = RelatedTarget(root, projects, "a/a.go")
	if !ok || tg.Units[0].Run != "" {
		t.Fatalf("related source file: %+v", tg)
	}
	if _, ok := RelatedTarget(root, projects, "README.md"); ok {
		t.Fatal("README must not map to tests")
	}
}

func TestGoParserEvents(t *testing.T) {
	r := NewRunner(0)
	r.index = map[string]int{}
	g := newGoParser(r, "p")
	st := &r.state
	for _, l := range []string{
		`{"Action":"start","Package":"x"}`,
		`{"Action":"run","Package":"x","Test":"TestA"}`,
		`{"Action":"output","Package":"x","Test":"TestA","Output":"--- PASS: TestA (0.01s)\n"}`,
		`{"Action":"pass","Package":"x","Test":"TestA","Elapsed":0.01}`,
		`{"Action":"run","Package":"x","Test":"TestB"}`,
		`{"Action":"output","Package":"x","Test":"TestB","Output":"    b_test.go:9: expected 200, got 401\n"}`,
		`{"Action":"fail","Package":"x","Test":"TestB","Elapsed":0.042}`,
		`{"Action":"run","Package":"x","Test":"TestC"}`,
		`{"Action":"skip","Package":"x","Test":"TestC"}`,
		`{"Action":"run","Package":"x","Test":"TestD"}`,
		`{"Action":"output","Package":"x","Test":"TestD","Output":"PASS is just text\n"}`,
		`not json at all`,
		`{"Action":"fail","Package":"x","Elapsed":0.1}`,
		`{"ImportPath":"y","Action":"build-output","Output":"y/y.go:3:1: syntax error\n"}`,
		`{"ImportPath":"y","Action":"build-fail"}`,
		`{"Action":"fail","Package":"y","Elapsed":0,"FailedBuild":"y"}`,
	} {
		g.handle(st, l)
	}
	want := map[string]Status{"TestA": Passed, "TestB": Failed, "TestC": Skipped, "TestD": Running, "(falha de compilação)": Failed}
	if len(st.Tests) != len(want) {
		t.Fatalf("tests: %+v", st.Tests)
	}
	for _, tr := range st.Tests {
		if want[tr.Name] != tr.Status {
			t.Errorf("%s = %v, want %v", tr.Name, tr.Status, want[tr.Name])
		}
	}
	b := st.Tests[1]
	if b.Elapsed != 42*time.Millisecond || !strings.Contains(strings.Join(b.Output, "\n"), "expected 200, got 401") {
		t.Errorf("TestB detail: %+v", b)
	}
	if !strings.Contains(strings.Join(st.Tests[4].Output, "\n"), "syntax error") {
		t.Errorf("build output not attached: %+v", st.Tests[4])
	}
	// A package failure explained by a failing test must not add a pseudo-entry.
	for _, tr := range st.Tests {
		if tr.Name == "(pacote)" {
			t.Error("unexpected package-level entry")
		}
	}
}

func TestJestReportParsing(t *testing.T) {
	r := NewRunner(0)
	r.index = map[string]int{}
	p := &Project{Name: "web", Dir: "/w"}
	report := `{"testResults":[
	 {"name":"/w/src/a.test.ts","status":"failed","assertionResults":[
	  {"fullName":"sum adds","status":"passed","duration":3},
	  {"fullName":"sum fails","status":"failed","duration":5,"failureMessages":["Expected: 200\nReceived: 401"]},
	  {"fullName":"todo","status":"todo"}]},
	 {"name":"/w/src/b.test.ts","status":"failed","message":"SyntaxError: x","assertionResults":[]}]}`
	if err := parseJestReport(r, &r.state, p, []byte(report)); err != nil {
		t.Fatal(err)
	}
	c := r.state.Counts()
	if c.Passed != 1 || c.Failed != 2 || c.Skipped != 1 {
		t.Fatalf("counts %+v", c)
	}
	if r.state.Tests[0].Package != "src/a.test.ts" {
		t.Errorf("file not made relative: %q", r.state.Tests[0].Package)
	}
	if err := parseJestReport(r, &r.state, p, []byte("{broken")); err == nil {
		t.Error("expected error for invalid report")
	}
}

// --- integration tests running the real `go test` -------------------------

func goProjectFixture(t *testing.T, files map[string]string) *Project {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not installed")
	}
	root := t.TempDir()
	files["go.mod"] = gomod
	write(t, root, files)
	projects, err := Discover(root, nil)
	if err != nil || len(projects) != 1 {
		t.Fatalf("discover: %v %+v", err, projects)
	}
	return projects[0]
}

func runAndWait(t *testing.T, r *Runner, tg Target) RunState {
	t.Helper()
	if err := r.Start(tg); err != nil {
		t.Fatal(err)
	}
	r.Wait()
	return r.Snapshot()
}

func byName(s RunState) map[string]TestResult {
	m := map[string]TestResult{}
	for _, t := range s.Tests {
		m[t.Name] = t
	}
	return m
}

func TestRunGoPassFailSkip(t *testing.T) {
	p := goProjectFixture(t, map[string]string{
		"x/x_test.go": `package x
import "testing"
func TestPass(t *testing.T) {}
func TestFail(t *testing.T) { t.Fatal("Expected: 200, Received: 401") }
func TestSkip(t *testing.T) { t.Skip("later") }
func TestSub(t *testing.T) { t.Run("inner", func(t *testing.T) { t.Error("boom") }) }
`})
	r := NewRunner(time.Minute)
	s := runAndWait(t, r, Target{Label: "all", Units: []Unit{{Project: p}}})
	if s.Status != Failed || !s.Structured || s.ExitCode == 0 {
		t.Fatalf("run status %v structured=%v exit=%d err=%q", s.Status, s.Structured, s.ExitCode, s.Err)
	}
	got := byName(s)
	for name, want := range map[string]Status{"TestPass": Passed, "TestFail": Failed, "TestSkip": Skipped, "TestSub": Failed, "TestSub/inner": Failed} {
		if got[name].Status != want {
			t.Errorf("%s = %v, want %v", name, got[name].Status, want)
		}
	}
	if !strings.Contains(strings.Join(got["TestFail"].Output, "\n"), "Received: 401") {
		t.Errorf("failure output missing: %v", got["TestFail"].Output)
	}
	if len(s.Log) == 0 {
		t.Error("log not captured")
	}

	ft, ok := FailedTarget(s)
	if !ok || ft.Units[0].Run != "^(TestFail|TestSub)$" || ft.Units[0].Packages[0] != "./x" {
		t.Fatalf("failed target: %+v", ft)
	}
	s = runAndWait(t, r, ft)
	if got := byName(s); got["TestPass"].Name != "" || got["TestFail"].Status != Failed {
		t.Errorf("rerun of failures ran unexpected tests: %v", s.Tests)
	}
}

func TestRunGoAllPassed(t *testing.T) {
	p := goProjectFixture(t, map[string]string{"x/x_test.go": "package x\nimport \"testing\"\nfunc TestOK(t *testing.T) {}\n"})
	s := runAndWait(t, NewRunner(time.Minute), Target{Units: []Unit{{Project: p}}})
	if s.Status != Passed || s.ExitCode != 0 || byName(s)["TestOK"].Status != Passed {
		t.Fatalf("%v %q %+v", s.Status, s.Err, s.Tests)
	}
}

func TestRunGoBuildFailure(t *testing.T) {
	p := goProjectFixture(t, map[string]string{"x/x_test.go": "package x\nimport \"testing\"\nfunc TestX(t *testing.T) { undefined() }\n"})
	s := runAndWait(t, NewRunner(time.Minute), Target{Units: []Unit{{Project: p}}})
	if s.Status != Failed {
		t.Fatalf("status %v err=%q tests=%+v", s.Status, s.Err, s.Tests)
	}
	bf, ok := byName(s)["(falha de compilação)"]
	if !ok || !strings.Contains(strings.Join(bf.Output, "\n"), "undefined") {
		t.Fatalf("build failure not reported: %+v\nlog:\n%s", s.Tests, strings.Join(s.Log, "\n"))
	}
}

func TestRunGoUnexpectedExit(t *testing.T) {
	p := goProjectFixture(t, map[string]string{"x/x_test.go": `package x
import ("os"; "testing")
func TestExit(t *testing.T) { os.Exit(3) }
`})
	s := runAndWait(t, NewRunner(time.Minute), Target{Units: []Unit{{Project: p}}})
	if s.Status == Passed {
		t.Fatal("a process that exited mid-test must not pass")
	}
	if got := byName(s)["TestExit"]; got.Status == Passed || got.Status == Running {
		t.Fatalf("TestExit = %v", got.Status)
	}
}

const sleepyTest = `package x
import ("testing"; "time")
func TestSleep(t *testing.T) { time.Sleep(60 * time.Second) }
`

func waitRunning(t *testing.T, r *Runner) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		for _, tr := range r.Snapshot().Tests {
			if tr.Name == "TestSleep" && tr.Status == Running {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("test never started")
}

func TestRunCancel(t *testing.T) {
	p := goProjectFixture(t, map[string]string{"x/x_test.go": sleepyTest})
	r := NewRunner(time.Minute)
	if err := r.Start(Target{Units: []Unit{{Project: p}}}); err != nil {
		t.Fatal(err)
	}
	waitRunning(t, r)
	if err := r.Start(Target{Units: []Unit{{Project: p}}}); !errors.Is(err, ErrBusy) {
		t.Fatalf("duplicate run allowed: %v", err)
	}
	start := time.Now()
	r.Cancel()
	r.Wait()
	s := r.Snapshot()
	if s.Status != Cancelled || byName(s)["TestSleep"].Status != Cancelled {
		t.Fatalf("status %v tests %+v", s.Status, s.Tests)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatalf("cancellation took %s", time.Since(start))
	}
	if r.Running() {
		t.Fatal("runner still marked as running")
	}
}

func TestRunTimeout(t *testing.T) {
	p := goProjectFixture(t, map[string]string{"x/x_test.go": sleepyTest})
	r := NewRunner(3 * time.Second)
	s := runAndWait(t, r, Target{Units: []Unit{{Project: p}}})
	if s.Status != Cancelled || !strings.Contains(s.Err, "tempo limite") {
		t.Fatalf("status %v err %q", s.Status, s.Err)
	}
	if s.Duration() > 15*time.Second {
		t.Fatalf("timeout not enforced: %s", s.Duration())
	}
}

func TestRunCommandUnstructured(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	p := &Project{Name: "custom", Kind: KindCommand, Framework: "command", Dir: t.TempDir(), Rel: ".",
		Command: []string{"sh", "-c", "echo PASS; echo oops >&2; exit 3"}}
	s := runAndWait(t, NewRunner(time.Minute), Target{Units: []Unit{{Project: p}}})
	if s.Status != Failed || s.Structured || s.ExitCode != 3 || len(s.Tests) != 0 {
		t.Fatalf("%+v", s)
	}
	log := strings.Join(s.Log, "\n")
	if !strings.Contains(log, "PASS") || !strings.Contains(log, "oops") {
		t.Errorf("stdout/stderr not captured: %q", log)
	}
}

func TestRunMissingCommand(t *testing.T) {
	p := &Project{Name: "nope", Kind: KindCommand, Dir: t.TempDir(), Rel: ".", Command: []string{"gitview-no-such-binary"}}
	s := runAndWait(t, NewRunner(time.Minute), Target{Units: []Unit{{Project: p}}})
	if s.Status != InfraError || !strings.Contains(s.Err, "não foi possível iniciar") {
		t.Fatalf("%v %q", s.Status, s.Err)
	}
}
