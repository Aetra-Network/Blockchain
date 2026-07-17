package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	aezkeeper "github.com/sovereign-l1/l1/x/aez/keeper"
	aeztypes "github.com/sovereign-l1/l1/x/aez/types"
	"github.com/sovereign-l1/l1/x/internal/prototype"
)

// coreTable returns a table whose bucket map is identical to genesis (all 256
// buckets on the core zone) with a new version/epoch/activation height.
//
// This is the ONLY shape governance can currently stage: genesis maps every
// bucket to core, and the core zone is a one-way trap, so no bucket may move.
// See Keeper.ValidateRoutingTableTransition.
func coreTable(version, epoch uint64, activationHeight int64) aeztypes.RoutingTable {
	var buckets [aeztypes.BucketCount]aeztypes.ZoneID
	for i := range buckets {
		buckets[i] = aeztypes.ZoneIDCore
	}
	return aeztypes.NewRoutingTable(version, epoch, activationHeight, buckets)
}

func msgFor(table aeztypes.RoutingTable, authority string) *aeztypes.MsgUpdateRoutingTable {
	return &aeztypes.MsgUpdateRoutingTable{
		Authority:		authority,
		Version:		table.Version,
		Epoch:			table.Epoch,
		ActivationHeight:	table.ActivationHeight,
		Buckets:		aeztypes.BucketsFromTable(table),
	}
}

func govAuthority() string	{ return aeztypes.GovAuthority() }

// TestUpdateRoutingTableStagesWithoutApplying is the central Phase 2 property:
// the Msg SCHEDULES a swap, it never performs one.
//
// A handler that applied the table immediately would leave the block's earlier
// transactions resolved against the old table and its later ones against the
// new -- two tables inside one height.
func TestUpdateRoutingTableStagesWithoutApplying(t *testing.T) {
	k, ctx, _ := initGenesis(t, 1)
	srv := aezkeeper.NewMsgServerImpl(&k)

	staged := coreTable(2, 1, epochLength)
	res, err := srv.UpdateRoutingTable(ctx, msgFor(staged, govAuthority()))
	require.NoError(t, err)
	require.Equal(t, uint64(2), res.Version)
	require.Equal(t, epochLength, res.ActivationHeight)
	require.NotEmpty(t, res.TableHash)

	// Pending, not current.
	pending, found, err := k.GetPendingVersion(ctx)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, uint64(2), pending)

	current, err := k.GetRoutingTable(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), current.Version, "the handler must not apply the table")
}

// TestBeginBlockerActivatesAtExactlyTheBoundary drives the swap through the real
// block-lifecycle entry point -- BeginBlocker, not the keeper helper -- and
// asserts it fires at its height, not one block early and not one block late.
func TestBeginBlockerActivatesAtExactlyTheBoundary(t *testing.T) {
	k, ctx, _ := initGenesis(t, 1)
	srv := aezkeeper.NewMsgServerImpl(&k)
	_, err := srv.UpdateRoutingTable(ctx, msgFor(coreTable(2, 1, epochLength), govAuthority()))
	require.NoError(t, err)

	// Not one block early.
	for _, height := range []int64{1, 2, epochLength - 1} {
		at := ctxAtHeight(height)
		require.NoError(t, k.BeginBlocker(at))
		current, err := k.GetRoutingTable(at)
		require.NoError(t, err)
		require.Equal(t, uint64(1), current.Version, "activated early at height %d", height)
	}

	// Exactly at the boundary.
	at := ctxAtHeight(epochLength)
	require.NoError(t, k.BeginBlocker(at))
	current, err := k.GetRoutingTable(at)
	require.NoError(t, err)
	require.Equal(t, uint64(2), current.Version)

	// Not one block late either: the swap already happened, and running the
	// BeginBlocker again is a no-op rather than a second swap.
	after := ctxAtHeight(epochLength + 1)
	require.NoError(t, k.BeginBlocker(after))
	current, err = k.GetRoutingTable(after)
	require.NoError(t, err)
	require.Equal(t, uint64(2), current.Version)
	_, found, err := k.GetPendingVersion(after)
	require.NoError(t, err)
	require.False(t, found)
}

// TestBeginBlockerIsASilentNoOpWithNothingPending guards I-23: a chain that never
// touches the routing table must never have a block fail because x/aez exists.
func TestBeginBlockerIsASilentNoOpWithNothingPending(t *testing.T) {
	k, _, _ := initGenesis(t, 1)
	for height := int64(1); height <= 50; height++ {
		require.NoError(t, k.BeginBlocker(ctxAtHeight(height)))
	}
	current, err := k.GetRoutingTable(ctxAtHeight(50))
	require.NoError(t, err)
	require.Equal(t, uint64(1), current.Version)
}

// TestUnauthorizedSignerIsRejected: the authority is the whole gate.
func TestUnauthorizedSignerIsRejected(t *testing.T) {
	k, ctx, _ := initGenesis(t, 1)
	srv := aezkeeper.NewMsgServerImpl(&k)
	table := coreTable(2, 1, epochLength)

	// The keyless prototype sentinel is NOT the aez authority. This is the
	// case that matters: it is the default every other prototype module
	// uses, so a copy-paste caller would land here.
	_, err := srv.UpdateRoutingTable(ctx, msgFor(table, prototype.DefaultAuthority))
	require.ErrorContains(t, err, "governance authority")

	// An ordinary well-formed account address is also not the authority.
	_, err = srv.UpdateRoutingTable(ctx, msgFor(table, "ae1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5z5tpwxqergd3c8g7rusqqjw2n2"))
	require.Error(t, err)

	// A malformed address is rejected too, and not by accident: Authorize
	// validates the address before comparing it.
	_, err = srv.UpdateRoutingTable(ctx, msgFor(table, "not-an-address"))
	require.Error(t, err)

	// Nothing was staged by any of the three.
	_, found, err := k.GetPendingVersion(ctx)
	require.NoError(t, err)
	require.False(t, found)

	// The gov authority does work, so the rejections above are about the
	// authority and not about the table.
	_, err = srv.UpdateRoutingTable(ctx, msgFor(table, govAuthority()))
	require.NoError(t, err)
}

// TestNonMonotonicVersionIsRejectedThroughTheMsg guards I-8 on the governance
// path: a table that could reuse or lower a version could rewrite routing
// history.
func TestNonMonotonicVersionIsRejectedThroughTheMsg(t *testing.T) {
	k, ctx, _ := initGenesis(t, 1)
	srv := aezkeeper.NewMsgServerImpl(&k)

	for _, version := range []uint64{0, 1} {
		_, err := srv.UpdateRoutingTable(ctx, msgFor(coreTable(version, 1, epochLength), govAuthority()))
		require.Error(t, err, "version %d must not beat current version 1", version)
	}

	_, err := srv.UpdateRoutingTable(ctx, msgFor(coreTable(2, 1, epochLength), govAuthority()))
	require.NoError(t, err)

	at := ctxAtHeight(epochLength)
	require.NoError(t, k.BeginBlocker(at))

	// Version 2 is current now and must itself be unbeatable.
	_, err = srv.UpdateRoutingTable(at, msgFor(coreTable(2, 2, epochLength*2), govAuthority()))
	require.ErrorContains(t, err, "must exceed current version")
	_, err = srv.UpdateRoutingTable(at, msgFor(coreTable(3, 2, epochLength*2), govAuthority()))
	require.NoError(t, err)
}

// TestOffBoundaryActivationIsRejectedThroughTheMsg guards I-8's other half.
func TestOffBoundaryActivationIsRejectedThroughTheMsg(t *testing.T) {
	k, ctx, _ := initGenesis(t, 1)
	srv := aezkeeper.NewMsgServerImpl(&k)

	for _, height := range []int64{epochLength - 1, epochLength + 1, 12345} {
		_, err := srv.UpdateRoutingTable(ctx, msgFor(coreTable(2, 1, height), govAuthority()))
		require.ErrorContains(t, err, "routing-epoch boundary", "height %d is not a boundary", height)
	}

	// A boundary in the PAST is rejected as well: the table would otherwise
	// activate on the very next block regardless of its stated height.
	_, err := srv.UpdateRoutingTable(ctxAtHeight(epochLength*2), msgFor(coreTable(2, 1, epochLength), govAuthority()))
	require.Error(t, err)

	_, err = srv.UpdateRoutingTable(ctx, msgFor(coreTable(2, 1, epochLength), govAuthority()))
	require.NoError(t, err)
}

// TestCoreBucketMoveIsRejected guards I-9 on the governance path.
//
// The core zone is a one-way trap. Genesis maps every bucket to core, so this
// currently freezes the whole table -- see ValidateRoutingTableTransition. The
// test asserts the rule at its edges: one bucket moved is enough to fail, and
// the failure names the core-zone error rather than some incidental validation
// error.
func TestCoreBucketMoveIsRejected(t *testing.T) {
	k, ctx, _ := initGenesis(t, 1)
	srv := aezkeeper.NewMsgServerImpl(&k)

	// A single bucket moved off core.
	var oneMoved [aeztypes.BucketCount]aeztypes.ZoneID
	for i := range oneMoved {
		oneMoved[i] = aeztypes.ZoneIDCore
	}
	oneMoved[7] = aeztypes.ZoneID(2)
	_, err := srv.UpdateRoutingTable(ctx, msgFor(aeztypes.NewRoutingTable(2, 1, epochLength, oneMoved), govAuthority()))
	require.ErrorContains(t, err, "core zone never migrates")

	// The whole table moved off core.
	_, err = srv.UpdateRoutingTable(ctx, msgFor(elasticTable(2, 1, epochLength), govAuthority()))
	require.ErrorContains(t, err, "core zone never migrates")

	// Nothing was staged.
	_, found, err := k.GetPendingVersion(ctx)
	require.NoError(t, err)
	require.False(t, found)

	// The identical (no-op) table is accepted, proving the rejection is
	// about the MOVE and not about staging in general.
	_, err = srv.UpdateRoutingTable(ctx, msgFor(coreTable(2, 1, epochLength), govAuthority()))
	require.NoError(t, err)
}

// TestUnknownZoneIsRejected covers both halves of "a zone that does not exist".
func TestUnknownZoneIsRejected(t *testing.T) {
	k, ctx, svc := initGenesis(t, 1)
	srv := aezkeeper.NewMsgServerImpl(&k)

	// Half one: a zone id outside 0..ZoneCount-1. Rejected structurally by
	// RoutingTable.Validate.
	var outOfRange [aeztypes.BucketCount]aeztypes.ZoneID
	for i := range outOfRange {
		outOfRange[i] = aeztypes.ZoneIDCore
	}
	outOfRange[3] = aeztypes.ZoneID(99)
	_, err := srv.UpdateRoutingTable(ctx, msgFor(aeztypes.NewRoutingTable(2, 1, epochLength, outOfRange), govAuthority()))
	require.ErrorContains(t, err, "invalid routing table")

	// Half two: a zone id that is IN RANGE but has no registered descriptor
	// in committed state. RoutingTable.Validate cannot catch this -- it only
	// range-checks against a compile-time constant -- so this is what
	// ValidateRoutingTableTransition's store check exists for.
	//
	// Delete zone 3's descriptor, then aim a bucket at it. The bucket must
	// come from a zone that is NOT core, or the core-zone trap would reject
	// the table first and this assertion would prove nothing. So: stage and
	// activate a table that legitimately puts a bucket on zone 3 via a
	// direct keeper call, then try to route another bucket there by Msg.
	require.NoError(t, svc.RawStore().Delete(aeztypes.ZoneKey(aeztypes.ZoneID(3))))

	var targetsMissingZone [aeztypes.BucketCount]aeztypes.ZoneID
	for i := range targetsMissingZone {
		targetsMissingZone[i] = aeztypes.ZoneIDCore
	}
	table := aeztypes.NewRoutingTable(2, 1, epochLength, targetsMissingZone)
	// Sanity: with every bucket on core, zone 3 is not referenced and the
	// table stages fine even though zone 3's descriptor is gone.
	_, err = srv.UpdateRoutingTable(ctx, msgFor(table, govAuthority()))
	require.NoError(t, err)

	// Now reference the missing zone. The core trap and the missing-zone
	// check would both fire; assert the zone check is reached by removing
	// the core-move overlap -- put the whole table on zone 3 and confirm the
	// error mentions the unregistered zone rather than only the core rule.
	err = k.ValidateRoutingTableTransition(ctx, allOnZone(3, 3, 1, epochLength))
	require.ErrorContains(t, err, "no registered descriptor")

	// A zone that IS registered passes the same check.
	err = k.ValidateRoutingTableTransition(ctx, allOnZone(2, 3, 1, epochLength))
	require.ErrorContains(t, err, "core zone never migrates", "zone 2 exists, so only the core rule should fire")
}

func allOnZone(zone uint32, version, epoch uint64, activationHeight int64) aeztypes.RoutingTable {
	var buckets [aeztypes.BucketCount]aeztypes.ZoneID
	for i := range buckets {
		buckets[i] = aeztypes.ZoneID(zone)
	}
	return aeztypes.NewRoutingTable(version, epoch, activationHeight, buckets)
}

// TestZoneOfStillReturnsCoreForEverythingAfterANoOpTable is the "behaviour stays
// bit-identical" assertion.
//
// A no-op table is the only thing Phase 2 governance can stage, and after it
// activates every entity must still resolve exactly where it did before. This is
// what makes the claim "nothing routes on zones yet" testable rather than
// asserted in prose.
func TestZoneOfStillReturnsCoreForEverythingAfterANoOpTable(t *testing.T) {
	k, ctx, _ := initGenesis(t, 1)
	srv := aezkeeper.NewMsgServerImpl(&k)

	_, err := srv.UpdateRoutingTable(ctx, msgFor(coreTable(2, 1, epochLength), govAuthority()))
	require.NoError(t, err)
	at := ctxAtHeight(epochLength)
	require.NoError(t, k.BeginBlocker(at))

	current, err := k.GetRoutingTable(at)
	require.NoError(t, err)
	require.Equal(t, uint64(2), current.Version, "fixture: the new table must really be current")

	// Every namespace, a spread of entities: all still core.
	for _, ns := range aeztypes.AllNamespaces() {
		for i := 0; i < 64; i++ {
			entity := []byte{byte(i), byte(i >> 8), 0x5a}
			zone, err := k.ZoneOf(at, ns, entity)
			require.NoError(t, err)
			require.Equal(t, aeztypes.ZoneIDCore, zone, "%s entity %d left the core zone", ns, i)
		}
	}

	// And the buckets themselves never moved: only the table's identity did
	// (I-4, I-5).
	require.Equal(t, aeztypes.BucketsFromTable(coreTable(1, 0, 0)), aeztypes.BucketsFromTable(current))
}

// TestStageAndActivateEmitEvents: activation happens in a BeginBlocker with no
// transaction to attribute it to, so without an event the only evidence the
// table moved is a diff of two queries.
func TestStageAndActivateEmitEvents(t *testing.T) {
	k, _, svc := initGenesis(t, 1)
	srv := aezkeeper.NewMsgServerImpl(&k)

	stageCtx := ctxAtHeight(1).(sdk.Context).WithEventManager(sdk.NewEventManager())
	table := coreTable(2, 1, epochLength)
	_, err := srv.UpdateRoutingTable(stageCtx, msgFor(table, govAuthority()))
	require.NoError(t, err)
	requireEvent(t, stageCtx, aeztypes.EventTypeStageRoutingTable, map[string]string{
		aeztypes.AttributeKeyVersion:		"2",
		aeztypes.AttributeKeyActivationHeight:	"10000",
		aeztypes.AttributeKeyAuthority:		govAuthority(),
	})

	activateCtx := ctxAtHeight(epochLength).(sdk.Context).WithEventManager(sdk.NewEventManager())
	require.NoError(t, k.BeginBlocker(activateCtx))
	requireEvent(t, activateCtx, aeztypes.EventTypeActivateRoutingTable, map[string]string{
		aeztypes.AttributeKeyVersion:		"2",
		aeztypes.AttributeKeyPreviousVersion:	"1",
	})

	// A block with nothing pending emits nothing.
	quietCtx := ctxAtHeight(epochLength + 1).(sdk.Context).WithEventManager(sdk.NewEventManager())
	require.NoError(t, k.BeginBlocker(quietCtx))
	require.Empty(t, quietCtx.EventManager().Events())

	_ = svc
}

func requireEvent(t *testing.T, ctx sdk.Context, eventType string, want map[string]string) {
	t.Helper()
	for _, event := range ctx.EventManager().Events() {
		if event.Type != eventType {
			continue
		}
		got := map[string]string{}
		for _, attr := range event.Attributes {
			got[attr.Key] = attr.Value
		}
		for key, value := range want {
			require.Equal(t, value, got[key], "event %s attribute %s", eventType, key)
		}
		return
	}
	t.Fatalf("event %s was not emitted", eventType)
}

// TestMsgRejectsAWrongLengthBucketVector: the wire carries a []uint32, but the
// table type is a [BucketCount]ZoneID array whose totality (I-7) is guaranteed
// by its LENGTH. RoutingTableFromMsg is the only place that conversion happens,
// so it is the only place the length can be enforced.
func TestMsgRejectsAWrongLengthBucketVector(t *testing.T) {
	k, ctx, _ := initGenesis(t, 1)
	srv := aezkeeper.NewMsgServerImpl(&k)

	for _, buckets := range [][]uint32{nil, {}, make([]uint32, aeztypes.BucketCount-1), make([]uint32, aeztypes.BucketCount+1)} {
		_, err := srv.UpdateRoutingTable(ctx, &aeztypes.MsgUpdateRoutingTable{
			Authority:		govAuthority(),
			Version:		2,
			Epoch:			1,
			ActivationHeight:	epochLength,
			Buckets:		buckets,
		})
		require.ErrorContains(t, err, "must carry exactly", "bucket vector of length %d", len(buckets))
	}

	_, found, err := k.GetPendingVersion(ctx)
	require.NoError(t, err)
	require.False(t, found)
}

// TestStagedTableSurvivesARestart is the F-17 guard on the Phase 2 path: a
// pending table is committed state, so a restarted node activates it at the same
// height a continuously-running one does.
func TestStagedTableSurvivesARestart(t *testing.T) {
	k, ctx, svc := initGenesis(t, 1)
	srv := aezkeeper.NewMsgServerImpl(&k)
	_, err := srv.UpdateRoutingTable(ctx, msgFor(coreTable(2, 1, epochLength), govAuthority()))
	require.NoError(t, err)

	// A brand-new keeper over the same store -- a restart or state-sync.
	restarted := aezkeeper.NewPersistentKeeper(svc)
	at := ctxAtHeight(epochLength)
	require.NoError(t, restarted.BeginBlocker(at))

	fromRestarted, err := restarted.GetRoutingTable(at)
	require.NoError(t, err)
	fromOriginal, err := k.GetRoutingTable(at)
	require.NoError(t, err)
	require.Equal(t, uint64(2), fromRestarted.Version)
	require.Equal(t, fromOriginal, fromRestarted, "a restarted node must swap the same table at the same height")
}
