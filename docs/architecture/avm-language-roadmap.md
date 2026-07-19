# AVM / Aetralis language roadmap — toward serious contracts

Owner target: the language + VM must COMPILE and FULLY EXECUTE the hardest contract classes on-chain --
perpetual/derivatives DEX, lending with liquidations, trustless bridge + on-chain light client,
ZK-verifier, concentrated-liquidity AMM, on-chain order book, account abstraction, upgradeable DAO,
complex on-chain games, and advanced auctions. Determinism is absolute (no float, no wall-clock, no
network/fs/threads); crypto and heavy math are VM intrinsics with explicit gas, not hand-rolled bytecode.

Worked in adversarially-verified phases; each phase ships reference contracts proven to compile AND
execute through the real VM, not just parse.

## Phase A — byte-exact crypto + safe mulDiv (DONE)
sha256/keccak256/ripemd160/sha512/blake2b opcodes (distinct from the chunk-tree hash()); byte ops
concat/slice/byteAt/toBytesBE/fromBytesBE; mulDiv(u256); secp256k1 verify + ecrecover; ed25519 -> ZIP-215
(ed25519consensus). Gas per input byte. Reference: PoW miner, sig wallet (ed25519+secp256k1), DEX on u256.

## Phase B — financial math (unblocks lending / perpetual / AMM)
- VM intrinsics: fullMul (512-bit product), mulDivFloor / mulDivCeil (a*b/denominator at 512-bit internal
  precision even without a public u512 type), isqrt, pow/exp, bitwise (and/or/xor/shl/shr), bitmap ops.
- stdlib types (compiler newtypes over uintN + safe intrinsics, NOT new opcodes): BasisPoints (fees,
  slippage, commission, collateral factor, liquidation penalty), Ratio256 (exchange rate reserveA/reserveB
  -- compare without reducing; reduce only to save gas), Decimal128/Decimal256 (UFixed128<18>/UFixed256<18>
  for ACCUMULATING values: borrow index, funding index, compound interest, oracle price, TWAP, sqrtPrice,
  accumulated-reward-per-share -- Ratio alone is wrong for these).
- Reference: lending health-factor math, perpetual PnL (signed, EntryPrice/ExitPrice), CL-AMM tick math.

### Phase B status (finance numeric library — examples/avm/finance/)
The NUMERIC PRIMITIVES are implemented and execute through the real VM under gas, proven by
`x/aetravm/conformance/finance_acceptance_test.go`: BasisPoints (scale 10000), Ratio256 (unreduced
num/den compared by cross-multiply), Decimal18 (UFixed<18>: mul/div/fromInt/toInt/decSqrt), and signed
PnL (int256 subtract/multiply/add + `< 0` and `<` compare). They ship as free `@pure` namespaces on a
host contract plus three demonstration contracts — a same-numeraire lending health factor, a perpetual
mark-PnL + maintenance-margin liquidation check, and a sqrt-price / geometric-mean price helper.

These are DEMONSTRATIONS of the primitives, not full protocols. The FULL reference contracts named above
(production CL-AMM with per-tick liquidity, a perpetual with periodic funding, lending with a price oracle
and real liquidations) remain BLOCKED on language upgrades surfaced by the AVM v1 capability probe:
- (a) struct field access on VALUE types -- STILL OPEN, and more specific than first scoped: a LOCAL
  variable of struct type can already read/write a field today (a struct literal lowers to a runtime map,
  and OpReadField has a map-keyed-field branch), but a struct-typed FUNCTION PARAMETER cannot -- parameters
  are always decoded as scalar message-body fields, and the field-access path has no case for a parameter
  binding. This is the one gap left before Ratio256/BasisPoints/Decimal can be real, invariant-enforcing
  structs instead of raw-scalar naming conventions (a shared library function taking one as an argument
  needs exactly the blocked case).
- (b) DONE -- `mulCmp` (opcode 0x56, full-range sign(a*b-c*d) at unbounded width) shipped; `ratioCmp` /
  `ratioGtFull` in the finance stdlib now compare over the FULL uint256 range, not just below 2^128.
- (c) DONE -- `mulDivSigned` (opcode 0x57, signed (a*b)/c truncated toward zero) shipped; `decMulSigned` /
  `pnlScale` use it for signed scaled math.

(a) is the one remaining Phase E / follow-up item; until it lands, the finance library stays a
primitive-level demonstration, honestly labelled as such in each contract header.

Struct field access has since landed (commit `1165cf4f`) and the full financial struct library
(BasisPoints/Ratio256/Decimal128/Decimal256/SignedDecimal128/SignedDecimal256) has shipped in
`examples/avm/finance/finance_types.atlx`. See `docs/architecture/avm-financial-arithmetic.md` for the
complete numeric-type catalog, fixed-point scale/rounding rules, overflow handling, `mulDiv*` internals,
error codes, and gas costs, and `docs/architecture/avm-financial-abi.md` for the wire-format reference.

## Phase C — bridge / light-client primitives (DONE)
merkle_verify (RFC-6962 domain-separated: leaf = H(0x00||data), internal node = H(0x01||left||right), so
an internal node can never be replayed as a leaf), parameterized by hash algo (sha256/keccak256); batched
secp256k1 verification over a validator set with distinct-signer dedup and a strict >2/3 threshold.
Reference: `examples/avm/bridge/bridge_verify.atlx` — a light-client accept path requiring BOTH signature
quorum AND merkle inclusion against a header's stateRoot, proven via known-answer + adversarial (forged-leaf,
tampered-proof, duplicate-signer) conformance vectors.

## Phase D — advanced crypto (ZK / pairing) (DONE, scoped)
BN254 primitives (`OpBn254G1Add`/`OpBn254G1ScalarMul`/`OpBn254G1IsOnCurve` = 0x5c-0x5e,
`OpBn254G2Add`/`OpBn254G2ScalarMul`/`OpBn254PairingCheck`/`OpPoseidon2Bn254` = 0x5f-0x62), matching compiler
builtins, a Groth16-over-BN254 `.atlx` stdlib, and a real Groth16 verifier reference contract. Built on
`github.com/consensys/gnark-crypto` (added as a normal go.mod dependency, INTERIM `-tags purego,noadx`
alternative rather than full vendoring — see design doc's Status/Implementation-status sections for the
accepted amd64-vs-arm64 cross-architecture tradeoff this leaves open, and every build entrypoint that must
carry the flag). Proven with a real Groth16 differential golden vector (two proofs generated offline via
gnark's own R1CS+prover, never added to this repo's go.mod) plus an adversarial soft-fail matrix
(bit-flipped proof, out-of-subgroup G2 point, length/count mismatches).
NOT done, honestly scoped out: BLS12-381 (v1's explicit scope call — BN254 only), KZG verification, PLONK
verify, and full vendoring-and-stripping of gnark-crypto's `fptower` (the stronger hardening pass that would
close the cross-architecture gap the INTERIM build-tag approach leaves open).
Reference: `docs/architecture/avm-phase-d-zk-design.md` (design v1-v3 + Status + Stages 1-4 implementation
log); `examples/avm/zk/groth16_stdlib.atlx` (verifier library) and `examples/avm/zk/groth16_verifier.atlx`
(reference contract).

## Phase E — language surface
Exhaustive match/pattern, full generics, tuples, fixed + dynamic arrays, enums, traits, and Move-style
RESOURCE ABILITIES (copy/drop/store) so tokens/NFTs can't be duplicated at the type level. Early return,
structured error propagation. (Recursion deliberately bounded.)

### Phase E status
DONE: struct field access on locals AND function/method parameters (commit `1165cf4f`); nested (3+
segment) struct field READ chains of arbitrary depth (`o.inner.z`, `st.outer.z`, ...) through local
bindings, storage/`state` aliases, and struct-typed parameters (commit `b1d4555a`) -- `lowerExprToIR`'s
`ExprPath` case now chains `IRExprField`/`OpReadField` per segment instead of erroring past depth 2,
verified end-to-end through the real AVM runner at 3- and 4-segment depth
(`struct_field_access_test.go`); the same commit found and closed the adjacent silent-corruption bug the
read-side fix made newly reachable -- a 3+ segment `set` target (`set st.outer.b = st.spare`) previously
compiled clean and silently overwrote the WHOLE `outer` struct with `spare`'s value instead of just field
`b`; now rejected explicitly at compile time (`E_SET_NESTED_UNSUPPORTED`) rather than corrupting state
(genuine write support -- actually lowering to a nested-field update -- is still not implemented, see
below); hard-abort, real tag-compare-and-jump match codegen for the message-opcode-union match path
(`match(msg)` handlers); Move-style RESOURCE ABILITIES as a compiler-only, intra-function-scoped static
linear-use check (`@resource` struct annotation + `CheckResourceAbilities`, commit `52d02d47`), now WIRED
into `Compiler.Compile()`'s automatic pipeline (commit `b1d4555a`, called from `typecheck()` as the last
check before codegen, no longer opt-in; still dormant/zero-cost in practice since no shipped example
declares `@resource` yet).

**DONE — the call-mechanism prerequisite shipped** (v5 design, see
`docs/architecture/avm-call-mechanism-v5-design.md`): real intra-contract CALL/RET (`OpCall`/`OpRet`, a
genuine VM call stack replacing `tryInlineUserFunctionCall`'s single-return-expression AST-splice,
`MaxCallDepth=32` runtime-enforced against adversarial raw bytecode) now compiles any non-trivial
(branching/looping/multi-statement) function body, which for the first time unblocks early return /
structured error propagation for such bodies, and tuples/multi-value returns (the value representation
and wire/ABI encoding already existed; destructuring syntax, tuple literals, and positional field access
are new). Proven via a refactored reference contract -- `bridge_verify.atlx`'s three duplicated
Merkle-walk-loop copies and two duplicated quorum-loop copies (duplicated specifically because the old
inliner could only splice a single return expression, never a loop) collapsed into two real shared
functions (`merkleWalk`, `verifyQuorum`), with the pre-existing, unmodified `TestAcceptanceBridgeVerify`
still passing byte-for-byte -- plus a new example (`batch_stats.atlx`: a 4-tuple-returning function with
an early-return guard and a mutating loop). Tuples are call/return/local-position only by deliberate scope
decision (never a `@storage`/`@message`/`@event` field type -- the wire codec's bare-JSON-array tuple
shape doesn't round-trip through the generic field-access-off-raw-bytes path used for message/storage
fields).

STILL NOT supported: field access through a MAP-FETCHED struct value (e.g. `m.get(k)!.field`), for either
read or write -- rejected at parse time (`unexpected expression token "."`; `parsePrimary` has no postfix
field-access loop after a call/`!`-unwrap), so it never reaches the lowering pass above. This is an
independent parser gap, not a consequence of the call mechanism -- the v5 pass did not attempt it, and it
remains open follow-up work, not inherited blocked status from anything below.

Genuine nested-field WRITE support (the compiler actually lowering `set st.outer.b = ...` to a targeted
field update, instead of refusing to compile it) is likewise still not implemented -- only the
explicit-rejection half of that gap has landed (see DONE above). A real linked-list order book, for
example, still needs a new-struct-literal-and-whole-field-reassignment workaround.

OPEN, real gap (not cosmetic): the match path for user-declared enums/`Option`/`Result`/structs (i.e. every
match EXCEPT the message-opcode-union one) still only handles its scrutinee via compile-time constant
folding; when that fails for a genuine runtime value, it silently falls back to the wildcard arm if present,
or otherwise **silently executes the first arm regardless of the actual runtime tag**. Latent today (no
shipped example declares a user enum), but a real, fund-loss-class correctness gap the moment a contract
uses non-trivial enum/Option/Result matching. Needs the same tag-compare-and-jump codegen the message-match
path already has.

Generics and traits (v5 design §4/§5): scoped down deliberately, and STILL NOT BUILT this pass (design-only
so far, confirmed against the shipped code, not merely unmentioned):
- Full generics via compile-time monomorphization with **explicit type arguments only**
  (`max<uint64>(x, y)`, no inference engine) was designed but not implemented -- zero type-parameter-list
  grammar exists anywhere in `parser.go`/`types.go` today.
- Static trait dispatch (a `trait` as a compile-time signature contract, direct dispatch on a concrete
  non-generic receiver resolved before the whole-program recursion checker runs) was designed but not
  implemented -- `trait Demo {}` is still rejected by a pinned regression test (`surface_test.go:422-427`).
- Dynamic trait dispatch (trait-typed values, vtables) is deferred **permanently**, not merely delayed --
  it needs an indirect call target, which is in direct, structural tension with the call mechanism's
  compile-time-immediate `OpCall` target invariant that the rest of that design's safety argument depends
  on. Trait dispatch on an in-scope generic type parameter (as opposed to a concrete receiver) is deferred
  for a related but distinct reason: its concrete callee is only known post-monomorphization, after the
  recursion checker has already run on the un-substituted declarations, so allowing it today would let a
  dispatch cycle bypass the acyclicity guarantee the call mechanism's own safety argument relies on.

No longer BLOCKED on a paused call-mechanism design: `docs/architecture/avm-phase-ef-call-design.md`'s
four-times-rejected v1-v4 synchronous-cross-contract-call track is superseded by the v5 design
(`docs/architecture/avm-call-mechanism-v5-design.md`), which re-scoped to an intra-contract-only call
mechanism and shipped it (see DONE above). Full generics, trait dispatch beyond a concrete-receiver static
case, and map-fetched-struct field access remain genuinely not-done follow-up work with their own reasons
(stated above), not inherited "blocked pending Phase F" status.

## Phase F — composability + safety
Synchronous contract-to-contract calls with return values, atomic rollback/revert/abort, a transactional
journal, call-depth + reentrancy guards, a shared gas meter across the call tree, read-only calls, batched
calls, and a capability model (a MintCapability object gates minting, not an address compare).

### Phase F status
Synchronous cross-contract MUTATION with atomic rollback (the original scope above) is **not pursued, and
will not be pursued** -- this is the v5 design's own considered architectural position (§7), not merely an
inherited constraint. Four independent design rounds (v1-v4, `docs/architecture/avm-phase-ef-call-design.md`)
each fixed the prior round's fund-loss bug and found a new one, with no convergence in severity -- v4's
finding (silent destruction of an ordinary counterparty's pre-existing balance on the single most common
real-world code path) was worse than v1's. Separately, AEZ's entire future value proposition is zone
isolation, and a synchronous atomic call across a future zone boundary is fundamentally incompatible with
that isolation model, so even a version made fully safe for today's single-zone reality would become a
standing liability the day zones actually split. Cross-contract mutation stays on the existing async
message bus (`x/aez/keeper/outbox.go`/`inbox.go`/`drain.go` -- exactly-once delivery, bounded queue depth,
bounce-on-failure, deadline-based timeout, already shipped and safe), permanently, by design.

Read-only cross-contract calls (a contract synchronously calling a `@get` getter on another
already-deployed contract, reading its current committed storage, with no write path and therefore no
atomicity/rollback problem at all) were designed in full (v5 §6: a keeper-side resolver callback preserving
`avm.go`'s existing acyclicity, gas metering charged across the module boundary, a separate, small
`MaxExternalGetDepth` bound since this is the one place real Go-level recursion is used) but **not
implemented this pass** -- confirmed directly against the shipped code: zero `OpCallExternalGet` opcode in
`avm.go`, zero new grammar, zero `RuntimeContext.ExternalGetResolver` field. Left as real, designed-but-
unbuilt follow-up work; the design itself is considered sound (a read cannot corrupt anything), just not
yet built or tested.

## Phase G — storage + lifecycle (DONE, scoped)
Nested maps (Map<Address, Map<TokenId, u256>>), ordered maps / trees / heaps for order books, bounded
pagination over large collections, bitmaps; deterministic-address factories (CREATE2-style), minimal-proxy
clones, initial-state passing; upgrade models (immutable / upgrade-authority / proxy / code-hash swap with
kept storage) + state migration + timelock + permanent upgrade lock.

Shipped (commits `2140d2b1`, `b5d1cddc`): nested maps already worked pre-Phase-G (see `multi_id_ledger.atlx`);
this phase added `examples/avm/orderbook/order_book.atlx` (price-bucketed order book: fixed-tier Map book
sides + fixed-capacity per-level FIFO slot arrays, since AVM v1 has no native ordered-map/heap and no
map-fetched-struct field WRITE, and nested (3+ segment) struct field WRITE is still explicitly rejected
rather than supported (see Phase E status) — a real linked-list order book is not expressible until those
land); `examples/avm/collections/{pagination_stdlib,bitmap_stdlib}.atlx` (page-sharded
bounded pagination, word-packed bitmap); and `MsgScheduleContractUpgrade`/`MsgApplyScheduledUpgrade`
(governed `MinUpgradeDelay` timelock + the pre-existing permanent upgrade lock, both keyed off the REAL
chain block height at the gRPC layer, not a caller-supplied `Height` field — see
`x/contracts/keeper/grpc_server.go`'s `blockHeight()`).

Two honest, NOT-closed gaps, documented rather than hidden: (1) `order_book.atlx`'s Map operations still bill
against FINDING-001's per-Map-total-size model (10-28x measured cost variance vs. book depth, despite bounded
step counts) — closing it needs per-tier field sharding, deferred as a larger rewrite of a reference example;
(2) deterministic-address factories / minimal-proxy clones were not attempted this phase.

## Phase H — VM resource accounting + hard limits (DONE, scoped)
Gas = CPU + memory + storageRead + storageWrite + stateGrowth + delete + crypto + serialization +
contractCall + contractCreate + inputSize. Separate hard caps beyond gas: tx size, code size, memory,
stack depth, call depth, event count, return size, touched storage keys, max state growth.

Shipped (commit `2255e365`): `RequireStorageCloneGasFloor` (a minimum gas floor scaled to destination storage
size, so a message with an unrealistically low `GasLimit` can't force an expensive clone-and-discard for
near-zero cost), `enforceAVMExecutionCaps` (`MaxEventsPerExecution`, `MaxChangedStorageKeysPerExecution`),
and `requireStateGrowthWithinCap` (`MaxStateGrowthBytesPerExecution`) — see
`x/contracts/keeper/avm_execution_caps.go`. Also fixed a real, separate storage-layer gas bug found later in
the same area (commit `b5d1cddc`): `.save()` as a bare statement previously billed gas proportional to a
contract's ENTIRE storage footprint on every call (a provable no-op, since AVM v1's only storage-write path
is the immediate per-field assignment that already persists by the time `.save()` runs) — now compiles away
entirely. Tx size / code size / memory / stack depth / call depth caps beyond the above were not addressed
this phase.

## Reference-contract acceptance suite (the real bar)
perpetual DEX · lending w/ liquidations · trustless bridge/light client · ZK (Groth16) verifier ·
concentrated-liquidity AMM · on-chain order book · account-abstraction wallet · upgradeable DAO ·
on-chain game · sealed-bid/Dutch/batch auction. A phase is "done" when its reference contracts compile
and execute through the real VM under gas.

## Non-negotiable invariants (every phase)
Determinism across validators (canonical pure-Go libs, no float, sorted iteration, committed-state only);
crypto/math as VM intrinsics with explicit per-op + per-byte gas (AVM gas is separate from SDK gas, so an
uncharged op is invisible DoS); float BANNED; every intrinsic has known cost and hard bounds.
