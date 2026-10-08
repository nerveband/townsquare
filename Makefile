# Common tasks. See AGENTS.md for the rules behind them.
.PHONY: check build ui spec test deploy release verify-release package dev

check:            ## everything CI would run
	scripts/check.sh

build:            ## bin/townsquare with version info
	scripts/build.sh

ui:               ## rebuild web/dist from web/ui
	cd web/ui && npm run build

spec:             ## regenerate internal/server/openapi.json
	python3 tools/gen_openapi.py

test:
	go test ./...

dev:              ## UI dev server with hot reload, proxying /api to a running townsquare on :8890
	cd web/ui && npm run dev

deploy:           ## install the released Mac app on the production host: make deploy [V=v0.6.2]
	scripts/deploy.sh $(V)

release:          ## make release V=v0.6.0 [DRY=--dry-run]  (see docs/releasing.md)
	scripts/release.sh $(V) $(DRY)

verify-release:   ## make verify-release V=v0.6.0: check a published release like installs do
	scripts/verify-release.sh $(V)

package:          ## make package V=v0.6.0: build every download into dist/ (no publishing)
	scripts/package.sh $(V)
