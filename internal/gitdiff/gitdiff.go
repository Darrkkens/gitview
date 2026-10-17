// Package gitdiff parses unified diffs produced by git into a structured form
// shared by the renderer, the search index and the exporters.
package gitdiff

import (
	"strconv"
	"strings"
)

type LineKind int

const (
	Context LineKind = iota
	Added
	Removed
)

func (k LineKind) String() string {
	switch k {
	case Added:
		return "added"
	case Removed:
		return "removed"
	}
	return "context"
}

func (k LineKind) MarshalText() ([]byte, error) { return []byte(k.String()), nil }

type Line struct {
	Kind   LineKind `json:"type"`
	OldNum int      `json:"old,omitempty"`
	NewNum int      `json:"new,omitempty"`
	Text   string   `json:"text"`
}

// Num is the line number shown for the line: old for removals, new otherwise.
func (l Line) Num() int {
	if l.Kind == Removed {
		return l.OldNum
	}
	return l.NewNum
}

type Hunk struct {
	Header   string `json:"header"`
	Section  string `json:"section,omitempty"`
	OldStart int    `json:"oldStart"`
	OldLines int    `json:"oldLines"`
	NewStart int    `json:"newStart"`
	NewLines int    `json:"newLines"`
	Lines    []Line `json:"lines"`
}

// File is one changed file with its parsed hunks.
type File struct {
	Path    string `json:"path"`
	OldPath string `json:"oldPath,omitempty"`
	Status  string `json:"status"`
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
	Binary  bool   `json:"binary"`
	Hunks   []Hunk `json:"hunks"`
	Raw     string `json:"-"`
}

// Parse extracts hunks from a unified diff. Several concatenated file diffs
// (as git emits for renames it did not pair) are parsed as consecutive hunks.
func Parse(raw string) (hunks []Hunk, binary bool) {
	var cur *Hunk
	var oldN, newN int
	for _, l := range strings.Split(strings.TrimRight(raw, "\n"), "\n") {
		switch {
		case strings.HasPrefix(l, "diff --git"):
			cur = nil
		case strings.HasPrefix(l, "Binary files"):
			binary = true
		case strings.HasPrefix(l, "@@"):
			h := parseHeader(l)
			hunks = append(hunks, h)
			cur = &hunks[len(hunks)-1]
			oldN, newN = h.OldStart, h.NewStart
		case cur == nil, strings.HasPrefix(l, `\`):
		default:
			var sign byte = ' '
			body := l
			if l != "" {
				sign, body = l[0], l[1:]
			}
			ln := Line{Text: strings.TrimRight(body, "\r")}
			switch sign {
			case '+':
				ln.Kind, ln.NewNum = Added, newN
				newN++
			case '-':
				ln.Kind, ln.OldNum = Removed, oldN
				oldN++
			default:
				ln.OldNum, ln.NewNum = oldN, newN
				oldN++
				newN++
			}
			cur.Lines = append(cur.Lines, ln)
		}
	}
	return hunks, binary
}

// Count returns added and removed line totals.
func Count(hunks []Hunk) (added, removed int) {
	for _, h := range hunks {
		for _, l := range h.Lines {
			switch l.Kind {
			case Added:
				added++
			case Removed:
				removed++
			}
		}
	}
	return
}

func parseHeader(l string) Hunk {
	h := Hunk{Header: l, OldStart: 1, NewStart: 1}
	if i := strings.Index(l[2:], "@@"); i >= 0 {
		h.Section = strings.TrimSpace(l[i+4:])
	}
	f := strings.Fields(l) // @@ -a,b +c,d @@
	if len(f) < 3 {
		return h
	}
	rng := func(s string) (start, n int) {
		s = strings.TrimLeft(s, "-+")
		n = 1
		if i := strings.IndexByte(s, ','); i >= 0 {
			n, _ = strconv.Atoi(s[i+1:])
			s = s[:i]
		}
		start, _ = strconv.Atoi(s)
		return
	}
	h.OldStart, h.OldLines = rng(f[1])
	h.NewStart, h.NewLines = rng(f[2])
	return h
}
