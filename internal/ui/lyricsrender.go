package ui

import (
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"vktui/internal/lyrics"
)

// lyrManualHold: after a manual scroll, synced lyrics stop following the song for a while.
const lyrManualHold = 10 * time.Second

// syncedLines returns the synced lines, with a leading "pause" line when the
// singing starts late (an intro), so the dots show during the intro too.
func (m *Model) syncedLines() []lyrics.Line {
	ls := m.lyr.Synced
	if len(ls) > 0 && ls[0].At > 3 {
		ls = append([]lyrics.Line{{At: 0}}, ls...)
	}
	return ls
}

func isPause(l lyrics.Line) bool { return strings.TrimSpace(l.Text) == "" }

// lyricRows wraps the lyrics to width w and returns the rows, the current
// line (-1 if not synced) and the first visible row for a body of height h.
func (m *Model) lyricRows(w, h int) (rows []lyrRow, cur, top int) {
	l := m.lyr
	cur = -1
	if l.IsSynced() {
		lines := m.syncedLines()
		pos := m.shownPos() + 0.15
		for i, ln := range lines {
			if ln.At <= pos {
				cur = i
			}
		}
		for i, ln := range lines {
			if isPause(ln) {
				rows = append(rows, lyrRow{"·  ·  ·", i})
				continue
			}
			for _, r := range strings.Split(ansi.Wrap(clean(ln.Text), w, ""), "\n") {
				rows = append(rows, lyrRow{r, i})
			}
		}
	} else {
		for i, ln := range l.Plain {
			for _, r := range strings.Split(ansi.Wrap(clean(ln), w, ""), "\n") {
				rows = append(rows, lyrRow{r, i})
			}
		}
	}
	maxTop := max(0, len(rows)-h)
	follow := l.IsSynced() && time.Since(m.lyrManualAt) > lyrManualHold
	if follow && cur >= 0 {
		first := 0
		for i, r := range rows {
			if r.line == cur {
				first = i
				break
			}
		}
		top = first - h/3 // keep the current line in the upper third, like Spotify
	} else {
		top = m.lyrScroll
	}
	top = max(0, min(maxTop, top))
	return rows, cur, top
}

// pauseDots draws the three dots of an instrumental pause. They grow one
// after another over the length of the pause: · → • → ●.
func (m *Model) pauseDots(lines []lyrics.Line, i int) string {
	start := lines[i].At
	end := m.trackDur()
	if i+1 < len(lines) {
		end = lines[i+1].At
	}
	f := 0.0
	if end > start {
		f = (m.shownPos() - start) / (end - start)
	}
	f = math.Max(0, math.Min(1, f))
	glyph := []string{"·", "•", "●"}
	var dots []string
	for k := 0; k < 3; k++ {
		local := math.Max(0, math.Min(1, f*3-float64(k))) // this dot's own progress
		dots = append(dots, glyph[min(2, int(local*3))])
	}
	return strings.Join(dots, "  ")
}

// lyrBody returns the text width and visible height of the lyrics panel body.
func (m *Model) lyrBody() (w, h int) { return m.lyrW() - 5, m.mainH() - 4 }

// scrollLyrics scrolls the lyrics panel by d rows (wheel, "[" / "]").
func (m *Model) scrollLyrics(d int) {
	if !m.lyrOn || m.lyr == nil || m.lyr.Empty() {
		return
	}
	w, h := m.lyrBody()
	rows, _, top := m.lyricRows(w, h)
	m.lyrScroll = max(0, min(len(rows)-h, top+d))
	m.lyrManualAt = time.Now()
}

// ---------- view ----------

func (m Model) viewLyrics(w, h int) string {
	var b strings.Builder
	head := " Текст"
	manual := false
	if m.lyr != nil && !m.lyr.Empty() {
		head += " · " + m.lyr.Source
		if m.lyr.IsSynced() {
			head += " · синхронный"
			manual = time.Since(m.lyrManualAt) < lyrManualHold
		}
	}
	b.WriteString(sTitle.Render(fit(head, w)) + "\n")
	if manual {
		b.WriteString(sFaint.Render(fit(" прокрутка вручную · c — к текущей строке", w)) + "\n")
	} else {
		b.WriteString("\n")
	}
	bodyH := h - 2
	msg := func(s string) string {
		b.WriteString(sDim.Render(fit(" "+s, w)))
		return b.String()
	}
	switch {
	case m.playing == nil:
		return msg("Включи трек — здесь появится текст")
	case m.lyrLoading:
		return msg("Ищу текст…")
	case m.lyrErr != "":
		return msg(m.lyrErr)
	case m.lyr == nil || m.lyr.Empty():
		return msg("Текст не найден (VK, LRCLIB, Genius)")
	}
	rows, cur, top := m.lyricRows(w-3, bodyH)
	var lines []lyrics.Line
	if m.lyr.IsSynced() {
		lines = m.syncedLines()
	}
	end := min(len(rows), top+bodyH)
	for i := top; i < end; i++ {
		r := rows[i]
		text := r.text
		if r.line == cur && lines != nil && isPause(lines[cur]) {
			text = m.pauseDots(lines, cur)
		}
		line := " " + fit(text, w-3)
		// scrollbar on the right edge when the text doesn't fit
		bar := " "
		if len(rows) > bodyH {
			thumb := top * bodyH / len(rows)
			size := max(1, bodyH*bodyH/len(rows))
			if i-top >= thumb && i-top < thumb+size {
				bar = sFaint.Render("█")
			} else {
				bar = sFaint.Render("│")
			}
		}
		switch {
		case cur < 0:
			b.WriteString(sTitle.UnsetBold().Render(line))
		case r.line == cur:
			b.WriteString(sGreenB.Render(line))
		case r.line < cur:
			b.WriteString(sFaint.Render(line))
		default:
			b.WriteString(sDim.Render(line))
		}
		b.WriteString(" " + bar + "\n")
	}
	return b.String()
}

// ---------- mouse ----------

// lyricsMouse handles the wheel (scroll) and clicks (seek to a synced line).
func (m Model) lyricsMouse(e tea.MouseMsg, wheel int) (tea.Model, tea.Cmd) {
	if e.Y == 1 && wheel == 0 && e.Action == tea.MouseActionPress {
		m.lyricsSrcMenu() // click on the panel title: choose where lyrics come from
		return m, nil
	}
	if m.lyr == nil || m.lyr.Empty() {
		return m, nil
	}
	if wheel != 0 {
		m.scrollLyrics(wheel * 3)
		return m, nil
	}
	bw, bodyH := m.lyrBody()
	rows, _, top := m.lyricRows(bw, bodyH)
	i := top + (e.Y - 3) // border, header, hint line
	if !m.lyr.IsSynced() || i < top || i >= len(rows) {
		return m, nil
	}
	m.pl.SeekAbs(m.syncedLines()[rows[i].line].At)
	m.lyrManualAt = time.Time{}
	return m, nil
}
