# Aetra API Reference

The complete surface for building on Aetra: reading chain state, quoting fees,
previewing consequences, and submitting transactions. Everything a wallet,
explorer, framework, or dApp needs.

There are **two layers**, and they serve the same data:

1. **Node gRPC (`:9090`) — the canonical, full-fidelity API.** Every module
   (standard cosmos-sdk + Aetra's own) exposes typed Query and Msg services
   here. This is what backends (Go / Rust / Python) and the framework talk to.
2. **Gateway HTTP+JSON (`ecosystem/explorer/server`, `:8080`) — the browser
   face.** Browsers can't speak gRPC and the node's REST gateway is *Not
   Implemented* for the hand-rolled `x/contracts` module, so the gateway
   translates the gRPC/RPC surface into plain JSON with permissive CORS. The
   explorer and the wallet consume this.

Pick the layer that fits: a browser wallet uses the gateway; a server-side
indexer or bot can use either.

---

## 1. Node gRPC (`:9090`) — canonical

All Aetra module types are **gogoproto** messages. A gRPC client must use a
gogoproto codec (grpc-go's default protov2 codec will not decode the
hand-rolled `x/contracts` types). See `ecosystem/explorer/server/chainquery`
for a reference `rawCodec` that does this.

### Standard cosmos-sdk services (unchanged, fully available)

| Service | Use |
| --- | --- |
| `cosmos.auth.v1beta1.Query/Account` | account number + sequence for signing |
| `cosmos.bank.v1beta1.Query/{Balance,AllBalances,TotalSupply}` | balances, supply |
| `cosmos.staking.v1beta1.Query/{DelegatorDelegations,DelegatorUnbondingDelegations,Validators,Params}` | staking positions + set |
| `cosmos.distribution.v1beta1.Query/DelegationTotalRewards` | accrued staking rewards |
| `cosmos.tx.v1beta1.Service/{Simulate,BroadcastTx,GetTx,GetTxsEvent}` | gas dry-run, broadcast, fetch |

Transactions are built and signed exactly as on any cosmos-sdk chain (SIGN_MODE
DIRECT), with Aetra's addresses and the deterministic fee (below).

### Aetra module services

| Service | Methods |
| --- | --- |
| `l1.contracts.v1.Query` | `Contract`, `Contracts`, `Code`, `Codes`, `ContractStorage`, `ContractReceipts`, `ContractQueue`, `ContractEvents`, `ContractStateRoot`, `SecurityAttestations`, `SecurityBadge`, `Params` |
| `l1.contracts.v1.Msg` | `StoreCode`, `DeployContract`, `ExecuteExternal`, `ExecuteInternal`, `SendInternalMessage`, `UpdateContractParams`, `SubmitSecurityAttestation`, `RevokeSecurityAttestation` |
| `l1.fees.v1.Query` | `Params`, `EstimateFee`, `Accounting`, `ModuleBalances`, `NetworkLoad` |
| plus | `l1.aetraeconomics`, `l1.validatorelection`, `l1.validatorregistry`, `l1.nominatorpool`, `l1.systemregistry`, `l1.actorregistry`, `l1.constitution`, `l1.config`, `l1.configvoting`, `l1.scheduler`, `l1.reporter`, `l1.evidence`, … (one `v1.Query`/`v1.Msg` per module under `api/l1/`) |

Contract message type URLs (for building txs): `/l1.contracts.v1.MsgStoreCode`,
`/l1.contracts.v1.MsgDeployContract`, `/l1.contracts.v1.MsgExecuteExternal`,
`/l1.contracts.v1.MsgExecuteInternal`, `/l1.contracts.v1.MsgSendInternalMessage`.

---

## 2. Gateway HTTP+JSON (`:8080`) — browser / wallet

All responses are JSON. Lists take `?limit=` (max 200) and `?offset=`. The
gateway is **non-custodial**: it never holds a key and cannot sign. Its only
write-shaped route relays already-signed bytes.

### Read (explorer surface)

| Route | Returns |
| --- | --- |
| `GET /status` | chain id, tip height/time/hash, indexed counts, moniker, sync state |
| `GET /blocks`, `GET /blocks/{height\|hash}` | block summaries / detail (tx hashes) |
| `GET /txs`, `GET /txs/{hash}` | tx summaries / detail (messages, fee, gas, code, events, memo, message-chain events) |
| `GET /accounts/{addr}/txs` | tx history for an address (paginated) |
| `GET /address/{addr}` | **unified view** for any address form — see below |
| `GET /contracts`, `GET /contracts/{addr}` | deployed AVM contracts / one contract |
| `GET /validators`, `GET /supply` | validator set / total supply |
| `GET /search?q=` | resolve a height, block/tx hash, or any address form |
| `GET /healthz` | liveness + indexed height |

### Transactions (wallet surface)

| Route | Returns |
| --- | --- |
| `GET /accounts/{addr}` | signing material: `account_number`, `sequence`, `exists`, pubkey type |
| `GET /fees/estimate?gas=N` | deterministic fee quote: `required_fee` (the exact ante-handler minimum), `base_fee`, `max_fee`, `utilization_bps`, `congested`, `at_hard_cap` |
| `POST /tx/simulate` `{"tx_bytes":"<b64>"}` | gas dry-run (`gas_used`, `gas_wanted`) |
| `POST /tx/preview` `{"tx_bytes":"<b64>"}` | **consequences** of a built tx: fee + payer, per-message effects (e.g. "send 2500000naet from A to B"), coin changes, dangerous permissions, warnings — no execution |
| `POST /tx/broadcast` `{"tx_bytes":"<b64>"}` | relays a **signed** tx; returns `hash`, CheckTx `code`, `accepted` |

### Staking (wallet surface)

| Route | Returns |
| --- | --- |
| `GET /staking/{addr}` | `delegations[]` (validator, shares, balance), `unbonding[]` (balance, completion_time), `rewards[]` |
| `GET /staking/params` | `bond_denom`, `unbonding_time`, `max_validators`, `max_entries`, `min_commission_rate` |

### `GET /address/{addr}` shape

Accepts any form (AE / `4:` / `-7:` / hex), normalizes, classifies:

```json
{
  "valid": true,
  "address": "AEJk…",
  "kind": "wallet | contract | system",
  "balance": "<naet integer string>",
  "status": "active | frozen | … | uninit | nonexistent",
  "forms": {
    "user_friendly": "AEJk…",        // base64url; MAY contain - and _
    "raw": "4:<64hex>",
    "hex": "<64hex>",
    "system_raw": "-7:<64hex>"        // present ONLY for system entities
  },
  "contract": {                        // present only when kind == contract
    "status": "active", "code_id": "…", "code_hash": "…",
    "creator": "AEJk…", "admin": "AEJk…", "storage_bytes": 1234,
    "created_height": 100, "updated_height": 200, "state_root": "…",
    "bytecode": { "size": 1050, "hex": "…", "base64": "…", "hash": "<sha256>", "code_hash": "…" },
    "data":     { "size": 184,  "hex": "…", "base64": "…", "hash": "<sha256>", "chunks": [ { "depth": 0, "bits": 64, "hash": "…", "refs": 1, "hex": "…" } ] }
  }
}
```

---

## 3. Wallet flow (end-to-end, verified)

```text
1. GET  /accounts/{addr}           -> account_number, sequence
2. GET  /fees/estimate?gas=200000  -> required_fee (the ante will accept exactly this)
3.       build + sign the tx CLIENT-SIDE (keys never leave the wallet; SIGN_MODE DIRECT)
4. POST /tx/preview                -> show the user what it does before they confirm
5. POST /tx/simulate               -> optional: exact gas before signing for real
6. POST /tx/broadcast              -> { "hash": "...", "accepted": true }
7. GET  /txs/{hash}                -> delivery result, events, message chain, memo
```

The framework/wallet builds messages with the standard cosmos-sdk tx builder
using Aetra's message type URLs; nothing about signing is Aetra-specific.

---

## 4. Fees — deterministic, never-rejected

Aetra uses a **deterministic full-formula fee**: for a given gas limit and the
current congestion, exactly one fee is admissible. `GET /fees/estimate?gas=N`
(or `l1.fees.v1.Query/EstimateFee`) returns that `required_fee` — pay it and the
ante handler will not reject the tx. Key facts:

- Base denom is `naet`; `1 AET = 10^9 naet`.
- `max_tx_gas` is `1_000_000` — AVM deploy/execute txs need an explicit
  `--gas 1000000` (the default 200000 is too low for the whole-module persist).
- The quote includes `utilization_bps` / `congested` / `at_hard_cap` so a wallet
  can warn the user when the network is busy.

---

## 5. Contract lifecycle (via transactions)

Build these as ordinary txs (any of them can be previewed/simulated/broadcast
through the gateway or sent over `cosmos.tx.v1beta1.Service`):

1. **StoreCode** (`/l1.contracts.v1.MsgStoreCode`) — register compiled AVM
   bytecode. Code id = `sha256("aetra-avm-code-v1/" + module.bin)`.
2. **DeployContract** (`/l1.contracts.v1.MsgDeployContract`) — instantiate with
   init storage + initial balance + salt.
3. **ExecuteExternal** (`/l1.contracts.v1.MsgExecuteExternal`) — call an
   `@external` entrypoint with an ABI-encoded body + opcode.
4. Lifecycle: `UpdateContractParams`, freeze/unfreeze (via `x/storage-rent`'s
   `MsgUnfreezeContract` / `MsgFreezeExpiredContract` / `MsgDeleteExpiredContract`),
   and the in-contract withdraw-all / self-destruct send modes (see
   `docs/architecture/message-model.md`).

Every execution emits `avm_execute` + `avm_internal_send` events into the tx
log, so the message chain `caller → contract → {destinations}` is
reconstructable from `GET /txs/{hash}` (the explorer draws it).

---

## 6. Addresses

Three interchangeable forms, all resolvable by `/address`, `/search`, and the
address-parsing helpers in `app/addressing`:

- **User-friendly** `AEJk…` — `base64.RawURLEncoding`, so it legitimately
  contains `-` and `_`. The primary identity shown to users.
- **Raw** `4:<64hex>` — the workchain-tagged raw form (secondary/technical).
- **System raw** `-7:<64hex>` — only for **system entities**; not emitted for
  ordinary wallets or contracts.

---

## 7. Security model

- The gateway is **non-custodial**: no keys, no signing. `POST /tx/broadcast`
  accepts only fully-signed bytes and relays them to CheckTx — the same thing a
  public RPC node does. Bodies are capped at 1 MiB, base64-validated, POST-only.
- Run the gateway on non-validator infrastructure (a node dedicated to
  RPC/gRPC/indexing), never on a validator.
- Signing, key storage, and the "what am I signing" decision stay client-side.
  `POST /tx/preview` exists precisely so a wallet can render the consequences
  (fee, transfers, dangerous permissions, warnings) before the user confirms.

---

## 8. Networks

| Network | chain-id |
| --- | --- |
| Mainnet | `18` |
| Public testnet | `-19` (negative marks a test network) |
| Dev / local | `aetra-local-1`, `aetra-testnet-…`, `aetra-preflight-…` |
