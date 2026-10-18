package audit

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/athos/gitview/internal/inspect/sqlops"
)

// tableDef is a table declared in the repository's SQL files.
type tableDef struct {
	columns map[string]bool
	file    string
	line    int
}

var (
	createTable = regexp.MustCompile(`(?is)^create\s+(?:(?:global|local)\s+)?(?:unlogged\s+|temp(?:orary)?\s+)?table\s+(?:if\s+not\s+exists\s+)?([\w."]+)\s*\((.*)\)`)
	alterAdd    = regexp.MustCompile(`(?is)^alter\s+table\s+(?:if\s+exists\s+)?(?:only\s+)?([\w."]+)\s+(.*)$`)
	addColumn   = regexp.MustCompile(`(?is)\badd\s+(?:column\s+)?(?:if\s+not\s+exists\s+)?([\w"]+)`)
	constraintW = map[string]bool{"constraint": true, "primary": true, "unique": true, "foreign": true, "check": true, "exclude": true, "like": true}
)

func normTable(name string) string {
	name = strings.ToLower(strings.ReplaceAll(name, `"`, ""))
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	return name
}

// splitTop splits at commas outside parentheses.
func splitTop(s string) []string {
	var out []string
	depth, start := 0, 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// schemaOf reads the tables declared by the .sql files of the repository
// (migrations, schema dumps): CREATE TABLE and ALTER TABLE … ADD COLUMN.
// It is evidence about which tables carry an owner or tenant column; when
// the repository has no SQL files it is empty and nothing is assumed.
func schemaOf(root string) map[string]*tableDef {
	tables := map[string]*tableDef{}
	if root == "" {
		return tables
	}
	files := 0
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", ".cache", "dist", "build":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".sql") || files > 2000 {
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
			return nil
		}
		files++
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		for _, st := range sqlops.Statements(string(data)) {
			text := strings.TrimSpace(st.Text)
			if m := createTable.FindStringSubmatch(text); m != nil {
				t := &tableDef{columns: map[string]bool{}, file: rel, line: st.Line}
				for _, item := range splitTop(m[2]) {
					f := strings.Fields(strings.TrimSpace(item))
					if len(f) == 0 || constraintW[strings.ToLower(f[0])] {
						continue
					}
					t.columns[strings.ToLower(strings.Trim(f[0], `"`))] = true
				}
				name := normTable(m[1])
				if old := tables[name]; old != nil {
					for c := range old.columns {
						t.columns[c] = true
					}
				}
				tables[name] = t
				continue
			}
			if m := alterAdd.FindStringSubmatch(text); m != nil {
				t := tables[normTable(m[1])]
				if t == nil {
					t = &tableDef{columns: map[string]bool{}, file: rel, line: st.Line}
					tables[normTable(m[1])] = t
				}
				for _, a := range addColumn.FindAllStringSubmatch(m[2], -1) {
					if !constraintW[strings.ToLower(a[1])] {
						t.columns[strings.ToLower(strings.Trim(a[1], `"`))] = true
					}
				}
			}
		}
		return nil
	})
	return tables
}

// ownerColumns lists the owner or tenant columns of a table.
func (t *tableDef) ownerColumns() []string {
	var out []string
	for c := range t.columns {
		if ownerColumn.MatchString(c) && c != "id" {
			out = append(out, c)
		}
	}
	return out
}
