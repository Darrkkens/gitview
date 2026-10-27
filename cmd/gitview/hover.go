package main

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/athos/gitview/internal/export"
	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/hover"
)

// maxOfflineHovers bounds the identifiers resolved for an offline report.
const maxOfflineHovers = 20000

// offlineHovers resolves the identifiers of the Go code an offline report
// shows — the new side of the diffs and the Inspector's functions — so the
// page can answer hovers without a server. Nothing comes from a file whose
// content the report withholds; an item whose text looks like a secret is
// dropped, and the value of a repository constant declared outside the
// shown files is not included.
func offlineHovers(progs []*flow.Program, rep *export.Report) *hover.Table {
	if len(progs) == 0 {
		return nil
	}
	r := hover.New(progs)
	lines := map[string][]int{}
	shown := map[string]bool{}
	withheld := map[string]bool{}
	for _, p := range rep.Omitted {
		withheld[p] = true
	}
	for _, f := range rep.Files {
		if withheld[f.Path] || f.Binary || !strings.HasSuffix(f.Path, ".go") || !r.Has(f.Path) {
			continue
		}
		shown[f.Path] = true
		for _, h := range f.Hunks {
			for _, l := range h.Lines {
				if l.NewNum > 0 {
					lines[f.Path] = append(lines[f.Path], l.NewNum)
				}
			}
		}
	}
	if rep.Inspect != nil {
		for _, b := range rep.Inspect.Symbols {
			s := b.Symbol
			if withheld[s.File] || s.Line <= 0 || !r.Has(s.File) {
				continue
			}
			shown[s.File] = true
			for l := s.Line; l <= max(s.EndLine, s.Line); l++ {
				lines[s.File] = append(lines[s.File], l)
			}
		}
	}
	if len(lines) == 0 {
		return nil
	}
	for f, ls := range lines {
		lines[f] = uniqueInts(ls)
	}
	t := r.Table(lines, maxOfflineHovers)
	drop := map[int]bool{}
	for i, in := range t.Items {
		if in.Local && withheld[in.File] || export.SensitivePath(in.File) ||
			export.SensitiveText(in.Signature) || export.SensitiveText(in.Decl) || export.SensitiveText(in.Doc) {
			drop[i] = true
			continue
		}
		if in.Kind == "const" && in.Local && !shown[in.File] {
			c := *in
			c.Signature = constValue.ReplaceAllString(c.Signature, "")
			t.Items[i] = &c
		}
	}
	if len(drop) > 0 {
		for _, m := range t.Files {
			for k, i := range m {
				if drop[i] {
					delete(m, k)
				}
			}
		}
		for i := range drop {
			t.Items[i] = nil // keeps the indexes; the page skips missing items
		}
	}
	return t
}

// constValue is the " = value" ending of a constant's signature.
var constValue = regexp.MustCompile(` = .*$`)

func uniqueInts(xs []int) []int {
	seen := map[int]bool{}
	out := xs[:0]
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

// hoverAPI answers GET /api/hover?file=&line=&col=&name= from the analysis
// of the report section (the code of Changes and the Inspector).
func (w *workspace) hoverAPI(r *http.Request, _ []byte) (any, int, error) {
	q := r.URL.Query()
	line, err1 := strconv.Atoi(q.Get("line"))
	col, err2 := strconv.Atoi(q.Get("col"))
	file, name := q.Get("file"), q.Get("name")
	if err1 != nil || err2 != nil || line < 1 || col < 1 || file == "" || !validIdent(name) {
		return nil, http.StatusBadRequest, errBadHover
	}
	w.mu.Lock()
	res := w.hover
	w.mu.Unlock()
	return hover.Answer(res, file, line, col, name), 0, nil
}

var errBadHover = errors.New("file, line, col and name (a Go identifier) are required")

var identRe = regexp.MustCompile(`^[\p{L}_][\p{L}\p{Nd}_]*$`)

func validIdent(s string) bool { return len(s) <= 256 && identRe.MatchString(s) }
