package compiler

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
)

// requireCompileErrorCode asserts err is a *CompileError whose first
// diagnostic carries the given code -- CompileError.Error() deliberately
// renders only "<pos>: <message>" (compile.go), never the machine-readable
// Code, so asserting on err.Error()'s text would only pin the (more
// volatile) human-readable message, not the actual diagnostic identity.
func requireCompileErrorCode(t *testing.T, err error, code string) {
	t.Helper()
	require.Error(t, err)
	var compileErr *CompileError
	require.ErrorAs(t, err, &compileErr)
	require.NotEmpty(t, compileErr.Diagnostics)
	require.Equal(t, code, compileErr.Diagnostics[0].Code, "unexpected diagnostic: %s", compileErr.Error())
}

// TestGenericPairExampleCompilesAndExecutes compiles and executes the
// reference contract for AVM generics v1
// (examples/avm/collections/generic_pair.atlx) end to end through the real,
// unmodified Compiler.Compile() -> avm.NewRunner() pipeline. It proves:
//
//   - the generic struct Pair<A,B> and the generic functions maxOf<T>/
//     makePair<A,B>/largestPair<T> compile at all;
//   - TWO DISTINCT instantiations of the same generic declarations
//     (T=uint64 and T=uint128) are compiled into the SAME module as
//     separate call targets, each claiming its own disjoint local-slot
//     range -- verified both structurally (the mangled names below) and
//     behaviorally (the uint64 and uint128 getters return the numerically
//     correct, non-cross-contaminated results); and
//   - the monomorphized output is semantically correct, not merely
//     "compiles without error".
func TestGenericPairExampleCompilesAndExecutes(t *testing.T) {
	src, err := os.ReadFile("../../../examples/avm/collections/generic_pair.atlx")
	require.NoError(t, err, "read generic_pair.atlx")

	c, err := New(DefaultOptions())
	require.NoError(t, err)
	res, err := c.Compile(src)
	require.NoError(t, err, "compile generic_pair.atlx")
	require.NoError(t, res.Manifest.Validate())

	// Structural proof of monomorphization (design doc §3.1's mangling
	// scheme, "name$T1$T2..."): maxOf<T> and largestPair<T> both branch
	// (an if/return), so tryInlineUserFunctionCall cannot represent either
	// one -- every call to them, for EVERY instantiation, must go through
	// the real CALL/RET mechanism and appear in CalledFunctions.
	// makePair<A,B>'s body IS a single return expression, so it is
	// INLINED (spliced directly into its caller) for every instantiation
	// and must NEVER appear here -- proving generics compose correctly
	// with BOTH existing call-lowering paths, not just the real-call one.
	compiledNames := map[string]bool{}
	for _, entry := range res.IR.CalledFunctions {
		compiledNames[entry.Name] = true
	}
	for _, want := range []string{"maxOf$uint64", "maxOf$uint128", "largestPair$uint64", "largestPair$uint128"} {
		require.True(t, compiledNames[want], "expected compiled call target %q, got %v", want, compiledNames)
	}
	for name := range compiledNames {
		require.NotContains(t, name, "makePair", "makePair<A,B> is trivially inlinable and must never be compiled as a real call target")
	}

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)

	getU64 := func(state avm.Storage, getter string) uint64 {
		t.Helper()
		exec, err := runner.Run(res.Module, state, avm.RuntimeContext{
			Entry:    avm.EntryQuery,
			Message:  async.MessageEnvelope{Opcode: getterSelector(t, res, getter), GasLimit: 200_000},
			GasLimit: 200_000,
		})
		require.NoError(t, err, "run getter %s", getter)
		require.Equal(t, async.ResultOK, exec.ResultCode, "getter %s result code", getter)
		v, err := exec.ReturnValue.AsUint64()
		require.NoError(t, err, "decode getter %s return value", getter)
		return v
	}

	empty := avm.Storage{}

	// maxOf<uint64>(5, 12) = 12, called standalone (no Pair involved).
	require.Equal(t, uint64(12), getU64(empty, "maxU64"))

	// largestPair<uint64>(3, 9, 7, 2):
	//   m1 = maxOf(3, 9) = 9
	//   m2 = maxOf(7, 2) = 7
	//   Pair{ first: 9, second: 7 }
	require.Equal(t, uint64(9), getU64(empty, "largestFirstU64"), "largestPair<uint64>.first")
	require.Equal(t, uint64(7), getU64(empty, "largestSecondU64"), "largestPair<uint64>.second")

	// largestPair<uint128>(100, 250, 999, 500), a DISTINCT instantiation
	// compiled alongside the uint64 one above:
	//   m1 = maxOf(100, 250) = 250
	//   m2 = maxOf(999, 500) = 999
	//   Pair{ first: 250, second: 999 }
	// If the uint64 and uint128 instantiations' compiled locals ever
	// aliased (the sibling-instantiation analogue of the slot-overlap bug
	// call_slot_allocator_test.go pins for the plain call-stack mechanism),
	// these would come back wrong -- either corrupted by the uint64
	// instantiation's own still-live intermediates, or vice versa.
	require.Equal(t, uint64(250), getU64(empty, "largestFirstU128"), "largestPair<uint128>.first")
	require.Equal(t, uint64(999), getU64(empty, "largestSecondU128"), "largestPair<uint128>.second")

	// The ordinary (non-generic) part of the contract -- the @external
	// handler and its storage write -- must be entirely unaffected by any
	// of the above: generics changes zero bytes of a plain handler's own
	// lowering.
	touchBody, err := res.MessageBodies["Touch"].Encode(map[string]any{"nonce": uint64(1)})
	require.NoError(t, err)
	exec, err := runner.Run(res.Module, empty, avm.RuntimeContext{
		Entry:           avm.EntryReceiveExternal,
		ContractAddress: nil,
		GasLimit:        1_000_000,
		Message: async.MessageEnvelope{
			Opcode:   res.MessageBodyOpcodes["Touch"],
			QueryID:  1,
			Body:     touchBody,
			GasLimit: 1_000_000,
		},
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	require.Equal(t, uint64(1), getU64(exec.State, "touchCount"))
}

// TestGenericTurbofishDoesNotBreakChainedComparison is the regression test
// for the exact grammar-ambiguity hole a bare `f<T>(...)` design would have
// reopened (design doc §1.1, reviewer 1's blocking finding): `a < b > (x,
// y)`, the legal chained-comparison interpretation, must keep parsing
// EXACTLY as it always has -- as ((a < b) > (x, y)), a comparison of a
// comparison against a tuple literal -- with zero interaction with the new
// '::<...>' turbofish path. '::' was never a valid token sequence before
// this design (no two-character lookahead existed on ':'), so there is no
// shared grammar surface for a bare '<' to be ambiguous about: parseComparison
// (parser.go) never even looks at a tokenColonColon.
//
// This checks the PARSED AST SHAPE directly (ParseSource), not a full
// Compile(): `a < b > (x, y)` is deliberately semantically meaningless (no
// operator here validates its operand types -- ExprCompare's own
// inferExprType case always yields bool regardless of operand shape, and a
// bare function name used as a VALUE, rather than called, was never
// lowerable even before this design existed), so asserting on Compile()
// succeeding would conflate "does this misparse as a generic call" with
// "is this program semantically well-formed", which are unrelated
// questions -- only the former is what this design could possibly regress.
func TestGenericTurbofishDoesNotBreakChainedComparison(t *testing.T) {
	const src = `
func check(): bool {
  const a = 1
  const b = 2
  const x = 3
  const y = 4
  return a < b > (x, y)
}
`
	file, err := ParseSource(src)
	require.NoError(t, err)
	require.Len(t, file.Functions, 1)
	body := file.Functions[0].Body
	require.Len(t, body, 5)
	ret := body[4]
	require.Equal(t, StatementReturn, ret.Kind)

	// Outermost: (a < b) > (x, y) -- an ExprCompare whose Op is ">" and
	// whose Left is itself an ExprCompare ("<"), never a generic ExprCall
	// (which would require expr.TypeArgs to be set, and can only ever be
	// produced by a literal '::<' token sequence -- absent here entirely).
	require.Equal(t, ExprCompare, ret.Value.Kind)
	require.Equal(t, ">", ret.Value.Op)
	require.Nil(t, ret.Value.TypeArgs)
	require.NotNil(t, ret.Value.Left)
	require.Equal(t, ExprCompare, ret.Value.Left.Kind)
	require.Equal(t, "<", ret.Value.Left.Op)
	require.NotNil(t, ret.Value.Left.Left)
	require.Equal(t, ExprIdent, ret.Value.Left.Left.Kind)
	require.Equal(t, "a", ret.Value.Left.Left.Text)
	require.NotNil(t, ret.Value.Left.Right)
	require.Equal(t, ExprIdent, ret.Value.Left.Right.Kind)
	require.Equal(t, "b", ret.Value.Left.Right.Text)

	// The right-hand side of '>' is the tuple literal (x, y) -- a plain
	// ExprTupleLiteral, not a struct literal or a generic call's argument
	// list.
	require.NotNil(t, ret.Value.Right)
	require.Equal(t, ExprTupleLiteral, ret.Value.Right.Kind)
	require.Len(t, ret.Value.Right.Args, 2)
}

// TestGenericHandlerRejected pins design doc §1.4's E_GENERIC_HANDLER check
// -- the SOLE protection against a generic @external/@internal/@bounced
// handler (there is no second, redundant type-level barrier in this
// parser).
func TestGenericHandlerRejected(t *testing.T) {
	const src = `
struct DemoState {
  flag: bool = false
}

@message(1)
struct Touch {}

type ExternalMsg = Touch

contract GenericHandlerDemo {
  storage: DemoState
  incomingExternal: ExternalMsg

  @external
  func onExternalMessage<T>(inMsg: Segment) {
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	_, err = c.Compile([]byte(src))
	requireCompileErrorCode(t, err, "E_GENERIC_HANDLER")
}

// TestGenericGetterRejected pins this implementation's deliberate extension
// of E_GENERIC_HANDLER to @get getters: an externally dispatched selector
// must resolve to exactly one concrete signature, and there is no
// compile-time type argument available at an external call site to pick an
// instantiation with -- the identical structural problem a handler has.
func TestGenericGetterRejected(t *testing.T) {
	const src = `
struct DemoState {
  flag: bool = false
}

@message(1)
struct Touch {}

type ExternalMsg = Touch

contract GenericGetterDemo {
  storage: DemoState
  incomingExternal: ExternalMsg

  @external
  func onExternalMessage(inMsg: Segment) {
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func identity<T>(x: T): T {
    return x
  }
}
`
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	_, err = c.Compile([]byte(src))
	requireCompileErrorCode(t, err, "E_GENERIC_HANDLER")
}

// TestGenericFunctionRequiresExplicitReturnType pins design doc
// §1.1/§1.2's E_GENERIC_RETURN_TYPE_REQUIRED check: a generic function
// without an explicit return type must be rejected cleanly by
// validateFunction, not fall through to inferMissingReturnTypes attempting
// (and either mis-succeeding or confusingly failing) return-type inference
// over a body whose expressions are typed in terms of unbound type
// parameters.
func TestGenericFunctionRequiresExplicitReturnType(t *testing.T) {
	const src = `
struct DemoState {
  flag: bool = false
}

@message(1)
struct Touch {}

type ExternalMsg = Touch

func identity<T>(x: T) {
  return x
}

contract GenericNoReturnTypeDemo {
  storage: DemoState
  incomingExternal: ExternalMsg

  @external
  func onExternalMessage(inMsg: Segment) {
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	_, err = c.Compile([]byte(src))
	requireCompileErrorCode(t, err, "E_GENERIC_RETURN_TYPE_REQUIRED")
}

// TestGenericInstantiationBudgetExceeded pins design doc §1.6's hard,
// enforced, module-wide instantiation cap: a chain of generic functions,
// each calling the previous one twice with two syntactically distinct
// composed type arguments (Wrap<T> vs Pair<T,T>), can reach exponentially
// many distinct instantiations from linearly many lines of source despite
// the bare-name call graph being acyclic -- this must fail with
// E_GENERIC_INSTANTIATION_BUDGET, not hang or exhaust memory.
func TestGenericInstantiationBudgetExceeded(t *testing.T) {
	var b strings.Builder
	b.WriteString(`
struct DemoState {
  flag: bool = false
}

@message(1)
struct Touch {}

type ExternalMsg = Touch

struct Wrap<T> {
  value: T
}

struct Pair<T> {
  first: T
  second: T
}

func g0<T>(x: T): T {
  return x
}
`)
	// Each g_i<T> calls g_{i-1} twice, with two distinct composed type
	// arguments -- Wrap<T> and Pair<T> -- so the instantiation COUNT can
	// double at every level even though the bare-name call graph
	// (g_i -> g_{i-1}) is a simple acyclic chain.
	const chainLength = 12 // 2^12 = 4096 >> maxGenericInstantiations (128)
	for i := 1; i <= chainLength; i++ {
		fmt.Fprintf(&b, `
func g%d<T>(x: T): T {
  const a = g%d::<Wrap<T>>(Wrap::<T>{ value: x })
  const c = g%d::<Pair<T>>(Pair::<T>{ first: x, second: x })
  return x
}
`, i, i-1, i-1)
	}
	fmt.Fprintf(&b, `
contract GenericBudgetDemo {
  storage: DemoState
  incomingExternal: ExternalMsg

  @external
  func onExternalMessage(inMsg: Segment) {
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func run(): uint64 {
    const r = g%d::<uint64>(1)
    return r
  }
}
`, chainLength)

	c, err := New(DefaultOptions())
	require.NoError(t, err)
	_, err = c.Compile([]byte(b.String()))
	requireCompileErrorCode(t, err, "E_GENERIC_INSTANTIATION_BUDGET")
}
