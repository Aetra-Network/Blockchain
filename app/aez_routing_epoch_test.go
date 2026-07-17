package app

import (
	"encoding/json"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/stretchr/testify/require"

	sims "github.com/cosmos/cosmos-sdk/testutil/sims"

	aezkeeper "github.com/sovereign-l1/l1/x/aez/keeper"
	aeztypes "github.com/sovereign-l1/l1/x/aez/types"
)

// TestAEZRoutingTableSwapsInARealBlockLifecycle is the AEZ Phase 2 live proof.
//
// Every other Phase 2 test calls the keeper or the Msg handler directly. None of
// them proves the thing the promotion to systemModules actually bought: that the
// MODULE MANAGER calls x/aez's BeginBlocker on a real block. A module can
// implement appmodule.HasBeginBlocker perfectly and still never run if it is
// missing from OrderBeginBlockers or wired as the wrong family -- and that
// failure is invisible to keeper tests, which call BeginBlocker themselves.
//
// So this drives real FinalizeBlock calls and asserts the table swaps at exactly
// its activation height, from nothing but the passage of blocks.
//
// The default routing epoch is 10,000 blocks, which is not runnable here, so
// genesis is built with a short epoch. That is a change to a genesis PARAM, not
// to the rule under test: the boundary arithmetic
// (types.IsRoutingEpochBoundary) is identical at 5 and at 10,000.
func TestAEZRoutingTableSwapsInARealBlockLifecycle(t *testing.T) {
	const shortEpoch = uint64(5)

	app, _ := setup(true, 5)
	genesis := GenesisStateWithSingleValidator(t, app)

	aezGenesis := aeztypes.DefaultGenesis()
	aezGenesis.Params.RoutingEpochLength = shortEpoch
	require.NoError(t, aezGenesis.Validate())
	rawAEZ, err := json.Marshal(aezGenesis)
	require.NoError(t, err)
	genesis[aeztypes.ModuleName] = rawAEZ

	stateBytes, err := json.MarshalIndent(genesis, "", " ")
	require.NoError(t, err)
	_, err = app.InitChain(&abci.RequestInitChain{
		Validators:		[]abci.ValidatorUpdate{},
		ConsensusParams:	sims.DefaultConsensusParams,
		AppStateBytes:		stateBytes,
	})
	require.NoError(t, err)

	// InitChain stages genesis but does not commit it: run block 1 first so
	// the routing table is readable from committed state.
	_, err = app.FinalizeBlock(&abci.RequestFinalizeBlock{Height: 1, Hash: app.LastCommitID().Hash})
	require.NoError(t, err)
	_, err = app.Commit()
	require.NoError(t, err)

	// Stage a table through the real Msg handler, with the real authority.
	// The bucket map is identical to genesis (all core): the core zone is a
	// one-way trap, so this is the only table governance can stage today.
	stageCtx := app.NewUncachedContext(false, cmtproto.Header{Height: 2})
	current, err := app.AEZKeeper.GetRoutingTable(stageCtx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), current.Version)

	_, err = aezkeeper.NewMsgServerImpl(&app.AEZKeeper).UpdateRoutingTable(stageCtx, &aeztypes.MsgUpdateRoutingTable{
		Authority:		aeztypes.GovAuthority(),
		Version:		2,
		Epoch:			1,
		ActivationHeight:	int64(shortEpoch),
		Buckets:		aeztypes.BucketsFromTable(current),
	})
	require.NoError(t, err)

	// Blocks 2..shortEpoch-1: the BeginBlocker runs every block and must not
	// swap.
	for height := int64(2); height < int64(shortEpoch); height++ {
		_, err = app.FinalizeBlock(&abci.RequestFinalizeBlock{Height: height, Hash: app.LastCommitID().Hash})
		require.NoError(t, err)
		_, err = app.Commit()
		require.NoError(t, err)

		ctx := app.NewUncachedContext(false, cmtproto.Header{Height: height})
		table, err := app.AEZKeeper.GetRoutingTable(ctx)
		require.NoError(t, err)
		require.Equal(t, uint64(1), table.Version, "table swapped early, at height %d", height)
	}

	// The activation height itself.
	_, err = app.FinalizeBlock(&abci.RequestFinalizeBlock{Height: int64(shortEpoch), Hash: app.LastCommitID().Hash})
	require.NoError(t, err)
	_, err = app.Commit()
	require.NoError(t, err)

	ctx := app.NewUncachedContext(false, cmtproto.Header{Height: int64(shortEpoch)})
	table, err := app.AEZKeeper.GetRoutingTable(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(2), table.Version, "the module manager did not run x/aez's BeginBlocker at the boundary")

	// Pending is cleared, so the swap cannot repeat.
	_, found, err := app.AEZKeeper.GetPendingVersion(ctx)
	require.NoError(t, err)
	require.False(t, found)

	// The whole point of Phase 2's core-zone trap: after a real governance
	// swap on a real chain, every entity still resolves to the core zone.
	// Nothing routes anywhere.
	for _, ns := range aeztypes.AllNamespaces() {
		for i := 0; i < 16; i++ {
			zone, err := app.AEZKeeper.ZoneOf(ctx, ns, []byte{byte(i), 0x5a})
			require.NoError(t, err)
			require.Equal(t, aeztypes.ZoneIDCore, zone)
		}
	}
}
