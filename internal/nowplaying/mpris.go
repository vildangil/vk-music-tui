//go:build linux || freebsd

// Package nowplaying shows the current track in the desktop's "Now playing"
// widget. On Linux/BSD that is MPRIS over D-Bus (GNOME, KDE, waybar, playerctl…);
// mpv run with --no-config does not provide it itself.
package nowplaying

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"

	"vktui/internal/player"
)

const (
	objPath     = "/org/mpris/MediaPlayer2"
	ifaceRoot   = "org.mpris.MediaPlayer2"
	ifacePlayer = "org.mpris.MediaPlayer2.Player"
	ifaceProps  = "org.freedesktop.DBus.Properties"
)

// Wrap returns e with MPRIS attached, or e itself when there is no session bus.
func Wrap(e player.Engine) player.Engine {
	if os.Getenv("VKTUI_NO_MPRIS") != "" {
		return e
	}
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return e
	}
	w := &mpris{Engine: e, conn: conn, stop: make(chan struct{})}
	if err := w.export(); err != nil {
		conn.Close()
		return e
	}
	go w.watch()
	return w
}

type mpris struct {
	player.Engine
	conn *dbus.Conn
	stop chan struct{}
	once sync.Once

	mu                   sync.Mutex
	title, artist, album string
	track                int
	loaded               bool
	lastStatus           string
	lastDur              float64
	lastVol              float64
}

// ---------- engine overrides ----------

func (w *mpris) Load(url, title, artist, album string) {
	w.mu.Lock()
	w.title, w.artist, w.album = title, artist, album
	w.track++
	w.loaded = true
	w.lastDur = 0
	w.mu.Unlock()
	w.Engine.Load(url, title, artist, album)
	w.changed(map[string]dbus.Variant{"Metadata": dbus.MakeVariant(w.metadata()), "PlaybackStatus": dbus.MakeVariant("Playing")})
	w.mu.Lock()
	w.lastStatus = "Playing"
	w.mu.Unlock()
}

func (w *mpris) Stop() {
	w.mu.Lock()
	w.loaded = false
	w.mu.Unlock()
	w.Engine.Stop()
	w.refresh()
}

func (w *mpris) SeekAbs(s float64) {
	w.Engine.SeekAbs(s)
	_ = w.conn.Emit(objPath, ifacePlayer+".Seeked", int64(s*1e6))
}

func (w *mpris) Seek(d float64) {
	w.Engine.Seek(d)
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = w.conn.Emit(objPath, ifacePlayer+".Seeked", int64(w.Engine.State().Pos*1e6))
	}()
}

func (w *mpris) Close() {
	w.once.Do(func() {
		close(w.stop)
		w.conn.Close()
	})
	w.Engine.Close()
}

// ---------- state → D-Bus ----------

func (w *mpris) status() string {
	w.mu.Lock()
	loaded := w.loaded
	w.mu.Unlock()
	st := w.Engine.State()
	switch {
	case !loaded:
		return "Stopped"
	case st.Paused:
		return "Paused"
	default:
		return "Playing"
	}
}

func (w *mpris) metadata() map[string]dbus.Variant {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.loaded {
		return map[string]dbus.Variant{"mpris:trackid": dbus.MakeVariant(dbus.ObjectPath("/org/mpris/MediaPlayer2/TrackList/NoTrack"))}
	}
	m := map[string]dbus.Variant{
		"mpris:trackid": dbus.MakeVariant(dbus.ObjectPath(fmt.Sprintf("/org/vktui/track/%d", w.track))),
		"xesam:title":   dbus.MakeVariant(w.title),
		"xesam:artist":  dbus.MakeVariant([]string{w.artist}),
	}
	if w.album != "" {
		m["xesam:album"] = dbus.MakeVariant(w.album)
	}
	if d := w.Engine.State().Dur; d > 0 {
		m["mpris:length"] = dbus.MakeVariant(int64(d * 1e6))
	}
	return m
}

func (w *mpris) changed(props map[string]dbus.Variant) {
	_ = w.conn.Emit(objPath, ifaceProps+".PropertiesChanged", ifacePlayer, props, []string{})
}

// refresh announces status / length / volume changes.
func (w *mpris) refresh() {
	st := w.Engine.State()
	s := w.status()
	props := map[string]dbus.Variant{}
	w.mu.Lock()
	if s != w.lastStatus {
		w.lastStatus = s
		props["PlaybackStatus"] = dbus.MakeVariant(s)
	}
	durChanged := st.Dur > 0 && st.Dur != w.lastDur
	if durChanged {
		w.lastDur = st.Dur
	}
	if st.Volume != w.lastVol {
		w.lastVol = st.Volume
		props["Volume"] = dbus.MakeVariant(st.Volume / 100)
	}
	w.mu.Unlock()
	if durChanged || s == "Stopped" && len(props) > 0 {
		props["Metadata"] = dbus.MakeVariant(w.metadata())
	}
	if len(props) > 0 {
		w.changed(props)
	}
}

func (w *mpris) watch() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-w.stop:
			return
		case <-t.C:
			w.refresh()
		}
	}
}

func (w *mpris) key(action string) {
	select {
	case w.Engine.Events() <- player.MediaKeyEvent{Action: action}:
	default:
	}
}

// ---------- exported D-Bus objects ----------

type rootObj struct{ w *mpris }

func (rootObj) Raise() *dbus.Error { return nil }
func (r rootObj) Quit() *dbus.Error {
	return nil // quitting is done in the terminal
}

type playerObj struct{ w *mpris }

func (p playerObj) Next() *dbus.Error     { p.w.key("next"); return nil }
func (p playerObj) Previous() *dbus.Error { p.w.key("prev"); return nil }
func (p playerObj) PlayPause() *dbus.Error {
	p.w.key("playpause")
	return nil
}
func (p playerObj) Pause() *dbus.Error {
	if p.w.status() == "Playing" {
		p.w.key("playpause")
	}
	return nil
}
func (p playerObj) Play() *dbus.Error {
	if p.w.status() == "Paused" {
		p.w.key("playpause")
	}
	return nil
}
func (p playerObj) Stop() *dbus.Error {
	return p.Pause()
}

// SeekDBus is MPRIS "Seek" (renamed: go vet expects Seek to be io.Seeker).
func (p playerObj) SeekDBus(us int64) *dbus.Error {
	p.w.Seek(float64(us) / 1e6)
	return nil
}
func (p playerObj) SetPosition(_ dbus.ObjectPath, us int64) *dbus.Error {
	p.w.SeekAbs(float64(us) / 1e6)
	return nil
}
func (playerObj) OpenUri(string) *dbus.Error { return nil }

type propsObj struct{ w *mpris }

func (o propsObj) all(iface string) (map[string]dbus.Variant, bool) {
	switch iface {
	case ifaceRoot:
		return map[string]dbus.Variant{
			"CanQuit":             dbus.MakeVariant(false),
			"CanRaise":            dbus.MakeVariant(false),
			"HasTrackList":        dbus.MakeVariant(false),
			"Identity":            dbus.MakeVariant("VK Music TUI"),
			"DesktopEntry":        dbus.MakeVariant("vk-music-tui"),
			"SupportedUriSchemes": dbus.MakeVariant([]string{}),
			"SupportedMimeTypes":  dbus.MakeVariant([]string{}),
		}, true
	case ifacePlayer:
		st := o.w.Engine.State()
		return map[string]dbus.Variant{
			"PlaybackStatus": dbus.MakeVariant(o.w.status()),
			"LoopStatus":     dbus.MakeVariant("None"),
			"Rate":           dbus.MakeVariant(1.0),
			"Shuffle":        dbus.MakeVariant(false),
			"Metadata":       dbus.MakeVariant(o.w.metadata()),
			"Volume":         dbus.MakeVariant(st.Volume / 100),
			"Position":       dbus.MakeVariant(int64(st.Pos * 1e6)),
			"MinimumRate":    dbus.MakeVariant(1.0),
			"MaximumRate":    dbus.MakeVariant(1.0),
			"CanGoNext":      dbus.MakeVariant(true),
			"CanGoPrevious":  dbus.MakeVariant(true),
			"CanPlay":        dbus.MakeVariant(true),
			"CanPause":       dbus.MakeVariant(true),
			"CanSeek":        dbus.MakeVariant(true),
			"CanControl":     dbus.MakeVariant(true),
		}, true
	}
	return nil, false
}

func (o propsObj) Get(iface, prop string) (dbus.Variant, *dbus.Error) {
	m, ok := o.all(iface)
	if !ok {
		return dbus.Variant{}, dbus.MakeFailedError(fmt.Errorf("unknown interface %s", iface))
	}
	v, ok := m[prop]
	if !ok {
		return dbus.Variant{}, dbus.MakeFailedError(fmt.Errorf("unknown property %s", prop))
	}
	return v, nil
}

func (o propsObj) GetAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	m, ok := o.all(iface)
	if !ok {
		return nil, dbus.MakeFailedError(fmt.Errorf("unknown interface %s", iface))
	}
	return m, nil
}

func (o propsObj) Set(iface, prop string, v dbus.Variant) *dbus.Error {
	if iface == ifacePlayer && prop == "Volume" {
		if f, ok := v.Value().(float64); ok {
			o.w.Engine.SetVolume(max(0, min(150, f*100)))
		}
	}
	return nil
}

const introspectXML = `<node>
 <interface name="org.freedesktop.DBus.Introspectable">
  <method name="Introspect"><arg name="data" direction="out" type="s"/></method>
 </interface>
 <interface name="org.freedesktop.DBus.Properties">
  <method name="Get"><arg direction="in" type="s"/><arg direction="in" type="s"/><arg direction="out" type="v"/></method>
  <method name="GetAll"><arg direction="in" type="s"/><arg direction="out" type="a{sv}"/></method>
  <method name="Set"><arg direction="in" type="s"/><arg direction="in" type="s"/><arg direction="in" type="v"/></method>
  <signal name="PropertiesChanged"><arg type="s"/><arg type="a{sv}"/><arg type="as"/></signal>
 </interface>
 <interface name="org.mpris.MediaPlayer2">
  <method name="Raise"/><method name="Quit"/>
  <property name="CanQuit" type="b" access="read"/>
  <property name="CanRaise" type="b" access="read"/>
  <property name="HasTrackList" type="b" access="read"/>
  <property name="Identity" type="s" access="read"/>
  <property name="DesktopEntry" type="s" access="read"/>
  <property name="SupportedUriSchemes" type="as" access="read"/>
  <property name="SupportedMimeTypes" type="as" access="read"/>
 </interface>
 <interface name="org.mpris.MediaPlayer2.Player">
  <method name="Next"/><method name="Previous"/><method name="Pause"/><method name="PlayPause"/>
  <method name="Stop"/><method name="Play"/>
  <method name="Seek"><arg name="Offset" direction="in" type="x"/></method>
  <method name="SetPosition"><arg name="TrackId" direction="in" type="o"/><arg name="Position" direction="in" type="x"/></method>
  <method name="OpenUri"><arg name="Uri" direction="in" type="s"/></method>
  <signal name="Seeked"><arg name="Position" type="x"/></signal>
  <property name="PlaybackStatus" type="s" access="read"/>
  <property name="LoopStatus" type="s" access="read"/>
  <property name="Rate" type="d" access="read"/>
  <property name="Shuffle" type="b" access="read"/>
  <property name="Metadata" type="a{sv}" access="read"/>
  <property name="Volume" type="d" access="readwrite"/>
  <property name="Position" type="x" access="read"/>
  <property name="MinimumRate" type="d" access="read"/>
  <property name="MaximumRate" type="d" access="read"/>
  <property name="CanGoNext" type="b" access="read"/>
  <property name="CanGoPrevious" type="b" access="read"/>
  <property name="CanPlay" type="b" access="read"/>
  <property name="CanPause" type="b" access="read"/>
  <property name="CanSeek" type="b" access="read"/>
  <property name="CanControl" type="b" access="read"/>
 </interface>
</node>`

type introspectObj struct{}

func (introspectObj) Introspect() (string, *dbus.Error) { return introspectXML, nil }

func (w *mpris) export() error {
	if err := w.conn.ExportWithMap(playerObj{w}, map[string]string{"SeekDBus": "Seek"}, objPath, ifacePlayer); err != nil {
		return err
	}
	for iface, obj := range map[string]any{
		ifaceRoot:                             rootObj{w},
		ifaceProps:                            propsObj{w},
		"org.freedesktop.DBus.Introspectable": introspectObj{},
	} {
		if err := w.conn.Export(obj, objPath, iface); err != nil {
			return err
		}
	}
	name := "org.mpris.MediaPlayer2.vktui"
	r, err := w.conn.RequestName(name, dbus.NameFlagDoNotQueue)
	if err != nil {
		return err
	}
	if r != dbus.RequestNameReplyPrimaryOwner { // another vktui is running
		if _, err := w.conn.RequestName(fmt.Sprintf("%s.instance%d", name, os.Getpid()), dbus.NameFlagDoNotQueue); err != nil {
			return err
		}
	}
	return nil
}
