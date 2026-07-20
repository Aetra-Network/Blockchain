package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
)

// constFoldOverflowSource declares a single-return generic function, addT<T>,
// whose body ("return a + b") is exactly the shape evalConstStatements's
// narrow supported-statement set (StatementReturn) can fold. Every getter
// below calls it with two compile-time integer literals, so every call site
// is fully const-foldable -- the precise condition that used to let
// evalConstValue's ExprCall case (compile.go) resolve the callee by bare
// name (functions[expr.Text], ignoring TypeArgs) and evaluate the addition in
// unchecked native Go uint64 instead of routing through the real,
// width-checked runtimeAdd/enforceIntWidth path (avm.go) that every other
// call shape (e.g. non-literal, message-derived arguments) already used.
const constFoldOverflowSource = `
struct DemoState {
  touch: uint64 = 0
}

func addT<T>(a: T, b: T): T {
  return a + b
}

@message(1)
struct Touch {
  nonce: uint64
}

type ExternalMsg = Touch

contract ConstFoldOverflowDemo {
  storage: DemoState
  incomingExternal: ExternalMsg

  @external
  func onExternalMessage(inMsg: Segment) {
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  // 2^64-1 + 1: the exact repro from the adversarial finding. The old
  // buggy fold computed this in native Go uint64, which silently wraps to
  // 0 and returns ResultOK -- the real AVM ISA instead traps.
  @get
  func addU64Overflow(): uint64 {
    return addT::<uint64>(18446744073709551615, 1)
  }

  // Sanity companion: a non-overflowing literal-argument generic call must
  // still fold/compile/execute to the numerically correct answer -- the
  // fix must not turn every generic literal call into a spurious failure.
  @get
  func addU64Ok(): uint64 {
    return addT::<uint64>(2, 3)
  }
}
`

// TestGenericCallLiteralArgumentsOverflowTraps is the behavioral regression
// test for the fix: a generic call site whose type argument is explicit
// (::<uint64>) must never be folded by evalConstValue's bare-name,
// width-blind ExprCall case. Compile-time folding must bail out whenever
// expr.TypeArgs is non-empty, sending the call through the real lowering
// path (resolveUserFunction's mangled-name resolution, the same path
// tryInlineUserFunctionCall/tryRealUserFunctionCall already use), which
// emits actual opcodes evaluated by the real interpreter instead of
// pre-computing a wrapped constant at compile time. Before the fix,
// addU64Overflow returned ResultOK with value 0 (silent wraparound); after
// the fix it must trap with async.ResultOutOfRange, exactly like the
// equivalent non-literal-argument (message/storage-derived) addition already
// did via OpAdd/runtimeAdd/enforceIntWidth.
func TestGenericCallLiteralArgumentsOverflowTraps(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	res, err := c.Compile([]byte(constFoldOverflowSource))
	require.NoError(t, err)
	require.NoError(t, res.Manifest.Validate())

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	empty := avm.Storage{}
	runGetter := func(getter string) (avm.Execution, error) {
		t.Helper()
		return runner.Run(res.Module, empty, avm.RuntimeContext{
			Entry:    avm.EntryQuery,
			Message:  async.MessageEnvelope{Opcode: getterSelector(t, res, getter), GasLimit: 200_000},
			GasLimit: 200_000,
		})
	}

	// addT::<uint64>(2^64-1, 1): the exact adversarial repro. Must trap,
	// not silently return ResultOK with value 0.
	exec, err := runGetter("addU64Overflow")
	require.Error(t, err, "addT::<uint64>(2^64-1, 1) must fail, not silently wrap to 0")
	require.Equal(t, async.ResultOutOfRange, exec.ResultCode,
		"addT::<uint64>(2^64-1, 1) must trap with the same ResultOutOfRange code the "+
			"real OpAdd/runtimeAdd/enforceIntWidth path uses for every non-literal-argument overflow")

	// addT::<uint64>(2, 3): a non-overflowing literal-argument generic call
	// must still compile and execute to the numerically correct answer.
	exec, err = runGetter("addU64Ok")
	require.NoError(t, err, "addT::<uint64>(2, 3) must succeed")
	require.Equal(t, async.ResultOK, exec.ResultCode)
	v, err := exec.ReturnValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(5), v, "addT::<uint64>(2, 3)")
}
