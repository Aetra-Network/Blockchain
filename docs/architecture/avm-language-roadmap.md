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
bindings, storage/`state` aliases, and struct-typed parameters -- `lowerExprToIR`'s `ExprPath` case now
chains `IRExprField`/`OpReadField` per segment instead of erroring past depth 2, verified end-to-end
through the real AVM runner at 3- and 4-segment depth (`struct_field_access_test.go`); hard-abort,
real tag-compare-and-jump match codegen for the message-opcode-union match path (`match(msg)` handlers);
Move-style RESOURCE ABILITIES as a compiler-only, intra-function-scoped static linear-use check (`@resource`
struct annotation + `CheckResourceAbilities`, commit `52d02d47`) -- now WIRED into `Compiler.Compile()`'s
automatic pipeline (called from `typecheck()` as the last check before codegen, no longer opt-in; still
dormant/zero-cost in practice since no shipped example declares `@resource` yet).

STILL NOT supported: field access through a MAP-FETCHED struct value (e.g. `m.get(k)!.field`), for either
read or write -- rejected at parse time (`unexpected expression token "."`; `parsePrimary` has no postfix
field-access loop after a call/`!`-unwrap), so it never reaches the lowering pass above. This stays blocked
on the paused Phase F call-mechanism prerequisite below.

Separately, a pre-existing gap the read-side fix makes more reachable rather than causes: nested-field
WRITE (`set st.outer.b = ...` for a 3+-segment path) silently corrupts state today, with no compiler
diagnostic and no runtime failure. `validateStatement`/`lowerStatementsToIR`'s `StatementSet` handling only
ever inspects `stmt.Path[1]`, regardless of `len(stmt.Path)`; for a 3-segment target it typechecks the RHS
against the *container* field's type (`outer`, not `b`) and lowering then overwrites the whole container
with the RHS, discarding the trailing segment. Not yet fixed -- `set` should reject `len(Path) >= 3` with a
clear "not supported" error (mirroring the read side's pre-fix behavior) until write-side nested-field
support is implemented.

OPEN, real gap (not cosmetic): the match path for user-declared enums/`Option`/`Result`/structs (i.e. every
match EXCEPT the message-opcode-union one) still only handles its scrutinee via compile-time constant
folding; when that fails for a genuine runtime value, it silently falls back to the wildcard arm if present,
or otherwise **silently executes the first arm regardless of the actual runtime tag**. Latent today (no
shipped example declares a user enum), but a real, fund-loss-class correctness gap the moment a contract
uses non-trivial enum/Option/Result matching. Needs the same tag-compare-and-jump codegen the message-match
path already has.

BLOCKED on the paused Phase F call-mechanism prerequisite (see `docs/architecture/avm-phase-ef-call-design.md`
— four rounds rejected, no convergence, paused pending direct owner design): full generics, tuples/multi-value
returns (no tuple value representation, wire/ABI encoding, or destructuring syntax exists at all), traits with
dynamic dispatch, early return / structured error propagation for non-trivial (branching, looping,
multi-statement) function bodies. `tryInlineUserFunctionCall`, the only current intra-contract "call"
mechanism, is an AST-splice supporting strictly one lazy-storage-binding-then-return shape and structurally
cannot support any of these — they are not independent Phase E work, they inherit Phase F's blocked status.

## Phase F — composability + safety
Synchronous contract-to-contract calls with return values, atomic rollback/revert/abort, a transactional
journal, call-depth + reentrancy guards, a shared gas meter across the call tree, read-only calls, batched
calls, and a capability model (a MintCapability object gates minting, not an address compare). Async
cross-zone stays the AEZ message bus (exactly-once, bounce, timeout) -- never promise atomicity across
independent zones.

## Phase G — storage + lifecycle (DONE, scoped)
Nested maps (Map<Address, Map<TokenId, u256>>), ordered maps / trees / heaps for order books, bounded
pagination over large collections, bitmaps; deterministic-address factories (CREATE2-style), minimal-proxy
clones, initial-state passing; upgrade models (immutable / upgrade-authority / proxy / code-hash swap with
kept storage) + state migration + timelock + permanent upgrade lock.

Shipped (commits `2140d2b1`, `b5d1cddc`): nested maps already worked pre-Phase-G (see `multi_id_ledger.atlx`);
this phase added `examples/avm/orderbook/order_book.atlx` (price-bucketed order book: fixed-tier Map book
sides + fixed-capacity per-level FIFO slot arrays, since AVM v1 has no native ordered-map/heap and no
map-fetched-struct field WRITE — a real linked-list order book is not expressible until Phase F's call
mechanism unblocks that); `examples/avm/collections/{pagination_stdlib,bitmap_stdlib}.atlx` (page-sharded
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
