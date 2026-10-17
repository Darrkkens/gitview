// Package inspecttest loads the Go fixtures shared by the Deep Inspector
// tests (internal/inspect/testdata).
package inspecttest

import (
	"context"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/athos/gitview/internal/flow"
	"golang.org/x/tools/go/ssa"
)

var (
	once sync.Once
	prog *flow.Program
	err  error
)

// Dir returns the absolute path of the fixture module "deep".
func Dir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "testdata", "deep")
}

// TestdataDir returns the directory holding the fixture module and stubs.
func TestdataDir() string { return filepath.Dir(Dir()) }

// Deep loads (once) the fixture module with the repository root at the
// module directory, so files are reported as "service/client.go".
func Deep(t testing.TB) *flow.Program {
	t.Helper()
	once.Do(func() {
		dir, _ := filepath.Abs(Dir())
		prog, err = flow.Load(context.Background(), dir, dir)
	})
	if err != nil {
		t.Fatal(err)
	}
	return prog
}

// Func resolves exactly one function of the fixture.
func Func(t testing.TB, p *flow.Program, q string) *ssa.Function {
	t.Helper()
	fn, err := p.Resolve(q)
	if err != nil {
		t.Fatal(err)
	}
	return fn
}
