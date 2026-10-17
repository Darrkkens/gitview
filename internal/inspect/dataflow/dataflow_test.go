package dataflow

import (
	"errors"
	"strings"
	"testing"

	"github.com/athos/gitview/internal/evidence"
	"github.com/athos/gitview/internal/inspect/inspecttest"
)

func trace(t *testing.T, fn, variable string, opt Options) *Result {
	t.Helper()
	p := inspecttest.Deep(t)
	r, err := Trace(p, inspecttest.Func(t, p, fn), Query{Variable: variable, Return: -1}, opt)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// reach returns the nodes reachable from the roots in one direction.
func reach(r *Result, dir Direction) []*Node {
	var out []*Node
	for _, row := range r.Tree(dir) {
		if !row.Ref {
			out = append(out, r.Node(row.Node))
		}
	}
	return out
}

func find(t *testing.T, r *Result, dir Direction, pred func(*Node) bool, what string) *Node {
	t.Helper()
	for _, n := range reach(r, dir) {
		if pred(n) {
			return n
		}
	}
	var all []string
	for _, n := range reach(r, dir) {
		all = append(all, n.Kind+" "+n.Label+" @"+n.File)
	}
	t.Fatalf("%s not found in %s trace:\n%s", what, dir, strings.Join(all, "\n"))
	return nil
}

func label(s string) func(*Node) bool { return func(n *Node) bool { return n.Label == s } }

// pathTo returns the chain of nodes from a root to id (destination edges).
func pathTo(r *Result, id int) []*Node {
	parent := map[int]int{}
	for _, row := range r.Tree(Destination) {
		if row.Edge != nil && !row.Ref {
			parent[row.Node] = row.Edge.From
		}
	}
	var out []*Node
	for cur, ok := id, true; ok; cur, ok = parent[cur] {
		out = append([]*Node{r.Node(cur)}, out...)
	}
	return out
}

func TestClientIDFromHTTPToSQL(t *testing.T) {
	r := trace(t, "api.(*Handler).GetClient", "clientID", Options{})
	if r.Selection.Name != "clientID" || r.Selection.Type != "string" || r.Symbol.ID != "(*example.com/deep/api.Handler).GetClient" {
		t.Errorf("selection %+v symbol %+v", r.Selection, r.Symbol)
	}
	src := find(t, r, Origin, func(n *Node) bool { return n.Kind == KindSource }, "HTTP source")
	if src.Label != `HTTP path parameter "id"` || !strings.Contains(src.Detail, "chi.URLParam") || src.Tags[0] != "http-input" {
		t.Errorf("source %+v", src)
	}
	sql := find(t, r, Destination, func(n *Node) bool { return n.Kind == KindSQL }, "SQL sink")
	if sql.Label != "SQL parameter $1 of pgx QueryRow" || sql.File != "repository/pg.go" || !strings.Contains(sql.Detail, "WHERE id = $1") {
		t.Errorf("sql node %+v", sql)
	}
	// The chain goes through the conversion, the service and the repository.
	var chain []string
	for _, n := range pathTo(r, sql.ID) {
		chain = append(chain, n.Label+"@"+n.FuncName)
	}
	want := []string{
		`clientID = chi.URLParam(r, "id")@api.(*Handler).GetClient`,
		"id@api.(*Handler).GetClient",
		"param id@service.(*ClientService).GetClientByID",
		"param id@repository.(*ClientRepository).FindByID",
	}
	got := strings.Join(chain, " → ")
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("chain missing %q:\n%s", w, got)
		}
	}
	// Interface dispatch with a single implementation is likely, not exact.
	if sql.Conf != evidence.Likely {
		t.Errorf("SQL sink confidence %s, want likely (via interface Repository)", sql.Conf)
	}
	var checks []string
	for _, v := range r.Validations {
		checks = append(checks, v.Text+" in "+v.Func+" ("+v.Kind+")")
	}
	for _, w := range []string{"err != nil in api.(*Handler).GetClient (error check)", "id <= 0 in service.(*ClientService).GetClientByID (comparison)"} {
		if !strings.Contains(strings.Join(checks, "\n"), w) {
			t.Errorf("validation %q missing: %v", w, checks)
		}
	}
	for _, n := range r.Nodes {
		if n.File == "" && n.Kind != KindGlobal {
			t.Errorf("node without location: %+v", n)
		}
	}
}

func TestQueryStringReachesDynamicSQL(t *testing.T) {
	r := trace(t, "api.(*Handler).SearchClients", "term", Options{})
	src := find(t, r, Origin, func(n *Node) bool { return n.Kind == KindSource }, "query source")
	if src.Label != `HTTP query parameter "q"` {
		t.Errorf("source %q", src.Label)
	}
	sql := find(t, r, Destination, func(n *Node) bool { return n.Kind == KindSQL }, "SQL text sink")
	if sql.Label != "SQL text of pgx Query" || sql.Tags[0] != "sql-text" {
		t.Errorf("sink %+v", sql)
	}
	find(t, r, Destination, func(n *Node) bool { return strings.HasPrefix(n.Code, `q := "SELECT`) }, "concatenation")
}

func TestLocalsAndConversions(t *testing.T) {
	r := trace(t, "dataflow.Locals", "trimmed", Options{})
	n := find(t, r, Destination, label("n"), "n")
	if n.Conf != evidence.Derived {
		t.Errorf("value derived through strconv.Atoi should be derived, got %s", n.Conf)
	}
	find(t, r, Destination, label("doubled = n * 2"), "doubled")
	find(t, r, Origin, label("param raw"), "origin raw")
}

func TestParamsAndReturnsAcrossCallers(t *testing.T) {
	r := trace(t, "dataflow.Wrap", "s", Options{})
	lit := find(t, r, Origin, label(`"lit"`), "constant argument")
	if lit.Kind != KindConst || lit.Level != -1 || lit.Line != 36 {
		t.Errorf("constant %+v", lit)
	}
	find(t, r, Origin, label("param p"), "caller parameter")
	ret := find(t, r, Destination, label("suffix(s)"), "return of suffix to Wrap")
	if ret.Level != 0 {
		t.Errorf("returned into Wrap at level %d", ret.Level)
	}
	find(t, r, Destination, label(`Wrap("lit")`), "returned to Caller")

	p := inspecttest.Deep(t)
	res, err := Trace(p, inspecttest.Func(t, p, "dataflow.Pointer"), Query{Return: 0}, Options{Direction: Origin})
	if err != nil {
		t.Fatal(err)
	}
	v := find(t, res, Origin, label("param v"), "value written through the pointer")
	if v.Conf != evidence.Exact {
		t.Errorf("single reaching store should be exact, got %s", v.Conf)
	}
	for _, n := range reach(res, Origin) {
		if n.Label == "0" {
			t.Errorf("the initial x := 0 is overwritten before the read and must not be an origin")
		}
	}
}

func TestStructFieldsAndPointerParams(t *testing.T) {
	r := trace(t, "dataflow.Fields", "name", Options{})
	f := find(t, r, Destination, func(n *Node) bool { return n.Kind == KindField }, "field c.Name")
	if f.Label != "c.Name" {
		t.Errorf("field label %q", f.Label)
	}
	find(t, r, Destination, func(n *Node) bool { return n.Kind == KindReturn }, "returned field")

	s := trace(t, "dataflow.SetName", "name", Options{})
	u := find(t, s, Destination, func(n *Node) bool { return n.Kind == KindUnresolved }, "write through pointer param")
	if !strings.Contains(u.Label, "param c") || u.Conf != evidence.Possible {
		t.Errorf("pointer param write: %+v", u)
	}
	db := trace(t, "repository.(*ClientRepository).FindByID", "c", Options{})
	row := find(t, db, Origin, func(n *Node) bool { return n.Kind == KindSource }, "Scan source")
	if !strings.Contains(row.Label, "database row") || row.Tags[0] != "db-result" {
		t.Errorf("db source %+v", row)
	}
}

func TestInterfacesUnresolvedAndClosures(t *testing.T) {
	r := trace(t, "dataflow.Iface", "key", Options{})
	put := find(t, r, Destination, func(n *Node) bool { return n.FuncName == "dataflow.(*memStore).Put" && n.Kind == KindParam }, "implementation param")
	if put.Conf != evidence.Likely {
		t.Errorf("single implementation → likely, got %s", put.Conf)
	}
	d := trace(t, "dataflow.Dynamic", "in", Options{})
	u := find(t, d, Destination, func(n *Node) bool { return n.Kind == KindUnresolved }, "call through function value")
	if u.Conf != evidence.Unresolved {
		t.Errorf("function value call: %+v", u)
	}
	for _, n := range d.Nodes {
		if n.Kind == KindReturn {
			t.Errorf("the result of an unknown function must not be attributed to the input: %+v", n)
		}
	}
	c := trace(t, "dataflow.Closure", "in", Options{})
	find(t, c, Destination, func(n *Node) bool { return n.Label == `in + "?"` && strings.HasSuffix(n.FuncName, ".func1") }, "use inside the closure")
}

func TestShadowedVariablesAreNotPickedArbitrarily(t *testing.T) {
	p := inspecttest.Deep(t)
	fn := inspecttest.Func(t, p, "dataflow.Shadow")
	_, err := Trace(p, fn, Query{Variable: "v", Return: -1}, Options{})
	var amb *AmbiguousError
	if !errors.As(err, &amb) || len(amb.Candidates) != 2 || !strings.Contains(err.Error(), "--line") {
		t.Fatalf("expected ambiguity, got %v", err)
	}
	r, err := Trace(p, fn, Query{Variable: "v", Line: 84, Return: -1}, Options{})
	if err != nil || r.Selection.Line != 84 {
		t.Fatalf("line selection: %v %+v", err, r)
	}
	if _, err := Trace(p, fn, Query{Variable: "nope", Return: -1}, Options{}); err == nil || !strings.Contains(err.Error(), "variables: a, v") {
		t.Errorf("unknown variable error: %v", err)
	}
	vars := Variables(p, inspecttest.Func(t, p, "api.(*Handler).GetClient"))
	var names []string
	for _, v := range vars {
		names = append(names, v.Kind+":"+v.Name)
	}
	if got := strings.Join(names, ","); got != "param:h,param:w,param:r,local:clientID,local:id,local:err,local:c" {
		t.Errorf("variables: %s", got)
	}
}

func TestDepthAndNodeLimitsArePartial(t *testing.T) {
	r := trace(t, "api.(*Handler).GetClient", "clientID", Options{MaxDepth: 1, Direction: Destination})
	for _, n := range r.Nodes {
		if n.Kind == KindSQL {
			t.Fatal("SQL is two calls away and must not be reached with depth 1")
		}
	}
	if r.Status.Status != evidence.Partial || !strings.Contains(r.Status.String(), "depth limit") {
		t.Errorf("status %v", r.Status)
	}
	small := trace(t, "api.(*Handler).GetClient", "clientID", Options{MaxNodes: 3})
	if len(small.Nodes) > 3 || small.Status.Status != evidence.Partial {
		t.Errorf("node limit: %d nodes, %v", len(small.Nodes), small.Status)
	}
}

func TestExpressionSelectionAndIntermediateFilter(t *testing.T) {
	p := inspecttest.Deep(t)
	fn := inspecttest.Func(t, p, "repository.(*ClientRepository).FindByID")
	// The id argument of QueryRow at repository/pg.go:22.
	col := strings.Index(p.Source("repository/pg.go", 22, 22), "id).Scan") + 1
	r, err := Trace(p, fn, Query{File: "repository/pg.go", Line: 22, Col: col, Return: -1}, Options{Direction: Origin})
	if err != nil {
		t.Fatal(err)
	}
	if r.Selection.Kind != "expr" || r.Selection.Name != "id" {
		t.Errorf("selection %+v", r.Selection)
	}
	find(t, r, Origin, func(n *Node) bool { return n.Kind == KindSource && strings.Contains(n.Label, "HTTP path parameter") }, "HTTP origin of the SQL parameter")

	full := trace(t, "api.(*Handler).GetClient", "clientID", Options{})
	slim := full.HideIntermediate()
	if len(slim.Edges) >= len(full.Edges) {
		t.Errorf("intermediate values not removed: %d → %d edges", len(full.Edges), len(slim.Edges))
	}
	sql := find(t, slim, Destination, func(n *Node) bool { return n.Kind == KindSQL }, "SQL after hiding intermediates")
	for _, n := range pathTo(slim, sql.ID) {
		if n.Intermediate {
			t.Errorf("intermediate node %q still on the path", n.Label)
		}
	}
}

func origins(r *Result) []string {
	var out []string
	for _, n := range reach(r, Origin) {
		if n.Kind == KindConst || n.Kind == KindSource || n.Kind == KindParam {
			out = append(out, n.Label+"["+n.Conf+"]")
		}
	}
	return out
}

func TestCallContextIsKept(t *testing.T) {
	r := trace(t, "dataflow.ContextA", "x", Options{Direction: Origin, MaxDepth: 4})
	got := strings.Join(origins(r), " ")
	if !strings.Contains(got, `"a"`) || strings.Contains(got, `"b"`) {
		t.Errorf("origins of x in ContextA: %s", got)
	}
	for _, e := range r.Edges {
		if strings.Contains(e.Label, " at :0") {
			t.Errorf("call site without location: %+v", e)
		}
	}
}

func TestLoopCarriedMemory(t *testing.T) {
	// total += a.n reads a.n, written before the loop and later in the body.
	r := trace(t, "dataflow.LoopCarried", "total", Options{Direction: Origin})
	var labels []string
	for _, n := range reach(r, Origin) {
		labels = append(labels, n.Label+"["+n.Conf+"]")
	}
	all := strings.Join(labels, " ")
	if !strings.Contains(all, " 1[") || !strings.Contains(all, " x[") {
		t.Errorf("both the initial write and the loop-carried write must be origins: %s", all)
	}
	if strings.Contains(all, " 1[exact]") {
		t.Errorf("two writes reach the read: neither is exact: %s", all)
	}
	// Selecting the field read itself traces the memory location.
	p := inspecttest.Deep(t)
	col := strings.Index(p.Source("dataflow/cases.go", 111, 111), "a.n") + 3
	res, err := Trace(p, inspecttest.Func(t, p, "dataflow.LoopCarried"), Query{File: "dataflow/cases.go", Line: 111, Col: col, Return: -1}, Options{Direction: Origin})
	if err != nil || res.Selection.Name != "a.n" || res.Selection.Type != "int" {
		t.Fatalf("selection %+v %v", res.Selection, err)
	}
	if len(reach(res, Origin)) < 3 {
		t.Errorf("field read has no origins: %+v", reach(res, Origin))
	}
}

func TestBoundMethodReceiverAndClosureExpressions(t *testing.T) {
	p := inspecttest.Deep(t)
	r, err := Trace(p, inspecttest.Func(t, p, "dataflow.(*Greeter).Hello"), Query{Variable: "g", Return: -1}, Options{Direction: Origin})
	if err != nil {
		t.Fatal(err)
	}
	find(t, r, Origin, label("param x"), "receiver bound in Bound")

	line := 0
	for l := 100; l < 220; l++ {
		if strings.Contains(p.Source("dataflow/cases.go", l, l), "strings.ToUpper(in)") {
			line = l
		}
	}
	col := strings.Index(p.Source("dataflow/cases.go", line, line), "ToUpper(in)") + len("ToUpper(") + 1
	res, err := Trace(p, inspecttest.Func(t, p, "dataflow.Outer"), Query{File: "dataflow/cases.go", Line: line, Col: col, Return: -1}, Options{Direction: Origin})
	if err != nil {
		t.Fatal(err)
	}
	if res.Selection.Name != "in" || !strings.HasSuffix(res.Symbol.ID, "Outer$1") {
		t.Errorf("selection inside the closure: %+v in %s", res.Selection, res.Symbol.ID)
	}
	if _, err := Trace(p, inspecttest.Func(t, p, "dataflow.Locals"), Query{File: "dataflow/cases.go", Line: line, Col: col, Return: -1}, Options{}); err == nil {
		t.Error("a position outside the function must be refused")
	}
}
