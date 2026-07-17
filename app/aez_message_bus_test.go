package app

import (
	"encoding/json"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/stretchr/testify/require"

	sims "github.com/cosmos/cosmos-sdk/testutil/sims"

	aeztypes "github.com/sovereign-l1/l1/x/aez/types"
)

// busPrefixEnd returns the exclusive upper bound for a key prefix scan (the
// prefix with its last non-0xff byte incremented), so an iterator over
// [prefix, busPrefixEnd(prefix)) visits exactly that prefix.
func busPrefixEnd(prefix []byte) []byte {
	end := append([]byte(nil), prefix...)
	for i := len(end) - 1; i >= 0; i-- {
		end[i]++
		if end[i] != 0 {
			return end[:i+1]
		}
	}
	return nil
}

// TestAEZMessageBusInertInRealBlockLifecycle is the Phase 4 inertness proof at
// the app level: the wired BeginBlocker (routing swap + message-bus drain) runs
// on every real FinalizeBlock, and under the default single-zone genesis it
// produces NO state. With all 256 buckets on zone 0, every sender and recipient
// resolve to the same zone, so no ZoneMessage is ever produced and the drain is a
// no-op -- behaviour stays bit-identical to a build without the bus.
//
// Complements the keeper-level TestBusIsInertUnderOneZone (which snapshots the
// store) by proving the same through the module manager on committed state.
func TestAEZMessageBusInertInRealBlockLifecycle(t *testing.T) {
	app, _ := setup(true, 5)
	genesis := GenesisStateWithSingleValidator(t, app)

	stateBytes, err := json.MarshalIndent(genesis, "", " ")
	require.NoError(t, err)
	_, err = app.InitChain(&abci.RequestInitChain{
		Validators:      []abci.ValidatorUpdate{},
		ConsensusParams: sims.DefaultConsensusParams,
		AppStateBytes:   stateBytes,
	})
	require.NoError(t, err)

	// Drive several real blocks: the aez BeginBlocker (swap + drain) runs each.
	for height := int64(1); height <= 6; height++ {
		_, err = app.FinalizeBlock(&abci.RequestFinalizeBlock{Height: height, Hash: app.LastCommitID().Hash})
		require.NoError(t, err, "the wired aez drain must never fail a block under one zone (I-23)")
		_, err = app.Commit()
		require.NoError(t, err)
	}

	ctx := app.NewUncachedContext(false, cmtproto.Header{Height: 6})

	// The routing table is still core-only and every entity still routes to zone
	// 0 -- so the cross-zone branch that produces a message is unreachable.
	table, err := app.AEZKeeper.GetRoutingTable(ctx)
	require.NoError(t, err)
	for i := 0; i < aeztypes.BucketCount; i++ {
		require.True(t, table.Buckets[i].IsCore(), "bucket %d left the core zone", i)
	}

	// Not one key exists under any of the four message-bus prefixes: no outbox,
	// no sequence counter, no inbox, no processed marker. The bus is latent
	// code plus empty key-space, nothing more.
	store := ctx.KVStore(app.keys[aeztypes.StoreKey])
	for _, prefix := range [][]byte{
		aeztypes.OutboxPrefix,
		aeztypes.OutboxSeqPrefix,
		aeztypes.InboxPrefix,
		aeztypes.ProcessedPrefix,
		aeztypes.InboxCountKey,
	} {
		it := store.Iterator(prefix, busPrefixEnd(prefix))
		require.False(t, it.Valid(), "aez bus prefix %x must be empty under one zone", prefix)
		require.NoError(t, it.Close())
	}
}
