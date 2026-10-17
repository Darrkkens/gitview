package dataflow

import (
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"

	"github.com/athos/gitview/internal/evidence"
	"github.com/athos/gitview/internal/flow"
	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/ssa"
)

type Options struct {
	Direction Direction
	MaxDepth  int // function boundaries crossed from the selection (default 3)
	MaxNodes  int // default 300
}

func (o Options) withDefaults() Options {
	if o.Direction == "" {
		o.Direction = Both
	}
	if o.MaxDepth <= 0 {
		o.MaxDepth = 3
	}
	if o.MaxNodes <= 0 {
		o.MaxNodes = 300
	}
	return o
}

// Query selects what to trace inside a function.
type Query struct {
	Variable string // local variable or parameter name
	Line     int    // restrict the variable to its references on this line
	Return   int    // trace the k-th result instead (k >= 0); -1 when unused
	File     string // with Line and Col: the expression at file:line:col
	Col      int
}

// VarInfo describes a selectable variable of a function.
type VarInfo struct {
	Name string `json:"name"`
	Kind string `json:"kind"` // param | local | result
	Type string `json:"type"`
	Line int    `json:"line"` // declaration line
	Refs int    `json:"refs"` // references in SSA debug information
}

// AmbiguousError lists variables sharing a name (shadowing).
type AmbiguousError struct {
	Name       string
	Candidates []VarInfo
}

func (e *AmbiguousError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d variables are named %q; use --line to choose one:", len(e.Candidates), e.Name)
	for _, c := range e.Candidates {
		fmt.Fprintf(&b, "\n  %-8s %s %s declared at line %d", c.Kind, c.Name, c.Type, c.Line)
	}
	return b.String()
}

// Variables lists the parameters, named results and local variables of fn
// that SSA debug information can attribute values to.
func Variables(p *flow.Program, fn *ssa.Function) []VarInfo {
	fn = flow.Origin(fn)
	objs := variableObjects(fn)
	var out []VarInfo
	for _, vo := range objs {
		out = append(out, vo.info(p))
	}
	return out
}

type varObj struct {
	obj  *types.Var
	kind string
	refs []*ssa.DebugRef
	prm  *ssa.Parameter
}

func (v varObj) info(p *flow.Program) VarInfo {
	_, line, _ := p.Pos(v.obj.Pos())
	return VarInfo{Name: v.obj.Name(), Kind: v.kind, Type: types.TypeString(v.obj.Type(), qualifier), Line: line, Refs: len(v.refs)}
}

func qualifier(p *types.Package) string { return p.Name() }

func variableObjects(fn *ssa.Function) []*varObj {
	byObj := map[*types.Var]*varObj{}
	var order []*varObj
	add := func(obj *types.Var, kind string) *varObj {
		if vo, ok := byObj[obj]; ok {
			return vo
		}
		vo := &varObj{obj: obj, kind: kind}
		byObj[obj] = vo
		order = append(order, vo)
		return vo
	}
	for _, prm := range fn.Params {
		if obj, ok := prm.Object().(*types.Var); ok && obj.Name() != "_" && obj.Name() != "" {
			add(obj, "param").prm = prm
		}
	}
	results := fn.Signature.Results()
	for i := 0; i < results.Len(); i++ {
		if r := results.At(i); r.Name() != "" && r.Name() != "_" {
			add(r, "result")
		}
	}
	for _, b := range fn.Blocks {
		for _, ins := range b.Instrs {
			dr, ok := ins.(*ssa.DebugRef)
			if !ok {
				continue
			}
			obj, ok := dr.Object().(*types.Var)
			if !ok || obj.Name() == "_" || obj.IsField() || obj.Pkg() != nil && obj.Parent() == obj.Pkg().Scope() {
				continue // fields and package-level variables are not locals
			}
			vo := add(obj, "local")
			vo.refs = append(vo.refs, dr)
		}
	}
	sort.SliceStable(order, func(i, j int) bool { return order[i].obj.Pos() < order[j].obj.Pos() })
	return order
}

// Trace runs a data flow query on fn.
func Trace(p *flow.Program, fn *ssa.Function, q Query, opt Options) (*Result, error) {
	fn = flow.Origin(fn)
	if q.File != "" && q.Line > 0 {
		// The expression belongs to the innermost function at the position,
		// which must be fn or a closure inside it.
		inner := p.FuncAt(q.File, q.Line)
		within := false
		for f := inner; f != nil; f = f.Parent() {
			within = within || flow.Origin(f) == fn
		}
		if !within {
			return nil, fmt.Errorf("%s:%d is not inside %s", q.File, q.Line, flow.ShortName(fn))
		}
		fn = flow.Origin(inner)
	}
	t := newTracer(p, fn, opt)
	var roots []item
	switch {
	case q.File != "" && q.Line > 0:
		v, isAddr, sel, err := t.exprValue(q)
		if err != nil {
			return nil, err
		}
		t.res.Selection = sel
		roots = []item{{v: v, mem: isAddr}}
	case q.Return >= 0 && q.Variable == "":
		n := fn.Signature.Results().Len()
		if q.Return >= n {
			return nil, fmt.Errorf("%s has %d result(s); there is no result #%d", flow.ShortName(fn), n, q.Return)
		}
		t.res.Selection = Selection{Kind: "return", Name: fmt.Sprintf("result #%d", q.Return), Type: types.TypeString(fn.Signature.Results().At(q.Return).Type(), qualifier)}
		for _, b := range fn.Blocks {
			for _, ins := range b.Instrs {
				if ret, ok := ins.(*ssa.Return); ok && q.Return < len(ret.Results) {
					roots = append(roots, item{v: ret.Results[q.Return], retIdx: q.Return, ret: ret})
				}
			}
		}
		if len(roots) == 0 {
			return nil, fmt.Errorf("%s never returns normally", flow.ShortName(fn))
		}
	case q.Variable != "":
		var err error
		roots, err = t.variableRoots(q)
		if err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("nothing selected: give a variable, a result or an expression")
	}
	t.run(roots)
	return t.res, nil
}

// TraceValue traces a specific SSA value of fn (used by the SQL Explorer
// to follow the parameters of a query).
func TraceValue(p *flow.Program, fn *ssa.Function, v ssa.Value, sel Selection, opt Options) *Result {
	t := newTracer(p, flow.Origin(fn), opt)
	t.res.Selection = sel
	t.run([]item{{v: v}})
	return t.res
}

// frame is one entry of the call-string context: the call site through
// which the trace entered the current function (nil call: unknown, e.g. a
// closure reached through a captured variable).
type frame struct {
	call ssa.CallInstruction
	up   *frame
}

type item struct {
	v      ssa.Value
	mem    bool   // v is an address: trace the contents
	ctx    *frame // call sites that led into the current function
	level  int
	cross  int
	node   int
	conf   string
	hops   int
	retIdx int         // for return roots
	ret    *ssa.Return // root taken from a return statement
	store  *ssa.Store  // for memory items reached by a write: that write
}

type nodeKey struct {
	v     ssa.Value
	mem   bool
	instr ssa.Instruction
	kind  string
	extra string
}

// top returns the call site that entered the current function, if known.
func (it item) top() ssa.CallInstruction {
	if it.ctx == nil {
		return nil
	}
	return it.ctx.call
}

type visitKey struct {
	v     ssa.Value
	mem   bool
	ctx   *frame
	dir   Direction
	store *ssa.Store
}

type tracer struct {
	p        *flow.Program
	fn       *ssa.Function
	opt      Options
	res      *Result
	nodes    map[nodeKey]*Node
	visited  map[visitKey]bool
	edgeSeen map[string]bool
	funcs    map[*ssa.Function]*funcInfo
	full     bool
	dir      Direction
}

type funcInfo struct {
	file  *ast.File
	lhs   map[*ast.Ident]bool
	reach map[*ssa.BasicBlock]map[*ssa.BasicBlock]bool
}

func newTracer(p *flow.Program, fn *ssa.Function, opt Options) *tracer {
	opt = opt.withDefaults()
	t := &tracer{p: p, fn: fn, opt: opt, nodes: map[nodeKey]*Node{}, visited: map[visitKey]bool{},
		edgeSeen: map[string]bool{}, funcs: map[*ssa.Function]*funcInfo{}}
	t.res = &Result{Symbol: p.Ref(fn), Direction: opt.Direction, MaxDepth: opt.MaxDepth}
	t.res.Evidence = append(t.res.Evidence, evidence.Evidence{Kind: evidence.Static, Basis: evidence.BasisStatic,
		SymbolID: t.res.Symbol.ID, File: t.res.Symbol.File, Line: t.res.Symbol.Line, Source: "golang.org/x/tools/go/ssa",
		Details: "def-use chains of the SSA form with debug references to source variables"})
	return t
}

func (t *tracer) info(fn *ssa.Function) *funcInfo {
	if fi, ok := t.funcs[fn]; ok {
		return fi
	}
	fi := &funcInfo{lhs: map[*ast.Ident]bool{}}
	t.funcs[fn] = fi
	fi.file = t.p.FileOf(fn, fn.Pos())
	if syn := fn.Syntax(); syn != nil {
		ast.Inspect(syn, func(n ast.Node) bool {
			switch s := n.(type) {
			case *ast.AssignStmt:
				for _, l := range s.Lhs {
					if id, ok := l.(*ast.Ident); ok {
						fi.lhs[id] = true
					}
				}
			case *ast.ValueSpec:
				for _, id := range s.Names {
					fi.lhs[id] = true
				}
			case *ast.RangeStmt:
				for _, e := range []ast.Expr{s.Key, s.Value} {
					if id, ok := e.(*ast.Ident); ok {
						fi.lhs[id] = true
					}
				}
			}
			return true
		})
	}
	return fi
}

// --- selection -------------------------------------------------------------------

func (t *tracer) variableRoots(q Query) ([]item, error) {
	var matches []*varObj
	for _, vo := range variableObjects(t.fn) {
		if vo.obj.Name() == q.Variable {
			matches = append(matches, vo)
		}
	}
	if len(matches) == 0 {
		var names []string
		for _, v := range Variables(t.p, t.fn) {
			names = append(names, v.Name)
		}
		return nil, fmt.Errorf("no variable %q in %s (variables: %s)", q.Variable, flow.ShortName(t.fn), strings.Join(names, ", "))
	}
	if q.Line > 0 && len(matches) > 1 {
		// A declaration on the line wins over a mere reference.
		var declared, referenced []*varObj
		for _, vo := range matches {
			if vo.info(t.p).Line == q.Line {
				declared = append(declared, vo)
				continue
			}
			for _, dr := range vo.refs {
				if _, l, _ := t.p.Pos(dr.Expr.Pos()); l == q.Line {
					referenced = append(referenced, vo)
					break
				}
			}
		}
		matches = declared
		if len(matches) == 0 {
			matches = referenced
		}
	}
	if len(matches) > 1 {
		amb := &AmbiguousError{Name: q.Variable}
		for _, vo := range matches {
			amb.Candidates = append(amb.Candidates, vo.info(t.p))
		}
		return nil, amb
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("variable %q is not declared or referenced at line %d", q.Variable, q.Line)
	}
	vo := matches[0]
	vi := vo.info(t.p)
	t.res.Selection = Selection{Kind: "variable", Name: vi.Name, Type: vi.Type, Line: vi.Line}
	if vo.kind == "param" {
		t.res.Selection.Kind = "param"
	}
	seen := map[ssa.Value]bool{}
	var roots []item
	if vo.prm != nil {
		seen[vo.prm] = true
		roots = append(roots, item{v: vo.prm})
	}
	for _, dr := range vo.refs {
		if q.Line > 0 {
			if _, l, _ := t.p.Pos(dr.Expr.Pos()); l != q.Line && vi.Line != q.Line {
				continue
			}
		}
		x := dr.X
		if seen[x] {
			continue
		}
		seen[x] = true
		if dr.IsAddr {
			roots = append(roots, item{v: x, mem: true})
		} else {
			roots = append(roots, item{v: x})
		}
	}
	if len(roots) == 0 {
		return nil, fmt.Errorf("variable %q has no values recorded by SSA (it may be unused)", q.Variable)
	}
	return roots, nil
}

func (t *tracer) exprValue(q Query) (ssa.Value, bool, Selection, error) {
	fi := t.info(t.fn)
	if fi.file == nil {
		return nil, false, Selection{}, errors.New("source file not available")
	}
	tf := t.p.Fset().File(fi.file.Pos())
	if q.Line < 1 || q.Line > tf.LineCount() {
		return nil, false, Selection{}, fmt.Errorf("line %d is outside %s", q.Line, tf.Name())
	}
	pos := tf.LineStart(q.Line) + token.Pos(max(q.Col-1, 0))
	path, _ := astutil.PathEnclosingInterval(fi.file, pos, pos)
	for k, n := range path {
		e, ok := n.(ast.Expr)
		if !ok {
			continue
		}
		// The field name of x.f stands for the whole selector.
		if id, isIdent := e.(*ast.Ident); isIdent && k+1 < len(path) {
			if sel, ok := path[k+1].(*ast.SelectorExpr); ok && sel.Sel == id {
				continue
			}
		}
		// For addressable expressions SSA may give the address: the
		// contents are then traced through memory.
		if v, isAddr := t.fn.ValueForExpr(e); v != nil {
			typ := v.Type()
			if pt, ok := typ.Underlying().(*types.Pointer); ok && isAddr {
				typ = pt.Elem()
			}
			return v, isAddr, Selection{Kind: "expr", Name: oneLine(t.p.NodeText(e), 80), Type: types.TypeString(typ, qualifier), Line: q.Line}, nil
		}
	}
	return nil, false, Selection{}, fmt.Errorf("no traceable expression of %s at %s:%d:%d", flow.ShortName(t.fn), q.File, q.Line, q.Col)
}

// --- driver ------------------------------------------------------------------------

func (t *tracer) run(roots []item) {
	for i := range roots {
		r := &roots[i]
		n := t.valueNode(r.v, r.mem, "selected", 0)
		if n == nil {
			continue
		}
		n.Conf = evidence.Exact
		if sel := t.res.Selection; (sel.Kind == "variable" || sel.Kind == "param") && !hasName(n.Var, sel.Name) {
			n.Var = joinLabels(sel.Name, n.Var)
			n.Label = sel.Name + " = " + n.Label
		}
		r.node, r.conf = n.ID, evidence.Exact
		if !contains(t.res.Roots, n.ID) {
			t.res.Roots = append(t.res.Roots, n.ID)
		}
	}
	if t.opt.Direction != Destination {
		t.dir = Origin
		t.bfs(roots, t.preds)
	}
	if t.opt.Direction != Origin {
		t.dir = Destination
		t.bfs(roots, t.succs)
	}
	if t.full {
		t.res.Status.Limit(fmt.Sprintf("node limit (%d) reached", t.opt.MaxNodes))
	}
	t.res.Status.Done()
	t.res.Notes = append(t.res.Notes,
		"Static analysis: edges are possible flows on some execution path, not observed values.",
		"Values passed through memory shared with other goroutines, reflection or code outside the module are only followed where the analysis can show the relation.")
}

func hasName(list, name string) bool {
	for _, n := range strings.Split(list, ", ") {
		if n == name {
			return true
		}
	}
	return false
}

func contains(xs []int, x int) bool {
	for _, y := range xs {
		if x == y {
			return true
		}
	}
	return false
}

// step is one relation found from the current item.
type step struct {
	v       ssa.Value // next value to trace (nil for terminal event nodes)
	mem     bool
	node    *Node // explicit node for events (sinks, sources, checks)
	kind    string
	conf    string
	label   string
	pos     token.Pos
	ctx     ssa.CallInstruction // with enter: the call being entered
	enter   bool                // crosses into a callee (level+1, pushes ctx)
	leave   bool                // returns to the calling function (level-1, pops)
	cross   bool                // crosses into another function without call context (closure capture)
	stop    bool                // do not continue from the next node
	fromCtx bool                // keep the current context
	store   *ssa.Store
}

func (t *tracer) bfs(start []item, next func(item) []step) {
	queue := append([]item(nil), start...)
	for len(queue) > 0 {
		it := queue[0]
		queue = queue[1:]
		vk := visitKey{it.v, it.mem, it.ctx, t.dir, it.store}
		if it.v != nil {
			if t.visited[vk] {
				continue
			}
			t.visited[vk] = true
		}
		for _, s := range next(it) {
			if t.full {
				return
			}
			level, cross := it.level, it.cross
			var ctx *frame
			switch {
			case s.enter:
				ctx = &frame{call: s.ctx, up: it.ctx}
				level++
				cross++
			case s.leave:
				if it.ctx != nil {
					ctx = it.ctx.up
				}
				level--
				cross++
			case s.cross:
				cross++
			case s.fromCtx:
				ctx = it.ctx
			}
			n := s.node
			if n == nil {
				if s.v == nil {
					continue
				}
				n = t.valueNode(s.v, s.mem, string(t.dir), level)
			} else {
				n = t.eventNode(n, string(t.dir), level)
			}
			if n == nil {
				t.full = true
				return
			}
			if n.File == "" && s.pos.IsValid() {
				n.File, n.Line, _ = t.p.Pos(s.pos)
				n.Code = strings.TrimSpace(t.p.Source(n.File, n.Line, n.Line))
			}
			conf := weaker(it.conf, s.conf)
			if n.Conf == "" || confRank[conf] > confRank[n.Conf] {
				n.Conf = conf
			}
			if n.Hops == 0 && n.Dir != "selected" {
				n.Hops = it.hops + 1
			}
			from, to := it.node, n.ID
			if t.dir == Origin {
				from, to = n.ID, it.node
			}
			t.addEdge(&Edge{From: from, To: to, Kind: s.kind, Conf: s.conf, Label: s.label, Dir: string(t.dir)}, s.pos)
			if s.stop || s.v == nil {
				continue
			}
			if cross > t.opt.MaxDepth {
				n.Detail = joinLabels(n.Detail, "not followed further: depth limit")
				t.res.Status.Limit(fmt.Sprintf("call depth limit (%d) reached", t.opt.MaxDepth))
				continue
			}
			queue = append(queue, item{v: s.v, mem: s.mem, store: s.store, ctx: ctx, level: level, cross: cross, node: n.ID, conf: conf, hops: it.hops + 1})
		}
	}
}

func (t *tracer) addEdge(e *Edge, pos token.Pos) {
	if pos.IsValid() {
		e.File, e.Line, _ = t.p.Pos(pos)
	}
	key := fmt.Sprintf("%d>%d>%s>%s>%s", e.From, e.To, e.Kind, e.Dir, e.Label)
	if t.edgeSeen[key] || e.From == e.To {
		return
	}
	t.edgeSeen[key] = true
	t.res.Edges = append(t.res.Edges, e)
}

func (t *tracer) newNode(key nodeKey, n *Node) *Node {
	if old, ok := t.nodes[key]; ok {
		return old
	}
	if len(t.res.Nodes) >= t.opt.MaxNodes {
		return nil
	}
	n.ID = len(t.res.Nodes)
	t.nodes[key] = n
	t.res.Nodes = append(t.res.Nodes, n)
	switch n.Kind {
	case KindSource:
		t.res.Sources = append(t.res.Sources, n.ID)
	case KindSQL, KindExternal, KindReturn:
		t.res.Sinks = append(t.res.Sinks, n.ID)
	}
	return n
}

func (t *tracer) eventNode(n *Node, dir string, level int) *Node {
	key := nodeKey{kind: n.Kind, extra: fmt.Sprintf("%s|%s:%d|%s|%s", n.Label, n.File, n.Line, n.Detail, n.Func)}
	if old, ok := t.nodes[key]; ok {
		return old
	}
	n.Dir, n.Level = dir, level
	if n.Code == "" && n.File != "" {
		n.Code = strings.TrimSpace(t.p.Source(n.File, n.Line, n.Line))
	}
	if n.Kind == KindCheck {
		defer func() {
			if n.ID >= 0 {
				t.res.Validations = append(t.res.Validations, Validation{Node: n.ID, Text: n.Label, Kind: n.Detail, Func: n.FuncName, File: n.File, Line: n.Line})
			}
		}()
	}
	n.ID = -1
	out := t.newNode(key, n)
	return out
}

// --- node descriptions ------------------------------------------------------------

func (t *tracer) valueNode(v ssa.Value, mem bool, dir string, level int) *Node {
	key := nodeKey{v: v, mem: mem}
	if old, ok := t.nodes[key]; ok {
		return old
	}
	n := t.describe(v, mem)
	n.Dir, n.Level = dir, level
	return t.newNode(key, n)
}

func (t *tracer) describe(v ssa.Value, mem bool) *Node {
	n := &Node{Kind: KindValue, Type: types.TypeString(v.Type(), qualifier)}
	fn := v.Parent()
	if fn != nil {
		fn = flow.Origin(fn)
		n.Func, n.FuncName = fn.String(), flow.ShortName(fn)
	}
	defs, uses := t.varNames(v, mem)
	pos := token.NoPos
	if len(defs) > 0 {
		n.Var = strings.Join(defs, ", ")
	} else if len(uses) > 0 {
		n.Var = strings.Join(uses, ", ")
	}
	expr := t.exprText(v)
	switch x := v.(type) {
	case *ssa.Parameter:
		n.Kind = KindParam
		n.Label = "param " + x.Name()
		if n.Var != "" && !hasName(n.Var, x.Name()) {
			n.Label = n.Var + " = param " + x.Name()
		}
		pos = x.Pos()
	case *ssa.FreeVar:
		n.Kind = KindParam
		n.Label = "captured " + x.Name()
		pos = x.Pos()
	case *ssa.Const:
		n.Kind = KindConst
		n.Label = constText(x)
	case *ssa.Global:
		n.Kind = KindGlobal
		n.Label = "var " + x.Name()
		pos = x.Pos()
		n.Func, n.FuncName = "", x.Pkg.Pkg.Name()
	case *ssa.Call:
		n.Kind = KindCall
	case *ssa.Phi:
		n.Kind = KindMerge
	case *ssa.Alloc:
		if n.Var == "" && x.Comment != "" && x.Comment != "varargs" && x.Comment != "complit" {
			n.Var = x.Comment
			if x.Heap {
				n.Detail = "variable moved to the heap (captured or address taken)"
			}
		}
	case *ssa.FieldAddr:
		if mem {
			n.Kind, n.Label = KindField, t.addrText(x)
		}
	case *ssa.IndexAddr:
		if mem {
			n.Kind = KindField
		}
	}
	if mem {
		n.Type = strings.TrimPrefix(n.Type, "*")
	}
	if n.Label == "" {
		switch {
		case n.Var != "" && expr != "" && !strings.Contains(expr, n.Var) && n.Kind != KindMerge:
			n.Label = n.Var + " = " + oneLine(expr, 60)
		case n.Var != "":
			n.Label = n.Var
		case expr != "":
			n.Label = oneLine(expr, 70)
		default:
			n.Label = ssaDescription(v)
		}
	}
	if !pos.IsValid() {
		pos = t.valuePos(v, mem)
	}
	if pos.IsValid() {
		n.File, n.Line, _ = t.p.Pos(pos)
		n.Code = strings.TrimSpace(t.p.Source(n.File, n.Line, n.Line))
	}
	switch v.(type) {
	case *ssa.Call, *ssa.BinOp, *ssa.Parameter, *ssa.FreeVar, *ssa.Const, *ssa.Global, *ssa.Phi:
	default:
		n.Intermediate = len(defs) == 0 && len(uses) == 0 && expr == "" || isConversion(v) && len(defs) == 0
	}
	if n.Kind == KindMerge {
		n.Detail = "one of several values, depending on the path taken"
	}
	return n
}

func isConversion(v ssa.Value) bool {
	switch v.(type) {
	case *ssa.MakeInterface, *ssa.ChangeType, *ssa.ChangeInterface, *ssa.Extract:
		return true
	}
	return false
}

func ssaDescription(v ssa.Value) string {
	switch x := v.(type) {
	case *ssa.Extract:
		return fmt.Sprintf("result #%d", x.Index)
	case *ssa.MakeInterface:
		return "as " + types.TypeString(x.Type(), qualifier)
	case *ssa.Phi:
		return "merge"
	}
	s := v.String()
	if i := strings.IndexByte(s, '#'); i > 0 {
		s = s[:i]
	}
	return oneLine(s, 60)
}

func constText(k *ssa.Const) string {
	if k.Value == nil {
		if k.IsNil() {
			return "nil"
		}
		return "zero " + types.TypeString(k.Type(), qualifier)
	}
	if k.Value.Kind() == constant.String {
		return strconv.Quote(oneLine(constant.StringVal(k.Value), 60))
	}
	return k.Value.ExactString()
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// varNames returns the source variables bound to v: those assigned at this
// point (defs) and those merely referring to it (uses).
func (t *tracer) varNames(v ssa.Value, mem bool) (defs, uses []string) {
	refs := v.Referrers()
	if refs == nil {
		return nil, nil
	}
	fi := t.info(v.Parent())
	seenD, seenU := map[string]bool{}, map[string]bool{}
	for _, r := range *refs {
		dr, ok := r.(*ssa.DebugRef)
		if !ok || dr.X != v || dr.IsAddr != mem {
			continue
		}
		obj, ok := dr.Object().(*types.Var)
		if !ok || obj.IsField() {
			continue
		}
		id, _ := dr.Expr.(*ast.Ident)
		if id != nil && fi.lhs[id] {
			if !seenD[obj.Name()] {
				seenD[obj.Name()] = true
				defs = append(defs, obj.Name())
			}
		} else if !seenU[obj.Name()] {
			seenU[obj.Name()] = true
			uses = append(uses, obj.Name())
		}
	}
	if prm, ok := v.(*ssa.Parameter); ok && len(defs) == 0 {
		defs = []string{prm.Name()}
	}
	return defs, uses
}

// exprText returns the source expression that computes v.
func (t *tracer) exprText(v ssa.Value) string {
	if refs := v.Referrers(); refs != nil {
		for _, r := range *refs {
			if dr, ok := r.(*ssa.DebugRef); ok && dr.X == v && !dr.IsAddr {
				if _, isIdent := dr.Expr.(*ast.Ident); !isIdent {
					return t.p.NodeText(dr.Expr)
				}
			}
		}
	}
	if e := t.exprAt(v.Parent(), v.Pos()); e != nil {
		return t.p.NodeText(e)
	}
	return ""
}

// exprAt finds the expression whose distinguishing token is at pos (the
// position SSA records: call parenthesis, operator, selector...).
func (t *tracer) exprAt(fn *ssa.Function, pos token.Pos) ast.Expr {
	if fn == nil || !pos.IsValid() {
		return nil
	}
	fi := t.info(fn)
	if fi.file == nil {
		return nil
	}
	path, _ := astutil.PathEnclosingInterval(fi.file, pos, pos)
	for _, n := range path {
		e, ok := n.(ast.Expr)
		if !ok {
			continue
		}
		switch x := e.(type) {
		case *ast.CallExpr:
			if x.Lparen == pos {
				return x
			}
		case *ast.BinaryExpr:
			if x.OpPos == pos {
				return x
			}
		case *ast.UnaryExpr:
			if x.OpPos == pos {
				return x
			}
		case *ast.StarExpr:
			if x.Star == pos {
				return x
			}
		case *ast.SelectorExpr:
			if x.Sel.Pos() == pos {
				return x
			}
		case *ast.IndexExpr:
			if x.Lbrack == pos {
				return x
			}
		case *ast.SliceExpr:
			if x.Lbrack == pos {
				return x
			}
		case *ast.TypeAssertExpr:
			if x.Lparen == pos {
				return x
			}
		case *ast.CompositeLit:
			if x.Lbrace == pos {
				return x
			}
		case *ast.FuncLit:
			if x.Pos() == pos {
				return x
			}
		}
	}
	return nil
}

func (t *tracer) addrText(fa *ssa.FieldAddr) string {
	if e := t.exprAt(fa.Parent(), fa.Pos()); e != nil {
		return t.p.NodeText(e)
	}
	return "field " + fieldName(fa.X.Type(), fa.Field)
}

func fieldName(ptr types.Type, i int) string {
	if pt, ok := ptr.Underlying().(*types.Pointer); ok {
		ptr = pt.Elem()
	}
	if st, ok := ptr.Underlying().(*types.Struct); ok && i < st.NumFields() {
		return st.Field(i).Name()
	}
	return "#" + strconv.Itoa(i)
}

func (t *tracer) valuePos(v ssa.Value, mem bool) token.Pos {
	if refs := v.Referrers(); refs != nil {
		fi := t.info(v.Parent())
		var first token.Pos
		for _, r := range *refs {
			if dr, ok := r.(*ssa.DebugRef); ok && dr.X == v && dr.IsAddr == mem {
				if id, ok := dr.Expr.(*ast.Ident); ok && fi.lhs[id] {
					return id.Pos()
				}
				if !first.IsValid() || dr.Expr.Pos() < first {
					first = dr.Expr.Pos()
				}
			}
		}
		if p := v.Pos(); p.IsValid() {
			return p
		}
		if first.IsValid() {
			return first
		}
	}
	if p := v.Pos(); p.IsValid() {
		return p
	}
	if ins, ok := v.(ssa.Instruction); ok && ins.Block() != nil {
		instrs := ins.Block().Instrs
		for i := len(instrs) - 1; i >= 0; i-- {
			if instrs[i] == ins {
				for j := i; j >= 0; j-- {
					if p := instrs[j].Pos(); p.IsValid() {
						return p
					}
				}
				for j := i + 1; j < len(instrs); j++ {
					if p := instrs[j].Pos(); p.IsValid() {
						return p
					}
				}
			}
		}
	}
	if v.Parent() != nil {
		return v.Parent().Pos()
	}
	return token.NoPos
}

// event creates a terminal node (source, sink, check) located at pos.
func (t *tracer) event(kind, label, detail string, fn *ssa.Function, pos token.Pos, tags ...string) *Node {
	n := &Node{Kind: kind, Label: label, Detail: detail, Tags: tags}
	if fn != nil {
		fn = flow.Origin(fn)
		n.Func, n.FuncName = fn.String(), flow.ShortName(fn)
	}
	if pos.IsValid() {
		n.File, n.Line, _ = t.p.Pos(pos)
	} else if fn != nil {
		n.File, n.Line, _ = t.p.Pos(fn.Pos())
	}
	return n
}
