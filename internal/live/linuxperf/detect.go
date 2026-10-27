// Package linuxperf samples the CPU of a running process from the outside
// with Linux perf (perf record / perf script): no profiling endpoint, no
// instrumentation and no restart of the monitored application.
//
// perf runs as a child process started with exec.CommandContext (never a
// shell), attached to one PID, writing to a private temporary directory
// that is removed when the capture ends. GitView never changes kernel
// settings: when perf_event_paranoid or the process's owner forbid
// sampling, Detect says why and the capture goes on without perf.
//
// perf samples CPU only. Go heap, goroutine and execution-trace profiles
// still need net/http/pprof or GitView's collector.
package linuxperf

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// EnvBin overrides the perf executable (an absolute path).
const EnvBin = "GITVIEW_PERF"

// Capability says whether perf can sample a process, and how.
type Capability struct {
	Available bool   `json:"available"`
	Bin       string `json:"bin,omitempty"`
	Version   string `json:"version,omitempty"`
	// Paranoid is kernel.perf_event_paranoid (-1 when unreadable is
	// represented by Known=false).
	Paranoid      int  `json:"paranoid"`
	ParanoidKnown bool `json:"paranoidKnown"`
	KptrRestrict  int  `json:"kptrRestrict"`
	Privileged    bool `json:"privileged"` // root, CAP_PERFMON or CAP_SYS_ADMIN
	// UserOnly: only user-space time can be sampled (cpu-clock:u); CPU spent
	// in the kernel on behalf of the process is not in the samples.
	UserOnly bool     `json:"userOnly"`
	Reason   string   `json:"reason,omitempty"` // why it is unavailable
	Warnings []string `json:"warnings,omitempty"`
}

// Env is what Detect reads; tests replace it.
type Env struct {
	Bin     string     // perf executable; default $GITVIEW_PERF, else perf on PATH
	ProcSys string     // default /proc/sys
	Self    string     // status file of this process; default /proc/self/status
	EUID    func() int // default os.Geteuid
	// Run executes perf --version; default exec.CommandContext.
	Run func(ctx context.Context, bin string, args ...string) ([]byte, error)
}

func (e *Env) defaults() {
	if e.ProcSys == "" {
		e.ProcSys = "/proc/sys"
	}
	if e.Self == "" {
		e.Self = "/proc/self/status"
	}
	if e.EUID == nil {
		e.EUID = os.Geteuid
	}
	if e.Run == nil {
		e.Run = func(ctx context.Context, bin string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, bin, args...).CombinedOutput()
		}
	}
}

// FindBin locates perf: $GITVIEW_PERF (must be absolute), else PATH.
func FindBin() (string, error) {
	if b := os.Getenv(EnvBin); b != "" {
		if !filepath.IsAbs(b) {
			return "", fmt.Errorf("%s must be an absolute path", EnvBin)
		}
		return b, nil
	}
	b, err := exec.LookPath("perf")
	if err != nil {
		return "", errors.New("Linux perf is not installed (install linux-tools / linux-perf / perf for your distribution)")
	}
	return b, nil
}

// Capabilities CAP_SYS_ADMIN and CAP_PERFMON (bits of CapEff).
const (
	capSysAdmin = 21
	capPerfmon  = 38
)

// Detect checks perf for sampling a process owned by uid.
func Detect(ctx context.Context, env Env, uid int) Capability {
	env.defaults()
	c := Capability{}
	bin := env.Bin
	if bin == "" {
		b, err := FindBin()
		if err != nil {
			c.Reason = err.Error()
			return c
		}
		bin = b
	}
	c.Bin = bin
	vctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	out, err := env.Run(vctx, bin, "--version")
	cancel()
	if err != nil {
		c.Reason = "perf does not run (" + strings.TrimSpace(firstLine(out, err)) + "): on some distributions the perf wrapper needs the tools package of the running kernel"
		return c
	}
	c.Version = strings.TrimSpace(strings.TrimPrefix(firstLine(out, nil), "perf version"))

	euid := env.EUID()
	c.Privileged = euid == 0 || hasCap(env.Self, capPerfmon) || hasCap(env.Self, capSysAdmin)
	if n, err := readInt(filepath.Join(env.ProcSys, "kernel/perf_event_paranoid")); err == nil {
		c.Paranoid, c.ParanoidKnown = n, true
	}
	if n, err := readInt(filepath.Join(env.ProcSys, "kernel/kptr_restrict")); err == nil {
		c.KptrRestrict = n
	}
	if !c.ParanoidKnown && !c.Privileged {
		// No perf_event support in the kernel (or no /proc/sys): the attempt
		// will tell; sample user space only.
		c.UserOnly = true
		c.Warnings = append(c.Warnings, "kernel.perf_event_paranoid cannot be read: perf may be unavailable in this kernel or container.")
	}
	if !c.Privileged && uid >= 0 && uid != euid {
		c.Reason = fmt.Sprintf("the process belongs to another user (uid %d): perf can only attach to your own processes without root or CAP_PERFMON", uid)
		return c
	}
	if c.ParanoidKnown && !c.Privileged {
		switch {
		case c.Paranoid >= 3:
			c.Reason = fmt.Sprintf("kernel.perf_event_paranoid is %d: unprivileged users cannot use perf. An administrator can allow it (sysctl kernel.perf_event_paranoid=2) or grant CAP_PERFMON to perf; GitView does not change kernel settings", c.Paranoid)
			return c
		case c.Paranoid == 2:
			c.UserOnly = true
			c.Warnings = append(c.Warnings, "kernel.perf_event_paranoid=2: only user-space CPU is sampled; time the kernel spends in system calls of the process is in the /proc totals but not in the perf samples.")
		}
	}
	if !c.UserOnly && c.KptrRestrict > 0 && !c.Privileged {
		c.Warnings = append(c.Warnings, "kernel.kptr_restrict hides kernel addresses: kernel frames are not symbolized.")
	}
	c.Available = true
	return c
}

func firstLine(out []byte, err error) string {
	s := strings.TrimSpace(string(out))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if s == "" && err != nil {
		s = err.Error()
	}
	return s
}

func readInt(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(b)))
}

// hasCap reports whether bit is set in CapEff of a status file.
func hasCap(status string, bit uint) bool {
	data, err := os.ReadFile(status)
	if err != nil {
		return false
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "CapEff:"); ok {
			n, err := strconv.ParseUint(strings.TrimSpace(v), 16, 64)
			return err == nil && n&(1<<bit) != 0
		}
	}
	return false
}
