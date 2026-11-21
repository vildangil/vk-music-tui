// Package ui is the Bubble Tea interface.
package ui

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"vktui/internal/config"
	"vktui/internal/lyrics"
	"vktui/internal/player"
	"vktui/internal/vk"
)

const pageSize = 200

type section int

const (
	secMy section = iota
	secMix
	secRec
	secPop
	secSearch
	secPlaylists
	secSettings
)

var sectionNames = []string{"Моя музыка", "VK Микс", "Рекомендации", "Популярное", "Поиск", "Плейлисты", "Настройки"}

type focus int

const (
	focSide focus = iota
	focList
	focSearch
)

type fetchFn func(ctx context.Context, offset int) ([]vk.Track, int, error)

type page struct {
	title     string
	isLists   bool
	tracks    []vk.Track
	playlists []vk.Playlist
	total     int
	cursor    int
	top       int
	loading   bool
	done      bool
	err       string
	fetch     fetchFn

	eager       bool            // load every page in the background (my music, playlists)
	mix         bool            // endless VK Mix stream
	settings    bool            // the settings screen
	queue       bool            // the play queue (tracks follow m.order from the current one)
	myMusic     bool            // the user's own music (saved for offline use)
	offlineMode bool            // filled from the saved library: no connection
	autostart   bool            // start playing as soon as the first batch arrives
	retries     int             // consecutive failed loads
	seen        map[string]bool // de-duplication for the mix stream
}

type loadMoreMsg struct{ p *page }

func (p *page) length() int {
	if p.isLists {
		return len(p.playlists)
	}
	return len(p.tracks)
}

// messages
type tracksMsg struct {
	p      *page
	tracks []vk.Track
	total  int
	err    error
}
type listsMsg struct {
	p     *page
	lists []vk.Playlist
	err   error
}
type resolvedMsg struct {
	key   string
	track vk.Track
	err   error
}
type meMsg struct {
	u   vk.User
	err error
}
type addedMsg struct {
	t   vk.Track
	err error
}
type tickMsg time.Time
type playerMsg struct{ ev any }

type Model struct {
	api *vk.Client
	pl  *player.Player
	cfg *config.Config

	w, h  int
	focus focus
	side  int
	stack []*page
	input textinput.Model

	me vk.User

	// queue
	queue   []vk.Track
	qsrc    *page
	order   []int
	opos    int
	shuffle bool
	repeat  int // 0 off, 1 all, 2 one
	playing *vk.Track
	retried map[string]bool
	errRun  int

	status    string
	statusErr bool
	statusAt  time.Time

	initCmd tea.Cmd

	lastClick   int
	lastClickAt time.Time

	// lyrics panel
	finder      *lyrics.Finder
	lyrOn       bool
	lyr         *lyrics.Lyrics
	lyrKey      string
	lyrLoading  bool
	lyrErr      string
	lyrScroll   int
	lyrManualAt time.Time

	cache map[section]*page // loaded sections, kept while switching

	dl        *player.Downloader
	local     map[string]string // downloaded tracks: key → file
	offline   map[string]bool   // tracks in the offline library
	confirmDL *page             // "download all" waits for a second press
	wantLogin bool              // quit to add an account (see WantLogin)
	menu      *menu             // open popup menu, if any

	waitMix  bool    // queue reached the end of the mix and waits for the next batch
	dragging bool    // dragging the progress bar knob
	dragFrac float64 // 0..1 while dragging
}

func New(api *vk.Client, pl *player.Player, cfg *config.Config) Model {
	ti := textinput.New()
	ti.Placeholder = "исполнитель или трек…"
	ti.Prompt = "  "
	ti.CharLimit = 200
	m := Model{
		api: api, pl: pl, cfg: cfg,
		input:   ti,
		focus:   focList,
		shuffle: cfg.Shuffle,
		repeat:  cfg.Repeat % 3,
		retried: map[string]bool{},
		me:      vk.User{ID: cfg.UserID},
	}
	m.lastClick = -1
	m.finder = newFinder(api, cfg.LyricsSrc)
	m.local = loadDownloadIndex()
	oldOff, oldCache := config.OldMediaDirs()
	config.MigrateDir(oldOff, config.OfflineDir())
	config.MigrateDir(oldCache, config.CacheDir())
	m.offline = scanOffline()
	if pl != nil && pl.Path != "" {
		m.dl = player.NewDownloader(pl.Path)
	}
	m.lyrOn = cfg.Lyrics
	applyAccent(cfg.Accent)
	m.initCmd = m.openSection(secMy)
	return m
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.initCmd, m.fetchMe(), tick(), m.waitPlayer(), m.waitDownload(), tea.SetWindowTitle("VK Music")}
	if m.audioFilters() != "" {
		cmds = append(cmds, m.applyFilters())
	}
	return tea.Batch(cmds...)
}

func (m *Model) mixPage(title string) (*page, tea.Cmd) {
	api := m.api
	p, cmd := m.trackPage(title, func(ctx context.Context, off int) ([]vk.Track, int, error) {
		return api.StreamMix(ctx, off > 0, 20)
	})
	p.mix = true
	p.seen = map[string]bool{}
	return p, cmd
}

func tick() tea.Cmd { return tickEvery(500 * time.Millisecond) }

func tickEvery(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) waitPlayer() tea.Cmd {
	ch := m.pl.Events
	return func() tea.Msg { return playerMsg{<-ch} }
}

func (m Model) fetchMe() tea.Cmd {
	api := m.api
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		u, err := api.Me(ctx)
		return meMsg{u, err}
	}
}

func (m *Model) cur() *page {
	if len(m.stack) == 0 {
		return nil
	}
	return m.stack[len(m.stack)-1]
}

func (m *Model) setStatus(s string, isErr bool) {
	m.status, m.statusErr, m.statusAt = s, isErr, time.Now()
}

func loadTracks(p *page, offset int) tea.Cmd {
	f := p.fetch
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		t, n, err := f(ctx, offset)
		return tracksMsg{p: p, tracks: t, total: n, err: err}
	}
}

func (m *Model) trackPage(title string, f fetchFn) (*page, tea.Cmd) {
	p := &page{title: title, fetch: f, loading: true}
	return p, loadTracks(p, 0)
}

// openSection shows a section. Loaded sections stay in memory, so switching
// back is instant; Ctrl+R / F5 reloads the current one.
func (m *Model) openSection(s section) tea.Cmd {
	if p := m.cache[s]; p != nil && s != secSearch {
		m.side = int(s)
		m.stack = []*page{p}
		m.input.Blur()
		if p.err != "" && p.length() == 0 { // last load failed: try again
			return m.loadSection(s)
		}
		return nil
	}
	return m.loadSection(s)
}

// reloadSection drops the cached copy of the current section and loads it again.
func (m *Model) reloadSection() tea.Cmd {
	s := section(m.side)
	if s == secSearch || s == secSettings {
		return nil
	}
	delete(m.cache, s)
	m.setStatus("Обновляю «"+sectionNames[s]+"»…", false)
	return m.loadSection(s)
}

func (m *Model) loadSection(s section) tea.Cmd {
	cmd := m.buildSection(s)
	if s != secSearch && len(m.stack) > 0 {
		if m.cache == nil {
			m.cache = map[section]*page{}
		}
		m.cache[s] = m.stack[0]
	}
	return cmd
}

func (m *Model) buildSection(s section) tea.Cmd {
	m.side = int(s)
	api := m.api
	var p *page
	var cmd tea.Cmd
	switch s {
	case secMy:
		p, cmd = m.trackPage("Моя музыка", func(ctx context.Context, off int) ([]vk.Track, int, error) {
			return api.Audio(ctx, 0, 0, "", off, pageSize)
		})
		p.eager = true
		p.myMusic = true
	case secMix:
		p, cmd = m.mixPage("VK Микс")
	case secSettings:
		p = &page{title: "Настройки", settings: true, done: true}
	case secRec:
		p, cmd = m.trackPage("Рекомендации", func(ctx context.Context, off int) ([]vk.Track, int, error) {
			return api.Recommendations(ctx, off, 100)
		})
	case secPop:
		p, cmd = m.trackPage("Популярное", func(ctx context.Context, off int) ([]vk.Track, int, error) {
			return api.Popular(ctx, off, 100)
		})
	case secSearch:
		p = &page{title: "Поиск", done: true}
		m.stack = []*page{p}
		m.focus = focSearch
		m.input.SetValue("")
		return m.input.Focus()
	case secPlaylists:
		p = &page{title: "Плейлисты", isLists: true, loading: true, done: true}
		uid := m.me.ID
		cmd = func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()
			if uid == 0 {
				u, err := api.Me(ctx)
				if err != nil {
					return listsMsg{p: p, err: err}
				}
				uid = u.ID
			}
			l, _, err := api.Playlists(ctx, uid, 0, 200)
			return listsMsg{p: p, lists: l, err: err}
		}
	}
	m.stack = []*page{p}
	m.input.Blur()
	return cmd
}

func (m *Model) openPlaylist(pl vk.Playlist) tea.Cmd {
	owner, id, key := pl.Source()
	api := m.api
	p, cmd := m.trackPage(pl.Title, func(ctx context.Context, off int) ([]vk.Track, int, error) {
		return api.Audio(ctx, owner, id, key, off, pageSize)
	})
	p.eager = true
	m.stack = append(m.stack, p)
	return cmd
}

func (m *Model) search(q string) tea.Cmd {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil
	}
	api := m.api
	p, cmd := m.trackPage("Поиск: "+q, func(ctx context.Context, off int) ([]vk.Track, int, error) {
		return api.Search(ctx, q, off, 100)
	})
	m.side = int(secSearch)
	m.stack = []*page{p}
	return cmd
}

// ---------- queue / playback ----------

func (m *Model) startQueue(src *page, idx int) tea.Cmd {
	m.qsrc = src
	m.queue = append([]vk.Track(nil), src.tracks...)
	m.buildOrder(idx)
	m.errRun = 0
	return m.playCurrent()
}

func (m *Model) buildOrder(current int) {
	n := len(m.queue)
	m.order = make([]int, n)
	for i := range m.order {
		m.order[i] = i
	}
	if n == 0 {
		m.opos = 0
		return
	}
	if m.shuffle {
		rand.Shuffle(n, func(i, j int) { m.order[i], m.order[j] = m.order[j], m.order[i] })
		for i, v := range m.order {
			if v == current {
				m.order[0], m.order[i] = m.order[i], m.order[0]
				break
			}
		}
		m.opos = 0
	} else {
		m.opos = current
	}
}

func (m *Model) currentIdx() int {
	if m.opos < 0 || m.opos >= len(m.order) {
		return -1
	}
	return m.order[m.opos]
}

func (m *Model) playCurrent() tea.Cmd {
	i := m.currentIdx()
	if i < 0 {
		return nil
	}
	t := m.queue[i]
	m.playing = &t
	title := tea.SetWindowTitle("► " + clean(mediaTitle(t)) + " · VK Music")
	if f := m.localFile(t); f != "" {
		// downloaded or cached: instant start, works offline
		m.pl.Load(f, t.Title, t.Artist, t.AlbumTitle())
		return tea.Batch(title, m.fetchLyrics())
	}
	if t.Playable() {
		m.pl.Load(t.URL, t.Title, t.Artist, t.AlbumTitle())
		m.cacheTrack(t)
		return tea.Batch(title, m.fetchLyrics())
	}
	return tea.Batch(title, m.resolve(t), m.fetchLyrics())
}

// togglePlay is the play/pause button: pauses, resumes, or starts the selected track.
func (m Model) togglePlay() (tea.Model, tea.Cmd) {
	if m.playing != nil {
		m.pl.TogglePause()
		return m, nil
	}
	return m.activate()
}

// activate plays the selected track or opens the selected playlist.
func (m Model) activate() (tea.Model, tea.Cmd) {
	p := m.cur()
	if p == nil || p.length() == 0 {
		return m, nil
	}
	if p.isLists {
		return m, m.openPlaylist(p.playlists[p.cursor])
	}
	if p.queue {
		// jump inside the running queue instead of building a new one
		m.opos += p.cursor
		p.cursor = 0
		m.errRun = 0
		return m, tea.Batch(m.playCurrent(), m.mixTopUp())
	}
	return m, m.startQueue(p, p.cursor)
}

func (m *Model) cycleRepeat() {
	m.repeat = (m.repeat + 1) % 3
	m.cfg.Repeat = m.repeat
	_ = m.cfg.Save()
}

func (m *Model) resolve(t vk.Track) tea.Cmd {
	api := m.api
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		nt, err := api.Resolve(ctx, t)
		return resolvedMsg{key: t.Key(), track: nt, err: err}
	}
}

// next advances the queue. auto=true means the track ended by itself.
func (m *Model) next(auto bool) tea.Cmd {
	if len(m.order) == 0 {
		return nil
	}
	if auto && m.repeat == 2 {
		return m.playCurrent()
	}
	m.opos++
	if m.opos >= len(m.order) {
		switch {
		case m.qsrc != nil && m.qsrc.mix:
			// the mix is endless: wait for the next batch
			m.opos = len(m.order) - 1
			m.waitMix = true
			return m.loadMore(m.qsrc)
		case m.repeat == 1 || !auto:
			m.opos = 0
		case m.cfg.AutoplayOn():
			// like Spotify autoplay: keep the music going with VK Mix
			m.opos = len(m.order) - 1
			p, cmd := m.mixPage("VK Микс")
			p.autostart = true
			m.setStatus("Очередь закончилась — дальше играет VK Микс", false)
			return cmd
		default:
			m.opos = len(m.order) - 1
			m.playing = nil
			m.pl.Stop()
			return nil
		}
	}
	return tea.Batch(m.playCurrent(), m.mixTopUp())
}

// mixTopUp fetches more mix tracks when the queue is about to run out.
func (m *Model) mixTopUp() tea.Cmd {
	if m.qsrc == nil || !m.qsrc.mix || m.opos < len(m.order)-3 {
		return nil
	}
	return m.loadMore(m.qsrc)
}

func (m *Model) loadMore(p *page) tea.Cmd {
	if p == nil || p.loading || p.fetch == nil || (p.done && !p.mix) {
		return nil
	}
	p.loading = true
	return loadTracks(p, len(p.tracks))
}

func (m *Model) prev() tea.Cmd {
	if len(m.order) == 0 {
		return nil
	}
	if m.pl.State().Pos > 3 {
		m.pl.SeekAbs(0)
		return nil
	}
	m.opos--
	if m.opos < 0 {
		m.opos = len(m.order) - 1
	}
	return m.playCurrent()
}

func (m *Model) toggleShuffle() {
	m.shuffle = !m.shuffle
	if cur := m.currentIdx(); cur >= 0 {
		m.buildOrder(cur)
	}
	m.cfg.Shuffle = m.shuffle
	_ = m.cfg.Save()
}

// ---------- update ----------

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.input.Width = max(10, m.w-30)
		return m, nil

	case tickMsg:
		if m.status != "" && time.Since(m.statusAt) > 5*time.Second {
			m.status = ""
		}
		if m.lyrOn && m.lyr.IsSynced() && m.playing != nil {
			return m, tickEvery(200 * time.Millisecond) // smooth "live" lyrics
		}
		return m, tick()

	case meMsg:
		if msg.err != nil {
			m.setStatus("Не удалось получить профиль: "+msg.err.Error(), true)
		} else {
			m.me = msg.u
			if name := strings.TrimSpace(msg.u.FirstName + " " + msg.u.LastName); name != "" {
				m.cfg.SetActiveName(name)
				_ = m.cfg.Save()
			}
			if m.cfg.UserID != msg.u.ID {
				m.cfg.UserID = msg.u.ID
				_ = m.cfg.Save()
			}
		}
		return m, nil

	case lyricsMsg:
		if msg.key == m.lyrKey {
			m.lyrLoading = false
			m.lyr = msg.l
			m.lyrErr = ""
			if msg.err != nil {
				m.lyrErr = lyricsErrText(msg.err)
			}
		}
		return m, nil

	case loadMoreMsg:
		return m, m.loadMore(msg.p)

	case filterErrMsg:
		m.setStatus("Эквалайзер не применился: "+msg.err.Error(), true)
		return m, nil

	case tracksMsg:
		p := msg.p
		p.loading = false
		if msg.err != nil {
			// VK rate limits (6, 9) and network hiccups: retry with backoff instead of giving up
			// "Моя музыка" without a connection: open the saved offline library at once
			if p.myMusic && p.length() == 0 && m.goOffline(p, msg.err) {
				return m, nil
			}
			if p.retries < 6 && retryable(msg.err) {
				p.retries++
				d := time.Duration(p.retries) * 1500 * time.Millisecond
				return m, tea.Tick(d, func(time.Time) tea.Msg { return loadMoreMsg{p} })
			}
			p.err = humanErr(msg.err)
			p.done = true
			if p.length() > 0 {
				m.setStatus(fmt.Sprintf("Загружено %d из %d: %s", p.length(), p.total, p.err), true)
			}
			return m, nil
		}
		p.err = ""
		p.retries = 0
		if p.mix {
			fresh := msg.tracks[:0:0]
			for _, t := range msg.tracks {
				if !p.seen[t.Key()] {
					p.seen[t.Key()] = true
					fresh = append(fresh, t)
				}
			}
			msg.tracks = fresh
		}
		p.tracks = append(p.tracks, msg.tracks...)
		if p.autostart && len(p.tracks) > 0 {
			p.autostart = false
			return m, m.startQueue(p, 0)
		}
		if p == m.qsrc && len(m.order) == len(m.queue) {
			// extend the running queue with newly loaded tracks
			base := len(m.queue)
			m.queue = append(m.queue, msg.tracks...)
			for i := range msg.tracks {
				m.order = append(m.order, base+i)
			}
			if m.shuffle {
				tail := m.order[m.opos+1:]
				rand.Shuffle(len(tail), func(i, j int) { tail[i], tail[j] = tail[j], tail[i] })
			}
		}
		if p.mix {
			p.total = len(p.tracks)
			// the queue was waiting at its end for this batch
			if p == m.qsrc && m.waitMix && m.opos+1 < len(m.order) {
				m.waitMix = false
				m.opos++
				return m, m.playCurrent()
			}
			return m, nil
		}
		p.total = msg.total
		if len(msg.tracks) == 0 || (p.total > 0 && len(p.tracks) >= p.total) {
			p.done = true
		}
		if p.myMusic && p.done {
			saveLibrary(m.cfg.UserID, p.tracks)
			m.syncOffline(p)
		}
		if p.eager && !p.done {
			// fetch the rest in the background, gently (VK limits requests per second)
			return m, tea.Tick(350*time.Millisecond, func(time.Time) tea.Msg { return loadMoreMsg{p} })
		}
		return m, m.maybeLoadMore()

	case listsMsg:
		p := msg.p
		p.loading = false
		if msg.err != nil {
			p.err = humanErr(msg.err)
			return m, nil
		}
		p.playlists = msg.lists
		p.total = len(msg.lists)
		return m, nil

	case resolvedMsg:
		if m.playing == nil || m.playing.Key() != msg.key {
			return m, nil
		}
		if msg.err != nil || !msg.track.Playable() {
			m.setStatus("Трек недоступен: "+m.playing.Artist+" — "+m.playing.Title, true)
			return m, m.skipOnError()
		}
		if i := m.currentIdx(); i >= 0 {
			m.queue[i] = msg.track
		}
		t := msg.track
		m.playing = &t
		m.pl.Load(t.URL, t.Title, t.Artist, t.AlbumTitle())
		m.cacheTrack(t)
		return m, nil

	case addedMsg:
		if msg.err != nil {
			m.setStatus("Не удалось добавить: "+humanErr(msg.err), true)
		} else {
			m.setStatus("Добавлено в «Моя музыка»: "+msg.t.Artist+" — "+msg.t.Title, false)
		}
		return m, nil

	case playerMsg:
		cmd := m.handlePlayer(msg.ev)
		return m, tea.Batch(cmd, m.waitPlayer())

	case dlMsg:
		m.handleDownload(msg.ev)
		return m, m.waitDownload()

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		return m.handleMouse(msg)
	}
	return m, nil
}

func (m *Model) skipOnError() tea.Cmd {
	m.errRun++
	if m.errRun >= 5 {
		m.setStatus("Несколько треков подряд не играют — остановлено. Проверь сеть/прокси.", true)
		m.playing = nil
		m.pl.Stop()
		return nil
	}
	return m.next(true)
}

func (m *Model) handlePlayer(ev any) tea.Cmd {
	switch e := ev.(type) {
	case player.EndEvent:
		if m.playing == nil {
			return nil
		}
		switch e.Reason {
		case "eof":
			m.errRun = 0
			return m.next(true)
		case "error":
			// Stream URLs expire after a while — refresh once before skipping.
			k := m.playing.Key()
			if !m.retried[k] {
				m.retried[k] = true
				return m.resolve(*m.playing)
			}
			msg := "Ошибка воспроизведения"
			if e.Err != "" {
				msg += " (" + e.Err + ")"
			}
			msg += " — если включён VPN, добавь okcdn.ru и vkuseraudio.net в исключения"
			m.setStatus(msg, true)
			return m.skipOnError()
		}
	case player.MediaKeyEvent:
		// Buttons in the Windows/macOS media overlay and hardware media keys.
		switch e.Action {
		case "next":
			if m.playing == nil {
				m.pl.Stop()
				return nil
			}
			return m.next(false)
		case "prev":
			if m.playing == nil {
				m.pl.Stop()
				return nil
			}
			return m.prev()
		case "playpause":
			if m.playing != nil {
				m.pl.TogglePause()
			}
		}
	case player.DiedEvent:
		m.setStatus("mpv неожиданно завершился. Перезапусти плеер.", true)
		m.playing = nil
	}
	return nil
}

func retryable(err error) bool {
	if e, ok := err.(*vk.APIError); ok {
		return e.Code == 6 || e.Code == 9 || e.Code == 10 || e.Code == 1
	}
	return true // network errors
}

func humanErr(err error) string {
	if e, ok := err.(*vk.APIError); ok {
		switch e.Code {
		case 5:
			return "Токен недействителен — выполни `vktui login`"
		case 6:
			return "Слишком много запросов, подожди секунду"
		case 15, 201:
			return "Доступ к аудио запрещён (" + e.Msg + ")"
		case 3:
			return "Этому токену VK не даёт музыку — выполни `vktui login` (вход по коду)"
		case 9:
			return "VK ограничил частоту запросов (flood control), подожди пару минут"
		case 25:
			return "Токен не от того приложения — выполни `vktui login`"
		}
	}
	return err.Error()
}

func (m *Model) listHeight() int {
	// full height − player box(4) − help(1) − list box borders(2) − title(1) − header(1)
	h := m.h - 4 - 1 - 2 - 2
	if m.cur() != nil && m.side == int(secSearch) {
		h -= 2
	}
	return max(1, h)
}

func (m *Model) maybeLoadMore() tea.Cmd {
	p := m.cur()
	if p == nil || p.isLists || p.loading || p.done || p.fetch == nil {
		return nil
	}
	if p.cursor >= len(p.tracks)-m.listHeight()*2 {
		p.loading = true
		return loadTracks(p, len(p.tracks))
	}
	return nil
}

func (m *Model) moveCursor(d int) tea.Cmd {
	p := m.cur()
	if p == nil || p.length() == 0 {
		return nil
	}
	p.cursor += d
	if p.cursor < 0 {
		p.cursor = 0
	}
	if p.cursor >= p.length() {
		p.cursor = p.length() - 1
	}
	return m.maybeLoadMore()
}

func (m Model) handleKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := k.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	if m.menu != nil {
		return m, m.menuKey(latinKey(key))
	}

	if m.focus == focSearch {
		switch key {
		case "enter":
			q := m.input.Value()
			m.input.Blur()
			m.focus = focList
			return m, m.search(q)
		case "esc":
			m.input.Blur()
			m.focus = focList
			return m, nil
		case "tab":
			m.input.Blur()
			m.focus = focSide
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(k)
		return m, cmd
	}

	key = latinKey(key) // hotkeys work on Russian/Ukrainian layouts too

	// settings screen: arrows change values instead of seeking
	if p := m.cur(); p != nil && p.settings && m.focus == focList {
		switch key {
		case "up", "k":
			p.cursor = (p.cursor + settingsRows - 1) % settingsRows
			return m, nil
		case "down", "j":
			p.cursor = (p.cursor + 1) % settingsRows
			return m, nil
		case "left", "h":
			return m, m.changeSetting(p.cursor, -1)
		case "right", "l":
			return m, m.changeSetting(p.cursor, 1)
		case "enter", " ":
			if m.settingsMenu(p.cursor) {
				return m, nil
			}
			return m, m.changeSetting(p.cursor, 1)
		}
	}

	// global playback keys
	switch key {
	case "q":
		return m, tea.Quit
	case " ", "space":
		return m.togglePlay()
	case "n", ">":
		return m, m.next(false)
	case "p", "<":
		return m, m.prev()
	case "right", "l":
		m.pl.Seek(5)
		return m, nil
	case "left", "h":
		m.pl.Seek(-5)
		return m, nil
	case "shift+right", "L":
		m.pl.Seek(30)
		return m, nil
	case "shift+left", "H":
		m.pl.Seek(-30)
		return m, nil
	case "+", "=":
		return m, m.changeVolume(m.cfg.Step())
	case "-", "_":
		return m, m.changeVolume(-m.cfg.Step())
	case "s":
		m.toggleShuffle()
		return m, nil
	case "r":
		m.cycleRepeat()
		return m, nil
	case "/", ".": // "." is where "/" is on the Russian layout
		m.side = int(secSearch)
		m.focus = focSearch
		m.input.SetValue("")
		return m, m.input.Focus()
	case "tab", "shift+tab":
		if m.focus == focSide {
			m.focus = focList
		} else {
			m.focus = focSide
		}
		return m, nil
	case "1", "2", "3", "4", "5", "6", "7":
		s := section(key[0] - '1')
		m.focus = focList
		return m, m.openSection(s)
	case "o":
		return m, m.openQueue()
	case "ctrl+r", "f5":
		return m, m.reloadSection()
	case "S": // Shift+S: shuffle-play the whole list
		return m, m.playAll(m.cur(), true)
	case "P": // Shift+P: play the whole list in order
		return m, m.playAll(m.cur(), false)
	case "[":
		m.scrollLyrics(-3)
		return m, nil
	case "]":
		m.scrollLyrics(3)
		return m, nil
	case "t":
		return m, m.toggleLyrics()
	case "c":
		return m, m.jumpToPlaying()
	}

	if m.focus == focSide {
		switch key {
		case "up", "k":
			m.side = (m.side + len(sectionNames) - 1) % len(sectionNames)
		case "down", "j":
			m.side = (m.side + 1) % len(sectionNames)
		case "enter":
			m.focus = focList
			return m, m.openSection(section(m.side))
		}
		return m, nil
	}

	p := m.cur()
	switch key {
	case "up", "k":
		return m, m.moveCursor(-1)
	case "down", "j":
		return m, m.moveCursor(1)
	case "pgup", "ctrl+u":
		return m, m.moveCursor(-m.listHeight())
	case "pgdown", "ctrl+d":
		return m, m.moveCursor(m.listHeight())
	case "home", "g":
		return m, m.moveCursor(-1 << 30)
	case "end", "G":
		return m, m.moveCursor(1 << 30)
	case "esc", "backspace":
		if len(m.stack) > 1 {
			m.stack = m.stack[:len(m.stack)-1]
		}
		return m, nil
	case "enter":
		return m.activate()
	case "d":
		if p != nil && !p.isLists && !p.settings && p.length() > 0 {
			m.downloadTrack(p.tracks[p.cursor])
		}
		return m, nil
	case "D":
		m.downloadAll(p)
		return m, nil
	case "a":
		if p != nil && !p.isLists && p.length() > 0 {
			t := p.tracks[p.cursor]
			api := m.api
			return m, func() tea.Msg {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				return addedMsg{t: t, err: api.Add(ctx, t)}
			}
		}
	}
	return m, nil
}

func (m *Model) changeVolume(d int) tea.Cmd {
	v := m.cfg.Volume + d
	if v < 0 {
		v = 0
	}
	if v > 150 {
		v = 150
	}
	m.cfg.Volume = v
	m.pl.SetVolume(float64(v))
	_ = m.cfg.Save()
	return nil
}

func (m *Model) jumpToPlaying() tea.Cmd {
	m.lyrManualAt = time.Time{} // lyrics follow the song again
	p := m.cur()
	if p == nil || m.playing == nil || p.isLists {
		return nil
	}
	k := m.playing.Key()
	for i, t := range p.tracks {
		if t.Key() == k {
			p.cursor = i
			m.focus = focList
			return nil
		}
	}
	return nil
}

// ---------- view ----------

func (m Model) viewSide(w, h int) string {
	lines := strings.Split(strings.TrimRight(m.viewSideRaw(w, h), "\n"), "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
}

func (m Model) viewSideRaw(w, h int) string {
	var b strings.Builder
	b.WriteString(sLogo.Render(fit(" VK Music", w)) + "\n")
	name := strings.TrimSpace(m.me.FirstName + " " + m.me.LastName)
	b.WriteString(sFaint.Render(fit(" "+name, w)) + "\n\n")
	for i, n := range sectionNames {
		label := fmt.Sprintf(" %d %s%s", i+1, m.icons().sections[i], n)
		switch {
		case i == m.side && m.focus == focSide:
			b.WriteString(sSel.Render(fit(label, w)))
		case i == m.side:
			b.WriteString(sGreenB.Render(fit(label, w)))
		default:
			b.WriteString(sDim.Render(fit(label, w)))
		}
		b.WriteString("\n")
	}
	if m.playing != nil && len(m.queue) > 0 {
		st := sDim
		if c := m.cur(); c != nil && c.queue {
			st = sGreenB
		}
		b.WriteString("\n" + st.Render(fit(fmt.Sprintf(" %sОчередь %d/%d", m.icons().queue, m.opos+1, len(m.queue)), w)))
	}
	return b.String()
}

func (m Model) viewMain(w, h int) string {
	return strings.TrimRight(m.viewMainRaw(w, h), "\n")
}

func (m Model) viewMainRaw(w, h int) string {
	p := m.cur()
	if p == nil {
		return ""
	}
	if p.settings {
		return m.viewSettings(w, h)
	}
	if p.queue {
		m.syncQueuePage(p)
	}
	var b strings.Builder
	cnt := m.headerCount(p)
	b.WriteString(m.viewHeader(p, w, cnt) + "\n")
	used := 1
	if m.side == int(secSearch) {
		b.WriteString(m.input.View() + "\n" + sFaint.Render(strings.Repeat("─", w)) + "\n")
		used += 2
	}
	listH := h - used - 1

	if p.err != "" && p.length() == 0 {
		b.WriteString("\n " + sErr.Render(p.err))
		return b.String()
	}
	if p.length() == 0 {
		if p.loading {
			b.WriteString("\n " + sDim.Render("Загрузка…"))
		} else if m.side == int(secSearch) && p.title == "Поиск" {
			b.WriteString("\n " + sDim.Render("Введи запрос и нажми Enter"))
		} else {
			b.WriteString("\n " + sDim.Render("Пусто"))
		}
		return b.String()
	}

	// scroll window
	if p.cursor < p.top {
		p.top = p.cursor
	}
	if p.cursor >= p.top+listH {
		p.top = p.cursor - listH + 1
	}
	if p.top < 0 {
		p.top = 0
	}

	if p.isLists {
		b.WriteString(sFaint.Render(fit("   #  Название", w-8)+fit("Треков", 8)) + "\n")
		for i := p.top; i < min(p.length(), p.top+listH); i++ {
			pl := p.playlists[i]
			num := fmt.Sprintf("%4d  ", i+1)
			line := fit(num+pl.Title, w-8) + fit(fmt.Sprintf("%6d", pl.Count), 8)
			if i == p.cursor && m.focus == focList {
				b.WriteString(sSel.Render(line))
			} else {
				b.WriteString(line)
			}
			b.WriteString("\n")
		}
		return b.String()
	}

	numW, timeW := 6, 7
	rest := w - numW - timeW
	titleW := rest * 55 / 100
	artW := rest - titleW
	b.WriteString(sFaint.Render(fit("    #", numW)+fit("Название", titleW)+fit("Исполнитель", artW)+fit("  Время", timeW)) + "\n")
	playingKey := ""
	if m.playing != nil {
		playingKey = m.playing.Key()
	}
	for i := p.top; i < min(p.length(), p.top+listH); i++ {
		t := p.tracks[i]
		isPlaying := t.Key() == playingKey
		num := fmt.Sprintf("%5d ", i+1)
		if isPlaying {
			num = fit("    "+m.icons().playing, numW)
		}
		tt := fit(t.Title, titleW-1) + " "
		ar := fit(t.Artist, artW-1) + " "
		mark := " "
		if m.availableOffline(t) {
			mark = "↓" // downloaded: plays offline
		}
		du := fit(fmt.Sprintf("%s%6s", mark, fmtDur(float64(t.Duration))), timeW)
		sel := i == p.cursor && m.focus == focList
		switch {
		case sel:
			b.WriteString(sSel.Render(num+tt) + sSelDim.Render(ar+du))
		case isPlaying:
			b.WriteString(sGreenB.Render(num+tt) + sGreen.Render(ar+du))
		case t.URL == "":
			b.WriteString(sFaint.Render(num + tt + ar + du))
		default:
			b.WriteString(sFaint.Render(num) + sTitle.UnsetBold().Render(tt) + sDim.Render(ar+du))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (m Model) viewHelp() string {
	if m.status != "" {
		st := sGreen
		if m.statusErr {
			st = sErr
		}
		return " " + st.Render(fit(m.status, m.w-2))
	}
	keys := [][2]string{
		{"enter", "играть"}, {"space", "пауза"}, {"n/p", "трек"}, {"←/→", "±5с"},
		{"+/-", "громк."}, {"s", "shuffle"}, {"r", "repeat"}, {"/", "поиск"},
		{"t", "текст"}, {"o", "очередь"}, {"S", "перемешать всё"}, {"d", "скачать"}, {"a", "в мою музыку"}, {"tab", "панель"}, {"q", "выход"},
	}
	var parts []string
	width := 1
	for _, k := range keys {
		s := sKey.Render(k[0]) + " " + sFaint.Render(k[1])
		sw := lipgloss.Width(s) + 3
		if width+sw > m.w {
			break
		}
		width += sw
		parts = append(parts, s)
	}
	return " " + strings.Join(parts, sFaint.Render(" · "))
}

// headerCount is the counter on the right of a list header.
func (m *Model) headerCount(p *page) string {
	cnt := ""
	switch {
	case p.offlineMode:
		cnt = fmt.Sprintf("  офлайн · %d", p.length())
	case p.queue && m.shuffle:
		cnt = fmt.Sprintf("  перемешано · %d", p.length())
	case p.queue:
		cnt = fmt.Sprintf("  %d", p.length())
	case p.mix:
		cnt = "  бесконечный поток"
	case p.total > 0 && !p.done && p.length() < p.total:
		cnt = fmt.Sprintf("  загружено %d из %d", p.length(), p.total)
	case p.total > 0:
		cnt = fmt.Sprintf("  %d", p.total)
	}
	return cnt
}
