//go:build !linux && !windows

package player

import "os/exec"

func setDeathSig(*exec.Cmd) {}
