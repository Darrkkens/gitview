package export

import (
	"encoding/json"
	"html"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/athos/gitview/internal/study"
)

func TestStudySectionIsInert(t *testing.T) {
	evil := `</script><script>alert(1)</script><img src=x onerror=alert(2)>`
	m := &study.Model{Version: 1, Fingerprint: "abc", Layers: study.Layers,
		ReadingOrder: []study.Step{{N: 1, Kind: "function", Title: evil, ID: "x." + evil, File: "a.go", Line: 1, Layer: study.LayerService,
			Why: []study.Evidence{{Basis: study.Static, Text: evil, File: "a.go", Line: 1, Ref: "codeflow:" + evil}}}},
		Exercises: []study.Exercise{{ID: evil, Kind: "quiz", Type: "sql", Prompt: evil, Code: evil, Options: []string{evil, "b", "c"}, SourceType: "SQL", Evidence: study.Evidence{Basis: study.SQLBasis, Text: evil}}},
		SQL:       study.SQLStudy{Queries: []study.SQLQuery{{ID: evil, FuncName: evil, SQL: evil, Statement: "select"}}}}
	r := Report{Project: "p", GeneratedAt: time.Unix(0, 0), Study: m}
	out, err := Render(HTML, r)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "<script>alert") || strings.Contains(s, "<img src=x") {
		t.Fatal("hostile markup reached the page")
	}
	if !strings.Contains(s, `href="#study" data-section="study"`) || !strings.Contains(html.UnescapeString(s), scriptHash(studyJS)) {
		t.Error("Study tab or its pinned script missing")
	}
	data := regexp.MustCompile(`(?s)<script type="application/json" id="study-data">(.*?)</script>`).FindStringSubmatch(s)
	var back study.Model
	if len(data) != 2 || json.Unmarshal([]byte(data[1]), &back) != nil || back.ReadingOrder[0].Title != evil {
		t.Error("study data block unreadable")
	}
	// Without Study the report is unchanged: no tab, no script.
	plain, _ := Render(HTML, Report{Project: "p", GeneratedAt: time.Unix(0, 0)})
	if strings.Contains(string(plain), `id="study"`) || strings.Contains(string(plain), "study-data") {
		t.Error("Study present in a report without it")
	}
	md, _ := Render(Markdown, r)
	if !strings.Contains(string(md), "## Study: start here") {
		t.Error("markdown summary missing")
	}
}
