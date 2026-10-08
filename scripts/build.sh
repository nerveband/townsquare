#!/usr/bin/env bash
# Build bin/townsquare with version info. Usage: scripts/build.sh [version] [GOOS/GOARCH] [out]
set -euo pipefail
cd "$(dirname "$0")/.."
VERSION="${1:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
TARGET="${2:-}"
OUT="${3:-bin/townsquare}"
COMMIT="$(git rev-parse --short HEAD)"
DATE="$(date -u +%Y-%m-%d)"
PKG=github.com/nerveband/townsquare/internal/version
LDFLAGS="-s -w -X $PKG.Version=$VERSION -X $PKG.Commit=$COMMIT -X $PKG.Date=$DATE"
# Release builds bake in the shared Telegram app id ("ID:HASH", see internal/tg/app.go).
if [ -n "${TOWNSQUARE_TG_APP:-}" ]; then
  LDFLAGS="$LDFLAGS -X github.com/nerveband/townsquare/internal/tg.builtinApp=$TOWNSQUARE_TG_APP"
fi
if [ -n "$TARGET" ]; then
  export GOOS="${TARGET%/*}" GOARCH="${TARGET#*/}" CGO_ENABLED=0
fi
go build -trimpath -ldflags "$LDFLAGS" -o "$OUT" ./cmd/townsquare
echo "built $OUT ($VERSION${TARGET:+, $TARGET})"
