package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Screen geometry. View and mouse hit-testing both use these, so they never disagree.
const (
	sideW = 22 // sidebar width including borders
	barH  = 4  // player box height including borders
	helpH = 1
)

func (m *Model) mainH() int { return m.h - barH - helpH } // height of the top boxes incl. borders
func (m *Model) mainW() int { return m.w - sideW }

// rowsY is the screen row of the first list row inside the main box.
func (m *Model) rowsY() int {
	y := 1 + 1 + 1 // top border, title, column header
	if m.side == int(secSearch) {
		y += 2
	}
	return y
}

// ---------- view ----------

func (m Model) View() string {
	if m.w < 50 || m.h < 14 {
		return "Окно слишком маленькое (нужно хотя бы 50×14)"
	}
	mh := m.mainH()
	side := clip(m.viewSide(sideW-2, mh-2), mh-2)
	boxes := []string{box(m.focus == focSide).Width(sideW - 2).Height(mh - 2).Render(side)}
	if lw := m.listW(); lw > 0 {
		main := clip(m.viewMain(lw-2, mh-2), mh-2)
		boxes = append(boxes, box(m.focus != focSide).Width(lw-2).Height(mh-2).Render(main))
	}
	if yw := m.lyrW(); yw > 0 {
		ly := clip(m.viewLyrics(yw-2, mh-2), mh-2)
		boxes = append(boxes, box(false).Width(yw-2).Height(mh-2).Render(ly))
	}
	top := lipgloss.JoinHorizontal(lipgloss.Top, boxes...)
	bar := box(false).Width(m.w - 2).Render(m.viewPlayer(m.w - 2))
	out := lipgloss.JoinVertical(lipgloss.Left, top, bar, m.viewHelp())

	// Hard guarantee: exactly m.h lines, none wider than the terminal.
	// A single overlong line wraps and makes the whole screen scroll ("drift").
	lines := strings.Split(out, "\n")
	if len(lines) > m.h {
		lines = lines[:m.h]
	}
	lines = m.overlayMenu(lines)
	for i, l := range lines {
		if ansi.StringWidth(l) > m.w {
			lines[i] = ansi.Truncate(l, m.w, "")
		}
	}
	return strings.Join(lines, "\n")
}

func clip(s string, h int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
}

type playerLayout struct {
	prev, play, next [2]int // x ranges [from, to) on line 1
	lyrics           [2]int
	shuffle, repeat  [2]int
	volDown, volUp   [2]int
	barX, barW       int // progress bar on line 2
}

// layoutPlayer computes positions inside the player box (inner width w, x offset 1).
// playerLabels are the texts of the toggles on the right of the player bar.
type playerLabels struct{ lyrics, shuffle, repeat, vol string }

func (m *Model) playerLabels() playerLabels {
	ic := m.icons()
	rp := "repeat"
	if m.repeat == 2 {
		rp = "repeat 1"
	}
	return playerLabels{
		lyrics:  ic.lyrics + "текст",
		shuffle: ic.shuffle + "shuffle",
		repeat:  ic.repeat + rp,
		vol:     ic.volume + fmt.Sprintf("%3d%%", m.cfg.Volume),
	}
}

// layoutPlayer computes positions inside the player box (inner width w, x offset 1).
func (m *Model) layoutPlayer(w int) (playerLayout, playerLabels, string) {
	var L playerLayout
	lb := m.playerLabels()
	sw := ansi.StringWidth
	// right side: "текст  shuffle  repeat  − 70% + "
	right := lb.lyrics + "  " + lb.shuffle + "  " + lb.repeat + "  − " + lb.vol + " + "
	x := 1 + w - sw(right)
	L.lyrics = [2]int{x, x + sw(lb.lyrics)}
	x += sw(lb.lyrics) + 2
	L.shuffle = [2]int{x, x + sw(lb.shuffle)}
	x += sw(lb.shuffle) + 2
	L.repeat = [2]int{x, x + sw(lb.repeat)}
	x += sw(lb.repeat) + 2
	L.volDown = [2]int{x, x + 2}
	x += 2 + sw(lb.vol) + 1
	L.volUp = [2]int{x - 1, x + 2}
	// left side: " |◀ ▶  ▶| " — every button is 2 cells
	L.prev = [2]int{2, 4}
	L.play = [2]int{5, 7}
	L.next = [2]int{8, 10}
	return L, lb, right
}

func (m Model) viewPlayer(w int) string {
	st := m.pl.State()
	_, lb, right := m.layoutPlayer(w)
	ic := m.icons()

	play := ic.play
	if m.playing != nil && !st.Paused {
		play = ic.pause
	}
	buttons := " " + sTitle.Render(ic.prev) + " " + sGreenB.Render(play) + " " + sTitle.Render(ic.next) + "  "
	btnW := 11

	title := sDim.Render(fit("Ничего не играет", 16))
	plainTitle := ""
	if m.playing != nil {
		plainTitle = clean(m.playing.Title) + " — " + clean(m.playing.Artist)
	}
	leftW := w - ansi.StringWidth(right) - btnW - 1
	if m.playing != nil {
		t := fit(plainTitle, leftW)
		// colour title and artist differently when the whole thing fits
		if ansi.StringWidth(plainTitle) <= leftW {
			ct := clean(m.playing.Title)
			t = sTitle.Render(ct) + sDim.Render(strings.TrimPrefix(t, ct))
		} else {
			t = sTitle.Render(t)
		}
		title = t
	}
	pad := w - btnW - ansi.StringWidth(title) - ansi.StringWidth(right)
	if pad < 0 {
		pad = 0
	}

	on := func(b bool, s string) string {
		if b {
			return sGreen.Render(s)
		}
		return sFaint.Render(s)
	}
	rightStyled := on(m.lyrOn, lb.lyrics) + "  " + on(m.shuffle, lb.shuffle) + "  " + on(m.repeat > 0, lb.repeat) +
		"  " + sTitle.Render("−") + " " + sDim.Render(lb.vol) + " " + sTitle.Render("+") + " "
	line1 := buttons + title + strings.Repeat(" ", pad) + rightStyled

	dur := m.trackDur()
	pos := m.shownPos()
	l, r := " "+fmtDur(pos)+" ", " "+fmtDur(dur)+" "
	line2 := sDim.Render(l) + progressBar(pos, dur, w-len(l)-len(r)) + sDim.Render(r)
	return line1 + "\n" + line2
}

// ---------- mouse ----------

func (m Model) handleMouse(e tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.menu != nil {
		return m, m.menuMouse(e)
	}
	mh := m.mainH()
	wheel := 0
	switch e.Button {
	case tea.MouseButtonWheelUp:
		wheel = -1
	case tea.MouseButtonWheelDown:
		wheel = 1
	}
	// dragging the progress knob: follow the mouse, seek when released
	if m.dragging && e.Action == tea.MouseActionPress && wheel == 0 {
		m.dragging = false // the release got lost (e.g. outside the window): drop the drag
	}
	if m.dragging {
		x, w, dur := m.barGeom()
		frac := float64(e.X-x) / float64(max(1, w-1))
		m.dragFrac = math.Max(0, math.Min(1, frac))
		if e.Action == tea.MouseActionRelease {
			m.dragging = false
			if dur > 0 {
				m.pl.SeekAbs(m.dragFrac * dur)
			}
		}
		return m, nil
	}
	if wheel == 0 && (e.Action != tea.MouseActionPress || e.Button != tea.MouseButtonLeft) {
		return m, nil
	}

	switch {
	// player box
	case e.Y >= mh && e.Y < mh+barH:
		if wheel != 0 {
			return m, m.changeVolume(-wheel * m.cfg.Step())
		}
		L, _, _ := m.layoutPlayer(m.w - 2)
		in := func(r [2]int) bool { return e.X >= r[0] && e.X < r[1] }
		if e.Y == mh+1 {
			switch {
			case in(L.prev):
				return m, m.prev()
			case in(L.play):
				return m.togglePlay()
			case in(L.next):
				return m, m.next(false)
			case in(L.lyrics):
				return m, m.toggleLyrics()
			case in(L.shuffle):
				m.toggleShuffle()
			case in(L.repeat):
				m.cycleRepeat()
			case in(L.volDown):
				return m, m.changeVolume(-m.cfg.Step())
			case in(L.volUp):
				return m, m.changeVolume(m.cfg.Step())
			}
			return m, nil
		}
		if e.Y == mh+2 && m.playing != nil {
			barX, barW, dur := m.barGeom()
			// grab anywhere on the bar (±1 cell of slack at the ends)
			if dur > 0 && barW > 1 && e.X >= barX-1 && e.X <= barX+barW {
				m.dragging = true
				m.dragFrac = math.Max(0, math.Min(1, float64(e.X-barX)/float64(barW-1)))
			}
		}
		return m, nil

	// sidebar
	case e.Y < mh && e.X < sideW:
		if wheel != 0 {
			return m, nil
		}
		i := e.Y - 4 // border, logo, name, blank
		if i >= 0 && i < len(sectionNames) {
			m.focus = focList
			return m, m.openSection(section(i))
		}
		if i == len(sectionNames)+1 && m.playing != nil { // "Очередь: N/M"
			return m, m.openQueue()
		}
		return m, nil

	// lyrics panel (right of the list)
	case e.Y < mh && e.X >= sideW+m.listW():
		return m.lyricsMouse(e, wheel)

	// main list
	case e.Y < mh:
		p := m.cur()
		if p == nil {
			return m, nil
		}
		if p.settings {
			row := e.Y - settingsRowsY + p.top
			if wheel != 0 {
				p.cursor = (p.cursor + wheel + settingsRows) % settingsRows
				return m, nil
			}
			if row < 0 || row >= settingsRows {
				return m, nil
			}
			m.focus = focList
			p.cursor = row
			// click on the left half of the value lowers it, right half raises it
			d := 1
			if e.X < sideW+1+26+(m.listW()-2-26)/3 {
				d = -1
			}
			if m.settingsMenu(row) {
				return m, nil
			}
			return m, m.changeSetting(row, d)
		}
		if wheel != 0 {
			return m, m.scroll(wheel * 3)
		}
		if e.Y == 1 && wheel == 0 && p.hasPlayButtons() {
			for _, b := range m.headerButtons(p) {
				if e.X >= b.x[0] && e.X < b.x[1] {
					return m, m.runHeader(p, b.act)
				}
			}
			return m, nil
		}
		if m.side == int(secSearch) && (e.Y == 2 || e.Y == 3) {
			m.focus = focSearch
			return m, m.input.Focus()
		}
		row := e.Y - m.rowsY()
		if row < 0 || p.length() == 0 {
			return m, nil
		}
		idx := p.top + row
		if idx >= p.length() || row >= m.listHeight() {
			return m, nil
		}
		m.focus = focList
		m.input.Blur()
		double := idx == m.lastClick && time.Since(m.lastClickAt) < 450*time.Millisecond
		m.lastClick, m.lastClickAt = idx, time.Now()
		p.cursor = idx
		if double {
			m.lastClick = -1
			return m.activate()
		}
		return m, m.maybeLoadMore()
	}
	return m, nil
}

// trackDur is the duration to show: mpv's once known, VK's before that.
func (m *Model) trackDur() float64 {
	if m.playing == nil {
		return 0
	}
	if d := m.pl.State().Dur; d > 0 {
		return d
	}
	return float64(m.playing.Duration)
}

// shownPos is the playback position to draw (the drag position while dragging).
func (m *Model) shownPos() float64 {
	if m.playing == nil {
		return 0
	}
	if m.dragging {
		return m.dragFrac * m.trackDur()
	}
	return m.pl.State().Pos
}

// barGeom returns the progress bar's screen x, width and the track duration.
func (m *Model) barGeom() (x, w int, dur float64) {
	dur = m.trackDur()
	l := len(" " + fmtDur(m.shownPos()) + " ")
	r := len(" " + fmtDur(dur) + " ")
	return 1 + l, m.w - 2 - l - r, dur
}

// scroll moves the viewport (and the cursor with it) by d rows.
func (m *Model) scroll(d int) tea.Cmd {
	p := m.cur()
	if p == nil || p.length() == 0 {
		return nil
	}
	h := m.listHeight()
	maxTop := max(0, p.length()-h)
	p.top = min(max(0, p.top+d), maxTop)
	if p.cursor < p.top {
		p.cursor = p.top
	}
	if p.cursor >= p.top+h {
		p.cursor = p.top + h - 1
	}
	return m.maybeLoadMore()
}
