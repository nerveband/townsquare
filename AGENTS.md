# AGENTS.md: Townsquare

One calendar to schedule all your community posts, across WhatsApp and Telegram. One Go binary:
platform senders (whatsmeow for WhatsApp; gotd/td for the user's Telegram account, plus an
optional Bot API bot), scheduler, SQLite, REST API (`/api/v1`), an embedded Svelte UI, and a
signed self-updater. Shipped as a Mac app, Windows exe, `.deb` and plain binaries.

## 1. UI and API ship together

Every capability exists in the web UI **and** `/api/v1`, in the same change (machine-only
things like API keys and `expect_change` are the exception). A change is done when:

1. **One handler, two mounts.** Logic lives in `internal/server` / `internal/store`; the UI route
   (`/api/...`) and the v1 route call the same function.
2. **v1 route** in `v1Mux()` (`internal/server/v1.go`) with the right scope: GET = `read`,
   changes = `write`, anything that weakens safety (safe mode, allowlist, keys, sessions) =
   `admin` (`needsAdmin` in `auth.go`).
3. **OpenAPI** updated in `tools/gen_openapi.py`, then `make spec` (a test fails if a spec path
   isn't routed).
4. **Agent guide** `internal/server/guide.md` updated when agents should work differently.
5. **UI built** (`make ui`) and `web/dist` committed (the binary embeds it).
6. **History:** every write goes through `DB.Mutate(...)` with a plain-English summary and the
   right entity keys, so it is undoable and attributed (`you`, `sender`, `api:<key>`).
7. **Tests** for new logic, and a `CHANGELOG.md` line under `## [Unreleased]` incl. "API changes".
   Users read the changelog inside the app after every update: write it for them, in plain words.

## 2. Safety (real WhatsApp groups)

- Never send to a real group, community or channel while developing. Test sends go only to the
  owner's own chats (WhatsApp "Message yourself" = `me`, the "Townsquare test" channel, or a
  Telegram test chat the owner set up), and only when the owner said testing is OK for this task.
  Prefer drafts or `paused` posts; delete test data.
- Safe mode defaults on. Every send path goes through the dispatcher's safe-mode, allowlist,
  quiet-hours and daily-cap checks. Only `POST /test-send` skips them, hard-wired to `me`.
- UI work, screenshots and click-testing: demo mode only (`bin/townsquare serve --demo --listen
  127.0.0.1:8891`; fake data, never connects or sends). README images come from demo mode.
- Runs on the owner's own machine. No cloud one-click deploys (datacenter IPs raise WhatsApp ban
  risk). One process per WhatsApp session: stop `serve` before CLI send commands.
- Telegram uses the official API only (gotd/td as the user's account, or the Bot API). Never log
  or commit bot tokens, Telegram sessions, or the shared Telegram app id.
- Web routes (`/api/...`) need a signed-in session; agents use `/api/v1` with keys. Only
  `/api/auth/*`, the public v1 docs and static files are open.
- Never log, commit or print secrets (API keys, bot tokens, `TS_AUTHKEY`, files under
  `~/.townsquare`, the release signing key and the Telegram app id in `~/.config/townsquare`).
  The repo is public: keep host names, IPs, phone numbers, real group ids and personal data out
  of it (tests use made-up ids).
- Never restart or deploy production within 15 minutes of a scheduled send (`bin/townsquare
  due`; `make deploy` checks). Production updates itself from releases on the same rule.

## 3. Where code goes

`internal/wa` WhatsApp only · `internal/tg` Telegram account (and its app id sources) ·
`internal/tgbot` Telegram bot · `internal/store` SQL only · `internal/server` HTTP shapes, the
send loop, stats, updates and setup · `internal/update` signed self-update and hand-off ·
`internal/autostart` start at login per OS · `internal/changelog` reads `CHANGELOG.md` (embedded
by the root package) · `web/ui` presentation only (the server validates everything) ·
`tools/gen_openapi.py` is the spec source · `tools/sign`, `tools/manifest`, `tools/mkdeb.py`
release tooling · `scripts/` check, build, package, release, verify-release, deploy, service.

## 4. Rules the code won't tell you

- User-facing words (UI, README, errors) at about an 8th-grade level; technical detail goes in
  this file, the API guide or comments. No em dashes anywhere (`make check` enforces).
- Errors say what to do next. API errors are `{"error", "code"}` via `fail` / `failCode`.
- Pure Go, no cgo. Time: wall time + IANA zone per schedule, compute in UTC, never the server's
  zone. IDs are never reused; schedule ids survive edits (deliveries point at them).
- Schema changes are additive and idempotent in `store.Open`; never drop user data.
- UI: Svelte 5 runes; state in `lib/state.svelte.js`; calls via `api()`, writes via `act()`.
  Works at desktop, tablet (≤1000px) and phone (≤700px). Times render in `tz()`. Repeating-post
  edits make the scope explicit. Destructive actions: do it, then offer Undo (no confirm dialogs).
  Visual changes need a screenshot check; refresh `docs/screenshots/` (from demo mode) when the
  UI changes visibly. Fonts: Sofia Sans Extra Condensed 600 for headings only, Inter for UI,
  JetBrains Mono for times and labels; palette tokens in `app.css`. Dense by default.

## 5. More detail (read when relevant)

- Changing or removing anything in `/api/v1`: read `docs/api-deprecation.md` first.
- Cutting a release, deploying, or touching updates, packaging or the signing key: read
  `docs/releasing.md` and follow its checklist in order. Release only when the owner asks.
- Before pushing, always: `make check`.
