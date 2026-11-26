#!/bin/bash
# make-app.sh <path/VK Music TUI.app> [version]
# Builds the launcher bundle that opens vktui in Terminal and signs it ad hoc.
set -euo pipefail
APP="$1"
VERSION="${2:-0.0.0}"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"

cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>VK Music TUI</string>
  <key>CFBundleDisplayName</key><string>VK Music TUI</string>
  <key>CFBundleIdentifier</key><string>io.github.vildangil.vk-music-tui.launcher</string>
  <key>CFBundleVersion</key><string>$VERSION</string>
  <key>CFBundleShortVersionString</key><string>$VERSION</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleExecutable</key><string>vk-music-tui</string>
  <key>CFBundleIconFile</key><string>vk-music-tui</string>
  <key>LSMinimumSystemVersion</key><string>10.15</string>
  <key>LSApplicationCategoryType</key><string>public.app-category.music</string>
  <key>NSHighResolutionCapable</key><true/>
</dict>
</plist>
PLIST
printf 'APPL????' > "$APP/Contents/PkgInfo"

cat > "$APP/Contents/MacOS/vk-music-tui" <<'SH'
#!/bin/sh
# Open the player in Terminal (a .command file is a terminal script).
DIR="$(cd "$(dirname "$0")/../Resources" && pwd)"
exec /usr/bin/open -a Terminal "$DIR/vktui.command"
SH
chmod 755 "$APP/Contents/MacOS/vk-music-tui"

cat > "$APP/Contents/Resources/vktui.command" <<'SH'
#!/bin/zsh
# Started by "VK Music TUI.app" in a Terminal window.
export PATH="/usr/local/bin:/opt/homebrew/bin:/opt/local/bin:$HOME/.local/bin:$PATH"
printf '\033]0;VK Music\007'
clear
VKTUI=/usr/local/bin/vktui
[ -x "$VKTUI" ] || VKTUI="$(command -v vktui)"
if [ -z "$VKTUI" ]; then
  echo "vktui не найден — переустанови VK Music TUI."
elif "$VKTUI"; then
  exit 0
fi
echo
read -k 1 "?Нажми любую клавишу, чтобы закрыть окно…"
exit 0
SH
chmod 755 "$APP/Contents/Resources/vktui.command"

if [ -f "$ROOT/packaging/icons/vk-music-tui.icns" ]; then
  cp "$ROOT/packaging/icons/vk-music-tui.icns" "$APP/Contents/Resources/vk-music-tui.icns"
fi
# the mpv installer also ships inside the app, for `vktui deps`
cp "$ROOT/packaging/macos/install-deps.sh" "$APP/Contents/Resources/install-deps.sh"
chmod 755 "$APP/Contents/Resources/install-deps.sh"

# ad-hoc signature: Apple Silicon refuses to start bundles with a broken one
codesign --force --deep --sign - "$APP"
codesign --verify --deep --strict "$APP"
