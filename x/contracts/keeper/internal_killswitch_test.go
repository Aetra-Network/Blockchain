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
