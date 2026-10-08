#!/usr/bin/env python3
"""Check rule 1 of AGENTS.md: every /api/v1 operation has a web app route too.

A v1 route passes when the web app (Handler() in server.go) mounts the same
handler, or the web app does the same thing through a listed counterpart.
Exit 1 lists anything missing.
"""
import re, sys

v1 = open("internal/server/v1.go").read()
web = open("internal/server/server.go").read()
route = re.compile(r'HandleFunc\("(\w+) (/api/[^"]*)",\s*(?:s\.)?([A-Za-z0-9_]+)')
v1_routes = [(m, p, h) for m, p, h in route.findall(v1) if p.startswith("/api/v1")]
web_handlers = {h for _, p, h in route.findall(web) if not p.startswith("/api/v1")}

# v1 handlers whose web app counterpart has another name (same store/server logic underneath),
# or that only make sense for agents. Keep reasons short and true.
COUNTERPART = {
    "v1Index": "agent entry point", "func": "inline docs/spec/guide/settings handlers",
    "v1Status": "web app uses /api/state", "v1Targets": "web uses /api/targets", "v1Target": "web uses /api/targets",
    "v1ListNamed": "web gets tags/clients/sets in /api/state", "v1PatchNamed": "web uses PUT saveNamed",
    "v1SaveSet": "web uses saveSet", "v1CreatePost": "web uses createPost", "v1Preview": "web uses /api/preview",
    "v1PatchPost": "web uses updatePost", "v1SetStatus": "web pauses/resumes via updatePost",
    "v1Sends": "web uses /api/sends", "v1Changes": "web uses /api/changes", "v1Undo": "web uses /api/undo",
    "v1TestSend": "web uses /api/test", "v1Keys": "web uses /api/keys",
}
missing = [(m, p, h) for m, p, h in v1_routes if h not in web_handlers and h not in COUNTERPART]
for m, p, h in missing:
    print(f"  {m} {p} -> s.{h} has no web app route")
if missing:
    sys.exit(1)
print(f"  {len(v1_routes)} v1 routes checked")
