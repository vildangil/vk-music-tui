//go:build !linux && !freebsd

// Package nowplaying shows the current track in the desktop's "Now playing"
// widget. On Windows and macOS mpv does it itself (--media-controls).
package nowplaying

import "vktui/internal/player"

// Wrap returns e unchanged on this OS.
func Wrap(e player.Engine) player.Engine { return e }
