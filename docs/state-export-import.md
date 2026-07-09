# State Export/Import Acceptance

State export/import is consensus-critical. The acceptance target is: after local
runtime flows, exported genesis validates, contains no local secrets, preserves
the active runtime state that is currently wired, and rejects corrupted import
data with a clear validation error.

## Covered State

The acceptance smoke runs a multi-validator localnet and covers only modules
that are wired in the active runtime path. Historical native tokenfactory and
DEX modules are not part of this gate; token, NFT, market, and exchange-style
application logic targets AVM contracts and standards.

| Area | Export check |
| --- | --- |
| Chain header | `chain_id` matches the local chain id |
| Fees | `app_state.fees.params.allowed_fee_denoms` is exactly `naet` |
| Bank | funded `naet` balances for validator and non-validator accounts are preserved |
| Staking | `bond_denom` is `naet`; rejected direct user delegation is not exported as a delegation |
| Security | exported JSON does not contain mnemonic, private key, keyring, seed, wallet, or validator key markers |

Run it from the repo root:

```powershell
.\tests\e2e\export_import_smoke.ps1
```

The script uses `.localnet-export-import` and shifted ports by default, then
writes exported genesis under `.work\genesis\export-import\node0-export.json`.
Both paths are runtime paths and must remain untracked.

## Corrupted Import

The smoke copies the exported state, corrupts a bank balance amount, and expects:

```powershell
build\aetrad.exe genesis validate-genesis .work\genesis\export-import\node0-export-corrupt.json --home .localnet-export-import\node0\aetrad
```

to fail with an `amount`, `invalid`, `unmarshal`, `big.Int`, or `not-an-int`
validation error.

## Unit Round Trip

The executable export/import evidence is the localnet smoke
`tests\e2e\export_import_smoke.ps1`, the public-testnet readiness workflow, and
the readiness report check `export_import_roundtrip` in
`scripts\testnet\public-testnet-readiness-report.ps1`.

## Current Limit

The smoke validates exported genesis with `genesis validate-genesis` and corrupt
input rejection. It does not yet start a multi-validator localnet from the
exported genesis because the exported validator set must be paired with matching
private validator keys and node topology. A restart-from-export localnet remains
a public-testnet readiness drill before production claims.
