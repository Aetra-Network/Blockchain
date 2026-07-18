package keeper

import (
	"context"
)

// zoneCore is the AEZ Core Zone id (x/aez ZoneIDCore). It is duplicated here as
// a plain constant rather than imported so that x/fees keeps naming no x/aez
// symbol at all -- see ZoneResolver. Zone 0 is the one zone id frozen forever by
// the AEZ design (the Core Zone never migrates, I-9), so this constant cannot
// drift out of sync with x/aez the way a mutable value could.
const zoneCore = uint32(0)

// ZoneResolver resolves a tx's home zone and that zone's per-block gas cap.
//
// It is declared HERE, on the CONSUMER side, and names only stdlib types.
// x/aez/keeper.Keeper satisfies it structurally (ZoneOfAddress + GasQuotaForZone),
// so x/fees imports x/aez NOT AT ALL. This mirrors LoadSink (congestion.go) and
// x/native-account's own ZoneResolver, and it is the pattern aez.md's Phase 6
// integration note requires: x/fees is a consumer of the quota table, never its
// owner, and must not acquire a concrete dependency on x/aez.
//
// Both methods report the Core Zone / an uncapped budget on the inert path:
//   - ZoneOfAddress returns 0 for a Core-pinned or single-zone address.
//   - GasQuotaForZone returns 0 (the uncapped sentinel) for the Core Zone.
// So a nil resolver and an all-buckets-zone-0 resolver are behaviourally
// identical, which is what the inertness proof asserts.
type ZoneResolver interface {
	ZoneOfAddress(ctx context.Context, address string) (uint32, error)
	GasQuotaForZone(ctx context.Context, zoneID uint32) (uint64, error)
}

// WithZoneResolver returns a Keeper whose AdmitTx enforces AEZ Phase 6 per-zone
// gas quotas alongside the global budget. Wired in app/keepers.go. A nil
// resolver (the default) leaves the single global-budget behaviour untouched.
func (k Keeper) WithZoneResolver(r ZoneResolver) Keeper {
	k.zoneResolver = r
	return k
}
