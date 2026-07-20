package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
)

// threeTypeArgSource declares a single branchy (real CALL/RET, not
// inlinable) generic function, maxT<T>, instantiated with THREE distinct
// type arguments (uint64, uint128, uint256) in the same module -- broader
// than the 2-type-argument minimum this task asked for, specifically to
// confirm the monomorphization pass genuinely scales past exactly two
// sibling instantiations and that a THIRD, differently-widthed instantiation
// doesn't collide with either of the first two.
const threeTypeArgSource = `
struct DemoState {
  touch: uint64 = 0
}

// maxT uses a var plus reassignment (not "if (cond) { return a } return b")
// so a pre-existing, generics-unaware constant folder in the compiler
// cannot silently short-circuit these calls with the still-generic body's
// untyped evaluation instead of the real per-type-argument instantiation
// (see generics_const_fold_bypass_test.go for a dedicated regression test on that
// underlying gap).
@impure
func maxT<T>(a: T, b: T): T {
  var result = a
  if (b > a) {
    result = b
  }
  return result
}

@message(1)
struct Touch {
  nonce: uint64
}

type ExternalMsg = Touch

contract ThreeTypeArgDemo {
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
  func max64(): uint64 {
    return maxT::<uint64>(11, 47)
  }

  @get
  func max128(): uint128 {
    return maxT::<uint128>(500, 233)
  }

  @get
  func max256(): uint256 {
    return maxT::<uint256>(9000, 8999)
  }
}
`

// TestGenericBasicInstantiationThreeDistinctTypeArguments is the base case
// this task asked for: 2+ (here, 3) distinct type arguments of the SAME
// generic declaration must each produce their own genuinely distinct,
// correctly monomorphized compiled call target -- not be merged into one,
// not silently ignore the type argument, and not corrupt one another.
func TestGenericBasicInstantiationThreeDistinctTypeArguments(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	res, err := c.Compile([]byte(threeTypeArgSource))
	require.NoError(t, err)
	require.NoError(t, res.Manifest.Validate())

	// Structural proof: three distinct mangled call targets, the generic
	// (unmangled) declaration itself never compiled directly.
	counts := map[string]int{}
	for _, e := range res.IR.CalledFunctions {
		counts[e.Name]++
	}
	require.Equal(t, 1, counts["maxT$uint64"], "maxT<uint64> must be compiled exactly once")
	require.Equal(t, 1, counts["maxT$uint128"], "maxT<uint128> must be compiled exactly once")
	require.Equal(t, 1, counts["maxT$uint256"], "maxT<uint256> must be compiled exactly once")
	require.Equal(t, 0, counts["maxT"], "the bare, still-generic declaration must never itself be compiled as a call target")

	// The three instantiations must be pairwise slot-disjoint too (see
	// generics_slot_disjointness_test.go for the dedicated version of this
	// property; re-checked here across three siblings, not just two, since
	// nothing about the allocator's correctness is inherently limited to
	// pairs).
	entries := map[string]map[uint32]bool{
		"maxT$uint64":  collectIREntrySlots(calledFunctionByName(t, res.IR.CalledFunctions, "maxT$uint64")),
		"maxT$uint128": collectIREntrySlots(calledFunctionByName(t, res.IR.CalledFunctions, "maxT$uint128")),
		"maxT$uint256": collectIREntrySlots(calledFunctionByName(t, res.IR.CalledFunctions, "maxT$uint256")),
	}
	names := []string{"maxT$uint64", "maxT$uint128", "maxT$uint256"}
	for i, ni := range names {
		require.NotEmpty(t, entries[ni], "%s must claim at least one slot", ni)
		for j, nj := range names {
			if i == j {
				continue
			}
			for slot := range entries[ni] {
				require.False(t, entries[nj][slot], "%s and %s must not share slot %d", ni, nj, slot)
			}
		}
	}

	// Behavioral proof: each instantiation, run through the real interpreter,
	// must produce the numerically correct result for ITS OWN arguments --
	// not the other instantiations' values, and not a type-erased/generic
	// fallback.
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

	require.Equal(t, uint64(47), getU64("max64"), "maxT<uint64>(11,47)")
	require.Equal(t, uint64(500), getU64("max128"), "maxT<uint128>(500,233)")
	require.Equal(t, uint64(9000), getU64("max256"), "maxT<uint256>(9000,8999)")

	// The ordinary (non-generic) part of the contract must be entirely
	// unaffected.
	touchBody, err := res.MessageBodies["Touch"].Encode(map[string]any{"nonce": uint64(1)})
	require.NoError(t, err)
	exec, err := runner.Run(res.Module, empty, avm.RuntimeContext{
		Entry:    avm.EntryReceiveExternal,
		GasLimit: 1_000_000,
		Message: async.MessageEnvelope{
			Opcode:   res.MessageBodyOpcodes["Touch"],
			QueryID:  1,
			Body:     touchBody,
			GasLimit: 1_000_000,
		},
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
}
