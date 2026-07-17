# Code-bloat audit — reachability proof (nothing deleted)

272,213 lines of production Go (excl. tests + generated .pb.go). This audit proved
the reachability of every prototype/suspected-dead module by a strict definition,
adversarially verified. Kept for the owner to decide what to remove; NOTHING was
deleted.

**Reachable if:** (a) a user Msg with a registered handler + a CustomGetSigners
entry, (b) real Begin/EndBlock logic that moves the app hash (not a no-op order
slot), (c) a *live* module calls its keeper, or (d) the ante/proposal path invokes
it. Wiring registration, a default-seeding InitGenesis, a query-only server, and
being imported by app_types.go do NOT count.

## Two verdicts the first pass got WRONG (adversarial refutation caught them)

- **x/aetracore is NOT wholesale-dead.** The keeper (555 LOC) is dead, but
  `x/aetracore/types` (30,147 LOC) is a live shared proof/hash library: imported by
  live x/contracts (`keeper.go:314` `ValidateHash` on the StoreCode path) and by
  x/proofregistry → live x/native-account (`full_genesis.go:52`). Deleting the
  directory would break the build. Only keeper+module (~555 LOC) is removable.
- **x/load is live.** x/fees calls `LoadKeeper.ApplyBlockMetrics` every block via
  `WithLoadSink` (app/keepers.go:280 → x/fees congestion.go EndBlocker). Deleting it
  breaks the compile of the live x/fees. Keep.

## Buckets

### GREEN — genuinely unneeded, safe to delete (~16K LOC)
| Module | LOC | Proof |
|---|---|---|
| x/performance | 7,370 | live scorer is x/validator-registry; this is a dup with 0 callers |
| x/aetra-economics | 1,471 | Msg non-broadcastable (no signer); only invariants.go reads it; superseded by x/emissions+x/fees |
| x/aetra-validator-score | 1,237 | dup scoring, non-broadcastable Msg, 0 callers |
| x/mesh | 1,539 | superseded by the x/aez message bus |
| x/avm-scheduler | 1,425 | superseded by x/contracts internal-message queue |
| x/sharding-coordinator | 1,354 | superseded by x/aez |
| x/routing | 833 | superseded by x/aez routing table; ante does not import it |
| x/aetracore keeper+module | 555 | 0 non-test callers (KEEP x/aetracore/types) |

### YELLOW — dead but a roadmap stub, owner decides (~70K LOC)
x/payments (38,290 — DeFi, planned via AVM contracts not this module),
x/networking (24,383 — node layer), x/bridge-hub (998), x/cross-chain-registry
(1,117) (bridges — planned via AVM crypto primitives), x/delegator-protection
(1,191), x/validator-insurance (1,175), x/dynamic-commission (908),
x/stake-concentration (786), x/aetra-staking-policy (1,309). All prototype
(blob genesis, Enabled=false), non-broadcastable, no live caller.

### RED — reachable, KEEP
x/aetracore/types (30,147), x/load (650).

## Deletion blast radius (when the owner decides)
Each GREEN/YELLOW module, on deletion, must be removed from:
app/wiring/aetracore/modules.go (prototypeModules + PrototypeStoreKeys),
app/wiring/aetracore/order.go (all order lists — kept positionally paired),
app/wiring/storekeys, app/app_types.go, app/modules.go, app/modulewiring,
app/keepers.go, app/keeperconfig/tx.go (if a Msg entry), app/accounts/
module_accounts.go (if it owns a module account — delegator-protection /
validator-insurance do), app/launch_module_inventory.json, genesis defaults, and
the wiring gate (app/aetra_core_wiring.go). x/mesh and x/routing are also imported
by cmd/l1d/cmd/execution_os.go (CLI simulator). x/aetra-economics and
x/validator-insurance are read by app/invariants.go.
