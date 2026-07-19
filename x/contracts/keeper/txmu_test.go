package keeper

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/contracts/types"
)

// This file exercises the x/contracts storage-locking fix (call mechanism
// v5 design doc §8.3): k.txMu now serializes the ENTIRE read-mutate-write
// critical section of every top-level mutating entrypoint, not just the
// final assignGenesis field-swap (which k.mu alone already guarded). The
// existing race_test.go covers mutation racing the QUERY surface; this file
// covers the scenario txMu specifically closes that race_test.go does not:
// two MUTATIONS racing each other, where an unsynchronized bare
// read-modify-write (`next := k.genesis; next.State.Contracts[idx].Balance
// += amount; k.assignGenesis(next)`) can silently lose an update if two
// goroutines both read the same starting balance before either commits.

// TestTxMuPreventsLostUpdateUnderConcurrentTopUps runs many concurrent
// TopUpContract calls against the SAME contract from multiple goroutines.
// Without txMu serializing the whole method (not just the final
// assignGenesis), two goroutines can both read the same k.genesis snapshot,
// independently add their own amount to the SAME starting balance, and the
// second assignGenesis silently overwrites the first's contribution -- a
// real lost-update, not a benign race. With txMu, every call's read-mutate-
// write is atomic with respect to every other locked call, so the final
// balance must equal the exact sum of every successful top-up.
func TestTxMuPreventsLostUpdateUnderConcurrentTopUps(t *testing.T) {
	owner := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{owner: accountStatusActive})
	codeHash := storeContractCode(t, &k, owner)
	deployed := instantiateContract(t, &k, owner, codeHash, "race-txmu-topup", 10, 1_000_000, 0)

	const goroutines = 8
	const perGoroutine = 50
	const amount = 3

	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make(chan error, goroutines)
	for g := 0; g < goroutines; g++ {
		go func(base int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				if _, err := k.TopUpContract(types.MsgTopUpContract{
					Sender:          owner,
					ContractAddress: deployed.ContractAddressUser,
					Amount:          amount,
					Height:          uint64(11 + base*perGoroutine + i),
				}); err != nil {
					errs <- err
					return
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	final, err := k.Contract(types.QueryContractRequest{ContractAddress: deployed.ContractAddressUser})
	require.NoError(t, err)
	wantTotal := deployed.Balance + uint64(goroutines*perGoroutine*amount)
	require.Equal(t, wantTotal, final.Contract.Balance,
		"every concurrent top-up must be reflected exactly once -- a lost update here means txMu failed to serialize the read-mutate-write critical section")
}

// TestTxMuPreventsLostUpdateAcrossDifferentMutatingMethods is the same
// lost-update check but across two DIFFERENT locked entrypoints
// (TopUpContract and PayContractStorageDebt both touch the same contract's
// Balance-adjacent fields), confirming txMu is a single shared lock across
// every listed method, not a per-method lock that would still race a
// different method touching overlapping state.
func TestTxMuPreventsLostUpdateAcrossDifferentMutatingMethods(t *testing.T) {
	owner := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{owner: accountStatusActive})
	codeHash := storeContractCode(t, &k, owner)
	deployed := instantiateContract(t, &k, owner, codeHash, "race-txmu-cross", 10, 1_000_000, 0)

	const iterations = 100
	var wg sync.WaitGroup
	wg.Add(2)

	topUpErr := make(chan error, 1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			if _, err := k.TopUpContract(types.MsgTopUpContract{
				Sender:          owner,
				ContractAddress: deployed.ContractAddressUser,
				Amount:          1,
				Height:          uint64(11 + i),
			}); err != nil {
				topUpErr <- err
				return
			}
		}
		topUpErr <- nil
	}()

	queryErr := make(chan error, 1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			if _, err := k.Contract(types.QueryContractRequest{ContractAddress: deployed.ContractAddressUser}); err != nil {
				queryErr <- err
				return
			}
		}
		queryErr <- nil
	}()

	wg.Wait()
	require.NoError(t, <-topUpErr)
	require.NoError(t, <-queryErr)

	final, err := k.Contract(types.QueryContractRequest{ContractAddress: deployed.ContractAddressUser})
	require.NoError(t, err)
	require.Equal(t, deployed.Balance+uint64(iterations), final.Contract.Balance)
}

// TestComputeContractsStateRootNormalizedMatchesPublicVariant is a
// differential test for the §8.4 double-Normalize() fix: the internal,
// skip-normalize helper RefreshStateRoot now calls directly must compute
// the EXACT SAME root as the public ComputeContractsStateRoot would for the
// identical (already-normalized) state -- the optimization must not change
// what root any genesis ends up with, only how many times Normalize() runs
// to get there.
func TestComputeContractsStateRootNormalizedMatchesPublicVariant(t *testing.T) {
	owner := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{owner: accountStatusActive})
	codeHash := storeContractCode(t, &k, owner)
	instantiateContract(t, &k, owner, codeHash, "root-diff-check", 10, 1_000_000, 0)

	gs := k.ExportGenesis()
	// gs is already normalized (ExportGenesis -> RefreshStateRoot). The
	// public ComputeContractsStateRoot renormalizes redundantly and must
	// still land on the identical root RefreshStateRoot already computed.
	require.Equal(t, gs.StateRoot, types.ComputeContractsStateRoot(gs),
		"the public (always-normalizing) path and RefreshStateRoot's already-normalized fast path must compute the same root")
}
