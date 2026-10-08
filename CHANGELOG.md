# Changelog

All notable changes to Townsquare (called WA Cal before v0.6.0). Format: [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versions follow [Semantic Versioning](https://semver.org/). The REST API is versioned separately
(`/api/v1`); see "API changes" in each release.

## [Unreleased]

### Added
- Engagement stats (new Stats view, key S). Townsquare now counts WhatsApp reads, deliveries,
  voice note plays, reactions and replies as they happen, plus WhatsApp channel views and Telegram
  views, shares, replies, reactions and story views. The board shows reach, read rate, read speed
  (1 hour to 1 week), best time to post, delivery health, what works by type, tag, client and app,
  a chat table with trends, top reactions, member growth and Telegram's own channel insights.
  Calendar cards show small reach and reaction numbers, and each sent post shows its numbers,
  a 48-hour read curve and (optional) who read, reacted or replied. Copy or send a text summary,
  or download a CSV. Counting starts with sends made after this update.
- API: `GET /api/v1/stats/summary`, `/stats/summary.txt`, `/stats/posts/{id}`, `/stats/badges`,
  `/stats/export.csv`, `POST /stats/share`, and the `stats_people` setting.
- `scripts/service.sh install` runs Townsquare as a macOS background service (launchd): it starts
  at login and restarts if it stops. `scripts/deploy.sh` uses the service when installed.
- New Townsquare logo (a glossy green gazebo on a town green with a yellow flag) as the app icon,
  browser tab icon, home screen icon and web app manifest.
- **Telegram**, logged in as your own account (official API via gotd/td, QR login with two-step
  password support): Settings → Telegram. Your groups, supergroups and channels appear as chats
  with a TG badge; new ones start off the allowlist, Saved Messages is allowed for tests. Posts
  can mix WhatsApp and Telegram chats. WhatsApp-style formatting is converted for Telegram, and
  photos and videos go as one album. "Test on Telegram" sends to Saved Messages.
- Telegram forum topics as their own chats, and stories ("My Story" and channels you can post
  stories to) as targets, like WhatsApp Status.
- Telegram's own scheduled queue (Settings → Telegram, off by default): sends due within 24 hours,
  48 hours or 7 days are handed to Telegram ahead of time so they go out even if this computer is
  off. Edits, moves, pauses, deletes and undo keep Telegram's queue in step.
- Optional Telegram bot (go-telegram/bot): add the bot to a group or channel and it appears as a
  "TG bot" chat.
- Sign-in links can go to Telegram Saved Messages as well as WhatsApp, and the sign-in screen
  explains where the link arrives.

### Changed
- Reminders to yourself ("Message yourself", Telegram Saved Messages) ignore quiet hours and
  send without the pause between messages. Before, a reminder to yourself at night was held back.
- App colors now match the logo: sky blue for actions, grass green for "connected", and the flag
  yellow for today. WhatsApp green stays only in the WhatsApp preview and platform badges.
- AGENTS.md slimmed to the always-needed rules (68 lines); deprecation and release detail moved to
  `docs/`. CLAUDE.md imports AGENTS.md. The old `design/` mock was removed.
- Deploy settings for a specific host now live in an untracked `.deploy.env`.
- README rewritten in plain language with a new banner, demo screenshots for desktop and phone,
  and a short tour GIF.
- WhatsApp channel view counts are now read from the channel's message list, because the
  "updates" query kept timing out. The old query is still tried if the list fails.

### Changed (breaking, renamed)
- **WA Cal is now Townsquare**: "One calendar to schedule all your community posts."
  - Repo `github.com/nerveband/townsquare`, Go module path, command `townsquare` (was `wacal`).
  - Data folder `~/.townsquare` (deploy moves `~/.wacal` and leaves a symlink).
  - Tailnet machine name `townsquare` (was `wa-cal`).
  - New API keys start with `tsq_` (old `wacal_` keys keep working).
  - Session cookie renamed, so browsers sign in once more. Header `X-Townsquare-Version`.
  - The linked device shows as "Townsquare" on new pairings.

### Added
- Demo mode: `wacal serve --demo` opens the app with sample groups and posts in a separate
  data folder, never connects to WhatsApp and never sends.
- Simpler README with screenshots of every view.
- Search (press `/` or ⌘K): find any post by words, group, tag or client; filter by status, tag
  and client; quick view with preview, schedule and next sends; quick actions per row.
- Bulk changes: check several posts, then pause, resume, tag, set client, add or remove groups,
  move to drafts, archive or delete, all as one undoable change.
- Sidebar tag and client filters are checkboxes with All, None and "only".
- Web app sign-in: new browsers get a one-time sign-in link in your "Message yourself" chat
  (or run `wacal login-link`). Settings → Access lists signed-in devices.
- Quiet hours per client, each in its own time zone (or none), in Settings → Clients and the API.
- Real WhatsApp preview: bold, italic, strikethrough, monospace, quotes, lists and links render
  everywhere (composer, cards, preview card, search). The composer shows each message exactly
  as it will arrive (one bubble per photo, video, voice note or file).
- Composer: the editor grows with long messages, list and quote buttons, a character count, and
  a wide mode with the preview beside the editor.

### Changed
- Tablet and phone layouts reworked: two-row top bar, floating new-post button below 1000px,
  scrollable filter chips, sticky hours in the Time view, and a bottom sheet on touch screens.
- A send due inside quiet hours is now recorded as "held" with the reason right away, instead
  of waiting and then being marked missed.

### Fixed
- The Telegram bot no longer stays off for the session if Telegram is slow to answer at startup.
- Townsquare now shuts down cleanly on SIGTERM (what launchd and systemd send to stop a service).
- Broken images when the session expired: images now fall back to a placeholder and the
  sign-in screen appears.
- The web app's internal routes needed no sign-in, so any program on the tailnet could skip
  API key scopes (including the admin-only safety switches). They now require a session.
- The app version and demo flag now reach the web app (Settings shows the real version).
- The v0.5.0 id migration counted group JIDs as ids, which made new ids too large for
  JavaScript. Sequences are repaired and affected history entries renumbered on startup.

### API changes
- `GET /api/v1/status` includes `demo`.
- New `/api/v1/telegram` (status), `/login`, `/qr.png`, `/password`, `/logout` (admin) and `/refresh`.
- Targets have `platform` (`whatsapp` or `telegram`); `POST /api/v1/test-send` takes `platform`.
- New `POST /api/v1/posts/bulk`.
- Clients have `quiet_start`, `quiet_end`, `timezone` (additive).
- New `GET /api/v1/sessions`, `DELETE /api/v1/sessions/{id}` (admin).
- OpenAPI `PostPatch` now lists its properties.
- Undo and redo are recorded as their own history entries (`undid: ...`, `redid: ...`). The
  response's `change` is that entry's id, plus `undid` / `redid` with the reverted change.
- `GET /api/v1/targets` sends `X-Total-Count` (matches before `limit`).
- **Changed (breaking, approved pre-1.0 exception):** `POST /api/v1/posts` now saves a draft unless
  `"status": "scheduled"` is sent, even when a time is given; the response adds a `notice` in that
  case. Before, a time without a status scheduled the post. The web app is unaffected.
- `GET /api/v1/posts` items now include `next_at`, `last_at` and `upcoming` (additive).

## [v0.5.0] - 2026-10-06

### Added
- REST API `/api/v1` for scripts and AI agents: API keys with `read`/`write`/`admin` scopes,
  OpenAPI 3.1 spec, interactive docs, Markdown agent guide, chat names accepted as targets,
  partial `PATCH` updates, `send_at` shortcut, `POST /posts/preview` dry run, media by URL,
  `expect_change` guard on undo.
- API keys tab in Settings and `wacal apikey create|list|revoke`.
- Dedicated tailnet address (`--tailscale NAME` → https://NAME.<tailnet>.ts.net).
- `wacal version`, version in `/api/v1/status` and the `X-WACal-Version` header.
- Release tooling: `scripts/check.sh`, `build.sh`, `deploy.sh`, `release.sh`, Makefile, AGENTS.md.

### Fixed
- Ids are never reused, so undoing an old delete can't overwrite a newer post (database migrated
  to AUTOINCREMENT; sequences bumped past every id in history).

### API changes
- New: everything under `/api/v1`.

## [v0.4.0] - 2026-10-06

### Added
- Time zone preview slider: view every time on screen in another zone, make it the default.
- Dual-zone times on cards, preview card and composer ("Same moment: …").
- Strong "today" highlight and a "now" line in Week, Time, Month and List.
- Inline tag and client picker: find, create, rename, recolor, delete.

## [v0.3.0] - 2026-10-06

### Added
- Time view (week by hour) with click-to-create at a time and drag to a time slot.
- Click empty space in Week or Month to create a post.
- Sortable groups table in Settings.
- Tablet and phone layouts: drawer tray, stacked week, bottom sheets, floating new-post button.
- Undo and redo live in the History panel with tooltips; shortcuts show a toast.

## [v0.2.0] - 2026-10-06

### Added
- The app: scheduler with repeat rules, send loop with safe mode, pacing, quiet hours and grace
  window; undo/redo with full history; Svelte UI with Week cards, Month and List views, composer,
  hover preview, drafts tray, tags, clients, group sets and settings.

## [v0.1.0] - 2026-10-06

### Added
- Phase 0 CLI spike: pair by QR or code, list targets, send text, image, video, voice note and
  document to chats, channels and Status.
