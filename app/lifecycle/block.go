package lifecycle

import (
	"encoding/json"
	"fmt"

	abci "github.com/cometbft/cometbft/abci/types"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/sovereign-l1/l1/app/genesisconfig"
	"github.com/sovereign-l1/l1/observability"
)

// FinalizeBlock wraps the BaseApp FinalizeBlock with observability recording.
// All wall-clock reads live inside the observability package (see
// StartFinalizeObservation) so this consensus path stays free of time tokens;
// the metrics are process-local side effects and never influence state.
func FinalizeBlock(req *abci.RequestFinalizeBlock, finalize func(*abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error)) (*abci.ResponseFinalizeBlock, error) {
	observe := observability.StartFinalizeObservation(req.Height, req.Time, len(req.Txs))
	res, err := finalize(req)

	var failedTxCodespaces []string
	if err == nil && res != nil {
		for _, txResult := range res.TxResults {
			if txResult != nil && txResult.Code != 0 {
				failedTxCodespaces = append(failedTxCodespaces, txResult.Codespace)
			}
		}
	}
	observe(err != nil, failedTxCodespaces)
	return res, err
}

type InitChainDependencies struct {
	AppCodec                     codec.Codec
	ModuleManager                *module.Manager
	SetModuleVersionMap          func(sdk.Context, module.VersionMap) error
	ValidateGenesis              func(genesisconfig.State) error
	EnsureCoreGenesisCollections func(sdk.Context) error
}

func InitChain(ctx sdk.Context, req *abci.RequestInitChain, deps InitChainDependencies) (*abci.ResponseInitChain, error) {
	var genesisState genesisconfig.State
	if err := json.Unmarshal(req.AppStateBytes, &genesisState); err != nil {
		return nil, fmt.Errorf("decode genesis state: %w", err)
	}
	if err := deps.SetModuleVersionMap(ctx, deps.ModuleManager.GetVersionMap()); err != nil {
		return nil, err
	}
	if err := deps.ValidateGenesis(genesisState); err != nil {
		return nil, err
	}
	res, err := deps.ModuleManager.InitGenesis(ctx, deps.AppCodec, genesisState)
	if err != nil {
		return nil, err
	}
	if err := deps.EnsureCoreGenesisCollections(ctx); err != nil {
		return nil, err
	}
	return res, nil
}
