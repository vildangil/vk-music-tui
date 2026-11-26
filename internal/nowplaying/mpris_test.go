//go:build linux || freebsd

package nowplaying

import (
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"vktui/internal/player"
)

type stubEngine struct {
	st     player.State
	events chan any
}

func (s *stubEngine) Name() string                        { return "stub" }
func (s *stubEngine) Binary() string                      { return "" }
func (s *stubEngine) Load(string, string, string, string) { s.st.Idle = false; s.st.Dur = 200 }
func (s *stubEngine) TogglePause()                        { s.st.Paused = !s.st.Paused }
func (s *stubEngine) Seek(float64)                        {}
func (s *stubEngine) SeekAbs(float64)                     {}
func (s *stubEngine) SetVolume(v float64)                 { s.st.Volume = v }
func (s *stubEngine) Stop()                               { s.st.Idle = true }
func (s *stubEngine) State() player.State                 { return s.st }
func (s *stubEngine) SetAudioFilters(string) error        { return nil }
func (s *stubEngine) Events() chan any                    { return s.events }
func (s *stubEngine) Close()                              {}

func TestMPRIS(t *testing.T) {
	bus, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Skip("no D-Bus session bus:", err)
	}
	defer bus.Close()
	stub := &stubEngine{events: make(chan any, 4), st: player.State{Volume: 80, Idle: true}}
	eng := Wrap(stub)
	defer eng.Close()
	if _, ok := eng.(*mpris); !ok {
		t.Skip("MPRIS not attached")
	}
	eng.Load("https://x/a.mp3", "Песня", "Исполнитель", "Альбом")

	var names []string
	if err := bus.BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&names); err != nil {
		t.Fatal(err)
	}
	name := ""
	for _, n := range names {
		if len(n) >= len("org.mpris.MediaPlayer2.vktui") && n[:len("org.mpris.MediaPlayer2.vktui")] == "org.mpris.MediaPlayer2.vktui" {
			name = n
		}
	}
	if name == "" {
		t.Fatal("vktui not on the bus")
	}
	obj := bus.Object(name, objPath)
	v, err := obj.GetProperty(ifacePlayer + ".Metadata")
	if err != nil {
		t.Fatal(err)
	}
	md := v.Value().(map[string]dbus.Variant)
	if md["xesam:title"].Value() != "Песня" {
		t.Fatalf("metadata %v", md)
	}
	if st, _ := obj.GetProperty(ifacePlayer + ".PlaybackStatus"); st.Value() != "Playing" {
		t.Fatalf("status %v", st)
	}
	if c := obj.Call(ifacePlayer+".Next", 0); c.Err != nil {
		t.Fatal(c.Err)
	}
	select {
	case ev := <-stub.events:
		if k, ok := ev.(player.MediaKeyEvent); !ok || k.Action != "next" {
			t.Fatalf("event %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Next did not reach the player")
	}
	if c := obj.Call(ifacePlayer+".Seek", 0, int64(5e6)); c.Err != nil {
		t.Fatal("Seek:", c.Err)
	}
}
