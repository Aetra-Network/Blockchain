package keeper

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/internal/kvtest"
	"github.com/sovereign-l1/l1/x/internal/prototype"
	"github.com/sovereign-l1/l1/x/nominator-pool/types"
	validatorregistrytypes "github.com/sovereign-l1/l1/x/validator-registry/types"
)

// TestConcurrentLoadForBlockAndLookupDoesNotRace is a regression guard for
// F-14 (CRITICAL): the Keeper used to hold its entire module state
// (k.genesis) plus a derived index (k.indexes, a plain Go map) in process
// memory, outside the KV store, with NO synchronization at all.
//
// baseapp runs Msg execution for BOTH execModeFinalize (a real block, on the
// consensus goroutine) and execModeSimulate (the public
// /cosmos.tx.v1beta1.Service/Simulate RPC, served on a query goroutine)
// through the exact same msg-handler code path -- only execModeCheck
// short-circuits differently. That means a client hammering the public
// Simulate endpoint with nominator-pool messages ran rebuildIndexes() --
// which reassigns k.indexes, a WRITE to a map shared with every other
// goroutine touching this keeper -- concurrently with a real block's
// FinalizeBlock reading that same map via lookupPool/lookupDelegator.
//
// A concurrent Go map read/write is not an ordinary panic: it is a
// runtime.throw ("fatal error: concurrent map read and map write" or
// "concurrent map writes"), which recover() cannot catch, and which
// therefore crashes the entire validator process -- reachable by anyone with
// RPC access, zero privilege required.
//
// This test drives that exact shape directly against the keeper: several
// goroutines repeatedly call the unexported loadForBlock (which mutates
// k.genesis and, via rebuildIndexes, reassigns k.indexes) concurrently with
// several goroutines repeatedly calling the read-only NominatorPool /
// PoolDelegator query methods (which read k.indexes via
// lookupPool/lookupDelegator, and can themselves trigger a rebuild through
// ensureIndexes). Before the mutex fix this reliably crashed the whole test
// binary -- not merely a `go test` failure, but a hard process exit -- within
// a handful of iterations, even without -race, because the Go runtime's map
// implementation detects concurrent access unconditionally, independent of
// the race detector. Running this file under `go test -race` additionally
// covers the plain (non-map) data races on k.genesis/k.counters that don't
// hit the map fast-path check but are still real races.
func TestConcurrentLoadForBlockAndLookupDoesNotRace(t *testing.T) {
	ctx := context.Background()
	service := kvtest.NewStoreService()
	k := NewPersistentKeeper(service)
	require.NoError(t, k.InitGenesisState(ctx, DefaultGenesis()))

	const poolCount = 8
	poolIDs := make([]string, poolCount)
	for i := 0; i < poolCount; i++ {
		poolID := fmt.Sprintf("concurrent-pool-%03d", i)
		_, err := k.CreateNominatorPool(types.MsgCreateNominatorPool{
			Authority:         prototype.DefaultAuthority,
			PoolID:            poolID,
			PoolOperator:      rawPoolAddressFromInt(2*i + 1),
			ValidatorTarget:   rawPoolAddressFromInt(2*i + 2),
			PoolCommissionBps: 100,
			Height:            1,
			ValidatorStatus:   validatorregistrytypes.StatusActive,
		})
		require.NoError(t, err)
		poolIDs[i] = poolID
	}
	delegator := rawPoolAddress("33")

	const writers = 8
	const readers = 8
	const iterations = 300

	var wg sync.WaitGroup
	wg.Add(writers + readers)

	// Writers: simulate concurrent Msg-handler entry points (loadForBlock is
	// what msg_server.go calls at the top of every handler -- see F-14) --
	// this is the call that reassigns k.indexes via rebuildIndexes.
	for w := 0; w < writers; w++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				_ = k.loadForBlock(ctx)
			}
		}()
	}
	// Readers: simulate concurrent query_server.go gRPC traffic (and, per
	// F-14, concurrent Simulate traffic) reading through the same indexes.
	for r := 0; r < readers; r++ {
		go func(idx int) {
			defer wg.Done()
			poolID := poolIDs[idx%poolCount]
			for i := 0; i < iterations; i++ {
				k.NominatorPool(poolID)
				k.PoolDelegator(poolID, delegator)
				k.PoolRewards(poolID, delegator)
			}
		}(r)
	}
	wg.Wait()
}
