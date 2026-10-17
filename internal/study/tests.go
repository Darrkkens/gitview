package study

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// TestRef is a module function a test calls directly. Test files are not
// part of the SSA program, so references are read from their syntax:
// STATIC when the call names one function unambiguously (F(...) in the
// same package, pkg.F(...) of an imported module package), HEURISTIC when
// only a method name matches (x.M(...): the receiver's type is not known).
type TestRef struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Basis      string `json:"basis"`
	Call       string `json:"call"`
	Line       int    `json:"line"`
	Candidates int    `json:"candidates,omitempty"` // other functions with the same method name
}

// TestFunc is a test, benchmark or fuzz function.
type TestFunc struct {
	Name   string    `json:"name"`
	Pkg    string    `json:"pkg"`
	File   string    `json:"file"`
	Line   int       `json:"line"`
	Refs   []TestRef `json:"refs,omitempty"`
	Status string    `json:"status,omitempty"` // outcome in the last test run (RUNTIME)
}

// scanTests reads the _test.go files of every module package.
func scanTests(in Inputs, x *graph) []TestFunc {
	results := map[string]string{}
	for _, t := range in.Tests {
		results[t.Package+"\x00"+t.Name] = t.Status
	}
	// Module packages by directory and by import path.
	type pkgInfo struct{ path, name string }
	byDir := map[string]pkgInfo{}
	modulePkgs := map[string]bool{}
	for _, p := range in.Programs {
		for _, pi := range p.Packages() {
			byDir[pi.Dir] = pkgInfo{pi.Path, pi.Name}
			modulePkgs[pi.Path] = true
		}
	}
	dirs := make([]string, 0, len(byDir))
	for d := range byDir {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	var out []TestFunc
	for _, dir := range dirs {
		pkg := byDir[dir]
		entries, err := os.ReadDir(filepath.Join(in.Root, filepath.FromSlash(dir)))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			rel := dir + "/" + e.Name()
			if dir == "." {
				rel = e.Name()
			}
			out = append(out, scanTestFile(in.Root, rel, pkg.path, pkg.name, modulePkgs, x)...)
		}
	}
	for i := range out {
		out[i].Status = results[out[i].Pkg+"\x00"+out[i].Name]
	}
	return out
}

func scanTestFile(root, rel, pkgPath, pkgName string, modulePkgs map[string]bool, x *graph) []TestFunc {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Join(root, filepath.FromSlash(rel)), nil, parser.SkipObjectResolution)
	if err != nil {
		return nil
	}
	internal := f.Name.Name == pkgName // package foo, not foo_test
	imports := map[string]string{}     // local name → module package path
	for _, im := range f.Imports {
		p, _ := strconv.Unquote(im.Path.Value)
		if !modulePkgs[p] {
			continue
		}
		name := p[strings.LastIndexByte(p, '/')+1:]
		if im.Name != nil {
			name = im.Name.Name
		}
		imports[name] = p
	}
	// Packages whose methods a test may call: its own and the imported ones.
	scope := []string{pkgPath}
	for _, p := range imports {
		scope = append(scope, p)
	}
	sort.Strings(scope)
	var out []TestFunc
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Recv != nil || fd.Body == nil || !isTestName(fd.Name.Name) {
			continue
		}
		tf := TestFunc{Name: fd.Name.Name, Pkg: pkgPath, File: rel, Line: fset.Position(fd.Pos()).Line}
		seen := map[string]bool{}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			line := fset.Position(call.Pos()).Line
			text := callText(call)
			addRef := func(id, basis string, candidates int) {
				node := x.nodes[id]
				if node == nil || seen[id] {
					return
				}
				seen[id] = true
				tf.Refs = append(tf.Refs, TestRef{ID: id, Name: node.Name, Basis: basis, Call: text, Line: line, Candidates: candidates})
			}
			switch fn := call.Fun.(type) {
			case *ast.Ident:
				if internal {
					addRef(pkgPath+"."+fn.Name, Static, 0)
				}
			case *ast.SelectorExpr:
				if id, ok := fn.X.(*ast.Ident); ok {
					if p, ok := imports[id.Name]; ok {
						addRef(p+"."+fn.Sel.Name, Static, 0)
						return true
					}
				}
				var cands []string
				for _, p := range scope {
					for _, node := range x.byPkg[p] {
						if strings.HasSuffix(node.ID, ")."+fn.Sel.Name) && strings.Contains(node.ID, p+".") {
							cands = append(cands, node.ID)
						}
					}
				}
				if len(cands) >= 1 && len(cands) <= 3 {
					for _, c := range cands {
						addRef(c, Heuristic, len(cands)-1)
					}
				}
			}
			return true
		})
		out = append(out, tf)
	}
	return out
}

func isTestName(name string) bool {
	for _, p := range []string{"Test", "Benchmark", "Fuzz", "Example"} {
		if strings.HasPrefix(name, p) && (len(name) == len(p) || !isLower(name[len(p)])) {
			return true
		}
	}
	return false
}

func isLower(c byte) bool { return c >= 'a' && c <= 'z' }

func callText(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name + "(…)"
	case *ast.SelectorExpr:
		if id, ok := fn.X.(*ast.Ident); ok {
			return id.Name + "." + fn.Sel.Name + "(…)"
		}
		return "…." + fn.Sel.Name + "(…)"
	}
	return "(…)"
}

// testReach maps each module function to the tests that reach it through
// the call graph from their direct references (at most depth calls).
type testReach struct {
	Test  int    // index in the tests
	Hops  int    // 0: referenced directly
	Basis string // HEURISTIC when the first reference is
	Via   TestRef
}

func reachByTests(tests []TestFunc, x *graph, depth int) map[string][]testReach {
	out := map[string][]testReach{}
	for ti, t := range tests {
		best := map[string]testReach{}
		for _, r := range t.Refs {
			queue := []string{r.ID}
			hops := map[string]int{r.ID: 0}
			for len(queue) > 0 {
				id := queue[0]
				queue = queue[1:]
				h := hops[id]
				cur, ok := best[id]
				if !ok || h < cur.Hops || h == cur.Hops && r.Basis == Static && cur.Basis != Static {
					best[id] = testReach{Test: ti, Hops: h, Basis: r.Basis, Via: r}
				}
				if h >= depth {
					continue
				}
				for _, e := range x.callees(id) {
					if _, seen := hops[e.To]; !seen && x.isModuleFunc(e.To) {
						hops[e.To] = h + 1
						queue = append(queue, e.To)
					}
				}
			}
		}
		ids := make([]string, 0, len(best))
		for id := range best {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			out[id] = append(out[id], best[id])
		}
	}
	for _, l := range out {
		sort.SliceStable(l, func(i, j int) bool {
			if l[i].Hops != l[j].Hops {
				return l[i].Hops < l[j].Hops
			}
			return tests[l[i].Test].Name < tests[l[j].Test].Name
		})
	}
	return out
}
