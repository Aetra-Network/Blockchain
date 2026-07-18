# AVM Financial Arithmetic

This document is the single reference for how numeric/financial math works
in Aetralis contracts: the type catalog, when to use which type, how
fixed-point scale and rounding work, how overflow/underflow are handled, how
`mulDiv*` is implemented internally, how the financial types serialize, the
error-code taxonomy, and approximate gas costs. It complements two existing
documents rather than repeating them:

- `docs/architecture/avm-language-roadmap.md` — the phased language/VM
  roadmap (Phase A/B/C/... status); this doc is the deep-dive on the
  numeric/financial slice of Phase B onward.
- `docs/architecture/avm-financial-abi.md` — the Stage-4 wire-format
  reference (`CanonicalEncode` and the JSON `{name,type,value}` codec) for
  the six struct types below. Serialization here only summarizes; see that
  doc for byte-level detail.

Source of truth for the types themselves:
`examples/avm/finance/finance_types.atlx` (structs + constructors + methods)
and `examples/avm/finance/finance_stdlib.atlx` (the earlier namespace-function
convention, `bp*`/`ratio*`/`dec*`/`pnl*`, that finance_types.atlx builds on).

## 1. Why float is banned

Aetra is a replicated state machine: every validator must recompute the
exact same result from the exact same inputs, or the chain forks. IEEE-754
floating point is **not** guaranteed bit-identical across platforms/compilers
for all operations (rounding modes, fused-multiply-add availability, `x87`
vs SSE historically, differing libm transcendental implementations), and
even where it is reproducible in principle, it invites subtle non-determinism
bugs that are exceptionally hard to detect in review. The AVM therefore has
**no float type, no float opcode, and no floating-point literal** anywhere in
the language or VM. All "fractional" values are represented as integers with
an explicit, contract-defined fixed-point scale (see §4), and all math is
integer math over `big.Int` magnitudes, checked at every step (see §5).

This is one instance of a broader rule: no float, no wall-clock reads, no
`rand`, no unsorted map iteration anywhere in AVM execution — anything that
could cause two honest validators to compute different results from the same
transaction is disallowed by construction, not by convention.

## 2. The numeric type catalog

### 2.1 Plain integer primitives

`uint8`..`uint256` and `int8`..`int256` (powers-of-two widths only). These
are ordinary two's-complement/unsigned integers with **checked, trapping**
arithmetic (§5) — never silently-wrapping. Use a plain integer for:

- counts, indices, ids, block heights, nonces;
- raw token amounts already in their smallest unit (wei-style), where the
  contract never needs to reason about a fractional "1.0";
- anything that is naturally an integer and never needs a ratio, percentage,
  or accumulating fixed-point value.

Do **not** use a plain integer to represent a fee percentage, an exchange
rate, or an accumulating index/price — those are exactly what the financial
types below exist to make safe.

### 2.2 The six financial struct types

All six are real AVM value structs (not new opcodes, not new `RuntimeValue`
tags) — compiler-level newtypes over the plain integer primitives, built with
invariant-checking constructors and reduced/scaled/compared with the
`mulDiv*`/`mulCmp`/`mulDivSigned` intrinsics described in §6. Each is
constructed from **scalar** arguments only (see the ABI constraint in
`avm-financial-abi.md` §1.1 — a struct cannot yet arrive directly as a wire
argument), returns the real typed struct, and is used as a first-class value
internally from then on.

| Type | Fields | Backing width | Represents |
|---|---|---|---|
| `BasisPoints` | `bps: uint256` | unsigned 256 | A fee/slippage/commission/collateral-factor/liquidation-penalty expressed in basis points (1 bp = 0.01%), bounded `0..=MAX_BPS` (10000 = 100%). |
| `Ratio256` | `num: uint256`, `den: uint256` | unsigned 256 | An exchange rate or ratio (e.g. `reserveA/reserveB`) kept **unreduced** by default — compared via cross-multiplication (`mulCmp`), reduced only when you explicitly want to spend the gas to shrink it. |
| `Decimal256` | `raw: uint256` | unsigned 256, scale `1e18` | An accumulating unsigned fixed-point value: borrow index, funding index, compound interest, oracle price, TWAP, sqrt-price, accumulated-reward-per-share. |
| `Decimal128` | `raw: uint128` | unsigned 128, scale `1e9` | Same role as `Decimal256` for values that comfortably fit a 128-bit magnitude at 9-digit precision — cheaper to store when 256-bit range isn't needed. |
| `SignedDecimal256` | `raw: int256` | signed 256, scale `1e18` | A signed accumulating fixed-point value: perpetual PnL, funding payments, any accumulator that can go negative. |
| `SignedDecimal128` | `raw: int128` | signed 128, scale `1e9` | Same role as `SignedDecimal256` at 128-bit width. |

Naming convention (for anyone adding a seventh type later): `BasisPoints`/
`Ratio256` are mandatory, owner-specified names; every other name follows
`finance_stdlib.atlx`'s own convention (`bp*`/`ratio*`/`dec*`/`pnl*` prefixes,
`Decimal` + scale-digit precedent from `Decimal18`) extended to distinguish
by **backing width** — `Decimal128`/`Decimal256` (unsigned),
`SignedDecimal128`/`SignedDecimal256` (signed). No type is generic over its
scale (a `Decimal<N>` type parameter is deliberately out of scope); each
concrete type hard-codes its own scale.

## 3. When to use plain integers vs. a financial type

- **Plain integer**: the value is inherently a whole-number count, id, or an
  amount already in its smallest indivisible unit that the contract never
  needs to scale, compare as a ratio, or accumulate multiplicatively.
- **`BasisPoints`**: any percentage-like parameter (fee, slippage tolerance,
  liquidation penalty, collateral factor) that must be bounded and applied
  via `mulDiv`, never a bare "multiply by percent/100" that could silently
  exceed 100% or divide by zero.
- **`Ratio256`**: a rate defined as a fraction of two integers you want to
  compare or use exactly (no rounding) rather than collapse to a lossy
  decimal — e.g. an AMM's `reserveA/reserveB`.
- **`Decimal128`/`Decimal256`**: any value that **accumulates** — gets
  multiplied by itself or by another decimal repeatedly over time (an index
  that compounds, a TWAP, a sqrt-price) — where a `Ratio256` would force you
  to keep re-deriving a fraction instead of holding one fixed-point number.
  Prefer `Decimal128` when the magnitude and precision needs comfortably fit
  128 bits (cheaper to store/move); use `Decimal256` when you need 256-bit
  range or `1e18` precision to match an existing convention (e.g. matching
  `Decimal18` from `finance_stdlib.atlx`).
- **`SignedDecimal128`/`SignedDecimal256`**: the same accumulating role as
  the unsigned Decimal types, but for a value that can legitimately go
  negative (PnL, net funding paid/received).

## 4. Fixed-point scale

Every Decimal type stores its value as `raw`, an integer equal to the true
value times a fixed `SCALE`:

- `Decimal128` / `SignedDecimal128`: `SCALE128 = 1_000_000_000` (`1e9`, 9
  decimal digits) — chosen to leave ample integer-part headroom inside a
  128-bit magnitude.
- `Decimal256` / `SignedDecimal256`: `SCALE256 = 1_000_000_000_000_000_000`
  (`1e18`, 18 decimal digits) — matching the existing `Decimal18` convention
  from `finance_stdlib.atlx`.

So `raw = 1_500_000_000` for a `Decimal128` means the represented value is
`1.5`. Multiplying two Decimals of the same width divides out one extra
factor of `SCALE` via `mulDiv`/`mulDivSigned` (never plain integer `*`, which
would leave the result scaled by `SCALE^2`); converting a Decimal to a plain
integer divides by `SCALE` using one of the three explicit rounding modes
(§4.1). `BasisPoints` and `Ratio256` are **not** scaled types — a
`BasisPoints` is compared/applied directly against `MAX_BPS = 10000`, and a
`Ratio256`'s `num`/`den` are plain integers compared by cross-multiplication,
not by dividing to a fixed-point value.

No type is generic over its scale: `Decimal<N>` was explicitly rejected as
out of scope, so every concrete Decimal type hard-codes one `SCALE`
constant rather than taking it as a parameter.

### 4.1 Rounding is always explicit

There is no hidden or implicit rounding anywhere in this library. Every
operation that can lose precision — a Decimal-to-integer conversion, or a
`mulDiv`-family call — has three separately named variants, and the caller
must pick one:

- **Floor** (`mulDivFloor`, `dec*ToIntegerFloor`): truncate toward zero (or
  more precisely, always round down for unsigned/floor semantics) — plain
  integer division with no bias.
- **Ceil** (`mulDivCeil`, `dec*ToIntegerCeil`): round up unless the division
  was already exact.
- **Nearest** (`mulDivNearest`, `dec*ToIntegerNearest`): round half-up (bias
  the numerator by half the divisor before dividing).

There is deliberately no `ResultBadRoundingMode` error and no rounding-mode
enum: because every rounding mode is a **separate named function**, there is
no runtime "mode" value to construct incorrectly or trap on — an invalid
rounding mode simply cannot be expressed. Signed arithmetic
(`SignedDecimal128`/`SignedDecimal256`) is narrower in scope here: the only
signed fused multiply-divide is `mulDivSigned`, truncating toward zero (e.g.
`mulDivSigned(-7, 3, 2) = -10`, not `-11`) — there is no signed
floor/ceil/nearest triple, a deliberate scope decision (see
`finance_types.atlx`'s header comment), not an oversight.

## 5. Overflow and underflow: checked, trapping, never silent

Every arithmetic operation in the AVM — plain `+`/`-`/`*`/`/`/`%`/`shl` and
every `mulDiv*`/`mulCmp`/`mulDivSigned` intrinsic — validates its result
against the destination type's bit width (`enforceIntWidth`) before
producing a value. If the true mathematical result does not fit (an
unsigned subtraction that would go negative, an addition/multiplication that
exceeds the type's max, a left shift that overflows), execution **traps**:
the call rolls back with a specific exit code (§7), never silently wraps
modulo `2^width` and never returns a truncated result. This applies
uniformly across every width from `uint8`/`int8` up to `uint256`/`int256` —
there is no small-width exemption.

`mulDiv`/`mulDivFloor`/`mulDivCeil`/`mulDivNearest`/`mulCmp` always compute
their intermediate product at **unbounded precision** (via Go's `big.Int`,
effectively up to 512 bits for a `uint256 * uint256` product) so the
intermediate `a*b` itself never overflows — only the **final** result is
checked against `uint256`/`int256`. This is exactly why these intrinsics
exist instead of writing `a * b / c` by hand in Aetralis source: a hand-
written `a * b` would trap the moment the product alone exceeded `uint256`,
even when the final `a*b/c` result is small and perfectly representable.

Checked narrowing casts (`toUint128`, `toInt128`, `toInt256` in
`finance_types.atlx`) reuse the exact same `enforceIntWidth` check: they pop
a value of any width/signedness and re-tag it at a narrower or
differently-signed width, trapping — never wrapping — if the magnitude
doesn't fit. These exist because `mulDiv`/`mulDivRoundUp`/`mulDivNearest`/
`mulCmp` always yield `uint256` and `mulDivSigned` always yields `int256`
regardless of operand width, so storing such a result into a genuinely
`uint128`/`int128` field (as `Decimal128`/`SignedDecimal128` do) requires an
explicit narrowing step.

## 6. How `mulDiv` and friends work internally

All fused multiply-divide intrinsics share one shape: compute `a*b` as an
unbounded `big.Int` product, then divide by `c` (checked for a zero divisor),
then check the final quotient fits the destination width. They differ only
in what happens to the remainder and in signedness:

- **`mulDiv`** (alias: `mulDivFloor`) — `floor(a*b/c)`, unsigned, plain
  integer division (Go's `big.Int.Div` truncates toward zero on positive
  operands, which is floor for unsigned inputs). Opcode `OpMulDiv` (`0x51`).
- **`mulDivRoundUp`** (alias: `mulDivCeil`) — `ceil(a*b/c)`: same computation
  as `mulDiv`, plus one extra `+1` step when the division isn't exact.
  Opcode `OpMulDivRoundUp` (`0x52`).
- **`mulDivNearest`** — round-half-up: same unbounded `a*b` intermediate as
  `mulDiv`/`mulDivRoundUp`, plus one extra shift-and-compare to test whether
  the remainder, doubled, is `>= c` (round up) or not (round down). Opcode
  `OpMulDivNearest` (`0x58`).
- **`mulCmp(a,b,c,d)`** — `sign(a*b - c*d)` as `-1`/`0`/`+1`, computed as two
  unbounded products and one `big.Int.Cmp`, never actually dividing — this
  is what lets `Ratio256` compare cross-multiplied fractions over the
  **full** `uint256` range without ever risking the overflow a naive
  `mulDiv(x,y,1)`-based comparison would trap on. Opcode `OpMulCmp` (`0x56`).
  Operands are treated as unsigned.
- **`mulDivSigned(a,b,c)`** — `(a*b)/c` truncated toward zero, signed. Same
  unbounded-product-then-checked-quotient shape, over signed magnitudes.
  Opcode `OpMulDivSigned` (`0x57`).

`mulDivFloor` and `mulDivCeil` are **pure aliases**: they compile to the
identical `OpMulDiv`/`OpMulDivRoundUp` opcodes with no behavior difference
from the historically-shipped `mulDiv`/`mulDivRoundUp` spellings — both
names are supported so `finance_types.atlx` can name its floor/ceil/nearest
trio consistently, while `finance_stdlib.atlx`'s existing 9+ call sites and
the conformance tests keep working unchanged under the original names.

`isqrt(x) = floor(sqrt(x))` (opcode `OpIsqrt`, `0x53`) is a bounded run of
`big.Int` Newton iterations over a `uint256` operand; it traps (rather than
panicking) on a negative operand reachable via `OpNeg` or a signed→unsigned
mismatch.

## 7. Error codes

Every arithmetic trap used to report the single generic
`async.ResultExecutionFailed` (`3`) with only a free-text Go error string as
the differentiator. `async/types.go` defines a stable, numeric exit-code
taxonomy (wired through `exit_codes.go`'s `runtimeExitCodes` table and
`ContractExitCodeForRuntime`); the interpreter's `arithResultCode` helper
(`x/aetravm/avm/avm.go`) now classifies a subset of arithmetic errors to
their specific code instead of the generic one:

| Code | Value | Meaning | Reachable as a distinct trap today? |
|---|---|---|---|
| `ResultDivisionByZero` | 16 | Division or `mulDiv*`/`mulDivSigned` with a zero divisor. | Yes — classified by `arithResultCode`. |
| `ResultInvalidShift` | 17 | `shl`/`shr` by an out-of-range shift amount. | Yes — classified by `arithResultCode`. |
| `ResultArithmeticUnderflow` | 18 | Unsigned subtraction that would go negative. | Yes — classified by `arithResultCode`. |
| `ResultTypeCheckError` | 24 | A numeric operation received a non-numeric `RuntimeValue`. | Yes — classified by `arithResultCode`. |
| `ResultOutOfRange` | 43 | `enforceIntWidth` overflow: a checked result (or narrowing cast) doesn't fit the destination width. | Yes — classified by `arithResultCode`; this is the code a `dec128OverflowTrap`-style test observes. |
| `ResultBadDenominator` | 39 | A `Ratio256` constructed/used with `den == 0`. | Minted for taxonomy completeness, **not yet independently classified** — today this trips the same generic division-by-zero path (`ResultDivisionByZero`), because `finance_types.atlx`'s invariant checks are deliberately plain expressions over the same generic checked arithmetic every operator already traps through, not a distinguishable VM-level signal. |
| `ResultBadBasisPoints` | 40 | A `BasisPoints` constructed with `bps > MAX_BPS`. | Minted for taxonomy completeness only — same generic-arithmetic-trap caveat as above (today surfaces as `ResultOutOfRange` or `ResultArithmeticUnderflow` depending on the exact check expression, not a dedicated code). |
| `ResultPrecisionLoss` | 41 | Reserved for a future implicit-truncation trap. | Not reachable today — every Decimal-to-integer conversion in this library is an explicit, named rounding mode (Floor/Ceil/Nearest), never an implicit truncation, so there is nothing to trap for "precision loss" yet. |
| `ResultBadConversion` | 42 | Reserved for a future invalid-conversion trap. | Minted for taxonomy completeness; not independently classified today. |

In short: **only `ResultOutOfRange`** (alongside the pre-existing
`ResultDivisionByZero`/`ResultInvalidShift`/`ResultArithmeticUnderflow`/
`ResultTypeCheckError`) is a genuinely new, distinctly-classified sentinel as
of this pass. `ResultBadDenominator`/`ResultBadBasisPoints`/
`ResultPrecisionLoss`/`ResultBadConversion` exist in the taxonomy so that
future work can wire a contract-specific signal to them, but no such signal
exists yet — `finance_types.atlx`'s invariant checks intentionally reuse
generic checked arithmetic (see that file's header comment) rather than
calling a dedicated "reject" host function, so the VM has no way to tell
"this division by zero was `Ratio256`'s own denominator check" apart from an
ordinary unrelated division by zero elsewhere.

## 8. Approximate gas cost of each operation

All fused multiply-divide/compare intrinsics are **flat-cost**: their
operands are fixed-width integers (not variable-length bytes), so there is
no per-byte term, only the flat opcode cost below plus 1 gas per
"operand unit" (`GasPerOperandUnit`) for any map/tuple/byte-slice cloning
elsewhere in the same op (not applicable to these fixed-width intrinsics
themselves).

| Opcode | Gas | Notes |
|---|---|---|
| `OpAdd` | 3 | Plain checked add, any width. |
| `OpSub` | 3 | Plain checked sub, any width. |
| `OpMul` | 4 | Plain checked mul, any width. |
| `OpDiv` / `OpMod` | 5 | Plain checked div/mod, any width. |
| `OpShl` | 3 | Plain checked shift. |
| `OpMulDiv` (`mulDiv`/`mulDivFloor`) | 12 | Unbounded `a*b` then checked divide. |
| `OpMulDivRoundUp` (`mulDivRoundUp`/`mulDivCeil`) | 13 | Same as `OpMulDiv` plus the ceil bias step. |
| `OpMulDivNearest` (`mulDivNearest`) | 13 | Same as `OpMulDiv` plus one shift+compare for the round-half-up test. |
| `OpMulCmp` (`mulCmp`) | 13 | Two unbounded multiplies + one `big.Int.Cmp`; priced alongside the other fused-mul-div variants. |
| `OpMulDivSigned` (`mulDivSigned`) | 13 | Unbounded signed multiply + checked divide. |
| `OpIsqrt` (`isqrt`) | 30 | Bounded Newton iterations over a 256-bit operand — above the fused-mul-div ops (fixed-width divides), well below any curve op. |
| `OpNarrowToUint128` / `OpNarrowToInt128` / `OpNarrowToInt256` | 3 | One `enforceIntWidth` range check; priced like other flat unary casts. |
| `OpVerifySecp256k1` | 6,000 | Reference point: elliptic-curve verify is ~460x a fused mul-div — crypto is priced far above arithmetic so it can never be an under-priced DoS vector. |
| `OpEcrecover` | 8,000 | Reference point: adds a field inversion + point multiplication over `OpVerifySecp256k1`. |

Composite operations on the financial types (e.g. a `Decimal256` multiply,
which lowers to one `mulDiv` call, or a `Ratio256` comparison, which lowers
to one `mulCmp` call) cost exactly the sum of their constituent opcodes —
there is no additional struct-level overhead beyond the underlying
`TagMap`-based field reads/writes each constructor and method already
performs (`OpMapEmpty`/`OpMapSet`/`OpReadField`, priced under the generic map
opcode costs, not repeated here since they are unchanged by this work).

## 9. Serialization

See `docs/architecture/avm-financial-abi.md` for the full byte-level
reference. Summary: each of the six types is a plain AVM value struct — at
runtime a `TagMap` keyed by field name (sorted byte-lexicographically, not
declaration order) under the pre-existing `CanonicalEncode` mechanism; at the
JSON message/getter-argument ABI layer, each of the six canonical type names
is registered in `codec.go`'s `financialStructFieldSpecs`, which enforces an
exact, closed field set on encode and decode (a missing or extra field is a
hard error, not a silent default) — closing a fail-open gap that used to let
an unrecognized struct type name decode to an unvalidated empty map. A
struct-typed value still cannot arrive directly as a wire message/getter
*argument* (only scalar arguments decode off the wire); it must be
constructed inside the contract from scalar arguments via a constructor
function, then used as a first-class value internally — the correct usage
pattern, not a workaround, and unchanged by anything in this document.
