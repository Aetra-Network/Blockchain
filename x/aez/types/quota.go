package types

import "fmt"

// aez.md Phase 6: a global block gas budget split into per-zone quotas, with the
// Core Zone getting a guaranteed reserved slice that elastic zones can never
// consume. This file is the committed, governable, consensus-validated quota
// model; x/fees is a downstream CONSUMER of it through a narrow interface (it
// reads a zone's MaxGas at admission) and owns none of this shape.
//
// The whole model is static in v1: no borrowing, no dynamic rebalancing. Those
// are explicitly deferred by the spec.

// ZoneGasQuota is one zone's slice of the global block gas budget.
//
//   - Elastic zones (ZoneID 1..ZoneCount-1): MaxGas is a hard per-block CAP and
//     ReservedGas is 0.
//   - Core Zone (ZoneID 0): ReservedGas is a guaranteed FLOOR and MaxGas is 0
//     (uncapped).
//
// The Core Zone is NEVER capped. Capping it would drop the single-zone block
// budget from MaxBlockGas to CoreReserved and break inertness: with every bucket
// on zone 0 EVERY tx's home zone is Core, so a Core cap of 8,000,000 would reject
// the 9th max-gas tx of a block the pre-Phase-6 chain admitted up to 20,000,000.
// The reservation is enforced instead by capping the SUM OF ELASTIC quotas at
// MaxBlockGas - CoreReserved (see GasQuotaParams.Validate), which leaves at least
// CoreReserved always free for the Core Zone without ever gating a Core tx on
// anything but the untouched global budget.
type ZoneGasQuota struct {
	ZoneID		ZoneID
	MaxGas		uint64
	ReservedGas	uint64
}

// GasQuotaParams is the whole per-zone budget, carried inside x/aez Params.
//
// MaxBlockGas mirrors x/fees DefaultMaxBlockGas (20,000,000). It is carried here
// only so Validate is self-contained and needs no x/aez -> x/fees import; x/fees
// keeps its own MaxBlockGas as the authoritative global check at admission. The
// two are equal at genesis by construction (both default to 20,000,000).
type GasQuotaParams struct {
	MaxBlockGas	uint64
	// Quotas holds exactly ZoneCount entries in ascending, dense ZoneID order
	// (index i is zone i). A fixed, ordered slice keeps every traversal
	// byte-ordered and deterministic without a sort (I-22), and Validate
	// re-asserts the ordering so a malformed genesis cannot smuggle a
	// mis-indexed entry past it.
	Quotas	[]ZoneGasQuota
}

// DefaultGasQuotaParams returns the spec split of MaxBlockGas = 20,000,000: the
// Core Zone reserves 8,000,000 (a floor, uncapped) and each of the four elastic
// zones is capped at 3,000,000. Sum of elastic (12,000,000) + Core reserve
// (8,000,000) == 20,000,000, so the reserved-Core invariant holds with equality.
func DefaultGasQuotaParams() GasQuotaParams {
	const (
		defaultMaxBlockGas	= uint64(20_000_000)
		defaultCoreReserved	= uint64(8_000_000)
		defaultElasticMaxGas	= uint64(3_000_000)
	)
	quotas := make([]ZoneGasQuota, 0, ZoneCount)
	for _, id := range AllZoneIDs() {
		if id.IsCore() {
			quotas = append(quotas, ZoneGasQuota{ZoneID: id, MaxGas: 0, ReservedGas: defaultCoreReserved})
			continue
		}
		quotas = append(quotas, ZoneGasQuota{ZoneID: id, MaxGas: defaultElasticMaxGas, ReservedGas: 0})
	}
	return GasQuotaParams{
		MaxBlockGas:	defaultMaxBlockGas,
		Quotas:		quotas,
	}
}

// Validate enforces the deterministic reserved-Core invariant (I-18/I-19).
//
// All arithmetic is integer and overflow-checked; there is no map iteration and
// no float. The Quotas slice is required to be dense and ascending (zone i at
// index i), so the whole check is a single ordered pass.
func (q GasQuotaParams) Validate() error {
	if q.MaxBlockGas == 0 {
		return fmt.Errorf("%w: MaxBlockGas must be positive", ErrInvalidGasQuota)
	}
	if uint32(len(q.Quotas)) != ZoneCount {
		return fmt.Errorf("%w: need exactly %d zone quotas, got %d", ErrInvalidGasQuota, ZoneCount, len(q.Quotas))
	}
	var elasticSum, coreReserved uint64
	for i, zq := range q.Quotas {
		if uint32(zq.ZoneID) != uint32(i) {
			return fmt.Errorf("%w: quota %d out of order (zone %d)", ErrInvalidGasQuota, i, uint32(zq.ZoneID))
		}
		if err := zq.ZoneID.Validate(); err != nil {
			return fmt.Errorf("%w: %s", ErrInvalidGasQuota, err)
		}
		if zq.ZoneID.IsCore() {
			// Core is a floor, never a cap. A capped Core would break
			// inertness (see ZoneGasQuota).
			if zq.MaxGas != 0 {
				return fmt.Errorf("%w: Core Zone must not be capped (MaxGas %d)", ErrInvalidGasQuota, zq.MaxGas)
			}
			coreReserved = zq.ReservedGas
			continue
		}
		if zq.ReservedGas != 0 {
			return fmt.Errorf("%w: elastic zone %d must not reserve gas (ReservedGas %d)", ErrInvalidGasQuota, uint32(zq.ZoneID), zq.ReservedGas)
		}
		if zq.MaxGas == 0 {
			return fmt.Errorf("%w: elastic zone %d needs a positive cap", ErrInvalidGasQuota, uint32(zq.ZoneID))
		}
		if elasticSum+zq.MaxGas < elasticSum {
			return fmt.Errorf("%w: elastic quota sum overflows uint64", ErrInvalidGasQuota)
		}
		elasticSum += zq.MaxGas
	}
	// The reserved-Core invariant: elastic zones collectively can never eat
	// into the Core floor. Overflow-checked before the comparison.
	if elasticSum+coreReserved < elasticSum {
		return fmt.Errorf("%w: elastic sum + core reserve overflows uint64", ErrInvalidGasQuota)
	}
	if elasticSum+coreReserved > q.MaxBlockGas {
		return fmt.Errorf("%w: elastic sum %d + core reserve %d exceeds MaxBlockGas %d",
			ErrInvalidGasQuota, elasticSum, coreReserved, q.MaxBlockGas)
	}
	return nil
}

// MaxGasForZone returns the per-block gas cap for a zone. The Core Zone (and any
// zone whose cap is 0) is UNCAPPED: 0 is the sentinel the admission gate reads as
// "no per-zone limit", which is exactly what keeps a single-zone chain inert.
//
// It reindexes by ZoneID directly (Quotas is dense and ascending after Validate),
// so it is O(1) and needs no search.
func (q GasQuotaParams) MaxGasForZone(zoneID uint32) (uint64, error) {
	if zoneID >= uint32(len(q.Quotas)) {
		return 0, fmt.Errorf("%w: no quota for zone %d", ErrInvalidGasQuota, zoneID)
	}
	return q.Quotas[zoneID].MaxGas, nil
}

// CoreReservedGas returns the Core Zone floor. It is the amount of the global
// budget that the elastic caps are validated never to be able to consume.
func (q GasQuotaParams) CoreReservedGas() uint64 {
	for _, zq := range q.Quotas {
		if zq.ZoneID.IsCore() {
			return zq.ReservedGas
		}
	}
	return 0
}
