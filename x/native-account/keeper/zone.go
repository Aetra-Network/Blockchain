package keeper

import (
	"context"
)

// CoreZone is the Core Zone id (x/aez ZoneIDCore). It is duplicated here as a
// plain constant rather than imported so that x/native-account keeps naming no
// x/aez symbol at all -- see ZoneResolver. Zone 0 is the one zone id that is
// frozen forever by the AEZ design (the Core Zone never migrates, I-9), so this
// constant cannot drift out of sync with x/aez the way a mutable value could.
const CoreZone = uint32(0)

// ZoneResolver resolves the home zone of an account address.
//
// It is declared HERE, on the CONSUMER side, and names only stdlib types.
// x/aez/keeper.Keeper satisfies it structurally via ZoneOfAddress, so
// x/native-account imports x/aez NOT AT ALL -- verified by
// TestNativeAccountDoesNotImportAEZ.
//
// This mirrors the pattern already used in this tree for exactly the same
// reason: app/txhandlers declares NativeAccountStatusReader on its own side
// rather than importing the keeper it consumes.
//
// The indirection buys a real property, not tidiness. x/aez/types/pins.go
// documents that the acyclicity of x/native-account -> x/aez is CONDITIONAL on
// app/accounts never gaining an x/native-account/types import. A structural
// interface makes the cycle unrepresentable instead of merely absent today.
type ZoneResolver interface {
	ZoneOfAddress(ctx context.Context, address string) (uint32, error)
}

// AccountZone is the resolved home zone of a native account.
//
// Resolved is the field that matters, and it exists for the same reason
// x/aez/keeper.ZoneResolution carries Hashed: with every bucket mapped to zone 0
// today, a consumer asserting only Zone == 0 would pass VACUOUSLY and keep
// passing even if resolution silently stopped working. Resolved distinguishes
// "x/aez says this account is in zone 0" from "nothing resolved a zone and 0 is
// the fallback". A later phase that acts on a zone MUST branch on Resolved.
//
// The zone is DERIVED, never persisted. It is not a field on Account and it is
// not in any store key. That is the whole design (see the package doc note on
// Phase 3): because zone = f(routing_table, bucket(entity)) is recomputable in
// O(1) at any time, storing it would duplicate a derived value into the one
// place that is expensive to change, and a routing-epoch rezone would then have
// to rewrite it for every account inside one block.
type AccountZone struct {
	Zone		uint32
	Resolved	bool
}

// ZoneOf resolves the home zone of a native account address.
//
// A NIL resolver yields (CoreZone, Resolved=false) and NO ERROR. This rule is
// the crux of the Phase 3 design and it is deliberate:
//
//   - It preserves I-23 ("a disabled x/aez never fails a block"). Nothing in
//     x/native-account's read path may acquire a hard dependency on x/aez being
//     initialized. AccountByUser, AccountStatus and the ante path
//     (app/txhandlers/storage_rent.go) never call this at all, so a broken or
//     absent x/aez cannot make an account unreadable or reject a transaction.
//   - It is only expressible because the zone is derived rather than keyed. A
//     KEY must always be some concrete byte string, so a zone-prefixed key
//     layout has no "unresolved" state to fall back from -- it would be forced to
//     guess, and a wrong guess orphans the account.
//
// A resolver that is present but FAILS returns the error rather than
// masquerading as zone 0. Callers on a non-consensus path (queries) may surface
// it; the event path deliberately degrades instead -- see zoneTag.
func (k Keeper) ZoneOf(ctx context.Context, userAddress string) (AccountZone, error) {
	if k.zoneResolver == nil {
		return AccountZone{Zone: CoreZone, Resolved: false}, nil
	}
	zone, err := k.zoneResolver.ZoneOfAddress(ctx, userAddress)
	if err != nil {
		return AccountZone{Zone: CoreZone, Resolved: false}, err
	}
	return AccountZone{Zone: zone, Resolved: true}, nil
}

// zoneTag is the NON-FAILING form of ZoneOf, used only where the caller must not
// be failed by zone resolution -- today, tagging the account-activated event.
//
// An event is observability. Failing an activation because an advisory tag could
// not be computed would trade a real property (accounts activate) for a cosmetic
// one (the tag is present), which is the trade I-23 forbids. On any resolution
// failure the tag degrades to Resolved=false, which is honest: it tells an
// indexer not to trust the accompanying zone rather than silently asserting 0.
//
// It is deterministic: every node reads the same committed routing table and
// takes the same branch.
func (k Keeper) zoneTag(ctx context.Context, userAddress string) AccountZone {
	zone, err := k.ZoneOf(ctx, userAddress)
	if err != nil {
		return AccountZone{Zone: CoreZone, Resolved: false}
	}
	return zone
}
