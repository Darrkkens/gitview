package linuxperf

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakePerf writes a stand-in for perf (tests only): --version answers,
// record writes perf's pipe format according to $FAKE_PERF_MODE, script
// prints $FAKE_PERF_SCRIPT.
func fakePerf(t *testing.T, mode, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "perf")
	const header = `printf 'PERFILE2\020\000\000\000\000\000\000\000'`
	const sample = `printf '\011\000\000\000\000\000\020\000\000\000\000\000\000\000\000\000'`
	src := `#!/bin/sh
case "$1" in
--version) echo "perf version 9.9-fake"; exit 0;;
script) cat "$FAKE_PERF_SCRIPT"; exit 0;;
record) ;;
*) exit 2;;
esac
case "$FAKE_PERF_MODE" in
denied)
	echo "Error: Access to performance monitoring and observability operations is limited." >&2
	echo "Consider adjusting /proc/sys/kernel/perf_event_paranoid setting" >&2
	exit 255;;
gone)
	echo "Error: The sys_perf_event_open() syscall returned with 3 (No such process) for event (cpu-clock:u)." >&2
	exit 255;;
exit)
	` + header + `; ` + sample + `; ` + sample + `; exit 0;;
empty)
	trap 'exit 0' INT; ` + header + `;;
stubborn)
	trap '' INT; ` + header + `; ` + sample + `;;
*)
	trap 'exit 0' INT; ` + header + `; ` + sample + `; ` + sample + `; ` + sample + `;;
esac
while :; do sleep 0.05; done
`
	if err := os.WriteFile(bin, []byte(src), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_PERF_MODE", mode)
	if script != "" {
		sf := filepath.Join(dir, "script.txt")
		if err := os.WriteFile(sf, []byte(script), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("FAKE_PERF_SCRIPT", sf)
	}
	return bin
}

// fakeEnv is a system where perf is allowed for this user.
func fakeEnv(t *testing.T, bin string, paranoid int, caps string) Env {
	t.Helper()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "kernel"), 0o755)
	os.WriteFile(filepath.Join(dir, "kernel/perf_event_paranoid"), []byte(strings.TrimSpace(itoa(paranoid))+"\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "kernel/kptr_restrict"), []byte("1\n"), 0o644)
	status := filepath.Join(dir, "status")
	os.WriteFile(status, []byte("Name:\tgitview\nCapEff:\t"+caps+"\n"), 0o644)
	return Env{Bin: bin, ProcSys: dir, Self: status, EUID: func() int { return 1000 }}
}

func itoa(n int) string {
	if n < 0 {
		return "-" + itoa(-n)
	}
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}

func TestDetectPerfMissing(t *testing.T) {
	t.Setenv(EnvBin, "")
	t.Setenv("PATH", t.TempDir())
	c := Detect(context.Background(), Env{}, os.Geteuid())
	if c.Available || !strings.Contains(c.Reason, "not installed") {
		t.Fatalf("perf missing: %+v", c)
	}
}

func TestDetectRelativeOverrideRefused(t *testing.T) {
	t.Setenv(EnvBin, "perf")
	if c := Detect(context.Background(), Env{}, os.Geteuid()); c.Available {
		t.Fatalf("a relative %s must be refused: %+v", EnvBin, c)
	}
}

func TestDetectPermissions(t *testing.T) {
	bin := fakePerf(t, "ok", "")
	for _, tc := range []struct {
		name      string
		paranoid  int
		caps      string
		uid       int
		available bool
		userOnly  bool
		reason    string
	}{
		{"paranoid 3 forbids", 3, "0000000000000000", 1000, false, false, "perf_event_paranoid is 3"},
		{"paranoid 2 user space only", 2, "0000000000000000", 1000, true, true, ""},
		{"paranoid 1 kernel too", 1, "0000000000000000", 1000, true, false, ""},
		{"another user's process", 1, "0000000000000000", 0, false, false, "another user"},
		{"CAP_PERFMON lifts paranoid", 3, "0000004000000000", 0, true, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Detect(context.Background(), fakeEnv(t, bin, tc.paranoid, tc.caps), tc.uid)
			if c.Available != tc.available || c.UserOnly != tc.userOnly {
				t.Fatalf("got %+v", c)
			}
			if tc.reason != "" && !strings.Contains(c.Reason, tc.reason) {
				t.Errorf("reason %q, want %q", c.Reason, tc.reason)
			}
			if c.Available && c.Version != "9.9-fake" {
				t.Errorf("version %q", c.Version)
			}
		})
	}
}

func TestRecordArgsUseNoShell(t *testing.T) {
	o := RecordOptions{PID: 42, Freq: 99, UserOnly: true}
	got := strings.Join(o.Args(), " ")
	want := "record -e cpu-clock:u -F 99 -g -k monotonic --no-buffering -p 42 -o -"
	if got != want {
		t.Fatalf("args %q, want %q", got, want)
	}
}

func startFake(t *testing.T, bin string, verify func() error) (*Recorder, string, error) {
	t.Helper()
	tmp := t.TempDir()
	r, err := Record(context.Background(), RecordOptions{Bin: bin, PID: 4242, TempDir: tmp, Verify: verify})
	return r, tmp, err
}

func emptyDir(t *testing.T, dir string) {
	t.Helper()
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
}

func TestRecordPermissionDenied(t *testing.T) {
	_, tmp, err := startFake(t, fakePerf(t, "denied", ""), nil)
	if !errors.Is(err, ErrPermission) || !strings.Contains(err.Error(), "perf_event_paranoid") {
		t.Fatalf("err = %v", err)
	}
	emptyDir(t, tmp)
}

func TestRecordProcessGoneBeforeAttach(t *testing.T) {
	_, tmp, err := startFake(t, fakePerf(t, "gone", ""), nil)
	if !errors.Is(err, ErrNoProcess) {
		t.Fatalf("err = %v", err)
	}
	emptyDir(t, tmp)
}

// The PID is checked before perf starts and again once it has attached.
func TestRecordPIDReused(t *testing.T) {
	calls := 0
	_, tmp, err := startFake(t, fakePerf(t, "ok", ""), func() error {
		calls++
		if calls == 2 {
			return ErrReused
		}
		return nil
	})
	if !errors.Is(err, ErrReused) || calls != 2 {
		t.Fatalf("err = %v after %d checks", err, calls)
	}
	emptyDir(t, tmp)
}

// Enter or Ctrl+C: SIGINT, perf flushes and exits; the samples are counted.
func TestRecordStoppedByUser(t *testing.T) {
	r, tmp, err := startFake(t, fakePerf(t, "ok", ""), nil)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for r.Samples() < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	res, err := r.Stop(5 * time.Second)
	if err != nil || res.Samples != 3 || res.Early {
		t.Fatalf("stop: %+v, %v", res, err)
	}
	r.Cleanup()
	emptyDir(t, tmp)
}

// A perf that ignores SIGINT is killed after the grace period.
func TestRecordKilledAfterGrace(t *testing.T) {
	r, _, err := startFake(t, fakePerf(t, "stubborn", ""), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Cleanup()
	start := time.Now()
	if _, err := r.Stop(300 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("stop took %s", d)
	}
}

func TestRecordProcessEndsDuringCapture(t *testing.T) {
	r, _, err := startFake(t, fakePerf(t, "exit", ""), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Cleanup()
	select {
	case <-r.Exited():
	case <-time.After(5 * time.Second):
		t.Fatal("perf did not exit")
	}
	res, err := r.Stop(time.Second)
	if err != nil || !res.Early || res.Samples != 2 {
		t.Fatalf("got %+v, %v", res, err)
	}
}

func sessionWith(t *testing.T, mode, script string) (Result, string, string) {
	t.Helper()
	bin := fakePerf(t, mode, script)
	dir, tmp := t.TempDir(), t.TempDir()
	c := Capability{Available: true, Bin: bin, Version: "9.9-fake", UserOnly: true}
	s, err := Start(context.Background(), Options{Cap: c, PID: 4242, Dir: dir, TempDir: tmp, Proc: t.TempDir()}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return s.Stop(context.Background(), 3*time.Second), dir, tmp
}

func TestSessionNoSamples(t *testing.T) {
	res, dir, tmp := sessionWith(t, "empty", "")
	if res.Error != "" || res.File != "" || res.Samples != 0 || len(res.Notes) == 0 {
		t.Fatalf("got %+v", res)
	}
	emptyDir(t, dir)
	emptyDir(t, tmp)
}

// Without the executable (Go symbols unavailable) the samples are kept with
// perf's names; [unknown] frames are counted.
func TestSessionWithoutGoSymbols(t *testing.T) {
	res, dir, tmp := sessionWith(t, "ok", scriptFixture)
	if res.Error != "" || res.File != DataFile {
		t.Fatalf("got %+v", res)
	}
	emptyDir(t, tmp)
	d, err := Load(dir, res.File)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(res.Notes, " "), "Go symbol table not read") {
		t.Errorf("missing note about the Go symbols: %v", res.Notes)
	}
	r, err := Analyze(&res, d, nil, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.Stats.UnknownLeaf != 1 || r.CPU == nil {
		t.Fatalf("stats %+v", d.Stats)
	}
}

const scriptFixture = `Warning: some message from perf script

  101 1000.000000100:   10101010
	          4a0010 main.work+0x10 (/srv/app)
	          4a0100 main.main+0x20 (/srv/app)
	          430000 runtime.main+0x30 (/srv/app)

  101 1000.010000100:   10101010
	          4b0000 crypto/sha256.blockAVX2.abi0+0x40 (/srv/app)
	e9b5dba5b5c0fbcf [unknown] ([unknown])

  102 1000.020000100:   10101010
	          4a0050 strconv.Itoa (inlined)
	          4a0050 main.work+0x50 (/srv/app)
	          4a0110 net/http.(*Request).Context (inlined)
	          4a0110 main.main+0x30 (/srv/app)

  102 1000.030000100:   10101010
	    7fff0000aa00 [unknown] ([unknown])

  103 1000.040000100:   10101010
	    7fff1000aa00 __vdso_clock_gettime+0x20 ([vdso])
	          4a0020 time.now+0x8 (/srv/app)
`

func TestParseScript(t *testing.T) {
	var got []RawSample
	if err := Parse(strings.NewReader(scriptFixture), func(s RawSample) { got = append(got, s) }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("%d samples, want 5", len(got))
	}
	s := got[0]
	if s.TID != 101 || s.Time != 1000*1e9+100 || s.Period != 10101010 || len(s.Frames) != 3 {
		t.Fatalf("first sample %+v", s)
	}
	if f := s.Frames[0]; f.IP != 0x4a0010 || f.Sym != "main.work" || f.Off != 0x10 || f.DSO != "/srv/app" {
		t.Errorf("frame %+v", f)
	}
	if f := got[2].Frames[0]; !f.Inlined || f.Sym != "strconv.Itoa" {
		t.Errorf("inlined frame %+v", f)
	}
	if f := got[3].Frames[0]; f.Sym != Unknown || f.DSO != Unknown {
		t.Errorf("unknown frame %+v", f)
	}
}

func TestBuilder(t *testing.T) {
	b, _ := NewBuilder(1000*1e9, "/srv/app", nil, nil)
	if err := Parse(strings.NewReader(scriptFixture), b.Add); err != nil {
		t.Fatal(err)
	}
	d := b.Data()
	name := func(sample int) []string {
		var out []string
		for _, f := range d.Stacks[d.S[sample]] {
			out = append(out, d.Frames[f].Func)
		}
		return out
	}
	if d.T[0] != 100 || d.T[1] != 10*int64(time.Millisecond)+100 {
		t.Errorf("offsets %v", d.T)
	}
	// The ABI suffix is dropped; the garbage frame after assembly code ends
	// the stack.
	if got := name(1); strings.Join(got, ",") != "crypto/sha256.blockAVX2" {
		t.Errorf("assembly sample %v", got)
	}
	// Inlined calls are kept at the sampled instruction, dropped at a
	// caller's return address.
	if got := name(2); strings.Join(got, ",") != "strconv.Itoa,main.work,main.main" {
		t.Errorf("inlined sample %v", got)
	}
	st := d.Stats
	if st.Samples != 5 || st.Broken != 1 || st.UnknownLeaf != 1 || st.Foreign != 1 {
		t.Errorf("stats %+v", st)
	}
	n, cpu := d.Between(0, 25*time.Millisecond)
	if n != 3 || cpu != 3*10101010 {
		t.Errorf("between: %d samples, %d ns", n, cpu)
	}
	r, err := Analyze(&Result{Event: "cpu-clock:u", Freq: 99, UserOnly: true}, d, func(t time.Duration) bool { return t < 25*time.Millisecond }, 10*time.Millisecond, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Samples != 3 || r.Recorded != 5 || r.CPU.Samples != 3 || r.SampledCPU != 3*10101010 {
		t.Errorf("report: samples %d recorded %d cpu.samples %d sampled %d", r.Samples, r.Recorded, r.CPU.Samples, r.SampledCPU)
	}
	if len(r.Timeline) != 5 || r.Timeline[0].Samples != 1 {
		t.Errorf("timeline %+v", r.Timeline)
	}
}

func TestDataRejectsBadFile(t *testing.T) {
	dir := t.TempDir()
	d := &Data{Frames: []Frame{{Func: "f"}}, Stacks: [][]int32{{3}}, T: []int64{1}, S: []int32{0}, P: []int64{1}}
	if _, err := d.Save(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir, DataFile); err == nil {
		t.Fatal("a stack pointing outside the frames was accepted")
	}
}
