// Package sqlops is the static part of the SQL Explorer: it finds the
// database operations (pgx, pgxpool, database/sql, sqlx) a Go function
// performs, the SQL they send, their parameters, where rows are scanned to
// and which transaction they run in.
//
// SQL built at runtime is never completed by guesswork: literal parts are
// shown and the computed parts are marked as such.
package sqlops

import (
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
	"github.com/athos/gitview/internal/inspect/dataflow"
	"golang.org/x/tools/go/ssa"
)

// SQL text kinds.
const (
	TextLiteral      = "literal"
	TextConstant     = "constant"      // named constant
	TextConcat       = "concatenation" // literal parts joined with runtime values
	TextSprintf      = "fmt.Sprintf"
	TextAlternatives = "alternatives" // one of several literals depending on the path
	TextParameter    = "parameter"    // received from the caller
	TextDynamic      = "dynamic"      // computed in a way the analysis does not resolve
	TextNone         = "none"         // the operation has no SQL (Begin, Commit...)
)

type SQLText struct {
	Kind      string   `json:"kind"`
	Text      string   `json:"text,omitempty"` // complete SQL, when known statically
	Formatted string   `json:"formatted,omitempty"`
	Template  string   `json:"template,omitempty"` // partially known: runtime parts shown as ⟨expr⟩
	Variants  []string `json:"variants,omitempty"` // complete alternatives
	Constant  string   `json:"constant,omitempty"` // pkg.Name of the constant holding the SQL
	ConstFile string   `json:"constFile,omitempty"`
	ConstLine int      `json:"constLine,omitempty"`
	Statement string   `json:"statement"`
	Multi     bool     `json:"multi,omitempty"` // several statements in one string
	Dollar    int      `json:"placeholders,omitempty"`
	Question  int      `json:"questionMarks,omitempty"`
	Note      string   `json:"note,omitempty"`
}

// Resolved reports whether the full SQL text is known statically.
func (t SQLText) Resolved() bool { return t.Text != "" }

type Param struct {
	Index int    `json:"index"` // $N
	Expr  string `json:"expr"`
	Type  string `json:"type,omitempty"`
	Line  int    `json:"line,omitempty"`
	Col   int    `json:"col,omitempty"`
	// Origins are the sources Data Flow finds for the value (TraceParams).
	Origins      []ParamOrigin `json:"origins,omitempty"`
	OriginStatus string        `json:"originStatus,omitempty"`

	value ssa.Value
}

// ParamOrigin is where a parameter value may come from.
type ParamOrigin struct {
	Label string `json:"label"`
	Kind  string `json:"kind"` // source | const | unresolved
	File  string `json:"file,omitempty"`
	Line  int    `json:"line,omitempty"`
	Func  string `json:"func,omitempty"`
	Conf  string `json:"conf"`
}

type ScanTarget struct {
	Expr string `json:"expr"`
	Type string `json:"type,omitempty"`
}

type Scan struct {
	Method  string       `json:"method"`
	File    string       `json:"file"`
	Line    int          `json:"line"`
	Targets []ScanTarget `json:"targets"`
	InLoop  bool         `json:"inLoop,omitempty"` // rows.Scan inside rows.Next()
	Note    string       `json:"note,omitempty"`
}

type TxRef struct {
	Role      string `json:"role"` // begin | in transaction | commit | rollback
	Var       string `json:"var,omitempty"`
	BeginLine int    `json:"beginLine,omitempty"`
	FromParam bool   `json:"fromParam,omitempty"` // transaction received as a parameter
	Deferred  bool   `json:"deferred,omitempty"`
}

// Op is one database operation found in the code.
type Op struct {
	ID       string             `json:"id"` // also the Code Flow node ID
	Lib      string             `json:"lib"`
	Op       string             `json:"op"`     // normalised: Query, QueryRow, Exec, Begin...
	Method   string             `json:"method"` // as called: QueryRowContext, Exec...
	Receiver string             `json:"receiver,omitempty"`
	Func     evidence.SymbolRef `json:"func"`
	File     string             `json:"file"`
	Line     int                `json:"line"`
	Call     string             `json:"call"`
	Conf     string             `json:"conf"`          // exact, or possible through a module interface
	Via      []string           `json:"via,omitempty"` // call chain from the inspected function
	SQL      SQLText            `json:"sql"`
	Params   []Param            `json:"params,omitempty"`
	ParamsOK bool               `json:"paramsKnown"` // false when arguments are passed as a slice (args...)
	Scan     *Scan              `json:"scan,omitempty"`
	Tx       *TxRef             `json:"tx,omitempty"`
	Notes    []string           `json:"notes,omitempty"`
	Deferred bool               `json:"deferred,omitempty"`

	fn    *ssa.Function
	instr ssa.CallInstruction
}

// Tx groups the operations of one transaction started in a function.
type Tx struct {
	Begin            string   `json:"begin"` // op ID
	Var              string   `json:"var,omitempty"`
	Func             string   `json:"func"`
	Line             int      `json:"line"`
	Ops              []string `json:"ops"`
	Commits          []int    `json:"commits,omitempty"`
	Rollbacks        []int    `json:"rollbacks,omitempty"`
	DeferredRollback bool     `json:"deferredRollback,omitempty"`
}

type Result struct {
	Symbol    evidence.SymbolRef    `json:"symbol"`
	Ops       []*Op                 `json:"ops"`
	Txs       []Tx                  `json:"transactions,omitempty"`
	Reachable bool                  `json:"reachable"`
	Depth     int                   `json:"depth,omitempty"`
	Status    evidence.Completeness `json:"status"`
	Notes     []string              `json:"notes,omitempty"`
	Evidence  []evidence.Evidence   `json:"evidence"`
}

type Options struct {
	Reachable bool // include operations of functions it calls
	Depth     int  // call depth for Reachable (default 4)
}

// ForFunction lists the operations performed by fn (and, with Reachable,
// by the module functions it may call).
func ForFunction(p *flow.Program, fn *ssa.Function, opt Options) *Result {
	fn = flow.Origin(fn)
	if opt.Depth <= 0 {
		opt.Depth = 4
	}
	res := &Result{Symbol: p.Ref(fn), Reachable: opt.Reachable}
	if opt.Reachable {
		res.Depth = opt.Depth
	}
	type visit struct {
		fn  *ssa.Function
		via []string
	}
	queue := []visit{{fn, nil}}
	seen := map[*ssa.Function]bool{fn: true}
	for len(queue) > 0 {
		v := queue[0]
		queue = queue[1:]
		for _, op := range Discover(p, v.fn) {
			op.Via = v.via
			if len(v.via) > 0 {
				op.Conf = weakest(op.Conf, v.via)
			}
			res.Ops = append(res.Ops, op)
		}
		res.Txs = append(res.Txs, transactions(p, v.fn, res.Ops)...)
		if !opt.Reachable || len(v.via) >= opt.Depth {
			if opt.Reachable && len(v.via) >= opt.Depth && len(p.CallSites(v.fn)) > 0 {
				res.Status.Limit(fmt.Sprintf("call depth limit (%d) reached", opt.Depth))
			}
			continue
		}
		for _, cs := range p.CallSites(v.fn) {
			if cs.Callback {
				continue
			}
			for _, t := range cs.Targets {
				if t.Fn == nil || !p.InModule(t.Fn) || seen[flow.Origin(t.Fn)] {
					continue
				}
				callee := flow.Origin(t.Fn)
				seen[callee] = true
				step := flow.ShortName(callee)
				if t.Conf != flow.Confirmed {
					step += " [" + t.Conf + "]"
				}
				queue = append(queue, visit{callee, append(append([]string(nil), v.via...), step)})
			}
		}
	}
	if opt.Reachable {
		res.Notes = append(res.Notes, "Operations in called functions are possible paths found statically, not executed queries.")
	}
	res.Evidence = append(res.Evidence, evidence.Evidence{Kind: evidence.Static, Basis: evidence.BasisStatic, SymbolID: res.Symbol.ID,
		File: res.Symbol.File, Line: res.Symbol.Line, Source: "golang.org/x/tools/go/ssa",
		Details: "database calls recognised by package and method (pgx, database/sql, sqlx), SQL resolved from constants"})
	res.Status.Done()
	return res
}

func weakest(conf string, via []string) string {
	for _, v := range via {
		switch {
		case strings.HasSuffix(v, "["+flow.Possible+"]"):
			return evidence.Possible
		case strings.HasSuffix(v, "["+flow.Likely+"]") && conf == evidence.Exact:
			conf = evidence.Likely
		}
	}
	return conf
}

// All lists every database operation of the module.
func All(p *flow.Program) []*Op {
	var out []*Op
	for _, fn := range p.Funcs() {
		out = append(out, Discover(p, fn)...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out
}

// Discover lists the database operations called directly by fn.
func Discover(p *flow.Program, fn *ssa.Function) []*Op {
	var out []*Op
	for _, cs := range p.CallSites(fn) {
		if cs.Callback {
			continue
		}
		for _, t := range cs.Targets {
			if t.DB == nil {
				continue
			}
			out = append(out, newOp(p, fn, cs, t))
			break
		}
	}
	return out
}

func newOp(p *flow.Program, fn *ssa.Function, cs flow.CallSite, t flow.Callee) *Op {
	c := cs.Instr.Common()
	op := &Op{ID: flow.DBNodeID(cs.File, cs.Line, t.DB.Op), Lib: t.DB.Lib, Op: t.DB.Op, Func: p.Ref(fn),
		File: cs.File, Line: cs.Line, Conf: evidence.Exact, fn: fn, instr: cs.Instr, ParamsOK: true}
	_, isDefer := cs.Instr.(*ssa.Defer)
	op.Deferred = isDefer
	if c.IsInvoke() {
		op.Method = c.Method.Name()
		op.Receiver = types.TypeString(c.Value.Type(), qualifier)
	} else if sc := c.StaticCallee(); sc != nil {
		op.Method = sc.Name()
		if r := sc.Signature.Recv(); r != nil {
			op.Receiver = types.TypeString(r.Type(), qualifier)
		}
	}
	if t.Conf == flow.Possible {
		op.Conf = evidence.Possible
		op.Notes = append(op.Notes, "called through module interface "+op.Receiver+", implemented by "+t.DB.ImplType)
	}
	call := p.CallExprAt(fn, c.Pos())
	if call != nil {
		op.Call = oneLine(p.NodeText(call), 160)
	}
	sqlIdx := flow.SQLArgIndex(c)
	if sqlIdx < 0 || sqlIdx >= len(c.Args) {
		op.SQL = SQLText{Kind: TextNone, Statement: ""}
	} else {
		shift := 0
		if !c.IsInvoke() && c.StaticCallee() != nil && c.StaticCallee().Signature.Recv() != nil {
			shift = 1
		}
		var astArg ast.Expr
		if call != nil && sqlIdx-shift < len(call.Args) {
			astArg = call.Args[sqlIdx-shift]
		}
		op.SQL = resolveSQL(p, fn, c.Args[sqlIdx], astArg, 0)
		op.params(p, fn, c, call, sqlIdx, shift)
		op.checkPlaceholders()
	}
	op.scan(p)
	return op
}

func qualifier(p *types.Package) string { return p.Name() }

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// resolveSQL describes the SQL argument without inventing missing parts.
func resolveSQL(p *flow.Program, fn *ssa.Function, v ssa.Value, astArg ast.Expr, depth int) SQLText {
	t := resolveValue(p, fn, v, astArg, depth)
	switch {
	case t.Text != "":
		t.Statement, t.Multi = Classify(t.Text)
		t.Formatted = Format(t.Text)
		t.Dollar, t.Question = Placeholders(t.Text)
	case len(t.Variants) > 0:
		t.Statement, t.Multi = Classify(t.Variants[0])
		for _, v := range t.Variants[1:] {
			if k, _ := Classify(v); k != t.Statement {
				t.Statement = StmtUnknown
			}
		}
		for _, v := range t.Variants {
			d, q := Placeholders(v)
			t.Dollar, t.Question = max(t.Dollar, d), max(t.Question, q)
		}
	case t.Template != "":
		t.Statement, _ = Classify(t.Template)
		t.Formatted = Format(t.Template)
		t.Dollar, t.Question = Placeholders(t.Template)
	default:
		t.Statement = StmtUnknown
	}
	return t
}

func resolveValue(p *flow.Program, fn *ssa.Function, v ssa.Value, astArg ast.Expr, depth int) SQLText {
	if depth > 4 {
		return SQLText{Kind: TextDynamic, Note: "SQL built too indirectly to resolve"}
	}
	switch x := v.(type) {
	case *ssa.Const:
		if x.Value == nil || x.Value.Kind() != constant.String {
			return SQLText{Kind: TextDynamic}
		}
		t := SQLText{Kind: TextLiteral, Text: constant.StringVal(x.Value)}
		if obj := constObject(p, fn, astArg); obj != nil {
			t.Kind = TextConstant
			t.Constant = obj.Pkg().Name() + "." + obj.Name()
			t.ConstFile, t.ConstLine, _ = p.Pos(obj.Pos())
		}
		return t
	case *ssa.BinOp:
		if x.Op == token.ADD {
			parts := concatParts(p, fn, x, 0)
			var b strings.Builder
			for _, part := range parts {
				b.WriteString(part)
			}
			return SQLText{Kind: TextConcat, Template: b.String(),
				Note: "built by string concatenation: ⟨…⟩ parts are computed at runtime"}
		}
	case *ssa.Phi:
		var variants []string
		for _, e := range x.Edges {
			sub := resolveValue(p, fn, e, nil, depth+1)
			if sub.Text == "" {
				return SQLText{Kind: TextDynamic, Note: "one of several values, not all of them literal"}
			}
			variants = appendUnique(variants, sub.Text)
		}
		if len(variants) == 1 {
			return SQLText{Kind: TextLiteral, Text: variants[0]}
		}
		return SQLText{Kind: TextAlternatives, Variants: variants, Note: fmt.Sprintf("one of %d literal statements, depending on the path taken", len(variants))}
	case *ssa.Call:
		c := x.Common()
		if sc := c.StaticCallee(); sc != nil {
			if obj, ok := sc.Object().(*types.Func); ok && obj.FullName() == "fmt.Sprintf" && len(c.Args) > 0 {
				if k, ok := c.Args[0].(*ssa.Const); ok && k.Value != nil && k.Value.Kind() == constant.String {
					format := constant.StringVal(k.Value)
					var args []string
					if call := p.CallExprAt(fn, x.Pos()); call != nil {
						for _, a := range call.Args[1:] {
							args = append(args, p.NodeText(a))
						}
					}
					return SQLText{Kind: TextSprintf, Template: sprintfTemplate(format, args),
						Note: "built with fmt.Sprintf: ⟨…⟩ parts are formatted at runtime"}
				}
			}
			if p.InModule(sc) {
				// A module function returning the SQL: its literal returns.
				callee := flow.Origin(sc)
				var variants []string
				for _, b := range callee.Blocks {
					for _, ins := range b.Instrs {
						if ret, ok := ins.(*ssa.Return); ok && len(ret.Results) > 0 {
							sub := resolveValue(p, callee, ret.Results[0], nil, depth+1)
							if sub.Text == "" {
								return SQLText{Kind: TextDynamic, Note: "built at runtime by " + flow.ShortName(callee)}
							}
							variants = appendUnique(variants, sub.Text)
						}
					}
				}
				if len(variants) == 1 {
					return SQLText{Kind: TextLiteral, Text: variants[0], Note: "returned by " + flow.ShortName(callee)}
				}
				if len(variants) > 1 {
					return SQLText{Kind: TextAlternatives, Variants: variants, Note: "one of the literals returned by " + flow.ShortName(callee)}
				}
			}
		}
		return SQLText{Kind: TextDynamic, Note: "returned by a call the analysis does not resolve"}
	case *ssa.Parameter:
		var variants []string
		all := true
		callers := p.CallersOf(x.Parent())
		idx := -1
		for i, prm := range x.Parent().Params {
			if prm == x {
				idx = i
			}
		}
		for _, cs := range callers {
			if cs.Callback || idx < 0 || cs.Instr.Common().IsInvoke() {
				all = false
				continue
			}
			args := cs.Instr.Common().Args
			if idx >= len(args) {
				all = false
				continue
			}
			sub := resolveValue(p, cs.Caller, args[idx], nil, depth+1)
			if sub.Text == "" {
				all = false
				continue
			}
			variants = appendUnique(variants, sub.Text)
		}
		t := SQLText{Kind: TextParameter, Note: "received as parameter " + x.Name()}
		if all && len(variants) > 0 {
			t.Variants = variants
			t.Note += fmt.Sprintf("; every caller in the module passes a literal (%d variant(s))", len(variants))
		} else if len(callers) == 0 {
			t.Note += "; no callers in the module"
		} else {
			t.Note += "; some callers pass SQL that is not a literal"
		}
		return t
	case *ssa.ChangeType:
		return resolveValue(p, fn, x.X, astArg, depth+1)
	}
	return SQLText{Kind: TextDynamic, Note: "computed at runtime"}
}

func appendUnique(xs []string, s string) []string {
	for _, x := range xs {
		if x == s {
			return xs
		}
	}
	return append(xs, s)
}

// constObject returns the named constant the SQL argument refers to.
func constObject(p *flow.Program, fn *ssa.Function, e ast.Expr) *types.Const {
	if e == nil {
		return nil
	}
	info := p.TypesInfo(fn)
	if info == nil {
		return nil
	}
	var id *ast.Ident
	switch x := ast.Unparen(e).(type) {
	case *ast.Ident:
		id = x
	case *ast.SelectorExpr:
		id = x.Sel
	}
	if id == nil {
		return nil
	}
	c, _ := info.Uses[id].(*types.Const)
	return c
}

// concatParts renders a + b + c with literal parts as text and runtime
// parts as ⟨expr⟩.
func concatParts(p *flow.Program, fn *ssa.Function, v ssa.Value, depth int) []string {
	if b, ok := v.(*ssa.BinOp); ok && b.Op == token.ADD && depth < 20 {
		return append(concatParts(p, fn, b.X, depth+1), concatParts(p, fn, b.Y, depth+1)...)
	}
	if k, ok := v.(*ssa.Const); ok && k.Value != nil && k.Value.Kind() == constant.String {
		return []string{constant.StringVal(k.Value)}
	}
	return []string{"⟨" + valueText(p, fn, v) + "⟩"}
}

// valueText names a runtime value by its variable or expression.
func valueText(p *flow.Program, fn *ssa.Function, v ssa.Value) string {
	if refs := v.Referrers(); refs != nil {
		for _, r := range *refs {
			if dr, ok := r.(*ssa.DebugRef); ok && dr.X == v && !dr.IsAddr {
				return oneLine(p.NodeText(dr.Expr), 40)
			}
		}
	}
	if prm, ok := v.(*ssa.Parameter); ok {
		return prm.Name()
	}
	if call := p.CallExprAt(fn, v.Pos()); call != nil {
		return oneLine(p.NodeText(call), 40)
	}
	return "value"
}

// sprintfTemplate replaces formatting verbs by the argument expressions.
func sprintfTemplate(format string, args []string) string {
	var b strings.Builder
	ai := 0
	for i := 0; i < len(format); i++ {
		c := format[i]
		if c != '%' {
			b.WriteByte(c)
			continue
		}
		if i+1 < len(format) && format[i+1] == '%' {
			b.WriteByte('%')
			i++
			continue
		}
		j := i + 1
		for j < len(format) && strings.IndexByte("+-# 0123456789.[]*", format[j]) >= 0 {
			j++
		}
		if j < len(format) {
			arg := "?"
			if ai < len(args) {
				arg = args[ai]
			}
			ai++
			b.WriteString("⟨" + oneLine(arg, 40) + "⟩")
			i = j
		}
	}
	return b.String()
}

// params lists the arguments after the SQL text: $1, $2...
func (op *Op) params(p *flow.Program, fn *ssa.Function, c *ssa.CallCommon, call *ast.CallExpr, sqlIdx, shift int) {
	rest := c.Args[sqlIdx+1:]
	if len(rest) == 0 {
		return
	}
	var values []ssa.Value
	variadic := c.Signature().Variadic()
	for i, a := range rest {
		if variadic && i == len(rest)-1 {
			elems, ok := flow.VariadicElems(a)
			if !ok {
				op.ParamsOK = false
				op.Notes = append(op.Notes, "arguments are passed as a slice (args...); individual parameters are not known statically")
				break
			}
			values = append(values, elems...)
			continue
		}
		values = append(values, a)
	}
	for i, v := range values {
		inner := v
		if mi, ok := v.(*ssa.MakeInterface); ok {
			inner = mi.X
		}
		prm := Param{Index: i + 1, Type: types.TypeString(inner.Type(), qualifier), value: inner}
		astIdx := sqlIdx - shift + 1 + i
		if call != nil && astIdx < len(call.Args) && !(call.Ellipsis.IsValid() && astIdx == len(call.Args)-1) {
			prm.Expr = oneLine(p.NodeText(call.Args[astIdx]), 80)
			_, prm.Line, prm.Col = p.Pos(call.Args[astIdx].Pos())
		} else {
			prm.Expr = valueText(p, fn, inner)
		}
		op.Params = append(op.Params, prm)
	}
}

func (op *Op) checkPlaceholders() {
	if !op.SQL.Resolved() || !op.ParamsOK {
		return
	}
	n := len(op.Params)
	switch {
	case op.SQL.Dollar > 0 && op.SQL.Dollar != n:
		op.Notes = append(op.Notes, fmt.Sprintf("the SQL uses placeholders up to $%d but %d argument(s) are passed", op.SQL.Dollar, n))
	case op.SQL.Dollar == 0 && op.SQL.Question > 0 && op.SQL.Question != n:
		op.Notes = append(op.Notes, fmt.Sprintf("the SQL has %d ? placeholder(s) but %d argument(s) are passed", op.SQL.Question, n))
	case op.SQL.Dollar == 0 && op.SQL.Question == 0 && n > 0:
		op.Notes = append(op.Notes, fmt.Sprintf("%d argument(s) are passed but the SQL has no placeholders", n))
	}
}

// scan finds where the rows returned by the operation are scanned to.
func (op *Op) scan(p *flow.Program) {
	call, ok := op.instr.(*ssa.Call)
	if !ok || (op.Op != "QueryRow" && op.Op != "Query") {
		return
	}
	var rows []ssa.Value
	if op.Op == "QueryRow" {
		rows = []ssa.Value{call}
	} else if refs := call.Referrers(); refs != nil {
		for _, r := range *refs {
			if ex, ok := r.(*ssa.Extract); ok && ex.Index == 0 {
				rows = append(rows, ex)
			}
		}
		if _, isTuple := call.Type().(*types.Tuple); !isTuple {
			rows = []ssa.Value{call}
		}
	}
	for _, rv := range rows {
		for _, ci := range receiverCalls(rv) {
			c := ci.Common()
			name := ""
			if c.IsInvoke() {
				name = c.Method.Name()
			} else if sc := c.StaticCallee(); sc != nil {
				name = sc.Name()
			}
			switch name {
			case "Scan":
				s := &Scan{Method: "Scan"}
				s.File, s.Line, _ = p.Pos(ci.Pos())
				s.InLoop = op.Op == "Query"
				if ce := p.CallExprAt(ci.Parent(), c.Pos()); ce != nil {
					for _, a := range ce.Args {
						tv := p.TypesInfo(ci.Parent()).Types[a]
						typ := ""
						if tv.Type != nil {
							typ = types.TypeString(tv.Type, qualifier)
						}
						s.Targets = append(s.Targets, ScanTarget{Expr: oneLine(p.NodeText(a), 60), Type: typ})
					}
					if ce.Ellipsis.IsValid() {
						s.Note = "destinations passed as a slice; individual targets are not known statically"
					}
				}
				op.Scan = s
				return
			}
		}
		// Rows handed to a helper (pgx.CollectRows, a module function...).
		if refs := rv.Referrers(); refs != nil {
			for _, r := range *refs {
				if ci, ok := r.(ssa.CallInstruction); ok {
					for _, a := range ci.Common().Args {
						if a == rv {
							s := &Scan{Method: "(passed on)", Note: "rows are handed to " + calleeLabel(ci.Common()) + "; scanning happens there"}
							s.File, s.Line, _ = p.Pos(ci.Pos())
							op.Scan = s
							return
						}
					}
				}
			}
		}
	}
}

func calleeLabel(c *ssa.CallCommon) string {
	if c.IsInvoke() {
		return c.Method.Name()
	}
	if sc := c.StaticCallee(); sc != nil {
		return flow.ShortName(flow.Unwrap(sc))
	}
	return "a function value"
}

// receiverCalls returns the method calls made on v (or on conversions of v).
func receiverCalls(v ssa.Value) []ssa.CallInstruction {
	var out []ssa.CallInstruction
	var visit func(v ssa.Value, depth int)
	visit = func(v ssa.Value, depth int) {
		refs := v.Referrers()
		if refs == nil || depth > 3 {
			return
		}
		for _, r := range *refs {
			switch x := r.(type) {
			case ssa.CallInstruction:
				c := x.Common()
				if (c.IsInvoke() && c.Value == v) || (!c.IsInvoke() && len(c.Args) > 0 && c.Args[0] == v && c.StaticCallee() != nil && c.StaticCallee().Signature.Recv() != nil) {
					out = append(out, x)
				}
			case *ssa.ChangeInterface:
				visit(x, depth+1)
			case *ssa.MakeInterface:
				visit(x, depth+1)
			case *ssa.ChangeType:
				visit(x, depth+1)
			}
		}
	}
	visit(v, 0)
	return out
}

// transactions groups the operations of fn by the Begin call whose result
// they use as receiver, and marks operations on transactions received as
// parameters.
func transactions(p *flow.Program, fn *ssa.Function, ops []*Op) []Tx {
	var mine []*Op
	for _, op := range ops {
		if op.fn == fn {
			mine = append(mine, op)
		}
	}
	byBegin := map[ssa.Value]*Tx{}
	var order []ssa.Value
	for _, op := range mine {
		if op.Op != "Begin" {
			continue
		}
		call, ok := op.instr.(*ssa.Call)
		if !ok {
			continue
		}
		tx := &Tx{Begin: op.ID, Func: flow.ShortName(fn), Line: op.Line}
		op.Tx = &TxRef{Role: "begin", BeginLine: op.Line}
		if refs := call.Referrers(); refs != nil {
			for _, r := range *refs {
				if ex, ok := r.(*ssa.Extract); ok && ex.Index == 0 {
					tx.Var = varName(ex)
					byBegin[ex] = tx
					order = append(order, ex)
				}
			}
		}
		op.Tx.Var = tx.Var
	}
	for _, op := range mine {
		if op.Op == "Begin" {
			continue
		}
		c := op.instr.Common()
		var recv ssa.Value
		if c.IsInvoke() {
			recv = c.Value
		} else if len(c.Args) > 0 {
			recv = c.Args[0]
		}
		recv = unconvert(recv)
		role := "in transaction"
		switch op.Op {
		case "Commit":
			role = "commit"
		case "Rollback":
			role = "rollback"
		}
		if tx, ok := byBegin[recv]; ok {
			op.Tx = &TxRef{Role: role, Var: tx.Var, BeginLine: tx.Line, Deferred: op.Deferred}
			tx.Ops = append(tx.Ops, op.ID)
			switch role {
			case "commit":
				tx.Commits = append(tx.Commits, op.Line)
			case "rollback":
				tx.Rollbacks = append(tx.Rollbacks, op.Line)
				tx.DeferredRollback = tx.DeferredRollback || op.Deferred
			}
			continue
		}
		if prm, ok := recv.(*ssa.Parameter); ok && isTxType(prm.Type()) {
			op.Tx = &TxRef{Role: role, Var: prm.Name(), FromParam: true, Deferred: op.Deferred}
		}
	}
	var out []Tx
	for _, k := range order {
		out = append(out, *byBegin[k])
	}
	return out
}

func unconvert(v ssa.Value) ssa.Value {
	for i := 0; i < 4 && v != nil; i++ {
		switch x := v.(type) {
		case *ssa.ChangeInterface:
			v = x.X
		case *ssa.MakeInterface:
			v = x.X
		case *ssa.ChangeType:
			v = x.X
		default:
			return v
		}
	}
	return v
}

func isTxType(t types.Type) bool {
	s := types.TypeString(t, nil)
	return strings.HasSuffix(s, "pgx/v5.Tx") || strings.HasSuffix(s, "pgx.Tx") || s == "*database/sql.Tx" || strings.HasSuffix(s, "sqlx.Tx")
}

func varName(v ssa.Value) string {
	if refs := v.Referrers(); refs != nil {
		for _, r := range *refs {
			if dr, ok := r.(*ssa.DebugRef); ok && dr.X == v {
				if obj, ok := dr.Object().(*types.Var); ok {
					return obj.Name()
				}
			}
		}
	}
	return ""
}

// TraceParams follows every parameter backwards with Data Flow and records
// the origins found (input sources, constants, or where the trace stopped).
func (op *Op) TraceParams(p *flow.Program, opt dataflow.Options) {
	opt.Direction = dataflow.Origin
	for i := range op.Params {
		prm := &op.Params[i]
		if prm.value == nil {
			continue
		}
		r := dataflow.TraceValue(p, op.fn, prm.value, dataflow.Selection{Kind: "value", Name: prm.Expr, Line: prm.Line}, opt)
		prm.OriginStatus = string(r.Status.Status)
		hasIn := map[int]bool{}
		for _, e := range r.Edges {
			if e.Dir == string(dataflow.Origin) {
				hasIn[e.To] = true
			}
		}
		for _, n := range r.Nodes {
			// Origins are where the backward trace ends.
			ends := true
			for _, e := range r.Edges {
				if e.Dir == string(dataflow.Origin) && e.To == n.ID {
					ends = false
					break
				}
			}
			if !ends || n.Dir == "selected" {
				continue
			}
			kind := n.Kind
			switch kind {
			case dataflow.KindSource, dataflow.KindConst, dataflow.KindUnresolved, dataflow.KindGlobal:
			default:
				continue
			}
			prm.Origins = append(prm.Origins, ParamOrigin{Label: n.Label, Kind: kind, File: n.File, Line: n.Line, Func: n.FuncName, Conf: n.Conf})
		}
	}
}

// Value returns the SSA value of a parameter, for Data Flow.
func (pr Param) Value() ssa.Value { return pr.value }

// Function returns the function performing the operation.
func (op *Op) Function() *ssa.Function { return op.fn }

// Label is a short description: "pgx QueryRow at repository/pg.go:22".
func (op *Op) Label() string {
	return op.Lib + " " + op.Op + " at " + op.File + ":" + strconv.Itoa(op.Line)
}
