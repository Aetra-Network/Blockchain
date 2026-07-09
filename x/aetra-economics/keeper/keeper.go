package keeper

import (
	"context"
	"encoding/json"

	corestore "cosmossdk.io/core/store"

	"github.com/sovereign-l1/l1/x/aetra-economics/types"
)

var genesisKey = []byte{0x01}

type Keeper struct {
	// state holds the genesis defaults and, for the non-persistent (test) keeper
	// with no storeService, the in-memory state. For the persistent keeper the
	// authoritative state lives in the KV store and is read/written through the
	// live block context on every access — never through a cached context.
	// See SEC-MED: aetra-economics persists through a stale genesis context.
	state        types.GenesisState
	storeService corestore.KVStoreService
}

func NewKeeper(authority string) Keeper {
	return Keeper{state: types.DefaultGenesisState(authority)}
}

func NewPersistentKeeper(storeService corestore.KVStoreService, authority string) Keeper {
	return Keeper{state: types.DefaultGenesisState(authority), storeService: storeService}
}

// Authority returns the constitutional (genesis) authority. It is fixed at
// genesis and is not changed by parameter updates, so it does not require the
// block context.
func (k Keeper) Authority() string {
	return k.state.Params.Authority
}

// getState returns the authoritative state for the given block context. For the
// persistent keeper it reads from the KV store (falling back to the genesis
// defaults before the first write); for the in-memory keeper it returns k.state.
func (k Keeper) getState(ctx context.Context) (types.GenesisState, error) {
	if k.storeService == nil {
		return k.state, nil
	}
	bz, err := k.storeService.OpenKVStore(ctx).Get(genesisKey)
	if err != nil {
		return types.GenesisState{}, err
	}
	if len(bz) == 0 {
		return k.state, nil
	}
	var gs types.GenesisState
	if err := json.Unmarshal(bz, &gs); err != nil {
		return types.GenesisState{}, err
	}
	if err := gs.Validate(); err != nil {
		return types.GenesisState{}, err
	}
	return gs, nil
}

// setState persists the state through the given block context. For the in-memory
// keeper it updates k.state; for the persistent keeper it writes to the block's
// KV store so the change is committed and identical across all nodes.
func (k *Keeper) setState(ctx context.Context, gs types.GenesisState) error {
	if err := gs.Validate(); err != nil {
		return err
	}
	if k.storeService == nil {
		k.state = gs
		return nil
	}
	bz, err := json.Marshal(gs)
	if err != nil {
		return err
	}
	return k.storeService.OpenKVStore(ctx).Set(genesisKey, bz)
}

func (k Keeper) Params(ctx context.Context) (types.Params, error) {
	state, err := k.getState(ctx)
	if err != nil {
		return types.Params{}, err
	}
	return state.Params, nil
}

func (k *Keeper) SetParams(ctx context.Context, params types.Params) error {
	if err := params.Validate(); err != nil {
		return types.ErrInvalidParams.Wrap(err.Error())
	}
	next, err := k.getState(ctx)
	if err != nil {
		return err
	}
	next.Params = params
	if err := next.Validate(); err != nil {
		return types.ErrInvalidParams.Wrap(err.Error())
	}
	return k.setState(ctx, next)
}

func (k *Keeper) ApplyEpoch(ctx context.Context, input types.EpochEconomicsInput) (types.EpochRewardSummary, error) {
	state, err := k.getState(ctx)
	if err != nil {
		return types.EpochRewardSummary{}, err
	}
	nextState, summary, err := types.ApplyEpoch(state.Params, state.State, input)
	if err != nil {
		return types.EpochRewardSummary{}, err
	}
	state.State = nextState
	if err := k.setState(ctx, state); err != nil {
		return types.EpochRewardSummary{}, err
	}
	return summary, nil
}

func (k Keeper) QueryCurrentInflation(ctx context.Context, req types.QueryCurrentInflationRequest) (types.QueryCurrentInflationResponse, error) {
	state, err := k.getState(ctx)
	if err != nil {
		return types.QueryCurrentInflationResponse{}, err
	}
	return types.QueryCurrentInflationResponse{InflationBps: state.State.CurrentInflationBps}, nil
}

func (k Keeper) QueryCurrentBondedRatio(ctx context.Context, req types.QueryCurrentBondedRatioRequest) (types.QueryCurrentBondedRatioResponse, error) {
	state, err := k.getState(ctx)
	if err != nil {
		return types.QueryCurrentBondedRatioResponse{}, err
	}
	return types.QueryCurrentBondedRatioResponse{BondedRatioBps: state.State.CurrentBondedRatioBps}, nil
}

func (k Keeper) QueryEstimatedAPR(ctx context.Context, req types.QueryEstimatedAPRRequest) (types.QueryEstimatedAPRResponse, error) {
	state, err := k.getState(ctx)
	if err != nil {
		return types.QueryEstimatedAPRResponse{}, err
	}
	return types.EstimateAPRBreakdown(state.Params, state.State, req)
}

func (k Keeper) QueryFeeSplitParams(ctx context.Context, req types.QueryFeeSplitParamsRequest) (types.QueryFeeSplitParamsResponse, error) {
	state, err := k.getState(ctx)
	if err != nil {
		return types.QueryFeeSplitParamsResponse{}, err
	}
	params := state.Params
	return types.QueryFeeSplitParamsResponse{
		BurnMinBps:			params.BurnMinBps,
		BurnMaxBps:			params.BurnMaxBps,
		BurnCurrentBps:			params.BurnCurrentBps,
		ValidatorRewardMinBps:		params.ValidatorRewardMinBps,
		ValidatorRewardMaxBps:		params.ValidatorRewardMaxBps,
		ValidatorRewardBps:		params.ValidatorRewardBps,
		TreasuryMinBps:			params.TreasuryMinBps,
		TreasuryMaxBps:			params.TreasuryMaxBps,
		TreasuryBps:			params.TreasuryBps,
		EmergencyAllowZeroRewardShare:	params.EmergencyAllowZeroRewardShare,
	}, nil
}

func (k Keeper) QueryBurnedSupply(ctx context.Context, req types.QueryBurnedSupplyRequest) (types.QueryBurnedSupplyResponse, error) {
	state, err := k.getState(ctx)
	if err != nil {
		return types.QueryBurnedSupplyResponse{}, err
	}
	return types.QueryBurnedSupplyResponse{BurnedSupply: state.State.BurnedSupply}, nil
}

func (k Keeper) QueryTreasuryBalance(ctx context.Context, req types.QueryTreasuryBalanceRequest) (types.QueryTreasuryBalanceResponse, error) {
	state, err := k.getState(ctx)
	if err != nil {
		return types.QueryTreasuryBalanceResponse{}, err
	}
	return types.QueryTreasuryBalanceResponse{TreasuryBalance: state.State.TreasuryBalance}, nil
}

func (k Keeper) QueryEpochRewardSummary(ctx context.Context, req types.QueryEpochRewardSummaryRequest) (types.QueryEpochRewardSummaryResponse, error) {
	state, err := k.getState(ctx)
	if err != nil {
		return types.QueryEpochRewardSummaryResponse{}, err
	}
	for _, summary := range state.State.RewardHistory {
		if summary.Epoch == req.Epoch {
			return types.QueryEpochRewardSummaryResponse{Summary: summary}, nil
		}
	}
	return types.QueryEpochRewardSummaryResponse{}, types.ErrNotFound
}

func (k *Keeper) InitGenesis(gs types.GenesisState) error {
	if err := gs.Validate(); err != nil {
		return err
	}
	k.state = gs
	return nil
}

func (k *Keeper) InitGenesisState(ctx context.Context, gs types.GenesisState) error {
	if err := gs.Validate(); err != nil {
		return err
	}
	k.state = gs
	if k.storeService == nil {
		return nil
	}
	return k.setState(ctx, gs)
}

func (k Keeper) ExportGenesis() (types.GenesisState, error) {
	if err := k.state.Validate(); err != nil {
		return types.GenesisState{}, err
	}
	return k.state, nil
}

func (k Keeper) ExportGenesisState(ctx context.Context) (types.GenesisState, error) {
	if k.storeService == nil {
		return k.ExportGenesis()
	}
	return k.getState(ctx)
}

func (k Keeper) MarshalGenesis() ([]byte, error) {
	gs, err := k.ExportGenesis()
	if err != nil {
		return nil, err
	}
	return json.Marshal(gs)
}

func (k *Keeper) UnmarshalGenesis(bz []byte) error {
	var gs types.GenesisState
	if err := json.Unmarshal(bz, &gs); err != nil {
		return err
	}
	return k.InitGenesis(gs)
}
