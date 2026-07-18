package keeper_test

import (
	"bytes"
	"testing"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	l1app "github.com/sovereign-l1/l1/app"
	feeskeeper "github.com/sovereign-l1/l1/x/fees/keeper"
	"github.com/sovereign-l1/l1/x/fees/types"
)

// admitRun captures everything about a run of AdmitTx over a fees-store branch
// that could move the AppHash: the metered gas consumed, the accept/reject
// outcome per tx, and a byte-exact dump of the fees KV store afterwards.
type admitRun struct {
	gasConsumed	uint64
	errs		[]string
	dump		[][2][]byte
}

// TestZoneResolverIsBitIdenticalOnSingleZone is the load-bearing Phase 6
// inertness proof at the keeper level.
//
// With the default (single-zone) genesis EVERY sender resolves to the Core Zone,
// whose cap is 0 (uncapped). This test runs an identical sequence of AdmitTx
// calls twice against branches of the same committed state -- once with the REAL
// wired resolver (&app.AEZKeeper) and once with NO resolver (the pre-Phase-6
// behaviour) -- and asserts three things that together mean the AppHash cannot
// move while the chain is single-zone:
//
//  1. identical metered gas consumed  -> the resolver's routing-table reads run
//     on an infinite-meter child ctx and never inflate gasUsed (which would
//     otherwise feed the block gas meter and the committed congestion bps);
//  2. identical accept/reject outcome -> the per-zone gate is skipped for Core;
//  3. byte-identical fees KV store    -> no per-zone counter key is ever written
//     for a Core tx (the height-keyed counter is untouched).
func TestZoneResolverIsBitIdenticalOnSingleZone(t *testing.T) {
	app := l1app.Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(1)

	// A normal 20-byte user address: resolves through the bucket hash + routing
	// table (so ZoneOfAddress actually reads the aez store) and lands in zone 0
	// under the single-zone genesis.
	sender := sdk.AccAddress(bytes.Repeat([]byte{0x07}, 20))
	fee := sdk.NewCoins(sdk.NewCoin(types.BondDenom, requiredFullFee(t, 100_000, 0)))
	txs := make([]feeTx, 0, 6)
	for i := 0; i < 6; i++ {
		txs = append(txs, feeTx{fees: fee, payer: sender, gas: 100_000})
	}

	withResolver := runAdmitSequence(t, app, ctx, app.FeesKeeper, sender, txs)
	noResolver := runAdmitSequence(t, app, ctx, app.FeesKeeper.WithZoneResolver(nil), sender, txs)

	require.Equal(t, noResolver.errs, withResolver.errs, "admission decisions must match")
	require.Equal(t, noResolver.gasConsumed, withResolver.gasConsumed, "resolver reads must be gas-neutral on the metered meter")
	require.Equal(t, len(noResolver.dump), len(withResolver.dump), "fees store key count must match")
	require.Equal(t, noResolver.dump, withResolver.dump, "fees store must be byte-identical (no per-zone key, no core write)")

	// And prove the resolver is really live and single-zone, so the equality
	// above is meaningful rather than vacuous.
	zone, err := app.AEZKeeper.ZoneOfAddress(ctx, validRawAddress(0x07))
	require.NoError(t, err)
	require.Equal(t, uint32(0), zone, "single-zone genesis must resolve this sender to Core")
}

// runAdmitSequence branches the committed context, installs a fresh bounded gas
// meter (so GasConsumed starts at 0), runs AdmitTx for each tx, and returns the
// metered gas, per-tx outcome, and a sorted dump of the fees store.
func runAdmitSequence(t *testing.T, app *l1app.L1App, ctx sdk.Context, k feeskeeper.Keeper, sender sdk.AccAddress, txs []feeTx) admitRun {
	t.Helper()
	branch, _ := ctx.CacheContext()
	branch = branch.WithGasMeter(storetypes.NewGasMeter(1_000_000_000))

	run := admitRun{}
	for _, tx := range txs {
		_, err := k.AdmitTx(branch, tx, sender, false)
		if err != nil {
			run.errs = append(run.errs, err.Error())
		} else {
			run.errs = append(run.errs, "")
		}
	}
	run.gasConsumed = branch.GasMeter().GasConsumed()
	run.dump = dumpStore(branch.KVStore(app.GetKey(types.StoreKey)))
	return run
}

// dumpStore returns every key/value in the store in ascending key order.
func dumpStore(store storetypes.KVStore) [][2][]byte {
	var out [][2][]byte
	it := store.Iterator(nil, nil)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		k := append([]byte(nil), it.Key()...)
		v := append([]byte(nil), it.Value()...)
		out = append(out, [2][]byte{k, v})
	}
	return out
}
