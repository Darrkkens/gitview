package export

import (
	"fmt"
	"strings"
	"time"

	"github.com/athos/gitview/internal/gitdiff"
)

func renderMarkdown(r Report) []byte {
	var b strings.Builder
	p := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	added, deleted := r.totals()

	p("# GitView · %s\n\n", mdText(r.Project))
	p("- **Branch:** %s\n", mdCode(r.Branch))
	p("- **Exportado em:** %s\n", r.GeneratedAt.Format("2006-01-02 15:04:05 -0700"))
	if r.Scope != "" {
		p("- **Escopo:** %s\n", mdText(r.Scope))
	}
	if r.Query != "" {
		p("- **Busca ativa:** %s\n", mdCode(r.Query))
	}

	p("\n## Resumo\n\n%d arquivo(s) · **+%d** / **-%d**\n\n", len(r.Files), added, deleted)
	if len(r.Files) > 0 {
		p("| Status | Arquivo | + | - |\n|:---:|---|---:|---:|\n")
		for _, f := range r.Files {
			name := mdCode(f.Path)
			if f.OldPath != "" {
				name = mdCode(f.OldPath) + " → " + name
			}
			p("| %s | %s | %d | %d |\n", f.Status, name, f.Added, f.Deleted)
		}
	}

	p("\n## Testes\n\n")
	writeTestsMarkdown(&b, r.Tests)
	if r.Study != nil {
		writeStudyMarkdown(&b, r.Study)
	}
	if r.Inspect != nil {
		writeInspectMarkdown(&b, r.Inspect)
	}

	if len(r.Warnings) > 0 || len(r.Omitted) > 0 {
		p("\n## Avisos\n\n")
		for _, w := range r.Warnings {
			p("- %s\n", mdText(w))
		}
		for _, o := range r.Omitted {
			p("- Conteúdo de %s omitido: arquivo potencialmente sensível.\n", mdCode(o))
		}
	}

	p("\n## Alterações\n")
	for _, f := range r.Files {
		p("\n### %s\n\n", mdCode(f.Path))
		switch {
		case r.omitted(f.Path):
			p("_Conteúdo omitido (arquivo potencialmente sensível)._\n")
		case f.Binary:
			p("_Arquivo binário._\n")
		case len(f.Hunks) == 0:
			p("_Sem alterações de conteúdo._\n")
		default:
			body := diffText(f.Hunks)
			fence := codeFence(body)
			p("%sdiff\n%s%s\n", fence, body, fence)
		}
	}
	return []byte(b.String())
}

func writeTestsMarkdown(b *strings.Builder, t *TestReport) {
	if t == nil || !t.Ran {
		b.WriteString("_Testes não executados nesta sessão._\n")
		return
	}
	fmt.Fprintf(b, "- **Alvo:** %s\n- **Status:** %s\n", mdText(t.Target), t.Status)
	if !t.Finished.IsZero() {
		fmt.Fprintf(b, "- **Duração:** %s\n", t.Finished.Sub(t.Started).Round(time.Millisecond))
	}
	fmt.Fprintf(b, "- **Código de saída:** %d\n", t.ExitCode)
	if t.Error != "" {
		fmt.Fprintf(b, "- **Erro:** %s\n", mdText(t.Error))
	}
	if !t.Structured {
		b.WriteString("\n_Saída sem formato estruturado: contagens individuais indisponíveis._\n")
		return
	}
	fmt.Fprintf(b, "- **Resultado:** %d aprovado(s), %d falha(s), %d ignorado(s)\n", t.Passed, t.Failed, t.Skipped)
	if len(t.Cases) == 0 {
		return
	}
	b.WriteString("\n| Status | Teste | Pacote | Tempo |\n|:---:|---|---|---:|\n")
	for _, c := range t.Cases {
		fmt.Fprintf(b, "| %s | %s | %s | %dms |\n", c.Status, mdCode(c.Name), mdCode(c.Package), c.ElapsedMs)
	}
	for _, c := range t.Cases {
		if c.Status == "failed" && c.Output != "" {
			fence := codeFence(c.Output)
			fmt.Fprintf(b, "\n**Falha: %s**\n\n%stext\n%s\n%s\n", mdCode(c.Name), fence, strings.TrimRight(c.Output, "\n"), fence)
		}
	}
}

func diffText(hunks []gitdiff.Hunk) string {
	var b strings.Builder
	for _, h := range hunks {
		b.WriteString(h.Header + "\n")
		for _, l := range h.Lines {
			switch l.Kind {
			case gitdiff.Added:
				b.WriteByte('+')
			case gitdiff.Removed:
				b.WriteByte('-')
			default:
				b.WriteByte(' ')
			}
			b.WriteString(l.Text + "\n")
		}
	}
	return b.String()
}

// codeFence returns a backtick fence longer than any backtick run in s.
func codeFence(s string) string {
	longest, run := 0, 0
	for _, r := range s {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return strings.Repeat("`", max(3, longest+1))
}

// mdCode renders s as an inline code span that survives backticks and pipes.
func mdCode(s string) string {
	if s == "" {
		return "—"
	}
	s = strings.ReplaceAll(s, "|", `\|`)
	fence := "`"
	if strings.Contains(s, "`") {
		fence = "`` "
		return fence + s + " ``"
	}
	return fence + s + fence
}

var mdEscaper = strings.NewReplacer(
	`\`, `\\`, "`", "\\`", "*", `\*`, "_", `\_`, "[", `\[`, "]", `\]`,
	"<", "&lt;", ">", "&gt;", "|", `\|`, "#", `\#`,
)

func mdText(s string) string { return mdEscaper.Replace(s) }
