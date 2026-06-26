package services

import (
	"encoding/json"
	"fmt"

	"github.com/grpc-ecosystem/grpc-gateway/runtime"

	"cosmossdk.io/core/appmodule"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/sovereign-l1/l1/x/services/keeper"
	"github.com/sovereign-l1/l1/x/services/types"
)

const ConsensusVersion = 1

var (
	_	module.AppModuleBasic	= AppModule{}
	_	module.HasGenesis	= AppModule{}
	_	module.HasServices	= AppModule{}
	_	appmodule.AppModule	= AppModule{}
)

type AppModule struct {
	Keeper keeper.Keeper
}

func NewAppModule(k keeper.Keeper) AppModule {
	return AppModule{Keeper: k}
}

func (AppModule) IsOnePerModuleType()	{}
func (AppModule) IsAppModule()		{}
func (AppModule) Name() string		{ return types.ModuleName }

func (AppModule) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	types.RegisterLegacyAminoCodec(cdc)
}

func (AppModule) RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	types.RegisterInterfaces(registry)
}

func (AppModule) RegisterGRPCGatewayRoutes(client.Context, *runtime.ServeMux) {}

func (am AppModule) RegisterServices(cfg module.Configurator) {
	types.RegisterMsgServer(cfg.MsgServer(), keeper.NewMsgServerImpl(&am.Keeper))
	types.RegisterQueryServer(cfg.QueryServer(), am.Keeper)
}

func (AppModule) DefaultGenesis(codec.JSONCodec) json.RawMessage {
	return mustMarshalGenesis(types.ModuleName, types.DefaultGenesis())
}

func (AppModule) ValidateGenesis(_ codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	var gs types.GenesisState
	if err := unmarshalGenesis(types.ModuleName, bz, &gs); err != nil {
		return err
	}
	return gs.Validate()
}

func (am AppModule) InitGenesis(ctx sdk.Context, _ codec.JSONCodec, bz json.RawMessage) {
	var gs types.GenesisState
	if err := unmarshalGenesis(types.ModuleName, bz, &gs); err != nil {
		panic(err)
	}
	if err := am.Keeper.InitGenesis(gs); err != nil {
		panic(fmt.Errorf("failed to initialize %s genesis: %w", types.ModuleName, err))
	}
}

func (am AppModule) ExportGenesis(ctx sdk.Context, _ codec.JSONCodec) json.RawMessage {
	return mustMarshalGenesis(types.ModuleName, am.Keeper.ExportGenesis())
}

func (AppModule) ConsensusVersion() uint64	{ return ConsensusVersion }

func mustMarshalGenesis(moduleName string, value any) json.RawMessage {
	bz, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Errorf("failed to marshal %s genesis: %w", moduleName, err))
	}
	return bz
}

func unmarshalGenesis(moduleName string, bz json.RawMessage, target any) error {
	if len(bz) == 0 {
		return fmt.Errorf("missing %s genesis state", moduleName)
	}
	if err := json.Unmarshal(bz, target); err != nil {
		return fmt.Errorf("failed to unmarshal %s genesis state: %w", moduleName, err)
	}
	return nil
}