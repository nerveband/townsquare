#!/usr/bin/env bash
# Run Townsquare as a macOS background service (launchd LaunchAgent) that starts when you
# log in and restarts if it stops. Usage: scripts/service.sh install|uninstall|restart|status
# Reads the same settings as deploy.sh (.deploy.env: TOWNSQUARE_LISTEN, TOWNSQUARE_TAILSCALE,
# TOWNSQUARE_DATA, TOWNSQUARE_LOG).
set -euo pipefail
cd "$(dirname "$0")/.."
[ -f .deploy.env ] && . ./.deploy.env
[ "$(uname -s)" = Darwin ] || { echo "service.sh supports macOS (launchd) for now"; exit 1; }

LABEL="com.townsquare.server"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
DOMAIN="gui/$(id -u)"
DATA="${TOWNSQUARE_DATA:-$HOME/.townsquare}"
LISTEN="${TOWNSQUARE_LISTEN:-127.0.0.1:8890}"
TSNAME="${TOWNSQUARE_TAILSCALE:-}"
LOG="${TOWNSQUARE_LOG:-$DATA/serve.log}"
BIN="$PWD/bin/townsquare"

loaded() { launchctl print "$DOMAIN/$LABEL" >/dev/null 2>&1; }

write_plist() {
  local ts_args=""
  [ -n "$TSNAME" ] && ts_args="<string>--tailscale</string><string>$TSNAME</string>"
  mkdir -p "$(dirname "$PLIST")" "$(dirname "$LOG")"
  cat > "$PLIST" <<PL
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>$LABEL</string>
  <key>ProgramArguments</key><array>
    <string>$BIN</string><string>--data</string><string>$DATA</string>
    <string>serve</string><string>--listen</string><string>$LISTEN</string>$ts_args
  </array>
  <key>WorkingDirectory</key><string>$PWD</string>
  <key>EnvironmentVariables</key><dict>
    <key>HOME</key><string>$HOME</string>
    <key>PATH</key><string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin</string>
  </dict>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ThrottleInterval</key><integer>10</integer>
  <key>StandardOutPath</key><string>$LOG</string>
  <key>StandardErrorPath</key><string>$LOG</string>
</dict></plist>
PL
  plutil -lint "$PLIST" >/dev/null
}

case "${1:-status}" in
  install)
    [ -x "$BIN" ] || { echo "build first: make build"; exit 1; }
    tmux kill-session -t townsquare 2>/dev/null || true   # older setups ran in tmux
    loaded && launchctl bootout "$DOMAIN/$LABEL" 2>/dev/null || true
    write_plist
    launchctl bootstrap "$DOMAIN" "$PLIST"
    echo "✓ installed $LABEL: starts at login, restarts if it stops. Log: $LOG"
    ;;
  uninstall)
    loaded && launchctl bootout "$DOMAIN/$LABEL" || true
    rm -f "$PLIST"
    echo "✓ removed $LABEL"
    ;;
  stop)
    loaded && launchctl bootout "$DOMAIN/$LABEL" || true
    ;;
  start)
    [ -f "$PLIST" ] || { echo "not installed: scripts/service.sh install"; exit 1; }
    loaded || launchctl bootstrap "$DOMAIN" "$PLIST"
    ;;
  restart)
    launchctl kickstart -k "$DOMAIN/$LABEL"
    ;;
  status)
    if loaded; then launchctl print "$DOMAIN/$LABEL" | grep -E "state =|pid =|last exit code" | sed 's/^[[:space:]]*//'; else echo "not running as a service"; fi
    ;;
  *) echo "usage: scripts/service.sh install|uninstall|start|stop|restart|status"; exit 1 ;;
esac
