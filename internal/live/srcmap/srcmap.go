// Package srcmap is the Source Mapper of the Runtime Performance
// Inspector: it ties what was observed in a running process — stack
// frames, SQL call sites, request routes — to the Go source of the
// repository, using the same symbol identity as Code Flow and the Deep
// Inspector (flow symbol IDs).
package srcmap

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/inspect/perf"
	"github.com/athos/gitview/internal/inspect/sqlops"
	"github.com/athos/gitview/internal/live/httpcol"
	"golang.org/x/tools/go/ssa"
)

// Location is a module function.
type Location struct {
	Symbol string `json:"symbol"` // flow symbol ID
	Name   string `json:"name"`
	File   string `json:"file"` // repository-relative
	Line   int    `json:"line"`
	Conf   string `json:"conf"` // exact (position) | likely (runtime name)
}

// Mapper maps runtime observations to the programs of a repository.
type Mapper struct {
	progs []*flow.Program
	ops   []*sqlops.Op
	opsOK bool
	root  string
}

// New creates a mapper; root is the repository root (for file times).
func New(root string, progs []*flow.Program) *Mapper { return &Mapper{progs: progs, root: root} }

// Programs returns the programs the mapper uses.
func (m *Mapper) Programs() []*flow.Program { return m.progs }

func (m *Mapper) loc(p *flow.Program, fn *ssa.Function, conf string) Location {
	ref := p.Ref(fn)
	return Location{Symbol: ref.ID, Name: ref.Qualified, File: ref.File, Line: ref.Line, Conf: conf}
}

// Frame maps a runtime frame (function name, file, line).
func (m *Mapper) Frame(funcName, file string, line int) (Location, bool) {
	for _, p := range m.progs {
		if file != "" && line > 0 {
			if fn := p.FuncAt(file, line); fn != nil {
				return m.loc(p, fn, "exact"), true
			}
		}
	}
	for _, p := range m.progs {
		if fn := p.FuncForFrame(funcName, "", 0); fn != nil {
			return m.loc(p, fn, "likely"), true
		}
	}
	return Location{}, false
}

func (m *Mapper) allOps() []*sqlops.Op {
	if !m.opsOK {
		for _, p := range m.progs {
			m.ops = append(m.ops, sqlops.All(p)...)
		}
		m.opsOK = true
	}
	return m.ops
}

// SQLMatch relates an observed statement to a SQL Explorer operation.
type SQLMatch struct {
	OpID  string     `json:"opId"`
	Op    *sqlops.Op `json:"-"`
	Func  Location   `json:"func"`
	Conf  string     `json:"conf"`  // exact | likely | possible
	Basis string     `json:"basis"` // what the match rests on
}

// normSQL compares statements: literals removed (as the collector does),
// white space collapsed, case folded.
func normSQL(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(redact(s)), " "))
}

// SQL finds the operation that executed a statement: by its call site
// when the collector recorded one, else by its text.
func (m *Mapper) SQL(query, file string, line int, funcName string) (SQLMatch, bool) {
	ops := m.allOps()
	if line > 0 {
		var near *sqlops.Op
		for _, p := range m.progs {
			fn := p.FuncAt(file, line)
			if fn == nil && funcName != "" {
				fn = p.FuncForFrame(funcName, "", 0)
			}
			if fn == nil {
				continue
			}
			ref := p.Ref(fn)
			for _, op := range ops {
				if op.Func.ID != ref.ID {
					continue
				}
				if op.Line == line {
					return SQLMatch{OpID: op.ID, Op: op, Func: m.loc(p, fn, "exact"), Conf: "exact", Basis: "call site " + op.File + ":" + itoa(op.Line)}, true
				}
				// A call expression spread over several lines is reported
				// at one of them.
				if d := line - op.Line; d >= -3 && d <= 3 && near == nil {
					near = op
				}
			}
			if near != nil {
				return SQLMatch{OpID: near.ID, Op: near, Func: m.loc(p, fn, "exact"), Conf: "likely", Basis: "call site within 3 lines of " + near.File + ":" + itoa(near.Line)}, true
			}
		}
	}
	want := normSQL(query)
	if want == "" {
		return SQLMatch{}, false
	}
	var found []*sqlops.Op
	for _, op := range ops {
		texts := append([]string{op.SQL.Text}, op.SQL.Variants...)
		for _, t := range texts {
			if t != "" && normSQL(t) == want {
				found = append(found, op)
				break
			}
		}
	}
	if len(found) == 0 {
		return SQLMatch{}, false
	}
	op := found[0]
	conf, basis := "likely", "same statement text"
	if len(found) > 1 {
		conf, basis = "possible", "same statement text as "+itoa(len(found))+" operations"
	}
	match := SQLMatch{OpID: op.ID, Op: op, Conf: conf, Basis: basis}
	for _, p := range m.progs {
		if fn := p.FuncByID(op.Func.ID); fn != nil {
			match.Func = m.loc(p, fn, conf)
		}
	}
	return match, true
}

// RouteMatch relates a request to a route registration.
type RouteMatch struct {
	Method     string     `json:"method"`
	Path       string     `json:"path"`
	Router     string     `json:"router"`
	File       string     `json:"file"`
	Line       int        `json:"line"`
	Handler    *Location  `json:"handler,omitempty"`
	Middleware []Location `json:"middleware,omitempty"`
	Conf       string     `json:"conf"`
	Basis      string     `json:"basis"`
}

// Route finds the registration serving a request: by the route pattern the
// application reported, else by matching the path against the patterns.
func (m *Mapper) Route(method, pattern, path string) (RouteMatch, bool) {
	if i := strings.IndexByte(pattern, ' '); i >= 0 {
		pattern = pattern[i+1:]
	}
	type cand struct {
		p     *flow.Program
		r     flow.RouteInfo
		score int
		exact bool
	}
	var best []cand
	for _, p := range m.progs {
		for _, r := range p.Routes() {
			if r.Method != "" && r.Method != "?" && !strings.EqualFold(r.Method, method) {
				continue
			}
			c := cand{p: p, r: r}
			switch {
			case pattern != "" && samePattern(r.Path, pattern):
				c.exact, c.score = true, 1000
			case pattern == "" && path != "":
				s, ok := matchPath(r.Path, path)
				if !ok {
					continue
				}
				c.score = s
			default:
				continue
			}
			if r.Method != "" {
				c.score++
			}
			switch {
			case len(best) == 0 || c.score > best[0].score:
				best = []cand{c}
			case c.score == best[0].score:
				best = append(best, c)
			}
		}
	}
	if len(best) == 0 {
		return RouteMatch{}, false
	}
	c := best[0]
	rm := RouteMatch{Method: c.r.Method, Path: c.r.Path, Router: c.r.Router, File: c.r.File, Line: c.r.Line}
	switch {
	case c.exact:
		rm.Conf, rm.Basis = "exact", "route pattern reported by the application"
	default:
		rm.Conf, rm.Basis = "likely", "request path matches the registered pattern"
	}
	if len(best) > 1 {
		rm.Conf, rm.Basis = "possible", rm.Basis+" (one of "+itoa(len(best))+" registrations)"
	}
	if c.r.Handler != nil {
		l := m.loc(c.p, c.r.Handler, rm.Conf)
		rm.Handler = &l
	}
	for _, mw := range c.r.Middleware {
		if mw != nil {
			rm.Middleware = append(rm.Middleware, m.loc(c.p, mw, rm.Conf))
		}
	}
	return rm, true
}

func segments(p string) []string { return strings.Split(strings.Trim(p, "/"), "/") }

func isParam(s string) bool {
	return strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") || strings.HasPrefix(s, ":")
}

func isWildcard(s string) bool {
	return s == "*" || strings.HasPrefix(s, "*") || strings.HasSuffix(s, "...}")
}

// samePattern compares route patterns of different routers: parameter
// names and syntaxes ({id}, :id) do not matter.
func samePattern(a, b string) bool {
	sa, sb := segments(a), segments(b)
	if len(sa) != len(sb) {
		return false
	}
	for i := range sa {
		pa, pb := isParam(sa[i]) || isWildcard(sa[i]), isParam(sb[i]) || isWildcard(sb[i])
		if pa != pb || !pa && sa[i] != sb[i] {
			return false
		}
	}
	return true
}

// matchPath matches a request path against a pattern; the score prefers
// literal segments.
func matchPath(pattern, path string) (int, bool) {
	ps, xs := segments(pattern), segments(path)
	score := 0
	for i, s := range ps {
		if isWildcard(s) {
			return score, true
		}
		if i >= len(xs) {
			return 0, false
		}
		switch {
		case isParam(s):
			score += 1
		case s == xs[i]:
			score += 10
		default:
			return 0, false
		}
	}
	if len(xs) != len(ps) {
		// ServeMux patterns ending in "/" match every path below them.
		if strings.HasSuffix(pattern, "/") && len(ps) <= len(xs) {
			return score, true
		}
		return 0, false
	}
	return score + 5, true
}

// PathStep is one function on the heaviest path of a flame graph.
type PathStep struct {
	Name   string  `json:"name"`
	Symbol string  `json:"symbol,omitempty"` // set for module functions
	Value  int64   `json:"value"`
	Self   int64   `json:"self"`
	Pct    float64 `json:"pct"`
}

// HotPath follows the heaviest child from the root of a flame graph: the
// critical execution path of the sampled work.
func HotPath(root *perf.FlameNode) []PathStep {
	if root == nil || root.Value <= 0 {
		return nil
	}
	var out []PathStep
	n := root
	for len(n.Children) > 0 {
		next := n.Children[0]
		for _, c := range n.Children[1:] {
			if c.Value > next.Value {
				next = c
			}
		}
		if next.Name == "(other)" {
			break
		}
		out = append(out, PathStep{Name: next.Name, Symbol: next.Symbol, Value: next.Value, Self: next.Self, Pct: 100 * float64(next.Value) / float64(root.Value)})
		n = next
	}
	return out
}

// StaleFiles returns the files (repository-relative) modified after the
// process started: line numbers observed in the process may not match
// them.
func (m *Mapper) StaleFiles(started time.Time, files []string) []string {
	if started.IsZero() || m.root == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, f := range files {
		if f == "" || seen[f] || filepath.IsAbs(f) {
			continue
		}
		seen[f] = true
		if st, err := os.Stat(filepath.Join(m.root, f)); err == nil && st.ModTime().After(started) {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

func itoa(n int) string { return strconv.Itoa(n) }

func redact(s string) string { return httpcol.RedactSQL(s) }
