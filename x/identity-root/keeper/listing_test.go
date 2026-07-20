package keeper

import (
	"context"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/identity-root/types"
)

// This file tests the ANS Phase B owner fixed-price sale surface
// (docs/architecture/ans.md "Owner fixed-price sale"): MsgListForSale,
// MsgDelistName and the buyer side MsgBuyListedName. It reuses
// setupCollectionKeeper/mockBank from collection_test.go because BuyListedName
// moves real money and the mock bank enforces non-negativity, exactly like the
// auction tests in that file.

func TestListForSaleThenBuySucceedsAndPaysSeller(t *testing.T) {
	k, bank := setupCollectionKeeper(t)
	seller := accAddr(t, ownerA)
	buyer := accAddr(t, ownerB)
	fund(bank, buyer, 1_000_000)

	_, err := k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "alice", Height: 1})
	require.NoError(t, err)
	sellerBefore := bank.get(seller.String())
	buyerBefore := bank.get(buyer.String())

	listing, err := k.ListForSale(types.MsgListForSale{Owner: ownerA, Name: "alice", PriceNaet: 2000, Height: 5})
	require.NoError(t, err)
	require.Equal(t, "alice.aet", listing.Name)
	require.Equal(t, ownerA, listing.Seller)
	require.Equal(t, "2000", listing.PriceNaet)

	found, ok, err := k.listingView(context.Background(), "alice")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, listing, found)

	outcome, err := k.BuyListedName(types.MsgBuyListedName{Buyer: ownerB, Name: "alice", Height: 6})
	require.NoError(t, err)
	require.Equal(t, "alice.aet", outcome.Name)
	require.Equal(t, ownerB, outcome.Owner)
	require.Equal(t, uint64(2000), outcome.PriceNaet)
	require.Equal(t, uint64(106), outcome.ExpiryHeight) // 6 + RegistrationPeriod(100): a purchase resets the term.

	// Name and payment moved atomically.
	record, ok, err := k.NameRecord(context.Background(), "alice")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, ownerB, record.Owner)
	require.Equal(t, uint64(106), record.ExpiryHeight)
	require.Equal(t, sellerBefore.AddRaw(2000), bank.get(seller.String()), "seller is paid the listing price")
	require.Equal(t, buyerBefore.SubRaw(2000), bank.get(buyer.String()), "buyer pays exactly the listing price")
	require.True(t, moduleBalance(bank).IsZero(), "the module never retains a fixed-price sale's proceeds")

	// The listing is gone after the sale.
	_, ok, err = k.listingView(context.Background(), "alice")
	require.NoError(t, err)
	require.False(t, ok, "listing must clear once bought")
}

func TestBuyListedNameWithoutListingRejected(t *testing.T) {
	k, bank := setupCollectionKeeper(t)
	buyer := accAddr(t, ownerB)
	fund(bank, buyer, 1_000_000)

	_, err := k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "bob", Height: 1})
	require.NoError(t, err)

	_, err = k.BuyListedName(types.MsgBuyListedName{Buyer: ownerB, Name: "bob", Height: 2})
	require.ErrorContains(t, err, "no listing")
	require.True(t, moduleBalance(bank).IsZero(), "a rejected buy must move no money")

	record, ok, err := k.NameRecord(context.Background(), "bob")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, ownerA, record.Owner, "ownership must not change on a rejected buy")
}

func TestBuyListedNameInsufficientFundsRejected(t *testing.T) {
	k, bank := setupCollectionKeeper(t)
	buyer := accAddr(t, ownerB)
	// Buyer has far less than the listing price.
	fund(bank, buyer, 100)

	_, err := k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "alice", Height: 1})
	require.NoError(t, err)
	_, err = k.ListForSale(types.MsgListForSale{Owner: ownerA, Name: "alice", PriceNaet: 2000, Height: 5})
	require.NoError(t, err)

	_, err = k.BuyListedName(types.MsgBuyListedName{Buyer: ownerB, Name: "alice", Height: 6})
	require.ErrorContains(t, err, "insufficient balance")

	// Nothing committed: ownership, listing, and the buyer's balance are all
	// exactly as before the failed attempt (the state checks in BuyListedName
	// all run before the bank is touched, and persistLocked is never reached).
	record, ok, err := k.NameRecord(context.Background(), "alice")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, ownerA, record.Owner)
	_, stillListed, err := k.listingView(context.Background(), "alice")
	require.NoError(t, err)
	require.True(t, stillListed)
	require.Equal(t, sdkmath.NewInt(100), bank.get(buyer.String()))
}

func TestListForSaleByNonOwnerRejected(t *testing.T) {
	k := setupKeeper(t)
	_, err := k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "alice", Height: 1})
	require.NoError(t, err)

	_, err = k.ListForSale(types.MsgListForSale{Owner: ownerB, Name: "alice", PriceNaet: 1000, Height: 5})
	require.ErrorContains(t, err, "requires owner")

	_, found, err := k.listingView(context.Background(), "alice")
	require.NoError(t, err)
	require.False(t, found, "a rejected list must not create a listing")
}

func TestDelistThenBuyRejected(t *testing.T) {
	k, bank := setupCollectionKeeper(t)
	buyer := accAddr(t, ownerB)
	fund(bank, buyer, 1_000_000)

	_, err := k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "alice", Height: 1})
	require.NoError(t, err)
	_, err = k.ListForSale(types.MsgListForSale{Owner: ownerA, Name: "alice", PriceNaet: 2000, Height: 5})
	require.NoError(t, err)

	delisted, err := k.DelistName(types.MsgDelistName{Owner: ownerA, Name: "alice", Height: 6})
	require.NoError(t, err)
	require.Equal(t, "alice.aet", delisted.Name)

	_, found, err := k.listingView(context.Background(), "alice")
	require.NoError(t, err)
	require.False(t, found)

	_, err = k.BuyListedName(types.MsgBuyListedName{Buyer: ownerB, Name: "alice", Height: 7})
	require.ErrorContains(t, err, "no listing")
	require.True(t, moduleBalance(bank).IsZero())
}

func TestListForSalePriceMustBePositive(t *testing.T) {
	k := setupKeeper(t)
	_, err := k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "alice", Height: 1})
	require.NoError(t, err)

	_, err = k.ListForSale(types.MsgListForSale{Owner: ownerA, Name: "alice", PriceNaet: 0, Height: 5})
	require.ErrorContains(t, err, "positive")

	_, found, err := k.listingView(context.Background(), "alice")
	require.NoError(t, err)
	require.False(t, found)
}

// TestListForSaleRejectsWhileAuctionOpen and TestStartAuctionRejectsWhileListed
// prove the two "for sale" mechanisms are mutually exclusive for the same name,
// so a buy-now purchase and a winning bid can never race for the same record.
func TestListForSaleRejectsWhileAuctionOpen(t *testing.T) {
	k, bank := setupCollectionKeeper(t)
	fund(bank, accAddr(t, ownerA), 1_000_000)

	// Open an owner-listed auction on a name ownerA owns.
	_, err := k.SendToNameCollection(types.MsgSendToNameCollection{Sender: ownerA, Opcode: types.OpcodeRegister, Comment: "alice", AmountNaet: 5000, Height: 1})
	require.NoError(t, err)
	runEndBlock(t, k, 11)
	_, err = k.StartAuction(types.MsgStartAuction{Owner: ownerA, Name: "alice", StartPriceNaet: 1000, DurationDays: 7, Height: 12})
	require.NoError(t, err)

	_, err = k.ListForSale(types.MsgListForSale{Owner: ownerA, Name: "alice", PriceNaet: 2000, Height: 13})
	require.ErrorContains(t, err, "auction is already open")
}

func TestStartAuctionRejectsWhileListed(t *testing.T) {
	k, bank := setupCollectionKeeper(t)
	fund(bank, accAddr(t, ownerA), 1_000_000)

	_, err := k.SendToNameCollection(types.MsgSendToNameCollection{Sender: ownerA, Opcode: types.OpcodeRegister, Comment: "alice", AmountNaet: 5000, Height: 1})
	require.NoError(t, err)
	runEndBlock(t, k, 11)

	_, err = k.ListForSale(types.MsgListForSale{Owner: ownerA, Name: "alice", PriceNaet: 2000, Height: 12})
	require.NoError(t, err)

	_, err = k.StartAuction(types.MsgStartAuction{Owner: ownerA, Name: "alice", StartPriceNaet: 1000, DurationDays: 7, Height: 13})
	require.ErrorContains(t, err, "already listed")
}

// TestTransferNameClearsListing proves a plain gift clears any open listing so
// the listing's Seller can never go stale relative to the new owner (see
// state.go's Validate "identity listing seller must match name owner" check).
func TestTransferNameClearsListing(t *testing.T) {
	k := setupKeeper(t)
	_, err := k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "alice", Height: 1})
	require.NoError(t, err)
	_, err = k.ListForSale(types.MsgListForSale{Owner: ownerA, Name: "alice", PriceNaet: 2000, Height: 5})
	require.NoError(t, err)

	_, err = k.TransferName(types.MsgTransferName{Owner: ownerA, Name: "alice", NewOwner: ownerB, Height: 6})
	require.NoError(t, err)

	_, found, err := k.listingView(context.Background(), "alice")
	require.NoError(t, err)
	require.False(t, found, "a gift transfer must clear any open listing")
}

// TestListForSaleAndBuyReachableThroughGRPC proves the whole surface is wired
// through the real grpcMsgServer/grpcQueryServer, mirroring
// phase_c_grpc_test.go's TestPhaseCMsgsAreLiveReachable.
func TestListForSaleAndBuyReachableThroughGRPC(t *testing.T) {
	k, bank := setupCollectionKeeper(t)
	buyer := accAddr(t, ownerB)
	fund(bank, buyer, 1_000_000)
	msgSrv := NewGRPCMsgServer(k)
	querySrv := NewGRPCQueryServer(k)
	ctx := sdk.Context{}.WithBlockHeight(10)
	k.runtimeCtx = ctx

	_, err := k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "alice", Height: 1})
	require.NoError(t, err)

	listRes, err := msgSrv.ListForSale(ctx, &types.MsgListForSale{Owner: ownerA, Name: "alice", PriceNaet: 3000})
	require.NoError(t, err)
	require.Equal(t, "alice.aet", listRes.Name)
	require.Equal(t, uint64(3000), listRes.PriceNaet)

	queryRes, err := querySrv.Listing(ctx, &types.QueryListingRequest{Name: "alice"})
	require.NoError(t, err)
	require.True(t, queryRes.Found)
	require.Equal(t, "3000", queryRes.Listing.PriceNaet)
	require.Equal(t, ownerA, queryRes.Listing.Seller)

	buyRes, err := msgSrv.BuyListedName(ctx, &types.MsgBuyListedName{Buyer: ownerB, Name: "alice"})
	require.NoError(t, err)
	require.Equal(t, ownerB, buyRes.Owner)
	require.Equal(t, uint64(3000), buyRes.PriceNaet)

	queryRes, err = querySrv.Listing(ctx, &types.QueryListingRequest{Name: "alice"})
	require.NoError(t, err)
	require.False(t, queryRes.Found, "listing must clear after the grpc buy")
}
