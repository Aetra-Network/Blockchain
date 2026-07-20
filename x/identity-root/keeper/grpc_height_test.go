package keeper

import (
	"context"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/identity-root/types"
)

// TestMsgServerIgnoresUserSuppliedHeight is the DEFECT 2 regression. The live
// Msg server used to forward the user-supplied msg.Height verbatim to the keeper
// (grpc_server.go -> collection.go), which drives the auction OpenedHeight /
// DeadlineHeight / CreatedHeight. A caller could therefore pick any deadline for
// their own issuance auction. The fix overwrites msg.Height from the block
// context (blockHeight(ctx)) UNCONDITIONALLY before the keeper runs, so auction
// timing is block-driven, never user-driven.
//
// Each sub-test drives the real grpcMsgServer with an SDK context pinned to a
// block height and a msg.Height set to a wildly different attacker value, then
// asserts the recorded auction timing reflects the block height, not the attacker
// value. Against the pre-fix tree (no override) these assertions fail: the
// deadline/created height come out at the attacker's 900_000-based numbers.
func TestMsgServerIgnoresUserSuppliedHeight(t *testing.T) {
	const (
		blockHeightAt   = int64(500)
		attackerHeight  = uint64(900_000)
		auctionDuration = uint64(10) // IssuanceAuctionDurationBlocks in setupCollectionKeeper
	)

	t.Run("SendToNameCollection opens a block-driven auction", func(t *testing.T) {
		k, bank := setupCollectionKeeper(t)
		fund(bank, accAddr(t, ownerA), 1_000_000)
		srv := NewGRPCMsgServer(k)
		ctx := sdk.Context{}.WithBlockHeight(blockHeightAt)

		res, err := srv.SendToNameCollection(ctx, &types.MsgSendToNameCollection{
			Sender:     ownerA,
			Opcode:     types.OpcodeRegister,
			Comment:    "alice",
			AmountNaet: 5000,
			Height:     attackerHeight,
		})
		require.NoError(t, err)
		require.True(t, res.AuctionOpened)
		// Block-driven: 500 + 10, NOT 900_000 + 10.
		require.Equal(t, uint64(blockHeightAt)+auctionDuration, res.DeadlineHeight,
			"deadline must be derived from the block height, not the caller's msg.Height")

		auction, found, err := k.auctionView(context.Background(), "alice")
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, uint64(blockHeightAt), auction.CreatedHeight,
			"auction CreatedHeight must be the block height, not the caller's msg.Height")
		require.Equal(t, uint64(blockHeightAt)+auctionDuration, auction.DeadlineHeight)
	})

	t.Run("StartAuction deadline is block-driven", func(t *testing.T) {
		// The name is issued at block 1 and closes at 11 with a 100-block term
		// (expiry 111), so it must be listed while still active. List at block 50.
		const listHeight = int64(50)

		k, bank := setupCollectionKeeper(t)
		fund(bank, accAddr(t, ownerA), 1_000_000)
		srv := NewGRPCMsgServer(k)

		// Acquire alice via issuance and close it, so ownerA can list it.
		_, err := srv.SendToNameCollection(sdk.Context{}.WithBlockHeight(1), &types.MsgSendToNameCollection{
			Sender: ownerA, Opcode: types.OpcodeRegister, Comment: "alice", AmountNaet: 5000, Height: 1,
		})
		require.NoError(t, err)
		runEndBlock(t, k, 11)

		// List it at block height 50 with an attacker msg.Height. 7 days *
		// BlocksPerDay(10) = 70 blocks, so a block-driven deadline is 50 + 70.
		res, err := srv.StartAuction(sdk.Context{}.WithBlockHeight(listHeight), &types.MsgStartAuction{
			Owner: ownerA, Name: "alice", StartPriceNaet: 1000, DurationDays: 7, Height: attackerHeight,
		})
		require.NoError(t, err)
		require.Equal(t, uint64(listHeight)+70, res.DeadlineHeight,
			"owner-listed auction deadline must be derived from the block height, not msg.Height")
	})

	t.Run("PlaceBid is evaluated at the block height", func(t *testing.T) {
		k, bank := setupCollectionKeeper(t)
		fund(bank, accAddr(t, ownerA), 1_000_000)
		fund(bank, accAddr(t, ownerB), 1_000_000)
		srv := NewGRPCMsgServer(k)

		// Open an issuance auction at block 1 (deadline 11).
		_, err := srv.SendToNameCollection(sdk.Context{}.WithBlockHeight(1), &types.MsgSendToNameCollection{
			Sender: ownerA, Opcode: types.OpcodeRegister, Comment: "alice", AmountNaet: 5000, Height: 1,
		})
		require.NoError(t, err)

		// A bid whose ATTACKER height (900_000) is well past the deadline must be
		// rejected, because the handler evaluates the bid at the real block height
		// (2 < 11), not at the caller-supplied height. Pre-fix, forwarding
		// msg.Height=900_000 >= deadline(11) makes the keeper reject the bid as
		// "auction has ended" -- so this NoError assertion fails pre-fix.
		bid, err := srv.PlaceBid(sdk.Context{}.WithBlockHeight(2), &types.MsgPlaceBid{
			Bidder: ownerB, Name: "alice", AmountNaet: 5250, Height: attackerHeight,
		})
		require.NoError(t, err, "bid at block height 2 must be accepted; the auction closes at 11 regardless of msg.Height")
		require.Equal(t, ownerB, bid.HighBidder)
	})
}
