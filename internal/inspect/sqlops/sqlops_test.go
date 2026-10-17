package sqlops

import (
	"strconv"
	"strings"
	"testing"

	"github.com/athos/gitview/internal/evidence"
	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/inspect/dataflow"
	"github.com/athos/gitview/internal/inspect/inspecttest"
)

func ops(t *testing.T, fn string) []*Op {
	t.Helper()
	p := inspecttest.Deep(t)
	return ForFunction(p, inspecttest.Func(t, p, fn), Options{}).Ops
}

func one(t *testing.T, fn, op string) *Op {
	t.Helper()
	for _, o := range ops(t, fn) {
		if o.Op == op {
			return o
		}
	}
	t.Fatalf("no %s in %s", op, fn)
	return nil
}

func TestLiteralQueryParamsAndScan(t *testing.T) {
	o := one(t, "repository.(*ClientRepository).FindByID", "QueryRow")
	if o.Lib != "pgx" || o.Receiver != "*pgxpool.Pool" || o.File != "repository/pg.go" || o.Line != 22 || o.Conf != evidence.Exact {
		t.Errorf("op %+v", o)
	}
	if o.SQL.Kind != TextLiteral || o.SQL.Text != "SELECT id, name FROM clients WHERE id = $1" || o.SQL.Statement != StmtSelect || o.SQL.Dollar != 1 {
		t.Errorf("sql %+v", o.SQL)
	}
	if o.SQL.Formatted != "SELECT id, name\nFROM clients\nWHERE id = $1" {
		t.Errorf("formatted:\n%s", o.SQL.Formatted)
	}
	if len(o.Params) != 1 || o.Params[0].Index != 1 || o.Params[0].Expr != "id" || o.Params[0].Type != "int64" {
		t.Errorf("params %+v", o.Params)
	}
	if o.Scan == nil || len(o.Scan.Targets) != 2 || o.Scan.Targets[0].Expr != "&c.ID" || o.Scan.Targets[1].Type != "*string" {
		t.Errorf("scan %+v", o.Scan)
	}
	if len(o.Notes) != 0 {
		t.Errorf("unexpected notes %v", o.Notes)
	}
}

func TestConstantAndDynamicSQL(t *testing.T) {
	c := one(t, "repository.(*ClientRepository).FindByEmail", "QueryRow")
	if c.SQL.Kind != TextConstant || c.SQL.Constant != "repository.findByEmailSQL" || c.SQL.ConstLine != 12 || !strings.Contains(c.SQL.Text, "WHERE email = $1") {
		t.Errorf("constant %+v", c.SQL)
	}
	s := one(t, "repository.(*ClientRepository).Search", "Query")
	if s.SQL.Kind != TextConcat || s.SQL.Text != "" || s.SQL.Template != "SELECT id, name FROM clients WHERE name LIKE '%⟨name⟩%'" {
		t.Errorf("concatenation must not be completed by guesswork: %+v", s.SQL)
	}
	if s.Scan == nil || !s.Scan.InLoop || s.Scan.Line != 49 {
		t.Errorf("rows.Scan in loop: %+v", s.Scan)
	}
	f := one(t, "repository.(*ClientRepository).ListByStatus", "Query")
	if f.SQL.Kind != TextSprintf || f.SQL.Template != "SELECT id, name FROM clients WHERE status = $1 ORDER BY ⟨order⟩" || len(f.Params) != 1 {
		t.Errorf("sprintf %+v %+v", f.SQL, f.Params)
	}
	if !strings.Contains(f.SQL.Formatted, "ORDER BY ⟨order⟩") {
		t.Errorf("hole broken by the formatter:\n%s", f.SQL.Formatted)
	}
	a := one(t, "repository.(*ClientRepository).Count", "QueryRow")
	if a.SQL.Kind != TextAlternatives || len(a.SQL.Variants) != 2 || a.SQL.Statement != StmtSelect {
		t.Errorf("alternatives %+v", a.SQL)
	}
	m := one(t, "repository.(*ClientRepository).Mismatch", "Exec")
	if len(m.Notes) != 1 || !strings.Contains(m.Notes[0], "up to $2 but 1 argument") {
		t.Errorf("placeholder mismatch not reported: %v", m.Notes)
	}
	q := one(t, "repository.(*ClientRepository).QueueAll", "Queue")
	if q.SQL.Statement != StmtUpdate || len(q.Params) != 1 {
		t.Errorf("batch queue %+v", q)
	}
}

func TestTransactions(t *testing.T) {
	p := inspecttest.Deep(t)
	r := ForFunction(p, inspecttest.Func(t, p, "repository.(*ClientRepository).Rename"), Options{})
	if len(r.Txs) != 1 {
		t.Fatalf("transactions %+v", r.Txs)
	}
	tx := r.Txs[0]
	if tx.Var != "tx" || tx.Line != 76 || len(tx.Ops) != 4 || !tx.DeferredRollback || len(tx.Commits) != 1 || tx.Commits[0] != 87 {
		t.Errorf("tx %+v", tx)
	}
	roles := map[string]string{}
	for _, o := range r.Ops {
		if o.Tx != nil {
			roles[o.Op+"@"+strconv.Itoa(o.Line)] = o.Tx.Role
		}
	}
	for k, v := range map[string]string{"Begin@76": "begin", "Rollback@80": "rollback", "Exec@81": "in transaction", "Exec@84": "in transaction", "Commit@87": "commit"} {
		if roles[k] != v {
			t.Errorf("%s role %q, want %q (all: %v)", k, roles[k], v, roles)
		}
	}
	sq := ForFunction(p, inspecttest.Func(t, p, "repository.(*AuditRepository).Purge"), Options{})
	if len(sq.Txs) != 1 || sq.Ops[0].Lib != "database/sql" || len(sq.Txs[0].Rollbacks) != 1 || sq.Txs[0].DeferredRollback {
		t.Errorf("database/sql tx %+v", sq.Txs)
	}
	ex := one(t, "repository.(*AuditRepository).Log", "Exec")
	if ex.Method != "ExecContext" || len(ex.Params) != 2 || ex.Params[1].Expr != "action" {
		t.Errorf("database/sql exec %+v", ex)
	}
}

// The SQL Explorer and Code Flow name the same operation the same way, and
// Data Flow explains where its parameters come from.
func TestReachableOpsShareCodeFlowIdentityAndDataFlowOrigins(t *testing.T) {
	p := inspecttest.Deep(t)
	fn := inspecttest.Func(t, p, "service.(*ClientService).GetClientByID")
	r := ForFunction(p, fn, Options{Reachable: true})
	if len(r.Ops) != 1 {
		t.Fatalf("reachable ops %+v", r.Ops)
	}
	o := r.Ops[0]
	if o.Conf != evidence.Likely || len(o.Via) != 1 || !strings.Contains(o.Via[0], "FindByID [likely]") {
		t.Errorf("reachable op %+v via %v", o.Conf, o.Via)
	}
	g, err := p.Query(fn, flow.Callees, flow.Options{Depth: 4, External: true})
	if err != nil {
		t.Fatal(err)
	}
	if n := g.Node(o.ID); n == nil || n.Kind != flow.KindDB || n.SQL != o.SQL.Text {
		t.Errorf("Code Flow has no node %s matching the SQL Explorer op", o.ID)
	}
	if r.Symbol.ID != p.Ref(fn).ID {
		t.Errorf("symbol identity differs")
	}
	o.TraceParams(p, dataflow.Options{MaxDepth: 4})
	var found []string
	for _, or := range o.Params[0].Origins {
		found = append(found, or.Kind+":"+or.Label+"@"+or.File)
	}
	if !strings.Contains(strings.Join(found, "\n"), `source:HTTP path parameter "id"@api/handler.go`) {
		t.Errorf("origins of $1: %v", found)
	}
}

func TestClassifyFormatAndPlaceholders(t *testing.T) {
	for sql, want := range map[string]string{
		"select 1":                                              StmtSelect,
		"  -- comment\n SELECT * FROM t":                        StmtSelect,
		"WITH x AS (SELECT 1) SELECT * FROM x":                  StmtSelect,
		"WITH d AS (DELETE FROM t RETURNING *) SELECT * FROM d": StmtWithModify,
		"insert into t values (1)":                              StmtInsert,
		"UPDATE t SET a = 1":                                    StmtUpdate,
		"delete from t":                                         StmtDelete,
		"DROP TABLE t":                                          StmtDDL,
		"BEGIN":                                                 StmtTransaction,
		"":                                                      StmtEmpty,
		"VACUUM":                                                StmtDDL,
		"(SELECT 1) UNION (SELECT 2)":                           StmtSelect,
	} {
		if got, _ := Classify(sql); got != want {
			t.Errorf("Classify(%q) = %s, want %s", sql, got, want)
		}
	}
	if _, multi := Classify("SELECT 1; DROP TABLE t"); !multi {
		t.Error("multiple statements not detected")
	}
	if _, multi := Classify("SELECT ';' ; "); multi {
		t.Error("semicolon in a string or trailing semicolon is not a second statement")
	}
	if _, multi := Classify("SELECT $$a;b$$"); multi {
		t.Error("dollar-quoted semicolon")
	}
	d, q := Placeholders("SELECT $1, '$9', $2 -- $5\n /* ? */ WHERE x = ?")
	if d != 2 || q != 1 {
		t.Errorf("placeholders %d %d", d, q)
	}
	if got := Format("select a from t where x = 1 and y = 'and from' order by a"); got != "select a\nfrom t\nwhere x = 1\n  and y = 'and from'\norder by a" {
		t.Errorf("format:\n%s", got)
	}
	if got := Format("DELETE FROM t WHERE id = $1"); got != "DELETE FROM t\nWHERE id = $1" {
		t.Errorf("format delete:\n%s", got)
	}
}
