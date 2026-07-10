# Aetralis Language Specification

This document defines the source-level language that compiles to AVM.
Aetralis is the only public name of the stable contract track.
Aetralis is the stable contract language track.
The canonical source extension is `.atlx`.
Legacy names or transitional aliases are not part of the public surface
language.
This language spec is the single source of truth for surface syntax and
stable naming.

For practical contract-writing conventions and recommended ATLX patterns, see
[`atlx-practical-guide.md`](./atlx-practical-guide.md).

The grammar and ABI rules for `contract`, `struct`, `enum`, `type`, `func`,
and the declaration annotations are normative here. Messages are declared as
`@message(opcode)` structs grouped into closed unions; handlers and getters
are declared only through annotated functions (`@internal func
onInternalMessage(...)`, `@external func onExternalMessage(...)`, `@bounced
func onBouncedMessage(...)`, `@get func name(): T`). The language is
deterministic, strongly typed, and closed under canonical compilation.
Anything not defined here is not part of the language. The runnable contracts
under `examples/avm/` are the reference for this surface.

## Canonical Surface Grammar

The stable-track source extension is `.atlx`.
The grammar below defines the canonical public surface accepted by Aetralis v1.
The compiler MAY accept transitional compatibility forms only where the
compatibility policy in this document allows it, but every accepted source MUST
canonicalize to the same AST, ABI package, selector registry, and artifact
layout for the same semantic tree.

```ebnf
source              := { top_decl }

top_decl            := import_decl
                     | const_decl
                     | struct_decl
                     | enum_decl
                     | type_decl
                     | func_decl
                     | contract_decl

import_decl         := "import" [ alias ] string_literal [ "version" string_literal ] [ "as" alias ] [ ";" ]
const_decl          := "const" Ident "=" expr [ ";" ]

struct_decl         := [ annotation ] "struct" Ident "{" { struct_field [ ";" ] } "}"
struct_field        := Ident [ ":" ] [ "lazy" ] type_ref [ "=" expr ]

enum_decl           := "enum" Ident "{" { enum_variant [ ";" ] } "}"
enum_variant        := Ident [ "(" [ field_decl { "," field_decl } ] ")" ]
field_decl          := Ident ":" type_ref
type_decl           := "type" Ident "=" type_ref { "|" type_ref } [ ";" ]

func_decl           := [ annotation ] "func" func_name param_list [ return_type ] block
func_name           := Ident [ "." Ident ]

contract_decl       := "contract" Ident "{" { contract_item } "}"
contract_item       := contract_meta_decl
                     | func_decl

contract_meta_decl  := "author" ":" string_literal
                     | "description" ":" string_literal
                     | "version" ":" string_literal
                     | "storage" ":" TypeName
                     | "incomingMessages" ":" TypeName
                     | "incomingExternal" ":" TypeName
                     | "namespace" string_literal
                     | "chain" string_literal
                     | "deployer" string_literal
                     | "salt" string_literal
                     | "initial_balance" integer_literal

annotation          := "@internal"
                     | "@external"
                     | "@bounced"
                     | "@get"
                     | "@pure"
                     | "@impure"
                     | "@store"
                     | "@storage"
                     | "@message" "(" integer_literal ")"

param_list          := "(" [ param { "," param } ] ")"
param               := [ "mutate" ] Ident [ ":" type_ref ]
return_type         := "->" type_ref | ":" type_ref
block               := "{" { statement [ ";" ] } "}"
```

At most one annotation is allowed per declaration. A contract MUST declare
`incomingMessages:`, `incomingExternal:`, or both. Handler functions use
reserved names bound to their annotations: `@internal` requires
`onInternalMessage(in: InMessage)`, `@external` requires
`onExternalMessage(inMsg: Segment)`, and `@bounced` requires
`onBouncedMessage(in: InMessageBounced)`; each may be declared at most once
per contract and the reserved names may not be used anywhere else.

Only the declaration forms in the grammar above exist. Anything outside it —
including legacy declaration styles from other languages — is a parse error.
Annotations never carry argument lists except `@message(opcode)`; handler
parameters are declared in the function signature.

Canonicalization rules:

- the canonical public surface is keyword-based, deterministic, and
  case-sensitive;
- annotation names are semantic labels preserved in ABI and tooling metadata;
- message opcodes come exclusively from the `@message(opcode)` annotation;
  getter selectors are derived from the canonical getter name — there is no
  explicit selector pin;
- any ambiguous precedence, associativity, or nesting rule is a grammar
  design error and MUST be rejected at compile time;
- a semantic source tree that differs only by formatting, import order, or
  declaration order MUST produce the same canonical AST and artifacts.

Surface-vs-stdlib boundary:

- `@storage` and `@message` are declaration annotations for schema-bearing
  types;
- `@internal`, `@external`, `@bounced`, and `@get` are contract entrypoint
  annotations;
- `@pure` and `@impure` are helper-function mutability annotations;
- `buildMessage`, `<StorageType>.load`, `<StorageType>.save`, `fromChunk`, `toChunk`,
  `SEND_*`, `ChunkCursor`, and `lazy` are canonical language/stdlib surface
  forms that MUST lower into ABI, serialization, or runtime behavior and MUST
  NOT be treated as ad hoc runtime helpers.

### Type System

Aetralis uses a strict, closed type system. The compiler MUST reject any
ambiguous or implicit type behavior at compile time.

Type classes:

- scalar types: fixed-width integers, booleans, addresses, hashes, and other
  primitive ABI values;
- value types: `struct` and `enum`;
- union aliases: `type Name = A | B | C`, used for closed message-family and
  routing schemas;
- collections: `List<T>` and `Map<K, V>`;
- nullable and branching types: `Option<T>` and `Result<T, E>`;
- chunk-backed payload types: `Chunk`, `Segment`, `ChunkRef<T>`, `ChunkLink<T>`,
  and `ChunkCursor`;
- contract storage root types: exactly one named root type per contract.

Normative rules:

- runtime typing and reflection are forbidden;
- implicit casts are forbidden unless they are an explicit language rule and
  preserve canonical semantics;
- field order, variant order, generic arity, and serialization shape are part
  of the type identity;
- any ambiguity in type name, arity, field order, nesting, or encoding MUST be
  a compile-time error;
- `struct` and `enum` are value types and are never instantiable roots;
- `List<T>` and `Map<K, V>` are bounded collections and MUST have canonical
  ordering rules;
- `Option<T>` and `Result<T, E>` are tagged branching types and MUST have a
  single canonical encoding per value;
- chunk-backed payload types MUST preserve out-of-line payload identity in ABI
  and serialization commitments;
- `ChunkCursor` is read-only navigation metadata and MUST NOT be used as a
  mutable storage carrier or hidden state source;
- `lazy` materialization MUST be deterministic and MUST derive only from the
  canonical state, canonical message context, or canonical decode input;
- `lazy` materialization MUST NOT depend on wall-clock time, host-local
  randomness, process state, iteration order, or early materialization of a
  value that is not yet requested.

Currency literal helper:

- `aet("...")` is a compile-time builtin that converts an exact decimal AET
  amount into base-unit `Coins`;
- it accepts only string literals and MUST reject excess fractional precision
  instead of rounding;
- it MUST not perform runtime parsing or depend on host state.

Send mode builtins:

- `SEND_DEFAULT`, `SEND_CARRY_REMAINDER`, `SEND_DRAIN_BALANCE`,
  `SEND_ESTIMATE_ONLY`, `SEND_FEE_FROM_BALANCE`, `SEND_IGNORE_ERRORS`,
  `SEND_BOUNCE_ON_FAIL`, and `SEND_DESTROY_IF_EMPTY` are canonical typed
  `u32` surface constants;
- the compiler MUST lower them to runtime send-mode flags rather than treating
  them as magic numbers;
- the constants are intrinsic to the language surface and MUST remain stable
  across formatting and canonicalization;
- `msg.send()` is the preferred high-level surface form for outbound sends. It
  takes no arguments: the send mode is declared exclusively in the message's
  optional `mode:` field (`buildMessage({ ..., mode: SEND_... })`), so delivery
  semantics have exactly one canonical home. `.send()` MUST lower through the
  same canonical runtime envelope as the equivalent structured send statement;
- a message without a `mode:` field is sent as `SEND_DEFAULT`;
- implementations MUST reject `.send(...)` arguments and unknown send-mode
  identifiers at compile time.

Unknown-message policy:

- the runtime MUST reject unknown selectors deterministically and MUST NOT
  partially execute the contract before rejection;
- external messages default to reject if no handler matches;
- internal messages default to reject if no handler matches;
- empty top-up or no-op behavior is allowed only when it is explicit in source
  and ABI metadata, not as an implicit fallback;
- bounced messages without a handler MUST terminate without creating a new
  bounce loop;
- unknown-message policy MUST be part of the language spec and ABI metadata.

## Public Name And File Extension

- The public name of the language is `Aetralis`.
- The canonical source extension is `.atlx`.
- Any older name, alias, or transitional label is compatibility-only and is
  not part of the stable public surface.

## Package And Versioning Rules

- Packages form a dependency graph and MUST be versioned explicitly.
- Package identity is determined by package name, version, source hash, ABI
  hash, selector registry hash, storage schema hash, and migration version.
- Semantic changes MUST bump versioned commitments.
- Rename-only changes are breaking unless they are explicitly marked as
  deprecated aliases under a compatibility policy.

## Compatibility Policy

- `Aetralis v1.0` accepts deprecated aliases with warnings.
- `Aetralis v1.1` is the cutoff version for new projects: deprecated aliases
  become hard errors unless an explicit compatibility profile is enabled.
- `Aetralis v2.0` removes the compatibility layer entirely.
- Compatibility behavior MUST be explicit and MUST NOT silently rewrite
  semantics without a migration policy.

## Deprecated Aliases

Deprecated aliases are transitional only:

- `Ref<T>` -> `ChunkRef<T>` or `ChunkLink<T>`

Deprecated alias handling rules:

- `Ref<T>` is a transitional compatibility form only;
- warnings MUST be emitted in compatibility mode before the cutoff version;
- hard errors MUST be emitted at and after the cutoff version for new-project
  compatibility profiles;
- the compatibility layer MUST NOT introduce new semantics or persistence
  commitments.

## Migration Version Policy

- Every public terminology change MUST carry a migration version.
- The migration version MUST be listed in the language spec and in release
  notes.
- Backward compatibility for old projects MUST be described in terms of the
  migration version and cutoff version.

## Canonical Terminology Table

| Canonical term | Surface role | Legacy aliases | Notes |
|---|---|---|---|
| `Aetralis` | language name | `AVM language`, other transitional labels | Public name only |
| `.atlx` | source extension | `.avm` | Canonical stable-track extension |
| `ChunkRef<T>` | deferred reference | `Ref<T>` | Preferred for out-of-line payload access |
| `ChunkLink<T>` | semantic reference | `Ref<T>` | Use when the relationship itself is public ABI |

## 1. Scope And Guarantees

The language has four hard guarantees:

- the same semantic source tree MUST produce the same ABI and module hash;
- the same ABI package MUST dispatch the same selectors for the same source;
- runtime behavior MUST be deterministic and reflection-free;
- object reuse MUST happen by composition, interfaces, traits, or compiler
  flattening, never by runtime inheritance.

Stable-track guarantees:

- `Aetralis v1` is the stable contract track;
- `.atlx` is the only canonical source extension for stable-track contracts;
- the grammar, ABI rules, and selector rules in this document are normative;
- any ambiguity in selector derivation, field ordering, serialization, or
  storage layout MUST be rejected as a compile-time error.

The compiler MAY canonicalize syntax, ordering, imports, and metadata, but it
MUST NOT change semantic meaning.

Normative stability rules:

- identical semantic input MUST produce identical module, manifest, selector
  registry, storage layout, codec, event topic, wallet action, dependency
  lock, and StateInit commitments;
- canonical serialization MUST be deterministic across compiler runs;
- semantic changes in any package MUST be reflected in the package lock, ABI
  commitments, and selector registry;
- rename-only changes without a version bump are breaking changes.

## 2. Object Model

The language has six top-level declaration kinds:

- `contract`: the primary instantiable object type, analogous to a class.
- `struct`: a value type for state, message bodies, and DTOs; annotated
  `@storage` for storage schemas and `@message(opcode)` for message payloads.
- `enum`: a closed tagged union for states, commands, routes, and errors.
- `type`: a closed union or alias used for message-family routing and typed
  decode surfaces.
- `func`: a top-level helper function.
- `const`: a named compile-time constant.

Contract members are metadata keys (`storage:`, `incomingMessages:`,
`incomingExternal:`, `author:`, `description:`, `version:`) and annotated
member functions: `@internal`/`@external`/`@bounced` handlers, `@get`
getters, `@store` storage codecs, and plain helpers.

Normative rules:

- a `contract` is the only instantiable object type;
- a created contract instance has one canonical storage root and one code
  identity;
- `struct` and `enum` are pure types and do not have identity, lifecycle, or
  dispatch tables;
- `@message(opcode)` structs, handler functions, and `@get` getters are the
  ABI-facing declarations;
- runtime inheritance is not used, and there is no virtual dispatch or
  reflection;
- any reuse across contract surfaces MUST be resolved statically.
- a `@get` getter MUST always declare an explicit return type
  (`@get func name(): T`);
- event schemas and wallet-action records are manifest-level ABI metadata,
  not source declaration forms; where present they are ABI-significant and
  MUST participate in hash commitments and selector registry records.

## 3. Package And Reuse

Source files form a package graph. Packages may import other packages by
canonical package name and version.

Project-local imports are also allowed when a source tree is compiled from a
file root. In that mode, the compiler may resolve file paths such as
`"constants.atlx"` or `"structs/messages.atlx"` relative to the compilation
root and normalize them into the same deterministic dependency graph.

Each package MUST participate in a dependency lock that records:

- package name;
- package version;
- source hash;
- ABI hash;
- selector registry hash;
- storage schema hash;
- migration version.

Reuse is permitted through:

- interfaces: compile-time contracts for a selector surface;
- traits: reusable source-level behavior flattened by the compiler;
- composition: explicit field embedding and delegation;
- codegen flattening: the compiler may inline reusable fragments into the final
  canonical module.

Normative rules:

- interfaces and traits MUST NOT introduce runtime inheritance;
- imported behavior MUST be pinned by dependency lock and ABI hash;
- dependency lock commitments are part of the canonical ABI surface;
- changing an imported package version MUST be observable in the dependency
  lock and final commitments;
- semantic changes in a package MUST require a version bump;
- compatible changes MAY add new declarations, new fields with canonical
  defaults, or new versioned ABI records, but MUST NOT change existing
  selector ids or storage commitments;
- breaking changes include rename-only changes, field reordering, selector
  changes, storage layout changes, and mutability changes.

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
- the storage root MUST be a named type and the contract MUST reference it via
  `storage TypeName`;
- contract storage MUST be represented as one typed root, not as an anonymous
  bag of fields;
- contract state MUST be represented by the declared storage root and its
  nested fields;
- contract code MUST be deterministic for a given source tree, dependency
  lock, and compiler version;
- contract identity is derived from canonical StateInit, not from source text
  identity or runtime reflection;
- a contract MAY define initialization logic, but initialization
  MUST execute exactly once per instance.

A contract MAY also carry human metadata fields such as `author`,
`description`, and `version`. These fields are informational only and MUST
NOT affect ABI commitments, selector derivation, storage layout, or runtime
behavior.

Contract semantics are class-like, but there is no inheritance tree at runtime.
All reuse is compiled away before instance creation.

Storage schema declarations and message-body declarations are ordinary source
symbols. A contract binds those symbols into its ABI surface, so the canonical
authoring style is to define them at top level and reference them from the
contract shell rather than nesting them inline unless a compatibility form is
required.

## 5. Struct

A `struct` is a product type with fields in declaration order.

Normative rules:

- field order is semantically significant;
- field names MUST be unique within the struct;
- field types MUST be serializable;
- a struct MAY contain nested structs, enums, options, results, lists, maps,
  chunk references, lazy fields, and fixed-width scalars;
- a struct MUST NOT contain host state, dynamic dispatch, or nondeterministic
  values.

Structs are value types:

- assigning a struct copies its value semantics;
- a struct has no identity outside the value it carries;
- a struct cannot be instantiated as a contract and cannot receive messages.
- `@storage struct Name { ... }` declares a persistent storage schema and is
  the canonical source form for contract state;
- `@message(0x...) struct Name { ... }` declares a typed message-body schema
  and binds the opcode canonically for ABI decode;
- storage structs MAY contain nested storage structs, `Chunk<T>?` fields, and
  lazy fields when the layout remains deterministic;
- storage structs expose canonical `toChunk` / `fromChunk` helpers generated
  by the compiler from the declared schema.
- `<StorageType>.load()` and `<StorageType>.fromChunk()` are canonical
  read-only unpacking sugar over the same serialization path; they MUST NOT
  introduce mutable runtime state or alternate decode semantics.
- `<StorageType>.save()` and `<StorageType>.toChunk()` are canonical writeback
  sugar over the same serialization path; they MUST preserve the storage
  commitment and MUST NOT bypass canonical serialization.
- nested storage schemas and chunk-backed fields MUST round-trip through the
  canonical storage schema, not through ad hoc runtime helpers.

## 6. Enum

An `enum` is a closed tagged union.

Normative rules:

- variant order is semantically significant;
- each variant MUST have a unique tag within the enum;
- a variant MAY carry zero or more ordered payload fields;
- enum matching MUST be exhaustive unless a wildcard arm is explicit;
- union matching MUST be exhaustive unless a wildcard arm is explicit, and
  the compiler MUST emit a compile-time error for any unhandled union member;
- enum layout MUST be canonical and versioned.

Enums are value types:

- an enum has no identity or lifecycle;
- a variant MUST be discriminated deterministically;
- hidden variants or runtime-constructed tags are forbidden.

## 7. Functions And Mutability

`func` declares a helper function in the top-level symbol namespace.

Normative rules:

- `@pure` and `@impure` are the canonical explicit mutability markers for
  helper functions and are mutually exclusive;
- a pure helper function MUST be deterministic for the same inputs and MUST
  be storage-free and side-effect-free;
- a pure helper function MUST NOT write persistent storage, emit or send
  messages, schedule work, commit or finalize chain-visible effects, mutate a
  runtime seed, or call any helper or host API that is itself classified as
  impure;
- an impure helper function MAY write persistent storage, emit or send
  messages, commit or finalize chain-visible effects, mutate a runtime seed,
  or perform any other chain-visible side effect;
- unannotated helper functions are purity-inferred from their body and call
  graph; if they contain no side effects they are pure, otherwise they are
  impure;
- the compiler MUST diagnose any `@pure` function that directly performs a
  side effect or calls an impure helper;
- the compiler MUST accept `@impure` for helpers whose body or host-call
  behavior is impure, and it MUST classify such helpers as impure even when
  the body contains no direct writes;
- AVM host calls are classified by effect: read-only host calls MAY be used in
  pure helpers, but host calls such as storage write, outbound message
  emission, commit/finalize, or random-seed mutation make the helper impure
  and MUST be rejected if the function was explicitly marked `@pure`;
- recursion is allowed only when the compiler can prove bounded evaluation;
- unbounded recursion MUST be rejected as a compile-time error;
- pure functions are not ABI entrypoints unless they are explicitly exposed as
  messages or getters.
- message-handler annotations have fixed reserved function names;
- inside a single `contract {}` block, `@external` handlers MUST use
  `onExternalMessage`;
- inside a single `contract {}` block, `@internal` handlers MUST use
  `onInternalMessage`;
- inside a single `contract {}` block, `@bounced` handlers MUST use
  `onBouncedMessage`;
- the reserved handler names MUST NOT be used by ordinary helper functions or
  by handlers with a different annotation.

## 8. Contract Metadata

A contract may declare human metadata fields inside the contract shell:
`author`, `description`, and `version`.

Normative rules:

- these fields are informational only;
- they MUST NOT affect ABI commitments, selector derivation, storage layout,
  StateInit, or runtime behavior;
- the canonical serialization order is `author`, then `description`, then
  `version`;
- absent fields are omitted from the canonical metadata record rather than
  serialized as empty strings;
- if toolchains emit a metadata hash, it MUST be computed from the canonical
  metadata record only and MUST remain separate from ABI, layout, and selector
  commitments.

Top-level helper functions are the source-level reuse mechanism for shared
logic. They do not create runtime objects.

### Symbol Resolution

The compiler MUST resolve names using separate namespaces:

- top-level declarations: packages, imports, structs, enums, and helper
  functions;
- contract members: contract metadata keys and annotated member functions
  (handlers, `@get` getters, `@store` storage codecs, helpers);
- ABI entrypoints: canonical dispatch records derived from contract members.
- ABI entrypoints are resolved separately from top-level type and helper
  symbols, so message unions do not collide with ordinary declarations.

Resolution rules:

- local variables and parameters shadow top-level helper symbols inside a
  function body;
- top-level helper symbols MAY reuse names that appear in contract members;
  the compiler disambiguates by namespace, so the same spelling is only an
  error when two declarations collide inside the same namespace;
- ABI entrypoints are not ordinary expression symbols and MUST be resolved only
  during manifest generation and dispatch lowering;
- if two declarations collide inside the same namespace, the compiler MUST
  reject the source as ambiguous.

Union/type resolution rules:

- `type Name = A | B | C` declares a canonical closed union of source symbols;
- the compiler MUST resolve union members against top-level declarations before
  lowering ABI dispatch;
- union aliases MAY exist outside contracts and are available as ordinary
  symbols;
- canonical typed decode SHOULD prefer the declared union or message schema
  over raw segment parsing when a typed binding exists.

## 8. Message

A `@message(opcode)` struct declares the canonical typed payload schema for
ABI decode. The compiler uses it to build opcode-to-struct decode tables and
to keep raw segment parsing out of the canonical path. Message schemas are
grouped into closed unions (`type InternalMsg = A | B | C`) that the contract
binds through `incomingMessages:` and `incomingExternal:`; the annotated
handler functions are the callable entrypoints.

Message kinds:

- `external`: called from outside the chain, usually via a user-signed
  request; handled by `@external func onExternalMessage(inMsg: Segment)`.
- `internal`: called from another contract or runtime component; handled by
  `@internal func onInternalMessage(in: InMessage)`.
- `bounced`: called after a failed bounceable inbound message; handled by
  `@bounced func onBouncedMessage(in: InMessageBounced)`.

Normative rules:

- a message body schema MUST map to exactly one canonical opcode or selector
  binding in its schema domain;
- dispatch MUST be kind-first, then selector-first, and internal messages MUST
  decode through the canonical ABI descriptor before body execution;
- if a canonical selector or opcode does not map to a declared typed payload,
  the compiler or runtime MUST reject the message before execution;
- a message MAY be impure, payable, or both, depending on its declared
  effects;
- a message MAY read storage, write storage, emit outbound messages, or return
  a value, subject to its mutability rules;
- a message that changes state or moves value MUST expose its side effects in
  ABI metadata;
- `bounced` is a first-class message kind, not a special-case error path.
- handler names are reserved and fixed: `onInternalMessage`,
  `onExternalMessage`, and `onBouncedMessage` MUST be used with their
  matching annotations and nowhere else; the message kind in the ABI remains
  authoritative;
- message-body schemas MAY be matched as typed unions after opcode decode, so
  `match msg { ... }` can operate on decoded message symbols instead of raw
  slices.
- union aliases MAY bind those message-body schemas into named families such as
  `type InternalMsg = Inc | Dec | Withdraw | SetTarget`; selector and opcode
  derivation MUST remain canonical for the member schemas themselves.
- handlers MAY inspect canonical envelope fields such as `value`, `bounce`,
  `bounced`, `flags`, `opcode`, and `body` where those fields are defined by
  the message kind and decoder.

`buildMessage({ ... })` is the canonical builder DSL for outbound message
construction. It is surface syntax that MUST lower into the canonical ABI /
runtime message envelope, not a runtime-only helper.

Builder rules:

- `buildMessage` MUST accept `bounce`, `amount`, `receiver`, `body`, and typed
  body payloads;
- typed body payloads MUST lower through the canonical message-body codec and
  MUST NOT bypass ABI encoding;
- the compiler MUST lower builder output before ABI hashing, selector
  generation, or runtime dispatch;
- `buildMessage` MUST NOT be implemented as a runtime-only API, because that
  would leave ABI commitments under-specified and unsafe.

Dispatch is selector-based and kind-aware. The same text in different message
kinds is a different ABI item.

## 9. Getter

A getter is a contract function annotated `@get` with an explicit return
type: `@get func name(): T`. It declares a read-only entrypoint; its selector
is derived from the canonical name.

Normative rules:

- a getter MUST NOT mutate state;
- a getter MUST NOT emit consensus messages;
- a getter MUST be deterministic for the same state root and input;
- a getter MAY be served off-chain by wallets, explorers, or SDKs;
- a getter MAY read storage and derive values, but it MAY NOT depend on
  wall-clock time, local process state, or nondeterministic iteration.

## 10. Event

An event is an immutable emission schema recorded in the interface manifest.
ATLX v1 has no `event` source declaration form; event schemas enter the
manifest through tooling and attestation layers, not contract source.

Normative rules:

- events are append-only from the contract perspective;
- event payloads MUST be serializable and canonically ordered;
- event data MUST be deterministic for a given state transition;
- events MUST NOT carry hidden mutable references.

## 11. Wallet Action

A wallet action is wallet-facing metadata attached to a message or getter in
the interface manifest. ATLX v1 has no `wallet action` source declaration
form; wallet-action records enter the manifest through tooling and
attestation layers, not contract source.

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
- any change to wallet action metadata is ABI-significant and MUST be
  reflected in canonical commitments.

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
- `impure`: may write state and may emit outbound messages;
- `payable`: may accept value transfer as part of dispatch.

Local bindings:

- `const` declares an immutable local binding;
- `var` declares a mutable local binding;
- local bindings MUST use one of these two forms only in stable-track source.

Control flow and expressions:

- `break` and `continue` are supported inside loop bodies;
- `while`, `do`, and `repeat` are deterministic runtime loops, and `repeat`
  count MAY be computed at runtime;
- `match` supports bindings and destructuring in arms, and matching MUST remain
  exhaustive unless a wildcard arm is explicit;
- comparison operators are `==`, `!=`, `<`, `>`, `<=`, `>=`, and `<=>`;
- logical operators are `!`, `&&`, and `||`, nullable operators are `??` and
  `!`, and the ternary operator is `cond ? a : b`;
- conditions MUST be explicit booleans; implicit `int -> bool` conversion is
  forbidden.

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
3. contract creation produces canonical StateInit;
4. the runtime derives the contract address from StateInit and chain context;
5. the creation handler runs exactly once;
6. the instance becomes active if and only if creation succeeds;
7. later messages dispatch by kind and selector;
8. getters execute against a read-only snapshot;
9. migration, if supported, is an explicit versioned entrypoint and not an
   implicit runtime behavior.

Dispatch rules:

- the runtime MUST route by message kind before selector lookup;
- unknown selectors MUST fail closed;
- unknown-message policy is reject by default unless the ABI metadata states
  an explicit compatibility or no-op policy;
- a no-op path MUST be explicit in source and ABI metadata; it MUST NOT be
  implied by a missing handler or an empty body;
- missing bounced handlers MUST not synthesize arbitrary fallback logic;
- one selector MUST resolve to at most one handler inside a single contract;
- creation, getter, and bounced handlers are distinct dispatch domains.

## 14. Canonical Built-In Types

The compiler and runtime MUST agree on the following built-in types and their
meaning:

- `bool`: deterministic boolean value encoded as a single byte `0x00` or
  `0x01`.
- `u8`, `u16`, `u32`, `u64`: fixed-width unsigned integers encoded in
  big-endian byte order.
- `i64`: fixed-width signed integer encoded as two's-complement big-endian.
- `bytes`: variable-length byte sequence.
- `string`: UTF-8 text encoded deterministically.
- `hash32`: 32-byte commitment value.
- `Address`: chain-bound account or contract address.
- `Coins`: non-negative base-unit amount.
- `Chunk`: canonical public base data unit and payload root.
- `Segment`: bounded public view over a `Chunk`.
- `ChunkCursor`: read-only navigation type for bounded chunk traversal.
- `ChunkRef<T>`: typed out-of-line reference optimized for deferred loading.
- `ChunkLink<T>`: typed out-of-line reference used when the relationship
  itself is part of the domain model or public ABI.
- `Option<T>`: present-or-absent value.
- `Result<T, E>`: success-or-error value.
- `List<T>`: bounded ordered collection with canonical element order.
- `Map<K, V>`: bounded canonical map with deterministic ordering and unique
  keys.

Normative rules:

- generic arity MUST match the type constructor;
- optionality is a type modifier, not a separate runtime object;
- all built-in types MUST have canonical serialization rules;
- built-ins MAY be nested, but their nesting MUST remain bounded and
  deterministic;
- `Option<T>` MUST encode as a tagged nullable branch and `Result<T, E>` MUST
  encode as a tagged success-or-error branch;
- `ChunkRef<T>` and `ChunkLink<T>` MAY share the same wire encoding, but the
  compiler MUST preserve the declared semantic form in ABI and documentation;
- `ChunkCursor` is a read-only navigation type and MUST NOT commit mutable
  state or hidden materialization state;
- `Ref<T>` is a deprecated alias only and MUST NOT be emitted as new public
  syntax.

Canonical selector rules:

- canonical selector text MUST include kind, contract or interface name,
  callable name, canonical parameter types, and canonical return types;
- numeric selectors MUST derive from canonical selector text using the
  versioned ABI derivation algorithm;
- explicit numeric selectors are compatibility pins only and MUST NOT create a
  second semantic path without a version bump;
- numeric collisions inside one ABI package are hard errors.

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

- identical semantic input MUST produce identical module, manifest, selector
  registry, storage layout, codec, wallet action, dependency lock, and
  StateInit hashes;
- file formatting, import ordering, and declaration ordering MUST not change
  the result unless they change the semantic source tree;
- any ambiguity in selector derivation, field ordering, serialization, or
  storage layout MUST be rejected as a compile-time error.

## 16. Regression Barriers

The stable contract track is only considered stable when the compiler,
runtime, and tooling tests keep these commitments green:

- grammar and ABI lowering are locked by compiler snapshot tests;
- selector derivation, storage layout, wallet actions, and canonical
  serialization are locked by artifact hash comparisons;
- standard contract tracks such as counter, treasury, token, NFT, and DEX are
  compiled and executed by conformance smoke tests;
- negative cases such as selector collisions, getter mutation, and bounded gas
  rejection remain covered by regression tests;
- legacy alias coverage remains available only in compatibility-mode tests.

## 17. Migration Policy

Deprecated alias terms are transitional compatibility only.

Deprecated alias table:

| Old term | New term | Deprecated since | Removed in | Compatibility rule |
|----------|----------|------------------|------------|--------------------|
| `Ref<T>` | `ChunkRef<T>` or `ChunkLink<T>` | Aetralis v1.0 | Aetralis v2.0 | Warning in compatibility mode; compiler must suggest the explicit replacement |

Migration rules:

- Aetralis v1.0 MAY accept legacy aliases with compiler warnings and optional
  auto-fix suggestions.
- Aetralis v1.1 is the cutoff for new projects: legacy terms MUST be rejected
  by default unless the project explicitly opts into a compatibility profile.
- Aetralis v2.0 removes the compatibility layer entirely and treats legacy
  aliases as hard errors everywhere.
- Automatic migration SHOULD be provided for direct rename cases.
- Manual migration guidance is REQUIRED for ambiguous `Ref<T>` replacements
  because the compiler must choose between `ChunkRef<T>` and `ChunkLink<T>`
  based on the semantic context.
- Migration notes for existing codebases MUST list the old term, the new term,
  the affected files or declarations, the compatibility profile used, and the
  target version that removes the alias.
- Release notes MUST include the migration version and the backward
  compatibility policy for any change that touches public terminology.

## 18. Example

The canonical style is the one used by the runnable contracts under
`examples/avm/` (see `examples/avm/counter_should_be.atlx` and
`examples/avm/token/`):

```text
const ERR_BAD_MSG = 0xFFFF

@storage
struct TreasuryState {
  balance: Coins = aet("0")
}

@message(0x4101)
struct Deposit {
  amount: uint64
}

@message(0x4102)
struct Withdraw {
  amount: uint64
}

type TreasuryMsg = Deposit | Withdraw

contract Treasury {
  author: "Aetralis reference"
  description: "Canonical treasury reference contract"
  version: "1.0.0"

  storage: TreasuryState
  incomingMessages: TreasuryMsg

  @store
  func TreasuryState.load() {
    return TreasuryState.fromChunk(contract.getData())
  }

  @store
  func TreasuryState.save(self) {
    contract.setData(self.toChunk())
  }

  @internal
  func onInternalMessage(in: InMessage) {
    const msg = lazy TreasuryMsg.fromSegment(in.body)

    match (msg) {
      Deposit => {
        var st = lazy TreasuryState.load()
        st.balance += msg.amount
        st.save()
      }

      Withdraw => {
        var st = lazy TreasuryState.load()
        st.balance -= msg.amount
        st.save()
      }

      else => {
        assert (in.body.isEmpty()) throw ERR_BAD_MSG
      }
    }
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func balance(): Coins {
    const st = lazy TreasuryState.load()
    return st.balance
  }
}
```

This example is valid only if the storage schema, selectors, serialization
rules, and ABI commitments are canonical and collision-free. The legacy
declaration surface (`storage TreasuryState` without a colon, `message
internal ... selector = auto`, `getter`, `event`, `wallet action`) is
rejected by the compiler.
