package observability

import (
	"runtime"
	"time"
)

// StartFinalizeObservation begins timing a FinalizeBlock execution and returns
// a completion callback that records every finalize-related metric (block
// gauges, processing duration, finality latency, failed-tx reasons). ALL
// wall-clock reads live here, inside the metrics package, so consensus source
// files stay free of time tokens (enforced by the consensus-source scanner in
// app/security_attack_audit_test.go); the callback returns nothing, so no
// clock-derived value can flow back into consensus logic.
func StartFinalizeObservation(height int64, blockTime time.Time, txCount int) func(failed bool, failedTxCodespaces []string) {
	started := time.Now()
	return func(failed bool, failedTxCodespaces []string) {
		RecordFinalizeBlock(height, blockTime, txCount, time.Since(started))
		if failed {
			RecordModuleError("app", "finalize_block", "error")
			return
		}
		// Finality latency: proposal timestamp to local commit completion.
		if !blockTime.IsZero() {
			if latency := time.Since(blockTime); latency > 0 {
				RecordFinalityLatency(latency.Seconds())
			}
		}
		for _, codespace := range failedTxCodespaces {
			RecordFailedTx(codespace)
		}
	}
}

func RecordFinalizeBlock(height int64, blockTime time.Time, txCount int, duration time.Duration) {
	if height >= 0 {
		SetGauge(MetricBlockHeight, nil, float64(height))
	}
	if !blockTime.IsZero() {
		SetGauge(MetricBlockTimeSeconds, nil, float64(blockTime.Unix()))
	}
	if duration >= 0 {
		Observe(MetricBlockProcessing, Labels{"result": "finalized"}, duration.Seconds())
		if txCount > 0 {
			Observe(MetricTxLatency, Labels{"result": "finalized"}, duration.Seconds()/float64(txCount))
		}
	}
}

func RecordModuleError(module, action, reason string) {
	IncCounter(MetricModuleErrors, Labels{"module": module, "action": action, "reason": reason}, 1)
}

func RecordFeeAccepted() {
	IncCounter(MetricFeesAccepted, Labels{"result": "accepted"}, 1)
}

func RecordFeeRejected(reason string) {
	IncCounter(MetricFeesRejected, Labels{"reason": reason}, 1)
}

func RecordEconomicControl(inflationBps, burnRatioBps, validatorFeeRatioBps int64, deflationGuardActive, queueLimited, rateLimited bool) {
	SetGauge(MetricEconomyInflationBps, nil, float64(inflationBps))
	SetGauge(MetricEconomyBurnRatioBps, nil, float64(burnRatioBps))
	SetGauge(MetricEconomyValidatorFeeRatioBps, nil, float64(validatorFeeRatioBps))
	SetGauge(MetricEconomyDeflationGuard, nil, boolFloat(deflationGuardActive))
	SetGauge(MetricEconomyQueueLimited, nil, boolFloat(queueLimited))
	SetGauge(MetricEconomyRateLimited, nil, boolFloat(rateLimited))
}

func RecordEconomicFlow(totalChargesNaet, burnNaet, treasuryNaet, validatorRewardsNaet int64) {
	SetGauge(MetricEconomyTotalChargesNaet, Labels{"denom": "naet"}, float64(totalChargesNaet))
	SetGauge(MetricEconomyBurnNaet, Labels{"denom": "naet"}, float64(burnNaet))
	SetGauge(MetricEconomyTreasuryNaet, Labels{"denom": "naet"}, float64(treasuryNaet))
	SetGauge(MetricEconomyValidatorRewardsNaet, Labels{"denom": "naet"}, float64(validatorRewardsNaet))
}

func RecordOptimalEconomicState(optimal bool, failedConditionCount int) {
	if failedConditionCount < 0 {
		failedConditionCount = 0
	}
	SetGauge(MetricEconomyOptimalState, nil, boolFloat(optimal))
	SetGauge(MetricEconomyFailedConditions, nil, float64(failedConditionCount))
}

func RecordEconomicInvariants(satisfied bool, failedInvariantCount int) {
	if failedInvariantCount < 0 {
		failedInvariantCount = 0
	}
	SetGauge(MetricEconomyInvariantsSatisfied, nil, boolFloat(satisfied))
	SetGauge(MetricEconomyInvariantFailures, nil, float64(failedInvariantCount))
}

func RecordEconomicRiskControls(weaknessControlsReady bool, missingControlCount, inflationRiskCount, circuitBreakerReasonCount int, circuitBreakerActive bool) {
	if missingControlCount < 0 {
		missingControlCount = 0
	}
	if inflationRiskCount < 0 {
		inflationRiskCount = 0
	}
	if circuitBreakerReasonCount < 0 {
		circuitBreakerReasonCount = 0
	}
	SetGauge(MetricEconomyWeaknessControlsReady, nil, boolFloat(weaknessControlsReady))
	SetGauge(MetricEconomyMissingControls, nil, float64(missingControlCount))
	SetGauge(MetricEconomyInflationRiskCount, nil, float64(inflationRiskCount))
	SetGauge(MetricEconomyCircuitBreakerActive, nil, boolFloat(circuitBreakerActive))
	SetGauge(MetricEconomyCircuitBreakerReasons, nil, float64(circuitBreakerReasonCount))
}

func RecordValidatorEconomics(validatorIncentivesHealthy bool, validatorIncentiveFindingCount int, stakingCentralizationHealthy bool, stakingCentralizationRiskCount int) {
	if validatorIncentiveFindingCount < 0 {
		validatorIncentiveFindingCount = 0
	}
	if stakingCentralizationRiskCount < 0 {
		stakingCentralizationRiskCount = 0
	}
	SetGauge(MetricValidatorIncentivesHealthy, nil, boolFloat(validatorIncentivesHealthy))
	SetGauge(MetricValidatorIncentiveFindings, nil, float64(validatorIncentiveFindingCount))
	SetGauge(MetricStakingCentralizationHealthy, nil, boolFloat(stakingCentralizationHealthy))
	SetGauge(MetricStakingCentralizationRisks, nil, float64(stakingCentralizationRiskCount))
}

func RecordFeeModelEfficiency(healthy bool, riskCount int) {
	if riskCount < 0 {
		riskCount = 0
	}
	SetGauge(MetricFeeModelEfficiencyHealthy, nil, boolFloat(healthy))
	SetGauge(MetricFeeModelEfficiencyRisks, nil, float64(riskCount))
}

func RecordValidatorProfitability(state string, rewardPerVotingPowerNaet int64, profitabilityMarginBps int32) {
	if rewardPerVotingPowerNaet < 0 {
		rewardPerVotingPowerNaet = 0
	}
	if state == "" {
		state = "unknown"
	}
	labels := Labels{"state": state, "denom": "naet"}
	SetGauge(MetricValidatorRewardPerPowerNaet, labels, float64(rewardPerVotingPowerNaet))
	SetGauge(MetricValidatorProfitabilityBps, Labels{"state": state}, float64(profitabilityMarginBps))
}

func RecordSlashingRoute(reason string, penaltyNaet, burnNaet, treasuryNaet, reporterNaet int64) {
	if penaltyNaet < 0 {
		penaltyNaet = 0
	}
	if burnNaet < 0 {
		burnNaet = 0
	}
	if treasuryNaet < 0 {
		treasuryNaet = 0
	}
	if reporterNaet < 0 {
		reporterNaet = 0
	}
	labels := Labels{"reason": reason, "denom": "naet"}
	SetGauge(MetricSlashingPenaltyNaet, labels, float64(penaltyNaet))
	SetGauge(MetricSlashingBurnNaet, labels, float64(burnNaet))
	SetGauge(MetricSlashingTreasuryNaet, labels, float64(treasuryNaet))
	SetGauge(MetricSlashingReporterNaet, labels, float64(reporterNaet))
}

// RecordValidatorConcentration records the live validator-set voting-power
// concentration: a headline concentration figure plus the top-10/20/33 power
// shares (each in basis points, bounded to 0..10000). All are aggregate gauges
// with bounded labels (no per-validator cardinality).
func RecordValidatorConcentration(concentrationBps, top10Bps, top20Bps, top33Bps int64) {
	SetGauge(MetricValidatorConcentrationBps, nil, clampBpsFloat(concentrationBps))
	SetGauge(MetricValidatorTopNPowerBps, Labels{"n": "10"}, clampBpsFloat(top10Bps))
	SetGauge(MetricValidatorTopNPowerBps, Labels{"n": "20"}, clampBpsFloat(top20Bps))
	SetGauge(MetricValidatorTopNPowerBps, Labels{"n": "33"}, clampBpsFloat(top33Bps))
}

// RecordValidatorConcentrationRisks records the count of active concentration
// warnings (bounded gauge), kept separate from the share figures above.
func RecordValidatorConcentrationRisks(warningCount int) {
	if warningCount < 0 {
		warningCount = 0
	}
	SetGauge(MetricValidatorConcentrationRisks, nil, float64(warningCount))
}

// RecordBondedRatio records the fraction of total supply that is bonded, in
// basis points (0..10000).
func RecordBondedRatio(bondedRatioBps int64) {
	SetGauge(MetricEconomyBondedRatioBps, nil, clampBpsFloat(bondedRatioBps))
}

// RecordContractExecutionGas records the gas consumed by one AVM contract
// execution, labeled by VM and a bounded result (success or revert). The caller
// passes a uint64 so no floating-point value appears at the call site (the
// x/contracts consensus path is inside the determinism gate's float-free zone);
// the conversion happens here.
func RecordContractExecutionGas(vm, result string, gasUsed uint64) {
	if vm == "" {
		vm = "avm"
	}
	if result == "" {
		result = "unknown"
	}
	Observe(MetricContractExecutionGas, Labels{"vm": vm, "result": result}, float64(gasUsed))
}

// RecordFinalityLatency records the observed proposal-time-to-local-commit
// latency for a finalized block. Wall-clock based; feeds only the process-local
// metrics registry, never consensus.
func RecordFinalityLatency(seconds float64) {
	if seconds < 0 {
		return
	}
	Observe(MetricFinalityLatencySeconds, Labels{"phase": "finalize"}, seconds)
}

// RecordFailedTx counts a failed transaction from a finalized block, labeled by
// its error codespace (bounded: codespaces are module names).
func RecordFailedTx(codespace string) {
	if codespace == "" {
		codespace = "unknown"
	}
	IncCounter(MetricFailedTxReasons, Labels{"codespace": codespace}, 1)
}

// RecordBurnedTotal records the cumulative protocol coins burned in naet (fee
// routing plus the emission burn bucket), from x/burn's by-denom ledger.
func RecordBurnedTotal(naet int64) {
	if naet < 0 {
		return
	}
	SetGauge(MetricEconomyBurnedFeesNaet, Labels{"denom": "naet"}, float64(naet))
}

// RecordTreasuryBalance records the treasury module account's spendable balance
// in naet.
func RecordTreasuryBalance(naet int64) {
	if naet < 0 {
		return
	}
	SetGauge(MetricEconomyTreasuryBalanceNaet, Labels{"denom": "naet"}, float64(naet))
}

// RecordEstimatedAPR records the estimated gross staking APR in basis points
// (before validator commissions). Not clamped to 10000: APR can legitimately
// exceed 100% when bonded stake is small relative to emissions.
func RecordEstimatedAPR(aprBps int64) {
	if aprBps < 0 {
		aprBps = 0
	}
	SetGauge(MetricEconomyEstimatedAPRBps, nil, float64(aprBps))
}

// RecordValidatorUptime records the minimum and average uptime across the
// bonded validator set, in basis points over the slashing signed-blocks window.
func RecordValidatorUptime(minBps, avgBps int64) {
	SetGauge(MetricValidatorUptimeBps, Labels{"stat": "min"}, clampBpsFloat(minBps))
	SetGauge(MetricValidatorUptimeBps, Labels{"stat": "avg"}, clampBpsFloat(avgBps))
}

// RecordValidatorMissedBlocks counts newly observed missed blocks, aggregated
// across the validator set (positive deltas of the slashing signing-info
// missed-blocks counters between observations).
func RecordValidatorMissedBlocks(delta int64) {
	if delta <= 0 {
		return
	}
	IncCounter(MetricValidatorMissedBlocks, Labels{"scope": "validator_set"}, float64(delta))
}

// RecordValidatorJailEvent counts an observed jail transition, labeled by the
// inferred reason (downtime or double_sign).
func RecordValidatorJailEvent(reason string) {
	IncCounter(MetricValidatorJailEventsTotal, Labels{"reason": boundedSlashReason(reason)}, 1)
}

// RecordValidatorUnjailEvent counts an observed unjail transition.
func RecordValidatorUnjailEvent() {
	IncCounter(MetricValidatorUnjailEventsTotal, Labels{"reason": "unjail"}, 1)
}

// RecordSlashingEvent counts an observed slashing event, labeled by the
// inferred reason (downtime or double_sign).
func RecordSlashingEvent(reason string) {
	IncCounter(MetricSlashingEventsTotal, Labels{"reason": boundedSlashReason(reason)}, 1)
}

func boundedSlashReason(reason string) string {
	switch reason {
	case "downtime", "double_sign":
		return reason
	default:
		return "other"
	}
}

func clampBpsFloat(bps int64) float64 {
	if bps < 0 {
		return 0
	}
	if bps > 10000 {
		return 10000
	}
	return float64(bps)
}

func (r *Registry) collectRuntime() {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	r.SetGauge(MetricProcessUptimeSeconds, nil, time.Since(r.startedAt).Seconds())
	r.SetGauge(MetricProcessMemoryBytes, Labels{"type": "alloc"}, float64(mem.Alloc))
	r.SetGauge(MetricProcessGoroutines, nil, float64(runtime.NumGoroutine()))
}
