# AVM Language Specification

This document defines the source-level language that compiles to AVM.
The language is deterministic, strongly typed, and closed under canonical
compilation. Anything not defined here is not part of the language.

## 1. Scope And Guarantees

The language has four hard guarantees:

- the same semantic source tree MUST produce the same ABI and module hash;
- the same ABI package MUST dispatch the same selectors for the same source;
- runtime behavior MUST be deterministic and reflection-free;
- object reuse MUST happen by composition, interfaces, traits, or compiler
  flattening, never by runtime inheritance.

The compiler MAY canonicalize syntax, ordering, imports, and metadata, but it
MUST NOT change semantic meaning.

## 2. Object Model

The language has seven top-level declaration kinds:

- `contract`: the primary deployable object type, analogous to a class.
- `struct`: a value type for state, message bodies, DTOs, and event payloads.
- `enum`: a closed tagged union for states, commands, routes, and errors.
- `message`: a callable public entrypoint of a contract.
- `getter`: a read-only entrypoint of a contract.
- `event`: an immutable schema for emitted records.
- `wallet action`: wallet-facing UI and permission metadata bound to ABI items.

Normative rules:

- a `contract` is the only deployable object type;
- a deployed contract instance has one canonical storage root and one code
  identity;
- `struct` and `enum` are pure types and do not have identity, lifecycle, or
  dispatch tables;
- `message`, `getter`, `event`, and `wallet action` are ABI-facing declarations;
- runtime inheritance is not used, and there is no virtual dispatch or
  reflection;
- any reuse across contract surfaces MUST be resolved statically.

## 3. Package And Reuse

Source files form a package graph. Packages may import other packages by stable
name and version.

Reuse is permitted through:

- interfaces: compile-time contracts for a selector surface;
- traits: reusable source-level behavior flattened by the compiler;
- composition: explicit field embedding and delegation;
- codegen flattening: the compiler may inline reusable fragments into the final
  canonical module.

Normative rules:

- interfaces and traits MUST NOT introduce runtime inheritance;
- imported behavior MUST be pinned by dependency lock and ABI hash;
- changing an imported package version MUST be observable in the dependency
  lock and final commitments.

## 4. Contract

A `contract` is the named root object that owns storage, entrypoints, and ABI.

Required properties:

- exactly one root storage type;
- exactly one code artifact per compiled module;
- deterministic selector set;
- deterministic StateInit derivation;
- deterministic ABI package.

Normative rules:

- a contract MUST declare its storage schema explicitly or through a canonical
  inferred root;
- contract state MUST be represented by the declared storage root and its
  nested fields;
- contract code MUST be deterministic for a given source tree, dependency
  lock, and compiler version;
- contract identity is derived from canonical StateInit, not from source text
  identity or runtime reflection;
- a contract MAY define deploy-time initialization logic, but initialization
  MUST execute exactly once per instance.

Contract semantics are class-like, but there is no inheritance tree at runtime.
All reuse is compiled away before deployment.

## 5. Struct

A `struct` is a product type with fields in declaration order.

Normative rules:

- field order is semantically significant;
- field names MUST be unique within the struct;
- field types MUST be serializable;
- a struct MAY contain nested structs, enums, options, lists, maps, refs, and
  fixed-width scalars;
- a struct MUST NOT contain host state, dynamic dispatch, or nondeterministic
  values.

Structs are value types:

- assigning a struct copies its value semantics;
- a struct has no identity outside the value it carries;
- a struct cannot be deployed and cannot receive messages.

## 6. Enum

An `enum` is a closed tagged union.

Normative rules:

- variant order is semantically significant;
- each variant MUST have a unique tag within the enum;
- a variant MAY carry zero or more ordered payload fields;
- enum matching MUST be exhaustive unless a wildcard arm is explicit;
- enum layout MUST be canonical and versioned.

Enums are value types:

- an enum has no identity or lifecycle;
- a variant MUST be discriminated deterministically;
- hidden variants or runtime-constructed tags are forbidden.

## 7. Functions And Purity

`fn` declares a pure helper function.

Normative rules:

- a pure function MUST be deterministic for the same inputs and MUST be
  storage-free and side-effect-free;
- a pure function MUST NOT emit messages, schedule work, or mutate state;
- recursion is allowed only when the compiler can prove bounded evaluation;
- pure functions are not ABI entrypoints unless they are explicitly exposed as
  messages or getters.

Pure functions are the source-level reuse mechanism for shared logic. They do
not create runtime objects.

## 8. Message

A `message` declares a callable contract entrypoint.

Message kinds:

- `external`: called from outside the chain, usually via a user-signed request.
- `internal`: called from another contract or runtime component.
- `bounced`: called after a failed bounceable inbound message.
- `deploy`: called during instance creation and initialization.

Normative rules:

- a message MUST map to exactly one canonical selector in its selector domain;
- a message MAY be stateful, payable, or both, depending on its declared
  effects;
- a message MAY read storage, write storage, emit outbound messages, or return
  a value, subject to its mutability rules;
- a message that changes state or moves value MUST expose its side effects in
  ABI metadata;
- `deploy` is the constructor-like entrypoint and MUST run before any normal
  external or internal dispatch for a new instance;
- `bounced` is a first-class message kind, not a special-case error path.

Dispatch is selector-based and kind-aware. The same text in different message
kinds is a different ABI item.

## 9. Getter

A `getter` declares a read-only entrypoint.

Normative rules:

- a getter MUST NOT mutate state;
- a getter MUST NOT emit consensus messages;
- a getter MUST be deterministic for the same state root and input;
- a getter MAY be served off-chain by wallets, explorers, or SDKs;
- a getter MAY read storage and derive values, but it MAY NOT depend on
  wall-clock time, local process state, or nondeterministic iteration.

## 10. Event

An `event` declares an immutable emission schema.

Normative rules:

- events are append-only from the contract perspective;
- event payloads MUST be serializable and canonically ordered;
- event data MUST be deterministic for a given state transition;
- events MUST NOT carry hidden mutable references.

## 11. Wallet Action

A `wallet action` is wallet-facing metadata attached to a message or getter.

Required fields:

- `title`
- `risk`
- `confirm_label`
- `warning_level`
- `expected_side_effects`
- `fund_access`
- `approval_semantics`

Normative rules:

- wallet metadata MUST come from the ABI or from a signed attestation;
- wallets MUST NOT invent or rewrite verified labels;
- if metadata is missing, wallets MUST treat the action as reviewable and
  potentially dangerous;
- a wallet action MUST remain stable across compilation if the underlying ABI
  is unchanged.

## 12. Visibility, Mutability, Access Control

The language uses a closed visibility and mutability model.

Visibility:

- `public`: exported in the ABI and callable through dispatch;
- `contract`: visible only within the same contract body;
- `package`: visible within the same package;
- `private`: visible only within the declaration scope or source file.

Mutability:

- `pure`: no state writes, no message emission, no side effects;
- `view`: read-only state access, no state writes, no message emission;
- `stateful`: may write state and may emit outbound messages;
- `payable`: may accept value transfer as part of dispatch.

Access control:

- authorization MUST be expressed as explicit deterministic checks;
- the runtime MUST NOT infer authority from inheritance, reflection, or host
  state;
- if access depends on sender, origin, role, or capability, that dependency
  MUST be encoded in the ABI or storage-derived checks.

## 13. Dispatch And Instance Lifecycle

Instance lifecycle:

1. source is parsed and type-checked;
2. the compiler canonicalizes the contract, selectors, storage layout, and ABI;
3. the deploy message produces canonical StateInit;
4. the runtime derives the contract address from StateInit and chain context;
5. the deploy handler runs exactly once;
6. the instance becomes active if and only if deploy succeeds;
7. later messages dispatch by kind and selector;
8. getters execute against a read-only snapshot;
9. migration, if supported, is an explicit versioned entrypoint and not an
   implicit runtime behavior.

Dispatch rules:

- the runtime MUST route by message kind before selector lookup;
- unknown selectors MUST fail closed;
- missing bounced handlers MUST not synthesize arbitrary fallback logic;
- one selector MUST resolve to at most one handler inside a single ABI package;
- deploy, getter, and bounced handlers are distinct dispatch domains.

## 14. Canonical Built-In Types

The compiler and runtime MUST agree on the following built-in types and their
meaning:

- `bool`: deterministic boolean value.
- `u8`, `u16`, `u32`, `u64`: fixed-width unsigned integers.
- `i64`: fixed-width signed integer.
- `bytes`: variable-length byte sequence.
- `string`: UTF-8 text encoded deterministically.
- `hash32`: 32-byte commitment value.
- `Address`: chain-bound account or contract address.
- `Coins`: non-negative base-unit amount.
- `Cell`: canonical chunk or payload root.
- `Slice`: bounded view over a `Cell`.
- `Ref<T>`: typed out-of-line reference.
- `Option<T>`: present-or-absent value.
- `Result<T, E>`: success-or-error value.
- `List<T>`: bounded ordered collection.
- `Map<K, V>`: bounded canonical map with deterministic ordering.

Normative rules:

- generic arity MUST match the type constructor;
- optionality is a type modifier, not a separate runtime object;
- all built-in types MUST have canonical serialization rules;
- built-ins MAY be nested, but their nesting MUST remain bounded and
  deterministic.

## 15. Canonical Compilation

A compiler targeting this language MUST emit:

- code bytes;
- ABI manifest;
- selector registry entries;
- storage schema commitments;
- message codec descriptors;
- event topic commitments;
- wallet action metadata;
- dependency lock commitments;
- StateInit commitments;
- module hash and ABI hash.

Canonical compilation rules:

- identical semantic input MUST produce identical module and ABI hashes;
- file formatting, import ordering, and declaration ordering MUST not change
  the result unless they change the semantic source tree;
- any ambiguity in selector derivation, field ordering, serialization, or
  storage layout MUST be rejected as a compile-time error.

## 16. Example

```text
contract Treasury {
  storage TreasuryState

  message deploy Init(owner: Address)
  message external Transfer(to: Address, amount: u64)
  message bounced Refund()
  getter GetBalance() -> u64
  event TransferRecorded(from: Address, to: Address, amount: u64)

  wallet action Transfer {
    title = "Transfer funds"
    risk = high
    confirm_label = "Send treasury funds"
    warning_level = warn
    expected_side_effects = ["state write", "value transfer"]
    fund_access = true
    approval_semantics = "spend"
  }
}
```

This example is valid only if the storage schema, selectors, serialization
rules, and ABI commitments are canonical and collision-free.
