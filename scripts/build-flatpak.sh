#!/usr/bin/env bash
# Build Flatpak bundle locally (requires flatpak and flatpak-builder).
# Usage: ./scripts/build-flatpak.sh [--install]
set -euo pipefail
cd "$(dirname "$0")/.."

if ! command -v flatpak-builder >/dev/null 2>&1; then
  echo "flatpak-builder not found. Install: sudo apt install flatpak flatpak-builder" >&2
  exit 1
fi

MANIFEST="flatpak/io.github.JO0812.TinyPS2Manager.json"
if [ ! -f "$MANIFEST" ]; then
  echo "manifest $MANIFEST not found" >&2
  exit 1
fi

# Ensure web is built and licenses are generated (flatpak's dir source skips build/dist)
./scripts/build-web.sh
./scripts/gen-third-party.sh

echo "Building Flatpak (this may take a few minutes)..."
flatpak-builder --user --disable-rofiles-fuse --repo=repo --force-clean --ccache flatpak-build "$MANIFEST"

if [ "${1:-}" = "--install" ]; then
  echo "Installing to user..."
  flatpak --user install -y repo io.github.JO0812.TinyPS2Manager || true
  echo "Run: flatpak run io.github.JO0812.TinyPS2Manager"
else
  flatpak build-bundle repo io.github.JO0812.TinyPS2Manager.flatpak io.github.JO0812.TinyPS2Manager --runtime-repo=https://flathub.org/repo/flathub.flatpakrepo || true
  ls -lh io.github.JO0812.TinyPS2Manager.flatpak 2>/dev/null || echo "bundle at repo/ (use build-bundle to create .flatpak)"
  echo "To install: flatpak --user install repo io.github.JO0812.TinyPS2Manager"
fi
