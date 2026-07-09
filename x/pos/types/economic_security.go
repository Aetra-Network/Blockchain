package types

import (
	"errors"
	"fmt"
	"sort"

	sdkmath "cosmossdk.io/math"
)

const (
	CentralizationWarningValidatorShare        = "validator_voting_power_concentration"
	CentralizationWarningTopNShare             = "top_n_voting_power_concentration"
	CentralizationWarningStakeSaturation       = "stake_saturation_ratio"
	CentralizationWarningDelegationRisk        = "delegation_risk_concentration"
	CentralizationWarningRewardDampeningActive = "reward_dampening_active"
	CentralizationWarningTaskAssignmentShare   = "task_assignment_concentration"
	CentralizationWarningBootstrapEligible     = "bootstrap_eligible_reliable_validator"

	ConcentrationAlertSeverityWarning  = "warning"
	ConcentrationAlertSeverityCritical = "critical"
)

type EconomicSecurityInput struct {
	Validators              []ScoredValidator
	RiskWindows             []RiskWindowRecord
	StakeAtRiskNaet         sdkmath.Int
	TopN                    uint32
	ParticipatingValidators uint64
	EligibleValidators      uint64
	AcceptedSlashEvents     uint64
	DetectedFaultEvents     uint64
	AcceptedEvidence        uint64
	SubmittedEvidence       uint64
	CompletedTasks          uint64
	ExpectedTasks           uint64
}

type DelegationRiskBucket struct {
	ValidatorAddress string
	ExposureNaet     sdkmath.Int
	RiskWindowCount  uint64
}

type EconomicSecurityMetrics struct {
	TotalBondedStakeNaet            sdkmath.Int
	EffectiveStakeNaet              sdkmath.Int
	TotalStakeAtRiskNaet            sdkmath.Int
	StakeSaturationRatioBps         uint32
	TopN                            uint32
	TopNVotingPowerConcentrationBps uint32
	ParticipationRateBps            uint32
	SlashingEfficiencyBps           uint32
	EvidenceAcceptanceRateBps       uint32
	AverageValidatorScore           sdkmath.Int
	DelegationRiskDistribution      []DelegationRiskBucket
	TaskCompletionRateBps           uint32
	SecurityNaet                    sdkmath.Int
}

type SecurityMetricQuery struct {
	Input EconomicSecurityInput
}

type SecurityMetricQueryResult struct {
	Metrics EconomicSecurityMetrics
}

type CentralizationControlParams struct {
	MaxValidatorShareBps            uint32
	MaxTopNConcentrationBps         uint32
	MaxStakeSaturationRatioBps      uint32
	MaxDelegationRiskBucketBps      uint32
	MinBootstrapPerformanceBps      uint32
	MinBootstrapReliabilityBps      uint32
	MaxTaskAssignmentShareBps       uint32
	BootstrapMaxVotingPowerShareBps uint32
}

type CentralizationTaskAssignment struct {
	TaskGroupID      string
	ValidatorAddress string
	AssignmentCount  uint64
}

type CentralizationValidatorControl struct {
	ValidatorAddress    string
	VotingPowerShareBps uint32
	EffectiveStakeNaet  sdkmath.Int
	SaturatedStakeNaet  sdkmath.Int
	RewardDampeningBps  uint32
	BootstrapEligible   bool
	Warnings            []string
}

type DelegationRiskWarning struct {
	ValidatorAddress string
	ExposureNaet     sdkmath.Int
	ExposureShareBps uint32
	ThresholdBps     uint32
}

type TaskAssignmentDiversityReport struct {
	TotalAssignments        uint64
	MaxValidatorAssignments uint64
	MaxValidatorAddress     string
	MaxAssignmentShareBps   uint32
	DiversityScoreBps       uint32
	Warnings                []string
}

type ConcentrationInvariantAlert struct {
	AlertType    string
	Severity     string
	ObservedBps  uint32
	ThresholdBps uint32
}

type CentralizationDashboardInput struct {
	SecurityInput   EconomicSecurityInput
	ControlParams   CentralizationControlParams
	TaskAssignments []CentralizationTaskAssignment
}

type CentralizationDashboardData struct {
	Metrics                 EconomicSecurityMetrics
	ValidatorControls       []CentralizationValidatorControl
	DelegationRiskWarnings  []DelegationRiskWarning
	TaskAssignmentDiversity TaskAssignmentDiversityReport
	Alerts                  []ConcentrationInvariantAlert
}

type StakeConcentrationSimulationInput struct {
	Params                  Params
	Candidates              []Candidate
	TargetValidatorID       string
	AddedDelegatedStakeNaet sdkmath.Int
	TopN                    uint32
}

type StakeConcentrationSimulationResult struct {
	Before                        EconomicSecurityMetrics
	After                         EconomicSecurityMetrics
	TopNConcentrationDeltaBps     int32
	TargetEffectiveStakeDeltaNaet sdkmath.Int
	Alerts                        []ConcentrationInvariantAlert
}

type StakeSplittingSimulationInput struct {
	Params     Params
	Candidate  Candidate
	SplitCount uint32
	TopN       uint32
}

type StakeSplittingSimulationResult struct {
	SingleEffectiveStakeNaet sdkmath.Int
	SplitEffectiveStakeNaet  sdkmath.Int
	EffectiveStakeGainNaet   sdkmath.Int
	SingleConcentrationBps   uint32
	SplitConcentrationBps    uint32
}

func (p CentralizationControlParams) Validate() error {
	checks := []struct {
		name  string
		value uint32
	}{
		{name: "max_validator_share_bps", value: p.MaxValidatorShareBps},
		{name: "max_top_n_concentration_bps", value: p.MaxTopNConcentrationBps},
		{name: "max_stake_saturation_ratio_bps", value: p.MaxStakeSaturationRatioBps},
		{name: "max_delegation_risk_bucket_bps", value: p.MaxDelegationRiskBucketBps},
		{name: "min_bootstrap_performance_bps", value: p.MinBootstrapPerformanceBps},
		{name: "min_bootstrap_reliability_bps", value: p.MinBootstrapReliabilityBps},
		{name: "max_task_assignment_share_bps", value: p.MaxTaskAssignmentShareBps},
		{name: "bootstrap_max_voting_power_share_bps", value: p.BootstrapMaxVotingPowerShareBps},
	}
	for _, check := range checks {
		if check.value == 0 || check.value > BasisPoints {
			return fmt.Errorf("%s must be within 1..%d bps", check.name, BasisPoints)
		}
	}
	return nil
}

func ComputeEconomicSecurityMetrics(input EconomicSecurityInput) (EconomicSecurityMetrics, error) {
	if len(input.Validators) == 0 {
		return EconomicSecurityMetrics{}, errors.New("economic security validators are required")
	}
	if input.TopN == 0 {
		return EconomicSecurityMetrics{}, errors.New("economic security top-n must be positive")
	}
	if input.ParticipatingValidators > input.EligibleValidators {
		return EconomicSecurityMetrics{}, errors.New("participating validators cannot exceed eligible validators")
	}
	if input.AcceptedSlashEvents > input.DetectedFaultEvents {
		return EconomicSecurityMetrics{}, errors.New("accepted slash events cannot exceed detected fault events")
	}
	if input.AcceptedEvidence > input.SubmittedEvidence {
		return EconomicSecurityMetrics{}, errors.New("accepted evidence cannot exceed submitted evidence")
	}
	if input.StakeAtRiskNaet.IsNil() {
		input.StakeAtRiskNaet = sdkmath.ZeroInt()
	}
	if input.StakeAtRiskNaet.IsNegative() {
		return EconomicSecurityMetrics{}, errors.New("stake at risk cannot be negative")
	}
	taskCompletion, err := ComputeTaskCompletionRate(TaskCompletionRateInput{
		CompletedAssignedTasks: input.CompletedTasks,
		ExpectedAssignedTasks:  input.ExpectedTasks,
	})
	if err != nil {
		return EconomicSecurityMetrics{}, err
	}

	totalBonded := sdkmath.ZeroInt()
	effectiveStake := sdkmath.ZeroInt()
	saturatedStake := sdkmath.ZeroInt()
	totalScore := sdkmath.ZeroInt()
	totalVotingPower := sdkmath.ZeroInt()
	for _, validator := range input.Validators {
		if err := validateScoredValidatorForSecurity(validator); err != nil {
			return EconomicSecurityMetrics{}, err
		}
		totalBonded = totalBonded.Add(validator.TotalStakeNaet)
		effectiveStake = effectiveStake.Add(validator.EffectiveStakeNaet)
		saturatedStake = saturatedStake.Add(validator.ScoreComponents.SaturatedStakeNaet)
		totalScore = totalScore.Add(validator.Score)
		totalVotingPower = totalVotingPower.Add(validator.VotingPowerNaet)
	}

	stakeAtRisk := input.StakeAtRiskNaet
	riskDistribution, riskExposure, err := BuildDelegationRiskDistribution(input.RiskWindows)
	if err != nil {
		return EconomicSecurityMetrics{}, err
	}
	if !stakeAtRisk.IsPositive() {
		stakeAtRisk = riskExposure
	}
	if !stakeAtRisk.IsPositive() {
		stakeAtRisk = totalBonded
	}

	participation := ratioBps(input.ParticipatingValidators, input.EligibleValidators)
	slashingEfficiency := ratioBps(input.AcceptedSlashEvents, input.DetectedFaultEvents)
	security := mulIntBps(stakeAtRisk, participation)
	security = mulIntBps(security, slashingEfficiency)

	return EconomicSecurityMetrics{
		TotalBondedStakeNaet:            totalBonded,
		EffectiveStakeNaet:              effectiveStake,
		TotalStakeAtRiskNaet:            stakeAtRisk,
		StakeSaturationRatioBps:         intRatioBps(saturatedStake, totalBonded),
		TopN:                            input.TopN,
		TopNVotingPowerConcentrationBps: TopNVotingPowerConcentrationBps(input.Validators, input.TopN),
		ParticipationRateBps:            participation,
		SlashingEfficiencyBps:           slashingEfficiency,
		EvidenceAcceptanceRateBps:       ratioBps(input.AcceptedEvidence, input.SubmittedEvidence),
		AverageValidatorScore:           totalScore.QuoRaw(int64(len(input.Validators))),
		DelegationRiskDistribution:      riskDistribution,
		TaskCompletionRateBps:           taskCompletion,
		SecurityNaet:                    security,
	}, nil
}

func QuerySecurityMetrics(query SecurityMetricQuery) (SecurityMetricQueryResult, error) {
	metrics, err := ComputeEconomicSecurityMetrics(query.Input)
	if err != nil {
		return SecurityMetricQueryResult{}, err
	}
	return SecurityMetricQueryResult{Metrics: metrics}, nil
}

func DefaultCentralizationControlParams(params Params) CentralizationControlParams {
	maxValidatorShare := params.MaxVotingPowerBps
	if maxValidatorShare == 0 {
		maxValidatorShare = DefaultMaxVotingPowerBps
	}
	return CentralizationControlParams{
		MaxValidatorShareBps:            maxValidatorShare,
		MaxTopNConcentrationBps:         6_700,
		MaxStakeSaturationRatioBps:      2_500,
		MaxDelegationRiskBucketBps:      5_000,
		MinBootstrapPerformanceBps:      9_000,
		MinBootstrapReliabilityBps:      9_000,
		MaxTaskAssignmentShareBps:       5_000,
		BootstrapMaxVotingPowerShareBps: maxValidatorShare / 2,
	}
}

func BuildCentralizationDashboard(input CentralizationDashboardInput) (CentralizationDashboardData, error) {
	params := input.ControlParams
	if err := params.Validate(); err != nil {
		return CentralizationDashboardData{}, err
	}
	metrics, err := ComputeEconomicSecurityMetrics(input.SecurityInput)
	if err != nil {
		return CentralizationDashboardData{}, err
	}
	taskDiversity, err := BuildTaskAssignmentDiversityReport(input.TaskAssignments, params.MaxTaskAssignmentShareBps)
	if err != nil {
		return CentralizationDashboardData{}, err
	}
	alerts := ValidateConcentrationInvariants(metrics, params)
	if taskDiversity.MaxAssignmentShareBps > params.MaxTaskAssignmentShareBps {
		alerts = append(alerts, ConcentrationInvariantAlert{
			AlertType:    CentralizationWarningTaskAssignmentShare,
			Severity:     ConcentrationAlertSeverityWarning,
			ObservedBps:  taskDiversity.MaxAssignmentShareBps,
			ThresholdBps: params.MaxTaskAssignmentShareBps,
		})
	}
	return CentralizationDashboardData{
		Metrics:                 metrics,
		ValidatorControls:       BuildCentralizationValidatorControls(input.SecurityInput.Validators, metrics, params),
		DelegationRiskWarnings:  BuildDelegationRiskWarnings(metrics.DelegationRiskDistribution, metrics.TotalStakeAtRiskNaet, params.MaxDelegationRiskBucketBps),
		TaskAssignmentDiversity: taskDiversity,
		Alerts:                  alerts,
	}, nil
}

func BuildCentralizationValidatorControls(validators []ScoredValidator, metrics EconomicSecurityMetrics, params CentralizationControlParams) []CentralizationValidatorControl {
	out := make([]CentralizationValidatorControl, 0, len(validators))
	for _, validator := range validators {
		share := intRatioBps(validator.VotingPowerNaet, metrics.EffectiveStakeNaet)
		dampening := RewardDampeningAboveSoftCapBps(share, params.MaxValidatorShareBps)
		warnings := make([]string, 0)
		if share > params.MaxValidatorShareBps {
			warnings = append(warnings, CentralizationWarningValidatorShare)
		}
		if validator.ScoreComponents.SaturatedStakeNaet.IsPositive() {
			warnings = append(warnings, CentralizationWarningStakeSaturation)
		}
		if dampening > 0 {
			warnings = append(warnings, CentralizationWarningRewardDampeningActive)
		}
		bootstrap := IsBootstrapEligibleReliableValidator(validator, share, params)
		if bootstrap {
			warnings = append(warnings, CentralizationWarningBootstrapEligible)
		}
		out = append(out, CentralizationValidatorControl{
			ValidatorAddress:    validator.ValidatorID,
			VotingPowerShareBps: share,
			EffectiveStakeNaet:  validator.EffectiveStakeNaet,
			SaturatedStakeNaet:  validator.ScoreComponents.SaturatedStakeNaet,
			RewardDampeningBps:  dampening,
			BootstrapEligible:   bootstrap,
			Warnings:            warnings,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].VotingPowerShareBps != out[j].VotingPowerShareBps {
			return out[i].VotingPowerShareBps > out[j].VotingPowerShareBps
		}
		return out[i].ValidatorAddress < out[j].ValidatorAddress
	})
	return out
}

func BuildDelegationRiskWarnings(distribution []DelegationRiskBucket, totalStakeAtRisk sdkmath.Int, thresholdBps uint32) []DelegationRiskWarning {
	warnings := make([]DelegationRiskWarning, 0)
	for _, bucket := range distribution {
		share := intRatioBps(bucket.ExposureNaet, totalStakeAtRisk)
		if share <= thresholdBps {
			continue
		}
		warnings = append(warnings, DelegationRiskWarning{
			ValidatorAddress: bucket.ValidatorAddress,
			ExposureNaet:     bucket.ExposureNaet,
			ExposureShareBps: share,
			ThresholdBps:     thresholdBps,
		})
	}
	return warnings
}

func BuildTaskAssignmentDiversityReport(assignments []CentralizationTaskAssignment, maxShareBps uint32) (TaskAssignmentDiversityReport, error) {
	if maxShareBps == 0 || maxShareBps > BasisPoints {
		return TaskAssignmentDiversityReport{}, fmt.Errorf("max task assignment share must be within 1..%d bps", BasisPoints)
	}
	byValidator := make(map[string]uint64)
	total := uint64(0)
	for _, assignment := range assignments {
		if err := validatePosToken("task assignment validator address", assignment.ValidatorAddress); err != nil {
			return TaskAssignmentDiversityReport{}, err
		}
		if err := validatePosToken("task group id", assignment.TaskGroupID); err != nil {
			return TaskAssignmentDiversityReport{}, err
		}
		if assignment.AssignmentCount == 0 {
			return TaskAssignmentDiversityReport{}, errors.New("task assignment count must be positive")
		}
		nextTotal, err := checkedAddUint64(total, assignment.AssignmentCount, "task assignment count overflow")
		if err != nil {
			return TaskAssignmentDiversityReport{}, err
		}
		total = nextTotal
		nextValidatorTotal, err := checkedAddUint64(byValidator[assignment.ValidatorAddress], assignment.AssignmentCount, "validator task assignment count overflow")
		if err != nil {
			return TaskAssignmentDiversityReport{}, err
		}
		byValidator[assignment.ValidatorAddress] = nextValidatorTotal
	}
	report := TaskAssignmentDiversityReport{TotalAssignments: total, DiversityScoreBps: BasisPoints}
	for validator, count := range byValidator {
		if count > report.MaxValidatorAssignments || count == report.MaxValidatorAssignments && validator < report.MaxValidatorAddress {
			report.MaxValidatorAddress = validator
			report.MaxValidatorAssignments = count
		}
	}
	if total > 0 {
		report.MaxAssignmentShareBps = ratioBps(report.MaxValidatorAssignments, total)
		if report.MaxAssignmentShareBps <= BasisPoints {
			report.DiversityScoreBps = BasisPoints - report.MaxAssignmentShareBps
		}
	}
	if report.MaxAssignmentShareBps > maxShareBps {
		report.Warnings = append(report.Warnings, CentralizationWarningTaskAssignmentShare)
	}
	return report, nil
}

func ValidateConcentrationInvariants(metrics EconomicSecurityMetrics, params CentralizationControlParams) []ConcentrationInvariantAlert {
	alerts := make([]ConcentrationInvariantAlert, 0)
	if metrics.TopNVotingPowerConcentrationBps > params.MaxTopNConcentrationBps {
		alerts = append(alerts, concentrationAlert(CentralizationWarningTopNShare, metrics.TopNVotingPowerConcentrationBps, params.MaxTopNConcentrationBps))
	}
	if metrics.StakeSaturationRatioBps > params.MaxStakeSaturationRatioBps {
		alerts = append(alerts, concentrationAlert(CentralizationWarningStakeSaturation, metrics.StakeSaturationRatioBps, params.MaxStakeSaturationRatioBps))
	}
	for _, bucket := range metrics.DelegationRiskDistribution {
		share := intRatioBps(bucket.ExposureNaet, metrics.TotalStakeAtRiskNaet)
		if share > params.MaxDelegationRiskBucketBps {
			alerts = append(alerts, concentrationAlert(CentralizationWarningDelegationRisk, share, params.MaxDelegationRiskBucketBps))
			break
		}
	}
	return alerts
}

func RewardDampeningAboveSoftCapBps(votingPowerShareBps uint32, softCapBps uint32) uint32 {
	if softCapBps >= BasisPoints || votingPowerShareBps <= softCapBps {
		return 0
	}
	over := votingPowerShareBps - softCapBps
	denom := uint64(BasisPoints - softCapBps)
	return uint32((uint64(over) * uint64(BasisPoints)) / denom)
}

func IsBootstrapEligibleReliableValidator(validator ScoredValidator, votingPowerShareBps uint32, params CentralizationControlParams) bool {
	if votingPowerShareBps > params.BootstrapMaxVotingPowerShareBps {
		return false
	}
	if validator.ScoreComponents.PerformanceFactorBps < params.MinBootstrapPerformanceBps {
		return false
	}
	if validator.ScoreComponents.ReliabilityIndexBps < params.MinBootstrapReliabilityBps {
		return false
	}
	return validator.ScoreComponents.SaturatedStakeNaet.IsZero()
}

func SimulateStakeConcentration(input StakeConcentrationSimulationInput) (StakeConcentrationSimulationResult, error) {
	if err := input.Params.Validate(); err != nil {
		return StakeConcentrationSimulationResult{}, err
	}
	if input.AddedDelegatedStakeNaet.IsNil() || !input.AddedDelegatedStakeNaet.IsPositive() {
		return StakeConcentrationSimulationResult{}, errors.New("added delegated stake must be positive")
	}
	if err := validatePosToken("target validator id", input.TargetValidatorID); err != nil {
		return StakeConcentrationSimulationResult{}, err
	}
	beforeValidators, err := ScoreCandidatesForSecurity(input.Params, input.Candidates)
	if err != nil {
		return StakeConcentrationSimulationResult{}, err
	}
	afterCandidates := make([]Candidate, len(input.Candidates))
	for i, candidate := range input.Candidates {
		afterCandidates[i] = cloneCandidate(candidate)
		if afterCandidates[i].ValidatorID == input.TargetValidatorID {
			afterCandidates[i].DelegatedStakeNaet = afterCandidates[i].DelegatedStakeNaet.Add(input.AddedDelegatedStakeNaet)
			afterCandidates[i].Nominations = append(afterCandidates[i].Nominations, Nomination{
				NominatorID: "simulated-concentration-delegator",
				StakeNaet:   input.AddedDelegatedStakeNaet,
			})
		}
	}
	afterValidators, err := ScoreCandidatesForSecurity(input.Params, afterCandidates)
	if err != nil {
		return StakeConcentrationSimulationResult{}, err
	}
	before, err := securityMetricsFromValidators(beforeValidators, input.TopN)
	if err != nil {
		return StakeConcentrationSimulationResult{}, err
	}
	after, err := securityMetricsFromValidators(afterValidators, input.TopN)
	if err != nil {
		return StakeConcentrationSimulationResult{}, err
	}
	beforeTarget, found := scoredValidatorByID(beforeValidators, input.TargetValidatorID)
	if !found {
		return StakeConcentrationSimulationResult{}, errors.New("target validator not found")
	}
	afterTarget, found := scoredValidatorByID(afterValidators, input.TargetValidatorID)
	if !found {
		return StakeConcentrationSimulationResult{}, errors.New("target validator not found")
	}
	return StakeConcentrationSimulationResult{
		Before:                        before,
		After:                         after,
		TopNConcentrationDeltaBps:     int32(after.TopNVotingPowerConcentrationBps) - int32(before.TopNVotingPowerConcentrationBps),
		TargetEffectiveStakeDeltaNaet: afterTarget.EffectiveStakeNaet.Sub(beforeTarget.EffectiveStakeNaet),
		Alerts:                        ValidateConcentrationInvariants(after, DefaultCentralizationControlParams(input.Params)),
	}, nil
}

func SimulateStakeSplitting(input StakeSplittingSimulationInput) (StakeSplittingSimulationResult, error) {
	if err := input.Params.Validate(); err != nil {
		return StakeSplittingSimulationResult{}, err
	}
	if input.SplitCount < 2 {
		return StakeSplittingSimulationResult{}, errors.New("split count must be at least two")
	}
	single, err := ScoreCandidate(input.Params, input.Candidate)
	if err != nil {
		return StakeSplittingSimulationResult{}, err
	}
	totalStake := input.Candidate.SelfStakeNaet.Add(input.Candidate.DelegatedStakeNaet)
	baseStake := totalStake.QuoRaw(int64(input.SplitCount))
	remainder := totalStake.Sub(baseStake.MulRaw(int64(input.SplitCount)))
	splitCandidates := make([]Candidate, input.SplitCount)
	for i := range splitCandidates {
		stake := baseStake
		if i == 0 {
			stake = stake.Add(remainder)
		}
		split := cloneCandidate(input.Candidate)
		split.ValidatorID = fmt.Sprintf("%s-split-%03d", input.Candidate.ValidatorID, i)
		split.SelfStakeNaet = stake
		split.DelegatedStakeNaet = sdkmath.ZeroInt()
		split.Nominations = nil
		splitCandidates[i] = split
	}
	splitValidators, err := ScoreCandidatesForSecurity(input.Params, splitCandidates)
	if err != nil {
		return StakeSplittingSimulationResult{}, err
	}
	singleMetrics, err := securityMetricsFromValidators([]ScoredValidator{single}, input.TopN)
	if err != nil {
		return StakeSplittingSimulationResult{}, err
	}
	splitMetrics, err := securityMetricsFromValidators(splitValidators, input.TopN)
	if err != nil {
		return StakeSplittingSimulationResult{}, err
	}
	splitEffective := sdkmath.ZeroInt()
	for _, validator := range splitValidators {
		splitEffective = splitEffective.Add(validator.EffectiveStakeNaet)
	}
	return StakeSplittingSimulationResult{
		SingleEffectiveStakeNaet: single.EffectiveStakeNaet,
		SplitEffectiveStakeNaet:  splitEffective,
		EffectiveStakeGainNaet:   splitEffective.Sub(single.EffectiveStakeNaet),
		SingleConcentrationBps:   singleMetrics.TopNVotingPowerConcentrationBps,
		SplitConcentrationBps:    splitMetrics.TopNVotingPowerConcentrationBps,
	}, nil
}

func ScoreCandidatesForSecurity(params Params, candidates []Candidate) ([]ScoredValidator, error) {
	if len(candidates) == 0 {
		return nil, errors.New("security scoring candidates are required")
	}
	validators := make([]ScoredValidator, len(candidates))
	for i, candidate := range candidates {
		scored, err := ScoreCandidate(params, candidate)
		if err != nil {
			return nil, err
		}
		validators[i] = scored
	}
	return validators, nil
}

func securityMetricsFromValidators(validators []ScoredValidator, topN uint32) (EconomicSecurityMetrics, error) {
	return ComputeEconomicSecurityMetrics(EconomicSecurityInput{
		Validators:              validators,
		TopN:                    topN,
		ParticipatingValidators: uint64(len(validators)),
		EligibleValidators:      uint64(len(validators)),
		AcceptedSlashEvents:     1,
		DetectedFaultEvents:     1,
		AcceptedEvidence:        1,
		SubmittedEvidence:       1,
		CompletedTasks:          1,
		ExpectedTasks:           1,
	})
}

func scoredValidatorByID(validators []ScoredValidator, validatorID string) (ScoredValidator, bool) {
	for _, validator := range validators {
		if validator.ValidatorID == validatorID {
			return validator, true
		}
	}
	return ScoredValidator{}, false
}

func concentrationAlert(alertType string, observedBps uint32, thresholdBps uint32) ConcentrationInvariantAlert {
	severity := ConcentrationAlertSeverityWarning
	if thresholdBps > 0 && observedBps >= minUint32(BasisPoints, thresholdBps*2) {
		severity = ConcentrationAlertSeverityCritical
	}
	return ConcentrationInvariantAlert{
		AlertType:    alertType,
		Severity:     severity,
		ObservedBps:  observedBps,
		ThresholdBps: thresholdBps,
	}
}

func BuildDelegationRiskDistribution(windows []RiskWindowRecord) ([]DelegationRiskBucket, sdkmath.Int, error) {
	byValidator := make(map[string]DelegationRiskBucket)
	totalExposure := sdkmath.ZeroInt()
	for _, window := range windows {
		if err := window.Validate(); err != nil {
			return nil, sdkmath.Int{}, err
		}
		if window.Status == RiskWindowStatusExpired {
			continue
		}
		bucket := byValidator[window.ValidatorAddress]
		bucket.ValidatorAddress = window.ValidatorAddress
		if bucket.ExposureNaet.IsNil() {
			bucket.ExposureNaet = sdkmath.ZeroInt()
		}
		bucket.ExposureNaet = bucket.ExposureNaet.Add(window.AmountNaet)
		bucket.RiskWindowCount++
		byValidator[window.ValidatorAddress] = bucket
		totalExposure = totalExposure.Add(window.AmountNaet)
	}
	distribution := make([]DelegationRiskBucket, 0, len(byValidator))
	for _, bucket := range byValidator {
		distribution = append(distribution, bucket)
	}
	sort.SliceStable(distribution, func(i, j int) bool {
		if !distribution[i].ExposureNaet.Equal(distribution[j].ExposureNaet) {
			return distribution[i].ExposureNaet.GT(distribution[j].ExposureNaet)
		}
		return distribution[i].ValidatorAddress < distribution[j].ValidatorAddress
	})
	return distribution, totalExposure, nil
}

func TopNVotingPowerConcentrationBps(validators []ScoredValidator, topN uint32) uint32 {
	if len(validators) == 0 || topN == 0 {
		return 0
	}
	ordered := make([]ScoredValidator, len(validators))
	copy(ordered, validators)
	sort.SliceStable(ordered, func(i, j int) bool {
		if !ordered[i].VotingPowerNaet.Equal(ordered[j].VotingPowerNaet) {
			return ordered[i].VotingPowerNaet.GT(ordered[j].VotingPowerNaet)
		}
		return ordered[i].ValidatorID < ordered[j].ValidatorID
	})
	total := sdkmath.ZeroInt()
	for _, validator := range ordered {
		if validator.VotingPowerNaet.IsNil() || !validator.VotingPowerNaet.IsPositive() {
			continue
		}
		total = total.Add(validator.VotingPowerNaet)
	}
	if !total.IsPositive() {
		return 0
	}
	limit := int(topN)
	if limit > len(ordered) {
		limit = len(ordered)
	}
	top := sdkmath.ZeroInt()
	for i := 0; i < limit; i++ {
		if ordered[i].VotingPowerNaet.IsNil() || !ordered[i].VotingPowerNaet.IsPositive() {
			continue
		}
		top = top.Add(ordered[i].VotingPowerNaet)
	}
	return intRatioBps(top, total)
}

func validateScoredValidatorForSecurity(validator ScoredValidator) error {
	if err := validatePosToken("economic security validator id", validator.ValidatorID); err != nil {
		return err
	}
	if validator.TotalStakeNaet.IsNil() || validator.TotalStakeNaet.IsNegative() {
		return errors.New("validator total stake cannot be nil or negative")
	}
	if validator.EffectiveStakeNaet.IsNil() || validator.EffectiveStakeNaet.IsNegative() {
		return errors.New("validator effective stake cannot be nil or negative")
	}
	if validator.VotingPowerNaet.IsNil() || validator.VotingPowerNaet.IsNegative() {
		return errors.New("validator voting power cannot be nil or negative")
	}
	if validator.Score.IsNil() || validator.Score.IsNegative() {
		return errors.New("validator score cannot be nil or negative")
	}
	if validator.ScoreComponents.SaturatedStakeNaet.IsNil() || validator.ScoreComponents.SaturatedStakeNaet.IsNegative() {
		return errors.New("validator saturated stake cannot be nil or negative")
	}
	return nil
}
