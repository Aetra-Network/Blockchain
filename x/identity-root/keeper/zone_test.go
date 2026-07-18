package keeper

import (
	"testing"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"cosmossdk.io/log/v2"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/app/addressing"
	aezkeeper "github.com/sovereign-l1/l1/x/aez/keeper"
	aeztypes "github.com/sovereign-l1/l1/x/aez/types"
	"github.com/sovereign-l1/l1/x/internal/kvtest"
)

func zoneTestCtx() sdk.Context {
	return sdk.NewContext(nil, cmtproto.Header{Height: 1}, false, log.NewNopLogger())
}

// mustBucketEntity searches over trivially-derived 20-byte addresses for one
// whose normalized native-account identity hashes into the requested bucket,
// so the routing-table test below can prove a REAL entity's zone actually
// moves (the sanity check that the table swap itself is not a no-op) using
// only exported, deterministic functions -- no test-only hash bypass.
func mustBucketEntity(t *testing.T, bucket aeztypes.BucketID) []byte {
	t.Helper()
	raw := make([]byte, 20)
	for counter := 0; counter < 100000; counter++ {
		raw[18] = byte(counter >> 8)
		raw[19] = byte(counter)
		identity, err := addressing.NormalizeToAccountIdentity(raw)
		require.NoError(t, err)
		if aeztypes.ComputeBucket(aeztypes.NamespaceNativeAccount, identity) == bucket {
			return identity
		}
	}
	t.Fatalf("no address found in bucket %d after exhausting search", bucket)
	return nil
}

// TestNameZoneWithoutResolverDegradesInsteadOfFailing is the I-23 analogue for
// names: a Keeper built without WithZoneResolver (every pre-existing
// identity-root unit test) must answer NameZone with Resolved=false and no
// error, never panic or fail a query just because x/aez isn't wired.
func TestNameZoneWithoutResolverDegradesInsteadOfFailing(t *testing.T) {
	k := setupKeeper(t)
	nz, err := k.NameZone(zoneTestCtx(), "alice")
	require.NoError(t, err)
	require.False(t, nz.Resolved)
	require.Zero(t, nz.Zone)
	require.Zero(t, nz.Bucket)
}

// TestNameZoneMatchesAEZGoldenBucketVector wires a REAL x/aez keeper in and
// proves two things against the live construction, not a stub:
//
//  1. BucketOfName("alice.aet") is EXACTLY 220 -- the frozen golden vector in
//     x/aez/types/bucket_test.go ("name/alice.aet") -- so the plumbing added
//     here (CanonicalEntityID(EntityKindName, ...) + ComputeBucket, reached
//     through x/identity-root's own NormalizeName) hashes the identical bytes
//     x/aez's own test freezes, not a subtly different encoding.
//  2. ZoneOfName resolves to the Core Zone, unconditionally, because
//     x/aez/types.NamespaceName is Core-pinned (I-9) -- proving the
//     integration surfaces the REAL zone (permanently Core), not a fabricated
//     "it moved" answer.
func TestNameZoneMatchesAEZGoldenBucketVector(t *testing.T) {
	svc := kvtest.NewStoreService()
	aez := aezkeeper.NewPersistentKeeper(svc)
	ctx := zoneTestCtx()
	require.NoError(t, aez.InitGenesisState(ctx, aeztypes.DefaultGenesis()))

	k := setupKeeper(t).WithZoneResolver(&aez)

	nz, err := k.NameZone(ctx, "alice")
	require.NoError(t, err)
	require.True(t, nz.Resolved)
	require.Equal(t, uint32(220), nz.Bucket, "must match x/aez's frozen name/alice.aet golden vector")
	require.Equal(t, uint32(aeztypes.ZoneIDCore), nz.Zone, "NamespaceName is Core-pinned (I-9): every name lives in Core")
}

// TestNameZoneStaysCoreAcrossARoutingTableThatWouldMoveTheBucket is the
// integration-level proof of I-9 for names, mirroring
// x/aez/keeper/multizone_test.go's own
// TestActivateSecondZoneRemapsSubsetWhileCoreStaysPinned: even after a live
// governance-style routing-table update remaps alice.aet's OWN bucket (220)
// to a non-Core zone, x/identity-root's NameZone -- read through the full
// resolver wiring -- still reports Core, because ZoneOfEntity never enters
// the hash for a Core-pinned namespace in the first place. The bucket answer
// is unaffected either way, since BucketOfName never consults the routing
// table at all.
func TestNameZoneStaysCoreAcrossARoutingTableThatWouldMoveTheBucket(t *testing.T) {
	svc := kvtest.NewStoreService()
	aez := aezkeeper.NewPersistentKeeper(svc)
	ctx := zoneTestCtx()
	require.NoError(t, aez.InitGenesisState(ctx, aeztypes.DefaultGenesis()))

	k := setupKeeper(t).WithZoneResolver(&aez)

	before, err := k.NameZone(ctx, "alice")
	require.NoError(t, err)
	require.Equal(t, uint32(220), before.Bucket)
	require.Equal(t, uint32(aeztypes.ZoneIDCore), before.Zone)

	// Move bucket 220 (alice.aet's own bucket) to zone 2 in a freshly staged
	// and activated routing table -- the same schedule/activate mechanism
	// x/aez's own tests (bus_test.go's installTable) use, not a test-only
	// setter. Activation must land on a routing-epoch boundary
	// (DefaultRoutingEpochLength = 10000).
	current, err := aez.GetRoutingTable(ctx)
	require.NoError(t, err)
	buckets := current.Buckets
	buckets[220] = aeztypes.ZoneID(2)
	table := aeztypes.NewRoutingTable(current.Version+1, current.Epoch+1, 10000, buckets)
	require.NoError(t, table.Validate())
	require.NoError(t, aez.SetPendingRoutingTable(ctx, table))

	activateCtx := sdk.NewContext(nil, cmtproto.Header{Height: 10000}, false, log.NewNopLogger())
	activated, err := aez.MaybeActivatePendingRoutingTable(activateCtx)
	require.NoError(t, err)
	require.True(t, activated, "the staged table must activate at its ActivationHeight")

	// Sanity check the table actually moved: an ordinary address hashing into
	// bucket 220 must now resolve to zone 2 -- proving the table swap is real,
	// not a no-op, before asserting the name-specific pin below.
	moved, err := aez.ResolveZone(activateCtx, aeztypes.NamespaceNativeAccount, mustBucketEntity(t, 220))
	require.NoError(t, err)
	require.Equal(t, aeztypes.ZoneID(2), moved.Zone, "an ordinary entity in bucket 220 must follow the new table")

	after, err := k.NameZone(activateCtx, "alice")
	require.NoError(t, err)
	require.Equal(t, uint32(220), after.Bucket, "the bucket assignment itself never depends on the routing table")
	require.Equal(t, uint32(aeztypes.ZoneIDCore), after.Zone,
		"a Core-pinned namespace must stay on Core even when its own bucket is remapped elsewhere")
}
