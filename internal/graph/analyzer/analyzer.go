// Package analyzer turns the programs analysed by Code Flow into Repository
// Graph nodes and edges. It does not load or analyse code itself: packages,
// files and imports come from go/packages, types and their relations from
// go/types, and functions, calls, routes and database operations from the
// Code Flow index (SSA). Every edge records where it was found.
package analyzer

import (
	"fmt"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strings"

	"github.com/athos/gitview/internal/evidence"
	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/graph/model"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
)

// Node ID prefixes. Code Flow IDs of routes, database operations, external
// functions and unresolved calls ("route:", "db:", "ext:", "unresolved:")
// are kept unchanged so both views name the same things.
const (
	prefixModule  = "module:"
	prefixPackage = "package:"
	prefixFile    = "file:"
	prefixType    = "type:"
	prefixFunc    = "func:"
)

func ModuleID(path string) string  { return prefixModule + path }
func PackageID(path string) string { return prefixPackage + path }
func FileID(rel string) string     { return prefixFile + rel }
func TypeID(pkg, name string) string {
	return prefixType + pkg + "." + name
}

// FuncID maps a Code Flow function ID (the SSA name) to a graph node ID.
func FuncID(flowID string) string { return prefixFunc + flowID }

// Result is the raw output of the analysis, before edges are merged.
type Result struct {
	Nodes    map[string]*model.Node
	Edges    []*model.Edge
	Modules  []string
	Versions []string
	Limits   []string
	// Flow holds the Code Flow index of each program, for analyses that
	// work on it (layer classification).
	Flow []*flow.Graph
}

type analysis struct {
	res     *Result
	filePkg map[string]string // repository-relative file → package node ID
	inMod   map[string]bool   // import paths of analysed packages
}

// Analyze describes the given programs. Programs from different modules of
// the same repository may be passed together; type relations (implements)
// are only computed within a module, since each module is type-checked on
// its own.
func Analyze(progs []*flow.Program) *Result {
	a := &analysis{
		res:     &Result{Nodes: map[string]*model.Node{}},
		filePkg: map[string]string{},
		inMod:   map[string]bool{},
	}
	for _, p := range progs {
		for _, pk := range p.TypedPackages() {
			a.inMod[pk.PkgPath] = true
		}
	}
	for _, p := range progs {
		a.program(p)
	}
	sort.Strings(a.res.Modules)
	return a.res
}

func (a *analysis) node(n *model.Node) *model.Node {
	if old, ok := a.res.Nodes[n.ID]; ok {
		return old
	}
	n.Community = -1
	a.res.Nodes[n.ID] = n
	return n
}

func (a *analysis) edge(typ, src, dst, conf string, ev *evidence.Evidence, note string, meta map[string]any) {
	a.res.Edges = append(a.res.Edges, &model.Edge{Source: src, Target: dst, Type: typ, Confidence: conf, Evidence: ev, Note: note, Metadata: meta})
}

func staticEv(source, file string, line int) *evidence.Evidence {
	if file == "" {
		return &evidence.Evidence{Kind: evidence.Static, Basis: evidence.BasisStatic, Source: source}
	}
	return &evidence.Evidence{Kind: evidence.Static, Basis: evidence.BasisStatic, Source: source, File: file, Line: line}
}

func (a *analysis) program(p *flow.Program) {
	mod := ModuleID(p.Module)
	a.res.Modules = append(a.res.Modules, p.Module)
	a.res.Versions = append(a.res.Versions, p.Version)
	for _, e := range p.Errors {
		a.res.Limits = append(a.res.Limits, p.Module+": "+e)
	}
	dir := filepath.ToSlash(p.RelFile(p.Dir))
	if dir == p.Dir || dir == "" {
		dir = "."
	}
	a.node(&model.Node{ID: mod, Type: model.KindModule, Name: p.Module, Module: p.Module, File: dir})

	pkgs := p.TypedPackages()
	for _, pk := range pkgs {
		a.pkg(p, pk)
	}
	a.implements(p, pkgs)
	a.funcs(p)
	a.index(p)
}

func isStd(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

func (a *analysis) pos(p *flow.Program, pos token.Pos) (string, int) {
	f, l, _ := p.Pos(pos)
	return f, l
}

func (a *analysis) pkg(p *flow.Program, pk *packages.Package) {
	id := PackageID(pk.PkgPath)
	n := a.node(&model.Node{ID: id, Type: model.KindPackage, Name: pk.PkgPath, Qualified: pk.Name, Package: pk.PkgPath,
		Module: p.Module, Parent: ModuleID(p.Module), Group: id})
	n.Metadata = map[string]any{"name": pk.Name, "files": len(pk.GoFiles)}
	if len(pk.GoFiles) > 0 {
		n.File = filepath.ToSlash(filepath.Dir(p.RelFile(pk.GoFiles[0])))
	}
	a.edge(model.EdgeContains, ModuleID(p.Module), id, model.Confirmed, staticEv("go/packages", "", 0), "", nil)

	for _, f := range pk.GoFiles {
		rel := p.RelFile(f)
		fid := FileID(rel)
		a.filePkg[rel] = id
		a.node(&model.Node{ID: fid, Type: model.KindFile, Name: filepath.Base(rel), Package: pk.PkgPath, Module: p.Module,
			File: rel, Parent: id, Group: id})
		a.edge(model.EdgeContains, id, fid, model.Confirmed, staticEv("go/packages", rel, 0), "", nil)
	}

	// Imports, with the first import declaration as evidence and the number
	// of files importing as weight.
	type imp struct {
		file  string
		line  int
		files int
	}
	imps := map[string]*imp{}
	for _, f := range pk.Syntax {
		for _, spec := range f.Imports {
			path := strings.Trim(spec.Path.Value, "\"`")
			file, line := a.pos(p, spec.Pos())
			if x := imps[path]; x != nil {
				x.files++
				continue
			}
			imps[path] = &imp{file, line, 1}
		}
	}
	paths := make([]string, 0, len(imps))
	for path := range imps {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if path == "C" { // cgo pseudo-package
			continue
		}
		x := imps[path]
		target := PackageID(path)
		if !a.inMod[path] {
			a.externalPkg(path)
		}
		a.edge(model.EdgeImports, id, target, model.Confirmed, staticEv("go/packages", x.file, x.line), "",
			map[string]any{"files": x.files})
	}

	scope := pk.Types.Scope()
	for _, name := range scope.Names() {
		tn, ok := scope.Lookup(name).(*types.TypeName)
		if !ok {
			continue
		}
		a.typeDecl(p, pk, tn)
	}
}

func (a *analysis) externalPkg(path string) {
	id := PackageID(path)
	n := a.node(&model.Node{ID: id, Type: model.KindPackage, Name: path, Package: path, External: true, Group: id})
	if n.Metadata == nil {
		n.Metadata = map[string]any{"std": isStd(path)}
	}
}

func kindOfType(t types.Type) string {
	switch t.Underlying().(type) {
	case *types.Struct:
		return model.KindStruct
	case *types.Interface:
		return model.KindInterface
	}
	return model.KindType
}

func (a *analysis) typeDecl(p *flow.Program, pk *packages.Package, tn *types.TypeName) {
	file, line := a.pos(p, tn.Pos())
	id := TypeID(pk.PkgPath, tn.Name())
	kind := kindOfType(tn.Type())
	meta := map[string]any{"exported": tn.Exported()}
	if tn.IsAlias() {
		meta["alias"] = true
		kind = model.KindType
	}
	named, _ := tn.Type().(*types.Named)
	if named != nil && named.TypeParams().Len() > 0 {
		meta["typeParams"] = named.TypeParams().Len()
	}
	n := a.node(&model.Node{ID: id, Type: kind, Name: tn.Name(), Qualified: pk.Name + "." + tn.Name(), Package: pk.PkgPath,
		Module: p.Module, File: file, Line: line, Parent: FileID(file), Group: PackageID(pk.PkgPath),
		Signature: "type " + tn.Name() + " " + types.TypeString(tn.Type().Underlying(), qualifier(pk.Types))})
	n.Metadata = meta
	a.edge(model.EdgeContains, FileID(file), id, model.Confirmed, staticEv("go/types", file, line), "", nil)
	if tn.IsAlias() {
		return
	}

	switch u := tn.Type().Underlying().(type) {
	case *types.Struct:
		meta["fields"] = u.NumFields()
		for i := 0; i < u.NumFields(); i++ {
			f := u.Field(i)
			ff, fl := a.pos(p, f.Pos())
			if f.Embedded() {
				if t := a.moduleNamed(deref(f.Type())); t != "" && t != id {
					a.edge(model.EdgeEmbeds, id, t, model.Confirmed, staticEv("go/types", ff, fl), "", nil)
					continue
				}
			}
			for _, t := range a.namedIn(f.Type()) {
				if t != id {
					a.edge(model.EdgeReferences, id, t, model.Confirmed, staticEv("go/types", ff, fl), "", map[string]any{"role": "field", "field": f.Name()})
				}
			}
		}
	case *types.Interface:
		meta["methods"] = u.NumExplicitMethods()
		for i := 0; i < u.NumEmbeddeds(); i++ {
			if t := a.moduleNamed(u.EmbeddedType(i)); t != "" && t != id {
				a.edge(model.EdgeEmbeds, id, t, model.Confirmed, staticEv("go/types", file, line), "", nil)
			}
		}
		for i := 0; i < u.NumExplicitMethods(); i++ {
			m := u.ExplicitMethod(i)
			mf, ml := a.pos(p, m.Pos())
			for _, t := range a.namedIn(m.Type()) {
				if t != id {
					a.edge(model.EdgeReferences, id, t, model.Confirmed, staticEv("go/types", mf, ml), "", map[string]any{"role": "method", "method": m.Name()})
				}
			}
		}
	default:
		for _, t := range a.namedIn(u) {
			if t != id {
				a.edge(model.EdgeReferences, id, t, model.Confirmed, staticEv("go/types", file, line), "", map[string]any{"role": "underlying"})
			}
		}
	}
}

func qualifier(self *types.Package) types.Qualifier {
	return func(p *types.Package) string {
		if p == self {
			return ""
		}
		return p.Name()
	}
}

func deref(t types.Type) types.Type {
	if p, ok := t.(*types.Pointer); ok {
		return p.Elem()
	}
	return t
}

// moduleNamed returns the node ID of t when it is a named type declared in
// an analysed package.
func (a *analysis) moduleNamed(t types.Type) string {
	var obj *types.TypeName
	switch t := types.Unalias(t).(type) {
	case *types.Named:
		obj = t.Origin().Obj()
	case *types.Alias:
		obj = t.Obj()
	default:
		return ""
	}
	if obj.Pkg() == nil || !a.inMod[obj.Pkg().Path()] || obj.Parent() != obj.Pkg().Scope() {
		return ""
	}
	return TypeID(obj.Pkg().Path(), obj.Name())
}

// namedIn lists the module named types a type expression mentions (through
// pointers, slices, maps, channels, function signatures and type arguments),
// sorted and without duplicates.
func (a *analysis) namedIn(t types.Type) []string {
	seen := map[string]bool{}
	var walk func(t types.Type, depth int)
	walk = func(t types.Type, depth int) {
		if t == nil || depth > 8 {
			return
		}
		switch x := t.(type) {
		case *types.Named:
			if id := a.moduleNamed(x); id != "" {
				seen[id] = true
			}
			if args := x.TypeArgs(); args != nil {
				for i := 0; i < args.Len(); i++ {
					walk(args.At(i), depth+1)
				}
			}
		case *types.Alias:
			if id := a.moduleNamed(x); id != "" {
				seen[id] = true
			}
		case *types.Pointer:
			walk(x.Elem(), depth+1)
		case *types.Slice:
			walk(x.Elem(), depth+1)
		case *types.Array:
			walk(x.Elem(), depth+1)
		case *types.Map:
			walk(x.Key(), depth+1)
			walk(x.Elem(), depth+1)
		case *types.Chan:
			walk(x.Elem(), depth+1)
		case *types.Signature:
			for _, tu := range []*types.Tuple{x.Params(), x.Results()} {
				for i := 0; i < tu.Len(); i++ {
					walk(tu.At(i).Type(), depth+1)
				}
			}
		}
	}
	walk(t, 0)
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// implements links each concrete named type of the module to the module
// interfaces its value or pointer method set satisfies (types.Implements).
// Empty interfaces are skipped: every type satisfies them.
func (a *analysis) implements(p *flow.Program, pkgs []*packages.Package) {
	type decl struct {
		id   string
		obj  *types.TypeName
		file string
		line int
	}
	var concrete, ifaces []decl
	for _, pk := range pkgs {
		scope := pk.Types.Scope()
		for _, name := range scope.Names() {
			tn, ok := scope.Lookup(name).(*types.TypeName)
			if !ok || tn.IsAlias() {
				continue
			}
			named, ok := tn.Type().(*types.Named)
			if !ok || named.TypeParams().Len() > 0 {
				continue
			}
			file, line := a.pos(p, tn.Pos())
			d := decl{TypeID(pk.PkgPath, tn.Name()), tn, file, line}
			if it, ok := named.Underlying().(*types.Interface); ok {
				if it.NumMethods() > 0 {
					ifaces = append(ifaces, d)
				}
				continue
			}
			concrete = append(concrete, d)
		}
	}
	for _, c := range concrete {
		for _, i := range ifaces {
			it := i.obj.Type().Underlying().(*types.Interface)
			switch {
			case types.Implements(c.obj.Type(), it):
				a.edge(model.EdgeImplements, c.id, i.id, model.Confirmed, staticEv("go/types", c.file, c.line), "",
					map[string]any{"receiver": "value"})
			case types.Implements(types.NewPointer(c.obj.Type()), it):
				a.edge(model.EdgeImplements, c.id, i.id, model.Confirmed, staticEv("go/types", c.file, c.line), "pointer receiver",
					map[string]any{"receiver": "pointer"})
			}
		}
	}
}

// funcs adds every module function, method and closure.
func (a *analysis) funcs(p *flow.Program) {
	for _, fn := range p.Funcs() {
		a.funcNode(p, fn)
	}
}

func (a *analysis) funcNode(p *flow.Program, fn *ssa.Function) *model.Node {
	fn = flow.Origin(fn)
	id := FuncID(fn.String())
	if n, ok := a.res.Nodes[id]; ok {
		return n
	}
	sym := p.SymbolOf(fn)
	file, line, end := p.Range(fn)
	kind := model.KindFunc
	name := sym.Qualified
	if i := strings.IndexByte(name, '.'); i >= 0 && fn.Parent() == nil {
		name = name[i+1:] // drop the package name: F, (*T).M
	} else if fn.Parent() != nil {
		name = strings.TrimPrefix(name, fn.Pkg.Pkg.Name()+".")
	}
	parent := FileID(file)
	meta := map[string]any{"exported": token.IsExported(fn.Name()), "flowId": fn.String()}
	switch {
	case fn.Parent() != nil:
		kind = model.KindClosure
		parent = FuncID(flow.Origin(fn.Parent()).String())
		meta["exported"] = false
	case fn.Signature.Recv() != nil:
		kind = model.KindMethod
		meta["receiver"] = sym.Recv
		_, ptr := fn.Signature.Recv().Type().(*types.Pointer)
		meta["pointerReceiver"] = ptr
	}
	pkg := ""
	if fn.Pkg != nil {
		pkg = fn.Pkg.Pkg.Path()
	}
	n := a.node(&model.Node{ID: id, Type: kind, Name: name, Qualified: sym.Qualified, Package: pkg, Module: p.Module,
		File: file, Line: line, EndLine: end, Signature: sym.Signature, Parent: parent, Group: PackageID(pkg)})
	n.Metadata = meta
	a.edge(model.EdgeContains, parent, id, model.Confirmed, staticEv("go/ssa", file, line), "", nil)

	if recv := fn.Signature.Recv(); recv != nil && fn.Parent() == nil {
		if t := a.moduleNamed(deref(recv.Type())); t != "" {
			a.edge(model.EdgeMethod, t, id, model.Confirmed, staticEv("go/types", file, line), "", nil)
		}
	}
	if fn.Parent() == nil {
		sig := fn.Signature
		for role, tu := range map[string]*types.Tuple{"param": sig.Params(), "result": sig.Results()} {
			for i := 0; i < tu.Len(); i++ {
				v := tu.At(i)
				vf, vl := a.pos(p, v.Pos())
				if vf == "" {
					vf, vl = file, line
				}
				for _, t := range a.namedIn(v.Type()) {
					a.edge(model.EdgeReferences, id, t, model.Confirmed, staticEv("go/types", vf, vl), "", map[string]any{"role": role})
				}
			}
		}
	}
	return n
}

// index converts the Code Flow index (calls, routes, database operations,
// external and unresolved targets) into graph nodes and edges.
func (a *analysis) index(p *flow.Program) {
	g := p.Index(flow.Options{External: true, MaxNodes: 1 << 30})
	a.res.Flow = append(a.res.Flow, g)
	if g.Truncated {
		a.res.Limits = append(a.res.Limits, p.Module+": the call index hit its size limit")
	}
	ids := map[string]string{} // Code Flow ID → graph ID
	for _, fn := range g.Nodes {
		ids[fn.ID] = a.flowNode(p, fn)
	}
	for _, e := range g.Edges {
		src, dst := ids[e.From], ids[e.To]
		if src == "" || dst == "" {
			continue
		}
		typ := model.EdgeCalls
		switch {
		case e.Kind == flow.EdgeHandler:
			typ = model.EdgeHandles
		case strings.HasPrefix(e.To, "db:"):
			typ = model.EdgeQueries
		case e.Kind == flow.EdgeGo:
			typ = model.EdgeGo
		case e.Kind == flow.EdgeDefer:
			typ = model.EdgeDefer
		}
		var meta map[string]any
		if e.Order > 0 {
			meta = map[string]any{"order": e.Order}
		}
		a.edge(typ, src, dst, e.Confidence, staticEv("go/ssa", e.File, e.Line), e.Note, meta)
	}
}

func (a *analysis) groupOfFile(file string) string { return a.filePkg[file] }

func (a *analysis) flowNode(p *flow.Program, n *flow.Node) string {
	switch n.Kind {
	case flow.KindFunc, flow.KindClosure:
		if fn := p.FuncByID(n.ID); fn != nil {
			x := a.funcNode(p, fn)
			for _, t := range n.Tags {
				if t == "http handler" || t == "middleware" {
					x.Metadata["role"] = t
				}
			}
			return x.ID
		}
		return ""
	case flow.KindRoute:
		x := a.node(&model.Node{ID: n.ID, Type: model.KindRoute, Name: n.Name, Module: p.Module, File: n.File, Line: n.Line,
			Parent: FileID(n.File), Group: a.groupOfFile(n.File)})
		x.Package = strings.TrimPrefix(x.Group, prefixPackage)
		x.Metadata = map[string]any{"method": n.Method, "path": n.Path, "router": n.Router}
		if n.Partial {
			x.Metadata["partial"] = true
		}
		if n.Note != "" {
			x.Metadata["note"] = n.Note
		}
		a.edge(model.EdgeContains, x.Parent, x.ID, model.Confirmed, staticEv("go/ssa", n.File, n.Line), "", nil)
		return x.ID
	case flow.KindDB:
		x := a.node(&model.Node{ID: n.ID, Type: model.KindDB, Name: n.Name, Module: p.Module, File: n.File, Line: n.Line,
			Parent: FileID(n.File), Group: a.groupOfFile(n.File), Signature: n.Signature})
		x.Package = strings.TrimPrefix(x.Group, prefixPackage)
		x.Metadata = map[string]any{"op": n.DBOp, "lib": n.DBLib}
		if n.SQL != "" {
			x.Metadata["sql"] = n.SQL
		}
		if n.SQLDynamic {
			x.Metadata["sqlDynamic"] = true
		}
		if n.Note != "" {
			x.Metadata["note"] = n.Note
		}
		return x.ID
	case flow.KindExternal:
		pkg := n.Pkg
		if !a.inMod[pkg] && pkg != "" {
			a.externalPkg(pkg)
		}
		parent := ""
		if pkg != "" {
			parent = PackageID(pkg)
		}
		x := a.node(&model.Node{ID: n.ID, Type: model.KindExternal, Name: n.Name, Package: pkg, Signature: n.Signature,
			External: true, Parent: parent, Group: parent})
		if parent != "" {
			a.edge(model.EdgeContains, parent, x.ID, model.Confirmed, staticEv("go/ssa", "", 0), "", nil)
		}
		return x.ID
	case flow.KindUnresolved:
		x := a.node(&model.Node{ID: n.ID, Type: model.KindUnresolved, Name: n.Name, Module: p.Module, File: n.File, Line: n.Line,
			Parent: FileID(n.File), Group: a.groupOfFile(n.File)})
		x.Package = strings.TrimPrefix(x.Group, prefixPackage)
		if n.Note != "" {
			x.Metadata = map[string]any{"note": n.Note}
		}
		return x.ID
	}
	return ""
}

// String is used in test failures.
func (r *Result) String() string {
	return fmt.Sprintf("%d nodes, %d edges", len(r.Nodes), len(r.Edges))
}
