#!/usr/bin/env bash
# Quick check while working: this system only, and only the Go packages that
# changed since origin/master. `make check` is still the full gate before a release.
set -euo pipefail
cd "$(dirname "$0")/.."
fail() { echo "✗ $*" >&2; exit 1; }
step() { echo "• $*"; }
changed="$( { git diff --name-only origin/master...HEAD; git diff --name-only HEAD; git ls-files --others --exclude-standard; } | sort -u)"

step "gofmt"
[ -z "$(gofmt -l cmd internal web tools *.go 2>/dev/null)" ] || fail "gofmt needed: $(gofmt -l cmd internal web tools *.go)"

pkgs="$(echo "$changed" | grep '\.go$' | xargs -n1 dirname 2>/dev/null | sort -u | sed 's|^|./|' | while read -r d; do [ -d "$d" ] && echo "$d"; done || true)"
if [ -n "$pkgs" ]; then
  step "go vet and go test: $(echo $pkgs)"
  go vet $pkgs
  go test $pkgs >/dev/null
fi

if echo "$changed" | grep -q '^tools/gen_openapi.py$'; then
  step "contract and CLI docs are current"
  python3 tools/gen_openapi.py >/dev/null && go run ./tools/clidoc >/dev/null
  git diff --quiet -- internal/contract/openapi.json docs/cli.md || echo "  (regenerated: commit internal/contract/openapi.json and docs/cli.md)"
fi
if echo "$changed" | grep -q '^web/ui/'; then
  step "UI builds"
  ( cd web/ui && npm run build --silent >/dev/null 2>&1 ) || fail "UI build failed (cd web/ui && npm run build)"
fi
echo "✓ quick checks passed (run make check before releasing)"
