package testrunner

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// goEvent is one line of `go test -json` (cmd/test2json) output.
type goEvent struct {
	Time        time.Time
	Action      string
	Package     string
	Test        string
	Elapsed     float64
	Output      string
	ImportPath  string // build-output / build-fail events (Go 1.24+)
	FailedBuild string
}

type goParser struct {
	r       *Runner
	project string
	pkgOut  map[string][]string
	build   map[string][]string
	pkgFail map[string]bool // packages that reported a failing test
}

func newGoParser(r *Runner, project string) *goParser {
	return &goParser{r: r, project: project, pkgOut: map[string][]string{}, build: map[string][]string{}, pkgFail: map[string]bool{}}
}

// handle processes one stdout line; it reports whether it was a JSON event.
// Results are derived only from structured actions, never from text output.
func (g *goParser) handle(st *RunState, line string) bool {
	var ev goEvent
	if !strings.HasPrefix(line, "{") || json.Unmarshal([]byte(line), &ev) != nil {
		g.r.logLine(st, line)
		return false
	}
	out := strings.TrimRight(ev.Output, "\n")
	switch ev.Action {
	case "output":
		g.r.logLine(st, out)
		if ev.Test != "" {
			appendOutput(g.r.result(st, g.project, ev.Package, ev.Test), out)
		} else {
			g.pkgOut[ev.Package] = append(g.pkgOut[ev.Package], out)
		}
	case "build-output":
		g.r.logLine(st, out)
		g.build[ev.ImportPath] = append(g.build[ev.ImportPath], out)
	case "run":
		g.r.result(st, g.project, ev.Package, ev.Test).Status = Running
	case "pass", "fail", "skip":
		if ev.Test == "" {
			g.packageDone(st, ev)
			break
		}
		t := g.r.result(st, g.project, ev.Package, ev.Test)
		t.Elapsed = time.Duration(ev.Elapsed * float64(time.Second))
		switch ev.Action {
		case "pass":
			t.Status = Passed
		case "fail":
			t.Status = Failed
			g.pkgFail[ev.Package] = true
		case "skip":
			t.Status = Skipped
		}
	}
	return true
}

// packageDone records package-level failures that no individual test
// explains: build failures, panics in init/TestMain, os.Exit, timeouts.
func (g *goParser) packageDone(st *RunState, ev goEvent) {
	if ev.Action != "fail" || g.pkgFail[ev.Package] {
		return
	}
	name := "(pacote)"
	output := g.pkgOut[ev.Package]
	if ev.FailedBuild != "" {
		name = "(falha de compilação)"
		output = append(append([]string{}, g.build[ev.FailedBuild]...), output...)
	} else if b := g.build[ev.Package]; len(b) > 0 {
		name = "(falha de compilação)"
		output = append(append([]string{}, b...), output...)
	}
	for _, l := range output {
		if strings.Contains(l, "[build failed]") || strings.Contains(l, "[setup failed]") {
			name = "(falha de compilação)"
		}
	}
	t := g.r.result(st, g.project, ev.Package, name)
	t.Status = Failed
	t.Elapsed = time.Duration(ev.Elapsed * float64(time.Second))
	for _, l := range output {
		appendOutput(t, l)
	}
}

// jestReport is the JSON report format shared by Jest (--json) and Vitest
// (--reporter=json).
type jestReport struct {
	TestResults []struct {
		Name             string `json:"name"`
		Status           string `json:"status"`
		Message          string `json:"message"`
		AssertionResults []struct {
			FullName        string   `json:"fullName"`
			Title           string   `json:"title"`
			Status          string   `json:"status"`
			Duration        *float64 `json:"duration"`
			FailureMessages []string `json:"failureMessages"`
		} `json:"assertionResults"`
	} `json:"testResults"`
}

func parseJestReport(r *Runner, st *RunState, p *Project, data []byte) error {
	var rep jestReport
	if err := json.Unmarshal(data, &rep); err != nil {
		r.logLine(st, "relatório JSON inválido: "+err.Error())
		return err
	}
	for _, f := range rep.TestResults {
		file := f.Name
		if rel, err := filepath.Rel(p.Dir, f.Name); err == nil && !strings.HasPrefix(rel, "..") {
			file = filepath.ToSlash(rel)
		}
		if len(f.AssertionResults) == 0 && f.Status == "failed" {
			t := r.result(st, p.Name, file, "(arquivo de teste)")
			t.Status = Failed
			for _, l := range strings.Split(f.Message, "\n") {
				appendOutput(t, l)
			}
			continue
		}
		for _, a := range f.AssertionResults {
			name := a.FullName
			if name == "" {
				name = a.Title
			}
			t := r.result(st, p.Name, file, name)
			switch a.Status {
			case "passed":
				t.Status = Passed
			case "failed":
				t.Status = Failed
			default: // pending, skipped, todo, disabled
				t.Status = Skipped
			}
			if a.Duration != nil {
				t.Elapsed = time.Duration(*a.Duration * float64(time.Millisecond))
			}
			for _, m := range a.FailureMessages {
				for _, l := range strings.Split(m, "\n") {
					appendOutput(t, l)
				}
			}
		}
	}
	return nil
}

// FailedTarget builds a target that re-runs only the failed tests of a run.
func FailedTarget(s RunState) (Target, bool) {
	type key struct {
		project *Project
		pkg     string
	}
	byProject := map[string]*Project{}
	for _, u := range s.Target.Units {
		byProject[u.Project.Name] = u.Project
	}
	order := []key{}
	names := map[key][]string{}
	seen := map[string]bool{}
	for _, t := range s.Tests {
		if t.Status != Failed && t.Status != InfraError {
			continue
		}
		p := byProject[t.Project]
		if p == nil {
			continue
		}
		k := key{p, t.Package}
		if _, ok := names[k]; !ok {
			order = append(order, k)
			names[k] = nil
		}
		if p.Kind == KindGo && !strings.HasPrefix(t.Name, "(") {
			top := strings.SplitN(t.Name, "/", 2)[0]
			if !seen[t.Package+top] {
				seen[t.Package+top] = true
				names[k] = append(names[k], regexp.QuoteMeta(top))
			}
		}
	}
	if len(order) == 0 {
		return Target{}, false
	}
	t := Target{Label: "Falhas · " + s.Target.Label}
	var nodeFiles = map[*Project][]string{}
	for _, k := range order {
		switch k.project.Kind {
		case KindGo:
			if len(k.project.Command) > 0 {
				t.Units = append(t.Units, Unit{Project: k.project})
				continue
			}
			u := Unit{Project: k.project, Packages: []string{k.project.GoPackageDir(k.pkg)}}
			if n := names[k]; len(n) > 0 {
				u.Run = "^(" + strings.Join(n, "|") + ")$"
			}
			t.Units = append(t.Units, u)
		case KindNode:
			nodeFiles[k.project] = append(nodeFiles[k.project], k.pkg)
		default:
			t.Units = append(t.Units, Unit{Project: k.project})
		}
	}
	for p, files := range nodeFiles {
		t.Units = append(t.Units, Unit{Project: p, Files: files})
	}
	return t, true
}
