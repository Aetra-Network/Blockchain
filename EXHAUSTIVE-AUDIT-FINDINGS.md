# Aetra L1 — Exhaustive Multi-Agent Re-Audit (Pass 2.5)

**Date:** 2026-07-15 · **Base:** branch `remediation/pass2-security` (on top of `bc361fc9`)
**Method:** 13 adversarial finder agents (re-audit all 22 Pass-1 findings for bypasses + fresh hunt across money / consensus / contracts / ante / staking / addressing / determinism / crypto / gated modules + break-test of the 8 Pass-2 fixes) → dedup → 3-lens skeptic verification (exploitability / consensus-or-fund reachability / correctness). 35 candidates, 3-lens-confirmed subset below.

> The verify pass partially hit the account session limit, so some candidates are marked **UNVERIFIED** (limit-failed, not rejected). Treat those as leads.

---

## Fixed this session (committed on `remediation/pass2-security`)

| Finding | Fix | Commit |
|---|---|---|
| #3/#6 SA2-S05 regression — dup consensus key halts the auto-commit EndBlocker | reject the duplicate at submission (`ApplyForValidatorSet`) so a bad application is never stored | `9e806007` |
| #19 `ApplyPoolReward` unchecked uint64 commission multiply | use checked `MulDivUint64` | `be7eeccc` |
| #14 SA2-S02 frozen-stake bound is a production no-op | add `MaxFrozenStakesV1=512` hard cap in `Normalize` | `ea45434b` |
| #4 co-signature replay — native-account lifecycle handlers never increment the sequence | `next.Sequence++` in all 7 co-signed handlers | `5c680dc4` |
| #32 localnet/testnet validator commission = 100% | default to 5% / 20% / 1% | `22df43c1` |
| #22 auth policy accepts two keys sharing a public key | reject duplicate public keys in `validateAuthKeys` | `a8facbb8` |
| #26 evidence proof hash uses a NUL separator (non-injective) | length-prefix framing (matches FINDING-016) | `94075f71` |
| #20 `messageBeaconHash` concatenates variable-length fields without framing | length-prefix Source/Destination/Body | `0c11a618` |
| #8 kill-switch not enforced on top-up / pay-storage-debt / unfreeze | add the `Enabled` guard to all three | `156f5b6d` |
| **C-1 (CRITICAL)** + #25 | election override emits a per-block DELTA (imposed-set history in state), never re-removing an already-removed validator; validators keyed on the full pubkey. **New 2-block test proves no re-removal on block 2.** | `fc6394cb` |
| #5 (HIGH) AVM operand-gas counted only top-level fan-out (FINDING-001 bypass) | recursive value-size counting; nested-value test | `6c8a69dc` |
| #15 (LOW) determinism gate missed `time.Since`/`After`/`Tick`/`NewTimer`/`NewTicker` | added those package-qualified timer funcs; gate still green | `<gate>` |

**Validation:** the full test suite for every touched package (`x/validator-election`, `x/native-account`, `x/contracts`, `x/aetravm`, `x/nominator-pool`, `x/evidence`, `x/emissions`, `x/fees`, `app/...`, `cmd/l1d/...`) passes (exit 0), and `go build ./...` is green — all fixes integrate with no regression.

(Earlier Pass-2 fixes SA2-S02/S03/S04/S05/S06/S07/S08/I02 remain committed; see `SECOND-AUDIT-REPORT.md`.)

**#7 (TopUpContract free value)** — NOT a one-liner: `contract.Balance` credit needs collection into a **contract-balance escrow**, but `collectRentPayment` routes only to the rent reserve and is a no-op when `bankKeeper==nil`. This is the same value-custody design decision as #2/SA2-F01 (which module account backs contract/pool balances). Left for a deliberate change, not a mis-routing patch.

---

## RESOLVED — CRITICAL ✅

### C-1 · Election override re-emits stale validator removals every block → permanent CometBFT halt

**STATUS: FIXED in `fc6394cb`** (per-block delta + AppliedValidatorSet/PreviousAppliedValidatorSet history, validated by a 2-block test). The analysis below is what was built.
**`app/elector_validator_updates.go`** (confirmed 3/3 against vendored CometBFT v0.39.3).
`applyElectionValidatorUpdates` (called unconditionally from `FinalizeBlock`) rebuilds the **complete** removal set every block from stable sources — `addStakingValidatorRemovals` (all `GetAllValidators`) and `addElectionSetRemovals` (all `PreviousValidatorSet`) — emitting `Power:0` for the same non-elected validators on consecutive blocks. CometBFT rejects removing an already-removed validator (`validator_set.go` `verifyRemovals` / empty-set guard) → `updateState` errors → `consensus/state.go` `panic("failed to apply block")`. Deterministic → whole network halts on the block after any set change (a validator not re-applying, an exit, a second validator — normal operation, not just the repo's own single-genesis-validator test).

**Correct fix (designed, needs a dedicated 2+block test):** emit only the **delta** against the set last imposed on CometBFT.
1. Add `AppliedValidatorSet` **and** `PreviousAppliedValidatorSet` to the election `State` (sorted in `Normalize`).
2. In the election **EndBlocker** (writable deliver state), at the end (post-finalize): `PreviousAppliedValidatorSet = <old AppliedValidatorSet>`, `AppliedValidatorSet = CurrentValidatorSet`; save only on change.
3. The override reads the **in-flight** post-EndBlocker state (empirically confirmed: `NewUncachedContext` here sees this block's EndBlocker write, *not* committed H-1 as the finding assumed). So the override uses `PreviousAppliedValidatorSet` as the baseline; removals = `baseline − elected`; on the very first override (`PreviousAppliedValidatorSet` empty) fall back to `GetAllValidators` **plus** `res.ValidatorUpdates` (staking's in-flight genesis bonding, not yet in the committed store at height 1).
4. Also fix **#25**: `validatorUpdateKey` must key on the full proto-encoded pubkey (`PubKey.Marshal()`), not `GetEd25519()` (which collapses all non-ed25519 keys into one bucket).

*(A first implementation attempt was reverted because it needs a proper multi-block CometBFT-level test to validate the in-flight read/write semantics — the existing `TestValidatorElectionCurrentSetControlsFinalizeBlockValidatorUpdates` only runs one FinalizeBlock and gives false confidence.)*

---

## OPEN — HIGH

| ID | Finding | Location | Fix |
|---|---|---|---|
| #2 (SA2-N01) | Nominator-pool is a no-bank-custody ledger: deposits credit shares/stake with no debit; claims/withdrawals move no coins | `x/nominator-pool/keeper/keeper.go:575` | Wire a BankKeeper; move real coins to/from a module account on every value path + per-pool balance invariant. Interim: gate the deposit/claim Msgs. (= SA2-F01) |
| #5 | FINDING-001 gas bypass: AVM operand-gas meter counts only top-level fan-out, not recursive node/byte count | `x/aetravm/avm/value.go:187` | Charge by recursive total node-count/bytes, or hard-cap total in-stack value size on every push/clone. (Latent: AVM off on testnet.) |

## OPEN — MEDIUM

| ID | Finding | Location | Fix |
|---|---|---|---|
| #7 | `MsgTopUpContract` credits `contract.Balance` with no bank collection (free value) | `x/contracts/keeper/keeper.go:1616` | Collect `msg.Amount` from `msg.Sender` before crediting (mirror `PayContractStorageDebt`). *(parallel-session code)* |
| #8 | Kill-switch (`contracts.Params.Enabled`) not enforced on `TopUpContract`/`PayContractStorageDebt`/`unfreezeContract` | `x/contracts/keeper/keeper.go:1602,1642,1702` | Add the `!Enabled → disabled` guard at the top of each. *(parallel-session code)* |
| #9 | Determinism gate can't detect map-iteration order (the primary consensus-fork class) | `app/security_attack_audit_test.go:18` | AST pass flagging `range <mapTyped>` on the write path. |
| #10 | `x/stake-concentration` limits are advisory-only (never enforced on deposits/rewards) | `x/stake-concentration/keeper/keeper.go:119` | Consult `CanAcceptDelegation`/`RewardModifierBps` in the pool deposit/reward paths. (= SA2-F03) |
| #11 | delegator-protection compensation uncallable; real emission AET stranded | `x/delegator-protection/types/protection.go:491` | Back `Fund.Balance` with the real `protection` module account. |
| #12 (SA2-N02) | AVM `random()` beacon = current block hash → proposer-biasable | `x/aetravm/avm/host.go:354` | VRF / commit-reveal / previous-block entropy. (Latent: AVM off.) |
| #13 (FINDING-009) | Whole-`State` blob re-serialized on every write (open, broader than documented) | `x/nominator-pool/keeper/keeper.go:168` | Per-entity KV keys (= SA2-F07). |

## OPEN — LOW / INFO

| ID | Finding | Location |
|---|---|---|
| #15/#16/#17 | Determinism gate misses `time.Since`/`time.Unix`, named-func goroutines, and client/cli dirs | `app/security_attack_audit_test.go` |
| #18 | dynamic-commission effective rate computed but never applied to reward distribution | `x/dynamic-commission/keeper/keeper.go:121` |
| #20 | `messageBeaconHash` concatenates variable-length fields without length framing (collision) | `x/aetravm/avm/host.go:332` |
| #21 | Stake-weighted per-sender admission limit inert: `SenderStake` hardcoded to 0 | `x/fees/keeper/fee_policy.go:64` |
| #22 | native-account auth policy accepts multiple AuthKeys sharing one public key | `x/native-account/types/auth_policy_authorize.go:135` |
| #23 (I01) | Emergency protocol-mint authorization is self-certified, never checked vs `x/constitution` state | `x/mint-authority/types/authority.go:635` |
| #24 | SA2-S08 clamps the response but still copies+sorts the whole store per query | `x/nominator-pool/keeper/query_server.go:36` |
| #25 | Override keys removals on `GetEd25519()`, collapsing non-ed25519 keys (fold into C-1 fix) | `app/elector_validator_updates.go:118` |
| #26 | FINDING-016 hash-separator class still present in x/evidence | `x/evidence/keeper/keeper.go:779` |
| #27 | Stake-weighted tx priority (`PriorityScore`) is dead code, never applied to mempool | `x/fees/keeper/fee_policy.go:179` |

## Inaccuracies (severity=info)

**RESOLVED ✅ in `e129f0c3`** — canonical source pinned in code at both sites (`x/aetra-economics/types/state.go` DefaultParams, `x/fees/types/genesis.go`): `x/emissions`+`app/params` and `x/fee-collector` are declared AUTHORITATIVE; `x/aetra-economics` is declared ADVISORY/self-contained. The numbers were never broken — the defect was ambiguity about which source is real, and that is now unambiguous. Rationale below.

**Resolution decision:** #29/#30/#33/#34 are NOT bugs but two coexisting economic models — `x/aetra-economics` is a self-contained, tested model (3.5% inflation midpoint, block-based epochs, 35% share, with tests asserting `midpoint=350`, `APR(400,6000)=667`) that intentionally differs from the live `x/emissions`/`fee-collector` drivers (3%, daily epochs, 70%, 50/35/15). Force-aligning would break a coherent tested module and is an economic-model decision, not a patch. **Canonical = `x/emissions` + `x/fee-collector`** (the live drivers); reconciling `x/aetra-economics`/residual `x/fees` accounting to them should be a deliberate, coordinated change with its tests updated. Documented, not force-patched.

**#5/#12/#13/#21/#23 + gate hardening (#9/#15/#16/#17):** dedicated passes — #5 (recursive AVM gas) and #23 (mint signer-binding) are real fixes with real test surface; #12 (VRF) and #13 (per-entity KV) are larger; making the determinism gate stricter may surface existing code and needs a controlled run. All latent (AVM off) or non-consensus. **#7/#2 (contract/pool custody):** feature work (which module account backs balances) — SA2-F01, not a patch.


| ID | Inaccuracy | Location |
|---|---|---|
| #29 | Dual inflation defaults: x/emissions 3.0% target vs x/aetra-economics 3.5% midpoint | `x/aetra-economics/types/state.go:169` |
| #30 | Dual fee-split: x/fees 98/2 accounting vs fee-collector 50/35/15 | `x/fees/keeper/keeper.go:239` |
| #31 | Uncalibrated `AnnualReferenceSupply = 365 AET` makes emission negligible | `app/params/mint.go:15` |
| #32 | Localnet/testnet gentx sets validator commission to 100% | `cmd/l1d/cmd/testnet.go:493` |
| #33 | `EstimateAPRBps` overstates APR by ignoring the 70% validator/delegator share | `x/aetra-economics/types/state.go:218` |
| #34 | Dual `EpochsPerYear` constants: 365 (emissions) vs 6,307,200 (aetra-economics) | `x/aetra-economics/types/state.go:160` |

## Verified genuinely fixed (no action)
Pass-1 findings **002–008, 010, 011** re-verified fixed in the current working tree (#28). Prototype gate (#35): dormant zones/routing/mesh/sharding-coordinator/networking/load modules cannot be triggered — no live Msg/Query/EndBlock surface; gate off by default (negative result confirmed).

---

## UNVERIFIED leads (verify pass hit the session limit — re-check)
`ApplyPoolReward`/`Cosignature`/`Nativeaccountauth`/`Determinismgate`/`Gate*`/`AVMrandombeacon`/`messageBeaconHash`/`SA2-S05`/`SA2-S02-FrozenStakes`/`SA2-S08`/`Dual*`/`Uncalibrated`/`Localnet`/`EstimateAPR`/`Prototypegate`/`SA2-N01` — most correspond to findings already listed above (several now fixed).
