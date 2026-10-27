package hover

import (
	"errors"
	"sort"
	"strconv"
)

// Reply is the answer to one hover request.
type Reply struct {
	// Status is ok, stale (the file changed after the analysis), none (no
	// identifier there, or nothing describable), unknown-file (not an
	// analysed Go file) or pending (no analysis yet).
	Status  string `json:"status"`
	Info    *Info  `json:"info,omitempty"`
	Message string `json:"message,omitempty"`
}

// Answer looks up an identifier; r may be nil (no analysis yet).
func Answer(r *Resolver, path string, line, col int, name string) Reply {
	if r == nil {
		return Reply{Status: "pending", Message: "The Go analysis of the repository is still running."}
	}
	in, err := r.Lookup(path, line, col, name)
	switch {
	case err == nil:
		return Reply{Status: "ok", Info: in}
	case errors.Is(err, ErrStale):
		return Reply{Status: "stale", Message: err.Error()}
	case errors.Is(err, ErrNotAnalysed):
		return Reply{Status: "unknown-file", Message: err.Error()}
	}
	return Reply{Status: "none", Message: err.Error()}
}

// Table holds the hovers of chosen lines, for pages without a server.
type Table struct {
	Items []*Info `json:"items"`
	// Files maps a repository-relative file to "line:col:name" → index of
	// the identifier's Info in Items.
	Files map[string]map[string]int `json:"files"`
	// Truncated: the limit was reached; the other identifiers have no hover.
	Truncated bool `json:"truncated,omitempty"`
}

// Key addresses an identifier in Table.Files.
func Key(line, col int, name string) string {
	return strconv.Itoa(line) + ":" + strconv.Itoa(col) + ":" + name
}

// Table resolves every identifier on the given lines of each file, up to
// max identifiers (0: no limit). Identifiers referring to the same object
// share one Info.
func (r *Resolver) Table(lines map[string][]int, max int) *Table {
	t := &Table{Items: []*Info{}, Files: map[string]map[string]int{}}
	index := map[*Info]int{}
	files := make([]string, 0, len(lines))
	for f := range lines {
		files = append(files, f)
	}
	sort.Strings(files)
	n := 0
	for _, path := range files {
		for _, oc := range r.Idents(path, lines[path]) {
			if max > 0 && n >= max {
				t.Truncated = true
				return t
			}
			in, err := r.Lookup(path, oc.Line, oc.Col, oc.Name)
			if err != nil {
				continue
			}
			i, ok := index[in]
			if !ok {
				i = len(t.Items)
				t.Items = append(t.Items, in)
				index[in] = i
			}
			m := t.Files[cleanPath(path)]
			if m == nil {
				m = map[string]int{}
				t.Files[cleanPath(path)] = m
			}
			m[Key(oc.Line, oc.Col, oc.Name)] = i
			n++
		}
	}
	return t
}
