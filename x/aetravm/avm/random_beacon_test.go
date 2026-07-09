package avm

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/aetravm/async"
)

// TestOpReadRandomIsDeterministicBeaconBacked proves random() (OpReadRandom) is
// wired to the deterministic beacon: identical consensus inputs yield identical
// values (validator agreement), the value tracks the beacon primitive, and it
// is no longer the old constant-zero stub.
func TestOpReadRandomIsDeterministicBeaconBacked(t *testing.T) {
	runner := newTestRunner(t)
	msg := testAsyncMessage(testAddr(9), testAddr(8), 1)
	ctx := RuntimeContext{
		Entry:         EntryQuery,
		Message:       msg,
		GasLimit:      100_000,
		PrevStateRoot: []byte("prev-state-root-A"),
		BlockEntropy:  []byte("block-hash-A"),
	}
	mod := Module{
		Version: Version,
		Exports: map[Entrypoint]uint32{EntryQuery: 0},
		Code:    []Instruction{{Op: OpReadRandom}},
	}

	exec1, err := runner.Run(mod, nil, ctx)
	require.NoError(t, err)
	got1, err := exec1.ReturnValue.AsUint64()
	require.NoError(t, err)

	exec2, err := runner.Run(mod, nil, ctx)
	require.NoError(t, err)
	got2, err := exec2.ReturnValue.AsUint64()
	require.NoError(t, err)

	require.Equal(t, got1, got2, "identical consensus inputs must yield identical randomness")
	require.Equal(t, BeaconRandomU64(ctx.PrevStateRoot, ctx.BlockEntropy, msg, 0), got1,
		"random() must equal the beacon primitive for the first call")
	require.NotZero(t, got1, "random() must no longer be the constant-zero stub")
}

// TestOpReadRandomVariesWithBlockEntropy proves the value is unpredictable
// without the current block hash: changing block entropy changes the result,
// while everything else is held constant.
func TestOpReadRandomVariesWithBlockEntropy(t *testing.T) {
	runner := newTestRunner(t)
	msg := testAsyncMessage(testAddr(9), testAddr(8), 1)
	base := RuntimeContext{
		Entry:         EntryQuery,
		Message:       msg,
		GasLimit:      100_000,
		PrevStateRoot: []byte("prev-state-root"),
		BlockEntropy:  []byte("block-hash-A"),
	}
	mod := Module{
		Version: Version,
		Exports: map[Entrypoint]uint32{EntryQuery: 0},
		Code:    []Instruction{{Op: OpReadRandom}},
	}

	execA, err := runner.Run(mod, nil, base)
	require.NoError(t, err)
	gotA, err := execA.ReturnValue.AsUint64()
	require.NoError(t, err)

	other := base
	other.BlockEntropy = []byte("block-hash-B")
	execB, err := runner.Run(mod, nil, other)
	require.NoError(t, err)
	gotB, err := execB.ReturnValue.AsUint64()
	require.NoError(t, err)

	require.NotEqual(t, gotA, gotB, "different block entropy must produce different randomness")
}

// TestOpReadRandomDomainSeparatesRepeatedReads proves successive random() reads
// inside one execution are domain-separated, so a contract calling random()
// twice gets two independent values rather than the same one.
func TestOpReadRandomDomainSeparatesRepeatedReads(t *testing.T) {
	runner := newTestRunner(t)
	ctx := RuntimeContext{
		Entry:         EntryReceiveInternal,
		Message:       testAsyncMessage(testAddr(3), testAddr(4), 2),
		GasLimit:      100_000,
		PrevStateRoot: []byte("root"),
		BlockEntropy:  []byte("entropy"),
	}
	mod := Module{
		Version: Version,
		Imports: []HostFunction{HostWriteStorage, HostReturn},
		Exports: map[Entrypoint]uint32{EntryReceiveInternal: 0},
		Code: []Instruction{
			{Op: OpReadRandom},
			{Op: OpWriteStorage, Data: []byte("r0")},
			{Op: OpReadRandom},
			{Op: OpWriteStorage, Data: []byte("r1")},
			{Op: OpReturn, Arg: uint64(async.ResultOK)},
		},
	}

	exec, err := runner.Run(mod, nil, ctx)
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	require.NotEmpty(t, exec.State["r0"])
	require.NotEqual(t, exec.State["r0"], exec.State["r1"], "repeated random() reads must be domain-separated")

	require.Equal(t, BeaconRandomU64(ctx.PrevStateRoot, ctx.BlockEntropy, ctx.Message, 0), DecodeU64(exec.State["r0"]))
	require.Equal(t, BeaconRandomU64(ctx.PrevStateRoot, ctx.BlockEntropy, ctx.Message, 1), DecodeU64(exec.State["r1"]))
}

// TestOpReadRandomOpcodeClassification pins the security boundary: the new
// deterministic random source is allowed, while the legacy process-entropy
// OpRandom stays forbidden.
func TestOpReadRandomOpcodeClassification(t *testing.T) {
	require.True(t, IsAllowedOpcode(OpReadRandom))
	require.False(t, IsForbiddenOpcode(OpReadRandom))
	require.True(t, IsForbiddenOpcode(OpRandom), "process-entropy OpRandom must remain forbidden")
}
