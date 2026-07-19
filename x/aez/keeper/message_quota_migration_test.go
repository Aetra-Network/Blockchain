package keeper

// message_quota_migration_test.go proves the Phase 6b migration-safety
// guarantee (docs/architecture/aez-throughput-preservation-design.md §5.4,
// Layer 2): an OLD-SHAPE Params blob -- what a chain that upgraded its
// BINARY but has not yet had anything call SetParams since would still hold
// committed, because json.Unmarshal does not error on a missing field --
// must drain EXACTLY like today's pre-Phase-6b single-global-budget
// algorithm: not panic, not error, and not silently cap every zone at zero
// (which a naive read of the resulting Go zero-value MessageQuota would
// otherwise produce).

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/aez/types"
	"github.com/sovereign-l1/l1/x/internal/kvtest"
)

// writeOldShapeParams writes params to the store's ParamsKey with the
// MessageQuota field REMOVED from the JSON entirely -- not merely
// zero-valued -- reproducing exactly what a pre-Phase-6b binary would have
// committed to the store. It bypasses Keeper.SetParams (which would reject
// a zero-value MessageQuota via Validate()) on purpose: the hazard under
// test is precisely the window BEFORE anything calls SetParams again after a
// binary upgrade.
func writeOldShapeParams(t *testing.T, k Keeper, ctx context.Context, params types.Params) {
	t.Helper()
	bz, err := json.Marshal(params)
	require.NoError(t, err)

	var asMap map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(bz, &asMap))
	_, hadField := asMap["MessageQuota"]
	require.True(t, hadField, "test setup: current Params must actually carry a MessageQuota field to strip")
	delete(asMap, "MessageQuota")

	oldShapeBz, err := json.Marshal(asMap)
	require.NoError(t, err)

	store := k.storeService.OpenKVStore(ctx)
	require.NoError(t, store.Set(types.ParamsKey, oldShapeBz))
}

// TestGetParamsReadsOldShapeBlobWithMessageQuotaAsZeroValue proves the exact
// JSON-level mechanics the design doc's §5.4 hazard depends on: reading back
// an old-shape blob succeeds (no error), and the missing field silently
// becomes the Go zero value, which fails Validate() -- exactly the signal
// DrainWith uses to choose the legacy fallback branch rather than trusting a
// "every zone capped at zero" MessageQuota.
func TestGetParamsReadsOldShapeBlobWithMessageQuotaAsZeroValue(t *testing.T) {
	svc := kvtest.NewStoreService()
	k := NewPersistentKeeper(svc)
	ctx := busCtx(1)
	require.NoError(t, k.InitGenesisState(ctx, types.DefaultGenesis()))
	params := types.DefaultParams()
	params.Prototype.Enabled = true
	writeOldShapeParams(t, k, ctx, params)

	got, err := k.GetParams(ctx)
	require.NoError(t, err, "an old-shape blob must still unmarshal without error")
	require.Equal(t, types.MessageQuotaParams{}, got.MessageQuota, "the missing field must read back as the Go zero value")
	require.Error(t, got.MessageQuota.Validate(), "the zero-value MessageQuota must fail Validate")

	// The REST of Params is unaffected: an old-shape blob's other fields
	// round-trip normally.
	require.True(t, got.Prototype.Enabled)
	require.Equal(t, params.GasQuota, got.GasQuota)
}

// TestDrainFallsBackToLegacyBudgetForOldShapeParamsBlob is the full
// end-to-end migration-safety proof: with an old-shape blob committed (no
// MessageQuota field at all), DrainWith must neither panic nor error, and
// must reproduce today's EXACT pre-Phase-6b numbers -- a single shared
// 8,000,000 budget, canonical id order, stop (not skip) on the first
// over-budget message -- for the identical 9-message/single-zone scenario
// TestDrainBudgetStopsAndResumes already proves for the CURRENT (populated)
// MessageQuota. The two tests' identical expected counts (8 then 9) are the
// point: an operator who upgrades the binary and does nothing else observes
// no behavioural change at all, indefinitely.
func TestDrainFallsBackToLegacyBudgetForOldShapeParamsBlob(t *testing.T) {
	svc := kvtest.NewStoreService()
	k := NewPersistentKeeper(svc)
	ctx := busCtx(1)
	require.NoError(t, k.InitGenesisState(ctx, types.DefaultGenesis()))

	params := types.DefaultParams()
	params.Prototype.Enabled = true
	params.Prototype.TestnetProfile = true
	writeOldShapeParams(t, k, ctx, params)

	sender, recipient := twoDistinctBucketAddrs(t)
	installTable(t, k, 1, 2, 10000, twoZoneRecipientElsewhere(t, recipient))

	const perMsg = types.MaxGasPerDelivery
	enqueued := map[string]bool{}
	for i := 0; i < 9; i++ {
		msg, produced, err := k.EnqueueMessage(busCtx(10000), EnqueueRequest{
			SenderKind:    types.EntityKindAddress,
			Sender:        sender,
			RecipientKind: types.EntityKindAddress,
			Recipient:     recipient,
			Payload:       []byte{byte(i)},
			GasLimit:      perMsg,
		})
		require.NoError(t, err)
		require.True(t, produced)
		enqueued[string(msg.ID)] = true
	}

	rec := &recorder{}
	require.NoError(t, k.DrainWith(busCtx(10001), rec.deliver),
		"an old-shape params blob must never panic or fail the block")
	require.Len(t, rec.calls, 8, "the legacy fallback must reproduce today's exact 8,000,000/1,000,000 = 8 admissions")

	require.NoError(t, k.DrainWith(busCtx(10002), rec.deliver))
	require.Len(t, rec.calls, 9, "the 9th message, held over, still delivers -- nothing is dropped under the fallback either")

	delivered := map[string]bool{}
	for _, c := range rec.calls {
		require.False(t, delivered[string(c.id)], "no message delivered twice")
		delivered[string(c.id)] = true
	}
	require.Equal(t, enqueued, delivered, "every enqueued message delivered exactly once under the fallback")
}
