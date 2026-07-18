package conformance

import (
	"math/big"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/app/addressing"
	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/aetravm/compiler"
)

// mapField looks up a named field inside a struct-typed RuntimeValue (a
// TagMap value, as returned directly by a getter whose declared return type
// is one of this file's structs -- CanonicalEncode already gives any struct
// a field-name-keyed wire shape, and AsMap() is the same shape at the Go
// runtime-value level, so a getter's struct return value can be inspected
// directly without going through the (unrelated, argument-only) JSON codec.
func mapField(t *testing.T, v avm.RuntimeValue, name string) avm.RuntimeValue {
	t.Helper()
	entries, err := v.AsMap()
	require.NoError(t, err)
	for _, entry := range entries {
		key, err := entry.Key.AsString()
		require.NoError(t, err)
		if key == name {
			return entry.Value
		}
	}
	t.Fatalf("field %q not found in struct value", name)
	return avm.RuntimeValue{}
}

func fieldBigInt(t *testing.T, v avm.RuntimeValue, name string) *big.Int {
	t.Helper()
	got, err := mapField(t, v, name).AsBigInt()
	require.NoError(t, err)
	return got
}

func fieldTag(t *testing.T, v avm.RuntimeValue, name string) avm.ValueTag {
	t.Helper()
	return mapField(t, v, name).Tag
}

// TestAcceptanceFinanceTypesBasisPoints proves BasisPoints' bounded/
// unrestricted constructors, explicit-rounding-mode apply, complement, and
// checked add/sub -- including every documented trap -- through the real VM.
func TestAcceptanceFinanceTypesBasisPoints(t *testing.T) {
	deployer := testAddress(0x96)
	res := compileExampleFile(t, filepath.Join("finance", "finance_types.atlx"), compiler.Options{
		DeployerAddress: addressing.FormatAccAddress(deployer),
	})
	require.NoError(t, avm.VerifyInterface(res.Module, res.Manifest))
	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	u256 := "uint256"

	// fromRaw is unrestricted: bps > 10000 (>100%) is representable.
	bp := callGetter(t, runner, res, avm.Storage{}, "bpFromRawG", []string{u256}, bi(15000))
	requireBigEq(t, bi(15000), fieldBigInt(t, bp, "bps"), "bpFromRawG allows >100%")

	// fromPercentBounded accepts 0-100% inclusive...
	bounded := callGetter(t, runner, res, avm.Storage{}, "bpFromPercentBoundedG", []string{u256}, bi(9000))
	requireBigEq(t, bi(9000), fieldBigInt(t, bounded, "bps"), "bpFromPercentBoundedG valid")
	// ...and TRAPS above it.
	callGetterExpectTrap(t, runner, res, avm.Storage{}, "bpFromPercentBoundedG", []string{u256}, bi(10001))

	requireBigEq(t, bi(4242), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "bpRawG", []string{u256}, bi(4242))), "bpRawG accessor")

	// Explicit rounding modes: 5000bps (50%) of 1 unit = 0.5, which floor/
	// ceil/nearest must resolve DIFFERENTLY.
	requireBigEq(t, bi(0), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "bpApplyFloorG", []string{u256, u256}, bi(5000), bi(1))), "bpApplyFloor 0.5 -> 0")
	requireBigEq(t, bi(1), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "bpApplyCeilG", []string{u256, u256}, bi(5000), bi(1))), "bpApplyCeil 0.5 -> 1")
	requireBigEq(t, bi(1), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "bpApplyNearestG", []string{u256, u256}, bi(5000), bi(1))), "bpApplyNearest 0.5 -> 1 (round-half-up)")

	comp := callGetter(t, runner, res, avm.Storage{}, "bpComplementG", []string{u256}, bi(30))
	requireBigEq(t, bi(9970), fieldBigInt(t, comp, "bps"), "bpComplement(30) == 9970")
	// complement of an over-100% BasisPoints TRAPS (MAX_BPS - bps underflows).
	callGetterExpectTrap(t, runner, res, avm.Storage{}, "bpComplementG", []string{u256}, bi(10001))

	added := callGetter(t, runner, res, avm.Storage{}, "bpAddG", []string{u256, u256}, bi(9000), bi(999))
	requireBigEq(t, bi(9999), fieldBigInt(t, added, "bps"), "bpAdd 9000+999")
	maxU256 := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	callGetterExpectTrap(t, runner, res, avm.Storage{}, "bpAddG", []string{u256, u256}, maxU256, bi(1))

	sub := callGetter(t, runner, res, avm.Storage{}, "bpSubG", []string{u256, u256}, bi(100), bi(50))
	requireBigEq(t, bi(50), fieldBigInt(t, sub, "bps"), "bpSub 100-50")
	callGetterExpectTrap(t, runner, res, avm.Storage{}, "bpSubG", []string{u256, u256}, bi(50), bi(100))
}

// TestAcceptanceFinanceTypesRatio256 proves Ratio256's den != 0 invariant is
// UNCONSTRUCTIBLE-when-violated at every entry point (fromRaw, invert, div),
// the full-range compare, checked add/sub/mul/div, explicit-rounding apply,
// and the storage-backed gcd reduce() -- through the real VM.
func TestAcceptanceFinanceTypesRatio256(t *testing.T) {
	deployer := testAddress(0x97)
	res := compileExampleFile(t, filepath.Join("finance", "finance_types.atlx"), compiler.Options{
		DeployerAddress: addressing.FormatAccAddress(deployer),
	})
	require.NoError(t, avm.VerifyInterface(res.Module, res.Manifest))
	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	u256 := "uint256"

	r := callGetter(t, runner, res, avm.Storage{}, "ratioFromRawG", []string{u256, u256}, bi(3), bi(4))
	requireBigEq(t, bi(3), fieldBigInt(t, r, "num"), "ratioFromRaw num")
	requireBigEq(t, bi(4), fieldBigInt(t, r, "den"), "ratioFromRaw den")
	// den == 0 is UNCONSTRUCTIBLE: TRAPS, never silently produced.
	callGetterExpectTrap(t, runner, res, avm.Storage{}, "ratioFromRawG", []string{u256, u256}, bi(5), bi(0))

	requireBigEq(t, bi(3), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "ratioNumeratorG", []string{u256, u256}, bi(3), bi(4))), "ratioNumerator")
	requireBigEq(t, bi(4), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "ratioDenominatorG", []string{u256, u256}, bi(3), bi(4))), "ratioDenominator")

	inv := callGetter(t, runner, res, avm.Storage{}, "ratioInvertG", []string{u256, u256}, bi(3), bi(4))
	requireBigEq(t, bi(4), fieldBigInt(t, inv, "num"), "invert(3/4).num")
	requireBigEq(t, bi(3), fieldBigInt(t, inv, "den"), "invert(3/4).den")
	// Inverting the ZERO ratio (0/5) would produce den == 0: TRAPS.
	callGetterExpectTrap(t, runner, res, avm.Storage{}, "ratioInvertG", []string{u256, u256}, bi(0), bi(5))

	// 3/4 (0.75) > 2/3 (0.667): compare > 0, greaterThan == 1.
	requireBigEq(t, bi(1), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "ratioCompareG", []string{u256, u256, u256, u256}, bi(3), bi(4), bi(2), bi(3))), "ratioCompare 3/4 vs 2/3")
	require.Equal(t, uint64(1), u64Result(t, callGetter(t, runner, res, avm.Storage{}, "ratioGreaterThanG", []string{u256, u256, u256, u256}, bi(3), bi(4), bi(2), bi(3))), "3/4 > 2/3")
	require.Equal(t, uint64(0), u64Result(t, callGetter(t, runner, res, avm.Storage{}, "ratioGreaterThanG", []string{u256, u256, u256, u256}, bi(2), bi(3), bi(3), bi(4))), "2/3 > 3/4 is false")

	// 1/2 + 1/3 = 5/6 (unreduced by construction).
	add := callGetter(t, runner, res, avm.Storage{}, "ratioAddG", []string{u256, u256, u256, u256}, bi(1), bi(2), bi(1), bi(3))
	requireBigEq(t, bi(5), fieldBigInt(t, add, "num"), "1/2+1/3 num")
	requireBigEq(t, bi(6), fieldBigInt(t, add, "den"), "1/2+1/3 den")

	// 1/2 - 1/3 = 1/6.
	sub := callGetter(t, runner, res, avm.Storage{}, "ratioSubG", []string{u256, u256, u256, u256}, bi(1), bi(2), bi(1), bi(3))
	requireBigEq(t, bi(1), fieldBigInt(t, sub, "num"), "1/2-1/3 num")
	requireBigEq(t, bi(6), fieldBigInt(t, sub, "den"), "1/2-1/3 den")
	// 1/3 - 1/2 is negative: Ratio256 has no negative representation, TRAPS.
	callGetterExpectTrap(t, runner, res, avm.Storage{}, "ratioSubG", []string{u256, u256, u256, u256}, bi(1), bi(3), bi(1), bi(2))

	// 1/2 * 2/3 = 2/6 (unreduced -- NOT auto-reduced to 1/3).
	mul := callGetter(t, runner, res, avm.Storage{}, "ratioMulG", []string{u256, u256, u256, u256}, bi(1), bi(2), bi(2), bi(3))
	requireBigEq(t, bi(2), fieldBigInt(t, mul, "num"), "1/2*2/3 num (unreduced)")
	requireBigEq(t, bi(6), fieldBigInt(t, mul, "den"), "1/2*2/3 den (unreduced)")

	// (1/2) / (1/3) = 3/2.
	div := callGetter(t, runner, res, avm.Storage{}, "ratioDivG", []string{u256, u256, u256, u256}, bi(1), bi(2), bi(1), bi(3))
	requireBigEq(t, bi(3), fieldBigInt(t, div, "num"), "(1/2)/(1/3) num")
	requireBigEq(t, bi(2), fieldBigInt(t, div, "den"), "(1/2)/(1/3) den")
	// Dividing by the ZERO ratio (0/3) TRAPS.
	callGetterExpectTrap(t, runner, res, avm.Storage{}, "ratioDivG", []string{u256, u256, u256, u256}, bi(1), bi(2), bi(0), bi(3))

	// applyTo(10) at ratio 1/3: floor=3, ceil=4, nearest=3 (0.33 rounds down).
	requireBigEq(t, bi(3), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "ratioApplyToFloorG", []string{u256, u256, u256}, bi(1), bi(3), bi(10))), "applyToFloor 10/3")
	requireBigEq(t, bi(4), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "ratioApplyToCeilG", []string{u256, u256, u256}, bi(1), bi(3), bi(10))), "applyToCeil 10/3")
	requireBigEq(t, bi(3), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "ratioApplyToNearestG", []string{u256, u256, u256}, bi(1), bi(3), bi(10))), "applyToNearest 10/3")

	// reduce(): the gcd loop runs in the ReduceRatio message handler (an
	// impure entry point -- see the file's doc comment on why @get can't
	// hold the mutating accumulator), writing into storage; ratioReduce()
	// reads the committed result back out.
	reduceCodec := res.MessageBodies["ReduceRatio"]
	require.NotEmpty(t, reduceCodec.Fields, "ReduceRatio codec must be registered")
	reduceOpcode := res.MessageBodyOpcodes["ReduceRatio"]
	submitReduce := func(num, den *big.Int) avm.Storage {
		body := mustCodecBody(t, reduceCodec, map[string]any{"num": num, "den": den})
		exec, err := runner.Run(res.Module, avm.Storage{}, avm.RuntimeContext{
			Entry:           avm.EntryReceiveInternal,
			ContractAddress: deployer,
			GasLimit:        10_000_000,
			Message: async.MessageEnvelope{
				Opcode:   reduceOpcode,
				QueryID:  uint64(reduceOpcode),
				Body:     body,
				GasLimit: 10_000_000,
			},
		})
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, exec.ResultCode, "ReduceRatio submit")
		return exec.State
	}
	readReduced := func(state avm.Storage) avm.RuntimeValue {
		exec, err := runner.Run(res.Module, state, avm.RuntimeContext{
			Entry:    avm.EntryQuery,
			GasLimit: 10_000_000,
			Message:  async.MessageEnvelope{Opcode: opcodeForGetter(t, res, "ratioReduce"), GasLimit: 10_000_000},
		})
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, exec.ResultCode, "ratioReduce getter")
		return exec.ReturnValue
	}

	// gcd(12, 18) = 6 -> reduced to 2/3.
	reduced := readReduced(submitReduce(big.NewInt(12), big.NewInt(18)))
	requireBigEq(t, bi(2), fieldBigInt(t, reduced, "num"), "reduce(12/18) num")
	requireBigEq(t, bi(3), fieldBigInt(t, reduced, "den"), "reduce(12/18) den")

	// The zero ratio 0/7 reduces to the canonical 0/1.
	reducedZero := readReduced(submitReduce(big.NewInt(0), big.NewInt(7)))
	requireBigEq(t, bi(0), fieldBigInt(t, reducedZero, "num"), "reduce(0/7) num")
	requireBigEq(t, bi(1), fieldBigInt(t, reducedZero, "den"), "reduce(0/7) den")
}

// e9 returns 1e9 (the Decimal128/SignedDecimal128 scale).
func e9() *big.Int { return big.NewInt(1_000_000_000) }

// TestAcceptanceFinanceTypesDecimal256 proves Decimal256's constructors,
// explicit-rounding mul/div (via mulDiv's unbounded intermediate), and
// explicit-rounding integer conversion / part accessors.
func TestAcceptanceFinanceTypesDecimal256(t *testing.T) {
	deployer := testAddress(0x98)
	res := compileExampleFile(t, filepath.Join("finance", "finance_types.atlx"), compiler.Options{
		DeployerAddress: addressing.FormatAccAddress(deployer),
	})
	require.NoError(t, avm.VerifyInterface(res.Module, res.Manifest))
	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	u256 := "uint256"

	d := callGetter(t, runner, res, avm.Storage{}, "dec256FromIntG", []string{u256}, bi(5))
	requireBigEq(t, scaled(5), fieldBigInt(t, d, "raw"), "dec256FromInt(5)")
	require.Equal(t, avm.TagUint256, fieldTag(t, d, "raw"), "dec256's raw field is genuinely TagUint256")

	requireBigEq(t, bi(123), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "dec256RawG", []string{u256}, bi(123))), "dec256Raw accessor")

	// 1/3 at 1e18 scale: floor=333333333333333333, ceil is one more,
	// nearest rounds down (remainder 1/3 < 1/2).
	third := new(big.Int)
	third.SetString("333333333333333333", 10)
	thirdCeil := new(big.Int).Add(third, big.NewInt(1))
	fFloor := callGetter(t, runner, res, avm.Storage{}, "dec256FromRatioFloorG", []string{u256, u256}, bi(1), bi(3))
	requireBigEq(t, third, fieldBigInt(t, fFloor, "raw"), "dec256FromRatioFloor(1/3)")
	fCeil := callGetter(t, runner, res, avm.Storage{}, "dec256FromRatioCeilG", []string{u256, u256}, bi(1), bi(3))
	requireBigEq(t, thirdCeil, fieldBigInt(t, fCeil, "raw"), "dec256FromRatioCeil(1/3)")
	fNearest := callGetter(t, runner, res, avm.Storage{}, "dec256FromRatioNearestG", []string{u256, u256}, bi(1), bi(3))
	requireBigEq(t, third, fieldBigInt(t, fNearest, "raw"), "dec256FromRatioNearest(1/3) rounds down")

	bpDec := callGetter(t, runner, res, avm.Storage{}, "dec256FromBasisPointsG", []string{u256}, bi(5000))
	requireBigEq(t, new(big.Int).Div(e18(), big.NewInt(2)), fieldBigInt(t, bpDec, "raw"), "dec256FromBasisPoints(5000bps) == 0.5")

	// mul at raw values 5e17 (0.5) * 3 (raw units): product 1.5e18,
	// remainder exactly half of SCALE256 -> floor=1, ceil=2, nearest=2
	// (round-half-up).
	halfScale := new(big.Int).Div(e18(), big.NewInt(2))
	mFloor := callGetter(t, runner, res, avm.Storage{}, "dec256MulFloorG", []string{u256, u256}, halfScale, bi(3))
	requireBigEq(t, bi(1), fieldBigInt(t, mFloor, "raw"), "dec256MulFloor half-remainder rounds down")
	mCeil := callGetter(t, runner, res, avm.Storage{}, "dec256MulCeilG", []string{u256, u256}, halfScale, bi(3))
	requireBigEq(t, bi(2), fieldBigInt(t, mCeil, "raw"), "dec256MulCeil half-remainder rounds up")
	mNearest := callGetter(t, runner, res, avm.Storage{}, "dec256MulNearestG", []string{u256, u256}, halfScale, bi(3))
	requireBigEq(t, bi(2), fieldBigInt(t, mNearest, "raw"), "dec256MulNearest half-remainder rounds up (round-half-up)")

	// div: 1/3 at scale, same triple as fromRatio above.
	dFloor := callGetter(t, runner, res, avm.Storage{}, "dec256DivFloorG", []string{u256, u256}, bi(1), bi(3))
	requireBigEq(t, third, fieldBigInt(t, dFloor, "raw"), "dec256DivFloor(1,3)")
	dCeil := callGetter(t, runner, res, avm.Storage{}, "dec256DivCeilG", []string{u256, u256}, bi(1), bi(3))
	requireBigEq(t, thirdCeil, fieldBigInt(t, dCeil, "raw"), "dec256DivCeil(1,3)")

	// toInteger at 2.5 (2.5e18): floor=2, ceil=3, nearest=3 (round-half-up).
	twoAndHalf := new(big.Int).Add(scaled(2), halfScale)
	requireBigEq(t, bi(2), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "dec256ToIntegerFloorG", []string{u256}, twoAndHalf)), "toIntegerFloor(2.5)")
	requireBigEq(t, bi(3), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "dec256ToIntegerCeilG", []string{u256}, twoAndHalf)), "toIntegerCeil(2.5)")
	requireBigEq(t, bi(3), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "dec256ToIntegerNearestG", []string{u256}, twoAndHalf)), "toIntegerNearest(2.5)")
	requireBigEq(t, bi(2), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "dec256IntegerPartG", []string{u256}, twoAndHalf)), "integerPart(2.5)")
	requireBigEq(t, halfScale, bigResult(t, callGetter(t, runner, res, avm.Storage{}, "dec256FractionalPartG", []string{u256}, twoAndHalf)), "fractionalPart(2.5)")
}

// TestAcceptanceFinanceTypesDecimal128 proves Decimal128's narrower scale and
// the mandatory checked-narrowing overflow trap (toUint128 must TRAP, not
// silently wrap, when a mulDiv-family uint256 result exceeds uint128).
func TestAcceptanceFinanceTypesDecimal128(t *testing.T) {
	deployer := testAddress(0x99)
	res := compileExampleFile(t, filepath.Join("finance", "finance_types.atlx"), compiler.Options{
		DeployerAddress: addressing.FormatAccAddress(deployer),
	})
	require.NoError(t, avm.VerifyInterface(res.Module, res.Manifest))
	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	u256 := "uint256"

	d := callGetter(t, runner, res, avm.Storage{}, "dec128FromIntG", []string{u256}, bi(3))
	requireBigEq(t, new(big.Int).Mul(bi(3), e9()), fieldBigInt(t, d, "raw"), "dec128FromInt(3)")
	require.Equal(t, avm.TagUint128, fieldTag(t, d, "raw"), "dec128's raw field is genuinely TagUint128, not TagUint256")

	requireBigEq(t, bi(123), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "dec128RawG", []string{u256}, bi(123))), "dec128Raw accessor")

	third9 := big.NewInt(333333333) // floor(1e9/3)
	fFloor := callGetter(t, runner, res, avm.Storage{}, "dec128FromRatioFloorG", []string{u256, u256}, bi(1), bi(3))
	requireBigEq(t, third9, fieldBigInt(t, fFloor, "raw"), "dec128FromRatioFloor(1/3)")

	bpDec := callGetter(t, runner, res, avm.Storage{}, "dec128FromBasisPointsG", []string{u256}, bi(5000))
	requireBigEq(t, big.NewInt(500_000_000), fieldBigInt(t, bpDec, "raw"), "dec128FromBasisPoints(5000bps) == 0.5")

	mFloor := callGetter(t, runner, res, avm.Storage{}, "dec128MulFloorG", []string{u256, u256}, big.NewInt(500_000_000), bi(3))
	requireBigEq(t, bi(1), fieldBigInt(t, mFloor, "raw"), "dec128MulFloor half-remainder rounds down")

	dFloor := callGetter(t, runner, res, avm.Storage{}, "dec128DivFloorG", []string{u256, u256}, bi(1), bi(3))
	requireBigEq(t, third9, fieldBigInt(t, dFloor, "raw"), "dec128DivFloor(1,3)")

	twoAndHalf9 := big.NewInt(2_500_000_000)
	requireBigEq(t, bi(2), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "dec128ToIntegerFloorG", []string{u256}, twoAndHalf9)), "toIntegerFloor(2.5)")
	requireBigEq(t, bi(3), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "dec128ToIntegerCeilG", []string{u256}, twoAndHalf9)), "toIntegerCeil(2.5)")
	requireBigEq(t, bi(3), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "dec128ToIntegerNearestG", []string{u256}, twoAndHalf9)), "toIntegerNearest(2.5)")
	requireBigEq(t, big.NewInt(500_000_000), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "dec128FractionalPartG", []string{u256}, twoAndHalf9)), "fractionalPart(2.5)")

	// Overflow: 2^130 * SCALE128 vastly exceeds uint128's ~3.4e38 max --
	// mulDivFloor (uint256) succeeds, but toUint128 MUST trap.
	huge := new(big.Int).Lsh(big.NewInt(1), 130)
	callGetterExpectTrap(t, runner, res, avm.Storage{}, "dec128OverflowTrapG", []string{u256}, huge)
}

// TestAcceptanceFinanceTypesSignedDecimal256 proves SignedDecimal256's
// constructors, mulDivSigned-based mul/div/toInteger (truncated toward
// zero -- NOT floor), and that its raw field genuinely negative-signs
// through, via the real VM.
func TestAcceptanceFinanceTypesSignedDecimal256(t *testing.T) {
	deployer := testAddress(0x9a)
	res := compileExampleFile(t, filepath.Join("finance", "finance_types.atlx"), compiler.Options{
		DeployerAddress: addressing.FormatAccAddress(deployer),
	})
	require.NoError(t, avm.VerifyInterface(res.Module, res.Manifest))
	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	i256 := "int256"
	u256 := "uint256"

	pos := callGetter(t, runner, res, avm.Storage{}, "sdec256FromIntG", []string{i256}, bi(7))
	requireBigEq(t, scaled(7), fieldBigInt(t, pos, "raw"), "sdec256FromInt(7)")
	neg := callGetter(t, runner, res, avm.Storage{}, "sdec256FromIntG", []string{i256}, bi(-7))
	requireBigEq(t, new(big.Int).Neg(scaled(7)), fieldBigInt(t, neg, "raw"), "sdec256FromInt(-7)")
	require.Equal(t, avm.TagInt256, fieldTag(t, neg, "raw"), "sdec256's raw field is genuinely TagInt256")

	requireBigEq(t, bi(-123), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "sdec256RawG", []string{i256}, bi(-123))), "sdec256Raw accessor")

	// fromRatio(1,3): unsigned Ratio256 -> non-negative SignedDecimal256, via
	// toInt256's checked re-tag.
	third := new(big.Int)
	third.SetString("333333333333333333", 10)
	fr := callGetter(t, runner, res, avm.Storage{}, "sdec256FromRatioG", []string{u256, u256}, bi(1), bi(3))
	requireBigEq(t, third, fieldBigInt(t, fr, "raw"), "sdec256FromRatio(1/3)")

	// mul: -2.0 * 3.0 = -6.0.
	mul := callGetter(t, runner, res, avm.Storage{}, "sdec256MulG", []string{i256, i256}, new(big.Int).Neg(scaled(2)), scaled(3))
	requireBigEq(t, new(big.Int).Neg(scaled(6)), fieldBigInt(t, mul, "raw"), "sdec256Mul -2.0*3.0")

	// div, exact: -7.0 / 2.0 = -3.5.
	div := callGetter(t, runner, res, avm.Storage{}, "sdec256DivG", []string{i256, i256}, new(big.Int).Neg(scaled(7)), scaled(2))
	requireBigEq(t, new(big.Int).Neg(new(big.Int).Add(scaled(3), new(big.Int).Div(e18(), big.NewInt(2)))), fieldBigInt(t, div, "raw"), "sdec256Div -7.0/2.0 == -3.5")

	// div, truncating toward zero (NOT floor): mulDivSigned(-1, 1e18, 3) =
	// -(1e18/3) truncated = -333333333333333333, not -333333333333333334.
	divTrunc := callGetter(t, runner, res, avm.Storage{}, "sdec256DivG", []string{i256, i256}, bi(-1), bi(3))
	requireBigEq(t, new(big.Int).Neg(third), fieldBigInt(t, divTrunc, "raw"), "sdec256Div truncates toward zero")
	// Dividing by zero TRAPS.
	callGetterExpectTrap(t, runner, res, avm.Storage{}, "sdec256DivG", []string{i256, i256}, bi(7), bi(0))

	// toInteger/fractionalPart at -2.5: truncate toward zero -> -2 (not
	// floor's -3); fractional part carries the dividend's sign (-0.5).
	negTwoAndHalf := new(big.Int).Neg(new(big.Int).Add(scaled(2), new(big.Int).Div(e18(), big.NewInt(2))))
	requireBigEq(t, bi(-2), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "sdec256ToIntegerG", []string{i256}, negTwoAndHalf)), "sdec256ToInteger(-2.5) truncates toward zero")
	requireBigEq(t, bi(-2), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "sdec256IntegerPartG", []string{i256}, negTwoAndHalf)), "sdec256IntegerPart(-2.5)")
	requireBigEq(t, new(big.Int).Neg(new(big.Int).Div(e18(), big.NewInt(2))), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "sdec256FractionalPartG", []string{i256}, negTwoAndHalf)), "sdec256FractionalPart(-2.5) == -0.5")
}

// TestAcceptanceFinanceTypesSignedDecimal128 proves SignedDecimal128's
// narrower scale and that the checked narrow-to-int128 cast traps on the
// SIGNED range (a huge-magnitude negative value), not just on magnitude.
func TestAcceptanceFinanceTypesSignedDecimal128(t *testing.T) {
	deployer := testAddress(0x9b)
	res := compileExampleFile(t, filepath.Join("finance", "finance_types.atlx"), compiler.Options{
		DeployerAddress: addressing.FormatAccAddress(deployer),
	})
	require.NoError(t, avm.VerifyInterface(res.Module, res.Manifest))
	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	i256 := "int256"

	neg := callGetter(t, runner, res, avm.Storage{}, "sdec128FromIntG", []string{i256}, bi(-4))
	requireBigEq(t, new(big.Int).Neg(new(big.Int).Mul(bi(4), e9())), fieldBigInt(t, neg, "raw"), "sdec128FromInt(-4)")
	require.Equal(t, avm.TagInt128, fieldTag(t, neg, "raw"), "sdec128's raw field is genuinely TagInt128")

	requireBigEq(t, bi(-99), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "sdec128RawG", []string{i256}, bi(-99))), "sdec128Raw accessor")

	scale9 := e9()
	mul := callGetter(t, runner, res, avm.Storage{}, "sdec128MulG", []string{i256, i256}, new(big.Int).Neg(new(big.Int).Mul(bi(2), scale9)), new(big.Int).Mul(bi(3), scale9))
	requireBigEq(t, new(big.Int).Neg(new(big.Int).Mul(bi(6), scale9)), fieldBigInt(t, mul, "raw"), "sdec128Mul -2.0*3.0")

	div := callGetter(t, runner, res, avm.Storage{}, "sdec128DivG", []string{i256, i256}, new(big.Int).Neg(new(big.Int).Mul(bi(7), scale9)), new(big.Int).Mul(bi(2), scale9))
	requireBigEq(t, new(big.Int).Neg(new(big.Int).Add(new(big.Int).Mul(bi(3), scale9), new(big.Int).Div(scale9, bi(2)))), fieldBigInt(t, div, "raw"), "sdec128Div -7.0/2.0 == -3.5")

	negTwoAndHalf9 := big.NewInt(-2_500_000_000)
	requireBigEq(t, bi(-2), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "sdec128ToIntegerG", []string{i256}, negTwoAndHalf9)), "sdec128ToInteger(-2.5) truncates toward zero")

	// A huge-magnitude negative int256 narrowed to int128 MUST TRAP.
	hugeNeg := new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 130))
	callGetterExpectTrap(t, runner, res, avm.Storage{}, "sdec128OverflowTrapG", []string{i256}, hugeNeg)
}

// TestAcceptanceFinanceTypesPnlEquity proves the SignedDecimal256-based PnL /
// equity helper (extending finance_stdlib.atlx's pnl* namespace with a real
// typed result) through the real VM.
func TestAcceptanceFinanceTypesPnlEquity(t *testing.T) {
	deployer := testAddress(0x9c)
	res := compileExampleFile(t, filepath.Join("finance", "finance_types.atlx"), compiler.Options{
		DeployerAddress: addressing.FormatAccAddress(deployer),
	})
	require.NoError(t, avm.VerifyInterface(res.Module, res.Manifest))
	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	i256 := "int256"

	// Long entered at 3000, exited at 2500, size 10 -> loss of 5000.
	pnl := callGetter(t, runner, res, avm.Storage{}, "pnlEquityOfG", []string{i256, i256, i256}, bi(3000), bi(2500), bi(10))
	requireBigEq(t, bi(-5000), fieldBigInt(t, pnl, "raw"), "pnlEquityOf long loss")
	require.Equal(t, uint64(1), u64Result(t, callGetter(t, runner, res, avm.Storage{}, "pnlEquityIsNegativeG", []string{i256, i256, i256}, bi(3000), bi(2500), bi(10))), "loss is negative")
	require.Equal(t, uint64(0), u64Result(t, callGetter(t, runner, res, avm.Storage{}, "pnlEquityIsNegativeG", []string{i256, i256, i256}, bi(3000), bi(3500), bi(10))), "profit is not negative")

	// Funding scale: rate -0.5 (at 1e18) * size 10e18 / 1e18 = -5e18.
	negHalf := new(big.Int).Neg(new(big.Int).Div(e18(), big.NewInt(2)))
	scaleResult := callGetter(t, runner, res, avm.Storage{}, "pnlEquityScaleG", []string{i256, i256}, negHalf, scaled(10))
	requireBigEq(t, new(big.Int).Neg(scaled(5)), fieldBigInt(t, scaleResult, "raw"), "pnlEquityScale -0.5*10")

	// equity = margin + pnl: 6000 + (-5000) = 1000.
	total := callGetter(t, runner, res, avm.Storage{}, "pnlEquityTotalG", []string{i256, i256}, bi(6000), bi(-5000))
	requireBigEq(t, bi(1000), fieldBigInt(t, total, "raw"), "pnlEquityTotal margin+pnl")
}
