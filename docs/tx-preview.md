# Native transaction preview

`aetrad tx preview` is the native pre-sign dry-run surface for wallets, dApps, and operators.
It decodes a generated or signed transaction and prints a JSON report that explains what the
signer is about to authorize before the transaction is signed or broadcast.

## Workflow

Build an unsigned transaction with the normal Cosmos CLI flow, then preview it:

```powershell
aetrad tx bank send AEfrom AEto 42naet --fees 2naet --generate-only > tx.json
aetrad tx preview tx.json --account AEfrom --dapp-address AEdapp
```

The command can also read stdin:

```powershell
Get-Content tx.json | aetrad tx preview -
```

Binary tx bytes are supported with:

```powershell
aetrad tx preview tx.bin --binary
```

## What the report contains

The output is designed for direct CLI display and UI rendering:

- `mode`: always `native_tx_preview`.
- `dry_run`: always `true`; preview does not write chain state.
- `mutation`: always `none`.
- `fee`: fee amount, gas limit, fee payer/granter, and fee state effect.
- `signers`: signer addresses inferred from known message types.
- `messages`: one preview item per message.
- `messages[].state_diff`: human-readable state changes such as bank debits/credits, authz grants, fee allowances, native-account changes, staking-pool changes, or AVM contract storage effects.
- `messages[].expected_events`: expected event names where statically known.
- `messages[].execution_messages`: AVM/internal/authz nested execution summaries.
- `messages[].potential_approvals`: permission and allowance changes the signer should review.
- `dapp_access`: separate view of dApp/grantee/spender/contract participation.

Unknown message types are not hidden. They are marked with an `unknown` state diff and a risk
note telling the caller to run live simulation for keeper-level behavior.

## dApp access and existing approvals

Offline preview can always show approvals contained in the transaction itself, for example:

- `authz.MsgGrant` and `authz.MsgRevoke`
- `feegrant.MsgGrantAllowance` and `feegrant.MsgRevokeAllowance`
- native-account auth policy/key changes
- AVM contract admin or execution participation

Existing access requires live chain data. Wallets or UI backends should query the chain and pass
known results into preview:

```powershell
aetrad tx preview tx.json `
  --dapp-address AEdapp `
  --existing-account-permission "authz: AEowner -> AEdapp /cosmos.bank.v1beta1.MsgSend" `
  --existing-spending-allowance "feegrant: AEowner -> AEdapp BasicAllowance 100naet" `
  --existing-contract-authorization "contract admin: AEcontract admin=AEdapp"
```

When no known access is provided, `dapp_access.existing_access_status` is
`not_queried_offline` and the report includes query hints. When known access is provided, the
status is `provided_by_caller`.

## Preview versus simulate

`tx preview` is a pre-sign intent/diff view. It is deterministic, local, and safe to run before a
signature exists.

`tx simulate` is still required for live ante handler, gas, account sequence, mempool, and keeper
execution checks against a node. The intended production wallet flow is:

```text
generate-only -> tx preview -> user signs -> tx simulate/broadcast
```

This split prevents wallets from showing only gas estimation when the user also needs to see
balance changes, authorization changes, contract execution intent, and expected events.
