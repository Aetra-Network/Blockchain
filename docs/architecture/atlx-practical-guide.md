# ATLX Practical Guide

This document is a practical writing guide for `.atlx` smart contracts that
target AVM. It is intentionally narrower and more operational than the formal
language specification.

The `.atlx` example files in `examples/avm` are the working standard for
syntax. If a form is not shown there, treat it as non-canonical.

Use this guide for:

- everyday contract authoring;
- canonical handler naming rules;
- local coding rules for `const`, `var`, `assert`, `throw`, `lazy`, and
  `mutate`;
- common contract structure and storage/message patterns;
- current implementation expectations for AVM-facing source.

The formal source of truth for stable surface grammar remains
[`language-spec.md`](./language-spec.md). This guide documents the intended
authoring style and current project rules.

## Core Model

ATLX source compiles to AVM contracts.

- `contract` is the top-level contract container.
- `struct` defines storage or message bodies.
- `type` defines an alias or a union type.
- `func` defines a function or handler.
- `const` defines an immutable value.
- `var` defines a mutable local value.
- `self` is the current receiver in a method.
- `mutate` allows mutation of a receiver or argument.
- `lazy` requests deferred decoding/reading.
- `null` means absence of value.
- `throw` aborts execution with an exit code.
- `assert (cond) throw code` is the canonical short validation form.

## Imports

Use `import` to split project source across files.

Examples:

```atlx
import "constants.atlx"
import "structs/messages.atlx"
```

Project-local imports are resolved relative to the compilation root when the
compiler is launched from a file path, and imported files may themselves
import other project files transitively. Package-style imports with explicit
version strings remain supported for dependency-locked reuse.

Practical rules:

- use `.atlx` file paths for intra-project reuse;
- keep shared constants, messages, and helper functions in separate files;
- prefer relative project paths over hardcoded code duplication;
- use package/version imports only for external or version-pinned reuse.

## Declaration Rules

### Annotations

Exactly one annotation is allowed per function or struct declaration.

Allowed struct annotations:

- `@storage`
- `@message(opcode)`

Allowed function annotations:

- `@internal`
- `@external`
- `@bounced`
- `@get`
- `@pure`
- `@impure`
- `@store`

Invalid combinations are rejected. In particular:

- multiple annotations on one declaration are rejected;
- `@storage` and `@message(...)` must not be combined.

### Reserved Handler Names

AVM message handlers use reserved names.

Internal handler:

```atlx
@internal
func onInternalMessage(in: InMessage) {
    // ...
}
```

Bounced handler:

```atlx
@bounced
func onBouncedMessage(in: InMessageBounced) {
    // ...
}
```

External handler:

```atlx
@external
func onExternalMessage(inMsg: Segment) {
    // ...
}
```

These names are reserved and must not be reused for unrelated functions.

## Variables and Constants

Only two local binding forms are allowed:

Immutable:

```atlx
const x = 1
```

Mutable:

```atlx
var x = 1
x += 1
```

## Types

Common style:

- `uint32`
- `uint64`
- `uint256`
- `int64`
- `coins`
- `address`
- `bool`
- `Segment`
- `Chunk<T>`
- `Code`

Nullable types use `?`:

```atlx
target: address?
packed: Chunk<PackedState>?
```

Non-null assertion uses `!`:

```atlx
st.target!
```

## Structs

Storage structs:

```atlx
@storage
struct Storage {
    counter: int64
    owner: address
    target: address?
}
```

Message structs:

```atlx
@message(0x1001)
struct Inc {
    by: uint32
}
```

## Type Aliases and Unions

Aliases:

```atlx
type CounterValue = int64
type PackedState = Packed
```

Unions:

```atlx
type InternalMsg = Inc | Dec | Withdraw | SetTarget
type ExternalMsg = Touch
```

Use unions for internal and external decoded message families.

## Contract Layout

A typical contract contains:

- optional metadata fields;
- optional `storage`;
- optional `incomingMessages`;
- optional `incomingExternal`;
- helper functions;
- handlers;
- getters.

Example:

```atlx
contract Counter {
    author: "some author"
    description: "some text"
    version: "0.01.2"

    storage: Storage
    incomingMessages: InternalMsg
    incomingExternal: ExternalMsg
}
```

Practical rule:

- a contract may omit storage if it is stateless;
- a contract should expose at least one inbound surface:
  `incomingMessages`, `incomingExternal`, or both.

## Storage Helpers

Recommended pattern:

```atlx
@store
func StorageType.load() {
    return StorageType.fromChunk(contract.getData())
}

@store
func StorageType.save(self) {
    contract.setData(self.toChunk())
}
```

State-touch helper:

```atlx
@impure
func Storage.touch(mutate self) {
    self.lastNow = now()
    self.lastBalance = getBalance()
    self.lastRandom = random()
}
```

Practical rule:

- load storage once near the start of a handler;
- mutate a local `var st`;
- save once at the end of the branch where possible.
- the receiver name in `StorageType.load()` / `save()` is just an example;
  any user-defined storage struct name is valid and no name is reserved by the
  language.

## Functions

Pure helper:

```atlx
@pure
func Packed.fromState(counter: CounterValue, at: int64) {
    return Packed {
        counter: counter,
        at: at,
    }
}
```

Mutable helper:

```atlx
@impure
func inc(mutate x: int64) {
    x += 1
}
```

Purity rule:

- `@pure` functions must not write storage or cause chain-visible side effects;
- `@impure` functions may write storage, send messages, or otherwise mutate
  contract-visible state.

## Match

Use `match` to decode unions and enums.

```atlx
match (msg) {
    Inc => { ... }
    Dec => { ... }
    else => { ... }
}
```

Practical rule:

- for inbound message unions, include an `else` branch unless the match is
  intentionally exhaustive and type-safe.

## Assert and Throw

Canonical short validation form:

```atlx
assert (in.senderAddress == st.owner) throw ERR_NOT_OWNER
assert (msg.seqno == st.seqno) throw 401
assert (msg.validUntil > now()) throw 408
```

Direct abort:

```atlx
throw ERR_BAD_NONCE
throw 403
throw 0xFFFF
```

Current rule:

- `assert` conditions are expected to be boolean expressions;
- `throw` carries an exit code.

## Boolean Rules

Boolean literals:

```atlx
true
false
```

Boolean logic:

```atlx
!a
a && b
a || b
```

Comparison operators:

```atlx
a == b
a != b
a < b
a > b
a <= b
a >= b
```

Practical rule:

- `bool` is not implied from integers;
- do not write `if (x)` for numeric `x`;
- write `if (x != 0)` when that is the intended meaning.

## Arithmetic and Assignment Style

Preferred arithmetic:

```atlx
a + b
a - b
a * b
a / b
a % b
```

Mutation style:

```atlx
var i = 0
i += 1
i -= 1
```

Do not use:

```atlx
i++
i--
```

## Nullable Values

Check before unwrap:

```atlx
if (st.target == null) {
    throw ERR_NO_TARGET
}

const dest = st.target!
```

Typical pattern:

```atlx
if (st.target != null) {
    const dest = st.target!
    // ...
}
```

## Internal Messages

Canonical structure:

```atlx
@internal
func onInternalMessage(in: InMessage) {
    const msg = lazy InternalMsg.fromSegment(in.body)

    match (msg) {
        Inc => {
            var st = lazy StorageType.load()
            st.counter += msg.by
            st.save()
        }
        else => {
            assert (in.body.isEmpty()) throw ERR_BAD_MSG
        }
    }
}
```

Typical internal fields:

- `in.body`
- `in.senderAddress`
- `in.valueCoins`
- `in.originalForwardFee`

## Bounced Messages

Canonical structure:

```atlx
@bounced
func onBouncedMessage(in: InMessageBounced) {
    in.bouncedBody.skipBouncedPrefix()

    const bounced = lazy Ping.fromSegment(in.bouncedBody)

    var st = lazy StorageType.load()
    if (st.pingTicket != null && st.pingTicket == bounced.ticket) {
        st.pingTicket = null
        st.save()
    }
}
```

## External Messages

Canonical structure:

```atlx
@external
func onExternalMessage(inMsg: Segment) {
    const msg = lazy ExternalMsg.fromSegment(inMsg)

    match (msg) {
        Touch => {
            var st = lazy StorageType.load()
            // ...
            st.save()
        }
        else => {
            assert (inMsg.isEmpty()) throw ERR_BAD_MSG
        }
    }
}
```

Practical rule:

- external flows should usually validate `nonce`, signature, or expiry;
- do not assume internal-style sender/value helpers exist on the external body
  segment surface.

## Getters

Canonical getter:

```atlx
@get
func currentCounter(): CounterValue {
    const st = lazy StorageType.load()
    return st.counter
}
```

Getter rule:

- getters must not mutate storage;
- getters must not send messages;
- getters should only read state and compute a return value.

## Chunk and Segment

`Chunk<T>` is the chunk-backed structured value/reference form.

`Segment` is the inbound body-reading form.

Common usage:

```atlx
Packed.fromChunk(st.packed!)
Ping.fromSegment(in.body)
```

## Messages and Sending

Message construction:

```atlx
const ping = buildMessage({
    bounce: BounceMode.Only256BitsOfBody,
    amount: 0,
    receiver: st.target!,
    body: Ping {
        ticket: msg.ticket,
        counter: st.counter,
    }
})
```

Sending:

```atlx
ping.send(SEND_BOUNCE_ON_FAIL)
```

Recommended send mode names:

- `SEND_DEFAULT`
- `SEND_CARRY_REMAINDER`
- `SEND_DRAIN_BALANCE`
- `SEND_ESTIMATE_ONLY`
- `SEND_FEE_FROM_BALANCE`
- `SEND_IGNORE_ERRORS`
- `SEND_BOUNCE_ON_FAIL`
- `SEND_DESTROY_IF_EMPTY`

Modes combine with `+`, and you can set the mode inside `buildMessage` via the
`mode:` field instead of passing it to `.send(...)`:

```atlx
const out = buildMessage({
    bounce: false,
    amount: 0,
    receiver: st.beneficiary,
    mode: SEND_DRAIN_BALANCE + SEND_DESTROY_IF_EMPTY,
    textComment: "vault closed",
    body: Payout {}
})
out.send(SEND_DEFAULT)
```

`SEND_DRAIN_BALANCE` sends the whole balance (withdraw everything); to keep a
reserve, use the default mode with an explicit `amount`. `SEND_DESTROY_IF_EMPTY`
retires the contract once it hits zero (status `deleted`, storage cleared). The
compiler rejects illogical combinations: `SEND_DRAIN_BALANCE +
SEND_CARRY_REMAINDER` (mutually exclusive) and any combination involving
`SEND_ESTIMATE_ONLY` (must be sent alone).

`textComment` is the message memo: one free-form string per message (any
characters, up to 512 bytes, priced per byte). See `message-model.md` for the
full semantics and the withdraw-all-and-self-destruct idiom.

## Runtime Builtins

Common runtime helpers used in this source style:

- `contract.getData()`
- `contract.setData(...)`
- `now()`
- `getBalance()`
- `random()`

Target runtime surface that should be treated as part of the intended language
direction:

- `getAddress()`
- `getOriginalBalance()`
- `Code.fromChunk(...)`
- `Code.fromHex(...)`
- `Code.fromBase64(...)`
- `Code.toChunk(...)`
- `Code.hash()`
- `setCodePostponed(newCode)`
- `logicalTime()`
- `currentBlockLogicalTime()`
- `chunk.hash()`
- `segment.hash()`
- `segment.bitsHash()`
- `isSignatureValid(hash, signature, publicKey)`
- `isSegmentSignatureValid(dataSegment, signature, publicKey)`

`Code` is the canonical contract bytecode value. Prefer storing it as typed
data and building it with one of the constructors above instead of string
contract-name lookup.

## Compile-Time Helpers

Target compile-time helper style:

```atlx
const owner = address("AE...")
const fee = aet("1.5")
```

Intended meaning:

- `address("AE...")` embeds a canonical address constant;
- `aet("1.5")` converts a decimal amount into the chain base unit at compile
  time.

## Random

Target random surface:

- `random.uint256()`
- `random.range(limit)`
- `random.getSeed()`
- `random.setSeed(seed)`
- `random.initialize()`
- `random.initializeBy(x)`

Practical rule:

- initialization and seed mutation should be treated as impure;
- read-only random access should be documented clearly by the runtime if it is
  permitted inside pure contexts.

## Enums

Intended enum style:

```atlx
enum Err {
    NotOwner = 401,
    Expired
}
```

Use enums for:

- error families;
- mode/state values;
- protocol dispatch categories.

## Loops

The intended control-flow surface includes:

- `for`
- `while`
- `do`
- `repeat`

Practical rule:

- on-chain loops should always be bounded;
- avoid loops whose cost depends on unbounded inbound data.

## Recommended Contract Skeleton

```atlx
const ERR_NOT_OWNER = 1001
const ERR_BAD_MSG = 0xFFFF

@storage
struct StorageType {
    owner: address
    counter: int64
    nonce: uint32
}

@message(0x1001)
struct Inc {
    by: uint32
}

type InternalMsg = Inc

contract Counter {
    storage: StorageType
    incomingMessages: InternalMsg

    @store
    func StorageType.load() {
        return StorageType.fromChunk(contract.getData())
    }

    @store
    func StorageType.save(self) {
        contract.setData(self.toChunk())
    }

    @internal
    func onInternalMessage(in: InMessage) {
        const msg = lazy InternalMsg.fromSegment(in.body)

        match (msg) {
            Inc => {
                var st = lazy StorageType.load()
                assert (in.senderAddress == st.owner) throw ERR_NOT_OWNER
                st.counter += msg.by
                st.save()
            }
            else => {
                assert (in.body.isEmpty()) throw ERR_BAD_MSG
            }
        }
    }

    @bounced
    func onBouncedMessage(in: InMessageBounced) {
        return 0
    }

    @get
    func currentCounter(): int64 {
        const st = lazy StorageType.load()
        return st.counter
    }
}
```

## Authoring Rules Summary

- use only `const` and `var` for local bindings;
- use exactly one annotation per function or struct;
- use reserved handler names for internal, external, and bounced handlers
  inside each `contract {}` block;
- treat `assert (cond) throw code` as the standard validation form;
- treat `throw code` as the standard abort form;
- make boolean intent explicit;
- check nullable values before `!`;
- load storage once, mutate locally, save once where possible;
- keep `@pure` functions side-effect free;
- prefer bounded logic and deterministic decoding paths;
- use `match` for union/enum dispatch.

## Current Implementation Note

This guide describes the intended ATLX writing rules and project direction.
Some surface items are already implemented in parser/typechecking/tooling, while
others are still pending full AVM runtime lowering support. When in doubt:

- treat [`language-spec.md`](./language-spec.md) as the formal grammar source;
- treat the current compiler tests under `x/aetravm/compiler` as the executable
  reference for what compiles today;
- treat this guide as the canonical authoring style the project is moving
  toward.

## Runtime Status

The current AVM bridge should be treated as partially complete, not magical.

- `@internal`, `@bounced`, `@external`, `@get`, `@pure`, `@impure`, and
  `@store` are the only allowed declaration annotations.
- Reserved handler names remain fixed within each contract:
  `onInternalMessage(in: InMessage)`,
  `onBouncedMessage(in: InMessageBounced)`,
  `onExternalMessage(inMsg: Segment)`.
- `const` is immutable and `var` is mutable.
- `assert (cond) throw code` and `throw code` are the canonical control-flow
  failure forms.
- Queue-backed internal messages are stored explicitly in
  `genesis.State.InternalMessages`; delivery is a keeper/runtime action, not an
  implicit language feature.
- `OpEmitInternal` currently still depends on runtime destination plumbing, so
  send paths that need dynamic destinations are not yet fully general.
- Contract code should live in the `Code` type, which is chunk-backed and can
  be stored in state or locals without falling back to a raw hex string.
- `import "file.atlx"` is supported through the compiler file resolver and is
  the recommended way to split a project into constants, messages, helpers,
  and contract roots.
