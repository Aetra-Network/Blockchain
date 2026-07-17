package app

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	identityrootkeeper "github.com/sovereign-l1/l1/x/identity-root/keeper"
	identityroottypes "github.com/sovereign-l1/l1/x/identity-root/types"
)

func TestIdentityRootSystemModuleWiringAndGenesis(t *testing.T) {
	app, genesis := setup(true, 5)
	_ = genesis

	require.NoError(t, app.ValidateAetraCoreWiringGate())
	require.Contains(t, app.ModuleManager.Modules, identityroottypes.ModuleName)
	require.Contains(t, app.keys, identityroottypes.StoreKey)
	require.Contains(t, genesis, identityroottypes.ModuleName)
	// ANS Phase A graduated identity-root to a system module with a real bank
	// module account (the .aet collection custody), so it now HAS a maccperm
	// entry -- and the wiring gate accepts it precisely because it is a reserved
	// system module account.
	require.Contains(t, GetMaccPerms(), identityroottypes.ModuleName)
	require.True(t, IsReservedSystemModuleAccountName(identityroottypes.ModuleName))

	var gs identityrootkeeper.GenesisState
	require.NoError(t, json.Unmarshal(genesis[identityroottypes.ModuleName], &gs))
	require.NoError(t, gs.Validate())
	// The default (mainnet) genesis ships the module disabled behind the
	// prototype gate, exactly like x/contracts ships contracts-off; testnet
	// genesis enables it (see cmd/l1d/cmd/testnet_genesis.go).
	require.False(t, gs.Params.Enabled)
	require.Empty(t, gs.State.Records)
	require.Equal(t, identityroottypes.DefaultRootNamespace, gs.IdentityParams.RootNamespace)
	// Genesis carries the full price table.
	require.NotEmpty(t, gs.IdentityParams.PriceTable)
	require.Equal(t, uint32(3), gs.IdentityParams.PriceTable[0].MinLabelLen)
}
