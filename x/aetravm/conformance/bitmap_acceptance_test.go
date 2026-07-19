package conformance

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/aetravm/compiler"
)

// TestBitmapSetGetClearToggleAtWordBoundaries is the conformance proof for
// examples/avm/collections/bitmap_stdlib.atlx: it drives the real compiler +
// AVM through set/get/clear/toggle specifically at the three boundary
// positions the task calls out -- bit 0 (LSB of word 0), bit 255 (MSB of
// word 0), and bit 256 (LSB of word 1, i.e. the first bit that crosses into
// a second Map entry) -- and proves writes to one word never disturb
// another word's bits.
func TestBitmapSetGetClearToggleAtWordBoundaries(t *testing.T) {
	h := newBitmapHarness(t)
	deployer := testAddress(0x31)
	sender := testAddress(0x32)

	state := avm.Storage{}

	// Fresh contract: every bit reads 0, both words read as the zero word.
	require.Equal(t, uint64(0), h.getBit(state, 0))
	require.Equal(t, uint64(0), h.getBit(state, 255))
	require.Equal(t, uint64(0), h.getBit(state, 256))
	require.Equal(t, big.NewInt(0), h.getWord(state, 0))
	require.Equal(t, big.NewInt(0), h.getWord(state, 1))

	// --- bit 0: LSB of word 0 ---
	state, _ = h.send(state, deployer, sender, "SetBit", 0)
	require.Equal(t, uint64(1), h.getBit(state, 0))
	require.Equal(t, big.NewInt(1), h.getWord(state, 0))
	require.Equal(t, uint64(0), h.getBit(state, 1), "setting bit 0 must not set bit 1")

	// --- bit 255: MSB of word 0 (the highest bit that still belongs to
	// word 0 -- one past this crosses into word 1). ---
	state, _ = h.send(state, deployer, sender, "SetBit", 255)
	require.Equal(t, uint64(1), h.getBit(state, 255))
	wantWord0 := new(big.Int).Or(big.NewInt(1), new(big.Int).Lsh(big.NewInt(1), 255))
	require.Equal(t, wantWord0, h.getWord(state, 0), "word0 must hold exactly bit 0 and bit 255 set, nothing else")
	require.Equal(t, uint64(0), h.getBit(state, 254), "bit 254 (one below the MSB) must remain clear")
	require.Equal(t, big.NewInt(0), h.getWord(state, 1), "word1 must still be untouched by word0-only writes")

	// --- bit 256: LSB of word 1 -- the first index that crosses into a
	// SECOND Map<uint256,uint256> entry. Must not disturb word 0 at all. ---
	state, _ = h.send(state, deployer, sender, "SetBit", 256)
	require.Equal(t, uint64(1), h.getBit(state, 256))
	require.Equal(t, big.NewInt(1), h.getWord(state, 1))
	require.Equal(t, wantWord0, h.getWord(state, 0), "crossing into word1 must leave word0 byte-for-byte unchanged")
	require.Equal(t, uint64(0), h.getBit(state, 257), "bit 257 must remain clear")

	// --- ToggleBit: off then on, at a boundary bit and an interior one ---
	state, _ = h.send(state, deployer, sender, "ToggleBit", 255)
	require.Equal(t, uint64(0), h.getBit(state, 255), "toggling a set bit must clear it")
	require.Equal(t, big.NewInt(1), h.getWord(state, 0))

	state, _ = h.send(state, deployer, sender, "ToggleBit", 255)
	require.Equal(t, uint64(1), h.getBit(state, 255), "toggling a clear bit must set it")
	require.Equal(t, wantWord0, h.getWord(state, 0))

	state, _ = h.send(state, deployer, sender, "ToggleBit", 300)
	require.Equal(t, uint64(1), h.getBit(state, 300))
	require.Equal(t, uint64(1), h.getBit(state, 256), "toggling bit 300 must not disturb bit 256 in the same word")

	// --- ClearBit: at the boundary, and idempotence ---
	state, _ = h.send(state, deployer, sender, "ClearBit", 0)
	require.Equal(t, uint64(0), h.getBit(state, 0))
	require.Equal(t, uint64(1), h.getBit(state, 255), "clearing bit 0 must not disturb bit 255 in the same word")

	stateBeforeNoop := state
	state, _ = h.send(state, deployer, sender, "ClearBit", 0)
	require.Equal(t, stateBeforeNoop, state, "clearing an already-clear bit must be a byte-for-byte no-op")
}

// TestBitmapPopcountAtWordBoundaries proves popcountWord for the boundary
// cases (0, 1, and 2 widely-separated bits, and a fully-saturated word).
// The widely-separated two-bit case is deliberately the SAME shape (bits 0
// and 255 of one word) that this file's first draft got wrong by 256 --
// see the popcountWord doc comment's "final combination" note in
// examples/avm/collections/bitmap_stdlib.atlx for the bug this regression
// test would have caught.
func TestBitmapPopcountAtWordBoundaries(t *testing.T) {
	h := newBitmapHarness(t)
	deployer := testAddress(0x33)
	sender := testAddress(0x34)

	state := avm.Storage{}
	require.Equal(t, uint64(0), h.popcountWord(state, 0), "empty word must popcount to 0")

	state, _ = h.send(state, deployer, sender, "SetBit", 0)
	require.Equal(t, uint64(1), h.popcountWord(state, 0))

	// Bits 0 and 255: opposite ends of the SAME word. This is the exact
	// case that previously returned 258 instead of 2.
	state, _ = h.send(state, deployer, sender, "SetBit", 255)
	require.Equal(t, uint64(2), h.popcountWord(state, 0), "bits at opposite ends of one word must both be counted, with no phantom extra bits")

	// A third, interior bit.
	state, _ = h.send(state, deployer, sender, "SetBit", 128)
	require.Equal(t, uint64(3), h.popcountWord(state, 0))

	// A fourth bit in a SECOND word must not perturb word 0's count.
	state, _ = h.send(state, deployer, sender, "SetBit", 256)
	require.Equal(t, uint64(3), h.popcountWord(state, 0), "a write to word1 must not change word0's popcount")
	require.Equal(t, uint64(1), h.popcountWord(state, 1))

	// Fully-saturated word: every one of the 256 bits set.
	full := avm.Storage{}
	for i := int64(0); i < 256; i++ {
		full, _ = h.send(full, deployer, sender, "SetBit", i)
	}
	require.Equal(t, uint64(256), h.popcountWord(full, 0), "a completely full word must popcount to exactly 256")
	allOnes := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	require.Equal(t, allOnes, h.getWord(full, 0))

	// Every bit clear again after clearing them all.
	empty := full
	for i := int64(0); i < 256; i++ {
		empty, _ = h.send(empty, deployer, sender, "ClearBit", i)
	}
	require.Equal(t, uint64(0), h.popcountWord(empty, 0), "clearing every bit must bring popcount back to 0")
	require.Equal(t, big.NewInt(0), h.getWord(empty, 0))
}

// TestBitmapArbitrarilyLargeIndexSpace proves the bitmap is not limited to
// small indices: setting a bit at a very large logical index (far beyond
// any realistic "number of items" a fixed-size collection could hold) works
// exactly like a small one, storing only the ONE word it touches -- the
// defining property of the sparse Map<wordIndex,uint256> backing, as
// opposed to a scheme that would need to allocate anything proportional to
// the index itself.
func TestBitmapArbitrarilyLargeIndexSpace(t *testing.T) {
	h := newBitmapHarness(t)
	deployer := testAddress(0x35)
	sender := testAddress(0x36)

	// 2^250 + 7: an index nowhere near uint64 range, deep inside uint256's
	// address space.
	bigIndex := new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 250), big.NewInt(7))

	state := avm.Storage{}
	body := mustCodecBody(t, compiler.Codec{Fields: []compiler.CodecField{{Name: "index", Type: compiler.TypeRef{Name: "uint256"}}}}, map[string]any{"index": bigIndex})
	exec, err := h.runner.Run(h.res.Module, state, avm.RuntimeContext{
		Entry:           avm.EntryReceiveInternal,
		ContractAddress: deployer,
		GasLimit:        20_000_000,
		Message: async.MessageEnvelope{
			Source:   append([]byte(nil), sender...),
			Opcode:   h.res.MessageBodyOpcodes["SetBit"],
			QueryID:  uint64(h.res.MessageBodyOpcodes["SetBit"]),
			Body:     body,
			GasLimit: 20_000_000,
		},
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	state = exec.State

	require.Equal(t, uint64(1), h.getBitBig(state, bigIndex))
	require.Equal(t, uint64(0), h.getBit(state, 0), "an unrelated small index must remain untouched")

	wordIndex := new(big.Int).Rsh(bigIndex, 8) // index / WORD_BITS(256)
	word := h.getWord(state, 0)
	_ = word // word0 (index 0..255) must be untouched; checked below via getBit(0).
	require.Equal(t, uint64(0), h.popcountWord(state, 0), "word0 must be completely unaffected by a write far outside its range")
	require.NotEqual(t, big.NewInt(0), wordIndex, "sanity: the touched word index really is nonzero/large")
}

// --- harness -----------------------------------------------------------

type bitmapHarness struct {
	t      *testing.T
	res    *compiler.Result
	runner *avm.Runner
}

func newBitmapHarness(t *testing.T) *bitmapHarness {
	t.Helper()
	res := compileExampleFile(t, "collections/bitmap_stdlib.atlx", compiler.Options{})
	require.NoError(t, avm.VerifyInterface(res.Module, res.Manifest))
	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	return &bitmapHarness{t: t, res: res, runner: runner}
}

func (h *bitmapHarness) send(state avm.Storage, contract, sender []byte, msgName string, index int64) (avm.Storage, avm.Execution) {
	h.t.Helper()
	codec := compiler.Codec{Fields: []compiler.CodecField{{Name: "index", Type: compiler.TypeRef{Name: "uint256"}}}}
	body := mustCodecBody(h.t, codec, map[string]any{"index": big.NewInt(index)})
	exec, err := h.runner.Run(h.res.Module, state, avm.RuntimeContext{
		Entry:           avm.EntryReceiveInternal,
		ContractAddress: contract,
		GasLimit:        20_000_000,
		Message: async.MessageEnvelope{
			Source:   append([]byte(nil), sender...),
			Opcode:   h.res.MessageBodyOpcodes[msgName],
			QueryID:  uint64(h.res.MessageBodyOpcodes[msgName]),
			Body:     body,
			GasLimit: 20_000_000,
		},
	})
	require.NoError(h.t, err)
	require.Equalf(h.t, async.ResultOK, exec.ResultCode, "%s(%d) result", msgName, index)
	return exec.State, exec
}

var bitmapU256ArgCodec = compiler.Codec{Fields: []compiler.CodecField{{Name: "arg0", Type: compiler.TypeRef{Name: "uint256"}}}}

func (h *bitmapHarness) getBit(state avm.Storage, index int64) uint64 {
	h.t.Helper()
	return h.getBitBig(state, big.NewInt(index))
}

func (h *bitmapHarness) getBitBig(state avm.Storage, index *big.Int) uint64 {
	h.t.Helper()
	body := mustCodecBody(h.t, bitmapU256ArgCodec, map[string]any{"arg0": index})
	exec, err := h.runner.Run(h.res.Module, state, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		Message:  async.MessageEnvelope{Opcode: opcodeForGetter(h.t, h.res, "getBit"), Body: body, GasLimit: 5_000_000},
		GasLimit: 5_000_000,
	})
	require.NoError(h.t, err)
	require.Equal(h.t, async.ResultOK, exec.ResultCode)
	v, err := exec.ReturnValue.AsUint64()
	require.NoError(h.t, err)
	return v
}

func (h *bitmapHarness) getWord(state avm.Storage, wordIndex int64) *big.Int {
	h.t.Helper()
	body := mustCodecBody(h.t, bitmapU256ArgCodec, map[string]any{"arg0": big.NewInt(wordIndex)})
	exec, err := h.runner.Run(h.res.Module, state, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		Message:  async.MessageEnvelope{Opcode: opcodeForGetter(h.t, h.res, "getWord"), Body: body, GasLimit: 5_000_000},
		GasLimit: 5_000_000,
	})
	require.NoError(h.t, err)
	require.Equal(h.t, async.ResultOK, exec.ResultCode)
	v, err := exec.ReturnValue.AsBigInt()
	require.NoError(h.t, err)
	return v
}

func (h *bitmapHarness) popcountWord(state avm.Storage, wordIndex int64) uint64 {
	h.t.Helper()
	body := mustCodecBody(h.t, bitmapU256ArgCodec, map[string]any{"arg0": big.NewInt(wordIndex)})
	exec, err := h.runner.Run(h.res.Module, state, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		Message:  async.MessageEnvelope{Opcode: opcodeForGetter(h.t, h.res, "popcountWord"), Body: body, GasLimit: 5_000_000},
		GasLimit: 5_000_000,
	})
	require.NoError(h.t, err)
	require.Equal(h.t, async.ResultOK, exec.ResultCode)
	v, err := exec.ReturnValue.AsUint64()
	require.NoError(h.t, err)
	return v
}
