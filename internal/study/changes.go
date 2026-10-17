package study

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// ChangedSymbol is a function whose body the diff touches.
type ChangedSymbol struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Line  int    `json:"line"`
	Layer string `json:"layer"`
}

// Related links a changed function to another part of the change, or to
// a function of another layer, through a call.
type Related struct {
	From     string   `json:"from"` // changed function
	To       string   `json:"to"`   // node ID
	ToName   string   `json:"toName"`
	ToFile   string   `json:"toFile,omitempty"`
	Layer    string   `json:"layer"`
	Dir      string   `json:"dir"` // calls | called by
	Changed  bool     `json:"changed,omitempty"`
	Evidence Evidence `json:"evidence"`
}

// ChangedFile is a file of the diff in the change journey.
type ChangedFile struct {
	Path     string          `json:"path"`
	Status   string          `json:"status"`
	Added    int             `json:"added"`
	Deleted  int             `json:"deleted"`
	Layer    string          `json:"layer"`
	Basis    string          `json:"basis"`
	Evidence []Evidence      `json:"evidence"`
	Symbols  []ChangedSymbol `json:"symbols,omitempty"`
	Related  []Related       `json:"related,omitempty"`
}

// ChangeGroup is the part of the change in one layer.
type ChangeGroup struct {
	Layer string        `json:"layer"`
	Files []ChangedFile `json:"files"`
}

var frontendExt = map[string]bool{".vue": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true,
	".svelte": true, ".css": true, ".scss": true, ".html": true, ".htm": true}

// fileLayerOf classifies a changed file: Go files by their changed
// functions (or their package), the others by name or extension.
func (a *arch) fileLayerOf(f FileChange, syms []ChangedSymbol) (string, string, []Evidence) {
	base := path.Base(f.Path)
	ext := strings.ToLower(path.Ext(base))
	switch {
	case strings.HasSuffix(base, "_test.go"):
		return LayerTests, Static, []Evidence{{Basis: Static, Text: "Go test file (name ends in _test.go)", File: f.Path}}
	case base == "go.mod" || base == "go.sum" || base == "go.work":
		return LayerDeps, Static, []Evidence{{Basis: Static, Text: "Go module file", File: f.Path}}
	case ext == ".go":
		counts := map[string]int{}
		for _, s := range syms {
			counts[s.Layer]++
		}
		if l, n := mostFrequent(counts); n > 0 && l != LayerUnknown {
			basis := Heuristic
			var ev []Evidence
			for _, s := range syms {
				if r := a.final[s.ID]; r != nil && r.Layer == l {
					basis = r.Basis
					ev = append(ev, Evidence{Basis: r.Basis, Text: fmt.Sprintf("changed function %s is %s", s.Name, l), File: f.Path, Line: s.Line, Ref: "codeflow:" + s.ID})
					if r.Basis == Static {
						break
					}
				}
			}
			return l, basis, ev[:1]
		}
		dir := path.Dir(f.Path)
		for _, p := range a.order {
			if pi := a.pkgs[p]; pi.Dir == dir {
				return pi.Layer, Heuristic, []Evidence{{Basis: Heuristic, Text: "file of package " + pi.Name + ", classified as " + pi.Layer, File: f.Path}}
			}
		}
		if l, ev, ok := pathLayer(dir, []string{f.Path}); ok {
			return l, Heuristic, []Evidence{ev}
		}
	case ext == ".sql":
		text := "SQL file"
		if strings.Contains(strings.ToLower(f.Path), "migration") {
			text = "SQL file in a migrations directory"
		}
		return LayerDatabase, Heuristic, []Evidence{{Basis: Heuristic, Text: text + " (by extension)", File: f.Path}}
	case frontendExt[ext]:
		return LayerFrontend, Heuristic, []Evidence{{Basis: Heuristic, Text: "web front-end file (by extension " + ext + ")", File: f.Path}}
	case ext == ".md" || ext == ".rst" || ext == ".txt" && strings.Contains(strings.ToLower(base), "readme"):
		return LayerDocs, Heuristic, []Evidence{{Basis: Heuristic, Text: "documentation (by extension)", File: f.Path}}
	}
	return LayerUnknown, Heuristic, []Evidence{{Basis: Heuristic, Text: "the role of this file could not be determined", File: f.Path}}
}

// changeJourney groups the changed files by layer, with their changed
// functions and the calls that connect them to the rest.
func changeJourney(in Inputs, x *graph, a *arch) []ChangeGroup {
	changedFile := map[string]bool{}
	for _, f := range in.Files {
		changedFile[f.Path] = true
	}
	groups := map[string]*ChangeGroup{}
	for _, f := range in.Files {
		cf := ChangedFile{Path: f.Path, Status: f.Status, Added: f.Added, Deleted: f.Deleted}
		for _, n := range x.byFile[f.Path] {
			if !n.Changed || n.Kind != "func" {
				continue
			}
			l, _ := a.layerOf(n.ID)
			cf.Symbols = append(cf.Symbols, ChangedSymbol{ID: n.ID, Name: n.Name, Line: n.Line, Layer: l})
		}
		cf.Layer, cf.Basis, cf.Evidence = a.fileLayerOf(f, cf.Symbols)
		cf.Evidence = append(cf.Evidence, Evidence{Basis: Git, Text: fmt.Sprintf("+%d −%d lines in the working tree", f.Added, f.Deleted), File: f.Path})
		cf.Related = related(x, a, cf, changedFile)
		g := groups[cf.Layer]
		if g == nil {
			g = &ChangeGroup{Layer: cf.Layer}
			groups[cf.Layer] = g
		}
		g.Files = append(g.Files, cf)
	}
	var out []ChangeGroup
	for _, l := range Layers {
		if g := groups[l]; g != nil {
			sort.Slice(g.Files, func(i, j int) bool { return g.Files[i].Path < g.Files[j].Path })
			out = append(out, *g)
		}
	}
	return out
}

// related lists, for the changed functions of a file, the calls to or from
// changed code elsewhere and to other layers (at most 8).
func related(x *graph, a *arch, cf ChangedFile, changedFile map[string]bool) []Related {
	var out []Related
	seen := map[string]bool{}
	add := func(sym ChangedSymbol, other string, dir string, file string, line int) {
		n := x.nodes[other]
		if n == nil || seen[sym.ID+dir+other] || other == sym.ID {
			return
		}
		l, _ := a.layerOf(other)
		changed := n.Changed || changedFile[n.File] && n.File != cf.Path
		if !changed && l == sym.Layer {
			return
		}
		seen[sym.ID+dir+other] = true
		out = append(out, Related{From: sym.ID, To: other, ToName: n.Name, ToFile: n.File, Layer: l, Dir: dir, Changed: changed,
			Evidence: Evidence{Basis: Static, Text: dir + " " + n.Name, File: file, Line: line, Ref: "codeflow:" + sym.ID}})
	}
	for _, sym := range cf.Symbols {
		for _, e := range x.callers(sym.ID) {
			add(sym, e.From, "called by", e.File, e.Line)
		}
		for _, e := range x.callees(sym.ID) {
			add(sym, e.To, "calls", e.File, e.Line)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Changed != out[j].Changed {
			return out[i].Changed
		}
		return layerRank(out[i].Layer) < layerRank(out[j].Layer)
	})
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}
