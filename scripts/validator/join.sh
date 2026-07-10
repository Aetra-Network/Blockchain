#!/usr/bin/env bash
# Join (or re-join) a public Aetra network from an already-initialized node
# home: install the published genesis and configure peers, without touching
# priv_validator_key.json, priv_validator_state.json, or node_key.json.
# See docs/VALIDATOR.md and docs/validator-onboarding.md.
#
# Usage:
#   ./scripts/validator/join.sh -H <home> -g <genesis-url-or-path> \
#     [-p <persistent-peers>] [-s <seeds>] [-b <aetrad-binary>]
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BINARY="$REPO_ROOT/build/aetrad"
HOME_DIR="${HOME}/.aetra"
GENESIS=""
PEERS=""
SEEDS=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    -H|--home) HOME_DIR="$2"; shift 2 ;;
    -g|--genesis) GENESIS="$2"; shift 2 ;;
    -p|--peers) PEERS="$2"; shift 2 ;;
    -s|--seeds) SEEDS="$2"; shift 2 ;;
    -b|--binary) BINARY="$2"; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 1 ;;
  esac
done

if [[ -z "$GENESIS" ]]; then
  echo "usage: $0 -H <home> -g <genesis-url-or-path> [-p <persistent-peers>] [-s <seeds>]" >&2
  exit 1
fi
if [[ ! -d "$HOME_DIR/config" ]]; then
  echo "no node home at $HOME_DIR (run scripts/validator/init.sh first)" >&2
  exit 1
fi
if [[ ! -x "$BINARY" ]]; then
  echo "aetrad binary not found or not executable: $BINARY" >&2
  exit 1
fi

GENESIS_DEST="$HOME_DIR/config/genesis.json"
if [[ -f "$GENESIS_DEST" ]]; then
  cp "$GENESIS_DEST" "$GENESIS_DEST.bak.$(date +%s)"
fi
if [[ "$GENESIS" =~ ^https?:// ]]; then
  curl -fsSL "$GENESIS" -o "$GENESIS_DEST"
else
  cp "$GENESIS" "$GENESIS_DEST"
fi
"$BINARY" genesis validate-genesis "$GENESIS_DEST" --home "$HOME_DIR"
echo "Genesis installed and validated: $GENESIS_DEST"

CONFIG_TOML="$HOME_DIR/config/config.toml"
if [[ -n "$PEERS" ]]; then
  sed -i "s/^persistent_peers *=.*/persistent_peers = \"$PEERS\"/" "$CONFIG_TOML"
  echo "persistent_peers set"
fi
if [[ -n "$SEEDS" ]]; then
  sed -i "s/^seeds *=.*/seeds = \"$SEEDS\"/" "$CONFIG_TOML"
  echo "seeds set"
fi

echo "Ready to join. Verify trust height/hash from the launch announcement before enabling state sync."
