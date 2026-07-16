# AEZ: Aetra Elastic Zones (Phase 0 Design)

Deterministic zoned execution inside **one** chain.

AEZ partitions state and execution into zones that live under a single
CometBFT consensus, a single validator set, a single height, and a single
global state root. **Every validator executes every zone.** There are no
separate chains, no per-zone committees, no masterchain, and no cross-shard
consensus. AEZ is *not* sharding — it is a container model for state and
execution inside one replicated state machine.

| Concept | Definition |
| --- | --- |
| Core Zone | `zone_id = 0`. Validators, elections, staking, slashing, governance, nominator pool, protocol params, upgrades, the routing table itself, and the native DNS registry. Never migrates. Holds a reserved gas budget. |
| Elastic Zone | `zone_id = 1..4`. Each hosts **both** native accounts (native executor) and Aetralis contracts (AVM executor). A zone is a state+execution container, not an app type. |
| Bucket | One of 256 virtual buckets. `bucket_id = Hash(namespace ‖ canonical_entity_id) % 256`. |
| Routing table | Versioned `bucket → zone` map. Mutable **only** at routing-epoch boundaries. |
| Cross-zone message | No direct foreign writes. Outbox → inbox, delivered no earlier than `H+1`, exactly-once, bounce/refund on failure. |
| Hybrid DNS | Native registry in the Core Zone + Aetralis resolver contracts in elastic zones. |

This document is Phase 0: what exists today, what it costs to get to AEZ, and
where the AEZ specification **conflicts with the code as written**. Every
claim below is cited to `file:line` and was verified against the working tree
at the time of writing.

## 1. Current architecture

### Consensus and ABCI surface

The app is a **plain baseapp** (`app/app.go:78`). It is deliberately minimal
at the consensus boundary:

- **No `PrepareProposal`.** No `ProcessProposal`. No custom mempool. No
  `SetPrepareProposal` / `SetProcessProposal` / `SetMempool` call exists
  anywhere in the tree.
- A vote-extension handler *type* exists (`app/abcihandlers/vote_extension.go:42`,
  `:47-48` `SetHandlers`) but is **never wired from app construction** — only
  tests call `NewVoteExtensionHandler()` (`app/abci_test.go:12`,
  `app/abcihandlers/vote_extension_test.go:13`). This is documented as
  intentional in `app/consensus_params_test.go:42-46` and
  `docs/security/determinism-gate.md:33`.
- Optimistic execution is on (`app/app.go:68`) and the real block gas meter is
  explicitly re-enabled (`app/app.go:76`); the comment there notes the app does
  **not** call `SetBlockSTMTxRunner`, so there is no parallel tx runner.

Consequence for AEZ: **there is no existing hook where a proposer could order
or partition work by zone.** Everything AEZ does must happen inside
`FinalizeBlock` — deterministically, on every node, from committed state.

### The wiring gate

`app/app.go:101-103` calls `ValidateAetraCoreWiringGate()` and **panics** on
error. That gate's first check (`app/aetra_core_wiring.go:9-11`) rejects any
routing execution point other than `ANTE_ADMISSION_ONLY`, and
`ANTE_ADMISSION_ONLY` is the *only* value the type defines
(`app/wiring/aetracore/modules.go:39-45`). The gate additionally requires that
every declared prototype/system module be registered and have its store key
mounted (`app/aetra_core_wiring.go:18-29`, `:35-46`), and forbids module-account
permissions on those modules unless the name is a reserved system module
account (`:26-28`, `:43-45`).

### Module families

| Family | Source | Status |
| --- | --- | --- |
| Prototype modules (16) | `app/wiring/aetracore/modules.go:47-64` | Disabled by default (`prototype.DefaultParams()` → `Enabled: false`). Includes the entire zone stack: `x/aetracore`, `x/load`, `x/routing`, `x/zones`, `x/mesh`. |
| System modules (15) | `app/wiring/aetracore/modules.go:66-88` | Live. `x/contracts` graduated into this set (`:81-87`) with live Msg/Query services and a default-off EndBlocker drain. |
| SDK modules | `app/wiring/aetracore/order.go:65-154` | auth, bank, staking, gov, distribution, slashing, evidence, upgrade, epochs, protocolpool, … |

### Two keeper storage patterns

This is the single most important architectural fact for AEZ:

1. **Blob genesis (the majority).** A keeper holds the whole module state in a
   `k.genesis` struct in RAM, `json.Marshal`s it into **one** store key on
   write, and `json.Unmarshal`s it all back on read. **15 modules** define
   `loadForBlock` this way: `x/actor-registry`, `x/config`, `x/config-voting`,
   `x/constitution`, `x/contracts`, `x/evidence`, `x/nominator-pool`,
   `x/reporter`, `x/scheduler`, `x/single-nominator-pool`, `x/storage-rent`,
   `x/system-registry`, `x/validator-election`, `x/validator-insurance`,
   `x/validator-registry`.
2. **Per-entity KV (exactly one module).** `x/native-account` writes one key
   per account plus secondary indexes (`x/native-account/keeper/keeper.go:167-220`
   `SetAccount`, index helpers `:411-458`).

### Two value systems

- **Native balances are real `x/bank` coins.** The native `Account` struct has
  **no balance field at all** (`x/native-account/types/account.go:28-44`).
- **Contract balances are `uint64` bookkeeping** inside the contracts blob
  (`x/contracts/types/contract_state.go:72`).

"Value conservation" therefore means two different things in two different
places, checked by two different mechanisms.

### The dead zone stack

`x/zones`, `x/routing`, `x/mesh`, `x/load`, and `x/aetracore` are prototype
modules (`app/wiring/aetracore/modules.go:47-64`) that:

- register **no Msg servers and no Query servers** — a tree-wide grep for
  `RegisterMsgServer` / `RegisterQueryServer` under those five modules returns
  nothing; `x/zones/module.go:47-53` `RegisterServices` registers only a state
  migration;
- mutate `k.genesis` **in RAM and never touch the store**
  (`x/zones/keeper/keeper.go:116-150` `RegisterZone`/`ActivateZone`/`AppendCommitment`;
  `x/routing/keeper/keeper.go:138-154` `SetRoutingTable`).

Nothing they compute ever reaches the AppHash. Everything they hold is lost on
restart. They are executable specifications, not modules.

### The duplicate kernel

`x/aetracore` contains a **complete AEZ kernel** with **zero callers**:

- `x/aetracore/types/commitment.go:11-23` — `ZoneCommitment` with `InboxRoot`,
  `OutboxRoot`, `ReceiptsRoot`, `ShardRootsRoot`.
- `x/aetracore/types/commitment.go:25-39` — `GlobalStateRoot`.
- `x/aetracore/types/layout.go:96-101` — `RoutingTableCommitment`.
- `x/aetracore/keeper/keeper.go:210-262` — `PrepareKernelProposal`,
  `ProcessKernelProposal`, `PrepareKernelABCIProposal`,
  `ProcessKernelABCIProposal`, `FinalizeKernelBlock`, `FinalizeKernelABCIBlock`,
  `CommitKernelABCIBlock`.

Those symbols appear in exactly seven files: their own package, their own
tests, and `THIRD-AUDIT-REPORT.md`. No production path calls any of them. And
the keeper mutates `k.genesis` in RAM (`:246`, `:258`) like the rest of the
prototype stack.

## 2. Current transaction/block execution flow

| # | Stage | Code | Zone-relevant behaviour |
| --- | --- | --- | --- |
| 1 | Proposal | CometBFT default | No app hook. Proposer cannot see, order, or partition by zone. |
| 2 | Tx decode | `txConfig.TxDecoder()` (`app/app.go:78`) | — |
| 3 | Ante / admission | `app.setAnteHandler` (`app/app.go:114`); `x/fees/types/fee_model.go:157-172` `ValidateAdmission` | The **only** place "routing" is permitted to run today (`ANTE_ADMISSION_ONLY`). Enforces `MaxTxGas` (`:165-167`), `MaxBlockTxs` (`:168-170`), cumulative `MaxBlockGas` (`:171-173`). |
| 4 | `PreBlock` | `app/block_lifecycle.go:13-15` → `ModuleManager.PreBlock`; order `app/wiring/aetracore/order.go:65-67` (upgrade, auth) | Upgrades run first. |
| 5 | `BeginBlock` | `app/block_lifecycle.go:17-19`; order `order.go:69-111` | Prototype zone modules are in the order list (`:94-98`) but no-op while disabled. |
| 6 | Tx execution | baseapp `FinalizeBlock` | Sequential. One global multistore. Any keeper may write any store it holds a handle to. |
| 7 | `EndBlock` | `app/block_lifecycle.go:21-41`; order `order.go:113-154` | Module EndBlockers, then `maybeFinalizeNativeEmissionEpoch` (`:26-28`), then non-state observability (`:32`) and the invariant re-check (`:39`). `x/fees` and `x/fee-collector` run **last** (`order.go:151-152`). |
| 8 | Contracts drain | `x/contracts/keeper/keeper.go:2197-2249` | `loadForBlock` **first** (`:2205`), then budget check (`:2208`). Default budget is 0 ⇒ inert. |
| 9 | Validator updates | `app/block_lifecycle.go:43-52` → `app/elector_validator_updates.go:18` | **Rewrites `ValidatorUpdates` after module EndBlock**, from `x/validator-election` state, overriding what `x/staking` emitted. |
| 10 | Commit | baseapp | One AppHash for the whole multistore. |

## 3. Integration points

| Integration point | File:line | Why AEZ touches it |
| --- | --- | --- |
| Baseapp construction | `app/app.go:78` | Store key mounting for `x/aez` (`app/app.go:108` `MountKVStores`). |
| Wiring gate | `app/app.go:101-103`, `app/aetra_core_wiring.go:9-11`, `:18-29` | `x/aez` must be declared, registered, and store-mounted or the node **panics at startup**. |
| Routing execution point | `app/wiring/aetracore/modules.go:39-45` | Any move past ante-admission requires editing this const **and** the gate. |
| Prototype / system module sets | `app/wiring/aetracore/modules.go:47-64`, `:66-88`, `:98-117`, `:123-141` | Where `x/aez` gets declared. Note the module-account rule (`app/aetra_core_wiring.go:26-28`). |
| Module order | `app/wiring/aetracore/order.go:65-154` | `x/aez` drain must run at a fixed, deterministic position. |
| Block lifecycle | `app/block_lifecycle.go:13-52` | The only place AEZ can execute. |
| Validator-update override | `app/elector_validator_updates.go:18` | Proves the Core Zone owns the validator set post-EndBlock. Must never be zone-partitioned. |
| Bucket hash construction (reuse) | `x/routing/types/routing.go:234-251` `AssignShard` | Correct hash *shape* (domain-separated SHA-256, big-endian fold). Re-domain it; use `% 256`, **not** `% activeShards`. |
| Canonical entity extraction (reuse) | `x/routing/types/routing.go:362-406` `Locality.PrimaryActor` | The pattern for pulling a canonical entity id out of a tx. |
| Message / receipt / replay model (reuse) | `x/mesh/types/types.go:76-143` | `MeshMessage` (has `Sequence` `:90`, `Nonce` `:82`), `MeshReceipt` `:102-115`, `ReplayMarker` `:117-122`, `BounceReceipt` `:124-133`, `RefundReceipt` `:135-143`. The right shape. |
| Drain skeleton (reuse) | `x/contracts/keeper/keeper.go:2197-2249` | Gas budget (`:2211`), queue snapshot (`:2220`), panic recovery (`:2288-2293`), bounded queue (`x/contracts/types/api.go:26` = 65536). |
| Load-before-read fork analysis (reuse) | `x/contracts/keeper/keeper.go:2184-2196` | The F-17 write-up. Required reading; the exact bug class `x/aez` must be structurally immune to. |
| Canonical identity | `app/addressing/derivation.go:50-52` `NormalizeToAccountIdentity` | Idempotent; what activation records under. The correct `canonical_entity_id` for native accounts. |
| Address encoding | `app/addressing/codec.go:16-35` | `AE…` user-facing (`:26`) vs `ae1…` bech32 raw (`:22`). **Hash identity bytes, never display strings.** |
| Gas params | `x/fees/types/fee_model.go:22-23` (`MaxTxGas` 1,000,000; `MaxBlockGas` 20,000,000) | The budget AEZ subdivides into per-zone quotas. |
| Congestion → load | `x/fees/keeper/congestion.go:35-61` | Already feeds `x/load` (`:53-59`); the natural per-zone metering point. |
| Native registry | `x/identity-root/types/state.go:55-160` | `NameRecord`, `ResolverRecord`, `ReverseRecord`, subdomains, reserved names. Core Zone DNS. |
| Resolver examples | `examples/avm/dns/dns_registry.atlx`, `dns_record.atlx` | Examples only. Not deployed, not wired. |

## 4. Risks and incompatibilities

Stated plainly. Several of these are not "risks" — they are walls.

### 4.1 The blob-genesis wall (blocking, and it is the whole project)

`x/contracts/keeper/keeper.go:2793-2806` `writeGenesis` `json.Marshal`s the
**entire module state** — every contract, every code entry, every queued
internal message, every receipt — into **one** store key. `:2769-2791`
`loadForBlock` reads all of it back, per handler, per block.

**One key is one leaf is one root.** You cannot prefix-split it. You cannot
overlay a zone prefix on it. You cannot iterate it. You cannot give zone 1 and
zone 3 different sub-roots. A "per-zone contract state" does not exist and
cannot be made to exist while this shape holds.

The partial mitigation already in-tree does not help:
`x/internal/prefixgenesis/store.go:79-121` `Save` splits the blob **per struct
field**, not per entity — it walks `source.NumField()` (`:98`) and writes one
key per exported field (`:107-117`). It bounds write amplification. It does not
give you per-entity keys, per-entity roots, or per-zone anything.

`x/native-account` is the **only** module in the tree already shaped correctly
(`x/native-account/keeper/keeper.go:167-220`, `:411-458`).

**Conclusion:** AEZ for contracts *is* the x/contracts keeper rewrite. There is
no cheaper path, no adapter, no overlay. Phases 1–4 can deliver zoned *native
accounts*. Contracts cannot be zoned until Phase 5 lands. Any plan that claims
otherwise is wrong.

### 4.2 The dead zone stack is not a head start

`x/zones`, `x/routing`, `x/mesh`, `x/load`, `x/aetracore` look like 5 modules of
completed work. They are 5 modules of completed *prose*. No Msg servers, no
Query servers, and mutators that write RAM (`x/zones/keeper/keeper.go:116-150`;
`x/routing/keeper/keeper.go:138-154`). Nothing they compute reaches the
AppHash; everything is lost on restart.

Reuse the **types** (`AssignShard`, `Locality.PrimaryActor`, the mesh
message/receipt/replay shapes). Do not reuse the keepers. Do not "enable" them.

### 4.3 The duplicate x/aetracore kernel (audit hazard)

`x/aetracore` already implements ZoneCommitment with inbox/outbox roots
(`types/commitment.go:11-23`), a GlobalStateRoot (`:25-39`), a
RoutingTableCommitment (`types/layout.go:96-101`), and a full kernel ABCI
lifecycle (`keeper/keeper.go:210-262`) — with **zero callers**.

Building `x/aez` while `x/aetracore` stays in-tree means **two zone kernels**,
one live and one dead, with overlapping vocabulary. That is a maintenance trap
and an audit trap: a reviewer cannot tell which one is authoritative, and
`ZoneCommitment` will mean two different things depending on the import path.

Do not build `x/aez` on top of `x/aetracore`. Its kernel assumes a
proposal/committee model AEZ explicitly rejects, and its keeper is
`k.genesis`-in-RAM. **One of the two must be deleted.** Recommendation: quarantine
`x/aetracore` behind a deprecation note in Phase 1 and delete it in Phase 4,
after `x/aez` has absorbed the type shapes worth keeping.

### 4.4 App-type zones vs container zones (the model is inverted)

The existing code models zones as **application types**:

```
x/zones/types/types.go:19-24
    ZoneIDFinancial   ZoneID = "FINANCIAL_ZONE"
    ZoneIDIdentity    ZoneID = "IDENTITY_ZONE"
    ZoneIDApplication ZoneID = "APPLICATION_ZONE"
    ZoneIDContract    ZoneID = "CONTRACT_ZONE"
```

and routes **by message type**: `x/routing/types/routing.go:133` `ClassifyTx(msgType)`
→ `TxClass`, then `:217-232` `ZoneForClass(txClass)` → `ZoneID`. A financial
message goes to the financial zone; a contract message goes to the contract
zone.

AEZ is the exact opposite. A zone is a **container** holding both native
accounts and contracts, and the destination is a function of the **entity**,
not the message. Under AEZ, two `MsgSend`s route to different zones because
their *senders* live in different zones. Under `x/routing`, they always route
to the same zone because they are the same message type.

`ZoneForClass` and the `ZoneKind` taxonomy (`x/zones/types/types.go:26-33`) are
**not reusable as-is** and must not be adapted. Only `AssignShard`
(`x/routing/types/routing.go:234-251`) and `Locality.PrimaryActor` (`:362-406`)
survive the model change.

### 4.5 The wiring gate panics

`app/app.go:101-103` panics if `ValidateAetraCoreWiringGate` errors, and that
gate hard-rejects any routing execution point except `ANTE_ADMISSION_ONLY`
(`app/aetra_core_wiring.go:9-11`), which is the only value that exists
(`app/wiring/aetracore/modules.go:39-45`).

Practical consequences:

- Registering `x/aez` without adding it to both a module-name list **and** the
  matching store-key list (`modules.go:47-64` / `:98-117`, or `:66-88` / `:123-141`)
  is a **startup panic**, not a test failure. The counts are compared directly
  (`app/aetra_core_wiring.go:14-16`, `:32-34`).
- A prototype/system module **must not** hold module-account permissions unless
  its name is a reserved system module account (`:26-28`, `:43-45`). This bites
  AEZ directly: escrowing native value for a cross-zone transfer needs a module
  account. So either `x/aez` graduates into `systemModules` the way `x/contracts`
  did (`modules.go:81-87`) with a reserved account name, or cross-zone native
  value moves through `x/bank` under Core Zone authority and `x/aez` carries no
  custody at all. **Recommendation: the latter** — `x/aez` moves messages, never
  money.

### 4.6 The message bus does not do what AEZ requires

| AEZ requirement | Reality |
| --- | --- |
| Delivery no earlier than `H+1` | The drain loop (`x/contracts/keeper/keeper.go:2216-2244`) **never compares `msg.Height` to `ctx.BlockHeight()`**. It checks gas and nothing else. Same-block delivery is possible. |
| `DeliverAtBlock` schedules delivery | It does not. `:2347-2350` sets `msgHeight` from `envelope.DeliverAtBlock`, which feeds `Height` into `ComputeInternalMessageID`. Since nothing reads `Height` at drain time, **`DeliverAtBlock` only perturbs the message id**. It is a scheduling API that does not schedule. |
| Exactly-once via processed marker | Replay protection is **dequeue-by-id** (`:2121-2142`): find the entry whose id matches and drop it. There is no processed set. A message that is not in the queue is simply "missing" (`:2139-2141`), indistinguishable from one already handled. |
| Per-source sequence | Does not exist. `ComputeInternalMessageID` (`x/contracts/types/contract_state.go:822-849`) hashes **content + `Height` + `LogicalTime`**. `LogicalTime` is caller-overridable (`keeper.go:2352-2353`), so two byte-identical messages sharing `Height` and `LogicalTime` produce the **same id**, and dequeue-by-first-match (`:2127`) makes "which one was delivered" ambiguous. A processed-marker set keyed on this id could not distinguish a retry from a replay. |

Exactly-once needs `message_id = H(domain ‖ src_zone ‖ src_entity ‖ src_seq ‖ …)`
with `src_seq` a stored, monotonic, **non-caller-controllable** counter. That is
a new mechanism, not a patch.

### 4.7 Cross-cutting writers contradict "no cross-zone writes"

Several live paths write across module boundaries in a single call:

- `app/native_economy.go:35-61` (entry points) → `:63+` (`finalizeNativeEconomyEpoch`)
  reads `x/emissions` params (`:76`), previews emission (`:85`), reads and
  re-sizes `x/mint-authority` state (`:90-104`), mints into the fee-collector
  module account (`:118-126`), and continues into the fee-collector and
  nominator-pool distribution.
- `x/nominator-pool/keeper/keeper.go:247-256` sends coins via `x/bank` (`:247`),
  looks up a validator in `x/staking` (`:250`), and **delegates into `x/staking`**
  (`:254`).
- `x/contracts/keeper/keeper.go:2576-2593` sends coins from a contract creator's
  account into the fee-collector's storage-rent reserve module account
  (`storageRentReserveModule = "feecollector_storage_rent_reserve"`, `keeper.go:32`;
  the same constant `x/fee-collector` owns at `x/fee-collector/types/keys.go:16`).
- `x/fee-collector/types/protocol_income.go:44-58` fans one income event out to
  **8 module accounts** (`:48-55`).

If these modules landed in different zones, every one of those calls would be an
illegal foreign write.

**Recommendation: pin all economics, staking, and fee modules to the Core Zone
permanently** — `x/bank`, `x/auth`, `x/staking`, `x/distribution`, `x/slashing`,
`x/gov`, `x/emissions`, `x/mint-authority`, `x/fees`, `x/fee-collector`,
`x/nominator-pool`, `x/single-nominator-pool`, `x/validator-*`, `x/aetra-*`,
`x/config*`, `x/constitution`, `x/upgrade`, `x/identity-root`. Not as a Phase-1
convenience — as a permanent invariant. Money never leaves zone 0.

### 4.8 Two value systems, two conservation proofs

Native value is real `x/bank` coins (`x/native-account/types/account.go:28-44`
has no balance field). Contract value is a `uint64` field inside the contracts
blob (`x/contracts/types/contract_state.go:72`).

A cross-zone transfer between a native account in zone 1 and a contract in zone
3 crosses **both** a zone boundary and a value-system boundary. There is no
single conservation invariant that covers it. AEZ must specify which system is
authoritative at the boundary (recommendation: `x/bank` in the Core Zone is the
only authority; contract `uint64` balances are a derived ledger that must
reconcile against a Core-held escrow).

### 4.9 Zones do not add throughput

All validators execute all zones, sequentially, inside one `FinalizeBlock`. The
block gas ceiling stays at `MaxBlockGas = 20,000,000` (`x/fees/types/fee_model.go:23`).
Parallel execution is not available: `app/app.go:74-75` states the app does not
call `SetBlockSTMTxRunner`, and doing so is mutually exclusive with the block gas
meter enabled at `:76`.

AEZ buys **isolation, fairness, blast-radius containment, and future migration
optionality**. It does **not** buy capacity. Any roadmap language implying
"elastic = more TPS" is false under this model and should be corrected before it
reaches a public document.

### 4.10 DNS does not exist yet

There is **no `x/dns`**. The native registry is `x/identity-root`
(`types/state.go:55-160`) — and it is a prototype module
(`app/wiring/aetracore/modules.go:60`), blob-backed (single `genesisKey = []byte{0x01}`
at `x/identity-root/keeper/keeper.go:16`, written whole at `:79`, read whole at
`:93`), disabled by default (`prototype.DefaultParams()` → `Enabled: false`), and
carries no Msg servers. `examples/avm/dns/dns_registry.atlx` and `dns_record.atlx`
are **examples**, not deployments. Hybrid DNS is a from-scratch build on both
halves.

## 5. Proposed AEZ module structure: `x/aez`

A new module. **Per-entity KV from day one. No blob. No `k.genesis` field.** The
keeper holds a `storeService` and nothing else that carries state across calls.

This is not a style preference. `x/contracts/keeper/keeper.go:2184-2196`
documents, in the tree, exactly why a keeper with in-memory state that is loaded
lazily is **consensus-unsafe**: a restarted or state-synced node holds
`DefaultGenesis()` while a continuously-running node holds the real value, the
two disagree, and the chain forks. A keeper with no in-memory state cannot have
that bug. `x/aez` is the module that must never reintroduce it.

```
x/aez/
  types/
    zone.go            ZoneID (0..4), ZoneKind{CORE,ELASTIC}, Zone descriptor, CoreZoneID=0
    namespace.go       Namespace closed set + CorePinned(ns) predicate
    bucket.go          BucketID, BucketCount=256, BucketFor(ns, entityID) — pure
    routing_table.go   RoutingTable{Version,ActivationHeight,Buckets [256]ZoneID}, TableHash, Validate
    epoch.go           RoutingEpoch, boundary rules, pending-table activation
    message.go         ZoneMessage envelope, ComputeMessageID, SourceSequence
    receipt.go         DeliveryReceipt, BounceReceipt, RefundReceipt, BounceDepth
    processed.go       ProcessedMarker
    quota.go           per-zone gas quotas, Core reservation, bounded queue depths
    params.go          Params{Enabled, Authority, Quotas, CoreReservation, MaxBounceDepth}
    keys.go            StoreKey + per-entity key layout (NO genesis blob key)
    genesis.go         GenesisState + Validate; export via iterators, byte-ordered
    errors.go
    events.go
  keeper/
    keeper.go          Keeper{storeService, …} — NO k.genesis, NO loadForBlock, NO writeGenesis
    routing_table.go   Get/SetPending/Activate; epoch-boundary guard
    bucket.go          ZoneFor(ns, entityID) = table.Buckets[BucketFor(ns, entityID)]
    outbox.go          Enqueue; per-(zone,entity) monotonic sequence
    inbox.go           Deliver; H+1 guard; processed markers
    drain.go           EndBlocker: per-zone gas budget, panic recovery, bounce/refund
    quota.go           per-zone budget accounting (per-block, transient)
    msg_server.go      real Msg service (unlike the prototype stack)
    grpc_server.go     Query service
    genesis.go         InitGenesis/ExportGenesis over per-entity iterators
  module.go            AppModule; RegisterServices registers Msg AND Query
  autocli.go
```

### Store layout (every key holds one entity)

| Key | Value |
| --- | --- |
| `aez/params` | `Params` (a single small struct — legitimately one key) |
| `aez/routing_table/current` | current version (uint64 BE) |
| `aez/routing_table/pending` | pending version + activation height |
| `aez/routing_table/v/<version_be8>` | `RoutingTable` |
| `aez/zone/<zone_id>` | `Zone` descriptor |
| `aez/outbox/<src_zone>/<seq_be8>` | `ZoneMessage` |
| `aez/outbox_seq/<src_zone>/<entity_id>` | uint64 BE, monotonic |
| `aez/inbox/<dst_zone>/<deliver_height_be8>/<message_id>` | `ZoneMessage` |
| `aez/processed/<dst_zone>/<message_id>` | `ProcessedMarker` |

Iteration is byte-ordered, so every traversal is deterministic without a sort.
Height-prefixed inbox keys make "everything deliverable at height H" a bounded
range scan rather than a full-queue walk.

### Bucket calculation

```
bucket_id = BE_uint64(SHA256("aetra-aez-bucket-v1" ‖ 0x00 ‖ namespace ‖ 0x00 ‖ canonical_entity_id)[0:8]) % 256
```

Domain-separated and folded exactly as `x/routing/types/routing.go:238-250` does
it — re-domained (`aetra-aez-bucket-v1`, not `aetra-routing-v1`) so no vector can
ever collide across the two schemes while both exist.

Two deliberate deviations from `AssignShard`:

1. **`% 256`, never `% activeShards`.** `AssignShard` folds modulo a *live*
   count (`:250`), so changing the shard count remaps every entity. AEZ's bucket
   count is a constant; only the `bucket → zone` table moves.
2. **The routing epoch is NOT in the hash.** `AssignShard` mixes `routingEpoch`
   into the digest (`:245-247`), which would remap **every entity on every
   epoch** — a total state migration per epoch. Buckets are permanent; the
   *table* is versioned.

`canonical_entity_id` is **bytes, never a display string**. For native accounts
it is `NormalizeToAccountIdentity` (`app/addressing/derivation.go:50-52`) — the
same identity activation records under, and idempotent, so callers holding
either the plain address or the derived identity get the same bucket.
`AE…` and `ae1…` (`app/addressing/codec.go:16-35`) are two encodings of the
same bytes and **must never** reach the hash.

`CorePinned(namespace)` short-circuits the whole calculation: Core-Zone
namespaces resolve to zone 0 unconditionally, bypassing the table. That is how
"the Core Zone never migrates" is enforced structurally rather than by
convention — no table version can express a Core-Zone move.

## 6. Phased plan

### Phase 1 — types, routing table, bucket calc (purely additive)

All 256 buckets → zone 0. Nothing reads the table on any hot path.

- **New:** `x/aez/types/{zone,namespace,bucket,routing_table,epoch,params,keys,genesis,errors,events}.go`
- **New:** `x/aez/keeper/{keeper,routing_table,bucket,msg_server,grpc_server,genesis}.go`
- **New:** `x/aez/module.go`, `x/aez/autocli.go`
- **Edit:** `app/keepers.go` — construct the keeper
- **Edit:** `app/app.go` — mount the store key (`newKVStoreKeys`, `:83`/`:108`)
- **Edit:** `app/wiring/aetracore/modules.go:47-64` **and** `:98-117` — add name and
  store key **together** (mismatched counts panic at `app/aetra_core_wiring.go:14-16`)
- **Edit:** `app/wiring/aetracore/order.go:69-111`, `:113-154`, `InitGenesisOrder`
- **Deprecation note:** `x/aetracore/README` — mark the dead kernel superseded

**Honest scope note.** "Bit-identical" means **execution semantics are
unchanged**: no tx path, no BeginBlock/EndBlock path, and no existing keeper
reads anything from `x/aez`. It does **not** mean the AppHash is literally
unchanged — adding a module adds a store and genesis bytes, which moves the root
at the genesis/upgrade boundary. On an existing chain this requires a store
upgrade and an upgrade handler. Say so in the upgrade plan; do not claim a
no-op.

### Phase 2 — zone tagging (read-only)

Resolve and expose the zone for an entity. Decide nothing.

- **New:** `x/aez/keeper/bucket.go` `ZoneFor`; Query endpoints in `grpc_server.go`
- **Edit:** `x/aez/types/events.go` — advisory zone tag events
- Optional ante-level advisory tag only, consistent with `ANTE_ADMISSION_ONLY`
  (`app/wiring/aetracore/modules.go:39-45`). **No** routing decision, **no**
  rejection, **no** ordering change.

### Phase 3 — per-zone prefixes for `x/native-account`

The only module already shaped for this (`keeper.go:167-220`, `:411-458`).

- **Edit:** `x/native-account/types/keys.go` — zone-prefixed `AccountByUserKey`
  and the raw/number/reputation indexes
- **Edit:** `x/native-account/keeper/keeper.go` — `SetAccount` (`:167`),
  `deleteAccountIndexes` (`:439`), `setIndex` (`:431`), the unique-index helpers
  (`:411`, `:423`)
- **New:** `x/native-account/keeper/migrations.go` — rewrite every account key
- **Edit:** `x/native-account/module.go` — register the migration

**Cost:** a state migration touching every account. With all buckets on zone 0
the new prefix is constant, so the migration is mechanical — but it is not free
and it is not reversible without a second migration.

### Phase 4 — message bus

- **New:** `x/aez/keeper/{outbox,inbox,drain}.go`; `x/aez/types/{message,receipt,processed}.go`
- **Edit:** `app/wiring/aetracore/order.go:113-154` — fix the drain's position
- **Delete:** `x/aetracore` (its type shapes now live in `x/aez`)

Mechanics, each closing a specific gap from §4.6:

- `message_id = H(domain ‖ src_zone ‖ src_entity ‖ src_seq ‖ dst_zone ‖ dst_entity ‖ payload_hash)`.
  `src_seq` is stored and monotonic per `(src_zone, src_entity)` and **not**
  caller-settable — unlike `LogicalTime` (`x/contracts/keeper/keeper.go:2352-2353`).
- `deliver_height ≥ enqueue_height + 1`, enforced at **both** enqueue and drain.
  Never trust the envelope: the drain re-checks `deliver_height > ctx.BlockHeight()-1`
  — the check `x/contracts/keeper/keeper.go:2216-2244` does not perform.
- Processed marker written **before** effects, keyed `(dst_zone, message_id)`.
  Replay is a marker hit, not a queue miss.
- Bounce/refund on failure with a `MaxBounceDepth` cap; a bounce of a bounce is
  rejected, never re-enqueued.
- Drain reuses the proven skeleton (`x/contracts/keeper/keeper.go:2197-2249`):
  snapshot the range up front, per-zone gas budget, panic recovery per delivery
  (`:2288-2293`) so one bad contract cannot halt the chain, bounded depth
  (compare `x/contracts/types/api.go:26` = 65536).
- **Re-resolution rule:** `dst_zone` is pinned at enqueue, and re-resolved at
  delivery. If the entity has moved zones across a routing epoch in the
  meantime, the message follows it to the new zone rather than failing. This
  rule must be tested explicitly (§8) — it is the only thing standing between a
  mid-flight rezone and a stranded message.

### Phase 5 — `x/contracts` per-zone split (the expensive one)

This is the keeper rewrite. There is no incremental version of it.

- **Edit:** `x/contracts/keeper/keeper.go` — **delete** `k.genesis`, `loadForBlock`
  (`:2769-2791`), `writeGenesis` (`:2793-2806`), `assignGenesis`. Every handler
  that reads or writes module state changes. That is essentially the whole file.
- **New:** `x/contracts/types/keys.go` — per-entity, zone-prefixed keys for
  contracts, code, internal messages, receipts
- **Edit:** `x/contracts/keeper/genesis.go` — iterator-based init/export
- **New:** `x/contracts/keeper/migrations.go` — blob → per-entity. Read the one
  key, fan it out, delete it. One-way.
- **Edit:** `x/contracts/keeper/{msg_server,grpc_server}.go`

Only after this do contracts have per-zone state. Before this, "an Aetralis
contract in zone 3" is not a thing that can exist.

### Phase 6 — per-zone gas quotas

- **New:** `x/aez/keeper/quota.go`; `x/aez/types/quota.go`
- **Edit:** `x/fees/types/fee_model.go:157-172` `ValidateAdmission` — add a
  per-zone cumulative check beside the existing `MaxBlockGas` check (`:171-173`)
- **Edit:** `x/fees/keeper/congestion.go:35-61` — per-zone utilization, fed to
  `x/load` (`:53-59`) per zone

Starting split of `MaxBlockGas = 20,000,000` (`x/fees/types/fee_model.go:23`):
Core reserved 8,000,000; each of 4 elastic zones 3,000,000. Sum = 20,000,000.
With `MaxTxGas = 1,000,000` (`:22`) an elastic zone still fits at least 3
maximum-gas transactions per block. **The Core reservation is the point** — a
contract storm in zone 2 must never be able to starve a slashing or governance
transaction. Quota params are committed state; per-block consumption is
transient and recomputed each block, so it never enters the AppHash by itself.

### Phase 7 — hybrid DNS

- **Edit:** `x/identity-root/keeper/keeper.go` — de-blob (`genesisKey` at `:16`,
  `:79`, `:93`) to per-entity keys, one per `NameRecord` /
  `ResolverRecord` / `ReverseRecord` (`types/state.go:55-82`)
- **New:** `x/identity-root/keeper/msg_server.go` — real Msg service for the
  `Msg*` types already defined (`types/state.go:103-160`)
- **Edit:** `app/wiring/aetracore/modules.go` — graduate `identity-root` from
  `prototypeModules` (`:60`) into `systemModules`, exactly as `x/contracts` did
  (`:81-87`)
- **New:** `x/identity-root/keeper/migrations.go`
- **Pin:** `CorePinned("name") == true` in `x/aez/types/namespace.go`
- **Resolvers:** `examples/avm/dns/dns_registry.atlx`, `dns_record.atlx` become
  the deployable resolver contracts in elastic zones, reading Core-Zone names
  through the message bus (Phase 4) — never through a direct foreign read.

## 7. Invariants

| # | Invariant | Enforced at |
| --- | --- | --- |
| I-1 | One consensus, one validator set, one height, one AppHash across all zones | baseapp (`app/app.go:78`); no `PrepareProposal`/`ProcessProposal`/mempool exists to break it |
| I-2 | The Core Zone owns the validator set unconditionally | `app/elector_validator_updates.go:18` (rewrites `ValidatorUpdates` after module EndBlock) |
| I-3 | `bucket_id` is a pure, stable function of `(namespace, canonical_entity_id)` | `x/aez/types/bucket.go`; golden vectors (§8) |
| I-4 | Bucket count is exactly 256 and never varies with zone count | `x/aez/types/bucket.go` (`BucketCount` const; `% 256`, never `% activeShards` — cf. `x/routing/types/routing.go:250`) |
| I-5 | The routing epoch never enters the bucket hash | `x/aez/types/bucket.go` (cf. `x/routing/types/routing.go:245-247`) |
| I-6 | `canonical_entity_id` is identity **bytes**, never a display string | `x/aez/types/bucket.go` + `app/addressing/derivation.go:50-52`; encodings at `app/addressing/codec.go:16-35` |
| I-7 | Every one of the 256 buckets maps to exactly one zone | `x/aez/types/routing_table.go` `Validate` |
| I-8 | `bucket → zone` changes only at a routing-epoch boundary | `x/aez/keeper/routing_table.go` (pending table + activation height) |
| I-9 | The Core Zone never migrates | `x/aez/types/namespace.go` `CorePinned` — bypasses the table entirely; no table version can express a Core move |
| I-10 | Money never leaves the Core Zone | Permanent module pinning (§4.7); `x/aez` holds no module account (`app/aetra_core_wiring.go:26-28`, `:43-45`) |
| I-11 | No direct foreign-zone writes | Zone-prefixed keys (Phases 3, 5); `x/aez` keeper holds no other module's store handle; review gate |
| I-12 | Delivery no earlier than `H+1` | `x/aez/keeper/inbox.go` + re-check in `drain.go` (the check absent at `x/contracts/keeper/keeper.go:2216-2244`) |
| I-13 | Exactly-once delivery | `message_id` + non-caller-settable `src_seq` + processed marker (`x/aez/keeper/inbox.go`), replacing dequeue-by-id (`x/contracts/keeper/keeper.go:2121-2142`) |
| I-14 | Failure bounces or refunds; bounces never loop | `x/aez/keeper/drain.go`, `MaxBounceDepth` in `x/aez/types/quota.go` |
| I-15 | One delivery panic cannot halt the block | `x/aez/keeper/drain.go` per-delivery recover (pattern: `x/contracts/keeper/keeper.go:2288-2293`) |
| I-16 | Native value conservation | `app/invariants.go` (bank supply / emission cap / burn reconciliation), re-run at `app/block_lifecycle.go:39` |
| I-17 | Contract value conservation reconciles to a Core escrow | `x/contracts` value-conservation checks (Phase 5); `x/contracts/types/contract_state.go:72` is a derived ledger, not an authority |
| I-18 | Per-zone gas quota holds; Core reservation is never consumed by an elastic zone | `x/fees/types/fee_model.go:157-172` + `x/aez/keeper/quota.go` |
| I-19 | Sum of all zone gas ≤ `MaxBlockGas` | `x/fees/types/fee_model.go:171-173` (unchanged; the global check stays authoritative) |
| I-20 | No keeper state survives across blocks in RAM | `x/aez/keeper/keeper.go` has no `k.genesis` field — structurally immune to `x/contracts/keeper/keeper.go:2184-2196` |
| I-21 | All state is bounded | `x/aez/types/quota.go` queue depths (cf. `x/contracts/types/api.go:26` = 65536) |
| I-22 | No wall clock, randomness, floats, or map-iteration order in zone code | `scripts/security/determinism-gate.ps1` (see `docs/security/determinism-gate.md`) |
| I-23 | A disabled `x/aez` never fails a block | `prototype.DefaultParams()` → `Enabled: false`; every feed a silent no-op (the `x/load` rule, `docs/architecture/load-and-zones.md:65-66`) |

## 8. Test plan

### 8.1 Unit

| Target | Test |
| --- | --- |
| Bucket calc | `x/aez/types/bucket_test.go` — **golden vectors**: a frozen, committed table of `(namespace, entity_id_hex) → bucket_id` covering all 256 buckets, both address encodings resolving to the same bucket (`AE…` and `ae1…`, `app/addressing/codec.go:16-35`), and `NormalizeToAccountIdentity` idempotency (`app/addressing/derivation.go:50-52`). Golden values are frozen at Phase 1 and **any** later change to them is a consensus break, not a test update. |
| Bucket domain separation | Assert `x/aez` and `x/routing/types/routing.go:234-251` never agree on a vector — the domains are distinct by construction |
| Routing table | `x/aez/types/routing_table_test.go` — all 256 mapped; duplicate/missing/out-of-range zone rejected; `TableHash` stable across field reordering |
| Epoch boundary | `x/aez/keeper/routing_table_test.go` — mid-epoch `Set` rejected; pending table activates at exactly its height, not one block early or late |
| Core pinning | `CorePinned` namespaces resolve to 0 under **every** table version, including a hand-crafted malicious table that maps their buckets elsewhere |
| Message id / sequence | `x/aez/types/message_test.go` — identical content + identical height ⇒ **different** ids via `src_seq`; the collision that `ComputeInternalMessageID` (`x/contracts/types/contract_state.go:822-849`) permits must be impossible here |
| Quota | `x/aez/types/quota_test.go` — quotas sum to `MaxBlockGas`; Core reservation cannot be borrowed |

### 8.2 Determinism and AppHash equality

- **Phase 1 semantic no-op.** Run an identical block sequence on a build with
  `x/aez` and one without; assert every module's exported state is
  byte-identical and no tx result differs. The AppHash **will** differ (new
  store) — assert the difference is confined to the new store and nothing else.
- Extend `app/determinism_test.go` (deterministic default genesis, deterministic
  export, identical export after the same empty-block sequence) to cover `x/aez`.
- Assert `x/aez` export is byte-stable across two runs with different map
  insertion orders — per-entity iteration must be byte-ordered, never sorted
  after the fact.
- `scripts/security/determinism-gate.ps1` must return no untriaged High/Critical
  for `x/aez`.

### 8.3 The restart-divergence test

This is the most important test in the plan and it exists because
`x/contracts/keeper/keeper.go:2184-2196` describes the exact bug in prose.

Reproduce the F-17 scenario against `x/aez`:

1. Node **A** runs continuously from genesis; governance raises a param at
   height `h`.
2. Node **B** is constructed fresh at height `h` (a restart / state-sync), so any
   in-memory keeper field holds `DefaultGenesis()` rather than the committed
   value.
3. Both execute block `h+1`, which exercises the routing table, the bucket calc,
   and the drain.
4. **Assert `AppHash(A) == AppHash(B)`** and that both took the same branch on
   the enabled/disabled and budget checks.

Because `x/aez/keeper.Keeper` has no `k.genesis` field, this should be trivially
green from day one. That is the point: it is the **regression guard** that keeps
it green. Add the same test at Phase 5 for the rewritten `x/contracts` keeper —
where it is not trivial, and where the original bug lived.

### 8.4 Adversarial

| Attack | Test |
| --- | --- |
| Replay | Deliver the same `message_id` twice across different blocks; second must hit the processed marker and be rejected — **not** silently "missing from queue" the way `x/contracts/keeper/keeper.go:2139-2141` reports it |
| Same-block delivery | Enqueue at `H`, force the drain at `H`; assert **not** delivered. This is the regression test for the gap at `x/contracts/keeper/keeper.go:2216-2244` |
| Forged `deliver_height` | Envelope claims `deliver_height = H-5`; drain must re-check and reject rather than trust the envelope |
| Id collision | Two byte-identical messages, same height, same caller-set logical time; assert distinct ids and two distinct deliveries |
| Bounce loop | A→B fails, bounces; the bounce fails and would bounce back; assert `MaxBounceDepth` terminates it and value lands in exactly one place |
| Refund conservation | Force every failure mode; assert total native supply unchanged (`app/invariants.go`) and no contract `uint64` balance drifts from its Core escrow |
| Rezone under load | Flip the routing table at an epoch boundary with N messages in flight to a rezoning entity; assert every message is delivered exactly once to the entity's **new** zone (the re-resolution rule, Phase 4) and none is stranded, duplicated, or delivered to the old zone |
| Rezone + restart | The above, with node B restarted mid-flight; assert AppHash equality |
| Quota starvation | Saturate one elastic zone to its quota with max-gas txs; assert Core-Zone slashing and governance txs still admit (`x/fees/types/fee_model.go:157-172`) |
| Queue flood | Enqueue past the bounded depth; assert deterministic rejection, not unbounded growth (`x/contracts/types/api.go:26` pattern) |
| Panic injection | A contract that panics on delivery; assert the block still completes (`x/contracts/keeper/keeper.go:2288-2293` pattern) and the message bounces rather than vanishing |

### 8.5 Migration

| Phase | Test |
| --- | --- |
| 1 | Upgrade handler adds the `x/aez` store; export before/after differs **only** by the new store |
| 3 | `x/native-account` key migration: export equality pre/post (same accounts, same balances, same indexes, different keys); indexes resolve identically; the migration is idempotent under replay |
| 5 | `x/contracts` blob → per-entity: export equality pre/post for every contract, code entry, queued message, and receipt; the blob key is deleted; a re-run of the migration is a no-op; a mid-migration halt is not observable (single block, all-or-nothing) |
| 7 | `x/identity-root` blob → per-entity + prototype → system graduation: every `NameRecord`/`ResolverRecord`/`ReverseRecord` survives byte-identically; the wiring gate still passes (`app/aetra_core_wiring.go:18-46`) |
| All | Golden bucket vectors unchanged across every migration — a bucket that moves is a consensus break |
