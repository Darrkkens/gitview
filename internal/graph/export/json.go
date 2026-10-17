// Package export writes the Repository Graph dataset as JSON or as a single
// offline HTML page.
package export

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/athos/gitview/internal/evidence"
	"github.com/athos/gitview/internal/graph/model"
)

// Levels of detail.
const (
	LevelFull      = "full"
	LevelPackages  = "packages"
	LevelFiles     = "files"
	LevelFunctions = "functions"
	LevelTypes     = "types"
)

// Levels lists the accepted --level values.
var Levels = []string{LevelFull, LevelPackages, LevelFiles, LevelFunctions, LevelTypes}

func ValidLevel(l string) bool {
	for _, x := range Levels {
		if x == l {
			return true
		}
	}
	return false
}

var levelNodes = map[string]map[string]bool{
	LevelPackages: {model.KindModule: true, model.KindPackage: true},
	LevelFiles:    {model.KindPackage: true, model.KindFile: true},
	LevelFunctions: {model.KindFunc: true, model.KindMethod: true, model.KindClosure: true, model.KindRoute: true,
		model.KindDB: true, model.KindExternal: true, model.KindUnresolved: true},
	LevelTypes: {model.KindStruct: true, model.KindInterface: true, model.KindType: true},
}

var levelEdges = map[string]map[string]bool{
	LevelPackages:  {model.EdgeContains: true, model.EdgeImports: true},
	LevelFiles:     {model.EdgeContains: true},
	LevelFunctions: {model.EdgeCalls: true, model.EdgeGo: true, model.EdgeDefer: true, model.EdgeHandles: true, model.EdgeQueries: true, model.EdgeContains: true},
	LevelTypes:     {model.EdgeImplements: true, model.EdgeEmbeds: true, model.EdgeReferences: true},
}

// Filter returns the dataset restricted to one level. The full level is the
// graph itself. The files level adds file → file edges derived from the
// relations between the symbols they declare (marked "derived", with the
// number and kinds of relations they summarise); every other edge is one
// found by the analysis.
func Filter(g *model.Graph, level string) *model.Graph {
	if level == "" || level == LevelFull {
		return g
	}
	keepN, keepE := levelNodes[level], levelEdges[level]
	out := *g
	out.Nodes, out.Edges = nil, nil
	kept := map[string]bool{}
	for _, n := range g.Nodes {
		if keepN[n.Type] {
			out.Nodes = append(out.Nodes, n)
			kept[n.ID] = true
		}
	}
	for _, e := range g.Edges {
		if keepE[e.Type] && kept[e.Source] && kept[e.Target] {
			out.Edges = append(out.Edges, e)
		}
	}
	if level == LevelFiles {
		out.Edges = append(out.Edges, fileEdges(g)...)
	}
	for i := range out.Nodes {
		n := out.Nodes[i]
		if n.Parent != "" && !kept[n.Parent] || n.Group != "" && !kept[n.Group] {
			c := *n
			if !kept[c.Parent] {
				c.Parent = ""
			}
			if !kept[c.Group] {
				c.Group = "" // the package path is still in Package
			}
			out.Nodes[i] = &c
		}
	}
	var comms []model.Community
	for _, c := range g.Communities {
		if level == LevelPackages || level == LevelFiles {
			if c.Level == "package" {
				comms = append(comms, c)
			}
		} else if c.Level == "symbol" {
			comms = append(comms, c)
		}
	}
	out.Communities = comms
	out.Sort()
	return &out
}

// fileEdges aggregates symbol relations between declarations of different
// files.
func fileEdges(g *model.Graph) []*model.Edge {
	idx := g.Index()
	type agg struct {
		count int
		kinds map[string]int
		conf  string
		ev    *evidence.Evidence
	}
	rank := map[string]int{model.Confirmed: 4, model.Likely: 3, model.Possible: 2, model.Unresolved: 1}
	m := map[[2]string]*agg{}
	for _, e := range g.Edges {
		if e.Type == model.EdgeContains || e.Type == model.EdgeImports {
			continue
		}
		s, t := idx[e.Source], idx[e.Target]
		if s.File == "" || t.File == "" || s.File == t.File || s.Type == model.KindFile || t.Type == model.KindFile {
			continue
		}
		fs, ft := "file:"+s.File, "file:"+t.File
		if idx[fs] == nil || idx[ft] == nil {
			continue
		}
		key := [2]string{fs, ft}
		a := m[key]
		if a == nil {
			a = &agg{kinds: map[string]int{}, conf: e.Confidence, ev: e.Evidence}
			m[key] = a
		}
		a.count += max(e.Weight, 1)
		a.kinds[e.Type]++
		if rank[e.Confidence] > rank[a.conf] {
			a.conf = e.Confidence
		}
	}
	keys := make([][2]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] != keys[j][0] {
			return keys[i][0] < keys[j][0]
		}
		return keys[i][1] < keys[j][1]
	})
	out := make([]*model.Edge, 0, len(keys))
	for _, k := range keys {
		a := m[k]
		ev := evidence.Evidence{Kind: evidence.Static, Basis: evidence.BasisStatic, Source: "derived from symbol relations",
			Details: fmt.Sprintf("%d relation(s) between declarations of the two files", a.count)}
		if a.ev != nil {
			ev.File, ev.Line = a.ev.File, a.ev.Line
		}
		out = append(out, &model.Edge{ID: model.EdgeID(model.EdgeDepends, k[0], k[1]), Source: k[0], Target: k[1], Type: model.EdgeDepends,
			Confidence: a.conf, Weight: a.count, Evidence: &ev, Metadata: map[string]any{"derived": true, "kinds": a.kinds}})
	}
	return out
}

// JSON writes the dataset at the given level, indented.
func JSON(w io.Writer, g *model.Graph, level string) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(Filter(g, level))
}
