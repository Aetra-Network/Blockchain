package keeper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"

	corestore "cosmossdk.io/core/store"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/store/v2/dbadapter"
	"github.com/cosmos/cosmos-sdk/store/v2/gaskv"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/contracts/types"
	"github.com/sovereign-l1/l1/x/internal/kvtest"
)

// This file pins the storage layout that keeps a contract transaction's gas
// independent of unrelated module state. See persistence.go for the full
// write-up; the short version is that the module used to serialize every code
// record, contract and receipt into ONE KV value on every write, so gas was
// ~33 gas per byte of TOTAL module state and contracts sat at the MaxTxGas
// ceiling.

// meteredKeeper builds a keeper over a store charged the SAME production gas
// prices the chain charges (storetypes.KVGasConfig(): WriteCostFlat 2000,
// WriteCostPerByte 30, ReadCostFlat 1000, ReadCostPerByte 3). A test that
// counts Set calls instead would not notice per-byte write amplification at
// all -- that is exactly the cost this layout exists to remove.
func meteredKeeper(t *testing.T) (*Keeper, storetypes.GasMeter) {
	t.Helper()
	parent := dbadapter.Store{DB: dbm.NewMemDB()}
	meter := storetypes.NewInfiniteGasMeter()
	metered := gaskv.NewStore(parent, meter, storetypes.KVGasConfig())
	k := NewPersistentKeeper(meteredService{store: metered})
	k.accountStatusReader = testAccountStatus{growthWallet: accountStatusActive}
	return &k, meter
}

type meteredService struct{ store storetypes.KVStore }

func (s meteredService) OpenKVStore(context.Context) corestore.KVStore { return meteredKV{s.store} }

type meteredKV struct{ store storetypes.KVStore }

func (k meteredKV) Get(key []byte) ([]byte, error) { return k.store.Get(key), nil }
func (k meteredKV) Has(key []byte) (bool, error)   { return k.store.Has(key), nil }
func (k meteredKV) Set(key, value []byte) error    { k.store.Set(key, value); return nil }
func (k meteredKV) Delete(key []byte) error        { k.store.Delete(key); return nil }
func (k meteredKV) Iterator(s, e []byte) (corestore.Iterator, error) {
	return k.store.Iterator(s, e), nil
}
func (k meteredKV) ReverseIterator(s, e []byte) (corestore.Iterator, error) {
	return k.store.ReverseIterator(s, e), nil
}

var growthWallet = aeAddress("11")

func growthGenesis() types.GenesisState {
	gs := types.DefaultGenesis()
	gs.Params.Enabled = true
	return types.RefreshStateRoot(gs)
}

func growthCodeMsg(i int) types.MsgStoreCode {
	sum := sha256.Sum256([]byte(fmt.Sprintf("growth-code-%d", i)))
	return types.MsgStoreCode{
		Authority: growthWallet,
		CodeHash:  hex.EncodeToString(sum[:]),
		CodeBytes: 2048,
	}
}

// TestStoreCodeGasDoesNotGrowWithUnrelatedCodeCount pins the fix for the
// unbounded term in a contract transaction's gas cost.
//
// Gas is charged per byte WRITTEN (30/byte, ten times the read cost), and the
// module wrote its entire state on every mutation, so storing one code re-wrote
// every other dapp's bytecode and paid for it. Measured through a real
// gas-metered store, storing one code against N unrelated codes cost:
//
//	before: N=0    28,710 gas   ->  after: N=0     32,810 gas
//	before: N=10  105,591 gas   ->  after: N=10    42,911 gas
//	before: N=50  413,151 gas   ->  after: N=50    79,751 gas
//	before: N=100 797,601 gas   ->  after: N=100  125,801 gas
//
// i.e. the per-unrelated-record slope fell from ~7,689 gas to ~930 gas (8.3x),
// and what remains is the READ scan at 3 gas/byte -- not the 30 gas/byte write
// that was pushing contracts through MaxTxGas = 1,000,000.
//
// The invariant: adding one code must cost about the same whether the module
// holds nothing or holds a hundred other people's codes.
func TestStoreCodeGasDoesNotGrowWithUnrelatedCodeCount(t *testing.T) {
	ctx := context.Background()

	measure := func(n int) uint64 {
		t.Helper()
		k, meter := meteredKeeper(t)
		require.NoError(t, k.InitGenesisState(ctx, growthGenesis()))
		srv := NewGRPCMsgServer(k)
		for i := 0; i < n; i++ {
			msg := growthCodeMsg(i)
			_, err := srv.StoreCode(ctx, &msg)
			require.NoError(t, err)
		}
		before := meter.GasConsumed()
		msg := growthCodeMsg(1_000_000 + n)
		_, err := srv.StoreCode(ctx, &msg)
		require.NoError(t, err)
		return meter.GasConsumed() - before
	}

	atZero := measure(0)
	atHundred := measure(100)
	slope := float64(atHundred-atZero) / 100

	t.Logf("StoreCode gas: N=0 %d, N=100 %d, slope %.1f gas/record", atZero, atHundred, slope)

	// Pre-fix this slope was ~7,689 gas per unrelated record, which is what put
	// StoreCode/Deploy/Execute over MaxTxGas at a handful of contracts. The
	// bound is deliberately far above the measured ~930 so the test pins the
	// COLLAPSE rather than a specific gas number that innocent changes perturb.
	require.Less(t, slope, 2_000.0,
		"storing a code must not re-pay for every unrelated code in the module")
	require.Less(t, atHundred, uint64(200_000),
		"one StoreCode against 100 unrelated codes must stay far under MaxTxGas=1,000,000")
}

// TestExecuteGasDoesNotGrowWithOwnReceiptCount is the important one.
//
// Contract COUNT was never the binding constraint -- lifetime OPERATION count
// was. Every operation appends a ContractReceipt, and the receipt log lived in
// the same blob as everything else, so with ONE code and ONE contract and
// nothing else in state a single contract bricked ITSELF:
//
//	exec#1   blob  5,557   gas   191,964   ok
//	exec#50  blob 19,032   gas   639,399   ok
//	exec#90  blob 30,112   gas 1,005,039   OVER MaxTxGas
//
// MaxRetainedReceipts = 8192 is real but sits ~91x beyond the gas wall, so the
// bound never engaged before the module stopped working -- and because every
// contract shared the one key, it took the whole module with it. One wallet
// paying ordinary fees could permanently disable a contract in 90 txs.
//
// Appending a receipt is now one new small record instead of a rewrite of the
// whole log, which moves the wall out by ~10x:
//
//	exec#1   88,984 gas      exec#249  349,567 gas
//	exec#99 191,854 gas      exec#499  612,817 gas
//
// i.e. ~1,053 gas per prior receipt, extrapolating to MaxTxGas at ~exec#866
// (was ~exec#90).
//
// BE HONEST ABOUT WHAT THIS DOES NOT FIX. The wall moved; it did not go away.
// The residual slope is the READ scan: ComputeContractsStateRoot hashes the
// WHOLE normalized state, so the read path must still reassemble every record,
// at 3 gas/byte. That is forced by the root formula, which must stay
// byte-identical or every node forks -- so O(state) reads cannot be removed
// without redefining the root as a fold over per-record hashes. Consequently
// MaxRetainedReceipts = 8192 STILL sits ~9.5x beyond the gas wall (8192
// receipts would be ~8.6M gas), so the bound still never engages before the
// module stops working. See persistence.go.
//
// The invariant this test pins: repeated execution of one contract must not
// make the next execution multiples more expensive, and #99 must fit in a block.
func TestExecuteGasDoesNotGrowWithOwnReceiptCount(t *testing.T) {
	ctx := context.Background()
	k, meter := meteredKeeper(t)
	require.NoError(t, k.InitGenesisState(ctx, growthGenesis()))
	srv := NewGRPCMsgServer(k)

	code := growthCodeMsg(7)
	stored, err := srv.StoreCode(ctx, &code)
	require.NoError(t, err)
	// Funded well past the storage rent the 99 blocks below accrue: this test is
	// about the gas of writing receipts, not about rent.
	deployed, err := srv.DeployContract(ctx, &types.MsgDeployContract{
		Creator:        growthWallet,
		CodeID:         stored.CodeID,
		Salt:           "receipt-growth",
		InitPayload:    []byte("init"),
		InitialBalance: 1_000_000_000,
		Admin:          growthWallet,
		Height:         1,
	})
	require.NoError(t, err)

	executeOnce := func(height uint64) uint64 {
		t.Helper()
		before := meter.GasConsumed()
		_, err := srv.ExecuteExternal(ctx, &types.MsgExecuteExternal{
			Sender:          growthWallet,
			ContractAddress: deployed.ContractAddressUser,
			Payload:         []byte("call"),
			GasLimit:        k.Params().MaxGasPerExecution,
			Height:          height,
		})
		require.NoError(t, err)
		return meter.GasConsumed() - before
	}

	first := executeOnce(2)
	for height := uint64(3); height < 100; height++ {
		executeOnce(height)
	}
	ninetyNinth := executeOnce(100)

	t.Logf("Execute gas: #1 %d, #99 %d (delta %d)", first, ninetyNinth, int64(ninetyNinth)-int64(first))

	// Pre-fix, execution #90 alone crossed MaxTxGas = 1,000,000.
	require.Less(t, ninetyNinth, uint64(1_000_000),
		"a contract must not brick itself by being used: execution #99 must fit in MaxTxGas")
	// Each execution appends one receipt, which is one new small record rather
	// than a rewrite of the whole log, so the growth per execution is the read
	// scan only. Allow generous headroom over the measured value while still
	// failing hard if the blob behaviour ever returns.
	require.Less(t, ninetyNinth, first*4,
		"execution #99 must not cost multiples of execution #1")
}

// TestContractsExportImportExportRoundTripsByteIdentically proves the record
// split did not change the module's logical state, only where its bytes live.
func TestContractsExportImportExportRoundTripsByteIdentically(t *testing.T) {
	ctx := context.Background()
	service := kvtest.NewStoreService()
	k := NewPersistentKeeper(service)
	k.accountStatusReader = testAccountStatus{growthWallet: accountStatusActive}
	require.NoError(t, k.InitGenesisState(ctx, growthGenesis()))
	seedContractState(t, ctx, &k)

	exported, err := k.ExportGenesisState(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, exported.State.Codes)
	require.NotEmpty(t, exported.State.Contracts)
	require.NotEmpty(t, exported.State.Receipts)

	first, err := json.Marshal(exported)
	require.NoError(t, err)

	// Import into a completely fresh store, then export again.
	target := NewPersistentKeeper(kvtest.NewStoreService())
	target.accountStatusReader = testAccountStatus{growthWallet: accountStatusActive}
	require.NoError(t, target.InitGenesisState(ctx, exported))
	reExported, err := target.ExportGenesisState(ctx)
	require.NoError(t, err)

	second, err := json.Marshal(reExported)
	require.NoError(t, err)
	require.Equal(t, string(first), string(second), "export -> import -> export must round-trip byte-identically")
	require.Equal(t, exported.StateRoot, reExported.StateRoot, "the state root must survive a round trip")
}

// TestContractsRestartedKeeperReadsBackExactlyWhatWasWritten is the F-17
// regression guard, ported to the per-record layout.
//
// A restarted or state-synced node does not re-run InitGenesis, so it starts
// with the empty in-memory default. If it cannot rebuild the committed state
// from the store it diverges from a continuously-running node and the chain
// forks. With state spread over many keys instead of one, this is the test that
// proves the prefix scans reassemble it exactly.
func TestContractsRestartedKeeperReadsBackExactlyWhatWasWritten(t *testing.T) {
	ctx := context.Background()
	service := kvtest.NewStoreService()

	source := NewPersistentKeeper(service)
	source.accountStatusReader = testAccountStatus{growthWallet: accountStatusActive}
	require.NoError(t, source.InitGenesisState(ctx, growthGenesis()))
	seedContractState(t, ctx, &source)
	want, err := source.ExportGenesisState(ctx)
	require.NoError(t, err)

	restarted := NewPersistentKeeper(service)
	restarted.accountStatusReader = testAccountStatus{growthWallet: accountStatusActive}
	require.Empty(t, restarted.ExportGenesis().State.Codes, "a fresh keeper starts at the empty default in memory")

	require.NoError(t, restarted.loadForBlock(ctx))
	got := restarted.ExportGenesis()

	require.Equal(t, want.StateRoot, got.StateRoot, "a restarted node must agree on the state root or the chain forks")
	require.Equal(t, want.State.Codes, got.State.Codes)
	require.Equal(t, want.State.Contracts, got.State.Contracts)
	require.Equal(t, want.State.Receipts, got.State.Receipts)
}

// TestContractsStateRootIsUnchangedForTheSameLogicalState pins that the record
// split is invisible to consensus. ComputeContractsStateRoot hashes the whole
// normalized State as one JSON string; that formula is deliberately untouched,
// because computing it costs CPU and zero gas. The same logical state must hash
// identically no matter which keys its bytes were stored under.
func TestContractsStateRootIsUnchangedForTheSameLogicalState(t *testing.T) {
	ctx := context.Background()
	service := kvtest.NewStoreService()
	k := NewPersistentKeeper(service)
	k.accountStatusReader = testAccountStatus{growthWallet: accountStatusActive}
	require.NoError(t, k.InitGenesisState(ctx, growthGenesis()))
	seedContractState(t, ctx, &k)

	exported, err := k.ExportGenesisState(ctx)
	require.NoError(t, err)

	// The root of the reassembled state must equal the root computed directly
	// from the same logical state by the untouched formula.
	require.Equal(t, types.ComputeContractsStateRoot(exported), exported.StateRoot)

	// And a keeper with NO store at all -- which never touches a key -- must
	// derive the identical root from the identical logical state.
	memOnly := NewKeeper()
	require.NoError(t, memOnly.InitGenesis(exported))
	require.Equal(t, exported.StateRoot, memOnly.ExportGenesis().StateRoot,
		"the state root must not depend on the storage layout")
}

// TestContractsLegacyBlobFansOutToPerRecordKeys covers the upgrade path from a
// store written before this layout existed: everything inside the one blob at
// genesisKey, no per-record keys at all.
//
// This is the case that silently eats state if it is got wrong. On such a store
// the per-record keys hold NOTHING, so the write baseline for those collections
// must be EMPTY -- if it were seeded from the blob's copy instead, the first
// write would diff every pre-upgrade record as "unchanged", skip it, and then
// rewrite the residual without those records: every pre-upgrade code and
// contract would be deleted.
func TestContractsLegacyBlobFansOutToPerRecordKeys(t *testing.T) {
	ctx := context.Background()
	service := kvtest.NewStoreService()

	// Build a realistic state, then re-write it the OLD way: one blob holding
	// everything, and no per-record keys.
	seed := NewPersistentKeeper(service)
	seed.accountStatusReader = testAccountStatus{growthWallet: accountStatusActive}
	require.NoError(t, seed.InitGenesisState(ctx, growthGenesis()))
	seedContractState(t, ctx, &seed)
	legacy, err := seed.ExportGenesisState(ctx)
	require.NoError(t, err)

	raw := service.RawStore()
	for key := range raw.Snapshot() {
		require.NoError(t, raw.Delete([]byte(key)))
	}
	blob, err := json.Marshal(legacy)
	require.NoError(t, err)
	require.NoError(t, raw.Set(genesisKey, blob))

	// A node coming up on that store must see the full state...
	upgraded := NewPersistentKeeper(service)
	upgraded.accountStatusReader = testAccountStatus{growthWallet: accountStatusActive}
	require.NoError(t, upgraded.loadForBlock(ctx))
	require.Equal(t, legacy.StateRoot, upgraded.ExportGenesis().StateRoot,
		"a node upgrading over a legacy blob must not change the state root")

	// ...and the first write must fan every record out rather than drop it.
	code := growthCodeMsg(4242)
	_, err = NewGRPCMsgServer(&upgraded).StoreCode(ctx, &code)
	require.NoError(t, err)

	for _, want := range legacy.State.Codes {
		bz, err := raw.Get(types.CodeKey(want.CodeID))
		require.NoError(t, err)
		require.NotEmpty(t, bz, "pre-upgrade code %s must survive the fan-out", want.CodeID)
	}
	for _, want := range legacy.State.Contracts {
		bz, err := raw.Get(types.ContractKey(want.AddressUser))
		require.NoError(t, err)
		require.NotEmpty(t, bz, "pre-upgrade contract %s must survive the fan-out", want.AddressUser)
	}

	// The residual blob must no longer carry the split-out collections, or the
	// bytes would be stored twice and the read path would have to choose.
	residual, err := raw.Get(genesisKey)
	require.NoError(t, err)
	var after types.GenesisState
	require.NoError(t, json.Unmarshal(residual, &after))
	require.Empty(t, after.State.Codes, "the residual blob must not keep a second copy of the codes")
	require.Empty(t, after.State.Contracts, "the residual blob must not keep a second copy of the contracts")
	require.Empty(t, after.State.Receipts, "the residual blob must not keep a second copy of the receipts")

	// And a restart over the fanned-out store must still agree on every record.
	restarted := NewPersistentKeeper(service)
	restarted.accountStatusReader = testAccountStatus{growthWallet: accountStatusActive}
	require.NoError(t, restarted.loadForBlock(ctx))
	got := restarted.ExportGenesis()
	require.Len(t, got.State.Codes, len(legacy.State.Codes)+1)
	require.Equal(t, legacy.State.Contracts, got.State.Contracts)
	require.Equal(t, legacy.State.Receipts, got.State.Receipts)
}

// TestContractsImportOverPopulatedStoreRemovesUnmentionedRecords pins that a
// genesis import is a REPLACE, not a merge. Per-record keys are authoritative,
// so a record left behind by an import would be resurrected by the next read.
func TestContractsImportOverPopulatedStoreRemovesUnmentionedRecords(t *testing.T) {
	ctx := context.Background()
	service := kvtest.NewStoreService()
	k := NewPersistentKeeper(service)
	k.accountStatusReader = testAccountStatus{growthWallet: accountStatusActive}
	require.NoError(t, k.InitGenesisState(ctx, growthGenesis()))
	seedContractState(t, ctx, &k)
	populated, err := k.ExportGenesisState(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, populated.State.Codes)

	// Import an empty genesis over that populated store.
	fresh := NewPersistentKeeper(service)
	fresh.accountStatusReader = testAccountStatus{growthWallet: accountStatusActive}
	require.NoError(t, fresh.InitGenesisState(ctx, growthGenesis()))

	reread := NewPersistentKeeper(service)
	reread.accountStatusReader = testAccountStatus{growthWallet: accountStatusActive}
	require.NoError(t, reread.loadForBlock(ctx))
	got := reread.ExportGenesis()
	require.Empty(t, got.State.Codes, "an import must delete the records the imported genesis does not mention")
	require.Empty(t, got.State.Contracts)
	require.Empty(t, got.State.Receipts)

	for _, gone := range populated.State.Codes {
		bz, err := service.RawStore().Get(types.CodeKey(gone.CodeID))
		require.NoError(t, err)
		require.Empty(t, bz, "code %s must not survive an import that omits it", gone.CodeID)
	}
}

// seedContractState drives the real handlers to build a state with codes,
// contracts and receipts in it.
func seedContractState(t *testing.T, ctx context.Context, k *Keeper) {
	t.Helper()
	srv := NewGRPCMsgServer(k)
	for i := 0; i < 3; i++ {
		code := growthCodeMsg(i)
		stored, err := srv.StoreCode(ctx, &code)
		require.NoError(t, err)
		deployed, err := srv.DeployContract(ctx, &types.MsgDeployContract{
			Creator:        growthWallet,
			CodeID:         stored.CodeID,
			Salt:           fmt.Sprintf("seed-%d", i),
			InitPayload:    []byte("init"),
			InitialBalance: 1_000_000_000,
			Admin:          growthWallet,
			Height:         uint64(10 + i),
		})
		require.NoError(t, err)
		_, err = srv.ExecuteExternal(ctx, &types.MsgExecuteExternal{
			Sender:          growthWallet,
			ContractAddress: deployed.ContractAddressUser,
			Payload:         []byte("call"),
			GasLimit:        k.Params().MaxGasPerExecution,
			Height:          uint64(11 + i),
		})
		require.NoError(t, err)
	}
}
