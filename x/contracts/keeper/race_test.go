package keeper

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/contracts/types"
)

// TestConcurrentQueryAndMutationDoNotRace is the regression guard for
// FINDING-008: Keeper.genesis had no synchronization between the query
// surface (Codes/Contracts/Contract/ContractStorage/... -- reachable from
// concurrent gRPC/REST query goroutines) and the write path (mutation
// methods such as StoreCode, which reassign k.genesis on every
// state-changing message, mirroring what loadForBlock does per block).
//
// Run with `go test -race ./x/contracts/keeper/... -run
// TestConcurrentQueryAndMutationDoNotRace`. The race detector is the actual
// assertion here: one goroutine repeatedly mutates the keeper (a real
// mutation method, storeCodeUnchecked, which ends in assignGenesis exactly
// like every other write site in keeper.go) while another concurrently
// hammers the query surface (Contracts/Codes), exactly the shape of a gRPC
// query goroutine racing block processing. Before the fix (k.genesis read
// and written with no synchronization) this reliably trips "WARNING: DATA
// RACE"; after the fix (snapshotGenesis/assignGenesis under a
// sync.RWMutex) it is race-clean. See this package's git history / the
// keeper.go doc comments on Keeper.mu for how this was verified: the
// RLock/Lock calls inside snapshotGenesis/assignGenesis were temporarily
// removed and this exact test was re-run to confirm it fails without them.
func TestConcurrentQueryAndMutationDoNotRace(t *testing.T) {
	k := NewKeeper()
	wallet := aeAddress("11")
	k.accountStatusReader = testAccountStatus{wallet: accountStatusActive}

	// storeCodeUnchecked ends every call with RefreshStateRoot (Normalize +
	// Validate) over the WHOLE stored-code list, so the writer loop's total
	// cost is O(iterations^2) regardless of -race; 200 keeps this test's
	// normal (non-race) runtime reasonable while still being far more
	// iterations than needed to reliably trigger the race detector on
	// unsynchronized access (empirically confirmed: this test, temporarily
	// run against snapshotGenesis/assignGenesis with their RLock/Lock calls
	// removed, reported "WARNING: DATA RACE" between assignGenesis's write
	// and Contracts' read well before either loop finished).
	const iterations = 200

	var wg sync.WaitGroup
	wg.Add(2)

	writerErr := make(chan error, 1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			// A real mutation method: reads k.genesis directly to build
			// `next` (safe -- ABCI-style single-writer-at-a-time), then
			// commits via assignGenesis exactly like every other write site.
			if _, err := k.storeCodeUnchecked(types.MsgStoreCode{
				Authority: wallet,
				CodeHash:  sha256Hex(fmt.Sprintf("race-fixture-%d", i)),
				CodeBytes: 128,
			}); err != nil {
				writerErr <- err
				return
			}
		}
		writerErr <- nil
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			// Exactly what the gRPC/REST query surface does concurrently
			// with block processing: Contracts/Codes both read k.genesis
			// through the same query path grpcQueryServer uses.
			_, _ = k.Contracts(types.QueryContractsRequest{Pagination: types.PageRequest{Limit: 10}})
			_, _ = k.Codes(types.QueryCodesRequest{Pagination: types.PageRequest{Limit: 10}})
			_, _, _ = k.Code(types.QueryCodeRequest{CodeID: sha256Hex(fmt.Sprintf("race-fixture-%d", i))})
		}
	}()

	wg.Wait()
	require.NoError(t, <-writerErr)

	exported := k.ExportGenesis()
	require.Len(t, exported.State.Codes, iterations, "every store must have committed exactly once despite the concurrent query load")
}

// TestConcurrentTopUpAndContractsQueryDoNotRace is a second, complementary
// regression guard for FINDING-008. Fixing the top-level k.genesis race
// (above) surfaced a related, independent hazard: about ten mutation
// methods (TopUpContract, UpgradeContractCode, MigrateContractState,
// SetContractAdmin, DisableContractUpgrades, executeContract,
// PayContractStorageDebt, unfreezeContract, InjectNativeStaking,
// persistContractAt) did `next := k.genesis; next.State.Contracts[idx] = X`
// -- `next := k.genesis` only copies the Contracts SLICE HEADER, not its
// backing array, so writing to next.State.Contracts[idx] without first
// cloning that slice mutates the SAME backing array a concurrent query's
// snapshotGenesis() snapshot may still reference, even with k.genesis
// itself now properly locked. All ten sites now clone Contracts before the
// index-write (see the "Clone before index-write" comments in keeper.go).
// TopUpContract is the simplest of the ten to exercise here.
func TestConcurrentTopUpAndContractsQueryDoNotRace(t *testing.T) {
	owner := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{owner: accountStatusActive})
	codeHash := storeContractCode(t, &k, owner)
	deployed := instantiateContract(t, &k, owner, codeHash, "race-topup", 10, 1_000_000, 0)

	const iterations = 300

	var wg sync.WaitGroup
	wg.Add(2)

	writerErr := make(chan error, 1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			if _, err := k.TopUpContract(types.MsgTopUpContract{
				Sender:          owner,
				ContractAddress: deployed.ContractAddressUser,
				Amount:          1,
				Height:          uint64(11 + i),
			}); err != nil {
				writerErr <- err
				return
			}
		}
		writerErr <- nil
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_, _ = k.Contracts(types.QueryContractsRequest{Pagination: types.PageRequest{Limit: 10}})
		}
	}()

	wg.Wait()
	require.NoError(t, <-writerErr)

	final, err := k.Contract(types.QueryContractRequest{ContractAddress: deployed.ContractAddressUser})
	require.NoError(t, err)
	require.Equal(t, deployed.Balance+uint64(iterations), final.Contract.Balance, "every top-up must have applied exactly once despite the concurrent query load")
}
