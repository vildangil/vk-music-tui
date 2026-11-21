package lyrics

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

func getJSON(ctx context.Context, hc *http.Client, u string, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return resp.StatusCode, ErrNotFound
	}
	if resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return resp.StatusCode, json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
}

// ---------- VK ----------

// VKFunc fetches lyrics from VK for a track id ("owner_id_id").
// It returns synced lines and/or plain text.
type VKFunc func(ctx context.Context, key string) (synced []Line, plain []string, err error)

type VK struct{ Fetch VKFunc }

func (VK) Name() string { return "VK" }

func (v VK) Get(ctx context.Context, q Query) (*Lyrics, error) {
	if v.Fetch == nil || q.Key == "" {
		return nil, ErrNotFound
	}
	s, p, err := v.Fetch(ctx, q.Key)
	if err != nil {
		return nil, err
	}
	l := &Lyrics{Synced: s, Plain: p}
	if l.Empty() {
		return nil, ErrNotFound
	}
	return l, nil
}

// ---------- LRCLIB (https://lrclib.net) ----------

type LRCLIB struct{ HTTP *http.Client }

func (LRCLIB) Name() string { return "LRCLIB" }

type lrclibItem struct {
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	Duration     float64 `json:"duration"`
	Instrumental bool    `json:"instrumental"`
	Plain        string  `json:"plainLyrics"`
	Synced       string  `json:"syncedLyrics"`
}

func (it *lrclibItem) toLyrics() *Lyrics {
	if it.Instrumental {
		return &Lyrics{Plain: []string{"Инструментальная композиция — без слов"}}
	}
	return &Lyrics{Synced: ParseLRC(it.Synced), Plain: splitPlain(it.Plain)}
}

func (c LRCLIB) Get(ctx context.Context, q Query) (*Lyrics, error) {
	hc := c.HTTP
	if hc == nil {
		hc = NewHTTP()
	}
	try := []struct{ a, t string }{{q.Artist, q.Title}}
	if a, t := MainArtist(q.Artist), CleanTitle(q.Title); a != q.Artist || t != q.Title {
		try = append(try, struct{ a, t string }{a, t})
	}
	// 1) exact match by artist + title + duration
	for _, x := range try {
		v := url.Values{"artist_name": {x.a}, "track_name": {x.t}}
		if q.Duration > 0 {
			v.Set("duration", fmt.Sprint(q.Duration))
		}
		var it lrclibItem
		if _, err := getJSON(ctx, hc, "https://lrclib.net/api/get?"+v.Encode(), &it); err == nil {
			if l := it.toLyrics(); !l.Empty() {
				return l, nil
			}
		} else if err != ErrNotFound {
			return nil, err
		}
	}
	// 2) fuzzy search, pick the closest duration, prefer synced
	for _, x := range try {
		v := url.Values{"track_name": {x.t}, "artist_name": {x.a}}
		var items []lrclibItem
		if _, err := getJSON(ctx, hc, "https://lrclib.net/api/search?"+v.Encode(), &items); err != nil || len(items) == 0 {
			continue
		}
		best, bestScore := -1, math.Inf(1)
		for i, it := range items {
			score := 0.0
			if q.Duration > 0 && it.Duration > 0 {
				d := math.Abs(it.Duration - float64(q.Duration))
				if d > 8 {
					continue // a different recording
				}
				score += d
			}
			if it.Synced == "" {
				score += 20
			}
			if score < bestScore {
				best, bestScore = i, score
			}
		}
		if best >= 0 {
			if l := items[best].toLyrics(); !l.Empty() {
				return l, nil
			}
		}
	}
	return nil, ErrNotFound
}

// ---------- Genius (plain text) ----------

type Genius struct{ HTTP *http.Client }

func (Genius) Name() string { return "Genius" }

func norm(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if r == ' ' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || (r >= 'а' && r <= 'я') || r == 'ё' {
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func (g Genius) Get(ctx context.Context, q Query) (*Lyrics, error) {
	hc := g.HTTP
	if hc == nil {
		hc = NewHTTP()
	}
	artist, title := MainArtist(q.Artist), CleanTitle(q.Title)
	var res struct {
		Response struct {
			Sections []struct {
				Hits []struct {
					Type   string `json:"type"`
					Result struct {
						URL           string `json:"url"`
						Title         string `json:"title"`
						PrimaryArtist struct {
							Name string `json:"name"`
						} `json:"primary_artist"`
					} `json:"result"`
				} `json:"hits"`
			} `json:"sections"`
		} `json:"response"`
	}
	u := "https://genius.com/api/search/multi?per_page=5&q=" + url.QueryEscape(artist+" "+title)
	if _, err := getJSON(ctx, hc, u, &res); err != nil {
		return nil, err
	}
	na, nt := norm(artist), norm(title)
	page := ""
	for _, s := range res.Response.Sections {
		for _, h := range s.Hits {
			if h.Type != "song" {
				continue
			}
			ra, rt := norm(h.Result.PrimaryArtist.Name), norm(h.Result.Title)
			// both artist and title must roughly match, or we'd show another song's lyrics
			if (strings.Contains(ra, na) || strings.Contains(na, ra)) && (strings.Contains(rt, nt) || strings.Contains(nt, rt)) && ra != "" && rt != "" {
				page = h.Result.URL
				break
			}
		}
		if page != "" {
			break
		}
	}
	if page == "" {
		return nil, ErrNotFound
	}
	lines, err := geniusPage(ctx, hc, page)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, ErrNotFound
	}
	return &Lyrics{Plain: lines}, nil
}

// geniusPage extracts text from <div data-lyrics-container="true"> blocks.
func geniusPage(ctx context.Context, hc *http.Client, page string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, page, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; vktui)")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	doc, err := html.Parse(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	var walk func(n *html.Node, in bool)
	walk = func(n *html.Node, in bool) {
		if n.Type == html.ElementNode {
			for _, a := range n.Attr {
				if a.Key == "data-lyrics-container" && a.Val == "true" {
					if b.Len() > 0 {
						b.WriteString("\n")
					}
					in = true
				}
				if a.Key == "data-exclude-from-selection" && a.Val == "true" {
					return // "N Contributors", translations header, etc.
				}
			}
			if in && n.Data == "br" {
				b.WriteString("\n")
			}
		}
		if in && n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, in)
		}
	}
	walk(doc, false)
	var out []string
	for _, l := range splitPlain(b.String()) {
		out = append(out, strings.TrimSpace(l))
	}
	// collapse runs of empty lines
	var res []string
	for i, l := range out {
		if l == "" && (i == 0 || out[i-1] == "") {
			continue
		}
		res = append(res, l)
	}
	// drop Genius' own header line ("[Текст песни «…»]", "Song Lyrics")
	if len(res) > 0 {
		h := strings.ToLower(res[0])
		if strings.Contains(h, "текст песни") || strings.HasSuffix(h, "lyrics") || strings.HasSuffix(h, "lyrics]") {
			res = res[1:]
		}
	}
	return res, nil
}
