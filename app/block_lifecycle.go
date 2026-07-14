package app

import (
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/sovereign-l1/l1/app/lifecycle"
)

func (app *L1App) Name() string	{ return app.BaseApp.Name() }

func (app *L1App) PreBlocker(ctx sdk.Context, _ *abci.RequestFinalizeBlock) (*sdk.ResponsePreBlock, error) {
	return app.ModuleManager.PreBlock(ctx)
}

func (app *L1App) BeginBlocker(ctx sdk.Context) (sdk.BeginBlock, error) {
	return app.ModuleManager.BeginBlock(ctx)
}

func (app *L1App) EndBlocker(ctx sdk.Context) (sdk.EndBlock, error) {
	res, err := app.ModuleManager.EndBlock(ctx)
	if err != nil {
		return res, err
	}
	if err := app.maybeFinalizeNativeEmissionEpoch(ctx); err != nil {
		return sdk.EndBlock{}, err
	}
	// Pure side-effect: records aggregate validator-health observability gauges
	// from committed state. Never writes to the store and is internally
	// recovered, so it cannot affect the AppHash or halt the block.
	app.recordValidatorObservabilityMetrics(ctx)
	// Pure side-effect: periodically re-runs the critical app-invariant
	// registry (bank supply conservation, emission cap, burn/treasury
	// reconciliation, ...) as a defense-in-depth cross-check. Never writes to
	// the store and is internally recovered, so it cannot affect the AppHash
	// or halt the block; a violation is logged and evented, not returned as
	// an error. See security audit FINDING-002.
	app.maybeRunCriticalInvariants(ctx)
	return res, nil
}

func (app *L1App) FinalizeBlock(req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	res, err := lifecycle.FinalizeBlock(req, app.BaseApp.FinalizeBlock)
	if err != nil {
		return res, err
	}
	if err := app.applyElectionValidatorUpdates(req, res); err != nil {
		return res, err
	}
	return res, nil
}

func (a *L1App) Configurator() module.Configurator {
	return a.configurator
}

func (app *L1App) InitChainer(ctx sdk.Context, req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	return lifecycle.InitChain(ctx, req, lifecycle.InitChainDependencies{
		AppCodec:	app.appCodec,
		ModuleManager:	app.ModuleManager,
		SetModuleVersionMap: func(ctx sdk.Context, versionMap module.VersionMap) error {
			return app.UpgradeKeeper.SetModuleVersionMap(ctx, versionMap)
		},
		ValidateGenesis:		app.validateAetraGenesis,
		EnsureCoreGenesisCollections:	app.ensureCoreGenesisCollections,
	})
}
