package sqlops

import (
	"strconv"
	"strings"
	"unicode"
)

type tokKind int

const (
	tWord tokKind = iota
	tSpace
	tString  // '...', E'...', $tag$...$tag$
	tQuoted  // "identifier"
	tComment // -- ... or /* ... */
	tParam   // $1
	tPunct
	tHole // ⟨expr⟩: a part computed at runtime
)

type sqlToken struct {
	kind tokKind
	text string
}

// lex splits SQL into tokens without interpreting it. Unterminated literals
// run to the end of the text.
func lex(s string) []sqlToken {
	var out []sqlToken
	r := []rune(s)
	for i := 0; i < len(r); {
		c := r[i]
		start := i
		switch {
		case c == '⟨':
			for i < len(r) && r[i] != '⟩' {
				i++
			}
			i = min(i+1, len(r))
			out = append(out, sqlToken{tHole, string(r[start:i])})
		case unicode.IsSpace(c):
			for i < len(r) && unicode.IsSpace(r[i]) {
				i++
			}
			out = append(out, sqlToken{tSpace, string(r[start:i])})
		case c == '-' && i+1 < len(r) && r[i+1] == '-':
			// Like PostgreSQL, a line comment ends at \n or \r.
			for i < len(r) && r[i] != '\n' && r[i] != '\r' {
				i++
			}
			out = append(out, sqlToken{tComment, string(r[start:i])})
		case c == '/' && i+1 < len(r) && r[i+1] == '*':
			// Block comments nest in PostgreSQL.
			depth := 0
			for i < len(r) {
				if r[i] == '/' && i+1 < len(r) && r[i+1] == '*' {
					depth++
					i += 2
					continue
				}
				if r[i] == '*' && i+1 < len(r) && r[i+1] == '/' {
					depth--
					i += 2
					if depth == 0 {
						break
					}
					continue
				}
				i++
			}
			out = append(out, sqlToken{tComment, string(r[start:i])})
		case (c == 'E' || c == 'e') && i+1 < len(r) && r[i+1] == '\'' && (i == 0 || !identChar(r[i-1])):
			// Escape string: a backslash escapes the next character.
			i += 2
			for i < len(r) {
				if r[i] == '\\' {
					i += 2
					continue
				}
				if r[i] == '\'' {
					if i+1 < len(r) && r[i+1] == '\'' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			i = min(i, len(r))
			out = append(out, sqlToken{tString, string(r[start:i])})
		case c == '\'':
			i++
			for i < len(r) {
				if r[i] == '\'' {
					if i+1 < len(r) && r[i+1] == '\'' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			out = append(out, sqlToken{tString, string(r[start:i])})
		case c == '"':
			i++
			for i < len(r) && r[i] != '"' {
				i++
			}
			i = min(i+1, len(r))
			out = append(out, sqlToken{tQuoted, string(r[start:i])})
		case c == '$' && i+1 < len(r) && unicode.IsDigit(r[i+1]):
			i++
			for i < len(r) && unicode.IsDigit(r[i]) {
				i++
			}
			out = append(out, sqlToken{tParam, string(r[start:i])})
		case c == '$':
			// Dollar quoting: $tag$ ... $tag$
			j := i + 1
			for j < len(r) && (unicode.IsLetter(r[j]) || unicode.IsDigit(r[j]) || r[j] == '_') {
				j++
			}
			if j < len(r) && r[j] == '$' {
				tag := string(r[i : j+1])
				end := strings.Index(string(r[j+1:]), tag)
				if end >= 0 {
					i = j + 1 + len([]rune(string(r[j+1:])[:end])) + len([]rune(tag))
				} else {
					i = len(r)
				}
				out = append(out, sqlToken{tString, string(r[start:i])})
				continue
			}
			i++
			out = append(out, sqlToken{tPunct, "$"})
		case unicode.IsLetter(c) || c == '_':
			// Identifiers may contain $ after the first character (x$a$ is one word).
			for i < len(r) && (identChar(r[i]) || r[i] == '.') {
				i++
			}
			out = append(out, sqlToken{tWord, string(r[start:i])})
		default:
			i++
			out = append(out, sqlToken{tPunct, string(c)})
		}
	}
	return out
}

func identChar(c rune) bool {
	return unicode.IsLetter(c) || unicode.IsDigit(c) || c == '_' || c == '$'
}

func significant(toks []sqlToken) []sqlToken {
	var out []sqlToken
	for _, t := range toks {
		if t.kind != tSpace && t.kind != tComment && t.kind != tHole {
			out = append(out, t)
		}
	}
	return out
}

// Statement kinds.
const (
	StmtSelect      = "select"
	StmtInsert      = "insert"
	StmtUpdate      = "update"
	StmtDelete      = "delete"
	StmtMerge       = "merge"
	StmtWithModify  = "with (data-modifying)"
	StmtDDL         = "ddl"
	StmtTransaction = "transaction control"
	StmtCopy        = "copy"
	StmtOther       = "other"
	StmtEmpty       = "empty"
	StmtUnknown     = "unknown" // SQL text not known statically
)

// Classify returns the kind of the first statement and whether the text
// holds more than one statement.
func Classify(sql string) (kind string, multi bool) {
	toks := significant(lex(sql))
	if len(toks) == 0 {
		return StmtEmpty, false
	}
	for i, t := range toks {
		if t.kind == tPunct && t.text == ";" && i+1 < len(toks) {
			multi = true
			break
		}
	}
	first := strings.ToUpper(toks[0].text)
	if toks[0].kind == tPunct && toks[0].text == "(" && len(toks) > 1 {
		first = strings.ToUpper(toks[1].text)
	}
	switch first {
	case "SELECT", "VALUES", "TABLE", "SHOW":
		return StmtSelect, multi
	case "INSERT":
		return StmtInsert, multi
	case "UPDATE":
		return StmtUpdate, multi
	case "DELETE":
		return StmtDelete, multi
	case "MERGE":
		return StmtMerge, multi
	case "WITH":
		depth := 0
		for _, t := range toks[1:] {
			switch {
			case t.kind == tPunct && t.text == "(":
				depth++
			case t.kind == tPunct && t.text == ")":
				depth--
			case t.kind == tWord && depth > 0:
				switch strings.ToUpper(t.text) {
				case "INSERT", "UPDATE", "DELETE", "MERGE":
					return StmtWithModify, multi
				}
			case t.kind == tWord && depth == 0:
				switch strings.ToUpper(t.text) {
				case "INSERT", "UPDATE", "DELETE", "MERGE":
					return StmtWithModify, multi
				}
			}
		}
		return StmtSelect, multi
	case "CREATE", "ALTER", "DROP", "TRUNCATE", "GRANT", "REVOKE", "COMMENT", "REINDEX", "CLUSTER", "VACUUM", "ANALYZE", "REFRESH":
		return StmtDDL, multi
	case "BEGIN", "COMMIT", "ROLLBACK", "SAVEPOINT", "RELEASE", "START", "END", "SET", "RESET":
		return StmtTransaction, multi
	case "COPY":
		return StmtCopy, multi
	}
	return StmtOther, multi
}

// ReadOnly reports whether a statement kind never modifies data.
func ReadOnly(kind string) bool { return kind == StmtSelect }

// Placeholders returns the highest $N placeholder and the number of ?
// placeholders outside literals and comments.
func Placeholders(sql string) (dollar, question int) {
	for _, t := range lex(sql) {
		switch {
		case t.kind == tParam:
			if n, err := strconv.Atoi(t.text[1:]); err == nil && n > dollar {
				dollar = n
			}
		case t.kind == tPunct && t.text == "?":
			question++
		}
	}
	return
}

var breakBefore = map[string]bool{
	"FROM": true, "WHERE": true, "GROUP": true, "ORDER": true, "HAVING": true, "LIMIT": true, "OFFSET": true,
	"VALUES": true, "SET": true, "RETURNING": true, "UNION": true, "INTERSECT": true, "EXCEPT": true,
	"JOIN": true, "LEFT": true, "RIGHT": true, "INNER": true, "FULL": true, "CROSS": true, "ON": false,
	"SELECT": true, "INSERT": true, "UPDATE": true, "DELETE": true, "WITH": true, "WINDOW": true, "FOR": true,
}

// Format lays a statement out one clause per line (AND/OR indented) without
// changing tokens, literals or comments. Whitespace runs collapse to one space.
func Format(sql string) string {
	toks := lex(sql)
	var b strings.Builder
	depth := 0
	prevWord := ""
	lineStart := true
	write := func(s string) {
		b.WriteString(s)
		lineStart = false
	}
	newline := func(indent string) {
		if b.Len() > 0 {
			b.WriteString("\n" + indent)
			lineStart = true
		}
	}
	for i, t := range toks {
		switch t.kind {
		case tSpace:
			if !lineStart && i+1 < len(toks) {
				b.WriteString(" ")
			}
			continue
		case tComment:
			write(t.text)
			if strings.HasPrefix(t.text, "--") {
				newline("")
			}
			continue
		case tPunct:
			switch t.text {
			case "(":
				depth++
			case ")":
				depth--
			}
		case tWord:
			up := strings.ToUpper(t.text)
			if up == "ON" && strings.EqualFold(nextWord(toks, i), "CONFLICT") && depth == 0 {
				trimTrailingSpace(&b)
				newline("")
			} else if depth == 0 && breakBefore[up] && b.Len() > 0 {
				// Keep multi-word clauses together: LEFT JOIN, GROUP BY, UNION ALL.
				switch {
				case up == "JOIN" && (prevWord == "LEFT" || prevWord == "RIGHT" || prevWord == "INNER" || prevWord == "FULL" || prevWord == "CROSS" || prevWord == "OUTER"):
				case up == "SELECT" && (prevWord == "UNION" || prevWord == "ALL" || prevWord == "INTERSECT" || prevWord == "EXCEPT" || prevWord == "INSERT"):
				case up == "SET" && prevWord == "UPDATE" || up == "UPDATE" && prevWord == "DO":
				case up == "FROM" && prevWord == "DELETE":
				default:
					trimTrailingSpace(&b)
					newline("")
				}
			} else if depth == 0 && (up == "AND" || up == "OR") && prevWord != "BETWEEN" {
				trimTrailingSpace(&b)
				newline("  ")
			}
			prevWord = up
		}
		write(t.text)
	}
	return strings.TrimSpace(b.String())
}

func nextWord(toks []sqlToken, i int) string {
	for j := i + 1; j < len(toks); j++ {
		switch toks[j].kind {
		case tSpace, tComment:
			continue
		case tWord:
			return toks[j].text
		}
		return ""
	}
	return ""
}

func trimTrailingSpace(b *strings.Builder) {
	s := strings.TrimRight(b.String(), " ")
	b.Reset()
	b.WriteString(s)
}
