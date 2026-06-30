# Public Testnet Long-Running Evidence

This checklist records the minimum evidence required before public testnet can
be promoted toward mainnet readiness. It is a placeholder until a live
long-running network exists, but every metric below must have an owner, source,
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

## Evidence Captured On 2026-06-29

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
