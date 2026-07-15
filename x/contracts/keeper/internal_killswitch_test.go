package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/contracts/types"
)

// TestInternalMessageRejectedWhenModuleDisabled is the regression guard for
// SEC-LOW: kill-switch not enforced on internal handlers. After governance
// disables the contracts module (Params.Enabled=false), the publicly-routable
// internal-message path must also stop executing, matching StoreCode /
// instantiate / external-execute.
func TestInternalMessageRejectedWhenModuleDisabled(t *testing.T) {
	k := NewKeeper()
	k.genesis.Params.Enabled = false

	msg := types.MsgSendInternalMessage{
		Height: 5,
		Message: types.InternalMessage{
			SourceContractUser: aeAddress("11"),
			DestinationAccount: aeAddress("22"),
			Height:             5,
		},
	}
	_, err := k.SendInternalMessage(msg)
	require.ErrorContains(t, err, "module disabled")

	_, err = k.ExecuteInternal(types.MsgExecuteInternal{Height: 5, Message: msg.Message})
	require.ErrorContains(t, err, "module disabled")
}

// TestContractLifecycleOpsRejectedWhenModuleDisabled covers SA2 #8: the newer
// top-up / pay-storage-debt / unfreeze entry points must also honor the
// kill-switch, matching the other mutating handlers.
func TestContractLifecycleOpsRejectedWhenModuleDisabled(t *testing.T) {
	k := NewKeeper()
	k.genesis.Params.Enabled = false

	_, err := k.TopUpContract(types.MsgTopUpContract{Sender: aeAddress("11"), ContractAddress: aeAddress("22"), Amount: 1, Height: 5})
	require.ErrorContains(t, err, "module disabled")

	_, err = k.PayContractStorageDebt(types.MsgPayContractStorageDebt{Sender: aeAddress("11"), ContractAddress: aeAddress("22"), Amount: 1, Height: 5})
	require.ErrorContains(t, err, "module disabled")

	_, err = k.UnfreezeContract(types.MsgUnfreezeContract{Sender: aeAddress("11"), ContractAddress: aeAddress("22"), Height: 5})
	require.ErrorContains(t, err, "module disabled")
}
