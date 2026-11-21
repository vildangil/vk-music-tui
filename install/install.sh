#!/bin/sh
# VK Music TUI installer for any Linux (also macOS, FreeBSD, Termux).
#   curl -fsSL https://raw.githubusercontent.com/vildangil/vk-music-tui/main/install/install.sh | sh
#
# On Debian/Ubuntu, Fedora/openSUSE, Arch and Alpine it installs the native
# package (mpv comes as a dependency); elsewhere it installs the binary.
set -eu

REPO="vildangil/vk-music-tui"
API="https://api.github.com/repos/$REPO/releases/latest"

say()  { printf '\033[36m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[33m!!\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[31mxx\033[0m %s\n' "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

fetch() { # url dest
  if have curl; then curl -fsSL "$1" -o "$2"
  elif have wget; then wget -qO "$2" "$1"
  else die "Нужен curl или wget"; fi
}

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in linux|darwin|freebsd) ;; *) die "Неподдерживаемая ОС: $os" ;; esac
m=$(uname -m)
case "$m" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  armv7*|armv8l|armhf) arch=armv7 ;;
  i386|i686) arch=386 ;;
  *) die "Неподдерживаемая архитектура: $m" ;;
esac

termux=0
case "${PREFIX:-}" in *com.termux*) termux=1 ;; esac
SUDO=""
if [ "$(id -u)" -ne 0 ] && [ "$termux" -eq 0 ]; then
  have sudo && SUDO="sudo" || { have doas && SUDO="doas"; }
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

# ---------- native package ----------
pkg_pattern=""
if [ "$os" = linux ] && [ "$termux" -eq 0 ] && [ -z "${VKTUI_URL:-}" ]; then
  if have apt-get && have dpkg; then
    pkg_pattern="_$(dpkg --print-architecture)\.deb"
  elif have pacman; then
    pkg_pattern="-$(uname -m | sed 's/armv7l/armv7h/')\.pkg\.tar\.zst"
  elif have dnf || have zypper || have yum; then
    pkg_pattern="\.$(uname -m | sed 's/armv7l/armv7hl/;s/i686/i386/')\.rpm"
  elif have apk; then
    pkg_pattern="_$(uname -m | sed 's/i686/x86/;s/armv7l/armv7/')\.apk"
  fi
fi

if [ -n "$pkg_pattern" ]; then
  say "Ищу пакет для этой системы…"
  fetch "$API" "$tmp/release.json" || die "Нет связи с GitHub"
  url=$(grep -o '"browser_download_url": *"[^"]*"' "$tmp/release.json" | sed 's/.*"\(http[^"]*\)"/\1/' | grep -E -e "$pkg_pattern\$" | head -n1 || true)
  if [ -n "$url" ]; then
    file="$tmp/$(basename "$url")"
    say "Скачиваю $(basename "$url")"
    fetch "$url" "$file"
    say "Устанавливаю (mpv поставится как зависимость)"
    if { if have apt-get; then $SUDO apt-get install -y "$file"
         elif have pacman; then $SUDO pacman -U --noconfirm --needed "$file"
         elif have dnf; then $SUDO dnf install -y "$file"
         elif have zypper; then $SUDO zypper --non-interactive install --allow-unsigned-rpm "$file"
         elif have yum; then $SUDO yum install -y "$file"
         elif have apk; then $SUDO apk add --allow-untrusted "$file"
         else false; fi; }; then
      printf '
[32mГотово![0m Запускай: vktui  (или «VK Music TUI» в меню приложений)
'
      exit 0
    fi
    warn "Пакет не установился (возможно, устарели списки пакетов — обнови систему) — ставлю обычную версию"
  else
    warn "Готового пакета не нашлось — ставлю обычную версию"
  fi
fi

# ---------- plain binary (any distro, macOS, FreeBSD, Termux) ----------
if [ -n "${VKTUI_BIN_DIR:-}" ]; then bindir="$VKTUI_BIN_DIR"
elif [ "$termux" -eq 1 ]; then bindir="$PREFIX/bin"
elif [ "$(id -u)" -eq 0 ]; then bindir="/usr/local/bin"
else bindir="$HOME/.local/bin"; fi
mkdir -p "$bindir"

url="${VKTUI_URL:-https://github.com/$REPO/releases/latest/download/vk-music-tui_${os}_${arch}.tar.gz}"
say "Скачиваю $url"
fetch "$url" "$tmp/vktui.tgz"
tar -xzf "$tmp/vktui.tgz" -C "$tmp"
install -m 0755 "$tmp/vktui" "$bindir/vktui" 2>/dev/null || { cp "$tmp/vktui" "$bindir/vktui"; chmod 0755 "$bindir/vktui"; }
say "vktui установлен в $bindir/vktui"

if [ "$os" = linux ] && [ "$termux" -eq 0 ] && [ -f "$tmp/packaging/linux/vk-music-tui.desktop" ]; then
  if [ "$(id -u)" -eq 0 ]; then share=/usr/local/share; else share="$HOME/.local/share"; fi
  mkdir -p "$share/applications" "$share/icons/hicolor/256x256/apps"
  sed "s#^Exec=vktui#Exec=$bindir/vktui#" "$tmp/packaging/linux/vk-music-tui.desktop" > "$share/applications/vk-music-tui.desktop"
  cp "$tmp/packaging/icons/vk-music-tui-256.png" "$share/icons/hicolor/256x256/apps/vk-music-tui.png" 2>/dev/null || true
fi

if ! have mpv; then
  say "Ставлю mpv (движок воспроизведения)"
  ok=1
  if [ "$termux" -eq 1 ]; then pkg install -y mpv || ok=0
  elif [ "$os" = darwin ]; then { have brew && brew install mpv; } || ok=0
  elif [ "$os" = freebsd ]; then $SUDO pkg install -y mpv || ok=0
  elif have apt-get; then { $SUDO apt-get update -qq && $SUDO apt-get install -y mpv; } || ok=0
  elif have dnf; then $SUDO dnf install -y mpv || ok=0
  elif have pacman; then $SUDO pacman -S --noconfirm --needed mpv || ok=0
  elif have zypper; then $SUDO zypper --non-interactive install mpv || ok=0
  elif have apk; then $SUDO apk add mpv || ok=0
  elif have xbps-install; then $SUDO xbps-install -y mpv || ok=0
  elif have emerge; then $SUDO emerge media-video/mpv || ok=0
  elif have nix-env; then nix-env -iA nixpkgs.mpv || ok=0
  else ok=0; fi
  [ "$ok" -eq 1 ] || warn "Не получилось поставить mpv автоматически — установи его сам"
fi

case ":$PATH:" in
  *":$bindir:"*) ;;
  *) warn "$bindir нет в PATH. Добавь в ~/.bashrc или ~/.zshrc:  export PATH=\"$bindir:\$PATH\"" ;;
esac
printf '\n\033[32mГотово!\033[0m Запускай: vktui\n'
