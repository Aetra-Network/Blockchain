package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sovereign-l1/l1/x/contracts/types"
)

// setMinUpgradeDelay overrides the governed MinUpgradeDelay param for a test,
// mirroring the gs := k.ExportGenesis(); mutate; k.InitGenesis(gs) pattern
// setContractLifecycle already uses elsewhere in this package.
func setMinUpgradeDelay(t *testing.T, k *Keeper, delay uint64) {
	t.Helper()
	gs := k.ExportGenesis()
	gs.Params.MinUpgradeDelay = delay
	require.NoError(t, k.InitGenesis(gs))
}

// deployUpgradeableTimelockFixture stores two distinct codes and deploys an
// upgradeable contract on the first, returning the contract address, the
// admin address, and the second code's ID for use as an upgrade target.
func deployUpgradeableTimelockFixture(t *testing.T, k *Keeper, wallet, admin string, height uint64) (contractAddr string, codeV2 string) {
	t.Helper()
	codeV1 := storeContractCode(t, k, wallet)
	codeV2 = sha256Hex("timelock-code-v2/" + wallet)
	_, err := k.StoreCode(types.MsgStoreCode{Authority: wallet, CodeHash: codeV2, CodeBytes: 256})
	require.NoError(t, err)
	deployed, err := k.InstantiateContract(types.MsgInstantiateContract{
		Creator: wallet, CodeID: codeV1, InitMsg: []byte("v1"), Admin: admin, Salt: "timelock-fixture", Upgradeable: true, SchemaVersion: 1, Height: height,
	})
	require.NoError(t, err)
	return deployed.ContractAddressUser, codeV2
}

// TestScheduleContractUpgradeRejectsEarlyApplication proves the core
// timelock property: an upgrade scheduled at height H with MinUpgradeDelay=D
// cannot be applied before H+D.
func TestScheduleContractUpgradeRejectsEarlyApplication(t *testing.T) {
	wallet := aeAddress("51")
	admin := aeAddress("52")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive, admin: accountStatusActive})
	setMinUpgradeDelay(t, &k, 10)

	contractAddr, codeV2 := deployUpgradeableTimelockFixture(t, &k, wallet, admin, 100)

	scheduleResp, err := k.ScheduleContractUpgrade(types.MsgScheduleContractUpgrade{
		Actor: admin, ContractAddress: contractAddr, NewCodeID: codeV2, MigrationHandler: "schema_only", Height: 100,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(110), scheduleResp.EarliestActivationHeight)
	require.Equal(t, "schedule_upgrade", scheduleResp.Receipt.Operation)

	// The scheduled code must not have applied yet.
	query, err := k.Contract(types.QueryContractRequest{ContractAddress: contractAddr})
	require.NoError(t, err)
	require.NotEqual(t, codeV2, query.Contract.CodeID)
	require.True(t, query.Contract.HasPendingUpgrade())
	require.Equal(t, uint64(110), query.Contract.PendingUpgradeEarliestHeight)

	// One block before eligibility: must be rejected.
	_, err = k.ApplyScheduledContractUpgrade(types.MsgApplyScheduledUpgrade{
		Actor: admin, ContractAddress: contractAddr, Height: 109,
	})
	require.Error(t, err)
	require.ErrorContains(t, err, types.ErrUnauthorized)

	// The rejected early application must not have mutated the contract.
	query, err = k.Contract(types.QueryContractRequest{ContractAddress: contractAddr})
	require.NoError(t, err)
	require.NotEqual(t, codeV2, query.Contract.CodeID)
	require.True(t, query.Contract.HasPendingUpgrade())
}

// TestScheduleContractUpgradeAppliesAtEarliestHeight proves the boundary:
// applying exactly at H+D succeeds, and a subsequent apply attempt (no
// pending schedule remaining) is rejected.
func TestScheduleContractUpgradeAppliesAtEarliestHeight(t *testing.T) {
	wallet := aeAddress("53")
	admin := aeAddress("54")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive, admin: accountStatusActive})
	setMinUpgradeDelay(t, &k, 5)

	contractAddr, codeV2 := deployUpgradeableTimelockFixture(t, &k, wallet, admin, 200)

	scheduleResp, err := k.ScheduleContractUpgrade(types.MsgScheduleContractUpgrade{
		Actor: admin, ContractAddress: contractAddr, NewCodeID: codeV2, MigrationHandler: "schema_only", Height: 200,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(205), scheduleResp.EarliestActivationHeight)

	applyResp, err := k.ApplyScheduledContractUpgrade(types.MsgApplyScheduledUpgrade{
		Actor: admin, ContractAddress: contractAddr, Height: 205,
	})
	require.NoError(t, err)
	require.Equal(t, "apply_scheduled_upgrade", applyResp.Operation)

	query, err := k.Contract(types.QueryContractRequest{ContractAddress: contractAddr})
	require.NoError(t, err)
	require.Equal(t, codeV2, query.Contract.CodeID)
	require.False(t, query.Contract.HasPendingUpgrade())
	require.Equal(t, uint64(0), query.Contract.PendingUpgradeEarliestHeight)

	// No pending schedule remains: a second apply must fail.
	_, err = k.ApplyScheduledContractUpgrade(types.MsgApplyScheduledUpgrade{
		Actor: admin, ContractAddress: contractAddr, Height: 206,
	})
	require.ErrorContains(t, err, "no scheduled upgrade")
}

// TestScheduleContractUpgradeZeroDelayAppliesImmediately proves
// MinUpgradeDelay == 0 (the zero-value / pre-feature default) allows apply
// in the same block it was scheduled in -- no accidental off-by-one lockout.
func TestScheduleContractUpgradeZeroDelayAppliesImmediately(t *testing.T) {
	wallet := aeAddress("55")
	admin := aeAddress("56")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive, admin: accountStatusActive})
	setMinUpgradeDelay(t, &k, 0)

	contractAddr, codeV2 := deployUpgradeableTimelockFixture(t, &k, wallet, admin, 300)

	scheduleResp, err := k.ScheduleContractUpgrade(types.MsgScheduleContractUpgrade{
		Actor: admin, ContractAddress: contractAddr, NewCodeID: codeV2, MigrationHandler: "schema_only", Height: 300,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(300), scheduleResp.EarliestActivationHeight)

	_, err = k.ApplyScheduledContractUpgrade(types.MsgApplyScheduledUpgrade{
		Actor: admin, ContractAddress: contractAddr, Height: 300,
	})
	require.NoError(t, err)
}

// TestScheduleContractUpgradeRequiresAdmin mirrors
// TestContractLifecycleMsgsLiveReachableThroughGRPCMsgServer's authorization
// check for the pre-existing four msgs: a non-admin actor cannot schedule
// (or apply) an upgrade.
func TestScheduleContractUpgradeRequiresAdmin(t *testing.T) {
	wallet := aeAddress("57")
	admin := aeAddress("58")
	other := aeAddress("59")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive, admin: accountStatusActive, other: accountStatusActive})
	setMinUpgradeDelay(t, &k, 1)

	contractAddr, codeV2 := deployUpgradeableTimelockFixture(t, &k, wallet, admin, 400)

	_, err := k.ScheduleContractUpgrade(types.MsgScheduleContractUpgrade{
		Actor: other, ContractAddress: contractAddr, NewCodeID: codeV2, MigrationHandler: "schema_only", Height: 400,
	})
	require.ErrorContains(t, err, types.ErrUnauthorized)

	scheduleResp, err := k.ScheduleContractUpgrade(types.MsgScheduleContractUpgrade{
		Actor: admin, ContractAddress: contractAddr, NewCodeID: codeV2, MigrationHandler: "schema_only", Height: 400,
	})
	require.NoError(t, err)

	_, err = k.ApplyScheduledContractUpgrade(types.MsgApplyScheduledUpgrade{
		Actor: other, ContractAddress: contractAddr, Height: scheduleResp.EarliestActivationHeight,
	})
	require.ErrorContains(t, err, types.ErrUnauthorized)
}

// TestScheduleContractUpgradeRescheduleReplacesPending proves that calling
// ScheduleContractUpgrade again before a pending schedule applies replaces
// the prior target and restarts the delay, rather than stacking two
// schedules or being rejected outright.
func TestScheduleContractUpgradeRescheduleReplacesPending(t *testing.T) {
	wallet := aeAddress("60")
	admin := aeAddress("61")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive, admin: accountStatusActive})
	setMinUpgradeDelay(t, &k, 10)

	contractAddr, codeV2 := deployUpgradeableTimelockFixture(t, &k, wallet, admin, 500)
	codeV3 := sha256Hex("timelock-code-v3/" + wallet)
	_, err := k.StoreCode(types.MsgStoreCode{Authority: wallet, CodeHash: codeV3, CodeBytes: 300})
	require.NoError(t, err)

	_, err = k.ScheduleContractUpgrade(types.MsgScheduleContractUpgrade{
		Actor: admin, ContractAddress: contractAddr, NewCodeID: codeV2, MigrationHandler: "schema_only", Height: 500,
	})
	require.NoError(t, err)

	// Re-schedule to a different target at a later height: this must replace
	// the pending codeV2 target with codeV3 and recompute the earliest height
	// from the NEW schedule height, not the original one.
	reschedule, err := k.ScheduleContractUpgrade(types.MsgScheduleContractUpgrade{
		Actor: admin, ContractAddress: contractAddr, NewCodeID: codeV3, MigrationHandler: "schema_only", Height: 503,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(513), reschedule.EarliestActivationHeight)

	// Applying at the ORIGINAL (now-stale) earliest height must still fail:
	// the effective earliest height is the rescheduled one.
	_, err = k.ApplyScheduledContractUpgrade(types.MsgApplyScheduledUpgrade{
		Actor: admin, ContractAddress: contractAddr, Height: 510,
	})
	require.ErrorContains(t, err, types.ErrUnauthorized)

	applyResp, err := k.ApplyScheduledContractUpgrade(types.MsgApplyScheduledUpgrade{
		Actor: admin, ContractAddress: contractAddr, Height: 513,
	})
	require.NoError(t, err)
	_ = applyResp

	query, err := k.Contract(types.QueryContractRequest{ContractAddress: contractAddr})
	require.NoError(t, err)
	require.Equal(t, codeV3, query.Contract.CodeID)
}

// TestImmediateUpgradeClearsStalePendingSchedule proves that using the
// pre-existing immediate UpgradeContractCode path while a schedule is
// pending clears the stale schedule, so it cannot later be applied as an
// unintended second swap.
func TestImmediateUpgradeClearsStalePendingSchedule(t *testing.T) {
	wallet := aeAddress("62")
	admin := aeAddress("63")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive, admin: accountStatusActive})
	setMinUpgradeDelay(t, &k, 10)

	contractAddr, codeV2 := deployUpgradeableTimelockFixture(t, &k, wallet, admin, 600)
	codeV3 := sha256Hex("timelock-code-v3-immediate/" + wallet)
	_, err := k.StoreCode(types.MsgStoreCode{Authority: wallet, CodeHash: codeV3, CodeBytes: 300})
	require.NoError(t, err)

	_, err = k.ScheduleContractUpgrade(types.MsgScheduleContractUpgrade{
		Actor: admin, ContractAddress: contractAddr, NewCodeID: codeV2, MigrationHandler: "schema_only", Height: 600,
	})
	require.NoError(t, err)

	_, err = k.UpgradeContractCode(types.MsgUpgradeContractCode{
		Actor: admin, ContractAddress: contractAddr, NewCodeID: codeV3, MigrationHandler: "schema_only", Height: 601,
	})
	require.NoError(t, err)

	query, err := k.Contract(types.QueryContractRequest{ContractAddress: contractAddr})
	require.NoError(t, err)
	require.Equal(t, codeV3, query.Contract.CodeID)
	require.False(t, query.Contract.HasPendingUpgrade())

	// The now-cleared schedule for codeV2 must not be applicable later.
	_, err = k.ApplyScheduledContractUpgrade(types.MsgApplyScheduledUpgrade{
		Actor: admin, ContractAddress: contractAddr, Height: 700,
	})
	require.ErrorContains(t, err, "no scheduled upgrade")
	query, err = k.Contract(types.QueryContractRequest{ContractAddress: contractAddr})
	require.NoError(t, err)
	require.Equal(t, codeV3, query.Contract.CodeID)
}

// TestScheduleContractUpgradeLiveGRPCRoute mirrors
// TestContractLifecycleMsgsLiveReachableThroughGRPCMsgServer: drives
// ScheduleContractUpgrade and ApplyScheduledUpgrade through NewGRPCMsgServer,
// the entry point RegisterServices wires to baseapp's MsgServiceRouter, not
// the keeper method directly, proving both are genuinely live Msg routes.
func TestScheduleContractUpgradeLiveGRPCRoute(t *testing.T) {
	wallet := aeAddress("64")
	admin := aeAddress("65")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive, admin: accountStatusActive})
	setMinUpgradeDelay(t, &k, 3)

	contractAddr, codeV2 := deployUpgradeableTimelockFixture(t, &k, wallet, admin, 800)
	msgServer := NewGRPCMsgServer(&k)

	// The gRPC layer overwrites msg.Height with the real block height (see
	// blockHeight in grpc_server.go), so the request's own Height field is
	// irrelevant here -- what matters is the ctx-carried height, mirroring
	// x/identity-root's grpc_height_test.go convention.
	scheduleResp, err := msgServer.ScheduleContractUpgrade(sdk.Context{}.WithBlockHeight(800), &types.MsgScheduleContractUpgrade{
		Actor: admin, ContractAddress: contractAddr, NewCodeID: codeV2, MigrationHandler: "schema_only", Height: 800,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(803), scheduleResp.EarliestActivationHeight)

	_, err = msgServer.ApplyScheduledUpgrade(sdk.Context{}.WithBlockHeight(802), &types.MsgApplyScheduledUpgrade{
		Actor: admin, ContractAddress: contractAddr, Height: 802,
	})
	require.Error(t, err)

	applyResp, err := msgServer.ApplyScheduledUpgrade(sdk.Context{}.WithBlockHeight(803), &types.MsgApplyScheduledUpgrade{
		Actor: admin, ContractAddress: contractAddr, Height: 803,
	})
	require.NoError(t, err)
	require.Equal(t, "apply_scheduled_upgrade", applyResp.Receipt.Operation)

	// Nil-request guard, matching every other handler in grpc_server.go.
	ctx := sdk.Context{}.WithBlockHeight(803)
	_, err = msgServer.ScheduleContractUpgrade(ctx, nil)
	require.Error(t, err)
	_, err = msgServer.ApplyScheduledUpgrade(ctx, nil)
	require.Error(t, err)
}

// TestScheduleContractUpgradeCannotBypassDelayViaMsgHeight is the direct
// regression proof for the timelock-bypass bug: ApplyScheduledUpgrade must
// key off the REAL block height carried by ctx, not the caller-supplied
// msg.Height, or any actor could self-report a far-future Height and apply a
// scheduled upgrade in the very next transaction regardless of
// MinUpgradeDelay. Reproduces the exact scenario the bug report described --
// schedule at a low real height, then attempt to apply while lying about
// Height being far in the future -- and asserts it is still rejected because
// the real (ctx) height has not actually advanced.
func TestScheduleContractUpgradeCannotBypassDelayViaMsgHeight(t *testing.T) {
	wallet := aeAddress("70")
	admin := aeAddress("71")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive, admin: accountStatusActive})
	setMinUpgradeDelay(t, &k, 1_000_000)

	contractAddr, codeV2 := deployUpgradeableTimelockFixture(t, &k, wallet, admin, 1)
	msgServer := NewGRPCMsgServer(&k)

	scheduleResp, err := msgServer.ScheduleContractUpgrade(sdk.Context{}.WithBlockHeight(1), &types.MsgScheduleContractUpgrade{
		Actor: admin, ContractAddress: contractAddr, NewCodeID: codeV2, MigrationHandler: "schema_only", Height: 1,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(1_000_001), scheduleResp.EarliestActivationHeight)

	// The real chain has NOT advanced (ctx still reports height 1), but the
	// request lies about Height being far beyond the earliest activation
	// height. Before the fix, ApplyScheduledContractUpgrade trusted
	// msg.Height directly and this would have succeeded instantly.
	_, err = msgServer.ApplyScheduledUpgrade(sdk.Context{}.WithBlockHeight(1), &types.MsgApplyScheduledUpgrade{
		Actor: admin, ContractAddress: contractAddr, Height: 1_000_001,
	})
	require.Error(t, err)
	require.ErrorContains(t, err, types.ErrUnauthorized)

	query, err := k.Contract(types.QueryContractRequest{ContractAddress: contractAddr})
	require.NoError(t, err)
	require.NotEqual(t, codeV2, query.Contract.CodeID)
	require.True(t, query.Contract.HasPendingUpgrade())

	// Once the real chain height actually reaches the earliest activation
	// height, the same schedule applies normally.
	applyResp, err := msgServer.ApplyScheduledUpgrade(sdk.Context{}.WithBlockHeight(1_000_001), &types.MsgApplyScheduledUpgrade{
		Actor: admin, ContractAddress: contractAddr, Height: 1,
	})
	require.NoError(t, err)
	require.Equal(t, "apply_scheduled_upgrade", applyResp.Receipt.Operation)
}

// ---- Permanent-lock adversarial matrix ----
//
// TestDisableContractUpgradesIsPermanent drives every keeper-level code path
// that touches Contract.Upgradeable / Contract.UpgradesDisabled after
// DisableContractUpgrades has been called, and asserts none of them can
// re-enable upgrades: SetContractAdmin, re-calling DisableContractUpgrades,
// ScheduleContractUpgrade, ApplyScheduledUpgrade (against a schedule that
// existed before the disable), UpgradeContractCode (the immediate path),
// MigrateContractState, and a genesis export/import roundtrip.
func TestDisableContractUpgradesIsPermanent(t *testing.T) {
	wallet := aeAddress("66")
	admin := aeAddress("67")
	newAdmin := aeAddress("68")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive, admin: accountStatusActive, newAdmin: accountStatusActive})
	setMinUpgradeDelay(t, &k, 5)

	contractAddr, codeV2 := deployUpgradeableTimelockFixture(t, &k, wallet, admin, 900)
	codeV3 := sha256Hex("timelock-permanent-v3/" + wallet)
	_, err := k.StoreCode(types.MsgStoreCode{Authority: wallet, CodeHash: codeV3, CodeBytes: 300})
	require.NoError(t, err)

	assertLocked := func(t *testing.T) {
		t.Helper()
		query, err := k.Contract(types.QueryContractRequest{ContractAddress: contractAddr})
		require.NoError(t, err)
		require.False(t, query.Contract.Upgradeable, "contract must remain non-upgradeable")
		require.True(t, query.Contract.UpgradesDisabled, "contract must remain permanently disabled")
	}

	// 1. Schedule an upgrade BEFORE disabling, so a pending record exists at
	//    disable time -- the adversarial case: does disabling clear it, and
	//    does apply-time re-checking catch it even if not?
	scheduleResp, err := k.ScheduleContractUpgrade(types.MsgScheduleContractUpgrade{
		Actor: admin, ContractAddress: contractAddr, NewCodeID: codeV2, MigrationHandler: "schema_only", Height: 900,
	})
	require.NoError(t, err)

	// 2. Disable upgrades permanently.
	disableResp, err := k.DisableContractUpgrades(types.MsgDisableContractUpgrades{
		Actor: admin, ContractAddress: contractAddr, Height: 905,
	})
	require.NoError(t, err)
	require.Equal(t, "disable_upgrades", disableResp.Operation)
	assertLocked(t)

	// The pending schedule recorded before the disable must have been
	// cleared by the disable itself (defense in depth).
	query, err := k.Contract(types.QueryContractRequest{ContractAddress: contractAddr})
	require.NoError(t, err)
	require.False(t, query.Contract.HasPendingUpgrade())

	// 3. Applying the pre-disable schedule must fail even though its
	//    earliest activation height has now been reached, and even though
	//    (belt-and-braces) the pending record was already cleared.
	_, err = k.ApplyScheduledContractUpgrade(types.MsgApplyScheduledUpgrade{
		Actor: admin, ContractAddress: contractAddr, Height: scheduleResp.EarliestActivationHeight,
	})
	require.Error(t, err)
	assertLocked(t)

	// 4. SetContractAdmin must succeed (admin rotation is not an upgrade
	//    operation) but must NOT touch Upgradeable/UpgradesDisabled.
	_, err = k.SetContractAdmin(types.MsgSetContractAdmin{
		Actor: admin, ContractAddress: contractAddr, NewAdmin: newAdmin, Height: 910,
	})
	require.NoError(t, err)
	assertLocked(t)

	// 5. The new admin cannot schedule, apply, or immediately upgrade.
	_, err = k.ScheduleContractUpgrade(types.MsgScheduleContractUpgrade{
		Actor: newAdmin, ContractAddress: contractAddr, NewCodeID: codeV2, MigrationHandler: "schema_only", Height: 911,
	})
	require.ErrorContains(t, err, "immutable")
	assertLocked(t)

	_, err = k.UpgradeContractCode(types.MsgUpgradeContractCode{
		Actor: newAdmin, ContractAddress: contractAddr, NewCodeID: codeV3, MigrationHandler: "schema_only", Height: 912,
	})
	require.ErrorContains(t, err, "immutable")
	assertLocked(t)

	_, err = k.MigrateContractState(types.MsgMigrateContractState{
		Actor: newAdmin, ContractAddress: contractAddr, FromSchemaVersion: 1, ToSchemaVersion: 2, MigrationHandler: "append", Payload: []byte(":v2"), Height: 913,
	})
	require.ErrorContains(t, err, "immutable")
	assertLocked(t)

	// 6. Re-calling DisableContractUpgrades must be a harmless no-op (still
	//    disabled afterwards), not an error that somehow leaves the flags
	//    ambiguous, and not a path that flips them back.
	secondDisable, err := k.DisableContractUpgrades(types.MsgDisableContractUpgrades{
		Actor: newAdmin, ContractAddress: contractAddr, Height: 914,
	})
	require.NoError(t, err)
	require.Equal(t, "disable_upgrades", secondDisable.Operation)
	assertLocked(t)

	// 7. A genesis export/import roundtrip (the persistence path used by
	//    node restarts and state sync) must preserve the permanent lock.
	gs := k.ExportGenesis()
	require.NoError(t, k.InitGenesis(gs))
	assertLocked(t)

	// 8. Final check through the live gRPC Msg route too, matching
	//    TestContractLifecycleMsgsLiveReachableThroughGRPCMsgServer's own
	//    "must now fail" assertion for UpgradeContractCode.
	msgServer := NewGRPCMsgServer(&k)
	_, err = msgServer.ApplyScheduledUpgrade(sdk.Context{}.WithBlockHeight(1000), &types.MsgApplyScheduledUpgrade{
		Actor: newAdmin, ContractAddress: contractAddr, Height: 1000,
	})
	require.Error(t, err)
	assertLocked(t)
}

// TestScheduleContractUpgradeRejectsWhenAlreadyImmutable proves scheduling
// (not just applying) is blocked once a contract was deployed non-
// upgradeable, matching UpgradeContractCode's existing immutability check.
func TestScheduleContractUpgradeRejectsWhenAlreadyImmutable(t *testing.T) {
	wallet := aeAddress("69")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})
	setMinUpgradeDelay(t, &k, 1)

	codeV1 := storeContractCode(t, &k, wallet)
	codeV2 := sha256Hex("timelock-immutable-v2/" + wallet)
	_, err := k.StoreCode(types.MsgStoreCode{Authority: wallet, CodeHash: codeV2, CodeBytes: 256})
	require.NoError(t, err)

	deployed, err := k.InstantiateContract(types.MsgInstantiateContract{
		Creator: wallet, CodeID: codeV1, InitMsg: []byte("v1"), Admin: wallet, Salt: "immutable-fixture", Upgradeable: false, SchemaVersion: 1, Height: 1000,
	})
	require.NoError(t, err)

	_, err = k.ScheduleContractUpgrade(types.MsgScheduleContractUpgrade{
		Actor: wallet, ContractAddress: deployed.ContractAddressUser, NewCodeID: codeV2, MigrationHandler: "schema_only", Height: 1000,
	})
	require.ErrorContains(t, err, "immutable")
}
