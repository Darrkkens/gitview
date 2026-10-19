package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

// clickText clicks the first cell of the first screen line containing text.
func clickText(t *testing.T, m *model, text string, last bool) {
	t.Helper()
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	order := make([]int, len(lines))
	for i := range lines {
		order[i] = i
		if last {
			order[i] = len(lines) - 1 - i
		}
	}
	for _, y := range order {
		if i := strings.Index(lines[y], text); i >= 0 {
			m.Update(tea.MouseMsg{X: runewidth.StringWidth(lines[y][:i]), Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
			return
		}
	}
	t.Fatalf("%q not on screen:\n%s", text, strings.Join(lines, "\n"))
}

func TestMouseClicks(t *testing.T) {
	m := newTestModel(t, sampleFiles()...)
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	clickText(t, m, "x Exportar", true) // footer hint
	if m.screen != screenExport {
		t.Fatal("clicking the x hint did not open the export screen")
	}
	clickText(t, m, "HTML", false)
	if m.export.format != 1 || m.export.focus != focusFormat || !strings.HasSuffix(m.export.dest.Value(), ".html") {
		t.Errorf("format %d focus %d dest %q", m.export.format, m.export.focus, m.export.dest.Value())
	}
	clickText(t, m, " Study", false) // section title: focus only
	if m.export.focus != focusStudy || !m.export.includeStudy {
		t.Errorf("title click: focus %d study %v", m.export.focus, m.export.includeStudy)
	}
	clickText(t, m, "[x] Por onde começar", false)
	if m.export.includeStudy {
		t.Error("clicking the Study checkbox did not toggle it")
	}
	clickText(t, m, "[x] Incluir grafo", false)
	if m.export.includeFlow || m.export.focus != focusFlow {
		t.Error("clicking the Code Flow checkbox did not toggle it")
	}
	clickText(t, m, "Esc Cancelar", true)
	if m.screen != screenDiff {
		t.Fatal("clicking Esc did not close the export screen")
	}
	clickText(t, m, "? Ajuda", true)
	if !m.showHelp {
		t.Fatal("clicking ? did not open the help")
	}
	clickText(t, m, "Esc Voltar", true)
	if m.showHelp {
		t.Fatal("clicking Esc did not close the help")
	}
	// The wheel still scrolls the diff, and clicks elsewhere do nothing.
	m.Update(tea.MouseMsg{X: 5, Y: 5, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if m.screen != screenDiff {
		t.Error("a click in the diff changed screens")
	}
}

func TestHintKey(t *testing.T) {
	for hint, want := range map[string]string{"x Exportar": "x", "Enter Abrir": "enter", "Esc Voltar": "esc", "n/N 1/3": "n", "/ Buscar": "/", "Espaço Pausar": " ", "Tab Seção": "tab", "+/- ←→ Zoom": "+"} {
		k, ok := hintKey(hint)
		if !ok || k.String() != want {
			t.Errorf("hintKey(%q) = %q, %v", hint, k.String(), ok)
		}
	}
	for _, hint := range []string{"↑↓ Navegar", "1-6 Abas", "Relatório salvo em x"} {
		if _, ok := hintKey(hint); ok {
			t.Errorf("hintKey(%q) should not be clickable", hint)
		}
	}
}
