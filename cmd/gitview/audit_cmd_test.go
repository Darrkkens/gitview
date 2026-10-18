package main

import (
	"bytes"
	"context"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/athos/gitview/internal/audit"
	"github.com/athos/gitview/internal/audit/ai"
	"github.com/athos/gitview/internal/flow"
	"github.com/charmbracelet/x/ansi"
)

const auditFixture = "../../internal/audit/testdata"

// auditRepo copies the CodeAudit fixture into a new git repository; its
// state goes to a temporary cache.
func auditRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("GITVIEW_CACHE_DIR", t.TempDir())
	t.Setenv("GITVIEW_OLLAMA_URL", "")
	t.Setenv("OLLAMA_HOST", "")
	t.Setenv("GITVIEW_AI_MODEL", "")
	root := t.TempDir()
	err := filepath.Walk(auditFixture, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(auditFixture, p)
		dst := filepath.Join(root, rel)
		if info.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	git(t, root, "init", "-q")
	git(t, root, "add", "-A")
	git(t, root, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "fixture")
	return root
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return string(out)
}

func runAuditCLI(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var out, errb bytes.Buffer
	code := runAudit(args, &out, &errb)
	return out.String(), errb.String(), code
}

var findingRe = regexp.MustCompile(`finding:[a-z-]+:[0-9a-f]{12}`)

func idFor(t *testing.T, out, rule string) string {
	t.Helper()
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		if strings.Contains(l, rule) && i+2 < len(lines) {
			if m := findingRe.FindString(lines[i+2]); m != "" {
				return m
			}
		}
	}
	t.Fatalf("no %s finding in:\n%s", rule, out)
	return ""
}

func TestAuditCLIJourney(t *testing.T) {
	root := auditRepo(t)
	app := filepath.Join(root, "app")
	vdb := filepath.Join(root, "vulndb")
	out, errs, code := runAuditCLI(t, "scan", "--module", app, "--vulndb", vdb, root)
	if code != 0 {
		t.Fatalf("scan: %d %s", code, errs)
	}
	for _, want := range []string{"CodeAudit", "GO-INJ-001", "GO-AUTHZ-001", "vulnerable (evidência suficiente)", "snapshot"} {
		if !strings.Contains(out+errs, want) {
			t.Errorf("scan output missing %q", want)
		}
	}
	sqli := idFor(t, out, "GO-INJ-001")

	crit, _, _ := runAuditCLI(t, "findings", "--severity", "critical", root)
	if !strings.Contains(crit, "GO-INJ-002") || strings.Contains(crit, "GO-INJ-001") {
		t.Errorf("findings --severity critical:\n%s", crit)
	}
	show, _, code := runAuditCLI(t, "show", sqli[len(sqli)-12:], root)
	for _, want := range []string{"Vulnerable — SQL built from untrusted input", "Caminho dos dados", "HTTP query parameter \"q\"", "Rota GET /api/search", "✓ authentication", "ev-001", "> ", "Correção:"} {
		if code != 0 || !strings.Contains(show, want) {
			t.Errorf("show missing %q (code %d)", want, code)
		}
	}
	if _, errs, code := runAuditCLI(t, "review", sqli, "--status", "confirmed", root); code != 2 || !strings.Contains(errs, "justification") {
		t.Errorf("a confirmation without a note was accepted: %d %s", code, errs)
	}
	if _, errs, code := runAuditCLI(t, "review", sqli, "--status", "confirmed", "--note", "reproduced with a quote", root); code != 0 {
		t.Fatalf("review: %s", errs)
	}
	hist, _, _ := runAuditCLI(t, "history", sqli, root)
	if !strings.Contains(hist, "open → confirmed") || !strings.Contains(hist, "reproduced with a quote") {
		t.Errorf("history:\n%s", hist)
	}
	// Fix the injection, commit, scan again, compare.
	repoFile := filepath.Join(app, "repo", "repo.go")
	data, _ := os.ReadFile(repoFile)
	fixed := strings.Replace(string(data), `"SELECT id, name FROM clients WHERE name LIKE '%"+term+"%'")`, `"SELECT id, name FROM clients WHERE name LIKE $1", "%"+term+"%")`, 1)
	if fixed == string(data) {
		t.Fatal("fixture changed: cannot fix the injection")
	}
	os.WriteFile(repoFile, []byte(fixed), 0o644)
	git(t, root, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qam", "fix")
	time.Sleep(1100 * time.Millisecond) // snapshot IDs have second precision
	if _, errs, code := runAuditCLI(t, "scan", "--module", app, "--vulndb", vdb, root); code != 0 {
		t.Fatal(errs)
	}
	diff, _, code := runAuditCLI(t, "diff", root)
	if code != 0 || !regexp.MustCompile(`Resolved findings:\s+1`).MatchString(diff) || !strings.Contains(diff, "GO-INJ-001") {
		t.Errorf("diff:\n%s", diff)
	}
	// The fixed finding is gone from the last audit and can still be
	// marked fixed.
	if _, errs, code := runAuditCLI(t, "review", sqli, "--status", "fixed", "--note", "parameterised in fix commit", root); code != 0 {
		t.Errorf("review of a resolved finding: %s", errs)
	}

	// The report: offline, only excerpts, no secret values.
	dst := filepath.Join(t.TempDir(), "security.html")
	if _, errs, code := runAuditCLI(t, "export", "--module", app, "-o", dst, root); code != 0 {
		t.Fatal(errs)
	}
	page, _ := os.ReadFile(dst)
	html := string(page)
	if secs := pageSections(t, html); secs["security"] == nil || secs["graph"] == nil || strings.Contains(html, "connect-src") || strings.Contains(html, `id="gitview-sources"`) {
		t.Error("the report must embed the audit, no whole sources, and no connect-src")
	}
	for _, secret := range []string{"fixture-signing-", "key-0123456789abcdef", "pa55", "word-for-the-fixture"} {
		if strings.Contains(html, secret) {
			t.Errorf("the report contains a piece of a secret: %q", secret)
		}
	}
	start := time.Now()
	if _, errs, code := runAuditCLI(t, "export", "--module", app, "-o", dst, root); code == 0 || !strings.Contains(errs, "--overwrite") || time.Since(start) > 2*time.Second {
		t.Errorf("existing report replaced, or refused only after the analysis: %d %s", code, errs)
	}
	// Nothing but our own change touched the repository.
	if st := git(t, root, "status", "--porcelain"); strings.TrimSpace(st) != "" {
		t.Errorf("the audit modified the repository:\n%s", st)
	}
}

// fakeOllama answers like Ollama, citing the first evidence of the context.
func fakeOllama(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			json.NewEncoder(w).Encode(map[string]any{"models": []any{map[string]any{"name": "gemma3:4b", "digest": "sha256:x", "size": 1, "details": map[string]any{"family": "gemma3"}}}})
		case "/api/chat":
			var req struct {
				Messages []struct{ Content string } `json:"messages"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			user := req.Messages[1].Content
			var c ai.Context
			json.Unmarshal([]byte(user[strings.Index(user, "<<<CONTEXT")+10:strings.Index(user, "CONTEXT>>>")]), &c)
			ans, _ := json.Marshal(map[string]any{"summary": "fake explanation", "possible_problem": "x", "why": "y", "data_path": []string{}, "confidence": "low",
				"evidence_ids": []string{c.Evidence[0].ID}, "references": []any{}, "missing_information": []string{}, "recommendations": []string{"fix"}, "suggested_tests": []string{"test"}})
			json.NewEncoder(w).Encode(map[string]any{"message": map[string]any{"role": "assistant", "content": string(ans)}, "done": true})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAuditScreen(t *testing.T) {
	root := auditRepo(t)
	t.Setenv("GITVIEW_OLLAMA_URL", fakeOllama(t).URL)
	m := newTestModel(t)
	m.repo.Root = root
	runCmd(m, m.press("s"))
	if m.screen != screenAudit || m.audit.aiStatus == nil || m.audit.aiStatus.Selected != "gemma3:4b" {
		t.Fatalf("screen %v, ollama %+v", m.screen, m.audit.aiStatus)
	}
	runCmd(m, m.press("r"))
	if m.audit.a == nil || m.audit.err != "" {
		t.Fatalf("scan: %q", m.audit.err)
	}
	view := ansi.Strip(m.View())
	for _, want := range []string{"CodeAudit", "Ollama: gemma3:4b", "findings:", "OS command built from untrusted input"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
	m.press("enter")
	if m.audit.view != "detail" || !strings.Contains(m.auditDetailText(), "Evidências") {
		t.Errorf("details not shown: view=%s", m.audit.view)
	}
	runCmd(m, m.press("g"))
	f := m.auditSelected()
	if res := m.audit.results[f.ID]; res == nil || res.Answer == nil || !res.Validation.Valid {
		t.Fatalf("AI result %+v (%s)", res, m.audit.aiErr)
	}
	if !strings.Contains(m.auditDetailText(), "fake explanation") {
		t.Error("the AI answer is not shown")
	}
	m.press("3")
	if !m.audit.reviewing {
		t.Fatal("review input not open")
	}
	for _, r := range "confirmed in test" {
		m.press(string(r))
	}
	m.press("enter")
	if m.audit.reviews.Status(f.ID) != audit.StatusConfirmed {
		t.Errorf("status %s (%s)", m.audit.reviews.Status(f.ID), m.audit.msg)
	}
	runCmd(m, m.press("x"))
	if m.audit.exported == "" {
		t.Fatalf("export: %s", m.audit.msg)
	}
	if _, err := os.Stat(m.audit.exported); err != nil {
		t.Fatal(err)
	}
	m.press("esc")
	m.press("esc")
	if m.screen != screenDiff {
		t.Error("Esc did not go back")
	}
}

func TestAuditServerGuards(t *testing.T) {
	root := auditRepo(t)
	app := filepath.Join(root, "app")
	f := auditFlags{module: app}
	store, _ := audit.StoreFor(root, "")
	cl, _ := ai.NewClient(ai.Config{Endpoint: fakeOllama(t).URL})
	w := &workspace{dir: root, root: root, f: f, store: store, progress: io.Discard, cache: flow.NewCache(),
		inv: &ai.Investigator{Client: cl, Cache: store, Read: repoReader(root)}}
	srv := w.server("security", nil)
	ln, err := srv.Listen(0)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Serve(ctx, ln)
	base := "http://" + ln.Addr().String()
	token := srv.Token()
	do := func(method, path, body string, hdr map[string]string) (int, string) {
		req, _ := http.NewRequest(method, base+path, strings.NewReader(body))
		for k, v := range hdr {
			if k == "Host" {
				req.Host = v
				continue
			}
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(data)
	}
	tok := map[string]string{"X-GitView-Token": token, "Content-Type": "application/json"}
	if code, _ := do("GET", "/", "", nil); code != 403 {
		t.Errorf("page without token: %d", code)
	}
	code, page := do("GET", "/?t="+token, "", nil)
	if code != 200 || !strings.Contains(html.UnescapeString(page), "connect-src 'self'") || !strings.Contains(page, token) {
		t.Errorf("page: %d", code)
	}
	if code, _ := do("GET", "/?t="+token, "", map[string]string{"Host": "attacker.example:80"}); code != 403 {
		t.Errorf("foreign Host accepted: %d", code)
	}
	if code, _ := do("GET", "/api/section/security", "", nil); code != 403 {
		t.Errorf("section without token: %d", code)
	}
	code, sec := do("GET", "/api/section/security", "", tok)
	var pageData struct {
		Live  bool `json:"live"`
		Audit struct {
			Findings []struct {
				ID string `json:"id"`
			} `json:"findings"`
		} `json:"audit"`
	}
	if err := json.Unmarshal([]byte(sec), &pageData); code != 200 || err != nil || !pageData.Live || len(pageData.Audit.Findings) == 0 {
		t.Fatalf("security section: %d %v %.200s", code, err, sec)
	}
	if code, g := do("GET", "/api/section/graph", "", tok); code != 200 || !strings.Contains(g, `"nodes"`) {
		t.Errorf("graph section: %d", code)
	}
	if code, st := do("GET", "/api/status", "", tok); code != 200 || !strings.Contains(st, `"security":{"state":"ready"`) {
		t.Errorf("status: %d %s", code, st)
	}
	fd := w.repo.a.Finding(pageData.Audit.Findings[0].ID)
	body := `{"finding":"` + fd.ID + `","status":"investigating","note":""}`
	if code, _ := do("POST", "/api/review", body, map[string]string{"Content-Type": "application/json"}); code != 403 {
		t.Errorf("API without token: %d", code)
	}
	if code, _ := do("POST", "/api/review", body, map[string]string{"X-GitView-Token": token, "Content-Type": "application/json", "Origin": "https://evil.example"}); code != 403 {
		t.Errorf("API from another origin: %d", code)
	}
	if code, _ := do("POST", "/api/review", body, map[string]string{"X-GitView-Token": token, "Content-Type": "text/plain"}); code != 415 {
		t.Errorf("non-JSON body: %d", code)
	}
	if code, _ := do("POST", "/api/review", strings.Repeat("x", 70<<10), tok); code != 413 {
		t.Errorf("large body: %d", code)
	}
	if code, out := do("POST", "/api/review", body, tok); code != 200 || !strings.Contains(out, "investigating") {
		t.Errorf("review: %d %s", code, out)
	}
	if rv, _ := store.Reviews(); rv.Status(fd.ID) != audit.StatusInvestigating {
		t.Error("the decision was not stored")
	}
	if code, out := do("POST", "/api/explain", `{"finding":"`+fd.ID+`"}`, tok); code != 200 || !strings.Contains(out, "fake explanation") || !strings.Contains(out, `"valid":true`) {
		t.Errorf("explain: %d %.300s", code, out)
	}
	if code, _ := do("POST", "/api/explain", `{"finding":"finding:nope"}`, tok); code != 404 {
		t.Errorf("unknown finding: %d", code)
	}
}
