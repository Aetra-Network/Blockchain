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

// --- Phase 6b: per-zone cross-zone message-bus drain budget -----------------
//
// docs/architecture/aez-throughput-preservation-design.md is the full design
// (read it for the adversarial-review history behind the shape below, in
// particular why Core is folded symmetrically into the same own-allotment
// mechanism as the elastic zones rather than special-cased as unconditional
// past its floor). Summary: x/aez/keeper/drain.go's DrainWith drains the
// cross-zone message bus against a single global budget today
// (types.LegacyGlobalMessageGasPerBlock). This type carries a per-zone split
// of that budget, mirroring GasQuotaParams' shape field-for-field, but for a
// DIFFERENT resource (BeginBlock message-bus drain gas, not ante-time tx
// admission gas) -- the two totals are independent and nothing requires or
// checks any numeric relationship between them.

// ZoneMessageQuota is one zone's slice of the cross-zone message-bus's
// per-block drain budget. Same shape as ZoneGasQuota: Core Zone (ZoneID 0)
// gets ReservedGas (a floor) and MaxGas == 0 (never validated as a cap);
// elastic zones get MaxGas (a cap) and ReservedGas == 0.
//
// Unlike GasQuotaParams' use at the tx-admission gate, "Core is never capped"
// here does NOT mean "Core is admitted subject only to the global backstop."
// The drain algorithm (x/aez/keeper/drain.go, drainWeighted) uses
// OwnAllotmentForZone to treat Core's ReservedGas as its own allotment in the
// SAME own-allotment-then-rollover mechanism every elastic zone uses -- this
// is the fix for the adversarial-review Finding 1 blocker (see the design
// doc's revision note): an uncapped-past-its-floor Core could otherwise
// exhaust the global counter before a victim elastic zone's own-cap message
// was ever reached.
type ZoneMessageQuota struct {
	ZoneID      ZoneID
	MaxGas      uint64
	ReservedGas uint64
}

// MessageQuotaParams is the whole per-zone message-bus drain budget, carried
// inside x/aez Params (Params.MessageQuota) -- committed, governable state,
// exactly like Params.GasQuota.
type MessageQuotaParams struct {
	TotalMessageGasPerBlock uint64
	// Quotas holds exactly ZoneCount entries in ascending, dense ZoneID
	// order (index i is zone i), identical discipline to
	// GasQuotaParams.Quotas.
	Quotas []ZoneMessageQuota
}

// DefaultMessageQuotaParams returns the spec split of
// TotalMessageGasPerBlock = 8,000,000 (unchanged from today's
// LegacyGlobalMessageGasPerBlock constant -- this is a redistribution, not a
// capacity increase): the Core Zone reserves 4,000,000 (a floor) and each of
// the four elastic zones is capped at 1,000,000 (== MaxGasPerDelivery, so
// every elastic zone is guaranteed at least one maximum-size delivery per
// block no matter what). Sum of elastic (4,000,000) + Core reserve
// (4,000,000) == 8,000,000, so the reserved-Core invariant holds with
// equality, mirroring DefaultGasQuotaParams' discipline.
func DefaultMessageQuotaParams() MessageQuotaParams {
	const (
		defaultTotalMessageGasPerBlock = uint64(8_000_000)
		defaultCoreMessageReserved     = uint64(4_000_000)
		defaultElasticMessageMaxGas    = uint64(1_000_000)
	)
	quotas := make([]ZoneMessageQuota, 0, ZoneCount)
	for _, id := range AllZoneIDs() {
		if id.IsCore() {
			quotas = append(quotas, ZoneMessageQuota{ZoneID: id, MaxGas: 0, ReservedGas: defaultCoreMessageReserved})
			continue
		}
		quotas = append(quotas, ZoneMessageQuota{ZoneID: id, MaxGas: defaultElasticMessageMaxGas, ReservedGas: 0})
	}
	return MessageQuotaParams{
		TotalMessageGasPerBlock: defaultTotalMessageGasPerBlock,
		Quotas:                  quotas,
	}
}

// Validate mirrors GasQuotaParams.Validate field-for-field, against
// TotalMessageGasPerBlock instead of MaxBlockGas and ErrInvalidMessageQuota
// instead of ErrInvalidGasQuota.
func (q MessageQuotaParams) Validate() error {
	if q.TotalMessageGasPerBlock == 0 {
		return fmt.Errorf("%w: TotalMessageGasPerBlock must be positive", ErrInvalidMessageQuota)
	}
	if uint32(len(q.Quotas)) != ZoneCount {
		return fmt.Errorf("%w: need exactly %d zone quotas, got %d", ErrInvalidMessageQuota, ZoneCount, len(q.Quotas))
	}
	var elasticSum, coreReserved uint64
	for i, zq := range q.Quotas {
		if uint32(zq.ZoneID) != uint32(i) {
			return fmt.Errorf("%w: quota %d out of order (zone %d)", ErrInvalidMessageQuota, i, uint32(zq.ZoneID))
		}
		if err := zq.ZoneID.Validate(); err != nil {
			return fmt.Errorf("%w: %s", ErrInvalidMessageQuota, err)
		}
		if zq.ZoneID.IsCore() {
			if zq.MaxGas != 0 {
				return fmt.Errorf("%w: Core Zone must not be capped (MaxGas %d)", ErrInvalidMessageQuota, zq.MaxGas)
			}
			coreReserved = zq.ReservedGas
			continue
		}
		if zq.ReservedGas != 0 {
			return fmt.Errorf("%w: elastic zone %d must not reserve gas (ReservedGas %d)", ErrInvalidMessageQuota, uint32(zq.ZoneID), zq.ReservedGas)
		}
		if zq.MaxGas == 0 {
			return fmt.Errorf("%w: elastic zone %d needs a positive cap", ErrInvalidMessageQuota, uint32(zq.ZoneID))
		}
		if elasticSum+zq.MaxGas < elasticSum {
			return fmt.Errorf("%w: elastic quota sum overflows uint64", ErrInvalidMessageQuota)
		}
		elasticSum += zq.MaxGas
	}
	if elasticSum+coreReserved < elasticSum {
		return fmt.Errorf("%w: elastic sum + core reserve overflows uint64", ErrInvalidMessageQuota)
	}
	if elasticSum+coreReserved > q.TotalMessageGasPerBlock {
		return fmt.Errorf("%w: elastic sum %d + core reserve %d exceeds TotalMessageGasPerBlock %d",
			ErrInvalidMessageQuota, elasticSum, coreReserved, q.TotalMessageGasPerBlock)
	}
	return nil
}

// MaxMessageGasForZone returns the per-block message-bus drain cap for a
// zone. Mirrors GasQuotaParams.MaxGasForZone: the Core Zone reads as the
// UNCAPPED-BY-VALIDATION sentinel 0 (its real budget is ReservedGas, read
// via CoreReservedMessageGas or OwnAllotmentForZone).
func (q MessageQuotaParams) MaxMessageGasForZone(zoneID uint32) (uint64, error) {
	if zoneID >= uint32(len(q.Quotas)) {
		return 0, fmt.Errorf("%w: no quota for zone %d", ErrInvalidMessageQuota, zoneID)
	}
	return q.Quotas[zoneID].MaxGas, nil
}

// CoreReservedMessageGas returns the Core Zone's message-bus floor.
func (q MessageQuotaParams) CoreReservedMessageGas() uint64 {
	for _, zq := range q.Quotas {
		if zq.ZoneID.IsCore() {
			return zq.ReservedGas
		}
	}
	return 0
}

// OwnAllotmentForZone returns the zone's own slice of the per-block
// message-bus drain budget for the two-pass drain algorithm
// (x/aez/keeper/drain.go, drainWeighted): ReservedGas for the Core Zone,
// MaxGas for an elastic zone.
//
// This is the accessor that makes Core a symmetric participant in the
// own-allotment-then-rollover mechanism rather than a special-cased
// unconditional-past-its-floor path -- the fix for the adversarial-review
// Finding 1 blocker (see the design doc's revision note). Both passes of
// drainWeighted index by this, uniformly, with no Core-shaped branch.
func (q MessageQuotaParams) OwnAllotmentForZone(zoneID uint32) (uint64, error) {
	if zoneID >= uint32(len(q.Quotas)) {
		return 0, fmt.Errorf("%w: no quota for zone %d", ErrInvalidMessageQuota, zoneID)
	}
	zq := q.Quotas[zoneID]
	if zq.ZoneID.IsCore() {
		return zq.ReservedGas, nil
	}
	return zq.MaxGas, nil
}
