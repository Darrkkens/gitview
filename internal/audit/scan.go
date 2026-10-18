package audit

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/athos/gitview/internal/evidence"
	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/graph/analyzer"
	"github.com/athos/gitview/internal/graph/model"
	"golang.org/x/tools/go/ssa"
)

// Input is what a scan reads: the loaded programs and the Repository Graph
// built from them (its HTTP catalog included). Nothing else is loaded.
type Input struct {
	Programs []*flow.Program
	Graph    *model.Graph
	Root     string // repository root, for go.mod files
	Commit   string
	Dirty    bool
	Version  string // GitView version, written into the audit
}

// Options select rules and limits.
type Options struct {
	Rules        []string // only these rule IDs (empty: all)
	Disable      []string // rule IDs to skip
	VulnDB       string   // local OSV database directory (GO-DEP-001); empty: dependencies not checked
	IncludeTests bool     // also look for secrets in _test.go files
	MaxDepth     int      // function boundaries Data Flow crosses (default 4)
	Now          func() time.Time
}

type scanner struct {
	ctx    context.Context
	in     Input
	opt    Options
	g      *model.Graph
	nodes  map[string]*model.Node
	routes map[string]*model.Route
	inEdge map[string][]*model.Edge // calls, go, defer, handles entering a node
	outE   map[string][]*model.Edge
	audit  *Audit
	anchor map[string]int
	status evidence.Completeness
	traces map[ssa.Value]*taint
	fnProg map[*ssa.Function]*flow.Program
}

var flowEdge = map[string]bool{model.EdgeCalls: true, model.EdgeGo: true, model.EdgeDefer: true, model.EdgeHandles: true, model.EdgeQueries: true}

// Scan runs the enabled rules. It does not execute or modify the analysed
// code, and makes no network request.
func Scan(ctx context.Context, in Input, opt Options) (*Audit, error) {
	if in.Graph == nil {
		return nil, fmt.Errorf("audit: no repository graph")
	}
	if opt.MaxDepth <= 0 {
		opt.MaxDepth = 4
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	start := time.Now()
	s := &scanner{ctx: ctx, in: in, opt: opt, g: in.Graph, nodes: in.Graph.Index(), routes: map[string]*model.Route{},
		inEdge: map[string][]*model.Edge{}, outE: map[string][]*model.Edge{}, anchor: map[string]int{}, traces: map[ssa.Value]*taint{},
		fnProg: map[*ssa.Function]*flow.Program{}}
	for _, e := range in.Graph.Edges {
		if flowEdge[e.Type] {
			s.inEdge[e.Target] = append(s.inEdge[e.Target], e)
			s.outE[e.Source] = append(s.outE[e.Source], e)
		}
	}
	if in.Graph.HTTP != nil {
		for _, r := range in.Graph.HTTP.Routes {
			s.routes[r.ID] = r
		}
	}
	for _, p := range in.Programs {
		for _, fn := range p.Funcs() {
			s.fnProg[fn] = p
		}
	}
	s.audit = &Audit{Schema: Schema, RulesVersion: RulesVersion, AnalyzerVersion: in.Version, Repository: in.Graph.Repository,
		Commit: in.Commit, Dirty: in.Dirty, Generated: opt.Now().UTC(), Modules: in.Graph.Modules}
	if in.Graph.Completeness.Status != "" && in.Graph.Completeness.Status != evidence.Complete {
		for _, r := range in.Graph.Completeness.Reasons {
			s.status.Limit("repository graph: " + r)
		}
	}
	only := map[string]bool{}
	for _, id := range opt.Rules {
		only[strings.ToUpper(id)] = true
	}
	off := map[string]bool{}
	for _, id := range opt.Disable {
		off[strings.ToUpper(id)] = true
	}
	for _, r := range catalog {
		info := r.RuleInfo
		info.Enabled = !off[r.ID] && (len(only) == 0 || only[r.ID])
		before := len(s.audit.Findings)
		if info.Enabled {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			r.run(s, r)
		}
		info.Findings = len(s.audit.Findings) - before
		s.audit.Rules = append(s.audit.Rules, info)
	}
	s.posture()
	sort.SliceStable(s.audit.Findings, func(i, j int) bool {
		a, b := s.audit.Findings[i], s.audit.Findings[j]
		if sevRank[a.Severity] != sevRank[b.Severity] {
			return sevRank[a.Severity] > sevRank[b.Severity]
		}
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})
	s.status.Done()
	s.audit.Status = s.status
	s.audit.DurationMS = time.Since(start).Milliseconds()
	return s.audit, nil
}

var sevRank = map[string]int{SevCritical: 5, SevHigh: 4, SevMedium: 3, SevLow: 2, SevInfo: 1}

// ---------- building findings ----------

// newFinding starts a finding of rule r at a sensitive call in fn. The
// anchor (rule, function, normalised code of the call) is the identity;
// several identical calls in one function are numbered in order.
func (s *scanner) newFinding(r *rule, p *flow.Program, fn *ssa.Function, file string, line int, anchorText string) *Finding {
	f := &Finding{RuleID: r.ID, Title: r.Title, Category: r.Category, Severity: r.Severity, Origin: "static-rule",
		Why: r.Why, Remediation: append([]string(nil), r.Remediation...), Tests: append([]string(nil), r.Tests...),
		File: file, Line: line, AnalysisStatus: string(evidence.Complete)}
	sym := ""
	if fn != nil && p != nil {
		ref := p.Ref(flow.Origin(fn))
		f.Symbol = &ref
		f.SymbolID = analyzer.FuncID(ref.ID)
		sym = ref.ID
	}
	code := normCode(anchorText)
	if code == "" && p != nil {
		code = normCode(p.Source(file, line, line))
	}
	base := r.ID + "\x00" + sym + "\x00" + code
	if sym == "" {
		base += "\x00" + file // file-level findings (configuration, dependencies)
	}
	n := s.anchor[base]
	s.anchor[base] = n + 1
	f.Anchor = fmt.Sprintf("%s|%s|%s#%d", r.ID, sym, code, n)
	f.ID = stableID("finding:"+r.Category+":", base, fmt.Sprint(n))
	f.CodeHash = stableID("", code)
	return f
}

// ev appends evidence with the next ev-NNN identifier of the finding.
func (f *Finding) ev(e Evidence) string {
	e.ID = fmt.Sprintf("ev-%03d", len(f.Evidence)+1)
	if e.Basis == "" {
		e.Basis = "static"
	}
	e.Code = Redact(e.Code)
	f.Evidence = append(f.Evidence, e)
	return e.ID
}

func (f *Finding) limit(reason string) {
	f.AnalysisStatus = string(evidence.Partial)
	for _, l := range f.Limits {
		if l == reason {
			return
		}
	}
	f.Limits = append(f.Limits, reason)
}

func (s *scanner) add(f *Finding) {
	if f.Verdict == "" {
		f.Verdict = Review
	}
	if f.VerdictLabel == "" {
		f.VerdictLabel = verdictLabel(f)
	}
	if f.Confidence == "" {
		f.Confidence = ConfPossible
	}
	s.audit.Findings = append(s.audit.Findings, f)
}

func verdictLabel(f *Finding) string {
	switch f.Verdict {
	case Vulnerable:
		return "Vulnerable — " + f.Title
	case Incomplete:
		return "Analysis Incomplete — " + f.Title
	}
	return "Review Required — " + f.Title
}

// progOf returns the program whose module holds a repository-relative file.
func (s *scanner) progOf(file string) *flow.Program {
	var best *flow.Program
	for _, p := range s.in.Programs {
		rel := p.RelFile(p.Dir)
		if rel == "." || rel == "" || strings.HasPrefix(file, rel+"/") {
			if best == nil || len(p.Dir) > len(best.Dir) {
				best = p
			}
		}
	}
	return best
}

// code returns the source line, trimmed and with secrets redacted.
func code(p *flow.Program, file string, line int) string {
	if p == nil || file == "" || line <= 0 {
		return ""
	}
	return Redact(strings.TrimSpace(p.Source(file, line, line)))
}

// ---------- routes and execution paths ----------

const maxRouteDepth = 12

// rvisit is a step of the backward search from a finding to the routes:
// next is the element one call closer to the finding, edge the relation
// from this element to next.
type rvisit struct {
	id   string
	next *rvisit
	edge *model.Edge
	d    int
}

// linkRoutes finds the routes whose handler chain may reach the function
// (through the call edges of the Repository Graph) and records each path.
func (s *scanner) linkRoutes(f *Finding) {
	if f.SymbolID == "" {
		return
	}
	seen := map[string]bool{f.SymbolID: true}
	queue := []*rvisit{{id: f.SymbolID}}
	limited := false
	for len(queue) > 0 && len(f.Routes) < 25 {
		v := queue[0]
		queue = queue[1:]
		if r := s.routes[v.id]; r != nil {
			f.Routes = append(f.Routes, s.routePath(f, r, v))
			continue
		}
		if v.d >= maxRouteDepth {
			limited = true
			continue
		}
		for _, e := range s.inEdge[v.id] {
			if seen[e.Source] {
				continue
			}
			seen[e.Source] = true
			queue = append(queue, &rvisit{id: e.Source, next: v, edge: e, d: v.d + 1})
		}
	}
	if limited {
		f.limit(fmt.Sprintf("routes more than %d calls away are not linked", maxRouteDepth))
	}
	sort.Slice(f.Routes, func(i, j int) bool { return f.Routes[i].Key < f.Routes[j].Key })
}

// routePath is the route, its middleware chain, and the calls from the
// chain element that leads to the finding down to the finding's function.
func (s *scanner) routePath(f *Finding, r *model.Route, v *rvisit) RouteRef {
	ref := RouteRef{ID: r.ID, Key: r.Key, Auth: authText(r)}
	ref.Path = append(ref.Path, PathStep{ID: r.ID, Name: r.Key, Role: "route", File: r.File, Line: r.Line, Conf: ConfConfirmed})
	// When the finding is in a middleware of the chain, the path ends there:
	// the elements after it are not on the way.
	if v.next != nil && v.next.next == nil {
		for k, m := range r.Middlewares {
			if m.ID != f.SymbolID {
				continue
			}
			for _, x := range r.Middlewares[:k+1] {
				ref.Path = append(ref.Path, PathStep{ID: x.ID, Name: x.Name, Role: "middleware", File: x.File, Line: x.Line, Conf: x.Confidence, Check: s.chainCheck(r, x.Name)})
			}
			ref.Path[len(ref.Path)-1].Role = "sink"
			return ref
		}
	}
	inChain := map[string]bool{}
	for _, m := range r.Middlewares {
		inChain[m.ID] = true
		ref.Path = append(ref.Path, PathStep{ID: m.ID, Name: m.Name, Role: "middleware", File: m.File, Line: m.Line, Conf: m.Confidence, Check: s.chainCheck(r, m.Name)})
	}
	for x := v; x.next != nil; x = x.next {
		id := x.next.id
		if inChain[id] && x == v {
			continue // already listed with the chain
		}
		n := s.nodes[id]
		ps := PathStep{ID: id, Role: "function", Conf: x.edge.Confidence}
		if n != nil {
			ps.Name, ps.File, ps.Line = n.Name, n.File, n.Line
		}
		if x == v {
			ps.Role = "handler"
			if r.Handler != nil && r.Handler.ID != id {
				ps.Role = "middleware"
			}
			ps.Check = s.chainCheck(r, nameOf(r, id))
		}
		if x.next.next == nil {
			ps.Role = "sink"
		}
		ref.Path = append(ref.Path, ps)
	}
	return ref
}

func nameOf(r *model.Route, id string) string {
	if r.Handler != nil && r.Handler.ID == id {
		return r.Handler.Name
	}
	for _, m := range r.Middlewares {
		if m.ID == id {
			return m.Name
		}
	}
	return ""
}

func authText(r *model.Route) string {
	if r.Auth != nil && r.Auth.Recognized {
		t := "recognised"
		if r.Auth.Scheme != "" {
			t += " (" + r.Auth.Scheme + ")"
		}
		if r.Auth.By != "" {
			t += " by " + r.Auth.By
		}
		return t
	}
	return "none identified"
}

// chainCheck names the security check a chain element performs, from the
// route's contract: reading a credential with a 401 answer (authentication)
// or answering 403 (authorization).
func (s *scanner) chainCheck(r *model.Route, name string) string {
	var checks []string
	if r.Auth != nil && r.Auth.Recognized && r.Auth.By == name {
		checks = append(checks, "authentication")
	}
	ai := -1
	if r.Auth != nil && r.Auth.Recognized {
		ai = chainIndex(r, r.Auth.By)
	}
	for _, x := range r.Responses {
		if x.Status == 403 && x.From == name {
			if i := chainIndex(r, name); ai >= 0 && i > ai {
				checks = append(checks, "authorization (answers 403)")
			} else {
				checks = append(checks, "request filter (answers 403)")
			}
			break
		}
	}
	return strings.Join(checks, ", ")
}
