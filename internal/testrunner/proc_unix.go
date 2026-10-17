//go:build unix

package testrunner

import (
	"os/exec"
	"syscall"
)

// prepareCommand runs the command in its own process group so cancellation
// reaches the test binaries `go test` and Node runners spawn.
func prepareCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGINT)
	}
}

// killProcessGroup removes any stragglers left in the group after exit.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
