# Aetralis Syntax Highlighting Guide

This document defines the canonical highlighting model for `.atlx` sources.
It matches the shipped VS Code extension in `ecosystem/extension` exactly and
is the single source of truth for token groups and colors.

Design rules:

- colors are fixed and theme-independent: the extension pins every token color
  via `configurationDefaults` token color rules scoped to unique `*.atlx`
  TextMate scopes, so switching the VS Code theme never changes `.atlx` colors;
- the grammar is fully declarative and backtracking-safe: no runtime code, no
  activation events, every regex is bounded and linear;
- only the canonical language surface is highlighted. Words that are not part
  of the language get no special treatment.

## Not part of the language

These words MUST NOT appear in any keyword list, grammar, or completion set.
The only top-level unit form is `import`; handlers are declared only through
annotations (`@internal func onInternalMessage(...)`, `@external func
onExternalMessage(...)`, `@bounced func onBouncedMessage(...)`):

- `package`
- `migrate`
- `selector`
- `message external` / `message internal` / `message bounced` declaration forms
- `let`, `val`, `mut` (the compiler rejects them; only `const` and `var` exist)
- `slice`, `cell`, `isSlice`, `isSliceSignatureValid`, `Ref<T>`

## Token Groups And Colors

### Declaration keywords — `#C792EA`

`import` `contract` `struct` `enum` `type` `func`

`getter`, `event`, `wallet`, `action` are NOT keywords — they are plain
identifiers (e.g. `const wallet = ...`). Getters are declared with
`@get func name(): T`, not with a `getter` keyword.

### Annotations — `#E5B567`, bold italic

`@internal` `@external` `@bounced` `@get` `@pure` `@impure` `@storage` `@message` `@store`

Handler rules (enforced by the compiler and mirrored by extension
diagnostics):

- `@internal`, `@external`, `@bounced` may each be declared at most once per
  contract;
- their functions have reserved names: `onInternalMessage`,
  `onExternalMessage`, `onBouncedMessage`;
- no other function may use those names;
- canonical signatures: `func onInternalMessage(in: InMessage)`,
  `func onExternalMessage(inMsg: Segment)`,
  `func onBouncedMessage(in: InMessageBounced)`.

### Reserved handler names — `#DCDCAA`, bold

`onInternalMessage` `onExternalMessage` `onBouncedMessage`

Contract metadata keys (when followed by `:`) share this color:
`author` `description` `version` `storage` `incomingMessages` `incomingExternal`

### Control flow and aborts — `#E06C75`

`if` `else` `while` `do` `repeat` `for` `in` `match` `break` `continue` `return` `assert` `throw`

### Bindings and mutation — `#E5C07B`

`const` `var` `lazy` `mutate` `set` `self`

### Runtime builtins — `#56B6C2`

Builtin namespaces before a dot: `contract` `random`

Builtin calls: `buildMessage` `counterfactualAddress` `autoDeployAddress`
`send` `refund` `emit` `now` `getBalance` `random` `getAddress`
`getOriginalBalance` `getAttachedValue` `logicalTime`
`currentBlockLogicalTime` `setCodePostponed` `isSignatureValid`
`isSegmentSignatureValid` `aet` `getData` `setData` `fromChunk` `toChunk`
`fromSegment` `fromHex` `fromBase64` `hash` `bitsHash` `skipBouncedPrefix`
`isEmpty` `uint256` `range` `getSeed` `setSeed` `initialize` `initializeBy`

### Scalar types — `#61AFEF`

`bool` `coins` `address` `bytes` `string` `timestamp` `hash` `hash32`
`u8`–`u256`, `i8`–`i256`, `uint8`–`uint256`, `int8`–`int256`
(capitalized spec aliases `Coins` `Address` `Timestamp` `Hash` `Bytes` included)

### Container and builtin object types — `#4FC1FF`

`Chunk` `Segment` `ChunkCursor` `ChunkRef` `ChunkLink` `Option` `Result`
`List` `Map` `Code` `StateInit` `InMessage` `InMessageBounced` `BounceMode`

User-defined type names (any `UpperCamelCase` identifier: contracts, structs,
enums, type aliases, match arms) share this color.

### User functions and methods — `#DCDCAA`

Any `lowerCamelCase` identifier directly before `(`, plus names in
`func Name(...)` and `func Type.method(...)` declarations.

### Literals and constants — `#D19A66`

`true` `false` `null`, decimal and `0x` numbers, `SEND_*` send modes,
and any `SCREAMING_CASE` constant (`ERR_NOT_OWNER`, ...).

Send modes: `SEND_DEFAULT` `SEND_CARRY_REMAINDER` `SEND_DRAIN_BALANCE`
`SEND_ESTIMATE_ONLY` `SEND_FEE_FROM_BALANCE` `SEND_IGNORE_ERRORS`
`SEND_BOUNCE_ON_FAIL` `SEND_DESTROY_IF_EMPTY`

### Strings — `#C9A575`

Double-quoted strings; escape sequences (`\n` `\t` `\r` `\"` `\\`) use `#E0BE8C`.

### Comments — `#7A9D54`

Line comments `//` and block comments `/* ... */`.

### Operators, punctuation, plain identifiers — `#ABB2BF`

All operators (`+ - * / % == != <= >= <=> && || ?? ! ? -> => << >> & | ^ ~`),
punctuation (`{ } ( ) [ ] , : ; .`), parameters, local variables, and
struct fields.

## Palette Summary

| Group | Color |
|---|---|
| Declarations | `#C792EA` |
| Annotations (bold italic), contract meta | `#E5B567` |
| Reserved handler names (bold) | `#DCDCAA` |
| Control flow, aborts | `#E06C75` |
| Bindings (`const`/`var`/`lazy`/`mutate`/`set`/`self`) | `#E5C07B` |
| Runtime builtins | `#56B6C2` |
| Scalar types | `#61AFEF` |
| Container + user types | `#4FC1FF` |
| User functions | `#DCDCAA` |
| Literals, constants | `#D19A66` |
| Strings | `#C9A575` |
| Comments | `#7A9D54` |
| Operators, punctuation, identifiers | `#ABB2BF` |
