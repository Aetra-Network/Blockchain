# Aetra public-testnet monitoring

Ready-to-use monitoring artifacts for the Aetra process metrics endpoint. These
satisfy the public-testnet dashboard-readiness gate described in
[docs/architecture/observability-public-metrics.md](../../docs/architecture/observability-public-metrics.md).

```
deploy/monitoring/
├── grafana/
│   └── aetra-public-testnet-dashboard.json   # importable Grafana dashboard (5 panels)
└── prometheus/
    ├── prometheus.yml                        # scrape config
    └── alerts.yml                            # alerting rules
```

## 1. Enable the endpoint on each node

Start `aetrad` with metrics on (see [docs/VALIDATOR.md](../../docs/VALIDATOR.md#monitor)):

```bash
aetrad start --observability-metrics true --observability-metrics-addr 0.0.0.0:27780
```

Verify: `curl http://<node>:27780/metrics` should list `aetra_*` series.

## 2. Scrape with Prometheus

Edit `prometheus/prometheus.yml` — replace the `targets` list with your node
endpoints — then run Prometheus with it:

```bash
prometheus --config.file=deploy/monitoring/prometheus/prometheus.yml
```

`alerts.yml` is loaded via `rule_files`. Point `alerting.alertmanagers` at your
Alertmanager (a commented stanza is included) to route the alerts.

## 3. Import the Grafana dashboard

Grafana → Dashboards → Import → upload
`grafana/aetra-public-testnet-dashboard.json`, and select your Prometheus data
source when prompted. Panels, per the architecture spec:

- **Consensus health** — block height, block-processing time, finality latency, node sync status;
- **Validator reliability** — uptime (min/avg), missed blocks, jail/unjail/slashing events;
- **Decentralization** — voting-power concentration and top-10/20/33 shares;
- **Economics** — inflation, bonded ratio, estimated APR, burned coins, treasury balance;
- **VM and transactions** — failed-tx reasons by codespace, contract execution gas.

## Metric conventions

- `_bps` series are basis points (0–10000 = 0–100%); the dashboard divides by 100 to show percent.
- `_naet` series are raw naet (1 AET = 1e9 naet); the dashboard divides by 1e9 to show AET.
- Summaries (`_seconds`) expose `_sum` and `_count`; averages are `rate(_sum) / rate(_count)`.
- Labels are bounded (validator state, bounded slash reason, top-N bucket, codespace) — never addresses/hashes/pool ids.

## Not yet emitted

Two required series are declared but not yet populated, so their panels/queries
show no data (the live status is the `Emitted` flags on
`observability.DefaultPublicMetricSpecs`):

- `aetra_node_sync_status` — sync state is owned by CometBFT; use its native
  instrumentation (config.toml `[instrumentation] prometheus = true`) or
  `aetrad status` until an in-process bridge is added.
- `aetra_contract_execution_gas` — `x/contracts` sits inside the determinism
  gate's float-free zone, so the emitter cannot live there without a gate change.

Everything else on the dashboard is live.
