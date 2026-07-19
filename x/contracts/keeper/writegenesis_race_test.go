package keeper

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/contracts/types"
	"github.com/sovereign-l1/l1/x/internal/kvtest"
)

// TestConcurrentStateWrapperPersistDoesNotRace is a regression guard closing
// a gap the design doc (avm-call-mechanism-v5-design.md §8.3) fix did not
// originally cover: every wire-level "...State" entrypoint (e.g.
// TopUpContractState) calls its txMu-locked core method (e.g. TopUpContract)
// and THEN, after that call has already returned and released txMu,
// separately calls writeGenesis to persist the result -- writeGenesis's own
// bare read of k.genesis, and writeDiff's bare reads/writes of
// k.written/k.writtenResidual (persistence.go), were themselves completely
// unsynchronized. A persistent keeper is required to exercise this: writeGenesis
// is a no-op when storeService == nil, which is why the earlier
// txmu_test.go tests (built against NewKeeperWithAccountStatus, non-persistent)
// did not catch it.
//
// Empirically confirmed both directions per this session's verification
// discipline: with writeGenesis/writeReplacingState's k.txMu.Lock() calls
// temporarily removed, this test reliably reports "WARNING: DATA RACE" in
// writeDiff (persistence.go, on k.writtenResidual) within a few hundred
// milliseconds using 8 goroutines x 300 iterations; with the lock calls
// restored (current keeper.go/persistence.go), it is race-clean. Run with
// `go test -race ./x/contracts/keeper/... -run TestConcurrentStateWrapperPersistDoesNotRace`.
func TestConcurrentStateWrapperPersistDoesNotRace(t *testing.T) {
	ctx := context.Background()
	service := kvtest.NewStoreService()
	owner := aeAddress("11")

	k := NewPersistentKeeper(service)
	k.accountStatusReader = testAccountStatus{owner: accountStatusActive}
	require.NoError(t, k.InitGenesisState(ctx, types.DefaultGenesis()))

	codeRes, err := k.StoreCodeState(ctx, types.MsgStoreCode{Authority: owner, CodeHash: sha256Hex("race-writegenesis-code"), CodeBytes: 128})
	require.NoError(t, err)

	deployed, err := k.InstantiateContractState(ctx, types.MsgInstantiateContract{
		Creator: owner,
		CodeID:  codeRes.CodeID,
		InitMsg: []byte("init"),
		Funds:   1_000_000,
		Admin:   owner,
		Salt:    "race-writegenesis",
		Height:  10,
	})
	require.NoError(t, err)

	const goroutines = 8
	const iterations = 300
	var wg sync.WaitGroup
	wg.Add(goroutines)

	errs := make(chan error, goroutines)
	for g := 0; g < goroutines; g++ {
		go func(base int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				if _, err := k.TopUpContractState(ctx, types.MsgTopUpContract{
					Sender:          owner,
					ContractAddress: deployed.ContractAddressUser,
					Amount:          1,
					Height:          uint64(11 + base*iterations + i),
				}); err != nil {
					errs <- err
					return
				}
			}
			errs <- nil
		}(g)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	final, err := k.Contract(types.QueryContractRequest{ContractAddress: deployed.ContractAddressUser})
	require.NoError(t, err)
	wantTotal := deployed.Balance + uint64(goroutines*iterations)
	require.Equal(t, wantTotal, final.Contract.Balance,
		"every concurrent top-up must be reflected exactly once in the persisted store path too")
}
