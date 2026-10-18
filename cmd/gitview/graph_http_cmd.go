package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/athos/gitview/internal/graph/httpapi"
	gmodel "github.com/athos/gitview/internal/graph/model"
)

// printRoutes lists the HTTP catalog grouped like the HTML explorer.
func printRoutes(w io.Writer, g *gmodel.Graph) {
	if g.HTTP == nil || len(g.HTTP.Routes) == 0 {
		fmt.Fprintln(w, "Nenhuma rota HTTP encontrada (gin, chi, echo e net/http são reconhecidos).")
		return
	}
	h := g.HTTP
	groups := map[string][]*gmodel.Route{}
	var names []string
	for _, r := range h.Routes {
		if _, ok := groups[r.Group]; !ok {
			names = append(names, r.Group)
		}
		groups[r.Group] = append(groups[r.Group], r)
	}
	sort.Strings(names)
	partial := 0
	for _, r := range h.Routes {
		if r.Status != gmodel.StatusComplete {
			partial++
		}
	}
	base := ""
	if ex := h.Routes[0].Example; ex != nil {
		base = ex.BaseURL + " (" + ex.BaseURLSrc + ")"
	}
	fmt.Fprintf(w, "%d rotas · %d com contrato parcial · base URL %s\n", len(h.Routes), partial, base)
	for _, n := range names {
		fmt.Fprintf(w, "\n%s\n", n)
		for _, r := range groups[n] {
			handler := "não resolvido"
			if r.Handler != nil {
				handler = r.Handler.Name
			}
			var flags []string
			if r.Auth != nil && r.Auth.Recognized {
				if r.Auth.Scheme != "" {
					flags = append(flags, "auth:"+r.Auth.Scheme)
				} else {
					flags = append(flags, "auth")
				}
			}
			if r.Status != gmodel.StatusComplete {
				flags = append(flags, r.Status)
			}
			var st []string
			for _, x := range r.Responses {
				if x.Status > 0 {
					st = append(st, fmt.Sprint(x.Status))
				} else {
					st = append(st, "?")
				}
			}
			fmt.Fprintf(w, "  %-7s %-42s %-48s %s:%d", r.Method, r.Path, handler, r.File, r.Line)
			if len(st) > 0 {
				fmt.Fprintf(w, "  [%s]", strings.Join(uniqueStrings(st), " "))
			}
			if len(flags) > 0 {
				fmt.Fprintf(w, "  %s", strings.Join(flags, " "))
			}
			fmt.Fprintln(w)
		}
	}
}

func uniqueStrings(list []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range list {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// findRoutes resolves --route: a route ID, "METHOD /path" as registered or
// as an OpenAPI template, or a bare path (every method).
func findRoutes(h *gmodel.HTTP, q string) []*gmodel.Route {
	q = strings.TrimSpace(q)
	method, path, hasMethod := strings.Cut(q, " ")
	if !hasMethod {
		method, path = "", q
	}
	method = strings.ToUpper(method)
	var out []*gmodel.Route
	for _, r := range h.Routes {
		switch {
		case r.ID == q:
			return []*gmodel.Route{r}
		case (method == "" || r.Method == method) && (r.Path == path || r.Template == path):
			out = append(out, r)
		}
	}
	return out
}

func runJourney(stdout, stderr io.Writer, g *gmodel.Graph, q string, depth int, jsonOut bool) int {
	if g.HTTP == nil {
		fmt.Fprintln(stderr, "gitview graph journey: nenhuma rota HTTP encontrada")
		return 1
	}
	rs := findRoutes(g.HTTP, q)
	switch {
	case len(rs) == 0:
		fmt.Fprintf(stderr, "gitview graph journey: nenhuma rota %q; veja gitview graph routes\n", q)
		return 1
	case len(rs) > 1:
		fmt.Fprintf(stderr, "gitview graph journey: %q é ambíguo, %d registros:\n", q, len(rs))
		for _, r := range rs {
			fmt.Fprintf(stderr, "  %s  (--route %q)\n", r.Key, r.ID)
		}
		return 3
	}
	r := rs[0]
	ix := httpapi.NewIndex(g)
	j := ix.Journey(r.ID, depth, 3000)
	if jsonOut {
		type step struct {
			ID, Name, Type, Role, Layer, File string
			Line, Depth                       int
		}
		var steps []step
		for _, s := range j.Steps {
			layer, _ := s.Node.Metadata["layer"].(string)
			steps = append(steps, step{s.Node.ID, s.Node.Name, s.Node.Type, s.Role, layer, s.Node.File, s.Node.Line, s.Depth})
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		err := enc.Encode(map[string]any{"route": r, "steps": steps, "edges": j.Edges, "truncated": j.Truncated, "recursive": j.Recursive,
			"unresolvedCalls": j.Unresolved, "depth": depth})
		if err != nil {
			return 1
		}
		return 0
	}
	printJourney(stdout, r, ix, j, g.HTTP.Schemas)
	return 0
}

func printJourney(w io.Writer, r *gmodel.Route, ix *httpapi.Index, j *httpapi.Journey, defs map[string]*gmodel.Shape) {
	fmt.Fprintf(w, "%s  (%s, registrada em %s:%d)\n", r.Key, r.Framework, r.File, r.Line)
	fmt.Fprintf(w, "Contrato: %s", r.Status)
	if len(r.Reasons) > 0 {
		fmt.Fprintf(w, " — %s", strings.Join(r.Reasons, "; "))
	}
	fmt.Fprintln(w)

	fmt.Fprintln(w, "\nCadeia de handlers")
	for i, m := range r.Middlewares {
		fmt.Fprintf(w, "  %d. %-50s middleware %-7s %-10s adicionado em %s:%d\n", i+1, m.Name, m.Scope, m.Confidence, m.AddedFile, m.AddedLine)
	}
	if r.Handler != nil {
		fmt.Fprintf(w, "  %d. %-50s handler    %-7s %-10s %s:%d\n", len(r.Middlewares)+1, r.Handler.Name, "", r.Handler.Confidence, r.Handler.File, r.Handler.Line)
	}

	fmt.Fprintf(w, "\nGrafo de execução (profundidade %d; caminhos possíveis, não um trace)\n", j.Depth)
	in := map[string]bool{}
	for _, s := range j.Steps {
		in[s.Node.ID] = true
	}
	out := map[string][]*gmodel.Edge{}
	for _, e := range j.Edges {
		out[e.Source] = append(out[e.Source], e)
	}
	printed := map[string]bool{}
	var tree func(id string, indent string, path map[string]bool, depth int)
	tree = func(id, indent string, path map[string]bool, depth int) {
		for _, e := range out[id] {
			if e.Type == gmodel.EdgeHandles {
				continue
			}
			n := ix.Nodes[e.Target]
			label := n.Name
			if layer, _ := n.Metadata["layer"].(string); layer != "" {
				label += "  [" + layer + "]"
			}
			if sql, _ := n.Metadata["sql"].(string); sql != "" {
				label += "  " + oneLine(sql, 70)
			}
			mark := ""
			switch {
			case path[e.Target]:
				mark = "  ↻ recursão"
			case printed[e.Target]:
				mark = "  (já mostrado acima)"
			}
			site := ""
			if e.Evidence != nil && e.Evidence.File != "" {
				site = fmt.Sprintf("  %s:%d", e.Evidence.File, e.Evidence.Line)
			}
			fmt.Fprintf(w, "%s→ %s  %s%s%s\n", indent, label, e.Confidence, site, mark)
			if mark != "" {
				continue
			}
			printed[e.Target] = true
			path[e.Target] = true
			tree(e.Target, indent+"   ", path, depth+1)
			delete(path, e.Target)
		}
	}
	for _, s := range j.Steps {
		if s.Role != "middleware" && s.Role != "handler" {
			continue
		}
		layer, _ := s.Node.Metadata["layer"].(string)
		if layer == "" {
			layer = s.Role
		}
		fmt.Fprintf(w, "  %s  [%s]  %s:%d\n", s.Node.Name, layer, s.Node.File, s.Node.Line)
		printed[s.Node.ID] = true
		tree(s.Node.ID, "     ", map[string]bool{s.Node.ID: true}, 1)
	}
	if j.Truncated {
		fmt.Fprintln(w, "  … chamadas além da profundidade não mostradas (use --depth)")
	}
	if j.Unresolved > 0 {
		fmt.Fprintf(w, "  %d chamada(s) sem destino estático (unresolved)\n", j.Unresolved)
	}

	fmt.Fprintln(w, "\nRequisição")
	for _, p := range r.Params {
		from := ""
		if p.From != "" && (r.Handler == nil || p.From != r.Handler.Name) {
			from = " · lido por " + p.From
		}
		typ := "string"
		if p.Schema != nil {
			typ = p.Schema.Type
			if p.Schema.Format != "" {
				typ += "/" + p.Schema.Format
			}
		}
		fmt.Fprintf(w, "  %-6s %-24s %-14s obrigatório: %-7s %s%s\n", p.In, p.Name, typ, p.Required, p.Source, from)
	}
	if b := r.Body; b != nil {
		fmt.Fprintf(w, "  body   %s via %s", strings.Join(b.ContentTypes, ", "), b.Source)
		if b.Schema != nil {
			fmt.Fprintf(w, " → %s", shapeName(b.Schema))
		}
		fmt.Fprintln(w)
		if b.Schema != nil {
			sh := b.Schema
			if sh.Ref != "" {
				sh = defs[sh.Ref]
			}
			if sh != nil {
				for _, f := range sh.Fields {
					req := ""
					if f.Required(b.Enforces) {
						req = " *obrigatório"
					}
					var rules []string
					for _, x := range f.Rules {
						rr := x.Name
						if x.Param != "" {
							rr += "=" + x.Param
						}
						rules = append(rules, rr)
					}
					rs := ""
					if len(rules) > 0 {
						rs = "  " + f.Rules[0].Source + ":" + strings.Join(rules, ",")
					}
					fmt.Fprintf(w, "         %-24s %s%s%s\n", f.Name, shapeName(f.Schema), req, rs)
				}
			}
		}
	}
	if r.Auth != nil {
		fmt.Fprintf(w, "  auth   reconhecida=%v %s %s (%s) %s\n", r.Auth.Recognized, r.Auth.Scheme, r.Auth.Credential, r.Auth.By, r.Auth.Note)
	}

	fmt.Fprintln(w, "\nRespostas")
	for _, x := range r.Responses {
		st := "???"
		if x.Status > 0 {
			st = fmt.Sprintf("%d %s", x.Status, x.StatusText)
		}
		body := ""
		if x.Schema != nil {
			body = shapeName(x.Schema)
		}
		loc := ""
		if len(x.Evidence) > 0 {
			loc = fmt.Sprintf("%s:%d", x.Evidence[0].File, x.Evidence[0].Line)
		}
		fmt.Fprintf(w, "  %-28s %-18s %-30s %s  %s  [%s/%s]\n", st, x.ContentType, body, x.Source, loc, x.Basis, x.Confidence)
		if len(x.Via) > 0 {
			fmt.Fprintf(w, "  %-28s via %s\n", "", strings.Join(x.Via, " → "))
		}
	}
	if r.Example != nil {
		fmt.Fprintf(w, "\ncURL (valores sintéticos; base %s)\n%s\n", r.Example.BaseURLSrc, r.Example.Curl)
	}
}

func shapeName(sh *gmodel.Shape) string {
	if sh == nil {
		return ""
	}
	switch {
	case sh.Name != "":
		return sh.Name
	case sh.Type == gmodel.TypeArray && sh.Items != nil:
		return "[]" + shapeName(sh.Items)
	case sh.Type == gmodel.TypeObject && len(sh.Fields) > 0:
		var keys []string
		for _, f := range sh.Fields {
			k := f.Name
			if f.Conditional {
				k += "?"
			}
			keys = append(keys, k)
		}
		s := "{" + strings.Join(keys, ", ") + "}"
		if sh.Partial {
			s += "+"
		}
		return s
	case sh.GoType != "":
		return sh.GoType
	}
	return sh.Type
}
