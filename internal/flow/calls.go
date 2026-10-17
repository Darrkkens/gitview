package flow

import (
	"context"
	"fmt"
	"go/constant"
	"go/types"
	"sort"
	"strings"

	"golang.org/x/tools/go/ssa"
)

// target is one possible callee of a call site.
type target struct {
	fn   *ssa.Function // nil for unresolved or external interface methods
	conf string
	note string

	// External interface method with no implementation in the module.
	extName string
	extPkg  string

	db *dbOp
}

type callSite struct {
	instr    ssa.CallInstruction
	kind     string // call | go | defer
	pos      int    // call site line
	file     string
	targets  []target
	desc     string // description for unresolved calls
	callback bool   // a function value passed as argument, not the call itself
}

type callerRef struct {
	caller   *ssa.Function
	instr    ssa.CallInstruction
	file     string
	line     int
	conf     string
	callback bool // the function is passed as an argument, not called here
}

// index resolves every call site of the module once.
func (p *Program) index(ctx context.Context) {
	for _, fn := range p.funcs {
		if ctx.Err() != nil {
			return
		}
		for _, b := range fn.Blocks {
			for _, ins := range b.Instrs {
				cs, ok := p.callSiteOf(ins)
				if !ok {
					continue
				}
				p.calls[fn] = append(p.calls[fn], cs)
				for _, t := range cs.targets {
					if t.fn != nil && p.inModule(t.fn) {
						p.callers[origin(t.fn)] = append(p.callers[origin(t.fn)], callerRef{caller: fn, instr: cs.instr, file: cs.file, line: cs.pos, conf: t.conf})
					}
				}
			}
			// Function values passed as arguments (callbacks, errgroup.Go,
			// http.HandlerFunc...) may run inside the callee.
			for _, ins := range b.Instrs {
				ci, ok := ins.(ssa.CallInstruction)
				if !ok {
					continue
				}
				for _, a := range ci.Common().Args {
					cb := funcValue(a)
					if cb == nil || !p.inModule(cb) || cb == ci.Common().StaticCallee() {
						continue
					}
					file, line := p.site(ci.Pos())
					callee := "a call"
					if sc := ci.Common().StaticCallee(); sc != nil {
						callee = shortName(sc)
					} else if ci.Common().IsInvoke() {
						callee = ci.Common().Method.Name()
					}
					cs := callSite{instr: ci, kind: EdgeCall, pos: line, file: file, callback: true, targets: []target{{
						fn: cb, conf: Possible, note: "function value passed to " + callee + "; it runs if the callee invokes it",
					}}}
					p.calls[fn] = append(p.calls[fn], cs)
					p.callers[origin(cb)] = append(p.callers[origin(cb)], callerRef{caller: fn, instr: ci, file: file, line: line, conf: Possible, callback: true})
				}
			}
		}
	}
}

// funcValue returns the function a value statically denotes, if any.
func funcValue(v ssa.Value) *ssa.Function {
	switch v := v.(type) {
	case *ssa.Function:
		return unwrap(v)
	case *ssa.MakeClosure:
		if fn, ok := v.Fn.(*ssa.Function); ok {
			return unwrap(fn)
		}
	case *ssa.ChangeType:
		return funcValue(v.X)
	case *ssa.Convert:
		return funcValue(v.X)
	}
	return nil
}

// unwrap follows synthetic wrappers (bound methods, thunks, generic
// instances) to the declared function they call.
func unwrap(fn *ssa.Function) *ssa.Function {
	for i := 0; fn != nil && fn.Synthetic != "" && i < 4; i++ {
		var next *ssa.Function
		for _, b := range fn.Blocks {
			for _, ins := range b.Instrs {
				if c, ok := ins.(ssa.CallInstruction); ok {
					if sc := c.Common().StaticCallee(); sc != nil {
						next = sc
					}
				}
			}
		}
		if next == nil {
			break
		}
		fn = next
	}
	return origin(fn)
}

func (p *Program) callSiteOf(ins ssa.Instruction) (callSite, bool) {
	var common *ssa.CallCommon
	kind := EdgeCall
	switch c := ins.(type) {
	case *ssa.Call:
		common = c.Common()
	case *ssa.Go:
		common, kind = c.Common(), EdgeGo
	case *ssa.Defer:
		common, kind = c.Common(), EdgeDefer
	default:
		return callSite{}, false
	}
	if common.IsInvoke() || common.StaticCallee() != nil {
		// handled below
	} else if _, builtin := common.Value.(*ssa.Builtin); builtin {
		return callSite{}, false
	}
	file, line := p.site(ins.Pos())
	cs := callSite{instr: ins.(ssa.CallInstruction), kind: kind, pos: line, file: file}
	cs.targets, cs.desc = p.resolve(common)
	return cs, true
}

// resolve returns the possible targets of a call.
func (p *Program) resolve(c *ssa.CallCommon) ([]target, string) {
	if c.IsInvoke() {
		return p.resolveInvoke(c)
	}
	if fn := c.StaticCallee(); fn != nil {
		fn = unwrap(fn)
		t := target{fn: fn, conf: Confirmed}
		if op := p.dbOpOf(fn, c); op != nil {
			t.db = op
		}
		return []target{t}, ""
	}
	desc := "call through function value"
	if name := valueName(c.Value); name != "" {
		desc += " " + name
	}
	return []target{{conf: Unresolved, note: desc + "; target not determinable statically"}}, desc
}

func valueName(v ssa.Value) string {
	switch v := v.(type) {
	case *ssa.Parameter:
		return v.Name()
	case *ssa.FreeVar:
		return v.Name()
	case *ssa.UnOp:
		if fa, ok := v.X.(*ssa.FieldAddr); ok {
			if st, ok := fa.X.Type().Underlying().(*types.Pointer); ok {
				if s, ok := st.Elem().Underlying().(*types.Struct); ok {
					return "field " + s.Field(fa.Field).Name()
				}
			}
		}
		if g, ok := v.X.(*ssa.Global); ok {
			return g.Name()
		}
	case *ssa.Field:
		if s, ok := v.X.Type().Underlying().(*types.Struct); ok {
			return "field " + s.Field(v.Field).Name()
		}
	}
	return ""
}

// resolveInvoke finds the implementations of an interface method among the
// module's named types (class-hierarchy analysis restricted to the module).
func (p *Program) resolveInvoke(c *ssa.CallCommon) ([]target, string) {
	iface := c.Value.Type()
	m := c.Method
	ifaceName := types.TypeString(iface, shortQualifier)
	if op := dbInterfaceOp(iface, m.Name()); op != nil {
		if i := SQLArgIndex(c); i >= 0 {
			op.sql, op.dynamic = sqlValue(c.Args[i])
		}
		return []target{{conf: Confirmed, db: op, extName: ifaceName + "." + m.Name(), extPkg: pkgOf(iface)}}, ""
	}
	impls := p.implementations(iface, m)
	if len(impls) == 0 {
		// A module interface backed by a database driver type.
		if op := p.dbImplOp(iface, m.Name()); op != nil {
			if i := SQLArgIndex(c); i >= 0 {
				op.sql, op.dynamic = sqlValue(c.Args[i])
			}
			return []target{{conf: Possible, db: op, extName: ifaceName + "." + m.Name(), extPkg: pkgOf(iface),
				note: "interface " + ifaceName + " is implemented by " + op.implType}}, ""
		}
		if pkg := pkgOf(iface); pkg != "" && !p.isModulePath(pkg) {
			return []target{{conf: Possible, extName: ifaceName + "." + m.Name(), extPkg: pkg,
				note: "interface method; no implementation in the module"}}, ""
		}
		desc := "interface call " + ifaceName + "." + m.Name()
		return []target{{conf: Unresolved, note: desc + " has no implementation in the module"}}, desc
	}
	conf := Possible
	if len(impls) == 1 {
		conf = Likely
	}
	out := make([]target, 0, len(impls))
	for _, fn := range impls {
		out = append(out, target{fn: fn, conf: conf, note: fmt.Sprintf("via interface %s (%d implementation(s) in the module)", ifaceName, len(impls))})
	}
	return out, ""
}

func (p *Program) isModulePath(pkg string) bool {
	return p.Module != "" && (pkg == p.Module || strings.HasPrefix(pkg, p.Module+"/"))
}

func (p *Program) implementations(iface types.Type, m *types.Func) []*ssa.Function {
	key := iface.String() + "." + m.Name()
	p.implMu.Lock()
	defer p.implMu.Unlock()
	if r, ok := p.implCache[key]; ok {
		return r
	}
	it, ok := iface.Underlying().(*types.Interface)
	if !ok {
		return nil
	}
	var out []*ssa.Function
	for _, t := range p.named {
		var recv types.Type
		switch {
		case types.Implements(t, it):
			recv = t
		case types.Implements(types.NewPointer(t), it):
			recv = types.NewPointer(t)
		default:
			continue
		}
		if fn := p.prog.LookupMethod(recv, m.Pkg(), m.Name()); fn != nil {
			if fn = unwrap(fn); p.inModule(fn) {
				out = append(out, fn)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	p.implCache[key] = out
	return out
}

func pkgOf(t types.Type) string {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if n, ok := t.(*types.Named); ok && n.Obj().Pkg() != nil {
		return n.Obj().Pkg().Path()
	}
	return ""
}

// --- database operations -----------------------------------------------------

type dbOp struct {
	lib      string
	op       string
	sql      string
	dynamic  bool
	implType string
}

var dbLibs = []struct{ prefix, lib string }{
	{"github.com/jackc/pgx", "pgx"},
	{"database/sql", "database/sql"},
	{"github.com/jmoiron/sqlx", "sqlx"},
}

var dbMethods = map[string]string{
	"Query": "Query", "QueryRow": "QueryRow", "Exec": "Exec", "Begin": "Begin", "BeginTx": "Begin",
	"BeginFunc": "Begin", "Commit": "Commit", "Rollback": "Rollback", "SendBatch": "SendBatch",
	"CopyFrom": "CopyFrom", "QueryContext": "Query", "QueryRowContext": "QueryRow", "ExecContext": "Exec",
	"Prepare": "Prepare", "PrepareContext": "Prepare", "Queue": "Queue",
}

func dbLib(pkg string) string {
	for _, l := range dbLibs {
		if pkg == l.prefix || strings.HasPrefix(pkg, l.prefix+"/") {
			return l.lib
		}
	}
	return ""
}

// dbOpOf recognises a static call to a database method.
func (p *Program) dbOpOf(fn *ssa.Function, c *ssa.CallCommon) *dbOp {
	if fn.Signature.Recv() == nil || fn.Pkg == nil {
		return nil
	}
	lib := dbLib(fn.Pkg.Pkg.Path())
	op := dbMethods[fn.Name()]
	if lib == "" || op == "" {
		return nil
	}
	d := &dbOp{lib: lib, op: op}
	if i := SQLArgIndex(c); i >= 0 {
		d.sql, d.dynamic = sqlValue(c.Args[i])
	}
	return d
}

func dbInterfaceOp(iface types.Type, method string) *dbOp {
	lib := dbLib(pkgOf(iface))
	op := dbMethods[method]
	if lib == "" || op == "" {
		return nil
	}
	return &dbOp{lib: lib, op: op}
}

// dbImplOp checks whether a module interface whose method is a database
// operation is implemented by a known driver type loaded in the program.
func (p *Program) dbImplOp(iface types.Type, method string) *dbOp {
	op := dbMethods[method]
	it, ok := iface.Underlying().(*types.Interface)
	if op == "" || !ok {
		return nil
	}
	for _, pk := range p.prog.AllPackages() {
		lib := dbLib(pk.Pkg.Path())
		if lib == "" {
			continue
		}
		for _, name := range []string{"Pool", "Conn", "DB", "Tx"} {
			obj := pk.Pkg.Scope().Lookup(name)
			if obj == nil {
				continue
			}
			t := obj.Type()
			for _, recv := range []types.Type{t, types.NewPointer(t)} {
				if _, isIface := t.Underlying().(*types.Interface); isIface {
					continue
				}
				if types.Implements(recv, it) {
					return &dbOp{lib: lib, op: op, implType: types.TypeString(recv, shortQualifier)}
				}
			}
		}
	}
	return nil
}

// SQLArgIndex returns the index in c.Args of the SQL text argument: the
// parameter the API declares as "sql" or "query" (pgx, database/sql, sqlx),
// or else the first string parameter. It returns -1 when there is none.
func SQLArgIndex(c *ssa.CallCommon) int {
	var sig *types.Signature
	shift := 0
	if c.IsInvoke() {
		sig, _ = c.Method.Type().(*types.Signature)
	} else if fn := c.StaticCallee(); fn != nil {
		sig = fn.Signature
		if sig.Recv() != nil {
			shift = 1 // Args[0] is the receiver
		}
	}
	if sig == nil {
		return -1
	}
	first := -1
	for i := 0; i < sig.Params().Len(); i++ {
		prm := sig.Params().At(i)
		b, ok := prm.Type().Underlying().(*types.Basic)
		if !ok || b.Kind() != types.String {
			continue
		}
		if prm.Name() == "sql" || prm.Name() == "query" {
			return i + shift
		}
		if first < 0 {
			first = i + shift
		}
	}
	if first >= len(c.Args) {
		return -1
	}
	return first
}

// sqlValue returns the literal value of a SQL argument, or dynamic=true when
// it is computed at runtime.
func sqlValue(a ssa.Value) (sql string, dynamic bool) {
	if k, ok := a.(*ssa.Const); ok && k.Value != nil && k.Value.Kind() == constant.String {
		return constant.StringVal(k.Value), false
	}
	return "", true
}

// --- naming ------------------------------------------------------------------

func shortQualifier(p *types.Package) string { return p.Name() }

// shortName renders fn like Go stack traces: pkg.Func, pkg.(*T).Method, pkg.Func.func1.
func shortName(fn *ssa.Function) string {
	fn = origin(fn)
	if fn.Parent() != nil {
		parent := shortName(fn.Parent())
		idx := 1
		for i, a := range fn.Parent().AnonFuncs {
			if a == fn {
				idx = i + 1
			}
		}
		return fmt.Sprintf("%s.func%d", parent, idx)
	}
	pkg := ""
	if fn.Pkg != nil {
		pkg = fn.Pkg.Pkg.Name() + "."
	}
	if recv := fn.Signature.Recv(); recv != nil {
		t := recv.Type()
		ptr := ""
		if pt, ok := t.(*types.Pointer); ok {
			t, ptr = pt.Elem(), "*"
		}
		name := types.TypeString(t, func(*types.Package) string { return "" })
		if i := strings.IndexByte(name, '['); i >= 0 {
			name = name[:i]
		}
		if ptr != "" {
			return fmt.Sprintf("%s(*%s).%s", pkg, name, fn.Name())
		}
		return fmt.Sprintf("%s%s.%s", pkg, name, fn.Name())
	}
	return pkg + fn.Name()
}

func signature(fn *ssa.Function) string {
	fn = origin(fn)
	sig := types.TypeString(fn.Signature, shortQualifier)
	name := fn.Name()
	if fn.Parent() != nil {
		return "func" + strings.TrimPrefix(sig, "func")
	}
	if recv := fn.Signature.Recv(); recv != nil {
		return fmt.Sprintf("func (%s %s) %s%s", recv.Name(), types.TypeString(recv.Type(), shortQualifier), name, strings.TrimPrefix(sig, "func"))
	}
	return "func " + name + strings.TrimPrefix(sig, "func")
}
