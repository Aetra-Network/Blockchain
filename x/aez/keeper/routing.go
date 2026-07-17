package keeper

import (
	"context"

	"github.com/sovereign-l1/l1/x/aez/types"
)

// ZoneResolution is the full, auditable account of how a zone was decided.
//
// Hashed is the field that matters. A test asserting only Zone == 0 passes
// VACUOUSLY in Phase 1, because every bucket already maps to zone 0 -- it would
// prove nothing for four phases and then start failing silently the moment a
// real table lands. Recording whether the bucket hash was entered at all is what
// makes the Core-pin test meaningful TODAY: a pinned entity must reach zone 0
// WITHOUT hashing (I-9).
type ZoneResolution struct {
	// Zone is the resolved zone.
	Zone	types.ZoneID
	// Namespace is the namespace the entity classified into.
	Namespace	types.Namespace
	// Pinned reports whether CorePinned short-circuited the resolution.
	Pinned	bool
	// Hashed reports whether ComputeBucket was entered. Always false when
	// Pinned is true.
	Hashed	bool
	// Bucket is the computed bucket. Meaningful only when Hashed is true.
	Bucket	types.BucketID
	// TableVersion is the routing table version consulted. Meaningful only
	// when Hashed is true.
	TableVersion	uint64
}

// ZoneOf resolves the zone for a canonical (namespace, entityID) pair.
//
// Order is the invariant: PINS FIRST, hash second.
//
// A Core-pinned namespace returns zone 0 before the bucket hash is entered and
// before the routing table is even read. That is what makes "the Core Zone never
// migrates" structural (I-9): the table is never consulted, so no table version
// -- including a hand-crafted malicious one that maps every bucket to zone 4 --
// can express a Core-Zone move. pins_test.go proves exactly that.
func (k Keeper) ZoneOf(ctx context.Context, ns types.Namespace, entityID []byte) (types.ZoneID, error) {
	resolution, err := k.ResolveZone(ctx, ns, entityID)
	if err != nil {
		return types.ZoneIDCore, err
	}
	return resolution.Zone, nil
}

// ResolveZone is ZoneOf with the full resolution record. It is the instrumented
// form the pin invariant test asserts against.
func (k Keeper) ResolveZone(ctx context.Context, ns types.Namespace, entityID []byte) (ZoneResolution, error) {
	if err := ns.Validate(); err != nil {
		return ZoneResolution{}, err
	}
	if len(entityID) == 0 {
		return ZoneResolution{}, types.ErrInvalidEntity
	}
	if types.CorePinned(ns) {
		// Short-circuit. No hash, no table read.
		return ZoneResolution{
			Zone:		types.ZoneIDCore,
			Namespace:	ns,
			Pinned:		true,
			Hashed:		false,
		}, nil
	}
	table, err := k.GetRoutingTable(ctx)
	if err != nil {
		return ZoneResolution{}, err
	}
	bucket := types.ComputeBucket(ns, entityID)
	return ZoneResolution{
		Zone:		table.ZoneForBucket(bucket),
		Namespace:	ns,
		Pinned:		false,
		Hashed:		true,
		Bucket:		bucket,
		TableVersion:	table.Version,
	}, nil
}

// ZoneOfEntity classifies a caller-supplied entity and resolves its zone in one
// step. It is the form callers should prefer: CanonicalEntityID owns the
// system-first classification that enforces I-10, so a caller cannot accidentally
// route a module account through the native-account path.
func (k Keeper) ZoneOfEntity(ctx context.Context, kind types.EntityKind, entity any) (ZoneResolution, error) {
	ns, entityID, err := types.CanonicalEntityID(kind, entity)
	if err != nil {
		return ZoneResolution{}, err
	}
	return k.ResolveZone(ctx, ns, entityID)
}

// ZoneOfAddress resolves the home zone of an account-shaped address and returns
// it as a plain uint32.
//
// The SIGNATURE is the point: it names no x/aez type. A consumer module
// (x/native-account, Phase 3) declares a one-method interface of this exact
// shape on its OWN side and this keeper satisfies it STRUCTURALLY -- so the
// consumer imports x/aez not at all.
//
// That is not a stylistic nicety. types/pins.go records that the absence of an
// import cycle between x/aez and x/native-account is CONDITIONAL: app/accounts
// is not a leaf, and the day anything adds x/native-account/types to it, a
// direct x/native-account -> x/aez import would close the cycle. A structural
// interface makes that question UNREPRESENTABLE rather than merely
// answered-for-now.
//
// Classification is deliberately owned here rather than by the caller.
// CanonicalEntityID resolves system entities FIRST, on raw bytes, before any
// normalization -- which is what keeps a module account pinned to zone 0 (I-10).
// A caller that hard-coded "namespace = native-account" would silently break
// that, so callers are not given the chance.
func (k Keeper) ZoneOfAddress(ctx context.Context, address string) (uint32, error) {
	resolution, err := k.ZoneOfEntity(ctx, types.EntityKindAddress, address)
	if err != nil {
		return uint32(types.ZoneIDCore), err
	}
	return uint32(resolution.Zone), nil
}
