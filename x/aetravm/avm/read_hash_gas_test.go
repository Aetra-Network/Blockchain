package avm

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/aetravm/async"
)

// storageReadLoopModule builds a module whose EntryReceiveExternal handler runs
// a FIXED `iters`-count loop that executes OpReadStorage followed by OpDrop.
// The loop body is a fixed set of instructions independent of how large the
// value behind `key` is, so the ONLY execution cost that can vary with the
// stored value size is the OpReadStorage charge itself.
//
// key == nil selects the whole-state snapshot branch (empty Instruction.Data);
// a non-nil key selects the single-key branch. Both branches run a
// CanonicalDecode over the stored bytes (O(stored size)) yet were charged a
// flat / key-COUNT price -- the R-1 (single key) and R-2 (snapshot) siblings of
// FINDING-001. See avm.go "case OpReadStorage".
func storageReadLoopModule(key []byte, iters uint64) Module {
	const ctr = 0
	const loopStart = 2
	const exit = 13
	return Module{
		Version: Version,
		Imports: []HostFunction{HostReadStorage, HostReturn},
		Exports: map[Entrypoint]uint32{EntryReceiveExternal: 0},
		Code: []Instruction{
			/*0*/ {Op: OpPushU64, Arg: 0},
			/*1*/ {Op: OpStoreLocal, Arg: ctr},
			// LOOP (pc=2..12): while ctr < iters { read; drop; ctr++ }
			/*2*/ {Op: OpLoadLocal, Arg: ctr},
			/*3*/ {Op: OpPushU64, Arg: iters},
			/*4*/ {Op: OpLt},
			/*5*/ {Op: OpJumpIfZero, Arg: exit},
			/*6*/ {Op: OpReadStorage, Data: key},
			/*7*/ {Op: OpDrop},
			/*8*/ {Op: OpLoadLocal, Arg: ctr},
			/*9*/ {Op: OpPushU64, Arg: 1},
			/*10*/ {Op: OpAdd},
			/*11*/ {Op: OpStoreLocal, Arg: ctr},
			/*12*/ {Op: OpJump, Arg: loopStart},
			/*13*/ {Op: OpReturn, Arg: uint64(async.ResultOK)},
		},
	}
}

// hashLoopModule runs a FIXED `iters`-count loop of OpReadMsgBody; OpHash;
// OpDrop. OpReadMsgBody pushes the message body by reference (flat cost,
// size-independent), OpDrop drops the 32-byte hash result (size-independent),
// and the loop-counter ops all work on small integers, so the ONLY cost that
// can vary with the body size is the OpHash charge -- the R-2/R-3 sibling of
// FINDING-001. See avm.go "case OpHash" / runtimeHashValue.
func hashLoopModule(iters uint64) Module {
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
			// LOOP (pc=2..13): while ctr < iters { hash(body); drop; ctr++ }
			/*2*/ {Op: OpLoadLocal, Arg: ctr},
			/*3*/ {Op: OpPushU64, Arg: iters},
			/*4*/ {Op: OpLt},
			/*5*/ {Op: OpJumpIfZero, Arg: exit},
			/*6*/ {Op: OpReadMsgBody},
			/*7*/ {Op: OpHash},
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

func runStorageReadGas(t *testing.T, key []byte, iters uint64, valueSize int, gasLimit uint64) uint64 {
	t.Helper()
	runner := newTestRunner(t)
	// Single key holding a value of the requested size. len==8 would be
	// re-interpreted as a bare uint64 by runtimeValueFromStorage, so keep the
	// small size clear of that special case.
	storage := Storage{"k": make([]byte, valueSize)}
	exec, err := runner.Run(storageReadLoopModule(key, iters), storage, RuntimeContext{
		Entry:    EntryReceiveExternal,
		Message:  testAsyncMessage(testAddr(9), testAddr(8), 1),
		GasLimit: gasLimit,
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode,
		"execution must complete within gasLimit=%d for valueSize=%d (raise gasLimit if this fails)", gasLimit, valueSize)
	return exec.GasUsed
}

// TestAVMReadStorageSingleKeyGasScalesWithValueSize is the regression test for
// R-1: the single-key OpReadStorage branch ran a CanonicalDecode over the
// stored bytes (O(len(value))) but was charged only the flat per-opcode price,
// so a key holding a large value read at the identical gas as a tiny one. That
// is a DoS/consensus-liveness sibling of the FINDING-001 snapshot fix that the
// original fix skipped.
//
// The loop body and iteration count are identical between the two runs, so
// every flat charge cancels and gasLarge-gasSmall isolates exactly the
// per-read operand charge difference. Before the fix that difference is ZERO
// (this test fails); after it, it tracks the value size.
func TestAVMReadStorageSingleKeyGasScalesWithValueSize(t *testing.T) {
	const iters = 200
	const smallSize = 16
	const largeSize = 16 * 1024
	const gasLimit = 50_000_000

	gasSmall := runStorageReadGas(t, []byte("k"), iters, smallSize, gasLimit)
	gasLarge := runStorageReadGas(t, []byte("k"), iters, largeSize, gasLimit)

	t.Logf("single-key read: gasSmall(%dB)=%d gasLarge(%dB)=%d delta=%d", smallSize, gasSmall, largeSize, gasLarge, gasLarge-gasSmall)

	// Under the fixed pricing each of `iters` reads is charged
	// GasPerOperandUnit per stored byte, so the delta must be ~iters*(large-
	// small). Require at least half of that -- comfortably above the ZERO the
	// bug produced.
	minDelta := uint64(iters) * uint64(largeSize-smallSize) / 2
	require.Greater(t, gasLarge, gasSmall+minDelta,
		"FIX REGRESSION (R-1): single-key OpReadStorage must charge gas proportional to the decoded value size; got delta %d, need > %d", gasLarge-gasSmall, minDelta)
}

// TestAVMReadStorageSnapshotGasScalesWithValueSize is the regression test for
// R-2: the whole-state snapshot OpReadStorage branch was charged by KEY COUNT
// (uint64(len(state))) while runtimeStorageSnapshotValue decodes EVERY stored
// value. A state of one key holding a large value was therefore billed a single
// operand unit for an O(value) decode on every read.
//
// Both runs hold exactly ONE key (so the key-count charge is identical); only
// the value size differs, so gasLarge-gasSmall isolates the missing per-byte
// decode charge. Before the fix that delta is ZERO (this test fails).
func TestAVMReadStorageSnapshotGasScalesWithValueSize(t *testing.T) {
	const iters = 200
	const smallSize = 16
	const largeSize = 16 * 1024
	const gasLimit = 50_000_000

	// nil key => empty Instruction.Data => snapshot branch.
	gasSmall := runStorageReadGas(t, nil, iters, smallSize, gasLimit)
	gasLarge := runStorageReadGas(t, nil, iters, largeSize, gasLimit)

	t.Logf("snapshot read: gasSmall(%dB)=%d gasLarge(%dB)=%d delta=%d", smallSize, gasSmall, largeSize, gasLarge, gasLarge-gasSmall)

	minDelta := uint64(iters) * uint64(largeSize-smallSize) / 2
	require.Greater(t, gasLarge, gasSmall+minDelta,
		"FIX REGRESSION (R-2): snapshot OpReadStorage must charge gas proportional to the total decoded bytes, not the key count; got delta %d, need > %d", gasLarge-gasSmall, minDelta)
}

func runHashGas(t *testing.T, iters uint64, bodySize int, gasLimit uint64) uint64 {
	t.Helper()
	runner := newTestRunner(t)
	msg := testAsyncMessage(testAddr(9), testAddr(8), 1)
	msg.Body = make([]byte, bodySize)
	exec, err := runner.Run(hashLoopModule(iters), Storage{}, RuntimeContext{
		Entry:    EntryReceiveExternal,
		Message:  msg,
		GasLimit: gasLimit,
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode,
		"execution must complete within gasLimit=%d for bodySize=%d (raise gasLimit if this fails)", gasLimit, bodySize)
	return exec.GasUsed
}

// TestAVMHashGasScalesWithValueSize is the regression test for R-3: OpHash ran
// an O(value-size) chunk-tree build / CanonicalEncode + sha256 but was charged
// the flat GasSchedule[OpHash] price, so hashing a large value cost the same
// gas as hashing a tiny one.
//
// The two runs execute the byte-identical module and differ only in the runtime
// message body they hash, so every flat charge (and the fixed 32-byte hash
// drop) cancels and gasLarge-gasSmall isolates exactly the OpHash operand
// charge. Before the fix that delta is ZERO (this test fails).
func TestAVMHashGasScalesWithValueSize(t *testing.T) {
	const iters = 200
	const smallSize = 16
	const largeSize = 4096
	const gasLimit = 50_000_000

	gasSmall := runHashGas(t, iters, smallSize, gasLimit)
	gasLarge := runHashGas(t, iters, largeSize, gasLimit)

	t.Logf("hash: gasSmall(%dB)=%d gasLarge(%dB)=%d delta=%d", smallSize, gasSmall, largeSize, gasLarge, gasLarge-gasSmall)

	minDelta := uint64(iters) * uint64(largeSize-smallSize) / 2
	require.Greater(t, gasLarge, gasSmall+minDelta,
		"FIX REGRESSION (R-3): OpHash must charge gas proportional to the hashed value size; got delta %d, need > %d", gasLarge-gasSmall, minDelta)
}
