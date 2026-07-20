# Aetra L1 — Second-Pass Security Audit & Remediation Report

**Type:** Internal follow-up review (Audit Pass 2) — *not a third-party attestation*
**Date:** 2026-07-15
**Commit under review:** `d77f1b2b` (HEAD of `main`)
**Stack:** Cosmos SDK v0.54.3 / CometBFT v0.39.3 · native token AET (1 AET = 1e9 naet) · valuation basis $0.01/AET
**Method:** `cosmos-vulnerability-scanner` skill (1 discovery + 3 pattern scanners, §1–27) · 2 independent fix-verification passes · a live 3-node localnet driven to height 3,167+ under a 100-tx RPC flood · whole-tree `go build` + targeted security regression tests.

> **Scope note.** This document is the second pass over the codebase, building on the 22-finding Pass-1 audit (`security-audit/` on branch `security-audit-2026-07`). It (a) confirms which Pass-1 fixes are real, (b) records new findings from a fresh independent scan, and (c) is primarily a **prioritized remediation & implementation backlog** — everything still to be fixed, finished, or built before the feature-complete network and mainnet. An external third-party audit is still required and is listed as a launch gate (the project's own `mainnet_readiness.go` correctly reports `security_audit_completed=false`).
>
> **Addendum (same day, commit `bc361fc9`).** §3.5 below adds 4 more findings from a live end-to-end verification pass (deploy/execute/query a real contract against a running localnet, not a static scan) — all 4 are already fixed as of this commit. SA2-S15 in §4 is resolved as part of that pass. Everything else in this document (§1–§3, §4 minus S15, §5, §6) still describes `d77f1b2b` and is unchanged.
>
> **Remediation progress (branch `remediation/pass2-security`, 8 fixes landed — each with a regression test and a scoped commit; whole-tree `go build` green):**
> - **SA2-S04** bound election voting-power params to the CometBFT `MaxInt64/8` ceiling — `a08b187c`
> - **SA2-S05** reject duplicate consensus keys in validator-set validation — `b532b558`
> - **SA2-I02** underflow-safe election total-power cap check — `b6e55ca0`
> - **SA2-S03** no chain halt on a committed-but-empty election — `a7f96042`
> - **SA2-S02** bound the unbounded election history slices — `dc77d56d`
> - **SA2-S07** require a non-zero gov `MinInitialDepositRatio` — `eab584a5`
> - **SA2-S06** disable the desync-causing standalone emission-finalize message — `b2ba8f47`
> - **SA2-S08** bound the nominator-pool list-query page size — `f253b516`
>
> Still open: **SA2-I01** (needs a mint-authorization redesign, not a quick patch), **SA2-S14** (CI regression guards), the `x/contracts`-touching **SA2-S13 / S12 / N02** (coordinate with in-flight contract work), **SA2-N01a** (couples with SA2-F01), and the **F01–F07** feature tracks.

---

## 1. Executive Summary

The **deployed/committed code is genuinely hardened.** Of the 22 Pass-1 findings, **21 are verifiably closed** with regression tests (the Critical in-memory-genesis rehydration confirmed across 15 modules / 46 call-sites, restart-divergence test passing live). A fresh consensus-critical scan found **no attacker-triggerable Critical/High**; every live `bank` mint/burn/send path conserves value, so **there is no live fund-loss or supply-inflation vector**. The live network burns fees (exact 50%), meters fees, throttles per-sender spam, and stays block-time-stable under load.

The remaining work is **deferred features and calibration, not shipped defects.** The material items are: one un-fixed Medium (write amplification), a cluster of governance foot-guns and latent (AVM-off) hardening items from the fresh scan, and the large feature gaps the project already labels as incomplete — liquid/pooled staking has no token custody, the emission reference supply is a placeholder, concentration caps are advisory-only, and zones/sharding is dormant scaffolding.

**Verdict by scope:**

| Target | Readiness | Blockers |
|---|---|---|
| Validator-liveness testnet (AVM-off, self-bond only) | **Sound** | Operational gates only (external audit, soak, rollback drill) |
| Feature testnet (contracts + pooled staking) | **Materially incomplete** | SA2-F01, SA2-F04, SA2-S01/S02 |
| Mainnet | **Not ready** | All P0/P1 + external audit + calibration (SA2-F02/F03) |

**Headline live metrics:** block time 1.22 s dev / 5–8 s production target · ~130 tx/block (gas-bound) → ~22–107 TPS · genesis supply = validators × 1,000 AET (no premine) · fee split 50/35/15 (burn/validators/treasury) · inflation 3% · Nakamoto(⅓) 34 at equal stake / 3–10 under realistic skew.

---

## 2. Status of Pass-1 Findings (22)

| ID | Severity | Status | Evidence |
|---|---|---|---|
| 001 AVM gas mispricing DoS | High | ✅ Fixed | operand-proportional pricing; `gas_mispricing_dos_test` PASS. *Residual → SA2-S12.* |
| 002 Invariants never run | Low | ✅ Fixed | wired into EndBlock, fail-soft (log+event, no halt, deliberate) |
| 003 Emission cap misscoped | Low | ✅ Fixed | multi-year lifetime cap math |
| 004 StoreCode no decode/verify | Low | ✅ Fixed | real `DecodeModule + Verify` |
| 005 Economics stale state | Low | ✅ Fixed | reads live ctx store |
| 006 In-memory genesis not rehydrated | **Critical** | ✅ Fixed | `loadForBlock` in every mutating handler + both EndBlockers, 15 modules / 46 sites; restart test PASS. *Residual → SA2-S13.* |
| 007 Phantom-state resurrection | High | ✅ Fixed | reload-at-entry; test PASS (effective, not atomic-rollback) |
| 008 Contracts query data race | High/Med | ✅ Fixed | RWMutex + snapshot/assign; `-race` not run here (no gcc) |
| 009 Write amplification | Medium | ❌ **Deferred** | only skip-if-identical guard; O(N)/write remains → **SA2-S01** |
| 010 Unmetered query deep-copy | Medium | ✅ Fixed | clone after page truncation |
| 011 Fee exceeds hard cap | Medium | ✅ Fixed | clamp in `ComputeFullTransferFee` |
| 012 Non-canonical bech32 desync | Medium | ✅ Fixed | canonicalize + SDK verifier wired |
| 013 Authz nesting evades caps | Low | ✅ Fixed | recursive `WalkMessages` |
| 014 Zero-address nested authz | Info | ✅ Fixed | walk nested before check |
| 015 Faucet proxy rate-limit | Low | ✅ Fixed | opt-in trusted-proxy |
| 016 Actor-registry hash separator | Low | ✅ Fixed | length-prefixed framing |
| 017 Vanity-address fund loss | Low | ✅ Fixed | `CanReceiveUserFunds=false` + allowlist |
| 018a/b/c CI hardening | Low | ✅ Fixed | triage expiry, pinned scanners, scoped tokens. *Residual → SA2-S14.* |

**Net: 21/22 closed. FINDING-009 remains open (SA2-S01).**

---

## 3. New Findings (Pass-2 Fresh Scan)

No attacker-triggerable Critical/High. All items below are governance-gated, latent (AVM-off), design-deferred, or non-consensus.

| ID | Severity | Finding | Reachability |
|---|---|---|---|
| SA2-S01 | Medium | Nominator-pool state write-amplification (== FINDING-009) | Consensus, bounded by caps |
| SA2-N01 | **High (design)** | Pooled staking ledger has **no `x/bank` custody** — deposits credit shares with no debit; claims/withdrawals move no coins | Msg surface live; pool gov-gated; **not a live supply break** (no bank ops) |
| SA2-S02 | Medium | Election-state slices (`ElectionResults`, `RewardDistributionSnapshots`, `FrozenStakes`) grow unbounded, re-validated every `FinalizeBlock` → slow-ABCI liveness creep | Only if elected-set path is activated |
| SA2-S03 | Medium | Auto-`EndBlock` halt when a committed election yields an empty next-set | Governance-authored only |
| SA2-S04 | Low | No upper bound on `MaxValidatorPower`/`MaxTotalVotingPower` vs CometBFT `MaxTotalVotingPower` → gov-settable `FinalizeBlock` halt | Gov misconfig |
| SA2-S05 | Low | Validator set deduplicated by operator, not consensus key → recorded-set / CometBFT divergence | Gov-authored |
| SA2-S06 | Low | Emission dual-finalize desync: `MsgFinalizeEmissionEpoch` records + advances inflation without minting or cap-check | Gov only (under-mints, no inflation exploit) |
| SA2-N02 | Low → Med (latent) | `RandomBeacon` entropy = current block hash → proposer-biasable `random()` | Zero now (AVM off); Med once an RNG contract deploys |
| SA2-S07 | Low | Gov proposal spam: `MinInitialDepositRatio = 0` | Permissionless proposal spam |
| SA2-S08 | Low | Unbounded list queries (`nominator-pool.NominatorPools` + ~8 others) | Query-side DoS, non-consensus |
| SA2-I01 | Info | `x/mint-authority` authorizes on a caller-computable hash, not the tx signer — safe today only because `"x/emissions"` is an unsignable address | Latent if emergency-mint enabled or codec changes |
| SA2-I02 | Info | `validator-election` total-power cap underflow (`keeper.go:470`) admits a candidate past the cap | Requires per-val cap > total cap misconfig |

---

## 3.5 Pass-2 Follow-up — Live-Verification Findings (found *and* fixed same day)

A same-day follow-up session drove a **live contract deploy → execute → query cycle** on a running 3-node localnet (not a static scan) specifically to check the "is it actually usable end-to-end" question this report's Pass-1/Pass-2 tables don't cover. That surfaced 4 more bugs beyond §2/§3, none caught by either scan pass because none are visible from reading the code in isolation — each only shows up when a real transaction is actually signed, broadcast, and executed. **All 4 are fixed, tested, and pushed** as commit `bc361fc9` (on top of `d77f1b2b`). Listed here so the fix commits are traceable and nobody re-discovers them from scratch.

| ID | Severity | Finding | Evidence it's fixed |
|---|---|---|---|
| SA2-S16 | **High** | `x/fees/keeper/ante.go`'s `validateNoZeroMsgAddresses` required the strict "AE..." user-facing address form for stock cosmos-sdk message fields (`banktypes.MsgSend.FromAddress/ToAddress`, `MsgMultiSend`, `distrtypes.MsgSetWithdrawAddress`) that the SDK always populates in native bech32 ("ae1...") form — **every ordinary bank send was unconditionally rejected.** Fails safe (rejects, doesn't misroute funds), but is a total break of the chain's most basic operation. | New `validateWellFormedNonZeroAddress` helper accepts either address form; new test `TestAnteHandlerDecoratorAcceptsBech32BankSendAddresses`; live-verified with a real transfer + balance-delta check on localnet |
| SA2-S17 | **High** | `x/contracts/keeper.ensureActiveWallet` checked native-account activation status by the caller's *plain* account address, but `x/native-account` records activation under the account's derived *v2 identity* — the same plain-vs-v2 split already solved in `x/nominator-pool` ([[nominator-pool-staking-writes]] Gap2). Any genuinely-activated wallet got `contracts_account_inactive` on **every** contract operation (store-code, deploy, execute, unfreeze) — contracts were unusable end-to-end regardless of Pass-1/Pass-2's SA2-F04 AVM-enablement status. | `normalizeAccountIdentity` added to `x/contracts/keeper`, mirroring `x/nominator-pool`'s existing helper; live-verified: activated wallet successfully ran `store-code → deploy → execute` on localnet |
| SA2-S18 | Medium | `l1d query avm contract` / `query avm code` crashed with `"types.Contract does not implement proto.Message"` (or `CodeRecord`) even though the underlying gRPC query succeeded — 8 nested response value-types (`Contract`, `CodeRecord`, `Params`, `PageRequest`, `StateInit`, `CodeDependency`, `ContractStorageEntry`, `ContractReceipt`) had wire-format `Marshal/Size/Unmarshal` but no `Reset()/String()/ProtoMessage()`, which `clientCtx.PrintProto`'s JSON path needs at every level of the nested object graph. CLI/tooling-only — the query itself was never broken, only printing the result. | Added the 3-method triple to all 8 types in `x/contracts/types/query_marshal.go`; live-verified: `query avm contract <addr>` prints full contract state (owner, status, storage, etc.) |
| SA2-S19 | Medium-High | `MsgTopUpContract` / `MsgPayContractStorageDebt` / `MsgUnfreezeContract` had working, unit-tested keeper logic but were **never registered** on the msg-service (`types/service.go`'s `GRPCMsgServer`/`Msg_serviceDesc`), never handled in `keeper/grpc_server.go`, and had no CLI command — so a contract that ran out of balance and accrued storage-rent debt (a normal, expected outcome of the storage-rent economic model under low balance, not an attack) had **zero live recovery path**. Effectively a permanent-brick footgun for any contract operator who under-funds their contract. This is also what SA2-S15 above was blocking on mid-fix. | Wired end-to-end following the existing `UpdateContractParams` pattern (interface + dispatch + wire descriptors + `codec.go` registration + `grpc_server.go` handlers + new `grpc_server_test.go` + CLI `tx avm top-up/pay-debt/unfreeze`); live-verified on localnet: a contract that hit real rent debt was recovered via `pay-debt` (code=0) → `top-up` (code=0) → subsequent `execute` succeeded (code=0, receipt exit_code=0) |

Also fixed in the same pass: a **pre-existing** (unrelated to the above) nil-`Codec` panic in `cmd/l1d/cmd/avm_test.go`'s `TestAVMCLIE2ESmokeDeployExecuteQuery` — query commands have no dry-run mode and need a real `client.Context`, which the test never provided. Fixed by exercising the query against a real in-process gRPC server instead of a bare `client.Context{}`.

**Net effect on this report's headline verdict:** the "Feature testnet (contracts + pooled staking)" row in §1's table listed SA2-F01/F04/S01/S02 as blockers; contracts are now demonstrably usable end-to-end where they weren't before this pass (SA2-S16/S17/S19 were all-of-contracts blockers, not edge cases), but SA2-F01 (pooled staking has no bank custody) and SA2-F04 (AVM off by design on public testnet) are unchanged and still gate that row.

---

## 4. Remediation Backlog — Security (prioritized)

**Effort key:** S ≤ ½ day · M ≈ 1–3 days · L ≈ 1–2 weeks · XL > 2 weeks.

### P0 — Before the next commit / any tag

| ID | Item | Location | Remediation | Effort | Acceptance |
|---|---|---|---|---|---|
| SA2-S15 | ~~Working tree does not construct the app~~ — **RESOLVED 2026-07-15, commit `bc361fc9`** — in-flight `TopUpContract` left `MsgTopUpContract` registered on the msg-service before `RegisterInterfaces`, panicking `app.Setup` | `x/contracts/types/service.go`, `codec.go`, `keeper/grpc_server.go` | Fixed as part of finishing the TopUp/PayDebt/Unfreeze wiring — see SA2-S19 below | S | ✅ `app.Setup` no longer panics; `go build ./...` + `go test ./...` (123 packages) both clean |
| SA2-N01a | Gate the **live but unbacked** pool deposit/claim Msg surface until custody exists (interim safety) | `x/nominator-pool/module.go:50`, `x/single-nominator-pool` | Reject `MsgDepositToStakingPool`/claim/withdraw with `feature disabled` until SA2-F01 lands, OR keep pool creation gov-only and document that shares are unbacked | S | No wallet can obtain unbacked shares on a running node |

### P1 — Before a feature (contracts + pooled-staking) testnet

| ID | Item | Location | Remediation | Effort | Acceptance |
|---|---|---|---|---|---|
| SA2-S01 | Write amplification (FINDING-009): whole `State` blob + every pool/share re-serialized per write | `x/nominator-pool/keeper/keeper.go:172-194`; `prefixgenesis/store.go:69-96` | Migrate the module from single-blob genesis to per-entity KV (write only touched keys) | L | O(1)/write for a single-pool deposit; new bench test asserts write cost is page-bounded |
| SA2-S02 | Prune unbounded election-state slices | `x/validator-election/types/state.go:349-363` (`Normalize`), `Validate` | Bound `ElectionResults` / `RewardDistributionSnapshots` / released `FrozenStakes` to windows (as already done for `TransitionHistory`); ideally move off the per-block whole-blob load path | M | Per-block load+validate cost is O(window), not O(all epochs); test with 10k epochs |
| SA2-S03 | Empty committed-election auto-halt | `x/validator-election/keeper/keeper.go:219-221, 391-424` | Skip auto-commit when `computeNextSet` is empty, or treat "committed but empty" as finalizable | S | Regression: apply+exit same operators in one window → no halt |
| SA2-S12 | Meter AVM `exec.GasUsed` into the SDK gas meter (FINDING-001 #4) | `x/contracts/keeper/keeper.go:1479` | Consume AVM gas from the tx gas meter so fees reflect VM work (currently observability-only) | M | Fee for a heavy contract call scales with `GasUsed`; test |
| SA2-S13 | FINDING-006 residual: contracts EndBlocker reads `k.genesis.Params` before `loadForBlock` | `x/contracts/keeper/keeper.go:2166-2168` | Call `loadForBlock(ctx)` first, then read the drain budget | S | Restarted node with gov-enabled drain does not diverge |
| SA2-N02 | `RandomBeacon` proposer-bias — must land **before** any randomness-dependent contract | `x/aetravm/avm/host.go:319-357`; `x/contracts/keeper/keeper.go:1359-1362` | Replace current-block-hash entropy with VRF, commit-reveal, or previous-block/finalized entropy the proposer cannot grind | M | A lottery contract's outcome is not proposer-controllable; determinism preserved |

### P2 — Before mainnet (governance foot-guns & DoS)

| ID | Item | Location | Remediation | Effort |
|---|---|---|---|---|
| SA2-S04 | Cap `MaxValidatorPower`/`MaxTotalVotingPower` below `cmttypes.MaxTotalVotingPower` in `Params.Validate` | `x/validator-election/types/state.go:185-214` | Add upper-bound checks | S |
| SA2-S05 | Reject duplicate consensus keys in `validateValidatorSet` | `x/validator-election/types/state.go:466-490` | Add `seenConsensusKey` set | S |
| SA2-S06 | Make `MsgFinalizeEmissionEpoch` route through the app mint+cap path, or remove it | `x/emissions/keeper/msg_server.go:38`; `app/native_economy.go:137` | Single finalize authority | S |
| SA2-S07 | Set `MinInitialDepositRatio` (e.g. 0.25) | `app/genesisconfig/defaults.go:22` | Override SDK default | S |
| SA2-S08 | Clamp list-query pagination (`Limit==0` ≠ "all"; cap large `Limit`) | `x/nominator-pool/keeper/query_server.go:28-55` + ~8 queries | Apply `query.ForwardPageBounds` / `prototype.NormalizePage` | S |
| SA2-I01 | Bind `x/mint-authority` mint to the actual tx signer; treat decision/constitution artifacts as signatures, not recomputable hashes | `x/mint-authority/keeper/keeper.go:71`; `authority.go:600-646` | Signer-binding check | M |
| SA2-I02 | Guard the total-power subtraction against uint64 underflow | `x/validator-election/keeper/keeper.go:470` | `checkedSub` | S |
| SA2-S14 | Add CI anti-regression guards for 018a/b/c (actionlint / pin-policy lint / triage-format test) | `.github/workflows/` | New CI job/tests | S |

---

## 5. Feature-Completion Backlog (what still needs to be *implemented*)

These are the deferred-feature gaps that separate the current validator-liveness chain from the advertised feature-complete network.

| ID | Feature | Current reality | What to build | Effort |
|---|---|---|---|---|
| SA2-F01 | **Liquid / pooled staking made economically live** | In-memory accounting only: no `x/bank` custody, no cosmos delegation, rewards do not flow (`keeper.go:1500-1514`) | Wire real custody (`SendCoinsFromAccountToModule` on deposit; module→account on claim/withdraw), real staking delegation, auto reward distribution in `EndBlock` (`SyncPoolRewards`), the e2e slashing bridge into the pool, and a per-pool bank-balance invariant like `x/fee-collector.AssertModuleAccountingInvariant` | L |
| SA2-F02 | **Emission calibration** | `AnnualReferenceSupply = 365 AET` placeholder (`mint.go:15`); adaptive inflation inert because the auto-path feeds `stakingRatio = target` (`native_economy.go:146`) | Set the reference to true circulating supply; feed the real bonded ratio so inflation actually adapts within [1.5%, 5%] | M |
| SA2-F03 | **Enforced decentralization (raise Nakamoto structurally)** | Concentration caps (33.34% single / 15% soft / 67% top-N) and `x/stake-concentration` are advisory/dormant; commission-floor & self-bond are policy structs not wired to `x/staking` | Wire `x/stake-concentration` reward-modifier + delegation-rejection into distribution/delegation; bind commission-floor (3%) and self-bond (10k AET) into `x/staking` genesis; enable the `applyVotingPowerCap`/reward-dampening path | L |
| SA2-F04 | **AVM mainnet enablement** | `Enabled=false` on public testnet by design; query surface + wallet-normalization work uncommitted; internal-message autonomy off (`MaxInternalMessageGasPerBlock=0`) | Commit the uncommitted query/wallet fixes; gov-enable contracts after AVM audit gates; set a safe internal-message gas budget with SA2-S12 metering in place | M |
| SA2-F05 | **Zones / sharding (horizontal scaling)** | Dormant, honestly-labeled prototype; monolithic single-block pipeline; only fee-congestion backpressure is live | Either build the execution/routing/mesh runtime (per-shard block production, dynamic split/merge, cross-zone messaging) — a major, multi-quarter effort — or keep it explicitly dormant and stop advertising horizontal scaling | XL / defer |
| SA2-F06 | **Reconcile duplicate economic params** | `x/emissions` (3% target) vs `x/aetra-economics` (3.5% midpoint); canonical fee-collector 50/35/15 vs residual `x/fees` 98/2 | Pick one source of truth per parameter; delete/alias the other | S |
| SA2-F07 | **In-memory blob → per-entity KV (systemic)** | ~27 modules serialize whole state under one key + in-memory `k.genesis`; SA2-S01 is one instance | Adopt per-entity KV across the pattern (also removes the write-amp and query-clone classes wholesale) | XL |

---

## 6. Operational / Launch-Gate Items (non-code)

| ID | Item | Status |
|---|---|---|
| SA2-O01 | Independent third-party security audit | **Not done** — required before mainnet (`security_audit_completed=false`) |
| SA2-O02 | Multi-day soak on a 100-validator localnet | Only 10-min / 5-node soak done |
| SA2-O03 | Rollback drill on a signed release binary | Not performed |
| SA2-O04 | Real (non-noop) state-migration upgrade rehearsal | Only noop upgrade rehearsed |
| SA2-O05 | Localnet gentx commission = 100% (dev-tooling artifact) → set a sane default | Cosmetic but should be fixed before onboarding docs go public |

---

## 7. Suggested Sequencing

1. ~~**Now:** SA2-S15 (unblock the build)~~ — **done** (commit `bc361fc9`, alongside SA2-S16/S17/S18/S19). Remaining from this step: SA2-N01a (gate unbacked pool Msgs).
2. **Sprint 1 (security hardening):** SA2-S02, S03, S13, then S04–S08, I01, I02, S14.
3. **Sprint 2 (scalability):** SA2-S01, and evaluate SA2-F07 (per-entity KV) as the umbrella fix.
4. **Feature track (parallel, gated behind audit):** SA2-F01 (staking custody) → SA2-F02 (calibration) → SA2-F04 (AVM) → SA2-N02/S12 (VRF + gas metering) before enabling RNG contracts.
5. **Decentralization track:** SA2-F03 to move Nakamoto from "average" to structurally above-average.
6. **Pre-mainnet gates:** SA2-O01…O05 + SA2-F06.
7. **Long-horizon / optional:** SA2-F05 (real sharding) — only if horizontal scaling is a genuine product goal; otherwise keep dormant and de-advertise.

---

## Appendix A — Evidence & Live Metrics

- **Verification:** `go build ./...` clean. Regression: `x/nominator-pool` restart/phantom (006/007) PASS · `x/aetravm/avm` gas-mispricing (001) PASS · `x/contracts` 85 + `x/aetravm` 613 tests PASS.
- **Live localnet (`aetra-local-1`, commit `d77f1b2b`, 3 nodes):** height 2,145 → 3,167+ stable; block time 1.22 s at `timeout_commit=1s`.
- **Admission caps (live):** `max_block_gas=20,000,000` (binding), `max_block_txs=5,000`, `max_tx_gas=1,000,000`; CometBFT `max_gas=-1`, `max_bytes=22,020,096`.
- **Flood test:** 100 tx @ 76 tx/s; single sender capped by stake-weighted per-sender limit; block time stable ~1.2 s under load; **7.5 AET burned = exactly 50% of 25×0.6 AET fees**; cumulative burn since genesis 19.65 AET (3,000 → 2,980.35).
- **Live contract/reputation query surface:** `GET /l1/contracts/v1/params` → `{"enabled":true,…}` (the Pass-1 "Not Implemented" gap was a wrong REST path + stale binary).
- **Staking (live):** 3 validators, 100 AET bonded each (300 AET, 10% ratio), `max_validators=128`, unbonding 21 days.

## Appendix B — Tokenomics Reference (1 AET = $0.01)

- Genesis supply = validators × 1,000 AET (no premine): 3 val → 3,000 AET ($30); 100 val → 100,000 AET ($1,000).
- Inflation 3% (band 1.5–5%) → emission +10.95 AET/yr ($0.11); emission burn 5% (−0.5475 AET/yr); net +10.40 AET/yr (+2.85% on the 365-AET reference, ~+0.1% on a real network).
- Fee burn = 50% of every fee (second, usage-scaling deflation stream). Fee split 50/35/15 burn/validators/treasury.
- Typical transfer ≈ 0.5 AET ($0.005) = 0.25 burn + 0.175 validators + 0.075 treasury. Hard cap 5 AET ($0.05).
- Validator commission 5%/10%/20% (floor/default/ceiling). Staker APR ~5% @60% bonded (inflation-only) / ~3.5% after 70% validator share.
- Pool example (10 × 10,000 AET, 10% commission): formula says 5,000 AET reward (500 validator / 4,500 stakers, 450 each); **chain currently pays 0** (pool not economically live) — closing SA2-F01 + SA2-F02 is what makes (A) real.

---

*Prepared as an internal second-pass review. Findings are reproducible from the commit and live-node evidence above. This report supersedes no third-party audit and does not itself constitute one.*
