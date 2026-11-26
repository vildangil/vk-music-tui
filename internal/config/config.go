package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type Config struct {
	Token      string `json:"token"`
	App        string `json:"app,omitempty"` // "android" (default) or "kate"
	DeviceID   string `json:"device_id,omitempty"`
	UserID     int64  `json:"user_id"`
	Volume     int    `json:"volume"`
	Shuffle    bool   `json:"shuffle"`
	Repeat     int    `json:"repeat"`
	MPV        string `json:"mpv_path,omitempty"`
	Engine     string `json:"engine,omitempty"`      // "" / "auto", "mpv", "vlc", "ffplay", "mplayer"
	EnginePath string `json:"engine_path,omitempty"` // program path for a non-mpv engine

	// Settings screen
	EQPreset  string      `json:"eq_preset,omitempty"`  // name from ui presets; "" = off
	EQBands   [10]float64 `json:"eq_bands"`             // custom preset gains, dB
	Normalize bool        `json:"normalize"`            // level out loud/quiet tracks
	Autoplay  *bool       `json:"autoplay,omitempty"`   // continue with VK Mix when the queue ends (default on)
	Accent    string      `json:"accent,omitempty"`     // green (default), blue, cyan, purple, pink, red, orange, yellow, mint, lavender, mono
	VolStep   int         `json:"vol_step,omitempty"`   // volume step for +/-, default 5
	Lyrics    bool        `json:"lyrics"`               // lyrics panel open
	NerdIcons *bool       `json:"nerd_icons,omitempty"` // Nerd Font icons (need a Nerd Font in the terminal)

	CacheOn     *bool  `json:"cache,omitempty"`         // keep played tracks on disk (default on)
	CacheMB     int    `json:"cache_mb,omitempty"`      // cache size limit, default 1024
	DownloadDir string `json:"download_dir,omitempty"`  // default ~/Music/VK Music
	OfflineAll  bool   `json:"offline_all"`             // keep every track of "Моя музыка" offline
	LyricsSrc   string `json:"lyrics_source,omitempty"` // "" = auto (VK → LRCLIB → Genius), or "vk" / "lrclib" / "genius"

	// All signed-in accounts; Token/App/DeviceID/UserID above are the active one.
	Accounts []Account `json:"accounts,omitempty"`
}

// Account is one signed-in VK account.
type Account struct {
	Name     string `json:"name"`
	UserID   int64  `json:"user_id"`
	Token    string `json:"token"`
	App      string `json:"app,omitempty"`
	DeviceID string `json:"device_id,omitempty"`
}

// syncAccounts makes sure the active account is in the list (older configs had only one).
func (c *Config) syncAccounts() {
	if c.Token == "" {
		return
	}
	for _, a := range c.Accounts {
		if a.Token == c.Token {
			return
		}
	}
	// a re-login of a known user (new token): update that entry
	for i, a := range c.Accounts {
		if a.UserID != 0 && a.UserID == c.UserID {
			c.Accounts[i].Token, c.Accounts[i].App, c.Accounts[i].DeviceID = c.Token, c.App, c.DeviceID
			return
		}
	}
	c.Accounts = append(c.Accounts, Account{UserID: c.UserID, Token: c.Token, App: c.App, DeviceID: c.DeviceID})
}

// ActiveIndex returns the index of the active account in Accounts (-1 if none).
func (c *Config) ActiveIndex() int {
	c.syncAccounts()
	for i, a := range c.Accounts {
		if a.Token == c.Token {
			return i
		}
	}
	return -1
}

// AddAccount stores an account (replacing the same VK user) and makes it active.
func (c *Config) AddAccount(a Account) {
	c.syncAccounts()
	replaced := false
	for i := range c.Accounts {
		if c.Accounts[i].UserID == a.UserID && a.UserID != 0 {
			c.Accounts[i] = a
			replaced = true
		}
	}
	if !replaced {
		c.Accounts = append(c.Accounts, a)
	}
	c.Use(a)
}

// Use makes an account active.
func (c *Config) Use(a Account) {
	c.Token, c.App, c.DeviceID, c.UserID = a.Token, a.App, a.DeviceID, a.UserID
}

// SetActiveName remembers the display name of the active account.
func (c *Config) SetActiveName(name string) {
	if i := c.ActiveIndex(); i >= 0 && name != "" {
		c.Accounts[i].Name = name
	}
}

// RemoveActive signs out of the active account and switches to another one, if any.
func (c *Config) RemoveActive() {
	i := c.ActiveIndex()
	if i >= 0 {
		c.Accounts = append(c.Accounts[:i], c.Accounts[i+1:]...)
	}
	if len(c.Accounts) > 0 {
		c.Use(c.Accounts[0])
	} else {
		c.Use(Account{})
	}
}

// AccountName is a label for an account.
func (a Account) Label() string {
	if a.Name != "" {
		return a.Name
	}
	if a.UserID != 0 {
		return "id" + itoa(a.UserID)
	}
	return "аккаунт"
}

func itoa(v int64) string { b, _ := json.Marshal(v); return string(b) }

// DataDir is for large, durable app data (offline library). Unlike the cache it
// is never cleaned automatically.
func DataDir() string {
	if d := os.Getenv("VKTUI_DATA_DIR"); d != "" {
		return d
	}
	switch runtime.GOOS {
	case "windows":
		if la := os.Getenv("LOCALAPPDATA"); la != "" {
			return filepath.Join(la, "vktui")
		}
	case "darwin":
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, "Library", "Application Support", "vktui")
		}
	default:
		if x := os.Getenv("XDG_DATA_HOME"); x != "" {
			return filepath.Join(x, "vktui")
		}
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, ".local", "share", "vktui")
		}
	}
	return filepath.Join(Dir(), "data")
}

// OfflineDir holds the offline library (<owner>_<id>.mp3).
// Windows: next to the program; Linux/macOS: inside the music folder.
func OfflineDir() string {
	if d := os.Getenv("VKTUI_DATA_DIR"); d != "" {
		return filepath.Join(d, "offline")
	}
	if runtime.GOOS == "windows" {
		if d := appDir(); d != "" {
			return filepath.Join(d, "offline")
		}
		return filepath.Join(DataDir(), "offline")
	}
	return filepath.Join(MusicDir(), "VK Music", ".offline")
}

func (c *Config) CacheEnabled() bool { return c.CacheOn == nil || *c.CacheOn }

func (c *Config) CacheLimitMB() int {
	if c.CacheMB <= 0 {
		return 1024
	}
	return c.CacheMB
}

// CacheDir holds cached tracks (named <owner>_<id>.mp3).
func CacheDir() string {
	if d := os.Getenv("VKTUI_CACHE_DIR"); d != "" {
		return d
	}
	if runtime.GOOS == "windows" {
		if d := appDir(); d != "" {
			return filepath.Join(d, "cache")
		}
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return filepath.Join(Dir(), "cache")
	}
	return filepath.Join(base, "vktui", "audio")
}

// Downloads is where downloaded tracks go: the program folder on Windows,
// the music folder ("VK Music") on Linux and macOS.
func (c *Config) Downloads() string {
	if c.DownloadDir != "" {
		return c.DownloadDir
	}
	if runtime.GOOS == "windows" {
		if d := appDir(); d != "" {
			return filepath.Join(d, "music")
		}
	}
	return filepath.Join(MusicDir(), "VK Music")
}

// MusicDir is the user's music folder (honours XDG_MUSIC_DIR, e.g. "~/Музыка").
func MusicDir() string {
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "linux" || runtime.GOOS == "freebsd" {
		if d := os.Getenv("XDG_MUSIC_DIR"); d != "" {
			return d
		}
		if b, err := os.ReadFile(filepath.Join(home, ".config", "user-dirs.dirs")); err == nil {
			for _, line := range strings.Split(string(b), "\n") {
				if v, ok := strings.CutPrefix(strings.TrimSpace(line), "XDG_MUSIC_DIR="); ok {
					v = strings.Trim(v, `"`)
					v = strings.Replace(v, "$HOME", home, 1)
					if v != "" && v != home {
						return v
					}
				}
			}
		}
	}
	return filepath.Join(home, "Music")
}

var appDirCache *string

// appDir is the program's own folder when it is writable (the installer puts
// vktui in a per-user folder), otherwise "".
func appDir() string {
	if appDirCache != nil {
		return *appDirCache
	}
	dir := ""
	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		d := filepath.Dir(exe)
		low := strings.ToLower(d)
		// never inside Go's build cache / temp folders (go run, go test)
		if !strings.Contains(low, "go-build") && !strings.Contains(low, strings.ToLower(os.TempDir())) {
			probe := filepath.Join(d, ".vktui-write-test")
			if f, err := os.Create(probe); err == nil {
				f.Close()
				os.Remove(probe)
				dir = d
			}
		}
	}
	appDirCache = &dir
	return dir
}

// MigrateDir moves an old media folder to its new place once (same disk: instant).
func MigrateDir(from, to string) {
	if from == "" || to == "" || from == to {
		return
	}
	if st, err := os.Stat(from); err != nil || !st.IsDir() {
		return
	}
	if _, err := os.Stat(to); err == nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(to), 0o755)
	_ = os.Rename(from, to)
}

// OldMediaDirs are where earlier versions kept the offline library and the cache.
func OldMediaDirs() (offline, cache string) {
	if os.Getenv("VKTUI_DATA_DIR") != "" || os.Getenv("VKTUI_CACHE_DIR") != "" {
		return "", "" // tests / custom setups: never move real user files
	}
	offline = filepath.Join(DataDir(), "offline")
	if base, err := os.UserCacheDir(); err == nil {
		cache = filepath.Join(base, "vktui", "audio")
	}
	return
}

// NerdDetected is set at startup when no explicit choice was saved.
var NerdDetected bool

func (c *Config) NerdIconsOn() bool {
	if c.NerdIcons == nil {
		return NerdDetected
	}
	return *c.NerdIcons
}

func (c *Config) AutoplayOn() bool { return c.Autoplay == nil || *c.Autoplay }

func (c *Config) Step() int {
	if c.VolStep <= 0 {
		return 5
	}
	return c.VolStep
}

func Dir() string {
	if d := os.Getenv("VKTUI_CONFIG_DIR"); d != "" {
		return d
	}
	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	return filepath.Join(base, "vktui")
}

func Path() string { return filepath.Join(Dir(), "config.json") }

func Load() *Config {
	c := &Config{Volume: 70}
	if b, err := os.ReadFile(Path()); err == nil {
		_ = json.Unmarshal(b, c)
	}
	if c.Volume <= 0 || c.Volume > 150 {
		c.Volume = 70
	}
	return c
}

func (c *Config) Save() error {
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	tmp := Path() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, Path())
}
