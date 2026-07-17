# Aetra Blockchain

<p align="center">
  <img src="assets/aetra.png" alt="Aetra logo" width="220" />
</p>

Aetra is a sovereign Layer 1 blockchain: a deterministic, account-based
proof-of-stake network with its own smart-contract virtual machine (AVM) and
contract language (Aetralis). It is built to run on ordinary consumer
hardware, with decentralization and security prioritized over raw speed.

Aetra is copyright (c) 2026 Aetra Network and is distributed under the
[Aetra Network Source-Available License](LICENSE): you may download, use, and
run it (including operating validators), but you may not sell it, present it
as your own product, or commercially exploit Aetra Network's designs and
solutions as your own. The Aetra name and logo are covered by the
[trademark policy](docs/legal/trademark-policy.md); forks may reuse the code
under the license but may not present themselves as the official Aetra
Network.

## At a Glance

| Property | Value |
|----------|-------|
| Native asset | **AET** (1 AET = 10⁹ naet) |
| Consensus | CometBFT proof of stake |
| Chain IDs | Mainnet `18` · Testnet `-19` (the leading `-` marks a test network; dev/local ids contain `-local-` / `-testnet-`) |
| Average transfer fee | ~0.4963 AET for a standard transfer — a continuous formula, not a fixed 0.5, and it rises with network load |
| Smart contracts | AVM (deterministic virtual machine) + Aetralis language (`.atlx`) |
| Staking | Pool-based — users deposit into the official liquid-staking pool, never pick validators directly |
| Execution | Aetra Elastic Zones (AEZ): deterministic zoned execution inside one chain — one consensus, one validator set, all validators run every zone |
| Addresses | User-facing `AE…` · raw bech32 `ae1…` (the older `4:` / `-7:` string forms have been removed) |

## Fees

The fee is **not** a fixed number — it is a continuous function computed
deterministically in whole naet, so a real fee looks like `0.4963` AET, not a
round `0.5`. A standard transfer starts from a `0.4` AET anchor plus per-gas,
per-size, and per-message components (≈0.4963 AET total). On top of that:
a storage-rent charge for transactions that grow chain state, a bounded
premium/discount from the sender's reputation, and a congestion surcharge that
climbs toward a `5` AET ceiling as blocks fill. Collected fees are split
**50% burned / 35% to validators / 15% to treasury** (the authoritative
`x/fee-collector` split); validators net ~34.3% after the 2% community tax.
All rates are governance parameters.

Emission is the sole source of protocol inflation: a governance-pinned annual
target of **4%** (band 1.5–5%), calibrated so net supply growth is ~3.10% per
year at ordinary load. Because fees are burned, net growth is
throughput-dependent — above heavy sustained load the burn can exceed emission.

## Run a Validator

A validator runs on an average PC:

```text
CPU: 4-8 modern cores        RAM: 16 GB (32 GB recommended)
Disk: 500 GB - 1 TB NVMe     Network: 100 Mbps+, low packet loss
OS: Linux recommended
```

The validator set grows in phases: 100–128 at genesis, then 150–200 and
250–300 raised by governance after load testing.

- [Validator Guide](docs/VALIDATOR.md)
- [Validator Onboarding](docs/validator-onboarding.md)
- [Testnet Guide](docs/TESTNET.md)
- [Upgrades with Cosmovisor](docs/COSMOVISOR.md)

## Quick Start (Local Network)

```powershell
# Build the node binary
.\scripts\build-aetrad.ps1

# Start a local 3-validator network
.\scripts\localnet\init.ps1 -ChainId aetra-local-1 -ValidatorCount 3
.\scripts\localnet\start.ps1 -ChainId aetra-local-1

# Check health
.\scripts\localnet\health.ps1
```

No external dependencies — just the binary and CometBFT.

Shortest probes (this README keeps only the shortest probes; the full
operator runbook is in [Operator Commands](docs/operator-commands.md)):

```powershell
build\aetrad.exe version --long --output json
build\aetrad.exe status --node tcp://127.0.0.1:26657
build\aetrad.exe query bank total-supply-of naet --node tcp://127.0.0.1:26657 --output json
```

For symptom-specific fixes and recovery steps, see the
[Operator Troubleshooting Runbook](docs/operator-troubleshooting.md).

## Smart Contracts

Contracts are written in **Aetralis** (`.atlx`) — a typed, deterministic
language — and run on the **AVM**. Reference contracts (token, NFT, DAO,
DNS, staking) live in [`examples/avm/`](examples/avm/).

```powershell
build\aetrad.exe avm compile examples\avm\counter_should_be.atlx --deployer <AE-address>
```

See the [AVM overview](docs/AVM.md) and the
[language specification](docs/architecture/language-spec.md).

## Staking and Accounts

Staking is pool-based: direct user delegation to validators is disabled, so
users make an official liquid-staking pool deposit and the protocol allocates
that stake to a real `x/staking` delegation on their behalf — normal users
never select a validator. Unbonding time is network-dependent (about a week on
mainnet, short on local/testnet, set at genesis via `--unbonding-time`).
Accounts, freezing, storage rent, and reputation are described in
[Native Account, Staking, Reputation, And Rent Model](docs/native-account-staking-reputation.md).

## Elastic Zones (AEZ)

Aetra runs **Aetra Elastic Zones**: deterministic zoned execution inside a
single chain — one CometBFT consensus, one validator set, one block height, one
global state root, and every validator executes every zone. It is not sharding:
there are no separate chains, committees, or cross-shard consensus. Entities map
to one of 256 buckets, buckets map to zones through a governance-controlled
routing table, and system entities (validators, staking, governance) are pinned
to a Core Zone. A deterministic cross-zone message bus carries exactly-once,
next-block messages between zones. See
[AEZ architecture](docs/architecture/aez.md). Native `.aet` naming (the Aetra
Name System) builds on it — see [ANS](docs/architecture/ans.md).

## Repository Layout

| Directory | Contents |
|-----------|----------|
| `app/` | Node application: module wiring, economics, invariants |
| `x/` | Chain modules (accounts, fees, staking pools, contracts, ...) |
| `cmd/l1d/` | `aetrad` node binary and CLI |
| `proto/`, `api/` | Protocol definitions |
| `docs/` | Guides and architecture notes |
| `scripts/` | Build, local network, and testnet tooling |
| `examples/avm/` | Reference Aetralis contracts |
| `tests/` | Integration, e2e, and adversarial test suites |

## Token

| Field | Value |
|-------|-------|
| Name | Aetra |
| Symbol | AET |
| Base denom | `naet` (1 AET = 1,000,000,000 naet) |
| Staking / fee denom | `naet` |
| Inflation | Emission-only, 4% governance-pinned target (1.5–5% band), ~3.10% net after fee burn |
| Emission epoch | ~6 hours (1460 epochs/year) |

## Status and Security

Aetra is pre-testnet software. Known limitations are tracked honestly in
[Prototype Limitations](docs/release/prototype-limitations.md), and the
security posture (internal audit results, dependency triage, production
gates) is documented under [`docs/security/`](docs/security/). Genesis
validation, export/import round-trips, deterministic fee admission, and app
invariants run in CI on every change.
