package player

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// Engine is an audio backend: mpv (default, full featured), ffplay, VLC or MPlayer.
// All engines report the same events: EndEvent, MediaKeyEvent, DiedEvent.
type Engine interface {
	Name() string   // "mpv", "ffplay", "vlc", "mplayer"
	Binary() string // path of the program that plays
	Load(url, title, artist, album string)
	TogglePause()
	Seek(d float64)
	SeekAbs(s float64)
	SetVolume(v float64)
	Stop()
	State() State
	// SetAudioFilters takes an mpv --af list (lavfi=[...] parts); engines
	// without filter support return ErrUnsupported for a non-empty list.
	SetAudioFilters(filters string) error
	Events() chan any
	Close()
}

// ErrUnsupported is returned for features the chosen engine can't do.
var ErrUnsupported = errors.New("не поддерживается этим движком")

// EngineInfo describes an engine vktui knows how to drive.
type EngineInfo struct {
	Name  string // config value
	Title string // human readable
	Note  string // what is missing compared to mpv
	bins  []string
	extra map[string][]string // per-OS well-known install paths
}

// Engines lists the supported backends, best first ("auto" picks the first found).
var Engines = []EngineInfo{
	{
		Name: "mpv", Title: "mpv", Note: "всё: эквалайзер, медиаклавиши, плавная перемотка",
		bins: []string{"mpv"},
		extra: map[string][]string{
			"darwin":  {"/opt/homebrew/bin/mpv", "/usr/local/bin/mpv", "/opt/local/bin/mpv", "/Applications/mpv.app/Contents/MacOS/mpv"},
			"linux":   {"/usr/bin/mpv", "/usr/local/bin/mpv", "/var/lib/flatpak/exports/bin/io.mpv.Mpv"},
			"freebsd": {"/usr/local/bin/mpv"},
		},
	},
	{
		Name: "vlc", Title: "VLC", Note: "без эквалайзера",
		bins: []string{"cvlc", "vlc"},
		extra: map[string][]string{
			"darwin":  {"/Applications/VLC.app/Contents/MacOS/VLC", "/opt/homebrew/bin/vlc", "/usr/local/bin/vlc"},
			"linux":   {"/usr/bin/cvlc", "/usr/bin/vlc", "/snap/bin/vlc", "/var/lib/flatpak/exports/bin/org.videolan.VLC"},
			"windows": {`C:\Program Files\VideoLAN\VLC\vlc.exe`, `C:\Program Files (x86)\VideoLAN\VLC\vlc.exe`},
		},
	},
	{
		Name: "ffplay", Title: "FFmpeg (ffplay)", Note: "перемотка, пауза и громкость перезапускают поток",
		bins: []string{"ffplay"},
		extra: map[string][]string{
			"darwin":  {"/opt/homebrew/bin/ffplay", "/usr/local/bin/ffplay", "/opt/local/bin/ffplay"},
			"linux":   {"/usr/bin/ffplay", "/usr/local/bin/ffplay"},
			"windows": {`C:\ffmpeg\bin\ffplay.exe`, `C:\Program Files\ffmpeg\bin\ffplay.exe`},
		},
	},
	{
		Name: "mplayer", Title: "MPlayer", Note: "без эквалайзера",
		bins: []string{"mplayer"},
		extra: map[string][]string{
			"darwin":  {"/opt/homebrew/bin/mplayer", "/usr/local/bin/mplayer", "/opt/local/bin/mplayer"},
			"linux":   {"/usr/bin/mplayer", "/usr/local/bin/mplayer"},
			"windows": {`C:\Program Files\MPlayer\mplayer.exe`},
		},
	},
}

// EngineByName returns the engine description ("" if unknown).
func EngineByName(name string) (EngineInfo, bool) {
	for _, e := range Engines {
		if e.Name == strings.ToLower(strings.TrimSpace(name)) {
			return e, true
		}
	}
	return EngineInfo{}, false
}

func exeName(n string) string {
	if runtime.GOOS == "windows" {
		return n + ".exe"
	}
	return n
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// Locate finds the engine's program: next to vktui, in PATH, then well-known places.
func (e EngineInfo) Locate() (string, error) {
	var cands []string
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for _, b := range e.bins {
			cands = append(cands, filepath.Join(dir, exeName(b)), filepath.Join(dir, e.Name, exeName(b)))
		}
	}
	for _, c := range cands {
		if isFile(c) {
			return c, nil
		}
	}
	for _, b := range e.bins {
		if p, err := exec.LookPath(b); err == nil {
			return p, nil
		}
	}
	for _, c := range e.extra[runtime.GOOS] {
		if isFile(c) {
			return c, nil
		}
	}
	if runtime.GOOS == "windows" && e.Name == "ffplay" {
		la := os.Getenv("LOCALAPPDATA")
		if m, _ := filepath.Glob(filepath.Join(la, "Microsoft", "WinGet", "Packages", "Gyan.FFmpeg*", "*", "bin", "ffplay.exe")); len(m) > 0 {
			return m[0], nil
		}
		if p := filepath.Join(la, "Microsoft", "WinGet", "Links", "ffplay.exe"); isFile(p) {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s не найден", e.Title)
}

// Available lists engines that are installed, with their paths.
func Available() map[string]string {
	out := map[string]string{}
	for _, e := range Engines {
		if e.Name == "mpv" {
			if p, err := Find(""); err == nil {
				out["mpv"] = p
			}
			continue
		}
		if p, err := e.Locate(); err == nil {
			out[e.Name] = p
		}
	}
	return out
}

// Choose resolves the engine to use. name "" or "auto" picks the first installed one;
// path overrides where the program is (for the chosen engine).
func Choose(name, path string) (EngineInfo, string, error) {
	if env := os.Getenv("VKTUI_ENGINE"); env != "" {
		name = env
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name != "" && name != "auto" {
		e, ok := EngineByName(name)
		if !ok {
			return EngineInfo{}, "", fmt.Errorf("неизвестный движок %q (есть: mpv, vlc, ffplay, mplayer)", name)
		}
		if e.Name == "mpv" {
			p, err := Find(path)
			return e, p, err
		}
		if path != "" && isFile(path) {
			return e, path, nil
		}
		p, err := e.Locate()
		return e, p, err
	}
	if p, err := Find(path); err == nil {
		return Engines[0], p, nil
	}
	for _, e := range Engines[1:] {
		if p, err := e.Locate(); err == nil {
			return e, p, nil
		}
	}
	return EngineInfo{}, "", errors.New("не найден ни один звуковой движок (mpv, VLC, ffplay, MPlayer)")
}

// Open starts the engine.
func Open(e EngineInfo, path string, volume int) (Engine, error) {
	var (
		eng Engine
		err error
	)
	switch e.Name {
	case "ffplay":
		var f *FFplay
		f, err = StartFFplay(path, volume)
		eng = f
	case "vlc":
		var v *VLC
		v, err = StartVLC(path, volume)
		eng = v
	case "mplayer":
		var m *MPlayer
		m, err = StartMPlayer(path, volume)
		eng = m
	default:
		var p *Player
		p, err = Start(path, volume)
		eng = p
	}
	if err != nil {
		return nil, err
	}
	return eng, nil
}

// FindEncoder returns a program that can save a stream to mp3 for downloads:
// mpv (when it's the engine or installed) or ffmpeg.
func FindEncoder(eng Engine) string {
	if eng != nil && eng.Name() == "mpv" {
		return eng.Binary()
	}
	if p, err := Find(""); err == nil {
		return p
	}
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		return p
	}
	for _, c := range []string{"/opt/homebrew/bin/ffmpeg", "/usr/local/bin/ffmpeg", "/opt/local/bin/ffmpeg", "/usr/bin/ffmpeg"} {
		if isFile(c) {
			return c
		}
	}
	if eng != nil && eng.Name() == "ffplay" { // ffmpeg usually sits next to ffplay
		if p := filepath.Join(filepath.Dir(eng.Binary()), exeName("ffmpeg")); isFile(p) {
			return p
		}
	}
	return ""
}

var lavfiRe = regexp.MustCompile(`lavfi=\[([^\]]*)\]`)

// lavfiChain turns mpv's "lavfi=[a,b],lavfi=[c]" into the plain ffmpeg chain "a,b,c".
func lavfiChain(af string) string {
	var parts []string
	for _, m := range lavfiRe.FindAllStringSubmatch(af, -1) {
		if s := strings.TrimSpace(m[1]); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ",")
}

// freePort asks the OS for a free localhost TCP port.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func emitTo(ch chan any, ev any) {
	select {
	case ch <- ev:
	default:
	}
}
