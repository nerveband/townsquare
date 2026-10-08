#!/usr/bin/env bash
# Cut a release: scripts/release.sh v0.6.0 [--dry-run]
#  1. clean tree on master, in sync with origin
#  2. CHANGELOG.md has a "## [v0.6.0]" section (move items out of Unreleased first)
#  3. all checks pass
#  4. build darwin/arm64 + linux/amd64 binaries with version info
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

NOTES="$(awk -v v="$V" '$0 ~ "^## \\[" v "\\]" {on=1; next} on && /^## \[/ {exit} on {print}' CHANGELOG.md)"
[ -n "$(echo "$NOTES" | tr -d '[:space:]')" ] || { echo "CHANGELOG.md needs a non-empty \"## [$V]\" section"; exit 1; }

scripts/check.sh

rm -rf dist && mkdir -p dist
for t in darwin/arm64 linux/amd64; do
  scripts/build.sh "$V" "$t" "dist/townsquare-$V-${t%/*}-${t#*/}"
done
( cd dist && shasum -a 256 townsquare-* > SHA256SUMS )

if [ "$DRY" = "--dry-run" ]; then
  echo "--- dry run: would tag $V and publish these notes:"; echo "$NOTES"; ls -la dist; exit 0
fi
git tag -a "$V" -m "Townsquare $V"
git push -q origin "$V"
gh release create "$V" dist/* --title "Townsquare $V" --notes "$NOTES"
echo "✓ released $V"
