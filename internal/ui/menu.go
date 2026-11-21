package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// menu is a popup list drawn over the UI:
//
//	╭─ Эквалайзер ───────╮
//	│ ● Выкл             │
//	│   Бас+             │
//	│   Рок              │
//	╰─ Enter · Esc ──────╯
type menu struct {
	title string
	items []string
	sel   int // highlighted item
	cur   int // item that is currently active (marked ●), -1 = none
	top   int
	pick  func(m *Model, i int) tea.Cmd
}

func (m *Model) openMenu(title string, items []string, cur int, pick func(m *Model, i int) tea.Cmd) {
	if cur < 0 || cur >= len(items) {
		cur = -1
	}
	sel := max(0, cur)
	m.menu = &menu{title: title, items: items, sel: sel, cur: cur, pick: pick}
}

// geometry: inner width/height and the top-left corner on screen
func (m *Model) menuRect() (x, y, w, h int) {
	mn := m.menu
	w = ansi.StringWidth(mn.title) + 6
	for _, it := range mn.items {
		w = max(w, ansi.StringWidth(clean(it))+6)
	}
	w = min(w, m.w-4)
	h = min(len(mn.items), max(3, m.h-6))
	x = (m.w - (w + 2)) / 2
	y = max(1, (m.h-(h+2))/2)
	return
}

func (m *Model) menuVisible() (first, n int) {
	_, _, _, h := m.menuRect()
	mn := m.menu
	if mn.sel < mn.top {
		mn.top = mn.sel
	}
	if mn.sel >= mn.top+h {
		mn.top = mn.sel - h + 1
	}
	return mn.top, h
}

func (m *Model) menuKey(key string) tea.Cmd {
	mn := m.menu
	switch key {
	case "up", "k":
		mn.sel = (mn.sel + len(mn.items) - 1) % len(mn.items)
	case "down", "j":
		mn.sel = (mn.sel + 1) % len(mn.items)
	case "home", "g":
		mn.sel = 0
	case "end", "G":
		mn.sel = len(mn.items) - 1
	case "enter", " ", "space", "right", "l":
		return m.menuPick(mn.sel)
	case "esc", "q", "left", "h", "backspace":
		m.menu = nil
	}
	return nil
}

func (m *Model) menuPick(i int) tea.Cmd {
	mn := m.menu
	m.menu = nil
	if mn.pick == nil || i < 0 || i >= len(mn.items) {
		return nil
	}
	return mn.pick(m, i)
}

func (m *Model) menuMouse(e tea.MouseMsg) tea.Cmd {
	x, y, w, _ := m.menuRect()
	first, n := m.menuVisible()
	switch e.Button {
	case tea.MouseButtonWheelUp:
		m.menu.sel = max(0, m.menu.sel-1)
		return nil
	case tea.MouseButtonWheelDown:
		m.menu.sel = min(len(m.menu.items)-1, m.menu.sel+1)
		return nil
	}
	if e.Action != tea.MouseActionPress || e.Button != tea.MouseButtonLeft {
		return nil
	}
	if e.X < x || e.X > x+w+1 || e.Y < y || e.Y > y+n+1 {
		m.menu = nil // click outside closes
		return nil
	}
	row := e.Y - y - 1
	if row >= 0 && row < n && first+row < len(m.menu.items) {
		return m.menuPick(first + row)
	}
	return nil
}

// menuBox renders the popup lines (each exactly w+2 cells wide).
func (m *Model) menuBox() []string {
	_, _, w, _ := m.menuRect()
	mn := m.menu
	first, n := m.menuVisible()
	border := sGreen
	title := " " + mn.title + " "
	topLine := border.Render("╭─") + sTitle.Render(title) + border.Render(strings.Repeat("─", max(0, w-1-ansi.StringWidth(title)))+"╮")
	lines := []string{topLine}
	for i := first; i < first+n && i < len(mn.items); i++ {
		mark := "  "
		if i == mn.cur {
			mark = "● "
		}
		text := fit(" "+mark+mn.items[i], w)
		if i == mn.sel {
			text = sSel.Render(text)
		} else if i == mn.cur {
			text = sGreen.Render(text)
		} else {
			text = sTitle.UnsetBold().Render(text)
		}
		lines = append(lines, border.Render("│")+text+border.Render("│"))
	}
	hint := " Enter · Esc "
	if len(mn.items) > n {
		hint = " ↑↓ ещё · Enter · Esc "
	}
	lines = append(lines, border.Render("╰─")+sFaint.Render(hint)+border.Render(strings.Repeat("─", max(0, w-1-ansi.StringWidth(hint)))+"╯"))
	return lines
}

// overlayMenu paints the popup over already rendered screen lines.
func (m *Model) overlayMenu(lines []string) []string {
	if m.menu == nil {
		return lines
	}
	x, y, w, _ := m.menuRect()
	for i, bl := range m.menuBox() {
		row := y + i
		if row < 0 || row >= len(lines) {
			continue
		}
		base := lines[row]
		left := ansi.Truncate(base, x, "")
		if pad := x - ansi.StringWidth(left); pad > 0 {
			left += strings.Repeat(" ", pad)
		}
		right := ansi.TruncateLeft(base, x+w+2, "")
		lines[row] = left + "\x1b[0m" + bl + "\x1b[0m" + right
	}
	return lines
}
