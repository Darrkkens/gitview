package flow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

var skipDirs = map[string]bool{".git": true, "node_modules": true, "vendor": true, "testdata": true, ".venv": true}

// FindModules returns the directories of Go modules under root.
func FindModules(root string) []string {
	var mods []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != root && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "go.mod" {
			mods = append(mods, filepath.Dir(p))
		}
		return nil
	})
	sort.Strings(mods)
	return mods
}

// EnclosingModule returns the nearest directory at or above dir (and not
// above stop) that contains a go.mod, or "" if there is none. Unlike
// FindModules it does not skip testdata or hidden directories, so a module
// named explicitly is always found.
func EnclosingModule(dir, stop string) string {
	dir, stop = filepath.Clean(dir), filepath.Clean(stop)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		if dir == stop {
			return ""
		}
		parent := filepath.Dir(dir)
		if parent == dir || !strings.HasPrefix(dir, stop) {
			return ""
		}
		dir = parent
	}
}

// ModuleFor returns the innermost module directory containing file (absolute).
func ModuleFor(modules []string, file string) string {
	best := ""
	for _, m := range modules {
		if (file == m || strings.HasPrefix(file, m+string(filepath.Separator))) && len(m) > len(best) {
			best = m
		}
	}
	return best
}

// Version fingerprints the module's Go sources (path, size, mtime), so a
// cached analysis is reused until code actually changes.
func Version(moduleDir string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(moduleDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != moduleDir && (skipDirs[d.Name()] && d.Name() != "vendor" || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			if p != moduleDir {
				if _, err := os.Stat(filepath.Join(p, "go.mod")); err == nil {
					return filepath.SkipDir // nested module
				}
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") && name != "go.mod" && name != "go.sum" && name != "modules.txt" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		fmt.Fprintf(h, "%s\x00%d\x00%d\n", p, info.Size(), info.ModTime().UnixNano())
		return nil
	})
	return hex.EncodeToString(h.Sum(nil)), err
}

// Program is an analysed module.
type Program struct {
	Module   string // module path
	Dir      string // module directory
	RepoRoot string
	Version  string
	Errors   []string // package load/type errors (analysis continues without them)

	fset    *token.FileSet
	prog    *ssa.Program
	project map[*types.Package]bool
	funcs   []*ssa.Function // every function with a body declared in the module
	named   []types.Type    // named non-interface types declared in the module

	pkgs map[*types.Package]*packages.Package // type-checked syntax of module packages

	calls   map[*ssa.Function][]callSite
	callers map[*ssa.Function][]callerRef
	routes  []*route
	handler map[*ssa.Function][]*route // functions registered as route handlers

	implMu    sync.Mutex
	implCache map[string][]*ssa.Function
	srcCache  map[string][]string
	textCache map[string]string
	srcMu     sync.Mutex

	posOnce  sync.Once
	posIndex map[string][]*ssa.Function // repository-relative file → functions declared in it
}

// Load type-checks and builds SSA for every package of the module at dir.
// Dependencies are loaded from export data only (types, no bodies), which
// keeps loading fast and never analyses third-party code.
func Load(ctx context.Context, repoRoot, dir string) (*Program, error) {
	version, err := Version(dir)
	if err != nil {
		return nil, err
	}
	cfg := &packages.Config{
		Context: ctx,
		Dir:     dir,
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes |
			packages.NeedTypesInfo | packages.NeedImports | packages.NeedModule,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("carregando pacotes: %w", err)
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	p := &Program{
		Dir: dir, RepoRoot: repoRoot, Version: version,
		project:   map[*types.Package]bool{},
		pkgs:      map[*types.Package]*packages.Package{},
		calls:     map[*ssa.Function][]callSite{},
		callers:   map[*ssa.Function][]callerRef{},
		handler:   map[*ssa.Function][]*route{},
		implCache: map[string][]*ssa.Function{},
		srcCache:  map[string][]string{},
	}
	var good []*packages.Package
	for _, pk := range pkgs {
		for _, e := range pk.Errors {
			p.Errors = append(p.Errors, e.Error())
		}
		if pk.Module != nil && p.Module == "" {
			p.Module = pk.Module.Path
		}
		if pk.Types == nil || pk.IllTyped || len(pk.Syntax) == 0 {
			continue
		}
		good = append(good, pk)
	}
	if len(good) == 0 {
		if len(p.Errors) > 0 {
			return nil, errors.New(p.Errors[0])
		}
		return nil, errors.New("nenhum pacote Go carregado")
	}
	// GlobalDebug keeps the mapping from source expressions and variables to
	// SSA values (DebugRef), which Data Flow needs to name what it traces.
	prog, ssaPkgs := ssautil.Packages(good, ssa.GlobalDebug)
	p.prog, p.fset = prog, prog.Fset
	for i, sp := range ssaPkgs {
		if sp != nil {
			p.project[sp.Pkg] = true
			p.pkgs[sp.Pkg] = good[i]
		}
	}
	if err := buildSSA(ctx, ssaPkgs); err != nil {
		return nil, err
	}
	p.collect(ssaPkgs)
	p.index(ctx)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	p.findRoutes()
	return p, nil
}

func buildSSA(ctx context.Context, pkgs []*ssa.Package) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("falha ao construir SSA: %v", r)
		}
	}()
	for _, sp := range pkgs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if sp != nil {
			sp.Build()
		}
	}
	return nil
}

// collect gathers module functions (including methods and closures) and
// named types.
func (p *Program) collect(pkgs []*ssa.Package) {
	seen := map[*ssa.Function]bool{}
	var add func(fn *ssa.Function)
	add = func(fn *ssa.Function) {
		if fn == nil || seen[fn] || len(fn.Blocks) == 0 || fn.Synthetic != "" {
			return
		}
		seen[fn] = true
		p.funcs = append(p.funcs, fn)
		for _, a := range fn.AnonFuncs {
			add(a)
		}
	}
	for _, sp := range pkgs {
		if sp == nil {
			continue
		}
		names := make([]string, 0, len(sp.Members))
		for n := range sp.Members {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			switch m := sp.Members[n].(type) {
			case *ssa.Function:
				add(m)
			case *ssa.Type:
				t := m.Type()
				if _, isIface := t.Underlying().(*types.Interface); isIface {
					continue
				}
				if nt, ok := t.(*types.Named); ok && nt.TypeParams().Len() > 0 {
					continue
				}
				p.named = append(p.named, t)
				for _, recv := range []types.Type{t, types.NewPointer(t)} {
					ms := p.prog.MethodSets.MethodSet(recv)
					for i := 0; i < ms.Len(); i++ {
						add(p.prog.MethodValue(ms.At(i)))
					}
				}
			}
		}
	}
	sort.Slice(p.funcs, func(i, j int) bool { return p.funcs[i].Pos() < p.funcs[j].Pos() })
}

func (p *Program) inModule(fn *ssa.Function) bool {
	fn = origin(fn)
	return fn != nil && fn.Pkg != nil && p.project[fn.Pkg.Pkg] && len(fn.Blocks) > 0
}

// origin maps generic instantiations to their declaration.
func origin(fn *ssa.Function) *ssa.Function {
	if fn != nil && fn.Origin() != nil {
		return fn.Origin()
	}
	return fn
}

// position returns the repository-relative file, start and end line of fn.
func (p *Program) position(fn *ssa.Function) (file string, line, end int) {
	pos := fn.Pos()
	if !pos.IsValid() {
		return "", 0, 0
	}
	ps := p.fset.Position(pos)
	file, line = p.relFile(ps.Filename), ps.Line
	end = line
	switch syn := fn.Syntax().(type) {
	case *ast.FuncDecl:
		end = p.fset.Position(syn.End()).Line
	case *ast.FuncLit:
		end = p.fset.Position(syn.End()).Line
	}
	return file, line, end
}

func (p *Program) relFile(abs string) string {
	if abs == "" {
		return ""
	}
	if r, err := filepath.Rel(p.RepoRoot, abs); err == nil && !strings.HasPrefix(r, "..") {
		return filepath.ToSlash(r)
	}
	return abs
}

func (p *Program) site(pos token.Pos) (string, int) {
	if !pos.IsValid() {
		return "", 0
	}
	ps := p.fset.Position(pos)
	return p.relFile(ps.Filename), ps.Line
}

// source returns lines [from, to] of a repository-relative file.
func (p *Program) source(file string, from, to int) string {
	p.srcMu.Lock()
	lines, ok := p.srcCache[file]
	if !ok {
		data, err := os.ReadFile(filepath.Join(p.RepoRoot, file))
		if err == nil {
			lines = strings.Split(string(data), "\n")
		}
		p.srcCache[file] = lines
	}
	p.srcMu.Unlock()
	if from < 1 || from > len(lines) {
		return ""
	}
	to = min(to, len(lines))
	return strings.Join(lines[from-1:to], "\n")
}

// Cache keeps one analysed Program per module and reloads it only when the
// module's Version changes. Loads for the same module are serialised.
type Cache struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
	progs map[string]*Program
}

func NewCache() *Cache {
	return &Cache{locks: map[string]*sync.Mutex{}, progs: map[string]*Program{}}
}

func (c *Cache) Get(ctx context.Context, repoRoot, dir string) (*Program, bool, error) {
	c.mu.Lock()
	l, ok := c.locks[dir]
	if !ok {
		l = &sync.Mutex{}
		c.locks[dir] = l
	}
	c.mu.Unlock()

	l.Lock()
	defer l.Unlock()
	v, err := Version(dir)
	if err != nil {
		return nil, false, err
	}
	c.mu.Lock()
	cached := c.progs[dir]
	c.mu.Unlock()
	if cached != nil && cached.Version == v {
		return cached, true, nil
	}
	prog, err := Load(ctx, repoRoot, dir)
	if err != nil {
		return nil, false, err
	}
	c.mu.Lock()
	c.progs[dir] = prog
	c.mu.Unlock()
	return prog, false, nil
}
