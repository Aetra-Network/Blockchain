#!/usr/bin/env bash
# Stop an Aetra validator node started with scripts/validator/start.sh
# --daemon. Sends SIGTERM and waits, falling back to SIGKILL only if the
# process ignores the graceful shutdown window. See docs/VALIDATOR.md
# (Restart section) for the state that must survive a stop/start cycle.
#
# Usage: ./scripts/validator/stop.sh [-H <home>] [-t <timeout-seconds>]
set -euo pipefail

HOME_DIR="${HOME}/.aetra"
TIMEOUT=30

while [[ $# -gt 0 ]]; do
  case "$1" in
    -H|--home) HOME_DIR="$2"; shift 2 ;;
    -t|--timeout) TIMEOUT="$2"; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 1 ;;
  esac
done

PID_FILE="$HOME_DIR/aetrad.pid"
if [[ ! -f "$PID_FILE" ]]; then
  echo "no pid file at $PID_FILE; nothing to stop (if running under systemd, use systemctl stop instead)" >&2
  exit 1
fi

PID="$(cat "$PID_FILE")"
if ! kill -0 "$PID" 2>/dev/null; then
  echo "pid $PID from $PID_FILE is not running; removing stale pid file"
  rm -f "$PID_FILE"
  exit 0
fi

kill -TERM "$PID"
for _ in $(seq 1 "$TIMEOUT"); do
  if ! kill -0 "$PID" 2>/dev/null; then
    rm -f "$PID_FILE"
    echo "stopped pid $PID"
    exit 0
  fi
  sleep 1
done

echo "pid $PID did not exit within ${TIMEOUT}s, sending SIGKILL" >&2
kill -KILL "$PID" 2>/dev/null || true
rm -f "$PID_FILE"
