package keeper_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"cosmossdk.io/log/v2"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/sovereign-l1/l1/app/addressing"
	aezkeeper "github.com/sovereign-l1/l1/x/aez/keeper"
	aeztypes "github.com/sovereign-l1/l1/x/aez/types"
	"github.com/sovereign-l1/l1/x/internal/kvtest"
)

// newKeeper builds a keeper over an in-memory KV store plus an sdk.Context at
// the given height. The keeper holds NO state of its own, so a fresh Keeper over
// the same store is byte-for-byte equivalent to a long-lived one -- which is the
// F-17 property TestKeeperHoldsNoStateAcrossRestart exercises.
func newKeeper(t *testing.T, height int64) (aezkeeper.Keeper, context.Context, *kvtest.StoreService) {
	t.Helper()
	svc := kvtest.NewStoreService()
	k := aezkeeper.NewPersistentKeeper(svc)
	ctx := sdk.NewContext(nil, cmtproto.Header{Height: height}, false, log.NewNopLogger())
	return k, ctx, svc
}

func initGenesis(t *testing.T, height int64) (aezkeeper.Keeper, context.Context, *kvtest.StoreService) {
	t.Helper()
	k, ctx, svc := newKeeper(t, height)
	require.NoError(t, k.InitGenesisState(ctx, aeztypes.DefaultGenesis()))
	return k, ctx, svc
}

func ctxAtHeight(height int64) context.Context {
	return sdk.NewContext(nil, cmtproto.Header{Height: height}, false, log.NewNopLogger())
}

func TestInitAndExportGenesisRoundTrips(t *testing.T) {
	k, ctx, _ := initGenesis(t, 1)

	exported, err := k.ExportGenesisState(ctx)
	require.NoError(t, err)
	require.NoError(t, exported.Validate())
	require.Equal(t, aeztypes.DefaultGenesis(), exported)
	require.True(t, exported.IsCoreOnly())
}

// TestExportReadsFromTheStoreNotFromRam is the F-17 regression guard.
//
// A brand-new Keeper -- the state of a restarted or state-synced node -- reads
// the SAME committed bytes as the keeper that wrote them. There is no k.genesis
// field that could hold DefaultGenesis() while the running node holds something
// else, so the two cannot disagree (I-20).
func TestExportReadsFromTheStoreNotFromRam(t *testing.T) {
	svc := kvtest.NewStoreService()
	ctx := ctxAtHeight(5)

	writer := aezkeeper.NewPersistentKeeper(svc)
	require.NoError(t, writer.InitGenesisState(ctx, aeztypes.DefaultGenesis()))

	// Mutate committed state so "the store" and "DefaultGenesis()" differ.
	params := aeztypes.DefaultParams()
	params.RoutingEpochLength = 777
	require.NoError(t, writer.SetParams(ctx, params))

	// A fresh keeper (restart) must observe the committed value, not the
	// default.
	restarted := aezkeeper.NewPersistentKeeper(svc)
	got, err := restarted.GetParams(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(777), got.RoutingEpochLength)

	fromWriter, err := writer.ExportGenesisState(ctx)
	require.NoError(t, err)
	fromRestarted, err := restarted.ExportGenesisState(ctx)
	require.NoError(t, err)
	require.Equal(t, fromWriter, fromRestarted, "a restarted node must export identical state")
}

// TestGetParamsErrorsWhenUninitialized: an uninitialized keeper must ERROR, not
// silently return DefaultParams(). A silent default is exactly how a restarted
// node and a running node come to take different branches.
func TestGetParamsErrorsWhenUninitialized(t *testing.T) {
	k, ctx, _ := newKeeper(t, 1)
	_, err := k.GetParams(ctx)
	require.Error(t, err)

	_, err = k.GetRoutingTable(ctx)
	require.ErrorIs(t, err, aeztypes.ErrRoutingTableNotFound)
}

func TestInitRoutingTableRefusesToOverwrite(t *testing.T) {
	k, ctx, _ := initGenesis(t, 1)
	require.Error(t, k.InitRoutingTable(ctx, aeztypes.GenesisRoutingTable()))
}

func TestZoneOfResolvesEveryBucketToCoreAtGenesis(t *testing.T) {
	k, ctx, _ := initGenesis(t, 1)
	for _, ns := range aeztypes.AllNamespaces() {
		for i := 0; i < 64; i++ {
			entity := []byte{byte(i), byte(i >> 8), 0x5a}
			zone, err := k.ZoneOf(ctx, ns, entity)
			require.NoError(t, err)
			require.Equal(t, aeztypes.ZoneIDCore, zone)
		}
	}
}

func TestZoneOfRejectsInvalidInput(t *testing.T) {
	k, ctx, _ := initGenesis(t, 1)

	_, err := k.ZoneOf(ctx, aeztypes.Namespace("bogus"), []byte("x"))
	require.ErrorIs(t, err, aeztypes.ErrInvalidNamespace)

	_, err = k.ZoneOf(ctx, aeztypes.NamespaceNativeAccount, nil)
	require.ErrorIs(t, err, aeztypes.ErrInvalidEntity)
}

// TestCorePinnedEntitiesNeverReachTheHash is the invariant test aez.md:615 asks
// for, and the "hashed == false" assertion is what makes it meaningful TODAY.
//
// The table below is ADVERSARIAL: every one of the 256 buckets maps to zone 4.
// Any entity that reaches the bucket hash therefore resolves to zone 4. Only a
// PRE-HASH pin can return zone 0. Asserting merely `zone == 0` against the
// genesis table would pass vacuously (all buckets are already 0) and would prove
// nothing until Phase 3.
func TestCorePinnedEntitiesNeverReachTheHash(t *testing.T) {
	k, ctx, _ := initGenesis(t, 1)

	// Install a hostile table as current, using only the REAL API: schedule
	// it as pending at the next epoch boundary, then activate it there. No
	// test-only setter exists, so this cannot install a table the chain
	// itself could not.
	var hostile [aeztypes.BucketCount]aeztypes.ZoneID
	for i := range hostile {
		hostile[i] = aeztypes.ZoneID(4)
	}
	malicious := aeztypes.NewRoutingTable(2, 1, int64(aeztypes.DefaultRoutingEpochLength), hostile)
	require.NoError(t, malicious.Validate())
	require.NoError(t, k.SetPendingRoutingTable(ctx, malicious))

	ctx = ctxAtHeight(int64(aeztypes.DefaultRoutingEpochLength))
	activated, err := k.MaybeActivatePendingRoutingTable(ctx)
	require.NoError(t, err)
	require.True(t, activated)

	// Sanity: the fixture really does route away from core, so the
	// assertions below are not vacuous.
	unpinned, err := k.ResolveZone(ctx, aeztypes.NamespaceNativeAccount, []byte("ordinary-user"))
	require.NoError(t, err)
	require.True(t, unpinned.Hashed, "fixture: an unpinned entity must reach the hash")
	require.Equal(t, aeztypes.ZoneID(4), unpinned.Zone, "fixture: the hostile table must actually apply")

	entries, err := aeztypes.SystemPinSet()
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	for _, entry := range entries {
		resolution, err := k.ZoneOfEntity(ctx, aeztypes.EntityKindAddress, entry.EntityID)
		require.NoError(t, err, entry.Label)
		require.Equal(t, aeztypes.ZoneIDCore, resolution.Zone, "%s escaped the core zone", entry.Label)
		require.True(t, resolution.Pinned, "%s was not pinned", entry.Label)
		require.False(t, resolution.Hashed, "%s reached the bucket hash", entry.Label)
	}

	// The name namespace is pinned at the NAMESPACE level, so every name
	// pins regardless of its bucket.
	for _, name := range []string{"alice.aet", "aet", "sub.alice.aet"} {
		resolution, err := k.ZoneOfEntity(ctx, aeztypes.EntityKindName, name)
		require.NoError(t, err)
		require.Equal(t, aeztypes.ZoneIDCore, resolution.Zone, name)
		require.True(t, resolution.Pinned, name)
		require.False(t, resolution.Hashed, "%s reached the bucket hash", name)
	}
}

// TestUnpinnedEntitiesDoReachTheHash is the complement: the pin must be
// selective, not a blanket "everything returns core".
func TestUnpinnedEntitiesDoReachTheHash(t *testing.T) {
	k, ctx, _ := initGenesis(t, 1)

	identity, err := addressing.NormalizeToAccountIdentity([]byte{
		1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
	})
	require.NoError(t, err)

	resolution, err := k.ZoneOfEntity(ctx, aeztypes.EntityKindAddress, identity)
	require.NoError(t, err)
	require.Equal(t, aeztypes.NamespaceNativeAccount, resolution.Namespace)
	require.False(t, resolution.Pinned)
	require.True(t, resolution.Hashed, "an ordinary account must reach the bucket hash")
	require.Equal(t, aeztypes.BucketID(142), resolution.Bucket)
	// ...but still lands on core in Phase 1, because every bucket maps there.
	require.Equal(t, aeztypes.ZoneIDCore, resolution.Zone)
}

// TestModuleAccountResolvesToCoreViaSystemNotNativeAccount is the I-10 keeper
// level guard: the pool's module account must pin, never hash.
func TestModuleAccountResolvesToCoreViaSystemNotNativeAccount(t *testing.T) {
	k, ctx, _ := initGenesis(t, 1)
	macc := authtypes.NewModuleAddress("nominator-pool")

	resolution, err := k.ZoneOfEntity(ctx, aeztypes.EntityKindAddress, []byte(macc))
	require.NoError(t, err)
	require.Equal(t, aeztypes.NamespaceSystem, resolution.Namespace)
	require.True(t, resolution.Pinned)
	require.False(t, resolution.Hashed)
	require.Equal(t, aeztypes.ZoneIDCore, resolution.Zone)
}

func TestZoneOfEntityAcceptsBothEncodings(t *testing.T) {
	k, ctx, _ := initGenesis(t, 1)
	identity, err := addressing.NormalizeToAccountIdentity([]byte{
		1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
	})
	require.NoError(t, err)
	user, err := addressing.FormatUserFriendly(identity)
	require.NoError(t, err)
	raw := addressing.Format(identity)

	fromUser, err := k.ZoneOfEntity(ctx, aeztypes.EntityKindAddress, user)
	require.NoError(t, err)
	fromRaw, err := k.ZoneOfEntity(ctx, aeztypes.EntityKindAddress, raw)
	require.NoError(t, err)
	require.Equal(t, fromUser, fromRaw)
	require.Equal(t, aeztypes.BucketID(142), fromUser.Bucket)
}

func TestZonesAreStoredPerEntity(t *testing.T) {
	k, ctx, svc := initGenesis(t, 1)

	zones, err := k.GetAllZones(ctx)
	require.NoError(t, err)
	require.Len(t, zones, int(aeztypes.ZoneCount))
	for i, zone := range zones {
		require.Equal(t, aeztypes.ZoneID(uint32(i)), zone.ID, "zones must come back in ascending id order")
	}

	// Each zone is its OWN key -- not one blob. This is the structural
	// property that makes per-zone overlays possible later.
	for _, id := range aeztypes.AllZoneIDs() {
		found, err := svc.RawStore().Has(aeztypes.ZoneKey(id))
		require.NoError(t, err)
		require.True(t, found, "zone %d must have its own store key", uint32(id))
	}
}
