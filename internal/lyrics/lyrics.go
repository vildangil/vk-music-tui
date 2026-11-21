// Package lyrics finds song lyrics, preferring time-synced ("live") ones.
//
// Sources, in order: VK (synced, when VK has them), LRCLIB (synced, open
// database), Genius (plain text). Results are cached on disk.
package lyrics

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Line is one synced lyric line; At is the start time in seconds.
type Line struct {
	At   float64 `json:"at"`
	Text string  `json:"text"`
}

type Lyrics struct {
	Source string   `json:"source"` // "VK", "LRCLIB", "Genius"
	Synced []Line   `json:"synced,omitempty"`
	Plain  []string `json:"plain,omitempty"`
}

func (l *Lyrics) IsSynced() bool { return l != nil && len(l.Synced) > 0 }
func (l *Lyrics) Empty() bool    { return l == nil || (len(l.Synced) == 0 && len(l.Plain) == 0) }

// Query identifies a track.
type Query struct {
	Key      string // stable id for caching (owner_id_id)
	Artist   string
	Title    string
	Duration int // seconds
}

// ErrNotFound means no source had lyrics for the track.
var ErrNotFound = errors.New("текст не найден")

// Provider is one lyrics source.
type Provider interface {
	Name() string
	Get(ctx context.Context, q Query) (*Lyrics, error)
}

// Finder tries providers in order and caches results.
type Finder struct {
	Providers []Provider
	CacheDir  string // "" disables the disk cache
}

func NewHTTP() *http.Client { return &http.Client{Timeout: 12 * time.Second} }

// UserAgent identifies the app to public lyrics APIs (LRCLIB asks for one).
const UserAgent = "vktui (terminal music player)"

// Find returns synced lyrics if any source has them, otherwise the first plain text.
func (f *Finder) Find(ctx context.Context, q Query) (*Lyrics, error) {
	if l, ok := f.fromCache(q); ok {
		if l.Empty() {
			return nil, ErrNotFound
		}
		return l, nil
	}
	var plain *Lyrics
	var lastErr error
	for _, p := range f.Providers {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		l, err := p.Get(ctx, q)
		if err != nil {
			if !errors.Is(err, ErrNotFound) {
				lastErr = fmt.Errorf("%s: %w", p.Name(), err)
			}
			continue
		}
		if l.Empty() {
			continue
		}
		l.Source = p.Name()
		if l.IsSynced() {
			f.toCache(q, l)
			return l, nil
		}
		if plain == nil {
			plain = l
		}
	}
	if plain != nil {
		f.toCache(q, plain)
		return plain, nil
	}
	if lastErr == nil {
		f.toCache(q, &Lyrics{}) // remember "not found" for a while
		return nil, ErrNotFound
	}
	return nil, lastErr
}

// ---------- cache ----------

type cacheEntry struct {
	Saved  time.Time `json:"saved"`
	Lyrics *Lyrics   `json:"lyrics"`
}

func (f *Finder) cachePath(q Query) string {
	k := q.Key
	if k == "" {
		h := sha1.Sum([]byte(strings.ToLower(q.Artist + "\x00" + q.Title)))
		k = hex.EncodeToString(h[:8])
	}
	return filepath.Join(f.CacheDir, k+".json")
}

func (f *Finder) fromCache(q Query) (*Lyrics, bool) {
	if f.CacheDir == "" {
		return nil, false
	}
	b, err := os.ReadFile(f.cachePath(q))
	if err != nil {
		return nil, false
	}
	var e cacheEntry
	if json.Unmarshal(b, &e) != nil || e.Lyrics == nil {
		return nil, false
	}
	// "not found" and plain-only results are retried after a few days: sources grow.
	if (e.Lyrics.Empty() || !e.Lyrics.IsSynced()) && time.Since(e.Saved) > 72*time.Hour {
		return nil, false
	}
	return e.Lyrics, true
}

func (f *Finder) toCache(q Query, l *Lyrics) {
	if f.CacheDir == "" {
		return
	}
	_ = os.MkdirAll(f.CacheDir, 0o700)
	b, _ := json.Marshal(cacheEntry{Saved: time.Now(), Lyrics: l})
	_ = os.WriteFile(f.cachePath(q), b, 0o600)
}

// ---------- helpers ----------

var lrcTag = regexp.MustCompile(`\[(\d{1,3}):(\d{1,2})(?:[.:](\d{1,3}))?\]`)

// ParseLRC parses "[mm:ss.xx] text" lines (several stamps per line allowed).
func ParseLRC(s string) []Line {
	var out []Line
	for _, raw := range strings.Split(strings.ReplaceAll(s, "\r", ""), "\n") {
		stamps := lrcTag.FindAllStringSubmatchIndex(raw, -1)
		if len(stamps) == 0 {
			continue
		}
		text := strings.TrimSpace(raw[stamps[len(stamps)-1][1]:])
		for _, st := range stamps {
			mm, _ := strconv.Atoi(raw[st[2]:st[3]])
			ss, _ := strconv.Atoi(raw[st[4]:st[5]])
			frac := 0.0
			if st[6] >= 0 {
				fs := raw[st[6]:st[7]]
				fv, _ := strconv.Atoi(fs)
				frac = float64(fv) / pow10(len(fs))
			}
			out = append(out, Line{At: float64(mm*60+ss) + frac, Text: text})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At < out[j].At })
	return out
}

func pow10(n int) float64 {
	p := 1.0
	for i := 0; i < n; i++ {
		p *= 10
	}
	return p
}

func splitPlain(s string) []string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r", ""))
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

var (
	reBrackets = regexp.MustCompile(`(?i)\s*[\(\[][^\)\]]*(feat|ft\.|prod|remaster|remix|version|edit|live|mix|bonus|explicit|official)[^\)\]]*[\)\]]`)
	reDashTail = regexp.MustCompile(`(?i)\s+-\s+(remaster(ed)?|live|radio edit|single version|bonus track|explicit).*$`)
	reFeat     = regexp.MustCompile(`(?i)\s+(feat\.?|ft\.?|featuring|x|&|при уч\.?)\s+.*$`)
)

// CleanTitle strips "(feat. …)", "(Remastered 2011)", "- Live" etc.
func CleanTitle(s string) string {
	s = reBrackets.ReplaceAllString(s, "")
	s = reDashTail.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

// MainArtist keeps the first artist of "A, B feat. C".
func MainArtist(s string) string {
	if i := strings.IndexAny(s, ",;/"); i > 0 {
		s = s[:i]
	}
	s = reFeat.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}
