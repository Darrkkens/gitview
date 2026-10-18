package ai

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/athos/gitview/internal/audit"
)

// SourceReader returns the lines of a repository file (1-based slices are
// taken by the builder), or an error when the file is not available.
type SourceReader func(file string) ([]string, error)

// Context is what the model receives: the evidence of one finding or one
// path, small and verifiable. Every code snippet has an evidence ID the
// answer must cite.
type Context struct {
	Kind                string         `json:"kind"` // finding | path
	Finding             *FindingInfo   `json:"finding,omitempty"`
	Route               *RouteInfo     `json:"route,omitempty"`
	ExecutionPath       []StepInfo     `json:"execution_path"`
	DataFlow            []FlowInfo     `json:"data_flow"`
	SourceSnippets      []Snippet      `json:"source_snippets"`
	SecurityChecks      []CheckInfo    `json:"security_checks"`
	UnresolvedRelations []string       `json:"unresolved_relations"`
	Evidence            []EvidenceInfo `json:"evidence"`
	Omitted             []string       `json:"omitted,omitempty"` // left out to stay within the budget
}

type FindingInfo struct {
	ID             string   `json:"id"`
	RuleID         string   `json:"rule_id"`
	Title          string   `json:"title"`
	Category       string   `json:"category"`
	Severity       string   `json:"severity"`
	Confidence     string   `json:"confidence"`
	Verdict        string   `json:"verdict"`
	Message        string   `json:"message"`
	Why            string   `json:"why"`
	AnalysisStatus string   `json:"analysis_status"`
	Limits         []string `json:"limits,omitempty"`
}

type RouteInfo struct {
	Key  string `json:"key"`
	Auth string `json:"authentication"`
}

type StepInfo struct {
	Name  string `json:"name"`
	Role  string `json:"role"`
	File  string `json:"file,omitempty"`
	Line  int    `json:"line,omitempty"`
	Conf  string `json:"call_confidence,omitempty"`
	Check string `json:"security_check,omitempty"`
}

type FlowInfo struct {
	Role  string `json:"role"`
	Label string `json:"label"`
	Func  string `json:"function,omitempty"`
	File  string `json:"file,omitempty"`
	Line  int    `json:"line,omitempty"`
	Via   string `json:"via,omitempty"`
}

type CheckInfo struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
	File  string `json:"file,omitempty"`
	Line  int    `json:"line,omitempty"`
}

type EvidenceInfo struct {
	ID    string `json:"evidence_id"`
	Kind  string `json:"kind"`
	Basis string `json:"basis"`
	Text  string `json:"text"`
	File  string `json:"file,omitempty"`
	Line  int    `json:"line,omitempty"`
}

// Snippet is an excerpt of the analysed code, sent as data.
type Snippet struct {
	EvidenceID string `json:"evidence_id"`
	File       string `json:"file"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	Symbol     string `json:"symbol,omitempty"`
	Content    string `json:"content"`
}

// Limits bound what the builder sends.
type Limits struct {
	MaxTokens   int // estimated as characters / 4
	MaxPath     int // execution path steps
	Context     int // lines around an evidence line
	MaxSnippets int
}

func (l Limits) withDefaults() Limits {
	if l.MaxTokens <= 0 {
		l.MaxTokens = 6000
	}
	if l.MaxPath <= 0 {
		l.MaxPath = 14
	}
	if l.Context <= 0 {
		l.Context = 4
	}
	if l.MaxSnippets <= 0 {
		l.MaxSnippets = 12
	}
	return l
}

func estimate(v any) int {
	data, _ := json.Marshal(v)
	return len(data) / 4
}

type span struct {
	file       string
	from, to   int
	id, symbol string
	prio       int
}

// snippets reads the excerpts, merging overlapping ranges of a file, and
// redacts credentials. A secret's own line keeps none of its strings.
func snippets(spans []span, read SourceReader, secretLines map[string]bool, maxN int) ([]Snippet, []string) {
	sort.SliceStable(spans, func(i, j int) bool { return spans[i].prio < spans[j].prio })
	var out []Snippet
	var missing []string
	covered := map[string][][2]int{}
	files := map[string][]string{}
	for _, s := range spans {
		if len(out) >= maxN {
			missing = append(missing, fmt.Sprintf("snippet %s:%d (snippet limit)", s.file, s.from))
			continue
		}
		lines, ok := files[s.file]
		if !ok {
			var err error
			lines, err = read(s.file)
			if err != nil {
				missing = append(missing, s.file+": "+err.Error())
				files[s.file] = nil
				continue
			}
			files[s.file] = lines
		}
		if lines == nil {
			continue
		}
		from, to := max(1, s.from), min(len(lines), s.to)
		if from > to {
			continue
		}
		dup := false
		for _, r := range covered[s.file] {
			if from >= r[0] && to <= r[1] {
				dup = true
			}
		}
		if dup {
			continue // the same lines are already sent under another evidence ID
		}
		covered[s.file] = append(covered[s.file], [2]int{from, to})
		var b strings.Builder
		for n := from; n <= to; n++ {
			l := lines[n-1]
			if secretLines[fmt.Sprintf("%s:%d", s.file, n)] {
				l = audit.RedactStrings(l)
			}
			fmt.Fprintf(&b, "%d: %s\n", n, audit.Redact(l))
		}
		out = append(out, Snippet{EvidenceID: s.id, File: s.file, StartLine: from, EndLine: to, Symbol: s.symbol, Content: b.String()})
	}
	return out, missing
}

// BuildFindingContext gathers the evidence of a finding within the limits.
func BuildFindingContext(f *audit.Finding, read SourceReader, lim Limits) *Context {
	lim = lim.withDefaults()
	c := &Context{Kind: "finding", ExecutionPath: []StepInfo{}, DataFlow: []FlowInfo{}, SourceSnippets: []Snippet{}, SecurityChecks: []CheckInfo{}, UnresolvedRelations: []string{}, Evidence: []EvidenceInfo{}}
	c.Finding = &FindingInfo{ID: f.ID, RuleID: f.RuleID, Title: f.Title, Category: f.Category, Severity: f.Severity, Confidence: f.Confidence,
		Verdict: f.Verdict, Message: audit.Redact(f.Message), Why: f.Why, AnalysisStatus: f.AnalysisStatus, Limits: f.Limits}
	if len(f.Routes) > 0 {
		r := f.Routes[0]
		c.Route = &RouteInfo{Key: r.Key, Auth: r.Auth}
		for i, st := range r.Path {
			if i >= lim.MaxPath {
				c.Omitted = append(c.Omitted, fmt.Sprintf("%d more execution path steps", len(r.Path)-i))
				break
			}
			c.ExecutionPath = append(c.ExecutionPath, StepInfo{Name: st.Name, Role: st.Role, File: st.File, Line: st.Line, Conf: st.Conf, Check: st.Check})
		}
		if len(f.Routes) > 1 {
			c.Omitted = append(c.Omitted, fmt.Sprintf("%d other routes reach the same code", len(f.Routes)-1))
		}
	}
	for _, st := range f.Flow {
		c.DataFlow = append(c.DataFlow, FlowInfo{Role: st.Role, Label: audit.Redact(st.Label), Func: st.Func, File: st.File, Line: st.Line, Via: st.Via})
	}
	for _, ch := range f.Checks {
		c.SecurityChecks = append(c.SecurityChecks, CheckInfo{Kind: ch.Kind, Label: ch.Label, File: ch.File, Line: ch.Line})
	}
	c.UnresolvedRelations = append(c.UnresolvedRelations, f.Unresolved...)
	secret := map[string]bool{}
	var spans []span
	for k, e := range f.Evidence {
		c.Evidence = append(c.Evidence, EvidenceInfo{ID: e.ID, Kind: e.Kind, Basis: e.Basis, Text: audit.Redact(e.Text), File: e.File, Line: e.Line})
		if f.RuleID == "GO-DATA-001" && e.File != "" {
			secret[fmt.Sprintf("%s:%d", e.File, e.Line)] = true
		}
		if e.File == "" || e.Line <= 0 {
			continue
		}
		prio := 2 + k
		if e.Kind == audit.EvSink {
			prio = 0
		} else if e.Kind == audit.EvSource {
			prio = 1
		}
		spans = append(spans, span{file: e.File, from: e.Line - lim.Context, to: e.Line + lim.Context, id: e.ID, symbol: e.Symbol, prio: prio})
	}
	// The function holding the sink, when it is short enough.
	if f.Symbol != nil && f.Symbol.File != "" && f.Symbol.EndLine > 0 && f.Symbol.EndLine-f.Symbol.Line <= 60 {
		id := fmt.Sprintf("ev-%03d", len(f.Evidence)+1)
		c.Evidence = append(c.Evidence, EvidenceInfo{ID: id, Kind: "function", Basis: "static", Text: "declaration of " + f.Symbol.Qualified, File: f.Symbol.File, Line: f.Symbol.Line})
		spans = append(spans, span{file: f.Symbol.File, from: f.Symbol.Line, to: f.Symbol.EndLine, id: id, symbol: f.Symbol.Qualified, prio: -1})
	}
	c.trimEvidence(lim)
	c.fitSnippets(spans, read, secret, lim)
	return c
}

// trimEvidence keeps the evidence list within part of the budget: long
// texts are cut, and propagation steps in the middle of a long data path
// go first (sources, sinks, checks and absences always stay).
func (c *Context) trimEvidence(lim Limits) {
	for i := range c.Evidence {
		if len(c.Evidence[i].Text) > 240 {
			c.Evidence[i].Text = c.Evidence[i].Text[:239] + "…"
		}
	}
	dropped := 0
	for estimate(c) > lim.MaxTokens*6/10 {
		k := -1
		for i := len(c.Evidence) - 1; i >= 0; i-- {
			if kind := c.Evidence[i].Kind; kind == audit.EvPropagation || kind == audit.EvTransform {
				k = i
				break
			}
		}
		if k < 0 {
			break
		}
		c.Evidence = append(c.Evidence[:k], c.Evidence[k+1:]...)
		dropped++
	}
	if dropped > 0 {
		c.Omitted = append(c.Omitted, fmt.Sprintf("%d propagation evidence items (token budget)", dropped))
	}
	if len(c.DataFlow) > 2*lim.MaxPath {
		n := len(c.DataFlow)
		c.DataFlow = append(append([]FlowInfo{}, c.DataFlow[:lim.MaxPath]...), c.DataFlow[n-lim.MaxPath:]...)
		c.Omitted = append(c.Omitted, fmt.Sprintf("%d data flow steps in the middle", n-2*lim.MaxPath))
	}
}

// fitSnippets adds snippets while the context stays within the budget.
func (c *Context) fitSnippets(spans []span, read SourceReader, secret map[string]bool, lim Limits) {
	all, missing := snippets(spans, read, secret, lim.MaxSnippets)
	for _, m := range missing {
		c.Omitted = append(c.Omitted, m)
	}
	for _, s := range all {
		if estimate(c)+estimate(s) > lim.MaxTokens {
			c.Omitted = append(c.Omitted, fmt.Sprintf("snippet %s:%d-%d (token budget)", s.File, s.StartLine, s.EndLine))
			continue
		}
		c.SourceSnippets = append(c.SourceSnippets, s)
	}
}

// PathStep is one node of a path to explain (from the Repository Graph).
type PathStep struct {
	ID, Name, Role, File string
	Line, EndLine        int
	Conf                 string // of the edge into the step
	Check                string
}

// BuildPathContext gathers a path between two nodes: its steps with their
// code, and the data flows and checks of the findings along it.
func BuildPathContext(steps []PathStep, related []*audit.Finding, unresolved []string, read SourceReader, lim Limits) *Context {
	lim = lim.withDefaults()
	c := &Context{Kind: "path", ExecutionPath: []StepInfo{}, DataFlow: []FlowInfo{}, SourceSnippets: []Snippet{}, SecurityChecks: []CheckInfo{}, UnresolvedRelations: append([]string{}, unresolved...), Evidence: []EvidenceInfo{}}
	var spans []span
	for i, st := range steps {
		if i >= lim.MaxPath {
			c.Omitted = append(c.Omitted, fmt.Sprintf("%d more steps", len(steps)-i))
			break
		}
		c.ExecutionPath = append(c.ExecutionPath, StepInfo{Name: st.Name, Role: st.Role, File: st.File, Line: st.Line, Conf: st.Conf, Check: st.Check})
		if st.Check != "" {
			c.SecurityChecks = append(c.SecurityChecks, CheckInfo{Kind: "check", Label: st.Name + ": " + st.Check, File: st.File, Line: st.Line})
		}
		if st.File == "" || st.Line <= 0 {
			continue
		}
		id := fmt.Sprintf("ev-%03d", len(c.Evidence)+1)
		c.Evidence = append(c.Evidence, EvidenceInfo{ID: id, Kind: "path-step", Basis: "static", Text: st.Role + " " + st.Name, File: st.File, Line: st.Line})
		to := st.Line + lim.Context*2
		if st.EndLine > st.Line && st.EndLine-st.Line <= 40 {
			to = st.EndLine
		}
		spans = append(spans, span{file: st.File, from: st.Line, to: to, id: id, symbol: st.Name, prio: i})
	}
	for _, f := range related {
		for _, st := range f.Flow {
			c.DataFlow = append(c.DataFlow, FlowInfo{Role: st.Role, Label: audit.Redact(st.Label), Func: st.Func, File: st.File, Line: st.Line, Via: st.Via})
		}
		for _, ch := range f.Checks {
			c.SecurityChecks = append(c.SecurityChecks, CheckInfo{Kind: ch.Kind, Label: ch.Label, File: ch.File, Line: ch.Line})
		}
		id := fmt.Sprintf("ev-%03d", len(c.Evidence)+1)
		c.Evidence = append(c.Evidence, EvidenceInfo{ID: id, Kind: "finding", Basis: "static", Text: f.RuleID + " " + f.Title + ": " + audit.Redact(f.Message), File: f.File, Line: f.Line})
	}
	c.fitSnippets(spans, read, map[string]bool{}, lim)
	return c
}
