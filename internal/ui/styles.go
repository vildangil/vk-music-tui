package ui

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/text/unicode/norm"

	"vktui/internal/vk"
)

var (
	cGreen  = lipgloss.AdaptiveColor{Light: "#148a3d", Dark: "#1DB954"}
	cText   = lipgloss.AdaptiveColor{Light: "#111111", Dark: "#FFFFFF"}
	cDim    = lipgloss.AdaptiveColor{Light: "#666666", Dark: "#B3B3B3"}
	cFaint  = lipgloss.AdaptiveColor{Light: "#999999", Dark: "#535353"}
	cRed    = lipgloss.AdaptiveColor{Light: "#c0392b", Dark: "#F15E6C"}
	cSelBg  = lipgloss.AdaptiveColor{Light: "#DDDDDD", Dark: "#2A2A2A"}
	cAccent = lipgloss.AdaptiveColor{Light: "#3a5f9a", Dark: "#5181B8"} // VK blue

	sTitle   = lipgloss.NewStyle().Bold(true).Foreground(cText)
	sDim     = lipgloss.NewStyle().Foreground(cDim)
	sFaint   = lipgloss.NewStyle().Foreground(cFaint)
	sGreen   = lipgloss.NewStyle().Foreground(cGreen)
	sGreenB  = lipgloss.NewStyle().Foreground(cGreen).Bold(true)
	sErr     = lipgloss.NewStyle().Foreground(cRed)
	sSel     = lipgloss.NewStyle().Background(cSelBg).Foreground(cText).Bold(true)
	sSelDim  = lipgloss.NewStyle().Background(cSelBg).Foreground(cDim)
	sLogo    = lipgloss.NewStyle().Bold(true).Foreground(cAccent)
	sKey     = lipgloss.NewStyle().Foreground(cText).Bold(true)
	sBoxBase = lipgloss.NewStyle().Border(lipgloss.RoundedBorder())
)

func box(focused bool) lipgloss.Style {
	if focused {
		return sBoxBase.BorderForeground(cGreen)
	}
	return sBoxBase.BorderForeground(cFaint)
}

// clean removes characters whose on-screen width terminals disagree about
// (emoji, variation selectors, zero-width joiners, combining marks, control chars).
// One miscounted cell makes a line wrap and the whole UI "drift".
func clean(s string) string {
	if s == "" {
		return s
	}
	s = norm.NFC.String(s)
	var b strings.Builder
	b.Grow(len(s))
	dropped, lastSpace := false, false
	for _, r := range s {
		if r == 0x09 || r == 0x0a || r == 0x0d {
			r = ' '
		}
		switch {
		case r < 0x20 || (r >= 0x7f && r < 0xa0),
			r == 0x00ad, r >= 0x200b && r <= 0x200f, r >= 0x2028 && r <= 0x202e, r >= 0x2060 && r <= 0x206f, r == 0xfeff,
			r >= 0xfe00 && r <= 0xfe0f, r >= 0xe0000 && r <= 0xe01ef,
			r >= 0x1f000 && r <= 0x1faff, r >= 0x2600 && r <= 0x27bf, r >= 0x2b00 && r <= 0x2bff, r >= 0x2300 && r <= 0x23ff,
			unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf, unicode.Cs):
			dropped = true
			continue
		}
		if r == ' ' && dropped && (b.Len() == 0 || lastSpace) {
			continue // don't leave double/leading spaces where an emoji was
		}
		b.WriteRune(r)
		lastSpace = r == ' '
		if !lastSpace {
			dropped = false
		}
	}
	return b.String()
}

// fit truncates or pads s to exactly w cells, measuring like lipgloss does.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = clean(s)
	if ansi.StringWidth(s) > w {
		s = ansi.Truncate(s, w, "…")
	}
	if pad := w - ansi.StringWidth(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

func fmtDur(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	s := int(sec)
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func progressBar(pos, dur float64, w int) string {
	if w < 3 {
		return ""
	}
	frac := 0.0
	if dur > 0 {
		frac = pos / dur
	}
	if frac > 1 {
		frac = 1
	}
	if frac < 0 {
		frac = 0
	}
	filled := int(frac * float64(w-1))
	return sGreen.Render(strings.Repeat("━", filled)) + sTitle.Render("●") + sFaint.Render(strings.Repeat("─", w-1-filled))
}

func mediaTitle(t vk.Track) string { return t.Title + " — " + t.Artist }
