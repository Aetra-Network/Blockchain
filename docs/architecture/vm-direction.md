# VM Direction

Phase 11 defines the VM decision boundary for Aetra. The current project
status is:

- AVM language, compiler, executable spec, and developer tooling are ready for
  contract developers;
- AVM production runtime is enabled through the `x/contracts` keeper/module
  path and must still satisfy the same security, determinism, and adversarial
  evidence requirements.

AVM means Aetra Virtual Machine. CosmWasm is not a genesis requirement; it is
an optional gated compatibility layer that may be added only after explicit
security, gas, state-growth, export/import, and adversarial evidence.

## Genesis AVM Runtime

AVM is the native Aetra smart contract runtime target and is production-
enabled through the `x/contracts` keeper/module path. It matches Aetra's async
execution model, native `naet` fee model, contract standards, and bounded
execution requirements, which is why it remains the primary runtime surface.

Required genesis rules:

- AVM is the primary VM.
- AVM executes Aetra-native contracts and standards.
- AVM contract entrypoints are deterministic and versioned.
- AVM bytecode verification rejects malformed, oversized, unknown, or
  nondeterministic opcodes before execution.
- AVM gas metering is deterministic and bounded.
- AVM storage reads, storage writes, internal messages, scheduled
  continuations, getters, migrations, and bounced-message handlers are explicit
  entrypoints or host functions.
- AVM state snapshots, async queues, receipts, events, and contract metadata
  are export/import safe.
- AVM fees use native `naet` and must interact correctly with fee burn,
  storage pricing, forwarding fees, and staking reward accounting.
- AVM production enablement requires the security review, gas model review,
  state growth benchmarks, interaction tests with fee burn, interaction tests
  with staking rewards, export/import tests, and adversarial contract tests to
  remain green.

Current AVM implementation surface:

- `x/contracts`: production keeper/module wiring for store code, instantiate,
  execute, query, migrate, and export/import.
- `x/aetravm/avm`: executable bytecode spec, verifier, local runner, storage
  snapshot ABI, gas schedule, host function allowlist, forbidden opcode
  checks, and async handler adapter.
- `x/aetravm/async`: deterministic queue behavior, bounce behavior, fee/value
  denomination, limits, observability, and export/import.



## Optional CosmWasm Compatibility Layer

CosmWasm remains an explicitly gated optional compatibility layer already
reflected in `app/wasmconfig`. It must stay disabled by default.

Required optional-layer rules:

- CosmWasm is disabled by default.
- CosmWasm is enabled only by explicit config or feature gate.
- Upload permissions are explicit: governance-only by default, allowlist only
  for dev/test networks with a non-empty valid address allowlist.
- Instantiate permissions are explicit: code-owner-only by default, everybody
  only for explicitly gated dev/test networks.
- Instantiate, execute, query, and migrate support must exist before any public
  gated network enables the real `x/wasm` keeper.
- Admin and migration policy is explicit: migration requires the current
  non-zero contract admin.
- Gas limits are explicit and bounded.
- Contract size limits are explicit and bounded.
- Contract storage rent or storage pricing is explicit and cannot bypass AVM
  or base-chain storage economics.
- Memory/cache limits are explicit and bounded.
- Query limits are explicit and bounded.
- Events must be compatible with indexers.
- Localnet smoke tests are required.
- Malicious contract tests are required.
- Export/import tests with active contracts are required.
- Pinned code is disabled by default and governance-only if enabled later.
- Governance authority for enabling/disabling CosmWasm is explicit.
- CosmWasm contracts cannot bypass `naet` fee policy, address policy,
  zero-address policy, or genesis validation.

The current readiness policy defines:

- recommended wasmd version: `v0.70.2`
- recommended wasmvm version: `v3.0.6`
- recommended Cosmos SDK minor: `v0.54`
- max stored contract size: `800 KiB`
- max proposal contract size: `3 MiB`
- smart query gas limit: `3,000,000`
- simulation gas limit: `20,000,000`
- gas multiplier: `140,000`
- memory cache: `100 MiB`, hard cap `256 MiB`
- smart query response limit: `256 KiB`, hard cap `1 MiB`
- smart query depth limit: `8`, hard cap `16`
- pinned code count: `0` by default, hard cap `128`

CosmWasm readiness must not add a `wasm` store key, genesis state, module
account, CLI upload/instantiate/execute/migrate surface, or keeper wiring by
default. Real `x/wasm` integration requires the explicit gate and the full
security checklist.

## Future VM Rule

No additional future VM may be implemented before the R&D spec defines:

- binary serialization spec
- message ABI
- storage ABI
- gas schedule
- deterministic execution proof
- fuzz tests
- upgrade/migration policy
- adversarial audit

Production sharding is out of launch scope; the earlier sharding R&D
simulator was removed from the repo. Any future sharding work restarts from a
new R&D gate (spec, simulator, prototype keepers, adversarial tests, long-run
testnet, independent audit, consensus-safety proof) before production claims.

## Compatibility Boundaries

Supported program class:

- any deterministic smart contract that does not depend on filesystem access,
  network access, process spawning, wall-clock time, random host entropy, or
  other nondeterministic host state;
- contract families such as token, NFT, wallet, DNS/domain registry, DEX,
  staking-pool, farm, registry, bridge-like application logic, and other
  deterministic application-layer systems that can be lowered into the AVM
  execution model.

Unsupported program class:

- general-purpose programs that require OS behavior, random external I/O,
  process management, or nondeterministic timing;
- programs that require ambient host state as an input to consensus behavior;
- protocol-layer staking, validator set management, voting power, or slashing
  logic, which remain native protocol responsibilities outside AVM.

In AVM, staking means application-layer contract logic only:

- staking pool contracts;
- vesting and vault contracts;
- farms and reward accounting contracts;
- related reward distribution and share accounting logic.

It does not mean protocol staking or consensus staking. Validator set changes,
voting power, and slashing remain consensus-layer responsibilities and must not
be redefined as contract behavior inside AVM.

Contract standards are AVM-native first, but their message schemas must remain
explicit enough for future gated adapters.


Every contract standard must define:

- explicit storage schema
- explicit inbound messages
- explicit outbound messages
- explicit getters
- explicit unknown-message policy
- explicit bounce behavior
- explicit fee behavior
- explicit deployment behavior

If CosmWasm is enabled later, CosmWasm message handlers must conform to these
standard packages or declare explicit adapter semantics. CosmWasm must not
become the default path silently.

## Acceptance Gates

- AVM language/tooling is contract-developer ready.
- AVM production execution is enabled through the real keeper/module path and
  must keep security review, gas review, state growth benchmarks, fee burn
  tests, staking reward tests, export/import tests, and adversarial contract
  tests green.
- CosmWasm readiness does not weaken base chain security.
- CosmWasm remains disabled by default unless an explicit compatibility gate is
  enabled.
- Contract standards can be tested independent of optional compatibility
  layers.
- `go test ./app/wasmconfig`
- `go test ./x/aetravm/async`
- `go test ./x/aetravm/avm`
