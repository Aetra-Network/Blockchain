package types

import (
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/app/addressing"
)

func TestContractsTxAPIValidationRejectsMalformedAddressesAndBounds(t *testing.T) {
	params := DefaultParams()
	sender := contractAPIAddress(0x11)
	stateInit := NewStateInit(sender, strings.Repeat("a", 64), nil, "api", 0)
	contract, _, err := DeriveContractAddressFromStateInit("", "", sender, stateInit, params)
	require.NoError(t, err)

	require.NoError(t, MsgDeployContract{
		Creator:     sender,
		CodeID:      strings.Repeat("a", 64),
		InitPayload: []byte("init"),
		Admin:       sender,
		Height:      1,
	}.ValidateBasic(params))
	require.ErrorContains(t, MsgDeployContract{Creator: sender, CodeID: "code", InitPayload: make([]byte, MaxContractPayloadBytes+1), Height: 1}.ValidateBasic(params), "payload")
	require.ErrorContains(t, MsgDeployContract{Creator: sender, CodeID: "code", Metadata: make([]byte, MaxContractMetadataBytes+1), Height: 1}.ValidateBasic(params), "metadata")

	require.NoError(t, MsgExecuteExternal{
		Sender:          sender,
		ContractAddress: contract,
		Payload:         []byte("call"),
		GasLimit:        params.MaxGasPerExecution,
		Height:          2,
	}.ValidateBasic(params))
	require.ErrorContains(t, MsgExecuteExternal{Sender: sender, ContractAddress: contract, GasLimit: params.MaxGasPerExecution + 1, Height: 2}.ValidateBasic(params), "gas limit")
	require.Error(t, MsgExecuteExternal{Sender: sender, ContractAddress: "4:" + strings.Repeat("00", 32), GasLimit: 1, Height: 2}.ValidateBasic(params))
}

// TestRequireStorageCloneGasFloor proves the Phase H "uncharged double
// CloneStorage" mitigation: below MinStorageBytesForCloneGasFloor any gas
// limit (even 1) is accepted (the clone is cheap regardless), at/above it a
// gas limit under the storage-proportional floor is rejected with a
// specific, checkable error, and a gas limit at or above the floor passes.
func TestRequireStorageCloneGasFloor(t *testing.T) {
	// Below the threshold: even a gas limit of 1 is fine, no floor applies.
	require.NoError(t, RequireStorageCloneGasFloor(MinStorageBytesForCloneGasFloor, 1))
	require.NoError(t, RequireStorageCloneGasFloor(0, 0))

	// Above the threshold: a near-zero gas limit is rejected.
	const bigStorage = MinStorageBytesForCloneGasFloor + 1024
	err := RequireStorageCloneGasFloor(bigStorage, 1)
	require.ErrorContains(t, err, ErrExecutionFailed)
	require.ErrorContains(t, err, "gas limit")

	// The exact boundary: one gas unit below the computed floor is rejected...
	floor := (uint64(bigStorage) / StorageCloneGasFloorDivisor) * 2
	require.Greater(t, floor, uint64(1))
	require.Error(t, RequireStorageCloneGasFloor(bigStorage, floor-1))
	// ...and the floor itself (or more) is accepted.
	require.NoError(t, RequireStorageCloneGasFloor(bigStorage, floor))

	// A large-but-realistic storage size (the AVM's own always-binding 1 MiB
	// MaxMemoryBytes ceiling) must still clear comfortably under the
	// module's default gas budgets, so ordinary executions against
	// max-size contracts are never rejected by this floor.
	const oneMiB = 1024 * 1024
	require.NoError(t, RequireStorageCloneGasFloor(oneMiB, 100_000))
}

// TestRequireCloneGasFloor proves RequireCloneGasFloor closes the gap
// RequireStorageCloneGasFloor alone leaves open: a contract with tiny
// storage but large bytecode. It must be rejected by the CODE floor even
// though its storage never crosses MinStorageBytesForCloneGasFloor, and
// symmetrically a contract with tiny code but large storage must still be
// rejected by the (delegated) storage floor -- proving neither dimension
// can be used to smuggle an unbilled O(size) decode past the other.
func TestRequireCloneGasFloor(t *testing.T) {
	// Both dimensions small: no floor applies regardless of gas.
	require.NoError(t, RequireCloneGasFloor(0, 0, 0))
	require.NoError(t, RequireCloneGasFloor(MinCodeBytesForDecodeGasFloor, MinStorageBytesForCloneGasFloor, 1))

	// Large bytecode, near-empty storage: RequireStorageCloneGasFloor ALONE
	// (the pre-fix check) would never engage here since storageBytes never
	// crosses its threshold -- this is exactly the evasion the confirmed
	// finding describes, and RequireCloneGasFloor must still reject it.
	const bigCode = MinCodeBytesForDecodeGasFloor + 4096
	require.NoError(t, RequireStorageCloneGasFloor(64, 1), "sanity: the storage-only check alone would accept this")
	err := RequireCloneGasFloor(bigCode, 64, 1)
	require.ErrorContains(t, err, ErrExecutionFailed)
	require.ErrorContains(t, err, "gas limit")
	require.ErrorContains(t, err, "byte-code")

	codeFloor := uint64(bigCode) / CodeDecodeGasFloorDivisor
	require.Greater(t, codeFloor, uint64(1))
	require.Error(t, RequireCloneGasFloor(bigCode, 64, codeFloor-1))
	require.NoError(t, RequireCloneGasFloor(bigCode, 64, codeFloor))

	// Large storage, near-empty code: the storage floor still applies via
	// delegation to RequireStorageCloneGasFloor, unaffected by this change.
	const bigStorage = MinStorageBytesForCloneGasFloor + 4096
	require.ErrorContains(t, RequireCloneGasFloor(64, bigStorage, 1), "byte-storage")

	// Both dimensions large: whichever floor is higher must be met.
	storageFloor := (uint64(bigStorage) / StorageCloneGasFloorDivisor) * 2
	require.NoError(t, RequireCloneGasFloor(bigCode, bigStorage, max(codeFloor, storageFloor)))
}

// TestMaxStateGrowthBytesPerExecutionBelowAVMMemoryCeiling documents (and
// pins) that the per-execution growth cap is meaningfully smaller than the
// AVM's own always-binding 1 MiB MaxMemoryBytes ceiling (avm.DefaultParams,
// always used by the keeper's Runner construction) -- otherwise the cap
// would never be reachable in practice. See MaxStateGrowthBytesPerExecution's
// doc comment.
func TestMaxStateGrowthBytesPerExecutionBelowAVMMemoryCeiling(t *testing.T) {
	const avmMaxMemoryBytes = 1024 * 1024
	require.Less(t, uint64(MaxStateGrowthBytesPerExecution), uint64(avmMaxMemoryBytes))
}

func TestContractsStoreCodeAndQueryAPIValidation(t *testing.T) {
	params := DefaultParams()
	sender := contractAPIAddress(0x22)
	bytecode := []byte("AVM1 deterministic")
	require.NoError(t, MsgStoreCode{Authority: sender, Bytecode: bytecode}.ValidateBasic(params))
	// ValidateBasic only runs the cheap structural checks (header/size); it
	// cannot decode/verify the module without an x/aetravm/avm import, which
	// would cycle back into this package (see bytecode.go's doc comment).
	// The real accept/reject decision now lives in
	// x/contracts/keeper.storeCodeUnchecked (FINDING-004) and is covered
	// there, not here. This still proves the header check alone rejects
	// obviously-malformed input.
	require.ErrorContains(t, MsgStoreCode{Authority: sender, Bytecode: []byte("BAD1 random")}.ValidateBasic(params), ErrInvalidBytecode)

	require.NoError(t, ValidateQueryPagination(PageRequest{Limit: MaxContractQueryLimit}))
	require.ErrorContains(t, ValidateQueryPagination(PageRequest{}), "query limit")
	require.ErrorContains(t, ValidateQueryPagination(PageRequest{Limit: MaxContractQueryLimit + 1}), "query limit")
}

func contractAPIAddress(fill byte) string {
	bz := make([]byte, 20)
	for i := range bz {
		bz[i] = fill
	}
	return addressing.FormatAccAddress(sdk.AccAddress(bz))
}
