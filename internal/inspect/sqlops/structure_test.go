package sqlops

import (
	"reflect"
	"testing"
)

func TestStructureOfSelect(t *testing.T) {
	s := StructureOf(`SELECT u.id, u.total, p.quota FROM usage u JOIN plans p ON p.id = u.plan_id
		LEFT JOIN "billing"."discounts" AS d USING (plan_id)
		WHERE u.company_id = $1 AND u.period BETWEEN $2 AND $3 AND u.deleted_at IS NULL
		GROUP BY u.id, p.quota HAVING count(*) > 1 ORDER BY u.created_at DESC, u.id LIMIT 10 OFFSET $4 FOR UPDATE OF u`)
	if s.Statement != StmtSelect || s.Partial {
		t.Errorf("statement %q partial %v", s.Statement, s.Partial)
	}
	wantTables := []TableRef{{Name: "usage", Alias: "u", Role: "from"}, {Name: "plans", Alias: "p", Role: "join"}, {Name: `"billing"."discounts"`, Alias: "d", Role: "join"}}
	if !reflect.DeepEqual(s.Tables, wantTables) {
		t.Errorf("tables %+v", s.Tables)
	}
	wantJoins := []Join{{Kind: "JOIN", Table: "plans", Alias: "p", On: "p.id = u.plan_id"}, {Kind: "LEFT JOIN", Table: `"billing"."discounts"`, Alias: "d", On: "USING (plan_id)"}}
	if !reflect.DeepEqual(s.Joins, wantJoins) {
		t.Errorf("joins %+v", s.Joins)
	}
	check := func(name string, got, want any) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %#v, want %#v", name, got, want)
		}
	}
	check("columns", s.Columns, []string{"u.id", "u.total", "p.quota"})
	check("filters", s.Filters, []string{"u.company_id = $1", "u.period BETWEEN $2 AND $3", "u.deleted_at IS NULL"})
	check("group", s.GroupBy, []string{"u.id", "p.quota"})
	check("having", s.Having, "count(*) > 1")
	check("order", s.OrderBy, []string{"u.created_at DESC", "u.id"})
	check("limit", s.Limit, "10")
	check("offset", s.Offset, "$4")
	check("lock", s.Lock, "FOR UPDATE OF u")
}

func TestStructureOfOtherStatements(t *testing.T) {
	s := StructureOf(`WITH recent AS (SELECT id FROM orders WHERE created_at > now() - interval '1 day')
		UPDATE accounts a SET balance = balance - o.total, updated_at = now() FROM recent r JOIN orders o ON o.id = r.id
		WHERE a.id = o.account_id OR a.flag RETURNING a.id`)
	if !reflect.DeepEqual(s.CTEs, []string{"recent"}) {
		t.Errorf("ctes %v", s.CTEs)
	}
	want := []TableRef{{Name: "orders", Role: "from", Depth: 1}, {Name: "accounts", Alias: "a", Role: "update"}, {Name: "orders", Alias: "o", Role: "join"}}
	if !reflect.DeepEqual(s.Tables, want) {
		t.Errorf("tables %+v", s.Tables)
	}
	if !reflect.DeepEqual(s.Columns, []string{"balance", "updated_at"}) || len(s.Filters) != 1 || !reflect.DeepEqual(s.Returning, []string{"a.id"}) {
		t.Errorf("update: columns %v filters %v returning %v", s.Columns, s.Filters, s.Returning)
	}

	s = StructureOf(`INSERT INTO charges (usage_id, amount) VALUES ($1, $2) ON CONFLICT (usage_id) DO UPDATE SET amount = EXCLUDED.amount RETURNING id`)
	if !reflect.DeepEqual(s.Tables, []TableRef{{Name: "charges", Role: "insert"}}) || !reflect.DeepEqual(s.Columns, []string{"usage_id", "amount"}) {
		t.Errorf("insert: %+v %v", s.Tables, s.Columns)
	}
	if s.Conflict != "ON CONFLICT (usage_id) DO UPDATE SET amount = EXCLUDED.amount" || !reflect.DeepEqual(s.Returning, []string{"id"}) {
		t.Errorf("conflict %q returning %v", s.Conflict, s.Returning)
	}

	s = StructureOf(`DELETE FROM sessions s USING users u WHERE s.user_id = u.id AND u.disabled; SELECT 1`)
	if !reflect.DeepEqual(s.Tables, []TableRef{{Name: "sessions", Alias: "s", Role: "delete"}, {Name: "users", Alias: "u", Role: "using"}}) || len(s.Filters) != 2 || len(s.Notes) != 1 {
		t.Errorf("delete: %+v %v %v", s.Tables, s.Filters, s.Notes)
	}

	// Functions using FROM, a derived table, comma joins, runtime parts.
	s = StructureOf(`SELECT extract(year FROM t.at), x.n FROM t, (SELECT count(*) n FROM logs) x WHERE t.a IS DISTINCT FROM ⟨col⟩ ORDER BY ⟨order⟩`)
	want = []TableRef{{Name: "t", Role: "from"}, {Name: "logs", Role: "from", Depth: 1}}
	if !reflect.DeepEqual(s.Tables, want) || !s.Partial {
		t.Errorf("tables %+v partial %v", s.Tables, s.Partial)
	}
	if !reflect.DeepEqual(s.OrderBy, []string{"⟨order⟩"}) {
		t.Errorf("order %v", s.OrderBy)
	}
}

func TestSchemaIndexes(t *testing.T) {
	file := `-- schema
CREATE TABLE usage (
  id bigserial PRIMARY KEY,
  company_id bigint NOT NULL,
  period text,
  note text DEFAULT 'a;b',
  CONSTRAINT usage_company_period UNIQUE (company_id, period),
  CHECK (period <> '')
);

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_period ON ONLY public.usage USING btree (period DESC NULLS LAST, lower(note)) INCLUDE (company_id) WHERE period IS NOT NULL;
CREATE INDEX ON plans (quota);
ALTER TABLE ONLY charges ADD CONSTRAINT charges_pkey PRIMARY KEY (id);
DROP INDEX IF EXISTS idx_old, idx_older;
CREATE FUNCTION f() RETURNS void AS $$ BEGIN CREATE INDEX nope ON x (y); END $$ LANGUAGE plpgsql;
`
	stmts := Statements(file)
	if len(stmts) != 6 || stmts[0].Line != 2 || stmts[1].Line != 11 || stmts[4].Line != 14 {
		for _, s := range stmts {
			t.Logf("%d: %.40q", s.Line, s.Text)
		}
		t.Fatalf("%d statements", len(stmts))
	}
	var defs []IndexDef
	var dropped []string
	for _, s := range stmts {
		ch := Indexes(s.Text)
		defs = append(defs, ch.Defs...)
		dropped = append(dropped, ch.Dropped...)
	}
	want := []IndexDef{
		{Table: "usage", Columns: []string{"id"}, Unique: true, Primary: true, Implicit: true},
		{Name: "usage_company_period", Table: "usage", Columns: []string{"company_id", "period"}, Unique: true, Implicit: true},
		{Name: "idx_usage_period", Table: "public.usage", Columns: []string{"period", "lower(note)"}, Unique: true, Method: "btree", Include: []string{"company_id"}, Where: "period IS NOT NULL"},
		{Table: "plans", Columns: []string{"quota"}},
		{Name: "charges_pkey", Table: "charges", Columns: []string{"id"}, Unique: true, Primary: true, Implicit: true},
	}
	if !reflect.DeepEqual(defs, want) {
		for _, d := range defs {
			t.Logf("%+v", d)
		}
		t.Error("index definitions differ")
	}
	if !reflect.DeepEqual(dropped, []string{"idx_old", "idx_older"}) {
		t.Errorf("dropped %v", dropped)
	}
}
