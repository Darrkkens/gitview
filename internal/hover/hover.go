// Package hover answers "what is this identifier?" for the Go code GitView
// shows, as an editor's hover does: the declaration, signature and
// documentation of the object an identifier refers to. It reads the
// analysis GitView already ran (go/types over the repository's packages);
// no language server is involved and nothing is executed.
//
// An identifier is addressed by file, line, byte column and name. The name
// must match the identifier the analysis found at that position, so code
// that changed after the analysis is reported as stale instead of being
// described wrongly, and a word that is no identifier (in a string or a
// comment) gets no description.
package hover

import (
	"errors"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/tools/go/packages"

	"github.com/athos/gitview/internal/flow"
)

// Info describes the object an identifier refers to.
type Info struct {
	Name string `json:"name"`
	// Kind is func, method, type, field, var, const, package, builtin or
	// label.
	Kind string `json:"kind"`
	// Signature is the declaration in Go syntax, qualified relative to the
	// package of the hovered code: "func strings.TrimSpace(s string) string".
	Signature string `json:"sig"`
	// Decl is the source of a struct or interface type, with the comments
	// of its fields (at most MaxDeclLines lines).
	Decl string `json:"decl,omitempty"`
	Doc  string `json:"doc,omitempty"`
	Pkg  string `json:"pkg,omitempty"` // import path
	// File and Line locate the declaration: repository-relative when it is
	// in the repository (Local), else $GOROOT/… or module@version/….
	File  string `json:"file,omitempty"`
	Line  int    `json:"line,omitempty"`
	Local bool   `json:"local,omitempty"`
	// Symbol is the function's Code Flow ID, for functions of the
	// repository (links to the Inspector).
	Symbol string `json:"symbol,omitempty"`
}

// MaxDeclLines bounds Decl.
const MaxDeclLines = 60

var (
	ErrNotAnalysed = errors.New("this file is not in the analysed Go packages")
	ErrStale       = errors.New("the file changed after the analysis: run the analysis again to describe this code")
	ErrNoIdent     = errors.New("no Go identifier with this name at this position (a word in a string or a comment?)")
	ErrNoObject    = errors.New("this identifier refers to nothing that can be described")
)

// Resolver answers hovers over the files of analysed programs. It is safe
// for concurrent use.
type Resolver struct {
	files map[string]*file // repository-relative path
	abs   map[string]*file // absolute path (declarations in the repository)
	src   *sources

	mu    sync.Mutex
	cache map[cacheKey]*Info
}

type cacheKey struct {
	obj  types.Object
	from *types.Package
}

// file is one analysed source file.
type file struct {
	prog *flow.Program
	pkg  *packages.Package
	ast  *ast.File
	tf   *token.File
	// size and mod are the file's at indexing time: a lookup that finds no
	// identifier tells a changed file (stale) from a word that is not one.
	size int64
	mod  time.Time

	once   sync.Once
	idents map[int][]*ast.Ident // by line
}

// New indexes the files of the programs. A file found in several programs
// (overlapping modules) is taken from the first.
func New(progs []*flow.Program) *Resolver {
	r := &Resolver{files: map[string]*file{}, abs: map[string]*file{}, src: newSources(), cache: map[cacheKey]*Info{}}
	for _, p := range progs {
		fset := p.Fset()
		for _, pk := range p.TypedPackages() {
			if pk.TypesInfo == nil {
				continue
			}
			for _, f := range pk.Syntax {
				tf := fset.File(f.Pos())
				if tf == nil {
					continue
				}
				rel := p.RelFile(tf.Name())
				if filepath.IsAbs(rel) {
					continue // outside the repository
				}
				if _, dup := r.files[rel]; dup {
					continue
				}
				e := &file{prog: p, pkg: pk, ast: f, tf: tf}
				if st, err := os.Stat(tf.Name()); err == nil {
					e.size, e.mod = st.Size(), st.ModTime()
				}
				r.files[rel] = e
				r.abs[tf.Name()] = e
			}
		}
	}
	return r
}

// Files reports how many files the resolver knows.
func (r *Resolver) Files() int { return len(r.files) }

// Has reports whether file (repository-relative) was analysed.
func (r *Resolver) Has(path string) bool { return r.files[cleanPath(path)] != nil }

func cleanPath(p string) string {
	return strings.TrimPrefix(filepath.ToSlash(filepath.Clean(p)), "./")
}

func (f *file) index() {
	f.once.Do(func() {
		f.idents = map[int][]*ast.Ident{}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				line := f.tf.Line(id.Pos())
				f.idents[line] = append(f.idents[line], id)
			}
			return true
		})
	})
}

// changed reports whether the file on disk is no longer the indexed one.
func (f *file) changed() bool {
	st, err := os.Stat(f.tf.Name())
	return err != nil || st.Size() != f.size || !st.ModTime().Equal(f.mod)
}

// ident finds the identifier named name at line:col (col in bytes, 1-based).
func (f *file) ident(line, col int, name string) *ast.Ident {
	f.index()
	for _, id := range f.idents[line] {
		if id.Name == name && f.tf.Position(id.Pos()).Column == col {
			return id
		}
	}
	return nil
}

// Lookup describes the identifier name at path:line:col.
func (r *Resolver) Lookup(path string, line, col int, name string) (*Info, error) {
	f := r.files[cleanPath(path)]
	if f == nil {
		return nil, ErrNotAnalysed
	}
	id := f.ident(line, col, name)
	if id == nil {
		if f.changed() {
			return nil, ErrStale
		}
		return nil, ErrNoIdent
	}
	return r.describeIdent(f, id)
}

func (r *Resolver) describeIdent(f *file, id *ast.Ident) (*Info, error) {
	info := f.pkg.TypesInfo
	obj := info.Uses[id]
	if obj == nil {
		obj = info.Defs[id]
	}
	if obj == nil {
		obj = info.Implicits[id]
	}
	if obj == nil {
		return nil, ErrNoObject
	}
	from := f.pkg.Types
	k := cacheKey{obj, from}
	r.mu.Lock()
	if in, ok := r.cache[k]; ok {
		r.mu.Unlock()
		return in, nil
	}
	r.mu.Unlock()
	in := r.describe(f.prog, obj, from)
	if in == nil {
		return nil, ErrNoObject
	}
	r.mu.Lock()
	r.cache[k] = in
	r.mu.Unlock()
	return in, nil
}

// Occurrence is an identifier of a file, for Table.
type Occurrence struct {
	Line, Col int
	Name      string
}

// Idents lists the identifiers on the given lines of a file.
func (r *Resolver) Idents(path string, lines []int) []Occurrence {
	f := r.files[cleanPath(path)]
	if f == nil {
		return nil
	}
	f.index()
	var out []Occurrence
	for _, l := range lines {
		for _, id := range f.idents[l] {
			out = append(out, Occurrence{Line: l, Col: f.tf.Position(id.Pos()).Column, Name: id.Name})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].Col < out[j].Col
	})
	return out
}
