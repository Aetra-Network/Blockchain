# Phase 9 Aether Core Wiring Gate Security Notes

## Scope

Phase 9 wires accepted prototype modules into Aether Core as disabled-by-default
SDK modules:

- `x/load`
- `x/routing`
- `x/mesh`

The wiring adds store keys, keepers, module manager registration, default
genesis, validation, export, and migration skeletons. It does not add public Msg
services, contract execution, production sharding, or cross-zone settlement.

## AEZ Phase 2 amendment: `x/aez` is no longer a prototype module

`x/aez` was listed above as a prototype module through Phase 1. **AEZ Phase 2
promoted it into `systemModules`**, exactly as `x/contracts` was promoted before
it. This section records what that changed and why, because the promotion is the
one thing this document must not describe stalely.

### Why the promotion was necessary, not convenient

Phase 2's requirement is a routing table that governance can change and that
swaps deterministically at a routing-epoch boundary. The swap must happen in a
block-lifecycle hook — it is triggered by height, not by a transaction.

The prototype family is *defined* by having no such hook. So a BeginBlocker
could not land on a prototype `x/aez` at all: the family invariant forbids it.
**The promotion and the BeginBlocker are necessarily a single change.** They are
in a single commit for that reason.

### What the gate now enforces about the prototype family

Previously "prototype modules do not implement BeginBlocker or EndBlocker" was
asserted only by `app/aetra_core_wiring_test.go`. A prototype module could
therefore grow a block-lifecycle hook and reach a production binary with nothing
but a unit test in the way.

Phase 2 moved that check **into the gate itself** (`app/aetra_core_wiring.go`),
where it panics the binary at startup. Adding a Begin/EndBlocker to a prototype
module without also promoting it out of `prototypeModules` is now a startup
panic on every node rather than a red test. This is a strengthening, and it is
what makes "the promotion and the BeginBlocker are one change" a structural fact
rather than a review convention.

### What the promotion did *not* change

- **No module account, and never** (I-10/I-11). The gate applies the identical
  module-account prohibition to system modules that it applies to prototypes.
  The promotion bought `x/aez` no custody relief whatsoever. `x/aez` moves
  messages, never money.
- **Routing execution point is still `ANTE_ADMISSION_ONLY`**, still hard-rejected
  otherwise. The table is governable; **nothing routes on it.** No tx path, no
  ante rejection, and no ordering decision consults `ZoneOf`.
- **Genesis is unchanged**: all 256 buckets map to zone 0, so every entity
  resolves to the Core Zone and execution semantics are bit-identical.
- **No EndBlocker.** The Phase 4 message-bus drain does not exist.
- **Keeper is still `storeService`-only** — no `k.genesis`, no `loadForBlock`.
  The F-17 fork class remains structurally unreachable (I-20).

### The honest statement of what this gate guarantees

The gate guarantees the declared module sets are wired consistently (registered,
store key mounted, counts paired) and hold no custody, and that prototype modules
have no block-lifecycle hook.

**It does not, and never did, guarantee that any module is dormant.** Dormancy
was a property of `x/aez`'s membership in the prototype family, not a check this
gate performed. Phase 2 ends that dormancy deliberately. What contains `x/aez`
now is not the gate; it is the three properties below.

## What contains `x/aez` after Phase 2

1. **Governance-only authority.** `MsgUpdateRoutingTable` requires
   `Params.Prototype.Authority`, which defaults to the **gov module account**
   (`aeztypes.GovAuthority()`), not `prototype.DefaultAuthority`. The keyless
   all-zero sentinel was rejected deliberately: it would make the handler
   permanently unreachable — the bug `x/nominator-pool` shipped and had to patch
   around in genesis (`cmd/l1d/cmd/testnet_genesis.go`).
2. **The Core Zone is a one-way trap.** `ValidateRoutingTableTransition` rejects
   any table that moves a bucket currently mapped to zone 0. Since genesis maps
   *all* 256 buckets to zone 0, **the only table governance can currently stage
   is one whose bucket map is identical to the current one** — a no-op that bumps
   Version/Epoch/ActivationHeight. That is intentional for Phase 2: the
   governance surface, the epoch swap and the observability all ship and are
   exercised, while the ability to actually relocate an entity waits for the
   phase that gives entities per-zone state to be relocated into
   (`x/native-account`, Phase 3). Relaxing this rule is that phase's deliberate
   decision.
3. **Core-pinned namespaces bypass the table entirely.** `CorePinned`
   short-circuits before the bucket hash and before the table is read, so no
   table version — including a hand-crafted malicious one — can express a
   Core-Zone move (I-9).

## Routing Decision Point

Routing is fixed to `ANTE_ADMISSION_ONLY` for this gate. That means routing is an
auditable admission/classification spec and is not executed in
`PrepareProposal`, `ProcessProposal`, `FinalizeBlock`, or a production Msg
server. A later coordinated upgrade must explicitly change this policy before
routing can mutate consensus state.

`x/aez`'s Msg service does **not** change this. It mutates the routing *table*,
which is `x/aez`'s own committed state; it does not route anything, and no
execution path reads the table.

## Consensus Safety

- Prototype feature gates are disabled in default genesis.
- Prototype modules have no module account permissions.
- Prototype modules do not implement BeginBlocker or EndBlocker — **enforced by
  the gate**, not only by tests.
- BeginBlocker and EndBlocker order lists include prototype module names as
  explicit no-op lifecycle positions.
- System modules have no module account permissions either, unless the name is a
  reserved system module account. `x/aez` is not one and must never become one.
- Aether Core does not execute smart contracts or application logic.
- Contract-zone execution remains target architecture, not live behavior.

### `x/aez` BeginBlocker safety

- **Begin, not End.** BeginBlock at height H runs before any transaction at H, so
  a table stamped `ActivationHeight = H` is the table every transaction at H
  resolves against: all of block H sees exactly one table. An EndBlocker would
  make the committed activation height off-by-one from the height the table
  actually takes effect.
- **Position unchanged.** `app/wiring/aetracore/order.go` already listed `aez`
  after config/config-voting/aetracore/load/routing and before mesh, payments,
  the schedulers, actor-registry, contracts, storage-rent and identity-root. No
  ordering change was needed.
- **Deterministic.** It reads only committed store values and `ctx.BlockHeight()`
  — no wall clock, no randomness, no map iteration (I-22).
- **Silent no-op when idle.** With no pending table it is a single store read
  returning `(false, nil)`, so a chain that never touches the routing table can
  never fail a block because `x/aez` exists (I-23).
- **Gov interaction is collision-free.** Gov proposals execute in gov's
  EndBlocker, so a `MsgUpdateRoutingTable` passing at H writes the pending table
  at EndBlock H and the earliest BeginBlock that can activate it is H+1 —
  consistent with the "strictly future, exact epoch boundary" staging rule.

## Security Audit Notes

- No randomness, wall-clock time, goroutines, external API calls, or local
  latency inputs were added to app lifecycle paths.
- Store-backed genesis import/export validates state before write and after
  read.
- Export/import state remains deterministic for disabled modules.
- Query response bounds remain in the prototype keeper layer.
- Migration handlers are no-op validators from consensus version `1` to `2`.
- `x/aez`'s hand-written Msg descriptor declares real fields and the
  `cosmos.msg.v1.service` option, and its signer is resolved through
  `signing.Options.CustomGetSigners` (`app/keeperconfig/tx.go`), which feeds both
  TxConfig constructions so they cannot drift. This is guarded on the wire, not
  only in Go: `app/aez_msg_wire_format_test.go` marshals the message and resolves
  its signer through the app's own codec. Field-less hand-rolled descriptors have
  twice produced empty marshals and node-side Unmarshal panics in this tree
  (`x/contracts`, `x/nominator-pool` — the latter found live).

## Remaining Production Gates

- Add protobuf Msg/Query contracts only after API review.
- Add prefix-bounded KV iteration before public queries.
- Add production routing persistence only through governance and software
  version gate.
- Re-run determinism, export/import, localnet restart, and long-run testnet
  checks before enabling any mutating prototype feature.
- Keep public docs wording as `experimental sharding` until the production gate
  passes.
- **Before Phase 3 relaxes the Core-Zone trap**, re-verify that no execution path
  reads `ZoneOf`, and that a bucket leaving zone 0 has somewhere to land.
- **`x/aez` now adds consensus-reachable state transitions** (the
  pending/current pointer). On an existing chain, promoting it and adding the
  BeginBlocker need a deliberate `ConsensusVersion` and migration decision — do
  not claim a no-op.
