#!/usr/bin/env bash
# Full UX walkthrough: boots the real app, drives every view with Chromium
# (Puppeteer) and asserts outcomes incl. device artifacts. Needs node +
# Chromium (CHROMIUM_BIN, default /usr/bin/chromium); network is used for
# the RiptOPL step and skipped gracefully when unreachable.
set -euo pipefail
cd "$(dirname "$0")/.."

PORT=41431
BASE="http://127.0.0.1:$PORT"
BIN=$(mktemp -d)/oplbm
FIX=$(mktemp -d)
SRC=$(mktemp -d)
DEV=$(mktemp -d)
DB=$(mktemp)
SET=$(mktemp)
SHOTS=$(mktemp -d)
cleanup() {
  kill "$SERVE" 2>/dev/null || true
  rm -rf "$BIN" "$FIX" "$SRC" "$DEV" "$DB" "$SET"
}
trap cleanup EXIT

echo "### build + fixtures ###"
./scripts/build-web.sh
go build -o "$BIN" .
go run ./testdata/gensrc --out "$FIX"
# Synthetic PS2 ISOs carrying SYSTEM.CNF/BOOT2 serials (certain + mod) and
# a sparse >2 GiB BIN/CUE pair for the POPSTARTER-ceiling warning.
python3 - "$FIX" <<'EOF'
import struct, sys
out = sys.argv[1]
def make_iso(path, cnf):
    sec = 2048
    img = bytearray(32 * sec)
    pvd = memoryview(img)[16*sec:17*sec]
    pvd[0], pvd[6] = 1, 1
    pvd[1:6] = b'CD001'
    rec = pvd[156:156+34]
    rec[0], rec[1], rec[25] = 34, 0, 2
    struct.pack_into('<I', rec, 2, 20)
    struct.pack_into('>I', rec, 6, 20)
    struct.pack_into('<I', rec, 10, sec)
    struct.pack_into('>I', rec, 14, sec)
    rec[32], rec[33] = 1, 0
    d = memoryview(img)[20*sec:21*sec]
    def entry(off, extent, size, flags, ident):
        rl = 33 + len(ident)
        if rl % 2: rl += 1
        r = d[off:off+rl]
        r[0], r[1], r[25] = rl, 0, flags
        struct.pack_into('<I', r, 2, extent)
        struct.pack_into('>I', r, 6, extent)
        struct.pack_into('<I', r, 10, size)
        struct.pack_into('>I', r, 14, size)
        r[32] = len(ident)
        r[33:33+len(ident)] = ident
        return off + rl
    off = entry(0, 20, sec, 2, b'\x00')
    off = entry(off, 20, sec, 2, b'\x01')
    entry(off, 21, len(cnf), 0, b'SYSTEM.CNF;1')
    img[21*sec:21*sec+len(cnf)] = cnf
    open(path, 'wb').write(img)
make_iso(f'{out}/Tour Racer (USA).iso',
         b'BOOT = cdrom0:\\SLUS_213.85;1\nBOOT2 = cdrom0:\\SLUS_213.85;1\nVER = 1.00\n')
make_iso(f'{out}/Tour Mod (Europe).iso',
         b'BOOT = cdrom0:\\SLES_512.30;1\nBOOT2 = cdrom0:\\SLES_512.30;1\nHACK = fan translation v1.2\n')
n = (2*1024**3)//2352 + 1
with open(f'{out}/big.bin', 'wb') as f:
    f.truncate(n * 2352)
open(f'{out}/big.cue', 'w').write('FILE "big.bin" BINARY\n  TRACK 01 MODE2/2352\n    INDEX 01 00:00:00\n')
print('tour fixtures written')
EOF

echo "### serve ###"
"$BIN" serve --bind "127.0.0.1:$PORT" --db "$DB" --settings "$SET" >"$FIX/serve.log" 2>&1 &
SERVE=$!
for i in $(seq 1 150); do
  curl -sf "$BASE/api/library" >/dev/null && break
  sleep 0.1
  if ! kill -0 "$SERVE" 2>/dev/null; then echo "serve died:"; cat "$FIX/serve.log"; exit 1; fi
done

echo "### UX tour (shots in $SHOTS) ###"
if [ ! -d scripts/uiflow/node_modules ]; then
  (cd scripts/uiflow && npm ci)
fi
BASE="$BASE" FIX="$FIX" SRC="$SRC" DEV="$DEV" SHOTS="$SHOTS" node scripts/uiflow/ux-tour.js
echo "UX TOUR OK (shots kept in $SHOTS)"
trap - EXIT
