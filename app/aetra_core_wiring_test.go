package app

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/log/v2"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/client/flags"
	sims "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"

	aetracorekeeper "github.com/sovereign-l1/l1/x/aetracore/keeper"
	aetracoretypes "github.com/sovereign-l1/l1/x/aetracore/types"
	aezkeeper "github.com/sovereign-l1/l1/x/aez/keeper"
	aeztypes "github.com/sovereign-l1/l1/x/aez/types"
	contractskeeper "github.com/sovereign-l1/l1/x/contracts/keeper"
	contractstypes "github.com/sovereign-l1/l1/x/contracts/types"
	loadkeeper "github.com/sovereign-l1/l1/x/load/keeper"
	loadtypes "github.com/sovereign-l1/l1/x/load/types"
	meshkeeper "github.com/sovereign-l1/l1/x/mesh/keeper"
	meshtypes "github.com/sovereign-l1/l1/x/mesh/types"
	networkingkeeper "github.com/sovereign-l1/l1/x/networking/keeper"
	networkingtypes "github.com/sovereign-l1/l1/x/networking/types"
	paymentskeeper "github.com/sovereign-l1/l1/x/payments/keeper"
	paymentstypes "github.com/sovereign-l1/l1/x/payments/types"
	routingkeeper "github.com/sovereign-l1/l1/x/routing/keeper"
	routingtypes "github.com/sovereign-l1/l1/x/routing/types"
	schedulerkeeper "github.com/sovereign-l1/l1/x/scheduler/keeper"
	schedulertypes "github.com/sovereign-l1/l1/x/scheduler/types"
)

// keylessPrototypeAuthority is prototype.DefaultAuthority, the all-zero address
// nobody can sign for. x/internal/prototype cannot be imported from app/.
const keylessPrototypeAuthority = "ae1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqp8e93gq"

func TestAetraCoreWiringGateRegistersPrototypeModulesDisabled(t *testing.T) {
	app, genesis := setup(true, 5)

	require.NoError(t, app.ValidateAetraCoreWiringGate())
	require.Equal(t, RoutingExecutionPointAnteAdmissionOnly, AetraCoreRoutingExecutionPoint())

	prototypeModuleNames := AetraCorePrototypeModuleNames()
	prototypeStoreKeys := AetraCorePrototypeStoreKeys()
	require.Len(t, prototypeStoreKeys, len(prototypeModuleNames))
	for i, moduleName := range prototypeModuleNames {
		require.Contains(t, app.ModuleManager.Modules, moduleName)
		require.Contains(t, app.keys, prototypeStoreKeys[i])
		require.Contains(t, genesis, moduleName)
		if IsReservedSystemModuleAccountName(moduleName) {
			require.Contains(t, GetMaccPerms(), moduleName)
			require.Nil(t, GetMaccPerms()[moduleName])
		} else {
			require.NotContains(t, GetMaccPerms(), moduleName)
		}
		_, hasBegin := app.ModuleManager.Modules[moduleName].(appmodule.HasBeginBlocker)
		_, hasEnd := app.ModuleManager.Modules[moduleName].(appmodule.HasEndBlocker)
		require.False(t, hasBegin, moduleName)
		require.False(t, hasEnd, moduleName)
		require.True(t, slices.Contains(app.ModuleManager.OrderBeginBlockers, moduleName), moduleName)
		require.True(t, slices.Contains(app.ModuleManager.OrderEndBlockers, moduleName), moduleName)
	}

	// AEZ Phase 2: x/aez left the prototype family, so the loop above no
	// longer covers it. Without this loop NOTHING in the test suite would
	// assert that a system module is registered, store-mounted and
	// custody-free -- the gate checks it at startup, but a gate that only
	// runs in a panic path is not a test.
	//
	// The Begin/EndBlocker assertions are deliberately ABSENT here rather
	// than forgotten: having a block-lifecycle hook is exactly what
	// distinguishes the system family from the prototype family. x/contracts
	// has an EndBlocker and x/aez now has a BeginBlocker.
	systemModuleNames := AetraCoreSystemModuleNames()
	systemStoreKeys := AetraCoreSystemStoreKeys()
	require.Len(t, systemStoreKeys, len(systemModuleNames))
	for i, moduleName := range systemModuleNames {
		require.Contains(t, app.ModuleManager.Modules, moduleName)
		require.Contains(t, app.keys, systemStoreKeys[i])
		require.Contains(t, genesis, moduleName)
		if IsReservedSystemModuleAccountName(moduleName) {
			require.Contains(t, GetMaccPerms(), moduleName)
			require.Nil(t, GetMaccPerms()[moduleName])
		} else {
			require.NotContains(t, GetMaccPerms(), moduleName)
		}
	}

	// x/aez is a system module now, and must NOT be a prototype one. A
	// module silently present in both lists would satisfy every loop above
	// while making the prototype family's inertness claim false.
	require.Contains(t, systemModuleNames, aeztypes.ModuleName)
	require.NotContains(t, prototypeModuleNames, aeztypes.ModuleName)

	// The BeginBlocker that swaps the routing table is the reason the
	// promotion was necessary. Assert it exists, and that no EndBlocker
	// snuck in with it (the Phase 4 drain does not exist yet).
	_, aezHasBegin := app.ModuleManager.Modules[aeztypes.ModuleName].(appmodule.HasBeginBlocker)
	_, aezHasEnd := app.ModuleManager.Modules[aeztypes.ModuleName].(appmodule.HasEndBlocker)
	require.True(t, aezHasBegin, "x/aez must activate pending routing tables in BeginBlock")
	require.False(t, aezHasEnd, "x/aez has no EndBlocker until the Phase 4 drain lands")
	require.True(t, slices.Contains(app.ModuleManager.OrderBeginBlockers, aeztypes.ModuleName))

	aetherCoreGenesis := decodeJSONGenesis[aetracorekeeper.GenesisState](t, genesis[aetracoretypes.ModuleName])
	require.False(t, aetherCoreGenesis.Params.Enabled)
	require.Empty(t, aetherCoreGenesis.State.ZoneDescriptors)
	require.Empty(t, aetherCoreGenesis.State.ServiceDescriptors)
	require.Empty(t, aetherCoreGenesis.State.ZoneCommitments)
	require.Empty(t, aetherCoreGenesis.State.GlobalRoots)

	loadGenesis := decodeJSONGenesis[loadkeeper.GenesisState](t, genesis[loadtypes.ModuleName])
	require.False(t, loadGenesis.Params.Enabled)
	require.Empty(t, loadGenesis.History)

	routingGenesis := decodeJSONGenesis[routingkeeper.GenesisState](t, genesis[routingtypes.ModuleName])
	require.False(t, routingGenesis.Params.Enabled)
	require.Empty(t, routingGenesis.Shards)

	// x/aez's default genesis is deliberately NON-empty: it ships the full
	// 256-bucket routing table. Every bucket maps to the core zone, so no
	// entity can resolve anywhere else and execution semantics are unchanged
	// -- the assertion is "core-only", not "empty". Phase 2 made the table
	// governable but did not move a single bucket.
	aezGenesis := decodeJSONGenesis[aeztypes.GenesisState](t, genesis[aeztypes.ModuleName])
	require.NoError(t, aezGenesis.Validate())
	require.False(t, aezGenesis.Params.Prototype.Enabled)
	require.True(t, aezGenesis.IsCoreOnly())
	require.Equal(t, uint32(aeztypes.ZoneCount), uint32(len(aezGenesis.Zones)))
	// AEZ Phase 2: the routing table is governance-owned. A keyless
	// authority here would make MsgUpdateRoutingTable unreachable forever --
	// the bug x/nominator-pool shipped and had to patch around in genesis.
	require.Equal(t, aeztypes.GovAuthority(), aezGenesis.Params.Prototype.Authority)
	// x/internal/prototype is not importable from app/, so the keyless
	// sentinel is spelled out. It is the same literal the routing/scheduler
	// assertions in this file already use.
	require.NotEqual(t, keylessPrototypeAuthority, aezGenesis.Params.Prototype.Authority)

	meshGenesis := decodeJSONGenesis[meshkeeper.GenesisState](t, genesis[meshtypes.ModuleName])
	require.False(t, meshGenesis.Params.Enabled)
	require.Empty(t, meshGenesis.State.Destinations)
	require.Empty(t, meshGenesis.State.ReplayMarkers)

	networkingGenesis := decodeJSONGenesis[networkingkeeper.GenesisState](t, genesis[networkingtypes.ModuleName])
	require.False(t, networkingGenesis.Params.Enabled)
	require.NotEmpty(t, networkingGenesis.State.ChannelPolicies)
	require.Empty(t, networkingGenesis.State.NodeRecords)
	require.Empty(t, networkingGenesis.State.Sessions)

	paymentsGenesis := decodeJSONGenesis[paymentskeeper.GenesisState](t, genesis[paymentstypes.ModuleName])
	require.False(t, paymentsGenesis.Params.Enabled)
	require.Empty(t, paymentsGenesis.State.Channels)
	require.Empty(t, paymentsGenesis.State.Settlements)

	schedulerGenesis := decodeJSONGenesis[schedulerkeeper.GenesisState](t, genesis[schedulertypes.ModuleName])
	require.False(t, schedulerGenesis.Params.Enabled)
	require.Empty(t, schedulerGenesis.State.Jobs)
	require.Empty(t, schedulerGenesis.State.History)

	contractsGenesis := decodeJSONGenesis[contractstypes.GenesisState](t, genesis[contractstypes.ModuleName])
	require.NoError(t, contractsGenesis.Validate())
	require.True(t, contractsGenesis.Params.Enabled)
	require.Empty(t, contractsGenesis.State.Codes)
	require.Equal(t, contractskeeper.DefaultGenesis().StateRoot, contractsGenesis.StateRoot)
}

// prototypeModuleWithBeginBlocker is a stand-in for a prototype module that grew
// a block-lifecycle hook. It implements just enough of appmodule.AppModule to be
// swapped into ModuleManager.Modules, which is a map[string]any.
type prototypeModuleWithBeginBlocker struct{}

func (prototypeModuleWithBeginBlocker) IsOnePerModuleType()			{}
func (prototypeModuleWithBeginBlocker) IsAppModule()				{}
func (prototypeModuleWithBeginBlocker) BeginBlock(context.Context) error	{ return nil }

type prototypeModuleWithEndBlocker struct{}

func (prototypeModuleWithEndBlocker) IsOnePerModuleType()		{}
func (prototypeModuleWithEndBlocker) IsAppModule()			{}
func (prototypeModuleWithEndBlocker) EndBlock(context.Context) error	{ return nil }

// TestWiringGateRejectsPrototypeModuleWithBlockLifecycleHook proves the AEZ
// Phase 2 gate check FIRES.
//
// Every prototype module in the tree already lacks a Begin/EndBlocker, so the
// check passes on the real app whether or not it works -- which is exactly the
// shape of a guard that quietly does nothing. Injecting a module that violates
// the rule is the only way to show the gate would actually catch the mistake it
// exists to catch: shipping a BeginBlocker on a prototype module instead of
// promoting it to systemModules first, which is the step AEZ Phase 2 had to
// take for x/aez.
func TestWiringGateRejectsPrototypeModuleWithBlockLifecycleHook(t *testing.T) {
	prototypeNames := AetraCorePrototypeModuleNames()
	require.NotEmpty(t, prototypeNames)
	victim := prototypeNames[0]

	t.Run("BeginBlocker", func(t *testing.T) {
		app, _ := setup(true, 5)
		require.NoError(t, app.ValidateAetraCoreWiringGate(), "fixture: the unmodified app must pass")

		app.ModuleManager.Modules[victim] = prototypeModuleWithBeginBlocker{}
		err := app.ValidateAetraCoreWiringGate()
		require.ErrorContains(t, err, "must not implement BeginBlocker")
		require.ErrorContains(t, err, victim)
	})

	t.Run("EndBlocker", func(t *testing.T) {
		app, _ := setup(true, 5)
		app.ModuleManager.Modules[victim] = prototypeModuleWithEndBlocker{}
		err := app.ValidateAetraCoreWiringGate()
		require.ErrorContains(t, err, "must not implement EndBlocker")
	})

	// The rule is scoped to the prototype family: x/aez is a system module
	// and has a BeginBlocker, and the real app passes the gate. If this rule
	// ever leaked into the system loop, the assertion at the top of the
	// BeginBlocker subtest would have failed.
}

func TestFeatureDisabledMainnetProfileHasNoActiveProductionShardingBehavior(t *testing.T) {
	app := Setup(t, false)

	_, err := app.LoadKeeper.ApplyMetrics(loadtypes.Metrics{CanonicalMempoolSize: 1})
	require.ErrorContains(t, err, "disabled")
	err = app.RoutingKeeper.SetRoutingTable("ae1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqp8e93gq", 1, []routingkeeper.ShardConfig{{ZoneID: routingtypes.ZoneFinancial, ActiveShards: 1}})
	require.ErrorContains(t, err, "disabled")
	// AEZ Phase 2: x/aez DOES have a mutator now -- MsgUpdateRoutingTable --
	// so the old claim here ("no Msg service exists, so nothing can move the
	// table") is dead and is replaced by the assertion it used to stand in
	// for. Unlike the prototype modules above, x/aez's gate is the AUTHORITY,
	// not a feature flag: an unauthorized caller is rejected on the
	// mainnet profile, so the routing table is unreachable without gov.
	aezTable, err := app.AEZKeeper.GetRoutingTable(sdk.UnwrapSDKContext(app.NewUncachedContext(false, cmtproto.Header{Height: 1})))
	require.NoError(t, err)
	_, err = aezkeeper.NewMsgServerImpl(&app.AEZKeeper).UpdateRoutingTable(
		app.NewUncachedContext(false, cmtproto.Header{Height: 1}),
		&aeztypes.MsgUpdateRoutingTable{
			Authority:		keylessPrototypeAuthority,
			Version:		aezTable.Version + 1,
			Epoch:			1,
			ActivationHeight:	int64(aeztypes.DefaultRoutingEpochLength),
			Buckets:		aeztypes.BucketsFromTable(aezTable),
		},
	)
	require.ErrorContains(t, err, "governance authority")

	err = app.MeshKeeper.RegisterDestination(meshtypes.MeshDestination{})
	require.ErrorContains(t, err, "disabled")
	err = app.NetworkingKeeper.RegisterNodeRecord(networkingtypes.NodeRecord{}, nil, 1)
	require.ErrorContains(t, err, "disabled")
	err = app.PaymentsKeeper.OpenChannel(paymentstypes.ChannelRecord{})
	require.ErrorContains(t, err, "disabled")
	err = app.AetraCoreKeeper.RegisterZoneDescriptor(aetracoretypes.ZoneDescriptor{})
	require.ErrorContains(t, err, "disabled")
	err = app.SchedulerKeeper.RegisterScheduledJob(schedulertypes.MsgRegisterScheduledJob{
		Authority:	"ae1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqp8e93gq",
		Job: schedulertypes.ScheduledJob{
			ID:			"disabled",
			OwnerModule:		"aetracore",
			Type:			schedulertypes.JobTypeDelayed,
			NextExecutionHeight:	1,
			MaxGas:			1,
		},
	})
	require.ErrorContains(t, err, "disabled")
}

func TestPrototypeGenesisInitializesRuntimeKeeperState(t *testing.T) {
	app, _ := setup(true, 5)
	genesis := GenesisStateWithSingleValidator(t, app)
	routingGenesis := routingkeeper.DefaultGenesis()
	routingGenesis.Shards = []routingkeeper.ShardConfig{{ZoneID: routingtypes.ZoneFinancial, ActiveShards: 2}}
	rawRoutingGenesis, err := json.Marshal(routingGenesis)
	require.NoError(t, err)
	genesis[routingtypes.ModuleName] = rawRoutingGenesis
	stateBytes, err := json.MarshalIndent(genesis, "", " ")
	require.NoError(t, err)

	_, err = app.InitChain(&abci.RequestInitChain{
		Validators:		[]abci.ValidatorUpdate{},
		ConsensusParams:	sims.DefaultConsensusParams,
		AppStateBytes:		stateBytes,
	})
	require.NoError(t, err)

	shards, _, err := app.RoutingKeeper.Shards(nil)
	require.NoError(t, err)
	require.Equal(t, []routingkeeper.ShardConfig{{ZoneID: routingtypes.ZoneFinancial, ActiveShards: 2}}, shards)
}

func TestAetraCorePrototypeStateSurvivesRestartWhenDisabled(t *testing.T) {
	db := dbm.NewMemDB()
	appOptions := sims.AppOptionsMap{flags.FlagHome: DefaultNodeHome}
	source := NewL1App(log.NewNopLogger(), db, true, appOptions)
	genesis := GenesisStateWithSingleValidator(t, source)
	stateBytes, err := json.MarshalIndent(genesis, "", " ")
	require.NoError(t, err)

	_, err = source.InitChain(&abci.RequestInitChain{
		Validators:		[]abci.ValidatorUpdate{},
		ConsensusParams:	sims.DefaultConsensusParams,
		AppStateBytes:		stateBytes,
	})
	require.NoError(t, err)
	_, err = source.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height:	1,
		Hash:	source.LastCommitID().Hash,
	})
	require.NoError(t, err)
	_, err = source.Commit()
	require.NoError(t, err)

	sourceCtx := source.NewUncachedContext(false, cmtproto.Header{Height: source.LastBlockHeight()})
	sourceLoad, err := source.LoadKeeper.ExportGenesisState(sourceCtx)
	require.NoError(t, err)
	sourceRouting, err := source.RoutingKeeper.ExportGenesisState(sourceCtx)
	require.NoError(t, err)
	sourceAEZ, err := source.AEZKeeper.ExportGenesisState(sourceCtx)
	require.NoError(t, err)
	sourceMesh, err := source.MeshKeeper.ExportGenesisState(sourceCtx)
	require.NoError(t, err)
	sourceNetworking, err := source.NetworkingKeeper.ExportGenesisState(sourceCtx)
	require.NoError(t, err)
	sourcePayments, err := source.PaymentsKeeper.ExportGenesisState(sourceCtx)
	require.NoError(t, err)
	sourceAetraCore, err := source.AetraCoreKeeper.ExportGenesisState(sourceCtx)
	require.NoError(t, err)
	sourceScheduler, err := source.SchedulerKeeper.ExportGenesisState(sourceCtx)
	require.NoError(t, err)

	restarted := NewL1App(log.NewNopLogger(), db, true, appOptions)
	restartedCtx := restarted.NewUncachedContext(false, cmtproto.Header{Height: restarted.LastBlockHeight()})
	restartedLoad, err := restarted.LoadKeeper.ExportGenesisState(restartedCtx)
	require.NoError(t, err)
	restartedRouting, err := restarted.RoutingKeeper.ExportGenesisState(restartedCtx)
	require.NoError(t, err)
	restartedAEZ, err := restarted.AEZKeeper.ExportGenesisState(restartedCtx)
	require.NoError(t, err)
	restartedMesh, err := restarted.MeshKeeper.ExportGenesisState(restartedCtx)
	require.NoError(t, err)
	restartedNetworking, err := restarted.NetworkingKeeper.ExportGenesisState(restartedCtx)
	require.NoError(t, err)
	restartedPayments, err := restarted.PaymentsKeeper.ExportGenesisState(restartedCtx)
	require.NoError(t, err)
	restartedAetraCore, err := restarted.AetraCoreKeeper.ExportGenesisState(restartedCtx)
	require.NoError(t, err)
	restartedScheduler, err := restarted.SchedulerKeeper.ExportGenesisState(restartedCtx)
	require.NoError(t, err)

	require.Equal(t, sourceAetraCore, restartedAetraCore)
	require.Equal(t, sourceLoad, restartedLoad)
	require.Equal(t, sourceRouting, restartedRouting)
	require.Equal(t, sourceAEZ, restartedAEZ)
	require.Equal(t, sourceMesh, restartedMesh)
	require.Equal(t, sourceNetworking, restartedNetworking)
	require.Equal(t, sourcePayments, restartedPayments)
	require.Equal(t, sourceScheduler, restartedScheduler)
}

func decodeJSONGenesis[T any](t *testing.T, raw json.RawMessage) T {
	t.Helper()
	var out T
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}
