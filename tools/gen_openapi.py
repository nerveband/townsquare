#!/usr/bin/env python3
"""Generates internal/server/openapi.json (OpenAPI 3.1) for the Townsquare /api/v1 API.
Run: python3 tools/gen_openapi.py"""
import json
import pathlib

def ref(name): return {"$ref": f"#/components/schemas/{name}"}
def arr(x): return {"type": "array", "items": x}
S = {"type": "string"}; I = {"type": "integer"}; B = {"type": "boolean"}; N = {"type": "number"}
def nullable(x): return {"oneOf": [x, {"type": "null"}]}
def obj(props, req=None, desc=None, extra=None):
    o = {"type": "object", "properties": props}
    if req: o["required"] = req
    if desc: o["description"] = desc
    if extra: o.update(extra)
    return o

schemas = {
    "Error": obj({"error": S, "code": {**S, "enum": ["bad_request", "bad_json", "unauthorized", "forbidden", "not_found", "conflict", "invalid", "unknown_target", "unprocessable", "media_failed", "fetch_failed", "internal", "unavailable"]}}, ["error", "code"]),
    "Status": obj({"connected": B, "phone": S, "timezone": S, "safe_mode": B, "allowlisted_targets": I, "sent_today": I, "daily_cap": I,
                   "undo": {**S, "description": "What undo would revert next"}, "redo": S, "now": {**S, "format": "date-time"},
                   "version": {**S, "description": "App version, e.g. v0.5.0"}, "commit": S,
                   "key": obj({"name": S, "scope": S})}),
    "Settings": obj({k: S for k in ["timezone", "safe_mode", "gap_min", "gap_max", "daily_cap", "quiet_start", "quiet_end", "grace_min", "tg_queue_hours", "stats_people", "auto_update"]},
                    desc="All values are strings. safe_mode is \"1\" or \"0\"; times are HH:MM; gaps in seconds; grace in minutes. Changing safe_mode needs admin scope."),
    "Target": obj({"jid": {**S, "example": "120363012345678901@g.us", "description": "WhatsApp JID; Telegram: tg:self, tg:chat:<id>, tg:ch:<id>:<hash>, tg:ch:<id>:<hash>:t<topic>, tg:story:self, tg:story:ch:<id>:<hash>; bot: tgbot:<chat id>"},
                   "platform": {**S, "enum": ["whatsapp", "telegram", "telegram_bot"]}, "kind": {**S, "enum": ["self", "status", "announce", "group", "channel", "community"]},
                   "name": S, "parent": {**S, "description": "Community name for announcement/sub groups"}, "can_send": B, "members": I,
                   "client_id": nullable(I), "allowed": {**B, "description": "On the safe-mode allowlist"}, "starred": B, "gone": B}),
    "TargetPatch": obj({"client_id": nullable(I), "starred": B, "allowed": {**B, "description": "Admin scope"}}),
    "Tag": obj({"id": I, "name": S, "color": {**S, "example": "#128C7E"}}, ["id", "name", "color"]),
    "TagInput": obj({"name": S, "color": S}),
    "Client": obj({"id": I, "name": S, "color": S,
                   "quiet_start": {**S, "description": "HH:MM. Empty = use the global quiet hours. Equal to quiet_end = no quiet hours."},
                   "quiet_end": {**S, "description": "HH:MM"}, "timezone": {**S, "description": "Zone for the quiet hours; empty = the app's zone"}}),
    "ClientInput": obj({"name": S, "color": S, "quiet_start": S, "quiet_end": S, "timezone": S},
                       desc="Quiet hours apply to chats assigned to this client (and to posts with this client when the chat has none)."),
    "UpdateState": obj({"current": S, "latest": S, "notes": {**S, "description": "Release notes URL"}, "available": B,
                        "staged": {**S, "description": "Downloaded version that starts on the next restart"}, "checked_at": I, "error": S,
                        "platform": {**S, "description": "e.g. darwin-arm64, linux-armv7, windows-amd64"},
                        "dev": {**B, "description": "Built from source: updates are reported, never installed"}, "auto": {**B, "description": "auto_update setting"},
                        "changes": arr(ref("ChangelogEntry")),
                        "install_at": {**I, "description": "Unix time of the next moment with no post due within 15 minutes either side; automatic updates and restarts wait for it"}}),
    "ServerConfig": obj({"listen": {**S, "description": "host:port for the web app and API (default 127.0.0.1:8890)"},
                         "tailscale": {**S, "description": "tailnet machine name, \"\" = off"},
                         "running": obj({"listen": S, "tailscale": S}), "restart_needed": B}),
    "ChangelogEntry": obj({"version": S, "date": S, "notes": {**S, "description": "Markdown from CHANGELOG.md"}}),
    "Autostart": obj({"supported": B, "enabled": B, "app_mode": {**B, "description": "Started by opening the app (offers Quit)"}}),
    "WhatsAppState": obj({"linked": B, "connected": B, "number": S,
                          "link": obj({"state": {**S, "enum": ["waiting", "code", "linked", "error"]}, "pair_code": S, "expires": I, "message": S, "version": I})}),
    "TelegramState": obj({"configured": {**B, "description": "a Telegram app id is available"},
                          "app_source": {**S, "enum": ["own", "shared", "built-in"], "description": "where the app id comes from"},
                          "status": {**S, "enum": ["off", "starting", "logged_out", "qr", "password", "ready", "error"]},
                          "user": S, "username": S, "error": S, "qr_expires": I, "version": I,
                          "chats": {**I, "description": "Telegram chats known"}, "allowlisted": I,
                          "queue_hours": {**I, "description": "0 = off; otherwise Telegram sends due within this many hours are handed to Telegram's own scheduled queue"},
                          "bot": obj({"name": S, "username": S, "chats": I})}),
    "Session": obj({"id": I, "device": {**S, "description": "Browser user agent"}, "created_at": I, "last_seen": I, "current": B}),
    "Set": obj({"id": I, "name": S, "client_id": nullable(I), "jids": arr(S)}),
    "SetInput": obj({"name": S, "client_id": nullable(I), "jids": {**arr(S), "description": "JIDs or exact chat names"}}),
    "Media": obj({"id": I, "kind": {**S, "enum": ["image", "video", "voice", "audio", "document"]}, "name": S, "mime": S, "width": I, "height": I, "seconds": I}),
    "Override": obj({"occ": S, "at": S, "skipped": B, "caption": S, "media": arr(I), "targets": arr(S)}, desc="A per-send exception on a repeating schedule"),
    "Schedule": obj({"id": {**I, "description": "Keep when editing so deliveries stay linked"}, "start": {**S, "example": "2026-10-14T18:30", "description": "Local wall time in tz"},
                     "tz": {**S, "example": "America/New_York"}, "rrule": {**S, "example": "FREQ=WEEKLY;BYDAY=WE", "description": "RFC 5545 RRULE body; empty = once"},
                     "until": {**S, "description": "Local YYYY-MM-DDTHH:MM, inclusive"}, "overrides": arr(ref("Override"))}, ["start", "tz"]),
    "Post": obj({"id": I, "title": S, "caption": S, "media": arr(I), "targets": arr(S), "tag_id": nullable(I), "client_id": nullable(I),
                 "status": {**S, "enum": ["draft", "scheduled", "paused", "archived"]}, "created_at": I, "updated_at": I, "schedules": arr(ref("Schedule"))}),
    "PostSummary": {"allOf": [ref("Post"), obj({"next_at": nullable({**S, "format": "date-time"}), "last_at": nullable({**S, "format": "date-time"}),
                                                  "upcoming": {**I, "description": "Sends in the next 90 days"}})]},
    "BulkInput": obj({"ids": arr(I), "action": {**S, "enum": ["pause", "resume", "draft", "archive", "delete", "tag", "client", "add_targets", "remove_targets"]},
                      "tag_id": nullable(I), "client_id": nullable(I), "targets": {**arr(S), "description": "For add_targets/remove_targets: JIDs or names"}},
                     ["ids", "action"]),
    "PostInput": obj({
        "title": S, "caption": {**S, "description": "WhatsApp markup allowed: *bold* _italic_ ~strike~ ```mono```"},
        "media": {**arr(I), "description": "Media ids from POST /media; each is sent as its own message, caption on the first"},
        "targets": {**arr(S), "description": "JIDs, exact chat names, \"me\" or \"status\""},
        "tag_id": nullable(I), "client_id": nullable(I),
        "status": {**S, "enum": ["draft", "scheduled", "paused", "archived"], "description": "Default: draft (even with a time). Set scheduled to send."},
        "schedules": arr(ref("Schedule")),
        "send_at": {**S, "description": "Shortcut for one send: RFC 3339, or local YYYY-MM-DDTHH:MM in tz", "example": "2026-10-14T18:30:00-07:00"},
        "tz": {**S, "description": "Zone for send_at and schedules without tz. Default: the app's time zone"},
    }, extra={"example": {"title": "Weekly flyer", "caption": "*This week* at the center", "media": [12], "targets": ["Main group"],
                          "status": "scheduled", "schedules": [{"start": "2026-10-14T18:30", "tz": "America/New_York", "rrule": "FREQ=WEEKLY;BYDAY=WE"}]}}),
    "PostPatch": None,  # filled below: PostInput properties plus edit scope
    "Send": obj({"post_id": I, "schedule_id": I, "occ": S, "at": {**S, "format": "date-time"}, "tz": S, "repeating": B, "rrule": S, "index": I, "edited": B,
                 "caption": S, "media": arr(I), "targets": arr(S), "status": S,
                 "delivery": {**S, "enum": ["", "sent", "partial", "failed", "blocked"]}, "sent": I, "failed": I, "blocked": I}),
    "SendRef": obj({"post_id": I, "schedule_id": I, "occ": S, "to": {**S, "description": "New local YYYY-MM-DDTHH:MM"}, "tz": {**S, "description": "Zone of to"},
                    "scope": {**S, "enum": ["one", "all"]}}, ["post_id", "schedule_id", "occ"]),
    "Delivery": obj({"jid": S, "state": {**S, "enum": ["sent", "failed", "blocked", "missed"]}, "error": S}),
    "Change": obj({"id": I, "at": I, "actor": {**S, "description": "you, sender, or api:<key name>"}, "summary": S, "undone": B, "undoable": B, "redoable": B}),
    "WriteResult": obj({"change": {**I, "description": "History id of this change; pass to POST /undo as expect_change. For undo/redo: the new history entry"},
                        "undo": S, "redo": S, "summary": S, "post": ref("Post"), "id": I,
                        "undid": {**I, "description": "Undo only: the change that was reverted"}, "redid": {**I, "description": "Redo only: the change that was re-applied"},
                        "notice": {**S, "description": "Create only: present when a post with a time was saved as a draft because no status was given"}}),
    "StatsSummary": obj({"from": S, "to": S, "days": I, "tz": S, "tiles": ref("StatTiles"), "previous": ref("StatTiles"), "member_growth": I,
        "daily": arr(obj({"day": S, "reach": I, "posts": I, "sent": I, "held": I, "failed": I, "missed": I, "members": I})),
        "speed": {"type": "object", "description": "Keys 1h, 6h, 24h, 7d", "additionalProperties": obj({"rate": N, "sends": I})},
        "heatmap": arr(arr(obj({"rate": N, "sends": I}))),
        "by_kind": arr(ref("StatGroup")), "by_tag": arr(ref("StatGroup")), "by_client": arr(ref("StatGroup")), "by_platform": arr(ref("StatGroup")),
        "chats": arr(obj({"jid": S, "name": S, "platform": S, "kind": S, "members": I, "sends": I, "reach": I, "read_rate": N, "reactions": I, "replies": I, "growth": I, "trend": arr(I)})),
        "emoji": arr(obj({"emoji": S, "count": I})), "insights": {"type": "object"}, "names": B, "tracking_since": I}),
    "StatTiles": obj({"posts": I, "sends": I, "on_time": N, "reach": I, "read_rate": N, "reactions": I, "replies": I, "forwards": I, "reactions_per_post": N, "replies_per_post": N}),
    "StatGroup": obj({"key": S, "label": S, "color": S, "sends": I, "read_rate": N, "reach": N, "reactions": N, "replies": N, "rate_sends": I}),
    "PostStats": obj({"sends": arr({"type": "object"}), "totals": {"type": "object", "additionalProperties": I}, "read_rate": N, "curve": arr(I),
        "emoji": {"type": "object", "additionalProperties": I}, "names": B,
        "people": arr(obj({"delivery_id": I, "who": S, "what": {**S, "enum": ["read", "played", "react", "reply"]}, "emoji": S, "at": I}))}),
    "APIKey": obj({"id": I, "name": S, "scope": {**S, "enum": ["read", "write", "admin"]}, "prefix": S, "created_at": I, "last_used_at": I, "revoked_at": I}),
}

schemas["PostPatch"] = obj({
    **schemas["PostInput"]["properties"],
    "scope": {**S, "enum": ["all", "one", "future"], "description": "Repeating posts: which sends to change. Default all"},
    "schedule_id": {**I, "description": "With scope one/future: the schedule of the send"},
    "occ": {**S, "description": "With scope one/future: the send's original local time (from GET /sends)"},
    "at": {**S, "description": "scope=one: new local time for this send"},
    "at_tz": {**S, "description": "Zone of at; default the app's zone"},
}, desc="Send only the fields to change. status: draft, scheduled (sends), paused or archived.")

def resp(schema, desc="OK"): return {"description": desc, "content": {"application/json": {"schema": schema}}}
ERR = {str(c): {"$ref": f"#/components/responses/E{c}"} for c in (400, 401, 403, 404, 422)}
def op(tag, summary, ok, body=None, params=None, desc=None, errs=(400, 401, 404), deprecated=None):
    """deprecated: None, or {"sunset": "2027-01-01", "successor": "/api/v1/..."}; the handler must use server.deprecated()."""
    o = {"tags": [tag], "summary": summary, "responses": {"200": ok, **{str(c): {"$ref": f"#/components/responses/E{c}"} for c in errs}}}
    if desc: o["description"] = desc
    if body: o["requestBody"] = {"required": True, "content": {"application/json": {"schema": body}}}
    if params: o["parameters"] = params
    if deprecated:
        o["deprecated"] = True
        o["description"] = (o.get("description", "") + f" Deprecated: use {deprecated['successor']} (sunset {deprecated['sunset']}).").strip()
    o["operationId"] = summary.lower().replace(" ", "_").replace("'", "").replace(",", "").replace("/", "_")
    return o
def q(name, schema=S, desc=None, req=False): return {"name": name, "in": "query", "required": req, "schema": schema, **({"description": desc} if desc else {})}
def p(name, schema=I): return {"name": name, "in": "path", "required": True, "schema": schema}
OKW = resp(ref("WriteResult"))

STATQ = [q("days", I, "7, 30 (default) or 90; any 1 to 366"), q("to", desc="Last day, YYYY-MM-DD (default today)"),
         q("platform", desc="Comma list: whatsapp,telegram"), q("tag", desc="Comma list of tag ids; 0 = no tag"),
         q("client", desc="Comma list of client ids; 0 = no client"), q("chat", desc="Comma list of chat jids")]

paths = {
    "/status": {"get": op("Status", "Get status", resp(ref("Status")), desc="Connection, safe mode, today's count, time zone, and your key's scope. Call this first.")},
    "/settings": {"get": op("Settings", "Get settings", resp(ref("Settings"))),
                  "patch": op("Settings", "Update settings", OKW, ref("Settings"), errs=(400, 401, 403))},
    "/targets": {"get": op("Targets", "List targets", {**resp(arr(ref("Target"))), "headers": {"X-Total-Count": {"description": "Matches before limit", "schema": I}}}, params=[
        q("q", desc="Name contains"), q("kind", desc="Comma list: self,status,announce,group,channel,community"), q("client_id"),
        q("allowed", desc="true/false"), q("can_send", desc="true to hide admin-only chats"), q("limit", I), q("include_gone")])},
    "/targets/refresh": {"post": op("Targets", "Refresh targets from WhatsApp", resp(arr(ref("Target"))), errs=(401, 503))},
    "/targets/{jid}": {"get": op("Targets", "Get target", resp(ref("Target")), params=[p("jid", S)]),
                       "patch": op("Targets", "Update target", OKW, ref("TargetPatch"), [p("jid", S)], "Set client (null clears), star, or allowlist (admin).", errs=(400, 401, 403, 404))},
    "/tags": {"get": op("Tags", "List tags", resp(arr(ref("Tag")))), "post": op("Tags", "Create tag", OKW, ref("TagInput"))},
    "/tags/{id}": {"patch": op("Tags", "Update tag", OKW, ref("TagInput"), [p("id")]), "delete": op("Tags", "Delete tag", OKW, params=[p("id")])},
    "/clients": {"get": op("Clients", "List clients", resp(arr(ref("Client")))), "post": op("Clients", "Create client", OKW, ref("ClientInput"), errs=(400, 401, 422))},
    "/clients/{id}": {"patch": op("Clients", "Update client", OKW, ref("ClientInput"), [p("id")], "Partial update, including per-client quiet hours.", errs=(400, 401, 404, 422)),
                      "delete": op("Clients", "Delete client", OKW, params=[p("id")])},
    "/sets": {"get": op("Sets", "List group sets", resp(arr(ref("Set")))), "post": op("Sets", "Create group set", OKW, ref("SetInput"))},
    "/sets/{id}": {"patch": op("Sets", "Update group set", OKW, ref("SetInput"), [p("id")]), "delete": op("Sets", "Delete group set", OKW, params=[p("id")])},
    "/media": {"post": {"tags": ["Media"], "summary": "Upload media", "operationId": "upload_media",
        "description": "Multipart field `file`, or JSON {\"url\": ..., \"kind\"?: image|video|voice|audio|document}. Converted for WhatsApp (ffmpeg).",
        "requestBody": {"required": True, "content": {
            "multipart/form-data": {"schema": obj({"file": {"type": "string", "format": "binary"}, "kind": S})},
            "application/json": {"schema": obj({"url": S, "kind": S, "name": S}, ["url"])}}},
        "responses": {"200": resp(ref("Media")), **{k: ERR[k] for k in ("400", "401", "422")}}}},
    "/media/{id}": {"get": op("Media", "Get media", resp(ref("Media")), params=[p("id")])},
    "/media/{id}/file": {"get": {"tags": ["Media"], "summary": "Download converted file", "operationId": "media_file", "parameters": [p("id")], "responses": {"200": {"description": "The file"}, "404": {"$ref": "#/components/responses/E404"}}}},
    "/media/{id}/preview": {"get": {"tags": ["Media"], "summary": "JPEG preview", "operationId": "media_preview", "parameters": [p("id")], "responses": {"200": {"description": "image/jpeg"}, "404": {"$ref": "#/components/responses/E404"}}}},
    "/posts": {"get": op("Posts", "List posts", resp(arr(ref("PostSummary"))), params=[
        q("status", desc="Comma list"), q("tag_id"), q("client_id"), q("target", desc="JID"), q("q", desc="Title or caption contains")]),
        "post": op("Posts", "Create post", OKW, ref("PostInput"), desc="Targets may be names. Posts are drafts unless status is \"scheduled\", even with a time (the response then includes a notice).", errs=(400, 401, 422))},
    "/posts/bulk": {"post": op("Posts", "Bulk update posts", OKW, ref("BulkInput"),
                               desc="One action on many posts, recorded as a single undoable change. tag/client: pass tag_id/client_id (null clears).", errs=(400, 401, 422))},
    "/posts/preview": {"post": op("Posts", "Preview post", resp(obj({"valid": B, "targets": arr(S), "next_sends": arr(ref("Send"))})), ref("PostInput"),
                                  desc="Validate, resolve targets and list the next sends. Saves nothing.", errs=(400, 401, 422))},
    "/posts/{id}": {"get": op("Posts", "Get post", resp(ref("Post")), params=[p("id")]),
                    "patch": op("Posts", "Update post", OKW, ref("PostPatch"), [p("id")], "Partial update. For repeating posts set scope (one/future/all).", errs=(400, 401, 404, 422)),
                    "delete": op("Posts", "Delete post", OKW, params=[p("id"), q("scope", {**S, "enum": ["all", "one", "future"]}), q("schedule_id", I), q("occ")],
                                 desc="scope=one skips a single send; future stops the series from occ.")},
    "/posts/{id}/sends": {"get": op("Posts", "List upcoming sends of a post", resp(arr(ref("Send"))), params=[p("id"), q("n", I)])},
    "/posts/{id}/send-now": {"post": op("Posts", "Send post now", OKW, params=[p("id")], desc="Adds a one-off send at the current minute; the send loop picks it up within 15 s. Safe mode still applies.")},
    "/posts/{id}/duplicate": {"post": op("Posts", "Duplicate post", OKW, params=[p("id")], desc="Creates a draft copy without times.")},
    "/posts/{id}/pause": {"post": op("Posts", "Pause post", OKW, params=[p("id")])},
    "/posts/{id}/resume": {"post": op("Posts", "Resume post", OKW, params=[p("id")])},
    "/sends": {"get": op("Sends", "List sends", resp(obj({"sends": arr(ref("Send")), "posts": {"type": "object", "additionalProperties": ref("Post")},
                                                       "media": {"type": "object", "additionalProperties": ref("Media")}})),
                         params=[q("from", desc="YYYY-MM-DD or RFC 3339"), q("to"), q("post_id")])},
    "/sends/deliveries": {"get": op("Sends", "List deliveries of a send", resp(arr(ref("Delivery"))), params=[q("schedule_id", I, req=True), q("occ", req=True)])},
    "/sends/move": {"post": op("Sends", "Move send", OKW, ref("SendRef"), desc="scope=one moves only this send; all shifts the whole series.")},
    "/sends/copy": {"post": op("Sends", "Copy send", OKW, ref("SendRef"), desc="Creates a new one-off post at `to`.")},
    "/sends/skip": {"post": op("Sends", "Skip send", OKW, ref("SendRef"))},
    "/changes": {"get": op("History", "List changes", resp(arr(ref("Change"))))},
    "/undo": {"post": op("History", "Undo", OKW, obj({"expect_change": {**I, "description": "Only undo if this change is still the latest"}}),
                         desc="Reverts the latest undoable change by anyone. Pass expect_change to be safe.", errs=(401, 409))},
    "/redo": {"post": op("History", "Redo", OKW, errs=(401, 409))},
    "/test-send": {"post": op("Posts", "Test send to yourself", resp(obj({"ok": B})), obj({"caption": S, "media": arr(I), "platform": {**S, "enum": ["whatsapp", "telegram"]}}),
                              desc="Sends now to your own chat only: WhatsApp Message yourself, or Telegram Saved Messages.", errs=(401, 503))},
    "/telegram": {"get": op("Telegram", "Get Telegram status", resp(ref("TelegramState")), errs=(401,))},
    "/telegram/login": {"post": op("Telegram", "Start Telegram QR login", resp(ref("TelegramState")),
                                   desc="Admin. Then show GET /telegram/qr.png and scan it in Telegram: Settings > Devices > Link Desktop Device.", errs=(401, 403, 409, 503))},
    "/telegram/qr.png": {"get": {"tags": ["Telegram"], "summary": "Telegram login QR code", "operationId": "telegram_qr",
                                 "description": "Admin. PNG of the current login QR. It refreshes about every 30 seconds while a login is running.",
                                 "responses": {"200": {"description": "image/png"}, "404": {"$ref": "#/components/responses/E404"}}}},
    "/telegram/password": {"post": op("Telegram", "Send Telegram two-step password", resp(ref("TelegramState")), obj({"password": S}, ["password"]), errs=(400, 401, 403, 409))},
    "/telegram/logout": {"post": op("Telegram", "Log out of Telegram", resp(ref("TelegramState")), errs=(401, 403, 409))},
    "/telegram/refresh": {"post": op("Telegram", "Refresh Telegram chats", resp(ref("TelegramState")), errs=(401, 503))},
    "/stats/summary": {"get": op("Stats", "Get engagement summary", resp(ref("StatsSummary")), params=STATQ,
        desc="Everything on the stats board: tiles (with the previous period for comparison), daily reach and delivery health, read speed, best-time heat map (weekday Monday=0 by hour), breakdowns by type, tag, client and app, chats with trends, top reactions, and Telegram's own channel insights. Counting starts with sends made after this feature shipped.", errs=(401,))},
    "/stats/summary.txt": {"get": {"tags": ["Stats"], "summary": "Get summary as text", "operationId": "stats_text", "parameters": STATQ,
        "description": "A short plain-text report (WhatsApp formatting) for copying or posting.", "responses": {"200": {"description": "text/plain"}}}},
    "/stats/posts/{id}": {"get": op("Stats", "Get post engagement", resp(ref("PostStats")), params=[p("id"), q("schedule_id", I, "One series"), q("occ", desc="One send (with schedule_id)")],
        desc="Per-chat numbers for a post's sends, cumulative reads per hour for 48 hours, reactions, and (when stats_people is on) who read, reacted or replied.", errs=(401, 404))},
    "/stats/badges": {"get": op("Stats", "Get calendar badges", resp({"type": "object", "additionalProperties": obj({"reach": I, "reactions": I, "replies": I, "members": I})}),
        params=[q("from", desc="YYYY-MM-DD"), q("to", desc="YYYY-MM-DD")], desc="Small numbers per send, keyed \"schedule_id|occ\".", errs=(401,))},
    "/stats/export.csv": {"get": {"tags": ["Stats"], "summary": "Download stats as CSV", "operationId": "stats_csv", "parameters": STATQ,
        "description": "One row per send.", "responses": {"200": {"description": "text/csv"}}}},
    "/stats/share": {"post": op("Stats", "Send summary to a chat", resp(obj({"ok": B, "sent_to": S})), obj({"to": {**S, "description": "\"me\" (default, your own chat) or a chat jid. Safe mode limits this to allowlisted chats."}}),
        params=STATQ, desc="Sends the text summary right away. Takes the same filters as /stats/summary in the query string.", errs=(400, 401, 403, 404))},
    "/update": {"get": op("System", "Get update status", resp(ref("UpdateState")), errs=(401,))},
    "/update/check": {"post": op("System", "Check for updates", resp(ref("UpdateState")), desc="Fetches and verifies the newest release's signed manifest. Downloads nothing.", errs=(401, 409))},
    "/update/install": {"post": op("System", "Install update and restart", resp(obj({"ok": B, "restarting": B, "version": S, "up_to_date": B, "current": S})),
        obj({"force": {**B, "description": "Restart even when a post is due within 15 minutes"}}),
        desc="Admin. Downloads the newest release, checks its signature and hash, and restarts into it. Refuses (409 busy) when a post is due within 15 minutes unless force is true, and (409 built_from_source) on builds from source.", errs=(401, 403, 409))},
    "/autostart": {"get": op("System", "Get start-at-login", resp(ref("Autostart")), errs=(401,)),
                   "put": op("System", "Set start-at-login", resp(ref("Autostart")), obj({"enabled": B}, ["enabled"]), desc="Admin. macOS LaunchAgent, Linux systemd user service, or Windows Run key. Takes effect at the next login.", errs=(400, 401, 403, 409))},
    "/quit": {"post": op("System", "Stop Townsquare", resp(obj({"ok": B})), desc="Admin. Stops the server. A service manager (launchd, systemd) may start it again.", errs=(401, 403))},
    "/config": {"get": op("System", "Get server settings", resp(ref("ServerConfig")), errs=(401,)),
                "patch": op("System", "Change server settings", resp(ref("ServerConfig")), obj({"listen": S, "tailscale": S}),
                    desc="Admin. Where the web app listens and its tailnet name (saved in config.json). Applies after POST /restart.", errs=(400, 401, 403, 422))},
    "/restart": {"post": op("System", "Restart Townsquare", resp(obj({"ok": B, "restarting": B})), obj({"force": B}),
        desc="Admin. Applies server settings or a new bot token. Refuses (409 busy) when a post is due within 15 minutes unless force is true.", errs=(401, 403, 409))},
    "/login-link": {"post": op("Keys", "Make a sign-in link for a person", resp(obj({"link": S, "expires_at": I})), obj({"base": {**S, "description": "Address people open Townsquare at (default: this request's)"}}),
        desc="Admin. A one-time link that signs a browser in to the web app. Works once, for 15 minutes.", errs=(400, 401, 403))},
    "/whatsapp/channels": {"post": op("WhatsApp", "Create a WhatsApp channel", resp(obj({"jid": S, "name": S})), obj({"name": S, "description": S}, ["name"]),
        desc="Admin. Creates a channel you own. It starts off the allowlist.", errs=(400, 401, 403, 502, 503))},
    "/telegram/bot": {"put": op("Telegram", "Set Telegram bot token", resp(obj({"ok": B, "restart_needed": B})), obj({"token": S}, ["token"]),
                                desc="Admin. Token from @BotFather. Starts the bot, or applies after POST /restart if one is running.", errs=(400, 401, 403, 409)),
                      "delete": op("Telegram", "Remove Telegram bot token", resp(obj({"ok": B, "restart_needed": B})), desc="Admin.", errs=(401, 403))},
    "/whatsapp": {"get": op("WhatsApp", "Get WhatsApp link status", resp(ref("WhatsAppState")), errs=(401,))},
    "/whatsapp/link": {"post": op("WhatsApp", "Start linking WhatsApp", resp(ref("WhatsAppState")), obj({"phone": {**S, "description": "Optional, with country code, for a typed pairing code"}}),
        desc="Admin. Then show GET /whatsapp/qr.png and scan it in WhatsApp: Settings > Linked devices > Link a device. Codes refresh for up to 10 minutes.", errs=(401, 403, 409))},
    "/whatsapp/qr.png": {"get": {"tags": ["WhatsApp"], "summary": "WhatsApp link QR code", "operationId": "whatsapp_qr",
        "description": "Admin. PNG of the current link QR while linking.", "responses": {"200": {"description": "image/png"}, "404": {"$ref": "#/components/responses/E404"}}}},
    "/whatsapp/logout": {"post": op("WhatsApp", "Unlink WhatsApp", resp(ref("WhatsAppState")), desc="Admin. Removes this linked device from your WhatsApp account.", errs=(401, 403, 409))},
    "/telegram/app": {"post": op("Telegram", "Use your own Telegram app id", resp(ref("TelegramState")), obj({"api_id": S, "api_hash": S}, ["api_id", "api_hash"]),
        desc="Admin. Optional: releases include Townsquare's shared app id (updated over the air). Saves your own api_id and api_hash from my.telegram.org instead. Log out of Telegram first. Telegram restarts to use it (the response is then {\"ok\": true, \"restarting\": true}).", errs=(400, 401, 403, 409)),
                      "delete": op("Telegram", "Use the shared Telegram app id", resp(ref("TelegramState")), desc="Admin. Removes your own app id. Log out of Telegram first.", errs=(401, 403, 409))},
    "/changelog": {"get": op("System", "Get release notes", resp(arr(ref("ChangelogEntry"))), params=[q("from", desc="Exclusive, e.g. v0.6.0"), q("to", desc="Inclusive"), q("limit", I)],
        desc="Sections of the CHANGELOG.md built into this version, newest first. Notes for a newer release are in GET /update → changes.", errs=(401,))},
    "/sessions": {"get": op("Keys", "List signed-in browsers", resp(arr(ref("Session"))), errs=(401, 403))},
    "/sessions/{id}": {"delete": op("Keys", "Sign out a browser", resp(obj({"ok": B})), params=[p("id")], errs=(401, 403, 404))},
    "/keys": {"get": op("Keys", "List API keys", resp(arr(ref("APIKey"))), errs=(401, 403)),
              "post": op("Keys", "Create API key", resp(obj({"key": ref("APIKey"), "secret": S, "note": S})), obj({"name": S, "scope": {**S, "enum": ["read", "write", "admin"]}}, ["name"]), errs=(401, 403, 422))},
    "/keys/{id}": {"delete": op("Keys", "Revoke API key", resp(obj({"ok": B})), params=[p("id")], errs=(401, 403, 404))},
}
for k in ("409",):
    pass

spec = {
    "openapi": "3.1.0",
    "info": {"title": "Townsquare API", "version": "1.0.0",
             "description": "Schedule WhatsApp posts to groups, communities, channels and Status. Everything the web app can do. "
                            "Read the agent guide at /api/v1/guide.md first. All changes are recorded in history and can be undone."},
    "servers": [{"url": "/api/v1"}],
    "security": [{"bearer": []}],
    "tags": [{"name": n, "description": d} for n, d in [
        ("Status", "Start here: connection, safe mode, your key's scope."),
        ("Posts", "Create, edit, schedule, pause, send now. Targets accept names."),
        ("Sends", "Individual occurrences on the calendar: list, move, copy, skip, deliveries."),
        ("Targets", "Groups, community announcement groups, channels, Status and yourself."),
        ("Media", "Upload by file or URL; converted for WhatsApp."),
        ("Tags", "Color labels for posts."),
        ("Clients", "Group posts and chats by client."),
        ("Sets", "Named lists of chats to add in one go."),
        ("History", "Who changed what; undo and redo."),
        ("Settings", "Time zone, safe mode, pacing, quiet hours."),
        ("Telegram", "Log in as your Telegram account and list your groups and channels."),
        ("Stats", "Engagement: reads, views, reactions, replies, shares and member growth for what Townsquare sent."),
        ("Keys", "API keys (admin scope)."),
        ("WhatsApp", "Link or unlink your WhatsApp account from the browser or an agent."),
        ("System", "Version, signed self-updates, start at login, and stopping the server.")]],
    "paths": paths,
    "components": {
        "securitySchemes": {"bearer": {"type": "http", "scheme": "bearer", "description": "API key from Settings → API keys (tsq_...)"}},
        "schemas": schemas,
        "responses": {f"E{c}": {"description": d, "content": {"application/json": {"schema": ref("Error")}}}
                      for c, d in [(400, "Bad request"), (401, "Missing or invalid API key"), (403, "Key scope too low"), (404, "Not found"),
                                   (409, "Conflict"), (422, "Invalid input (message explains what to fix)"), (503, "WhatsApp not connected")]},
    },
}
out = pathlib.Path(__file__).resolve().parent.parent / "internal/server/openapi.json"
out.write_text(json.dumps(spec, indent=1, ensure_ascii=False))
print("wrote", out, len(paths), "paths")
