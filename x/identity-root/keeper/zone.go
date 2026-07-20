package keeper

import (
	"context"

	"github.com/sovereign-l1/l1/x/identity-root/types"
)

// NameZoneResolver resolves the AEZ zone and canonical bucket of an
// already-normalized FQDN.
//
// It is declared HERE, on the CONSUMER side, and names no x/aez symbol.
// x/aez/keeper.Keeper satisfies it structurally via ZoneOfName/BucketOfName,
// so x/identity-root imports x/aez not at all. This mirrors
// x/native-account/keeper/zone.go's ZoneResolver, which does the same thing
// one namespace over (addresses instead of names) for the identical reason:
// x/aez/types/pins.go documents that the acyclicity of
// x/identity-root -> x/aez is worth keeping unrepresentable rather than
// merely absent today.
//
// Two methods, not one, because they answer two genuinely different
// questions once x/aez/types.CorePinned is taken into account (see NameZone's
// doc below): ZoneOfName answers the routing decision (always Core, for
// every name, forever); BucketOfName answers the underlying hash that
// decision never even consults.
type NameZoneResolver interface {
	ZoneOfName(ctx context.Context, name string) (uint32, error)
	BucketOfName(ctx context.Context, name string) (uint32, error)
}

// NameZone is the resolved AEZ placement of a name: both the zone it actually
// lives in and the canonical bucket its entity id hashes to.
//
// Resolved distinguishes "x/aez answered" from "no resolver is wired, so the
// zero values are a fallback, not an answer" -- the same distinction
// x/native-account/keeper.AccountZone draws for account zones, and for the
// same reason (I-23 analogue: a disabled or unwired x/aez must never make a
// name-zone query panic or lie).
//
// IMPORTANT, and the reason this type carries two fields instead of one: Zone
// and Bucket are NOT the same question once you read x/aez/types/namespace.go.
// NamespaceName is Core-pinned (I-9: "the Core Zone owns the whole registry").
// x/aez/keeper.ResolveZone short-circuits on that pin BEFORE it ever computes
// a bucket or reads the routing table -- so Zone is unconditionally Core for
// every name, permanently, and no governance routing-table update can ever
// change it (x/aez/keeper/multizone_test.go's
// TestActivateSecondZoneRemapsSubsetWhileCoreStaysPinned asserts exactly this
// for "alice.aet"). Bucket is answered by a SEPARATE, unconditional
// computation (BucketOfName) that still runs the real
// CanonicalEntityID+ComputeBucket construction -- the same one
// x/aez/types/bucket_test.go's golden vector freezes at 220 for "alice.aet"
// -- so tooling can see which of the 256 buckets a name WOULD occupy even
// though that bucket is never actually consulted for routing.
//
// Neither field is stored on NameRecord. Both are recomputed on every call
// from the normalized FQDN string alone (Bucket) or from that string plus
// x/aez's Core pin (Zone, which needs no routing-table read at all) -- so
// there is nothing to migrate if x/aez's pin policy or hash domain ever
// changed, and nothing to keep in sync with NameRecord's own fields.
type NameZone struct {
	Zone		uint32
	Bucket		uint32
	Resolved	bool
}

// WithZoneResolver wires AEZ zone/bucket resolution for registered names.
// Without it NameZone degrades to Resolved=false (see NameZone above) rather
// than failing -- the existing bare-keeper unit tests construct a Keeper this
// way and must keep working.
func (k Keeper) WithZoneResolver(zr NameZoneResolver) Keeper {
	k.zoneResolver = zr
	return k
}

// NameZone resolves the current AEZ zone and bucket for name, which need not
// already be registered: both are pure functions of the normalized FQDN
// string (Bucket) or of the FQDN's namespace alone (Zone, Core-pinned -- see
// the NameZone type doc). Registration status is a completely separate
// question, answered by NameRecord.
//
// A nil resolver (x/aez not wired) yields (Resolved=false, nil error) rather
// than an error: a query about zone placement must not become a hard failure
// just because the zone layer happens to be absent, mirroring
// x/native-account/keeper.Keeper.ZoneOf's same rule for account zones.
func (k *Keeper) NameZone(ctx context.Context, name string) (NameZone, error) {
	// Read through viewGenesis (not k.genesis directly) so this answers
	// correctly on a freshly restarted/state-synced persistent keeper too --
	// see viewGenesis's doc comment.
	gs, err := k.viewGenesis(ctx)
	if err != nil {
		return NameZone{}, err
	}
	k.lockR()
	resolver := k.zoneResolver
	k.unlockR()

	normalized, err := types.NormalizeName(name, gs.IdentityParams.RootNamespace)
	if err != nil {
		return NameZone{}, err
	}
	if resolver == nil {
		return NameZone{Resolved: false}, nil
	}
	// Resolved OUTSIDE the read lock: the resolver is x/aez, a different
	// module's keeper with its own locking, and calling into it while still
	// holding this keeper's lock would risk a cross-module lock-ordering
	// hazard for no benefit (zoneResolver is wiring-time only; see
	// WithZoneResolver's doc and the identical pattern in
	// x/native-account/keeper.Keeper.ZoneOf).
	zone, err := resolver.ZoneOfName(ctx, normalized)
	if err != nil {
		return NameZone{Resolved: false}, err
	}
	bucket, err := resolver.BucketOfName(ctx, normalized)
	if err != nil {
		return NameZone{Resolved: false}, err
	}
	return NameZone{Zone: zone, Bucket: bucket, Resolved: true}, nil
}
