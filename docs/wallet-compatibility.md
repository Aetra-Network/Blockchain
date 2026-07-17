# Wallet Compatibility Extension (AWCE-1)

> Reconstructed 2026-07-09: the previous copy of this file was lost (it was
> never committed to git). This version is rebuilt from the structural
> requirements enforced by `docs/wallet_compatibility_test.go` and verified
> against the current native-account, addressing, and nominator-pool code.

This document defines the Aetra Wallet Compatibility Extension, standard
**AWCE-1**, so that third-party wallets, signers, and explorers can integrate
with native accounts, official liquid staking, and the AE address family
without guessing at conventions.

## Canonical Addresses

Aetra uses two address families, and a wallet integration must never mix them:

- `AE...` is the only user-facing account, validator-facing, pool, and
  contract-owner address format shown to end users, in APIs, and in
  transaction previews.
- `ae1…` (standard bech32) is the raw/internal address format used for
  proof metadata and internal store keys. It is derived from the same public
  key as the `AE...` address, but it is not a user-facing or "primary"
  address — wallets must not present it as the account's normal address. (The
  legacy `4:<64 lowercase hex>` raw string is no longer produced or parsed.)

The underlying Cosmos SDK bech32 prefix `ae` (`aevaloper...`, `aevalcons...`)
exists only for SDK/tooling compatibility on the validator and consensus key
paths. `aevaloper`/`aevalcons` addresses are internal validator-operator and
consensus-key identifiers only — a wallet must never present them as a normal
user account address, and normal users never hold or sign with them.

## Address Derivation

```text
derive(pubkey) -> AE...            # user-facing account address
format_raw(pubkey_hash) -> ae1...  # raw/internal bech32 address, same key
```

- `AE...` and `ae1...` are derived from the same public key and must round-trip
  to each other; a wallet should treat a derivation mismatch as a fatal error,
  not a warning.
- Address derivation does not change across account migration, auth-policy
  updates, recovery, multisig changes, metadata changes, or staking changes.

## Dual-Address Example

```text
pubkey:      02a1...ef (secp256k1, compressed)
AE address:  AE...            <- show this to the user everywhere
raw address: ae1...9x2        <- bech32 proof/internal use only, never the primary label
```

## Signing Scheme

Wallets sign Aetra transactions the same way as any Cosmos SDK chain:
`SIGN_MODE_DIRECT` over the protobuf `SignDoc`, secp256k1 keys, standard
Cosmos HD derivation. There is no custom signature algorithm and no
seed-phrase handling outside the wallet's own secure key storage — a
compliant wallet keeps mnemonic and private key material inside its own
signing boundary and never transmits it to Aetra RPC, CLI, or any external
service.

## Account Lifecycle

A native account has one virtual state and several persistent states:

- `inactive` is virtual only: a derivable `AE...` address with no persistent
  state, not present in genesis or export, and it accrues no storage rent.
- `active`, `frozen`, `recovered`, `archived`, and `closed` are persistent
  states with explicit transition rules.

### Activation

`MsgActivateAccount` is the normal first persistent-state transition. It
validates that the requested `AE...` address matches the address derived from
the supplied public key, that the account is not already active, and that
account-number/sequence initialization is deterministic. Activation is
idempotency-safe: a duplicate activation for the same account is rejected.

### Frozen / Recovery Flow

`frozen` is fully recoverable: balance, sequence, ownership links, auth
policy, reputation links, pool shares, unbondings, rewards, and contract data
are preserved while frozen. A whitelisted set of actions remains available on
a frozen account (for example, paying down storage-rent debt or running an
authorized recovery), while ordinary message flows are rejected until the
account is unfrozen.

Recovery never accepts a bare list of claimed recovery-key identifiers.
Recovery keys are public on-chain state, so naming them proves nothing; each
recovery signer must produce a cryptographic co-signature over the canonical
recovery digest, and the message is rejected if fewer co-signatures verify
than the configured recovery threshold.

## Pool-Based Only

Normal users stake through the official liquid staking pool path only:

```text
User -> Liquid Staking Contract / Pool -> Validators
```

A user deposits AET into the pool and receives pool shares; the user does not
choose a validator, and direct user delegation to a validator through the
normal wallet path is disabled. Validator selection, allocation, and
rebalancing are handled centrally and deterministically by the pool/allocation
logic, not by an individual `MsgDelegate`-style call from the wallet.

## Auth Policy System

An account's authorization policy controls which keys and thresholds can
authorize its actions; it never changes the account's `AE...`/`ae1...`
addresses. Supported modes:

- `single_key` — one configured public key authorizes ordinary actions.
- `multisig` — a fixed set of public keys signs together.
- `threshold` — at least N of the configured signers are required.
- `weighted` — signer weights must sum to the configured threshold.
- `two_device` — a primary key plus a device key for protected operations.
- `timelock` — protected updates become executable only after a delay.
- `recovery` — a recovery policy moves the account to `recovered` once its
  threshold of co-signatures is met.
- `spending_limits` — small transfers may use weaker authorization while
  large transfers, staking changes, and auth updates require the full policy.

## Account Metadata

Accounts carry a bounded, versioned metadata record (for example a metadata
hash set through `MsgUpdateAccountMetadata`). Metadata never carries private
keys, mnemonics, or other secret material — only public, bounded-size
references.

## Security Rules

- A wallet must never ask a user to reveal a seed phrase or private key to
  Aetra RPC, a block explorer, or any third party; key material stays inside
  the wallet's own signing boundary at all times.
- `AE...` is the only address a wallet may present as the user's account
  address; `ae1...` and `aevaloper`/`aevalcons` values are internal/validator
  identifiers only and must be labeled as such if shown at all.
- A wallet must not offer direct validator selection as the normal staking
  action; the default staking action is depositing into the official pool.
- Genesis, export, events, logs, and proof metadata never contain private
  keys or seed phrases.

## Machine-Readable Extension Descriptor

Wallets can detect AWCE-1 support from a descriptor shaped like this:

```json
{
  "standard": "AWCE-1",
  "canonical_user_address": "AE...",
  "raw_address": "ae1...",
  "signing": "cosmos-signdoc-secp256k1",
  "default_hd_path": "m/44'/118'/0'/0/0",
  "features": [
    "native-account-activation",
    "official-liquid-staking-pool",
    "auth-policy-recovery"
  ]
}
```

## CLI Reference

There is no dedicated `aetrad tx native-account ...` command tree; native
account messages (`MsgActivateAccount`, `MsgUpdateAuthPolicy`, `MsgRotateKey`,
`MsgRecoverAccount`, `MsgFreezeAccount`, `MsgUnfreezeAccount`,
`MsgPayStorageDebt`, `MsgUpdateAccountMetadata`) are built and signed directly
through the standard Cosmos SDK transaction builder against the native
account's proto message types, then broadcast like any other signed
transaction.

Pool-based staking does have a CLI surface:

```powershell
aetrad tx nominator-pool deposit-to-pool <pool-id> 10000000naet --from AE... --chain-id aetra-local-1 --fees 1000000naet --yes
aetrad query nominator-pool pool <pool-id> --output json
```

## Revision History

- 2026-07-09: reconstructed from `docs/wallet_compatibility_test.go`
  requirements after the previous revision was lost (untracked file, no git
  history). Content verified against `app/addressing`, `x/native-account`,
  and `x/nominator-pool` as of this date.
