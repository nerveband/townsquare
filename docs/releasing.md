# Releasing and deploying

Release only when the owner asks. Every release reaches every install on its own (signed
self-updates), so a mistake ships everywhere within hours. Follow the steps in order.

## Versions and changelog

- Semantic versioning; before 1.0, minor = features, patch = fixes. Versions only go up:
  installs never move backwards, so never delete, re-tag or replace a published release's
  binaries. If a release is broken, ship a fixed patch (vX.Y.Z+1).
- Every user-visible change gets a line in `CHANGELOG.md` under `## [Unreleased]`
  (Added / Changed / Deprecated / Removed / Fixed / API changes), in the same commit.
- The changelog is what people read in the app (the **Update** / **What's new** button), on
  GitHub, and in `/api/v1/update`. Write for the people who use Townsquare: plain words, about
  an 8th-grade level, what changed for them. Put developer detail in commits, not here.

## Release checklist

1. **Pre-flight.** On `master`, clean tree, in sync with origin. `make check` passes.
   No post due within 15 minutes (`bin/townsquare due`). Secrets present (both restore from
   1Password if missing, see below):
   - `~/.config/townsquare/release-signing.key` (or `TOWNSQUARE_SIGNING_KEY`)
   - `~/.config/townsquare/telegram-app` (or `TOWNSQUARE_TG_APP_FILE`)
2. **Changelog.** Move everything under `[Unreleased]` into `## [vX.Y.Z] - YYYY-MM-DD` at the
   top, leave `[Unreleased]` empty, read it once as a user would. Commit, push.
3. **If the change touches updates** (`internal/update`, `cmd/townsquare` start-up, `Restart`,
   `RunUpdates`, `scripts/package.sh`), test an update locally first: build two versions, serve a
   folder with a signed `latest.json` and a binary, and run the older one with
   `TOWNSQUARE_UPDATE_URL=http://127.0.0.1:PORT` (`townsquare update`, then `version`; and a
   running `serve` restarting into it). Breaking the updater strands every install.
4. **Dry run.** `make release V=vX.Y.Z DRY=--dry-run`. It refuses when the version isn't newer
   than the latest release, the newest changelog section isn't `[vX.Y.Z]`, `[Unreleased]` isn't
   empty, or a secret is missing. It then runs `make check` and builds everything into `dist/`.
5. **Release.** `make release V=vX.Y.Z`: tags, pushes the tag, creates the GitHub release
   (notes from the changelog, marked latest), then runs `scripts/verify-release.sh`:
   - all 13 downloads attached;
   - `releases/latest/download/latest.json` is this version;
   - the previous release, as installed, downloads this one, verifies it and runs it.
   If verification fails, stop and fix with a new patch release. Don't delete the release.
6. **Production.** The production host runs a release build, so it updates itself within 6
   hours, restarting only when no post is due within 15 minutes. To update right away:
   `bin/townsquare update` (downloads it; the running server switches at its next 10-minute
   check). Then confirm the version: `curl -sI https://<host>/api/auth/status | grep -i version`.
7. **Tell the owner** the version, the release link, and anything they must do.

Downloads built by `scripts/package.sh` (macOS only): plain binaries (darwin arm64/amd64, linux
amd64/arm64/armv7, windows amd64), `Townsquare-vX.Y.Z-mac.dmg`, `.deb` for amd64/arm64/armhf,
`SHA256SUMS`, and the signed `latest.json` (+ `.sig`) with the newest changelog sections and the
shared Telegram app id.

## Deploying unreleased code

`make deploy` on the production host backs up the database, builds the checkout, restarts the
launchd service (`scripts/service.sh install`) or the tmux session, and checks the version. It
refuses when a post is due within 15 minutes (`FORCE=1` overrides). A build of an untagged
commit is a dev build: it reports updates but never installs them, so production stops
auto-updating until a tagged build runs again (`git checkout vX.Y.Z && make deploy`, or
`bin/townsquare update` from a release build). Host settings (listen address, tailnet name,
`TOWNSQUARE_DEPLOY_HOST`) live in the untracked `.deploy.env`.

## How updates work (and what not to break)

- Installs fetch `releases/latest/download/latest.json` every 6 hours and accept it only with a
  valid `latest.json.sig` from a key in `internal/update/key.go`, then check the binary's SHA-256.
- The new binary goes to `~/.townsquare/bin`; the installed file is never touched. On every
  start, `update.Handoff` runs the newest verified binary there instead (dev builds never hand
  off). A running server restarts into it via `update.Restart` when no post is due.
- The web app shows the **Update** button from `/api/update` (notes from `latest.json`) and the
  **What's new** button from `/api/changelog` (the `CHANGELOG.md` embedded in the binary), and
  reloads itself after the server restarts into a new version.

## Secrets

- **Release signing key:** `~/.config/townsquare/release-signing.key`, backed up in 1Password
  ("Townsquare release signing key", vault "AI Agents", field `credential`). Restore with mode
  600. Without it, existing installs can't accept new releases. To rotate:
  `go run ./tools/sign keygen NEWFILE`, add its public key to `key.go`, release once signed with
  the old key, then sign with the new one.
- **Shared Telegram app id:** `~/.config/townsquare/telegram-app` (`api_id:api_hash`, mode
  600), backed up in 1Password ("Townsquare shared Telegram app id", vault "AI Agents"). Release builds bake it in and `latest.json` carries it. Installs use their own
  `telegram.app` first, then the id from the newest signed `latest.json`
  (`telegram.shared.json`), then the built-in one. To swap it everywhere without a release, put
  the new id in that file and run `scripts/telegram-app.sh`; installs switch at their next check
  and restart when no post is due. If Telegram rejects existing logins, people scan again.
- Neither ever goes in the repo, logs or chat.

## Other

- Push only to `github.com/nerveband/townsquare`.
- API docs on any running instance: `/api/v1/docs`.
- Local: `make build && bin/townsquare serve --listen 127.0.0.1:8890`; UI hot reload: `make dev`;
  demo: `bin/townsquare serve --demo --listen 127.0.0.1:8891`; agent key:
  `bin/townsquare apikey create NAME --scope write`.
