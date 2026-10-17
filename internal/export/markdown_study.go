package export

import (
	"fmt"
	"strings"

	"github.com/athos/gitview/internal/study"
)

// writeStudyMarkdown lists the reading order and the change journey; the
// interactive parts of Study Mode live in the HTML report.
func writeStudyMarkdown(b *strings.Builder, m *study.Model) {
	p := func(f string, a ...any) { fmt.Fprintf(b, f, a...) }
	p("\n## Study: start here\n\n")
	if len(m.ReadingOrder) == 0 {
		p("No reading order: no changed Go code was found.\n")
	}
	for _, s := range m.ReadingOrder {
		loc := s.File
		if s.Line > 0 {
			loc = fmt.Sprintf("%s:%d", s.File, s.Line)
		}
		p("%d. **%s** `%s` — %s", s.N, s.Layer, mdEscape(s.Title), loc)
		if len(s.Why) > 0 {
			w := s.Why[len(s.Why)-1]
			p(" — %s [%s]", mdEscape(w.Text), w.Basis)
		}
		p("\n")
	}
	if len(m.Changes) > 0 {
		p("\n### Change journey\n\n")
		for _, g := range m.Changes {
			p("- **%s**\n", g.Layer)
			for _, f := range g.Files {
				p("  - `%s` (+%d −%d) [%s]\n", mdEscape(f.Path), f.Added, f.Deleted, f.Basis)
			}
		}
	}
	if len(m.SQL.Queries) > 0 {
		p("\n### SQL (read from the code)\n\n")
		for _, q := range m.SQL.Queries {
			p("- `%s` %s at %s:%d", mdEscape(q.FuncName), strings.ToUpper(q.Statement), q.File, q.Line)
			if s := q.Structure; s != nil {
				var ts []string
				for _, t := range s.Tables {
					ts = append(ts, t.Name)
				}
				if len(ts) > 0 {
					p(" — tables: %s", mdEscape(strings.Join(ts, ", ")))
				}
				if len(s.Filters) > 0 {
					p(" — WHERE: `%s`", mdEscape(strings.Join(s.Filters, " AND ")))
				}
			}
			p(" [SQL]\n")
		}
	}
	if n := len(m.Exercises); n > 0 {
		p("\n%d quiz questions and challenges, with their evidence, are in the Study tab of the HTML report.\n", n)
	}
}

func mdEscape(s string) string { return strings.ReplaceAll(s, "`", "'") }
