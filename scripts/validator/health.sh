#!/usr/bin/env bash
# Report the minimum health signals from docs/VALIDATOR.md's Monitor
# section: sync status, latest block height, peer count, and (if enabled)
# metrics endpoint reachability. Exits non-zero if the node is unreachable
# or still catching up, so it can be used as a process-manager health check.
#
# Usage: ./scripts/validator/health.sh [-n <rpc-node>] [-m <metrics-addr>]
set -euo pipefail

RPC_NODE="http://127.0.0.1:26657"
METRICS_ADDR=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    -n|--node) RPC_NODE="$2"; shift 2 ;;
    -m|--metrics) METRICS_ADDR="$2"; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 1 ;;
  esac
done

status="$(curl -fsS "$RPC_NODE/status" 2>/dev/null)" || {
  echo "UNREACHABLE: could not reach $RPC_NODE/status" >&2
  exit 1
}

extract() {
  # Minimal JSON field extraction (no hard jq dependency); falls back to jq
  # if present for exact parsing.
  if command -v jq >/dev/null 2>&1; then
    echo "$status" | jq -r "$1"
  else
    echo "$status" | grep -o "\"${2}\":\"\{0,1\}[^,\"}]*\"\{0,1\}" | head -1 | sed -E 's/.*:"?([^",}]*)"?/\1/'
  fi
}

catching_up="$(extract '.result.sync_info.catching_up' catching_up)"
latest_height="$(extract '.result.sync_info.latest_block_height' latest_block_height)"
latest_time="$(extract '.result.sync_info.latest_block_time' latest_block_time)"
node_id="$(extract '.result.node_info.id' id)"

net_info="$(curl -fsS "$RPC_NODE/net_info" 2>/dev/null || echo '{}')"
if command -v jq >/dev/null 2>&1; then
  peer_count="$(echo "$net_info" | jq -r '.result.n_peers // "unknown"')"
else
  peer_count="unknown"
fi

echo "node_id:       $node_id"
echo "latest_height: $latest_height"
echo "latest_time:   $latest_time"
echo "catching_up:   $catching_up"
echo "peer_count:    $peer_count"

if [[ -n "$METRICS_ADDR" ]]; then
  if curl -fsS "http://$METRICS_ADDR/metrics" -o /dev/null 2>/dev/null; then
    echo "metrics:       reachable at $METRICS_ADDR"
  else
    echo "metrics:       UNREACHABLE at $METRICS_ADDR" >&2
  fi
fi

if [[ "$catching_up" == "true" ]]; then
  echo "UNHEALTHY: node is still catching up" >&2
  exit 1
fi

echo "OK"
