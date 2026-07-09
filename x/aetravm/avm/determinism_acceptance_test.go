package avm

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeterminismAcceptanceRejectsForbiddenHostCallsAndOpcodes(t *testing.T) {
	caps := CapabilityMask{Crypto: true, Chain: true, Messaging: true, Storage: true}

	require.Error(t, ValidateHostImport(HostWallClockTime, caps))
	require.Error(t, ValidateHostImport(HostRandomness, caps))
	require.NoError(t, ValidateHostImport(HostHashSHA256, caps))

	module := Module{
		Version: Version,
		Exports: map[Entrypoint]uint32{EntryReceiveInternal: 0},
		Code: []Instruction{
			{Op: OpWallClock},
		},
	}
	verifier, err := NewVerifier(DefaultParams())
	require.NoError(t, err)
	require.ErrorContains(t, verifier.Verify(module), "forbidden")

	forbidden := DefaultParams()
	require.NoError(t, forbidden.Validate())
}

func TestGasScheduleIsFixedAndComplete(t *testing.T) {
	first := DefaultParams()
	second := DefaultParams()

	require.NoError(t, first.Validate())
	require.NoError(t, second.Validate())
	require.True(t, reflect.DeepEqual(first.GasSchedule, second.GasSchedule))

	require.Greater(t, first.GasSchedule[OpMapKeys], uint64(0))
	require.Greater(t, first.GasSchedule[OpMapEntries], uint64(0))
	require.Greater(t, first.GasSchedule[OpVerifySignature], uint64(0))
	require.Greater(t, first.GasSchedule[OpDeleteStorage], uint64(0))
}

func TestBoundedMapIterationIsDeterministicAndLimited(t *testing.T) {
	entriesA := []runtimeMapEntry{
		mustRuntimeMapEntry(t, ValueString("z"), ValueUint64(3)),
		mustRuntimeMapEntry(t, ValueString("a"), ValueUint64(1)),
		mustRuntimeMapEntry(t, ValueString("m"), ValueUint64(2)),
	}
	entriesB := []runtimeMapEntry{
		mustRuntimeMapEntry(t, ValueString("m"), ValueUint64(2)),
		mustRuntimeMapEntry(t, ValueString("z"), ValueUint64(3)),
		mustRuntimeMapEntry(t, ValueString("a"), ValueUint64(1)),
	}

	normA, err := runtimeMapNormalize(entriesA)
	require.NoError(t, err)
	normB, err := runtimeMapNormalize(entriesB)
	require.NoError(t, err)
	require.True(t, reflect.DeepEqual(normA, normB))

	keysA := runtimeMapKeys(normA, 2)
	keysB := runtimeMapKeys(normB, 2)
	require.True(t, reflect.DeepEqual(keysA, keysB))

	tuple, err := keysA.AsTuple()
	require.NoError(t, err)
	require.Len(t, tuple, 2)
	firstKey, err := tuple[0].AsString()
	require.NoError(t, err)
	secondKey, err := tuple[1].AsString()
	require.NoError(t, err)
	require.Equal(t, "a", firstKey)
	require.Equal(t, "m", secondKey)

	entries := runtimeMapEntriesValue(normA, 2)
	entryTuple, err := entries.AsTuple()
	require.NoError(t, err)
	require.Len(t, entryTuple, 2)
}

func mustRuntimeMapEntry(t *testing.T, key, value RuntimeValue) runtimeMapEntry {
	t.Helper()
	entry, err := runtimeMapEntryFrom(key, value)
	require.NoError(t, err)
	return entry
}
