package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
)

// This file pins the design's explicit dedup contract (generics.go,
// instantiateGenericFunction's doc comment): "Cached by mangled name (a
// cache hit returns immediately, no re-validation, no re-charge against the
// instantiation budget) -- both for ordinary dedup (the same instantiation
// reached from multiple call sites) AND as a recursion guard." A generic
// call reached from TWO DIFFERENT call sites with the SAME closed type
// argument must resolve to and compile ONE shared instantiation, not
// duplicate it -- while two call sites with DIFFERENT type arguments must
// still produce two genuinely separate instantiations, so the contrast
// proves the dedup key is really (name, type-args), not just name.

// dedupSource declares clampLow<T>, a branchy (real CALL/RET) generic
// function, called from THREE getters: two with T=uint64 (from different
// argument values, so their results can't accidentally agree by coincidence
// alone) and one with T=uint128.
const dedupSource = `
struct DemoState {
  touch: uint64 = 0
}

// clampLow uses a var plus reassignment (not "if (cond) { return lo }
// return x") so a pre-existing, generics-unaware constant folder in the
// compiler cannot silently short-circuit a call using the still-generic
// body's untyped evaluation instead of the real per-type-argument
// instantiation this test exists to verify (see generics_const_fold_bypass_test.go).
@impure
func clampLow<T>(x: T, lo: T): T {
  var result = x
  if (x < lo) {
    result = lo
  }
  return result
}

@message(1)
struct Touch {
  nonce: uint64
}

type ExternalMsg = Touch

contract DedupDemo {
  storage: DemoState
  incomingExternal: ExternalMsg

  @external
  func onExternalMessage(inMsg: Segment) {
    const msg = lazy ExternalMsg.fromSegment(inMsg)
    match (msg) {
      Touch => {
        var st = lazy DemoState.load()
        st.touch += 1
        st.save()
      }
      else => {
        assert (inMsg.isEmpty()) throw 0xFFFF
      }
    }
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func clampBelowU64(): uint64 {
    return clampLow::<uint64>(3, 10)
  }

  @get
  func clampAboveU64(): uint64 {
    return clampLow::<uint64>(50, 10)
  }

  @get
  func clampU128(): uint128 {
    return clampLow::<uint128>(5, 2)
  }
}
`

// TestGenericSameTypeArgumentDedupedAcrossCallSites proves the design's
// dedup contract: clampBelowU64 and clampAboveU64 both instantiate
// clampLow<uint64> -- from two DIFFERENT call sites -- and must resolve to
// exactly ONE compiled call target, not two. clampU128 instantiates a
// genuinely DIFFERENT (name, type-args) pair and must remain its own,
// separate, second entry -- proving the single "clampLow$uint64" entry
// really is deduped by (name, type-args), not accidentally collapsed by
// name alone (which would be a correctness bug, not the documented dedup
// behavior).
func TestGenericSameTypeArgumentDedupedAcrossCallSites(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	res, err := c.Compile([]byte(dedupSource))
	require.NoError(t, err)
	require.NoError(t, res.Manifest.Validate())

	counts := map[string]int{}
	for _, e := range res.IR.CalledFunctions {
		counts[e.Name]++
	}
	require.Equal(t, 1, counts["clampLow$uint64"],
		"clampLow<uint64>, reached from TWO different call sites (clampBelowU64 and clampAboveU64), must be compiled exactly ONCE, not duplicated")
	require.Equal(t, 1, counts["clampLow$uint128"],
		"clampLow<uint128> is a genuinely distinct instantiation and must still be compiled as its own separate entry")
	require.Equal(t, 0, counts["clampLow"], "the bare, still-generic declaration must never be compiled as a call target")

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	empty := avm.Storage{}
	getU64 := func(getter string) uint64 {
		t.Helper()
		exec, err := runner.Run(res.Module, empty, avm.RuntimeContext{
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

	// Both call sites into the SHARED clampLow$uint64 instantiation must
	// still behave correctly and independently for their own arguments --
	// dedup must not mean "the second call site silently gets whatever the
	// first call site computed."
	require.Equal(t, uint64(10), getU64("clampBelowU64"), "clampLow(3, lo=10) -> 10 (clamped up)")
	require.Equal(t, uint64(50), getU64("clampAboveU64"), "clampLow(50, lo=10) -> 50 (already above lo)")
	require.Equal(t, uint64(5), getU64("clampU128"), "clampLow(5, lo=2) -> 5 (already above lo), uint128 instantiation")
}
