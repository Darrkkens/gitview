// Package pgexplain asks a PostgreSQL server for the execution plan of one
// statement. It is deliberately separate from the static SQL Explorer:
// nothing here runs unless a connection is supplied explicitly, and every
// request is checked before a connection is opened.
//
// Safety rules:
//   - the DSN comes only from the caller (never from the analysed project);
//   - only loopback hosts and Unix sockets are accepted unless AllowRemote;
//   - only one SELECT/INSERT/UPDATE/DELETE/MERGE statement per request;
//   - plain EXPLAIN runs in a READ ONLY transaction that is always rolled back;
//   - EXPLAIN ANALYZE executes the statement: it must be requested
//     explicitly, and data-modifying statements additionally need AllowWrite
//     (they run in a transaction that is always rolled back);
//   - statement_timeout and lock_timeout bound every request;
//   - credentials and parameter values are never stored in the result.
package pgexplain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/athos/gitview/internal/evidence"
	"github.com/athos/gitview/internal/inspect/sqlops"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// EnvDSN is the only environment variable GitView reads a connection from.
const EnvDSN = "GITVIEW_PG_DSN"

type Config struct {
	DSN         string
	AllowRemote bool          // accept hosts other than loopback and Unix sockets
	Timeout     time.Duration // whole request, including connection (default 15s)
}

type Request struct {
	SQL        string
	Analyze    bool     // EXPLAIN ANALYZE: executes the statement
	AllowWrite bool     // ANALYZE of a data-modifying statement (rolled back)
	Params     []string // text values for $1..$N; never recorded
	Buffers    bool     // with ANALYZE: buffer usage
}

type PlanNode struct {
	NodeType            string      `json:"nodeType"`
	Relation            string      `json:"relation,omitempty"`
	Alias               string      `json:"alias,omitempty"`
	Index               string      `json:"index,omitempty"`
	JoinType            string      `json:"joinType,omitempty"`
	Strategy            string      `json:"strategy,omitempty"`
	ParentRelationship  string      `json:"parentRelationship,omitempty"`
	StartupCost         float64     `json:"startupCost"`
	TotalCost           float64     `json:"totalCost"`
	PlanRows            float64     `json:"planRows"`
	PlanWidth           float64     `json:"planWidth"`
	ActualStartupMs     *float64    `json:"actualStartupMs,omitempty"`
	ActualTotalMs       *float64    `json:"actualTotalMs,omitempty"`
	ActualRows          *float64    `json:"actualRows,omitempty"`
	ActualLoops         *float64    `json:"actualLoops,omitempty"`
	RowsRemovedByFilter *float64    `json:"rowsRemovedByFilter,omitempty"`
	Conditions          []Condition `json:"conditions,omitempty"`
	Keys                []Condition `json:"keys,omitempty"` // sort and group keys
	Buffers             string      `json:"buffers,omitempty"`
	Plans               []*PlanNode `json:"plans,omitempty"`
}

type Condition struct {
	Name  string `json:"name"`  // Filter, Index Cond, Hash Cond...
	Value string `json:"value"` // as printed by PostgreSQL
}

type Plan struct {
	Target      string              `json:"target"` // host:port/database, without credentials
	Server      string              `json:"server"`
	Statement   string              `json:"statement"`
	SQL         string              `json:"sql"`
	Mode        string              `json:"mode"`
	Analyzed    bool                `json:"analyzed"`
	Generic     bool                `json:"generic"`
	ParamsGiven int                 `json:"paramsGiven,omitempty"` // count only: values are not recorded
	Root        *PlanNode           `json:"root"`
	PlanningMs  *float64            `json:"planningMs,omitempty"`
	ExecutionMs *float64            `json:"executionMs,omitempty"`
	Scans       []string            `json:"scans,omitempty"`
	Joins       []string            `json:"joins,omitempty"`
	Indexes     []string            `json:"indexes,omitempty"`
	Warnings    []string            `json:"warnings,omitempty"`
	At          time.Time           `json:"at"`
	Evidence    []evidence.Evidence `json:"evidence"`
}

// Target parses a DSN without connecting and reports where it points.
// The returned string never contains credentials.
func Target(dsn string) (target string, local bool, err error) {
	if strings.TrimSpace(dsn) == "" {
		return "", false, errors.New("no PostgreSQL connection configured (use --dsn or " + EnvDSN + ")")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return "", false, errors.New("invalid PostgreSQL connection string (details withheld: it may contain credentials)")
	}
	local = isLocal(cfg.Host)
	for _, fb := range cfg.Fallbacks {
		local = local && isLocal(fb.Host)
	}
	host := cfg.Host
	if strings.HasPrefix(host, "/") {
		host = "unix:" + host
	}
	return fmt.Sprintf("%s:%d/%s", host, cfg.Port, cfg.Database), local, nil
}

func isLocal(host string) bool {
	if strings.HasPrefix(host, "/") || host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Check validates a request without connecting: statement kind, placeholders
// and the authorisation needed. It returns the statement kind.
func Check(req Request) (string, error) {
	sql := strings.TrimSpace(req.SQL)
	if sql == "" {
		return "", errors.New("no SQL to explain")
	}
	if strings.ContainsAny(sql, "⟨⟩") {
		return "", errors.New("the SQL is only partially known (built at runtime); provide the complete statement to explain it")
	}
	for _, r := range sql {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' || r == 0x7f {
			return "", fmt.Errorf("the SQL contains a control character (%U); refusing to send it", r)
		}
	}
	kind, multi := sqlops.Classify(sql)
	if multi {
		return kind, errors.New("the text holds more than one statement; EXPLAIN accepts exactly one")
	}
	switch kind {
	case sqlops.StmtSelect, sqlops.StmtInsert, sqlops.StmtUpdate, sqlops.StmtDelete, sqlops.StmtMerge, sqlops.StmtWithModify:
	default:
		return kind, fmt.Errorf("refusing to explain a %s statement: only SELECT, INSERT, UPDATE, DELETE and MERGE are explained", kind)
	}
	dollar, _ := sqlops.Placeholders(sql)
	if len(req.Params) > 0 && len(req.Params) != dollar {
		return kind, fmt.Errorf("the statement has %d placeholder(s) but %d value(s) were given", dollar, len(req.Params))
	}
	if req.Analyze {
		if dollar > 0 && len(req.Params) == 0 {
			return kind, fmt.Errorf("EXPLAIN ANALYZE executes the statement and needs values for $1..$%d", dollar)
		}
		if !sqlops.ReadOnly(kind) && !req.AllowWrite {
			return kind, fmt.Errorf("EXPLAIN ANALYZE would execute this %s statement; it needs explicit permission to run a write (it is rolled back afterwards)", kind)
		}
	}
	return kind, nil
}

// Explain runs EXPLAIN (FORMAT JSON) for one statement.
func Explain(ctx context.Context, cfg Config, req Request) (*Plan, error) {
	kind, err := Check(req)
	if err != nil {
		return nil, err
	}
	target, local, err := Target(cfg.DSN)
	if err != nil {
		return nil, err
	}
	if !local && !cfg.AllowRemote {
		return nil, fmt.Errorf("%s is not a local server; GitView only connects to loopback hosts or Unix sockets unless remote access is allowed explicitly", target)
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	pcfg, err := pgx.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, errors.New("invalid PostgreSQL connection string")
	}
	pcfg.ConnectTimeout = min(cfg.Timeout, 10*time.Second)
	pcfg.RuntimeParams["application_name"] = "gitview-explain"
	conn, err := pgx.ConnectConfig(ctx, pcfg)
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", target, sanitize(err))
	}
	defer conn.Close(context.Background())
	pg := conn.PgConn()

	plan := &Plan{Target: target, Server: pg.ParameterStatus("server_version"), Statement: kind,
		SQL: strings.TrimSpace(req.SQL), Analyzed: req.Analyze, ParamsGiven: len(req.Params), At: time.Now()}
	major, _ := strconv.Atoi(strings.SplitN(strings.TrimSpace(plan.Server), ".", 2)[0])
	dollar, _ := sqlops.Placeholders(req.SQL)
	if dollar > 0 && len(req.Params) == 0 {
		if major > 0 && major < 16 {
			return nil, fmt.Errorf("the statement has placeholders; PostgreSQL %s cannot build a generic plan (needs 16+): give values for $1..$%d", plan.Server, dollar)
		}
		plan.Generic = true
	}

	access := "READ ONLY"
	if req.Analyze && !sqlops.ReadOnly(kind) {
		access = "READ WRITE"
	}
	if err := simple(ctx, pg, "BEGIN TRANSACTION "+access); err != nil {
		return nil, sanitize(err)
	}
	defer func() {
		rctx, rcancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer rcancel()
		_ = simple(rctx, pg, "ROLLBACK")
	}()
	ms := cfg.Timeout.Milliseconds()
	for _, s := range []string{
		fmt.Sprintf("SET LOCAL statement_timeout = %d", ms),
		"SET LOCAL lock_timeout = 2000",
		fmt.Sprintf("SET LOCAL idle_in_transaction_session_timeout = %d", ms+5000),
	} {
		if err := simple(ctx, pg, s); err != nil {
			return nil, sanitize(err)
		}
	}

	opts := []string{"FORMAT JSON"}
	switch {
	case plan.Generic:
		opts = append(opts, "GENERIC_PLAN")
		plan.Mode = "EXPLAIN (GENERIC_PLAN): estimated plan, independent of parameter values; the statement was not executed"
	case req.Analyze:
		opts = append(opts, "ANALYZE")
		if req.Buffers {
			opts = append(opts, "BUFFERS")
		}
		plan.Mode = "EXPLAIN ANALYZE: the statement was executed inside a transaction that was rolled back"
	default:
		plan.Mode = "EXPLAIN: estimated plan; the statement was not executed"
	}
	query := "EXPLAIN (" + strings.Join(opts, ", ") + ") " + strings.TrimSpace(req.SQL)

	var raw []byte
	if len(req.Params) > 0 {
		values := make([][]byte, len(req.Params))
		oids := make([]uint32, len(req.Params)) // 0: let the server infer the types
		for i, v := range req.Params {
			values[i] = []byte(v)
		}
		res := pg.ExecParams(ctx, query, values, oids, nil, nil).Read()
		if res.Err != nil {
			return nil, explainErr(ctx, res.Err, req.Params)
		}
		if len(res.Rows) == 0 || len(res.Rows[0]) == 0 {
			return nil, errors.New("EXPLAIN returned no plan")
		}
		raw = res.Rows[0][0]
	} else {
		// A generic plan needs the text without bound values, which only the
		// simple protocol allows. The simple protocol runs every statement
		// of the text, so the server first parses it with the extended
		// protocol (Parse/Describe: nothing is executed), which rejects
		// more than one statement with the same grammar.
		if _, err := pg.Prepare(ctx, "", query, nil); err != nil {
			var pe *pgconn.PgError
			if errors.As(err, &pe) && pe.Code == "42601" && strings.Contains(pe.Message, "multiple commands") {
				return nil, errors.New("PostgreSQL parsed more than one statement in the text; refusing to run it")
			}
			return nil, explainErr(ctx, err, req.Params)
		}
		results, err := pg.Exec(ctx, query).ReadAll()
		if err != nil {
			return nil, explainErr(ctx, err, req.Params)
		}
		if len(results) == 0 || len(results[0].Rows) == 0 || len(results[0].Rows[0]) == 0 {
			return nil, errors.New("EXPLAIN returned no plan")
		}
		raw = results[0].Rows[0][0]
	}
	if err := plan.parse(raw); err != nil {
		return nil, err
	}
	redact(plan.Root, req.Params)
	plan.Warnings = append(plan.Warnings, "Costs and row counts are planner estimates based on the statistics of "+target+".")
	if req.Analyze {
		plan.Warnings = append(plan.Warnings, "Actual times and rows come from one execution on "+target+"; they depend on its data, caches and load.")
		plan.Warnings = append(plan.Warnings, "The statement was executed: functions it calls may have effects a transaction does not undo (other connections, signals, external calls).")
		if !sqlops.ReadOnly(kind) {
			plan.Warnings = append(plan.Warnings, "The write was rolled back, but effects outside transactional control (sequence increments, external side effects of triggers or functions) are not undone.")
		}
	}
	src := "EXPLAIN (" + strings.Join(opts, ", ") + ")"
	basis := evidence.BasisEstimated
	if req.Analyze {
		basis = evidence.BasisObserved
	}
	plan.Evidence = append(plan.Evidence, evidence.Evidence{Kind: evidence.Database, Basis: basis, Source: src + " on " + target + " (PostgreSQL " + plan.Server + ")"})
	return plan, nil
}

func simple(ctx context.Context, pg *pgconn.PgConn, sql string) error {
	_, err := pg.Exec(ctx, sql).ReadAll()
	return err
}

func explainErr(ctx context.Context, err error, params []string) error {
	if ctx.Err() != nil {
		return fmt.Errorf("EXPLAIN cancelled or timed out: %w", ctx.Err())
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		switch pe.Code {
		case "57014":
			return errors.New("EXPLAIN stopped by statement_timeout")
		case "25006":
			return errors.New("PostgreSQL refused the statement in a read-only transaction: " + redactText(pe.Message, params))
		}
		return fmt.Errorf("PostgreSQL: %s (%s)", redactText(pe.Message, params), pe.Code)
	}
	return errors.New(redactText(sanitize(err).Error(), params))
}

// sanitize keeps driver errors from echoing connection details.
func sanitize(err error) error {
	if err == nil {
		return nil
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return fmt.Errorf("PostgreSQL: %s (%s)", pe.Message, pe.Code)
	}
	msg := err.Error()
	if i := strings.Index(msg, "password"); i >= 0 {
		msg = msg[:i] + "password …"
	}
	return errors.New(msg)
}

// parse reads the JSON EXPLAIN output.
func (p *Plan) parse(raw []byte) error {
	var doc []map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil || len(doc) == 0 {
		return fmt.Errorf("unexpected EXPLAIN output: %v", err)
	}
	top := doc[0]
	root, ok := top["Plan"].(map[string]any)
	if !ok {
		return errors.New("EXPLAIN output has no plan")
	}
	p.Root = parseNode(root)
	p.PlanningMs = num(top, "Planning Time")
	p.ExecutionMs = num(top, "Execution Time")
	seen := map[string]bool{}
	var walk func(n *PlanNode)
	walk = func(n *PlanNode) {
		switch {
		case strings.Contains(n.NodeType, "Scan"):
			s := n.NodeType
			if n.Relation != "" {
				s += " on " + n.Relation
			}
			if n.Index != "" {
				s += " using " + n.Index
			}
			if !seen["s"+s] {
				seen["s"+s] = true
				p.Scans = append(p.Scans, s)
			}
		case strings.Contains(n.NodeType, "Join") || n.NodeType == "Nested Loop":
			j := n.NodeType
			if n.JoinType != "" {
				j += " (" + n.JoinType + ")"
			}
			p.Joins = append(p.Joins, j)
		}
		if n.Index != "" && !seen["i"+n.Index] {
			seen["i"+n.Index] = true
			p.Indexes = append(p.Indexes, n.Index)
		}
		for _, c := range n.Plans {
			walk(c)
		}
	}
	walk(p.Root)
	return nil
}

// redact replaces the parameter values PostgreSQL prints in custom plans
// ('42'::bigint, or a bare 42) by their placeholders, so supplied values
// never reach the result.
func redact(n *PlanNode, params []string) {
	if n == nil || len(params) == 0 {
		return
	}
	for i := range n.Conditions {
		n.Conditions[i].Value = redactText(n.Conditions[i].Value, params)
	}
	for i := range n.Keys {
		n.Keys[i].Value = redactText(n.Keys[i].Value, params)
	}
	for _, c := range n.Plans {
		redact(c, params)
	}
}

// redactText replaces every form of a supplied value in s by $N: quoted
// ('v', "v") and, for numbers and booleans, the bare token.
func redactText(s string, params []string) string {
	idx := make([]int, len(params))
	for i := range idx {
		idx[i] = i
	}
	// Longest values first, so "4" does not clobber part of "42".
	sort.Slice(idx, func(a, b int) bool { return len(params[idx[a]]) > len(params[idx[b]]) })
	for _, i := range idx {
		v := params[i]
		if v == "" {
			continue
		}
		ph := "$" + strconv.Itoa(i+1)
		s = strings.ReplaceAll(s, "'"+strings.ReplaceAll(v, "'", "''")+"'", ph)
		s = strings.ReplaceAll(s, `"`+v+`"`, ph)
		_, numErr := strconv.ParseFloat(v, 64)
		if numErr == nil || strings.EqualFold(v, "true") || strings.EqualFold(v, "false") {
			re := regexp.MustCompile(`(^|[^\w.$])` + regexp.QuoteMeta(v) + `([^\w.]|$)`)
			for re.MatchString(s) {
				s = re.ReplaceAllString(s, "${1}"+strings.ReplaceAll(ph, "$", "$$")+"${2}")
			}
		}
	}
	return s
}

// ParsePlan parses the output of EXPLAIN (FORMAT JSON).
func ParsePlan(raw []byte) (*Plan, error) {
	p := &Plan{}
	if err := p.parse(raw); err != nil {
		return nil, err
	}
	return p, nil
}

var conditionKeys = []string{"Index Cond", "Recheck Cond", "Filter", "Join Filter", "Hash Cond", "Merge Cond", "One-Time Filter", "TID Cond"}

func parseNode(m map[string]any) *PlanNode {
	n := &PlanNode{NodeType: str(m, "Node Type"), Relation: str(m, "Relation Name"), Alias: str(m, "Alias"),
		Index: str(m, "Index Name"), JoinType: str(m, "Join Type"), Strategy: str(m, "Strategy"),
		ParentRelationship: str(m, "Parent Relationship")}
	if s := str(m, "Schema"); s != "" && n.Relation != "" && s != "public" {
		n.Relation = s + "." + n.Relation
	}
	if v := num(m, "Startup Cost"); v != nil {
		n.StartupCost = *v
	}
	if v := num(m, "Total Cost"); v != nil {
		n.TotalCost = *v
	}
	if v := num(m, "Plan Rows"); v != nil {
		n.PlanRows = *v
	}
	if v := num(m, "Plan Width"); v != nil {
		n.PlanWidth = *v
	}
	n.ActualStartupMs = num(m, "Actual Startup Time")
	n.ActualTotalMs = num(m, "Actual Total Time")
	n.ActualRows = num(m, "Actual Rows")
	n.ActualLoops = num(m, "Actual Loops")
	n.RowsRemovedByFilter = num(m, "Rows Removed by Filter")
	for _, k := range conditionKeys {
		if v := str(m, k); v != "" {
			n.Conditions = append(n.Conditions, Condition{k, v})
		}
	}
	for _, k := range []string{"Sort Key", "Group Key", "Hash Key"} {
		if arr, ok := m[k].([]any); ok {
			var parts []string
			for _, a := range arr {
				parts = append(parts, fmt.Sprint(a))
			}
			n.Keys = append(n.Keys, Condition{k, strings.Join(parts, ", ")})
		}
	}
	var buf []string
	for _, k := range []string{"Shared Hit Blocks", "Shared Read Blocks", "Shared Dirtied Blocks", "Temp Read Blocks", "Temp Written Blocks"} {
		if v := num(m, k); v != nil && *v > 0 {
			buf = append(buf, fmt.Sprintf("%s=%g", strings.ToLower(strings.TrimSuffix(k, " Blocks")), *v))
		}
	}
	sort.Strings(buf)
	n.Buffers = strings.Join(buf, " ")
	if kids, ok := m["Plans"].([]any); ok {
		for _, k := range kids {
			if km, ok := k.(map[string]any); ok {
				n.Plans = append(n.Plans, parseNode(km))
			}
		}
	}
	return n
}

func str(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}

func num(m map[string]any, k string) *float64 {
	if v, ok := m[k].(float64); ok {
		return &v
	}
	return nil
}
