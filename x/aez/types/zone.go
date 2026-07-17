package types

import "fmt"

// ZoneID identifies one deterministic execution container inside the single
// AEZ chain. Zones are NOT shards: every validator executes every zone, under
// one CometBFT consensus, one validator set, one height, and one global state
// root (see docs/architecture/aez.md, I-1).
type ZoneID uint32

const (
	// ZoneIDCore is the Core Zone. It owns validators, elections, staking,
	// slashing, governance, the nominator pool, protocol params, upgrades,
	// the routing table itself, and the native DNS registry. It never
	// migrates (I-9) and no routing table version can express a Core move.
	ZoneIDCore	ZoneID	= 0

	// ZoneCount is the total number of zones: the Core Zone plus four
	// elastic zones (ZoneID 1..4). Each elastic zone hosts BOTH native
	// accounts and Aetralis contracts -- a zone is a state+execution
	// container, not an application type.
	ZoneCount	uint32	= 5
)

// ZoneKind distinguishes the Core Zone from the elastic zones.
type ZoneKind string

const (
	ZoneKindCore	ZoneKind	= "CORE"
	ZoneKindElastic	ZoneKind	= "ELASTIC"
)

// IsValid reports whether the zone id is one of the ZoneCount known zones.
func (z ZoneID) IsValid() bool {
	return uint32(z) < ZoneCount
}

// IsCore reports whether the zone is the Core Zone.
func (z ZoneID) IsCore() bool {
	return z == ZoneIDCore
}

// Kind returns the ZoneKind for the zone.
func (z ZoneID) Kind() ZoneKind {
	if z.IsCore() {
		return ZoneKindCore
	}
	return ZoneKindElastic
}

// Validate rejects any zone id outside 0..ZoneCount-1.
func (z ZoneID) Validate() error {
	if !z.IsValid() {
		return fmt.Errorf("%w: zone %d is not in 0..%d", ErrInvalidZone, uint32(z), ZoneCount-1)
	}
	return nil
}

func (z ZoneID) String() string {
	return fmt.Sprintf("zone-%d", uint32(z))
}

// AllZoneIDs returns every valid zone id in ascending, deterministic order.
func AllZoneIDs() []ZoneID {
	out := make([]ZoneID, 0, ZoneCount)
	for i := uint32(0); i < ZoneCount; i++ {
		out = append(out, ZoneID(i))
	}
	return out
}
