#!/usr/bin/env bash
# Swap the shared Telegram app id on every install, without a new release.
# Usage: scripts/telegram-app.sh [FILE]   (FILE holds "api_id:api_hash";
#        default ~/.config/townsquare/telegram-app)
#
# It downloads the newest release's latest.json, replaces "telegram", signs it
# with the release key and uploads it back. Installs pick it up at their next
# update check (every 6 hours) and restart into it when no post is due. People
# who set their own app id keep theirs. Also update the default FILE so later
# releases bake in the same id.
set -euo pipefail
cd "$(dirname "$0")/.."
FILE="${1:-$HOME/.config/townsquare/telegram-app}"
KEY="${TOWNSQUARE_SIGNING_KEY:-$HOME/.config/townsquare/release-signing.key}"
[ -f "$FILE" ] && [ -f "$KEY" ] || { echo "need $FILE and $KEY"; exit 1; }
TAG="$(gh release view --json tagName --jq .tagName)"
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
gh release download "$TAG" --pattern latest.json --dir "$TMP"
python3 - "$TMP/latest.json" "$FILE" <<'PY'
import json, sys
m = json.load(open(sys.argv[1]))
i, h = open(sys.argv[2]).read().strip().split(":")
assert i.isdigit() and len(h) == 32, "FILE must be api_id:api_hash"
m["telegram"] = {"api_id": int(i), "api_hash": h}
open(sys.argv[1], "w").write(json.dumps(m, indent=2) + "\n")
PY
go run ./tools/sign sign "$KEY" "$TMP/latest.json"
gh release upload "$TAG" "$TMP/latest.json" "$TMP/latest.json.sig" --clobber
echo "✓ shared Telegram app id updated in $TAG's latest.json"
