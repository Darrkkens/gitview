package audit

import (
	"sort"
	"strings"
)

// DiffItem is a finding in a comparison.
type DiffItem struct {
	ID       string   `json:"id"`
	RuleID   string   `json:"ruleId"`
	Title    string   `json:"title"`
	Severity string   `json:"severity"`
	File     string   `json:"file,omitempty"`
	Line     int      `json:"line,omitempty"`
	Changes  []string `json:"changes,omitempty"` // for changed findings
	Reason   string   `json:"reason,omitempty"`  // why a missing finding is not counted as resolved
	Base     *Finding `json:"base,omitempty"`    // the finding as it was (its path, for the comparison)
	Head     *Finding `json:"head,omitempty"`
}

// Diff compares two audits of the same repository.
type Diff struct {
	BaseCommit  string     `json:"baseCommit"`
	HeadCommit  string     `json:"headCommit"`
	BaseStatus  string     `json:"baseStatus"`
	HeadStatus  string     `json:"headStatus"`
	New         []DiffItem `json:"new"`
	Resolved    []DiffItem `json:"resolved"`
	Unchanged   []DiffItem `json:"unchanged"`
	Changed     []DiffItem `json:"changed"`
	Unconfirmed []DiffItem `json:"unconfirmed"` // gone from head, but the head analysis cannot vouch for it
}

func item(f *Finding) DiffItem {
	return DiffItem{ID: f.ID, RuleID: f.RuleID, Title: f.Title, Severity: f.Severity, File: f.File, Line: f.Line}
}

// Compare correlates findings by ID. A finding missing from head counts as
// resolved only when head ran its rule, analysed its file completely and
// has no finding of the same rule in the same function (which would mean
// the identity changed rather than the problem).
func Compare(base, head *Audit) *Diff {
	d := &Diff{BaseCommit: base.Commit, HeadCommit: head.Commit, BaseStatus: string(base.Status.Status), HeadStatus: string(head.Status.Status)}
	hb := map[string]*Finding{}
	for _, f := range head.Findings {
		hb[f.ID] = f
	}
	bb := map[string]*Finding{}
	for _, f := range base.Findings {
		bb[f.ID] = f
	}
	ran := map[string]bool{}
	for _, r := range head.Rules {
		ran[r.ID] = r.Enabled
	}
	sameFn := map[string]bool{}
	for _, f := range head.Findings {
		sameFn[f.RuleID+"\x00"+f.SymbolID] = true
	}
	for _, f := range base.Findings {
		h, ok := hb[f.ID]
		if ok {
			it := item(h)
			it.Base, it.Head = f, h
			if ch := changes(f, h); len(ch) > 0 {
				it.Changes = ch
				d.Changed = append(d.Changed, it)
			} else {
				d.Unchanged = append(d.Unchanged, it)
			}
			continue
		}
		it := item(f)
		it.Base = f
		switch {
		case !ran[f.RuleID]:
			it.Reason = "the rule did not run in the newer audit"
		case f.SymbolID != "" && sameFn[f.RuleID+"\x00"+f.SymbolID]:
			it.Reason = "the same rule still reports the same function: the finding may have changed identity"
		case incompleteFor(head, f):
			it.Reason = "the newer analysis is incomplete where the finding was"
		case f.RuleID == "GO-DEP-001" && !head.Dependencies.Checked:
			it.Reason = "dependencies were not checked in the newer audit"
		}
		if it.Reason != "" {
			d.Unconfirmed = append(d.Unconfirmed, it)
		} else {
			d.Resolved = append(d.Resolved, it)
		}
	}
	for _, f := range head.Findings {
		if _, ok := bb[f.ID]; !ok {
			it := item(f)
			it.Head = f
			d.New = append(d.New, it)
		}
	}
	for _, l := range [][]DiffItem{d.New, d.Resolved, d.Unchanged, d.Changed, d.Unconfirmed} {
		sort.Slice(l, func(i, j int) bool {
			return sevRank[l[i].Severity] > sevRank[l[j].Severity] || sevRank[l[i].Severity] == sevRank[l[j].Severity] && l[i].ID < l[j].ID
		})
	}
	return d
}

func changes(a, b *Finding) []string {
	var out []string
	if a.Severity != b.Severity {
		out = append(out, "severity "+a.Severity+" → "+b.Severity)
	}
	if a.Verdict != b.Verdict {
		out = append(out, "verdict "+a.Verdict+" → "+b.Verdict)
	}
	if a.Confidence != b.Confidence {
		out = append(out, "confidence "+a.Confidence+" → "+b.Confidence)
	}
	if a.CodeHash != b.CodeHash {
		out = append(out, "code changed")
	}
	if pathText(a) != pathText(b) {
		out = append(out, "execution paths changed")
	}
	return out
}

func pathText(f *Finding) string {
	var b strings.Builder
	for _, r := range f.Routes {
		b.WriteString(r.Key)
		for _, s := range r.Path {
			b.WriteString(">" + s.ID)
		}
		b.WriteString(";")
	}
	return b.String()
}

// incompleteFor: packages that failed to load or a partial graph near the
// finding's file make its absence unreliable.
func incompleteFor(a *Audit, f *Finding) bool {
	dir := f.File
	if i := strings.LastIndex(dir, "/"); i >= 0 {
		dir = dir[:i]
	}
	for _, r := range a.Status.Reasons {
		if strings.HasPrefix(r, "not analysed:") && (dir != "" && strings.Contains(r, dir) || f.File != "" && strings.Contains(r, f.File)) {
			return true
		}
	}
	return false
}
