# AVM Message Model

This document defines inbound and outbound message semantics for AVM.

## 1. Inbound Message Classes

### External

An external message originates outside the chain and is usually signed by a
wallet.

Rules:

- external messages MAY carry value;
- external messages MAY deploy a contract;
- external messages MUST be authenticated by the transaction layer;
- external messages are not bounced as external traffic.

### Internal

An internal message is produced by a contract or runtime component.

Rules:

- internal messages MAY transfer value;
- internal messages MAY call another contract;
- internal messages MAY carry a bounce flag;
- internal messages MUST be deterministic and ordered by consensus rules.

### Bounced

A bounced message is a synthetic inbound message created after a failed
bounceable internal or deploy message.

Rules:

- bounced messages MUST use the bounced handler if the contract defines one;
- bounced messages MUST not request another bounce;
- bounced messages MUST preserve the original message identity in payload form;
- bounced messages MUST be clearly marked so wallets and explorers can show
  failure provenance.

### Deploy

A deploy message is the canonical initial message for contract creation.

Rules:

- deploy messages MUST include code identity and initial state data;
- deploy messages MAY be external or internal depending on the sender;
- deploy messages MUST compute the deployed address from StateInit;
- deploy messages MUST fail if the address already exists or the init is
  invalid.

### Query / Getter

Query and getter calls are read-only.

Rules:

- they MUST NOT change state;
- they MUST NOT emit consensus messages;
- they SHOULD be repeatable from the same state root and input;
- they MAY be served off-chain by SDK, explorer, or wallet tooling.

## 2. Outbound Message Classes

### Ordinary Message

A normal outbound message is an internal message that can call another account
or contract.

### Bounced Reply

A bounced reply is the canonical failure reply for a bounceable message.

### Refund

A refund returns remaining value to the sender or fee payer after a failure or
partial execution path.

### Deploy Message

A deploy message creates a new contract or account from a StateInit binding.

### Self-Message

A self-message is addressed to the same contract and is used for deferred work,
timeouts, or continuation handling.

## 3. Bounce Semantics

Rules:

- A message is bounceable only if its envelope sets `bounce = true`.
- External messages are never bounced.
- A bounced message MUST be generated only after a failed bounceable inbound
  internal or deploy message.
- If the target contract defines a bounced handler, the runtime MUST dispatch
  the bounced payload to that handler.
- If no bounced handler exists, the runtime MUST stop after emitting the
  failure receipt and any eligible refund.
- Bounce processing MUST not recurse forever.
- Bounce handling MUST not create a new bounceable message for the same failure.

## 4. Canonical Bounced Payload

The bounced body MUST encode:

- original message id;
- original sender and destination;
- original opcode or selector;
- original query id;
- failure class;
- failure code;
- failure reason hash or bytes;
- original body bytes.

The bounced envelope MUST set:

- `kind = bounced`
- `bounced = true`
- `bounce = false`

## 5. Examples

### Deploy

```text
external deploy:
  sender = AEwallet
  destination = contract address derived from StateInit
  state_init = { code_hash, init_data, salt, chain_id, namespace }
  result = deploy contract, run init, emit deploy receipt
```

### Internal Message

```text
internal call:
  sender = AEcontractA
  destination = AEcontractB
  opcode = transfer
  body = canonical transfer payload
  result = state update or bounce
```

### Bounced Message

```text
bounced reply:
  sender = AEcontractB
  destination = AEcontractA
  kind = bounced
  bounced = true
  body = original message id + failure code + original body
```

### Getter

```text
getter call:
  selector = get_balance
  input = account address
  output = u64 balance
  side effects = none
```

### Refund

```text
refund:
  sender = runtime
  destination = AEwallet
  value = remaining_value_after_failure
  body = refund reason
```

## 6. Wallet Visibility

Wallets and explorers MUST display:

- message kind;
- sender and destination;
- bounce flag;
- bounced provenance;
- deploy state init summary;
- getter read-only status;
- refund reason and amount.

If the source or domain does not match the expected dApp binding, the request
MUST be marked as a mismatch before signing.
