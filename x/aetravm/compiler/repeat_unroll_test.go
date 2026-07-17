package compiler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// repeatContractSource wraps a repeat statement in the smallest contract that
// reaches the IR-lowering stage where constant `repeat` counts are unrolled.
func repeatContractSource(body string) string {
	return `
struct DemoState {
  count: u64 = 0
}

contract Demo {
  storage: DemoState
  incomingMessages: DemoState

  @internal
  func onInternalMessage(in: InMessage) {
    ` + body + `
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`
}

// compileWithinTimeout compiles src in a goroutine and reports whether it
// finished within d. It is used to detect the pre-fix unbounded compile-time
// loop (which never returns) without hanging the test binary.
func compileWithinTimeout(src string, d time.Duration) (err error, finished bool) {
	type outcome struct{ err error }
	done := make(chan outcome, 1)
	go func() {
		c, cerr := New(DefaultOptions())
		if cerr != nil {
			done <- outcome{cerr}
			return
		}
		_, e := c.Compile([]byte(src))
		done <- outcome{e}
	}()
	select {
	case o := <-done:
		return o.err, true
	case <-time.After(d):
		return nil, false
	}
}

// TestRepeatEmptyBodyHugeCountDoesNotHang is the regression test for the
// empty-body arm of the repeat unroll DoS: `repeat <max-uint64> {}` previously
// iterated the (empty) body count times -- an unbounded compile-time loop.
// After the fix an empty body short-circuits immediately, so the compile
// finishes essentially instantly. Before the fix this compile never returns
// within the timeout.
func TestRepeatEmptyBodyHugeCountDoesNotHang(t *testing.T) {
	src := repeatContractSource(`repeat 18446744073709551615 {}`)

	err, finished := compileWithinTimeout(src, 15*time.Second)
	require.True(t, finished,
		"FIX REGRESSION: repeat with an empty body must not iterate its (huge) constant count; the compile hung")
	require.NoError(t, err, "an empty-bodied repeat is a no-op and must compile cleanly")
}

// TestRepeatNonEmptyBodyOverBudgetRejectedUpFront is the regression test for the
// non-empty arm: a constant `repeat` whose unroll would exceed the code-size
// budget must be rejected up front (E_REPEAT_UNROLL) rather than fully
// materialized and only then rejected by the post-materialization size check.
// Before the fix the same source unrolls the whole body first and fails with a
// different error (E_CODE_SIZE) after doing all the work, so asserting the
// specific up-front error fails on the current code.
func TestRepeatNonEmptyBodyOverBudgetRejectedUpFront(t *testing.T) {
	// 600000 > 8*DefaultMaxCodeBytes (=524288), so even a one-instruction body
	// trips the up-front unroll guard.
	src := repeatContractSource(`repeat 600000 {
      set state.count = 1
    }`)

	err, finished := compileWithinTimeout(src, 60*time.Second)
	require.True(t, finished, "compile did not finish within the timeout")
	require.Error(t, err)
	// The up-front guard reports a distinctive "repeat unrolls past" message
	// (diagnostic code E_REPEAT_UNROLL). Before the fix the body is fully
	// unrolled and the compile instead fails later with E_CODE_SIZE
	// ("generated module exceeds code size limit"), so this assertion fails on
	// the current code.
	require.ErrorContains(t, err, "repeat unrolls past",
		"FIX REGRESSION: an over-budget constant repeat must be rejected up front, not fully unrolled first")
}
