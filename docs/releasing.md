# Releasing and deploying

- Semantic versioning; before 1.0, minor = features, patch = fixes.
- Every user-visible change: a line in `CHANGELOG.md` under `## [Unreleased]`
  (Added / Changed / Deprecated / Removed / Fixed / API changes).
- Release only when the owner asks:
  1. Move Unreleased items into `## [vX.Y.Z] - YYYY-MM-DD`, commit, push.
  2. `make release V=vX.Y.Z DRY=--dry-run`, then `make release V=vX.Y.Z` (checks, runs
     `scripts/package.sh`, tags, creates the GitHub release with the changelog notes). Runs on
     macOS. Builds plain binaries (darwin arm64/amd64, linux amd64/arm64/armv7, windows amd64),
     the Mac `.dmg`, three `.deb` files, `SHA256SUMS`, and the signed `latest.json`.
  3. `make deploy` on the production host: backs up the database, builds, restarts the app and
     verifies the version. It restarts the launchd service if `scripts/service.sh install` was
     run (starts at login, restarts on crash), else the `townsquare` tmux session. Host-specific settings (listen address,
     tailnet name) live in an untracked `.deploy.env` next to the Makefile.
- Push only to `github.com/nerveband/townsquare`.
- **Self-updates.** Installs read `releases/latest/download/latest.json` and accept it only with a
  valid `latest.json.sig` from a key in `internal/update/key.go`. The private key is
  `~/.config/townsquare/release-signing.key` (or `TOWNSQUARE_SIGNING_KEY`), never in the repo.
  Keep a backup in a password manager: without it, existing installs can't verify new releases.
  To rotate: `go run ./tools/sign keygen NEWFILE`, add its public key to `key.go`, release once
  signed with the old key, then sign with the new one. Never mark a broken build as the latest
  release; publish a fixed patch instead (installs only move forward).
- **Shared Telegram app id.** Release builds bake in `~/.config/townsquare/telegram-app`
  (`api_id:api_hash`, mode 600, or `TOWNSQUARE_TG_APP_FILE`) and `latest.json` carries it too.
  Installs use, in order: their own `telegram.app`, the id from the newest signed `latest.json`
  (saved as `telegram.shared.json`), then the built-in one. To swap it everywhere without a new
  release, put the new id in that file and run `scripts/telegram-app.sh`; installs switch at their
  next check and restart when no post is due. Logged-in sessions normally carry over; if Telegram
  rejects them, people scan the QR code again.
- **In-app release notes** come from `CHANGELOG.md`: the binary embeds it (shown after an update)
  and `latest.json` carries the newest sections (shown before one). Write entries for people,
  not developers.
- Test an update locally: serve a folder with `latest.json`, its `.sig` and a binary, and run
  with `TOWNSQUARE_UPDATE_URL=http://127.0.0.1:PORT`.
- API docs on any running instance: `/api/v1/docs`.
- Local: `make build && bin/townsquare serve --listen 127.0.0.1:8890`; UI hot reload: `make dev`;
  agent key: `bin/townsquare apikey create NAME --scope write`.
