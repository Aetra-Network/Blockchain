package keeper

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/contracts/types"
)

// --- Pure unit tests of the checker functions themselves -------------------

func TestChangedStorageKeyCount(t *testing.T) {
	before := avm.Storage{"a": []byte("1"), "b": []byte("2"), "c": []byte("3")}
	after := avm.Storage{
		"a": []byte("1"),        // unchanged
		"b": []byte("changed"),  // value changed
		"d": []byte("new"),      // added
		// "c" deleted
	}
	require.Equal(t, 3, changedStorageKeyCount(before, after))
	require.Equal(t, 0, changedStorageKeyCount(before, before))
}

func TestEnforceAVMExecutionCapsEvents(t *testing.T) {
	preStorage := avm.Storage{}
	within := avm.Execution{State: preStorage, Outgoing: make([]async.MessageEnvelope, types.MaxEventsPerExecution)}
	require.NoError(t, enforceAVMExecutionCaps(preStorage, within))

	tooMany := avm.Execution{State: preStorage, Outgoing: make([]async.MessageEnvelope, types.MaxEventsPerExecution+1)}
	err := enforceAVMExecutionCaps(preStorage, tooMany)
	require.ErrorContains(t, err, types.ErrExecutionFailed)
	require.ErrorContains(t, err, "outgoing messages")
}

func TestEnforceAVMExecutionCapsChangedKeys(t *testing.T) {
	preStorage := avm.Storage{}
	after := make(avm.Storage, types.MaxChangedStorageKeysPerExecution+1)
	for i := 0; i < types.MaxChangedStorageKeysPerExecution+1; i++ {
		after[fmt.Sprintf("k%d", i)] = []byte("v")
	}
	err := enforceAVMExecutionCaps(preStorage, avm.Execution{State: after})
	require.ErrorContains(t, err, types.ErrExecutionFailed)
	require.ErrorContains(t, err, "distinct storage keys")

	// Exactly at the cap is fine.
	atCap := make(avm.Storage, types.MaxChangedStorageKeysPerExecution)
	for i := 0; i < types.MaxChangedStorageKeysPerExecution; i++ {
		atCap[fmt.Sprintf("k%d", i)] = []byte("v")
	}
	require.NoError(t, enforceAVMExecutionCaps(preStorage, avm.Execution{State: atCap}))
}

func TestRequireStateGrowthWithinCap(t *testing.T) {
	require.NoError(t, requireStateGrowthWithinCap(1000, 500), "shrinking is always allowed")
	require.NoError(t, requireStateGrowthWithinCap(0, types.MaxStateGrowthBytesPerExecution))
	err := requireStateGrowthWithinCap(0, types.MaxStateGrowthBytesPerExecution+1)
	require.ErrorContains(t, err, types.ErrExecutionFailed)
	require.ErrorContains(t, err, "grew contract storage")
}

// --- Real AVM execution feeding a genuine avm.Execution into the checker --

func TestEnforceAVMExecutionCapsAgainstRealEmitterExecution(t *testing.T) {
	// A real avm.Runner.Run producing genuine Outgoing entries via the
	// legacy (non-map) OpEmitInternal form, proving the checker rejects
	// actual VM output shapes, not just hand-built structs. The legacy form
	// requires ctx.EmitDestination, which keeper.buildAVMContext never sets
	// (production contracts use the TagMap form instead) -- so this drives
	// avm.Runner directly, the same way x/aetravm/avm's own tests do.
	n := types.MaxEventsPerExecution + 5
	code := make([]avm.Instruction, 0, n+1)
	for i := 0; i < n; i++ {
		code = append(code, avm.Instruction{Op: avm.OpEmitInternal, Arg: 1, Data: []byte("x")})
	}
	code = append(code, avm.Instruction{Op: avm.OpReturn, Arg: uint64(async.ResultOK)})
	module := avm.Module{
		Version: avm.Version,
		Imports: []avm.HostFunction{avm.HostEmitInternal, avm.HostReturn},
		Exports: map[avm.Entrypoint]uint32{avm.EntryReceiveInternal: 0},
		Code:    code,
	}
	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	preStorage := avm.Storage{}
	exec, err := runner.Run(module, preStorage, avm.RuntimeContext{
		Entry:           avm.EntryReceiveInternal,
		EmitDestination: bytes.Repeat([]byte{0x01}, 20),
		Message:         async.MessageEnvelope{GasLimit: 10_000_000},
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	require.Len(t, exec.Outgoing, n)

	err = enforceAVMExecutionCaps(preStorage, exec)
	require.ErrorContains(t, err, types.ErrExecutionFailed)
	require.ErrorContains(t, err, "outgoing messages")
}

// --- End-to-end integration tests through the real keeper flow -------------

// trivialReturnModule is a minimal, real, decodable+verifiable AVM module
// used where the module's own behavior is irrelevant to the test.
func trivialReturnModule(entry avm.Entrypoint) avm.Module {
	return avm.Module{
		Version: avm.Version,
		Imports: []avm.HostFunction{avm.HostReturn},
		Exports: map[avm.Entrypoint]uint32{entry: 0},
		Code:    []avm.Instruction{{Op: avm.OpReturn, Arg: uint64(async.ResultOK)}},
	}
}

// manyDistinctKeysModule writes n distinct storage keys in a straight-line
// sequence (no VM-level loop needed: OpWriteStorage's key is the
// compile-time Data field, so n writes just means n instructions).
func manyDistinctKeysModule(n int) avm.Module {
	code := make([]avm.Instruction, 0, n*2+1)
	for i := 0; i < n; i++ {
		code = append(code,
			avm.Instruction{Op: avm.OpPushU64, Arg: uint64(i)},
			avm.Instruction{Op: avm.OpWriteStorage, Data: []byte(fmt.Sprintf("k%05d", i))},
		)
	}
	code = append(code, avm.Instruction{Op: avm.OpReturn, Arg: uint64(async.ResultOK)})
	return avm.Module{
		Version: avm.Version,
		Imports: []avm.HostFunction{avm.HostWriteStorage, avm.HostReturn},
		Exports: map[avm.Entrypoint]uint32{avm.EntryReceiveExternal: 0},
		Code:    code,
	}
}

// bigValueGrowthModule builds one ~64KiB bytes value (a small embedded seed
// doubled via Dup+Concat, staying well under MaxCodeBytes) and writes it to
// six distinct keys, growing contract storage by ~384KiB in one execution --
// comfortably above MaxStateGrowthBytesPerExecution (256KiB) while touching
// far fewer than MaxChangedStorageKeysPerExecution keys, so it isolates the
// growth-bytes cap from the changed-keys cap.
func bigValueGrowthModule() avm.Module {
	// Instruction Data (including an OpPushBytes literal) is capped at 128
	// bytes (MaxKeySize), so the seed must start there and double up to
	// MaxBytesLength via Dup+Concat instead of being embedded directly:
	// 128 * 2^9 = 65536.
	seed := bytes.Repeat([]byte{0x42}, 128)
	code := []avm.Instruction{{Op: avm.OpPushBytes, Data: seed}}
	for i := 0; i < 9; i++ { // 128 * 2^9 = 65536 bytes
		code = append(code, avm.Instruction{Op: avm.OpDup}, avm.Instruction{Op: avm.OpConcat})
	}
	for i := 0; i < 5; i++ {
		code = append(code,
			avm.Instruction{Op: avm.OpDup},
			avm.Instruction{Op: avm.OpWriteStorage, Data: []byte(fmt.Sprintf("big%d", i))},
		)
	}
	code = append(code, avm.Instruction{Op: avm.OpWriteStorage, Data: []byte("big5")})
	code = append(code, avm.Instruction{Op: avm.OpReturn, Arg: uint64(async.ResultOK)})
	return avm.Module{
		Version: avm.Version,
		Imports: []avm.HostFunction{avm.HostWriteStorage, avm.HostReturn},
		Exports: map[avm.Entrypoint]uint32{avm.EntryReceiveExternal: 0},
		Code:    code,
	}
}

// storeExecutableModuleAndInstantiate stores bytecode, instantiates a
// contract against it, and stamps the contract's Data with an empty-but-
// decodable AVM storage snapshot (InstantiateContract's own InitMsg bytes
// are not one) so ExecuteContract's decodeContractSnapshot succeeds.
// executionCapTestFunds is large enough to keep every contract created via
// storeExecutableModuleAndInstantiate well clear of storage-rent debt across
// the handful of blocks these tests span, regardless of how large a test
// then grows StorageBytes to (see chargeContractRentAt: any positive
// balance shortfall freezes the contract before the AVM even runs).
const executionCapTestFunds = 1_000_000_000

func storeExecutableModuleAndInstantiate(t *testing.T, k *Keeper, wallet string, module avm.Module, salt string, height uint64) types.InstantiateContractResponse {
	t.Helper()
	bytecode, err := avm.EncodeModule(module)
	require.NoError(t, err)
	_, err = k.StoreCode(types.MsgStoreCode{Authority: wallet, Bytecode: bytecode})
	require.NoError(t, err)
	codeHash := types.CanonicalCodeHash(bytecode)
	created := instantiateContract(t, k, wallet, codeHash, salt, height, executionCapTestFunds, 0)
	setContractLifecycle(t, k, created.ContractAddressUser, types.ContractStatusActive, func(c *types.Contract) {
		c.Data = encodeSnapshotStorage(avm.Storage{})
	})
	return created
}

// bigDecodableSnapshot builds a real, avm.DecodeSnapshot-decodable contract
// storage blob of at least minBytes, so tests exercising a large-storage
// contract's execution (not just the pre-flight gas-floor check) can
// actually reach and complete Runner.Run.
func bigDecodableSnapshot(minBytes int) []byte {
	storage := avm.Storage{}
	value := bytes.Repeat([]byte{0x07}, 200)
	for i := 0; ; i++ {
		storage[fmt.Sprintf("pad%05d", i)] = append([]byte(nil), value...)
		encoded := avm.EncodeSnapshot(storage)
		if len(encoded) >= minBytes {
			return encoded
		}
	}
}

func TestReceiveInternalMessageRejectsGasBelowStorageCloneFloor(t *testing.T) {
	wallet := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})

	module := trivialReturnModule(avm.EntryReceiveInternal)
	source := storeExecutableModuleAndInstantiate(t, &k, wallet, module, "gas-floor-source", 9)
	destination := storeExecutableModuleAndInstantiate(t, &k, wallet, module, "gas-floor-dest", 10)

	bigSnapshot := bigDecodableSnapshot(int(types.MinStorageBytesForCloneGasFloor) + 4096)
	setContractLifecycle(t, &k, destination.ContractAddressUser, types.ContractStatusActive, func(c *types.Contract) {
		c.Data = append([]byte(nil), bigSnapshot...)
		c.StorageBytes = uint64(len(bigSnapshot))
	})

	queued := enqueueInternalForTest(t, &k, types.InternalMessage{
		SourceContractUser: source.ContractAddressUser,
		DestinationAccount: destination.ContractAddressUser,
		GasLimit:           1, // attacker-chosen, far below the storage-proportional floor
		Height:              12,
	})
	_, err := k.ReceiveInternalMessage(types.MsgReceiveInternalMessage{MessageID: queued.MessageID, Height: 12})
	require.ErrorContains(t, err, types.ErrExecutionFailed)
	require.ErrorContains(t, err, "gas limit")
	require.ErrorContains(t, err, "minimum")

	// Rejected before delivery: the message must remain queued, not consumed.
	require.Len(t, k.ExportGenesis().State.InternalMessages, 1)
}

func TestReceiveInternalMessageAcceptsGasAtOrAboveStorageCloneFloor(t *testing.T) {
	wallet := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})

	module := trivialReturnModule(avm.EntryReceiveInternal)
	source := storeExecutableModuleAndInstantiate(t, &k, wallet, module, "gas-floor-ok-source", 9)
	destination := storeExecutableModuleAndInstantiate(t, &k, wallet, module, "gas-floor-ok-dest", 10)

	bigSnapshot := bigDecodableSnapshot(int(types.MinStorageBytesForCloneGasFloor) + 4096)
	setContractLifecycle(t, &k, destination.ContractAddressUser, types.ContractStatusActive, func(c *types.Contract) {
		c.Data = append([]byte(nil), bigSnapshot...)
		c.StorageBytes = uint64(len(bigSnapshot))
	})

	queued := enqueueInternalForTest(t, &k, types.InternalMessage{
		SourceContractUser: source.ContractAddressUser,
		DestinationAccount: destination.ContractAddressUser,
		GasLimit:           200_000,
		Height:              12,
	})
	_, err := k.ReceiveInternalMessage(types.MsgReceiveInternalMessage{MessageID: queued.MessageID, Height: 12})
	require.NoError(t, err)
	require.Empty(t, k.ExportGenesis().State.InternalMessages, "successfully delivered message is dequeued")
}

// TestReceiveInternalMessageAcceptsDefaultGasLimitAtStorageCloneFloor proves
// the regression the Phase H storage-clone-gas-floor check itself introduced
// and then had fixed: GasLimit==0 is a valid, documented "use module
// default" sentinel (InternalMessage.Validate accepts it, and
// appendAVMOutgoingMessages copies GasLimit straight from an emitting
// contract's outgoing envelope, so 0 is plausibly the COMMON case, not an
// edge case). Before the fix, the floor check ran against the raw 0 instead
// of the resolved defaultAVMGasLimit=100_000, so every ordinary message
// omitting an explicit gas limit was permanently rejected the moment the
// destination's storage crossed MinStorageBytesForCloneGasFloor -- this test
// pins a destination well past that threshold and asserts delivery succeeds.
func TestReceiveInternalMessageAcceptsDefaultGasLimitAtStorageCloneFloor(t *testing.T) {
	wallet := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})

	module := trivialReturnModule(avm.EntryReceiveInternal)
	source := storeExecutableModuleAndInstantiate(t, &k, wallet, module, "gas-floor-default-source", 9)
	destination := storeExecutableModuleAndInstantiate(t, &k, wallet, module, "gas-floor-default-dest", 10)

	bigSnapshot := bigDecodableSnapshot(int(types.MinStorageBytesForCloneGasFloor) + 4096)
	setContractLifecycle(t, &k, destination.ContractAddressUser, types.ContractStatusActive, func(c *types.Contract) {
		c.Data = append([]byte(nil), bigSnapshot...)
		c.StorageBytes = uint64(len(bigSnapshot))
	})

	queued := enqueueInternalForTest(t, &k, types.InternalMessage{
		SourceContractUser: source.ContractAddressUser,
		DestinationAccount: destination.ContractAddressUser,
		GasLimit:           0, // the "use module default" sentinel -- must resolve to defaultAVMGasLimit BEFORE the floor check
		Height:              12,
	})
	_, err := k.ReceiveInternalMessage(types.MsgReceiveInternalMessage{MessageID: queued.MessageID, Height: 12})
	require.NoError(t, err)
	require.Empty(t, k.ExportGenesis().State.InternalMessages, "successfully delivered message is dequeued")
}

func TestExecuteContractRejectsTooManyChangedStorageKeys(t *testing.T) {
	wallet := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})

	n := types.MaxChangedStorageKeysPerExecution + 50
	contract := storeExecutableModuleAndInstantiate(t, &k, wallet, manyDistinctKeysModule(n), "too-many-keys", 10)

	before := k.ExportGenesis()
	_, err := k.ExecuteContract(types.MsgExecuteContract{
		Sender:          wallet,
		ContractAddress: contract.ContractAddressUser,
		Msg:             []byte("go"),
		Height:          11,
	})
	require.ErrorContains(t, err, types.ErrExecutionFailed)
	require.ErrorContains(t, err, "distinct storage keys")
	require.Equal(t, before, k.ExportGenesis(), "rejected execution must leave genesis state unchanged")
}

func TestExecuteContractAcceptsChangedStorageKeysAtCap(t *testing.T) {
	wallet := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})

	n := types.MaxChangedStorageKeysPerExecution
	contract := storeExecutableModuleAndInstantiate(t, &k, wallet, manyDistinctKeysModule(n), "keys-at-cap", 10)

	_, err := k.ExecuteContract(types.MsgExecuteContract{
		Sender:          wallet,
		ContractAddress: contract.ContractAddressUser,
		Msg:             []byte("go"),
		Height:          11,
	})
	require.NoError(t, err)
}

func TestExecuteContractRejectsExcessiveStorageGrowthInOneExecution(t *testing.T) {
	wallet := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})

	contract := storeExecutableModuleAndInstantiate(t, &k, wallet, bigValueGrowthModule(), "big-growth", 10)

	before := k.ExportGenesis()
	_, err := k.ExecuteContract(types.MsgExecuteContract{
		Sender:          wallet,
		ContractAddress: contract.ContractAddressUser,
		Msg:             []byte("go"),
		Height:          11,
	})
	require.ErrorContains(t, err, types.ErrExecutionFailed)
	require.ErrorContains(t, err, "grew contract storage")
	require.Equal(t, before, k.ExportGenesis(), "rejected execution must leave genesis state unchanged")
}
