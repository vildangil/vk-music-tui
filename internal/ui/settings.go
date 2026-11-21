package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"vktui/internal/config"
	"vktui/internal/player"
)

// ---------- equalizer ----------

var eqFreqs = [10]int{31, 62, 125, 250, 500, 1000, 2000, 4000, 8000, 16000}

type eqPreset struct {
	Name  string
	Gains [10]float64
}

const customPreset = "Свой"

var eqPresets = []eqPreset{
	{"Выкл", [10]float64{}},
	{"Бас+", [10]float64{6, 5, 4, 2, 0, 0, 0, 0, 0, 0}},
	{"Бас++", [10]float64{9, 8, 6, 3, 1, 0, 0, 0, 0, 0}},
	{"Рок", [10]float64{5, 4, 2, -1, -2, -1, 2, 4, 5, 5}},
	{"Поп", [10]float64{-1, 1, 3, 4, 3, 0, -1, -1, 0, 1}},
	{"Электроника", [10]float64{6, 5, 1, 0, -2, 2, 1, 2, 5, 6}},
	{"Хип-хоп", [10]float64{7, 6, 2, 3, -1, -1, 1, 0, 2, 3}},
	{"Вокал", [10]float64{-2, -2, -1, 1, 4, 4, 3, 1, 0, -1}},
	{"Классика", [10]float64{4, 3, 2, 1, -1, -1, 0, 2, 3, 4}},
	{"Мягкий верх", [10]float64{0, 0, 0, 0, 0, 0, -2, -4, -5, -6}},
	{customPreset, [10]float64{}},
}

func presetIndex(name string) int {
	for i, p := range eqPresets {
		if p.Name == name {
			return i
		}
	}
	return 0
}

// currentGains returns the band gains of the selected preset.
func (m *Model) currentGains() [10]float64 {
	if m.cfg.EQPreset == customPreset {
		return m.cfg.EQBands
	}
	return eqPresets[presetIndex(m.cfg.EQPreset)].Gains
}

// audioFilters builds mpv's --af value for the equalizer and normalization.
func (m *Model) audioFilters() string {
	var chain []string
	g := m.currentGains()
	peak := 0.0
	for i, gain := range g {
		if gain == 0 {
			continue
		}
		peak = math.Max(peak, gain)
		chain = append(chain, fmt.Sprintf("equalizer=f=%d:t=o:w=1:g=%g", eqFreqs[i], gain))
	}
	var parts []string
	if len(chain) > 0 {
		if peak > 0 {
			// leave headroom so boosted bands don't clip
			chain = append([]string{fmt.Sprintf("volume=-%gdB", math.Round(peak*0.7*10)/10)}, chain...)
		}
		parts = append(parts, "lavfi=["+strings.Join(chain, ",")+"]")
	}
	if m.cfg.Normalize {
		parts = append(parts, "lavfi=[dynaudnorm=f=250:g=15:m=8]")
	}
	return strings.Join(parts, ",")
}

type filterErrMsg struct{ err error }

func (m *Model) applyFilters() tea.Cmd {
	pl, f := m.pl, m.audioFilters()
	return func() tea.Msg {
		if err := pl.SetAudioFilters(f); err != nil {
			return filterErrMsg{err}
		}
		return nil
	}
}

// ---------- settings screen ----------

const (
	rowPreset = iota
	rowBand0
	rowNormalize = rowBand0 + 10
	rowAutoplay  = rowNormalize + 1
	rowLyrics    = rowAutoplay + 1
	rowLyricsSrc = rowLyrics + 1
	rowIcons     = rowLyricsSrc + 1
	rowCache     = rowIcons + 1
	rowCacheSize = rowCache + 1
	rowOffline   = rowCacheSize + 1
	rowDlDir     = rowOffline + 1
	rowVolStep   = rowDlDir + 1
	rowAccent    = rowVolStep + 1
	rowAccount   = rowAccent + 1
	rowAddAcc    = rowAccount + 1
	settingsRows = rowAddAcc + 1
)

func onOff(b bool) string {
	if b {
		return "Вкл"
	}
	return "Выкл"
}

// changeSetting changes the selected row by d (-1 / +1). Returns a command to apply audio changes.
func (m *Model) changeSetting(row, d int) tea.Cmd {
	c := m.cfg
	var cmd tea.Cmd
	switch {
	case row == rowPreset:
		i := (presetIndex(c.EQPreset) + d + len(eqPresets)) % len(eqPresets)
		c.EQPreset = eqPresets[i].Name
		if c.EQPreset == "Выкл" {
			c.EQPreset = ""
		}
		cmd = m.applyFilters()
	case row >= rowBand0 && row < rowBand0+10:
		if c.EQPreset != customPreset {
			c.EQBands = m.currentGains() // start editing from what is playing now
			c.EQPreset = customPreset
		}
		b := row - rowBand0
		c.EQBands[b] = math.Max(-12, math.Min(12, c.EQBands[b]+float64(d)))
		cmd = m.applyFilters()
	case row == rowNormalize:
		c.Normalize = !c.Normalize
		cmd = m.applyFilters()
	case row == rowAutoplay:
		v := !c.AutoplayOn()
		c.Autoplay = &v
	case row == rowLyricsSrc:
		i := lyricsSrcIndex(c.LyricsSrc)
		return m.setLyricsSrc((i + d + len(lyricsSources)) % len(lyricsSources))
	case row == rowLyrics:
		return m.toggleLyrics()
	case row == rowIcons:
		v := !c.NerdIconsOn()
		c.NerdIcons = &v
		if v {
			m.setStatus("Иконки Nerd Font включены. Нужен шрифт с nerdfonts.com, выбранный в терминале, иначе будут квадратики", false)
		}
	case row == rowCache:
		v := !c.CacheEnabled()
		c.CacheOn = &v
	case row == rowCacheSize:
		sizes := []int{256, 512, 1024, 2048, 5120, 10240}
		i := 2
		for k, s := range sizes {
			if s == c.CacheLimitMB() {
				i = k
			}
		}
		c.CacheMB = sizes[(i+d+len(sizes))%len(sizes)]
		player.TrimDir(config.CacheDir(), int64(c.CacheMB)<<20)
	case row == rowOffline:
		m.toggleOfflineAll()
	case row == rowDlDir:
		openFolder(c.Downloads())
	case row == rowVolStep:
		steps := []int{1, 2, 5, 10}
		i := 0
		for k, s := range steps {
			if s == c.Step() {
				i = k
			}
		}
		c.VolStep = steps[(i+d+len(steps))%len(steps)]
	case row == rowAccount:
		return m.switchAccount(d)
	case row == rowAddAcc:
		return m.addAccount()
	case row == rowAccent:
		if c.Accent == "blue" {
			c.Accent = "green"
		} else {
			c.Accent = "blue"
		}
		applyAccent(c.Accent)
	}
	_ = c.Save()
	return cmd
}

func eqBar(g float64, w int) string {
	// w cells, centre = 0 dB, edges = ±12 dB
	if w < 5 {
		return ""
	}
	mid := w / 2
	pos := mid + int(math.Round(g/12*float64(mid)))
	pos = max(0, min(w-1, pos))
	var b strings.Builder
	for i := 0; i < w; i++ {
		switch {
		case i == pos:
			b.WriteString("●")
		case (i > mid && i < pos) || (i < mid && i > pos):
			b.WriteString("━")
		case i == mid:
			b.WriteString("|")
		default:
			b.WriteString("─")
		}
	}
	return b.String()
}

func (m Model) viewSettings(w, h int) string {
	p := m.cur()
	var b strings.Builder
	b.WriteString(sTitle.Render(fit(" Настройки", w)) + "\n")
	b.WriteString(sFaint.Render(fit(" ↑/↓ выбрать · ←/→ или клик изменить", w)) + "\n")

	gains := m.currentGains()
	preset := m.cfg.EQPreset
	if preset == "" {
		preset = "Выкл"
	}
	labelW := 26
	var rows []string // rendered, one per settings row; windowed below
	row := func(i int, label, value string) {
		sel := p != nil && p.cursor == i && m.focus == focList
		l := fit("  "+label, labelW)
		v := fit(value, w-labelW)
		if sel {
			rows = append(rows, sSel.Render(l+v))
		} else {
			rows = append(rows, sDim.Render(l)+sTitle.Render(v))
		}
	}
	row(rowPreset, "Эквалайзер", "‹ "+preset+" ›")
	barW := min(33, w-labelW-10)
	for i := 0; i < 10; i++ {
		f := eqFreqs[i]
		name := fmt.Sprintf("%d Гц", f)
		if f >= 1000 {
			name = fmt.Sprintf("%d кГц", f/1000)
		}
		row(rowBand0+i, "   "+name, eqBar(gains[i], barW)+fmt.Sprintf(" %+3.0f дБ", gains[i]))
	}
	row(rowNormalize, "Выравнивание громкости", onOff(m.cfg.Normalize))
	row(rowAutoplay, "Дальше играть VK Микс", onOff(m.cfg.AutoplayOn()))
	row(rowLyrics, "Текст песни (t)", onOff(m.lyrOn))
	row(rowLyricsSrc, "Источник текста", "‹ "+lyricsSources[lyricsSrcIndex(m.cfg.LyricsSrc)].name+" ›")
	row(rowIcons, "Иконки Nerd Font", onOff(m.cfg.NerdIconsOn())+"  · нужен шрифт с nerdfonts.com, иначе будут квадратики")
	row(rowCache, "Кэш прослушанных треков", onOff(m.cfg.CacheEnabled())+"  · повтор без интернета и мгновенно")
	used := cacheUsedMB()
	row(rowCacheSize, "Размер кэша", fmt.Sprintf("‹ %s ›  · занято %.0f МБ", mbStr(m.cfg.CacheLimitMB()), used))
	row(rowOffline, "Вся «Моя музыка» офлайн", onOff(m.cfg.OfflineAll)+"  · хранится навсегда, без интернета")
	row(rowDlDir, "Папка загрузок (Enter)", m.cfg.Downloads())
	row(rowVolStep, "Шаг громкости", fmt.Sprintf("%d%%", m.cfg.Step()))
	acc := "зелёный"
	if m.cfg.Accent == "blue" {
		acc = "синий VK"
	}
	row(rowAccent, "Цвет", acc)
	accs := ""
	if n := len(m.cfg.Accounts); n > 1 {
		accs = fmt.Sprintf("  · %d из %d, ←/→ переключить", m.cfg.ActiveIndex()+1, n)
	}
	row(rowAccount, "Аккаунт", "‹ "+m.accountLabel()+" ›"+accs)
	row(rowAddAcc, "Добавить аккаунт (Enter)", "вход по коду в этом окне, потом плеер вернётся")
	// scroll so the selected row stays visible on short terminals
	vis := max(1, h-2)
	top := m.settingsTop(p, vis)
	for i := top; i < min(len(rows), top+vis); i++ {
		b.WriteString(rows[i] + "\n")
	}
	return b.String()
}

// settingsRowsY: first settings row on screen (border + title + hint).
const settingsRowsY = 3

// ---------- accent colour ----------

var (
	greenAccent = lipgloss.AdaptiveColor{Light: "#148a3d", Dark: "#1DB954"}
	blueAccent  = lipgloss.AdaptiveColor{Light: "#2a5885", Dark: "#71AAEB"}
)

func applyAccent(name string) {
	c := greenAccent
	if name == "blue" {
		c = blueAccent
	}
	cGreen = c
	sGreen = lipgloss.NewStyle().Foreground(cGreen)
	sGreenB = lipgloss.NewStyle().Foreground(cGreen).Bold(true)
}

func mbStr(mb int) string {
	if mb >= 1024 {
		return fmt.Sprintf("%d ГБ", mb/1024)
	}
	return fmt.Sprintf("%d МБ", mb)
}

var (
	cacheUsed   float64
	cacheUsedAt time.Time
)

// cacheUsedMB is the cache size, re-read at most every few seconds (the view redraws often).
func cacheUsedMB() float64 {
	if time.Since(cacheUsedAt) > 3*time.Second {
		cacheUsed = float64(player.DirSize(config.CacheDir())) / (1 << 20)
		cacheUsedAt = time.Now()
	}
	return cacheUsed
}

// settingsTop is the first visible settings row for a window of vis rows.
func (m *Model) settingsTop(p *page, vis int) int {
	if p == nil {
		return 0
	}
	if p.cursor < p.top {
		p.top = p.cursor
	}
	if p.cursor >= p.top+vis {
		p.top = p.cursor - vis + 1
	}
	p.top = max(0, min(p.top, max(0, settingsRows-vis)))
	return p.top
}
