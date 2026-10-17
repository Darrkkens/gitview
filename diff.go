package main

import (
	"runtime"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/alecthomas/chroma/v2"
	"github.com/athos/gitview/internal/gitdiff"
	"github.com/athos/gitview/internal/highlight"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

const tabWidth = 4

type lineKind byte

const (
	kindCtx lineKind = iota
	kindAdd
	kindDel
	kindHunk
)

type diffLine struct {
	kind  lineKind
	num   int    // new line number for context/additions, old for removals
	text  string // tabs expanded; hunk section for kindHunk
	spans []span
}

// parsedDiff is the display form of a file's diff. lines is the flat list of
// hunk headers and code lines; search hits refer to indices into it.
type parsedDiff struct {
	hunks       []gitdiff.Hunk
	lines       []diffLine
	binary      bool
	numW        int  // width of the line-number column, fixed per file
	highlighted bool // false: plain text until the background highlighting arrives
}

func (pd *parsedDiff) texts() []string {
	out := make([]string, len(pd.lines))
	for i, l := range pd.lines {
		out[i] = l.text
	}
	return out
}

// parseDiff builds the display lines with syntax highlighting. Each hunk's new
// side (context + additions) and old side (removals) are highlighted
// separately so each is lexed as coherent source.
func parseDiff(path, diff string) *parsedDiff { return parseDiffWith(path, diff, true) }

// parseDiffPlain builds the display lines without syntax highlighting: it is
// cheap, so the file list appears at once; colours are added later.
func parseDiffPlain(path, diff string) *parsedDiff { return parseDiffWith(path, diff, false) }

func parseDiffWith(path, diff string, colour bool) *parsedDiff {
	hunks, binary := gitdiff.Parse(diff)
	pd := &parsedDiff{hunks: hunks, binary: binary, numW: 3, highlighted: colour}
	var lexer chroma.Lexer
	if colour {
		lexer = highlight.Lexer(path)
	}
	maxN := 0
	for _, h := range hunks {
		pd.lines = append(pd.lines, diffLine{kind: kindHunk, text: expandTabs(h.Section)})
		var newIdx, oldIdx []int
		var newTxt, oldTxt []string
		for _, l := range h.Lines {
			dl := diffLine{num: l.Num(), text: expandTabs(l.Text)}
			switch l.Kind {
			case gitdiff.Added:
				dl.kind = kindAdd
			case gitdiff.Removed:
				dl.kind = kindDel
			}
			i := len(pd.lines)
			if dl.kind == kindDel {
				oldIdx, oldTxt = append(oldIdx, i), append(oldTxt, dl.text)
			} else {
				newIdx, newTxt = append(newIdx, i), append(newTxt, dl.text)
			}
			maxN = max(maxN, dl.num)
			pd.lines = append(pd.lines, dl)
		}
		for k, s := range highlight.Lines(lexer, newTxt) {
			pd.lines[newIdx[k]].spans = toSpans(s)
		}
		for k, s := range highlight.Lines(lexer, oldTxt) {
			pd.lines[oldIdx[k]].spans = toSpans(s)
		}
	}
	pd.numW = max(len(strconv.Itoa(maxN)), 3)
	return pd
}

func expandTabs(s string) string {
	if !strings.Contains(s, "\t") {
		return s
	}
	var b strings.Builder
	col := 0
	for _, r := range s {
		if r == '\t' {
			n := tabWidth - col%tabWidth
			b.WriteString(strings.Repeat(" ", n))
			col += n
			continue
		}
		b.WriteRune(r)
		col += runewidth.RuneWidth(r)
	}
	return b.String()
}

// gutterWidth is the number of columns before the code: "  + 382 │ ".
func gutterWidth(numW int) int { return 2 + 1 + 1 + numW + 3 }

// hitRange is a search hit inside one display line, in byte offsets.
type hitRange struct {
	start, end int
	mark       mark
}

// renderDiff draws the parsed diff at the given width, scrolled hoff columns.
// rows maps each index of pd.lines to its row in the output (-1 when hidden).
func renderDiff(pd *parsedDiff, width, hoff int, hits map[int][]hitRange) (out []string, rows []int) {
	pad := strings.Repeat(" ", gutterWidth(pd.numW))
	if pd.binary {
		return []string{pad + stMuted.Render("arquivo binário")}, nil
	}
	if len(pd.lines) == 0 {
		return []string{pad + stMuted.Render("sem alterações de conteúdo")}, nil
	}
	codeW := max(width-gutterWidth(pd.numW)-1, 8)
	out = make([]string, 0, len(pd.lines)+4)
	rows = make([]int, len(pd.lines))
	for i, l := range pd.lines {
		rows[i] = -1
		if l.kind == kindHunk {
			if i > 0 {
				out = append(out, "")
			}
			if l.text == "" {
				continue
			}
		}
		rows[i] = len(out)
		out = append(out, renderDiffLine(l, pd.numW, codeW, hoff, hits[i]))
	}
	return out, rows
}

func renderDiffLine(l diffLine, numW, codeW, hoff int, hits []hitRange) string {
	num := strconv.Itoa(l.num)
	num = strings.Repeat(" ", numW-len(num)) + num
	sep := stBorder.Render(" " + sym.vline + " ")
	var bg lipgloss.TerminalColor
	var gutter string
	switch l.kind {
	case kindHunk:
		pad := strings.Repeat(" ", 4+numW)
		spans := markSpans([]span{{text: l.text}}, hits)
		return pad + stBorder.Render(" "+sym.hunk+" ") + renderSpans(spans, codeW, 0, nil, true)
	case kindAdd:
		gutter = "  " + stAdd.Render("+ "+num) + sep
		bg = colAddBg
	case kindDel:
		gutter = "  " + stDel.Render("- "+num) + sep
		bg = colDelBg
	default:
		gutter = "    " + stMuted.Render(num) + sep
	}
	return gutter + renderCode(markSpans(l.spans, hits), codeW, hoff, bg)
}

func renderCode(spans []span, codeW, hoff int, bg lipgloss.TerminalColor) string {
	return renderSpans(spans, codeW, hoff, bg, false)
}

// renderSpans slices spans horizontally and applies the change tint only to
// actual code: never to indentation, trailing whitespace or empty lines.
// Search hits keep their syntax color and gain a distinct background.
func renderSpans(spans []span, codeW, hoff int, bg lipgloss.TerminalColor, muted bool) string {
	total := 0
	for _, s := range spans {
		total += runewidth.StringWidth(s.text)
	}
	var b strings.Builder
	w := codeW
	if hoff > 0 && total > hoff {
		b.WriteString(stMuted.Render(sym.less))
		w--
	}
	more := total > hoff+w
	if more {
		w--
	}
	visible := trimTrailingSpace(sliceSpans(spans, hoff, w))
	leading := hoff == 0
	for _, s := range visible {
		text := s.text
		st := tokenStyle(s.tok)
		if muted {
			st = stMuted.Italic(true)
		}
		if leading && s.mark == markNone {
			trimmed := strings.TrimLeftFunc(text, unicode.IsSpace)
			b.WriteString(text[:len(text)-len(trimmed)])
			text = trimmed
		}
		if text == "" {
			continue
		}
		leading = false
		switch s.mark {
		case markCurrent:
			st = st.Foreground(colCurFg).Background(colCurBg).Bold(true)
		case markHit:
			st = st.Background(colHitBg).Underline(true)
		default:
			if bg != nil {
				st = st.Background(bg)
			}
		}
		b.WriteString(st.Render(text))
	}
	if more {
		b.WriteString(stMuted.Render(sym.more))
	}
	return b.String()
}

func sliceSpans(spans []span, from, w int) []span {
	var out []span
	col := 0
	for _, s := range spans {
		var b strings.Builder
		for _, r := range s.text {
			rw := runewidth.RuneWidth(r)
			if col < from {
				col += rw
				continue
			}
			if col+rw > from+w {
				if b.Len() > 0 {
					out = append(out, span{b.String(), s.tok, s.mark})
				}
				return out
			}
			b.WriteRune(r)
			col += rw
		}
		if b.Len() > 0 {
			out = append(out, span{b.String(), s.tok, s.mark})
		}
	}
	return out
}

func trimTrailingSpace(spans []span) []span {
	for len(spans) > 0 {
		last := &spans[len(spans)-1]
		if last.mark != markNone {
			break
		}
		last.text = strings.TrimRightFunc(last.text, unicode.IsSpace)
		if last.text != "" {
			break
		}
		spans = spans[:len(spans)-1]
	}
	return spans
}

// parseCache keeps parsed diffs between refreshes so only changed files are re-highlighted.
type parseCache struct {
	mu      sync.Mutex
	entries map[string]parseEntry
}

type parseEntry struct {
	diff string
	pd   *parsedDiff
}

func newParseCache() *parseCache { return &parseCache{entries: map[string]parseEntry{}} }

// update parses the diffs that changed without syntax highlighting (cheap,
// so the screen is ready at once); highlightPending adds the colours.
func (c *parseCache) update(files []FileChange) map[string]*parsedDiff {
	c.mu.Lock()
	defer c.mu.Unlock()
	next := make(map[string]parseEntry, len(files))
	out := make(map[string]*parsedDiff, len(files))
	for _, f := range files {
		e, ok := c.entries[f.Path]
		if !ok || e.diff != f.Diff {
			e = parseEntry{f.Diff, parseDiffPlain(f.Path, f.Diff)}
		}
		next[f.Path] = e
		out[f.Path] = e.pd
	}
	c.entries = next
	return out
}

// highlightPending highlights, in parallel, the cached diffs still shown as
// plain text, and returns those that are still current.
func (c *parseCache) highlightPending() map[string]*parsedDiff {
	c.mu.Lock()
	var jobs []parseJob
	for path, e := range c.entries {
		if !e.pd.highlighted && !e.pd.binary {
			jobs = append(jobs, parseJob{path, e.diff})
		}
	}
	c.mu.Unlock()
	if len(jobs) == 0 {
		return nil
	}
	results := make([]*parsedDiff, len(jobs))
	work := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < min(runtime.NumCPU(), len(jobs)); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				results[i] = parseDiff(jobs[i].path, jobs[i].diff)
			}
		}()
	}
	for i := range jobs {
		work <- i
	}
	close(work)
	wg.Wait()
	c.mu.Lock()
	defer c.mu.Unlock()
	out := map[string]*parsedDiff{}
	for i, j := range jobs {
		if e, ok := c.entries[j.path]; ok && e.diff == j.diff {
			c.entries[j.path] = parseEntry{j.diff, results[i]}
			out[j.path] = results[i]
		}
	}
	return out
}

type parseJob struct{ path, diff string }

// hasPending reports whether some diff is still shown without highlighting.
func (c *parseCache) hasPending() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.entries {
		if !e.pd.highlighted && !e.pd.binary {
			return true
		}
	}
	return false
}
