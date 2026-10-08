#!/usr/bin/env bash
# Proof of behavior for the agent CLI: run the real binary against a demo server
# and check every promise the schema makes (output, errors and exit codes,
# safety rails, paging, idempotency, delivery, profiles). Runs in CI on macOS,
# Windows and Linux. Usage: scripts/cli-e2e.sh [path-to-townsquare-binary]
set -uo pipefail
cd "$(dirname "$0")/.."
BIN="${1:-}"
WORK="$(mktemp -d)"
if [ -z "$BIN" ]; then
  BIN="$WORK/townsquare"; [ "${OS:-}" = Windows_NT ] && BIN="$BIN.exe"
  go build -o "$BIN" ./cmd/townsquare || exit 1
fi
PORT="${TS_E2E_PORT:-8897}"
NWORK="$WORK"; command -v cygpath >/dev/null 2>&1 && NWORK="$(cygpath -m "$WORK")"  # native path for file: values on Windows
export TOWNSQUARE_CONFIG_DIR="$WORK/cfg" TOWNSQUARE_URL="" TOWNSQUARE_API_KEY="" TOWNSQUARE_PROFILE="" TOWNSQUARE_OUTPUT=""
pass=0; failed=0
ok()   { pass=$((pass+1)); }
bad()  { failed=$((failed+1)); echo "✗ $*"; }
# expect CODE DESCRIPTION -- command...   (stdout in $OUT, stderr in $ERR)
expect() {
  local want="$1" what="$2"; shift 3
  "$@" >"$WORK/out" 2>"$WORK/err" </dev/null; local got=$?
  OUT="$(cat "$WORK/out")"; ERR="$(cat "$WORK/err")"
  if [ "$got" = "$want" ]; then ok; else bad "$what: exit $got, want $want: $(tail -c 300 "$WORK/err")"; fi
}
has() { case "$1" in *"$2"*) ok ;; *) bad "$3: missing $2 in: ${1:0:300}" ;; esac; }

"$BIN" --data "$WORK/data" serve --demo --listen "127.0.0.1:$PORT" >"$WORK/serve.log" 2>&1 &
SERVER=$!
trap 'kill $SERVER 2>/dev/null; rm -rf "$WORK"' EXIT
for i in $(seq 1 150); do curl -fs -o /dev/null "http://127.0.0.1:$PORT/api/auth/status" && break; sleep 1; done
T="$BIN"

# Discoverability and contract (no server or key needed).
expect 0 "root help" -- "$T" --help; has "$OUT" "schema" "root help mentions schema"
expect 0 "group help" -- "$T" posts; has "$OUT" "posts create" "group lists verbs"
expect 0 "command help" -- "$T" posts create --help; has "$OUT" "Examples:" "help has examples"
expect 0 "schema" -- "$T" schema; has "$OUT" '"clispec":"0.3"' "schema version"
expect 0 "schema narrows" -- "$T" schema posts list; has "$OUT" '"posts list"' "narrowed schema"
expect 0 "schema validates" -- "$T" schema --validate; has "$OUT" '"valid":true' "schema --validate"
expect 0 "request schema" -- "$T" posts create --request-schema; has "$OUT" '"properties"' "request schema"
expect 0 "version" -- "$T" version; has "$OUT" '"version"' "version is JSON when piped"
expect 0 "skill" -- "$T" skills show; has "$OUT" "name: townsquare" "skill text"
# Every command in the schema answers --help with examples.
"$T" schema --transform 'commands.#.name' | tr -d '[]"' | tr ',' '\n' > "$WORK/names"
n=0; while IFS= read -r name; do
  [ -z "$name" ] && continue; n=$((n+1))
  # shellcheck disable=SC2086
  "$T" $name --help >"$WORK/h" 2>&1 </dev/null && grep -qi "example" "$WORK/h" && ok || bad "$name --help"
done < "$WORK/names"
[ "$n" -gt 100 ] && ok || bad "only $n commands in the schema"
expect 0 "local dry-run" -- "$T" --data "$WORK/data" serve --dry-run; has "$OUT" '"dry_run":true' "serve dry run"
expect 0 "apikey dry-run" -- "$T" --data "$WORK/data" apikey create x --dry-run; has "$OUT" '"validated":"local"' "apikey dry run"

# Auth: no key, then a key from stdin (never argv).
expect 3 "no key" -- "$T" --url "http://127.0.0.1:$PORT" status get; has "$ERR" '"kind":"auth"' "auth error envelope"
"$T" --data "$WORK/data-demo" apikey create e2e --scope admin -o json >"$WORK/key.json" 2>/dev/null
SECRET="$(sed -E 's/.*"secret":"([^"]+)".*/\1/' "$WORK/key.json")"
printf '%s' "$SECRET" > "$WORK/key"
expect 0 "auth login" -- sh -c "\"$T\" auth login --url http://127.0.0.1:$PORT --profile demo < \"$WORK/key\""
expect 0 "auth status" -- "$T" auth status; has "$OUT" '"scope":"admin"' "auth status scope"
case "$(cat "$TOWNSQUARE_CONFIG_DIR/cli.json")" in *tsq_*) bad "key stored in cli.json" ;; *) ok ;; esac
expect 0 "context" -- "$T" context; has "$OUT" '"source":"profile"' "context shows sources"
expect 0 "profiles" -- "$T" profiles list; has "$OUT" '"has_key":true' "profiles list"
TOWNSQUARE_API_KEY=tsq_wrong expect 3 "bad key" -- "$T" status get
expect 0 "doctor runs" -- sh -c "\"$T\" doctor >/dev/null; rc=\$?; [ \$rc = 0 ] || [ \$rc = 21 ]"

# Reads, envelopes, paging, fields.
expect 0 "list" -- "$T" posts list --limit 2 --fields id,title; has "$OUT" '"items":[' "envelope"; has "$OUT" '"truncated":true' "truncation flag"
expect 0 "page 2" -- "$T" posts list --limit 2 --offset 2 --id-only; has "$OUT" '"offset":2' "offset"
expect 0 "count" -- "$T" posts list --count; has "$OUT" '"count":' "count"
expect 0 "transform" -- "$T" posts list --limit 1 --transform items.0.id
expect 0 "jsonl" -- "$T" -o jsonl targets list --limit 3 --id-only
expect 0 "text" -- "$T" -o text tags list; has "$OUT" "NAME" "text table"
expect 0 "text columns" -- "$T" -o text posts list --limit 2 --fields id,next_at; has "$OUT" "NEXT_AT" "--fields picks text columns"
case "$OUT" in *$'\033'*) bad "ANSI in output" ;; *) ok ;; esac
expect 0 "raw" -- "$T" -o raw status get; has "$OUT" '"safe_mode"' "raw passthrough"
expect 0 "max depth" -- "$T" posts get 2 --max-depth 1; has "$OUT" "items]" "depth collapsed"

# Errors and exit codes.
expect 2 "unknown command" -- "$T" postz list
expect 2 "unknown flag" -- "$T" posts list --stauts draft; has "$ERR" "Did you mean --status" "flag suggestion"
expect 2 "enum" -- "$T" targets list --kind bogus; has "$ERR" "group" "enum lists valid values"
expect 2 "bad id" -- "$T" posts get abc
expect 4 "not found" -- "$T" posts get 99999
expect 5 "conflict" -- "$T" history redo
expect 7 "network" -- "$T" --url http://127.0.0.1:1 status get; has "$ERR" '"retryable":true' "network retryable"
expect 10 "timeout" -- "$T" --url http://10.255.255.1:9 --timeout 300ms status get
expect 2 "missing value" -- "$T" posts list --status
"$T" postz list >/dev/null 2>"$WORK/err2"; [ -s "$WORK/err2" ] && ok || bad "errors on stderr"
"$T" postz list 2>/dev/null | grep -q . && bad "error leaked to stdout" || ok

# Safety rails.
expect 0 "dry-run create" -- "$T" posts create --title E2E --caption hi --targets "Main Group" --send-at 2030-01-02T09:00 --dry-run
has "$OUT" '"validated":"server"' "server-validated dry run"; has "$OUT" '"next_sends"' "dry run preview"
expect 0 "dry-run transform" -- "$T" posts create --title E2E --caption hi --targets "Main Group" --send-at 2030-01-02T09:00 --dry-run --transform validated; has "$OUT" 'server' "--transform on dry runs"
BEFORE="$("$T" posts list --count --transform count)"
expect 0 "create" -- "$T" posts create --title E2E --caption hi --targets "Main Group" --idempotency-key e2e-1 --transform post.id
ID="$OUT"
expect 0 "replay" -- "$T" posts create --title E2E --caption hi --targets "Main Group" --idempotency-key e2e-1 --transform post.id
[ "$OUT" = "$ID" ] && ok || bad "idempotency replay returned $OUT, want $ID"; has "$ERR" "replayed" "replay note"
AFTER="$("$T" posts list --count --transform count)"
[ "$AFTER" = "$((BEFORE+1))" ] && ok || bad "idempotent create made $((AFTER-BEFORE)) posts"
expect 6 "confirmation without tty" -- "$T" posts delete "$ID"; has "$ERR" '"kind":"confirmation_required"' "confirmation kind"
expect 0 "delete dry-run" -- "$T" posts delete "$ID" --dry-run; has "$OUT" '"target"' "dry run names target"
expect 0 "delete" -- "$T" posts delete "$ID" --yes
expect 0 "delete again" -- "$T" posts delete "$ID" --yes; has "$OUT" '"changed":false' "idempotent delete"
expect 0 "pause" -- "$T" posts pause 1 --fields changed; has "$OUT" '"changed":true' "first pause changes"
expect 0 "pause again" -- "$T" posts pause 1 --fields changed; has "$OUT" '"changed":false' "second pause is a no-op"
expect 0 "resume" -- "$T" posts resume 1
expect 0 "bulk" -- "$T" posts bulk --body '{"ids":[1,99999],"action":"pause"}' --yes; has "$OUT" '"not_found"' "per-item bulk results"
expect 0 "resume again" -- "$T" posts resume 1

# Taking posts back and the undo-send pause.
expect 0 "pending" -- "$T" sends pending; has "$OUT" '"delay_seconds"' "pending sends"
expect 6 "unsend needs --yes" -- "$T" sends unsend --post-id 2 --schedule-id 2 --occ 2026-10-07T18:30
expect 0 "undo-send setting" -- "$T" settings update --send-delay 60
expect 2 "undo-send values" -- "$T" settings update --send-delay 7
expect 0 "undo-send off" -- "$T" settings update --send-delay 0

# Files (artifacts) and uploads.
expect 0 "deliver file" -- "$T" stats export --days 30 --deliver "file:$NWORK/s.csv"; [ -s "$WORK/s.csv" ] && ok || bad "csv not written"
expect 5 "no overwrite" -- "$T" stats export --days 30 --deliver "file:$NWORK/s.csv"
expect 2 "bad scheme" -- "$T" stats export --deliver "ftp:x"; has "$ERR" "file:PATH" "schemes listed"
printf 'hello' > "$WORK/doc.txt"
expect 0 "upload" -- "$T" media upload --file "$WORK/doc.txt" --kind document --idempotency-key e2e-up; has "$OUT" '"id"' "upload id"

# Local commands and feedback.
expect 0 "feedback" -- "$T" feedback "e2e note" --command "posts list"
expect 0 "feedback list" -- "$T" feedback list; has "$OUT" "e2e note" "feedback saved"
expect 0 "local help" -- "$T" due --help; has "$OUT" "Examples:" "local help"

echo "cli-e2e: $pass passed, $failed failed"
[ "$failed" = 0 ]
