#!/usr/bin/env bash
# Run the Aetra block-explorer data source (l1-explorer) against a node.
# See docs/explorer.md for the API it serves.
#
# Usage:
#   ./scripts/validator/explorer.sh [-r <rpc>] [-g <grpc>] [-l <listen>] \
#     [-s <start-height>] [-b <l1-explorer-binary>]
#
# Serve public explorer traffic from non-validator infrastructure, never from
# a validator node.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BINARY="$REPO_ROOT/build/l1-explorer"
RPC="http://127.0.0.1:26657"
GRPC="127.0.0.1:9090"
LISTEN="0.0.0.0:8080"
START="0"

while [[ $# -gt 0 ]]; do
  case "$1" in
    -r|--rpc) RPC="$2"; shift 2 ;;
    -g|--grpc) GRPC="$2"; shift 2 ;;
    -l|--listen) LISTEN="$2"; shift 2 ;;
    -s|--start-height) START="$2"; shift 2 ;;
    -b|--binary) BINARY="$2"; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 1 ;;
  esac
done

if [[ ! -x "$BINARY" ]]; then
  echo "l1-explorer binary not found at $BINARY" >&2
  echo "build it with: go build -o build/l1-explorer ./cmd/l1-explorer" >&2
  exit 1
fi

exec "$BINARY" -rpc "$RPC" -grpc "$GRPC" -listen "$LISTEN" -start-height "$START"
