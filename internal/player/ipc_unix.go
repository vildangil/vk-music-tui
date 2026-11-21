//go:build !windows

package player

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
)

func ipcPath() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, fmt.Sprintf("vktui-mpv-%d.sock", os.Getpid()))
}

func dialIPC(path string) (net.Conn, error) { return net.Dial("unix", path) }

func cleanupIPC(path string) { _ = os.Remove(path) }

// mpv stays in our process group, so closing the terminal (SIGHUP) stops it too.
// On Linux we additionally ask the kernel to kill it if we die.
func prepareCmd(cmd *exec.Cmd) { setDeathSig(cmd) }

func afterStart(*exec.Cmd) {}
