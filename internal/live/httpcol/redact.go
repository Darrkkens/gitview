package httpcol

// The functions in this file are copies of those of the collector module
// (github.com/athos/gitview/collector), which GitView cannot import; a test
// checks the copies stay identical.

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// NewID returns a new 32-hex-digit trace ID (W3C trace-id compatible).
func NewID() string {
	var b [16]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// ValidID accepts 8 to 64 characters of [A-Za-z0-9._-].
func ValidID(id string) bool {
	if len(id) < 8 || len(id) > 64 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

// MaskPath keeps only the path segments that are plainly words ("clients",
// "v2", "oauth2", "how-to-configure"): every other segment is replaced by a
// placeholder — "{n}" for numbers, "{uuid}", "{hex}", "{value}" for the
// rest — so identifiers, codes and tokens are never recorded.
func MaskPath(p string) string {
	if !strings.Contains(p, "/") {
		return p
	}
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = maskSegment(s)
	}
	return strings.Join(parts, "/")
}

func maskSegment(s string) string {
	switch {
	case s == "" || isWord(s) || strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") && len(s) <= 40:
		return s
	case isDigits(s):
		return "{n}"
	case isUUID(s):
		return "{uuid}"
	case len(s) >= 8 && isHex(s):
		return "{hex}"
	}
	return "{value}"
}

// isWord accepts runs of letters joined by single '-', '_' or '.', each run
// at most 16 letters with few case changes, optionally ending in up to two
// digits after at least two letters ("v2", "oauth2", "ipv6"; not "s3").
func isWord(s string) bool {
	if len(s) > 64 {
		return false
	}
	if len(s) >= 2 && len(s) <= 4 && (s[0] == 'v' || s[0] == 'V') && isDigits(s[1:]) {
		return true // API version: v1, v12
	}
	body := strings.TrimRight(s, "0123456789")
	if n := len(s) - len(body); n > 2 || n > 0 && len(body) < 2 {
		return false
	}
	run, changes := 0, 0
	prevUpper, prevSep := false, true
	for i, r := range body {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			upper := r <= 'Z'
			if run > 0 && upper != prevUpper {
				changes++
			}
			prevUpper = upper
			run++
			if run > 16 || changes > 3 {
				return false
			}
			prevSep = false
		case r == '-' || r == '_' || r == '.':
			if prevSep || i == len(body)-1 {
				return false
			}
			prevSep, run, changes = true, 0, 0
		default:
			return false
		}
	}
	return body != ""
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

func isHex(s string) bool {
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return s != ""
}

func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if r != '-' {
				return false
			}
		} else if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}

// RedactSQL removes string, numeric and dollar-quoted literals (replaced by
// "?") and comments from a SQL statement and collapses white space, so
// that values written into the statement text are not recorded.
// Placeholders ($1, ?, :name) and quoted identifiers are kept.
func RedactSQL(q string) string {
	var b strings.Builder
	space := false
	lastWord := "" // previous keyword or identifier, upper-cased
	emit := func(s string) {
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteString(s)
		lastWord = ""
	}
	isIdent := func(c byte) bool {
		return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c >= 0x80
	}
	for i := 0; i < len(q); {
		c := q[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f':
			space = true
			i++
		case c == '-' && i+1 < len(q) && q[i+1] == '-', c == '#':
			// "--" comments, and "#" comments of MySQL (in PostgreSQL "#"
			// is an operator: dropping the rest of the line only hides more).
			for i < len(q) && q[i] != '\n' && q[i] != '\r' {
				i++
			}
			space = true
		case c == '/' && i+1 < len(q) && q[i+1] == '*':
			depth := 0
			for i < len(q) {
				if strings.HasPrefix(q[i:], "/*") {
					depth++
					i += 2
				} else if strings.HasPrefix(q[i:], "*/") {
					depth--
					i += 2
					if depth == 0 {
						break
					}
				} else {
					i++
				}
			}
			space = true
		case c == '\'':
			i = skipString(q, i, false)
			emit("?")
		case (c == 'E' || c == 'e') && i+1 < len(q) && q[i+1] == '\'' && (i == 0 || !isIdent(q[i-1])):
			i = skipString(q, i+1, true)
			emit("?")
		case (c == 'B' || c == 'b' || c == 'X' || c == 'x' || c == 'N' || c == 'n') && i+1 < len(q) && q[i+1] == '\'' && (i == 0 || !isIdent(q[i-1])):
			i = skipString(q, i+1, false)
			emit("?")
		case (c == 'U' || c == 'u') && strings.HasPrefix(q[i+1:], "&'") && (i == 0 || !isIdent(q[i-1])):
			i = skipString(q, i+2, false)
			emit("?")
		case c == '"':
			j := i + 1
			for j < len(q) {
				if q[j] == '\\' && j+1 < len(q) {
					j += 2
					continue
				}
				if q[j] == '"' {
					if j+1 < len(q) && q[j+1] == '"' {
						j += 2
						continue
					}
					j++
					break
				}
				j++
			}
			// A quoted identifier in PostgreSQL, a string in MySQL: kept
			// only where an identifier is expected (after FROM, JOIN, INTO,
			// UPDATE, TABLE, or next to a dot).
			prevDot := b.Len() > 0 && b.String()[b.Len()-1] == '.' && !space
			nextDot := j < len(q) && q[j] == '.'
			switch lastWord {
			case "FROM", "JOIN", "INTO", "UPDATE", "TABLE":
				prevDot = true
			}
			if prevDot || nextDot {
				emit(q[i:j])
			} else {
				emit("?")
			}
			i = j
		case c == '$':
			j := i + 1
			for j < len(q) && q[j] >= '0' && q[j] <= '9' {
				j++
			}
			if j > i+1 { // $1 placeholder
				emit(q[i:j])
				i = j
				break
			}
			// $tag$ … $tag$ dollar quote
			j = i + 1
			for j < len(q) && (isIdent(q[j]) && !(j == i+1 && q[j] >= '0' && q[j] <= '9')) {
				j++
			}
			if j < len(q) && q[j] == '$' {
				tag := q[i : j+1]
				if end := strings.Index(q[j+1:], tag); end >= 0 {
					i = j + 1 + end + len(tag)
				} else {
					i = len(q)
				}
				emit("?")
				break
			}
			emit("$")
			i++
		case c >= '0' && c <= '9' || c == '.' && i+1 < len(q) && q[i+1] >= '0' && q[i+1] <= '9':
			if i > 0 && isIdent(q[i-1]) {
				// part of an identifier such as t1 (handled below)
				emit(string(c))
				i++
				break
			}
			j := i
			for j < len(q) && (isIdent(q[j]) || q[j] == '.' || (q[j] == '+' || q[j] == '-') && j > i && (q[j-1] == 'e' || q[j-1] == 'E')) {
				j++
			}
			emit("?")
			i = j
		case isIdent(c):
			j := i
			for j < len(q) && (isIdent(q[j]) || q[j] == '$') {
				j++
			}
			emit(q[i:j])
			lastWord = strings.ToUpper(q[i:j])
			i = j
		default:
			emit(string(c))
			i++
		}
	}
	out := b.String()
	if len(out) > 4096 {
		out = out[:4096] + "…"
	}
	return out
}

// skipString returns the index after the string literal whose opening
// quote is at i. Backslash escapes are honoured in every string (MySQL
// style): where a backslash is literal (standard SQL), the string merely
// appears to run further, which hides more text, never less.
func skipString(q string, i int, _ bool) int {
	j := i + 1
	for j < len(q) {
		switch {
		case q[j] == '\\' && j+1 < len(q):
			j += 2
		case q[j] == '\'' && j+1 < len(q) && q[j+1] == '\'':
			j += 2
		case q[j] == '\'':
			return j + 1
		default:
			j++
		}
	}
	return j
}
