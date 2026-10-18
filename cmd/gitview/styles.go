package main

import "github.com/charmbracelet/lipgloss"

// Palette inspired by Catppuccin Mocha. The terminal's own background is kept.
var (
	colText   = lipgloss.Color("#CDD6F4")
	colSubtle = lipgloss.Color("#9399B2")
	colMuted  = lipgloss.Color("#6C7086")
	colSelBg  = lipgloss.Color("#313244")
	colAccent = lipgloss.Color("#89B4FA")
	colAdd    = lipgloss.Color("#A6E3A1")
	colDel    = lipgloss.Color("#F38BA8")
	colBorder = lipgloss.Color("#45475A")
	colMod    = lipgloss.Color("#F9E2AF")
	colAddBg  = lipgloss.Color("#26332C")
	colDelBg  = lipgloss.Color("#3A2632")
	colHitBg  = lipgloss.Color("#585B70")
	colCurBg  = lipgloss.Color("#F9E2AF")
	colCurFg  = lipgloss.Color("#1E1E2E")
)

var (
	stText   = lipgloss.NewStyle().Foreground(colText)
	stSubtle = lipgloss.NewStyle().Foreground(colSubtle)
	stMuted  = lipgloss.NewStyle().Foreground(colMuted)
	stBorder = lipgloss.NewStyle().Foreground(colBorder)
	stAccent = lipgloss.NewStyle().Foreground(colAccent)
	stAdd    = lipgloss.NewStyle().Foreground(colAdd)
	stDel    = lipgloss.NewStyle().Foreground(colDel)
	stMod    = lipgloss.NewStyle().Foreground(colMod)
	stBold   = lipgloss.NewStyle().Foreground(colText).Bold(true)
	stTitle  = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
)

type symbolSet struct {
	open, closed, bar, dot, vline, hunk, more, less, ellipsis, updown string
	pass, fail, skip, running, idle, warn                             string
	spark                                                             string // sparkline levels, lowest first
}

var unicodeSymbols = symbolSet{
	open: "▾", closed: "▸", bar: "▌", dot: "●", vline: "│", hunk: "┊",
	more: "›", less: "‹", ellipsis: "…", updown: "↑↓",
	pass: "✓", fail: "✗", skip: "○", running: "◌", idle: "·", warn: "!",
	spark: "▁▂▃▄▅▆▇█",
}

var asciiSymbols = symbolSet{
	open: "v", closed: ">", bar: ">", dot: "*", vline: "|", hunk: ":",
	more: ">", less: "<", ellipsis: "~", updown: "j/k",
	pass: "+", fail: "x", skip: "-", running: "~", idle: ".", warn: "!",
	spark: "_.-=+*#",
}

var sym = unicodeSymbols
