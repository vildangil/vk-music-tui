package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"vktui/internal/lyrics"
)

// ---------- lyrics source ----------

var lyricsSources = []struct{ id, name string }{
	{"", "Авто: VK → LRCLIB → Genius"},
	{"vk", "Только VK"},
	{"lrclib", "Только LRCLIB (синхронный)"},
	{"genius", "Только Genius (без синхронизации)"},
}

func lyricsSrcIndex(id string) int {
	for i, s := range lyricsSources {
		if s.id == id {
			return i
		}
	}
	return 0
}

func srcDir(src string) string {
	if src == "" {
		return "auto"
	}
	return src
}

func pickProviders(src string, v lyrics.VK, l lyrics.LRCLIB, g lyrics.Genius) []lyrics.Provider {
	switch src {
	case "vk":
		return []lyrics.Provider{v}
	case "lrclib":
		return []lyrics.Provider{l}
	case "genius":
		return []lyrics.Provider{g}
	}
	return []lyrics.Provider{v, l, g}
}

func (m *Model) setLyricsSrc(i int) tea.Cmd {
	m.cfg.LyricsSrc = lyricsSources[i].id
	_ = m.cfg.Save()
	m.finder = newFinder(m.api, m.cfg.LyricsSrc)
	m.lyrKey = "" // reload for the playing track
	m.setStatus("Источник текста: "+lyricsSources[i].name, false)
	return m.fetchLyrics()
}

func (m *Model) lyricsSrcMenu() {
	names := make([]string, len(lyricsSources))
	for i, s := range lyricsSources {
		names[i] = s.name
	}
	m.openMenu("Источник текста", names, lyricsSrcIndex(m.cfg.LyricsSrc), func(m *Model, i int) tea.Cmd { return m.setLyricsSrc(i) })
}

// ---------- settings rows with a list of choices ----------

// settingsMenu opens a popup for rows that have several values. Returns false
// for plain on/off rows (those just toggle).
func (m *Model) settingsMenu(row int) bool {
	c := m.cfg
	switch row {
	case rowPreset:
		names := make([]string, len(eqPresets))
		for i, p := range eqPresets {
			names[i] = p.Name
		}
		m.openMenu("Эквалайзер", names, presetIndex(c.EQPreset), func(m *Model, i int) tea.Cmd {
			m.cfg.EQPreset = eqPresets[i].Name
			if m.cfg.EQPreset == "Выкл" {
				m.cfg.EQPreset = ""
			}
			_ = m.cfg.Save()
			return m.applyFilters()
		})
	case rowLyricsSrc:
		m.lyricsSrcMenu()
	case rowCacheSize:
		sizes := []int{256, 512, 1024, 2048, 5120, 10240}
		names := make([]string, len(sizes))
		cur := -1
		for i, s := range sizes {
			names[i] = mbStr(s)
			if s == c.CacheLimitMB() {
				cur = i
			}
		}
		m.openMenu("Размер кэша", names, cur, func(m *Model, i int) tea.Cmd {
			m.cfg.CacheMB = sizes[i]
			_ = m.cfg.Save()
			return nil
		})
	case rowVolStep:
		steps := []int{1, 2, 5, 10}
		names := make([]string, len(steps))
		cur := -1
		for i, s := range steps {
			names[i] = fmt.Sprintf("%d%%", s)
			if s == c.Step() {
				cur = i
			}
		}
		m.openMenu("Шаг громкости", names, cur, func(m *Model, i int) tea.Cmd {
			m.cfg.VolStep = steps[i]
			_ = m.cfg.Save()
			return nil
		})
	case rowAccent:
		cur := 0
		if c.Accent == "blue" {
			cur = 1
		}
		m.openMenu("Цвет", []string{"Зелёный", "Синий VK"}, cur, func(m *Model, i int) tea.Cmd {
			m.cfg.Accent = map[int]string{0: "green", 1: "blue"}[i]
			applyAccent(m.cfg.Accent)
			_ = m.cfg.Save()
			return nil
		})
	case rowAccount:
		var names []string
		for _, a := range c.Accounts {
			names = append(names, a.Label())
		}
		n := len(names)
		names = append(names, "+ Добавить аккаунт")
		m.openMenu("Аккаунт", names, c.ActiveIndex(), func(m *Model, i int) tea.Cmd {
			if i == n {
				return m.addAccount()
			}
			if d := i - m.cfg.ActiveIndex(); d != 0 {
				return m.switchAccount(d)
			}
			return nil
		})
	default:
		return false
	}
	return true
}
