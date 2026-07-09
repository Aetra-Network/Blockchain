# Aetra Virtual Machine

AVM is the native Aetra Virtual Machine research track for deterministic smart
contracts. The current implementation is split into two statuses:

- the language, compiler, executable spec, and developer tooling are
  contract-developer ready;
- the production runtime is enabled through the `x/contracts` keeper/module
  path and must satisfy the same deterministic, bounded, and adversarial
  constraints as the executable spec.

AVM is not a general-purpose OS runtime. It supports only deterministic
contract code and must not depend on filesystem access, network access,
process spawning, wall-clock timing, or ambient host state.

The canonical contract language for the AVM track is Aetralis v1, and the
canonical source extension is `.atlx`.

The executable spec lives in `x/aetravm/avm`; the production runtime wiring
and export/import live in `x/contracts` and `app`. The developer CLI lives in
`cmd/l1d/cmd`. No single surface alone is the whole runtime story.

For contract authoring style and practical ATLX rules, see
[`atlx-practical-guide.md`](atlx-practical-guide.md).

## Non-Negotiable Requirements

- binary serialization spec
- message ABI
- storage ABI
- deterministic execution proof
- gas schedule
- memory limits
- code size limits
- stack/register limits
- host function allowlist
- fuzz tests
- differential tests
- upgrade and migration policy
- adversarial audit

## Bytecode Format

The AVM bytecode format is deterministic and big-endian:

```text
magic               4 bytes, "AVM1"
version             uint16
metadata_hash       32 bytes
import_count        uint16
imports             repeated uint16 host function ids
export_count        uint16
exports             repeated (uint8 entrypoint, uint32 instruction offset)
instruction_count   uint32
instructions        repeated (uint8 opcode, uint64 arg, uint16 data_len, data)
```

The module code hash is `sha256(encoded_module)`. The verifier rejects malformed
headers, unsupported versions, oversized code, unknown imports, missing exports,
invalid export offsets, unknown opcodes, nondeterministic opcodes, and oversized
instruction data.

## Execution Entrypoints

AVM entrypoints are explicit:

- `deploy`
- `receive external`
- `receive internal`
- `receive bounced`
- `query/getter`
- `migrate`

Async messages use `receive internal` by default and `receive bounced` when the
message envelope has `bounced = true`. Missing bounced handlers fail
deterministically and must not create bounce/refund loops. Internal dispatch
is kind-first and then selector/opcode-decoded through the canonical ABI
descriptor for the message body.

## Message ABI

AVM receives the Aetra async message envelope:

- source
- destination
- value in `naet`
- opcode
- query id
- body
- bounce flag
- bounced flag
- created logical time
- deadline
- gas limit
- forward fee
- depth

AVM output messages are normal async `MessageEnvelope` values. They inherit
deterministic queue semantics from `x/aetravm/async`.

Canonical typed message bodies are bound through `@message(opcode) struct`
schemas; raw parsing remains available for compatibility and debugging, but it
is not the only supported path.

## Storage ABI

AVM storage is a per-contract deterministic typed root:

- each contract has exactly one canonical storage root type;
- the root schema is derived from the declared `@storage struct` schema, not
  from runtime lookup or anonymous fields;
- persisted state is committed through canonical records, not ad hoc runtime
  reflection;
- keys used in the underlying storage layer are byte strings with bounded
  length;
- stored values and snapshot payloads are canonical bytes derived from the
  storage schema;
- integer helpers use big-endian encoding;
- snapshots are sorted by key or canonical field order as appropriate;
- iteration must be bounded and paginated;
- exported snapshots must be deterministic;
- state size, nesting depth, chunk spillover, and memory limits are enforced
  before committing state.
- storage schemas use compiler-generated `toChunk` / `fromChunk` helpers to
  keep round-trips typed and deterministic.
- read-only chunk navigation uses `ChunkCursor`; it is not a storage
  commitment and must not be treated as mutable state.

## Host Function Allowlist

Allowed host functions:

- read storage
- write storage
- emit internal message
- inspect message envelope
- get block context
- charge gas
- return result code

Any host function outside the allowlist is rejected by the verifier.

## Forbidden Behavior

AVM bytecode and host functions must not allow:

- wall-clock time
- random host entropy outside consensus-approved randomness
- filesystem or network access
- floating point
- unbounded iteration
- nondeterministic map iteration

The executable verifier rejects the forbidden opcode set used to model these
classes.

## Gas And Limits

AVM execution is bounded by:

- gas schedule per opcode;
- per-message gas limit;
- max code bytes;
- max instructions;
- max imports;
- max stack depth;
- max memory bytes;
- max key/data sizes;
- async emitted-message limits;
- async storage-write limits.

Gas accounting is deterministic and local to the runner. When AVM is wired into
keepers, keeper gas and AVM gas must be reconciled without allowing non-`naet`
protocol fees.

## Toolchain

Required AVM toolchain components:

- bytecode verifier
- disassembler
- local runner
- gas profiler
- contract test harness
- state snapshot inspector

The current pure Go package includes the verifier, deterministic encoder and
decoder, local runner, storage snapshot encoder, async handler adapter,
disassembler, and gas profiler. The developer CLI exposes `avm compile`,
`avm inspect`, `avm disasm`, `avm gas`, `avm test`, `avm selectors`, and
`avm lsp` as non-production tooling.

Developer tooling is not the same as production wiring: the executable spec and
tooling validate behavior, while `x/contracts` provides the production keeper
and module path used by the app.

## Runtime Path

AVM production wiring is provided through the `x/contracts` keeper/module path.
It must provide:

- store code
- instantiate contract
- route external message
- process internal queue
- execute getters
- export/import state

Runtime wiring must reuse the async queue export/import semantics and must not
bypass address validation, zero-address rejection, `naet` fee policy, signer
checks, malformed transaction handling, or genesis validation.

Runtime evidence includes:

- keeper/module wiring tests that cover store code, instantiate, execute,
  query, migrate, and export/import round-trips through the real module path;
- malicious contract tests that cover invalid bytecode, selector collisions,
  getter-only write rejection, bounce handling, and rollback on failure;
- gas model checks that prove gas is bounded and deterministic for the same
  source tree and input;
- state growth benchmarks that demonstrate bounded storage, queue, and receipt
  growth under repeated contract activity;
- adversarial tests that cover gas exhaustion, bounce storms, malformed
  payloads, partial side effects, and state corruption attempts.

## Required Tests

```powershell
go test ./x/aetravm/avm
go test ./x/aetravm/async
go test ./...
```

The current package tests cover:

- deploy valid contract module
- reject malformed bytecode
- reject oversized code
- reject nondeterministic opcode
- reject non-allowlisted host function
- run simple counter deterministically
- deterministic gas bound
- deterministic storage snapshot
- storage memory limit
- send internal message through async queue
- bounce failed message
- preserve state across runner calls
- preserve queue across export/import

## Language And ABI References

The source-language and ABI specs live alongside this document:

- [language-spec.md](language-spec.md)
- [abi-spec.md](abi-spec.md)
- [serialization-spec.md](serialization-spec.md)
- [message-model.md](message-model.md)
- [storage-model.md](storage-model.md)
- [selector-registry.md](selector-registry.md)

Fuzz tests, differential tests, keeper tests, and adversarial audit are required
before AVM can be enabled beyond the executable specification.

## Acceptance

Developer/toolchain acceptance:

- AVM can compile a minimal contract deterministically.
- AVM inspect/disasm/gas/test/selectors/LSP outputs are stable for the same
  source tree.
- AVM artifacts are canonical and reproducible.

Runtime acceptance is production-enabled through the keeper/module path:

- AVM gas is deterministic and bounded.
- AVM contracts can participate in the async message queue.
- AVM cannot weaken base-chain signer, fee, denom, zero-address, transaction,
  or genesis validation.
- AVM runtime stays production-enabled only while the keeper/module path,
  malicious contract suite, gas model checks, state growth benchmarks, and
  adversarial tests remain green on the real module surface.
