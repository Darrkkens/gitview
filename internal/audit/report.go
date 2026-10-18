package audit

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/athos/gitview/internal/evidence"
	"github.com/athos/gitview/internal/graph/model"
)

// Page is what the Security Journey page embeds: the audit, the review
// decisions, the AI investigations that were asked for, an optional
// comparison with the previous audit, and only the code snippets the
// findings point to (never whole files), redacted.
type Page struct {
	Audit      *Audit                     `json:"audit"`
	Reviews    map[string]*ReviewRecord   `json:"reviews"`
	Revalidate map[string]string          `json:"revalidate,omitempty"`
	AI         map[string]json.RawMessage `json:"ai,omitempty"` // finding ID → investigation
	Diff       *Diff                      `json:"diff,omitempty"`
	Snippets   map[string][]Excerpt       `json:"snippets"`
	Omitted    []string                   `json:"omitted,omitempty"`
	Live       bool                       `json:"live"`
}

// Excerpt is a range of lines of a repository file.
type Excerpt struct {
	From  int      `json:"from"`
	To    int      `json:"to"`
	Lines []string `json:"lines"`
}

// Limits of the embedded code.
const (
	excerptContext = 3
	maxFuncLines   = 120
	maxPageLines   = 25000
)

// BuildPage assembles the page data. read returns the lines of a file
// (nil when unavailable: the finding then says so in the page).
func BuildPage(a *Audit, rv *Reviews, read func(file string) ([]string, error), diff *Diff, investigations map[string]json.RawMessage) *Page {
	p := &Page{Audit: a, Reviews: map[string]*ReviewRecord{}, Revalidate: map[string]string{}, AI: investigations, Diff: diff, Snippets: map[string][]Excerpt{}}
	if rv != nil {
		for _, f := range a.Findings {
			if rec := rv.Findings[f.ID]; rec != nil {
				p.Reviews[f.ID] = rec
			}
			if r := rv.Revalidation(f); r != "" {
				p.Revalidate[f.ID] = r
			}
		}
	}
	type rng struct{ from, to int }
	want := map[string][]rng{}
	secret := map[string]map[int]bool{}
	add := func(file string, from, to int) {
		if file == "" || file == "go.mod" || from <= 0 {
			return
		}
		want[file] = append(want[file], rng{max(1, from), max(from, to)})
	}
	for _, f := range a.Findings {
		if f.RuleID == "GO-DATA-001" {
			if secret[f.File] == nil {
				secret[f.File] = map[int]bool{}
			}
			secret[f.File][f.Line] = true
		}
		if f.Symbol != nil && f.Symbol.File != "" && f.Symbol.EndLine >= f.Symbol.Line {
			if f.Symbol.EndLine-f.Symbol.Line <= maxFuncLines {
				add(f.Symbol.File, f.Symbol.Line, f.Symbol.EndLine)
			} else {
				add(f.Symbol.File, f.Line-20, f.Line+20)
			}
		}
		add(f.File, f.Line-excerptContext, f.Line+excerptContext)
		for _, e := range f.Evidence {
			add(e.File, e.Line-excerptContext, e.Line+excerptContext)
		}
		for _, st := range f.Flow {
			add(st.File, st.Line-2, st.Line+2)
		}
		for _, r := range f.Routes {
			for _, st := range r.Path {
				add(st.File, st.Line, st.Line+12)
			}
		}
	}
	files := make([]string, 0, len(want))
	for f := range want {
		files = append(files, f)
	}
	sort.Strings(files)
	total := 0
	for _, file := range files {
		lines, err := read(file)
		if err != nil || lines == nil {
			p.Omitted = append(p.Omitted, file+": not available")
			continue
		}
		rs := want[file]
		sort.Slice(rs, func(i, j int) bool { return rs[i].from < rs[j].from })
		var merged []rng
		for _, r := range rs {
			r.to = min(r.to, len(lines))
			if r.from > r.to {
				continue
			}
			if n := len(merged); n > 0 && r.from <= merged[n-1].to+1 {
				merged[n-1].to = max(merged[n-1].to, r.to)
				continue
			}
			merged = append(merged, r)
		}
		for _, r := range merged {
			if total+r.to-r.from+1 > maxPageLines {
				p.Omitted = append(p.Omitted, fmt.Sprintf("%s:%d-%d (page limit of %d lines)", file, r.from, r.to, maxPageLines))
				continue
			}
			ex := Excerpt{From: r.from, To: r.to}
			for n := r.from; n <= r.to; n++ {
				l := lines[n-1]
				if secret[file][n] {
					l = RedactStrings(l)
				}
				ex.Lines = append(ex.Lines, Redact(l))
			}
			total += len(ex.Lines)
			p.Snippets[file] = append(p.Snippets[file], ex)
		}
	}
	return p
}

// Annotate attaches the findings to the Repository Graph (the nodes of
// their functions and their routes), for the dataset's consumers. The graph
// builder never writes annotations; this is the audit's.
func Annotate(g *model.Graph, a *Audit) {
	idx := g.Index()
	routes := map[string]*model.Route{}
	if g.HTTP != nil {
		for _, r := range g.HTTP.Routes {
			routes[r.ID] = r
		}
	}
	for _, f := range a.Findings {
		var ev []evidence.Evidence
		for _, e := range f.Evidence {
			if e.Kind == EvSink || e.Kind == EvSource {
				ev = append(ev, evidence.Evidence{Kind: evidence.Static, Basis: evidence.BasisStatic, File: e.File, Line: e.Line, SymbolID: e.Symbol, Source: e.Source, Confidence: e.Confidence, Details: e.Text})
			}
		}
		an := model.Annotation{Kind: "security:" + f.RuleID, Severity: f.Severity, Title: f.Title, Details: f.VerdictLabel + " · " + f.ID + " · " + f.Message, Evidence: ev}
		if n := idx[f.SymbolID]; n != nil {
			n.Annotations = append(n.Annotations, an)
		}
		for _, r := range f.Routes {
			if rt := routes[r.ID]; rt != nil {
				rt.Annotations = append(rt.Annotations, an)
			}
		}
	}
}
