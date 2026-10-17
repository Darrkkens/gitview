package main

import (
	"context"
	"io"
	"path/filepath"

	"github.com/athos/gitview/internal/export"
	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/inspect"
	"github.com/athos/gitview/internal/study"
)

// loadRepoPrograms analyses (or takes from the cache) every Go module of
// the repository.
func loadRepoPrograms(ctx context.Context, root string, cache *flow.Cache) ([]*flow.Program, []string) {
	var progs []*flow.Program
	var failures []string
	for _, m := range flow.FindModules(root) {
		if ctx.Err() != nil {
			break
		}
		p, _, err := cache.Get(ctx, root, m)
		if err != nil {
			rel, _ := filepath.Rel(root, m)
			failures = append(failures, rel+": "+err.Error())
			continue
		}
		progs = append(progs, p)
	}
	return progs, failures
}

// studyInputs gathers what the report already computed for Study Mode.
func studyInputs(root string, progs []*flow.Program, fd *export.FlowData, changed map[string]map[int]bool, files []FileChange, tests *export.TestReport, rt inspect.Runtime) study.Inputs {
	in := study.Inputs{Root: root, Programs: progs, Changed: changed, Trace: rt.Trace, CPU: rt.CPU, Mem: rt.Mem,
		Benches: rt.Benches, Escape: rt.Escape, Plans: rt.Plans}
	if fd != nil {
		in.Graph = fd.Graph
	}
	for _, f := range files {
		in.Files = append(in.Files, study.FileChange{Path: f.Path, Status: f.Status, Added: f.Added, Deleted: f.Deleted})
	}
	if tests != nil {
		for _, c := range tests.Cases {
			if c.Kind == "test" {
				in.Tests = append(in.Tests, study.TestResult{Package: c.Package, Name: c.Name, Status: c.Status})
			}
		}
	}
	if rt.Coverage != nil {
		in.Coverage = rt.Coverage.Name
	}
	return in
}

// buildStudyReport fills the Code Flow, Deep Inspector and Study sections
// of a report from one analysis of the repository: the functions of the
// reading order are added to the inspector so that their CFG, Data Flow
// and SQL are in the report.
func buildStudyReport(ctx context.Context, rep *export.Report, root string, progs []*flow.Program, failures []string,
	changed map[string]map[int]bool, files []FileChange, sess *inspect.Session, targets []*inspect.Resolved) {
	rep.Flow = flowIndex(progs, changed, failures)
	in := studyInputs(root, progs, rep.Flow, changed, files, rep.Tests, sess.Snapshot())
	for _, id := range study.Plan(in) {
		if r, err := sess.Resolve(id); err == nil {
			targets = append(targets, r)
		}
	}
	rep.Inspect = buildInspectData(sess, targets)
	in.Bundles = rep.Inspect.Symbols
	rep.Study = study.Build(in)
}

// runStudy writes a report with the Study tab: "gitview inspect report
// --study --diff", with gitview-study.html as the default output (a later
// -o overrides it).
func runStudy(args []string, stdout, stderr io.Writer) int {
	return runInspect(append([]string{"report", "--study", "--diff", "-o", "gitview-study.html"}, args...), stdout, stderr)
}
