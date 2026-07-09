# Aetra Testnet Health Check Documentation

This document defines the canonical health checks for Aetra localnet and
public testnet operator runs. Prefer the bundled script first; use the manual
commands below when you need to inspect a specific surface.

## Canonical Localnet Check

Run the bundled health script against the localnet output directory:

```powershell
.\scripts\localnet\health.ps1 -OutputDir .localnet
.\scripts\localnet\health.ps1 -OutputDir .localnet -Json
```

The script checks:

- RPC status
- block height progress
- `catching_up` state
- peer count
- validator signing info
- REST health
- gRPC health
- recent logs and process snapshot metadata

## Manual Checks

### 1. Process Alive

```powershell
Invoke-RestMethod http://127.0.0.1:26657/status
```

Expected:

- HTTP 200
- `result.sync_info` present

### 2. RPC Status

```powershell
build\aetrad.exe status --node tcp://127.0.0.1:26657 --output json
```

Expected fields:

```json
{
  "NodeInfo": {
    "version": "...",
    "network": "aetra-testnet-1"
  },
  "SyncInfo": {
    "latest_block_height": "12345",
    "catching_up": false
  },
  "ValidatorInfo": {
    "address": "...",
    "voting_power": "1000000"
  }
}
```

### 3. Block Height Increasing

```powershell
$height1 = (Invoke-RestMethod http://127.0.0.1:26657/status).result.sync_info.latest_block_height
Start-Sleep -Seconds 10
$height2 = (Invoke-RestMethod http://127.0.0.1:26657/status).result.sync_info.latest_block_height
[int64]$height2 -gt [int64]$height1
```

Expected:

- later height is greater than earlier height

### 4. Catching Up

```powershell
(Invoke-RestMethod http://127.0.0.1:26657/status).result.sync_info.catching_up
```

Expected:

- `False`

### 5. Peer Count

```powershell
(Invoke-RestMethod http://127.0.0.1:26657/net_info).result.n_peers
```

Expected:

- `1+` peers for a multi-validator localnet
- `0` only when the node is intentionally isolated

### 6. Validator Signing

```powershell
build\aetrad.exe query staking validators --node tcp://127.0.0.1:26657 --output json
build\aetrad.exe query slashing signing-infos --node tcp://127.0.0.1:26657 --output json
Invoke-RestMethod http://127.0.0.1:26657/validators?per_page=100
```

Expected:

- bonded validators are present
- `jailed` is `false` for healthy validators
- missed blocks stay within the configured alert threshold

### 7. App Invariant

```powershell
build\aetrad.exe export --home $HOME
```

Expected:

- command exits `0`
- export does not panic
- exported state can be used for later import / restart evidence

## Prometheus Metrics

Expose metrics at `http://localhost:27780/metrics` when observability metrics
are enabled:

| Metric | Description |
|--------|-------------|
| `aetrad_block_height` | Current block height |
| `aetrad_validator_voting_power` | Validator voting power |
| `aetrad_peers` | Number of connected peers |
| `aetrad_missed_blocks` | Missed blocks in the current window |

## Alert Thresholds

| Condition | Severity | Action |
|-----------|----------|--------|
| Node unreachable > 60s | Critical | Restart process |
| Catching up > 5 min | Warning | Check peer connections |
| Peers < 2 | Warning | Check network configuration |
| Height not increasing > 120s | Critical | Check consensus |
| Missed blocks > 50% | Critical | Check validator status |

## Troubleshooting

### Node Not Producing Blocks

1. Check `.\scripts\localnet\health.ps1 -OutputDir .localnet -Json`.
2. Verify peer connections with `Invoke-RestMethod http://127.0.0.1:26657/net_info`.
3. Check validator signing info with `build\aetrad.exe query slashing signing-infos --node tcp://127.0.0.1:26657 --output json`.

### Peers Not Connecting

1. Verify firewall rules allow port `26656`.
2. Check seed nodes are accessible.
3. Verify node ID and peer list in the launch announcement.

### Invariant Check Failing

1. Check logs for panic or assertion messages.
2. Verify genesis configuration.
3. Run `build\aetrad.exe export --home $HOME` to confirm whether state export still works.
