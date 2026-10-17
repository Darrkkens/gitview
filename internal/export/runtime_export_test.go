package export

import (
	"bytes"
	"encoding/json"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/athos/gitview/internal/live/capture"
	"github.com/athos/gitview/internal/live/report"
)

func hostileAnalysis(id string) *report.Analysis {
	evil := `</script><script>alert(1)</script><img src=x onerror=alert(2)>`
	return &report.Analysis{
		ID: id, Label: evil, Created: time.Unix(1000, 0), Status: capture.StatusComplete,
		ProcessInfo: capture.ProcessInfo{PID: 7, Exe: evil},
		Process:     report.Process{Phases: []report.PhaseStats{{Name: "baseline"}, {Name: "active"}, {Name: "cooldown"}}},
		Routes: []report.RouteStats{{Key: "GET " + evil, Count: 1, Status: map[int]int{200: 1},
			Statements: []report.StatementStats{{Query: "SELECT '" + evil + "'", Count: 1}}}},
		Requests:    []report.RequestRow{{ID: "r1", Key: "GET " + evil, Path: evil, Status: 200, Phase: "active"}},
		Limitations: []string{evil},
	}
}

func TestRuntimeHTMLIsInertAndPinned(t *testing.T) {
	a, b := hostileAnalysis("A1"), hostileAnalysis("B2")
	var buf bytes.Buffer
	if err := RuntimeHTML(&buf, b, report.Compare(a, b)); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	if strings.Contains(s, "<script>alert") || strings.Contains(s, "<img src=x") {
		t.Fatal("hostile markup reached the page")
	}
	csp := regexp.MustCompile(`Content-Security-Policy" content="([^"]+)"`).FindStringSubmatch(s)
	if len(csp) < 2 {
		t.Fatal("no CSP")
	}
	policy := html.UnescapeString(csp[1])
	if !strings.Contains(policy, "default-src 'none'") || !strings.Contains(policy, scriptHash(runtimeJS)) || strings.Count(policy, "sha256-") != 1 {
		t.Errorf("CSP: %s", policy)
	}
	scripts := regexp.MustCompile(`(?s)<script>(.*?)</script>`).FindAllStringSubmatch(s, -1)
	if len(scripts) != 1 || scripts[0][1] != runtimeJS {
		t.Errorf("inline scripts: %d", len(scripts))
	}
	data := regexp.MustCompile(`(?s)<script type="application/json" id="rt-data">(.*?)</script>`).FindStringSubmatch(s)
	if len(data) != 2 {
		t.Fatal("no data block")
	}
	var payload struct {
		A          *report.Analysis   `json:"a"`
		Comparison *report.Comparison `json:"comparison"`
	}
	if err := json.Unmarshal([]byte(data[1]), &payload); err != nil || payload.A.ID != "B2" || payload.Comparison == nil {
		t.Fatalf("payload: %v", err)
	}
	if !strings.Contains(payload.A.Label, "<script>") {
		t.Error("the label should survive as data")
	}
	// No network access: nothing is loaded from elsewhere.
	if regexp.MustCompile(`(src|href)="(https?:)?//`).MatchString(s) {
		t.Error("external resource referenced")
	}
}

func TestRuntimeJSParses(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	f := filepath.Join(t.TempDir(), "runtime.js")
	os.WriteFile(f, []byte(runtimeJS), 0o644)
	if out, err := exec.Command(node, "--check", f).CombinedOutput(); err != nil {
		t.Errorf("runtime.js: %v\n%s", err, out)
	}
}
