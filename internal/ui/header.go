package ui

import (
	"fmt"
	"math/rand"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

const (
	btnPlayAll = "► Играть"
	btnShuffle = "Перемешать"
	btnRefresh = "Обновить"
)

type headerAction int

const (
	actPlay headerAction = iota
	actShuffle
	actRefresh
	actOffline
)

type headerButton struct {
	label string
	act   headerAction
	x     [2]int // screen x range, row y = 1
}

// hasPlayButtons: track lists get "Играть" / "Перемешать" in their header.
func (p *page) hasPlayButtons() bool {
	return p != nil && !p.isLists && !p.settings && !p.queue && p.length() > 0
}

// headerButtons lists the header buttons of a page with their screen positions.
func (m *Model) headerButtons(p *page) []headerButton {
	if !p.hasPlayButtons() {
		return nil
	}
	var bs []headerButton
	if p.mix {
		bs = []headerButton{{label: btnPlayAll, act: actPlay}, {label: btnRefresh, act: actRefresh}}
	} else {
		bs = []headerButton{{label: btnPlayAll, act: actPlay}, {label: btnShuffle, act: actShuffle}}
	}
	if p.myMusic && !p.offlineMode {
		lbl := "↓ Сохранить офлайн"
		if m.cfg.OfflineAll {
			lbl = fmt.Sprintf("↓ Офлайн %d/%d", m.offlineCount(p), p.length())
		}
		bs = append(bs, headerButton{label: lbl, act: actOffline})
	}
	// keep only the buttons that fit between the title and the counter
	inner := m.listW() - 2
	if m.lyrW() == m.mainW() {
		inner = m.mainW() - 2
	}
	limit := sideW + 1 + inner - ansi.StringWidth(m.headerCount(p))
	x := sideW + 1 + ansi.StringWidth(clean(" "+p.title)) + 3
	for i := range bs {
		w := ansi.StringWidth(bs[i].label) + 2
		if x+w > limit {
			return bs[:i]
		}
		bs[i].x = [2]int{x, x + w}
		x += w + 1
	}
	return bs
}

func (m *Model) runHeader(p *page, a headerAction) tea.Cmd {
	switch a {
	case actPlay:
		return m.playAll(p, false)
	case actShuffle:
		return m.playAll(p, true)
	case actRefresh:
		return m.reloadSection()
	case actOffline:
		m.toggleOfflineAll()
	}
	return nil
}

// viewHeader draws the list title, the header buttons and the counter.
func (m Model) viewHeader(p *page, w int, cnt string) string {
	cw := ansi.StringWidth(cnt)
	title := clean(" " + p.title)
	bs := m.headerButtons(p)
	if len(bs) == 0 {
		return sTitle.Render(fit(title, w-cw)) + sFaint.Render(cnt)
	}
	btns, bw := "  ", 2
	for i, b := range bs {
		st := sTitle
		if i == 0 {
			st = sGreenB
		}
		btns += " " + st.Render("["+b.label+"]")
		bw += 1 + ansi.StringWidth(b.label) + 2
	}
	tw := ansi.StringWidth(title)
	return sTitle.Render(title) + btns + fit("", w-tw-bw-cw) + sFaint.Render(cnt)
}

// playAll starts the list from the top; shuffled=true turns shuffle on and
// starts from a random track.
func (m *Model) playAll(p *page, shuffled bool) tea.Cmd {
	if !p.hasPlayButtons() {
		return nil
	}
	if p.mix {
		shuffled = false // the mix is already a random stream
	}
	if m.shuffle != shuffled {
		m.shuffle = shuffled
		m.cfg.Shuffle = shuffled
		_ = m.cfg.Save()
	}
	start := 0
	if shuffled {
		start = rand.Intn(p.length())
		m.setStatus("Перемешано: "+p.title, false)
	}
	return m.startQueue(p, start)
}
