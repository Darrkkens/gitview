package export

import (
	"io"
	"time"

	"github.com/athos/gitview/internal/live/report"
	"github.com/athos/gitview/internal/webui"
)

// The HTML report is the web UI (internal/webui) with the report's data:
// the same app as `gitview serve`, offline, making no network request.

func renderHTML(r Report) ([]byte, error) {
	title := "GitView · " + r.Project
	if r.Scope != "" {
		title += " · " + r.Scope
	}
	return webui.Render(webui.Document{Kind: "report", Title: title, Repository: r.Project, Generated: r.GeneratedAt,
		Sections: map[string]any{webui.SectionReport: NewReportSection(r)}})
}

// RuntimeHTML writes the standalone report of a runtime capture: the web
// UI's Runtime view, offline. a is the capture; cmp, when given, compares it
// with an earlier capture (a is then the comparison's B).
func RuntimeHTML(w io.Writer, a *report.Analysis, cmp *report.Comparison) error {
	title := "GitView · Runtime Performance · " + a.ID
	if a.Label != "" {
		title = "GitView · Runtime Performance · " + a.Label
	}
	if cmp != nil {
		title = "GitView · Runtime comparison · " + cmp.AID + " → " + cmp.BID
	}
	return webui.Write(w, webui.Document{Kind: "runtime", Title: title, Generated: time.Now(),
		Sections: map[string]any{webui.SectionRuntime: NewRuntimeSection(a, cmp)}})
}
