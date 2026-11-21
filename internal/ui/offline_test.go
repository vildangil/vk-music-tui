package ui

import (
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

type harness struct {
	t     *testing.T
	p     *tea.Program
	pw    *io.PipeWriter
	mu    sync.Mutex
	last  string
	model *Model
}

func startUI(t *testing.T, pl *player.Player, cfg *config.Config) *harness {
	h := &harness{t: t}
	var mp *Model
	r := recorder{Model: New(vk.New(vk.Kate, "x", ""), pl, cfg), mu: &h.mu, last: &h.last, mm: &mp}
	pr, pw := io.Pipe()
	h.pw = pw
	h.p = tea.NewProgram(r, tea.WithInput(pr), tea.WithOutput(io.Discard))
	go h.p.Run()
	h.p.Send(tea.WindowSizeMsg{Width: 120, Height: 30})
	go func() {
		for {
			h.mu.Lock()
			h.model = mp
			h.mu.Unlock()
			time.Sleep(20 * time.Millisecond)
		}
	}()
	return h
}

func (h *harness) frame() string { h.mu.Lock(); defer h.mu.Unlock(); return h.last }
func (h *harness) m() *Model {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.model
}
func (h *harness) send(msg tea.Msg) { h.p.Send(msg); time.Sleep(60 * time.Millisecond) }
func (h *harness) waitFor(what string, d time.Duration, cond func() bool) {
	dl := time.Now().Add(d)
	for {
		if m := h.m(); m != nil && cond() {
			return
		}
		if time.Now().After(dl) {
			h.t.Fatalf("timeout waiting for %s\n%s", what, h.frame())
		}
		time.Sleep(50 * time.Millisecond)
	}
}
func (h *harness) stop() {
	if m := h.m(); m != nil && m.dl != nil {
		m.dl.Stop()
	}
	h.p.Quit()
	h.pw.Close()
}

// "Моя музыка офлайн": tracks are saved for good, and with VK unreachable the
// player opens the saved library and plays the files.
func TestOfflineLibrary(t *testing.T) {
	base := os.Getenv("VKTUI_API_BASE")
	if base == "" {
		t.Skip("needs mock API (VKTUI_API_BASE)")
	}
	mpv, err := player.Find("")
	if err != nil {
		t.Skip("no mpv")
	}
	os.Setenv("VKTUI_CONFIG_DIR", t.TempDir())
	os.Setenv("VKTUI_CACHE_DIR", t.TempDir())
	os.Setenv("VKTUI_DATA_DIR", t.TempDir())
	cfg := &config.Config{Token: "x", UserID: 1, Volume: 0, DownloadDir: t.TempDir()}

	pl, err := player.Start(mpv, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer pl.Close()

	// ---- online: turn on "save offline" with the header button ----
	h := startUI(t, pl, cfg)
	h.waitFor("my music loaded", 15*time.Second, func() bool {
		p := h.m().cur()
		return p != nil && p.myMusic && p.done && p.length() == 450
	})
	if !strings.Contains(h.frame(), "Сохранить офлайн") {
		t.Fatalf("no offline button\n%s", h.frame())
	}
	var off headerButton
	for _, b := range h.m().headerButtons(h.m().cur()) {
		if b.act == actOffline {
			off = b
		}
	}
	h.send(tea.MouseMsg{X: off.x[0] + 2, Y: 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	h.waitFor("offline sync on", 3*time.Second, func() bool { return h.m().cfg.OfflineAll })
	h.waitFor("some tracks saved offline", 60*time.Second, func() bool { return len(scanOffline()) >= 4 })
	if _, err := os.Stat(libraryPath(cfg.UserID)); err != nil {
		t.Fatalf("library not saved: %v", err)
	}
	t.Log("\n" + h.frame())
	h.stop()
	time.Sleep(1500 * time.Millisecond) // let the running encode finish
	saved := len(scanOffline())

	// ---- offline: VK unreachable ----
	os.Setenv("VKTUI_API_BASE", "http://127.0.0.1:1/method/")
	defer os.Setenv("VKTUI_API_BASE", base)
	h2 := startUI(t, pl, cfg)
	defer h2.stop()
	h2.waitFor("offline library shown", 10*time.Second, func() bool {
		p := h2.m().cur()
		return p != nil && p.offlineMode && p.length() >= 4
	})
	if n := h2.m().cur().length(); n != saved && n != saved+1 {
		t.Fatalf("offline page has %d tracks, %d saved", n, saved)
	}
	h2.send(tea.KeyMsg{Type: tea.KeyEnter})
	h2.waitFor("plays offline file", 8*time.Second, func() bool {
		path, _ := pl.GetProperty("path")
		return strings.Contains(string(path), "offline") && pl.State().Pos > 0.3
	})
	if !strings.Contains(h2.frame(), "офлайн") {
		t.Fatalf("no offline marker\n%s", h2.frame())
	}
	t.Log("\n" + h2.frame())
}
