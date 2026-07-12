# Aetra Validator Guide

This is the canonical validator operator guide for public testnet and later
launches.

## Hardware

An Aetra validator runs on an average consumer PC. Decentralization and
security are the priority; block time is deliberately relaxed (5-8s) so that
ordinary hardware stays sufficient as the validator set grows.

Baseline (final for public testnet):

```text
CPU: 4-8 modern cores (mid-range desktop CPU)
RAM: 16 GB minimum (32 GB recommended for archive/heavy indexing)
Storage: 500 GB - 1 TB NVMe SSD
Network: stable 100 Mbps+ with low packet loss
```

## Validator Set Size

Phased growth, each ceiling raised by governance after load-testing the
previous phase: genesis 100-128 (genesis `max_validators` = 128), growth
150-200, mature 250-300. Sets of 500+ are not supported.

## OS

Linux is the preferred production environment. Windows is supported for local
tooling, build verification, and rehearsal scripts.

## Linux Quick Start

Production validators run Linux. `scripts/validator/*.sh` covers the
essential lifecycle so operators are not translating the PowerShell examples
below by hand. Each script is self-contained bash (`set -euo pipefail`) and
accepts `--home`/`-H` to target a non-default node home.

```bash
git clone https://github.com/Aetra-Network/Blockchain.git
cd Blockchain
./scripts/validator/build.sh                                   # build\aetrad -> build/aetrad
./scripts/validator/init.sh -m <moniker> -c <chain-id>          # aetrad init
./scripts/validator/join.sh -H "$HOME/.aetra" \
  -g <genesis-url> -p <persistent-peers> -s <seeds>             # install + validate genesis, set peers
./scripts/validator/start.sh --daemon --metrics                 # background start, PID + log file
./scripts/validator/health.sh -m 0.0.0.0:27780                  # sync status, peer count, metrics check
./scripts/validator/stop.sh                                     # graceful stop
```

`init.sh` also accepts `-g`/`-p`/`-s` directly for a one-shot init-and-join.
`start.sh` without `--daemon` runs in the foreground instead. For a
systemd-managed node (recommended for production), see
[Systemd Unit](#systemd-unit) below instead of `start.sh`/`stop.sh`.

Each script's `-h`-equivalent is its own top-of-file usage comment; run any of
them with no required arguments to see the error message listing the flags.

## Systemd Unit

`scripts/validator/aetrad.service` is an installable systemd unit template
that runs the node under Cosmovisor (see [Upgrade](#upgrade) and
[docs/COSMOVISOR.md](COSMOVISOR.md)):

```bash
sudo cp scripts/validator/aetrad.service /etc/systemd/system/aetrad.service
# edit User/Group and the DAEMON_HOME / ExecStart path for this host
sudo systemctl daemon-reload
sudo systemctl enable --now aetrad
sudo journalctl -u aetrad -f
```

## Build Or Download Binary

Build from source or use the signed release artifact published for the target
network.

Build example:

```powershell
.\scripts\build-aetrad.ps1
build\aetrad.exe version --long --output json
```

Release operators should verify the release checksum and commit before first
start.

## Version Verification

Verify the binary version, commit, and dirty flag before joining a network:

```powershell
build\aetrad.exe version --long --output json
```

The verified version must match the published release notes and upgrade plan.

## Chain ID

Use the exact published chain ID. Do not invent a local testnet chain ID for a
public join.

## Genesis Validation

After downloading genesis, validate it locally before the first node start:

```powershell
build\aetrad.exe genesis validate-genesis $HOME\config\genesis.json --home $HOME
```

Genesis validation must pass before state sync, snapshot restore, or a
full-from-genesis start.

## Keyring

Use a secure keyring backend for validator operator keys.

```powershell
build\aetrad.exe keys add <key-name> --home $HOME --keyring-backend os
build\aetrad.exe keys show <key-name> -a --home $HOME --keyring-backend os
```

## Validator Key Safety

Never copy `priv_validator_key.json` to sentries or external hosts. Preserve
`priv_validator_state.json` across restarts and upgrades. Never publish keyring
files, mnemonics, or node keys.

## State Sync

State sync is the preferred fast join path when the network publishes a trust
height, trust hash, and supported RPC list.

Example:

```powershell
# edit $HOME\config\config.toml with trust height/hash and rpc_servers
build\aetrad.exe start --home $HOME
```

Operators must verify the trusted height and hash from the launch announcement.

## Snapshots

Publishers should provide snapshot height, archive checksum, and source
validator identity. Joiners should keep at least one recent snapshot and one
fallback snapshot available until after the next coordinated upgrade.

## Create Validator

Fund the validator account first, then create the validator with the published
chain ID and `naet` fees.

```powershell
$VAL_PUBKEY = build\aetrad.exe comet show-validator --home $HOME
build\aetrad.exe tx staking create-validator `
  --amount 100000000naet `
  --pubkey $VAL_PUBKEY `
  --moniker <moniker> `
  --chain-id $CHAIN_ID `
  --from <key-name> `
  --home $HOME `
  --keyring-backend os `
  --fees 1000000naet `
  --commission-rate 0.05 `
  --commission-max-rate 0.20 `
  --commission-max-change-rate 0.01 `
  --min-self-delegation 1 `
  --node tcp://127.0.0.1:26657 `
  -y
```

## Monitor

Monitor at least:

- block height;
- validator voting power;
- peer count;
- disk usage;
- process restart count;
- RPC, REST, and gRPC health;
- missed blocks and any jail/slash warnings.

Several of the signals above (validator voting power, peer count, RPC/REST/gRPC
health, missed blocks, jail status) are read from CometBFT's own metrics and
`aetrad query staking validators` / `aetrad status` — monitor them from those
sources today.

Prometheus metrics (enable with `--observability-metrics true --observability-metrics-addr 0.0.0.0:27780`):

```powershell
# Check metrics endpoint
curl http://localhost:27780/metrics
```

The Aetra process endpoint currently populates block-liveness and
fee/economic-controller series (block height/time, block-processing and tx
latency, module errors, fees accepted/rejected, inflation/burn/validator-fee
controller output). The validator-health series (`aetra_validator_missed_blocks_total`,
`aetra_validator_uptime_bps`, `aetra_validator_concentration_bps`,
`aetra_validator_top_n_power_bps`, `aetra_slashing_events_total`,
`aetra_validator_jail_events_total`) and several economic gauges
(`aetra_economy_bonded_ratio_bps`, `aetra_economy_estimated_apr_bps`,
`aetra_economy_burned_fees_naet`, `aetra_economy_treasury_balance_naet`) are
declared and scrapeable but **not yet emitted by this endpoint** — do not build
alerts on them until they are wired (see `observability.DefaultPublicMetricSpecs`
`Emitted` flags for the live status). Use the CometBFT/CLI sources above for
validator health in the meantime.

gRPC queries (any module, port 9090):

```powershell
# Query constitution
build\aetrad.exe query constitution params --node tcp://127.0.0.1:9090 --output json
# Query system registry entities
build\aetrad.exe query system-registry system-entities --node tcp://127.0.0.1:9090 --output json
# Query nominator pools
build\aetrad.exe query nominator-pool pools --node tcp://127.0.0.1:9090 --output json
```

Useful checks:

```powershell
build\aetrad.exe query staking validators --node tcp://127.0.0.1:26657 --output json
build\aetrad.exe status --node tcp://127.0.0.1:26657 --output json
```

## Restart

Restart with the same home directory. Preserve:

- `config\priv_validator_key.json`;
- `config\priv_validator_state.json`;
- `config\node_key.json`;
- `data\`;
- logs needed for incident review.

Do not roll back validator state to an earlier height unless the incident owner
has explicitly approved recovery steps.

## Upgrade

Follow the coordinated upgrade plan and rehearse it before public launch.
Upgrade handler names must match the announced plan name. Use
[docs/COSMOVISOR.md](COSMOVISOR.md) for Cosmovisor-managed nodes and
[docs/upgrade-playbook.md](upgrade-playbook.md) for rehearsal flow and
post-upgrade validation.

For a Cosmovisor-managed Linux node, stage the new binary with:

```bash
./scripts/validator/build.sh -o ./build/aetrad-<upgrade-name>
./scripts/validator/cosmovisor-upgrade.sh -H "$HOME/.aetra" \
  -n <upgrade-name> -b ./build/aetrad-<upgrade-name>
```

This only stages the binary into the Cosmovisor upgrade slot; it never
touches the currently-running binary, and refuses to overwrite an
already-staged upgrade slot.

## Incident Response

If block production stalls, voting power diverges, or a validator key is
suspected compromised, follow the published incident response runbook and keep
evidence intact.

See [Testnet Incident Response](testnet-incident-response.md).
