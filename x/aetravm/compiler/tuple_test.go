package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
)

// This file exercises the tuple surface (design doc §2): tuple literals
// `(a, b)`, destructuring `const (a, b) = ...`, and a function's
// parenthesized multi-value return type `-> (T1, T2)`, end to end through
// the real compiler + interpreter, not just at the value-representation
// level (already covered by x/aetravm/avm's own tests).

// divModSource declares a genuinely non-trivial (branching -- not
// tryInlineUserFunctionCall's shape) function with a multi-value return
// type, called and destructured from a getter.
const divModSource = `
struct DemoState {
  count: uint64 = 0
}

func divMod(a: uint64, b: uint64) -> (uint64, uint64) {
  if b == 0 {
    return (0, 0)
  }
  return (a / b, a % b)
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
  func quotient(): uint64 {
    const st = DemoState.load()
    const (q, r) = divMod(st.count, 3)
    return q + r
  }
}
`

func TestCompileMultiValueReturnAndDestructuring(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	res, err := c.Compile([]byte(divModSource))
	require.NoError(t, err, "a function with a parenthesized multi-value return type, destructured at the call site, must compile")
	require.True(t, hasOpcode(res.Module.Code, avm.OpCall))
	require.True(t, hasOpcode(res.Module.Code, avm.OpRet))
	require.True(t, hasOpcode(res.Module.Code, avm.OpMakeTuple), "the (a/b, a%%b) return value must construct a real tuple")

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	sel := getterSelector(t, res, "quotient")

	// count=10, divMod(10,3) = (3, 1) -> q+r = 4.
	exec, err := runner.Run(res.Module, avm.Storage{"count": avm.EncodeU64(10)}, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		Message:  async.MessageEnvelope{Opcode: sel, QueryID: 1, GasLimit: 100_000},
		GasLimit: 100_000,
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	v, err := exec.ReturnValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(4), v)

	// count=7, divMod(7,3) = (2, 1) -> q+r = 3.
	exec2, err := runner.Run(res.Module, avm.Storage{"count": avm.EncodeU64(7)}, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		Message:  async.MessageEnvelope{Opcode: sel, QueryID: 2, GasLimit: 100_000},
		GasLimit: 100_000,
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec2.ResultCode)
	v, err = exec2.ReturnValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(3), v)
}

// tupleLiteralLocalSource exercises destructuring a BARE tuple literal
// (not a function call), and 3-element tuples.
const tupleLiteralLocalSource = `
struct DemoState {
  count: uint64 = 0
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
  func triple(): uint64 {
    const st = DemoState.load()
    const (a, b, c) = (st.count, st.count + 1, st.count + 2)
    return a + b + c
  }
}
`

func TestCompileDestructuresBareTupleLiteral(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	res, err := c.Compile([]byte(tupleLiteralLocalSource))
	require.NoError(t, err)
	require.True(t, hasOpcode(res.Module.Code, avm.OpMakeTuple))

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	exec, err := runner.Run(res.Module, avm.Storage{"count": avm.EncodeU64(10)}, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		Message:  async.MessageEnvelope{Opcode: getterSelector(t, res, "triple"), QueryID: 1, GasLimit: 100_000},
		GasLimit: 100_000,
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	v, err := exec.ReturnValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(10+11+12), v)
}

// TestCompileTupleArityMismatchRejected guards that a destructuring binding
// whose name count doesn't match the tuple's element count is a compile
// error, not a silent truncation/padding.
func TestCompileTupleArityMismatchRejected(t *testing.T) {
	src := `
struct DemoState {
  count: uint64 = 0
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
    const (a, b, c) = (1, 2)
    return a + b + c
  }
}
`
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	_, err = c.Compile([]byte(src))
	require.ErrorContains(t, err, "destructuring binding expects a 3-element tuple")
}

// TestCompileTupleFieldAccessByIndex exercises reading a tuple element back
// by decimal index via ordinary field-access syntax (design doc §2.1's
// extended runtimeFieldValue), not just destructuring.
const tupleIndexAccessSource = `
struct DemoState {
  count: uint64 = 0
}

func pair(a: uint64, b: uint64) -> (uint64, uint64) {
  return (a, b)
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
  func second(): uint64 {
    const st = DemoState.load()
    const p = pair(st.count, 99)
    return p.1
  }
}
`

func TestCompileTupleFieldAccessByDecimalIndex(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	res, err := c.Compile([]byte(tupleIndexAccessSource))
	require.NoError(t, err)

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	exec, err := runner.Run(res.Module, avm.Storage{"count": avm.EncodeU64(5)}, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		Message:  async.MessageEnvelope{Opcode: getterSelector(t, res, "second"), QueryID: 1, GasLimit: 100_000},
		GasLimit: 100_000,
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	v, err := exec.ReturnValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(99), v)
}

// TestCompileTupleDeterministicAcrossRepeatedCompiles guards the new tuple
// grammar/lowering against any nondeterminism, matching this codebase's
// absolute-determinism requirement.
func TestCompileTupleDeterministicAcrossRepeatedCompiles(t *testing.T) {
	var firstBytes []byte
	for i := 0; i < 5; i++ {
		c, err := New(DefaultOptions())
		require.NoError(t, err)
		res, err := c.Compile([]byte(divModSource))
		require.NoError(t, err)
		if i == 0 {
			firstBytes = res.ModuleBytes
			continue
		}
		require.Equal(t, firstBytes, res.ModuleBytes, "identical tuple-using source must compile to byte-identical modules on every repeated run (iteration %d)", i)
	}
}

// TestCompileOrdinaryParenthesizedExpressionStillWorks is a non-regression
// guard: a single parenthesized expression (no comma) must still parse as
// ordinary grouping, not a one-element tuple -- the design doc's own claim
// that this is a zero-collision additive grammar extension.
func TestCompileOrdinaryParenthesizedExpressionStillWorks(t *testing.T) {
	src := `
struct DemoState {
  count: uint64 = 0
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
  func grouped(): uint64 {
    const st = DemoState.load()
    return (st.count + 1) * 2
  }
}
`
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	res, err := c.Compile([]byte(src))
	require.NoError(t, err)
	require.False(t, hasOpcode(res.Module.Code, avm.OpMakeTuple), "an ordinary parenthesized grouping expression must not become a tuple")

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	exec, err := runner.Run(res.Module, avm.Storage{"count": avm.EncodeU64(9)}, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		Message:  async.MessageEnvelope{Opcode: getterSelector(t, res, "grouped"), QueryID: 1, GasLimit: 100_000},
		GasLimit: 100_000,
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	v, err := exec.ReturnValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(20), v) // (9+1)*2
}
