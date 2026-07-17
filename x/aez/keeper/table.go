package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sovereign-l1/l1/x/aez/types"
)

// SetRoutingTableVersion stores one routing table under its own per-version key.
// It does not make it current.
func (k Keeper) SetRoutingTableVersion(ctx context.Context, table types.RoutingTable) error {
	if err := table.Validate(); err != nil {
		return err
	}
	return k.setJSON(ctx, types.RoutingTableVersionKey(table.Version), table)
}

// GetRoutingTableVersion reads the table stored at a specific version.
//
// It re-validates on read, so a table whose bytes were tampered with in the
// store fails its committed TableHash rather than silently routing entities
// somewhere new.
func (k Keeper) GetRoutingTableVersion(ctx context.Context, version uint64) (types.RoutingTable, bool, error) {
	var table types.RoutingTable
	found, err := k.getJSON(ctx, types.RoutingTableVersionKey(version), &table)
	if err != nil {
		return types.RoutingTable{}, false, err
	}
	if !found {
		return types.RoutingTable{}, false, nil
	}
	if err := table.Validate(); err != nil {
		return types.RoutingTable{}, false, fmt.Errorf("stored routing table version %d is invalid: %w", version, err)
	}
	return table, true, nil
}

// setCurrentVersion points the current pointer at a version.
func (k Keeper) setCurrentVersion(ctx context.Context, version uint64) error {
	store := k.storeService.OpenKVStore(ctx)
	return store.Set(types.RoutingTableCurrentKey, types.EncodeUint64(version))
}

// GetCurrentVersion returns the current routing table version.
func (k Keeper) GetCurrentVersion(ctx context.Context) (uint64, bool, error) {
	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get(types.RoutingTableCurrentKey)
	if err != nil {
		return 0, false, err
	}
	if len(bz) == 0 {
		return 0, false, nil
	}
	version, ok := types.DecodeUint64(bz)
	if !ok {
		return 0, false, fmt.Errorf("aez current routing table version is corrupt")
	}
	return version, true, nil
}

// GetPendingVersion returns the pending routing table version, if any.
func (k Keeper) GetPendingVersion(ctx context.Context) (uint64, bool, error) {
	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get(types.RoutingTablePendingKey)
	if err != nil {
		return 0, false, err
	}
	if len(bz) == 0 {
		return 0, false, nil
	}
	version, ok := types.DecodeUint64(bz)
	if !ok {
		return 0, false, fmt.Errorf("aez pending routing table version is corrupt")
	}
	return version, true, nil
}

func (k Keeper) clearPendingVersion(ctx context.Context) error {
	store := k.storeService.OpenKVStore(ctx)
	return store.Delete(types.RoutingTablePendingKey)
}

// GetRoutingTable returns the current routing table.
//
// It reads the current version pointer from the store on EVERY call and never
// caches. A restarted node and a continuously-running node therefore read the
// same committed bytes and take the same branch (the F-17 regression guard,
// I-20).
func (k Keeper) GetRoutingTable(ctx context.Context) (types.RoutingTable, error) {
	version, found, err := k.GetCurrentVersion(ctx)
	if err != nil {
		return types.RoutingTable{}, err
	}
	if !found {
		return types.RoutingTable{}, types.ErrRoutingTableNotFound
	}
	table, found, err := k.GetRoutingTableVersion(ctx, version)
	if err != nil {
		return types.RoutingTable{}, err
	}
	if !found {
		return types.RoutingTable{}, fmt.Errorf("%w: current version %d", types.ErrRoutingTableNotFound, version)
	}
	return table, nil
}

// SetPendingRoutingTable schedules a table to become current at its
// ActivationHeight.
//
// It enforces, in order:
//
//   - version monotonicity: the new version must strictly exceed the current one
//     (I-8). A table that could reuse or lower a version could rewrite routing
//     history.
//   - epoch-boundary activation: ActivationHeight must be an exact routing-epoch
//     boundary AND strictly in the future. "Strictly in the future" is what makes
//     a mid-epoch swap unrepresentable rather than merely discouraged -- a table
//     activating at the current height would take effect part-way through a block
//     whose earlier transactions already resolved against the old table.
func (k Keeper) SetPendingRoutingTable(ctx context.Context, table types.RoutingTable) error {
	if err := table.Validate(); err != nil {
		return err
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	current, found, err := k.GetCurrentVersion(ctx)
	if err != nil {
		return err
	}
	if found && table.Version <= current {
		return fmt.Errorf("%w: pending version %d must exceed current version %d", types.ErrRoutingTableVersion, table.Version, current)
	}
	height := sdk.UnwrapSDKContext(ctx).BlockHeight()
	if table.ActivationHeight <= height {
		return fmt.Errorf("%w: activation height %d must be in the future (current height %d)", types.ErrRoutingEpochBoundary, table.ActivationHeight, height)
	}
	if !types.IsRoutingEpochBoundary(table.ActivationHeight, params.RoutingEpochLength) {
		return fmt.Errorf("%w: activation height %d is not a multiple of the routing epoch length %d", types.ErrRoutingEpochBoundary, table.ActivationHeight, params.RoutingEpochLength)
	}
	if err := k.SetRoutingTableVersion(ctx, table); err != nil {
		return err
	}
	store := k.storeService.OpenKVStore(ctx)
	return store.Set(types.RoutingTablePendingKey, types.EncodeUint64(table.Version))
}

// MaybeActivatePendingRoutingTable promotes a pending table to current when the
// block height reaches its ActivationHeight exactly, and reports whether it did.
//
// The comparison is height >= ActivationHeight, not ==. Equality would be a
// liveness bug: if the activation height were ever skipped (an empty block range
// or a missed invocation), a table pinned to == would remain pending forever and
// the chain would silently route on a stale table. >= is still deterministic --
// every node evaluates the same committed ActivationHeight against the same
// height -- and it activates at exactly its height on the happy path, which
// table_test.go asserts block by block (not one early, not one late).
//
// Phase 1 note: nothing calls this on a block boundary. x/aez registers NO
// BeginBlocker and NO EndBlocker (the wiring gate asserts their absence for
// every prototype module, app/aetra_core_wiring_test.go:57-60), so Phase 1 is
// inert by construction. The activation rule is implemented and tested here so
// the semantics are frozen before anything depends on them; the caller arrives
// when x/aez graduates to systemModules.
func (k Keeper) MaybeActivatePendingRoutingTable(ctx context.Context) (bool, error) {
	pending, found, err := k.GetPendingVersion(ctx)
	if err != nil || !found {
		return false, err
	}
	table, found, err := k.GetRoutingTableVersion(ctx, pending)
	if err != nil {
		return false, err
	}
	if !found {
		return false, fmt.Errorf("%w: pending version %d", types.ErrRoutingTableNotFound, pending)
	}
	if sdk.UnwrapSDKContext(ctx).BlockHeight() < table.ActivationHeight {
		return false, nil
	}
	if err := k.setCurrentVersion(ctx, table.Version); err != nil {
		return false, err
	}
	if err := k.clearPendingVersion(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// InitRoutingTable installs the genesis table as current. It is genesis-only:
// it refuses to overwrite an existing current pointer.
func (k Keeper) InitRoutingTable(ctx context.Context, table types.RoutingTable) error {
	if err := table.Validate(); err != nil {
		return err
	}
	if _, found, err := k.GetCurrentVersion(ctx); err != nil {
		return err
	} else if found {
		return fmt.Errorf("aez routing table is already initialized")
	}
	if err := k.SetRoutingTableVersion(ctx, table); err != nil {
		return err
	}
	return k.setCurrentVersion(ctx, table.Version)
}
