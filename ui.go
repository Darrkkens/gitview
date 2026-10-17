package main

import (
	"strings"
	"time"

	"github.com/athos/gitview/internal/export"
	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/testrunner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"
)

const (
	changedHighlight = 4 * time.Second
	hScrollStep      = 8
	toastDuration    = 5 * time.Second
)

type screen int

const (
	screenDiff screen = iota
	screenSearch
	screenExport
	screenTests
	screenFlow
	screenRuntime
)

type (
	refreshMsg struct {
		files  []FileChange
		parsed map[string]*parsedDiff
		branch string
		head   *export.Commit
		err    error
	}
	fsEventMsg struct{}
	tickMsg    time.Time
)

type renderEntry struct {
	pd          *parsedDiff
	width, hoff int
	delta       bool
	searchSig   int
	lines       []string
	rows        []int
}

type options struct {
	interval    time.Duration
	useDelta    bool
	expandAll   bool
	exportDir   string
	testTimeout time.Duration
	testConfig  []testrunner.ProjectConfig
}

type model struct {
	repo     *Repo
	events   <-chan struct{}
	interval time.Duration

	files     []FileChange
	parsed    map[string]*parsedDiff
	visible   []int
	expanded  map[string]bool
	changedAt map[string]time.Time
	cursor    string
	cursorIdx int
	headers   []int // content line of each visible file header
	branch    string
	head      *export.Commit
	err       error

	loading, pending, loaded bool
	highlighting             bool
	expandNew                bool
	hasRecent                bool

	vp       viewport.Model
	screen   screen
	showHelp bool
	hoff     int

	search *searchUI
	export *exportUI
	tests  *testsUI

	toast      string
	toastErr   bool
	toastUntil time.Time

	flowCache   *flow.Cache
	flow        *flowUI
	insp        *inspUI
	rt          *runtimeUI
	parseCache  *parseCache
	renderCache map[string]renderEntry
	delta       *deltaRenderer
	useDelta    bool
	hasDelta    bool

	width, height int
}

func newModel(repo *Repo, events <-chan struct{}, opt options) *model {
	hasDelta := deltaAvailable()
	return &model{
		repo:        repo,
		events:      events,
		interval:    opt.interval,
		expanded:    map[string]bool{},
		changedAt:   map[string]time.Time{},
		parsed:      map[string]*parsedDiff{},
		expandNew:   opt.expandAll,
		search:      newSearchUI(),
		export:      newExportUI(opt.exportDir),
		tests:       newTestsUI(testrunner.NewRunner(opt.testTimeout), opt.testConfig),
		parseCache:  newParseCache(),
		flowCache:   flow.NewCache(),
		flow:        newFlowUI(),
		insp:        newInspUI(),
		rt:          newRuntimeUI(),
		renderCache: map[string]renderEntry{},
		delta:       newDeltaRenderer(),
		hasDelta:    hasDelta,
		useDelta:    opt.useDelta && hasDelta,
		loading:     true,
		vp:          viewport.New(0, 0),
	}
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.refresh(), m.waitEvent(), m.tick(), m.waitTests())
}

// refresh scans git and parses/highlights diffs off the UI goroutine.
func (m *model) refresh() tea.Cmd {
	repo, cache := m.repo, m.parseCache
	return func() tea.Msg {
		files, err := repo.Changes()
		var parsed map[string]*parsedDiff
		if err == nil {
			parsed = cache.update(files)
		}
		msg := refreshMsg{files: files, parsed: parsed, branch: repo.Branch(), err: err}
		if c := repo.HeadCommit(); c != nil {
			msg.head = &export.Commit{Hash: c.Hash, Short: c.Short, Subject: c.Subject, Author: c.Author, Date: c.Date}
		}
		return msg
	}
}

func (m *model) waitEvent() tea.Cmd {
	if m.events == nil {
		return nil
	}
	return func() tea.Msg {
		<-m.events
		return fsEventMsg{}
	}
}

func (m *model) tick() tea.Cmd {
	return tea.Tick(m.interval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// highlightMsg carries diffs re-parsed with syntax highlighting.
type highlightMsg struct{ parsed map[string]*parsedDiff }

// highlight colours, in the background, the diffs shown as plain text.
func (m *model) highlight() tea.Cmd {
	if m.highlighting {
		return nil
	}
	m.highlighting = true
	cache := m.parseCache
	return func() tea.Msg { return highlightMsg{cache.highlightPending()} }
}

// requestRefresh coalesces refreshes so at most one git scan runs at a time.
func (m *model) requestRefresh() tea.Cmd {
	if m.loading {
		m.pending = true
		return nil
	}
	m.loading = true
	return m.refresh()
}

func (m *model) setToast(msg string, isErr bool) {
	m.toast, m.toastErr, m.toastUntil = msg, isErr, time.Now().Add(toastDuration)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if cmd, ok := m.rtUpdate(msg); ok {
		return m, cmd
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.vp.Width = msg.Width
		m.vp.Height = max(msg.Height-3, 1)
		m.export.dest.Width = max(msg.Width-8, 8)
		m.rebuild()
		m.scrollTo(m.cursorIndex(), false)

	case refreshMsg:
		m.loading = false
		m.err = msg.err
		var cmd tea.Cmd
		if msg.err == nil {
			cmd = tea.Batch(m.applyRefresh(msg), m.highlight())
		}
		if m.pending {
			m.pending = false
			return m, tea.Batch(cmd, m.requestRefresh())
		}
		return m, cmd

	case highlightMsg:
		m.highlighting = false
		changed := false
		for path, pd := range msg.parsed {
			if cur, ok := m.parsed[path]; ok && cur != pd {
				m.parsed[path] = pd
				changed = true
			}
		}
		if changed {
			if !m.search.query().Empty() {
				m.runSearch()
			}
			m.rebuild()
		}
		if m.parseCache.hasPending() {
			// A refresh brought new diffs while these were being highlighted.
			return m, m.highlight()
		}
		return m, nil

	case fsEventMsg:
		return m, tea.Batch(m.requestRefresh(), m.waitEvent())

	case tickMsg:
		if m.hasRecent {
			m.rebuild() // fade "recently changed" markers
		}
		if m.toast != "" && time.Now().After(m.toastUntil) {
			m.toast = ""
		}
		return m, tea.Batch(m.requestRefresh(), m.tick())

	case searchDebounceMsg:
		if msg.seq == m.search.seq {
			m.runSearch()
		}

	case exportDoneMsg:
		m.exportDone(msg)

	case testsDiscoveredMsg:
		m.testsDiscovered(msg)

	case flowLoadedMsg:
		m.flowLoaded(msg)
		if m.insp.sess != nil && msg.err == nil {
			m.insp.sess.SetPrograms(msg.progs)
		}
		if m.screen == screenFlow && m.flow.phase == phaseGraph {
			return m, m.inspRefresh(true)
		}

	case inspDocMsg:
		m.inspDocDone(msg)

	case inspRunMsg:
		return m, m.inspRunDone(msg)

	case testUpdateMsg:
		m.tests.snap = m.tests.runner.Snapshot()
		m.tests.clampSelection()
		return m, m.waitTests()

	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if cmd, ok := m.click(msg); ok {
				return m, cmd
			}
		}
		if m.screen == screenDiff && !m.showHelp {
			var cmd tea.Cmd
			m.vp, cmd = m.vp.Update(msg)
			return m, cmd
		}

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.tests.runner.Cancel()
			m.inspStop()
			m.rtStopAll()
			return m, tea.Quit
		}
		if m.showHelp {
			if k := msg.String(); k == "?" || k == "esc" || k == "q" {
				m.showHelp = false
			}
			return m, nil
		}
		switch m.screen {
		case screenSearch:
			return m, m.searchKey(msg)
		case screenExport:
			return m, m.exportKey(msg)
		case screenTests:
			return m, m.testsKey(msg)
		case screenFlow:
			return m, m.flowKey(msg)
		case screenRuntime:
			return m, m.runtimeKey(msg)
		}
		return m, m.handleKey(msg)
	}
	return m, nil
}

// applyRefresh swaps in new git state while keeping selection and the scroll
// position anchored to the selected file, even if files above it grew or shrank.
func (m *model) applyRefresh(msg refreshMsg) tea.Cmd {
	oldHeader := -1
	if i := m.cursorIndex(); i < len(m.headers) {
		oldHeader = m.headers[i]
	}

	old := make(map[string]string, len(m.files))
	for _, f := range m.files {
		old[f.Path] = f.Diff
	}
	now := time.Now()
	changed := !m.loaded || len(old) != len(msg.files) || m.branch != msg.branch
	for _, f := range msg.files {
		prev, seen := old[f.Path]
		if prev != f.Diff || !seen {
			changed = true
			if m.loaded {
				m.changedAt[f.Path] = now
			}
		}
		if !seen && m.expandNew {
			m.expanded[f.Path] = true
		}
	}
	firstLoad := !m.loaded
	m.files, m.parsed, m.branch, m.head = msg.files, msg.parsed, msg.branch, msg.head
	m.loaded = true
	if !changed {
		return nil // nothing to redraw: avoid needless re-renders
	}

	if !m.search.query().Empty() {
		m.runSearch() // keep results in sync with the new diffs
	}
	m.rebuild()
	if i := m.cursorIndex(); oldHeader >= 0 && i < len(m.headers) {
		m.vp.SetYOffset(m.vp.YOffset + m.headers[i] - oldHeader)
	}
	if !firstLoad {
		return m.testsOnChange()
	}
	return nil
}

func (m *model) handleKey(msg tea.KeyMsg) tea.Cmd {
	idx := m.cursorIndex()
	switch msg.String() {
	case "q":
		m.tests.runner.Cancel()
		m.inspStop()
		m.rtStopAll()
		return tea.Quit
	case "?":
		m.showHelp = true
	case "up", "k", "shift+tab":
		m.moveCursor(idx - 1)
	case "down", "j", "tab":
		m.moveCursor(idx + 1)
	case "g", "home":
		m.moveCursor(0)
		m.vp.GotoTop()
	case "G", "end":
		m.moveCursor(len(m.visible) - 1)
	case "enter", " ":
		if f := m.current(); f != nil {
			m.expanded[f.Path] = !m.expanded[f.Path]
			m.rebuild()
			m.scrollTo(idx, m.expanded[f.Path])
		}
	case "e":
		m.setAll(true)
	case "c":
		m.setAll(false)
	case "left", "h":
		m.hoff = max(m.hoff-hScrollStep, 0)
		m.rebuild()
	case "right", "l":
		m.hoff += hScrollStep
		m.rebuild()
	case "0":
		m.hoff = 0
		m.rebuild()
	case "ctrl+d", "pgdown":
		m.vp.SetYOffset(m.vp.YOffset + m.vp.Height/2)
	case "ctrl+u", "pgup":
		m.vp.SetYOffset(m.vp.YOffset - m.vp.Height/2)
	case "J", "ctrl+e":
		m.vp.SetYOffset(m.vp.YOffset + 1)
	case "K", "ctrl+y":
		m.vp.SetYOffset(m.vp.YOffset - 1)
	case "d":
		if m.hasDelta {
			m.useDelta = !m.useDelta
			m.rebuild()
		}
	case "/":
		return m.openSearch()
	case "n":
		m.stepMatch(1)
	case "N":
		m.stepMatch(-1)
	case "esc":
		if !m.search.query().Empty() {
			m.clearSearch()
		}
	case "x":
		return m.openExport()
	case "t":
		return m.openTests()
	case "f":
		return m.openFlow()
	case "i":
		return m.openInspect()
	case "m":
		return m.openRuntime()
	case "r":
		return m.requestRefresh()
	}
	return nil
}

func (m *model) setAll(open bool) {
	for _, i := range m.visible {
		m.expanded[m.files[i].Path] = open
	}
	m.rebuild()
	m.scrollTo(m.cursorIndex(), open)
}

func (m *model) cursorIndex() int {
	for vi, i := range m.visible {
		if m.files[i].Path == m.cursor {
			return vi
		}
	}
	return 0
}

func (m *model) current() *FileChange {
	if len(m.visible) == 0 {
		return nil
	}
	return &m.files[m.visible[m.cursorIndex()]]
}

func (m *model) moveCursor(vi int) {
	if len(m.visible) == 0 {
		return
	}
	vi = min(max(vi, 0), len(m.visible)-1)
	m.cursor = m.files[m.visible[vi]].Path
	m.rebuild()
	m.scrollTo(vi, false)
}

// scrollTo keeps the header of visible file vi on screen. With top=true the
// header is pinned to the top so an expanded diff gets the full viewport.
func (m *model) scrollTo(vi int, top bool) {
	if vi < 0 || vi >= len(m.headers) {
		return
	}
	h := m.headers[vi]
	switch {
	case h < m.vp.YOffset:
		m.vp.SetYOffset(h)
	case top:
		if end := m.blockEnd(vi); end >= m.vp.YOffset+m.vp.Height {
			m.vp.SetYOffset(h)
		}
	case h >= m.vp.YOffset+m.vp.Height:
		if m.expanded[m.files[m.visible[vi]].Path] {
			m.vp.SetYOffset(h)
		} else {
			m.vp.SetYOffset(h - m.vp.Height + 1)
		}
	}
}

// blockEnd is the last content line belonging to visible file vi.
func (m *model) blockEnd(vi int) int {
	if vi+1 < len(m.headers) {
		return m.headers[vi+1] - 1
	}
	return m.vp.TotalLineCount() - 1
}

func (m *model) rebuild() {
	if m.width == 0 {
		return
	}
	m.visible = m.visible[:0]
	nameCol := 0
	for i, f := range m.files {
		m.visible = append(m.visible, i)
		name, _ := splitPath(f.Path)
		nameCol = max(nameCol, runewidth.StringWidth(name))
	}
	nameCol = min(nameCol, m.width*2/5)

	// Keep the selection; if its file vanished, stay at the same position.
	if len(m.visible) > 0 {
		found := false
		for vi, i := range m.visible {
			if m.files[i].Path == m.cursor {
				m.cursorIdx, found = vi, true
			}
		}
		if !found {
			m.cursorIdx = min(m.cursorIdx, len(m.visible)-1)
			m.cursor = m.files[m.visible[m.cursorIdx]].Path
		}
	}

	var lines []string
	m.headers = m.headers[:0]
	now := time.Now()
	m.hasRecent = false
	prevExpanded := false
	for vi, i := range m.visible {
		f := m.files[i]
		open := m.expanded[f.Path]
		if vi > 0 && (open || prevExpanded) {
			lines = append(lines, "")
		}
		m.headers = append(m.headers, len(lines))
		recent := now.Sub(m.changedAt[f.Path]) < changedHighlight
		m.hasRecent = m.hasRecent || recent
		lines = append(lines, renderFileHeader(f, m.width, nameCol, f.Path == m.cursor, open, recent))
		if open {
			lines = append(lines, m.diffBlock(f).lines...)
		}
		prevExpanded = open
	}
	if len(m.visible) == 0 {
		msg := "Nenhuma alteração  ·  aguardando modificações"
		if !m.loaded {
			msg = "Carregando…"
		}
		lines = append(lines, "  "+stMuted.Render(msg))
	}
	m.vp.SetContent(strings.Join(lines, "\n"))
}

// diffBlock renders (or reuses) the diff lines of an expanded file.
func (m *model) diffBlock(f FileChange) renderEntry {
	pd := m.parsed[f.Path]
	if pd == nil {
		return renderEntry{}
	}
	hits, sig := m.fileHits(f.Path)
	if c, ok := m.renderCache[f.Path]; ok && c.pd == pd && c.width == m.width && c.hoff == m.hoff &&
		c.delta == m.useDelta && c.searchSig == sig {
		return c
	}
	e := renderEntry{pd: pd, width: m.width, hoff: m.hoff, delta: m.useDelta, searchSig: sig}
	if m.useDelta {
		e.lines = m.delta.render(f.Path, f.Diff, m.width)
	}
	if e.lines == nil {
		e.lines, e.rows = renderDiff(pd, m.width, m.hoff, hits)
	}
	m.renderCache[f.Path] = e
	return e
}

func (m *model) View() string {
	if m.width == 0 {
		return ""
	}
	top := renderTopBar(m.width, m.branch, m.files, m.topExtra(), m.events != nil, m.err != nil)
	var body string
	switch {
	case m.showHelp:
		body = renderHelp(m.width, m.vp.Height, m.helpRows())
	case m.screen == screenSearch:
		body = m.searchView()
	case m.screen == screenExport:
		body = m.exportView()
	case m.screen == screenTests:
		body = m.testsView()
	case m.screen == screenFlow:
		body = m.flowView()
	case m.screen == screenRuntime:
		body = m.runtimeView()
	default:
		body = m.vp.View()
	}
	return top + "\n\n" + fitBody(body, m.width, m.vp.Height) + "\n" + m.footerView()
}

// topExtra shows compact search and test indicators in the top bar.
func (m *model) topExtra() string {
	var parts []string
	if r := m.search.res; r.Active() {
		parts = append(parts, stAccent.Render("/")+stSubtle.Render(truncateRight(r.Query.Text, 12))+" "+stMuted.Render(itoa(len(r.Matches))))
	}
	if s := m.tests.indicator(); s != "" {
		parts = append(parts, s)
	}
	if s := m.rtIndicator(); s != "" {
		parts = append(parts, s)
	}
	return strings.Join(parts, "   ")
}

func (m *model) footerView() string {
	if m.toast != "" {
		st := stAdd
		if m.toastErr {
			st = stDel
		}
		return " " + st.Render(ansiTruncate(m.toast, m.width-2))
	}
	if m.showHelp {
		return renderHints(m.width, []keyHint{{"?", "Fechar"}, {"Esc", "Voltar"}}, 2)
	}
	switch m.screen {
	case screenSearch:
		return m.searchFooter()
	case screenExport:
		return m.exportFooter()
	case screenTests:
		return m.testsFooter()
	case screenFlow:
		return m.flowFooter()
	case screenRuntime:
		return m.runtimeFooter()
	}
	if m.err != nil {
		return " " + stDel.Render(ansiTruncate(m.err.Error(), m.width-2))
	}
	var hints []keyHint
	if r := m.search.res; r.Active() && len(r.Matches) > 0 {
		pos := "–"
		if m.search.cur >= 0 {
			pos = itoa(m.search.cur + 1)
		}
		hints = append(hints, keyHint{"n/N", pos + "/" + itoa(len(r.Matches))}, keyHint{"Esc", "limpar"})
	}
	hints = append(hints,
		keyHint{sym.updown, "Navegar"},
		keyHint{"Enter", "Abrir"},
		keyHint{"/", "Buscar"},
		keyHint{"t", "Testes"},
		keyHint{"f", "Flow"},
		keyHint{"i", "Inspector"},
		keyHint{"m", "Runtime"},
		keyHint{"x", "Exportar"},
	)
	if m.hoff > 0 {
		hints = append(hints, keyHint{"0", "col 1"})
	}
	hints = append(hints, keyHint{"?", "Ajuda"}, keyHint{"q", "Sair"})
	return renderHints(m.width, hints, 2)
}
