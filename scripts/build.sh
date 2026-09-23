#!/usr/bin/env bash
# Full app build: frontend first (embedded via web/embed.go), then the Go
# binary. Without npm the binary still compiles, serving API-only with a
# 503 explanation at / until scripts/build-web.sh is run.
set -euo pipefail
cd "$(dirname "$0")/.."
if command -v npm >/dev/null 2>&1; then
  ./scripts/build-web.sh
else
  echo "warning: npm missing, skipping web build (API-only binary)" >&2
fi
go build -o dist/oplbm ./cmd/oplbm
