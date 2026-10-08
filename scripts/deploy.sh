#!/usr/bin/env bash
# Install a released Mac app on the production host, exactly what users download
# (dogfooding): scripts/deploy.sh [vX.Y.Z]   (default: the latest release)
#
#  1. refuses if a post is due within 15 minutes (FORCE=1 overrides)
#  2. backs up the database
#  3. downloads Townsquare-vX.Y.Z-mac.dmg, checks it against SHA256SUMS
#  4. installs /Applications/Townsquare.app, saves the server address settings
#     (TOWNSQUARE_LISTEN / TOWNSQUARE_TAILSCALE from .deploy.env) into config.json
#  5. points the launchd service (start at login, restart if it stops) at the app
#  6. removes other copies (old builds, downloaded updates) and checks the version
#
# After this, the app keeps itself up to date from new releases. Unreleased code
# never runs in production: test it with `serve --demo` or a local data folder.
set -euo pipefail
cd "$(dirname "$0")/.."
[ -f .deploy.env ] && . ./.deploy.env
if [ -n "${TOWNSQUARE_DEPLOY_HOST:-}" ] && [ "$(hostname -s)" != "$TOWNSQUARE_DEPLOY_HOST" ]; then
  echo "deploy.sh runs on $TOWNSQUARE_DEPLOY_HOST only (this is $(hostname -s))"; exit 1
fi
[ "$(uname -s)" = Darwin ] || { echo "deploy.sh installs the Mac app"; exit 1; }
DATA="${TOWNSQUARE_DATA:-$HOME/.townsquare}"
LISTEN="${TOWNSQUARE_LISTEN:-127.0.0.1:8890}"
TSNAME="${TOWNSQUARE_TAILSCALE:-}"
APP=/Applications/Townsquare.app
BIN="$APP/Contents/MacOS/townsquare-server"
LABEL=com.townsquare.server
DOMAIN="gui/$(id -u)"
V="${1:-$(gh release view --json tagName --jq .tagName)}"

# 1. Never restart right around a send. (Exit code 3 = something is due.)
for b in "$BIN" bin/townsquare; do
  [ -x "$b" ] || continue
  code=0; "$b" --data "$DATA" due --within 15m >/dev/null 2>&1 || code=$?
  if [ "$code" = 3 ] && [ -z "${FORCE:-}" ]; then
    "$b" --data "$DATA" due --within 15m || true
    echo "a post is due within 15 minutes; deploy after it goes out (or FORCE=1)"; exit 1
  fi
  break
done

# 2. Back up the database.
if [ -f "$DATA/app.db" ]; then
  mkdir -p "$DATA/backups"
  B="$DATA/backups/app-$(date +%Y%m%d-%H%M%S).db"
  sqlite3 "$DATA/app.db" ".backup '$B'" && echo "backup: $B"
  ls -1t "$DATA"/backups/app-*.db | tail -n +21 | xargs -r rm -f   # keep the newest 20
fi

# 3. Download and check the release's Mac app.
TMP="$(mktemp -d)"; trap 'hdiutil detach -quiet "$TMP/mnt" 2>/dev/null || true; rm -rf "$TMP"' EXIT
DMG="Townsquare-$V-mac.dmg"
gh release download "$V" --pattern "$DMG" --pattern SHA256SUMS --dir "$TMP"
( cd "$TMP" && grep " $DMG\$" SHA256SUMS | shasum -a 256 -c - >/dev/null ) || { echo "checksum mismatch for $DMG"; exit 1; }
echo "✓ downloaded $DMG (checksum ok)"

# 4. Stop the service, install the app, save settings.
launchctl bootout "$DOMAIN/$LABEL" 2>/dev/null || true
tmux kill-session -t townsquare 2>/dev/null || true
sleep 2
hdiutil attach -quiet -nobrowse -readonly -mountpoint "$TMP/mnt" "$TMP/$DMG"
rm -rf "$APP.new" && cp -R "$TMP/mnt/Townsquare.app" "$APP.new"
rm -rf "$APP" && mv "$APP.new" "$APP"
xattr -dr com.apple.quarantine "$APP" 2>/dev/null || true
"$BIN" --data "$DATA" config set "listen=$LISTEN" "tailscale=$TSNAME" >/dev/null

# 5. The service runs the app's own program (address and tailnet come from config.json).
"$BIN" --data "$DATA" autostart on >/dev/null
launchctl bootstrap "$DOMAIN" "$HOME/Library/LaunchAgents/$LABEL.plist"

# 6. One copy only: no repo builds, no older downloaded updates.
rm -f bin/townsquare bin/townsquare.new
rm -rf "$DATA/bin"

for i in $(seq 1 30); do
  got="$(curl -s -m 3 -D - -o /dev/null "http://$LISTEN/api/auth/status" | tr -d '\r' | awk -F': ' 'tolower($1)=="x-townsquare-version"{print $2}')"
  [ "$got" = "$V" ] && { echo "✓ running $V from $APP (http://$LISTEN${TSNAME:+, tailnet name $TSNAME})"; exit 0; }
  sleep 1
done
echo "✗ $V didn't answer at http://$LISTEN; see $DATA/serve.log"; exit 1
