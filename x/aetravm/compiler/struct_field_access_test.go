package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
)

// structFieldLocalsSource exercises local struct-literal field read, field
// write (mutate a `var`-bound struct then read back), and copy-on-assign
// semantics (`var copyOfOriginal = original` must not alias `original`) —
// all going through the same runtime mechanism (OpMapEmpty/OpMapSet/OpReadField
// on a value-struct map), gated purely by lowering-time type propagation.
const structFieldLocalsSource = `
struct Bp {
  bps: uint64
}

struct DemoState {
  readOnlyField: uint64 = 0
  mutatedField: uint64 = 0
  originalField: uint64 = 0
  copyField: uint64 = 0
}

contract StructFieldsDemo {
  storage: DemoState
  incomingExternal: DemoState

  @external
  func onExternalMessage(inMsg: Segment) {
    const readOnly = Bp{bps: 30}
    set state.readOnlyField = readOnly.bps

    var mutated = Bp{bps: 1}
    mutated.bps = 40
    set state.mutatedField = mutated.bps

    var original = Bp{bps: 5}
    var copyOfOriginal = original
    copyOfOriginal.bps = 99
    set state.originalField = original.bps
    set state.copyField = copyOfOriginal.bps
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func getReadOnlyField(): uint64 {
    const st = DemoState.load()
    return st.readOnlyField
  }

  @get
  func getMutatedField(): uint64 {
    const st = DemoState.load()
    return st.mutatedField
  }

  @get
  func getOriginalField(): uint64 {
    const st = DemoState.load()
    return st.originalField
  }

  @get
  func getCopyField(): uint64 {
    const st = DemoState.load()
    return st.copyField
  }
}
`

// TestLocalStructLiteralFieldReadWriteAndCopySemantics is the regression test
// for the compile.go fix that propagates a local's lowering-time type when its
// RHS is a struct literal (`Bp{bps: 30}`, ExprStruct) or a copy of an existing
// local/param (`var c = b`, ExprIdent). Before the fix, `env.types[name]`
// never got set for either shape, so isFieldLikeType("") rejected any field
// access on such a local with "local field access is not supported in AVM
// v1" even though OpReadField/OpMapSet already had full runtime support.
func TestLocalStructLiteralFieldReadWriteAndCopySemantics(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)

	res, err := c.Compile([]byte(structFieldLocalsSource))
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

	// Local struct literal field READ.
	require.Equal(t, uint64(30), avm.DecodeU64(exec.State["readOnlyField"]))
	// Local struct field WRITE (mutate a `var`-bound struct, then read back).
	require.Equal(t, uint64(40), avm.DecodeU64(exec.State["mutatedField"]))
	// Copy-on-assign must NOT alias: mutating the copy leaves the original
	// unchanged. runtimeMapSet (avm/value.go) always clones into a brand new
	// entries slice rather than mutating a shared map, so this is expected to
	// hold, not just hoped for.
	require.Equal(t, uint64(5), avm.DecodeU64(exec.State["originalField"]), "original must be unchanged by mutating its copy")
	require.Equal(t, uint64(99), avm.DecodeU64(exec.State["copyField"]), "the copy itself must reflect the mutation")

	for _, tc := range []struct {
		getter string
		want   uint64
	}{
		{"getReadOnlyField", 30},
		{"getMutatedField", 40},
		{"getOriginalField", 5},
		{"getCopyField", 99},
	} {
		getExec, err := runner.Run(res.Module, exec.State, avm.RuntimeContext{
			Entry:    avm.EntryQuery,
			Message:  async.MessageEnvelope{Opcode: getterSelector(t, res, tc.getter), QueryID: 2, GasLimit: 200_000},
			GasLimit: 200_000,
		})
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, getExec.ResultCode)
		got, err := getExec.ReturnValue.AsUint64()
		require.NoError(t, err)
		require.Equalf(t, tc.want, got, "getter %s", tc.getter)
	}
}

// findAdjacentInstructionPair reports whether the code stream contains `first`
// immediately followed by `second` (matching both Op and Data, when Data is
// non-empty) anywhere in the module. IRExprField always emits its receiver
// expression first and its own OpReadField second (see emitIRExpr's
// IRExprField case), so this precisely distinguishes "field read off the
// declared parameter's own arg slot" from "field read off the bare top-level
// message" -- the two lowerings a struct-typed parameter could produce.
func findAdjacentInstructionPair(code []avm.Instruction, first, second avm.Instruction) bool {
	for i := 0; i+1 < len(code); i++ {
		if code[i].Op != first.Op || code[i+1].Op != second.Op {
			continue
		}
		if len(first.Data) > 0 && string(code[i].Data) != string(first.Data) {
			continue
		}
		if len(second.Data) > 0 && string(code[i+1].Data) != string(second.Data) {
			continue
		}
		return true
	}
	return false
}

// structParamFieldSource declares a @get getter whose OWN declared parameter
// is struct-typed, and reads a field directly off that parameter -- the exact
// shape CONFIRMED FACT 2 addresses (a struct-typed function parameter, not a
// local). This is a genuinely top-level entrypoint parameter (env.params),
// never substituted away by the helper-inlining path, so it is the case that
// actually exercises the lowerExprToIR ExprPath len==2 params branch.
const structParamFieldSource = `
struct Bp2 {
  bps: uint64
}

struct DemoState2 {
  count: uint64 = 0
}

contract ParamFieldDemo {
  storage: DemoState2
  incomingExternal: DemoState2

  @external
  func onExternalMessage(inMsg: Segment) {
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func readParamField(p: Bp2): uint64 {
    return p.bps
  }
}
`

// TestStructTypedParameterFieldReadLowersToArgField is the regression test for
// the compile.go fix in lowerExprToIR's ExprPath (len==2) case: a struct-typed
// parameter (only present in env.params/env.types, never env.locals) must
// lower `p.bps` to a read of the parameter's own arg slot (OpReadMsgField
// "arg0" followed by OpReadField "bps"), not to a bare top-level
// OpReadMsgField "bps" that happens to share the field's name but ignores
// which parameter it came from.
//
// This is checked at the IR/bytecode level rather than via full VM execution
// because the wire codec (runtimeValueFromJSONField in avm/avm.go) has no
// case that decodes a JSON object into a TagMap value for an arbitrary
// struct-typed top-level message/getter argument -- only scalar getter
// arguments (the arg0/arg1/... convention already used by multi-arg getters)
// round-trip through the wire today. That gap is pre-existing, is not part of
// the diagnosed fix, and is out of scope here; it is called out explicitly so
// it isn't silently assumed away.
func TestStructTypedParameterFieldReadLowersToArgField(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)

	res, err := c.Compile([]byte(structParamFieldSource))
	require.NoError(t, err)

	found := findAdjacentInstructionPair(res.Module.Code,
		avm.Instruction{Op: avm.OpReadMsgField, Data: []byte("arg0")},
		avm.Instruction{Op: avm.OpReadField, Data: []byte("bps")},
	)
	require.True(t, found, "expected p.bps to lower to OpReadMsgField(arg0) -> OpReadField(bps)")

	// The old (buggy) lowering emitted a bare top-level field read instead;
	// assert it is gone so this test would have failed before the fix.
	regressed := false
	for _, ins := range res.Module.Code {
		if ins.Op == avm.OpReadMsgField && string(ins.Data) == "bps" {
			regressed = true
		}
	}
	require.False(t, regressed, "must not read a top-level message field named after the parameter's own field")
}

// selfParamFieldSource proves "self is just a parameter named self": the
// exact same struct-typed-parameter lowering must apply uniformly whether the
// parameter happens to be named "self" (as a method receiver would be) or
// anything else -- there is nothing special-cased on the name "self" in the
// lowering, only on env.params/env.types membership.
const selfParamFieldSource = `
struct SelfDemo {
  bps: uint64
}

struct DemoState3 {
  count: uint64 = 0
}

contract SelfFieldDemo {
  storage: DemoState3
  incomingExternal: DemoState3

  @external
  func onExternalMessage(inMsg: Segment) {
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func readSelfField(self: SelfDemo): uint64 {
    return self.bps
  }
}
`

func TestSelfNamedParameterFieldReadUsesSameParamMechanism(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)

	res, err := c.Compile([]byte(selfParamFieldSource))
	require.NoError(t, err)

	found := findAdjacentInstructionPair(res.Module.Code,
		avm.Instruction{Op: avm.OpReadMsgField, Data: []byte("arg0")},
		avm.Instruction{Op: avm.OpReadField, Data: []byte("bps")},
	)
	require.True(t, found, "expected self.bps to lower to OpReadMsgField(arg0) -> OpReadField(bps), same as any other parameter")
}

// nestedStructFieldChainSource is a struct field whose own declared type is
// another struct, accessed via a 3-segment chain (o.inner.z). This is an
// explicitly OUT-OF-SCOPE follow-up: lowerExprToIR's ExprPath case only
// special-cases len(expr.Path) == 1 and == 2; a 3-segment chain falls through
// to the generic "AVM v1 lowering supports only state.<field> reads" error,
// even though the separate static type-checker pass (resolvePathType) already
// walks a field chain of arbitrary depth and would happily type-check this
// same expression. Documented here rather than silently shipped as
// half-working.
const nestedStructFieldChainSource = `
struct Inner {
  z: uint64
}

struct Outer {
  inner: Inner
}

struct DemoState4 {
  count: uint64 = 0
}

contract NestedFieldDemo {
  storage: DemoState4
  incomingExternal: DemoState4

  @external
  func onExternalMessage(inMsg: Segment) {
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func readNestedField(): uint64 {
    const o = Outer{inner: Inner{z: 5}}
    return o.inner.z
  }
}
`

// TestNestedStructFieldChainIsNotSupported documents that a 3+ segment field
// chain (a struct field whose own type is another struct) is not lowered by
// AVM v1 today. It fails at compile time with a clear diagnostic rather than
// silently compiling to a wrong read -- this test pins that behavior so a
// future attempt to add nested-chain support notices this test and updates it
// deliberately instead of it drifting unnoticed.
func TestNestedStructFieldChainIsNotSupported(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)

	_, err = c.Compile([]byte(nestedStructFieldChainSource))
	require.Error(t, err, "nested struct field chains (x.y.z) are not supported by AVM v1 lowering yet")
}
