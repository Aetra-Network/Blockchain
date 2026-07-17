package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	aeztypes "github.com/sovereign-l1/l1/x/aez/types"
)

const epochLength = int64(aeztypes.DefaultRoutingEpochLength)

func elasticTable(version, epoch uint64, activationHeight int64) aeztypes.RoutingTable {
	var buckets [aeztypes.BucketCount]aeztypes.ZoneID
	for i := range buckets {
		buckets[i] = aeztypes.ZoneID(uint32(i) % aeztypes.ZoneCount)
	}
	return aeztypes.NewRoutingTable(version, epoch, activationHeight, buckets)
}

func TestGenesisInstallsVersionOneAsCurrent(t *testing.T) {
	k, ctx, _ := initGenesis(t, 1)

	version, found, err := k.GetCurrentVersion(ctx)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, uint64(1), version)

	_, found, err = k.GetPendingVersion(ctx)
	require.NoError(t, err)
	require.False(t, found, "genesis must not leave a pending table")
}

// TestPendingTableActivatesAtExactlyItsHeight: not one block early, not one
// block late.
func TestPendingTableActivatesAtExactlyItsHeight(t *testing.T) {
	k, ctx, _ := initGenesis(t, 1)
	pending := elasticTable(2, 1, epochLength)
	require.NoError(t, k.SetPendingRoutingTable(ctx, pending))

	// Still pending, and the CURRENT table is untouched, at every height
	// before activation.
	for _, height := range []int64{1, 2, epochLength - 2, epochLength - 1} {
		at := ctxAtHeight(height)
		activated, err := k.MaybeActivatePendingRoutingTable(at)
		require.NoError(t, err)
		require.False(t, activated, "activated early at height %d", height)

		current, err := k.GetRoutingTable(at)
		require.NoError(t, err)
		require.Equal(t, uint64(1), current.Version, "current table changed early at height %d", height)

		version, found, err := k.GetPendingVersion(at)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, uint64(2), version)
	}

	// Activates exactly at its height.
	at := ctxAtHeight(epochLength)
	activated, err := k.MaybeActivatePendingRoutingTable(at)
	require.NoError(t, err)
	require.True(t, activated)

	current, err := k.GetRoutingTable(at)
	require.NoError(t, err)
	require.Equal(t, uint64(2), current.Version)
	require.Equal(t, pending.TableHash, current.TableHash)

	// Pending is cleared, so re-running is a no-op (idempotent).
	_, found, err := k.GetPendingVersion(at)
	require.NoError(t, err)
	require.False(t, found)
	activated, err = k.MaybeActivatePendingRoutingTable(at)
	require.NoError(t, err)
	require.False(t, activated)
}

// TestActivationIsIdempotentAcrossRepeatedBlocks: the swap must happen once.
func TestActivationIsIdempotentAcrossRepeatedBlocks(t *testing.T) {
	k, ctx, _ := initGenesis(t, 1)
	require.NoError(t, k.SetPendingRoutingTable(ctx, elasticTable(2, 1, epochLength)))

	count := 0
	for height := int64(1); height <= epochLength+5; height++ {
		activated, err := k.MaybeActivatePendingRoutingTable(ctxAtHeight(height))
		require.NoError(t, err)
		if activated {
			count++
			require.Equal(t, epochLength, height, "activation happened at the wrong height")
		}
	}
	require.Equal(t, 1, count, "the table must activate exactly once")
}

// TestMidEpochActivationIsRejected guards I-8.
func TestMidEpochActivationIsRejected(t *testing.T) {
	k, ctx, _ := initGenesis(t, 1)
	for _, height := range []int64{epochLength - 1, epochLength + 1, epochLength + 4999, 12345} {
		err := k.SetPendingRoutingTable(ctx, elasticTable(2, 1, height))
		require.ErrorIs(t, err, aeztypes.ErrRoutingEpochBoundary, "height %d must not be a valid activation", height)
	}
	// Multiples of the epoch length are accepted.
	for _, height := range []int64{epochLength, epochLength * 2, epochLength * 7} {
		require.NoError(t, k.SetPendingRoutingTable(ctx, elasticTable(2, 1, height)), "height %d should be a boundary", height)
	}
}

// TestActivationInThePastOrPresentIsRejected: a table activating at the current
// height would take effect part-way through a block whose earlier transactions
// already resolved against the old table.
func TestActivationInThePastOrPresentIsRejected(t *testing.T) {
	k, _, _ := initGenesis(t, epochLength)

	// Height 0 is a boundary but is in the past.
	err := k.SetPendingRoutingTable(ctxAtHeight(epochLength), elasticTable(2, 1, 0))
	require.ErrorIs(t, err, aeztypes.ErrRoutingEpochBoundary)

	// The current height, even though it is a boundary.
	err = k.SetPendingRoutingTable(ctxAtHeight(epochLength), elasticTable(2, 1, epochLength))
	require.ErrorIs(t, err, aeztypes.ErrRoutingEpochBoundary)

	// The next boundary is fine.
	require.NoError(t, k.SetPendingRoutingTable(ctxAtHeight(epochLength), elasticTable(2, 1, epochLength*2)))
}

// TestVersionMustIncreaseMonotonically guards I-8: a table that could reuse or
// lower a version could rewrite routing history.
func TestVersionMustIncreaseMonotonically(t *testing.T) {
	k, ctx, _ := initGenesis(t, 1)

	for _, version := range []uint64{0, 1} {
		err := k.SetPendingRoutingTable(ctx, elasticTable(version, 1, epochLength))
		require.Error(t, err, "version %d must be rejected against current version 1", version)
	}
	require.NoError(t, k.SetPendingRoutingTable(ctx, elasticTable(2, 1, epochLength)))

	// After activation, version 2 is current and must itself be unbeatable.
	at := ctxAtHeight(epochLength)
	_, err := k.MaybeActivatePendingRoutingTable(at)
	require.NoError(t, err)

	err = k.SetPendingRoutingTable(at, elasticTable(2, 2, epochLength*2))
	require.ErrorIs(t, err, aeztypes.ErrRoutingTableVersion)
	require.NoError(t, k.SetPendingRoutingTable(at, elasticTable(3, 2, epochLength*2)))
}

func TestSetPendingRejectsInvalidTable(t *testing.T) {
	k, ctx, _ := initGenesis(t, 1)

	tampered := elasticTable(2, 1, epochLength)
	tampered.Buckets[0] = aeztypes.ZoneID(3)	// hash no longer matches
	require.ErrorIs(t, k.SetPendingRoutingTable(ctx, tampered), aeztypes.ErrInvalidRoutingTable)

	var bad [aeztypes.BucketCount]aeztypes.ZoneID
	bad[0] = aeztypes.ZoneID(99)
	require.ErrorIs(t, k.SetPendingRoutingTable(ctx, aeztypes.NewRoutingTable(2, 1, epochLength, bad)), aeztypes.ErrInvalidRoutingTable)
}

// TestStoredTableIsRevalidatedOnRead: a table tampered with in the store must
// fail its committed hash rather than silently routing entities somewhere new.
func TestStoredTableIsRevalidatedOnRead(t *testing.T) {
	k, ctx, svc := initGenesis(t, 1)

	// Corrupt the stored bytes directly.
	corrupt := `{"Version":1,"Epoch":0,"ActivationHeight":0,"Buckets":[4],"TableHash":"AAAA"}`
	require.NoError(t, svc.RawStore().Set(aeztypes.RoutingTableVersionKey(1), []byte(corrupt)))

	_, err := k.GetRoutingTable(ctx)
	require.Error(t, err, "a corrupt stored table must not be served")
}

// TestEachTableVersionIsItsOwnKey: versions are per-entity keys, so an older
// version stays readable after a swap. That is what makes the table auditable.
func TestEachTableVersionIsItsOwnKey(t *testing.T) {
	k, ctx, _ := initGenesis(t, 1)
	require.NoError(t, k.SetPendingRoutingTable(ctx, elasticTable(2, 1, epochLength)))

	at := ctxAtHeight(epochLength)
	_, err := k.MaybeActivatePendingRoutingTable(at)
	require.NoError(t, err)

	v1, found, err := k.GetRoutingTableVersion(at, 1)
	require.NoError(t, err)
	require.True(t, found, "version 1 must remain readable after the swap")
	require.Equal(t, uint64(1), v1.Version)

	v2, found, err := k.GetRoutingTableVersion(at, 2)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, uint64(2), v2.Version)

	_, found, err = k.GetRoutingTableVersion(at, 99)
	require.NoError(t, err)
	require.False(t, found)
}

// TestZoneOfFollowsTheTableAfterAnEpochSwap: an unpinned entity moves with the
// table; a pinned one does not.
func TestZoneOfFollowsTheTableAfterAnEpochSwap(t *testing.T) {
	k, ctx, _ := initGenesis(t, 1)

	entity := []byte("relocating-entity")
	bucket := aeztypes.ComputeBucket(aeztypes.NamespaceNativeAccount, entity)

	before, err := k.ZoneOf(ctx, aeztypes.NamespaceNativeAccount, entity)
	require.NoError(t, err)
	require.Equal(t, aeztypes.ZoneIDCore, before)

	pending := elasticTable(2, 1, epochLength)
	require.NoError(t, k.SetPendingRoutingTable(ctx, pending))
	at := ctxAtHeight(epochLength)
	_, err = k.MaybeActivatePendingRoutingTable(at)
	require.NoError(t, err)

	after, err := k.ZoneOf(at, aeztypes.NamespaceNativeAccount, entity)
	require.NoError(t, err)
	require.Equal(t, pending.ZoneForBucket(bucket), after)

	// The bucket itself NEVER moved -- only the table did (I-4, I-5).
	require.Equal(t, bucket, aeztypes.ComputeBucket(aeztypes.NamespaceNativeAccount, entity))

	// A core-pinned entity is unaffected by the swap.
	pinned, err := k.ZoneOfEntity(at, aeztypes.EntityKindName, "alice.aet")
	require.NoError(t, err)
	require.Equal(t, aeztypes.ZoneIDCore, pinned.Zone)
	require.False(t, pinned.Hashed)
}
