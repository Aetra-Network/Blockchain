#!/usr/bin/env bash
# Stage a new aetrad binary into the Cosmovisor upgrade directory layout
# described in docs/COSMOVISOR.md, without touching the currently-running
# binary. Cosmovisor switches to it automatically at the scheduled upgrade
# height once the on-chain upgrade plan's handler name matches.
#
# Usage:
#   ./scripts/validator/cosmovisor-upgrade.sh -H <home> -n <upgrade-name> \
#     -b <new-aetrad-binary>
#
# Follows the rollback policy in docs/COSMOVISOR.md: it never deletes an
# existing upgrade-slot binary, and never touches the genesis/ (currently
# running) binary slot.
set -euo pipefail

HOME_DIR="${HOME}/.aetra"
UPGRADE_NAME=""
NEW_BINARY=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    -H|--home) HOME_DIR="$2"; shift 2 ;;
    -n|--name) UPGRADE_NAME="$2"; shift 2 ;;
    -b|--binary) NEW_BINARY="$2"; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 1 ;;
  esac
done

if [[ -z "$UPGRADE_NAME" || -z "$NEW_BINARY" ]]; then
  echo "usage: $0 -H <home> -n <upgrade-name> -b <new-aetrad-binary>" >&2
  exit 1
fi
if [[ ! -x "$NEW_BINARY" ]]; then
  echo "binary not found or not executable: $NEW_BINARY" >&2
  exit 1
fi
if [[ ! "$UPGRADE_NAME" =~ ^[A-Za-z0-9._-]+$ ]]; then
  echo "upgrade name may contain only letters, numbers, dot, underscore, or dash: $UPGRADE_NAME" >&2
  exit 1
fi

COSMOVISOR_ROOT="$HOME_DIR/cosmovisor"
GENESIS_BIN_DIR="$COSMOVISOR_ROOT/genesis/bin"
UPGRADE_BIN_DIR="$COSMOVISOR_ROOT/upgrades/$UPGRADE_NAME/bin"
UPGRADE_BIN="$UPGRADE_BIN_DIR/aetrad"

if [[ ! -f "$GENESIS_BIN_DIR/aetrad" ]]; then
  echo "no existing cosmovisor/genesis/bin/aetrad found; this looks like a first-time" \
       "cosmovisor setup, not an upgrade. Populate $GENESIS_BIN_DIR manually with the" \
       "currently running binary before staging an upgrade." >&2
  exit 1
fi

if [[ -f "$UPGRADE_BIN" ]]; then
  echo "refusing to overwrite existing upgrade slot: $UPGRADE_BIN" \
       "(remove it manually first if this upgrade name is being re-staged)" >&2
  exit 1
fi

mkdir -p "$UPGRADE_BIN_DIR"
cp "$NEW_BINARY" "$UPGRADE_BIN"
chmod +x "$UPGRADE_BIN"

echo "Staged upgrade '$UPGRADE_NAME' at $UPGRADE_BIN"
echo "Verify:"
echo "  1. The on-chain upgrade plan's handler name is exactly '$UPGRADE_NAME'."
echo "  2. '$UPGRADE_BIN' version --long --output json matches the announced release."
echo "  3. The previous binary in $GENESIS_BIN_DIR (or an earlier upgrades/ slot) is kept"
echo "     until this upgrade is validated post-switch -- do not delete it now."
