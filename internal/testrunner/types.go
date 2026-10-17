// Package testrunner discovers test suites (Go and Node.js) and runs them on
// explicit request, reporting structured per-test results when the ecosystem
// provides a reliable machine-readable format.
//
// Running tests executes code from the repository and may have external side
// effects; callers must only start runs after an explicit user action.
package testrunner

import (
	"time"
)

type Kind string

const (
	KindGo      Kind = "go"
	KindNode    Kind = "node"
	KindCommand Kind = "command"
)

// Project is a discovered (or configured) test suite.
type Project struct {
	Name       string
	Kind       Kind
	Dir        string   // absolute working directory
	Rel        string   // Dir relative to the repository root ("." for the root)
	Framework  string   // go | vitest | jest | script | command
	ModulePath string   // Go module path
	Packages   []string // Go packages with tests, as "./rel" relative to Dir
	Command    []string // explicit command (configured projects and node scripts)
	Bin        []string // node: command prefix that launches the framework
}

// ProjectConfig is a user-defined suite from .gitview.json.
type ProjectConfig struct {
	Name    string   `json:"name"`
	Kind    string   `json:"kind"` // go | node | command
	Dir     string   `json:"dir"`
	Command []string `json:"command"`
}

// Unit is one command invocation within a run.
type Unit struct {
	Project  *Project
	Packages []string // go: packages to test (default ./...)
	Run      string   // go: -run pattern
	Files    []string // node: test files
	Related  string   // node: source file whose related tests should run
}

// Target is something the user can run: a list of units executed in order.
type Target struct {
	Label string
	Units []Unit
}

type Status int

const (
	NotRun Status = iota
	Running
	Passed
	Failed
	Skipped
	Cancelled
	InfraError
)

func (s Status) String() string {
	return [...]string{"não executado", "executando", "aprovado", "falhou", "ignorado", "cancelado", "erro de infraestrutura"}[s]
}

// Key is a stable identifier used in exports.
func (s Status) Key() string {
	return [...]string{"notrun", "running", "passed", "failed", "skipped", "cancelled", "error"}[s]
}

type TestResult struct {
	Project string
	Package string // Go import path or Node test file
	Name    string
	Status  Status
	Elapsed time.Duration
	Output  []string
}

// RunState is a snapshot of the current or last run.
type RunState struct {
	Target     Target
	Status     Status
	Started    time.Time
	Finished   time.Time
	ExitCode   int
	Structured bool // per-test results come from a structured report
	Tests      []TestResult
	Commands   []Command // each command executed
	Log        []string
	Err        string
}

// Command is one executed command line and its working directory
// (relative to the repository root).
type Command struct {
	Line string
	Dir  string
}

type Counts struct{ Passed, Failed, Skipped, Running, Other int }

func (s RunState) Counts() Counts {
	var c Counts
	for _, t := range s.Tests {
		switch t.Status {
		case Passed:
			c.Passed++
		case Failed:
			c.Failed++
		case Skipped:
			c.Skipped++
		case Running:
			c.Running++
		default:
			c.Other++
		}
	}
	return c
}

func (s RunState) Duration() time.Duration {
	if s.Started.IsZero() {
		return 0
	}
	if s.Finished.IsZero() {
		return time.Since(s.Started)
	}
	return s.Finished.Sub(s.Started)
}
