# API deprecation policy

`/api/v1` is a contract with scripts and agents. Never break it in place.

- Additive changes (new endpoint, optional field, documented enum value) are fine.
- Breaking changes (remove or rename a field or route, change a type or meaning):
  1. Ship the replacement first.
  2. Wrap the old handler with `deprecated(h, sunset, successor)` (`internal/server/server.go`):
     sends `Deprecation: true`, `Sunset` and `Link: <successor>; rel="successor-version"`.
  3. Mark the operation `deprecated={...}` in `tools/gen_openapi.py`, naming the replacement.
  4. Note it under "Deprecated" in CHANGELOG.md and in `guide.md` if agents use it.
  5. Keep it working at least 60 days and one minor release, then remove it under "Removed".
- Many breaking changes at once: add `/api/v2` alongside v1 instead.
- Before v1.0 the owner may approve a breaking change in place (usually for safety). Record it as
  "Changed (breaking, approved pre-1.0 exception)" and update the guide and spec in the same
  change. Example: API posts default to draft (v0.6.0).
- UI routes (`/api/...`) are internal and may change freely, but keep sharing handlers with v1.
