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
	p := filepath.Join(dir, fmt.Sprintf("vktui-mpv-%d.sock", os.Getpid()))
	if len(p) > 100 { // unix socket paths are limited to 104 bytes on macOS
		p = filepath.Join("/tmp", fmt.Sprintf("vktui-mpv-%d-%d.sock", os.Getuid(), os.Getpid()))
	}
	return p
}

func dialIPC(path string) (net.Conn, error) { return net.Dial("unix", path) }

func cleanupIPC(path string) { _ = os.Remove(path) }

// mpv stays in our process group, so closing the terminal (SIGHUP) stops it too.
// If we are killed hard it must die too: Linux asks the kernel (Pdeathsig),
// macOS/BSD use a watchdog (see watchParent).
func prepareCmd(cmd *exec.Cmd) { setDeathSig(cmd) }

func afterStart(cmd *exec.Cmd) { watchParent(cmd) }
