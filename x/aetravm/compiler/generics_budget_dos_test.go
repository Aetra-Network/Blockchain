package compiler

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// This file complements generics_test.go's TestGenericInstantiationBudgetExceeded
// (which already pins that an over-limit generic fanout is REJECTED with the
// correct E_GENERIC_INSTANTIATION_BUDGET diagnostic) with the two properties
// that test does not itself make explicit: the rejection must happen FAST
// (bounded time, not a hang) and it must happen CLEANLY (a returned error,
// not a panic that would crash the whole compiler/process). A plain
// `require.Error` assertion cannot distinguish "failed correctly in 3ms"
// from "would have failed correctly in 3 hours" or "failed via an unrecovered
// panic that happens to still make the test binary exit non-zero" -- this
// file makes both of those failure modes explicit, first-class assertions.

// buildExponentialFanoutSource generates the same doubling-fanout shape
// TestGenericInstantiationBudgetExceeded uses (design doc §1.6): a chain of
// chainLength generic functions, each calling the previous one twice with
// two syntactically distinct composed type arguments (Wrap<T> and Pair<T>),
// so the number of DISTINCT (name, type-args) pairs the worklist could reach
// doubles at every level even though the bare-name call graph
// (g_i -> g_{i-1}) is a simple, non-cyclic chain.
func buildExponentialFanoutSource(chainLength int) string {
	var b strings.Builder
	b.WriteString(`
struct DemoState {
  flag: bool = false
}

@message(1)
struct Touch {
  nonce: uint64
}

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
contract GenericBudgetDosDemo {
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
	return b.String()
}

// compileResult carries a compile attempt's outcome (or panic) back across
// the goroutine boundary in TestGenericInstantiationBudgetDoesNotHangOrPanic.
type compileResult struct {
	err        error
	panicValue any
	elapsed    time.Duration
}

// TestGenericInstantiationBudgetDoesNotHangOrPanic is the adversarial DoS
// test this task asked for: a fanout chain deep enough that, WITHOUT the
// module-wide budget cap (maxGenericInstantiations, generics.go), the
// worklist would need to discover on the order of 2^40 distinct (name,
// type-args) pairs -- a number no compiler could ever finish enumerating,
// let alone validate/lower. This proves three things about the REAL cap,
// together, in one test:
//
//  1. the compile finishes at all, well within a generous deadline (not a
//     hang) -- run on a background goroutine with a time.After race so a
//     genuine hang fails the test instead of blocking the test binary
//     forever;
//  2. it never panics (a `recover()` inside that same goroutine catches and
//     reports one explicitly, rather than letting an unrecovered goroutine
//     panic crash the entire `go test` process, which would make the
//     failure far harder to diagnose than a clean test failure);
//  3. having finished, it fails with the exact, clear, documented
//     E_GENERIC_INSTANTIATION_BUDGET diagnostic -- not a generic parse
//     error, not a silent success, not a stack overflow.
func TestGenericInstantiationBudgetDoesNotHangOrPanic(t *testing.T) {
	// chainLength=40 -> a theoretical 2^40 (~1.1 trillion) distinct
	// instantiations if nothing bounded the worklist -- deliberately far
	// beyond what even an unbounded-but-not-hanging compiler could ever
	// enumerate, to make the point sharply: the ONLY way this test can pass
	// is if the budget check aborts discovery immediately upon crossing
	// maxGenericInstantiations (128), not after building/expanding the rest
	// of the (astronomically large) worklist first.
	const chainLength = 40
	src := []byte(buildExponentialFanoutSource(chainLength))

	done := make(chan compileResult, 1)
	go func() {
		start := time.Now()
		var result compileResult
		defer func() {
			if r := recover(); r != nil {
				result.panicValue = r
			}
			result.elapsed = time.Since(start)
			done <- result
		}()
		c, err := New(DefaultOptions())
		if err != nil {
			result.err = err
			return
		}
		_, result.err = c.Compile(src)
	}()

	select {
	case result := <-done:
		require.Nil(t, result.panicValue,
			"compiling an exponential-fanout generic chain must never panic, got: %v", result.panicValue)
		t.Logf("compile of a 2^%d-shaped generic fanout finished in %s", chainLength, result.elapsed)
		requireCompileErrorCode(t, result.err, "E_GENERIC_INSTANTIATION_BUDGET")
		require.Contains(t, result.err.Error(), "128",
			"the budget error message should name the actual configured limit for a clear diagnostic")
	case <-time.After(10 * time.Second):
		t.Fatal("compiling an over-budget generic fanout hung past a generous 10s deadline instead of failing fast -- the instantiation budget check is not aborting discovery early")
	}
}

// TestGenericInstantiationBudgetErrorIsActionable is a lighter-weight
// companion: it re-uses the exact chainLength=12 shape
// TestGenericInstantiationBudgetExceeded (generics_test.go) already proves
// gets rejected, and additionally asserts the diagnostic text itself is
// specific and actionable (names the limit, names what was being
// discovered) rather than a bare, uninformative error code -- "a clear
// error", per this task's own wording, is a statement about the message a
// contract author actually sees, not just the machine-readable Code.
func TestGenericInstantiationBudgetErrorIsActionable(t *testing.T) {
	src := []byte(buildExponentialFanoutSource(12))

	c, err := New(DefaultOptions())
	require.NoError(t, err)
	_, err = c.Compile(src)
	requireCompileErrorCode(t, err, "E_GENERIC_INSTANTIATION_BUDGET")

	var compileErr *CompileError
	require.ErrorAs(t, err, &compileErr)
	require.NotEmpty(t, compileErr.Diagnostics)
	msg := compileErr.Diagnostics[0].Message
	require.Contains(t, msg, "128", "message should name the configured budget")
	require.Contains(t, strings.ToLower(msg), "instantiation", "message should explain what budget was exhausted")
}
