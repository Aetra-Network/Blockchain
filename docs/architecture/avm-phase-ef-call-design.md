# AVM Phase E/F call mechanism — design v1 (REJECTED), v2 (REJECTED)

Status: **THREE adversarial review rounds run so far (v1, v2); each found a genuinely NEW, serious,
fund-loss-class bug, not a repeat of a prior one. Do not implement any version below. This phase needs a v3
pass, and possibly a human architect's final call, not more blind automation.** This is the shared prerequisite
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

## Status

**Not started. Do not implement.** v3 must specifically fix: (a) fold value transfer into the SAME
overlay/undo deferred-commit model as storage (no eager bank-keeper mutation that survives a later,
unrelated abort), (b) correct `OriginalBalance` to be pre-credit, matching `buildAVMContext`'s established
convention, (c) explicitly state whether callee `Module` verification is a deploy-time-only invariant or
re-runs per call (and charge for it if the latter), (d) fill in the missing `RuntimeContext` fields
(`LogicalTime`, `CurrentBlockLogicalTime`, `GasLimit`, `EmitDestination`).

Two review rounds (v1, v2) have now each found a genuinely NEW, non-repeating, fund-loss-class bug — this is a
strong signal the automated design→adversarial-review loop is doing real work, but is also approaching the
point where a human architect's final call on the "owner decisions" list (now 7 items, growing each round)
may be more productive than further automated iteration alone. Treat v3 as likely the last fully-automated
pass before this needs direct owner review of the accumulated open questions.
