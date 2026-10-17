package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

func ansiTruncate(s string, w int) string { return ansi.Truncate(s, max(w, 0), sym.ellipsis) }

// truncateLeft keeps the end of s (the most specific part of a path).
func truncateLeft(s string, w int) string {
	if runewidth.StringWidth(s) <= w {
		return s
	}
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	for len(r) > 0 && runewidth.StringWidth(string(r))+runewidth.StringWidth(sym.ellipsis) > w {
		r = r[1:]
	}
	return sym.ellipsis + string(r)
}

func statusBadge(status string) (string, lipgloss.Style) {
	switch status {
	case "?", "A":
		return "A", stAdd
	case "D":
		return "D", stDel
	case "R", "C":
		return "R", stAccent
	default:
		return "M", stMod
	}
}

func splitPath(p string) (name, dir string) {
	dir = filepath.Dir(p)
	if dir == "." {
		dir = ""
	}
	return filepath.Base(p), dir
}

func statsText(f FileChange) (plain string, styled func(lipgloss.Style) string) {
	if f.Binary {
		return "bin", func(bg lipgloss.Style) string { return bg.Inherit(stMuted).Render("bin") }
	}
	var parts []string
	var render []string
	if f.Added > 0 {
		s := fmt.Sprintf("+%d", f.Added)
		parts = append(parts, s)
		render = append(render, "a"+s)
	}
	if f.Deleted > 0 {
		s := fmt.Sprintf("-%d", f.Deleted)
		parts = append(parts, s)
		render = append(render, "d"+s)
	}
	return strings.Join(parts, " "), func(bg lipgloss.Style) string {
		var out []string
		for _, r := range render {
			st := stAdd
			if r[0] == 'd' {
				st = stDel
			}
			out = append(out, bg.Inherit(st).Render(r[1:]))
		}
		return strings.Join(out, bg.Render(" "))
	}
}

// renderFileHeader draws one file row:
//
//	▌▾ M  name.go       some/dir           +3 -1
//
// nameCol aligns directories across rows; the file name always wins over the
// directory when space is short, and stats stay right-aligned.
func renderFileHeader(f FileChange, width, nameCol int, selected, expanded, recent bool) string {
	bg := lipgloss.NewStyle()
	if selected {
		bg = bg.Background(colSelBg)
	}
	with := func(s lipgloss.Style) lipgloss.Style { return bg.Inherit(s) }

	marker := bg.Render(" ")
	if selected {
		marker = with(stAccent).Render(sym.bar)
	}
	arrow, arrowSt := sym.closed, stMuted
	if expanded {
		arrow, arrowSt = sym.open, stSubtle
	}
	if selected {
		arrowSt = stAccent
	}
	badge, badgeSt := statusBadge(f.Status)
	left := marker + with(arrowSt).Render(arrow+" ") + with(badgeSt).Render(badge) + bg.Render("  ")
	leftW := lipgloss.Width(left)

	statsPlain, statsStyled := statsText(f)
	statsW := runewidth.StringWidth(statsPlain)
	dot := ""
	if recent {
		dot = " " + sym.dot
	}
	avail := width - leftW - statsW - 2 - runewidth.StringWidth(dot)

	name, dir := splitPath(f.Path)
	nameW := runewidth.StringWidth(name)
	if nameW > avail {
		name, dir = truncateLeft(name, avail), ""
		nameW = runewidth.StringWidth(name)
	}
	nameSt := stText
	if selected {
		nameSt = stBold
	}
	body := with(nameSt).Render(name)
	if dot != "" {
		body += with(stAccent).Render(dot)
	}
	used := nameW + runewidth.StringWidth(dot)
	if dir != "" {
		col := max(min(nameCol, avail-12), used) + 2
		if dirW := avail - col; dirW >= 6 {
			dir = truncateLeft(dir, dirW)
			body += bg.Render(strings.Repeat(" ", col-used)) + with(stSubtle).Render(dir)
			used = col + runewidth.StringWidth(dir)
		}
	}
	gap := max(width-leftW-used-statsW-1, 1)
	return left + body + bg.Render(strings.Repeat(" ", gap)) + statsStyled(bg) + bg.Render(" ")
}

func renderTopBar(width int, branch string, files []FileChange, extra string, live, failed bool) string {
	adds, dels := 0, 0
	for _, f := range files {
		adds += f.Added
		dels += f.Deleted
	}
	left := " " + stTitle.Render("GitView")
	if branch != "" {
		left += "  " + stSubtle.Render(truncateLeft(branch, max(width/3, 8)))
	}

	count := fmt.Sprintf("%d arquivo", len(files))
	if len(files) != 1 {
		count += "s"
	}
	status := stAdd.Render(sym.dot) + stMuted.Render(" LIVE")
	if !live {
		status = stMod.Render(sym.dot) + stMuted.Render(" POLL")
	}
	if failed {
		status = stDel.Render(sym.dot) + stMuted.Render(" ERRO")
	}
	// Drop the least important parts first when the terminal is narrow.
	parts := []string{
		stSubtle.Render(count),
		stAdd.Render(fmt.Sprintf("+%d", adds)) + " " + stDel.Render(fmt.Sprintf("-%d", dels)),
	}
	if extra != "" {
		parts = append(parts, extra)
	}
	parts = append(parts, status)
	for len(parts) > 0 {
		right := strings.Join(parts, "   ") + " "
		if lipgloss.Width(left)+lipgloss.Width(right)+1 <= width {
			return left + strings.Repeat(" ", width-lipgloss.Width(left)-lipgloss.Width(right)) + right
		}
		parts = parts[1:]
	}
	return ansiTruncate(left, width)
}

type keyHint struct{ key, desc string }

func renderHints(width int, hints []keyHint, essential int) string {
	render := func(hs []keyHint) string {
		var out []string
		for _, h := range hs {
			out = append(out, stText.Render(h.key)+" "+stMuted.Render(h.desc))
		}
		return " " + strings.Join(out, stBorder.Render("   "))
	}
	// Keep the trailing `essential` hints; drop optional ones from the end of the optional list.
	opt, ess := hints[:len(hints)-essential], hints[len(hints)-essential:]
	for n := len(opt); n >= 0; n-- {
		s := render(append(append([]keyHint{}, opt[:n]...), ess...))
		if lipgloss.Width(s) <= width {
			return s
		}
	}
	return ansiTruncate(render(ess), width)
}

var diffHelp = []keyHint{
	{"↑ ↓  k j", "arquivo anterior / próximo"},
	{"Tab  Shift+Tab", "próximo / anterior"},
	{"Enter  Espaço", "expandir / recolher"},
	{"e  c", "expandir todos / recolher todos"},
	{"← →  h l", "rolagem horizontal (0 volta ao início)"},
	{"PgUp PgDn", "rolar meia página"},
	{"J K", "rolar uma linha"},
	{"g G", "início / fim"},
	{"/", "busca global nos diffs"},
	{"n N", "próxima / anterior ocorrência"},
	{"Esc", "limpar busca"},
	{"t", "painel de testes"},
	{"f", "Code Flow: chamadores e chamadas de uma função Go"},
	{"i", "Go Deep Inspector: Data Flow, Trace, Branches, Performance, SQL"},
	{"m", "Runtime Performance: medir um processo Go em execução (PID)"},
	{"a", "Repository Graph: o repositório inteiro como grafo (exporta HTML)"},
	{"x", "exportar relatório"},
	{"d", "alternar renderização Delta / nativa"},
	{"r", "atualizar agora"},
	{"q", "sair"},
}

var searchHelp = []keyHint{
	{"digitar", "busca em conteúdo, nome e caminho"},
	{"↑ ↓  Tab", "escolher arquivo"},
	{"Enter", "abrir na primeira ocorrência"},
	{"Alt+C", "diferenciar maiúsculas"},
	{"Alt+R", "expressão regular (sintaxe RE2)"},
	{"n N", "depois de abrir: próxima / anterior"},
	{"Esc", "fechar e limpar a busca"},
}

var exportHelp = []keyHint{
	{"Tab", "próxima seção"},
	{"↑ ↓", "escolher formato / escopo"},
	{"Espaço", "incluir conteúdo sensível"},
	{"Enter", "exportar (de novo para sobrescrever)"},
	{"Esc", "cancelar"},
}

var testsHelp = []keyHint{
	{"Enter", "executar o alvo selecionado"},
	{"a  Tab", "alternar alvos / resultados"},
	{"r", "reexecutar o último alvo"},
	{"f", "reexecutar apenas falhas"},
	{"c", "cancelar execução"},
	{"l", "logs completos (G segue o fim)"},
	{"w", "modo watch: reexecuta quando os diffs mudam"},
	{"Esc", "voltar"},
}

func (m *model) helpRows() []keyHint {
	switch m.screen {
	case screenSearch:
		return searchHelp
	case screenExport:
		return exportHelp
	case screenTests:
		return testsHelp
	case screenRuntime:
		return runtimeHelp
	case screenGraph:
		return graphHelp
	case screenFlow:
		if m.insp.tab != tabCodeFlow && m.flow.phase == phaseGraph {
			return inspHelp
		}
		return flowHelp
	}
	return diffHelp
}

func renderHelp(width, height int, rows []keyHint) string {
	var b strings.Builder
	b.WriteString(" " + stBold.Render("Atalhos") + "\n\n")
	for _, r := range rows {
		line := "   " + stAccent.Render(fmt.Sprintf("%-16s", r.key)) + stSubtle.Render(r.desc)
		b.WriteString(ansiTruncate(line, width) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// fitBody pads or cuts s to exactly h lines (so the footer never moves) and
// truncates lines wider than w with an ellipsis instead of a hard cut.
func fitBody(s string, w, h int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for i, l := range lines {
		if lipgloss.Width(l) > w {
			lines[i] = ansiTruncate(l, w)
		}
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func truncateRight(s string, w int) string { return runewidth.Truncate(s, w, sym.ellipsis) }

func itoa(n int) string { return fmt.Sprint(n) }
