package sqlops

import (
	"strings"
)

// TableRef is a table named by a statement.
type TableRef struct {
	Name  string `json:"name"`
	Alias string `json:"alias,omitempty"`
	Role  string `json:"role"`            // from | join | insert | update | delete | using | merge
	Depth int    `json:"depth,omitempty"` // 0: main statement; >0: inside a subquery or CTE
}

// Join is a JOIN of a statement and its condition.
type Join struct {
	Kind  string `json:"kind"` // JOIN, LEFT JOIN, ...
	Table string `json:"table"`
	Alias string `json:"alias,omitempty"`
	On    string `json:"on,omitempty"` // ON condition, or USING (...)
	Depth int    `json:"depth,omitempty"`
}

// Structure is the shape of one SQL statement read from its text: the
// tables it names and the clauses of the main statement. It is a reading
// aid, not a full SQL parser: what is not recognised is left out, never
// guessed, and parts computed at runtime (⟨expr⟩) are not described.
type Structure struct {
	Statement string     `json:"statement"`
	Tables    []TableRef `json:"tables,omitempty"`
	Joins     []Join     `json:"joins,omitempty"`
	CTEs      []string   `json:"ctes,omitempty"`
	Columns   []string   `json:"columns,omitempty"` // SELECT list, INSERT columns or UPDATE SET targets
	Filters   []string   `json:"filters,omitempty"` // WHERE, split at top-level AND
	GroupBy   []string   `json:"groupBy,omitempty"`
	Having    string     `json:"having,omitempty"`
	OrderBy   []string   `json:"orderBy,omitempty"`
	Limit     string     `json:"limit,omitempty"`
	Offset    string     `json:"offset,omitempty"`
	Returning []string   `json:"returning,omitempty"`
	Conflict  string     `json:"conflict,omitempty"` // ON CONFLICT ...
	Lock      string     `json:"lock,omitempty"`     // FOR UPDATE ...
	Partial   bool       `json:"partial,omitempty"`
	Notes     []string   `json:"notes,omitempty"`
}

// StructureOf describes the first statement of sql.
func StructureOf(sql string) *Structure {
	s := &Structure{}
	s.Statement, _ = Classify(sql)
	p := &shape{all: lex(sql), s: s}
	depth := 0
	for i, t := range p.all {
		switch t.kind {
		case tSpace, tComment:
			continue
		case tHole:
			s.Partial = true
		case tPunct:
			switch t.text {
			case "(":
				depth++
			case ")":
				depth--
			case ";":
				if depth <= 0 {
					if p.more(i) {
						s.Notes = append(s.Notes, "several statements in one string: only the first is described")
					}
					goto done
				}
			}
		}
		p.sig = append(p.sig, i)
	}
done:
	if len(p.sig) == 0 {
		return s
	}
	p.scope()
	p.ctes()
	p.tables()
	p.clauses()
	if s.Partial {
		s.Notes = append(s.Notes, "parts computed at runtime (⟨…⟩) are not described")
	}
	return s
}

type shape struct {
	all   []sqlToken
	sig   []int  // significant tokens (indexes into all) of the first statement
	depth []int  // parenthesis depth of each significant token
	inSub []bool // at depth 0 or only inside subquery parentheses
	main  int    // first token of the main statement (after WITH ...)
	s     *Structure
}

func (p *shape) more(i int) bool {
	for _, t := range p.all[i+1:] {
		if t.kind != tSpace && t.kind != tComment {
			return true
		}
	}
	return false
}

func (p *shape) n() int { return len(p.sig) }

func (p *shape) tok(k int) sqlToken {
	if k < 0 || k >= len(p.sig) {
		return sqlToken{kind: tSpace}
	}
	return p.all[p.sig[k]]
}

// word returns the upper-case keyword at k ("" when not a word).
func (p *shape) word(k int) string {
	if t := p.tok(k); t.kind == tWord {
		return strings.ToUpper(t.text)
	}
	return ""
}

func (p *shape) punct(k int, s string) bool {
	t := p.tok(k)
	return t.kind == tPunct && t.text == s
}

// text renders significant tokens a..b (inclusive) as written, with
// whitespace collapsed and comments dropped.
func (p *shape) text(a, b int) string {
	if a > b || a < 0 || b >= len(p.sig) {
		return ""
	}
	var sb strings.Builder
	for _, t := range p.all[p.sig[a] : p.sig[b]+1] {
		if t.kind == tSpace || t.kind == tComment {
			sb.WriteByte(' ')
			continue
		}
		sb.WriteString(t.text)
	}
	return strings.Join(strings.Fields(sb.String()), " ")
}

// scope computes depths and which tokens are outside function-call or
// expression parentheses (where FROM and JOIN name tables).
func (p *shape) scope() {
	n := p.n()
	p.depth = make([]int, n)
	p.inSub = make([]bool, n)
	var stack []bool // per open parenthesis: holds a subquery
	sub := func() bool {
		for _, b := range stack {
			if !b {
				return false
			}
		}
		return true
	}
	for k := 0; k < n; k++ {
		p.depth[k] = len(stack)
		p.inSub[k] = sub()
		switch {
		case p.punct(k, "("):
			w := p.word(k + 1)
			stack = append(stack, w == "SELECT" || w == "WITH" || w == "VALUES" || w == "INSERT" || w == "UPDATE" || w == "DELETE")
		case p.punct(k, ")") && len(stack) > 0:
			stack = stack[:len(stack)-1]
		}
	}
}

// closing returns the index of the parenthesis closing the one at k.
func (p *shape) closing(k int) int {
	d := 0
	for j := k; j < p.n(); j++ {
		switch {
		case p.punct(j, "("):
			d++
		case p.punct(j, ")"):
			d--
			if d == 0 {
				return j
			}
		}
	}
	return p.n() - 1
}

func (p *shape) ctes() {
	if p.word(0) != "WITH" {
		return
	}
	k := 1
	if p.word(k) == "RECURSIVE" {
		k++
	}
	for k < p.n() {
		name, j, ok := p.name(k)
		if !ok {
			break
		}
		if p.punct(j, "(") {
			j = p.closing(j) + 1
		}
		if p.word(j) != "AS" {
			break
		}
		j++
		if p.word(j) == "NOT" {
			j++
		}
		if p.word(j) == "MATERIALIZED" {
			j++
		}
		if !p.punct(j, "(") {
			break
		}
		j = p.closing(j) + 1
		p.s.CTEs = append(p.s.CTEs, name)
		k = j
		if !p.punct(k, ",") {
			break
		}
		k++
	}
	p.main = k
}

// name reads a possibly qualified identifier at k.
func (p *shape) name(k int) (string, int, bool) {
	t := p.tok(k)
	if t.kind != tWord && t.kind != tQuoted {
		return "", k, false
	}
	if t.kind == tWord && reserved[strings.ToUpper(t.text)] {
		return "", k, false
	}
	name := t.text
	k++
	for p.punct(k, ".") {
		if n := p.tok(k + 1); n.kind == tWord || n.kind == tQuoted {
			name += "." + n.text
			k += 2
			continue
		}
		break
	}
	return name, k, true
}

// alias reads an optional [AS] alias at k.
func (p *shape) alias(k int) (string, int) {
	if p.word(k) == "AS" {
		if a, j, ok := p.name(k + 1); ok {
			return a, j
		}
		return "", k + 1
	}
	t := p.tok(k)
	if (t.kind == tWord && !reserved[strings.ToUpper(t.text)]) || t.kind == tQuoted {
		return t.text, k + 1
	}
	return "", k
}

// Words that end a table reference or start a clause.
var reserved = map[string]bool{
	"WHERE": true, "JOIN": true, "LEFT": true, "RIGHT": true, "FULL": true, "INNER": true, "CROSS": true, "NATURAL": true,
	"OUTER": true, "ON": true, "USING": true, "GROUP": true, "ORDER": true, "HAVING": true, "LIMIT": true, "OFFSET": true,
	"SET": true, "VALUES": true, "RETURNING": true, "UNION": true, "INTERSECT": true, "EXCEPT": true, "WINDOW": true,
	"FOR": true, "SELECT": true, "DEFAULT": true, "OVERRIDING": true, "DO": true, "FETCH": true, "TABLESAMPLE": true,
	"WHEN": true, "THEN": true, "LATERAL": true, "AS": true, "INTO": true, "FROM": true, "WITH": true, "ONLY": true,
	"AND": true, "OR": true, "NOT": true,
}

var joinWords = map[string]bool{"LEFT": true, "RIGHT": true, "FULL": true, "INNER": true, "CROSS": true, "NATURAL": true, "OUTER": true}

func (p *shape) isCTE(name string) bool {
	for _, c := range p.s.CTEs {
		if strings.EqualFold(c, name) {
			return true
		}
	}
	return false
}

func (p *shape) tables() {
	seen := map[TableRef]bool{}
	for k := 0; k < p.n(); k++ {
		if !p.inSub[k] {
			continue
		}
		prev := p.word(k - 1)
		role := ""
		switch p.word(k) {
		case "FROM":
			switch prev {
			case "DELETE":
				role = "delete"
			case "DISTINCT": // IS [NOT] DISTINCT FROM
				continue
			default:
				role = "from"
			}
		case "JOIN":
			role = "join"
		case "INTO":
			switch prev {
			case "INSERT":
				role = "insert"
			case "MERGE":
				role = "merge"
			default:
				continue
			}
		case "UPDATE":
			// Not FOR UPDATE, DO UPDATE, NO KEY UPDATE.
			if prev != "" {
				continue
			}
			role = "update"
		case "USING":
			if p.punct(k+1, "(") { // JOIN ... USING (columns)
				continue
			}
			role = "using"
		default:
			continue
		}
		d := p.depth[k]
		for j := k + 1; j < p.n(); {
			for p.word(j) == "ONLY" || p.word(j) == "LATERAL" {
				j++
			}
			var tr TableRef
			if p.punct(j, "(") { // derived table
				j = p.closing(j) + 1
				tr.Name = "(subquery)"
			} else {
				name, nj, ok := p.name(j)
				if !ok {
					break
				}
				j = nj
				if p.punct(j, "(") && role != "insert" && role != "merge" { // table function
					j = p.closing(j) + 1
					tr.Name = name + "(…)"
				} else {
					tr.Name = name
				}
			}
			if role == "insert" {
				if p.word(j) == "AS" {
					tr.Alias, j = p.alias(j)
				}
			} else {
				tr.Alias, j = p.alias(j)
			}
			tr.Role, tr.Depth = role, d
			table := !strings.HasSuffix(tr.Name, ")") && !strings.HasSuffix(tr.Name, "…)") && !p.isCTE(tr.Name)
			if table && !seen[tr] {
				seen[tr] = true
				p.s.Tables = append(p.s.Tables, tr)
			}
			if role == "join" {
				p.join(k, j, tr)
			}
			// FROM a, b
			if (role == "from" || role == "using") && p.punct(j, ",") && p.depth[j] == d {
				j++
				continue
			}
			break
		}
	}
}

func (p *shape) join(k, j int, tr TableRef) {
	var words []string
	for i := k - 1; i >= 0 && joinWords[p.word(i)]; i-- {
		words = append([]string{p.word(i)}, words...)
	}
	jn := Join{Kind: strings.Join(append(words, "JOIN"), " "), Table: tr.Name, Alias: tr.Alias, Depth: p.depth[k]}
	switch p.word(j) {
	case "ON":
		end := j + 1
		for ; end < p.n(); end++ {
			if p.depth[end] < p.depth[k] || (p.depth[end] == p.depth[k] && (clauseWord[p.word(end)] || joinWords[p.word(end)] || p.word(end) == "JOIN" || p.punct(end, ",") || p.punct(end, ")"))) {
				break
			}
		}
		jn.On = p.text(j+1, end-1)
	case "USING":
		if p.punct(j+1, "(") {
			jn.On = "USING " + p.text(j+1, p.closing(j+1))
		}
	}
	p.s.Joins = append(p.s.Joins, jn)
}

var clauseWord = map[string]bool{
	"WHERE": true, "GROUP": true, "ORDER": true, "HAVING": true, "LIMIT": true, "OFFSET": true, "RETURNING": true,
	"UNION": true, "INTERSECT": true, "EXCEPT": true, "WINDOW": true, "FOR": true, "FETCH": true, "SET": true,
	"VALUES": true, "FROM": true, "SELECT": true,
}

// clauses splits the main statement at its top-level clause keywords.
func (p *shape) clauses() {
	type clause struct {
		name       string
		start, end int // content tokens
	}
	var cs []clause
	base := 0
	if p.main < p.n() {
		base = p.depth[p.main]
	}
	// open starts a clause at keyword kw whose content starts at from.
	open := func(name string, kw, from int) {
		if len(cs) > 0 {
			cs[len(cs)-1].end = kw - 1
		}
		cs = append(cs, clause{name: name, start: from})
	}
	stmt := ""
	for k := p.main; k < p.n(); k++ {
		if p.depth[k] != base || p.tok(k).kind != tWord {
			continue
		}
		w := p.word(k)
		if stmt == "" {
			stmt = w
		}
		if len(cs) > 0 && cs[len(cs)-1].name == "conflict" && w != "RETURNING" {
			continue
		}
		switch w {
		case "SELECT", "WHERE", "HAVING", "LIMIT", "OFFSET", "RETURNING", "VALUES", "WINDOW":
			if w == "SELECT" && len(cs) > 0 && cs[len(cs)-1].name == "insert" {
				open("query", k, k+1)
				continue
			}
			open(strings.ToLower(w), k, k+1)
		case "FROM":
			if stmt == "DELETE" && len(cs) == 0 {
				open("delete", k, k+1)
			} else {
				open("from", k, k+1)
			}
		case "INTO":
			if stmt == "INSERT" {
				open("insert", k, k+1)
			}
		case "UPDATE":
			if p.word(k-1) == "" {
				open("update", k, k+1)
			}
		case "SET":
			if stmt == "UPDATE" {
				open("set", k, k+1)
			}
		case "GROUP", "ORDER":
			if p.word(k+1) == "BY" {
				open(strings.ToLower(w), k, k+2)
			}
		case "FETCH":
			open("fetch", k, k+1)
		case "FOR":
			switch p.word(k + 1) {
			case "UPDATE", "SHARE", "NO", "KEY":
				open("lock", k, k)
			}
		case "ON":
			if p.word(k+1) == "CONFLICT" {
				open("conflict", k, k)
			}
		case "UNION", "INTERSECT", "EXCEPT":
			p.s.Notes = append(p.s.Notes, "compound query ("+w+"): clauses of the first part only; tables of every part are listed")
			if len(cs) > 0 {
				cs[len(cs)-1].end = k - 1
			}
			goto split
		}
	}
	if len(cs) > 0 {
		cs[len(cs)-1].end = p.n() - 1
	}
split:
	for _, c := range cs {
		switch c.name {
		case "select":
			p.s.Columns = p.list(c.start, c.end, base)
		case "where":
			p.s.Filters = p.conditions(c.start, c.end, base)
		case "group":
			p.s.GroupBy = p.list(c.start, c.end, base)
		case "order":
			p.s.OrderBy = p.list(c.start, c.end, base)
		case "having":
			p.s.Having = p.text(c.start, c.end)
		case "limit", "fetch":
			p.s.Limit = p.text(c.start, c.end)
			if c.name == "fetch" {
				p.s.Limit = "FETCH " + p.s.Limit
			}
		case "offset":
			p.s.Offset = p.text(c.start, c.end)
		case "returning":
			p.s.Returning = p.list(c.start, c.end, base)
		case "conflict":
			p.s.Conflict = p.text(c.start, c.end)
		case "lock":
			p.s.Lock = p.text(c.start, c.end)
		case "set":
			for _, item := range p.ranges(c.start, c.end, base) {
				for j := item[0]; j <= item[1]; j++ {
					if p.punct(j, "=") && p.depth[j] == base {
						p.s.Columns = append(p.s.Columns, p.text(item[0], j-1))
						break
					}
				}
			}
		case "insert":
			// INSERT INTO t [AS a] (columns)
			for j := c.start; j <= c.end; j++ {
				if p.punct(j, "(") && p.depth[j] == base {
					p.s.Columns = p.list(j+1, p.closing(j)-1, base+1)
					break
				}
			}
		}
	}
}

// ranges splits a..b at commas of the given depth.
func (p *shape) ranges(a, b, depth int) [][2]int {
	var out [][2]int
	start := a
	for j := a; j <= b; j++ {
		if p.punct(j, ",") && p.depth[j] == depth {
			if start <= j-1 {
				out = append(out, [2]int{start, j - 1})
			}
			start = j + 1
		}
	}
	if start <= b {
		out = append(out, [2]int{start, b})
	}
	return out
}

func (p *shape) list(a, b, depth int) []string {
	var out []string
	for _, r := range p.ranges(a, b, depth) {
		out = append(out, p.text(r[0], r[1]))
	}
	return out
}

// conditions splits a WHERE clause at its top-level ANDs. With a top-level
// OR (which binds looser than AND) the clause is kept whole.
func (p *shape) conditions(a, b, depth int) []string {
	for j := a; j <= b; j++ {
		if p.depth[j] == depth && p.word(j) == "OR" {
			return []string{p.text(a, b)}
		}
	}
	var out []string
	start, between := a, false
	for j := a; j <= b; j++ {
		if p.depth[j] != depth {
			continue
		}
		switch p.word(j) {
		case "BETWEEN":
			between = true
		case "AND":
			if between {
				between = false
				continue
			}
			if t := p.text(start, j-1); t != "" {
				out = append(out, t)
			}
			start = j + 1
		}
	}
	if t := p.text(start, b); t != "" {
		out = append(out, t)
	}
	return out
}
