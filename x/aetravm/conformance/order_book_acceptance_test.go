package conformance

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/aetravm/compiler"
)

// orderBookHarness bundles the compiled module plus small helper closures
// for driving examples/avm/orderbook/order_book.atlx through the real
// compiler + VM, mirroring the send/query helper style already established
// by TestNestedMapCompilesAndExecutes in nested_map_test.go.
type orderBookHarness struct {
	t      *testing.T
	res    *compiler.Result
	runner *avm.Runner
}

func newOrderBookHarness(t *testing.T) *orderBookHarness {
	t.Helper()
	res := compileExampleFile(t, "orderbook/order_book.atlx", compiler.Options{})
	require.NoError(t, avm.VerifyInterface(res.Module, res.Manifest))
	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	return &orderBookHarness{t: t, res: res, runner: runner}
}

func (h *orderBookHarness) send(state avm.Storage, sender []byte, msgName string, fields map[string]any) (avm.Storage, avm.Execution) {
	h.t.Helper()
	body := mustCodecBody(h.t, h.res.MessageBodies[msgName], fields)
	exec, err := h.runner.Run(h.res.Module, state, avm.RuntimeContext{
		Entry:    avm.EntryReceiveInternal,
		GasLimit: 20_000_000,
		Message: async.MessageEnvelope{
			Source:   append([]byte(nil), sender...),
			Opcode:   h.res.MessageBodyOpcodes[msgName],
			QueryID:  uint64(h.res.MessageBodyOpcodes[msgName]),
			Body:     body,
			GasLimit: 20_000_000,
		},
	})
	require.NoErrorf(h.t, err, "send %s", msgName)
	require.Equalf(h.t, async.ResultOK, exec.ResultCode, "send %s result", msgName)
	return exec.State, exec
}

// sendExpectTrap is like send but asserts the message TRAPS (a deterministic
// rollback) and that storage is left byte-for-byte untouched.
func (h *orderBookHarness) sendExpectTrap(state avm.Storage, sender []byte, msgName string, fields map[string]any) {
	h.t.Helper()
	body := mustCodecBody(h.t, h.res.MessageBodies[msgName], fields)
	exec, err := h.runner.Run(h.res.Module, state, avm.RuntimeContext{
		Entry:    avm.EntryReceiveInternal,
		GasLimit: 20_000_000,
		Message: async.MessageEnvelope{
			Source:   append([]byte(nil), sender...),
			Opcode:   h.res.MessageBodyOpcodes[msgName],
			QueryID:  uint64(h.res.MessageBodyOpcodes[msgName]),
			Body:     body,
			GasLimit: 20_000_000,
		},
	})
	require.NotEqualf(h.t, async.ResultOK, exec.ResultCode, "send %s should have trapped (err=%v)", msgName, err)
	require.Equal(h.t, state, exec.State, "a trapped message must leave storage untouched")
}

func (h *orderBookHarness) queryU64(state avm.Storage, getter string, argTypes []compiler.TypeRef, args map[string]any) uint64 {
	h.t.Helper()
	var body []byte
	if len(argTypes) > 0 {
		fields := make([]compiler.CodecField, len(argTypes))
		for i, ty := range argTypes {
			fields[i] = compiler.CodecField{Name: argName(i), Type: ty}
		}
		body = mustCodecBody(h.t, compiler.Codec{Name: getter, Fields: fields}, args)
	}
	exec, err := h.runner.Run(h.res.Module, state, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		Message:  async.MessageEnvelope{Opcode: opcodeForGetter(h.t, h.res, getter), Body: body, GasLimit: 5_000_000},
		GasLimit: 5_000_000,
	})
	require.NoErrorf(h.t, err, "query %s", getter)
	require.Equalf(h.t, async.ResultOK, exec.ResultCode, "query %s result", getter)
	v, err := exec.ReturnValue.AsUint64()
	require.NoError(h.t, err)
	return v
}

func (h *orderBookHarness) queryBigInt(state avm.Storage, getter string, argTypes []compiler.TypeRef, args map[string]any) *big.Int {
	h.t.Helper()
	var body []byte
	if len(argTypes) > 0 {
		fields := make([]compiler.CodecField, len(argTypes))
		for i, ty := range argTypes {
			fields[i] = compiler.CodecField{Name: argName(i), Type: ty}
		}
		body = mustCodecBody(h.t, compiler.Codec{Name: getter, Fields: fields}, args)
	}
	exec, err := h.runner.Run(h.res.Module, state, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		Message:  async.MessageEnvelope{Opcode: opcodeForGetter(h.t, h.res, getter), Body: body, GasLimit: 5_000_000},
		GasLimit: 5_000_000,
	})
	require.NoErrorf(h.t, err, "query %s", getter)
	require.Equalf(h.t, async.ResultOK, exec.ResultCode, "query %s result", getter)
	v, err := exec.ReturnValue.AsBigInt()
	require.NoError(h.t, err)
	return v
}

func argName(i int) string {
	return [...]string{"arg0", "arg1", "arg2"}[i]
}

func (h *orderBookHarness) bestBid(state avm.Storage) uint64 {
	return h.queryU64(state, "bestBid", nil, nil)
}
func (h *orderBookHarness) bestAsk(state avm.Storage) uint64 {
	return h.queryU64(state, "bestAsk", nil, nil)
}
func (h *orderBookHarness) levelDepth(state avm.Storage, isBuy bool, tier uint64) uint64 {
	return h.queryU64(state, "levelDepth", []compiler.TypeRef{{Name: "bool"}, {Name: "uint64"}}, map[string]any{"arg0": isBuy, "arg1": tier})
}
func (h *orderBookHarness) orderAmount(state avm.Storage, orderID uint64) *big.Int {
	return h.queryBigInt(state, "orderAmount", []compiler.TypeRef{{Name: "uint64"}}, map[string]any{"arg0": orderID})
}
func (h *orderBookHarness) orderActive(state avm.Storage, orderID uint64) uint64 {
	return h.queryU64(state, "orderActive", []compiler.TypeRef{{Name: "uint64"}}, map[string]any{"arg0": orderID})
}

func (h *orderBookHarness) placeOrder(state avm.Storage, sender []byte, isBuy bool, tier uint64, amount int64) avm.Storage {
	h.t.Helper()
	next, _ := h.send(state, sender, "PlaceOrder", map[string]any{"isBuy": isBuy, "tier": tier, "amount": big.NewInt(amount)})
	return next
}

func (h *orderBookHarness) cancelOrder(state avm.Storage, sender []byte, orderID uint64) avm.Storage {
	h.t.Helper()
	next, _ := h.send(state, sender, "CancelOrder", map[string]any{"orderId": orderID})
	return next
}

func (h *orderBookHarness) matchOrder(state avm.Storage, sender []byte) (avm.Storage, avm.Execution) {
	h.t.Helper()
	return h.send(state, sender, "MatchOrder", map[string]any{})
}

// --- MAX_PRICE_TIERS / SLOTS_PER_LEVEL, mirrored from order_book.atlx.
// These are the constants under test: the whole point of the adversarial
// test below is proving the contract's OWN declared bounds actually hold,
// so they are pinned here rather than re-derived from the compiled module. ---
const obMaxPriceTiers = 32
const obSlotsPerLevel = 4

// TestOrderBookBestPriceTracking places several orders at different price
// tiers on both sides and checks that bestBid (highest bid tier) and bestAsk
// (lowest ask tier) track correctly as better and worse orders arrive, on a
// freshly deployed (empty, unseeded avm.Storage{}) contract.
func TestOrderBookBestPriceTracking(t *testing.T) {
	h := newOrderBookHarness(t)
	alice := testAddress(0xA1)
	bob := testAddress(0xB2)
	carol := testAddress(0xC3)
	dave := testAddress(0xD4)
	erin := testAddress(0xE5)
	frank := testAddress(0xF6)

	state := avm.Storage{}
	require.Equal(t, uint64(obMaxPriceTiers), h.bestBid(state), "empty book -> NO_TIER sentinel")
	require.Equal(t, uint64(obMaxPriceTiers), h.bestAsk(state), "empty book -> NO_TIER sentinel")

	state = h.placeOrder(state, alice, true, 10, 100)
	require.Equal(t, uint64(10), h.bestBid(state))

	state = h.placeOrder(state, bob, true, 15, 50)
	require.Equal(t, uint64(15), h.bestBid(state), "higher bid tier must become the new best")

	state = h.placeOrder(state, carol, true, 12, 30)
	require.Equal(t, uint64(15), h.bestBid(state), "worse bid tier must not disturb the cached best")

	state = h.placeOrder(state, dave, false, 20, 40)
	require.Equal(t, uint64(20), h.bestAsk(state))

	state = h.placeOrder(state, erin, false, 18, 60)
	require.Equal(t, uint64(18), h.bestAsk(state), "lower ask tier must become the new best")

	state = h.placeOrder(state, frank, false, 25, 10)
	require.Equal(t, uint64(18), h.bestAsk(state), "worse ask tier must not disturb the cached best")

	require.Equal(t, uint64(1), h.levelDepth(state, true, 10))
	require.Equal(t, uint64(1), h.levelDepth(state, true, 12))
	require.Equal(t, uint64(1), h.levelDepth(state, true, 15))
	require.Equal(t, uint64(1), h.levelDepth(state, false, 18))
	require.Equal(t, uint64(1), h.levelDepth(state, false, 20))
	require.Equal(t, uint64(1), h.levelDepth(state, false, 25))
	require.Equal(t, uint64(0), h.levelDepth(state, true, 11), "untouched tier must report zero depth")
}

// TestOrderBookMatchConsumesLiquidity builds a book with no initial cross,
// then places one crossing buy order and drives MatchOrder, checking that:
//   - a fully-drained resting order is deleted (orderAmount -> 0, orderActive
//     -> 0) and its level's best-price cache correctly rescans;
//   - a partially-filled resting order keeps its reduced remaining amount
//     and stays active;
//   - MatchOrder performs MULTIPLE crossing trades in a single call (bounded
//     by MAX_MATCH_STEPS), not just one, as long as the book stays crossed;
//   - untouched price levels on both sides are completely unaffected.
func TestOrderBookMatchConsumesLiquidity(t *testing.T) {
	h := newOrderBookHarness(t)
	alice := testAddress(0xA1) // bid tier10 amt100 (untouched)
	carol := testAddress(0xC3) // bid tier12 amt30  (untouched)
	bob := testAddress(0xB2)   // bid tier15 amt50   (untouched)
	dave := testAddress(0xD4)  // ask tier20 amt40   (partially filled)
	erin := testAddress(0xE5)  // ask tier18 amt60   (fully drained)
	frank := testAddress(0xF6) // ask tier25 amt10   (untouched)
	gwen := testAddress(0x67)  // crossing bid tier20 amt70

	state := avm.Storage{}
	state = h.placeOrder(state, alice, true, 10, 100)
	state = h.placeOrder(state, bob, true, 15, 50)
	state = h.placeOrder(state, carol, true, 12, 30)
	state = h.placeOrder(state, dave, false, 20, 40)
	state = h.placeOrder(state, erin, false, 18, 60)
	state = h.placeOrder(state, frank, false, 25, 10)
	require.Equal(t, uint64(15), h.bestBid(state))
	require.Equal(t, uint64(18), h.bestAsk(state), "book is not crossed yet (15 < 18)")

	orderIDAlice, orderIDBob, orderIDCarol := uint64(1), uint64(2), uint64(3)
	orderIDDave, orderIDErin, orderIDFrank := uint64(4), uint64(5), uint64(6)

	// Cross the book: a tier-20 bid for 70 now sits above the tier-18 ask.
	state = h.placeOrder(state, gwen, true, 20, 70)
	orderIDGwen := uint64(7)
	require.Equal(t, uint64(20), h.bestBid(state))
	require.Equal(t, uint64(18), h.bestAsk(state), "still crossed: bestBid 20 >= bestAsk 18")

	state, _ = h.matchOrder(state, alice)

	// Trade 1 (within the same MatchOrder call): gwen's bid(20,70) vs
	// erin's ask(18,60) -> tradeQty=60. erin's ask fully drains (deleted,
	// tier18 level removed, bestAsk rescans upward and lands on tier20 since
	// dave's ask is there). gwen's bid partially fills down to 10 and stays
	// resting at tier20 as the crossed book's new head.
	//
	// Trade 2 (bestBid 20 >= bestAsk 20, so MatchOrder keeps going): gwen's
	// remaining bid(20,10) vs dave's ask(20,40) -> tradeQty=10. gwen's bid
	// fully drains (deleted, tier20 BID level removed, bestBid rescans
	// downward and lands on tier15 -- alice's tier10 and carol's tier12 are
	// both worse). dave's ask partially fills down to 30 and stays resting.
	//
	// bestBid(15) < bestAsk(20) after trade 2 -> loop breaks, no trade 3.
	require.Equal(t, big.NewInt(0), h.orderAmount(state, orderIDErin), "erin's ask must be fully drained")
	require.Equal(t, uint64(0), h.orderActive(state, orderIDErin))
	require.Equal(t, big.NewInt(0), h.orderAmount(state, orderIDGwen), "gwen's crossing bid must be fully drained")
	require.Equal(t, uint64(0), h.orderActive(state, orderIDGwen))
	require.Equal(t, big.NewInt(30), h.orderAmount(state, orderIDDave), "dave's ask must be PARTIALLY filled: 40-10=30 remaining")
	require.Equal(t, uint64(1), h.orderActive(state, orderIDDave), "a partially filled order stays active")

	require.Equal(t, uint64(15), h.bestBid(state))
	require.Equal(t, uint64(20), h.bestAsk(state))

	// Untouched levels/orders must be completely unaffected by the match.
	require.Equal(t, big.NewInt(100), h.orderAmount(state, orderIDAlice))
	require.Equal(t, big.NewInt(50), h.orderAmount(state, orderIDBob))
	require.Equal(t, big.NewInt(30), h.orderAmount(state, orderIDCarol))
	require.Equal(t, big.NewInt(10), h.orderAmount(state, orderIDFrank))
	require.Equal(t, uint64(1), h.levelDepth(state, true, 10))
	require.Equal(t, uint64(1), h.levelDepth(state, true, 12))
	require.Equal(t, uint64(1), h.levelDepth(state, true, 15))
	require.Equal(t, uint64(1), h.levelDepth(state, false, 25))
	require.Equal(t, uint64(0), h.levelDepth(state, false, 18), "drained level must report zero depth")
	require.Equal(t, uint64(0), h.levelDepth(state, true, 20), "drained level must report zero depth")

	// A further MatchOrder call on a now-uncrossed book must be a no-op.
	stateBefore := state
	state, matchExec := h.matchOrder(state, alice)
	require.Equal(t, stateBefore, state, "MatchOrder on an uncrossed book must not mutate storage")
	require.Greater(t, matchExec.GasUsed, uint64(0), "even a no-op crank call is genuinely gas-charged, not free")
}

// TestOrderBookCancelOrder exercises CancelOrder in three shapes:
//  1. cancelling a lone order at a non-best tier (must not disturb the
//     cached best or any other tier);
//  2. cancelling the CURRENT best tier's only order (must trigger the
//     bounded rescan and correctly find the next-best tier);
//  3. cancelling from the HEAD and then the MIDDLE of a multi-order FIFO at
//     one price level (must shift-compact the remaining slots correctly,
//     and a subsequently placed order must land in the freed slot without
//     corrupting the survivors) -- this is the direct proof that
//     cancellation "removes an order without corrupting other price
//     levels" AND without corrupting sibling orders at the SAME level.
func TestOrderBookCancelOrder(t *testing.T) {
	h := newOrderBookHarness(t)
	alice := testAddress(0xA1)
	bob := testAddress(0xB2)
	carol := testAddress(0xC3)
	dave := testAddress(0xD4)
	stranger := testAddress(0x99)

	state := avm.Storage{}
	state = h.placeOrder(state, alice, true, 10, 100) // orderId 1, non-best
	state = h.placeOrder(state, bob, true, 15, 50)    // orderId 2, best
	state = h.placeOrder(state, carol, false, 25, 10) // orderId 3, ask side (unrelated side)
	orderIDAlice, orderIDBob, orderIDCarol := uint64(1), uint64(2), uint64(3)
	require.Equal(t, uint64(15), h.bestBid(state))

	// (1) Cancelling a non-best-tier order leaves the cached best untouched
	// and does not corrupt any other level.
	state = h.cancelOrder(state, alice, orderIDAlice)
	require.Equal(t, uint64(0), h.orderActive(state, orderIDAlice))
	require.Equal(t, uint64(0), h.levelDepth(state, true, 10))
	require.Equal(t, uint64(15), h.bestBid(state), "cancelling a worse tier must not change the cached best")
	require.Equal(t, uint64(1), h.levelDepth(state, true, 15), "bob's level must be untouched")
	require.Equal(t, big.NewInt(50), h.orderAmount(state, orderIDBob))
	require.Equal(t, uint64(1), h.levelDepth(state, false, 25), "the ask side must be completely untouched")
	require.Equal(t, big.NewInt(10), h.orderAmount(state, orderIDCarol))

	// Only the order's own trader may cancel it.
	h.sendExpectTrap(state, stranger, "CancelOrder", map[string]any{"orderId": orderIDBob})
	// A non-existent orderId must trap too.
	h.sendExpectTrap(state, alice, "CancelOrder", map[string]any{"orderId": uint64(999)})

	// (2) Cancelling the current best (and only) bid tier must trigger the
	// bounded rescan. With tier10 already gone, the book has no other bids
	// left, so bestBid must fall back to the empty-book sentinel.
	state = h.cancelOrder(state, bob, orderIDBob)
	require.Equal(t, uint64(0), h.orderActive(state, orderIDBob))
	require.Equal(t, uint64(obMaxPriceTiers), h.bestBid(state), "no bids left -> NO_TIER sentinel")
	require.Equal(t, uint64(1), h.levelDepth(state, false, 25), "cancelling the whole bid side must not touch the ask side")

	// (3) Multi-order FIFO at one level: place 3 orders at tier5, cancel the
	// HEAD, verify the survivors shifted down correctly, place a 4th order
	// (must land in the freed slot), then cancel the new MIDDLE order.
	state = h.placeOrder(state, dave, true, 5, 11) // orderId 4, slot0
	orderIDH := uint64(4)
	state = h.placeOrder(state, bob, true, 5, 22) // orderId 5, slot1
	orderIDI := uint64(5)
	state = h.placeOrder(state, carol, true, 5, 33) // orderId 6 (carol reused as a distinct trader), slot2
	orderIDJ := uint64(6)
	require.Equal(t, uint64(3), h.levelDepth(state, true, 5))

	state = h.cancelOrder(state, dave, orderIDH) // cancel the HEAD (slot0)
	require.Equal(t, uint64(0), h.orderActive(state, orderIDH))
	require.Equal(t, uint64(2), h.levelDepth(state, true, 5))
	require.Equal(t, big.NewInt(22), h.orderAmount(state, orderIDI), "survivor must shift down to slot0 with its value intact")
	require.Equal(t, big.NewInt(33), h.orderAmount(state, orderIDJ), "survivor must shift down to slot1 with its value intact")

	state = h.placeOrder(state, alice, true, 5, 44) // orderId 7, must land in the freed slot2
	orderIDK := uint64(7)
	require.Equal(t, uint64(3), h.levelDepth(state, true, 5))
	require.Equal(t, big.NewInt(44), h.orderAmount(state, orderIDK))
	require.Equal(t, big.NewInt(22), h.orderAmount(state, orderIDI), "inserting into the freed slot must not corrupt the existing survivor")
	require.Equal(t, big.NewInt(33), h.orderAmount(state, orderIDJ), "inserting into the freed slot must not corrupt the existing survivor")

	// Cancel the now-MIDDLE order (orderIDJ, slot1) and check the tail
	// (orderIDK, slot2) shifts down while the head (orderIDI, slot0) is
	// untouched.
	state = h.cancelOrder(state, carol, orderIDJ)
	require.Equal(t, uint64(0), h.orderActive(state, orderIDJ))
	require.Equal(t, uint64(2), h.levelDepth(state, true, 5))
	require.Equal(t, big.NewInt(22), h.orderAmount(state, orderIDI), "head must be untouched by a middle cancel")
	require.Equal(t, big.NewInt(44), h.orderAmount(state, orderIDK), "tail must shift down to fill the middle gap")
}

// TestOrderBookLevelCapacityIsBounded proves SLOTS_PER_LEVEL is a real,
// enforced hard cap (PlaceOrder past it traps), not just documentation.
func TestOrderBookLevelCapacityIsBounded(t *testing.T) {
	h := newOrderBookHarness(t)
	trader := testAddress(0x42)

	state := avm.Storage{}
	for i := 0; i < obSlotsPerLevel; i++ {
		state = h.placeOrder(state, trader, true, 7, int64(i+1))
	}
	require.Equal(t, uint64(obSlotsPerLevel), h.levelDepth(state, true, 7))

	// One more at the same tier must trap (ERR_LEVEL_FULL), leaving state
	// untouched -- the level is genuinely capacity-bounded, not just slow.
	h.sendExpectTrap(state, trader, "PlaceOrder", map[string]any{"isBuy": true, "tier": uint64(7), "amount": big.NewInt(999)})
}

// TestOrderBookAdversarialManyPriceLevels is the bounded-depth adversarial
// case: it (a) fills the book to its full declared MAX_PRICE_TIERS capacity
// on the bid side and checks best-price tracking still holds at max
// density, then (b) constructs the WORST-CASE single-call rescan -- only
// the lowest and highest tiers populated, so cancelling the highest forces
// the downward rescan to walk the ENTIRE MAX_PRICE_TIERS-1 gap before
// finding the next best -- and asserts the executed-instruction count for
// that one CancelOrder call is bounded by a small constant multiple of
// MAX_PRICE_TIERS, NOT by the total number of orders/levels ever placed in
// the contract's history (which is far larger by the time this call runs).
// This is the concrete, measured proof behind the header comment's
// "O(MAX_PRICE_TIERS), not O(log n)" claim: bounded and linear, not
// unbounded.
func TestOrderBookAdversarialManyPriceLevels(t *testing.T) {
	h := newOrderBookHarness(t)
	trader := testAddress(0x55)

	// (a) Fill every single tier in [0, MAX_PRICE_TIERS) on the bid side and
	// check best-price tracking holds at full declared capacity.
	fullState := avm.Storage{}
	for tier := uint64(0); tier < obMaxPriceTiers; tier++ {
		fullState = h.placeOrder(fullState, trader, true, tier, 1)
		require.Equal(t, tier, h.bestBid(fullState), "best bid must track the highest tier placed so far")
	}
	require.Equal(t, uint64(obMaxPriceTiers-1), h.bestBid(fullState))
	for tier := uint64(0); tier < obMaxPriceTiers; tier++ {
		require.Equal(t, uint64(1), h.levelDepth(fullState, true, tier), "every tier in the full domain must hold exactly one order")
	}

	// (b) Isolate the rescan's cost from total book/state size: build TWO
	// states holding the exact same NUMBER of orders (so Storage.toChunk/
	// fromChunk serialization cost -- which this VM's gas model charges per
	// message regardless of loop structure -- is held constant), differing
	// ONLY in how far apart the two populated tiers are. Cancelling the
	// higher tier in each then measures the rescan loop's OWN cost in
	// isolation:
	//   - "near": tier(MAX-1) and tier(MAX-2) populated -- the rescan's
	//     first candidate below the cancelled tier is immediately populated.
	//   - "far":  tier(MAX-1) and tier0 populated -- the rescan must walk
	//     the entire MAX_PRICE_TIERS-1 gap before finding the next best.
	nearState := avm.Storage{}
	nearState = h.placeOrder(nearState, trader, true, obMaxPriceTiers-2, 7)
	nearSurvivorID := uint64(1)
	nearState = h.placeOrder(nearState, trader, true, obMaxPriceTiers-1, 9)
	nearCancelID := uint64(2)
	require.Equal(t, uint64(obMaxPriceTiers-1), h.bestBid(nearState))
	nearState, nearExec := h.send(nearState, trader, "CancelOrder", map[string]any{"orderId": nearCancelID})
	require.Equal(t, uint64(obMaxPriceTiers-2), h.bestBid(nearState), "near case: adjacent tier found on essentially the first candidate")
	require.Equal(t, uint64(1), h.orderActive(nearState, nearSurvivorID))

	farState := avm.Storage{}
	farState = h.placeOrder(farState, trader, true, 0, 7)
	farSurvivorID := uint64(1)
	farState = h.placeOrder(farState, trader, true, obMaxPriceTiers-1, 9)
	farCancelID := uint64(2)
	require.Equal(t, uint64(obMaxPriceTiers-1), h.bestBid(farState))
	farState, farExec := h.send(farState, trader, "CancelOrder", map[string]any{"orderId": farCancelID})
	require.Equal(t, uint64(0), h.bestBid(farState), "far case: worst-case rescan must still correctly find tier0 across the full gap")
	require.Equal(t, uint64(1), h.orderActive(farState, farSurvivorID), "the untouched low order must survive")

	t.Logf("near-cancel (2 orders, 1-tier gap) executed %d opcodes (gas %d); far-cancel (2 orders, %d-tier gap) executed %d opcodes (gas %d)",
		len(nearExec.ExecutedOpcode), nearExec.GasUsed, obMaxPriceTiers-1, len(farExec.ExecutedOpcode), farExec.GasUsed)

	// Both states hold exactly 2 orders (before the cancel) / 1 order
	// (after), so serialization cost is identical -- any remaining GAS
	// difference is attributable ONLY to the rescan loop itself, and it
	// must be strictly higher for the far case AND bounded by a small
	// constant multiple of MAX_PRICE_TIERS, not by total book size.
	require.Greaterf(t, farExec.GasUsed, nearExec.GasUsed,
		"with book/state size held equal, the far-gap rescan must cost strictly more gas than the near-gap rescan")
	require.Greaterf(t, len(farExec.ExecutedOpcode), len(nearExec.ExecutedOpcode),
		"with book/state size held equal, the far-gap rescan must execute strictly more opcodes than the near-gap rescan")

	const perTierScanOpcodeCeiling = 35 // generous per-candidate-tier instruction budget for the has()+compare+branch sequence (measured ~26/tier)
	maxExpectedExtraOpcodes := obMaxPriceTiers * perTierScanOpcodeCeiling
	extraOpcodes := len(farExec.ExecutedOpcode) - len(nearExec.ExecutedOpcode)
	require.LessOrEqualf(t, extraOpcodes, maxExpectedExtraOpcodes,
		"far-case rescan executed %d more opcodes than the near case, expected at most %d (MAX_PRICE_TIERS=%d x %d/tier) -- the rescan must be bounded by the tier domain, not by total book size",
		extraOpcodes, maxExpectedExtraOpcodes, obMaxPriceTiers, perTierScanOpcodeCeiling)

	// Sanity check on fullState from (a): cancelling its top tier (adjacent
	// to the next tier down, by construction) must still succeed cleanly
	// against a book with 32 pre-existing levels -- the rescan logic itself
	// does not depend on book size, only this test's cost ISOLATION above
	// does (serialization cost genuinely does scale with entry count, which
	// is a property of Storage.toChunk/fromChunk, not of the rescan loop).
	fullState, _ = h.send(fullState, trader, "CancelOrder", map[string]any{"orderId": uint64(obMaxPriceTiers)})
	require.Equal(t, uint64(obMaxPriceTiers-2), h.bestBid(fullState))
}

// TestOrderBookFreshContractStorageDefault documents the same
// avm.Storage{} fresh-deploy convention used throughout this suite (see
// storage_default_test.go) works for order_book.atlx's Map-typed fields
// without any explicit seeding, and that the very first order ever placed
// (tier 0, the same value every uint64 field zero-initializes to) is
// tracked correctly -- the genesis case the has()-based self-healing cache
// design in the header comment specifically calls out.
func TestOrderBookFreshContractStorageDefault(t *testing.T) {
	h := newOrderBookHarness(t)
	trader := testAddress(0x01)

	state := avm.Storage{}
	state = h.placeOrder(state, trader, true, 0, 5)
	require.Equal(t, uint64(0), h.bestBid(state), "the very first order, at tier 0, must be correctly tracked as the best bid")
	require.Equal(t, uint64(1), h.levelDepth(state, true, 0))
}

// TestOrderBookGasScalesWithMapUsageDespiteBoundedSteps is the permanent
// regression proof behind this file's header "Gas cost caveat" section: it
// isolates the LOGICAL shape of the work (same kind of new-level PlaceOrder;
// same kind of lone-non-best-level CancelOrder with NO best-price rescan) so
// the executed OPCODE COUNT is identical between a nearly-empty book and a
// nearly-full one -- proving the loop-step bounds documented at the top of
// order_book.atlx really do hold -- while showing GAS still differs
// substantially, because AVM v1 bills every Map get/set/has/delete against
// that map's CURRENT TOTAL size (bidSlots/askSlots/orderLocation each cover
// ALL 32 tiers combined in one field), not against the touched key. This is
// FINDING-001 (security-audit/05-findings/FINDING-001-avm-gas-mispricing-dos.md),
// a DIFFERENT mechanism from the whole-struct-snapshot `.save()` issue fixed
// in x/aetravm/compiler/compile.go (see its "case save:" comment) -- fixing
// `.save()` does NOT fix this, and this test proves the gap survives that
// fix. Unlike TestPaginationInsertGasBoundedAcrossFreshPages in
// pagination_acceptance_test.go (which shards into small per-page Map
// fields specifically to avoid this), order_book.atlx uses a small number of
// large, unsharded Map fields by design (see the header comment's point 2),
// so this gap is EXPECTED here, not a bug to fix -- this test documents and
// bounds it, it does not assert it away.
func TestOrderBookGasScalesWithMapUsageDespiteBoundedSteps(t *testing.T) {
	h := newOrderBookHarness(t)
	trader := testAddress(0x91)

	// --- PlaceOrder: creating a brand-new level at an untouched tier. ---
	lowPlaceBase := avm.Storage{}
	lowPlaceBase = h.placeOrder(lowPlaceBase, trader, true, 10, 1)
	lowPlaceBase = h.placeOrder(lowPlaceBase, trader, true, 20, 1)
	_, lowPlaceExec := h.send(lowPlaceBase, trader, "PlaceOrder", map[string]any{"isBuy": true, "tier": uint64(0), "amount": big.NewInt(1)})

	highPlaceBase := avm.Storage{}
	for tier := uint64(1); tier < obMaxPriceTiers; tier++ {
		for slot := 0; slot < obSlotsPerLevel; slot++ {
			highPlaceBase = h.placeOrder(highPlaceBase, trader, true, tier, 1)
		}
	}
	_, highPlaceExec := h.send(highPlaceBase, trader, "PlaceOrder", map[string]any{"isBuy": true, "tier": uint64(0), "amount": big.NewInt(1)})

	require.Equal(t, len(lowPlaceExec.ExecutedOpcode), len(highPlaceExec.ExecutedOpcode),
		"PlaceOrder's own executed opcode count must be identical regardless of book size (the bounded-step-count claim) -- if this fails, the header comment's O(1) claim is the thing that's wrong, not this test")
	require.Greater(t, highPlaceExec.GasUsed, lowPlaceExec.GasUsed*5,
		"despite IDENTICAL opcode counts, PlaceOrder against a near-full book must cost substantially more gas than against a near-empty one -- this is FINDING-001's Map-total-size billing, not a step-count regression")

	// --- CancelOrder: cancelling a lone order at a non-best tier, so the
	// bounded best-price rescan never triggers. ---
	lowCancelBase := avm.Storage{}
	lowCancelBase = h.placeOrder(lowCancelBase, trader, true, 20, 1) // orderId1, best
	lowCancelBase = h.placeOrder(lowCancelBase, trader, true, 5, 1)  // orderId2, lone non-best
	_, lowCancelExec := h.send(lowCancelBase, trader, "CancelOrder", map[string]any{"orderId": uint64(2)})

	highCancelBase := avm.Storage{}
	for tier := uint64(1); tier < obMaxPriceTiers; tier++ {
		for slot := 0; slot < obSlotsPerLevel; slot++ {
			highCancelBase = h.placeOrder(highCancelBase, trader, true, tier, 1)
		}
	}
	highCancelBase = h.placeOrder(highCancelBase, trader, true, 0, 1) // lone non-best order, last placed
	lastOrderID := uint64((obMaxPriceTiers-1)*obSlotsPerLevel + 1)
	_, highCancelExec := h.send(highCancelBase, trader, "CancelOrder", map[string]any{"orderId": lastOrderID})

	require.Equal(t, len(lowCancelExec.ExecutedOpcode), len(highCancelExec.ExecutedOpcode),
		"CancelOrder's own executed opcode count must be identical regardless of book size (the bounded-step-count claim)")
	require.Greater(t, highCancelExec.GasUsed, lowCancelExec.GasUsed*10,
		"despite IDENTICAL opcode counts, CancelOrder against a near-full book must cost substantially more gas than against a near-empty one -- this is FINDING-001's Map-total-size billing, not a step-count regression")

	t.Logf("PlaceOrder gas: low=%d high=%d (%dx); CancelOrder gas: low=%d high=%d (%dx) -- opcode counts held equal in both pairs",
		lowPlaceExec.GasUsed, highPlaceExec.GasUsed, highPlaceExec.GasUsed/lowPlaceExec.GasUsed,
		lowCancelExec.GasUsed, highCancelExec.GasUsed, highCancelExec.GasUsed/lowCancelExec.GasUsed)
}
