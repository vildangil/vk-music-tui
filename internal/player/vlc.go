package player

import (
	"bufio"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// VLC drives VLC through its "rc" (remote control) interface over a local TCP
// socket — works the same on Linux, macOS and Windows.
type VLC struct {
	bin    string
	events chan any
	cmd    *exec.Cmd
	conn   net.Conn
	done   chan struct{}

	wmu sync.Mutex
	mu  sync.Mutex
	st  State
	gen int // bumped by Load/Stop: answers to older queries are ignored
	// queries waiting for a numeric answer, in order (rc answers in order)
	queue      []vlcQuery
	loaded     bool // a track was handed to VLC and not stopped
	sawPlaying bool // VLC confirmed it plays the current track
	loadedAt   time.Time
	pausedAt   time.Time
	quit       bool
	stop       chan struct{}
}

type vlcQuery struct{ gen int }

// vlcVolume converts percent to VLC's rc scale (256 = 100%).
func vlcVolume(v float64) int { return int(v * 256 / 100) }

func StartVLC(bin string, volume int) (*VLC, error) {
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	cmd := exec.Command(bin,
		"-I", "rc", "--rc-host="+addr, "--rc-quiet",
		"--no-video", "--no-osd", "--quiet", "--no-loop", "--no-repeat", "--play-and-stop",
		"--no-media-library", "--no-playlist-autostart",
		"--http-reconnect", "--network-caching=1500",
		fmt.Sprintf("--volume=%d", vlcVolume(float64(volume))),
	)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	prepareCmd(cmd)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("не удалось запустить VLC: %w", err)
	}
	afterStart(cmd)
	v := &VLC{bin: bin, events: make(chan any, 32), cmd: cmd, done: make(chan struct{}), stop: make(chan struct{})}
	v.st = State{Volume: float64(volume), Idle: true}
	var waitErr error
	go func() { waitErr = cmd.Wait(); close(v.done) }()

	deadline := time.Now().Add(10 * time.Second)
	for {
		c, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			v.conn = c
			break
		}
		select {
		case <-v.done:
			return nil, fmt.Errorf("VLC завершился сразу после запуска: %v", waitErr)
		default:
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			return nil, fmt.Errorf("не удалось подключиться к VLC: %w", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	go func() {
		<-v.done
		v.mu.Lock()
		q := v.quit
		v.mu.Unlock()
		if !q {
			emitTo(v.events, DiedEvent{Err: waitErr})
		}
	}()
	go v.readLoop()
	go v.pollLoop()
	return v, nil
}

func (v *VLC) Name() string     { return "vlc" }
func (v *VLC) Binary() string   { return v.bin }
func (v *VLC) Events() chan any { return v.events }

func (v *VLC) send(lines ...string) {
	v.wmu.Lock()
	defer v.wmu.Unlock()
	if v.conn == nil {
		return
	}
	_ = v.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_, _ = v.conn.Write([]byte(strings.Join(lines, "\n") + "\n"))
}

// rcQuote makes a path or URL safe for an rc command line.
func rcQuote(s string) string {
	s = strings.NewReplacer("\n", "", "\r", "").Replace(s)
	if strings.Contains(s, "://") {
		return s
	}
	return "file://" + strings.ReplaceAll(s, " ", "%20")
}

// One poll sends "is_playing", "get_time", "get_length", "status". is_playing
// always answers; the others may print nothing when VLC is idle, so answers are
// collected per block and the "( state … )" line of "status" closes the block.
func (v *VLC) readLoop() {
	sc := bufio.NewScanner(v.conn)
	sc.Buffer(make([]byte, 16*1024), 1024*1024)
	var nums []float64
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		for strings.HasPrefix(line, ">") { // prompt
			line = strings.TrimSpace(strings.TrimPrefix(line, ">"))
		}
		if n, err := strconv.ParseFloat(line, 64); err == nil {
			nums = append(nums, n)
			continue
		}
		if !strings.HasPrefix(line, "( state") {
			continue // other status lines, help text
		}
		paused := strings.Contains(line, "paused")
		v.mu.Lock()
		if len(v.queue) == 0 {
			v.mu.Unlock()
			nums = nil
			continue
		}
		q := v.queue[0]
		v.queue = v.queue[1:]
		ev := v.applyLocked(q, nums, paused)
		v.mu.Unlock()
		nums = nil
		if ev != nil {
			emitTo(v.events, ev)
		}
	}
}

// applyLocked updates the state from one poll's answers.
func (v *VLC) applyLocked(q vlcQuery, nums []float64, paused bool) any {
	if q.gen != v.gen || !v.loaded || len(nums) == 0 {
		return nil
	}
	if nums[0] == 1 {
		v.sawPlaying, v.st.Idle = true, false
		if len(nums) > 1 && (!v.st.Paused || nums[1] > 0) {
			v.st.Pos = nums[1]
		}
		if len(nums) > 2 && nums[2] > 0 {
			v.st.Dur = nums[2]
		}
		if paused != v.st.Paused && time.Since(v.pausedAt) > time.Second {
			v.st.Paused = paused
		}
		return nil
	}
	if v.st.Paused || paused {
		return nil // some VLC versions say "not playing" while paused
	}
	if v.sawPlaying {
		v.loaded, v.st.Idle = false, true
		return EndEvent{Reason: "eof"}
	}
	if time.Since(v.loadedAt) > 25*time.Second {
		v.loaded, v.st.Idle = false, true
		return EndEvent{Reason: "error", Err: "VLC не смог открыть трек"}
	}
	return nil
}

func (v *VLC) pollLoop() {
	t := time.NewTicker(400 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-v.stop:
			return
		case <-v.done:
			return
		case <-t.C:
		}
		v.mu.Lock()
		if !v.loaded {
			v.mu.Unlock()
			continue
		}
		if len(v.queue) > 10 { // VLC stopped answering the way we expect: start over
			v.queue = nil
		}
		v.queue = append(v.queue, vlcQuery{gen: v.gen})
		v.mu.Unlock()
		v.send("is_playing", "get_time", "get_length", "status")
	}
}

func (v *VLC) Load(url, title, artist, album string) {
	v.mu.Lock()
	v.gen++
	v.loaded, v.sawPlaying, v.loadedAt = true, false, time.Now()
	v.st.Pos, v.st.Dur, v.st.Paused, v.st.Idle = 0, 0, false, false
	v.mu.Unlock()
	v.send("stop", "clear", "add "+rcQuote(url))
}

func (v *VLC) TogglePause() {
	v.mu.Lock()
	if !v.loaded {
		v.mu.Unlock()
		return
	}
	v.st.Paused = !v.st.Paused
	v.pausedAt = time.Now()
	v.mu.Unlock()
	v.send("pause")
}

func (v *VLC) SeekAbs(s float64) {
	if s < 0 {
		s = 0
	}
	v.mu.Lock()
	v.st.Pos = s
	v.mu.Unlock()
	v.send(fmt.Sprintf("seek %d", int(s)))
}

func (v *VLC) Seek(d float64) {
	v.mu.Lock()
	p := v.st.Pos + d
	v.mu.Unlock()
	v.SeekAbs(p)
}

func (v *VLC) SetVolume(x float64) {
	v.mu.Lock()
	v.st.Volume = x
	v.mu.Unlock()
	v.send(fmt.Sprintf("volume %d", vlcVolume(x)))
}

func (v *VLC) SetAudioFilters(filters string) error {
	if strings.TrimSpace(filters) == "" {
		return nil
	}
	return fmt.Errorf("эквалайзер: %w (нужен mpv или ffplay)", ErrUnsupported)
}

func (v *VLC) Stop() {
	v.mu.Lock()
	v.gen++
	v.loaded = false
	v.st = State{Volume: v.st.Volume, Idle: true}
	v.mu.Unlock()
	v.send("stop", "clear")
}

func (v *VLC) State() State {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.st
}

func (v *VLC) Close() {
	v.mu.Lock()
	if v.quit {
		v.mu.Unlock()
		return
	}
	v.quit = true
	close(v.stop)
	v.mu.Unlock()
	v.send("stop", "quit", "shutdown")
	select {
	case <-v.done:
	case <-time.After(1500 * time.Millisecond):
		_ = v.cmd.Process.Kill()
	}
	if v.conn != nil {
		_ = v.conn.Close()
	}
}
