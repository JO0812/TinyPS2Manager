#!/usr/bin/env bash
# M1 end-to-end demo on synthetic fixtures (no game data, nothing committed).
# Exercises every M1 subcommand against the real engines. Fails loudly.
set -euo pipefail
cd "$(dirname "$0")/.."

BIN=$(mktemp -d)/oplbm
FIX=$(mktemp -d)
DB=$(mktemp)
trap 'rm -rf "$BIN" "$FIX" "$DB"' EXIT

echo "== build =="
go build -o "$BIN" ./cmd/oplbm

echo "== fixtures =="
go run ./testdata/gensrc --out "$FIX"
find "$FIX" -type f | sort

echo "== import =="
"$BIN" import --db "$DB" "$FIX"

echo "== inspect (id 1) =="
"$BIN" inspect --db "$DB" 1

echo "== inspect (dvd path) =="
"$BIN" inspect --db "$DB" "$FIX/ps2dvd.iso"

echo "== convert (cue path) =="
"$BIN" convert --out "$FIX/out" --db "$DB" "$FIX/ps1/game.cue"

echo "== split (dvd iso) =="
"$BIN" split --out "$FIX/out" --name "Demo DVD" "$FIX/ps2dvd.iso"

echo "== tree (dry run) =="
cat > "$FIX/jobs.json" <<'EOF'
{"files": [
  {"bucket": "DVD", "subdir": "", "name": "Demo DVD.iso"},
  {"bucket": "CD", "subdir": "", "name": "Demo CD.iso"},
  {"bucket": "", "subdir": "", "name": "ul.cfg"},
  {"bucket": "POPS", "subdir": "", "name": "game.VCD"}
 ],
 "multidisc": [
  {"vcds": ["Demo Quest (Disc 1).VCD", "Demo Quest (Disc 2).VCD"],
   "vmcdir": "Demo Quest (Disc 1)"}
 ]}
EOF
"$BIN" tree --plan "$FIX/jobs.json" --dest "$FIX/dest" --prefix OPL

echo "M1 DEMO OK"
