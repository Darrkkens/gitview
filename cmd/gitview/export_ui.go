package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/athos/gitview/internal/export"
	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/gitdiff"
	"github.com/athos/gitview/internal/testrunner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type exportScope int

const (
	scopeAll exportScope = iota
	scopeSelected
	scopeFiltered
)

var scopeLabels = []string{"Todos os arquivos", "Arquivo selecionado", "Arquivos filtrados pela busca"}

const flowExportTimeout = 3 * time.Minute

// reportScopes are the scope names written into exported reports.
var reportScopes = []string{"All files", "Selected file", "Files matching the search"}

const (
	focusFormat = iota
	focusScope
	focusDest
	focusFlow
	focusSensitive // only when the scope has potentially sensitive files
	focusInspect
	focusStudy
)

type exportUI struct {
	dir              string
	focus            int
	format           int
	scope            exportScope
	dest             textinput.Model
	destEdited       bool
	includeSensitive bool
	sensitive        []string // sensitive paths within the current scope
	confirmOverwrite bool
	includeFlow      bool // Code Flow index (HTML and JSON)
	includeInspect   bool // Go Deep Inspector results
	includeStudy     bool // Study Mode (implies Code Flow and the inspector)
	busy             bool
	cancel           context.CancelFunc
	msg              string
	msgErr           bool
}

type exportDoneMsg struct {
	path string
	err  error
}

func newExportUI(dir string) *exportUI {
	ti := textinput.New()
	ti.Prompt = ""
	ti.TextStyle = stText
	ti.CharLimit = 4096
	if dir == "" {
		dir = "."
	}
	return &exportUI{dir: dir, dest: ti, includeFlow: true, includeInspect: true, includeStudy: true}
}

func (e *exportUI) defaultDest() string {
	return filepath.Join(e.dir, "gitview-report"+export.Formats[e.format].Ext())
}

func (m *model) openExport() tea.Cmd {
	e := m.export
	m.screen = screenExport
	e.focus, e.msg, e.confirmOverwrite, e.busy = focusFormat, "", false, false
	if !e.destEdited {
		e.dest.SetValue(e.defaultDest())
	}
	if e.scope == scopeFiltered && !m.search.res.Active() {
		e.scope = scopeAll
	}
	m.refreshSensitive()
	return nil
}

// scopeFiles returns the files an export would contain, honouring the scope
// and the active search.
func (m *model) scopeFiles(scope exportScope) []gitdiff.File {
	var out []gitdiff.File
	for _, f := range m.files {
		switch scope {
		case scopeSelected:
			if f.Path != m.cursor {
				continue
			}
		case scopeFiltered:
			if _, ok := m.search.byPath[f.Path]; !ok {
				continue
			}
		}
		gf := gitdiff.File{Path: f.Path, OldPath: f.OldPath, Status: f.Status, Added: f.Added,
			Deleted: f.Deleted, Binary: f.Binary, Raw: f.Diff}
		if pd := m.parsed[f.Path]; pd != nil {
			gf.Hunks = pd.hunks
		}
		out = append(out, gf)
	}
	return out
}

func (m *model) refreshSensitive() {
	_, m.export.sensitive = export.Redact(m.scopeFiles(m.export.scope), false)
	if len(m.export.sensitive) == 0 {
		m.export.includeSensitive = false
		if m.export.focus == focusSensitive {
			m.export.focus = focusDest
		}
	}
}

// buildReport snapshots everything the export needs, so rendering and
// writing can happen off the UI goroutine without touching model state.
func (m *model) buildReport() export.Report {
	e := m.export
	files, sensitive := export.Redact(m.scopeFiles(e.scope), e.includeSensitive)
	r := export.Report{
		Project:     filepath.Base(m.repo.Root),
		Branch:      m.branch,
		Root:        m.repo.Root,
		Base:        m.head,
		GeneratedAt: time.Now(),
		Scope:       reportScopes[e.scope],
		Files:       files,
		Tests:       testReport(m.tests.runner.Snapshot()),
	}
	if m.search.res.Active() {
		r.Query = m.search.res.Query.Text
	}
	if e.includeSensitive {
		if len(sensitive) > 0 {
			r.Warnings = append(r.Warnings, "Inclui conteúdo potencialmente sensível: "+strings.Join(sensitive, ", "))
		}
	} else {
		r.Omitted = sensitive
	}
	if m.err != nil {
		r.Warnings = append(r.Warnings, "Erro do git na última atualização: "+m.err.Error())
	}
	if m.useDelta {
		r.Warnings = append(r.Warnings, "Exportação usa o diff estruturado (não a saída do Delta).")
	}
	return r
}

func testReport(s testrunner.RunState) *export.TestReport {
	if s.Started.IsZero() {
		return &export.TestReport{Ran: false}
	}
	c := s.Counts()
	tr := &export.TestReport{
		Ran: true, Target: reportTarget(s.Target.Label), Status: s.Status.Key(), Structured: s.Structured,
		Started: s.Started, Finished: s.Finished, ExitCode: s.ExitCode,
		Passed: c.Passed, Failed: c.Failed, Skipped: c.Skipped, Other: c.Running + c.Other, Error: s.Err,
	}
	for _, c := range s.Commands {
		tr.Commands = append(tr.Commands, export.TestCommand{Line: c.Line, Dir: c.Dir})
	}
	const maxLog = 5000
	tr.LogTotal = len(s.Log)
	tr.Log = s.Log[max(len(s.Log)-maxLog, 0):]
	for _, t := range s.Tests {
		kind := "test"
		if strings.HasPrefix(t.Name, "(") {
			kind = "package" // build failure or crash reported for the whole package
		}
		tc := export.TestCase{Kind: kind, Package: t.Package, Name: t.Name, Status: t.Status.Key(), ElapsedMs: t.Elapsed.Milliseconds()}
		if t.Status == testrunner.Failed || t.Status == testrunner.InfraError {
			tc.Output = strings.Join(t.Output, "\n")
		}
		tr.Cases = append(tr.Cases, tc)
	}
	return tr
}

// reportTarget translates the TUI's target labels for exported reports.
func reportTarget(label string) string {
	for _, r := range [][2]string{
		{"Todos os testes", "All tests"},
		{"Arquivo atual · ", "Tests related to "},
		{"Falhas · ", "Failed tests of: "},
	} {
		label = strings.ReplaceAll(label, r[0], r[1])
	}
	return label
}

func (m *model) startExport() tea.Cmd {
	e := m.export
	path := strings.TrimSpace(e.dest.Value())
	if path == "" {
		e.msg, e.msgErr = "Informe o arquivo de destino", true
		return nil
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, path[2:])
		}
	}
	if e.scope == scopeFiltered && !m.search.res.Active() {
		e.msg, e.msgErr = "Nenhuma busca ativa para filtrar", true
		return nil
	}
	report := m.buildReport()
	format := export.Formats[e.format]
	overwrite := e.confirmOverwrite
	e.busy, e.msg = true, "Exportando…"
	withFlow := e.includeFlow && format != export.Markdown
	changed, root, cache := m.changedLinesOf(), m.repo.Root, m.flowCache
	ctx, cancel := context.WithTimeout(context.Background(), flowExportTimeout)
	e.cancel = cancel
	if withFlow {
		e.msg = "Exportando… analisando o código Go (Esc cancela)"
	}
	inspectSnap := m.inspectSnapshot()
	withInspect := e.includeInspect
	withStudy := e.includeStudy
	allFiles := append([]FileChange(nil), m.files...)
	if withStudy {
		e.msg = "Exportando… analisando o código Go para o Study (Esc cancela)"
	}
	return func() tea.Msg {
		defer cancel()
		var progs []*flow.Program // the Go analysis, when the export runs one
		if withStudy {
			// Study builds on Code Flow and the inspector: one analysis for all three.
			var failures []string
			progs, failures = loadRepoPrograms(ctx, root, cache)
			sess := inspectSnap.session(root, progs)
			buildStudyReport(ctx, &report, root, progs, failures, changed, allFiles, sess, inspectSnap.targets(sess, changed))
			if ctx.Err() != nil {
				return exportDoneMsg{path: path, err: errors.New("exportação cancelada")}
			}
			withFlow, withInspect = false, false
		}
		if withFlow {
			report.Flow = buildFlowData(ctx, root, changed, cache)
			if ctx.Err() != nil {
				return exportDoneMsg{path: path, err: errors.New("exportação cancelada")}
			}
		}
		if withInspect {
			report.Inspect = inspectSnap.build(ctx, root, changed, cache)
			if ctx.Err() != nil {
				return exportDoneMsg{path: path, err: errors.New("exportação cancelada")}
			}
		}
		if format == export.HTML && (progs != nil || withFlow || withInspect) {
			// Hovers for the code the page shows; the programs are in the
			// cache when Code Flow or the inspector analysed them.
			if progs == nil {
				progs, _ = loadRepoPrograms(ctx, root, cache)
			}
			report.Hovers = offlineHovers(progs, &report)
		}
		data, err := export.Render(format, report)
		if err == nil {
			err = export.WriteFile(path, data, overwrite)
		}
		return exportDoneMsg{path: path, err: err}
	}
}

func (m *model) exportDone(msg exportDoneMsg) {
	e := m.export
	e.busy = false
	switch {
	case errors.Is(msg.err, export.ErrExists):
		e.confirmOverwrite = true
		e.msg, e.msgErr = "O arquivo já existe. Enter novamente para sobrescrever.", true
	case msg.err != nil:
		e.msg, e.msgErr = "Falha ao exportar: "+msg.err.Error(), true
	default:
		e.confirmOverwrite = false
		m.screen = screenDiff
		const prefix = "Relatório salvo em "
		m.setToast(prefix+truncateLeft(msg.path, max(m.width-len(prefix)-3, 10)), false)
	}
}

func (m *model) exportKey(msg tea.KeyMsg) tea.Cmd {
	e := m.export
	key := msg.String()
	if e.busy {
		if key == "esc" && e.cancel != nil {
			e.cancel()
		}
		return nil
	}
	stops := []int{focusFormat, focusScope, focusDest, focusFlow}
	if len(e.sensitive) > 0 {
		stops = append(stops, focusSensitive)
	}
	stops = append(stops, focusInspect, focusStudy)
	pos := 0
	for k, f := range stops {
		if f == e.focus {
			pos = k
		}
	}
	switch key {
	case "esc":
		m.screen = screenDiff
		e.dest.Blur()
		return nil
	case "enter":
		return m.startExport()
	case "tab":
		e.focus = stops[(pos+1)%len(stops)]
	case "shift+tab":
		e.focus = stops[(pos+len(stops)-1)%len(stops)]
	case "?":
		if e.focus != focusDest {
			m.showHelp = true
			return nil
		}
	}
	if e.focus == focusDest {
		e.dest.Focus()
	} else {
		e.dest.Blur()
	}
	if key == "tab" || key == "shift+tab" {
		return nil
	}

	switch e.focus {
	case focusFormat:
		switch key {
		case "up", "k":
			e.setFormat((e.format + len(export.Formats) - 1) % len(export.Formats))
		case "down", "j":
			e.setFormat((e.format + 1) % len(export.Formats))
		}
	case focusScope:
		next := e.scope
		switch key {
		case "up", "k":
			next = (e.scope + 2) % 3
		case "down", "j":
			next = (e.scope + 1) % 3
		}
		if next == scopeFiltered && !m.search.res.Active() {
			if key == "up" || key == "k" {
				next = scopeSelected
			} else {
				next = scopeAll
			}
		}
		if next != e.scope {
			e.scope = next
			e.confirmOverwrite = false
			m.refreshSensitive()
		}
	case focusDest:
		before := e.dest.Value()
		var cmd tea.Cmd
		e.dest, cmd = e.dest.Update(msg)
		if e.dest.Value() != before {
			e.destEdited, e.confirmOverwrite, e.msg = true, false, ""
		}
		return cmd
	case focusFlow:
		if key == " " || key == "space" {
			e.includeFlow = !e.includeFlow
		}
	case focusInspect:
		if key == " " || key == "space" {
			e.includeInspect = !e.includeInspect
		}
	case focusStudy:
		if key == " " || key == "space" {
			e.includeStudy = !e.includeStudy
		}
	case focusSensitive:
		if key == " " || key == "space" {
			e.includeSensitive = !e.includeSensitive
		}
	}
	return nil
}

func (e *exportUI) setFormat(f int) {
	oldExt := export.Formats[e.format].Ext()
	e.format = f
	e.confirmOverwrite, e.msg = false, ""
	if !e.destEdited {
		e.dest.SetValue(e.defaultDest())
		return
	}
	if v := e.dest.Value(); strings.HasSuffix(v, oldExt) {
		e.dest.SetValue(strings.TrimSuffix(v, oldExt) + export.Formats[f].Ext())
	}
}

func (m *model) exportView() string {
	lines, _ := m.exportLayout()
	return strings.Join(lines, "\n")
}

// exportClick is what clicking a line of the export screen does: focus a
// section and, for options and checkboxes, pick or toggle it.
type exportClick struct {
	focus int
	index int // option index (format, scope); -1 a checkbox (toggles); -2 a section title (focus only)
}

// exportLayout renders the export screen and records which lines are
// clickable (by line index).
func (m *model) exportLayout() ([]string, map[int]exportClick) {
	clicks := map[int]exportClick{}
	e := m.export
	w := m.width
	section := func(title string, focused bool) string {
		st := stSubtle
		if focused {
			st = stAccent
		}
		return " " + st.Bold(true).Render(title)
	}
	option := func(label string, selected, focused, disabled bool) string {
		pointer := "  "
		st := stSubtle
		switch {
		case disabled:
			st = stBorder
		case selected && focused:
			pointer, st = stAccent.Render(sym.closed)+" ", stBold
		case selected:
			pointer, st = stMuted.Render(sym.closed)+" ", stText
		}
		return "  " + pointer + st.Render(label)
	}

	lines := []string{" " + stBold.Render("Exportar alterações"), ""}
	clicks[len(lines)] = exportClick{focusFormat, -2}
	lines = append(lines, section("Formato", e.focus == focusFormat))
	for i, f := range export.Formats {
		clicks[len(lines)] = exportClick{focusFormat, i}
		lines = append(lines, option(f.String(), i == e.format, e.focus == focusFormat, false))
	}
	lines = append(lines, "")
	clicks[len(lines)] = exportClick{focusScope, -2}
	lines = append(lines, section("Escopo", e.focus == focusScope))
	for i, l := range scopeLabels {
		disabled := exportScope(i) == scopeFiltered && !m.search.res.Active()
		label := l
		switch exportScope(i) {
		case scopeAll:
			label += stMuted.Render(fmt.Sprintf("  %d", len(m.files)))
		case scopeSelected:
			if m.cursor != "" {
				label += stMuted.Render("  " + truncateLeft(m.cursor, max(w-30, 10)))
			}
		case scopeFiltered:
			if disabled {
				label += "  (sem busca ativa)"
			} else {
				label += stMuted.Render(fmt.Sprintf("  %d · /%s", len(m.search.res.Files), m.search.res.Query.Text))
			}
		}
		clicks[len(lines)] = exportClick{focusScope, i}
		lines = append(lines, option(label, exportScope(i) == e.scope, e.focus == focusScope, disabled))
	}
	lines = append(lines, "")
	clicks[len(lines)] = exportClick{focusDest, -2}
	clicks[len(lines)+1] = exportClick{focusDest, -2}
	lines = append(lines, section("Destino", e.focus == focusDest))
	dest := e.dest.View()
	if e.focus != focusDest {
		dest = stText.Render(truncateLeft(e.dest.Value(), w-3))
	}
	lines = append(lines, "  "+ansiTruncate(dest, w-3))

	box := "[ ]"
	if e.includeFlow {
		box = "[x]"
	}
	flowSt := stSubtle
	if e.focus == focusFlow {
		flowSt = stAccent
	}
	clicks[len(lines)+1] = exportClick{focusFlow, -2}
	clicks[len(lines)+2] = exportClick{focusFlow, -1}
	lines = append(lines, "", section("Code Flow", e.focus == focusFlow),
		"  "+flowSt.Render(box+" Incluir grafo de chamadas Go  ")+stMuted.Render("HTML e JSON · Espaço alterna"))

	if n := len(e.sensitive); n > 0 {
		box := "[ ]"
		if e.includeSensitive {
			box = "[x]"
		}
		clicks[len(lines)+1] = exportClick{focusSensitive, -2}
		lines = append(lines, "", section("Dados sensíveis", e.focus == focusSensitive))
		lines = append(lines, "  "+stMod.Render(sym.warn)+" "+stSubtle.Render(ansiTruncate(
			fmt.Sprintf("%d %s com possíveis segredos: %s", n, plural(n, "arquivo", "arquivos"), strings.Join(e.sensitive, ", ")), w-6)))
		st := stSubtle
		if e.focus == focusSensitive {
			st = stAccent
		}
		clicks[len(lines)] = exportClick{focusSensitive, -1}
		lines = append(lines, "  "+st.Render(box+" Incluir conteúdo (por padrão é omitido)  ")+stMuted.Render("Espaço alterna"))
	}
	ibox, inspSt := "[ ]", stSubtle
	if e.includeInspect {
		ibox = "[x]"
	}
	if e.focus == focusInspect {
		inspSt = stAccent
	}
	clicks[len(lines)+1] = exportClick{focusInspect, -2}
	clicks[len(lines)+2] = exportClick{focusInspect, -1}
	lines = append(lines, "", section("Go Deep Inspector", e.focus == focusInspect),
		"  "+inspSt.Render(ibox+" Funções modificadas e a inspecionada, com os dados de execução carregados  ")+stMuted.Render("Espaço alterna"))
	sbox, studySt := "[ ]", stSubtle
	if e.includeStudy {
		sbox = "[x]"
	}
	if e.focus == focusStudy {
		studySt = stAccent
	}
	clicks[len(lines)+1] = exportClick{focusStudy, -2}
	clicks[len(lines)+2] = exportClick{focusStudy, -1}
	lines = append(lines, "", section("Study", e.focus == focusStudy),
		"  "+studySt.Render(sbox+" Por onde começar, arquitetura e jornada da alteração (inclui Code Flow e Inspector)  ")+stMuted.Render("Espaço alterna"))
	if e.msg != "" {
		st := stSubtle
		if e.msgErr {
			st = stDel
		}
		lines = append(lines, "", " "+st.Render(ansiTruncate(e.msg, w-2)))
	}
	return lines, clicks
}

func (m *model) exportFooter() string {
	action := "Exportar"
	if m.export.confirmOverwrite {
		action = "Sobrescrever"
	}
	return renderHints(m.width, []keyHint{
		{"Tab", "Seção"}, {sym.updown, "Escolher"}, {"Enter", action}, {"Esc", "Cancelar"},
	}, 2)
}
