package avm

import (
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/aetravm/async"
)

// runByteCode runs a bare code sequence under EntryQuery with no imports. The
// sequence must leave its result on top of the stack; execution falls off the
// end of the code, which sets ResultOK and ReturnValue = top-of-stack (same as
// an explicit OpReturn, see the Runner.Run tail). Returns (exec, err) so trap
// tests can inspect the error and the rolled-back ResultCode.
func runByteCode(t *testing.T, code []Instruction) (Execution, error) {
	t.Helper()
	runner := newTestRunner(t)
	module := Module{
		Version: Version,
		Exports: map[Entrypoint]uint32{EntryQuery: 0},
		Code:    code,
	}
	return runner.Run(module, nil, runtimeCtx(EntryQuery))
}

func pushBytes(b []byte) Instruction { return Instruction{Op: OpPushBytes, Data: b} }
func pushU64(v uint64) Instruction   { return Instruction{Op: OpPushU64, Arg: v} }

// TestAVMByteExactHashKnownVectors pins each new byte-exact hash opcode to the
// canonical published test vector for the empty string and "abc". These are
// hard-coded (not recomputed with the same library the opcode uses) so they are
// true known-answer tests: a wrong algorithm, wrong padding, or a tag/length
// prefix leaking into the preimage would change the digest and fail here. The
// keccak256("") vector is the one called out in the task.
func TestAVMByteExactHashKnownVectors(t *testing.T) {
	cases := []struct {
		name    string
		op      Opcode
		input   string
		wantHex string
		isHash  bool // true => 32-byte TagHash result; false => TagBytes
	}{
		{"sha256/empty", OpSha256, "", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", true},
		{"sha256/abc", OpSha256, "abc", "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", true},
		{"keccak256/empty", OpKeccak256, "", "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470", true},
		{"keccak256/abc", OpKeccak256, "abc", "4e03657aea45a94fc7d47ba826c8d667c0d1e6e33a64a036ec44f58fa12d6c45", true},
		{"blake2b/empty", OpBlake2b, "", "0e5751c026e543b2e8ab2eb06099daa1d1e5df47778f7787faab45cdf12fe3a8", true},
		{"blake2b/abc", OpBlake2b, "abc", "bddd813c634239723171ef3fee98579b94964e3bb1cb3e427262c8c068d52319", true},
		{"ripemd160/empty", OpRipemd160, "", "9c1185a5c5e9fc54612808977ee8f548b2258d31", false},
		{"ripemd160/abc", OpRipemd160, "abc", "8eb208f7e05d987a9b044a8e98c6b087f15a0bfc", false},
		{"sha512/empty", OpSha512, "", "cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3e", false},
		{"sha512/abc", OpSha512, "abc", "ddaf35a193617abacc417349ae20413112e6fa4e89a97ea20a9eeee64b55d39a2192992a274fc1a836ba3c23a3feebbd454d4423643ce80e2a9ac94fa54ca49f", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exec, err := runByteCode(t, []Instruction{pushBytes([]byte(tc.input)), {Op: tc.op}})
			require.NoError(t, err)
			require.Equal(t, async.ResultOK, exec.ResultCode)
			var got string
			if tc.isHash {
				h, err := exec.ReturnValue.AsHash()
				require.NoError(t, err)
				got = hex.EncodeToString(h[:])
			} else {
				require.Equal(t, TagBytes, exec.ReturnValue.Tag, "non-32-byte digest must return as bytes")
				b, err := exec.ReturnValue.AsBytes()
				require.NoError(t, err)
				got = hex.EncodeToString(b)
			}
			require.Equal(t, tc.wantHex, got)
		})
	}
}

// TestAVMByteExactHashDiffersFromChunkHash proves the new sha256 opcode is a
// genuine byte-exact hash, NOT the OpHash BLAKE3 chunk-tree Merkle root over a
// tag+length-prefixed canonical encoding. A bridge could never line up OpHash
// against a foreign sha256; OpSha256 is exactly the primitive that unblocks it.
func TestAVMByteExactHashDiffersFromChunkHash(t *testing.T) {
	input := []byte("cross-chain header bytes")

	sha, err := runByteCode(t, []Instruction{pushBytes(input), {Op: OpSha256}})
	require.NoError(t, err)
	shaHash, err := sha.ReturnValue.AsHash()
	require.NoError(t, err)

	chunk, err := runByteCode(t, []Instruction{pushBytes(input), {Op: OpHash}})
	require.NoError(t, err)
	chunkHash, err := chunk.ReturnValue.AsHash()
	require.NoError(t, err)

	require.NotEqual(t, chunkHash, shaHash, "OpSha256 must hash raw bytes, not the chunk-tree root that OpHash returns")

	// And it must equal the standard sha256 of the raw bytes.
	want := sha256.Sum256(input)
	require.Equal(t, want, shaHash)
}

// TestAVMConcat covers concatenation, including empty operands and order.
func TestAVMConcat(t *testing.T) {
	exec, err := runByteCode(t, []Instruction{
		pushBytes([]byte{0x01, 0x02}),
		pushBytes([]byte{0x03, 0x04, 0x05}),
		{Op: OpConcat},
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	got, err := exec.ReturnValue.AsBytes()
	require.NoError(t, err)
	require.Equal(t, []byte{0x01, 0x02, 0x03, 0x04, 0x05}, got, "concat(a,b) must be a||b in order")

	// Empty left / right operands.
	exec, err = runByteCode(t, []Instruction{
		pushBytes(nil),
		pushBytes([]byte{0xaa}),
		{Op: OpConcat},
	})
	require.NoError(t, err)
	got, err = exec.ReturnValue.AsBytes()
	require.NoError(t, err)
	require.Equal(t, []byte{0xaa}, got)
}

// TestAVMSliceAndByteAt covers slice windows and single-byte reads.
func TestAVMSliceAndByteAt(t *testing.T) {
	data := []byte{0x10, 0x11, 0x12, 0x13, 0x14, 0x15}

	// slice(data, 2, 3) == data[2:5]
	exec, err := runByteCode(t, []Instruction{
		pushBytes(data),
		pushU64(2),
		pushU64(3),
		{Op: OpSlice},
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	got, err := exec.ReturnValue.AsBytes()
	require.NoError(t, err)
	require.Equal(t, []byte{0x12, 0x13, 0x14}, got)

	// slice(data, len, 0) at the exact end is an empty slice, not a trap.
	exec, err = runByteCode(t, []Instruction{
		pushBytes(data),
		pushU64(uint64(len(data))),
		pushU64(0),
		{Op: OpSlice},
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	got, err = exec.ReturnValue.AsBytes()
	require.NoError(t, err)
	require.Empty(t, got)

	// byteAt(data, 4) == data[4]
	exec, err = runByteCode(t, []Instruction{
		pushBytes(data),
		pushU64(4),
		{Op: OpByteAt},
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	b, err := exec.ReturnValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(0x14), b)
	require.Equal(t, TagUint8, exec.ReturnValue.Tag)
}

// TestAVMBytesRoundTrip proves toBytesBE / fromBytesBE are exact big-endian
// inverses across widths, and that toBytesBE zero-pads on the left.
func TestAVMBytesRoundTrip(t *testing.T) {
	values := []uint64{0, 1, 255, 256, 0xdeadbeef, 0x0102030405060708}
	widths := []uint64{8, 16, 32}
	for _, v := range values {
		for _, n := range widths {
			exec, err := runByteCode(t, []Instruction{
				pushU64(v),
				pushU64(n),
				{Op: OpToBytesBE},
				{Op: OpFromBytesBE},
			})
			require.NoError(t, err)
			require.Equalf(t, async.ResultOK, exec.ResultCode, "v=%d n=%d", v, n)
			got, err := exec.ReturnValue.AsBigInt()
			require.NoError(t, err)
			require.Equalf(t, 0, got.Cmp(new(big.Int).SetUint64(v)), "fromBytesBE(toBytesBE(%d,%d)) mismatch: got %s", v, n, got)
			require.Equal(t, TagUint256, exec.ReturnValue.Tag, "fromBytesBE must widen to uint256")
		}
	}

	// toBytesBE(0x0102, 4) is left-zero-padded big-endian.
	exec, err := runByteCode(t, []Instruction{
		pushU64(0x0102),
		pushU64(4),
		{Op: OpToBytesBE},
	})
	require.NoError(t, err)
	got, err := exec.ReturnValue.AsBytes()
	require.NoError(t, err)
	require.Equal(t, []byte{0x00, 0x00, 0x01, 0x02}, got)
}

// TestAVMByteOpBoundsTrap proves every out-of-range byte-op input traps
// deterministically (rolled-back ResultExecutionFailed + error), never panics.
func TestAVMByteOpBoundsTrap(t *testing.T) {
	data := []byte{0x00, 0x01, 0x02, 0x03}
	cases := []struct {
		name string
		code []Instruction
	}{
		{"slice/window past end", []Instruction{pushBytes(data), pushU64(2), pushU64(3), {Op: OpSlice}}},
		{"slice/start past end", []Instruction{pushBytes(data), pushU64(5), pushU64(0), {Op: OpSlice}}},
		{"byteAt/index == len", []Instruction{pushBytes(data), pushU64(4), {Op: OpByteAt}}},
		{"byteAt/empty", []Instruction{pushBytes(nil), pushU64(0), {Op: OpByteAt}}},
		{"toBytesBE/does not fit", []Instruction{pushU64(0x1234), pushU64(1), {Op: OpToBytesBE}}},
		{"fromBytesBE/too wide", []Instruction{pushBytes(make([]byte, 33)), {Op: OpFromBytesBE}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exec, err := runByteCode(t, tc.code)
			require.Error(t, err, "out-of-range byte op must trap")
			require.Equal(t, async.ResultExecutionFailed, exec.ResultCode)
		})
	}
}

// TestAVMFromBytesBEAcceptsFullWidth confirms exactly 32 bytes is the widest
// accepted input (boundary of the uint256 target).
func TestAVMFromBytesBEAcceptsFullWidth(t *testing.T) {
	full := make([]byte, 32)
	for i := range full {
		full[i] = 0xff
	}
	exec, err := runByteCode(t, []Instruction{pushBytes(full), {Op: OpFromBytesBE}})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	got, err := exec.ReturnValue.AsBigInt()
	require.NoError(t, err)
	// 2^256 - 1
	want := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	require.Equal(t, 0, got.Cmp(want))
}

// TestAVMHashAcceptsHashOperand proves a byte-exact hash can consume a hash
// value (the 32 hash bytes), so digests chain (e.g. Bitcoin double-sha256).
func TestAVMHashAcceptsHashOperand(t *testing.T) {
	exec, err := runByteCode(t, []Instruction{
		pushBytes([]byte("preimage")),
		{Op: OpSha256},
		{Op: OpSha256}, // sha256(sha256(preimage)) — operand is a hash value
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	_, err = exec.ReturnValue.AsHash()
	require.NoError(t, err)
}

// byteHashLoopModule loops OpReadMsgBody; <hashOp>; OpDrop `iters` times. Every
// per-instruction flat cost is fixed across runs, so the only gas that varies
// with the message-body size is the per-input-byte charge inside the hash
// handler. Clone of hashLoopModule with a parameterized opcode.
func byteHashLoopModule(op Opcode, iters uint64) Module {
	const ctr = 0
	const loopStart = 2
	const exit = 14
	return Module{
		Version: Version,
		Imports: []HostFunction{HostInspectMsg, HostReturn},
		Exports: map[Entrypoint]uint32{EntryReceiveExternal: 0},
		Code: []Instruction{
			/*0*/ {Op: OpPushU64, Arg: 0},
			/*1*/ {Op: OpStoreLocal, Arg: ctr},
			/*2*/ {Op: OpLoadLocal, Arg: ctr},
			/*3*/ {Op: OpPushU64, Arg: iters},
			/*4*/ {Op: OpLt},
			/*5*/ {Op: OpJumpIfZero, Arg: exit},
			/*6*/ {Op: OpReadMsgBody},
			/*7*/ {Op: op},
			/*8*/ {Op: OpDrop},
			/*9*/ {Op: OpLoadLocal, Arg: ctr},
			/*10*/ {Op: OpPushU64, Arg: 1},
			/*11*/ {Op: OpAdd},
			/*12*/ {Op: OpStoreLocal, Arg: ctr},
			/*13*/ {Op: OpJump, Arg: loopStart},
			/*14*/ {Op: OpReturn, Arg: uint64(async.ResultOK)},
		},
	}
}

func runByteHashGas(t *testing.T, op Opcode, iters uint64, bodySize int, gasLimit uint64) uint64 {
	t.Helper()
	runner := newTestRunner(t)
	msg := testAsyncMessage(testAddr(9), testAddr(8), 1)
	msg.Body = make([]byte, bodySize)
	exec, err := runner.Run(byteHashLoopModule(op, iters), Storage{}, RuntimeContext{
		Entry:    EntryReceiveExternal,
		Message:  msg,
		GasLimit: gasLimit,
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode,
		"execution must complete within gasLimit=%d for bodySize=%d", gasLimit, bodySize)
	return exec.GasUsed
}

// TestAVMByteExactHashGasScalesWithInputSize is the anti-DoS regression: each
// new hash opcode must charge gas proportional to the hashed input size (base +
// per byte), not a flat price, or a hostile contract could hash a huge preimage
// for free. Byte-identical modules differ only in body size, so the delta
// isolates the per-input-byte charge; before the per-byte charge it is ZERO.
func TestAVMByteExactHashGasScalesWithInputSize(t *testing.T) {
	const iters = 100
	const smallSize = 16
	const largeSize = 4096
	const gasLimit = 50_000_000

	for _, op := range []Opcode{OpSha256, OpKeccak256, OpRipemd160, OpSha512, OpBlake2b} {
		gasSmall := runByteHashGas(t, op, iters, smallSize, gasLimit)
		gasLarge := runByteHashGas(t, op, iters, largeSize, gasLimit)
		t.Logf("op=0x%02x gasSmall(%dB)=%d gasLarge(%dB)=%d delta=%d", byte(op), smallSize, gasSmall, largeSize, gasLarge, gasLarge-gasSmall)
		minDelta := uint64(iters) * uint64(largeSize-smallSize) / 2
		require.Greaterf(t, gasLarge, gasSmall+minDelta,
			"hash opcode 0x%02x must charge per input byte; got delta %d, need > %d", byte(op), gasLarge-gasSmall, minDelta)
	}
}

// concatLoopModule loops OpReadMsgBody; OpReadMsgBody; OpConcat; OpDrop, so the
// only size-varying cost is the per-output-byte concat charge (output = 2*body).
func concatLoopModule(iters uint64) Module {
	const ctr = 0
	const loopStart = 2
	const exit = 15
	return Module{
		Version: Version,
		Imports: []HostFunction{HostInspectMsg, HostReturn},
		Exports: map[Entrypoint]uint32{EntryReceiveExternal: 0},
		Code: []Instruction{
			/*0*/ {Op: OpPushU64, Arg: 0},
			/*1*/ {Op: OpStoreLocal, Arg: ctr},
			/*2*/ {Op: OpLoadLocal, Arg: ctr},
			/*3*/ {Op: OpPushU64, Arg: iters},
			/*4*/ {Op: OpLt},
			/*5*/ {Op: OpJumpIfZero, Arg: exit},
			/*6*/ {Op: OpReadMsgBody},
			/*7*/ {Op: OpReadMsgBody},
			/*8*/ {Op: OpConcat},
			/*9*/ {Op: OpDrop},
			/*10*/ {Op: OpLoadLocal, Arg: ctr},
			/*11*/ {Op: OpPushU64, Arg: 1},
			/*12*/ {Op: OpAdd},
			/*13*/ {Op: OpStoreLocal, Arg: ctr},
			/*14*/ {Op: OpJump, Arg: loopStart},
			/*15*/ {Op: OpReturn, Arg: uint64(async.ResultOK)},
		},
	}
}

// TestAVMConcatGasScalesWithOutputSize proves concat charges per output byte
// before allocating, so an oversized result cannot be produced for a flat fee.
func TestAVMConcatGasScalesWithOutputSize(t *testing.T) {
	const iters = 100
	const smallSize = 16
	const largeSize = 4096
	const gasLimit = 50_000_000

	run := func(bodySize int) uint64 {
		runner := newTestRunner(t)
		msg := testAsyncMessage(testAddr(9), testAddr(8), 1)
		msg.Body = make([]byte, bodySize)
		exec, err := runner.Run(concatLoopModule(iters), Storage{}, RuntimeContext{
			Entry:    EntryReceiveExternal,
			Message:  msg,
			GasLimit: gasLimit,
		})
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, exec.ResultCode)
		return exec.GasUsed
	}

	gasSmall := run(smallSize)
	gasLarge := run(largeSize)
	t.Logf("concat gasSmall(%dB)=%d gasLarge(%dB)=%d delta=%d", smallSize, gasSmall, largeSize, gasLarge, gasLarge-gasSmall)
	// Output is 2*body, so the delta is ~iters*2*(large-small); require > the
	// single-width lower bound (comfortably above the ZERO a flat charge gives).
	minDelta := uint64(iters) * uint64(largeSize-smallSize)
	require.Greater(t, gasLarge, gasSmall+minDelta,
		"concat must charge per output byte; got delta %d, need > %d", gasLarge-gasSmall, minDelta)
}

// TestAVMConcatExceedsMaxBytesTraps proves concat refuses to build a result
// larger than MaxBytesLength (deterministic trap, before allocation).
func TestAVMConcatExceedsMaxBytesTraps(t *testing.T) {
	// Two message-body copies of 40000 bytes concatenate to 80000 > 65536.
	half := int(MaxBytesLength/2) + 8000
	runner := newTestRunner(t)
	msg := testAsyncMessage(testAddr(9), testAddr(8), 1)
	msg.Body = make([]byte, half)
	exec, err := runner.Run(Module{
		Version: Version,
		Imports: []HostFunction{HostInspectMsg, HostReturn},
		Exports: map[Entrypoint]uint32{EntryReceiveExternal: 0},
		Code: []Instruction{
			{Op: OpReadMsgBody},
			{Op: OpReadMsgBody},
			{Op: OpConcat},
			{Op: OpReturn, Arg: uint64(async.ResultOK)},
		},
	}, Storage{}, RuntimeContext{
		Entry:    EntryReceiveExternal,
		Message:  msg,
		GasLimit: 50_000_000,
	})
	require.Error(t, err)
	require.Equal(t, async.ResultExecutionFailed, exec.ResultCode)
}

// TestAVMByteExactHashDeterministic runs each hash opcode twice on the same
// input and requires byte-identical results — the determinism floor every
// validator relies on.
func TestAVMByteExactHashDeterministic(t *testing.T) {
	input := []byte("determinism across validators")
	for _, op := range []Opcode{OpSha256, OpKeccak256, OpRipemd160, OpSha512, OpBlake2b} {
		first, err := runByteCode(t, []Instruction{pushBytes(input), {Op: op}})
		require.NoError(t, err)
		second, err := runByteCode(t, []Instruction{pushBytes(input), {Op: op}})
		require.NoError(t, err)
		a, err := runtimeRawBytes(first.ReturnValue)
		require.NoError(t, err)
		b, err := runtimeRawBytes(second.ReturnValue)
		require.NoError(t, err)
		require.Equalf(t, a, b, "opcode 0x%02x must be deterministic", byte(op))
	}
}
