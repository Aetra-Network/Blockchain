package keeper

import (
	"context"
	"errors"

	sdkmath "cosmossdk.io/math"

	"github.com/sovereign-l1/l1/x/identity-root/types"
)

// This file is the ANS Phase B owner fixed-price sale surface
// (docs/architecture/ans.md "Owner fixed-price sale"): MsgListForSale,
// MsgDelistName, and the buyer side MsgBuyListedName. A listing never escrows
// anything at list time -- money only moves inside BuyListedName, and moves
// exactly once, atomically with the ownership transfer, mirroring collection.go's
// moveIn/moveOut bank-custody pattern. Every state check for BuyListedName runs
// BEFORE any money moves, so a rejected buy never touches the bank and a bank
// failure (e.g. the buyer's insufficient balance) never leaves the module's
// record/listing state mutated -- the keeper method returns an error and
// persistLocked is never reached, so the whole message is a no-op on failure.

// BuyListingOutcome is the result of a MsgBuyListedName.
type BuyListingOutcome struct {
	Name		string
	Owner		string
	PriceNaet	uint64
	ExpiryHeight	uint64
}

// ListForSale lists a domain the caller owns for a fixed-price sale. Rejects a
// non-owner, an inactive (expired) domain, a non-positive price, and a name that
// already has an open auction -- a listing and an auction are mutually exclusive
// "for sale" states for the same name (see StartAuction's symmetric guard).
// Re-listing an already-listed name simply replaces the price (and both
// heights), matching upsertAuction's replace-wholesale convention.
func (k *Keeper) ListForSale(msg types.MsgListForSale) (types.Listing, error) {
	k.lockW()
	defer k.unlockW()
	if err := k.requireEnabled(); err != nil {
		return types.Listing{}, err
	}
	if msg.Height == 0 {
		return types.Listing{}, errors.New("identity list-for-sale height must be positive")
	}
	_, record, err := k.requireOwnedName(msg.Name, msg.Owner, msg.Height, true)
	if err != nil {
		return types.Listing{}, err
	}
	price := sdkmath.NewIntFromUint64(msg.PriceNaet)
	if !price.IsPositive() {
		return types.Listing{}, errors.New("identity listing price must be positive")
	}
	if _, _, found := auctionIndex(k.genesis.State.Auctions, record.Name); found {
		return types.Listing{}, errors.New("identity an auction is already open for this name")
	}
	listing := types.Listing{
		Name:		record.Name,
		Seller:		msg.Owner,
		PriceNaet:	price.String(),
		CreatedHeight:	msg.Height,
		UpdatedHeight:	msg.Height,
	}.Normalize(k.genesis.IdentityParams)
	next := cloneGenesisUnsorted(k.genesis)
	next.State.Listings = upsertListing(next.State.Listings, listing)
	// Incremental validation (FD-02): the auction-conflict guard above is
	// already enforced; only the listing's own field validity is scoped here.
	// The listing's Seller is set to msg.Owner, the SAME address requireOwnedName
	// just proved equals record.Owner, so the genesis-boundary seller/owner
	// cross-check in state.go's Validate() holds by construction and needs no
	// incremental reproduction.
	if err := validateGlobal(next); err != nil {
		return types.Listing{}, err
	}
	if err := listing.Validate(next.IdentityParams); err != nil {
		return types.Listing{}, err
	}
	if err := k.persistLocked(next); err != nil {
		return types.Listing{}, err
	}
	return listing, nil
}

// DelistName clears an owner's fixed-price listing without a sale. Active
// ownership is not required (mirrors DetachDomain) so the owner can clean up a
// listing on a domain that has since expired.
func (k *Keeper) DelistName(msg types.MsgDelistName) (types.Listing, error) {
	k.lockW()
	defer k.unlockW()
	if err := k.requireEnabled(); err != nil {
		return types.Listing{}, err
	}
	if msg.Height == 0 {
		return types.Listing{}, errors.New("identity delist height must be positive")
	}
	_, record, err := k.requireOwnedName(msg.Name, msg.Owner, msg.Height, false)
	if err != nil {
		return types.Listing{}, err
	}
	_, listing, found := listingIndex(k.genesis.State.Listings, record.Name)
	if !found {
		return types.Listing{}, errors.New("identity name has no listing to delist")
	}
	next := cloneGenesisUnsorted(k.genesis)
	next.State.Listings = removeListingByName(next.State.Listings, record.Name)
	// Removal-only mutation: cannot newly violate any invariant.
	if err := validateGlobal(next); err != nil {
		return types.Listing{}, err
	}
	if err := k.persistLocked(next); err != nil {
		return types.Listing{}, err
	}
	return listing, nil
}

// BuyListedName is the buyer side of MsgListForSale: paying the LIVE listing
// price atomically transfers the NameRecord to Buyer and the price from Buyer to
// the CURRENT record owner (re-derived fresh from the record, never trusted off
// the listing's Seller field), resetting the term to a fresh RegistrationPeriod
// -- a PURCHASE, exactly like an auction win (docs/architecture/ans.md "A
// PURCHASE resets the term; a gift does NOT"). The buyer pays exactly the
// listing's current price; there is no caller-supplied amount to reconcile.
//
// Every state/field check below runs BEFORE k.moveIn/k.moveOut touch the bank,
// so: (1) buying without a listing, (2) a non-positive/corrupt listing price,
// and (3) any invariant the purchase would break are all rejected with zero
// money movement. Only once every check has passed does payment move --
// buyer -> module -> seller -- immediately followed by persistLocked; if the
// buyer's balance is insufficient, moveIn fails and the message handler returns
// an error before persistLocked ever runs, so nothing commits.
func (k *Keeper) BuyListedName(msg types.MsgBuyListedName) (BuyListingOutcome, error) {
	k.lockW()
	defer k.unlockW()
	if err := k.requireEnabled(); err != nil {
		return BuyListingOutcome{}, err
	}
	if msg.Height == 0 {
		return BuyListingOutcome{}, errors.New("identity buy-listed-name height must be positive")
	}
	if err := types.ValidateUserFacingAEAddress("identity buy listed name buyer", msg.Buyer); err != nil {
		return BuyListingOutcome{}, err
	}
	params := k.genesis.IdentityParams
	name, err := types.NormalizeName(msg.Name, params.RootNamespace)
	if err != nil {
		return BuyListingOutcome{}, err
	}
	_, listing, found := listingIndex(k.genesis.State.Listings, name)
	if !found {
		return BuyListingOutcome{}, errors.New("identity no listing for that name")
	}
	price, err := listing.Price()
	if err != nil {
		return BuyListingOutcome{}, err
	}
	if !price.IsPositive() {
		return BuyListingOutcome{}, errors.New("identity listing price must be positive")
	}
	_, record, found := recordIndex(k.genesis.State.Records, name)
	if !found {
		return BuyListingOutcome{}, errors.New("identity listed name has no record")
	}
	if !types.IsActive(record, msg.Height) {
		return BuyListingOutcome{}, errors.New("identity listed name has expired")
	}
	if record.Owner == msg.Buyer {
		return BuyListingOutcome{}, errors.New("identity buyer already owns this name")
	}
	seller := record.Owner

	expiry, err := addHeight(msg.Height, params.RegistrationPeriod)
	if err != nil {
		return BuyListingOutcome{}, err
	}
	record.Owner = msg.Buyer
	record.ExpiryHeight = expiry
	record.RenewalHeight = msg.Height
	record.UpdatedHeight = msg.Height
	// A purchase settlement, not a passive expiry: any accrued rent debt is
	// cleared exactly like an auction grant clears it (grantAuctionName), since
	// the fresh term restarts storage accounting from this height.
	record.StorageRentDebt = 0
	record.LastStorageChargeHeight = msg.Height
	record.NFTBinding = types.IdentityNFTBindingReference{Name: record.Name}

	next := cloneGenesisUnsorted(k.genesis)
	idx, _, found := recordIndex(next.State.Records, name)
	if !found {
		return BuyListingOutcome{}, errors.New("identity name vanished during purchase")
	}
	next.State.Records[idx] = record.Normalize(next.IdentityParams)
	next.State.ReverseRecords = removeReverseByName(next.State.ReverseRecords, name)
	next.State.Attachments = removeAttachmentByName(next.State.Attachments, name)
	next.State.Listings = removeListingByName(next.State.Listings, name)

	updated := next.State.Records[idx]
	if err := validateGlobal(next); err != nil {
		return BuyListingOutcome{}, err
	}
	if err := updated.Validate(next.IdentityParams); err != nil {
		return BuyListingOutcome{}, err
	}
	if err := checkReservedOwnership(next, updated.Name); err != nil {
		return BuyListingOutcome{}, err
	}
	if err := transferPreservesSubdomainOwnershipPolicy(next, updated.Name); err != nil {
		return BuyListingOutcome{}, err
	}

	if err := k.moveIn(msg.Buyer, price); err != nil {
		return BuyListingOutcome{}, err
	}
	if err := k.moveOut(seller, price); err != nil {
		return BuyListingOutcome{}, err
	}
	if err := k.persistLocked(next); err != nil {
		return BuyListingOutcome{}, err
	}
	return BuyListingOutcome{
		Name:		updated.Name,
		Owner:		updated.Owner,
		PriceNaet:	uintFromInt(price),
		ExpiryHeight:	updated.ExpiryHeight,
	}, nil
}

// listingView is the query-server read accessor: reads a fresh committed-store
// snapshot via viewGenesis (see keeper.go), resolves the normalized name, and
// reports whether a listing currently exists for it. Mirrors auctionView's
// shape exactly -- both answer "is there an open X for this name" against the
// SAME committed state, so a freshly restarted or state-synced node's Listing
// query returns the same answer a continuously-running node's in-memory cache
// would (the FINDING-008 class viewGenesis exists to close).
func (k *Keeper) listingView(ctx context.Context, name string) (types.Listing, bool, error) {
	gs, err := k.viewGenesis(ctx)
	if err != nil {
		return types.Listing{}, false, err
	}
	if err := gs.Params.Validate(); err != nil {
		return types.Listing{}, false, err
	}
	normalized, err := types.NormalizeName(name, gs.IdentityParams.RootNamespace)
	if err != nil {
		return types.Listing{}, false, err
	}
	_, listing, found := listingIndex(gs.State.Listings, normalized)
	return listing, found, nil
}

// --- listing slice helpers (mirror auctionIndex/upsertAuction/removeAuction). ---

func listingIndex(listings []types.Listing, name string) (int, types.Listing, bool) {
	for i, listing := range listings {
		if listing.Name == name {
			return i, listing, true
		}
	}
	return -1, types.Listing{}, false
}

func upsertListing(listings []types.Listing, listing types.Listing) []types.Listing {
	out := append([]types.Listing(nil), listings...)
	if i, _, found := listingIndex(out, listing.Name); found {
		out[i] = listing
	} else {
		out = append(out, listing)
	}
	types.SortListings(out)
	return out
}

func removeListingByName(listings []types.Listing, name string) []types.Listing {
	out := make([]types.Listing, 0, len(listings))
	for _, listing := range listings {
		if listing.Name == name {
			continue
		}
		out = append(out, listing)
	}
	return out
}
