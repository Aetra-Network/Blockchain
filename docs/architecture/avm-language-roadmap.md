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

## Phase C — bridge / light-client primitives (DONE)
merkle_verify (RFC-6962 domain-separated: leaf = H(0x00||data), internal node = H(0x01||left||right), so
an internal node can never be replayed as a leaf), parameterized by hash algo (sha256/keccak256); batched
secp256k1 verification over a validator set with distinct-signer dedup and a strict >2/3 threshold.
Reference: `examples/avm/bridge/bridge_verify.atlx` — a light-client accept path requiring BOTH signature
quorum AND merkle inclusion against a header's stateRoot, proven via known-answer + adversarial (forged-leaf,
tampered-proof, duplicate-signer) conformance vectors.

## Phase D — advanced crypto (ZK / pairing)
BLS12-381, BN254, pairing_check, Poseidon hash, KZG verification, Groth16/PLONK verify intrinsics.
Reference: a Groth16 verifier contract. (Largest phase; gas cost per op explicit.)

## Phase E — language surface
Exhaustive match/pattern, full generics, tuples, fixed + dynamic arrays, enums, traits, and Move-style
RESOURCE ABILITIES (copy/drop/store) so tokens/NFTs can't be duplicated at the type level. Early return,
structured error propagation. (Recursion deliberately bounded.)

## Phase F — composability + safety
Synchronous contract-to-contract calls with return values, atomic rollback/revert/abort, a transactional
journal, call-depth + reentrancy guards, a shared gas meter across the call tree, read-only calls, batched
calls, and a capability model (a MintCapability object gates minting, not an address compare). Async
cross-zone stays the AEZ message bus (exactly-once, bounce, timeout) -- never promise atomicity across
independent zones.

## Phase G — storage + lifecycle
Nested maps (Map<Address, Map<TokenId, u256>>), ordered maps / trees / heaps for order books, bounded
pagination over large collections, bitmaps; deterministic-address factories (CREATE2-style), minimal-proxy
clones, initial-state passing; upgrade models (immutable / upgrade-authority / proxy / code-hash swap with
kept storage) + state migration + timelock + permanent upgrade lock.

## Phase H — VM resource accounting + hard limits
Gas = CPU + memory + storageRead + storageWrite + stateGrowth + delete + crypto + serialization +
contractCall + contractCreate + inputSize. Separate hard caps beyond gas: tx size, code size, memory,
stack depth, call depth, event count, return size, touched storage keys, max state growth.

## Reference-contract acceptance suite (the real bar)
perpetual DEX · lending w/ liquidations · trustless bridge/light client · ZK (Groth16) verifier ·
concentrated-liquidity AMM · on-chain order book · account-abstraction wallet · upgradeable DAO ·
on-chain game · sealed-bid/Dutch/batch auction. A phase is "done" when its reference contracts compile
and execute through the real VM under gas.

## Non-negotiable invariants (every phase)
Determinism across validators (canonical pure-Go libs, no float, sorted iteration, committed-state only);
crypto/math as VM intrinsics with explicit per-op + per-byte gas (AVM gas is separate from SDK gas, so an
uncharged op is invisible DoS); float BANNED; every intrinsic has known cost and hard bounds.
