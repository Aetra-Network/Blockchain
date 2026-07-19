# AEZ throughput preservation under load-skew (admission-control only)

## Revision note (post-adversarial-review, pre-implementation)

Two adversarial reviews were run against the original draft of this document.
Both are addressed below, in the sections they touch, rather than only here;
this note is a map to where.

**Review 1 (correctness/consensus): FAIL, one blocker (Finding 1).** The
original §1.3 special-cased Core as "admitted subject only to the global
backstop, never its own cap" -- an unconditional exemption, not a rollover
participant. The reviewer proved that this let Core's (or, in Finding 2's
variant, a second elastic zone's) excess-of-floor consumption exhaust the
*global* counter before a victim zone's own message was ever reached, even
though that message fit entirely inside the victim's own dedicated cap --
directly contradicting this document's own headline claim. **Fix:** §1.3/§1.4
below now treat Core symmetrically as just another participant in the same
own-allotment-then-rollover mechanism (its own allotment is `ReservedGas`
rather than an elastic zone's `MaxGas`, but the admission logic is identical
and uses the same shared surplus pool in both directions). This is not a
narrower-scoped workaround (the review's alternative option (a)); it is a
strictly stronger fix that closes the gap in both directions at once, and, as
a direct consequence, *also* resolves Review 2's finding below without
touching a single test assertion. Finding 2 (rollover-pool fairness among
multiple simultaneously-*overloaded* zones/Core contesting the same surplus)
remains present by design -- the reviewer already characterized it as
non-blocking -- and is now documented explicitly in §5.1b.

**Review 2 (scope/honesty): CONDITIONAL PASS, one concrete self-contradiction.**
The reviewer proved that §1.5's claim "`TestDrainBudgetStopsAndResumes`
continues to pass unchanged" was false against the *original* algorithm,
because populating `DefaultParams().MessageQuota` would cap the test's
single busy elastic zone at 1,000,000 (its own cap) + at most 3,000,000
(rollover from the three other idle elastic zones) = 4,000,000, not the
8,000,000 the test hard-asserts -- a real capacity reduction in the single-
busy-zone case the reviewer also flagged separately in point (c). **Fix:**
the same Finding-1 fix above resolves this as a side effect, not a
coincidence: with Core folded symmetrically into the rollover pool, an idle
Core's full 4,000,000 floor becomes available surplus alongside the three
idle elastic zones' 3,000,000, giving the sole busy zone access to the full
legacy 8,000,000 budget when nothing else is active -- exactly reproducing
`TestDrainBudgetStopsAndResumes`'s existing numbers with **zero test changes
required** (worked out in the revised §3.4/§3.5 and confirmed against the
actual test file before implementation). This also closes point (c)'s
"common case throughput drops to a hard ceiling" concern: it does not, in the
case that matters most (everything else idle, including Core).

Both reviews' minor/non-blocking nits (Review 1 Findings 3-4; Review 2's two
implementation-completeness gaps) are addressed at their original locations
in §5.4 and in the implementation notes; see those sections for the specific
wording changes.

Status: **design, revised post-review, ready for implementation.** Scope: admission-control / scheduling only.
No concurrent execution, no `PrepareProposal`/custom mempool, no `x/contracts`
changes. This document targets one concrete acceptance criterion from the owner
(paraphrased): *"if the chain is loaded, AEZ preserves per-zone throughput --
the chain keeps working fast."* That is a **load-skew** claim ("zone A's flood
does not crowd out zone B"), which is distinct from and does not contradict
`docs/architecture/aez.md` §4.9's **capacity** claim ("zones do not add
throughput" -- the global `MaxBlockGas = 20,000,000` ceiling is unchanged and
unchangeable by anything sequential). Both are true at once; this document is
about the load-skew property only.

## 0. What already exists (verified against the working tree)

Two independent per-zone gating mechanisms exist today. Both were read in full
before writing this document; line numbers below were spot-checked against the
current tree, not taken on faith from a prior pass.

### 0.1 The tx-admission gate (live, functionally inert)

- `x/aez/types/quota.go` -- `GasQuotaParams`/`ZoneGasQuota`: Core Zone (`ZoneID
  0`) gets a `ReservedGas` **floor** and `MaxGas == 0` (the uncapped sentinel);
  each of the 4 elastic zones gets a `MaxGas` **cap** and `ReservedGas == 0`.
  `Validate()` (quota.go:80-125) enforces: `MaxBlockGas > 0`; exactly
  `ZoneCount` (5) quotas in dense ascending `ZoneID` order; Core never capped;
  elastic zones never reserve; elastic caps sum, overflow-checked, to at most
  `MaxBlockGas - CoreReservedGas` (equality by default: `4*3,000,000 +
  8,000,000 == 20,000,000`, matching `x/fees/types/fee_model.go:23`'s
  `DefaultMaxBlockGas`).
- `x/aez/keeper/quota.go` -- `GasQuotaForZone(ctx, zoneID)` reads committed
  params on every call (no cache, the F-17/I-20 discipline) and returns `0`
  (uncapped) for Core.
- `x/fees/keeper/zone.go` -- `ZoneResolver` interface (`ZoneOfAddress` +
  `GasQuotaForZone`, stdlib-typed) declared on the **consumer** side; `x/aez`'s
  keeper satisfies it structurally, so `x/fees` imports `x/aez` not at all.
- `x/fees/keeper/fee_policy.go` `AdmitTx` (~line 37-236): resolves the tx's
  home zone on an infinite-gas child context (gas-neutral, so it cannot move
  the metered `gasUsed`/congestion bps), reads that zone's reserved-so-far gas
  via a height-keyed self-resetting counter (`getZoneGasConsumed`,
  fee_policy.go:287-305), and gates admission **additionally** to (never
  instead of) the untouched global `MaxBlockGas` check
  (`x/fees/types/fee_model.go` `ValidateAdmission`, ~line 165-218). A nil
  resolver, nil sender, or resolver error degrades to Core/uncapped -- **never
  fatal** (fee_policy.go:70-88). This precedent -- degrade-to-safe-default,
  never panic/reject-by-default -- is reused throughout this design.
- **Why it is inert today:** `x/aez/types/routing_table.go`
  `GenesisRoutingTable()` (verified: lines 62-68) maps all 256 buckets to
  `ZoneIDCore`. Every tx's sender resolves to Core, `GasQuotaForZone` returns
  `0` for Core, and the per-zone branch in `ValidateAdmission`
  (fee_model.go:191-195, `if in.ZoneMaxGas > 0`) is never entered. There is no
  `Params.Prototype.Enabled` gate on this path at all -- `ResolveZone`/
  `GetRoutingTable` never check it (confirmed: `x/aez/keeper/routing.go`
  `ResolveZone`, `x/aez/keeper/table.go`). The moment a routing table splits
  buckets across zones, this gate goes live with no separate switch.

### 0.2 The message bus and its single global drain budget (the gap this document closes)

- `x/aez/types/message.go:28-52` (verified): three bus-wide constants, **not**
  committed `Params` -- deliberately, per the file's own comment, because "the
  drain path reads no param... so there is no governance-raised value a
  restarted node could hold stale":
  - `MaxZoneMessageQueueDepth = 65536` (inbox cap, I-21)
  - `MaxBounceDepth = 8`
  - `ZoneMessageGasPerBlock = 8_000_000` -- **the single global, non-zone-weighted
    per-block drain budget.** The file's own comment (message.go:41-46) names
    the gap directly: *"Phase 6 replaces this single global budget with a
    per-zone budget plus a Core reservation; that is deliberately NOT built
    here."*
  - `MaxGasPerDelivery = 1_000_000` -- clamps one delivery's charge (mirrors
    `x/fees` `DefaultMaxTxGas`). **Zone-independent; unchanged by this design.**
- `x/aez/keeper/inbox.go` `scanDueInbox` (verified lines 62-91): a **single,
  height-first-keyed, cross-zone** range scan. Critically, `InboxKey` (`x/aez/
  types/keys.go:109-116`) is `0x08 || deliver_height_be8 || message_id_32` --
  **there is no zone in the physical key**, by design, because the
  re-resolution rule (aez.md:551-556) means a message's destination zone can
  change between enqueue and delivery. So "all messages due this block, across
  every zone, interleaved by message id" is one snapshot (`due
  []dueInboxMessage`), fully materialized in memory before the drain loop
  runs -- **not a live per-zone iterator**. This matters directly for the
  design below: there is no cheap way to iterate "zone A's due messages" and
  "zone B's due messages" separately without either (a) a zone-keyed
  sub-index (a new mechanism, not attempted here) or (b) a second pass over
  the already-materialized `due` slice (what this design does).
- `x/aez/keeper/drain.go` `DrainWith` (verified lines 57-86): single ordered
  pass over `due`; for each message, clamp `gasCost` to `[1, MaxGasPerDelivery]`
  (`0` or over-max both clamp to `MaxGasPerDelivery`); if `gasCost > budget`,
  **`break` -- stop the entire drain**, not skip-and-continue; otherwise
  `budget -= gasCost` and deliver. Nothing is ever dropped (undelivered
  messages stay queued and are retried on a later block,
  `TestDrainBudgetStopsAndResumes`), but a flood that sorts early in
  `(deliver_height, message_id)` order can exhaust the shared 8,000,000 before
  a *single* message from an uninvolved zone is ever reached this block.
- The budget is a **local variable inside `DrainWith`**, computed fresh every
  call from the constant -- there is no committed or transient store key for
  it today.

### 0.3 What is confirmed dormant / paused (do not touch)

- No module's storage is zone-partitioned today. `x/native-account/keeper/
  zone.go` (verified) only *derives* a zone tag (`ZoneOf`/`AccountZone`); it is
  never persisted, and a nil resolver degrades to `(Core, Resolved=false)`,
  never an error. `docs/architecture/aez.md`'s Phase 3 section describes
  physical zone-prefixed keys for `x/native-account` as a future phase, not
  something that shipped -- stating this accurately here per the task's
  correction, since aez.md's own prose still reads as if it's pending future
  work (which it is).
- `docs/architecture/aez-parallel-execution-design.md` (verified): two
  adversarial rounds (v1, v2) on real concurrent zone-goroutine execution were
  **both rejected**, v2 with *more* severe findings than v1 (a network-wide
  crash-DoS vector, and proof that `x/contracts/keeper/keeper.go`'s
  `assignGenesis` (verified: keeper.go:103) / `ComputeContractsStateRoot`
  (verified: `x/contracts/types/types.go:228`, hashing the **entire** module
  state as one JSON blob) make per-zone contract state structurally
  unrepresentable today, independent of the concurrency question). That track
  stays paused pending direct owner/architect review. **This document's
  design touches none of it**: no goroutines, no deferred execution, no
  concurrent writers -- every mechanism below runs strictly sequentially inside
  the existing `BeginBlocker`/ante-handler call graph.
- `app/aetra_core_wiring.go` (verified lines 36-98) panics unless
  `AetraCoreRoutingExecutionPoint() == RoutingExecutionPointAnteAdmissionOnly`,
  the only value `app/wiring/aetracore/modules.go` defines. **This design adds
  no new routing execution point.** It does not reorder transactions within a
  block (CometBFT's given tx order inside a block is untouched) and does not
  reorder *messages* relative to any tx-ordering concern -- the message-bus
  drain's internal admit/skip decision order (§1.3 below) is a `BeginBlocker`-
  internal accounting detail entirely within `x/aez`'s own existing authority
  (it already reorders relative to raw enqueue order today, by design --
  canonical id order, not enqueue order, per
  `TestDrainResultIsIdenticalRegardlessOfEnqueueOrder`). It is not the
  intra-block *transaction* reordering that would require `PrepareProposal`.

---

## 1. The concrete gap and its fix: per-zone-weighted message-bus budget

### 1.1 Design decision: keyed by **source zone**, not destination, not both

A `ZoneMessage` carries two zone fields (`x/aez/types/message.go:109-166`,
verified):

- `SourceZone` -- the sender's zone **at enqueue**, stamped once, never
  re-resolved, permanent for the life of the message (including through a
  bounce -- `enqueueBounce`, drain.go:200-239, sets the bounce's `SourceZone =
  attemptedZone`, i.e. wherever delivery just failed).
- `DestZoneAtEnqueue` -- observability only; the *real* destination is
  **re-resolved** at delivery time via `resolveRecipientZone` (inbox.go:160-162)
  against the routing table current at drain time, because the recipient may
  have moved zones across an epoch boundary since enqueue (the re-resolution
  rule).

**Decision: the per-zone message-bus budget is keyed by `SourceZone` only.**

Reasons, in order of weight:

1. **It is the direct analogue of the already-shipped, already-proven
   mechanism.** The tx-admission gate (§0.1) charges a tx's declared
   `GasLimit` against the **sender's** zone, never against any zone a tx's
   messages might touch. Keying the message-bus budget by the **message's
   origin zone** is the same "the entity that produced the load pays for it"
   principle, one hop later in the pipeline (a cross-zone message is itself
   the byproduct of some earlier admitted tx in the source zone). Reusing an
   identical mental model both for the reviewer and for anyone auditing the
   two mechanisms together is a real property, not just tidiness.
2. **It is free; dest-keying is not.** `SourceZone` is a field already sitting
   on the in-memory `ZoneMessage` from the `scanDueInbox` snapshot -- reading
   it costs nothing. Charging by *destination* would require calling
   `resolveRecipientZone` (a routing-table read, possibly a bucket hash) for
   **every candidate message before deciding whether it is even admitted this
   block** -- including every message that ends up skipped. Today,
   `resolveRecipientZone` is only ever called once admission is already
   decided (inside `processInboxMessage`). Moving it earlier, into the hot
   admission-decision loop, adds a real per-message cost that scales with
   however many messages get scanned-and-rejected, for a resource
   (destination-zone fairness) that Phase 4a's delivery is a no-op for anyway
   (see point 3).
3. **The resource being protected has no real destination-side cost yet.**
   `deliverMessage` (drain.go:38-40) is a deterministic no-op in Phase 4a --
   there is no recipient executor wired in. The "gas" this budget spends is
   **bus processing capacity**, attributable to whoever is generating
   cross-zone traffic (the source), not to whoever happens to be named as a
   recipient. Charging the recipient's zone for *being sent mail* has no
   natural justification here, the same way an email system rate-limits
   senders, not inboxes.
4. **Keying by both independently was considered and rejected as
   double-accounting with no offsetting benefit.** A message's SourceZone and
   its resolved DestZone are typically two *different* zones (same-zone pairs
   never produce a message at all -- `EnqueueMessage`'s Guard 2,
   outbox.go:67-71). Charging one delivery against **two** independent
   budgets means a single spammy zone-pair interaction drains capacity from a
   zone that did nothing wrong (the destination), which is the opposite of
   the isolation property this feature exists to provide. If a future phase
   needs a genuine destination-side protection (e.g., once Phase 4b/5 gives
   delivery a real, variable execution cost), it should be its own
   *separate*, explicitly-designed budget -- not silently folded into this one
   by keying on both.

### 1.2 Shape: mirror `GasQuotaParams` exactly

New type in `x/aez/types/quota.go` (same file as `GasQuotaParams`, so the two
per-zone budget shapes stay textually adjacent and are reviewed together):

```go
// ZoneMessageQuota is one zone's slice of the cross-zone message-bus's
// per-block drain budget. Same shape as ZoneGasQuota, deliberately: Core is
// a floor (ReservedGas, MaxGas == 0, uncapped), elastic zones are hard caps
// (MaxGas, ReservedGas == 0).
type ZoneMessageQuota struct {
	ZoneID      ZoneID
	MaxGas      uint64
	ReservedGas uint64
}

// MessageQuotaParams is the whole per-zone message-bus drain budget, carried
// inside x/aez Params (Params.MessageQuota) -- committed, governable state,
// exactly like Params.GasQuota. It is DELIBERATELY a separate total from
// GasQuotaParams.MaxBlockGas: message-bus drain gas and tx-execution-
// admission gas are two independent counters protecting two independent
// resources (BeginBlock delivery processing vs ante-time tx admission) that
// happen to share a "gas" unit name. Nothing requires or checks any numeric
// relationship between the two totals.
type MessageQuotaParams struct {
	TotalMessageGasPerBlock uint64
	// Quotas holds exactly ZoneCount entries in ascending, dense ZoneID
	// order, identical discipline to GasQuotaParams.Quotas.
	Quotas []ZoneMessageQuota
}
```

`DefaultMessageQuotaParams()`:

```go
const (
	defaultTotalMessageGasPerBlock = uint64(8_000_000) // == today's ZoneMessageGasPerBlock; NOT a capacity increase
	defaultCoreMessageReserved     = uint64(4_000_000)
	defaultElasticMessageMaxGas    = uint64(1_000_000) // == MaxGasPerDelivery: guarantees >=1 max-size delivery per elastic zone per block
)
```

`4 * 1,000,000 + 4,000,000 == 8,000,000` -- equality by default, same
discipline as the tx-gate's `4*3,000,000 + 8,000,000 == 20,000,000`. The total
is **unchanged from today's constant** (8,000,000): this redistributes the
existing budget, it does not enlarge it (non-goal (a), §4).

Rationale for `defaultElasticMessageMaxGas == MaxGasPerDelivery`: every elastic
zone is guaranteed **at least one** maximum-size cross-zone delivery per
block, unconditionally, regardless of what any other zone is doing. That is
the concrete, memorable form of "zone B's throughput is not crowded out by
zone A's flood."

`Validate()` mirrors `GasQuotaParams.Validate()` field-for-field (quota.go:80-125):

- `TotalMessageGasPerBlock > 0`.
- Exactly `ZoneCount` entries, dense ascending `ZoneID` order (index `i` is
  zone `i`; out-of-order or wrong-length both rejected).
- Core (`ZoneID 0`): `MaxGas` must be `0` (never capped -- capping Core would
  mean a Core-only chain's single populated zone drops from 8,000,000 to
  `ReservedGas`, breaking inertness exactly as `GasQuotaParams.Validate`'s
  comment explains for the tx-gate).
- Elastic zones: `ReservedGas` must be `0`; `MaxGas` must be positive.
- Elastic `MaxGas` sum, overflow-checked, plus Core `ReservedGas`,
  overflow-checked, must not exceed `TotalMessageGasPerBlock`.

New error `ErrInvalidMessageQuota` in `x/aez/types/errors.go` (kept distinct
from `ErrInvalidGasQuota` since they validate two different committed tables
and a caller/test may reasonably want to tell them apart).

Accessors, mirroring `quota.go`'s existing `MaxGasForZone`/`CoreReservedGas`:

```go
func (q MessageQuotaParams) MaxMessageGasForZone(zoneID uint32) (uint64, error)
func (q MessageQuotaParams) CoreReservedMessageGas() uint64
```

**Plus one new accessor the corrected §1.3 algorithm needs that has no tx-gate
analogue**, because the tx-gate never lets Core draw on elastic surplus and so
never needed a uniform "this zone's own slice" accessor across both kinds of
zone:

```go
// OwnAllotmentForZone returns ReservedGas for the Core Zone and MaxGas for an
// elastic zone -- the "own cap" both passes of the two-pass drain algorithm
// (drain.go) index by. Core participates in the SAME own-allotment-then-
// rollover mechanism as every elastic zone: this is what makes that
// symmetric (see the revision note and the corrected §1.3/§1.4 below).
func (q MessageQuotaParams) OwnAllotmentForZone(zoneID uint32) (uint64, error)
```

`x/aez/types/params.go`: add `MessageQuota MessageQuotaParams` to `Params`;
`DefaultParams()` sets it via `DefaultMessageQuotaParams()`; `Params.Validate()`
adds `if err := p.MessageQuota.Validate(); err != nil { return err }` alongside
the existing `p.GasQuota.Validate()` call.

`x/aez/keeper/quota.go`: add `MessageGasQuotaForZone(ctx, zoneID) (uint64,
error)` mirroring `GasQuotaForZone` exactly (reads committed params fresh on
every call, no cache). This exists for queries/tests/observability; the drain
hot loop (below) reads `params.MessageQuota` **once** per `DrainWith` call, not
once per candidate message, so this accessor is not on the hot path.

### 1.3 Unused-budget policy: **rolls over within the block** (recommended), with the tradeoff stated

The task requires an explicit choice. **Recommendation: unused elastic-zone
budget rolls over, within the same block's drain, to other elastic zones that
have exceeded their own cap.** Justification and the mechanism that makes it
implementable without violating determinism (I-22) or the "no reordering"
constraint:

`scanDueInbox` already returns `due []dueInboxMessage` as a **fully
materialized slice**, not a live/streaming iterator (verified,
inbox.go:68-91). That means a full second look at the same snapshot costs
nothing architecturally new -- it is not a new store read, not a new
nondeterminism surface, and does not touch CometBFT's tx ordering at all
(this is purely `x/aez`'s own `BeginBlocker`-internal bookkeeping, which
already reorders relative to raw enqueue order today by canonical id, per
`TestDrainResultIsIdenticalRegardlessOfEnqueueOrder`). This makes a two-pass
algorithm both cheap and safe:

**Corrected after Review 1's Finding 1** (see the revision note at the top of
this document): Core is **not** a special-cased "only the global backstop
applies" path. It is folded into the *same* own-allotment-then-rollover
mechanism as every elastic zone, using `ReservedGas` as its own allotment
instead of an elastic zone's `MaxGas`. This is a strictly stronger fix than
scoping the guarantee down to "elastic-vs-elastic only": it protects an
elastic zone's own cap from Core's excess consumption (closing Finding 1)
*and* protects Core's floor from elastic zones (preserving the original I-18
analogue) *and* lets Core draw the full budget when elastic zones are idle
(preserving inertness) -- all from one uniform loop, with no Core-shaped
branch anywhere in Pass 2.

**Pass 1 -- measure unused capacity (no state mutation, no delivery).**
Using fixed-size `[ZoneCount]uint64` arrays (never a map -- I-22), for
**every** zone `z` including Core, look up `ownAllotment[z] :=
OwnAllotmentForZone(z)` (Core's `ReservedGas`; an elastic zone's `MaxGas`) and
accumulate `demand[z] = sum of clamped gasCost for every due message with
SourceZone == z` (overflow-guarded; saturates rather than wraps). Then:

```
usedOwnCap[z]      = min(demand[z], ownAllotment[z])
surplusFromZone[z] = ownAllotment[z] - usedOwnCap[z]   // 0 if z met or exceeded its own allotment
totalSurplus       = sum(surplusFromZone[z] for EVERY zone, Core included)
```

Note what this does and does not change relative to the original draft: if
Core actually has due traffic this block, `demand[ZoneIDCore]` reflects that
real demand, `usedOwnCap[ZoneIDCore]` is capped at `ReservedGas` exactly like
an elastic zone's `usedOwnCap` is capped at its `MaxGas`, and
`surplusFromZone[ZoneIDCore]` is `0` whenever Core's real demand meets or
exceeds its floor -- so a busy Core still cannot have its floor eaten by
elastic zones, the original I-18 analogue holds unchanged. What changes is
only the case Finding 1 exploited: Core no longer has an *unconditional* path
past its own allotment -- past `ReservedGas`, Core must now draw on the same
measured `totalSurplus` pool an elastic zone draws on, so Core's excess
consumption can never again crowd an elastic zone's own guaranteed cap out of
the shared global counter before that zone's message is even reached.

**Pass 2 -- spend, in the unchanged canonical `(deliver_height, message_id)`
order.** Per-zone counters `spent[z]` (for every zone, Core included), a
running `totalSpent`, and `surplusRemaining := totalSurplus`, all initialized
once per `DrainWith` call:

```
for each due message m (canonical order):
    g := clamp(m.GasLimit, 1, MaxGasPerDelivery)   // unchanged clamp rule
    if totalSpent + g > TotalMessageGasPerBlock:
        skip m, continue                          // global backstop, see 1.4
    z := m.SourceZone                              // Core included, no special case
    if spent[z] + g <= ownAllotment[z]:
        admit m; spent[z] += g; totalSpent += g     // own-allotment path
    else if g <= surplusRemaining:
        admit m; surplusRemaining -= g; totalSpent += g   // rollover path
    else:
        skip m, continue                           // stays queued; NOT a break
```

`admit m` calls `processInboxMessage` exactly where today's code calls it
(drain.go:81) -- the only change is that a message failing its budget check
is `continue`d past rather than causing the whole loop to `break`. Admitted
messages are still processed in canonical id order (a strict subsequence of
today's order), so `TestDrainResultIsIdenticalRegardlessOfEnqueueOrder`'s
"delivery order is the canonical id order" property is preserved unchanged;
only "which messages are admitted" changes.

**Why this cannot regress the elastic-vs-elastic proof that was already
correct:** the algebra Review 1 verified for "any zone's own-allotment
admission can never be blocked by another zone's excess consumption" did not
actually depend on Core being special-cased -- it depended only on (a) every
zone's own-path spend being bounded by its own allotment, and (b) the shared
surplus pool being measured *before* Pass 2 runs, from real per-zone demand.
Both hold for Core exactly as they hold for an elastic zone once Core is
folded into the same arrays, which is precisely why making Core symmetric
closes the gap rather than merely relocating it.

**Tradeoff, stated explicitly (the task requires this even for the
recommended option):** rollover means a heavily-loaded zone *can* legitimately
consume another, currently-idle zone's allotment within the same block --
i.e., the per-zone cap is a **guaranteed minimum**, not a **hard ceiling**,
whenever slack exists elsewhere. The alternative (strict, non-rolling
reservation: each elastic zone's own cap is a hard ceiling every block, no
matter how idle its neighbours are) wastes capacity whenever any zone has
nothing queued -- exactly the "hard isolation, wastes capacity" case the task
names -- but gives a **stronger, simpler-to-reason-about worst-case
guarantee** ("zone B's own cap is always exactly its own cap, full stop,
independent of anything else happening this block"). Rollover is recommended
because (a) it strictly weakly dominates strict reservation for the property
under test -- a zone's own reserved minimum is never reduced by rollover, it
can only ever gain access to more when others are idle -- and (b) the
two-pass algorithm above makes it exactly as auditable and exactly as
deterministic as the strict variant, so there is no implementation-simplicity
reason to prefer strict isolation instead. If a future audit finds rollover
lets one adversarial zone camp on other zones' idle capacity in a way that
matters in practice, switching to strict reservation is a one-line change
(delete the `else if g <= surplusRemaining` branch) with no other mechanism
affected.

### 1.4 The global backstop is unconditional; every zone's own floor/cap is a protected guaranteed minimum, symmetrically

Restated post-Finding-1-fix, because these are load-bearing and easy to
silently lose while refactoring the loop:

- **I-19 analogue (unchanged):** `totalSpent + g > TotalMessageGasPerBlock`
  always skips, regardless of per-zone accounting. This is checked first, on
  every candidate, admitted-via-own-allotment or admitted-via-rollover alike.
  The per-zone/rollover machinery can never cause the aggregate to exceed the
  total (the same "global check stays authoritative" discipline
  `x/fees/types/fee_model.go:182-195` already documents for the tx-gate).
- **I-18 analogue, now bidirectional (this is the Finding-1 fix):** a zone's
  own allotment -- `ReservedGas` for Core, `MaxGas` for an elastic zone -- can
  never be consumed by *any other zone's* excess spending, Core included in
  both roles (protector and, potentially, protectee). This holds because
  Pass 1 measures each zone's real demand against its own allotment *before*
  Pass 2 runs, and `totalSurplus` is built only from what that measurement
  found idle; a zone that actually wants its own allotment this block always
  gets it, in Pass 2, before the global backstop can ever be the reason it
  doesn't (see the worked algebra at the end of §1.3). The original draft's
  claim -- "a contract-storm-equivalent flood of cross-zone message traffic
  must never be able to starve a Core-sourced bounce or a governance-
  triggered message" -- is preserved (Core's floor still cannot be eaten by
  elastic zones), and the document now additionally guarantees the reverse
  that the original draft implicitly assumed but did not actually deliver:
  an elastic zone's own cap cannot be eaten by Core's (or another zone's)
  excess spending either.
- **Inertness is preserved by the same mechanism, not a separate carve-out.**
  On a Core-only chain (production genesis, all buckets on Core), every
  elastic zone is permanently idle, so its full `MaxGas` becomes measured
  surplus every block; Core's own allotment (`ReservedGas`) plus that full
  elastic surplus sums to `TotalMessageGasPerBlock` by construction (the
  `Validate()` equality default), so Core can still reach the entire legacy
  8,000,000 budget exactly as it can today. Symmetrically, whenever Core is
  idle (true of every test fixture in this document, §2 -- no bucket ever
  hashes to Core), Core's `ReservedGas` becomes measured surplus available to
  a busy elastic zone, which is what makes
  `TestDrainBudgetStopsAndResumes` continue to pass with its existing
  numbers unchanged (see the revised §3.4/§3.5 and the revision note).

### 1.5 Files and functions touched

| File | Change |
| --- | --- |
| `x/aez/types/quota.go` | Add `ZoneMessageQuota`, `MessageQuotaParams`, `DefaultMessageQuotaParams()`, `(MessageQuotaParams).Validate()`, `.MaxMessageGasForZone()`, `.CoreReservedMessageGas()`, and `.OwnAllotmentForZone()` (the post-Finding-1-fix accessor, §1.2) |
| `x/aez/types/errors.go` | Add `ErrInvalidMessageQuota` |
| `x/aez/types/params.go` | Add `Params.MessageQuota MessageQuotaParams`; wire into `DefaultParams()` and `Params.Validate()` |
| `x/aez/types/message.go` | Keep `MaxGasPerDelivery` unchanged. Rename `ZoneMessageGasPerBlock` to `LegacyGlobalMessageGasPerBlock` (same value, `8_000_000`) and re-document it as the migration-safety fallback total only (§5.4) -- not deleted, because the fallback path (§5.4) must reproduce it exactly, byte-for-byte, not merely "a similar number" |
| `x/aez/keeper/quota.go` | Add `MessageGasQuotaForZone(ctx, zoneID) (uint64, error)` |
| `x/aez/keeper/drain.go` | `DrainWith` reads `params.MessageQuota` once (only when `len(due) > 0`, preserving the I-23 fast path) and dispatches to one of two helpers: `drainWeighted` (the corrected two-pass algorithm, §1.3/§1.4) when `MessageQuota.Validate()` succeeds, else `drainLegacyGlobalBudget` (the migration-safety fallback, §5.4) -- both replace the single inline loop that used to be `DrainWith`'s body. No change to `processInboxMessage`, `enqueueBounce`, or `safeDeliver` |
| `x/aez/keeper/quota_test.go`, `x/aez/types/quota_test.go` | Mirror every existing `GasQuotaParams`/`GasQuotaForZone` test for the new `MessageQuotaParams`/`MessageGasQuotaForZone` (capped-Core rejected, elastic-reservation rejected, zero-elastic-cap rejected, wrong-length rejected, out-of-order rejected, duplicate-zone rejected, overflow rejected, zero-total rejected, governed-update-reflected, restart-reads-committed-value), plus `OwnAllotmentForZone` coverage (Core returns `ReservedGas`, elastic returns `MaxGas`, out-of-range errors) |
| `x/aez/keeper/drain.go`'s test file (`bus_test.go`) | `TestDrainBudgetStopsAndResumes` continues to pass **unchanged, verified against the actual test file, not merely asserted** -- see the revision note and §3.4/§3.5: with the Finding-1 fix, its single busy elastic zone draws idle Core's `ReservedGas` (4,000,000) plus the three other idle elastic zones' `MaxGas` (3,000,000) as rollover on top of its own 1,000,000 cap, reproducing the legacy 8-then-1 admission split exactly. New multi-zone tests added per §3 |
| `app/upgrades` (new file, structural `AEZKeeper` interface + `FixupAEZMessageQuota` helper, following the already-established convention of `app/upgrades/native_account.go`'s `NativeAccountVersionUpgradePlan`/`ValidateNativeAccountVersionUpgradeHandler`) | A ready, exported, unit-tested Layer-1 fixup function (§5.4) -- **not** force-wired into the existing `v053-to-v054` `SetUpgradeHandler` closure, for the same reason `native_account.go`'s helpers aren't: that closure is an explicit reference implementation for an unrelated SDK version bump, and (per Review 1 Finding 3, confirmed by grep) there is today no production call to `x/aez.Keeper.SetParams` other than `InitGenesisState` -- no live upgrade plan exists yet for this to hang off of. A real future upgrade plan calls this from its own named handler, at that plan's own boundary. Layer 2 (below) does not depend on Layer 1 ever running. |

No change to `x/aez/types/genesis.go`: `MessageQuota` lives inside `Params`,
which genesis already carries whole (`GenesisState.Params`) -- it needs no new
top-level genesis field, the same way `GasQuota` needed none when it landed.

---

## 2. Test/benchmark routing table (fixture only, not a genesis proposal)

`GenesisRoutingTable()` maps all 256 buckets to Core today
(`routing_table.go:62-68`, verified) and **this document does not propose
changing that.** Whether/when production genesis or a governance-driven table
update actually activates elastic zones is a separate, larger decision for the
owner -- out of scope here.

To exercise and prove the per-zone message-bus budget, tests need a table that
actually splits buckets across zones. `x/aez/keeper/bus_test.go`'s existing
`installTable` helper (verified lines 76-88) already installs a table through
the **real** mechanism -- `SetPendingRoutingTable` + activation at an epoch
boundary via `MaybeActivatePendingRoutingTable` -- so no test can express a
table the chain itself could not produce. Reuse it verbatim; do not add a new
test-only setter.

**Fixture: `fourElasticZonesRoundRobin`** -- a `func(bucket int) types.ZoneID`
for `installTable`'s `assign` parameter:

```go
// fourElasticZonesRoundRobin is a TEST FIXTURE, not a production genesis
// proposal. It assigns bucket b to elastic zone (b % 4) + 1, so all four
// elastic zones are populated roughly evenly (64 buckets each) and NO bucket
// hashes to Core -- Core is reachable in these tests only through
// CorePinned's pin short-circuit (system accounts, names), exactly as it is
// in production, never through the table. This maximizes elastic-zone
// realism for the load test in section 3 while leaving Core's own pin
// invariants (I-9) completely untouched and still exercised.
func fourElasticZonesRoundRobin(bucket int) types.ZoneID {
	return types.ZoneID(bucket%4 + 1)
}
```

Install it exactly as `bus_test.go`'s existing multi-zone tests do:
`installTable(t, k, scheduleHeight, version, activationHeight,
fourElasticZonesRoundRobin)`.

---

## 3. The acceptance-criteria load test

### 3.1 What existing pattern this extends

- Harness: `x/aez/keeper/bus_test.go`'s `busKeeper`, `busCtx`, `installTable`,
  `addr`, `bucketOf`, `recorder` (an injectable, call-recording
  `DeliveryFunc`), and `x/aez/keeper/multizone_test.go`'s pattern of driving
  real multi-zone behavior through the production entry points
  (`EnqueueMessage`, `DrainWith`/`BeginBlocker`) rather than poking internal
  state. This is a `testing.T`-based deterministic correctness test, not a
  `testing.B` wall-clock benchmark: the property under test ("how many
  messages admit this block, and which ones") is a pure function of committed
  params and the enqueued set, so it belongs in the same file family as
  `TestDrainBudgetStopsAndResumes`, extended, not a new harness.
- A companion wall-clock characterization benchmark (the `x/fees/types/
  bench_test.go`/`x/mesh/types/bench_test.go` style already used elsewhere in
  this tree) is worth adding **only** to confirm the two-pass scan over
  `MaxZoneMessageQueueDepth = 65536` messages stays cheap (it is `O(n)` twice
  over a fixed-size array, no maps, no allocations proportional to zone
  count) -- this is a performance sanity check, not the correctness proof, and
  is not required to demonstrate the acceptance criterion.

### 3.2 Scenario

Install `fourElasticZonesRoundRobin` (§2). Populate:

- **Zone A (spammed):** using `distinctSenders`-style address search (the
  existing brute-force-search-for-a-property idiom from `multizone_test.go`'s
  `distinctSenders`/`twoDistinctBucketAddrs`), find 10 sender addresses whose
  bucket hashes to zone 1 under the fixture table. Each enqueues one
  cross-zone `NORMAL` message (to a recipient in a different zone, e.g. zone
  2) with `GasLimit = MaxGasPerDelivery` (1,000,000) -- i.e. **10x** a single
  zone's default elastic cap of 1,000,000, a genuine flood.
- **Zone B (fair share, the zone whose throughput must be preserved):** one
  sender address whose bucket hashes to zone 2, enqueuing exactly **one**
  cross-zone message, `GasLimit = 1,000,000` -- B's normal, non-abusive load
  for the block.

All 11 messages are enqueued at the same height `H` (so all are due at
`H+1`), through the real `EnqueueMessage` path.

### 3.3 The "before" state: exercise the migration-safety fallback, not a duplicated old implementation

Rather than keeping a second, hand-maintained copy of "today's algorithm"
alive purely for comparison, the before/after contrast is obtained from the
**same shipped code**, using the fact that §5.4's migration-safety fallback
*is* today's exact algorithm (single shared counter, canonical id order,
`break` on first overrun, byte-for-byte): set `Params.MessageQuota` to its
zero value (`MessageQuotaParams{}`, which fails `Validate()`) before calling
`SetParams`-bypassing test setup (direct store write, or a params override
helper), so `DrainWith` takes the fallback branch. This both (a) gives a real
"before" measurement without maintaining dead duplicate code, and (b) is
itself a direct test of the migration-safety fallback described in §5.4 --
two obligations satisfied by one test.

**"Before" (fallback / today's behavior) assertion:** with 11 messages of
1,000,000 gas each competing for a single shared 8,000,000 budget in
canonical id order, exactly 8 of the 11 admit this block and 3 are pushed to
a later block -- and **which 3** (whether B's single message is among them)
depends entirely on where its content-addressed message id happens to sort
relative to A's ten. Use the existing brute-force-search idiom (vary B's
message `Payload` bytes, recomputing `ComputeMessageID`, until the resulting
id is provably excluded from the first 8 in sorted order for this exact
11-message set) to make the "B is starved under the old mechanism" outcome
**deterministic**, not a matter of statistical luck across CI runs. Assert
`rec.calls` does **not** contain B's message id after one `DrainWith` call at
`H+1`.

### 3.4 The "after" state: same enqueued set, populated `MessageQuota`

**Recomputed against the corrected §1.3 algorithm** (the original draft's
numbers here were computed against the pre-fix, Core-special-cased algorithm
and are no longer accurate -- see the revision note). Re-run the identical
enqueued set (a fresh keeper/store, same 11 messages, same table) with
`Params.MessageQuota = DefaultMessageQuotaParams()` (the real default: Core
reserved 4,000,000, each elastic zone capped at 1,000,000). The fixture
(§2) never routes any bucket to Core, so Core's due demand is `0` throughout
this scenario -- which means Core, exactly like the two idle elastic zones,
contributes its full own allotment to `totalSurplus`. Worked-out expected
numbers, for the test to assert exactly (these follow mechanically from the
corrected §1.3 algorithm and are safe to bake into the test as literal
expected values, not approximate bounds):

- Pass 1: `demand[zone1] = 10,000,000`, `usedOwnCap[zone1] = min(10M, 1M) =
  1,000,000`, `surplusFromZone[1] = 0`. `demand[zone2] = 1,000,000,
  usedOwnCap[zone2] = 1,000,000, surplusFromZone[2] = 0`. Zones 3 and 4 have
  no due messages: `surplusFromZone[3] = surplusFromZone[4] = 1,000,000`
  each. **Core has no due messages either** (the fixture never hashes a
  bucket to Core): `surplusFromZone[Core] = ReservedGas - 0 = 4,000,000`.
  `totalSurplus = 1,000,000 + 1,000,000 + 4,000,000 = 6,000,000`.
- Pass 2: B's one message admits immediately via its own allotment
  (`spent[2] = 0 + 1,000,000 <= 1,000,000`). **B's message is delivered at
  `H+1`, in the same block, regardless of its message id's sort position** --
  this is the headline property, and it is what the "before" case (§3.3)
  proved is *not* guaranteed under the old mechanism. This holds even in the
  worst-case ordering where all ten of A's messages sort before B's: at most
  7 of them can ever admit (below), so `totalSpent` is at most `7,000,000`
  when B is reached, and `7,000,000 + 1,000,000 = 8,000,000 <=
  TotalMessageGasPerBlock` -- B's own-allotment check is reached regardless.
  A's first (canonical-order) message admits via its own allotment
  (`spent[1] = 0 + 1,000,000 <= 1,000,000`). A's next six admit via rollover,
  draining `surplusRemaining`: `6,000,000 -> 5,000,000 -> 4,000,000 ->
  3,000,000 -> 2,000,000 -> 1,000,000 -> 0`. A's remaining three messages are
  skipped -- own allotment and surplus both exhausted -- and stay queued.
  (Which of A's ten specific messages land in the "own allotment" vs.
  "rollover" vs. "skipped" bucket depends on canonical id order, exactly as
  before; the *counts* -- 1 own-allotment, 6 rollover, 3 skipped -- do not,
  because `totalSurplus` is fixed by Pass 1 before Pass 2 ever runs.)
- **Assert:** `len(rec.calls) == 8` at `H+1` (B's 1 + A's 7); B's message id
  is in `rec.calls`; exactly 7 of zone A's ten message ids are in
  `rec.calls`; the remaining 3 are still present via `scanDueInbox` at a
  later height (nothing dropped, matching the existing
  `TestDrainBudgetStopsAndResumes` "delayed, never lost" invariant). A
  `DrainWith` at `H+2` (budget fully refreshed; zones 2/3/4 and Core all idle,
  so `totalSurplus = 1M+1M+1M+4M = 7,000,000`) admits all 3 of A's remaining
  messages in one call (`1,000,000` own allotment + `2,000,000` rollover, well
  inside the `7,000,000` available). Total: all 11 messages deliver across
  exactly **2** blocks (not 3 -- see the revision note on why this differs
  from the original draft), with zone B's message never delayed even one
  block, and with the **same total throughput this block (8 admissions) as
  the legacy mechanism delivered** -- the new mechanism does not cost
  anything here, it only fixes *which* 8 are guaranteed to include B.

### 3.5 What this test proves and does not prove

Proves: for an identical enqueued workload, the new mechanism (a) guarantees
zone B's fair-share message delivers in the same block it becomes due,
independent of zone A's flood size or the flood's message-id sort order,
while the old (fallback-equivalent) mechanism does not make that guarantee,
and (b) does so at **no throughput cost in this scenario** -- both mechanisms
admit exactly 8 of the 11 messages at `H+1`; the new one just makes B
provably one of them. Does not prove: anything about total chain capacity
(unchanged, §4a) or transaction-level admission throughput (a separate,
already-tested mechanism, §0.1) -- this test is scoped to the message bus
specifically, per the task's identified gap. It also does not exercise
Finding 2's documented, non-blocking limitation (§5.1b): this scenario has
only one zone contesting the rollover surplus at a time (zone 3, 4, and Core
are idle throughout), not two simultaneously-overloaded zones splitting the
same pool -- that is a separate, explicitly out-of-scope property, not
silently assumed away.

---

## 4. Non-goals / scope boundaries

a. **No increase in total chain capacity, and -- after the Finding-1 fix --
   no decrease in the common single-busy-zone case either.**
   `MaxBlockGas = 20,000,000` (`x/fees/types/fee_model.go:23`) is untouched.
   `TotalMessageGasPerBlock` defaults to the same `8,000,000` the bus already
   budgets today -- this design redistributes an existing budget across
   zones, it does not create new capacity. `docs/architecture/aez.md` §4.9's
   claim ("zones do not add throughput... AEZ buys isolation, fairness,
   blast-radius containment, and future migration optionality, not capacity")
   remains accurate and is not contradicted by anything here. Review 2 (point
   c) flagged, against the *original* draft, that the single-busy-zone case
   (everything else, including Core, idle) would have dropped from the full
   `8,000,000` to a hard `4,000,000` ceiling (that zone's own `1,000,000` cap
   plus `3,000,000` rollover from the three other idle elastic zones, with
   Core's floor walled off regardless of Core's actual, in this case zero,
   usage). The Finding-1 fix (§1.3/§1.4, folding Core symmetrically into the
   same rollover pool) closes this too, as a side effect: an idle Core's full
   `4,000,000` floor becomes measured surplus exactly like an idle elastic
   zone's cap, so the sole busy zone still reaches the full legacy
   `8,000,000` when nothing else is active -- verified directly against
   `TestDrainBudgetStopsAndResumes` in §3.4/§3.5 and the revision note.
b. **No intra-block transaction reordering.** This design adds no
   `PrepareProposal`, `ProcessProposal`, or custom mempool, and the wiring
   gate (`app/aetra_core_wiring.go`) still hard-panics on any routing
   execution point other than `ANTE_ADMISSION_ONLY`. CometBFT's given
   transaction order within a block is untouched by everything in this
   document. (The message-bus drain's own internal admit/skip ordering, §1.3,
   is `x/aez`'s pre-existing `BeginBlocker`-internal authority over its own
   queue, not tx-mempool ordering -- it already reorders relative to raw
   enqueue order today.) Real intra-block reordering, if ever wanted, is a
   separate, much larger architecture change requiring its own owner sign-off
   -- explicitly out of scope here.
c. **No `x/contracts` execution-concurrency change of any kind.** No
   goroutines, no `SetBlockSTMTxRunner`, no per-zone parallel execution. The
   two rejected parallel-execution design rounds
   (`docs/architecture/aez-parallel-execution-design.md`) stay paused,
   unrelated to and untouched by this document. Every mechanism above is a
   single-threaded, sequential accounting change inside the existing
   `BeginBlocker`/ante call graph.

---

## 5. Risk / adversarial self-check

Performed in the same spirit as the two adversarial rounds that caught the
parallel-execution track's blockers: actively looking for reasons this
design is wrong before treating it as final, rather than only reasoning
about why it should work.

### 5.1 Gas-limit-based reservation gaming -- deferred, matches an existing accepted tradeoff

The bus charges a message's **declared, clamped** `GasLimit`, not its actual
execution cost (`deliverMessage` is a no-op in Phase 4a, so "actual cost"
isn't even a meaningful concept yet). A sender could declare a message that
reserves a full `MaxGasPerDelivery` (1,000,000) unit of its own zone's budget
for negligible real work once a real executor lands (Phase 4b/5), denying
that slot to other genuine messages from the same zone this block.

This is the **exact** limitation the tx-admission gate already has and
already documents as accepted, not fixed: `fee_policy.go`'s own comment
(~line 222-228) states reservation is "by LIMIT, not actual... deterministic
and strictly conservative... it can only ever reject MORE, never fewer, than
an actual-based cap," with no refund mechanism. Actually fixing this would
require a reserve-then-true-up accounting pattern (meter real usage post-hoc
and credit back unused reservation within the same block) that does not
exist anywhere in this codebase today for either gate -- a materially larger
mechanism than this task's scope.

**Decision: deferred, not a blocker.** Critically, because the budget is
keyed by `SourceZone` (§1.1), this gaming is **zone-local**: a bad actor in
zone A wastes zone A's own budget and, if it also exhausts A's contribution
to `totalSurplus`, denies rollover to *other* zones only in the sense that
A's own unused capacity was already spent by A, not that A can reach into
zone B's own reserved cap. The cross-zone isolation property this task is
scoped to prove (zone B's throughput is not degraded by zone A's flood, §3)
holds regardless of whether A's flood is "real" traffic or gamed
reservations -- the per-zone partition protects B either way. This mirrors
the tx-gate's own precedent closely enough that deferring it here, rather
than re-solving it, is the consistent choice.

### 5.1b Rollover-pool fairness among multiple simultaneously-overloaded zones -- accepted, documented, non-blocking (Review 1 Finding 2)

Distinct from Finding 1 (fixed above): even after the fix, the *shared
surplus pool itself* is spent strictly FCFS in canonical `(deliver_height,
message_id)` order among however many zones are simultaneously drawing on
it. If two or more zones are **all** over their own allotment in the same
block (e.g. two elastic zones both flooding, or -- newly possible after the
Finding-1 fix -- Core and an elastic zone both over their own allotment at
once), `spent[z]` is not what partitions `surplusRemaining` between them;
whichever zone's messages sort first in canonical id order claims the shared
surplus first, and message ids are cheaply attacker-grindable (the same
property the "before" load test in §3.3 deliberately exploits as a *test
tool*, which is a direct existence proof the same grinding is available to a
real adversary, per Review 1's own observation).

**This does not reopen Finding 1.** It cannot let one zone consume
*another zone's own allotment* -- that guarantee is unconditional and holds
regardless of processing order (§1.4). It only means that when there is
genuine contention for the surplus *left over after every zone's own
allotment is honored*, that leftover is allocated FCFS-by-grind-order, not
proportionally or fairly among the contending zones. A camping zone can
grind its ids to monopolize the surplus pool at another over-capacity zone's
expense -- but never at an idle-or-merely-own-allotment-using zone's expense,
and never past the global backstop.

**Decision: accepted, not fixed, matching Review 1's own characterization.**
Fixing this would require abandoning simple FCFS-by-id-order for some
explicit proportional-split rule over the surplus pool specifically -- a
real mechanism change with its own design questions (proportional to what:
equal shares, remaining demand, original allotment?) that the task's
acceptance criterion (§3, one flooding zone vs. one idle-or-fair-share zone)
does not require answering. §3's load test does not exercise this path on
purpose (only zone A ever draws on the surplus pool in that scenario; zones
3, 4, and Core are idle throughout) -- see §3.5's "does not prove" list.

### 5.2 Interaction with the existing tx-admission `ZoneGasQuota` gate -- independent, with one bounded, non-amplifying cross-zone coupling worth naming

The two mechanisms protect different resources at different ABCI phases
(ante-time tx execution admission vs. `BeginBlock`-time message-bus drain),
keyed by potentially different snapshots of "zone" (the tx-gate resolves the
sender's *current* zone fresh at ante time of block N; a message's
`SourceZone` was fixed whenever it was originally enqueued, possibly many
blocks earlier). They do not share a counter and cannot double-charge the
same unit of gas. Ordinary compounding -- a zone that is both transaction-busy
and message-bus-busy hits both ceilings -- is intended, not a bug: it is the
whole point of protecting two independent resources along two independent
axes of the same zone's "blast radius."

One real, bounded coupling exists and should be documented rather than left
implicit: `enqueueBounce` (drain.go:200-239) stamps a produced bounce's
`SourceZone = attemptedZone` -- the zone that just *attempted and failed* a
delivery, which may be a completely different zone than whoever originally
sent the failing message. A malicious zone A could deliberately send zone B
messages engineered to fail delivery at B, forcing bounces whose `SourceZone`
is B, consuming **B's own** message-bus quota on a later block for traffic B
never chose to send. This is **not an amplification vector**: to generate N
such bounces, A must first get N of its own messages successfully drained
(charged against **A's own** quota, per §1.1) at some earlier block, and each
one produces at most one bounce (`MaxBounceDepth = 8` bounds any further
chain, and a bounce of a bounce is terminal, never re-enqueued -- I-14). The
exchange is 1:1 and A pays its own quota to impose it -- a real but strictly
bounded cross-zone coupling, not a free multiplier.

**Decision: accepted and documented, not fixed in this design.** A future
refinement could charge a bounce against the **original failing message's**
resolved destination zone's own separate "bounce reserve" rather than mixing
it into the general `SourceZone`-keyed pool -- deferred as a Phase-6b-style
follow-up, since the current coupling is bounded and does not defeat the
acceptance criterion under test (§3's scenario has no failing deliveries and
is unaffected by this).

### 5.3 Routing-table transition mid-epoch -- checked, no double-count or loss found

Investigated directly, not assumed: could a message enqueued under an old
routing table's zone assignment get double-counted or lost against a new
table's budget after `BeginBlocker`'s `MaybeActivatePendingRoutingTable`
(which runs immediately before `Drain` in the same call, `abci.go:58-63`)
swaps tables at an epoch boundary?

- `SourceZone` is stamped **once, at enqueue, and never re-resolved** --
  unlike `DestZoneAtEnqueue`/the recipient, which *is* re-resolved at
  delivery specifically because the re-resolution rule exists to route
  correctly to a recipient that moved. There is no analogous "re-resolve the
  source" step anywhere in the drain, so a routing-table swap cannot change
  which zone's budget an in-flight message is charged against -- it is
  charged against the same `SourceZone` it always would have been, decided
  once, before this design existed. No double-counting (each message has
  exactly one immutable `SourceZone`) and no loss (the message still drains
  normally; it is simply accounted under its historical zone rather than
  whatever zone its original sender's bucket resolves to under the table
  active at drain time).
- `ZoneCount` (5: Core + 4 elastic) is a Go compile-time constant
  (`x/aez/types/zone.go:22`), never a governance-mutable value -- a routing
  table can move which *bucket* maps to which zone, but it cannot introduce
  or retire a `ZoneID`. `MessageQuotaParams.Validate()` requires exactly
  `ZoneCount` entries at all times, so a message's `SourceZone`, always in
  `[0, ZoneCount)`, remains a valid index into `Quotas` no matter how long it
  sits queued across however many routing-table or `MessageQuota`
  governance updates happen in the meantime.
- A message enqueued **before** this feature ships (i.e., before `Params`
  gained a `MessageQuota` field at all) is an ordinary `ZoneMessage` with a
  perfectly valid, pre-existing `SourceZone` field -- adding a new `Params`
  field does not touch `ZoneMessage` at all, so in-flight messages queued
  across the upgrade need no special handling beyond the params-migration
  safety in §5.4.

**Conclusion: no fix needed here; this is a verified non-issue, not an
assumed one.**

### 5.4 Genesis/param-migration safety -- an existing chain must not brick

This is the one place a naive implementation genuinely could misbehave, so
it gets a concrete two-layer answer rather than a one-line assurance.

**The hazard, precisely:** `Params` is `json.Marshal`ed/`Unmarshal`ed as one
blob under `ParamsKey` (`keeper.go` `setJSON`/`getJSON`). Adding a
`MessageQuota MessageQuotaParams` field to the Go struct means that
`GetParams` on a chain that upgraded its **binary** but has not yet had
anything call `SetParams` since will successfully unmarshal old-shape bytes
into the new struct -- `json.Unmarshal` does not error on a missing field, it
silently leaves `MessageQuota` at its Go zero value
(`TotalMessageGasPerBlock: 0, Quotas: nil`). `GetParams` itself never calls
`Validate()` (only `SetParams` does) -- so this zero-value `MessageQuota`
reads back successfully, with no error, on every `BeginBlocker` call, forever,
until something fixes it. Two distinct dangers follow:

1. `DrainWith`, reading this zero-value `MessageQuota` naively, would treat
   every elastic zone's cap as `0` -- a single-attribute mechanism reading
   this as "everything is capped at zero, nothing may ever drain" would
   silently halt the entire cross-zone bus the moment any zone but Core is
   populated, on every upgraded node, with no error and no warning.
2. Any **unrelated** governance action that calls `SetParams` (e.g., raising
   `RoutingEpochLength`) would call `Params.Validate()`, which -- correctly,
   by design (§1.2, mirroring `GasQuotaParams.Validate`'s strictness) --
   rejects a zero-value `MessageQuota`. Left unaddressed, this would mean
   *every* aez governance action is blocked post-upgrade until someone
   separately fixes `MessageQuota`, an operational trap unrelated to what the
   proposal was actually trying to change.

**Layer 1 (a ready, exported fixup helper -- corrected framing per Review 1
Finding 3): not "automatic" in the sense of "runs on every upgrade without
anyone wiring it," because no such thing exists in this codebase for any
module.** The original draft called this "an automatic, one-time upgrade-
handler fixup, not a governance-gated migration," which overstated what
`app/upgrades.go`'s `SetUpgradeHandler(Name, handler)` actually gives you:
a handler under a given `Name` only ever runs when a stored `x/upgrade`
`Plan` with that matching name reaches its target height -- a Plan that must
itself be authored and applied (typically via a governance-gated
`MsgSoftwareUpgrade`). Verified directly: the one handler currently
registered (`Name = "v053-to-v054"`) is an explicit **reference
implementation** for an unrelated SDK version bump (its own doc comment says
so), and grepping the tree confirms the only production call to
`x/aez.Keeper.SetParams` today is `InitGenesisState` -- `x/aez` ships no
params-update `Msg` at all (`msg_server.go`'s own comment: "x/aez has exactly
ONE Msg," `MsgUpdateRoutingTable`, which calls `StageRoutingTable`, never
`SetParams`). So danger 2 (an unrelated governance action getting rejected)
has **no reachable trigger on the current tree** -- there is no
`MsgUpdateAezParams`-shaped handler for a zero-value `MessageQuota` to block
in the first place.

Given that, this design adds `app/upgrades.FixupAEZMessageQuota` (a
structural `AEZKeeper` interface + one function: read committed `Params`; if
`params.MessageQuota.Validate() != nil`, set
`params.MessageQuota = types.DefaultMessageQuotaParams()` and write it back)
as a **ready, exported, unit-tested helper**, following the exact convention
this codebase already established for exactly this situation:
`app/upgrades/native_account.go`'s `NativeAccountVersionUpgradePlan` /
`ValidateNativeAccountVersionUpgradeHandler` are likewise exported,
unit-tested, and **not** wired into the live `v053-to-v054` closure -- they
exist for a real future named upgrade plan to call from its own handler, at
that plan's own boundary, rather than being piggybacked onto a handler for
an unrelated upgrade. `FixupAEZMessageQuota` does the same job Layer 1 always
described, it is simply honest that "wiring it into a real upgrade" is a
step a future, actual binary-upgrade plan takes, not something this design
can truthfully claim happens automatically today.

**Layer 2 (defense in depth): `DrainWith` degrades to the legacy behavior,
never fails a block.** Independent of whether layer 1 ran (belt-and-suspenders
for an edge case such as a state-synced node that snapshots between the
binary swap and the upgrade handler's execution, if such a window can exist
for a given node's operational procedure): `DrainWith`, when `len(due) > 0`,
checks `params.MessageQuota.Validate()` itself before trusting it. On
failure, it takes a separate, explicit fallback branch --
`drainLegacyGlobalBudget` -- that reproduces **today's exact algorithm**,
byte-for-byte: a single shared counter initialized to
`LegacyGlobalMessageGasPerBlock` (the renamed, retained `8_000_000` constant,
§1.5), the unchanged canonical id order, and `break` (not skip-and-continue)
on the first message whose clamped cost exceeds the remaining shared budget.
This is a **literal reproduction** of the current shipped behavior, not "a
similarly-shaped fallback" -- which is exactly what makes it possible to reuse
as the deterministic "before" state in the load test (§3.3) rather than
requiring a second, separately-maintained implementation purely for
comparison purposes. This matches the fee_policy.go precedent explicitly
called out in the task: a nil/broken/invalid input degrades to the safe,
pre-existing default, never to a fatal error, and never to "everything is
capped at zero."

**Net result:** an operator who upgrades the binary and does nothing else
gets today's exact behavior, unchanged, indefinitely -- Layer 2 alone
guarantees this, unconditionally, with no dependency on Layer 1 ever being
invoked. Layer 1 exists so that a real future named upgrade (one that ships
its own `SetUpgradeHandler(Name, ...)`, e.g. because it also touches store
keys or module versions) can call `FixupAEZMessageQuota` as one line in its
own handler and close danger 2 for good, at that plan's boundary, the moment
such a plan exists. The per-zone-weighted behavior activates automatically,
with no separate "enable" step, the moment `MessageQuota` is valid --
consistent with how the tx-admission gate already activates the moment the
routing table splits buckets, no separate switch anywhere in this design
either.

### 5.4b The drain path's new params read punctures its own stated F-17-immunity -- acknowledged, low severity (Review 1 Finding 4)

`message.go`'s own rationale for keeping bus bounds as compile-time constants
rather than `Params` was explicit: "x/aez reads no param on the per-block
drain path... so there is no governance-raised value a restarted node could
hold stale... the F-17 class the whole module is structurally immune to."
`DrainWith` now calls `k.GetParams(ctx)` whenever `len(due) > 0`, which can
return a genuine store-level error (not just an invalid `MessageQuota`,
which is handled by the Layer-2 fallback above) -- reopening, narrowly, the
failure surface that comment named. **Accepted, not fixed:** the I-23 fast
path (nothing due -> no read at all) is preserved, so an inert/disabled chain
is unaffected; and `EnqueueMessage` (`outbox.go:42`) already calls
`GetParams` unconditionally on every enqueue today, so `DrainWith` gaining
the same failure surface is not a NEW class of risk for this module, only a
new call site of an existing one. A genuine `GetParams` store fault
propagating and failing the block is the same behavior every other
`GetParams`-calling entry point in this module already has (`GasQuotaForZone`,
`EnqueueMessage`, `UpdateRoutingTable`) -- consistent, not a new precedent.
