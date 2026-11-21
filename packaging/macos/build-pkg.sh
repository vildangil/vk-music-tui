#!/bin/bash
# Builds dist/vk-music-tui-<version>.pkg on macOS:
#   /usr/local/bin/vktui                  universal (Intel + Apple Silicon) binary
#   /Applications/VK Music TUI.app        opens Terminal with the player
# The postinstall script installs mpv with Homebrew when it's missing.
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
mkdir -p "$PAYLOAD/usr/local/bin" "$PAYLOAD/Applications"
lipo -create -output "$PAYLOAD/usr/local/bin/vktui" "$WORK/vktui-arm64" "$WORK/vktui-amd64"
chmod 755 "$PAYLOAD/usr/local/bin/vktui"

# A tiny AppleScript app so the player can be started from Launchpad / Dock.
cat > "$WORK/launcher.applescript" <<'EOF'
tell application "Terminal"
	activate
	do script "clear; /usr/local/bin/vktui; exit"
end tell
EOF
osacompile -o "$PAYLOAD/Applications/VK Music TUI.app" "$WORK/launcher.applescript"
if [ -f "$ROOT/packaging/icons/vk-music-tui.icns" ]; then
  cp "$ROOT/packaging/icons/vk-music-tui.icns" "$PAYLOAD/Applications/VK Music TUI.app/Contents/Resources/applet.icns"
fi

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

cat > "$WORK/distribution.xml" <<EOF
<?xml version="1.0" encoding="utf-8"?>
<installer-gui-script minSpecVersion="2">
  <title>VK Music TUI $VERSION</title>
  <welcome mime-type="text/plain"><![CDATA[Установщик VK Music TUI — лёгкого консольного плеера VK Музыки.

Будут установлены:
• команда vktui (/usr/local/bin)
• приложение «VK Music TUI» в Программах (открывает плеер в Терминале)

Для звука нужен mpv: если он не установлен, установщик поставит его через Homebrew (если Homebrew есть). Иначе выполни: brew install mpv]]></welcome>
  <options customize="never" require-scripts="false" hostArchitectures="arm64,x86_64"/>
  <choices-outline><line choice="default"><line choice="app"/></line></choices-outline>
  <choice id="default"/>
  <choice id="app" visible="false"><pkg-ref id="io.github.vildangil.vk-music-tui"/></choice>
  <pkg-ref id="io.github.vildangil.vk-music-tui" version="$VERSION" onConclusion="none">component.pkg</pkg-ref>
</installer-gui-script>
EOF

productbuild --distribution "$WORK/distribution.xml" --package-path "$WORK" "$ROOT/dist/vk-music-tui-$VERSION.pkg"
echo "built dist/vk-music-tui-$VERSION.pkg"
