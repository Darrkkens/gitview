package study

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/athos/gitview/internal/inspect/pgexplain"
	"github.com/athos/gitview/internal/inspect/sqlops"
)

// IndexMatch is an index declared in the project's SQL files on a table a
// query names. Referenced lists, as text matches, where its columns appear
// in the query; whether PostgreSQL uses it is only known from a plan.
type IndexMatch struct {
	sqlops.IndexDef
	File       string   `json:"file"`
	Line       int      `json:"line"`
	Referenced []string `json:"referenced,omitempty"` // "period (WHERE)"
	InPlan     bool     `json:"inPlan,omitempty"`     // named by the EXPLAIN plan of the query
	Dropped    string   `json:"dropped,omitempty"`    // file:line of a DROP INDEX with its name
}

// PlanSummary is the EXPLAIN plan recorded for a query.
type PlanSummary struct {
	Mode        string   `json:"mode"`
	Analyzed    bool     `json:"analyzed"`
	Generic     bool     `json:"generic,omitempty"`
	Scans       []string `json:"scans,omitempty"`
	Joins       []string `json:"joins,omitempty"`
	Indexes     []string `json:"indexes,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
	PlanningMs  *float64 `json:"planningMs,omitempty"`
	ExecutionMs *float64 `json:"executionMs,omitempty"`
	Evidence    Evidence `json:"evidence"`
}

// CallerRef is a function that calls the one running a query.
type CallerRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	File string `json:"file"`
	Line int    `json:"line"`
}

// SQLQuery is one database operation with SQL.
type SQLQuery struct {
	ID        string            `json:"id"` // Code Flow node of the operation
	Func      string            `json:"func"`
	FuncName  string            `json:"funcName"`
	File      string            `json:"file"`
	Line      int               `json:"line"`
	Lib       string            `json:"lib"`
	Method    string            `json:"method"`
	TextKind  string            `json:"textKind"` // literal, constant, concatenation...
	SQL       string            `json:"sql,omitempty"`
	Variants  int               `json:"variants,omitempty"`
	Statement string            `json:"statement"`
	InReport  bool              `json:"inReport"` // run by a function of the reading order
	Structure *sqlops.Structure `json:"structure,omitempty"`
	Params    []string          `json:"params,omitempty"`
	Scan      []string          `json:"scan,omitempty"`
	Tx        string            `json:"tx,omitempty"`
	Callers   []CallerRef       `json:"callers,omitempty"`
	Indexes   []IndexMatch      `json:"indexes,omitempty"`
	Plan      *PlanSummary      `json:"plan,omitempty"`
	Evidence  []Evidence        `json:"evidence"`
	Notes     []string          `json:"notes,omitempty"`
}

// SQLStudy lists the queries of the module with their structure.
type SQLStudy struct {
	Queries     []SQLQuery `json:"queries,omitempty"`
	SchemaFiles []string   `json:"schemaFiles,omitempty"` // .sql files read for indexes
	Tables      []string   `json:"tables,omitempty"`      // tables created in them or named by queries
	Indexes     int        `json:"indexes"`
	Notes       []string   `json:"notes,omitempty"`
}

const (
	maxSQLQueries   = 60
	maxSchemaFiles  = 500
	maxSchemaBytes  = 2 << 20
	maxQueryCallers = 8
)

type schemaIndex struct {
	def  sqlops.IndexDef
	file string
	line int
}

// scanSchema reads the index definitions of the project's .sql files
// (vendor, node_modules, testdata and hidden directories excluded).
func scanSchema(root string) (defs []schemaIndex, drops map[string]string, files, tables, notes []string) {
	drops = map[string]string{}
	if root == "" {
		return
	}
	n := 0
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if p != root && (strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules" || name == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(name), ".sql") || !d.Type().IsRegular() {
			return nil
		}
		if n++; n > maxSchemaFiles {
			notes = append(notes, "more than 500 .sql files: the remaining ones were not read for indexes")
			return filepath.SkipAll
		}
		info, err := d.Info()
		if err != nil || info.Size() > maxSchemaBytes {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		files = append(files, rel)
		for _, st := range sqlops.Statements(string(data)) {
			ch := sqlops.Indexes(st.Text)
			if ch.Table != "" {
				tables = append(tables, ch.Table)
			}
			for _, def := range ch.Defs {
				defs = append(defs, schemaIndex{def, rel, st.Line})
			}
			for _, name := range ch.Dropped {
				drops[normName(name)] = loc(rel, st.Line)
			}
		}
		return nil
	})
	return
}

// normName compares SQL identifiers: unquoted, lower case, without schema.
func normName(s string) string {
	if i := strings.LastIndexByte(s, '.'); i >= 0 {
		s = s[i+1:]
	}
	return strings.ToLower(strings.Trim(s, `"`))
}

func normSQL(s string) string { return strings.Join(strings.Fields(s), " ") }

func sqlStudy(in Inputs, x *graph, order []string) SQLStudy {
	var st SQLStudy
	defs, drops, files, tables, notes := scanSchema(in.Root)
	st.SchemaFiles, st.Indexes, st.Notes = files, len(defs), notes
	inReport := map[string]bool{}
	for _, id := range order {
		inReport[id] = true
	}
	// Operations of the report's functions first (already analysed, with
	// parameter origins), then the rest of the module.
	var ops []*sqlops.Op
	seen := map[string]bool{}
	for _, b := range orderBundles(in.Bundles, order) {
		if b.SQL == nil {
			continue
		}
		for _, op := range b.SQL.Ops {
			if !seen[op.ID] {
				seen[op.ID] = true
				ops = append(ops, op)
			}
		}
	}
	var rest []*sqlops.Op
	for _, p := range in.Programs {
		for _, op := range sqlops.All(p) {
			if !seen[op.ID] {
				seen[op.ID] = true
				rest = append(rest, op)
			}
		}
	}
	sort.SliceStable(rest, func(i, j int) bool {
		a, b := inReport[rest[i].Func.ID], inReport[rest[j].Func.ID]
		return a && !b
	})
	ops = append(ops, rest...)
	plans := map[string]*pgexplain.Plan{}
	for _, p := range in.Plans {
		plans[normSQL(p.SQL)] = p // the latest plan of a statement wins
	}
	for _, op := range ops {
		if op.SQL.Kind == sqlops.TextNone {
			continue
		}
		if len(st.Queries) >= maxSQLQueries {
			st.Notes = append(st.Notes, "more than 60 database operations: the remaining ones are listed in the SQL Explorer of each function")
			break
		}
		st.Queries = append(st.Queries, query(op, x, inReport, defs, drops, plans))
	}
	// Known tables, by normalised name, first spelling kept.
	seenT := map[string]bool{}
	addT := func(t string) {
		if k := normName(t); k != "" && !seenT[k] && !strings.HasSuffix(t, ")") {
			seenT[k] = true
			st.Tables = append(st.Tables, t)
		}
	}
	for _, t := range tables {
		addT(t)
	}
	for _, q := range st.Queries {
		if q.Structure != nil {
			for _, t := range q.Structure.Tables {
				addT(t.Name)
			}
		}
	}
	st.Notes = append(st.Notes, "Indexes are read from CREATE INDEX, PRIMARY KEY and UNIQUE in the project's .sql files, in any order: migrations applied elsewhere, or later dropped, may differ from the database. A column matching a query does not mean the index is used; only an EXPLAIN plan shows that.")
	return st
}

func query(op *sqlops.Op, x *graph, inReport map[string]bool, defs []schemaIndex, drops map[string]string, plans map[string]*pgexplain.Plan) SQLQuery {
	q := SQLQuery{ID: op.ID, Func: op.Func.ID, FuncName: op.Func.Qualified, File: op.File, Line: op.Line, Lib: op.Lib, Method: op.Method,
		TextKind: op.SQL.Kind, Statement: op.SQL.Statement, InReport: inReport[op.Func.ID], Notes: append([]string(nil), op.Notes...)}
	if n := x.nodes[op.Func.ID]; n != nil {
		q.FuncName = n.Name
	}
	q.Evidence = append(q.Evidence, Evidence{Basis: SQLBasis, Text: "SQL read from the code (" + op.SQL.Kind + ") in " + q.FuncName, File: op.File, Line: op.Line, Ref: "inspect:" + op.Func.ID + ":sql"})
	text := op.SQL.Text
	switch {
	case text != "":
		q.SQL = op.SQL.Formatted
		if q.SQL == "" {
			q.SQL = text
		}
	case len(op.SQL.Variants) > 0:
		text = op.SQL.Variants[0]
		q.SQL, q.Variants = sqlops.Format(text), len(op.SQL.Variants)
		q.Notes = append(q.Notes, "the SQL is one of "+itoa(len(op.SQL.Variants))+" alternatives depending on the path; the structure describes the first")
	case op.SQL.Template != "":
		text = op.SQL.Template
		q.SQL = op.SQL.Template
	default:
		q.Notes = append(q.Notes, "SQL text not known statically ("+op.SQL.Kind+"): structure, tables and indexes unknown")
	}
	if op.SQL.Note != "" {
		q.Notes = append(q.Notes, op.SQL.Note)
	}
	if text != "" {
		q.Structure = sqlops.StructureOf(text)
	}
	for _, p := range op.Params {
		s := "$" + itoa(p.Index) + " = " + p.Expr
		if p.Type != "" {
			s += " (" + p.Type + ")"
		}
		q.Params = append(q.Params, s)
	}
	if op.Scan != nil {
		for _, t := range op.Scan.Targets {
			q.Scan = append(q.Scan, t.Expr)
		}
	}
	if op.Tx != nil {
		q.Tx = op.Tx.Role
	}
	for _, e := range x.callers(op.Func.ID) {
		if len(q.Callers) >= maxQueryCallers {
			break
		}
		if n := x.nodes[e.From]; n != nil && x.isModuleFunc(n.ID) {
			q.Callers = append(q.Callers, CallerRef{ID: n.ID, Name: n.Name, File: e.File, Line: e.Line})
		}
	}
	plan := plans[normSQL(op.SQL.Text)]
	if op.SQL.Text != "" && plan != nil {
		ps := &PlanSummary{Mode: plan.Mode, Analyzed: plan.Analyzed, Generic: plan.Generic, Scans: plan.Scans, Joins: plan.Joins, Indexes: plan.Indexes, Warnings: plan.Warnings, PlanningMs: plan.PlanningMs, ExecutionMs: plan.ExecutionMs,
			Evidence: Evidence{Basis: SQLBasis, Text: "EXPLAIN plan (the planner's estimate; the statement was not executed)", Ref: "inspect:" + op.Func.ID + ":sql"}}
		if plan.Analyzed {
			ps.Evidence = Evidence{Basis: Runtime, Text: "EXPLAIN ANALYZE (the statement was executed to measure it)", Ref: "inspect:" + op.Func.ID + ":sql"}
		}
		q.Plan = ps
	}
	if q.Structure != nil {
		q.Indexes = indexMatches(q.Structure, defs, drops, plan)
	}
	return q
}

// indexMatches lists the indexes on the tables of a query and where their
// columns appear in its clauses (identifier match, qualified by the
// table's alias or name, or unqualified).
func indexMatches(s *sqlops.Structure, defs []schemaIndex, drops map[string]string, plan *pgexplain.Plan) []IndexMatch {
	var out []IndexMatch
	done := map[int]bool{}
	for _, t := range s.Tables {
		quals := map[string]bool{normName(t.Name): true}
		if t.Alias != "" {
			quals[strings.ToLower(strings.Trim(t.Alias, `"`))] = true
		}
		for i, d := range defs {
			if done[i] || normName(d.def.Table) != normName(t.Name) {
				continue
			}
			done[i] = true
			m := IndexMatch{IndexDef: d.def, File: d.file, Line: d.line}
			if d.def.Name != "" {
				m.Dropped = drops[normName(d.def.Name)]
				if plan != nil {
					for _, used := range plan.Indexes {
						if normName(used) == normName(d.def.Name) {
							m.InPlan = true
						}
					}
				}
			}
			for _, col := range d.def.Columns {
				for _, c := range clausesOf(s) {
					for _, text := range c.texts {
						if mentions(text, col, quals) {
							m.Referenced = append(m.Referenced, col+" ("+c.name+")")
							break
						}
					}
				}
			}
			out = append(out, m)
		}
	}
	return out
}

type clauseTexts struct {
	name  string
	texts []string
}

func clausesOf(s *sqlops.Structure) []clauseTexts {
	var on []string
	for _, j := range s.Joins {
		on = append(on, j.On)
	}
	return []clauseTexts{{"WHERE", s.Filters}, {"JOIN", on}, {"ORDER BY", s.OrderBy}, {"GROUP BY", s.GroupBy}}
}

// mentions reports whether text names column col, qualified with one of
// quals or unqualified. Only plain identifiers are matched.
func mentions(text, col string, quals map[string]bool) bool {
	col = strings.ToLower(strings.Trim(col, `"`))
	if !isIdent(col) {
		return false
	}
	for _, w := range identifiers(text) {
		w = strings.ToLower(w)
		if w == col {
			return true
		}
		if i := strings.LastIndexByte(w, '.'); i >= 0 && w[i+1:] == col && quals[w[:i]] {
			return true
		}
	}
	return false
}

func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		if !(c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || i > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// identifiers splits text into dotted identifiers, skipping string literals.
func identifiers(text string) []string {
	var out []string
	var cur strings.Builder
	inStr := false
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, strings.ReplaceAll(cur.String(), `"`, ""))
			cur.Reset()
		}
	}
	for _, c := range text {
		switch {
		case c == '\'':
			flush()
			inStr = !inStr
		case inStr:
		case c == '_' || c == '.' || c == '"' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9':
			cur.WriteRune(c)
		default:
			flush()
		}
	}
	flush()
	return out
}
