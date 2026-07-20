package keeper

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/identity-root/types"
)

// TestRegisterCapacityGuardPreventsAuctionBatchOverflow is the regression
// guard for the audit's CRITICAL finding: an identity-root auction-grant
// batch could push Records past MaxRecords, hard-failing the whole
// EndBlocker (a deterministic full-network chain halt, not a rejected tx).
//
// Minimal trigger, reproduced exactly as the audit traced it: the registry
// sits at MaxRecords-1, and two ordinary REGISTERs for distinct free labels
// land in the SAME block. msg.Height is block-height-driven (grpc_server.go's
// blockHeight override), so both auctions automatically share a deadline --
// no attacker coordination required. Without the fix, BOTH auctions open,
// both grant in the same EndBlocker batch, and runEndBlockLocked's single
// post-batch validateGlobal call rejects the whole batch atomically, which
// propagates as an EndBlocker error = chain halt.
//
// With the fix, registerViaCollectionLocked rejects the SECOND register at
// open-time (Records + open-issuance-auctions + 1 > MaxRecords), via the
// ordinary refund-minus-fee path, so the batch can never form in the first
// place.
func TestRegisterCapacityGuardPreventsAuctionBatchOverflow(t *testing.T) {
	k, bank := setupCollectionKeeper(t)
	fund(bank, accAddr(t, ownerA), 1_000_000)
	fund(bank, accAddr(t, ownerB), 1_000_000)

	gs := k.ExportGenesis()
	gs.IdentityParams.MaxRecords = 1
	require.NoError(t, k.InitGenesis(gs))
	k.runtimeCtx = context.Background()

	// First REGISTER: 0 records + 0 open issuance auctions + 1 <= MaxRecords(1) -> opens.
	res1, err := k.SendToNameCollection(types.MsgSendToNameCollection{Sender: ownerA, Opcode: types.OpcodeRegister, Comment: "alice", AmountNaet: 5000, Height: 1})
	require.NoError(t, err)
	require.True(t, res1.AuctionOpened)
	require.Equal(t, "auction_opened", res1.Outcome)

	// Second REGISTER, SAME block, distinct free label: 0 records + 1 open
	// issuance auction (alice's) + 1 > MaxRecords(1) -> must be rejected at
	// open-time, refunded minus fee, never reaching the EndBlocker as a second
	// auction that could overflow the batch.
	res2, err := k.SendToNameCollection(types.MsgSendToNameCollection{Sender: ownerB, Opcode: types.OpcodeRegister, Comment: "bob", AmountNaet: 5000, Height: 1})
	require.NoError(t, err)
	require.Equal(t, "rejected_capacity", res2.Outcome, "the second REGISTER in the same block must be capacity-rejected, not allowed to open a second auction")
	require.False(t, res2.AuctionOpened)
	require.Equal(t, uint64(50), res2.FeeKeptNaet)
	require.Equal(t, uint64(4950), res2.RefundNaet)

	// Only alice's auction is due; the EndBlocker must succeed and Records
	// must land at exactly MaxRecords, never over it.
	runEndBlock(t, k, 11)
	exported := k.ExportGenesis()
	require.Len(t, exported.State.Records, 1)
	require.Equal(t, "alice.aet", exported.State.Records[0].Name)
}

// TestRegisterCapacityGuardRejectsOverMaxAuctions isolates the second,
// independent cap the same fix introduces (Finding 4): MaxAuctions bounds the
// number of simultaneously open auctions on its own, even when Records has
// headroom (MaxRecords left at its generous default here).
func TestRegisterCapacityGuardRejectsOverMaxAuctions(t *testing.T) {
	k, bank := setupCollectionKeeper(t)
	fund(bank, accAddr(t, ownerA), 1_000_000)
	fund(bank, accAddr(t, ownerB), 1_000_000)

	gs := k.ExportGenesis()
	gs.IdentityParams.MaxAuctions = 1
	require.NoError(t, k.InitGenesis(gs))
	k.runtimeCtx = context.Background()

	res1, err := k.SendToNameCollection(types.MsgSendToNameCollection{Sender: ownerA, Opcode: types.OpcodeRegister, Comment: "alice", AmountNaet: 5000, Height: 1})
	require.NoError(t, err)
	require.True(t, res1.AuctionOpened)

	res2, err := k.SendToNameCollection(types.MsgSendToNameCollection{Sender: ownerB, Opcode: types.OpcodeRegister, Comment: "bob", AmountNaet: 5000, Height: 1})
	require.NoError(t, err)
	require.Equal(t, "rejected_capacity", res2.Outcome, "a second auction must be rejected once MaxAuctions is reached, independent of MaxRecords")
}

// TestStartAuctionRejectsOverMaxAuctions proves the same MaxAuctions cap
// applies to StartAuction (owner-listed auctions), which grows Auctions
// without growing Records -- the second, independent path the fix guards
// (grantAuctionName only re-owns an EXISTING record for an owner-listed
// auction; MaxRecords alone would never catch this).
func TestStartAuctionRejectsOverMaxAuctions(t *testing.T) {
	k, bank := setupCollectionKeeper(t)
	fund(bank, accAddr(t, ownerA), 1_000_000)

	// Acquire two names (auctions closed, so they don't count against
	// MaxAuctions once capped below).
	_, err := k.SendToNameCollection(types.MsgSendToNameCollection{Sender: ownerA, Opcode: types.OpcodeRegister, Comment: "alice", AmountNaet: 5000, Height: 1})
	require.NoError(t, err)
	runEndBlock(t, k, 11)

	gs := k.ExportGenesis()
	gs.IdentityParams.MaxAuctions = 1
	require.NoError(t, k.InitGenesis(gs))
	k.runtimeCtx = context.Background()

	// Open a fresh issuance auction on a second label to occupy the one slot.
	_, err = k.SendToNameCollection(types.MsgSendToNameCollection{Sender: ownerA, Opcode: types.OpcodeRegister, Comment: "bob", AmountNaet: 5000, Height: 12})
	require.NoError(t, err)

	// StartAuction on the already-owned, active "alice" must be rejected: the
	// bob issuance auction already occupies the single MaxAuctions slot.
	_, err = k.StartAuction(types.MsgStartAuction{Owner: ownerA, Name: "alice", StartPriceNaet: 1000, DurationDays: 7, Height: 12})
	require.ErrorContains(t, err, "capacity")
}

// TestStartAuctionDurationOverflowGuard is the regression guard for the
// audit's unchecked-multiplication finding: StartAuction computed
// days*params.BlocksPerDay as a bare argument expression before addHeight
// ever saw it, so a BlocksPerDay large enough (a genesis value; no live Msg
// path can change it) could overflow uint64 silently, wrapping to a tiny or
// arbitrary deadline instead of erroring. The fix guards the multiplication
// itself with the same idiom DomainStorageRentDelta already uses.
func TestStartAuctionDurationOverflowGuard(t *testing.T) {
	k, bank := setupCollectionKeeper(t)
	fund(bank, accAddr(t, ownerA), 1_000_000)

	_, err := k.SendToNameCollection(types.MsgSendToNameCollection{Sender: ownerA, Opcode: types.OpcodeRegister, Comment: "alice", AmountNaet: 5000, Height: 1})
	require.NoError(t, err)
	runEndBlock(t, k, 11)

	// A BlocksPerDay chosen so that days(365, the max StartAuction allows) *
	// BlocksPerDay overflows uint64 with plenty of margin: days=365 must be
	// greater than ^uint64(0)/BlocksPerDay for the guard to trip.
	gs := k.ExportGenesis()
	gs.IdentityParams.BlocksPerDay = ^uint64(0) / 2
	gs.IdentityParams.OwnerAuctionMinDurationBlocks = 1
	gs.IdentityParams.OwnerAuctionMaxDurationBlocks = ^uint64(0)
	require.NoError(t, k.InitGenesis(gs))
	k.runtimeCtx = context.Background()

	_, err = k.StartAuction(types.MsgStartAuction{Owner: ownerA, Name: "alice", StartPriceNaet: 1000, DurationDays: 365, Height: 12})
	require.ErrorContains(t, err, "overflow", "a duration*BlocksPerDay product that overflows uint64 must be rejected, not silently wrapped")
}
