package perf

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"time"

	"github.com/athos/gitview/internal/evidence"
	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/inspect/run"
)

// BenchCapture is the outcome of running benchmarks with profiles.
type BenchCapture struct {
	Command string
	Dir     string
	Files   BenchFiles
	Result  run.Result
	Run     *BenchRun
	CPU     *Report
	Mem     *Report
	Errors  []string
}

// RunBench executes the benchmarks of pkg. The caller must have obtained
// the user's consent: benchmarks run code from the repository.
func RunBench(ctx context.Context, progs []*flow.Program, moduleDir, pkg string, opt BenchOptions, timeout time.Duration) *BenchCapture {
	spec, files := BenchSpec(moduleDir, pkg, opt)
	spec.Timeout = timeout
	c := &BenchCapture{Command: spec.CommandLine(), Dir: moduleDir, Files: files}
	c.Result = run.Run(ctx, spec)
	if files.Binary != "" {
		os.Remove(files.Binary)
	}
	if err := os.WriteFile(files.Output, c.Result.Stdout, 0o644); err != nil {
		c.Errors = append(c.Errors, err.Error())
	}
	if c.Result.Err != nil {
		c.Errors = append(c.Errors, c.Result.Err.Error())
		return c
	}
	if c.Result.ExitCode != 0 {
		c.Errors = append(c.Errors, fmt.Sprintf("go test exited with code %d", c.Result.ExitCode))
	}
	captured := evidence.Artifact{Command: c.Command, CapturedAt: time.Now()}
	if r, err := ParseBench(bytes.NewReader(c.Result.Stdout)); err == nil {
		c.Run = r
		c.Run.Artifact = captured
		c.Run.Artifact.Kind, c.Run.Artifact.Name, c.Run.Artifact.Path = "bench", "bench.txt", files.Output
	} else {
		c.Errors = append(c.Errors, err.Error())
	}
	for _, p := range []struct {
		path   string
		sample string
		dst    **Report
	}{{files.CPU, "", &c.CPU}, {files.Mem, "alloc_space", &c.Mem}} {
		if p.path == "" {
			continue
		}
		if _, err := os.Stat(p.path); err != nil {
			continue
		}
		r, err := Load(p.path, progs, Options{SampleType: p.sample})
		if err != nil {
			c.Errors = append(c.Errors, err.Error())
			continue
		}
		r.Artifact.Imported, r.Artifact.Command, r.Artifact.CapturedAt = false, c.Command, captured.CapturedAt
		*p.dst = r
	}
	return c
}
