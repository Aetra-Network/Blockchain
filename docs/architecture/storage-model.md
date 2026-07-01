# AVM Storage Model

This document defines the canonical contract storage model for AVM.

## 1. Storage Root

Each contract has one typed storage root.

Rules:

- the storage root is declared in the contract ABI;
- the root type MUST be serializable and deterministic;
- the root hash MUST commit to every persisted field;
- the runtime MUST be able to reconstruct the root from canonical storage
  records.

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

## 3. Lazy Fields

A lazy field is a field whose value may be materialized on first read.

Rules:

- a lazy field MUST have a deterministic initializer or derivation rule;
- the initializer MUST depend only on current canonical state and the current
  message/context, not on wall-clock time or nondeterministic host state;
- once materialized, the field MUST be saved in canonical form;
- a lazy field MUST still contribute to the storage root after materialization.

## 4. Lists, Maps, And Iteration

- Lists are ordered and bounded.
- Maps are represented as canonical sorted key/value sets.
- Iteration over maps MUST be explicitly bounded.
- Runtime MUST reject unbounded iteration.
- Runtime MUST reject any algorithm that depends on hash-map iteration order.

## 5. Bounded Iteration

Every iteration interface MUST accept a limit.

Rules:

- the limit MUST be positive;
- the limit MUST have a published maximum;
- iteration MUST stop once the limit is reached;
- pagination MUST be explicit;
- the same storage root and request MUST produce the same page results.

## 6. Refs And Derived Data

Large values MAY be stored as chunk references.

Rules:

- the chunk root hash is the committed storage value;
- chunk trees MUST be canonical and bounded;
- derived indexes MAY be cached, but the cache MUST be derivable from the
  canonical root;
- a derived cache MUST NOT become the source of truth.

## 7. State Root Example

```text
storage CounterState {
  owner: Address
  count: u64
  notes: list<bytes>
  cache lazy bytes
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
