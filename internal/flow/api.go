package flow

// Exported view of an analysed module, shared by the Go Deep Inspector
// modules (Branch Explorer, Data Flow, SQL Explorer, Performance and
// Execution Trace) so they all speak about the same functions.

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/athos/gitview/internal/evidence"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
)

// Fset returns the file set of the analysed sources.
func (p *Program) Fset() *token.FileSet { return p.fset }

// SSA returns the SSA program. It is fully built and must not be modified.
func (p *Program) SSA() *ssa.Program { return p.prog }

// Funcs lists every function with a body declared in the module, closures
// included, in source order. The slice must not be modified.
func (p *Program) Funcs() []*ssa.Function { return p.funcs }

// InModule reports whether fn is declared (with a body) in the module.
func (p *Program) InModule(fn *ssa.Function) bool { return p.inModule(fn) }

// FuncByID returns the module function (or closure) with the given ID.
func (p *Program) FuncByID(id string) *ssa.Function { return p.funcByID(id) }

// Package returns the type-checked package that declares fn.
func (p *Program) Package(fn *ssa.Function) *packages.Package {
	fn = origin(fn)
	if fn == nil || fn.Pkg == nil {
		return nil
	}
	return p.pkgs[fn.Pkg.Pkg]
}

// TypesInfo returns the type information of fn's package.
func (p *Program) TypesInfo(fn *ssa.Function) *types.Info {
	if pk := p.Package(fn); pk != nil {
		return pk.TypesInfo
	}
	return nil
}

// RelFile maps an absolute path to a repository-relative one.
func (p *Program) RelFile(abs string) string { return p.relFile(abs) }

// Pos returns the repository-relative file, line and column of pos.
func (p *Program) Pos(pos token.Pos) (file string, line, col int) {
	if !pos.IsValid() {
		return "", 0, 0
	}
	ps := p.fset.Position(pos)
	return p.relFile(ps.Filename), ps.Line, ps.Column
}

// Source returns lines [from, to] of a repository-relative file.
func (p *Program) Source(file string, from, to int) string { return p.source(file, from, to) }

// Range returns the file and the first and last line of fn.
func (p *Program) Range(fn *ssa.Function) (file string, line, end int) { return p.position(fn) }

// SymbolOf describes fn with the shared symbol identity.
func (p *Program) SymbolOf(fn *ssa.Function) Symbol { return p.symbol(origin(fn)) }

// Ref returns the evidence identity of fn, including the analysed version.
func (p *Program) Ref(fn *ssa.Function) evidence.SymbolRef {
	fn = origin(fn)
	s := p.symbol(fn)
	_, _, end := p.position(fn)
	return evidence.SymbolRef{ID: s.ID, Name: s.Name, Qualified: s.Qualified, Pkg: s.Pkg, Recv: s.Recv,
		Signature: s.Signature, File: s.File, Line: s.Line, EndLine: end, Module: p.Module, Version: shortVersion(p.Version)}
}

func shortVersion(v string) string {
	if len(v) > 12 {
		return v[:12]
	}
	return v
}

// ShortName renders fn like Go stack traces (pkg.(*T).Method, pkg.F.func1).
func ShortName(fn *ssa.Function) string { return shortName(fn) }

// Signature renders fn's declaration signature.
func Signature(fn *ssa.Function) string { return signature(fn) }

// Origin maps generic instantiations to their declaration.
func Origin(fn *ssa.Function) *ssa.Function { return origin(fn) }

// Unwrap follows synthetic wrappers to the declared function they call.
func Unwrap(fn *ssa.Function) *ssa.Function { return unwrap(fn) }

// FuncValue returns the function a value statically denotes, if any.
func FuncValue(v ssa.Value) *ssa.Function { return funcValue(v) }

// DBOp describes a recognised database operation at a call site.
type DBOp struct {
	Lib      string // pgx, database/sql, sqlx
	Op       string // normalised operation: Query, QueryRow, Exec, Begin...
	SQL      string // literal SQL, when the argument is a constant
	Dynamic  bool   // the SQL argument is computed at runtime
	ImplType string // driver type implementing a module interface, if dispatch is indirect
}

// Callee is one possible target of a call site.
type Callee struct {
	Fn      *ssa.Function // nil for unresolved calls and external interface methods
	Conf    string        // confirmed, likely, possible, unresolved
	Note    string
	ExtName string // external interface method with no implementation in the module
	ExtPkg  string
	DB      *DBOp
}

// CallSite is a call (or a function value handed to a call) in a function.
type CallSite struct {
	Instr    ssa.CallInstruction
	Kind     string // call | go | defer
	File     string
	Line     int
	Targets  []Callee
	Callback bool // the target is a function value passed as an argument
}

func exportTargets(ts []target) []Callee {
	out := make([]Callee, 0, len(ts))
	for _, t := range ts {
		c := Callee{Fn: t.fn, Conf: t.conf, Note: t.note, ExtName: t.extName, ExtPkg: t.extPkg}
		if t.db != nil {
			c.DB = &DBOp{Lib: t.db.lib, Op: t.db.op, SQL: t.db.sql, Dynamic: t.db.dynamic, ImplType: t.db.implType}
		}
		out = append(out, c)
	}
	return out
}

// CallSites lists the call sites of fn resolved when the module was loaded.
func (p *Program) CallSites(fn *ssa.Function) []CallSite {
	var out []CallSite
	for _, cs := range p.calls[fn] {
		out = append(out, CallSite{Instr: cs.instr, Kind: cs.kind, File: cs.file, Line: cs.pos, Targets: exportTargets(cs.targets), Callback: cs.callback})
	}
	return out
}

// CalleesOf resolves the targets of one call instruction.
func (p *Program) CalleesOf(ci ssa.CallInstruction) []Callee {
	for _, cs := range p.calls[ci.Parent()] {
		if cs.instr == ci && !cs.callback {
			return exportTargets(cs.targets)
		}
	}
	if cs, ok := p.callSiteOf(ci); ok {
		return exportTargets(cs.targets)
	}
	return nil
}

// CallerSite is a call site that may invoke a function.
type CallerSite struct {
	Caller   *ssa.Function
	Instr    ssa.CallInstruction
	File     string
	Line     int
	Conf     string
	Callback bool // the function is passed as a value; arguments are bound elsewhere
}

// CallersOf lists every known call site of fn in the module.
func (p *Program) CallersOf(fn *ssa.Function) []CallerSite {
	var out []CallerSite
	for _, r := range p.callers[origin(fn)] {
		out = append(out, CallerSite{Caller: r.caller, Instr: r.instr, File: r.file, Line: r.line, Conf: r.conf, Callback: r.callback})
	}
	return out
}

// Implementations lists module methods implementing an interface method.
func (p *Program) Implementations(iface types.Type, m *types.Func) []*ssa.Function {
	return p.implementations(iface, m)
}

// normFile maps a path found in a profile, a trace or compiler output to a
// repository-relative file: absolute paths, paths relative to the module
// directory, and -trimpath paths prefixed with the module path.
func (p *Program) normFile(file string) string {
	file = filepath.ToSlash(file)
	if filepath.IsAbs(filepath.FromSlash(file)) {
		return p.relFile(filepath.FromSlash(file))
	}
	if p.Module != "" && strings.HasPrefix(file, p.Module+"/") {
		return p.relFile(filepath.Join(p.Dir, strings.TrimPrefix(file, p.Module+"/")))
	}
	return p.relFile(filepath.Join(p.Dir, strings.TrimPrefix(file, "./")))
}

// NormFile is the exported form of normFile.
func (p *Program) NormFile(file string) string { return p.normFile(file) }

func (p *Program) buildPosIndex() {
	p.posIndex = map[string][]*ssa.Function{}
	for _, fn := range p.funcs {
		file, _, _ := p.position(fn)
		if file != "" {
			p.posIndex[file] = append(p.posIndex[file], fn)
		}
	}
}

// FuncAt returns the innermost module function (closures included) whose
// body contains file:line. file may be absolute, repository-relative or
// module-path qualified.
func (p *Program) FuncAt(file string, line int) *ssa.Function {
	p.posOnce.Do(p.buildPosIndex)
	rel := file
	if _, known := p.posIndex[file]; !known {
		rel = p.normFile(file)
	}
	var best *ssa.Function
	bestLine := -1
	for _, fn := range p.posIndex[rel] {
		_, l, e := p.position(fn)
		if line >= l && line <= e && l >= bestLine {
			best, bestLine = fn, l
		}
	}
	return best
}

// RuntimeName returns the name the Go runtime gives fn in stack traces and
// profiles: pkg.F, pkg.(*T).M, pkg.T.M, pkg.F.func1, pkg.F.func1.1.
func RuntimeName(fn *ssa.Function) string {
	fn = origin(fn)
	if par := fn.Parent(); par != nil {
		idx := 1
		for i, a := range par.AnonFuncs {
			if a == fn {
				idx = i + 1
			}
		}
		if par.Parent() != nil {
			return RuntimeName(par) + "." + strconv.Itoa(idx)
		}
		return RuntimeName(par) + ".func" + strconv.Itoa(idx)
	}
	pkg := pkgPath(fn)
	if recv := fn.Signature.Recv(); recv != nil {
		t := recv.Type()
		ptr := false
		if pt, ok := t.(*types.Pointer); ok {
			t, ptr = pt.Elem(), true
		}
		name := types.TypeString(t, func(*types.Package) string { return "" })
		if i := strings.IndexByte(name, '['); i >= 0 {
			name = name[:i]
		}
		if ptr {
			return pkg + ".(*" + name + ")." + fn.Name()
		}
		return pkg + "." + name + "." + fn.Name()
	}
	return pkg + "." + fn.Name()
}

// NormalizeRuntimeName strips generic shape arguments ([...] or
// [go.shape.int]) so runtime names compare with RuntimeName.
func NormalizeRuntimeName(name string) string {
	var b strings.Builder
	depth := 0
	for _, r := range name {
		switch {
		case r == '[':
			depth++
		case r == ']' && depth > 0:
			depth--
		case depth == 0:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// FuncForFrame maps a runtime frame (from a profile or a trace) to a module
// function: by position first (exact for inlined code and closures), then
// by runtime name. It returns nil for code outside the module.
func (p *Program) FuncForFrame(funcName, file string, line int) *ssa.Function {
	if file != "" && line > 0 {
		if fn := p.FuncAt(file, line); fn != nil {
			return fn
		}
	}
	name := NormalizeRuntimeName(funcName)
	if name == "" {
		return nil
	}
	p.posOnce.Do(p.buildPosIndex)
	for _, fn := range p.funcs {
		if RuntimeName(fn) == name {
			return fn
		}
	}
	return nil
}

// SyntaxOf returns the declaration (FuncDecl or FuncLit) of fn, if any.
func SyntaxOf(fn *ssa.Function) (body *ast.BlockStmt, typ *ast.FuncType) {
	switch s := origin(fn).Syntax().(type) {
	case *ast.FuncDecl:
		return s.Body, s.Type
	case *ast.FuncLit:
		return s.Body, s.Type
	}
	return nil, nil
}

// FileOf returns the parsed file containing pos in fn's package.
func (p *Program) FileOf(fn *ssa.Function, pos token.Pos) *ast.File {
	pk := p.Package(fn)
	if pk == nil {
		return nil
	}
	for _, f := range pk.Syntax {
		if f.FileStart <= pos && pos <= f.FileEnd {
			return f
		}
	}
	return nil
}

// SortFuncs orders functions by qualified name, for stable output.
func SortFuncs(fns []*ssa.Function) {
	sort.Slice(fns, func(i, j int) bool { return fns[i].String() < fns[j].String() })
}

// Text returns the exact source between two positions of the same file.
func (p *Program) Text(pos, end token.Pos) string {
	if !pos.IsValid() || !end.IsValid() || end < pos {
		return ""
	}
	tf := p.fset.File(pos)
	if tf == nil || tf != p.fset.File(end) {
		return ""
	}
	p.srcMu.Lock()
	data, ok := p.textCache[tf.Name()]
	if !ok {
		if b, err := os.ReadFile(tf.Name()); err == nil {
			data = string(b)
		}
		if p.textCache == nil {
			p.textCache = map[string]string{}
		}
		p.textCache[tf.Name()] = data
	}
	p.srcMu.Unlock()
	from, to := tf.Offset(pos), tf.Offset(end)
	if to > len(data) || from > to {
		return ""
	}
	return data[from:to]
}

// NodeText returns the source of an AST node.
func (p *Program) NodeText(n ast.Node) string {
	if n == nil {
		return ""
	}
	return p.Text(n.Pos(), n.End())
}

// DBNodeID is the Code Flow node ID of a database operation, shared with the
// SQL Explorer so both refer to the same call.
func DBNodeID(file string, line int, op string) string {
	return fmt.Sprintf("db:%s:%d:%s", file, line, op)
}

// CallExprAt returns the call expression of fn whose opening parenthesis is
// at pos (the position SSA records for calls).
func (p *Program) CallExprAt(fn *ssa.Function, pos token.Pos) *ast.CallExpr {
	f := p.FileOf(fn, pos)
	if f == nil || !pos.IsValid() {
		return nil
	}
	var found *ast.CallExpr
	ast.Inspect(f, func(n ast.Node) bool {
		if found != nil || n == nil || pos < n.Pos() || pos >= n.End() {
			return false
		}
		if c, ok := n.(*ast.CallExpr); ok && c.Lparen == pos {
			found = c
			return false
		}
		return true
	})
	return found
}

// VariadicElems expands the slice SSA builds for a variadic call into its
// elements, in order. ok is false when the slice is not a literal list
// (for example args... passed through).
func VariadicElems(v ssa.Value) (elems []ssa.Value, ok bool) {
	if k, isConst := v.(*ssa.Const); isConst && k.IsNil() {
		return nil, true
	}
	out := variadic(v)
	if len(out) == 1 && out[0] == v {
		return nil, false
	}
	return out, true
}

// DisplayPath shortens paths outside the repository so reports do not
// expose the local machine: the Go installation becomes $GOROOT, the module
// cache $GOMODCACHE and the home directory ~.
func DisplayPath(path string) string {
	if path == "" || !filepath.IsAbs(path) {
		return path
	}
	type prefix struct{ dir, name string }
	var prefixes []prefix
	if gr := build.Default.GOROOT; gr != "" {
		prefixes = append(prefixes, prefix{gr, "$GOROOT"})
	}
	mc := os.Getenv("GOMODCACHE")
	if mc == "" && build.Default.GOPATH != "" {
		mc = filepath.Join(strings.Split(build.Default.GOPATH, string(filepath.ListSeparator))[0], "pkg", "mod")
	}
	if mc != "" {
		prefixes = append(prefixes, prefix{mc, "$GOMODCACHE"})
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		prefixes = append(prefixes, prefix{home, "~"})
	}
	for _, pr := range prefixes {
		if rel, err := filepath.Rel(pr.dir, path); err == nil && !strings.HasPrefix(rel, "..") {
			return pr.name + "/" + filepath.ToSlash(rel)
		}
	}
	return path
}

// RouteInfo is an HTTP route registration found in the code.
type RouteInfo struct {
	Router  string // gin | echo | chi | net/http
	Method  string // "" when any method; "?" when computed at runtime
	Path    string // pattern as registered, group prefixes included
	File    string
	Line    int
	Partial bool
	// Handler is the final handler; Middleware runs before it, in order.
	Handler     *ssa.Function
	HandlerConf string
	Middleware  []*ssa.Function
}

// Routes returns the HTTP routes registered in the program.
func (p *Program) Routes() []RouteInfo {
	out := make([]RouteInfo, 0, len(p.routes))
	for _, r := range p.routes {
		ri := RouteInfo{Router: r.router, Method: r.method, Path: r.path, File: r.file, Line: r.line, Partial: r.partial}
		for _, h := range r.handlers {
			if h.middleware {
				ri.Middleware = append(ri.Middleware, h.fn)
			} else {
				ri.Handler, ri.HandlerConf = h.fn, h.conf
			}
		}
		out = append(out, ri)
	}
	return out
}

// PackageInfo describes a package of the module.
type PackageInfo struct {
	Path    string   // import path
	Name    string   // package name
	Dir     string   // repository-relative directory
	Files   []string // repository-relative Go files (tests excluded)
	Imports []string // import paths of other module packages it imports
}

// Packages lists the module's packages, sorted by path.
func (p *Program) Packages() []PackageInfo {
	var out []PackageInfo
	for tp, pkg := range p.pkgs {
		pi := PackageInfo{Path: tp.Path(), Name: tp.Name()}
		for _, f := range pkg.GoFiles {
			pi.Files = append(pi.Files, p.relFile(f))
		}
		sort.Strings(pi.Files)
		if len(pi.Files) > 0 {
			pi.Dir = filepath.ToSlash(filepath.Dir(pi.Files[0]))
		}
		for path := range pkg.Imports {
			if p.Module != "" && (path == p.Module || strings.HasPrefix(path, p.Module+"/")) {
				pi.Imports = append(pi.Imports, path)
			}
		}
		sort.Strings(pi.Imports)
		out = append(out, pi)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// TypedPackages returns the type-checked syntax of every module package,
// sorted by import path, for analyses that need declarations other than
// functions (types, imports, embedding). The packages must not be modified.
func (p *Program) TypedPackages() []*packages.Package {
	out := make([]*packages.Package, 0, len(p.pkgs))
	for _, pk := range p.pkgs {
		out = append(out, pk)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PkgPath < out[j].PkgPath })
	return out
}
