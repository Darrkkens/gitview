package sqlops

import "strings"

// Statement is one statement of a SQL file.
type Statement struct {
	Text string
	Line int // 1-based line where it starts
}

// Statements splits SQL text at top-level semicolons, honouring strings,
// dollar quotes and comments.
func Statements(sql string) []Statement {
	var out []Statement
	var b strings.Builder
	line, start, depth := 1, 0, 0
	flush := func() {
		if t := strings.TrimSpace(b.String()); t != "" {
			out = append(out, Statement{Text: t, Line: start})
		}
		b.Reset()
		start = 0
	}
	for _, t := range lex(sql) {
		switch t.kind {
		case tSpace, tComment:
		case tPunct:
			switch t.text {
			case "(":
				depth++
			case ")":
				depth--
			case ";":
				if depth <= 0 {
					flush()
					depth = 0
					continue
				}
			}
			fallthrough
		default:
			if start == 0 {
				start = line
			}
		}
		if start != 0 {
			b.WriteString(t.text)
		}
		line += strings.Count(t.text, "\n")
	}
	flush()
	return out
}

// IndexDef is an index declared in SQL: CREATE INDEX, or the implicit index
// of a PRIMARY KEY or UNIQUE constraint.
type IndexDef struct {
	Name     string   `json:"name,omitempty"` // empty for unnamed constraints
	Table    string   `json:"table"`
	Columns  []string `json:"columns"` // columns or expressions, in index order
	Unique   bool     `json:"unique,omitempty"`
	Primary  bool     `json:"primary,omitempty"`
	Method   string   `json:"method,omitempty"` // USING btree, gin...
	Include  []string `json:"include,omitempty"`
	Where    string   `json:"where,omitempty"` // partial index predicate
	Implicit bool     `json:"implicit,omitempty"`
}

// IndexChange is what one statement declares about indexes.
type IndexChange struct {
	Table   string // table created by the statement (CREATE TABLE)
	Defs    []IndexDef
	Dropped []string // DROP INDEX names
}

// Indexes reads the index definitions of one statement: CREATE INDEX,
// CREATE TABLE constraints, ALTER TABLE ... ADD [CONSTRAINT] PRIMARY
// KEY/UNIQUE, and DROP INDEX.
func Indexes(stmt string) IndexChange {
	p := &shape{all: lex(stmt), s: &Structure{}}
	for i, t := range p.all {
		if t.kind != tSpace && t.kind != tComment {
			p.sig = append(p.sig, i)
		}
	}
	if len(p.sig) == 0 {
		return IndexChange{}
	}
	p.scope()
	var ch IndexChange
	k := 0
	switch p.word(0) {
	case "CREATE":
		k = 1
		if p.word(k) == "OR" && p.word(k+1) == "REPLACE" {
			k += 2
		}
		unique := false
		if p.word(k) == "UNIQUE" {
			unique = true
			k++
		}
		switch p.word(k) {
		case "INDEX":
			if d, ok := p.createIndex(k+1, unique); ok {
				ch.Defs = append(ch.Defs, d)
			}
		case "TABLE", "TEMP", "TEMPORARY", "UNLOGGED":
			for p.word(k) != "TABLE" && k < p.n() {
				k++
			}
			ch.Table = p.tableName(k + 1)
			ch.Defs = append(ch.Defs, p.createTable(k+1)...)
		}
	case "ALTER":
		if p.word(1) != "TABLE" {
			break
		}
		k = 2
		for p.word(k) == "IF" || p.word(k) == "EXISTS" || p.word(k) == "ONLY" {
			k++
		}
		table, j, ok := p.name(k)
		if !ok {
			break
		}
		for j < p.n() {
			if p.word(j) == "ADD" {
				if d, ok := p.constraint(j+1, table); ok {
					ch.Defs = append(ch.Defs, d)
				}
			}
			j++
		}
	case "DROP":
		if p.word(1) != "INDEX" {
			break
		}
		for k = 2; k < p.n(); k++ {
			switch p.word(k) {
			case "IF", "EXISTS", "CONCURRENTLY", "CASCADE", "RESTRICT":
				continue
			}
			if name, j, ok := p.name(k); ok {
				ch.Dropped = append(ch.Dropped, name)
				k = j - 1
			}
		}
	}
	return ch
}

// createIndex reads [CONCURRENTLY] [IF NOT EXISTS] [name] ON [ONLY] table
// [USING method] (columns) [INCLUDE (...)] [WHERE ...].
func (p *shape) createIndex(k int, unique bool) (IndexDef, bool) {
	d := IndexDef{Unique: unique}
	for {
		switch p.word(k) {
		case "CONCURRENTLY", "IF", "NOT", "EXISTS":
			k++
			continue
		}
		break
	}
	if p.word(k) != "ON" {
		name, j, ok := p.name(k)
		if !ok {
			return d, false
		}
		d.Name, k = name, j
	}
	if p.word(k) != "ON" {
		return d, false
	}
	k++
	if p.word(k) == "ONLY" {
		k++
	}
	table, k, ok := p.name(k)
	if !ok {
		return d, false
	}
	d.Table = table
	if p.word(k) == "USING" {
		d.Method = p.text(k+1, k+1)
		k += 2
	}
	if !p.punct(k, "(") {
		return d, false
	}
	end := p.closing(k)
	d.Columns = p.indexColumns(k+1, end-1)
	k = end + 1
	for k < p.n() {
		switch p.word(k) {
		case "INCLUDE":
			if p.punct(k+1, "(") {
				e := p.closing(k + 1)
				d.Include = p.list(k+2, e-1, p.depth[k+1]+1)
				k = e + 1
				continue
			}
		case "WHERE":
			d.Where = p.text(k+1, p.n()-1)
			return d, true
		}
		k++
	}
	return d, true
}

// indexColumns lists index elements without ASC/DESC/NULLS/opclass words
// for plain columns; expressions are kept as written.
func (p *shape) indexColumns(a, b int) []string {
	var out []string
	for _, r := range p.ranges(a, b, p.depth[a]) {
		if name, j, ok := p.name(r[0]); ok && !p.punct(j, "(") {
			out = append(out, name)
			continue
		}
		out = append(out, p.text(r[0], r[1]))
	}
	return out
}

// tableName reads [IF NOT EXISTS] name.
func (p *shape) tableName(k int) string {
	for p.word(k) == "IF" || p.word(k) == "NOT" || p.word(k) == "EXISTS" {
		k++
	}
	name, _, _ := p.name(k)
	return name
}

// createTable reads the PRIMARY KEY and UNIQUE constraints of CREATE TABLE.
func (p *shape) createTable(k int) []IndexDef {
	for p.word(k) == "IF" || p.word(k) == "NOT" || p.word(k) == "EXISTS" {
		k++
	}
	table, k, ok := p.name(k)
	if !ok || !p.punct(k, "(") {
		return nil
	}
	end := p.closing(k)
	var out []IndexDef
	for _, r := range p.ranges(k+1, end-1, p.depth[k]+1) {
		if d, ok := p.constraint(r[0], table); ok {
			out = append(out, d)
			continue
		}
		// Column definition: name type ... PRIMARY KEY | UNIQUE
		switch p.word(r[0]) {
		case "CONSTRAINT", "CHECK", "FOREIGN", "EXCLUDE", "LIKE":
			continue
		}
		col, j, ok := p.name(r[0])
		if !ok {
			continue
		}
		for ; j <= r[1]; j++ {
			switch {
			case p.word(j) == "PRIMARY" && p.word(j+1) == "KEY":
				out = append(out, IndexDef{Table: table, Columns: []string{col}, Unique: true, Primary: true, Implicit: true})
			case p.word(j) == "UNIQUE":
				out = append(out, IndexDef{Table: table, Columns: []string{col}, Unique: true, Implicit: true})
			}
		}
	}
	return out
}

// constraint reads [CONSTRAINT name] PRIMARY KEY (cols) | UNIQUE (cols).
func (p *shape) constraint(k int, table string) (IndexDef, bool) {
	d := IndexDef{Table: table, Implicit: true, Unique: true}
	if p.word(k) == "CONSTRAINT" {
		name, j, ok := p.name(k + 1)
		if !ok {
			return d, false
		}
		d.Name, k = name, j
	}
	switch {
	case p.word(k) == "PRIMARY" && p.word(k+1) == "KEY":
		d.Primary = true
		k += 2
	case p.word(k) == "UNIQUE":
		k++
		if p.word(k) == "NULLS" { // NULLS [NOT] DISTINCT
			for p.word(k) != "DISTINCT" && k < p.n() {
				k++
			}
			k++
		}
	default:
		return d, false
	}
	if !p.punct(k, "(") {
		return d, false
	}
	e := p.closing(k)
	d.Columns = p.indexColumns(k+1, e-1)
	return d, true
}
