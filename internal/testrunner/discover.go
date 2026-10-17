package testrunner

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var skipDirs = map[string]bool{".git": true, "node_modules": true, "vendor": true, "testdata": true, "dist": true, "build": true, ".venv": true}

func skipDir(name string) bool {
	return skipDirs[name] || (len(name) > 1 && (name[0] == '.' || name[0] == '_'))
}

// Discover finds Go modules and Node.js packages with tests under root. When
// configured projects are given they are used instead of discovery. Nothing
// is executed.
func Discover(root string, configured []ProjectConfig) ([]*Project, error) {
	if len(configured) > 0 {
		return fromConfig(root, configured)
	}
	var out []*Project
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != root && skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		switch d.Name() {
		case "go.mod":
			if pr := goProject(root, filepath.Dir(p)); pr != nil {
				out = append(out, pr)
			}
		case "package.json":
			if pr := nodeProject(root, filepath.Dir(p)); pr != nil {
				out = append(out, pr)
			}
		}
		return nil
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].Rel < out[j].Rel })
	return out, err
}

func fromConfig(root string, cfg []ProjectConfig) ([]*Project, error) {
	var out []*Project
	for _, c := range cfg {
		dir := filepath.Join(root, c.Dir)
		if r := rel(root, dir); r == ".." || strings.HasPrefix(r, "../") {
			return nil, fmt.Errorf("diretório de teste fora do repositório: %s", c.Dir)
		}
		var p *Project
		switch Kind(c.Kind) {
		case KindGo:
			p = goProject(root, dir)
		case KindNode:
			p = nodeProject(root, dir)
		case KindCommand, "":
			if len(c.Command) == 0 {
				return nil, fmt.Errorf("projeto %q sem comando", c.Name)
			}
			p = &Project{Kind: KindCommand, Framework: "command", Dir: dir, Rel: rel(root, dir)}
		default:
			return nil, fmt.Errorf("tipo de projeto desconhecido: %q", c.Kind)
		}
		if p == nil {
			return nil, fmt.Errorf("nenhum projeto %s em %s", c.Kind, c.Dir)
		}
		if len(c.Command) > 0 {
			p.Command = c.Command
		}
		if c.Name != "" {
			p.Name = c.Name
		}
		out = append(out, p)
	}
	return out, nil
}

func rel(root, dir string) string {
	r, err := filepath.Rel(root, dir)
	if err != nil {
		return dir
	}
	return filepath.ToSlash(r)
}

var moduleLine = regexp.MustCompile(`(?m)^module\s+"?([^\s"]+)"?`)

func goProject(root, dir string) *Project {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return nil
	}
	p := &Project{Kind: KindGo, Framework: "go", Dir: dir, Rel: rel(root, dir)}
	if m := moduleLine.FindSubmatch(data); m != nil {
		p.ModulePath = string(m[1])
	}
	p.Name = p.ModulePath
	if p.Name == "" {
		p.Name = p.Rel
	}
	p.Packages = goTestPackages(dir)
	if len(p.Packages) == 0 {
		return nil
	}
	return p
}

// goTestPackages lists directories containing *_test.go files, without
// descending into nested modules.
func goTestPackages(dir string) []string {
	seen := map[string]bool{}
	var pkgs []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p == dir {
				return nil
			}
			if skipDir(d.Name()) {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(p, "go.mod")); err == nil {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), "_test.go") {
			pkg := "./" + rel(dir, filepath.Dir(p))
			if pkg == "./." {
				pkg = "."
			}
			if !seen[pkg] {
				seen[pkg] = true
				pkgs = append(pkgs, pkg)
			}
		}
		return nil
	})
	sort.Strings(pkgs)
	return pkgs
}

// GoPackageDir converts an import path reported by `go test -json` back to a
// package pattern relative to the module directory.
func (p *Project) GoPackageDir(importPath string) string {
	if p.ModulePath == "" || importPath == p.ModulePath {
		return "."
	}
	if r, ok := strings.CutPrefix(importPath, p.ModulePath+"/"); ok {
		return "./" + r
	}
	return importPath
}

type packageJSON struct {
	Name            string            `json:"name"`
	Scripts         map[string]string `json:"scripts"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

const npmDefaultTest = `echo "Error: no test specified" && exit 1`

func nodeProject(root, dir string) *Project {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return nil
	}
	var pkg packageJSON
	if json.Unmarshal(data, &pkg) != nil {
		return nil
	}
	script := pkg.Scripts["test"]
	has := func(dep string) bool {
		_, a := pkg.Dependencies[dep]
		_, b := pkg.DevDependencies[dep]
		return a || b || strings.Contains(script, dep)
	}
	p := &Project{Kind: KindNode, Dir: dir, Rel: rel(root, dir), Name: pkg.Name}
	if p.Name == "" {
		p.Name = p.Rel
	}
	pm := packageManager(root, dir)
	switch {
	case has("vitest"):
		p.Framework, p.Bin = "vitest", nodeBin(root, dir, "vitest")
	case has("jest"):
		p.Framework, p.Bin = "jest", nodeBin(root, dir, "jest")
	case script != "" && !strings.Contains(script, npmDefaultTest):
		p.Framework, p.Command = "script", []string{pm, "test"}
	default:
		return nil
	}
	return p
}

// nodeBin prefers a locally installed binary; npx --no-install never downloads.
func nodeBin(root, dir, name string) []string {
	for d := dir; ; d = filepath.Dir(d) {
		bin := filepath.Join(d, "node_modules", ".bin", name)
		if _, err := os.Stat(bin); err == nil {
			return []string{bin}
		}
		if d == root || d == filepath.Dir(d) || !strings.HasPrefix(d, root) {
			break
		}
	}
	return []string{"npx", "--no-install", name}
}

func packageManager(root, dir string) string {
	for d := dir; strings.HasPrefix(d, root); d = filepath.Dir(d) {
		switch {
		case exists(filepath.Join(d, "pnpm-lock.yaml")):
			return "pnpm"
		case exists(filepath.Join(d, "yarn.lock")):
			return "yarn"
		case exists(filepath.Join(d, "package-lock.json")):
			return "npm"
		}
		if d == root || d == filepath.Dir(d) {
			break
		}
	}
	return "npm"
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Targets lists runnable targets: all tests, tests related to currentFile
// (repository-relative, may be empty), each project and each Go package.
func Targets(root string, projects []*Project, currentFile string) []Target {
	if len(projects) == 0 {
		return nil
	}
	var out []Target
	all := Target{Label: "Todos os testes"}
	for _, p := range projects {
		all.Units = append(all.Units, Unit{Project: p})
	}
	out = append(out, all)
	if t, ok := RelatedTarget(root, projects, currentFile); ok {
		out = append(out, t)
	}
	for _, p := range projects {
		if len(projects) > 1 {
			out = append(out, Target{Label: fmt.Sprintf("%s · %s", p.Framework, p.Name), Units: []Unit{{Project: p}}})
		}
		if p.Kind == KindGo && len(p.Command) == 0 {
			for _, pkg := range p.Packages {
				label := pkg
				if p.Rel != "." {
					label = p.Rel + "/" + strings.TrimPrefix(pkg, "./")
				}
				out = append(out, Target{Label: "go · " + label, Units: []Unit{{Project: p, Packages: []string{pkg}}}})
			}
		}
	}
	return out
}

var goTestFunc = regexp.MustCompile(`^func\s+((?:Test|Example|Fuzz)\w*)\s*\(`)
var nodeSourceFile = regexp.MustCompile(`\.([cm]?[jt]sx?|vue)$`)
var nodeTestFile = regexp.MustCompile(`\.(test|spec)\.[cm]?[jt]sx?$`)

// RelatedTarget returns the tests related to a repository-relative file when
// that relation can be determined.
func RelatedTarget(root string, projects []*Project, file string) (Target, bool) {
	if file == "" {
		return Target{}, false
	}
	abs := filepath.Join(root, file)
	label := "Arquivo atual · " + file
	for _, p := range projects {
		r, err := filepath.Rel(p.Dir, abs)
		if err != nil || strings.HasPrefix(r, "..") || len(p.Command) > 0 && p.Kind != KindNode {
			continue
		}
		switch p.Kind {
		case KindGo:
			if !strings.HasSuffix(file, ".go") {
				continue
			}
			pkg := "./" + filepath.ToSlash(filepath.Dir(r))
			if pkg == "./." {
				pkg = "."
			}
			if !contains(p.Packages, pkg) {
				continue
			}
			u := Unit{Project: p, Packages: []string{pkg}}
			if strings.HasSuffix(file, "_test.go") {
				if names := goTestNames(abs); len(names) > 0 {
					u.Run = "^(" + strings.Join(names, "|") + ")$"
				}
			}
			return Target{Label: label, Units: []Unit{u}}, true
		case KindNode:
			if p.Framework != "vitest" && p.Framework != "jest" {
				continue
			}
			if !nodeSourceFile.MatchString(file) {
				continue
			}
			u := Unit{Project: p}
			if nodeTestFile.MatchString(file) {
				u.Files = []string{r}
			} else {
				u.Related = r
			}
			return Target{Label: label, Units: []Unit{u}}, true
		}
	}
	return Target{}, false
}

func goTestNames(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var names []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if m := goTestFunc.FindStringSubmatch(sc.Text()); m != nil {
			names = append(names, regexp.QuoteMeta(m[1]))
		}
	}
	return names
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
