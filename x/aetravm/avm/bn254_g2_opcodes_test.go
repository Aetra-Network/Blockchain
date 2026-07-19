package avm

import (
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc/bn254"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr/poseidon2"
	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/aetravm/async"
)

// --- BN254 G2 test vectors: derived directly from gnark-crypto's own
// exported Generators(), the same library the opcodes are built on -- these
// are cross-checks against the library's own arithmetic (proving the AVM's
// codec/dispatch/gas wiring is correct), not independent third-party known-
// answer vectors. A genuinely independent differential vector (e.g. from a
// second toolchain) is the strengthened-test-matrix work the design doc
// defers to the Groth16 stdlib stage, once these primitive opcodes exist for
// it to build on. ---

func TestAVMBn254G2AddKnownVector(t *testing.T) {
	_, _, _, g2Aff := bn254.Generators()
	require.True(t, g2Aff.IsOnCurve(), "sanity: generator is on curve")
	require.True(t, g2Aff.IsInSubGroup(), "sanity: generator is in the correct subgroup")
	gBytes := bn254EncodeG2(g2Aff)
	require.Len(t, gBytes, bn254G2PointSize)

	var want bn254.G2Affine
	want.Add(&g2Aff, &g2Aff) // 2G

	exec, err := runByteCode(t, []Instruction{pushBytes(gBytes), pushBytes(gBytes), {Op: OpBn254G2Add}})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	got, err := exec.ReturnValue.AsBytes()
	require.NoError(t, err)
	require.Equal(t, bn254EncodeG2(want), got, "bn254G2Add(G,G) must equal 2G")
	require.GreaterOrEqual(t, exec.GasUsed, uint64(13_500), "bn254G2Add must charge G2_ADD_BASE + 2*G2_SUBGROUP_CHECK_COST")
}

func TestAVMBn254G2ScalarMulKnownVector(t *testing.T) {
	_, _, _, g2Aff := bn254.Generators()
	gBytes := bn254EncodeG2(g2Aff)
	scalar := big.NewInt(7)

	var want bn254.G2Affine
	want.ScalarMultiplication(&g2Aff, scalar)

	code := []Instruction{pushBytes(gBytes)}
	code = append(code, pushU256(t, scalar)...)
	code = append(code, Instruction{Op: OpBn254G2ScalarMul})
	exec, err := runByteCode(t, code)
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	got, err := exec.ReturnValue.AsBytes()
	require.NoError(t, err)
	require.Equal(t, bn254EncodeG2(want), got, "bn254G2ScalarMul(G,7) must equal 7G")
	require.GreaterOrEqual(t, exec.GasUsed, uint64(24_000), "bn254G2ScalarMul must charge G2_SCALARMUL_BASE + G2_SUBGROUP_CHECK_COST")
}

// TestAVMBn254G2ScalarMulNegativeScalarSoftFails mirrors G1's convention: a
// negative scalar magnitude (reachable via signed tags, since the verifier
// does no stack-type analysis) soft-fails to empty bytes rather than
// trapping. Exercised directly against the runtime helper (like
// TestAVMIsqrtNegativeTraps) since there is no bytecode-level way to push a
// negative int256 literal directly.
func TestAVMBn254G2ScalarMulNegativeScalarSoftFails(t *testing.T) {
	_, _, _, g2Aff := bn254.Generators()
	point := ValueBytes(bn254EncodeG2(g2Aff))
	neg, err := runtimeFromBigIntChecked(TagInt256, big.NewInt(-1))
	require.NoError(t, err)

	out, err := runtimeBn254G2ScalarMul(point, neg)
	require.NoError(t, err, "a negative scalar must soft-fail, not trap")
	require.Empty(t, out)
}

// TestAVMBn254G2InfinityIsAccepted proves the all-zero 128-byte encoding
// decodes as the valid identity element with no AVM-side special-casing
// (gnark-crypto's own IsOnCurve()/IsInSubGroup() already accept (0,0,0,0)),
// per the design doc's Status-section correction to v3's fix 4: adding the
// identity to any point P must return P unchanged.
func TestAVMBn254G2InfinityIsAccepted(t *testing.T) {
	_, _, _, g2Aff := bn254.Generators()
	gBytes := bn254EncodeG2(g2Aff)
	infinity := make([]byte, bn254G2PointSize)

	exec, err := runByteCode(t, []Instruction{pushBytes(gBytes), pushBytes(infinity), {Op: OpBn254G2Add}})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	got, err := exec.ReturnValue.AsBytes()
	require.NoError(t, err)
	require.Equal(t, gBytes, got, "G + infinity must equal G")
}

// TestAVMBn254G2MalformedInputSoftFails covers the non-exception malformed
// cases the opcode family shares: wrong length, a coordinate >= the base
// field modulus p (non-canonical, must be REJECTED not silently reduced),
// and a coordinate pair that does not satisfy the twist curve equation
// (off-curve). All soft-fail to empty bytes; none trap.
func TestAVMBn254G2MalformedInputSoftFails(t *testing.T) {
	_, _, _, g2Aff := bn254.Generators()
	gBytes := bn254EncodeG2(g2Aff)

	t.Run("wrong length", func(t *testing.T) {
		short := gBytes[:127]
		exec, err := runByteCode(t, []Instruction{pushBytes(gBytes), pushBytes(short), {Op: OpBn254G2Add}})
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, exec.ResultCode)
		got, err := exec.ReturnValue.AsBytes()
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("non-canonical coordinate >= p", func(t *testing.T) {
		p := fpModulusBytesForTest(t)
		bad := append([]byte(nil), gBytes...)
		copy(bad[0:32], p) // X.A0 = p, not canonical (must be < p)
		exec, err := runByteCode(t, []Instruction{pushBytes(gBytes), pushBytes(bad), {Op: OpBn254G2Add}})
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, exec.ResultCode)
		got, err := exec.ReturnValue.AsBytes()
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("off curve", func(t *testing.T) {
		bad := append([]byte(nil), gBytes...)
		bad[127] ^= 0x01 // tweak the low byte of Y.A1: overwhelmingly unlikely to remain a curve point
		exec, err := runByteCode(t, []Instruction{pushBytes(gBytes), pushBytes(bad), {Op: OpBn254G2Add}})
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, exec.ResultCode)
		got, err := exec.ReturnValue.AsBytes()
		require.NoError(t, err)
		require.Empty(t, got)
	})
}

// TestAVMBn254G2OutOfSubgroupSoftFails closes the one acknowledged gap named
// in docs/architecture/avm-phase-d-zk-design.md's Stage 2 note: "no test
// constructs a genuine on-curve-but-out-of-subgroup G2 point ... deferred".
// gnark-crypto's own test suite builds such vectors via unexported
// internal/fptower helpers this (external) module cannot import -- but
// gnark-crypto's EXPORTED bn254.MapToCurve2 (the pre-cofactor-clearing step
// of its SVDW hash-to-curve map, explicitly documented as NOT performing
// cofactor clearing) lands on-curve while, for any fixed input, landing
// off-subgroup with overwhelming probability (BN254's G2 cofactor is
// astronomically large relative to the r-order subgroup) -- confirmed for
// this exact input via the sanity assertions below before it is fed
// adversarially into the opcode, proving bn254DecodeG2's mandatory
// IsInSubGroup() call (the blocker-1 fix) actually fires and rejects it,
// not merely that it is present in source.
func TestAVMBn254G2OutOfSubgroupSoftFails(t *testing.T) {
	var u bn254.E2
	u.A0.SetUint64(12345)
	badPoint := bn254.MapToCurve2(&u)
	require.True(t, badPoint.IsOnCurve(), "sanity: MapToCurve2 output must be on-curve")
	require.False(t, badPoint.IsInSubGroup(), "sanity: MapToCurve2 output must NOT be in the r-order subgroup")

	_, _, _, g2Aff := bn254.Generators()
	gBytes := bn254EncodeG2(g2Aff)
	badBytes := bn254EncodeG2(badPoint)

	t.Run("OpBn254G2Add rejects an out-of-subgroup operand", func(t *testing.T) {
		exec, err := runByteCode(t, []Instruction{pushBytes(gBytes), pushBytes(badBytes), {Op: OpBn254G2Add}})
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, exec.ResultCode)
		got, err := exec.ReturnValue.AsBytes()
		require.NoError(t, err)
		require.Empty(t, got, "an on-curve-but-out-of-subgroup G2 point must soft-fail to empty bytes")
	})

	t.Run("OpBn254PairingCheck rejects an out-of-subgroup g2 record", func(t *testing.T) {
		g1Aff, _ := generatorsForTest(t)
		g1Bytes := bn254EncodeG1(g1Aff)
		got, err := runtimeBn254PairingCheck(ValueBytes(g1Bytes), ValueBytes(badBytes), 1)
		require.NoError(t, err, "an out-of-subgroup g2 record must soft-fail, not trap")
		require.False(t, got)
	})
}

// fpModulusBytesForTest returns the 32-byte big-endian encoding of the
// BN254 base field modulus p, used to construct a non-canonical (>= p)
// coordinate for the malformed-input test above.
func fpModulusBytesForTest(t *testing.T) []byte {
	t.Helper()
	// BN254 base field modulus (decimal), per gnark-crypto's fp package.
	p, ok := new(big.Int).SetString("21888242871839275222246405745257275088696311157297823662689037894645226208583", 10)
	require.True(t, ok)
	buf := make([]byte, 32)
	p.FillBytes(buf)
	return buf
}

// --- OpBn254PairingCheck ---

// TestAVMBn254PairingCheckKnownVector uses the bilinearity identity
// e(P,Q)*e(P,-Q) = e(P,Q)*e(P,Q)^-1 = 1: pairing (G1,G2) against (G1,-G2)
// (k=2) must check TRUE, and a lone (G1,G2) pair (k=1) must check FALSE
// (since e(G1,G2) != 1 for the nontrivial generators).
func TestAVMBn254PairingCheckKnownVector(t *testing.T) {
	g1Aff, g2Aff := generatorsForTest(t)
	var negG2 bn254.G2Affine
	negG2.Neg(&g2Aff)

	g1Bytes := bn254EncodeG1(g1Aff)
	g2Bytes := bn254EncodeG2(g2Aff)
	negG2Bytes := bn254EncodeG2(negG2)

	t.Run("k=2 balanced pair checks true", func(t *testing.T) {
		g1s := append(append([]byte(nil), g1Bytes...), g1Bytes...)
		g2s := append(append([]byte(nil), g2Bytes...), negG2Bytes...)
		// g2s is 256 bytes, over OpPushBytes's 128-byte inline literal cap
		// (MaxKeySize) -- build it on the stack via OpConcat chunking, same
		// as a real contract would have to for any blob bigger than one
		// literal.
		code := append(pushLargeBytes(g1s), pushLargeBytes(g2s)...)
		code = append(code, pushU256(t, big.NewInt(2))...)
		code = append(code, Instruction{Op: OpBn254PairingCheck})
		exec, err := runByteCodeGas(t, code, 300_000)
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, exec.ResultCode)
		got, err := exec.ReturnValue.AsBool()
		require.NoError(t, err)
		require.True(t, got, "e(G1,G2)*e(G1,-G2) must check true")
		require.GreaterOrEqual(t, exec.GasUsed, uint64(45_000+2*(34_000+6_000)))
	})

	t.Run("k=1 lone pair checks false", func(t *testing.T) {
		code := []Instruction{pushBytes(g1Bytes), pushBytes(g2Bytes)}
		code = append(code, pushU256(t, big.NewInt(1))...)
		code = append(code, Instruction{Op: OpBn254PairingCheck})
		exec, err := runByteCodeGas(t, code, 300_000)
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, exec.ResultCode)
		got, err := exec.ReturnValue.AsBool()
		require.NoError(t, err)
		require.False(t, got, "a lone nontrivial pairing must not check true")
	})

	// k=0 is NOT treated as the vacuously-true empty product: gnark-crypto's
	// own MillerLoop explicitly rejects n==0 as an "invalid inputs sizes"
	// error (it is not a no-op/identity case in the library's own
	// convention), and runtimeBn254PairingCheck converts that error into a
	// soft-fail false rather than trapping, consistent with this opcode
	// family's never-trap convention. A k=0 call is best read as a
	// degenerate/malformed request, not a meaningful "verify nothing"
	// no-op, so soft-failing to false (never accepting a bogus/empty proof)
	// is the conservative, safe behavior here.
	t.Run("k=0 soft-fails false (gnark-crypto rejects an empty pairing)", func(t *testing.T) {
		code := []Instruction{pushBytes([]byte{}), pushBytes([]byte{})}
		code = append(code, pushU256(t, big.NewInt(0))...)
		code = append(code, Instruction{Op: OpBn254PairingCheck})
		exec, err := runByteCode(t, code)
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, exec.ResultCode)
		got, err := exec.ReturnValue.AsBool()
		require.NoError(t, err)
		require.False(t, got, "k=0 must soft-fail false, not trap or vacuously succeed")
	})
}

// TestAVMBn254PairingCheckHardCapAndLengthMismatchSoftFail proves k > 16 is
// a hard, consensus-critical cap (soft-fails false WITHOUT even reading the
// point blobs' cost), and that a declared k mismatching the actual blob
// lengths also soft-fails false rather than trapping.
func TestAVMBn254PairingCheckHardCapAndLengthMismatchSoftFail(t *testing.T) {
	t.Run("k=17 exceeds the hard cap", func(t *testing.T) {
		k := big.NewInt(17)
		out, ok, err := runtimeBn254PairingCheckCount(ValueBigInt256(k))
		require.NoError(t, err)
		require.False(t, ok)
		require.Zero(t, out)
	})

	t.Run("k=16 is exactly at the cap", func(t *testing.T) {
		k := big.NewInt(16)
		out, ok, err := runtimeBn254PairingCheckCount(ValueBigInt256(k))
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, uint64(16), out)
	})

	t.Run("negative k soft-fails", func(t *testing.T) {
		neg, err := runtimeFromBigIntChecked(TagInt256, big.NewInt(-1))
		require.NoError(t, err)
		_, ok, err := runtimeBn254PairingCheckCount(neg)
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("length mismatch against k soft-fails false", func(t *testing.T) {
		g1Aff, g2Aff := generatorsForTest(t)
		g1Bytes := bn254EncodeG1(g1Aff)
		g2Bytes := bn254EncodeG2(g2Aff)
		// k=1 but g1s is short one byte.
		got, err := runtimeBn254PairingCheck(ValueBytes(g1Bytes[:len(g1Bytes)-1]), ValueBytes(g2Bytes), 1)
		require.NoError(t, err)
		require.False(t, got)
	})
}

// --- OpPoseidon2Bn254 ---

// TestAVMPoseidon2Bn254MatchesLibraryHasher cross-checks the opcode against
// gnark-crypto's own canonical POSEIDON2_BN254 construction
// (NewMerkleDamgardHasher), the same one runtimePoseidon2Bn254 wraps -- this
// pins the opcode's byte-for-byte wiring (stack order, block-size chunking,
// digest output), not an independent third-party vector (none is
// established for gnark-crypto's Poseidon2-BN254 parameterization at this
// library version).
func TestAVMPoseidon2Bn254MatchesLibraryHasher(t *testing.T) {
	// 3 field elements: each 32-byte big-endian block is all-zero except a
	// distinct low-order byte (1, 2, 3) -- trivially canonical (far below
	// the ~2^254 Fr modulus, which starts with byte 0x30) unlike naively
	// filling every byte position with an incrementing value, which would
	// make the LEADING byte large and risk landing >= the modulus.
	elems := make([]byte, 96)
	elems[31] = 1
	elems[63] = 2
	elems[95] = 3

	hasher := poseidon2.NewMerkleDamgardHasher()
	for i := 0; i < len(elems); i += 32 {
		_, err := hasher.Write(elems[i : i+32])
		require.NoError(t, err)
	}
	want := hasher.Sum(nil)
	require.Len(t, want, 32)

	code := []Instruction{pushBytes(elems)}
	code = append(code, pushU256(t, big.NewInt(3))...)
	code = append(code, Instruction{Op: OpPoseidon2Bn254})
	exec, err := runByteCode(t, code)
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	got, err := exec.ReturnValue.AsBytes()
	require.NoError(t, err)
	require.Equal(t, want, got)
	require.GreaterOrEqual(t, exec.GasUsed, uint64(300+3*1_200))
}

// TestAVMPoseidon2Bn254EmptyInput proves n=0 is well-defined (the hasher's
// initial state, never absorbing any block), not a trap.
func TestAVMPoseidon2Bn254EmptyInput(t *testing.T) {
	code := []Instruction{pushBytes([]byte{})}
	code = append(code, pushU256(t, big.NewInt(0))...)
	code = append(code, Instruction{Op: OpPoseidon2Bn254})
	exec, err := runByteCode(t, code)
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	got, err := exec.ReturnValue.AsBytes()
	require.NoError(t, err)
	require.Equal(t, make([]byte, 32), got)
}

// TestAVMPoseidon2Bn254MalformedLengthTraps is the regression pinning this
// opcode's one deliberate departure from the rest of the family: unlike
// every point opcode (which soft-fails malformed input), a length that does
// not match n TRAPS, because there is no "invalid point" failure mode for a
// plain hash -- just a length precondition, per the design doc's own
// reasoning.
func TestAVMPoseidon2Bn254MalformedLengthTraps(t *testing.T) {
	code := []Instruction{pushBytes(make([]byte, 31))} // not a multiple of 32
	code = append(code, pushU256(t, big.NewInt(1))...)
	code = append(code, Instruction{Op: OpPoseidon2Bn254})
	exec, err := runByteCode(t, code)
	require.Error(t, err, "a length mismatched against n must trap")
	require.Equal(t, async.ResultExecutionFailed, exec.ResultCode)
}

// TestAVMPoseidon2Bn254NonCanonicalElementTraps proves a 32-byte chunk >=
// the BN254 SCALAR field modulus r (non-canonical) also traps, rather than
// being silently reduced mod r or soft-failing -- there is no safe soft-fail
// sentinel for a hash primitive's fixed 32-byte output.
func TestAVMPoseidon2Bn254NonCanonicalElementTraps(t *testing.T) {
	r := fr.Modulus()
	buf := make([]byte, 32)
	r.FillBytes(buf) // exactly r: non-canonical (must be < r)

	code := []Instruction{pushBytes(buf)}
	code = append(code, pushU256(t, big.NewInt(1))...)
	code = append(code, Instruction{Op: OpPoseidon2Bn254})
	exec, err := runByteCode(t, code)
	require.Error(t, err, "a non-canonical scalar-field element must trap")
	require.Equal(t, async.ResultExecutionFailed, exec.ResultCode)
}

// --- shared test helpers ---

func generatorsForTest(t *testing.T) (bn254.G1Affine, bn254.G2Affine) {
	t.Helper()
	_, _, g1Aff, g2Aff := bn254.Generators()
	return g1Aff, g2Aff
}

// pushLargeBytes builds instructions that leave b on the stack, chunking via
// OpPushBytes+OpConcat when b exceeds MaxKeySize (128) bytes -- a single
// OpPushBytes literal cannot be longer than that (enforced at both dispatch
// and encode time), so any bigger blob (e.g. a packed multi-record
// OpBn254PairingCheck operand) has to be assembled on the stack the same way
// a real contract would have to.
func pushLargeBytes(b []byte) []Instruction {
	const chunkSize = MaxKeySize
	if len(b) <= chunkSize {
		return []Instruction{pushBytes(b)}
	}
	code := []Instruction{pushBytes(b[:chunkSize])}
	rest := b[chunkSize:]
	for len(rest) > 0 {
		n := chunkSize
		if n > len(rest) {
			n = len(rest)
		}
		code = append(code, pushBytes(rest[:n]), Instruction{Op: OpConcat})
		rest = rest[n:]
	}
	return code
}

// runByteCodeGas mirrors runByteCode but with an overridable gas limit, for
// opcodes (like OpBn254PairingCheck at k>1) whose real cost exceeds
// runtimeCtx's default 100_000 test budget.
func runByteCodeGas(t *testing.T, code []Instruction, gasLimit uint64) (Execution, error) {
	t.Helper()
	runner := newTestRunner(t)
	module := Module{
		Version: Version,
		Exports: map[Entrypoint]uint32{EntryQuery: 0},
		Code:    code,
	}
	ctx := runtimeCtx(EntryQuery)
	ctx.GasLimit = gasLimit
	return runner.Run(module, nil, ctx)
}
