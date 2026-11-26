#!/bin/bash
# Installs mpv — the audio engine of vktui — on macOS.
# Used by the .pkg postinstall (as root) and by `vktui deps` (as the user).
#
# Apple Silicon: Homebrew (prebuilt bottles, fast) → official mpv.app
#                (github.com/mpv-player/mpv) → stolendata build → MacPorts
# Intel:         official mpv.app (macOS 15+) → stolendata build (macOS 11+) →
#                MacPorts → Homebrew (Homebrew has no Intel bottles any more and
#                compiles mpv with all its dependencies from source — that can
#                take an hour, so the .pkg installer never does it)
# MacPorts is last everywhere: it has no binary archive for ffmpeg and builds
# it (and more) from source.
#
# Downloaded builds go to /Applications/mpv.app (or ~/Applications when not writable).
# VKTUI_DEPS_DRYRUN=1 only prints what would be installed (for tests);
# VKTUI_DEPS_ARCH / VKTUI_DEPS_OS override the detected arch and macOS version.
set -u
LOG="${VKTUI_DEPS_LOG:-/tmp/vk-music-tui-deps.log}"
# The installer writes the log as root; a later `vktui deps` can't append to it,
# and every ">>$LOG" redirect (curl, unzip…) would then fail.
if ! { : >>"$LOG"; } 2>/dev/null; then
  LOG="${TMPDIR:-/tmp}/vk-music-tui-deps-$(id -un).log"
  : >>"$LOG" 2>/dev/null || LOG=/dev/null
fi
say() { echo "vk-music-tui: $*"; echo "$(date '+%F %T') $*" >>"$LOG" 2>/dev/null; }

have_mpv() {
  for p in /opt/homebrew/bin/mpv /usr/local/bin/mpv /opt/local/bin/mpv \
           /Applications/mpv.app/Contents/MacOS/mpv "$HOME/Applications/mpv.app/Contents/MacOS/mpv"; do
    [ -x "$p" ] && return 0
  done
  return 1
}

DRY="${VKTUI_DEPS_DRYRUN:-0}"
if [ "$DRY" != 1 ] && have_mpv; then
  say "mpv уже установлен"
  exit 0
fi

ROOT=0
[ "$(id -u)" = "0" ] && ROOT=1
# the user sitting at the Mac (postinstall runs as root)
if [ "$ROOT" = 1 ]; then
  USER_NAME="$(stat -f%Su /dev/console 2>/dev/null)"
else
  USER_NAME="$(id -un)"
fi
ARCH="$(uname -m)"
# Rosetta: a process translated on Apple Silicon still reports x86_64
if [ "$ARCH" = "x86_64" ] && [ "$(sysctl -n sysctl.proc_translated 2>/dev/null)" = "1" ]; then
  ARCH="arm64"
fi
OS_MAJOR="$(sw_vers -productVersion | cut -d. -f1)"
ARCH="${VKTUI_DEPS_ARCH:-$ARCH}"
OS_MAJOR="${VKTUI_DEPS_OS:-$OS_MAJOR}"
PKGS="mpv"

# run: output goes to the terminal for `vktui deps`, only to the log in the installer
run_logged() {
  if [ "$ROOT" = 1 ]; then
    "$@" >>"$LOG" 2>&1
  else
    "$@" 2>&1 | tee -a "$LOG"
    return "${PIPESTATUS[0]}"
  fi
}

as_user() {
  if [ "$ROOT" = 1 ]; then
    [ -n "$USER_NAME" ] && [ "$USER_NAME" != "root" ] || return 1
    sudo -u "$USER_NAME" -H env PATH="/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin" \
      HOMEBREW_NO_AUTO_UPDATE=1 HOMEBREW_NO_INSTALL_CLEANUP=1 "$@"
  else
    env HOMEBREW_NO_AUTO_UPDATE=1 HOMEBREW_NO_INSTALL_CLEANUP=1 "$@"
  fi
}

try_brew() {
  [ "$DRY" = 1 ] && { say "[dry-run] Homebrew"; return 1; }
  for brew in /opt/homebrew/bin/brew /usr/local/bin/brew; do
    [ -x "$brew" ] || continue
    say "ставлю $PKGS через Homebrew ($brew)…"
    # shellcheck disable=SC2086
    if run_logged as_user "$brew" install $PKGS; then
      say "готово (Homebrew)"
      return 0
    fi
    say "Homebrew не справился, подробности в $LOG"
    return 1
  done
  return 1
}

try_port() {
  [ "$DRY" = 1 ] && { say "[dry-run] MacPorts"; return 1; }
  [ -x /opt/local/bin/port ] || return 1
  say "ставлю $PKGS через MacPorts…"
  local sudo=""
  [ "$ROOT" = 1 ] || sudo="sudo"
  # shellcheck disable=SC2086
  if run_logged $sudo /opt/local/bin/port -N install $PKGS; then
    say "готово (MacPorts)"
    return 0
  fi
  say "MacPorts не справился, подробности в $LOG"
  return 1
}

# install_app <dir with mpv.app>
install_app() {
  local src="$1/mpv.app" dest="/Applications"
  [ -x "$src/Contents/MacOS/mpv" ] || { say "в архиве нет mpv.app"; return 1; }
  [ -w "$dest" ] || { dest="$HOME/Applications"; mkdir -p "$dest"; }
  rm -rf "$dest/mpv.app"
  ditto "$src" "$dest/mpv.app" || return 1
  xattr -dr com.apple.quarantine "$dest/mpv.app" 2>/dev/null || true
  if [ "$ROOT" = 1 ] && [ -n "$USER_NAME" ] && [ "$USER_NAME" != "root" ]; then
    chown -R "$USER_NAME":staff "$dest/mpv.app" 2>/dev/null || true
  fi
  say "mpv установлен в $dest/mpv.app"
}

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# fetch <url> <file>: gives up when the speed stays under ~30 KB/s for 20 s
# (GitHub downloads can crawl at a few KB/s from some networks), so the next
# source gets its turn. Shows a progress bar in a terminal.
fetch() {
  local opts=(-fL --connect-timeout 15 --max-time 900 --speed-limit 30000 --speed-time 20 --retry 2 -o "$2")
  if [ "$ROOT" = 0 ] && [ -t 2 ]; then
    curl "${opts[@]}" --progress-bar "$1"
  else
    curl "${opts[@]}" -sS "$1" >>"$LOG" 2>&1
  fi || { say "не удалось скачать (медленно или нет связи): $1"; return 1; }
}

# Official build: newest release with a build for this macOS version or older.
try_github() {
  local kind="intel" url="" best=0 tag="" u v t
  [ "$ARCH" = "arm64" ] && kind="arm"
  say "ищу официальную сборку mpv (macOS $OS_MAJOR, $kind)…"
  local api
  api="$(curl -fsSL --max-time 30 "https://api.github.com/repos/mpv-player/mpv/releases?per_page=6" 2>>"$LOG")" || return 1
  for u in $(printf '%s\n' "$api" | grep -o '"browser_download_url": *"[^"]*-macos-[0-9]*-'"$kind"'\.zip"' | sed 's/.*"\(https[^"]*\)"/\1/'); do
    t="$(printf '%s' "$u" | sed -E 's#.*/download/([^/]+)/.*#\1#')"
    case "$t" in v[0-9]*) ;; *) continue ;; esac # skip nightly "git-release"
    [ -n "$tag" ] && [ "$t" != "$tag" ] && break # releases come newest first
    v="$(printf '%s' "$u" | sed -E 's/.*-macos-([0-9]+)-.*/\1/')"
    if [ "$v" -le "$OS_MAJOR" ] && [ "$v" -gt "$best" ]; then
      best="$v"; url="$u"; tag="$t"
    fi
  done
  if [ -z "$url" ]; then
    say "официальной сборки для macOS $OS_MAJOR ($kind) нет"
    return 1
  fi
  say "скачиваю $url"
  [ "$DRY" = 1 ] && return 0
  fetch "$url" "$TMP/mpv.zip" || return 1
  mkdir -p "$TMP/gh"
  ( cd "$TMP/gh" && unzip -q ../mpv.zip && { [ -d mpv.app ] || tar xzf mpv.tar.gz; } ) >>"$LOG" 2>&1
  install_app "$TMP/gh"
}

# Builds linked from mpv.io: Intel for macOS 11+, Apple Silicon for macOS 14+.
try_stolendata() {
  local file="mpv-latest.tar.gz" min=11
  if [ "$ARCH" = "arm64" ]; then file="mpv-arm64-latest.tar.gz"; min=14; fi
  [ "$OS_MAJOR" -ge "$min" ] || { say "сборки stolendata нужен macOS $min+"; return 1; }
  local url="https://laboratory.stolendata.net/~djinn/mpv_osx/$file"
  say "скачиваю $url"
  [ "$DRY" = 1 ] && return 0
  fetch "$url" "$TMP/sd.tar.gz" || return 1
  mkdir -p "$TMP/sd"
  tar xzf "$TMP/sd.tar.gz" -C "$TMP/sd" >>"$LOG" 2>&1
  install_app "$TMP/sd"
}

if [ "$ARCH" = "arm64" ]; then
  try_brew || try_github || try_stolendata || try_port
else
  try_github || try_stolendata || try_port || {
    if [ "$ROOT" = 0 ] && [ -x /usr/local/bin/brew ]; then
      say "⚠ Homebrew на Intel собирает mpv из исходников — это может занять больше часа."
      printf 'Всё равно ставить через Homebrew? [y/N] '
      read -r ans
      case "$ans" in [yYдД]*) try_brew ;; *) false ;; esac
    else
      false
    fi
  }
fi

RES=$?
[ "$DRY" = 1 ] && exit "$RES"
if have_mpv; then
  exit 0
fi
say "mpv не установлен. Можно поставить вручную (https://mpv.io/installation/) или играть через VLC: vktui engine vlc"
exit 1
