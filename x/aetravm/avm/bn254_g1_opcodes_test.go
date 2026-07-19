package avm

import (
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc/bn254"
	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/aetravm/async"
)

// --- BN254 G1 test vectors: derived directly from gnark-crypto's own
// exported Generators(), mirroring bn254_g2_opcodes_test.go's G2 coverage.
// OpBn254G1Add/OpBn254G1ScalarMul/OpBn254G1IsOnCurve previously had zero
// dedicated test coverage in this repo (bn254EncodeG1/bn254DecodeG1 were
// exercised only incidentally as helpers building operands for G2/pairing
// tests, never through the G1 opcodes themselves) despite sharing the exact
// same decode/validate/soft-fail contract as the already-tested G2 family. ---

func TestAVMBn254G1AddKnownVector(t *testing.T) {
	g1Aff, _ := generatorsForTest(t)
	require.True(t, g1Aff.IsOnCurve(), "sanity: generator is on curve")
	gBytes := bn254EncodeG1(g1Aff)
	require.Len(t, gBytes, bn254G1PointSize)

	var want bn254.G1Affine
	want.Add(&g1Aff, &g1Aff) // 2G

	exec, err := runByteCode(t, []Instruction{pushBytes(gBytes), pushBytes(gBytes), {Op: OpBn254G1Add}})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	got, err := exec.ReturnValue.AsBytes()
	require.NoError(t, err)
	require.Equal(t, bn254EncodeG1(want), got, "bn254G1Add(G,G) must equal 2G")
	require.GreaterOrEqual(t, exec.GasUsed, uint64(500), "bn254G1Add must charge its GasSchedule entry")
}

func TestAVMBn254G1ScalarMulKnownVector(t *testing.T) {
	g1Aff, _ := generatorsForTest(t)
	gBytes := bn254EncodeG1(g1Aff)
	scalar := big.NewInt(7)

	var want bn254.G1Affine
	want.ScalarMultiplication(&g1Aff, scalar)

	code := []Instruction{pushBytes(gBytes)}
	code = append(code, pushU256(t, scalar)...)
	code = append(code, Instruction{Op: OpBn254G1ScalarMul})
	exec, err := runByteCode(t, code)
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	got, err := exec.ReturnValue.AsBytes()
	require.NoError(t, err)
	require.Equal(t, bn254EncodeG1(want), got, "bn254G1ScalarMul(G,7) must equal 7G")
	require.GreaterOrEqual(t, exec.GasUsed, uint64(6_000), "bn254G1ScalarMul must charge its GasSchedule entry")
}

// TestAVMBn254G1ScalarMulNegativeScalarSoftFails mirrors G2's convention: a
// negative scalar magnitude (reachable via signed tags, since the verifier
// does no stack-type analysis) soft-fails to empty bytes rather than
// trapping.
func TestAVMBn254G1ScalarMulNegativeScalarSoftFails(t *testing.T) {
	g1Aff, _ := generatorsForTest(t)
	point := ValueBytes(bn254EncodeG1(g1Aff))
	neg, err := runtimeFromBigIntChecked(TagInt256, big.NewInt(-1))
	require.NoError(t, err)

	out, err := runtimeBn254G1ScalarMul(point, neg)
	require.NoError(t, err, "a negative scalar must soft-fail, not trap")
	require.Empty(t, out)
}

// TestAVMBn254G1InfinityIsAccepted proves the all-zero 64-byte encoding
// decodes as the valid identity element with no AVM-side special-casing
// (gnark-crypto's own IsOnCurve() already accepts (0,0)).
func TestAVMBn254G1InfinityIsAccepted(t *testing.T) {
	g1Aff, _ := generatorsForTest(t)
	gBytes := bn254EncodeG1(g1Aff)
	infinity := make([]byte, bn254G1PointSize)

	exec, err := runByteCode(t, []Instruction{pushBytes(gBytes), pushBytes(infinity), {Op: OpBn254G1Add}})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	got, err := exec.ReturnValue.AsBytes()
	require.NoError(t, err)
	require.Equal(t, gBytes, got, "G + infinity must equal G")
}

// TestAVMBn254G1IsOnCurveKnownAnswers proves the standalone predicate opcode
// accepts a genuine point (and the identity) and rejects an off-curve one,
// mirroring the family's shared soft-fail (never-trap) contract.
func TestAVMBn254G1IsOnCurveKnownAnswers(t *testing.T) {
	g1Aff, _ := generatorsForTest(t)
	gBytes := bn254EncodeG1(g1Aff)
	infinity := make([]byte, bn254G1PointSize)

	t.Run("generator is on curve", func(t *testing.T) {
		exec, err := runByteCode(t, []Instruction{pushBytes(gBytes), {Op: OpBn254G1IsOnCurve}})
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, exec.ResultCode)
		got, err := exec.ReturnValue.AsBool()
		require.NoError(t, err)
		require.True(t, got)
	})

	t.Run("infinity is on curve", func(t *testing.T) {
		exec, err := runByteCode(t, []Instruction{pushBytes(infinity), {Op: OpBn254G1IsOnCurve}})
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, exec.ResultCode)
		got, err := exec.ReturnValue.AsBool()
		require.NoError(t, err)
		require.True(t, got)
	})

	t.Run("off curve is rejected", func(t *testing.T) {
		bad := append([]byte(nil), gBytes...)
		bad[63] ^= 0x01 // tweak the low byte of Y: overwhelmingly unlikely to remain a curve point
		exec, err := runByteCode(t, []Instruction{pushBytes(bad), {Op: OpBn254G1IsOnCurve}})
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, exec.ResultCode)
		got, err := exec.ReturnValue.AsBool()
		require.NoError(t, err)
		require.False(t, got)
	})
}

// TestAVMBn254G1MalformedInputSoftFails covers the non-exception malformed
// cases the opcode family shares: wrong length, a coordinate >= the base
// field modulus p (non-canonical, must be REJECTED not silently reduced),
// and a coordinate pair that does not satisfy the curve equation (off-curve).
// All soft-fail to empty bytes; none trap.
func TestAVMBn254G1MalformedInputSoftFails(t *testing.T) {
	g1Aff, _ := generatorsForTest(t)
	gBytes := bn254EncodeG1(g1Aff)

	t.Run("wrong length", func(t *testing.T) {
		short := gBytes[:63]
		exec, err := runByteCode(t, []Instruction{pushBytes(gBytes), pushBytes(short), {Op: OpBn254G1Add}})
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, exec.ResultCode)
		got, err := exec.ReturnValue.AsBytes()
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("non-canonical coordinate >= p", func(t *testing.T) {
		p := fpModulusBytesForTest(t)
		bad := append([]byte(nil), gBytes...)
		copy(bad[0:32], p) // X = p, not canonical (must be < p)
		exec, err := runByteCode(t, []Instruction{pushBytes(gBytes), pushBytes(bad), {Op: OpBn254G1Add}})
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, exec.ResultCode)
		got, err := exec.ReturnValue.AsBytes()
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("off curve", func(t *testing.T) {
		bad := append([]byte(nil), gBytes...)
		bad[63] ^= 0x01 // tweak the low byte of Y: overwhelmingly unlikely to remain a curve point
		exec, err := runByteCode(t, []Instruction{pushBytes(gBytes), pushBytes(bad), {Op: OpBn254G1Add}})
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, exec.ResultCode)
		got, err := exec.ReturnValue.AsBytes()
		require.NoError(t, err)
		require.Empty(t, got)
	})
}
