package types_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/sovereign-l1/l1/app/accounts"
	"github.com/sovereign-l1/l1/app/addressing"
	aeztypes "github.com/sovereign-l1/l1/x/aez/types"
	"github.com/sovereign-l1/l1/x/internal/prototype"
)

// Golden pin-set facts. Both source lists are edited by DEVELOPERS, not
// governance, so a new module account or catalog address is a SILENT pin-set
// change. Freezing the count and the digest turns that into a loud failure.
//
// If this test fails you added or removed a system entity. That is allowed --
// but confirm the entity is Core-pinned on purpose and then update these
// constants deliberately, in the same commit.
const (
	// ANS Phase A added the identity-root (.aet collection) module account to
	// moduleAccountPermissions: 23 -> 24. Its custodied deposits/escrows must
	// live in the Core Zone, so it is core-pinned on purpose -- the pin set is
	// computed from GetMaccPerms(), so this is automatic; only these golden
	// constants (and the digest below) are updated deliberately.
	goldenModuleAccountCount	= 24
	goldenCatalogCount		= 29
	goldenAuthorityCount		= 1
	goldenPinCount			= goldenModuleAccountCount + goldenCatalogCount + goldenAuthorityCount
)

func TestSystemPinSetCountIsFrozen(t *testing.T) {
	entries, err := aeztypes.SystemPinSet()
	require.NoError(t, err)
	require.Len(t, entries, goldenPinCount)

	byLayer := map[aeztypes.SystemPinLayer]int{}
	for _, entry := range entries {
		byLayer[entry.Layer]++
	}
	require.Equal(t, goldenModuleAccountCount, byLayer[aeztypes.SystemPinLayerModuleAccount])
	require.Equal(t, goldenCatalogCount, byLayer[aeztypes.SystemPinLayerCatalog])
	require.Equal(t, goldenAuthorityCount, byLayer[aeztypes.SystemPinLayerAuthority])
}

func TestSystemPinDigestIsFrozen(t *testing.T) {
	digest, err := aeztypes.SystemPinDigest()
	require.NoError(t, err)
	require.Equal(t,
		"678d36b7d5c39a471e2554522b4fadb44382410e55b0755597aabb006409fa5a",
		digest,
		"the system pin set changed -- confirm every added entity must be core-pinned, then update this digest deliberately")
}

// TestSystemPinSetIsDeterministicallyOrdered guards I-22. GetMaccPerms returns a
// MAP, so an implementation that iterated it directly would produce a different
// order per run -- harmless for a lookup, fatal for the digest above.
func TestSystemPinSetIsDeterministicallyOrdered(t *testing.T) {
	first, err := aeztypes.SystemPinSet()
	require.NoError(t, err)
	for i := 0; i < 32; i++ {
		again, err := aeztypes.SystemPinSet()
		require.NoError(t, err)
		require.Equal(t, first, again, "pin set order is not deterministic across runs")
	}
	for i := 1; i < len(first); i++ {
		prev, cur := first[i-1], first[i]
		if prev.Layer == cur.Layer {
			require.Negative(t, bytes.Compare(prev.EntityID, cur.EntityID), "entries within a layer must ascend by entity id")
		} else {
			require.Less(t, string(prev.Layer), string(cur.Layer), "layers must ascend")
		}
	}
}

// TestModuleAccountAndCatalogLayersAreDisjoint backs the "two-layer address
// model": every reserved system entity has BOTH a catalog vanity address AND a
// distinct cosmos module account. Pinning one and not the other would strand the
// other in an elastic zone.
func TestModuleAccountAndCatalogLayersAreDisjoint(t *testing.T) {
	entries, err := aeztypes.SystemPinSet()
	require.NoError(t, err)

	seen := map[string]aeztypes.SystemPinEntry{}
	for _, entry := range entries {
		previous, duplicate := seen[string(entry.EntityID)]
		require.False(t, duplicate, "pin set has byte-duplicate entries: %s and %s", previous.Label, entry.Label)
		seen[string(entry.EntityID)] = entry
	}

	// And specifically: each of the 12 joins has a catalog address that
	// differs from its module account.
	for _, reserved := range accounts.ReservedSystemModuleAccounts() {
		catalog, err := addressing.Parse(reserved.Raw)
		require.NoError(t, err)
		macc := authtypes.NewModuleAddress(reserved.ModuleAccountName)
		require.NotEqual(t, catalog, []byte(macc),
			"%s: catalog address must differ from its module account (two-layer model)", reserved.Name)
	}
}

// TestEverySystemEntityIsPinnedPreNormalization is the I-10 guard.
//
// Every entity in the union must classify as NamespaceSystem when passed as RAW
// BYTES, and its entity id must come back verbatim. If any fell through to
// native-account it would be normalized into a phantom identity and hashed into
// an ELASTIC bucket -- and "money never leaves the Core Zone" would break for
// exactly the fund-bearing accounts.
func TestEverySystemEntityIsPinnedPreNormalization(t *testing.T) {
	entries, err := aeztypes.SystemPinSet()
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	for _, entry := range entries {
		t.Run(entry.Label, func(t *testing.T) {
			found, ok, err := aeztypes.IsSystemEntity(entry.EntityID)
			require.NoError(t, err)
			require.True(t, ok, "%s is not recognized as a system entity", entry.Label)
			require.Equal(t, entry.EntityID, found.EntityID)

			ns, id, err := aeztypes.CanonicalEntityID(aeztypes.EntityKindAddress, entry.EntityID)
			require.NoError(t, err)
			require.Equal(t, aeztypes.NamespaceSystem, ns, "%s escaped the system namespace", entry.Label)
			require.Equal(t, entry.EntityID, id, "%s entity id must be raw bytes, unnormalized", entry.Label)
			require.True(t, aeztypes.CorePinned(ns))
		})
	}
}

// TestEveryModuleAccountIsPinned is the gate: adding a module account to
// moduleAccountPermissions without pinning it must fail CI rather than silently
// place a fund-bearing account in an elastic zone.
func TestEveryModuleAccountIsPinned(t *testing.T) {
	for name := range accounts.ModuleAccountPermissions() {
		macc := authtypes.NewModuleAddress(name)
		_, ok, err := aeztypes.IsSystemEntity(macc)
		require.NoError(t, err)
		require.True(t, ok, "module account %q is not in the AEZ system pin set", name)

		ns, _, err := aeztypes.CanonicalEntityID(aeztypes.EntityKindAddress, []byte(macc))
		require.NoError(t, err)
		require.Equal(t, aeztypes.NamespaceSystem, ns, "module account %q must be core-pinned", name)
	}
}

// TestEveryCatalogAddressIsPinned is the same gate for the reserved catalog.
func TestEveryCatalogAddressIsPinned(t *testing.T) {
	for _, address := range addressing.AllSystemAddresses() {
		bz, err := addressing.Parse(address.Raw)
		require.NoError(t, err)
		_, ok, err := aeztypes.IsSystemEntity(bz)
		require.NoError(t, err)
		require.True(t, ok, "catalog address %s is not in the AEZ system pin set", address.Name)
	}
}

// TestCatalogCoreFlagIsNotAnAEZZoneFlag is an audit-trap guard.
//
// SystemAddress.Core is true for only 6 of the 29 catalog entries and predates
// AEZ. It is NOT an AEZ zone flag: AETMint, AETBurn, AETFeeCollector and
// AETTreasury are all Core=false yet are exactly the money entities that must be
// pinned to zone 0 permanently. ALL 29 pin regardless of the flag.
func TestCatalogCoreFlagIsNotAnAEZZoneFlag(t *testing.T) {
	nonCore := 0
	for _, address := range addressing.AllSystemAddresses() {
		if !address.Core {
			nonCore++
		}
		bz, err := addressing.Parse(address.Raw)
		require.NoError(t, err)
		ns, _, err := aeztypes.CanonicalEntityID(aeztypes.EntityKindAddress, bz)
		require.NoError(t, err)
		require.Equal(t, aeztypes.NamespaceSystem, ns,
			"%s (Core=%v) must pin regardless of the legacy Core flag", address.Name, address.Core)
	}
	require.Positive(t, nonCore, "fixture: some catalog entries must be Core=false, else this proves nothing")
}

// TestNominatorPoolBothLayersArePinned covers the specifically flagged case: the
// pool is deliberately UNBLOCKED (so x/distribution can pay it), which makes it
// look like a user account to a "pin only blocked addresses" heuristic. It is
// also the one system account with live cross-module traffic, so a missed pin
// would strand real custodied deposits across a zone boundary.
func TestNominatorPoolBothLayersArePinned(t *testing.T) {
	macc := authtypes.NewModuleAddress("nominator-pool")
	catalog, found := addressing.SystemAddressByName("AETNominatorPool")
	require.True(t, found)
	catalogBytes, err := addressing.Parse(catalog.Raw)
	require.NoError(t, err)

	require.NotEqual(t, []byte(macc), catalogBytes, "the two layers must be distinct addresses")

	for _, entity := range [][]byte{macc, catalogBytes} {
		ns, id, err := aeztypes.CanonicalEntityID(aeztypes.EntityKindAddress, entity)
		require.NoError(t, err)
		require.Equal(t, aeztypes.NamespaceSystem, ns)
		require.Equal(t, entity, id)
	}

	// It is NOT in BlockedAddresses -- proving pin membership does not
	// depend on bank blocked-ness.
	require.NotContains(t, accounts.BlockedAddresses(), macc.String())
}

// TestBlockedAddressesIsTheWrongPinAccessor documents why the obvious accessor
// is a trap: it deliberately drops gov, nominator-pool and AETBurn, which are
// among the entities most needing a pin.
func TestBlockedAddressesIsTheWrongPinAccessor(t *testing.T) {
	blocked := accounts.BlockedAddresses()
	for _, name := range []string{"gov", "nominator-pool"} {
		macc := authtypes.NewModuleAddress(name)
		require.NotContains(t, blocked, macc.String(), "fixture: %s is expected to be unblocked", name)
		_, ok, err := aeztypes.IsSystemEntity(macc)
		require.NoError(t, err)
		require.True(t, ok, "%s must still be pinned despite being unblocked", name)
	}
}

// TestPrototypeDefaultAuthorityIsPinned covers the 53rd entity that no existing
// accessor enumerates. It is a keyless sentinel gating param updates on every
// prototype and system module, and it is NOT the gov module address.
func TestPrototypeDefaultAuthorityIsPinned(t *testing.T) {
	authority, err := addressing.Parse(prototype.DefaultAuthority)
	require.NoError(t, err)

	_, ok, err := aeztypes.IsSystemEntity(authority)
	require.NoError(t, err)
	require.True(t, ok, "prototype.DefaultAuthority must be pinned")

	ns, _, err := aeztypes.CanonicalEntityID(aeztypes.EntityKindAddress, authority)
	require.NoError(t, err)
	require.Equal(t, aeztypes.NamespaceSystem, ns)

	// Two distinct authorities coexist; they must not be confused.
	gov := authtypes.NewModuleAddress("gov")
	require.NotEqual(t, authority, []byte(gov), "prototype authority is not the gov module account")
}

// TestBothEncodingsPinForEverySystemEntity guards I-6 across the whole pin set:
// an entity supplied as "AE..." or "ae1..." must pin exactly as its bytes do.
func TestBothEncodingsPinForEverySystemEntity(t *testing.T) {
	entries, err := aeztypes.SystemPinSet()
	require.NoError(t, err)
	for _, entry := range entries {
		user, err := addressing.FormatUserFriendly(entry.EntityID)
		require.NoError(t, err, entry.Label)
		raw := addressing.Format(entry.EntityID)

		for _, form := range []string{user, raw} {
			ns, _, err := aeztypes.CanonicalEntityID(aeztypes.EntityKindAddress, form)
			require.NoError(t, err, "%s via %s", entry.Label, form)
			require.Equal(t, aeztypes.NamespaceSystem, ns, "%s via %s escaped the system namespace", entry.Label, form)
		}
	}
}

func TestCorePinnedNamespaces(t *testing.T) {
	require.True(t, aeztypes.CorePinned(aeztypes.NamespaceSystem))
	require.True(t, aeztypes.CorePinned(aeztypes.NamespaceName))
	require.False(t, aeztypes.CorePinned(aeztypes.NamespaceNativeAccount))
	require.False(t, aeztypes.CorePinned(aeztypes.NamespaceContract))
}

// TestNamespaceTokensAreNulFree asserts the ONE invariant that ComputeBucket's
// single-0x00 framing depends on for injectivity.
func TestNamespaceTokensAreNulFree(t *testing.T) {
	for _, ns := range aeztypes.AllNamespaces() {
		require.NoError(t, ns.Validate())
		require.NotContains(t, string(ns), "\x00", "namespace %q contains NUL -- bucket framing is no longer injective", ns)
		for i := 0; i < len(ns); i++ {
			require.Less(t, ns[i], byte(0x80), "namespace %q must be ASCII", ns)
		}
	}
	require.ErrorIs(t, aeztypes.Namespace("nope").Validate(), aeztypes.ErrInvalidNamespace)
	require.ErrorIs(t, aeztypes.Namespace("").Validate(), aeztypes.ErrInvalidNamespace)
}

// TestNamespaceFramingIsInjective is the empirical companion to the framing
// argument in ComputeBucket's doc comment: no two (namespace, entity) pairs may
// share a preimage. The classic attack is a namespace/entity boundary shift.
func TestNamespaceFramingIsInjective(t *testing.T) {
	// "native-account" + "\x00x" vs "native-account\x00x" + "" would collide
	// under naive concatenation. With the 0x00 delimiter and a NUL-free
	// namespace set, they cannot.
	seen := map[aeztypes.BucketID][]string{}
	collisionSuspects := []struct {
		ns	aeztypes.Namespace
		entity	[]byte
	}{
		{aeztypes.NamespaceName, []byte("x")},
		{aeztypes.NamespaceName, []byte("\x00x")},
		{aeztypes.NamespaceSystem, []byte("x")},
		{aeztypes.NamespaceContract, []byte("x")},
		{aeztypes.NamespaceNativeAccount, []byte("x")},
	}
	preimages := map[string]bool{}
	for _, tc := range collisionSuspects {
		preimage := string(aeztypes.BucketDomain) + "\x00" + string(tc.ns) + "\x00" + string(tc.entity)
		require.False(t, preimages[preimage], "preimage collision for ns=%q entity=%q", tc.ns, tc.entity)
		preimages[preimage] = true
		bucket := aeztypes.ComputeBucket(tc.ns, tc.entity)
		seen[bucket] = append(seen[bucket], preimage)
	}
}
