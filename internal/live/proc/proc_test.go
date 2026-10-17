package proc

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeProc builds a procfs tree.
type fakeProc struct {
	t    *testing.T
	root string
}

func newFake(t *testing.T) *fakeProc {
	t.Helper()
	root := t.TempDir()
	f := &fakeProc{t: t, root: root}
	f.write("stat", "cpu  1 2 3\nbtime 1700000000\n")
	f.link("self/ns/net", "net:[4026531840]")
	return f
}

func (f *fakeProc) write(rel, content string) {
	f.t.Helper()
	p := filepath.Join(f.root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fakeProc) link(rel, target string) {
	f.t.Helper()
	p := filepath.Join(f.root, rel)
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.Remove(p)
	if err := os.Symlink(target, p); err != nil {
		f.t.Fatal(err)
	}
}

func statLine(pid int, comm, state string, utime, stime, threads, start, rss int) string {
	// Fields 3..: state ppid pgrp session tty tpgid flags minflt cminflt
	// majflt cmajflt utime stime cutime cstime priority nice threads
	// itrealvalue starttime vsize rss ...
	return fmt.Sprintf("%d (%s) %s 1 %d %d 0 -1 4194304 10 0 0 0 %d %d 0 0 20 0 %d 0 %d 1000000 %d 18446744073709551615 0 0 0\n",
		pid, comm, state, pid, pid, utime, stime, threads, start, rss)
}

// process writes a complete process. exe is the executable link target.
func (f *fakeProc) process(pid int, comm, exe string, start int, sockets []int) {
	d := strconv.Itoa(pid)
	f.write(d+"/stat", statLine(pid, comm, "S", 150, 50, 3, start, 100))
	f.write(d+"/status", "Name:\t"+comm+"\nUid:\t1000\t1000\t1000\t1000\nVmHWM:\t   9000 kB\nVmRSS:\t   8000 kB\nThreads:\t3\nvoluntary_ctxt_switches:\t40\nnonvoluntary_ctxt_switches:\t2\n")
	f.write(d+"/io", "rchar: 2884\nwchar: 8\nsyscr: 6\nsyscw: 1\nread_bytes: 4096\nwrite_bytes: 0\ncancelled_write_bytes: 0\n")
	f.write(d+"/cmdline", "server\x00--password=hunter2\x00")
	if exe != "" {
		f.link(d+"/exe", exe)
	}
	os.MkdirAll(filepath.Join(f.root, d, "fd"), 0o755)
	f.link(d+"/fd/0", "/dev/null")
	for i, ino := range sockets {
		f.link(fmt.Sprintf("%s/fd/%d", d, i+3), fmt.Sprintf("socket:[%d]", ino))
	}
	f.link(d+"/ns/net", "net:[4026531840]")
	f.write(d+"/net/tcp", `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 5001 1 0000000000000000 100 0 0 10 0
   1: 00000000:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 5002 1 0000000000000000 100 0 0 10 0
   2: 0100007F:1F90 0100007F:D431 01 00000000:00000000 00:00000000 00000000  1000        0 5003 1 0000000000000000 100 0 0 10 0
   3: 00000000:1538 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 9999 1 0000000000000000 100 0 0 10 0
`)
	f.write(d+"/net/tcp6", `  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000000000000000000001000000:1F91 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 5004 1 0000000000000000 100 0 0 10 0
   1: 0000000000000000FFFF00000100007F:1F92 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 5005 1 0000000000000000 100 0 0 10 0
`)
	f.write(d+"/task/"+d+"/schedstat", "1000000000 0 10\n")
	f.write(d+"/task/"+strconv.Itoa(pid+1)+"/schedstat", "500000000 0 10\n")
}

func TestParseStatHandlesParenthesesInName(t *testing.T) {
	st, err := parseStat([]byte(statLine(42, "evil) S 1 (x", "R", 7, 3, 9, 12345, 77)))
	if err != nil {
		t.Fatal(err)
	}
	if st.comm != "evil) S 1 (x" || st.state != "R" || st.utime != 7 || st.stime != 3 || st.threads != 9 || st.startTicks != 12345 || st.rssPages != 77 {
		t.Errorf("parsed %+v", st)
	}
	if _, err := parseStat([]byte("42 (x) S 1")); err == nil {
		t.Error("short stat accepted")
	}
}

func TestParseHexAddr(t *testing.T) {
	cases := map[string]string{
		"0100007F:1F90":                         "127.0.0.1:8080",
		"00000000:0050":                         "0.0.0.0:80",
		"00000000000000000000000001000000:1F91": "::1:8081",
		"00000000000000000000000000000000:0016": ":::22",
		"0000000000000000FFFF00000100007F:1F92": "127.0.0.1:8082",
		"B80D0120000000000000000001000000:0050": "2001:db8::1:80",
	}
	for in, want := range cases {
		addr, port, ok := parseHexAddr(in)
		if got := fmt.Sprintf("%s:%d", addr, port); !ok || got != want {
			t.Errorf("%s → %s (ok=%v), want %s", in, got, ok, want)
		}
	}
}

func TestInspectFakeProcess(t *testing.T) {
	f := newFake(t)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	f.process(4242, "server", self, 500, []int{5001, 5002, 5004, 5005})
	p, err := FS{Root: f.root}.Inspect(4242)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Go || p.GoVersion != runtime.Version() || p.Exe != self {
		t.Errorf("go detection: go=%v version=%q exe=%q", p.Go, p.GoVersion, p.Exe)
	}
	if p.UID != 1000 || p.RSS != 8000*1024 || p.Threads != 3 || p.Args != 2 || p.CPUTime != 2*time.Second || p.CPUAvg <= 0 {
		t.Errorf("identity: %+v", p)
	}
	if want := time.Unix(1700000000, 0).Add(5 * time.Second); !p.Started.Equal(want) {
		t.Errorf("start time %v, want %v", p.Started, want)
	}
	var ports []string
	for _, pt := range p.Ports {
		ports = append(ports, pt.String())
	}
	// 5003 is an established connection, 9999 another process's socket.
	if got := strings.Join(ports, " "); got != "0.0.0.0:80 127.0.0.1:8080 [::1]:8081 127.0.0.1:8082" {
		t.Errorf("ports: %s", got)
	}
	if !p.Ports[1].Loopback || p.Ports[0].Loopback || p.Ports[0].Dial() != "127.0.0.1:80" {
		t.Errorf("loopback/dial: %+v", p.Ports)
	}
	if !p.Access.Stat || !p.Access.Status || !p.Access.IO || !p.Access.FD || !p.Access.Exe || p.Access.Net {
		t.Errorf("access: %+v", p.Access)
	}
	if r := p.Access.Reasons[MetricNet]; !strings.Contains(r, "no per-process network byte counters") {
		t.Errorf("network reason: %q", r)
	}
	// The command line is never kept.
	if data := fmt.Sprintf("%+v", *p); strings.Contains(data, "hunter2") {
		t.Error("command line leaked into the process description")
	}
}

func TestInspectReportsUnreadableMetrics(t *testing.T) {
	f := newFake(t)
	f.process(7, "other", "", 300, nil)
	// A process of another user: io, fd and exe cannot be read.
	for _, rel := range []string{"7/io", "7/fd"} {
		os.RemoveAll(filepath.Join(f.root, rel))
	}
	os.Mkdir(filepath.Join(f.root, "7", "fd"), 0)
	os.WriteFile(filepath.Join(f.root, "7", "io"), nil, 0)
	if os.Getuid() == 0 {
		t.Skip("root reads everything")
	}
	p, err := FS{Root: f.root}.Inspect(7)
	if err != nil {
		t.Fatal(err)
	}
	if p.Access.IO || p.Access.FD || p.Access.Exe || p.Go || p.GoKnown {
		t.Errorf("access: %+v go=%v known=%v", p.Access, p.Go, p.GoKnown)
	}
	for _, m := range []string{MetricIO, MetricFDs, MetricPorts} {
		if !strings.Contains(p.Access.Reasons[m], "permission denied") {
			t.Errorf("%s reason: %q", m, p.Access.Reasons[m])
		}
	}
	if !strings.Contains(p.Access.Reasons[MetricExe], "not available") {
		t.Errorf("exe reason: %q", p.Access.Reasons[MetricExe])
	}
}

func TestListFindsGoProcesses(t *testing.T) {
	f := newFake(t)
	self, _ := os.Executable()
	notGo := filepath.Join(t.TempDir(), "script")
	os.WriteFile(notGo, []byte("#!/bin/sh\necho hi\n"), 0o755)
	f.process(10, "goapp", self, 100, nil)
	f.process(11, "script", notGo, 200, nil)
	f.process(12, "goapp", self, 300, nil)
	list, err := FS{Root: f.root}.List(true)
	if err != nil {
		t.Fatal(err)
	}
	var pids []int
	for _, p := range list {
		pids = append(pids, p.PID)
	}
	if fmt.Sprint(pids) != "[12 10]" {
		t.Errorf("go processes, newest first: %v", pids)
	}
	all, _ := FS{Root: f.root}.List(false)
	if len(all) != 3 {
		t.Errorf("all processes: %d", len(all))
	}
	// A restart of pid 10 is found by executable and start time.
	if s := (FS{Root: f.root}).FindSuccessor(list[1]); s == nil || s.PID != 12 {
		t.Errorf("successor: %+v", s)
	}
	if s := (FS{Root: f.root}).FindSuccessor(list[0]); s != nil {
		t.Errorf("newest process has no successor, got %d", s.PID)
	}
}

func TestSamplerDetectsExitAndReuse(t *testing.T) {
	f := newFake(t)
	f.process(20, "app", "", 100, nil)
	fs := FS{Root: f.root}
	p, err := fs.Inspect(20)
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Now()
	s := fs.NewSampler(p, t0)
	a, err := s.Sample(t0.Add(100 * time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if a.CPUPct != -1 || !a.CPUExact || !a.HasMem || a.RSS != 8000*1024 || a.HWM != 9000*1024 || !a.HasIO || a.ReadBytes != 4096 || !a.HasFDs || a.FDs != 1 || a.HasNet {
		t.Errorf("first sample: %+v", a)
	}
	// 0.25 s of scheduler time in 0.5 s → 50 % of one core, measured from
	// schedstat.
	f.write("20/task/20/schedstat", "1200000000 0 10\n")
	f.write("20/task/21/schedstat", "550000000 0 10\n")
	b, err := s.Sample(t0.Add(600 * time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if b.CPUPct < 49.9 || b.CPUPct > 50.1 || !b.CPUExact {
		t.Errorf("cpu %.2f%% exact=%v", b.CPUPct, b.CPUExact)
	}
	// A thread exits: that interval falls back to the tick counter.
	os.RemoveAll(filepath.Join(f.root, "20/task/21"))
	f.write("20/stat", statLine(20, "app", "S", 160, 60, 2, 100, 100))
	c, err := s.Sample(t0.Add(1100 * time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if c.CPUExact || c.CPUPct < 39.9 || c.CPUPct > 40.1 {
		t.Errorf("tick fallback: %.2f%% exact=%v", c.CPUPct, c.CPUExact)
	}
	// The PID now belongs to another process.
	f.write("20/stat", statLine(20, "other", "S", 1, 1, 1, 999, 10))
	if _, err := s.Sample(t0.Add(2 * time.Second)); !errors.Is(err, ErrRestarted) {
		t.Errorf("pid reuse: %v", err)
	}
	f.write("20/stat", statLine(20, "app", "Z", 1, 1, 1, 100, 0))
	if _, err := s.Sample(t0.Add(3 * time.Second)); !errors.Is(err, ErrExited) {
		t.Errorf("zombie: %v", err)
	}
	os.RemoveAll(filepath.Join(f.root, "20"))
	if _, err := s.Sample(t0.Add(4 * time.Second)); !errors.Is(err, ErrExited) {
		t.Errorf("gone: %v", err)
	}
}

// TestHelperProcess is not a test: it is the Go program the real-process
// test monitors.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GITVIEW_PROC_HELPER") != "1" {
		t.Skip("helper process")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Println("ERR", err)
		os.Exit(1)
	}
	fmt.Println(ln.Addr().(*net.TCPAddr).Port)
	in := bufio.NewScanner(os.Stdin)
	for in.Scan() {
		if in.Text() == "burn" {
			end := time.Now().Add(300 * time.Millisecond)
			x := 0
			for time.Now().Before(end) {
				x++
			}
			fmt.Println("done", x > 0)
		}
	}
	os.Exit(0)
}

func TestRealProcess(t *testing.T) {
	if Supported() != nil {
		t.Skip(Supported())
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
	cmd.Env = append(os.Environ(), "GITVIEW_PROC_HELPER=1")
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	out := bufio.NewScanner(stdout)
	if !out.Scan() {
		t.Fatal("helper did not start")
	}
	port, err := strconv.Atoi(out.Text())
	if err != nil {
		t.Fatalf("helper: %s", out.Text())
	}
	p, err := Default.Inspect(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Go || !p.Access.IO || !p.Access.FD || !p.Access.Exe {
		t.Fatalf("own child: %+v", p)
	}
	found := false
	for _, pt := range p.Ports {
		found = found || pt.Port == port && pt.Loopback
	}
	if !found {
		t.Errorf("listening port %d not found in %+v", port, p.Ports)
	}
	listed, _ := Default.List(true)
	inList := false
	for _, q := range listed {
		inList = inList || q.PID == p.PID
	}
	if !inList {
		t.Error("helper not listed among Go processes")
	}

	t0 := time.Now()
	s := Default.NewSampler(p, t0)
	if _, err := s.Sample(time.Now()); err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(stdin, "burn")
	out.Scan()
	b, err := s.Sample(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// 300 ms of busy loop: well above zero, at most what the clock allows.
	if b.CPUPct < 20 || b.CPUPct > 100*float64(runtime.NumCPU()) || !b.HasMem || b.RSS == 0 || b.Threads == 0 {
		t.Errorf("busy sample: cpu=%.1f%% rss=%d threads=%d exact=%v", b.CPUPct, b.RSS, b.Threads, b.CPUExact)
	}
	stdin.Close()
	cmd.Wait()
	if _, err := s.Sample(time.Now()); !errors.Is(err, ErrExited) {
		t.Errorf("after exit: %v", err)
	}
}
