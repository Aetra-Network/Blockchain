package avm

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/aetravm/async"
)

// This file exercises the read-only cross-contract call mechanism
// (OpCallExternalGet, design doc §6, §6.8) directly at the interpreter
// level, by hand-assembling Module.Code sequences the way the compiler
// would after §6.8 point 4's lowering -- no compiler involvement, so these
// tests pin the VM-level contract the compiler must produce correct
// bytecode against, independent of any compiler-side wiring bug, mirroring
// call_stack_test.go's own convention for OpCall/OpRet.

func packExternalGetArg(selector uint32, tag ValueTag) uint64 {
	return uint64(selector) | uint64(tag)<<32
}

// externalGetCallerModule: entrypoint pushes an empty argument tuple, pushes
// the (test-opaque, not-necessarily-bech32) target address, then
// OpCallExternalGet's with the given selector/expected-tag, and returns
// whatever came back.
func externalGetCallerModule(selector uint32, expectedTag ValueTag, target string) Module {
	return Module{
		Version: Version,
		Imports: []HostFunction{HostReturn},
		Exports: map[Entrypoint]uint32{EntryReceiveInternal: 0},
		Code: []Instruction{
			{Op: OpMakeTuple, Arg: 0},                                     // 0: empty args tuple
			{Op: OpPushAddress, Data: []byte(target)},                     // 1: target address
			{Op: OpCallExternalGet, Arg: packExternalGetArg(selector, expectedTag)}, // 2
			{Op: OpReturn, Arg: 0},                           // 3
		},
	}
}

// externalGetCalleeModule: a @get-shaped EntryQuery entrypoint that checks
// the dispatched selector (via OpReadMsgOpcode, mirroring how a real
// compiled getter's selector-dispatch trap works) and either returns
// `value` or OpAbort's with 0xffff -- the same soft-fail-as-abort shape
// contract_get.go's own selector-mismatch heuristic already pattern-matches
// against ("abort"/"ffff"), so a caller-supplied selector that doesn't
// match this callee's own getter produces exactly the "getter not found"
// trap the design doc's failure-semantics section describes.
func externalGetCalleeModule(wantSelector uint32, value uint64) Module {
	return Module{
		Version: Version,
		Imports: []HostFunction{HostReturn, HostInspectMsg},
		Exports: map[Entrypoint]uint32{EntryQuery: 0},
		Code: []Instruction{
			{Op: OpReadMsgOpcode},                    // 0
			{Op: OpPushU64, Arg: uint64(wantSelector)}, // 1
			{Op: OpEq},                                // 2
			{Op: OpJumpIfZero, Arg: 6},                 // 3: mismatch -> abort at pc 6
			{Op: OpPushU64, Arg: value},                // 4
			{Op: OpReturn, Arg: 0},        // 5
			{Op: OpAbort, Arg: 0xffff},                 // 6
		},
	}
}

// externalGetMutatingCalleeModule attempts OpWriteStorage before returning --
// under a genuine EntryQuery invocation this must trap via the existing
// `if readOnly` guard (avm.go:1062, unchanged by this feature), proving
// OpCallExternalGet's forced Entry: EntryQuery composes correctly with the
// interpreter's pre-existing write-path enforcement.
func externalGetMutatingCalleeModule() Module {
	return Module{
		Version: Version,
		Imports: []HostFunction{HostReturn, HostWriteStorage},
		Exports: map[Entrypoint]uint32{EntryQuery: 0},
		Code: []Instruction{
			{Op: OpPushU64, Arg: 1},                         // 0
			{Op: OpWriteStorage, Data: []byte("k")},         // 1: must trap (readOnly)
			{Op: OpPushU64, Arg: 42},                        // 2 (unreachable)
			{Op: OpReturn, Arg: 0},              // 3 (unreachable)
		},
	}
}

// externalGetDeleteStorageCalleeModule / externalGetEmitInternalCalleeModule /
// externalGetScheduleSelfCalleeModule are the OpDeleteStorage/OpEmitInternal/
// OpScheduleSelf siblings of externalGetMutatingCalleeModule above -- design
// doc §6.7(a) requires all four mutating opcodes be proven to trap through a
// nested externalGet() call, not just OpWriteStorage, since the `if
// readOnly` guard is duplicated per-opcode (avm.go:1145, 1170, 1622, 2506)
// rather than centralized, so a future per-opcode refactor could regress any
// one of them independently without the others catching it.
func externalGetDeleteStorageCalleeModule() Module {
	return Module{
		Version: Version,
		Imports: []HostFunction{HostReturn, HostDeleteStorage},
		Exports: map[Entrypoint]uint32{EntryQuery: 0},
		Code: []Instruction{
			{Op: OpDeleteStorage, Data: []byte("k")}, // 0: must trap (readOnly)
			{Op: OpPushU64, Arg: 42},                 // 1 (unreachable)
			{Op: OpReturn, Arg: 0},                   // 2 (unreachable)
		},
	}
}

func externalGetEmitInternalCalleeModule() Module {
	return Module{
		Version: Version,
		Imports: []HostFunction{HostReturn, HostEmitInternal},
		Exports: map[Entrypoint]uint32{EntryQuery: 0},
		Code: []Instruction{
			{Op: OpEmitInternal, Arg: 1, Data: []byte("out")}, // 0: must trap (readOnly)
			{Op: OpPushU64, Arg: 42},                          // 1 (unreachable)
			{Op: OpReturn, Arg: 0},                            // 2 (unreachable)
		},
	}
}

func externalGetScheduleSelfCalleeModule() Module {
	return Module{
		Version: Version,
		Imports: []HostFunction{HostReturn, HostScheduleSelf},
		Exports: map[Entrypoint]uint32{EntryQuery: 0},
		Code: []Instruction{
			{Op: OpScheduleSelf, Arg: 1, Data: []byte("resume")}, // 0: must trap (readOnly)
			{Op: OpPushU64, Arg: 42},                             // 1 (unreachable)
			{Op: OpReturn, Arg: 0},                               // 2 (unreachable)
		},
	}
}

// externalGetSelfChainModule always calls externalGet on the SAME address
// (a self-referential resolver hands this exact module back every time),
// regardless of the dispatched selector -- used to build a real multi-hop
// recursion chain for the depth-cap test without needing N distinct
// modules/addresses.
func externalGetSelfChainModule(target string, tag ValueTag) Module {
	return Module{
		Version: Version,
		Imports: []HostFunction{HostReturn},
		Exports: map[Entrypoint]uint32{EntryQuery: 0},
		Code: []Instruction{
			{Op: OpMakeTuple, Arg: 0},
			{Op: OpPushAddress, Data: []byte(target)},
			{Op: OpCallExternalGet, Arg: packExternalGetArg(0, tag)},
			{Op: OpReturn, Arg: 0},
		},
	}
}

func externalGetCtx(resolver ExternalGetResolver) RuntimeContext {
	ctx := runtimeCtx(EntryReceiveInternal)
	ctx.ExternalGetResolver = resolver
	return ctx
}

// TestExternalGetSuccess: a genuine successful cross-contract read -- the
// caller invokes another (already-"deployed", i.e. resolver-known) module's
// getter and receives its return value.
func TestExternalGetSuccess(t *testing.T) {
	runner := newTestRunner(t)
	const target = "callee-address-1"
	const selector = uint32(0xC0FFEE)
	callee := externalGetCalleeModule(selector, 777)
	caller := externalGetCallerModule(selector, TagUint64, target)

	resolver := func(addr string, gasBudget uint64) (Module, Storage, bool, error) {
		require.Equal(t, target, addr)
		require.Greater(t, gasBudget, uint64(0))
		return callee, Storage{}, true, nil
	}

	exec, err := runner.Run(caller, Storage{}, externalGetCtx(resolver))
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	got, err := exec.ReturnValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(777), got)
}

// TestExternalGetGasConservation: the callee's actual gas cost is reflected
// exactly once in the caller's total -- no double charge, no free ride. The
// expected total is independently computed: the caller's own flat opcode
// costs, plus the byte-proportional decode charge (computed the same way
// the OpCallExternalGet case computes it), plus the callee's OWN gas usage
// measured by running it standalone.
func TestExternalGetGasConservation(t *testing.T) {
	runner := newTestRunner(t)
	const target = "callee-address-2"
	const selector = uint32(0xBEEF)
	callee := externalGetCalleeModule(selector, 12345)
	calleeStorage := Storage{}
	caller := externalGetCallerModule(selector, TagUint64, target)

	resolver := func(addr string, gasBudget uint64) (Module, Storage, bool, error) {
		return callee, calleeStorage, true, nil
	}

	exec, err := runner.Run(caller, Storage{}, externalGetCtx(resolver))
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)

	// Independently measure the callee's own gas cost in isolation.
	calleeExec, err := runner.Run(callee, calleeStorage, RuntimeContext{
		Entry:    EntryQuery,
		GasLimit: 100_000,
		Message:  async.MessageEnvelope{Opcode: selector, GasLimit: 100_000},
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, calleeExec.ResultCode)

	params := DefaultParams()
	flatCost := params.GasSchedule[OpMakeTuple] + params.GasSchedule[OpPushAddress] +
		params.GasSchedule[OpCallExternalGet] + params.GasSchedule[OpReturn]
	calleeBytes, err := EncodeModule(callee)
	require.NoError(t, err)
	decodeUnits := uint64(len(calleeBytes)) + StorageMemoryBytes(calleeStorage) + uint64(len(calleeStorage))
	expected := flatCost + params.GasPerOperandUnit*decodeUnits + calleeExec.GasUsed

	require.Equal(t, expected, exec.GasUsed, "caller gas must be flat costs + decode charge + callee's own gas, exactly once")
}

// TestExternalGetReturnValueCloneCharged proves the caller pays for cloning
// the callee's return value onto its OWN stack (the `stack =
// append(stack, nestedExec.ReturnValue.clone())` line in avm.go's
// OpCallExternalGet case), a SECOND, independent O(N) copy on top of the
// decodeUnits charge TestExternalGetGasConservation already pins (which is
// priced off the callee's module+storage size, not its return value's
// size). TestExternalGetGasConservation alone cannot catch a regression
// here because its callee returns a plain uint64 -- runtimeValueSizeUnits()
// is 0 for every scalar tag, so chargeOperandGas would silently no-op even
// if it were never called at all. This test uses a multi-KB bytes return
// value instead, whose runtimeValueSizeUnits() equals its length, so the
// charge is actually observable in exec.GasUsed. (Sized at MaxKeySize, the
// interpreter's own ceiling on a single instruction's inline Data payload
// -- large enough to be non-trivial, small enough to stay a single
// OpPushBytes instruction.)
func TestExternalGetReturnValueCloneCharged(t *testing.T) {
	runner := newTestRunner(t)
	const target = "callee-address-bytes"
	const selector = uint32(0xDEADBEEF)
	payload := bytes.Repeat([]byte{0xAB}, MaxKeySize)
	// Same selector-dispatch shape as externalGetCalleeModule, but returns a
	// bytes value (OpPushBytes) instead of a uint64 so the clone this test
	// exists to price is non-trivial.
	callee := Module{
		Version: Version,
		Imports: []HostFunction{HostReturn, HostInspectMsg},
		Exports: map[Entrypoint]uint32{EntryQuery: 0},
		Code: []Instruction{
			{Op: OpReadMsgOpcode},                      // 0
			{Op: OpPushU64, Arg: uint64(selector)},     // 1
			{Op: OpEq},                                 // 2
			{Op: OpJumpIfZero, Arg: 6},                  // 3: mismatch -> abort at pc 6
			{Op: OpPushBytes, Data: payload},           // 4
			{Op: OpReturn, Arg: 0},         // 5
			{Op: OpAbort, Arg: 0xffff},                  // 6
		},
	}
	calleeStorage := Storage{}
	caller := externalGetCallerModule(selector, TagBytes, target)

	resolver := func(addr string, gasBudget uint64) (Module, Storage, bool, error) {
		return callee, calleeStorage, true, nil
	}

	exec, err := runner.Run(caller, Storage{}, externalGetCtx(resolver))
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	got, err := exec.ReturnValue.AsBytes()
	require.NoError(t, err)
	require.Equal(t, payload, got)

	// Independently measure the callee's own gas cost in isolation -- it
	// already pays its OWN OpReturn clone charge for `payload` out of its
	// own gas budget (avm.go:2520-2528), refunded whole into the caller's
	// exec.GasUsed by the boundary-gas-sharing logic just above the clone
	// this test targets.
	calleeExec, err := runner.Run(callee, calleeStorage, RuntimeContext{
		Entry:    EntryQuery,
		GasLimit: 1_000_000,
		Message:  async.MessageEnvelope{Opcode: selector, GasLimit: 1_000_000},
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, calleeExec.ResultCode)

	params := DefaultParams()
	flatCost := params.GasSchedule[OpMakeTuple] + params.GasSchedule[OpPushAddress] +
		params.GasSchedule[OpCallExternalGet] + params.GasSchedule[OpReturn]
	calleeBytes, err := EncodeModule(callee)
	require.NoError(t, err)
	decodeUnits := uint64(len(calleeBytes)) + StorageMemoryBytes(calleeStorage) + uint64(len(calleeStorage))
	// The charge this test exists to pin: pushing the callee's returned
	// `payload` onto the CALLER's own stack (OpCallExternalGet's clone) is
	// priced the same as OpDup/OpLoadLocal/OpReturn would price cloning a
	// same-sized value -- on top of, not instead of, the decode charge and
	// the callee's own gas. It is charged TWICE over in this specific test,
	// for two independent, both-legitimate reasons: once by
	// OpCallExternalGet itself (the fix this test targets) for pushing
	// `payload` onto the stack, and once more by the CALLER's own trailing
	// OpReturn (pre-existing, unrelated to this fix -- see avm.go:2520-2528)
	// because `payload` is still sitting on top of the stack when the
	// caller itself returns, so its own OpReturn clones it too.
	returnCloneUnits := uint64(len(payload))
	expected := flatCost + params.GasPerOperandUnit*decodeUnits + calleeExec.GasUsed + 2*params.GasPerOperandUnit*returnCloneUnits

	require.Equal(t, expected, exec.GasUsed, "caller gas must also include the return-value clone charge, not just the decode charge")
}

// TestExternalGetTargetNotFound: a resolver soft-fail (found=false, err=nil,
// mirroring "contract not found"/"code record missing"/"not executable"/
// "storage not decodable") aborts the caller's whole execution -- no
// rollback bookkeeping needed for the target, since it never ran.
func TestExternalGetTargetNotFound(t *testing.T) {
	runner := newTestRunner(t)
	const selector = uint32(1)
	caller := externalGetCallerModule(selector, TagUint64, "missing-address")

	resolver := func(addr string, gasBudget uint64) (Module, Storage, bool, error) {
		return Module{}, nil, false, nil
	}

	exec, err := runner.Run(caller, Storage{}, externalGetCtx(resolver))
	require.Error(t, err)
	require.Equal(t, async.ResultExecutionFailed, exec.ResultCode)
}

// TestExternalGetTargetNotQueryable: a resolver hard-fail (err != nil,
// mirroring EnsureContractLifecycleAction rejecting a frozen/upgrading
// contract, or the pre-decode storage-clone gas floor rejecting a
// near-zero remaining budget against a large-storage target) also aborts
// the caller's whole execution.
func TestExternalGetTargetNotQueryable(t *testing.T) {
	runner := newTestRunner(t)
	const selector = uint32(1)
	caller := externalGetCallerModule(selector, TagUint64, "frozen-address")

	resolver := func(addr string, gasBudget uint64) (Module, Storage, bool, error) {
		return Module{}, nil, false, errors.New("contract lifecycle forbids query")
	}

	exec, err := runner.Run(caller, Storage{}, externalGetCtx(resolver))
	require.Error(t, err)
	require.Equal(t, async.ResultExecutionFailed, exec.ResultCode)
}

// TestExternalGetGetterNotFound: the target contract exists and is
// queryable, but the caller-requested getter name (selector) does not match
// anything the callee's own dispatch recognizes -- surfaced as a nested-
// Run() trap (OpAbort here, standing in for a real compiled getter's
// selector-dispatch default case), which the caller treats as a whole-
// execution abort exactly like any other external-get failure (design doc
// §6.6 point 6's scope-reduced failure semantics).
func TestExternalGetGetterNotFound(t *testing.T) {
	runner := newTestRunner(t)
	const target = "callee-address-3"
	const wantSelector = uint32(42)
	const wrongSelector = uint32(999)
	callee := externalGetCalleeModule(wantSelector, 1)
	caller := externalGetCallerModule(wrongSelector, TagUint64, target)

	resolver := func(addr string, gasBudget uint64) (Module, Storage, bool, error) {
		return callee, Storage{}, true, nil
	}

	exec, err := runner.Run(caller, Storage{}, externalGetCtx(resolver))
	require.Error(t, err)
	// OpAbort in the callee stamps its own Arg (0xffff) as the result code,
	// and that specific code survives back to the top-level caller (design
	// doc §6.8 point 2 step 8) rather than being collapsed to a generic
	// ResultExecutionFailed.
	require.Equal(t, uint32(0xffff), exec.ResultCode)
}

// TestExternalGetReturnTypeMismatchTrapped: the callee is a genuinely
// reachable, successfully-dispatched getter (unlike
// TestExternalGetGetterNotFound's selector-mismatch abort, where the callee
// itself never returns successfully) -- but its actual return value's Tag
// does not match the caller's expectedTag packed into the OpCallExternalGet
// operand. The avm.go:1529 check (comparing nestedExec.ReturnValue.Tag
// against expectedTag AFTER the nested Run() has already succeeded) must
// reject this, not silently hand the caller a wrongly-typed RuntimeValue
// (e.g. a uint64 the caller decodes as if it were a string). Before this
// test, no test anywhere in the codebase (avm, compiler, or keeper package)
// exercised a genuine tag mismatch against a callee that itself ran to
// completion.
func TestExternalGetReturnTypeMismatchTrapped(t *testing.T) {
	runner := newTestRunner(t)
	const target = "callee-address-mismatch"
	const selector = uint32(0x1234)
	// The callee genuinely succeeds and returns a uint64 -- it is the
	// CALLER's expectedTag (TagString) that is wrong, so this is a type
	// mismatch on a successful call, not a dispatch failure.
	callee := externalGetCalleeModule(selector, 999)
	caller := externalGetCallerModule(selector, TagString, target)

	resolver := func(addr string, gasBudget uint64) (Module, Storage, bool, error) {
		return callee, Storage{}, true, nil
	}

	exec, err := runner.Run(caller, Storage{}, externalGetCtx(resolver))
	require.Error(t, err)
	require.Equal(t, async.ResultExecutionFailed, exec.ResultCode)
	require.ErrorContains(t, err, "return type mismatch")
}

// TestExternalGetMutationTrapped: a mutation attempt from inside the callee
// still traps via the existing `if readOnly` guard, because
// OpCallExternalGet's nested RuntimeContext always forces Entry: EntryQuery
// -- proving the resolver/opcode composes correctly with pre-existing
// write-path enforcement rather than needing new guards of its own. Covers
// all four mutating opcodes (design doc §6.7(a)'s pre-merge requirement),
// not just OpWriteStorage: OpDeleteStorage, OpEmitInternal and
// OpScheduleSelf each have their own independent `if readOnly` guard
// (avm.go:1170, 1622, 2506) that a per-opcode refactor could regress without
// the OpWriteStorage-only version of this test ever noticing.
func TestExternalGetMutationTrapped(t *testing.T) {
	cases := []struct {
		name       string
		callee     Module
		errContain string
	}{
		{"OpWriteStorage", externalGetMutatingCalleeModule(), "cannot write storage"},
		{"OpDeleteStorage", externalGetDeleteStorageCalleeModule(), "cannot delete storage"},
		{"OpEmitInternal", externalGetEmitInternalCalleeModule(), "cannot emit internal messages"},
		{"OpScheduleSelf", externalGetScheduleSelfCalleeModule(), "cannot schedule self messages"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner := newTestRunner(t)
			const target = "callee-address-4"
			caller := externalGetCallerModule(0, TagUint64, target)

			resolver := func(addr string, gasBudget uint64) (Module, Storage, bool, error) {
				return tc.callee, Storage{}, true, nil
			}

			exec, err := runner.Run(caller, Storage{}, externalGetCtx(resolver))
			require.Error(t, err)
			require.Equal(t, async.ResultExecutionFailed, exec.ResultCode)
			require.ErrorContains(t, err, tc.errContain, "must trap via the opcode's own readOnly guard specifically, not some other unrelated failure")
		})
	}
}

// TestExternalGetRecursionDepthEnforced: a real multi-hop chain (a
// self-referential resolver hands back a module that itself calls
// externalGet again on the same address) is rejected once it would exceed
// Params.MaxExternalGetDepth, checked BEFORE the resolver is ever invoked
// at the offending depth -- and a chain that stays within the cap succeeds.
func TestExternalGetRecursionDepthEnforced(t *testing.T) {
	const target = "self-chain-address"
	chain := externalGetSelfChainModule(target, TagUint64)

	var resolveCount int
	resolver := func(addr string, gasBudget uint64) (Module, Storage, bool, error) {
		resolveCount++
		return chain, Storage{}, true, nil
	}

	t.Run("exceeds cap", func(t *testing.T) {
		resolveCount = 0
		runner, err := NewRunner(func() Params {
			p := DefaultParams()
			p.MaxExternalGetDepth = 4
			return p
		}())
		require.NoError(t, err)
		// chain only exports EntryQuery (it IS the getter this whole test
		// keeps recursively "cross-contract"-calling into itself), so the
		// top-level invocation must dispatch to EntryQuery too, not
		// externalGetCtx's default EntryReceiveInternal.
		ctx := RuntimeContext{
			Entry:               EntryQuery,
			GasLimit:            1_000_000,
			Message:             async.MessageEnvelope{GasLimit: 1_000_000},
			ExternalGetResolver: resolver,
		}
		exec, err := runner.Run(chain, Storage{}, ctx)
		require.Error(t, err)
		require.Equal(t, async.ResultLimitExceeded, exec.ResultCode)
		// Depths 0,1,2,3 each successfully resolve and recurse; the 5th
		// invocation (depth 4) is rejected by avm.go's own check BEFORE the
		// resolver is ever called again, so resolveCount is bounded exactly
		// at the cap, not unbounded.
		require.Equal(t, 4, resolveCount, "resolver must not be invoked past the depth cap")
	})

	t.Run("within cap succeeds at depth 1", func(t *testing.T) {
		// A single external-get hop (no self-recursion inside the callee)
		// well within any positive depth cap must still succeed -- sanity
		// check that the cap only rejects genuine over-depth chains, not
		// ordinary single-hop calls.
		runner := newTestRunner(t)
		const selector = uint32(7)
		callee := externalGetCalleeModule(selector, 55)
		caller := externalGetCallerModule(selector, TagUint64, "one-hop-address")
		oneHopResolver := func(addr string, gasBudget uint64) (Module, Storage, bool, error) {
			return callee, Storage{}, true, nil
		}
		exec, err := runner.Run(caller, Storage{}, externalGetCtx(oneHopResolver))
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, exec.ResultCode)
	})
}

// TestExternalGetNoResolverConfigured: a call site that never wired
// ExternalGetResolver (RuntimeContext's zero value, matching every
// pre-existing call site before this feature) traps cleanly instead of
// panicking with a nil-function-call.
func TestExternalGetNoResolverConfigured(t *testing.T) {
	runner := newTestRunner(t)
	caller := externalGetCallerModule(0, TagUint64, "any-address")
	ctx := runtimeCtx(EntryReceiveInternal) // ExternalGetResolver left nil

	exec, err := runner.Run(caller, Storage{}, ctx)
	require.Error(t, err)
	require.Equal(t, async.ResultExecutionFailed, exec.ResultCode)
}
