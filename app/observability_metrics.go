package app

import (
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	appparams "github.com/sovereign-l1/l1/app/params"
	"github.com/sovereign-l1/l1/observability"
	feecollectortypes "github.com/sovereign-l1/l1/x/fee-collector/types"
)

// stakingValidator aliases the staking validator record used by the
// observability sweep helpers.
type stakingValidator = stakingtypes.Validator

// validatorHealthMetricInterval throttles the validator-health observability
// sweep so the whole validator set is not iterated on every block. The sweep is
// a pure side-effect (Prometheus gauges) and never touches the store.
const validatorHealthMetricInterval = 20

// validatorHealthObsSnapshot is the process-local snapshot the observability
// sweep diffs against to derive event counters (missed blocks, jail/unjail,
// slashing). It is metrics-only state: never persisted, never read by consensus
// logic, and empty after a restart (the first sweep only initializes it, so no
// spurious events are counted across restarts).
type validatorHealthObsSnapshot struct {
	initialized		bool
	missedByCons		map[string]int64
	jailedByOp		map[string]bool
	tombstonedByCons	map[string]bool
}

// validatorHealthTransitions is the pure diff between two snapshots.
type validatorHealthTransitions struct {
	missedDelta		int64
	downtimeJails		int
	doubleSignJails		int
	unjails			int
	downtimeSlashes		int
	doubleSignSlashes	int
}

// recordValidatorObservabilityMetrics records aggregate validator-set health
// and economic gauges (voting-power concentration, top-N power shares, bonded
// ratio, uptime, burned total, treasury balance, estimated APR) plus
// transition-derived counters (missed blocks, jail/unjail, slashing events)
// from committed state.
//
// It is deterministic in its reads — it reads only committed state and writes
// nothing to the KV store, so it cannot affect the AppHash — and it is
// defensively recovered so a metrics bug can never halt the chain from
// EndBlock. It is a no-op when telemetry is disabled (the observability
// registry ignores writes).
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

	// Economic gauges that do not depend on the validator set.
	if entry, found, err := app.BurnKeeper.GetBurnedDenomEntry(ctx, appparams.BaseDenom); err == nil && found {
		if amt := entry.Amount.AmountOf(appparams.BaseDenom); amt.IsInt64() {
			observability.RecordBurnedTotal(amt.Int64())
		}
	}
	treasuryAddr := authtypes.NewModuleAddress(feecollectortypes.TreasuryModuleName)
	if bal := app.BankKeeper.GetBalance(ctx, treasuryAddr, appparams.BaseDenom).Amount; bal.IsInt64() {
		observability.RecordTreasuryBalance(bal.Int64())
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

	// Estimated gross staking APR (before validator commissions) from the
	// native-emission parameters: annual validator-bucket emission over bonded
	// stake.
	if params, err := app.EmissionsKeeper.GetParams(ctx); err == nil {
		if aprBps, ok := estimatedGrossAPRBps(
			params.AnnualReferenceSupply.Amount,
			int64(params.CurrentInflationBps),
			int64(params.DistributionWeights.ValidatorRewardBps),
			totalBonded,
		); ok {
			observability.RecordEstimatedAPR(aprBps)
		}
	}

	app.recordValidatorSigningMetrics(ctx, validators)
}

// recordValidatorSigningMetrics derives uptime gauges and transition counters
// (missed blocks, jail/unjail, slashing) from x/slashing signing infos and the
// staking validator records, diffing against the process-local snapshot.
func (app *L1App) recordValidatorSigningMetrics(ctx sdk.Context, bondedValidators []stakingValidator) {
	window, err := app.SlashingKeeper.SignedBlocksWindow(ctx)
	if err != nil || window <= 0 {
		return
	}

	missedByCons := map[string]int64{}
	tombstonedByCons := map[string]bool{}
	err = app.SlashingKeeper.IterateValidatorSigningInfos(ctx, func(addr sdk.ConsAddress, info slashingtypes.ValidatorSigningInfo) bool {
		missedByCons[string(addr)] = info.MissedBlocksCounter
		tombstonedByCons[string(addr)] = info.Tombstoned
		return false
	})
	if err != nil {
		return
	}

	// Uptime over the CURRENTLY BONDED set only: jailed/unbonded validators have
	// reset or stale counters that would skew the aggregate. A bonded validator
	// without a signing info yet counts as fully up (missed=0).
	minUptime, avgUptime, ok := bondedUptimeBps(bondedValidators, missedByCons, window)
	if ok {
		observability.RecordValidatorUptime(minUptime, avgUptime)
	}

	allValidators, err := app.StakingKeeper.GetAllValidators(ctx)
	if err != nil {
		return
	}
	jailedByOp := make(map[string]bool, len(allValidators))
	tombstonedByOp := make(map[string]bool, len(allValidators))
	for _, v := range allValidators {
		jailedByOp[v.OperatorAddress] = v.Jailed
		if consBz, consErr := v.GetConsAddr(); consErr == nil {
			tombstonedByOp[v.OperatorAddress] = tombstonedByCons[string(consBz)]
		}
	}

	current := validatorHealthObsSnapshot{
		initialized:		true,
		missedByCons:		missedByCons,
		jailedByOp:		jailedByOp,
		tombstonedByCons:	tombstonedByCons,
	}
	if app.validatorHealthObs != nil && app.validatorHealthObs.initialized {
		transitions := diffValidatorHealth(*app.validatorHealthObs, current, tombstonedByOp)
		observability.RecordValidatorMissedBlocks(transitions.missedDelta)
		for i := 0; i < transitions.downtimeJails; i++ {
			observability.RecordValidatorJailEvent("downtime")
		}
		for i := 0; i < transitions.doubleSignJails; i++ {
			observability.RecordValidatorJailEvent("double_sign")
		}
		for i := 0; i < transitions.unjails; i++ {
			observability.RecordValidatorUnjailEvent()
		}
		for i := 0; i < transitions.downtimeSlashes; i++ {
			observability.RecordSlashingEvent("downtime")
		}
		for i := 0; i < transitions.doubleSignSlashes; i++ {
			observability.RecordSlashingEvent("double_sign")
		}
	}
	app.validatorHealthObs = &current
}

// diffValidatorHealth computes transition counters between two observability
// snapshots. Pure function; transition semantics (all at sweep granularity —
// a jail+unjail cycle completing entirely between sweeps is not observed):
//   - missedDelta: sum of positive per-validator missed-blocks counter deltas
//     (window slides and jail resets shrink counters; only growth counts).
//   - a newly tombstoned consensus address is a double-sign slash; a newly
//     jailed operator whose consensus address is NOT tombstoned is a downtime
//     jail+slash (in x/slashing a downtime jail always accompanies a downtime
//     slash); a newly jailed operator that IS tombstoned is a double-sign jail
//     (its slash is already counted by the tombstone transition).
func diffValidatorHealth(prev, current validatorHealthObsSnapshot, tombstonedByOp map[string]bool) validatorHealthTransitions {
	out := validatorHealthTransitions{}
	for cons, missed := range current.missedByCons {
		if delta := missed - prev.missedByCons[cons]; delta > 0 {
			out.missedDelta += delta
		}
	}
	for cons, tombstoned := range current.tombstonedByCons {
		if tombstoned && !prev.tombstonedByCons[cons] {
			out.doubleSignSlashes++
		}
	}
	for op, jailed := range current.jailedByOp {
		wasJailed := prev.jailedByOp[op]
		switch {
		case jailed && !wasJailed:
			if tombstonedByOp[op] {
				out.doubleSignJails++
			} else {
				out.downtimeJails++
				out.downtimeSlashes++
			}
		case !jailed && wasJailed:
			out.unjails++
		}
	}
	return out
}

// bondedUptimeBps computes the minimum and average uptime (in basis points over
// the signed-blocks window) across the bonded validator set. ok is false when
// there are no bonded validators or the window is non-positive.
func bondedUptimeBps(bonded []stakingValidator, missedByCons map[string]int64, window int64) (minBps, avgBps int64, ok bool) {
	if len(bonded) == 0 || window <= 0 {
		return 0, 0, false
	}
	minBps = 10_000
	total := int64(0)
	counted := 0
	for _, v := range bonded {
		consBz, err := v.GetConsAddr()
		if err != nil {
			continue
		}
		missed := missedByCons[string(consBz)]
		if missed < 0 {
			missed = 0
		}
		if missed > window {
			missed = window
		}
		uptime := (window - missed) * 10_000 / window
		if uptime < minBps {
			minBps = uptime
		}
		total += uptime
		counted++
	}
	if counted == 0 {
		return 0, 0, false
	}
	return minBps, total / int64(counted), true
}

// estimatedGrossAPRBps estimates the gross staking APR (before validator
// commissions) in basis points: the annual validator-bucket emission
// (annual reference supply x inflation x validator reward share) over total
// bonded stake. ok is false when any input is non-positive or the result
// cannot be represented.
func estimatedGrossAPRBps(annualReferenceSupply math.Int, inflationBps, validatorShareBps int64, totalBonded math.Int) (int64, bool) {
	if annualReferenceSupply.IsNil() || !annualReferenceSupply.IsPositive() ||
		inflationBps <= 0 || validatorShareBps <= 0 ||
		totalBonded.IsNil() || !totalBonded.IsPositive() {
		return 0, false
	}
	annualValidatorRewards := annualReferenceSupply.
		MulRaw(inflationBps).
		MulRaw(validatorShareBps).
		QuoRaw(10_000 * 10_000)
	aprBps := annualValidatorRewards.MulRaw(10_000).Quo(totalBonded)
	if !aprBps.IsInt64() {
		return 0, false
	}
	v := aprBps.Int64()
	if v < 0 {
		return 0, false
	}
	return v, true
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
