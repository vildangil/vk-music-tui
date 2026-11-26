package player

import (
	"os/exec"
	"syscall"
)

func setDeathSig(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
}

// watchParent: Pdeathsig already kills the child with us.
func watchParent(*exec.Cmd) {}
