package audit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/graph/builder"
)

func vulnDB(t testing.TB) string {
	d, _ := filepath.Abs("testdata/vulndb")
	return d
}

type want struct {
	rule, file, verdict string
	contains            string // in the message
}

func findAll(a *Audit, rule, file string) []*Finding {
	var out []*Finding
	for _, f := range a.Findings {
		if f.RuleID == rule && (file == "" || f.File == file) {
			out = append(out, f)
		}
	}
	return out
}

// Every insecure case of the fixture is reported, with the expected
// verdict, and nothing else is.
func TestRulesOnFixture(t *testing.T) {
	a := scanFixture(t, Options{VulnDB: vulnDB(t)})
	wants := []want{
		{"GO-INJ-001", "repo/repo.go", Vulnerable, `HTTP query parameter "q"`},
		{"GO-INJ-002", "handlers/handlers.go", Vulnerable, `form value "file"`},
		{"GO-INJ-003", "handlers/handlers.go", Vulnerable, `query parameter "name".`},
		{"GO-INJ-003", "handlers/handlers.go", Review, "strings.HasPrefix"},
		{"GO-INJ-004", "handlers/handlers.go", Review, `query parameter "to"`},
		{"GO-AUTHN-001", "main.go", Review, "POST /public/clients"},
		{"GO-AUTHN-002", "handlers/handlers.go", Vulnerable, `cookie "session"`},
		{"GO-AUTHN-002", "legacy/legacy.go", Vulnerable, `cookie "sid"`},
		{"GO-AUTHN-003", "middlewares/middlewares.go", Review, "ParseUnverified"},
		{"GO-AUTHZ-001", "repo/repo.go", Review, `path parameter "id"`},
		{"GO-DATA-001", "config/config.go", Review, "JWTSecret"},
		{"GO-DATA-001", "config/config.go", Review, "DatabasePassword"},
		{"GO-DATA-002", "handlers/handlers.go", Review, "500"},
		{"GO-DATA-003", "handlers/handlers.go", Review, "passwordHash"},
		{"GO-DATA-004", "middlewares/middlewares.go", Vulnerable, `"Authorization"`},
		{"GO-CONF-001", "middlewares/middlewares.go", Vulnerable, "Origin header without"},
		{"GO-CONF-001", "middlewares/middlewares.go", Review, "allowed[origin]"},
		{"GO-CONF-002", "handlers/handlers.go", Vulnerable, "InsecureSkipVerify"},
		{"GO-CONF-003", "legacy/legacy.go", Review, "ListenAndServe"},
		{"GO-CONF-003", "main.go", Review, "Run"},
		{"GO-CONF-003", "handlers/handlers.go", Review, "http.Client"},
		{"GO-CONF-004", "main.go", Review, "debug mode"},
		{"GO-CONF-004", "main.go", Review, "pprof"},
		{"GO-DEP-001", "go.mod", Review, "GO-TEST-0001"},
	}
	matched := map[*Finding]bool{}
	for _, w := range wants {
		var hit *Finding
		for _, f := range findAll(a, w.rule, w.file) {
			if !matched[f] && strings.Contains(f.Message, w.contains) {
				hit = f
				break
			}
		}
		if hit == nil {
			t.Errorf("missing %s in %s (%q)", w.rule, w.file, w.contains)
			continue
		}
		matched[hit] = true
		if hit.Verdict != w.verdict {
			t.Errorf("%s %s: verdict %s, want %s", w.rule, w.contains, hit.Verdict, w.verdict)
		}
	}
	for _, f := range a.Findings {
		if !matched[f] {
			t.Errorf("unexpected finding %s at %s:%d: %s", f.RuleID, f.File, f.Line, f.Message)
		}
	}
}

// The secure variants produce nothing: parameterised SQL, a number
// formatted into SQL, filepath.Base, a tenant filter, a role check, a
// secure cookie, names and placeholders that are not secrets, a server
// with timeouts, an advisory for another version.
func TestNoKnownFalsePositives(t *testing.T) {
	a := scanFixture(t, Options{VulnDB: vulnDB(t)})
	for _, f := range a.Findings {
		sym := ""
		if f.Symbol != nil {
			sym = f.Symbol.Name
		}
		switch {
		case sym == "SafeSearch" || sym == "Report" || sym == "SafeDownload" || sym == "FindForCompany" || sym == "GetTenantClient" || sym == "secureCookie":
			t.Errorf("%s reported on secure code %s: %s", f.RuleID, sym, f.Message)
		case strings.Contains(f.Message, "PasswordField") || strings.Contains(f.Message, "APIKeyPlaceholder"):
			t.Errorf("not a secret: %s", f.Message)
		case strings.Contains(f.Message, "GO-TEST-0002"):
			t.Error("advisory for another version reported")
		}
		for _, r := range f.Routes {
			if f.RuleID == "GO-AUTHZ-001" && (r.Key == "DELETE /admin/clients/:id" || r.Key == "GET /api/tenant/clients/:id") {
				t.Errorf("authorization finding on %s, which has a 403 check or a tenant filter", r.Key)
			}
		}
	}
	for _, r := range a.Routes {
		if r.Key == "GET /api/tenant/clients/:id" || r.Key == "DELETE /admin/clients/:id" {
			if len(r.Checks) < 2 {
				t.Errorf("%s: checks %v", r.Key, r.Checks)
			}
		}
	}
}

// Findings carry their evidence: a sink with the exact source line, the
// data path from the input for taint rules, the routes that reach them.
func TestEvidenceAndPaths(t *testing.T) {
	p, _ := fixture(t)
	a := scanFixture(t, Options{VulnDB: vulnDB(t)})
	for _, f := range a.Findings {
		if len(f.Evidence) == 0 {
			t.Errorf("%s without evidence", f.ID)
		}
		for _, e := range f.Evidence {
			if !regexp.MustCompile(`^ev-\d{3}$`).MatchString(e.ID) {
				t.Errorf("%s: evidence id %q", f.ID, e.ID)
			}
			if e.File != "" && e.Line > 0 && e.Code != "" && f.RuleID != "GO-DATA-001" && e.File != "go.mod" {
				if src := Redact(strings.TrimSpace(p.Source(e.File, e.Line, e.Line))); src != e.Code {
					t.Errorf("%s %s: code %q, source %q", f.RuleID, e.ID, e.Code, src)
				}
			}
		}
	}
	sqli := findAll(a, "GO-INJ-001", "")[0]
	if sqli.Flow[0].Role != "source" || sqli.Flow[len(sqli.Flow)-1].Role != "sink" || len(sqli.Flow) < 4 {
		t.Errorf("SQL injection flow %+v", sqli.Flow)
	}
	if len(sqli.Routes) != 1 || sqli.Routes[0].Key != "GET /api/search" {
		t.Fatalf("SQL injection routes %+v", sqli.Routes)
	}
	var roles, names []string
	for _, st := range sqli.Routes[0].Path {
		roles = append(roles, st.Role)
		names = append(names, st.Name)
	}
	if strings.Join(roles, ",") != "route,middleware,middleware,handler,function,sink" {
		t.Errorf("path roles %v (%v)", roles, names)
	}
	if !strings.Contains(sqli.Routes[0].Path[2].Check, "authentication") {
		t.Errorf("the Auth middleware is not marked as a check: %+v", sqli.Routes[0].Path[2])
	}
	// A finding inside a middleware: the path ends at that middleware.
	cors := findAll(a, "GO-CONF-001", "middlewares/middlewares.go")[0]
	for _, r := range cors.Routes {
		last := r.Path[len(r.Path)-1]
		if last.Role != "sink" || last.Name != "middlewares.CORS" && last.Name != "middlewares.CORSAllowList" {
			t.Errorf("%s: CORS path ends at %s (%s)", r.Key, last.Name, last.Role)
		}
	}
	authz := findAll(a, "GO-AUTHZ-001", "")[0]
	if authz.VerdictLabel != "Review Required — Authorization Not Established" || authz.Verdict != Review {
		t.Errorf("authorization finding %s %s", authz.VerdictLabel, authz.Verdict)
	}
	// The migrations show clients has company_id: the query not using it
	// raises the confidence, with the declaration as evidence.
	var schemaEv bool
	for _, e := range authz.Evidence {
		schemaEv = schemaEv || e.Source == "sql schema" && strings.Contains(e.Text, "company_id") && e.File == "migrations/0001_init.sql"
	}
	if authz.Confidence != ConfLikely || !schemaEv {
		t.Errorf("authorization finding: confidence %s, schema evidence %v", authz.Confidence, schemaEv)
	}
	if len(authz.Limits) == 0 || authz.AnalysisStatus != "partial" {
		t.Error("the authorization finding must state what it cannot see")
	}
	// A 403 answered before the authentication does not count as
	// authorization: GET /v2/clients/:id is reported with the others.
	var v2 bool
	for _, r := range authz.Routes {
		v2 = v2 || r.Key == "GET /v2/clients/:id"
	}
	if !v2 {
		t.Errorf("GET /v2/clients/:id is missing from the authorization finding: %+v", authz.Routes)
	}
	// The route with a call through a function value says so.
	for _, r := range a.Routes {
		if r.Key == "GET /api/dynamic" && r.Unresolved == 0 {
			t.Error("unresolved call of GET /api/dynamic not reported")
		}
	}
	if a.Status.Status != "partial" {
		t.Errorf("audit status %v: the graph has unresolved calls", a.Status)
	}
}

// IDs come from the rule, the symbol and the code of the sensitive call:
// the same code gives the same IDs, also when its lines move.
func TestStableIDs(t *testing.T) {
	a := scanFixture(t, Options{VulnDB: vulnDB(t)})
	b := scanFixture(t, Options{VulnDB: vulnDB(t)})
	ids := map[string]bool{}
	for i, f := range a.Findings {
		if f.ID != b.Findings[i].ID {
			t.Errorf("ID changed between runs: %s %s", f.ID, b.Findings[i].ID)
		}
		if ids[f.ID] {
			t.Errorf("duplicate ID %s", f.ID)
		}
		ids[f.ID] = true
		if !strings.HasPrefix(f.ID, "finding:"+f.Category+":") {
			t.Errorf("ID %s", f.ID)
		}
	}
	// Shift every line of handlers.go and scan the copy.
	dir := t.TempDir()
	copyDir(t, "testdata", dir)
	h := filepath.Join(dir, "app", "handlers", "handlers.go")
	data, _ := os.ReadFile(h)
	os.WriteFile(h, []byte(strings.Replace(string(data), "package handlers\n", "package handlers\n\n\n\n// moved\n", 1)), 0o644)
	root := filepath.Join(dir, "app")
	p, err := flow.Load(context.Background(), root, root)
	if err != nil {
		t.Fatal(err)
	}
	g, err := builder.Build(context.Background(), []*flow.Program{p}, builder.Options{Repository: "app", Root: root, Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	moved, err := Scan(context.Background(), Input{Programs: []*flow.Program{p}, Graph: g, Root: root}, Options{VulnDB: vulnDB(t), Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range a.Findings {
		if f.File != "handlers/handlers.go" {
			continue
		}
		m := moved.Finding(f.ID)
		if m == nil {
			t.Errorf("%s (%s) changed ID when its lines moved", f.ID, f.Message)
		} else if m.Line != f.Line+4 {
			t.Errorf("%s: line %d, want %d", f.ID, m.Line, f.Line+4)
		}
	}
}

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		out := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(out, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Secret values never leave the analysis.
func TestSecretsAreNeverShown(t *testing.T) {
	a := scanFixture(t, Options{VulnDB: vulnDB(t)})
	data, _ := json.Marshal(a)
	for _, secret := range []string{"fixture-signing-key-0123456789abcdef", "fixture-signing-", "key-0123456789abcdef", "pa55word-for-the-fixture", "word-for-the-fixture"} {
		if strings.Contains(string(data), secret) {
			t.Errorf("the audit contains the secret %q", secret)
		}
	}
	for _, in := range []string{`password = "hunter2hunter2"`, "token: ghp_" + strings.Repeat("a", 36), "postgres://app:s3cret@db/x", "-----BEGIN RSA PRIVATE KEY-----"} {
		if out := Redact(in); strings.Contains(out, "hunter2") || strings.Contains(out, "aaaaaaaa") || strings.Contains(out, "s3cret") || strings.Contains(out, "BEGIN RSA") {
			t.Errorf("Redact(%q) = %q", in, out)
		}
	}
}

// Rules can be selected; without a vulnerability database the dependency
// check says it did not run instead of reporting nothing.
func TestRuleSelectionAndDependencyStatus(t *testing.T) {
	a := scanFixture(t, Options{Rules: []string{"GO-INJ-001"}})
	for _, f := range a.Findings {
		if f.RuleID != "GO-INJ-001" {
			t.Errorf("rule %s ran", f.RuleID)
		}
	}
	b := scanFixture(t, Options{Disable: []string{"GO-INJ-001"}})
	if len(findAll(b, "GO-INJ-001", "")) != 0 {
		t.Error("disabled rule ran")
	}
	if b.Dependencies.Checked || !strings.Contains(b.Dependencies.Note, "not checked") {
		t.Errorf("dependency status %+v", b.Dependencies)
	}
	stale := scanFixture(t, Options{VulnDB: vulnDB(t), Now: func() time.Time { return fixedNow().AddDate(1, 0, 0) }})
	if !stale.Dependencies.Stale {
		t.Error("a database a year old is not reported as stale")
	}
}

// BenchmarkScan measures the rules on the fixture (the programs and the
// graph are loaded once).
func BenchmarkScan(b *testing.B) {
	p, g := fixture(b)
	root, _ := filepath.Abs("testdata/app")
	in := Input{Programs: []*flow.Program{p}, Graph: g, Root: root}
	vdb, _ := filepath.Abs("testdata/vulndb")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Scan(context.Background(), in, Options{VulnDB: vdb, Now: fixedNow}); err != nil {
			b.Fatal(err)
		}
	}
}
