package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// This file used to document a genuine correctness gap discovered while
// building this test suite: evalConstValue's ExprCall case (compile.go, the
// compiler's PRE-EXISTING, generics-independent compile-time constant-folder
// for literal-argument calls to plain declared functions) resolved its
// callee with a bare-name lookup -- `functions[expr.Text]` -- that
// completely ignored expr.TypeArgs. For an ordinary (non-generic) function
// this was harmless (bare name IS the only entry). For a GENERIC call site
// (`f::<uint128>(...)`), `expr.Text` is still just "f", so this lookup found
// the ORIGINAL, still-generic FunctionDecl (TypeParams non-empty, param/
// return types literally named after the type parameters) -- completely
// bypassing resolveCallFunction/resolveUserFunction's generics-aware
// mangled-name resolution, and therefore bypassing instantiateGenericFunction,
// compileCalledFunction, and claimLocalSlot entirely for that call site. Worse
// than the missing bookkeeping, it also evaluated the call's arithmetic in
// unchecked native Go uint64 with no notion of the instantiation's declared
// width, so a literal-argument generic call that should have overflow-trapped
// (the same way runtimeAdd/enforceIntWidth traps every other call shape,
// avm.go) could silently compute a wrapped, wrong answer instead -- see
// generics_const_fold_overflow_test.go for the dedicated behavioral
// regression test on that.
//
// FIXED: evalConstValue's ExprCall case now bails out of constant-folding
// whenever expr.TypeArgs is non-empty (compile.go, immediately before the
// bare-name `functions[expr.Text]` lookup), sending every generic call site
// through the real lowering path (tryInlineUserFunctionCall /
// tryRealUserFunctionCall), which resolves through resolveUserFunction's
// mangled-name lookup the same way every other generics-aware call site
// already did. This file is kept, with its test renamed and its assertion
// direction unchanged, as a pinning regression test that the gap stays
// fixed -- res.IR.CalledFunctions must contain the real, mangled
// instantiation for a generic call site even when every argument is a
// compile-time literal.
const constFoldBypassSource = `
struct DemoState {
  touch: uint64 = 0
}

func pickBigger<T>(a: T, b: T): T {
  if (a > b) {
    return a
  }
  return b
}

@message(1)
struct Touch {
  nonce: uint64
}

type ExternalMsg = Touch

contract ConstFoldBypassDemo {
  storage: DemoState
  incomingExternal: ExternalMsg

  @external
  func onExternalMessage(inMsg: Segment) {
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func biggerU128(): uint128 {
    return pickBigger::<uint128>(500, 233)
  }
}
`

// TestGenericCallWithLiteralArgumentsInstantiatesThroughRealPath is the
// regression test pinning the fix described above: biggerU128's call to
// pickBigger::<uint128>(500, 233), with both arguments compile-time integer
// literals and a branch that resolves to a trailing `return` reachable from
// evalConstStatements, must now compile through the real generic-
// instantiation path (generics.go) like any other generic call site -- so
// "pickBigger$uint128" must appear in res.IR.CalledFunctions, and the
// original, un-mangled "pickBigger" declaration must never itself be
// compiled as a call target.
func TestGenericCallWithLiteralArgumentsInstantiatesThroughRealPath(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	res, err := c.Compile([]byte(constFoldBypassSource))
	require.NoError(t, err)

	counts := map[string]int{}
	for _, e := range res.IR.CalledFunctions {
		counts[e.Name]++
	}
	require.Equal(t, 1, counts["pickBigger$uint128"],
		"pickBigger::<uint128>(500, 233) must compile through the real generic-instantiation "+
			"path (generics.go) like any other generic call site, even though both arguments are "+
			"compile-time literals -- evalConstValue must bail out of constant-folding whenever "+
			"expr.TypeArgs is non-empty instead of resolving the callee by bare name")
	require.Equal(t, 0, counts["pickBigger"],
		"the bare, still-generic declaration must never itself be compiled as a call target")
}
