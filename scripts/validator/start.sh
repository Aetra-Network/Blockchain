#!/usr/bin/env bash
# Start an Aetra validator node. See docs/VALIDATOR.md.
#
# Usage:
#   ./scripts/validator/start.sh [-H <home>] [-b <aetrad-binary>] [--daemon] \
#     [--metrics] [--metrics-addr <addr>]
#
# Without --daemon, runs in the foreground (Ctrl-C to stop). With --daemon,
# detaches into the background, writes logs to <home>/aetrad.log, and
# records the PID in <home>/aetrad.pid for scripts/validator/stop.sh and
# scripts/validator/health.sh.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BINARY="$REPO_ROOT/build/aetrad"
HOME_DIR="${HOME}/.aetra"
DAEMON=false
METRICS=false
METRICS_ADDR="0.0.0.0:27780"

while [[ $# -gt 0 ]]; do
  case "$1" in
    -H|--home) HOME_DIR="$2"; shift 2 ;;
    -b|--binary) BINARY="$2"; shift 2 ;;
    --daemon) DAEMON=true; shift ;;
    --metrics) METRICS=true; shift ;;
    --metrics-addr) METRICS_ADDR="$2"; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 1 ;;
  esac
done

if [[ ! -x "$BINARY" ]]; then
  echo "aetrad binary not found or not executable: $BINARY" >&2
  exit 1
fi
if [[ ! -d "$HOME_DIR/config" ]]; then
  echo "no node home at $HOME_DIR (run scripts/validator/init.sh first)" >&2
  exit 1
fi

ARGS=(start --home "$HOME_DIR")
if [[ "$METRICS" == true ]]; then
  ARGS+=(--observability-metrics true --observability-metrics-addr "$METRICS_ADDR")
fi

if [[ "$DAEMON" != true ]]; then
  exec "$BINARY" "${ARGS[@]}"
fi

PID_FILE="$HOME_DIR/aetrad.pid"
LOG_FILE="$HOME_DIR/aetrad.log"
if [[ -f "$PID_FILE" ]] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
  echo "already running with pid $(cat "$PID_FILE") (see $PID_FILE)" >&2
  exit 1
fi

nohup "$BINARY" "${ARGS[@]}" >>"$LOG_FILE" 2>&1 &
echo $! > "$PID_FILE"
echo "started aetrad (pid $(cat "$PID_FILE")), logging to $LOG_FILE"
