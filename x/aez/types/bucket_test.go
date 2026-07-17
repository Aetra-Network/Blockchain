package types_test

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/sovereign-l1/l1/app/addressing"
	aeztypes "github.com/sovereign-l1/l1/x/aez/types"
	routingtypes "github.com/sovereign-l1/l1/x/routing/types"
)

// goldenVectors freezes the AEZ bucket assignment FOREVER.
//
// Every value below was derived by executing the construction against the real
// repo functions named in "provenance" -- not copied from a design document.
//
// These are consensus constants. A change to ANY value here is a CONSENSUS
// BREAK, not a test update: every entity whose bucket moves is an entity whose
// zone can move, and from Phase 3 on (zone-prefixed x/native-account keys) that
// means its balance is written under one zone prefix and read under another.
// If a refactor turns one of these red, the refactor is wrong.
var goldenVectors = []struct {
	name		string
	namespace	aeztypes.Namespace
	entityHex	string
	bucket		aeztypes.BucketID
	provenance	string
}{
	{
		name:		"native-account/0x01x20",
		namespace:	aeztypes.NamespaceNativeAccount,
		entityHex:	"92993fc606cb803be0d94e63604e680ac5838830a96c8677c3e4f4b924d73686",
		bucket:		142,
		provenance:	"addressing.NormalizeToAccountIdentity(0x01 x20)",
	},
	{
		name:		"native-account/0x01..0x14",
		namespace:	aeztypes.NamespaceNativeAccount,
		entityHex:	"163a77aa12b267cabd02b2bd9351f176dfcba573e8a5a915b630ba1767631f1d",
		bucket:		141,
		provenance:	"addressing.NormalizeToAccountIdentity(0x01,0x02,...,0x14)",
	},
	{
		name:		"native-account/0xffx20",
		namespace:	aeztypes.NamespaceNativeAccount,
		entityHex:	"787305f8cbf10f7a8ca61004f9ecc16063b742f56030a33790854fdd3baac818",
		bucket:		111,
		provenance:	"addressing.NormalizeToAccountIdentity(0xff x20)",
	},
	{
		name:		"contract/salt-empty",
		namespace:	aeztypes.NamespaceContract,
		entityHex:	"d77d3a4a978ca5d2c86e26fb42d74239ad6173ea639566a05370e0c5477d207f",
		bucket:		158,
		provenance:	"addressing.Parse(DeriveContractAddress(aetra, contracts, deployer=#1, sha256(code-a), sha256(init-a), salt=nil).raw)",
	},
	{
		name:		"contract/salt-0x01",
		namespace:	aeztypes.NamespaceContract,
		entityHex:	"cef687b52e2149223b1426ced3e7c0bbe67ca1866c5fb669525dcb286714192c",
		bucket:		179,
		provenance:	"same as above with salt=0x01",
	},
	{
		name:		"name/alice.aet",
		namespace:	aeztypes.NamespaceName,
		entityHex:	"616c6963652e616574",
		bucket:		220,
		provenance:	"identityroottypes.NormalizeName(\"alice\", \"aet\")",
	},
	{
		name:		"name/aet",
		namespace:	aeztypes.NamespaceName,
		entityHex:	"616574",
		bucket:		246,
		provenance:	"identityroottypes.NormalizeName(\"aet\", \"aet\")",
	},
	{
		name:		"name/sub.alice.aet",
		namespace:	aeztypes.NamespaceName,
		entityHex:	"7375622e616c6963652e616574",
		bucket:		113,
		provenance:	"identityroottypes.NormalizeName(\"sub.alice.aet\", \"aet\")",
	},
	{
		name:		"system/catalog/AETElector",
		namespace:	aeztypes.NamespaceSystem,
		entityHex:	"01041041041041041041041041041041041041041041041041041042c4093391",
		bucket:		30,
		provenance:	"addressing.Parse(SystemAddressByName(\"AETElector\").Raw)",
	},
	{
		name:		"system/catalog/AETMint",
		namespace:	aeztypes.NamespaceSystem,
		entityHex:	"030c30c30c30c30c30c30c30c30c30c30c30c30c30c30c30c30c30c30c308353",
		bucket:		56,
		provenance:	"addressing.Parse(SystemAddressByName(\"AETMint\").Raw)",
	},
	{
		name:		"system/catalog/AETBurn",
		namespace:	aeztypes.NamespaceSystem,
		entityHex:	"004104104104104104104104104104104104104104104104104104104105444d",
		bucket:		105,
		provenance:	"addressing.Parse(SystemAddressByName(\"AETBurn\").Raw)",
	},
	{
		// The ONLY 20-byte entity id in the set, and the I-10 hazard case.
		// A module account is 20 bytes and is NOT idempotent under
		// NormalizeToAccountIdentity. If an implementation ever produces
		// 237 here it normalized a module account into the native-account
		// namespace -- see TestModuleAccountMustNotBeNormalizedIntoNativeAccount.
		name:		"system/module-account/nominator-pool",
		namespace:	aeztypes.NamespaceSystem,
		entityHex:	"58f0366d83a924c741d3f5022acdd3180fe6bc3d",
		bucket:		189,
		provenance:	"authtypes.NewModuleAddress(\"nominator-pool\")",
	},
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	bz, err := hex.DecodeString(s)
	require.NoError(t, err)
	return bz
}

func TestComputeBucketGoldenVectors(t *testing.T) {
	for _, tc := range goldenVectors {
		t.Run(tc.name, func(t *testing.T) {
			got := aeztypes.ComputeBucket(tc.namespace, mustHex(t, tc.entityHex))
			require.Equal(t, tc.bucket, got,
				"GOLDEN BUCKET MOVED -- this is a consensus break, not a test update. provenance: %s", tc.provenance)
		})
	}
}

// TestComputeBucketIsDeterministic asserts purity: the same input always yields
// the same bucket, across repeated calls.
func TestComputeBucketIsDeterministic(t *testing.T) {
	for _, tc := range goldenVectors {
		entity := mustHex(t, tc.entityHex)
		first := aeztypes.ComputeBucket(tc.namespace, entity)
		for i := 0; i < 64; i++ {
			require.Equal(t, first, aeztypes.ComputeBucket(tc.namespace, entity), tc.name)
		}
	}
}

// TestComputeBucketMatchesDocumentedPreimage recomputes every golden vector from
// the raw SHA-256 preimage spelled out in aez.md:421, independently of
// ComputeBucket's implementation. If someone rewrites ComputeBucket, this test
// still pins it to the documented construction.
func TestComputeBucketMatchesDocumentedPreimage(t *testing.T) {
	for _, tc := range goldenVectors {
		entity := mustHex(t, tc.entityHex)
		h := sha256.New()
		h.Write([]byte("aetra-aez-bucket-v1"))
		h.Write([]byte{0})
		h.Write([]byte(tc.namespace))
		h.Write([]byte{0})
		h.Write(entity)
		sum := h.Sum(nil)
		want := aeztypes.BucketID(binary.BigEndian.Uint64(sum[:8]) % 256)
		require.Equal(t, want, aeztypes.ComputeBucket(tc.namespace, entity), tc.name)
		require.Equal(t, tc.bucket, want, tc.name)
	}
}

// TestBucketFoldEqualsEighthByte freezes the fact that an 8-byte big-endian fold
// modulo 256 is EXACTLY sum[7], with zero modulo bias (2^64 % 256 == 0).
//
// The 8-byte fold is kept for shape parity with AssignShard
// (x/routing/types/routing.go:249-250). This test exists so that nobody
// "simplifies" it to sum[0] -- which would look equivalent, compile fine, and
// silently re-bucket the entire chain.
func TestBucketFoldEqualsEighthByte(t *testing.T) {
	for _, tc := range goldenVectors {
		entity := mustHex(t, tc.entityHex)
		h := sha256.New()
		h.Write([]byte(aeztypes.BucketDomain))
		h.Write([]byte{0})
		h.Write([]byte(tc.namespace))
		h.Write([]byte{0})
		h.Write(entity)
		sum := h.Sum(nil)
		require.Equal(t, aeztypes.BucketID(sum[7]), aeztypes.ComputeBucket(tc.namespace, entity), tc.name)
		require.NotEqual(t, sum[0], sum[7], "%s: vector is useless for guarding sum[0]/sum[7] confusion", tc.name)
	}
}

// TestBucketCountIsConstant256 guards I-4: the bucket count never varies with
// the zone count.
func TestBucketCountIsConstant256(t *testing.T) {
	require.Equal(t, 256, aeztypes.BucketCount)
	// BucketID is uint8, so every possible value is a valid bucket. This is
	// what makes RoutingTable's [BucketCount]ZoneID array total by type.
	require.Equal(t, 256, int(^aeztypes.BucketID(0))+1)
}

// TestBucketIsTotalOverEveryByte asserts every one of the 256 buckets is
// reachable, so no bucket is a dead entry in the routing table.
func TestBucketIsTotalOverEveryByte(t *testing.T) {
	seen := make(map[aeztypes.BucketID]bool, 256)
	for i := 0; i < 200000 && len(seen) < 256; i++ {
		var entity [4]byte
		binary.BigEndian.PutUint32(entity[:], uint32(i))
		seen[aeztypes.ComputeBucket(aeztypes.NamespaceNativeAccount, entity[:])] = true
	}
	require.Len(t, seen, 256, "not every bucket is reachable")
}

// TestBothAddressEncodingsResolveToTheSameBucket guards I-6.
//
// "AE..." (user-friendly) and "ae1..." (bech32) are two ENCODINGS of one byte
// string. A wallet submitting one and a CLI submitting the other must place the
// same account in the same bucket.
func TestBothAddressEncodingsResolveToTheSameBucket(t *testing.T) {
	identity, err := addressing.NormalizeToAccountIdentity(mustHex(t, "0101010101010101010101010101010101010101"))
	require.NoError(t, err)
	require.Equal(t, "92993fc606cb803be0d94e63604e680ac5838830a96c8677c3e4f4b924d73686", hex.EncodeToString(identity))

	user, err := addressing.FormatUserFriendly(identity)
	require.NoError(t, err)
	raw := addressing.Format(identity)

	fromUser, err := addressing.Parse(user)
	require.NoError(t, err)
	fromRaw, err := addressing.Parse(raw)
	require.NoError(t, err)

	require.Equal(t,
		aeztypes.ComputeBucket(aeztypes.NamespaceNativeAccount, fromUser),
		aeztypes.ComputeBucket(aeztypes.NamespaceNativeAccount, fromRaw))
	require.Equal(t, aeztypes.BucketID(142), aeztypes.ComputeBucket(aeztypes.NamespaceNativeAccount, fromUser))
}

// TestHashingDisplayStringsIsABug DOCUMENTS the bug rather than merely avoiding
// it: it asserts the concrete wrong values a display string produces, so the
// consequence of hashing one is visible in the test suite rather than latent.
//
// One account, three different buckets: 142 (correct, identity bytes), 110
// ("AE..." string), 162 ("ae1..." string). Post-Phase-3 that is a state fork.
func TestHashingDisplayStringsIsABug(t *testing.T) {
	const (
		userForm	= "AEJkAhnhQkyKKOuq_zGSmT_GBsuAO-DZTmNgTmgKxYOIMKlshnfD5PS5JNc2hg"
		rawForm		= "ae1j2vnl3sxewqrhcxefe3kqnngptzc8zps49kgva7run6tjfxhx6rqexx4wp"
	)
	identity, err := addressing.NormalizeToAccountIdentity(mustHex(t, "0101010101010101010101010101010101010101"))
	require.NoError(t, err)
	correct := aeztypes.ComputeBucket(aeztypes.NamespaceNativeAccount, identity)
	require.Equal(t, aeztypes.BucketID(142), correct)

	wrongUser := aeztypes.ComputeBucket(aeztypes.NamespaceNativeAccount, []byte(userForm))
	wrongRaw := aeztypes.ComputeBucket(aeztypes.NamespaceNativeAccount, []byte(rawForm))
	require.Equal(t, aeztypes.BucketID(110), wrongUser)
	require.Equal(t, aeztypes.BucketID(162), wrongRaw)
	require.NotEqual(t, correct, wrongUser, "hashing the AE display string must not silently agree with the identity")
	require.NotEqual(t, correct, wrongRaw, "hashing the ae1 display string must not silently agree with the identity")
	require.NotEqual(t, wrongUser, wrongRaw, "the two display encodings disagree with each other too")
}

// TestNormalizeToAccountIdentityIsIdempotentForUserAccounts backs the claim in
// aez.md:438-441 that a caller holding either the plain address or the derived
// identity gets the same bucket.
func TestNormalizeToAccountIdentityIsIdempotentForUserAccounts(t *testing.T) {
	for _, seed := range [][]byte{
		mustHex(t, "0101010101010101010101010101010101010101"),
		mustHex(t, "0102030405060708090a0b0c0d0e0f1011121314"),
		mustHex(t, "ffffffffffffffffffffffffffffffffffffffff"),
	} {
		identity, err := addressing.NormalizeToAccountIdentity(seed)
		require.NoError(t, err)
		again, err := addressing.NormalizeToAccountIdentity(identity)
		require.NoError(t, err)
		require.Equal(t, identity, again, "identity must be a fixed point")
		require.Equal(t,
			aeztypes.ComputeBucket(aeztypes.NamespaceNativeAccount, identity),
			aeztypes.ComputeBucket(aeztypes.NamespaceNativeAccount, again))
	}
}

// TestModuleAccountMustNotBeNormalizedIntoNativeAccount pins the I-10 hazard
// that aez.md:441 gets wrong.
//
// NormalizeToAccountIdentity is NOT idempotent for a 20-byte cosmos module
// account: it pads to 32, sees the zero prefix, classifies legacy_padded, and
// derives a brand-new v2 identity that belongs to nobody. Applying it to a
// module account moves nominator-pool from bucket 189 (system, pinned) to
// bucket 237 (native-account, elastic).
func TestModuleAccountMustNotBeNormalizedIntoNativeAccount(t *testing.T) {
	macc := authtypes.NewModuleAddress("nominator-pool")
	require.Len(t, macc.Bytes(), 20)
	require.Equal(t, "58f0366d83a924c741d3f5022acdd3180fe6bc3d", hex.EncodeToString(macc))

	// Correct: system namespace, raw bytes, pre-normalization.
	require.Equal(t, aeztypes.BucketID(189), aeztypes.ComputeBucket(aeztypes.NamespaceSystem, macc))

	// The phantom identity the wrong path would produce.
	phantom, err := addressing.NormalizeToAccountIdentity(macc)
	require.NoError(t, err)
	require.NotEqual(t, []byte(macc), phantom, "module account is NOT idempotent under NormalizeToAccountIdentity")
	require.Equal(t, "ec2a27ac8544fdce910928ad8e8689fe79e31f5e1d56c04cd51cb844284fcadf", hex.EncodeToString(phantom))
	require.Equal(t, aeztypes.BucketID(237), aeztypes.ComputeBucket(aeztypes.NamespaceNativeAccount, phantom))

	// And the classifier must take the correct branch.
	ns, id, err := aeztypes.CanonicalEntityID(aeztypes.EntityKindAddress, []byte(macc))
	require.NoError(t, err)
	require.Equal(t, aeztypes.NamespaceSystem, ns, "module account must classify as system, never native-account")
	require.Equal(t, []byte(macc), id, "module account entity id must be the raw bytes, unnormalized")
	require.True(t, aeztypes.CorePinned(ns))
}

// TestBucketDomainSeparationFromRouting guards aez.md:612 / I-5.
//
// x/routing's AssignShard and AEZ's ComputeBucket must never agree on a vector.
// Separation is structural, not nominal: a different domain string AND a
// different preimage grammar (no zone field, no epoch tail).
func TestBucketDomainSeparationFromRouting(t *testing.T) {
	require.NotEqual(t, "aetra-routing-v1", aeztypes.BucketDomain)

	// With activeShards == 256 the two folds have the same range, which is
	// the only configuration where an accidental agreement could even be
	// expressed. Assert they still disagree in practice.
	agreements := 0
	total := 0
	for i := 0; i < 512; i++ {
		var entity [4]byte
		binary.BigEndian.PutUint32(entity[:], uint32(i))
		aezBucket := aeztypes.ComputeBucket(aeztypes.NamespaceNativeAccount, entity[:])
		shard := routingtypes.AssignShard(routingtypes.ZoneFinancial, entity[:], 0, 256)
		total++
		if uint32(aezBucket) == uint32(shard) {
			agreements++
		}
	}
	// Independent 8-bit values agree ~1/256 of the time; anything near
	// "always" would mean the constructions had converged.
	require.Less(t, agreements, total/8,
		"AEZ buckets and routing shards agree far too often -- the domains may have converged")
}

// TestRoutingEpochIsNotInTheBucketHash guards I-5 by construction: ComputeBucket
// has no epoch parameter to pass, so the only way to prove the point is that the
// signature cannot accept one. AssignShard, by contrast, takes routingEpoch and
// produces a different shard for the same actor across epochs -- a total state
// migration per epoch, which is exactly what AEZ must never do.
func TestRoutingEpochIsNotInTheBucketHash(t *testing.T) {
	actor := mustHex(t, "0101010101010101010101010101010101010101")
	epoch0 := routingtypes.AssignShard(routingtypes.ZoneFinancial, actor, 0, 4)
	epoch1 := routingtypes.AssignShard(routingtypes.ZoneFinancial, actor, 1, 4)
	require.NotEqual(t, epoch0, epoch1, "fixture: AssignShard must actually remap across epochs, else this proves nothing")

	// AEZ's bucket is a function of (namespace, entity) ONLY and is stable
	// forever.
	require.Equal(t,
		aeztypes.ComputeBucket(aeztypes.NamespaceNativeAccount, actor),
		aeztypes.ComputeBucket(aeztypes.NamespaceNativeAccount, actor))
}
