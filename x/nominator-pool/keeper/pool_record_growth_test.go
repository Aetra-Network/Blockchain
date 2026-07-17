package keeper

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/internal/kvtest"
	"github.com/sovereign-l1/l1/x/nominator-pool/types"
)

// poolRecordSize returns the byte length of the committed pool record -- the
// value whose size the chain charges write gas on for every mutation of that
// pool.
func poolRecordSize(t *testing.T, service *kvtest.StoreService, poolID string) int {
	t.Helper()
	bz, err := service.RawStore().Get(types.PoolKey(poolID))
	require.NoError(t, err)
	require.NotEmpty(t, bz, "the pool record must exist at PoolKey -- the off-chain indexer reads it there")
	return len(bz)
}

// TestPoolRecordDoesNotGrowWithDepositCount pins the fix for the unbounded term
// in a deposit's gas cost.
//
// The pool record is the hottest value this module writes, and gas is charged
// per byte written, so anything that accumulates inside it is re-paid for on
// every future deposit by every future depositor. pool.PendingDeposits used to
// accumulate one entry per deposit and was never drained or read, which made a
// deposit's cost grow with how many deposits the pool had EVER served rather
// than with how many depositors it has. Measured through the real app that took
// a single wallet's own deposit from 231,521 gas to 560,473 by its 100th, on
// course to cross MaxTxGas (1,000,000) around its 250th -- after which no
// deposit and no unbond fits in a block for ANY user of that pool, and every
// depositor's principal is trapped. One wallet paying ordinary fees could do
// that to a pool on purpose.
//
// The invariant that kills that whole class of bug: repeated deposits from an
// unchanging set of depositors must not grow the record at all.
func TestPoolRecordDoesNotGrowWithDepositCount(t *testing.T) {
	ctx := context.Background()
	service := kvtest.NewStoreService()
	user := aePoolAddress(t, "22")
	k := NewPersistentKeeper(service)
	k.accountStatusReader = accountStatusFixture{user: accountStatusActive}.byIdentity(t)
	require.NoError(t, k.InitGenesisState(ctx, DefaultGenesis()))
	pool := createOfficialLiquidStakingPool(t, &k, "growth-guard")

	depositOnce := func(height uint64) {
		t.Helper()
		_, err := k.DepositToStakingPool(types.MsgDepositToStakingPool{
			PoolID:        pool.PoolID,
			WalletAddress: user,
			Amount:        types.DefaultMinPoolDeposit,
			Height:        height,
		})
		require.NoError(t, err)
	}

	depositOnce(2)
	baseline := poolRecordSize(t, service, pool.PoolID)

	for height := uint64(3); height <= 60; height++ {
		depositOnce(height)
	}

	after := poolRecordSize(t, service, pool.PoolID)

	// The record is allowed to move by a few bytes: running totals like
	// TotalBondedStake are JSON numbers, so they gain a character each time
	// they cross a power of ten. That is O(log(amount)) -- capped at the ~20
	// digits of a uint64 -- and is not a function of the deposit count. What
	// must not happen is a per-deposit entry: the old PendingDeposits append
	// added ~90 bytes EVERY time, i.e. ~5,200 bytes across these 58 deposits.
	const digitGrowthAllowance = 64
	require.Less(t, after-baseline, digitGrowthAllowance,
		"58 further deposits from the SAME wallet must not meaningfully grow the pool record: "+
			"the depositor set never changed, so nothing about this pool should have gotten more "+
			"expensive to write. Growth proportional to the deposit count makes every future deposit "+
			"cost more until the pool bricks at MaxTxGas and traps everyone's principal.")

	// The accounting those deposits are actually for must still be recorded --
	// this guards against "fixing" the growth by dropping real state.
	var committed types.NominatorPool
	require.NoError(t, json.Unmarshal(mustGet(t, service, types.PoolKey(pool.PoolID)), &committed))
	require.Equal(t, 59*types.DefaultMinPoolDeposit, committed.TotalBondedStake,
		"every one of the 59 deposits must still be counted in the pool's bonded stake")
	require.Len(t, committed.DelegatorShares, 1, "one wallet must still hold exactly one delegator share")
	require.Empty(t, committed.PendingDeposits,
		"PendingDeposits has no consumer and nothing pends by the time it was written -- it must stay empty")
}

// TestPoolRecordGrowsOnlyPerDepositorNotPerDeposit states the growth that DOES
// remain, so the bound is a tested fact rather than a claim: the pool record
// carries one DelegatorShare per distinct depositor, so it grows per depositor
// and nothing else. That residual term is why a deposit's gas is still O(pool
// membership) -- see persistence.go.
func TestPoolRecordGrowsOnlyPerDepositorNotPerDeposit(t *testing.T) {
	ctx := context.Background()
	service := kvtest.NewStoreService()
	users := []string{aePoolAddress(t, "31"), aePoolAddress(t, "32"), aePoolAddress(t, "33")}
	fixture := accountStatusFixture{}
	for _, user := range users {
		fixture[user] = accountStatusActive
	}
	k := NewPersistentKeeper(service)
	k.accountStatusReader = fixture.byIdentity(t)
	require.NoError(t, k.InitGenesisState(ctx, DefaultGenesis()))
	pool := createOfficialLiquidStakingPool(t, &k, "growth-shape")

	sizes := make([]int, 0, len(users))
	for idx, user := range users {
		_, err := k.DepositToStakingPool(types.MsgDepositToStakingPool{
			PoolID:        pool.PoolID,
			WalletAddress: user,
			Amount:        types.DefaultMinPoolDeposit,
			Height:        uint64(2 + idx),
		})
		require.NoError(t, err)
		sizes = append(sizes, poolRecordSize(t, service, pool.PoolID))
	}

	first := sizes[1] - sizes[0]
	second := sizes[2] - sizes[1]
	require.Positive(t, first, "each new depositor adds a DelegatorShare to the pool record")
	require.Equal(t, first, second,
		"the per-depositor cost must be a constant, not compounding -- if these differ, something else is accumulating too")
}

// TestDepositRewritesOnlyTheDepositorsOwnRecords pins the property the gas fix
// rests on: a deposit must touch its own pool and its own share, and must not
// rewrite the records of depositors who had nothing to do with it.
//
// Every mutation used to re-serialize every pool and every share (on top of the
// whole module state as one blob), three times per deposit. That is what made
// gas O(module state) and bricked the pool at ~7 depositors. The KV contract
// the off-chain indexer depends on is that these keys hold these records --
// not that they are rewritten when nothing about them changed.
func TestDepositRewritesOnlyTheDepositorsOwnRecords(t *testing.T) {
	ctx := context.Background()
	service := kvtest.NewStoreService()
	bystander := aePoolAddress(t, "41")
	depositor := aePoolAddress(t, "42")
	k := NewPersistentKeeper(service)
	k.accountStatusReader = accountStatusFixture{
		bystander: accountStatusActive,
		depositor: accountStatusActive,
	}.byIdentity(t)
	require.NoError(t, k.InitGenesisState(ctx, DefaultGenesis()))
	pool := createOfficialLiquidStakingPool(t, &k, "touch-guard")

	_, err := k.DepositToStakingPool(types.MsgDepositToStakingPool{
		PoolID:        pool.PoolID,
		WalletAddress: bystander,
		Amount:        types.DefaultMinPoolDeposit,
		Height:        2,
	})
	require.NoError(t, err)

	service.RawStore().ResetWriteCounts()

	_, err = k.DepositToStakingPool(types.MsgDepositToStakingPool{
		PoolID:        pool.PoolID,
		WalletAddress: depositor,
		Amount:        types.DefaultMinPoolDeposit,
		Height:        3,
	})
	require.NoError(t, err)

	require.Zero(t, service.RawStore().SetCount(types.PoolShareKey(pool.PoolID, bystander)),
		"an unrelated depositor's share record must not be rewritten -- rewriting every share on every "+
			"deposit is what made gas grow with the number of users until deposits stopped fitting in a block")
	require.Zero(t, service.RawStore().DeleteCount(types.PoolShareKey(pool.PoolID, bystander)),
		"an unrelated depositor's share record must never be deleted")
	require.Equal(t, uint64(1), service.RawStore().SetCount(types.PoolShareKey(pool.PoolID, depositor)),
		"the depositor's own share record is written exactly once, even though the deposit path saves three times")
	require.Equal(t, uint64(1), service.RawStore().SetCount(types.PoolKey(pool.PoolID)),
		"the pool record is written exactly once per deposit, not once per internal save")

	// The bystander's committed record must still be intact and readable at the
	// key the indexer reads -- "not rewritten" must never mean "lost".
	var share types.PoolShare
	require.NoError(t, json.Unmarshal(mustGet(t, service, types.PoolShareKey(pool.PoolID, bystander)), &share))
	require.Equal(t, bystander, share.Owner)
	require.Equal(t, types.DefaultMinPoolDeposit, share.Shares)
}

// TestFullyUnbondedShareRecordIsDeleted guards the other side of diffed writes:
// a record that leaves state must leave the store. Writes used to be
// append-or-overwrite only, so a share that was fully unbonded vanished from
// module state but stayed readable at its key forever -- leaving the indexer
// serving a share whose owner no longer has one.
func TestFullyUnbondedShareRecordIsDeleted(t *testing.T) {
	ctx := context.Background()
	service := kvtest.NewStoreService()
	user := aePoolAddress(t, "51")
	k := NewPersistentKeeper(service)
	k.accountStatusReader = accountStatusFixture{user: accountStatusActive}.byIdentity(t)
	require.NoError(t, k.InitGenesisState(ctx, DefaultGenesis()))
	pool := createOfficialLiquidStakingPool(t, &k, "delete-guard")

	receipt, err := k.DepositToStakingPool(types.MsgDepositToStakingPool{
		PoolID:        pool.PoolID,
		WalletAddress: user,
		Amount:        types.DefaultMinPoolDeposit,
		Height:        2,
	})
	require.NoError(t, err)
	require.NotEmpty(t, mustGet(t, service, types.PoolShareKey(pool.PoolID, user)))

	_, err = k.RequestPoolUnbond(types.MsgRequestPoolUnbond{
		PoolID:       pool.PoolID,
		OwnerAddress: user,
		RequestID:    "delete-guard-1",
		Shares:       receipt.Shares,
		Height:       3,
	})
	require.NoError(t, err)

	exported, err := k.ExportGenesisState(ctx)
	require.NoError(t, err)
	require.Empty(t, exported.State.PoolShares, "unbonding every share removes it from module state")
	require.Empty(t, mustGet(t, service, types.PoolShareKey(pool.PoolID, user)),
		"a share that left module state must also leave the store, or the record outlives the state it mirrors")
}

// TestImportingGenesisOverPopulatedStoreRemovesUnmentionedPools guards the
// import path. Pool and share records are authoritative storage, not a mirror
// of some other copy, so a record left behind by an import is not cosmetic --
// the next read hands it back as live state. Importing a genesis that does not
// mention a pool must remove it, or the pool (and every share in it) is
// resurrected out of the previous chain's state.
func TestImportingGenesisOverPopulatedStoreRemovesUnmentionedPools(t *testing.T) {
	ctx := context.Background()
	service := kvtest.NewStoreService()
	user := aePoolAddress(t, "61")
	k := NewPersistentKeeper(service)
	k.accountStatusReader = accountStatusFixture{user: accountStatusActive}.byIdentity(t)
	require.NoError(t, k.InitGenesisState(ctx, DefaultGenesis()))
	pool := createOfficialLiquidStakingPool(t, &k, "import-orphan-guard")

	_, err := k.DepositToStakingPool(types.MsgDepositToStakingPool{
		PoolID:        pool.PoolID,
		WalletAddress: user,
		Amount:        types.DefaultMinPoolDeposit,
		Height:        2,
	})
	require.NoError(t, err)
	require.NotEmpty(t, mustGet(t, service, types.PoolKey(pool.PoolID)))
	require.NotEmpty(t, mustGet(t, service, types.PoolShareKey(pool.PoolID, user)))

	// Import a genesis with no pools at all over the same store.
	require.NoError(t, k.InitGenesisState(ctx, DefaultGenesis()))

	require.Empty(t, mustGet(t, service, types.PoolKey(pool.PoolID)),
		"a pool the imported genesis does not mention must not survive in the store")
	require.Empty(t, mustGet(t, service, types.PoolShareKey(pool.PoolID, user)),
		"a share the imported genesis does not mention must not survive in the store")

	// The read path is the thing that would resurrect it, so assert there too.
	reread, err := k.ExportGenesisState(ctx)
	require.NoError(t, err)
	require.Empty(t, reread.State.Pools, "the imported genesis had no pools; reading it back must find none")
	require.Empty(t, reread.State.PoolShares, "the imported genesis had no shares; reading it back must find none")
}

func mustGet(t *testing.T, service *kvtest.StoreService, key []byte) []byte {
	t.Helper()
	bz, err := service.RawStore().Get(key)
	require.NoError(t, err)
	return bz
}
