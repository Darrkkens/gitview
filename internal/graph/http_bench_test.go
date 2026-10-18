package graph_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/graph/httpapi"
	"github.com/athos/gitview/internal/graph/model"
)

func apiProgram(b *testing.B) *flow.Program {
	root, _ := filepath.Abs("testdata/api")
	p, err := flow.Load(context.Background(), root, root)
	if err != nil {
		b.Fatal(err)
	}
	return p
}

// BenchmarkLoadAndRouteDiscovery: type-checking, SSA, the call index and
// route discovery (they run together in flow.Load).
func BenchmarkLoadAndRouteDiscovery(b *testing.B) {
	root, _ := filepath.Abs("testdata/api")
	for i := 0; i < b.N; i++ {
		if _, err := flow.Load(context.Background(), root, root); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRouteList(b *testing.B) {
	p := apiProgram(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.Routes()
	}
}

func BenchmarkContracts(b *testing.B) {
	p := apiProgram(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		httpapi.Analyze(p, map[string]*model.Shape{}, httpapi.Options{})
	}
}

func BenchmarkJourneyFixture(b *testing.B) {
	g := apiDataset(b)
	ix := httpapi.NewIndex(g)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, r := range g.HTTP.Routes {
			ix.Journey(r.ID, 6, 5000)
		}
	}
}

// syntheticAPI is a dataset with routes, each handler calling a chain of
// services and repositories, plus unrelated functions that a route
// subgraph must not visit.
func syntheticAPI(routes, perRoute, noise int) *model.Graph {
	g := &model.Graph{Schema: model.Schema}
	add := func(id, typ string) { g.Nodes = append(g.Nodes, &model.Node{ID: id, Type: typ}) }
	edge := func(typ, s, t string) {
		g.Edges = append(g.Edges, &model.Edge{ID: model.EdgeID(typ, s, t), Source: s, Target: t, Type: typ, Confidence: model.Confirmed})
	}
	for i := 0; i < noise; i++ {
		add(fmt.Sprintf("func:noise.F%d", i), model.KindFunc)
		if i > 0 {
			edge(model.EdgeCalls, fmt.Sprintf("func:noise.F%d", i-1), fmt.Sprintf("func:noise.F%d", i))
		}
	}
	for r := 0; r < routes; r++ {
		rid := fmt.Sprintf("route:GET /r%d@routes.go:%d", r, r+1)
		add(rid, model.KindRoute)
		prev := rid
		for k := 0; k < perRoute; k++ {
			id := fmt.Sprintf("func:app.R%dF%d", r, k)
			add(id, model.KindFunc)
			if k == 0 {
				g.Edges = append(g.Edges, &model.Edge{ID: model.EdgeID(model.EdgeHandles, prev, id), Source: prev, Target: id, Type: model.EdgeHandles,
					Confidence: model.Confirmed, Metadata: map[string]any{"order": 1}})
			} else {
				edge(model.EdgeCalls, prev, id)
			}
			prev = id
		}
	}
	return g
}

func BenchmarkJourneySynthetic(b *testing.B) {
	for _, size := range []struct{ routes, perRoute, noise int }{{100, 20, 10000}, {1000, 20, 100000}} {
		g := syntheticAPI(size.routes, size.perRoute, size.noise)
		b.Run(fmt.Sprintf("%dnodes", len(g.Nodes)), func(b *testing.B) {
			ix := httpapi.NewIndex(g)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ix.Journey(fmt.Sprintf("route:GET /r%d@routes.go:%d", i%size.routes, i%size.routes+1), 6, 5000)
			}
		})
	}
}

func BenchmarkJourneyIndex(b *testing.B) {
	g := syntheticAPI(1000, 20, 100000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		httpapi.NewIndex(g)
	}
}
