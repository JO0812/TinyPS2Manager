#!/usr/bin/env bash
# Full demo + behavioral bootup: CLI paths (M1), API paths (M2), and
# browser-driven UI flows (M3). UI flows need node + a Chromium binary
# (CHROMIUM_BIN, default /usr/bin/chromium); they skip gracefully without.
set -euo pipefail
cd "$(dirname "$0")/.."

echo "### M1 CLI demo ###"
./scripts/m1-demo.sh

echo "### M2 API demo ###"
./scripts/m2-demo.sh

echo "### M3 UI flows ###"
if ! command -v node >/dev/null 2>&1; then
  echo "skip: node missing"
  exit 0
fi
if [ ! -x "${CHROMIUM_BIN:-/usr/bin/chromium}" ]; then
  echo "skip: no Chromium binary (set CHROMIUM_BIN)"
  exit 0
fi
if [ ! -d scripts/uiflow/node_modules ]; then
  (cd scripts/uiflow && npm ci)
fi

PORT=41420
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

go build -o "$BIN" ./cmd/oplbm
go run ./testdata/gensrc --out "$FIX" >/dev/null
"$BIN" serve --bind "127.0.0.1:$PORT" --db "$DB" --settings "$SET" >/dev/null 2>&1 &
SERVE=$!
for i in $(seq 1 150); do
  curl -sf "$BASE/api/library" >/dev/null && break
  sleep 0.1
done

BASE="$BASE" FIX="$FIX" DEV="$DEV" node scripts/uiflow/flow.js
echo "E2E OK"
