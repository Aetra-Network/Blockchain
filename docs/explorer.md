# Aetra Block Explorer Data Source

`l1-explorer` is the backend a block-explorer frontend reads from. It indexes
a node's blocks and transactions over CometBFT RPC, proxies live module state
(contracts, validators, supply) over gRPC, and serves everything as a
read-only JSON HTTP API with permissive CORS. It is **database-optional**: the
default store is a bounded in-memory index, so one binary against one node is
enough to stand up an explorer backend.

```
                +------------------+        +-------------------------+
   aetrad  ---> | CometBFT RPC     | -----> |  l1-explorer            |
   (node)       | :26657 blocks/tx |        |  - indexes blocks/txs   | --> HTTP JSON API
                | gRPC :9090 state | -----> |  - proxies live state   |     (:8080)
                +------------------+        +-------------------------+
```

## Where it lives

The explorer data source is **not part of this repo**. It lives beside the
explorer site it serves, in the ecosystem monorepo, as its own Go module:
`ecosystem/explorer/server` (module `github.com/aetra-network/explorer-server`).
It depends on this chain module for the tx codec, address formatting, and
`x/contracts` query types via a local `replace` back to the blockchain repo, so
keep both checked out side by side. This page documents the API it serves.

## Run

Point it at a node that has RPC and gRPC enabled (see
[docs/validator-onboarding.md](validator-onboarding.md)):

```bash
# build the node
./scripts/validator/build.sh            # or: go build -o build/aetrad ./cmd/l1d

# build the explorer data source (in the ecosystem repo, next to the site)
cd ecosystem/explorer/server && go build -o l1-explorer.exe .

# run against a local node
./l1-explorer.exe \
  -rpc  http://127.0.0.1:26657 \
  -grpc 127.0.0.1:9090 \
  -listen 0.0.0.0:8080 \
  -start-height 1
```

Flags:

| Flag | Default | Meaning |
| --- | --- | --- |
| `-rpc` | `http://127.0.0.1:26657` | node CometBFT RPC to index |
| `-grpc` | `127.0.0.1:9090` | node gRPC for live contract/validator/supply (empty disables those routes) |
| `-listen` | `0.0.0.0:8080` | explorer HTTP API address |
| `-start-height` | `0` | first height to index (0 = earliest the node retains) |
| `-retain-blocks` | `100000` | in-memory retention window (0 = unbounded) |
| `-poll` | `1s` | block polling interval |

## API

All responses are JSON. Lists take `?limit=` (max 200) and `?offset=`.

| Route | Returns |
| --- | --- |
| `GET /status` | chain id, live tip height/time/hash, indexed counts, node moniker |
| `GET /blocks` | recent block summaries (newest first) |
| `GET /blocks/{height\|hash}` | block detail with tx hashes |
| `GET /txs` | recent tx summaries |
| `GET /txs/{hash}` | tx detail: decoded messages, fee, gas, success/code, events, touched addresses |
| `GET /accounts/{addr}/txs` | txs involving an account/contract address |
| `GET /contracts` | deployed AVM contracts (address, code id, status, balance, creator) — live gRPC |
| `GET /contracts/{addr}` | one contract's detail (state root, code hash, admin, storage, heights) — live gRPC |
| `GET /validators` | bonded validator set — live gRPC |
| `GET /supply` | total token supply — live gRPC |
| `GET /search?q=` | resolves a height, block/tx hash, or address to its canonical route |
| `GET /healthz` | liveness + indexed height |

### Transaction gateway (wallet surface)

The same binary is the non-custodial transaction gateway: it never sees a
private key — the only write-shaped route relays **already-signed** bytes.

| Route | Returns |
| --- | --- |
| `GET /accounts/{addr}` | signing material: `account_number`, `sequence`, `exists` |
| `GET /address/{addr}` | unified view for AE / `4:` / `-7:` forms: representations, balance, kind, contract bytecode + raw data |
| `GET /fees/estimate?gas=N` | deterministic fee quote (`required_fee` = the ante-handler minimum, base/max, congestion) |
| `POST /tx/simulate` `{"tx_bytes":"<b64>"}` | dry-run gas usage |
| `POST /tx/broadcast` `{"tx_bytes":"<b64>"}` | relays a signed tx; returns `hash`, CheckTx `code`, `accepted` |

Wallet flow: `GET /accounts/{addr}` → `GET /fees/estimate` → sign client-side
→ `POST /tx/simulate` (optional) → `POST /tx/broadcast` → `GET /txs/{hash}`.
Backend developers can use the node gRPC (`:9090`) directly instead — every
module including the hand-rolled `x/contracts` is served there, plus
`cosmos.tx.v1beta1.Service` (Simulate / BroadcastTx / GetTx); the gateway is
the browser/JSON face of that surface.

### Example

```bash
curl -s localhost:8080/status
curl -s "localhost:8080/txs?limit=10"
curl -s localhost:8080/txs/<HASH>
curl -s localhost:8080/contracts
curl -s "localhost:8080/accounts/AEJk.../txs"
```

## What feeds it

- **Blocks & transactions** come from CometBFT RPC (`Block` + `BlockResults`).
  Each tx is decoded with the app's own tx config, so messages, fees, memo,
  and events render correctly — including Aetra's hand-rolled `x/contracts`
  messages. A tx that fails to decode is still indexed (hash, result code,
  raw log) so rejected/malformed txs remain visible.
- **Contracts, validators, supply** are read live over gRPC on each request
  rather than indexed, because they are current-state queries the node already
  serves cheaply. The client forces a gogoproto codec so the hand-rolled
  `x/contracts` query types decode correctly.
- **Message chains** come from the `avm_execute` / `avm_internal_send` events
  every contract execution emits into the tx log: `avm_execute` carries
  `contract`, `caller`, `funds`, `opcode`; each `avm_internal_send` carries
  `source`, `destination`, `amount`, `opcode`, `mode` and (when set) `comment`.
  Matching `avm_internal_send.source` to the executed `contract` reconstructs
  the chain `caller -> contract -> {destinations}` — this is what the
  explorer's transaction-flow diagram draws.

## Scaling notes

- The in-memory store bounds retention (`-retain-blocks`). For a long-running
  public explorer, either raise the window on a well-provisioned host or
  implement the `server/store.Store` interface against Postgres — `ingest`
  and `api` are storage-agnostic and need no changes.
- The indexer is a single reader; run one per explorer backend. It is safe to
  restart: it resumes from the node's earliest retained height (or
  `-start-height`).
- Serve public explorer traffic from non-validator infrastructure (a node
  dedicated to RPC/gRPC/indexing), never from a validator node.
