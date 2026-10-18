package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestTruncateLeftKeepsEnd(t *testing.T) {
	if got := truncateLeft("backend/internal/telecom", 10); got != "…l/telecom" {
		t.Errorf("got %q", got)
	}
}

func TestFileHeaderFitsAndKeepsName(t *testing.T) {
	f := FileChange{Path: "backend/internal/very/long/directory/structure/telecom_service.go", Status: "M", Added: 12, Deleted: 3}
	for _, w := range []int{30, 50, 80, 140} {
		h := renderFileHeader(f, w, 20, true, false, true)
		plain := ansi.Strip(h)
		if lipgloss.Width(h) > w {
			t.Errorf("width %d: header is %d wide: %q", w, lipgloss.Width(h), plain)
		}
		if !strings.Contains(plain, "+12 -3") {
			t.Errorf("width %d: stats missing: %q", w, plain)
		}
		if w >= 50 && !strings.Contains(plain, "telecom_service.go") {
			t.Errorf("width %d: name truncated: %q", w, plain)
		}
	}
}

func TestHintsKeepEssentials(t *testing.T) {
	hints := []keyHint{{"↑↓", "Navegar"}, {"Enter", "Abrir"}, {"/", "Buscar"}, {"?", "Ajuda"}, {"q", "Sair"}}
	for _, w := range []int{20, 40, 120} {
		s := ansi.Strip(renderHints(w, hints, 2))
		if lipgloss.Width(s) > w {
			t.Errorf("width %d overflow: %q", w, s)
		}
		if w >= 20 && (!strings.Contains(s, "Ajuda") || !strings.Contains(s, "Sair")) {
			t.Errorf("width %d: essentials missing: %q", w, s)
		}
	}
}

func TestTopBarFits(t *testing.T) {
	files := []FileChange{{Path: "a", Added: 3}, {Path: "b", Deleted: 1}}
	for _, w := range []int{15, 40, 100} {
		if got := renderTopBar(w, "feature/very-long-branch-name", files, "", true, false); lipgloss.Width(got) > w {
			t.Errorf("width %d overflow: %q", w, ansi.Strip(got))
		}
	}
}
