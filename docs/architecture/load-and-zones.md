# Load Protection And Zone Scaling

How Aetra stays up under load, and how it scales horizontally when a single
lane is no longer enough. Three layers, from always-on to governance-gated.

## Layer 1: Economic backpressure (always on)

The chain's first and primary protection is deterministic fee backpressure:

- every block, the `x/fees` EndBlocker records the finalized block gas
  utilization as committed congestion state
  (`x/fees/keeper/congestion.go`);
- the fee formula adds a congestion surcharge on top of the 0.5 AET target
  transfer fee, scaling with that stored utilization
  (`x/fees/types/fee_formula_params.go`, capped by
  `MaxCongestionSurchargeNaet`);
- fee admission happens in the ante handler against committed state only, so
  every validator prices identically and underpriced spam never enters a
  block.

Combined with hard per-block gas limits, bounded internal-message queues
(`MaxInternalMessageQueueDepth`), the per-execution AVM gas cap, and
storage-rent freezing of underfunded contracts, overload degrades into
"transactions cost more and queue longer" — never into missed blocks or state
divergence.

## Layer 2: Live load scoring (feeds the router)

`x/load` maintains a deterministic load score: per-metric basis-point scores,
an EMA over the configured window, and a LOW/MEDIUM/HIGH band
(`x/load/types/load.go`). The `x/fees` EndBlocker feeds it the finalized
block metrics every block (`Keeper.WithLoadSink`, wired in `app/keepers.go`),
so the score is live consensus state on every node, identical everywhere,
with history bounded at `MaxHistoryEntries`.

While `x/load` is disabled (the default), the feed is a silent no-op and
consumes nothing.

## Layer 3: Zones (governance-gated horizontal scale)

`x/zones`, `x/routing`, `x/mesh`, and `x/sharding-coordinator` carry the
zone architecture: aetra core as the upper settlement layer, zones as
execution lanes, routing tables mapping traffic classes to zones, and the
load band from Layer 2 as the routing input. They ship disabled-by-default
with routing fixed to admission-level classification
(`ANTE_ADMISSION_ONLY`, see
[docs/security/phase9-aether-core-wiring-gate.md](../security/phase9-aether-core-wiring-gate.md)).

Activation order when capacity requires it:

1. governance enables `x/load` (Layer 2 starts scoring live);
2. governance enables `x/zones`/`x/routing` with a published routing table;
3. a coordinated software upgrade moves routing beyond admission-only into
   proposal/execution paths.

Until then, Layer 1 alone carries the load story — and it is sized so that
the consumer-hardware validator profile (16 GB RAM, 4-8 cores, 5-8s blocks)
never needs Layer 3 at testnet scale.

## Invariants

- No load signal may originate from wall clocks, mempool gossip, or other
  nondeterministic inputs; only finalized, committed block state feeds
  scoring and pricing.
- A disabled prototype module must never fail a block: every feed into one
  is a silent no-op while it is disabled.
- All load-driven state is bounded (`MaxHistoryEntries`, congestion state is
  a single value) — protection mechanisms must not themselves grow state
  under attack.
