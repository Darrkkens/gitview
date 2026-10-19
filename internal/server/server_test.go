package server

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type harness struct {
	srv    *Server
	base   string
	cancel context.CancelFunc
}

func start(t *testing.T, cfg Config) *harness {
	t.Helper()
	s := New(cfg)
	ln, err := s.Listen(0)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Serve(ctx, ln); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return &harness{srv: s, base: "http://" + ln.Addr().String(), cancel: cancel}
}

func (h *harness) do(t *testing.T, method, path string, hdr map[string]string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(method, h.base+path, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultTransport.RoundTrip(req) // no transparent gzip
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var r io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		zr, err := gzip.NewReader(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		r = zr
	}
	data, _ := io.ReadAll(r)
	return resp, string(data)
}

func (h *harness) tok() map[string]string {
	return map[string]string{"X-GitView-Token": h.srv.Token()}
}

func TestSectionsAreComputedOnceAndRefreshed(t *testing.T) {
	var runs atomic.Int32
	changes := make(chan struct{}, 1)
	h := start(t, Config{Repository: "repo", Changes: changes, Sections: map[string]Analyzer{
		"graph": func(ctx context.Context) (any, error) {
			n := runs.Add(1)
			return map[string]any{"run": n, "big": strings.Repeat("x", 20<<10)}, nil
		},
		"broken": func(ctx context.Context) (any, error) { return nil, errors.New("no go.mod") },
	}})
	for i := 0; i < 2; i++ {
		resp, body := h.do(t, "GET", "/api/section/graph", map[string]string{"X-GitView-Token": h.srv.Token(), "Accept-Encoding": "gzip"})
		if resp.StatusCode != 200 || !strings.Contains(body, `"run":1`) {
			t.Fatalf("section: %d %.80s", resp.StatusCode, body)
		}
		if resp.Header.Get("Content-Encoding") != "gzip" {
			t.Error("large sections are not compressed")
		}
	}
	if resp, body := h.do(t, "GET", "/api/section/broken", h.tok()); resp.StatusCode != 422 || !strings.Contains(body, "no go.mod") {
		t.Errorf("failing analysis: %d %s", resp.StatusCode, body)
	}
	if resp, _ := h.do(t, "GET", "/api/section/nope", h.tok()); resp.StatusCode != 404 {
		t.Errorf("unknown section: %d", resp.StatusCode)
	}

	// A change marks analysed sections stale; a refresh analyses again.
	changes <- struct{}{}
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, st := h.do(t, "GET", "/api/status", h.tok())
		if strings.Contains(st, `"stale":true`) && strings.Contains(st, `"changedAt"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("status after a change: %s", st)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if resp, _ := h.do(t, "POST", "/api/section/graph/refresh", h.tok()); resp.StatusCode != 200 {
		t.Fatalf("refresh: %d", resp.StatusCode)
	}
	if _, body := h.do(t, "GET", "/api/section/graph", h.tok()); !strings.Contains(body, `"run":2`) {
		t.Errorf("after refresh: %.80s", body)
	}
	if runs.Load() != 2 {
		t.Errorf("analysed %d times, want 2", runs.Load())
	}
}

func TestGuardAndPage(t *testing.T) {
	h := start(t, Config{Repository: "<repo>", View: "security", Sections: map[string]Analyzer{
		"graph": func(context.Context) (any, error) { return 1, nil },
	}})
	if resp, _ := h.do(t, "GET", "/", nil); resp.StatusCode != 403 {
		t.Errorf("page without token: %d", resp.StatusCode)
	}
	resp, page := h.do(t, "GET", "/?t="+h.srv.Token(), nil)
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Security-Policy"), "connect-src 'self'") {
		t.Fatalf("page: %d %q", resp.StatusCode, resp.Header.Get("Content-Security-Policy"))
	}
	for _, want := range []string{`"kind":"serve"`, `"view":"security"`, `"offered":["graph"]`, `"token":"` + h.srv.Token() + `"`, `"repository":"\` + `u003crepo\` + `u003e"`, "<title>GitView · &lt;repo&gt;</title>"} {
		if !strings.Contains(page, want) {
			t.Errorf("page missing %s", want)
		}
	}
	if resp, _ := h.do(t, "GET", "/api/status", nil); resp.StatusCode != 403 {
		t.Errorf("API without token: %d", resp.StatusCode)
	}
	if resp, _ := h.do(t, "GET", "/api/status", map[string]string{"X-GitView-Token": h.srv.Token(), "Origin": "https://evil.example"}); resp.StatusCode != 403 {
		t.Errorf("API from another origin: %d", resp.StatusCode)
	}
	req, _ := http.NewRequest("GET", h.base+"/?t="+h.srv.Token(), nil)
	req.Host = "attacker.example"
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != 403 {
		t.Errorf("foreign Host accepted: %v", err)
	}
	// The page's own origin is accepted.
	if resp, _ := h.do(t, "GET", "/api/status", map[string]string{"X-GitView-Token": h.srv.Token(), "Origin": h.base}); resp.StatusCode != 200 {
		t.Errorf("same origin refused: %d", resp.StatusCode)
	}
}

func TestEventsStream(t *testing.T) {
	changes := make(chan struct{}, 1)
	h := start(t, Config{Changes: changes, Sections: map[string]Analyzer{}})
	if resp, _ := h.do(t, "GET", "/api/events", nil); resp.StatusCode != 403 {
		t.Errorf("events without token: %d", resp.StatusCode)
	}
	resp, err := http.Get(h.base + "/api/events?t=" + h.srv.Token())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("content type %q", resp.Header.Get("Content-Type"))
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		changes <- struct{}{}
	}()
	sc := bufio.NewScanner(resp.Body)
	var event, data string
	for sc.Scan() {
		line := sc.Text()
		if v, ok := strings.CutPrefix(line, "event: "); ok {
			event = v
		}
		if v, ok := strings.CutPrefix(line, "data: "); ok {
			data = v
			break
		}
	}
	var payload map[string]string
	if event != "changed" || json.Unmarshal([]byte(data), &payload) != nil || payload["at"] == "" {
		t.Errorf("event %q %q", event, data)
	}
}

func TestLayoutIsStoredOutsideTheRepository(t *testing.T) {
	file := filepath.Join(t.TempDir(), "cache", "layout.json")
	h := start(t, Config{LayoutFile: file, Sections: map[string]Analyzer{},
		ParseLayout: func(b []byte) (json.RawMessage, error) {
			var v struct {
				V int `json:"layout_version"`
			}
			if json.Unmarshal(b, &v) != nil || v.V != 1 {
				return nil, errors.New("not a layout")
			}
			return b, nil
		}})
	put := func(body, ctype string, hdr map[string]string) (int, string) {
		req, _ := http.NewRequest("PUT", h.base+"/api/layout", strings.NewReader(body))
		req.Header.Set("Content-Type", ctype)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(data)
	}
	if resp, body := h.do(t, "GET", "/api/layout", h.tok()); resp.StatusCode != 204 || body != "" {
		t.Errorf("no layout yet: %d %q", resp.StatusCode, body)
	}
	doc := `{"layout_version":1,"views":{"repository":{"nodes":{"package:x":{"x":1,"y":2}}}}}`
	if code, _ := put(doc, "application/json", nil); code != 403 {
		t.Errorf("PUT without token: %d", code)
	}
	if code, _ := put(doc, "text/plain", h.tok()); code != 415 {
		t.Errorf("PUT non-JSON: %d", code)
	}
	if code, out := put(`{"layout_version":7}`, "application/json", h.tok()); code != 400 || !strings.Contains(out, "not a layout") {
		t.Errorf("invalid document: %d %s", code, out)
	}
	if code, out := put(doc, "application/json", h.tok()); code != 200 || !strings.Contains(out, `"saved":true`) {
		t.Fatalf("PUT: %d %s", code, out)
	}
	if resp, body := h.do(t, "GET", "/api/layout", h.tok()); resp.StatusCode != 200 || body != doc {
		t.Errorf("GET after PUT: %d %s", resp.StatusCode, body)
	}
	if fi, err := os.Stat(filepath.Dir(file)); err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("layout directory: %v", err)
	}
}
