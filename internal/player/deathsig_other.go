//go:build !linux && !windows

package player

import (
	"fmt"
	"os"
	"os/exec"
)

func setDeathSig(*exec.Cmd) {}

// watchParent covers what Linux does with Pdeathsig: macOS/BSD have no way to
// make a child die with its parent, so if vktui is killed hard (Force Quit,
// kill -9, Terminal crash) the player would keep playing on its own. A tiny
// shell loop checks every second that vktui is alive, kills the player when it
// isn't, and exits by itself when the player does.
func watchParent(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	script := fmt.Sprintf(`while kill -0 %[2]d 2>/dev/null; do
  if ! kill -0 %[1]d 2>/dev/null; then kill %[2]d 2>/dev/null; sleep 2; kill -9 %[2]d 2>/dev/null; exit 0; fi
  sleep 1
done`, os.Getpid(), cmd.Process.Pid)
	w := exec.Command("/bin/sh", "-c", script)
	w.Stdin, w.Stdout, w.Stderr = nil, nil, nil
	if w.Start() == nil {
		go func() { _ = w.Wait() }() // reap it when it exits with the player
	}
}
