// Package highlight tokenizes source lines with Chroma for the TUI and the
// HTML exporter, never altering the text it is given.
package highlight

import (
	"path/filepath"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

type Span struct {
	Text string
	Tok  chroma.TokenType
}

// Style describes how to draw a token in the Catppuccin Mocha theme.
type Style struct {
	Color        string // "#rrggbb", empty for the default text color
	Bold, Italic bool
}

var theme = styles.Get("catppuccin-mocha")

func StyleOf(t chroma.TokenType) Style {
	e := theme.Get(t)
	s := Style{Bold: e.Bold == chroma.Yes, Italic: e.Italic == chroma.Yes}
	if e.Colour.IsSet() {
		s.Color = e.Colour.String()
	}
	return s
}

// Lexer returns a lexer for the file name, or nil for plain text.
// Matching a name against every lexer's patterns is slow, so results are
// cached by file name, and by extension for common source files.
func Lexer(path string) chroma.Lexer {
	base := filepath.Base(path)
	key := base
	if ext := strings.ToLower(filepath.Ext(base)); commonExt[ext] {
		key, base = ext, "x"+ext
	}
	if v, ok := lexerCache.Load(key); ok {
		l, _ := v.(chroma.Lexer)
		return l
	}
	var out chroma.Lexer
	if l := lexers.Match(base); l != nil {
		out = chroma.Coalesce(l)
	}
	lexerCache.Store(key, out)
	return out
}

var lexerCache sync.Map // file name or extension → chroma.Lexer (nil: plain text)

// commonExt are extensions whose lexer does not depend on the rest of the
// file name.
var commonExt = map[string]bool{
	".go": true, ".js": true, ".mjs": true, ".ts": true, ".tsx": true, ".jsx": true, ".vue": true,
	".py": true, ".rb": true, ".rs": true, ".java": true, ".kt": true, ".c": true, ".h": true, ".cpp": true,
	".cs": true, ".php": true, ".sh": true, ".sql": true, ".css": true, ".scss": true, ".html": true,
	".md": true, ".json": true, ".yaml": true, ".yml": true, ".toml": true, ".xml": true, ".proto": true,
}

// Lines tokenizes texts as one contiguous block (so multi-line constructs like
// block comments are understood) and returns spans per line. Any line whose
// spans do not reproduce its text exactly falls back to plain text.
func Lines(lexer chroma.Lexer, texts []string) [][]Span {
	out := make([][]Span, len(texts))
	for i, t := range texts {
		out[i] = Plain(t)
	}
	if lexer == nil || len(texts) == 0 {
		return out
	}
	it, err := lexer.Tokenise(nil, strings.Join(texts, "\n")+"\n")
	if err != nil {
		return out
	}
	for i, toks := range chroma.SplitTokensIntoLines(it.Tokens()) {
		if i >= len(texts) {
			break
		}
		var spans []Span
		var b strings.Builder
		for _, t := range toks {
			v := strings.TrimSuffix(t.Value, "\n")
			if v == "" {
				continue
			}
			spans = append(spans, Span{v, t.Type})
			b.WriteString(v)
		}
		if b.String() == texts[i] {
			out[i] = spans
		}
	}
	return out
}

func Plain(t string) []Span {
	if t == "" {
		return nil
	}
	return []Span{{t, chroma.Text}}
}

// Class returns the short CSS class Chroma uses for a token type (e.g. "kd"),
// walking up to the parent category when the exact type has no class.
func Class(t chroma.TokenType) string {
	for _, c := range []chroma.TokenType{t, t.SubCategory(), t.Category()} {
		if cls, ok := chroma.StandardTypes[c]; ok && cls != "" {
			return cls
		}
	}
	return ""
}
