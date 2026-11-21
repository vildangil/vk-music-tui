package ui

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"vktui/internal/config"
	"vktui/internal/lyrics"
	"vktui/internal/vk"
)

type lyricsMsg struct {
	key string
	l   *lyrics.Lyrics
	err error
}

func newFinder(api *vk.Client, src string) *lyrics.Finder {
	vkf := func(ctx context.Context, key string) ([]lyrics.Line, []string, error) {
		ts, text, err := api.Lyrics(ctx, key)
		if err != nil {
			return nil, nil, err
		}
		var out []lyrics.Line
		for _, t := range ts {
			out = append(out, lyrics.Line{At: float64(t.Begin) / 1000, Text: t.Line})
		}
		return out, text, nil
	}
	hc := lyrics.NewHTTP()
	return &lyrics.Finder{
		Providers: pickProviders(src, lyrics.VK{Fetch: vkf}, lyrics.LRCLIB{HTTP: hc}, lyrics.Genius{HTTP: hc}),
		CacheDir:  filepath.Join(config.Dir(), "lyrics", srcDir(src)),
	}
}

// fetchLyrics loads lyrics for the playing track if the panel is open.
func (m *Model) fetchLyrics() tea.Cmd {
	if !m.lyrOn || m.playing == nil || m.finder == nil {
		return nil
	}
	t := *m.playing
	key := t.Key()
	if key == m.lyrKey {
		return nil
	}
	m.lyrKey, m.lyr, m.lyrErr, m.lyrLoading, m.lyrScroll = key, nil, "", true, 0
	m.lyrManualAt = time.Time{}
	f := m.finder
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		l, err := f.Find(ctx, lyrics.Query{Key: key, Artist: t.Artist, Title: t.Title, Duration: t.Duration})
		return lyricsMsg{key: key, l: l, err: err}
	}
}

func (m *Model) toggleLyrics() tea.Cmd {
	m.lyrOn = !m.lyrOn
	m.cfg.Lyrics = m.lyrOn
	_ = m.cfg.Save()
	if !m.lyrOn {
		return nil
	}
	m.lyrKey = "" // force a (cached) reload for the current track
	return m.fetchLyrics()
}

// ---------- geometry ----------

// lyrW is the lyrics panel width incl. borders (0 = closed). On narrow
// terminals the panel takes the whole main area instead of squeezing the list.
func (m *Model) lyrW() int {
	if !m.lyrOn {
		return 0
	}
	mw := m.mainW()
	if mw < 72 {
		return mw
	}
	return max(30, min(64, mw*2/5))
}

// listW is the track list box width incl. borders (0 = hidden).
func (m *Model) listW() int { return m.mainW() - m.lyrW() }

type lyrRow struct {
	text string
	line int // index into Synced / Plain
}

func lyricsErrText(err error) string {
	if errors.Is(err, lyrics.ErrNotFound) {
		return ""
	}
	return "Не удалось загрузить текст: " + err.Error()
}

// ---------- queue view ----------

// syncQueuePage fills the queue page with the playing track and what follows,
// in real play order (shuffled order when shuffle is on).
func (m *Model) syncQueuePage(p *page) {
	p.tracks = p.tracks[:0]
	if m.opos < 0 || m.playing == nil {
		return
	}
	for i := m.opos; i < len(m.order); i++ {
		p.tracks = append(p.tracks, m.queue[m.order[i]])
	}
	p.total = len(p.tracks)
}

func (m *Model) openQueue() tea.Cmd {
	if cur := m.cur(); cur != nil && cur.queue {
		// toggle back to where we were
		if len(m.stack) > 1 {
			m.stack = m.stack[:len(m.stack)-1]
		}
		return nil
	}
	p := &page{title: "Очередь", queue: true, done: true}
	m.syncQueuePage(p)
	m.stack = append(m.stack, p)
	m.focus = focList
	return nil
}
