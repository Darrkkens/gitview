package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/athos/gitview/internal/config"
	"github.com/athos/gitview/internal/live/capture"
	"github.com/athos/gitview/internal/live/report"
)

func runRuntimeCLI(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var out, errb bytes.Buffer
	code := runRuntimeIO(args, &out, &errb, strings.NewReader(""))
	return out.String(), errb.String(), code
}

// trafficTo sends requests to the proxy announced on stderr while a
// capture runs.
func trafficTo(t *testing.T, stderr *syncBuf, path string, n int) {
	go func() {
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			s := stderr.String()
			if i := strings.Index(s, "http://127.0.0.1:"); i >= 0 {
				addr := strings.Fields(s[i:])[0]
				time.Sleep(300 * time.Millisecond) // past the baseline
				for k := 0; k < n; k++ {
					if resp, err := http.Get(addr + path); err == nil {
						io.Copy(io.Discard, resp.Body)
						resp.Body.Close()
					}
				}
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
}

type syncBuf struct {
	mu chan struct{}
	b  bytes.Buffer
}

func newSyncBuf() *syncBuf { return &syncBuf{mu: make(chan struct{}, 1)} }

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu <- struct{}{}
	defer func() { <-s.mu }()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu <- struct{}{}
	defer func() { <-s.mu }()
	return s.b.String()
}

func TestRuntimeCLIUsage(t *testing.T) {
	if out, _, code := runRuntimeCLI(t, "help"); code != exitOK || !strings.Contains(out, "gitview runtime capture <pid>") {
		t.Errorf("help: %d", code)
	}
	if _, _, code := runRuntimeCLI(t, "nope"); code != exitUsage {
		t.Errorf("unknown subcommand: %d", code)
	}
	t.Setenv(capture.EnvDisable, "off")
	if _, errs, code := runRuntimeCLI(t, "ps"); code != exitErr || !strings.Contains(errs, "desativado") {
		t.Errorf("disabled: %d %s", code, errs)
	}
}

func TestRuntimeCLICaptureShowCompare(t *testing.T) {
	t.Setenv("GITVIEW_CACHE_DIR", t.TempDir())
	privateRuntimeDir(t)
	pid, _ := liveApp(t, "-pprof")
	app := liveAppDir()

	out, _, code := runRuntimeCLI(t, "ps", "--json")
	if code != exitOK || !strings.Contains(out, fmt.Sprintf(`"pid": %d`, pid)) {
		t.Fatalf("ps: %d", code)
	}
	out, _, code = runRuntimeCLI(t, "inspect", fmt.Sprint(pid), "--json")
	if code != exitOK || !strings.Contains(out, `"kind": "pprof"`) {
		t.Fatalf("inspect: %d\n%s", code, out)
	}
	// Profiling needs consent: without --yes nothing runs or is saved.
	_, errs, code := runRuntimeCLI(t, "capture", fmt.Sprint(pid), "--duration", "1s", "--profile", "cpu", "--non-interactive", "--repo", app)
	if code != exitConsent || !strings.Contains(errs, "/debug/pprof/profile?seconds=2") || !strings.Contains(errs, "Nada foi executado") {
		t.Fatalf("consent: %d\n%s", code, errs)
	}
	if out, _, _ := runRuntimeCLI(t, "history", "--json", "--repo", app); strings.TrimSpace(out) != "[]" {
		t.Fatalf("something was saved without consent: %s", out)
	}

	capture1 := func(label string) string {
		var outb bytes.Buffer
		errb := newSyncBuf()
		trafficTo(t, errb, "/clients/5", 8)
		code := runRuntimeIO([]string{"capture", fmt.Sprint(pid), "--duration", "1500ms", "--baseline", "200ms", "--cooldown", "200ms",
			"--interval", "100ms", "--profile", "cpu", "--cpu-window", "1s", "--yes", "--label", label, "--repo", app, "--json"}, &outb, errb, strings.NewReader(""))
		if code != exitOK {
			t.Fatalf("capture: %d\n%s", code, errb.String())
		}
		var a report.Analysis
		if err := json.Unmarshal(outb.Bytes(), &a); err != nil {
			t.Fatalf("capture json: %v", err)
		}
		if len(a.Routes) == 0 || a.Routes[0].Count != 8 || a.Routes[0].Route == nil || !strings.HasSuffix(a.Routes[0].Route.Handler.Name, "getClient") {
			t.Fatalf("capture %s: %+v", label, a.Routes)
		}
		if a.Profiles == nil || a.Profiles.CPU == nil {
			t.Errorf("no CPU profile in the capture")
		}
		return a.ID
	}
	a := capture1("before")
	b := capture1("after")

	html := filepath.Join(t.TempDir(), "r.html")
	out, errs, code = runRuntimeCLI(t, "show", a[:len(a)-2], "--repo", app, "-o", html, "--no-source")
	if code != exitOK || !strings.Contains(out, "GET /clients/{n}") || !strings.Contains(errs, "relatório HTML") {
		t.Fatalf("show: %d\n%s\n%s", code, out, errs)
	}
	if data, err := os.ReadFile(html); err != nil || !bytes.Contains(data, []byte(a)) {
		t.Errorf("html: %v", err)
	}
	out, _, code = runRuntimeCLI(t, "compare", a, b, "--repo", app, "--no-source")
	if code != exitOK || !strings.Contains(out, "Condições") || !strings.Contains(out, "GET /clients/{n}") {
		t.Fatalf("compare: %d\n%s", code, out)
	}
	out, _, code = runRuntimeCLI(t, "compare", a, b, "--repo", app, "--no-source", "--json")
	var cmp report.Comparison
	if code != exitOK || json.Unmarshal([]byte(out), &cmp) != nil || len(cmp.Routes) != 1 {
		t.Fatalf("compare json: %d %s", code, out)
	}
	if _, _, code := runRuntimeCLI(t, "delete", a, "--repo", app); code != exitOK {
		t.Errorf("delete: %d", code)
	}
	out, _, _ = runRuntimeCLI(t, "history", "--json", "--repo", app)
	if strings.Contains(out, a) || !strings.Contains(out, b) {
		t.Errorf("history after delete: %s", out)
	}
}

func TestRepositoryConfigOnlyTightensLimits(t *testing.T) {
	l := capture.DefaultLimits
	applyRuntimeConfig(&l, config.RuntimeConfig{MaxCaptures: 1000, MaxStorageMB: 1 << 20, MaxDuration: "48h", Interval: "1ms"})
	if l != capture.DefaultLimits {
		t.Errorf("a repository raised the limits: %+v", l)
	}
	applyRuntimeConfig(&l, config.RuntimeConfig{MaxCaptures: 3, MaxStorageMB: 10, MaxDuration: "30s", Interval: "1s"})
	if l.MaxCaptures != 3 || l.MaxBytes != 10<<20 || l.MaxDuration != 30*time.Second || l.DefaultEvery != time.Second {
		t.Errorf("limits not lowered: %+v", l)
	}
	// A repository can turn the inspector off.
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, config.FileName), []byte(`{"runtime":{"disabled":true}}`), 0o644)
	e := &runtimeEnv{}
	if _, err := e.store(dir); err == nil || !strings.Contains(err.Error(), "desativado") {
		t.Errorf("disabled by the repository: %v", err)
	}
}

func TestStudyCommandWritesReport(t *testing.T) {
	if testing.Short() {
		t.Skip("analyses a module")
	}
	repo := t.TempDir()
	src := filepath.Join("internal", "study", "testdata", "shop")
	filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		rel, _ := filepath.Rel(src, p)
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(repo, rel), 0o755)
		}
		data, _ := os.ReadFile(p)
		return os.WriteFile(filepath.Join(repo, rel), data, 0o644)
	})
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", repo, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	git("init", "-q")
	git("add", "-A")
	git("commit", "-qm", "init")
	rating := filepath.Join(repo, "internal", "services", "rating.go")
	data, _ := os.ReadFile(rating)
	os.WriteFile(rating, []byte(strings.Replace(string(data), "rate == 0", "rate <= 0", 1)), 0o644)
	out := filepath.Join(t.TempDir(), "s.html")
	var stdout, stderr bytes.Buffer
	if code := runStudy([]string{"-o", out, "--non-interactive", repo}, &stdout, &stderr); code != exitOK {
		t.Fatalf("study: %d\n%s", code, stderr.String())
	}
	html, _ := os.ReadFile(out)
	for _, want := range []string{`id="study-data"`, "calculateOverage", `"readingOrder"`} {
		if !bytes.Contains(html, []byte(want)) {
			t.Errorf("report missing %q", want)
		}
	}
}
