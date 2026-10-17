// Package evidence is the shared vocabulary of the Go Deep Inspector: every
// result carries where it came from (a symbol, a file and line, a command, a
// profile or a trace event) and whether it is static, observed or estimated.
//
// Nothing here performs analysis; it only describes results so the CLI, the
// TUI and the HTML report present them the same way.
package evidence

import (
	"fmt"
	"strings"
	"time"
)

// Kind says which source produced a piece of evidence.
type Kind string

const (
	Static    Kind = "static"    // source analysis: go/types, SSA, go/cfg
	Compiler  Kind = "compiler"  // diagnostics printed by the Go compiler
	Runtime   Kind = "runtime"   // events recorded while code ran (runtime/trace, go test events)
	Profile   Kind = "profile"   // sampled profiles (pprof)
	Benchmark Kind = "benchmark" // measurements printed by go test -bench
	Database  Kind = "database"  // PostgreSQL planner output
	Coverage  Kind = "coverage"  // go test -coverprofile counters
)

// Basis separates what was derived from code, what was observed while it ran
// and what is an estimate (sampling, planner statistics).
type Basis string

const (
	BasisStatic    Basis = "static"
	BasisObserved  Basis = "observed"
	BasisEstimated Basis = "estimated"
)

// Confidence of a static relation, shared by Code Flow and Data Flow.
const (
	Exact      = "exact"      // demonstrated by the SSA def-use chain or a static call
	Derived    = "derived"    // the value is computed from the source (conversion, arithmetic, opaque call)
	Likely     = "likely"     // single possible target (interface with one implementation)
	Possible   = "possible"   // one of several targets, or through memory that may alias
	Unresolved = "unresolved" // the analysis could not follow the value
)

// Evidence ties a result to something verifiable.
type Evidence struct {
	Kind       Kind   `json:"kind"`
	Basis      Basis  `json:"basis"`
	File       string `json:"file,omitempty"`
	Line       int    `json:"line,omitempty"`
	SymbolID   string `json:"symbol,omitempty"`
	Source     string `json:"source"` // tool or artifact, e.g. "go/ssa", "cpu.prof", "go test -trace"
	Confidence string `json:"confidence,omitempty"`
	Details    string `json:"details,omitempty"`
}

func (e Evidence) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "[%s/%s] %s", e.Kind, e.Basis, e.Source)
	if e.File != "" {
		fmt.Fprintf(&b, " %s:%d", e.File, e.Line)
	}
	if e.Confidence != "" {
		fmt.Fprintf(&b, " (%s)", e.Confidence)
	}
	if e.Details != "" {
		b.WriteString(" — " + e.Details)
	}
	return b.String()
}

// Status tells whether a result is complete. Analyses never hide limits:
// whatever stopped them is listed in Completeness.Reasons.
type Status string

const (
	Complete     Status = "complete"
	Partial      Status = "partial"      // a limit was hit or some part could not be followed
	Inconclusive Status = "inconclusive" // the question cannot be answered from the available evidence
	Unavailable  Status = "unavailable"  // no data (e.g. no trace was captured)
)

type Completeness struct {
	Status  Status   `json:"status"`
	Reasons []string `json:"reasons,omitempty"`
}

// Limit records a reason the result is partial (keeping the worst status).
func (c *Completeness) Limit(reason string) {
	if c.Status == "" || c.Status == Complete {
		c.Status = Partial
	}
	for _, r := range c.Reasons {
		if r == reason {
			return
		}
	}
	c.Reasons = append(c.Reasons, reason)
}

// Done marks the result complete when nothing limited it.
func (c *Completeness) Done() {
	if c.Status == "" {
		c.Status = Complete
	}
}

func (c Completeness) String() string {
	if len(c.Reasons) == 0 {
		return string(c.Status)
	}
	return string(c.Status) + ": " + strings.Join(c.Reasons, "; ")
}

// SymbolRef is the identity every module shares for a Go function. ID is the
// fully qualified SSA name (package path, receiver and name), which is unique
// within a build even for homonyms; the rest locates it in the analysed
// version of the code.
type SymbolRef struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Qualified string `json:"qualified"`
	Pkg       string `json:"pkg"`
	Recv      string `json:"recv,omitempty"`
	Signature string `json:"sig,omitempty"`
	File      string `json:"file,omitempty"`
	Line      int    `json:"line,omitempty"`
	EndLine   int    `json:"end,omitempty"`
	Module    string `json:"module,omitempty"`
	Version   string `json:"version,omitempty"` // fingerprint of the analysed sources
}

// Artifact describes runtime data (trace, profile, benchmark output,
// coverage, compiler diagnostics) and how it was obtained.
type Artifact struct {
	Kind        string    `json:"kind"` // trace | cpu | heap | bench | coverage | escape | explain
	Name        string    `json:"name"` // file name shown to the user (never credentials)
	Path        string    `json:"-"`    // local path; not written into reports
	Imported    bool      `json:"imported"`
	Command     string    `json:"command,omitempty"` // command GitView ran to produce it
	Dir         string    `json:"dir,omitempty"`     // working directory of Command, repository-relative
	CapturedAt  time.Time `json:"capturedAt,omitzero"`
	CodeVersion string    `json:"codeVersion,omitempty"` // source fingerprint when GitView captured it
	Stale       bool      `json:"stale,omitempty"`       // sources changed after capture
	Notes       []string  `json:"notes,omitempty"`
}
