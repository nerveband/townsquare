#!/usr/bin/env bash
# Check a published release the way installs see it. Usage: scripts/verify-release.sh vX.Y.Z
#  1. every download is attached
#  2. releases/latest/download/latest.json is this version and matches dist/latest.json
#  3. the previous release, as installed, finds this update, verifies it, and runs it
set -euo pipefail
cd "$(dirname "$0")/.."
V="${1:?usage: scripts/verify-release.sh vX.Y.Z}"
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
fail() { echo "✗ $*"; exit 1; }

want=(latest.json latest.json.sig SHA256SUMS "Townsquare-$V-mac.dmg" "townsquare-$V-windows-amd64.exe"
  "townsquare_${V#v}_amd64.deb" "townsquare_${V#v}_arm64.deb" "townsquare_${V#v}_armhf.deb")
for p in darwin-arm64 darwin-amd64 linux-amd64 linux-arm64 linux-armv7; do want+=("townsquare-$V-$p"); done
have="$(gh release view "$V" --json assets --jq '.assets[].name')"
for a in "${want[@]}"; do grep -qxF "$a" <<<"$have" || fail "missing asset $a"; done
echo "✓ ${#want[@]} downloads attached"

curl -fsSL -o "$TMP/latest.json" https://github.com/nerveband/townsquare/releases/latest/download/latest.json
grep -q "\"version\": \"$V\"" "$TMP/latest.json" || fail "latest.json is not $V (is the release marked latest?)"
[ ! -f dist/latest.json ] || cmp -s dist/latest.json "$TMP/latest.json" || echo "! latest.json differs from dist/ (fine if scripts/telegram-app.sh ran since)"
echo "✓ latest.json is $V"

case "$(uname -s)-$(uname -m)" in Darwin-arm64) P=darwin-arm64 ;; Darwin-x86_64) P=darwin-amd64 ;; Linux-x86_64) P=linux-amd64 ;; Linux-aarch64) P=linux-arm64 ;; *) P="" ;; esac
PREV="$(gh release list --limit 20 --json tagName --jq '.[].tagName' | grep -vxF "$V" | head -1 || true)"
if [ -z "$P" ] || [ -z "$PREV" ]; then echo "- skipped the update test (no earlier release or unknown platform)"; exit 0; fi
gh release download "$PREV" --pattern "townsquare-$PREV-$P" --dir "$TMP" >/dev/null
chmod +x "$TMP/townsquare-$PREV-$P"
env -u TOWNSQUARE_UPDATE_URL "$TMP/townsquare-$PREV-$P" --data "$TMP/data" update >/dev/null || fail "$PREV couldn't download $V"
got="$(env -u TOWNSQUARE_UPDATE_URL "$TMP/townsquare-$PREV-$P" --data "$TMP/data" version)"   # text or JSON, by version
case "$got" in *"$V"*) ;; *) fail "$PREV installed, then ran something else: $got" ;; esac
echo "✓ $PREV updates itself to $V (signature, checksum and hand-off)"
