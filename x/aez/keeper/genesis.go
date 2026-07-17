package keeper

import (
	"context"
	"fmt"

	"github.com/sovereign-l1/l1/x/aez/types"
)

// DefaultGenesis returns the module's default genesis state.
func DefaultGenesis() types.GenesisState {
	return types.DefaultGenesis()
}

// InitGenesisState writes genesis into the store as PER-ENTITY keys: params
// under its own key, one key per zone descriptor, and the routing table under
// its own version key plus a current pointer.
//
// Nothing is retained in RAM. There is no assignGenesis step, because there is
// no k.genesis field to assign to (I-20).
func (k Keeper) InitGenesisState(ctx context.Context, gs types.GenesisState) error {
	if err := gs.Validate(); err != nil {
		return err
	}
	if err := k.SetParams(ctx, gs.Params); err != nil {
		return err
	}
	for _, zone := range gs.Zones {
		if err := k.SetZone(ctx, zone); err != nil {
			return err
		}
	}
	return k.InitRoutingTable(ctx, gs.RoutingTable)
}

// ExportGenesisState reads genesis back OUT OF THE STORE.
//
// Every value is read from committed state, never from a cached struct and never
// gated on a reflect.DeepEqual comparison against DefaultGenesis(). An export
// that reads RAM exports whatever the process happened to be holding, which is
// how a restarted node and a running node produce different exports from the
// same committed state.
func (k Keeper) ExportGenesisState(ctx context.Context) (types.GenesisState, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return types.GenesisState{}, err
	}
	zones, err := k.GetAllZones(ctx)
	if err != nil {
		return types.GenesisState{}, err
	}
	table, err := k.GetRoutingTable(ctx)
	if err != nil {
		return types.GenesisState{}, fmt.Errorf("failed to export aez routing table: %w", err)
	}
	return types.GenesisState{
		Params:		params,
		RoutingTable:	table,
		Zones:		zones,
	}, nil
}
