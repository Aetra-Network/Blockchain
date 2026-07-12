package app

import (
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	appparams "github.com/sovereign-l1/l1/app/params"
	"github.com/sovereign-l1/l1/observability"
)

// validatorHealthMetricInterval throttles the validator-health observability
// sweep so the whole validator set is not iterated on every block. The sweep is
// a pure side-effect (Prometheus gauges) and never touches the store.
const validatorHealthMetricInterval = 20

// recordValidatorObservabilityMetrics records aggregate validator-set health
// gauges (voting-power concentration, top-N power shares, bonded ratio) from
// committed state.
//
// It is deterministic — it reads only committed state and writes nothing to the
// KV store, so it cannot affect the AppHash — and it is defensively recovered so
// a metrics bug can never halt the chain from EndBlock. It is a no-op when
// telemetry is disabled (the observability registry ignores writes) and when
// there is no bonded validator set yet.
func (app *L1App) recordValidatorObservabilityMetrics(ctx sdk.Context) {
	defer func() {
		if r := recover(); r != nil {
			ctx.Logger().Error("validator observability metrics sweep panicked; skipping", "recover", r)
		}
	}()

	height := ctx.BlockHeight()
	if height <= 0 || height%validatorHealthMetricInterval != 0 {
		return
	}

	// GetBondedValidatorsByPower returns the bonded set sorted by descending
	// power, so the first N entries are the top N validators. Read it once and
	// derive both the concentration shares and the bonded total from it.
	validators, err := app.StakingKeeper.GetBondedValidatorsByPower(ctx)
	if err != nil || len(validators) == 0 {
		return
	}
	tokensDesc := make([]math.Int, len(validators))
	totalBonded := math.ZeroInt()
	for i, v := range validators {
		tokensDesc[i] = v.Tokens
		if !v.Tokens.IsNil() {
			totalBonded = totalBonded.Add(v.Tokens)
		}
	}

	if top10, top20, top33, ok := topNPowerSharesBps(tokensDesc); ok {
		// The headline concentration figure is the top-33 share — the closest
		// proxy to the one-third voting-power threshold at which a colluding
		// subset can halt the chain.
		observability.RecordValidatorConcentration(top33, top10, top20, top33)
	}

	// Bonded ratio: total bonded stake over total supply of the bond denom.
	supply := app.BankKeeper.GetSupply(ctx, appparams.BaseDenom).Amount
	if bps, ok := ratioBps(totalBonded, supply); ok {
		observability.RecordBondedRatio(bps)
	}
}

// topNPowerSharesBps computes the top-10/20/33 voting-power shares, each in basis
// points, from a power-descending list of validator token amounts. Concentration
// is measured over token stake, which is a floor-free proxy for consensus power
// (power is a scaled-and-floored function of tokens). ok is false when the input
// is empty or the total is non-positive.
func topNPowerSharesBps(tokensDesc []math.Int) (top10, top20, top33 int64, ok bool) {
	if len(tokensDesc) == 0 {
		return 0, 0, 0, false
	}
	total := math.ZeroInt()
	for _, t := range tokensDesc {
		if t.IsNil() {
			continue
		}
		total = total.Add(t)
	}
	if !total.IsPositive() {
		return 0, 0, 0, false
	}
	topN := func(n int) int64 {
		if n > len(tokensDesc) {
			n = len(tokensDesc)
		}
		sum := math.ZeroInt()
		for i := 0; i < n; i++ {
			if tokensDesc[i].IsNil() {
				continue
			}
			sum = sum.Add(tokensDesc[i])
		}
		share, shareOK := ratioBps(sum, total)
		if !shareOK {
			return 10_000
		}
		return share
	}
	return topN(10), topN(20), topN(33), true
}

// ratioBps returns numerator/denominator in basis points (0..10000), bounded.
// ok is false when the denominator is non-positive or the result cannot be
// represented (in which case the caller should skip rather than emit garbage).
func ratioBps(numerator, denominator math.Int) (int64, bool) {
	if numerator.IsNil() || denominator.IsNil() || !denominator.IsPositive() {
		return 0, false
	}
	if numerator.IsNegative() {
		return 0, true
	}
	bps := numerator.MulRaw(10_000).Quo(denominator)
	if !bps.IsInt64() {
		return 0, false
	}
	v := bps.Int64()
	if v > 10_000 {
		v = 10_000
	}
	return v, true
}
