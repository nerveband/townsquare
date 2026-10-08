#!/usr/bin/env bash
# Build every download for a release into dist/. Runs on macOS (it needs lipo,
# codesign and hdiutil for the Mac app). Usage: scripts/package.sh v0.6.0
#
#   townsquare-V-<os>-<arch>[.exe]   plain binaries (the self-updater downloads these)
#   Townsquare-V-mac.dmg             Mac app (Apple silicon and Intel)
#   townsquare_X.Y.Z_<arch>.deb      Debian, Ubuntu, Raspberry Pi OS (amd64, arm64, armhf)
#   latest.json + latest.json.sig    signed update manifest (with changelog and Telegram app id)
#   SHA256SUMS
#
# The signing key lives outside the repo: TOWNSQUARE_SIGNING_KEY (default
# ~/.config/townsquare/release-signing.key). See docs/releasing.md.
set -euo pipefail
cd "$(dirname "$0")/.."
V="${1:?usage: scripts/package.sh vX.Y.Z}"
KEY="${TOWNSQUARE_SIGNING_KEY:-$HOME/.config/townsquare/release-signing.key}"
[ -f "$KEY" ] || { echo "signing key not found: $KEY"; exit 1; }
# Shared Telegram app id ("ID:HASH"), baked into binaries and sent in latest.json.
TGAPP="${TOWNSQUARE_TG_APP_FILE:-$HOME/.config/townsquare/telegram-app}"
[ -f "$TGAPP" ] || { echo "shared Telegram app id not found: $TGAPP"; exit 1; }
export TOWNSQUARE_TG_APP="$(tr -d '[:space:]' < "$TGAPP")"
[ "$(uname -s)" = Darwin ] || { echo "package.sh runs on macOS"; exit 1; }

rm -rf dist && mkdir -p dist
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT

# Plain binaries.
for t in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 linux/arm windows/amd64; do
  os="${t%/*}"; arch="${t#*/}"; [ "$arch" = arm ] && arch=armv7
  ext=""; [ "$os" = windows ] && ext=".exe"
  if [ "$t" = linux/arm ]; then export GOARM=7; else unset GOARM; fi
  scripts/build.sh "$V" "$t" "dist/townsquare-$V-$os-$arch$ext" >/dev/null
done
unset GOARM
echo "✓ binaries"

# Mac app: a tiny launcher script starts the server in the background and exits,
# so opening the app again just opens the browser.
APP="$TMP/Townsquare.app"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
lipo -create -output "$APP/Contents/MacOS/townsquare-server" "dist/townsquare-$V-darwin-arm64" "dist/townsquare-$V-darwin-amd64"
cat > "$APP/Contents/MacOS/Townsquare" <<'SH'
#!/bin/sh
# Start Townsquare (or, if it's already running, open it in the browser).
DIR="$(cd "$(dirname "$0")" && pwd)"
mkdir -p "$HOME/.townsquare"
nohup "$DIR/townsquare-server" >>"$HOME/.townsquare/app.log" 2>&1 &
SH
chmod 755 "$APP/Contents/MacOS/Townsquare"
cp assets/brand/Townsquare.icns "$APP/Contents/Resources/Townsquare.icns"
cat > "$APP/Contents/Info.plist" <<PL
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleName</key><string>Townsquare</string>
  <key>CFBundleDisplayName</key><string>Townsquare</string>
  <key>CFBundleIdentifier</key><string>com.townsquare.app</string>
  <key>CFBundleExecutable</key><string>Townsquare</string>
  <key>CFBundleIconFile</key><string>Townsquare</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>${V#v}</string>
  <key>CFBundleVersion</key><string>${V#v}</string>
  <key>LSMinimumSystemVersion</key><string>11.0</string>
  <key>LSUIElement</key><true/>
  <key>NSHumanReadableCopyright</key><string>AGPL-3.0</string>
</dict></plist>
PL
plutil -lint "$APP/Contents/Info.plist" >/dev/null
codesign --force --deep --sign - "$APP" || { echo "✗ codesign failed"; exit 1; }
mkdir -p "$TMP/dmg" && cp -R "$APP" "$TMP/dmg/" && ln -s /Applications "$TMP/dmg/Applications"
# hdiutil sometimes fails with "resource busy" right after another disk image was used; retry.
for try in 1 2 3; do
  hdiutil create -quiet -volname "Townsquare" -srcfolder "$TMP/dmg" -ov -format UDZO "dist/Townsquare-$V-mac.dmg" && break
  [ "$try" = 3 ] && { echo "✗ hdiutil create failed"; exit 1; }
  echo "hdiutil create failed; retrying ($try)"; sleep 5
done
echo "✓ dmg"

# Debian packages.
for pair in amd64:amd64 arm64:arm64 armv7:armhf; do
  python3 tools/mkdeb.py "$V" "${pair#*:}" "dist/townsquare-$V-linux-${pair%:*}" "dist/townsquare_${V#v}_${pair#*:}.deb" >/dev/null
done
echo "✓ deb"

# Signed update manifest (binaries, changelog, shared Telegram app id).
go run ./tools/manifest "$V" dist "$TGAPP" >/dev/null
go run ./tools/sign sign "$KEY" dist/latest.json
( cd dist && shasum -a 256 townsquare-* Townsquare-* *.deb latest.json > SHA256SUMS )
echo "✓ signed latest.json"
ls -la dist
