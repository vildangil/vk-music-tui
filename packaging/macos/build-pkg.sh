#!/bin/bash
# Builds dist/vk-music-tui-<version>.pkg on macOS:
#   /usr/local/bin/vktui                  universal (Intel + Apple Silicon) binary
#   /Applications/VK Music TUI.app        opens Terminal with the player
#   /usr/local/share/vk-music-tui/install-deps.sh
# The postinstall script installs mpv when it's missing (Homebrew, MacPorts or
# the official mpv.app build), see install-deps.sh.
set -euo pipefail
VERSION="${1:-0.0.0}"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$ROOT/dist"

LDFLAGS="-s -w -X main.version=$VERSION"
( cd "$ROOT" && CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "$LDFLAGS" -o "$WORK/vktui-arm64" . )
( cd "$ROOT" && CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "$LDFLAGS" -o "$WORK/vktui-amd64" . )

PAYLOAD="$WORK/payload"
mkdir -p "$PAYLOAD/usr/local/bin" "$PAYLOAD/Applications" "$PAYLOAD/usr/local/share/vk-music-tui"
lipo -create -output "$PAYLOAD/usr/local/bin/vktui" "$WORK/vktui-arm64" "$WORK/vktui-amd64"
chmod 755 "$PAYLOAD/usr/local/bin/vktui"
codesign --force --sign - "$PAYLOAD/usr/local/bin/vktui"

cp "$ROOT/packaging/macos/install-deps.sh" "$PAYLOAD/usr/local/share/vk-music-tui/install-deps.sh"
chmod 755 "$PAYLOAD/usr/local/share/vk-music-tui/install-deps.sh"

# "VK Music TUI.app" for Launchpad / Dock / Finder. A plain bundle whose
# executable is a shell script that opens a .command file in Terminal:
#  - no AppleScript, so no "allow to control Terminal" prompt;
#  - the icon is a normal CFBundleIconFile (osacompile applets use Assets.car,
#    which ignored our .icns, and swapping their icon broke the code signature,
#    so the app didn't start from Launchpad).
bash "$ROOT/packaging/macos/make-app.sh" "$PAYLOAD/Applications/VK Music TUI.app" "$VERSION"

mkdir -p "$WORK/scripts"
cp "$ROOT/packaging/macos/postinstall" "$WORK/scripts/postinstall"
chmod 755 "$WORK/scripts/postinstall"

pkgbuild \
  --root "$PAYLOAD" \
  --scripts "$WORK/scripts" \
  --identifier "io.github.vildangil.vk-music-tui" \
  --version "$VERSION" \
  --install-location / \
  "$WORK/component.pkg"

cat > "$WORK/distribution.xml" <<XML
<?xml version="1.0" encoding="utf-8"?>
<installer-gui-script minSpecVersion="2">
  <title>VK Music TUI $VERSION</title>
  <welcome mime-type="text/plain"><![CDATA[Установщик VK Music TUI — лёгкого консольного плеера VK Музыки.

Будут установлены:
• команда vktui (/usr/local/bin)
• приложение «VK Music TUI» в Программах (открывает плеер в Терминале)

Для звука нужен mpv. Если его нет, установщик поставит его сам (нужен интернет, ~25–50 МБ):
• Apple Silicon — через Homebrew, иначе готовая сборка mpv.app (MacPorts — в последнюю очередь);
• Intel — сразу готовая сборка mpv.app (с github.com/mpv-player или со страницы сборок, на которую ссылается mpv.io): Homebrew на Intel собирает mpv из исходников, это очень долго.
Ход установки mpv пишется в /tmp/vk-music-tui-deps.log

Другие движки (VLC, FFmpeg, MPlayer) тоже поддерживаются: vktui engines.
Если что-то не встало — в Терминале: vktui deps]]></welcome>
  <options customize="never" require-scripts="false" hostArchitectures="arm64,x86_64"/>
  <choices-outline><line choice="default"><line choice="app"/></line></choices-outline>
  <choice id="default"/>
  <choice id="app" visible="false"><pkg-ref id="io.github.vildangil.vk-music-tui"/></choice>
  <pkg-ref id="io.github.vildangil.vk-music-tui" version="$VERSION" onConclusion="none">component.pkg</pkg-ref>
</installer-gui-script>
XML

productbuild --distribution "$WORK/distribution.xml" --package-path "$WORK" "$ROOT/dist/vk-music-tui-$VERSION.pkg"
echo "built dist/vk-music-tui-$VERSION.pkg"
