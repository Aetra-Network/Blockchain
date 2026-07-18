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

// dexScale is 10^18 (one token in 18-decimal base units), the natural unit for a
// real ERC-20-style pool. Reserves in the millions of tokens are therefore ~1e24
// base units — far beyond what a uint64 field could hold — and the constant-
// product math forms the PRODUCT of two such reserves (~1e48), which overflows
// uint64 by ~29 orders of magnitude. The pre-rewrite dex_amm.atlx (uint64 +
// bare `amountIn*FEE_NUM*reserveOut`) could neither store these reserves nor
// evaluate the quote without trapping.
func dexScale() *big.Int { return new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil) }

func tokens(n int64) *big.Int { return new(big.Int).Mul(big.NewInt(n), dexScale()) }

// quoteOutGo mirrors the contract's quoteOut exactly:
// floor(amountIn*997 * reserveOut / (reserveIn*1000 + amountIn*997)).
func quoteOutGo(amountIn, reserveIn, reserveOut *big.Int) *big.Int {
	inWithFee := new(big.Int).Mul(amountIn, big.NewInt(997))
	num := new(big.Int).Mul(inWithFee, reserveOut)
	den := new(big.Int).Add(new(big.Int).Mul(reserveIn, big.NewInt(1000)), inWithFee)
	return new(big.Int).Quo(num, den)
}

// TestAcceptanceDexRealReserves compiles the rewritten constant-product AMM and
// drives its pure quoteWithReserves getter (which runs the exact quoteOut the
// swap path uses) at reserve sizes the old uint64 contract could not handle:
//  1. realistic reserves (~1e24 base units) whose triple product overflows
//     uint64 — the old contract trapped here; and
//  2. extreme reserves whose triple product amountIn*997*reserveOut overflows
//     even uint256 — proving mulDiv's full-width intermediate is doing real
//     work, not just widening the type.
//
// In both cases the compiled getter returns the mathematically exact output with
// no trap.
func TestAcceptanceDexRealReserves(t *testing.T) {
	deployer := testAddress(0x71)
	res := compileExampleFile(t, filepath.Join("dex", "dex_amm.atlx"), compiler.Options{
		DeployerAddress: addressing.FormatAccAddress(deployer),
	})
	require.NoError(t, avm.VerifyInterface(res.Module, res.Manifest))

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)

	// Getter parameters are read positionally from the query body as arg0/arg1/
	// arg2 (see the ident lowering in compile.go), so drive the getter with a
	// codec whose fields carry those synthetic names.
	argCodec := compiler.Codec{
		Name: "quoteWithReserves",
		Fields: []compiler.CodecField{
			{Name: "arg0", Type: compiler.TypeRef{Name: "uint256"}},
			{Name: "arg1", Type: compiler.TypeRef{Name: "uint256"}},
			{Name: "arg2", Type: compiler.TypeRef{Name: "uint256"}},
		},
	}
	quote := func(amountIn, reserveIn, reserveOut *big.Int) *big.Int {
		body := mustCodecBody(t, argCodec, map[string]any{
			"arg0": amountIn,
			"arg1": reserveIn,
			"arg2": reserveOut,
		})
		op := opcodeForGetter(t, res, "quoteWithReserves")
		exec, err := runner.Run(res.Module, avm.Storage{}, avm.RuntimeContext{
			Entry:    avm.EntryQuery,
			Message:  async.MessageEnvelope{Opcode: op, Body: body, GasLimit: 10_000_000},
			GasLimit: 10_000_000,
		})
		require.NoError(t, err)
		require.Equalf(t, async.ResultOK, exec.ResultCode, "quoteWithReserves trapped (reserves=%s/%s, in=%s)", reserveIn, reserveOut, amountIn)
		v, err := exec.ReturnValue.AsBigInt()
		require.NoError(t, err)
		return v
	}

	// (1) Realistic pool: 5,000,000 / 3,000,000 tokens at 18 decimals, a
	// 1,000-token swap. amountIn*997 alone (~1e21) already overflows uint64, so
	// the old contract trapped before even multiplying by reserveOut.
	amountIn := tokens(1_000)
	reserveIn := tokens(5_000_000)
	reserveOut := tokens(3_000_000)
	got := quote(amountIn, reserveIn, reserveOut)
	want := quoteOutGo(amountIn, reserveIn, reserveOut)
	require.Equal(t, 1, want.Sign(), "sanity: the quote is positive")
	require.Equalf(t, 0, got.Cmp(want), "realistic-reserve quote wrong: got %s want %s", got, want)

	// (2) Extreme pool where amountIn*997*reserveOut overflows uint256. With
	// amountIn = reserveOut = 2^140, the triple product is ~2^290 (> 2^256), so a
	// naive uint256 `a*b*c` would trap; mulDiv forms the product in an unbounded
	// intermediate and the ~2^140 quotient fits uint256 cleanly.
	big140 := new(big.Int).Lsh(big.NewInt(1), 140)
	twoPow256 := new(big.Int).Lsh(big.NewInt(1), 256)
	rawTriple := new(big.Int).Mul(new(big.Int).Mul(big140, big.NewInt(997)), big140)
	require.Equal(t, 1, rawTriple.Cmp(twoPow256), "sanity: the raw triple product must exceed 2^256 so mulDiv is load-bearing")
	gotBig := quote(big140, big140, big140)
	wantBig := quoteOutGo(big140, big140, big140)
	require.Less(t, wantBig.Cmp(twoPow256), 0, "sanity: the quotient still fits uint256")
	require.Equalf(t, 0, gotBig.Cmp(wantBig), "u256-overflow quote wrong: got %s want %s", gotBig, wantBig)
}
