#!/bin/bash
# Installs mpv — the audio engine of vktui — on macOS.
# Used by the .pkg postinstall (as root) and by `vktui deps` (as the user).
#   1. Homebrew  → brew install mpv   (as the logged-in user: brew refuses root)
#   2. MacPorts  → port install mpv
#   3. otherwise → the official mpv.app build from github.com/mpv-player/mpv
#                  goes to /Applications/mpv.app
# Set VKTUI_DEPS_FFMPEG=1 to also install ffmpeg (ffplay engine, downloads).
set -u
LOG="${VKTUI_DEPS_LOG:-/tmp/vk-music-tui-deps.log}"
say() { echo "vk-music-tui: $*"; echo "$(date '+%F %T') $*" >>"$LOG" 2>/dev/null; }

have_mpv() {
  for p in /opt/homebrew/bin/mpv /usr/local/bin/mpv /opt/local/bin/mpv /Applications/mpv.app/Contents/MacOS/mpv; do
    [ -x "$p" ] && return 0
  done
  return 1
}

if have_mpv && [ "${VKTUI_DEPS_FFMPEG:-0}" != "1" ]; then
  say "mpv уже установлен"
  exit 0
fi

# the user sitting at the Mac (postinstall runs as root)
if [ "$(id -u)" = "0" ]; then
  USER_NAME="$(stat -f%Su /dev/console 2>/dev/null)"
else
  USER_NAME="$(id -un)"
fi
as_user() {
  if [ "$(id -u)" = "0" ]; then
    [ -n "$USER_NAME" ] && [ "$USER_NAME" != "root" ] || return 1
    sudo -u "$USER_NAME" -H env PATH="/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin" HOMEBREW_NO_AUTO_UPDATE=1 "$@"
  else
    env HOMEBREW_NO_AUTO_UPDATE=1 "$@"
  fi
}

PKGS="mpv"
[ "${VKTUI_DEPS_FFMPEG:-0}" = "1" ] && PKGS="mpv ffmpeg"

# 1. Homebrew
for brew in /opt/homebrew/bin/brew /usr/local/bin/brew; do
  if [ -x "$brew" ]; then
    say "ставлю $PKGS через Homebrew ($brew)…"
    # shellcheck disable=SC2086
    if as_user "$brew" install $PKGS >>"$LOG" 2>&1; then
      say "готово (Homebrew)"
      exit 0
    fi
    say "Homebrew не справился, подробности в $LOG"
    break
  fi
done

# 2. MacPorts
if [ -x /opt/local/bin/port ]; then
  say "ставлю $PKGS через MacPorts…"
  SUDO=""
  [ "$(id -u)" = "0" ] || SUDO="sudo"
  # shellcheck disable=SC2086
  if $SUDO /opt/local/bin/port -N install $PKGS >>"$LOG" 2>&1; then
    say "готово (MacPorts)"
    exit 0
  fi
  say "MacPorts не справился, подробности в $LOG"
fi

have_mpv && exit 0

# 3. official build from GitHub
ARCH="$(uname -m)"
[ "$ARCH" = "arm64" ] && KIND="arm" || KIND="intel"
OS_MAJOR="$(sw_vers -productVersion | cut -d. -f1)"
say "скачиваю mpv с github.com/mpv-player/mpv (macOS $OS_MAJOR, $KIND)…"

API="$(curl -fsSL --max-time 30 https://api.github.com/repos/mpv-player/mpv/releases/latest 2>>"$LOG")"
# pick the newest build made for this macOS version or older
URL=""
BEST=0
for u in $(printf '%s\n' "$API" | grep -o '"browser_download_url": *"[^"]*-macos-[0-9]*-'"$KIND"'\.zip"' | sed 's/.*"\(https[^"]*\)"/\1/'); do
  v="$(printf '%s' "$u" | sed -E 's/.*-macos-([0-9]+)-.*/\1/')"
  if [ "$v" -le "$OS_MAJOR" ] && [ "$v" -gt "$BEST" ]; then
    BEST="$v"
    URL="$u"
  fi
done
if [ -z "$URL" ]; then
  say "нет готовой сборки mpv для macOS $OS_MAJOR ($KIND). Поставь Homebrew (https://brew.sh) и выполни: brew install mpv"
  exit 1
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
if ! curl -fL --max-time 600 -o "$TMP/mpv.zip" "$URL" >>"$LOG" 2>&1; then
  say "не удалось скачать $URL"
  exit 1
fi
( cd "$TMP" && unzip -q mpv.zip && { [ -d mpv.app ] || tar xzf mpv.tar.gz; } ) >>"$LOG" 2>&1
if [ ! -x "$TMP/mpv.app/Contents/MacOS/mpv" ]; then
  say "в архиве нет mpv.app"
  exit 1
fi

DEST="/Applications"
if [ ! -w "$DEST" ]; then
  DEST="$HOME/Applications"
  mkdir -p "$DEST"
fi
rm -rf "$DEST/mpv.app"
ditto "$TMP/mpv.app" "$DEST/mpv.app"
xattr -dr com.apple.quarantine "$DEST/mpv.app" 2>/dev/null || true
if [ "$(id -u)" = "0" ] && [ -n "$USER_NAME" ] && [ "$USER_NAME" != "root" ]; then
  chown -R "$USER_NAME":staff "$DEST/mpv.app" 2>/dev/null || true
fi
# vktui looks for /Applications/mpv.app; link a per-user install where it searches PATH
if [ "$DEST" != "/Applications" ]; then
  mkdir -p "$HOME/.local/bin"
  ln -sf "$DEST/mpv.app/Contents/MacOS/mpv" "$HOME/.local/bin/mpv"
  say "mpv установлен в $DEST/mpv.app (ссылка: ~/.local/bin/mpv — добавь эту папку в PATH или задай VKTUI_MPV)"
else
  say "mpv установлен в $DEST/mpv.app"
fi
exit 0
