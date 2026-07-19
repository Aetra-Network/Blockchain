package types

import (
	"fmt"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/sovereign-l1/l1/app/addressing"
	"github.com/sovereign-l1/l1/x/internal/prototype"
)

// DefaultRoutingEpochLength is the routing-epoch length in blocks. The
// bucket->zone table may only change at a multiple of this height (I-8).
const DefaultRoutingEpochLength = uint64(10000)

// GovAuthority returns the governance module account address -- the authority
// that may execute MsgUpdateRoutingTable.
//
// This deliberately OVERRIDES prototype.DefaultAuthority
// ("ae1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqp8e93gq"), which is the all-zero, KEYLESS
// sentinel. Keyless is the correct default for a module that must be inert:
// nobody can sign for it, so no param update can ever execute. Phase 2's entire
// point is a Msg that governance CAN execute, and a keyless authority would make
// MsgUpdateRoutingTable a handler no transaction could ever reach.
//
// That is not a hypothetical. x/nominator-pool shipped exactly this bug: its
// authority defaulted to the keyless sentinel, MsgCreateNominatorPool could never
// be authorized, and liquid staking could not be exercised at all until
// cmd/l1d/cmd/testnet_genesis.go:40-43 patched the authority into genesis as a
// workaround. Its own doc note records the intended end state --
// testnet_genesis.go:146-151: "On a real network that authority is the gov module
// account." x/aez starts there instead of arriving there via a genesis patch.
//
// Governance is also the RIGHT class, not merely a working one: the routing table
// pins the chain's zone layout and is mutable only at epoch boundaries
// (docs/architecture/aez.md I-8). That is a consensus-layout change.
//
// This does NOT touch the frozen golden bucket vectors. The gov module account is
// already a pinned system entity via the module-account layer
// (app/accounts/module_accounts.go, SystemPinLayerModuleAccount), and
// prototype.DefaultAuthority remains pinned via SystemPinLayerAuthority. Changing
// a param DEFAULT is not a change to the pin set, and neither address is hashed.
func GovAuthority() string {
	return addressing.FormatAccAddress(authtypes.NewModuleAddress(govtypes.ModuleName))
}

// Params is the x/aez module's committed parameters.
//
// Prototype embeds the standard prototype gate, so Params.Prototype.Enabled is
// FALSE at genesis (prototype.DefaultParams()) and a disabled x/aez can never
// fail a block (I-23).
type Params struct {
	Prototype		prototype.Params
	RoutingEpochLength	uint64
	// GasQuota is the Phase 6 per-zone block-gas budget: a Core reservation
	// plus a per-elastic-zone cap, validated so the elastic caps can never eat
	// into the Core floor (I-18/I-19). It is committed, governable state. With
	// every bucket on zone 0 the Core cap is 0 (uncapped), so this table has no
	// behavioural effect on a single-zone chain.
	GasQuota	GasQuotaParams
	// MessageQuota is the Phase 6b per-zone cross-zone MESSAGE-BUS drain
	// budget (docs/architecture/aez-throughput-preservation-design.md) -- a
	// separate, independent counter from GasQuota protecting a different
	// resource (BeginBlock message-bus drain gas, not ante-time tx admission
	// gas). x/aez/keeper/drain.go's DrainWith falls back to the legacy
	// single-global-budget algorithm whenever this fails Validate() (e.g. an
	// old-shape, pre-this-field Params blob unmarshalled after a binary
	// upgrade), so it can never brick an upgrading chain.
	MessageQuota	MessageQuotaParams
}

// DefaultParams returns the genesis params: prototype-disabled, governed by the
// gov module account.
//
// Only Authority is overridden. Enabled stays FALSE (I-23) and every other
// prototype field keeps its standard default, so app_test.go's
// Params.Prototype.Enabled assertions and the aez-prototype operator profile
// stay green.
func DefaultParams() Params {
	prototypeParams := prototype.DefaultParams()
	prototypeParams.Authority = GovAuthority()
	return Params{
		Prototype:		prototypeParams,
		RoutingEpochLength:	DefaultRoutingEpochLength,
		GasQuota:		DefaultGasQuotaParams(),
		MessageQuota:		DefaultMessageQuotaParams(),
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
	if err := p.GasQuota.Validate(); err != nil {
		return err
	}
	if err := p.MessageQuota.Validate(); err != nil {
		return err
	}
	return nil
}

// Zone is the stored descriptor for one zone. It stays minimal: the Phase 6 gas
// quotas landed as PARAMS (Params.GasQuota), not as a per-descriptor field,
// because a quota is a governed, block-global budget validated as a set (the
// reserved-Core invariant couples the zones), not an independent attribute of
// one zone. Queue depths likewise live with the message bus, not here, so no
// field exists on Zone that nothing writes.
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
