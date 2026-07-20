package types

import (
	"errors"
	"sort"
	"strings"

	sdkmath "cosmossdk.io/math"
)

// This file is the ANS Phase B owner fixed-price sale surface
// (docs/architecture/ans.md "Owner fixed-price sale"): the Listing record type,
// the three wire messages that drive it (MsgListForSale / MsgDelistName /
// MsgBuyListedName -- declared in tx.go alongside every other hand-rolled Msg in
// this tree), and their ValidateBasic sanity checks. The keeper logic lives in
// x/identity-root/keeper/listing.go, mirroring how attach.go pairs with the
// Attachment type declared in state.go.

// Listing is one open owner fixed-price sale: the current owner names a price;
// MsgBuyListedName by anyone paying it transfers the NameRecord atomically
// against payment -- a PURCHASE, so the term resets to a fresh 365-day period
// exactly like an auction win (docs/architecture/ans.md "Registration period and
// renewal window"). One listing per name; TransferName, an auction grant, and
// MsgDelistName all clear it (see keeper.TransferName, keeper.grantAuctionName,
// keeper.DelistName), so a live listing's Seller always equals its NameRecord's
// current Owner by construction.
type Listing struct {
	// Name is the normalized FQDN and the per-record store key.
	Name string
	// Seller is the owner who listed the name, captured at listing time.
	// BuyListedName always re-derives the actual payee from the LIVE NameRecord
	// owner rather than trusting this field, so a stale Seller can never redirect
	// payment; it exists for the query surface and the genesis cross-check only.
	Seller string
	// PriceNaet is the fixed buy-now price in naet, a positive decimal string
	// (the same ParseNaet convention as Auction.OpenPriceNaet/HighBidNaet).
	PriceNaet     string
	CreatedHeight uint64
	UpdatedHeight uint64
}

// Normalize canonicalizes a listing's name, seller and price string.
func (l Listing) Normalize(params IdentityRootParams) Listing {
	l.Name, _ = NormalizeName(l.Name, params.RootNamespace)
	l.Seller = strings.TrimSpace(l.Seller)
	l.PriceNaet = strings.TrimSpace(l.PriceNaet)
	return l
}

// Validate checks a listing record's internal consistency.
func (l Listing) Validate(params IdentityRootParams) error {
	l = l.Normalize(params)
	if err := ValidateName(l.Name, params); err != nil {
		return err
	}
	if err := ValidateUserFacingAEAddress("identity listing seller", l.Seller); err != nil {
		return err
	}
	price, err := ParseNaet(l.PriceNaet)
	if err != nil {
		return err
	}
	if !price.IsPositive() {
		return errors.New("identity listing price must be positive")
	}
	if l.CreatedHeight == 0 || l.UpdatedHeight == 0 {
		return errors.New("identity listing heights must be positive")
	}
	if l.UpdatedHeight < l.CreatedHeight {
		return errors.New("identity listing updated height cannot precede creation")
	}
	return nil
}

// Price returns the parsed listing price.
func (l Listing) Price() (sdkmath.Int, error) { return ParseNaet(l.PriceNaet) }

// SortListings orders listings by name in byte order (deterministic iteration).
func SortListings(listings []Listing) {
	sort.SliceStable(listings, func(i, j int) bool { return listings[i].Name < listings[j].Name })
}

func cloneListings(listings []Listing) []Listing {
	out := append([]Listing(nil), listings...)
	for i := range out {
		out[i].Name = strings.ToLower(strings.TrimSpace(out[i].Name))
		out[i].Seller = strings.TrimSpace(out[i].Seller)
		out[i].PriceNaet = strings.TrimSpace(out[i].PriceNaet)
	}
	return out
}

// QueryListing is a flattened, wire-friendly view of a Listing.
type QueryListing struct {
	Name          string `protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
	Seller        string `protobuf:"bytes,2,opt,name=seller,proto3" json:"seller,omitempty"`
	PriceNaet     string `protobuf:"bytes,3,opt,name=price_naet,proto3" json:"price_naet,omitempty"`
	CreatedHeight uint64 `protobuf:"varint,4,opt,name=created_height,proto3" json:"created_height,omitempty"`
	UpdatedHeight uint64 `protobuf:"varint,5,opt,name=updated_height,proto3" json:"updated_height,omitempty"`
}

// ListingView flattens a Listing into its query shape.
func ListingView(l Listing) QueryListing {
	return QueryListing{
		Name:          l.Name,
		Seller:        l.Seller,
		PriceNaet:     l.PriceNaet,
		CreatedHeight: l.CreatedHeight,
		UpdatedHeight: l.UpdatedHeight,
	}
}

// --- ValidateBasic: stateless sanity checks on the wire message itself, ahead
// of (and independent from) the keeper's stateful checks (ownership, active
// domain, auction conflicts, live listing existence). Every hand-rolled Msg in
// this tree relies on the keeper for its REAL validation (see tx.go's doc
// comment); these exist as the cheap first filter the sdk.HasValidateBasic
// interface expects, and as a direct unit-testable spec of each message's
// field-shape requirements. ---

// ValidateBasic sanity-checks MsgListForSale's fields.
func (msg *MsgListForSale) ValidateBasic() error {
	if err := ValidateUserFacingAEAddress("identity listing owner", msg.Owner); err != nil {
		return err
	}
	if strings.TrimSpace(msg.Name) == "" {
		return errors.New("identity listing name is required")
	}
	if msg.PriceNaet == 0 {
		return errors.New("identity listing price must be positive")
	}
	return nil
}

// ValidateBasic sanity-checks MsgDelistName's fields.
func (msg *MsgDelistName) ValidateBasic() error {
	if err := ValidateUserFacingAEAddress("identity delist owner", msg.Owner); err != nil {
		return err
	}
	if strings.TrimSpace(msg.Name) == "" {
		return errors.New("identity delist name is required")
	}
	return nil
}

// ValidateBasic sanity-checks MsgBuyListedName's fields. It deliberately carries
// no price/amount field to validate: the buyer always pays the LIVE listing
// price the keeper reads at execution time (see keeper.BuyListedName), not a
// caller-supplied amount, so there is no over/under-payment shape to check here.
func (msg *MsgBuyListedName) ValidateBasic() error {
	if err := ValidateUserFacingAEAddress("identity buy listed name buyer", msg.Buyer); err != nil {
		return err
	}
	if strings.TrimSpace(msg.Name) == "" {
		return errors.New("identity buy listed name requires a name")
	}
	return nil
}
