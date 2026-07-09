# AVM Storage Model

This document defines the canonical contract storage model for AVM.

## 1. Storage Root

Each contract has one typed storage root.

Rules:

- the storage root is declared in the contract ABI;
- in Aetralis source, the root is referenced inside the contract body through
  `storage TypeName` and must not be inlined as an anonymous schema;
- the storage schema itself is an ordinary source symbol; the canonical
  authoring style is `@storage struct Name { ... }` at top level, bound into
  the contract by name;
- the root type MUST be serializable and deterministic;
- storage serialization MUST follow declared field order and the canonical
  rules from the serialization spec;
- the root hash MUST commit to every persisted field;
- the runtime MUST be able to reconstruct the root from canonical storage
  records;
- the storage root MUST be the single typed root for the contract state;
- storage layout MUST be derived from the named root type, not from runtime
  lookup or hidden fields;
- exactly one storage root is allowed per contract.
- the storage root type name is arbitrary and MUST NOT be reserved; examples
  such as `Storage`, `StorageType`, or `WalletState` are illustrative only and
  do not imply any built-in contract name.
- storage structs MAY contain nested storage structs, `Chunk<T>?` fields,
  `ChunkCursor` navigation state, and lazy fields when the layout remains
  deterministic;
- the compiler MUST generate canonical `toChunk` / `fromChunk` helpers from
  the storage schema so storage round-trips stay typed;
- `<StorageType>.load()` is read-only unpacking sugar over canonical
  deserialization and MUST NOT perform mutation;
- `<StorageType>.save()` is writeback sugar over canonical serialization and
  MUST commit through the same deterministic storage root;
- `<StorageType>.fromChunk()` and `<StorageType>.toChunk()` are the same
  canonical storage round-trip helpers exposed as user-facing sugar, not a
  separate runtime channel.

## 2. Load And Save

Load rules:

- storage is loaded by field path or stable key;
- missing non-lazy fields MUST resolve to the declared default;
- type coercion is not allowed;
- load order MUST be deterministic.

Save rules:

- dirty fields MUST be written in canonical field order;
- saving MUST update the storage root commitment;
- a save MUST NOT depend on native map iteration order;
- a save MUST fail if any field exceeds its bounded size.
- a save MUST preserve the canonical layout hash for all unchanged fields;
- a save MUST NOT introduce implicit fields or reorder nested data.
- nested structs, enums, lists, maps, options, results, and chunk-backed
  payloads MUST serialize using the canonical serialization spec before the
  root commitment is updated.

## 3. Lazy Fields

A lazy field is a field whose value may be materialized on first read.

Rules:

- a lazy field MUST have a deterministic initializer or derivation rule;
- the initializer MUST depend only on current canonical state and the current
  message/context, not on wall-clock time or nondeterministic host state;
- once materialized, the field MUST be saved in canonical form;
- a lazy field MUST still contribute to the storage root after materialization;
- lazy materialization MUST NOT change the storage layout commitment;
- a lazy field may materialize data from a Chunk graph, but the result MUST be
  deterministic and must not depend on host-local iteration order;
- lazy storage fields are canonical in storage schemas and are encoded through
  the generated `toChunk` / `fromChunk` helpers;
- `lazy` is allowed on storage fields and message decode paths only when the
  materialized value comes from canonical state, canonical context, or the
  canonical decode input;
- `lazy` materialization MUST NOT use nondeterministic sources or trigger
  early materialization before the field is actually requested.

## 4. Lists, Maps, And Iteration

- Lists are ordered and bounded.
- Maps are represented as canonical sorted key/value sets.
- nested data MUST stay within published depth and size bounds;
- a load or save that would exceed those bounds MUST fail deterministically.
- Iteration over maps MUST be explicitly bounded.
- Runtime MUST reject unbounded iteration.
- Runtime MUST reject any algorithm that depends on hash-map iteration order.

## 5. Bounded Iteration

Every iteration interface MUST accept a limit.

Rules:

- the limit MUST be positive;
- the limit MUST have a published maximum;
- iteration MUST stop once the limit is reached;
- requests that require completeness beyond the limit MUST be rejected rather
  than silently truncated;
- pagination MUST be explicit;
- the same storage root and request MUST produce the same page results.

## 6. ChunkRefs And Derived Data

Large values MAY be stored as chunk references.

Rules:

- the chunk root hash is the committed storage value;
- chunk trees MUST be canonical and bounded;
- `Segment` is a bounded read-only view used by query and pagination paths;
- `ChunkRef<T>` and `ChunkLink<T>` are the public typed reference forms for
  out-of-line storage nodes;
- `ChunkCursor` is a read-only navigation type for iterating chunk-backed
  storage and MUST NOT mutate the underlying tree;
- state reconstruction from `Chunk` trees MUST obey the canonical `toChunk`
  / `fromChunk` rules;
- any invalid chunk tree, oversized spillover, or malformed nested payload MUST
  be rejected during decode;
- derived indexes MAY be cached, but the cache MUST be derivable from the
  canonical root;
- a derived cache MUST NOT become the source of truth.

## 7. State Root Example

```text
@storage struct CounterState {
  owner: Address
  count: u64
  notes: list<bytes>
  cache: lazy bytes
}

load:
  owner, count, notes from canonical keys
  cache materialized on first read

save:
  write owner -> count -> notes -> cache in field order
```

## 8. Forbidden Patterns

Storage semantics MUST NOT allow:

- nondeterministic map traversal;
- implicit sort order based on host locale;
- reading uninitialized bytes as meaningful data;
- unbounded recursion through storage references;
- hidden writes during read-only queries.

## 9. Explorer And SDK Expectations

Explorers and SDKs MUST be able to:

- list storage fields in schema order;
- page through bounded iterators;
- show lazy fields explicitly;
- verify a storage root from canonical records;
- display hash commitments for nested chunks and derived values.
