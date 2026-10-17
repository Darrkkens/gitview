package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	tr "github.com/athos/gitview/internal/testrunner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

type testsView int

const (
	viewTargets testsView = iota
	viewResults
	viewLogs
)

type testsUI struct {
	runner *tr.Runner
	config []tr.ProjectConfig

	projects    []*tr.Project
	discovered  bool
	discovering bool
	discoverErr error
	targets     []tr.Target

	view    testsView
	tsel    int
	rsel    int
	logOff  int
	follow  bool
	ack     bool       // user acknowledged that tests run repository code
	pending *tr.Target // target awaiting that acknowledgement
	watch   bool
	last    *tr.Target
	snap    tr.RunState
	msg     string
}

type (
	testsDiscoveredMsg struct {
		projects []*tr.Project
		err      error
	}
	testUpdateMsg struct{}
)

func newTestsUI(r *tr.Runner, cfg []tr.ProjectConfig) *testsUI {
	return &testsUI{runner: r, config: cfg, follow: true}
}

// waitTests relays runner updates to the UI, throttled so a fast test stream
// cannot starve key handling or git monitoring.
func (m *model) waitTests() tea.Cmd {
	ch := m.tests.runner.Updates()
	return func() tea.Msg {
		<-ch
		time.Sleep(60 * time.Millisecond)
		return testUpdateMsg{}
	}
}

func (m *model) openTests() tea.Cmd {
	t := m.tests
	m.screen = screenTests
	t.msg = ""
	if t.discovering {
		return nil
	}
	if t.discovered {
		m.updateTargets()
		return nil
	}
	t.discovering = true
	root, cfg := m.repo.Root, t.config
	return func() tea.Msg {
		p, err := tr.Discover(root, cfg)
		return testsDiscoveredMsg{p, err}
	}
}

func (m *model) testsDiscovered(msg testsDiscoveredMsg) {
	t := m.tests
	t.discovering, t.discovered = false, true
	t.projects, t.discoverErr = msg.projects, msg.err
	m.updateTargets()
}

// updateTargets recomputes targets, including the one related to the file
// currently selected in the diff view.
func (m *model) updateTargets() {
	t := m.tests
	var label string
	if t.tsel < len(t.targets) {
		label = t.targets[t.tsel].Label
	}
	t.targets = tr.Targets(m.repo.Root, t.projects, m.cursor)
	t.tsel = 0
	for i, tg := range t.targets {
		if tg.Label == label {
			t.tsel = i
		}
	}
}

func (t *testsUI) clampSelection() {
	t.rsel = min(t.rsel, max(len(t.snap.Tests)-1, 0))
}

func (t *testsUI) hasRun() bool { return !t.snap.Started.IsZero() }

// start asks for the one-time acknowledgement, then launches the target.
func (m *model) startTests(target tr.Target) tea.Cmd {
	t := m.tests
	if !t.ack {
		t.pending = &target
		return nil
	}
	t.pending = nil
	if err := t.runner.Start(target); err != nil {
		if errors.Is(err, tr.ErrBusy) {
			t.msg = "Já existe uma execução em andamento (c cancela)"
		} else {
			t.msg = err.Error()
		}
		return nil
	}
	t.last = &target
	t.view, t.rsel, t.follow, t.msg = viewResults, 0, true, ""
	t.snap = t.runner.Snapshot()
	return nil
}

// testsOnChange re-runs the last target when watch mode was explicitly enabled.
func (m *model) testsOnChange() tea.Cmd {
	t := m.tests
	if !t.watch || t.last == nil || t.runner.Running() {
		return nil
	}
	return m.startTests(*t.last)
}

func (m *model) testsKey(msg tea.KeyMsg) tea.Cmd {
	t := m.tests
	key := msg.String()

	if t.pending != nil {
		switch key {
		case "enter", "y":
			t.ack = true
			return m.startTests(*t.pending)
		case "esc", "n", "q":
			t.pending = nil
		}
		return nil
	}

	switch key {
	case "esc", "q":
		if t.view == viewLogs {
			t.view = viewResults
			if !t.hasRun() {
				t.view = viewTargets
			}
			return nil
		}
		m.screen = screenDiff
		return nil
	case "?":
		m.showHelp = true
		return nil
	case "tab", "a":
		if t.view == viewTargets && t.hasRun() {
			t.view = viewResults
		} else {
			t.view = viewTargets
			m.updateTargets()
		}
		return nil
	case "r":
		if t.last != nil {
			return m.startTests(*t.last)
		}
		if t.view == viewTargets && t.tsel < len(t.targets) {
			return m.startTests(t.targets[t.tsel])
		}
		return nil
	case "f":
		if t.runner.Running() {
			t.msg = "Aguarde o fim da execução atual"
			return nil
		}
		if target, ok := tr.FailedTarget(t.snap); ok {
			return m.startTests(target)
		}
		t.msg = "Nenhuma falha para reexecutar"
		return nil
	case "c", "ctrl+x":
		if t.runner.Running() {
			t.runner.Cancel()
			t.msg = "Cancelando…"
		}
		return nil
	case "w":
		t.watch = !t.watch
		if t.watch {
			t.msg = "Modo watch ativo: o último alvo roda novamente quando os diffs mudarem"
		} else {
			t.msg = "Modo watch desativado"
		}
		return nil
	case "l":
		t.view = viewLogs
		t.follow = true
		return nil
	}

	switch t.view {
	case viewTargets:
		switch key {
		case "up", "k":
			t.tsel = max(t.tsel-1, 0)
		case "down", "j":
			t.tsel = min(t.tsel+1, max(len(t.targets)-1, 0))
		case "enter":
			if t.tsel < len(t.targets) {
				return m.startTests(t.targets[t.tsel])
			}
		}
	case viewResults:
		switch key {
		case "up", "k":
			t.rsel = max(t.rsel-1, 0)
		case "down", "j":
			t.rsel = min(t.rsel+1, max(len(t.snap.Tests)-1, 0))
		case "g", "home":
			t.rsel = 0
		case "G", "end":
			t.rsel = max(len(t.snap.Tests)-1, 0)
		}
	case viewLogs:
		page := max(m.vp.Height-3, 1)
		switch key {
		case "up", "k":
			t.follow, t.logOff = false, max(t.logOff-1, 0)
		case "down", "j":
			t.logOff++
		case "pgup", "ctrl+u":
			t.follow, t.logOff = false, max(t.logOff-page, 0)
		case "pgdown", "ctrl+d", " ":
			t.logOff += page
		case "g", "home":
			t.follow, t.logOff = false, 0
		case "G", "end":
			t.follow = true
		}
	}
	return nil
}

func statusStyle(s tr.Status) (string, lipgloss.Style) {
	switch s {
	case tr.Passed:
		return sym.pass, stAdd
	case tr.Failed:
		return sym.fail, stDel
	case tr.Skipped:
		return sym.skip, stMuted
	case tr.Running:
		return sym.running, stAccent
	case tr.Cancelled:
		return sym.skip, stSubtle
	case tr.InfraError:
		return sym.warn, stMod
	}
	return sym.idle, stMuted
}

// indicator is the compact test status shown in the top bar.
func (t *testsUI) indicator() string {
	s := t.snap
	if !t.hasRun() {
		return ""
	}
	c := s.Counts()
	icon, st := statusStyle(s.Status)
	switch s.Status {
	case tr.Running:
		return st.Render(icon) + stMuted.Render(fmt.Sprintf(" testes %d", c.Passed+c.Failed+c.Skipped))
	case tr.Passed:
		return st.Render(icon) + stMuted.Render(fmt.Sprintf(" %d", c.Passed))
	case tr.Failed:
		return st.Render(icon) + stMuted.Render(fmt.Sprintf(" %d", max(c.Failed, 1)))
	}
	return st.Render(icon) + stMuted.Render(" testes")
}

func (m *model) testsView() string {
	t := m.tests
	w, h := m.width, m.vp.Height

	if t.pending != nil {
		return m.testsAckView(*t.pending)
	}
	if t.discovering {
		return " " + stBold.Render("Testes") + "\n\n  " + stMuted.Render("Procurando projetos de teste…")
	}

	var lines []string
	switch t.view {
	case viewTargets:
		lines = m.testsTargetsView()
	case viewLogs:
		lines = m.testsLogView()
	default:
		lines = m.testsResultsView()
	}
	if t.msg != "" {
		lines = append(lines[:min(len(lines), h-2)], "", " "+stSubtle.Render(ansiTruncate(t.msg, w-2)))
	}
	return strings.Join(lines, "\n")
}

func (m *model) testsTitle(right string) string {
	left := " " + stBold.Render("Testes")
	if t := m.tests; t.watch {
		left += "  " + stAccent.Render("watch")
	}
	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(right)-1, 1)
	return left + strings.Repeat(" ", gap) + right + " "
}

func (m *model) testsAckView(target tr.Target) string {
	w := m.width
	lines := []string{
		m.testsTitle(""), "",
		" " + stMod.Render(sym.warn+" Executar testes roda código do repositório"),
		"",
		"   " + stSubtle.Render(ansiTruncate("Testes podem acessar rede, arquivos, bancos de dados e outros", w-4)),
		"   " + stSubtle.Render(ansiTruncate("serviços externos. Execute apenas código em que você confia.", w-4)),
		"",
		"   " + stMuted.Render("Alvo: ") + stText.Render(ansiTruncate(target.Label, w-12)),
	}
	for _, u := range target.Units {
		lines = append(lines, "   "+stMuted.Render("em ")+stSubtle.Render(ansiTruncate(u.Project.Rel+"  ("+u.Project.Framework+")", w-8)))
	}
	lines = append(lines, "", " "+stText.Render("Enter")+" "+stMuted.Render("confirmar para esta sessão")+"   "+stText.Render("Esc")+" "+stMuted.Render("cancelar"))
	return strings.Join(lines, "\n")
}

func (m *model) testsTargetsView() []string {
	t := m.tests
	w := m.width
	var summary []string
	for _, p := range t.projects {
		summary = append(summary, p.Framework+" · "+p.Name)
	}
	lines := []string{m.testsTitle(stMuted.Render(fmt.Sprintf("%d %s", len(t.projects), plural(len(t.projects), "projeto", "projetos")))), ""}
	if t.discoverErr != nil {
		lines = append(lines, " "+stDel.Render(ansiTruncate(t.discoverErr.Error(), w-2)), "")
	}
	if len(t.targets) == 0 {
		return append(lines,
			"  "+stMuted.Render("Nenhum projeto de testes detectado (go.mod com *_test.go, package.json com vitest/jest/test)."),
			"  "+stMuted.Render("Configure comandos e diretórios em .gitview.json."))
	}
	lines = append(lines, "  "+stMuted.Render(ansiTruncate(strings.Join(summary, "   "), w-3)), "")
	avail := max(m.vp.Height-len(lines), 1)
	start := max(t.tsel-avail+1, 0)
	for i := start; i < len(t.targets) && i-start < avail; i++ {
		lines = append(lines, selectableRow(w, t.targets[i].Label, "", i == t.tsel, sym.closed, stMuted))
	}
	return lines
}

func (m *model) testsResultsView() []string {
	t := m.tests
	s := t.snap
	w, h := m.width, m.vp.Height
	if !t.hasRun() {
		return m.testsTargetsView()
	}
	c := s.Counts()
	var parts []string
	if c.Passed > 0 {
		parts = append(parts, stAdd.Render(fmt.Sprintf("%d %s", c.Passed, plural(c.Passed, "aprovado", "aprovados"))))
	}
	if c.Failed > 0 {
		parts = append(parts, stDel.Render(fmt.Sprintf("%d %s", c.Failed, plural(c.Failed, "falha", "falhas"))))
	}
	if c.Skipped > 0 {
		parts = append(parts, stMuted.Render(fmt.Sprintf("%d %s", c.Skipped, plural(c.Skipped, "ignorado", "ignorados"))))
	}
	icon, st := statusStyle(s.Status)
	parts = append(parts, st.Render(icon+" "+s.Status.String()), stMuted.Render(s.Duration().Round(100*time.Millisecond).String()))
	lines := []string{m.testsTitle(strings.Join(parts, stMuted.Render(" · "))), " " + stSubtle.Render(ansiTruncate(s.Target.Label, w-2))}
	if s.Err != "" {
		lines = append(lines, " "+stMod.Render(ansiTruncate(sym.warn+" "+s.Err, w-2)))
	}
	lines = append(lines, "")

	if len(s.Tests) == 0 {
		switch {
		case s.Status == tr.Running:
			lines = append(lines, "  "+stMuted.Render("Executando… nenhum resultado recebido ainda"))
		case !s.Structured:
			lines = append(lines, "  "+stSubtle.Render(fmt.Sprintf("Processo terminou com código %d. Formato não estruturado: sem contagens individuais.", s.ExitCode)))
		default:
			lines = append(lines, "  "+stMuted.Render("Nenhum teste reportado"))
		}
		lines = append(lines, "")
		tail := max(h-len(lines), 0)
		return append(lines, logTail(s.Log, tail, w)...)
	}

	// Results list plus, for failed tests, the failure detail below it.
	sel := s.Tests[min(t.rsel, len(s.Tests)-1)]
	detail := sel.Status == tr.Failed || sel.Status == tr.InfraError
	listH := h - len(lines)
	if detail && len(sel.Output) > 0 {
		listH = max(min(len(s.Tests), listH/2), 3)
	}
	nameW := 0
	for _, r := range s.Tests {
		nameW = max(nameW, runewidth.StringWidth(r.Name))
	}
	start := max(t.rsel-listH+1, 0)
	for i := start; i < len(s.Tests) && i-start < listH; i++ {
		lines = append(lines, testRow(w, nameW, s.Tests[i], i == t.rsel))
	}
	if detail && len(sel.Output) > 0 {
		lines = append(lines, "", " "+stDel.Render("Falha: ")+stBold.Render(ansiTruncate(sel.Name, w-10)))
		for _, l := range sel.Output {
			if len(lines) >= h {
				break
			}
			lines = append(lines, "   "+stSubtle.Render(ansiTruncate(expandTabs(l), w-4)))
		}
	}
	return lines
}

func testRow(w, nameW int, r tr.TestResult, selected bool) string {
	icon, st := statusStyle(r.Status)
	dur := ""
	if r.Status != tr.Running && r.Elapsed > 0 {
		dur = fmt.Sprintf("%dms", r.Elapsed.Milliseconds())
	}
	pkg := shortPackage(r.Package)
	nameW = min(nameW, w/2)
	name := runewidth.Truncate(r.Name, nameW, sym.ellipsis)
	label := name + strings.Repeat(" ", max(nameW-runewidth.StringWidth(name), 0)) + "  " + stMuted.Render(pkg)
	right := dur
	return selectableRow(w, label, right, selected, icon, st)
}

func shortPackage(p string) string {
	parts := strings.Split(p, "/")
	if len(parts) > 2 {
		return strings.Join(parts[len(parts)-2:], "/")
	}
	return p
}

// selectableRow draws a list row in the same style as the file headers.
func selectableRow(w int, label, right string, selected bool, icon string, iconSt lipgloss.Style) string {
	bg := lipgloss.NewStyle()
	if selected {
		bg = bg.Background(colSelBg)
	}
	with := func(st lipgloss.Style) lipgloss.Style { return bg.Inherit(st) }
	marker := bg.Render(" ")
	if selected {
		marker = with(stAccent).Render(sym.bar)
	}
	nameSt := stText
	if selected {
		nameSt = stBold
	}
	left := marker + with(iconSt).Render(icon) + bg.Render(" ")
	avail := w - lipgloss.Width(left) - runewidth.StringWidth(right) - 2
	body := with(nameSt).Render(ansiTruncate(label, avail))
	gap := max(w-lipgloss.Width(left)-lipgloss.Width(body)-runewidth.StringWidth(right)-1, 1)
	return left + body + bg.Render(strings.Repeat(" ", gap)) + with(stMuted).Render(right) + bg.Render(" ")
}

func logTail(log []string, n, w int) []string {
	var out []string
	for _, l := range log[max(len(log)-n, 0):] {
		out = append(out, "  "+stSubtle.Render(ansiTruncate(expandTabs(l), w-3)))
	}
	return out
}

func (m *model) testsLogView() []string {
	t := m.tests
	log := t.snap.Log
	h := max(m.vp.Height-2, 1)
	maxOff := max(len(log)-h, 0)
	if t.follow || t.logOff > maxOff {
		t.logOff = maxOff
	}
	right := stMuted.Render(fmt.Sprintf("%d linhas", len(log)))
	lines := []string{m.testsTitle(right), ""}
	if len(log) == 0 {
		return append(lines, "  "+stMuted.Render("Sem saída registrada"))
	}
	for _, l := range log[t.logOff:min(t.logOff+h, len(log))] {
		lines = append(lines, "  "+stText.Render(ansiTruncate(expandTabs(l), m.width-3)))
	}
	return lines
}

func (m *model) testsFooter() string {
	t := m.tests
	if t.pending != nil {
		return renderHints(m.width, []keyHint{{"Enter", "Confirmar"}, {"Esc", "Cancelar"}}, 2)
	}
	var hints []keyHint
	switch t.view {
	case viewTargets:
		hints = append(hints, keyHint{sym.updown, "Alvo"}, keyHint{"Enter", "Executar"})
	case viewLogs:
		hints = append(hints, keyHint{sym.updown, "Rolar"}, keyHint{"G", "Seguir"})
	default:
		hints = append(hints, keyHint{"r", "Reexecutar"}, keyHint{"f", "Falhas"}, keyHint{"l", "Logs"}, keyHint{"a", "Alvos"})
	}
	if t.runner.Running() {
		hints = append(hints, keyHint{"c", "Cancelar"})
	}
	hints = append(hints, keyHint{"?", "Ajuda"}, keyHint{"Esc", "Voltar"})
	return renderHints(m.width, hints, 2)
}
