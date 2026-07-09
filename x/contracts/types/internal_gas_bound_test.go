package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestInternalMessageValidateBasicBoundsGasLimit is the regression guard for
// SEC-CRIT: uncapped AVM gas on internal messages. MsgSendInternalMessage and
// MsgExecuteInternal are permissionless, so an unbounded gas limit would let a
// hostile contract loop the AVM interpreter effectively forever and halt the
// chain. ValidateBasic must reject any gas limit above the per-execution cap.
func TestInternalMessageValidateBasicBoundsGasLimit(t *testing.T) {
	params := DefaultParams()
	base := InternalMessage{
		SourceContractUser: contractAPIAddress(0x11),
		DestinationAccount: contractAPIAddress(0x22),
		Height:             10,
	}

	// At the cap (and 0 == "use default") is accepted.
	for _, gas := range []uint64{0, params.MaxGasPerExecution} {
		msg := base
		msg.GasLimit = gas
		require.NoError(t, MsgSendInternalMessage{Message: msg, Height: 10}.ValidateBasic(params))
		require.NoError(t, MsgExecuteInternal{Message: msg, Height: 10}.ValidateBasic(params))
	}

	// Above the cap — including a pathological near-uint64-max value — is rejected.
	for _, gas := range []uint64{params.MaxGasPerExecution + 1, 1 << 62} {
		msg := base
		msg.GasLimit = gas
		require.Error(t, MsgSendInternalMessage{Message: msg, Height: 10}.ValidateBasic(params),
			"send internal must reject gas %d", gas)
		require.Error(t, MsgExecuteInternal{Message: msg, Height: 10}.ValidateBasic(params),
			"execute internal must reject gas %d", gas)
	}
}
