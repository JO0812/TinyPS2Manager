#!/usr/bin/env bash
# Installs web deps and builds the frontend (web/dist) for go:embed.
set -euo pipefail

cd "$(dirname "$0")/.."
cd web
if [ ! -d node_modules ]; then
  npm ci
fi
npm run build
# Vite empties dist (including the committed README.md placeholder that
# keeps go:embed compiling on fresh clones — Go ignores dotfiles, so
# .gitkeep cannot work): rewrite it byte-identical so the tree stays clean.
cat > dist/README.md <<'EOF'
# Build output of `scripts/build-web.sh` (Svelte/Vite → static files served
# embedded by the Go binary). This placeholder keeps `go:embed web/dist`
# compiling on fresh clones (Go ignores dotfiles, so .gitkeep cannot work);
# build-web.sh rewrites it after every build since Vite empties this dir.
EOF
