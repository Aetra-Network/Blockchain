# AVM Phase E/F call mechanism — design v1 (REJECTED, do not implement)

Status: **v1 rejected by adversarial review — 2 blockers, do not implement as written.** This is the shared
prerequisite behind Phase E (real function call/return — `OpReturn` today halts the entire VM, not just a
function) and Phase F (synchronous cross-contract calls). Confirmed the single largest, most consensus-critical
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

## Owner decisions still needed (unchanged by the rejection)

- Reentrancy: VM-enforced default-ban with a deploy-time-immutable opt-in bit, vs. a hard no-exceptions ban
  forever (the opt-in path is exactly what blocker #1 breaks — fixing #1 may change this tradeoff).
- Exact `MaxCallDepth` number (32 proposed, no technically-correct answer, a risk-appetite choice).
- Whether a failed `OpCallExternal` always aborts the whole transaction (v1's recommendation) vs. a catchable
  try-call construct.
- Whether `MaxCallDepth` should ever be unified with `async.MaxRecursionDepth=8` (v1 recommends keeping them
  separate).
- Whether the match-dispatch fix ships as its own standalone release (behavior change: silent-first-arm becomes
  a hard abort) before any call-mechanism work.

## Status

**Not started. Do not implement.** v2 design must specifically fix: (a) the reentrancy-opt-in/overlay
lost-update interaction (likely needs a per-address in-flight lock or a copy-on-conflict merge rule, not just a
tag), (b) mandated canonical (sorted) overlay flush order, (c) gas charging for full callee-storage clone cost,
(d) a complete `ContractResolver` signature, (e) an honest reassessment of whether F1/F2 is a real staging
boundary or should be one combined phase.
