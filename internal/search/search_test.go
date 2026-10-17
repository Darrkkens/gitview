package search

import (
	"strings"
	"testing"
)

func docs() []Doc {
	return []Doc{
		{Path: "auth/service.go", Lines: []string{"func token_create() {", "  token_ := 1", "}"}, Version: 1},
		{Path: "auth/handler.go", Lines: []string{"x := TOKEN_value", ""}, Version: 1},
		{Path: "telecom/telecom.go", Lines: []string{"nothing here"}, Version: 1},
		{Path: "token_store/store.go", Lines: nil, Version: 1}, // collapsed/unloaded: path only
	}
}

func TestSearchAcrossFilesCaseInsensitive(t *testing.T) {
	r := NewIndex().Search(Query{Text: "token_"}, docs())
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	if len(r.Files) != 3 {
		t.Fatalf("files = %+v", r.Files)
	}
	if got := len(r.Matches); got != 4 {
		t.Fatalf("matches = %d: %+v", got, r.Matches)
	}
	if !r.Files[2].PathMatch || r.Matches[r.Files[2].First].Line != PathLine {
		t.Errorf("path match not reported: %+v", r.Files[2])
	}
	if s := r.Summary(); s != "4 resultados · 3 arquivos" {
		t.Errorf("summary %q", s)
	}
	m := r.Matches[0]
	if m.Path != "auth/service.go" || m.Line != 0 || docs()[0].Lines[0][m.Start:m.End] != "token_" {
		t.Errorf("first match %+v", m)
	}
}

func TestSearchCaseSensitive(t *testing.T) {
	r := NewIndex().Search(Query{Text: "TOKEN", CaseSensitive: true}, docs())
	if len(r.Matches) != 1 || r.Matches[0].Path != "auth/handler.go" {
		t.Fatalf("matches = %+v", r.Matches)
	}
}

func TestSearchRegex(t *testing.T) {
	r := NewIndex().Search(Query{Text: `token_\w+\(`, Regex: true}, docs())
	if len(r.Matches) != 1 || r.Matches[0].End-r.Matches[0].Start != len("token_create(") {
		t.Fatalf("matches = %+v", r.Matches)
	}
	// Literal mode must not interpret metacharacters.
	r = NewIndex().Search(Query{Text: `(`}, docs())
	if r.Err != nil || len(r.Matches) != 1 {
		t.Fatalf("literal paren: %+v %v", r.Matches, r.Err)
	}
}

func TestSearchInvalidRegex(t *testing.T) {
	r := NewIndex().Search(Query{Text: `token_(`, Regex: true}, docs())
	if r.Err == nil || !strings.Contains(r.Err.Error(), "regex inválida") {
		t.Fatalf("err = %v", r.Err)
	}
	if r.Active() || len(r.Matches) != 0 {
		t.Fatal("invalid regex must not produce results")
	}
}

func TestSearchNoResults(t *testing.T) {
	r := NewIndex().Search(Query{Text: "zzz"}, docs())
	if r.Err != nil || len(r.Matches) != 0 || len(r.Files) != 0 || r.Summary() != "nenhum resultado" {
		t.Fatalf("%+v", r)
	}
	if r.Nearest("x", 0) != -1 {
		t.Fatal("nearest on empty result")
	}
}

func TestSearchEmptyMatchesIgnored(t *testing.T) {
	r := NewIndex().Search(Query{Text: `x*`, Regex: true}, docs())
	for _, m := range r.Matches {
		if m.Start == m.End {
			t.Fatalf("empty match %+v", m)
		}
	}
}

func TestIndexIsIncremental(t *testing.T) {
	ix := NewIndex()
	d := docs()
	q := Query{Text: "token_"}
	ix.Search(q, d)

	// Same version: cached matches are reused even though lines changed
	// (proves unchanged documents are not rescanned).
	d[0].Lines = []string{"no match"}
	if r := ix.Search(q, d); len(r.Matches) != 4 {
		t.Fatalf("expected cached result, got %d", len(r.Matches))
	}
	// New version: rescanned.
	d[0].Version = 2
	if r := ix.Search(q, d); len(r.Matches) != 2 {
		t.Fatalf("expected rescanned result, got %d", len(r.Matches))
	}
	// Removed documents disappear from results.
	if r := ix.Search(q, d[1:2]); len(r.Files) != 1 {
		t.Fatalf("files after removal: %+v", r.Files)
	}
}

func TestNearestKeepsPosition(t *testing.T) {
	r := NewIndex().Search(Query{Text: "token_"}, docs())
	if i := r.Nearest("auth/service.go", 1); r.Matches[i].Line != 1 {
		t.Fatalf("nearest = %+v", r.Matches[i])
	}
	if i := r.Nearest("gone.go", 0); i != 0 {
		t.Fatalf("fallback nearest = %d", i)
	}
}
