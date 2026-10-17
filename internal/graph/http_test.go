package graph_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/graph/builder"
	"github.com/athos/gitview/internal/graph/export"
	"github.com/athos/gitview/internal/graph/httpapi"
	"github.com/athos/gitview/internal/graph/model"
)

const api = "example.com/api"

var (
	apiOnce  sync.Once
	apiGraph *model.Graph
	apiErr   error
)

// apiDataset builds the dataset of the HTTP fixture once.
func apiDataset(t testing.TB) *model.Graph {
	t.Helper()
	apiOnce.Do(func() {
		root, _ := filepath.Abs("testdata/api")
		p, err := flow.Load(context.Background(), root, root)
		if err != nil {
			apiErr = err
			return
		}
		apiGraph, apiErr = builder.Build(context.Background(), []*flow.Program{p}, builder.Options{Repository: "api", Now: fixedNow})
	})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	return apiGraph
}

func route(t *testing.T, g *model.Graph, key string) *model.Route {
	t.Helper()
	for _, r := range g.HTTP.Routes {
		if r.Key == key {
			return r
		}
	}
	t.Fatalf("route %s not found", key)
	return nil
}

func TestRouteDiscovery(t *testing.T) {
	g := apiDataset(t)
	var keys []string
	for _, r := range g.HTTP.Routes {
		keys = append(keys, r.Key)
	}
	sort.Strings(keys)
	want := []string{
		"DELETE /api/users/:id", "GET /admin/stats", "GET /api/admin/users", "GET /api/kinds/{dynamic}", "GET /api/users", "GET /api/users/:id",
		"GET /health", "GET /items/{id}", "GET /reports/{reportID}", "POST /api/users", "POST /api/users/:id/avatar",
		"POST /items", "PUT /api/users/:id",
	}
	if strings.Join(keys, "\n") != strings.Join(want, "\n") {
		t.Fatalf("routes:\n%s\nwant:\n%s", strings.Join(keys, "\n"), strings.Join(want, "\n"))
	}
	// The dynamic path is reported, not guessed.
	if r := route(t, g, "GET /api/kinds/{dynamic}"); r.Status != model.StatusPartial || !strings.Contains(strings.Join(r.Reasons, ";"), "computed at runtime") {
		t.Errorf("dynamic route: %s %v", r.Status, r.Reasons)
	}
	// Every route is a node of the graph with the same ID.
	idx := g.Index()
	for _, r := range g.HTTP.Routes {
		if n := idx[r.ID]; n == nil || n.Type != model.KindRoute {
			t.Errorf("route %s has no graph node", r.ID)
		}
	}
	if r := route(t, g, "GET /api/users/:id"); r.Template != "/api/users/{id}" || r.Framework != "gin" || r.Group != "/api" {
		t.Errorf("template %q framework %q group %q", r.Template, r.Framework, r.Group)
	}
}

func chain(r *model.Route) []string {
	var out []string
	for _, m := range r.Middlewares {
		out = append(out, m.Scope+":"+m.Name)
	}
	if r.Handler != nil {
		out = append(out, "handler:"+r.Handler.Name)
	}
	return out
}

func TestMiddlewareChains(t *testing.T) {
	g := apiDataset(t)
	cases := map[string][]string{
		// Late is registered with Use after the routes: gin does not apply it.
		"POST /api/users": {"global:middlewares.RequestID.func1", "group:middlewares.Logger", "route:middlewares.(*Auth).RequireUser.func1",
			"handler:controllers.(*UserController).Create"},
		"GET /health": {"global:middlewares.RequestID.func1", "handler:controllers.Health"},
		// chi: the parent's Use applies to the mounted router.
		"GET /admin/stats": {"global:chiapp.audit", "handler:chiapp.stats"},
		// Use on the router plus With(audit) inline: audit runs twice.
		"GET /reports/{reportID}": {"global:chiapp.audit", "group:chiapp.audit", "handler:chiapp.report"},
		"POST /items":             {"handler:nethttp.createItem"},
		// Handlers captured by a closure helper: with(h) → [user, h].
		"GET /api/admin/users": {"global:middlewares.RequestID.func1", "group:middlewares.Logger", "route:middlewares.(*Auth).RequireUser.func1",
			"handler:controllers.(*UserController).List"},
	}
	for key, want := range cases {
		if got := chain(route(t, g, key)); strings.Join(got, " → ") != strings.Join(want, " → ") {
			t.Errorf("%s:\n got %v\nwant %v", key, got, want)
		}
	}
	r := route(t, g, "POST /api/users")
	if m := r.Middlewares[1]; m.AddedFile != "cmd/server/main.go" || m.AddedLine != 15 {
		t.Errorf("Logger added at %s:%d, want the Group call", m.AddedFile, m.AddedLine)
	}
	if r.Middlewares[2].Confidence != model.Likely || r.Handler.Confidence != model.Confirmed {
		t.Errorf("confidences: middleware %s handler %s", r.Middlewares[2].Confidence, r.Handler.Confidence)
	}
}

func params(r *model.Route) map[string]model.Param {
	out := map[string]model.Param{}
	for _, p := range r.Params {
		out[p.In+":"+p.Name] = p
	}
	return out
}

func TestRequestContracts(t *testing.T) {
	g := apiDataset(t)
	defs := g.HTTP.Schemas

	r := route(t, g, "POST /api/users")
	b := r.Body
	if b == nil || b.Source != "c.ShouldBindJSON" || b.Schema.Ref != api+"/controllers.CreateUserRequest" || strings.Join(b.ContentTypes, ",") != "application/json" {
		t.Fatalf("body: %+v", b)
	}
	if strings.Join(b.Enforces, ",") != "binding" {
		t.Errorf("ShouldBindJSON applies binding tags, got %v", b.Enforces)
	}
	def := defs[b.Schema.Ref]
	fields := map[string]model.Field{}
	var names []string
	for _, f := range def.Fields {
		fields[f.Name] = f
		names = append(names, f.Name)
	}
	if strings.Join(names, ",") != "created_by,name,email,age,nickname,role,tags,address,labels,birthday" {
		t.Errorf("fields: %v (secret and the unexported field must be absent; created_by is promoted)", names)
	}
	for name, req := range map[string]bool{"name": true, "email": true, "age": false, "nickname": false, "tags": false, "address": false} {
		if got := fields[name].Required(b.Enforces); got != req {
			t.Errorf("%s required = %v, want %v", name, got, req)
		}
	}
	if f := fields["nickname"]; !f.Schema.Nullable || !f.OmitEmpty {
		t.Errorf("nickname: %+v", f)
	}
	if f := fields["birthday"]; f.Schema.Format != "date-time" || f.Schema.Type != model.TypeString {
		t.Errorf("birthday: %+v", f.Schema)
	}
	if f := fields["created_by"]; f.Embedded != "controllers.Audit" {
		t.Errorf("created_by embedded from %q", f.Embedded)
	}
	if f := fields["labels"]; f.Schema.Type != model.TypeObject || f.Schema.Values.Type != model.TypeString {
		t.Errorf("labels: %+v", f.Schema)
	}
	addr := defs[fields["address"].Schema.Ref]
	if addr == nil || !addr.Fields[0].Required(b.Enforces) {
		t.Errorf("address.street must be required by its binding tag: %+v", addr)
	}

	// json.Decoder does not apply binding tags.
	u := route(t, g, "PUT /api/users/:id")
	if u.Body == nil || u.Body.Source != "json.NewDecoder(r.Body).Decode" || len(u.Body.Enforces) != 0 || !u.Body.DisallowUnknown {
		t.Errorf("PUT body: %+v", u.Body)
	}
	if p := params(u)["path:id"]; p.Source != "c.ShouldBindUri" || p.Required != "yes" {
		t.Errorf("PUT id: %+v", p)
	}

	l := params(route(t, g, "GET /api/users"))
	if p := l["query:page"]; p.Default != "1" || p.Required != "no" {
		t.Errorf("page: %+v", p)
	}
	if p := l["query:limit"]; p.Schema.Type != model.TypeInteger || p.Required != "unknown" {
		t.Errorf("limit (strconv.Atoi): %+v", p)
	}
	if p := l["header:Authorization"]; p.From != "middlewares.(*Auth).RequireUser.func1" {
		t.Errorf("Authorization is read by the middleware, got %q", p.From)
	}

	it := params(route(t, g, "GET /items/{id}"))
	if it["path:id"].Source != "r.PathValue" || it["query:expand"].Source != "r.URL.Query().Get" || it["header:X-Api-Key"].Source != "r.Header.Get" {
		t.Errorf("net/http params: %+v", it)
	}
	if p := params(route(t, g, "GET /reports/{reportID}"))["path:reportID"]; p.Source != "chi.URLParam" {
		t.Errorf("chi param: %+v", p)
	}
	av := route(t, g, "POST /api/users/:id/avatar")
	if p := params(av)["form:file"]; p.Schema.Format != "binary" || av.Body == nil || av.Body.ContentTypes[0] != "multipart/form-data" {
		t.Errorf("upload: %+v body %+v", p, av.Body)
	}
	// A parameter read by the code but absent from the pattern is flagged.
	if p := params(route(t, g, "GET /api/kinds/{dynamic}"))["path:kind"]; p.InPattern == nil || *p.InPattern {
		t.Errorf("kind: %+v", p)
	}
}

func statuses(r *model.Route) []int {
	var out []int
	for _, x := range r.Responses {
		out = append(out, x.Status)
	}
	return out
}

func response(r *model.Route, status int, from string) *model.Response {
	for i, x := range r.Responses {
		if x.Status == status && strings.Contains(x.From, from) {
			return &r.Responses[i]
		}
	}
	return nil
}

func TestResponses(t *testing.T) {
	g := apiDataset(t)
	r := route(t, g, "POST /api/users")
	if got := statuses(r); !equalInts(got, []int{201, 400, 401, 404, 500}) {
		t.Fatalf("statuses %v", got)
	}
	if x := response(r, 201, "Create"); x.Schema.Ref != api+"/controllers.UserResponse" || x.Basis != model.BasisStatic || x.Source != "c.JSON" {
		t.Errorf("201: %+v", x)
	}
	// 404 travels: fail → httpx.WriteError(errUserNotFound) → httpx.JSON,
	// the status read from the field of a package variable.
	x := response(r, 404, "Create")
	if x == nil || strings.Join(x.Via, " → ") != "controllers.(*UserController).fail → httpx.WriteError → httpx.JSON" || x.ContentType != "application/json" {
		t.Fatalf("404: %+v", x)
	}
	var keys []string
	for _, f := range x.Schema.Fields {
		keys = append(keys, f.Name)
	}
	if strings.Join(keys, ",") != "code,error" {
		t.Errorf("404 body keys %v", keys)
	}
	if x := response(r, 401, "RequireUser"); x == nil || x.Confidence != model.Likely {
		t.Errorf("401 from the auth middleware: %+v", x)
	}

	if x := response(route(t, g, "DELETE /api/users/:id"), 204, "Delete"); x == nil || x.Schema != nil {
		t.Errorf("204: %+v", x)
	}
	// c.Status(ctl.svc.AvatarStatus()) with a field set at runtime.
	av := route(t, g, "POST /api/users/:id/avatar")
	if x := response(av, 0, "Avatar"); x == nil || x.Basis == model.BasisStatic {
		t.Errorf("dynamic status must be unknown: %v", av.Responses)
	}
	// net/http: Encode without WriteHeader is the implicit 200, possible.
	gi := route(t, g, "GET /items/{id}")
	if x := response(gi, 200, "getItem"); x == nil || !x.Implicit || x.Confidence != model.Possible || x.ContentType != "application/json" {
		t.Errorf("implicit 200: %+v", x)
	}
	ci := route(t, g, "POST /items")
	if got := statuses(ci); !equalInts(got, []int{201, 400}) {
		t.Errorf("POST /items statuses %v", got)
	}
	if x := response(ci, 201, "createItem"); x.Source != "WriteHeader + json.NewEncoder(w).Encode" || x.Schema.Ref != api+"/nethttp.Item" {
		t.Errorf("201 pairing: %+v", x)
	}
	// The look-alike handler that no route uses answers 418: nowhere.
	for _, r := range g.HTTP.Routes {
		for _, x := range r.Responses {
			if x.Status == 418 {
				t.Errorf("%s: 418 from an unregistered function", r.Key)
			}
		}
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestAuthAndServers(t *testing.T) {
	g := apiDataset(t)
	if a := route(t, g, "GET /api/users").Auth; a == nil || !a.Recognized || a.Scheme != "bearer" || a.Credential != "Authorization header" {
		t.Errorf("bearer auth: %+v", a)
	}
	if a := route(t, g, "GET /health").Auth; a != nil {
		t.Errorf("/health has no authentication: %+v", a)
	}
	if a := route(t, g, "GET /items/{id}").Auth; a == nil || !strings.Contains(a.Note, "X-Api-Key") {
		t.Errorf("401 after reading X-Api-Key: %+v", a)
	}
	var addrs []string
	for _, s := range g.HTTP.Servers {
		addrs = append(addrs, s.Addr+" "+s.URL)
	}
	sort.Strings(addrs)
	if strings.Join(addrs, ";") != ":8080 http://localhost:8080;:9090 http://localhost:9090" {
		t.Errorf("servers %v", addrs)
	}
}

func TestExamplesAndCurl(t *testing.T) {
	g := apiDataset(t)
	r := route(t, g, "POST /api/users")
	var body map[string]any
	if err := json.Unmarshal([]byte(r.Example.Body), &body); err != nil {
		t.Fatalf("example is not JSON: %v\n%s", err, r.Example.Body)
	}
	if body["email"] != "user@example.com" || body["role"] != "admin" || body["age"] != float64(0) || len(body["name"].(string)) < 2 {
		t.Errorf("example ignores the rules: %v", body)
	}
	if _, ok := body["secret"]; ok {
		t.Error("json:\"-\" field in the example")
	}
	if r.Example.Valid != "compatible" {
		t.Errorf("valid = %s %v", r.Example.Valid, r.Example.Notes)
	}
	want := "curl -X POST 'http://localhost:8080/api/users' \\\n  -H 'Authorization: Bearer <token>' \\\n  -H 'Content-Type: application/json' \\\n  --data-raw '{\"created_by\":"
	if !strings.HasPrefix(r.Example.Curl, want) {
		t.Errorf("curl:\n%s", r.Example.Curl)
	}
	get := route(t, g, "GET /api/users").Example.Curl
	if get != "curl 'http://localhost:8080/api/users?page=1' \\\n  -H 'Authorization: Bearer <token>'" {
		t.Errorf("GET curl:\n%s", get)
	}
	q := httpapi.Request{Method: "POST", BaseURL: "http://h/", Path: "/a/{id}", PathVal: map[string]string{"id": "x y"},
		Query: [][2]string{{"q", "it's"}}, Body: `{"n":"o'k"}`}
	if c := httpapi.Curl(q); c != "curl -X POST 'http://h/a/x%20y?q=it%27s' \\\n  --data-raw '{\"n\":\"o'\\''k\"}'" {
		t.Errorf("quoting:\n%s", c)
	}
}

func TestRouteJourney(t *testing.T) {
	g := apiDataset(t)
	ix := httpapi.NewIndex(g)
	r := route(t, g, "POST /api/users")
	j := ix.Journey(r.ID, 6, 1000)
	roles := map[string]string{}
	for _, s := range j.Steps {
		roles[s.Node.Name] = s.Role
	}
	for name, role := range map[string]string{"RequireUser.func1": "middleware", "(*UserController).Create": "handler",
		"(*UserService).Create": "call", "(*PG).Insert": "call", "pgx QueryRow": "call"} {
		found := false
		for n, r := range roles {
			if strings.HasSuffix(n, name) && r == role {
				found = true
			}
		}
		if !found {
			t.Errorf("journey misses %s (%s): %v", name, role, roles)
		}
	}
	if len(j.SQL) != 1 || !strings.Contains(j.SQL[0].Metadata["sql"].(string), "INSERT INTO users") {
		t.Errorf("SQL: %v", j.SQL)
	}
	// Every edge of the journey is an edge of the dataset between two steps.
	ids := map[string]bool{}
	for _, e := range g.Edges {
		ids[e.ID] = true
	}
	in := map[string]bool{}
	for _, s := range j.Steps {
		in[s.Node.ID] = true
	}
	for _, e := range j.Edges {
		if !ids[e.ID] || !in[e.Source] || !in[e.Target] {
			t.Errorf("edge %s is not a dataset edge between steps", e.ID)
		}
	}
	for _, e := range j.Edges {
		if strings.HasSuffix(e.Source, "(*example.com/api/services.UserService).Create") && strings.Contains(e.Target, "PG).Insert") && e.Confidence != model.Likely {
			t.Errorf("service → repository through a one-implementation interface is likely: %s", e.Confidence)
		}
	}
	if j2 := ix.Journey(r.ID, 1, 1000); len(j2.Steps) >= len(j.Steps) || !j2.Truncated {
		t.Errorf("depth 1 must cut the graph: %d steps, truncated=%v", len(j2.Steps), j2.Truncated)
	}
	if ix.Journey("func:"+api+"/controllers.Health", 3, 10) != nil {
		t.Error("journey of a non-route")
	}
}

func TestLayers(t *testing.T) {
	g := apiDataset(t)
	idx := g.Index()
	for id, layer := range map[string]string{
		"func:(*" + api + "/repository.PG).Insert":              "Repository",
		"func:(*" + api + "/controllers.UserController).Create": "Handler",
		"func:(*" + api + "/services.UserService).Create":       "Service",
	} {
		n := idx[id]
		if n == nil || n.Metadata["layer"] != layer || n.Metadata["layerBasis"] != "STATIC" {
			t.Errorf("%s: layer %v basis %v", id, n.Metadata["layer"], n.Metadata["layerBasis"])
		}
	}
}

func TestTemplates(t *testing.T) {
	for in, want := range map[string]string{
		"/users/:id": "/users/{id}", "/files/*path": "/files/{path}", "/r/{id:[0-9]+}": "/r/{id}",
		"/static/{path...}": "/static/{path}", "/exact/{$}": "/exact", "/": "/", "/a/": "/a/",
	} {
		if got := httpapi.Template(in); got != want {
			t.Errorf("Template(%q) = %q, want %q", in, got, want)
		}
	}
	if got := strings.Join(httpapi.PathParams("/a/:x/b/{y}/{dynamic}"), ","); got != "x,y" {
		t.Errorf("params %s", got)
	}
}

func TestHTTPDatasetIsValid(t *testing.T) {
	g := apiDataset(t)
	if err := g.Validate(); err != nil {
		t.Fatal(err)
	}
	if g.Stats.Routes != len(g.HTTP.Routes) {
		t.Errorf("stats routes %d", g.Stats.Routes)
	}
	// No handler of the catalog points to a node that does not exist.
	idx := g.Index()
	for _, r := range g.HTTP.Routes {
		for _, c := range append(append([]model.ChainEntry{}, r.Middlewares...), *r.Handler) {
			if c.ID != "" && idx[c.ID] == nil {
				t.Errorf("%s: chain entry %s not in the graph", r.Key, c.ID)
			}
		}
		for _, x := range r.Responses {
			if len(x.Evidence) == 0 || x.Evidence[0].File == "" {
				t.Errorf("%s %d: response without evidence", r.Key, x.Status)
			}
		}
	}
}

func TestOpenAPIExport(t *testing.T) {
	g := apiDataset(t)
	var buf strings.Builder
	if err := export.OpenAPI(&buf, g); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(buf.String()), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["openapi"] != "3.1.0" {
		t.Fatalf("openapi %v", doc["openapi"])
	}
	paths := doc["paths"].(map[string]any)
	for _, p := range []string{"/api/users", "/api/users/{id}", "/items/{id}", "/admin/stats", "/reports/{reportID}"} {
		if paths[p] == nil {
			t.Errorf("missing path %s", p)
		}
	}
	// A path computed at runtime cannot be described: it is listed apart.
	if paths["/api/kinds/{dynamic}"] != nil {
		t.Error("dynamic route in paths")
	}
	if u := doc["x-gitview-unresolved-routes"].([]any); len(u) != 1 {
		t.Errorf("unresolved routes %v", u)
	}
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	req := schemas["controllers.CreateUserRequest"].(map[string]any)
	if r, _ := json.Marshal(req["required"]); string(r) != `["name","email"]` {
		t.Errorf("required %s", r)
	}
	props := req["properties"].(map[string]any)
	if props["email"].(map[string]any)["format"] != "email" || props["name"].(map[string]any)["minLength"] != float64(2) {
		t.Errorf("enforced rules not mapped: %v %v", props["email"], props["name"])
	}
	// json.Decoder applies no tag: no required list for updateRequest.
	if _, ok := schemas["controllers.updateRequest"].(map[string]any)["required"]; ok {
		t.Error("updateRequest must not have required fields")
	}
	post := paths["/api/users"].(map[string]any)["post"].(map[string]any)
	if sec, _ := json.Marshal(post["security"]); string(sec) != `[{"bearerAuth":[]}]` {
		t.Errorf("security %s", sec)
	}
	resps := post["responses"].(map[string]any)
	for _, s := range []string{"201", "400", "401", "404", "500"} {
		if resps[s] == nil {
			t.Errorf("response %s missing", s)
		}
	}
	avatar := paths["/api/users/{id}/avatar"].(map[string]any)["post"].(map[string]any)["responses"].(map[string]any)
	if avatar["default"] == nil {
		t.Error("the dynamic status must be the default response")
	}
}

func TestPostmanExport(t *testing.T) {
	g := apiDataset(t)
	var buf strings.Builder
	if err := export.Postman(&buf, g); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["info"].(map[string]any)["schema"] != "https://schema.getpostman.com/json/collection/v2.1.0/collection.json" {
		t.Error("schema")
	}
	vars := map[string]string{}
	for _, v := range doc["variable"].([]any) {
		m := v.(map[string]any)
		vars[m["key"].(string)] = m["value"].(string)
	}
	if vars["baseUrl"] != "http://localhost:8080" || vars["token"] != "" {
		t.Errorf("variables %v", vars)
	}
	if !strings.Contains(out, `"raw": "{{baseUrl}}/api/users/:id"`) || !strings.Contains(out, `"value": "{{token}}"`) {
		t.Error("path variables or bearer auth variable missing")
	}
	if strings.Contains(out, "Bearer <token>") {
		t.Error("a credential placeholder leaked into a header instead of the auth variable")
	}
}

func TestHTMLEmbedsTheCatalog(t *testing.T) {
	g := apiDataset(t)
	var buf strings.Builder
	if err := export.HTML(&buf, g, export.HTMLOptions{}); err != nil {
		t.Fatal(err)
	}
	page := buf.String()
	start := strings.Index(page, `<script type="application/json" id="graph-data">`)
	end := strings.Index(page[start:], "</script>")
	var data struct {
		HTTP struct {
			Routes []struct {
				Key       string `json:"key"`
				Responses []struct {
					Evidence      []map[string]any `json:"evidence"`
					EvidenceTotal int              `json:"evidenceTotal"`
				} `json:"responses"`
			} `json:"routes"`
		} `json:"http"`
	}
	raw := page[start+len(`<script type="application/json" id="graph-data">`) : start+end]
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		t.Fatal(err)
	}
	if len(data.HTTP.Routes) != len(g.HTTP.Routes) {
		t.Fatalf("embedded %d routes, want %d", len(data.HTTP.Routes), len(g.HTTP.Routes))
	}
	for _, r := range data.HTTP.Routes {
		for _, x := range r.Responses {
			if len(x.Evidence) > 3 || x.EvidenceTotal < len(x.Evidence) {
				t.Errorf("%s: %d evidence entries embedded (total %d)", r.Key, len(x.Evidence), x.EvidenceTotal)
			}
			for _, ev := range x.Evidence {
				if _, ok := ev["symbol"]; ok {
					t.Errorf("%s: page evidence keeps symbol IDs", r.Key)
				}
			}
		}
	}
	if strings.Contains(page, "Bearer topsecret") || strings.Contains(page, `"send"`) {
		t.Error("unexpected content in the page")
	}
}
