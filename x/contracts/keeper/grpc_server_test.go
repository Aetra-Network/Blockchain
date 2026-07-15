package keeper

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/contracts/types"
)

// TestGRPCMsgServerTopUpPayDebtUnfreezeHappyPath exercises the grpcMsgServer
// wrappers for TopUpContract/PayContractStorageDebt/UnfreezeContract end to
// end, mirroring TestFrozenContractRecoveryKeepsCodeDataAndBalance in
// keeper_test.go but driven through the gRPC dispatch layer (grpc_server.go)
// instead of calling the keeper's *State methods directly, so a regression in
// the handler wiring itself (nil-check, loadForBlock, response wrapping) is
// caught even though the underlying keeper business logic is already covered
// there.
func TestGRPCMsgServerTopUpPayDebtUnfreezeHappyPath(t *testing.T) {
	wallet := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})
	codeHash := storeContractCode(t, &k, wallet)
	created := instantiateContract(t, &k, wallet, codeHash, "grpc-lifecycle", 10, 1, 100)

	ctx := context.Background()
	srv := NewGRPCMsgServer(&k)

	// Exhaust storage rent to accrue a debt and freeze the contract, the same
	// way TestFrozenContractRecoveryKeepsCodeDataAndBalance does.
	_, err := k.ExecuteContract(types.MsgExecuteContract{Sender: wallet, ContractAddress: created.ContractAddressUser, Msg: []byte("too late"), Height: 200})
	require.ErrorContains(t, err, types.ErrStorageRent)

	frozen, err := k.Contract(types.QueryContractRequest{ContractAddress: created.ContractAddressUser})
	require.NoError(t, err)
	require.True(t, frozen.Found)
	require.Equal(t, types.ContractStatusFrozen, frozen.Contract.Status)
	require.NotZero(t, frozen.Contract.StorageRentDebt)

	toppedResp, err := srv.TopUpContract(ctx, &types.MsgTopUpContract{
		Sender:          wallet,
		ContractAddress: created.ContractAddressUser,
		Amount:          frozen.Contract.StorageRentDebt + 50,
		Height:          201,
	})
	require.NoError(t, err)
	require.Equal(t, codeHash, toppedResp.Contract.CodeID)
	require.Greater(t, toppedResp.Contract.Balance, frozen.Contract.Balance)

	paidResp, err := srv.PayContractStorageDebt(ctx, &types.MsgPayContractStorageDebt{
		Sender:          wallet,
		ContractAddress: created.ContractAddressUser,
		Amount:          frozen.Contract.StorageRentDebt,
		Height:          202,
	})
	require.NoError(t, err)
	require.Zero(t, paidResp.Contract.StorageRentDebt)

	unfrozenResp, err := srv.UnfreezeContract(ctx, &types.MsgUnfreezeContract{
		Sender:          wallet,
		ContractAddress: created.ContractAddressUser,
		Height:          203,
	})
	require.NoError(t, err)
	require.Equal(t, types.ContractStatusActive, unfrozenResp.Contract.Status)
	require.Equal(t, codeHash, unfrozenResp.Contract.CodeID)
	require.NotZero(t, unfrozenResp.Contract.Balance)
}

// TestGRPCMsgServerContractLifecycleNilRequestRejected covers the nil-check
// guard each of the 3 new handlers does before touching the keeper, matching
// the shape every other grpcMsgServer handler in this file already uses
// (see e.g. StoreCode/UpdateContractParams above).
func TestGRPCMsgServerContractLifecycleNilRequestRejected(t *testing.T) {
	k := NewKeeper()
	srv := NewGRPCMsgServer(&k)
	ctx := context.Background()

	cases := []struct {
		name string
		call func() (any, error)
		want string
	}{
		{
			name: "TopUpContract",
			call: func() (any, error) { return srv.TopUpContract(ctx, nil) },
			want: "empty contracts top-up request",
		},
		{
			name: "PayContractStorageDebt",
			call: func() (any, error) { return srv.PayContractStorageDebt(ctx, nil) },
			want: "empty contracts storage debt payment request",
		},
		{
			name: "UnfreezeContract",
			call: func() (any, error) { return srv.UnfreezeContract(ctx, nil) },
			want: "empty contracts unfreeze request",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.call()
			require.ErrorContains(t, err, tc.want)
		})
	}
}

// TestGRPCMsgServerContractLifecycleUnknownContractRejected covers the
// handlers' delegation to the keeper's contract-not-found error path for an
// address that was never deployed.
func TestGRPCMsgServerContractLifecycleUnknownContractRejected(t *testing.T) {
	wallet := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})
	srv := NewGRPCMsgServer(&k)
	ctx := context.Background()
	const missing = "AEnonexistentcontractaddress00000000000000000000000000000"

	t.Run("TopUpContract", func(t *testing.T) {
		_, err := srv.TopUpContract(ctx, &types.MsgTopUpContract{Sender: wallet, ContractAddress: missing, Amount: 1, Height: 1})
		require.ErrorContains(t, err, types.ErrContractNotFound)
	})
	t.Run("PayContractStorageDebt", func(t *testing.T) {
		_, err := srv.PayContractStorageDebt(ctx, &types.MsgPayContractStorageDebt{Sender: wallet, ContractAddress: missing, Amount: 1, Height: 1})
		require.ErrorContains(t, err, types.ErrContractNotFound)
	})
	t.Run("UnfreezeContract", func(t *testing.T) {
		_, err := srv.UnfreezeContract(ctx, &types.MsgUnfreezeContract{Sender: wallet, ContractAddress: missing, Height: 1})
		require.ErrorContains(t, err, types.ErrContractNotFound)
	})
}
