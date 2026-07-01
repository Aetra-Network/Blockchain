# AVM ABI Specification

This document defines the canonical ABI package for AVM contracts.
The ABI is the contract between compiler, runtime, wallet, explorer, SDK,
indexer, and verifier.

## 1. ABI Contents

An ABI package MUST contain:

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

Any change in selector text, field order, field type, return type, or action
metadata MUST change the corresponding hash.

## 4. Versioning

Rules:

- ABI versions are monotonically increasing integers.
- Backward-compatible additions MAY keep the same major ABI family but MUST
  add new versioned descriptors.
- Breaking changes MUST produce a new ABI version and a new ABI hash.
- Runtime MUST reject ABI packages whose declared version is unsupported.

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

## 6. Example ABI Record

```json
{
  "contract": "Treasury",
  "abi_version": 1,
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
