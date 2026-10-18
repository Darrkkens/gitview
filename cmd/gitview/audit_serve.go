package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/athos/gitview/internal/audit"
	"github.com/athos/gitview/internal/export"
	gexport "github.com/athos/gitview/internal/graph/export"
	gmodel "github.com/athos/gitview/internal/graph/model"
)

func signalContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(ctx, os.Interrupt)
}

// securityData is the page data of the Security Journey (audit.Page): the
// findings with the snippets they point to (never whole files), the
// reviews, the comparison with the previous snapshot and the saved AI
// investigations. live marks a page that may call the local server.
func securityData(a *audit.Audit, store *audit.Store, root string, live bool) (json.RawMessage, error) {
	reviews, err := store.Reviews()
	if err != nil {
		return nil, err
	}
	var diff *audit.Diff
	if list, err := store.Snapshots(); err == nil && len(list) >= 2 {
		for i := len(list) - 1; i >= 0; i-- {
			prev, _, err := store.Load(list[i].ID)
			if err != nil || prev.Generated.Equal(a.Generated) || !prev.Generated.Before(a.Generated) {
				continue
			}
			diff = audit.Compare(prev, a)
			break
		}
	}
	inv := map[string]json.RawMessage{}
	for _, f := range a.Findings {
		var raw json.RawMessage
		if store.LatestInvestigation(f.ID, &raw) {
			inv[f.ID] = raw
		}
	}
	read := repoReader(root)
	page := audit.BuildPage(a, reviews, func(file string) ([]string, error) { return read(file) }, diff, inv)
	page.Live = live
	return json.Marshal(page)
}

// securityPage renders the offline Security Journey: the Repository Graph
// page with the audit embedded and, instead of whole source files, only
// the snippets the findings point to.
func securityPage(g *gmodel.Graph, a *audit.Audit, store *audit.Store, root string) ([]byte, error) {
	data, err := securityData(a, store, root, false)
	if err != nil {
		return nil, err
	}
	audit.Annotate(g, a)
	var buf bytes.Buffer
	err = gexport.HTML(&buf, g, gexport.HTMLOptions{NoSource: true, Security: data})
	return buf.Bytes(), err
}

func runAuditExport(ctx context.Context, stdout, stderr io.Writer, store *audit.Store, dir string, f auditFlags) int {
	if f.format != "html" && f.format != "json" {
		fmt.Fprintln(stderr, "gitview audit export: --format deve ser html ou json")
		return 2
	}
	out := f.output
	if out == "" && f.format == "html" {
		out = "gitview-security.html"
	}
	// Refuse before analysing, not after.
	if out != "" && !f.overwrite {
		if _, err := os.Stat(out); err == nil {
			fmt.Fprintf(stderr, "gitview audit export: %s já existe; use --overwrite\n", out)
			return 1
		}
	}
	a, g, err := auditScan(ctx, dir, f, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "gitview audit export:", err)
		return 1
	}
	if _, err := store.Save(a); err != nil {
		fmt.Fprintln(stderr, "gitview audit export: snapshot:", err)
		return 1
	}
	root, _ := repoRoot(dir)
	var data []byte
	if f.format == "json" {
		reviews, _ := store.Reviews()
		page := audit.BuildPage(a, reviews, repoReader(root), nil, nil)
		data, err = json.MarshalIndent(page, "", "  ")
		data = append(data, '\n')
	} else {
		data, err = securityPage(g, a, store, root)
	}
	if err != nil {
		fmt.Fprintln(stderr, "gitview audit export:", err)
		return 1
	}
	if out == "" {
		_, err = stdout.Write(data)
		if err != nil {
			return 1
		}
		return 0
	}
	if err := export.WriteFile(out, data, f.overwrite); err != nil {
		fmt.Fprintln(stderr, "gitview audit export:", err)
		return 1
	}
	fmt.Fprintf(stderr, "relatório gravado em %s (%s) · %d findings\n", out, humanBytes(int64(len(data))), len(a.Findings))
	return 0
}

// runAuditServe opens the Security Journey served locally: gitview serve
// on the Security view, with the same analysis and API.
func runAuditServe(ctx context.Context, stdout, stderr io.Writer, dir string, f auditFlags) int {
	return serveRepo(ctx, stdout, stderr, dir, serveOptions{audit: f, view: "security", watch: true})
}
