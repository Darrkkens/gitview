package xtrace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/inspect/run"
	"golang.org/x/tools/go/ssa"
)

type CaptureOptions struct {
	Pkg        string   // package pattern relative to the module, e.g. ./service
	Run        string   // -run regexp (default: all tests of the package)
	Instrument []string // symbol IDs to wrap in trace regions (optional)
	OutDir     string   // outside the repository
	Timeout    time.Duration
}

// Plan is a capture ready to be shown to the user before it runs.
type Plan struct {
	Spec         run.Spec
	TraceFile    string
	Overlay      string            // overlay JSON, when instrumenting
	Replaced     map[string]string // original file → instrumented copy
	Instrumented []string          // symbol IDs actually instrumented
	Skipped      []string          // symbols that could not be instrumented, with the reason

	files map[string][]byte // written by RunCapture, after consent
}

const (
	traceAlias = "__gitview_trace"
	ctxAlias   = "__gitview_ctx"
)

// PlanCapture builds the go test command that records an execution trace,
// with optional temporary instrumentation through -overlay. Nothing runs.
func PlanCapture(p *flow.Program, opt CaptureOptions) (*Plan, error) {
	if opt.Pkg == "" {
		return nil, errors.New("no package to test")
	}
	if opt.OutDir == "" {
		return nil, errors.New("no output directory")
	}
	pl := &Plan{TraceFile: filepath.Join(opt.OutDir, "trace.out"), Replaced: map[string]string{}, files: map[string][]byte{}}
	args := []string{"go", "test", "-count=1"}
	if opt.Run != "" {
		args = append(args, "-run="+opt.Run)
	}
	args = append(args, "-trace="+pl.TraceFile, "-o", filepath.Join(opt.OutDir, "trace.test"))
	if len(opt.Instrument) > 0 {
		overlay, err := instrument(p, opt.Instrument, opt.OutDir, pl)
		if err != nil {
			return nil, err
		}
		if overlay != "" {
			pl.Overlay = overlay
			args = append(args, "-overlay="+overlay)
		}
	}
	args = append(args, opt.Pkg)
	pl.Spec = run.Spec{Args: args, Dir: p.Dir, Timeout: opt.Timeout}
	return pl, nil
}

// instrument prepares instrumented copies of the files declaring the given
// functions and the overlay mapping originals to copies. Nothing is written
// here: RunCapture writes them once the user agreed to run the tests.
func instrument(p *flow.Program, ids []string, outDir string, pl *Plan) (string, error) {
	type insert struct {
		off  int
		text string
	}
	type fileEdit struct {
		size    int
		header  int
		inserts []insert
	}
	byFile := map[string]*fileEdit{}
	var order []string
	for _, id := range ids {
		fn := p.FuncByID(id)
		if fn == nil {
			pl.Skipped = append(pl.Skipped, id+": not found in the module")
			continue
		}
		if fn.Parent() != nil {
			pl.Skipped = append(pl.Skipped, id+": closures are not instrumented")
			continue
		}
		decl, ok := fn.Syntax().(*ast.FuncDecl)
		if !ok || decl.Body == nil {
			pl.Skipped = append(pl.Skipped, id+": no Go body")
			continue
		}
		tf := p.Fset().File(decl.Pos())
		file := p.FileOf(fn, decl.Pos())
		if tf == nil || file == nil {
			pl.Skipped = append(pl.Skipped, id+": source file unavailable")
			continue
		}
		name := tf.Name()
		fe := byFile[name]
		if fe == nil {
			fe = &fileEdit{size: tf.Size(), header: tf.Offset(file.Name.End())}
			byFile[name] = fe
			order = append(order, name)
		}
		// Same line as the opening brace: line numbers stay unchanged.
		stmt := fmt.Sprintf(" defer %s.StartRegion(%s.Background(), %s).End();", traceAlias, ctxAlias, strconv.Quote(RegionPrefix+fn.String()))
		fe.inserts = append(fe.inserts, insert{tf.Offset(decl.Body.Lbrace) + 1, stmt})
		pl.Instrumented = append(pl.Instrumented, fn.String())
	}
	if len(byFile) == 0 {
		return "", nil
	}
	dir := filepath.Join(outDir, "overlay")
	replace := map[string]string{}
	for n, name := range order {
		fe := byFile[name]
		src, err := os.ReadFile(name)
		if err != nil {
			return "", err
		}
		// The offsets come from the analysed version of the file: refuse to
		// edit a file that changed since.
		if len(src) != fe.size {
			return "", fmt.Errorf("%s changed since it was analysed; reopen the inspector to analyse it again", filepath.Base(name))
		}
		for _, in := range fe.inserts {
			if in.off < 1 || in.off > len(src) || src[in.off-1] != '{' {
				return "", fmt.Errorf("%s changed since it was analysed; reopen the inspector to analyse it again", filepath.Base(name))
			}
		}
		ins := append(fe.inserts, insert{fe.header, fmt.Sprintf("; import (%s %q; %s %q)", traceAlias, "runtime/trace", ctxAlias, "context")})
		sort.Slice(ins, func(i, j int) bool { return ins[i].off > ins[j].off })
		out := append([]byte(nil), src...)
		for _, in := range ins {
			out = append(out[:in.off], append([]byte(in.text), out[in.off:]...)...)
		}
		dst := filepath.Join(dir, fmt.Sprintf("%d_%s", n+1, filepath.Base(name)))
		pl.files[dst] = out
		replace[name] = dst
		pl.Replaced[name] = dst
	}
	data, err := json.MarshalIndent(map[string]any{"Replace": replace}, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(outDir, "overlay.json")
	pl.files[path] = data
	return path, nil
}

// Contents returns the instrumented copy prepared for an original file.
func (pl *Plan) Contents(original string) []byte { return pl.files[pl.Replaced[original]] }

// Capture is the outcome of running a planned capture.
type Capture struct {
	Plan     *Plan
	Result   run.Result
	Timeline *Timeline
	Err      error
}

// RunCapture executes a plan. The caller must have the user's consent:
// tests run code from the repository.
func RunCapture(ctx context.Context, pl *Plan, progs []*flow.Program) *Capture {
	c := &Capture{Plan: pl}
	if err := os.MkdirAll(filepath.Dir(pl.TraceFile), 0o755); err != nil {
		c.Err = err
		return c
	}
	for path, data := range pl.files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			c.Err = err
			return c
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			c.Err = err
			return c
		}
	}
	c.Result = run.Run(ctx, pl.Spec)
	os.Remove(filepath.Join(filepath.Dir(pl.TraceFile), "trace.test"))
	if c.Result.Err != nil {
		c.Err = c.Result.Err
		return c
	}
	if _, err := os.Stat(pl.TraceFile); err != nil {
		c.Err = fmt.Errorf("go test did not write a trace (exit code %d)", c.Result.ExitCode)
		return c
	}
	t, err := Load(pl.TraceFile, progs, Limits{})
	if err != nil {
		c.Err = err
		return c
	}
	t.Artifact.Imported = false
	t.Artifact.Command = pl.Spec.CommandLine()
	t.Artifact.CapturedAt = time.Now()
	if c.Result.ExitCode != 0 {
		t.Notes = append(t.Notes, fmt.Sprintf("go test exited with code %d (tests failed or did not build); the trace covers what ran.", c.Result.ExitCode))
	}
	c.Timeline = t
	return c
}

// FuncsOfFile lists the instrumentable functions of a file (helper for UIs).
func FuncsOfFile(p *flow.Program, file string) []*ssa.Function {
	var out []*ssa.Function
	for _, fn := range p.Funcs() {
		if fn.Parent() != nil {
			continue
		}
		if f, _, _ := p.Range(fn); f == file {
			out = append(out, fn)
		}
	}
	return out
}
