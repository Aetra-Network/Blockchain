package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
)

// This file exercises the compiler-side wiring of the intra-contract
// CALL/RET call mechanism (design doc §1.7): a call to a declared function
// whose body tryInlineUserFunctionCall's narrow AST-splice cannot represent
// (branching, looping, multiple statements, or an early return not in tail
// position) must compile to a real OpCall/OpRet pair instead of failing with
// "cannot be lowered by AVM v1" -- and running the result through the real
// interpreter must produce the correct value, not just "compiles."

// branchingHelperSource declares a genuinely non-trivial helper (an `if`
// with no `else`, followed by a second `return` -- two return statements,
// not tryInlineUserFunctionCall's single-return-expression shape) and calls
// it from a getter that reads its input off storage, so a single compiled
// module can be re-run against different storage to exercise both branches.
const branchingHelperSource = `
struct DemoState {
  count: uint64 = 0
}

@pure func maxOf(a: uint64, b: uint64) -> uint64 {
  if a > b {
    return a
  }
  return b
}

contract Demo {
  storage: DemoState
  incomingMessages: DemoState

  @internal
  func onInternalMessage(in: InMessage) {
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func maxWithConst(): uint64 {
    const st = DemoState.load()
    return maxOf(st.count, 42)
  }
}
`

func TestCompileRealCallForBranchingHelperReturnsCorrectResult(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	res, err := c.Compile([]byte(branchingHelperSource))
	require.NoError(t, err, "a branching, multi-statement, early-returning helper must compile via real CALL/RET, not fail with AVM v1's inline-only error")
	require.True(t, hasOpcode(res.Module.Code, avm.OpCall), "a non-trivial helper call must lower to a real OpCall")
	require.True(t, hasOpcode(res.Module.Code, avm.OpRet), "a non-trivial helper's body must terminate with OpRet, not OpReturn")

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	sel := getterSelector(t, res, "maxWithConst")

	// a=st.count=10, b=42: 10 > 42 is false -> falls through to `return b` (42).
	lowExec, err := runner.Run(res.Module, avm.Storage{"count": avm.EncodeU64(10)}, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		Message:  async.MessageEnvelope{Opcode: sel, QueryID: 1, GasLimit: 100_000},
		GasLimit: 100_000,
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, lowExec.ResultCode)
	got, err := lowExec.ReturnValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(42), got)

	// a=st.count=100, b=42: 100 > 42 is true -> early return from inside the
	// if-branch (a).
	highExec, err := runner.Run(res.Module, avm.Storage{"count": avm.EncodeU64(100)}, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		Message:  async.MessageEnvelope{Opcode: sel, QueryID: 2, GasLimit: 100_000},
		GasLimit: 100_000,
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, highExec.ResultCode)
	got, err = highExec.ReturnValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(100), got)
}

// TestCompileTrivialHelperStillInlinesWithoutOpCall is a non-regression
// guard: a helper whose body IS the trivial "lazy bindings + one return
// expression" shape must still take tryInlineUserFunctionCall's cheap,
// jump-free path -- the real CALL/RET fallback added by this design must
// never fire for a call tryInlineUserFunctionCall can already handle
// (design doc §1.7: "both paths are safe to keep side by side; which one a
// given call site uses is an optimization choice").
func TestCompileTrivialHelperStillInlinesWithoutOpCall(t *testing.T) {
	src := `
struct DemoState {
  count: uint64 = 0
}

@pure func pickMin(a: uint64, b: uint64) -> uint64 {
  return a < b ? a : b
}

contract Demo {
  storage: DemoState
  incomingMessages: DemoState

  @internal
  func onInternalMessage(in: InMessage) {
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func clamped(): uint64 {
    const st = DemoState.load()
    return pickMin(st.count, 100)
  }
}
`
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	res, err := c.Compile([]byte(src))
	require.NoError(t, err)
	require.False(t, hasOpcode(res.Module.Code, avm.OpCall), "a trivial single-return helper must still be inlined, not compiled to a real call")
	require.False(t, hasOpcode(res.Module.Code, avm.OpRet), "no called-function block should exist when every call site was inlined")
}

// helperCalledFromTwoGettersSource declares one non-trivial helper called
// from two different getters, to check the "compiled once, shared by every
// call site" dedup (design doc §1.7: "Each distinct called function is
// lowered once").
const helperCalledFromTwoGettersSource = `
struct DemoState {
  count: uint64 = 0
}

@pure func clampTo(x: uint64, ceiling: uint64) -> uint64 {
  if x > ceiling {
    return ceiling
  }
  return x
}

contract Demo {
  storage: DemoState
  incomingMessages: DemoState

  @internal
  func onInternalMessage(in: InMessage) {
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func clampedLow(): uint64 {
    const st = DemoState.load()
    return clampTo(st.count, 10)
  }

  @get
  func clampedHigh(): uint64 {
    const st = DemoState.load()
    return clampTo(st.count, 1000)
  }
}
`

func TestCompileRealCallDedupesSharedHelperAcrossCallSites(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	res, err := c.Compile([]byte(helperCalledFromTwoGettersSource))
	require.NoError(t, err)

	var callTargets []uint64
	for _, ins := range res.Module.Code {
		if ins.Op == avm.OpCall {
			callTargets = append(callTargets, ins.Arg)
		}
	}
	require.Len(t, callTargets, 2, "both getters call the shared helper once each")
	require.Equal(t, callTargets[0], callTargets[1], "the same helper called from two sites must compile to ONE shared code block, not two independent copies")

	// callTargets[0] == callTargets[1] (checked above) already proves both
	// call sites share ONE compiled block -- clampTo's body legitimately
	// contains two OpRet instructions (one per `return` statement: the
	// if-branch's `return ceiling` and the trailing `return x`), so an
	// OpRet *count* is not itself a duplication signal; a duplicated
	// compile would instead show up as two DIFFERENT call targets, which
	// the check above already rules out.

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	storage := avm.Storage{"count": avm.EncodeU64(500)}

	lowExec, err := runner.Run(res.Module, storage, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		Message:  async.MessageEnvelope{Opcode: getterSelector(t, res, "clampedLow"), QueryID: 1, GasLimit: 100_000},
		GasLimit: 100_000,
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, lowExec.ResultCode)
	v, err := lowExec.ReturnValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(10), v)

	highExec, err := runner.Run(res.Module, storage, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		Message:  async.MessageEnvelope{Opcode: getterSelector(t, res, "clampedHigh"), QueryID: 2, GasLimit: 100_000},
		GasLimit: 100_000,
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, highExec.ResultCode)
	v, err = highExec.ReturnValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(500), v)
}

// nestedRealCallSource: `outer` (non-trivial) calls `inner` (also
// non-trivial), exercising a real nested call chain end to end through the
// actual compiled bytecode and interpreter, not just at the avm-package
// hand-assembled level.
const nestedRealCallSource = `
struct DemoState {
  count: uint64 = 0
}

@pure func inner(x: uint64) -> uint64 {
  if x == 0 {
    return 1
  }
  return x
}

@pure func outer(x: uint64, y: uint64) -> uint64 {
  if x > y {
    return inner(x)
  }
  return inner(y)
}

contract Demo {
  storage: DemoState
  incomingMessages: DemoState

  @internal
  func onInternalMessage(in: InMessage) {
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func compute(): uint64 {
    const st = DemoState.load()
    return outer(st.count, 0)
  }
}
`

func TestCompileNestedRealCallsAcrossTwoFunctions(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	res, err := c.Compile([]byte(nestedRealCallSource))
	require.NoError(t, err)

	callCount := 0
	targets := map[uint64]bool{}
	for _, ins := range res.Module.Code {
		if ins.Op == avm.OpCall {
			callCount++
			targets[ins.Arg] = true
		}
	}
	require.Equal(t, 3, callCount, "outer's two branches each call inner (2 sites) plus the getter's call to outer (1 site)")
	require.Len(t, targets, 2, "only two DISTINCT call targets despite 3 call sites: outer and inner, each compiled once")

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	sel := getterSelector(t, res, "compute")

	// st.count=0 -> outer(0,0): x>y false -> inner(y=0) -> x==0 true -> 1.
	exec, err := runner.Run(res.Module, avm.Storage{"count": avm.EncodeU64(0)}, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		Message:  async.MessageEnvelope{Opcode: sel, QueryID: 1, GasLimit: 100_000},
		GasLimit: 100_000,
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	v, err := exec.ReturnValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(1), v)

	// st.count=7 -> outer(7,0): x>y true -> inner(x=7) -> x==0 false -> 7.
	exec2, err := runner.Run(res.Module, avm.Storage{"count": avm.EncodeU64(7)}, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		Message:  async.MessageEnvelope{Opcode: sel, QueryID: 2, GasLimit: 100_000},
		GasLimit: 100_000,
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec2.ResultCode)
	v, err = exec2.ReturnValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(7), v)
}

// loopHelperSource: a helper with a `while` loop (genuinely not
// representable by tryInlineUserFunctionCall), summing 1..n.
const loopHelperSource = `
struct DemoState {
  count: uint64 = 0
}

@impure func sumTo(n: uint64) -> uint64 {
  var total = 0
  var i = 1
  while i <= n {
    total += i
    i += 1
  }
  return total
}

contract Demo {
  storage: DemoState
  incomingMessages: DemoState

  @internal
  func onInternalMessage(in: InMessage) {
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func triangular(): uint64 {
    const st = DemoState.load()
    return sumTo(st.count)
  }
}
`

func TestCompileLoopHelperRealCallProducesCorrectSum(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	res, err := c.Compile([]byte(loopHelperSource))
	require.NoError(t, err)
	require.True(t, hasOpcode(res.Module.Code, avm.OpCall))
	require.True(t, hasOpcode(res.Module.Code, avm.OpRet))

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	exec, err := runner.Run(res.Module, avm.Storage{"count": avm.EncodeU64(5)}, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		Message:  async.MessageEnvelope{Opcode: getterSelector(t, res, "triangular"), QueryID: 1, GasLimit: 100_000},
		GasLimit: 100_000,
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	v, err := exec.ReturnValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(15), v) // 1+2+3+4+5
}

// TestCompileRealCallDeterministicAcrossRepeatedCompiles guards the module-
// wide slot allocator and the two-pass call-target link against any
// nondeterminism (e.g. map-iteration-order dependence) -- this codebase's
// absolute-determinism requirement applies to compiled bytecode.
func TestCompileRealCallDeterministicAcrossRepeatedCompiles(t *testing.T) {
	var firstBytes []byte
	for i := 0; i < 5; i++ {
		c, err := New(DefaultOptions())
		require.NoError(t, err)
		res, err := c.Compile([]byte(nestedRealCallSource))
		require.NoError(t, err)
		if i == 0 {
			firstBytes = res.ModuleBytes
			continue
		}
		require.Equal(t, firstBytes, res.ModuleBytes, "identical source must compile to byte-identical modules on every repeated run (iteration %d)", i)
	}
}

// TestCompileRealCallWrongArgCountStillRejected guards that the new real-
// call path enforces the same arity check the inliner already did -- the
// fallback must not accidentally become more permissive.
func TestCompileRealCallWrongArgCountStillRejected(t *testing.T) {
	src := `
struct DemoState {
  count: uint64 = 0
}

@pure func maxOf(a: uint64, b: uint64) -> uint64 {
  if a > b {
    return a
  }
  return b
}

contract Demo {
  storage: DemoState
  incomingMessages: DemoState

  @internal
  func onInternalMessage(in: InMessage) {
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func bad(): uint64 {
    return maxOf(1)
  }
}
`
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	_, err = c.Compile([]byte(src))
	require.ErrorContains(t, err, "expects 2 args")
}

// TestCompileRealCallDoesNotBypassRecursionCheck guards that a
// non-trivial (branching) SELF-recursive function -- not just the simple
// tail-call shape the inliner already rejects -- is still caught by
// validateFunctionRecursion before ever reaching the real-call codegen
// path this design adds.
func TestCompileRealCallDoesNotBypassRecursionCheck(t *testing.T) {
	src := `
struct DemoState {
  count: uint64 = 0
}

func countDown(n: uint64) -> uint64 {
  if n == 0 {
    return 0
  }
  return countDown(n - 1)
}

contract Demo {
  storage: DemoState
  incomingMessages: DemoState

  @internal
  func onInternalMessage(in: InMessage) {
    const sanity = countDown(3)
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	_, err = c.Compile([]byte(src))
	require.ErrorContains(t, err, "recursive function call cycle detected")
}
