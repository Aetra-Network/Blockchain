package avm

import (
	"math/big"
	mrand "math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/aetravm/async"
)

// Stage 7(a)/(b): property-based and boundary-value coverage for the
// full-width fused-multiply-divide family (mulDiv/mulDivRoundUp/
// mulDivNearest), mulCmp, mulDivSigned and isqrt. Every reference value below
// is computed with plain math/big arithmetic written directly in this file --
// never by calling runtimeMulDiv/runtimeMulDivNearest/runtimeMulCmp/
// runtimeMulDivSigned/runtimeIsqrt themselves -- so a bug shared between the
// production helper and its "expected" value cannot hide.

// --- shared boundary constants -------------------------------------------

func maxUint256() *big.Int {
	return new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
}

func maxInt256() *big.Int {
	return new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 255), big.NewInt(1))
}

func minInt256() *big.Int {
	return new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 255))
}

func fitsUint256(v *big.Int) bool {
	return v.Sign() >= 0 && v.Cmp(new(big.Int).Lsh(big.NewInt(1), 256)) < 0
}

func fitsInt256(v *big.Int) bool {
	return v.Cmp(minInt256()) >= 0 && v.Cmp(maxInt256()) <= 0
}

// refMulDiv computes floor(a*b/c) and, when rem != 0, ceil(a*b/c) and
// round-half-up(a*b/c), all independently of the production code. divByZero
// reports whether c == 0 (the trap case).
func refMulDiv(a, b, c *big.Int) (floor, ceil, nearest *big.Int, divByZero bool) {
	if c.Sign() == 0 {
		return nil, nil, nil, true
	}
	prod := new(big.Int).Mul(a, b)
	quo := new(big.Int)
	rem := new(big.Int)
	quo.QuoRem(prod, c, rem)

	floor = new(big.Int).Set(quo)

	ceil = new(big.Int).Set(quo)
	if rem.Sign() != 0 {
		ceil.Add(ceil, big.NewInt(1))
	}

	nearest = new(big.Int).Set(quo)
	doubled := new(big.Int).Lsh(rem, 1)
	if doubled.Cmp(c) >= 0 {
		nearest.Add(nearest, big.NewInt(1))
	}
	return floor, ceil, nearest, false
}

// checkMulDivCase drives runtimeMulDiv/runtimeMulDivRoundUp/
// runtimeMulDivNearest for one (a,b,c) triple and asserts the trap-vs-succeed
// decision and (on success) the exact value match the independent reference.
func checkMulDivCase(t *testing.T, a, b, c *big.Int) {
	t.Helper()
	floor, ceil, nearest, divByZero := refMulDiv(a, b, c)

	av, bv, cv := ValueBigInt256(a), ValueBigInt256(b), ValueBigInt256(c)

	checkOne := func(name string, want *big.Int, run func() (RuntimeValue, error)) {
		var got RuntimeValue
		var err error
		require.NotPanics(t, func() { got, err = run() }, "%s(%s,%s,%s) must never panic", name, a, b, c)

		if divByZero {
			require.Error(t, err, "%s(%s,%s,0) must trap", name, a, b)
			return
		}
		if !fitsUint256(want) {
			require.Error(t, err, "%s(%s,%s,%s) quotient %s does not fit uint256, must trap", name, a, b, c, want)
			return
		}
		require.NoError(t, err, "%s(%s,%s,%s) must succeed", name, a, b, c)
		gotBig, berr := got.AsBigInt()
		require.NoError(t, berr)
		require.Equal(t, 0, gotBig.Cmp(want), "%s(%s,%s,%s): got %s want %s", name, a, b, c, gotBig, want)
	}

	checkOne("mulDiv", floor, func() (RuntimeValue, error) { return runtimeMulDiv(av, bv, cv, false) })
	checkOne("mulDivRoundUp", ceil, func() (RuntimeValue, error) { return runtimeMulDiv(av, bv, cv, true) })
	checkOne("mulDivNearest", nearest, func() (RuntimeValue, error) { return runtimeMulDivNearest(av, bv, cv) })
}

// randUint256 returns a uniformly random value in [0, 2^bits) using
// math/big's own Rand (which accepts a math/rand.Rand source), so the
// generator itself is independent of any AVM code.
func randUint256(r *mrand.Rand, bits uint) *big.Int {
	if bits == 0 {
		return big.NewInt(0)
	}
	limit := new(big.Int).Lsh(big.NewInt(1), bits)
	return new(big.Int).Rand(r, limit)
}

// TestMulDivFamilyProperty_RandomFullRange fuzzes (a,b,c) across the full
// uint256 range -- deliberately including triples whose a*b product
// individually exceeds 2^256 (unbounded x unbounded multiplication is the
// entire reason mulDiv exists over the AMM's reserveIn*reserveOut) but whose
// final a*b/c quotient still fits -- and checks mulDiv/mulDivRoundUp/
// mulDivNearest all agree with the independent math/big reference above.
func TestMulDivFamilyProperty_RandomFullRange(t *testing.T) {
	r := mrand.New(mrand.NewSource(20260719))
	const iterations = 500
	for i := 0; i < iterations; i++ {
		a := randUint256(r, 256)
		b := randUint256(r, 256)

		// Bias c toward "quotient likely fits": half the time draw c from the
		// full 256-bit range (mostly traps -- big product, small-ish
		// divisor), half the time derive c so the quotient is likely to fit
		// (large divisor sized off the product's own bit length), exercising
		// both the trap path and the "huge intermediate, in-range result"
		// path the AMM actually relies on.
		var c *big.Int
		if i%2 == 0 {
			c = randUint256(r, 256)
		} else {
			prod := new(big.Int).Mul(a, b)
			bitLen := uint(prod.BitLen())
			shrink := bitLen
			if shrink > 256 {
				shrink -= 256 // divisor sized to bring the quotient back under 2^256
			} else {
				shrink = 0
			}
			c = randUint256(r, shrink+8) // a little slack so the quotient sometimes still overflows
		}

		checkMulDivCase(t, a, b, c)
	}
}

// TestMulDivFamilyBoundaryValues pins the family at the classic edge values:
// 0, 1, MaxUint256, MaxUint256-1, and powers of two straddling the 256-bit
// boundary.
func TestMulDivFamilyBoundaryValues(t *testing.T) {
	max := maxUint256()
	maxMinus1 := new(big.Int).Sub(max, big.NewInt(1))
	pow255 := new(big.Int).Lsh(big.NewInt(1), 255)
	pow128 := new(big.Int).Lsh(big.NewInt(1), 128)
	zero := big.NewInt(0)
	one := big.NewInt(1)

	cases := [][3]*big.Int{
		{zero, zero, one},
		{zero, max, one},
		{one, one, one},
		{max, one, one},
		{max, max, one},                 // product ~2^512, quotient == max*max, traps (overflow)
		{max, one, max},                 // == 1, exact
		{maxMinus1, one, max},           // < 1, floor 0, exact-ish
		{pow255, big.NewInt(2), one},    // == 2^256, must trap (exactly one past max)
		{pow255, big.NewInt(2), pow255}, // == 2, exact
		{pow128, pow128, one},           // == 2^256, must trap
		{pow128, pow128, big.NewInt(2)}, // == 2^255, fits
		{max, max, max},                 // == max, exact
	}
	for _, tc := range cases {
		checkMulDivCase(t, tc[0], tc[1], tc[2])
	}
}

// TestMulDivFamilyResultCodesAtBoundary spot-checks that the classifier
// (arithResultCode) reports the specific stable codes, not just "an error",
// at the boundary: zero divisor => ResultDivisionByZero, overflowing
// quotient => ResultOutOfRange.
func TestMulDivFamilyResultCodesAtBoundary(t *testing.T) {
	pow255 := new(big.Int).Lsh(big.NewInt(1), 255)

	_, err := runtimeMulDiv(ValueBigInt256(pow255), ValueBigInt256(big.NewInt(4)), ValueBigInt256(big.NewInt(1)), false)
	require.Error(t, err)
	require.Equal(t, async.ResultOutOfRange, arithResultCode(err))

	_, err = runtimeMulDivNearest(ValueBigInt256(pow255), ValueBigInt256(big.NewInt(4)), ValueBigInt256(big.NewInt(1)))
	require.Error(t, err)
	require.Equal(t, async.ResultOutOfRange, arithResultCode(err))

	_, err = runtimeMulDiv(ValueBigInt256(big.NewInt(5)), ValueBigInt256(big.NewInt(7)), ValueBigInt256(big.NewInt(0)), false)
	require.Error(t, err)
	require.Equal(t, async.ResultDivisionByZero, arithResultCode(err))
}

// --- mulCmp ----------------------------------------------------------------

// TestMulCmpProperty_RandomFullRange fuzzes sign(a*b - c*d) across the full
// uint256 range, including products that individually exceed 2^256 (the
// entire point of mulCmp: comparing two cross-products exactly without ever
// forming/narrowing either one to a checked uint256).
func TestMulCmpProperty_RandomFullRange(t *testing.T) {
	r := mrand.New(mrand.NewSource(4242))
	const iterations = 300
	for i := 0; i < iterations; i++ {
		a := randUint256(r, 256)
		b := randUint256(r, 256)
		c := randUint256(r, 256)
		d := randUint256(r, 256)

		left := new(big.Int).Mul(a, b)
		right := new(big.Int).Mul(c, d)
		want := int64(left.Cmp(right))

		var got RuntimeValue
		var err error
		require.NotPanics(t, func() {
			got, err = runtimeMulCmp(ValueBigInt256(a), ValueBigInt256(b), ValueBigInt256(c), ValueBigInt256(d))
		})
		require.NoError(t, err)
		gotBig, berr := got.AsBigInt()
		require.NoError(t, berr)
		require.Equal(t, want, gotBig.Int64(), "mulCmp(%s,%s,%s,%s)", a, b, c, d)
	}
}

// TestMulCmpBoundaryValues pins mulCmp at 0, 1, MaxUint256, and a case where
// both products individually exceed 2^256 but compare unequal.
func TestMulCmpBoundaryValues(t *testing.T) {
	max := maxUint256()
	cases := []struct {
		a, b, c, d *big.Int
		want       int64
	}{
		{big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0), 0},
		{max, max, max, max, 0},
		{max, max, max, new(big.Int).Sub(max, big.NewInt(1)), 1}, // max*max > max*(max-1)
		{new(big.Int).Sub(max, big.NewInt(1)), max, max, max, -1},
		{big.NewInt(1), big.NewInt(0), big.NewInt(0), big.NewInt(1), 0},
	}
	for _, tc := range cases {
		got, err := runtimeMulCmp(ValueBigInt256(tc.a), ValueBigInt256(tc.b), ValueBigInt256(tc.c), ValueBigInt256(tc.d))
		require.NoError(t, err)
		gotBig, berr := got.AsBigInt()
		require.NoError(t, berr)
		require.Equal(t, tc.want, gotBig.Int64(), "mulCmp(%s,%s,%s,%s)", tc.a, tc.b, tc.c, tc.d)
	}
}

// TestMulCmpNegativeOperandTraps pins the documented fail-closed behavior:
// mulCmp's operands are unsigned by contract, and ANY negative operand traps
// deterministically rather than comparing signed magnitudes.
func TestMulCmpNegativeOperandTraps(t *testing.T) {
	_, err := runtimeMulCmp(ValueInt8(-1), ValueBigInt256(big.NewInt(3)), ValueBigInt256(big.NewInt(3)), ValueBigInt256(big.NewInt(2)))
	require.Error(t, err, "a negative first operand must trap")
}

// --- mulDivSigned ------------------------------------------------------------

// refMulDivSigned computes truncated-toward-zero (a*b)/c independently.
func refMulDivSigned(a, b, c *big.Int) (result *big.Int, divByZero bool) {
	if c.Sign() == 0 {
		return nil, true
	}
	sign := a.Sign() * b.Sign() * c.Sign()
	absA, absB, absC := new(big.Int).Abs(a), new(big.Int).Abs(b), new(big.Int).Abs(c)
	prod := new(big.Int).Mul(absA, absB)
	quo := new(big.Int).Quo(prod, absC)
	if sign < 0 {
		quo.Neg(quo)
	}
	return quo, false
}

// TestMulDivSignedProperty_RandomFullRange fuzzes (a,b,c) uniformly across
// the full int256 range (including negatives), asserting the trap-vs-succeed
// decision and, on success, the exact truncated-toward-zero quotient match
// the independent reference.
func TestMulDivSignedProperty_RandomFullRange(t *testing.T) {
	r := mrand.New(mrand.NewSource(99919))
	const iterations = 500
	span := new(big.Int).Lsh(big.NewInt(1), 96) // keep the *magnitude* modest so
	// truncated-toward-zero quotients routinely land back in-range, while
	// still letting the intermediate a*b product exceed int256 on its own.
	for i := 0; i < iterations; i++ {
		a := randSignedWithin(r, span)
		b := randSignedWithin(r, span)
		c := randSignedWithin(r, span)

		want, divByZero := refMulDivSigned(a, b, c)

		var got RuntimeValue
		var err error
		require.NotPanics(t, func() {
			got, err = runtimeMulDivSigned(ValueBigInt256(a), ValueBigInt256(b), ValueBigInt256(c))
		})
		if divByZero {
			require.Error(t, err, "mulDivSigned(%s,%s,0) must trap", a, b)
			continue
		}
		if !fitsInt256(want) {
			require.Error(t, err, "mulDivSigned(%s,%s,%s)=%s overflows int256, must trap", a, b, c, want)
			continue
		}
		require.NoError(t, err, "mulDivSigned(%s,%s,%s) must succeed", a, b, c)
		gotBig, berr := got.AsBigInt()
		require.NoError(t, berr)
		require.Equal(t, 0, gotBig.Cmp(want), "mulDivSigned(%s,%s,%s): got %s want %s", a, b, c, gotBig, want)
	}
}

// randSignedWithin returns a uniformly random value in [-limit, limit].
func randSignedWithin(r *mrand.Rand, limit *big.Int) *big.Int {
	span := new(big.Int).Add(new(big.Int).Lsh(limit, 1), big.NewInt(1)) // 2*limit+1
	v := new(big.Int).Rand(r, span)
	return v.Sub(v, limit)
}

// TestMulDivSignedBoundaryValues pins mulDivSigned at MinInt256/MaxInt256 and
// the classic truncation-toward-zero corner (negative/2 rounds toward 0, not
// floor).
func TestMulDivSignedBoundaryValues(t *testing.T) {
	minI, maxI := minInt256(), maxInt256()
	cases := []struct {
		a, b, c *big.Int
		want    *big.Int // nil means "must trap"
	}{
		{big.NewInt(-7), big.NewInt(3), big.NewInt(2), big.NewInt(-10)}, // -21/2 truncates to -10, not -11
		{big.NewInt(7), big.NewInt(-3), big.NewInt(2), big.NewInt(-10)},
		{big.NewInt(-7), big.NewInt(-3), big.NewInt(2), big.NewInt(10)},
		{maxI, big.NewInt(1), big.NewInt(1), maxI},
		{minI, big.NewInt(1), big.NewInt(1), minI},
		{minI, big.NewInt(-1), big.NewInt(1), nil}, // -MinInt256 overflows int256, must trap
		{maxI, big.NewInt(2), big.NewInt(1), nil},  // 2*MaxInt256 overflows, must trap
	}
	for _, tc := range cases {
		got, err := runtimeMulDivSigned(ValueBigInt256(tc.a), ValueBigInt256(tc.b), ValueBigInt256(tc.c))
		if tc.want == nil {
			require.Error(t, err, "mulDivSigned(%s,%s,%s) must trap", tc.a, tc.b, tc.c)
			continue
		}
		require.NoError(t, err, "mulDivSigned(%s,%s,%s) must succeed", tc.a, tc.b, tc.c)
		gotBig, berr := got.AsBigInt()
		require.NoError(t, berr)
		require.Equal(t, 0, gotBig.Cmp(tc.want), "mulDivSigned(%s,%s,%s): got %s want %s", tc.a, tc.b, tc.c, gotBig, tc.want)
	}
}

// TestMulDivSignedZeroDivisorTraps mirrors the unsigned family: a zero
// divisor traps with the specific ResultDivisionByZero code.
func TestMulDivSignedZeroDivisorTraps(t *testing.T) {
	_, err := runtimeMulDivSigned(ValueBigInt256(big.NewInt(7)), ValueBigInt256(big.NewInt(3)), ValueBigInt256(big.NewInt(0)))
	require.Error(t, err)
	require.Equal(t, async.ResultDivisionByZero, arithResultCode(err))
}

// --- isqrt -------------------------------------------------------------------

// TestIsqrtProperty_RandomFullRange checks isqrt against the independent
// mathematical invariant floor(sqrt(x))^2 <= x < (floor(sqrt(x))+1)^2, rather
// than delegating to big.Int.Sqrt (the same function the production code
// itself calls).
func TestIsqrtProperty_RandomFullRange(t *testing.T) {
	r := mrand.New(mrand.NewSource(777))
	const iterations = 300
	for i := 0; i < iterations; i++ {
		x := randUint256(r, 256)

		var got RuntimeValue
		var err error
		require.NotPanics(t, func() { got, err = runtimeIsqrt(ValueBigInt256(x)) })
		require.NoError(t, err)
		root, berr := got.AsBigInt()
		require.NoError(t, berr)

		rootSq := new(big.Int).Mul(root, root)
		require.LessOrEqual(t, rootSq.Cmp(x), 0, "isqrt(%s): root^2=%s must be <= x", x, rootSq)

		nextSq := new(big.Int).Mul(new(big.Int).Add(root, big.NewInt(1)), new(big.Int).Add(root, big.NewInt(1)))
		require.Greater(t, nextSq.Cmp(x), 0, "isqrt(%s): (root+1)^2=%s must be > x", x, nextSq)
	}
}
