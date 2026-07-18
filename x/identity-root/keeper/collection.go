package keeper

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sovereign-l1/l1/app/addressing"
	"github.com/sovereign-l1/l1/x/identity-root/types"
)

// This file is the ANS Phase A collection surface: the message-driven
// TOPUP/REGISTER entry point, ascending-bid auctions, and the treasury sweep
// EndBlocker. Every deadline/interval is a BLOCK HEIGHT; every amount is
// sdkmath.Int naet (no floats); every iteration over auctions is over a sorted
// slice, so the whole file is inside the determinism gate.
//
// Non-negativity of the module account is structural: every REGISTER refund is
// incoming - min(incoming, fee), always in [0, incoming]; every bid refund pays
// out an escrow the module already holds; the sweep sends at most
// balance - openEscrows - floor. No handler ever pays out more than it holds.

// CollectionOutcome is the result of a MsgSendToNameCollection.
type CollectionOutcome struct {
	Outcome		string
	Name		string
	RefundNaet	uint64
	FeeKeptNaet	uint64
	AuctionOpened	bool
	DeadlineHeight	uint64
}

// BidOutcome is the result of a MsgPlaceBid.
type BidOutcome struct {
	Name			string
	HighBidNaet		uint64
	HighBidder		string
	RefundedPreviousNaet	uint64
	DeadlineHeight		uint64
}

// StartAuctionOutcome is the result of a MsgStartAuction.
type StartAuctionOutcome struct {
	Name		string
	DeadlineHeight	uint64
}

// SendToNameCollection is the message-driven collection entry point. TOPUP adds
// AmountNaet to the collection module account; REGISTER parses the label from
// Comment and opens an issuance auction, refunds (minus a fee) when underfunded
// or when the name is taken/reserved.
func (k *Keeper) SendToNameCollection(msg types.MsgSendToNameCollection) (CollectionOutcome, error) {
	k.lockW()
	defer k.unlockW()
	if err := k.requireEnabled(); err != nil {
		return CollectionOutcome{}, err
	}
	if msg.Height == 0 {
		return CollectionOutcome{}, errors.New("identity collection message height must be positive")
	}
	if err := types.ValidateUserFacingAEAddress("identity collection sender", msg.Sender); err != nil {
		return CollectionOutcome{}, err
	}
	if len(msg.Comment) > types.MaxCommentBytes {
		return CollectionOutcome{}, errors.New("identity collection comment exceeds max bytes")
	}
	amount := sdkmath.NewIntFromUint64(msg.AmountNaet)
	switch msg.Opcode {
	case types.OpcodeTopUp:
		if !amount.IsPositive() {
			return CollectionOutcome{}, errors.New("identity collection topup requires a positive amount")
		}
		if err := k.moveIn(msg.Sender, amount); err != nil {
			return CollectionOutcome{}, err
		}
		return CollectionOutcome{Outcome: "topped_up"}, nil
	case types.OpcodeRegister:
		return k.registerViaCollectionLocked(msg, amount)
	default:
		return CollectionOutcome{}, fmt.Errorf("identity collection opcode %d is not supported", msg.Opcode)
	}
}

func (k *Keeper) registerViaCollectionLocked(msg types.MsgSendToNameCollection, incoming sdkmath.Int) (CollectionOutcome, error) {
	params := k.genesis.IdentityParams
	root, err := types.NormalizeRootNamespace(params.RootNamespace)
	if err != nil {
		return CollectionOutcome{}, err
	}
	label := strings.ToLower(strings.TrimSpace(msg.Comment))
	if label == "" {
		return CollectionOutcome{}, errors.New("identity collection register requires a label in the comment")
	}
	fqdn, err := types.NormalizeName(label, params.RootNamespace)
	if err != nil {
		return CollectionOutcome{}, err
	}
	if fqdn == root {
		return CollectionOutcome{}, errors.New("identity root namespace cannot be registered")
	}
	parent, err := types.ParentName(fqdn, params.RootNamespace)
	if err != nil {
		return CollectionOutcome{}, err
	}
	if parent != "" {
		return CollectionOutcome{}, errors.New("identity collection register only creates top-level .aet names")
	}
	if err := types.ValidateName(fqdn, params); err != nil {
		return CollectionOutcome{}, err
	}
	labelOnly := strings.Split(fqdn, ".")[0]
	price, err := params.PriceForLabel(labelOnly)
	if err != nil {
		return CollectionOutcome{}, err
	}
	// Incoming moves into the module account first; every refund below is
	// bounded by it, so the module account can never go negative.
	if err := k.moveIn(msg.Sender, incoming); err != nil {
		return CollectionOutcome{}, err
	}
	fee := sdkmath.NewIntFromUint64(params.CollectionFeeNaet)

	if incoming.LT(price) {
		return k.rejectWithFeeLocked(msg.Sender, incoming, fee, "underfunded_refunded", fqdn)
	}
	if isReserved(k.genesis.State.ReservedNames, fqdn) && !isRootAuthority(k.genesis.State.RootAuthorities, msg.Sender) {
		return k.rejectWithFeeLocked(msg.Sender, incoming, fee, "rejected_reserved", fqdn)
	}
	if _, record, found := recordIndex(k.genesis.State.Records, fqdn); found && types.IsActive(record, msg.Height) {
		return k.rejectWithFeeLocked(msg.Sender, incoming, fee, "rejected_taken", fqdn)
	}
	if _, _, found := auctionIndex(k.genesis.State.Auctions, fqdn); found {
		return k.rejectWithFeeLocked(msg.Sender, incoming, fee, "auction_already_open", fqdn)
	}

	deadline, err := addHeight(msg.Height, params.IssuanceAuctionDurationBlocks)
	if err != nil {
		return CollectionOutcome{}, err
	}
	auction := types.Auction{
		Name:		fqdn,
		Kind:		types.AuctionKindIssuance,
		Seller:		"",
		OpenPriceNaet:	price.String(),
		HighBidNaet:	incoming.String(),
		HighBidder:	msg.Sender,
		DeadlineHeight:	deadline,
		CreatedHeight:	msg.Height,
	}
	next := cloneGenesis(k.genesis)
	next.State.Auctions = upsertAuction(next.State.Auctions, auction)
	next.State = next.State.Export()
	if err := next.Validate(); err != nil {
		return CollectionOutcome{}, err
	}
	if err := k.persistLocked(next); err != nil {
		return CollectionOutcome{}, err
	}
	return CollectionOutcome{Outcome: "auction_opened", Name: fqdn, AuctionOpened: true, DeadlineHeight: deadline}, nil
}

// rejectWithFeeLocked keeps min(incoming, fee) and refunds the rest. No genesis
// change (the money move is via the bank), so it does not persist.
func (k *Keeper) rejectWithFeeLocked(sender string, incoming, fee sdkmath.Int, outcome, name string) (CollectionOutcome, error) {
	feeKept := fee
	if incoming.LT(fee) {
		feeKept = incoming
	}
	refund := incoming.Sub(feeKept) // always in [0, incoming]
	if err := k.moveOut(sender, refund); err != nil {
		return CollectionOutcome{}, err
	}
	return CollectionOutcome{
		Outcome:	outcome,
		Name:		name,
		RefundNaet:	uintFromInt(refund),
		FeeKeptNaet:	uintFromInt(feeKept),
	}, nil
}

// PlaceBid escrows a new bid, refunds the previous high bidder, and records the
// new high bid. A bid below the standing high plus the minimum raise (or below
// the opening price when there is no bid yet) is rejected.
func (k *Keeper) PlaceBid(msg types.MsgPlaceBid) (BidOutcome, error) {
	k.lockW()
	defer k.unlockW()
	if err := k.requireEnabled(); err != nil {
		return BidOutcome{}, err
	}
	if msg.Height == 0 {
		return BidOutcome{}, errors.New("identity bid height must be positive")
	}
	if err := types.ValidateUserFacingAEAddress("identity bid bidder", msg.Bidder); err != nil {
		return BidOutcome{}, err
	}
	params := k.genesis.IdentityParams
	name, err := types.NormalizeName(msg.Name, params.RootNamespace)
	if err != nil {
		return BidOutcome{}, err
	}
	_, auction, found := auctionIndex(k.genesis.State.Auctions, name)
	if !found {
		return BidOutcome{}, errors.New("identity no open auction for that name")
	}
	if msg.Height >= auction.DeadlineHeight {
		return BidOutcome{}, errors.New("identity auction is closed for bidding")
	}
	bid := sdkmath.NewIntFromUint64(msg.AmountNaet)
	if !bid.IsPositive() {
		return BidOutcome{}, errors.New("identity bid must be positive")
	}
	highBid, err := auction.HighBid()
	if err != nil {
		return BidOutcome{}, err
	}
	openPrice, err := auction.OpenPrice()
	if err != nil {
		return BidOutcome{}, err
	}
	minBid := openPrice
	if auction.HasBid() {
		raise := highBid.Mul(sdkmath.NewIntFromUint64(params.MinBidRaisePctBps)).Quo(sdkmath.NewIntFromUint64(types.BidRaiseDenomBps))
		if !raise.IsPositive() {
			raise = sdkmath.OneInt()
		}
		minBid = highBid.Add(raise)
	}
	if bid.LT(minBid) {
		return BidOutcome{}, fmt.Errorf("identity bid %s is below the minimum acceptable bid %s", bid.String(), minBid.String())
	}

	// Escrow the new bid first, then refund the previous high bidder out of the
	// escrow the module already holds -- so the refund is always covered.
	if err := k.moveIn(msg.Bidder, bid); err != nil {
		return BidOutcome{}, err
	}
	refundedPrev := sdkmath.ZeroInt()
	if auction.HasBid() {
		if err := k.moveOut(auction.HighBidder, highBid); err != nil {
			return BidOutcome{}, err
		}
		refundedPrev = highBid
	}
	auction.HighBidNaet = bid.String()
	auction.HighBidder = msg.Bidder

	next := cloneGenesis(k.genesis)
	next.State.Auctions = upsertAuction(next.State.Auctions, auction)
	next.State = next.State.Export()
	if err := next.Validate(); err != nil {
		return BidOutcome{}, err
	}
	if err := k.persistLocked(next); err != nil {
		return BidOutcome{}, err
	}
	return BidOutcome{
		Name:			name,
		HighBidNaet:		msg.AmountNaet,
		HighBidder:		msg.Bidder,
		RefundedPreviousNaet:	uintFromInt(refundedPrev),
		DeadlineHeight:		auction.DeadlineHeight,
	}, nil
}

// StartAuction lists a domain the caller owns for an owner-listed auction of
// 7..365 days at a custom start price. No money moves at start.
func (k *Keeper) StartAuction(msg types.MsgStartAuction) (StartAuctionOutcome, error) {
	k.lockW()
	defer k.unlockW()
	if err := k.requireEnabled(); err != nil {
		return StartAuctionOutcome{}, err
	}
	if msg.Height == 0 {
		return StartAuctionOutcome{}, errors.New("identity start-auction height must be positive")
	}
	_, record, err := k.requireOwnedName(msg.Name, msg.Owner, msg.Height, true)
	if err != nil {
		return StartAuctionOutcome{}, err
	}
	params := k.genesis.IdentityParams
	days := uint64(msg.DurationDays)
	if days < types.OwnerAuctionMinDays || days > types.OwnerAuctionMaxDays {
		return StartAuctionOutcome{}, fmt.Errorf("identity owner auction duration must be between %d and %d days", types.OwnerAuctionMinDays, types.OwnerAuctionMaxDays)
	}
	startPrice := sdkmath.NewIntFromUint64(msg.StartPriceNaet)
	if !startPrice.IsPositive() {
		return StartAuctionOutcome{}, errors.New("identity owner auction start price must be positive")
	}
	if _, _, found := auctionIndex(k.genesis.State.Auctions, record.Name); found {
		return StartAuctionOutcome{}, errors.New("identity an auction is already open for this name")
	}
	deadline, err := addHeight(msg.Height, days*params.BlocksPerDay)
	if err != nil {
		return StartAuctionOutcome{}, err
	}
	auction := types.Auction{
		Name:		record.Name,
		Kind:		types.AuctionKindOwnerListed,
		Seller:		msg.Owner,
		OpenPriceNaet:	startPrice.String(),
		HighBidNaet:	"0",
		HighBidder:	"",
		DeadlineHeight:	deadline,
		CreatedHeight:	msg.Height,
	}
	next := cloneGenesis(k.genesis)
	next.State.Auctions = upsertAuction(next.State.Auctions, auction)
	next.State = next.State.Export()
	if err := next.Validate(); err != nil {
		return StartAuctionOutcome{}, err
	}
	if err := k.persistLocked(next); err != nil {
		return StartAuctionOutcome{}, err
	}
	return StartAuctionOutcome{Name: record.Name, DeadlineHeight: deadline}, nil
}

// UpdatePriceTable replaces the governance-owned price table.
func (k *Keeper) UpdatePriceTable(msg types.MsgUpdatePriceTable) (int, error) {
	k.lockW()
	defer k.unlockW()
	if err := k.requireAuthority(msg.Authority); err != nil {
		return 0, err
	}
	tiers := types.PriceTiersFromMsg(&msg)
	next := cloneGenesis(k.genesis)
	next.IdentityParams.PriceTable = tiers
	if err := next.IdentityParams.Validate(); err != nil {
		return 0, err
	}
	if err := next.Validate(); err != nil {
		return 0, err
	}
	if err := k.persistLocked(next); err != nil {
		return 0, err
	}
	return len(tiers), nil
}

// EndBlocker closes due auctions (sorted by (DeadlineHeight, Name)), leaves
// expired names to stop resolving on their own, and runs the daily treasury
// sweep -- all keyed off block height.
func (k *Keeper) EndBlocker(ctx context.Context) error {
	if err := k.loadForBlock(ctx); err != nil {
		return err
	}
	height := uint64(sdk.UnwrapSDKContext(ctx).BlockHeight())
	k.lockW()
	defer k.unlockW()
	return k.runEndBlockLocked(height)
}

func (k *Keeper) runEndBlockLocked(height uint64) error {
	if err := k.genesis.Params.RequireEnabled(); err != nil {
		// A disabled collection is inert -- nothing to close or sweep.
		return nil
	}
	next := cloneGenesis(k.genesis)
	changed := false

	for _, auction := range dueAuctions(next.State.Auctions, height) {
		if err := k.closeAuctionLocked(&next, auction, height); err != nil {
			return err
		}
		next.State.Auctions = removeAuction(next.State.Auctions, auction.Name)
		changed = true
	}

	swept, err := k.sweepLocked(&next, height)
	if err != nil {
		return err
	}
	if swept {
		changed = true
	}

	if !changed {
		return nil
	}
	next.State = next.State.Export()
	if err := next.Validate(); err != nil {
		return err
	}
	return k.persistLocked(next)
}

// closeAuctionLocked settles one due auction: the highest bidder gets the
// NameRecord with a fresh term (a purchase resets the term); issuance proceeds
// stay retained in the module, owner-listed proceeds are paid to the seller. A
// no-bid auction grants nothing and moves no money.
func (k *Keeper) closeAuctionLocked(next *GenesisState, auction types.Auction, height uint64) error {
	if !auction.HasBid() {
		return nil
	}
	if err := grantAuctionName(next, auction, height); err != nil {
		return err
	}
	if auction.Kind == types.AuctionKindOwnerListed {
		highBid, err := auction.HighBid()
		if err != nil {
			return err
		}
		if err := k.moveOut(auction.Seller, highBid); err != nil {
			return err
		}
	}
	return nil
}

// grantAuctionName grants (or re-grants) the auctioned name to the winner with a
// fresh RegistrationPeriod term.
func grantAuctionName(next *GenesisState, auction types.Auction, height uint64) error {
	params := next.IdentityParams
	expiry, err := addHeight(height, params.RegistrationPeriod)
	if err != nil {
		return err
	}
	// An auction RE-GRANTS the name to a new owner. Any attachment left dangling
	// by the prior owner (passive expiry never clears the attachment record, only
	// the expiry-aware fee gate ignores it) MUST be cleared here -- exactly as
	// TransferName does on a sale -- or the reputation fee gate revives off the new
	// owner's freshly-acquired, now-active name for a wallet unrelated to them.
	// Idempotent, so it also covers the fresh-grant branch below where a record
	// was swept but its attachment lingered. Audit: reputation is gated on LIVE
	// ownership and never carried across an ownership change.
	next.State.Attachments = removeAttachmentByName(next.State.Attachments, auction.Name)
	if idx, record, found := recordIndex(next.State.Records, auction.Name); found {
		record.Owner = auction.HighBidder
		record.ExpiryHeight = expiry
		record.RenewalHeight = height
		record.UpdatedHeight = height
		record.StorageRentDebt = 0
		record.LastStorageChargeHeight = height
		record.NFTBinding = types.IdentityNFTBindingReference{Name: record.Name}
		next.State.Records[idx] = record.Normalize(params)
		next.State.ReverseRecords = removeReverseByName(next.State.ReverseRecords, record.Name)
		return nil
	}
	parent, err := types.ParentName(auction.Name, params.RootNamespace)
	if err != nil {
		return err
	}
	record := types.NameRecord{
		Name:				auction.Name,
		ParentName:			parent,
		Owner:				auction.HighBidder,
		ResolverRoot:			types.DefaultResolverRoot,
		ExpiryHeight:			expiry,
		RenewalHeight:			height,
		SubdomainPolicy:		types.SubdomainPolicyOwnerOnly,
		NFTBinding:			types.IdentityNFTBindingReference{Name: auction.Name},
		LastStorageChargeHeight:	height,
		RentPayerPolicy:		nextDefaultRentPayerPolicy(params),
		CreatedHeight:			height,
		UpdatedHeight:			height,
	}.Normalize(params)
	next.State.Records = append(next.State.Records, record)
	return nil
}

// sweepLocked runs the treasury sweep at most once per SweepIntervalBlocks. It
// sends balance - openEscrows - floor to the treasury module account, so live
// auction escrows are never swept and the floor always remains. Returns whether
// SweepState changed.
func (k *Keeper) sweepLocked(next *GenesisState, height uint64) (bool, error) {
	if !k.hasCustody() {
		return false, nil
	}
	params := next.IdentityParams
	last := next.State.SweepState.LastSweepHeight
	nextEligible, err := addHeight(last, params.SweepIntervalBlocks)
	if err != nil {
		return false, err
	}
	if height < nextEligible {
		return false, nil
	}
	balance := k.bankKeeper.SpendableCoins(k.runtimeCtx, CollectionModuleAddress()).AmountOf(types.CollectionDenom)
	escrow := openEscrowTotal(next.State.Auctions)
	floor := sdkmath.NewIntFromUint64(params.SweepFloorNaet)
	sweepable := balance.Sub(escrow).Sub(floor)
	next.State.SweepState.LastSweepHeight = height
	if !sweepable.IsPositive() {
		// Interval elapsed but nothing to sweep; the height bump still counts as
		// a change so the next sweep is a full interval away, not next block.
		return true, nil
	}
	if err := k.bankKeeper.SendCoinsFromModuleToModule(k.runtimeCtx, types.ModuleName, params.TreasuryModuleName, collectionCoins(sweepable)); err != nil {
		return false, err
	}
	return true, nil
}

// --- money helpers (no-ops for a ledger-only keeper without a bank) ---

func (k *Keeper) moveIn(sender string, amount sdkmath.Int) error {
	if !k.hasCustody() || !amount.IsPositive() {
		return nil
	}
	addr, err := collectionAccAddress("identity collection sender", sender)
	if err != nil {
		return err
	}
	return k.bankKeeper.SendCoinsFromAccountToModule(k.runtimeCtx, addr, types.ModuleName, collectionCoins(amount))
}

func (k *Keeper) moveOut(recipient string, amount sdkmath.Int) error {
	if !k.hasCustody() || !amount.IsPositive() {
		return nil
	}
	addr, err := collectionAccAddress("identity collection recipient", recipient)
	if err != nil {
		return err
	}
	return k.bankKeeper.SendCoinsFromModuleToAccount(k.runtimeCtx, types.ModuleName, addr, collectionCoins(amount))
}

func collectionCoins(amount sdkmath.Int) sdk.Coins {
	return sdk.NewCoins(sdk.NewCoin(types.CollectionDenom, amount))
}

func collectionAccAddress(field, text string) (sdk.AccAddress, error) {
	bz, err := addressing.Parse(text)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", field, err)
	}
	return sdk.AccAddress(bz), nil
}

func uintFromInt(value sdkmath.Int) uint64 {
	if value.IsNil() || !value.IsUint64() {
		return 0
	}
	return value.Uint64()
}

// --- auction slice helpers ---

func auctionIndex(auctions []types.Auction, name string) (int, types.Auction, bool) {
	for i, auction := range auctions {
		if auction.Name == name {
			return i, auction, true
		}
	}
	return -1, types.Auction{}, false
}

func upsertAuction(auctions []types.Auction, auction types.Auction) []types.Auction {
	out := append([]types.Auction(nil), auctions...)
	if i, _, found := auctionIndex(out, auction.Name); found {
		out[i] = auction
	} else {
		out = append(out, auction)
	}
	types.SortAuctions(out)
	return out
}

func removeAuction(auctions []types.Auction, name string) []types.Auction {
	out := make([]types.Auction, 0, len(auctions))
	for _, auction := range auctions {
		if auction.Name == name {
			continue
		}
		out = append(out, auction)
	}
	return out
}

func dueAuctions(auctions []types.Auction, height uint64) []types.Auction {
	var due []types.Auction
	for _, auction := range auctions {
		if auction.DeadlineHeight <= height {
			due = append(due, auction)
		}
	}
	sort.SliceStable(due, func(i, j int) bool {
		if due[i].DeadlineHeight != due[j].DeadlineHeight {
			return due[i].DeadlineHeight < due[j].DeadlineHeight
		}
		return due[i].Name < due[j].Name
	})
	return due
}

func openEscrowTotal(auctions []types.Auction) sdkmath.Int {
	total := sdkmath.ZeroInt()
	for _, auction := range auctions {
		if !auction.HasBid() {
			continue
		}
		high, err := auction.HighBid()
		if err != nil {
			continue
		}
		total = total.Add(high)
	}
	return total
}
