#!/usr/bin/env bash
# Cut a release: scripts/release.sh v0.6.0 [--dry-run]
#  1. clean tree on master, in sync with origin
#  2. CHANGELOG.md has a "## [v0.6.0]" section (move items out of Unreleased first)
#  3. all checks pass
#  4. build every download (scripts/package.sh): binaries, Mac dmg, .deb, signed latest.json
#  5. tag, push the tag, create the GitHub release with the changelog notes
set -euo pipefail
cd "$(dirname "$0")/.."
V="${1:-}"; DRY="${2:-}"
[[ "$V" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[a-z0-9.]+)?$ ]] || { echo "usage: scripts/release.sh vX.Y.Z [--dry-run]"; exit 1; }
[ "$(git rev-parse --abbrev-ref HEAD)" = "master" ] || { echo "release from master"; exit 1; }
[ -z "$(git status --porcelain)" ] || { echo "working tree not clean"; exit 1; }
git fetch -q origin
[ "$(git rev-parse HEAD)" = "$(git rev-parse origin/master)" ] || { echo "push or pull first: HEAD != origin/master"; exit 1; }
git rev-parse -q --verify "refs/tags/$V" >/dev/null && { echo "tag $V exists"; exit 1; }
# Versions only go up: installs never move backwards.
LAST="$(gh release list --limit 1 --exclude-drafts --exclude-pre-releases --json tagName --jq '.[0].tagName' 2>/dev/null || true)"
if [ -n "$LAST" ]; then
  newest="$(printf '%s\n%s\n' "${LAST#v}" "${V#v}" | sort -V | tail -1)"
  [ "$newest" = "${V#v}" ] && [ "$LAST" != "$V" ] || { echo "$V must be newer than the latest release $LAST"; exit 1; }
fi
for f in "${TOWNSQUARE_SIGNING_KEY:-$HOME/.config/townsquare/release-signing.key}" "${TOWNSQUARE_TG_APP_FILE:-$HOME/.config/townsquare/telegram-app}"; do
  [ -f "$f" ] || { echo "missing $f (restore it from 1Password, see docs/releasing.md)"; exit 1; }
done

NOTES="$(awk -v v="$V" '$0 ~ "^## \\[" v "\\]" {on=1; next} on && /^## \[/ {exit} on {print}' CHANGELOG.md)"
[ -n "$(echo "$NOTES" | tr -d '[:space:]')" ] || { echo "CHANGELOG.md needs a non-empty \"## [$V]\" section"; exit 1; }
UNREL="$(awk '/^## \[Unreleased\]/ {on=1; next} on && /^## \[/ {exit} on {print}' CHANGELOG.md | tr -d '[:space:]')"
[ -z "$UNREL" ] || { echo "CHANGELOG.md still has items under [Unreleased]; move them into [$V] or leave them out on purpose with UNRELEASED_OK=1"; [ -n "${UNRELEASED_OK:-}" ] || exit 1; }
FIRST="$(grep -m1 -oE '^## \[v[^]]+\]' CHANGELOG.md || true)"
[ "$FIRST" = "## [$V]" ] || { echo "the newest section in CHANGELOG.md must be [$V] (found $FIRST)"; exit 1; }

scripts/check.sh

scripts/package.sh "$V"

if [ "$DRY" = "--dry-run" ]; then
  echo "--- dry run: would tag $V and publish these notes:"; echo "$NOTES"; ls -la dist; exit 0
fi
git tag -a "$V" -m "Townsquare $V"
git push -q origin "$V"
gh release create "$V" dist/* --title "Townsquare $V" --notes "$NOTES" --latest
echo "✓ released $V"
sleep 5
scripts/verify-release.sh "$V"
