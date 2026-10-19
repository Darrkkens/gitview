package export

import (
	"encoding/json"
	"io"
	"time"

	"github.com/athos/gitview/internal/graph/model"
	"github.com/athos/gitview/internal/webui"
)

// HTMLOptions configure the page.
type HTMLOptions struct {
	// Level is the initial exploration level of the Architecture canvas:
	// "" or "modules" (modules and their top-level directories), packages,
	// files, or functions/types/full (files expanded in the package view).
	Level    string
	MaxNodes int // elements drawn at once; beyond it the canvas asks before expanding
	MaxEdges int // relations drawn at once
	// NoSource leaves the Go source out of the page (no Source tab content).
	NoSource bool
	// Security is the CodeAudit page data (audit.Page as JSON): the page
	// then opens the Security Journey.
	Security json.RawMessage
	// Live is set when the page is served locally: the page may then call
	// that server (and only it) with the token.
	Live *webui.Live
	// Layout is a layout document (ParseLayout) the page opens with: the
	// card positions saved by gitview serve or exported from the page.
	Layout json.RawMessage
}

// GraphSection returns the page's graph section (the compact dataset the
// Architecture and HTTP views read) and, unless NoSource, the source bundle
// of the repository's Go files the graph describes.
func GraphSection(g *model.Graph, opt HTMLOptions) (map[string]any, SourceBundle, error) {
	if opt.MaxNodes <= 0 {
		opt.MaxNodes = 5000
	}
	if opt.MaxEdges <= 0 {
		opt.MaxEdges = 25000
	}
	c := compact(g, opt)
	var src SourceBundle
	if !opt.NoSource {
		var err error
		if src, err = sources(g); err != nil {
			return nil, src, err
		}
	}
	c["sources"] = map[string]any{"embedded": src.Encoded != "", "files": src.Files, "bytes": src.Bytes, "skipped": src.Skipped, "disabled": opt.NoSource}
	if len(opt.Layout) > 0 {
		c["layout"] = opt.Layout
	}
	return c, src, nil
}

// HTML writes the interactive page: the web UI with the whole dataset
// embedded (the views aggregate and filter it in the browser), plus, unless
// NoSource, the repository's Go files the graph describes, compressed.
func HTML(w io.Writer, g *model.Graph, opt HTMLOptions) error {
	c, src, err := GraphSection(g, opt)
	if err != nil {
		return err
	}
	d := webui.Document{Kind: "graph", Title: "GitView · " + g.Repository, Repository: g.Repository,
		Sections: map[string]any{webui.SectionGraph: c}, Sources: src.Encoded, Live: opt.Live}
	if t, err := time.Parse(time.RFC3339, g.Generated); err == nil {
		d.Generated = t
	}
	if len(opt.Security) > 0 {
		d.Kind, d.View = "security", "security"
		d.Title += " · Security Journey"
		d.Sections[webui.SectionSecurity] = opt.Security
	}
	return webui.Write(w, d)
}
