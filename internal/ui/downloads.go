package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"vktui/internal/config"
	"vktui/internal/player"
	"vktui/internal/vk"
)

type dlMsg struct{ ev player.DownloadEvent }

func (m Model) waitDownload() tea.Cmd {
	if m.dl == nil {
		return nil
	}
	ch := m.dl.Events
	return func() tea.Msg { return dlMsg{<-ch} }
}

// ---------- index of downloaded files (track key → path) ----------

func dlIndexPath() string { return filepath.Join(config.Dir(), "downloads.json") }

func loadDownloadIndex() map[string]string {
	idx := map[string]string{}
	if b, err := os.ReadFile(dlIndexPath()); err == nil {
		_ = json.Unmarshal(b, &idx)
	}
	return idx
}

func (m *Model) saveDownloadIndex() {
	_ = os.MkdirAll(config.Dir(), 0o700)
	b, _ := json.MarshalIndent(m.local, "", "  ")
	_ = os.WriteFile(dlIndexPath(), b, 0o600)
}

func exists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir() && st.Size() > 0
}

// isDownloaded reports whether the user downloaded the track (cheap: index only).
func (m *Model) isDownloaded(t vk.Track) bool {
	_, ok := m.local[t.Key()]
	return ok
}

// localFile returns a file to play instead of streaming: a download or a cached copy.
func (m *Model) localFile(t vk.Track) string {
	if m.offline[t.Key()] {
		if p := offlinePath(t); exists(p) {
			return p
		}
		delete(m.offline, t.Key())
	}
	if p, ok := m.local[t.Key()]; ok {
		if exists(p) {
			return p
		}
		delete(m.local, t.Key()) // the user deleted the file
		m.saveDownloadIndex()
	}
	if p := filepath.Join(config.CacheDir(), t.Key()+".mp3"); exists(p) {
		player.Touch(p)
		return p
	}
	return ""
}

// safeName makes "Artist - Title" usable as a file name on every OS.
func safeName(s string) string {
	s = clean(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case strings.ContainsRune(`<>:"/\|?*`, r), r < 32:
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Trim(strings.TrimSpace(b.String()), ".")
	if r := []rune(out); len(r) > 120 {
		out = string(r[:120])
	}
	if out == "" {
		out = "track"
	}
	return out
}

func (m *Model) job(t vk.Track, dest string, cache bool) player.DownloadJob {
	api := m.api
	return player.DownloadJob{
		Key: t.Key(), URL: t.URL, Title: t.Title, Artist: t.Artist, Album: t.AlbumTitle(), Dest: dest, Cache: cache,
		Resolve: func(ctx context.Context) (string, error) {
			nt, err := api.Resolve(ctx, t)
			return nt.URL, err
		},
	}
}

// downloadTrack saves a track to the downloads folder.
func (m *Model) downloadTrack(t vk.Track) {
	if m.dl == nil {
		m.setStatus("Для скачивания нужен mpv или ffmpeg", true)
		return
	}
	if m.isDownloaded(t) && exists(m.local[t.Key()]) {
		m.setStatus("Уже скачан: "+m.local[t.Key()], false)
		return
	}
	dest := filepath.Join(m.cfg.Downloads(), safeName(t.Artist+" - "+t.Title)+".mp3")
	if exists(dest) {
		m.local[t.Key()] = dest
		m.saveDownloadIndex()
		m.setStatus("Уже скачан: "+dest, false)
		return
	}
	if m.dl.Add(m.job(t, dest, false)) {
		m.setStatus(fmt.Sprintf("В очереди на скачивание: %s — %s", t.Artist, t.Title), false)
	}
}

// downloadAll downloads every track of a list; asks to press D twice.
func (m *Model) downloadAll(p *page) {
	if p == nil || !p.hasPlayButtons() || p.mix {
		return
	}
	if m.confirmDL != p {
		m.confirmDL = p
		m.setStatus(fmt.Sprintf("Скачать все %d треков из «%s»? Нажми Shift+D ещё раз", p.length(), p.title), false)
		return
	}
	m.confirmDL = nil
	n := 0
	for _, t := range p.tracks {
		if !m.isDownloaded(t) {
			dest := filepath.Join(m.cfg.Downloads(), safeName(t.Artist+" - "+t.Title)+".mp3")
			if m.dl.Add(m.job(t, dest, false)) {
				n++
			}
		}
	}
	m.setStatus(fmt.Sprintf("Скачиваю %d треков в %s", n, m.cfg.Downloads()), false)
}

// cacheTrack quietly keeps a played track on disk for instant / offline replays.
func (m *Model) cacheTrack(t vk.Track) {
	if m.dl == nil || !m.cfg.CacheEnabled() || !t.Playable() {
		return
	}
	dest := filepath.Join(config.CacheDir(), t.Key()+".mp3")
	if exists(dest) || m.isDownloaded(t) {
		return
	}
	m.dl.Add(m.job(t, dest, true))
}

func (m *Model) handleDownload(e player.DownloadEvent) {
	j := e.Job
	if j.Cache {
		if e.Started || e.Err != nil {
			return
		}
		if filepath.Dir(j.Dest) == config.OfflineDir() {
			m.offline[j.Key] = true
			if e.Queued == 0 && m.dl.Pending() == 0 {
				if p := m.cache[secMy]; p != nil {
					m.setStatus(fmt.Sprintf("Офлайн: сохранено %d из %d треков «Моей музыки»", m.offlineCount(p), p.length()), false)
				}
			}
			return
		}
		// a cached track that belongs to the offline library moves there
		if m.cfg.OfflineAll {
			if p := m.cache[secMy]; p != nil {
				for _, t := range p.tracks {
					if t.Key() == j.Key {
						if os.Rename(j.Dest, offlinePath(t)) == nil {
							m.offline[j.Key] = true
						}
						break
					}
				}
			}
		}
		player.TrimDir(config.CacheDir(), int64(m.cfg.CacheLimitMB())<<20)
		return
	}
	name := j.Artist + " — " + j.Title
	switch {
	case e.Started:
		more := ""
		if e.Queued > 0 {
			more = fmt.Sprintf(" (ещё %d в очереди)", e.Queued)
		}
		m.setStatus("Скачиваю: "+name+more, false)
	case e.Err != nil:
		m.setStatus("Не удалось скачать "+name+": "+e.Err.Error(), true)
	default:
		m.local[j.Key] = j.Dest
		m.saveDownloadIndex()
		if e.Queued == 0 {
			m.setStatus("Скачано: "+j.Dest, false)
		}
	}
}

func openFolder(dir string) {
	_ = os.MkdirAll(dir, 0o755)
	var c *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		c = exec.Command("explorer", dir)
	case "darwin":
		c = exec.Command("open", dir)
	default:
		c = exec.Command("xdg-open", dir)
	}
	_ = c.Start()
}
