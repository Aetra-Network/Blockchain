# Aetra Name System (ANS) — design

Native `.aet` naming, decided by the chain owner. Model: **native in-Go**, not `.atlx`.
The reserved system address `AETIdentityRoot` (`app/addressing/system_addresses.go:123`)
is THE `.aet` collection / factory. Domains are native `NameRecord`s in
`x/identity-root`, but the collection and each dns-item behave like message-driven
contracts with native get-methods. This is AEZ Phase 7's `x/identity-root`
graduation (`docs/architecture/aez.md:591-605`), extended with pricing, auctions,
treasury sweep, attach rules and a reputation gate.

Money is `naet`; 1 AET = 1e9 naet.

## Message-driven interface
A wallet sends the collection `MsgSendToNameCollection{Sender, Opcode, Comment, AmountNaet}`.
Opcodes: `TOPUP` (accept AET) and `REGISTER` (parse the label from `Comment`, e.g.
"daniil" → daniil.aet, plus enough AET). Conceptually an AEZ cross-zone message;
Core Zone is zone 0 today so a direct Msg is the correct first cut, routed through
the AEZ bus later. Reuse the AVM textComment/opcode convention
(`x/aetravm/async/types.go`, `MaxCommentBytes=512`).

## Pricing (start-of-auction price by label length; 3..67 chars)
MAINNET: 3→50000, 4→25000, 5→15000, 6→7500, 7→5000, 8→2500, 9+→1000 AET.
TESTNET / LOCALNET: the same table divided by 10 (3→5000, … 9+→100 AET).
Stored as decimal naet strings, parsed to `sdkmath.Int` (float-free / determinism).
The `/10` is a genesis-time choice keyed off the network (chain-id / a
testnet_genesis flag), NOT a runtime branch on chain-id in consensus code.

**Governance-adjustable.** The price table is a governance parameter: a
`MsgUpdatePriceTable` (or a config-voting proposal, `x/config-voting`) under the
governance authority can lower/raise every price — e.g. if the AET/USD rate
rises, a vote can cut prices so a domain stays affordable. This is the only path
that changes prices; there is no automatic oracle. Bounds validated so a vote
cannot set a zero or absurd price.

## REGISTER flow + non-negativity
`incoming` moves into the module account first. If `incoming < price`:
`feeKept = min(incoming, CollectionFee≈0.5 AET)`, refund the rest — collection net
`+feeKept ≥ 0`. If `incoming ≥ price`: taken & not expired → reject (refund minus
fee); free or expired → opens/enters an auction at `price` with `incoming` as the
opening bid. The module account only ever pays out coins received in the same
handler, so its balance is non-negative by construction. **This is the
collection-goes-negative trap; the min() rule is the guard.**

## Auction (block height, never wall clock)
Min raise +5% (`MinBidRaisePctBps=500`). EndBlocker closes auctions at
`DeadlineHeight <= height`: highest bid wins the `NameRecord`, losing bids refunded,
collection retains the winning proceeds. Expired domains are re-auctionable by
anyone.

**Two auction kinds with different durations:**
- **Issuance auction** (free/expired domain via REGISTER): fixed duration
  `AuctionDurationBlocks` — TESTNET/LOCALNET 12 (~1 min), MAINNET 1440 (~2 h) at 5 s.
- **Owner-listed auction** (`MsgStartAuction`, on a domain you own): the owner
  chooses the duration, **7 to 365 days**, and a **custom start price**. Validate the
  duration into blocks (7d=120960 … 365d=6307200 at 5 s) and reject outside [7,365]d.

**Owner fixed-price sale** (`MsgListForSale`): the owner sets any price they want;
a buyer paying it triggers the transfer (wrapper over existing `TransferName`,
`x/identity-root/keeper/keeper.go:206`).

**Implemented.** `MsgListForSale` / `MsgDelistName` / `MsgBuyListedName`
(`x/identity-root/types/listing.go`, `x/identity-root/keeper/listing.go`),
backed by a new per-record `Listing` collection (`ListingKeyPrefix=0x07`,
de-blobbed like `Auctions`/`Attachments`, see `keeper/persistence.go`). Only the
current owner can list an active domain at a positive price; a listing and an
open auction for the same name are mutually exclusive (`ListForSale` rejects
while an auction is open, `StartAuction` rejects while listed). Buying pays
the buyer -> module -> current owner via the same `moveIn`/`moveOut`
bank-custody helpers `collection.go` uses, atomically with the ownership
transfer, and resets the term to a fresh `RegistrationPeriod` (a purchase, not
a gift). `TransferName` and an auction grant (`grantAuctionName`) both clear
any open listing for the name they touch, so a live listing's seller always
matches the record's current owner (`IdentityRootState.Validate()` enforces
this cross-check at genesis/import boundaries). Query: `Listing` (get-method
for "is this name currently listed, and at what price").
Wire-complete: the three messages' `CustomGetSigners` entries are registered
in `app/keeperconfig/tx.go` (list/delist resolve to `owner`, buy resolves to
`buyer`), guarded by `TestIdentityRootListingMsgsDecodeAndResolveSigner` in
`app/identity_root_msg_wire_format_test.go`, the same wire-decode +
signer-resolution proof the other Phase A/B/C messages carry. CLI:
`l1d tx identityroot list-for-sale|delist-name|buy-listed-name`,
`l1d query identityroot listing`. SDK: `MsgListForSaleType` /
`MsgDelistNameType` / `MsgBuyListedNameType` in
`ecosystem/sdk/src/tx/proto/identityRoot.ts`.

## Treasury sweep
Once per `SweepIntervalBlocks` (17280 ≈ 1 day): if collection balance > 100 AET,
send everything above 100 AET to the treasury module account, always leaving 100
(2000 → 1900 swept). In the EndBlocker, keyed off block height.

## Collection balance = real bank module account
A live cosmos module account at `AETIdentityRoot` (register in
`reservedSystemModuleAccountSpecs`, add `moduleAccountPermissions["identityroot"]`,
flip `CanHoldFunds=true`, keep `CanReceiveUserFunds=false`). Funds enter only via
`MsgSendToNameCollection.Amount` → `SendCoinsFromAccountToModule`, never a raw bank
send (FINDING-017 stranding trap). Keeper gains a `BankKeeper` like `x/contracts`.

## Attach rules
`MsgAttachDomain{Owner, FQDN, Target}`: caller owns FQDN; classify `Target` with
`x/aez` `CanonicalEntityID` (system-first). **Allow only** a user contract or a
native_account. **Reject** system entities (covers the collection, pools
`AETNominatorPool`/`AETSingleNominatorPool`), a dns item (a name, not an address),
and — belt-and-suspenders — anything in `IsReservedSystemAddressText` or
`BlockedAddresses` (catches official-staking bonded pools). One domain per wallet
via a per-wallet index.

## Fee model (owner clarification — mostly already built)
The tx fee is NOT flat; it already floats with network load and charges storage
rent, and a domain'd wallet's reputation scales it:
- **Load-dependent:** `DynamicFeeAmount(base, max, target, utilization)`
  (`x/fees/types/fee_model.go:198`) rises with block utilization from the 0.4 AET
  base toward the 5 AET ceiling — the more congested, the higher. Already wired
  (`fee_policy.go:70`).
- **Storage rent on send:** `StorageRentDecorator` (`app/txhandlers/handlers.go:52`)
  + `account.StorageRentDebt` (`x/native-account/types/account.go:43`) charge a
  wallet rent for the data it stores, collected when it transacts. A bare wallet
  is deliberately CHEAP / lightweight (low base rent); rent is what makes an
  idle-but-stored account eventually pay.
- **Reputation multiplier, gated on a domain:** only a wallet that currently holds
  a domain (or a validator) gets a reputation-scaled fee. Excellent reputation
  cuts the fee hard (≈0.2×), poor reputation much less (≈0.8×) — approximate,
  exact bounds a governance param. The existing hook is ADDITIVE (a naet
  premium/discount cap, `fee_formula_params.go`); this multiplicative 0.2–0.8×
  model is a mechanism change to make in ANS Phase B, gated by domain ownership.
  A wallet with no domain pays the plain load+rent fee, no rep scaling.

## Reputation (gate on CURRENT domain ownership; no carry on sale)
Reputation lives on the native_account in `x/reputation`, never on `NameRecord`.
The fee link already exists and is wired (`x/fees` LowReputationPremium /
HighReputationDiscount → `GetIdentityReputationScore`). Gate it: the adapter
(`app/keeperwiring/native.go:44-51`) returns `found=false` (neutral, no
premium/discount) unless the account currently holds a domain OR is a validator.
`ComputeIdentityScore` already models age + stake + domain + uptime
(`x/reputation/.../identity_reputation.go:74-102`); add a live hook that seeds a
wallet's reputation from account age + official-liquid-staking activity when it
first acquires a domain. **Reputation-carry trap:** never add a reputation field to
`NameRecord`, never copy reputation in `TransferName` or on auction win — the buyer
keeps their own record.

## Registration period and renewal window
Renewal is allowed **only in the last 60 days** before expiry: `RenewName` rejects
unless `ExpiryHeight - height <= RenewalWindowBlocks` (60d = 1036800 blocks at 5 s)
— you cannot renew earlier. On renewal, extend from the current `ExpiryHeight` (not
from `height`) so an early-but-in-window renewal does not lose time.

**A PURCHASE resets the term; a gift does NOT** (owner-flagged security fix):
- **Purchase** — an issuance-auction win, an owner-listed-auction win, or a
  fixed-price sale — is paid for, so the domain gets a fresh **365-day** term
  (`RegistrationPeriodBlocks` = 6307200 blocks).
- **Plain transfer / gift** (`TransferName` with no payment) does NOT reset the
  term: the domain keeps its existing `ExpiryHeight`, and the new owner must renew
  it (in the 60-day window) like anyone else. Without this, gifting a domain to
  yourself would be free perpetual renewal — the vulnerability the owner caught.
The term reset therefore lives in the auction-close / sale-settlement path, NOT in
`TransferName`.

## Subdomains
`test.daniil.aet` via existing `CreateSubdomain` (`keeper.go:287`), exposed on the
new msg server.

## Liquid staking (x/nominator-pool, NOT ANS — tracked separately)
Two owner requirements land in `x/nominator-pool`, not identity-root:
- **Minimum liquid-staking deposit = 1000 AET.** Enforce in
  `DepositToStakingPool` / `depositToOfficialLiquidStakingLocked` against a
  `MinPoolDeposit` param (already exists as `DefaultMinPoolDeposit`; raise it).
- **Network-dependent unbonding:** TESTNET/LOCALNET withdraw after ~1 minute,
  MAINNET after ~1 week. This is the existing `x/staking` unbonding time, already
  a genesis flag (`--unbonding-time`); set the mainnet genesis to 7 days and keep
  the localnet/testnet genesis short.

## Graduation + wiring
De-blob `x/identity-root` to per-record KV (copy `x/contracts/keeper/persistence.go`
+ a one-way blob→per-record migration, bump ConsensusVersion), add msg/query
servers + EndBlocker, move it from `prototypeModules`/`PrototypeStoreKeys` to
`systemModules`/`SystemStoreKeys` (positionally paired). **Coordination: AEZ Phase 4
also edits `app/wiring/aetracore/modules.go` — land ANS graduation after Phase 4
merges.** `CorePinned("name")` is already true, golden vector `name/alice.aet=220`
must not change.

## Phasing
- **A** — issuance / auction / treasury (de-blob, module account, REGISTER/TOPUP/
  PlaceBid/StartAuction, pricing+refund, EndBlocker close+expiry+sweep, get-methods,
  graduation). End-to-end testable with no reputation/attach.
- **B** — attach / reputation / subdomain (forbidden-set, per-wallet index, live
  reputation hook + domain-ownership fee gate, ListForSale, subdomain msg).
- **C** — polish (initial-score derivation from stake amount/frequency/duration,
  decay wiring).

## Determinism risks
Auctions and sweeps key off block height, not time. Iterate auctions/bids over
sorted slices, Set/Delete in byte order, pricing in `sdkmath.Int` (no floats). No
wall-clock in consensus (determinism gate).
