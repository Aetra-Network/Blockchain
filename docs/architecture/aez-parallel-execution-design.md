# AEZ real parallel execution — design v1 (REJECTED), v2 (REJECTED, worse than v1)

Status: **Two adversarial review rounds; the second found MORE severe blockers than the first (a newly-
introduced network-wide crash DoS, and proof that `x/contracts`'s current storage architecture is
structurally incompatible with zone-goroutine concurrency independent of Phase 3/5). Do not implement below
as-is. Paused pending direct owner/architect involvement — see "Status" at the end of this document.** This
is a new design track, not in the original AEZ phased plan (`docs/architecture/aez.md`), requested to
close the gap that document's own §4.9 names explicitly: "Zones do not add throughput... AEZ today buys
isolation/fairness/blast-radius-containment, NOT capacity."

## What v1 got right (kept forward)

- **Correctly identified the real hazard before designing around it**: `ctx.BlockGasMeter()` is a single,
  shared, *unlocked* `uint64` counter (verified directly against `cosmos-sdk@v0.54.3/store/v2/types/gas.go`)
  that every tx's admission check reads and every tx's execution mutates — calling `RunTx` concurrently
  against the stock pipeline is a confirmed data race, not a hypothetical. This correctly forced the design's
  central architectural decision: **admission (ante) stays sequential; only message execution parallelizes.**
- **Real hook point, verified against vendored source, not assumed**: `sdk.TxRunner` / `SetBlockSTMTxRunner`
  (`baseapp/options.go`), called from `internalFinalizeBlock` → `executeTxsWithExecutor` → `txRunner.Run`
  (`baseapp/abci.go`) — the actual per-block-of-txs dispatch point, not the coarser `FinalizeBlock` wrapper.
  Correctly found that the panic guard against combining `SetBlockSTMTxRunner` with the block gas meter only
  fires for the concrete `*txnrunner.STMRunner` type, so a distinct `AEZZoneRunner` type sidesteps the
  type-check — flagged (correctly) as sidestepping the *check*, not proving the underlying hazard absent.
- **Deterministic, reused partitioning**: resolves each tx's home zone via the *already-shipped* Phase 6
  `ZoneOfAddress`/fee-payer-else-first-signer rule (no second resolution mechanism invented), appends indices
  to per-zone buckets in ascending block order (so per-zone sub-order trivially equals original block order
  with no sort), and scatters results into a pre-sized, index-owned slice (race-free by construction, no two
  zones ever write the same slot) so final output order is always original block order, never
  completion order.
- **Store-layer concurrency-safety claim, independently verified against the actual vendored IAVL/store-v2
  source** (not just asserted): concurrent zone-goroutine *reads* into the shared parent tree are genuinely
  race-free by construction (`nodeDB.GetNode`/`GetFastNode` take a real mutex despite the field's `RWMutex`
  type), and writes never touch the parent store during the parallel window — only in a single-goroutine,
  fixed-order (`AllZoneIDs()`, ascending 0..4) commit phase after the join barrier. This part of the design
  survived adversarial review unchallenged.
- **Honest, unhedged non-throughput admission**: explicitly states that with `GenesisRoutingTable()` mapping
  all 256 buckets to Core today and Phase 3/5 unstarted, this design — even if built exactly as specified —
  delivers **zero observable throughput improvement today**, and recommends landing Phase 3 (native-account
  zone-prefixed keys) before this engine is enabled/measured, so tests exercise real multi-zone isolation
  instead of "a routing table claiming zone 3 while the underlying keys don't actually partition by zone."
  This candor was not weakened or hedged by review.
- **Correctly generalized the existing per-delivery panic-recovery precedent** to per-zone-goroutine recovery
  as a *starting point* — right shape (skip that zone's `Write()`, deterministic synthetic failure results for
  its txs, other zones unaffected), even though review found the recovery *policy* itself needs to change
  (see blocker 2 below).

## Why v1 is rejected — 2 blockers found by adversarial review

1. **Silent collision with Optimistic Execution, which is already enabled in this exact app.**
   `app/app.go` already calls `baseapp.SetOptimisticExecution()`, which speculatively invokes the *same*
   `internalFinalizeBlock` → `executeTxsWithExecutor` → `txRunner.Run` call chain this design hooks, for a
   *predicted* future block, potentially while the real current block is still settling. The entire design
   (one well-defined block window, one `CaptureBlockContext` call per block, one fixed zone-commit sequence)
   implicitly assumes `Run()` fires exactly once, sequentially, per actual block — it never analyzes whether a
   real invocation and a later-discarded speculative OE invocation could be concurrently live against branches
   of the same underlying multistore. Cosmos SDK's own authors already flag exactly this class of hazard
   (shared/concurrent block-execution state + speculative/parallel execution = indeterminism) via the
   `SetBlockSTMTxRunner` panic guard — OE is a second, already-shipped instance of that same class, and v1 is
   silent on it.
2. **Per-zone-goroutine panic recovery, as designed, is a real fork vector.** Whether a panic fires in the new
   glue code can be genuinely host/scheduling-dependent (a latent race, a goroutine-count-sensitive heisenbug,
   GC-timing interaction) — unlike the existing per-tx recovery precedent, where firing is a pure function of
   deterministic inputs (tx bytes, state) and therefore identical on every honest validator. If a race panics
   on validator A's zone-Z goroutine but not on validator B's (different `GOMAXPROCS`/scheduling/load), A skips
   zone Z's effects and B applies them — guaranteed `AppHash` divergence. v1's own residual-risks section names
   this risk but offers only a same-machine, same-binary `GOMAXPROCS` 1-vs-8 test as mitigation, which
   structurally cannot reproduce cross-validator hardware/OS/GC heterogeneity — the one hazard the design
   itself flags as most dangerous is not actually covered by its own test plan. Fix direction: treat *any*
   zone-goroutine panic as fatal to the whole validator process (crash, don't continue with divergent state —
   the safe default for anything whose panic-or-not cannot be proven purely input-deterministic), not a
   per-zone recoverable event.

Also found (majors, must fix before implementation):
- **The design's own central claim — "`MaxBlockGas` remains a hard cap, I-19 holds structurally" — is false
  for admission-time enforcement, though true for the commit-time sum.** `x/fees/keeper/fee_policy.go` reads
  `ctx.BlockGasMeter().GasConsumed()` on **every tx's ante-time admission**, and today this is accurate only
  because `consumeBlockGas()` runs strictly after tx *i*'s message execution and strictly before tx *i+1*'s
  ante — sequential fusion is what keeps the running total live during the whole ante pass. v1's own
  architecture defers message execution to per-zone goroutines and only folds real consumed gas into the live
  `BlockGasMeter` *after* the join barrier — meaning for the entire ante-pass over a block, every tx's
  admission check sees a stale/near-zero running total that never reflects any earlier tx's real
  message-execution gas within the same block. On the current chain (Core is the only populated zone, and its
  per-zone quota check is a no-op by design since `ZoneMaxGas==0`), this would silently defang the *only* real
  gas cap that matters today, collapsing the effective ceiling to `MaxBlockTxs × MaxTxGas` — which can vastly
  exceed `MaxBlockGas`. Fixing this requires either (a) converting the global cap check to a reserve-by-limit
  running counter analogous to the per-zone mechanism already shipped (a real, consensus-affecting semantics
  change to fee/congestion pricing needing its own design and owner sign-off), or (b) explicitly abandoning the
  "byte-identical to today's admission" claim — the design must do one of these, not silently assert
  equivalence while the underlying mechanism can't actually provide it.
- **The "different zones can never conflict" premise is unconditionally true only for zoned entity state
  (Phase 3/5, unstarted).** Every other stateful module — `x/bank` balances/supply outside native-account
  management, `x/staking`, `x/gov`, `x/distribution`, `x/mint`, `x/slashing`, `x/fee-collector`, `x/fees`,
  `x/aez`'s own routing-table/quota state, any future IBC modules — remains globally keyed, not
  zone-partitioned, and the design never says otherwise. Partitioning by the tx's *signer's* zone is not the
  same guarantee as partitioning by *touched keys' *zone: any message that writes into a global key from two
  different-zone transactions in the same block has both zones' branches read the same stale pre-block value
  and the later-committed zone's write silently clobbers the earlier one — a deterministic-but-wrong lost
  update (same wrong answer on every validator, so not a fork trigger, but a real value-conservation
  violation, exactly as unacceptable on a financial chain). Fix direction: explicitly enumerate zone-scoped-safe
  modules and force any message touching a non-zone-scoped module onto the sequential path.
- **"Final result order is original block order" was conflated with "results equal to what sequential
  execution would have produced."** The design establishes the first (true, via race-free index-scatter) but
  not the second, which the gas-admission blocker above shows is actually false for any handler path
  consulting the live cross-tx `BlockGasMeter`/congestion-bps signal — light clients, indexers, and replay
  tooling that reasonably assume "in original block order" means "as if executed sequentially in that order"
  would get silently different fee/congestion values with no detection mechanism named.
- **Store-safety claim has an unstated precondition**: the concurrent-read-safety argument for the shared
  parent IAVL tree requires the app's inter-block cache to stay disabled (`SetInterBlockCache` is never called
  today, confirmed by repo grep) — true today, but stated by the design as an unconditional fact rather than an
  explicit precondition a future performance change could invalidate without anyone tracing it back here.

Minor, not blocking: a citation error (the per-delivery panic-recovery precedent lives at a different line
range than cited — `deliverQueuedInternalMessage`'s recover block, not the self-destruct/drain lines quoted);
existing single-threaded `CacheContext()` usages were cited as concurrency precedent when they only establish
the single-threaded branch-then-`Write()` idiom, not concurrent-safety (the real concurrent-safety argument
stands on its own IAVL-source-reading and doesn't need that citation); the `GOMAXPROCS` variance test doesn't
separately vary Go's per-process map-iteration seed, which could hide a map-iteration-order bug that a fixed
`AllZoneIDs()`-slice-based implementation wouldn't have but a naive `map[ZoneID][]int` iteration would.

## Owner decisions needed (v1)

- Whether to raise `MaxBlockGas` once wall-clock time to process a block drops, as a separate, later, measured
  decision, or leave it untouched indefinitely (design deliberately does not decide this).
- Whether to sequence Phase 3 (native-account zone-prefixed keys) *before* landing/enabling this engine
  (recommended), or accept it ships structurally dormant for an unknown period.
- Accept the real engineering cost: this cannot be a thin `sdk.TxRunner` wrapper — baseapp v0.54.3 fuses
  ante+msgs behind unexported internals with one shared, unsynchronized `BlockGasMeter`, so this requires an
  app-owned reimplementation of the ante→msg→postHandler shape using only baseapp's exported surface. Accept
  this cost now, or wait for an SDK version exposing a real split ante/msg execution seam?
- Whether the current Phase 6 gas-quota split (Core reserved 8M / each elastic zone capped at 3M) is still
  right once real parallel throughput is measurable.

## v2 — resolved v1's 2 blockers decisively, but REJECTED again: MORE severe findings than v1, not fewer

v2 made real, verified progress on two fronts: (1) **proved, not assumed**, that Optimistic Execution can
never run concurrently with a real `internalFinalizeBlock` invocation — read the actual vendored OE source
and traced every call site (`PrepareProposal`/`ProcessProposal` unconditionally call the blocking `Abort()`
before any `Execute()`; `FinalizeBlock` unconditionally calls the blocking `WaitResult()` before falling back
to a synchronous invocation) — v1's blocker 1 is closed, confirmed correct by independent re-verification.
(2) Extended the *already-shipped* per-zone reserve-by-limit gas mechanism (`x/fees/keeper/fee_policy.go`'s
`ZoneGasConsumed`) to the *global* admission check — not a new mechanism, the same one the codebase already
uses one field over — with an honestly-quantified, explicitly-flagged economic tradeoff (conservative
admission can reject transactions an actual-consumption model would have accepted) requiring owner sign-off,
not a silent behavior change. Also correctly determined, by reading `x/native-account/keeper/zone.go`'s own
doc comment directly, that the "which modules are zone-scoped-safe" allow-list is **legitimately empty
today** — no module's storage is actually zone-prefixed yet, independently confirming v1's "zero throughput
today" conclusion from a second angle.

**v2 was REJECTED — and this round's findings are MORE severe than v1's, the opposite of the convergence
Phase D's design track showed over its three rounds:**

1. **A concrete, deterministic, synchronized, network-wide chain-halting DoS, newly introduced by v2's own
   fix.** The zone-classification/bucket-assignment step (deciding which zone-goroutine a tx goes to) must
   run *before* `RunTx`'s existing per-tx panic recovery, so it has no protection — and v2's own fatal-crash
   policy, applied to this step, means a single crafted transaction that deterministically panics during
   classification takes down **every honest validator that includes it in a block, simultaneously**, since
   zone classification is required to be deterministic (same rule, same bytes, same result everywhere). This
   is strictly worse than the status quo (`RunTx` already gracefully recovers per-tx panics into an ordinary
   failed-tx result today) and worse than v1's original rejected recover-and-continue policy.
2. **`x/contracts`'s actual current storage architecture is structurally incompatible with zone-goroutine
   concurrency, independent of whether Phase 3/5 ever lands key-prefixing.** `x/contracts/keeper.Keeper`
   holds its entire module state as a single shared Go field, mutated via a non-atomic
   read-then-build-then-replace pattern with no lock held across the read+derive step (only the final
   `assignGenesis` write is serialized) — two zone-goroutines executing `x/contracts` messages concurrently
   is a textbook lost-update race whose outcome depends on OS thread-scheduling order between validators, a
   genuine `AppHash`-divergence fork vector. Worse: `x/contracts`'s state-root computation hashes the
   **entire module state as one JSON blob on every single write**, regardless of which contract or zone it
   touches — key-prefixing (Phase 3/5's whole premise) changes *where* a record lives, not the fact that
   root/prune logic reads across every record on every write. A Msg-type-URL allow-list has no way to detect
   either hazard, since both are invisible from a message's type name alone.
3. **Fix 3 (gas reservation) and fix 2 (panic policy) contradict each other.** Fix 3's soundness proof
   implicitly depends on each zone-goroutine preserving the existing per-tx `GasWanted` ceiling and its
   graceful out-of-gas/panic isolation (today provided by `RunTx`'s own recovery middleware) — but fix 2's
   mechanism, applied literally, would route an entirely ordinary, expected, frequent event (a single tx
   running out of gas, or panicking on a routine edge case — both already happen today and are already
   gracefully isolated) to the zone-goroutine's fatal outer recover, crashing the whole validator on routine
   load rather than failing just that one tx.
4. **A concrete, code-verified instance of exactly the "looks safe but isn't" hazard the review was asked to
   hunt for.** `x/contracts`'s rent-charging path calls `SendCoinsFromAccountToModule` against a single
   global `x/bank` module account as a routine side effect of ordinary contract execution — invisible from
   any message's type URL, confirming the allow-list approach is the wrong granularity in principle, not
   merely incomplete for one module.

Also found (minor, correctly not blocking on their own): a citation error in which ABCI paths carry
`recover()` (the load-bearing conclusion survived independent re-verification regardless); an unstated
dependency of `oe.Abort()`'s own internal synchronization on CometBFT's single-connection ABCI calling
convention (no practical failure scenario under that convention, flagged for completeness).

## Status

**Not started. Do not implement.** Two rounds now — and unlike Phase D's design track (3 rounds, strictly
DECREASING severity, converging toward owner sign-off) or even the Phase E/F call-mechanism track (4 rounds,
roughly constant severity), this track's second round surfaced **more numerous and more severe** blockers
than its first, including a newly-introduced DoS vector and a structural incompatibility
(`x/contracts`'s whole-state-hash-per-write architecture) that no amount of message-type classification can
route around. This is a clear, stronger-than-usual signal to stop automated iteration: real parallel
execution for AEZ is gated on `x/contracts` itself getting the Phase 5 rewrite `aez.md` already calls "the
expensive one, there is no incremental version of it," *and* on resolving a genuine tension between
per-tx failure isolation and process-level crash safety that has no design-doc-only answer — this needs
direct owner/architect involvement to decide the sequencing (Phase 5 first? a narrower v1-scope limited to
only truly-isolated modules? abandon deferred/parallel execution in favor of a completely different
throughput strategy?) rather than a v3 automated pass. Recommend surfacing this conclusion, and the two
design docs' accumulated owner-decision lists, directly to the owner. AEZ's existing isolation/fairness
properties (Phase 1-2, 6, already shipped) are unaffected by this pause and remain valid and in production
regardless of how or whether real parallel execution is ever pursued.
