package keeper

import (
	"context"
	"encoding/json"
	"fmt"

	corestore "cosmossdk.io/core/store"

	"github.com/sovereign-l1/l1/x/aez/types"
)

// Keeper is the x/aez keeper.
//
// It holds a storeService AND NOTHING ELSE. There is no `genesis` field, no
// `loadForBlock`, no `writeGenesis`, and no cached state of any kind that
// survives a call.
//
// This is not a style preference. x/contracts/keeper/keeper.go:2184-2196
// documents, in this tree, why a keeper with lazily-loaded in-memory state is
// consensus-unsafe: a restarted or state-synced node holds DefaultGenesis()
// while a continuously-running node holds the committed value, the two disagree,
// and the chain forks (the F-17 class). A keeper with no in-memory state cannot
// have that bug -- it is structurally immune rather than carefully coded
// (I-20). Every read below goes to the store, every time.
//
// Adding a field that caches state here re-opens F-17. Do not.
type Keeper struct {
	storeService corestore.KVStoreService
}

// NewPersistentKeeper builds the keeper. It takes no bank, staking, or other
// module handle: x/aez moves messages, never money (I-10), and holds no other
// module's store handle (I-11). It is registered as a PROTOTYPE module and must
// therefore never hold module-account permissions
// (app/aetra_core_wiring.go:26-28).
func NewPersistentKeeper(storeService corestore.KVStoreService) Keeper {
	return Keeper{storeService: storeService}
}

// SetParams writes the module params.
func (k Keeper) SetParams(ctx context.Context, params types.Params) error {
	if err := params.Validate(); err != nil {
		return err
	}
	return k.setJSON(ctx, types.ParamsKey, params)
}

// GetParams reads the module params from the store. It returns an error rather
// than silently falling back to DefaultParams(): a silent default is precisely
// how a restarted node and a running node come to disagree (F-17).
func (k Keeper) GetParams(ctx context.Context) (types.Params, error) {
	var params types.Params
	found, err := k.getJSON(ctx, types.ParamsKey, &params)
	if err != nil {
		return types.Params{}, err
	}
	if !found {
		return types.Params{}, fmt.Errorf("aez params are not initialized")
	}
	return params, nil
}

// SetZone writes one zone descriptor under its own key.
func (k Keeper) SetZone(ctx context.Context, zone types.Zone) error {
	if err := zone.Validate(); err != nil {
		return err
	}
	return k.setJSON(ctx, types.ZoneKey(zone.ID), zone)
}

// GetZone reads one zone descriptor.
func (k Keeper) GetZone(ctx context.Context, id types.ZoneID) (types.Zone, bool, error) {
	var zone types.Zone
	found, err := k.getJSON(ctx, types.ZoneKey(id), &zone)
	if err != nil {
		return types.Zone{}, false, err
	}
	return zone, found, nil
}

// GetAllZones reads every zone descriptor in ascending zone order. It iterates
// AllZoneIDs rather than the store so the order is fixed by the type, not by
// key layout.
func (k Keeper) GetAllZones(ctx context.Context) ([]types.Zone, error) {
	out := make([]types.Zone, 0, types.ZoneCount)
	for _, id := range types.AllZoneIDs() {
		zone, found, err := k.GetZone(ctx, id)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		out = append(out, zone)
	}
	return out, nil
}

// setJSON marshals value under key. json.Marshal of a struct emits fields in
// declaration order, so encoding is deterministic (I-22). Note this is
// per-ENTITY encoding -- one key, one entity -- not the blob pattern: the object
// being marshalled here is a single small struct, never a collection of them.
func (k Keeper) setJSON(ctx context.Context, key []byte, value any) error {
	bz, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal aez value: %w", err)
	}
	store := k.storeService.OpenKVStore(ctx)
	return store.Set(key, bz)
}

// getJSON reads and unmarshals key, reporting whether it existed.
func (k Keeper) getJSON(ctx context.Context, key []byte, target any) (bool, error) {
	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get(key)
	if err != nil {
		return false, err
	}
	if len(bz) == 0 {
		return false, nil
	}
	if err := json.Unmarshal(bz, target); err != nil {
		return false, fmt.Errorf("failed to unmarshal aez value: %w", err)
	}
	return true, nil
}

// has reports whether key exists.
func (k Keeper) has(ctx context.Context, key []byte) (bool, error) {
	store := k.storeService.OpenKVStore(ctx)
	return store.Has(key)
}
