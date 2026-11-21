package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	"vktui/internal/vk"
)

// WantLogin reports that the user picked "Добавить аккаунт": main signs in
// on the plain terminal and then starts the player again.
func (m Model) WantLogin() bool { return m.wantLogin }

// Shutdown stops background work (downloads) before the model is dropped.
func (m Model) Shutdown() {
	if m.dl != nil {
		m.dl.Stop()
	}
}

func (m *Model) addAccount() tea.Cmd {
	m.wantLogin = true
	return tea.Quit
}

// switchAccount moves d accounts forward/back and reloads everything for it.
func (m *Model) switchAccount(d int) tea.Cmd {
	c := m.cfg
	i := c.ActiveIndex()
	n := len(c.Accounts)
	if n < 2 {
		m.setStatus("Других аккаунтов нет — «Добавить аккаунт» ниже", false)
		return nil
	}
	a := c.Accounts[(i+d+n)%n]
	c.Use(a)
	_ = c.Save()

	// fresh client and state for the other account
	m.api = vk.New(vk.AppByName(c.App), c.Token, c.DeviceID)
	m.finder = newFinder(m.api, m.cfg.LyricsSrc)
	m.me = vk.User{ID: c.UserID}
	m.pl.Stop()
	m.playing, m.queue, m.order, m.qsrc, m.opos = nil, nil, nil, nil, 0
	m.cache = nil
	m.lyr, m.lyrKey = nil, ""
	m.setStatus("Аккаунт: "+a.Label(), false)
	cmd := m.loadSection(secMy)
	// stay on the settings screen so you can keep switching
	if p := (&page{title: "Настройки", settings: true, done: true}); true {
		p.cursor = rowAccount
		m.stack = []*page{p}
		m.side = int(secSettings)
	}
	return tea.Batch(cmd, m.fetchMe(), tea.SetWindowTitle("VK Music"))
}

func (m *Model) accountLabel() string {
	c := m.cfg
	i := c.ActiveIndex()
	if i < 0 {
		return "—"
	}
	return c.Accounts[i].Label()
}
