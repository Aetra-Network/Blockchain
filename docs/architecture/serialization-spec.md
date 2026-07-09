# AVM Serialization Specification

This document defines the canonical serialization rules used by AVM language
types, ABI descriptors, message bodies, and storage commitments.

## 1. Canonical Principles

- Serialization MUST be deterministic.
- The same value MUST always serialize to the same byte sequence.
- Decoders MUST reject non-canonical encodings.
- Field order is fixed and part of the schema.
- Unknown fields are not allowed in canonical network payloads.
- The schema, type identity, and encoding rules are coupled; any ambiguity in
  type shape, field order, nesting, or width is invalid.

## 2. Scalar Types

Fixed-width scalars use big-endian encoding:

- `bool` -> 1 byte, `0x00` or `0x01`
- `u8` -> 1 byte
- `u16` -> 2 bytes
- `u32` -> 4 bytes
- `u64` -> 8 bytes
- signed integers -> two's-complement big-endian of the declared width
- `i8`, `i16`, `i32`, `i64`, `i128`, and `i256` MUST use the declared width
  exactly; no shorter or alternative encodings are allowed

Addresses, hashes, and identifiers are fixed-length byte arrays and MUST be
encoded as raw bytes without reformatting.

## 3. Strings And Bytes

- `string` is UTF-8 bytes with a `u32` length prefix.
- `bytes` is raw bytes with a `u32` length prefix.
- Empty values are encoded as length `0`.
- Length prefixes MUST match the actual payload length exactly.
- Canonical decoding MUST reject any alternative byte prefix or overlong form.

## 4. Structs

Struct fields are serialized in declaration order.

Rules:

- every declared field participates in the serialization;
- optional fields are encoded with an explicit presence marker followed by the
  value if present;
- nested structs are serialized inline using their own canonical rules;
- no field may depend on reflection order or language runtime map iteration.
- `Option<T>` MUST encode as a stable tagged presence/value pair.
- `Result<T, E>` MUST encode as a stable tagged success/error pair.
- changing field order, field type, or optionality is a breaking change
  because it changes the canonical bytes and all hash commitments derived from
  them;
- decoders MUST reject payloads whose struct field order, field count, or field
  names do not match the declared schema exactly.

## 5. Enums

Enums are serialized as:

1. variant tag;
2. variant payload, if any.

Variant tags are canonical and stable. Reordering variants changes the enum
encoding and MUST change the ABI hash.
- enums are closed tagged unions;
- decoders MUST reject unknown tags and non-canonical variant payload shapes;
- exhaustive match on enums is required unless the source declares an explicit
  wildcard arm.

## 6. Lists And Maps

- Lists are serialized as `u32` length followed by each element in order.
- an empty list has exactly one canonical encoding;
- Maps MUST NOT rely on native map iteration.
- Canonical map encoding is a sorted list of key/value entries.
- Keys are sorted by their canonical encoded bytes.
- Duplicate keys are invalid.
- lists and maps are bounded collection types; decoders MUST reject payloads
  that exceed the declared bounds.
- lists preserve declaration order or source order, not runtime sort order;
- map layout is bounded, deterministic, and versioned;
- decoders MUST reject non-canonical list lengths, unordered map entries, and
  trailing bytes.

## 7. Large Payloads

Payloads larger than the inline limit MUST be represented through Chunk trees.

Canonical chunk rules:

- `Chunk` is the public base payload unit.
- `Segment` is a bounded read-only view over a `Chunk`.
- `ChunkCursor` is a read-only navigation type for walking chunk-backed data
  without committing mutable state.
- `ChunkRef<T>` and `ChunkLink<T>` are typed out-of-line references; they are
  semantic forms, not raw pointers.
- each chunk has bounded data bits;
- each chunk has at most eight refs;
- refs are ordered by fixed index;
- chunk hashes are content-addressed;
- a chunk tree root commits to the whole payload.
- `toChunk` is the canonical serialization path from a typed value to chunk
  representation.
- `fromChunk` is the canonical deserialization path from chunk representation
  back to the typed value.
- for every admissible semantic value, `fromChunk(toChunk(x)) == x`.
- `ChunkCursor` MUST NOT be used as an alternate serialization commitment or
  as a mutable runtime pointer.
- chunk trees MUST be bounded, deterministic, and free of host-specific
  ordering;
- invalid chunk trees, oversized spillover, or non-canonical nesting MUST be
  rejected during decode;
- any ad hoc compression or alternative chunk layout invalidates the
  commitment.

Large payloads MUST NOT be serialized by ad hoc compression or host-specific
encoding.

## 8. Hashing And Identification

The serialization layer does not pick one global hash. Each artifact defines
its own commitment:

- message IDs: SHA-256 over canonical message bytes;
- event topics: SHA-256 over canonical event selector and schema;
- StateInit hashes: BLAKE3-256 over canonical StateInit bytes;
- chunk hashes: BLAKE3-256 over canonical chunk bytes;
- state roots and ABI roots: SHA-256 over canonical sorted record sets.

Implementations MUST use the exact hash associated with the artifact type.

## 9. Canonicalization Example

```text
struct Transfer {
  to: Address
  amount: u64
}

Canonical bytes:
  u32 len(to) || to_bytes ||
  u64(amount)
```

If `amount` changes from `1` to `2`, the serialized bytes, hash, and selector
commitments MUST change.

## 10. Invalid Encodings

Decoders MUST reject:

- truncated payloads;
- trailing bytes after the canonical payload;
- non-canonical length prefixes;
- duplicate map keys;
- unordered canonical sets;
- oversized chunk trees;
- fields serialized in any order other than the canonical order.
- invalid chunk trees, overlong payloads, and depth-limit violations are decode
  errors.

## 11. Compiler Checks

- the compiler MUST verify canonical type usage before it emits artifacts;
- the compiler MUST reject ambiguous serialization paths;
- nested data, list depth, map depth, chunk-tree size, and field size MUST be
  checked against published bounds;
- the storage root MUST be declared exactly once;
- source-level schema declarations MUST not allow silent canonical rewrites;
- any ambiguity MUST fail at compile time, not normalize silently.

## 12. Runtime Decode Checks

Runtime decoders MUST reject:

- truncated payloads;
- trailing bytes;
- invalid length prefixes;
- duplicate map keys;
- unordered canonical sets;
- oversized chunk trees;
- unknown enum tags;
- invalid presence markers;
- non-canonical chunk representations.

Decode failure MUST be deterministic and MUST NOT leave partial state
mutation.

## 13. Hash Commitments

Canonical commitments MUST be deterministic for:

- storage root;
- canonical serialized value;
- chunk root;
- ABI package commitments;
- selector registry commitments.

The same semantic input MUST produce the same commitments across compiler runs.
Any change in layout, field order, selector binding, or serialization rules
MUST change the corresponding hash.
