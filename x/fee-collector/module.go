package feecollector

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/spf13/cobra"

	"cosmossdk.io/core/appmodule"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/sovereign-l1/l1/x/fee-collector/keeper"
	"github.com/sovereign-l1/l1/x/fee-collector/types"
)

const ConsensusVersion = 1

var (
	_	module.AppModuleBasic	= AppModule{}
	_	module.HasGenesis	= AppModule{}
	_	module.HasServices	= AppModule{}
	_	appmodule.AppModule	= AppModule{}
	_	appmodule.HasEndBlocker	= AppModule{}
)

type AppModule struct {
	cdc	codec.Codec
	keeper	keeper.Keeper
}

func NewAppModule(cdc codec.Codec, k keeper.Keeper) AppModule {
	return AppModule{cdc: cdc, keeper: k}
}

func (AppModule) IsOnePerModuleType()	{}

func (AppModule) IsAppModule()	{}
func (AppModule) Name() string	{ return types.ModuleName }
func (AppModule) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	types.RegisterLegacyAminoCodec(cdc)
}
func (AppModule) RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	types.RegisterInterfaces(registry)
}

func (AppModule) RegisterGRPCGatewayRoutes(clientCtx client.Context, mux *runtime.ServeMux) {
	if err := types.RegisterQueryHandlerClient(context.Background(), mux, types.NewQueryClient(clientCtx)); err != nil {
		panic(err)
	}
}

func (am AppModule) RegisterServices(cfg module.Configurator) {
	types.RegisterMsgServer(cfg.MsgServer(), keeper.NewMsgServerImpl(am.keeper))
	types.RegisterQueryServer(cfg.QueryServer(), am.keeper)
}

func (am AppModule) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	return cdc.MustMarshalJSON(types.DefaultGenesisState())
}

func (am AppModule) ValidateGenesis(cdc codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	var gs types.GenesisState
	if err := cdc.UnmarshalJSON(bz, &gs); err != nil {
		return fmt.Errorf("failed to unmarshal %s genesis state: %w", types.ModuleName, err)
	}
	return gs.Validate()
}

func (am AppModule) InitGenesis(ctx sdk.Context, cdc codec.JSONCodec, bz json.RawMessage) {
	var gs types.GenesisState
	if err := cdc.UnmarshalJSON(bz, &gs); err != nil {
		panic(fmt.Errorf("failed to unmarshal %s genesis state: %w", types.ModuleName, err))
	}
	if err := am.keeper.InitGenesis(ctx, gs); err != nil {
		panic(fmt.Errorf("failed to initialize %s genesis: %w", types.ModuleName, err))
	}
}

func (am AppModule) ExportGenesis(ctx sdk.Context, cdc codec.JSONCodec) json.RawMessage {
	gs, err := am.keeper.ExportGenesis(ctx)
	if err != nil {
		panic(fmt.Errorf("failed to export %s genesis: %w", types.ModuleName, err))
	}
	return cdc.MustMarshalJSON(gs)
}

// EventTypeFeeDistributionSkipped is emitted when the automatic per-block
// fee distribution fails and is skipped instead of propagating the error.
// Skipping (rather than erroring) keeps a poisoned or colliding FeeHistory
// entry -- e.g. one planted at a future height by a governance-submitted
// MsgDistributeFees -- from halting the chain in EndBlock. See security
// audit finding F-15: MsgDistributeFees epoch/height collision chain halt.
const EventTypeFeeDistributionSkipped = "fee_distribution_skipped"

func (am AppModule) EndBlock(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	pending, err := am.keeper.GetPendingDistribution(ctx)
	if err != nil {
		return err
	}
	if pending.Total().Empty() {
		return nil
	}
	height := uint64(sdkCtx.BlockHeight())
	if _, err := am.keeper.DistributeFees(ctx, height); err != nil {
		// Any failure here (duplicate history from a colliding epoch,
		// accounting invariant, etc.) must not propagate out of EndBlock --
		// doing so would deterministically halt every validator. Log and
		// emit an event instead so the skip is observable, and let the
		// pending fees carry over to the next block's attempt.
		sdkCtx.Logger().Error("fee distribution skipped: automatic distribution failed",
			"height", height, "err", err.Error())
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			EventTypeFeeDistributionSkipped,
			sdk.NewAttribute(types.AttributeKeyEpoch, fmt.Sprintf("%d", height)),
			sdk.NewAttribute("reason", err.Error()),
		))
		return nil
	}
	return nil
}

func (am AppModule) ConsensusVersion() uint64		{ return ConsensusVersion }
func (am AppModule) GetTxCmd() *cobra.Command		{ return nil }
func (am AppModule) GetQueryCmd() *cobra.Command	{ return nil }
