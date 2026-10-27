//go:build !linux

package linuxperf

import "syscall"

func sysProcAttr() *syscall.SysProcAttr { return nil }

// Monotonic is unavailable outside Linux (perf is Linux only).
func Monotonic() int64 { return 0 }
