package export

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"strconv"
	"strings"
	"time"

	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/gitdiff"
	"github.com/athos/gitview/internal/highlight"
)

// Large diffs start collapsed so the report stays quick to open.
const (
	maxOpenFileLines  = 800
	maxOpenTotalLines = 4000
)

type htmlSpan struct {
	Text  string
	Class string
}

type htmlLine struct {
	Kind     string // add | del | ctx
	Old, New int    // 0 when the line does not exist on that side
	Sign     string
	Spans    []htmlSpan
	Empty    bool
}

type htmlHunk struct {
	Range   string // "@@ -10,3 +10,4 @@"
	Section string
	Lines   []htmlLine
}

type htmlFile struct {
	ID         string
	Path       string
	Name       string
	Dir        string // with trailing slash, for the diff header
	DirLabel   string // without it, for the overview list
	OldPath    string
	Letter     string
	StatusName string
	Added      int
	Deleted    int
	Bar        []string // five diffstat blocks: add | del | none
	Binary     bool
	Omitted    bool
	Open       bool
	LineCount  int
	NumDigits  int
	Hunks      []htmlHunk
}

type htmlPackage struct {
	Name     string
	Tests    []htmlCase
	Outcomes []htmlCase // package-level results that are not individual tests
}

type htmlCase struct {
	TestCase
	Label    string
	Glyph    string
	Class    string
	Duration string
}

type htmlTests struct {
	*TestReport
	Summary   []htmlStat
	Status    string
	Class     string
	Duration  string
	Started   string
	Packages  []htmlPackage
	Failures  []htmlCase
	LogText   string
	LogShown  int
	LogHidden int
}

type htmlStat struct {
	Text  string
	Class string
}

type htmlData struct {
	R           Report
	Added       int
	Deleted     int
	Generated   string
	GeneratedTS string
	BaseDate    string
	Files       []htmlFile
	Tests       *htmlTests
	TestsLine   htmlStat
	CSP         string
	ThemeCSS    template.CSS
	Style       template.CSS
	HeadJS      template.JS
	JS          template.JS
	FlowJS      template.JS
	FlowJSON    template.JS // inert JSON data block, HTML-escaped by encoding/json
	HasFlow     bool
	FlowError   string
	FlowStats   string
	InspectJS   template.JS
	InspectJSON template.JS // inert JSON data block for the Go Deep Inspector
	HasInspect  bool
	InspectInfo string
	StudyJS     template.JS
	StudyJSON   template.JS // inert JSON data block for Study Mode
	HasStudy    bool
	StudyInfo   string
}

func renderHTML(r Report) ([]byte, error) {
	d := htmlData{
		R:           r,
		Generated:   r.GeneratedAt.Format("Jan 2, 2006 15:04 MST"),
		GeneratedTS: r.GeneratedAt.Format(time.RFC3339),
		CSP:         contentSecurityPolicy,
		ThemeCSS:    themeCSS,
		Style:       template.CSS(reportCSS + flowCSS + inspectCSS + studyCSS),
		HeadJS:      template.JS(headJS),
		JS:          template.JS(reportJS),
		FlowJS:      template.JS(flowJS),
		InspectJS:   template.JS(inspectJS),
		StudyJS:     template.JS(studyJS),
	}
	d.Added, d.Deleted = r.totals()
	if r.Base != nil {
		if t, err := time.Parse(time.RFC3339, r.Base.Date); err == nil {
			d.BaseDate = t.Format("Jan 2, 2006 15:04")
		}
	}
	total := 0
	for i, f := range r.Files {
		hf := buildFile(i, f, r.omitted(f.Path))
		total += hf.LineCount
		hf.Open = hf.LineCount <= maxOpenFileLines && total <= maxOpenTotalLines
		d.Files = append(d.Files, hf)
	}
	d.Tests, d.TestsLine = buildTests(r.Tests)
	if err := d.addFlow(r); err != nil {
		return nil, err
	}
	if err := d.addInspect(r); err != nil {
		return nil, err
	}
	if r.Study != nil {
		data, err := json.Marshal(r.Study)
		if err != nil {
			return nil, err
		}
		d.HasStudy, d.StudyJSON = true, template.JS(data)
		d.StudyInfo = fmt.Sprintf("%d steps to read · %d changed files", len(r.Study.ReadingOrder), r.Study.Summary.ChangedFiles)
	}

	var buf bytes.Buffer
	if err := htmlTmpl.Execute(&buf, d); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

var statusNames = map[string]string{"A": "Added", "D": "Deleted", "R": "Renamed", "M": "Modified"}

// addFlow embeds the call index as JSON. encoding/json escapes <, > and &,
// so repository content can never close the data block or inject markup.
func (d *htmlData) addFlow(r Report) error {
	if r.Flow == nil {
		return nil
	}
	d.HasFlow = true
	if r.Flow.Error != "" || r.Flow.Graph == nil {
		d.FlowError = r.Flow.Error
		if d.FlowError == "" {
			d.FlowError = "no Go code was analysed"
		}
		return nil
	}
	anchors := map[string]string{}
	for _, f := range d.Files {
		if !f.Omitted && !f.Binary {
			anchors[f.Path] = f.ID
		}
	}
	funcs := 0
	for _, n := range r.Flow.Graph.Nodes {
		if n.Kind == flow.KindFunc || n.Kind == flow.KindClosure {
			funcs++
		}
	}
	d.FlowStats = fmt.Sprintf("%d functions · %d calls indexed", funcs, len(r.Flow.Graph.Edges))
	payload := struct {
		*flow.Graph
		Anchors map[string]string `json:"anchors"`
		Modules []string          `json:"modules"`
		Errors  []string          `json:"errors,omitempty"`
	}{r.Flow.Graph, anchors, r.Flow.Modules, r.Flow.Errors}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	d.FlowJSON = template.JS(data)
	return nil
}

// addInspect embeds the Go Deep Inspector results as inert JSON, escaped by
// encoding/json like the Code Flow index.
func (d *htmlData) addInspect(r Report) error {
	if r.Inspect == nil {
		return nil
	}
	d.HasInspect = true
	anchors := map[string]string{}
	for _, f := range d.Files {
		if !f.Omitted && !f.Binary {
			anchors[f.Path] = f.ID
		}
	}
	n := len(r.Inspect.Symbols)
	d.InspectInfo = fmt.Sprintf("%d function(s)", n)
	var runtime []string
	if r.Inspect.Trace != nil {
		runtime = append(runtime, "trace")
	}
	if r.Inspect.CPU != nil || r.Inspect.Mem != nil {
		runtime = append(runtime, "profiles")
	}
	if len(r.Inspect.Benches) > 0 {
		runtime = append(runtime, "benchmarks")
	}
	if r.Inspect.Escape != nil {
		runtime = append(runtime, "compiler diagnostics")
	}
	if len(r.Inspect.Plans) > 0 {
		runtime = append(runtime, "query plans")
	}
	if len(runtime) > 0 {
		d.InspectInfo += " · runtime data: " + strings.Join(runtime, ", ")
	} else {
		d.InspectInfo += " · no runtime data"
	}
	payload := struct {
		*InspectData
		Anchors map[string]string `json:"anchors"`
	}{r.Inspect, anchors}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	d.InspectJSON = template.JS(data)
	return nil
}

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

func buildFile(i int, f gitdiff.File, omitted bool) htmlFile {
	name, dir := f.Path, ""
	if j := strings.LastIndexByte(f.Path, '/'); j >= 0 {
		name, dir = f.Path[j+1:], f.Path[:j+1]
	}
	letter := statusLetter(f.Status)
	hf := htmlFile{
		ID: "file-" + strconv.Itoa(i+1), Path: f.Path, Name: name, Dir: dir, DirLabel: strings.TrimSuffix(dir, "/"), OldPath: f.OldPath,
		Letter: letter, StatusName: statusNames[letter], Added: f.Added, Deleted: f.Deleted,
		Binary: f.Binary, Omitted: omitted, Bar: diffstatBar(f.Added, f.Deleted),
	}
	if omitted {
		return hf
	}
	lexer := highlight.Lexer(f.Path)
	maxNum := 0
	for _, h := range f.Hunks {
		hh := htmlHunk{Range: h.Header, Section: h.Section, Lines: make([]htmlLine, len(h.Lines))}
		if k := strings.Index(h.Header[2:], "@@"); k >= 0 {
			hh.Range = h.Header[:k+4]
		}
		var newIdx, oldIdx []int
		var newTxt, oldTxt []string
		for k, l := range h.Lines {
			hl := htmlLine{Kind: "ctx", Sign: " ", Old: l.OldNum, New: l.NewNum, Empty: strings.TrimSpace(l.Text) == ""}
			switch l.Kind {
			case gitdiff.Added:
				hl.Kind, hl.Sign, hl.Old = "add", "+", 0
			case gitdiff.Removed:
				hl.Kind, hl.Sign, hl.New = "del", "-", 0
			}
			maxNum = max(maxNum, l.OldNum, l.NewNum)
			hh.Lines[k] = hl
			if l.Kind == gitdiff.Removed {
				oldIdx, oldTxt = append(oldIdx, k), append(oldTxt, l.Text)
			} else {
				newIdx, newTxt = append(newIdx, k), append(newTxt, l.Text)
			}
		}
		// Each side is highlighted as coherent source; spans reproduce the
		// original text exactly, so whitespace and tabs are preserved.
		for k, spans := range highlight.Lines(lexer, newTxt) {
			hh.Lines[newIdx[k]].Spans = toHTMLSpans(spans)
		}
		for k, spans := range highlight.Lines(lexer, oldTxt) {
			hh.Lines[oldIdx[k]].Spans = toHTMLSpans(spans)
		}
		hf.LineCount += len(h.Lines)
		hf.Hunks = append(hf.Hunks, hh)
	}
	hf.NumDigits = max(len(strconv.Itoa(maxNum)), 2)
	return hf
}

func toHTMLSpans(spans []highlight.Span) []htmlSpan {
	out := make([]htmlSpan, len(spans))
	for i, s := range spans {
		out[i] = htmlSpan{Text: s.Text, Class: highlight.Class(s.Tok)}
	}
	return out
}

// diffstatBar distributes five blocks proportionally between additions and
// deletions, like the compact bars of code review tools.
func diffstatBar(add, del int) []string {
	bar := []string{"none", "none", "none", "none", "none"}
	total := add + del
	if total == 0 {
		return bar
	}
	filled := min(total, 5)
	adds := (add*filled + total/2) / total
	if add > 0 && adds == 0 {
		adds = 1
	}
	if del > 0 && adds == filled {
		adds = filled - 1
	}
	for i := 0; i < filled; i++ {
		if i < adds {
			bar[i] = "add"
		} else {
			bar[i] = "del"
		}
	}
	return bar
}

var packageLabels = map[string]string{
	"(falha de compilação)": "Build failed",
	"(pacote)":              "Package failed",
	"(arquivo de teste)":    "Test file failed to run",
}

var testStatus = map[string]struct{ glyph, class, label string }{
	"passed":    {"✓", "ok", "passed"},
	"failed":    {"✗", "bad", "failed"},
	"skipped":   {"○", "muted", "skipped"},
	"cancelled": {"○", "warn", "cancelled"},
	"error":     {"!", "warn", "did not finish"},
	"running":   {"…", "warn", "running"},
	"notrun":    {"·", "muted", "not run"},
}

func buildTests(t *TestReport) (*htmlTests, htmlStat) {
	if t == nil || !t.Ran {
		return nil, htmlStat{"Tests not run", "muted"}
	}
	ht := &htmlTests{TestReport: t}
	switch t.Status {
	case "passed":
		ht.Status, ht.Class = "Passed", "ok"
	case "failed":
		ht.Status, ht.Class = "Failed", "bad"
	case "cancelled":
		ht.Status, ht.Class = "Cancelled", "warn"
	case "running":
		ht.Status, ht.Class = "Running", "warn"
	default:
		ht.Status, ht.Class = "Did not complete", "warn"
	}
	line := htmlStat{"Tests " + strings.ToLower(ht.Status), ht.Class}
	if t.Structured {
		ht.Summary = []htmlStat{
			{fmt.Sprintf("%d passed", t.Passed), classIf(t.Passed > 0, "ok")},
			{fmt.Sprintf("%d failed", t.Failed), classIf(t.Failed > 0, "bad")},
		}
		if t.Skipped > 0 {
			ht.Summary = append(ht.Summary, htmlStat{fmt.Sprintf("%d skipped", t.Skipped), "muted"})
		}
	} else {
		ht.Summary = []htmlStat{{fmt.Sprintf("exit code %d", t.ExitCode), classIf(t.ExitCode != 0, "bad")}}
	}
	if !t.Finished.IsZero() {
		ht.Duration = t.Finished.Sub(t.Started).Round(time.Millisecond).String()
	}
	if !t.Started.IsZero() {
		ht.Started = t.Started.Format("Jan 2, 2006 15:04:05")
	}

	index := map[string]int{}
	for _, c := range t.Cases {
		hc := htmlCase{TestCase: c, Label: c.Name}
		st, ok := testStatus[c.Status]
		if !ok {
			st = testStatus["notrun"]
		}
		hc.Glyph, hc.Class = st.glyph, st.class
		if c.ElapsedMs > 0 {
			hc.Duration = strconv.FormatInt(c.ElapsedMs, 10) + " ms"
		}
		i, seen := index[c.Package]
		if !seen {
			i = len(ht.Packages)
			index[c.Package] = i
			ht.Packages = append(ht.Packages, htmlPackage{Name: c.Package})
		}
		if c.Kind == "package" {
			if l, ok := packageLabels[c.Name]; ok {
				hc.Label = l
			}
			ht.Packages[i].Outcomes = append(ht.Packages[i].Outcomes, hc)
		} else {
			ht.Packages[i].Tests = append(ht.Packages[i].Tests, hc)
		}
		if (c.Status == "failed" || c.Status == "error") && c.Output != "" {
			ht.Failures = append(ht.Failures, hc)
		}
	}
	ht.LogText = strings.Join(t.Log, "\n")
	ht.LogShown = len(t.Log)
	ht.LogHidden = max(t.LogTotal-len(t.Log), 0)
	return ht, line
}

func classIf(cond bool, class string) string {
	if cond {
		return class
	}
	return "muted"
}

var themeCSS = template.CSS(
	highlight.ThemeCSS("catppuccin-mocha", `:root[data-theme="dark"] .cd`) +
		highlight.ThemeCSS("catppuccin-latte", `:root[data-theme="light"] .cd`))

// contentSecurityPolicy forbids every network request and allows only the
// report's own scripts, identified by hash, so nothing injected can run.
var contentSecurityPolicy = fmt.Sprintf(
	"default-src 'none'; style-src 'unsafe-inline'; img-src data:; script-src '%s' '%s' '%s' '%s' '%s'; base-uri 'none'; form-action 'none'",
	scriptHash(headJS), scriptHash(reportJS), scriptHash(flowJS), scriptHash(inspectJS), scriptHash(studyJS))

func scriptHash(js string) string {
	sum := sha256.Sum256([]byte(js))
	return "sha256-" + base64.StdEncoding.EncodeToString(sum[:])
}

var htmlTmpl = template.Must(template.New("report").Funcs(template.FuncMap{
	"plural": func(n int, one, many string) string {
		if n == 1 {
			return fmt.Sprintf("%d %s", n, one)
		}
		return fmt.Sprintf("%d %s", n, many)
	},
	"digits": func(n int) template.CSS { return template.CSS("--d:" + strconv.Itoa(n)) },
}).Parse(htmlSource))
