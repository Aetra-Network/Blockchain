package aez

import (
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

	"github.com/sovereign-l1/l1/x/aez/keeper"
	"github.com/sovereign-l1/l1/x/aez/types"
	"github.com/sovereign-l1/l1/x/internal/prototype"
)

const ConsensusVersion = prototype.CurrentGenesisVersion

// x/aez is registered as a PROTOTYPE module (app/wiring/aetracore/modules.go).
//
// The interface list below is deliberately short, and every omission is load-
// bearing. app/aetra_core_wiring_test.go:38-63 iterates EVERY prototype module
// and asserts it implements NEITHER appmodule.HasBeginBlocker NOR
// appmodule.HasEndBlocker, while still appearing in both order lists as a no-op
// position. So:
//
//   - NO BeginBlock, NO EndBlock. Phase 1 is inert. The Phase 4 drain EndBlocker
//     cannot land while x/aez is a prototype; it graduates into systemModules
//     first, exactly as x/contracts did (modules.go:81-87).
//   - NO module.HasServices Msg registration. Query only -- there is no
//     transaction that can move the routing table.
var (
	_	module.AppModuleBasic	= AppModule{}
	_	module.HasGenesis	= AppModule{}
	_	module.HasServices	= AppModule{}
	_	appmodule.AppModule	= AppModule{}
)

type AppModule struct {
	keeper *keeper.Keeper
}

func NewAppModule(k *keeper.Keeper) AppModule	{ return AppModule{keeper: k} }

func (AppModule) IsOnePerModuleType()	{}
func (AppModule) IsAppModule()		{}
func (AppModule) Name() string		{ return types.ModuleName }

// RegisterLegacyAminoCodec and RegisterInterfaces are no-ops: x/aez defines no
// Msg types, so it has nothing to register into the interface registry.
func (AppModule) RegisterLegacyAminoCodec(_ *codec.LegacyAmino)		{}
func (AppModule) RegisterInterfaces(_ codectypes.InterfaceRegistry)	{}

// RegisterGRPCGatewayRoutes is a no-op: the hand-written Query descriptors carry
// no grpc-gateway annotations (see x/aez/types/service.go on why the descriptors
// are hand-written rather than buf-generated).
func (AppModule) RegisterGRPCGatewayRoutes(_ client.Context, _ *runtime.ServeMux)	{}

// RegisterServices registers the Query service and NOTHING else. There is
// deliberately no cfg.MsgServer() registration.
func (am AppModule) RegisterServices(cfg module.Configurator) {
	types.RegisterQueryServer(cfg.QueryServer(), keeper.NewQueryServerImpl(am.keeper))
}

func (AppModule) DefaultGenesis(codec.JSONCodec) json.RawMessage {
	return mustMarshalGenesis(types.ModuleName, keeper.DefaultGenesis())
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
	if err := am.keeper.InitGenesisState(ctx, gs); err != nil {
		panic(fmt.Errorf("failed to initialize %s genesis: %w", types.ModuleName, err))
	}
}

func (am AppModule) ExportGenesis(ctx sdk.Context, _ codec.JSONCodec) json.RawMessage {
	gs, err := am.keeper.ExportGenesisState(ctx)
	if err != nil {
		panic(fmt.Errorf("failed to export %s genesis: %w", types.ModuleName, err))
	}
	return mustMarshalGenesis(types.ModuleName, gs)
}

func (AppModule) ConsensusVersion() uint64	{ return ConsensusVersion }
func (AppModule) GetTxCmd() *cobra.Command	{ return nil }
func (AppModule) GetQueryCmd() *cobra.Command {
	return nil
}

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
