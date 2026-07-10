#!/usr/bin/env bash
# Initialize a single validator node home directory for a public Aetra
# network. See docs/VALIDATOR.md and docs/validator-onboarding.md.
#
# Usage:
#   ./scripts/validator/init.sh -m <moniker> -c <chain-id> [-H <home>] \
#     [-g <genesis-url-or-path>] [-p <persistent-peers>] [-s <seeds>] \
#     [-b <aetrad-binary>]
#
# Only initializes local node config/keys. It never touches keyring
# contents or priv_validator_key.json beyond what `aetrad init` itself
# creates on a fresh home; it refuses to run against an existing home
# unless --force is passed, so it can never overwrite validator key
# material.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BINARY="$REPO_ROOT/build/aetrad"
HOME_DIR="${HOME}/.aetra"
MONIKER=""
CHAIN_ID=""
GENESIS=""
PEERS=""
SEEDS=""
FORCE=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    -m|--moniker) MONIKER="$2"; shift 2 ;;
    -c|--chain-id) CHAIN_ID="$2"; shift 2 ;;
    -H|--home) HOME_DIR="$2"; shift 2 ;;
    -g|--genesis) GENESIS="$2"; shift 2 ;;
    -p|--peers) PEERS="$2"; shift 2 ;;
    -s|--seeds) SEEDS="$2"; shift 2 ;;
    -b|--binary) BINARY="$2"; shift 2 ;;
    --force) FORCE=true; shift ;;
    *) echo "unknown argument: $1" >&2; exit 1 ;;
  esac
done

if [[ -z "$MONIKER" || -z "$CHAIN_ID" ]]; then
  echo "usage: $0 -m <moniker> -c <chain-id> [-H <home>] [-g <genesis>] [-p <persistent-peers>] [-s <seeds>]" >&2
  exit 1
fi
if [[ ! -x "$BINARY" ]]; then
  echo "aetrad binary not found or not executable: $BINARY (run scripts/validator/build.sh first)" >&2
  exit 1
fi
if [[ -d "$HOME_DIR/config" && "$FORCE" != true ]]; then
  echo "refusing to re-init existing home $HOME_DIR (pass --force only if you understand this recreates config, not keys)" >&2
  exit 1
fi

"$BINARY" init "$MONIKER" --chain-id "$CHAIN_ID" --home "$HOME_DIR"

if [[ -n "$GENESIS" ]]; then
  GENESIS_DEST="$HOME_DIR/config/genesis.json"
  if [[ "$GENESIS" =~ ^https?:// ]]; then
    curl -fsSL "$GENESIS" -o "$GENESIS_DEST"
  else
    cp "$GENESIS" "$GENESIS_DEST"
  fi
  "$BINARY" genesis validate-genesis "$GENESIS_DEST" --home "$HOME_DIR"
  echo "Genesis installed and validated: $GENESIS_DEST"
fi

CONFIG_TOML="$HOME_DIR/config/config.toml"
if [[ -n "$PEERS" ]]; then
  sed -i "s/^persistent_peers *=.*/persistent_peers = \"$PEERS\"/" "$CONFIG_TOML"
  echo "persistent_peers set"
fi
if [[ -n "$SEEDS" ]]; then
  sed -i "s/^seeds *=.*/seeds = \"$SEEDS\"/" "$CONFIG_TOML"
  echo "seeds set"
fi

echo "Initialized $MONIKER for $CHAIN_ID at $HOME_DIR"
echo "Do not reuse localnet/dev keys. Verify the published genesis checksum before first start."
