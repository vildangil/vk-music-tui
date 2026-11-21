// Package player drives an mpv process over its JSON IPC.
package player

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type State struct {
	Pos, Dur float64
	Paused   bool
	Volume   float64
	Idle     bool
}

// EndEvent is sent when a file stops playing. Reason: eof, stop, quit, error, redirect.
type EndEvent struct {
	Reason string
	Err    string
}

// Silent placeholder playlist entries. When the OS overlay's prev/next button
// makes mpv switch to one of them we report it as a media key and load the real track.
const (
	phNext = "av://lavfi:anullsrc=r=44100:d=3600"
	phPrev = "av://lavfi:anullsrc=r=48000:d=3600"
)

// MediaKeyEvent is sent when the user presses a media key or a button in the
// Windows/macOS media overlay: "next", "prev", "playpause".
type MediaKeyEvent struct{ Action string }

// inputConf routes media keys to us instead of mpv's own (empty) playlist.
const inputConf = "NEXT script-message vktui next\n" +
	"PREV script-message vktui prev\n" +
	"MBTN_FORWARD script-message vktui next\n" +
	"MBTN_BACK script-message vktui prev\n" +
	"PLAYPAUSE script-message vktui playpause\n" +
	"PLAY script-message vktui playpause\n" +
	"PAUSE script-message vktui playpause\n"

func writeInputConf() string {
	f := filepath.Join(os.TempDir(), fmt.Sprintf("vktui-input-%d.conf", os.Getpid()))
	if os.WriteFile(f, []byte(inputConf), 0o600) != nil {
		return ""
	}
	return f
}

// DiedEvent is sent if the mpv process disappears.
type DiedEvent struct{ Err error }

type Player struct {
	Path   string // mpv binary
	cmd    *exec.Cmd
	conn   net.Conn
	sock   string
	wmu    sync.Mutex
	mu     sync.Mutex
	st     State
	Events chan any
	once   sync.Once
	done   chan struct{} // closed when mpv exits
	quit   bool

	eofSent bool
	reqID   int
	pending map[int]chan reply
}

// Find locates the mpv binary.
func Find(hint string) (string, error) {
	var cands []string
	if hint != "" {
		cands = append(cands, hint)
	}
	if e := os.Getenv("VKTUI_MPV"); e != "" {
		cands = append(cands, e)
	}
	name := "mpv"
	if runtime.GOOS == "windows" {
		name = "mpv.exe"
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		cands = append(cands, filepath.Join(dir, name), filepath.Join(dir, "mpv", name))
	}
	for _, c := range cands {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, nil
		}
	}
	if p, err := exec.LookPath("mpv"); err == nil {
		return p, nil
	}
	var extra []string
	switch runtime.GOOS {
	case "windows":
		la := os.Getenv("LOCALAPPDATA")
		up := os.Getenv("USERPROFILE")
		extra = []string{
			filepath.Join(la, "Microsoft", "WinGet", "Links", name),
			filepath.Join(up, "scoop", "shims", name),
			filepath.Join(up, "scoop", "apps", "mpv", "current", name),
			`C:\Program Files\MPV Player\mpv.exe`, // winget shinchiro.mpv
			`C:\Program Files\mpv\mpv.exe`,
			filepath.Join(la, "Programs", "mpv", name),
			`C:\ProgramData\chocolatey\bin\mpv.exe`,
		}
		// winget's shinchiro.mpv installs into Packages\shinchiro.mpv_*\
		if m, _ := filepath.Glob(filepath.Join(la, "Microsoft", "WinGet", "Packages", "shinchiro.mpv*", "*", name)); len(m) > 0 {
			extra = append(extra, m...)
		}
		if m, _ := filepath.Glob(filepath.Join(la, "Microsoft", "WinGet", "Packages", "shinchiro.mpv*", name)); len(m) > 0 {
			extra = append(extra, m...)
		}
	case "darwin":
		extra = []string{"/opt/homebrew/bin/mpv", "/usr/local/bin/mpv", "/Applications/mpv.app/Contents/MacOS/mpv"}
	default:
		extra = []string{"/usr/bin/mpv", "/usr/local/bin/mpv", "/var/lib/flatpak/exports/bin/io.mpv.Mpv"}
	}
	for _, c := range extra {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, nil
		}
	}
	return "", errors.New("mpv не найден")
}

func Start(mpvPath string, volume int) (*Player, error) {
	sock := ipcPath()
	args := []string{
		"--idle=yes",
		"--no-video",
		"--no-terminal",
		"--no-config",
		"--input-ipc-server=" + sock,
		fmt.Sprintf("--volume=%d", volume),
		"--volume-max=150",
		"--ytdl=no",
		"--audio-display=no",
		"--cache=yes",
		"--demuxer-max-bytes=16MiB",
		"--demuxer-max-back-bytes=4MiB",
		"--network-timeout=20",
		"--keep-open=always", // we detect the end via eof-reached and pick the next track ourselves
		"--gapless-audio=weak",
		"--media-controls=yes", // show the track in the OS media overlay (mpv ≥ 0.38)
		"--title=VK Music",
	}
	if ic := writeInputConf(); ic != "" {
		args = append(args, "--input-conf="+ic)
	}
	p, err := start(mpvPath, sock, args, volume)
	if err != nil && strings.Contains(err.Error(), "сразу после запуска") {
		// Older mpv doesn't know --media-controls: retry without it.
		var a2 []string
		for _, a := range args {
			if !strings.HasPrefix(a, "--media-controls") {
				a2 = append(a2, a)
			}
		}
		p, err = start(mpvPath, sock, a2, volume)
	}
	return p, err
}

func start(mpvPath, sock string, args []string, volume int) (*Player, error) {
	cmd := exec.Command(mpvPath, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	prepareCmd(cmd)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("не удалось запустить mpv: %w", err)
	}
	afterStart(cmd)
	p := &Player{Path: mpvPath, cmd: cmd, sock: sock, Events: make(chan any, 32), st: State{Volume: float64(volume), Idle: true}, done: make(chan struct{})}

	var waitErr error
	go func() { waitErr = cmd.Wait(); close(p.done) }()

	deadline := time.Now().Add(8 * time.Second)
	for {
		c, err := dialIPC(sock)
		if err == nil {
			p.conn = c
			break
		}
		select {
		case <-p.done:
			return nil, fmt.Errorf("mpv завершился сразу после запуска: %v", waitErr)
		default:
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			return nil, fmt.Errorf("не удалось подключиться к mpv: %w", err)
		}
		time.Sleep(60 * time.Millisecond)
	}
	go func() {
		<-p.done
		p.mu.Lock()
		q := p.quit
		p.mu.Unlock()
		if !q {
			p.emit(DiedEvent{Err: waitErr})
		}
	}()
	go p.readLoop()
	for i, prop := range []string{"time-pos", "duration", "pause", "volume", "idle-active", "eof-reached", "path"} {
		p.send("observe_property", i+1, prop)
	}
	return p, nil
}

func (p *Player) emit(ev any) {
	select {
	case p.Events <- ev:
	default:
	}
}

func (p *Player) readLoop() {
	sc := bufio.NewScanner(p.conn)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var m struct {
			Event     string          `json:"event"`
			Name      string          `json:"name"`
			Data      json.RawMessage `json:"data"`
			Reason    string          `json:"reason"`
			FileError string          `json:"file_error"`
			Args      []string        `json:"args"`
			RequestID int             `json:"request_id"`
			Error     string          `json:"error"`
		}
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		if m.Event == "" && m.RequestID != 0 {
			p.mu.Lock()
			ch := p.pending[m.RequestID]
			delete(p.pending, m.RequestID)
			p.mu.Unlock()
			if ch != nil {
				ch <- reply{m.Error, m.Data}
			}
			continue
		}
		switch m.Event {
		case "property-change":
			p.mu.Lock()
			switch m.Name {
			case "time-pos":
				var f float64
				if json.Unmarshal(m.Data, &f) == nil {
					p.st.Pos = f
				} else {
					p.st.Pos = 0
				}
			case "duration":
				var f float64
				if json.Unmarshal(m.Data, &f) == nil {
					p.st.Dur = f
				} else {
					p.st.Dur = 0
				}
			case "pause":
				_ = json.Unmarshal(m.Data, &p.st.Paused)
			case "volume":
				_ = json.Unmarshal(m.Data, &p.st.Volume)
			case "idle-active":
				_ = json.Unmarshal(m.Data, &p.st.Idle)
			case "path":
				var path string
				_ = json.Unmarshal(m.Data, &path)
				switch path {
				case phNext:
					go p.emit(MediaKeyEvent{Action: "next"})
				case phPrev:
					go p.emit(MediaKeyEvent{Action: "prev"})
				}
			case "eof-reached":
				var eof bool
				_ = json.Unmarshal(m.Data, &eof)
				if eof && !p.eofSent {
					p.eofSent = true
					go p.emit(EndEvent{Reason: "eof"})
				}
			}
			p.mu.Unlock()
		case "end-file":
			if m.Reason != "eof" { // eof is reported via eof-reached
				p.emit(EndEvent{Reason: m.Reason, Err: m.FileError})
			}
		case "client-message":
			if len(m.Args) == 2 && m.Args[0] == "vktui" {
				p.emit(MediaKeyEvent{Action: m.Args[1]})
			}
		}
	}
}

func (p *Player) send(args ...any) {
	b, _ := json.Marshal(map[string]any{"command": args})
	b = append(b, '\n')
	p.wmu.Lock()
	defer p.wmu.Unlock()
	if p.conn != nil {
		_ = p.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
		_, _ = p.conn.Write(b)
	}
}

func (p *Player) State() State {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.st
}

// edlEsc quotes a value for mpv's EDL format: %<byte length>%<text>.
func edlEsc(s string) string { return fmt.Sprintf("%%%d%%%s", len(s), s) }

// withTags wraps url in an EDL document that carries title/artist tags, so the
// OS media overlay shows the artist separately (VK streams have no metadata).
func withTags(url, title, artist, album string) string {
	if strings.Contains(url, "://") && !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return url // generated sources (av://, tests) play as is; local files and http get tags
	}
	return "edl://!global_tags,title=" + edlEsc(title) + ",artist=" + edlEsc(artist) + ",album=" + edlEsc(album) + ";" + edlEsc(url)
}

// Load starts playing url; title and artist are what the OS media overlay shows.
func (p *Player) Load(url, title, artist, album string) {
	p.mu.Lock()
	p.st.Pos, p.st.Dur = 0, 0
	p.eofSent = false
	p.mu.Unlock()
	p.send("set_property", "force-media-title", title)
	p.send("loadfile", withTags(url, title, artist, album), "replace")
	// Placeholder neighbours make the OS overlay enable its prev/next buttons.
	// They never play: keep-open=always stops at the end of the real track.
	p.send("loadfile", phNext, "append")
	p.send("loadfile", phPrev, "insert-at", 0)
	p.send("set_property", "pause", false)
}

type reply struct {
	err  string
	data json.RawMessage
}

// command sends a command and waits for mpv's reply.
func (p *Player) command(args ...any) error {
	_, err := p.request(args...)
	return err
}

// GetProperty reads an mpv property as raw JSON.
func (p *Player) GetProperty(name string) (json.RawMessage, error) {
	return p.request("get_property", name)
}

func (p *Player) request(args ...any) (json.RawMessage, error) {
	p.mu.Lock()
	if p.pending == nil {
		p.pending = map[int]chan reply{}
	}
	p.reqID++
	id := p.reqID
	ch := make(chan reply, 1)
	p.pending[id] = ch
	p.mu.Unlock()

	b, _ := json.Marshal(map[string]any{"command": args, "request_id": id})
	b = append(b, '\n')
	p.wmu.Lock()
	if p.conn != nil {
		_ = p.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
		_, _ = p.conn.Write(b)
	}
	p.wmu.Unlock()

	select {
	case r := <-ch:
		if r.err != "" && r.err != "success" {
			return nil, errors.New(r.err)
		}
		return r.data, nil
	case <-time.After(3 * time.Second):
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		return nil, errors.New("mpv не ответил")
	}
}

// SetAudioFilters replaces mpv's audio filter chain (equalizer, normalization).
// filters is an mpv --af list, e.g. "lavfi=[equalizer=f=60:t=o:w=1:g=5]". Empty clears it.
func (p *Player) SetAudioFilters(filters string) error {
	return p.command("set_property", "af", filters)
}

func (p *Player) TogglePause()        { p.send("cycle", "pause") }
func (p *Player) Seek(d float64)      { p.send("seek", d, "relative") }
func (p *Player) SeekAbs(s float64)   { p.send("seek", s, "absolute") }
func (p *Player) SetVolume(v float64) { p.send("set_property", "volume", v) }
func (p *Player) Stop()               { p.send("stop") }

func (p *Player) Close() {
	p.once.Do(func() {
		p.mu.Lock()
		p.quit = true
		p.mu.Unlock()
		p.send("quit")
		select {
		case <-p.done:
		case <-time.After(1500 * time.Millisecond):
			_ = p.cmd.Process.Kill()
		}
		if p.conn != nil {
			_ = p.conn.Close()
		}
		cleanupIPC(p.sock)
		_ = os.Remove(filepath.Join(os.TempDir(), fmt.Sprintf("vktui-input-%d.conf", os.Getpid())))
	})
}
