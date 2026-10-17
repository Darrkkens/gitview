// Package export renders GitView session reports as Markdown, HTML or JSON.
// It only uses data already loaded by the application and writes local files;
// nothing is sent anywhere.
package export

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/gitdiff"
	"github.com/athos/gitview/internal/inspect"
	"github.com/athos/gitview/internal/inspect/perf"
	"github.com/athos/gitview/internal/inspect/pgexplain"
	"github.com/athos/gitview/internal/inspect/xtrace"
	"github.com/athos/gitview/internal/study"
)

type Format int

const (
	Markdown Format = iota
	HTML
	JSON
)

var Formats = []Format{Markdown, HTML, JSON}

func (f Format) String() string {
	switch f {
	case HTML:
		return "HTML"
	case JSON:
		return "JSON"
	}
	return "Markdown"
}

func (f Format) Ext() string {
	switch f {
	case HTML:
		return ".html"
	case JSON:
		return ".json"
	}
	return ".md"
}

// Report is everything an export contains.
type Report struct {
	Project     string
	Branch      string
	Root        string
	Base        *Commit // commit the working tree is compared against; nil without commits
	GeneratedAt time.Time
	Scope       string
	Query       string // active search, if any
	Files       []gitdiff.File
	Omitted     []string // paths whose content was withheld as sensitive
	Tests       *TestReport
	Warnings    []string
	Flow        *FlowData    // Go call index for the Code Flow tab; nil when not requested
	Inspect     *InspectData // Go Deep Inspector; nil when not requested
	Study       *study.Model // Study Mode; nil when not requested
}

// InspectData is the Go Deep Inspector section: results computed when the
// report was generated (static analyses) and runtime data that had been
// captured or imported. The browser only displays it.
type InspectData struct {
	Symbols []*inspect.Bundle  `json:"symbols"`
	CPU     *perf.Report       `json:"cpu,omitempty"`
	Mem     *perf.Report       `json:"mem,omitempty"`
	Benches []*perf.BenchRun   `json:"benches,omitempty"`
	Compare *perf.Comparison   `json:"compare,omitempty"`
	Escape  *perf.EscapeReport `json:"escape,omitempty"`
	Trace   *xtrace.Timeline   `json:"trace,omitempty"`
	Plans   []*pgexplain.Plan  `json:"plans,omitempty"`
	Notes   []string           `json:"notes,omitempty"`
	Errors  []string           `json:"errors,omitempty"`
}

// FlowData is the precomputed Go call graph embedded in the report. The
// browser only navigates it; all analysis happens when the report is made.
type FlowData struct {
	Graph   *flow.Graph `json:"graph,omitempty"`
	Modules []string    `json:"modules"`
	Errors  []string    `json:"errors,omitempty"` // compile errors: results may be partial
	Error   string      `json:"error,omitempty"`  // analysis unavailable
}

// Commit is the base commit of the diff. The compared side is always the
// working tree, which has no commit of its own.
type Commit struct {
	Hash    string `json:"hash"`
	Short   string `json:"short"`
	Subject string `json:"subject"`
	Author  string `json:"author"`
	Date    string `json:"date"`
}

// TestCommand is one executed command and its working directory.
type TestCommand struct {
	Line string `json:"command"`
	Dir  string `json:"dir"`
}

// TestReport is the last test run of the session. Ran=false means no tests
// were executed, which reports state explicitly instead of inventing results.
type TestReport struct {
	Ran        bool          `json:"ran"`
	Target     string        `json:"target,omitempty"`
	Status     string        `json:"status,omitempty"`
	Structured bool          `json:"structured"`
	Started    time.Time     `json:"started,omitzero"`
	Finished   time.Time     `json:"finished,omitzero"`
	ExitCode   int           `json:"exitCode"`
	Passed     int           `json:"passed"`
	Failed     int           `json:"failed"`
	Skipped    int           `json:"skipped"`
	Other      int           `json:"other"`
	Error      string        `json:"error,omitempty"`
	Commands   []TestCommand `json:"commands,omitempty"`
	Cases      []TestCase    `json:"tests,omitempty"`
	Log        []string      `json:"log,omitempty"`
	LogTotal   int           `json:"logLines,omitempty"` // lines produced; Log may hold only the tail
}

// TestCase is one result. Kind "package" marks a package-level outcome (build
// failure, crash) that is not an individual test.
type TestCase struct {
	Kind      string `json:"kind"` // test | package
	Package   string `json:"package,omitempty"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	ElapsedMs int64  `json:"elapsedMs"`
	Output    string `json:"output,omitempty"`
}

func (r Report) totals() (added, deleted int) {
	for _, f := range r.Files {
		added += f.Added
		deleted += f.Deleted
	}
	return
}

func (r Report) omitted(path string) bool {
	for _, p := range r.Omitted {
		if p == path {
			return true
		}
	}
	return false
}

// Render produces the report bytes in the given format.
func Render(f Format, r Report) ([]byte, error) {
	switch f {
	case HTML:
		return renderHTML(r)
	case JSON:
		return renderJSON(r)
	}
	return renderMarkdown(r), nil
}

var ErrExists = errors.New("o arquivo já existe")

// WriteFile writes data atomically. Unless overwrite is set, an existing file
// is never replaced: the final step is a hard link, which fails if the
// destination exists, so there is no check-then-write race.
func WriteFile(path string, data []byte, overwrite bool) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if !overwrite {
		if _, err := os.Lstat(path); err == nil {
			return ErrExists
		}
	}
	tmp, err := os.CreateTemp(dir, ".gitview-export-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if overwrite {
		return os.Rename(tmp.Name(), path)
	}
	err = os.Link(tmp.Name(), path)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, os.ErrExist):
		return ErrExists
	}
	// Filesystems without hard links: fall back to an exclusive create.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		return ErrExists
	}
	if err != nil {
		return fmt.Errorf("gravando %s: %w", path, err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	return f.Close()
}
