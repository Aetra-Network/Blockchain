# Validator Onboarding

This guide is for a clean public testnet validator join. Localnet examples use PowerShell paths; public operators must replace local paths, chain id, peers, and keyring backend with launch values.

## Hardware Target

An Aetra validator runs on an average consumer PC. Decentralization and
security are the design priority — block time is deliberately relaxed (5-8s)
so that ordinary hardware stays sufficient even as the validator set grows.

```text
CPU: 4-8 modern cores (mid-range desktop CPU)
RAM: 16 GB minimum (32 GB recommended for archive/heavy indexing)
Storage: 500 GB - 1 TB NVMe SSD
Network: stable 100 Mbps+, low packet loss
OS: Linux recommended, Windows local tooling supported for development
```

This spec is final for the public testnet. AVM execution is gas-capped and
lightweight (a simple metered bytecode interpreter), so consensus gossip and
state growth — not contract execution — are the binding resources, and both
fit this profile at the target block time.

## Validator Set Size

The validator set grows in phases. Each phase ceiling is raised by
governance only after the previous phase is proven under load:

```text
Genesis phase: 100 minimum, 128 maximum   (genesis max_validators = 128)
Growth phase:  150-200                    (raised via governance)
Mature phase:  250-300                    (raised via governance)
Hard reject:   500+ is not a supported validator-set size
```

The 100 floor provides a meaningful decentralization margin far above the
BFT safety minimum; the 300 ceiling is a load-test-gated target that keeps
per-node consensus overhead (vote gossip and commit verification) trivially
affordable on the consumer hardware profile above at 5-8s blocks.

## Build

Linux (production validators):

```bash
git clone https://github.com/Aetra-Network/Blockchain.git
cd Blockchain
./scripts/validator/build.sh
build/aetrad version --long --output json
```

Windows (local tooling/dev only):

```powershell
git clone https://github.com/Aetra-Network/Blockchain.git
cd Blockchain
.\scripts\build-aetrad.ps1
build\aetrad.exe version --long --output json
```

Verify that the commit matches the published testnet release commit.

### Chain IDs

Public Aetra networks use plain numeric chain IDs:

```text
Mainnet: CHAIN_ID = "1"
Testnet: CHAIN_ID = "2"
```

Development networks keep the dash-separated form (e.g. `aetra-local-1`).
The chain ID must match the published genesis exactly. Do not invent a
custom chain ID for public join.

## Initialize Node

Linux, using `scripts/validator/init.sh` (also handles genesis install/validation
and peer configuration in one step; see `-g`/`-p`/`-s`):

```bash
./scripts/validator/init.sh -m <moniker> -c <testnet-chain-id> \
  -g <published-genesis-url> -p <persistent-peers> -s <seeds>
```

To (re)join an already-initialized home with a new genesis/peer set without
touching keys, use `scripts/validator/join.sh` instead (same `-g`/`-p`/`-s`
flags, operating on an existing `--home`).

Windows (local tooling/dev only):

```powershell
$CHAIN_ID = "<testnet-chain-id>"
$HOME = "$env:USERPROFILE\.aetra"
build\aetrad.exe init <moniker> --chain-id $CHAIN_ID --home $HOME
```

Replace `$HOME\config\genesis.json` with the published genesis file, then validate:

```powershell
build\aetrad.exe genesis validate-genesis $HOME\config\genesis.json --home $HOME
```

Configure peers and persistent peers from the launch announcement. Do not reuse localnet keys.

### Clean-Machine Drill

Before publishing a validator runbook as ready, run the local clean-machine
drill. It starts an existing trusted validator set, initializes a separate
fresh node home, copies only the published genesis, configures published peers,
creates a new validator key, funds it, joins the node, sends
`staking create-validator`, verifies validator-set membership, signing-info,
peer connectivity, status, restart safety, and the unjail command path:

```powershell
.\scripts\localnet\validator-onboarding-drill.ps1 `
  -OutputDir .work\validator-onboarding-drill `
  -Binary .\build\aetrad.exe `
  -ChainId aetra-local-validator-onboarding-1 `
  -SkipBuild
```

The evidence file is written to
`.work\validator-onboarding-drill\evidence\validator-onboarding-drill.json`.
The drill is passing only when `result = "passed"`, the validator set increases
by one, the fresh node has peers, signing-info includes the expanded set, and
the node restarts without deleting validator state.

### Port Configuration

Default ports for testnet:

| Service | Port |
|---------|------|
| P2P | `26656` (offset per node) |
| RPC | `26657` |
| REST API | `1317` |
| gRPC | `9090` |
| Prometheus | `27780` |
| pprof | `6060` |

For single-machine multi-node setups, P2P starts at `16656`, all other ports increment by node index.

The gRPC endpoint (`localhost:9090`) serves all module query and transaction services. Prototype module gRPC surfaces include system-registry, constitution, config, nominator-pool, contracts, aetra-economics, aetra-staking-policy, and aetra-validator-score.

## Create Validator Key

Use a secure keyring backend for public testnet:

```powershell
build\aetrad.exe keys add <key-name> --home $HOME --keyring-backend os
build\aetrad.exe keys show <key-name> -a --home $HOME --keyring-backend os
```

Store mnemonic backup offline. Never commit mnemonics, keyrings, `priv_validator_key.json`, or node keys.

## Sync

The network launch profile must provide state sync support, snapshots, pruning profiles, and an archive node profile.

Linux, foreground or background with `scripts/validator/start.sh` (for a
production systemd-managed node, use the unit in `scripts/validator/aetrad.service`
instead -- see [docs/VALIDATOR.md](VALIDATOR.md#systemd-unit)):

```bash
./scripts/validator/start.sh                      # foreground, from genesis or state sync
./scripts/validator/start.sh --daemon             # background: PID + log in the node home
```

For state sync, edit `config/config.toml` first (`enable=true`, `rpc_servers`,
`trust_height`, `trust_hash`, `trust_period` from the launch announcement),
then start as above.

Windows (local tooling/dev only):

```powershell
build\aetrad.exe start --home $HOME
```

Check sync status:

```bash
./scripts/validator/health.sh
```

or directly:

```powershell
build\aetrad.exe status --node tcp://127.0.0.1:26657 --output json
```

The node is caught up when `catching_up` is false.

### Observability Metrics

Aetra exposes a Prometheus-compatible metrics endpoint. Enable it on node start:

```powershell
build\aetrad.exe start `
  --home $HOME `
  --observability-metrics true `
  --observability-metrics-addr 0.0.0.0:27780
```

Metrics available at `http://localhost:27780/metrics` in Prometheus text format. Key metric categories:

- **Block**: height, time, finality latency, processing time
- **Transactions**: latency, fees accepted/rejected, failure reasons
- **Economic**: inflation, bonded ratio, APR, burn ratio, treasury
- **Staking**: centralization, concentration, validator incentives, slashing
- **Validator**: missed blocks, uptime, concentration, profitability

For production monitoring, scrape this endpoint with Prometheus and alert on missed blocks, jailing events, sync lag, and process restarts.

Recommended pruning profiles:

```toml
# Normal validator profile.
pruning = "default"

# Archive node profile. Preserves historical state.
pruning = "nothing"

# Low-disk development or non-critical nodes only.
pruning = "everything"
```

For public testnet, operators must use published snapshot/state-sync endpoints and verify trust height, trust hash, and trust period values from the launch announcement.

The launch announcement must publish at least two RPC servers, the trust
height, trust hash, trust period, snapshot archive checksum, and source
validator identity. A validator should not follow an unpublished trust hash or
single-RPC state-sync path.

## Create Validator

Fund the validator account from the faucet or launch allocation first. Then create the validator using `naet`. The
current CLI expects a validator JSON file, not legacy inline
`--amount/--pubkey` flags:

```powershell
$VAL_PUBKEY = build\aetrad.exe comet show-validator --home $HOME
$VALIDATOR_JSON = "$HOME\config\validator.json"
@"
{
  "pubkey": $VAL_PUBKEY,
  "amount": "100000000naet",
  "moniker": "<moniker>",
  "identity": "",
  "website": "",
  "security": "",
  "details": "",
  "commission-rate": "0.05",
  "commission-max-rate": "0.20",
  "commission-max-change-rate": "0.01",
  "min-self-delegation": "1"
}
"@ | Set-Content -Encoding utf8NoBOM -LiteralPath $VALIDATOR_JSON

build\aetrad.exe tx staking create-validator `
  $VALIDATOR_JSON `
  --chain-id $CHAIN_ID `
  --from <key-name> `
  --home $HOME `
  --keyring-backend os `
  --gas 500000 `
  --fees 1000000naet `
  --node tcp://127.0.0.1:26657 `
  -y
```

Verify:

```powershell
build\aetrad.exe query staking validators --node tcp://127.0.0.1:26657 --output json
build\aetrad.exe query tendermint-validator-set --node tcp://127.0.0.1:26657 --output json
```

## Operations

Monitor:

- latest block height,
- validator voting power,
- missed block counter,
- disk usage,
- process restart count,
- peer count,
- RPC/indexer lag if serving public endpoints.

Before restart, stop cleanly (Linux: `./scripts/validator/stop.sh`, which sends
SIGTERM and waits before falling back to SIGKILL; systemd: `systemctl stop
aetrad`) and preserve `$HOME\data`, `$HOME\config\priv_validator_key.json`,
`$HOME\config\priv_validator_state.json`, and `$HOME\config\node_key.json`.
Restart safety is mandatory: losing or rolling back validator state can
create double-sign risk.

State management readiness requires export/import reliability and deterministic app hash across restarts. Before public launch, the release process must export genesis, import it into a fresh home, restart the node, and verify deterministic app hash behavior for the same state.

Run the export/import smoke test to validate:

```powershell
.\tests\e2e\export_import_smoke.ps1 -Home $HOME -ChainId $CHAIN_ID
```

## Upgrade Readiness

Before joining a public testnet, verify the published release artifact and upgrade notes:

```powershell
build\aetrad.exe version --long --output json
Get-Content .\docs\upgrade-playbook.md
Get-Content .\docs\public-testnet-production-gates.md
```

Operators must confirm:

- the binary version, commit, and dirty flag match the launch announcement;
- the chain-id is the published `aetra-...` testnet chain-id;
- genesis validation passes locally before first start;
- snapshot or state-sync restore instructions are published;
- export/import and invariant CI gates passed for the release artifact;
- planned upgrade height, handler name, rollback policy, and communication channel are documented before any coordinated upgrade.

For a Cosmovisor-managed Linux node, `scripts/validator/cosmovisor-upgrade.sh`
stages the new binary into the correct upgrade slot without touching the
running binary (see [docs/VALIDATOR.md](VALIDATOR.md#upgrade) and
[docs/COSMOVISOR.md](COSMOVISOR.md)).

## Sentry Architecture

Public validators should use documented sentry architecture:

```text
public peers <-> sentry nodes <-> private validator node
```

Rules:

- never copy `priv_validator_key.json` to sentry nodes;
- expose P2P and optional public RPC from sentries, not from the private validator node;
- configure validator persistent peers to trusted sentries only;
- restrict inbound access to the validator node with firewall rules;
- serve public RPC/indexer traffic from non-validator infrastructure;
- diversify sentries across providers or regions when possible;
- preserve `priv_validator_state.json` during restarts and upgrades.

## CosmWasm Contract Smoke

If and only if the launch config explicitly enables CosmWasm, deploy the smoke contract:

```powershell
.\tests\e2e\cosmwasm_smoke.ps1 -EnableWasm -ContractWasm .\artifacts\cw_template.wasm -Node tcp://127.0.0.1:26657 -ChainId $CHAIN_ID -AppHome $HOME -From <key-name>
```

If wasm is not enabled, the disabled-by-default check must pass:

```powershell
.\tests\e2e\cosmwasm_smoke.ps1 -Node tcp://127.0.0.1:26657
```
