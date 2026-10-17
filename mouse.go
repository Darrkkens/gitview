package main

import (
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

// Mouse clicks: the key hints of the footer act like their key on every
// screen, and the options of the export screen can be picked with the
// mouse. The wheel keeps scrolling the diff.

// bodyTop is the screen row where the body starts (after the top bar and
// a blank line).
func (m *model) bodyTop() int {
	top := renderTopBar(m.width, m.branch, m.files, m.topExtra(), m.events != nil, m.err != nil)
	return strings.Count(top, "\n") + 2
}

func (m *model) footerRow() int { return m.bodyTop() + m.vp.Height }

// click handles a left click; ok is false when nothing is under it.
func (m *model) click(msg tea.MouseMsg) (tea.Cmd, bool) {
	if msg.Y == m.footerRow() {
		if key, ok := hintAt(m.footerView(), msg.X); ok {
			_, cmd := m.Update(key)
			return cmd, true
		}
		return nil, false
	}
	if m.screen == screenExport && !m.showHelp {
		return m.exportClick(msg.Y - m.bodyTop())
	}
	return nil, false
}

// hintAt returns the key of the footer hint at column x. Hints are
// rendered as "key desc" separated by three spaces.
func hintAt(footer string, x int) (tea.KeyMsg, bool) {
	plain := ansi.Strip(footer)
	col := 0
	for _, seg := range strings.Split(plain, "   ") {
		w := runewidth.StringWidth(seg)
		if x >= col && x < col+w {
			return hintKey(strings.TrimSpace(seg))
		}
		col += w + 3
	}
	return tea.KeyMsg{}, false
}

// hintKey turns a hint ("x Exportar", "Enter Abrir", "n/N 1/3") into the
// key it describes. Ranges and arrows are not clickable.
func hintKey(hint string) (tea.KeyMsg, bool) {
	label, _, _ := strings.Cut(hint, " ")
	switch label {
	case "Enter":
		return tea.KeyMsg{Type: tea.KeyEnter}, true
	case "Esc":
		return tea.KeyMsg{Type: tea.KeyEsc}, true
	case "Tab":
		return tea.KeyMsg{Type: tea.KeyTab}, true
	case "Espaço":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}, true
	}
	if first, _, ok := strings.Cut(label, "/"); ok && label != "/" {
		label = first // n/N, x/X, t/T: the first key
	}
	if utf8.RuneCountInString(label) != 1 || strings.ContainsAny(label, "↑↓←→") {
		return tea.KeyMsg{}, false
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(label)}, true
}

// exportClick focuses the clicked section of the export screen and picks
// the option or toggles the checkbox on that line.
func (m *model) exportClick(line int) (tea.Cmd, bool) {
	e := m.export
	if e.busy {
		return nil, false
	}
	_, clicks := m.exportLayout()
	c, ok := clicks[line]
	if !ok {
		return nil, false
	}
	e.focus = c.focus
	if c.focus == focusDest {
		e.dest.Focus()
	} else {
		e.dest.Blur()
	}
	switch c.focus {
	case focusFormat:
		if c.index >= 0 && c.index != e.format {
			e.setFormat(c.index)
		}
	case focusScope:
		if c.index >= 0 && exportScope(c.index) != e.scope && (exportScope(c.index) != scopeFiltered || m.search.res.Active()) {
			e.scope = exportScope(c.index)
			e.confirmOverwrite = false
			m.refreshSensitive()
		}
	}
	if c.index == -1 { // checkbox line
		switch c.focus {
		case focusFlow:
			e.includeFlow = !e.includeFlow
		case focusInspect:
			e.includeInspect = !e.includeInspect
		case focusStudy:
			e.includeStudy = !e.includeStudy
		case focusSensitive:
			e.includeSensitive = !e.includeSensitive
		}
	}
	return nil, true
}
