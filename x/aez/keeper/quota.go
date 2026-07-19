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

// MessageGasQuotaForZone returns the per-block cross-zone MESSAGE-BUS drain
// cap for a zone, read from committed params on every call (never cached --
// the same F-17/I-20 discipline as GasQuotaForZone). It is a DIFFERENT
// counter from GasQuotaForZone: message-bus drain gas and tx-execution-
// admission gas protect two independent resources.
//
// The Core Zone returns 0 (MaxMessageGasForZone's uncapped-by-validation
// sentinel), matching GasQuotaForZone's shape for API consistency -- but
// unlike the tx-gate, this is NOT what the drain algorithm itself uses to
// decide Core's admission; that reads OwnAllotmentForZone (ReservedGas for
// Core), which this accessor does not return. This exists for
// queries/tests/observability; the drain hot loop in x/aez/keeper/drain.go
// reads params.MessageQuota once per DrainWith call, not once per candidate
// message via this accessor.
func (k Keeper) MessageGasQuotaForZone(ctx context.Context, zoneID uint32) (uint64, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return 0, err
	}
	return params.MessageQuota.MaxMessageGasForZone(zoneID)
}
