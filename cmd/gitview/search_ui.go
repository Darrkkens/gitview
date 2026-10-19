package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/athos/gitview/internal/search"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

const searchDebounce = 120 * time.Millisecond

type searchDebounceMsg struct{ seq int }

type searchUI struct {
	input    textinput.Model
	caseSens bool
	regex    bool
	index    *search.Index
	res      search.Result
	byPath   map[string]search.FileHits
	gen      int // bumped whenever results change, to invalidate render caches
	sel      int // selected file in the results panel
	cur      int // current match (index into res.Matches), -1 when none
	seq      int // debounce token
}

func newSearchUI() *searchUI {
	ti := textinput.New()
	ti.Prompt = "/ "
	ti.Placeholder = "buscar em todos os diffs"
	ti.PromptStyle = stAccent
	ti.TextStyle = stText
	ti.PlaceholderStyle = stMuted
	return &searchUI{input: ti, index: search.NewIndex(), cur: -1}
}

func (s *searchUI) query() search.Query {
	return search.Query{Text: s.input.Value(), CaseSensitive: s.caseSens, Regex: s.regex}
}

func (m *model) openSearch() tea.Cmd {
	m.screen = screenSearch
	m.search.input.Focus()
	m.search.input.CursorEnd()
	m.search.sel = 0
	if fh, ok := m.search.byPath[m.cursor]; ok {
		for i, f := range m.search.res.Files {
			if f.Path == fh.Path {
				m.search.sel = i
			}
		}
	}
	return textinput.Blink
}

func (m *model) closeSearchPanel() {
	m.screen = screenDiff
	m.search.input.Blur()
}

func (m *model) clearSearch() {
	s := m.search
	s.input.SetValue("")
	s.res, s.byPath, s.cur, s.sel = search.Result{}, nil, -1, 0
	s.gen++
	m.rebuild()
}

// searchDocs exposes the already-parsed diffs to the index; the parsed diff
// pointer is the version, so unchanged files are never rescanned.
func (m *model) searchDocs() []search.Doc {
	docs := make([]search.Doc, 0, len(m.files))
	for _, f := range m.files {
		pd := m.parsed[f.Path]
		d := search.Doc{Path: f.Path, Version: pd}
		if pd != nil {
			d.Lines = pd.texts()
		} else {
			d.Version = nil
		}
		docs = append(docs, d)
	}
	return docs
}

// runSearch recomputes results and keeps the current match anchored to the
// same file and position when diffs change underneath it.
func (m *model) runSearch() {
	s := m.search
	var anchorPath string
	anchorLine := 0
	if s.cur >= 0 && s.cur < len(s.res.Matches) {
		anchorPath, anchorLine = s.res.Matches[s.cur].Path, s.res.Matches[s.cur].Line
	}
	hadCurrent := s.cur >= 0

	s.res = s.index.Search(s.query(), m.searchDocs())
	s.byPath = make(map[string]search.FileHits, len(s.res.Files))
	for _, f := range s.res.Files {
		s.byPath[f.Path] = f
	}
	s.cur = -1
	if hadCurrent {
		s.cur = s.res.Nearest(anchorPath, anchorLine)
	}
	s.sel = min(s.sel, max(len(s.res.Files)-1, 0))
	s.gen++
	m.rebuild()
}

func (m *model) searchKey(msg tea.KeyMsg) tea.Cmd {
	s := m.search
	switch msg.String() {
	case "esc":
		m.closeSearchPanel()
		m.clearSearch()
		return nil
	case "enter":
		if s.input.Value() != "" && s.res.Query != s.query() {
			m.runSearch() // flush a pending debounce
		}
		if len(s.res.Files) == 0 {
			return nil
		}
		m.closeSearchPanel()
		m.gotoMatch(s.res.Files[s.sel].First)
		return nil
	case "up", "ctrl+p", "shift+tab":
		s.sel = max(s.sel-1, 0)
		return nil
	case "down", "ctrl+n", "tab":
		s.sel = min(s.sel+1, max(len(s.res.Files)-1, 0))
		return nil
	case "alt+c":
		s.caseSens = !s.caseSens
		m.runSearch()
		return nil
	case "alt+r":
		s.regex = !s.regex
		m.runSearch()
		return nil
	}
	before := s.input.Value()
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	if s.input.Value() == before {
		return cmd
	}
	s.seq++
	seq := s.seq
	return tea.Batch(cmd, tea.Tick(searchDebounce, func(time.Time) tea.Msg { return searchDebounceMsg{seq} }))
}

// stepMatch moves to the next/previous match across all files.
func (m *model) stepMatch(dir int) {
	n := len(m.search.res.Matches)
	if n == 0 {
		return
	}
	i := m.search.cur
	if i < 0 {
		i = 0
		if dir < 0 {
			i = n - 1
		}
	} else {
		i = ((i+dir)%n + n) % n
	}
	m.gotoMatch(i)
}

// gotoMatch selects the match, expands only its file and scrolls so the hit
// is visible both vertically and horizontally.
func (m *model) gotoMatch(i int) {
	s := m.search
	if i < 0 || i >= len(s.res.Matches) {
		return
	}
	s.cur = i
	s.gen++
	mt := s.res.Matches[i]
	m.cursor = mt.Path
	if mt.Line != search.PathLine {
		m.expanded[mt.Path] = true
		if pd := m.parsed[mt.Path]; pd != nil && mt.Line < len(pd.lines) {
			text := pd.lines[mt.Line].text
			col := runewidth.StringWidth(text[:mt.Start])
			end := col + runewidth.StringWidth(text[mt.Start:mt.End])
			codeW := max(m.width-gutterWidth(pd.numW)-1, 8) - 2
			if col < m.hoff || end > m.hoff+codeW {
				m.hoff = max(col-codeW/3, 0)
			}
		}
	}
	m.rebuild()
	vi := m.cursorIndex()
	if vi >= len(m.headers) {
		return
	}
	row := m.headers[vi]
	if mt.Line != search.PathLine {
		if e := m.renderCache[mt.Path]; mt.Line < len(e.rows) && e.rows[mt.Line] >= 0 {
			row += 1 + e.rows[mt.Line]
		}
	}
	if row < m.vp.YOffset || row >= m.vp.YOffset+m.vp.Height {
		m.vp.SetYOffset(max(row-m.vp.Height/3, 0))
	}
}

// fileHits returns the hit ranges of a file and a signature that changes
// whenever they (or the current match inside the file) change.
func (m *model) fileHits(path string) (map[int][]hitRange, int) {
	s := m.search
	fh, ok := s.byPath[path]
	if !ok || !s.res.Active() {
		return nil, 0
	}
	hits := map[int][]hitRange{}
	curIn := 0
	for i := fh.First; i < fh.First+fh.Count; i++ {
		mt := s.res.Matches[i]
		if mt.Line == search.PathLine {
			continue
		}
		mk := markHit
		if i == s.cur {
			mk, curIn = markCurrent, i+1
		}
		hits[mt.Line] = append(hits[mt.Line], hitRange{mt.Start, mt.End, mk})
	}
	return hits, s.gen*1_000_003 + curIn + 1
}

func (m *model) searchView() string {
	s := m.search
	w := m.width
	flag := func(on bool, label string) string {
		if on {
			return stAccent.Render(label)
		}
		return stBorder.Render(label)
	}
	flags := flag(s.caseSens, "Aa") + " " + flag(s.regex, ".*")
	summary := ""
	switch {
	case s.input.Value() == "":
	case s.res.Err != nil:
		summary = stDel.Render(ansiTruncate(s.res.Err.Error(), w/2))
	case s.res.Query != s.query():
		summary = stMuted.Render("…")
	default:
		summary = stSubtle.Render(s.res.Summary())
	}
	right := summary + "   " + flags + " "
	input := " " + s.input.View()
	gap := max(w-lipgloss.Width(input)-lipgloss.Width(right), 1)
	lines := []string{ansiTruncate(input, w-lipgloss.Width(right)-1) + strings.Repeat(" ", gap) + right, ""}

	avail := max(m.vp.Height-len(lines), 1)
	start := 0
	if s.sel >= avail {
		start = s.sel - avail + 1
	}
	for i := start; i < len(s.res.Files) && i-start < avail; i++ {
		f := s.res.Files[i]
		lines = append(lines, m.searchRow(f, i == s.sel))
	}
	if s.input.Value() != "" && s.res.Err == nil && len(s.res.Files) == 0 && s.res.Query == s.query() {
		lines = append(lines, "  "+stMuted.Render("Nenhuma ocorrência nos diffs carregados"))
	}
	return strings.Join(lines, "\n")
}

func (m *model) searchRow(f search.FileHits, selected bool) string {
	bg := lipgloss.NewStyle()
	if selected {
		bg = bg.Background(colSelBg)
	}
	with := func(st lipgloss.Style) lipgloss.Style { return bg.Inherit(st) }
	marker := bg.Render(" ")
	if selected {
		marker = with(stAccent).Render(sym.bar)
	}
	arrow := sym.closed
	if m.expanded[f.Path] {
		arrow = sym.open
	}
	count := fmt.Sprintf("%d %s", f.Count, plural(f.Count, "ocorrência", "ocorrências"))
	if f.PathMatch {
		count = "nome · " + count
	}
	left := marker + with(stMuted).Render(arrow+" ")
	pathW := m.width - lipgloss.Width(left) - runewidth.StringWidth(count) - 3
	name, dir := splitPath(f.Path)
	path := name
	if dir != "" {
		path = dir + "/" + name
	}
	path = truncateLeft(path, pathW)
	nameSt := stText
	if selected {
		nameSt = stBold
	}
	body := with(nameSt).Render(path)
	gap := max(m.width-lipgloss.Width(left)-runewidth.StringWidth(path)-runewidth.StringWidth(count)-1, 1)
	return left + body + bg.Render(strings.Repeat(" ", gap)) + with(stSubtle).Render(count) + bg.Render(" ")
}

func (m *model) searchFooter() string {
	return renderHints(m.width, []keyHint{
		{sym.updown, "navegar"}, {"Enter", "abrir"}, {"Alt+C", "maiúsc."}, {"Alt+R", "regex"}, {"Esc", "fechar"},
	}, 1)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
