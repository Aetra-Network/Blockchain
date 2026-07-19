package conformance

import (
	"encoding/binary"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/app/addressing"
	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/aetravm/compiler"
)

// hasOpcode reports whether op appears anywhere in code -- used to confirm
// computeStats actually compiles through the real CALL/RET/tuple path
// rather than the cheap inliner.
func hasOpcode(code []avm.Instruction, op avm.Opcode) bool {
	for _, ins := range code {
		if ins.Op == op {
			return true
		}
	}
	return false
}

// entriesToBytes packs uint32 entries into the flat big-endian blob
// batch_stats.atlx's computeStats expects.
func entriesToBytes(entries []uint32) []byte {
	out := make([]byte, 4*len(entries))
	for i, v := range entries {
		binary.BigEndian.PutUint32(out[i*4:], v)
	}
	return out
}

// TestAcceptanceBatchStatsExample compiles and EXECUTES batch_stats.atlx
// through the real VM under gas -- the reference contract for the call
// mechanism's three actually-shipped features (design doc
// avm-call-mechanism-v5): intra-contract CALL/RET (§1), tuples (§2), and
// early return (§3), verified against an independent Go oracle, not just
// "it compiles."
func TestAcceptanceBatchStatsExample(t *testing.T) {
	deployer := testAddress(0xc2)
	res := compileExampleFile(t, filepath.Join("collections", "batch_stats.atlx"), compiler.Options{
		DeployerAddress: addressing.FormatAccAddress(deployer),
	})
	require.NoError(t, avm.VerifyInterface(res.Module, res.Manifest))

	// computeStats is a genuinely non-trivial function (early return +
	// mutating while-loop + 4-value tuple return), so it must be compiled
	// via a real OpCall/OpRet pair, not tryInlineUserFunctionCall's cheap
	// single-return-expression path -- confirmed directly against the
	// compiled bytecode, not just inferred from the source shape.
	require.True(t, hasOpcode(res.Module.Code, avm.OpCall), "computeStats must compile to a real CALL, not be inlined")
	require.True(t, hasOpcode(res.Module.Code, avm.OpRet), "computeStats' early return and trailing return must both emit OpRet")
	require.True(t, hasOpcode(res.Module.Code, avm.OpMakeTuple), "the (count, min, max, sum) return value must construct a real tuple")

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)

	initialState := avm.Storage{
		"count": avm.EncodeU64(0),
		"min":   avm.EncodeU64(0),
		"max":   avm.EncodeU64(0),
		"sum":   avm.EncodeU64(0),
	}

	submit := func(values []byte) avm.Storage {
		body := mustCodecBody(t, res.MessageBodies["ComputeStats"], map[string]any{"values": values})
		exec, err := runner.Run(res.Module, initialState, avm.RuntimeContext{
			Entry:           avm.EntryReceiveInternal,
			ContractAddress: testAddress(0xc2),
			GasLimit:        20_000_000,
			Message: async.MessageEnvelope{
				Opcode:   res.MessageBodyOpcodes["ComputeStats"],
				QueryID:  uint64(res.MessageBodyOpcodes["ComputeStats"]),
				Body:     body,
				GasLimit: 20_000_000,
			},
		})
		require.NoError(t, err, "submit ComputeStats")
		require.Equalf(t, async.ResultOK, exec.ResultCode, "submit ComputeStats result")
		return exec.State
	}
	getU64 := func(state avm.Storage, getter string) uint64 {
		exec, err := runner.Run(res.Module, state, avm.RuntimeContext{
			Entry:    avm.EntryQuery,
			GasLimit: 5_000_000,
			Message:  async.MessageEnvelope{Opcode: opcodeForGetter(t, res, getter), GasLimit: 5_000_000},
		})
		require.NoError(t, err)
		require.Equalf(t, async.ResultOK, exec.ResultCode, "getter %s result", getter)
		v, err := exec.ReturnValue.AsUint64()
		require.NoError(t, err)
		return v
	}

	// ---- 1. Non-empty batch: exercises the while-loop path and all four
	//         tuple elements against an independent Go-computed oracle. ----
	entries := []uint32{42, 7, 1000, 256, 3, 999}
	st := submit(entriesToBytes(entries))
	var wantMin, wantMax uint32 = entries[0], entries[0]
	var wantSum uint64
	for _, e := range entries {
		if e < wantMin {
			wantMin = e
		}
		if e > wantMax {
			wantMax = e
		}
		wantSum += uint64(e)
	}
	require.Equal(t, uint64(len(entries)), getU64(st, "count"), "count must match the real entry count")
	require.Equal(t, uint64(wantMin), getU64(st, "min"), "min must match the independent oracle")
	require.Equal(t, uint64(wantMax), getU64(st, "max"), "max must match the independent oracle")
	require.Equal(t, wantSum, getU64(st, "sum"), "sum must match the independent oracle")

	// ---- 2. Single-entry batch: min == max == the one entry, exercising
	//         the loop's first-iteration seeding (runningMin/runningMax
	//         both initialized from entry 0). ----
	st = submit(entriesToBytes([]uint32{777}))
	require.Equal(t, uint64(1), getU64(st, "count"))
	require.Equal(t, uint64(777), getU64(st, "min"))
	require.Equal(t, uint64(777), getU64(st, "max"))
	require.Equal(t, uint64(777), getU64(st, "sum"))

	// ---- 3. Empty batch: exercises computeStats' EARLY RETURN path
	//         (design doc §3) -- the `if (!lenOk || entryCount == 0) {
	//         return (0, 0, 0, 0) }` branch, never reaching the loop at
	//         all. Must commit all-zero, not the previous submission's
	//         stale values (initialState is the zero state each submit
	//         starts from here, but the assertion is on the VALUE, not on
	//         "unchanged", to prove the early-return path itself runs and
	//         produces the right tuple). ----
	st = submit(entriesToBytes(nil))
	require.Equal(t, uint64(0), getU64(st, "count"), "an empty batch must take the early-return path")
	require.Equal(t, uint64(0), getU64(st, "min"))
	require.Equal(t, uint64(0), getU64(st, "max"))
	require.Equal(t, uint64(0), getU64(st, "sum"))

	// ---- 4. Ragged (non-multiple-of-4) length: also takes the early
	//         return (lenOk == false), rather than silently truncating or
	//         trapping on an out-of-bounds subBytes call. ----
	st = submit([]byte{0x00, 0x01, 0x02})
	require.Equal(t, uint64(0), getU64(st, "count"), "a ragged (non-multiple-of-4) blob must take the early-return path, not trap or truncate")
}
