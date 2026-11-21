package player

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// DownloadJob saves one track to an mp3 file using mpv's encoder (no ffmpeg needed).
type DownloadJob struct {
	Key           string // track id, for de-duplication and events
	URL           string
	Title, Artist string
	Album         string
	Dest          string // final .mp3 path
	Cache         bool   // background cache job (quiet, lower priority)
	// Resolve returns a fresh stream URL; used once if the first try fails
	// (VK links expire).
	Resolve func(ctx context.Context) (string, error)
}

// DownloadEvent reports progress: Started, then Done with Err == nil on success.
type DownloadEvent struct {
	Job     DownloadJob
	Started bool
	Err     error
	Queued  int // jobs still waiting
}

// Downloader runs jobs one at a time, user downloads before cache jobs.
type Downloader struct {
	MPV    string
	Events chan DownloadEvent

	mu      sync.Mutex
	user    []DownloadJob
	cache   []DownloadJob
	known   map[string]bool // keys queued or running (per kind)
	wake    chan struct{}
	stopped bool
}

func NewDownloader(mpvPath string) *Downloader {
	d := &Downloader{MPV: mpvPath, Events: make(chan DownloadEvent, 64), known: map[string]bool{}, wake: make(chan struct{}, 1)}
	go d.loop()
	return d
}

func kindKey(j DownloadJob) string {
	if j.Cache {
		return "c:" + j.Key
	}
	return "d:" + j.Key
}

// Add queues a job; returns false if the same job is already queued/running.
func (d *Downloader) Add(j DownloadJob) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped || d.known[kindKey(j)] {
		return false
	}
	d.known[kindKey(j)] = true
	if j.Cache {
		d.cache = append(d.cache, j)
	} else {
		d.user = append(d.user, j)
	}
	select {
	case d.wake <- struct{}{}:
	default:
	}
	return true
}

// Pending returns how many user downloads are queued or running.
func (d *Downloader) Pending() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for k := range d.known {
		if strings.HasPrefix(k, "d:") {
			n++
		}
	}
	return n
}

// Stop drops queued jobs (the running one finishes) and refuses new ones.
func (d *Downloader) Stop() {
	d.mu.Lock()
	d.stopped = true
	d.user, d.cache = nil, nil
	d.mu.Unlock()
}

func (d *Downloader) next() (DownloadJob, int, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.user) > 0 {
		j := d.user[0]
		d.user = d.user[1:]
		return j, len(d.user), true
	}
	if len(d.cache) > 0 {
		j := d.cache[0]
		d.cache = d.cache[1:]
		return j, 0, true
	}
	return DownloadJob{}, 0, false
}

func (d *Downloader) emit(e DownloadEvent) {
	select {
	case d.Events <- e:
	default:
	}
}

func (d *Downloader) loop() {
	for {
		j, queued, ok := d.next()
		if !ok {
			<-d.wake
			continue
		}
		d.emit(DownloadEvent{Job: j, Started: true, Queued: queued})
		err := d.run(j)
		d.mu.Lock()
		delete(d.known, kindKey(j))
		d.mu.Unlock()
		d.emit(DownloadEvent{Job: j, Err: err, Queued: queued})
	}
}

func (d *Downloader) run(j DownloadJob) error {
	if _, err := os.Stat(j.Dest); err == nil {
		return nil // already there
	}
	if err := os.MkdirAll(filepath.Dir(j.Dest), 0o755); err != nil {
		return err
	}
	err := d.encode(j.URL, j)
	if err != nil && j.Resolve != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		u, rerr := j.Resolve(ctx)
		cancel()
		if rerr == nil && u != "" {
			err = d.encode(u, j)
		}
	}
	return err
}

func (d *Downloader) encode(url string, j DownloadJob) error {
	if url == "" {
		return errors.New("нет ссылки на трек")
	}
	part := j.Dest + ".part"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, d.MPV,
		"--no-config", "--no-terminal", "--ytdl=no", "--vid=no", "--sid=no",
		"--network-timeout=30",
		url,
		"--o="+part, "--of=mp3", "--oac=libmp3lame", "--oacopts=b=320k",
		"--ocopy-metadata=no",
		"--oset-metadata-append=title="+j.Title,
		"--oset-metadata-append=artist="+j.Artist,
	)
	if j.Album != "" {
		cmd.Args = append(cmd.Args, "--oset-metadata-append=album="+j.Album)
	}
	prepareCmd(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	afterStart(cmd)
	err := cmd.Wait()
	st, serr := os.Stat(part)
	if err != nil || serr != nil || st.Size() < 16*1024 {
		_ = os.Remove(part)
		if err == nil {
			err = errors.New("пустой файл")
		}
		return fmt.Errorf("mpv не смог сохранить трек: %w", err)
	}
	return os.Rename(part, j.Dest)
}

// ---------- cache housekeeping ----------

// TrimDir deletes the least recently used .mp3 files until the folder fits in limit bytes.
func TrimDir(dir string, limit int64) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type f struct {
		path string
		size int64
		t    time.Time
	}
	var files []f
	var total int64
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".mp3") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, f{filepath.Join(dir, e.Name()), info.Size(), info.ModTime()})
		total += info.Size()
	}
	sort.Slice(files, func(i, k int) bool { return files[i].t.Before(files[k].t) })
	for _, x := range files {
		if total <= limit {
			break
		}
		if os.Remove(x.path) == nil {
			total -= x.size
		}
	}
}

// DirSize returns the total size of .mp3 files in dir.
func DirSize(dir string) int64 {
	var total int64
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if info, err := e.Info(); err == nil && !e.IsDir() && strings.HasSuffix(e.Name(), ".mp3") {
			total += info.Size()
		}
	}
	return total
}

// Touch marks a cached file as recently used.
func Touch(path string) {
	now := time.Now()
	_ = os.Chtimes(path, now, now)
}
