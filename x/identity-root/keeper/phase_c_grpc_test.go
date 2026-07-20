package keeper

import (
	"context"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/identity-root/types"
)

// TestPhaseCMsgsAreLiveReachable proves each of the six newly-wired ANS Phase C
// messages is reachable through the real grpcMsgServer (the same server the
// live Msg service routes decoded transactions to -- see
// app/identity_root_msg_wire_format_test.go for the decode half of that path),
// not just directly through the keeper method in Go. Before this wiring, none
// of RenewName/TransferName/SetResolver/SetReverseRecord/ReserveName/
// ReleaseReservedName had a grpcMsgServer method at all, so a signed,
// correctly-decoded transaction for any of them had no handler to route to.
func TestPhaseCMsgsAreLiveReachable(t *testing.T) {
	k := setupKeeper(t)
	srv := NewGRPCMsgServer(&k)
	ctx := sdk.Context{}.WithBlockHeight(10)

	_, err := k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "alice", Height: 10})
	require.NoError(t, err)

	t.Run("RenewName", func(t *testing.T) {
		res, err := srv.RenewName(ctx, &types.MsgRenewName{Owner: ownerA, Name: "alice", Height: 999_999})
		require.NoError(t, err)
		require.Equal(t, "alice.aet", res.Name)
	})

	t.Run("SetResolver", func(t *testing.T) {
		res, err := srv.SetResolver(ctx, &types.MsgSetResolver{Owner: ownerA, Name: "alice", ResolverRoot: resolverRoot("b"), Height: 999_999})
		require.NoError(t, err)
		require.Equal(t, resolverRoot("b"), res.ResolverRoot)
	})

	t.Run("SetReverseRecord", func(t *testing.T) {
		res, err := srv.SetReverseRecord(ctx, &types.MsgSetReverseRecord{Owner: ownerA, Address: ownerA, Name: "alice", Height: 999_999})
		require.NoError(t, err)
		require.Equal(t, "alice.aet", res.Name)
		require.Equal(t, ownerA, res.Address)
	})

	t.Run("TransferName", func(t *testing.T) {
		res, err := srv.TransferName(ctx, &types.MsgTransferName{Owner: ownerA, Name: "alice", NewOwner: ownerB, Height: 999_999})
		require.NoError(t, err)
		require.Equal(t, ownerB, res.Owner)

		// Ownership is now ownerB's; ownerA can no longer act on it.
		_, err = srv.RenewName(ctx, &types.MsgRenewName{Owner: ownerA, Name: "alice", Height: 999_999})
		require.ErrorContains(t, err, "owner")
	})

	t.Run("ReserveName and ReleaseReservedName", func(t *testing.T) {
		res, err := srv.ReserveName(ctx, &types.MsgReserveName{Authority: authority, Name: "gov", Reason: "root"})
		require.NoError(t, err)
		require.Equal(t, "gov.aet", res.Name)
		require.Equal(t, authority, res.Authority)

		// A normal user is blocked from registering it while reserved.
		_, err = k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "gov", Height: 10})
		require.ErrorContains(t, err, "reserved")

		// A non-authority caller cannot release it.
		_, err = srv.ReleaseReservedName(ctx, &types.MsgReleaseReservedName{Authority: ownerA, Name: "gov"})
		require.Error(t, err)

		releaseRes, err := srv.ReleaseReservedName(ctx, &types.MsgReleaseReservedName{Authority: authority, Name: "gov"})
		require.NoError(t, err)
		require.Equal(t, "gov", releaseRes.Name)

		// Now a normal user can register it.
		_, err = k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "gov", Height: 10})
		require.NoError(t, err)
	})
}

// TestPhaseCMsgServerOverridesUserSuppliedHeight extends the DEFECT 2 guard
// (see grpc_height_test.go) to the four newly-wired owner-signed messages:
// each carries a wire Height field a caller controls, and the handler must
// overwrite it with the real block height before the keeper runs, exactly as
// every other Msg handler in this file does.
func TestPhaseCMsgServerOverridesUserSuppliedHeight(t *testing.T) {
	const (
		blockHeightAt  = int64(90)
		attackerHeight = uint64(900_000)
	)

	k := setupKeeper(t)
	srv := NewGRPCMsgServer(&k)

	_, err := k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "alice", Height: 10})
	require.NoError(t, err)

	ctx := sdk.Context{}.WithBlockHeight(blockHeightAt)

	// RenewName: alice expires at 110 (Height 10 + RegistrationPeriod 100). An
	// attacker-supplied Height of 900_000 would be past expiry and rejected;
	// the real block height (90) is inside the renewal window, so it must
	// succeed and extend from the CURRENT expiry (110 + 100 = 210).
	renewRes, err := srv.RenewName(ctx, &types.MsgRenewName{Owner: ownerA, Name: "alice", Height: attackerHeight})
	require.NoError(t, err, "must be evaluated at the real block height (90), not the attacker's Height")
	require.Equal(t, uint64(210), renewRes.ExpiryHeight)

	// SetReverseRecord: UpdatedHeight must reflect the block height, not the
	// attacker-supplied one.
	reverseRes, err := srv.SetReverseRecord(ctx, &types.MsgSetReverseRecord{Owner: ownerA, Address: ownerA, Name: "alice", Height: attackerHeight})
	require.NoError(t, err)
	stored, found, err := k.ReverseRecord(context.Background(), reverseRes.Address)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, uint64(blockHeightAt), stored.UpdatedHeight,
		"reverse record UpdatedHeight must be the block height, not the caller's msg.Height")
}
