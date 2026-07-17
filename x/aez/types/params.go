package types

import (
	"fmt"

	"github.com/sovereign-l1/l1/x/internal/prototype"
)

// DefaultRoutingEpochLength is the routing-epoch length in blocks. The
// bucket->zone table may only change at a multiple of this height (I-8).
const DefaultRoutingEpochLength = uint64(10000)

// Params is the x/aez module's committed parameters.
//
// Prototype embeds the standard prototype gate, so Params.Prototype.Enabled is
// FALSE at genesis (prototype.DefaultParams()) and a disabled x/aez can never
// fail a block (I-23).
type Params struct {
	Prototype		prototype.Params
	RoutingEpochLength	uint64
}

// DefaultParams returns the genesis params: prototype-disabled.
func DefaultParams() Params {
	return Params{
		Prototype:		prototype.DefaultParams(),
		RoutingEpochLength:	DefaultRoutingEpochLength,
	}
}

// Validate checks the embedded prototype params and the epoch length.
func (p Params) Validate() error {
	if err := p.Prototype.Validate(); err != nil {
		return err
	}
	if p.RoutingEpochLength == 0 {
		return fmt.Errorf("aez routing epoch length must be positive")
	}
	return nil
}

// Zone is the stored descriptor for one zone. Phase 1 keeps it minimal: the
// gas quotas and queue depths of aez.md §6 Phase 6 are deliberately absent
// rather than stubbed, so no field exists that nothing writes.
type Zone struct {
	ID	ZoneID
	Kind	ZoneKind
}

// NewZone returns the descriptor for a zone id.
func NewZone(id ZoneID) Zone {
	return Zone{ID: id, Kind: id.Kind()}
}

// Validate checks the zone id and that Kind agrees with it.
func (z Zone) Validate() error {
	if err := z.ID.Validate(); err != nil {
		return err
	}
	if z.Kind != z.ID.Kind() {
		return fmt.Errorf("%w: zone %d kind %q does not match id", ErrInvalidZone, uint32(z.ID), string(z.Kind))
	}
	return nil
}
