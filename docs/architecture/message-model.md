# AVM Message Model

This document defines inbound and outbound message semantics for AVM.

## 1. Inbound Message Classes

### External

An external message originates outside the chain and is usually signed by a
wallet.

Rules:

- external messages MAY carry value;
- external messages MAY create a contract from attached StateInit;
- external messages MUST be authenticated by the transaction layer;
- external messages are not bounced as external traffic;
- unknown external selectors MUST be rejected deterministically.

### Internal

An internal message is produced by a contract or runtime component.

Rules:

- internal messages MAY transfer value;
- internal messages MAY call another contract;
- internal messages MAY carry a bounce flag;
- internal messages MUST be deterministic and ordered by consensus rules;
- internal messages MUST be matched by kind first and only then decoded
  against their canonical opcode or selector binding;
- if the canonical opcode or selector does not map to a declared typed
  payload, the runtime MUST reject the message before any handler body runs;
- unknown internal selectors MUST be rejected deterministically if there is no
  explicitly declared handler;
- empty top-up or no-op behavior MUST be explicit in source and ABI metadata.

Implementations MAY expose decoded envelope fields such as `value`,
`bounce`, `bounced`, `flags`, `opcode`, and `body` to internal handlers where
those fields exist in the canonical envelope, but the canonical ABI binding
for the message MUST be the source of truth for typed decoding.

Typed message-body schemas are declared as `@message(opcode) struct Name {
... }` in source. The compiler MUST use those bindings to build typed opcode ->
schema decode metadata, and tools MAY match decoded values through unions or
`match msg { ... }` rather than forcing raw segment parsing as the only path.
Named unions such as `type InternalMsg = Inc | Dec | Withdraw | SetTarget`
are the canonical way to group message-body schemas into a closed dispatch
family; exhaustive matching on that union MUST be enforced at compile time.

Message-handler names are fixed and reserved:

- `@external` handlers MUST use `onExternalMessage`;
- `@internal` handlers MUST use `onInternalMessage`;
- `@bounced` handlers MUST use `onBouncedMessage`;
- the reserved handler names MUST NOT be reused for ordinary helper functions
  or for handlers carrying a different annotation;
- compiler diagnostics MUST explain the required name when a handler annotation
  and function name do not match.

### Bounced

A bounced message is a synthetic inbound message created after a failed
bounceable internal message.

Rules:

- bounced messages MUST use the bounced handler if the contract defines one;
- bounced messages MUST not request another bounce;
- bounced messages MUST preserve the original message identity in payload form;
- bounced messages MUST be clearly marked so wallets and explorers can show
  failure provenance;
- bounced messages without a handler MUST terminate without creating a new
  bounce loop.

Bounced dispatch is a separate synthetic entrypoint domain and MUST NOT be
treated as a fallback branch of ordinary internal dispatch.

### Contract Creation

A contract-creation message is the canonical initial message for contract
creation.

Rules:

- contract-creation messages MUST include code identity and initial state data;
- contract-creation messages MAY be external or internal depending on the sender;
- contract-creation messages MUST compute the created address from StateInit;
- contract-creation messages MUST fail if the address already exists or the init is invalid.

In Aetralis source, contract creation is expressed through the canonical
example style rather than a dedicated keyword. The example contracts are the
canonical working syntax. Message body schemas are ordinary source symbols as
well; the canonical authoring style is to define them outside the contract
shell and bind them into the ABI by name, rather than treating the contract as
the only namespace.

Implementations MAY expose creation entrypoints through the conventional
`onCreate` or `init` naming style, but the ABI binding remains the source of
truth.

### Query / Getter

Query and getter calls are read-only.

Rules:

- they MUST NOT change state;
- they MUST NOT emit consensus messages;
- they SHOULD be repeatable from the same state root and input;
- they MAY be served off-chain by SDK, explorer, or wallet tooling.

Getters are declared as `@get func name(): T`; the selector is derived from
the canonical getter name and cannot be pinned explicitly. The getter kind in
the ABI remains the source of truth.

## 2. Outbound Message Classes

### Ordinary Message

A normal outbound message is an internal message that can call another account
or contract.

### Bounced Reply

A bounced reply is the canonical failure reply for a bounceable message.

### Refund

A refund returns remaining value to the sender or fee payer after a failure or
partial execution path.

### Contract Creation Message

A contract-creation message creates a new contract or account from a StateInit binding.

### Self-Message

A self-message is addressed to the same contract and is used for deferred work,
timeouts, or continuation handling.

### Builder DSL

`buildMessage({ ... })` is the canonical surface builder for outbound
messages.

Rules:

- it MUST be lowered by the compiler into the canonical ABI/runtime message
  envelope;
- it MUST support `bounce`, `amount`, `receiver`, `body`, and typed body payloads;
- typed body payloads MUST lower through the canonical message-body codec;
- it MUST NOT exist as an only-runtime API that bypasses compiler lowering;
- the lowered result MUST be ABI-stable and identical to the canonical runtime
  envelope representation.

The full set of accepted builder fields is: `bounce`, `amount`, `receiver`,
`body`, `opcode`, `queryId`, `stateInit`, `mode`, and `textComment`. Any other
key is a compile error.

### Text comment (memo)

`textComment` attaches a free-form UTF-8 memo to a message — the analogue of a
bank-transfer memo, for both simple value transfers and rich typed-body
messages.

- it is an ordinary string: any characters are allowed;
- **at most one `textComment` per message** — a message carries a single
  canonical memo so wallets and explorers have exactly one string to display;
- it is bounded to `MaxCommentBytes` (512 bytes). A longer comment is rejected;
- it is charged through the normal per-byte message fee: a larger comment costs
  more. It is otherwise inert — the runtime never interprets it;
- the comment is bound into the internal-message id alongside the other
  envelope fields, so a relayer cannot alter or forge the memo in flight.

Regular native value transfers carry their memo through the standard
transaction memo field, which the explorer surfaces the same way.

### Send mode semantics

- `SEND_DEFAULT`, `SEND_CARRY_REMAINDER`, `SEND_DRAIN_BALANCE`,
  `SEND_ESTIMATE_ONLY`, `SEND_FEE_FROM_BALANCE`, `SEND_IGNORE_ERRORS`,
  `SEND_BOUNCE_ON_FAIL`, and `SEND_DESTROY_IF_EMPTY` are the canonical mode
  constants for send lowering;
- they are typed surface constants, not an ad hoc enum namespace;
- the compiler MUST translate the selected mode into the runtime send flags
  before ABI hashing and execution;
- message sends without an explicit mode default to `SEND_DEFAULT`.

Modes combine additively — `mode: SEND_DRAIN_BALANCE + SEND_DESTROY_IF_EMPTY`
is a single bitmask the compiler folds at build time. The combination must be
logically valid; the compiler rejects illogical ones:

- `SEND_DRAIN_BALANCE` and `SEND_CARRY_REMAINDER` are **mutually exclusive**
  (both decide the outgoing value — you cannot ask for both);
- `SEND_ESTIMATE_ONLY` **cannot be combined** with any other flag (it is a
  dry-run that must not actually send);
- a flag may not be repeated.

Effect of the balance-affecting modes:

- `SEND_DRAIN_BALANCE` — send the source contract's **entire** remaining
  balance (its `amount` is ignored; the runtime tracks a running balance so a
  batch of drains never over-allocates). This is the withdraw-everything
  primitive; to keep a reserve, send an explicit `amount` with the default mode
  instead.
- `SEND_DESTROY_IF_EMPTY` — after the send debit, if the source balance reached
  zero, the source contract is **deactivated irreversibly**: status becomes
  `deleted`, storage is cleared, and it holds no balance.
- `SEND_IGNORE_ERRORS` — if delivery of this message fails, drop it instead of
  retrying it every block.

**Withdraw-all-and-self-destruct idiom.** Combine the two to empty a contract
to a payout address and retire it in one message:

```atlx
const out = buildMessage({
    bounce: false,
    amount: 0,
    receiver: st.beneficiary,
    mode: SEND_DRAIN_BALANCE + SEND_DESTROY_IF_EMPTY,
    textComment: "vault closed",
    body: Payout {}
})
out.send()
```

The address then reports the `deleted` status; a fresh (never-deployed but
derivable) address reports `uninit`, and an unknown address reports
`nonexistent`, so a contract's lifecycle is observable end to end as
`uninit -> active -> frozen -> archived -> deleted` (plus `nonexistent`).

## 3. Bounce Semantics

Rules:

- A message is bounceable only if its envelope sets `bounce = true`.
- External messages are never bounced.
- A bounced message MUST be generated only after a failed bounceable inbound
  internal message.
- If the target contract defines a bounced handler, the runtime MUST dispatch
  the bounced payload to that handler.
- If no bounced handler exists, the runtime MUST stop after emitting the
  failure receipt and any eligible refund.
- Bounce processing MUST not recurse forever.
- Bounce handling MUST not create a new bounceable message for the same failure.

If the contract defines a dedicated bounced handler, implementations MAY map
it to a conventional `onBounced` entrypoint name, but the bounced kind in the
ABI remains authoritative.

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

### Contract Creation

```text
external creation:
  sender = AEwallet
  destination = contract address derived from StateInit
  state_init = { code_hash, init_data, salt, chain_id, namespace }
  result = create contract, run init, emit creation receipt
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
  output = uint64 balance
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
- contract creation state init summary;
- getter read-only status;
- refund reason and amount.

If the source or domain does not match the expected dApp binding, the request
MUST be marked as a mismatch before signing.
