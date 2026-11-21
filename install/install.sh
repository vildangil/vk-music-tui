#!/bin/sh
# vktui installer for Linux, macOS, FreeBSD and Termux.
#   curl -fsSL https://raw.githubusercontent.com/vildangil/vk-music-tui/main/install/install.sh | sh
set -eu

REPO="vildangil/vk-music-tui"

say()  { printf '\033[36m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[33m!!\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[31mxx\033[0m %s\n' "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux|darwin|freebsd) ;;
  *) die "Неподдерживаемая ОС: $os" ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  armv7*|armv8l|armhf) arch=armv7 ;;
  *) die "Неподдерживаемая архитектура: $(uname -m)" ;;
esac
if [ "$os" = darwin ] && [ "$arch" = armv7 ]; then die "armv7 на macOS не бывает"; fi

termux=0
case "${PREFIX:-}" in *com.termux*) termux=1 ;; esac

SUDO=""
if [ "$(id -u)" -ne 0 ] && [ "$termux" -eq 0 ] && have sudo; then SUDO="sudo"; fi

# where to put the binary
if [ -n "${VKTUI_BIN_DIR:-}" ]; then
  bindir="$VKTUI_BIN_DIR"
elif [ "$termux" -eq 1 ]; then
  bindir="$PREFIX/bin"
elif [ "$(id -u)" -eq 0 ]; then
  bindir="/usr/local/bin"
else
  bindir="$HOME/.local/bin"
fi
mkdir -p "$bindir"

url="${VKTUI_URL:-https://github.com/$REPO/releases/latest/download/vk-music-tui_${os}_${arch}.tar.gz}"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

say "Скачиваю $url"
if have curl; then curl -fsSL "$url" -o "$tmp/vktui.tgz"
elif have wget; then wget -qO "$tmp/vktui.tgz" "$url"
else die "Нужен curl или wget"; fi
tar -xzf "$tmp/vktui.tgz" -C "$tmp"
install -m 0755 "$tmp/vktui" "$bindir/vktui" 2>/dev/null || { cp "$tmp/vktui" "$bindir/vktui"; chmod 0755 "$bindir/vktui"; }
say "vktui установлен в $bindir/vktui"

# menu entry + icon for desktop Linux (per user, no root needed)
if [ "$os" = linux ] && [ "$termux" -eq 0 ] && [ -f "$tmp/packaging/linux/vk-music-tui.desktop" ]; then
  if [ "$(id -u)" -eq 0 ]; then share=/usr/local/share; else share="$HOME/.local/share"; fi
  mkdir -p "$share/applications" "$share/icons/hicolor/256x256/apps"
  sed "s#^Exec=vktui#Exec=$bindir/vktui#" "$tmp/packaging/linux/vk-music-tui.desktop" > "$share/applications/vk-music-tui.desktop"
  cp "$tmp/packaging/icons/vk-music-tui-256.png" "$share/icons/hicolor/256x256/apps/vk-music-tui.png" 2>/dev/null || true
  say "Ярлык добавлен в меню приложений"
fi

# mpv — the playback engine
if ! have mpv; then
  say "Ставлю mpv (движок воспроизведения)"
  mpv_ok=1
  if [ "$termux" -eq 1 ]; then pkg install -y mpv || mpv_ok=0
  elif [ "$os" = darwin ]; then
    if have brew; then brew install mpv || mpv_ok=0; else mpv_ok=0; fi
  elif [ "$os" = freebsd ]; then $SUDO pkg install -y mpv || mpv_ok=0
  elif have apt-get; then { $SUDO apt-get update -qq && $SUDO apt-get install -y mpv; } || mpv_ok=0
  elif have dnf; then $SUDO dnf install -y mpv || mpv_ok=0
  elif have pacman; then $SUDO pacman -S --noconfirm --needed mpv || mpv_ok=0
  elif have zypper; then $SUDO zypper --non-interactive install mpv || mpv_ok=0
  elif have apk; then $SUDO apk add mpv || mpv_ok=0
  elif have xbps-install; then $SUDO xbps-install -y mpv || mpv_ok=0
  elif have emerge; then $SUDO emerge media-video/mpv || mpv_ok=0
  elif have nix-env; then nix-env -iA nixpkgs.mpv || mpv_ok=0
  else mpv_ok=0; fi
  if [ "$mpv_ok" -eq 0 ]; then
    warn "mpv не установился автоматически — поставь его сам (Arch: sudo pacman -Syu mpv, macOS: brew install mpv)"
  fi
fi

case ":$PATH:" in
  *":$bindir:"*) ;;
  *) warn "$bindir нет в PATH. Добавь в ~/.bashrc или ~/.zshrc:  export PATH=\"$bindir:\$PATH\"" ;;
esac

printf '\n\033[32mГотово!\033[0m Запускай: vktui\n'
