# Native Account Identity

User-facing account, validator, and consensus addresses are always `AE...`.
The raw `ae1…` bech32 form is an internal/raw address form and is not accepted
by user-facing message or query validation (the legacy `4:<hex>` / `-7:<hex>`
raw strings are no longer produced or parsed).

Examples:

```text
account_address = AEAAAQAAAAAAAAAAAAAAAAAiIiIiIiIiIiIiIiIiIiIiIiIi
validator_address = AEAAAQAAAAAAAAAAAAAAAAAzMzMzMzMzMzMzMzMzMzMzMzMz
consensus_address = AEAAAQAAAAAAAAAAAAAAAAA0NDQ0NDQ0NDQ0NDQ0NDQ0NDQ0
raw_internal_key = ae1yg3zyg3zyg3zyg3zyg3zyg3zyg3zyg3z8vrlgh
```

Foreign Bech32 (non-`ae` HRP), old raw prefixes, mixed-case
raw addresses, and malformed `AE...` strings are rejected at user-facing
message and query boundaries.

Before activation, a derivable `AE...` address is a virtual inactive account.
Querying it returns an inactive non-persistent view. It is not exported in
genesis, does not accrue storage rent, and can only submit `MsgActivateAccount`.
`MsgActivateAccount` is the normal first persistent state creation path; a
controlled migration is the only other allowed persistent write reason.
