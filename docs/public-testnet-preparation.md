> Note: historical native asset-factory and native exchange modules have been removed from the active app graph.
# Public Testnet Preparation

This runbook is the Phase 16 gate before opening Aetra to external validators. It is not mainnet readiness.

The full public testnet and production gate ledger is
[Public Testnet And Production Gates](public-testnet-production-gates.md).

## Profiles

Run the objective readiness report before starting localnet profiles. It checks
that AVM/contracts, native-account, official pool staking, storage rent,
governance/config safety, app invariants, export/import evidence, and
contract-only asset boundaries are implemented in runtime paths rather
than only prototype/spec packages. The readiness gate also treats `buf lint`
as mandatory. The CI workflow and the local release toolchain both install
`buf` through `scripts\tooling\ensure-buf.ps1`, pinned by `BUF_VERSION`, so
proto lint is not a machine-specific convenience check:

```powershell
.\scripts\testnet\public-testnet-readiness-report.ps1
.\scripts\testnet\public-testnet-readiness-report.ps1 -OutputFormat Json
buf lint
```

Run all local profiles before publishing testnet genesis:

```powershell
.\scripts\testnet\public-testnet-preflight.ps1 -ValidatorProfile All
```

Individual profiles:

```powershell
.\scripts\testnet\public-testnet-preflight.ps1 -ValidatorProfile 3 -SkipBuild
.\scripts\testnet\public-testnet-preflight.ps1 -ValidatorProfile 5 -SkipBuild
.\scripts\testnet\public-testnet-preflight.ps1 -ValidatorProfile 10 -SkipBuild
```

When you are running a release binary, archive the evidence bundle alongside
the logs, checksums, chain-id, and genesis hash:

```powershell
.\scripts\testnet\public-testnet-preflight.ps1 `
  -ValidatorProfile All `
  -Binary .\dist\prototype\<version>\aetra-<version>-windows-amd64\bin\aetrad.exe `
  -SkipBuild `
  -EvidenceRoot .work\public-testnet-preflight-evidence\release-check `
  -ArchiveEvidence
```

The preflight runs full prototype acceptance, validates the requested validator count, exercises bank, fees, direct user delegation rejection, staking/slashing query surfaces, restart persistence, and asserts CosmWasm remains disabled unless explicitly gated. Application-level asset behavior must be exercised through AVM contracts and standards, not through native app modules. Token, NFT, and DEX-style behavior must be exercised through AVM contracts. Token, NFT, market, and exchange-style application logic is routed through the gated AVM contract track rather than native modules. Official liquid staking pool deposit/claim/unbond, validator operator self-bond compatibility, and storage-rent recovery still require their own focused runtime evidence and must not be inferred from this preflight alone. The 10-validator profile is the stress profile for public testnet readiness; it is expected to be slower and should run before advertising modular execution features.

Archived release-binary preflight evidence already exists for profiles 3, 5, 10, and All under:

- `.work\public-testnet-preflight-evidence\release-evidence-3`
- `.work\public-testnet-preflight-evidence\release-evidence-5`
- `.work\public-testnet-preflight-evidence\release-evidence-10`
- `.work\public-testnet-preflight-evidence\release-evidence-all`

Each archive includes `preflight.log`, `run-summary.json`, `profile-manifest.json`, `binary-version.json`, `binary-sha256.txt`, `chain-id.txt`, `genesis.json`, `genesis-sha256.txt`, and the captured `logs/` tree. The launch evidence bundle adds the trust data and snapshot publication manifest used by the readiness packet. That exact contract-only asset model is why token, NFT, market, and exchange-style application logic now targets AVM contracts rather than native modules.

AVM contract standard smoke is a separate evidence step, not something to infer
from the base preflight. Run the contract-only smoke wrapper when you need the
repeatable contract evidence path:

```powershell
.\tests\e2e\avm_contract_smoke.ps1
```

That smoke path uses the canonical AVM examples and the keeper/runtime tests
that cover store code, deploy, execute, query, migrate, bounce handling, and
negative cases. It now also covers counter, treasury, token, NFT, and DEX-style
lifecycle flows plus the measured-limits review gate for gas, memory, code size,
queue depth, and state growth.

When you need a single launch evidence bundle, package the release binary
checksum, genesis hash, chain-id, RPC endpoints, snapshot trust data, validator
docs, incident docs, security scan evidence, and preflight outputs together:

```powershell
.\scripts\testnet\launch-evidence-bundle.ps1 `
  -OutputDir .work\launch-evidence\release-check `
  -Binary .\dist\prototype\<version>\aetra-<version>-windows-amd64\bin\aetrad.exe `
  -PreflightEvidenceRoot .work\public-testnet-preflight-evidence\release-check `
  -StateSyncEvidencePath .work\state-sync-drill\evidence\state-sync-drill.json `
  -ValidatorOnboardingEvidencePath .work\validator-onboarding-drill\evidence\validator-onboarding-drill.json `
  -SecurityEvidencePaths @(
    ".github\security\govulncheck-triage.txt",
    "docs\security\security-gates-triage.md"
  ) `
  -Strict
```

`-Strict` fails if the bundle cannot prove the required fields. Use it for the
release packet, not for casual partial captures.

Public testnet staking remains pool-only: users enter through the official liquid staking pool, and direct delegation to validators stays disabled on the normal user path.

The focused E2E smoke command list is maintained in
[Public Testnet E2E Smoke Commands](public-testnet-e2e-smoke-commands.md).
Long-running network evidence is tracked in
[Public Testnet Long-Running Evidence](public-testnet-long-running-evidence.md).

## What To Tighten Before Public Testnet

To reach 100/100 for public testnet readiness, the remaining concrete steps are:

- Run `.\scripts\testnet\public-testnet-preflight.ps1 -ValidatorProfile 3`,
  `5`, `10`, and `All` on the release binary, then pin the passing output as
  launch evidence.
- Capture and archive evidence for snapshot restore, state-sync restore,
  fresh validator onboarding, and long-running restart and rollback traces.
- Run the full security layer and keep every `High`/`Critical` finding closed
  or explicitly triaged: `govulncheck`, `gosec`, `CodeQL`, `gitleaks`, and
  dependency review.
- Obtain an independent audit and close or explicitly accept all findings.
- Finish operational readiness for faucet, explorer/indexer, incident
  response, rollback planning, and validator documentation on a clean machine.
- Run `buf lint` through the same pinned helper used by CI and the local
  release package, and record it as a mandatory gate, not a local-only
  convenience check.
- Archive `public-testnet-preflight.ps1` evidence bundles for the release
  binary, including logs, version JSON, binary checksum, chain-id, and
  genesis hash.
- Keep the release artifact tree clean before the public launch so the launch
  cut is separated from unrelated worktree state.

## Launch Checklist Status

Status as of 2026-07-01:

- Faucet plan: implemented (2026-07-09). `aetrad faucet serve` is a real
  off-chain HTTP service (`cmd/l1d/cmd/faucet.go`) enforcing rate limiting,
  a fixed per-request grant, and normal `bank send` semantics; no on-chain
  mint path was added. Owner: testnet ops. Evidence:
  `cmd/l1d/cmd/faucet_test.go` (11 tests: rate limiter, HTTP handler,
  rate-limit release-on-failure), see Faucet Plan below for the operator
  command.
- Explorer/indexer plan: deferred for validator liveness. Public launch uses
  RPC/REST as the required surface; explorer and indexer remain optional
  operator services. Owner: infra. Evidence:
  node RPC status shows `tx_index: on` on all live nodes, `/tx_search`
  returns historical transactions, and there is no dedicated lag exporter in
  the release binary yet; use
  `.work/launch-evidence/20260701-snapshot-state-sync-validator-drill/evidence-pack/txsearch-1284.json`
  as the searchable-index proof.
- Rollback/restart readiness: restart drill completed on the launch-evidence
  localnet archive at `.work/launch-evidence/snapshot-state-sync-onboarding.zip`.
  Rollback remains documented and deferred until a signed release binary
  rollback target is exercised. Owner: release engineering. Evidence:
  `.work/launch-evidence/20260701-snapshot-state-sync-validator-drill/evidence-pack/block-1880.json`,
  `.work/launch-evidence/20260701-snapshot-state-sync-validator-drill/evidence-pack/telemetry.jsonl`,
  and `docs/testnet-incident-response.md`.
- Incident response: runbook published and cross-linked from the launch
  checklist. Owner: incident commander. Evidence:
  `docs/testnet-incident-response.md`.

## Localnet Hardening

Public testnet prep depends on these script rules:

- localnet output directories must stay under the repository workspace,
- start refuses occupied P2P, RPC, REST, gRPC, and pprof ports,
- `-CleanLogs` is explicit when old logs should be removed,
- snapshot and state-sync scripts resolve paths through localnet helpers,
- diagnostics must not package keyrings, validator private keys, mnemonics, or environment secrets.

## Faucet Plan

Use an off-chain faucet for public testnet. There is no on-chain mint path for
v1 — the faucet is an ordinary `bank send` from a normal, capped, prefunded
account, exactly like any other user transaction.

`aetrad faucet serve` is a real, running HTTP service (not a dry-run preview):
it signs and broadcasts a `MsgSend` per accepted request using the same
Factory/Sign/BroadcastTx pipeline as `tx bank send`, and enforces every rule
below in code, not just in this document.

```powershell
build\aetrad.exe faucet serve `
  --from <faucet-key> --keyring-backend <secure-backend> `
  --chain-id <testnet-chain-id> --node <rpc-url> --fees 1000000naet `
  --listen-addr 127.0.0.1:8099 --amount 1000000naet --cooldown 24h
```

Faucet rules and how they are enforced:

- faucet wallet is a normal account with capped prefunded `naet` — the
  service only ever spends from the single `--from` account; it has no
  minting capability,
- rate limit by address and IP — `faucetRateLimiter` in
  `cmd/l1d/cmd/faucet.go` tracks both independently; either being
  rate-limited rejects the request with HTTP 429,
- one request per address per cooldown window — `--cooldown` (default 24h);
  a failed broadcast releases the reservation so it does not silently burn
  the caller's window,
- max grant per request is fixed and documented before launch — `--amount` is
  an operator flag; the HTTP request body can only supply a recipient
  address, never an amount,
- faucet txs pay `naet` fees and use normal `bank send` — enforced by reusing
  the standard tx `Factory`/`MsgSend` pipeline, no custom transaction type,
- faucet private key is stored outside the repository in a secret manager —
  the service only ever references the key by `--from` name through the
  configured `--keyring-backend`; it never reads or logs raw key material,
- faucet logs never print private keys, mnemonics, or full environment dumps
  — request logs record only the recipient address, client IP, amount, and
  resulting tx hash.

Operate the service behind a reverse proxy that terminates TLS and enforces
its own coarser IP-level throttling; `GET /healthz` is provided for load
balancer/uptime checks. Tests: `cmd/l1d/cmd/faucet_test.go` (rate limiter,
HTTP handler validation, rate-limit release-on-failure, all with a fake
broadcaster — no live chain required).

## Explorer And Indexer Plan

Implemented: `l1-explorer` is the block-explorer data source — a block/tx
indexer over CometBFT RPC plus a live gRPC proxy for contract/validator/supply
state, served as a read-only JSON HTTP API. It lives in the ecosystem repo next
to the explorer site (`ecosystem/explorer/server`, module
`github.com/aetra-network/explorer-server`), not in this chain repo. Full
endpoint reference and run instructions are in [explorer.md](explorer.md);
run it with [scripts/validator/explorer.sh](../scripts/validator/explorer.sh).

Operational requirements it satisfies / an operator must still wire:

- **satisfied** — CometBFT RPC block/tx/validator/status views; gRPC bank
  (supply), staking (validators), and `x/contracts` module queries; tx
  message/fee/event decoding including hand-rolled `x/contracts` messages;
  per-address tx history and search;
- **operator wiring** — run `l1-explorer` against a dedicated non-validator
  RPC/gRPC node; alert when `/status` `indexed_height` lags `latest_height`
  beyond the launch threshold; for a large public history, back the
  `server/store.Store` interface with Postgres (credentials outside repo
  config) instead of the default in-memory window;
- **invariant** — the explorer is read-only and off the validator liveness
  path: a down indexer never affects consensus.

## Minimum Hardware

Development validator:

- 4 CPU cores,
- 8 GB RAM,
- 100 GB SSD,
- reliable broadband connection,
- Windows PowerShell for current scripts or Linux shell wrappers to be added before Linux-only operators join.

Public testnet validator:

- 4-8 CPU cores,
- 16 GB RAM,
- 200 GB SSD with growth monitoring,
- stable public P2P networking,
- separate monitoring host or process for alerting,
- time sync enabled.

Do not run public validators with localnet `--keyring-backend test` keys.

## Snapshot And State-Sync Plan

The release gate is the executable drill, not only manual config editing. It
starts a trusted localnet, publishes two trusted RPC endpoints, derives and
records trust height/hash, exports a snapshot archive and checksum, resets a
fresh joining node, enables state sync, and verifies that the node catches up
and stays stable without manual fixes:

```powershell
.\scripts\localnet\state-sync-drill.ps1 `
  -OutputDir .work\state-sync-drill `
  -Binary .\build\aetrad.exe `
  -ChainId aetra-local-state-sync-drill-1 `
  -SkipBuild
```

The evidence file is written to
`.work\state-sync-drill\evidence\state-sync-drill.json` and must contain:

- `trusted_rpcs` with at least two RPC URLs,
- `trust_height` and `trust_hash`,
- `snapshot_archive` and `snapshot_sha256`,
- `catching_up = false`,
- a target join height above the trust height,
- a nonzero peer count for the joined node.

Snapshot creation on a trusted node:

```powershell
.\scripts\localnet\snapshot.ps1 -OutputDir .localnet -NodeIndex 0 -Height <height> -ArchivePath .work\snapshots\aetra-testnet-<height>.tar
```

State sync configuration on a joining node:

```powershell
.\scripts\localnet\statesync.ps1 -OutputDir .localnet -TargetNodeIndex 2 -TrustHeight <height> -TrustHash <hash> -ResetData
```

Public testnet publishing requirements:

- publish snapshot height, hash, archive checksum, and source validator identity,
- publish at least two RPC servers for state sync,
- publish trust height, trust hash, trust period, and the exact RPC list in the launch announcement,
- never publish validator private key files or keyrings in snapshots,
- keep one recent snapshot and one older fallback snapshot until the next upgrade.

### Repeatable Restore Criteria

State-sync and snapshot restore are only accepted as launch-ready when the
same published data can be replayed from a clean machine or empty home more
than once without manual repair:

- start from a fresh node home or deleted data directory;
- use the published trust height, trust hash, trust period, RPC list, snapshot
  checksum, and source validator identity from the launch packet;
- repeat the restore or state-sync join from a second clean home using the
  same published values;
- the restored node must end with `catching_up = false`;
- the restored node's latest app hash must match the source node's app hash;
- no run may surface `AppHash mismatch` in the logs;
- the owner archives the command output, JSON evidence, and timestamps for each
  run.

This is the operator scenario that replaces any "it worked once" claim.

## CosmWasm Test Contract

CosmWasm remains disabled by default. If a testnet config explicitly enables the wasm gate, run:

```powershell
.\tests\e2e\cosmwasm_smoke.ps1 -EnableWasm -ContractWasm .\artifacts\cw_template.wasm
```

The contract upload and instantiate flow is documented in [CosmWasm Readiness](security/cosmwasm-readiness.md).

If async contracts are enabled in a public testnet config, run the async
execution smoke and the contract standards tests before opening the
network:

```powershell
.\.work\tools\go1.25.11\go\bin\go.exe test ./x/aetravm/async
```

## Rollback And Restart Procedure

Use restart only for operational failures where the chain state is valid:

1. Announce the target height, UTC window, affected RPC endpoints, and expected
   validator action.
2. Stop nonessential load generators, faucet jobs, and indexer writes.
3. Stop validators cleanly. Preserve `data`, `config\priv_validator_key.json`,
   `config\node_key.json`, genesis, and logs.
4. Apply only the announced config or binary change.
5. Restart one seed validator first, then the rest of the validator set.
6. Confirm block production, app hash agreement across at least three nodes,
   peer count, and public RPC health.
7. Resume faucet and indexer jobs after finalized blocks are visible.

Use rollback only when the new binary, config, genesis, snapshot, or state-sync
announcement is proven bad:

1. Freeze faucet/indexer writes and publish the rollback reason.
2. Restore the previous signed binary/config or corrected snapshot trust data.
3. Do not delete validator state unless the incident response owner explicitly
   requires a data reset and the validator has backed up evidence.
4. Run `go test ./...`, `go vet ./...`, `buf lint`, and the public-testnet
   preflight profile that matches the affected validator count before
   re-opening external joins.

## Launch Checklist

- `go test -p=1 ./...` passes.
- `go vet -p=1 ./...` passes.
- `buf lint` passes through the pinned helper and release toolchain.
- Security scans pass or findings are triaged.
- Deterministic execution gate passes.
- `.\scripts\testnet\public-testnet-preflight.ps1 -ValidatorProfile All -SkipBuild -Binary <release-binary> -ArchiveEvidence` passes.
- Genesis validates on every seed validator.
- At least one fresh validator follows [Validator Onboarding](validator-onboarding.md) and reaches the validator set.
- Fresh validator onboarding evidence is captured with
  `.\scripts\localnet\validator-onboarding-drill.ps1`.
- Faucet dry-run sends `naet` to a new address; see
  `.work/launch-evidence/20260701-snapshot-state-sync-validator-drill/evidence-pack/faucet-preview.report.json`.
- Explorer/indexer follows node height and shows tx details; see
  `.work/launch-evidence/20260701-snapshot-state-sync-validator-drill/evidence-pack/txsearch-1284.json`.
- Snapshot and state-sync instructions are tested.
- Incident response contacts and severity rules are published.
- Faucet plan is implemented or explicitly deferred.
- Explorer/indexer plan is implemented or explicitly deferred.
- Rollback/restart drill evidence is archived, and rollback is explicitly
  deferred until a signed release binary target is exercised.
- Fresh-machine onboarding drill evidence is archived and rerun for each
  release candidate.
- If CosmWasm or async contracts are enabled, simple contract deployment plus
  contract standards smoke tests pass first.

## Drill Cadence And Owners

The following drills are part of the release-candidate loop. If a drill is
deferred, the deferred state, owner, and evidence link must stay current:

- Fresh-machine onboarding: owner `validator operations`; rerun on every
  release candidate and after CLI, genesis, or keyring flow changes.
- Snapshot/state-sync restore: owner `release engineering`; rerun on every
  release candidate and after any snapshot, trust-data, or state-sync change.
- Incident response and rollback/restart: owner `incident commander` for the
  incident flow and `release engineering` for rollback/restart; rerun on every
  release candidate and after binary/config restart changes.
- Faucet/indexer plan: owner `testnet ops` for faucet and `infra` for
  indexer; rerun on every launch rehearsal until the item is implemented or
  explicitly deferred in the launch status section.
