package main

import (
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/athos/gitview/internal/highlight"
	"github.com/charmbracelet/lipgloss"
)

// mark flags a span as a search hit.
type mark uint8

const (
	markNone mark = iota
	markHit
	markCurrent
)

type span struct {
	text string
	tok  chroma.TokenType
	mark mark
}

var tokStyles sync.Map // chroma.TokenType -> lipgloss.Style

// tokenStyle maps a chroma token to a foreground-only lipgloss style, so the
// terminal background (or the add/remove tint) shows through.
func tokenStyle(t chroma.TokenType) lipgloss.Style {
	if s, ok := tokStyles.Load(t); ok {
		return s.(lipgloss.Style)
	}
	hs := highlight.StyleOf(t)
	s := lipgloss.NewStyle().Foreground(colText).Bold(hs.Bold).Italic(hs.Italic)
	if hs.Color != "" {
		s = s.Foreground(lipgloss.Color(hs.Color))
	}
	tokStyles.Store(t, s)
	return s
}

func toSpans(hs []highlight.Span) []span {
	out := make([]span, len(hs))
	for i, s := range hs {
		out[i] = span{text: s.Text, tok: s.Tok}
	}
	return out
}

// markSpans splits spans at the byte ranges of search hits and flags them, so
// hits are emphasised while keeping each token's own syntax color.
func markSpans(spans []span, hits []hitRange) []span {
	if len(hits) == 0 {
		return spans
	}
	var out []span
	pos := 0
	for _, s := range spans {
		start, end := pos, pos+len(s.text)
		pos = end
		cut := start
		for _, h := range hits {
			if h.end <= cut || h.start >= end {
				continue
			}
			hs, he := max(h.start, cut), min(h.end, end)
			if hs > cut {
				out = append(out, span{s.text[cut-start : hs-start], s.tok, markNone})
			}
			out = append(out, span{s.text[hs-start : he-start], s.tok, h.mark})
			cut = he
		}
		if cut < end {
			out = append(out, span{s.text[cut-start:], s.tok, markNone})
		}
	}
	return out
}
