# Public Testnet Long-Running Evidence

This checklist records the minimum evidence required before public testnet can
be promoted toward launch readiness. It is a live evidence pack, not a
production-readiness claim, and every metric below must have an owner, source,
sample interval, and retention policy before launch.

Required metrics:

| Metric | Required evidence |
| --- | --- |
| `app_hash` | App hash agreement across validators and after restart/export/import. |
| `finality_seconds` | Median, p95, and max finality under normal and degraded conditions. |
| `missed_blocks` | Per-validator missed block counts and recovery windows. |
| `evidence_age` | Evidence age distribution and evidence retention coverage. |
| `peer_count` | Peer count per validator, seed, and public RPC node. |
| `state_sync_restore` | At least one fresh node restore from published trust height/hash. |
| `snapshot_restore` | At least one restore from the published snapshot archive. |
| `storage_rent_debt` | User, contract, pool, and system rent debt totals and top-up events. |
| `system_rent_runway` | Reserve runway, warning threshold, critical threshold, and invariant alerts. |
| `pool_deposit_claim_unbond` | Official pool deposit, reward claim, unbond request, and matured withdrawal receipts. |
| `validator_uptime` | Uptime, jail/slash events, and validator set churn. |
| `incident_count` | Open/closed incidents by severity and whether fund-safety or consensus-safety was affected. |

Required long-run checklist:

- 3-validator, 5-validator, and 10-validator localnet profiles have passed.
- A public testnet run records at least one planned restart.
- Export/import roundtrip preserves account, contract, pool, storage rent, and
  governance/config state.
- Direct user delegation remains disabled throughout the run.
- Native application-asset modules remain absent; assets use AVM contracts.
- Official liquid staking pool remains recoverable under `frozen_limited`.
- Protocol-critical system state remains executable under system rent
  underfunding alerts.
- All high/critical security findings are closed or explicitly owner-triaged.

## Archived Release Preflight Evidence

The release-binary preflight runs for validator profiles 3, 5, 10, and All are
archived under:

- `.work\public-testnet-preflight-evidence\release-evidence-3`
- `.work\public-testnet-preflight-evidence\release-evidence-5`
- `.work\public-testnet-preflight-evidence\release-evidence-10`
- `.work\public-testnet-preflight-evidence\release-evidence-all`

Each archive carries the release binary checksum, `version --long --output json`
output, chain-id, genesis hash, run summary, profile manifest, and logs. The
paired launch evidence bundle carries the trust data used for restore and
readiness publication, including trust height/hash and RPC endpoints.

## Restore Acceptance Rule

State-sync and snapshot restore are not proven by a one-off historical run.
The accepted operator scenario requires:

- a fresh node home or clean machine for each run;
- published trust height, trust hash, trust period, RPC list, and snapshot
  checksum from the launch packet;
- a repeat run from a second clean home using the same published values;
- a final state where `catching_up = false` and the restored app hash matches
  the source node's app hash;
- no `AppHash mismatch` in the logs;
- archived command output and evidence JSON for each run.

If any restore attempt fails this rule, it remains retained evidence until the
owner reruns the drill successfully.

## Evidence Status Captured On 2026-07-01

Observed window: 2026-07-01 14:31-14:42 +03:00 on the launch-evidence localnet
archive at `.work/launch-evidence/snapshot-state-sync-onboarding.zip`.

| Metric | Owner | Source | Status | Observed evidence |
| --- | --- | --- | --- | --- |
| `app_hash` | Testnet SRE | `dashboard.txt` | captured | Height `1061`, app hash `267B1CAD72E5F60509B16FB79203FB694625483976DF514429CC3FC12B92C590`; 3 bonded validators agreed in the dashboard snapshot. |
| `finality_seconds` | Performance | `stress-profile-finality.json` | partial | `finality_wall_seconds = 5.5951`, `broadcast_seconds = 0.0486`, `committed_success = 1`; needs a longer observation window for median/p95/max. |
| `missed_blocks` | Consensus ops | `dashboard.txt`, `signing-infos.json` | partial | One validator was jailed/unbonding after the drill; the current query artifact does not expose a clean missed-block counter, so the per-validator counter still needs a dedicated capture path. |
| `evidence_age` | Release engineering | launch evidence archive | deferred | Retention and age distribution are not yet wired into a dedicated evidence collector. |
| `peer_count` | Networking | `dashboard.txt` | captured | Node0 reported `2` peers at capture time. |
| `state_sync_restore` | Release engineering | `snapshot-state-sync-summary.json` | captured | Trust height `178`, trust hash `659FF2B4E7B8DBEA7B4F9C7FE168C832CBCCF7AE9E53456697F234B38468164D`, join height `179`, catching up `false`. |
| `snapshot_restore` | Release engineering | `snapshot-state-sync-summary.json` | captured | Snapshot archive `aetra-testnet-178.tar`, checksum `fc10f0ec4d7d80ea5742f3491d2bb5788e4ff99429f3356f19a51e372efde5ba`. |
| `storage_rent_debt` | Protocol ops | `storage-rent-balance.json`, `treasury-balance.json`, `fee-collector-balance.json` | partial | Module balances captured at `0naet`, but user/system debt rollups still need a dedicated exporter. |
| `system_rent_runway` | Protocol ops | `storagerent-params.txt` | deferred | Current query surface did not yield a canonical runway value. |
| `pool_deposit_claim_unbond` | Staking ops | `dashboard.txt`, `nominator-pool` query artifacts | deferred | Official pool deposit/claim/unbond flow was not completed in this evidence window. |
| `validator_uptime` | Consensus ops | `dashboard.txt`, `signing-infos.json` | captured | 3 validators bonded, 1 validator jailed/unbonding (`validator-smoke`), peer nodes remained bonded and producing blocks. |
| `incident_count` | Incident commander | incident log | deferred | No incident window was opened during this capture window, so the formal count pipeline still needs to be filled. |

## Historical Evidence Captured On 2026-06-29

- `scripts/testnet/public-testnet-preflight.ps1 -ValidatorProfile 3` passed against `build/aetrad.exe` at commit `7afe0d3`.
- `scripts/testnet/public-testnet-preflight.ps1 -ValidatorProfile 5` passed against `build/aetrad.exe` at commit `7afe0d3`.
- `scripts/testnet/public-testnet-preflight.ps1 -ValidatorProfile 10` passed against `build/aetrad.exe` at commit `7afe0d3`.
- `scripts/testnet/public-testnet-preflight.ps1 -ValidatorProfile All` passed against `build/aetrad.exe` at commit `7afe0d3`.
- `tests/e2e/export_import_smoke.ps1` passed on a fresh localnet and verified export/import roundtrip behavior.
- Fresh clean-home onboarding smoke passed in a temporary user profile: `aetrad init`, `genesis validate-genesis`, `keys add`, and `keys show` all succeeded against the published genesis.
- Snapshot evidence was captured at `.work/snapshots/statesync-test-14.tar`.
- A state-sync restart on a 3-validator localnet reached `height=2` with `catching_up=False`.
- `gosec` reported `0` high-severity issues in the current tree.
- `govulncheck` reported only the repository-triaged GO IDs listed in `.github/security/govulncheck-triage.txt`.
- `gitleaks` passed after the repo-scoped allowlist in `.gitleaks.toml` was applied to the validator-registry consensus-key-length validation false positive.

## Current Drill Evidence Captured On 2026-07-01

Observed window: `2026-07-01T17:58:13Z` to `2026-07-01T18:08:37Z` UTC.
The canonical archive index is
[`README.md`](../.work/launch-evidence/20260701-snapshot-state-sync-validator-drill/evidence-pack/README.md).

| Metric | Owner | Status | Observed evidence |
| --- | --- | --- | --- |
| `app_hash` | Testnet SRE | captured | Height `1880`, app hash `C9D446A3D095411AA31DEAB7C5E253C326497972CD589A5593BAD497F9EDBE1F` |
| `finality_seconds` | Performance | captured | 11-sample run, node0 recomputed min `0.843s`, median `1.844s`, max `41.621s` |
| `missed_blocks` | Consensus ops | partial | `signing-infos.json` captures the live slashing state; the current CLI surface still does not provide a clean per-validator missed-block counter in one call |
| `peer_count` | Networking | captured | Peer count stayed at `2` throughout the telemetry window; current live status after the validator-smoke restart reports `3` peers |
| `snapshot_restore` | Release engineering | passed (2026-07-09) | Root cause of the 2026-07-01 AppHash panic: the drill skipped `comet bootstrap-state`, so CometBFT replayed from genesis against restored app state. The full offline flow (`snapshots load` -> `snapshots restore` -> `comet bootstrap-state` -> start) is now codified in `scripts/localnet/snapshot-restore-drill.ps1`; the re-run restored at height 4, synced to height 12, and matched the validator app hash (`.localnet-snapshot-restore-drill/evidence/snapshot-restore-drill.json`). Old failure logs retained in `snapshot-restore/logs.err.log` |
| `state_sync_restore` | Release engineering | captured | Previous drill completed a fresh-node join using published trust data; canonical note remains in `docs/testnet-incident-response.md` |
| `storage_rent_debt` | Protocol ops | deferred | No canonical exporter or operator query surface is wired yet |
| `system_rent_runway` | Protocol ops | deferred | No canonical runway query surface is wired yet |
| `pool_deposit_claim_unbond` | Staking ops | deferred | Official pool deposit/claim/unbond flow still needs a dedicated live operator drill |
| `incident_count` | Incident commander | captured | No incident was opened during the observation window |

Related archive files:

- `../.work/launch-evidence/20260701-snapshot-state-sync-validator-drill/evidence-pack/block-1880.json`
- `../.work/launch-evidence/20260701-snapshot-state-sync-validator-drill/evidence-pack/faucet-preview.report.json`
- `../.work/launch-evidence/20260701-snapshot-state-sync-validator-drill/evidence-pack/faucet-preview.tx.json`
- `../.work/launch-evidence/20260701-snapshot-state-sync-validator-drill/evidence-pack/telemetry.jsonl`
- `../.work/launch-evidence/20260701-snapshot-state-sync-validator-drill/evidence-pack/txsearch-1284.json`
- `../.work/launch-evidence/20260701-snapshot-state-sync-validator-drill/evidence-pack/txsearch-1880.json`
