# AVM Selector Registry

This document defines the canonical registry for selectors, opcodes, event
topics, getter selectors, and async handler selectors.

## 1. Selector Domains

Selectors are namespaced by kind:

- `method`
- `getter`
- `event`
- `async_handler`

The same text MAY appear in different domains, but the `(kind, selector)` pair
MUST be unique inside one ABI package.

## 2. Canonical Selector Text

Canonical selector text is:

```text
<kind>:<contract_or_interface>:<name>(<canonical_parameter_types>)-><canonical_return_types>
```

Rules:

- the kind token is mandatory;
- contract or interface name is mandatory;
- parameter and return types are canonical type names;
- whitespace is forbidden inside the canonical selector text;
- changing any part of the signature changes the selector.

## 3. Opcode And Topic Derivation

Selector IDs are derived from canonical selector text.

Rules:

- `selector_id` = low 32 bits of `blake3(canonical_selector_text)`;
- `opcode` = `selector_id` for message dispatch;
- `event_topic` = `sha256("event:" + canonical_selector_text)` unless a schema
  hash is also required, in which case the schema hash is appended before
  hashing;
- `async_handler_selector` uses the `async_handler` domain;
- `getter_selector` uses the `getter` domain.

Implementations MUST preserve the full selector text even when using the 32-bit
ID as a fast dispatch key.

## 4. Uniqueness And Collision Handling

Rules:

- The registry MUST reject duplicate canonical selector text.
- The registry MUST reject duplicate selector IDs inside the same ABI package.
- If two different selector texts map to the same 32-bit selector ID, the ABI
  package MUST be rejected.
- Event topics MUST be unique per event schema.
- A collision is a hard error, not a warning.

## 5. Registry Records

Each registry entry MUST include:

- `kind`
- `contract_or_interface`
- `name`
- `canonical_selector_text`
- `selector_id`
- `schema_hash`
- `version`
- `deprecated`
- `replaced_by_optional`

Deprecated records MAY remain in the registry, but they MUST NOT be callable
unless the ABI explicitly keeps them active.

## 6. Example Registry

```text
method:Treasury:transfer(Address,u64)->()
  selector_id = 0x3d2e5f8b1c22a144
  schema_hash = sha256:...

getter:Treasury:get_balance(Address)->u64
  selector_id = 0x90ab12c4fe88d0f1
  schema_hash = sha256:...

event:Treasury:transfer_recorded(Address,Address,u64)
  topic = sha256:...
```

## 7. Runtime Rules

Runtime MUST:

- dispatch by selector kind first;
- verify the selector text against the ABI package before execution;
- fail closed on unknown selectors;
- prefer the canonical text over numeric aliases when both are available;
- surface the registry to wallets, explorers, and SDK generators.

## 8. Wallet And Explorer Use

Wallets and explorers MAY use the registry to:

- render action labels;
- derive form schemas;
- show event names;
- map selectors to high-level documentation;
- warn about deprecated or replaced actions.
