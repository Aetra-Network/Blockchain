package types_test

import (
	"errors"
	"testing"

	aeztypes "github.com/sovereign-l1/l1/x/aez/types"
)

// coreQuota / elasticQuota build one entry of a quota table.
func coreQuota(reserved uint64) aeztypes.ZoneGasQuota {
	return aeztypes.ZoneGasQuota{ZoneID: aeztypes.ZoneIDCore, MaxGas: 0, ReservedGas: reserved}
}

func elasticQuota(id aeztypes.ZoneID, cap_ uint64) aeztypes.ZoneGasQuota {
	return aeztypes.ZoneGasQuota{ZoneID: id, MaxGas: cap_, ReservedGas: 0}
}

// fullTable returns a well-formed table: Core reserve + one cap per elastic zone.
func fullTable(maxBlock, coreReserved uint64, elasticCaps ...uint64) aeztypes.GasQuotaParams {
	quotas := []aeztypes.ZoneGasQuota{coreQuota(coreReserved)}
	for i, cap_ := range elasticCaps {
		quotas = append(quotas, elasticQuota(aeztypes.ZoneID(uint32(i+1)), cap_))
	}
	return aeztypes.GasQuotaParams{MaxBlockGas: maxBlock, Quotas: quotas}
}

func TestDefaultGasQuotaParamsIsTheSpecSplit(t *testing.T) {
	q := aeztypes.DefaultGasQuotaParams()
	if err := q.Validate(); err != nil {
		t.Fatalf("default quota params must validate: %v", err)
	}
	if q.MaxBlockGas != 20_000_000 {
		t.Fatalf("default MaxBlockGas = %d, want 20000000", q.MaxBlockGas)
	}
	if uint32(len(q.Quotas)) != aeztypes.ZoneCount {
		t.Fatalf("default must carry %d quotas, got %d", aeztypes.ZoneCount, len(q.Quotas))
	}
	if got := q.CoreReservedGas(); got != 8_000_000 {
		t.Fatalf("core reserve = %d, want 8000000", got)
	}
	// Core is uncapped; each elastic zone caps at 3,000,000.
	if maxGas, err := q.MaxGasForZone(0); err != nil || maxGas != 0 {
		t.Fatalf("core MaxGasForZone = (%d,%v), want (0,nil)", maxGas, err)
	}
	for z := uint32(1); z < aeztypes.ZoneCount; z++ {
		maxGas, err := q.MaxGasForZone(z)
		if err != nil || maxGas != 3_000_000 {
			t.Fatalf("elastic zone %d MaxGasForZone = (%d,%v), want (3000000,nil)", z, maxGas, err)
		}
	}
}

// TestReservedCoreInvariantHoldsWithEquality is the I-18/I-19 guard: the sum of
// elastic caps plus the Core reserve must never exceed MaxBlockGas. The default
// split hits it with exact equality (12M + 8M == 20M).
func TestReservedCoreInvariantHoldsWithEquality(t *testing.T) {
	// Exactly at the budget: accept.
	ok := fullTable(20_000_000, 8_000_000, 3_000_000, 3_000_000, 3_000_000, 3_000_000)
	if err := ok.Validate(); err != nil {
		t.Fatalf("sum-at-budget table must validate: %v", err)
	}
	// One naet over the budget (elastic sum 12,000,001 + 8,000,000): reject.
	over := fullTable(20_000_000, 8_000_000, 3_000_001, 3_000_000, 3_000_000, 3_000_000)
	if err := over.Validate(); !errors.Is(err, aeztypes.ErrInvalidGasQuota) {
		t.Fatalf("over-budget table must be rejected with ErrInvalidGasQuota, got %v", err)
	}
}

// TestElasticCannotConsumeCoreFloor proves the reservation is real: even if
// every elastic zone is saturated to its validated cap, at least CoreReserved
// gas of the global budget remains unclaimable by elastic zones.
func TestElasticCannotConsumeCoreFloor(t *testing.T) {
	q := aeztypes.DefaultGasQuotaParams()
	if err := q.Validate(); err != nil {
		t.Fatalf("default must validate: %v", err)
	}
	var elasticSum uint64
	for z := uint32(1); z < aeztypes.ZoneCount; z++ {
		maxGas, err := q.MaxGasForZone(z)
		if err != nil {
			t.Fatalf("zone %d: %v", z, err)
		}
		elasticSum += maxGas
	}
	// The most elastic zones can collectively reserve is elasticSum; the global
	// budget minus that is what always remains for the Core Zone.
	freeForCore := q.MaxBlockGas - elasticSum
	if freeForCore < q.CoreReservedGas() {
		t.Fatalf("elastic saturation leaves %d for core, below the %d reserve", freeForCore, q.CoreReservedGas())
	}
}

func TestGasQuotaValidateRejectsCappedCore(t *testing.T) {
	q := aeztypes.DefaultGasQuotaParams()
	q.Quotas[0].MaxGas = 8_000_000 // cap the Core Zone -- forbidden
	if err := q.Validate(); !errors.Is(err, aeztypes.ErrInvalidGasQuota) {
		t.Fatalf("capped Core must be rejected, got %v", err)
	}
}

func TestGasQuotaValidateRejectsElasticReservation(t *testing.T) {
	q := aeztypes.DefaultGasQuotaParams()
	q.Quotas[1].ReservedGas = 1 // an elastic zone may not reserve
	if err := q.Validate(); !errors.Is(err, aeztypes.ErrInvalidGasQuota) {
		t.Fatalf("elastic reservation must be rejected, got %v", err)
	}
}

func TestGasQuotaValidateRejectsZeroElasticCap(t *testing.T) {
	q := aeztypes.DefaultGasQuotaParams()
	q.Quotas[2].MaxGas = 0 // an elastic zone must carry a positive cap
	if err := q.Validate(); !errors.Is(err, aeztypes.ErrInvalidGasQuota) {
		t.Fatalf("zero elastic cap must be rejected, got %v", err)
	}
}

func TestGasQuotaValidateRejectsWrongLength(t *testing.T) {
	q := aeztypes.DefaultGasQuotaParams()
	q.Quotas = q.Quotas[:aeztypes.ZoneCount-1] // drop one zone
	if err := q.Validate(); !errors.Is(err, aeztypes.ErrInvalidGasQuota) {
		t.Fatalf("short quota table must be rejected, got %v", err)
	}
}

func TestGasQuotaValidateRejectsOutOfOrder(t *testing.T) {
	q := aeztypes.DefaultGasQuotaParams()
	// Swap zones 1 and 2 so index != ZoneID.
	q.Quotas[1], q.Quotas[2] = q.Quotas[2], q.Quotas[1]
	if err := q.Validate(); !errors.Is(err, aeztypes.ErrInvalidGasQuota) {
		t.Fatalf("out-of-order quota table must be rejected, got %v", err)
	}
}

func TestGasQuotaValidateRejectsDuplicateZone(t *testing.T) {
	q := aeztypes.DefaultGasQuotaParams()
	q.Quotas[2].ZoneID = aeztypes.ZoneID(1) // duplicate zone 1, so index 2 mismatches
	if err := q.Validate(); !errors.Is(err, aeztypes.ErrInvalidGasQuota) {
		t.Fatalf("duplicate zone must be rejected, got %v", err)
	}
}

func TestGasQuotaValidateRejectsZeroMaxBlockGas(t *testing.T) {
	q := aeztypes.DefaultGasQuotaParams()
	q.MaxBlockGas = 0
	if err := q.Validate(); !errors.Is(err, aeztypes.ErrInvalidGasQuota) {
		t.Fatalf("zero MaxBlockGas must be rejected, got %v", err)
	}
}

func TestGasQuotaValidateRejectsElasticOverflow(t *testing.T) {
	// Two elastic caps that individually fit but overflow uint64 when summed.
	q := fullTable(20_000_000, 0,
		^uint64(0)-10, 20, 1, 1)
	if err := q.Validate(); !errors.Is(err, aeztypes.ErrInvalidGasQuota) {
		t.Fatalf("elastic sum overflow must be rejected, got %v", err)
	}
}

func TestMaxGasForZoneRejectsUnknownZone(t *testing.T) {
	q := aeztypes.DefaultGasQuotaParams()
	if _, err := q.MaxGasForZone(aeztypes.ZoneCount); !errors.Is(err, aeztypes.ErrInvalidGasQuota) {
		t.Fatalf("out-of-range zone must error, got %v", err)
	}
}

// TestParamsValidateEnforcesGasQuota proves the invariant is reached through the
// module-level Params.Validate, i.e. governance cannot commit an over-budget
// table through SetParams.
func TestParamsValidateEnforcesGasQuota(t *testing.T) {
	p := aeztypes.DefaultParams()
	if err := p.Validate(); err != nil {
		t.Fatalf("default params must validate: %v", err)
	}
	p.GasQuota = fullTable(20_000_000, 8_000_000, 5_000_000, 5_000_000, 5_000_000, 5_000_000)
	if err := p.Validate(); !errors.Is(err, aeztypes.ErrInvalidGasQuota) {
		t.Fatalf("params carrying an over-budget quota table must be rejected, got %v", err)
	}
}
