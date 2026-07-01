# AVM Serialization Specification

This document defines the canonical serialization rules used by AVM language
types, ABI descriptors, message bodies, and storage commitments.

## 1. Canonical Principles

- Serialization MUST be deterministic.
- The same value MUST always serialize to the same byte sequence.
- Decoders MUST reject non-canonical encodings.
- Field order is fixed and part of the schema.
- Unknown fields are not allowed in canonical network payloads.

## 2. Scalar Types

Fixed-width scalars use big-endian encoding:

- `bool` -> 1 byte, `0x00` or `0x01`
- `u8` -> 1 byte
- `u16` -> 2 bytes
- `u32` -> 4 bytes
- `u64` -> 8 bytes
- signed integers -> two's-complement big-endian of the declared width

Addresses, hashes, and identifiers are fixed-length byte arrays and MUST be
encoded as raw bytes without reformatting.

## 3. Strings And Bytes

- `string` is UTF-8 bytes with a `u32` length prefix.
- `bytes` is raw bytes with a `u32` length prefix.
- Empty values are encoded as length `0`.
- Length prefixes MUST match the actual payload length exactly.

## 4. Structs

Struct fields are serialized in declaration order.

Rules:

- every declared field participates in the serialization;
- optional fields are encoded with an explicit presence marker followed by the
  value if present;
- nested structs are serialized inline using their own canonical rules;
- no field may depend on reflection order or language runtime map iteration.

## 5. Enums

Enums are serialized as:

1. variant tag;
2. variant payload, if any.

Variant tags are canonical and stable. Reordering variants changes the enum
encoding and MUST change the ABI hash.

## 6. Lists And Maps

- Lists are serialized as `u32` length followed by each element in order.
- Maps MUST NOT rely on native map iteration.
- Canonical map encoding is a sorted list of key/value entries.
- Keys are sorted by their canonical encoded bytes.
- Duplicate keys are invalid.

## 7. Large Payloads

Payloads larger than the inline limit MUST be represented through refs/chunks.

Canonical chunk rules:

- each chunk has bounded data bits;
- each chunk has at most eight refs;
- refs are ordered by fixed index;
- chunk hashes are content-addressed;
- a chunk tree root commits to the whole payload.

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
