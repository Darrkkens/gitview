package main

import (
	"encoding/json"
	"regexp"
	"testing"
)

var pageDataRe = regexp.MustCompile(`(?s)<script type="application/json" id="gitview-data">(.*?)</script>`)

// pageSections returns the sections of a page's envelope (internal/webui).
func pageSections(t *testing.T, page string) map[string]json.RawMessage {
	t.Helper()
	m := pageDataRe.FindStringSubmatch(page)
	if m == nil {
		t.Fatal("no data block in the page")
	}
	var env struct {
		Sections map[string]json.RawMessage `json:"sections"`
	}
	if err := json.Unmarshal([]byte(m[1]), &env); err != nil {
		t.Fatalf("data block is not JSON: %v", err)
	}
	return env.Sections
}
