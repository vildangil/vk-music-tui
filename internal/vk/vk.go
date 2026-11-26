// Package vk is a minimal VK API client for audio methods.
package vk

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// App describes which official VK client we impersonate. VK serves audio only to
// tokens of a few official apps.
type App struct {
	Name     string
	ID       string
	Secret   string
	UA       string
	APIBase  string
	TokenURL string
}

var (
	// Android is the official VK app (what Music-M / MusicX use). Recommended.
	Android = App{
		Name:     "android",
		ID:       "2274003",
		Secret:   "hHbZxrka2uZ6jB1inYsH",
		UA:       "VKAndroidApp/8.154-99999 (Android 12; SDK 32; arm64-v8a; Pixel 6; ru; 2960x1440)",
		APIBase:  "https://api.vk.ru/method/",
		TokenURL: "https://api.vk.ru/oauth/token",
	}
	// Kate is Kate Mobile. Its browser-flow tokens no longer get audio.
	Kate = App{
		Name:     "kate",
		ID:       "2685278",
		Secret:   "lxhD8OD7dMsqtXIm5IUY",
		UA:       "KateMobileAndroid/56 lite-460 (Android 4.4.2; SDK 19; x86; unknown Android SDK built for x86; en)",
		APIBase:  "https://api.vk.com/method/",
		TokenURL: "https://oauth.vk.com/token",
	}
)

func AppByName(n string) App {
	if n == Kate.Name {
		return Kate
	}
	return Android
}

const APIVersion = "5.131"

type Client struct {
	Token    string
	App      App
	DeviceID string
	HTTP     *http.Client
	UA       string     // overrides App.UA when set (diagnostics)
	Version  string     // overrides APIVersion when set
	Extra    url.Values // added to every call (captcha answer)
}

func New(app App, token, deviceID string) *Client {
	return &Client{Token: token, App: app, DeviceID: deviceID, HTTP: &http.Client{Timeout: 20 * time.Second}}
}

// NewDeviceID makes an id in the "androidId:token" shape the VK app uses.
func NewDeviceID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	t := make([]byte, 105)
	_, _ = rand.Read(t)
	return hex.EncodeToString(b) + ":" + base64.RawURLEncoding.EncodeToString(t)
}

type APIError struct {
	Code int    `json:"error_code"`
	Msg  string `json:"error_msg"`

	// error 14 (captcha needed): a picture to type, and/or VK's "I'm not a
	// robot" page. The person solves it; it is never solved automatically.
	CaptchaSID  string `json:"captcha_sid"`
	CaptchaImg  string `json:"captcha_img"`
	RedirectURI string `json:"redirect_uri"`
}

// IsCaptcha reports whether VK wants a captcha before going on.
func (e *APIError) IsCaptcha() bool { return e.Code == 14 }

func (e *APIError) Error() string { return fmt.Sprintf("VK API %d: %s", e.Code, e.Msg) }

type Track struct {
	ID        int64  `json:"id"`
	OwnerID   int64  `json:"owner_id"`
	Artist    string `json:"artist"`
	Title     string `json:"title"`
	Duration  int    `json:"duration"`
	URL       string `json:"url"`
	AccessKey string `json:"access_key"`
	Album     *struct {
		Title string `json:"title"`
	} `json:"album,omitempty"`
}

// AlbumTitle is the track's album name ("" if VK doesn't know it).
func (t Track) AlbumTitle() string {
	if t.Album == nil {
		return ""
	}
	return t.Album.Title
}

func (t Track) Key() string { return fmt.Sprintf("%d_%d", t.OwnerID, t.ID) }

// Playable reports whether the track has a usable stream URL.
func (t Track) Playable() bool {
	return t.URL != "" && !strings.Contains(t.URL, "audio_api_unavailable")
}

type Playlist struct {
	ID        int64  `json:"id"`
	OwnerID   int64  `json:"owner_id"`
	Title     string `json:"title"`
	Count     int    `json:"count"`
	AccessKey string `json:"access_key"`
	Original  *struct {
		PlaylistID int64  `json:"playlist_id"`
		OwnerID    int64  `json:"owner_id"`
		AccessKey  string `json:"access_key"`
	} `json:"original"`
}

// Source returns ids to use when fetching the playlist's tracks.
func (p Playlist) Source() (owner, id int64, key string) {
	if p.Original != nil && p.Original.PlaylistID != 0 {
		return p.Original.OwnerID, p.Original.PlaylistID, p.Original.AccessKey
	}
	return p.OwnerID, p.ID, p.AccessKey
}

type User struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

func (c *Client) Call(ctx context.Context, method string, params url.Values, out any) error {
	if params == nil {
		params = url.Values{}
	}
	if c.Token != "" {
		params.Set("access_token", c.Token)
	}
	if c.Version != "" {
		params.Set("v", c.Version)
	} else {
		params.Set("v", APIVersion)
	}
	params.Set("lang", "ru")
	params.Set("https", "1")
	for k, v := range c.Extra {
		params[k] = v
	}
	// The Android-style device id belongs to the official-app flow; Kate tokens
	// were issued without it and VK may treat the mismatch as suspicious.
	if c.DeviceID != "" && c.App.Name != Kate.Name {
		params.Set("device_id", c.DeviceID)
	}
	base := c.App.APIBase
	if o := os.Getenv("VKTUI_API_BASE"); o != "" {
		base = o
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+method, strings.NewReader(params.Encode()))
	if err != nil {
		return err
	}
	ua := c.App.UA
	if c.UA != "" {
		ua = c.UA
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("нет связи с VK (проверь VPN/прокси): %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var env struct {
		Response json.RawMessage `json:"response"`
		Error    *APIError       `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("непонятный ответ VK (HTTP %d)", resp.StatusCode)
	}
	if env.Error != nil {
		return env.Error
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(env.Response, out)
}

// itemsOrArray decodes either {"count":N,"items":[...]} or a bare array.
func itemsOrArray[T any](raw json.RawMessage) ([]T, int, error) {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) > 0 && raw[0] == '[' {
		var arr []T
		err := json.Unmarshal(raw, &arr)
		return arr, len(arr), err
	}
	var obj struct {
		Count int `json:"count"`
		Items []T `json:"items"`
	}
	err := json.Unmarshal(raw, &obj)
	return obj.Items, obj.Count, err
}

func (c *Client) tracks(ctx context.Context, method string, p url.Values) ([]Track, int, error) {
	var raw json.RawMessage
	if err := c.Call(ctx, method, p, &raw); err != nil {
		return nil, 0, err
	}
	return itemsOrArray[Track](raw)
}

func i64(v int64) string { return strconv.FormatInt(v, 10) }

func (c *Client) Me(ctx context.Context) (User, error) {
	var us []User
	if err := c.Call(ctx, "users.get", nil, &us); err != nil {
		return User{}, err
	}
	if len(us) == 0 {
		return User{}, fmt.Errorf("users.get: пустой ответ")
	}
	return us[0], nil
}

// Audio returns user's tracks (ownerID 0 = self) or a playlist's tracks (albumID != 0).
func (c *Client) Audio(ctx context.Context, ownerID, albumID int64, accessKey string, offset, count int) ([]Track, int, error) {
	p := url.Values{"offset": {strconv.Itoa(offset)}, "count": {strconv.Itoa(count)}}
	if ownerID != 0 {
		p.Set("owner_id", i64(ownerID))
	}
	if albumID != 0 {
		p.Set("album_id", i64(albumID))
	}
	if accessKey != "" {
		p.Set("access_key", accessKey)
	}
	return c.tracks(ctx, "audio.get", p)
}

func (c *Client) Search(ctx context.Context, q string, offset, count int) ([]Track, int, error) {
	p := url.Values{"q": {q}, "offset": {strconv.Itoa(offset)}, "count": {strconv.Itoa(count)}, "auto_complete": {"1"}, "sort": {"2"}}
	return c.tracks(ctx, "audio.search", p)
}

func (c *Client) Recommendations(ctx context.Context, offset, count int) ([]Track, int, error) {
	p := url.Values{"offset": {strconv.Itoa(offset)}, "count": {strconv.Itoa(count)}}
	return c.tracks(ctx, "audio.getRecommendations", p)
}

// StreamMix returns the next tracks of the personal endless "VK Mix" stream
// (same call as the official app / Music-M). more=false starts a fresh stream.
func (c *Client) StreamMix(ctx context.Context, more bool, count int) ([]Track, int, error) {
	app := "0"
	if more {
		app = "1"
	}
	cc := *c
	cc.Version = "5.243"
	return cc.tracks(ctx, "audio.getStreamMixAudios", url.Values{"mix_id": {"common"}, "append": {app}, "count": {strconv.Itoa(count)}})
}

// LyricLine is one time-synced line from VK (times in milliseconds).
type LyricLine struct {
	Begin int    `json:"begin"`
	End   int    `json:"end"`
	Line  string `json:"line"`
}

// Lyrics returns VK's lyrics for a track ("owner_id_id"): synced lines and/or plain text.
func (c *Client) Lyrics(ctx context.Context, key string) ([]LyricLine, []string, error) {
	cc := *c
	cc.Version = "5.243"
	var r struct {
		Lyrics struct {
			Timestamps []LyricLine `json:"timestamps"`
			Text       []string    `json:"text"`
		} `json:"lyrics"`
		Text string `json:"text"` // old API shape
	}
	if err := cc.Call(ctx, "audio.getLyrics", url.Values{"audio_id": {key}}, &r); err != nil {
		return nil, nil, err
	}
	text := r.Lyrics.Text
	if len(text) == 0 && r.Text != "" {
		text = strings.Split(strings.ReplaceAll(r.Text, "\r", ""), "\n")
	}
	return r.Lyrics.Timestamps, text, nil
}

func (c *Client) Popular(ctx context.Context, offset, count int) ([]Track, int, error) {
	p := url.Values{"offset": {strconv.Itoa(offset)}, "count": {strconv.Itoa(count)}, "only_eng": {"0"}}
	return c.tracks(ctx, "audio.getPopular", p)
}

func (c *Client) Playlists(ctx context.Context, ownerID int64, offset, count int) ([]Playlist, int, error) {
	p := url.Values{"owner_id": {i64(ownerID)}, "offset": {strconv.Itoa(offset)}, "count": {strconv.Itoa(count)}}
	var raw json.RawMessage
	if err := c.Call(ctx, "audio.getPlaylists", p, &raw); err != nil {
		return nil, 0, err
	}
	return itemsOrArray[Playlist](raw)
}

// Resolve fetches a fresh stream URL for a track.
func (c *Client) Resolve(ctx context.Context, t Track) (Track, error) {
	id := t.Key()
	if t.AccessKey != "" {
		id += "_" + t.AccessKey
	}
	var res []Track
	if err := c.Call(ctx, "audio.getById", url.Values{"audios": {id}}, &res); err != nil {
		return t, err
	}
	if len(res) == 0 {
		return t, fmt.Errorf("трек недоступен")
	}
	return res[0], nil
}

func (c *Client) Add(ctx context.Context, t Track) error {
	p := url.Values{"owner_id": {i64(t.OwnerID)}, "audio_id": {i64(t.ID)}}
	if t.AccessKey != "" {
		p.Set("access_key", t.AccessKey)
	}
	return c.Call(ctx, "audio.add", p, nil)
}
