# AVM Tooling And Traceability

This document records the developer tooling surface for AVM and the concrete
traceability between spec requirements, implementation files, tests, and gates.
It is intentionally scoped to developer tooling and does not claim production
AVM runtime enablement.

## Tooling Surface

The `aetrad avm` command group exposes the current developer workflow:

- `avm compile`: compile source into canonical artifacts;
- `avm inspect`: inspect ABI, selectors, storage layout, state init, and lock data;
- `avm disasm`: disassemble a compiled module or source file;
- `avm gas`: report deterministic gas usage for the compiled module;
- `avm test`: compile and run deterministic smoke checks against the compiled module;
- `avm selectors`: export the canonical selector registry;
- `avm lsp`: provide editor diagnostics over stdio.

Clean artifact output is written to a dedicated directory per command, not as a
side effect in the compiler core. The canonical layout is:

- `module.bin`
- `module.chunk`
- `interface.json`
- `stateinit.json`
- `storage-layout.json`
- `selector-registry.json`
- `codecs.json`
- `diagnostics.json`
- `ir.json`
- `dependency-lock.json`
- `test-report.json` for `avm test`

Top-level JSON keys in every artifact are stable and sorted so artifact
diffs stay deterministic across compiler runs.

## Traceability Matrix

| Spec requirement | Implementation file | Test | Gate |
| --- | --- | --- | --- |
| Deterministic compile, canonical ABI/storage/layout/state init commitments | `x/aetravm/compiler/compile.go` | `x/aetravm/compiler/compile_test.go` | `go test ./...` |
| Selector registry and artifact layout stability | `x/aetravm/compiler/compile.go`, `x/aetravm/compiler/codec.go` | `x/aetravm/compiler/compile_test.go`, `x/aetravm/compiler/format_test.go` | `go test ./x/aetravm/compiler` |
| Runtime entrypoint dispatch, bounce handling, and determinism | `x/aetravm/avm/avm.go`, `x/aetravm/avm/scenario.go` | `x/aetravm/avm/security_test.go`, `x/aetravm/avm/scenario_test.go` | `go test ./x/aetravm/avm ./x/aetravm/async` |
| Forbidden host surface and reject-by-default policy | `x/aetravm/avm/host.go`, `x/aetravm/avm/security.go` | `x/aetravm/avm/security_test.go`, `x/aetravm/avm/proof_test.go` | `tests/avm_determinism_gate/gate_test.go` |
| CLI tooling for compile, inspect, disasm, gas, selectors, and LSP | `cmd/l1d/cmd/avm_tools.go`, `cmd/l1d/cmd/avm_compile.go`, `cmd/l1d/cmd/avm_artifacts.go`, `cmd/l1d/cmd/avm_lsp.go` | `cmd/l1d/cmd/avm_test.go` | `go test ./cmd/l1d/cmd` |
| Canonical reference examples | `examples/avm/counter_should_be.atlx`, `examples/avm/token/token_master.atlx` | `x/aetravm/compiler/compile_test.go` | `go test ./x/aetravm/compiler` |
| Release gate must not overclaim AVM production readiness | `docs/public-testnet-production-gates.md`, `docs/security/prototype-audit-gate.md` | `docs/testnet_kernel_test.go` | `go test ./docs` |

## Reference Examples

- `examples/avm/counter_should_be.atlx` covers deploy, external, internal,
  bounced, and getter surfaces in the canonical Aetralis syntax.
- The contract families under `examples/avm/` (`token/`, `nft/`, `dao/`,
  `dns/`, `stake/`) cover the full lifecycle; `examples/avm/token/token_master.atlx`
  exercises deploy, external, internal, bounced, and getter surfaces across a
  master/wallet contract pair.

The examples are intentionally small and deterministic. They exist to keep the
language surface and tooling surface anchored to real source files.
