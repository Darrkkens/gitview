package flow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	appOnce sync.Once
	appProg *Program
	appErr  error
)

func app(t *testing.T) *Program {
	t.Helper()
	appOnce.Do(func() {
		dir, _ := filepath.Abs("testdata/app")
		appProg, appErr = Load(context.Background(), dir, dir)
	})
	if appErr != nil {
		t.Fatal(appErr)
	}
	return appProg
}

func query(t *testing.T, p *Program, q, dir string, depth int) *Graph {
	t.Helper()
	fn, err := p.Resolve(q)
	if err != nil {
		t.Fatal(err)
	}
	g, err := p.Query(fn, dir, Options{Depth: depth, External: true})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func byName(g *Graph, name string) *Node {
	for _, n := range g.Nodes {
		if n.Name == name {
			return n
		}
	}
	return nil
}

func edge(t *testing.T, g *Graph, from, to string) *Edge {
	t.Helper()
	for _, e := range g.Edges {
		f, d := g.Node(e.From), g.Node(e.To)
		if f != nil && d != nil && f.Name == from && d.Name == to {
			return e
		}
	}
	return nil
}

func mustEdge(t *testing.T, g *Graph, from, to, conf string) *Edge {
	t.Helper()
	e := edge(t, g, from, to)
	if e == nil {
		var all []string
		for _, e := range g.Edges {
			all = append(all, g.Node(e.From).Name+" -> "+g.Node(e.To).Name)
		}
		t.Fatalf("missing edge %s -> %s; edges:\n%s", from, to, strings.Join(all, "\n"))
	}
	if conf != "" && e.Confidence != conf {
		t.Errorf("%s -> %s confidence %s, want %s", from, to, e.Confidence, conf)
	}
	return e
}

// --- symbol search -------------------------------------------------------------

func TestFindUniqueFunction(t *testing.T) {
	s := app(t).Find("ValidateClient")
	if len(s) != 1 || s[0].Qualified != "client.ValidateClient" || s[0].File != "client/service.go" || s[0].Line != 30 {
		t.Fatalf("%+v", s)
	}
	if s[0].Pkg != "example.com/app/client" || !strings.HasPrefix(s[0].Signature, "func ValidateClient(id int) error") {
		t.Errorf("symbol details: %+v", s[0])
	}
}

func TestFindHomonymsAreNotPickedArbitrarily(t *testing.T) {
	p := app(t)
	found := p.Find("GetClientByID")
	if len(found) != 3 {
		t.Fatalf("found %d: %+v", len(found), found)
	}
	want := map[string]string{
		"catalog.GetClientByID":                  "",
		"client.(*ClientService).GetClientByID":  "(*ClientService)",
		"httpapi.(*ClientHandler).GetClientByID": "(*ClientHandler)",
	}
	for _, s := range found {
		recv, ok := want[s.Qualified]
		if !ok || s.Recv != recv {
			t.Errorf("unexpected symbol %+v", s)
		}
	}
	_, err := p.Resolve("GetClientByID")
	var amb *AmbiguityError
	if !errors.As(err, &amb) || len(amb.Candidates) != 3 || !strings.Contains(err.Error(), "catalog/client.go:4") {
		t.Fatalf("expected ambiguity error with candidates, got %v", err)
	}
}

func TestFindByQualifiedName(t *testing.T) {
	p := app(t)
	for q, want := range map[string]string{
		"client.(*ClientService).GetClientByID":                 "client.(*ClientService).GetClientByID",
		"(*ClientHandler).GetClientByID":                        "httpapi.(*ClientHandler).GetClientByID",
		"ClientHandler.GetClientByID":                           "httpapi.(*ClientHandler).GetClientByID",
		"catalog.GetClientByID":                                 "catalog.GetClientByID",
		"example.com/app/catalog.GetClientByID":                 "catalog.GetClientByID",
		"(*example.com/app/client.ClientService).GetClientByID": "client.(*ClientService).GetClientByID",
		"catalog/client.go:5":                                   "catalog.GetClientByID",
	} {
		s := p.Find(q)
		if len(s) != 1 || s[0].Qualified != want {
			t.Errorf("Find(%q) = %+v, want %s", q, s, want)
		}
	}
	if s := p.Find("NoSuchFunction"); len(s) != 0 {
		t.Errorf("unexpected match %+v", s)
	}
}

// --- bidirectional graph -------------------------------------------------------

func TestBothDirections(t *testing.T) {
	g := query(t, app(t), "client.(*ClientService).GetClientByID", Both, 5)
	root := g.Node(g.Roots[0].ID)
	if root.Name != "client.(*ClientService).GetClientByID" || g.Roots[0].Kind != "selected" {
		t.Fatalf("root %+v", g.Roots)
	}
	// Callees: direct, cross-type via interface, external, database.
	mustEdge(t, g, "client.(*ClientService).GetClientByID", "client.ValidateClient", Confirmed)
	e := mustEdge(t, g, "client.(*ClientService).GetClientByID", "client.(*PGRepo).Find", Likely)
	if !strings.Contains(e.Note, "client.Repository") {
		t.Errorf("interface note missing: %q", e.Note)
	}
	mustEdge(t, g, "client.ValidateClient", "errors.New", Confirmed)
	if n := byName(g, "errors.New"); n.Kind != KindExternal {
		t.Errorf("errors.New kind %s", n.Kind)
	}
	db := byName(g, "pgx QueryRow")
	if db == nil || db.SQL != "SELECT id, name FROM clients WHERE id = $1" || db.SQLDynamic {
		t.Fatalf("db node %+v", db)
	}
	// Callers: handler in another package, and the HTTP route registering it.
	mustEdge(t, g, "httpapi.(*ClientHandler).GetClientByID", "client.(*ClientService).GetClientByID", Confirmed)
	mustEdge(t, g, "GET /api/clients/:id", "httpapi.(*ClientHandler).GetClientByID", Confirmed)
	if !hasTag(byName(g, "httpapi.(*ClientHandler).GetClientByID"), "http handler") {
		t.Error("handler not tagged from route evidence")
	}
	if root.File != "client/service.go" || root.Line != 23 || root.Snippet == "" || root.Explain == "" {
		t.Errorf("root details %+v", root)
	}
}

func TestCallersOnlyAndCalleesOnly(t *testing.T) {
	p := app(t)
	callers := query(t, p, "client.(*ClientService).GetClientByID", Callers, 5)
	if byName(callers, "client.ValidateClient") != nil {
		t.Error("callers view must not include callees")
	}
	callees := query(t, p, "client.(*ClientService).GetClientByID", Callees, 5)
	if byName(callees, "httpapi.(*ClientHandler).GetClientByID") != nil {
		t.Error("callees view must not include callers")
	}
}

func TestDepthLimitPerDirection(t *testing.T) {
	p := app(t)
	g := query(t, p, "algo.L1", Callees, 5)
	if byName(g, "algo.L6") == nil || byName(g, "algo.L7") != nil {
		t.Fatalf("callees depth 5 should reach L6 and not L7: %v", names(g))
	}
	if !byName(g, "algo.L6").Truncated || !g.Truncated {
		t.Error("node at the limit must be marked truncated")
	}
	g = query(t, p, "algo.L7", Callers, 5)
	if byName(g, "algo.L2") == nil || byName(g, "algo.L1") != nil {
		t.Fatalf("callers depth 5 should reach L2 and not L1: %v", names(g))
	}
	// Each direction gets its own budget.
	g = query(t, p, "algo.L4", Both, 5)
	if byName(g, "algo.L1") == nil || byName(g, "algo.L7") == nil {
		t.Fatalf("both directions: %v", names(g))
	}
}

func names(g *Graph) []string {
	var out []string
	for _, n := range g.Nodes {
		out = append(out, n.Name)
	}
	return out
}

func TestInterfaceDispatch(t *testing.T) {
	p := app(t)
	g := query(t, p, "algo.Total", Callees, 5)
	mustEdge(t, g, "algo.Total", "algo.Square.Area", Possible)
	mustEdge(t, g, "algo.Total", "algo.(*Circle).Area", Possible)

	g = query(t, p, "algo.Alert", Callees, 5)
	var unresolved *Node
	for _, n := range g.Nodes {
		if n.Kind == KindUnresolved {
			unresolved = n
		}
	}
	if unresolved == nil || !strings.Contains(unresolved.Note, "algo.Notifier.Notify") {
		t.Fatalf("interface without implementation must be unresolved: %v", names(g))
	}
}

func TestUnresolvedFunctionValue(t *testing.T) {
	g := query(t, app(t), "algo.Apply", Callees, 5)
	var n *Node
	for _, x := range g.Nodes {
		if x.Kind == KindUnresolved {
			n = x
		}
	}
	if n == nil || !strings.Contains(n.Note, "function value f") {
		t.Fatalf("expected unresolved call through f: %+v", g.Nodes)
	}
	for _, e := range g.Edges {
		if e.To == n.ID && e.Confidence != Unresolved {
			t.Error("edge to unresolved node must be marked unresolved")
		}
	}
}

func TestRecursionAndCycles(t *testing.T) {
	p := app(t)
	g := query(t, p, "algo.Fact", Callees, 5)
	if n := byName(g, "algo.Fact"); !n.Recursive {
		t.Error("Fact must be recursive")
	}
	mustEdge(t, g, "algo.Fact", "algo.Fact", Confirmed)
	g = query(t, p, "algo.Ping", Both, 5)
	if !byName(g, "algo.Ping").Recursive || !byName(g, "algo.Pong").Recursive {
		t.Error("Ping/Pong cycle not detected")
	}
	if byName(g, "algo.L1") != nil {
		t.Error("unrelated nodes included")
	}
}

func TestNoCallersAndNoCalls(t *testing.T) {
	p := app(t)
	g := query(t, p, "catalog.GetClientByID", Callers, 5)
	if len(g.Nodes) != 1 || len(g.Edges) != 0 {
		t.Fatalf("function without callers: %v", names(g))
	}
	if !strings.Contains(g.Nodes[0].Explain, "No callers") {
		t.Errorf("explanation: %s", g.Nodes[0].Explain)
	}
	g = query(t, p, "client.Leaf", Callees, 5)
	if len(g.Nodes) != 1 || len(g.Edges) != 0 {
		t.Fatalf("function without calls: %v", names(g))
	}
}

func TestGoroutines(t *testing.T) {
	g := query(t, app(t), "algo.StartWorkers", Callees, 5)
	if e := mustEdge(t, g, "algo.StartWorkers", "algo.worker", Confirmed); e.Kind != EdgeGo {
		t.Errorf("kind %s, want go", e.Kind)
	}
	if e := mustEdge(t, g, "algo.StartWorkers", "algo.StartWorkers.func1", Confirmed); e.Kind != EdgeGo {
		t.Errorf("closure goroutine kind %s", e.Kind)
	}
	if n := byName(g, "algo.StartWorkers.func1"); n.Kind != KindClosure || !hasTag(n, "goroutine") {
		t.Errorf("closure node %+v", n)
	}
}

func TestDatabaseOperations(t *testing.T) {
	p := app(t)
	g := query(t, p, "client.(*PGRepo).Search", Callees, 5)
	db := byName(g, "pgx Query")
	if db == nil || !db.SQLDynamic || db.SQL != "" {
		t.Fatalf("dynamic SQL not detected: %+v", db)
	}
	if !strings.Contains(db.Explain, "not an observed query") {
		t.Error("explanation must state the analysis is static")
	}
	g = query(t, p, "client.(*PGRepo).Rename", Callees, 5)
	ops := map[string]string{}
	for _, n := range g.Nodes {
		if n.Kind == KindDB {
			ops[n.DBOp] = n.SQL
		}
	}
	for _, op := range []string{"Begin", "Exec", "Commit", "Rollback"} {
		if _, ok := ops[op]; !ok {
			t.Errorf("missing db op %s: %v", op, ops)
		}
	}
	if ops["Exec"] != "UPDATE clients SET name = $1 WHERE id = $2" {
		t.Errorf("tx Exec SQL %q", ops["Exec"])
	}
	for _, e := range g.Edges {
		if n := g.Node(e.To); n.DBOp == "Rollback" && e.Kind != EdgeDefer {
			t.Errorf("deferred Rollback edge kind %s", e.Kind)
		}
	}
}

// --- routes ----------------------------------------------------------------------

func routeNames(g *Graph) map[string]*Node {
	out := map[string]*Node{}
	for _, n := range g.Nodes {
		if n.Kind == KindRoute {
			out[n.Router+" "+n.Name] = n
		}
	}
	return out
}

func TestRoutes(t *testing.T) {
	g := app(t).RoutesFlow(Options{Depth: 3, External: true})
	r := routeNames(g)
	for _, want := range []string{
		"gin GET /api/clients/:id", "chi GET /v1/ping", "chi POST /items",
		"echo GET /admin/users", "echo GET /status", "net/http GET /health", "net/http ANY /x",
	} {
		if r[want] == nil {
			t.Errorf("route %q not found; have %v", want, keys(r))
		}
	}
	// Gin: Use(Logger) on the engine, Auth() factory, then the handler — in order.
	gin := r["gin GET /api/clients/:id"]
	var chain []string
	for _, e := range g.Out(gin.ID) {
		chain = append(chain, g.Node(e.To).Name+"#"+e.Confidence)
	}
	if strings.Join(chain, " ") != "httpapi.Logger#confirmed httpapi.Auth.func1#likely httpapi.(*ClientHandler).GetClientByID#confirmed" {
		t.Errorf("gin chain: %v", chain)
	}
	if !hasTag(byName(g, "httpapi.Logger"), "middleware") {
		t.Error("Logger not tagged as middleware")
	}
	// Handler lists built by helpers: spread slices and append.
	for route, want := range map[string]string{
		"gin GET /api/spread":    "httpapi.Logger httpapi.Auth.func1 httpapi.(*ClientHandler).GetClientByID",
		"gin POST /api/appended": "httpapi.Logger httpapi.Auth.func1 httpapi.(*ClientHandler).GetClientByID httpapi.Logger",
	} {
		n := r[route]
		if n == nil {
			t.Errorf("route %s missing", route)
			continue
		}
		var got []string
		for _, e := range g.Out(n.ID) {
			got = append(got, g.Node(e.To).Name)
		}
		if strings.Join(got, " ") != want {
			t.Errorf("%s chain = %v, want %s", route, got, want)
		}
	}
	// Echo: middleware runs before the handler.
	echo := r["echo GET /admin/users"]
	if out := g.Out(echo.ID); len(out) != 2 || g.Node(out[0].To).Name != "echoapi.AdminOnly" || g.Node(out[1].To).Name != "echoapi.ListUsers" {
		t.Errorf("echo chain wrong")
	}
	// net/http: http.Handler value resolves to its ServeHTTP method.
	mustEdge(t, g, "ANY /x", "nethttp.XHandler.ServeHTTP", Confirmed)
	// Dynamic registration is reported as partial, not guessed.
	var dyn *Node
	for _, n := range r {
		if n.Partial {
			dyn = n
		}
	}
	if dyn == nil || !strings.Contains(dyn.Note, "computed at runtime") {
		t.Fatalf("dynamic route not marked partial: %v", keys(r))
	}
}

func keys(m map[string]*Node) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// --- robustness and caching ------------------------------------------------------

func TestBrokenModulePartialResults(t *testing.T) {
	dir, _ := filepath.Abs("testdata/broken")
	p, err := Load(context.Background(), dir, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Errors) == 0 || !strings.Contains(strings.Join(p.Errors, "\n"), "undefinedThing") {
		t.Fatalf("compile errors not reported: %v", p.Errors)
	}
	g := query(t, p, "GetClientByID", Callees, 5)
	mustEdge(t, g, "good.GetClientByID", "good.helper", Confirmed)
}

func TestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dir, _ := filepath.Abs("testdata/app")
	if _, err := Load(ctx, dir, dir); err == nil {
		t.Fatal("expected cancellation error")
	}
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCacheInvalidatesOnChange(t *testing.T) {
	root := t.TempDir()
	copyTree(t, "testdata", root)
	dir := filepath.Join(root, "app")
	c := NewCache()
	p1, cached, err := c.Get(context.Background(), dir, dir)
	if err != nil || cached {
		t.Fatalf("first load: cached=%v err=%v", cached, err)
	}
	p2, cached, _ := c.Get(context.Background(), dir, dir)
	if !cached || p2 != p1 {
		t.Fatal("unchanged module must reuse the cached analysis")
	}
	file := filepath.Join(dir, "client", "service.go")
	data, _ := os.ReadFile(file)
	os.WriteFile(file, append(data, []byte("\nfunc Added() { Leaf() }\n")...), 0o644)
	future := time.Now().Add(2 * time.Second)
	os.Chtimes(file, future, future)
	p3, cached, err := c.Get(context.Background(), dir, dir)
	if err != nil || cached || p3 == p1 || len(p3.Find("Added")) != 1 {
		t.Fatalf("changed module must be reanalysed: cached=%v err=%v", cached, err)
	}
}

func TestIndexCoversWholeModule(t *testing.T) {
	p := app(t)
	g := p.Index(Options{})
	for _, s := range p.Symbols() {
		if g.Node(s.ID) == nil {
			t.Errorf("symbol %s missing from index", s.Qualified)
		}
	}
	if len(routeNames(g)) != p.RouteCount() {
		t.Errorf("routes in index %d, found %d", len(routeNames(g)), p.RouteCount())
	}
	if g.Truncated {
		t.Error("index of a small module must be complete")
	}
}
