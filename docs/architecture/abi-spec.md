# AVM ABI Specification

This document defines the canonical ABI package for AVM contracts.
The ABI is the contract between compiler, runtime, wallet, explorer, SDK,
indexer, and verifier.

## 0. Language Binding

The canonical public language name is Aetralis.
The stable source extension is `.atlx`.
ABI packages MUST record the language binding explicitly so that the runtime
can distinguish language-level compatibility from ABI-level compatibility.
Top-level helper functions remain source-level symbols and do not become ABI
entrypoints; only contract-bound message, getter, async handler, and wallet
descriptors are part of the ABI dispatch surface.

## 1. ABI Contents

An ABI package MUST contain:

- language name;
- language track;
- language version;
- migration version;
- source extension;
- artifact layout version;
- dependency lock commitment;
- contract identity;
- module/code hash commitment;
- storage schema commitment;
- message descriptors;
- getter descriptors;
- event descriptors;
- async handler descriptors;
- wallet action descriptors;
- selector registry entries;
- serialization version;
- compatibility version.
- unknown-message policy.
- contract human metadata record, if emitted by tooling as a separate
  informational artifact.

If a deployment pipeline emits an attestation record, that record is governed
by the separate [deployment pipeline spec](deployment-pipeline-spec.md) and is
optional, versioned, and hash-committed.

## 2. Descriptor Rules

Each descriptor MUST be canonicalized before hashing.

Rules:

- names MUST be ASCII and stable;
- fields MUST be listed in declaration order;
- nested descriptor lists MUST be sorted canonically by their selector or name;
- optional metadata MUST be serialized explicitly, not inferred;
- descriptors with the same semantic meaning MUST hash identically.

## 3. ABI Hashes

The ABI package MUST publish stable commitments:

- `module_hash` or `code_hash`: canonical code bytes commitment.
- `abi_hash`: entire ABI commitment.
- `method_hash`: method descriptor commitment.
- `event_hash`: event descriptor commitment.
- `getter_hash`: getter descriptor commitment.
- `async_handler_hash`: async handler descriptor commitment.
- `wallet_action_hash`: wallet metadata commitment.
- `unknown_message_policy_hash`: unknown-message policy commitment.

Any change in selector text, field order, field type, return type, or action
metadata MUST change the corresponding hash.

Contract metadata rules:

- `author`, `description`, and `version` are human metadata only;
- they MUST be serialized canonically in the order `author`, `description`,
  `version` when present;
- they MUST NOT affect ABI hash, selector derivation, storage layout, or
  StateInit commitments;
- if a separate metadata hash is published, it MUST be derived only from the
  canonical metadata record and MUST remain informational rather than ABI
  binding.

Deployment attestation boundary:

- deployment attestation is not hidden consensus behavior;
- mock wallets, static checks, safety profiles, and attestation hashes belong
  to the deployment pipeline, not to the ABI runtime contract model;
- the chain stores the code hash, ABI hash, storage schema hash, safety
  profile hash, and attestation hash as distinct commitments;
- any other deployment data remains off-chain pipeline metadata.

## 4. Versioning

Rules:

- ABI versions are monotonically increasing integers.
- ABI version, language version, migration version, and artifact layout
  version are independently versioned but cross-linked.
- Backward-compatible additions MAY keep the same major ABI family but MUST
  add new versioned descriptors and update the relevant commitments.
- Breaking changes MUST produce a new ABI version and a new ABI hash.
- Renames of public language terms are language-version changes and MUST be
  treated as breaking unless they are purely deprecated aliases in the
  compatibility layer.
- Unknown-message policy MUST be recorded in ABI metadata and hashed as part
  of the ABI package.
- Runtime MUST reject ABI packages whose declared version is unsupported or
  whose language binding does not match the supported track.

## 5. Wallet Metadata

Wallet metadata is ABI data, not local policy.

Required fields per wallet action:

- `title`
- `risk`
- `confirm_label`
- `warning_level`
- `expected_side_effects`
- `fund_access`
- `approval_semantics`

Wallets MUST render the exact metadata provided by the ABI or by a signed
attestation.

Unknown-message policy rules:

- the default policy is reject;
- explicit no-op behavior MUST be declared in source and ABI metadata;
- bounced messages without a handler MUST not create a bounce loop;
- dispatch MUST be kind-aware, and internal payloads MUST be decoded through
  the canonical typed descriptor before handler execution;
- unknown or ambiguous selector/opcode bindings MUST fail closed before any
  partial execution;
- runtime behavior MUST match the ABI-declared policy exactly.

## 6. Example ABI Record

```json
{
  "contract": "Treasury",
  "language_name": "Aetralis",
  "language_track": "v1",
  "abi_version": 1,
  "migration_version": 1,
  "source_extension": ".atlx",
  "artifact_layout_version": 1,
  "dependency_lock_hash": "d1c0...be",
  "unknown_message_policy": "reject",
  "code_hash": "b3f4...c1",
  "abi_hash": "9b8a...10",
  "messages": [
    {
      "kind": "external",
      "selector": "transfer",
      "input": "hash:amount-address",
      "output": "hash:empty",
      "state_mutation": true
    }
  ],
  "getters": [
    {
      "selector": "get_balance",
      "output": "hash:u64",
      "read_only": true
    }
  ],
  "events": [
    {
      "selector": "transfer_recorded",
      "topic": "sha256:..."
    }
  ],
  "wallet_actions": [
    {
      "title": "Transfer funds",
      "risk": "high",
      "confirm_label": "Send treasury funds",
      "warning_level": "warn",
      "expected_side_effects": ["bank transfer", "state write"],
      "fund_access": true,
      "approval_semantics": "spend"
    }
  ]
}
```

## 7. ABI Validation

An ABI package MUST be rejected if:

- two descriptors share the same canonical selector in the same namespace;
- a descriptor hash does not match its canonical form;
- a getter is writable;
- a wallet action lacks any required field;
- a selector registry entry collides with another entry in the same ABI.

## 8. Runtime Contract

Runtime MUST:

- load ABI commitments before dispatch;
- route by selector and message kind;
- refuse ambiguous selectors;
- expose getter descriptors to explorer and SDK clients;
- expose wallet action metadata to wallet clients;
- preserve the exact descriptor hash in all proofs, exports, and attestations.
