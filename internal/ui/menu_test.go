package ui

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"vktui/internal/config"
	"vktui/internal/player"
)

func TestSettingsMenus(t *testing.T) {
	if os.Getenv("VKTUI_API_BASE") == "" {
		t.Skip("needs mock API (VKTUI_API_BASE)")
	}
	mpv, err := player.Find("")
	if err != nil {
		t.Skip("no mpv")
	}
	os.Setenv("VKTUI_CONFIG_DIR", t.TempDir())
	os.Setenv("VKTUI_CACHE_DIR", t.TempDir())
	os.Setenv("VKTUI_DATA_DIR", t.TempDir())
	cfg := &config.Config{Token: "x", UserID: 1, DownloadDir: t.TempDir()}
	pl, err := player.Start(mpv, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer pl.Close()
	h := startUI(t, pl, cfg)
	defer h.stop()
	key := func(s string) {
		switch s {
		case "enter":
			h.send(tea.KeyMsg{Type: tea.KeyEnter})
		case "down":
			h.send(tea.KeyMsg{Type: tea.KeyDown})
		case "esc":
			h.send(tea.KeyMsg{Type: tea.KeyEsc})
		default:
			h.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
		}
	}
	check := func(where string) {
		lines := strings.Split(h.frame(), "\n")
		if len(lines) != 30 {
			t.Fatalf("%s: %d lines", where, len(lines))
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w != 120 && i < 29 {
				t.Fatalf("%s: line %d is %d wide: %q", where, i, w, ansi.Strip(l))
			}
		}
	}
	h.waitFor("ui", 5*time.Second, func() bool { return strings.Contains(h.frame(), "Моя музыка") })
	key("7")
	key("enter") // equalizer row → popup
	h.waitFor("eq menu", 3*time.Second, func() bool { return h.m().menu != nil && strings.Contains(h.frame(), "╭─ Эквалайзер") })
	check("eq menu open")
	t.Log("\n" + h.frame())
	key("down")
	key("down") // Бас++
	key("enter")
	h.waitFor("preset picked", 3*time.Second, func() bool { return h.m().menu == nil && h.m().cfg.EQPreset == "Бас++" })

	// lyrics source menu, click on an item
	for i := 0; i < rowLyricsSrc; i++ {
		key("down")
	}
	key("enter")
	h.waitFor("src menu", 3*time.Second, func() bool { return h.m().menu != nil && strings.Contains(h.frame(), "Только LRCLIB") })
	m := h.m()
	x, y, _, _ := m.menuRect()
	h.send(tea.MouseMsg{X: x + 3, Y: y + 1 + 2, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	h.waitFor("lrclib picked", 3*time.Second, func() bool { return h.m().cfg.LyricsSrc == "lrclib" })
	// Esc closes without changes
	key("enter")
	h.waitFor("menu again", 3*time.Second, func() bool { return h.m().menu != nil })
	key("esc")
	h.waitFor("closed", 3*time.Second, func() bool { return h.m().menu == nil && h.m().cfg.LyricsSrc == "lrclib" })
	check("after menus")
}
