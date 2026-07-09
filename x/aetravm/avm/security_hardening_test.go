package avm

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCanonicalDecodeRejectsTupleDecodeBomb ensures a crafted 5-byte tuple with
// a huge declared element count is rejected with an error instead of forcing a
// multi-terabyte allocation (OOM / chain halt).
func TestCanonicalDecodeRejectsTupleDecodeBomb(t *testing.T) {
	bomb := []byte{byte(TagTuple), 0xFF, 0xFF, 0xFF, 0xFF}
	_, _, err := CanonicalDecode(bomb)
	require.Error(t, err)
	require.Contains(t, err.Error(), "tuple")
}

// TestCanonicalDecodeRejectsMapDecodeBomb ensures the map decoder bounds its
// declared entry count against the remaining input before pre-sizing.
func TestCanonicalDecodeRejectsMapDecodeBomb(t *testing.T) {
	bomb := []byte{byte(TagMap), 0xFF, 0xFF, 0xFF, 0xFF}
	_, _, err := CanonicalDecode(bomb)
	require.Error(t, err)
	require.Contains(t, err.Error(), "map")
}

// TestDecodeSnapshotRejectsCountBomb ensures the storage snapshot decoder bounds
// the declared entry count against the input size.
func TestDecodeSnapshotRejectsCountBomb(t *testing.T) {
	// count = 0xFFFFFFFF, then no entries.
	bomb := []byte{0xFF, 0xFF, 0xFF, 0xFF}
	_, err := DecodeSnapshot(bomb)
	require.Error(t, err)
}

// TestDecodeSnapshotRejectsValueLengthBomb ensures a single entry cannot declare
// a value length larger than the remaining input.
func TestDecodeSnapshotRejectsValueLengthBomb(t *testing.T) {
	// count=1, keyLen=1, key='a', valueLen=0xFFFFFFFF, (no value bytes)
	bomb := []byte{
		0x00, 0x00, 0x00, 0x01, // count = 1
		0x00, 0x01, // keyLen = 1
		'a',                    // key
		0xFF, 0xFF, 0xFF, 0xFF, // valueLen = huge
	}
	_, err := DecodeSnapshot(bomb)
	require.Error(t, err)
}

// TestArithmeticOverflowFailsClosed ensures wide-integer arithmetic that would
// grow the value beyond its type width returns an error instead of letting the
// big.Int magnitude grow unbounded (the memory-exhaustion vector).
func TestArithmeticOverflowFailsClosed(t *testing.T) {
	// (2^255) as a u256, squared, overflows 256 bits -> must error.
	big255 := new(big.Int).Lsh(big.NewInt(1), 255)
	v := RuntimeValue{Tag: TagUint256, intVal: big255}
	_, err := runtimeBinaryArithmetic(OpMul, v, v)
	require.Error(t, err)
	require.Contains(t, err.Error(), "overflow")
}

// TestSmallWidthIntegerMulTrapsOnOverflow is the regression guard for SEC-MED:
// sub-128-bit add/mul silently wraps. u8..u64 arithmetic that exceeds the type
// width must trap (fail-closed) rather than truncate modulo 2^width, which would
// let a contract's u64 balance/supply counter overflow undetected.
func TestSmallWidthIntegerMulTrapsOnOverflow(t *testing.T) {
	// u64: 2^40 * 2^40 = 2^80 overflows 64 bits.
	_, err := runtimeBinaryArithmetic(OpMul, ValueUint64(1<<40), ValueUint64(1<<40))
	require.Error(t, err)
	require.Contains(t, err.Error(), "overflow")

	// u8: 16 * 16 = 256 > 255.
	_, err = runtimeBinaryArithmetic(OpMul, ValueUint8(16), ValueUint8(16))
	require.Error(t, err)
	require.Contains(t, err.Error(), "overflow")

	// In-range small-width arithmetic still succeeds: u8 15 * 15 = 225.
	out, err := runtimeBinaryArithmetic(OpMul, ValueUint8(15), ValueUint8(15))
	require.NoError(t, err)
	got, err := out.AsBigInt()
	require.NoError(t, err)
	require.Equal(t, big.NewInt(225), got)
}

// TestArithmeticWithinWidthSucceeds ensures ordinary in-range wide arithmetic
// still works after the overflow guard.
func TestArithmeticWithinWidthSucceeds(t *testing.T) {
	a := RuntimeValue{Tag: TagUint256, intVal: big.NewInt(1_000_000)}
	b := RuntimeValue{Tag: TagUint256, intVal: big.NewInt(2)}
	out, err := runtimeBinaryArithmetic(OpMul, a, b)
	require.NoError(t, err)
	got, err := out.AsBigInt()
	require.NoError(t, err)
	require.Equal(t, big.NewInt(2_000_000), got)
}

// TestBitNotUnsignedStaysInRange ensures bitwise-not of an unsigned wide value
// yields the width-complement (a valid in-range unsigned value) rather than a
// negative big.Int.
func TestBitNotUnsignedStaysInRange(t *testing.T) {
	v := RuntimeValue{Tag: TagUint128, intVal: big.NewInt(0)}
	out, err := runtimeUnaryArithmetic(OpBitNot, v)
	require.NoError(t, err)
	got, err := out.AsBigInt()
	require.NoError(t, err)
	// ^0 over 128 bits == 2^128 - 1
	expected := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 128), big.NewInt(1))
	require.Equal(t, expected, got)
}

// TestEmitCoinFromBigIntRejectsNegative ensures a negative emit amount is a
// normal error rather than a panic in sdk.NewCoin.
func TestEmitCoinFromBigIntRejectsNegative(t *testing.T) {
	_, err := emitCoinFromBigInt(big.NewInt(-1))
	require.Error(t, err)
}
