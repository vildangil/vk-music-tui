package ui

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"vktui/internal/config"
	"vktui/internal/player"
)

func TestAccountsConfig(t *testing.T) {
	c := &config.Config{Token: "a", UserID: 1}
	if c.ActiveIndex() != 0 || len(c.Accounts) != 1 {
		t.Fatalf("old single-account config not migrated: %+v", c.Accounts)
	}
	c.AddAccount(config.Account{Name: "Второй", UserID: 2, Token: "b"})
	if c.Token != "b" || len(c.Accounts) != 2 || c.ActiveIndex() != 1 {
		t.Fatalf("add: %+v", c)
	}
	c.AddAccount(config.Account{Name: "Второй", UserID: 2, Token: "b2"}) // re-login same user
	if len(c.Accounts) != 2 || c.Token != "b2" {
		t.Fatalf("re-login duplicated: %+v", c.Accounts)
	}
	c.RemoveActive()
	if c.Token != "a" || len(c.Accounts) != 1 {
		t.Fatalf("remove: %+v", c)
	}
	c.RemoveActive()
	if c.Token != "" || len(c.Accounts) != 0 {
		t.Fatalf("remove last: %+v", c)
	}
}

func TestAccountSwitchUI(t *testing.T) {
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
	cfg := &config.Config{Volume: 0, DownloadDir: t.TempDir()}
	cfg.AddAccount(config.Account{Name: "Первый", UserID: 1, Token: "tok1"})
	cfg.AddAccount(config.Account{Name: "Второй", UserID: 2, Token: "tok2"})
	cfg.Use(cfg.Accounts[0])

	pl, err := player.Start(mpv, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer pl.Close()
	h := startUI(t, pl, cfg)
	defer h.stop()
	h.waitFor("loaded", 10*time.Second, func() bool { p := h.m().cur(); return p != nil && p.length() > 0 })
	h.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("7")})
	for i := 0; i < rowAccount; i++ {
		h.send(tea.KeyMsg{Type: tea.KeyDown})
	}
	h.waitFor("account row", 3*time.Second, func() bool {
		return strings.Contains(h.frame(), "Аккаунт") && strings.Contains(h.frame(), "1 из 2")
	})
	h.send(tea.KeyMsg{Type: tea.KeyRight})
	h.waitFor("switched", 3*time.Second, func() bool {
		m := h.m()
		return m.cfg.Token == "tok2" && m.api.Token == "tok2" && strings.Contains(h.frame(), "2 из 2")
	})
	if p := h.m().cache[secMy]; p == nil || !p.myMusic {
		t.Fatal("my music not reloaded for the new account")
	}
	t.Log("\n" + h.frame())
	// "Добавить аккаунт" quits the UI asking main to run the login
	h.send(tea.KeyMsg{Type: tea.KeyDown})
	h.send(tea.KeyMsg{Type: tea.KeyEnter})
	h.waitFor("wants login", 3*time.Second, func() bool { return h.m().WantLogin() })
}
