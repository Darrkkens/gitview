package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/athos/gitview/internal/export"
	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/gitdiff"
	"github.com/athos/gitview/internal/hover"
	"github.com/athos/gitview/internal/inspect/inspecttest"
)

// fixtureCol is the byte column of the first name on a line of the
// Inspector's fixture module.
func fixtureCol(t *testing.T, rel string, line int, name string) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(inspecttest.Dir(), rel))
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(strings.Split(string(data), "\n")[line-1], name)
	if i < 0 {
		t.Fatalf("%s:%d has no %q", rel, line, name)
	}
	return i + 1
}

func TestHoverAPI(t *testing.T) {
	w := &workspace{}
	get := func(q string) (hover.Reply, int) {
		t.Helper()
		v, code, err := w.hoverAPI(httptest.NewRequest(http.MethodGet, "/api/hover?"+q, nil), nil)
		if err != nil {
			return hover.Reply{}, code
		}
		return v.(hover.Reply), code
	}
	c := fixtureCol(t, "service/client.go", 33, "Errorf")
	q := "file=service/client.go&line=33&col=" + strconv.Itoa(c) + "&name=Errorf"
	if r, _ := get(q); r.Status != "pending" {
		t.Errorf("before the analysis: %+v", r)
	}
	w.hover = hover.New([]*flow.Program{inspecttest.Deep(t)})
	if r, _ := get(q); r.Status != "ok" || r.Info == nil || r.Info.Name != "Errorf" {
		t.Errorf("hover: %+v", r)
	}
	for _, bad := range []string{"file=service/client.go&line=33&col=x&name=Errorf", "file=&line=1&col=1&name=x",
		"file=a.go&line=1&col=1&name=" + strings.Repeat("a", 300), "file=a.go&line=1&col=1&name=a-b"} {
		if _, code := get(bad); code != http.StatusBadRequest {
			t.Errorf("%s: %d", bad, code)
		}
	}
}

// The offline table covers the shown code and leaks nothing from a file
// whose content the report withholds.
func TestOfflineHovers(t *testing.T) {
	prog := inspecttest.Deep(t)
	lines := func(ns ...int) []gitdiff.Line {
		var out []gitdiff.Line
		for _, n := range ns {
			out = append(out, gitdiff.Line{Kind: gitdiff.Added, NewNum: n})
		}
		return out
	}
	rep := export.Report{
		Files: []gitdiff.File{
			{Path: "service/client.go", Hunks: []gitdiff.Hunk{{Lines: append(lines(27, 33), gitdiff.Line{Kind: gitdiff.Removed, OldNum: 40})}}},
			{Path: "model/client.go", Hunks: []gitdiff.Hunk{{Lines: lines(4)}}},
			{Path: "README.md", Hunks: []gitdiff.Hunk{{Lines: lines(1)}}},
		},
		Omitted: []string{"model/client.go"},
	}
	tb := offlineHovers([]*flow.Program{prog}, &rep)
	if tb == nil || len(tb.Files["service/client.go"]) == 0 {
		t.Fatalf("no hovers for the shown file: %+v", tb)
	}
	if _, ok := tb.Files["model/client.go"]; ok {
		t.Error("hovers built for a withheld file")
	}
	k := hover.Key(33, fixtureCol(t, "service/client.go", 33, "Errorf"), "Errorf")
	if i, ok := tb.Files["service/client.go"][k]; !ok || tb.Items[i] == nil || tb.Items[i].Name != "Errorf" {
		t.Errorf("Errorf missing (%s)", k)
	}
	// model.Client is declared in the withheld file: its declaration and
	// documentation must not reach the page.
	k = hover.Key(27, fixtureCol(t, "service/client.go", 27, "Client"), "Client")
	if i, ok := tb.Files["service/client.go"][k]; ok {
		t.Errorf("a type of a withheld file is described: %+v", tb.Items[i])
	}
	for _, in := range tb.Items {
		if in != nil && in.File == "model/client.go" {
			t.Errorf("item from a withheld file: %+v", in)
		}
	}
	if offlineHovers(nil, &rep) != nil {
		t.Error("hovers without an analysis")
	}
}
