#!/usr/bin/env bash
# Deploy the current checkout on this host: back up the database,
# build, restart the app (the launchd service if installed, see scripts/service.sh, else the
# tmux session "townsquare"), and verify the new version answers.
set -euo pipefail
cd "$(dirname "$0")/.."
# Host-specific settings live in an untracked .deploy.env (TOWNSQUARE_LISTEN, TOWNSQUARE_TAILSCALE,
# TOWNSQUARE_DEPLOY_HOST to refuse running anywhere else).
[ -f .deploy.env ] && . ./.deploy.env
if [ -n "${TOWNSQUARE_DEPLOY_HOST:-}" ] && [ "$(hostname -s)" != "$TOWNSQUARE_DEPLOY_HOST" ]; then
  echo "deploy.sh runs on $TOWNSQUARE_DEPLOY_HOST only (this is $(hostname -s))"; exit 1
fi
DATA="${TOWNSQUARE_DATA:-$HOME/.townsquare}"
LISTEN="${TOWNSQUARE_LISTEN:-127.0.0.1:8890}"
TSNAME="${TOWNSQUARE_TAILSCALE:-townsquare}"
LOG="${TOWNSQUARE_LOG:-$HOME/.townsquare/serve.log}"

# Stop the running app (and the pre-rename "wacal" session) before touching data.
SERVICE=""
[ -f "$HOME/Library/LaunchAgents/com.townsquare.server.plist" ] && SERVICE=1
[ -n "$SERVICE" ] && scripts/service.sh stop
tmux kill-session -t townsquare 2>/dev/null || true
tmux kill-session -t wacal 2>/dev/null || true
sleep 1

# One-time move from the old name: ~/.wacal -> ~/.townsquare (old path stays as a symlink).
if [ -d "$HOME/.wacal" ] && [ ! -L "$HOME/.wacal" ] && [ ! -e "$DATA" ]; then
  mv "$HOME/.wacal" "$DATA" && ln -s "$DATA" "$HOME/.wacal" && echo "moved ~/.wacal to $DATA"
fi

if [ -f "$DATA/app.db" ]; then
  mkdir -p "$DATA/backups"
  B="$DATA/backups/app-$(date +%Y%m%d-%H%M%S).db"
  sqlite3 "$DATA/app.db" ".backup '$B'" && echo "backup: $B"
  ls -1t "$DATA"/backups/app-*.db | tail -n +21 | xargs -r rm -f   # keep the newest 20
fi

scripts/build.sh "${1:-}" "" bin/townsquare.new
mv bin/townsquare.new bin/townsquare
VERSION="$(bin/townsquare version | awk '{print $2}')"

if [ -n "$SERVICE" ]; then
  scripts/service.sh start
else
  tmux new-session -d -s townsquare "cd '$PWD' && ./bin/townsquare --data '$DATA' serve --listen '$LISTEN' --tailscale '$TSNAME' 2>&1 | tee -a '$LOG'"
fi

for i in $(seq 1 20); do
  got="$(curl -s -m 2 "http://$LISTEN/api/v1/" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("app_version",""))' 2>/dev/null || true)"
  if [ "$got" = "$VERSION" ]; then echo "✓ deployed $VERSION (http://$LISTEN${TSNAME:+, tailnet name $TSNAME})"; exit 0; fi
  sleep 1
done
echo "✗ new version did not come up; see $LOG" >&2
exit 1
