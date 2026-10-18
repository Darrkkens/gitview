package webui

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

func TestRenderPinsTheScriptAndEmbedsInertData(t *testing.T) {
	page, err := Render(Document{Kind: "graph", Title: "GitView · <repo>", Repository: "repo",
		Sections: map[string]any{
			"graph":    map[string]string{"name": "</script><script>alert(1)</script>"},
			"security": json.RawMessage(`{"x":"</script>"}`),
		},
		Sources: "H4sIAAAA"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(page)
	if n := strings.Count(s, "</script>"); n != 3 { // app, data, sources
		t.Fatalf("%d </script> in the page, want 3 (data must not close its block)", n)
	}
	if !strings.Contains(s, `<title>GitView · &lt;repo&gt;</title>`) {
		t.Error("title not escaped")
	}
	csp := regexp.MustCompile(`<meta http-equiv="Content-Security-Policy" content="([^"]+)">`).FindStringSubmatch(s)
	if csp == nil {
		t.Fatal("no CSP meta")
	}
	if strings.Index(s, csp[0]) > strings.Index(s, "<script") {
		t.Error("the CSP must come before the first script")
	}
	policy := strings.ReplaceAll(csp[1], "&#39;", "'")
	if !strings.Contains(policy, "default-src 'none'") || strings.Contains(policy, "connect-src") {
		t.Errorf("offline policy %q", policy)
	}
	// The app's script is exactly the one pinned.
	m := regexp.MustCompile(`(?s)<script type="module"[^>]*>(.*?)</script>`).FindStringSubmatch(s)
	if m == nil || !strings.Contains(policy, hash(m[1])) {
		t.Error("the inline app script is not pinned by its hash")
	}
	data := regexp.MustCompile(`(?s)<script type="application/json" id="gitview-data">(.*?)</script>`).FindStringSubmatch(s)
	var env struct {
		Kind     string                     `json:"kind"`
		Sections map[string]json.RawMessage `json:"sections"`
	}
	if err := json.Unmarshal([]byte(data[1]), &env); err != nil {
		t.Fatal(err)
	}
	if env.Kind != "graph" || len(env.Sections) != 2 {
		t.Errorf("envelope %+v", env)
	}
	if !strings.Contains(s, `id="gitview-sources">H4sIAAAA</script>`) {
		t.Error("sources block missing")
	}
}

func TestLivePolicyAllowsOnlyTheServer(t *testing.T) {
	if p := CSP(true); !strings.Contains(p, "connect-src 'self'") {
		t.Errorf("live policy %q", p)
	}
	page, _ := Render(Document{Kind: "serve", Live: &Live{Token: "abc"}})
	if !strings.Contains(string(page), `"live":{"token":"abc"}`) {
		t.Error("live token missing from the envelope")
	}
}

func TestParseRejectsExternalScripts(t *testing.T) {
	_, err := parse(`<html><head><title>GitView</title><script src="https://x/y.js"></script></head><body></body></html>`)
	if err == nil {
		t.Error("an external script was accepted")
	}
}
