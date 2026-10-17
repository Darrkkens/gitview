package export

import (
	"fmt"
	"sort"
	"strings"
)

// writeInspectMarkdown summarises the Go Deep Inspector section. The full
// results are in the HTML and JSON exports.
func writeInspectMarkdown(b *strings.Builder, d *InspectData) {
	p := func(format string, a ...any) { fmt.Fprintf(b, format, a...) }
	p("\n## Go Deep Inspector\n\n")
	p("Resultados estáticos descrevem caminhos possíveis; traces e benchmarks são observações; perfis e planos são estimativas.\n")
	for _, s := range d.Symbols {
		p("\n### %s\n\n", mdCode(s.Symbol.Qualified))
		p("- **Local:** %s · **ID:** %s\n", mdCode(fmt.Sprintf("%s:%d", s.Symbol.File, s.Symbol.Line)), mdCode(s.Symbol.ID))
		if g := s.Branches; g != nil {
			m := g.Metrics
			p("- **Branch Explorer:** %d blocos, %d decisões, complexidade ciclomática %d, %d retornos, %d caminhos\n", m.Blocks, m.DecisionPoints, m.Cyclomatic, m.Returns, m.Paths)
		}
		var sources []string
		for _, r := range s.DataFlow {
			for _, id := range r.Sources {
				sources = append(sources, r.Node(id).Label)
			}
		}
		sort.Strings(sources)
		if len(sources) > 0 {
			p("- **Data Flow — fontes encontradas:** %s\n", mdText(strings.Join(dedupe(sources), "; ")))
		}
		if s.SQL != nil {
			for _, op := range s.SQL.Ops {
				sql := op.SQL.Text
				switch {
				case sql != "":
				case op.SQL.Template != "":
					sql = "não resolvido estaticamente: " + op.SQL.Template
				default:
					sql = op.SQL.Kind
				}
				p("- **SQL:** %s %s em %s — %s\n", op.Lib, op.Op, mdCode(fmt.Sprintf("%s:%d", op.File, op.Line)), mdCode(strings.Join(strings.Fields(sql), " ")))
			}
		}
		if s.Trace != nil && !s.Trace.Loaded {
			p("- **Execution Trace:** %s\n", s.Trace.Message)
		}
		if s.Perf != nil && s.Perf.Message != "" {
			p("- **Performance:** %s\n", s.Perf.Message)
		}
	}
	for _, n := range d.Notes {
		p("\n> %s\n", mdText(n))
	}
}

func dedupe(xs []string) []string {
	var out []string
	for i, x := range xs {
		if i == 0 || x != xs[i-1] {
			out = append(out, x)
		}
	}
	return out
}
