package keeper_test

import (
	"context"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"cosmossdk.io/log/v2"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/sovereign-l1/l1/app/addressing"
	aezkeeper "github.com/sovereign-l1/l1/x/aez/keeper"
	aeztypes "github.com/sovereign-l1/l1/x/aez/types"
	"github.com/sovereign-l1/l1/x/internal/kvtest"
	nativeaccountkeeper "github.com/sovereign-l1/l1/x/native-account/keeper"
	nativeaccount "github.com/sovereign-l1/l1/x/native-account/types"
)

// The AEZ Phase 3 tests for x/native-account.
//
// The property under test is deliberately NOT "the zone appears in the key".
// Phase 3 resolves a zone as a DERIVED attribute and leaves account keys flat.
// The tests below are written to fail loudly if anyone later moves the zone into
// the key layout, because that change is exactly what would break them:
// TestRoutingEpochRezoneRewritesNoAccountKey asserts the store is byte-identical
// across a rezone, which a zone-prefixed key layout cannot satisfy.

// zoneFixture is a native-account keeper wired to a REAL x/aez keeper.
//
// The two keepers get SEPARATE store services because they are separate modules
// with separate mounted stores -- sharing one would let a native-account key
// collide with an aez key and quietly invalidate every assertion here.
type zoneFixture struct {
	native		nativeaccountkeeper.Keeper
	aez		aezkeeper.Keeper
	nativeStore	*kvtest.StoreService
	aezStore	*kvtest.StoreService
}

func newZoneFixture(t *testing.T) zoneFixture {
	t.Helper()
	nativeStore := kvtest.NewStoreService()
	aezStore := kvtest.NewStoreService()
	aezKeeper := aezkeeper.NewPersistentKeeper(aezStore)
	native := nativeaccountkeeper.NewPersistentKeeper(nativeStore).WithZoneResolver(aezKeeper)
	return zoneFixture{native: native, aez: aezKeeper, nativeStore: nativeStore, aezStore: aezStore}
}

// zoneCtx builds a context with a real EventManager: the activation path emits an
// event, and a nil manager would panic rather than test anything.
func zoneCtx(height int64) context.Context {
	return sdk.NewContext(nil, cmtproto.Header{Height: height}, false, log.NewNopLogger()).
		WithEventManager(sdk.NewEventManager())
}

// activate creates one real account through the real activation path.
//
// The key is derived from a fixed secret, never randomly generated: a random key
// would make the account's address -- and therefore its BUCKET -- differ on every
// run, so a failure could not be reproduced (I-22 in spirit).
func activate(t *testing.T, k nativeaccountkeeper.Keeper, ctx context.Context, secret string) nativeaccount.Account {
	t.Helper()
	privKey := secp256k1.GenPrivKeyFromSecret([]byte(secret))
	msg, err := nativeaccount.NewMsgActivateAccountFromPubKey(privKey.PubKey(), 0)
	require.NoError(t, err)
	result, err := k.ActivateAccount(ctx, msg)
	require.NoError(t, err)
	return result.Account
}

// stageAndActivateTable installs a routing table mapping EVERY bucket to zone,
// then advances to its activation height and swaps it in.
//
// It goes through SetPendingRoutingTable (the MECHANISM) rather than
// StageRoutingTable (the governance POLICY) on purpose. The policy layer
// enforces the Core-Zone one-way trap, and since genesis maps all 256 buckets to
// zone 0, every bucket is currently frozen -- so the policy path cannot express a
// rezone at all today and could not test one. x/aez's own adversarial pin test
// uses the same mechanism-level entry for the same reason.
func stageAndActivateTable(t *testing.T, f zoneFixture, zone aeztypes.ZoneID, version uint64) context.Context {
	t.Helper()
	var buckets [aeztypes.BucketCount]aeztypes.ZoneID
	for i := range buckets {
		buckets[i] = zone
	}
	activationHeight := int64(aeztypes.DefaultRoutingEpochLength) * int64(version)
	table := aeztypes.NewRoutingTable(version, version, activationHeight, buckets)
	require.NoError(t, table.Validate())
	require.NoError(t, f.aez.SetPendingRoutingTable(zoneCtx(activationHeight-1), table))

	atActivation := zoneCtx(activationHeight)
	swapped, err := f.aez.MaybeActivatePendingRoutingTable(atActivation)
	require.NoError(t, err)
	require.True(t, swapped, "fixture: the table must actually activate")
	return atActivation
}

// TestRoutingEpochRezoneRewritesNoAccountKey is the central Phase 3 test.
//
// It asserts the property that made zone-prefixed keys the wrong design: a
// routing-epoch rezone moves every account to a new zone and rewrites NOTHING.
// The native-account store is byte-for-byte identical before and after, so there
// is no migration to run, no key to rewrite inside the activation block, and no
// window in which an account could be stranded.
//
// Under the literal Phase 3 spec (zone-prefixed keys) this test is unsatisfiable
// by construction: the primary record plus three indexes would each have to move
// for all N accounts inside the activation block.
func TestRoutingEpochRezoneRewritesNoAccountKey(t *testing.T) {
	f := newZoneFixture(t)
	ctx := zoneCtx(1)
	require.NoError(t, f.aez.InitGenesisState(ctx, aeztypes.DefaultGenesis()))

	accounts := []nativeaccount.Account{
		activate(t, f.native, ctx, "aez-phase3-rezone-a"),
		activate(t, f.native, ctx, "aez-phase3-rezone-b"),
		activate(t, f.native, ctx, "aez-phase3-rezone-c"),
	}

	before := f.nativeStore.RawStore().Snapshot()
	exportBefore, err := f.native.ExportGenesis(ctx)
	require.NoError(t, err)

	// Anti-vacuity: snapshot equality across the rezone proves nothing if the
	// store is empty or if the keys are not the ones a zone prefix would move.
	// Pin that the primary record and all three indexes are really present, and
	// that their keys carry NO zone component today.
	for _, account := range accounts {
		userKey, err := nativeaccount.AccountByUserKey(account.AddressUser)
		require.NoError(t, err)
		rawKey, err := nativeaccount.AccountByRawKey(account.AddressRaw)
		require.NoError(t, err)
		require.Contains(t, before, userKey, "fixture: primary record must be in the store")
		require.Contains(t, before, rawKey, "fixture: raw index must be in the store")
		require.Contains(t, before, nativeaccount.AccountByNumberKey(account.AccountNumber),
			"fixture: number index must be in the store")
		require.Equal(t, nativeaccount.AccountByUserPrefix+account.AddressUser, userKey,
			"the account key must carry no zone component: zone lives in the resolver, not the key")
	}

	// Every bucket is on zone 0 at genesis, so every account resolves to Core.
	for _, account := range accounts {
		zone, err := f.native.ZoneOf(ctx, account.AddressUser)
		require.NoError(t, err)
		require.Equal(t, nativeaccountkeeper.CoreZone, zone.Zone)
		require.True(t, zone.Resolved, "a wired resolver over an initialized table must actually resolve")
	}

	// THE REZONE: every bucket moves 0 -> 2 at an epoch boundary.
	afterCtx := stageAndActivateTable(t, f, aeztypes.ZoneID(2), 2)

	// The accounts moved zone...
	for _, account := range accounts {
		zone, err := f.native.ZoneOf(afterCtx, account.AddressUser)
		require.NoError(t, err)
		require.Equal(t, uint32(2), zone.Zone, "the rezone must actually apply, else this test is vacuous")
		require.True(t, zone.Resolved)
	}

	// ...and not one byte of account state moved with them.
	require.Equal(t, before, f.nativeStore.RawStore().Snapshot(),
		"a routing-epoch rezone must not rewrite any native-account key")

	exportAfter, err := f.native.ExportGenesis(afterCtx)
	require.NoError(t, err)
	require.Equal(t, exportBefore, exportAfter, "rezone must not change exported account state")
}

// TestEveryAccountStaysReadableThroughEveryLookupPathAcrossARezone is the
// "no account is lost or duplicated" proof.
//
// It exercises all THREE lookup paths, not just the primary one. The indexes are
// where a zone-prefixed layout would have split-brained: by_raw and by_number map
// to an AddressUser, so a zone mismatch between an index and the primary record
// makes an account unreachable through one path while still reachable through
// another -- the silent failure mode, not a loud one.
func TestEveryAccountStaysReadableThroughEveryLookupPathAcrossARezone(t *testing.T) {
	f := newZoneFixture(t)
	ctx := zoneCtx(1)
	require.NoError(t, f.aez.InitGenesisState(ctx, aeztypes.DefaultGenesis()))

	accounts := []nativeaccount.Account{
		activate(t, f.native, ctx, "aez-phase3-paths-a"),
		activate(t, f.native, ctx, "aez-phase3-paths-b"),
	}

	assertReadable := func(t *testing.T, ctx context.Context, when string) {
		t.Helper()
		for _, want := range accounts {
			byUser, found, err := f.native.AccountByUser(ctx, want.AddressUser)
			require.NoError(t, err, when)
			require.True(t, found, "%s: account %s unreachable by user address", when, want.AddressUser)
			require.Equal(t, want, byUser, when)

			byRaw, found, err := f.native.AccountByRaw(ctx, want.AddressRaw)
			require.NoError(t, err, when)
			require.True(t, found, "%s: account %s unreachable by raw address", when, want.AddressUser)
			require.Equal(t, want, byRaw, when)

			status, found, err := f.native.AccountStatus(ctx, want.AddressUser)
			require.NoError(t, err, when)
			require.True(t, found, when)
			require.Equal(t, nativeaccount.AccountStatusActive, status, when)
		}
	}

	assertReadable(t, ctx, "before rezone")

	afterCtx := stageAndActivateTable(t, f, aeztypes.ZoneID(3), 2)

	assertReadable(t, afterCtx, "after rezone")

	// No duplication: the exported set is exactly the accounts we made.
	exported, err := f.native.ExportGenesis(afterCtx)
	require.NoError(t, err)
	require.Len(t, exported.Accounts, len(accounts), "rezone must not duplicate or drop accounts")
}

// TestRezoneAcrossSuccessiveEpochsNeverStrandsAnAccount walks several routing
// epochs, because a single flip could hide an error that only accumulates.
func TestRezoneAcrossSuccessiveEpochsNeverStrandsAnAccount(t *testing.T) {
	f := newZoneFixture(t)
	ctx := zoneCtx(1)
	require.NoError(t, f.aez.InitGenesisState(ctx, aeztypes.DefaultGenesis()))

	account := activate(t, f.native, ctx, "aez-phase3-successive")
	snapshot := f.nativeStore.RawStore().Snapshot()

	// Explicit ordered sequence -- never a map range (I-22).
	steps := []struct {
		version	uint64
		zone	aeztypes.ZoneID
	}{
		{2, aeztypes.ZoneID(1)},
		{3, aeztypes.ZoneID(4)},
		{4, aeztypes.ZoneID(2)},
		{5, aeztypes.ZoneID(0)},
	}
	for _, step := range steps {
		stepCtx := stageAndActivateTable(t, f, step.zone, step.version)

		got, found, err := f.native.AccountByUser(stepCtx, account.AddressUser)
		require.NoError(t, err)
		require.True(t, found, "account stranded after rezone to zone %d", uint32(step.zone))
		require.Equal(t, account, got)

		zone, err := f.native.ZoneOf(stepCtx, account.AddressUser)
		require.NoError(t, err)
		require.Equal(t, uint32(step.zone), zone.Zone)
		require.True(t, zone.Resolved)

		require.Equal(t, snapshot, f.nativeStore.RawStore().Snapshot(),
			"epoch %d rezone rewrote native-account state", step.version)
	}
}

// TestZoneIsUnresolvedAndCoreWithoutAResolver pins the I-23 rule.
//
// A keeper with no resolver -- x/aez absent, disabled, or simply not wired -- must
// still read, write and export every account, and must NOT error. This is the
// rule that a zone-prefixed key layout could not express: a key must always be
// some concrete byte string, so there is no "unresolved" state to fall back from.
func TestZoneIsUnresolvedAndCoreWithoutAResolver(t *testing.T) {
	k := nativeaccountkeeper.NewPersistentKeeper(kvtest.NewStoreService())
	ctx := zoneCtx(1)

	account := activate(t, k, ctx, "aez-phase3-no-resolver")

	zone, err := k.ZoneOf(ctx, account.AddressUser)
	require.NoError(t, err, "an absent x/aez must never fail a native-account caller (I-23)")
	require.Equal(t, nativeaccountkeeper.CoreZone, zone.Zone)
	require.False(t, zone.Resolved, "Resolved must expose the fallback rather than assert a real zone 0")

	got, found, err := k.AccountByUser(ctx, account.AddressUser)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, account, got)
}

// TestAccountReadsNeverConsultTheRoutingTable is the liveness guard.
//
// A resolver is wired but x/aez has NO genesis, so any routing-table read errors.
// Every account read must still succeed, because none of them resolves a zone.
// AccountStatus is the one that matters most: app/txhandlers/storage_rent.go
// calls it for EVERY SIGNER OF EVERY TRANSACTION, so if it acquired a routing
// table dependency, an uninitialized x/aez would reject every transaction on the
// chain -- admitting nothing (I-23).
func TestAccountReadsNeverConsultTheRoutingTable(t *testing.T) {
	f := newZoneFixture(t)
	ctx := zoneCtx(1)
	// Deliberately NO InitGenesisState: the routing table does not exist.

	account := activate(t, f.native, ctx, "aez-phase3-no-table")

	got, found, err := f.native.AccountByUser(ctx, account.AddressUser)
	require.NoError(t, err, "AccountByUser must not depend on the routing table")
	require.True(t, found)
	require.Equal(t, account, got)

	status, found, err := f.native.AccountStatus(ctx, account.AddressUser)
	require.NoError(t, err, "the ante path must not depend on the routing table (I-23)")
	require.True(t, found)
	require.Equal(t, nativeaccount.AccountStatusActive, status)

	_, found, err = f.native.AccountByRaw(ctx, account.AddressRaw)
	require.NoError(t, err)
	require.True(t, found)

	_, err = f.native.ExportGenesis(ctx)
	require.NoError(t, err)

	// ZoneOf itself is the ONLY thing that surfaces the missing table, and it
	// is never on a consensus path.
	_, err = f.native.ZoneOf(ctx, account.AddressUser)
	require.ErrorIs(t, err, aeztypes.ErrRoutingTableNotFound)
}

// TestActivationSucceedsWhenZoneResolutionFails proves the event tag can never
// fail an activation: the zone is observability, and trading a real property
// (accounts activate) for a cosmetic one is what I-23 forbids.
func TestActivationSucceedsWhenZoneResolutionFails(t *testing.T) {
	f := newZoneFixture(t)
	ctx := zoneCtx(1)
	// No aez genesis => zoneTag's resolution fails.

	account := activate(t, f.native, ctx, "aez-phase3-activate-no-table")
	require.Equal(t, nativeaccount.AccountStatusActive, account.Status)

	event := requireActivationEvent(t, ctx)
	require.Equal(t, "0", eventAttribute(t, event, "zone"))
	require.Equal(t, "false", eventAttribute(t, event, "zone_resolved"),
		"a degraded tag must say so rather than silently assert zone 0")
}

// TestActivationEventCarriesTheResolvedZone is the observability proof.
func TestActivationEventCarriesTheResolvedZone(t *testing.T) {
	f := newZoneFixture(t)
	ctx := zoneCtx(1)
	require.NoError(t, f.aez.InitGenesisState(ctx, aeztypes.DefaultGenesis()))

	activate(t, f.native, ctx, "aez-phase3-event")

	event := requireActivationEvent(t, ctx)
	require.Equal(t, "0", eventAttribute(t, event, "zone"))
	require.Equal(t, "true", eventAttribute(t, event, "zone_resolved"))
}

// TestQueryServerExposesTheZone proves the zone is observable through the public
// query surface, for both an activated and a virtual account.
func TestQueryServerExposesTheZone(t *testing.T) {
	f := newZoneFixture(t)
	ctx := zoneCtx(1)
	require.NoError(t, f.aez.InitGenesisState(ctx, aeztypes.DefaultGenesis()))
	account := activate(t, f.native, ctx, "aez-phase3-query")

	query := nativeaccountkeeper.NewQueryServerImpl(f.native)

	resp, err := query.Account(ctx, &nativeaccount.QueryAccountRequest{Address: account.AddressUser})
	require.NoError(t, err)
	require.True(t, resp.Found)
	require.Equal(t, uint32(0), resp.Zone)
	require.True(t, resp.ZoneResolved)

	byRaw, err := query.AccountByRaw(ctx, &nativeaccount.QueryAccountByRawRequest{AddressRaw: account.AddressRaw})
	require.NoError(t, err)
	require.True(t, byRaw.Found)
	require.True(t, byRaw.ZoneResolved)
	require.Equal(t, resp.Zone, byRaw.Zone, "both encodings of one account must report one zone (I-6)")

	// A virtual (never-activated) account still has a home zone: the zone is a
	// function of the ADDRESS, not of the record.
	virtualKey := secp256k1.GenPrivKeyFromSecret([]byte("aez-phase3-query-virtual"))
	virtualMsg, err := nativeaccount.NewMsgActivateAccountFromPubKey(virtualKey.PubKey(), 0)
	require.NoError(t, err)
	virtual, err := query.Account(ctx, &nativeaccount.QueryAccountRequest{Address: virtualMsg.AddressUser})
	require.NoError(t, err)
	require.False(t, virtual.Found)
	require.True(t, virtual.Virtual)
	require.True(t, virtual.ZoneResolved, "an inactive address still resolves a zone")
}

// TestZoneFollowsTheAccountAfterARezoneThroughTheQuery ties the two halves
// together: the query is what an indexer sees, and it must report the NEW zone
// while returning the SAME account.
func TestZoneFollowsTheAccountAfterARezoneThroughTheQuery(t *testing.T) {
	f := newZoneFixture(t)
	ctx := zoneCtx(1)
	require.NoError(t, f.aez.InitGenesisState(ctx, aeztypes.DefaultGenesis()))
	account := activate(t, f.native, ctx, "aez-phase3-query-rezone")

	query := nativeaccountkeeper.NewQueryServerImpl(f.native)
	before, err := query.Account(ctx, &nativeaccount.QueryAccountRequest{Address: account.AddressUser})
	require.NoError(t, err)
	require.Equal(t, uint32(0), before.Zone)

	afterCtx := stageAndActivateTable(t, f, aeztypes.ZoneID(4), 2)

	after, err := query.Account(afterCtx, &nativeaccount.QueryAccountRequest{Address: account.AddressUser})
	require.NoError(t, err)
	require.Equal(t, uint32(4), after.Zone)
	require.True(t, after.ZoneResolved)
	require.Equal(t, before.AccountJSON, after.AccountJSON, "the record must be untouched by the rezone")
}

// TestZoneResolutionSurvivesAKeeperRestart is the F-17 regression guard applied
// to the Phase 3 path.
//
// A freshly constructed pair of keepers -- the state of a restarted or
// state-synced node -- must resolve the same zone from the same committed bytes
// as the long-lived pair that wrote them.
func TestZoneResolutionSurvivesAKeeperRestart(t *testing.T) {
	f := newZoneFixture(t)
	ctx := zoneCtx(1)
	require.NoError(t, f.aez.InitGenesisState(ctx, aeztypes.DefaultGenesis()))
	account := activate(t, f.native, ctx, "aez-phase3-restart")

	afterCtx := stageAndActivateTable(t, f, aeztypes.ZoneID(3), 2)

	restartedAEZ := aezkeeper.NewPersistentKeeper(f.aezStore)
	restartedNative := nativeaccountkeeper.NewPersistentKeeper(f.nativeStore).WithZoneResolver(restartedAEZ)

	live, err := f.native.ZoneOf(afterCtx, account.AddressUser)
	require.NoError(t, err)
	restarted, err := restartedNative.ZoneOf(afterCtx, account.AddressUser)
	require.NoError(t, err)
	require.Equal(t, live, restarted, "a restarted node must resolve the same zone (I-20)")
	require.Equal(t, uint32(3), restarted.Zone)
}

// TestZoneResolutionIsPureAcrossRepeatedCalls guards determinism: the same
// committed state must give the same answer every time, with no hidden ordering.
func TestZoneResolutionIsPureAcrossRepeatedCalls(t *testing.T) {
	f := newZoneFixture(t)
	ctx := zoneCtx(1)
	require.NoError(t, f.aez.InitGenesisState(ctx, aeztypes.DefaultGenesis()))
	account := activate(t, f.native, ctx, "aez-phase3-pure")

	first, err := f.native.ZoneOf(ctx, account.AddressUser)
	require.NoError(t, err)
	for i := 0; i < 16; i++ {
		again, err := f.native.ZoneOf(ctx, account.AddressUser)
		require.NoError(t, err)
		require.Equal(t, first, again, "zone resolution must be a pure function of committed state")
	}
}

// TestSystemEntitiesStayInTheCoreZoneUnderAHostileTableViaTheStringFacade proves
// I-10 holds through the NEW code path that Phase 3 introduces.
//
// x/aez has its own pin test over raw bytes; this one is not redundant, because
// ZoneOfAddress is a DIFFERENT entry point -- it takes a display STRING. It
// proves the facade still delegates classification to CanonicalEntityID, which
// matches system entities on raw bytes BEFORE normalization. Had ZoneOfAddress
// hard-coded namespace = native-account (the obvious shortcut, given its only
// consumer is x/native-account), a module account would be normalized into a
// phantom v2 identity, hashed, and land in an elastic bucket -- and money would
// leave the Core Zone.
//
// Both pin layers are covered because they are byte-disjoint and only one of
// them custodies coins:
//
//   - a 20-byte cosmos module account (Layer A) -- FormatAccAddress is the exact
//     formatter app/txhandlers/storage_rent.go uses on every signer, and it
//     round-trips 20 bytes WITHOUT padding, so the verbatim pin match survives
//     the string encoding;
//   - a 32-byte reserved catalog address (Layer B).
func TestSystemEntitiesStayInTheCoreZoneUnderAHostileTableViaTheStringFacade(t *testing.T) {
	f := newZoneFixture(t)
	ctx := zoneCtx(1)
	require.NoError(t, f.aez.InitGenesisState(ctx, aeztypes.DefaultGenesis()))

	// Every bucket -> zone 4, installed through the mechanism.
	hostileCtx := stageAndActivateTable(t, f, aeztypes.ZoneID(4), 2)

	// Fixture check: a normal account really does move, so the pins below are
	// not passing merely because nothing moved.
	account := activate(t, f.native, ctx, "aez-phase3-pin-control")
	moved, err := f.native.ZoneOf(hostileCtx, account.AddressUser)
	require.NoError(t, err)
	require.Equal(t, uint32(4), moved.Zone, "fixture: the hostile table must actually apply")

	pinned := []struct {
		label	string
		address	string
	}{
		{"module-account/nominator-pool", addressing.FormatAccAddress(authtypes.NewModuleAddress("nominator-pool"))},
		{"module-account/fee_collector", addressing.FormatAccAddress(authtypes.NewModuleAddress(authtypes.FeeCollectorName))},
		{"catalog/AETElector", addressing.SystemAddressAETElectorUserFriendly},
	}
	for _, entry := range pinned {
		zone, err := f.aez.ZoneOfAddress(hostileCtx, entry.address)
		require.NoError(t, err, entry.label)
		require.Equal(t, nativeaccountkeeper.CoreZone, zone,
			"%s must stay in the Core Zone under ANY table (I-9/I-10)", entry.label)
	}
}

// TestNativeAccountDoesNotImportAEZ is the structural guard behind
// keeper.ZoneResolver.
//
// x/native-account consumes x/aez through an interface declared on its OWN side,
// satisfied structurally, so no production file in this module may name x/aez at
// all. That keeps the import CYCLE unrepresentable rather than merely absent:
// x/aez/types/pins.go records that acyclicity is conditional on app/accounts
// never gaining an x/native-account/types import, and app/accounts is not a leaf.
//
// _test.go files are deliberately exempt -- this very file imports x/aez to build
// the fixture. A test binary is not the production dependency graph.
func TestNativeAccountDoesNotImportAEZ(t *testing.T) {
	const forbidden = "x/aez"

	moduleRoot, err := filepath.Abs("..")
	require.NoError(t, err)

	checked := 0
	err = filepath.WalkDir(moduleRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		checked++
		for _, imported := range file.Imports {
			require.NotContains(t, imported.Path.Value, forbidden,
				"%s imports %s: x/native-account must reach x/aez only through its own ZoneResolver interface",
				path, imported.Path.Value)
		}
		return nil
	})
	require.NoError(t, err)
	require.Greater(t, checked, 0, "fixture: the walk must actually parse files, else this test is vacuous")
}

func requireActivationEvent(t *testing.T, ctx context.Context) sdk.Event {
	t.Helper()
	for _, event := range sdk.UnwrapSDKContext(ctx).EventManager().Events() {
		if event.Type == nativeaccount.EventTypeAccountActivated {
			return event
		}
	}
	t.Fatalf("no %s event was emitted", nativeaccount.EventTypeAccountActivated)
	return sdk.Event{}
}

func eventAttribute(t *testing.T, event sdk.Event, key string) string {
	t.Helper()
	for _, attribute := range event.Attributes {
		if attribute.Key == key {
			return attribute.Value
		}
	}
	t.Fatalf("event %s has no attribute %q", event.Type, key)
	return ""
}
