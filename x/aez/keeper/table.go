package keeper

import (
	"context"
	"encoding/hex"
	"fmt"
	"strconv"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sovereign-l1/l1/x/aez/types"
)

// emitRoutingTableEvent emits one routing-table event with the table's identity
// plus any caller-supplied extra attributes.
//
// It tolerates a context with no EventManager (the keeper unit tests build an
// sdk.Context directly) rather than panicking: an event is observability, and
// failing a block over a missing event manager would trade a real consensus
// property for a cosmetic one.
func emitRoutingTableEvent(ctx context.Context, eventType string, table types.RoutingTable, extra []sdk.Attribute) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if sdkCtx.EventManager() == nil {
		return
	}
	attributes := []sdk.Attribute{
		sdk.NewAttribute(types.AttributeKeyVersion, strconv.FormatUint(table.Version, 10)),
		sdk.NewAttribute(types.AttributeKeyEpoch, strconv.FormatUint(table.Epoch, 10)),
		sdk.NewAttribute(types.AttributeKeyActivationHeight, strconv.FormatInt(table.ActivationHeight, 10)),
		sdk.NewAttribute(types.AttributeKeyTableHash, hex.EncodeToString(table.TableHash)),
	}
	attributes = append(attributes, extra...)
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(eventType, attributes...))
}

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

// StageRoutingTable is the GOVERNANCE path onto the routing table: it applies
// the full Phase 2 policy and then schedules the table.
//
// It is deliberately a layer ABOVE SetPendingRoutingTable rather than a change
// to it. SetPendingRoutingTable is the mechanism (validate, monotonic version,
// epoch boundary, strictly future); StageRoutingTable is the policy (target
// zones must exist, the Core Zone is a one-way trap). Keeping them apart is what
// lets keeper_test.go's adversarial pin test still install a hostile
// all-buckets-to-zone-4 table directly through the mechanism and prove that
// CorePinned holds EVEN THEN -- an assertion that would become unwritable, and
// the pin test vacuous, if policy were welded into the mechanism.
//
// The only consensus-reachable caller is the Msg handler (keeper/msg_server.go),
// which calls this, never SetPendingRoutingTable.
func (k Keeper) StageRoutingTable(ctx context.Context, table types.RoutingTable, authority string) error {
	if err := k.ValidateRoutingTableTransition(ctx, table); err != nil {
		return err
	}
	if err := k.SetPendingRoutingTable(ctx, table); err != nil {
		return err
	}
	emitRoutingTableEvent(ctx, types.EventTypeStageRoutingTable, table, []sdk.Attribute{
		sdk.NewAttribute(types.AttributeKeyAuthority, authority),
	})
	return nil
}

// ValidateRoutingTableTransition enforces the two policy rules that constrain
// WHICH tables governance may stage, on top of the structural rules
// RoutingTable.Validate already enforces.
//
//  1. Every zone a bucket targets must be a REGISTERED zone -- one with a stored
//     descriptor. RoutingTable.Validate only range-checks the zone id against
//     ZoneCount, which is a compile-time constant; this checks committed state,
//     so a table cannot point a bucket at a zone the chain does not actually
//     have.
//
//  2. A bucket currently mapped to the Core Zone may not be moved off it
//     (I-9). The Core Zone is a ONE-WAY TRAP: buckets may move among elastic
//     zones and may move INTO Core, never out.
//
// Rule 2 has a consequence worth stating plainly rather than discovering later:
// genesis maps all 256 buckets to zone 0, so on this chain, today, EVERY bucket
// is core and therefore frozen. The only table governance can currently stage is
// one whose bucket map is identical to the current one -- a no-op that bumps
// Version/Epoch/ActivationHeight and nothing else. That is intentional for Phase
// 2 ("nothing routes on zones yet"): the governance surface, the epoch swap, and
// the observability all ship and are exercised, while the ability to actually
// relocate an entity waits for the phase that gives entities per-zone state to
// be relocated INTO (x/native-account, Phase 3). Relaxing this rule is that
// phase's deliberate decision, not an oversight here.
func (k Keeper) ValidateRoutingTableTransition(ctx context.Context, table types.RoutingTable) error {
	if err := table.Validate(); err != nil {
		return err
	}
	// Validate range-checked every zone id, so each is < ZoneCount and this
	// index cannot escape the slice.
	used := make([]bool, types.ZoneCount)
	for i := 0; i < types.BucketCount; i++ {
		used[uint32(table.Buckets[i])] = true
	}
	// Iterate AllZoneIDs, not the bucket array: at most ZoneCount store reads
	// instead of BucketCount, in an order fixed by the type.
	for _, id := range types.AllZoneIDs() {
		if !used[uint32(id)] {
			continue
		}
		_, found, err := k.GetZone(ctx, id)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("%w: table targets zone %d, which has no registered descriptor", types.ErrInvalidZone, uint32(id))
		}
	}
	current, err := k.GetRoutingTable(ctx)
	if err != nil {
		return err
	}
	for i := 0; i < types.BucketCount; i++ {
		if current.Buckets[i].IsCore() && !table.Buckets[i].IsCore() {
			return fmt.Errorf("%w: bucket %d is mapped to the core zone and cannot be moved to zone %d", types.ErrCoreZoneImmutable, i, uint32(table.Buckets[i]))
		}
	}
	return nil
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
// Phase 2 note: the caller has arrived. x/aez graduated into systemModules and
// this now runs from the module's BeginBlocker (keeper/abci.go) on every block.
// BeginBlock, not EndBlock, is the only placement that keeps ActivationHeight
// honest -- see BeginBlocker's doc comment.
//
// With no pending table this is a single store read returning (false, nil): a
// silent no-op, which is what a disabled x/aez owes every block (I-23). It reads
// only committed state and ctx.BlockHeight() -- no wall clock, no map iteration
// (I-22).
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
	// Read the outgoing version BEFORE the swap so the event can report what
	// was replaced. A missing current pointer is not an error here: the swap
	// is what establishes the new one.
	previous, _, err := k.GetCurrentVersion(ctx)
	if err != nil {
		return false, err
	}
	if err := k.setCurrentVersion(ctx, table.Version); err != nil {
		return false, err
	}
	if err := k.clearPendingVersion(ctx); err != nil {
		return false, err
	}
	emitRoutingTableEvent(ctx, types.EventTypeActivateRoutingTable, table, []sdk.Attribute{
		sdk.NewAttribute(types.AttributeKeyPreviousVersion, strconv.FormatUint(previous, 10)),
	})
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
