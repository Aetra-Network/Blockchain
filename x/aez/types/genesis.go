package types

import "fmt"

// GenesisState is the x/aez genesis.
//
// It carries the FULL routing table explicitly rather than a "core-only" flag.
// Genesis is the one place the whole table is legitimately one value (there is
// exactly one table per version, and versions are per-entity keys in the store),
// and shipping all 256 assignments makes the Phase 1 promise -- every bucket on
// zone 0 -- auditable by reading the exported genesis rather than by trusting a
// constructor.
type GenesisState struct {
	Params		Params
	RoutingTable	RoutingTable
	Zones		[]Zone
}

// DefaultGenesis returns the Phase 1 genesis: prototype-disabled params, all 256
// buckets on the Core Zone, and one descriptor per zone.
func DefaultGenesis() GenesisState {
	zones := make([]Zone, 0, ZoneCount)
	for _, id := range AllZoneIDs() {
		zones = append(zones, NewZone(id))
	}
	return GenesisState{
		Params:		DefaultParams(),
		RoutingTable:	GenesisRoutingTable(),
		Zones:		zones,
	}
}

// Validate checks params, the routing table, and the zone descriptor set.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidGenesis, err)
	}
	if err := gs.RoutingTable.Validate(); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidGenesis, err)
	}
	if uint32(len(gs.Zones)) != ZoneCount {
		return fmt.Errorf("%w: genesis must declare exactly %d zones, got %d", ErrInvalidGenesis, ZoneCount, len(gs.Zones))
	}
	seen := make(map[ZoneID]bool, len(gs.Zones))
	for _, zone := range gs.Zones {
		if err := zone.Validate(); err != nil {
			return fmt.Errorf("%w: %s", ErrInvalidGenesis, err)
		}
		if seen[zone.ID] {
			return fmt.Errorf("%w: duplicate zone %d", ErrInvalidGenesis, uint32(zone.ID))
		}
		seen[zone.ID] = true
	}
	for _, id := range AllZoneIDs() {
		if !seen[id] {
			return fmt.Errorf("%w: genesis is missing zone %d", ErrInvalidGenesis, uint32(id))
		}
	}
	return nil
}

// IsCoreOnly reports whether every one of the BucketCount buckets maps to the
// Core Zone -- i.e. whether this genesis is the purely-additive Phase 1 shape.
//
// This is deliberately NOT enforced by Validate. "All buckets on zone 0" is a
// property of the genesis x/aez SHIPS (DefaultGenesis, asserted by
// genesis_test.go), not a structural requirement of a well-formed genesis: a
// table with elastic assignments is perfectly valid and is exactly what later
// phases will ship. Conflating the two would make Validate reject a legitimate
// future genesis. What protects the Core Zone is the CorePinned short-circuit,
// which no table version can express its way around (I-9), not a genesis check.
func (gs GenesisState) IsCoreOnly() bool {
	for i := 0; i < BucketCount; i++ {
		if !gs.RoutingTable.Buckets[i].IsCore() {
			return false
		}
	}
	return true
}
