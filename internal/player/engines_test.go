package player

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// The test binary doubles as fake ffplay / mplayer / vlc (FAKE_ENGINE=…), so the
// engine drivers are tested without the real programs installed.
func TestMain(m *testing.M) {
	switch os.Getenv("FAKE_ENGINE") {
	case "ffplay":
		fakeFFplay()
	case "mplayer":
		fakeMPlayer()
	case "vlc":
		fakeVLC()
	default:
		os.Exit(m.Run())
	}
	os.Exit(0)
}

const fakeLen = 2.0 // seconds of "audio" in every fake track

func fakeFFplay() {
	args := os.Args[1:]
	ss, url := 0.0, ""
	for i, a := range args {
		switch a {
		case "-version":
			fmt.Println("ffplay version fake")
			return
		case "-ss":
			fmt.Sscan(args[i+1], &ss)
		case "-i":
			url = args[i+1]
		}
	}
	if strings.Contains(url, "bad") {
		fmt.Fprintln(os.Stderr, url+": Server returned 403 Forbidden (access denied)")
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "Input #0, mp3, from '%s':\n  Duration: 00:00:02.00, start: 0.000000, bitrate: 320 kb/s\n", url)
	for p := ss; p < fakeLen; p += 0.1 {
		fmt.Fprintf(os.Stderr, "   %.2f M-A:  0.000 fd=   0 aq=   10KB vq=    0KB sq=    0B \r", p)
		time.Sleep(100 * time.Millisecond)
	}
}

type fakeClock struct {
	loaded  bool
	paused  bool
	started time.Time
	base    float64
}

func (c *fakeClock) pos() float64 {
	if !c.loaded {
		return -1
	}
	p := c.base
	if !c.paused {
		p += time.Since(c.started).Seconds()
	}
	if p >= fakeLen {
		return -1 // finished
	}
	return p
}

func (c *fakeClock) load()   { c.loaded, c.paused, c.base, c.started = true, false, 0, time.Now() }
func (c *fakeClock) toggle() { c.base = c.pos(); c.paused = !c.paused; c.started = time.Now() }

func fakeMPlayer() {
	var c fakeClock
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		f := strings.Fields(strings.TrimPrefix(strings.TrimPrefix(sc.Text(), "pausing_keep_force "), "pausing_keep "))
		if len(f) == 0 {
			continue
		}
		switch f[0] {
		case "loadfile":
			c.load()
		case "pause":
			c.toggle()
		case "stop":
			c.loaded = false
		case "quit":
			return
		case "get_property":
			switch f[1] {
			case "time_pos":
				if p := c.pos(); p >= 0 {
					fmt.Printf("ANS_time_pos=%.2f\n", p)
				} else {
					fmt.Println("ANS_ERROR=PROPERTY_UNAVAILABLE")
				}
			case "length":
				if c.pos() >= 0 {
					fmt.Printf("ANS_length=%.2f\n", fakeLen)
				}
			case "volume":
				fmt.Println("ANS_volume=50.000000")
			}
		}
	}
}

func fakeVLC() {
	addr := ""
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(a, "--rc-host=") {
			addr = strings.TrimPrefix(a, "--rc-host=")
		}
	}
	l, err := net.Listen("tcp", addr)
	if err != nil {
		os.Exit(1)
	}
	conn, err := l.Accept()
	if err != nil {
		os.Exit(1)
	}
	fmt.Fprint(conn, "VLC media player 3.0.0 Vetinari\nCommand Line Interface initialized. Type `help' for help.\n> ")
	var c fakeClock
	sc := bufio.NewScanner(conn)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 0 {
			continue
		}
		switch f[0] {
		case "add":
			c.load()
		case "pause":
			c.toggle()
		case "stop":
			c.loaded = false
		case "quit", "shutdown":
			return
		case "is_playing":
			if c.pos() >= 0 {
				fmt.Fprint(conn, "1\r\n> ")
			} else {
				fmt.Fprint(conn, "0\r\n> ")
			}
		case "get_time":
			if p := c.pos(); p >= 0 {
				fmt.Fprintf(conn, "%d\r\n> ", int(p))
			} else {
				fmt.Fprint(conn, "\r\n> ") // VLC prints nothing useful when idle
			}
		case "get_length":
			if c.pos() >= 0 {
				fmt.Fprintf(conn, "%d\r\n> ", int(fakeLen))
			}
		case "status":
			st := "stopped"
			if c.pos() >= 0 {
				st = "playing"
				if c.paused {
					st = "paused"
				}
			}
			fmt.Fprintf(conn, "( audio volume: 256 )\r\n( state %s )\r\n> ", st)
		}
	}
}

func fakeEngine(t *testing.T, name string) (Engine, func()) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_ENGINE", name)
	e, _ := EngineByName(name)
	eng, err := Open(e, exe, 50)
	if err != nil {
		t.Fatalf("start %s: %v", name, err)
	}
	return eng, eng.Close
}

// waitEnd waits for an EndEvent and checks the position moved on the way.
func waitEnd(t *testing.T, eng Engine) EndEvent {
	t.Helper()
	deadline := time.After(15 * time.Second)
	moved := false
	tk := time.NewTicker(50 * time.Millisecond)
	defer tk.Stop()
	for {
		select {
		case ev := <-eng.Events():
			if e, ok := ev.(EndEvent); ok {
				if e.Reason == "eof" && !moved {
					t.Errorf("%s: eof without the position ever moving", eng.Name())
				}
				return e
			}
		case <-tk.C:
			if eng.State().Pos > 0 {
				moved = true
			}
		case <-deadline:
			t.Fatalf("%s: no end event; state %+v", eng.Name(), eng.State())
		}
	}
}

func TestEnginesPlayToEnd(t *testing.T) {
	for _, name := range []string{"ffplay", "mplayer", "vlc"} {
		t.Run(name, func(t *testing.T) {
			eng, done := fakeEngine(t, name)
			defer done()
			if eng.Name() != name {
				t.Fatalf("name %q", eng.Name())
			}
			eng.Load("https://example.com/track.mp3", "Title", "Artist", "")
			if e := waitEnd(t, eng); e.Reason != "eof" {
				t.Fatalf("end %+v", e)
			}
			if d := eng.State().Dur; d < 1.5 {
				t.Errorf("duration %v", d)
			}
		})
	}
}

func TestEnginesPause(t *testing.T) {
	for _, name := range []string{"ffplay", "mplayer", "vlc"} {
		t.Run(name, func(t *testing.T) {
			eng, done := fakeEngine(t, name)
			defer done()
			eng.Load("https://example.com/track.mp3", "Title", "Artist", "")
			time.Sleep(700 * time.Millisecond)
			eng.TogglePause()
			if !eng.State().Paused {
				t.Fatal("not paused")
			}
			time.Sleep(2500 * time.Millisecond) // longer than the track: must not end while paused
			select {
			case ev := <-eng.Events():
				t.Fatalf("event while paused: %+v", ev)
			default:
			}
			eng.TogglePause()
			if e := waitEnd(t, eng); e.Reason != "eof" {
				t.Fatalf("end %+v", e)
			}
		})
	}
}

func TestFFplayError(t *testing.T) {
	eng, done := fakeEngine(t, "ffplay")
	defer done()
	eng.Load("https://example.com/bad.mp3", "T", "A", "")
	e := waitEnd(t, eng)
	if e.Reason != "error" || !strings.Contains(e.Err, "403") {
		t.Fatalf("end %+v", e)
	}
}

func TestFFplayFiltersAndVolume(t *testing.T) {
	f := &FFplay{vol: 120, url: "u", af: "lavfi=[volume=-2dB,equalizer=f=60:t=o:w=1:g=3],lavfi=[dynaudnorm=f=250]"}
	a := strings.Join(f.args(42), " ")
	for _, want := range []string{"-af volume=1.20,volume=-2dB,equalizer=f=60:t=o:w=1:g=3,dynaudnorm=f=250", "-ss 42.00", "-i u", "-nodisp"} {
		if !strings.Contains(a, want) {
			t.Errorf("args %q lack %q", a, want)
		}
	}
}

func TestChooseUnknown(t *testing.T) {
	if _, _, err := Choose("winamp", ""); err == nil {
		t.Fatal("expected error")
	}
}
