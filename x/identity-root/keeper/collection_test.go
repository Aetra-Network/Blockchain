package keeper

import (
	"context"
	"errors"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/app/addressing"
	"github.com/sovereign-l1/l1/x/identity-root/types"
	"github.com/sovereign-l1/l1/x/internal/prototype"
)

// mockBank is an in-memory bank ledger. It ENFORCES non-negativity by erroring
// on any transfer that would overdraw an account -- so if a handler ever tried
// to pay out more than the module holds, these tests fail immediately.
type mockBank struct {
	balances map[string]sdkmath.Int
}

func newMockBank() *mockBank { return &mockBank{balances: map[string]sdkmath.Int{}} }

func (b *mockBank) get(addr string) sdkmath.Int {
	if v, ok := b.balances[addr]; ok {
		return v
	}
	return sdkmath.ZeroInt()
}

func (b *mockBank) set(addr string, v sdkmath.Int) { b.balances[addr] = v }

func (b *mockBank) move(from, to string, amount sdkmath.Int) error {
	if b.get(from).LT(amount) {
		return errors.New("mock bank: insufficient balance (would go negative)")
	}
	b.set(from, b.get(from).Sub(amount))
	b.set(to, b.get(to).Add(amount))
	return nil
}

func (b *mockBank) SendCoinsFromAccountToModule(_ context.Context, sender sdk.AccAddress, module string, amt sdk.Coins) error {
	return b.move(sender.String(), authtypes.NewModuleAddress(module).String(), amt.AmountOf(types.CollectionDenom))
}

func (b *mockBank) SendCoinsFromModuleToAccount(_ context.Context, module string, recipient sdk.AccAddress, amt sdk.Coins) error {
	return b.move(authtypes.NewModuleAddress(module).String(), recipient.String(), amt.AmountOf(types.CollectionDenom))
}

func (b *mockBank) SendCoinsFromModuleToModule(_ context.Context, sender, recipient string, amt sdk.Coins) error {
	return b.move(authtypes.NewModuleAddress(sender).String(), authtypes.NewModuleAddress(recipient).String(), amt.AmountOf(types.CollectionDenom))
}

func (b *mockBank) SpendableCoins(_ context.Context, addr sdk.AccAddress) sdk.Coins {
	return sdk.NewCoins(sdk.NewCoin(types.CollectionDenom, b.get(addr.String())))
}

var _ BankKeeper = (*mockBank)(nil)

func accAddr(t *testing.T, aeText string) sdk.AccAddress {
	t.Helper()
	bz, err := addressing.Parse(aeText)
	require.NoError(t, err)
	return sdk.AccAddress(bz)
}

func moduleBalance(bank *mockBank) sdkmath.Int {
	return bank.get(CollectionModuleAddress().String())
}

func treasuryBalance(bank *mockBank) sdkmath.Int {
	return bank.get(authtypes.NewModuleAddress(types.DefaultTreasuryModuleName).String())
}

// setupCollectionKeeper builds an enabled keeper wired to a mock bank, with
// small test params (tiny prices, short auctions/intervals) so the money and
// height arithmetic is easy to read.
func setupCollectionKeeper(t *testing.T) (*Keeper, *mockBank) {
	t.Helper()
	bank := newMockBank()
	kv := NewKeeper().WithBankKeeper(bank)
	k := &kv
	gs := DefaultGenesis()
	gs.Params = prototype.TestnetParams()
	gs.IdentityParams.RegistrationPeriod = 100
	gs.IdentityParams.RenewalPeriod = 100
	gs.IdentityParams.RenewalWindowBlocks = 200
	gs.IdentityParams.IssuanceAuctionDurationBlocks = 10
	gs.IdentityParams.MinBidRaisePctBps = 500
	gs.IdentityParams.BlocksPerDay = 10
	gs.IdentityParams.OwnerAuctionMinDurationBlocks = 70
	gs.IdentityParams.OwnerAuctionMaxDurationBlocks = 3650
	gs.IdentityParams.SweepIntervalBlocks = 10
	gs.IdentityParams.SweepFloorNaet = 100_000_000_000 // 100 AET
	gs.IdentityParams.CollectionFeeNaet = 50
	gs.IdentityParams.MinLabelLen = 3
	gs.IdentityParams.PriceTable = []types.PriceTier{
		{MinLabelLen: 3, PriceNaet: "5000"},
		{MinLabelLen: 9, PriceNaet: "1000"},
	}
	require.NoError(t, k.InitGenesis(gs))
	k.runtimeCtx = context.Background()
	return k, bank
}

func fund(bank *mockBank, addr sdk.AccAddress, naet uint64) {
	bank.set(addr.String(), sdkmath.NewIntFromUint64(naet))
}

// runEndBlock drives the EndBlocker logic at an explicit height (no sdk.Context
// needed for the unit test -- the collection reads height, not wall clock).
func runEndBlock(t *testing.T, k *Keeper, height uint64) {
	t.Helper()
	k.lockW()
	k.runtimeCtx = context.Background()
	err := k.runEndBlockLocked(height)
	k.unlockW()
	require.NoError(t, err)
}

func TestPriceLookupByLabelLength(t *testing.T) {
	params := types.DefaultIdentityRootParams()
	// Mainnet defaults: 3-char is the most expensive, 9+ the cheapest.
	p3, err := params.PriceForLabel("abc")
	require.NoError(t, err)
	p9, err := params.PriceForLabel("abcdefghi")
	require.NoError(t, err)
	require.True(t, p3.GT(p9))
	require.Equal(t, "50000000000000", p3.String())
	require.Equal(t, "1000000000000", p9.String())
	// A 4-char label uses the 4-tier, not the 3-tier.
	p4, err := params.PriceForLabel("abcd")
	require.NoError(t, err)
	require.Equal(t, "25000000000000", p4.String())
	// Too short -> no price.
	_, err = params.PriceForLabel("ab")
	require.Error(t, err)
}

func TestRegisterRejectsBadLabelLength(t *testing.T) {
	k, bank := setupCollectionKeeper(t)
	sender := accAddr(t, ownerA)
	fund(bank, sender, 1_000_000)

	// Too short (2 chars) -> rejected before any money moves.
	_, err := k.SendToNameCollection(types.MsgSendToNameCollection{Sender: ownerA, Opcode: types.OpcodeRegister, Comment: "ab", AmountNaet: 5000, Height: 1})
	require.ErrorContains(t, err, "shorter than the minimum")
	require.True(t, moduleBalance(bank).IsZero(), "no money should move on a rejected label")

	// Too long (64 chars) -> rejected.
	long := ""
	for i := 0; i < 64; i++ {
		long += "a"
	}
	_, err = k.SendToNameCollection(types.MsgSendToNameCollection{Sender: ownerA, Opcode: types.OpcodeRegister, Comment: long, AmountNaet: 5000, Height: 1})
	require.Error(t, err)
}

func TestRegisterUnderfundedRefundMinusFee(t *testing.T) {
	k, bank := setupCollectionKeeper(t)
	sender := accAddr(t, ownerA)
	fund(bank, sender, 1000)
	before := bank.get(sender.String())

	// incoming (100) < price (5000): keep min(100, fee=50)=50, refund 50.
	res, err := k.SendToNameCollection(types.MsgSendToNameCollection{Sender: ownerA, Opcode: types.OpcodeRegister, Comment: "alice", AmountNaet: 100, Height: 1})
	require.NoError(t, err)
	require.Equal(t, "underfunded_refunded", res.Outcome)
	require.Equal(t, uint64(50), res.FeeKeptNaet)
	require.Equal(t, uint64(50), res.RefundNaet)
	// Module net += fee, and is non-negative.
	require.Equal(t, sdkmath.NewInt(50), moduleBalance(bank))
	require.False(t, moduleBalance(bank).IsNegative())
	// Sender lost exactly the fee.
	require.Equal(t, before.SubRaw(50), bank.get(sender.String()))

	// Fee larger than incoming: feeKept = incoming, refund = 0, still non-negative.
	res, err = k.SendToNameCollection(types.MsgSendToNameCollection{Sender: ownerA, Opcode: types.OpcodeRegister, Comment: "alice", AmountNaet: 30, Height: 2})
	require.NoError(t, err)
	require.Equal(t, uint64(30), res.FeeKeptNaet)
	require.Equal(t, uint64(0), res.RefundNaet)
	require.Equal(t, sdkmath.NewInt(80), moduleBalance(bank))
}

func TestRegisterFreeOpensAuctionTakenRejected(t *testing.T) {
	k, bank := setupCollectionKeeper(t)
	sender := accAddr(t, ownerA)
	fund(bank, sender, 1_000_000)
	fund(bank, accAddr(t, ownerB), 1_000_000)

	// Sufficient funds on a FREE label -> auction opens with incoming escrowed.
	res, err := k.SendToNameCollection(types.MsgSendToNameCollection{Sender: ownerA, Opcode: types.OpcodeRegister, Comment: "alice", AmountNaet: 5000, Height: 1})
	require.NoError(t, err)
	require.True(t, res.AuctionOpened)
	require.Equal(t, "alice.aet", res.Name)
	require.Equal(t, uint64(11), res.DeadlineHeight)
	require.Equal(t, sdkmath.NewInt(5000), moduleBalance(bank), "opening bid is escrowed")
	auction, found, err := k.auctionView("alice")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, types.AuctionKindIssuance, auction.Kind)

	// Close it so the name becomes taken & active.
	runEndBlock(t, k, 11)
	record, found, err := k.NameRecord("alice")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, ownerA, record.Owner)

	// REGISTER on a TAKEN active label -> rejected with refund minus fee.
	res, err = k.SendToNameCollection(types.MsgSendToNameCollection{Sender: ownerB, Opcode: types.OpcodeRegister, Comment: "alice", AmountNaet: 5000, Height: 12})
	require.NoError(t, err)
	require.Equal(t, "rejected_taken", res.Outcome)
	require.Equal(t, uint64(50), res.FeeKeptNaet)
	require.Equal(t, uint64(4950), res.RefundNaet)
}

func TestPlaceBidEnforcesRaiseAndRefundsLosers(t *testing.T) {
	k, bank := setupCollectionKeeper(t)
	a := accAddr(t, ownerA)
	b := accAddr(t, ownerB)
	fund(bank, a, 1_000_000)
	fund(bank, b, 1_000_000)
	aStart := bank.get(a.String())

	// ownerA opens the auction at 5000.
	_, err := k.SendToNameCollection(types.MsgSendToNameCollection{Sender: ownerA, Opcode: types.OpcodeRegister, Comment: "alice", AmountNaet: 5000, Height: 1})
	require.NoError(t, err)

	// A bid equal to the high bid is rejected (needs +5% = 5250).
	_, err = k.PlaceBid(types.MsgPlaceBid{Bidder: ownerB, Name: "alice", AmountNaet: 5000, Height: 2})
	require.ErrorContains(t, err, "below the minimum")
	// A bid just under the raise is rejected.
	_, err = k.PlaceBid(types.MsgPlaceBid{Bidder: ownerB, Name: "alice", AmountNaet: 5249, Height: 2})
	require.ErrorContains(t, err, "below the minimum")

	// A valid +5% bid escrows and refunds the previous high bidder (ownerA).
	bid, err := k.PlaceBid(types.MsgPlaceBid{Bidder: ownerB, Name: "alice", AmountNaet: 5250, Height: 2})
	require.NoError(t, err)
	require.Equal(t, uint64(5250), bid.HighBidNaet)
	require.Equal(t, ownerB, bid.HighBidder)
	require.Equal(t, uint64(5000), bid.RefundedPreviousNaet)
	require.Equal(t, aStart, bank.get(a.String()), "outbid opener is refunded in full")
	require.Equal(t, sdkmath.NewInt(5250), moduleBalance(bank), "only the current high bid is escrowed")

	// Close: ownerB wins; ownerA (loser) keeps their refund; proceeds retained.
	runEndBlock(t, k, 11)
	record, found, err := k.NameRecord("alice")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, ownerB, record.Owner)
	require.Equal(t, aStart, bank.get(a.String()))
	require.Equal(t, sdkmath.NewInt(5250), moduleBalance(bank), "issuance proceeds stay retained")
}

func TestAuctionCloseGrantsFreshTerm(t *testing.T) {
	k, bank := setupCollectionKeeper(t)
	fund(bank, accAddr(t, ownerA), 1_000_000)

	_, err := k.SendToNameCollection(types.MsgSendToNameCollection{Sender: ownerA, Opcode: types.OpcodeRegister, Comment: "alice", AmountNaet: 5000, Height: 1})
	require.NoError(t, err)

	// Not yet due.
	runEndBlock(t, k, 10)
	_, found, err := k.NameRecord("alice")
	require.NoError(t, err)
	require.False(t, found, "auction has not closed yet")

	// Due at 11: winner gets a fresh RegistrationPeriod term from the close height.
	runEndBlock(t, k, 11)
	record, found, err := k.NameRecord("alice")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, ownerA, record.Owner)
	require.Equal(t, uint64(111), record.ExpiryHeight) // 11 + RegistrationPeriod(100)
	_, closed, err := k.auctionView("alice")
	require.NoError(t, err)
	require.False(t, closed, "auction is removed after close")
}

func TestPurchaseResetsTermTransferDoesNot(t *testing.T) {
	k, bank := setupCollectionKeeper(t)
	fund(bank, accAddr(t, ownerA), 1_000_000)

	_, err := k.SendToNameCollection(types.MsgSendToNameCollection{Sender: ownerA, Opcode: types.OpcodeRegister, Comment: "alice", AmountNaet: 5000, Height: 1})
	require.NoError(t, err)
	runEndBlock(t, k, 11)
	record, _, err := k.NameRecord("alice")
	require.NoError(t, err)
	require.Equal(t, uint64(111), record.ExpiryHeight)

	// A plain gift transfer does NOT reset the term.
	transferred, err := k.TransferName(types.MsgTransferName{Owner: ownerA, Name: "alice", NewOwner: ownerB, Height: 50})
	require.NoError(t, err)
	require.Equal(t, ownerB, transferred.Owner)
	require.Equal(t, uint64(111), transferred.ExpiryHeight, "gift keeps the old expiry")
}

func TestRenewWindowRejectsOutsideAndWhenExpired(t *testing.T) {
	k, bank := setupCollectionKeeper(t)
	fund(bank, accAddr(t, ownerA), 1_000_000)
	// RenewalWindowBlocks=200, RegistrationPeriod=100.
	_, err := k.SendToNameCollection(types.MsgSendToNameCollection{Sender: ownerA, Opcode: types.OpcodeRegister, Comment: "alice", AmountNaet: 5000, Height: 1})
	require.NoError(t, err)
	runEndBlock(t, k, 11) // alice owned by ownerA, expiry 111.

	// Inside the window (111-30=81 <= 200) and before expiry -> accepted,
	// extends from ExpiryHeight (111 -> 211).
	renewed, err := k.RenewName(types.MsgRenewName{Owner: ownerA, Name: "alice", Height: 30})
	require.NoError(t, err)
	require.Equal(t, uint64(211), renewed.ExpiryHeight)

	// Past expiry -> rejected (must re-acquire via auction).
	_, err = k.RenewName(types.MsgRenewName{Owner: ownerA, Name: "alice", Height: 211})
	require.ErrorContains(t, err, "expired")

	// Register a second name with a far expiry, then try to renew way too early.
	_, err = k.SendToNameCollection(types.MsgSendToNameCollection{Sender: ownerA, Opcode: types.OpcodeRegister, Comment: "farname", AmountNaet: 5000, Height: 300})
	require.NoError(t, err)
	runEndBlock(t, k, 310) // farname expiry = 310 + 100 = 410.
	// At height 100, 410-100=310 > window(200) -> too early.
	_, err = k.RenewName(types.MsgRenewName{Owner: ownerA, Name: "farname", Height: 100})
	require.ErrorContains(t, err, "renewal window")
}

func TestTreasurySweepOncePerInterval(t *testing.T) {
	k, bank := setupCollectionKeeper(t)
	// 2000 AET in the collection, floor 100 AET, no open auctions.
	bank.set(CollectionModuleAddress().String(), sdkmath.NewInt(2_000_000_000_000))

	// First sweep at the interval boundary: 1900 AET to treasury, 100 remains.
	runEndBlock(t, k, 10)
	require.Equal(t, sdkmath.NewInt(1_900_000_000_000), treasuryBalance(bank))
	require.Equal(t, sdkmath.NewInt(100_000_000_000), moduleBalance(bank))

	// Not eligible again before a full interval elapses.
	runEndBlock(t, k, 15)
	require.Equal(t, sdkmath.NewInt(1_900_000_000_000), treasuryBalance(bank))
	require.Equal(t, sdkmath.NewInt(100_000_000_000), moduleBalance(bank))

	// Next interval: nothing above the floor, so nothing more is swept.
	runEndBlock(t, k, 20)
	require.Equal(t, sdkmath.NewInt(1_900_000_000_000), treasuryBalance(bank))
	require.Equal(t, sdkmath.NewInt(100_000_000_000), moduleBalance(bank))
}

func TestSweepNeverTakesOpenAuctionEscrow(t *testing.T) {
	k, bank := setupCollectionKeeper(t)
	fund(bank, accAddr(t, ownerA), 1_000_000)
	// Give the module a large balance plus an open auction escrow.
	bank.set(CollectionModuleAddress().String(), sdkmath.NewInt(2_000_000_000_000))
	// Open an auction (escrows 5000 more) that is NOT yet due.
	_, err := k.SendToNameCollection(types.MsgSendToNameCollection{Sender: ownerA, Opcode: types.OpcodeRegister, Comment: "alice", AmountNaet: 5000, Height: 1})
	require.NoError(t, err)
	moduleBefore := moduleBalance(bank)

	// Sweep at height 10: sweepable = balance - escrow(5000) - floor(100 AET).
	runEndBlock(t, k, 10)
	// The open auction's 5000 escrow must remain; only the excess above floor+escrow is swept.
	expectedSwept := moduleBefore.Sub(sdkmath.NewInt(5000)).Sub(sdkmath.NewInt(100_000_000_000))
	require.Equal(t, expectedSwept, treasuryBalance(bank))
	require.Equal(t, moduleBefore.Sub(expectedSwept), moduleBalance(bank))
	require.True(t, moduleBalance(bank).GTE(sdkmath.NewInt(5000)), "escrow is never swept")
}

func TestStartAuctionOwnerListedPaysSeller(t *testing.T) {
	k, bank := setupCollectionKeeper(t)
	seller := accAddr(t, ownerA)
	buyer := accAddr(t, ownerB)
	fund(bank, seller, 1_000_000)
	fund(bank, buyer, 1_000_000)

	// ownerA acquires alice via issuance.
	_, err := k.SendToNameCollection(types.MsgSendToNameCollection{Sender: ownerA, Opcode: types.OpcodeRegister, Comment: "alice", AmountNaet: 5000, Height: 1})
	require.NoError(t, err)
	runEndBlock(t, k, 11)

	// Duration out of range is rejected.
	_, err = k.StartAuction(types.MsgStartAuction{Owner: ownerA, Name: "alice", StartPriceNaet: 1000, DurationDays: 1, Height: 12})
	require.ErrorContains(t, err, "duration")

	// ownerA lists alice for 7 days (7*BlocksPerDay(10)=70 blocks) at start price 1000.
	start, err := k.StartAuction(types.MsgStartAuction{Owner: ownerA, Name: "alice", StartPriceNaet: 1000, DurationDays: 7, Height: 12})
	require.NoError(t, err)
	require.Equal(t, uint64(82), start.DeadlineHeight) // 12 + 70

	sellerBefore := bank.get(seller.String())
	// Buyer bids 1000 (meets the open price for the first bid).
	_, err = k.PlaceBid(types.MsgPlaceBid{Bidder: ownerB, Name: "alice", AmountNaet: 1000, Height: 20})
	require.NoError(t, err)

	// Close: buyer wins with a fresh term; seller is paid the proceeds.
	runEndBlock(t, k, 82)
	record, found, err := k.NameRecord("alice")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, ownerB, record.Owner)
	require.Equal(t, uint64(182), record.ExpiryHeight) // 82 + RegistrationPeriod(100)
	require.Equal(t, sellerBefore.AddRaw(1000), bank.get(seller.String()), "owner-listed proceeds go to the seller")
}

func TestTopUpAddsToCollection(t *testing.T) {
	k, bank := setupCollectionKeeper(t)
	fund(bank, accAddr(t, ownerA), 1_000_000)
	res, err := k.SendToNameCollection(types.MsgSendToNameCollection{Sender: ownerA, Opcode: types.OpcodeTopUp, AmountNaet: 12345, Height: 1})
	require.NoError(t, err)
	require.Equal(t, "topped_up", res.Outcome)
	require.Equal(t, sdkmath.NewInt(12345), moduleBalance(bank))
}
