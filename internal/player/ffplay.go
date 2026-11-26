package player

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// FFplay plays through ffplay (part of FFmpeg). ffplay has no control channel
// without a window, so every pause / seek / volume change restarts it at the
// current position. One process per track.
type FFplay struct {
	bin    string
	events chan any

	mu     sync.Mutex
	cmd    *exec.Cmd
	gen    int // bumped on every (re)start; exits of older processes are ignored
	url    string
	title  string
	af     string
	vol    float64
	st     State
	start  float64 // stream start time reported by ffplay ("start:")
	gotPos bool    // position comes from -stats output
	t0     time.Time
	offset float64 // where the current process started
	volTmr *time.Timer
	closed bool
}

func StartFFplay(bin string, volume int) (*FFplay, error) {
	if out, err := exec.Command(bin, "-version").Output(); err != nil || !bytes.Contains(out, []byte("ffplay")) {
		return nil, fmt.Errorf("%s не похож на ffplay: %v", bin, err)
	}
	f := &FFplay{bin: bin, events: make(chan any, 32), vol: float64(volume)}
	f.st = State{Volume: float64(volume), Idle: true}
	return f, nil
}

func (f *FFplay) Name() string     { return "ffplay" }
func (f *FFplay) Binary() string   { return f.bin }
func (f *FFplay) Events() chan any { return f.events }

func (f *FFplay) args(pos float64) []string {
	a := []string{"-nodisp", "-autoexit", "-hide_banner", "-loglevel", "info", "-stats",
		"-infbuf", "-volume", "100", "-window_title", "VK Music"}
	chain := []string{fmt.Sprintf("volume=%.2f", f.vol/100)}
	if c := lavfiChain(f.af); c != "" {
		chain = append(chain, c)
	}
	a = append(a, "-af", strings.Join(chain, ","))
	if pos > 0.5 {
		a = append(a, "-ss", strconv.FormatFloat(pos, 'f', 2, 64))
	}
	return append(a, "-i", f.url)
}

// launch (re)starts ffplay at pos. Caller holds f.mu.
func (f *FFplay) launch(pos float64) {
	f.killLocked()
	if f.url == "" || f.closed {
		return
	}
	f.gen++
	gen := f.gen
	cmd := exec.Command(f.bin, f.args(pos)...)
	stderr, _ := cmd.StderrPipe()
	prepareCmd(cmd)
	if err := cmd.Start(); err != nil {
		go emitTo(f.events, EndEvent{Reason: "error", Err: err.Error()})
		return
	}
	afterStart(cmd)
	f.cmd = cmd
	f.offset, f.t0, f.gotPos = pos, time.Now(), false
	f.st.Pos, f.st.Idle, f.st.Paused = pos, false, false
	go f.watch(cmd, stderr, gen)
}

var (
	ffDurRe  = regexp.MustCompile(`Duration:\s*(\d+):(\d+):(\d+(?:\.\d+)?)(?:,\s*start:\s*(-?\d+(?:\.\d+)?))?`)
	ffStatRe = regexp.MustCompile(`^\s*(-?\d+(?:\.\d+)?)\s+(?:M-A|A-V|M-V)`)
)

// scanCRLF splits on \r or \n: ffplay redraws its status line with \r.
func scanCRLF(data []byte, atEOF bool) (int, []byte, error) {
	for i, b := range data {
		if b == '\r' || b == '\n' {
			return i + 1, data[:i], nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

func (f *FFplay) watch(cmd *exec.Cmd, stderr io.Reader, gen int) {
	var lastErr string
	if stderr != nil {
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 16*1024), 1024*1024)
		sc.Split(scanCRLF)
		for sc.Scan() {
			line := sc.Text()
			f.mu.Lock()
			if gen == f.gen {
				if m := ffDurRe.FindStringSubmatch(line); m != nil {
					h, _ := strconv.ParseFloat(m[1], 64)
					mi, _ := strconv.ParseFloat(m[2], 64)
					s, _ := strconv.ParseFloat(m[3], 64)
					f.st.Dur = h*3600 + mi*60 + s
					if m[4] != "" {
						f.start, _ = strconv.ParseFloat(m[4], 64)
					}
				} else if m := ffStatRe.FindStringSubmatch(line); m != nil {
					if v, err := strconv.ParseFloat(m[1], 64); err == nil {
						if p := v - f.start; p >= 0 {
							f.st.Pos, f.gotPos = p, true
						}
					}
				} else if t := strings.TrimSpace(line); t != "" && looksLikeError(t) {
					lastErr = t
				}
			}
			f.mu.Unlock()
		}
	}
	err := cmd.Wait()
	f.mu.Lock()
	current := gen == f.gen
	if current {
		f.cmd = nil
		f.st.Idle = true
	}
	f.mu.Unlock()
	if !current {
		return // we killed it ourselves (seek, pause, next track)
	}
	if err != nil {
		if lastErr == "" {
			lastErr = err.Error()
		}
		emitTo(f.events, EndEvent{Reason: "error", Err: lastErr})
		return
	}
	emitTo(f.events, EndEvent{Reason: "eof"})
}

func looksLikeError(s string) bool {
	l := strings.ToLower(s)
	return strings.Contains(l, "error") || strings.Contains(l, "failed") || strings.Contains(l, "invalid") ||
		strings.Contains(l, "not found") || strings.Contains(l, "refused") || strings.Contains(l, "forbidden")
}

// killLocked stops the running process; its exit is ignored. Caller holds f.mu.
func (f *FFplay) killLocked() {
	f.gen++
	if f.cmd != nil && f.cmd.Process != nil {
		_ = f.cmd.Process.Kill()
	}
	f.cmd = nil
}

// posLocked is the current position: from -stats, else by the clock.
func (f *FFplay) posLocked() float64 {
	if f.st.Paused || f.st.Idle || f.gotPos {
		return f.st.Pos
	}
	return f.offset + time.Since(f.t0).Seconds()
}

func (f *FFplay) Load(url, title, artist, album string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.url, f.title = url, title
	f.st.Dur, f.start = 0, 0
	f.launch(0)
}

func (f *FFplay) TogglePause() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.url == "" {
		return
	}
	if f.st.Paused {
		f.launch(f.st.Pos)
		return
	}
	pos := f.posLocked()
	f.killLocked()
	f.st.Pos, f.st.Paused = pos, true
}

func (f *FFplay) seekTo(pos float64) {
	if pos < 0 {
		pos = 0
	}
	if f.st.Dur > 0 && pos > f.st.Dur-1 {
		pos = f.st.Dur - 1
	}
	if f.st.Paused {
		f.st.Pos = pos
		return
	}
	f.launch(pos)
}

func (f *FFplay) Seek(d float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.url != "" {
		f.seekTo(f.posLocked() + d)
	}
}

func (f *FFplay) SeekAbs(s float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.url != "" {
		f.seekTo(s)
	}
}

// SetVolume restarts playback after the user stops pressing +/- for a moment.
func (f *FFplay) SetVolume(v float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.vol, f.st.Volume = v, v
	if f.volTmr != nil {
		f.volTmr.Stop()
	}
	f.volTmr = time.AfterFunc(400*time.Millisecond, func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.cmd != nil && !f.st.Paused {
			f.launch(f.posLocked())
		}
	})
}

func (f *FFplay) SetAudioFilters(filters string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if filters == f.af {
		return nil
	}
	f.af = filters
	if f.cmd != nil && !f.st.Paused {
		f.launch(f.posLocked())
	}
	return nil
}

func (f *FFplay) Stop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.killLocked()
	f.url = ""
	f.st = State{Volume: f.vol, Idle: true}
}

func (f *FFplay) State() State {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.st
	s.Pos = f.posLocked()
	if s.Dur > 0 && s.Pos > s.Dur {
		s.Pos = s.Dur
	}
	return s
}

func (f *FFplay) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.killLocked()
	f.closed = true
	if f.volTmr != nil {
		f.volTmr.Stop()
	}
}
