package avm

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/aetravm/async"
)

// This file exercises the intra-contract OpCall/OpRet call stack directly at
// the interpreter level (call mechanism v5 design doc §1), by hand-assembling
// Module.Code sequences the way the compiler would after §1.7's lowering --
// no compiler involvement, so these tests pin the VM-level contract the
// compiler must produce correct bytecode against, independent of whether the
// compiler-side wiring has any bugs of its own.

// callAddModule: entrypoint at pc=0 pushes 5 and 7, OpCall's into a function
// body (`add`) at pc=4 that pops both args into locals 0/1 (module-wide slot
// allocator, §1.1), adds them, and OpRet's back to the caller, which then
// OpReturn's the sum. Verifies basic argument passing (reverse-pop
// convention), locals isolation, and that the return value flows back on the
// shared stack exactly where OpCall left off.
func callAddModule() Module {
	return Module{
		Version: Version,
		Imports: []HostFunction{HostReturn},
		Exports: map[Entrypoint]uint32{EntryReceiveInternal: 0},
		Code: []Instruction{
			// entrypoint (pc 0..3)
			{Op: OpPushU64, Arg: 5},     // 0
			{Op: OpPushU64, Arg: 7},     // 1
			{Op: OpCall, Arg: 4},        // 2: call add(5, 7)
			{Op: OpReturn, Arg: 0},      // 3: return sum
			// add(a, b) body (pc 4..8), slots 0=a? see binding order below
			{Op: OpStoreLocal, Arg: 1}, // 4: b (last pushed, first popped) -> slot 1
			{Op: OpStoreLocal, Arg: 0}, // 5: a -> slot 0
			{Op: OpLoadLocal, Arg: 0},  // 6
			{Op: OpLoadLocal, Arg: 1},  // 7
			{Op: OpAdd},                // 8
			{Op: OpRet},                // 9
		},
	}
}

func TestCallReturnsValueAndResumesCaller(t *testing.T) {
	runner := newTestRunner(t)
	module := callAddModule()
	ctx := runtimeCtx(EntryReceiveInternal)

	exec, err := runner.Run(module, Storage{}, ctx)
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	sum, err := exec.ReturnValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(12), sum)
}

// nestedCallModule: entrypoint calls f(), f() calls g() before returning its
// own computed value, exercising two simultaneously-open (but never
// simultaneously-live-same-function) call frames and disjoint locals slots
// across three regions (entrypoint, f, g).
func nestedCallModule() Module {
	return Module{
		Version: Version,
		Imports: []HostFunction{HostReturn},
		Exports: map[Entrypoint]uint32{EntryReceiveInternal: 0},
		Code: []Instruction{
			// entrypoint (pc 0..2): call f(), return its value
			{Op: OpPushU64, Arg: 100},
			{Op: OpCall, Arg: 3}, // -> f at pc 3
			{Op: OpReturn, Arg: 0},
			// f(x) body (pc 3..8): slot 0 = x. computes g(x) + x, returns it.
			{Op: OpStoreLocal, Arg: 0}, // 3: x -> slot 0
			{Op: OpLoadLocal, Arg: 0},  // 4: push x for g's argument
			{Op: OpCall, Arg: 9},       // 5: call g(x) -> pc 9
			{Op: OpLoadLocal, Arg: 0},  // 6: push x again
			{Op: OpAdd},                // 7: g(x) + x
			{Op: OpRet},                // 8
			// g(y) body (pc 9..12): slot 1 = y (disjoint from f's slot 0).
			// returns y * 2 (via add, no mul needed).
			{Op: OpStoreLocal, Arg: 1}, // 9: y -> slot 1
			{Op: OpLoadLocal, Arg: 1},  // 10
			{Op: OpLoadLocal, Arg: 1},  // 11
			{Op: OpAdd},                // 12: y + y
			{Op: OpRet},                // 13
		},
	}
}

func TestNestedCallsUseDisjointLocalsAndComposeCorrectly(t *testing.T) {
	runner := newTestRunner(t)
	module := nestedCallModule()
	ctx := runtimeCtx(EntryReceiveInternal)

	exec, err := runner.Run(module, Storage{}, ctx)
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	value, err := exec.ReturnValue.AsUint64()
	require.NoError(t, err)
	// f(100) = g(100) + 100 = (100+100) + 100 = 300.
	require.Equal(t, uint64(300), value)
}

// selfRecursiveModule hand-crafts a module that calls itself unconditionally
// -- exactly the shape the compiler's validateFunctionRecursion rejects at
// compile time (design doc §1.1), but reachable via raw adversarial
// MsgStoreCode bytecode (§1.6). Used to test that Params.MaxCallDepth is a
// genuine runtime enforcement, not merely a compiler invariant.
func selfRecursiveModule() Module {
	return Module{
		Version: Version,
		Imports: []HostFunction{HostReturn},
		Exports: map[Entrypoint]uint32{EntryReceiveInternal: 0},
		Code: []Instruction{
			{Op: OpCall, Arg: 0}, // 0: call self, unconditionally, forever
			{Op: OpReturn, Arg: 0},
		},
	}
}

func TestCallDepthLimitStopsAdversarialSelfRecursion(t *testing.T) {
	params := DefaultParams()
	params.MaxCallDepth = 8
	runner, err := NewRunner(params)
	require.NoError(t, err)
	module := selfRecursiveModule()
	ctx := runtimeCtx(EntryReceiveInternal)
	ctx.GasLimit = 1_000_000 // gas must not be what stops this -- depth must be

	exec, err := runner.Run(module, Storage{}, ctx)
	require.NoError(t, err)
	require.Equal(t, async.ResultLimitExceeded, exec.ResultCode)
	// Depth cap must be hit well before the (deliberately generous) gas
	// budget could ever be exhausted, proving depth -- not gas -- is what
	// bounded this adversarial module.
	require.Less(t, exec.GasUsed, uint64(1_000_000))
}

// TestCallDepthDefaultIsPositiveAndConfigurable exercises Params.Validate()'s
// new MaxCallDepth requirement directly.
func TestCallDepthDefaultIsPositiveAndConfigurable(t *testing.T) {
	params := DefaultParams()
	require.Equal(t, uint32(DefaultMaxCallDepth), params.MaxCallDepth)
	require.NoError(t, params.Validate())

	params.MaxCallDepth = 0
	require.ErrorContains(t, params.Validate(), "max call depth")
}

// trapMidCallModule writes to storage, then calls a function whose body
// traps (stack underflow via a bogus OpAdd with nothing pushed) -- exercises
// §1.5's claim that an abort anywhere, including inside a called function,
// rolls back the ENTIRE execution's storage, not just the called function's
// own (nonexistent) frame.
func trapMidCallModule() Module {
	return Module{
		Version: Version,
		Imports: []HostFunction{HostWriteStorage, HostReturn},
		Exports: map[Entrypoint]uint32{EntryReceiveInternal: 0},
		Code: []Instruction{
			{Op: OpPushU64, Arg: 999},
			{Op: OpWriteStorage, Data: []byte("marker")}, // write BEFORE the call
			{Op: OpCall, Arg: 4},
			{Op: OpReturn, Arg: 0},
			// callee body (pc 4): OpAdd with an empty stack -- traps.
			{Op: OpAdd}, // 4
			{Op: OpRet}, // 5 (unreached)
		},
	}
}

func TestTrapInsideCalledFunctionRollsBackWholeExecution(t *testing.T) {
	runner := newTestRunner(t)
	module := trapMidCallModule()
	ctx := runtimeCtx(EntryReceiveInternal)
	storage := Storage{}

	exec, err := runner.Run(module, storage, ctx)
	require.Error(t, err)
	require.Equal(t, async.ResultExecutionFailed, exec.ResultCode)
	// The pre-call write must NOT survive -- rollback() discards the whole
	// `state`, not a per-frame overlay (there is no such thing, §1.5).
	_, wrote := exec.State["marker"]
	require.False(t, wrote, "storage write before the trapping call must be rolled back")
	require.Empty(t, exec.State)
}

// TestRetWithEmptyCallStackTraps: adversarial raw bytecode with an OpRet that
// was never preceded by a matching OpCall (unreachable for compiler output,
// per §1.2's doc comment on OpRet).
func TestRetWithEmptyCallStackTraps(t *testing.T) {
	runner := newTestRunner(t)
	module := Module{
		Version: Version,
		Imports: []HostFunction{HostReturn},
		Exports: map[Entrypoint]uint32{EntryReceiveInternal: 0},
		Code: []Instruction{
			{Op: OpRet},
			{Op: OpReturn, Arg: 0},
		},
	}
	ctx := runtimeCtx(EntryReceiveInternal)

	exec, err := runner.Run(module, Storage{}, ctx)
	require.Error(t, err)
	require.Equal(t, async.ResultExecutionFailed, exec.ResultCode)
}

// TestCallTargetOutOfRangeTraps: adversarial OpCall pointed past the end of
// the module's own code -- must be rejected with the same runtime bound
// OpJump already gets (design doc §1.2), not merely at Verify time.
func TestCallTargetOutOfRangeTraps(t *testing.T) {
	runner := newTestRunner(t)
	module := Module{
		Version: Version,
		Imports: []HostFunction{HostReturn},
		Exports: map[Entrypoint]uint32{EntryReceiveInternal: 0},
		Code: []Instruction{
			{Op: OpCall, Arg: 999},
			{Op: OpReturn, Arg: 0},
		},
	}
	ctx := runtimeCtx(EntryReceiveInternal)

	exec, err := runner.Run(module, Storage{}, ctx)
	require.Error(t, err)
	require.Equal(t, async.ResultExecutionFailed, exec.ResultCode)
}

// TestGasAccountingIsSharedAcrossCallBoundary: gas for a version of the
// module using OpCall to reach a body must equal gas for running the exact
// same instruction sequence flattened inline (no call at all) plus the
// OpCall/OpRet pair's own flat costs -- i.e. one shared counter, no
// double-charge and no free ride, per design doc §1.4.
func TestGasAccountingIsSharedAcrossCallBoundary(t *testing.T) {
	runner := newTestRunner(t)
	ctx := runtimeCtx(EntryReceiveInternal)

	called := callAddModule()
	calledExec, err := runner.Run(called, Storage{}, ctx)
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, calledExec.ResultCode)

	inline := Module{
		Version: Version,
		Imports: []HostFunction{HostReturn},
		Exports: map[Entrypoint]uint32{EntryReceiveInternal: 0},
		Code: []Instruction{
			{Op: OpPushU64, Arg: 5},
			{Op: OpPushU64, Arg: 7},
			{Op: OpStoreLocal, Arg: 1},
			{Op: OpStoreLocal, Arg: 0},
			{Op: OpLoadLocal, Arg: 0},
			{Op: OpLoadLocal, Arg: 1},
			{Op: OpAdd},
			{Op: OpReturn, Arg: 0},
		},
	}
	inlineExec, err := runner.Run(inline, Storage{}, ctx)
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, inlineExec.ResultCode)

	params := DefaultParams()
	callRetCost := params.GasSchedule[OpCall] + params.GasSchedule[OpRet]
	require.Equal(t, inlineExec.GasUsed+callRetCost, calledExec.GasUsed,
		"OpCall/OpRet must add exactly their own flat cost on top of the callee's own metered work -- one shared counter, no double charge, no free ride")
}

// TestDeepCallChainGasGrowsLinearlyAndRespectsLimit builds a straight-line
// chain of N functions (f0 calls f1 calls f2 ... calls fN-1, which returns a
// constant), each hop costing exactly one OpCall + one OpRet beyond the
// innermost body, and checks the depth cap trips exactly where expected
// while a chain just under the limit still completes.
func chainModule(depth int) Module {
	// Layout: entry (pc 0: call f0, pc1: return), then depth functions, each
	// 2 instructions (call next / ret), the last function pushes a constant
	// and rets.
	code := []Instruction{
		{Op: OpCall, Arg: 2}, // 0: entry calls f0 at pc 2
		{Op: OpReturn, Arg: 0},
	}
	for i := 0; i < depth; i++ {
		if i == depth-1 {
			code = append(code,
				Instruction{Op: OpPushU64, Arg: 42},
				Instruction{Op: OpRet},
			)
		} else {
			nextTarget := uint64(len(code) + 2)
			code = append(code,
				Instruction{Op: OpCall, Arg: nextTarget},
				Instruction{Op: OpRet},
			)
		}
	}
	return Module{
		Version: Version,
		Imports: []HostFunction{HostReturn},
		Exports: map[Entrypoint]uint32{EntryReceiveInternal: 0},
		Code:    code,
	}
}

func TestCallDepthLimitTripsAtExactBoundary(t *testing.T) {
	// depth = MaxCallDepth exactly: entry's own OpCall is depth 1, then
	// (depth-1) more nested OpCalls to reach the last function -- total
	// `depth` simultaneously-open calls at the deepest point, which must
	// fit under a limit of `depth`.
	const depth = 5
	params := DefaultParams()
	params.MaxCallDepth = uint32(depth)
	runner, err := NewRunner(params)
	require.NoError(t, err)
	ctx := runtimeCtx(EntryReceiveInternal)

	ok := chainModule(depth)
	exec, err := runner.Run(ok, Storage{}, ctx)
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	value, err := exec.ReturnValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(42), value)

	tooDeep := chainModule(depth + 1)
	execTooDeep, err := runner.Run(tooDeep, Storage{}, ctx)
	require.NoError(t, err)
	require.Equal(t, async.ResultLimitExceeded, execTooDeep.ResultCode)
}

// TestMakeTupleConstructsAndIndexesElements exercises OpMakeTuple end to end:
// building a 3-element tuple and reading each element back by decimal index
// via the extended runtimeFieldValue TagTuple branch (design doc §2.1/§2.2).
func TestMakeTupleConstructsAndIndexesElements(t *testing.T) {
	runner := newTestRunner(t)
	module := Module{
		Version: Version,
		Imports: []HostFunction{HostReturn},
		Exports: map[Entrypoint]uint32{EntryReceiveInternal: 0},
		Code: []Instruction{
			{Op: OpPushU64, Arg: 10},
			{Op: OpPushU64, Arg: 20},
			{Op: OpPushU64, Arg: 30},
			{Op: OpMakeTuple, Arg: 3},
			{Op: OpDup},
			{Op: OpReadField, Data: []byte("2")}, // element index 2 -> 30
			{Op: OpReturn, Arg: 0},
		},
	}
	ctx := runtimeCtx(EntryReceiveInternal)
	exec, err := runner.Run(module, Storage{}, ctx)
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	v, err := exec.ReturnValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(30), v)
}

func TestMakeTupleUnderflowTraps(t *testing.T) {
	runner := newTestRunner(t)
	module := Module{
		Version: Version,
		Imports: []HostFunction{HostReturn},
		Exports: map[Entrypoint]uint32{EntryReceiveInternal: 0},
		Code: []Instruction{
			{Op: OpPushU64, Arg: 10},
			{Op: OpMakeTuple, Arg: 3}, // only 1 value on stack, needs 3
			{Op: OpReturn, Arg: 0},
		},
	}
	ctx := runtimeCtx(EntryReceiveInternal)
	exec, err := runner.Run(module, Storage{}, ctx)
	require.Error(t, err)
	require.Equal(t, async.ResultExecutionFailed, exec.ResultCode)
}

func TestMakeTupleElementCountAboveLimitFailsVerify(t *testing.T) {
	verifier := newTestVerifier(t)
	module := Module{
		Version: Version,
		Imports: []HostFunction{HostReturn},
		Exports: map[Entrypoint]uint32{EntryReceiveInternal: 0},
		Code: []Instruction{
			{Op: OpMakeTuple, Arg: uint64(MaxTupleElements) + 1},
			{Op: OpReturn, Arg: 0},
		},
	}
	require.ErrorContains(t, verifier.Verify(module), "tuple element count")
}
