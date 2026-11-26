package player

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MPlayer drives MPlayer in slave mode (commands on stdin, answers on stdout).
type MPlayer struct {
	bin    string
	events chan any
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	done   chan struct{}
	stop   chan struct{}

	wmu sync.Mutex
	mu  sync.Mutex
	st  State
	gen int
	// polls in flight; each ends with the answer to get_property volume
	polls      []int
	nums       map[string]float64
	loaded     bool
	sawPlaying bool
	loadedAt   time.Time
	quit       bool
}

func StartMPlayer(bin string, volume int) (*MPlayer, error) {
	cmd := exec.Command(bin, "-slave", "-idle", "-quiet", "-nolirc", "-noconsolecontrols",
		"-vo", "null", "-novideo", "-softvol", "-softvol-max", "150",
		"-cache", "4096", "-cache-min", "5",
		"-volume", strconv.Itoa(volume*100/150))
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	prepareCmd(cmd)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("не удалось запустить MPlayer: %w", err)
	}
	afterStart(cmd)
	m := &MPlayer{bin: bin, events: make(chan any, 32), cmd: cmd, stdin: in, done: make(chan struct{}), stop: make(chan struct{}), nums: map[string]float64{}}
	m.st = State{Volume: float64(volume), Idle: true}
	var waitErr error
	go func() { waitErr = cmd.Wait(); close(m.done) }()
	go func() {
		<-m.done
		m.mu.Lock()
		q := m.quit
		m.mu.Unlock()
		if !q {
			emitTo(m.events, DiedEvent{Err: waitErr})
		}
	}()
	go m.readLoop(out)
	go m.pollLoop()
	return m, nil
}

func (m *MPlayer) Name() string     { return "mplayer" }
func (m *MPlayer) Binary() string   { return m.bin }
func (m *MPlayer) Events() chan any { return m.events }

func (m *MPlayer) send(lines ...string) {
	m.wmu.Lock()
	defer m.wmu.Unlock()
	_, _ = io.WriteString(m.stdin, strings.Join(lines, "\n")+"\n")
}

// mpQuote quotes a file name for "loadfile".
func mpQuote(s string) string {
	s = strings.NewReplacer("\n", "", "\r", "", `\`, `\\`, `"`, `\"`).Replace(s)
	return `"` + s + `"`
}

// One poll asks for time_pos, length and pause, then volume. volume always
// answers, so it closes the poll; missing answers mean "nothing is playing".
func (m *MPlayer) readLoop(out io.Reader) {
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 16*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "ANS_") {
			continue
		}
		k, val, ok := strings.Cut(strings.TrimPrefix(line, "ANS_"), "=")
		if !ok {
			continue
		}
		var n float64
		switch val {
		case "yes":
			n = 1
		case "no":
			n = 0
		default:
			f, err := strconv.ParseFloat(strings.Trim(val, "'"), 64)
			if err != nil {
				continue // ANS_ERROR=PROPERTY_UNAVAILABLE etc.
			}
			n = f
		}
		m.mu.Lock()
		if k != "volume" {
			m.nums[k] = n
			m.mu.Unlock()
			continue
		}
		var ev any
		if len(m.polls) > 0 {
			g := m.polls[0]
			m.polls = m.polls[1:]
			ev = m.applyLocked(g)
		}
		m.nums = map[string]float64{}
		m.mu.Unlock()
		if ev != nil {
			emitTo(m.events, ev)
		}
	}
}

func (m *MPlayer) applyLocked(g int) any {
	if g != m.gen || !m.loaded {
		return nil
	}
	if pos, ok := m.nums["time_pos"]; ok {
		m.sawPlaying, m.st.Idle = true, false
		m.st.Pos = pos
		if l, ok := m.nums["length"]; ok && l > 0 {
			m.st.Dur = l
		}
		return nil
	}
	if m.st.Paused {
		return nil
	}
	if m.sawPlaying {
		m.loaded, m.st.Idle = false, true
		return EndEvent{Reason: "eof"}
	}
	if time.Since(m.loadedAt) > 25*time.Second {
		m.loaded, m.st.Idle = false, true
		return EndEvent{Reason: "error", Err: "MPlayer не смог открыть трек"}
	}
	return nil
}

func (m *MPlayer) pollLoop() {
	t := time.NewTicker(400 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-m.done:
			return
		case <-t.C:
		}
		m.mu.Lock()
		if !m.loaded {
			m.mu.Unlock()
			continue
		}
		if len(m.polls) > 10 {
			m.polls = nil
		}
		m.polls = append(m.polls, m.gen)
		m.mu.Unlock()
		// pausing_keep_force: queries must not unpause
		m.send("pausing_keep_force get_property time_pos",
			"pausing_keep_force get_property length",
			"pausing_keep_force get_property volume")
	}
}

func (m *MPlayer) Load(url, title, artist, album string) {
	if strings.Contains(url, ".m3u8") && !strings.HasPrefix(url, "ffmpeg://") {
		url = "ffmpeg://" + url // HLS through libavformat
	}
	m.mu.Lock()
	m.gen++
	m.loaded, m.sawPlaying, m.loadedAt = true, false, time.Now()
	m.st.Pos, m.st.Dur, m.st.Paused, m.st.Idle = 0, 0, false, false
	m.mu.Unlock()
	m.send("loadfile " + mpQuote(url) + " 0")
}

func (m *MPlayer) TogglePause() {
	m.mu.Lock()
	if !m.loaded {
		m.mu.Unlock()
		return
	}
	m.st.Paused = !m.st.Paused
	m.mu.Unlock()
	m.send("pause")
}

func (m *MPlayer) prefix() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.st.Paused {
		return "pausing_keep "
	}
	return ""
}

func (m *MPlayer) Seek(d float64) {
	m.send(fmt.Sprintf("%sseek %.1f 0", m.prefix(), d))
}

func (m *MPlayer) SeekAbs(s float64) {
	if s < 0 {
		s = 0
	}
	m.mu.Lock()
	m.st.Pos = s
	m.mu.Unlock()
	m.send(fmt.Sprintf("%sseek %.1f 2", m.prefix(), s))
}

func (m *MPlayer) SetVolume(v float64) {
	m.mu.Lock()
	m.st.Volume = v
	m.mu.Unlock()
	// -softvol-max 150: MPlayer's 100 is our 150
	m.send(fmt.Sprintf("%svolume %.0f 1", m.prefix(), v*100/150))
}

func (m *MPlayer) SetAudioFilters(filters string) error {
	if strings.TrimSpace(filters) == "" {
		return nil
	}
	return fmt.Errorf("эквалайзер: %w (нужен mpv или ffplay)", ErrUnsupported)
}

func (m *MPlayer) Stop() {
	m.mu.Lock()
	m.gen++
	m.loaded = false
	m.st = State{Volume: m.st.Volume, Idle: true}
	m.mu.Unlock()
	m.send("stop")
}

func (m *MPlayer) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.st
}

func (m *MPlayer) Close() {
	m.mu.Lock()
	if m.quit {
		m.mu.Unlock()
		return
	}
	m.quit = true
	close(m.stop)
	m.mu.Unlock()
	m.send("quit")
	select {
	case <-m.done:
	case <-time.After(1500 * time.Millisecond):
		_ = m.cmd.Process.Kill()
	}
	_ = m.stdin.Close()
}
