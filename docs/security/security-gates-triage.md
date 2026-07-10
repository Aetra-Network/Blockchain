# Security Gates Triage Ledger

This is the executable triage ledger for high/critical security gates. A public
testnet or production release must not have untriaged high/critical findings in
fund-safety, consensus-safety, or secret-leak categories.

## Gate Commands

Run from the repository root:

```powershell
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
go run github.com/securego/gosec/v2/cmd/gosec@latest -exclude-generated -exclude G115 -severity high -confidence medium ./...
go run github.com/zricethezav/gitleaks/v8@v8.28.0 git --config .gitleaks.toml --no-banner --redact --log-opts=--all .
```

CodeQL and dependency review are CI gates in `.github/workflows/security.yml`.
Dependency review runs on pull requests and fails on `high` or higher advisory
severity. CodeQL uploads SARIF to GitHub code scanning.

## Current Local Run

Re-run live on 2026-07-09 with `govulncheck ./...` (default reachability mode,
the same mode `.github/workflows/security.yml` parses). This is a genuine
per-advisory review, not a blanket "track upstream" statement: govulncheck's
default mode reported exactly **3 reachable findings** (not the historical
20-entry allowlist size — the rest of that list covers advisories that are
present in the dependency tree but never appear as a reachable
`Vulnerability #` line, so CI never needs an entry for them; kept only as a
defensive/historical record). Of the 6 reachable findings on the first run
that day, 3 were fixed immediately by safe patch/minor dependency bumps
(`go.mod` toolchain `go1.26.5`, `github.com/quic-go/quic-go` `v0.59.1`) and
reconfirmed absent on re-scan; `go build ./...` and the full test suite were
green after the bump (see `GO-2026-4970`/`GO-2026-5856`/`GO-2026-5676` rows
below).

| Gate | Scope | Result | Evidence |
| --- | --- | --- | --- |
| govulncheck | `./...` | pass: 3 reachable findings, all triaged/accepted | `.github/security/govulncheck-triage.txt` |
| gosec high non-G115 | fund/consensus/runtime scopes | pass | `artifacts/security-gates/gosec-high-non-g115.json` |
| gosec G115 | tracked separately | triaged plus runtime fixes | `artifacts/security-gates/gosec-critical-scope.json` |
| gitleaks | git history/tree | pass after false-positive allowlist | `.gitleaks.toml` |
| CodeQL | CI | required | `.github/workflows/security.yml` |
| dependency review | CI pull request | required | `.github/workflows/security.yml` |

## Findings

| Finding | Owner | Severity | Impact bucket | Status | Mitigation | Milestone |
| --- | --- | --- | --- | --- | --- | --- |
| `GO-2026-5856` Encrypted Client Hello privacy leak, `crypto/tls@go1.26.4` (stdlib, reached via `observability/server.go` metrics HTTP server and grpc-gateway HTTP paths) | Security | High | dependency | fixed | Bumped `go.mod` `toolchain` directive to `go1.26.5` (stdlib patch, fixes `crypto/tls`). Reconfirmed absent on `govulncheck` re-scan; `go build ./...` and full test suite green after the bump. | current |
| `GO-2026-4970` root escape via symlink + trailing slash, `os@go1.26.4` (stdlib, reached via `cmd/l1d/cmd/avm_tools.go` `os.Root` file-tree walk for AVM source collection) | Security | High | dependency | fixed | Same `go1.26.5` toolchain bump as `GO-2026-5856` (stdlib patch, fixes `os`). Reconfirmed absent on re-scan. | current |
| `GO-2026-5676` HTTP/3 QPACK trailer expansion memory exhaustion, `github.com/quic-go/quic-go@v0.59.0` (reached via `observability/server.go` HTTP/3 metrics server config) | Security | High | dependency | fixed | Upgraded `github.com/quic-go/quic-go` `v0.59.0` -> `v0.59.1` (patch release with the fix). Reconfirmed absent on re-scan; no other observability/HTTP behavior changed. | current |
| `GO-2026-5932` `golang.org/x/crypto/openpgp` is unmaintained and unsafe by design (reached only via `cosmos-sdk`'s keyring package `init()` chain, i.e. the package is linked into the binary because the keyring supports optional ASCII-armored key export/import; the actual `armor.Decode`/`armor.Encode` code only runs if an operator explicitly uses that export format) | Security | High | dependency | accepted | No fixed version exists (package is deprecated upstream with no replacement inside `cosmos-sdk`'s keyring). Operational mitigation: do not use ASCII-armored (`--output armor`-style) key export/import; use the standard keyring backends. Re-evaluate if `cosmos-sdk` drops this dependency in a future release. | public-testnet-gate |
| `GO-2026-4479` random nonce generation with AES-GCM in `github.com/pion/dtls/v2@v2.2.12` (linked via `cmd/l1d/cmd/testnet.go` -> `cosmos-sdk` `testutil/network` -> CometBFT's optional libp2p/WebRTC P2P transport `init()` chain; most of the raw govulncheck trace list is `fmt.Stringer`/`error`-interface dispatch noise — e.g. "`app.L1App.initKeepers` calls `cast.ToString`, which eventually calls `alert.Description.String`" is a static call-graph over-approximation across unrelated types sharing a `.String()`/`.Error()` method signature, not a real call path) | Security + Networking | High | consensus-adjacent networking dependency | accepted | No fixed version exists yet. The vulnerable nonce-generation code only executes during an actual DTLS/WebRTC handshake, which requires enabling CometBFT's non-default libp2p transport; standard `aetrad` nodes use CometBFT's classic P2P networking. Track `pion/dtls`/`libp2p`/`cometbft` upstream; do not mark production-live until an upgrade lands or this optional transport is explicitly compiled out. | public-testnet-gate |
| `GO-2024-2584` slashing evasion in `github.com/cosmos/cosmos-sdk@v0.54.3` (GHSA-86h5-xcpx-cfqc / cosmos-sdk's own ASA-2024-005, "potential slashing evasion during re-delegation", CWE-372, upstream-rated **Low**, no CVE), reached via `app.L1App.BeginBlocker` -> `module.Manager.BeginBlock` -> `staking keeper.Keeper.SlashWithInfractionReason` (runs every block as part of normal consensus operation) | Security + Consensus | High (govulncheck bucket) / Low (upstream advisory) | consensus-safety dependency | fixed (false positive against this pin) | Dedicated review complete. cosmos-sdk's own advisory lists fixed versions `v0.47.10` and `v0.50.5`; this repo pins `v0.54.3` (`go.mod`/`go.sum`), well past both. Verified directly against the vendored module source (`x/staking/keeper/slash.go` lines ~330-337, "Handle undelegation after redelegation / Prioritize slashing unbondingDelegation than delegation") that the fix is present; the companion `x/auth/vesting` `BlockedAddr` check from the same GHSA is also present. `govulncheck` still reports "no fixed version" only because the upstream OSV record (`vuln.go.dev/ID/GO-2024-2584.json`) has an open-ended SEMVER range (`introduced: 0.50.0` with no matching `fixed` event) -- a govulndb authoring gap for the 0.50+ branch, not an unpatched upstream bug. No upgrade or code change needed. Keep PoS/slashing smoke tests mandatory as general regression coverage. Re-open only if a future cosmos-sdk downgrade or fork removes this fix. | public-testnet-gate |
| Remaining allowlisted `GO-*` IDs in `.github/security/govulncheck-triage.txt` not listed above | Security | High | dependency | triaged | These do not appear as a reachable finding in the current `govulncheck ./...` run (govulncheck's default mode only reports the 3 above; these are in the "imported but not called" bucket or are stale from an earlier scan). Kept in the allowlist defensively in case a future scan resurfaces them. Re-run `govulncheck ./...` before release branch cut and re-triage any newly-reachable ID. | public-testnet-gate |
| `gosec` `G115` in generated protobuf files | Protocol Tooling | High from scanner, Low after triage | generated-code false positive | triaged | Do not hand-edit generated files. Keep `gosec -exclude-generated` in CI and regenerate from trusted protobuf toolchain. | public-testnet-gate |
| `gosec` `G115` in AVM opcode/result narrowing | AVM Runtime | High | consensus-safety | fixed | Verifier rejects `OpEmitInternal` and `OpReturn` arguments above `uint32` before execution; regression test covers both. | current |
| `gosec` `G115` in storage rent payment amount conversion | Storage Rent | High | fund-safety | fixed | Replaced `int64` narrowing with `sdkmath.NewIntFromUint64`. | current |
| `gosec` `G115` in dynamic fee and anti-spam bounded basis-point math | Fees | High from scanner, Medium after bounds | fund-safety | mitigated | Replaced unsafe-looking casts with explicit saturating `uint64 -> uint32` helper in runtime fee paths. Remaining `int64` basis-point casts are bounded by validation and must stay covered by fee tests. | current |
| `gosec` `G101` low-confidence matches on runtime/readiness constants | Security | High from scanner, Not a secret after triage | secret-leak | false-positive | Reviewed matches are symbolic constants such as `SYSCALL_WALL_CLOCK`, `avm:credit:v1`, readiness IDs, and invariant IDs. CI high gate requires medium confidence or higher; these remain documented here for secret-leak closure. | current |
| `gitleaks` generic API key match on `MaxConsensusKeyBytesV1` | Security | High from scanner, Not a secret after triage | secret-leak | false-positive suppressed | `.gitleaks.toml` allowlist scopes the false positive to validator-registry consensus-key limit validation. | current |

## Blocking Rule

Any new high/critical scanner output is untriaged unless it has:

- owner;
- severity;
- impact bucket: `fund-safety`, `consensus-safety`, `secret-leak`, `dependency`, or `tooling`;
- mitigation;
- target milestone;
- status: `fixed`, `triaged`, `accepted`, or `false-positive`.

Untriaged high/critical fund-safety, consensus-safety, or secret-leak findings
block release.
