package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
)

// resourceValidSource declares a @resource struct (Voucher) and consumes it
// exactly once (a single field read folded into a `set state.redeemed =
// v.amount`). This is the "happy path": it must both (a) satisfy
// CheckResourceAbilities' linear-use rule and (b) compile and execute
// end-to-end through the real, UNMODIFIED Compiler.Compile() ->
// avm.NewRunner() pipeline exactly like any other struct -- @resource is
// annotation-only and changes zero emitted bytecode.
const resourceValidSource = `
@resource
struct Voucher {
  amount: uint64
}

struct DemoResState {
  redeemed: uint64 = 0
}

contract ResourceDemo {
  storage: DemoResState
  incomingExternal: DemoResState

  @external
  func onExternalMessage(inMsg: Segment) {
    const v = Voucher{amount: 42}
    set state.redeemed = v.amount
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

// TestResourceAbilityValidSingleUseCompilesAndExecutes proves the tractable
// item end to end: a @resource-annotated struct moved exactly once (a) is
// accepted by CheckResourceAbilities and (b) compiles and actually executes
// through the real VM, producing the expected state -- i.e. @resource is
// non-disruptive to the existing, unmodified compile/execute pipeline.
func TestResourceAbilityValidSingleUseCompilesAndExecutes(t *testing.T) {
	src, err := ParseSource(resourceValidSource)
	require.NoError(t, err)
	require.NoError(t, CheckResourceAbilities(src), "a resource used exactly once must be accepted")

	c, err := New(DefaultOptions())
	require.NoError(t, err)
	res, err := c.Compile([]byte(resourceValidSource))
	require.NoError(t, err)

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)

	exec, err := runner.Run(res.Module, avm.Storage{}, avm.RuntimeContext{
		Entry:    avm.EntryReceiveExternal,
		Message:  async.MessageEnvelope{Opcode: 11, QueryID: 1, GasLimit: 200_000},
		GasLimit: 200_000,
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	require.Equal(t, uint64(42), avm.DecodeU64(exec.State["redeemed"]))

	getExec, err := runner.Run(res.Module, exec.State, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		Message:  async.MessageEnvelope{Opcode: getterSelector(t, res, "getRedeemed"), QueryID: 2, GasLimit: 200_000},
		GasLimit: 200_000,
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, getExec.ResultCode)
	got, err := getExec.ReturnValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(42), got)
}

// resourceDoubleUseSource references the same @resource local twice (into
// two different storage fields) inside the same function body -- exactly
// the duplication a 'no copy' resource must reject, since every struct read
// in AVM v1 is a value copy (see structFieldLocalsSource / commit 1165cf4f).
const resourceDoubleUseSource = `
@resource
struct Voucher {
  amount: uint64
}

struct DoubleUseState {
  a: uint64 = 0
  b: uint64 = 0
}

contract ResourceDoubleUse {
  storage: DoubleUseState
  incomingExternal: DoubleUseState

  @external
  func onExternalMessage(inMsg: Segment) {
    const v = Voucher{amount: 42}
    set state.a = v.amount
    set state.b = v.amount
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`

func TestResourceAbilityRejectsDoubleUse(t *testing.T) {
	src, err := ParseSource(resourceDoubleUseSource)
	require.NoError(t, err)

	err = CheckResourceAbilities(src)
	require.Error(t, err)
	var abilityErr *ResourceAbilityError
	require.ErrorAs(t, err, &abilityErr)
	require.Equal(t, "E_RESOURCE_DOUBLE_USE", abilityErr.Code)
	require.Contains(t, abilityErr.Error(), "v")
}

// resourceUnusedSource binds a @resource local and never references it --
// exactly the silent drop a 'no drop' resource must reject.
const resourceUnusedSource = `
@resource
struct Voucher {
  amount: uint64
}

struct UnusedState {
  a: uint64 = 0
}

contract ResourceUnused {
  storage: UnusedState
  incomingExternal: UnusedState

  @external
  func onExternalMessage(inMsg: Segment) {
    const v = Voucher{amount: 42}
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`

func TestResourceAbilityRejectsUnused(t *testing.T) {
	src, err := ParseSource(resourceUnusedSource)
	require.NoError(t, err)

	err = CheckResourceAbilities(src)
	require.Error(t, err)
	var abilityErr *ResourceAbilityError
	require.ErrorAs(t, err, &abilityErr)
	require.Equal(t, "E_RESOURCE_UNUSED", abilityErr.Code)
}

// resourceParamDoubleUseSource exercises the parameter path (not just
// locals): a @resource-typed getter parameter referenced twice.
const resourceParamDoubleUseSource = `
@resource
struct Voucher {
  amount: uint64
}

struct ParamState {
  a: uint64 = 0
}

contract ResourceParamDemo {
  storage: ParamState
  incomingExternal: ParamState

  @external
  func onExternalMessage(inMsg: Segment) {
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func sumTwice(v: Voucher): uint64 {
    return v.amount + v.amount
  }
}
`

func TestResourceAbilityRejectsDoubleUseOnParameter(t *testing.T) {
	src, err := ParseSource(resourceParamDoubleUseSource)
	require.NoError(t, err)

	err = CheckResourceAbilities(src)
	require.Error(t, err)
	var abilityErr *ResourceAbilityError
	require.ErrorAs(t, err, &abilityErr)
	require.Equal(t, "E_RESOURCE_DOUBLE_USE", abilityErr.Code)
}

// TestResourceAbilityWiredIntoCompilePipeline proves CheckResourceAbilities
// now runs automatically as part of Compiler.Compile()/CompileFiles() -- not
// just when called standalone. It deliberately does NOT call
// CheckResourceAbilities itself; it feeds the same double-use source
// straight into the real, unmodified c.Compile() entry point and asserts the
// compile fails with E_RESOURCE_DOUBLE_USE.
func TestResourceAbilityWiredIntoCompilePipeline(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)

	_, err = c.Compile([]byte(resourceDoubleUseSource))
	require.Error(t, err)
	var abilityErr *ResourceAbilityError
	require.ErrorAs(t, err, &abilityErr)
	require.Equal(t, "E_RESOURCE_DOUBLE_USE", abilityErr.Code)
}

// TestResourceAbilityUnusedWiredIntoCompilePipeline is the E_RESOURCE_UNUSED
// counterpart to TestResourceAbilityWiredIntoCompilePipeline: it feeds the
// unused-resource source straight into c.Compile() (again, without calling
// CheckResourceAbilities standalone) and asserts the real compile pipeline
// rejects it.
func TestResourceAbilityUnusedWiredIntoCompilePipeline(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)

	_, err = c.Compile([]byte(resourceUnusedSource))
	require.Error(t, err)
	var abilityErr *ResourceAbilityError
	require.ErrorAs(t, err, &abilityErr)
	require.Equal(t, "E_RESOURCE_UNUSED", abilityErr.Code)
}

// TestResourceAnnotationRejectsCombinationWithStorageOrMessage documents
// (and pins) the pre-existing "only one annotation per declaration" parser
// rule (parser.go parseAnnotationList) as it applies to @resource: a struct
// cannot be both @resource and @storage/@message. This is not a limitation
// introduced by this change -- it is inherited from the existing
// single-annotation-per-declaration constraint that already governs every
// other struct annotation -- but is pinned here so it is a deliberate,
// documented fact rather than an unnoticed surprise.
func TestResourceAnnotationRejectsCombinationWithOtherAnnotation(t *testing.T) {
	const src = `
@resource
@storage
struct BadVoucher {
  amount: uint64
}
`
	_, err := ParseSource(src)
	require.Error(t, err)
	require.Contains(t, err.Error(), "only one annotation is allowed per declaration")
}
