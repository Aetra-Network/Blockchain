package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/contracts/types"
)

// This file exercises the read-only cross-contract call mechanism (design
// doc §6, §6.8) through the REAL keeper wiring -- newExternalGetResolver
// closing over the correct point-in-time contract/code snapshot at each of
// the real call sites (ContractGet's query path and executeContract's
// mutating path), not a hand-rolled stub resolver -- deploying two
// genuinely separate, independently compiled contracts and letting one call
// the other's getter through the full StoreCode -> InstantiateContract ->
// ContractGet/ExecuteContract pipeline.

// externalGetWalletSource is the read-only TARGET contract: a trivial
// balance holder with one getter.
const externalGetWalletSource = `
@storage
struct Storage {
    balance: uint64
}

@message(0x9101)
struct Noop {}

type InternalMsg = Noop
type ExternalMsg = Noop

contract Wallet {
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
    }

    @external
    func onExternalMessage(inMsg: Segment) {
    }

    @get
    func getBalance(): uint64 {
        const st = lazy Storage.load()
        return st.balance
    }
}
`

// externalGetReaderSource is the CALLER contract: its @get getter reads
// another contract's balance synchronously (read-only query path), and its
// @external handler does the same mid-mutating-execution and persists the
// result (mutating/executeContract path) -- exercising both real keeper call
// sites newExternalGetResolver is wired into.
const externalGetReaderSource = `
@storage
struct Storage {
    target: address
    lastBalance: uint64
}

@message(0x9102)
struct Refresh {}

type InternalMsg = Refresh
type ExternalMsg = Refresh

contract Reader {
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
    }

    @external
    func onExternalMessage(inMsg: Segment) {
        var st = lazy Storage.load()
        st.lastBalance = externalGet(st.target, "getBalance", "uint64")
        st.save()
    }

    @get
    func readOtherBalance(target: address): uint64 {
        return externalGet(target, "getBalance", "uint64")
    }

    @get
    func lastBalance(): uint64 {
        const st = lazy Storage.load()
        return st.lastBalance
    }
}
`

// deployExternalGetWallet deploys the Wallet contract with the given initial
// balance and returns its user-facing address.
func deployExternalGetWallet(t *testing.T, k *Keeper, owner string, balance uint64, salt string, height uint64) string {
	t.Helper()
	c := mustFamilyCompiler(t)
	res, err := c.Compile([]byte(externalGetWalletSource))
	require.NoError(t, err)
	codeID := storeCompiledCode(t, k, owner, res)
	initData, err := res.StorageCodec.Encode(map[string]any{"balance": balance})
	require.NoError(t, err)
	created, err := k.InstantiateContract(types.MsgInstantiateContract{
		Creator: owner, CodeID: codeID, InitMsg: initData, Funds: 1_000_000,
		Admin: owner, Salt: salt, Height: height,
	})
	require.NoError(t, err)
	return created.ContractAddressUser
}

// deployExternalGetReader deploys the Reader contract pointed at target.
func deployExternalGetReader(t *testing.T, k *Keeper, owner, target, salt string, height uint64) string {
	t.Helper()
	c := mustFamilyCompiler(t)
	res, err := c.Compile([]byte(externalGetReaderSource))
	require.NoError(t, err)
	codeID := storeCompiledCode(t, k, owner, res)
	initData, err := res.StorageCodec.Encode(map[string]any{"target": target, "lastBalance": uint64(0)})
	require.NoError(t, err)
	created, err := k.InstantiateContract(types.MsgInstantiateContract{
		Creator: owner, CodeID: codeID, InitMsg: initData, Funds: 1_000_000,
		Admin: owner, Salt: salt, Height: height,
	})
	require.NoError(t, err)
	return created.ContractAddressUser
}

// TestKeeperContractGetExternalGetSuccess: a genuine successful cross-
// contract read through the real ContractGet query RPC -- the Reader's own
// getter, invoked by name, synchronously reads the Wallet's current
// committed storage and returns it.
func TestKeeperContractGetExternalGetSuccess(t *testing.T) {
	owner := aeAddress("31")
	k := NewKeeperWithAccountStatus(testAccountStatus{owner: accountStatusActive})
	require.NoError(t, k.InitGenesis(k.ExportGenesis()))

	walletAddr := deployExternalGetWallet(t, &k, owner, 4242, "wallet-a", 10)
	readerAddr := deployExternalGetReader(t, &k, owner, walletAddr, "reader-a", 11)

	got, err := k.ContractGet(types.QueryContractGetRequest{
		ContractAddress: readerAddr,
		Method:          "readOtherBalance",
		Args:            []types.GetMethodArg{{Type: "address", Value: walletAddr}},
	})
	require.NoError(t, err)
	require.True(t, got.Success, "externalGet() through the real resolver must succeed: %s", got.Error)
	require.Equal(t, "4242", got.Result)
	require.NotZero(t, got.GasUsed)
}

// TestKeeperContractGetExternalGetTargetNotFound: a target address that
// resolves to no deployed contract soft-fails the query (Success=false, a
// real error MESSAGE, but not a Go-level error return) -- matching
// ContractGet's own existing convention for any other dispatch failure.
func TestKeeperContractGetExternalGetTargetNotFound(t *testing.T) {
	owner := aeAddress("32")
	k := NewKeeperWithAccountStatus(testAccountStatus{owner: accountStatusActive})
	require.NoError(t, k.InitGenesis(k.ExportGenesis()))

	walletAddr := deployExternalGetWallet(t, &k, owner, 100, "wallet-b", 10)
	readerAddr := deployExternalGetReader(t, &k, owner, walletAddr, "reader-b", 11)

	got, err := k.ContractGet(types.QueryContractGetRequest{
		ContractAddress: readerAddr,
		Method:          "readOtherBalance",
		Args:            []types.GetMethodArg{{Type: "address", Value: aeAddress("99")}},
	})
	require.NoError(t, err)
	require.False(t, got.Success, "a nonexistent external-get target must soft-fail, not silently succeed")
}

// TestKeeperExecuteContractExternalGetSuccess exercises the MUTATING
// (executeContract) call site: the Reader's @external handler performs a
// read-only externalGet() mid-execution while itself writing its own
// storage -- proving the two compose (the read never touches the write
// path) and that executeContract's resolver (closed over k.genesis, since
// no other contract has been mutated yet in this method) resolves the real,
// currently-committed target contract correctly.
func TestKeeperExecuteContractExternalGetSuccess(t *testing.T) {
	owner := aeAddress("33")
	k := NewKeeperWithAccountStatus(testAccountStatus{owner: accountStatusActive})
	require.NoError(t, k.InitGenesis(k.ExportGenesis()))

	walletAddr := deployExternalGetWallet(t, &k, owner, 777, "wallet-c", 10)
	readerAddr := deployExternalGetReader(t, &k, owner, walletAddr, "reader-c", 11)

	_, err := k.ExecuteContract(types.MsgExecuteContract{
		Sender:          owner,
		ContractAddress: readerAddr,
		Msg:             []byte{},
		Height:          12,
	})
	require.NoError(t, err)

	got, err := k.ContractGet(types.QueryContractGetRequest{
		ContractAddress: readerAddr,
		Method:          "lastBalance",
	})
	require.NoError(t, err)
	require.True(t, got.Success, "lastBalance query must succeed: %s", got.Error)
	require.Equal(t, "777", got.Result, "the mutating handler's externalGet() result must have been persisted")
}

// TestNewExternalGetResolverCodeSizeFloor is the regression guard for the
// confirmed finding that newExternalGetResolver's pre-decode gas floor was
// keyed ONLY on the target's StorageBytes, completely ignoring its
// bytecode size -- so a target with near-empty storage but bytecode near
// Params.MaxCodeBytes sailed through with any gasBudget, including 1, and
// loadAVMModule's O(code-size) DecodeModule call ran fully unbilled for
// every hop of an externalGet() chain through it. Calls the resolver
// directly (this file is `package keeper`) with a target whose Bytecode is
// deliberately garbage: if the code-size floor did not run BEFORE
// loadAVMModule (either because it's missing entirely, as in the
// pre-fix version, or reordered wrong), the resolver would instead return
// the loadAVMModule soft-fail (found=false, err=nil) rather than the
// floor's hard reject (err != nil, specific "byte-code"/"gas limit" text) --
// the same found/err distinction TestContractGetGasFloorCheckedBeforeDecode
// uses for the sibling ContractGet check.
func TestNewExternalGetResolverCodeSizeFloor(t *testing.T) {
	const codeID = "garbage-code-resolver"
	owner := aeAddress("51")
	codes := []types.CodeRecord{{
		CodeID:   codeID,
		CodeHash: codeID,
		// Above MinCodeBytesForDecodeGasFloor so the CODE floor engages;
		// deliberately does not match len(Bytecode) -- see
		// TestContractGetGasFloorCheckedBeforeDecode's doc comment for why
		// that mismatch is intentional in this direct-genesis-write style
		// of test.
		CodeBytes: types.MinCodeBytesForDecodeGasFloor + 65536,
		Bytecode:  []byte("not a real AVM module"),
		Owner:     owner,
	}}
	addr := aeAddress("52")
	contracts := []types.Contract{{
		AddressUser:          addr,
		AddressRaw:           addressRawForTest(t, addr),
		CodeID:               codeID,
		Creator:              owner,
		Owner:                owner,
		Admin:                owner,
		Status:               types.ContractStatusActive,
		StorageSchemaVersion: 1,
		// Deliberately tiny: below MinStorageBytesForCloneGasFloor, so the
		// storage-only floor alone would never engage here -- this test is
		// specifically about the CODE dimension the confirmed finding says
		// was ignored.
		StorageBytes:  16,
		CreatedHeight: 1,
		UpdatedHeight: 1,
		LogicalTime:   1,
	}}

	resolver := newExternalGetResolver(contracts, codes)
	_, _, found, err := resolver(addr, 1)
	require.False(t, found)
	require.Error(t, err, "a near-zero gasBudget against large bytecode must be rejected by the code-size floor")
	require.ErrorContains(t, err, "gas limit")
	require.ErrorContains(t, err, "byte-code")
}
