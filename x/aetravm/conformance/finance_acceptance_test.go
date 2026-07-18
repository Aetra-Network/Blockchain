package conformance

import (
	"fmt"
	"math/big"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/app/addressing"
	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/aetravm/compiler"
)

// e18 is the Decimal18 scale (1.0 == 1e18).
func e18() *big.Int { return new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil) }

// scaled returns n * 1e18 (an integer amount expressed in 1e18 fixed-point).
func scaled(n int64) *big.Int { return new(big.Int).Mul(big.NewInt(n), e18()) }

func bi(n int64) *big.Int { return big.NewInt(n) }

// financeArgCodec builds a positional getter-argument codec whose fields are
// named arg0, arg1, … with the given declared types — the wire convention the
// compiler uses for scalar getter parameters (see IRExprMsgField in compile.go).
func financeArgCodec(name string, types ...string) compiler.Codec {
	fields := make([]compiler.CodecField, len(types))
	for i, ty := range types {
		fields[i] = compiler.CodecField{Name: fmt.Sprintf("arg%d", i), Type: compiler.TypeRef{Name: ty}}
	}
	return compiler.Codec{Name: name, Fields: fields}
}

// callGetter drives a getter with the given positional argument values against
// the supplied storage and returns its ReturnValue, asserting the call did not
// trap.
func callGetter(t *testing.T, runner *avm.Runner, res *compiler.Result, storage avm.Storage, name string, types []string, args ...*big.Int) avm.RuntimeValue {
	t.Helper()
	require.Len(t, args, len(types), "arg count must match type count for %s", name)
	codec := financeArgCodec(name, types...)
	values := make(map[string]any, len(args))
	for i, a := range args {
		values[fmt.Sprintf("arg%d", i)] = a
	}
	var body []byte
	if len(args) > 0 {
		body = mustCodecBody(t, codec, values)
	}
	op := opcodeForGetter(t, res, name)
	exec, err := runner.Run(res.Module, storage, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		Message:  async.MessageEnvelope{Opcode: op, Body: body, GasLimit: 10_000_000},
		GasLimit: 10_000_000,
	})
	require.NoError(t, err)
	require.Equalf(t, async.ResultOK, exec.ResultCode, "getter %s trapped (result=%d)", name, exec.ResultCode)
	return exec.ReturnValue
}

// callGetterExpectTrap drives a getter with the given positional argument values
// and asserts it TRAPPED (ResultCode != OK) rather than returning — used to prove
// deterministic rollbacks (division by zero, negative operand) instead of panics.
func callGetterExpectTrap(t *testing.T, runner *avm.Runner, res *compiler.Result, storage avm.Storage, name string, types []string, args ...*big.Int) {
	t.Helper()
	require.Len(t, args, len(types), "arg count must match type count for %s", name)
	codec := financeArgCodec(name, types...)
	values := make(map[string]any, len(args))
	for i, a := range args {
		values[fmt.Sprintf("arg%d", i)] = a
	}
	var body []byte
	if len(args) > 0 {
		body = mustCodecBody(t, codec, values)
	}
	op := opcodeForGetter(t, res, name)
	exec, err := runner.Run(res.Module, storage, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		Message:  async.MessageEnvelope{Opcode: op, Body: body, GasLimit: 10_000_000},
		GasLimit: 10_000_000,
	})
	// A trap is a deterministic rollback: Run returns a non-OK ResultCode (and,
	// for an execution failure, a non-nil error) — never a Go panic.
	require.NotEqualf(t, async.ResultOK, exec.ResultCode, "getter %s should have trapped but returned (result=%d, err=%v)", name, exec.ResultCode, err)
}

func bigResult(t *testing.T, v avm.RuntimeValue) *big.Int {
	t.Helper()
	got, err := v.AsBigInt()
	require.NoError(t, err)
	return got
}

func u64Result(t *testing.T, v avm.RuntimeValue) uint64 {
	t.Helper()
	got, err := v.AsUint64()
	require.NoError(t, err)
	return got
}

func requireBigEq(t *testing.T, want, got *big.Int, msg string) {
	t.Helper()
	require.Equalf(t, 0, want.Cmp(got), "%s: want %s got %s", msg, want, got)
}

// TestAcceptanceFinanceStdlib compiles the canonical finance stdlib host
// contract and drives every namespace routine (BasisPoints / Ratio256 /
// Decimal18 / signed PnL) through the real VM with exact numeric assertions.
func TestAcceptanceFinanceStdlib(t *testing.T) {
	deployer := testAddress(0x91)
	res := compileExampleFile(t, filepath.Join("finance", "finance_stdlib.atlx"), compiler.Options{
		DeployerAddress: addressing.FormatAccAddress(deployer),
	})
	require.NoError(t, avm.VerifyInterface(res.Module, res.Manifest))

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)

	u256 := "uint256"
	i256 := "int256"

	// BasisPoints: 30bps of 1_000_000 = 3000; complement(30)=9970; net = 997000.
	requireBigEq(t, bi(3000), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "bpApply", []string{u256, u256}, bi(30), bi(1_000_000))), "bpApply")
	requireBigEq(t, bi(9970), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "bpComp", []string{u256}, bi(30))), "bpComp")
	requireBigEq(t, bi(997_000), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "bpApplyComp", []string{u256, u256}, bi(30), bi(1_000_000))), "bpApplyComp")

	// Ratio256: 3/4 of 1000 = 750; inverse 4/3 of 750 = 1000; 3/4 > 2/3 => 1.
	requireBigEq(t, bi(750), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "ratioMul", []string{u256, u256, u256}, bi(3), bi(4), bi(1000))), "ratioMul")
	requireBigEq(t, bi(1000), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "ratioMulInv", []string{u256, u256, u256}, bi(3), bi(4), bi(750))), "ratioMulInv")
	require.Equal(t, uint64(1), u64Result(t, callGetter(t, runner, res, avm.Storage{}, "ratioGt", []string{u256, u256, u256, u256}, bi(3), bi(4), bi(2), bi(3))), "ratioGt 3/4 > 2/3")
	require.Equal(t, uint64(0), u64Result(t, callGetter(t, runner, res, avm.Storage{}, "ratioGt", []string{u256, u256, u256, u256}, bi(2), bi(3), bi(3), bi(4))), "ratioGt 2/3 > 3/4 is false")

	// Decimal18: 2.0 * 3.0 = 6.0; 6.0 / 3.0 = 2.0; fromInt(5)=5.0; toInt(5.0)=5.
	requireBigEq(t, scaled(6), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "decMulG", []string{u256, u256}, scaled(2), scaled(3))), "decMul 2*3")
	requireBigEq(t, scaled(2), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "decDivG", []string{u256, u256}, scaled(6), scaled(3))), "decDiv 6/3")
	requireBigEq(t, scaled(5), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "decFromIntG", []string{u256}, bi(5))), "decFromInt 5")
	requireBigEq(t, bi(5), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "decToIntG", []string{u256}, scaled(5))), "decToInt 5.0")

	// decSqrt is a Decimal18 -> Decimal18 square root: input and output are BOTH
	// 1e18-scaled. decSqrt(4.0) = 2.0 (i.e. 4e18 -> 2e18) and decSqrt(1.0) = 1.0.
	requireBigEq(t, scaled(2), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "decSqrtG", []string{u256}, scaled(4))), "decSqrt 4.0 == 2.0")
	requireBigEq(t, scaled(1), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "decSqrtG", []string{u256}, scaled(1))), "decSqrt 1.0 == 1.0")

	// Zero-denominator guards: getters that divide by a caller operand return 0
	// (rather than trapping mulDiv) when that denominator is 0.
	requireBigEq(t, bi(0), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "decDivG", []string{u256, u256}, scaled(6), bi(0))), "decDiv by 0 => 0")
	requireBigEq(t, bi(0), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "ratioMul", []string{u256, u256, u256}, bi(3), bi(0), bi(1000))), "ratioMul den 0 => 0")
	requireBigEq(t, bi(0), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "ratioMulInv", []string{u256, u256, u256}, bi(0), bi(4), bi(750))), "ratioMulInv num 0 => 0")

	// Signed PnL: a long entered at 3000, exited at 2500, size 10 => -5000.
	requireBigEq(t, bi(-5000), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "pnl", []string{i256, i256, i256}, bi(3000), bi(2500), bi(10))), "pnl long loss")
	require.Equal(t, uint64(1), u64Result(t, callGetter(t, runner, res, avm.Storage{}, "pnlIsNegative", []string{i256, i256, i256}, bi(3000), bi(2500), bi(10))), "pnl negative")
	requireBigEq(t, bi(5000), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "pnl", []string{i256, i256, i256}, bi(3000), bi(3500), bi(10))), "pnl long profit")
	require.Equal(t, uint64(0), u64Result(t, callGetter(t, runner, res, avm.Storage{}, "pnlIsNegative", []string{i256, i256, i256}, bi(3000), bi(3500), bi(10))), "pnl positive")

	// ---- mulCmp: sign(a*b - c*d) equal / less / greater -------------------
	requireBigEq(t, bi(0), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "mulCmpG", []string{u256, u256, u256, u256}, bi(2), bi(3), bi(3), bi(2))), "mulCmp 2*3 == 3*2 => 0")
	requireBigEq(t, bi(-1), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "mulCmpG", []string{u256, u256, u256, u256}, bi(2), bi(3), bi(3), bi(3))), "mulCmp 2*3 < 3*3 => -1")
	requireBigEq(t, bi(1), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "mulCmpG", []string{u256, u256, u256, u256}, bi(3), bi(3), bi(2), bi(3))), "mulCmp 3*3 > 2*3 => +1")

	// ---- ratioGtFull: FULL-RANGE ratio compare where a cross operand EXCEEDS
	// 2^128 (so the old bounded path's mulDiv(num,den,1) forms a > uint256
	// product and TRAPS). numA/denA = 3*2^100, numC/denC = 2^100, so numA/denA >
	// numC/denC; the full-range op returns the right answer, the bounded op traps.
	p200 := new(big.Int).Lsh(big.NewInt(1), 200)
	p100 := new(big.Int).Lsh(big.NewInt(1), 100)
	numA := new(big.Int).Mul(big.NewInt(3), p200) // 3*2^200
	denA := new(big.Int).Set(p100)                // 2^100
	numC := new(big.Int).Set(p200)                // 2^200
	denC := new(big.Int).Set(p100)                // 2^100
	// Cross products numA*denC = 3*2^300 and numC*denA = 2^300 each exceed 2^256.
	require.Equal(t, uint64(1), u64Result(t, callGetter(t, runner, res, avm.Storage{}, "ratioGtFullG", []string{u256, u256, u256, u256}, numA, denA, numC, denC)), "ratioGtFull 3*2^100 > 2^100 (cross operand > 2^128)")
	require.Equal(t, uint64(0), u64Result(t, callGetter(t, runner, res, avm.Storage{}, "ratioGtFullG", []string{u256, u256, u256, u256}, numC, denC, numA, denA)), "ratioGtFull 2^100 > 3*2^100 is false")
	requireBigEq(t, bi(1), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "ratioCmpG", []string{u256, u256, u256, u256}, numA, denA, numC, denC)), "ratioCmp full-range => +1")
	// The OLD bounded path TRAPS on the very same inputs (mulDiv cross product
	// overflows uint256), proving ratioGtFull is the overflow-safe replacement.
	callGetterExpectTrap(t, runner, res, avm.Storage{}, "ratioGt", []string{u256, u256, u256, u256}, numA, denA, numC, denC)

	// ---- mulDivSigned: signed, truncated-toward-zero (a*b)/c -----------------
	// -7*3/2 = -21/2 truncates TOWARD ZERO to -10 (not floor -11).
	requireBigEq(t, bi(-10), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "mulDivSignedG", []string{i256, i256, i256}, bi(-7), bi(3), bi(2))), "mulDivSigned -7*3/2 => -10 (trunc toward zero)")
	requireBigEq(t, bi(10), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "mulDivSignedG", []string{i256, i256, i256}, bi(7), bi(3), bi(2))), "mulDivSigned 7*3/2 => 10")
	requireBigEq(t, bi(10), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "mulDivSignedG", []string{i256, i256, i256}, bi(-7), bi(-3), bi(2))), "mulDivSigned -7*-3/2 => 10")
	requireBigEq(t, bi(-10), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "mulDivSignedG", []string{i256, i256, i256}, bi(7), bi(-3), bi(2))), "mulDivSigned 7*-3/2 => -10")
	// Signed funding scale: rate -0.5 (at 1e18) * size 10e18 / 1e18 = -5e18.
	negHalf := new(big.Int).Neg(new(big.Int).Div(e18(), big.NewInt(2))) // -0.5e18
	requireBigEq(t, new(big.Int).Neg(scaled(5)), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "pnlScaleG", []string{i256, i256}, negHalf, scaled(10))), "pnlScale -0.5*10 => -5.0")

	// ---- mulDivSigned by ZERO traps -----------------------------------------
	callGetterExpectTrap(t, runner, res, avm.Storage{}, "mulDivSignedG", []string{i256, i256, i256}, bi(7), bi(3), bi(0))

	// ---- Regression: a NEGATIVE operand to mulCmp TRAPS (deterministic
	// rollback) rather than panicking. mulCmpG's params are uint256, but a
	// negative int256 injected through the wire codec reaches the op as a signed
	// value; mulCmp guards it and fails closed.
	callGetterExpectTrap(t, runner, res, avm.Storage{}, "mulCmpSignedG", []string{i256, u256, u256, u256}, bi(-1), bi(3), bi(3), bi(2))
}

// TestAcceptanceLendingHealthFactor compiles the lending health-factor contract
// and proves the BasisPoints + Decimal18 composition yields the exact health
// factor at 1e18 scale, plus the liquidation predicate.
func TestAcceptanceLendingHealthFactor(t *testing.T) {
	deployer := testAddress(0x92)
	res := compileExampleFile(t, filepath.Join("finance", "health_factor.atlx"), compiler.Options{
		DeployerAddress: addressing.FormatAccAddress(deployer),
	})
	require.NoError(t, avm.VerifyInterface(res.Module, res.Manifest))

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	u256 := "uint256"

	// collateral 1500, debt 1000, threshold 8000bps:
	// adjusted = 1500*8000/10000 = 1200; HF = 1200*1e18/1000 = 1.2e18.
	hf := bigResult(t, callGetter(t, runner, res, avm.Storage{}, "healthFactorOf", []string{u256, u256, u256}, bi(1500), bi(1000), bi(8000)))
	want := new(big.Int).Div(new(big.Int).Mul(big.NewInt(12), e18()), big.NewInt(10)) // 1.2e18
	requireBigEq(t, want, hf, "healthFactorOf")
	require.Equal(t, uint64(0), u64Result(t, callGetter(t, runner, res, avm.Storage{}, "liquidatableOf", []string{u256, u256, u256}, bi(1500), bi(1000), bi(8000))), "healthy => not liquidatable")

	// An underwater position: collateral 1000, debt 1000, threshold 8000bps =>
	// adjusted 800, HF = 0.8e18 < 1.0 => liquidatable.
	hf2 := bigResult(t, callGetter(t, runner, res, avm.Storage{}, "healthFactorOf", []string{u256, u256, u256}, bi(1000), bi(1000), bi(8000)))
	want2 := new(big.Int).Div(new(big.Int).Mul(big.NewInt(8), e18()), big.NewInt(10)) // 0.8e18
	requireBigEq(t, want2, hf2, "healthFactorOf underwater")
	require.Equal(t, uint64(1), u64Result(t, callGetter(t, runner, res, avm.Storage{}, "liquidatableOf", []string{u256, u256, u256}, bi(1000), bi(1000), bi(8000))), "underwater => liquidatable")

	// Realistic 1e18-base-unit sizes the old uint64 pool could not hold:
	// collateral 1500e18, debt 1000e18, threshold 8000 => still HF 1.2e18.
	hf3 := bigResult(t, callGetter(t, runner, res, avm.Storage{}, "healthFactorOf", []string{u256, u256, u256}, scaled(1500), scaled(1000), bi(8000)))
	requireBigEq(t, want, hf3, "healthFactorOf at 1e18 base units")

	// Zero-debt guard: dividing by a zero debt would trap mulDiv, so the pure
	// getters return 0 for HF and treat a zero-debt position as NOT liquidatable.
	requireBigEq(t, bi(0), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "healthFactorOf", []string{u256, u256, u256}, bi(1500), bi(0), bi(8000))), "healthFactorOf debt 0 => 0")
	require.Equal(t, uint64(0), u64Result(t, callGetter(t, runner, res, avm.Storage{}, "liquidatableOf", []string{u256, u256, u256}, bi(1500), bi(0), bi(8000))), "zero debt => not liquidatable")
}

// TestAcceptancePerpPnl compiles the perpetual-PnL contract and proves the
// signed int256 math end to end — both through explicit int256 getter arguments
// and through decoded int256 STORAGE fields.
func TestAcceptancePerpPnl(t *testing.T) {
	deployer := testAddress(0x93)
	res := compileExampleFile(t, filepath.Join("finance", "perp_pnl.atlx"), compiler.Options{
		DeployerAddress: addressing.FormatAccAddress(deployer),
	})
	require.NoError(t, avm.VerifyInterface(res.Module, res.Manifest))

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	i256 := "int256"

	// Long that takes a loss: entry 3000, exit 2500, size +10 => -5000.
	requireBigEq(t, bi(-5000), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "pnlOfArgs", []string{i256, i256, i256}, bi(3000), bi(2500), bi(10))), "long loss pnl")
	require.Equal(t, uint64(1), u64Result(t, callGetter(t, runner, res, avm.Storage{}, "isLoss", []string{i256, i256, i256}, bi(3000), bi(2500), bi(10))), "long loss => isLoss")

	// Long that profits: entry 3000, exit 3500, size +10 => +5000.
	requireBigEq(t, bi(5000), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "pnlOfArgs", []string{i256, i256, i256}, bi(3000), bi(3500), bi(10))), "long profit pnl")
	require.Equal(t, uint64(0), u64Result(t, callGetter(t, runner, res, avm.Storage{}, "isLoss", []string{i256, i256, i256}, bi(3000), bi(3500), bi(10))), "long profit => not isLoss")

	// Short that takes a loss: entry 2500, exit 3000, size -10 => -5000. The
	// negative size arrives as a decoded int256 argument (a negative literal
	// could never be written in ATLX).
	requireBigEq(t, bi(-5000), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "pnlOfArgs", []string{i256, i256, i256}, bi(2500), bi(3000), bi(-10))), "short loss pnl")
	require.Equal(t, uint64(1), u64Result(t, callGetter(t, runner, res, avm.Storage{}, "isLoss", []string{i256, i256, i256}, bi(2500), bi(3000), bi(-10))), "short loss => isLoss")

	// Equity-based liquidation over explicit int256 operands. Liquidation is NOT
	// "any loss": it triggers only when equity (margin + PnL) < maintenanceMargin.
	// entry 3000, exit 2500, size 10 => pnl -5000. With margin 6000, maintenance
	// 2000 => equity 1000 < 2000 => liquidatable. With margin 8000 => equity 3000
	// >= 2000 => NOT liquidatable (a loss with adequate margin survives).
	i5 := []string{i256, i256, i256, i256, i256}
	require.Equal(t, uint64(1), u64Result(t, callGetter(t, runner, res, avm.Storage{}, "liquidatableArgs", i5, bi(3000), bi(2500), bi(10), bi(6000), bi(2000))), "equity 1000 < 2000 => liquidatable")
	require.Equal(t, uint64(0), u64Result(t, callGetter(t, runner, res, avm.Storage{}, "liquidatableArgs", i5, bi(3000), bi(2500), bi(10), bi(8000), bi(2000))), "equity 3000 >= 2000 => not liquidatable")
	// isLoss still reports a loss (pnl < 0) even where the position is NOT
	// liquidatable — the two predicates are distinct.
	require.Equal(t, uint64(1), u64Result(t, callGetter(t, runner, res, avm.Storage{}, "isLoss", []string{i256, i256, i256}, bi(3000), bi(2500), bi(10))), "adequately-margined loss is still a loss")

	// Storage-backed path: inject an int256 position into storage and read the
	// storage-backed pnl()/equity()/liquidatable() getters, proving int256
	// STORAGE-field decode AND signed int256 addition (margin + pnl) in the VM.
	posStorage := avm.Storage{
		"owner":             mustRuntimeValue(t, avm.ValueAddress(addressing.FormatAccAddress(deployer))),
		"entryPrice":        mustRuntimeValue(t, avm.ValueBigInt256(big.NewInt(3000))),
		"exitPrice":         mustRuntimeValue(t, avm.ValueBigInt256(big.NewInt(2500))),
		"size":              mustRuntimeValue(t, avm.ValueBigInt256(big.NewInt(10))),
		"margin":            mustRuntimeValue(t, avm.ValueBigInt256(big.NewInt(6000))),
		"maintenanceMargin": mustRuntimeValue(t, avm.ValueBigInt256(big.NewInt(2000))),
	}
	requireBigEq(t, bi(-5000), bigResult(t, callGetter(t, runner, res, posStorage, "pnl", nil)), "storage-backed pnl")
	requireBigEq(t, bi(1000), bigResult(t, callGetter(t, runner, res, posStorage, "equity", nil)), "storage-backed equity = margin + pnl")
	require.Equal(t, uint64(1), u64Result(t, callGetter(t, runner, res, posStorage, "liquidatable", nil)), "storage equity 1000 < 2000 => liquidatable")

	// Healthy storage position: same PnL but margin 8000 => equity 3000 >= 2000.
	posHealthy := avm.Storage{
		"owner":             mustRuntimeValue(t, avm.ValueAddress(addressing.FormatAccAddress(deployer))),
		"entryPrice":        mustRuntimeValue(t, avm.ValueBigInt256(big.NewInt(3000))),
		"exitPrice":         mustRuntimeValue(t, avm.ValueBigInt256(big.NewInt(2500))),
		"size":              mustRuntimeValue(t, avm.ValueBigInt256(big.NewInt(10))),
		"margin":            mustRuntimeValue(t, avm.ValueBigInt256(big.NewInt(8000))),
		"maintenanceMargin": mustRuntimeValue(t, avm.ValueBigInt256(big.NewInt(2000))),
	}
	require.Equal(t, uint64(0), u64Result(t, callGetter(t, runner, res, posHealthy, "liquidatable", nil)), "storage equity 3000 >= 2000 => not liquidatable")
}

// TestAcceptanceClammSqrtPrice compiles the concentrated-liquidity sqrt-price
// contract and proves the Decimal18 + isqrt round-trip and the bounded ratio
// cross-compare.
func TestAcceptanceClammSqrtPrice(t *testing.T) {
	deployer := testAddress(0x94)
	res := compileExampleFile(t, filepath.Join("finance", "sqrt_price.atlx"), compiler.Options{
		DeployerAddress: addressing.FormatAccAddress(deployer),
	})
	require.NoError(t, avm.VerifyInterface(res.Module, res.Manifest))

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	u256 := "uint256"

	// reserve0 = 1e18, reserve1 = 4e18 => price 4.0 (=4e18 at 1e18 scale).
	requireBigEq(t, scaled(4), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "priceOfArgs", []string{u256, u256}, scaled(1), scaled(4))), "price 4.0")

	// sqrtPrice = isqrt(4e18) = 2e9 (2.0 at 1e9 scale).
	twoE9 := big.NewInt(2_000_000_000)
	requireBigEq(t, twoE9, bigResult(t, callGetter(t, runner, res, avm.Storage{}, "sqrtPriceOfArgs", []string{u256, u256}, scaled(1), scaled(4))), "sqrtPrice 2e9")

	// Round-trip: (2e9)^2 = 4e18, recovering the 1e18-scaled price.
	requireBigEq(t, scaled(4), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "priceRoundTripArgs", []string{u256, u256}, scaled(1), scaled(4))), "round-trip price 4.0")

	// Realistic reserves the old uint64 pool could not hold: 5,000,000 vs
	// 3,000,000 tokens at 1e18 base units => price 0.6 (=0.6e18).
	price := bigResult(t, callGetter(t, runner, res, avm.Storage{}, "priceOfArgs", []string{u256, u256}, scaled(5_000_000), scaled(3_000_000)))
	wantPrice := new(big.Int).Div(new(big.Int).Mul(big.NewInt(6), e18()), big.NewInt(10)) // 0.6e18
	requireBigEq(t, wantPrice, price, "price 0.6 at 1e18 base units")

	// Bounded ratio compare: 3/2 (=1.5) > 1/1 (=1.0) => 1, with the 2^128 safety
	// bound supplied as a uint256 argument (it cannot be an ATLX literal).
	safeMax := new(big.Int).Lsh(big.NewInt(1), 128)
	require.Equal(t, uint64(1), u64Result(t, callGetter(t, runner, res, avm.Storage{}, "priceGt", []string{u256, u256, u256, u256, u256}, bi(2), bi(3), bi(1), bi(1), safeMax)), "3/2 > 1/1")
	require.Equal(t, uint64(0), u64Result(t, callGetter(t, runner, res, avm.Storage{}, "priceGt", []string{u256, u256, u256, u256, u256}, bi(1), bi(1), bi(2), bi(3), safeMax)), "1/1 > 3/2 is false")

	// Zero-reserve0 guard: dividing by a zero reserve0 would trap mulDiv, so the
	// pure argument getters return 0 instead of trapping.
	requireBigEq(t, bi(0), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "priceOfArgs", []string{u256, u256}, bi(0), scaled(4))), "priceOfArgs reserve0 0 => 0")
	requireBigEq(t, bi(0), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "sqrtPriceOfArgs", []string{u256, u256}, bi(0), scaled(4))), "sqrtPriceOfArgs reserve0 0 => 0")
	requireBigEq(t, bi(0), bigResult(t, callGetter(t, runner, res, avm.Storage{}, "priceRoundTripArgs", []string{u256, u256}, bi(0), scaled(4))), "priceRoundTripArgs reserve0 0 => 0")
}

// mustRuntimeValue canonical-encodes a RuntimeValue for injection into a storage
// snapshot map (mirrors the field/value encoding the runtime expects).
func mustRuntimeValue(t *testing.T, v avm.RuntimeValue) []byte {
	t.Helper()
	bz, err := avm.CanonicalEncode(v)
	require.NoError(t, err)
	return bz
}
