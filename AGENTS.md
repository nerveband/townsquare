# AGENTS.md: Townsquare

One calendar to schedule all your community posts, across WhatsApp and Telegram. One Go binary:
platform senders (whatsmeow for WhatsApp, the Bot API for Telegram), scheduler, SQLite, REST API
(`/api/v1`) and an embedded Svelte UI.

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
- Telegram goes through the official Bot API only. Never log or commit bot tokens.
- Web routes (`/api/...`) need a signed-in session; agents use `/api/v1` with keys. Only
  `/api/auth/*`, the public v1 docs and static files are open.
- Never log, commit or print secrets (API keys, bot tokens, `TS_AUTHKEY`, files under
  `~/.townsquare`). The repo is public: keep host names, IPs and personal data out of it.

## 3. Where code goes

`internal/wa` WhatsApp only · `internal/tg` Telegram only · `internal/store` SQL only ·
`internal/server` HTTP shapes and the send loop · `web/ui` presentation only (the server validates everything) · `tools/gen_openapi.py`
is the spec source · `scripts/` check, build, deploy, release.

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
- Cutting a release or deploying: read `docs/releasing.md`.
- Before pushing, always: `make check`.
