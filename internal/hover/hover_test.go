package hover

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/inspect/inspecttest"
)

func resolver(t *testing.T) *Resolver {
	t.Helper()
	return New([]*flow.Program{inspecttest.Deep(t)})
}

// col is the byte column (1-based) of the n-th (from 0) occurrence of
// name as a word on line of a fixture file.
func col(t *testing.T, rel string, line int, name string, n int) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(inspecttest.Dir(), rel))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	m := regexp.MustCompile(`\b`+regexp.QuoteMeta(name)+`\b`).FindAllStringIndex(lines[line-1], -1)
	if len(m) <= n {
		t.Fatalf("%s:%d has no %q #%d: %q", rel, line, name, n, lines[line-1])
	}
	return m[n][0] + 1
}

// lineOf is the first line of a fixture file containing text.
func lineOf(t *testing.T, rel, text string) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(inspecttest.Dir(), rel))
	if err != nil {
		t.Fatal(err)
	}
	for i, l := range strings.Split(string(data), "\n") {
		if strings.Contains(l, text) {
			return i + 1
		}
	}
	t.Fatalf("%s has no %q", rel, text)
	return 0
}

func lookup(t *testing.T, r *Resolver, rel string, line int, name string, n int) *Info {
	t.Helper()
	in, err := r.Lookup(rel, line, col(t, rel, line, name, n), name)
	if err != nil {
		t.Fatalf("%s:%d %s: %v", rel, line, name, err)
	}
	return in
}

func TestStandardLibraryFunction(t *testing.T) {
	r := resolver(t)
	in := lookup(t, r, "service/client.go", 33, "Errorf", 0)
	if in.Kind != "func" || in.Signature != "func fmt.Errorf(format string, a ...any) error" {
		t.Errorf("signature %q (%s)", in.Signature, in.Kind)
	}
	if !strings.HasPrefix(in.Doc, "Errorf formats according to a format specifier") {
		t.Errorf("doc %q", in.Doc)
	}
	if in.Local || !strings.HasPrefix(in.File, "$GOROOT/src/fmt/") || in.Line == 0 || in.Pkg != "fmt" {
		t.Errorf("location %s:%d local=%v pkg=%s", in.File, in.Line, in.Local, in.Pkg)
	}
}

func TestLocalMethodAndStruct(t *testing.T) {
	r := resolver(t)
	m := lookup(t, r, "service/client.go", 27, "GetClientByID", 0)
	if m.Kind != "method" || m.Signature != "func (s *ClientService) GetClientByID(ctx context.Context, id int64) (*model.Client, error)" {
		t.Errorf("method %q", m.Signature)
	}
	if m.Doc != "GetClientByID validates the id and loads the client." || !m.Local || m.File != "service/client.go" || m.Line != 27 {
		t.Errorf("method doc %q at %s:%d", m.Doc, m.File, m.Line)
	}
	if m.Symbol != "(*example.com/deep/service.ClientService).GetClientByID" {
		t.Errorf("symbol %q", m.Symbol)
	}
	c := lookup(t, r, "service/client.go", 27, "Client", 0)
	if c.Kind != "type" || c.Signature != "type model.Client struct{…}" || c.Doc != "Client is a row of the clients table." {
		t.Errorf("type %q doc %q", c.Signature, c.Doc)
	}
	if !strings.HasPrefix(c.Decl, "type Client struct {") || !strings.Contains(c.Decl, "Email") || c.File != "model/client.go" {
		t.Errorf("decl %q in %s", c.Decl, c.File)
	}
}

func TestVariablesFieldsAndConstants(t *testing.T) {
	r := resolver(t)
	if in := lookup(t, r, "service/client.go", 27, "ctx", 0); in.Kind != "var" || in.Signature != "var ctx context.Context" || in.Doc != "" {
		t.Errorf("parameter: %+v", in)
	}
	scan := lineOf(t, "repository/pg.go", "Scan(&c.ID")
	if in := lookup(t, r, "repository/pg.go", scan, "Name", 0); in.Kind != "field" || in.Signature != "field Name string" || in.File != "model/client.go" {
		t.Errorf("field: %+v", in)
	}
	in := lookup(t, r, "repository/pg.go", lineOf(t, "repository/pg.go", "(ctx, findByEmailSQL"), "findByEmailSQL", 0)
	if in.Kind != "const" || !strings.HasPrefix(in.Signature, "const findByEmailSQL untyped string = \"SELECT id") {
		t.Errorf("const: %q", in.Signature)
	}
}

func TestPredeclared(t *testing.T) {
	r := resolver(t)
	in := lookup(t, r, "service/client.go", 27, "error", 0)
	if in.Kind != "type" || in.Signature != "type error" || !strings.Contains(in.Doc, "conventional interface") || in.File != "$GOROOT/src/builtin/builtin.go" {
		t.Errorf("error: %+v", in)
	}
	if in := lookup(t, r, "service/client.go", 29, "nil", 0); in.Kind != "builtin" {
		t.Errorf("nil: %+v", in)
	}
	if in := lookup(t, r, "service/client.go", 12, "errors", 0); in.Kind != "package" || in.Signature != `package errors ("errors")` {
		t.Errorf("package: %+v", in)
	}
}

func TestStaleAndUnknown(t *testing.T) {
	r := resolver(t)
	c := col(t, "service/client.go", 33, "Errorf", 0)
	if _, err := r.Lookup("service/client.go", 33, c, "Sprintf"); !errors.Is(err, ErrNoIdent) {
		t.Errorf("another name at the position: %v", err)
	}
	if _, err := r.Lookup("service/client.go", 33, c+1, "Errorf"); !errors.Is(err, ErrNoIdent) {
		t.Errorf("another column: %v", err)
	}
	if _, err := r.Lookup("nope.go", 1, 1, "x"); !errors.Is(err, ErrNotAnalysed) {
		t.Errorf("unknown file: %v", err)
	}
	// A word inside a string literal is not an identifier.
	if _, err := r.Lookup("service/client.go", 33, col(t, "service/client.go", 33, "client", 0), "client"); !errors.Is(err, ErrNoIdent) {
		t.Errorf("word in a string: %v", err)
	}
	// The same miss in a file that changed since the analysis is stale.
	r.files["service/client.go"].size++
	if _, err := r.Lookup("service/client.go", 33, c+1, "Errorf"); !errors.Is(err, ErrStale) {
		t.Errorf("changed file: %v", err)
	}
	if rep := Answer(r, "service/client.go", 33, c+1, "Errorf"); rep.Status != "stale" {
		t.Errorf("answer for a changed file: %+v", rep)
	}
}

func TestTable(t *testing.T) {
	r := resolver(t)
	tb := r.Table(map[string][]int{"service/client.go": {27, 33}}, 0)
	m := tb.Files["service/client.go"]
	errorf := m[Key(33, col(t, "service/client.go", 33, "Errorf", 0), "Errorf")]
	if tb.Items[errorf].Name != "Errorf" {
		t.Fatalf("Errorf entry: %+v", tb.Items[errorf])
	}
	// "id" on both lines is one parameter: one item.
	a := m[Key(27, col(t, "service/client.go", 27, "id", 0), "id")]
	b := m[Key(33, col(t, "service/client.go", 33, "id", 0), "id")]
	if a != b || tb.Items[a].Signature != "var id int64" {
		t.Errorf("id: items %d and %d (%+v)", a, b, tb.Items[a])
	}
	if len(tb.Items) >= len(m) {
		t.Errorf("no item shared: %d items for %d identifiers", len(tb.Items), len(m))
	}
	if small := r.Table(map[string][]int{"service/client.go": {27, 33}}, 3); !small.Truncated || len(small.Files["service/client.go"]) != 3 {
		t.Errorf("limit: %+v", small)
	}
}
