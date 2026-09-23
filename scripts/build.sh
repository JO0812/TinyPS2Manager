#!/usr/bin/env bash
# Builds the Wails app for the host OS. Requires wails CLI installed.
# (Phase 0 stub: once Wails is wired this becomes `wails build`.)
set -euo pipefail
cd "$(dirname "$0")/.."
go build -o dist/oplbm ./cmd/oplbm