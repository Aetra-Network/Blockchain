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

// nestedStructFieldChainSource exercises 3-segment (m.inner.z) and 4-segment
// (o.middle.inner.z) local struct field READ chains, where every intermediate
// segment (inner, middle) is itself a plain struct-typed field of the
// previous segment's own declared type -- e.g. Middle.inner is an Inner, and
// Outer.middle is a Middle. This is the regression test for the compile.go
// fix that extends lowerExprToIR's ExprPath case (previously len==1/len==2
// only) to arbitrary depth N by chaining IRExprField/OpReadField reads. Two
// distinct depths with two distinct leaf values (7 vs 11) guard against a fix
// that happens to work for exactly one extra segment but not further, or
// that accidentally reads the wrong nested value.
//
// It also exercises the SAME lowering rooted at a storage alias
// (`const st = DemoState4.load(); st.outer.z`, a 3-segment chain) rather than
// a local, proving the fix is not local-only: a storage field whose OWN
// declared type is a struct (DemoState4.outer: Inner) can have ITS nested
// field read the same way, because IRExprStateRead produces the same kind of
// runtime map value an IRExprLocalLoad of a struct-typed local does.
const nestedStructFieldChainSource = `
struct Inner {
  z: uint64
}

struct Middle {
  inner: Inner
  y: uint64
}

struct Outer {
  middle: Middle
}

struct DemoState4 {
  count: uint64 = 0
  outer: Inner
}

contract NestedFieldDemo {
  storage: DemoState4
  incomingExternal: DemoState4

  @external
  func onExternalMessage(inMsg: Segment) {
    set state.outer = Inner{z: 77}
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func readThreeSegment(): uint64 {
    const m = Middle{inner: Inner{z: 7}, y: 9}
    return m.inner.z
  }

  @get
  func readFourSegment(): uint64 {
    const o = Outer{middle: Middle{inner: Inner{z: 11}, y: 13}}
    return o.middle.inner.z
  }

  @get
  func readStorageRootedThreeSegment(): uint64 {
    const st = DemoState4.load()
    return st.outer.z
  }
}
`

// TestNestedStructFieldChainReadsThroughVM is the regression test for the
// compile.go fix adding N-deep (N>=3) nested struct field READ lowering: it
// compiles nestedStructFieldChainSource (which the pre-fix compiler rejected
// outright, see the git history of this test) and executes it through the
// real AVM interpreter, not just a compile-time check, confirming each depth
// reads back the correct, distinct value -- proving the chain walks down the
// right nested map at each level rather than, say, silently returning the
// outermost or an unrelated field.
func TestNestedStructFieldChainReadsThroughVM(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)

	res, err := c.Compile([]byte(nestedStructFieldChainSource))
	require.NoError(t, err)

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)

	// Populate state.outer via the external handler so the storage-rooted
	// getter below has a real, non-default nested struct to read.
	exec, err := runner.Run(res.Module, avm.Storage{}, avm.RuntimeContext{
		Entry:    avm.EntryReceiveExternal,
		Message:  async.MessageEnvelope{Opcode: 11, QueryID: 1, GasLimit: 200_000},
		GasLimit: 200_000,
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)

	for _, tc := range []struct {
		getter string
		want   uint64
	}{
		{"readThreeSegment", 7},
		{"readFourSegment", 11},
		{"readStorageRootedThreeSegment", 77},
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

// mapGetUnwrapFieldChainSource is the DELIBERATELY OUT-OF-SCOPE shape called
// out in nestedStructFieldChainSource's fix: a chain through a Map
// .get()-then-unwrap, e.g. `m.get(k)!.bps.nested`. This is a different shape
// from a local/storage binding's own nested struct field -- here the struct
// comes from a MAP ENTRY's value, not from the receiver's own declared type
// -- and this test pins that it is still rejected, with the SAME clear
// diagnostic as before this fix (this source never reaches lowerExprToIR's
// ExprPath case at all, so the new N-deep branch added there cannot affect
// it): the grammar has no postfix `.field` selector after a call expression
// or its trailing `!` unwrap (see parser.go parsePrimary's `finish` closure,
// which consumes `!` but never loops back into a path/selector parse), so
// this is rejected at PARSE time with "unexpected expression token", not a
// confusing new compile/lowering error.
const mapGetUnwrapFieldChainSource = `
struct Bp5 {
  bps: uint64
}

struct DemoState8 {
  count: uint64 = 0
}

contract MapUnwrapDemo {
  storage: DemoState8
  incomingExternal: DemoState8

  @external
  func onExternalMessage(inMsg: Segment) {
    var m = Map.empty()
    set state.count = m.get(getAddress())!.bps.nested
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`

// TestMapGetUnwrapFieldChainStillRejectedClearly documents and pins that the
// out-of-scope map-fetched-struct-field shape keeps failing with a clear,
// unambiguous diagnostic ("unexpected expression token") rather than being
// silently accepted or newly mis-lowered by the N-deep chain support added
// alongside this test.
func TestMapGetUnwrapFieldChainStillRejectedClearly(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)

	_, err = c.Compile([]byte(mapGetUnwrapFieldChainSource))
	require.Error(t, err)
	require.Contains(t, err.Error(), "unexpected expression token",
		"map-fetched struct field chains must still fail with a clear parse-time diagnostic, not a confusing new one")
}

// nestedStructFieldChainWriteSource reproduces the exact silent-state-
// corruption repro found while adversarially reviewing the read-side N-deep
// chain fix above: before the E_SET_NESTED_UNSUPPORTED guard existed, `set
// st.outer.b = st.spare` compiled with ZERO errors (validateStatement's
// StatementSet case only ever inspected stmt.Path[1] == "outer", type-
// checking spare's whole-Inner type against outer's whole-Inner type --
// which matched, since both container fields share the same struct type)
// and lowered to `IRStmtStoreState{Key: "outer", ...}` (lowerStatementsToIR's
// StatementSet case, same stmt.Path[1]-only bug), silently overwriting ALL of
// outer (not just .b) with spare's value. This must now be a compile-time
// error, not a silent runtime corruption.
const nestedStructFieldChainWriteSource = `
struct Inner9 {
  a: uint64
  b: uint64
}

struct DemoState9 {
  outer: Inner9
  spare: Inner9
}

contract NestedFieldWriteDemo {
  storage: DemoState9
  incomingExternal: DemoState9

  @external
  func onExternalMessage(inMsg: Segment) {
    var st = lazy DemoState9.load()
    st.outer.b = st.spare
    st.save()
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`

// TestNestedStructFieldWriteRejectedNotSilentlyCorrupted is the regression
// test for the write-path bug: a 3-segment `set` target (here via a local
// storage-alias field assignment, `st.outer.b = ...`) must be rejected at
// compile time with E_SET_NESTED_UNSUPPORTED, never silently accepted and
// lowered to a whole-field overwrite that discards the trailing segment.
func TestNestedStructFieldWriteRejectedNotSilentlyCorrupted(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)

	_, err = c.Compile([]byte(nestedStructFieldChainWriteSource))
	require.Error(t, err, "a 3+ segment set target must be rejected at compile time, not silently accepted")
	require.Contains(t, err.Error(), "nested (3+ segment) struct field writes are not supported")
}

// nestedStructFieldChainLocalWriteSource is the same bug shape but through a
// LOCAL struct binding's own field chain (`o.middle.inner.z = ...`) rather
// than a storage alias -- lowerStatementsToIR's StatementSet case has a
// SEPARATE stmt.Path[1]-only branch for local bindings (distinct from the
// storage/state branch nestedStructFieldChainWriteSource exercises above), so
// this needs its own regression proof that the len(stmt.Path)>=3 guard closes
// both branches, not just one.
const nestedStructFieldChainLocalWriteSource = `
struct Inner10 {
  z: uint64
}

struct Middle10 {
  inner: Inner10
}

struct Outer10 {
  middle: Middle10
}

struct DemoState10 {
  count: uint64 = 0
}

contract NestedLocalFieldWriteDemo {
  storage: DemoState10
  incomingExternal: DemoState10

  @external
  func onExternalMessage(inMsg: Segment) {
    var o = Outer10{middle: Middle10{inner: Inner10{z: 1}}}
    o.middle.inner.z = 2
    set state.count = o.middle.inner.z
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`

// TestNestedStructFieldLocalWriteRejectedNotSilentlyCorrupted mirrors
// TestNestedStructFieldWriteRejectedNotSilentlyCorrupted for the LOCAL-
// binding branch of the same set-statement lowering, proving the
// E_SET_NESTED_UNSUPPORTED guard closes that branch too.
func TestNestedStructFieldLocalWriteRejectedNotSilentlyCorrupted(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)

	_, err = c.Compile([]byte(nestedStructFieldChainLocalWriteSource))
	require.Error(t, err, "a 3+ segment local-binding set target must be rejected at compile time, not silently accepted")
	require.Contains(t, err.Error(), "nested (3+ segment) struct field writes are not supported")
}
