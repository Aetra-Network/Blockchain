package aez

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

	"github.com/sovereign-l1/l1/x/aez/keeper"
	"github.com/sovereign-l1/l1/x/aez/types"
	"github.com/sovereign-l1/l1/x/internal/prototype"
)

const ConsensusVersion = prototype.CurrentGenesisVersion

// x/aez is registered as a SYSTEM module (app/wiring/aetracore/modules.go).
//
// Phase 2 graduated it out of prototypeModules, exactly as x/contracts did, and
// the graduation is what unblocks the two additions below. The prototype family
// is asserted to implement NEITHER appmodule.HasBeginBlocker NOR
// appmodule.HasEndBlocker (app/aetra_core_wiring_test.go), so a BeginBlocker
// could not have landed while x/aez was a prototype -- the promotion and the
// BeginBlocker are necessarily one change.
//
// What x/aez now has, and what it still does not:
//
//   - HasBeginBlocker: YES. It swaps a pending routing table into the active one
//     at its ActivationHeight. With no pending table it is one store read and a
//     nil return (I-23). See keeper/abci.go for why Begin and not End.
//   - Msg service: YES, exactly one -- MsgUpdateRoutingTable, gated on the gov
//     module account. Phase 1's "no handler exists" is over; the guarantee is now
//     "one handler, governance-only, and it cannot move a bucket off the Core
//     Zone" (keeper.ValidateRoutingTableTransition).
//   - HasEndBlocker: still NO. The Phase 4 message-bus drain does not exist yet.
//   - Module account: still NO, and never (I-10/I-11). The wiring gate enforces
//     this for system modules with the identical rule it applied to prototypes,
//     so the promotion bought no relief here.
var (
	_	module.AppModuleBasic		= AppModule{}
	_	module.HasGenesis		= AppModule{}
	_	module.HasServices		= AppModule{}
	_	appmodule.AppModule		= AppModule{}
	_	appmodule.HasBeginBlocker	= AppModule{}
)

type AppModule struct {
	keeper *keeper.Keeper
}

func NewAppModule(k *keeper.Keeper) AppModule	{ return AppModule{keeper: k} }

func (AppModule) IsOnePerModuleType()	{}
func (AppModule) IsAppModule()		{}
func (AppModule) Name() string		{ return types.ModuleName }

func (AppModule) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	types.RegisterLegacyAminoCodec(cdc)
}

func (AppModule) RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	types.RegisterInterfaces(registry)
}

// RegisterGRPCGatewayRoutes is a no-op: the hand-written Query descriptors carry
// no grpc-gateway annotations (see x/aez/types/service.go on why the descriptors
// are hand-written rather than buf-generated).
func (AppModule) RegisterGRPCGatewayRoutes(_ client.Context, _ *runtime.ServeMux)	{}

// RegisterServices registers the Msg and Query services.
func (am AppModule) RegisterServices(cfg module.Configurator) {
	types.RegisterMsgServer(cfg.MsgServer(), keeper.NewMsgServerImpl(am.keeper))
	types.RegisterQueryServer(cfg.QueryServer(), keeper.NewQueryServerImpl(am.keeper))
}

// BeginBlock activates a pending routing table at its ActivationHeight. See
// keeper/abci.go for the placement rationale.
func (am AppModule) BeginBlock(ctx context.Context) error {
	return am.keeper.BeginBlocker(ctx)
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
