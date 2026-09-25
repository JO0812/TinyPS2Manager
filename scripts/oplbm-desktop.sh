#!/usr/bin/env bash
# Desktop launcher for OPL Backup Manager (used by the .desktop entry).
# The installed binary is the headless build, so launching means: reuse a
# running `oplbm serve` if there is one, otherwise start it, wait for the
# API, then open the UI in the default browser.
set -u
BIND="${OPLBM_BIND:-127.0.0.1:41337}"
BASE="http://$BIND"
BIN="${OPLBM_BIN:-$HOME/.local/bin/oplbm}"
BROWSER="${BROWSER:-xdg-open}"
LOG="$HOME/.oplbm/logs/desktop-serve.log"

if ! curl -sf "$BASE/api/library" >/dev/null 2>&1; then
  mkdir -p "$(dirname "$LOG")"
  setsid "$BIN" serve --bind "$BIND" >>"$LOG" 2>&1 < /dev/null &
  ok=0
  for _ in $(seq 1 150); do
    if curl -sf "$BASE/api/library" >/dev/null 2>&1; then ok=1; break; fi
    sleep 0.1
  done
  if [ "$ok" != 1 ]; then
    msg="OPL Backup Manager failed to start (see $LOG)"
    if command -v notify-send >/dev/null 2>&1; then
      notify-send "OPL Backup Manager" "$msg"
    else
      echo "$msg" >&2
    fi
    exit 1
  fi
fi
"$BROWSER" "$BASE/"
