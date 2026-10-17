package main

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/athos/gitview/internal/export"
	"github.com/athos/gitview/internal/flow"
)

const maxReportedCompileErrors = 20

// buildFlowData analyses every Go module of the repository (reusing cached
// analyses whose sources did not change) and returns the call index for the
// report, with the functions modified in the working tree as starting points.
func buildFlowData(ctx context.Context, root string, changed map[string]map[int]bool, cache *flow.Cache) *export.FlowData {
	mods := flow.FindModules(root)
	if len(mods) == 0 {
		return &export.FlowData{Error: "no Go module (go.mod) in the repository"}
	}
	var progs []*flow.Program
	var failures []string
	for _, m := range mods {
		if ctx.Err() != nil {
			return &export.FlowData{Error: "analysis cancelled"}
		}
		rel, _ := filepath.Rel(root, m)
		p, _, err := cache.Get(ctx, root, m)
		if err != nil {
			failures = append(failures, rel+": "+err.Error())
			continue
		}
		progs = append(progs, p)
	}
	return flowIndex(progs, changed, failures)
}

// flowIndex builds the report's call index from analysed programs.
func flowIndex(progs []*flow.Program, changed map[string]map[int]bool, failures []string) *export.FlowData {
	fd := &export.FlowData{}
	g := &flow.Graph{}
	for _, p := range progs {
		fd.Modules = append(fd.Modules, p.Module)
		for _, e := range p.Errors {
			if len(fd.Errors) < maxReportedCompileErrors {
				fd.Errors = append(fd.Errors, e)
			}
		}
		idx := p.Index(flow.Options{Changed: changed})
		for _, c := range p.ChangedFunctions(changed) {
			idx.Roots = append(idx.Roots, flow.Root{ID: c.ID, Label: c.Name, Kind: "changed"})
		}
		g.Merge(idx)
	}
	fd.Errors = append(fd.Errors, failures...)
	if len(fd.Modules) == 0 {
		fd.Error = "Go analysis failed: " + strings.Join(failures, "; ")
		return fd
	}
	fd.Graph = g
	return fd
}

// changedLinesOf maps changed Go files to their changed new-side lines using
// the diffs already parsed for display.
func (m *model) changedLinesOf() map[string]map[int]bool {
	return changedFromFiles(m.files, func(f FileChange) *parsedDiff { return m.parsed[f.Path] })
}
