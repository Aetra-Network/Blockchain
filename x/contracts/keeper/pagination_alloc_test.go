package keeper

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/contracts/types"
)

// paginationAllocFixtureBytecode builds a small, distinct, real (decodable +
// verifiable) AVM module for each seed, cheaply (raw avm.EncodeModule, not
// the full .atlx compiler pipeline) -- fast enough to seed thousands of
// fixtures in a single test. See gateway_e2e_test.go's identical helper for
// why a real module is required (FINDING-004): a fake "AVM1 ..." ASCII
// placeholder no longer survives StoreCode, and CodeRecord.Validate (run on
// every keeper mutation) also runs ValidateAVMBytecode against it.
func paginationAllocFixtureBytecode(seed string) []byte {
	var metadata [32]byte
	copy(metadata[:], seed)
	module := avm.Module{
		Version:      avm.Version,
		Imports:      []avm.HostFunction{avm.HostReturn},
		Exports:      map[avm.Entrypoint]uint32{avm.EntryDeploy: 0},
		MetadataHash: metadata,
		Code:         []avm.Instruction{{Op: avm.OpReturn, Arg: 0}},
	}
	bz, err := avm.EncodeModule(module)
	if err != nil {
		panic(err)
	}
	return bz
}

// buildCodesKeeperForAllocTest constructs a Keeper whose in-memory genesis
// already holds n distinct stored code records, bypassing the full
// StoreCode/InitGenesis validation pipeline (irrelevant to this test, and
// slow at n in the thousands) by writing k.genesis directly -- this test
// file is `package keeper`, so it has that access.
func buildCodesKeeperForAllocTest(t testing.TB, n int) *Keeper {
	t.Helper()
	codes := make([]types.CodeRecord, n)
	for i := 0; i < n; i++ {
		bz := paginationAllocFixtureBytecode(fmt.Sprintf("alloc-fixture-%d", i))
		hash := types.CanonicalCodeHash(bz)
		codes[i] = types.CodeRecord{
			CodeID:    hash,
			CodeHash:  hash,
			CodeBytes: uint64(len(bz)),
			Bytecode:  bz,
			Owner:     aeAddress("11"),
		}
	}
	k := NewKeeper()
	k.genesis.State.Codes = codes
	return &k
}

// TestCodesQueryAllocCostScalesWithPageNotTotalCodeCount is the regression
// guard for FINDING-010: Codes() used to call k.genesis.State.Normalize(),
// which deep-clones EVERY stored Bytecode payload for the WHOLE state
// (cloneCodes: one extra allocation per stored code, for its Bytecode
// slice) before truncating to Pagination.Limit -- so a limit=1 query cost
// or (N stored codes) allocations regardless of what it returned. After the
// fix, pagination happens before the per-Bytecode clone, so a limit=1 query
// should cost about the same few allocations whether the state holds 5
// codes or several thousand.
//
// This uses testing.AllocsPerRun (not wall-clock time) because allocation
// COUNT is what actually distinguishes "clone everything, then slice" from
// "slice, then clone the page": both do a handful of allocations for a
// tiny page, but the old code additionally paid one allocation per STORED
// record no matter how small the page was.
func TestCodesQueryAllocCostScalesWithPageNotTotalCodeCount(t *testing.T) {
	const small = 5
	const large = 4000

	smallKeeper := buildCodesKeeperForAllocTest(t, small)
	largeKeeper := buildCodesKeeperForAllocTest(t, large)
	req := types.QueryCodesRequest{Pagination: types.PageRequest{Limit: 1}}

	// Sanity: both still return exactly one (correct) record.
	smallCodes, err := smallKeeper.Codes(req)
	require.NoError(t, err)
	require.Len(t, smallCodes, 1)
	largeCodes, err := largeKeeper.Codes(req)
	require.NoError(t, err)
	require.Len(t, largeCodes, 1)

	allocsSmall := testing.AllocsPerRun(50, func() {
		if _, err := smallKeeper.Codes(req); err != nil {
			t.Fatal(err)
		}
	})
	allocsLarge := testing.AllocsPerRun(50, func() {
		if _, err := largeKeeper.Codes(req); err != nil {
			t.Fatal(err)
		}
	})

	t.Logf("Codes(limit=1) allocs/op: %d stored codes -> %.1f allocs, %d stored codes -> %.1f allocs", small, allocsSmall, large, allocsLarge)

	// The large state has 800x as many stored codes as the small one. If
	// Codes() still deep-cloned the whole state before truncating, allocsLarge
	// would be roughly (large/small) times allocsSmall (~800x) -- one extra
	// allocation per stored Bytecode. With the fix, both are dominated by the
	// same fixed handful of allocations for the page itself (sort's own
	// backing copy, the one returned page's clone), so allow generous
	// constant-factor slack but assert it is nowhere near proportional to N.
	require.Lessf(t, allocsLarge, allocsSmall*3+20,
		"Codes(limit=1) alloc cost must not scale with total stored code count: small=%.1f large=%.1f", allocsSmall, allocsLarge)
}

// TestContractsAndQueueAndReceiptsPaginateBeforeCloning is a lighter
// correctness companion to the alloc-scaling test above: it proves
// Contracts/ContractQueue/ContractReceipts still return the right
// content/order after reordering pagination before the clone, for the
// other three methods FINDING-010 named.
func TestContractsAndQueueAndReceiptsPaginateBeforeCloning(t *testing.T) {
	k := NewKeeper()
	const n = 50
	contracts := make([]types.Contract, n)
	receipts := make([]types.ContractReceipt, 0, n)
	messages := make([]types.InternalMessage, 0, n)
	for i := 0; i < n; i++ {
		addr := aeAddress(fmt.Sprintf("%02x", i+1))
		contracts[i] = types.Contract{
			AddressUser:          addr,
			AddressRaw:           addressRawForTest(t, addr),
			CodeID:               "code",
			Creator:              addr,
			Owner:                addr,
			Admin:                addr,
			Status:               types.ContractStatusActive,
			StorageSchemaVersion: 1,
			CreatedHeight:        1,
			UpdatedHeight:        1,
			LogicalTime:          1,
		}
		receipt := types.ContractReceipt{
			ContractAddress: addr,
			Operation:       "deploy",
			ExitCode:        types.ExitCodeOK,
			LogicalTime:     1,
			Height:          uint64(i + 1),
		}
		receipt.ReceiptID = types.ComputeContractReceiptID(receipt)
		receipts = append(receipts, receipt)
		messages = append(messages, types.InternalMessage{
			SourceContractUser: addr,
			DestinationAccount: aeAddress("ff"),
			Height:             uint64(i + 1),
			LogicalTime:        1,
			Body:               []byte("body"),
		})
	}
	k.genesis.State.Contracts = contracts
	k.genesis.State.Receipts = receipts
	k.genesis.State.InternalMessages = messages

	page, err := k.Contracts(types.QueryContractsRequest{Pagination: types.PageRequest{Limit: 3}})
	require.NoError(t, err)
	require.Len(t, page, 3)
	require.True(t, page[0].AddressUser < page[1].AddressUser && page[1].AddressUser < page[2].AddressUser, "Contracts() must still return sorted-by-address order after reordering pagination before cloning")

	receiptPage, err := k.ContractReceipts(types.QueryContractReceiptsRequest{ContractAddress: contracts[0].AddressUser, Pagination: types.PageRequest{Limit: 10}})
	require.NoError(t, err)
	require.Len(t, receiptPage, 1)
	require.Equal(t, contracts[0].AddressUser, receiptPage[0].ContractAddress)

	queuePage, err := k.ContractQueue(types.QueryContractQueueRequest{ContractAddress: contracts[0].AddressUser, Pagination: types.PageRequest{Limit: 10}})
	require.NoError(t, err)
	require.Len(t, queuePage, 1)
	require.Equal(t, contracts[0].AddressUser, queuePage[0].SourceContractUser)
}

func addressRawForTest(t testing.TB, userAddress string) string {
	t.Helper()
	raw, err := types.RawAddressForUserAddress(userAddress)
	require.NoError(t, err)
	return raw
}
