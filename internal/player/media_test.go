package player

import (
	"os"
	"strings"
	"testing"
	"time"
)

// Manual check of the OS media overlay:
//
//	VKTUI_MEDIA_TEST=1 VKTUI_MEDIA_FILE=some.mp3 go test ./internal/player -run Media -v
func TestMediaKeys(t *testing.T) {
	if os.Getenv("VKTUI_MEDIA_TEST") == "" {
		t.Skip("manual")
	}
	mpv, err := Find("")
	if err != nil {
		t.Skip("no mpv")
	}
	p, err := Start(mpv, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	src := os.Getenv("VKTUI_MEDIA_FILE")
	if src == "" {
		t.Skip("set VKTUI_MEDIA_FILE")
	}
	p.Load(src, "Тестовый трек", "Тестовый Исполнитель", "Тестовый Альбом")
	time.Sleep(1500 * time.Millisecond)
	md, err := p.GetProperty("metadata")
	t.Logf("metadata: %s %v", md, err)
	mt, _ := p.GetProperty("media-title")
	t.Logf("media-title: %s", mt)
	if st := p.State(); st.Idle || st.Pos <= 0 {
		t.Fatalf("not playing: %+v", st)
	}
	if !strings.Contains(string(md), "Тестовый Исполнитель") && strings.HasPrefix(src, "http") {
		t.Fatalf("artist tag missing")
	}
	d, _ := p.GetProperty("duration")
	t.Logf("duration: %s", d)
	p.SeekAbs(20)
	time.Sleep(1500 * time.Millisecond)
	if pos := p.State().Pos; pos < 19 {
		t.Fatalf("seek didn't work: pos=%v", pos)
	}
	t.Logf("after seek: %.1f", p.State().Pos)
	if os.Getenv("VKTUI_MEDIA_WAIT") == "" {
		return
	}
	deadline := time.After(40 * time.Second)
	for {
		select {
		case ev := <-p.Events():
			t.Logf("event: %#v", ev)
			if mk, ok := ev.(MediaKeyEvent); ok && mk.Action == "next" {
				return
			}
		case <-deadline:
			t.Fatal("no media key event")
		}
	}
}
