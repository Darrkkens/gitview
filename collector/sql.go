package collector

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

// SQLSpan is one SQL statement observed while a capture was armed.
type SQLSpan struct {
	Query string `json:"query"` // literals and comments removed
	// Start is the offset from the start of the request, in nanoseconds;
	// for statements outside any request it is a Unix time in nanoseconds.
	Start      int64  `json:"start"`
	Dur        int64  `json:"dur"`
	File       string `json:"file,omitempty"`
	Line       int    `json:"line,omitempty"`
	Func       string `json:"func,omitempty"`
	Rows       int64  `json:"rows,omitempty"`
	HasRows    bool   `json:"hasRows,omitempty"`
	Err        string `json:"err,omitempty"` // the error's type, never its message
	Unfinished bool   `json:"unfinished,omitempty"`
	TraceID    string `json:"traceId,omitempty"`
}

// Span is a SQL statement being executed. A nil Span (returned when no
// capture is armed) accepts End calls and does nothing.
type Span struct {
	c    *Collector
	rs   *requestState
	t0   time.Time
	span SQLSpan
	done atomic.Bool
}

// maxOrphans is the number of statements outside requests kept.
const maxOrphans = 1024

// defaultSkip are the packages a call site is never attributed to.
var defaultSkip = []string{
	"github.com/athos/gitview/collector.(*Collector).StartSQL",
	"github.com/athos/gitview/collector.SQL",
	"runtime.",
	"database/sql.",
	"github.com/jackc/pgx",
	"github.com/lib/pq",
	"github.com/jmoiron/sqlx",
	"gorm.io/",
}

// SQL starts recording a SQL statement executed for the request in ctx,
// attributing it to the caller's source line, with the default collector.
//
//	span := collector.SQL(ctx, query)
//	rows, err := db.QueryContext(ctx, query, args...)
//	span.End(err)
func SQL(ctx context.Context, query string) *Span {
	return Default().StartSQL(ctx, query)
}

// StartSQL records a SQL statement. The call site is the first stack frame
// outside the collector, the runtime, database/sql and common drivers, and
// outside the extra package prefixes in skip.
func (c *Collector) StartSQL(ctx context.Context, query string, skip ...string) *Span {
	if c.disabled {
		return nil
	}
	rs := fromContext(ctx)
	if rs != nil {
		c = rs.c
	}
	if c.state() == nil {
		return nil
	}
	s := &Span{c: c, rs: rs, t0: time.Now()}
	s.span.Query = RedactSQL(query)
	if rs != nil {
		s.span.Start = int64(s.t0.Sub(rs.start))
		s.span.TraceID = rs.req.ID
	} else {
		s.span.Start = s.t0.UnixNano()
	}
	s.span.File, s.span.Line, s.span.Func = callSite(skip)
	return s
}

func callSite(extra []string) (string, int, string) {
	var pcs [32]uintptr
	n := runtime.Callers(2, pcs[:])
	frames := runtime.CallersFrames(pcs[:n])
	for {
		f, more := frames.Next()
		skip := false
		for _, list := range [][]string{defaultSkip, extra} {
			for _, p := range list {
				if strings.HasPrefix(f.Function, p) {
					skip = true
				}
			}
		}
		if !skip && f.Function != "" {
			return f.File, f.Line, f.Function
		}
		if !more {
			return "", 0, ""
		}
	}
}

// End finishes the statement.
func (s *Span) End(err error) { s.EndRows(err, -1) }

// EndRows finishes the statement with the number of rows it returned or
// affected (negative when unknown).
func (s *Span) EndRows(err error, rows int64) {
	if s == nil || !s.done.CompareAndSwap(false, true) {
		return
	}
	s.span.Dur = int64(time.Since(s.t0))
	if rows >= 0 {
		s.span.Rows, s.span.HasRows = rows, true
	}
	if err != nil {
		s.span.Err = fmt.Sprintf("%T", err)
	}
	if s.rs == nil {
		s.c.mu.Lock()
		s.c.orphSeq++
		if len(s.c.orphans) < maxOrphans {
			s.c.orphans = append(s.c.orphans, s.span)
		} else {
			s.c.orphans[int((s.c.orphSeq-1)%maxOrphans)] = s.span
		}
		s.c.mu.Unlock()
		return
	}
	rs := s.rs
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.finished {
		// The handler returned before the statement finished (it ran in a
		// goroutine the request did not wait for): the request record is
		// already published.
		return
	}
	if len(rs.sql) >= rs.c.opts.MaxSQL {
		rs.dropped++
		return
	}
	rs.sql = append(rs.sql, s.span)
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
