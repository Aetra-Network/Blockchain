package types

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	sdkmath "cosmossdk.io/math"
)

// ANS Phase A: the message-driven collection surface (pricing, auctions,
// treasury sweep). Every amount is naet (1 AET = 1e9 naet) carried as a decimal
// string and parsed to sdkmath.Int -- float-free, per the determinism gate.
// Every deadline/interval is a block height, never a wall clock.

const (
	// NaetPerAET is the naet denomination scale.
	NaetPerAET = uint64(1_000_000_000)

	// MinRegistrableLabelLen is the shortest label a REGISTER accepts. The
	// price table is keyed by label length starting here.
	MinRegistrableLabelLen = uint32(3)

	// CollectionFeeNaet is the non-refundable fee kept out of a rejected or
	// underfunded REGISTER (0.5 AET).
	CollectionFeeNaet = uint64(500_000_000)

	// RegistrationPeriodBlocks is a purchased term: 365 days at 5s.
	RegistrationPeriodBlocks = uint64(6_307_200)
	// RenewalWindowBlocks is the trailing renewal window: 60 days at 5s.
	RenewalWindowBlocks = uint64(1_036_800)

	// BlocksPerDay is 1 day at 5s.
	BlocksPerDay = uint64(17_280)

	// MainnetIssuanceAuctionDurationBlocks ~= 2h; TestnetIssuanceAuctionDurationBlocks ~= 1min.
	MainnetIssuanceAuctionDurationBlocks = uint64(1_440)
	TestnetIssuanceAuctionDurationBlocks = uint64(12)

	// MinBidRaisePctBps is the minimum bid raise over the standing high bid
	// (500 = +5%).
	MinBidRaisePctBps = uint64(500)
	// BidRaiseDenomBps is the basis-point denominator.
	BidRaiseDenomBps = uint64(10_000)

	// OwnerAuctionMinDays / OwnerAuctionMaxDays bound an owner-listed auction.
	OwnerAuctionMinDays = uint64(7)
	OwnerAuctionMaxDays = uint64(365)

	// SweepIntervalBlocks ~= 1 day; SweepFloorNaet = 100 AET always retained.
	SweepIntervalBlocks = uint64(17_280)
	SweepFloorNaet      = uint64(100_000_000_000)

	// DefaultTreasuryModuleName is the fee-collector treasury module account.
	DefaultTreasuryModuleName = "feecollector_treasury"

	AuctionKindIssuance    = "issuance"
	AuctionKindOwnerListed = "owner_listed"

	// CollectionDenom is the coin denomination the collection custodies.
	CollectionDenom = "naet"

	// Collection message opcodes (MsgSendToNameCollection.Opcode).
	OpcodeTopUp    = uint32(1)
	OpcodeRegister = uint32(2)

	// MaxCommentBytes bounds the collection message comment (AVM convention).
	MaxCommentBytes = 512
)

// PriceTier maps a minimum label length to its start-of-auction price in naet.
type PriceTier struct {
	MinLabelLen	uint32
	PriceNaet	string
}

// Auction is one open issuance or owner-listed auction. Losing bids are
// refunded the instant they are outbid, so only the current high bid is
// escrowed and there are no separate Bid records.
type Auction struct {
	// Name is the normalized FQDN and the per-record store key.
	Name		string
	// Kind is AuctionKindIssuance or AuctionKindOwnerListed.
	Kind		string
	// Seller is the listing owner for an owner-listed auction; "" for issuance.
	Seller		string
	// OpenPriceNaet is the opening / reserve price in naet.
	OpenPriceNaet	string
	// HighBidNaet is the standing high bid in naet ("0" when none yet).
	HighBidNaet	string
	// HighBidder is the AE address whose bid is currently escrowed ("" when none).
	HighBidder	string
	// DeadlineHeight closes the auction in the EndBlocker at height >= this.
	DeadlineHeight	uint64
	// CreatedHeight is the block the auction opened at.
	CreatedHeight	uint64
}

// SweepState carries the last treasury-sweep height so the daily sweep keys off
// block height alone.
type SweepState struct {
	LastSweepHeight uint64
}

// DefaultPriceTable is the mainnet table. A testnet / localnet genesis divides
// every price by 10 (a genesis choice, never a runtime chain-id branch).
func DefaultPriceTable() []PriceTier {
	return []PriceTier{
		{MinLabelLen: 3, PriceNaet: "50000000000000"}, // 50000 AET
		{MinLabelLen: 4, PriceNaet: "25000000000000"}, // 25000 AET
		{MinLabelLen: 5, PriceNaet: "15000000000000"}, // 15000 AET
		{MinLabelLen: 6, PriceNaet: "7500000000000"},  //  7500 AET
		{MinLabelLen: 7, PriceNaet: "5000000000000"},  //  5000 AET
		{MinLabelLen: 8, PriceNaet: "2500000000000"},  //  2500 AET
		{MinLabelLen: 9, PriceNaet: "1000000000000"},  //  1000 AET
	}
}

// ParseNaet parses a non-negative decimal naet string to sdkmath.Int.
func ParseNaet(value string) (sdkmath.Int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return sdkmath.ZeroInt(), nil
	}
	amount, ok := sdkmath.NewIntFromString(value)
	if !ok {
		return sdkmath.Int{}, fmt.Errorf("identity naet amount %q is not a valid integer", value)
	}
	if amount.IsNegative() {
		return sdkmath.Int{}, fmt.Errorf("identity naet amount %q must not be negative", value)
	}
	return amount, nil
}

// LabelLength returns the byte length of a normalized (lowercased, trimmed)
// label. Labels are ASCII (validateLabel), so bytes == characters.
func LabelLength(label string) int {
	return len(strings.ToLower(strings.TrimSpace(label)))
}

// PriceForLabel selects the price for a label by its length: the highest tier
// whose MinLabelLen does not exceed the label length. A label shorter than the
// smallest tier (or than MinLabelLen) has no price and is rejected.
func (p IdentityRootParams) PriceForLabel(label string) (sdkmath.Int, error) {
	n := uint32(LabelLength(label))
	if n < p.MinLabelLen {
		return sdkmath.Int{}, fmt.Errorf("identity label %q is shorter than the minimum %d", label, p.MinLabelLen)
	}
	tiers := append([]PriceTier(nil), p.PriceTable...)
	sort.SliceStable(tiers, func(i, j int) bool { return tiers[i].MinLabelLen < tiers[j].MinLabelLen })
	var (
		found bool
		price sdkmath.Int
	)
	for _, tier := range tiers {
		if tier.MinLabelLen > n {
			break
		}
		parsed, err := ParseNaet(tier.PriceNaet)
		if err != nil {
			return sdkmath.Int{}, err
		}
		price = parsed
		found = true
	}
	if !found {
		return sdkmath.Int{}, fmt.Errorf("identity label %q has no price tier", label)
	}
	return price, nil
}

// validateCollectionParams checks the Phase-A pricing / auction / sweep params.
func (p IdentityRootParams) validateCollectionParams() error {
	if p.MinLabelLen == 0 {
		return errors.New("identity minimum label length must be positive")
	}
	if p.CollectionFeeNaet == 0 {
		return errors.New("identity collection fee must be positive")
	}
	if p.RenewalWindowBlocks == 0 {
		return errors.New("identity renewal window must be positive")
	}
	if p.IssuanceAuctionDurationBlocks == 0 {
		return errors.New("identity issuance auction duration must be positive")
	}
	if p.MinBidRaisePctBps == 0 || p.MinBidRaisePctBps > BidRaiseDenomBps {
		return errors.New("identity minimum bid raise must be between 1 and 10000 bps")
	}
	if p.BlocksPerDay == 0 {
		return errors.New("identity blocks-per-day must be positive")
	}
	if p.OwnerAuctionMinDurationBlocks == 0 || p.OwnerAuctionMaxDurationBlocks < p.OwnerAuctionMinDurationBlocks {
		return errors.New("identity owner auction duration bounds are invalid")
	}
	if p.SweepIntervalBlocks == 0 {
		return errors.New("identity sweep interval must be positive")
	}
	if strings.TrimSpace(p.TreasuryModuleName) == "" {
		return errors.New("identity treasury module name is required")
	}
	if len(p.PriceTable) == 0 {
		return errors.New("identity price table must not be empty")
	}
	tiers := append([]PriceTier(nil), p.PriceTable...)
	sort.SliceStable(tiers, func(i, j int) bool { return tiers[i].MinLabelLen < tiers[j].MinLabelLen })
	if tiers[0].MinLabelLen > p.MinLabelLen {
		return errors.New("identity price table must cover the minimum label length")
	}
	prevLen := uint32(0)
	for i, tier := range tiers {
		if tier.MinLabelLen == 0 {
			return errors.New("identity price tier min label length must be positive")
		}
		if i > 0 && tier.MinLabelLen == prevLen {
			return errors.New("identity price table has a duplicate label length tier")
		}
		prevLen = tier.MinLabelLen
		price, err := ParseNaet(tier.PriceNaet)
		if err != nil {
			return err
		}
		if !price.IsPositive() {
			return errors.New("identity price tier price must be positive")
		}
	}
	return nil
}

// Normalize canonicalizes an auction's addresses, name and amounts.
func (a Auction) Normalize(params IdentityRootParams) Auction {
	a.Name, _ = NormalizeName(a.Name, params.RootNamespace)
	a.Kind = strings.TrimSpace(a.Kind)
	a.Seller = strings.TrimSpace(a.Seller)
	a.HighBidder = strings.TrimSpace(a.HighBidder)
	a.OpenPriceNaet = strings.TrimSpace(a.OpenPriceNaet)
	a.HighBidNaet = strings.TrimSpace(a.HighBidNaet)
	if a.HighBidNaet == "" {
		a.HighBidNaet = "0"
	}
	return a
}

// Validate checks an auction record's internal consistency.
func (a Auction) Validate(params IdentityRootParams) error {
	a = a.Normalize(params)
	if err := ValidateName(a.Name, params); err != nil {
		return err
	}
	switch a.Kind {
	case AuctionKindIssuance:
		if a.Seller != "" {
			return errors.New("identity issuance auction must have no seller")
		}
	case AuctionKindOwnerListed:
		if err := ValidateUserFacingAEAddress("identity auction seller", a.Seller); err != nil {
			return err
		}
	default:
		return fmt.Errorf("identity auction kind %q is invalid", a.Kind)
	}
	open, err := ParseNaet(a.OpenPriceNaet)
	if err != nil {
		return err
	}
	if !open.IsPositive() {
		return errors.New("identity auction open price must be positive")
	}
	high, err := ParseNaet(a.HighBidNaet)
	if err != nil {
		return err
	}
	if a.HighBidder != "" {
		if err := ValidateUserFacingAEAddress("identity auction high bidder", a.HighBidder); err != nil {
			return err
		}
		if !high.IsPositive() {
			return errors.New("identity auction high bid must be positive when a bidder is set")
		}
	} else if high.IsPositive() {
		return errors.New("identity auction high bid requires a bidder")
	}
	if a.DeadlineHeight == 0 || a.CreatedHeight == 0 {
		return errors.New("identity auction heights must be positive")
	}
	if a.DeadlineHeight <= a.CreatedHeight {
		return errors.New("identity auction deadline must follow its creation height")
	}
	return nil
}

// OpenPrice returns the parsed opening price.
func (a Auction) OpenPrice() (sdkmath.Int, error) { return ParseNaet(a.OpenPriceNaet) }

// HighBid returns the parsed standing high bid.
func (a Auction) HighBid() (sdkmath.Int, error) { return ParseNaet(a.HighBidNaet) }

// HasBid reports whether the auction currently holds an escrowed bid.
func (a Auction) HasBid() bool {
	return strings.TrimSpace(a.HighBidder) != ""
}

// SortAuctions orders auctions by name in byte order (deterministic iteration).
func SortAuctions(auctions []Auction) {
	sort.SliceStable(auctions, func(i, j int) bool { return auctions[i].Name < auctions[j].Name })
}

func cloneAuctions(auctions []Auction) []Auction {
	out := append([]Auction(nil), auctions...)
	for i := range out {
		out[i].Name = strings.ToLower(strings.TrimSpace(out[i].Name))
		out[i].Kind = strings.TrimSpace(out[i].Kind)
		out[i].Seller = strings.TrimSpace(out[i].Seller)
		out[i].HighBidder = strings.TrimSpace(out[i].HighBidder)
		out[i].OpenPriceNaet = strings.TrimSpace(out[i].OpenPriceNaet)
		out[i].HighBidNaet = strings.TrimSpace(out[i].HighBidNaet)
		if out[i].HighBidNaet == "" {
			out[i].HighBidNaet = "0"
		}
	}
	return out
}
