# AVM call mechanism v5 — intra-contract calls, tuples, generics, static traits, read-only cross-contract calls

Status: **Implemented and verified for §1 (intra-contract CALL/RET), §2 (tuples), §3 (early return), and
§8 (`x/contracts` locking + state-root fix).** §4 (generics), §5 (traits beyond doc-level scoping), and §6
(read-only cross-contract calls) were deliberately **not implemented** this pass — designed here, left as
real follow-up work, not silently dropped (see each section's own scope statement and the "Explicitly
deferred" section at the end of this document). Supersedes the paused v1–v4 track
(`docs/architecture/avm-phase-ef-call-design.md`, all four rejected — read in
full before touching this doc) by **not attempting the thing that killed all
four rounds**. v1–v4 tried synchronous CROSS-CONTRACT calls with mutation,
atomic rollback across two contracts' storage, and shared gas metering across
a call tree that spans contract boundaries. Every blocker across all four
rounds — the v1/v2 storage-overlay lost-update, the v2/v3 eager `TransferValue`
value-conservation break, the v3 torn-flush/overflow bug, the v4
fund-annihilation via `committedBalanceSnapshot=0` assignment-not-addition —
is an instance of the same underlying problem: **atomically committing writes
to a second contract's storage/balance from inside a first contract's
execution, when the VM has no access to that second contract's real ledger
state.** This design doesn't solve that problem. It **removes the need to
solve it**, per the scope decision below, and spends the freed-up ambition on
capabilities that are real, safe, and achievable given this VM's actual
architecture today.

Scope executed here (all verified against the current tree, branch
`remediation/pass2-security`, not assumed from prior rounds' citations):

1. Real intra-contract CALL/RET — same contract, same transaction, same gas
   meter, never touches another contract's storage or balance.
2. Tuples: value representation (already exists), wire encoding (already
   works), destructuring syntax (new, additive grammar).
3. Early return / structured error propagation (falls out of #1 almost for
   free).
4. Generics via compile-time monomorphization, explicit type arguments only
   (no inference engine).
5. Static, monomorphized trait-bounded dispatch — scoped down from "traits"
   as commonly understood; no trait-typed values, no vtables.
6. Read-only synchronous cross-contract calls (`@get` only) — the one
   cross-contract mechanism that is safe specifically because it cannot
   write.
7. Cross-contract mutation stays async-only, permanently, argued as this
   design's own considered position, not an inherited constraint.
8. The independent `x/contracts` storage-locking and state-root fixes.
9. An adversarial self-check proving (not asserting) that nothing in this
   document has a cross-contract fund-movement path.

Every file:line citation below was read directly this session, not carried
from the investigation report handed to this task.

---

## 1. Intra-contract CALL/RET

### 1.0 The key architectural fact that makes this simple

`x/aetravm/compiler/compile.go` has **no codegen path for a plain helper
function today.** `tryInlineUserFunctionCall` (compile.go:7050-7109) is the
only mechanism, and it is pure AST substitution: `substituteExprForInline`
(compile.go:6951-7048) clones the callee's single return expression and
textually replaces each parameter with the caller's argument *expression*,
then lowers the result inside the caller's own environment. There is no
second `[]avm.Instruction` sequence anywhere for a user function — only
handler-annotated functions (`@deploy`/`@external`/etc., matched via
`functionHandlerAnnotation`, compile.go:3730-3752) get compiled to real
bytecode, and those are entrypoints, not call targets. This is why the
inliner is capped at "lazy storage bindings + one return expression" — it
structurally cannot represent branching, loops, or multiple statements,
because it never produces control-flow instructions, only one more IR
expression node spliced into the caller's tree.

`x/aetravm/avm/avm.go`'s `Runner.Run` (avm.go:873-2175) is a single flat
`for` loop over one `module.Code []Instruction` with one `pc uint32`, one
shared `stack []RuntimeValue` (890), one shared `locals []RuntimeValue`
(891, flat, slot-indexed — `OpLoadLocal`/`OpStoreLocal`, avm.go:1189-1224,
index directly into it, growing it in place), and `OpReturn`
(avm.go:2138-2156) **halts the entire `Run()` call**, cloning the top of
`stack` into `exec.ReturnValue` and returning — it is not a function return,
it is "the transaction is over."

The mechanism below adds a real call stack to `Run()` without touching any
of this existing behavior for code that doesn't use it: `OpReturn`'s
semantics are byte-for-byte unchanged, so every module compiled before this
lands keeps working identically.

**Multiple code regions already coexist in one flat array today**, which is
the pattern this design reuses rather than invents: `c.buildModule`
(compile.go:3544-3626) lowers each entrypoint's IR **independently** (each
call to `c.lowerIREntry` starts labels/jumps at 0, compile.go:4528-4616),
then appends the result to a shared `code []avm.Instruction` at a running
offset (`entryBase := uint32(len(code))`, compile.go:3566) and shifts that
entry's `OpJump`/`OpJumpIfZero` targets by `entryBase`
(`relocateJumpTargets`, compile.go:3628-3638). `Module.Exports` just maps an
`Entrypoint` ID to its region's starting offset (avm.go:369-375). Function
bodies get the identical treatment — they simply never need an `Exports`
entry, because they're never dispatched from outside, only jumped to via a
new opcode from inside the same module.

### 1.1 The insight that avoids a dynamic frame pointer

The task requires the call graph to be recursion-free (no direct or mutual
recursion — enforced at compile time, see §1.6 for why this must ALSO be
enforced at runtime). Given that, and given the interpreter is
single-threaded, **at most one invocation of any given function body is ever
live at once.** Two *different* functions can be simultaneously "open" (A
calls B: A is paused mid-body while B runs), but never two invocations of
the *same* function.

That means the compiler can statically assign each function body (each
monomorphized instantiation, see §4) its own **disjoint range of local
slots** in the one flat `locals []RuntimeValue` array, decided once at
compile time — exactly the same mechanism `loweringEnv.nextLocalSlot`
(compile.go:4627) already uses per-entry today, just no longer reset to 0
for every lowering call. A's locals sit in slots `[10,13)`, B's in `[13,15)`;
while B runs, A's slots are untouched (nothing else writes them), and when B
returns, A resumes and finds its own slots exactly as it left them. **No
frame-relative addressing, no frame-pointer register, and no per-call
locals allocation are needed at runtime** — the existing flat,
statically-slotted locals array already does the job, given the no-
recursion constraint. This is the concrete answer to "stack-based or a
dedicated frame-local area": neither, in the dynamic sense — a
**compile-time-partitioned area of the existing flat array**, which is
strictly simpler and has strictly fewer moving parts than either.

Concrete compiler change required: today every one of the 4 call sites that
build a fresh `loweringEnv{...}` for an entry/function/getter
(compile.go:3718, 3752, 3788, 3820) leaves `nextLocalSlot` at its Go zero
value, i.e. every entrypoint and handler-function starts at slot 0. That is
safe today only because at most one such region ever executes per `Run()`
call. Once a function's compiled body can be **reached by CALL from another
region's still-live locals**, slot ranges must stop colliding — so
`c.buildModule` needs one counter, threaded across all entrypoint AND
function lowering calls for the whole module (not reset per call), handing
each region its own starting slot offset before lowering it.

Locals slot count is already bounded today: `OpStoreLocal`'s bound check
reuses `MaxStackDepth` as the local-slot ceiling (avm.go:1206,
`ins.Arg >= uint64(r.params.MaxStackDepth)`, default 1024,
avm.go:455-457/471). Since this design makes slot ranges span the whole
module rather than one entry, that ceiling is now a whole-module budget
shared by every entrypoint's locals plus every function body's (plus every
monomorphized instantiation's, §4) locals combined. Stated honestly as a
real, if generous, budget — no new field needed, the existing check already
enforces it once slot allocation is module-wide.

### 1.2 New opcodes

Two new opcodes, in the free `0x63`–`0xef` range (last allocated is
`OpPoseidon2Bn254 = 0x62`, avm.go:331; `0xf0`–`0xf4` are the permanently
forbidden non-deterministic set, avm.go:333-337):

- **`OpCall`** (`Arg` = absolute target PC, an immediate operand resolved at
  **compile time**, exactly like `OpJump`'s `Arg` — never a runtime-computed
  or indirect target; no vtable, no function pointer value, nothing on the
  operand stack identifies the callee). Runtime behavior: push the return
  address (`pc+1`) onto a **new**, small, `Run()`-local slice (not part of
  `Storage`, `Execution`, or `RuntimeContext` — purely interpreter-local,
  discarded with everything else on any rollback path, see §1.4); check
  the new depth cap (§1.6); set `pc = ins.Arg` and `continue` (skip the
  loop's trailing `pc++`, exactly like `OpJump` does today at
  avm.go:1132-1137).
- **`OpRet`** (function return, distinct from `OpReturn` — `OpReturn`'s
  existing whole-execution-halt semantics are untouched and still used for
  every top-level `return` statement outside a called function). Runtime
  behavior: pop the return-address slice; if empty, this is a malformed
  bytecode stream (unreachable for compiler output, reachable only via raw
  adversarial `MsgStoreCode` bytecode — see §1.6 for why this is still
  bounded) — trap with `ResultExecutionFailed`; otherwise `pc = <popped
  address>` and `continue`. **The return value is not moved anywhere** — it
  is whatever `RuntimeValue` the callee left on top of the shared `stack`
  (pushed by evaluating the `return` statement's expression exactly as
  `OpReturn` already does at avm.go:2142-2150), and the caller finds it
  there immediately after the jump back, ready to consume like any other
  expression result. No argument-marshaling area, no separate return
  register.

Both need the identical "5 registration sites" every existing opcode has:
the `Opcode` constant, a `DefaultParams().GasSchedule` entry
(avm.go:466-648), an `IsAllowedOpcode` switch arm (avm.go:4637+), the `Run()`
case, and (`OpCall` only, mirroring `OpJump`/`OpJumpIfZero` at
avm.go:4781-4784) a `validateInstructionArg` bound check that `Arg` fits
`uint32`. No **new** `Verifier.Verify` pass is needed beyond that: call
targets get the same runtime range check `OpJump` already gets
(`ins.Arg >= uint64(len(module.Code))`, mirroring avm.go:1133), enforced in
the `OpCall` case, not at Verify time — consistent with how jump targets are
handled today, not a weaker guarantee introduced by this design.

### 1.3 Argument passing and parameter binding

Caller evaluates each argument expression left-to-right, pushing each onto
the shared `stack` (exactly how every existing operator/builtin call already
evaluates its operands) — no new codegen shape here, this reuses whatever
`emitIRExpr` already does for its `Left`/`Right`/`Args` (compile.go's
`emitIRExpr`, e.g. avm.go-side nothing changes). Then `OpCall`. **The callee
does its own parameter binding in its own prologue** (compiled once,
identical for every call site, since the target function is compiled once
regardless of how many places call it — the same "compile once, call from N
places" property real machine code already has): `N` `OpStoreLocal`
instructions, argument `N` popped first (last pushed, first popped — the
same "last operand pushed is popped first" convention `OpBn254G1ScalarMul`
already documents at avm.go:258-260), each into that function's statically
assigned slot. `OpStoreLocal` already clones the value being stored
(avm.go:1224, `locals[slot] = value.clone()`) — so argument passing gets
value semantics (no aliasing between caller and callee) for free, from
machinery that already exists and is already gas-charged
(`chargeOperandGas`, avm.go:1213-1217) — not a new mechanism invented for
this design, the exact `RuntimeValue.clone()` discipline v1's own
"reusable findings" list called out (design doc, "Argument passing" bullet)
is already load-bearing here by construction, because it's the same
opcode already doing the same thing for every existing local write.

Argument count is not carried in the `OpCall` instruction at all — it's
implicit and fixed per callee (matches `fn.Params` length, checked once at
compile time), so there is nothing for the runtime to validate beyond what
the compiler already guarantees for well-formed output; adversarial raw
bytecode that mis-binds its own arguments only corrupts its own function's
own locals, never another contract's or another call's (§1.6, §9).

### 1.4 Gas metering across a call

One shared `exec.GasUsed` counter, unchanged (avm.go:922-930) — `OpCall` and
`OpRet` are just two more opcodes in the same flat loop, charged from the
same `GasSchedule` map, checked against the same `gasLimit` on every
iteration regardless of call depth. There is no second gas meter to keep in
sync, and so no way for a callee to "run for free" or be double-charged: it
literally is the same loop, same counter, just with `pc` having jumped.

Critically, **this incurs zero new re-verification or re-decode cost.**
`Run()` calls `NewVerifier(r.params)` + `verifier.Verify(module)` exactly
once, at the very top (avm.go:874-880), on the one `Module` passed in for
the whole `Run()` call. `OpCall`'s target is a PC inside that *same*
`module.Code` — it never resolves a second `Module`, never calls
`DecodeModule` again, never clones a second `Storage`. This is the one
place v1-v4's carried-forward finding ("is callee-module verification a new
per-call cost?") resolves trivially in this design's favor, for a reason
none of v1-v4 had available to them: those designs crossed *contract*
boundaries (a genuinely different `Module`), this one never leaves the
current `Module` at all.

### 1.5 Abort/trap unwind

This is the section where the difference from v1–v4 matters most, and it is
worth stating plainly rather than by assertion: **v1–v4 needed a per-frame
partial-commit/rollback story because they crossed contract boundaries** —
two different contracts' storage, two different balances, an overlay that
had to be flushed-or-discarded per frame. Intra-contract CALL/RET has
**exactly one `state Storage` for the entire `Run()` call, before, during,
and after any number of calls** — the same single `state` variable
(avm.go:889) that a today's zero-call execution already mutates in place
across an entire entrypoint body. A trap anywhere — inside a called
function or not — hits the *same* `rollback()` closure that already exists
(avm.go:913-918): `exec.State = originalState; exec.Outgoing = nil; return
exec, runErr`. This closure doesn't know or care whether any `OpCall` ever
executed; it discards `state` wholesale (reverting to the untouched
`originalState` clone taken at the very start, avm.go:888) and returns.

The new return-address stack (`[]uint32` or equivalent) is a plain Go local
variable inside `Run()`, exactly like `stack`/`locals` already are — it is
never referenced by `rollback()`, never partially unwound, never needs its
own undo log, because **there is nothing per-frame that needs undoing**:
no per-frame storage overlay exists (there is only the one `state`), no
per-frame balance snapshot exists (this mechanism never reads or writes
`OriginalBalance`/`AttachedValue`/any balance at all — see §9). On any
abort, the whole `Run()` call's local state — call stack included — is
simply not returned; the Go garbage collector reclaims it exactly like it
already reclaims `stack`/`locals` today. There is no "leaked orphaned frame"
failure mode to design against, because frames were never a separate
allocation with a separate lifetime from `Run()` itself.

### 1.6 Hard call-depth limit

New `Params` field: `MaxCallDepth uint32`, validated in `Params.Validate()`
(avm.go:675+, alongside the existing `MaxStackDepth`/`MaxMemoryBytes`
checks) to be positive. Checked in the `OpCall` case exactly like
`MaxStackDepth` is checked at every stack-growing opcode
(`if len(returnStack) >= int(r.params.MaxCallDepth) { return
rollback(async.ResultLimitExceeded, nil) }`), **before** pushing the new
return address.

Proposed default: **32**, unchanged from v1's original proposal and every
subsequent round's carry-forward, deliberately kept **separate** from
`async.Params.MaxRecursionDepth = 8` (`x/aetravm/async/params.go:23`,
enforced in `x/aetravm/async/validation.go:92-93`) — confirmed still a
different attack surface on rereading: `MaxRecursionDepth` bounds
cross-block/cross-message mailbox amplification (`msg.Depth`, incremented
per hop across `bounce.go`/`process.go`/`avm.go:4407`), a completely
different resource (queue/storage growth across many blocks) from
`MaxCallDepth` (same-transaction, same-`Run()`-call, native-stack-adjacent
depth). Unifying them would conflate two unrelated DoS budgets for no
benefit.

**Why this cap must be enforced at runtime, not just relied upon as a
compiler invariant** — this is the concrete, verified adversarial point:
`x/contracts/keeper/keeper.go`'s `StoreCode` (keeper.go:255-333) accepts
**raw bytes** (`msg.Bytecode`) decoded via `avm.DecodeModule`
(keeper.go:293) and gated only by `types.ValidateAVMBytecode` plus
`Verifier.Verify` — both purely structural/syntactic checks, neither of
which requires the bytes to have come from this compiler at all. A
hand-crafted `Module` can trivially encode `OpCall` targeting its own
containing function's start address as its very first instruction — direct
self-recursion, impossible to reach via compiler-produced output (the
compiler enforces a recursion-free call graph at compile time, §1.1) but
entirely reachable via raw `MsgStoreCode`. Without a runtime-enforced
`MaxCallDepth`, such a module would grow the return-address stack without
bound, limited only by the flat gas meter — and because each `OpCall` can
be priced cheaply (comparable to `OpJump`'s flat 1 gas, avm.go:509), the
existing `MaxRuntimeGasLimit = 1_000_000_000` (avm.go:464) alone would
permit on the order of a billion pushes before gas exhaustion, a real,
allocatable multi-GB blowup on a single node before the interpreter would
otherwise notice. `MaxCallDepth=32`, checked unconditionally on every
`OpCall` regardless of provenance, closes this the same way
`MaxStackDepth`/`MaxMemoryBytes` already close the equivalent gap for the
operand stack and storage. This is not a hypothetical: it is the direct
adversarial-bytecode analogue of `OpJump`'s already-existing "raw bytecode
can jump anywhere" reality (avm.go's jump-target check is already a runtime
bound for exactly this reason) — `OpCall` inherits the identical threat
model and needs the identical class of defense, independent of anything the
compiler guarantees for its own output.

### 1.7 Compiler mechanics, concretely

- New IR: `IRExprCallUser{Target string, Args []*IRExpr}` (an expression,
  since a call now genuinely produces a value in place, exactly like every
  other `IRExpr`), and a new terminator `IRStmtRet` (`ret`, sibling to the
  existing `IRStmtReturn = "return"` at compile.go's IR-kind list,
  `x/aetravm/compiler/ir.go:41`).
- `tryInlineUserFunctionCall` (compile.go:7050-7109) stops being the only
  path: a call to a declared (non-generic, non-trait-bound — see §4/§5)
  function whose body is more than "lazy bindings + one return" now compiles
  to `IRExprCallUser` instead of failing with `E_LOWER_CALL`. The existing
  inliner can stay as a **cheap-path optimization** for the trivial single-
  expression case (it produces smaller, jump-free bytecode for what is
  genuinely just an expression alias) — both paths are safe to keep side by
  side; which one a given call site uses is an optimization choice, not a
  correctness one, once real CALL/RET exists as the always-correct fallback.
- Each distinct called function (each monomorphized instantiation, §4) is
  lowered **once**, via the same `c.lowerStatementsToIR` +
  `c.lowerIREntry` pipeline entrypoints already use
  (compile.go:3718/3880/4528), with the terminator-selection difference
  from §1.7.1 below, producing its own 0-based `[]avm.Instruction` block.
- `c.buildModule` (compile.go:3544-3626) appends these function blocks to
  `code` **after** all entrypoint blocks, recording each function's
  `funcBase[name] = uint32(len(code))` as it goes — precisely mirroring
  `entryBase`'s existing role (compile.go:3566) one level down.
- Two-pass link, extending the existing one-pass-per-entry approach: after
  every block (entrypoint or function) has been placed and its own internal
  jumps relocated by `relocateJumpTargets` (compile.go:3628-3638, extended
  to also relocate `OpCall` the same way it already relocates
  `OpJump`/`OpJumpIfZero`), a second pass patches every `OpCall`
  instruction's `Arg` from a placeholder call-site-local marker to the
  callee's now-known `funcBase[...]` — the same "record index while
  emitting, patch once positions are known" shape `patchJumpTarget`
  (compile.go:6249-6254) and the label/patch list in `lowerIREntry`
  (compile.go:4608-4614) already use one level down; this is one more
  level of the identical technique, not a new one.
- Local-slot allocator becomes module-wide, not per-entry (§1.1) — the one
  concrete, load-bearing change to existing lowering-environment
  bookkeeping this design requires.

#### 1.7.1 `return` inside a called function vs. at entrypoint top level

`StatementReturn` lowering (compile.go:4192-4204) and the auto-appended
trailing return (`ensureReturn`, compile.go:4522-4523) both need to choose
`IRStmtReturn` (halt whole execution, unchanged) when lowering an
entrypoint/handler body directly, or `IRStmtRet` (pop one call frame) when
lowering a called function's body — a single boolean threaded through the
existing `lowerStatementsToIR`/`lowerIREntry` call chain (e.g.
`insideCalledFunction bool`, defaulting false at every existing call site
so nothing changes for current callers, set true only for the new
per-function lowering calls introduced by §1.7). Because this is a single
extra parameter on an already-recursive lowering call, it requires **no
change** to how `if`/`while`/`match`/`for` already lower their nested
statement lists (compile.go's `StatementIf` case at 4205-4219 and siblings)
— a `return` nested arbitrarily deep inside branches/loops inside a called
function's body just emits `[expr]; IRStmtRet` wherever it lexically
appears, exactly as `IRStmtReturn` already does today for entrypoint bodies,
using the exact same jump-label machinery for the surrounding control flow.
This is what makes item 3 (early return / structured error propagation)
almost free once item 1 exists: nothing about `if`/`while`/`match` codegen
changes at all.

---

## 2. Tuples

### 2.1 Value representation — already exists, verified

`RuntimeValue` already has a `tupleVal []RuntimeValue` field (avm.go:2185-
2187, value.go), a `TagTuple` tag (value.go:42), a constructor
`ValueTuple(elements []RuntimeValue) RuntimeValue` (value.go:491-495) that
clones its input slice, comparison support (avm.go:2535-2547, lexicographic,
shorter-is-less on length mismatch), truthiness/emptiness/length
(avm.go:2289-2330), and a hard bound `MaxTupleElements uint32 = 256`
(value.go:1232) already enforced at decode time (value.go:948-949,
973-974). **None of this needs to be built** — it was evidently built for a
future tuple feature (or for the map-entry pair representation, see below)
and never wired to source syntax. This design wires it.

One existing use collides with general N-arity: `runtimeFieldValue`'s
`TagTuple` branch (avm.go:2937-2948) only recognizes the literal field
names `key`/`first`/`left`/`0` (→ index 0) and `value`/`second`/`right`/`1`
(→ index 1) — a hardcoded pair accessor for map-entry values, not a general
indexed accessor. **Required change**: extend this branch to fall through
to a generic base-10 numeric parse (`strconv.Atoi(field)`) bounds-checked
against `len(source.tupleVal)` when the field doesn't match one of the
existing named aliases — additive, keeps the existing map-entry aliases
working unchanged, adds support for `.2`, `.3`, ... on genuine N-ary tuples.

### 2.2 New opcode: `OpMakeTuple`

`Arg` = element count `N` (compile-time constant, matching the literal's
or return statement's arity). Pops `N` values off `stack` (reverse push
order, same convention as every other multi-operand opcode in this file),
constructs one `ValueTuple([]RuntimeValue{...})`, pushes it. `GasSchedule`
entry priced like `OpDup`/`OpReturn` (flat dispatch cost) plus
`GasPerOperandUnit`-proportional charge via the existing
`chargeOperandGas` helper (avm.go:1168-1170 pattern) — tuples are already
priced per-element for `OpDup`/`OpLoadLocal`/`OpReturn` via
`runtimeValueSizeUnits` (value.go's tuple-element counting, referenced by
FINDING-001's existing mitigation), so `OpMakeTuple` just needs the same
per-element charge at construction time, not a new pricing model.

### 2.3 Wire/ABI encoding — already implemented, verified

`runtimeCodecBytes`'s `TagTuple` case (avm.go:4045-4054) already encodes a
tuple as a JSON array of its elements' own canonical encodings, and
`runtimeCodecField`'s `TagMap, TagTuple` case (avm.go:4141-4143) already
routes tuples through the same codec path structs/maps use. **Multi-value
return ABI encoding needs zero new code** — a function returning `(uint64,
Address)` already returns one `RuntimeValue` of `TagTuple` wrapping two
elements, and that value already round-trips through the existing
CLI/query/receipt JSON codec paths that any other struct-shaped return value
uses today.

### 2.4 Destructuring syntax: `const (a, b) = f();`

Grammar site: `parser.go:851-874`, the `case "const", "var":` branch. Today
`Statement{Kind: StatementBinding, Name: name, ...}` requires exactly one
`expectBindingName()` (parser.go:863). Additive extension: after consuming
`const`/`var`, if the next token is `tokenLParen`, parse a comma-separated
name list until `tokenRParen` instead of the single-name path; otherwise the
existing single-name path is completely unchanged (same tokens consumed,
same error messages, same AST shape for every program that doesn't use
this). New AST field: `Statement.Names []string` (nil for the ordinary
single-name case — `types.go:234-249`), set instead of `Name` when
destructuring. Lowering: `StatementBinding` with `Names != nil` evaluates
`Value` once (pushing one `TagTuple` `RuntimeValue`), then for each name in
order emits `OpDup` (all but the last) + `OpReadField` with `Data` = the
decimal index string (reusing the extended `runtimeFieldValue` from §2.1,
not a new opcode) + `OpStoreLocal` into that name's newly bound slot — a
purely mechanical unpacking, four existing opcodes in sequence, no new
runtime primitive.

### 2.5 Tuple literal expression: `(a, b)`

Grammar site: `parser.go:1743-1754`, the `case tokenLParen:` branch of
`parsePrimary`. Today this unconditionally parses exactly one `parseExpr()`
followed by `expect(tokenRParen)` — a comma where `)` is expected is
currently a parse error, meaning **there is no existing syntax this would
collide with.** Additive extension: after parsing the first inner
expression, if the next token is `tokenComma`, continue parsing a
comma-separated expression list and produce a new
`Expr{Kind: ExprTupleLiteral, Args: [...]}` (new `ExprKind`, `types.go:274-
290`) instead of the bare grouping `Expr`; the existing zero-comma path
(plain parenthesized grouping) is untouched — same tokens, same resulting
`Expr` for every existing program. Lowering: evaluate each `Args[i]` in
order, then `OpMakeTuple(Arg=len(Args))`.

### 2.6 Multi-value return type syntax

`FunctionDecl.ReturnType` is a single `TypeRef` (types.go:100). Rather than
add a new field, reuse the existing `TypeRef.Args []TypeRef` shape already
used for `Map<K,V>`/`Option<T>`/`Result<T,E>` (types.go:186-191): a
parenthesized return-type list `fn f() -> (uint64, Address) { ... }` parses
(extending `parseTypeRef`, parser.go:1828-1866, or equivalently the
return-type branch of `parseFunctionTail`, parser.go:351-372, to accept a
leading `tokenLParen` as an alternative to the mandatory `expectName()`) to
a synthetic `TypeRef{Name: "Tuple", Args: [uint64, Address]}` — zero schema
change, and the compiler's existing generic-arg-list plumbing
(`expectTypeClose`, parser.go:1876-1886, already handles the `>>`-splitting
edge case for nested generics) needs no analogous fix for `)` since `)` is
not an operator token that could ambiguously merge with an adjacent one.

### 2.7 Explicit scope boundary, closing a review finding: tuples are call/return/local-only, never a `@storage`/`@message`/`@event` field type

Review found a real codec incompatibility and this section closes it by
scoping, not by extending the codec. `runtimeCodecBytes`'s `TagTuple` case
(avm.go:4055-4064) encodes a tuple as a **bare JSON array** of its
elements' own encodings (e.g. `[5, "ae1..."]`), which is a different shape
from how a struct/map field is encoded when nested inside another message
(`runtimeCodecField`'s `TagMap, TagTuple` case, avm.go:4151-4153, tags both
as generic `"bytes"`). The generic runtime field-access path used to read a
field back off raw (not-yet-materialized) message bytes —
`runtimeMessageFieldValue` (avm.go:2888-2908) — does
`json.Unmarshal(body, &[]codecFieldValue{})`, which fails on a bare
JSON array/number/string, silently falling back to returning the raw
undedecoded bytes for *any* requested field/index. Concretely: if a tuple
value were ever stored as a `@message`/`@storage`/`@event` struct **field**
and then field/index-accessed generically off raw bytes (as opposed to an
already-in-memory `RuntimeValue` on the VM stack), the access would
silently return opaque bytes instead of the requested element — a wrong
answer, not a trap.

Every call/return/local-position use this design actually specifies —
destructuring (§2.4, `OpDup`+`OpReadField` on an in-memory `TagTuple`),
the tuple literal (§2.5, `OpMakeTuple`, in-memory only), and the
cross-contract getter path (§6, whose resolver hands back a native Go
`RuntimeValue`, never serialized through this path) — all stay in-memory
and never hit this codec path. **Scope decision, made explicit here rather
than left implicit**: a tuple `TypeRef` (i.e. the synthetic `Tuple` name
from this section, or a bare `(T1, T2, ...)` return-type/binding shape) is
permitted **only** in function parameter types, return types, and local
`const`/`var` bindings — never as a declared field type inside a
`@storage`/`@message`/`@event` struct declaration. The type-checker must
reject a tuple-typed struct field with a specific compile error, the same
way it already rejects other malformed field-type combinations, rather
than let it silently compile into a value that decodes wrong later. This
restriction is unlikely to be a real ergonomics loss — a struct field that
would be tuple-typed can always be declared as an equivalent named struct
with the same element types instead — and keeps this pass from having to
extend `runtimeMessageFieldValue`/`runtimeValueFromJSONField` to recognize
a bare-array shape as a positional tuple, which is a separate, decoupled
piece of work left for later if a real need for tuple-typed fields ever
materializes.

---

## 3. Early return / structured error propagation

Falls out of §1 with no additional mechanism, per §1.7.1: a `return`
statement anywhere inside a called function's body — at the top level, or
nested inside any number of `if`/`while`/`match`/`for` blocks — compiles to
`[emit return expression]; IRStmtRet`, using the exact same jump/label
codegen those constructs already have today for entrypoint bodies. Multi-
value early return is `[emit each tuple element]; OpMakeTuple(N); IRStmtRet`
via §2.

**Deliberately not added**: a catchable per-call exception/error channel
distinct from `return`. `StatementThrow`/`StatementAssert`
(compile.go:877-908) already exist and compile to `OpAbort`
(`IRStmtAbort`, compile.go:4181/4597-4603-adjacent path) — a **whole-
execution** abort, not a per-frame one. This design does not change that:
`throw`/`assert` inside a called function still aborts the entire
transaction, exactly as if the same statement appeared at entrypoint top
level today. "Structured error propagation" here means exactly what §2
already provides — a function can return a `Result<T,E>`-shaped tuple
(`(ok: bool, value: T, err: E)` or whatever the contract author's struct/
enum convention is) and the **caller** decides whether to `throw` based on
it, using existing statements — not a new non-local control-transfer
primitive. Adding a real catchable per-call exception (unwind some but not
all frames, run cleanup code, resume at a handler) is a materially bigger
feature (it needs handler-registration bookkeeping across the call stack,
and interacts with §1.5's "no per-frame state at all" safety argument in a
way that would need its own adversarial review) and is explicitly deferred,
not silently assumed away — noted here as documented future work, not
shipped as part of this pass.

---

## 4. Generics via compile-time monomorphization

### 4.0 Starting point, verified

Zero scaffolding exists. `FunctionDecl` (types.go:95-103) has no type-
parameter list. `StructDecl` (types.go:54-59) has no type-parameter list.
`TypeRef.Args` (types.go:186-191) exists only to express built-in
parametric container instantiation (`Map<K,V>`, `Option<T>`, `Result<T,E>`),
reusable as the shape for concrete type arguments but not currently
attached to any user declaration. `grep -i "generic"` across the compiler
package hits only lexer fixes for nested `>>`-splitting
(parser.go:1868-1886) and unrelated English-word uses — no partial generic-
semantics implementation anywhere.

### 4.1 Deliberate scope decision: explicit type arguments, no inference

The task permits "compile-time monomorphization only," not any particular
resolution strategy. This design picks **explicit type arguments at both
declaration and call site** — `fn max<T>(a: T, b: T) -> T { ... }` declared
with a type-parameter list (new, parsed as a `<Name, Name, ...>` list
immediately after the function name, reusing the identical angle-bracket
list-parsing already proven correct for `TypeRef.Args`, including the
`>>`-splitting fix at parser.go:1868-1886, which is a pure tokenization
concern equally applicable here), called as `max<uint64>(x, y)` (extending
`parsePrimary`'s existing call-parsing at parser.go:1721-1730 to optionally
consume a `<TypeRef, ...>` list before the argument list) — **not**
inferred from argument types. This is a deliberate simplification, not an
oversight: type inference (unification, generalization over multiple call
sites, ambiguity diagnostics) is substantially larger, riskier compiler
machinery than anything else in this document, and nothing in the task
requires it — "expand a generic function into a concrete instantiation per
distinct call-site type combination" is fully satisfiable by reading the
type combination directly off explicit syntax. If ergonomics later justify
inference for the unambiguous common case (single type parameter, argument
types match it exactly), that can be layered on later as a strictly
additive convenience without touching this section's core mechanism.

### 4.2 Instantiation detection, naming, and dedup

Each call site `f<T1,T2,...>(...)` is a request for one **instantiation**,
keyed by `(fn.Name, T1.String(), T2.String(), ...)` (`TypeRef.String()`
already exists and already canonically renders nested generic args,
types.go:193-209). A compiler-wide `map[string]*FunctionDecl` (the
monomorphization cache, populated lazily as call sites are discovered while
lowering the module) dedupes: two call sites requesting `max<uint64>` share
**one** compiled body; `max<uint64>` and `max<uint128>` are two separate
compiled bodies. Mangled name for the generated concrete function:
`fn.Name + "$" + T1.String() + "_" + T2.String() + ...` (e.g. `max$uint64`,
`pair$uint64_address`) — used as the key into the same `funcBase` map §1.7
already introduces for plain (non-generic) called functions, so
instantiated generics reuse the identical call-target-resolution and
code-block-layout machinery with no separate code path.

### 4.3 Substitution mechanism

Reuses the exact AST-clone-and-replace technique the codebase already
trusts for a different substitution target: `substituteExprForInline`
(compile.go:6951-7048) currently clones a return **expression** and
replaces parameter **identifiers** with argument **expressions** (value-
level substitution, for the existing inliner). Monomorphization needs the
same clone-and-replace operated on **type positions** instead: before
lowering a specific instantiation's body, clone the generic
`FunctionDecl` (params' `TypeRef`s, `ReturnType`, and any internal type
annotation the body references — e.g. a `const x: T = ...` binding's
declared type) and substitute each occurrence of the type-parameter name
with the concrete `TypeRef` this instantiation was requested with, then
lower the substituted, now fully-concrete `FunctionDecl` exactly like an
ordinary (non-generic) function per §1.7. This is "the same proven
technique, redirected from value expressions to type annotations" — not a
new kind of compiler pass.

### 4.4 Bytecode-size tradeoff, stated honestly

Total code size grows **linearly** in the number of distinct `(function,
type-tuple)` pairs actually called anywhere in the module — each
instantiation is a genuinely separate compiled body, by design (this is
what "no runtime type dispatch" costs: the alternative, one shared body
plus a runtime type tag, is exactly the vtable/dynamic-dispatch shape the
task explicitly excludes). This is not a new, uncapped risk: it is fully
covered by the **existing** `Verifier.Verify` hard caps —
`MaxCodeBytes = 64 * 1024` and `MaxInstructions = 4096`
(avm.go:816-824/468-469) — a contract that monomorphizes too aggressively
simply fails `Verify()` with the same "AVM code bytes must be <= 65536"
error any other code-bloat hits today. No new consensus-critical bound is
required for safety.

What genuinely compounds, and is worth an explicit, low-severity soft cap
purely for diagnostics quality (not safety): each instantiation also claims
its own disjoint local-slot range (§1.1), so heavy monomorphization eats
into the **same** shared `MaxStackDepth`-bounded slot budget the whole
module's locals draw from, on top of the code-size cost — a genuinely
doubly-compounding cost, honestly stated as the tradeoff the task asked to
name. Recommended: an explicit per-generic-function instantiation-count
soft limit (e.g. 16), enforced as an early, specific compile error
("generic function %q instantiated with %d distinct type combinations,
exceeds the limit of 16") **purely so a contract author gets a clear
message instead of discovering the problem as an opaque `MaxCodeBytes`
failure** possibly caused by unrelated code elsewhere in the same contract
— a usability improvement layered on top of the load-bearing
`Verify()`-time caps, not a substitute for them.

### 4.5 Monomorphization termination, made an explicit invariant (review finding closed)

§4.4's safety argument bounds compiled **output** size, but review
correctly pointed out that bounding the output doesn't by itself establish
that the monomorphization **process** terminates — those caps
(`MaxCodeBytes`/`MaxInstructions`) are checked in `Verifier.Verify` on the
*finished* module, after monomorphization already ran to completion. A
self-referential generic — `fn F<T>(...) { ...; F<Wrap<T>>(x); }` — would,
under §4.2's lazy worklist keyed by mangled name (`F$T.String()`), request
an ever-deeper distinct instantiation (`F$uint64` → `F$Wrap_uint64` →
`F$Wrap_Wrap_uint64` → ...) with no textual bound in the source, since each
level's `TypeRef` is synthesized by the substitution engine, not written
out by the author — a compiler-side (off-chain, non-consensus) hang/OOM
vector that `MaxCodeBytes` never gets a chance to catch, because it's never
reached.

This is, in fact, already prevented — but by a mechanism this document must
now state and rely on explicitly, not leave as an accidental side effect:
`validateFunctionRecursion` (compile.go:2062-2099, run once at
compile.go:699, on the raw, pre-generic `FunctionDecl`s, before any
monomorphization happens at all) is keyed on bare callee **name**, not
`(name, type-arguments)`. Since `F<Wrap<T>>(x)` is a call to callee name
`F` from within `F`'s own body, this reads as ordinary direct self-
recursion on the un-substituted declaration and is rejected outright by
this pre-existing, general-purpose check — before generics-specific logic
ever runs, and independent of anything §4 adds.

**This document therefore states, as a load-bearing invariant, not an
incidental convenience**: generic self-reference — a generic function
calling itself, or calling another generic function that (transitively)
calls back to it, under **any** distinct type-argument combination — is
forbidden by the exact same recursion-freedom rule that governs every
non-generic call in §1.1, checked on the bare function name **before**
monomorphization, and this is what makes the monomorphization worklist's
termination provable rather than merely typical. If a future change ever
wants to allow useful recursive-generic patterns (a tree/list walker being
the obvious motivating case), that future change must make the
instantiation-count cap in §4.4 (today "purely a usability improvement...
not a substitute for" the load-bearing size caps) into a genuinely
load-bearing, checked-**before**-recursing-into-a-new-instantiation's-body
limit at that time — not simply relax or bypass the name-keyed recursion
check without replacing what it was silently also providing. Absent that
future change, the existing checker's name-keyed behavior must not be
"fixed" to be more permissive (e.g. re-keyed to `(name, type-args)` to
allow legitimate non-recursive uses of the same function name with
different type arguments in a cycle) without re-deriving this termination
argument from scratch.

---

## 5. Static (compile-time-resolved) trait dispatch

### 5.0 Starting point, verified

100% unimplemented, confirmed two ways: `grep -i trait` across the compiler
package hits nothing semantic, and `surface_test.go:422-427` **pins**
`trait Demo {}` as a parser-rejection regression test ("unexpected
top-level declaration") — traits are not partially wired, they are actively
rejected by an existing test that would need to be updated as part of
shipping this.

### 5.1 What "safe, scoped" trait dispatch actually reduces to

The task allows dynamic (vtable, runtime-type-tag) dispatch to be
explicitly deferred if not safely achievable this pass. Worth stating
plainly rather than hedging: dynamic dispatch is not merely "harder," it
is architecturally in tension with this VM's whole-module, flat-code,
statically-resolved-call-target design from §1 — a vtable implies an
indirect call whose target is a **runtime value**, which is exactly the
"OpCall's target is always a compile-time-immediate absolute PC" invariant
§1.2 relies on for its entire safety argument (no second Module resolution,
no re-verification cost, no runtime-computed jump target to bounds-check
against anything other than the current module). Retrofitting indirect
calls would reopen a meaningfully different, unaudited design surface. This
is deferred, with that specific technical reason, not merely "not enough
time."

What **is** safely achievable, and is the version of "static trait
dispatch" this design recommends: a `trait` declaration is purely a
**compile-time signature contract** (a named set of method signatures,
optionally with type-parameter bounds), and an `impl TraitName for
StructName { fn method(...) {...} }` block provides one concrete body per
implementing type, **checked at compile time** for signature conformance
against the trait declaration. Every call site using trait-bound syntax
must have its concrete implementing type **statically known** — a
direct call on a variable/parameter of a concrete (non-trait-typed) struct
type, which needs no trait machinery *at all*, it's just an ordinary
method-call-to-mangled-function-name resolution (§5.2 explains precisely
*how* that resolution must happen for it to stay safe).

### 5.2 Review finding closed: trait dispatch must not bypass the compiler's recursion-freedom check

An adversarial review of this design (fund-safety pass) found a concrete,
non-adversarial-source gap in the paragraph above's original form, which
this section closes by narrowing scope rather than by patching the checker.
Restated precisely because it's the load-bearing precondition for all of
§1: `validateFunctionRecursion` (compile.go:2062-2099) is a whole-program
DFS-on-stack cycle detector over a `callGraph` built by
`collectFunctionCallsFromExpr` (compile.go:2149-2196), and §1.1's *entire*
justification for giving each function a compile-time-disjoint locals-slot
range (no dynamic frame pointer needed) rests on that checker guaranteeing
"at most one invocation of any given function is ever live at once."

The gap: `collectFunctionCallsFromExpr`'s `ExprCall` case only emits a
call-graph edge when `len(expr.Path) == 1` (compile.go:2153) — a bare
identifier call. This is deliberate today (it's how the checker already
skips namespace/stdlib calls like `finlib.mulDiv(...)`), but a receiver-
style call `x.compareTo(y)` parses through the *same* generic path
(`parser.go:1721-1742`, `parsePath()` then `Expr{Kind: ExprCall, Text:
path[0], Path: path, ...}`), producing `Path = ["x", "compareTo"]`, length
2 — invisible to the checker regardless of whether `x`'s type is concrete
or generic-bound. Combined with a trait-bounded generic function whose body
calls back into the generic function itself through a trait method
(`fn process<T: Comparable>(x: T, y: T) -> bool { return x.compareTo(y); }`
paired with `impl Comparable for Widget { fn compareTo(self, other:
Widget) -> bool { return process<Widget>(self, other); } }`), the cycle
`compareTo → process → compareTo` has one edge (`compareTo → process`)
that's checker-visible and one (`process → x.compareTo(y)`) that
structurally is not — so `validateFunctionRecursion` reports no error and
the module compiles with two simultaneously-live invocations of
`process$Widget` silently aliasing the same locals slot range. This is
reachable from **ordinary, non-adversarial Aetralis source**, not just raw
`MsgStoreCode` bytecode — a materially different (and worse) threat model
than §1.6/§9's adversarial-bytecode-only self-recursion analysis, which
this section does not weaken, only supplements.

**The fix this design adopts is scope reduction, not a checker patch,
because the checker patch has a hard ordering problem**: closing the gap
fully would require either (a) extending `collectFunctionCallsFromExpr` to
emit edges for multi-segment-`Path` calls once the receiver's concrete
callee is resolved, or (b) a second cycle check over the
post-monomorphization, `funcBase`-keyed call graph — and *both* options
require the concrete callee to be known, which for a trait-bounded generic
parameter (`x: T` where `T` is only pinned down to `Widget` at a specific
call site's monomorphization, §4.2) is only true **after** monomorphization
has already run — i.e. after `validateFunctionRecursion` has already
produced its verdict on the un-substituted declarations. Making that work
correctly is a real, separate compiler-ordering redesign (the recursion
check would need to move to a second pass over resolved instantiations,
or become interleaved with the lazy monomorphization worklist itself), and
is exactly the kind of larger, riskier change the owner's scoping
instruction says to defer rather than force through in the same pass as
everything else in this document.

**So this design ships a narrower, genuinely safe version of §5.1
instead**: trait method-call syntax (`x.method(y)`) is permitted **only**
when `x`'s static type is a concrete, non-generic struct type at the call
site — i.e., the receiver's type does not depend on any enclosing
function's type parameter. In that case, and *only* in that case, the
compiler resolves `x.method(y)` to its mangled concrete-impl name
(`StructName_method`) **during ordinary name resolution, before
`validateFunctionRecursion` runs**, rewriting the `ExprCall` node itself
to a single-segment `Path = ["StructName_method"]` — so by the time the
existing, unmodified whole-program cycle detector walks the call graph, a
concrete trait-method call looks exactly like, and is exactly as
checker-visible as, an ordinary bare function call. No change to
`validateFunctionRecursion` or `collectFunctionCallsFromExpr` is needed for
this narrower case, and no new call-site shape is invisible to it.

**Explicitly deferred, with the reasoning above, not silently dropped**:
calling a trait method on a value whose type is an in-scope generic type
parameter (`fn process<T: Comparable>(x: T, y: T) -> bool { ...
x.compareTo(y) ... }` — §5.1's originally-proposed "one case where traits
add real value") is **out of scope for this pass**, specifically because
its concrete callee is only known post-monomorphization, after the
recursion checker has already run, per the analysis above. Also
out of scope, for the separate, architectural reason already given: a
trait-typed *value* — a variable, parameter, field, or return position
declared as `x: dyn Comparable` (or equivalent) that could hold *any*
concrete implementor chosen at runtime, needing a vtable or runtime type
tag, which is in direct tension with §1.2's compile-time-immediate `OpCall`
target invariant. Both are real, useful capabilities left for a future
pass once the recursion/cycle check is redesigned to run post-
monomorphization (or interleaved with it) — not vague "traits are hard"
hand-waving. What ships this pass is deliberately smaller than §5.1
originally sketched: trait declarations and `impl` blocks as a compile-time
signature-conformance mechanism, plus direct dispatch on concrete-typed
receivers only, with zero new runtime footprint and zero new call-site
shape invisible to the existing safety-critical recursion check.

---

## 6. Read-only synchronous cross-contract calls

### 6.1 Why this is categorically different from what killed v1–v4

Every v1–v4 blocker required a **write path** to another contract's
storage or balance to exist at all — the lost-update was in overlay
*commit* ordering, the fund-annihilation was in `TransferValue`'s *credit*
flush. A getter call, by construction, cannot write: `IsReadOnlyEntrypoint`
(avm.go:4611-4613) returns true only for `EntryQuery`, and — re-verified
directly this session by grepping every `if readOnly {` guard in
`avm.go` — **exactly four** opcodes are gated behind it:
`OpWriteStorage` (avm.go:975-977), `OpDeleteStorage` (1000-1002),
`OpEmitInternal` (1226-1228), `OpScheduleSelf` (2110-2112) — the complete
set of state-mutating-or-value-moving opcodes in the interpreter. A nested
`Run()` invocation forced to `ctx.Entry = EntryQuery` is therefore
*mechanically* incapable of writing storage, emitting a message, scheduling
a self-message, or (there being no separate value-transfer opcode at all
today) moving any value. Not "the design intends it to be read-only" —
the same four `if readOnly` checks that already gate every production
getter call today (`contract_get.go:89`'s existing `runner.Run(module,
state, avm.RuntimeContext{..., Entry: avm.EntryQuery, ...})` call shape)
gate this identically, because it reuses that exact call shape, not a new
one.

### 6.2 Mechanism

`avm.go` has, and must keep, zero import of `x/contracts/keeper` (verified:
`avm.go` imports only `x/contracts/types`, avm.go:35 — the acyclicity
property v1 first flagged and every round since preserved). A new opcode,
**`OpCallExternalGet`** (`Arg` = a compile-time-constant getter selector,
carried the same way `OpEmitInternal` already carries its message opcode
in `Arg`, avm.go:1233-1236), pops a target `TagAddress` value plus its
argument values off `stack`, and invokes a **resolver callback** threaded
through a new `RuntimeContext` field (`ExternalGetResolver func(address
string, selector uint32, args []RuntimeValue, gasBudget uint64)
(RuntimeValue, uint64, error)` — return value, gas actually consumed,
error). `avm.go` never resolves the callee itself; it only calls the
function pointer it was handed, keeping the acyclicity property intact by
construction, the same shape v1's "candidate direction (a)" sketched for
the (rejected) mutating case, but now with no write path for it to need to
also carry.

The resolver is implemented in `x/contracts/keeper` (not `avm.go`), and
does **exactly** what `contract_get.go:89` already does for a same-
contract getter query, reused verbatim rather than reinvented: look up the
target contract by address in the **same** `next`/`k.genesis` scratch copy
the *enclosing* mutating keeper method is already threading through its own
read-mutate-write body (so the read is consistent with whatever the
outer call has already folded into `next` earlier in the same delivery —
no new consistency concern, it's the same snapshot the caller already
established); find its code, `avm.DecodeModule` it, and call
`runner.Run(module, contractStorage, avm.RuntimeContext{Entry:
avm.EntryQuery, ...})` — identical machinery, not new machinery.

### 6.3 Gas metering across the boundary

This is the one place in this whole document where a v1-class finding
("`ResolveCallee`'s storage clone is real, uncharged work") genuinely still
applies, because — unlike §1's intra-contract `OpCall` — this mechanism
really does resolve a **second** `Module` and clone a **second**
`Storage`. Required, and different from §1's "reuse the existing one shared
counter" story:

- The resolver must be handed a **remaining-budget cap**, not the callee's
  own independent gas limit: `gasBudget = gasLimit - exec.GasUsed` at the
  call site, so the callee's nested `Run()` cannot spend more than what's
  left of the *caller's own* transaction budget — this is what prevents
  "callee runs for free."
- On return, the caller adds the callee's actual `exec.GasUsed` into its
  **own** `exec.GasUsed` before continuing — gas is genuinely shared
  across the boundary, the same "one counter for the whole call tree"
  principle §1.4 established for intra-contract calls, just now
  bridged across two separate `Run()` invocations instead of staying
  inside one.
- A **new** gas charge is required for resolving the callee: byte-
  proportional to the callee's decoded module size plus its storage
  snapshot size (mirroring `chargeOperandUnits`'s existing pattern for
  `OpReadStorage`'s whole-state-snapshot branch, avm.go:952-955), charged
  **before** the nested `Run()` executes, so a chain of external-get calls
  through large contracts cannot buy O(depth × contract size) real work for
  O(depth) charged gas — precisely the gap v1's own review caught for the
  (rejected) mutating design, and precisely the gap that would reopen here
  if this one new decode/clone weren't priced.

### 6.4 The one place this design uses real Go recursion, and why that's bounded

Unlike §1 (a flat frame-stack **inside one `Run()` call**, deliberately
chosen specifically so AVM call depth never controls native Go stack
depth — v1's own explicit finding, carried forward unchanged), this
mechanism is **implemented as real Go-level recursion**: the resolver
callback is invoked from inside the caller's `Run()` opcode dispatch (i.e.,
from inside the `for` loop), and the callback itself calls `runner.Run()`
again — a genuine nested Go function call, not a flat `pc` reassignment.
This is architecturally different from §1 and must be flagged as such
rather than glossed over: flattening it (making the interpreter suspendable
/ resumable via an explicit continuation instead of the Go call stack)
would be a substantially larger interpreter redesign, correctly out of
scope for this pass per the owner's scoping instruction to prefer a
smaller, genuinely safe feature over forcing a bigger one through.

Given that, a **separate, small, hard-capped** depth limit is required
specifically for this mechanism — not reusing `MaxCallDepth` from §1, which
governs a different (native-stack-safe) resource. Recommended:
`MaxExternalGetDepth = 4`, deliberately far smaller than `MaxCallDepth=32`,
threaded through `RuntimeContext` (incremented by the resolver before each
nested `Run()`, checked before invoking it at all) precisely because this
one *does* consume real native stack. At depth 4, worst-case nested-`Run()`
Go-stack usage is a small, bounded constant (each `Run()` frame's own
internals are an iterative loop, not itself recursive, so each nesting
level adds a roughly fixed, small amount of native stack) — nowhere near
default goroutine stack limits, while still bounding worst-case metered
work (branching factor `MaxInstructions` per level, `depth` levels) to a
concrete, small ceiling. Even a maximal-depth chain does bounded, fully
metered, **read-only** work — no atomicity concern at any depth, since
nothing at any level ever writes.

### 6.5 Failure semantics reuse an existing language construct

A target that doesn't exist, or whose getter traps, or that exhausts the
remaining gas budget, is a plain failure with **no rollback bookkeeping
required for the target** (it made no changes at any point — there is
nothing to undo). The language already has a catchable-failure expression
form, `try <expr> [else <expr>]` (`ExprTry`, parsed at parser.go:1687-1706)
— wiring `OpCallExternalGet` as an ordinary fallible expression that can
appear as `try`'s operand gives contract authors existing, familiar syntax
for "call another contract's getter, and fall back to a default if it
fails" with **zero new error-handling primitives**. (Confirming exactly
which existing fallible operations `ExprTry` currently wraps, to extend the
same catch path cleanly to this new one, is a concrete pre-implementation
verification item — flagged here as an implementation-time check, not a
design gap: the grammar and the general mechanism already fit.)

---

## 7. Why cross-contract mutation stays async-only — this design's own position

Stated as this document's considered conclusion, not merely inherited from
the prompt that requested it:

**First, the empirical case.** Four independent, honest design rounds
(v1–v4) each fixed the previous round's specific bug and each found a new
one — with **no convergence in severity**, unlike this same team's Phase D
(ZK/pairing) design track, which converged monotonically over three rounds
to a sign-off. v4's finding was worse than v1's: not a reentrancy-gated
lost-update requiring a specific opt-in, but silent destruction of an
ordinary counterparty's *pre-existing* balance on the single most common
real-world code path (a plain payout to a wallet). That pattern — later
rounds finding *more* severe bugs, not fewer — is itself evidence that the
underlying problem (atomic multi-storage-domain commit + eager value
transfer + reentrancy, inside a VM with no access to the second party's
real ledger) has a structural tension with this VM's storage-overlay model
that isn't converging toward a fix, not merely "we haven't tried hard
enough."

**Second, the architectural case, specific to this chain.** AEZ's entire
future value proposition is zone isolation — the whole point of the
roadmap is that zones will eventually be separately-executed (and
eventually separately-sharded) domains. A synchronous, atomic call across a
zone boundary is fundamentally incompatible with that isolation model: once
zones are actually separate execution domains, "atomic call across the
boundary" becomes a distributed-transaction problem (coordinated two-phase
commit or equivalent), not an implementation gap solvable by a cleverer
single-process VM design. Even a version of synchronous cross-contract
mutation made *fully* safe for **today's** single-zone reality would become
a liability the day zones actually split — either it silently stops being
atomic across a zone boundary (a correctness regression nobody chose), or
the chain is stuck maintaining a genuine distributed-commit protocol for a
feature that was only ever supposed to be a same-process convenience.
Keeping mutation async-only is forward-compatible with AEZ by construction;
synchronous atomic mutation is a standing architectural debt from the day
it ships.

**Third, the mechanism already exists, is already shipped, and is already
the right shape.** `x/aez/keeper/outbox.go`/`inbox.go`/`drain.go` provide
exactly-once delivery (a `ProcessedKey` marker committed atomically with
its effects in the same block write, closing the "dequeue-by-id ambiguity"
`x/contracts`' own internal queue has, per the codebase's own comment
contrasting the two), bounded queue depth
(`MaxZoneMessageQueueDepth`, enforced in `writeEnqueued`, outbox.go:107-125),
bounce-on-failure with a strict kind ladder (only `MessageKindNormal`
bounces; a bounce that itself fails is terminal, no re-bounce loop,
drain.go:299-300), `MaxBounceDepth` (drain.go:340-341/373), and deadline-
based timeout (`msg.DeadlineHeight`, checked at drain.go:266). This bus is
provably inert for the current single-zone genesis
(`EnqueueMessage`'s `srcZone == dstZone` early-return, outbox.go:70-72) —
so choosing it as the *only* sanctioned cross-contract-mutation path costs
nothing today and is exactly the mechanism that starts mattering the moment
zones actually split. This is also not a novel pattern for this stack:
inter-*chain* communication in the wider Cosmos ecosystem (IBC) is
already async/queued/exactly-once for precisely the same reason — applying
the identical shape at the intra-chain cross-zone boundary is consistent
with how every other domain boundary in this stack already works, not an
invented constraint unique to this design.

**What §6 (read-only calls) already covers, so this isn't a capability
gap left unfilled**: price/oracle lookups, allowance/balance checks,
registry lookups, and any other "read another contract's current state and
react to it" composition pattern — the overwhelming majority of legitimate
cross-contract composition — are fully served by §6 with no atomicity
problem at all (a read cannot corrupt anything; worst case it observes
slightly-stale-but-still-internally-consistent state, and `x/contracts`
already serializes all writes through one ABCI-ordered path per block, so
"stale" here only ever means "as of an earlier point in the *same* block's
processing," never a genuinely inconsistent snapshot). What's left —
*mutating* another contract's state as part of the current transaction — is
precisely the case that needs the async bus, and only that case.

---

## 8. `x/contracts` storage correctness fixes

Independent of everything above — a real fix regardless of which parts of
§1–§7 ship.

### 8.1 The gap, precisely

`Keeper.mu *sync.RWMutex` (keeper.go:67) is documented (keeper.go:93-102)
to guard `k.genesis` via `snapshotGenesis()` (RLock, keeper.go:87-91) and
`assignGenesis()` (Lock, keeper.go:103-107) — but **every** mutating method
reads `k.genesis` *bare* (`next := k.genesis`, and repeated bare
`k.genesis.Params.X`/`k.genesis.State.X` reads through the body) rather than
through `snapshotGenesis()`, across a body that can span 50-200+ lines
(including AVM execution and bank-keeper calls), with only the single
final `k.assignGenesis(next)` actually taking the lock.

Independently re-derived (not just re-cited) the actual live-risk shape
this session, since it's more subtle than "any two goroutines can race
today": the **live** query surface already does the right thing —
`Contract()`/`Contracts()` (keeper.go:532-580) correctly call
`k.snapshotGenesis()` exactly once at the top, as documented. The bare-
reading query paths (`ValidateInvariants`, keeper.go:242-244;
`RootContribution`, 246-248; `ExportGenesis`/`ExportGenesisState`,
179-187) are confirmed to have **no live callers** anywhere in the repo
(grep hits only `keeper_test.go`) — and since ABCI genuinely does process
one message at a time (no live concurrent-writer path exists today either,
confirmed: no `SetPrepareProposal`/custom mempool/parallel executor
anywhere, §8.3), there is today no live goroutine that both writes
`k.genesis` unsynchronized and races a concurrent unsynchronized reader of
it. **The honest framing is therefore defense-in-depth and contract
correctness, not "there is a demonstrated live bug today"**: the mutex's
own documented purpose ("guards genesis against the concurrent gRPC/REST
query goroutines racing the... write path") is not actually upheld by its
current final-write-only scope — it only happens to be safe today because
of two separate facts (ABCI's single-writer guarantee, and the bare-reading
query methods being dead) that the keeper itself does nothing to enforce.
Reviving either — a future concurrent-tx-executor change (explicitly out of
scope here, see §8.3), or a new query method some future engineer writes
without remembering to call `snapshotGenesis()` — would silently reopen
exactly the race the mutex claims to prevent, with nothing catching it.
That is the fix's real justification: making the mutex's actual coverage
match its documented contract, structurally, rather than by convention.

### 8.2 Every mutating method, enumerated (23 `assignGenesis` call sites,
mapped to their enclosing top-level method this session, not copied from
the prior investigation):

`InitGenesis` (164) · `storeCodeUnchecked` (329) · `UpdateContractParams`
(485) · `SubmitSecurityAttestation` (506) · `RevokeSecurityAttestation`
(524) · `instantiateContract` (934) · `UpgradeContractCode` (1018) ·
`MigrateContractState` (1080) · `SetContractAdmin` (1112) ·
`DisableContractUpgrades` (1156) · `ScheduleContractUpgrade` (1245) ·
`ApplyScheduledContractUpgrade` (1325) · `executeContract` (1820) ·
`TopUpContract` (1930) · `PayContractStorageDebt` (1981) ·
`unfreezeContract` (2039) · `GrantNativeStakingCapability` (2069) ·
`InjectNativeStaking` (2115) · `ReceiveInternalMessage` (2525) ·
`dropQueuedInternalMessage` (2638) · `SetAssetOwner` (2791) ·
`persistContractAt` (2885) · `loadForBlock` (3149) — all line numbers in
`x/contracts/keeper/keeper.go`, this session's read.

**The deadlock hazard that any fix must specifically avoid** (verified by
reading `chargeContractRentAt`/`persistContractAt` directly,
keeper.go:2847-2887): `chargeContractRentAt` calls `persistContractAt` on
the storage-rent-debt branch, and `persistContractAt` **itself** calls
`k.assignGenesis(next)` (keeper.go:2885) — a separate, earlier commit —
from *inside* another mutating method's still-in-progress body.
`chargeContractRentAt` is called from `executeContract` (keeper.go:1679),
`InjectNativeStaking` (keeper.go:2090), and `ReceiveInternalMessage`
(keeper.go:2207) — each of which *also* calls `assignGenesis` again
itself at its own end. This is intentional (rent owed should stick even if
the triggering call is later rejected for the debt itself). **A naive fix
that wraps each public method in one lock spanning its whole body would
self-deadlock the instant it reaches this nested `assignGenesis` call**,
since `sync.RWMutex` is not reentrant.

### 8.3 The fix

A **second, separate** lock — `k.txMu *sync.Mutex` (not `k.mu`, a distinct
object) — acquired via `defer k.txMu.Unlock()` at the very top of each of
the ~21 **public, top-level** mutating entrypoints listed in §8.2 (not the
internal helpers `persistContractAt`/`chargeContractRentAt`, which are only
ever called from within an already-`txMu`-locked top-level call and must
**not** re-acquire it). This serializes the entire logical read-mutate-write
transaction — nested internal commits included — as one critical section
from the perspective of any other concurrent caller, while
`k.mu`/`snapshotGenesis`/`assignGenesis` continue to guard the raw field
swap itself exactly as today (still needed for query-side correctness
regardless of `txMu`, and layering rather than replacing keeps the fix
strictly additive). No deadlock: `txMu` is acquired exactly once per
top-level call and the nested helpers only ever touch `k.mu` (via
`assignGenesis`), a different lock object entirely.

**Correction found by review, applied here**: an earlier draft of this
section additionally excluded `loadForBlock` from the lock, on the
(checked-and-found-false) assumption that it is "only ever called from
within an already-`txMu`-locked top-level call." Verified directly against
`x/contracts/keeper/grpc_server.go`: every one of the ~16 msg-server RPC
handlers (lines 58, 69, 80, 91, 108, 125, 141, 158, 175, 189, 203, 230, 247,
264, 281, 298, 316) calls `m.keeper.loadForBlock(ctx)` **first, and
separately**, then calls the mutating `...State` method afterward — it is
not nested inside any §8.2 entrypoint's body, it runs and *returns* before
one is ever entered. `EndBlocker` (keeper.go:2564) likewise calls it
standalone, before its own message-delivery loop. `loadForBlock` itself
(keeper.go:3128-3151) does unsynchronized bare writes to `k.runtimeCtx`,
`k.written`, `k.writtenResidual` — none behind `k.mu` — before its own
`k.assignGenesis(gs)` call. Since it runs before literally every mutating
RPC and every `EndBlocker` invocation, it is the single most frequently
executed read-mutate-write critical section in the module; excluding it
would leave the fix's own stated goal (mutex coverage matching its
documented contract) unmet on the hottest path. **Corrected fix**:
`loadForBlock` gets its own independent `txMu.Lock()`/`defer Unlock()`
cycle, exactly like the ~21 enumerated entrypoints, *not* an exclusion.
Because callers invoke it sequentially (call it, let it return, then
separately call the mutating method), giving it its own independent
lock/unlock cycle introduces no nesting and therefore no deadlock — the
lock is fully released before the subsequent mutating call acquires it
again.

**Wrapper-adapter gap, also found by review, closed here**: several
exported methods are reached through thin wrapper adapters that never call
`assignGenesis` themselves (so they don't appear in the §8.2 list derived
from grepping `assignGenesis(` call sites) but do perform a **bare,
unsynchronized read of `k.genesis`** before delegating to a listed
entrypoint — e.g. `deployContract` (keeper.go:362-381) reads
`k.genesis.Params` at line 363 before calling `k.instantiateContract(...)`;
`executeExternal` (keeper.go:395-419) reads `k.genesis.State.Contracts` at
line 399 before calling `k.instantiateContract`/`k.executeContract`;
`ExecuteInternal` and `SendInternalMessage` (keeper.go:429-433, 452-456) —
confirmed live wire-level `Msg` handlers via `grpc_server.go:94,111`, not
test-only — both read `k.genesis.Params` before delegating to
`k.ReceiveInternalMessage(...)`. If `txMu` were acquired only inside the
listed entrypoints, these wrappers' own bare reads would sit outside the
lock entirely, leaving the "coverage matches the mutex's documented
contract" goal unmet for these paths specifically. **Fix**: each of these
wrapper adapters also acquires `txMu` at its own top (before its first bare
read), and the listed entrypoint it delegates to must **not** re-acquire
`txMu` on that call path — since Go's `sync.Mutex` is not reentrant, this
means the ~21-entry list in §8.2 needs one more pass at implementation time:
methods reachable *both* directly (as their own RPC/CLI entrypoint) *and*
indirectly (via one of these wrappers) need their locking pulled up to
whichever adapter is topologically first on any given call path, with the
inner method taking an already-locked private variant (mirroring the
existing public/lowercase-private split the codebase already uses
elsewhere, e.g. `ExecuteContract`/`ExecuteContractState` both delegating to
one shared lowercase `executeContract`). **Pre-implementation check this
design still flags rather than silently assumes**: re-derive the
entrypoint list from the wire-level `Msg`/`Query` surface
(`grpc_server.go`/`service.go`), not from internal `assignGenesis` call
sites, and confirm no locked method calls another locked method on the same
goroutine (which would double-acquire `txMu` and deadlock) before enabling
it — concrete and cheap to run first, and now scoped to include the
wrapper adapters and `loadForBlock` identified above, not just the original
21.

### 8.4 State-root computation: the smallest safe improvement, scoped honestly

Verified directly: `RefreshStateRoot` (`x/contracts/types/contract_state.go:
982-987`) does `gs.State = gs.State.Normalize()` (983) and then calls
`ComputeContractsStateRoot(gs)` (985) — which **itself** calls
`gs.State.Normalize()` again internally
(`x/contracts/types/types.go:229`). This is a real, redundant double
deep-clone-and-sort of the entire state on every single mutating call,
independent of which single field actually changed.

This cannot be fixed by simply deleting the internal `Normalize()` call
from `ComputeContractsStateRoot`, verified by checking its other two call
sites: `DefaultGenesis()` (types.go:163-168) and `GenesisState.Validate()`
(types.go:205-219, `gs.StateRoot != ComputeContractsStateRoot(gs)` at line
215) can both be called on a `gs` that has **not** already been normalized
in memory (e.g. `Validate()` runs against a freshly-unmarshaled genesis
before any `RefreshStateRoot` call) — removing the self-normalizing
behavior from the public function would break root verification for those
callers. **Before/after, scoped correctly**: add an unexported
`computeContractsStateRootNormalized(gs GenesisState) string` that skips
the `Normalize()` call, have the public `ComputeContractsStateRoot` call it
after normalizing (unchanged behavior for `DefaultGenesis`/`Validate`/the
existing test at `keeper/contract_record_growth_test.go:307`), and have
`RefreshStateRoot` call the skip-normalize variant directly, since its
input is already normalized two lines above. **Before**: 2× full
`Normalize()` (each O(total state size) — sorts every collection, deep-
clones every `CodeRecord.Bytecode`/`Contract.Data` blob) + 1×
`json.Marshal`, on every mutating call. **After**: 1× `Normalize()` + 1×
`json.Marshal`. Halves the CPU cost of state-root computation on the
hottest path in the module. Honestly scoped: this is a **CPU-only** fix —
`persistence.go`'s own doc comment (lines 60-65, re-read this session)
is explicit that state-root computation "costs CPU and ZERO GAS," so this
improves node performance, not consensus gas metering.

**Deliberately not attempted this pass, documented as the right next
step**: redefining the root as a fold over per-record hashes in key-byte
order, computed lazily on export/query instead of eagerly on every write —
`persistence.go`'s own doc comment (lines 89-105, re-verified) already
identifies this as tractable specifically because `RootContribution`/
`ValidateInvariants` (the only callers that would need the eager field)
have no live callers today, and because `Validate()`'s root check is
already a tautology on the load path (`RefreshStateRoot` overwrites the
field, then `Validate` immediately recomputes and compares against what it
just wrote). This is a materially larger redesign (touches the export/
query path, not just the write path) and is correctly deferred rather than
forced through in the same pass as the storage-locking fix above, per the
owner's explicit preference for a smaller, verifiably-safe change over a
larger, riskier one bundled in.

### 8.5 What this does **not** unlock — stated plainly, not left implied

`app/app.go:68`'s `baseapp.SetOptimisticExecution()` is **pipelining within
one block height's consensus round-trip** — verified against the actual
SDK v0.54.3 source (`cosmos-sdk@v0.54.3/baseapp/abci.go:618-621`,
987-1015): it speculatively runs the *same*, fully sequential,
one-tx-at-a-time `FinalizeBlock` for a given height in a background
goroutine, overlapping with CometBFT's prevote/precommit network round
trip for *that same height*, then either adopts or discards the result —
it is not concurrent execution of multiple transactions, and not
concurrent execution of multiple heights. `app/app.go:73-76`'s own comment,
re-read this session, states plainly that this app does **not** call
`SetBlockSTMTxRunner` (Cosmos SDK's actual parallel-tx-execution feature),
"which panics if constructed with the block gas meter enabled" — i.e. this
app is currently incompatible with that feature as configured, not merely
not using it by choice. `app/aetra_core_wiring.go`'s
`ValidateAetraCoreWiringGate` (read in full this session, lines 1-98)
hard-panics the binary unless `AetraCoreRoutingExecutionPoint() ==
RoutingExecutionPointAnteAdmissionOnly` — this gates `x/routing`, an
unrelated **admission-control** module, to ante-handler-only logic; it is
not, and was never intended as, a concurrency primitive, and confirms (via
its own scope) that no `PrepareProposal`-adjacent execution surface exists
behind that name either. Direct grep for
`SetPrepareProposal|PrepareProposalHandler|SetMempool|ParallelTx` across
`app/` returns zero hits.

**Stated plainly**: the §8.3/§8.4 fixes make `k.genesis` internally correct
*if* a concurrent caller ever existed. They do not create one, and nothing
in this codebase today provides one. Real concurrent zone execution would
require a separate, substantially larger architecture change — a real
`PrepareProposal`/custom mempool/concurrent-tx-executor, replacing the
current strictly-sequential `FinalizeBlock` path — which is explicitly out
of scope for this document and is not silently implied as "now solved" by
anything in §8. The chain's "AEZ preserves throughput under load" goal is
separately already addressed by an unrelated, already-in-flight admission-
control/throughput-preservation feature (task #40 in this workflow's own
tracker) — §8's fixes are a genuine, independently valuable correctness/
performance improvement on their own terms, not a parallelism unlock.

---

## 9. Adversarial self-check: proving, not asserting, there is no cross-contract fund-movement path

The single most important property this document claims is that §1
(intra-contract CALL/RET) cannot reproduce any of v1–v4's bug classes,
because it never crosses a contract boundary at all. That claim is checked
here by tracing exactly what data §1's new opcodes touch, rather than by
asserting care was taken.

**What `OpCall`/`OpRet` read or write, exhaustively**: `pc` (control flow,
already existed); a new `[]uint32` return-address stack (pure control
metadata — return addresses, i.e. `uint32` code offsets — never a `Storage`
key, a balance, or an address); the existing `stack []RuntimeValue`
(argument values and the return value, already existed, already the
mechanism every opcode already uses to pass data); the existing `locals
[]RuntimeValue` (already existed, now spans function-local slot ranges
too). That is the complete list — verified by reading every line of the
proposed `OpCall`/`OpRet` `Run()` cases in §1.2/§1.3 against what `Run()`
already has in scope at that point in the loop (avm.go:873-897's local
variable declarations: `originalState`, `state`, `stack`, `locals`,
`randomNonce`, `exec`, `readOnly`, `gasLimit` — no field among these is a
second contract's address, balance, or storage, and §1 introduces exactly
one new local, the return-address stack, of the same non-contract-
identifying shape). **None of `RuntimeContext.OriginalBalance`,
`AttachedValue`, `ContractAddress`, or any bank-keeper reference is ever
read or written by `OpCall`/`OpRet`** — the entire class of bug that killed
v1–v4 (wrong balance baseline, eager-vs-deferred transfer, recipient
overflow, stale cached balance) requires touching one of those four things,
and §1's opcodes touch none of them, structurally, not by discipline. This
is the strongest form the safety argument can take: not "we were careful
around balances," but "the data these opcodes can even reference doesn't
include a balance, a second address, or a second contract's storage map at
all" — verified by exhaustive enumeration of what's in scope, not by
review of what was written.

**Does `Run()` ever resolve a second `Module`/`Storage` for §1?** No —
checked directly: the only place `Run()` calls `NewVerifier`/`DecodeModule`
is once, at the top (avm.go:874-880), on the single `module` parameter
passed in for the whole call; `OpCall`'s target is a PC within that same
`module.Code` slice, never a lookup by contract address. §6
(`OpCallExternalGet`) is the **only** new opcode in this document that
resolves a second `Module`/`Storage` at all — and it is read-only by
construction (§6.1's exhaustive four-`if-readOnly`-guard check), so even
though it does cross a contract boundary, it cannot write across one.

**Trap/abort unwind, checked against a leaked-frame failure mode
specifically**: §1.5 argues the return-address stack needs no per-frame
undo log because there is no per-frame storage overlay to undo — checked
by confirming `rollback()` (avm.go:913-918) is a single closure reached
from **every** error return in `Run()` (every `return rollback(...)` call
site in the file, not a subset), and that closure references only
`exec`/`originalState` — it has no reference to the call stack at all,
meaning it is unreachable-and-discarded, not "reachable but forgotten to
unwind," on every one of those paths, including ones that fire from inside
a called function's body (which is just more iterations of the same loop,
with the same `rollback` closure in scope, since it's a closure over
`Run()`'s own local variables, not per-frame state).

**Does an adversarial, non-compiler-produced `Module` change any of the
above conclusions?** Checked in §1.6: raw bytecode can self-recurse via
`OpCall`, which the compiler's own output never would (recursion-free by
construction, §1.1) — but self-recursion still only manipulates the same
three things (the return-address stack, `stack`, `locals`), still never
references a second contract, and is bounded by the new runtime
`MaxCallDepth` check regardless of how the bytecode was produced. It cannot
turn §1 into a cross-contract mechanism no matter how adversarially it's
constructed, because `OpCall`'s target space is `module.Code`'s own index
range — there is no encoding of "jump to a different contract" available
to it at all; the instruction format has nowhere to put a contract address
even if an attacker wanted to.

**Conclusion of the self-check**: §1 has no cross-contract fund-movement
path because it has no cross-contract *reference* path — the opcodes
introduced cannot express "another contract" in their operands at all,
adversarial bytecode included. §6 does cross a contract boundary, and is
safe specifically because read-only execution is enforced by the same
four `if readOnly` guards that already protect every getter call in
production today, re-verified exhaustively (not sampled) this session.

---

## Summary of concrete artifacts this design specifies

- `avm.go`: `OpCall`, `OpRet`, `OpMakeTuple`, `OpCallExternalGet` (4 new
  opcodes, each needing its 5 registration sites); `Params.MaxCallDepth`
  and a separate `MaxExternalGetDepth` threading; a new
  `RuntimeContext.ExternalGetResolver` field; extend `runtimeFieldValue`'s
  `TagTuple` branch for arbitrary numeric index.
- `compile.go`: module-wide local-slot allocator (replacing per-entry
  reset); real per-function codegen reusing the entrypoint-lowering
  pipeline; two-pass call-target linking extending `relocateJumpTargets`/
  `patchJumpTarget`; `IRStmtRet` alongside `IRStmtReturn`; generic
  type-parameter parsing/monomorphization cache/name-mangling (self-
  referential generics still rejected pre-monomorphization by the existing
  name-keyed `validateFunctionRecursion`, now a stated invariant, §4.5);
  trait declaration/`impl`-block conformance checking (compile-time only),
  concrete-receiver dispatch resolved to a single-segment call **before**
  `validateFunctionRecursion` runs (§5.2) — trait-bounded generic dispatch
  deferred, not shipped.
- `parser.go`: destructuring `const (a, b) = ...`; tuple literal
  `(a, b)`; parenthesized multi-value return type; generic
  `<T, ...>` parameter lists on `fn`/call sites; `trait`/`impl` grammar.
- `types.go`: `Statement.Names []string`; new `ExprKind`s
  (`ExprTupleLiteral`); `FunctionDecl` type-parameter list; `TraitDecl`/
  `ImplDecl`; type-checker rejects a tuple `TypeRef` in struct/message
  field position (§2.7).
- `x/contracts/keeper/keeper.go`: new `k.txMu *sync.Mutex`, acquired once
  per top-level public mutating entrypoint (21 enumerated in §8.2, plus
  `loadForBlock` getting its own independent lock/unlock cycle and the
  wrapper adapters identified in §8.3 pulling their locking up to whichever
  method is topologically first on their call path), pre-implementation
  check re-derived from the wire-level Msg/Query surface for
  entrypoint-calling-entrypoint before enabling it.
- `x/contracts/types/{types.go,contract_state.go}`: unexported
  `computeContractsStateRootNormalized`, called by `RefreshStateRoot`
  directly, public `ComputeContractsStateRoot` unchanged for its other
  callers.

## Explicitly deferred, with reasons, not silently dropped

- Catchable per-call exceptions distinct from `return` (§3) — materially
  bigger feature, interacts with §1.5's safety argument in ways that would
  need their own review.
- Type inference for generics (§4.1) — explicit type arguments chosen
  instead; inference is strictly additive future work if ergonomics
  demand it.
- Trait-bounded generic dispatch — `x.method(y)` where `x`'s type is an
  in-scope generic type parameter (§5.1's originally-proposed "one case
  traits add real value") — deferred per §5.2: its concrete callee is only
  known post-monomorphization, after the whole-program recursion checker
  has already run on the un-substituted declarations, so allowing it today
  would let a trait-dispatch cycle bypass the exact acyclicity guarantee
  §1.1 depends on for its disjoint-locals-slot safety argument, reachable
  from ordinary (non-adversarial) source. Only direct dispatch on a
  concrete (non-generic) receiver type ships this pass.
- Trait-typed values / dynamic dispatch (§5.1) — needs an indirect call
  target, which is in direct tension with §1.2's compile-time-immediate
  `OpCall` target invariant that the rest of this design's safety argument
  leans on.
- Tuple-typed `@storage`/`@message`/`@event` struct fields (§2.7) — the
  wire codec's bare-array tuple encoding is incompatible with the generic
  field-access-off-raw-bytes path; tuples stay call/return/local-only this
  pass. A struct field that would be tuple-typed can be declared as an
  equivalent named struct instead.
- Fold-based incremental state-root hashing (§8.4) — correctly identified
  as tractable (dead `RootContribution`/`ValidateInvariants` callers,
  tautological `Validate()` root-check) but a larger change than the
  double-`Normalize()` fix; left as documented future work.
- Real concurrent zone execution (§8.5) — needs a `PrepareProposal`/
  custom-executor architecture change with nothing analogous in this
  codebase today; explicitly not implied as unlocked by §8's fixes.
