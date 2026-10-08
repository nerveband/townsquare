# Townsquare API: guide for agents

Townsquare schedules community posts (text, images, video, voice notes, documents) to WhatsApp
groups, community announcement groups, channels, Status and "Message yourself", and to Telegram
groups, supergroups, channels and Saved Messages.
This API can do everything the web app does. Full reference: `GET /api/v1/openapi.json`,
interactive docs: `/api/v1/docs`.

## Auth

Send `Authorization: Bearer tsq_...` on every request. Keys are created in the web app
(Settings → API keys) or with `townsquare apikey create NAME --scope write`.

| Scope | Can |
|---|---|
| `read` | GET anything |
| `write` | create, edit, move, delete, undo, upload, send tests |
| `admin` | also turn safe mode on/off, edit the allowlist, manage API keys |

Every change is recorded in history as `api:<key name>` and can be undone. Undo and redo are
recorded too: `POST /undo` returns `undid` (the change it reverted) and `change` (the new
history entry, summary `undid: ...`). In `GET /changes` the reverted entry has `undone: true`.

## Safety rules (read these first)

1. **Safe mode** is usually on: only allowlisted chats receive anything. Others are
   recorded as `blocked` ("held"). Check `GET /api/v1/status` → `safe_mode`.
2. **New posts are drafts unless you say otherwise.** A post only sends after you set
   `"status": "scheduled"`, even if it has a time. If you give a time without a status, it is
   saved as a draft and the response includes a `notice`. Schedule only when you were asked to.
3. **Preview first.** `POST /api/v1/posts/preview` validates a post and returns the next
   send times and resolved targets, without saving anything.
4. **Test on yourself.** `POST /api/v1/test-send` sends straight to "Message yourself".
5. **Undo your own change only.** `POST /api/v1/undo` with `{"expect_change": <change id
   from your write response>}` refuses if someone else changed something since.

## Concepts

- **Target**: a chat. `jid` is the stable id. `kind` is `self`, `status`, `announce`
  (community announcements), `group`, `channel` or `community` (not postable).
  `can_send` is false where only admins can post. `GET /targets` returns a plain list; the
  number of matches before `limit` is in the `X-Total-Count` response header. Anywhere a target is accepted you may
  pass a jid, an exact chat name, `"me"` or `"status"`. Unknown or ambiguous names fail
  with a list of close matches. Never guess; look up with `GET /api/v1/targets?q=...`.
- **Platforms:** a target's `platform` is `whatsapp` or `telegram`. Telegram JIDs look like
  `tg:self` (Saved Messages, safe for tests), `tg:chat:<id>` or `tg:ch:<id>:<hash>`. One post may
  mix platforms. Formatting is written WhatsApp-style (`*bold*`) and converted for Telegram.
  Telegram sends photos and videos as one album (caption on the first). Forum topics are their
  own targets (`...:t<topic>`); stories are targets too (`tg:story:self`, `tg:story:ch:...`) and
  need a photo or video. Bot chats (`tgbot:<id>`, platform `telegram_bot`) post as the bot.
- **Telegram queue:** with setting `tg_queue_hours` above 0, Telegram sends due within that window
  are handed to Telegram's own scheduled queue (they go out even if Townsquare is offline); the
  daily cap and gap don't apply to those. Stories always send live.
- **Post**: title, caption (WhatsApp markup: `*bold*` `_italic_` `~strike~`), media ids,
  targets, optional tag and client, status (`draft`, `scheduled`, `paused`, `archived`),
  and one or more **schedules**.
- **Schedule**: `start` (local `YYYY-MM-DDTHH:MM`), `tz` (IANA zone), optional `rrule`
  (RFC 5545 body, e.g. `FREQ=WEEKLY;BYDAY=WE`), optional `until`. A post can have several.
- **Send**: one occurrence of a schedule. Key = `schedule_id` + `occ` (its original local
  time). `GET /api/v1/sends?from=&to=` lists them with delivery state.
- **Edit scope** (repeating posts): `PATCH /api/v1/posts/{id}` with `scope`:
  `one` (only the send `schedule_id`+`occ`), `future` (that send and later), `all`.

## Common tasks

```http
GET /api/v1/status
GET /api/v1/targets?q=knoxville&can_send=true
POST /api/v1/media            {"url": "https://example.com/flyer.png"}   (or multipart "file")
POST /api/v1/posts/preview    {"caption": "Hi", "targets": ["me"], "send_at": "2026-10-14T18:30", "tz": "America/Los_Angeles"}
POST /api/v1/posts            {"title": "Flyer", "caption": "*This week*", "media": [12],
                               "targets": ["Main group"], "status": "draft"}
POST /api/v1/posts            {"caption": "Jumu'ah at 1:30", "targets": ["status"], "status": "scheduled",
                               "schedules": [{"start": "2026-10-09T11:00", "tz": "America/New_York", "rrule": "FREQ=WEEKLY;BYDAY=FR"}]}
PATCH /api/v1/posts/7         {"targets": ["Main group", "Youth group"]}
PATCH /api/v1/posts/7         {"caption": "Moved inside", "scope": "one", "schedule_id": 9, "occ": "2026-10-14T18:30"}
POST /api/v1/sends/move       {"post_id": 7, "schedule_id": 9, "occ": "2026-10-14T18:30", "to": "2026-10-15T18:30", "tz": "America/New_York", "scope": "one"}
POST /api/v1/sends/skip       {"post_id": 7, "schedule_id": 9, "occ": "2026-10-21T18:30"}
POST /api/v1/posts/7/pause
POST /api/v1/posts/bulk       {"ids": [7, 8, 9], "action": "tag", "tag_id": 2}      (one undoable change)
GET  /api/v1/posts?q=bake&status=scheduled,paused   (each has next_at, last_at, upcoming)
POST /api/v1/undo             {"expect_change": 42}
POST /api/v1/test-send        {"caption": "Test", "platform": "telegram"}   (goes to Saved Messages only)
GET  /api/v1/changes
```

Write responses include `change` (the history id), the resulting `post` where relevant,
and `undo`/`redo` hints. Errors are `{"error": "message", "code": "machine_code"}` with a
matching HTTP status (400, 401, 403, 404, 409, 422, 503).

## Sending behavior

The send loop runs every 15 s. For each chat it checks, in order: safe mode (allowlist),
quiet hours, whether you can post there, and the daily cap. Then it waits a random gap
between chats. Media goes as separate messages with the caption on the first.

- **Your own chat** (`kind: "self"`: WhatsApp "Message yourself", Telegram Saved Messages) has no
  quiet hours and no gap, so reminders to yourself send on time, day or night.
- **Quiet hours:** a send due inside quiet hours is **not sent**. It is recorded as
  `blocked` with the reason (for example "inside quiet hours (22:00 to 07:00
  America/New_York)") and is not retried. Pick times outside the window.
- **Which quiet hours apply:** the chat's client's quiet hours if that client has them;
  otherwise the post's client's; otherwise the global ones (`GET /settings`:
  `quiet_start`, `quiet_end`, in `timezone`). A client with `quiet_start` equal to
  `quiet_end` has no quiet hours. See `GET /clients`.
- **Late sends:** if Townsquare was offline at send time, it still sends within `grace_min`
  minutes; after that the send is marked `missed` instead of posting late.

## Engagement stats

Townsquare counts what happens after a send, starting with sends made after this feature shipped:
WhatsApp read, delivered and played receipts, reactions and replies (live, one per person),
WhatsApp channel views and reactions, Telegram views, shares, replies and reactions, and story
views (checked every 15 minutes for 48 hours, then daily for 30 days). Reach is readers, or
views where a platform only reports views. Read rate is reach ÷ members at send time. Sends to
your own chat aren't counted.

```
GET  /api/v1/stats/summary?days=30&platform=whatsapp&client=8   (tiles, daily, speed, heatmap, chats, ...)
GET  /api/v1/stats/summary.txt?days=7                            (short text report)
GET  /api/v1/stats/posts/31?schedule_id=40&occ=2026-10-09T06:00 (one send: per chat, 48-hour curve, people)
GET  /api/v1/stats/badges?from=2026-10-05&to=2026-10-12          ({"schedule_id|occ": {reach, reactions, replies}})
GET  /api/v1/stats/export.csv?days=90
POST /api/v1/stats/share?days=7  {"to": "me"}                     (sends the text report now; safe mode applies)
```

Setting `stats_people` ("0" default, "1" on, admin) keeps names for reads, reactions and
replies so `GET /stats/posts/{id}` can list them. Names are deleted after 30 days and right away
when it is turned off. Counts stay either way.


## Setup, updates and running

Admin keys can do the setup that used to need a terminal:

```
GET  /api/v1/whatsapp                         ({linked, connected, number, link: {state, pair_code}})
POST /api/v1/whatsapp/link  {"phone": "15551234567"}   (phone optional: adds a typed pairing code)
GET  /api/v1/whatsapp/qr.png                  (scan in WhatsApp: Settings > Linked devices > Link a device)
POST /api/v1/whatsapp/logout                  (unlinks this device)
POST /api/v1/telegram/app   {"api_id": "...", "api_hash": "..."}   (from my.telegram.org; then /telegram/login)
```

Townsquare updates itself from signed GitHub releases. `GET /api/v1/update` shows the current
and newest version; `POST /api/v1/update/check` checks now. With the `auto_update` setting on
(the default) it checks every 6 hours, downloads the release, verifies its Ed25519 signature
and SHA-256, and restarts into it when no post is due within 15 minutes.
`POST /api/v1/update/install` (admin) does it now; it answers 409 `busy` near a send unless you
pass `{"force": true}`. Builds from source report updates but never install them.

`GET/PUT /api/v1/autostart {"enabled": true}` (admin) turns "start when I log in" on or off.
`POST /api/v1/quit` (admin) stops the server.
