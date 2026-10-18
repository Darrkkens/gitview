package export

import (
	"github.com/athos/gitview/internal/evidence"
	"github.com/athos/gitview/internal/graph/model"
)

// The page's dataset is the graph in a compact form: string tables for
// kinds, relation types and confidences, and node indices instead of IDs in
// edges. Nothing is dropped except the layout coordinates, which the page
// recomputes per view; the JSON export keeps them.

type table struct {
	idx  map[string]int
	list []string
}

func (t *table) id(s string) int {
	if i, ok := t.idx[s]; ok {
		return i
	}
	if t.idx == nil {
		t.idx = map[string]int{}
	}
	t.idx[s] = len(t.list)
	t.list = append(t.list, s)
	return t.idx[s]
}

// Positions of the node fields in the compact form, read by data.js.
const (
	cID = iota
	cKind
	cName
	cQualified
	cPackage
	cFile
	cLine
	cEndLine
	cSignature
	cExternal
	cParent
	cGroup
	cCommunity
	cDegree
	cMeta
)

func compact(g *model.Graph, opt HTMLOptions) map[string]any {
	var kinds, etypes, confs, esrc table
	pos := make(map[string]int, len(g.Nodes))
	for i, n := range g.Nodes {
		pos[n.ID] = i
	}
	ref := func(id string) int {
		if i, ok := pos[id]; ok && id != "" {
			return i
		}
		return -1
	}
	b2i := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}
	nodes := make([][]any, len(g.Nodes))
	for i, n := range g.Nodes {
		var meta any
		if len(n.Metadata) > 0 || len(n.Annotations) > 0 {
			m := map[string]any{}
			for k, v := range n.Metadata {
				m[k] = v
			}
			if len(n.Annotations) > 0 {
				m["annotations"] = n.Annotations
			}
			meta = m
		}
		nodes[i] = []any{cID: n.ID, cKind: kinds.id(n.Type), cName: n.Name, cQualified: n.Qualified, cPackage: n.Package, cFile: n.File,
			cLine: n.Line, cEndLine: n.EndLine, cSignature: n.Signature, cExternal: b2i(n.External), cParent: ref(n.Parent), cGroup: ref(n.Group),
			cCommunity: n.Community, cDegree: n.Degree, cMeta: meta}
	}
	edges := make([][]any, len(g.Edges))
	for i, e := range g.Edges {
		file, line, src := "", 0, ""
		if e.Evidence != nil {
			file, line, src = e.Evidence.File, e.Evidence.Line, e.Evidence.Source
		}
		var meta any
		if len(e.Metadata) > 0 || len(e.Annotations) > 0 {
			m := map[string]any{}
			for k, v := range e.Metadata {
				m[k] = v
			}
			if len(e.Annotations) > 0 {
				m["annotations"] = e.Annotations
			}
			meta = m
		}
		edges[i] = []any{pos[e.Source], pos[e.Target], etypes.id(e.Type), confs.id(e.Confidence), e.Weight, file, line, e.Note, meta, esrc.id(src)}
	}
	comms := make([][]any, len(g.Communities))
	for i, c := range g.Communities {
		comms[i] = []any{c.ID, c.Level, c.Label, c.Size, c.Members}
	}
	return map[string]any{
		"schema": g.Schema, "repository": g.Repository, "version": g.Version, "generated": g.Generated,
		"modules": g.Modules, "stats": g.Stats, "completeness": g.Completeness, "method": g.Method,
		"initial": map[string]any{"level": opt.Level, "maxNodes": opt.MaxNodes, "maxEdges": opt.MaxEdges},
		"kinds":   kinds.list, "etypes": etypes.list, "confs": confs.list, "esrc": esrc.list,
		"communities": comms, "nodes": nodes, "edges": edges,
		"http": slimHTTP(g.HTTP),
	}
}

// maxEvidence bounds the evidence the page embeds per element; the JSON
// export keeps all of it, with symbol IDs and evidence kinds.
const maxEvidence = 3

// pageEvidence is the part of an evidence entry the page shows.
type pageEvidence struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Details string `json:"details,omitempty"`
}

func slimEvidence(list []evidence.Evidence) []pageEvidence {
	out := make([]pageEvidence, 0, min(len(list), maxEvidence))
	for i, e := range list {
		if i == maxEvidence {
			break
		}
		out = append(out, pageEvidence{e.File, e.Line, e.Details})
	}
	return out
}

// slimHTTP copies the HTTP catalog with at most maxEvidence evidence
// entries per parameter, body and response (the total is kept), so large
// APIs stay quick to open.
func slimHTTP(h *model.HTTP) any {
	if h == nil {
		return nil
	}
	type param struct {
		model.Param
		Evidence []pageEvidence `json:"evidence"`
	}
	type body struct {
		*model.Body
		Evidence []pageEvidence `json:"evidence"`
	}
	type response struct {
		model.Response
		Evidence      []pageEvidence `json:"evidence"`
		EvidenceTotal int            `json:"evidenceTotal"`
	}
	type auth struct {
		*model.Auth
		Evidence []pageEvidence `json:"evidence"`
	}
	type route struct {
		*model.Route
		Params    []param    `json:"params,omitempty"`
		Body      *body      `json:"body,omitempty"`
		Responses []response `json:"responses,omitempty"`
		Auth      *auth      `json:"auth,omitempty"`
	}
	out := struct {
		*model.HTTP
		Routes []route `json:"routes"`
	}{HTTP: h}
	for _, r := range h.Routes {
		x := route{Route: r}
		for _, p := range r.Params {
			x.Params = append(x.Params, param{p, slimEvidence(p.Evidence)})
		}
		if r.Body != nil {
			x.Body = &body{r.Body, slimEvidence(r.Body.Evidence)}
		}
		for _, resp := range r.Responses {
			x.Responses = append(x.Responses, response{resp, slimEvidence(resp.Evidence), len(resp.Evidence)})
		}
		if r.Auth != nil {
			x.Auth = &auth{r.Auth, slimEvidence(r.Auth.Evidence)}
		}
		out.Routes = append(out.Routes, x)
	}
	return out
}
