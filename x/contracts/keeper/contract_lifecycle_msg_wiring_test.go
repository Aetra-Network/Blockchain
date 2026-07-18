package keeper

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/contracts/types"
)

// TestContractLifecycleMsgsLiveReachableThroughGRPCMsgServer is the live-path
// regression guard described in app/avm_runtime_system_test.go's comment on
// MigrateContractState ("has no live Msg route... only mutates the keeper's
// in-memory genesis"): UpgradeContractCode, MigrateContractState,
// SetContractAdmin, and DisableContractUpgrades had correct, already
// unit-tested keeper logic, but no gRPC msg-server route and no Msg wire
// format -- so no live transaction could ever reach them. This drives each one
// through NewGRPCMsgServer, the same entry point RegisterServices wires to
// baseapp's MsgServiceRouter, rather than calling the keeper method directly.
func TestContractLifecycleMsgsLiveReachableThroughGRPCMsgServer(t *testing.T) {
	ctx := context.Background()
	wallet := aeAddress("11")
	admin := aeAddress("22")
	other := aeAddress("33")
	newAdmin := aeAddress("44")

	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive, admin: accountStatusActive, other: accountStatusActive, newAdmin: accountStatusActive})
	codeV1 := storeContractCode(t, &k, wallet)
	codeV2Hash := sha256Hex("lifecycle-msg-wiring-code-v2")
	_, err := k.StoreCode(types.MsgStoreCode{Authority: wallet, CodeHash: codeV2Hash, CodeBytes: 256})
	require.NoError(t, err)

	deployed, err := k.InstantiateContract(types.MsgInstantiateContract{
		Creator: wallet, CodeID: codeV1, InitMsg: []byte("v1"), Admin: admin, Salt: "grpc-wiring", Upgradeable: true, SchemaVersion: 1, Height: 20,
	})
	require.NoError(t, err)
	contractAddr := deployed.ContractAddressUser

	msgServer := NewGRPCMsgServer(&k)

	// UpgradeContractCode: a non-admin actor must be rejected -- the
	// authorization model (authorizeContractUpgradeActor) must still hold when
	// invoked through the live Msg route, not just via a direct keeper call.
	_, err = msgServer.UpgradeContractCode(ctx, &types.MsgUpgradeContractCode{
		Actor: other, ContractAddress: contractAddr, NewCodeID: codeV2Hash, MigrationHandler: "schema_only", Height: 21,
	})
	require.ErrorContains(t, err, types.ErrUnauthorized)

	upgradeResp, err := msgServer.UpgradeContractCode(ctx, &types.MsgUpgradeContractCode{
		Actor: admin, ContractAddress: contractAddr, NewCodeID: codeV2Hash, MigrationHandler: "schema_only", Height: 22,
	})
	require.NoError(t, err)
	require.Equal(t, "upgrade_code", upgradeResp.Receipt.Operation)

	query, err := k.Contract(types.QueryContractRequest{ContractAddress: contractAddr})
	require.NoError(t, err)
	require.Equal(t, codeV2Hash, query.Contract.CodeID)

	// MigrateContractState through the live route.
	migrateResp, err := msgServer.MigrateContractState(ctx, &types.MsgMigrateContractState{
		Actor: admin, ContractAddress: contractAddr, FromSchemaVersion: 1, ToSchemaVersion: 2, MigrationHandler: "append", Payload: []byte(":v2"), Height: 23,
	})
	require.NoError(t, err)
	require.Equal(t, "migrate_state", migrateResp.Receipt.Operation)

	migrated, err := k.Contract(types.QueryContractRequest{ContractAddress: contractAddr})
	require.NoError(t, err)
	require.Equal(t, uint64(2), migrated.Contract.StorageSchemaVersion)
	require.Equal(t, []byte("v1:v2"), migrated.Contract.Data)

	// SetContractAdmin through the live route.
	setAdminResp, err := msgServer.SetContractAdmin(ctx, &types.MsgSetContractAdmin{
		Actor: admin, ContractAddress: contractAddr, NewAdmin: newAdmin, Height: 24,
	})
	require.NoError(t, err)
	require.Equal(t, "set_admin", setAdminResp.Receipt.Operation)

	afterAdminChange, err := k.Contract(types.QueryContractRequest{ContractAddress: contractAddr})
	require.NoError(t, err)
	require.Equal(t, newAdmin, afterAdminChange.Contract.Admin)

	// The old admin must no longer be authorized once the admin has changed.
	_, err = msgServer.SetContractAdmin(ctx, &types.MsgSetContractAdmin{
		Actor: admin, ContractAddress: contractAddr, NewAdmin: admin, Height: 25,
	})
	require.ErrorContains(t, err, types.ErrUnauthorized)

	// DisableContractUpgrades through the live route, by the new admin.
	disableResp, err := msgServer.DisableContractUpgrades(ctx, &types.MsgDisableContractUpgrades{
		Actor: newAdmin, ContractAddress: contractAddr, Height: 26,
	})
	require.NoError(t, err)
	require.Equal(t, "disable_upgrades", disableResp.Receipt.Operation)

	afterDisable, err := k.Contract(types.QueryContractRequest{ContractAddress: contractAddr})
	require.NoError(t, err)
	require.True(t, afterDisable.Contract.UpgradesDisabled)
	require.False(t, afterDisable.Contract.Upgradeable)

	// The lock must hold through the live route too: a subsequent
	// UpgradeContractCode by the (correct, current) admin must now fail.
	_, err = msgServer.UpgradeContractCode(ctx, &types.MsgUpgradeContractCode{
		Actor: newAdmin, ContractAddress: contractAddr, NewCodeID: codeV1, MigrationHandler: "schema_only", Height: 27,
	})
	require.ErrorContains(t, err, "immutable")
}

// TestContractLifecycleMsgsRejectMalformedRequestThroughGRPCMsgServer checks
// the msg-server's own nil-request guard for all four new routes (mirroring
// the guard every other handler in grpc_server.go already has).
func TestContractLifecycleMsgsRejectMalformedRequestThroughGRPCMsgServer(t *testing.T) {
	ctx := context.Background()
	k := NewKeeper()
	msgServer := NewGRPCMsgServer(&k)

	_, err := msgServer.UpgradeContractCode(ctx, nil)
	require.Error(t, err)
	_, err = msgServer.MigrateContractState(ctx, nil)
	require.Error(t, err)
	_, err = msgServer.SetContractAdmin(ctx, nil)
	require.Error(t, err)
	_, err = msgServer.DisableContractUpgrades(ctx, nil)
	require.Error(t, err)
}
