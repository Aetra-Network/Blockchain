# Aetra Testnet Launch Scope

## Overview

This document defines the **testnet kernel**: the minimal, stable set of
functionality that constitutes the first runnable public testnet. The first
public testnet is a **validator-liveness** network — validators self-bond and
produce blocks. Features that are not yet economically live (official
liquid-staking pools, AVM smart contracts) are documented here as **deferred**
and are turned on in a later phase after their hardening and audit gates pass.
Anything not listed as live is out of scope for the first testnet.

The authoritative, machine-checked module classification is
`app/launch_module_inventory.json` (enforced by
`ValidatePublicTestnetLaunchProfile`); this document is the human-readable
summary and must stay consistent with it.

## Testnet Kernel (live at first launch)

### 1. Core Blockchain Infrastructure
- **Cosmos SDK + CometBFT node** (`aetrad` binary)
  - Standard consensus, mempool, ABCI
  - Deterministic execution
  - Block lifecycle management

### 2. Address & Wallet Compatibility
- **Wallet compatibility layer**
  - User-facing addresses start with the `AE` prefix
  - Raw/internal and validator addresses use bech32 (`ae1…`); the legacy `4:`
    and `-7:` raw forms were removed
  - Private keys and seed phrases never stored on-chain

### 3. Native Balance Layer
- **Bank module** for native token balances
  - Standard fungible transfers
  - Fee payment in the native denom (`naet`)
  - Custom asset creation (token / NFT / DEX-style) is contract-only and is
    therefore deferred together with the AVM (see section 6)

### 4. Account & Security
- **Native account / auth module**
- **Freeze functionality**
- **Storage rent**
- **Delegator protection**

### 5. Staking (validator self-bond)

For the first testnet, consensus is driven by **validator self-bond through the
standard SDK staking module**:

- Validators self-bond `naet` and are selected into the active set by bonded
  stake.
- **Direct user delegation to validators is disabled** — `x/nominator-pool`
  rejects the direct-delegation message route.
- **Official liquid-staking pools are deferred.** The pool subsystem
  (deposit → shares → pooled rewards → pool slashing) is implemented but is
  **not yet economically live** (no real token custody or cosmos delegation), so
  it is not advertised on the first testnet. When it is activated, user staking
  will flow through **official liquid staking** pools with a minimum deposit of
  **10 AET** per deposit and no user-chosen validators — pool operators and
  governance select validators.

### 6. AVM Smart Contracts (deferred)

- **AVM** contract execution runs through the production `x/contracts` runtime
  (the custom Aetralis stack VM). The executable AVM spec, compiler, and tooling
  live in `x/aetravm`, which is not app-wired as a runtime module. The AVM is a
  custom bytecode VM — **not** WASM/EVM.
- Contract upload, instantiate, execute, and query, and all contract-standard
  assets, are **disabled on the first testnet** (`contracts.Params.Enabled =
  false` in the generated launch genesis) and are enabled later via governance
  `MsgUpdateContractParams`, once the AVM bytecode-verification, adversarial, and
  audit gates pass.

### 7. Fee & Economy
- **Dynamic fee market**
  - Deterministic congestion-based pricing
  - Reputation-weighted priority
  - Burn, treasury, and validator-reward distribution
- **Fee collector**, **burn**, **treasury**, and **emissions** modules

## Module Status Notes

| Module | Status |
|--------|--------|
| `x/aetravm` | Executable AVM spec, compiler, and tooling only; the production runtime is `x/contracts` (not app-wired as a runtime module) |
| `x/contracts` | Production AVM runtime — **disabled** on the first testnet; enabled later via governance |
| `x/aetra-economics` | `launch_support` — wired, KV-backed governance-owned economic policy / query surface (not a core consensus driver) |
| `x/aetra-staking-policy` | `launch_support` — wired, KV-backed pool / validator allocation policy surface |
| `x/aetra-validator-score` | `launch_support` — wired, KV-backed deterministic validator-score surface |

Earlier revisions of this file labelled the three policy modules above as
"prototype — in-memory state"; that is stale. They are wired, KV-backed
`launch_support` modules per `app/launch_module_inventory.json`.

## User Staking Guide

```
# First testnet: validators self-bond via the standard staking module.
# Direct user delegation to a validator is DISABLED and will fail:
aetrad tx staking delegate [validator-addr] 10aet --from my-wallet   # rejected

# Deferred (enabled in a later phase): deposit to an official liquid staking pool
aetrad tx nominator-pool deposit-to-official-liquid-staking \
  --pool-id official-pool-1 \
  --amount 10aet \
  --from my-wallet
```

## Testnet Kernel Verification

Run the launch scope test:
```bash
go test ./docs/... -run TestTestnetKernel
```

## Revision History

| Version | Date | Changes |
|---------|------|---------|
| 1.0 | 2026-06-09 | Initial testnet kernel definition |
| 1.1 | 2026-07-13 | First testnet scoped to validator-liveness. Pool staking and AVM documented as deferred; addresses corrected to bech32 `ae1…`; policy modules corrected from "prototype / in-memory" to wired `launch_support`; AVM corrected from "WASM/EVM" to the custom `x/contracts` runtime. |
