package export

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/alecthomas/chroma/v2"

	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/gitdiff"
	"github.com/athos/gitview/internal/highlight"
	"github.com/athos/gitview/internal/hover"
	"github.com/athos/gitview/internal/inspect/perf"
	"github.com/athos/gitview/internal/live/report"
	"github.com/athos/gitview/internal/study"
)

// ReportSection is the session report as the web UI reads it (section
// "report" of the page, frontend/src/lib/types.ts): the working tree's
// changes, highlighted here with Chroma so the page only renders spans,
// plus the analyses that were requested. The browser computes nothing.
type ReportSection struct {
	Project   string       `json:"project"`
	Branch    string       `json:"branch"`
	Root      string       `json:"root"`
	Base      *Commit      `json:"base,omitempty"`
	Generated time.Time    `json:"generated"`
	Scope     string       `json:"scope,omitempty"`
	Query     string       `json:"query,omitempty"`
	Added     int          `json:"added"`
	Deleted   int          `json:"deleted"`
	Files     []DiffFile   `json:"files"`
	Omitted   []string     `json:"omitted,omitempty"`
	Warnings  []string     `json:"warnings,omitempty"`
	Tests     *TestReport  `json:"tests,omitempty"`
	Flow      *FlowSection `json:"flow,omitempty"`
	Inspect   *InspectView `json:"inspect,omitempty"`
	Study     *study.Model `json:"study,omitempty"`
	Hovers    *hover.Table `json:"hovers,omitempty"`
}

// DiffFile is a changed file ready to display. ID is the anchor the Code
// Flow and Inspector "anchors" maps point to.
type DiffFile struct {
	ID         string     `json:"id"`
	Path       string     `json:"path"`
	OldPath    string     `json:"oldPath,omitempty"`
	Status     string     `json:"status"` // A | D | R | M
	StatusName string     `json:"statusName"`
	Added      int        `json:"added"`
	Deleted    int        `json:"deleted"`
	Binary     bool       `json:"binary,omitempty"`
	Omitted    bool       `json:"omitted,omitempty"`
	Open       bool       `json:"open"` // large diffs start collapsed
	LineCount  int        `json:"lineCount"`
	Hunks      []DiffHunk `json:"hunks"`
}

type DiffHunk struct {
	Range   string     `json:"range"` // "@@ -10,3 +10,4 @@"
	Section string     `json:"section,omitempty"`
	Lines   []DiffLine `json:"lines"`
}

type DiffLine struct {
	Kind  string `json:"kind"` // add | del | ctx
	Old   int    `json:"old"`  // 0 when the line does not exist on that side
	New   int    `json:"new"`
	Spans []Span `json:"spans"`
}

// Span is highlighted text; it is encoded as ["text"] or ["text", "class"]
// with Chroma's standard short class names.
type Span struct {
	Text, Class string
}

func (s Span) MarshalJSON() ([]byte, error) {
	if s.Class == "" {
		return json.Marshal([1]string{s.Text})
	}
	return json.Marshal([2]string{s.Text, s.Class})
}

// FlowSection is the Code Flow call index: the graph (nodes, edges, roots),
// the analysed modules, and the anchors of the changed files; Error is set
// instead when the analysis was unavailable.
type FlowSection struct {
	*flow.Graph
	Anchors map[string]string `json:"anchors,omitempty"`
	Modules []string          `json:"modules,omitempty"`
	Errors  []string          `json:"errors,omitempty"`
	Error   string            `json:"error,omitempty"`
}

// InspectView is the Go Deep Inspector data with the anchors of the
// changed files.
type InspectView struct {
	*InspectData
	Anchors map[string]string `json:"anchors"`
}

// Large diffs start collapsed so the report stays quick to open.
const (
	maxOpenFileLines  = 800
	maxOpenTotalLines = 4000
)

var statusNames = map[string]string{"A": "Added", "D": "Deleted", "R": "Renamed", "M": "Modified"}

func statusLetter(s string) string {
	switch s {
	case "?", "A":
		return "A"
	case "D":
		return "D"
	case "R", "C":
		return "R"
	}
	return "M"
}

// NewReportSection prepares the report for the page.
func NewReportSection(r Report) *ReportSection {
	s := &ReportSection{Project: r.Project, Branch: r.Branch, Root: r.Root, Base: r.Base, Generated: r.GeneratedAt,
		Scope: r.Scope, Query: r.Query, Omitted: r.Omitted, Warnings: r.Warnings, Tests: r.Tests, Study: r.Study,
		Hovers: r.Hovers, Files: []DiffFile{}}
	s.Added, s.Deleted = r.totals()
	total := 0
	anchors := map[string]string{}
	for i, f := range r.Files {
		df := diffFile(i, f, r.omitted(f.Path))
		total += df.LineCount
		df.Open = df.LineCount <= maxOpenFileLines && total <= maxOpenTotalLines
		s.Files = append(s.Files, df)
		if !df.Omitted && !df.Binary {
			anchors[f.Path] = df.ID
		}
	}
	if r.Flow != nil {
		fs := &FlowSection{Anchors: anchors, Modules: r.Flow.Modules, Errors: r.Flow.Errors, Error: r.Flow.Error}
		if fs.Error == "" && r.Flow.Graph == nil {
			fs.Error = "no Go code was analysed"
		}
		if fs.Error == "" {
			fs.Graph = r.Flow.Graph
		}
		s.Flow = fs
	}
	if r.Inspect != nil {
		s.Inspect = &InspectView{InspectData: r.Inspect, Anchors: anchors}
	}
	return s
}

func diffFile(i int, f gitdiff.File, omitted bool) DiffFile {
	letter := statusLetter(f.Status)
	df := DiffFile{ID: "file-" + strconv.Itoa(i+1), Path: f.Path, OldPath: f.OldPath, Status: letter,
		StatusName: statusNames[letter], Added: f.Added, Deleted: f.Deleted, Binary: f.Binary, Omitted: omitted,
		Hunks: []DiffHunk{}}
	if omitted {
		return df
	}
	lexer := highlight.Lexer(f.Path)
	for _, h := range f.Hunks {
		dh := DiffHunk{Range: h.Header, Section: h.Section, Lines: make([]DiffLine, len(h.Lines))}
		if k := strings.Index(h.Header[2:], "@@"); k >= 0 {
			dh.Range = h.Header[:k+4]
		}
		var newIdx, oldIdx []int
		var newTxt, oldTxt []string
		for k, l := range h.Lines {
			dl := DiffLine{Kind: "ctx", Old: l.OldNum, New: l.NewNum}
			switch l.Kind {
			case gitdiff.Added:
				dl.Kind, dl.Old = "add", 0
			case gitdiff.Removed:
				dl.Kind, dl.New = "del", 0
			}
			dh.Lines[k] = dl
			if l.Kind == gitdiff.Removed {
				oldIdx, oldTxt = append(oldIdx, k), append(oldTxt, l.Text)
			} else {
				newIdx, newTxt = append(newIdx, k), append(newTxt, l.Text)
			}
		}
		// Each side is highlighted as coherent source; spans reproduce the
		// original text exactly, so whitespace and tabs are preserved.
		for k, spans := range highlight.Lines(lexerFor(lexer, newTxt), newTxt) {
			dh.Lines[newIdx[k]].Spans = toSpans(spans)
		}
		for k, spans := range highlight.Lines(lexerFor(lexer, oldTxt), oldTxt) {
			dh.Lines[oldIdx[k]].Spans = toSpans(spans)
		}
		for k := range dh.Lines {
			if dh.Lines[k].Spans == nil {
				dh.Lines[k].Spans = []Span{} // a list, never null
			}
		}
		df.LineCount += len(h.Lines)
		df.Hunks = append(df.Hunks, dh)
	}
	return df
}

// maxHighlightLine is the longest line highlighted: beyond it (minified or
// generated code) a token per span would multiply the page's size for
// nothing, so the text stays plain.
const maxHighlightLine = 2000

func lexerFor(lexer chroma.Lexer, texts []string) chroma.Lexer {
	for _, t := range texts {
		if len(t) > maxHighlightLine {
			return nil
		}
	}
	return lexer
}

func toSpans(spans []highlight.Span) []Span {
	out := make([]Span, len(spans))
	for i, s := range spans {
		out[i] = Span{Text: s.Text, Class: highlight.Class(s.Tok)}
	}
	return out
}

// RuntimeSection is a runtime capture as the page reads it (section
// "runtime"): the analysis a and, for a comparison, the earlier capture
// (base) and the comparison itself. Captures lists the saved captures when
// the page is served (it can then open another one).
type RuntimeSection struct {
	A          *report.Analysis   `json:"a"`
	Base       *report.Analysis   `json:"base,omitempty"`
	Comparison *report.Comparison `json:"comparison,omitempty"`
	Generated  time.Time          `json:"generated"`
	Captures   any                `json:"captures,omitempty"`
}

// NewRuntimeSection prepares a capture (a, nil when there is none) and an
// optional comparison whose B side is a.
func NewRuntimeSection(a *report.Analysis, cmp *report.Comparison) *RuntimeSection {
	s := &RuntimeSection{A: slim(a), Comparison: cmp, Generated: time.Now()}
	if cmp != nil {
		s.Base = slim(cmp.A)
	}
	return s
}

func (s *Span) UnmarshalJSON(b []byte) error {
	var parts []string
	if err := json.Unmarshal(b, &parts); err != nil {
		return err
	}
	if len(parts) > 0 {
		s.Text = parts[0]
	}
	if len(parts) > 1 {
		s.Class = parts[1]
	}
	return nil
}

// slim drops the parts of the profile reports the page does not show
// (per-line statistics, call edges), without modifying a.
func slim(a *report.Analysis) *report.Analysis {
	if a == nil || (a.Profiles == nil && a.Perf == nil) {
		return a
	}
	c := *a
	var reports []**perf.Report
	if a.Profiles != nil {
		p := *a.Profiles
		c.Profiles = &p
		reports = append(reports, &p.CPU, &p.Allocs, &p.Inuse, &p.Mutex, &p.Block, &p.Goroutines)
	}
	if a.Perf != nil {
		p := *a.Perf
		c.Perf = &p
		reports = append(reports, &p.CPU)
	}
	for _, r := range reports {
		if *r != nil {
			x := **r
			x.Lines, x.Edges, x.Evidence = nil, nil, nil
			*r = &x
		}
	}
	return &c
}
