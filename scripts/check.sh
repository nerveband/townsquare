#!/usr/bin/env bash
# Everything that must pass before a commit is pushed or a release is cut.
set -euo pipefail
cd "$(dirname "$0")/.."
fail() { echo "✗ $*" >&2; exit 1; }
step() { echo "• $*"; }

step "gofmt"
[ -z "$(gofmt -l cmd internal web tools *.go 2>/dev/null)" ] || fail "gofmt needed: $(gofmt -l cmd internal web tools *.go)"

step "go vet (this system, then every release platform)"
go vet ./...
for t in windows/amd64 linux/amd64 linux/arm64 linux/arm darwin/amd64; do
  GOOS="${t%/*}" GOARCH="${t#*/}" CGO_ENABLED=0 go vet ./... || fail "go vet failed for $t"
done

step "contract and CLI docs are generated and current"
python3 tools/gen_openapi.py >/dev/null
go run ./tools/clidoc >/dev/null
git diff --quiet -- internal/contract/openapi.json docs/cli.md || fail "openapi.json or docs/cli.md changed after regenerating; commit them (make spec)"

step "UI builds and web/dist is current"
( cd web/ui && { [ -d node_modules ] || npm ci --silent; } && npm run build --silent >/dev/null 2>&1 ) || fail "UI build failed (cd web/ui && npm run build)"
git diff --quiet -- web/dist || fail "web/dist changed after building; commit it"
[ -z "$(git ls-files --others --exclude-standard web/dist)" ] || fail "web/dist has new files; commit them"

step "go test"
go test ./... >/dev/null

step "no em dashes in source and docs"
if git grep -I -n $'\u2014' -- ':!web/dist' ':!*.lock' ':!package-lock.json' ':!go.sum' >/dev/null; then
  git grep -I -n $'\u2014' -- ':!web/dist' ':!package-lock.json' ':!go.sum' | head -5
  fail "replace em dashes with commas, colons or parentheses"
fi

step "every API route is in the web app too (parity)"
python3 tools/parity.py || fail "see above: add the missing route to Handler() in server.go, or list it in tools/parity.py with a reason"

echo "✓ all checks passed"
