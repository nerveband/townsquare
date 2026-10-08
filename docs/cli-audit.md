# Agent CLI Audit: townsquare

Checklist: [cli-best-practices](https://github.com/nerveband/cli-best-practices) `scorecards/agent-cli-audit.md`, version 3 (97 checks).
Binary tested: `go build ./cmd/townsquare` at the commit that added this file, against `townsquare serve --demo`.
Date: 2026-10-08
CLI Spec: conformant to v0.3 (`townsquare schema` validates against https://clispec.dev/schema/v0.3.json in CI, `tools/validate_clispec.py`; `townsquare schema --validate` checks the rules JSON Schema can't express).

Evidence is mechanical: `scripts/cli-e2e.sh` runs these behaviors against the real binary on macOS, Windows and Linux in CI (208 checks, including `--help` with examples for all 108 commands), and `go test ./internal/cli ./internal/server` covers the contract, vocabulary, docs and response shapes.

| Category | Score | Exemptions |
|----------|-------|------------|
| Discoverability | 7/7 | |
| Structured output | 6/6 | |
| Input flexibility | 5/5 | |
| Safety rails | 6/6 | |
| Error handling | 7/7 | |
| Context discipline | 5/5 | |
| Predictability | 7/7 | |
| Agent knowledge | 7/7 | |
| Resilience | 7/7 | 9.6 (declared: no durable jobs) |
| Distribution | 5/5 | |
| Three-layer introspection | 5/5 | |
| Persistent identity/config | 5/5 | |
| Two-way I/O/artifacts | 5/5 | 13.5 (declared: no upstream feedback endpoint) |
| Contract/generation discipline | 5/5 | 14.5 (declared: no MCP surface) |
| Unix composability/restraint | 5/5 | |
| API-native payload ergonomics | 5/5 | |
| Domain depth/proof gates | 5/5 | 17.1, 17.2 (declared: small local API, no cache layer) |
| **Total** | **97/97** | Agent-native band |

## Evidence by check

**1 Discoverability.** 1.1 `townsquare --help` lists every resource, setup command and local command with one-line descriptions. 1.2 every one of the 108 schema commands answers `--help` (cli-e2e loops over `schema --transform 'commands.#.name'`). 1.3/1.4 every command has examples with realistic values (`posts create --title 'Family dinner' --targets 'Main Group,Youth Group' --send-at 2026-10-15T18:30`); `schema --validate` fails on a command without examples or with one that doesn't parse. 1.5 `townsquare` → `townsquare posts` → `townsquare posts create --help`. 1.6 `townsquare schema` works with no key, config or network, is mentioned in root help, and validates against CLI Spec v0.3. 1.7 `townsquare version` (JSON when piped).

**2 Structured output.** 2.1 `-o json|jsonl|text|raw|auto`, explicit value always wins over TTY detection; `--json` alias. 2.2 lists are `{"items": [...], "total", "truncated"}` (+ `offset`, `limit`, `next_offset` when paged); single records are returned directly. 2.3 JSON when piped (declared `"output": {"tty": "text", "piped": "json"}`); no ANSI anywhere (cli-e2e checks). 2.4 with JSON output the error is one JSON line on stderr with `kind`, `message`, `hint`, `retryable`, `exit_code`, `details`. 2.5 distinct exit codes per kind (2 usage, 3 auth, 4 not found, 5 conflict, 6 confirmation, 7 network, 8 rate limit, 9 busy, 10 timeout, 11 uncertain outcome). 2.6 `--quiet`; stdout carries only data in every mode.

**3 Input flexibility.** 3.1 every input is a flag; nothing prompts except a confirmation on a TTY. 3.2 `--body -` reads JSON from stdin; `--body @file.json`. 3.3 keys come from `TOWNSQUARE_API_KEY`, stdin (`auth login < keyfile`) or a saved profile; there is no key flag; secret body fields warn when given on argv. 3.4 at most one positional (the id). 3.5 `--body` takes the exact API payload; flags override its fields.

**4 Safety rails.** 4.1/4.2 `--dry-run` on every remote command that isn't `read_only`, and on the local non-idempotent ones (`serve`, `pair`, `apikey create|revoke`, `login-link`, `auth local`, `feedback`). 4.3 dry runs state action, target, method and path, reversibility and `validated: server|local`; `posts create|update` dry runs are validated by `POST /posts/preview` and show resolved chats and next send times; deletes fetch the target first. 4.4 `--yes` (declared `confirmation_bypass_arg`); without a TTY a gated command exits 6 `confirmation_required` naming `--yes`. 4.5 idempotent commands converge: a second `posts pause` returns `"changed": false`, a second `delete` returns `{"changed": false, "status": "absent"}` with exit 0; every `non_idempotent` command declares `idempotency_key_arg` and the server honors `Idempotency-Key` (cli-e2e proves a replay creates nothing). 4.6 `effects` is declared per command in the contract (`x-cli`), never inferred.

**5 Error handling.** 5.1 errors say what's wrong and show a correct example. 5.2 missing values fail at once (`--status` alone: exit 2). 5.3 network errors exit 7, validation 2. 5.4 every kind carries a `hint` (auth: `townsquare auth status` / `auth login`). 5.5 errors on stderr only (cli-e2e checks both directions). 5.6 enum errors list valid values and the rejected one. 5.7 all flags, types, enums, ids and required fields are checked before any request.

**6 Context discipline.** 6.1 `--fields` (dotted paths). 6.2 `--limit`/`--offset` page on the server (`GET /posts`, `/targets`, `/changes`); partial results say `truncated: true` with `next_offset`. 6.3 `--id-only`. 6.4 `--count` uses `X-Total-Count` with `limit=0` on paged lists; other lists say `"counted": "client"`. 6.5 `--max-depth`.

**7 Predictability.** 7.1/7.5 resource + verb everywhere (`list`, `get`, `create`, `update`, `delete`, plus declared domain verbs). 7.2/7.6 one spelling per concept (`--output`, `--limit`, `--dry-run`, `--yes`, `--timeout`, `--profile`), declared in `global_args`. 7.3 output shapes are fixed and checked against the contract. 7.4 exit codes in `--help`, `schema`, `AGENTS.md`, `docs/cli.md`, SKILL.md. 7.7 `allowedVerbs`, `bannedWords` and `bannedFlags` are enforced by `schema --validate` in `go test` and CI.

**8 Agent knowledge.** 8.1/8.2 `AGENTS.md` section 4b (guardrails: dry run, `--yes`, idempotency keys, `--fields`). 8.3 workflow examples in SKILL.md (schedule, weekly post with photo, agenda and move, undo, stats). 8.4 common mistakes in AGENTS.md and SKILL.md "Do not". 8.5/8.6 `skills/townsquare/SKILL.md` (Agent Skills front matter, under 500 lines, trigger description, setup, safe defaults, "do not", untrusted content), shipped in the binary (`townsquare skills show`). 8.7 tests check every `townsquare ...` named in SKILL.md, README, AGENTS.md and the guides exists, and every SKILL.md command line parses.

**9 Resilience.** 9.1 `--timeout` → exit 10 with a hint. 9.2 `posts bulk` returns per-post `results` (`changed`, `unchanged`, `not_found`). 9.3 kinds declare `retryable`; 429 passes `retry_after_seconds` from `Retry-After`. 9.4 401 explains how to re-auth. 9.5 `--wait` on `whatsapp link`, `system update install`, `system restart`, bounded by `--timeout`, with partial state on timeout. 9.6 exemption declared in `schema` `extensions.async`: no durable jobs; the waiting commands are safe to re-run and return current state. 9.7 retry policy in `extensions.retries` and docs/cli.md.

**10 Distribution.** 10.1 one binary (Mac app, Windows exe, `.deb`, plain binaries). 10.2 `townsquare update [--check]` verifies an Ed25519 signature and SHA-256; the CLI never downloads by itself (declared in `extensions.updates`, including that a server on the same computer may). 10.3 the CLI warns on stderr when the server's `X-Townsquare-Version` differs; `doctor` checks it. 10.4 `auth login` (stdin), `auth status` (read-only), precedence documented in `context`, `schema` and docs. 10.5 `extensions.scope` (`remote`, `local`, `cli`) per command; list output and dry runs repeat `"scope"`.

**11 Three-layer introspection.** 11.1 progressive help. 11.2 `"clispec": "0.3"`. 11.3 commands, args, types, required, enums, output fields, examples, error kinds, effects, confirmation and idempotency args. 11.4 `--request-schema` / `--response-schema` print JSON Schema offline. 11.5 `townsquare skills list|show|path`.

**12 Persistent identity.** 12.1 profiles (`auth login --profile`, `profiles list|get|use|remove`). 12.2 precedence: flag > env > profile > default. 12.3 `profiles list -o json` (names, default, has_key; no secrets), separate from `schema`. 12.4 `townsquare context` lists each setting with its source. 12.5 keys live in `~/.config/townsquare/keys/` (600), never in `cli.json`; output shows `tsq_xxxx…` only.

**13 Two-way I/O.** 13.1 `--deliver stdout|file:PATH` on media, QR codes and CSV/text exports. 13.2 atomic writes; refuses to overwrite without `--overwrite`. 13.3 unknown schemes list `stdout, file:PATH`. 13.4 `townsquare feedback "..."` appends to a local JSONL log, no network. 13.5 declared: `extensions.feedback.upstream` is null.

**14 Contract discipline.** 14.1 one source: `tools/gen_openapi.py` → `internal/contract/openapi.json` (embedded) → server route and response tests, CLI commands, `schema`, `docs/cli.md`. 14.2 CI fails on drift: route coverage, response shapes (`TestResponsesMatchContract`), vocabulary, examples, docs, clispec validation, cli-e2e. 14.3 `openapi.json` carries `x-generated`; `docs/cli.md` starts with a generated notice; AGENTS.md says not to hand-edit. 14.4 scope in the contract (`extensions.scope`) and repeated in output. 14.5 declared: `extensions.mcp` (no MCP surface).

**15 Restraint.** 15.1 `-o text` tables. 15.2 JSON is the envelope plus bounded data; `--fields`/`--max-depth` cut it further. 15.3 deletes, sends, restarts and logouts need `--yes` without a TTY. 15.4 `--json` is an alias of `-o json`; help and schema show the canonical flag. 15.5 the skill teaches ordinary commands composed together.

**16 Payload ergonomics.** 16.1 commands mirror API resources (`posts`, `sends`, `targets`, `media`, ...), and each help shows its `METHOD /api/v1/path`. 16.2 `--format-error` is independent of `--output`. 16.3 `json`, `jsonl`, `raw` (exact API body), `text`. 16.4 `--transform items.0.id`, `items.#.title`. 16.5 `@file`, `@-`, `@base64:file`, `@@literal`; `media upload --file`.

**17 Domain depth.** 17.1/17.2 declared exemption (`extensions.data_layer`): Townsquare is itself a small local SQLite app, so there is nothing to cache or sync. 17.3 compound commands: `doctor` (server, key, versions, WhatsApp, Telegram, safe mode, updates, next hour) and `agenda` (sends joined with posts). 17.4 proof gates: `scripts/cli-e2e.sh` (CI, three OSes), `townsquare doctor`, `schema --validate`, contract tests. 17.5 provenance below.

## Provenance and coverage

Built to: the CLI Spec v0.3 (clispec.dev), clig.dev, the Agent Skills specification, and the
cli-best-practices patterns (structured output, safety rails, contract-first, self-update).

What schedulers and their CLIs/APIs offer, and where Townsquare stands:

| Workflow | Buffer / Hootsuite / Later (APIs) | Townsquare CLI |
|---|---|---|
| Create and schedule a post with media | yes | `media upload`, `posts create --status scheduled` |
| Preview before publishing | partial | `--dry-run` (server-validated: resolved chats, next send times) |
| Repeating posts | limited | `posts create --body` with `schedules[].rrule` |
| Calendar of what goes out next | yes (UI) | `agenda`, `sends list` |
| Move or skip one occurrence | rare | `sends move|copy|skip` |
| Undo | no | `history undo --expect-change` |
| Approval / safety gates | team approvals | safe mode + allowlist, quiet hours, `--yes`, idempotency keys |
| Analytics export | yes | `stats summary|post|export` |
| WhatsApp groups, communities, channels, Status | no | yes |
| Telegram groups, topics, channels, stories | no | yes |
| Runs on your own computer | no | yes |

## Highest-impact follow-ups

1. Add an MCP surface generated from the same contract, with a token budget check (14.5 would then be scored, not exempt).
2. Cursor-style paging for `history list` if history grows past thousands of entries.
3. Record CLI feedback upstream (opt-in) once there's a place to send it.
