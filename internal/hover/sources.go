package hover

import (
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// maxSource bounds the external files parsed for documentation.
const maxSource = 4 << 20

// sources parses files outside the repository (the standard library,
// dependencies) for their documentation, once each.
type sources struct {
	once           sync.Once
	goroot, modDir string

	mu     sync.Mutex
	parsed map[string]*parsed
}

type parsed struct {
	f       *ast.File
	fset    *token.FileSet
	display string
}

func newSources() *sources { return &sources{parsed: map[string]*parsed{}} }

// roots finds GOROOT and the module cache, as the go command sees them.
func (s *sources) roots() {
	s.once.Do(func() {
		if out, err := exec.Command("go", "env", "GOROOT", "GOMODCACHE").Output(); err == nil {
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			if len(lines) == 2 {
				s.goroot, s.modDir = strings.TrimSpace(lines[0]), strings.TrimSpace(lines[1])
			}
		}
		if s.goroot == "" {
			s.goroot = build.Default.GOROOT
		}
	})
}

// resolve maps a file name of the type information to a file on disk and
// a name to show. Export data may record standard library files as
// $GOROOT/src/….
func (s *sources) resolve(name string) (disk, display string) {
	s.roots()
	name = filepath.ToSlash(name)
	if rest, ok := strings.CutPrefix(name, "$GOROOT/"); ok {
		return filepath.Join(s.goroot, filepath.FromSlash(rest)), name
	}
	if s.goroot != "" {
		if rel, err := filepath.Rel(s.goroot, filepath.FromSlash(name)); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.FromSlash(name), "$GOROOT/" + filepath.ToSlash(rel)
		}
	}
	if s.modDir != "" {
		if rel, err := filepath.Rel(s.modDir, filepath.FromSlash(name)); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.FromSlash(name), filepath.ToSlash(rel)
		}
	}
	return filepath.FromSlash(name), name
}

// parse reads and parses a Go file (with comments) outside the repository.
func (s *sources) parse(name string) (*ast.File, *token.FileSet, string) {
	disk, display := s.resolve(name)
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.parsed[disk]; ok {
		return p.f, p.fset, display
	}
	p := &parsed{display: display}
	s.parsed[disk] = p
	if !strings.HasSuffix(disk, ".go") {
		return nil, nil, display
	}
	st, err := os.Stat(disk)
	if err != nil || !st.Mode().IsRegular() || st.Size() > maxSource {
		return nil, nil, display
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, disk, nil, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil && f == nil {
		return nil, nil, display
	}
	p.f, p.fset = f, fset
	return f, fset, display
}

// builtin parses $GOROOT/src/builtin/builtin.go, which documents the
// predeclared identifiers.
func (s *sources) builtin() (*ast.File, *token.FileSet, string) {
	f, fset, display := s.parse("$GOROOT/src/builtin/builtin.go")
	return f, fset, display
}
