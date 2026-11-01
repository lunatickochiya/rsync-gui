//go:build !unix

package rsync

import (
	"os"
	"os/exec"
)

func setProcessGroup(cmd *exec.Cmd) {}

func interruptProcess(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Signal(os.Interrupt)
	}
}

func killProcess(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
