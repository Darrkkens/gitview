package audit

import (
	"go/ast"
	"go/types"
	"regexp"
	"sort"
	"strings"

	"github.com/athos/gitview/internal/evidence"
	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/graph/model"
	"github.com/athos/gitview/internal/inspect/dataflow"
	"github.com/athos/gitview/internal/inspect/sqlops"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
)

// reach is what a route's chain may reach in the Repository Graph.
type reach struct {
	prev map[string]*model.Edge // edge into each reached node (shortest path)
}

// reachFrom walks the call edges forward from a node.
func (s *scanner) reachFrom(id string, depth int) *reach {
	rc := &reach{prev: map[string]*model.Edge{}}
	seen := map[string]int{id: 0}
	queue := []string{id}
	for len(queue) > 0 {
		x := queue[0]
		queue = queue[1:]
		if seen[x] >= depth {
			continue
		}
		for _, e := range s.outE[x] {
			if _, ok := seen[e.Target]; ok {
				continue
			}
			seen[e.Target] = seen[x] + 1
			rc.prev[e.Target] = e
			queue = append(queue, e.Target)
		}
	}
	return rc
}

// pathTo builds the PathStep list from the route to a reached node.
func (s *scanner) pathTo(r *model.Route, rc *reach, target string) []PathStep {
	var ids []string
	var confs []string
	for x := target; x != r.ID; {
		e := rc.prev[x]
		if e == nil {
			break
		}
		ids = append([]string{x}, ids...)
		confs = append([]string{e.Confidence}, confs...)
		x = e.Source
	}
	steps := []PathStep{{ID: r.ID, Name: r.Key, Role: "route", File: r.File, Line: r.Line, Conf: ConfConfirmed}}
	inChain := map[string]bool{}
	for _, m := range r.Middlewares {
		inChain[m.ID] = true
		steps = append(steps, PathStep{ID: m.ID, Name: m.Name, Role: "middleware", File: m.File, Line: m.Line, Conf: m.Confidence, Check: s.chainCheck(r, m.Name)})
	}
	for k, id := range ids {
		if k == 0 && inChain[id] {
			continue
		}
		n := s.nodes[id]
		ps := PathStep{ID: id, Role: "function", Conf: confs[k]}
		if n != nil {
			ps.Name, ps.File, ps.Line = n.Name, n.File, n.Line
		}
		if k == 0 {
			ps.Role = "handler"
			ps.Check = s.chainCheck(r, nameOf(r, id))
		}
		if k == len(ids)-1 {
			ps.Role = "sink"
		}
		steps = append(steps, ps)
	}
	return steps
}

func (s *scanner) sortedRoutes() []*model.Route {
	var out []*model.Route
	if s.g.HTTP == nil {
		return nil
	}
	out = append(out, s.g.HTTP.Routes...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// chainIndex is the position of a chain element (the handler is last).
func chainIndex(r *model.Route, name string) int {
	for i, m := range r.Middlewares {
		if m.Name == name {
			return i
		}
	}
	if r.Handler != nil && r.Handler.Name == name {
		return len(r.Middlewares)
	}
	return -1
}

// authorizationCheck finds a chain element that answers 403 after the
// authentication: it decides what an identified caller may do. A 403
// before authentication (an origin filter, a rate limit) is not one.
func authorizationCheck(r *model.Route) (string, bool) {
	if r.Auth == nil || !r.Auth.Recognized {
		return "", false
	}
	ai := chainIndex(r, r.Auth.By)
	if ai < 0 {
		return "", false
	}
	for _, x := range r.Responses {
		if x.Status == 403 {
			if i := chainIndex(r, x.From); i > ai {
				return x.From, true
			}
		}
	}
	return "", false
}

// ---------- GO-AUTHZ-001 ----------

var (
	// owner or tenant columns: a condition on one of them, filled from
	// outside the request, ties the row to the caller.
	ownerColumn = regexp.MustCompile(`(?i)^(?:[a-z_]+\.)?(company|tenant|org|organization|organisation|account|owner|user|customer|workspace|team|client|created_by|owned_by)(_?id)?$`)
	whereCond   = regexp.MustCompile(`(?i)([a-z_][a-z0-9_]*(?:\.[a-z_][a-z0-9_]*)?)\s*(?:=|<>|!=|\bin\b|=\s*any)\s*\(?\s*(\$\d+|\?)`)
)

// conditions maps the placeholders of a SQL text to the columns they are
// compared with in its conditions.
func conditions(sql string) map[int]string {
	out := map[int]string{}
	q := 0
	idx := map[int]int{}
	for _, m := range regexp.MustCompile(`\$\d+|\?`).FindAllStringIndex(sql, -1) {
		if sql[m[0]] == '?' {
			q++
			idx[m[0]] = q
		}
	}
	for _, m := range whereCond.FindAllStringSubmatchIndex(sql, -1) {
		col := sql[m[2]:m[3]]
		ph := sql[m[4]:m[5]]
		n := 0
		if ph == "?" {
			n = idx[m[4]]
		} else {
			for _, ch := range ph[1:] {
				n = n*10 + int(ch-'0')
			}
		}
		if n > 0 {
			out[n] = col
		}
	}
	return out
}

func fromRequest(o []sqlops.ParamOrigin) (string, bool) {
	for _, x := range o {
		if x.Kind == dataflow.KindSource && strings.HasPrefix(x.Label, "HTTP") {
			return x.Label, true
		}
	}
	return "", false
}

func ruleResourceAuthorization(s *scanner, r *rule) {
	ops := map[string]*sqlops.Op{}
	opProg := map[string]*flow.Program{}
	for _, p := range s.in.Programs {
		for _, op := range sqlops.All(p) {
			ops[op.ID] = op
			opProg[op.ID] = p
		}
	}
	traced := map[string]bool{}
	schema := schemaOf(s.in.Root)
	type hit struct {
		op     *sqlops.Op
		routes []*model.Route
		reach  map[string]*reach
	}
	hits := map[string]*hit{}
	var order []string
	for _, rt := range s.sortedRoutes() {
		if rt.Auth == nil || !rt.Auth.Recognized {
			continue // without identity there is no ownership to check (GO-AUTHN-001 looks at these)
		}
		if _, ok := authorizationCheck(rt); ok {
			continue // an authorization check exists in the chain
		}
		if rt.Handler != nil && rt.Auth.By == rt.Handler.Name {
			continue // the handler authenticates the request itself (sign-in, signed webhook): no identity owns data before it
		}
		rc := s.reachFrom(rt.ID, 10)
		for id := range rc.prev {
			op := ops[id]
			if op == nil {
				continue
			}
			kind, _ := sqlops.Classify(op.SQL.Text)
			if op.SQL.Text == "" || (kind != sqlops.StmtSelect && kind != sqlops.StmtUpdate && kind != sqlops.StmtDelete) {
				continue
			}
			if !traced[id] {
				traced[id] = true
				op.TraceParams(opProg[id], dataflow.Options{MaxDepth: s.opt.MaxDepth})
			}
			conds := conditions(op.SQL.Text)
			fromReq, owner := false, false
			for _, prm := range op.Params {
				col, isCond := conds[prm.Index]
				if !isCond {
					continue
				}
				_, req := fromRequest(prm.Origins)
				if req {
					fromReq = true
				} else if ownerColumn.MatchString(col) && len(prm.Origins) > 0 {
					owner = true
				}
			}
			if !fromReq || owner {
				continue
			}
			// With the repository's schema, data in tables without an owner
			// or tenant column (reference data) is not owned by anyone.
			if known, owned := ownedTables(schema, op.SQL.Text); known && len(owned) == 0 {
				continue
			}
			h := hits[id]
			if h == nil {
				h = &hit{op: op, reach: map[string]*reach{}}
				hits[id] = h
				order = append(order, id)
			}
			h.routes = append(h.routes, rt)
			h.reach[rt.ID] = rc
		}
	}
	sort.Strings(order)
	for _, id := range order {
		h := hits[id]
		op, p := h.op, opProg[id]
		f := s.newFinding(r, p, op.Function(), op.File, op.Line, op.Call)
		f.Confidence, f.Verdict = ConfPossible, Review
		f.VerdictLabel = "Review Required — Authorization Not Established"
		conds := conditions(op.SQL.Text)
		var inputs []string
		for _, prm := range op.Params {
			col, isCond := conds[prm.Index]
			if !isCond {
				continue
			}
			if lbl, req := fromRequest(prm.Origins); req {
				inputs = append(inputs, col+" = "+lbl)
				for _, o := range prm.Origins {
					if o.Kind == dataflow.KindSource {
						f.ev(Evidence{Kind: EvSource, Text: "resource identifier: " + o.Label + " → $" + itoa(prm.Index) + " (" + col + ")", File: o.File, Line: o.Line, Code: code(p, o.File, o.Line), Confidence: o.Conf, Source: "dataflow"})
						f.Flow = append(f.Flow, FlowStep{Role: "source", Label: o.Label, Func: o.Func, File: o.File, Line: o.Line, Code: code(p, o.File, o.Line), Conf: o.Conf})
					}
				}
			}
		}
		f.Message = "The query uses " + strings.Join(inputs, ", ") + " and no condition on an owner or tenant column filled from the session was found; no element of the chain answers 403 after the authentication."
		f.Flow = append(f.Flow, FlowStep{Role: "sink", Label: op.Lib + " " + op.Method + ": " + oneLine(op.SQL.Text, 160), Func: op.Func.Name, File: op.File, Line: op.Line, Code: code(p, op.File, op.Line)})
		f.ev(Evidence{Kind: EvSink, Text: "data access: " + oneLine(op.SQL.Text, 200), File: op.File, Line: op.Line, Code: code(p, op.File, op.Line), Symbol: f.SymbolID, Confidence: evidence.Exact, Source: "sqlops"})
		f.ev(Evidence{Kind: EvAbsence, Text: "no condition on an owner or tenant column (company_id, tenant_id, user_id, owner_id…) with a value from outside the request", Basis: "heuristic", File: op.File, Line: op.Line, Source: "sqlops"})
		if _, owned := ownedTables(schema, op.SQL.Text); len(owned) > 0 {
			for _, o := range owned {
				f.ev(Evidence{Kind: EvConfig, Text: "table " + o.table + " has " + strings.Join(o.columns, ", ") + " (the query does not filter by it)", File: o.def.file, Line: o.def.line, Source: "sql schema"})
			}
			f.Confidence = ConfLikely
		} else {
			f.limit("no table declaration found in the repository's SQL files: whether the table is owned per user or tenant is not known")
		}
		for _, rt := range h.routes {
			f.ev(Evidence{Kind: EvCheck, Text: "authentication " + authText(rt) + " on " + rt.Key, File: rt.File, Line: rt.Line, Source: "httpapi"})
			f.Checks = append(f.Checks, Check{Kind: "authentication", Label: authText(rt), File: rt.File, Line: rt.Line, Basis: "static"})
			f.ev(Evidence{Kind: EvAbsence, Text: "no element of the chain of " + rt.Key + " answers 403 after the authentication", File: rt.File, Line: rt.Line, Source: "httpapi"})
			f.Routes = append(f.Routes, RouteRef{ID: rt.ID, Key: rt.Key, Auth: authText(rt), Path: s.pathTo(rt, h.reach[rt.ID], op.ID)})
		}
		f.limit("an ownership check outside the query (after loading the row, in a database policy, in a gateway) is not visible to this rule")
		s.add(f)
	}
}

type owned struct {
	table   string
	columns []string
	def     *tableDef
}

// ownedTables looks the main tables of a statement up in the schema:
// known is false when one of them is not declared in the repository.
func ownedTables(schema map[string]*tableDef, sql string) (known bool, out []owned) {
	st := sqlops.StructureOf(sql)
	known = len(st.Tables) > 0
	for _, t := range st.Tables {
		if t.Depth > 0 || t.Role == "join" {
			continue
		}
		def := schema[normTable(t.Name)]
		if def == nil {
			known = false
			continue
		}
		if cols := def.ownerColumns(); len(cols) > 0 {
			sort.Strings(cols)
			out = append(out, owned{normTable(t.Name), cols, def})
		}
	}
	return known, out
}

// ---------- GO-AUTHZ-002 ----------

var privilegedPath = regexp.MustCompile(`(?i)/(admin|administrator|internal|manage|management|backoffice|backstage|staff|superuser|sudo)(/|$)`)

func rulePrivilegedRoutes(s *scanner, r *rule) {
	for _, rt := range s.sortedRoutes() {
		if !privilegedPath.MatchString(rt.Path) {
			continue
		}
		if _, ok := authorizationCheck(rt); ok {
			continue
		}
		f := s.routeFinding(r, rt)
		f.Confidence, f.Verdict = ConfPossible, Review
		f.VerdictLabel = "Review Required — Authorization Not Identified"
		f.Message = rt.Key + " is under an administrative path and no element of its chain answers 403 after the authentication" +
			map[bool]string{true: "; authentication is " + authText(rt) + ".", false: "; no authentication was identified either."}[rt.Auth != nil && rt.Auth.Recognized]
		f.ev(Evidence{Kind: EvRegistration, Text: "registered as " + rt.Key, File: rt.File, Line: rt.Line, Source: "go/ssa"})
		f.ev(Evidence{Kind: EvAbsence, Text: "no element of the chain answers 403 after the authentication", Source: "httpapi"})
		f.ev(Evidence{Kind: EvAbsence, Text: "the path matches " + privilegedPath.String(), Basis: "heuristic", Source: "audit"})
		s.add(f)
	}
}

// routeFinding is a finding about a route as a whole.
func (s *scanner) routeFinding(r *rule, rt *model.Route) *Finding {
	var p *flow.Program
	var fn *ssa.Function
	if rt.Handler != nil && rt.Handler.ID != "" {
		p, fn = s.funcByNode(rt.Handler.ID)
	}
	f := s.newFinding(r, p, fn, rt.File, rt.Line, rt.Key)
	f.File, f.Line = rt.File, rt.Line
	path := []PathStep{{ID: rt.ID, Name: rt.Key, Role: "route", File: rt.File, Line: rt.Line, Conf: ConfConfirmed}}
	for _, m := range rt.Middlewares {
		path = append(path, PathStep{ID: m.ID, Name: m.Name, Role: "middleware", File: m.File, Line: m.Line, Conf: m.Confidence, Check: s.chainCheck(rt, m.Name)})
	}
	if rt.Handler != nil {
		path = append(path, PathStep{ID: rt.Handler.ID, Name: rt.Handler.Name, Role: "handler", File: rt.Handler.File, Line: rt.Handler.Line, Conf: rt.Handler.Confidence, Check: s.chainCheck(rt, rt.Handler.Name)})
	}
	f.Routes = []RouteRef{{ID: rt.ID, Key: rt.Key, Auth: authText(rt), Path: path}}
	return f
}

// funcByNode finds the SSA function of a graph function node.
func (s *scanner) funcByNode(id string) (*flow.Program, *ssa.Function) {
	flowID := strings.TrimPrefix(id, "func:")
	for _, p := range s.in.Programs {
		if fn := p.FuncByID(flowID); fn != nil {
			return p, fn
		}
	}
	return nil, nil
}

// ---------- AST helpers shared by the declaration rules ----------

// eachFile visits the module's parsed files (test files only when asked).
func (s *scanner) eachFile(visit func(p *flow.Program, pkg *packages.Package, file *ast.File, rel string)) {
	seen := map[string]bool{}
	for _, p := range s.in.Programs {
		for _, pkg := range p.TypedPackages() {
			for _, file := range pkg.Syntax {
				rel := p.RelFile(pkg.Fset.Position(file.Pos()).Filename)
				if seen[rel] || (!s.opt.IncludeTests && strings.HasSuffix(rel, "_test.go")) {
					continue
				}
				seen[rel] = true
				visit(p, pkg, file, rel)
			}
		}
	}
}

// typeName is the qualified name of an expression's type, pointers removed.
func typeName(info *types.Info, e ast.Expr) string {
	t := info.TypeOf(e)
	if t == nil {
		return ""
	}
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	if n, ok := t.(*types.Named); ok && n.Obj().Pkg() != nil {
		return n.Obj().Pkg().Path() + "." + n.Obj().Name()
	}
	return t.String()
}
