package export

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseLayout(t *testing.T) {
	ok := `{ "layout_version": 1, "repository": "r",
	  "views": {"repository": {"nodes": {"package:x": {"x": 320, "y": 180, "locked": true}}}} }`
	doc, err := ParseLayout([]byte(ok))
	if err != nil || strings.ContainsAny(string(doc), " \n") {
		t.Fatalf("valid document: %v %s", err, doc)
	}
	for _, bad := range []string{`{"views":{}}`, `{"layout_version":2}`, `[1]`, `nope`, `{"layout_version":"1"}`} {
		if _, err := ParseLayout([]byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	if _, err := ParseLayout(bytes.Repeat([]byte(" "), MaxLayoutSize+1)); err == nil {
		t.Error("accepted an oversized document")
	}
}

func TestGraphSectionCarriesTheLayout(t *testing.T) {
	doc, _ := ParseLayout([]byte(`{"layout_version":1,"views":{}}`))
	c, _, err := GraphSection(sample(), HTMLOptions{NoSource: true, Layout: doc})
	if err != nil || c["layout"] == nil {
		t.Fatalf("layout missing: %v", err)
	}
	c, _, _ = GraphSection(sample(), HTMLOptions{NoSource: true})
	if _, ok := c["layout"]; ok {
		t.Error("layout present without one")
	}
}
