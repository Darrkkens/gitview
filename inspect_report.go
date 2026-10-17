package main

import (
	"context"
	"os"
	"strings"

	"github.com/athos/gitview/internal/export"
	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/gitdiff"
	"github.com/athos/gitview/internal/inspect"
	"github.com/athos/gitview/internal/inspect/branch"
	"github.com/athos/gitview/internal/inspect/perf"
	"github.com/athos/gitview/internal/inspect/pgexplain"
	"github.com/athos/gitview/internal/inspect/xtrace"
)

// maxReportSymbols bounds how many functions a report embeds.
const maxReportSymbols = 40

// buildInspectData computes the Deep Inspector section of a report for the
// given functions, with the runtime data loaded in the session.
func buildInspectData(sess *inspect.Session, targets []*inspect.Resolved) *export.InspectData {
	d := &export.InspectData{}
	seen := map[string]bool{}
	for _, r := range targets {
		if seen[r.Ref.ID] {
			continue
		}
		seen[r.Ref.ID] = true
		if len(d.Symbols) >= maxReportSymbols {
			d.Notes = append(d.Notes, "Only the first 40 functions were included.")
			break
		}
		d.Symbols = append(d.Symbols, sess.Bundle(r, inspect.BundleOptions{DataFlow: true}))
	}
	// Copies whose commands do not reveal the local home directory.
	rt := sess.Snapshot()
	if rt.CPU != nil {
		c := *rt.CPU
		c.Artifact.Command = redactHome(c.Artifact.Command)
		d.CPU = &c
	}
	if rt.Mem != nil {
		c := *rt.Mem
		c.Artifact.Command = redactHome(c.Artifact.Command)
		d.Mem = &c
	}
	if rt.Escape != nil {
		c := *rt.Escape
		c.Artifact.Command = redactHome(c.Artifact.Command)
		d.Escape = &c
	}
	for _, b := range rt.Benches {
		c := *b
		c.Artifact.Command = redactHome(c.Artifact.Command)
		d.Benches = append(d.Benches, &c)
	}
	if n := len(rt.Benches); n >= 2 {
		d.Compare = perf.Compare(rt.Benches[n-2], rt.Benches[n-1])
	}
	if rt.Trace != nil {
		d.Trace = rt.Trace.Compact(40000, 4000)
		d.Trace.Artifact.Command = redactHome(d.Trace.Artifact.Command)
	}
	d.Plans = rt.Plans
	if len(d.Symbols) == 0 {
		d.Notes = append(d.Notes, "No function was selected and none was modified in the working tree.")
	}
	return d
}

// redactHome replaces the user's home directory in commands shown in
// reports (artifact paths live in the user cache directory).
func redactHome(s string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" && s != "" {
		return strings.ReplaceAll(s, home, "~")
	}
	return s
}

// gitdiffFile converts a working-tree change for export.
func gitdiffFile(f FileChange) gitdiff.File {
	return gitdiff.File{Path: f.Path, OldPath: f.OldPath, Status: f.Status, Added: f.Added,
		Deleted: f.Deleted, Binary: f.Binary, Raw: f.Diff}
}

// inspectSnapshot captures, on the UI goroutine, what an export needs from
// the inspector session: the loaded runtime data and the selected symbol.
type inspectSnapshot struct {
	cpu, mem *perf.Report
	benches  []*perf.BenchRun
	escape   *perf.EscapeReport
	trace    *xtrace.Timeline
	plans    []*pgexplain.Plan
	coverage *branch.Profile
	symbol   string
}

func (m *model) inspectSnapshot() inspectSnapshot {
	var s inspectSnapshot
	if sess := m.insp.sess; sess != nil {
		rt := sess.Snapshot()
		s.cpu, s.mem, s.escape, s.trace, s.coverage = rt.CPU, rt.Mem, rt.Escape, rt.Trace, rt.Coverage
		s.benches, s.plans = rt.Benches, rt.Plans
	}
	s.symbol = m.flow.rootID
	return s
}

// session creates an inspector session over the programs, with the runtime
// data loaded in the TUI.
func (s inspectSnapshot) session(root string, progs []*flow.Program) *inspect.Session {
	sess := inspect.NewSession(root, progs)
	sess.CPU, sess.Mem, sess.Escape, sess.Trace, sess.Coverage = s.cpu, s.mem, s.escape, s.trace, s.coverage
	for _, b := range s.benches {
		sess.AddBench(b)
	}
	for _, p := range s.plans {
		sess.AddPlan(p)
	}
	return sess
}

// targets are the functions a report inspects: the selected one, then the
// changed ones.
func (s inspectSnapshot) targets(sess *inspect.Session, changed map[string]map[int]bool) []*inspect.Resolved {
	var targets []*inspect.Resolved
	if s.symbol != "" {
		if r, err := sess.Resolve(s.symbol); err == nil {
			targets = append(targets, r)
		}
	}
	return append(targets, sess.Changed(changed)...)
}

// build analyses the repository's modules (cached) and computes the
// inspector section for the changed functions and the selected one.
func (s inspectSnapshot) build(ctx context.Context, root string, changed map[string]map[int]bool, cache *flow.Cache) *export.InspectData {
	progs, errs := loadRepoPrograms(ctx, root, cache)
	sess := s.session(root, progs)
	d := buildInspectData(sess, s.targets(sess, changed))
	d.Errors = append(d.Errors, errs...)
	return d
}
