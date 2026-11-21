//go:build windows

package player

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

func ipcPath() string { return fmt.Sprintf(`\\.\pipe\vktui-mpv-%d`, os.Getpid()) }

func dialIPC(path string) (net.Conn, error) {
	t := 500 * time.Millisecond
	return winio.DialPipe(path, &t)
}

func cleanupIPC(string) {}

func prepareCmd(cmd *exec.Cmd) {
	// CREATE_NO_WINDOW: don't flash a console window for mpv.
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}

var job windows.Handle

// afterStart puts mpv into a job object that is killed when vktui exits
// (including when the terminal window is closed).
func afterStart(cmd *exec.Cmd) {
	if job == 0 {
		h, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			return
		}
		info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
		info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
		if _, err := windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
			windows.CloseHandle(h)
			return
		}
		job = h
	}
	ph, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(ph)
	_ = windows.AssignProcessToJobObject(job, ph)
}
