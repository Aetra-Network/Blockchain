package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	aeztypes "github.com/sovereign-l1/l1/x/aez/types"
)

func coreOnlyBuckets() [aeztypes.BucketCount]aeztypes.ZoneID {
	var buckets [aeztypes.BucketCount]aeztypes.ZoneID
	for i := range buckets {
		buckets[i] = aeztypes.ZoneIDCore
	}
	return buckets
}

// TestRoutingTableIsTotalOverAll256Buckets guards I-7.
//
// Totality is enforced by the TYPE ([BucketCount]ZoneID is a fixed-size array),
// so a missing bucket is unrepresentable. This test pins the arity so a refactor
// to a map or slice -- which WOULD permit a missing bucket -- fails loudly.
func TestRoutingTableIsTotalOverAll256Buckets(t *testing.T) {
	table := aeztypes.GenesisRoutingTable()
	require.NoError(t, table.Validate())
	require.Len(t, table.Buckets, 256)

	// Every possible BucketID indexes a real entry: total by construction,
	// cannot panic.
	for i := 0; i <= 255; i++ {
		zone := table.ZoneForBucket(aeztypes.BucketID(i))
		require.True(t, zone.IsValid())
		require.Equal(t, aeztypes.ZoneIDCore, zone)
	}
}

// TestGenesisRoutingTableIsCoreOnly is the Phase 1 additive guarantee: with
// every bucket on zone 0, no entity can resolve anywhere else.
func TestGenesisRoutingTableIsCoreOnly(t *testing.T) {
	gs := aeztypes.DefaultGenesis()
	require.NoError(t, gs.Validate())
	require.True(t, gs.IsCoreOnly())
	for i := 0; i < aeztypes.BucketCount; i++ {
		require.Equal(t, aeztypes.ZoneIDCore, gs.RoutingTable.Buckets[i], "bucket %d escaped the core zone", i)
	}
}

func TestRoutingTableRejectsOutOfRangeZone(t *testing.T) {
	buckets := coreOnlyBuckets()
	buckets[42] = aeztypes.ZoneID(aeztypes.ZoneCount)	// one past the last valid zone
	table := aeztypes.NewRoutingTable(1, 0, 0, buckets)
	err := table.Validate()
	require.ErrorIs(t, err, aeztypes.ErrInvalidRoutingTable)
	require.ErrorContains(t, err, "bucket 42")
}

func TestRoutingTableRejectsZeroVersion(t *testing.T) {
	table := aeztypes.NewRoutingTable(0, 0, 0, coreOnlyBuckets())
	require.ErrorIs(t, table.Validate(), aeztypes.ErrInvalidRoutingTable)
}

func TestRoutingTableRejectsNegativeActivationHeight(t *testing.T) {
	table := aeztypes.NewRoutingTable(1, 0, -1, coreOnlyBuckets())
	require.ErrorIs(t, table.Validate(), aeztypes.ErrInvalidRoutingTable)
}

// TestRoutingTableHashDetectsTampering: the committed hash is what makes a table
// read back from the store trustworthy.
func TestRoutingTableHashDetectsTampering(t *testing.T) {
	table := aeztypes.GenesisRoutingTable()
	require.NoError(t, table.Validate())

	for _, tc := range []struct {
		name	string
		mutate	func(*aeztypes.RoutingTable)
	}{
		{"bucket moved", func(tbl *aeztypes.RoutingTable) { tbl.Buckets[3] = aeztypes.ZoneID(2) }},
		{"version bumped", func(tbl *aeztypes.RoutingTable) { tbl.Version = 99 }},
		{"epoch bumped", func(tbl *aeztypes.RoutingTable) { tbl.Epoch = 99 }},
		{"activation height moved", func(tbl *aeztypes.RoutingTable) { tbl.ActivationHeight = 12345 }},
		{"hash truncated", func(tbl *aeztypes.RoutingTable) { tbl.TableHash = tbl.TableHash[:16] }},
		{"hash cleared", func(tbl *aeztypes.RoutingTable) { tbl.TableHash = nil }},
		{"hash flipped", func(tbl *aeztypes.RoutingTable) {
			flipped := append([]byte(nil), tbl.TableHash...)
			flipped[0] ^= 0xff
			tbl.TableHash = flipped
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tampered := aeztypes.GenesisRoutingTable()
			tc.mutate(&tampered)
			require.ErrorIs(t, tampered.Validate(), aeztypes.ErrInvalidRoutingTable)
		})
	}
}

// TestRoutingTableHashIsStableAndDeterministic: the canonical hash depends only
// on semantic content, and is reproducible.
func TestRoutingTableHashIsStableAndDeterministic(t *testing.T) {
	left := aeztypes.NewRoutingTable(7, 3, 100, coreOnlyBuckets())
	right := aeztypes.NewRoutingTable(7, 3, 100, coreOnlyBuckets())
	require.Equal(t, left.TableHash, right.TableHash)
	require.Equal(t, left.TableHash, left.ComputeTableHash())

	// Every semantic field is bound into the digest.
	require.NotEqual(t, left.TableHash, aeztypes.NewRoutingTable(8, 3, 100, coreOnlyBuckets()).TableHash)
	require.NotEqual(t, left.TableHash, aeztypes.NewRoutingTable(7, 4, 100, coreOnlyBuckets()).TableHash)
	require.NotEqual(t, left.TableHash, aeztypes.NewRoutingTable(7, 3, 101, coreOnlyBuckets()).TableHash)

	moved := coreOnlyBuckets()
	moved[255] = aeztypes.ZoneID(1)
	require.NotEqual(t, left.TableHash, aeztypes.NewRoutingTable(7, 3, 100, moved).TableHash)
}

// TestRoutingTableHashIsDomainSeparatedFromBuckets: a table hash can never be
// confused with a bucket preimage.
func TestRoutingTableHashIsDomainSeparatedFromBuckets(t *testing.T) {
	require.NotEqual(t, aeztypes.BucketDomain, aeztypes.RoutingTableDomain)
}

func TestRoutingEpochBoundary(t *testing.T) {
	const length = uint64(10000)
	require.True(t, aeztypes.IsRoutingEpochBoundary(0, length))
	require.True(t, aeztypes.IsRoutingEpochBoundary(10000, length))
	require.True(t, aeztypes.IsRoutingEpochBoundary(20000, length))
	require.False(t, aeztypes.IsRoutingEpochBoundary(9999, length))
	require.False(t, aeztypes.IsRoutingEpochBoundary(10001, length))
	require.False(t, aeztypes.IsRoutingEpochBoundary(-1, length))
	// A zero epoch length must never report a boundary (it would make every
	// height a boundary via modulo-by-zero, or panic).
	require.False(t, aeztypes.IsRoutingEpochBoundary(0, 0))
	require.False(t, aeztypes.IsRoutingEpochBoundary(100, 0))

	require.Equal(t, uint64(0), aeztypes.RoutingEpochForHeight(9999, length))
	require.Equal(t, uint64(1), aeztypes.RoutingEpochForHeight(10000, length))
	require.Equal(t, uint64(1), aeztypes.RoutingEpochForHeight(19999, length))
	require.Equal(t, uint64(2), aeztypes.RoutingEpochForHeight(20000, length))
	require.Equal(t, uint64(0), aeztypes.RoutingEpochForHeight(100, 0))
}

func TestZoneValidation(t *testing.T) {
	require.NoError(t, aeztypes.ZoneIDCore.Validate())
	require.True(t, aeztypes.ZoneIDCore.IsCore())
	require.Equal(t, aeztypes.ZoneKindCore, aeztypes.ZoneIDCore.Kind())

	for i := uint32(1); i < aeztypes.ZoneCount; i++ {
		zone := aeztypes.ZoneID(i)
		require.NoError(t, zone.Validate())
		require.False(t, zone.IsCore())
		require.Equal(t, aeztypes.ZoneKindElastic, zone.Kind())
	}

	require.ErrorIs(t, aeztypes.ZoneID(aeztypes.ZoneCount).Validate(), aeztypes.ErrInvalidZone)
	require.Len(t, aeztypes.AllZoneIDs(), int(aeztypes.ZoneCount))
	require.Equal(t, uint32(5), aeztypes.ZoneCount, "core zone plus four elastic zones")
}

func TestGenesisValidation(t *testing.T) {
	t.Run("default is valid", func(t *testing.T) {
		require.NoError(t, aeztypes.DefaultGenesis().Validate())
	})
	t.Run("prototype gate is disabled at genesis", func(t *testing.T) {
		// I-23: a disabled x/aez can never fail a block.
		require.False(t, aeztypes.DefaultGenesis().Params.Prototype.Enabled)
	})
	t.Run("rejects wrong zone count", func(t *testing.T) {
		gs := aeztypes.DefaultGenesis()
		gs.Zones = gs.Zones[:2]
		require.ErrorIs(t, gs.Validate(), aeztypes.ErrInvalidGenesis)
	})
	t.Run("rejects duplicate zone", func(t *testing.T) {
		gs := aeztypes.DefaultGenesis()
		gs.Zones[1] = aeztypes.NewZone(aeztypes.ZoneIDCore)
		require.ErrorIs(t, gs.Validate(), aeztypes.ErrInvalidGenesis)
	})
	t.Run("rejects mismatched zone kind", func(t *testing.T) {
		gs := aeztypes.DefaultGenesis()
		gs.Zones[0] = aeztypes.Zone{ID: aeztypes.ZoneIDCore, Kind: aeztypes.ZoneKindElastic}
		require.ErrorIs(t, gs.Validate(), aeztypes.ErrInvalidGenesis)
	})
	t.Run("rejects tampered routing table", func(t *testing.T) {
		gs := aeztypes.DefaultGenesis()
		gs.RoutingTable.Buckets[0] = aeztypes.ZoneID(3)
		require.ErrorIs(t, gs.Validate(), aeztypes.ErrInvalidGenesis)
	})
	t.Run("accepts a valid non-core table", func(t *testing.T) {
		// Validate checks STRUCTURE, not the Phase 1 shipping policy: a
		// table with elastic assignments is well-formed and is what later
		// phases will ship. IsCoreOnly is the separate, explicit predicate.
		gs := aeztypes.DefaultGenesis()
		buckets := coreOnlyBuckets()
		buckets[9] = aeztypes.ZoneID(2)
		gs.RoutingTable = aeztypes.NewRoutingTable(1, 0, 0, buckets)
		require.NoError(t, gs.Validate())
		require.False(t, gs.IsCoreOnly())
	})
}
