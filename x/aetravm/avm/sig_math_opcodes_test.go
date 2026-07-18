package avm

import (
	"crypto/ed25519"
	"crypto/sha256"
	"math/big"
	"testing"

	secp256k1 "github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"github.com/hdevalence/ed25519consensus"
	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/aetravm/async"
)

// pushU256 builds the instruction pair that leaves the 256-bit value v on the
// stack: a 32-byte big-endian literal decoded back to uint256 with OpFromBytesBE
// (there is no OpPush for values wider than u64, and mulDiv needs operands whose
// product exceeds u256).
func pushU256(t *testing.T, v *big.Int) []Instruction {
	t.Helper()
	require.False(t, v.Sign() < 0, "pushU256 requires a non-negative value")
	require.LessOrEqual(t, v.BitLen(), 256, "pushU256 requires a value that fits 256 bits")
	buf := make([]byte, 32)
	v.FillBytes(buf)
	return []Instruction{pushBytes(buf), {Op: OpFromBytesBE}}
}

func mulDivCode(t *testing.T, a, b, c *big.Int, roundUp bool) []Instruction {
	code := append([]Instruction(nil), pushU256(t, a)...)
	code = append(code, pushU256(t, b)...)
	code = append(code, pushU256(t, c)...)
	op := OpMulDiv
	if roundUp {
		op = OpMulDivRoundUp
	}
	return append(code, Instruction{Op: op})
}

// TestAVMMulDivFullWidth proves mulDiv forms the a*b product at full width: with
// a = b = 2^200 the product is 2^400 (far past u256), yet a*b/c = 2^240 fits and
// is returned exactly with NO trap. A plain checked u256 multiply would trap on
// the 2^400 intermediate — this is the whole point of mulDiv for the AMM.
func TestAVMMulDivFullWidth(t *testing.T) {
	a := new(big.Int).Lsh(big.NewInt(1), 200)
	b := new(big.Int).Lsh(big.NewInt(1), 200)
	c := new(big.Int).Lsh(big.NewInt(1), 160)
	want := new(big.Int).Lsh(big.NewInt(1), 240) // 2^400 / 2^160

	prod := new(big.Int).Mul(a, b)
	require.Equal(t, 1, prod.Cmp(new(big.Int).Lsh(big.NewInt(1), 256)), "sanity: a*b must exceed 2^256")

	exec, err := runByteCode(t, mulDivCode(t, a, b, c, false))
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	got, err := exec.ReturnValue.AsBigInt()
	require.NoError(t, err)
	require.Equal(t, 0, got.Cmp(want), "mulDiv(2^200, 2^200, 2^160) must be 2^240")
}

// TestAVMMulDivFloorAndRoundUp pins the rounding semantics: mulDiv floors,
// mulDivRoundUp ceils, and an exact division adds nothing.
func TestAVMMulDivFloorAndRoundUp(t *testing.T) {
	cases := []struct {
		a, b, c     int64
		wantFloor   int64
		wantRoundUp int64
	}{
		{7, 3, 2, 10, 11}, // 21/2 = 10.5
		{6, 2, 3, 4, 4},   // 12/3 = 4 exactly
		{100, 100, 3, 3333, 3334},
	}
	for _, tc := range cases {
		a, b, c := big.NewInt(tc.a), big.NewInt(tc.b), big.NewInt(tc.c)

		floorExec, err := runByteCode(t, mulDivCode(t, a, b, c, false))
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, floorExec.ResultCode)
		floor, err := floorExec.ReturnValue.AsBigInt()
		require.NoError(t, err)
		require.Equal(t, tc.wantFloor, floor.Int64())

		ceilExec, err := runByteCode(t, mulDivCode(t, a, b, c, true))
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, ceilExec.ResultCode)
		ceil, err := ceilExec.ReturnValue.AsBigInt()
		require.NoError(t, err)
		require.Equal(t, tc.wantRoundUp, ceil.Int64())
	}
}

// TestAVMMulDivByZeroTraps: a zero divisor is a deterministic trap (rollback),
// the same as an ordinary integer divide-by-zero.
func TestAVMMulDivByZeroTraps(t *testing.T) {
	exec, err := runByteCode(t, mulDivCode(t, big.NewInt(5), big.NewInt(7), big.NewInt(0), false))
	require.Error(t, err)
	// Stage 1(c): mulDiv-by-zero now threads the specific, already-defined
	// async.ResultDivisionByZero code instead of the generic
	// ResultExecutionFailed (indistinguishable from an ordinary div/mod by
	// zero, by design -- see runtimeMulDiv's doc comment).
	require.Equal(t, async.ResultDivisionByZero, exec.ResultCode)
}

// TestAVMMulDivResultOverflowTraps: when the QUOTIENT itself exceeds u256 (here
// 2^255 * 4 / 1 = 2^257), the checked narrow traps rather than wrapping. The
// trap is classified as async.ResultOutOfRange (Stage 6: enforceIntWidth's
// overflow path is wrapped in errIntegerOutOfRange and recognized by
// arithResultCode), not the generic async.ResultExecutionFailed.
func TestAVMMulDivResultOverflowTraps(t *testing.T) {
	a := new(big.Int).Lsh(big.NewInt(1), 255)
	exec, err := runByteCode(t, mulDivCode(t, a, big.NewInt(4), big.NewInt(1), false))
	require.Error(t, err)
	require.Equal(t, async.ResultOutOfRange, exec.ResultCode)
}

func mulDivNearestCode(t *testing.T, a, b, c *big.Int) []Instruction {
	code := append([]Instruction(nil), pushU256(t, a)...)
	code = append(code, pushU256(t, b)...)
	code = append(code, pushU256(t, c)...)
	return append(code, Instruction{Op: OpMulDivNearest})
}

// TestAVMMulDivNearestRoundsHalfUp pins mulDivNearest's rounding rule: round
// UP iff the true remainder, doubled, is >= the divisor (exact half rounds
// up), otherwise floor -- distinct from mulDiv (always floors) and
// mulDivRoundUp (ceils on ANY nonzero remainder).
func TestAVMMulDivNearestRoundsHalfUp(t *testing.T) {
	cases := []struct {
		a, b, c int64
		want    int64
	}{
		{7, 3, 2, 11},   // 21/2 = 10.5 -> exact half rounds UP to 11
		{6, 2, 3, 4},    // 12/3 = 4 exactly -> 4
		{100, 100, 3, 3333},  // 10000/3 = 3333.33 -> floor to 3333
		{100, 101, 3, 3367},  // 10100/3 = 3366.67 -> rounds UP to 3367
		{5, 1, 2, 3},    // 5/2 = 2.5 -> exact half rounds UP to 3
		{1, 1, 3, 0},    // 1/3 = 0.33 -> floor to 0
	}
	for _, tc := range cases {
		a, b, c := big.NewInt(tc.a), big.NewInt(tc.b), big.NewInt(tc.c)
		exec, err := runByteCode(t, mulDivNearestCode(t, a, b, c))
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, exec.ResultCode)
		got, err := exec.ReturnValue.AsBigInt()
		require.NoError(t, err)
		require.Equal(t, tc.want, got.Int64(), "mulDivNearest(%d,%d,%d)", tc.a, tc.b, tc.c)
	}
}

// TestAVMMulDivNearestFullWidth proves mulDivNearest also forms the a*b
// product at unbounded width, exactly like mulDiv/mulDivRoundUp.
func TestAVMMulDivNearestFullWidth(t *testing.T) {
	a := new(big.Int).Lsh(big.NewInt(1), 200)
	b := new(big.Int).Lsh(big.NewInt(1), 200)
	c := new(big.Int).Lsh(big.NewInt(1), 160)
	want := new(big.Int).Lsh(big.NewInt(1), 240) // exact, no rounding needed

	exec, err := runByteCode(t, mulDivNearestCode(t, a, b, c))
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	got, err := exec.ReturnValue.AsBigInt()
	require.NoError(t, err)
	require.Equal(t, 0, got.Cmp(want), "mulDivNearest(2^200, 2^200, 2^160) must be 2^240")
}

// TestAVMMulDivNearestByZeroTraps mirrors TestAVMMulDivByZeroTraps: a zero
// divisor traps with the specific async.ResultDivisionByZero code.
func TestAVMMulDivNearestByZeroTraps(t *testing.T) {
	exec, err := runByteCode(t, mulDivNearestCode(t, big.NewInt(5), big.NewInt(7), big.NewInt(0)))
	require.Error(t, err)
	require.Equal(t, async.ResultDivisionByZero, exec.ResultCode)
}

// TestAVMMulDivNearestResultOverflowTraps mirrors
// TestAVMMulDivResultOverflowTraps: a quotient that overflows u256 traps,
// classified as async.ResultOutOfRange (Stage 6), not the generic
// async.ResultExecutionFailed.
func TestAVMMulDivNearestResultOverflowTraps(t *testing.T) {
	a := new(big.Int).Lsh(big.NewInt(1), 255)
	exec, err := runByteCode(t, mulDivNearestCode(t, a, big.NewInt(4), big.NewInt(1)))
	require.Error(t, err)
	require.Equal(t, async.ResultOutOfRange, exec.ResultCode)
}

// TestAVMBitwiseWidthCheckTraps is the regression for Stage 1(b): OpBitAnd/
// OpBitOr/OpBitXor used to skip the width check every other binary arithmetic
// op (+,-,*,/,%,shl) applies, so a result computed from MISMATCHED-tag
// operands -- reachable because the verifier performs no stack-type-
// consistency analysis across a binary op's two operands -- could silently
// WRAP into a different, wrong, in-range value instead of trapping.
// Confirmed concretely before this fix: XOR(int8(-1), int256(10000))
// mathematically equals -10001 (outside int8's [-128,127] range), but
// runtimeFromBigInt's unchecked int8(v.Int64()) cast silently produced -17
// instead of trapping -- real value corruption, not just a theoretical gap.
func TestAVMBitwiseWidthCheckTraps(t *testing.T) {
	left := ValueInt8(-1)
	right := ValueBigInt256(big.NewInt(10000))

	_, err := runtimeBinaryArithmetic(OpBitXor, left, right)
	require.Error(t, err, "XOR of mismatched-width operands must trap, not silently wrap")

	// Same-width, same-signedness operands never trip this: the two's-
	// complement identity keeps a matched-width bitwise result in range, so
	// the new check adds no new failure mode for well-typed programs.
	same, err := runtimeBinaryArithmetic(OpBitXor, ValueInt8(-1), ValueInt8(5))
	require.NoError(t, err)
	got, err := same.AsBigInt()
	require.NoError(t, err)
	require.Equal(t, int64(-6), got.Int64(), "XOR(int8(-1), int8(5)) = -6, matched widths never trap")
}

// TestAVMArithResultCodeThreading proves Stage 1(c): ordinary integer div/mod
// by zero and an invalid shift amount now thread their specific,
// already-defined async.Result* codes instead of the generic
// ResultExecutionFailed every arithmetic trap used to report.
func TestAVMArithResultCodeThreading(t *testing.T) {
	t.Run("div by zero", func(t *testing.T) {
		exec, err := runByteCode(t, []Instruction{pushU64(7), pushU64(0), {Op: OpDiv}})
		require.Error(t, err)
		require.Equal(t, async.ResultDivisionByZero, exec.ResultCode)
	})
	t.Run("mod by zero", func(t *testing.T) {
		exec, err := runByteCode(t, []Instruction{pushU64(7), pushU64(0), {Op: OpMod}})
		require.Error(t, err)
		require.Equal(t, async.ResultDivisionByZero, exec.ResultCode)
	})
	t.Run("invalid shift amount", func(t *testing.T) {
		exec, err := runByteCode(t, []Instruction{pushU64(1), pushU64(4097), {Op: OpShl}})
		require.Error(t, err)
		require.Equal(t, async.ResultInvalidShift, exec.ResultCode)
	})
}

// --- secp256k1 test vectors: a fixed private scalar signs a fixed digest.
// decred's ECDSA is RFC6979-deterministic, so these are stable. ---

func secpVectors(t *testing.T) (digest [32]byte, pub33, sig64, ethSig65, highS64, wantXY []byte) {
	t.Helper()
	privBytes := make([]byte, 32)
	for i := range privBytes {
		privBytes[i] = byte(0x01 + i)
	}
	priv := secp256k1.PrivKeyFromBytes(privBytes)
	pub := priv.PubKey()
	pub33 = pub.SerializeCompressed()
	wantXY = pub.SerializeUncompressed()[1:] // 64-byte X‖Y

	digest = sha256.Sum256([]byte("aetra secp256k1 opcode vector"))
	compact := ecdsa.SignCompact(priv, digest[:], false) // [27+recid ‖ R ‖ S], low-S
	sig64 = append([]byte(nil), compact[1:]...)
	ethSig65 = append(append([]byte(nil), sig64...), compact[0]) // R‖S‖(27+recid), Ethereum layout

	// High-S malleation: S' = N - S.
	var s secp256k1.ModNScalar
	s.SetByteSlice(sig64[32:64])
	s.Negate()
	var sBytes [32]byte
	s.PutBytes(&sBytes)
	highS64 = append(append([]byte(nil), sig64[:32]...), sBytes[:]...)
	return
}

func TestAVMVerifySecp256k1(t *testing.T) {
	digest, pub33, sig64, _, highS64, _ := secpVectors(t)

	verify := func(d, sig, pub []byte) (bool, Execution) {
		exec, err := runByteCode(t, []Instruction{pushBytes(d), pushBytes(sig), pushBytes(pub), {Op: OpVerifySecp256k1}})
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, exec.ResultCode)
		b, err := exec.ReturnValue.AsBool()
		require.NoError(t, err)
		return b, exec
	}

	// Valid canonical low-S signature accepts.
	ok, exec := verify(digest[:], sig64, pub33)
	require.True(t, ok, "a valid low-S signature must verify")
	require.GreaterOrEqual(t, exec.GasUsed, uint64(6000), "verifySecp256k1 must charge its flat base gas")

	// High-S (malleated) signature is a canonical REJECT on every node.
	badS, _ := verify(digest[:], highS64, pub33)
	require.False(t, badS, "a high-S (malleated) signature must be rejected")

	// Tampered digest rejects.
	other := sha256.Sum256([]byte("different message"))
	tampered, _ := verify(other[:], sig64, pub33)
	require.False(t, tampered, "a signature over a different digest must be rejected")

	// Uncompressed (65-byte) key is rejected up front for determinism.
	privBytes := make([]byte, 32)
	for i := range privBytes {
		privBytes[i] = byte(0x01 + i)
	}
	uncompressed := secp256k1.PrivKeyFromBytes(privBytes).PubKey().SerializeUncompressed()
	badKey, _ := verify(digest[:], sig64, uncompressed)
	require.False(t, badKey, "a non-33-byte public key must be rejected")

	// Malformed signature length soft-fails rather than trapping.
	shortSig, _ := verify(digest[:], sig64[:63], pub33)
	require.False(t, shortSig)
}

func TestAVMEcrecover(t *testing.T) {
	digest, _, _, ethSig65, _, wantXY := secpVectors(t)

	recover := func(d, sig []byte) ([]byte, Execution) {
		exec, err := runByteCode(t, []Instruction{pushBytes(d), pushBytes(sig), {Op: OpEcrecover}})
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, exec.ResultCode)
		b, err := exec.ReturnValue.AsBytes()
		require.NoError(t, err)
		return b, exec
	}

	// Recovers the correct 64-byte uncompressed X‖Y public-key body.
	got, exec := recover(digest[:], ethSig65)
	require.Equal(t, wantXY, got, "ecrecover must recover the signer's public key")
	require.GreaterOrEqual(t, exec.GasUsed, uint64(8000), "ecrecover must charge its flat base gas")

	// Cross-check against the decred library directly.
	decredCompact := append([]byte{ethSig65[64]}, ethSig65[:64]...)
	libPub, _, err := ecdsa.RecoverCompact(decredCompact, digest[:])
	require.NoError(t, err)
	require.Equal(t, libPub.SerializeUncompressed()[1:], got)

	// v=27 (recid 0) must also be accepted (normalized).
	altV := append(append([]byte(nil), ethSig65[:64]...), 27)
	// Only assert it does not trap and returns 64 or 0 bytes deterministically.
	altOut, _ := recover(digest[:], altV)
	require.True(t, len(altOut) == 64 || len(altOut) == 0, "ecrecover must return a well-formed result for v=27")

	// Malformed length soft-fails to empty bytes (never traps).
	short, _ := recover(digest[:], ethSig65[:64])
	require.Len(t, short, 0, "a 64-byte (non-recoverable) signature must soft-fail to empty")
}

func isqrtCode(t *testing.T, x *big.Int) []Instruction {
	code := append([]Instruction(nil), pushU256(t, x)...)
	return append(code, Instruction{Op: OpIsqrt})
}

// TestAVMIsqrt proves the integer square root floors correctly, handles the
// boundary values (0, 1, perfect squares), and stays exact at full 256-bit
// width. The constant-product AMM mints initial LP shares as
// isqrt(reserveA*reserveB) (Uniswap-V2 style), so both the floor semantics and
// the wide-input exactness matter.
func TestAVMIsqrt(t *testing.T) {
	run := func(x *big.Int) (*big.Int, Execution) {
		exec, err := runByteCode(t, isqrtCode(t, x))
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, exec.ResultCode)
		got, err := exec.ReturnValue.AsBigInt()
		require.NoError(t, err)
		return got, exec
	}

	small := []struct{ in, want int64 }{
		{0, 0}, {1, 1}, {2, 1}, {3, 1}, {4, 2}, {8, 2}, {9, 3},
		{15, 3}, {16, 4}, {99, 9}, {100, 10}, {144, 12},
	}
	for _, tc := range small {
		got, exec := run(big.NewInt(tc.in))
		require.Equal(t, tc.want, got.Int64(), "isqrt(%d)", tc.in)
		require.GreaterOrEqual(t, exec.GasUsed, uint64(30), "isqrt must charge its flat base gas")
	}

	// 10^18 = (10^9)^2 is a perfect square at the AET/naet scale.
	e18 := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	got, _ := run(e18)
	require.Equal(t, 0, got.Cmp(new(big.Int).Exp(big.NewInt(10), big.NewInt(9), nil)), "isqrt(10^18)=10^9")

	// (2^128-1)^2 fits u256; its root is exactly 2^128-1.
	max128 := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 128), big.NewInt(1))
	sq := new(big.Int).Mul(max128, max128)
	got, _ = run(sq)
	require.Equal(t, 0, got.Cmp(max128), "isqrt((2^128-1)^2)=2^128-1")

	// One below that square floors to 2^128-2.
	got, _ = run(new(big.Int).Sub(sq, big.NewInt(1)))
	require.Equal(t, 0, got.Cmp(new(big.Int).Sub(max128, big.NewInt(1))), "isqrt((2^128-1)^2 - 1) floors")

	// isqrt(2^256-1) = 2^128 - 1, the largest representable root.
	max256 := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	got, _ = run(max256)
	require.Equal(t, 0, got.Cmp(max128), "isqrt(2^256-1)=2^128-1")
}

// TestAVMIsqrtNegativeTraps is the regression for the audit finding on OpIsqrt
// (0x55): a negative SIGNED operand is reachable in a validated program (the
// verifier does no stack-type analysis, so isqrt can be fed int256(-1) via OpNeg
// or a signed message field), and big.Int.Sqrt PANICS on a negative value. The
// guard must instead TRAP with a returned error, exactly like mulDiv's zero
// divisor, so the Run loop can never panic on this opcode.
func TestAVMIsqrtNegativeTraps(t *testing.T) {
	// int256(-1) is a constructible signed operand: enforceIntWidth permits
	// negatives for signed tags, which is precisely why the guard is required.
	neg, err := runtimeFromBigIntChecked(TagInt256, big.NewInt(-1))
	require.NoError(t, err)
	got, err := neg.AsBigInt()
	require.NoError(t, err)
	require.Equal(t, -1, got.Sign(), "sanity: the operand is negative")

	require.NotPanics(t, func() {
		_, ierr := runtimeIsqrt(neg)
		require.Error(t, ierr, "isqrt(negative) must trap with an error, not panic")
	})
}

// TestAVMEd25519ConsensusVerify proves the ed25519 opcode (now backed by
// ed25519consensus / ZIP-215) still verifies an ordinary stdlib-produced
// signature and rejects a tampered one, so the verification-set change did not
// break standard signatures.
func TestAVMEd25519ConsensusVerify(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(0xA0 + i)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	msg := []byte("aetra ed25519 zip215 vector")
	sig := ed25519.Sign(priv, msg)
	require.True(t, ed25519consensus.Verify(pub, msg, sig), "sanity: ZIP-215 accepts the stdlib signature")

	verify := func(m, s, p []byte) bool {
		exec, err := runByteCode(t, []Instruction{pushBytes(m), pushBytes(s), pushBytes(p), {Op: OpVerifySignature}})
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, exec.ResultCode)
		b, err := exec.ReturnValue.AsBool()
		require.NoError(t, err)
		return b
	}

	require.True(t, verify(msg, sig, pub), "ed25519 (ZIP-215) must accept a valid signature")

	tampered := append([]byte(nil), sig...)
	tampered[0] ^= 0x01
	require.False(t, verify(msg, tampered, pub), "ed25519 (ZIP-215) must reject a tampered signature")
}
