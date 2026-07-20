package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
)

// This file is the dedicated, direct regression test for AVM generics v1's
// load-bearing slot-disjointness guarantee: because instantiateGenericFunction
// (generics.go) registers each monomorphized clone into the SAME `functions`
// map every ordinary function lookup shares, every instantiation is compiled
// through the IDENTICAL, unmodified tryRealUserFunctionCall/compileCalledFunction/
// claimLocalSlot path (compile.go) that call_slot_allocator_test.go's
// TestCompileTwoRealCallsShareNoSlotWithCallerLocals already pins for plain
// (non-generic) functions. This file asks the equivalent question for
// GENERIC instantiations specifically: do two distinct instantiations of the
// SAME generic declaration -- which, unlike two differently-named plain
// functions, share every byte of source text and differ only in their
// substituted type argument -- ever get assigned overlapping local-slot
// ranges?
//
// Two complementary tests:
//
//   - TestGenericInstantiationsClaimDisjointLocalSlots is a WHITEBOX,
//     structural check: it reads the actual compiled IR of two sibling
//     instantiations directly and asserts their claimed slot-number sets
//     have empty intersection. This is the most literal possible proof of
//     "slots never overlap" -- it does not infer disjointness from correct
//     output, it inspects the slot numbers themselves.
//
//   - TestGenericInstantiationSiblingArmDoesNotCorruptLiveLocal is the
//     BEHAVIORAL counterpart, structurally mirroring
//     TestCompileTwoRealCallsShareNoSlotWithCallerLocals's own shape (a
//     local bound in one match arm, immediately followed by a call that --
//     if slot ranges ever collided -- would silently overwrite it): a local
//     is bound in a match arm, a BRAND NEW generic instantiation (never
//     before compiled anywhere in the module) is discovered and compiled for
//     the first time immediately afterward in the SAME arm, and the
//     previously-bound local must survive that call uncorrupted.

// collectIREntrySlots returns the full set of local-variable slot numbers
// referenced anywhere within entry's own compiled statement tree: every
// IRStmtStoreLocal's Slot (the parameter-binding prologue and every local
// assignment in the body), every element of an IRStmtDestructureTuple's
// Slots, and every IRExprLocalLoad's Slot reachable from any IRStmt's Expr
// or nested sub-expression (Left/Right/Else/Args/Fields). This is a
// whitebox read of exactly which physical slot numbers a single compiled
// call target's own body claims -- the most direct possible way to check
// "these two instantiations' local slots never overlap."
func collectIREntrySlots(entry IREntry) map[uint32]bool {
	out := map[uint32]bool{}
	var walkExpr func(e *IRExpr)
	walkExpr = func(e *IRExpr) {
		if e == nil {
			return
		}
		if e.Kind == IRExprLocalLoad {
			out[e.Slot] = true
		}
		walkExpr(e.Left)
		walkExpr(e.Right)
		walkExpr(e.Else)
		for _, a := range e.Args {
			walkExpr(a)
		}
		for _, f := range e.Fields {
			walkExpr(f.Expr)
		}
	}
	for _, s := range entry.Statements {
		if s.Kind == IRStmtStoreLocal {
			out[s.Slot] = true
		}
		for _, sl := range s.Slots {
			out[sl] = true
		}
		walkExpr(s.Expr)
	}
	return out
}

// calledFunctionByName finds the single IREntry in entries named exactly
// name, failing the test loudly (rather than returning a zero value that
// would silently pass a downstream assertion) if it is missing or -- itself
// a bug worth catching -- duplicated.
func calledFunctionByName(t *testing.T, entries []IREntry, name string) IREntry {
	t.Helper()
	var found []IREntry
	for _, e := range entries {
		if e.Name == name {
			found = append(found, e)
		}
	}
	require.Len(t, found, 1, "expected exactly one compiled call target named %q", name)
	return found[0]
}

// slotDisjointnessStructSource declares spread<T>, a branchy (if/if, real
// CALL/RET, not inlinable) generic function with FOUR distinct locals of its
// own (three parameters a/b/c plus one `var acc`), instantiated with T=uint64
// from one getter and T=uint128 from a sibling getter -- two DISTINCT
// compiled call targets sharing the same module-wide local-slot counter.
const slotDisjointnessStructSource = `
struct DemoState {
  touch: uint64 = 0
}

@impure
func spread<T>(a: T, b: T, c: T): T {
  var acc = a
  if (b > acc) {
    acc = b
  }
  if (c > acc) {
    acc = c
  }
  return acc
}

@message(1)
struct Touch {
  nonce: uint64
}

type ExternalMsg = Touch

contract SlotStructDemo {
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
  func spreadU64(): uint64 {
    return spread::<uint64>(3, 9, 7)
  }

  @get
  func spreadU128(): uint128 {
    return spread::<uint128>(100, 250, 999)
  }
}
`

// TestGenericInstantiationsClaimDisjointLocalSlots is the whitebox
// slot-disjointness regression test: it compiles two sibling instantiations
// of the SAME generic declaration (spread$uint64 / spread$uint128) into one
// module and directly inspects their compiled IR's own slot numbers, rather
// than merely inferring disjointness from correct execution output.
func TestGenericInstantiationsClaimDisjointLocalSlots(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	res, err := c.Compile([]byte(slotDisjointnessStructSource))
	require.NoError(t, err)

	u64Entry := calledFunctionByName(t, res.IR.CalledFunctions, "spread$uint64")
	u128Entry := calledFunctionByName(t, res.IR.CalledFunctions, "spread$uint128")

	u64Slots := collectIREntrySlots(u64Entry)
	u128Slots := collectIREntrySlots(u128Entry)

	// Sanity: the helper actually found real data. spread<T> has 3 params +
	// 1 local (`acc`), so each instantiation must claim at least 4 distinct
	// slots -- if this ever comes back empty/tiny, the test itself would be
	// vacuously "passing" for the wrong reason (a broken helper, not a
	// correct compiler), so pin a floor.
	require.GreaterOrEqual(t, len(u64Slots), 4, "spread$uint64 should claim at least 4 slots (3 params + 1 local)")
	require.GreaterOrEqual(t, len(u128Slots), 4, "spread$uint128 should claim at least 4 slots (3 params + 1 local)")

	for slot := range u64Slots {
		require.False(t, u128Slots[slot],
			"slot %d is claimed by BOTH spread$uint64 (%v) and spread$uint128 (%v) -- sibling generic instantiations must never share a physical local slot",
			slot, u64Slots, u128Slots)
	}
}

// slotDisjointnessBehavioralSource is the behavioral counterpart, engineered
// to structurally reproduce the exact shape of the pre-existing
// claimLocalSlot bug (call_slot_allocator_test.go) but for a GENERIC
// instantiation discovered mid-arm: the UseU128 arm binds a local (`extra`)
// FIRST, then makes the FIRST-EVER call to a brand new instantiation
// (pickMax3$uint128, never compiled by the UseU64 arm above it, which only
// ever touches pickMax3$uint64) immediately afterward, in the SAME arm. If
// pickMax3$uint128's own freshly-claimed slot range were ever computed from
// a stale or per-region-only counter instead of the module-wide, continuously
// synced one, its own internal locals (3 params + 1 `var acc`) could alias
// the physical slot `extra` already occupies -- silently corrupting `extra`
// the moment pickMax3::<uint128> is called, exactly the failure mode
// call_slot_allocator_test.go pins for the plain (non-generic) call
// mechanism.
const slotDisjointnessBehavioralSource = `
struct DemoState {
  resultA: uint64 = 0
  extraB: uint64 = 0
  resultB: uint128 = 0
}

@impure
func pickMax3<T>(a: T, b: T, c: T): T {
  var acc = a
  if (b > acc) {
    acc = b
  }
  if (c > acc) {
    acc = c
  }
  return acc
}

@message(0xB201)
struct UseU64 {
  x: uint64
  y: uint64
  z: uint64
}

@message(0xB202)
struct UseU128 {
  x: uint128
  y: uint128
  z: uint128
  extra: uint64
}

type DemoMsg = UseU64 | UseU128

contract SiblingSlotDemo {
  storage: DemoState
  incomingMessages: DemoMsg

  @internal
  func onInternalMessage(in: InMessage) {
    const msg = lazy DemoMsg.fromSegment(in.body)
    match (msg) {
      UseU64 => {
        var st = lazy DemoState.load()
        const r = pickMax3::<uint64>(msg.x, msg.y, msg.z)
        st.resultA = r
        st.save()
      }
      UseU128 => {
        var st = lazy DemoState.load()
        const extra = msg.extra
        const r = pickMax3::<uint128>(msg.x, msg.y, msg.z)
        st.extraB = extra
        st.resultB = r
        st.save()
      }
      else => {
      }
    }
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func resultA(): uint64 {
    const st = lazy DemoState.load()
    return st.resultA
  }

  @get
  func extraB(): uint64 {
    const st = lazy DemoState.load()
    return st.extraB
  }

  @get
  func resultB(): uint128 {
    const st = lazy DemoState.load()
    return st.resultB
  }
}
`

// TestGenericInstantiationSiblingArmDoesNotCorruptLiveLocal is the
// behavioral slot-disjointness regression test described above. It also
// re-runs the structural slot-set check on this second, independently
// engineered source, so the whitebox and behavioral proofs corroborate each
// other rather than relying on a single example.
func TestGenericInstantiationSiblingArmDoesNotCorruptLiveLocal(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	res, err := c.Compile([]byte(slotDisjointnessBehavioralSource))
	require.NoError(t, err)

	u64Entry := calledFunctionByName(t, res.IR.CalledFunctions, "pickMax3$uint64")
	u128Entry := calledFunctionByName(t, res.IR.CalledFunctions, "pickMax3$uint128")
	u64Slots := collectIREntrySlots(u64Entry)
	u128Slots := collectIREntrySlots(u128Entry)
	require.GreaterOrEqual(t, len(u64Slots), 4)
	require.GreaterOrEqual(t, len(u128Slots), 4)
	for slot := range u64Slots {
		require.False(t, u128Slots[slot], "pickMax3$uint64 and pickMax3$uint128 must not share slot %d", slot)
	}

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)

	// UseU64 first (x=3,y=9,z=7): pickMax3(3,9,7) = 9.
	u64Body, err := res.MessageBodies["UseU64"].Encode(map[string]any{
		"x": uint64(3), "y": uint64(9), "z": uint64(7),
	})
	require.NoError(t, err)
	exec1, err := runner.Run(res.Module, avm.Storage{}, avm.RuntimeContext{
		Entry:    avm.EntryReceiveInternal,
		GasLimit: 1_000_000,
		Message: async.MessageEnvelope{
			Opcode:   res.MessageBodyOpcodes["UseU64"],
			QueryID:  1,
			Body:     u64Body,
			GasLimit: 1_000_000,
		},
	})
	require.NoError(t, err, "submit UseU64")
	require.Equal(t, async.ResultOK, exec1.ResultCode)

	// UseU128 second (x=100,y=250,z=999,extra=555), against the state UseU64
	// just produced: `extra` (=555) is bound BEFORE the first-ever call to
	// pickMax3::<uint128>, exactly the ordering that would expose a
	// generics-specific slot-sync regression.
	u128Body, err := res.MessageBodies["UseU128"].Encode(map[string]any{
		"x": uint64(100), "y": uint64(250), "z": uint64(999), "extra": uint64(555),
	})
	require.NoError(t, err)
	exec2, err := runner.Run(res.Module, exec1.State, avm.RuntimeContext{
		Entry:    avm.EntryReceiveInternal,
		GasLimit: 1_000_000,
		Message: async.MessageEnvelope{
			Opcode:   res.MessageBodyOpcodes["UseU128"],
			QueryID:  2,
			Body:     u128Body,
			GasLimit: 1_000_000,
		},
	})
	require.NoError(t, err, "submit UseU128")
	require.Equal(t, async.ResultOK, exec2.ResultCode)

	getU64 := func(getter string) uint64 {
		t.Helper()
		exec, err := runner.Run(res.Module, exec2.State, avm.RuntimeContext{
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

	require.Equal(t, uint64(9), getU64("resultA"), "pickMax3<uint64>(3,9,7) from the first arm must be correct and survive the second message")
	require.Equal(t, uint64(555), getU64("extraB"), "`extra`, bound immediately before the first-ever call to pickMax3<uint128>, must not be corrupted by that call's own internal locals")
	require.Equal(t, uint64(999), getU64("resultB"), "pickMax3<uint128>(100,250,999) must be correct")
}
