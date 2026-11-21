package player

import (
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

func alive(pid int) bool {
	p := strconv.Itoa(pid)
	if runtime.GOOS == "windows" {
		out, _ := exec.Command("tasklist", "/FI", "PID eq "+p, "/NH").Output()
		return strings.Contains(string(out), p)
	}
	return exec.Command("kill", "-0", p).Run() == nil
}
