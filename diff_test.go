package main

import (
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const sampleDiff = `diff --git a/x.go b/x.go
--- a/x.go
+++ b/x.go
@@ -1,4 +1,6 @@ package x
 /* block
 comment */
-func Old() {}
+func New() {
+	return
+
+}
 var x = 1
`

func TestParseDiffNumbersAndKinds(t *testing.T) {
	pd := parseDiff("x.go", sampleDiff)
	want := []struct {
		kind lineKind
		num  int
		text string
	}{
		{kindHunk, 0, "package x"},
		{kindCtx, 1, "/* block"},
		{kindCtx, 2, "comment */"},
		{kindDel, 3, "func Old() {}"},
		{kindAdd, 3, "func New() {"},
		{kindAdd, 4, "    return"},
		{kindAdd, 5, ""},
		{kindAdd, 6, "}"},
		{kindCtx, 7, "var x = 1"},
	}
	if len(pd.lines) != len(want) {
		t.Fatalf("got %d lines, want %d", len(pd.lines), len(want))
	}
	for i, w := range want {
		l := pd.lines[i]
		if l.kind != w.kind || l.num != w.num || l.text != w.text {
			t.Errorf("line %d = %+v, want %+v", i, l, w)
		}
	}
}

func TestHighlightPreservesText(t *testing.T) {
	pd := parseDiff("x.go", sampleDiff)
	for _, l := range pd.lines[1:] {
		var b strings.Builder
		for _, s := range l.spans {
			b.WriteString(s.text)
		}
		if b.String() != l.text {
			t.Errorf("spans %q != text %q", b.String(), l.text)
		}
	}
	// The block comment continuation must be lexed as a comment, not code.
	if s := pd.lines[2].spans; len(s) == 0 || !s[0].tok.InCategory(chroma.Comment) {
		t.Errorf("block comment continuation not lexed as comment: %+v", s)
	}
}

func TestRenderedCodeIsAligned(t *testing.T) {
	pd := parseDiff("x.go", sampleDiff)
	g := gutterWidth(pd.numW)
	for _, l := range renderDiffLines(pd, 60) {
		plain := ansi.Strip(l)
		if plain == "" || strings.Contains(plain, sym.hunk) {
			continue
		}
		if r := []rune(plain); len(r) < g-1 || string(r[g-2]) != sym.vline {
			t.Errorf("separator not at column %d: %q", g-2, plain)
		}
		if lipgloss.Width(l) > 60 {
			t.Errorf("line wider than 60: %q", plain)
		}
	}
}

func TestRenderCodeHorizontalScroll(t *testing.T) {
	spans := []span{{text: strings.Repeat("abcdefghij", 5)}}
	got := ansi.Strip(renderCode(spans, 10, 0, nil))
	if got != "abcdefghi"+sym.more {
		t.Errorf("no scroll: %q", got)
	}
	got = ansi.Strip(renderCode(spans, 10, 45, nil))
	if got != sym.less+"fghij" {
		t.Errorf("scrolled: %q", got)
	}
}

func TestEmptyAddedLineHasNoTint(t *testing.T) {
	out := renderCode(nil, 20, 0, colAddBg)
	if out != "" {
		t.Errorf("empty line rendered %q", out)
	}
	out = renderCode([]span{{text: "    "}}, 20, 0, colAddBg)
	if ansi.Strip(out) != "" {
		t.Errorf("whitespace line rendered %q", out)
	}
}

func TestExpandTabs(t *testing.T) {
	if got := expandTabs("\tx\ty"); got != "    x   y" {
		t.Errorf("got %q", got)
	}
}

func renderDiffLines(pd *parsedDiff, w int) []string {
	out, _ := renderDiff(pd, w, 0, nil)
	return out
}

func TestFirstScreenDoesNotWaitForHighlighting(t *testing.T) {
	diff := "diff --git a/x.go b/x.go\n--- /dev/null\n+++ b/x.go\n@@ -0,0 +1,2 @@\n+// comment\n+package x\n"
	c := newParseCache()
	parsed := c.update([]FileChange{{Path: "x.go", Diff: diff}})
	pd := parsed["x.go"]
	if pd == nil || pd.highlighted || len(pd.lines) != 3 || !c.hasPending() {
		t.Fatalf("plain parse: %+v", pd)
	}
	// Highlighting results for a diff that changed meanwhile are discarded.
	c.update([]FileChange{{Path: "x.go", Diff: diff + "+var y = 1\n"}})
	got := c.highlightPending()
	if hl := got["x.go"]; hl == nil || !hl.highlighted || len(hl.lines) != 4 || c.hasPending() {
		t.Fatalf("highlighted: %+v pending=%v", hl, c.hasPending())
	}

	m := newTestModel(t, FileChange{Path: "x.go", Status: "A", Diff: diff})
	if m.parsed["x.go"].highlighted {
		t.Fatal("the first screen waited for highlighting")
	}
	_, cmd := m.Update(highlightMsg{m.parseCache.highlightPending()})
	if !m.parsed["x.go"].highlighted || cmd != nil {
		t.Errorf("colours not applied: %v", m.parsed["x.go"].highlighted)
	}
}
