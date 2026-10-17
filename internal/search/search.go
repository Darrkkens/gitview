// Package search finds text in the structured diffs already loaded in memory.
// It never calls git: callers hand it documents, and results for documents
// whose version did not change are reused between searches.
package search

import (
	"fmt"
	"regexp"
)

// Query describes what to look for. By default matching is case-insensitive
// and literal.
type Query struct {
	Text          string
	CaseSensitive bool
	Regex         bool
}

func (q Query) Empty() bool { return q.Text == "" }

// Compile turns the query into a regexp; literal queries are quoted.
func (q Query) Compile() (*regexp.Regexp, error) {
	pat := q.Text
	if !q.Regex {
		pat = regexp.QuoteMeta(pat)
	}
	if !q.CaseSensitive {
		pat = "(?i)" + pat
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return nil, fmt.Errorf("regex inválida: %w", err)
	}
	return re, nil
}

// Doc is one searchable file. Version identifies its content: when it is
// unchanged (==) between searches, cached matches are reused.
type Doc struct {
	Path    string
	Lines   []string
	Version any
}

// PathLine marks a match found in the file path rather than in its content.
const PathLine = -1

type Match struct {
	Path       string
	Line       int // index into Doc.Lines, or PathLine
	Start, End int // byte offsets within the line (or path)
}

type FileHits struct {
	Path      string
	Count     int
	PathMatch bool
	First     int // index of this file's first match in Result.Matches
}

type Result struct {
	Query   Query
	Matches []Match
	Files   []FileHits
	Err     error
}

func (r Result) Active() bool { return !r.Query.Empty() && r.Err == nil }

// FileOf returns the hits summary for path, if any.
func (r Result) FileOf(path string) (FileHits, bool) {
	for _, f := range r.Files {
		if f.Path == path {
			return f, true
		}
	}
	return FileHits{}, false
}

type cacheEntry struct {
	version any
	query   Query
	matches []Match
}

// Index caches per-document matches so repeated searches over live-updating
// diffs only rescan documents that changed.
type Index struct {
	cache map[string]cacheEntry
}

func NewIndex() *Index { return &Index{cache: map[string]cacheEntry{}} }

// maxPerLine bounds pathological regexes (e.g. empty matches) per line.
const maxPerLine = 256

func (ix *Index) Search(q Query, docs []Doc) Result {
	res := Result{Query: q}
	if q.Empty() {
		return res
	}
	re, err := q.Compile()
	if err != nil {
		res.Err = err
		return res
	}
	next := make(map[string]cacheEntry, len(docs))
	for _, d := range docs {
		e, ok := ix.cache[d.Path]
		if !ok || e.query != q || e.version != d.Version || d.Version == nil {
			e = cacheEntry{d.Version, q, scan(re, d)}
		}
		next[d.Path] = e
		if len(e.matches) == 0 {
			continue
		}
		fh := FileHits{Path: d.Path, Count: len(e.matches), First: len(res.Matches)}
		fh.PathMatch = e.matches[0].Line == PathLine
		res.Files = append(res.Files, fh)
		res.Matches = append(res.Matches, e.matches...)
	}
	ix.cache = next
	return res
}

func scan(re *regexp.Regexp, d Doc) []Match {
	var out []Match
	find := func(line int, s string) {
		for _, loc := range re.FindAllStringIndex(s, maxPerLine) {
			if loc[0] == loc[1] {
				continue // ignore empty matches such as "a*"
			}
			out = append(out, Match{Path: d.Path, Line: line, Start: loc[0], End: loc[1]})
		}
	}
	find(PathLine, d.Path)
	for i, l := range d.Lines {
		if l != "" {
			find(i, l)
		}
	}
	return out
}

// Nearest returns the index of the first match at or after (path, line) in
// document order, used to keep the current match stable across refreshes.
func (r Result) Nearest(path string, line int) int {
	for i, m := range r.Matches {
		if m.Path == path && m.Line >= line {
			return i
		}
	}
	for i, m := range r.Matches {
		if m.Path == path {
			return i
		}
	}
	if len(r.Matches) > 0 {
		return 0
	}
	return -1
}

// Summary is a short human description like "8 resultados · 3 arquivos".
func (r Result) Summary() string {
	if r.Err != nil {
		return r.Err.Error()
	}
	n, f := len(r.Matches), len(r.Files)
	plural := func(n int, one, many string) string {
		if n == 1 {
			return one
		}
		return many
	}
	if n == 0 {
		return "nenhum resultado"
	}
	return fmt.Sprintf("%d %s · %d %s", n, plural(n, "resultado", "resultados"), f, plural(f, "arquivo", "arquivos"))
}
