package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"vktui/internal/config"
	"vktui/internal/vk"
)

// The offline library: with "Моя музыка офлайн" on, every track of the user's
// music is kept in config.OfflineDir() for good (the cache, in contrast, is
// trimmed). The track list itself is saved too, so the player opens and plays
// "Моя музыка" with no connection at all.

// libraryPath is per account: each one has its own "Моя музыка".
func libraryPath(uid int64) string {
	return filepath.Join(config.DataDir(), fmt.Sprintf("library-%d.json", uid))
}

func scanOffline() map[string]bool {
	set := map[string]bool{}
	entries, _ := os.ReadDir(config.OfflineDir())
	for _, e := range entries {
		if n := e.Name(); !e.IsDir() && strings.HasSuffix(n, ".mp3") {
			set[strings.TrimSuffix(n, ".mp3")] = true
		}
	}
	return set
}

func offlinePath(t vk.Track) string { return filepath.Join(config.OfflineDir(), t.Key()+".mp3") }

// availableOffline: the track can be played without a connection.
func (m *Model) availableOffline(t vk.Track) bool {
	return m.offline[t.Key()] || m.isDownloaded(t)
}

func saveLibrary(uid int64, tracks []vk.Track) {
	lib := make([]vk.Track, len(tracks))
	for i, t := range tracks {
		t.URL = "" // stream links expire anyway
		lib[i] = t
	}
	_ = os.MkdirAll(config.DataDir(), 0o755)
	b, _ := json.Marshal(lib)
	tmp := libraryPath(uid) + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, libraryPath(uid))
	}
}

func loadLibrary(uid int64) []vk.Track {
	var lib []vk.Track
	if b, err := os.ReadFile(libraryPath(uid)); err == nil {
		_ = json.Unmarshal(b, &lib)
	}
	return lib
}

// goOffline fills a failed "Моя музыка" page from the saved library, keeping
// only tracks that can actually play offline. Returns false if there's nothing.
func (m *Model) goOffline(p *page, cause error) bool {
	lib := loadLibrary(m.cfg.UserID)
	var avail []vk.Track
	for _, t := range lib {
		if m.availableOffline(t) {
			avail = append(avail, t)
		}
	}
	if len(avail) == 0 {
		return false
	}
	p.tracks, p.total, p.done, p.loading, p.err = avail, len(avail), true, false, ""
	p.offlineMode = true
	why := "нет связи с VK"
	var ae *vk.APIError
	if errors.As(cause, &ae) {
		why = "VK не отвечает (" + ae.Msg + ")"
	}
	m.setStatus(fmt.Sprintf("Офлайн: %s — доступно %d из %d треков. Ctrl+R — попробовать снова", why, len(avail), len(lib)), true)
	return true
}

// syncOffline queues every not-yet-saved track of "Моя музыка" for the offline library.
func (m *Model) syncOffline(p *page) {
	if !m.cfg.OfflineAll || m.dl == nil || p == nil || !p.myMusic || p.offlineMode {
		return
	}
	_ = os.MkdirAll(config.OfflineDir(), 0o755)
	queued := 0
	for _, t := range p.tracks {
		if m.availableOffline(t) {
			continue
		}
		// already in the temporary cache: just move it over, no download needed
		if c := filepath.Join(config.CacheDir(), t.Key()+".mp3"); exists(c) {
			if os.Rename(c, offlinePath(t)) == nil {
				m.offline[t.Key()] = true
				continue
			}
		}
		if t.Playable() || t.ID != 0 {
			if m.dl.Add(m.job(t, offlinePath(t), true)) {
				queued++
			}
		}
	}
	if queued > 0 {
		m.setStatus(fmt.Sprintf("Сохраняю «Мою музыку» офлайн: %d треков в очереди", queued), false)
	}
}

// offlineCount returns how many tracks of the page are available offline.
func (m *Model) offlineCount(p *page) int {
	n := 0
	for _, t := range p.tracks {
		if m.availableOffline(t) {
			n++
		}
	}
	return n
}

func (m *Model) toggleOfflineAll() {
	m.cfg.OfflineAll = !m.cfg.OfflineAll
	_ = m.cfg.Save()
	if !m.cfg.OfflineAll {
		m.setStatus("Офлайн-синхронизация выключена (сохранённые треки остаются)", false)
		return
	}
	if p := m.cache[secMy]; p != nil && p.done {
		m.syncOffline(p)
	} else {
		m.setStatus("Офлайн включён: треки начнут сохраняться, когда загрузится «Моя музыка»", false)
	}
}
