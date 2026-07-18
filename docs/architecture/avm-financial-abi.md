# AVM Financial Types: ABI and Serialization

This document is the Stage-4 ABI reference for the Stage-3 financial struct
types shipped in `examples/avm/finance/finance_types.atlx`: `BasisPoints`,
`Ratio256`, `Decimal256`, `Decimal128`, `SignedDecimal256`,
`SignedDecimal128`. It covers both wire encodings the AVM stack actually uses
and states precisely how each new type serializes under each.

There are **two independent, previously undocumented encodings** in this
stack. They are not interchangeable and are used at different layers.

## 1. AVM `CanonicalEncode` (runtime wire format)

Source: `x/aetravm/avm/value.go` (`CanonicalEncode` / `CanonicalDecode`,
~line 663 / ~line 814).

This is the deterministic, tag-byte-prefixed encoding the interpreter itself
uses: for map keys inside `TagMap` values (`runtimeMapNormalize`,
~line 240), for storage snapshots, and anywhere a `RuntimeValue` needs a
canonical byte representation (e.g. as a map key, or when hashing/committing
state).

Format: `byte(tag) || payload`, where the payload shape depends on the tag:

- Integers (`TagInt8`..`TagInt256`, `TagUint8`..`TagUint256`, `TagCoins`,
  `TagTimestamp`): a **fixed-width**, big-endian, two's-complement-if-signed
  byte string (1/2/4/8/16/32 bytes per width; `TagCoins` and `TagTimestamp`
  are fixed at 16 and 8 bytes respectively). Width is enforced at encode
  time by `encodeIntBytes`.
- `TagBool`: one byte, `0x00`/`0x01`.
- `TagBytes`/`TagString`/`TagAddress`: a length prefix (`encodeLengthPrefix`)
  followed by the raw bytes.
- `TagTuple`: a length prefix followed by each element's own
  `CanonicalEncode` output, in order.
- `TagMap`: a length prefix followed by `(keyBytes, valueBytes)` pairs,
  **each length-prefixed**, in ascending order of `keyBytes` (a
  `bytes.Compare` byte-lexicographic sort over each entry's own
  `CanonicalEncode`d key — see `runtimeMapNormalize`). Duplicate keys are
  collapsed to the last-written value before encoding.

### 1.1 How a value struct (Ratio256, Decimal256, ...) encodes here

Every one of the six Stage-3 types is a plain AVM *value struct*: at
runtime it is not a distinct tag, it is a `TagMap` whose entries are the
struct's own fields, keyed by **field name encoded as `TagString`**, sorted
by the sort rule above (byte-lexicographic on the encoded key, which for
plain ASCII field names is the same order as alphabetical field-name order).
This is the pre-existing, previously-audited generic mechanism
(`OpMapEmpty`/`OpMapSet`/`OpReadField` with `TagMap`, see the struct-field-
access work landed in commit `1165cf4f`) — Stage 3 did not change it, it is
simply exercised by six new field layouts:

| Type               | Fields (declaration order) | TagMap entries (sorted by key) |
|--------------------|-----------------------------|---------------------------------|
| `BasisPoints`       | `bps: uint256`               | `{"bps": TagUint256}` |
| `Ratio256`          | `num: uint256`, `den: uint256` | `{"den": TagUint256, "num": TagUint256}` (den < num alphabetically) |
| `Decimal256`        | `raw: uint256`               | `{"raw": TagUint256}` |
| `Decimal128`        | `raw: uint128`               | `{"raw": TagUint128}` |
| `SignedDecimal256`  | `raw: int256`                | `{"raw": TagInt256}` |
| `SignedDecimal128`  | `raw: int128`                | `{"raw": TagInt128}` |

Concretely, `Ratio256{num: 3, den: 7}` encodes as:

```
0x2f                                  // TagMap
<len-prefix: 2 entries>
  <len-prefix><"den" as TagString>    <len-prefix><7 as TagUint256, 32 bytes BE>
  <len-prefix><"num" as TagString>    <len-prefix><3 as TagUint256, 32 bytes BE>
```

(byte values illustrative; see `value.go` for the exact tag byte constants
and `encodeLengthPrefix`'s exact width).

Because field order in the wire form is determined by sorted key bytes, not
declaration order, `Ratio256`'s `den` field is encoded before `num` (`"den"`
< `"num"` byte-lexicographically) even though `num` is declared first in
`finance_types.atlx`. This is a property of the pre-existing map mechanism,
not something Stage 3/4 introduced.

**Scope note (unchanged from the Stage-3 audit):** a struct-typed value
arriving directly as a message or getter *argument* has no decode path
today — only the internal-construction-then-pass-by-identifier path works
(construct the struct inside the contract from scalar message/getter
arguments via a constructor function such as `ratio256(num, den)`, then use
it as a first-class value internally). Closing that is a distinct,
larger ABI extension and out of scope for Stage 4.

## 2. Compiler JSON ABI (`{name, type, value}` codec)

Source: `x/aetravm/compiler/codec.go` (`Codec.Encode`/`Codec.Decode`,
`canonicalCodecTypeName`, `zeroValueForType`, `canonicalCodecValue`,
`assignDecodedValue`).

This is the *separate*, human/tooling-facing ABI used for message
parameters, getter parameters, event fields, and storage snapshots as
consumed by the CLI, wallet, and explorer: a JSON array of
`{"name": ..., "type": ..., "value": ...}` objects, one per declared field,
in **declaration order** (not sorted — `Codec.Decode` rejects a reordered
payload with a `"name mismatch"` error). Scalar values are represented as
plain JSON (numbers, strings, bools); `bytes`/`hash`/`hash32` are
hex-encoded strings; `chunk`/`code` values are a `{hex, base64, hash,
chunks}` snapshot object.

Before Stage 4, `canonicalCodecTypeName`'s type-name recognition was a
closed, hand-maintained set (scalar/bytes/string/chunk names only). An
**unrecognized type name failed open**: `zeroValueForType`'s `default` case
silently returned a generic `map[string]any{}` (an unstructured, unvalidated
zero value) instead of rejecting, and `canonicalCodecValue`'s `default` case
fell through to a fully generic, unvalidated `structToMap`/`mapToCanonical`
encode with no check that the expected field set was present.

### 2.1 Stage-4 fix: explicit registration for the six financial types

`codec.go` now carries a closed registry, `financialStructFieldSpecs`,
mapping each of the six canonical (lowercased) type names to its exact,
ordered field list (mirroring the table in §1.1):

```go
var financialStructFieldSpecs = map[string][]financialFieldSpec{
    "basispoints":      {{Name: "bps", Type: TypeRef{Name: "uint256"}}},
    "ratio256":         {{Name: "num", Type: TypeRef{Name: "uint256"}}, {Name: "den", Type: TypeRef{Name: "uint256"}}},
    "decimal256":       {{Name: "raw", Type: TypeRef{Name: "uint256"}}},
    "decimal128":       {{Name: "raw", Type: TypeRef{Name: "uint128"}}},
    "signeddecimal256": {{Name: "raw", Type: TypeRef{Name: "int256"}}},
    "signeddecimal128": {{Name: "raw", Type: TypeRef{Name: "int128"}}},
}
```

This closes the fail-open gap **specifically for these six names**:

- `zeroValueForType` now returns the correctly shaped zero value for a
  registered name (e.g. `{"bps": 0}` for `BasisPoints`, `{"num": 0, "den":
  0}` for `Ratio256`) instead of an empty generic map.
- `canonicalCodecValue` routes a registered name through
  `canonicalFinancialStructValue`, which requires the source Go value
  (struct or map) to contain **every** registered field; a missing field is
  a hard encode error (`"encode <Type>: missing field %q"`), not a silently
  omitted key.
- `assignDecodedValue` routes a registered name through
  `assignFinancialStructValue`, which requires the incoming JSON object to
  contain **exactly** the registered field set — unmarshals into
  `map[string]json.RawMessage` and rejects if the field count differs from
  the spec (extra/unknown field) or any registered field is absent
  (missing field), before decoding a single scalar. This is a deterministic
  rejection at decode time: a malformed or wrong-shaped payload for one of
  these six types can no longer silently decode into a partially-populated
  or spuriously-defaulted value.

This registry is intentionally scoped to exactly these six names. A type
name outside the registry keeps the pre-existing generic
fail-open-to-empty-map behavior (`TestFinancialABI_UnrecognizedNameStillFallsBackGenerically`
pins this down) — broadening the registry to arbitrary/future struct types
is a separate follow-up, not part of Stage 4.

### 2.2 Worked example: `BasisPoints` under the JSON ABI

A message/getter parameter (or storage field) declared `bps: BasisPoints`
encodes as:

```json
{"name": "bps", "type": "BasisPoints", "value": {"bps": 30}}
```

Decoding `{"bps": 30, "extra": 1}` for that same field now fails with
`"decode BasisPoints: expected 1 field(s), got 2"` rather than silently
accepting or dropping the unknown `extra` key. Decoding `{}` fails with
`"decode BasisPoints: expected 1 field(s), got 0"`. Decoding a bare scalar
(e.g. `30` instead of `{"bps": 30}`) fails with `"decode BasisPoints:
expected a JSON object: ..."`.

## 3. ABI round-trip tests

`x/aetravm/compiler/financial_abi_codec_test.go` adds, for each of the six
types:

- an encode → decode → re-encode round trip asserting the two encoded byte
  strings are identical (`TestFinancialABI_<Type>_RoundTrips`);
- a `zeroValueForType` shape assertion
  (`TestFinancialABI_ZeroValueForType_UsesRegisteredShape`);
- decode-side rejection of a missing field, an extra/unknown field, and a
  non-object payload (`TestFinancialABI_DecodeRejects*`);
- encode-side rejection of a missing field on the source value
  (`TestFinancialABI_EncodeRejectsMissingField`);
- a control case confirming an unregistered type name is unaffected
  (`TestFinancialABI_UnrecognizedNameStillFallsBackGenerically`).

These exercise the JSON ABI (§2) round trip specifically — the AVM
`CanonicalEncode` runtime wire format (§1) round trip for these six types is
covered transitively by the existing struct-field-access mechanism's own
tests (unchanged by Stage 3/4); see the Stage-3 audit note on this gap for
context (no new `CanonicalEncode`-level round-trip test was added here, as
that mechanism is generic and pre-existing, not specific to these types).

## 4. Stage-3 correctness fixes folded into this pass

While preparing this document, four Stage-3 verify blockers in
`examples/avm/finance/finance_types.atlx` were fixed (all four were the same
root cause at two widths):

- `dec256ToIntegerCeil` / `dec256ToIntegerNearest` (uint256, scale `1e18`)
  and `dec128ToIntegerCeil` / `dec128ToIntegerNearest` (uint128, scale
  `1e9`) each built their rounding bias with a **same-width checked `+`
  before dividing** (`(self.raw + SCALE - 1) / SCALE`,
  `(self.raw + SCALE/2) / SCALE`). For a legitimately-constructed value
  whose `raw` sits within `SCALE` (or `SCALE/2`) of the backing type's max,
  that addition itself overflowed and trapped, even though the true
  mathematical ceil/nearest result is small and well in range.
- Fix: route Ceil/Nearest through `mulDivCeil`/`mulDivNearest`'s unbounded
  big.Int intermediate instead (`mulDivCeil(self.raw, 1, SCALE...)`,
  narrowed back via `toUint128` for the 128-bit variants) — the same
  pattern every other Ceil/Nearest routine in the file already uses, and
  the fix specifically called out as safe by the Stage-3 verify findings.

`go build ./x/aetravm/...` and `go test ./x/aetravm/... -count=1` are green
after this change (see the Stage 4 task report for exact exit codes).
