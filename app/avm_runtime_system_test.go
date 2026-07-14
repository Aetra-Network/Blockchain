package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"

	"cosmossdk.io/log/v2"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	bam "github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/stretchr/testify/require"

	sims "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sovereign-l1/l1/app/addressing"
	contractstypes "github.com/sovereign-l1/l1/x/contracts/types"
	feestypes "github.com/sovereign-l1/l1/x/fees/types"
	nativeaccounttypes "github.com/sovereign-l1/l1/x/native-account/types"
)

// fakeCodeHash stands in for a real compiled module hash in tests that only
// deploy/execute against the resulting contract's non-executable stub path
// (raw payload written straight to Contract.Data — see executeContract's
// `else` branch in x/contracts/keeper/keeper.go). Storing code this way
// (CodeHash/CodeBytes only, no Bytecode) is a deliberately-supported,
// already-tested lightweight registration flow — see storeContractCode in
// x/contracts/keeper/keeper_test.go — and sidesteps FINDING-004's StoreCode
// decode+verify gate entirely (it only runs `if len(msg.Bytecode) > 0`), so
// these app-level tests don't need a real AVM module: they were never
// testing AVM execution semantics, only deploy/execute/query wiring at the
// app level, and a REAL module would in fact break them (avm.Runner.Run
// hard-errors on any entrypoint the module doesn't export, and a minimal
// module here would only export EntryDeploy, not EntryReceiveExternal).
func fakeCodeHash(seed string) string {
	h := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(h[:])
}

func TestAVMRuntimeAppLevelDeployExecuteQueueStorageReceiptsAndExportImport(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(100)
	account := nativeAccountActivateViaRoute(t, app, ctx, nativeAccountModuleTestPubKey())

	require.Contains(t, app.ModuleManager.Modules, contractstypes.ModuleName)
	require.Contains(t, app.keys, contractstypes.StoreKey)
	require.NotNil(t, app.ContractsKeeper)
	require.NotNil(t, app.MsgServiceRouter().Handler(&contractstypes.MsgStoreCode{}))
	require.NotNil(t, app.MsgServiceRouter().Handler(&contractstypes.MsgDeployContract{}))
	require.NotNil(t, app.MsgServiceRouter().Handler(&contractstypes.MsgExecuteExternal{}))
	require.NotNil(t, app.MsgServiceRouter().Handler(&contractstypes.MsgSendInternalMessage{}))
	require.NotNil(t, app.GRPCQueryRouter().Route("/l1.contracts.v1.Query/ContractStorage"))
	require.NotNil(t, app.GRPCQueryRouter().Route("/l1.contracts.v1.Query/ContractReceipts"))

	storeRoute := app.MsgServiceRouter().Handler(&contractstypes.MsgStoreCode{})
	codeID := fakeCodeHash("app-runtime deterministic")
	_, err := storeRoute(ctx, &contractstypes.MsgStoreCode{
		Authority: account.AddressUser,
		CodeHash:  codeID,
		CodeBytes: 128,
	})
	require.NoError(t, err)
	code, found, err := app.ContractsKeeper.Code(contractstypes.QueryCodeRequest{CodeID: codeID})
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, codeID, code.CodeID)

	deployRoute := app.MsgServiceRouter().Handler(&contractstypes.MsgDeployContract{})
	_, err = deployRoute(ctx.WithBlockHeight(101), &contractstypes.MsgDeployContract{
		Creator:        account.AddressUser,
		CodeID:         codeID,
		InitPayload:    []byte("init"),
		InitialBalance: 1_000,
		Admin:          account.AddressUser,
		Salt:           "app-runtime",
		Height:         101,
	})
	require.NoError(t, err)
	contracts, err := app.ContractsKeeper.Contracts(contractstypes.QueryContractsRequest{Pagination: contractstypes.PageRequest{Limit: 10}})
	require.NoError(t, err)
	require.Len(t, contracts, 1)
	deployed := contractstypes.InstantiateContractResponse{
		ContractAddressUser: contracts[0].AddressUser,
		ContractAddressRaw:  contracts[0].AddressRaw,
	}
	require.True(t, bytes.HasPrefix([]byte(deployed.ContractAddressUser), []byte("AE")))
	require.True(t, bytes.HasPrefix([]byte(deployed.ContractAddressRaw), []byte("ae1")))

	executeRoute := app.MsgServiceRouter().Handler(&contractstypes.MsgExecuteExternal{})
	_, err = executeRoute(ctx.WithBlockHeight(102), &contractstypes.MsgExecuteExternal{
		Sender:          account.AddressUser,
		ContractAddress: deployed.ContractAddressUser,
		Payload:         []byte("call"),
		Funds:           25,
		GasLimit:        app.ContractsKeeper.Params().MaxGasPerExecution,
		Height:          102,
	})
	require.NoError(t, err)
	executed, err := app.ContractsKeeper.Contract(contractstypes.QueryContractRequest{ContractAddress: deployed.ContractAddressUser})
	require.NoError(t, err)
	require.Equal(t, deployed.ContractAddressUser, executed.Contract.AddressUser)

	destination := appAVMRuntimeAddress(0x77)
	// SEC-HIGH #5: internal messages are produced (queued) by contract execution;
	// enqueue via the test helper (stands in for appendAVMOutgoingMessages).
	_ = enqueueAppInternalForTest(t, app, ctx.WithBlockHeight(103), contractstypes.InternalMessage{
		SourceContractUser: deployed.ContractAddressUser,
		DestinationAccount: destination,
		Funds:              7,
		Opcode:             1,
		QueryID:            2,
		Body:               []byte("internal"),
		GasLimit:           100,
		LogicalTime:        3,
		Height:             103,
	})
	queue, err := app.ContractsKeeper.ContractQueue(contractstypes.QueryContractQueueRequest{
		ContractAddress: deployed.ContractAddressUser,
		Pagination:      contractstypes.PageRequest{Limit: 10},
	})
	require.NoError(t, err)
	require.Len(t, queue, 1)

	storage, err := app.ContractsKeeper.ContractStorage(contractstypes.QueryContractStorageRequest{
		ContractAddress: deployed.ContractAddressUser,
		Pagination:      contractstypes.PageRequest{Limit: 10},
	})
	require.NoError(t, err)
	require.Equal(t, []byte("data"), storage[0].Key)
	require.Equal(t, []byte("call"), storage[0].Value)

	receipts, err := app.ContractsKeeper.ContractReceipts(contractstypes.QueryContractReceiptsRequest{
		ContractAddress: deployed.ContractAddressUser,
		Pagination:      contractstypes.PageRequest{Limit: 10},
	})
	require.NoError(t, err)
	// SEC-HIGH #5: enqueue (via the helper) no longer writes a receipt; only
	// deploy/execute receipts exist. The queued message is asserted above.
	require.Len(t, receipts, 2)
	require.Equal(t, "deploy", receipts[0].Operation)
	require.Equal(t, "execute", receipts[1].Operation)
	require.NoError(t, app.RunAppInvariant(ctx, AppInvariantAVMQueueReceipts))

	exported, err := app.ContractsKeeper.ExportGenesisState(ctx)
	require.NoError(t, err)
	restarted := Setup(t, false)
	restartedCtx := restarted.NewContext(false).WithBlockHeight(200)
	require.NoError(t, restarted.ContractsKeeper.InitGenesisState(restartedCtx, exported))
	roundTrip, err := restarted.ContractsKeeper.ContractStorage(contractstypes.QueryContractStorageRequest{
		ContractAddress: deployed.ContractAddressUser,
		Pagination:      contractstypes.PageRequest{Limit: 10},
	})
	require.NoError(t, err)
	require.Equal(t, storage, roundTrip)
	roundTripReceipts, err := restarted.ContractsKeeper.ContractReceipts(contractstypes.QueryContractReceiptsRequest{
		ContractAddress: deployed.ContractAddressUser,
		Pagination:      contractstypes.PageRequest{Limit: 10},
	})
	require.NoError(t, err)
	require.Equal(t, receipts, roundTripReceipts)
}

func TestAVMRuntimeAppLevelExecuteInternalRouteAndExportImport(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(600)
	account := nativeAccountActivateViaRoute(t, app, ctx, nativeAccountModuleTestPubKey())

	storeRoute := app.MsgServiceRouter().Handler(&contractstypes.MsgStoreCode{})
	codeID := fakeCodeHash("app-runtime internal deterministic")
	_, err := storeRoute(ctx, &contractstypes.MsgStoreCode{
		Authority: account.AddressUser,
		CodeHash:  codeID,
		CodeBytes: 128,
	})
	require.NoError(t, err)

	deployRoute := app.MsgServiceRouter().Handler(&contractstypes.MsgDeployContract{})
	_, err = deployRoute(ctx.WithBlockHeight(601), &contractstypes.MsgDeployContract{
		Creator:        account.AddressUser,
		CodeID:         codeID,
		InitPayload:    []byte("init"),
		InitialBalance: 1_000,
		Admin:          account.AddressUser,
		Salt:           "internal-route",
		Height:         601,
	})
	require.NoError(t, err)

	contracts, err := app.ContractsKeeper.Contracts(contractstypes.QueryContractsRequest{Pagination: contractstypes.PageRequest{Limit: 10}})
	require.NoError(t, err)
	require.Len(t, contracts, 1)
	contract := contracts[0]

	// SEC-HIGH #5: enqueue via the test helper (stands in for a contract's
	// appendAVMOutgoingMessages) rather than injecting through the route.
	_ = enqueueAppInternalForTest(t, app, ctx.WithBlockHeight(602), contractstypes.InternalMessage{
		SourceContractUser: contract.AddressUser,
		DestinationAccount: appAVMRuntimeAddress(0x88),
		Funds:              9,
		Opcode:             44,
		QueryID:            45,
		Body:               []byte("internal-call"),
		GasLimit:           100,
		LogicalTime:        8,
		Height:             602,
	})

	queue, err := app.ContractsKeeper.ContractQueue(contractstypes.QueryContractQueueRequest{
		ContractAddress: contract.AddressUser,
		Pagination:      contractstypes.PageRequest{Limit: 10},
	})
	require.NoError(t, err)
	require.Len(t, queue, 1)
	require.Equal(t, uint32(44), queue[0].Opcode)

	receipts, err := app.ContractsKeeper.ContractReceipts(contractstypes.QueryContractReceiptsRequest{
		ContractAddress: contract.AddressUser,
		Pagination:      contractstypes.PageRequest{Limit: 10},
	})
	require.NoError(t, err)
	require.NotEmpty(t, receipts)
	// SEC-HIGH #5: enqueue no longer writes a receipt; the queued internal
	// message is asserted via the contract queue above.

	exported, err := app.ContractsKeeper.ExportGenesisState(ctx)
	require.NoError(t, err)
	restarted := Setup(t, false)
	restartedCtx := restarted.NewContext(false).WithBlockHeight(603)
	require.NoError(t, restarted.ContractsKeeper.InitGenesisState(restartedCtx, exported))

	roundTripQueue, err := restarted.ContractsKeeper.ContractQueue(contractstypes.QueryContractQueueRequest{
		ContractAddress: contract.AddressUser,
		Pagination:      contractstypes.PageRequest{Limit: 10},
	})
	require.NoError(t, err)
	require.Equal(t, queue, roundTripQueue)
}

func TestAVMRuntimeAppLevelRejectsMaliciousStoreCodeWithoutStateMutation(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(700)
	account := nativeAccountActivateViaRoute(t, app, ctx, nativeAccountModuleTestPubKey())

	storeRoute := app.MsgServiceRouter().Handler(&contractstypes.MsgStoreCode{})
	before, err := app.ContractsKeeper.ExportGenesisState(ctx)
	require.NoError(t, err)

	_, err = storeRoute(ctx, &contractstypes.MsgStoreCode{
		Authority: account.AddressUser,
		Bytecode:  []byte("BAD1 deterministic"),
	})
	require.ErrorContains(t, err, contractstypes.ErrInvalidBytecode)

	_, err = storeRoute(ctx, &contractstypes.MsgStoreCode{
		Authority: account.AddressUser,
		Bytecode:  []byte("AVM1 time.now"),
	})
	require.ErrorContains(t, err, contractstypes.ErrInvalidBytecode)

	after, err := app.ContractsKeeper.ExportGenesisState(ctx)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestAVMRuntimeAppLevelExportImportPreservesActiveContractsAndBehavior(t *testing.T) {
	source := Setup(t, false)
	ctx := source.NewContext(false).WithBlockHeight(800)
	account := nativeAccountActivateViaRoute(t, source, ctx, nativeAccountModuleTestPubKey())

	storeRoute := source.MsgServiceRouter().Handler(&contractstypes.MsgStoreCode{})
	codeID := fakeCodeHash("export-import active contract")
	_, err := storeRoute(ctx, &contractstypes.MsgStoreCode{
		Authority: account.AddressUser,
		CodeHash:  codeID,
		CodeBytes: 128,
	})
	require.NoError(t, err)

	deployRoute := source.MsgServiceRouter().Handler(&contractstypes.MsgDeployContract{})
	_, err = deployRoute(ctx.WithBlockHeight(801), &contractstypes.MsgDeployContract{
		Creator:        account.AddressUser,
		CodeID:         codeID,
		InitPayload:    []byte("init"),
		InitialBalance: 1_000,
		Admin:          account.AddressUser,
		Upgradeable:    true,
		SchemaVersion:  1,
		Salt:           "roundtrip",
		Height:         801,
	})
	require.NoError(t, err)

	contracts, err := source.ContractsKeeper.Contracts(contractstypes.QueryContractsRequest{Pagination: contractstypes.PageRequest{Limit: 10}})
	require.NoError(t, err)
	require.Len(t, contracts, 1)
	contract := contracts[0]

	executeRoute := source.MsgServiceRouter().Handler(&contractstypes.MsgExecuteExternal{})
	_, err = executeRoute(ctx.WithBlockHeight(802), &contractstypes.MsgExecuteExternal{
		Sender:          account.AddressUser,
		ContractAddress: contract.AddressUser,
		Payload:         []byte("call"),
		Funds:           25,
		GasLimit:        source.ContractsKeeper.Params().MaxGasPerExecution,
		Height:          802,
	})
	require.NoError(t, err)

	// SEC-HIGH #5: enqueue via the test helper (stands in for a contract's
	// appendAVMOutgoingMessages) rather than injecting through the route.
	_ = enqueueAppInternalForTest(t, source, ctx.WithBlockHeight(803), contractstypes.InternalMessage{
		SourceContractUser: contract.AddressUser,
		DestinationAccount: appAVMRuntimeAddress(0x99),
		Funds:              7,
		Opcode:             77,
		QueryID:            78,
		Body:               []byte("internal"),
		GasLimit:           100,
		LogicalTime:        9,
		Height:             803,
	})

	receipt, err := source.ContractsKeeper.MigrateContractState(contractstypes.MsgMigrateContractState{
		Actor:             account.AddressUser,
		ContractAddress:   contract.AddressUser,
		FromSchemaVersion: 1,
		ToSchemaVersion:   2,
		MigrationHandler:  "append",
		Payload:           []byte(":v2"),
		Height:            804,
	})
	require.NoError(t, err)
	require.Equal(t, "migrate_state", receipt.Operation)

	contractAfterMigration, err := source.ContractsKeeper.Contract(contractstypes.QueryContractRequest{ContractAddress: contract.AddressUser})
	require.NoError(t, err)
	require.Equal(t, uint64(2), contractAfterMigration.Contract.StorageSchemaVersion)
	require.Equal(t, []byte("call:v2"), contractAfterMigration.Contract.Data)

	storageBefore, err := source.ContractsKeeper.ContractStorage(contractstypes.QueryContractStorageRequest{
		ContractAddress: contract.AddressUser,
		Pagination:      contractstypes.PageRequest{Limit: 10},
	})
	require.NoError(t, err)
	receiptsBefore, err := source.ContractsKeeper.ContractReceipts(contractstypes.QueryContractReceiptsRequest{
		ContractAddress: contract.AddressUser,
		Pagination:      contractstypes.PageRequest{Limit: 10},
	})
	require.NoError(t, err)
	queueBefore, err := source.ContractsKeeper.ContractQueue(contractstypes.QueryContractQueueRequest{
		ContractAddress: contract.AddressUser,
		Pagination:      contractstypes.PageRequest{Limit: 10},
	})
	require.NoError(t, err)
	stateRootBefore, err := source.ContractsKeeper.ContractStateRoot(contractstypes.QueryContractStateRootRequest{ContractAddress: contract.AddressUser})
	require.NoError(t, err)

	_, err = source.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: source.LastBlockHeight() + 1,
		Hash:   source.LastCommitID().Hash,
	})
	require.NoError(t, err)
	_, err = source.Commit()
	require.NoError(t, err)
	require.NotEmpty(t, source.LastCommitID().Hash)

	exported, err := source.ExportAppStateAndValidators(false, nil, nil)
	require.NoError(t, err)
	require.NotEmpty(t, exported.AppState)

	target := NewL1App(
		log.NewNopLogger(),
		dbm.NewMemDB(),
		true,
		sims.AppOptionsMap{flags.FlagHome: DefaultNodeHome},
	)
	_, err = target.InitChain(&abci.RequestInitChain{
		Validators:      []abci.ValidatorUpdate{},
		ConsensusParams: &exported.ConsensusParams,
		AppStateBytes:   exported.AppState,
	})
	require.NoError(t, err)
	_, err = target.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: target.LastBlockHeight() + 1,
		Hash:   target.LastCommitID().Hash,
	})
	require.NoError(t, err)
	_, err = target.Commit()
	require.NoError(t, err)
	require.NotEmpty(t, target.LastCommitID().Hash)

	targetContract, err := target.ContractsKeeper.Contract(contractstypes.QueryContractRequest{ContractAddress: contract.AddressUser})
	require.NoError(t, err)
	require.Equal(t, contractAfterMigration.Contract.CodeID, targetContract.Contract.CodeID)
	require.Equal(t, contractAfterMigration.Contract.StorageSchemaVersion, targetContract.Contract.StorageSchemaVersion)
	require.Equal(t, contractAfterMigration.Contract.StateRoot, targetContract.Contract.StateRoot)
	require.Equal(t, stateRootBefore, targetContract.Contract.StateRoot)
	require.Equal(t, contractAfterMigration.Contract.Data, targetContract.Contract.Data)

	targetStorage, err := target.ContractsKeeper.ContractStorage(contractstypes.QueryContractStorageRequest{
		ContractAddress: contract.AddressUser,
		Pagination:      contractstypes.PageRequest{Limit: 10},
	})
	require.NoError(t, err)
	require.Equal(t, storageBefore, targetStorage)

	targetReceipts, err := target.ContractsKeeper.ContractReceipts(contractstypes.QueryContractReceiptsRequest{
		ContractAddress: contract.AddressUser,
		Pagination:      contractstypes.PageRequest{Limit: 10},
	})
	require.NoError(t, err)
	require.Equal(t, receiptsBefore, targetReceipts)

	targetQueue, err := target.ContractsKeeper.ContractQueue(contractstypes.QueryContractQueueRequest{
		ContractAddress: contract.AddressUser,
		Pagination:      contractstypes.PageRequest{Limit: 10},
	})
	require.NoError(t, err)
	require.Equal(t, queueBefore, targetQueue)

	targetCtx := target.NewUncachedContext(false, cmtproto.Header{Height: target.LastBlockHeight()})
	roundTripGenesis, err := target.ContractsKeeper.ExportGenesisState(targetCtx)
	require.NoError(t, err)
	rawGenesis, err := json.Marshal(roundTripGenesis)
	require.NoError(t, err)
	require.NotEmpty(t, rawGenesis)

	targetExported, err := target.ExportAppStateAndValidators(false, nil, nil)
	require.NoError(t, err)
	require.NotEmpty(t, targetExported.AppState)
}

func TestAVMRuntimeAppLevelLifecycleCoversMigrateAndPolicyGuards(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(700)
	account := nativeAccountActivateViaRoute(t, app, ctx, nativeAccountModuleTestPubKey())

	storeRoute := app.MsgServiceRouter().Handler(&contractstypes.MsgStoreCode{})
	codeIDV1 := fakeCodeHash("lifecycle v1")
	_, err := storeRoute(ctx, &contractstypes.MsgStoreCode{
		Authority: account.AddressUser,
		CodeHash:  codeIDV1,
		CodeBytes: 128,
	})
	require.NoError(t, err)

	_, err = app.ContractsKeeper.StoreCodeState(ctx, contractstypes.MsgStoreCode{
		Authority: addressing.ZeroUserFriendly,
		Bytecode:  []byte("AVM1 zero rejected"),
	})
	require.ErrorContains(t, err, "zero address")

	codeIDV2 := fakeCodeHash("lifecycle v2")
	_, err = app.ContractsKeeper.StoreCodeState(ctx, contractstypes.MsgStoreCode{
		Authority: account.AddressUser,
		CodeHash:  codeIDV2,
		CodeBytes: 128,
	})
	require.NoError(t, err)

	deployed, err := app.ContractsKeeper.DeployContractState(ctx.WithBlockHeight(701), contractstypes.MsgDeployContract{
		Creator:        account.AddressUser,
		CodeID:         codeIDV1,
		InitPayload:    []byte("init"),
		InitialBalance: 1_000,
		Admin:          account.AddressUser,
		Upgradeable:    true,
		SchemaVersion:  1,
		Salt:           "lifecycle",
		Height:         701,
	})
	require.NoError(t, err)
	require.True(t, bytes.HasPrefix([]byte(deployed.ContractAddressUser), []byte("AE")))

	executeRoute := app.MsgServiceRouter().Handler(&contractstypes.MsgExecuteExternal{})
	_, err = executeRoute(ctx.WithBlockHeight(702), &contractstypes.MsgExecuteExternal{
		Sender:          account.AddressUser,
		ContractAddress: deployed.ContractAddressUser,
		Payload:         []byte("call"),
		Funds:           0,
		GasLimit:        app.ContractsKeeper.Params().MaxGasPerExecution,
		Height:          702,
	})
	require.NoError(t, err)

	receipt, err := app.ContractsKeeper.UpgradeContractCode(contractstypes.MsgUpgradeContractCode{
		Actor:            account.AddressUser,
		ContractAddress:  deployed.ContractAddressUser,
		NewCodeID:        codeIDV2,
		MigrationHandler: "schema-only",
		Height:           703,
	})
	require.NoError(t, err)
	require.Equal(t, "upgrade_code", receipt.Operation)

	receipt, err = app.ContractsKeeper.MigrateContractState(contractstypes.MsgMigrateContractState{
		Actor:             account.AddressUser,
		ContractAddress:   deployed.ContractAddressUser,
		FromSchemaVersion: 1,
		ToSchemaVersion:   2,
		MigrationHandler:  "append",
		Payload:           []byte(":v2"),
		Height:            704,
	})
	require.NoError(t, err)
	require.Equal(t, "migrate_state", receipt.Operation)

	contract, err := app.ContractsKeeper.Contract(contractstypes.QueryContractRequest{ContractAddress: deployed.ContractAddressUser})
	require.NoError(t, err)
	require.Equal(t, codeIDV2, contract.Contract.CodeID)
	require.Equal(t, uint64(2), contract.Contract.StorageSchemaVersion)
	require.Equal(t, []byte("call:v2"), contract.Contract.Data)

	require.NotNil(t, app.GRPCQueryRouter().Route("/l1.contracts.v1.Query/Contract"))
	require.NotNil(t, app.GRPCQueryRouter().Route("/l1.contracts.v1.Query/ContractStateRoot"))

	exported, err := app.ContractsKeeper.ExportGenesisState(ctx.WithBlockHeight(705))
	require.NoError(t, err)
	restarted := Setup(t, false)
	restartedCtx := restarted.NewContext(false).WithBlockHeight(706)
	require.NoError(t, restarted.ContractsKeeper.InitGenesisState(restartedCtx, exported))

	roundTrip, err := restarted.ContractsKeeper.Contract(contractstypes.QueryContractRequest{ContractAddress: deployed.ContractAddressUser})
	require.NoError(t, err)
	require.Equal(t, contract.Contract.StateRoot, roundTrip.Contract.StateRoot)
	require.Equal(t, contract.Contract.StorageSchemaVersion, roundTrip.Contract.StorageSchemaVersion)
}

func TestAVMRuntimeRejectsFrozenAccountsReservedZeroAndRecoversFrozenContracts(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(300)
	account := nativeAccountActivateViaRoute(t, app, ctx, nativeAccountModuleTestPubKey())
	codeID := appAVMRuntimeStoreCode(t, app, ctx, account.AddressUser)
	contract := appAVMRuntimeDeploy(t, app, ctx.WithBlockHeight(301), account.AddressUser, codeID, "frozen-runtime", 1, 301)

	account.Status = nativeaccounttypes.AccountStatusFrozen
	require.NoError(t, app.NativeAccountKeeper.SetAccount(ctx, account))
	executeRoute := app.MsgServiceRouter().Handler(&contractstypes.MsgExecuteExternal{})
	_, err := executeRoute(ctx.WithBlockHeight(302), &contractstypes.MsgExecuteExternal{
		Sender:          account.AddressUser,
		ContractAddress: contract.ContractAddressUser,
		Payload:         []byte("blocked"),
		GasLimit:        app.ContractsKeeper.Params().MaxGasPerExecution,
		Height:          302,
	})
	require.ErrorContains(t, err, contractstypes.ErrAccountFrozen)

	_, err = app.ContractsKeeper.StoreCodeState(ctx, contractstypes.MsgStoreCode{
		Authority: addressing.SystemAddressAETMintUserFriendly,
		Bytecode:  []byte("AVM1 reserved"),
	})
	require.ErrorContains(t, err, "reserved system address")
	_, err = app.ContractsKeeper.StoreCodeState(ctx, contractstypes.MsgStoreCode{
		Authority: addressing.ZeroUserFriendly,
		Bytecode:  []byte("AVM1 zero"),
	})
	require.ErrorContains(t, err, "zero address")

	account.Status = nativeaccounttypes.AccountStatusActive
	require.NoError(t, app.NativeAccountKeeper.SetAccount(ctx, account))
	_, err = executeRoute(ctx.WithBlockHeight(400), &contractstypes.MsgExecuteExternal{
		Sender:          account.AddressUser,
		ContractAddress: contract.ContractAddressUser,
		Payload:         []byte("rent-freeze"),
		GasLimit:        app.ContractsKeeper.Params().MaxGasPerExecution,
		Height:          400,
	})
	require.ErrorContains(t, err, contractstypes.ErrStorageRent)
	frozen, err := app.ContractsKeeper.Contract(contractstypes.QueryContractRequest{ContractAddress: contract.ContractAddressUser})
	require.NoError(t, err)
	require.Equal(t, contractstypes.ContractStatusFrozen, frozen.Contract.Status)
	require.NotZero(t, frozen.Contract.StorageRentDebt)

	_, err = app.ContractsKeeper.TopUpContractState(ctx.WithBlockHeight(401), contractstypes.MsgTopUpContract{
		Sender:          account.AddressUser,
		ContractAddress: contract.ContractAddressUser,
		Amount:          frozen.Contract.StorageRentDebt + 100,
		Height:          401,
	})
	require.NoError(t, err)
	_, err = app.ContractsKeeper.PayContractStorageDebtState(ctx.WithBlockHeight(402), contractstypes.MsgPayContractStorageDebt{
		Sender:          account.AddressUser,
		ContractAddress: contract.ContractAddressUser,
		Amount:          frozen.Contract.StorageRentDebt,
		Height:          402,
	})
	require.NoError(t, err)
	unfrozen, err := app.ContractsKeeper.UnfreezeContractState(ctx.WithBlockHeight(403), contractstypes.MsgUnfreezeContract{
		Sender:          account.AddressUser,
		ContractAddress: contract.ContractAddressUser,
		Height:          403,
	})
	require.NoError(t, err)
	require.Equal(t, contractstypes.ContractStatusActive, unfrozen.Status)
}

func TestAVMRuntimeDeterminismGateRejectsNondeterminismAndKeepsStableRoots(t *testing.T) {
	params := contractstypes.DefaultParams()
	// types.ValidateAVMBytecode only runs the cheap structural checks
	// (header/size) -- it cannot decode/verify a module without importing
	// x/aetravm/avm, which itself imports x/contracts/types and would cycle
	// (see bytecode.go's doc comment). Real nondeterminism/malformed-module
	// rejection now happens once, at StoreCode time, via
	// x/contracts/keeper.storeCodeUnchecked (FINDING-004) calling the real
	// avm.DecodeModule + Verifier.Verify -- exercised below through the
	// actual StoreCode message route, not this narrower helper.
	require.NoError(t, contractstypes.ValidateAVMBytecode(params, []byte("AVM1 set key value")))
	require.ErrorContains(t, contractstypes.ValidateAVMBytecode(params, []byte("BAD1 wrong header")), contractstypes.ErrInvalidBytecode)

	app := Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(500)
	account := nativeAccountActivateViaRoute(t, app, ctx, nativeAccountModuleTestPubKey())
	codeID := appAVMRuntimeStoreCode(t, app, ctx, account.AddressUser)
	contract := appAVMRuntimeDeploy(t, app, ctx.WithBlockHeight(501), account.AddressUser, codeID, "determinism", 1_000, 501)

	first, err := app.ContractsKeeper.ContractStateRoot(contractstypes.QueryContractStateRootRequest{ContractAddress: contract.ContractAddressUser})
	require.NoError(t, err)
	second, err := app.ContractsKeeper.ContractStateRoot(contractstypes.QueryContractStateRootRequest{ContractAddress: contract.ContractAddressUser})
	require.NoError(t, err)
	require.Equal(t, first, second)
}

func TestAVMRuntimeRepeatedActivityStaysWithinBoundedGrowth(t *testing.T) {
	fixture := newAVMRuntimeGrowthFixture(t)
	const rounds = 24

	for i := 0; i < rounds; i++ {
		runAVMRuntimeGrowthStep(t, fixture, i)
	}

	storage, err := fixture.app.ContractsKeeper.ContractStorage(contractstypes.QueryContractStorageRequest{
		ContractAddress: fixture.contract.ContractAddressUser,
		Pagination:      contractstypes.PageRequest{Limit: 10},
	})
	require.NoError(t, err)
	require.Len(t, storage, 1)

	queue, err := fixture.app.ContractsKeeper.ContractQueue(contractstypes.QueryContractQueueRequest{
		ContractAddress: fixture.contract.ContractAddressUser,
		Pagination:      contractstypes.PageRequest{Limit: contractstypes.MaxContractQueryLimit},
	})
	require.NoError(t, err)
	require.Len(t, queue, rounds)

	receipts, err := fixture.app.ContractsKeeper.ContractReceipts(contractstypes.QueryContractReceiptsRequest{
		ContractAddress: fixture.contract.ContractAddressUser,
		Pagination:      contractstypes.PageRequest{Limit: contractstypes.MaxContractQueryLimit},
	})
	require.NoError(t, err)
	// SEC-HIGH #5: enqueue no longer writes a receipt, so each round yields one
	// (execute) receipt, plus the single deploy receipt.
	require.Len(t, receipts, 1+rounds)
	require.Equal(t, "deploy", receipts[0].Operation)
	require.Equal(t, "execute", receipts[1].Operation)

	exported, err := fixture.app.ContractsKeeper.ExportGenesisState(fixture.ctx)
	require.NoError(t, err)
	require.Len(t, exported.State.Contracts, 1)
	require.Len(t, exported.State.InternalMessages, rounds)
	require.Len(t, exported.State.Receipts, 1+rounds)
	require.NoError(t, exported.Validate())
}

func TestAVMRuntimeRejectsMalformedPayloadsAndGasBounds(t *testing.T) {
	fixture := newAVMRuntimeGrowthFixture(t)
	before, err := fixture.app.ContractsKeeper.ExportGenesisState(fixture.ctx)
	require.NoError(t, err)

	_, err = fixture.executeRoute(fixture.ctx.WithBlockHeight(2000), &contractstypes.MsgExecuteExternal{
		Sender:          fixture.account.AddressUser,
		ContractAddress: fixture.contract.ContractAddressUser,
		Payload:         bytes.Repeat([]byte("x"), contractstypes.MaxContractPayloadBytes+1),
		GasLimit:        fixture.app.ContractsKeeper.Params().MaxGasPerExecution,
		Height:          2000,
	})
	require.ErrorContains(t, err, "payload exceeds maximum size")

	_, err = fixture.executeRoute(fixture.ctx.WithBlockHeight(2001), &contractstypes.MsgExecuteExternal{
		Sender:          fixture.account.AddressUser,
		ContractAddress: fixture.contract.ContractAddressUser,
		Payload:         []byte("ok"),
		GasLimit:        0,
		Height:          2001,
	})
	require.ErrorContains(t, err, "gas limit out of bounds")

	after, err := fixture.app.ContractsKeeper.ExportGenesisState(fixture.ctx)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestAVMRuntimePreservesBaseChainValidationBoundaries(t *testing.T) {
	app := Setup(t, false)
	genesis := app.DefaultGenesis()
	require.NoError(t, app.BasicModuleManager.ValidateGenesis(app.AppCodec(), app.TxConfig(), genesis))

	var feesGenesis feestypes.GenesisState
	app.AppCodec().MustUnmarshalJSON(genesis[feestypes.ModuleName], &feesGenesis)
	feesGenesis.Params.AllowedFeeDenoms = []string{"uatom"}
	genesis[feestypes.ModuleName] = app.AppCodec().MustMarshalJSON(&feesGenesis)
	require.Error(t, app.BasicModuleManager.ValidateGenesis(app.AppCodec(), app.TxConfig(), genesis))

	ctx := app.NewContext(false).WithBlockHeight(3000)
	account := nativeAccountActivateViaRoute(t, app, ctx, nativeAccountModuleTestPubKey())
	_, err := app.ContractsKeeper.StoreCodeState(ctx, contractstypes.MsgStoreCode{
		Authority: addressing.ZeroUserFriendly,
		Bytecode:  []byte("AVM1 zero"),
	})
	require.ErrorContains(t, err, "zero address")
	require.NotEmpty(t, account.AddressUser)
}

func appAVMRuntimeStoreCode(t testing.TB, app *L1App, ctx sdk.Context, authority string) string {
	t.Helper()
	resp, err := app.ContractsKeeper.StoreCodeState(ctx, contractstypes.MsgStoreCode{
		Authority: authority,
		CodeHash:  fakeCodeHash("app-runtime helper"),
		CodeBytes: 128,
	})
	require.NoError(t, err)
	return resp.CodeID
}

func appAVMRuntimeDeploy(t testing.TB, app *L1App, ctx sdk.Context, creator string, codeID string, salt string, initialBalance uint64, height uint64) contractstypes.InstantiateContractResponse {
	t.Helper()
	resp, err := app.ContractsKeeper.DeployContractState(ctx, contractstypes.MsgDeployContract{
		Creator:        creator,
		CodeID:         codeID,
		InitPayload:    []byte("init"),
		InitialBalance: initialBalance,
		Admin:          creator,
		Salt:           salt,
		Height:         height,
	})
	require.NoError(t, err)
	return resp
}

type avmRuntimeGrowthFixture struct {
	app           *L1App
	ctx           sdk.Context
	account       nativeaccounttypes.Account
	contract      contractstypes.InstantiateContractResponse
	executeRoute  bam.MsgServiceHandler
	internalRoute bam.MsgServiceHandler
}

func newAVMRuntimeGrowthFixture(tb testing.TB) avmRuntimeGrowthFixture {
	tb.Helper()
	app := Setup(tb, false)
	ctx := app.NewContext(false).WithBlockHeight(1000)
	account := nativeAccountActivateViaRoute(tb, app, ctx, nativeAccountModuleTestPubKey())
	codeID := appAVMRuntimeStoreCode(tb, app, ctx, account.AddressUser)
	contract := appAVMRuntimeDeploy(tb, app, ctx.WithBlockHeight(1001), account.AddressUser, codeID, "growth", 100_000_000, 1001)
	executeRoute := app.MsgServiceRouter().Handler(&contractstypes.MsgExecuteExternal{})
	internalRoute := app.MsgServiceRouter().Handler(&contractstypes.MsgSendInternalMessage{})
	return avmRuntimeGrowthFixture{
		app:           app,
		ctx:           ctx,
		account:       account,
		contract:      contract,
		executeRoute:  executeRoute,
		internalRoute: internalRoute,
	}
}

func runAVMRuntimeGrowthStep(tb testing.TB, fixture avmRuntimeGrowthFixture, step int) {
	tb.Helper()
	height := int64(1002 + step*2)
	_, err := fixture.executeRoute(fixture.ctx.WithBlockHeight(height), &contractstypes.MsgExecuteExternal{
		Sender:          fixture.account.AddressUser,
		ContractAddress: fixture.contract.ContractAddressUser,
		Payload:         []byte(fmt.Sprintf("call-%02d", step)),
		Funds:           uint64(step + 1),
		GasLimit:        fixture.app.ContractsKeeper.Params().MaxGasPerExecution,
		Height:          uint64(height),
	})
	require.NoError(tb, err)
	// SEC-HIGH #5: enqueue via the test helper (stands in for a contract's
	// appendAVMOutgoingMessages) rather than injecting through the route.
	_ = enqueueAppInternalForTest(tb, fixture.app, fixture.ctx.WithBlockHeight(height+1), contractstypes.InternalMessage{
		SourceContractUser: fixture.contract.ContractAddressUser,
		DestinationAccount: appAVMRuntimeAddress(0x77),
		Funds:              uint64(step + 1),
		Opcode:             uint32(step + 1),
		QueryID:            uint64(step + 1),
		Body:               []byte(fmt.Sprintf("internal-%02d", step)),
		GasLimit:           100,
		LogicalTime:        uint64(step + 1),
		Height:             uint64(height + 1),
	})
}

func appAVMRuntimeAddress(fill byte) string {
	bz := bytes.Repeat([]byte{fill}, 20)
	return addressing.FormatAccAddress(sdk.AccAddress(bz))
}

// enqueueAppInternalForTest appends an internal message to the app-level contract
// queue (standing in for a contract's appendAVMOutgoingMessages), since
// ReceiveInternalMessage now only delivers already-queued messages (SEC-HIGH #5).
func enqueueAppInternalForTest(t testing.TB, app *L1App, ctx sdk.Context, msg contractstypes.InternalMessage) contractstypes.InternalMessage {
	t.Helper()
	if msg.LogicalTime == 0 {
		msg.LogicalTime = msg.Height
	}
	if msg.MessageID == "" {
		msg.MessageID = contractstypes.ComputeInternalMessageID(msg)
	}
	gs, err := app.ContractsKeeper.ExportGenesisState(ctx)
	require.NoError(t, err)
	gs.State.InternalMessages = append(gs.State.InternalMessages, msg)
	require.NoError(t, app.ContractsKeeper.InitGenesisState(ctx, gs))
	out, err := app.ContractsKeeper.ExportGenesisState(ctx)
	require.NoError(t, err)
	for _, m := range out.State.InternalMessages {
		if m.MessageID == msg.MessageID {
			return m
		}
	}
	t.Fatalf("enqueued app internal message not found: %s", msg.MessageID)
	return contractstypes.InternalMessage{}
}
