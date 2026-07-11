# Official Liquid Staking UX

Normal user staking in Aetra is pool/index based:

```text
User -> Liquid Staking Contract -> Pool Contract -> Validators
```

The wallet, CLI, and user-facing API should present `deposit to official liquid staking pool` as the default staking action. The user supplies an `AE...` account address, an official pool id or contract, and an AET amount. The user does not supply or choose a validator address on the normal staking path.

Behavior:

- deposits below the governance-configured pool minimum are rejected;
- accepted deposits mint deterministic pool shares or a receipt token amount;
- the receipt represents a claim on pool assets and rewards, not ownership of any validator;
- the pool aggregates user deposits and sends validator-sized allocations through official pool accounting rules;
- validator rewards and slashing exposure return to the pool and are applied by pool share;
- direct user delegation to validators is disabled in user-facing transaction paths; validator/operator and pool funding use governance-approved pool allocation and capability hooks, not normal user `MsgDelegate`.

Example:

```text
user deposits 100 AET into official pool
pool total becomes 10,000 AET
allocation engine assigns deterministic validator weights
official pool contract injects pooled stake to validators
user reward share = user_pool_shares / total_pool_shares
```

Addresses:

- user-facing account, pool, and validator addresses are always `AE...`;
- raw/internal addresses are `4:...`;

## Wallet staking writes: signing model

A wallet's deposit / unbond / claim messages
(`MsgDepositToStakingPool`, `MsgRequestPoolUnbond`, `MsgClaimPoolRewards`) are
**signed with, and carry in their address field, the wallet's PLAIN account
address** — the same `AE...` address a bank `MsgSend` uses as `from_address`,
i.e. the one a signature verifies against. The chain resolves the signer to
that address (`x/nominator-pool/types/signing.go`, registered in
`app/keeperconfig.CustomGetSigners`) and then, server-side in the msg server,
**normalizes it to the account's v2 identity**
(`addressing.NormalizeToAccountIdentity`, the same identity native-account
records activation under) before the activation check and all share-ownership
bookkeeping. So:

- **writes** use the plain address (standard signing);
- **reads** (`GET /staking/pools/{poolId}/{addr}`) use the account's v2 identity
  — that is what share ownership is recorded under. The wallet reads with
  `nativeIdentity.addressUser`.

Delegator shares and unbonding entries are keyed by the `4:` **raw** form of the
owner address (`types.RawAddressForUserAddress`); the explorer position endpoint
queries and filters by that raw form (do not query by the `AE` form or the
position comes back empty).

## Standing up the official pool (chain-ops runbook)

`DefaultGenesis` ships **zero** pools. Pool creation
(`MsgCreateOfficialLiquidStakingPool`) is gated on `Params.Authority`, which
defaults to the keyless system constant
`4:0000000000000000000000000000000000000000000000000000000000000001` — **no key
signs for it**, and gov's default voting period is 48h. So a network must pick
one of two paths.

### Path A — genesis injection (recommended for a new testnet/localnet)

Ship the official pool in the nominator-pool genesis. Deterministic, needs no
keys, no waiting on gov. After `init` (before first start), patch
`app_state["nominator-pool"].State` — add one `NominatorPool` (with
`OfficialLiquidStaking: true`, `Status: "active"`) and the matching
`LiquidStakingPool`. The pool references a contract address pair
(`ContractAddressUser` / `ContractAddressRaw`); on this branch deposits are pure
keeper accounting and do not call the contract, so a reserved/placeholder pair
is functional, but for a faithful setup use the `examples/avm/stake` pool
contract's deterministic address (deploy it post-launch at that address — see
Path B step 2 for the deploy commands). Example (Go field names; the genesis has
no json tags):

```jsonc
// app_state["nominator-pool"].State.Pools[]  and  .LiquidStakingPools[]
{ "PoolID": "official-liquid-staking",
  "ContractAddressUser": "AE...", "ContractAddressRaw": "4:...",
  "OfficialLiquidStaking": true, "PoolOperator": "4:...",
  "PoolCommissionBps": 100, "Status": "active" }
```

Then `scripts/localnet/start.ps1 -NoInit -Wait`. Validate the patched genesis by
booting once — `poolGenesis.Validate()` runs on import.

### Path B — runtime registration by an operator authority

For a running network where you control the authority key (or route via gov):

1. Set `app_state["nominator-pool"].Params.Authority` to your operator key's
   `AE...` address at genesis (localnet: node0's address). On mainnet this is the
   gov module account and step 3 is a gov proposal.
2. Deploy the stake pool contract:
   ```bash
   l1d avm store-code --bytecode-file <compiled stake_pool> --from node0 \
       --keyring-backend test --chain-id aetra-local-1 --broadcast   # -> code-id
   l1d avm deploy <code-id> --from node0 --body-file <init-data> \
       --keyring-backend test --chain-id aetra-local-1 --broadcast    # -> contract AE.../4:...
   ```
3. Submit `MsgCreateOfficialLiquidStakingPool` signed by the authority key
   (`Authority` = that key's `AE...`, `ContractAddressUser/Raw` = the deployed
   contract, `PoolOperator`, `PoolCommissionBps`). This message is now signable —
   its signer resolves to the `authority` field (Gap 1 fix). There is no
   dedicated `l1d tx nominator-pool` CLI yet, so submit it via a `tsx` spike
   mirroring `ecosystem/wallet/src/lib/chainTx.ts` (build the proto body, sign,
   `/tx/broadcast`), or a gov proposal on mainnet.

### Verifying a wallet round-trip

With a pool live and a wallet created + funded + activated:

1. **Deposit** — wallet `stake` page (or a tsx spike): builds a real
   `MsgDepositToStakingPool` with `wallet.addressUser` (plain), broadcasts.
2. **Confirm position** — `GET /staking/pools/{poolId}/{nativeIdentity}` returns
   non-zero `shares`.
3. **Unbond** — `requestUnbond` broadcasts `MsgRequestPoolUnbond`; the same GET
   now lists the entry under `unbonding`.

`MsgWithdrawPoolStake` stays chain-gated to the pool's own contract
(`keeper.go` `WithdrawPoolStake` requires `CallerContractUser ==
pool.ContractAddressUser`), so an EOA-signed withdraw is out of scope — the
wallet surfaces its honest "pending protocol support" state.

