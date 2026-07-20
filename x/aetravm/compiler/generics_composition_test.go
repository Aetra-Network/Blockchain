package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
)

// This file covers AVM generics v1's composition with two other language
// features the design explicitly says it composes with:
//
//   - @resource abilities (resource_abilities.go): resolveCallReturnTypeName
//     is EXPLICITLY generics-aware ("delegates to resolveUserFunction...
//     for a generic call site... resolveUserFunction resolves through to
//     the already-instantiated, concrete clone"), so a generic function
//     called with a @resource struct as its type argument must have its
//     linear-use (exactly-once) rule enforced correctly THROUGH the
//     generic call boundary, both for the argument going in and the result
//     coming out.
//
//   - tuples (design doc §2 / §3.2's nested-TypeArgs obligation): a
//     generic function's parenthesized multi-value return type `-> (T, T)`
//     must substitute T inside the tuple's own Args correctly, and the
//     result must destructure correctly at each (possibly differently
//     typed) call site.

// ---------------------------------------------------------------------
// generics + @resource
// ---------------------------------------------------------------------

// resourceGenericSingleUseSource: passThrough<T> is a trivial generic
// identity function. Called with T=Token (a @resource struct) at the ONE
// call site, its result is bound to a new local (t2) and used exactly once
// -- the resource-linearity happy path, but flowing THROUGH a generic call
// boundary rather than a plain struct-literal/local-copy.
const resourceGenericSingleUseSource = `
@resource
struct Token {
  amount: uint64
}

struct DemoResState {
  redeemed: uint64 = 0
}

func passThrough<T>(x: T): T {
  return x
}

contract ResourceGenericDemo {
  storage: DemoResState
  incomingExternal: DemoResState

  @external
  func onExternalMessage(inMsg: Segment) {
    const t = Token{amount: 42}
    const t2 = passThrough::<Token>(t)
    set state.redeemed = t2.amount
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func getRedeemed(): uint64 {
    const st = DemoResState.load()
    return st.redeemed
  }
}
`

// TestGenericComposesWithResourceAbilitiesSingleUseCompilesAndExecutes proves
// the happy path end to end: a @resource value passed as a generic call's
// type argument, used exactly once going in (as the call argument) and
// exactly once coming out (t2.amount), is accepted by CheckResourceAbilities
// AND compiles/executes correctly through the real, unmodified pipeline --
// generics does not disable or weaken @resource linearity enforcement, and
// @resource does not block generics from compiling.
func TestGenericComposesWithResourceAbilitiesSingleUseCompilesAndExecutes(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	res, err := c.Compile([]byte(resourceGenericSingleUseSource))
	require.NoError(t, err)

	// The instantiated clone passThrough$Token must itself have been
	// resource-checked (instantiateGenericFunction's own
	// checkResourceBody call, generics.go) and found valid -- a param of
	// type Token used exactly once (`return x`) inside its own
	// substituted body.
	counts := map[string]int{}
	for _, e := range res.IR.CalledFunctions {
		counts[e.Name]++
	}
	// passThrough<T>'s body is a single return expression, so it is
	// inlined (like makePair<A,B> in the generics reference contract) --
	// confirming it does NOT need to appear as its own compiled call
	// target is itself part of proving @resource tracking works
	// independently of whether the callee is inlined or a real call.
	require.Zero(t, counts["passThrough$Token"], "a single-expression generic body must be inlined, not compiled as its own call target")

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)

	exec, err := runner.Run(res.Module, avm.Storage{}, avm.RuntimeContext{
		Entry:    avm.EntryReceiveExternal,
		Message:  async.MessageEnvelope{Opcode: 11, QueryID: 1, GasLimit: 200_000},
		GasLimit: 200_000,
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	require.Equal(t, uint64(42), avm.DecodeU64(exec.State["redeemed"]), "the resource's field must survive intact through the generic identity call")
}

// resourceGenericDoubleUseSource is the adversarial counterpart: t2, the
// result of a GENERIC call (passThrough::<Token>), is referenced TWICE --
// exactly the duplication a 'no copy' resource must reject, now flowing
// through resolveCallReturnTypeName's generics-aware call-result tracking
// rather than a plain local copy.
const resourceGenericDoubleUseSource = `
@resource
struct Token {
  amount: uint64
}

struct DoubleUseResState {
  a: uint64 = 0
  b: uint64 = 0
}

func passThrough<T>(x: T): T {
  return x
}

contract ResourceGenericDoubleUse {
  storage: DoubleUseResState
  incomingExternal: DoubleUseResState

  @external
  func onExternalMessage(inMsg: Segment) {
    const t = Token{amount: 42}
    const t2 = passThrough::<Token>(t)
    set state.a = t2.amount
    set state.b = t2.amount
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`

// TestGenericComposesWithResourceAbilitiesDoubleUseThroughGenericCallRejected
// proves the composition is not a blind spot: without generics-aware
// resolveCallReturnTypeName tracking, t2's static type would be unknown
// (resolveUserFunction previously had no generic-call-result case at all),
// so this double-use would silently slip through uncaught. With it wired in,
// the compile must fail with E_RESOURCE_DOUBLE_USE.
func TestGenericComposesWithResourceAbilitiesDoubleUseThroughGenericCallRejected(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	_, err = c.Compile([]byte(resourceGenericDoubleUseSource))
	require.Error(t, err)
	var abilityErr *ResourceAbilityError
	require.ErrorAs(t, err, &abilityErr)
	require.Equal(t, "E_RESOURCE_DOUBLE_USE", abilityErr.Code)
	require.Contains(t, abilityErr.Error(), "t2")
}

// ---------------------------------------------------------------------
// generics + tuples
// ---------------------------------------------------------------------

// tupleGenericSource declares splitOrSwap<T>, a branchy (real CALL/RET)
// generic function with a parenthesized multi-value return type `-> (T, T)`
// (design doc §2.6/§3.2): T must be substituted correctly INSIDE the
// tuple's own Args, for TWO distinct instantiations (uint64, uint128) in the
// same module, each destructured at its call site.
const tupleGenericSource = `
struct DemoState {
  touch: uint64 = 0
}

func splitOrSwap<T>(a: T, b: T, doSwap: bool) -> (T, T) {
  if (doSwap) {
    return (b, a)
  }
  return (a, b)
}

@message(1)
struct Touch {
  nonce: uint64
}

type ExternalMsg = Touch

contract TupleGenericDemo {
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
  func firstNoSwapU64(): uint64 {
    const (x, y) = splitOrSwap::<uint64>(11, 22, false)
    return x
  }

  @get
  func secondNoSwapU64(): uint64 {
    const (x, y) = splitOrSwap::<uint64>(11, 22, false)
    return y
  }

  @get
  func firstSwapU64(): uint64 {
    const (x, y) = splitOrSwap::<uint64>(11, 22, true)
    return x
  }

  @get
  func firstSwapU128(): uint128 {
    const (x, y) = splitOrSwap::<uint128>(1000, 2000, true)
    return x
  }
}
`

// TestGenericComposesWithTupleReturnType proves a generic function's
// parenthesized multi-value return type substitutes and lowers correctly:
// the tuple element types (both T) are substituted per instantiation, the
// real CALL/RET + OpMakeTuple + destructure machinery all still fire
// correctly for a monomorphized callee, and TWO distinct instantiations
// (uint64/uint128) coexist correctly. It also incidentally re-confirms the
// dedup property (generics_dedup_test.go) in a tuple-returning shape:
// firstNoSwapU64/secondNoSwapU64/firstSwapU64 all instantiate
// splitOrSwap<uint64> and must share ONE compiled call target.
func TestGenericComposesWithTupleReturnType(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	res, err := c.Compile([]byte(tupleGenericSource))
	require.NoError(t, err)
	require.NoError(t, res.Manifest.Validate())
	require.True(t, hasOpcode(res.Module.Code, avm.OpMakeTuple), "a (T, T) return value must construct a real tuple")

	counts := map[string]int{}
	for _, e := range res.IR.CalledFunctions {
		counts[e.Name]++
	}
	require.Equal(t, 1, counts["splitOrSwap$uint64"], "three call sites, same type argument -- one shared compiled instantiation")
	require.Equal(t, 1, counts["splitOrSwap$uint128"], "a distinct type argument -- its own separate instantiation")

	u64Entry := calledFunctionByName(t, res.IR.CalledFunctions, "splitOrSwap$uint64")
	u128Entry := calledFunctionByName(t, res.IR.CalledFunctions, "splitOrSwap$uint128")
	u64Slots := collectIREntrySlots(u64Entry)
	u128Slots := collectIREntrySlots(u128Entry)
	for slot := range u64Slots {
		require.False(t, u128Slots[slot], "splitOrSwap$uint64 and splitOrSwap$uint128 must not share slot %d", slot)
	}

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

	require.Equal(t, uint64(11), getU64("firstNoSwapU64"), "splitOrSwap(11,22,false) -> (11,22), first=11")
	require.Equal(t, uint64(22), getU64("secondNoSwapU64"), "splitOrSwap(11,22,false) -> (11,22), second=22")
	require.Equal(t, uint64(22), getU64("firstSwapU64"), "splitOrSwap(11,22,true) -> (22,11), first=22")
	require.Equal(t, uint64(2000), getU64("firstSwapU128"), "splitOrSwap<uint128>(1000,2000,true) -> (2000,1000), first=2000")
}
