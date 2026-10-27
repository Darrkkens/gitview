package hover

import (
	"bytes"
	"go/ast"
	"go/printer"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"golang.org/x/tools/go/ast/astutil"

	"github.com/athos/gitview/internal/flow"
)

// describe builds the hover of obj seen from package from.
func (r *Resolver) describe(prog *flow.Program, obj types.Object, from *types.Package) *Info {
	qf := func(p *types.Package) string {
		if p == from {
			return ""
		}
		return p.Name()
	}
	in := &Info{Name: obj.Name()}
	if p := obj.Pkg(); p != nil {
		in.Pkg = p.Path()
	}
	// qualified is name with its package's name when it is declared at
	// package level elsewhere: strings.TrimSpace.
	qualified := obj.Name()
	if p := obj.Pkg(); p != nil && p != from && obj.Parent() == p.Scope() {
		qualified = p.Name() + "." + obj.Name()
	}
	switch o := obj.(type) {
	case *types.Func:
		sig, _ := o.Type().(*types.Signature)
		var b bytes.Buffer
		b.WriteString("func ")
		if sig != nil && sig.Recv() != nil {
			in.Kind = "method"
			b.WriteString("(")
			if n := sig.Recv().Name(); n != "" && n != "_" {
				b.WriteString(n + " ")
			}
			b.WriteString(types.TypeString(sig.Recv().Type(), qf))
			b.WriteString(") " + o.Name())
		} else {
			in.Kind = "func"
			b.WriteString(qualified)
		}
		if sig != nil {
			types.WriteSignature(&b, sig, qf)
		}
		in.Signature = b.String()
	case *types.TypeName:
		in.Kind = "type"
		switch {
		case o.IsAlias():
			in.Signature = "type " + qualified + " = " + types.TypeString(o.Type(), qf)
		case o.Pkg() == nil:
			in.Signature = "type " + o.Name() // predeclared: string, error, any…
		default:
			u := o.Type().Underlying()
			switch u.(type) {
			case *types.Struct:
				in.Signature = "type " + qualified + " struct{…}"
			case *types.Interface:
				in.Signature = "type " + qualified + " interface{…}"
			default:
				in.Signature = "type " + qualified + " " + types.TypeString(u, qf)
			}
		}
	case *types.Var:
		t := types.TypeString(o.Type(), qf)
		if o.IsField() {
			in.Kind = "field"
			in.Signature = "field " + o.Name() + " " + t
		} else {
			in.Kind = "var"
			in.Signature = "var " + qualified + " " + t
		}
	case *types.Const:
		in.Kind = "const"
		in.Signature = "const " + qualified + " " + types.TypeString(o.Type(), qf)
		if v := o.Val(); v != nil {
			s := v.ExactString()
			if len(s) > 80 {
				s = s[:77] + "…"
			}
			in.Signature += " = " + s
		}
	case *types.PkgName:
		in.Kind = "package"
		in.Pkg = o.Imported().Path()
		in.Signature = "package " + o.Imported().Name() + " (" + strings.TrimSpace(`"`+o.Imported().Path()+`"`) + ")"
		return in
	case *types.Builtin:
		in.Kind = "builtin"
		in.Signature = "func " + o.Name()
	case *types.Nil:
		in.Kind = "builtin"
		in.Signature = "var nil Type"
	case *types.Label:
		in.Kind = "label"
		in.Signature = "label " + o.Name()
		return in
	default:
		return nil
	}
	r.declaration(prog, obj, in)
	return in
}

// declaration fills the documentation, the location and, for struct and
// interface types, the source of the declaration.
func (r *Resolver) declaration(prog *flow.Program, obj types.Object, in *Info) {
	var (
		fset *token.FileSet
		f    *ast.File
		pos  token.Pos
	)
	switch {
	case obj.Pkg() == nil || obj.Parent() == types.Universe:
		// Predeclared: documented in $GOROOT/src/builtin/builtin.go.
		bf, bfset, path := r.src.builtin()
		if bf == nil {
			return
		}
		fset, f = bfset, bf
		pos = findDecl(bf, obj.Name())
		in.File = path
	case obj.Pos().IsValid():
		p := prog.Fset().Position(obj.Pos())
		if loc := r.abs[p.Filename]; loc != nil {
			// Declared in the repository: the analysed syntax.
			fset, f, pos = loc.prog.Fset(), loc.ast, obj.Pos()
			in.File, in.Line, in.Local = loc.prog.RelFile(p.Filename), p.Line, true
			if fn, ok := obj.(*types.Func); ok {
				in.Symbol = fn.FullName()
			}
		} else {
			ef, efset, display := r.src.parse(p.Filename)
			in.File, in.Line = display, p.Line
			if ef == nil {
				return
			}
			fset, f = efset, ef
			// Export data keeps the line of a declaration, not always its
			// column: the declared name is looked up on that line.
			pos = identOnLine(efset, ef, p.Line, obj.Name())
		}
	default:
		return
	}
	if !pos.IsValid() {
		return
	}
	if in.Line == 0 {
		in.Line = fset.Position(pos).Line
	}
	path, _ := astutil.PathEnclosingInterval(f, pos, pos)
	doc, decl, tag := docAt(fset, f, path, pos)
	in.Doc = doc
	in.Decl = decl
	if tag != "" && in.Kind == "field" {
		in.Signature += " " + tag
	}
	if in.Kind == "builtin" && decl != "" {
		in.Signature, in.Decl = decl, ""
	}
}

// identOnLine is the position of the first identifier called name on a
// line of f (a declaration names itself before using other identifiers of
// the same name on its line), or NoPos.
func identOnLine(fset *token.FileSet, f *ast.File, line int, name string) token.Pos {
	found := token.NoPos
	ast.Inspect(f, func(n ast.Node) bool {
		if found.IsValid() || n == nil {
			return false
		}
		if l := fset.Position(n.Pos()).Line; l > line || fset.Position(n.End()).Line < line {
			return false // the subtree does not reach the line
		}
		if id, ok := n.(*ast.Ident); ok && id.Name == name && fset.Position(id.Pos()).Line == line {
			found = id.Pos()
		}
		return true
	})
	return found
}

// findDecl is the position of the top-level declaration named name.
func findDecl(f *ast.File, name string) token.Pos {
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil && d.Name.Name == name {
				return d.Name.Pos()
			}
		case *ast.GenDecl:
			for _, s := range d.Specs {
				switch s := s.(type) {
				case *ast.TypeSpec:
					if s.Name.Name == name {
						return s.Name.Pos()
					}
				case *ast.ValueSpec:
					for _, n := range s.Names {
						if n.Name == name {
							return n.Pos()
						}
					}
				}
			}
		}
	}
	return token.NoPos
}

// docAt walks from the identifier declared at pos to its declaration and
// returns its documentation, the source of a struct or interface type
// declaration, and a field's tag. The walk stops at a block: a local
// variable has no documentation, and the enclosing function's must not be
// taken for it.
func docAt(fset *token.FileSet, f *ast.File, path []ast.Node, pos token.Pos) (doc, decl, tag string) {
	names := func(ids []*ast.Ident) bool {
		for _, id := range ids {
			if id.Pos() == pos {
				return true
			}
		}
		return false
	}
	text := func(gs ...*ast.CommentGroup) string {
		for _, g := range gs {
			if g != nil {
				if t := strings.TrimSpace(g.Text()); t != "" {
					return t
				}
			}
		}
		return ""
	}
	for i, n := range path {
		switch n := n.(type) {
		case *ast.BlockStmt, *ast.FuncLit:
			return "", "", ""
		case *ast.Field:
			if names(n.Names) || len(n.Names) == 0 && n.Type.Pos() <= pos && pos < n.Type.End() {
				if n.Tag != nil {
					tag = n.Tag.Value
				}
				return text(n.Doc, n.Comment), "", tag
			}
		case *ast.FuncDecl:
			if n.Name.Pos() == pos {
				return text(n.Doc), "", ""
			}
			return "", "", ""
		case *ast.TypeSpec:
			if n.Name.Pos() != pos {
				continue
			}
			doc = text(n.Doc, n.Comment)
			if g, ok := parent(path, i).(*ast.GenDecl); ok && doc == "" && !g.Lparen.IsValid() {
				doc = text(g.Doc)
			}
			switch n.Type.(type) {
			case *ast.StructType, *ast.InterfaceType:
				decl = printSpec(fset, f, n)
			}
			return doc, decl, ""
		case *ast.ValueSpec:
			if !names(n.Names) {
				continue
			}
			doc = text(n.Doc, n.Comment)
			if g, ok := parent(path, i).(*ast.GenDecl); ok && doc == "" && !g.Lparen.IsValid() {
				doc = text(g.Doc)
			}
			return doc, "", ""
		}
	}
	// Builtins: findDecl gave the name of a FuncDecl or spec directly.
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Pos() == pos {
			return text(fd.Doc), printFunc(fset, fd), ""
		}
	}
	return "", "", ""
}

func parent(path []ast.Node, i int) ast.Node {
	if i+1 < len(path) {
		return path[i+1]
	}
	return nil
}

// printSpec prints "type Name …" with the comments inside the type (field
// documentation), at most MaxDeclLines lines.
func printSpec(fset *token.FileSet, f *ast.File, s *ast.TypeSpec) string {
	cp := *s
	cp.Doc, cp.Comment = nil, nil
	var comments []*ast.CommentGroup
	for _, c := range f.Comments {
		if c.Pos() >= s.Pos() && c.End() <= s.End() {
			comments = append(comments, c)
		}
	}
	var b bytes.Buffer
	b.WriteString("type ")
	cfg := printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 4}
	if err := cfg.Fprint(&b, fset, &printer.CommentedNode{Node: &cp, Comments: comments}); err != nil {
		return ""
	}
	return clipLines(b.String(), MaxDeclLines)
}

// printFunc prints a function's declaration without its body.
func printFunc(fset *token.FileSet, fd *ast.FuncDecl) string {
	cp := *fd
	cp.Doc, cp.Body = nil, nil
	var b bytes.Buffer
	if err := (&printer.Config{Mode: printer.UseSpaces, Tabwidth: 4}).Fprint(&b, fset, &cp); err != nil {
		return ""
	}
	return b.String()
}

func clipLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[:n-1], "\n") + "\n\t// … (" + strconv.Itoa(len(lines)-n+1) + " more lines)\n}"
}
