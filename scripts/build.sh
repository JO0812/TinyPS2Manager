#!/usr/bin/env bash
# Full app build: frontend first (embedded via web/embed.go), then the Go
# binary and, when Wails is available, the desktop bundle. Pure-Go
# cross-builds exclude the desktop tag so they stay CGO-free.
set -euo pipefail
cd "$(dirname "$0")/.."

if command -v npm >/dev/null 2>&1; then
  ./scripts/build-web.sh
else
  echo "warning: npm missing, skipping web build (API-only binary)" >&2
fi

# Regenerate third-party notices so every bundle ships current data.
./scripts/gen-third-party.sh

mkdir -p dist

# Headless binary always builds (no CGO, no Wails).
echo "building headless binary -> dist/oplbm"
go build -o dist/oplbm ./cmd/oplbm

# Desktop binary / bundle when possible (requires CGO + Wails).
# webkit2_41 is mandatory, not optional: without it Wails compiles the
# legacy assetserver variant whose request stubs drop HTTP methods and
# bodies, so every POST/PATCH/PUT from the UI fails with "Load failed"
# while GETs keep working (looks like a frozen app).
WAILS_TAGS="desktop webkit2_41"
if command -v wails >/dev/null 2>&1; then
  echo "wails found, building desktop bundle (build/bin/)..."
  if wails build -tags "$WAILS_TAGS" 2>&1; then
    echo "wails build OK -> build/bin/"
  else
    echo "wails build failed, falling back to go build -tags desktop -> dist/oplbm-desktop" >&2
    go build -tags desktop -o dist/oplbm-desktop ./cmd/oplbm 2>&1 | head -n 50 || true
  fi
else
  echo "wails CLI not installed (go install github.com/wailsapp/wails/v2/cmd/wails@latest); attempting desktop go build" >&2
  go build -tags desktop -o dist/oplbm-desktop ./cmd/oplbm 2>&1 | head -n 50 || {
    echo "desktop build needs CGO/webkit; headless dist/oplbm is usable (oplbm serve / CLI)" >&2
  }
fi

echo "outputs: dist/ (headless + licenses), build/bin/ (wails desktop when built)"
ls -lh dist/ 2>/dev/null || true
ls -lh build/bin/ 2>/dev/null || true
ls -lh THIRD_PARTY.md licenses/ 2>/dev/null | head -n 20 || true
