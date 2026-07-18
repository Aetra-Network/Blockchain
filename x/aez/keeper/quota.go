package keeper

import (
	"context"
)

// GasQuotaForZone returns the per-block gas cap for a zone, read from committed
// params on every call (never cached -- the F-17 regression guard, I-20).
//
// The Core Zone returns 0, which is the UNCAPPED sentinel the x/fees admission
// gate reads as "no per-zone limit". That is the mechanism that keeps a
// single-zone chain inert: with every bucket on zone 0 every tx resolves to
// Core, this returns 0, and the per-zone check is skipped entirely.
//
// The signature names no x/aez type, so x/fees satisfies its own ZoneResolver
// interface with this method STRUCTURALLY and imports x/aez not at all -- the
// same discipline ZoneOfAddress already follows for x/native-account.
func (k Keeper) GasQuotaForZone(ctx context.Context, zoneID uint32) (uint64, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return 0, err
	}
	return params.GasQuota.MaxGasForZone(zoneID)
}
