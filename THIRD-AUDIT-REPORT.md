# Aetra L1 — Third Audit: Live Network, Economics, Decentralization (Pass 3)

**Date:** 2026-07-16 · **Base:** `remediation/pass2-security` @ `ec6dcdf7`
**Method:** 7 skill-driven audit agents (`cosmos-vulnerability-scanner` ×2, `entry-point-analyzer`,
`state-invariant-detection`, `client-auditor`, `code-maturity-assessor`, zones/sharding deep-read)
**plus a live 4-validator network** (`l1d testnet init-files --validator-count 4 --single-host`,
chain-id `-19`, commit-timeout 5s) driven by a purpose-built pre-signing load generator
([tools/loadgen](tools/loadgen/main.go)).

Everything in the **Live measurements** and **Economics** sections is *measured on a running chain*,
not derived from reading code. Where a prior audit's number disagrees with the live one, the live
one wins and the discrepancy is called out.

**Price basis for this report: 1 AET = $0.01** (i.e. 1 naet = $10⁻¹¹; 1 AET = 10⁹ naet,
[app/params/token.go:7-11](app/params/token.go)).

---

## Remediation status (this session, same run)

Every finding below marked **FIXED** was fixed, built, unit-tested, and where noted
**live-verified** on a fresh 4-validator network in this same session, on branch
`remediation/pass2-security`. Commits: `8b8515b9`, `02e4c45d`, `d765937e`, `023f261d`, `04dba99d`.

| Finding | Status | What changed |
|---|---|---|
| **F-03** exit lockout | ✅ FIXED, live-verified | `ValidateUndelegate`/`ValidateBeginRedelegate` now exempt self-bond/pool, mirroring `ValidateDelegate`. Live: a validator unbonded its own self-bond (`code: 0`). |
| **CRITICAL** first-election halt | ✅ FIXED | Election override baseline now uses `GetLastValidators` (bonded only), not `GetAllValidators`. |
| **F-01/#31** inflation anchor | ✅ FIXED | Emission now anchors to real circulating supply (bank supply minus non-circulating reserves), with mint-authority caps and the adaptive-inflation staking ratio re-derived from the same live inputs. Exposed and fixed a matching bug in `assertEmissionCapInvariant` (same stale-constant class). |
| **F-06** free contract balance | ✅ FIXED | `TopUpContract`/`ExecuteExternal` now collect real coins before crediting `contract.Balance`. |
| **F-17** contracts restart-fork | ✅ FIXED | EndBlocker loads committed state before reading the gate param. |
| **F-15** fee-collector epoch halt | ✅ FIXED | `MsgDistributeFees` rejects a non-current-height epoch; EndBlock fails soft. |
| **F-16** burn-permission halt | ✅ FIXED | `Validate()` rejects any permission list omitting `fee_collector`, not just an empty one. |
| **F-02** block gas meter | ✅ FIXED, live-verified | Real `BlockGasMeter` enabled; live block showed real, non-zero cumulative gas (9.16M/75 tx) instead of a permanent 0. |
| **F-18** ante ordering | ✅ FIXED | Ante chain flattened so `SetUpContext` genuinely runs first; custom decorators spliced in immediately after, matching the SDK's own decorator order exactly. |
| **F-14** nominator-pool race/crash | ✅ FIXED | `sync.RWMutex` over every exported entry point. New concurrency regression test passes cleanly (repeated, no crash); `-race` unavailable in this environment (no gcc/cgo) — the added test would crash the process outright on the old code even without `-race`, per Go's unconditional concurrent-map-access detection. |
| Genesis defaults (R1/R5/R7, part of decentralization) | ✅ FIXED, live-verified | `testnet init-files` now applies `AetraSlashingParams()` and a 3% `MinCommissionRate` floor (was 0%); `aetrad init`'s CometBFT config now sets `timeout_commit=5s` (was 1s); `consensus_params.block.max_gas` now pinned to 20,000,000 (was unlimited). Live-verified via a fresh genesis + running network. |
| Real minimum validator self-bond | ✅ FIXED, live-verified | `PoolOnlyMsgServer.CreateValidator` now enforces `StakingMinSelfBondNaet` (10,000 AET) on live joins, exempting InitChain/gentx (`BlockHeight() <= 0`) so testnet bootstrap's 100 AET default keeps working. **This one broke chain startup on the first attempt** (gentx routes through the same message server as live txs) — caught by live verification, not unit tests, and fixed same-session (commit `023f261d`). Left in as a cautionary note: unit tests alone did not catch a genesis-breaking regression here; only booting a real network did. |
| **F-11** nominator-pool share overflow | ✅ FIXED | `CheckedAddUint64` on all three deposit/cancel accumulation sites, `sumShares` now propagates overflow instead of wrapping. New regression test. |
| **#2/F-04** nominator-pool bank + staking custody | ✅ FIXED, live-verified + E2E-tested | See "F-04 closed" below — this was reclassified mid-session from "deliberately not attempted" after explicit direction to finish it. |

### F-04 closed: nominator-pool deposits now really reach a validator

The plain-pool flow (`CreateNominatorPool` / `DepositToPool` / `RequestPoolWithdrawal`) now has
real bank + `x/staking` + `x/distribution` custody, not just a ledger entry:

- `DepositToPool` pulls real coins from the delegator into the pool's own module account
  (`authtypes.NewModuleAddress("nominator-pool")`, distinct from the reserved catalog address)
  and calls `x/staking`'s `Delegate` for real, so `TotalBondedStake` is backed by an actual
  delegation instead of a number nobody put behind it.
- `RequestPoolWithdrawal` calls real `Undelegate`; a new pool-side `EndBlocker` settles the
  payout to the depositor once the pool's own module account actually holds the matured funds.
- `ClaimPoolRewardsWithReceipt` pulls real accrued `x/distribution` income into the pool account
  before paying the claimant, for the plain-pool path (the official/contract-mediated pool keeps
  its existing synthetic-reward path unchanged — deliberately not touched).

Two bugs surfaced only by driving real transactions against a live 4-node testnet, both now fixed:

1. **Wire-marshal panic.** The three plain-pool message types had zero `protobuf` struct tags and
   zero signer resolution — broadcasting one crashed gogoproto's reflection-based `Unmarshal` on
   every receiving node (recovered by baseapp's panic middleware, so not a fatal crash, but the
   tx could never be delivered). This made the custody wiring above completely unreachable from
   any real transaction despite passing every unit test. Fixed the same way four sibling message
   types were already fixed in an earlier pass: struct tags, a `nominatorPoolMessageFields()`
   descriptor entry, and a `CustomGetSigners` registration each.
2. **Blocked recipient.** Once real transactions could land, `RequestPoolWithdrawal` still failed
   live with `"... is not allowed to receive funds: unauthorized"`. The pool's module account is a
   genuine `x/staking` delegator once it holds a live delegation, and `x/staking`'s
   `BeforeDelegationSharesModified` hook (plus `CompleteUnbonding`) both pay straight into the
   delegator address via `bankKeeper.SendCoinsFromModuleToAccount` — which always errors on a
   bank-blocked recipient, and every module account is blocked by default. Fixed by adding the
   pool's real module account to `BlockedAddresses()`'s explicit unblock list, the same exception
   already carved out for `gov`.

**Verification:** deposit → real delegation → withdrawal request → real undelegation were all
live-broadcast against a fresh 4-validator testnet and confirmed via `cosmos/staking` queries
(`tools/poolcheck`, new). The final matured-payout leg could **not** be observed live — this
module's own `UnbondingBlocks` parameter is genesis-floored to 14–21 days
(`appparams.ValidateStakingUnbondingBlocks`) for the same reason `x/staking`'s real
`unbonding_time` is, and neither can be shortened even for a test genesis without failing that
validation at chain start. Instead, `TestNominatorPoolCustodyEndToEndDepositDelegatesAndWithdrawalPaysOutReal`
(new, `app/nominator_pool_custody_e2e_test.go`) drives the exact same msg-server + real-keeper
code path in-process and advances `ctx` block time/height directly to the real maturity point —
the same fast-forward technique `x/staking`'s own test suite uses for the identical problem — and
asserts the delegator's real bank balance returns to their pre-deposit principal. Full round trip,
real keepers, zero mocks; only the multi-day wall-clock wait is synthetic.
- **R3/R4/R6 decentralization (real delegation, enforced power cap, neutralizing the dormant
  election override).** These touch the validator-set-formation path directly — the same class
  of code where the original C-1 CRITICAL (chain halt) lived, and where this session's own
  `GetLastValidators` fix required careful multi-block reasoning. A live power cap needs a
  dedicated multi-block CometBFT test harness to validate safely, per the same lesson. Not
  rushed.
- **F-08 Block-STM parallel execution.** F-02 (real block gas meter) is what actually unblocks
  this — confirmed the app does not currently use `SetBlockSTMTxRunner`, so enabling it is a new
  capability, not a bug fix, and carries AppHash-determinism risk that needs its own dedicated
  verification pass.
- **Zones/sharding, mesh cryptographic proofs, multi-shard block production.**
  Multi-quarter scope, unchanged from the prior audit's assessment — see §4 above.
- **F-09/F-10/F-12/F-13/F-19/F-20/F-21/F-22/F-24 and the rest of §8.** Lower severity, not
  reached this session; still open, see §9 remediation plan above for each.

---

---

## 0. Headline

The chain **produces blocks, moves money, and runs a real 4-validator consensus correctly**. The
Pass-2 CRITICAL (`C-1`, election-override halt) is confirmed fixed live: `num_val_updates=0` every
block, no halt across 250+ blocks.

But four of the five things this audit was asked to verify are **not true today**:

| Expectation | Reality | Verdict |
|---|---|---|
| Inflation grows naturally and correctly | Emission is anchored to a **365 AET constant**, not supply → **10.95 AET/year for the entire network** | ❌ |
| Zones shard load / raise throughput | Zones are **dormant**; no tx is ever routed to a zone. Worse: **block gas limit is not enforced at all** | ❌ |
| Stakers and validators get expected rewards | The pool **never bridges to `x/staking`** and holds **no coins**; voting power = self-bond only | ❌ |
| Contracts run a full lifecycle | Deploy/execute work, but **anyone can credit any contract unlimited balance for free** | ❌ |
| All system addresses perform their function | **4 of 11** are permanently empty or one-way sinks with no spend path | ⚠️ |

And one finding dominates everything else, measured live:

> ### 🔴 The token supply is burning ~5 orders of magnitude faster than it mints.
> In **22 minutes** of moderate load the network destroyed **1.48% of its entire money supply**.
> Emission over the same period: **zero** (and 0.03 AET/day when an epoch does land).
> **At 1 TPS a 21-validator testnet burns its whole supply in ~23 hours.**

---

## 1. Live network measurements

Network: 4 validators, single host (8-core consumer CPU), full mesh (`n_peers=3`), chain-id `-19`.

### 1.1 Block production

| Metric | Measured | Source |
|---|---|---|
| **Block time (idle)** | **5.45 s** avg (5.17 / 5.22 / 5.26 / 6.40 / 5.21) | `/block` header timestamps, h8→h13 |
| **Block time (under load)** | **5.20 s** avg | h149→h159 |
| **Finality** | **single block** — CometBFT BFT instant finality; no probabilistic depth | `finalized block` @ every height |
| Validator set | 4, equal power 100,000 each | `/validators` |
| `num_val_updates` | **0** every block | node0 log — **C-1 fix holds** |
| Genesis→height 250 | no halt, no fork, no restart | 4-node logs |

**Block time is 5.45 s idle / 5.20 s loaded.** `--commit-timeout` default is 5 s
([cmd/l1d/cmd/testnet.go](cmd/l1d/cmd/testnet.go)); the extra ~0.2–0.45 s is execution + 4 nodes
sharing one host. On separate hardware expect ~5.1–5.2 s.

> ⚠️ **The shipped node default is not 5 s.** `initCometBFTConfig()`
> ([cmd/l1d/cmd/commands.go:37-41](cmd/l1d/cmd/commands.go)) returns stock `cmtcfg.DefaultConfig()`
> → **`timeout_commit = 1 s`**, contradicting the project's own 5–8 s production target
> ([docs/VALIDATOR.md:9](docs/VALIDATOR.md)). Only `testnet init-files` sets 5 s. A validator
> following `init` + `join.sh` gets a 1 s chain. → **F-07**

### 1.2 Throughput (TPS)

Load: 8 distinct funded senders × 120 pre-signed transfers each, fired concurrently.

| Metric | Measured |
|---|---|
| **Peak block** | **200 txs** in block 205 |
| **Gas in that block** | **21,014,289** |
| Block bytes | 83,184 (0.4% of the 21 MiB cap) |
| Block time for that block | 5.29 s |
| **Peak TPS** | **37.8 tx/s** (200 / 5.29) |
| gas per transfer | **~105,071** (21,014,289 / 200) |
| Fee actually charged | **0.5 AET** = **$0.005** per transfer |

**The 200-tx ceiling was NOT the chain's capacity — it was `8 senders × 25`.** Every sender is
capped at exactly 25 tx/block, confirmed independently three times:
1. single-sender run: exactly **25** accepted, rest rejected;
2. 8-sender run: each loadgen reported exactly **`25 accepted, 95 rejected`**;
3. the constant: `DefaultSenderTxsPerBlock = 25` ([x/fees/types/fee_model.go:29](x/fees/types/fee_model.go)).

### 1.3 🔴 The block gas limit does not exist

**Block 205 consumed 21,014,289 gas against a declared `MaxBlockGas` of 20,000,000.** The limit was
exceeded by 5% and nothing rejected anything. Root cause, every link verified:

1. `disableBlockGasMeter: true` is the **SDK v0.54.3 default** (`baseapp/baseapp.go:188`), and
   nothing in this repo calls `EnableBlockGasMeter` (grep: zero hits).
2. → `getBlockGasMeter` returns `noopGasMeter{}`, whose `GasConsumed()` returns **0**.
3. → [x/fees/keeper/fee_policy.go:54-56](x/fees/keeper/fee_policy.go) reads `gasConsumed = 0`.
4. → `ValidateAdmission`'s check `BlockGasConsumed + GasLimit > MaxBlockGas`
   ([x/fees/types/fee_model.go:171](x/fees/types/fee_model.go)) degenerates into
   `GasLimit > MaxBlockGas` — a **per-tx** check. The block-level budget is never applied.
5. → the same zero feeds `x/fees/keeper/congestion.go:41` → `SetCongestionState(0)` forever →
   **the congestion fee surcharge can never engage** (its threshold is 8,000 bps) → and `x/load`
   is fed `UsedBlockGas: 0`.

**Consequence.** The only remaining block bound is `MaxBlockTxs = 5,000`
([fee_model.go:24](x/fees/types/fee_model.go)). 200 distinct senders × 25 = 5,000 txs × 105k gas =
**~525M gas in a single block — 26× the intended budget**. That block must be executed by every
validator inside `timeout_commit`. This is the decentralization ceiling in one line: a home
validator cannot absorb a 525M-gas block, so the network drifts to whoever has the biggest machine.
→ **F-02**

### 1.4 Home-validator feasibility

| Parameter | Configured | Assessment |
|---|---|---|
| `block.max_gas` | **`-1` (unlimited)** — genesis + live `/consensus_params` | 🔴 no bound |
| `block.max_bytes` | 22,020,096 (21 MiB) — CometBFT default | 🔴 21 MB × 16.6k blocks/day = **363 GB/day** worst case |
| `timeout_commit` | 1 s (shipped default) / 5 s (testnet init) | 🟠 inconsistent |
| `index-events` | `[]` = **index everything** | 🟠 unbounded index growth |
| Documented target | 4–8 cores, 16 GB RAM, 500 GB–1 TB NVMe, 100 Mbps ([docs/VALIDATOR.md:12-19](docs/VALIDATOR.md)) | reasonable |

**Verdict: consumer-PC-viable in principle, not in configuration.** Nothing in the config enforces
the documented envelope. The gap between "21 MB / unlimited-gas blocks" and "16 GB home PC" is the
single biggest structural threat to the user's stated priority. Fixing F-02 + F-07 closes it and
costs only block time — which the brief explicitly authorizes trading away.

---

## 2. Economics — measured, in AET and USD

### 2.1 Genesis supply

Genesis = **validators × 1,000 AET**, no premine (confirmed: 4 balances × 1,000 AET, no founder
allocation). Each validator self-bonds **100 AET** of it.

| Set size | Initial supply | **USD @ $0.01** | Bonded | Staking ratio |
|---|---|---|---|---|
| **4 (measured)** | **4,000 AET** | **$40.00** | 400 AET | 10% |
| 21 | 21,000 AET | $210.00 | 2,100 AET | 10% |
| 100 | 100,000 AET | $1,000.00 | 10,000 AET | 10% |
| 128 (`max_validators`) | 128,000 AET | $1,280.00 | 12,800 AET | 10% |

Target staking ratio is **60%** (`DefaultTargetStakeBps = 6000`); actual is **10%**. The adaptive
controller therefore drives inflation to its **5% maximum** — correct behaviour, wrong magnitude
(§2.3).

### 2.2 🔴 Measured burn — the supply is collapsing

Measured over ~250 blocks (~22 min) with 237 transfers:

| Quantity | naet | AET | **USD** |
|---|---|---|---|
| Genesis supply | 4,000,000,000,000 | **4,000.000000** | **$40.0000** |
| **Live supply after 22 min** | 3,940,750,000,000 | **3,940.750000** | **$39.4075** |
| **Burned** | 59,250,000,000 | **59.250000** | **$0.5925** |
| Treasury balance | 17,775,000,000 | **17.775000** | **$0.1778** |
| Validators (fee share, via `x/distribution`) | 41,475,000,000 | **41.475000** | **$0.4148** |
| **Total fees paid** | 118,500,000,000 | **118.500000** | **$1.1850** |

The split reconciles **exactly**: 237 txs × 0.5 AET = 118.5 AET → burn 50% = **59.25 ✓**,
treasury 15% = **17.775 ✓**, validators 35% = **41.475 ✓**
([x/fee-collector/types/genesis.go:21-33](x/fee-collector/types/genesis.go)).
**The fee split machinery is correct and works.** The problem is the magnitude.

**Supply destroyed: 1.48% in 22 minutes.** Extrapolating the measured burn rate:

| Sustained load | Burn rate | 4,000 AET supply gone in | 21,000 AET gone in |
|---|---|---|---|
| 37.8 TPS (measured peak) | 9.45 AET/s | **7 minutes** | 37 minutes |
| 1 TPS | 0.25 AET/s | 4.4 hours | **23 hours** |
| 0.1 TPS | 0.025 AET/s | 1.9 days | 9.7 days |

Against this, emission mints **10.95 AET/year** (§2.3). **Burn exceeds mint by ~5 orders of
magnitude.** The chain is not merely deflationary — under any real usage it destroys its own money
supply, at which point no account can pay a fee and the network is unusable. → **F-01**

Root cause is a mismatch of scales, not a broken mechanism: a **0.5 AET fee** (`DefaultBaseFeeAmount
= 0.4 AET`, [x/fees/types/fee_model.go:17](x/fees/types/fee_model.go)) is **0.05% of a validator's
entire 1,000 AET balance, per transaction**, and half of it is burned forever. Either genesis supply
must be ~10⁴–10⁶× larger, or the fee ~10⁴× smaller, or the burn ratio must be supply-aware.

### 2.3 🔴 Inflation: 0.05%, not 3% (#31 — confirmed at the constant and the call site)

`ComputeEpochEmission` ([x/emissions/types/genesis.go:197-223](x/emissions/types/genesis.go)):

```go
annual := params.AnnualReferenceSupply.Amount.MulRaw(int64(inflationBps)).QuoRaw(int64(BasisPoints))
amount := annual.QuoRaw(int64(params.EpochsPerYear))
```

`AnnualReferenceSupply = AnnualReferenceSupplyNaet = 365_000_000_000 naet` = **365 AET**, a
compile-time constant ([app/params/mint.go:15](app/params/mint.go)) — **not** the real supply.

| At inflation | Annual emission (whole network) | **USD/yr** | Per epoch (day) | **USD/day** |
|---|---|---|---|---|
| 3% (target) | **10.95 AET** | **$0.1095** | 0.030 AET | $0.0003 |
| 5% (max — where the controller actually sits) | **18.25 AET** | **$0.1825** | 0.050 AET | $0.0005 |

**Effective real inflation** = emission ÷ actual supply:

| Actual supply | Emission @3% | **Effective inflation** | vs. 3% target |
|---|---|---|---|
| 4,000 AET (measured net) | 10.95 AET | **0.274%** | 11× too low |
| 21,000 AET | 10.95 AET | **0.052%** | 58× too low |
| 100,000 AET | 10.95 AET | **0.011%** | 274× too low |

Validators' share of that (70%) is **7.67 AET/year — $0.0767/year split across the whole set.**
A 100-validator set earns **$0.00077 each per year** from inflation. Inflation rewards are, for
practical purposes, zero. → **F-01**

**The fix is coupled and must land as one change.** The mint-authority safety caps are derived from
the *same* 365-AET constant ([app/params/mint.go:64-83](app/params/mint.go)):
`MintAuthorityEpochCap = 365 × 5% ÷ 365 × 4 = 0.2 AET`. Re-anchoring emission to a real 21,000 AET
supply produces **1.726 AET/epoch**, which is **8.6× over that cap** → `ApplyMintProtocolCoins`
returns `ErrEpochCapExceeded` → [app/native_economy.go:86-96](app/native_economy.go) *skips the
emission* and emits `native_emission_skipped`. Result: inflation would drop from 0.05% to **exactly
0**. Anchor and caps must become supply-relative together.

### 2.4 Economic constants (code-true)

| Constant | Value | Source |
|---|---|---|
| Denom / decimals | `naet`, 10⁹ per AET | [app/params/token.go:7-11](app/params/token.go) |
| Genesis supply | validators × 1,000 AET, no premine | [cmd/l1d/cmd/testnet.go:473](cmd/l1d/cmd/testnet.go) |
| Self-bond at genesis | 100 AET | [cmd/l1d/cmd/testnet.go:487](cmd/l1d/cmd/testnet.go) |
| Inflation min / target / max | **1.5% / 3% / 5%** | [app/params/economy.go:17-19](app/params/economy.go) |
| Target staking ratio | 60% | `DefaultTargetStakeBps` |
| Responsiveness | 8% | `DefaultResponsivenessBps` |
| Emission epoch | **14,400 blocks** (= 86,400 s ÷ 6 s) ≈ **20.8 h** at the real 5.2 s block | [x/nominator-pool/types/state.go:52](x/nominator-pool/types/state.go) |
| Epochs/year | 365 | [app/params/mint.go:16](app/params/mint.go) |
| **Emission split** | validators **70%** / treasury 10% / protection 10% / burn 5% / ecosystem 5% | [x/emissions/types/genesis.go:12-20](x/emissions/types/genesis.go) |
| **Fee split** | **burn 50% / validators 35% / treasury 15%** / protection 0% | [x/fee-collector/types/genesis.go:21-33](x/fee-collector/types/genesis.go) |
| Base fee / max fee | **0.4 AET** / 5 AET | [x/fees/types/fee_model.go:17-19](x/fees/types/fee_model.go) |
| Commission (gentx) | 5% rate / 20% max / 1% max-change | verified in genesis |
| `min_commission_rate` | **0%** ← floor not wired | genesis `staking.params` |
| `min_self_delegation` | **1 naet** (10⁻⁹ AET) | genesis gentx |
| Unbonding | 21 days (1,814,400 s) | genesis `staking.params` |
| Slashing | double-sign **5%**, downtime **1%**, window 100, min-signed 50% | genesis `slashing.params` |
| `max_validators` | **128** | genesis `staking.params` |

> The epoch constant assumes 6 s blocks but the chain runs at 5.2 s, so an "epoch" is 20.8 h, not
> 24 h — emission is ~15% fast relative to its own annualization. Cosmetic next to F-01, but it
> means `EpochsPerYear = 365` is wrong for the real block time (~421 epochs/yr).

---

## 3. Decentralization & Nakamoto coefficient

### 3.1 Who actually picks the validator set

**`x/staking` does. The elector override is dormant.** `applyElectionValidatorUpdates`
([app/elector_validator_updates.go:28-30](app/elector_validator_updates.go)) early-returns when
`CurrentValidatorSet` is empty; it is empty in the default genesis and can only be filled via
`ApplyForValidatorSet`, which requires `Params.Authorize(msg.Authority)` where the authority is
`prototype.DefaultAuthority` = the **keyless address `0x00…01`** that nobody can sign for, and the
module exposes no `MsgUpdateParams`. So the override is permanently inert, and entry is
**permissionless**: any `MsgCreateValidator` with a non-zero self-bond. No allowlist, no KYC, no gov
approval.

### 3.2 🔴 Voting power = self-bond only, and stake can never leave

Two live-verified facts make this the dominant decentralization finding:

**(a) Undelegation is blocked unconditionally — proven on the live chain.**

```
$ l1d tx staking unbond <validator> 1000000000naet --from node0
code: 1
raw_log: direct user delegation to validators is disabled; use official liquid staking
```

That is a validator trying to withdraw **its own self-bond** and being refused.
`ValidateUndelegate`/`ValidateBeginRedelegate`
([app/stakingpolicy/msg_server.go:59-77](app/stakingpolicy/msg_server.go)) reject **everything** —
unlike the sibling `ValidateDelegate` (`:43-57`), they carry **no `IsValidatorSelfBond` and no
`IsNominatorPoolControlledDelegator` exemption**. This is an omission, not a policy: the exemptions
exist 10 lines above and were simply not copied down.
**Every bonded token on the chain is permanently locked. No validator can ever exit.** → **F-03**

**(b) Delegation is blocked, and the pool that is supposed to replace it isn't wired to staking.**
`PoolOnlyMsgServer.Delegate` + the `RejectDirectUserStakingDecorator` ante
([app/txhandlers/direct_staking.go:14-19](app/txhandlers/direct_staking.go)) redirect users to the
"official liquid staking pool" — but `x/nominator-pool`'s Keeper
([x/nominator-pool/keeper/keeper.go:49-56](x/nominator-pool/keeper/keeper.go)) holds **no staking
keeper and no bank keeper**, and **no `x/` module imports `cosmos-sdk/x/staking`** except
`x/fees/keeper/ante.go`. The pool cannot delegate. Therefore **validator voting power is exactly
each operator's personal self-bond**, and Nakamoto tracks the wealth curve directly. → **F-04**

### 3.3 Nakamoto coefficient

Nakamoto(⅓) = smallest *k* whose cumulative power exceeds 33.33% (the halt/censor threshold).

| Scenario | Nakamoto(⅓) | Arithmetic |
|---|---|---|
| **Live measured (N=4, equal)** | **2** | each 25%; 1×25% < 33.3%, 2×50% > 33.3% |
| N=100, equal stake | **34** | 34 × 1% = 34% > 33.33% |
| **N=100, realistic power-law (s=1.0)** | **3** | shares ∝ 1/i, H(100)=5.1874 → top-3 = 35.34% > 33.33% |
| N=100, s=1.2 / s=0.8 / s=0.5 | 2 / 6 / 15 | same method |
| **If the 3% cap were enforced** | **12** | 11×3%=33% ✗, 12×3%=36% ✓ |

**Realistic Nakamoto today ≈ 3.** That is *below* average for a Cosmos chain (Cosmos Hub ≈ 7,
Osmosis ≈ 6). The prior audit's "34 at equal stake / 3–10 under skew" is right in outline, but
equal stake is not a scenario — it is the genesis instant.

**Self-bond-only staking makes this structurally worse than a normal PoS chain**: with no delegation
to redistribute power, the wealthiest operators' capital *is* the power distribution, with nothing
to dilute it.

### 3.4 Every anti-concentration control is dead

| Control | Default | Status | Consuming call site |
|---|---|---|---|
| `x/stake-concentration` `MaxVotingPowerBps` | **3%** | **COMPUTED-BUT-IGNORED** | `CanAcceptDelegation`/`RewardModifierBps` — **zero non-test callers**; module in no block order |
| `x/aetra-staking-policy` commission floor / power cap | 3% / 3%→2% | **DEAD** | `RecomputePolicy` — zero non-test callers; module implements no Begin/EndBlocker |
| `x/dynamic-commission` floor/ceiling | 3% / 20% | **DEAD (externally)** | clamps only into its own KV record; no reader outside the module |
| `x/staking` `MinCommissionRate` (the only real floor) | **0%** | **ENFORCED AT 0%** | `PoolOnlyMsgServer.CreateValidator` is an unchecked passthrough |
| Min self-bond `StakingMinSelfBondAET` | 10,000 AET | **DEAD** | `ValidateValidatorBond` — zero callers |
| Validator entry stake | 1,000,000 AET | **DEAD** | `app/params` internal only |
| `app/params` `MaxTopValidatorConcentrationBps` = 33.34% | — | **DEAD** | *these are the numbers the prior audit cited* |

> **Correction to the prior audit.** SA2-F03 cited caps of "33.34% single / 15% soft / 67% top-N".
> Those constants live in [app/params/economy.go:36,58-59](app/params/economy.go) and are dead code.
> The *real* `x/stake-concentration` caps are **3% / 2.5% / 2%** — and are equally unenforced. The
> verdict was right; every number was wrong.

**A cap without a Sybil cost is theater.** With permissionless entry and a 1-naet minimum self-bond,
one whale splits into 34 validators for the cost of 34 nodes and defeats a per-address cap entirely.
Any power cap must ship together with a real minimum self-bond. → **F-05**

### 3.5 Barriers to entry (quantified)

| Barrier | Reality |
|---|---|
| Stake to join | **~0.1 AET** ($0.001). No network floor; `min_self_delegation` is self-declared ≥1 naet |
| Permission | **None** — no allowlist, gov gate, or KYC (`MandatoryKYC: false`) |
| Commission floor | **0%** — race to the bottom permitted |
| Hardware | Docs say 4–8 cores / 16 GB / NVMe / 100 Mbps; **config enforces nothing** (§1.4) |
| **Exit** | 🔴 **impossible** (F-03) |
| Inflation reward | **$0.0008/validator/year** at N=100 (F-01) |

The chain is **more permissionless than advertised** and **far more concentrated than advertised**.

---

## 4. Zones / sharding — does load distribute?

### **No. No transaction is ever routed to a zone.**

| Module | LOC | In app manager | Live Msg/Query/EndBlock | Gate | Verdict |
|---|---|---|---|---|---|
| `x/zones` | 7,736 | yes | **none** (migration only) | `Enabled=false` | **DORMANT** |
| `x/routing` | 833 | yes | **none** | `Enabled=false` | **DORMANT** |
| `x/mesh` | 1,539 | yes | **none** | `Enabled=false` | **DORMANT** |
| `x/sharding-coordinator` | 1,354 | yes | **none** | `Enabled=false` | **DORMANT** |
| `x/networking` | 31,507 | — | — | — | **DELETED** (removed post-audit: `Enabled=false`, zero live Msg/Query/EndBlock surface, no functional caller outside its own package and app wiring) |
| `x/aetracore` | 30,702 | yes | **none** — keeper never called outside tests | `Enabled=false` | **DORMANT** |
| `x/load` | 650 | yes | no surface; keeper called from `x/fees` EndBlocker | `Enabled=false` → silent no-op | **GATED, write-only** |
| `x/avm-scheduler` | 1,425 | yes | **none** | `Enabled=false` | **DORMANT** |

**Proof of the monolith.** `routingtypes.Route()` has exactly **one** non-test caller in the repo:
[cmd/l1d/cmd/execution_os.go:176](cmd/l1d/cmd/execution_os.go) — an operator CLI *simulator* that
hardcodes `ProductionLive: false`. The real path is single and sequential:
`app/block_lifecycle.go:43` → `BaseApp.FinalizeBlock` → `executeTxsWithExecutor(ctx, MultiStore,
req.Txs)` (`baseapp/abci.go:895`) → `txnrunner.NewDefaultRunner` — **one store, one state root, one
thread**. **No `SetPrepareProposal`/`SetProcessProposal` handler is registered anywhere.**

**7 of 8 cannot even be switched on at runtime**: with no Msg service there is no `UpdateParams`, so
`Enabled` can only be flipped in genesis — i.e. a chain restart.

**Dynamic split/merge exists as an algorithm, not a runtime.** `NewShardRebalanceDecision`
([x/aetracore/types/shard_state.go:88](x/aetracore/types/shard_state.go)) and the whole kernel
block-production API (`PrepareKernelProposal`/`FinalizeKernelABCIBlock`/`CommitKernelABCIBlock`) have
**only test callers**.

**Cross-zone "proofs" verify nothing.** `x/mesh` `BuildProof` ([x/mesh/types/hash.go:24](x/mesh/types/hash.go))
is a SHA-256 over message fields; `ValidateSourceProof` recomputes and compares. `MessageRoot` is
compared for **equality only** — never used to verify inclusion. There is no Merkle path and no
validator-set signature check. It is a well-formedness check, not a cryptographic proof.

**What is genuinely live for load handling:** only the `x/fees` admission checks (per-sender 25/block,
per-block 5,000 txs, per-tx 1M gas, fee floor/cap). And per §1.3 the **block-level** gas budget and
the congestion surcharge are **both dead**, because both read the disabled block gas meter.

> **Correction to the prior audit.** It said "only fee-congestion backpressure is live". Measured:
> fee-congestion backpressure is **not** live either — it is wired end-to-end and permanently reads
> zero. The chain has **no working block-level congestion response at all**.

### The honest path to real load distribution

A full multi-shard runtime is a multi-quarter effort and is not reachable now. But the SDK
already ships the thing that actually buys throughput:

> **SDK v0.54.3 includes a Block-STM parallel tx runner** (`baseapp/txnrunner/blockstm.go`,
> `app.SetBlockSTMTxRunner`, `baseapp/options.go:126`) — deterministic optimistic-concurrency
> execution of non-conflicting txs, single state root, **no consensus change**.

It panics if the block gas meter is enabled (`options.go:127-130`, "indeterminism") — which looks
like it conflicts with fixing §1.3. **It does not**: baseapp sets `WithBlockGasUsed()` from summed
`txResults[].GasUsed` at `abci.go:916-920`, *before* `endBlock()` at `:922`, and
`ctx.BlockGasUsed()` is deterministic and available in **both** modes. Migrating `x/fees` off
`BlockGasMeter()` onto `BlockGasUsed()` **unblocks both the gas limit and parallel execution at
once**. → **F-02**, then **F-08**.

---

## 5. Smart contracts

Deploy and execute work live (prior sessions confirmed the full AVM lifecycle on-chain). The
defects are in the **value** paths.

### 🔴 F-06 — Anyone can credit any contract unlimited balance, for free

Two user-signable paths credit a caller-controlled `uint64` to `contract.Balance` with **zero bank
collection**:

- [x/contracts/keeper/keeper.go:1619-1623](x/contracts/keeper/keeper.go) — `TopUpContract` (the
  known #7)
- [x/contracts/keeper/keeper.go:1445-1449](x/contracts/keeper/keeper.go) — **`ExecuteExternal`**
  — *newly found, not in any prior audit*

`bankKeeper` **is** wired ([app/keeperwiring/persistent.go:101](app/keeperwiring/persistent.go)) —
the omission is in the handler. That it is a defect and not a design choice is settled by the
sibling `PayContractStorageDebt` (`keeper.go:1668-1673`), which collects *before* mutating, with a
comment saying the ordering exists "so the freeze cannot be cleared for free".

**Exploit:** an attacker holding ~0 AET sends
`MsgExecuteExternal{Sender: A, ContractAddress: C, Funds: 2⁶⁴-1}` against **any** contract (neither
path checks ownership). `C.Balance` ≈ 1.8×10¹⁹ naet. `chargeRent` now always takes the
`Balance >= charge` branch, so `C` never accrues `StorageRentDebt` and never freezes —
**storage rent is defeated chain-wide for one tx fee.**

### 🟠 F-09 — Rent is charged twice, and any caller can drain the creator's real wallet

`chargeRent` debits the fictional `contract.Balance`; `chargeContractRentAt` returns that amount;
callers hand it to `chargeRentToReserve`, which bank-debits it **again** from `contract.Creator` —
not the contract, not the executor ([x/contracts/keeper/keeper.go:2558-2563](x/contracts/keeper/keeper.go);
call sites `:1530`, `:1814`, `:2141`). Combined with F-06 an attacker guarantees a positive
`rentCharged` every block and bills a victim's wallet for transactions they never signed; when the
wallet empties the contract becomes permanently unexecutable and unrecoverable.
Also `collectRentPayment` returns `nil` (**success**) when `ctx == nil` (`:2546-2549`) — fails open.

### 🟠 F-10 — `x/storage-rent` is entirely unreachable (empirically verified)

All 6 messages lack a `cosmos.msg.v1.signer` annotation; the module hand-rolls its descriptors with
bare `messageDescriptor(name)` — no fields, no options
([x/storage-rent/types/tx.go:864-925](x/storage-rent/types/tx.go)) — and has no `CustomGetSigners`
entry. Verified against the real registry built as `app/app.go` builds it:

```
SigningContext().Validate()             -> OK (no error at boot)
storagerent.MsgPayStorageRent           -> ERROR: no cosmos.msg.v1.signer option found
contracts.MsgTopUpContract              -> OK (signer resolved)
```

The boot gate misses it because `Validate()` skips any service lacking the
`cosmos.msg.v1.service` **extension**, which the synthesized descriptor never sets. So prepaid rent
already collected into `feecollector_storage_rent_reserve` can never be withdrawn, frozen contracts
can never be unfrozen, and governance has no control surface over rent economics — while the boot
gate stays green.

---

## 6. Staking pool / liquid staking

**The pool is a ledger with no money and no connection to staking.**

- `x/nominator-pool`'s Keeper ([keeper.go:49-56](x/nominator-pool/keeper/keeper.go)) has **no bank
  keeper and no staking keeper** — zero references to `bankKeeper|SendCoins|MintCoins|BurnCoins` in
  the entire module.
- `DepositToPool` (`:472-514`), `DepositToOfficialLiquidStaking` (`:516-562`) and
  `DepositToStakingPool` (`:564-654`) credit `delegator.Shares`, `pool.TotalShares` and
  `pool.TotalBondedStake` from a caller-supplied `msg.Amount` **with no debit anywhere**.
- `WithdrawPoolStake` (`:980`) and `ClaimPoolRewards` pay **nothing out**.
- The pool never delegates to `x/staking`, so pooled AET **never reaches a validator** and never
  earns.

**Answer to "how much did validators and stakers earn via the pool": exactly 0 AET = $0.00, for
both.** No AET can enter the pool (nothing debits a depositor), none reaches a validator (no staking
bridge), and none can be paid out (no bank keeper). The numbers in pool state are decorative.

Validators *do* earn — but only from **fees**, via stock `x/distribution` draining
`authtypes.FeeCollectorName` (the emission's 70% `ValidatorReward` leg is deliberately left there,
which is correct). Measured: **41.475 AET = $0.4148** across 4 validators in 22 min, all of it fee
income, none of it inflation. → **F-04**

### 🟠 F-11 — Fix the overflow *before* wiring custody

`DepositToPool` accumulates with bare `+=` (`:497,510-511`), and the invariant meant to catch it —
`TotalShares != sumShares(...)` ([x/nominator-pool/types/state.go:1380](x/nominator-pool/types/state.go))
— **wraps in lockstep**, because `sumShares` (`:2343-2349`) uses the same unchecked `+=`. Modular
arithmetic is associative, so both sides wrap identically and the check **passes on corrupted
state**. The module already has `CheckedAddUint64` and uses it correctly in `SyncPoolRewards`; the
deposit path just doesn't call it.

Two deposits of 2⁶³ wrap `TotalShares`/`TotalBondedStake` to 0; the next honest depositor is treated
as the first, and the attacker's 100-share unbond drains their principal. **Harmless today only
because the ledger holds no coins — wiring custody (F-04) without fixing this converts it directly
into real fund loss.**

---

## 7. System addresses

Two-layer model: 29 catalog vanity addresses
([app/addressing/system_addresses.go:92-123](app/addressing/system_addresses.go)) vs. 11 real module
accounts (`reservedSystemModuleAccountSpecs`, [app/accounts/module_accounts.go:124-150](app/accounts/module_accounts.go)).
Only the 11 can hold funds; the other 18 are label-only and correctly marked `CanHoldFunds=false`.

| Name | Module account | Credited by | Debited by | Verdict |
|---|---|---|---|---|
| AETMint | `mint-authority` | `keeper.go:86` | `:89` same tx | ✅ functional (transient) |
| AETFeeCollector | `feecollector` | `keeper.go:172,210`; `x/fees:255` | `keeper.go:320-335` | ✅ functional |
| AETTreasury | `feecollector_treasury` | fee split + `native_economy.go:156` | `x/treasury/keeper.go:291` | ✅ functional |
| AETBurn | `burn` | `x/burn/keeper.go:79,115` | `:82,119` | ✅ functional |
| **AETStorageRent** | `feecollector_storage_rent_reserve` | `storage-rent:566`; `contracts:2555` | **nothing** | 🟠 **one-way sink** |
| **AETDelegatorProtection** | `feecollector_protection` | fee split; `native_economy.go:159` | **nothing** | 🟠 **one-way sink** |
| **AETValidatorInsurance** | `feecollector_validator_insurance` | **nothing live** | nothing | 🔴 **permanently empty** |
| **AETReporterRewards** | `feecollector_reporter_rewards` | **nothing live** (weight 0) | nothing | 🔴 **permanently empty** |
| AETConfig / AETSystemRegistry / AETElector | own modules | nothing | nothing | ✅ empty by design (`CanHoldFunds=false`) |

Plus an orphan: **`feecollector_ecosystem_grants`** is credited live at
[app/native_economy.go:162](app/native_economy.go) (5% of every epoch) with **no spend path and no
reserved address**.

**All three one-way sinks are declared with `nil` permissions — no `Burner` — so governance cannot
even destroy the stranded balance.** Recovery requires a binary upgrade. → **F-12**

> **Correction to the working tree.** Commit `ec6dcdf7` re-pointed three custodians at fee-collector
> buckets. **AETStorageRent and AETDelegatorProtection are correct** — those buckets are credited by
> live code and the old targets were genuinely empty. **AETValidatorInsurance is not**: its stated
> premise is that `DefaultProtocolIncomePolicy` credits `feecollector_validator_insurance`, but that
> policy's only consumer `CollectAndDistributeProtocolIncomeFromAccount` has **zero non-test
> callers**. The live split has 4 destinations and insurance is not among them. The commit swapped
> one permanently-empty account for another. → **F-13**

---

## 8. Consensus / halt / DoS findings

| ID | Sev | Finding | Location |
|---|---|---|---|
| **F-14** | 🔴 | **Simulate goroutine races consensus state → fatal, unrecoverable node death.** `x/nominator-pool` keeps whole-module state + a derived index map in **process memory** and mutates it in every handler via `loadForBlock`/`rebuildIndexes`, with **no mutex**. baseapp runs `runMsgs` for `execModeSimulate` too, on the gRPC query goroutine, concurrently with `FinalizeBlock`. A concurrent Go map read/write is a `runtime.throw` — **`recover()` cannot catch it and the process exits.** Anyone with RPC access can spam `/Simulate` to kill validators. | [x/nominator-pool/keeper/keeper.go:49-56,348-361](x/nominator-pool/keeper/keeper.go); `x/contracts` has a mutex but `EndBlocker:2175` reads outside it |
| **F-15** | 🔴 | **A governance `MsgDistributeFees` halts the chain at a chosen future height.** The msg takes a free-form `uint64` epoch that shares a key space with block height. Plant `FeeHistory[500000]`; at height 500,000 EndBlock returns `ErrDuplicateHistory` → `FinalizeBlock` errors on every validator → halt. The poisoned entry is committed state, so restarting does not help. Also fires by accident (epochs and heights are both small ascending integers). | [x/fee-collector/module.go:94-105](x/fee-collector/module.go); [keeper.go:297-302](x/fee-collector/keeper/keeper.go) |
| **F-16** | 🟠 | **A well-formed burn-params proposal halts the chain at the next epoch.** `Validate` backfills `fee_collector` burn permission only when the list is **empty**; a non-empty list omitting it (the natural shape of a "tighten permissions" proposal) passes, then `BurnProtocolCoins(ctx, "fee_collector", …)` errors **unguarded out of EndBlock**. The file's own comment documents this hazard; `native_economy.go:86-96` guards the *mint* cap 20 lines earlier but not the burn. | [x/burn/types/genesis.go:72-89](x/burn/types/genesis.go) |
| **F-17** | 🟠 | **Restarted nodes fork once contract internal-messaging is enabled.** `x/contracts` EndBlocker reads `k.genesis.Params.MaxInternalMessageGasPerBlock` and early-returns **before** `loadForBlock(ctx)`. A restarted node holds the default (`0`) until some handler rehydrates it, so it skips the drain while running nodes perform it → different AppHash. Same class as FINDING-006; the guard just sits one line too early. | [x/contracts/keeper/keeper.go:2174-2180](x/contracts/keeper/keeper.go) |
| **F-18** | 🟠 | **Three ante decorators run before `SetUpContextDecorator`, unmetered.** Until `SetUpContext` runs, `ctx` carries baseapp's **infinite** gas meter, so `AdmitTx`'s KV reads + two KV **writes**, per-signer account lookups, and three full nested-message walks are all free. A 0-fee tx pays nothing and is rejected only afterwards. | [app/handlers.go:9-15](app/handlers.go) |
| **F-19** | 🟠 | **`FeeHistory` grows one permanent entry per block, never pruned.** ~5.2M entries/year; `ExportGenesis` is O(chain height) and builds a full dedup map. `x/mint-authority` added `pruneMintHistory` for exactly this reason — at one entry per *epoch*; this is one per *block*. | [x/fee-collector/keeper/keeper.go:371,507-523](x/fee-collector/keeper/keeper.go) |
| **F-20** | 🟠 | **Validator collateral is backed by nothing.** `x/validator-insurance` is a third no-custody ledger (no bankKeeper), yet `ValidateValidatorActivation` gates validator-set entry on `insurance.Balance >= MinimumInsurance` — a fabricated number. `FundValidatorInsurance` emits a "funded" event moving zero coins. | [x/validator-insurance/keeper/keeper.go:251-268,300-309](x/validator-insurance/keeper/keeper.go) |
| **F-21** | 🟡 | **`--gas auto` is broken for every user.** Simulation sets gas limit 0 by definition, but the fee ante rejects a zero gas limit even in simulate mode: `gas limit must be positive: invalid fee ... with gas used: '6671'`. Reproduced live on the first transfer attempt. Stock SDK antes skip the fee check when `simulate == true`. | ante chain; reproduced via CLI |
| **F-22** | 🟡 | **`MsgDelegateToValidator` is a validate-only stub** that returns success and writes nothing. Unreachable today (param-gated); becomes a silent lie the moment governance enables it. | [x/nominator-pool/keeper/keeper.go:656-658](x/nominator-pool/keeper/keeper.go) |
| **F-23** | 🟡 | 34 orphaned RPCs across 5 modules (`avm-scheduler` 3, `bridge-hub` 7, `cross-chain-registry` 5, `identity-root` 8, `sharding-coordinator` 6): keepers wired, modules in the manager, **no Msg service** — they present as live while accepting nothing. `bridge-hub`'s proto describes a full bridge control plane that does not exist. | per-module `module.go` |
| **F-24** | 🟡 | 5 live `x/contracts` tx entry points have **no proto definition** (`SubmitSecurityAttestation`, `RevokeSecurityAttestation`, `TopUpContract`, `PayContractStorageDebt`, `UnfreezeContract`) — routed via a hand-built `Msg_serviceDesc`, bypassing proto-derived tooling and proto-diff review. Security-attestation submit/revoke being off-proto is the sharpest edge. | [x/contracts/types/service.go:171-186](x/contracts/types/service.go) |

**Verified clean** (checked, not assumed): no map-iteration non-determinism on any money write path;
no `time.Now()`/`math/rand`/floats in money or consensus modules; no storage-key collisions across
56 store keys; no CacheContext event leaks (all 7 sites `write()` only on success); the fee-split
largest-remainder algorithm is deterministic; `record.ValidatorReward` is **not** stranded (it is
deliberately left for `x/distribution`).

---

## 9. Remediation plan

Ordered by (blocks-testnet × directly-asked). F-03 first because it is a one-function omission that
locks every token on the chain.

| # | Fix | Files | Effort |
|---|---|---|---|
| **F-03** | Add the `IsValidatorSelfBond` / `IsNominatorPoolControlledDelegator` exemptions to `ValidateUndelegate` + `ValidateBeginRedelegate`, mirroring `ValidateDelegate` | `app/stakingpolicy/msg_server.go:59-77` | **S** |
| **F-02** | Migrate `x/fees` off the disabled `BlockGasMeter()` onto `ctx.BlockGasUsed()` → restores the 20M block gas limit **and** the congestion surcharge **and** unblocks Block-STM | `x/fees/keeper/fee_policy.go:53-56`, `x/fees/keeper/congestion.go:40-44` | **S** |
| **F-01** | Anchor emission to **real bank supply** *and* make the mint-authority caps supply-relative in the same change (else emission is skipped → 0%) | `x/emissions/*`, `app/params/mint.go`, `app/native_economy.go` | **M** |
| **F-01b** | Re-scale the fee/supply ratio so burn cannot destroy the supply (raise genesis supply, or lower the base fee, or make the burn ratio supply-aware) | `app/params`, `x/fees/types/fee_model.go` | **M**, economic decision |
| **F-06** | Collect from the sender **before** crediting `contract.Balance` on **both** `ExecuteExternal` and `TopUpContract`; add invariant `Σ contract.Balance == bank(storage_rent_reserve)` | `x/contracts/keeper/keeper.go:1445,1619` | **S** |
| **F-14** | Guard `k.genesis`/`k.indexes` with a mutex across the load-then-read window (correct fix: read through the KV store per access) | `x/nominator-pool`, `x/contracts`, `x/storage-rent` keepers | **M** |
| **F-15** | Reject a caller-supplied epoch ≠ current height; make fee-collector `EndBlock` fail soft | `x/fee-collector/keeper/msg_server.go`, `module.go:94` | **S** |
| **F-16** | Assert `fee_collector` present in burn `Validate`; make the emission burn non-fatal | `x/burn/types/genesis.go:72-89`, `app/native_economy.go:165` | **S** |
| **F-07** | Set real consensus/node defaults: genesis `block.max_gas = 20,000,000`, `timeout_commit` 5 s, `index-events` whitelist | `cmd/l1d/cmd/commands.go:37-41`, genesis defaults | **S** |
| **F-05** | Genesis staking params: `min_commission_rate = 3%`, explicit `max_validators`, real min self-bond | `app/genesisconfig/defaults.go` | **S** |
| **F-11** | Use `CheckedAddUint64` on the deposit path; make `sumShares` return an error | `x/nominator-pool/keeper/keeper.go:497,510-511`, `types/state.go:2343` | **S** |
| **F-04** | Wire bank custody + an `x/staking` bridge into the pool — **only after F-11** | `x/nominator-pool` | **L** |
| **F-08** | Enable Block-STM parallel execution (`SetBlockSTMTxRunner`) — **after F-02** | `app/app.go` | **M** |
| **F-17** | Move `loadForBlock(ctx)` above the param guard | `x/contracts/keeper/keeper.go:2174` | **S** |
| **F-12** | Grant `Burner` to the three one-way sinks, or wire real payout paths | `app/accounts/module_accounts.go` | **S** |
| **F-13** | Revert the AETValidatorInsurance custodian change or wire `DefaultProtocolIncomePolicy` | `app/accounts/module_accounts.go:139` | **S** |
| **F-09/F-10/F-18/F-19/F-20/F-21/F-22/F-24** | see §5, §7, §8 | various | S–M |
| **F-23** | Delete or honestly label the 34 orphaned RPCs and the dead policy surface | various | M |

**Not attempted:** full multi-shard block production (F-25 / SA2-F05) — genuinely
multi-quarter. The honest framing is that `x/zones`/`x/routing`/`x/mesh`/`x/aetracore` are a ~66K-LOC
executable **specification** with sound deterministic algorithms and one substantive gap before they
could ever be load-bearing: **mesh "proofs" verify nothing** and must become real inclusion proofs
against `MessageRoot`. Until then the chain must stop advertising horizontal scaling. Block-STM
(F-08) is the real, reachable throughput win.

---

## Appendix — reproduce

```bash
# 4-validator network
l1d testnet init-files --validator-count 4 --output-dir ./net4 --chain-id -19 \
    --single-host --keyring-backend test --commit-timeout 5s
for i in 0 1 2 3; do l1d start --home ./net4/node$i/aetrad & done

# throughput probe (pre-signs offline, so it measures the chain, not the signer)
go run ./tools/loadgen --home ./net4/node0/aetrad --from node0 --to <addr> \
    --count 120 --concurrency 1 --settle 40s
# run 8 of these against distinct funded senders to reach the 200-tx block

# the supply-collapse measurement
curl -s localhost:1317/cosmos/bank/v1beta1/supply     # 4000 AET -> 3940.75 AET in 22 min
```
