#!/usr/bin/env bash
# M2 end-to-end: serve the API, drive it with curl (destinations, import,
# enqueue incl. copy+convert+split, drain, artifacts, SSE smoke).
set -euo pipefail
cd "$(dirname "$0")/.."

PORT=41399
BASE="http://127.0.0.1:$PORT"
BIN=$(mktemp -d)/oplbm
FIX=$(mktemp -d)
DB=$(mktemp)
SET=$(mktemp)
DEV=$(mktemp -d)

cleanup() {
  kill "$SERVE" 2>/dev/null || true
  rm -rf "$BIN" "$FIX" "$DB" "$SET" "$DEV"
}
trap cleanup EXIT

echo "== build + fixtures =="
go build -o "$BIN" ./cmd/oplbm
go run ./testdata/gensrc --out "$FIX"

echo "== serve =="
"$BIN" serve --bind "127.0.0.1:$PORT" --db "$DB" --settings "$SET" >"$FIX/serve.log" 2>&1 &
SERVE=$!
for i in $(seq 1 150); do
  curl -sf "$BASE/api/library" >/dev/null && break
  sleep 0.1
  if ! kill -0 "$SERVE" 2>/dev/null; then echo "serve died:"; cat "$FIX/serve.log"; exit 1; fi
done

api() { # method path [json]
  curl -sf -X "$1" "$BASE$2" ${3:+-H 'Content-Type: application/json' -d "$3"}
}

echo "== destination =="
DEST=$(api POST /api/destinations "{\"path\": \"$DEV\"}" | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")
# FAT32 override: small DVDs still copy (split needs >4GiB real files;
# split-through-API is covered paused in integration tests, and 5GB demo
# writes would blow tmpfs).
api PATCH "/api/destinations/$DEST" '{"filesystemOverride": "fat32"}' >/dev/null

echo "== import =="
ITEMS=$(api POST /api/library/import "{\"path\": \"$FIX\"}")
IDS=$(echo "$ITEMS" | python3 -c "
import json,sys
want = {}
for it in json.load(sys.stdin):
    if it['title'] in ('ps2cd', 'game', 'ps2dvd'): want[it['title']] = it['id']
print(' '.join(str(want[k]) for k in ('ps2cd', 'game', 'ps2dvd')))")
echo "ids: $IDS"

echo "== enqueue (copy + convert; small DVDs stay copy) =="
# shellcheck disable=SC2086
ENQ=$(api POST /api/queue "{\"destinationId\": $DEST, \"itemIds\": [$(echo $IDS | tr ' ' ',')]}")
echo "$ENQ" | python3 -c "
import json,sys
kinds = sorted(j['kind'] for j in json.load(sys.stdin))
assert kinds == ['convert-and-copy', 'copy', 'copy'], kinds
print('kinds ok:', kinds)"

echo "== drain =="
for i in $(seq 1 600); do
  PENDING=$(api GET /api/queue | python3 -c "
import json,sys
jobs = json.load(sys.stdin)
print(sum(1 for j in jobs if j['status'] not in ('done',)))
if any(j['status'] == 'error' for j in jobs): print('ERRORS:', jobs); sys.exit(2)")
  [ "$PENDING" = "0" ] && break
  sleep 0.5
done
[ "$PENDING" = "0" ] || { echo "queue did not drain"; exit 1; }

echo "== artifacts =="
test -f "$DEV/CD/ps2cd.iso"
test -f "$DEV/DVD/ps2dvd.iso"
test -f "$DEV/POPS/game.VCD"
ls "$DEV"

echo "== SSE smoke =="
curl -sf -N --max-time 3 "$BASE/api/queue/events" | grep -q "^data:" && echo "events ok"

echo "M2 DEMO OK"
