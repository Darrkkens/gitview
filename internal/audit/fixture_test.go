package audit

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/graph/builder"
	"github.com/athos/gitview/internal/graph/model"
)

var (
	fxOnce  sync.Once
	fxProg  *flow.Program
	fxGraph *model.Graph
	fxErr   error
)

var fixedNow = func() time.Time { return time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC) }

// fixture loads testdata/app once: Code Flow, then the Repository Graph
// with its HTTP catalog, as the CLI does.
func fixture(t testing.TB) (*flow.Program, *model.Graph) {
	t.Helper()
	fxOnce.Do(func() {
		root, _ := filepath.Abs("testdata/app")
		fxProg, fxErr = flow.Load(context.Background(), root, root)
		if fxErr != nil {
			return
		}
		fxGraph, fxErr = builder.Build(context.Background(), []*flow.Program{fxProg}, builder.Options{Repository: "app", Root: root, Now: fixedNow})
	})
	if fxErr != nil {
		t.Fatal(fxErr)
	}
	return fxProg, fxGraph
}

func scanFixture(t testing.TB, opt Options) *Audit {
	t.Helper()
	p, g := fixture(t)
	if opt.Now == nil {
		opt.Now = fixedNow
	}
	root, _ := filepath.Abs("testdata/app")
	a, err := Scan(context.Background(), Input{Programs: []*flow.Program{p}, Graph: g, Root: root, Commit: "abc123"}, opt)
	if err != nil {
		t.Fatal(err)
	}
	return a
}
