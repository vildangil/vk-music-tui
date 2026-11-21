package player

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestNoOrphan checks that mpv dies when the parent process is killed hard.
func TestNoOrphan(t *testing.T) {
	if os.Getenv("VKTUI_ORPHAN_CHILD") == "1" {
		mpv, err := Find("")
		if err != nil {
			os.Exit(3)
		}
		p, err := Start(mpv, 0)
		if err != nil {
			fmt.Println("ERR", err)
			os.Exit(4)
		}
		fmt.Println("PID", p.cmd.Process.Pid)
		time.Sleep(time.Minute)
		return
	}
	if _, err := Find(""); err != nil {
		t.Skip("no mpv")
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestNoOrphan")
	cmd.Env = append(os.Environ(), "VKTUI_ORPHAN_CHILD=1")
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(out)
	pid := 0
	for sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) == 2 && f[0] == "PID" {
			pid, _ = strconv.Atoi(f[1])
			break
		}
	}
	if pid == 0 {
		t.Fatal("child did not start mpv")
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	time.Sleep(1500 * time.Millisecond)
	if alive(pid) {
		t.Fatalf("mpv (pid %d) survived parent death", pid)
	}
}
