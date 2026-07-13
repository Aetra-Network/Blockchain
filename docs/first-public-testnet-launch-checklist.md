# First Public Testnet — Launch Checklist

Scope: the **validator-liveness** first public testnet (base chain only). AVM
smart contracts and official liquid-staking pools are deliberately deferred and
ship disabled/gated — see [TESTNET.md](TESTNET.md) and
[public-testnet-production-gates.md](public-testnet-production-gates.md).

Status legend: `[x]` done / green, `[ ]` outstanding, `[~]` partial.

## 1. Code & determinism gates (green)

- [x] `go build ./...` clean
- [x] `go vet` clean on touched packages
- [x] App invariants pass (`go test ./app -run Invariant`)
- [x] AVM determinism gate passes (`go test ./tests/avm_determinism_gate/...`)
- [x] Core economic + staking behavior tests pass (fee distribution, emission
      loop, consensus slashing reduces stake, jailed-earns-zero, direct
      delegation rejected, staking-reward withdrawal)
- [x] Generated testnet genesis ships AVM contracts **disabled**
      (`contracts.Params.Enabled = false`), asserted by the init-files test

## 2. Multi-validator liveness evidence (green)

- [x] 3 / 5 / 10-validator preflight passes end-to-end
      (`scripts\testnet\public-testnet-preflight.ps1 -ValidatorProfile All`):
      block production, RPC/gRPC/REST, peers, fee policy (wrong-denom rejected),
      pool-only staking policy (direct delegation rejected), restart continuity,
      CosmWasm-disabled smoke
- [~] Longer soak (hours → days) on 5–10 nodes to catch slow leaks / rare halts.
      A short soak has been run; a multi-day soak on the release binary is still
      required before public launch.
- [ ] Snapshot + state-sync restore from a published trust height/hash and at
      least two RPC servers (`scripts\localnet\state-sync-drill.ps1`,
      `scripts\localnet\snapshot-restore-drill.ps1`) — archive evidence
- [ ] Coordinated Cosmovisor upgrade rehearsal, evidence archived
      (`tests\e2e\upgrade_rehearsal_smoke.ps1`)

## 3. Security (outstanding)

- [x] Multi-scanner CI green / triaged (govulncheck, gosec, gitleaks, CodeQL,
      dependency review) — `docs\security\security-gates-triage.md`
- [ ] **Independent third-party security audit**; high/critical findings fixed
      or explicitly accepted by governance with public rationale
      (`docs\security\third-party-audit-status.md` is currently a placeholder)

## 4. Operational launch prerequisites (outstanding)

- [ ] Publish the Decision Record (per production-gates doc):
  - [ ] release commit + signed release binary
  - [ ] binary sha256 checksum
  - [ ] genesis hash
  - [ ] chain-id (small number `-19` for testnet, or `aetra-...`)
  - [ ] native denom `naet` confirmed
  - [ ] seed / persistent peers list (`scripts\testnet\validate-peer-lists.ps1`)
  - [ ] public RPC endpoints (≥ 2)
  - [ ] snapshot / state-sync trust height + trust hash
- [ ] Faucet: `aetrad faucet serve` deployed; faucet key held in a secret
      manager (never in the repo); rate limits confirmed
- [ ] Explorer / indexer: deferred-optional for validator liveness; RPC/REST is
      the required public surface (run `l1-explorer` on a non-validator node if
      desired)
- [ ] Monitoring deployed: Prometheus alerts + Grafana dashboard shipped under
      `observability/` — wire to the public nodes
- [ ] Incident response + rollback runbooks tested against the signed release
      binary (`docs\testnet-incident-response.md`)
- [ ] Fresh-machine validator onboarding tested from a clean home directory
      (`docs\validator-onboarding.md`,
      `scripts\localnet\validator-onboarding-drill.ps1`)

## 5. Final gate

- [ ] Objective readiness report green on **live** gates (not `-SkipLiveGates`):
      `scripts\testnet\public-testnet-readiness-report.ps1 -RunFullGates -RunLocalnetProfiles`
- [ ] No untriaged Critical/High fund-safety, consensus-safety, or secret-leak
      finding in the security triage ledger

## Explicitly out of scope for the first testnet

- AVM smart contracts / contract-standard assets (token, NFT, DEX) — enabled
  later via governance after AVM bytecode-verification + adversarial + audit
  gates
- Official liquid-staking pools / delegator staking — the pool subsystem is not
  yet economically live
- Sharding / Aether Mesh / execution zones / custom networking overlay —
  feature-gated, documented as experimental
