# Changelog

All notable changes to Townsquare (called WA Cal before v0.6.0). Format: [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versions follow [Semantic Versioning](https://semver.org/). The REST API is versioned separately
(`/api/v1`); see "API changes" in each release.

## [Unreleased]

### Added
- **More than one account, when you need it.** Settings → Accounts → "Add another WhatsApp
  number" or "Add another Telegram account" links a second (or third) account with its own QR
  code. Its chats show a small account label wherever you pick chats, and each chat posts from
  the account it belongs to. If you only use one account, nothing changes. Extra Telegram
  accounts send live (Telegram's own queue is for the first account).

### Fixed
- CLI: `--fields` and `--transform` now apply to `--dry-run` output too.
- Clients with one chat say "1 chat".

## [v0.8.0] - 2026-10-08

### Added
- **Take a post back.** Open a sent post to delete it for everyone in every chat it went to
  (WhatsApp allows about 2.5 days, Telegram has no limit), or to fix its text (WhatsApp: 15
  minutes, text only; Telegram: any time). It first shows which chats still allow it.
- **Undo send.** Settings → General → Undo send makes each post wait 30 seconds to 5 minutes
  after its time. A bar at the top counts down with a Cancel button.
- **Sent posts open a details view** instead of the editor: how it did (reach, reactions,
  replies, a 48-hour read curve, who read it), where it went, what was sent, and **Post again**,
  which starts a new draft with the same text, pictures and chats so you can change them.
- **Preview as WhatsApp or Telegram.** A switch in the editor shows a post the way each app
  shows it (Telegram puts photos into one album). One post can go to both apps.
- **Drafts board.** "See all" next to Drafts shows every draft with its picture, text,
  attachments, chats, tag and client. Search, sort, copy, archive or delete several at once.
- **Stats:** hover a day on the charts to see what went out that day; **Summary…** shows the
  text summary to copy or send. Calendar cards show their numbers as soon as stats exist.
- **Settings → Accounts** shows WhatsApp and Telegram side by side: who's linked, connected
  since, when chats were last loaded, refresh, test, link and unlink. General shows both.

### Changed
- Clients: quiet hours sit on their own line under each client, with the global hours shown
  when a client uses them.

### Fixed
- The Stats page now scrolls all the way down.

### API changes
- New `/api/v1/accounts` (list, add, get, rename, remove, login, qr.png, password, refresh).
  Targets have `account`; chats of extra accounts have ids `wa@<n>:…` and `tg@<n>:…`.
- New `POST /api/v1/sends/unsend`, `POST /sends/edit`, `GET /sends/pending`; setting
  `send_delay`. Sends can have delivery `unsent` and an `unsent` count.
- `GET /stats/summary` days include `items` (posts that went out that day).
- `GET /whatsapp` adds `name`, `chats`, `allowlisted`, `connected_since`, `last_disconnect`,
  `last_sync`; `GET /telegram` adds `last_sync`.

## [v0.7.1] - 2026-10-08

### Fixed
- The Stats page could stay on "Loading..." when no post had any reactions yet. Lists in the
  stats answers are now always lists, and a test checks every answer on an empty database too.
- `-o text` tables now show the columns you ask for with `--fields`.

## [v0.7.0] - 2026-10-08

### Added
- **A command line for AI agents and scripts.** Every feature is a `townsquare` command
  (`townsquare posts list`, `posts create`, `agenda`, `doctor`, ...), generated from the same
  contract as the API so they always match. JSON output, clear exit codes, `--dry-run` and
  `--yes` for anything that sends or deletes, safe retries, saved profiles, and a built-in agent
  guide (`townsquare skills show`). All commands: docs/cli.md or `townsquare schema`.
- `townsquare doctor` checks the server, key, versions, WhatsApp, Telegram and what's due;
  `townsquare agenda` lists what goes out next.

### Changed
- The old direct commands `townsquare targets`, `status`, `send`, `allow` and `channel-create`
  are replaced by `townsquare targets list`, `status get`, `test send` and
  `whatsapp channels create`, which go through the running Townsquare (so its safety checks
  apply). `townsquare due` now exits 20 when something is due (it was 3).

### API changes
- Writes accept an `Idempotency-Key` header (24-hour replay).
- `GET /posts` and `GET /changes` take `limit` and `offset`; `GET /targets` takes `offset`.
  `X-Total-Count` has the full count.
- `POST /posts/bulk` returns per-post `results`; pausing a paused post returns
  `"changed": false`. `GET /status` includes `demo`. Telegram status leaves out `app_source`
  when Telegram isn't set up.

## [v0.6.2] - 2026-10-08

### Added
- **Updates can't cost you a post.** Townsquare installs an update at the first moment with no
  post due within 15 minutes either side and nothing being sent, and shows when that will be.
  It test-runs the new version first; if it fails, or doesn't start properly twice, Townsquare
  skips it and keeps running the version that works.
- **Everything works without a screen.** New API (and Settings) for the server address and
  tailnet name, restarting, the Telegram bot token, making sign-in links for people, and
  creating WhatsApp channels. `townsquare config` and `townsquare autostart` do the same in a
  terminal. Opening the app and start at login use the saved address.
- Only one Townsquare can run on a data folder at a time, so two copies can never double-send.
- **Photos work without ffmpeg.** The Mac app and Windows exe can now post photos (JPEG, PNG,
  GIF, WebP) right away. Videos and voice notes still need ffmpeg, and the error says how to
  get it. The app also finds ffmpeg installed with Homebrew or winget when opened from Finder
  or the Start menu, and the demo runs without it.
- `townsquare due` lists posts due soon.
### API changes
- New `GET/PATCH /api/v1/config`, `POST /api/v1/restart`, `PUT/DELETE /api/v1/telegram/bot`,
  `POST /api/v1/login-link` and `POST /api/v1/whatsapp/channels` (admin except GET).
- `GET /api/v1/update` includes `install_at`.

## [v0.6.1] - 2026-10-08

### Added
- **Telegram works out of the box.** Releases include Townsquare's own Telegram app ID, so you
  just scan the QR code; no trip to my.telegram.org. The ID comes with each signed update, so it
  can be swapped on every install without a new release. You can still use your own ID in
  Settings → Telegram.
- **A quiet update button.** When an update is available, ready, or just installed, a small
  button appears in the top bar. Click it to read what's new. It never pops up over your work.
- **Seamless updates.** If Townsquare updates itself while the page is open, the page reloads on
  its own the next time you're not typing or editing (or shows **Reload** until then).
- Release notes in the app come straight from this changelog, the same text as on GitHub.

### Changed
- The search box and History button show as icons below 1440 pixels wide, to make room.

### API changes
- `GET /api/v1/update` includes `changes` (release notes for the newer version).
- New `GET /api/v1/changelog?from=&to=&limit=`.
- New `DELETE /api/v1/telegram/app` (admin) to go back to the shared app ID.
- Telegram status includes `app_source` (`own`, `shared` or `built-in`).

## [v0.6.0] - 2026-10-08

### Added
- **Downloads for every computer:** a Mac app (`.dmg`, Apple silicon and Intel), a Windows
  `.exe`, `.deb` packages for Debian, Ubuntu and Raspberry Pi OS (amd64, arm64, armhf), and plain
  binaries. Opening the app starts Townsquare and opens it in the browser, already signed in;
  opening it again just opens the browser.
- **Automatic updates** for every kind of install, including the command line. Every 6 hours
  Townsquare checks the newest GitHub release, verifies its Ed25519 signature and SHA-256,
  downloads it to `~/.townsquare/bin`, and restarts into it when no post is due within 15
  minutes. Settings → General shows the version with **Check for updates**, **Update now** and
  **Install updates automatically**; `townsquare update [--check]` does the same from a terminal.
  Builds from source report updates but never install them.
- **Setup in the browser:** link WhatsApp by QR code (or a typed code) from Settings, unlink it,
  and add the Telegram app id without editing files or restarting. A welcome banner points new
  users there.
- **Start when I log in** (Settings → General): a LaunchAgent on macOS, a systemd user service on
  Linux, a Run entry on Windows. **Quit Townsquare** when it was opened as an app.
- `townsquare help`, and running `townsquare` with no command opens the app.
- Licensed under the GNU AGPL v3.
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
- Tablet and phone layouts reworked: two-row top bar, floating new-post button below 1000px,
  scrollable filter chips, sticky hours in the Time view, and a bottom sheet on touch screens.
- A send due inside quiet hours is now recorded as "held" with the reason right away, instead
  of waiting and then being marked missed.

### Changed (breaking, renamed)
- **WA Cal is now Townsquare**: "One calendar to schedule all your community posts."
  - Repo `github.com/nerveband/townsquare`, Go module path, command `townsquare` (was `wacal`).
  - Data folder `~/.townsquare` (deploy moves `~/.wacal` and leaves a symlink).
  - Tailnet machine name `townsquare` (was `wa-cal`).
  - New API keys start with `tsq_` (old `wacal_` keys keep working).
  - Session cookie renamed, so browsers sign in once more. Header `X-Townsquare-Version`.
  - The linked device shows as "Townsquare" on new pairings.

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
- New `GET /api/v1/update`, `POST /update/check`, `POST /update/install` (admin, `force`).
- New `GET/PUT /api/v1/autostart` (PUT admin) and `POST /api/v1/quit` (admin).
- New `GET /api/v1/whatsapp`, `POST /whatsapp/link`, `GET /whatsapp/qr.png`, `POST /whatsapp/logout`
  (admin except GET), and `POST /api/v1/telegram/app` (admin).
- New setting `auto_update` ("1" default).
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
