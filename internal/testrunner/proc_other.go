//go:build !unix

package testrunner

import "os/exec"

func prepareCommand(cmd *exec.Cmd) {}

func killProcessGroup(cmd *exec.Cmd) {}
