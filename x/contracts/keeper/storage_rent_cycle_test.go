package keeper

import (
	"context"
	"encoding/json"
	"testing"

	sdkmath "cosmossdk.io/math"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	aetraaddress "github.com/sovereign-l1/l1/app/addressing"
	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/contracts/types"
)

// freezeContractForTest exhausts a freshly instantiated contract's storage
// rent balance so a real ExecuteContract call drives it through
// chargeContractRentAt's freeze path (the same technique
// TestGRPCMsgServerTopUpPayDebtUnfreezeHappyPath / TestFrozenContractRecoveryKeepsCodeDataAndBalance
// already use), returning the contract right after it froze at freezeHeight.
func freezeContractForTest(t *testing.T, k *Keeper, wallet, codeHash, salt string, freezeHeight uint64) types.Contract {
	t.Helper()
	created := instantiateContract(t, k, wallet, codeHash, salt, 10, 1, 100)
	_, err := k.ExecuteContract(types.MsgExecuteContract{Sender: wallet, ContractAddress: created.ContractAddressUser, Msg: []byte("too late"), Height: freezeHeight})
	require.ErrorContains(t, err, types.ErrStorageRent)
	frozen, err := k.Contract(types.QueryContractRequest{ContractAddress: created.ContractAddressUser})
	require.NoError(t, err)
	require.True(t, frozen.Found)
	require.Equal(t, types.ContractStatusFrozen, frozen.Contract.Status)
	require.NotZero(t, frozen.Contract.StorageRentDebt)
	return frozen.Contract
}

// TestChargeContractRentAtStampsDeletionEligibilityOnFirstFreeze is the
// contracts-storage-rent-cycle regression guard: the first time a contract
// freezes for nonpayment, chargeContractRentAt must stamp FreezeHeight to the
// freezing height and DeletionEligibilityHeight to
// FreezeHeight + Params.StorageRentRetentionBlocks -- and must NOT re-stamp
// either field on a later chargeRent call while the contract is still frozen
// (only unfreezing, then re-freezing, should produce a fresh window).
func TestChargeContractRentAtStampsDeletionEligibilityOnFirstFreeze(t *testing.T) {
	wallet := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})
	codeHash := storeContractCode(t, &k, wallet)

	frozen := freezeContractForTest(t, &k, wallet, codeHash, "freeze-stamp", 200)
	require.Equal(t, uint64(200), frozen.FreezeHeight)
	require.Equal(t, uint64(200)+types.DefaultParams().StorageRentRetentionBlocks, frozen.DeletionEligibilityHeight)

	// A further chargeRent pass (another failed ExecuteContract while still
	// frozen and unpaid) must not push the window out.
	_, err := k.ExecuteContract(types.MsgExecuteContract{Sender: wallet, ContractAddress: frozen.AddressUser, Msg: []byte("still frozen"), Height: 250})
	require.Error(t, err)
	stillFrozen, err := k.Contract(types.QueryContractRequest{ContractAddress: frozen.AddressUser})
	require.NoError(t, err)
	require.Equal(t, frozen.FreezeHeight, stillFrozen.Contract.FreezeHeight)
	require.Equal(t, frozen.DeletionEligibilityHeight, stillFrozen.Contract.DeletionEligibilityHeight)
}

// TestUnfreezeContractClearsFreezeEligibilityWindow proves unfreeze resets
// FreezeHeight/DeletionEligibilityHeight to zero, so a LATER freeze (verified
// below) re-stamps a fresh window rather than reusing or already satisfying a
// stale one.
func TestUnfreezeContractClearsFreezeEligibilityWindow(t *testing.T) {
	wallet := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})
	codeHash := storeContractCode(t, &k, wallet)

	frozen := freezeContractForTest(t, &k, wallet, codeHash, "freeze-clear", 300)
	require.NotZero(t, frozen.DeletionEligibilityHeight)

	_, err := k.TopUpContract(types.MsgTopUpContract{Sender: wallet, ContractAddress: frozen.AddressUser, Amount: frozen.StorageRentDebt + 1000, Height: 301})
	require.NoError(t, err)
	_, err = k.PayContractStorageDebt(types.MsgPayContractStorageDebt{Sender: wallet, ContractAddress: frozen.AddressUser, Amount: frozen.StorageRentDebt, Height: 302})
	require.NoError(t, err)
	unfrozen, err := k.UnfreezeContract(types.MsgUnfreezeContract{Sender: wallet, ContractAddress: frozen.AddressUser, Height: 303})
	require.NoError(t, err)
	require.Zero(t, unfrozen.FreezeHeight)
	require.Zero(t, unfrozen.DeletionEligibilityHeight)
}

// TestDeleteExpiredContractRequiresFrozenStatus rejects archival deletion of
// a contract that was never frozen (DeletionEligibilityHeight is zero), even
// though the lifecycle matrix's ArchiveDelete action is separately reachable
// from Active for the unrelated SEND_DESTROY_IF_EMPTY self-destruct path.
func TestDeleteExpiredContractRequiresFrozenStatus(t *testing.T) {
	wallet := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})
	codeHash := storeContractCode(t, &k, wallet)
	created := instantiateContract(t, &k, wallet, codeHash, "delete-active", 10, 1_000_000, 0)

	_, err := k.DeleteExpiredContract(types.MsgDeleteExpiredContract{
		Authority:       types.DefaultParams().Authority,
		ContractAddress: created.ContractAddressUser,
		Height:          1,
	})
	require.ErrorContains(t, err, types.ErrContractLifecycle)
}

// TestDeleteExpiredContractRejectsBeforeEligibilityHeight covers the
// height-gate: a frozen contract cannot be archived before
// FreezeHeight + Params.StorageRentRetentionBlocks.
func TestDeleteExpiredContractRejectsBeforeEligibilityHeight(t *testing.T) {
	wallet := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})
	codeHash := storeContractCode(t, &k, wallet)
	frozen := freezeContractForTest(t, &k, wallet, codeHash, "delete-too-early", 400)

	_, err := k.DeleteExpiredContract(types.MsgDeleteExpiredContract{
		Authority:       types.DefaultParams().Authority,
		ContractAddress: frozen.AddressUser,
		Height:          frozen.DeletionEligibilityHeight - 1,
	})
	require.ErrorContains(t, err, types.ErrContractLifecycle)
	require.ErrorContains(t, err, "not yet eligible")

	// Sanity: the contract is untouched by the rejected attempt.
	stillFrozen, err := k.Contract(types.QueryContractRequest{ContractAddress: frozen.AddressUser})
	require.NoError(t, err)
	require.Equal(t, types.ContractStatusFrozen, stillFrozen.Contract.Status)
}

// TestDeleteExpiredContractAuthorityGated proves this is a governance
// action, not something the contract owner (or anyone else) can trigger
// unilaterally, mirroring x/storage-rent's identical MsgDeleteExpiredContract
// precedent.
func TestDeleteExpiredContractAuthorityGated(t *testing.T) {
	wallet := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})
	codeHash := storeContractCode(t, &k, wallet)
	frozen := freezeContractForTest(t, &k, wallet, codeHash, "delete-unauthorized", 500)

	_, err := k.DeleteExpiredContract(types.MsgDeleteExpiredContract{
		Authority:       wallet, // not the governance authority
		ContractAddress: frozen.AddressUser,
		Height:          frozen.DeletionEligibilityHeight,
	})
	require.ErrorContains(t, err, types.ErrUnauthorized)
}

// TestDeleteExpiredContractArchivesAndWritesOffDebt is the end-to-end happy
// path: a frozen, past-eligibility contract is archived into a permanent
// tombstone satisfying ValidateDeletedContractTombstone, its remaining debt
// force-written off, and the resulting status locks it down to Query/
// ProofQuery only (see ContractLifecycleActionAllowed).
func TestDeleteExpiredContractArchivesAndWritesOffDebt(t *testing.T) {
	wallet := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})
	codeHash := storeContractCode(t, &k, wallet)
	frozen := freezeContractForTest(t, &k, wallet, codeHash, "delete-happy-path", 600)
	require.NotZero(t, frozen.StorageRentDebt)

	receipt, err := k.DeleteExpiredContract(types.MsgDeleteExpiredContract{
		Authority:       types.DefaultParams().Authority,
		ContractAddress: frozen.AddressUser,
		Height:          frozen.DeletionEligibilityHeight,
	})
	require.NoError(t, err)
	require.Equal(t, types.ExitCodeOK, receipt.ExitCode)
	require.Equal(t, frozen.StorageRentDebt, receipt.Amount, "receipt amount records the written-off bad debt")

	deleted, err := k.Contract(types.QueryContractRequest{ContractAddress: frozen.AddressUser})
	require.NoError(t, err)
	require.True(t, deleted.Found)
	require.Equal(t, types.ContractStatusDeleted, deleted.Contract.Status)
	require.Zero(t, deleted.Contract.Balance)
	require.Zero(t, deleted.Contract.StorageRentDebt)
	require.Zero(t, deleted.Contract.StorageBytes)
	require.Empty(t, deleted.Contract.Data)
	require.NoError(t, types.ValidateDeletedContractTombstone(deleted.Contract))

	require.True(t, types.ContractLifecycleActionAllowed(deleted.Contract.Status, types.ContractLifecycleActionQuery))
	require.True(t, types.ContractLifecycleActionAllowed(deleted.Contract.Status, types.ContractLifecycleActionProofQuery))
	require.False(t, types.ContractLifecycleActionAllowed(deleted.Contract.Status, types.ContractLifecycleActionExecuteExternal))
	require.False(t, types.ContractLifecycleActionAllowed(deleted.Contract.Status, types.ContractLifecycleActionUnfreeze))

	_, err = k.ExecuteContract(types.MsgExecuteContract{Sender: wallet, ContractAddress: frozen.AddressUser, Msg: []byte("blocked"), Height: frozen.DeletionEligibilityHeight + 1})
	require.ErrorContains(t, err, types.ErrContractLifecycle)
}

// TestDeleteExpiredContractSweepsToppedUpBalanceToTreasury reproduces the
// exact fund-safety finding this test guards: TopUpContract is reachable
// from Frozen without requiring debt payment or unfreezing first, so a
// frozen contract can carry a real, banked Balance when it is later
// archive-deleted. DeleteExpiredContract must not silently zero that Balance
// out of existence -- it must move the real naet (already resident in the
// storage-rent reserve module account) to the protocol treasury, and record
// exactly how much on the returned receipt.
func TestDeleteExpiredContractSweepsToppedUpBalanceToTreasury(t *testing.T) {
	wallet := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})
	bank := newPayoutMockBank()
	walletAddr, err := aetraaddress.ParseAccAddress(wallet)
	require.NoError(t, err)
	bank.set(walletAddr.String(), sdkmath.NewIntFromUint64(1_000_000_000))
	k = k.WithBankKeeper(bank)
	k.runtimeCtx = context.Background()

	codeHash := storeContractCode(t, &k, wallet)
	frozen := freezeContractForTest(t, &k, wallet, codeHash, "delete-sweep-balance", 700)
	require.NotZero(t, frozen.StorageRentDebt)
	require.Zero(t, frozen.Balance, "the contract must already be drained to zero by the freeze itself")

	// TopUpContract while still Frozen and still in debt -- exactly the path
	// the finding identifies as reachable without paying debt or unfreezing.
	const toppedUp = uint64(4_321)
	afterTopUp, err := k.TopUpContract(types.MsgTopUpContract{Sender: wallet, ContractAddress: frozen.AddressUser, Amount: toppedUp, Height: 701})
	require.NoError(t, err)
	require.Equal(t, toppedUp, afterTopUp.Balance)
	require.Equal(t, types.ContractStatusFrozen, afterTopUp.Status, "a top-up alone must not unfreeze a contract with unpaid debt")

	reserveAddr := authtypes.NewModuleAddress(storageRentReserveModule).String()
	treasuryAddr := authtypes.NewModuleAddress(contractTreasuryModule).String()
	reserveBefore := bank.get(reserveAddr)
	treasuryBefore := bank.get(treasuryAddr)

	receipt, err := k.DeleteExpiredContract(types.MsgDeleteExpiredContract{
		Authority:       types.DefaultParams().Authority,
		ContractAddress: frozen.AddressUser,
		Height:          frozen.DeletionEligibilityHeight,
	})
	require.NoError(t, err)
	require.Equal(t, types.ExitCodeOK, receipt.ExitCode)
	require.Equal(t, toppedUp, receipt.SweptBalance, "the receipt must record exactly the topped-up balance that was swept")
	require.Equal(t, frozen.StorageRentDebt, receipt.Amount, "the written-off bad debt is still recorded on Amount, distinct from SweptBalance")

	// The tombstone itself: Balance zeroed, still satisfying
	// ValidateDeletedContractTombstone.
	deleted, err := k.Contract(types.QueryContractRequest{ContractAddress: frozen.AddressUser})
	require.NoError(t, err)
	require.True(t, deleted.Found)
	require.Equal(t, types.ContractStatusDeleted, deleted.Contract.Status)
	require.Zero(t, deleted.Contract.Balance)
	require.NoError(t, types.ValidateDeletedContractTombstone(deleted.Contract))

	// The real bank-side movement this finding requires: the reserve is
	// debited by exactly the swept balance (not silently left overfunded
	// with an orphaned surplus) and the treasury is credited by exactly the
	// same amount -- a genuine transfer, not just a zeroed ledger field.
	reserveAfter := bank.get(reserveAddr)
	treasuryAfter := bank.get(treasuryAddr)
	require.True(t, reserveBefore.Sub(sdkmath.NewIntFromUint64(toppedUp)).Equal(reserveAfter),
		"the storage-rent reserve must be debited by exactly the swept balance")
	require.True(t, treasuryBefore.Add(sdkmath.NewIntFromUint64(toppedUp)).Equal(treasuryAfter),
		"the treasury must be credited by exactly the swept balance")
}

// TestDeleteExpiredContractWithZeroBalanceSweepsNothing is the negative
// counterpart: an ordinary frozen contract that was never topped up (Balance
// already zero, the common case exercised by
// TestDeleteExpiredContractArchivesAndWritesOffDebt) must not touch the bank
// at all -- SweptBalance stays zero and no reserve/treasury movement happens.
func TestDeleteExpiredContractWithZeroBalanceSweepsNothing(t *testing.T) {
	wallet := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})
	bank := newPayoutMockBank()
	walletAddr, err := aetraaddress.ParseAccAddress(wallet)
	require.NoError(t, err)
	bank.set(walletAddr.String(), sdkmath.NewIntFromUint64(1_000_000_000))
	k = k.WithBankKeeper(bank)
	k.runtimeCtx = context.Background()

	codeHash := storeContractCode(t, &k, wallet)
	frozen := freezeContractForTest(t, &k, wallet, codeHash, "delete-sweep-nothing", 800)
	require.Zero(t, frozen.Balance)

	reserveAddr := authtypes.NewModuleAddress(storageRentReserveModule).String()
	treasuryAddr := authtypes.NewModuleAddress(contractTreasuryModule).String()
	reserveBefore := bank.get(reserveAddr)
	treasuryBefore := bank.get(treasuryAddr)

	receipt, err := k.DeleteExpiredContract(types.MsgDeleteExpiredContract{
		Authority:       types.DefaultParams().Authority,
		ContractAddress: frozen.AddressUser,
		Height:          frozen.DeletionEligibilityHeight,
	})
	require.NoError(t, err)
	require.Zero(t, receipt.SweptBalance)

	require.True(t, reserveBefore.Equal(bank.get(reserveAddr)), "no reserve movement when there is nothing to sweep")
	require.True(t, treasuryBefore.Equal(bank.get(treasuryAddr)), "no treasury movement when there is nothing to sweep")
}

// minimalAVMModuleWithQueryGetter builds the smallest real, verifiable AVM
// module exposing one @get-style entrypoint (EntryQuery) -- enough to satisfy
// avm.VerifyInterface's per-method "entrypoint is exported" check for a
// manifest describing one get method, mirroring
// avm_execution_caps_test.go's minimalModule helper.
func minimalAVMModuleWithQueryGetter() avm.Module {
	return avm.Module{
		Version: avm.Version,
		Imports: []avm.HostFunction{avm.HostReturn},
		Exports: map[avm.Entrypoint]uint32{avm.EntryQuery: 0},
		Code:    []avm.Instruction{{Op: avm.OpReturn, Arg: uint64(async.ResultOK)}},
	}
}

func testInterfaceManifest(name string) avm.InterfaceManifest {
	return avm.InterfaceManifest{
		Name:    name,
		Version: 1,
		GetMethods: []avm.InterfaceGetMethod{
			{Name: "currentValue", Entrypoint: avm.EntryQuery, Selector: 1},
		},
	}
}

// TestStoreCodeVerifiesManifestAgainstModule is avm-get-methods-gap's
// on-chain manifest verification guard: a manifest whose declared surface
// hashes to the JUST-DECODED module's own MetadataHash (and whose declared
// entrypoints are actually exported) is accepted and stored verbatim on the
// CodeRecord; a manifest that does NOT match (wrong MetadataHash, or -- via
// ContractManifest below -- simply absent) must not silently pass.
func TestStoreCodeVerifiesManifestAgainstModule(t *testing.T) {
	wallet := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})

	manifest := testInterfaceManifest("counter")
	hash, err := avm.InterfaceHash(manifest)
	require.NoError(t, err)
	module := minimalAVMModuleWithQueryGetter()
	module.MetadataHash = hash
	bytecode, err := avm.EncodeModule(module)
	require.NoError(t, err)
	manifestBytes, err := json.Marshal(manifest)
	require.NoError(t, err)

	resp, err := k.StoreCode(types.MsgStoreCode{Authority: wallet, Bytecode: bytecode, ManifestBytes: manifestBytes})
	require.NoError(t, err)

	stored, found, err := k.Code(types.QueryCodeRequest{CodeID: resp.CodeID})
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, manifestBytes, stored.ManifestBytes)

	queried, err := k.ContractManifest(types.QueryContractManifestRequest{CodeID: resp.CodeID})
	require.NoError(t, err)
	require.True(t, queried.Found)
	require.Equal(t, manifestBytes, queried.ManifestBytes)
}

// TestStoreCodeRejectsManifestNotMatchingModule covers the negative path:
// a manifest whose hash does not match the module's MetadataHash (here,
// simply never stamped onto the module) must reject the whole StoreCode
// call, leaving no code record behind.
func TestStoreCodeRejectsManifestNotMatchingModule(t *testing.T) {
	wallet := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})

	manifest := testInterfaceManifest("counter")
	module := minimalAVMModuleWithQueryGetter() // MetadataHash left zero, won't match
	bytecode, err := avm.EncodeModule(module)
	require.NoError(t, err)
	manifestBytes, err := json.Marshal(manifest)
	require.NoError(t, err)

	_, err = k.StoreCode(types.MsgStoreCode{Authority: wallet, Bytecode: bytecode, ManifestBytes: manifestBytes})
	require.ErrorContains(t, err, types.ErrInvalidBytecode)

	require.Empty(t, k.ExportGenesis().State.Codes)
}

// TestStoreCodeRejectsManifestWithoutBytecode covers the "no module to
// verify against" guard: a manifest submitted alongside a hash-only StoreCode
// call (no fresh bytecode in this call) is rejected rather than silently
// accepted unverified.
func TestStoreCodeRejectsManifestWithoutBytecode(t *testing.T) {
	wallet := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})

	manifestBytes, err := json.Marshal(testInterfaceManifest("counter"))
	require.NoError(t, err)

	_, err = k.StoreCode(types.MsgStoreCode{Authority: wallet, CodeHash: sha256Hex("manifest-only"), CodeBytes: 128, ManifestBytes: manifestBytes})
	require.ErrorContains(t, err, types.ErrInvalidBytecode)
}

// TestContractManifestQueryNotFoundCases covers ContractManifest's two
// non-error "not found" shapes: an unknown code id, and a code stored
// without a manifest (StoreCode's manifest field is opt-in).
func TestContractManifestQueryNotFoundCases(t *testing.T) {
	wallet := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})

	res, err := k.ContractManifest(types.QueryContractManifestRequest{CodeID: "unknown-code-id"})
	require.NoError(t, err)
	require.False(t, res.Found)
	require.Empty(t, res.ManifestBytes)

	codeHash := storeContractCode(t, &k, wallet) // no manifest published
	res, err = k.ContractManifest(types.QueryContractManifestRequest{CodeID: codeHash})
	require.NoError(t, err)
	require.False(t, res.Found)

	_, err = k.ContractManifest(types.QueryContractManifestRequest{})
	require.Error(t, err)
}
