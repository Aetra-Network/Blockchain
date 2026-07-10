package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sovereign-l1/l1/x/fees/types"
	loadtypes "github.com/sovereign-l1/l1/x/load/types"
)

// LoadSink receives the finalized per-block load metrics. Implemented by the
// x/load keeper; optional (nil = no feed).
type LoadSink interface {
	ApplyBlockMetrics(ctx context.Context, metrics loadtypes.Metrics) (loadtypes.Result, bool, error)
}

// getKVCongestionBps reads the stored block utilization bps from KV-state.
// Uses the last finalized block's utilization (set by EndBlocker) so the value is
// deterministic and based only on committed state (Requirement 1.3).
// Falls back to live gas meter calculation if no stored state exists yet.
func (k Keeper) getKVCongestionBps(ctx sdk.Context, blockGasConsumed, txGasLimit, maxBlockGas uint64) uint32 {
	bz, err := k.storeService.OpenKVStore(ctx).Get(types.CongestionStateKey)
	if err == nil && len(bz) == 4 {
		stored := uint32(bz[0])<<24 | uint32(bz[1])<<16 | uint32(bz[2])<<8 | uint32(bz[3])
		if stored <= uint32(types.BasisPoints) {
			return stored
		}
	}
	return types.BlockUtilizationBps(blockGasConsumed, txGasLimit, maxBlockGas)
}

// EndBlocker records the finalized block utilization as congestion state.
// Called from the AppModule EndBlock after all transactions are processed.
func (k Keeper) EndBlocker(ctx sdk.Context) error {
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	gasConsumed := uint64(0)
	if ctx.BlockGasMeter() != nil {
		gasConsumed = ctx.BlockGasMeter().GasConsumed()
	}
	utilBps := types.BlockUtilizationBps(gasConsumed, 0, params.MaxBlockGas)
	if err := k.SetCongestionState(ctx, utilBps); err != nil {
		return err
	}
	// Feed the same finalized, deterministic block signal into the x/load
	// scorer (EMA + band). This is the live input the zone/routing layer
	// consumes for load distribution once enabled; while x/load is disabled
	// the sink is a silent no-op. Fee congestion backpressure above never
	// depends on it.
	if k.loadSink != nil {
		if _, _, err := k.loadSink.ApplyBlockMetrics(ctx, loadtypes.Metrics{
			UsedBlockGas: gasConsumed,
		}); err != nil {
			return err
		}
	}
	return nil
}

// SetCongestionState stores the finalized block_utilization_bps for the given height.
// Must be called from EndBlocker after block gas is finalized (Requirement 1.3).
func (k Keeper) SetCongestionState(ctx sdk.Context, utilizationBps uint32) error {
	bz := []byte{
		byte(utilizationBps >> 24),
		byte(utilizationBps >> 16),
		byte(utilizationBps >> 8),
		byte(utilizationBps),
	}
	return k.storeService.OpenKVStore(ctx).Set(types.CongestionStateKey, bz)
}

// GetCongestionState reads the stored block utilization bps.
func (k Keeper) GetCongestionState(ctx sdk.Context) uint32 {
	bz, err := k.storeService.OpenKVStore(ctx).Get(types.CongestionStateKey)
	if err != nil || len(bz) != 4 {
		return 0
	}
	return uint32(bz[0])<<24 | uint32(bz[1])<<16 | uint32(bz[2])<<8 | uint32(bz[3])
}
