// Package builder assembles the Repository Graph dataset from the analyzer
// output: it merges duplicate relations, computes degrees, detects
// communities and lays the graph out. Each step is deterministic, so the same
// code always produces the same dataset.
package builder

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"path"
	"sort"
	"time"

	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/graph/analyzer"
	"github.com/athos/gitview/internal/graph/layout"
	"github.com/athos/gitview/internal/graph/model"
)

// Options of a build.
type Options struct {
	Repository string // display name
	Root       string // absolute repository root (not exported)
	// MaxSites bounds the call sites listed on a merged edge.
	MaxSites int
	Now      func() time.Time
	// Analysis is how long loading the programs took, for the statistics.
	Analysis time.Duration
}

// Method documents how the dataset was computed; it is written into every
// export so a reader can check each kind of relation.
var Method = []string{
	"Packages, files and imports: go/packages (test files excluded).",
	"Structs, interfaces, embedding, field and signature references: go/types declarations of the analysed packages.",
	"implements: types.Implements for every concrete named type and every non-empty, non-generic interface declared in the same module (value or pointer method set).",
	"calls, go, defer, handles, queries: the Code Flow index (SSA). Confidence: confirmed (static call or known function value), likely (interface with one implementation), possible (one of several implementations, or a function value passed as argument), unresolved (no target found).",
	"Routes and database operations: Code Flow recognisers (net/http, gin, chi, echo; pgx, database/sql, sqlx).",
	"Several call sites between the same functions are merged into one edge; weight is the number of sites and confidence the strongest one.",
	"Communities: Louvain modularity (resolution 1), deterministic order (node ID), ties keep the current community. Package communities use imports plus cross-package symbol relations; symbol communities use calls, type relations and closures. Communities are structural groups, not architectural layers.",
	"Layout: deterministic Fruchterman–Reingold for packages, then each package's members in a disc around it. Positions depend only on the graph.",
}

var confRank = map[string]int{model.Confirmed: 4, model.Likely: 3, model.Possible: 2, model.Unresolved: 1}

// Structural edges describe containment, not dependencies; degrees and
// communities ignore them.
func structural(t string) bool { return t == model.EdgeContains }

// Build analyses the programs and returns the dataset.
func Build(ctx context.Context, progs []*flow.Program, opt Options) (*model.Graph, error) {
	if opt.MaxSites <= 0 {
		opt.MaxSites = 20
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	t0 := time.Now()
	res := analyzer.Analyze(progs)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g := &model.Graph{Schema: model.Schema, Repository: opt.Repository, Root: opt.Root, Modules: res.Modules,
		Generated: opt.Now().UTC().Format(time.RFC3339), Method: Method}
	h := sha256.New()
	vs := append([]string(nil), res.Versions...)
	sort.Strings(vs)
	for _, v := range vs {
		h.Write([]byte(v))
	}
	g.Version = hex.EncodeToString(h.Sum(nil))[:16]

	ids := make([]string, 0, len(res.Nodes))
	for id := range res.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		n := res.Nodes[id]
		if n.Parent != "" && res.Nodes[n.Parent] == nil {
			n.Parent = ""
		}
		if n.Group != "" && res.Nodes[n.Group] == nil {
			n.Group = ""
		}
		g.Nodes = append(g.Nodes, n)
	}
	g.Edges = mergeEdges(res, opt.MaxSites)
	g.Sort()
	degrees(g)
	g.Stats.BuildMS = time.Since(t0).Milliseconds()

	t1 := time.Now()
	communities(g)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	Layout(g, func() bool { return ctx.Err() != nil })
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g.Stats.LayoutMS = time.Since(t1).Milliseconds()

	for _, l := range res.Limits {
		g.Completeness.Limit(l)
	}
	stats(g)
	g.Stats.AnalysisMS = opt.Analysis.Milliseconds()
	if n := g.Stats.EdgesByConf[model.Unresolved]; n > 0 {
		g.Completeness.Limit(fmt.Sprintf("%d call(s) could not be resolved statically; they point to explicit unresolved nodes", n))
	}
	g.Completeness.Done()
	return g, nil
}

func mergeEdges(res *analyzer.Result, maxSites int) []*model.Edge {
	type site struct {
		File string `json:"file"`
		Line int    `json:"line"`
		Conf string `json:"confidence"`
		Note string `json:"note,omitempty"`
	}
	merged := map[string]*model.Edge{}
	sites := map[string][]site{}
	var order []string
	for _, e := range res.Edges {
		if res.Nodes[e.Source] == nil || res.Nodes[e.Target] == nil {
			continue
		}
		id := model.EdgeID(e.Type, e.Source, e.Target)
		s := site{Conf: e.Confidence, Note: e.Note}
		if e.Evidence != nil {
			s.File, s.Line = e.Evidence.File, e.Evidence.Line
		}
		m, ok := merged[id]
		if !ok {
			c := *e
			c.ID = id
			c.Weight = 1
			merged[id] = &c
			order = append(order, id)
			sites[id] = []site{s}
			continue
		}
		m.Weight++
		sites[id] = append(sites[id], s)
		if confRank[e.Confidence] > confRank[m.Confidence] {
			m.Confidence = e.Confidence
		}
		if m.Note == "" {
			m.Note = e.Note
		}
	}
	out := make([]*model.Edge, 0, len(order))
	for _, id := range order {
		e := merged[id]
		ss := sites[id]
		sort.SliceStable(ss, func(i, j int) bool {
			if ss[i].File != ss[j].File {
				return ss[i].File < ss[j].File
			}
			return ss[i].Line < ss[j].Line
		})
		// Evidence is the first site in source order, so it does not depend
		// on the order relations were found.
		if e.Evidence != nil && len(ss) > 0 && ss[0].File != "" {
			ev := *e.Evidence
			ev.File, ev.Line, ev.Confidence = ss[0].File, ss[0].Line, ss[0].Conf
			e.Evidence = &ev
		} else if e.Evidence != nil {
			ev := *e.Evidence
			ev.Confidence = e.Confidence
			e.Evidence = &ev
		}
		if len(ss) > 1 && e.Type != model.EdgeImports {
			if len(ss) > maxSites {
				ss = ss[:maxSites]
			}
			if e.Metadata == nil {
				e.Metadata = map[string]any{}
			} else {
				cp := make(map[string]any, len(e.Metadata)+1)
				for k, v := range e.Metadata {
					cp[k] = v
				}
				e.Metadata = cp
			}
			e.Metadata["sites"] = ss
		}
		out = append(out, e)
	}
	return out
}

func degrees(g *model.Graph) {
	idx := g.Index()
	nbr := map[string]map[string]bool{}
	for _, e := range g.Edges {
		if structural(e.Type) {
			continue
		}
		idx[e.Source].OutDegree++
		idx[e.Target].InDegree++
		for _, p := range [][2]string{{e.Source, e.Target}, {e.Target, e.Source}} {
			if nbr[p[0]] == nil {
				nbr[p[0]] = map[string]bool{}
			}
			nbr[p[0]][p[1]] = true
		}
	}
	for _, n := range g.Nodes {
		n.Degree = len(nbr[n.ID])
	}
}

func isSymbol(n *model.Node) bool {
	switch n.Type {
	case model.KindFunc, model.KindMethod, model.KindClosure, model.KindStruct, model.KindInterface, model.KindType, model.KindRoute, model.KindDB:
		return !n.External
	}
	return false
}

var confWeight = map[string]float64{model.Confirmed: 1, model.Likely: 0.8, model.Possible: 0.4}

// communities runs Louvain twice: on module packages and on module symbols.
// Package communities are numbered first; symbol communities continue the
// numbering, so a community ID is unique in the dataset.
func communities(g *model.Graph) {
	idx := g.Index()

	// Package level.
	var pkgs []*model.Node
	pi := map[string]int{}
	for _, n := range g.Nodes {
		if n.Type == model.KindPackage && !n.External {
			pi[n.ID] = len(pkgs)
			pkgs = append(pkgs, n)
		}
	}
	pw := map[[2]int]float64{}
	cross := map[[2]int]int{}
	for _, e := range g.Edges {
		if structural(e.Type) || e.Confidence == model.Unresolved {
			continue
		}
		a, b := idx[e.Source].Group, idx[e.Target].Group
		ia, okA := pi[a]
		ib, okB := pi[b]
		if !okA || !okB || ia == ib {
			continue
		}
		key := [2]int{min(ia, ib), max(ia, ib)}
		if e.Type == model.EdgeImports {
			pw[key] += 1
		} else {
			cross[key] += e.Weight
		}
	}
	for key, c := range cross {
		pw[key] += math.Log2(1 + float64(c))
	}
	pcomm, pq := Louvain(len(pkgs), sortedWEdges(pw))
	ncomm := 0
	for i, n := range pkgs {
		n.Community = pcomm[i]
		ncomm = max(ncomm, pcomm[i]+1)
	}
	g.Stats.Modularity = pq
	g.Communities = append(g.Communities, describe(pkgs, pcomm, "package", func(n *model.Node) string { return path.Base(n.Name) })...)
	for _, n := range g.Nodes {
		// Files are drawn with their package's community.
		if p := idx[n.Group]; p != nil && n.Type == model.KindFile {
			n.Community = p.Community
		}
	}

	// Symbol level.
	var syms []*model.Node
	si := map[string]int{}
	for _, n := range g.Nodes {
		if isSymbol(n) {
			si[n.ID] = len(syms)
			syms = append(syms, n)
		}
	}
	sw := map[[2]int]float64{}
	for _, e := range g.Edges {
		w := confWeight[e.Confidence]
		if w == 0 {
			continue
		}
		if structural(e.Type) {
			// A closure belongs with its parent function.
			if idx[e.Target].Type != model.KindClosure {
				continue
			}
		}
		a, okA := si[e.Source]
		b, okB := si[e.Target]
		if !okA || !okB || a == b {
			continue
		}
		key := [2]int{min(a, b), max(a, b)}
		sw[key] += w * (1 + math.Log2(float64(max(e.Weight, 1))))
	}
	scomm, _ := Louvain(len(syms), sortedWEdges(sw))
	for i := range scomm {
		scomm[i] += ncomm
	}
	for i, n := range syms {
		n.Community = scomm[i]
	}
	g.Communities = append(g.Communities, describe(syms, scomm, "symbol", func(n *model.Node) string { return n.Name })...)
	// Package communities report how many symbols they hold.
	for i := range g.Communities {
		c := &g.Communities[i]
		if c.Level != "package" {
			continue
		}
		for _, n := range g.Nodes {
			if isSymbol(n) {
				if p := idx[n.Group]; p != nil && p.Community == c.ID {
					c.Members++
				}
			}
		}
	}
}

func sortedWEdges(m map[[2]int]float64) []WEdge {
	keys := make([][2]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] != keys[j][0] {
			return keys[i][0] < keys[j][0]
		}
		return keys[i][1] < keys[j][1]
	})
	out := make([]WEdge, 0, len(keys))
	for _, k := range keys {
		out = append(out, WEdge{k[0], k[1], m[k]})
	}
	return out
}

// describe lists communities with the name of their most connected member
// (ties by ID) as label. Singletons are listed too: they are isolated nodes.
func describe(nodes []*model.Node, comm []int, level string, label func(*model.Node) string) []model.Community {
	best := map[int]*model.Node{}
	size := map[int]int{}
	for i, n := range nodes {
		c := comm[i]
		size[c]++
		if b := best[c]; b == nil || n.Degree > b.Degree || n.Degree == b.Degree && n.ID < b.ID {
			best[c] = n
		}
	}
	ids := make([]int, 0, len(size))
	for c := range size {
		ids = append(ids, c)
	}
	sort.Ints(ids)
	out := make([]model.Community, 0, len(ids))
	for _, c := range ids {
		out = append(out, model.Community{ID: c, Level: level, Label: label(best[c]), Size: size[c], Members: size[c]})
	}
	return out
}

func stats(g *model.Graph) {
	s := &g.Stats
	s.NodesByType, s.EdgesByType, s.EdgesByConf = map[string]int{}, map[string]int{}, map[string]int{}
	s.Modules = len(g.Modules)
	for _, n := range g.Nodes {
		s.NodesByType[n.Type]++
		switch n.Type {
		case model.KindPackage:
			if n.External {
				s.ExternalPackages++
			} else {
				s.Packages++
			}
		case model.KindFile:
			s.Files++
		case model.KindFunc, model.KindMethod, model.KindClosure:
			s.Functions++
		case model.KindStruct, model.KindInterface, model.KindType:
			s.Types++
		}
	}
	for _, e := range g.Edges {
		s.EdgesByType[e.Type]++
		if e.Type == model.EdgeCalls || e.Type == model.EdgeGo || e.Type == model.EdgeDefer || e.Type == model.EdgeHandles {
			s.EdgesByConf[e.Confidence]++
		}
	}
	s.Nodes, s.Edges = len(g.Nodes), len(g.Edges)
	s.Communities = len(g.Communities)
}

// Layout positions every node: packages by a force layout of the package
// graph, members of a package in a disc around it, so a symbol keeps its
// place whether the view shows packages, files or functions.
func Layout(g *model.Graph, stop func() bool) {
	idx := g.Index()
	var pkgs []*model.Node
	pi := map[string]int{}
	for _, n := range g.Nodes {
		if n.Type == model.KindPackage {
			pi[n.ID] = len(pkgs)
			pkgs = append(pkgs, n)
		}
	}
	members := make([][]*model.Node, len(pkgs))
	var loose []*model.Node // nodes without a package (modules)
	for _, n := range g.Nodes {
		if n.Type == model.KindPackage {
			continue
		}
		if i, ok := pi[n.Group]; ok {
			members[i] = append(members[i], n)
		} else {
			loose = append(loose, n)
		}
	}
	const unit = 10.0
	radius := make([]float64, len(pkgs))
	for i := range pkgs {
		radius[i] = unit * (1.5 + math.Sqrt(float64(len(members[i]))))
	}

	// Package graph: imports and aggregated cross-package relations.
	lw := map[[2]int]float64{}
	for _, e := range g.Edges {
		if structural(e.Type) {
			continue
		}
		a, okA := pi[idx[e.Source].Group]
		b, okB := pi[idx[e.Target].Group]
		if !okA || !okB || a == b {
			continue
		}
		key := [2]int{min(a, b), max(a, b)}
		if e.Type == model.EdgeImports {
			lw[key] += 1
		} else {
			lw[key] += 0.1
		}
	}
	var links []layout.Link
	for _, e := range sortedWEdges(lw) {
		links = append(links, layout.Link{A: e.A, B: e.B, W: math.Min(e.W, 3)})
	}
	// Start from a deterministic arrangement: communities around a circle,
	// then by ID; external packages on an outer ring.
	order := make([]int, len(pkgs))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		na, nb := pkgs[order[a]], pkgs[order[b]]
		if na.External != nb.External {
			return !na.External
		}
		if na.Community != nb.Community {
			return na.Community < nb.Community
		}
		return na.ID < nb.ID
	})
	pos := make([]layout.Point, len(pkgs))
	var internal, external int
	for _, n := range pkgs {
		if n.External {
			external++
		} else {
			internal++
		}
	}
	ki, ke := 0, 0
	for _, i := range order {
		if pkgs[i].External {
			a := 2 * math.Pi * float64(ke) / float64(max(external, 1))
			pos[i] = layout.Point{X: 2 * math.Cos(a), Y: 2 * math.Sin(a)}
			ke++
		} else {
			pos[i] = layout.Sunflower(ki, internal, 1)
			ki++
		}
	}
	layout.Force(pos, links, 0.15, 400, 0.05, 0, stop)
	// Scale so neighbouring discs do not overlap, then separate the rest.
	maxR := 0.0
	for _, r := range radius {
		maxR = math.Max(maxR, r)
	}
	minD := math.Inf(1)
	for i := range pos {
		for j := i + 1; j < len(pos) && j < i+200; j++ {
			minD = math.Min(minD, math.Hypot(pos[i].X-pos[j].X, pos[i].Y-pos[j].Y))
		}
	}
	scale := 1.0
	if !math.IsInf(minD, 1) && minD > 1e-6 {
		scale = math.Min((2*unit*3)/minD, 1e6)
	}
	scale = math.Max(scale, maxR*2)
	for i := range pos {
		pos[i].X *= scale
		pos[i].Y *= scale
	}
	layout.Separate(pos, radius, unit, 200)
	layout.Center(pos)
	for i, n := range pkgs {
		n.X, n.Y = round(pos[i].X), round(pos[i].Y)
	}

	// Members of each package, with the edges inside it.
	inner := make([][]*model.Edge, len(pkgs))
	for _, e := range g.Edges {
		a, b := idx[e.Source].Group, idx[e.Target].Group
		if i, ok := pi[a]; ok && a == b && idx[e.Source].Type != model.KindPackage && idx[e.Target].Type != model.KindPackage {
			inner[i] = append(inner[i], e)
		}
	}
	for i, ms := range members {
		if stop != nil && stop() {
			return
		}
		placeMembers(inner[i], pkgs[i], ms, radius[i]-unit*0.6, stop)
	}
	// Modules sit at the centroid of their packages.
	for _, n := range loose {
		var sx, sy float64
		var c int
		for _, p := range pkgs {
			if p.Module == n.Module && !p.External {
				sx, sy, c = sx+p.X, sy+p.Y, c+1
			}
		}
		if c > 0 {
			n.X, n.Y = round(sx/float64(c)), round(sy/float64(c))
		}
	}
}

func round(v float64) float64 { return math.Round(v*100) / 100 }

// placeMembers lays the members of one package in a disc of radius r around
// it. Members start on a sunflower spiral ordered by file, line and symbol
// community, then a bounded force layout pulls related members together.
func placeMembers(edges []*model.Edge, pkg *model.Node, ms []*model.Node, r float64, stop func() bool) {
	if len(ms) == 0 {
		return
	}
	sort.SliceStable(ms, func(a, b int) bool {
		x, y := ms[a], ms[b]
		if x.Community != y.Community {
			return x.Community < y.Community
		}
		if x.File != y.File {
			return x.File < y.File
		}
		if x.Line != y.Line {
			return x.Line < y.Line
		}
		return x.ID < y.ID
	})
	mi := make(map[string]int, len(ms))
	pos := make([]layout.Point, len(ms))
	for i, n := range ms {
		mi[n.ID] = i
		// Index 0 of the spiral is the centre, where the package node is.
		pos[i] = layout.Sunflower(i+1, len(ms)+1, r)
	}
	lw := map[[2]int]float64{}
	for _, e := range edges {
		a, okA := mi[e.Source]
		b, okB := mi[e.Target]
		if !okA || !okB || a == b {
			continue
		}
		w := 0.3
		if structural(e.Type) {
			w = 0.6 // file → declaration keeps declarations near their file
		}
		lw[[2]int{min(a, b), max(a, b)}] += w
	}
	var links []layout.Link
	for _, e := range sortedWEdges(lw) {
		links = append(links, layout.Link{A: e.A, B: e.B, W: math.Min(e.W, 2)})
	}
	k := r * 0.9 / math.Sqrt(float64(len(ms))+1)
	layout.Force(pos, links, k, 120, 0.06, r, stop)
	for i, n := range ms {
		n.X, n.Y = round(pkg.X+pos[i].X), round(pkg.Y+pos[i].Y)
	}
}
