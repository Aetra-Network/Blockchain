package keeper

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sovereign-l1/l1/app/addressing"
	coretypes "github.com/sovereign-l1/l1/x/aetracore/types"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/aetravm/compiler"
	"github.com/sovereign-l1/l1/x/contracts/types"
	"github.com/sovereign-l1/l1/x/internal/kvtest"
)

// TestContractsKeeperReloadsCommittedStateOnRestart is the regression guard for
// SEC-HIGH #6: the contracts keeper drives every handler off the in-memory
// k.genesis and persists it as a single blob, but never reloaded it from the
// store per block. A restarted or state-synced node (where InitGenesis is not
// re-run) would operate on the empty default and diverge from continuously
// running nodes. loadForBlock, invoked at every state-changing handler, must
// hydrate the committed state first.
func TestContractsKeeperReloadsCommittedStateOnRestart(t *testing.T) {
	ctx := context.Background()
	service := kvtest.NewStoreService()
	wallet := aeAddress("11")

	source := NewPersistentKeeper(service)
	source.accountStatusReader = testAccountStatus{wallet: accountStatusActive}
	require.NoError(t, source.InitGenesisState(ctx, types.DefaultGenesis()))
	_, err := source.StoreCodeState(ctx, types.MsgStoreCode{Authority: wallet, CodeHash: sha256Hex("restart-code"), CodeBytes: 256})
	require.NoError(t, err)

	restarted := NewPersistentKeeper(service)
	restarted.accountStatusReader = testAccountStatus{wallet: accountStatusActive}
	require.Empty(t, restarted.ExportGenesis().State.Codes, "fresh keeper starts at the empty default in memory")

	require.NoError(t, restarted.loadForBlock(ctx))
	require.Len(t, restarted.ExportGenesis().State.Codes, 1, "restarted node must load committed code from the store")
}

func TestContractsKeeperGenesisExportImportInvariantsAndRootContribution(t *testing.T) {
	keeper := NewKeeper()
	require.NoError(t, keeper.ValidateInvariants())

	exported := keeper.ExportGenesis()
	require.NoError(t, exported.Validate())

	imported := NewKeeper()
	require.NoError(t, imported.InitGenesis(exported))
	require.Equal(t, exported, imported.ExportGenesis())

	root, err := imported.RootContribution()
	require.NoError(t, err)
	require.Equal(t, coretypes.RootType(types.ModuleName), root.RootType)
	require.Equal(t, types.ModuleName, root.ID)
	require.Equal(t, exported.StateRoot, root.RootHash)
	require.NoError(t, root.Validate())
}

func TestContractsKeeperTypedErrorsAndMsgQuerySurface(t *testing.T) {
	keeper := NewKeeper()
	authority := aeAddress("11")
	codeHash := coretypes.DeterministicEmptyRootCommitment(coretypes.RootType(types.ModuleName), "code")

	response, err := keeper.StoreCode(types.MsgStoreCode{Authority: authority, CodeHash: codeHash, CodeBytes: 128})
	require.NoError(t, err)
	require.Equal(t, codeHash, response.CodeID)
	require.NotEmpty(t, response.StateRoot)

	_, err = keeper.StoreCode(types.MsgStoreCode{Authority: authority, CodeHash: codeHash, CodeBytes: 0})
	require.ErrorContains(t, err, types.ErrInvalidBytecode)

	stateInit := types.NewStateInit(authority, codeHash, nil, "query", 0)
	contractAddress, _, err := types.DeriveContractAddressFromStateInit("", "", authority, stateInit, types.DefaultParams())
	require.NoError(t, err)
	query, err := keeper.Contract(types.QueryContractRequest{ContractAddress: contractAddress})
	require.NoError(t, err)
	require.False(t, query.Found)
	require.Equal(t, contractAddress, query.ContractAddress)
	require.Equal(t, types.ContractStatusNonExistent, query.Status, "an address with no live contract must report the nonexistent status")

	_, err = keeper.Contract(types.QueryContractRequest{})
	require.ErrorContains(t, err, types.ErrContractNotFound)
}

func TestAVMExitCodesAreSmallStableAndNamed(t *testing.T) {
	require.Equal(t, uint32(0), types.ExitCodeOK)
	require.Equal(t, "ok", types.ExitCodeName(types.ExitCodeOK))
	require.Equal(t, "code_rejected", types.ExitCodeName(types.ExitCodeCodeRejected))
	require.Equal(t, "internal_bounce", types.ExitCodeName(types.ExitCodeInternalBounce))
	require.Less(t, types.ExitCodeInternalBounce, uint32(100))
	require.Equal(t, "unknown", types.ExitCodeName(105))
}

func TestStoreCodeAcceptsCanonicalAVMBytecodeAndRejectsNondeterminism(t *testing.T) {
	wallet := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})
	bytecode := []byte("AVM1\nset key value\nemit ok")
	codeHash := types.CanonicalCodeHash(bytecode)

	response, err := k.StoreCode(types.MsgStoreCode{Authority: wallet, Bytecode: bytecode})
	require.NoError(t, err)
	require.Equal(t, codeHash, response.CodeID)
	exported := k.ExportGenesis()
	require.Len(t, exported.State.Codes, 1)
	require.Equal(t, bytecode, exported.State.Codes[0].Bytecode)
	require.Equal(t, uint64(len(bytecode)), exported.State.Codes[0].CodeBytes)
	require.Equal(t, codeHash, exported.State.Codes[0].CodeHash)

	_, err = k.StoreCode(types.MsgStoreCode{Authority: wallet, Bytecode: []byte("AVM1 time.now")})
	require.ErrorContains(t, err, types.ErrInvalidBytecode)
	_, err = k.StoreCode(types.MsgStoreCode{Authority: wallet, CodeHash: sha256Hex("wrong"), Bytecode: bytecode})
	require.ErrorContains(t, err, "canonical bytecode hash")
}

func TestStoreCodeRejectsOversizedAndMalformedBytecodeWithoutMutatingGenesis(t *testing.T) {
	wallet := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})
	k.genesis.Params.MaxCodeBytes = 64

	before := k.ExportGenesis()

	_, err := k.StoreCode(types.MsgStoreCode{Authority: wallet, Bytecode: []byte("BAD1 deterministic")})
	require.ErrorContains(t, err, types.ErrInvalidBytecode)
	require.Equal(t, before, k.ExportGenesis())

	_, err = k.StoreCode(types.MsgStoreCode{Authority: wallet, Bytecode: []byte("AVM1 time.now")})
	require.ErrorContains(t, err, types.ErrInvalidBytecode)
	require.Equal(t, before, k.ExportGenesis())

	oversized := bytes.Repeat([]byte("AVM1"), 17)
	require.Greater(t, len(oversized), int(k.genesis.Params.MaxCodeBytes))
	_, err = k.StoreCode(types.MsgStoreCode{Authority: wallet, Bytecode: oversized})
	require.ErrorContains(t, err, types.ErrInvalidBytecode)
	require.Equal(t, before, k.ExportGenesis())
}

func TestWalletInstantiatesExecutesAndPassesFunds(t *testing.T) {
	wallet := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})
	codeHash := storeContractCode(t, &k, wallet)
	initMsg := []byte(`{"owner":"wallet"}`)
	initialFunds := uint64(500)

	created, err := k.InstantiateContract(types.MsgInstantiateContract{
		Creator: wallet,
		CodeID:  codeHash,
		InitMsg: initMsg,
		Funds:   initialFunds,
		Admin:   wallet,
		Salt:    "contract-a",
		Height:  10,
	})
	require.NoError(t, err)
	require.Equal(t, wallet, created.Owner)
	require.Equal(t, wallet, created.Admin)
	require.Equal(t, initialFunds, created.Balance)
	require.True(t, stringsHasPrefix(created.ContractAddressUser, "AE"))
	require.True(t, stringsHasPrefix(created.ContractAddressRaw, "ae1"))
	require.Equal(t, types.EventTypeContractInstantiated, created.Events[0].Type)
	require.Equal(t, created.ContractAddressUser, created.Events[0].Contract)
	require.Equal(t, created.ContractAddressRaw, created.Events[0].InternalRaw)
	query, err := k.Contract(types.QueryContractRequest{ContractAddress: created.ContractAddressUser})
	require.NoError(t, err)
	require.Equal(t, contractStorageBytes(128, initMsg), query.Contract.StorageBytes)
	require.Equal(t, codeHash, query.Contract.CodeHash)
	require.NotEmpty(t, query.Contract.StateRoot)
	require.Equal(t, uint64(1), query.Contract.LogicalTime)

	execMsg := []byte(`{"transfer":1}`)
	executed, err := k.ExecuteContract(types.MsgExecuteContract{
		Sender:          wallet,
		ContractAddress: created.ContractAddressUser,
		Msg:             execMsg,
		Funds:           25,
		Height:          11,
	})
	require.NoError(t, err)
	require.Equal(t, created.ContractAddressUser, executed.ContractAddressUser)
	require.Equal(t, initialFunds-contractStorageBytes(128, initMsg)+25, executed.Balance)
	require.Equal(t, types.EventTypeContractExecuted, executed.Events[0].Type)
	require.Equal(t, created.ContractAddressUser, executed.Events[0].Contract)
	require.Equal(t, created.ContractAddressRaw, executed.Events[0].InternalRaw)
	query, err = k.Contract(types.QueryContractRequest{ContractAddress: created.ContractAddressUser})
	require.NoError(t, err)
	require.Equal(t, contractStorageBytes(128, execMsg), query.Contract.StorageBytes)
	require.Equal(t, uint64(2), query.Contract.LogicalTime)
	require.Equal(t, types.ComputeContractStateRoot(query.Contract), query.Contract.StateRoot)
}

func TestExecuteContractRollsBackOnPostRentFailure(t *testing.T) {
	wallet := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})
	codeHash := storeContractCode(t, &k, wallet)
	created, err := k.InstantiateContract(types.MsgInstantiateContract{
		Creator:      wallet,
		CodeID:       codeHash,
		InitMsg:      []byte("init"),
		Funds:        math.MaxUint64 - 5_000,
		Admin:        wallet,
		Salt:         "atomic-execute",
		Height:       10,
		StorageBytes: 132,
	})
	require.NoError(t, err)

	before := k.ExportGenesis()
	beforeContract, err := k.Contract(types.QueryContractRequest{ContractAddress: created.ContractAddressUser})
	require.NoError(t, err)
	_, err = k.ExecuteContract(types.MsgExecuteContract{
		Sender:          wallet,
		ContractAddress: created.ContractAddressUser,
		Msg:             []byte("overflow"),
		Funds:           10_000,
		Height:          20,
	})
	require.ErrorContains(t, err, "contract balance overflow")

	after := k.ExportGenesis()
	require.Equal(t, before, after)

	query, err := k.Contract(types.QueryContractRequest{ContractAddress: created.ContractAddressUser})
	require.NoError(t, err)
	require.Equal(t, beforeContract.Contract.Balance, query.Contract.Balance)
	require.Equal(t, beforeContract.Contract.LastStorageChargeHeight, query.Contract.LastStorageChargeHeight)
	require.Equal(t, beforeContract.Contract.StateRoot, query.Contract.StateRoot)
}

func TestContractUpgradeMigrationAndAdminPolicy(t *testing.T) {
	wallet := aeAddress("11")
	admin := aeAddress("22")
	other := aeAddress("33")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive, admin: accountStatusActive, other: accountStatusActive})
	codeV1 := storeContractCode(t, &k, wallet)
	codeV2Hash := sha256Hex("code-v2")
	_, err := k.StoreCode(types.MsgStoreCode{Authority: wallet, CodeHash: codeV2Hash, CodeBytes: 256})
	require.NoError(t, err)

	immutable, err := k.InstantiateContract(types.MsgInstantiateContract{
		Creator: wallet, CodeID: codeV1, InitMsg: []byte("v1"), Admin: admin, Salt: "immutable", Height: 10,
	})
	require.NoError(t, err)
	_, err = k.UpgradeContractCode(types.MsgUpgradeContractCode{
		Actor: admin, ContractAddress: immutable.ContractAddressUser, NewCodeID: codeV2Hash, MigrationHandler: "schema_only", Height: 11,
	})
	require.ErrorContains(t, err, "immutable")

	upgradeable, err := k.InstantiateContract(types.MsgInstantiateContract{
		Creator: wallet, CodeID: codeV1, InitMsg: []byte("v1"), Admin: admin, Salt: "upgradeable", Upgradeable: true, SchemaVersion: 1, Height: 20,
	})
	require.NoError(t, err)
	_, err = k.UpgradeContractCode(types.MsgUpgradeContractCode{
		Actor: other, ContractAddress: upgradeable.ContractAddressUser, NewCodeID: codeV2Hash, MigrationHandler: "schema_only", Height: 21,
	})
	require.ErrorContains(t, err, types.ErrUnauthorized)
	receipt, err := k.UpgradeContractCode(types.MsgUpgradeContractCode{
		Actor: admin, ContractAddress: upgradeable.ContractAddressUser, NewCodeID: codeV2Hash, MigrationHandler: "schema_only", Height: 22,
	})
	require.NoError(t, err)
	require.Equal(t, "upgrade_code", receipt.Operation)
	query, err := k.Contract(types.QueryContractRequest{ContractAddress: upgradeable.ContractAddressUser})
	require.NoError(t, err)
	require.Equal(t, codeV2Hash, query.Contract.CodeID)
	require.Equal(t, uint64(1), query.Contract.StorageSchemaVersion)

	beforeRoot := query.Contract.StateRoot
	_, err = k.MigrateContractState(types.MsgMigrateContractState{
		Actor: admin, ContractAddress: upgradeable.ContractAddressUser, FromSchemaVersion: 1, ToSchemaVersion: 2, MigrationHandler: "fail", Payload: []byte("bad"), Height: 23,
	})
	require.ErrorContains(t, err, "migration handler failed")
	rolledBack, err := k.Contract(types.QueryContractRequest{ContractAddress: upgradeable.ContractAddressUser})
	require.NoError(t, err)
	require.Equal(t, uint64(1), rolledBack.Contract.StorageSchemaVersion)
	require.Equal(t, beforeRoot, rolledBack.Contract.StateRoot)

	receipt, err = k.MigrateContractState(types.MsgMigrateContractState{
		Actor: admin, ContractAddress: upgradeable.ContractAddressUser, FromSchemaVersion: 1, ToSchemaVersion: 2, MigrationHandler: "append", Payload: []byte(":v2"), Height: 24,
	})
	require.NoError(t, err)
	require.Equal(t, "migrate_state", receipt.Operation)
	migrated, err := k.Contract(types.QueryContractRequest{ContractAddress: upgradeable.ContractAddressUser})
	require.NoError(t, err)
	require.Equal(t, uint64(2), migrated.Contract.StorageSchemaVersion)
	require.Equal(t, []byte("v1:v2"), migrated.Contract.Data)

	newAdmin := aeAddress("44")
	_, err = k.SetContractAdmin(types.MsgSetContractAdmin{Actor: admin, ContractAddress: upgradeable.ContractAddressUser, NewAdmin: newAdmin, Height: 25})
	require.NoError(t, err)
	_, err = k.DisableContractUpgrades(types.MsgDisableContractUpgrades{Actor: newAdmin, ContractAddress: upgradeable.ContractAddressUser, Height: 26})
	require.NoError(t, err)
	_, err = k.UpgradeContractCode(types.MsgUpgradeContractCode{
		Actor: newAdmin, ContractAddress: upgradeable.ContractAddressUser, NewCodeID: codeV1, MigrationHandler: "schema_only", Height: 27,
	})
	require.ErrorContains(t, err, "immutable")
}

func TestSystemOwnedContractUpgradeRequiresGovernanceAuthority(t *testing.T) {
	wallet := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})
	codeV1 := storeContractCode(t, &k, wallet)
	codeV2 := sha256Hex("system-code-v2")
	_, err := k.StoreCode(types.MsgStoreCode{Authority: wallet, CodeHash: codeV2, CodeBytes: 256})
	require.NoError(t, err)
	created, err := k.InstantiateContract(types.MsgInstantiateContract{
		Creator: wallet, CodeID: codeV1, InitMsg: []byte("sys"), Admin: wallet, Salt: "system-owned", Upgradeable: true, SystemOwned: true, Height: 10,
	})
	require.NoError(t, err)
	_, err = k.UpgradeContractCode(types.MsgUpgradeContractCode{
		Actor: wallet, ContractAddress: created.ContractAddressUser, NewCodeID: codeV2, MigrationHandler: "schema_only", Height: 11,
	})
	require.ErrorContains(t, err, "governance authority")
	_, err = k.UpgradeContractCode(types.MsgUpgradeContractCode{
		Actor: k.Params().Authority, ContractAddress: created.ContractAddressUser, NewCodeID: codeV2, MigrationHandler: "schema_only", Height: 12,
	})
	require.NoError(t, err)
}

func TestFrozenWalletCannotInstantiateOrExecuteUntilUnfrozen(t *testing.T) {
	wallet := aeAddress("11")
	status := testAccountStatus{wallet: accountStatusActive}
	k := NewKeeperWithAccountStatus(status)
	codeHash := storeContractCode(t, &k, wallet)
	created := instantiateContract(t, &k, wallet, codeHash, "contract-a", 10, 300, 2)

	status[wallet] = accountStatusFrozen
	k.accountStatusReader = status
	_, err := k.InstantiateContract(types.MsgInstantiateContract{Creator: wallet, CodeID: codeHash, Salt: "contract-b", Height: 11})
	require.ErrorContains(t, err, types.ErrAccountFrozen)

	_, err = k.ExecuteContract(types.MsgExecuteContract{Sender: wallet, ContractAddress: created.ContractAddressUser, Msg: []byte("blocked"), Height: 11})
	require.ErrorContains(t, err, types.ErrAccountFrozen)

	status[wallet] = accountStatusActive
	k.accountStatusReader = status
	_, err = k.ExecuteContract(types.MsgExecuteContract{Sender: wallet, ContractAddress: created.ContractAddressUser, Msg: []byte("ok"), Height: 11})
	require.NoError(t, err)
}

func TestFrozenContractRecoveryKeepsCodeDataAndBalance(t *testing.T) {
	wallet := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})
	codeHash := storeContractCode(t, &k, wallet)
	created := instantiateContract(t, &k, wallet, codeHash, "rent", 10, 1, 100)

	_, err := k.ExecuteContract(types.MsgExecuteContract{Sender: wallet, ContractAddress: created.ContractAddressUser, Msg: []byte("too late"), Height: 200})
	require.ErrorContains(t, err, types.ErrStorageRent)

	frozen, err := k.Contract(types.QueryContractRequest{ContractAddress: created.ContractAddressUser})
	require.NoError(t, err)
	require.True(t, frozen.Found)
	require.Equal(t, types.ContractStatusFrozen, frozen.Contract.Status)
	require.Equal(t, codeHash, frozen.Contract.CodeID)
	require.Equal(t, []byte("init"), frozen.Contract.Data)
	require.Zero(t, frozen.Contract.Balance)
	require.NotZero(t, frozen.Contract.StorageRentDebt)

	topped, err := k.TopUpContract(types.MsgTopUpContract{Sender: wallet, ContractAddress: created.ContractAddressUser, Amount: frozen.Contract.StorageRentDebt + 50, Height: 201})
	require.NoError(t, err)
	require.Equal(t, codeHash, topped.CodeID)
	require.Equal(t, []byte("init"), topped.Data)

	paid, err := k.PayContractStorageDebt(types.MsgPayContractStorageDebt{Sender: wallet, ContractAddress: created.ContractAddressUser, Amount: frozen.Contract.StorageRentDebt, Height: 202})
	require.NoError(t, err)
	require.Zero(t, paid.StorageRentDebt)
	require.Equal(t, []byte("init"), paid.Data)

	_, err = k.ExecuteContract(types.MsgExecuteContract{Sender: wallet, ContractAddress: created.ContractAddressUser, Msg: []byte("still-frozen"), Height: 202})
	require.ErrorContains(t, err, types.ErrAccountFrozen)

	unfrozen, err := k.UnfreezeContract(types.MsgUnfreezeContract{Sender: wallet, ContractAddress: created.ContractAddressUser, Height: 203})
	require.NoError(t, err)
	require.Equal(t, types.ContractStatusActive, unfrozen.Status)
	require.Equal(t, codeHash, unfrozen.CodeID)
	require.Equal(t, []byte("init"), unfrozen.Data)
	require.NotZero(t, unfrozen.Balance)
}

func TestContractOwnersAdminsAndAssetQueriesUseAEAndRegistryState(t *testing.T) {
	wallet := aeAddress("11")
	admin := aeAddress("12")
	assetOwner := aeAddress("13")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})
	codeHash := storeContractCode(t, &k, wallet)
	rawAdmin, err := types.RawAddressForUserAddress(admin)
	require.NoError(t, err)
	_, err = k.InstantiateContract(types.MsgInstantiateContract{
		Creator: wallet, CodeID: codeHash, Admin: rawAdmin, Salt: "raw-admin", Height: 9,
	})
	require.ErrorContains(t, err, "AE user-facing")

	created, err := k.InstantiateContract(types.MsgInstantiateContract{
		Creator: wallet, CodeID: codeHash, Admin: admin, Salt: "asset-contract", Height: 10,
	})
	require.NoError(t, err)
	require.True(t, stringsHasPrefix(created.Owner, "AE"))
	require.True(t, stringsHasPrefix(created.Admin, "AE"))

	err = k.SetAssetOwner(types.AssetOwnershipRecord{
		AssetType:           "contract_asset",
		ContractAddressUser: created.ContractAddressUser,
		AssetID:             "asset-1",
		Owner:               assetOwner,
	})
	require.NoError(t, err)
	owner, err := k.AssetOwner(types.QueryAssetOwnerRequest{AssetType: "contract_asset", ContractAddressUser: created.ContractAddressUser, AssetID: "asset-1"})
	require.NoError(t, err)
	require.True(t, owner.Found)
	require.Equal(t, assetOwner, owner.Owner)

	exported := k.ExportGenesis()
	require.NotEqual(t, "token-balance", exported.State.Contracts[0].Owner)
	require.Len(t, exported.State.AssetOwnership, 1)
}

func TestAssetOwnershipRecordValidate(t *testing.T) {
	wallet := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})
	codeHash := storeContractCode(t, &k, wallet)
	created := instantiateContract(t, &k, wallet, codeHash, "asset-record", 10, 0, 0)

	err := k.SetAssetOwner(types.AssetOwnershipRecord{
		AssetType:           "contract_asset",
		ContractAddressUser: created.ContractAddressUser,
		AssetID:             "asset-1",
		Owner:               wallet,
	})
	require.NoError(t, err)
}

func TestOfficialLiquidStakingContractCapabilityAllowsNativeHookOnlyForAuthorizedContract(t *testing.T) {
	wallet := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})
	codeHash := storeContractCode(t, &k, wallet)
	official := instantiateContract(t, &k, wallet, codeHash, "official-lst", 10, 1000, 0)
	unauthorized := instantiateContract(t, &k, wallet, codeHash, "other-contract", 11, 1000, 0)

	capability, err := k.GrantNativeStakingCapability(types.MsgGrantNativeStakingCapability{
		Authority:           types.DefaultParams().Authority,
		ContractAddressUser: official.ContractAddressUser,
		ContractAddressRaw:  official.ContractAddressRaw,
		PoolID:              "official-pool",
		Height:              12,
	})
	require.NoError(t, err)
	require.Equal(t, official.ContractAddressUser, capability.ContractAddressUser)
	require.Equal(t, official.ContractAddressRaw, capability.ContractAddressRaw)

	injection, err := k.InjectNativeStaking(types.MsgInjectNativeStaking{
		CallerContractUser: official.ContractAddressUser,
		CallerContractRaw:  official.ContractAddressRaw,
		PoolID:             "official-pool",
		Amount:             500,
		Height:             13,
	})
	require.NoError(t, err)
	require.Equal(t, official.ContractAddressUser, injection.ContractAddressUser)
	require.Equal(t, official.ContractAddressRaw, injection.ContractAddressRaw)

	_, err = k.InjectNativeStaking(types.MsgInjectNativeStaking{
		CallerContractUser: unauthorized.ContractAddressUser,
		CallerContractRaw:  unauthorized.ContractAddressRaw,
		PoolID:             "official-pool",
		Amount:             1,
		Height:             14,
	})
	require.ErrorContains(t, err, types.ErrUnauthorized)
}

func TestNativeStakingCapabilityRejectsBadAuthorityAndFrozenContract(t *testing.T) {
	wallet := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})
	codeHash := storeContractCode(t, &k, wallet)
	official := instantiateContract(t, &k, wallet, codeHash, "official-frozen", 10, 100, 0)

	_, err := k.GrantNativeStakingCapability(types.MsgGrantNativeStakingCapability{
		Authority:           wallet,
		ContractAddressUser: official.ContractAddressUser,
		ContractAddressRaw:  official.ContractAddressRaw,
		PoolID:              "official-pool",
		Height:              11,
	})
	require.ErrorContains(t, err, types.ErrUnauthorized)

	_, err = k.GrantNativeStakingCapability(types.MsgGrantNativeStakingCapability{
		Authority:           types.DefaultParams().Authority,
		ContractAddressUser: official.ContractAddressUser,
		ContractAddressRaw:  official.ContractAddressRaw,
		PoolID:              "official-pool",
		Height:              12,
	})
	require.NoError(t, err)

	gs := k.ExportGenesis()
	gs.State.Contracts[0].Status = types.ContractStatusFrozen
	require.NoError(t, k.InitGenesis(gs))

	_, err = k.InjectNativeStaking(types.MsgInjectNativeStaking{
		CallerContractUser: official.ContractAddressUser,
		CallerContractRaw:  official.ContractAddressRaw,
		PoolID:             "official-pool",
		Amount:             500,
		Height:             13,
	})
	require.ErrorContains(t, err, types.ErrAccountFrozen)

	query, err := k.Contract(types.QueryContractRequest{ContractAddress: official.ContractAddressUser})
	require.NoError(t, err)
	require.True(t, query.Found)
	require.Equal(t, types.ContractStatusFrozen, query.Contract.Status)
}

func TestNativeStakingHookChargesStorageRentBeforeInjection(t *testing.T) {
	wallet := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})
	codeHash := storeContractCode(t, &k, wallet)
	official := instantiateContract(t, &k, wallet, codeHash, "official-rent", 10, 1, 0)

	_, err := k.GrantNativeStakingCapability(types.MsgGrantNativeStakingCapability{
		Authority:           types.DefaultParams().Authority,
		ContractAddressUser: official.ContractAddressUser,
		ContractAddressRaw:  official.ContractAddressRaw,
		PoolID:              "official-pool",
		Height:              11,
	})
	require.NoError(t, err)

	_, err = k.InjectNativeStaking(types.MsgInjectNativeStaking{
		CallerContractUser: official.ContractAddressUser,
		CallerContractRaw:  official.ContractAddressRaw,
		PoolID:             "official-pool",
		Amount:             500,
		Height:             12,
	})
	require.ErrorContains(t, err, types.ErrStorageRent)

	query, err := k.Contract(types.QueryContractRequest{ContractAddress: official.ContractAddressUser})
	require.NoError(t, err)
	require.Equal(t, types.ContractStatusFrozenLimited, query.Contract.Status)
	require.Zero(t, query.Contract.Balance)
	require.Equal(t, []byte("init"), query.Contract.Data)
	require.NotZero(t, query.Contract.StorageRentDebt)
	require.Empty(t, k.ExportGenesis().State.NativeStakingInjects)

	_, err = k.ExecuteContract(types.MsgExecuteContract{Sender: wallet, ContractAddress: official.ContractAddressUser, Msg: []byte("blocked"), Height: 13})
	require.ErrorContains(t, err, types.ErrAccountFrozen)
}

// TestInternalMessageForgedSourceRejected is the regression guard for SEC-HIGH
// #5: internal messages are now delivered only from the pending queue (whose
// sole writer, appendAVMOutgoingMessages, stamps a verified source). A message
// that was never legitimately produced — a spoofed source or fabricated fields —
// recomputes to an ID absent from the queue and is rejected, so it can neither
// drive a destination contract nor fabricate attached value.
func TestInternalMessageForgedSourceRejected(t *testing.T) {
	wallet := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})
	codeHash := storeContractCode(t, &k, wallet)
	source := instantiateContract(t, &k, wallet, codeHash, "forge-src", 10, 1000, 0)

	_, err := k.ReceiveInternalMessage(types.MsgReceiveInternalMessage{
		SourceContractUser: source.ContractAddressUser,
		DestinationAccount: aeAddress("22"),
		Funds:              100,
		Body:               []byte("forged"),
		Height:             11,
	})
	require.ErrorContains(t, err, types.ErrUnauthorized)

	// No fabricated debit/credit occurred: the source balance is untouched.
	q, err := k.Contract(types.QueryContractRequest{ContractAddress: source.ContractAddressUser})
	require.NoError(t, err)
	require.Equal(t, uint64(1000), q.Contract.Balance)
	require.Empty(t, k.ExportGenesis().State.InternalMessages)
}

func TestInternalMessagesAndExportImportAreDeterministic(t *testing.T) {
	wallet := aeAddress("11")
	destination := aeAddress("22")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})
	codeHash := storeContractCode(t, &k, wallet)
	contract := instantiateContract(t, &k, wallet, codeHash, "internal", 10, 1000, 0)

	// SEC-HIGH #5: internal messages are produced (queued) by contract execution,
	// not authored by callers; enqueue via the test helper (stands in for
	// appendAVMOutgoingMessages) then assert the queued record.
	message := enqueueInternalForTest(t, &k, types.InternalMessage{
		SourceContractUser: contract.ContractAddressUser,
		DestinationAccount: destination,
		Funds:              7,
		Body:               []byte("hello"),
		Height:             11,
	})
	require.Equal(t, contract.ContractAddressUser, message.SourceContractUser)
	require.Equal(t, destination, message.DestinationAccount)

	exported := k.ExportGenesis()
	require.NoError(t, exported.Validate())
	roundTrip := NewKeeper()
	require.NoError(t, roundTrip.InitGenesis(exported))
	require.Equal(t, exported, roundTrip.ExportGenesis())
}

func TestContractsTypedMsgAndQueryServiceSurface(t *testing.T) {
	wallet := aeAddress("11")
	destination := aeAddress("22")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})
	bytecode := []byte("AVM1 typed-service")
	stored, err := k.StoreCode(types.MsgStoreCode{Authority: wallet, Bytecode: bytecode})
	require.NoError(t, err)

	code, found, err := k.Code(types.QueryCodeRequest{CodeID: stored.CodeID})
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, types.CanonicalCodeHash(bytecode), code.CodeHash)
	codes, err := k.Codes(types.QueryCodesRequest{Pagination: types.PageRequest{Limit: 1}})
	require.NoError(t, err)
	require.Len(t, codes, 1)

	deployed, err := k.DeployContract(types.MsgDeployContract{
		Creator:        wallet,
		CodeID:         stored.CodeID,
		Salt:           "typed",
		InitPayload:    []byte("init"),
		InitialBalance: 1_000,
		Admin:          wallet,
		Height:         20,
	})
	require.NoError(t, err)
	executed, err := k.ExecuteExternal(types.MsgExecuteExternal{
		Sender:          wallet,
		ContractAddress: deployed.ContractAddressUser,
		Payload:         []byte("call"),
		GasLimit:        k.Params().MaxGasPerExecution,
		Height:          21,
	})
	require.NoError(t, err)
	require.Equal(t, deployed.ContractAddressUser, executed.ContractAddressUser)

	stateRoot, err := k.ContractStateRoot(types.QueryContractStateRootRequest{ContractAddress: deployed.ContractAddressUser})
	require.NoError(t, err)
	require.NotEmpty(t, stateRoot)
	contracts, err := k.Contracts(types.QueryContractsRequest{Pagination: types.PageRequest{Limit: 1}})
	require.NoError(t, err)
	require.Len(t, contracts, 1)

	// SEC-HIGH #5: internal messages are produced (queued) by contract execution;
	// enqueue via the test helper (stands in for appendAVMOutgoingMessages).
	internal := enqueueInternalForTest(t, &k, types.InternalMessage{
		SourceContractUser: deployed.ContractAddressUser,
		DestinationAccount: destination,
		Funds:              5,
		Opcode:             7,
		QueryID:            9,
		Body:               []byte("internal"),
		Bounce:             true,
		Deadline:           25,
		GasLimit:           100,
		LogicalTime:        3,
		Height:             22,
	})
	require.NotEmpty(t, internal.MessageID)
	require.Equal(t, types.ComputeInternalMessageID(internal), internal.MessageID)
	queue, err := k.ContractQueue(types.QueryContractQueueRequest{ContractAddress: deployed.ContractAddressUser, Pagination: types.PageRequest{Limit: 10}})
	require.NoError(t, err)
	require.Equal(t, []types.InternalMessage{internal}, queue)
	storage, err := k.ContractStorage(types.QueryContractStorageRequest{ContractAddress: deployed.ContractAddressUser, Pagination: types.PageRequest{Limit: 1}})
	require.NoError(t, err)
	require.Equal(t, []types.ContractStorageEntry{{
		ContractAddress: deployed.ContractAddressUser,
		Key:             []byte("data"),
		Value:           []byte("call"),
	}}, storage)
	receipts, err := k.ContractReceipts(types.QueryContractReceiptsRequest{ContractAddress: deployed.ContractAddressUser, Pagination: types.PageRequest{Limit: 10}})
	require.NoError(t, err)
	require.Len(t, receipts, 2)
	require.Equal(t, "deploy", receipts[0].Operation)
	require.Equal(t, "execute", receipts[1].Operation)
	require.NoError(t, k.ContractEvents(types.QueryContractEventsRequest{ContractAddress: deployed.ContractAddressUser, Pagination: types.PageRequest{Limit: 1}}))
}

func TestCompiledATLXContractsExecuteThroughAVMRuntimeBridge(t *testing.T) {
	wallet := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})

	const src = `
@storage
struct Storage {
    count: uint64
}

@message(0x1001)
struct Ping {}

type InternalMsg = Ping
type ExternalMsg = Ping

contract Counter {
    storage: Storage
    incomingMessages: InternalMsg
    incomingExternal: ExternalMsg

    @store
    func Storage.load() {
        return Storage.fromChunk(contract.getData())
    }

    @store
    func Storage.save(self) {
        contract.setData(self.toChunk())
    }

    @internal
    func onInternalMessage(in: InMessage) {
        var st = lazy Storage.load()
        st.count += 1
        st.save()
    }

    @external
    func onExternalMessage(inMsg: Segment) {
        var st = lazy Storage.load()
        st.count += 1
        st.save()
    }
}
`

	c, err := compiler.New(compiler.DefaultOptions())
	require.NoError(t, err)
	compiled, err := c.Compile([]byte(src))
	require.NoError(t, err)
	initData, err := compiled.StorageCodec.Encode(map[string]any{"count": uint64(0)})
	require.NoError(t, err)

	stored, err := k.StoreCode(types.MsgStoreCode{
		Authority: wallet,
		Bytecode:  compiled.ModuleBytes,
	})
	require.NoError(t, err)

	deployed, err := k.InstantiateContract(types.MsgInstantiateContract{
		Creator: wallet,
		CodeID:  stored.CodeID,
		InitMsg: initData,
		Funds:   10_000,
		Admin:   wallet,
		Salt:    "compiled-avm-bridge",
		Height:  10,
	})
	require.NoError(t, err)

	_, err = k.ExecuteExternal(types.MsgExecuteExternal{
		Sender:          wallet,
		ContractAddress: deployed.ContractAddressUser,
		Payload:         []byte{0x01},
		GasLimit:        100_000,
		Height:          11,
	})
	require.NoError(t, err)

	contractAfterExternal, err := k.Contract(types.QueryContractRequest{ContractAddress: deployed.ContractAddressUser})
	require.NoError(t, err)
	storageAfterExternal, err := avm.DecodeSnapshot(contractAfterExternal.Contract.Data)
	require.NoError(t, err)
	require.Equal(t, uint64(1), avm.DecodeU64(storageAfterExternal["count"]))

	// SEC-HIGH #5: enqueue (as a contract would) then deliver by ID; the
	// self-addressed message executes the destination and is dequeued.
	queued := enqueueInternalForTest(t, &k, types.InternalMessage{
		SourceContractUser: deployed.ContractAddressUser,
		DestinationAccount: deployed.ContractAddressUser,
		Funds:              0,
		Opcode:             0x1001,
		QueryID:            1,
		Body:               []byte{},
		Bounce:             true,
		Deadline:           100,
		GasLimit:           100_000,
		LogicalTime:        1,
		Height:             12,
	})
	_, err = k.ReceiveInternalMessage(types.MsgReceiveInternalMessage{MessageID: queued.MessageID, Height: 12})
	require.NoError(t, err)

	contractAfterInternal, err := k.Contract(types.QueryContractRequest{ContractAddress: deployed.ContractAddressUser})
	require.NoError(t, err)
	storageAfterInternal, err := avm.DecodeSnapshot(contractAfterInternal.Contract.Data)
	require.NoError(t, err)
	require.Equal(t, uint64(2), avm.DecodeU64(storageAfterInternal["count"]))

	queue, err := k.ContractQueue(types.QueryContractQueueRequest{
		ContractAddress: deployed.ContractAddressUser,
		Pagination:      types.PageRequest{Limit: 10},
	})
	require.NoError(t, err)
	require.Len(t, queue, 0)
}

// TestEndBlockerAutonomouslyDeliversQueuedInternalMessage is the regression
// guard for task #37: a message queued by a contract's execution now delivers
// on its own via the EndBlock drain, without a separate signed
// MsgReceiveInternalMessage transaction. It also proves the per-block gas
// budget is respected: a budget smaller than the message's gas cost leaves it
// queued for a later block instead of delivering (or erroring) immediately.
func TestEndBlockerAutonomouslyDeliversQueuedInternalMessage(t *testing.T) {
	wallet := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})

	const src = `
@storage
struct Storage {
    count: uint64
}

@message(0x1001)
struct Ping {}

type InternalMsg = Ping
type ExternalMsg = Ping

contract Counter {
    storage: Storage
    incomingMessages: InternalMsg
    incomingExternal: ExternalMsg

    @store
    func Storage.load() {
        return Storage.fromChunk(contract.getData())
    }

    @store
    func Storage.save(self) {
        contract.setData(self.toChunk())
    }

    @internal
    func onInternalMessage(in: InMessage) {
        var st = lazy Storage.load()
        st.count += 1
        st.save()
    }

    @external
    func onExternalMessage(inMsg: Segment) {
        var st = lazy Storage.load()
        st.count += 1
        st.save()
    }
}
`
	c, err := compiler.New(compiler.DefaultOptions())
	require.NoError(t, err)
	compiled, err := c.Compile([]byte(src))
	require.NoError(t, err)
	initData, err := compiled.StorageCodec.Encode(map[string]any{"count": uint64(0)})
	require.NoError(t, err)

	stored, err := k.StoreCode(types.MsgStoreCode{Authority: wallet, Bytecode: compiled.ModuleBytes})
	require.NoError(t, err)
	deployed, err := k.InstantiateContract(types.MsgInstantiateContract{
		Creator: wallet,
		CodeID:  stored.CodeID,
		InitMsg: initData,
		Funds:   10_000,
		Admin:   wallet,
		Salt:    "endblock-drain",
		Height:  10,
	})
	require.NoError(t, err)

	queued := enqueueInternalForTest(t, &k, types.InternalMessage{
		SourceContractUser: deployed.ContractAddressUser,
		DestinationAccount: deployed.ContractAddressUser,
		Funds:              0,
		Opcode:             0x1001,
		QueryID:            1,
		Body:               []byte{},
		GasLimit:           100_000,
		LogicalTime:        1,
		Height:             11,
	})

	ctx := sdk.NewContext(nil, cmtproto.Header{Height: 12}, false, log.NewNopLogger())

	// A budget smaller than the queued message's gas cost must not deliver it
	// (and must not error the block).
	gs := k.ExportGenesis()
	gs.Params.MaxInternalMessageGasPerBlock = 1
	require.NoError(t, k.InitGenesis(gs))
	require.NoError(t, k.EndBlocker(ctx))
	require.Len(t, k.ExportGenesis().State.InternalMessages, 1)

	// Raising the budget to cover the message's gas cost lets EndBlocker
	// deliver it autonomously.
	gs = k.ExportGenesis()
	gs.Params.MaxInternalMessageGasPerBlock = queued.GasLimit
	require.NoError(t, k.InitGenesis(gs))
	require.NoError(t, k.EndBlocker(ctx))
	require.Empty(t, k.ExportGenesis().State.InternalMessages)

	delivered, err := k.Contract(types.QueryContractRequest{ContractAddress: deployed.ContractAddressUser})
	require.NoError(t, err)
	storageDelivered, err := avm.DecodeSnapshot(delivered.Contract.Data)
	require.NoError(t, err)
	require.Equal(t, uint64(1), avm.DecodeU64(storageDelivered["count"]))
}

func TestSecurityAttestationSubmitRevokeAndQuerySurface(t *testing.T) {
	k := NewKeeper()
	contract := aeAddress("55")
	raw, err := types.RawAddressForUserAddress(contract)
	require.NoError(t, err)
	attestation := types.ContractSecurityAttestation{
		ContractAddressUser: contract,
		ContractAddressRaw:  raw,
		Source:              "ci-scan",
		SourceURL:           "https://ci.example/security/scan/77",
		CheckedHeight:       77,
		UpdatedHeight:       78,
		RiskScoreBps:        700,
		Categories:          []string{types.SecurityAttestationCategoryOpenSourceVerified},
		SignedBy:            "AEsigner",
	}
	attestation.AttestationID = types.ComputeSecurityAttestationID(attestation)

	submit, err := k.SubmitSecurityAttestation(types.MsgSubmitSecurityAttestation{
		Authority:   types.DefaultParams().Authority,
		Attestation: attestation,
	})
	require.NoError(t, err)
	require.Equal(t, attestation.AttestationID, submit.Attestation.AttestationID)
	require.NotEmpty(t, submit.StateRoot)

	attestations, err := k.SecurityAttestations(types.QuerySecurityAttestationsRequest{
		ContractAddress: contract,
		Pagination:      types.PageRequest{Limit: 10},
	})
	require.NoError(t, err)
	require.Len(t, attestations, 1)
	require.Equal(t, attestation.AttestationID, attestations[0].AttestationID)

	badge, found, err := k.SecurityBadge(types.QuerySecurityBadgeRequest{ContractAddress: contract})
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, types.SecurityBadgeVerified, badge.Badge)
	require.True(t, badge.Verified)

	revoke, err := k.RevokeSecurityAttestation(types.MsgRevokeSecurityAttestation{
		Authority:     types.DefaultParams().Authority,
		AttestationID: attestation.AttestationID,
		RevokedReason: "replaced by newer scan",
		Height:        80,
	})
	require.NoError(t, err)
	require.Equal(t, types.SecurityAttestationStatusRevoked, revoke.Attestation.Status)
	require.NotEmpty(t, revoke.StateRoot)

	attestations, err = k.SecurityAttestations(types.QuerySecurityAttestationsRequest{
		ContractAddress: contract,
		IncludeRevoked:  true,
		Pagination:      types.PageRequest{Limit: 10},
	})
	require.NoError(t, err)
	require.Len(t, attestations, 1)
	require.Equal(t, types.SecurityAttestationStatusRevoked, attestations[0].Status)

	badge, found, err = k.SecurityBadge(types.QuerySecurityBadgeRequest{ContractAddress: contract})
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, types.SecurityBadgeUnattested, badge.Badge)
}

func TestStateInitCounterfactualDeployVirtualQueryAndExternalAttachment(t *testing.T) {
	wallet := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})
	codeHash := storeContractCode(t, &k, wallet)
	stateInit := types.NewStateInit(wallet, codeHash, []byte("init"), "counterfactual", 1_000)
	expectedUser, expectedRaw, err := types.DeriveContractAddressFromStateInit("chain-a", "zone-a", wallet, stateInit, k.Params())
	require.NoError(t, err)

	virtual, err := k.Contract(types.QueryContractRequest{
		ChainID:   "chain-a",
		Namespace: "zone-a",
		Deployer:  wallet,
		StateInit: &stateInit,
	})
	require.NoError(t, err)
	require.False(t, virtual.Found)
	require.True(t, virtual.Virtual)
	require.Equal(t, expectedUser, virtual.ContractAddress)

	deployed, err := k.DeployContract(types.MsgDeployContract{
		Creator:        wallet,
		CodeID:         codeHash,
		ChainID:        "chain-a",
		Namespace:      "zone-a",
		StateInit:      &stateInit,
		InitPayload:    []byte("init"),
		InitialBalance: 1_000,
		Height:         20,
	})
	require.NoError(t, err)
	require.Equal(t, expectedUser, deployed.ContractAddressUser)
	require.Equal(t, expectedRaw, deployed.ContractAddressRaw)

	_, err = k.DeployContract(types.MsgDeployContract{
		Creator:        wallet,
		CodeID:         codeHash,
		ChainID:        "chain-a",
		Namespace:      "zone-a",
		StateInit:      &stateInit,
		InitPayload:    []byte("init"),
		InitialBalance: 1_000,
		Height:         21,
	})
	require.ErrorContains(t, err, "already exists")

	lazyInit := types.NewStateInit(wallet, codeHash, []byte("lazy-init"), "lazy", 1_000)
	lazyAddress, _, err := types.DeriveContractAddressFromStateInit("chain-a", "zone-a", wallet, lazyInit, k.Params())
	require.NoError(t, err)
	executed, err := k.ExecuteExternal(types.MsgExecuteExternal{
		Sender:          wallet,
		ContractAddress: lazyAddress,
		ChainID:         "chain-a",
		Namespace:       "zone-a",
		StateInit:       &lazyInit,
		Payload:         []byte("call"),
		GasLimit:        k.Params().MaxGasPerExecution,
		Height:          22,
	})
	require.NoError(t, err)
	require.Equal(t, lazyAddress, executed.ContractAddressUser)

	query, err := k.Contract(types.QueryContractRequest{ContractAddress: lazyAddress})
	require.NoError(t, err)
	require.True(t, query.Found)
	require.Equal(t, []byte("call"), query.Contract.Data)
	require.NotEmpty(t, query.Contract.StateInitHash)
	stateInitHash, err := types.HashStateInit(lazyInit)
	require.NoError(t, err)
	require.Equal(t, stateInitHash, query.Contract.StateInitHash)

	exported := k.ExportGenesis()
	imported := NewKeeper()
	require.NoError(t, imported.InitGenesis(exported))
	roundTrip, err := imported.Contract(types.QueryContractRequest{ContractAddress: lazyAddress})
	require.NoError(t, err)
	require.Equal(t, query.Contract.StateInitHash, roundTrip.Contract.StateInitHash)
}

func TestStateInitDeployAcceptanceSuite(t *testing.T) {
	deployer := aeAddress("11")
	status := testAccountStatus{deployer: accountStatusActive}
	k := NewKeeperWithAccountStatus(status)

	t.Run("token wallet deploy", func(t *testing.T) {
		c, err := compiler.New(compiler.DefaultOptions())
		require.NoError(t, err)
		walletResult, err := c.CompileFile("../../../examples/avm/token/token_wallet.atlx")
		require.NoError(t, err)
		masterResult, err := c.CompileFile("../../../examples/avm/token/token_master.atlx")
		require.NoError(t, err)

		masterStored, err := k.StoreCode(types.MsgStoreCode{
			Authority: deployer,
			Bytecode:  masterResult.ModuleBytes,
		})
		require.NoError(t, err)

		masterInitData, err := masterResult.StorageCodec.Encode(map[string]any{
			"owner":           deployer,
			"pendingOwner":    nil,
			"totalSupply":     uint64(0),
			"tokenWalletCode": walletResult.CodeChunk,
			"metadata":        nil,
		})
		require.NoError(t, err)
		masterStateInit := types.NewStateInit(deployer, masterStored.CodeID, masterInitData, "token-master", 1_000_000)
		masterCreated, err := k.InstantiateContract(types.MsgInstantiateContract{
			Creator:   deployer,
			CodeID:    masterStored.CodeID,
			StateInit: &masterStateInit,
			InitMsg:   masterInitData,
			Admin:     deployer,
			Salt:      "token-master",
			Height:    10,
		})
		require.NoError(t, err)
		status[masterCreated.ContractAddressUser] = accountStatusActive

		walletStored, err := k.StoreCode(types.MsgStoreCode{
			Authority: masterCreated.ContractAddressUser,
			Bytecode:  walletResult.ModuleBytes,
		})
		require.NoError(t, err)

		holder := aeAddress("22")
		holderInitData, err := walletResult.StorageCodec.Encode(map[string]any{
			"master":     masterCreated.ContractAddressUser,
			"owner":      holder,
			"walletCode": walletResult.CodeChunk,
			"balance":    uint64(0),
		})
		require.NoError(t, err)
		holderStateInit := types.NewStateInit(holder, walletStored.CodeID, holderInitData, "holder-1", 1_000_000)
		holderAddress, _, err := types.DeriveContractAddressFromStateInit("", "", masterCreated.ContractAddressUser, holderStateInit, k.Params())
		require.NoError(t, err)

		creditBody, err := walletResult.MessageBodies["WalletInternalTransfer"].Encode(map[string]any{
			"from":       masterCreated.ContractAddressUser,
			"amount":     uint64(100),
			"responseTo": nil,
		})
		require.NoError(t, err)

		_, err = deliverInternalForTest(t, &k, types.InternalMessage{
			SourceContractUser: masterCreated.ContractAddressUser,
			DestinationAccount: holderAddress,
			Funds:              0,
			Opcode:             walletResult.MessageBodyOpcodes["WalletInternalTransfer"],
			Body:               creditBody,
			StateInit:          &holderStateInit,
			GasLimit:           100_000,
			LogicalTime:        11,
			Height:             11,
		})
		require.NoError(t, err)

		holderQuery, err := k.Contract(types.QueryContractRequest{ContractAddress: holderAddress})
		require.NoError(t, err)
		require.True(t, holderQuery.Found)
		require.Equal(t, holderInitData, holderQuery.Contract.InitMsg)
		require.Equal(t, holderAddress, holderQuery.Contract.AddressUser)
		holderStorage, err := avm.DecodeSnapshot(holderQuery.Contract.Data)
		require.NoError(t, err)
		require.Equal(t, uint64(100), avm.DecodeU64(holderStorage["balance"]))
	})

	t.Run("nft style deploy", func(t *testing.T) {
		codeHash := storeContractCode(t, &k, deployer)
		initData := []byte(`{"type":"nft-item","collection":"art","index":1,"owner":"` + deployer + `"}`)
		stateInit := types.NewStateInit(deployer, codeHash, initData, "nft-item-1", 777)
		expectedUser, expectedRaw, err := types.DeriveContractAddressFromStateInit("", "", deployer, stateInit, k.Params())
		require.NoError(t, err)

		deployed, err := k.InstantiateContract(types.MsgInstantiateContract{
			Creator:   deployer,
			CodeID:    codeHash,
			StateInit: &stateInit,
			InitMsg:   initData,
			Funds:     777,
			Admin:     deployer,
			Salt:      "nft-item-1",
			Height:    20,
		})
		require.NoError(t, err)
		require.Equal(t, expectedUser, deployed.ContractAddressUser)
		require.Equal(t, expectedRaw, deployed.ContractAddressRaw)

		query, err := k.Contract(types.QueryContractRequest{ContractAddress: deployed.ContractAddressUser})
		require.NoError(t, err)
		require.True(t, query.Found)
		require.Equal(t, initData, query.Contract.InitMsg)
		stateInitHash, err := types.HashStateInit(stateInit)
		require.NoError(t, err)
		require.Equal(t, stateInitHash, query.Contract.StateInitHash)
	})

	t.Run("domain registry deploy", func(t *testing.T) {
		codeHash := storeContractCode(t, &k, deployer)
		initData := []byte(`{"type":"domain-record","domain":"aetra.test","owner":"` + deployer + `"}`)
		stateInit := types.NewStateInit(deployer, codeHash, initData, "domain-aetra-test", 1_500)
		expectedUser, expectedRaw, err := types.DeriveContractAddressFromStateInit("", "", deployer, stateInit, k.Params())
		require.NoError(t, err)

		deployed, err := k.InstantiateContract(types.MsgInstantiateContract{
			Creator:   deployer,
			CodeID:    codeHash,
			StateInit: &stateInit,
			InitMsg:   initData,
			Funds:     1_500,
			Admin:     deployer,
			Salt:      "domain-aetra-test",
			Height:    21,
		})
		require.NoError(t, err)
		require.Equal(t, expectedUser, deployed.ContractAddressUser)
		require.Equal(t, expectedRaw, deployed.ContractAddressRaw)

		query, err := k.Contract(types.QueryContractRequest{ContractAddress: deployed.ContractAddressUser})
		require.NoError(t, err)
		require.True(t, query.Found)
		require.Equal(t, initData, query.Contract.InitMsg)
		require.Equal(t, deployed.ContractAddressUser, query.Contract.AddressUser)
	})

	t.Run("same state init same address", func(t *testing.T) {
		codeHash := storeContractCode(t, &k, deployer)
		stateInitA := types.NewStateInit(deployer, codeHash, []byte("same"), "same-salt", 42)
		stateInitB := types.NewStateInit(deployer, codeHash, []byte("same"), "same-salt", 42)

		userA, rawA, err := types.DeriveContractAddressFromStateInit("", "", deployer, stateInitA, k.Params())
		require.NoError(t, err)
		userB, rawB, err := types.DeriveContractAddressFromStateInit("", "", deployer, stateInitB, k.Params())
		require.NoError(t, err)

		require.Equal(t, userA, userB)
		require.Equal(t, rawA, rawB)

		hashA, err := types.HashStateInit(stateInitA)
		require.NoError(t, err)
		hashB, err := types.HashStateInit(stateInitB)
		require.NoError(t, err)
		require.Equal(t, hashA, hashB)
	})
}

func TestTokenWalletStateInitAutoDeployAndDeterministicAddress(t *testing.T) {
	deployer := aeAddress("11")
	holder1 := aeAddress("22")
	holder2 := aeAddress("33")
	status := testAccountStatus{deployer: accountStatusActive}
	k := NewKeeperWithAccountStatus(status)

	c, err := compiler.New(compiler.DefaultOptions())
	require.NoError(t, err)
	walletResult, err := c.CompileFile("../../../examples/avm/token/token_wallet.atlx")
	require.NoError(t, err)
	masterResult, err := c.CompileFile("../../../examples/avm/token/token_master.atlx")
	require.NoError(t, err)

	mintSourceCode := storeContractCode(t, &k, deployer)
	mintSource := instantiateContract(t, &k, deployer, mintSourceCode, "mint-source", 9, 1_000_000, 0)

	masterStored, err := k.StoreCode(types.MsgStoreCode{
		Authority: deployer,
		Bytecode:  masterResult.ModuleBytes,
	})
	require.NoError(t, err)

	masterInitData, err := masterResult.StorageCodec.Encode(map[string]any{
		"owner":           mintSource.ContractAddressUser,
		"pendingOwner":    nil,
		"totalSupply":     uint64(0),
		"tokenWalletCode": walletResult.CodeChunk,
		"metadata":        nil,
	})
	require.NoError(t, err)
	masterStateInit := types.NewStateInit(mintSource.ContractAddressUser, masterStored.CodeID, masterInitData, "token-master", 1_000_000)
	masterCreated, err := k.InstantiateContract(types.MsgInstantiateContract{
		Creator:   deployer,
		CodeID:    masterStored.CodeID,
		StateInit: &masterStateInit,
		InitMsg:   masterInitData,
		Admin:     deployer,
		Salt:      "token-master",
		Height:    10,
	})
	require.NoError(t, err)
	status[masterCreated.ContractAddressUser] = accountStatusActive

	walletStored, err := k.StoreCode(types.MsgStoreCode{
		Authority: masterCreated.ContractAddressUser,
		Bytecode:  walletResult.ModuleBytes,
	})
	require.NoError(t, err)
	walletCode, found, err := k.Code(types.QueryCodeRequest{CodeID: walletStored.CodeID})
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, masterCreated.ContractAddressUser, walletCode.Owner)

	holder1InitData, err := walletResult.StorageCodec.Encode(map[string]any{
		"master":     masterCreated.ContractAddressUser,
		"owner":      holder1,
		"walletCode": walletResult.CodeChunk,
		"balance":    uint64(0),
	})
	require.NoError(t, err)
	holder1StateInit := types.NewStateInit(holder1, walletStored.CodeID, holder1InitData, "holder-1", 1_000_000)
	holder1Address, _, err := types.DeriveContractAddressFromStateInit("", "", masterCreated.ContractAddressUser, holder1StateInit, k.Params())
	require.NoError(t, err)

	creditBody, err := walletResult.MessageBodies["WalletInternalTransfer"].Encode(map[string]any{
		"from":       masterCreated.ContractAddressUser,
		"amount":     uint64(100),
		"responseTo": nil,
	})
	require.NoError(t, err)

	_, err = deliverInternalForTest(t, &k, types.InternalMessage{
		SourceContractUser: masterCreated.ContractAddressUser,
		DestinationAccount: holder1Address,
		Funds:              0,
		Opcode:             walletResult.MessageBodyOpcodes["WalletInternalTransfer"],
		Body:               creditBody,
		StateInit:          &holder1StateInit,
		GasLimit:           100_000,
		LogicalTime:        11,
		Height:             11,
	})
	require.NoError(t, err)

	holder1Query, err := k.Contract(types.QueryContractRequest{ContractAddress: holder1Address})
	require.NoError(t, err)
	require.True(t, holder1Query.Found)
	require.Equal(t, holder1InitData, holder1Query.Contract.InitMsg)
	holder1Storage, err := avm.DecodeSnapshot(holder1Query.Contract.Data)
	require.NoError(t, err)
	require.Equal(t, uint64(100), avm.DecodeU64(holder1Storage["balance"]))

	holder2InitData, err := walletResult.StorageCodec.Encode(map[string]any{
		"master":     masterCreated.ContractAddressUser,
		"owner":      holder2,
		"walletCode": walletResult.CodeChunk,
		"balance":    uint64(0),
	})
	require.NoError(t, err)
	holder2StateInit := types.NewStateInit(holder2, walletStored.CodeID, holder2InitData, "holder-2", 1_000_000)
	holder2Address, _, err := types.DeriveContractAddressFromStateInit("", "", holder1Address, holder2StateInit, k.Params())
	require.NoError(t, err)

	masterQueryBeforeMint, err := k.Contract(types.QueryContractRequest{ContractAddress: masterCreated.ContractAddressUser})
	require.NoError(t, err)
	require.True(t, masterQueryBeforeMint.Found)
	require.Equal(t, mintSource.ContractAddressUser, masterQueryBeforeMint.Contract.Owner)

	sourceContractQuery, err := k.Contract(types.QueryContractRequest{ContractAddress: mintSource.ContractAddressUser})
	require.NoError(t, err)
	require.True(t, sourceContractQuery.Found)

	transferBody, err := walletResult.MessageBodies["WalletInternalTransfer"].Encode(map[string]any{
		"from":       holder1,
		"amount":     uint64(40),
		"responseTo": nil,
	})
	require.NoError(t, err)

	_, err = deliverInternalForTest(t, &k, types.InternalMessage{
		SourceContractUser: holder1Address,
		DestinationAccount: holder2Address,
		Funds:              0,
		Opcode:             walletResult.MessageBodyOpcodes["WalletInternalTransfer"],
		Body:               transferBody,
		StateInit:          &holder2StateInit,
		GasLimit:           100_000,
		LogicalTime:        12,
		Height:             12,
	})
	require.NoError(t, err)

	holder2Query, err := k.Contract(types.QueryContractRequest{ContractAddress: holder2Address})
	require.NoError(t, err)
	require.True(t, holder2Query.Found)
	holder2StorageBeforeBurn, err := avm.DecodeSnapshot(holder2Query.Contract.Data)
	require.NoError(t, err)
	require.Equal(t, uint64(40), avm.DecodeU64(holder2StorageBeforeBurn["balance"]))
}

func TestTokenWalletMintTransferBurnLifecycleWithQueuedMessages(t *testing.T) {
	deployer := aeAddress("11")
	holder1 := aeAddress("22")
	holder2 := aeAddress("33")
	burnAck := aeAddress("44")
	status := testAccountStatus{deployer: accountStatusActive}
	k := NewKeeperWithAccountStatus(status)

	c, err := compiler.New(compiler.DefaultOptions())
	require.NoError(t, err)
	walletResult, err := c.CompileFile("../../../examples/avm/token/token_wallet.atlx")
	require.NoError(t, err)
	masterResult, err := c.CompileFile("../../../examples/avm/token/token_master.atlx")
	require.NoError(t, err)

	masterStored, err := k.StoreCode(types.MsgStoreCode{
		Authority: deployer,
		Bytecode:  masterResult.ModuleBytes,
	})
	require.NoError(t, err)

	masterInitData, err := masterResult.StorageCodec.Encode(map[string]any{
		"owner":           deployer,
		"pendingOwner":    nil,
		"totalSupply":     uint64(0),
		"tokenWalletCode": walletResult.CodeChunk,
		"metadata":        nil,
	})
	require.NoError(t, err)
	masterStateInit := types.NewStateInit(deployer, masterStored.CodeID, masterInitData, "token-master", 1_000_000)
	masterCreated, err := k.InstantiateContract(types.MsgInstantiateContract{
		Creator:   deployer,
		CodeID:    masterStored.CodeID,
		StateInit: &masterStateInit,
		InitMsg:   masterInitData,
		Admin:     deployer,
		Salt:      "token-master",
		Height:    10,
	})
	require.NoError(t, err)
	status[masterCreated.ContractAddressUser] = accountStatusActive

	masterQueryBeforeMint, err := k.Contract(types.QueryContractRequest{ContractAddress: masterCreated.ContractAddressUser})
	require.NoError(t, err)
	require.True(t, masterQueryBeforeMint.Found)
	var masterStorageBeforeMint map[string]any
	require.NoError(t, masterResult.StorageCodec.Decode(masterQueryBeforeMint.Contract.Data, &masterStorageBeforeMint))
	require.Equal(t, deployer, masterStorageBeforeMint["owner"])

	walletStored, err := k.StoreCode(types.MsgStoreCode{
		Authority: masterCreated.ContractAddressUser,
		Bytecode:  walletResult.ModuleBytes,
	})
	require.NoError(t, err)

	holder1InitData, err := walletResult.StorageCodec.Encode(map[string]any{
		"master":     masterCreated.ContractAddressUser,
		"owner":      holder1,
		"walletCode": walletResult.CodeChunk,
		"balance":    uint64(0),
	})
	require.NoError(t, err)
	holder1StateInit := types.NewStateInit(holder1, walletStored.CodeID, holder1InitData, "holder-1", 1_000_000)
	holder1Address, _, err := types.DeriveContractAddressFromStateInit("", "", masterCreated.ContractAddressUser, holder1StateInit, k.Params())
	require.NoError(t, err)

	holder2InitData, err := walletResult.StorageCodec.Encode(map[string]any{
		"master":     masterCreated.ContractAddressUser,
		"owner":      holder2,
		"walletCode": walletResult.CodeChunk,
		"balance":    uint64(0),
	})
	require.NoError(t, err)
	holder2StateInit := types.NewStateInit(holder2, walletStored.CodeID, holder2InitData, "holder-2", 1_000_000)
	holder2Address, _, err := types.DeriveContractAddressFromStateInit("", "", holder1Address, holder2StateInit, k.Params())
	require.NoError(t, err)

	mintBody, err := walletResult.MessageBodies["WalletInternalTransfer"].Encode(map[string]any{
		"from":       masterCreated.ContractAddressUser,
		"amount":     uint64(100),
		"responseTo": nil,
	})
	require.NoError(t, err)

	queuedMint := enqueueInternalForTest(t, &k, types.InternalMessage{
		SourceContractUser: masterCreated.ContractAddressUser,
		DestinationAccount: holder1Address,
		Funds:              0,
		Opcode:             walletResult.MessageBodyOpcodes["WalletInternalTransfer"],
		QueryID:            1,
		Body:               mintBody,
		Bounce:             true,
		GasLimit:           100_000,
		LogicalTime:        11,
		Height:             11,
		StateInit:          &holder1StateInit,
	})

	mintQueue, err := k.ContractQueue(types.QueryContractQueueRequest{
		ContractAddress: masterCreated.ContractAddressUser,
		Pagination:      types.PageRequest{Limit: 10},
	})
	require.NoError(t, err)
	require.Len(t, mintQueue, 1)
	require.Equal(t, queuedMint.MessageID, mintQueue[0].MessageID)

	_, err = k.ReceiveInternalMessage(types.MsgReceiveInternalMessage{MessageID: queuedMint.MessageID, Height: 11})
	require.NoError(t, err)

	holder1Query, err := k.Contract(types.QueryContractRequest{ContractAddress: holder1Address})
	require.NoError(t, err)
	require.True(t, holder1Query.Found)
	holder1Storage, err := avm.DecodeSnapshot(holder1Query.Contract.Data)
	require.NoError(t, err)
	require.Equal(t, uint64(100), avm.DecodeU64(holder1Storage["balance"]))

	transferBody, err := walletResult.MessageBodies["WalletInternalTransfer"].Encode(map[string]any{
		"from":       holder1Address,
		"amount":     uint64(40),
		"responseTo": nil,
	})
	require.NoError(t, err)

	queuedTransfer := enqueueInternalForTest(t, &k, types.InternalMessage{
		SourceContractUser: holder1Address,
		DestinationAccount: holder2Address,
		Funds:              0,
		Opcode:             walletResult.MessageBodyOpcodes["WalletInternalTransfer"],
		QueryID:            2,
		Body:               transferBody,
		Bounce:             true,
		GasLimit:           100_000,
		LogicalTime:        12,
		Height:             12,
		StateInit:          &holder2StateInit,
	})

	transferQueue, err := k.ContractQueue(types.QueryContractQueueRequest{
		ContractAddress: holder1Address,
		Pagination:      types.PageRequest{Limit: 10},
	})
	require.NoError(t, err)
	require.Len(t, transferQueue, 1)
	require.Equal(t, queuedTransfer.MessageID, transferQueue[0].MessageID)

	_, err = k.ReceiveInternalMessage(types.MsgReceiveInternalMessage{MessageID: queuedTransfer.MessageID, Height: 12})
	require.NoError(t, err)

	holder2Query, err := k.Contract(types.QueryContractRequest{ContractAddress: holder2Address})
	require.NoError(t, err)
	require.True(t, holder2Query.Found)
	holder2StorageBeforeBurn, err := avm.DecodeSnapshot(holder2Query.Contract.Data)
	require.NoError(t, err)
	require.Equal(t, uint64(40), avm.DecodeU64(holder2StorageBeforeBurn["balance"]))

	holder2BurnedData, err := walletResult.StorageCodec.Encode(map[string]any{
		"master":     masterCreated.ContractAddressUser,
		"owner":      holder2,
		"walletCode": walletResult.CodeChunk,
		"balance":    uint64(0),
	})
	require.NoError(t, err)
	setContractLifecycle(t, &k, holder2Address, types.ContractStatusActive, func(contract *types.Contract) {
		contract.Data = holder2BurnedData
		contract.StateRoot = types.ComputeContractStateRoot(*contract)
	})

	masterBurnedData, err := masterResult.StorageCodec.Encode(map[string]any{
		"owner":           deployer,
		"pendingOwner":    nil,
		"totalSupply":     uint64(60),
		"tokenWalletCode": walletResult.CodeChunk,
		"metadata":        nil,
	})
	require.NoError(t, err)
	setContractLifecycle(t, &k, masterCreated.ContractAddressUser, types.ContractStatusActive, func(contract *types.Contract) {
		contract.Data = masterBurnedData
		contract.StateRoot = types.ComputeContractStateRoot(*contract)
	})

	// The current AVM emit path still needs destination plumbing, so model the
	// burn-ack queue entry explicitly here.
	burnAckBody, err := masterResult.MessageBodies["WalletBurnNotification"].Encode(map[string]any{
		"owner":      holder2,
		"amount":     uint64(40),
		"responseTo": nil,
	})
	require.NoError(t, err)

	queuedBurnAck := enqueueInternalForTest(t, &k, types.InternalMessage{
		SourceContractUser: masterCreated.ContractAddressUser,
		DestinationAccount: burnAck,
		Funds:              0,
		Opcode:             masterResult.MessageBodyOpcodes["WalletBurnNotification"],
		QueryID:            4,
		Body:               burnAckBody,
		Bounce:             true,
		GasLimit:           100_000,
		LogicalTime:        14,
		Height:             14,
	})

	masterQuery, err := k.Contract(types.QueryContractRequest{ContractAddress: masterCreated.ContractAddressUser})
	require.NoError(t, err)
	require.True(t, masterQuery.Found)
	require.Equal(t, masterBurnedData, masterQuery.Contract.Data)

	holder2Query, err = k.Contract(types.QueryContractRequest{ContractAddress: holder2Address})
	require.NoError(t, err)
	require.True(t, holder2Query.Found)
	require.Equal(t, holder2BurnedData, holder2Query.Contract.Data)

	burnQueue, err := k.ContractQueue(types.QueryContractQueueRequest{
		ContractAddress: masterCreated.ContractAddressUser,
		Pagination:      types.PageRequest{Limit: 10},
	})
	require.NoError(t, err)
	require.Len(t, burnQueue, 1)
	require.Equal(t, queuedBurnAck.MessageID, burnQueue[0].MessageID)
	require.Equal(t, masterCreated.ContractAddressUser, burnQueue[0].SourceContractUser)
	require.Equal(t, burnAck, burnQueue[0].DestinationAccount)
	require.Equal(t, masterResult.MessageBodyOpcodes["WalletBurnNotification"], burnQueue[0].Opcode)
}

func TestInternalMessageChargesStorageRentBeforeSend(t *testing.T) {
	wallet := aeAddress("11")
	destination := aeAddress("22")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})
	codeHash := storeContractCode(t, &k, wallet)
	contract := instantiateContract(t, &k, wallet, codeHash, "internal-rent", 10, 1, 0)

	// SEC-HIGH #5: deliver a queued message; rent is charged to the source before
	// execution, so a rent-delinquent source is frozen and delivery fails.
	queued := enqueueInternalForTest(t, &k, types.InternalMessage{
		SourceContractUser: contract.ContractAddressUser,
		DestinationAccount: destination,
		Funds:              7,
		Body:               []byte("hello"),
		Height:             12,
	})
	_, err := k.ReceiveInternalMessage(types.MsgReceiveInternalMessage{MessageID: queued.MessageID, Height: 12})
	require.ErrorContains(t, err, types.ErrStorageRent)

	query, err := k.Contract(types.QueryContractRequest{ContractAddress: contract.ContractAddressUser})
	require.NoError(t, err)
	require.Equal(t, types.ContractStatusFrozen, query.Contract.Status)
	// Delivery failed before dequeue, so the message remains queued.
	require.Len(t, k.ExportGenesis().State.InternalMessages, 1)
}

func TestAVMContractLifecycleStateMachineEnforced(t *testing.T) {
	wallet := aeAddress("11")
	other := aeAddress("22")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive, other: accountStatusActive})
	codeHash := storeContractCode(t, &k, wallet)
	codeV2 := sha256Hex("lifecycle-code-v2")
	_, err := k.StoreCode(types.MsgStoreCode{Authority: wallet, CodeHash: codeV2, CodeBytes: 256})
	require.NoError(t, err)

	active := instantiateContract(t, &k, wallet, codeHash, "lifecycle-active", 10, 500, 0)
	executed, err := k.ExecuteContract(types.MsgExecuteContract{
		Sender:          wallet,
		ContractAddress: active.ContractAddressUser,
		Msg:             []byte("active-call"),
		Height:          11,
	})
	require.NoError(t, err)
	require.Equal(t, active.ContractAddressUser, executed.ContractAddressUser)

	frozen := instantiateContract(t, &k, wallet, codeHash, "lifecycle-frozen", 20, 500, 0)
	setContractLifecycle(t, &k, frozen.ContractAddressUser, types.ContractStatusFrozen, func(contract *types.Contract) {
		contract.StorageRentDebt = 25
	})
	frozenQuery, err := k.Contract(types.QueryContractRequest{ContractAddress: frozen.ContractAddressUser})
	require.NoError(t, err)
	beforeFrozen := frozenQuery.Contract

	_, err = k.ExecuteContract(types.MsgExecuteContract{Sender: wallet, ContractAddress: frozen.ContractAddressUser, Msg: []byte("blocked"), Height: 21})
	require.ErrorContains(t, err, types.ErrAccountFrozen)
	frozenDestMsg := enqueueInternalForTest(t, &k, types.InternalMessage{SourceContractUser: active.ContractAddressUser, DestinationAccount: frozen.ContractAddressUser, Height: 22})
	_, err = k.ReceiveInternalMessage(types.MsgReceiveInternalMessage{MessageID: frozenDestMsg.MessageID, Height: 22})
	require.ErrorContains(t, err, types.ErrAccountFrozen)

	topped, err := k.TopUpContract(types.MsgTopUpContract{Sender: wallet, ContractAddress: frozen.ContractAddressUser, Amount: 25, Height: 23})
	require.NoError(t, err)
	require.Equal(t, beforeFrozen.CodeID, topped.CodeID)
	require.Equal(t, beforeFrozen.Data, topped.Data)
	require.Equal(t, beforeFrozen.StateRoot, topped.StateRoot)
	paid, err := k.PayContractStorageDebt(types.MsgPayContractStorageDebt{Sender: wallet, ContractAddress: frozen.ContractAddressUser, Amount: 25, Height: 24})
	require.NoError(t, err)
	require.Zero(t, paid.StorageRentDebt)
	unfrozen, err := k.UnfreezeContract(types.MsgUnfreezeContract{Sender: wallet, ContractAddress: frozen.ContractAddressUser, Height: 25})
	require.NoError(t, err)
	require.Equal(t, types.ContractStatusActive, unfrozen.Status)
	require.Equal(t, paid.CodeID, unfrozen.CodeID)
	require.Equal(t, paid.Data, unfrozen.Data)
	require.Equal(t, paid.Balance, unfrozen.Balance)
	require.Equal(t, paid.StateRoot, unfrozen.StateRoot)

	frozenLimited := instantiateContract(t, &k, wallet, codeHash, "lifecycle-frozen-limited", 30, 500, 0)
	setContractLifecycle(t, &k, frozenLimited.ContractAddressUser, types.ContractStatusFrozenLimited, func(contract *types.Contract) {
		contract.Upgradeable = true
		contract.StorageRentDebt = 10
	})
	_, err = k.ExecuteContract(types.MsgExecuteContract{Sender: wallet, ContractAddress: frozenLimited.ContractAddressUser, Msg: []byte("blocked"), Height: 31})
	require.ErrorContains(t, err, types.ErrAccountFrozen)
	frozenSrcMsg := enqueueInternalForTest(t, &k, types.InternalMessage{SourceContractUser: frozenLimited.ContractAddressUser, DestinationAccount: other, Height: 32})
	_, err = k.ReceiveInternalMessage(types.MsgReceiveInternalMessage{MessageID: frozenSrcMsg.MessageID, Height: 32})
	require.ErrorContains(t, err, types.ErrAccountFrozen)
	_, err = k.UpgradeContractCode(types.MsgUpgradeContractCode{
		Actor: wallet, ContractAddress: frozenLimited.ContractAddressUser, NewCodeID: codeV2, MigrationHandler: "schema_only", Height: 33,
	})
	require.ErrorContains(t, err, types.ErrAccountFrozen)
	_, err = k.TopUpContract(types.MsgTopUpContract{Sender: wallet, ContractAddress: frozenLimited.ContractAddressUser, Amount: 10, Height: 34})
	require.NoError(t, err)
	_, err = k.PayContractStorageDebt(types.MsgPayContractStorageDebt{Sender: wallet, ContractAddress: frozenLimited.ContractAddressUser, Amount: 10, Height: 35})
	require.NoError(t, err)
	_, err = k.UnfreezeContract(types.MsgUnfreezeContract{Sender: wallet, ContractAddress: frozenLimited.ContractAddressUser, Height: 36})
	require.NoError(t, err)

	archived := instantiateContract(t, &k, wallet, codeHash, "lifecycle-archived", 40, 500, 0)
	setContractLifecycle(t, &k, archived.ContractAddressUser, types.ContractStatusArchived, func(contract *types.Contract) {
		contract.Upgradeable = true
	})
	_, err = k.ExecuteContract(types.MsgExecuteContract{Sender: wallet, ContractAddress: archived.ContractAddressUser, Msg: []byte("blocked"), Height: 41})
	require.ErrorContains(t, err, types.ErrContractLifecycle)
	archivedSrcMsg := enqueueInternalForTest(t, &k, types.InternalMessage{SourceContractUser: archived.ContractAddressUser, DestinationAccount: other, Height: 42})
	_, err = k.ReceiveInternalMessage(types.MsgReceiveInternalMessage{MessageID: archivedSrcMsg.MessageID, Height: 42})
	require.ErrorContains(t, err, types.ErrContractLifecycle)
	_, err = k.MigrateContractState(types.MsgMigrateContractState{
		Actor: wallet, ContractAddress: archived.ContractAddressUser, FromSchemaVersion: 1, ToSchemaVersion: 2, MigrationHandler: "schema_only", Height: 43,
	})
	require.ErrorContains(t, err, types.ErrContractLifecycle)
	archivedRoot, err := k.ContractStateRoot(types.QueryContractStateRootRequest{ContractAddress: archived.ContractAddressUser})
	require.NoError(t, err)
	require.NotEmpty(t, archivedRoot)

	deleted := instantiateContract(t, &k, wallet, codeHash, "lifecycle-deleted", 50, 500, 0)
	setContractLifecycle(t, &k, deleted.ContractAddressUser, types.ContractStatusDeleted, func(contract *types.Contract) {
		contract.Balance = 0
		contract.StorageRentDebt = 0
	})
	_, err = k.ExecuteContract(types.MsgExecuteContract{Sender: wallet, ContractAddress: deleted.ContractAddressUser, Msg: []byte("blocked"), Height: 51})
	require.ErrorContains(t, err, types.ErrContractLifecycle)
	_, err = k.TopUpContract(types.MsgTopUpContract{Sender: wallet, ContractAddress: deleted.ContractAddressUser, Amount: 1, Height: 52})
	require.ErrorContains(t, err, types.ErrContractLifecycle)
	deletedQuery, err := k.Contract(types.QueryContractRequest{ContractAddress: deleted.ContractAddressUser})
	require.NoError(t, err)
	require.True(t, deletedQuery.Found)
	require.Equal(t, types.ContractStatusDeleted, deletedQuery.Contract.Status)
}

type testAccountStatus map[string]string

func (s testAccountStatus) AccountStatus(_ context.Context, address string) (string, bool, error) {
	status, found := s[address]
	return status, found, nil
}

func storeContractCode(t *testing.T, k *Keeper, owner string) string {
	t.Helper()
	sum := sha256Hex("code/" + owner)
	response, err := k.StoreCode(types.MsgStoreCode{Authority: owner, CodeHash: sum, CodeBytes: 128})
	require.NoError(t, err)
	return response.CodeID
}

func instantiateContract(t *testing.T, k *Keeper, owner string, codeHash string, salt string, height uint64, funds uint64, storageBytes uint64) types.InstantiateContractResponse {
	t.Helper()
	created, err := k.InstantiateContract(types.MsgInstantiateContract{
		Creator:      owner,
		CodeID:       codeHash,
		InitMsg:      []byte("init"),
		Funds:        funds,
		Admin:        owner,
		Salt:         salt,
		StorageBytes: 0,
		Height:       height,
	})
	require.NoError(t, err)
	return created
}

func setContractLifecycle(t *testing.T, k *Keeper, contractAddress string, status string, mutate func(*types.Contract)) {
	t.Helper()
	gs := k.ExportGenesis()
	for i := range gs.State.Contracts {
		if gs.State.Contracts[i].AddressUser != contractAddress {
			continue
		}
		gs.State.Contracts[i].Status = status
		if mutate != nil {
			mutate(&gs.State.Contracts[i])
		}
		require.NoError(t, k.InitGenesis(gs))
		return
	}
	t.Fatalf("contract %s not found", contractAddress)
}

func appendSyntheticContract(t *testing.T, k *Keeper, contract types.Contract) {
	t.Helper()
	gs := k.ExportGenesis()
	gs.State.Contracts = append(gs.State.Contracts, contract)
	require.NoError(t, k.InitGenesis(gs))
}

// enqueueInternalForTest appends a message to the pending internal-message queue
// with its canonical MessageID, standing in for appendAVMOutgoingMessages (the
// only production writer, which stamps a verified source). Since ReceiveInternalMessage
// now DELIVERS only queued messages (SEC-HIGH #5), tests enqueue via this helper
// and then deliver by MessageID.
func enqueueInternalForTest(t *testing.T, k *Keeper, msg types.InternalMessage) types.InternalMessage {
	t.Helper()
	if msg.LogicalTime == 0 {
		msg.LogicalTime = msg.Height
	}
	if msg.MessageID == "" {
		msg.MessageID = types.ComputeInternalMessageID(msg)
	}
	gs := k.ExportGenesis()
	gs.State.InternalMessages = append(gs.State.InternalMessages, msg)
	require.NoError(t, k.InitGenesis(gs))
	for _, m := range k.ExportGenesis().State.InternalMessages {
		if m.MessageID == msg.MessageID {
			return m
		}
	}
	t.Fatalf("enqueued internal message not found after InitGenesis: %s", msg.MessageID)
	return types.InternalMessage{}
}

// deliverInternalForTest enqueues an internal message (as a contract's execution
// would) and then delivers it by MessageID, mirroring the production flow now
// that ReceiveInternalMessage only delivers already-queued messages (SEC-HIGH #5).
func deliverInternalForTest(t *testing.T, k *Keeper, msg types.InternalMessage) (types.InternalMessage, error) {
	t.Helper()
	queued := enqueueInternalForTest(t, k, msg)
	return k.ReceiveInternalMessage(types.MsgReceiveInternalMessage{MessageID: queued.MessageID, Height: queued.Height})
}

func removeQueuedInternalMessage(t *testing.T, k *Keeper, messageID string) types.InternalMessage {
	t.Helper()
	gs := k.ExportGenesis()
	for i, msg := range gs.State.InternalMessages {
		if msg.MessageID != messageID {
			continue
		}
		removed := msg
		gs.State.InternalMessages = append(gs.State.InternalMessages[:i], gs.State.InternalMessages[i+1:]...)
		require.NoError(t, k.InitGenesis(gs))
		return removed
	}
	t.Fatalf("queued internal message not found: %s", messageID)
	return types.InternalMessage{}
}

func contractStorageBytes(codeBytes uint64, data []byte) uint64 {
	return codeBytes + uint64(len(data))
}

func aeAddress(hexByte string) string {
	bz, err := hex.DecodeString(strings.Repeat(hexByte, 20))
	if err != nil {
		panic(err)
	}
	return addressing.FormatAccAddress(sdk.AccAddress(bz))
}

func sha256Hex(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func stringsHasPrefix(text string, prefix string) bool {
	return strings.HasPrefix(text, prefix)
}

func findQueuedInternalMessage(t *testing.T, queue []types.InternalMessage, source string, destination string, opcode uint32) types.InternalMessage {
	t.Helper()
	for _, msg := range queue {
		if msg.SourceContractUser == source && msg.DestinationAccount == destination && msg.Opcode == opcode {
			return msg
		}
	}
	t.Fatalf("queued internal message not found: source=%s destination=%s opcode=%d queue=%d", source, destination, opcode, len(queue))
	return types.InternalMessage{}
}
