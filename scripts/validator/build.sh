#!/usr/bin/env bash
# Build the aetrad binary for a Linux validator host.
# Linux/bash counterpart of scripts/build-aetrad.ps1 (used for Windows dev
# tooling); see docs/VALIDATOR.md and docs/validator-onboarding.md.
#
# Usage: ./scripts/validator/build.sh [-o output-binary] [-v version] [--skip-mod-verify]
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BINARY="$REPO_ROOT/build/aetrad"
VERSION=""
COMMIT=""
SKIP_MOD_VERIFY=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    -o|--output) BINARY="$2"; shift 2 ;;
    -v|--version) VERSION="$2"; shift 2 ;;
    --skip-mod-verify) SKIP_MOD_VERIFY=true; shift ;;
    *) echo "unknown argument: $1" >&2; exit 1 ;;
  esac
done

cd "$REPO_ROOT"

# Prefer the explicit `toolchain` directive (the actually-pinned version,
# e.g. after a stdlib CVE bump) over the `go` directive (a minimum language
# version, e.g. "1.25.11") -- go.mod frequently pins a newer toolchain than
# its minimum go line.
GO_PIN="$(grep -m1 -E '^toolchain go[0-9]+\.[0-9]+' go.mod | awk '{print $2}')"
GO_PIN="${GO_PIN#go}"
if [[ -z "$GO_PIN" ]]; then
  GO_PIN="$(grep -m1 -E '^go [0-9]+\.[0-9]+' go.mod | awk '{print $2}')"
fi
GO_VERSION_OUTPUT="$(go version)"
if [[ "$GO_VERSION_OUTPUT" != *"go${GO_PIN}"* ]]; then
  echo "Go toolchain mismatch: go.mod pins ${GO_PIN}, got: $GO_VERSION_OUTPUT" >&2
  exit 1
fi

COMMIT="${COMMIT:-$(git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)}"
VERSION="${VERSION:-$([ "$COMMIT" = unknown ] && echo dev || echo "dev-$COMMIT")}"
if [[ ! "$VERSION" =~ ^[A-Za-z0-9._-]+$ ]]; then
  echo "Version may contain only letters, numbers, dot, underscore, or dash: $VERSION" >&2
  exit 1
fi

DIRTY=false
if [[ -n "$(git status --porcelain --untracked-files=no 2>/dev/null)" ]]; then
  DIRTY=true
fi
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

mkdir -p "$(dirname "$BINARY")"

LDFLAGS="-X github.com/sovereign-l1/l1/cmd/l1d/cmd.appVersion=$VERSION"
LDFLAGS="$LDFLAGS -X github.com/sovereign-l1/l1/cmd/l1d/cmd.gitCommit=$COMMIT"
LDFLAGS="$LDFLAGS -X github.com/sovereign-l1/l1/cmd/l1d/cmd.buildDate=$BUILD_DATE"
LDFLAGS="$LDFLAGS -X github.com/sovereign-l1/l1/cmd/l1d/cmd.dirty=$DIRTY"

echo "Go: $GO_VERSION_OUTPUT"
go mod download
if [[ "$SKIP_MOD_VERIFY" != true ]]; then
  go mod verify
fi
# -tags purego,noadx: force gnark-crypto (AVM Phase D BN254 opcodes) onto its
# pure-Go arithmetic path on every validator build, closing the ADX-dispatch
# cross-architecture/runtime-CPU-feature determinism hazard documented in
# docs/architecture/avm-phase-d-zk-design.md (Status / owner-decisions). This
# is the INTERIM alternative (no vendoring); see that doc for the tradeoff
# accepted (amd64-vs-arm64 divergence stays open as a known gap) and every
# other place this flag combo must be passed.
CGO_ENABLED=0 go build -mod=readonly -trimpath -p=1 -tags purego,noadx -ldflags "$LDFLAGS" -o "$BINARY" ./cmd/l1d

echo "Binary: $BINARY"
echo "Version: $VERSION"
echo "Commit: $COMMIT"
echo "Dirty: $DIRTY"
echo "Build date: $BUILD_DATE"

"$BINARY" version --long --output json
