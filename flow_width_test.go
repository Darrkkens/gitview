package main

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestFlowRowsFitTerminalWidth(t *testing.T) {
	m := flowModel(t)
	runCmd(m, m.press("f"))
	for _, r := range "client.(*ClientService).GetClientByID" {
		m.press(string(r))
	}
	m.press("enter")
	for i, r := range m.flow.rows {
		s := m.flowRowView(r, i == m.flow.rsel, m.width)
		if r.header == "" && lipgloss.Width(s) != m.width {
			t.Errorf("row %d width %d != %d: %q", i, lipgloss.Width(s), m.width, ansi.Strip(s))
		}
	}
}
