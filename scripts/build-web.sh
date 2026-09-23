#!/usr/bin/env bash
# Installs web deps and builds the frontend (web/dist) for go:embed.
set -euo pipefail

cd "$(dirname "$0")/.."
cd web
if [ ! -d node_modules ]; then
  npm ci
fi
npm run build
# Vite empties dist (including the committed .gitkeep that keeps go:embed
# compiling on fresh clones): restore it so the tree stays clean.
touch dist/.gitkeep
