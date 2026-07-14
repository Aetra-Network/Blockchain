package avm

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/aetravm/async"
)

// mapCloneAmplifierModule builds a module whose EntryReceiveExternal handler:
//  1. Builds a runtime map of N key/value entries via a bounded loop (static
//     instruction count is independent of N -- the loop body is fixed-size and
//     runs N times at runtime, exactly like a real contract would build up
//     state).
//  2. Pushes that map onto the value stack once, then runs a SEPARATE, fixed
//     ITERS-count loop that repeatedly executes OpDup (gas cost 1, per the
//     default GasSchedule in avm.go) followed by OpDrop.
//
// OpDup's implementation (avm.go, "case OpDup") does
// `stack = append(stack, stack[len(stack)-1].clone())` -- i.e. it deep-clones
// whatever value sits on top of the stack, unconditionally, regardless of that
// value's size. When the top-of-stack value is a map with N entries, each
// OpDup is an O(N) deep-copy operation charged at the SAME flat 1-gas price as
// duplicating a single integer. This is FINDING-001
// (security-audit/05-findings/FINDING-001-avm-gas-mispricing-dos.md).
func mapCloneAmplifierModule(mapEntries, dupIterations uint64) Module {
	const (
		mapLocal  = 0 // holds the accumulated map during the build loop
		buildCtr  = 1 // build-loop counter (0..mapEntries)
		dosCtr    = 2 // dup-loop counter (0..dupIterations)
		buildLoop = 4
		exitBuild = 18
		dosLoop   = 21
		exitDos   = 32
	)
	return Module{
		Version: Version,
		Imports: []HostFunction{HostReturn},
		Exports: map[Entrypoint]uint32{EntryReceiveExternal: 0},
		Code: []Instruction{
			/*0*/ {Op: OpMapEmpty},
			/*1*/ {Op: OpStoreLocal, Arg: mapLocal},
			/*2*/ {Op: OpPushU64, Arg: 0},
			/*3*/ {Op: OpStoreLocal, Arg: buildCtr},

			// BUILD LOOP (pc=4..17): while buildCtr < mapEntries { map[buildCtr] = buildCtr; buildCtr++ }
			/*4*/ {Op: OpLoadLocal, Arg: buildCtr},
			/*5*/ {Op: OpPushU64, Arg: mapEntries},
			/*6*/ {Op: OpLt},
			/*7*/ {Op: OpJumpIfZero, Arg: exitBuild},
			/*8*/ {Op: OpLoadLocal, Arg: mapLocal},
			/*9*/ {Op: OpLoadLocal, Arg: buildCtr},
			/*10*/ {Op: OpLoadLocal, Arg: buildCtr},
			/*11*/ {Op: OpMapSet},
			/*12*/ {Op: OpStoreLocal, Arg: mapLocal},
			/*13*/ {Op: OpLoadLocal, Arg: buildCtr},
			/*14*/ {Op: OpPushU64, Arg: 1},
			/*15*/ {Op: OpAdd},
			/*16*/ {Op: OpStoreLocal, Arg: buildCtr},
			/*17*/ {Op: OpJump, Arg: buildLoop},

			// pc=18: push the finished map ONCE; it stays at the bottom of the
			// value stack for the entire DOS loop below.
			/*18*/ {Op: OpLoadLocal, Arg: mapLocal},
			/*19*/ {Op: OpPushU64, Arg: 0},
			/*20*/ {Op: OpStoreLocal, Arg: dosCtr},

			// DOS LOOP (pc=21..31): while dosCtr < dupIterations { dup(map); drop; dosCtr++ }
			// Each OpDup costs 1 gas (avm.go DefaultParams GasSchedule) but
			// clones the whole N-entry map underneath it.
			/*21*/ {Op: OpLoadLocal, Arg: dosCtr},
			/*22*/ {Op: OpPushU64, Arg: dupIterations},
			/*23*/ {Op: OpLt},
			/*24*/ {Op: OpJumpIfZero, Arg: exitDos},
			/*25*/ {Op: OpDup},
			/*26*/ {Op: OpDrop},
			/*27*/ {Op: OpLoadLocal, Arg: dosCtr},
			/*28*/ {Op: OpPushU64, Arg: 1},
			/*29*/ {Op: OpAdd},
			/*30*/ {Op: OpStoreLocal, Arg: dosCtr},
			/*31*/ {Op: OpJump, Arg: dosLoop},

			/*32*/ {Op: OpDrop}, // drop the original map, balancing the stack
			/*33*/ {Op: OpReturn, Arg: uint64(async.ResultOK)},
		},
	}
}

// TestAVMGasMispricingOpDupScalesWithMapSizeNotGas is the regression test for
// the FINDING-001 fix. It runs the IDENTICAL number of OpDup/OpDrop
// iterations against maps of increasing size and verifies that the DOS
// loop's OWN gas contribution -- isolated from the (also now
// size-proportional) map-build loop via a dupIterations=0 baseline run --
// scales up with map size, instead of staying flat as it did before OpDup,
// OpLoadLocal, and OpStoreLocal were priced proportionally to the size of
// the value they clone. See avm.go's chargeOperandGas / GasPerOperandUnit
// and FINDING-001 (security-audit/05-findings/FINDING-001-avm-gas-mispricing-dos.md).
//
// Before the fix, this test asserted the OPPOSITE: that gas stayed flat
// while wall-clock blew up (the bug). It now asserts gas tracks map size.
func TestAVMGasMispricingOpDupScalesWithMapSizeNotGas(t *testing.T) {
	const dupIterations = 100
	// Generous headroom: building a 1500-entry map now costs its true,
	// correctly-priced O(N) gas per element touched by OpLoadLocal/
	// OpMapSet/OpStoreLocal each build-loop iteration (empirically ~3.5M gas
	// for mapEntries=1500 below), which is intentional -- see the fix
	// commentary on Params.GasPerOperandUnit. Both sizes below complete
	// comfortably within this limit.
	const gasLimit = 10_000_000

	sizes := []uint64{50, 1500}
	durations := make([]time.Duration, len(sizes))
	dosLoopGas := make([]uint64, len(sizes))

	run := func(n, iters uint64) (Execution, time.Duration) {
		t.Helper()
		runner := newTestRunner(t)
		module := mapCloneAmplifierModule(n, iters)

		start := time.Now()
		exec, err := runner.Run(module, Storage{}, RuntimeContext{
			Entry:    EntryReceiveExternal,
			Message:  testAsyncMessage(testAddr(9), testAddr(8), 1),
			GasLimit: gasLimit,
		})
		elapsed := time.Since(start)
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, exec.ResultCode,
			"execution must complete within gasLimit=%d for mapEntries=%d, dupIterations=%d (raise gasLimit if this fails)", gasLimit, n, iters)
		return exec, elapsed
	}

	for i, n := range sizes {
		// baseline isolates the map-build loop's own (now also
		// size-proportional) cost, with ZERO OpDup/OpDrop iterations.
		baseline, _ := run(n, 0)
		// full re-runs the identical build loop plus dupIterations of
		// OpDup/OpDrop. Since the build loop is deterministic in N, its gas
		// contribution is IDENTICAL between baseline and full, so
		// subtracting it out isolates exactly what the DOS loop itself
		// (dominated by OpDup) cost in gas.
		full, elapsed := run(n, dupIterations)
		durations[i] = elapsed
		dosLoopGas[i] = full.GasUsed - baseline.GasUsed

		t.Logf("mapEntries=%-6d dupIterations=%-4d buildOnlyGas=%-9d totalGas=%-9d dosLoopGas=%-8d wallClock=%v",
			n, dupIterations, baseline.GasUsed, full.GasUsed, dosLoopGas[i], durations[i])
	}

	// FIX VERIFICATION (gas): identical dupIterations across both runs, so
	// under the OLD flat OpDup pricing dosLoopGas would be IDENTICAL
	// regardless of map size (gas ratio ~1x). Under the FIXED pricing,
	// OpDup clones the whole map every iteration and is charged
	// GasPerOperandUnit per entry cloned, so dosLoopGas must scale up with
	// map size along with it. Require the gas ratio to track at least half
	// the size ratio -- comfortably above the ~1x the bug would produce,
	// while leaving headroom for the fixed per-iteration overhead
	// (OpLoadLocal/OpPushU64/OpLt/... around the OpDup) that does not scale
	// with N.
	sizeRatio := float64(sizes[len(sizes)-1]) / float64(sizes[0])
	gasRatio := float64(dosLoopGas[len(dosLoopGas)-1]) / float64(dosLoopGas[0])
	require.Greater(t, gasRatio, sizeRatio/2,
		"FIX REGRESSION: DOS-loop gas for mapEntries=%d (%d gas) must scale roughly with map size relative to mapEntries=%d (%d gas) for the SAME %d OpDup iterations -- "+
			"got only a %.1fx gas increase for a %.1fx size increase; OpDup (and OpLoadLocal/OpStoreLocal feeding it) must charge gas proportional to the size of the cloned value, not a flat per-opcode constant (FINDING-001)",
		sizes[len(sizes)-1], dosLoopGas[len(dosLoopGas)-1], sizes[0], dosLoopGas[0], dupIterations, gasRatio, sizeRatio)

	// Sanity check (wall-clock): correctly PRICING OpDup must not have made
	// the underlying clone itself any cheaper -- the real work performed is
	// still O(mapEntries) per OpDup, so wall-clock for the large map must
	// still be markedly larger than for the small map. This confirms the
	// gas increase above tracks genuine additional work rather than being
	// an arbitrary surcharge unrelated to what the interpreter actually does.
	require.Greater(t, durations[len(durations)-1], durations[0]*3,
		"wall-clock time for mapEntries=%d (%v) should still be markedly larger than for mapEntries=%d (%v): pricing OpDup correctly must not change the real O(mapEntries) cost of the clone it performs",
		sizes[len(sizes)-1], durations[len(durations)-1], sizes[0], durations[0])
}
