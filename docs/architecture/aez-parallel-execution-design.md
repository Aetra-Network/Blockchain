# AEZ real parallel execution — design v1 (REJECTED)

Status: **First adversarial review round found 2 blockers and 4 majors, including one that directly refutes
the design's own central claim that `MaxBlockGas` stays correctly enforced. Do not implement below as-is.**
This is a new design track, not in the original AEZ phased plan (`docs/architecture/aez.md`), requested to
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

## Status

**Not started. Do not implement.** This is the FIRST round for this design track. Unlike a well-scoped
feature addition, this design reworks a fundamental cross-cutting property of the app's execution model
(admission-time gas accounting, which the fee/congestion-pricing system depends on) and collides with an
already-shipped feature (Optimistic Execution) — the blockers found are structural, not implementation
details. Proceed to a v2 pass targeting specifically: (a) an explicit reconciliation with or disabling of
Optimistic Execution when the AEZ runner is active; (b) panic-is-fatal-not-recoverable for zone-goroutine
faults; (c) a concrete redesign of admission-time gas checking compatible with deferred/parallel message
execution (the hardest, most consensus-and-economics-sensitive item — likely needs its own dedicated design
sub-track); (d) an explicit list of modules that force a message onto the sequential path. Given the
depth of blocker (c) in particular, this track should be watched for the same non-convergence pattern the
Phase E/F call-mechanism track showed (4 rounds, no convergence) rather than assumed to resolve quickly.
