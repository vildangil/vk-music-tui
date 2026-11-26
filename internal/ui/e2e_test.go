package ui

import (
	"fmt"

	"github.com/charmbracelet/x/ansi"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"vktui/internal/config"
	"vktui/internal/player"
	"vktui/internal/vk"
)

// recorder wraps Model and keeps the last rendered frame.
type recorder struct {
	Model
	mu   *sync.Mutex
	last *string
	mm   **Model
}

func (r recorder) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m, c := r.Model.Update(msg)
	r.Model = m.(Model)
	r.mu.Lock()
	*r.last = r.Model.View()
	mm := r.Model
	*r.mm = &mm
	r.mu.Unlock()
	return r, c
}

// Run with: VKTUI_API_BASE=http://127.0.0.1:18777/method/ go test ./internal/ui -run E2E -v
func TestE2E(t *testing.T) {
	if os.Getenv("VKTUI_API_BASE") == "" {
		t.Skip("needs mock API (VKTUI_API_BASE)")
	}
	mpv, err := player.Find("")
	if err != nil {
		t.Skip("no mpv")
	}
	pl, err := player.Start(mpv, 10)
	if err != nil {
		t.Fatal(err)
	}
	defer pl.Close()
	cfg := &config.Config{Token: "x", UserID: 1, Volume: 10, DownloadDir: t.TempDir()}
	os.Setenv("VKTUI_CONFIG_DIR", t.TempDir())
	os.Setenv("VKTUI_CACHE_DIR", t.TempDir())

	var mu sync.Mutex
	var last string
	var mp *Model
	r := recorder{Model: New(vk.New(vk.Kate, "x", ""), pl, cfg), mu: &mu, last: &last, mm: &mp}
	pr, pw := io.Pipe()
	p := tea.NewProgram(r, tea.WithInput(pr), tea.WithOutput(io.Discard))
	go p.Run()
	defer pw.Close()
	p.Send(tea.WindowSizeMsg{Width: 110, Height: 30})

	frame := func() string { mu.Lock(); defer mu.Unlock(); return last }
	model := func() *Model { mu.Lock(); defer mu.Unlock(); return mp }
	key := func(s string) {
		switch s {
		case "enter":
			p.Send(tea.KeyMsg{Type: tea.KeyEnter})
		case "down":
			p.Send(tea.KeyMsg{Type: tea.KeyDown})
		case "end":
			p.Send(tea.KeyMsg{Type: tea.KeyEnd})
		case "esc":
			p.Send(tea.KeyMsg{Type: tea.KeyEsc})
		default:
			p.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
		}
		time.Sleep(150 * time.Millisecond)
	}
	waitFor := func(what string, cond func() bool) {
		dl := time.Now().Add(8 * time.Second)
		for !cond() {
			if time.Now().After(dl) {
				t.Fatalf("timeout waiting for %s\n%s", what, frame())
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	waitFor("list", func() bool { return strings.Contains(frame(), "Трек номер 1 ") })
	t.Log("\n" + frame())

	// The frame must always be exactly 30 lines, none wider than 110 cells,
	// even with emoji / combining marks / CJK in titles (tracks 9-16).
	checkFrame := func(where string) {
		f := frame()
		lines := strings.Split(f, "\n")
		if len(lines) != 30 {
			t.Fatalf("%s: frame has %d lines\n%s", where, len(lines), f)
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w > 110 {
				t.Fatalf("%s: line %d is %d wide: %q", where, i, w, ansi.Strip(l))
			}
		}
	}
	for i := 0; i < 25; i++ {
		key("down")
		checkFrame(fmt.Sprintf("down #%d", i))
	}
	t.Log("\n" + frame())
	for i := 0; i < 25; i++ {
		key("up")
	}
	checkFrame("back up")

	// play track 1
	key("enter")
	waitFor("playing", func() bool { st := pl.State(); return st.Pos > 0.5 })
	t.Log("\n" + frame())

	// next -> track 2, next -> track 3 (empty url -> resolve), next -> track 4 (broken -> retry -> skip to 5)
	key("n")
	key("n")
	waitFor("resolved track 3 plays", func() bool {
		m := model()
		return m != nil && m.playing != nil && m.playing.ID == 3 && pl.State().Pos > 0.3
	})
	key("n")
	waitFor("skip broken to 5", func() bool {
		m := model()
		return m != nil && m.playing != nil && m.playing.ID == 5 && pl.State().Pos > 0.3
	})
	t.Log("\n" + frame())

	// auto-advance on EOF (6s tracks)
	waitFor2 := time.Now().Add(10 * time.Second)
	for time.Now().Before(waitFor2) {
		if m := model(); m != nil && m.playing != nil && m.playing.ID == 6 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if m := model(); m.playing == nil || m.playing.ID != 6 {
		t.Fatalf("no auto-advance: %+v", m.playing)
	}

	// pause
	key(" ")
	time.Sleep(400 * time.Millisecond)
	if !pl.State().Paused {
		t.Fatal("not paused")
	}
	key(" ")

	// all 450 tracks load in the background without scrolling
	waitFor("all 450 loaded", func() bool { m := model(); return m != nil && len(m.cur().tracks) == 450 })
	if m := model(); len(m.queue) != 450 {
		t.Fatalf("queue not extended: %d", len(m.queue))
	}
	t.Log("\n" + frame())

	// playlists
	key("6")
	waitFor("playlists", func() bool { return strings.Contains(frame(), "Плейлист А") })
	key("down")
	key("enter")
	waitFor("playlist tracks", func() bool { return strings.Contains(frame(), "Трек номер 1000") })
	key("esc")
	waitFor("back", func() bool { return strings.Contains(frame(), "Плейлист А") })

	// search
	key("/")
	for _, ch := range "кино" {
		key(string(ch))
	}
	key("enter")
	waitFor("search results", func() bool { return strings.Contains(frame(), "кино 0") })
	t.Log("\n" + frame())

	// popular (bare array response)
	key("4")
	waitFor("popular", func() bool { return strings.Contains(frame(), "Трек номер 4000") })
	key("a")
	waitFor("added", func() bool { return strings.Contains(frame(), "Добавлено") })

	// volume / shuffle / repeat
	key("+")
	key("s")
	key("r")
	time.Sleep(300 * time.Millisecond)
	if v := pl.State().Volume; v != 15 {
		t.Fatalf("volume %v", v)
	}
	t.Log("\n" + frame())
	// ---- mouse ----
	click := func(x, y int) {
		p.Send(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
		time.Sleep(60 * time.Millisecond)
	}
	if m := model(); m.shuffle {
		key("s") // deterministic next/prev below
	}
	// sidebar: "1 Моя музыка" is on screen row 4
	click(5, 4)
	waitFor("my music via click", func() bool {
		m := model()
		return m != nil && m.cur().title == "Моя музыка" && len(m.cur().tracks) > 0
	})
	// double-click the 2nd row (rows start at y=3)
	click(30, 4)
	click(30, 4)
	waitFor("double-click plays track 2", func() bool {
		m := model()
		return m != nil && m.playing != nil && m.playing.ID == 2 && pl.State().Pos > 0.2
	})
	// next button on player line 1 (y = 30-5+1 = 26), x = 8..9
	click(8, 26)
	waitFor("next button -> track 3", func() bool { m := model(); return m.playing != nil && m.playing.ID == 3 })
	time.Sleep(700 * time.Millisecond)
	// click on the right part of the progress bar (y=27) seeks forward
	click(95, 27)
	p.Send(tea.MouseMsg{X: 95, Y: 27, Action: tea.MouseActionRelease, Button: tea.MouseButtonNone})
	waitFor("seek by click", func() bool { return pl.State().Pos > 3.5 })
	// play/pause button x=5..6
	click(5, 26)
	waitFor("paused by click", func() bool { return pl.State().Paused })
	click(5, 26)
	// wheel scrolls the list
	p.Send(tea.MouseMsg{X: 50, Y: 10, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	waitFor("wheel scroll", func() bool { m := model(); return m.cur().top == 3 })
	// media overlay "next" button
	before := model().opos
	pl.Events() <- player.MediaKeyEvent{Action: "next"}
	waitFor("media key next advances the queue", func() bool {
		m := model()
		return m.playing != nil && m.opos > before
	})
	checkFrame("after mouse")
	t.Log("\n" + frame())

	// ---- drag the progress knob ----
	for i := 0; i < 3 && model().repeat != 2; i++ {
		key("r") // repeat one: the track can't change under the drag
	}
	waitFor("something playing", func() bool { m := model(); return m.playing != nil && m.repeat == 2 && pl.State().Pos > 0.2 })
	p.Send(tea.MouseMsg{X: 10, Y: 27, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	time.Sleep(50 * time.Millisecond)
	for x := 20; x <= 90; x += 10 {
		p.Send(tea.MouseMsg{X: x, Y: 27, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
		time.Sleep(20 * time.Millisecond)
	}
	waitFor("dragging shown", func() bool { m := model(); return m.dragging && m.dragFrac > 0.7 })
	p.Send(tea.MouseMsg{X: 90, Y: 27, Action: tea.MouseActionRelease, Button: tea.MouseButtonNone})
	waitFor("seek after drag", func() bool { m := model(); return !m.dragging && pl.State().Pos > 3.5 })

	// ---- settings: equalizer + normalization reach mpv ----
	key("7")
	waitFor("settings", func() bool { return strings.Contains(frame(), "Эквалайзер") })
	key("right") // preset -> Бас+
	waitFor("bass preset in mpv", func() bool {
		af, err := pl.GetProperty("af")
		return err == nil && strings.Contains(string(af), "equalizer")
	})
	for i := 0; i < 11; i++ {
		key("down")
	}
	key("enter") // normalization on
	waitFor("normalize in mpv", func() bool {
		af, _ := pl.GetProperty("af")
		return strings.Contains(string(af), "dynaudnorm")
	})
	checkFrame("settings")
	t.Log("\n" + frame())
	// a band edit switches to the custom preset
	key("up")
	key("right")
	if m := model(); m.cfg.EQPreset != customPreset || m.cfg.EQBands[9] != 1 {
		t.Fatalf("custom band edit: preset=%q bands=%v", m.cfg.EQPreset, m.cfg.EQBands)
	}
	if st := pl.State(); st.Idle {
		t.Fatal("playback stopped after changing filters")
	}

	// ---- VK Mix: endless queue ----
	for i := 0; i < 3 && model().repeat != 0; i++ {
		key("r") // repeat off again
	}
	key("2")
	waitFor("mix loaded", func() bool { return strings.Contains(frame(), "Микс трек 5000") })
	key("enter")
	waitFor("mix plays", func() bool { m := model(); return m.playing != nil && m.playing.ID == 5000 })
	for i := 0; i < 12; i++ {
		key("n")
	}
	waitFor("mix topped up", func() bool { m := model(); return len(m.queue) >= 20 && m.playing != nil && m.playing.ID >= 5012 })
	checkFrame("mix")
	t.Log("\n" + frame())

	// ---- lyrics panel, opened with the Russian-layout key for "t" ("е") ----
	for i := 0; i < 3 && model().repeat != 2; i++ {
		key("r") // repeat one so the track stays put
	}
	key("е")
	waitFor("lyrics shown", func() bool { return strings.Contains(frame(), "Текст · VK · синхронный") })
	checkFrame("lyrics open")
	waitFor("live line advances", func() bool {
		m := model()
		_, cur, _ := m.lyricRows(30, 15)
		return cur >= 1
	})
	t.Log("\n" + frame())
	// click the 3rd lyric line ("Третья строка"): seeks to 3.0s
	m0 := model()
	rows, _, top := m0.lyricRows(m0.lyrW()-4, m0.mainH()-4)
	target := -1
	for i, r := range rows {
		if strings.HasPrefix(r.text, "Третья") {
			target = i
		}
	}
	if target < 0 {
		t.Fatalf("no third line in %+v", rows)
	}
	click(sideW+m0.listW()+5, 3+target-top)
	waitFor("seek by lyric click", func() bool { p := pl.State().Pos; return p >= 2.9 && p < 4.4 })
	// close with the Latin key, panel disappears
	key("t")
	waitFor("lyrics closed", func() bool { return !strings.Contains(frame(), "Текст · VK") })
	// Russian layout for shuffle ("ы" = s)
	sh := model().shuffle
	key("ы")
	if model().shuffle == sh {
		t.Fatal("Russian-layout hotkey ignored")
	}
	checkFrame("end")
	p.Quit()
}
