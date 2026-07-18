package avm

import (
	"math/big"
	"testing"
)

// Stage 7(c): Go native fuzz tests for mulDiv (+ its mulDivRoundUp sibling,
// same seams) and mulDivNearest. The property under test is deliberately the
// operational one that matters most: the runtime helper must NEVER PANIC no
// matter what bytes the fuzzer throws at it, and whenever it decides NOT to
// trap, the returned value must exactly match an independent math/big
// floor/ceil/round-half-up computed fresh in this file (never by calling the
// production helpers).

// bytesToUint256 caps arbitrary fuzzer-supplied bytes to the first 32 bytes
// (256 bits) and interprets them as a big-endian unsigned magnitude via
// big.Int.SetBytes -- the same width a real uint256 operand is bounded to on
// the AVM stack (see pushU256 in sig_math_opcodes_test.go), so the fuzz
// corpus explores realistic operand widths instead of unboundedly large ones
// that could never reach these opcodes from validated bytecode.
func bytesToUint256(b []byte) *big.Int {
	if len(b) > 32 {
		b = b[:32]
	}
	return new(big.Int).SetBytes(b)
}

func FuzzMulDivFamily(f *testing.F) {
	// Seed corpus derived from the existing example-based test cases in
	// sig_math_opcodes_test.go (TestAVMMulDivFloorAndRoundUp /
	// TestAVMMulDivNearestRoundsHalfUp), plus explicit edge byte-strings.
	seed := func(a, b, c int64) (aBytes, bBytes, cBytes []byte) {
		return big.NewInt(a).Bytes(), big.NewInt(b).Bytes(), big.NewInt(c).Bytes()
	}
	add := func(a, b, c int64) {
		ab, bb, cb := seed(a, b, c)
		f.Add(ab, bb, cb)
	}
	add(7, 3, 2)
	add(6, 2, 3)
	add(100, 100, 3)
	add(100, 101, 3)
	add(5, 1, 2)
	add(1, 1, 3)
	add(5, 7, 0) // zero divisor
	f.Add([]byte{}, []byte{}, []byte{})
	f.Add(bytes32Of(0xff), bytes32Of(0xff), []byte{0x01}) // near MaxUint256 * MaxUint256

	f.Fuzz(func(t *testing.T, aBytes, bBytes, cBytes []byte) {
		a := bytesToUint256(aBytes)
		b := bytesToUint256(bBytes)
		c := bytesToUint256(cBytes)

		floor, ceil, nearest, divByZero := refMulDiv(a, b, c)

		av, bv, cv := ValueBigInt256(a), ValueBigInt256(b), ValueBigInt256(c)

		run := func(name string, want *big.Int, call func() (RuntimeValue, error)) {
			var got RuntimeValue
			var err error
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("%s(%s,%s,%s) panicked: %v", name, a, b, c, r)
					}
				}()
				got, err = call()
			}()

			switch {
			case divByZero:
				if err == nil {
					t.Fatalf("%s(%s,%s,0) succeeded, want trap", name, a, b)
				}
			case !fitsUint256(want):
				if err == nil {
					t.Fatalf("%s(%s,%s,%s) succeeded with out-of-range quotient %s, want trap", name, a, b, c, want)
				}
			default:
				if err != nil {
					t.Fatalf("%s(%s,%s,%s) trapped (%v), want success with value %s", name, a, b, c, err, want)
				}
				gotBig, berr := got.AsBigInt()
				if berr != nil {
					t.Fatalf("%s(%s,%s,%s): AsBigInt: %v", name, a, b, c, berr)
				}
				if gotBig.Cmp(want) != 0 {
					t.Fatalf("%s(%s,%s,%s): got %s want %s", name, a, b, c, gotBig, want)
				}
			}
		}

		run("mulDiv", floor, func() (RuntimeValue, error) { return runtimeMulDiv(av, bv, cv, false) })
		run("mulDivRoundUp", ceil, func() (RuntimeValue, error) { return runtimeMulDiv(av, bv, cv, true) })
		run("mulDivNearest", nearest, func() (RuntimeValue, error) { return runtimeMulDivNearest(av, bv, cv) })
	})
}

// bytes32Of returns 32 bytes all set to fill, e.g. bytes32Of(0xff) == the
// big-endian encoding of MaxUint256.
func bytes32Of(fill byte) []byte {
	b := make([]byte, 32)
	for i := range b {
		b[i] = fill
	}
	return b
}
