package conformance

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/aetravm/compiler"
)

// TestPaginationBoundedPageGasIndependentOfCollectionSize is the core
// conformance proof for examples/avm/collections/pagination_stdlib.atlx: it
// drives the real compiler + AVM (mirroring the send/query harness style
// already established by TestNestedMapCompilesAndExecutes in
// nested_map_test.go and orderBookHarness in order_book_acceptance_test.go)
// through a collection large enough (128 entries -- PAGE_SIZE=16 *
// NUM_PAGES=8) that an unbounded full scan is clearly distinguishable from
// one bounded page fetch, and proves the property pagination exists for:
// fetching page 0 costs EXACTLY the same gas whether the rest of the
// collection is empty or completely full, while the deliberately-unbounded
// contrast getter's gas genuinely grows with total collection size.
func TestPaginationBoundedPageGasIndependentOfCollectionSize(t *testing.T) {
	h := newPaginationHarness(t)
	deployer := testAddress(0x21)
	sender := testAddress(0x22)

	// stateOnePage: exactly one page's worth of data (16 entries, page0
	// full, pages 1-7 never touched / absent from storage entirely).
	stateOnePage := avm.Storage{}
	for i := 0; i < 16; i++ {
		stateOnePage, _ = h.insert(stateOnePage, deployer, sender, int64(9000+i))
	}

	// stateFull: the collection driven all the way to its fixed capacity
	// (128 entries, every one of the 8 pages full). page0's own 16
	// entries are byte-identical to stateOnePage's -- only pages 1-7
	// differ.
	stateFull := avm.Storage{}
	for i := 0; i < 128; i++ {
		stateFull, _ = h.insert(stateFull, deployer, sender, int64(9000+i))
	}

	// --- correctness: page0's contents are identical in both states ---
	for slot := uint64(0); slot < 16; slot++ {
		onePageVal := h.pageEntryValueAt(stateOnePage, 0, slot)
		fullVal := h.pageEntryValueAt(stateFull, 0, slot)
		require.Equal(t, onePageVal, fullVal, "page0 slot %d must read identically regardless of how many OTHER pages are populated", slot)
		require.Equal(t, big.NewInt(9000+int64(slot)), onePageVal)
	}
	require.Equal(t, uint64(16), h.pageEntryCount(stateOnePage, 0))
	require.Equal(t, uint64(16), h.pageEntryCount(stateFull, 0))

	// --- the core gas-invariance property ---
	countOnePageExec := h.query(stateOnePage, "pageEntryCount", u64ArgCodec, map[string]any{"arg0": uint64(0)})
	countFullExec := h.query(stateFull, "pageEntryCount", u64ArgCodec, map[string]any{"arg0": uint64(0)})
	require.Equal(t, countOnePageExec.GasUsed, countFullExec.GasUsed,
		"reading page 0 must cost the SAME gas whether the collection holds 16 entries or 128 -- page-fetch gas must be bounded by PAGE_SIZE, never by total collection size")
	require.NotZero(t, countOnePageExec.GasUsed)

	valOnePageExec := h.query(stateOnePage, "pageEntryValueAt", u64u64ArgCodec, map[string]any{"arg0": uint64(0), "arg1": uint64(15)})
	valFullExec := h.query(stateFull, "pageEntryValueAt", u64u64ArgCodec, map[string]any{"arg0": uint64(0), "arg1": uint64(15)})
	require.Equal(t, valOnePageExec.GasUsed, valFullExec.GasUsed,
		"reading one page slot must also cost the same gas regardless of collection size")

	// Reading a DIFFERENT page (page7, which does not even exist yet in
	// stateOnePage) must still be cheap and bounded -- it does not
	// somehow inherit page0's or the whole collection's size.
	emptyPage7Exec := h.query(stateOnePage, "pageEntryCount", u64ArgCodec, map[string]any{"arg0": uint64(7)})
	require.Equal(t, uint64(0), mustUint64(t, emptyPage7Exec))
	fullPage7Exec := h.query(stateFull, "pageEntryCount", u64ArgCodec, map[string]any{"arg0": uint64(7)})
	require.Equal(t, uint64(16), mustUint64(t, fullPage7Exec))

	// --- the contrast: an intentionally unbounded scan grows with size,
	// and costs strictly more than any single bounded page read on the
	// SAME (large) collection. ---
	scanOnePageExec := h.query(stateOnePage, "scanAllUnboundedCount", compiler.Codec{}, nil)
	scanFullExec := h.query(stateFull, "scanAllUnboundedCount", compiler.Codec{}, nil)
	require.Equal(t, uint64(16), mustUint64(t, scanOnePageExec))
	require.Equal(t, uint64(128), mustUint64(t, scanFullExec))
	require.Greater(t, scanFullExec.GasUsed, scanOnePageExec.GasUsed,
		"the unbounded scan's gas must grow as the collection grows -- this is the exact cost pagination exists to avoid")
	require.Greater(t, scanFullExec.GasUsed, countFullExec.GasUsed,
		"on the SAME large collection, the unbounded full scan must cost strictly more than one bounded page fetch")

	t.Logf("bounded pageEntryCount(0) gas: onePage=%d full=%d (equal, as required)", countOnePageExec.GasUsed, countFullExec.GasUsed)
	t.Logf("unbounded scanAllUnboundedCount gas: onePage=%d full=%d (grows with collection size)", scanOnePageExec.GasUsed, scanFullExec.GasUsed)
}

// TestPaginationCursorWalksEveryEntryInOrder proves the cursor-based paging
// API (pageEntryCount/pageEntryValueAt/nextCursor) actually reconstructs the
// full, correctly-ordered collection when a caller pages through it end to
// end, and that nextCursor's sentinel correctly signals "no more pages".
func TestPaginationCursorWalksEveryEntryInOrder(t *testing.T) {
	h := newPaginationHarness(t)
	deployer := testAddress(0x23)
	sender := testAddress(0x24)

	const n = 37 // spans page0(16) + page1(16) + partial page2(5)
	state := avm.Storage{}
	for i := 0; i < n; i++ {
		state, _ = h.insert(state, deployer, sender, int64(500+i))
	}

	var collected []*big.Int
	cursor := uint64(0)
	pages := 0
	for {
		count := h.pageEntryCount(state, cursor)
		for slot := uint64(0); slot < count; slot++ {
			collected = append(collected, h.pageEntryValueAt(state, cursor, slot))
		}
		pages++
		require.Less(t, pages, 20, "pagination loop must terminate well within NUM_PAGES")
		next := h.nextCursorOf(state, cursor)
		if next == cursor || count == 0 || next >= 8 {
			break
		}
		cursor = next
	}

	require.Len(t, collected, n, "walking every page via the cursor API must recover every inserted entry, no more and no fewer")
	for i, v := range collected {
		require.Equal(t, big.NewInt(500+int64(i)), v, "entries must come back in original insertion order (position %d)", i)
	}

	require.Equal(t, uint64(16), h.pageEntryCount(state, 0))
	require.Equal(t, uint64(16), h.pageEntryCount(state, 1))
	require.Equal(t, uint64(5), h.pageEntryCount(state, 2))
	require.Equal(t, uint64(0), h.pageEntryCount(state, 3))
	require.Equal(t, uint64(n), h.totalCountOf(state))

	// nextCursor is pure page-index arithmetic capped at NUM_PAGES (8),
	// independent of how much data actually exists.
	require.Equal(t, uint64(1), h.nextCursorOf(state, 0))
	require.Equal(t, uint64(7), h.nextCursorOf(state, 6))
	require.Equal(t, uint64(8), h.nextCursorOf(state, 7), "the last valid page must advance to the NUM_PAGES sentinel")
	require.Equal(t, uint64(8), h.nextCursorOf(state, 8), "the sentinel must be idempotent (stays at NUM_PAGES)")

	// Out-of-range slot reads within an existing page return 0, not an
	// error or another entry's value.
	require.Equal(t, big.NewInt(0), h.pageEntryValueAt(state, 2, 5), "slot 5 of the partially-filled page2 (only slots 0-4 occupied) must read as 0")
}

// TestPaginationCapacityIsEnforced proves the fixed compile-time capacity
// (NUM_PAGES*PAGE_SIZE = 128) is a hard, trapping limit, not a silently
// wrapping or overflowing one -- and that a rejected Insert leaves storage
// completely untouched (deterministic rollback).
func TestPaginationCapacityIsEnforced(t *testing.T) {
	h := newPaginationHarness(t)
	deployer := testAddress(0x25)
	sender := testAddress(0x26)

	state := avm.Storage{}
	for i := 0; i < 128; i++ {
		state, _ = h.insert(state, deployer, sender, int64(i))
	}
	require.Equal(t, uint64(128), h.totalCountOf(state))

	body := mustCodecBody(t, h.res.MessageBodies["Insert"], map[string]any{"value": big.NewInt(999)})
	exec, err := h.runner.Run(h.res.Module, state, avm.RuntimeContext{
		Entry:           avm.EntryReceiveInternal,
		ContractAddress: deployer,
		GasLimit:        20_000_000,
		Message: async.MessageEnvelope{
			Source:   append([]byte(nil), sender...),
			Opcode:   h.res.MessageBodyOpcodes["Insert"],
			QueryID:  uint64(h.res.MessageBodyOpcodes["Insert"]),
			Body:     body,
			GasLimit: 20_000_000,
		},
	})
	require.NotEqualf(t, async.ResultOK, exec.ResultCode, "inserting past fixed capacity must trap (err=%v)", err)
	require.Equal(t, state, exec.State, "a trapped Insert must leave storage byte-for-byte untouched")
}

// TestPaginationInsertGasBoundedAcrossFreshPages is the write-side
// counterpart to TestPaginationBoundedPageGasIndependentOfCollectionSize:
// Insert ends every branch with a bare `st.save()` statement, which the
// compiler now compiles away entirely (see the "case save:" comment in
// x/aetravm/compiler/compile.go) instead of emitting a real whole-snapshot
// OpReadStorage+OpWriteStorage pair. Before that fix, `st.save()` billed gas
// proportional to the CONTRACT'S ENTIRE storage footprint, so inserting the
// first entry into a fresh page cost dramatically more once other pages were
// already populated -- entirely defeating this file's own page-sharding
// design for its one mutating message. This test proves that no longer
// happens: the first insert into a fresh page costs roughly the same gas
// whether it is the contract's very first-ever write or its 113th (spanning
// 7 already-full prior pages), modulo the few extra gas units a strictly
// larger `nextId` value costs to encode -- not the ~14x blowup the O(total
// storage) bug used to cause.
func TestPaginationInsertGasBoundedAcrossFreshPages(t *testing.T) {
	h := newPaginationHarness(t)
	deployer := testAddress(0x27)
	sender := testAddress(0x28)

	// firstEverGas: the contract's very first write, storage completely
	// empty beforehand.
	empty := avm.Storage{}
	_, firstExec := h.insert(empty, deployer, sender, 111)
	firstEverGas := firstExec.GasUsed

	// freshPageAfter112Gas: the first entry of page7, with pages 0-6 (112
	// entries) already full. Under the pre-fix behavior this was billed
	// against ALL 112 prior entries' bytes on top of its own; under the fix
	// it should cost about the same as firstEverGas.
	loaded := avm.Storage{}
	for i := 0; i < 112; i++ {
		loaded, _ = h.insert(loaded, deployer, sender, int64(i))
	}
	_, freshPageExec := h.insert(loaded, deployer, sender, 222)
	freshPageAfter112Gas := freshPageExec.GasUsed

	require.NotZero(t, firstEverGas)
	require.Less(t, freshPageAfter112Gas, firstEverGas*2,
		"inserting into a brand-new page must stay within a small constant factor of the contract's very first write, "+
			"regardless of how many entries already exist in OTHER pages -- a bounded O(total storage) regression would blow this up ~14x")
	t.Logf("Insert gas: first-ever=%d, first-of-fresh-page-after-112-prior-entries=%d", firstEverGas, freshPageAfter112Gas)
}

// --- harness -----------------------------------------------------------

var (
	u64ArgCodec    = compiler.Codec{Fields: []compiler.CodecField{{Name: "arg0", Type: compiler.TypeRef{Name: "uint64"}}}}
	u64u64ArgCodec = compiler.Codec{Fields: []compiler.CodecField{
		{Name: "arg0", Type: compiler.TypeRef{Name: "uint64"}},
		{Name: "arg1", Type: compiler.TypeRef{Name: "uint64"}},
	}}
)

type paginationHarness struct {
	t      *testing.T
	res    *compiler.Result
	runner *avm.Runner
}

func newPaginationHarness(t *testing.T) *paginationHarness {
	t.Helper()
	res := compileExampleFile(t, "collections/pagination_stdlib.atlx", compiler.Options{})
	require.NoError(t, avm.VerifyInterface(res.Module, res.Manifest))
	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	return &paginationHarness{t: t, res: res, runner: runner}
}

func (h *paginationHarness) insert(state avm.Storage, contract, sender []byte, value int64) (avm.Storage, avm.Execution) {
	h.t.Helper()
	body := mustCodecBody(h.t, h.res.MessageBodies["Insert"], map[string]any{"value": big.NewInt(value)})
	exec, err := h.runner.Run(h.res.Module, state, avm.RuntimeContext{
		Entry:           avm.EntryReceiveInternal,
		ContractAddress: contract,
		GasLimit:        20_000_000,
		Message: async.MessageEnvelope{
			Source:   append([]byte(nil), sender...),
			Opcode:   h.res.MessageBodyOpcodes["Insert"],
			QueryID:  uint64(h.res.MessageBodyOpcodes["Insert"]),
			Body:     body,
			GasLimit: 20_000_000,
		},
	})
	require.NoError(h.t, err)
	require.Equal(h.t, async.ResultOK, exec.ResultCode, "Insert result")
	return exec.State, exec
}

func (h *paginationHarness) query(state avm.Storage, name string, codec compiler.Codec, args map[string]any) avm.Execution {
	h.t.Helper()
	var body []byte
	if args != nil {
		body = mustCodecBody(h.t, codec, args)
	}
	exec, err := h.runner.Run(h.res.Module, state, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		Message:  async.MessageEnvelope{Opcode: opcodeForGetter(h.t, h.res, name), Body: body, GasLimit: 5_000_000},
		GasLimit: 5_000_000,
	})
	require.NoError(h.t, err)
	require.Equalf(h.t, async.ResultOK, exec.ResultCode, "getter %s", name)
	return exec
}

func (h *paginationHarness) pageEntryCount(state avm.Storage, cursor uint64) uint64 {
	h.t.Helper()
	return mustUint64(h.t, h.query(state, "pageEntryCount", u64ArgCodec, map[string]any{"arg0": cursor}))
}

func (h *paginationHarness) pageEntryValueAt(state avm.Storage, cursor, slot uint64) *big.Int {
	h.t.Helper()
	exec := h.query(state, "pageEntryValueAt", u64u64ArgCodec, map[string]any{"arg0": cursor, "arg1": slot})
	v, err := exec.ReturnValue.AsBigInt()
	require.NoError(h.t, err)
	return v
}

func (h *paginationHarness) nextCursorOf(state avm.Storage, cursor uint64) uint64 {
	h.t.Helper()
	return mustUint64(h.t, h.query(state, "nextCursor", u64ArgCodec, map[string]any{"arg0": cursor}))
}

func (h *paginationHarness) totalCountOf(state avm.Storage) uint64 {
	h.t.Helper()
	return mustUint64(h.t, h.query(state, "totalCount", compiler.Codec{}, nil))
}

func mustUint64(t *testing.T, exec avm.Execution) uint64 {
	t.Helper()
	v, err := exec.ReturnValue.AsUint64()
	require.NoError(t, err)
	return v
}
