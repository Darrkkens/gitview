package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/athos/gitview/internal/live/capture"
	"github.com/athos/gitview/internal/live/proc"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

var (
	liveOnce sync.Once
	liveBin  string
	liveErr  error
)

func liveAppDir() string {
	d, _ := filepath.Abs(filepath.Join("..", "..", "internal", "live", "testdata", "app"))
	return d
}

// liveApp builds (once) and starts the runtime fixture server.
func liveApp(t *testing.T, args ...string) (pid int, addr string) {
	t.Helper()
	if testing.Short() {
		t.Skip("builds and runs a server")
	}
	if proc.Supported() != nil {
		t.Skip(proc.Supported())
	}
	liveOnce.Do(func() {
		dir, err := os.MkdirTemp("", "gitview-liveapp-")
		if err != nil {
			liveErr = err
			return
		}
		liveBin = filepath.Join(dir, "liveapp")
		cmd := exec.Command("go", "build", "-o", liveBin, ".")
		cmd.Dir = liveAppDir()
		cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOCACHE="+goBuildCache)
		if out, err := cmd.CombinedOutput(); err != nil {
			liveErr = fmt.Errorf("go build: %v\n%s", err, out)
		}
	})
	if liveErr != nil {
		t.Fatal(liveErr)
	}
	cmd := exec.Command(liveBin, args...)
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	sc := bufio.NewScanner(out)
	for sc.Scan() {
		if a, ok := strings.CutPrefix(sc.Text(), "LISTEN "); ok {
			go io.Copy(io.Discard, out)
			return cmd.Process.Pid, a
		}
	}
	t.Fatal("fixture server did not start")
	return 0, ""
}

func runtimeModel(t *testing.T) *model {
	t.Helper()
	t.Setenv("GITVIEW_CACHE_DIR", t.TempDir())
	privateRuntimeDir(t)
	m := newTestModel(t)
	m.Update(tea.WindowSizeMsg{Width: 150, Height: 45})
	m.repo.Root = liveAppDir()
	return m
}

// run executes a command and feeds its message back (one level).
func feed(m *model, cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		for _, c := range b {
			feed(m, c)
		}
		return nil
	}
	_, next := m.Update(msg)
	return next
}

func TestRuntimeScreenCaptureAndReport(t *testing.T) {
	m := runtimeModel(t) // sets the announcement directory the fixture uses
	pid, _ := liveApp(t, "-collector")
	feed(m, m.press("m"))
	if m.screen != screenRuntime || m.rt.view != rtProcs {
		t.Fatalf("screen %d view %d", m.screen, m.rt.view)
	}
	found := false
	for i, p := range m.rt.procs {
		if p.PID == pid {
			m.rt.psel, found = i, true
		}
	}
	if !found {
		t.Fatalf("fixture pid %d not listed", pid)
	}
	feed(m, m.press("enter"))
	if m.rt.view != rtSetup || m.rt.target == nil {
		t.Fatalf("setup: view=%d msg=%q", m.rt.view, m.rt.msg)
	}
	v := ansi.Strip(m.View())
	for _, want := range []string{"PID " + fmt.Sprint(pid), "collector do GitView: listener próprio", "[x] proxy local", "Iniciar captura"} {
		if !strings.Contains(v, want) {
			t.Errorf("setup view missing %q:\n%s", want, v)
		}
	}
	// Profiling asks first; Esc runs nothing.
	m.press("3", "enter")
	if m.rt.confirm == nil || !strings.Contains(strings.Join(m.rt.confirm, "\n"), "/debug/gitview/profile/cpu") {
		t.Fatalf("no confirmation for profiling: %v", m.rt.confirm)
	}
	m.press("esc")
	if m.rt.confirm != nil || m.rt.sess != nil || !strings.Contains(m.rt.msg, "nada foi executado") {
		t.Fatalf("cancel: %q", m.rt.msg)
	}
	m.press("3") // CPU off again
	cmd := m.press("enter")
	if m.rt.sess == nil || m.rt.view != rtLive {
		t.Fatalf("capture not started: %q", m.rt.msg)
	}
	s := m.rt.sess
	deadline := time.Now().Add(10 * time.Second)
	for s.Live(1).Phase != capture.PhaseActive && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	proxy := s.Live(1).Proxy
	for i := 0; i < 6; i++ {
		resp, err := http.Get("http://" + proxy + "/clients/3")
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	time.Sleep(700 * time.Millisecond) // the collector is polled every 500 ms
	m.Update(rtTickMsg{})
	if v := ansi.Strip(m.View()); !strings.Contains(v, "Captura captura ativa") || !strings.Contains(v, "/clients/{n}") || !strings.Contains(v, "http://"+proxy) {
		t.Errorf("live view:\n%s", v)
	}
	// The indicator stays visible on the other screens.
	m.press("esc")
	if v := ansi.Strip(m.View()); !strings.Contains(v, "captura") {
		t.Errorf("no capture indicator in the diff screen:\n%s", v)
	}
	feed(m, m.press("m"))
	m.press("enter") // stop
	// The command started with the capture waits for it to end.
	var done tea.Msg
	for _, c := range cmd().(tea.BatchMsg) {
		if msg, ok := c().(rtDoneMsg); ok {
			done = msg
		}
	}
	if done == nil {
		t.Fatal("no completion message")
	}
	_, next := m.Update(done)
	feed(m, next) // analysis
	if m.rt.view != rtReport || m.rt.analysis == nil {
		t.Fatalf("report: view=%d msg=%q", m.rt.view, m.rt.msg)
	}
	d := m.rt.doc.plain()
	for _, want := range []string{"GET /clients/{id}  6 na fase ativa", "handler main.(*api).getClient", "SQL Explorer: store.(*Store).ClientName", "server side"} {
		if !strings.Contains(d, want) {
			t.Errorf("report missing %q:\n%s", want, d)
		}
	}
	// Enter on the handler opens it in Code Flow.
	for i, l := range m.rt.doc {
		if strings.HasPrefix(l.ref, "sym:") && strings.Contains(fmt.Sprint(l.spans), "getClient") {
			m.rt.sel = i
		}
	}
	feed(m, m.press("enter"))
	if m.screen != screenFlow || m.flow.graph == nil || !strings.HasSuffix(m.flow.rootID, "getClient") {
		t.Fatalf("open function: screen=%d root=%q msg=%q", m.screen, m.flow.rootID, m.rt.msg)
	}
	// Esc returns to the report; export it.
	m.press("esc")
	if m.screen != screenRuntime || m.rt.view != rtReport {
		t.Fatalf("esc from the inspector: screen=%d view=%d", m.screen, m.rt.view)
	}
	m.press("e")
	files, _ := filepath.Glob(filepath.Join(m.export.dir, "runtime-*.html"))
	if len(files) != 1 {
		t.Fatalf("export: %v (%s)", files, m.rt.msg)
	}
	data, _ := os.ReadFile(files[0])
	if !bytes.Contains(data, []byte("Content-Security-Policy")) || !bytes.Contains(data, []byte("GET /clients/{id}")) {
		t.Error("exported report incomplete")
	}
	feed(m, m.press("h"))
	if m.rt.view != rtHistory || len(m.rt.history) != 1 {
		t.Errorf("history: %+v", m.rt.history)
	}
}

func TestQuitDuringCaptureDisarmsCollector(t *testing.T) {
	m := runtimeModel(t)
	pid, _ := liveApp(t, "-collector")
	tg, err := capture.Prepare(context.Background(), proc.Default, pid)
	if err != nil || tg.Collector == nil {
		t.Fatalf("prepare: %v %+v", err, tg)
	}
	m.screen = screenRuntime
	m.rt.target, m.rt.view = tg, rtSetup
	m.press("enter")
	if m.rt.sess == nil {
		t.Fatalf("not started: %q", m.rt.msg)
	}
	if info, _ := tg.Collector.Info(context.Background()); !info.Armed {
		t.Fatal("collector not armed during the capture")
	}
	m.press("esc")
	m.press("q")
	if info, err := tg.Collector.Info(context.Background()); err != nil || info.Armed {
		t.Errorf("collector still armed after quitting: %+v %v", info, err)
	}
	st, _ := m.rtStore()
	if list, _ := st.List(); len(list) != 0 {
		t.Errorf("a cancelled capture was saved: %+v", list)
	}
}

func TestRuntimeScreenOnSmallTerminal(t *testing.T) {
	t.Setenv("GITVIEW_CACHE_DIR", t.TempDir())
	m := newTestModel(t)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	m.screen = screenRuntime
	m.rt.procs = []*proc.Process{{PID: 4242, Comm: "api", Exe: "/very/long/path/to/the/build/output/api", GoVersion: "go1.25.0", MainModule: "example.com/api",
		Ports: []proc.Port{{Proto: "tcp", Addr: "0.0.0.0", Port: 8080}}}}
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "4242") || !strings.Contains(v, "Inspecionar") {
		t.Errorf("process list:\n%s", v)
	}
	m.rt.target = &capture.Target{FS: proc.Default, Process: m.rt.procs[0]}
	m.rt.view = rtSetup
	v = ansi.Strip(m.View())
	if !strings.Contains(v, "Iniciar captura") {
		t.Errorf("setup footer hidden:\n%s", v)
	}
	for _, l := range strings.Split(v, "\n") {
		if w := ansi.StringWidth(l); w > 60 {
			t.Errorf("line wider than the terminal (%d): %q", w, l)
		}
	}
	// Profiles cannot be enabled without an endpoint, and say why.
	m.press("3")
	if m.rt.opts[optCPU] || !strings.Contains(m.rt.msg, "No profiling data available") {
		t.Errorf("cpu without endpoint: %v %q", m.rt.opts[optCPU], m.rt.msg)
	}
	m.rt.view = rtLive
	m.rt.live = capture.Live{ID: "x", Phase: capture.PhaseActive}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "Encerrar") || !strings.Contains(v, "Nenhuma ainda") {
		t.Errorf("live view:\n%s", v)
	}
}

// privateRuntimeDir points XDG_RUNTIME_DIR at a private (0700) temporary
// directory and the user cache at another, so that collector announcements
// never reach the real home directory.
func privateRuntimeDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	os.Chmod(dir, 0o700)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("GOCACHE", goBuildCache) // the go tool keeps its real cache
	return dir
}

// goBuildCache is the Go build cache as it was before the tests redirected
// the user cache directory (a cold cache would rebuild the standard library).
var goBuildCache = func() string {
	if c := os.Getenv("GOCACHE"); c != "" {
		return c
	}
	d, err := os.UserCacheDir()
	if err != nil {
		return os.TempDir()
	}
	return filepath.Join(d, "go-build")
}()
