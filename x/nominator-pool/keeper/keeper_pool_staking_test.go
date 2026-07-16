package keeper

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/internal/kvtest"
	"github.com/sovereign-l1/l1/x/internal/prefixgenesis"
	"github.com/sovereign-l1/l1/x/internal/prototype"
	"github.com/sovereign-l1/l1/x/nominator-pool/types"
)

type accountStatusFixture map[string]string

func (f accountStatusFixture) AccountStatus(address string) (string, bool) {
	status, found := f[address]
	return status, found
}

func TestPoolDepositMintsReceiptAndKeepsRawInternal(t *testing.T) {
	user := aePoolAddress(t, "22")
	k := NewKeeperWithAccountStatus(accountStatusFixture{user: accountStatusActive})
	pool := createOfficialLiquidStakingPool(t, &k, "official-staking")

	receipt, err := k.DepositToStakingPool(types.MsgDepositToStakingPool{
		PoolID:		pool.PoolID,
		WalletAddress:	user,
		Amount:		types.DefaultMinPoolDeposit,
		Height:		2,
	})
	require.NoError(t, err)
	require.Equal(t, user, receipt.OwnerAddress)
	require.Equal(t, pool.ContractAddressUser, receipt.PoolContractAddressUser)
	require.Equal(t, types.DefaultMinPoolDeposit, receipt.Shares)
	require.Equal(t, types.DefaultParams().PoolReceiptDenomOrCodeID, receipt.ReceiptToken)
	require.Equal(t, rawPoolAddress("22"), receipt.InternalMetadata.OwnerRaw)
	require.Equal(t, pool.ContractAddressRaw, receipt.InternalMetadata.PoolContractAddressRaw)
	require.Equal(t, []string{
		string(types.PoolKey(pool.PoolID)),
		string(types.PoolShareKey(pool.PoolID, user)),
	}, receipt.InternalMetadata.TouchedKeys)

	exported := k.ExportGenesis()
	require.Len(t, exported.State.PoolShares, 1)
	require.Equal(t, user, exported.State.PoolShares[0].Owner)
	require.Equal(t, types.DefaultMinPoolDeposit, exported.State.PoolShares[0].Shares)
	require.Len(t, exported.State.LiquidStakingPools, 1)
	require.Equal(t, pool.ContractAddressUser, exported.State.LiquidStakingPools[0].ContractAddressUser)
	require.Equal(t, pool.ContractAddressRaw, exported.State.LiquidStakingPools[0].ContractAddressRaw)

	_, err = k.DepositToStakingPool(types.MsgDepositToStakingPool{
		PoolID:			pool.PoolID,
		WalletAddress:		user,
		ReservedRouting:	aePoolAddress(t, "33"),
		Amount:			types.DefaultMinPoolDeposit,
		Height:			3,
	})
	require.ErrorContains(t, err, "must not include a routing field")
}

func TestPersistentRuntimeMutationSurvivesRestartAndImport(t *testing.T) {
	ctx := context.Background()
	user := aePoolAddress(t, "52")
	service := kvtest.NewStoreService()
	source := NewPersistentKeeper(service)
	source.accountStatusReader = accountStatusFixture{user: accountStatusActive}
	require.NoError(t, source.InitGenesisState(ctx, DefaultGenesis()))
	pool := createOfficialLiquidStakingPool(t, &source, "official-persistent")

	receipt, err := source.DepositToStakingPool(types.MsgDepositToStakingPool{
		PoolID:		pool.PoolID,
		WalletAddress:	user,
		Amount:		types.DefaultMinPoolDeposit,
		Height:		2,
	})
	require.NoError(t, err)

	restarted := NewPersistentKeeper(service)
	exported, err := restarted.ExportGenesisState(ctx)
	require.NoError(t, err)
	require.Len(t, exported.State.Pools, 1)
	require.Len(t, exported.State.PoolShares, 1)
	require.Equal(t, receipt.Shares, exported.State.PoolShares[0].Shares)

	imported := NewPersistentKeeper(kvtest.NewStoreService())
	imported.accountStatusReader = accountStatusFixture{user: accountStatusActive}
	require.NoError(t, imported.InitGenesisState(ctx, exported))
	share, found := imported.PoolShare(types.QueryPoolShareRequest{PoolID: pool.PoolID, Delegator: rawPoolAddress("52")})
	require.True(t, found)
	require.Equal(t, receipt.Shares, share.Share.Shares)
}

// TestRestartedKeeperDivergesOnDepositToStakingPool documents FINDING-006
// (security-audit/05-findings/FINDING-006-inmemory-genesis-not-rehydrated-consensus-halt.md)
// AT THE BARE-KEEPER LEVEL: DepositToOfficialLiquidStaking's pool lookup
// (findPool(k.genesis.State.Pools, ...)) reads the process-local in-memory
// k.genesis directly, never the committed store, and calling a *Keeper method
// directly (bypassing msgServer) still exhibits the divergence -- the keeper
// itself was intentionally NOT patched to self-heal on every call (that would
// mean reloading from the store on every single method invocation, including
// nested keeper-to-keeper calls within one handler, which is wasteful).
//
// THE ACTUAL FIX lives one layer up: msgServer.DepositToStakingPool (and every
// other nominator-pool msg handler) now calls Keeper.loadForBlock(ctx) before
// touching state -- see msg_server.go and TestRestartedNodeViaMsgServerNoLongerDivergesOnDepositToStakingPool
// below, which proves the REAL production entry point (the only way a
// consensus tx ever reaches this keeper) no longer diverges. This test is kept
// as a targeted regression guard for the bare-keeper path and as documentation
// that bare *Keeper method calls are NOT self-healing -- any new code path
// that calls keeper methods directly (not through msgServer) must call
// loadForBlock itself first.
func TestRestartedKeeperDivergesOnDepositToStakingPool(t *testing.T) {
	ctx := context.Background()
	user := aePoolAddress(t, "71")
	service := kvtest.NewStoreService()

	// Continuously-running node: create the pool and deposit once.
	source := NewPersistentKeeper(service)
	source.accountStatusReader = accountStatusFixture{user: accountStatusActive}
	require.NoError(t, source.InitGenesisState(ctx, DefaultGenesis()))
	pool := createOfficialLiquidStakingPool(t, &source, "official-restart-divergence")

	sourceReceipt, err := source.DepositToStakingPool(types.MsgDepositToStakingPool{
		PoolID:		pool.PoolID,
		WalletAddress:	user,
		Amount:		types.DefaultMinPoolDeposit,
		Height:		2,
	})
	require.NoError(t, err, "the continuously-running node must accept the deposit")
	require.Equal(t, types.DefaultMinPoolDeposit, sourceReceipt.Shares)

	// Sanity: the pool IS committed to the shared store (this is what a
	// restarted node's store-backed read would see).
	committed, err := NewPersistentKeeper(service).ExportGenesisState(ctx)
	require.NoError(t, err)
	require.Len(t, committed.State.Pools, 1, "pool must be committed to the shared store")

	// Simulate a validator restart / state-sync join: a fresh keeper over the
	// SAME committed store, without InitGenesis (which only runs on InitChain,
	// never on a later process start -- see app/wiring PreBlockers, which do
	// not include a nominator-pool rehydration step).
	restarted := NewPersistentKeeper(service)
	restarted.accountStatusReader = accountStatusFixture{user: accountStatusActive}
	require.Empty(t, restarted.genesis.State.Pools, "a freshly-constructed keeper starts at the empty default in memory, per NewPersistentKeeper")

	// The SAME deposit message that succeeded on the continuously-running node
	// is now delivered to the restarted node (as would happen at the next
	// block height once both nodes are back in sync). It fails, because
	// DepositToOfficialLiquidStaking's pool lookup reads k.genesis.State.Pools
	// directly -- never the committed store the restarted keeper was
	// constructed over.
	_, err = restarted.DepositToStakingPool(types.MsgDepositToStakingPool{
		PoolID:		pool.PoolID,
		WalletAddress:	user,
		Amount:		types.DefaultMinPoolDeposit,
		Height:		3,
	})
	require.ErrorContains(t, err, "not found",
		"DIVERGENCE PROVEN: the restarted node rejects a deposit into a pool that demonstrably exists in the committed store (see 'committed' above), "+
			"while the continuously-running node accepted the identical message. Two honest nodes now produce different DeliverTx results / AppHash for the same (height, tx) -- a consensus-safety violation.")
}

// TestRestartedNodeViaMsgServerNoLongerDivergesOnDepositToStakingPool is the
// FIX-VERIFICATION counterpart to TestRestartedKeeperDivergesOnDepositToStakingPool
// above. It drives the SAME restart scenario through the REAL production
// consensus entry point -- msgServer, exactly as CometBFT/baseapp invokes it
// for every delivered tx -- instead of calling the bare *Keeper method
// directly. x/nominator-pool has no BeginBlocker/EndBlocker, so msgServer is
// the ONLY way a consensus tx ever reaches this keeper; msgServer.
// DepositToStakingPool now calls Keeper.loadForBlock(ctx) before touching any
// state (see msg_server.go), which reloads k.genesis from the committed store
// on every invocation. This test asserts the restarted node's msgServer call
// SUCCEEDS -- i.e. the AppHash/DeliverTx-result divergence that
// TestRestartedKeeperDivergesOnDepositToStakingPool demonstrates at the
// bare-keeper level cannot happen on the real consensus path anymore.
func TestRestartedNodeViaMsgServerNoLongerDivergesOnDepositToStakingPool(t *testing.T) {
	ctx := context.Background()
	user := aePoolAddress(t, "72")
	// msgServer.DepositToStakingPool normalizes WalletAddress to the account's
	// v2 identity BEFORE calling the keeper (see msg_server.go), so
	// ensureActiveWallet checks the NORMALIZED identity, not the raw address --
	// unlike the bare-keeper test above, which bypasses that normalization.
	userIdentity, err := normalizeAccountIdentity(user)
	require.NoError(t, err)
	service := kvtest.NewStoreService()

	// Continuously-running node: create the pool and deposit once, through the
	// real msgServer entry point.
	source := NewPersistentKeeper(service)
	source.accountStatusReader = accountStatusFixture{userIdentity: accountStatusActive}
	require.NoError(t, source.InitGenesisState(ctx, DefaultGenesis()))
	pool := createOfficialLiquidStakingPool(t, &source, "official-restart-fixed")
	sourceServer := NewMsgServerImpl(&source)

	sourceResp, err := sourceServer.DepositToStakingPool(ctx, &types.MsgDepositToStakingPool{
		PoolID:		pool.PoolID,
		WalletAddress:	user,
		Amount:		types.DefaultMinPoolDeposit,
		Height:		2,
	})
	require.NoError(t, err, "the continuously-running node must accept the deposit")
	require.Equal(t, types.DefaultMinPoolDeposit, sourceResp.Shares)

	// Sanity: the pool IS committed to the shared store.
	committed, err := NewPersistentKeeper(service).ExportGenesisState(ctx)
	require.NoError(t, err)
	require.Len(t, committed.State.Pools, 1, "pool must be committed to the shared store")

	// Simulate a validator restart / state-sync join: a fresh keeper over the
	// SAME committed store, without InitGenesis -- identical setup to the
	// vulnerability-demonstration test above.
	restarted := NewPersistentKeeper(service)
	restarted.accountStatusReader = accountStatusFixture{userIdentity: accountStatusActive}
	require.Empty(t, restarted.genesis.State.Pools, "a freshly-constructed keeper starts at the empty default in memory")
	restartedServer := NewMsgServerImpl(&restarted)

	// The SAME deposit message, delivered through the REAL msgServer entry
	// point this time. It must succeed: DepositToStakingPool's loadForBlock
	// call reloads k.genesis from the committed store before any pool lookup.
	restartedResp, err := restartedServer.DepositToStakingPool(ctx, &types.MsgDepositToStakingPool{
		PoolID:		pool.PoolID,
		WalletAddress:	user,
		Amount:		types.DefaultMinPoolDeposit,
		Height:		3,
	})
	require.NoError(t, err,
		"FIX VERIFIED: the restarted node must accept the deposit via the real msgServer entry point, "+
			"matching the continuously-running node -- no more AppHash/DeliverTx-result divergence on restart.")
	// Shares in the response is the delegator's CUMULATIVE total after this
	// deposit (see DepositToOfficialLiquidStaking), not the amount minted by
	// this call alone -- so after a second equal deposit at an unchanged 1:1
	// exchange rate it must be 2x the per-deposit amount.
	require.Equal(t, 2*types.DefaultMinPoolDeposit, restartedResp.Shares)

	// Both nodes must now agree on the pool's total shares.
	finalState, err := restarted.ExportGenesisState(ctx)
	require.NoError(t, err)
	require.Len(t, finalState.State.PoolShares, 1)
	require.Equal(t, 2*types.DefaultMinPoolDeposit, finalState.State.PoolShares[0].Shares,
		"restarted node's deposit must accumulate on top of the pre-restart deposit from the shared store")
}

// TestFailedTxLeavesPhantomInMemoryStateThatNextWriteResurrects documents
// FINDING-007 (security-audit/05-findings/FINDING-007-inmemory-mutation-not-rolled-back.md)
// AT THE BARE-KEEPER LEVEL: saveGenesis (keeper.go:128-139) unconditionally
// assigns k.genesis = cloneGenesis(next) BEFORE the KV write, and every write
// handler builds its `next` genesis via cloneGenesis(k.genesis) -- i.e. off
// the in-memory field. Calling *Keeper methods directly (bypassing msgServer,
// as this test does) still exhibits the phantom-resurrection defect, because
// the keeper itself was never patched to reload on every call (see the
// bare-keeper-vs-msgServer note on TestRestartedKeeperDivergesOnDepositToStakingPool
// above).
//
// THE ACTUAL FIX: msgServer.CreateOfficialLiquidStakingPool now calls
// Keeper.loadForBlock(ctx) before touching state, so any handler invocation
// -- including the very next one, whether from the same or a different tx --
// starts from a fresh read of the committed store, not from whatever the
// in-memory field happened to retain. See
// TestPhantomStateCannotResurrectThroughMsgServer below, which proves the
// real production entry point discards the phantom instead of resurrecting
// it.
func TestFailedTxLeavesPhantomInMemoryStateThatNextWriteResurrects(t *testing.T) {
	ctx := context.Background()
	service := kvtest.NewStoreService()
	k := NewPersistentKeeper(service)
	require.NoError(t, k.InitGenesisState(ctx, DefaultGenesis()))

	preTxStore, err := k.ExportGenesisState(ctx)
	require.NoError(t, err)
	require.Empty(t, preTxStore.State.Pools, "store starts with no pools")

	// msg1 of "tx1": succeeds, mutates k.genesis in-memory AND writes to the
	// store via k.runtimeCtx (bound by InitGenesisState above).
	poolA, err := k.CreateOfficialLiquidStakingPool(types.MsgCreateOfficialLiquidStakingPool{
		Authority:		prototype.DefaultAuthority,
		PoolID:			"pool-a-reverted-tx",
		ContractAddressUser:	aePoolAddress(t, "81"),
		ContractAddressRaw:	rawPoolAddress("81"),
		PoolOperator:		rawPoolAddress("82"),
		PoolCommissionBps:	100,
		Height:			2,
		ValidatorTarget:	rawPoolAddress("85"),
	})
	require.NoError(t, err)
	require.Equal(t, "pool-a-reverted-tx", poolA.PoolID)

	// "msg2 of tx1 fails" -> BaseApp discards tx1's KV cache branch, which
	// reverts msg1's store write. Simulate that discard directly on the shared
	// store by writing back the pre-tx1 (empty-pools) genesis -- this is
	// exactly what the store looks like after BaseApp abandons the cache
	// branch. Crucially, this does NOT touch k.genesis (the keeper's Go
	// field), because nothing in this codebase resets it on tx failure --
	// which is precisely the defect under test.
	require.NoError(t, prefixgenesis.Save(ctx, service, genesisKey, preTxStore))

	postDiscardStore, err := k.ExportGenesisState(ctx)
	require.NoError(t, err)
	require.Empty(t, postDiscardStore.State.Pools,
		"the committed store has been reverted to contain NO pools (models BaseApp's cache-branch discard)")

	require.Len(t, k.ExportGenesis().State.Pools, 1,
		"PHANTOM STATE PROVEN: despite the store now holding zero pools, the keeper's in-memory k.genesis still carries pool-a-reverted-tx -- "+
			"the in-memory mutation was never rolled back because it lives outside Cosmos's cache-multistore")

	// "Tx2": an unrelated, independently-successful write on the SAME live
	// keeper (exactly what happens on a real node processing the next tx in
	// the mempool -- no restart required).
	poolB, err := k.CreateOfficialLiquidStakingPool(types.MsgCreateOfficialLiquidStakingPool{
		Authority:		prototype.DefaultAuthority,
		PoolID:			"pool-b-unrelated-tx",
		ContractAddressUser:	aePoolAddress(t, "91"),
		ContractAddressRaw:	rawPoolAddress("91"),
		PoolOperator:		rawPoolAddress("92"),
		PoolCommissionBps:	100,
		Height:			3,
		ValidatorTarget:	rawPoolAddress("95"),
	})
	require.NoError(t, err)
	require.Equal(t, "pool-b-unrelated-tx", poolB.PoolID)

	final, err := k.ExportGenesisState(ctx)
	require.NoError(t, err)
	poolIDs := make([]string, 0, len(final.State.Pools))
	for _, p := range final.State.Pools {
		poolIDs = append(poolIDs, p.PoolID)
	}
	require.ElementsMatch(t, []string{"pool-a-reverted-tx", "pool-b-unrelated-tx"}, poolIDs,
		"RESURRECTION PROVEN: tx2's write serialized the WHOLE in-memory blob (which still carried the phantom pool-a), "+
			"permanently committing a pool from a transaction that was supposed to have been reverted -- silent on-chain state corruption with no restart involved")
}

// TestPhantomStateCannotResurrectThroughMsgServer is the FIX-VERIFICATION
// counterpart to TestFailedTxLeavesPhantomInMemoryStateThatNextWriteResurrects
// above. It reproduces the identical "tx1 mutates then gets reverted, tx2
// writes something unrelated" scenario, but drives BOTH steps through the real
// msgServer entry point instead of bare *Keeper calls. Because msgServer.
// CreateOfficialLiquidStakingPool now calls Keeper.loadForBlock(ctx) before
// touching state, tx2's handler invocation reloads k.genesis from the
// committed store FIRST -- discarding whatever phantom mutation the in-memory
// field carried from tx1 -- so the final committed state contains ONLY the
// tx2 pool, never a resurrected tx1 pool.
func TestPhantomStateCannotResurrectThroughMsgServer(t *testing.T) {
	ctx := context.Background()
	service := kvtest.NewStoreService()
	k := NewPersistentKeeper(service)
	require.NoError(t, k.InitGenesisState(ctx, DefaultGenesis()))
	server := NewMsgServerImpl(&k)

	preTxStore, err := k.ExportGenesisState(ctx)
	require.NoError(t, err)
	require.Empty(t, preTxStore.State.Pools, "store starts with no pools")

	// "tx1" via the real msgServer entry point: succeeds, mutates k.genesis
	// in-memory AND writes to the store.
	_, err = server.CreateOfficialLiquidStakingPool(ctx, &types.MsgCreateOfficialLiquidStakingPool{
		Authority:		prototype.DefaultAuthority,
		PoolID:			"pool-a-reverted-tx-fixed",
		ContractAddressUser:	aePoolAddress(t, "83"),
		ContractAddressRaw:	rawPoolAddress("83"),
		PoolOperator:		rawPoolAddress("84"),
		PoolCommissionBps:	100,
		Height:			2,
		ValidatorTarget:	rawPoolAddress("85"),
	})
	require.NoError(t, err)

	// "msg2 of tx1 fails" -> BaseApp discards tx1's KV cache branch. Simulate
	// that discard directly on the shared store, identical to the
	// vulnerability-demonstration test -- this still does NOT touch k.genesis.
	require.NoError(t, prefixgenesis.Save(ctx, service, genesisKey, preTxStore))
	postDiscardStore, err := k.ExportGenesisState(ctx)
	require.NoError(t, err)
	require.Empty(t, postDiscardStore.State.Pools, "store reverted to zero pools, modeling the cache-branch discard")

	// "tx2" via the real msgServer entry point: an unrelated, independently
	// successful write. Its loadForBlock call must discard the phantom before
	// building `next`.
	_, err = server.CreateOfficialLiquidStakingPool(ctx, &types.MsgCreateOfficialLiquidStakingPool{
		Authority:		prototype.DefaultAuthority,
		PoolID:			"pool-b-unrelated-tx-fixed",
		ContractAddressUser:	aePoolAddress(t, "93"),
		ContractAddressRaw:	rawPoolAddress("93"),
		PoolOperator:		rawPoolAddress("94"),
		PoolCommissionBps:	100,
		Height:			3,
		ValidatorTarget:	rawPoolAddress("95"),
	})
	require.NoError(t, err)

	final, err := k.ExportGenesisState(ctx)
	require.NoError(t, err)
	poolIDs := make([]string, 0, len(final.State.Pools))
	for _, p := range final.State.Pools {
		poolIDs = append(poolIDs, p.PoolID)
	}
	require.Equal(t, []string{"pool-b-unrelated-tx-fixed"}, poolIDs,
		"FIX VERIFIED: only the tx2 pool is committed -- loadForBlock discarded the tx1 phantom before tx2's write, "+
			"so the reverted pool was never resurrected")
}

func TestPoolDepositRejectsInactiveFrozenLowAndFrozenLimitedPool(t *testing.T) {
	active := aePoolAddress(t, "21")
	inactive := aePoolAddress(t, "22")
	frozen := aePoolAddress(t, "23")
	k := NewKeeperWithAccountStatus(accountStatusFixture{
		active:		accountStatusActive,
		inactive:	accountStatusInactive,
		frozen:		accountStatusFrozen,
	})
	pool := createOfficialLiquidStakingPool(t, &k, "official-rejects")

	_, err := k.DepositToStakingPool(types.MsgDepositToStakingPool{PoolID: pool.PoolID, WalletAddress: inactive, Amount: types.DefaultMinPoolDeposit, Height: 2})
	require.ErrorContains(t, err, "requires active wallet")

	_, err = k.DepositToStakingPool(types.MsgDepositToStakingPool{PoolID: pool.PoolID, WalletAddress: frozen, Amount: types.DefaultMinPoolDeposit, Height: 2})
	require.ErrorContains(t, err, "frozen wallet")

	_, err = k.DepositToStakingPool(types.MsgDepositToStakingPool{PoolID: pool.PoolID, WalletAddress: active, Amount: types.DefaultMinPoolDeposit - 1, Height: 2})
	require.ErrorContains(t, err, "below configured minimum")

	gs := k.ExportGenesis()
	gs.State.Pools[0].Status = types.PoolStatusFrozenLimited
	gs.State.LiquidStakingPools[0].Status = types.PoolStatusFrozenLimited
	require.NoError(t, k.InitGenesis(gs))
	_, err = k.DepositToStakingPool(types.MsgDepositToStakingPool{PoolID: pool.PoolID, WalletAddress: active, Amount: types.DefaultMinPoolDeposit, Height: 3})
	require.ErrorContains(t, err, "must be active for deposits")

	_, err = k.TopUpPoolReserve(types.MsgTopUpPoolReserve{PoolID: pool.PoolID, PayerAddress: active, Amount: 0, Height: 3})
	require.ErrorContains(t, err, "amount and height must be positive")

	_, err = k.TopUpPoolReserve(types.MsgTopUpPoolReserve{PoolID: pool.PoolID, PayerAddress: inactive, Amount: 1, Height: 3})
	require.ErrorContains(t, err, "requires active wallet")

	_, err = k.TopUpPoolReserve(types.MsgTopUpPoolReserve{PoolID: pool.PoolID, PayerAddress: frozen, Amount: 1, Height: 3})
	require.ErrorContains(t, err, "frozen wallet")
}

func TestFrozenLimitedPoolAllowsTopUpClaimUnbondAndMaturedWithdrawals(t *testing.T) {
	user := aePoolAddress(t, "24")
	k := NewKeeperWithAccountStatus(accountStatusFixture{user: accountStatusActive})
	pool := createOfficialLiquidStakingPool(t, &k, "official-exits")
	_, err := k.DepositToStakingPool(types.MsgDepositToStakingPool{PoolID: pool.PoolID, WalletAddress: user, Amount: 2 * types.DefaultMinPoolDeposit, Height: 2})
	require.NoError(t, err)
	_, err = k.ApplyPoolReward(pool.PoolID, 100)
	require.NoError(t, err)

	gs := k.ExportGenesis()
	gs.State.Pools[0].Status = types.PoolStatusFrozenLimited
	gs.State.LiquidStakingPools[0].Status = types.PoolStatusFrozenLimited
	gs.State.LiquidStakingPools[0].StorageRentDebt = 123
	require.NoError(t, k.InitGenesis(gs))

	topUp, err := k.TopUpPoolReserve(types.MsgTopUpPoolReserve{
		PoolID:		pool.PoolID,
		PayerAddress:	user,
		Amount:		50,
		Height:		3,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(50), topUp.StorageDebtPaid)
	require.Equal(t, []string{
		string(types.PoolKey(pool.PoolID)),
		string(types.PoolStorageDebtKey(pool.PoolID)),
	}, topUp.InternalMetadata.TouchedKeys)
	exportedAfterTopUp := k.ExportGenesis()
	require.Greater(t, exportedAfterTopUp.State.LiquidStakingPools[0].StorageRentDebt, uint64(0))
	require.Equal(t, types.PoolStatusFrozenLimited, exportedAfterTopUp.State.LiquidStakingPools[0].Status)

	claim, err := k.ClaimPoolRewardsWithReceipt(types.MsgClaimPoolRewards{PoolID: pool.PoolID, OwnerAddress: user, Height: 4})
	require.NoError(t, err)
	require.NotZero(t, claim.Amount)

	unbond, err := k.RequestPoolUnbond(types.MsgRequestPoolUnbond{
		PoolID:		pool.PoolID,
		OwnerAddress:	user,
		RequestID:	"unbond-1",
		Shares:		types.DefaultMinPoolDeposit,
		Height:		5,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(5)+k.ExportGenesis().Params.UnbondingBlocks, unbond.CompleteHeight)

	_, err = k.WithdrawPoolStake(types.MsgWithdrawPoolStake{
		CallerContractUser:	pool.ContractAddressUser,
		PoolID:			pool.PoolID,
		OwnerAddress:		user,
		RequestID:		"unbond-1",
		Height:			unbond.CompleteHeight - 1,
	})
	require.ErrorContains(t, err, "before unbonding period")

	withdrawal, err := k.WithdrawPoolStake(types.MsgWithdrawPoolStake{
		CallerContractUser:	pool.ContractAddressUser,
		PoolID:			pool.PoolID,
		OwnerAddress:		user,
		RequestID:		"unbond-1",
		Height:			unbond.CompleteHeight,
	})
	require.NoError(t, err)
	require.Equal(t, unbond.Amount, withdrawal.Amount)
	require.Contains(t, withdrawal.InternalMetadata.TouchedKeys, string(types.PoolUnbondingKey(pool.PoolID, user, "unbond-1")))
}

func TestValidatorRegistrationUpdateAndDuplicate(t *testing.T) {
	validator := aePoolAddress(t, "31")
	k := NewKeeperWithAccountStatus(accountStatusFixture{validator: accountStatusActive})

	_, err := k.RegisterValidator(types.MsgRegisterValidator{
		SignerAddress:		validator,
		ValidatorAddress:	validator,
		SelfStake:		types.DefaultMinValidatorStake - 1,
		CommissionBps:		types.DefaultParams().DefaultValidatorCommissionBps,
		Height:			1,
	})
	require.ErrorContains(t, err, "minimum validator stake")

	receipt, err := k.RegisterValidator(types.MsgRegisterValidator{
		SignerAddress:		validator,
		ValidatorAddress:	validator,
		SelfStake:		types.DefaultMinValidatorStake,
		CommissionBps:		types.DefaultParams().DefaultValidatorCommissionBps,
		Height:			2,
	})
	require.NoError(t, err)
	require.Equal(t, []string{string(types.ValidatorKey(validator))}, receipt.TouchedKeys)

	_, err = k.RegisterValidator(types.MsgRegisterValidator{
		SignerAddress:		validator,
		ValidatorAddress:	validator,
		SelfStake:		types.DefaultMinValidatorStake,
		CommissionBps:		types.DefaultParams().DefaultValidatorCommissionBps,
		Height:			3,
	})
	require.ErrorContains(t, err, "already registered")

	updated, err := k.UpdateValidator(types.MsgUpdateValidator{
		SignerAddress:		validator,
		ValidatorAddress:	validator,
		PerformanceScore:	9_500,
		CommissionBps:		types.DefaultParams().DefaultValidatorCommissionBps + 1,
		AllocationLimitBps:	types.MaxBasisPoints,
		Status:			types.StateValidatorStatusActive,
		Height:			4,
	})
	require.NoError(t, err)
	require.Equal(t, validator, updated.Validator)
}

func TestInjectAndRebalanceAllocationsAreDeterministicAndBounded(t *testing.T) {
	user := aePoolAddress(t, "40")
	v1 := aePoolAddress(t, "41")
	v2 := aePoolAddress(t, "42")
	v3 := aePoolAddress(t, "43")
	k := NewKeeperWithAccountStatus(accountStatusFixture{
		user:	accountStatusActive,
		v1:	accountStatusActive,
		v2:	accountStatusActive,
		v3:	accountStatusActive,
	})
	pool := createOfficialLiquidStakingPool(t, &k, "official-alloc")
	_, err := k.DepositToStakingPool(types.MsgDepositToStakingPool{PoolID: pool.PoolID, WalletAddress: user, Amount: 100 * types.DefaultAETBaseUnits, Height: 2})
	require.NoError(t, err)
	for _, validator := range []string{v1, v2, v3} {
		_, err := k.RegisterValidator(types.MsgRegisterValidator{
			SignerAddress:		validator,
			ValidatorAddress:	validator,
			SelfStake:		types.DefaultMinValidatorStake,
			CommissionBps:		types.DefaultParams().DefaultValidatorCommissionBps,
			Height:			3,
		})
		require.NoError(t, err)
	}

	injected, err := k.InjectPoolStake(types.MsgInjectPoolStake{
		CallerContractUser:	pool.ContractAddressUser,
		PoolID:			pool.PoolID,
		Height:			4,
		Allocations: []types.PoolAllocation{
			{ValidatorAddress: v2, Amount: 40 * types.DefaultAETBaseUnits, Height: 4},
			{ValidatorAddress: v1, Amount: 60 * types.DefaultAETBaseUnits, Height: 4},
		},
	})
	require.NoError(t, err)
	require.Len(t, injected.Allocations, 2)
	require.Equal(t, []string{
		string(types.PoolKey(pool.PoolID)),
		string(types.PoolAllocationKey(pool.PoolID, v2)),
		string(types.PoolAllocationKey(pool.PoolID, v1)),
	}, injected.InternalMetadata.TouchedKeys)

	beforeShare, found := k.PoolDelegator(pool.PoolID, rawPoolAddress("40"))
	require.True(t, found)
	rebalanced, err := k.RebalancePoolAllocations(types.MsgRebalancePoolAllocations{
		CallerContractUser:	pool.ContractAddressUser,
		PoolID:			pool.PoolID,
		Epoch:			1,
		Height:			5,
		Candidates: []types.ValidatorPolicyCandidate{
			{ValidatorAddress: v3, ReputationScore: 6_000, UptimeBps: 8_000, CommissionBps: 1_000, StakeEfficiencyBps: 7_000, SlashingRiskBps: 200, NetworkLoadBps: 2_000},
			{ValidatorAddress: v1, ReputationScore: 9_000, UptimeBps: 9_500, CommissionBps: 500, StakeEfficiencyBps: 9_000, SlashingRiskBps: 100, NetworkLoadBps: 1_000},
			{ValidatorAddress: v2, ReputationScore: 7_000, UptimeBps: 9_000, CommissionBps: 1_500, StakeEfficiencyBps: 8_000, SlashingRiskBps: 300, NetworkLoadBps: 2_500},
		},
	})
	require.NoError(t, err)
	require.Len(t, rebalanced.Allocations, 3)
	require.Equal(t, v2, rebalanced.Allocations[0].Validator)
	require.Equal(t, v3, rebalanced.Allocations[1].Validator)
	require.Equal(t, v1, rebalanced.Allocations[2].Validator)
	afterShare, found := k.PoolDelegator(pool.PoolID, rawPoolAddress("40"))
	require.True(t, found)
	require.Equal(t, beforeShare.Shares, afterShare.Shares)
	require.Equal(t, []string{
		string(types.PoolKey(pool.PoolID)),
		string(types.PoolAllocationKey(pool.PoolID, v2)),
		string(types.PoolAllocationKey(pool.PoolID, v3)),
		string(types.PoolAllocationKey(pool.PoolID, v1)),
	}, rebalanced.InternalMetadata.TouchedKeys)
}

func TestStakeReputationClaimTouchesOnlyShareKey(t *testing.T) {
	user := aePoolAddress(t, "50")
	k := NewKeeperWithAccountStatus(accountStatusFixture{user: accountStatusActive})
	pool := createOfficialLiquidStakingPool(t, &k, "official-reputation")
	_, err := k.DepositToStakingPool(types.MsgDepositToStakingPool{PoolID: pool.PoolID, WalletAddress: user, Amount: types.DefaultMinPoolDeposit, Height: 2})
	require.NoError(t, err)
	gs := k.ExportGenesis()
	gs.State.LiquidStakingPools[0].TotalActiveStake = types.DefaultMinPoolDeposit
	require.NoError(t, k.InitGenesis(gs))

	claim, err := k.ClaimStakeReputation(types.MsgClaimStakeReputation{PoolID: pool.PoolID, OwnerAddress: user, Height: 12})
	require.NoError(t, err)
	require.NotZero(t, claim.ReputationDelta)
	require.Equal(t, []string{
		string(types.PoolShareKey(pool.PoolID, user)),
	}, claim.InternalMetadata.TouchedKeys)

	exported := k.ExportGenesis()
	imported := NewKeeperWithAccountStatus(accountStatusFixture{user: accountStatusActive})
	require.NoError(t, imported.InitGenesis(exported))
}

func TestStakeReputationNoActiveExposureNoIncrease(t *testing.T) {
	user := aePoolAddress(t, "51")
	k := NewKeeperWithAccountStatus(accountStatusFixture{user: accountStatusActive})
	pool := createOfficialLiquidStakingPool(t, &k, "official-no-exposure")
	_, err := k.DepositToStakingPool(types.MsgDepositToStakingPool{PoolID: pool.PoolID, WalletAddress: user, Amount: types.DefaultMinPoolDeposit, Height: 2})
	require.NoError(t, err)

	claim, err := k.ClaimStakeReputation(types.MsgClaimStakeReputation{PoolID: pool.PoolID, OwnerAddress: user, Height: 12})
	require.NoError(t, err)
	require.Zero(t, claim.ReputationDelta)
	require.Equal(t, []string{string(types.PoolShareKey(pool.PoolID, user))}, claim.InternalMetadata.TouchedKeys)
}

func TestUpdateStakingParamsAlias(t *testing.T) {
	k := NewKeeper()
	next := k.ExportGenesis().Params
	next.TargetValidatorCount = 129
	updated, err := k.UpdateStakingParams(types.MsgUpdateStakingParams{Authority: prototype.DefaultAuthority, Params: next, Height: 2})
	require.NoError(t, err)
	require.Equal(t, uint32(129), updated.TargetValidatorCount)
}
