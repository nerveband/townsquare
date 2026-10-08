# Releasing and deploying

- Semantic versioning; before 1.0, minor = features, patch = fixes.
- Every user-visible change: a line in `CHANGELOG.md` under `## [Unreleased]`
  (Added / Changed / Deprecated / Removed / Fixed / API changes).
- Release only when the owner asks:
  1. Move Unreleased items into `## [vX.Y.Z] - YYYY-MM-DD`, commit, push.
  2. `make release V=vX.Y.Z DRY=--dry-run`, then `make release V=vX.Y.Z` (checks, builds
     darwin/arm64 and linux/amd64, tags, creates the GitHub release with the changelog notes).
  3. `make deploy` on the production host: backs up the database, builds, restarts the app and
     verifies the version. It restarts the launchd service if `scripts/service.sh install` was
     run (starts at login, restarts on crash), else the `townsquare` tmux session. Host-specific settings (listen address,
     tailnet name) live in an untracked `.deploy.env` next to the Makefile.
- Push only to `github.com/nerveband/townsquare`.
- API docs on any running instance: `/api/v1/docs`.
- Local: `make build && bin/townsquare serve --listen 127.0.0.1:8890`; UI hot reload: `make dev`;
  agent key: `bin/townsquare apikey create NAME --scope write`.
