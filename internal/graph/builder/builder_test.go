package builder

import (
	"fmt"
	"math"
	"testing"

	"github.com/athos/gitview/internal/graph/model"
)

func clique(off, n int) []WEdge {
	var out []WEdge
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			out = append(out, WEdge{off + i, off + j, 1})
		}
	}
	return out
}

func TestLouvainSeparatesCliques(t *testing.T) {
	// Three 5-cliques joined in a chain by single edges, plus an isolated node.
	var e []WEdge
	for k := 0; k < 3; k++ {
		e = append(e, clique(k*5, 5)...)
	}
	e = append(e, WEdge{4, 5, 1}, WEdge{9, 10, 1})
	comm, q := Louvain(16, e)
	for k := 0; k < 3; k++ {
		for i := 1; i < 5; i++ {
			if comm[k*5+i] != comm[k*5] {
				t.Fatalf("clique %d split: %v", k, comm)
			}
		}
	}
	if comm[0] == comm[5] || comm[5] == comm[10] || comm[15] == comm[0] {
		t.Fatalf("cliques merged: %v", comm)
	}
	if q < 0.5 {
		t.Errorf("modularity = %.3f", q)
	}
	if comm[15] != 3 {
		t.Errorf("isolated node is the smallest community, got %d", comm[15])
	}
	again, _ := Louvain(16, e)
	if fmt.Sprint(again) != fmt.Sprint(comm) {
		t.Fatal("Louvain is not deterministic")
	}
}

func TestLouvainEmpty(t *testing.T) {
	comm, q := Louvain(3, nil)
	if fmt.Sprint(comm) != "[0 1 2]" || q != 0 {
		t.Fatalf("got %v %v", comm, q)
	}
}

// synthetic builds a graph of p packages with f functions each; each function
// calls a few functions of its package and, sometimes, of the next package.
func synthetic(p, f int) *model.Graph {
	g := &model.Graph{Schema: model.Schema}
	for i := 0; i < p; i++ {
		pkg := fmt.Sprintf("package:m/p%03d", i)
		g.Nodes = append(g.Nodes, &model.Node{ID: pkg, Type: model.KindPackage, Group: pkg, Community: -1})
		if i > 0 {
			g.Edges = append(g.Edges, &model.Edge{ID: fmt.Sprint(i, "imp"), Source: pkg, Target: fmt.Sprintf("package:m/p%03d", i-1), Type: model.EdgeImports, Confidence: model.Confirmed, Weight: 1})
		}
		for j := 0; j < f; j++ {
			id := fmt.Sprintf("func:m/p%03d.F%04d", i, j)
			g.Nodes = append(g.Nodes, &model.Node{ID: id, Type: model.KindFunc, Group: pkg, File: fmt.Sprintf("p%03d/f.go", i), Line: j + 1, Community: -1})
			for k := 1; k <= 3; k++ {
				to := fmt.Sprintf("func:m/p%03d.F%04d", i, (j*7+k)%f)
				g.Edges = append(g.Edges, &model.Edge{ID: id + to, Source: id, Target: to, Type: model.EdgeCalls, Confidence: model.Confirmed, Weight: 1})
			}
			if j%10 == 0 && i+1 < p {
				to := fmt.Sprintf("func:m/p%03d.F%04d", i+1, j)
				g.Edges = append(g.Edges, &model.Edge{ID: id + to, Source: id, Target: to, Type: model.EdgeCalls, Confidence: model.Possible, Weight: 1})
			}
		}
	}
	g.Sort()
	return g
}

func TestLayoutDeterministicAndFinite(t *testing.T) {
	a, b := synthetic(12, 40), synthetic(12, 40)
	for _, g := range []*model.Graph{a, b} {
		degrees(g)
		communities(g)
		Layout(g, nil)
	}
	seen := map[[2]float64]string{}
	for i, n := range a.Nodes {
		if math.IsNaN(n.X) || math.IsNaN(n.Y) || math.IsInf(n.X, 0) || math.IsInf(n.Y, 0) {
			t.Fatalf("%s at %v,%v", n.ID, n.X, n.Y)
		}
		if n.X != b.Nodes[i].X || n.Y != b.Nodes[i].Y {
			t.Fatalf("%s: %v,%v vs %v,%v", n.ID, n.X, n.Y, b.Nodes[i].X, b.Nodes[i].Y)
		}
		k := [2]float64{n.X, n.Y}
		if other, dup := seen[k]; dup {
			t.Errorf("%s and %s share a position", n.ID, other)
		}
		seen[k] = n.ID
	}
	// Members stay closer to their own package than to any other package.
	idx := a.Index()
	for _, n := range a.Nodes {
		if n.Type != model.KindFunc {
			continue
		}
		own := idx[n.Group]
		d := math.Hypot(n.X-own.X, n.Y-own.Y)
		for _, p := range a.Nodes {
			if p.Type == model.KindPackage && p.ID != own.ID && math.Hypot(n.X-p.X, n.Y-p.Y) < d {
				t.Fatalf("%s is closer to %s than to its package", n.ID, p.ID)
			}
		}
	}
}

func benchGraph(b *testing.B, p, f int) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		g := synthetic(p, f)
		degrees(g)
		communities(g)
		Layout(g, nil)
		if i == 0 {
			b.ReportMetric(float64(len(g.Nodes)), "nodes")
			b.ReportMetric(float64(len(g.Edges)), "edges")
		}
	}
}

func BenchmarkBuild1k(b *testing.B)  { benchGraph(b, 20, 50) }
func BenchmarkBuild5k(b *testing.B)  { benchGraph(b, 50, 100) }
func BenchmarkBuild20k(b *testing.B) { benchGraph(b, 100, 200) }

func BenchmarkLouvain20k(b *testing.B) {
	g := synthetic(100, 200)
	idx := map[string]int{}
	for i, n := range g.Nodes {
		idx[n.ID] = i
	}
	var e []WEdge
	for _, x := range g.Edges {
		e = append(e, WEdge{idx[x.Source], idx[x.Target], 1})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Louvain(len(g.Nodes), e)
	}
}
