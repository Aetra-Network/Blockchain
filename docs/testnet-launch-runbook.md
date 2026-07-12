# Aetra Public Testnet Launch Runbook

The end-to-end sequence to stand up the Aetra public testnet and the services
around it (explorer, faucet, monitoring). Each step links the detailed doc.
This is the "do these in order" playbook; the linked docs are the reference.

Chain IDs are numeric: **testnet `-19`**, mainnet `18`
([app/params/chain_id.go](../app/params/chain_id.go)). Average transfer fee is
**~0.5 AET**, dynamic and governance-adjustable.

## 0. Prerequisites

- Release binary built and version-verified on every host
  ([VALIDATOR.md](VALIDATOR.md#build-or-download-binary)); Linux hosts use
  [scripts/validator/build.sh](../scripts/validator/build.sh).
- Hosts meet the validator hardware baseline (16 GB RAM, 4–8 cores,
  500 GB–1 TB NVMe, 100 Mbps) — [validator-onboarding.md](validator-onboarding.md#hardware-target).
- Security/determinism gates green
  ([public-testnet-production-gates.md](public-testnet-production-gates.md)).

## 1. Genesis ceremony

1. Decide the genesis validator set (start-phase target 100–128; a bootstrap
   launch may begin with a small trusted set and grow via governance).
2. Generate per-validator node homes + gentxs. For a coordinated multi-host
   set, each operator runs `init` + `gentx` and submits their gentx; the
   coordinator collects them. For a single-operator bootstrap, use
   `aetrad testnet init-files --validator-count N --chain-id -19 ...`
   (this also pre-populates the `x/native-account` bootstrap records so the
   founding keys can call AVM entrypoints without a self-activation tx — see
   [validator-onboarding.md](validator-onboarding.md)).
3. Validate the assembled genesis:
   `aetrad genesis validate-genesis <genesis.json> --home <home>`.
4. **Publish** `genesis.json` and its SHA-256 checksum. Every joiner verifies
   the checksum before first start.

## 2. Seed / peer infrastructure

1. Stand up ≥2 seed nodes and publish their `nodeID@host:26656` addresses.
2. Publish a persistent-peers list (validator sentries + seeds).
3. Operators configure peers via `scripts/validator/init.sh -p <peers> -s <seeds>`
   or `join.sh` ([VALIDATOR.md](VALIDATOR.md#linux-quick-start)).

## 3. Start validators

- systemd + Cosmovisor is the recommended production path
  ([scripts/validator/aetrad.service](../scripts/validator/aetrad.service),
  [COSMOVISOR.md](COSMOVISOR.md)); non-systemd hosts use
  `scripts/validator/start.sh --daemon`.
- Each operator funds and creates their validator
  ([validator-onboarding.md](validator-onboarding.md#create-validator)).
- Confirm block production and set membership:
  `scripts/validator/health.sh` / `aetrad query staking validators`.

## 4. State-sync & snapshots

- Publish trust height, trust hash, ≥2 trusted RPC servers, and a snapshot
  archive + checksum so new validators join fast
  ([validator-onboarding.md](validator-onboarding.md#sync)).
- Re-prove snapshot restore before launch (export → fresh import → identical
  app hash) — the export/import smoke path in
  [public-testnet-preparation.md](public-testnet-preparation.md).

## 5. Public read/RPC + explorer

Run these on **non-validator** infrastructure only:

1. A public node with RPC (`:26657`), gRPC (`:9090`), and REST (`:1317`)
   enabled (`app.toml` `[api] enable = true`).
2. The block explorer data source against that node:
   `scripts/validator/explorer.sh -r http://<node>:26657 -g <node>:9090`
   — serves `/blocks`, `/txs`, `/contracts`, `/validators`, `/supply`,
   `/search`, etc. ([explorer.md](explorer.md)). Point the explorer frontend
   at this API.
3. Alert when the explorer `/status` `indexed_height` lags `latest_height`.

## 6. Faucet

- Run the faucet as a service against a funded testnet account, rate-limited
  per address/IP, on non-validator infra. Fund policy and the funding command
  are in [local-funding.md](local-funding.md) and
  [operator-commands.md](operator-commands.md); the "Faucet Plan" in
  [public-testnet-preparation.md](public-testnet-preparation.md#faucet-plan).
- Keep the faucet key low-balance and refill from a cold source.

## 7. Monitoring

- Enable Prometheus metrics on validators and public nodes
  (`--observability-metrics true --observability-metrics-addr 0.0.0.0:27780`).
- Scrape with Prometheus; dashboard + alert on: block height stall, missed
  blocks / jailing, peer count, sync lag, disk usage, process restarts, and
  explorer indexer lag ([validator-onboarding.md](validator-onboarding.md#observability-metrics)).

## 8. Upgrade & incident readiness

- Rehearse one coordinated Cosmovisor upgrade before launch
  ([upgrade-playbook.md](upgrade-playbook.md); stage binaries with
  `scripts/validator/cosmovisor-upgrade.sh`).
- Publish the incident-response runbook and on-call
  ([testnet-incident-response.md](testnet-incident-response.md)).

## Launch gate

Do not open the public testnet until: genesis + checksum published; ≥2 seeds
and a peer list published; validators producing blocks; state-sync/snapshot
restore re-proven; a public RPC/gRPC node + explorer live; faucet live and
rate-limited; monitoring + alerts active; one upgrade rehearsal passed;
incident runbook published. The full gate ledger is
[public-testnet-production-gates.md](public-testnet-production-gates.md).
