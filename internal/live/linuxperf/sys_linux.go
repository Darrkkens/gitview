//go:build linux

package linuxperf

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// sysProcAttr puts perf in its own process group (a Ctrl+C in the terminal
// reaches GitView, which stops perf itself) and kills it if GitView dies.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
}

// Monotonic reads CLOCK_MONOTONIC in nanoseconds: the clock of perf's
// timestamps (perf record -k monotonic) and of Go's monotonic readings.
func Monotonic() int64 {
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts); err != nil {
		return 0
	}
	return ts.Nano()
}
