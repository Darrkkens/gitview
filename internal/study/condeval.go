package study

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"github.com/athos/gitview/internal/flow"
)

// cond is a branch condition the branch challenge can evaluate: integer
// and boolean operands (variables, fields, literals, constants) combined
// with > < >= <= == != && || !. Anything else is refused.
type cond struct {
	op   string // lit | var | ! | && | || | > | < | >= | <= | == | !=
	name string // var: the expression as written
	ival int64
	bval bool
	kind string // int | bool
	x, y *cond
}

// condOf finds the if condition at file:line of function id and converts
// it, using the type checker for the operand types.
func condOf(progs []*flow.Program, id, text string, line int) (*cond, bool) {
	for _, p := range progs {
		fn := p.FuncByID(id)
		if fn == nil || fn.Syntax() == nil {
			continue
		}
		info := p.TypesInfo(fn)
		if info == nil {
			return nil, false
		}
		var found ast.Expr
		ast.Inspect(fn.Syntax(), func(n ast.Node) bool {
			if found != nil {
				return false
			}
			if s, ok := n.(*ast.IfStmt); ok {
				if _, l, _ := p.Pos(s.Cond.Pos()); l == line && normSQL(p.NodeText(s.Cond)) == normSQL(text) {
					found = s.Cond
				}
			}
			return true
		})
		if found == nil {
			return nil, false
		}
		c, ok := convert(p, info, found)
		if !ok || c.kind != "bool" {
			return nil, false
		}
		return c, true
	}
	return nil, false
}

func convert(p *flow.Program, info *types.Info, e ast.Expr) (*cond, bool) {
	// Constants (literals, named constants, true/false).
	if tv, ok := info.Types[e]; ok && tv.Value != nil {
		switch tv.Value.Kind() {
		case constant.Int:
			if v, exact := constant.Int64Val(tv.Value); exact {
				return &cond{op: "lit", ival: v, kind: "int", name: normSQL(p.NodeText(e))}, true
			}
		case constant.Bool:
			return &cond{op: "lit", bval: constant.BoolVal(tv.Value), kind: "bool", name: normSQL(p.NodeText(e))}, true
		}
		return nil, false
	}
	switch e := e.(type) {
	case *ast.ParenExpr:
		return convert(p, info, e.X)
	case *ast.UnaryExpr:
		if e.Op != token.NOT {
			return nil, false
		}
		x, ok := convert(p, info, e.X)
		if !ok || x.kind != "bool" {
			return nil, false
		}
		return &cond{op: "!", x: x, kind: "bool"}, true
	case *ast.BinaryExpr:
		x, ok1 := convert(p, info, e.X)
		y, ok2 := convert(p, info, e.Y)
		if !ok1 || !ok2 || x.kind != y.kind {
			return nil, false
		}
		switch e.Op {
		case token.LAND, token.LOR:
			if x.kind != "bool" {
				return nil, false
			}
		case token.GTR, token.LSS, token.GEQ, token.LEQ:
			if x.kind != "int" {
				return nil, false
			}
		case token.EQL, token.NEQ:
		default:
			return nil, false
		}
		return &cond{op: e.Op.String(), x: x, y: y, kind: "bool"}, true
	case *ast.Ident, *ast.SelectorExpr:
		t := info.TypeOf(e)
		if t == nil {
			return nil, false
		}
		b, ok := t.Underlying().(*types.Basic)
		if !ok {
			return nil, false
		}
		name := normSQL(p.NodeText(e))
		switch {
		case b.Info()&types.IsInteger != 0:
			return &cond{op: "var", name: name, kind: "int"}, true
		case b.Info()&types.IsBoolean != 0:
			return &cond{op: "var", name: name, kind: "bool"}, true
		}
	}
	return nil, false
}

// vars lists the variables of c with their kinds, and the integer
// literals it compares with.
func (c *cond) vars(out map[string]string, lits map[int64]bool) {
	if c == nil {
		return
	}
	switch c.op {
	case "var":
		out[c.name] = c.kind
	case "lit":
		if c.kind == "int" {
			lits[c.ival] = true
		}
	}
	c.x.vars(out, lits)
	c.y.vars(out, lits)
}

type assignment struct {
	ints  map[string]int64
	bools map[string]bool
}

func (c *cond) eval(a assignment) (int64, bool) {
	switch c.op {
	case "lit":
		return c.ival, c.bval
	case "var":
		return a.ints[c.name], a.bools[c.name]
	case "!":
		_, b := c.x.eval(a)
		return 0, !b
	case "&&":
		_, l := c.x.eval(a)
		_, r := c.y.eval(a)
		return 0, l && r
	case "||":
		_, l := c.x.eval(a)
		_, r := c.y.eval(a)
		return 0, l || r
	}
	li, lb := c.x.eval(a)
	ri, rb := c.y.eval(a)
	switch c.op {
	case ">":
		return 0, li > ri
	case "<":
		return 0, li < ri
	case ">=":
		return 0, li >= ri
	case "<=":
		return 0, li <= ri
	case "==":
		if c.x.kind == "bool" {
			return 0, lb == rb
		}
		return 0, li == ri
	case "!=":
		if c.x.kind == "bool" {
			return 0, lb != rb
		}
		return 0, li != ri
	}
	return 0, false
}

const maxCondVars = 4

// scenarios returns one assignment that makes c true and one that makes it
// false (when they exist), chosen deterministically from small values
// around the literals of the condition.
func (c *cond) scenarios(seed string) (whenTrue, whenFalse *assignment, given map[*assignment][]string) {
	kinds := map[string]string{}
	lits := map[int64]bool{}
	c.vars(kinds, lits)
	if len(kinds) == 0 || len(kinds) > maxCondVars {
		return nil, nil, nil
	}
	cand := map[int64]bool{0: true, 5: true, 10: true, 12: true}
	for l := range lits {
		cand[l-1], cand[l], cand[l+1] = true, true, true
	}
	var ints []int64
	for v := range cand {
		ints = append(ints, v)
	}
	sort.Slice(ints, func(i, j int) bool { return ints[i] < ints[j] })
	names := make([]string, 0, len(kinds))
	for n := range kinds {
		names = append(names, n)
	}
	sort.Strings(names)
	// Enumerate the grid in a seed-dependent but fixed order.
	type option struct {
		a   assignment
		txt []string
		key uint32
	}
	var all []option
	var rec func(i int, a assignment, txt []string)
	rec = func(i int, a assignment, txt []string) {
		if len(all) > 4096 {
			return
		}
		if i == len(names) {
			ca := assignment{ints: map[string]int64{}, bools: map[string]bool{}}
			for k, v := range a.ints {
				ca.ints[k] = v
			}
			for k, v := range a.bools {
				ca.bools[k] = v
			}
			t := append([]string(nil), txt...)
			all = append(all, option{ca, t, hash(seed + strings.Join(t, ","))})
			return
		}
		n := names[i]
		if kinds[n] == "bool" {
			for _, b := range []bool{false, true} {
				a.bools[n] = b
				s := "false"
				if b {
					s = "true"
				}
				rec(i+1, a, append(txt, n+" = "+s))
			}
			return
		}
		for _, v := range ints {
			a.ints[n] = v
			rec(i+1, a, append(txt, n+" = "+itoa(int(v))))
		}
	}
	rec(0, assignment{ints: map[string]int64{}, bools: map[string]bool{}}, nil)
	sort.SliceStable(all, func(i, j int) bool { return all[i].key < all[j].key })
	given = map[*assignment][]string{}
	for i := range all {
		_, v := c.eval(all[i].a)
		switch {
		case v && whenTrue == nil:
			whenTrue = &all[i].a
			given[whenTrue] = all[i].txt
		case !v && whenFalse == nil:
			whenFalse = &all[i].a
			given[whenFalse] = all[i].txt
		}
	}
	return whenTrue, whenFalse, given
}
