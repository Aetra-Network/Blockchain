# AVM Phase E/F call mechanism — design v1 (REJECTED), v2 (REJECTED), v3 (REJECTED)

Status: **THREE adversarial review rounds run so far (v1, v2, v3); each found a genuinely NEW, serious,
fund-loss-class bug, not a repeat of a prior one. Do not implement any version below. Pausing further
automated design iteration here — see "Status" at the end of this document — pending direct owner/architect
review of the accumulated design and open decisions.** This is the shared prerequisite
behind Phase E (real function call/return — `OpReturn` today halts the entire VM, not just a function) and
Phase F (synchronous cross-contract calls). Confirmed the single largest, most consensus-critical
remaining phase in the roadmap: every prior phase touched 5 opcode-registration sites + 4 compiler sites with no
change to `Runner`'s control-flow shape; this phase requires restructuring `Runner.Run` around an explicit call
stack, a new cross-package resolver interface, a new storage-overlay/commit-boundary abstraction, and a new
reentrancy policy.

## Why v1 is rejected — 2 blockers found by adversarial review

1. **Reentrancy opt-in reopens a lost-update bug at the storage-overlay layer, not just the contract-logic
   layer.** If a contract sets `Module.AllowReentrancy=true`, two concurrently-open frames for the SAME address
   both resolve from the same stale pre-call snapshot (the overlay only updates `CommitCallee` on a frame's
   *return*, and the outer frame is still executing, blocked on the reentrant call). Whichever frame commits
   LAST silently overwrites the other's writes wholesale (e.g. an earlier balance decrement is erased by a
   later write of an equally-stale balance) — a bug no amount of checks-effects-interactions discipline in the
   CONTRACT's own code can prevent, because the corruption happens in the VM's cross-frame commit ordering.
2. **`CallOverlay` flush order is unspecified and Go map iteration is non-deterministic by design.** If the
   outermost-success flush (or any consensus-relevant derived side effect — events, newly-created contract
   records) iterates the overlay map without a mandated canonical sort (e.g. lexicographic by address), two
   honest validator binaries executing the identical call tree can produce byte-different state/events from
   map-iteration nondeterminism alone — a textbook, historically-real consensus-fork bug class. v1 carefully
   engineered gas-attribution determinism (section 5) but was silent on this parallel determinism requirement.

Also found (non-blocking but must be fixed before implementation):
- Gas only charges for call-argument deep-copies, never for `ResolveCallee`'s full `CloneStorage` of the
  callee's ENTIRE storage blob on every `OpCallExternal` — a chain of `MaxCallDepth` calls through
  near-max-size contracts buys O(depth × MaxStorageBytes) real work for O(depth × flat-opcode-cost) gas.
- `ContractResolver`'s sketched signature (`ResolveCallee(addr, entry) -> Module, Storage, RuntimeContext, ok,
  err`) is incomplete for what the surrounding prose claims it does (value transfer + sender-identity
  rewriting need the caller's address, attached value, and call args/selector, none of which the signature
  carries).
- The F1 (intra-contract calls)/F2 (cross-contract calls) staging split may be fictional: `Runner.Run`
  references `ctx`/`module` dozens of times across many opcode cases that assume ONE flat module/ctx for the
  whole function; either F1 already has to do F2's per-frame module/ctx threading (making "F1 is the big
  change" false) or F1 punts and F2 redoes comparable work later.
- Minor: the "avm never imports x/contracts" claim is already false today (avm.go imports
  `x/contracts/types`) — the acyclicity property that actually matters (x/contracts/types doesn't import back)
  does hold, but the doc overclaimed. The opcode next-free-slot number went stale WITHIN the same review pass
  (claimed 0x5b free; actually already OpNarrowToInt256) — a live demonstration of the concurrent-collision
  risk v1's own residual-risks section warned about.

## v1's real, reusable findings (kept forward into v2)

- **Call semantics**: real opcodes (`OpCall` intra-contract, `OpCallExternal` inter-contract) backed by an
  explicit `[]*Frame` call stack in `Runner.Run`, NOT extending `tryInlineUserFunctionCall` (that inliner is an
  AST-splice mechanism that structurally cannot support cross-contract calls — there's no second AST to
  splice from).
- **Reentrancy frame-tagging**: distinguish `FrameIntra` (same contract, always allowed — required for Phase E
  helper calls to work at all) from `FrameInter` (different contract, reentrancy-checked) — but the check must
  treat the outermost frame's address as implicitly open too (v1 caught this itself mid-derivation).
- **Argument passing**: always deep-copy via the existing, already-gas-metered `RuntimeValue.clone()` — closes
  the capability-aliasing failure mode by reusing a proven mechanism, not inventing a new one.
- **Gas propagation**: one shared counter for the whole call tree; strict left-to-right source-order execution
  is already deterministic by construction since the interpreter is single-threaded and this design introduces
  no batching/parallel-call primitive.
- **Call-depth cap**: MUST be a flat frame-stack push, never a recursive Go call to `Run()` itself, or AVM call
  depth directly controls native Go stack depth (a crash vector, not just a resource-limit nuisance). Proposed
  `MaxCallDepth=32`, deliberately kept separate from `async.MessageEnvelope.MaxRecursionDepth=8` (different
  attack surface: same-transaction native-stack risk vs. cross-block mailbox-amplification risk).
- **Match-dispatch fix** (independently valuable, ship first, standalone, zero opcode changes): user
  enum/Result/Option `match` today either constant-folds or silently runs the first arm with no error when the
  scrutinee isn't compile-time-constant and no wildcard exists — a live, so-far-unexercised (no shipped example
  declares a user `enum`) but real correctness gap. Fix: give it the same real tag-compare-and-jump codegen the
  message-opcode-union match already has, abort with a distinct error when no arm matches.
- **Reference contract target**: `Adder.add()` (pure, cross-contract) + `Caller.sumViaCall/doubleTotal/
  reenterProbe()` (return-value flow, intra-contract helper, and a must-reject reentrancy probe) is enough
  surface to exercise every piece of the mechanism with minimal scope.

## v2 — fixed v1's 2 blockers with a genuinely elegant insight, but introduced a NEW one

v2's core insight for the reentrancy blocker: the AVM interpreter is single-threaded and synchronous, so at any
instant the set of open call frames forms one root-to-current-frame **path**, never a tree with siblings — any
two open frames for the same address are always ancestor/descendant, never independent concurrent writers.
Fix: a **per-address shared working set** (`addressWorkingSet{storage, openCount, undo}`), refcounted, with ALL
open frames for one address sharing the SAME mutable storage object (no second `CloneStorage`, ever) instead of
each frame getting its own private copy that later gets flushed and silently clobbers the other. This makes the
lost-update bug structurally impossible rather than merely detected-and-rejected. v2 also mandated a canonical
sorted overlay-flush order (closing blocker #2), a concrete per-byte gas charge for `ResolveCallee`'s storage
clone (closing major #3), and a two-method `ContractResolver` (`ResolveCallee` + `TransferValue`) carrying
caller address / attached value / args (closing major #4).

**v2 was REJECTED — 2 new blockers found, specific to its own new value-transfer machinery (not repeats of
v1's bugs):**

1. **`TransferValue` fires eagerly against the REAL bank keeper, outside the CallOverlay/undo bookkeeping.**
   Concrete break: A calls B (transfers 100, B returns success) → A calls C (fails, aborts the whole tx per
   the stated default policy) → `CallOverlay` is discarded wholesale (correct: no storage changes persist) —
   but the earlier `TransferValue(A,B,100)` already moved real value in the committed bank keeper and NOTHING
   reverts it. The transaction reports as failed with zero storage side effects, yet A is permanently down 100
   and B permanently up 100 — a genuine value-conservation violation. `TransferValue`'s own spec only covers
   atomicity of its OWN failure, not being undone by a LATER, unrelated abort in the same call tree. Fix
   direction: value transfer must become overlay-tracked/deferred-to-commit exactly like storage writes (only
   actually hit the bank keeper at the top-level commit, or maintain a per-address balance delta in the same
   working-set object with the same undo-log discipline as storage) — it cannot remain a separate, eager,
   unwindable-only-by-its-own-error side effect.
2. **`RuntimeContext.OriginalBalance` is set to the POST-credit balance, inverting the codebase's own
   established convention.** `x/contracts/keeper/keeper.go`'s `buildAVMContext` sets `OriginalBalance` to the
   contract's balance BEFORE the message's funds are credited, with `AttachedValue` as a separate field —
   existing contract logic computes true current balance as `originalBalance + attachedValue`. Under v2's
   callee-context construction, a callee doing that SAME arithmetic double-counts the attached value on every
   value-carrying cross-contract call. Fix: `TransferValue` should return the callee's PRE-credit balance, or
   `Runner` should capture balance before calling it and use that for `OriginalBalance`.

Also found: `Run()`'s existing `verifier.Verify(module)` call is O(code size); v2 never states whether a
resolved callee's `Module` re-verifies per call (an uncharged O(depth × MaxCodeBytes) DoS parallel to the
storage-clone one v2 was written to close) or relies on a deploy-time-only invariant — must be stated
explicitly. The callee `RuntimeContext` construction also omits `LogicalTime`/`CurrentBlockLogicalTime`/
`GasLimit`/`EmitDestination`, which existing opcodes read directly — a callee frame emitting a message would
get a degenerate `CreatedLogicalTime` or hard-error on legacy-emit.

v2's `v1ItemsStillValid` list (unchanged, carry forward to v3): real opcodes + explicit frame stack (not
inline-splicing); `FrameIntra`/`FrameInter` tagging with the outermost frame counted as implicitly open;
always-deep-copy argument passing; one shared gas counter, left-to-right deterministic order; flat-frame-stack
depth cap (never native Go recursion); the standalone match-dispatch fix; the `Adder`/`Caller` reference
contracts.

## Owner decisions still needed (grown across v1 → v2, unresolved)

- Reentrancy opt-in shape: now that v2 gives a provably-safe (not just detect-and-abort) design for the
  storage half, is a deploy-time-immutable per-contract opt-in bit still wanted, or should reentrancy simply
  always be allowed once the value-transfer blocker is also fixed?
- `TransferValue` failure semantics: should a mid-chain value-transfer failure abort just that one
  `OpCallExternal` or the whole transaction?
- `Frame` struct pre-provisioning: should F1 (intra-contract) ship the full forward-compatible `Frame` struct
  from day one so F2 doesn't redo the call/return bookkeeping, or is a minimal F1-only shape acceptable?
- Exact `MaxCallDepth` number (32 proposed, unchanged — a risk-appetite choice, no technically correct answer).
- Whether a failed `OpCallExternal` always aborts the whole transaction (still the simplest default, but now
  cheaper to revisit later since the per-address undo log is a ready-made primitive for partial-subtree
  revert if a catchable-call feature is ever wanted) vs. a catchable try-call construct.
- Whether `MaxCallDepth` should ever be unified with `async.MaxRecursionDepth=8` (recommendation: keep separate
  — different attack surfaces).
- Whether the match-dispatch fix ships as its own standalone release (behavior change: silent-first-arm becomes
  a hard abort) before any call-mechanism work.

## v3 — fixed v2's 2 blockers, corrected a stale citation, but REJECTED again: 2 more new bugs

v3's value-transfer fix: extend v2's per-address `addressWorkingSet{storage, openCount, undo}` with
`committedBalanceSnapshot`/`balanceDelta` fields and a unified, frame-tagged undo log covering both storage
writes and balance changes with one discipline. `TransferValue` becomes a pure in-memory delta update inside
the working set — it never touches the real bank keeper until the top-level commit, so a later sibling
abort now genuinely reverts an earlier successful transfer (v2's blocker 1, closed). v3 also caught and
corrected a real citation error carried unquestioned through v1 and v2: `buildAVMContext`'s actual code
(re-read directly, `x/contracts/keeper/keeper.go:1463-1489` and the internal-message path at ~1958-1993)
sets `OriginalBalance` to the balance AFTER crediting attached funds, not before — v1/v2's "established
pre-credit convention" was never real. v3's callee-context fix matches the actual (post-credit) convention
instead. v3 also determined, by direct code reading, that callee `Module` verification is a deploy-time-only
invariant (StoreCode's `Verify` gate is the only bytecode ingestion path; flat-frame-stack `Run()` is called
once per transaction, so nothing re-verifies a callee module) — no re-verification and no new Verify gas
charge needed, though a new gas charge for `ResolveCallee`'s callee-code `Decode` (distinct from `Verify`,
not skippable, not currently memoized anywhere) is still required. And it filled in the `RuntimeContext`
field table (`GasLimit` inherited byte-for-byte from the one shared top-level counter; `LogicalTime`,
`CurrentBlockLogicalTime`, `BlockHeight`/`BlockTimestamp`, `PrevStateRoot`/`BlockEntropy` all inherited
unchanged from the caller; `EmitDestination` left at zero value, matching production's existing
legacy-emit-only usage).

**v3 was REJECTED — 2 more new blockers, neither a repeat of v1's or v2's:**

1. **The flush-commit procedure for the new balance-delta walk doesn't adopt the existing code's
   scratch-copy-then-atomic-swap discipline, and `TransferValue`'s solvency check only guards the sender.**
   `TransferValue` checks `fromCurrent < amount` on the sender side but never checks whether crediting the
   recipient would overflow their real balance (the in-memory delta is unbounded `sdkmath.Int` and silently
   absorbs any value). Separately, the existing internal-message delivery path achieves safety by mutating a
   *private scratch copy* of the whole `Contracts` slice and only assigning it back to `k.genesis` in one
   shot on success — the v3 text describes the new multi-address balance flush as a live, address-by-address,
   sorted loop without clearly re-adopting that scratch-then-swap discipline. Combined: a recipient near
   `uint64` max can have its credit rejected by `checkedAdd` mid-flush, after earlier addresses in sort order
   (including the sender's debit) have already been persisted — a mix of real value destruction (debited
   sender, un-applied credit) and a torn, partially-flushed commit for every address later in sort order.
2. **`RuntimeContext.OriginalBalance`/`AttachedValue` are cached once at frame-push time and never
   refreshed.** v3's fix correctly handles a callee's FIRST read on entry (it sees the pending credit from
   the triggering transfer), but `OpReadOriginalBalance`/`OpReadAttachedValue` just read cached scalar fields
   verbatim with no dynamic recomputation. A frame that issues an outbound `TransferValue` and then reads its
   own balance again — or a caller frame resuming after a callee returns, or a frame later re-entered and
   credited again by a sibling call — sees a stale snapshot, not the live `balanceDelta`. The general property
   ("callee code can observe value it was just sent within the same transaction") only holds for the one
   narrow case explicitly designed for, not for any subsequent read. Also flagged (major, not blocking):
   `Message.GasLimit` — a field distinct from the top-level shared `ctx.GasLimit` — is read at 3 production
   sites when constructing outgoing async messages from inside a frame, and v3's field-completion table never
   addresses it.

v3's `v1v2ItemsStillValid` (unchanged, carry forward to v4): everything from v2's list, PLUS v2's per-address
shared working set for reentrancy/storage-aliasing (explicitly not rejected, only extended) and v2's
canonical sorted overlay-flush order (extended to cover balance in the same pass, not a separate one).

## Owner decisions still needed (grown across v1 → v2 → v3, unresolved)

- Reentrancy opt-in shape (unchanged from v2): now that the storage half has a provably-safe design, is a
  deploy-time-immutable per-contract opt-in bit still wanted, or should reentrancy simply always be allowed?
- **Simplified by v3**: "TransferValue failure semantics" and "does a failed `OpCallExternal` abort just that
  call or the whole transaction" collapsed into ONE decision once value transfer became fully undo-log-backed
  like any other opcode failure — pick one abort policy (whole-tx default vs. catchable try-call) covering
  both uniformly.
- **New in v3**: should the top-level commit assert a global conservation invariant (sum of all
  `balanceDelta` across every address touched in the call tree == 0) as a defensive check before flushing?
  Cheap, high-value against a future debit/credit-pairing bug — but nobody has asked for it yet.
- Frame struct pre-provisioning, exact `MaxCallDepth`, whether to unify with `async.MaxRecursionDepth`,
  whether the match-dispatch fix ships standalone first — all unchanged from v1/v2.
- **New in v3**: whether callees reached via `OpCallExternal` mid-transaction get storage-rent-charged at
  resolution time — if yes, that charge must go through the same `balanceDelta`/working-set bookkeeping, not
  `contract.Balance` directly, or it invalidates `TransferValue`'s solvency check against a stale snapshot.
- **New in v3**: adding a `checkedSub` helper (symmetric to the existing `checkedAdd`) is required scope —
  confirm this belongs in the same implementation pass.

## Status

**Not started. Do not implement.** THREE consecutive design rounds (v1, v2, v3) have now each been rejected
for a genuinely NEW, non-repeating, fund-loss-class bug — not a refinement of a prior finding. This is no
longer just "a strong signal automated review is doing real work"; three-for-three with escalating structural
subtlety (v3's bugs are about read-freshness and commit-atomicity within brand-new machinery, one layer
deeper than v1/v2's) indicates this specific piece — synchronous cross-contract value transfer under a
deferred-commit model — is genuinely hard to get right by automated design→critique alone, and is exactly the
kind of real-money-moving consensus code where a human architect should look at the accumulated design
(sections 1-4 of v3, which are otherwise well-grounded in the actual current code — re-reads of
`buildAVMContext`, `Run()`'s verify call, `StoreCode`'s gate, and the emit-opcode split were all independently
confirmed correct by adversarial review) and the 9-item owner-decision list directly, rather than an automated
v4 repeating the same loop a fourth time. Recommend pausing further automated iteration on this specific
design track pending that review; other roadmap phases are not blocked by this pause.
