package app

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	feecollectortypes "github.com/sovereign-l1/l1/x/fee-collector/types"
	storagerentkeeper "github.com/sovereign-l1/l1/x/storage-rent/keeper"
	storagerenttypes "github.com/sovereign-l1/l1/x/storage-rent/types"
)

func TestStorageRentPrototypeModuleWiringAndGenesis(t *testing.T) {
	app, genesis := setup(true, 5)
	_ = genesis

	require.NoError(t, app.ValidateAetraCoreWiringGate())
	require.Contains(t, app.ModuleManager.Modules, storagerenttypes.ModuleName)
	require.Contains(t, app.keys, storagerenttypes.StoreKey)
	require.Contains(t, genesis, storagerenttypes.ModuleName)
	// The module custodies nothing: it only ever moves rent from the payer to
	// the feecollector_storage_rent_reserve bucket, which is what the
	// AETStorageRent catalog entry names as its custodian. So it must have no
	// module account of its own.
	require.NotContains(t, GetMaccPerms(), storagerenttypes.ModuleName)
	require.Contains(t, GetMaccPerms(), feecollectortypes.StorageRentReserveModuleName)

	var gs storagerentkeeper.GenesisState
	require.NoError(t, json.Unmarshal(genesis[storagerenttypes.ModuleName], &gs))
	require.NoError(t, gs.Validate())
	require.False(t, gs.Params.Enabled)
	require.Empty(t, gs.State.Contracts)
	require.Empty(t, gs.State.Distributions)
}
