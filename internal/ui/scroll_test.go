package ui

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"vktui/internal/config"
	"vktui/internal/player"
	"vktui/internal/vk"
)

// Long synced lyrics (track 2 in the mock has 60 lines, 0.1 s apart) must be
// scrollable to the very end with the wheel and with keys; the queue view
// opens from the sidebar and jumps to a picked track.
func TestLyricsScrollAndQueue(t *testing.T) {
	if os.Getenv("VKTUI_API_BASE") == "" {
		t.Skip("needs mock API (VKTUI_API_BASE)")
	}
	mpv, err := player.Find("")
	if err != nil {
		t.Skip("no mpv")
	}
	pl, err := player.Start(mpv, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer pl.Close()
	os.Setenv("VKTUI_CONFIG_DIR", t.TempDir())
	os.Setenv("VKTUI_CACHE_DIR", t.TempDir())
	cfg := &config.Config{Token: "x", UserID: 1, Volume: 0, Repeat: 2, DownloadDir: t.TempDir()}

	var mu sync.Mutex
	var last string
	var mp *Model
	r := recorder{Model: New(vk.New(vk.Kate, "x", ""), pl, cfg), mu: &mu, last: &last, mm: &mp}
	pr, pw := io.Pipe()
	p := tea.NewProgram(r, tea.WithInput(pr), tea.WithOutput(io.Discard))
	go p.Run()
	defer pw.Close()
	defer p.Quit()
	p.Send(tea.WindowSizeMsg{Width: 120, Height: 30})

	frame := func() string { mu.Lock(); defer mu.Unlock(); return last }
	model := func() *Model { mu.Lock(); defer mu.Unlock(); return mp }
	send := func(m tea.Msg) { p.Send(m); time.Sleep(60 * time.Millisecond) }
	keyRunes := func(s string) { send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}) }
	waitFor := func(what string, cond func() bool) {
		dl := time.Now().Add(8 * time.Second)
		for !cond() {
			if time.Now().After(dl) {
				t.Fatalf("timeout waiting for %s\n%s", what, frame())
			}
			time.Sleep(50 * time.Millisecond)
		}
	}

	waitFor("list", func() bool { return strings.Contains(frame(), "Трек номер 2 ") })

	// header button "Перемешать" shuffle-plays the whole list
	if !strings.Contains(frame(), "[Перемешать]") {
		t.Fatalf("no shuffle button\n%s", frame())
	}
	bs := model().headerButtons(model().cur())
	send(tea.MouseMsg{X: bs[1].x[0] + 2, Y: 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	waitFor("shuffle play", func() bool { m := model(); return m.shuffle && m.playing != nil && len(m.queue) > 100 })
	send(tea.MouseMsg{X: bs[0].x[0] + 1, Y: 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	waitFor("play in order", func() bool { m := model(); return !m.shuffle && m.playing != nil && m.playing.ID == 1 })
	keyRunes("S") // Shift+S
	waitFor("shuffle via key", func() bool { return model().shuffle })

	// "Моя музыка" stays loaded when switching sections
	waitFor("all loaded", func() bool { return len(model().cur().tracks) == 450 })
	mine := model().cur()
	keyRunes("4")
	waitFor("popular", func() bool { return model().cur().title == "Популярное" })
	keyRunes("1")
	if c := model().cur(); c != mine || len(c.tracks) != 450 || c.loading {
		t.Fatalf("my music was reloaded: same=%v n=%d loading=%v", c == mine, len(c.tracks), c.loading)
	}
	send(tea.KeyMsg{Type: tea.KeyCtrlR})
	waitFor("reload", func() bool { return model().cur() != mine })
	waitFor("reloaded fully", func() bool { return len(model().cur().tracks) == 450 })
	send(tea.KeyMsg{Type: tea.KeyHome})

	// ---- cache: the playing track is saved in the background ----
	cached := filepath.Join(config.CacheDir(), model().playing.Key()+".mp3")
	waitFor("track cached", func() bool { st, err := os.Stat(cached); return err == nil && st.Size() > 16*1024 })

	// ---- download: "d" on the selected row (track 5), with ID3 tags ----
	for i := 0; i < 4; i++ {
		send(tea.KeyMsg{Type: tea.KeyDown})
	}
	keyRunes("в") // "d" on the Russian layout
	dl := filepath.Join(cfg.Downloads(), "Исполнитель 5 - Трек номер 5 — очень длинное название песни чтобы проверить обрезку.mp3")
	waitFor("downloaded", func() bool { _, err := os.Stat(dl); return err == nil && model().isDownloaded(model().cur().tracks[4]) })
	waitFor("↓ marker", func() bool { return strings.Contains(frame(), "↓") })
	// playing a downloaded track uses the file, not the network
	send(tea.KeyMsg{Type: tea.KeyEnter})
	waitFor("plays local file", func() bool {
		path, _ := pl.GetProperty("path")
		return strings.Contains(string(path), "VK") || strings.Contains(string(path), ".mp3")
	})
	send(tea.KeyMsg{Type: tea.KeyHome})

	send(tea.KeyMsg{Type: tea.KeyDown})
	send(tea.KeyMsg{Type: tea.KeyEnter})
	waitFor("track 2 plays", func() bool { m := model(); return m.playing != nil && m.playing.ID == 2 })
	keyRunes("t")
	waitFor("lyrics", func() bool { return strings.Contains(frame(), "Длинная строка номер 1") })

	lyrX := sideW + model().listW() + 5
	for i := 0; i < 30; i++ {
		send(tea.MouseMsg{X: lyrX, Y: 10, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	}
	waitFor("last line visible after wheel", func() bool { return strings.Contains(frame(), "Длинная строка номер 60") })
	// it must stay there (no snapping back while the user reads)
	time.Sleep(1500 * time.Millisecond)
	if !strings.Contains(frame(), "Длинная строка номер 60") {
		t.Fatalf("lyrics snapped back too early\n%s", frame())
	}
	// keyboard scrolling: "[" up, "]" down
	for i := 0; i < 20; i++ {
		keyRunes("[")
	}
	waitFor("top again via keys", func() bool {
		return strings.Contains(frame(), "Длинная строка номер 1 ") || strings.Contains(frame(), "Длинная строка номер 1 ") || strings.Contains(frame(), "номер 1  ")
	})
	for i := 0; i < 20; i++ {
		keyRunes("]")
	}
	waitFor("end via keys", func() bool { return strings.Contains(frame(), "Длинная строка номер 60") })
	t.Log("\n" + frame())

	// ---- queue view from the sidebar ----
	keyRunes("t") // close lyrics
	if !model().shuffle {
		keyRunes("s") // shuffle on: order differs from the list
	}
	m := model()
	qy := 4 + len(sectionNames) + 1
	send(tea.MouseMsg{X: 5, Y: qy, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	waitFor("queue page", func() bool { m := model(); return m.cur() != nil && m.cur().queue })
	if !strings.Contains(frame(), "Очередь") {
		t.Fatalf("no queue title\n%s", frame())
	}
	m = model()
	// the first row is the playing track, the next rows follow the shuffled order
	q := m.cur()
	if len(q.tracks) != len(m.order)-m.opos || q.tracks[0].Key() != m.playing.Key() {
		t.Fatalf("queue page doesn't follow play order: %d vs %d", len(q.tracks), len(m.order)-m.opos)
	}
	want := m.queue[m.order[m.opos+5]]
	send(tea.KeyMsg{Type: tea.KeyDown})
	for i := 0; i < 4; i++ {
		send(tea.KeyMsg{Type: tea.KeyDown})
	}
	send(tea.KeyMsg{Type: tea.KeyEnter})
	waitFor("jump within queue", func() bool { m := model(); return m.playing != nil && m.playing.Key() == want.Key() })
	if mm := model(); len(mm.order) != len(m.order) {
		t.Fatalf("jumping rebuilt the queue: %d -> %d", len(m.order), len(mm.order))
	}
	t.Log("\n" + frame())

	// ---- pause dots grow with the pause (mock: pause from 4.5 s to the end) ----
	keyRunes("o") // back from the queue
	keyRunes("t")
	if model().playing.ID == 2 {
		t.Skip("picked the long-lyrics track")
	}
	pl.SeekAbs(4.6)
	waitFor("first dot grows", func() bool {
		return strings.Contains(frame(), "•  ·  ·") || strings.Contains(frame(), "●  ·  ·")
	})
	waitFor("dots fill up", func() bool { return strings.Contains(frame(), "●  ●  ") })
	t.Log("\n" + frame())
}
