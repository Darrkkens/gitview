//go:build !unix

package run

import "os/exec"

func prepare(cmd *exec.Cmd)   {}
func killGroup(cmd *exec.Cmd) {}
