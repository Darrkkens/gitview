package graph_test

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/graph/builder"
	"github.com/athos/gitview/internal/graph/export"
	"github.com/athos/gitview/internal/graph/model"
)

const mod = "example.com/repo"

var (
	once  sync.Once
	prog  *flow.Program
	root  string
	ldErr error
)

func load(t testing.TB) *flow.Program {
	t.Helper()
	once.Do(func() {
		root, _ = filepath.Abs("testdata/repo")
		prog, ldErr = flow.Load(context.Background(), root, root)
	})
	if ldErr != nil {
		t.Fatal(ldErr)
	}
	return prog
}

func fixedNow() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }

func build(t testing.TB) *model.Graph {
	t.Helper()
	g, err := builder.Build(context.Background(), []*flow.Program{load(t)}, builder.Options{Repository: "repo", Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func fn(name string) string { return "func:" + mod + "/" + name }

func TestNodeIdentity(t *testing.T) {
	g := build(t)
	idx := g.Index()
	want := map[string]struct {
		typ, file string
		line      int
	}{
		"package:" + mod + "/store":                 {model.KindPackage, "store", 0},
		"file:users/users.go":                       {model.KindFile, "users/users.go", 0},
		"type:" + mod + "/store.Store":              {model.KindInterface, "store/store.go", 9},
		"type:" + mod + "/store.MemStore":           {model.KindStruct, "store/store.go", 24},
		"func:(*" + mod + "/store.MemStore).Get":    {model.KindMethod, "store/store.go", 32},
		"func:(" + mod + "/store.Base).Close":       {model.KindMethod, "store/store.go", 22},
		"func:(*" + mod + "/users.Service).GetUser": {model.KindMethod, "users/users.go", 15},
		fn("algo.Fact"):                             {model.KindFunc, "algo/algo.go", 3},
		fn("cmd/app.main"):                          {model.KindFunc, "cmd/app/main.go", 23},
		"route:GET /users@cmd/app/main.go:27":       {model.KindRoute, "cmd/app/main.go", 27},
		"unresolved:algo/algo.go:28":                {model.KindUnresolved, "algo/algo.go", 28},
		"ext:strings:strings.ToUpper":               {model.KindExternal, "", 0},
	}
	for id, w := range want {
		n := idx[id]
		if n == nil {
			t.Errorf("missing node %s", id)
			continue
		}
		if n.Type != w.typ || n.File != w.file || n.Line != w.line {
			t.Errorf("%s = %s %s:%d, want %s %s:%d", id, n.Type, n.File, n.Line, w.typ, w.file, w.line)
		}
	}
	if n := idx["func:(*"+mod+"/store.MemStore).Get"]; n.Parent != "file:store/store.go" || n.Group != "package:"+mod+"/store" {
		t.Errorf("Get parent/group = %s / %s", n.Parent, n.Group)
	}
}

func TestHomonymsAreDistinct(t *testing.T) {
	g := build(t)
	var formats []string
	for _, n := range g.Nodes {
		if n.Name == "Format" {
			formats = append(formats, n.ID)
		}
	}
	sort.Strings(formats)
	want := []string{fn("report.Format"), fn("users.Format")}
	if strings.Join(formats, ",") != strings.Join(want, ",") {
		t.Fatalf("Format nodes = %v, want %v", formats, want)
	}
}

func edgesOf(g *model.Graph, types ...string) map[string]string {
	keep := map[string]bool{}
	for _, t := range types {
		keep[t] = true
	}
	out := map[string]string{}
	for _, e := range g.Edges {
		if keep[e.Type] {
			out[e.Type+" "+e.Source+" -> "+e.Target] = e.Confidence
		}
	}
	return out
}

// TestCallsAreExactlyTheCodeFlowCalls lists every call between module
// functions: anything more would be an invented relation.
func TestCallsAreExactlyTheCodeFlowCalls(t *testing.T) {
	g := build(t)
	idx := g.Index()
	got := map[string]string{}
	for k, c := range edgesOf(g, model.EdgeCalls, model.EdgeGo, model.EdgeDefer, model.EdgeHandles) {
		parts := strings.SplitN(k, " ", 2)
		ends := strings.Split(parts[1], " -> ")
		if idx[ends[1]].External {
			continue
		}
		got[k] = c
	}
	getMem := "func:(*" + mod + "/store.MemStore).Get"
	getUser := "func:(*" + mod + "/users.Service).GetUser"
	want := map[string]string{
		"calls " + getUser + " -> " + getMem:                                          model.Likely, // interface with one implementation
		"calls " + fn("algo.Apply") + " -> unresolved:algo/algo.go:28":                model.Unresolved,
		"calls " + fn("algo.Even") + " -> " + fn("algo.Odd"):                          model.Confirmed,
		"calls " + fn("algo.Odd") + " -> " + fn("algo.Even"):                          model.Confirmed,
		"calls " + fn("algo.Fact") + " -> " + fn("algo.Fact"):                         model.Confirmed,
		"calls " + fn("algo.Run") + " -> " + fn("algo.Apply"):                         model.Confirmed,
		"calls " + fn("algo.Run") + " -> " + fn("algo.Double"):                        model.Possible, // passed as a value
		"calls " + fn("cmd/app.main") + " -> " + fn("algo.Even"):                      model.Confirmed,
		"calls " + fn("cmd/app.main") + " -> " + fn("algo.Fact"):                      model.Confirmed,
		"calls " + fn("cmd/app.main") + " -> " + fn("algo.Run"):                       model.Confirmed,
		"calls " + fn("cmd/app.main") + " -> " + fn("cmd/app.userHandler"):            model.Possible,
		"calls " + fn("cmd/app.userHandler") + " -> " + getUser:                       model.Confirmed,
		"calls " + fn("cmd/app.userHandler") + " -> " + fn("report.Format"):           model.Confirmed,
		"calls " + fn("cmd/app.userHandler") + " -> " + fn("users.Format"):            model.Confirmed,
		"calls " + fn("cmd/app.userHandler") + " -> unresolved:cmd/app/main.go:17":    model.Unresolved,
		"handles route:GET /users@cmd/app/main.go:27 -> " + fn("cmd/app.userHandler"): model.Confirmed,
	}
	diff(t, got, want)
}

func diff(t *testing.T, got, want map[string]string) {
	t.Helper()
	for k, c := range want {
		if got[k] != c {
			t.Errorf("edge %s: confidence %q, want %q", k, got[k], c)
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("unexpected edge %s (%s)", k, got[k])
		}
	}
}

func TestTypeRelations(t *testing.T) {
	g := build(t)
	ty := func(n string) string { return "type:" + mod + "/" + n }
	want := map[string]string{
		"implements " + ty("store.MemStore") + " -> " + ty("store.Store"):                 model.Confirmed,
		"embeds " + ty("store.MemStore") + " -> " + ty("store.Base"):                      model.Confirmed,
		"method " + ty("store.MemStore") + " -> func:(*" + mod + "/store.MemStore).Get":   model.Confirmed,
		"method " + ty("store.Base") + " -> func:(" + mod + "/store.Base).Close":          model.Confirmed,
		"method " + ty("users.Service") + " -> func:(*" + mod + "/users.Service).GetUser": model.Confirmed,
	}
	diff(t, edgesOf(g, model.EdgeImplements, model.EdgeEmbeds, model.EdgeMethod), want)

	refs := edgesOf(g, model.EdgeReferences)
	for _, k := range []string{
		"references " + ty("store.User") + " -> " + ty("store.Profile"),
		"references " + ty("users.Service") + " -> " + ty("store.Store"),
		"references " + fn("report.Format") + " -> " + ty("store.User"),
	} {
		if refs[k] == "" {
			t.Errorf("missing %s", k)
		}
	}
	for k := range refs {
		if strings.Contains(k, "algo") || strings.Contains(k, "lonely") {
			t.Errorf("unexpected reference %s", k)
		}
	}
	for _, e := range g.Edges {
		if e.Type == model.EdgeImplements && e.Note != "pointer receiver" {
			t.Errorf("MemStore implements Store through *MemStore, note = %q", e.Note)
		}
	}
}

func TestPackagesImportsAndExternals(t *testing.T) {
	g := build(t)
	idx := g.Index()
	imps := edgesOf(g, model.EdgeImports)
	for _, k := range []string{
		"imports package:" + mod + "/users -> package:" + mod + "/store",
		"imports package:" + mod + "/users -> package:strings",
		"imports package:" + mod + "/cmd/app -> package:net/http",
	} {
		if imps[k] != model.Confirmed {
			t.Errorf("missing %s", k)
		}
	}
	for k := range imps {
		if strings.Contains(k, "lonely") {
			t.Errorf("lonely imports nothing and is imported by nobody: %s", k)
		}
	}
	s := idx["package:strings"]
	if s == nil || !s.External || s.Metadata["std"] != true || s.Community != -1 {
		t.Errorf("package:strings = %+v", s)
	}
	lonely := idx["package:"+mod+"/lonely"]
	if lonely.Degree != 0 {
		t.Errorf("lonely degree = %d", lonely.Degree)
	}
	for _, n := range g.Nodes {
		if n.Type == model.KindPackage && !n.External && n.ID != lonely.ID && n.Community == lonely.Community {
			t.Errorf("isolated package shares community %d with %s", n.Community, n.ID)
		}
	}
	if g.Stats.Packages != 6 || g.Stats.ExternalPackages != 5 || g.Stats.Files != 6 {
		t.Errorf("stats = %+v", g.Stats)
	}
}

func TestEvidenceOnEveryEdge(t *testing.T) {
	g := build(t)
	for _, e := range g.Edges {
		if e.Evidence == nil || e.Evidence.Source == "" {
			t.Errorf("%s has no evidence", e.ID)
			continue
		}
		if (e.Type == model.EdgeCalls || e.Type == model.EdgeReferences || e.Type == model.EdgeImports) && (e.Evidence.File == "" || e.Evidence.Line == 0) {
			t.Errorf("%s: evidence without location: %+v", e.ID, e.Evidence)
		}
	}
	// Several call sites merge into one weighted edge.
	for _, e := range g.Edges {
		if e.Type == model.EdgeCalls && e.Source == fn("cmd/app.userHandler") && e.Target == "ext:net/http:http.ResponseWriter.Write" {
			if e.Confidence != model.Possible {
				t.Errorf("external interface call confidence = %s", e.Confidence)
			}
		}
	}
	if g.Completeness.Status != "partial" || !strings.Contains(g.Completeness.String(), "unresolved") {
		t.Errorf("completeness = %v; unresolved calls must be reported", g.Completeness)
	}
}

func TestValidateAndLevels(t *testing.T) {
	g := build(t)
	if err := g.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, l := range export.Levels {
		var buf bytes.Buffer
		if err := export.JSON(&buf, g, l); err != nil {
			t.Fatal(err)
		}
		var back model.Graph
		if err := json.Unmarshal(buf.Bytes(), &back); err != nil {
			t.Fatalf("%s: invalid JSON: %v", l, err)
		}
		if err := back.Validate(); err != nil {
			t.Errorf("%s: %v", l, err)
		}
		if len(back.Nodes) == 0 {
			t.Errorf("%s: no nodes", l)
		}
		if strings.Contains(buf.String(), root) {
			t.Errorf("%s: export contains the absolute repository path", l)
		}
	}
	files := export.Filter(g, export.LevelFiles)
	found := false
	for _, e := range files.Edges {
		if e.Type == model.EdgeDepends && e.Source == "file:cmd/app/main.go" && e.Target == "file:users/users.go" {
			found = e.Metadata["derived"] == true
		}
	}
	if !found {
		t.Error("files level: missing derived dependency main.go → users.go")
	}
}

func TestStableAcrossRuns(t *testing.T) {
	a := build(t)
	p2, err := flow.Load(context.Background(), root, root)
	if err != nil {
		t.Fatal(err)
	}
	b, err := builder.Build(context.Background(), []*flow.Program{p2}, builder.Options{Repository: "repo", Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	a.Stats.AnalysisMS, a.Stats.BuildMS, a.Stats.LayoutMS = 0, 0, 0
	b.Stats.AnalysisMS, b.Stats.BuildMS, b.Stats.LayoutMS = 0, 0, 0
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if !bytes.Equal(ja, jb) {
		t.Fatal("two analyses of the same code produced different datasets (IDs, communities or positions)")
	}
}

func TestCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := builder.Build(ctx, []*flow.Program{load(t)}, builder.Options{}); err == nil {
		t.Fatal("cancelled build returned no error")
	}
}
