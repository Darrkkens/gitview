package pgexplain

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/athos/gitview/internal/inspect/sqlops"
	"github.com/jackc/pgx/v5"
)

func TestTargetNeverShowsCredentials(t *testing.T) {
	for dsn, want := range map[string]struct {
		target string
		local  bool
	}{
		"postgres://bob:s3cret@localhost:5433/dev":       {"localhost:5433/dev", true},
		"postgres://bob:s3cret@127.0.0.1/dev":            {"127.0.0.1:5432/dev", true},
		"host=/run/postgresql dbname=dev user=bob":       {"unix:/run/postgresql:5432/dev", true},
		"postgres://bob:s3cret@db.example.com:5432/prod": {"db.example.com:5432/prod", false},
		"postgres://bob:s3cret@10.0.0.7/prod":            {"10.0.0.7:5432/prod", false},
	} {
		got, local, err := Target(dsn)
		if err != nil {
			t.Fatal(err)
		}
		if got != want.target || local != want.local || strings.Contains(got, "s3cret") || strings.Contains(got, "bob") {
			t.Errorf("Target(%q) = %q local=%v", dsn, got, local)
		}
	}
	if _, _, err := Target("postgres://bob:s3cret@local host:x/"); err == nil || strings.Contains(err.Error(), "s3cret") {
		t.Errorf("invalid DSN error must not echo it: %v", err)
	}
	if _, _, err := Target(""); err == nil || !strings.Contains(err.Error(), EnvDSN) {
		t.Errorf("missing DSN: %v", err)
	}
}

func TestRemoteHostsNeedExplicitPermission(t *testing.T) {
	_, err := Explain(context.Background(), Config{DSN: "postgres://u:p@db.example.com/prod"}, Request{SQL: "SELECT 1"})
	if err == nil || !strings.Contains(err.Error(), "not a local server") {
		t.Fatalf("remote host accepted: %v", err)
	}
}

func TestRequestsAreCheckedBeforeConnecting(t *testing.T) {
	for _, c := range []struct {
		req  Request
		want string
	}{
		{Request{SQL: "SELECT 1; DROP TABLE clients"}, "more than one statement"},
		{Request{SQL: "DROP TABLE clients"}, "refusing to explain a ddl"},
		{Request{SQL: "BEGIN"}, "refusing"},
		{Request{SQL: "SELECT id FROM clients WHERE name LIKE '%⟨name⟩%'"}, "partially known"},
		{Request{SQL: "SELECT * FROM t WHERE id = $1", Analyze: true}, "needs values for $1..$1"},
		{Request{SQL: "SELECT * FROM t WHERE id = $1", Params: []string{"1", "2"}}, "1 placeholder(s) but 2 value(s)"},
		{Request{SQL: "DELETE FROM t", Analyze: true}, "needs explicit permission"},
		{Request{SQL: "WITH d AS (DELETE FROM t RETURNING *) SELECT * FROM d", Analyze: true}, "needs explicit permission"},
	} {
		// The DSN points nowhere: the request must be refused before any connection.
		_, err := Explain(context.Background(), Config{DSN: "postgres://u@127.0.0.1:1/x", Timeout: time.Second}, c.req)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: got %v, want %q", c.req.SQL, err, c.want)
		}
	}
	if k, err := Check(Request{SQL: "UPDATE t SET a = 1"}); err != nil || k != sqlops.StmtUpdate {
		t.Errorf("plain EXPLAIN of an UPDATE does not execute it and is allowed: %v", err)
	}
}

const sampleAnalyze = `[{"Plan": {"Node Type": "Hash Join", "Join Type": "Inner", "Startup Cost": 1.5, "Total Cost": 30.2,
 "Plan Rows": 10, "Plan Width": 40, "Actual Startup Time": 0.02, "Actual Total Time": 0.5, "Actual Rows": 3, "Actual Loops": 1,
 "Hash Cond": "(o.client_id = c.id)",
 "Plans": [
  {"Node Type": "Seq Scan", "Parent Relationship": "Outer", "Relation Name": "orders", "Alias": "o", "Startup Cost": 0, "Total Cost": 20, "Plan Rows": 1000, "Plan Width": 8, "Filter": "(total > 10)", "Rows Removed by Filter": 7},
  {"Node Type": "Index Scan", "Parent Relationship": "Inner", "Relation Name": "clients", "Index Name": "clients_pkey", "Alias": "c", "Startup Cost": 0.1, "Total Cost": 8.3, "Plan Rows": 1, "Plan Width": 32, "Index Cond": "(id = 42)", "Shared Hit Blocks": 3}
 ]}, "Planning Time": 0.12, "Execution Time": 0.61}]`

func TestRedactParams(t *testing.T) {
	n := &PlanNode{Conditions: []Condition{{"Index Cond", "(id = '42'::bigint)"}, {"Filter", "((status = 'it''s'::text) AND (x = '4'::int))"}},
		Plans: []*PlanNode{{Keys: []Condition{{"Sort Key", "'42'"}}}}}
	redact(n, []string{"42", "it's", "4"})
	if n.Conditions[0].Value != "(id = $1::bigint)" || n.Conditions[1].Value != "((status = $2::text) AND (x = $3::int))" || n.Plans[0].Keys[0].Value != "$1" {
		t.Errorf("redacted: %+v %+v", n.Conditions, n.Plans[0].Keys)
	}
	if got := redactText(`(id = 42) AND (n = 420) AND (b = true) AND (f = 1.5)`, []string{"42", "true", "1.5"}); got != `(id = $1) AND (n = 420) AND (b = $2) AND (f = $3)` {
		t.Errorf("bare values: %s", got)
	}
	if got := redactText(`invalid input syntax for type bigint: "abc"`, []string{"abc"}); got != `invalid input syntax for type bigint: $1` {
		t.Errorf("error message: %s", got)
	}
}

func TestLexerMatchesPostgreSQL(t *testing.T) {
	for sql, multi := range map[string]bool{
		`SELECT E'\''; COMMIT; DELETE FROM t; --`:    true,
		`SELECT x$a$ FROM t; DELETE FROM t`:          true,
		`SELECT 1 /* a /* b */ ; */ ; DELETE FROM t`: true,
		"SELECT 1 -- c\r; DELETE FROM t":             true,
		`SELECT E'a\'b;c'`:                           false,
		`SELECT $q$;$q$`:                             false,
	} {
		if _, m := sqlops.Classify(sql); m != multi {
			t.Errorf("Classify(%q) multi=%v, want %v", sql, m, multi)
		}
	}
	if _, err := Check(Request{SQL: "SELECT 1\x00; DROP TABLE t"}); err == nil || !strings.Contains(err.Error(), "control character") {
		t.Errorf("control characters: %v", err)
	}
}

func TestParsePlan(t *testing.T) {
	p, err := ParsePlan([]byte(sampleAnalyze))
	if err != nil {
		t.Fatal(err)
	}
	r := p.Root
	if r.NodeType != "Hash Join" || r.JoinType != "Inner" || *r.ActualRows != 3 || r.Conditions[0].Name != "Hash Cond" || len(r.Plans) != 2 {
		t.Errorf("root %+v", r)
	}
	if *p.PlanningMs != 0.12 || *p.ExecutionMs != 0.61 {
		t.Errorf("timings %v %v", *p.PlanningMs, *p.ExecutionMs)
	}
	if strings.Join(p.Scans, "|") != "Seq Scan on orders|Index Scan on clients using clients_pkey" || strings.Join(p.Indexes, ",") != "clients_pkey" || p.Joins[0] != "Hash Join (Inner)" {
		t.Errorf("summary scans=%v joins=%v indexes=%v", p.Scans, p.Joins, p.Indexes)
	}
	seq := r.Plans[0]
	if seq.ActualRows != nil || *seq.RowsRemovedByFilter != 7 || seq.Conditions[0].Value != "(total > 10)" {
		t.Errorf("seq scan %+v", seq)
	}
	if r.Plans[1].Buffers != "shared hit=3" {
		t.Errorf("buffers %q", r.Plans[1].Buffers)
	}
	if _, err := ParsePlan([]byte(`{"nope":1}`)); err == nil {
		t.Error("invalid plan accepted")
	}
}

// Integration tests need an authorised, disposable PostgreSQL:
//
//	GITVIEW_TEST_PG_DSN=postgres://postgres:pw@127.0.0.1:55432/postgres go test ./internal/inspect/pgexplain/
func testDSN(t *testing.T) string {
	dsn := os.Getenv("GITVIEW_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("GITVIEW_TEST_PG_DSN not set: no PostgreSQL authorised for tests")
	}
	return dsn
}

func TestExplainAgainstPostgres(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	table := fmt.Sprintf("gitview_explain_test_%d", time.Now().UnixNano())
	if _, err := conn.Exec(ctx, "CREATE TABLE "+table+" (id bigint PRIMARY KEY, name text, status text)"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Exec(context.Background(), "DROP TABLE IF EXISTS "+table) })
	if _, err := conn.Exec(ctx, "INSERT INTO "+table+" SELECT g, 'n'||g, 'active' FROM generate_series(1, 2000) g; ANALYZE "+table); err != nil {
		t.Fatal(err)
	}
	cfg := Config{DSN: dsn, Timeout: 10 * time.Second}

	// Placeholders without values: a generic plan, nothing executed.
	p, err := Explain(ctx, cfg, Request{SQL: "SELECT id, name FROM " + table + " WHERE id = $1"})
	if err != nil {
		t.Fatal(err)
	}
	if !p.Generic || p.Analyzed || p.Root == nil || p.Root.ActualRows != nil || len(p.Indexes) != 1 || !strings.HasSuffix(p.Indexes[0], "_pkey") {
		t.Errorf("generic plan %+v root %+v", p, p.Root)
	}
	if strings.Contains(p.Target, "password") || p.Server == "" || p.Evidence[0].Basis != "estimated" {
		t.Errorf("plan metadata %+v", p)
	}

	// Values given: a regular plan; the values are not recorded.
	p, err = Explain(ctx, cfg, Request{SQL: "SELECT id FROM " + table + " WHERE id = $1 AND status = $2", Params: []string{"42", "active"}})
	if err != nil {
		t.Fatal(err)
	}
	if p.Generic || p.ParamsGiven != 2 {
		t.Errorf("plan with params %+v", p)
	}
	if raw, _ := json.Marshal(p); strings.Contains(string(raw), "'42'") || strings.Contains(string(raw), "'active'") {
		t.Errorf("parameter values leaked into the plan: %s", raw)
	}

	// Plain EXPLAIN of an UPDATE runs in a read-only transaction and changes nothing.
	if _, err := Explain(ctx, cfg, Request{SQL: "UPDATE " + table + " SET name = 'x'"}); err != nil {
		t.Fatalf("EXPLAIN UPDATE (not executed): %v", err)
	}

	// ANALYZE of a write needs permission, executes, and is rolled back.
	p, err = Explain(ctx, cfg, Request{SQL: "UPDATE " + table + " SET name = 'changed' WHERE id <= 10", Analyze: true, AllowWrite: true})
	if err != nil {
		t.Fatal(err)
	}
	if !p.Analyzed || p.Root.ActualRows == nil || p.ExecutionMs == nil || p.Evidence[0].Basis != "observed" {
		t.Errorf("analyze plan %+v", p)
	}
	var changed int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE name = 'changed'").Scan(&changed); err != nil || changed != 0 {
		t.Errorf("EXPLAIN ANALYZE write was not rolled back: %d rows changed (%v)", changed, err)
	}

	// Text that hides a second statement from a client-side lexer is
	// rejected by the server's own parser before anything runs.
	for _, sneaky := range []string{
		`SELECT 1 FROM ` + table + ` WHERE name = E'\'' ; COMMIT; DELETE FROM ` + table + `; --'`,
	} {
		if _, err := Explain(ctx, cfg, Request{SQL: sneaky}); err == nil {
			t.Errorf("sneaky multi-statement accepted: %s", sneaky)
		}
	}
	var left int
	conn.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&left)
	if left != 2000 {
		t.Fatalf("rows deleted by a smuggled statement: %d left", left)
	}

	// Timeouts stop long statements.
	start := time.Now()
	_, err = Explain(ctx, Config{DSN: dsn, Timeout: time.Second}, Request{SQL: "SELECT pg_sleep(5)", Analyze: true})
	if err == nil || time.Since(start) > 4*time.Second {
		t.Errorf("timeout not enforced: %v after %s", err, time.Since(start))
	}
	// Cancellation through the context.
	cctx, cancel := context.WithCancel(ctx)
	go func() { time.Sleep(300 * time.Millisecond); cancel() }()
	start = time.Now()
	_, err = Explain(cctx, Config{DSN: dsn, Timeout: 10 * time.Second}, Request{SQL: "SELECT pg_sleep(5)", Analyze: true})
	if err == nil || time.Since(start) > 3*time.Second {
		t.Errorf("cancellation not honoured: %v after %s", err, time.Since(start))
	}
}
