package types

import (
	"errors"
	"fmt"
	"strings"

	sdkmath "cosmossdk.io/math"
)

type AssignmentOutcome struct {
	TaskID      string
	Role        ValidatorRole
	ValidatorID string
	Completed   bool
	Faulted     bool
	WorkUnits   uint64
}

type ValidatorWorkloadReward struct {
	ValidatorID string
	RewardNaet  sdkmath.Int
	WorkUnits   uint64
}

type WorkloadRewardInput struct {
	EpochID          uint64
	TotalRewardsNaet sdkmath.Int
	RoleWeights      []RoleRewardWeight
	Outcomes         []AssignmentOutcome
}

type WorkloadRewardSettlement struct {
	EpochID        uint64
	Rewards        []ValidatorWorkloadReward
	RemainderNaet  sdkmath.Int
	RewardRoot     string
	CompletedUnits uint64
}

type PerformanceFactorInput struct {
	CompletedTasks         uint64
	MissedTasks            uint64
	CorrectVerifications   uint64
	IncorrectVerifications uint64
	AvailableWindows       uint64
	CommittedWindows       uint64
}

type UptimeFactorInput struct {
	SignedBlocks             uint64
	TotalBlocks              uint64
	TaskParticipations       uint64
	MissedTaskParticipations uint64
}

type LatencyFactorInput struct {
	CommittedWindow bool
	AdvisoryOnly    bool
	TargetMillis    uint64
	P95Millis       uint64
}

type ReliabilityIndexInput struct {
	PriorIndexBps    uint32
	SlashEvents      uint64
	DowntimeEpochs   uint64
	MissedTasks      uint64
	RejectedEvidence uint64
	RecoveryEpochs   uint64
}

type CorrectnessScoreInput struct {
	ValidSignatures       uint64
	InvalidSignatures     uint64
	ValidTaskOutputs      uint64
	InvalidTaskOutputs    uint64
	AcceptedEvidence      uint64
	EvidencePenaltyWeight uint64
}

type TaskCompletionRateInput struct {
	CompletedAssignedTasks uint64
	ExpectedAssignedTasks  uint64
}

type PerformanceRewardInput struct {
	EpochID               uint64
	ValidatorID           string
	BaseEmissionNaet      sdkmath.Int
	UptimeScoreBps        uint32
	LatencyScoreBps       uint32
	CorrectnessScoreBps   uint32
	TaskCompletionRateBps uint32
}

type PerformanceRewardRecord struct {
	EpochID               uint64
	ValidatorID           string
	BaseEmissionNaet      sdkmath.Int
	UptimeScoreBps        uint32
	LatencyScoreBps       uint32
	CorrectnessScoreBps   uint32
	TaskCompletionRateBps uint32
	RewardNaet            sdkmath.Int
	RewardHash            string
}

type PerformanceRecord struct {
	EpochID               uint64
	OperatorAddress       string
	Role                  ValidatorRole
	AssignedTasks         uint64
	CompletedTasks        uint64
	MissedTasks           uint64
	InvalidTasks          uint64
	UptimeScoreBps        uint32
	LatencyScoreBps       uint32
	CorrectnessScoreBps   uint32
	TaskCompletionRateBps uint32
	RewardMultiplierBps   uint32
}

type PerformanceRecordInput struct {
	EpochID             uint64
	OperatorAddress     string
	Role                ValidatorRole
	AssignedTasks       uint64
	CompletedTasks      uint64
	MissedTasks         uint64
	InvalidTasks        uint64
	UptimeScoreBps      uint32
	LatencyScoreBps     uint32
	CorrectnessScoreBps uint32
}

type PerformanceDampeningInput struct {
	Record                           PerformanceRecord
	CurrentRewardNaet                sdkmath.Int
	FutureElectionScoreBps           uint32
	DelegationAttractivenessBps      uint32
	RoleEligibilityBps               uint32
	CollatorAssignmentProbabilityBps uint32
}

type PerformanceDampeningResult struct {
	EpochID                          uint64
	OperatorAddress                  string
	Role                             ValidatorRole
	RewardMultiplierBps              uint32
	CurrentRewardNaet                sdkmath.Int
	FutureElectionScoreBps           uint32
	DelegationAttractivenessBps      uint32
	RoleEligibilityBps               uint32
	CollatorAssignmentProbabilityBps uint32
}

func ComputePerformanceFactor(input PerformanceFactorInput) (uint32, error) {
	completionDenom := input.CompletedTasks + input.MissedTasks
	if completionDenom < input.CompletedTasks {
		return 0, errors.New("performance task count overflow")
	}
	correctnessDenom := input.CorrectVerifications + input.IncorrectVerifications
	if correctnessDenom < input.CorrectVerifications {
		return 0, errors.New("performance verification count overflow")
	}
	if input.CommittedWindows < input.AvailableWindows {
		return 0, errors.New("available windows cannot exceed committed windows")
	}
	completion := ratioBps(input.CompletedTasks, completionDenom)
	correctness := ratioBps(input.CorrectVerifications, correctnessDenom)
	availability := ratioBps(input.AvailableWindows, input.CommittedWindows)
	score := uint64(4_000)*uint64(completion) +
		uint64(4_000)*uint64(correctness) +
		uint64(2_000)*uint64(availability)
	return uint32(score / uint64(BasisPoints)), nil
}

func ComputeUptimeFactor(input UptimeFactorInput) (uint32, error) {
	if input.TotalBlocks < input.SignedBlocks {
		return 0, errors.New("signed blocks cannot exceed total blocks")
	}
	totalTaskParticipations := input.TaskParticipations + input.MissedTaskParticipations
	if totalTaskParticipations < input.TaskParticipations {
		return 0, errors.New("task participation count overflow")
	}
	blocks := ratioBps(input.SignedBlocks, input.TotalBlocks)
	tasks := ratioBps(input.TaskParticipations, totalTaskParticipations)
	score := uint64(7_000)*uint64(blocks) + uint64(3_000)*uint64(tasks)
	return uint32(score / uint64(BasisPoints)), nil
}

func ComputeLatencyFactor(input LatencyFactorInput) (uint32, error) {
	if !input.CommittedWindow {
		return 0, errors.New("latency factor requires committed measurement window")
	}
	if input.AdvisoryOnly {
		return BasisPoints, nil
	}
	if input.TargetMillis == 0 {
		return 0, errors.New("latency target must be positive")
	}
	if input.P95Millis == 0 || input.P95Millis <= input.TargetMillis {
		return BasisPoints, nil
	}
	return uint32(sdkmath.NewIntFromUint64(input.TargetMillis).MulRaw(int64(BasisPoints)).Quo(sdkmath.NewIntFromUint64(input.P95Millis)).Uint64()), nil
}

func ComputeReliabilityIndex(input ReliabilityIndexInput) (uint32, error) {
	if input.PriorIndexBps > BasisPoints {
		return 0, fmt.Errorf("prior reliability index must be <= %d bps", BasisPoints)
	}
	index := input.PriorIndexBps
	if index == 0 {
		index = BasisPoints
	}
	penalty, err := reliabilityPenalty(input)
	if err != nil {
		return 0, err
	}
	if penalty >= uint64(index) {
		index = 0
	} else {
		index -= uint32(penalty)
	}
	recovery := input.RecoveryEpochs * 100
	if recovery > uint64(BasisPoints-index) {
		return BasisPoints, nil
	}
	return index + uint32(recovery), nil
}

func ComputeCorrectnessScore(input CorrectnessScoreInput) (uint32, error) {
	penaltyWeight := input.EvidencePenaltyWeight
	if penaltyWeight == 0 {
		penaltyWeight = 2
	}
	validUnits, err := checkedAddUint64(input.ValidSignatures, input.ValidTaskOutputs, "correctness valid unit overflow")
	if err != nil {
		return 0, err
	}
	invalidUnits, err := checkedAddUint64(input.InvalidSignatures, input.InvalidTaskOutputs, "correctness invalid unit overflow")
	if err != nil {
		return 0, err
	}
	evidenceFaults, overflow := mulUint64Overflow(input.AcceptedEvidence, penaltyWeight)
	if overflow {
		return 0, errors.New("correctness evidence penalty overflow")
	}
	faultUnits, err := checkedAddUint64(invalidUnits, evidenceFaults, "correctness fault unit overflow")
	if err != nil {
		return 0, err
	}
	totalUnits, err := checkedAddUint64(validUnits, faultUnits, "correctness total unit overflow")
	if err != nil {
		return 0, err
	}
	return ratioBps(validUnits, totalUnits), nil
}

func ComputeTaskCompletionRate(input TaskCompletionRateInput) (uint32, error) {
	if input.CompletedAssignedTasks > input.ExpectedAssignedTasks {
		return 0, errors.New("completed assigned tasks cannot exceed expected tasks")
	}
	return ratioBps(input.CompletedAssignedTasks, input.ExpectedAssignedTasks), nil
}

func ComputePerformanceBasedReward(input PerformanceRewardInput) (PerformanceRewardRecord, error) {
	input.ValidatorID = strings.TrimSpace(input.ValidatorID)
	if input.EpochID == 0 {
		return PerformanceRewardRecord{}, errors.New("performance reward epoch id is required")
	}
	if err := validatePosToken("performance reward validator id", input.ValidatorID); err != nil {
		return PerformanceRewardRecord{}, err
	}
	if input.BaseEmissionNaet.IsNil() {
		input.BaseEmissionNaet = sdkmath.ZeroInt()
	}
	if input.BaseEmissionNaet.IsNegative() {
		return PerformanceRewardRecord{}, errors.New("performance reward base emission cannot be negative")
	}
	if err := validatePerformanceRewardBps(input.UptimeScoreBps, input.LatencyScoreBps, input.CorrectnessScoreBps, input.TaskCompletionRateBps); err != nil {
		return PerformanceRewardRecord{}, err
	}
	reward := input.BaseEmissionNaet
	reward = mulIntBps(reward, input.UptimeScoreBps)
	reward = mulIntBps(reward, input.LatencyScoreBps)
	reward = mulIntBps(reward, input.CorrectnessScoreBps)
	reward = mulIntBps(reward, input.TaskCompletionRateBps)
	record := PerformanceRewardRecord{
		EpochID:               input.EpochID,
		ValidatorID:           input.ValidatorID,
		BaseEmissionNaet:      input.BaseEmissionNaet,
		UptimeScoreBps:        input.UptimeScoreBps,
		LatencyScoreBps:       input.LatencyScoreBps,
		CorrectnessScoreBps:   input.CorrectnessScoreBps,
		TaskCompletionRateBps: input.TaskCompletionRateBps,
		RewardNaet:            reward,
	}
	record.RewardHash = ComputePerformanceRewardHash(record)
	return record, record.Validate()
}

func (r PerformanceRewardRecord) Validate() error {
	if r.EpochID == 0 {
		return errors.New("performance reward epoch id is required")
	}
	if err := validatePosToken("performance reward validator id", r.ValidatorID); err != nil {
		return err
	}
	if r.BaseEmissionNaet.IsNil() || r.BaseEmissionNaet.IsNegative() {
		return errors.New("performance reward base emission cannot be nil or negative")
	}
	if r.RewardNaet.IsNil() || r.RewardNaet.IsNegative() {
		return errors.New("performance reward amount cannot be nil or negative")
	}
	if r.RewardNaet.GT(r.BaseEmissionNaet) {
		return errors.New("performance reward cannot exceed base emission")
	}
	if err := validatePerformanceRewardBps(r.UptimeScoreBps, r.LatencyScoreBps, r.CorrectnessScoreBps, r.TaskCompletionRateBps); err != nil {
		return err
	}
	if err := validatePosHash("performance reward hash", r.RewardHash); err != nil {
		return err
	}
	if expected := ComputePerformanceRewardHash(r); expected != r.RewardHash {
		return errors.New("performance reward hash mismatch")
	}
	return nil
}

func ComputePerformanceRewardHash(record PerformanceRewardRecord) string {
	return posHashRoot("aetheris-pos-performance-reward-v1", func(w posByteWriter) {
		posWriteUint64(w, record.EpochID)
		posWritePart(w, record.ValidatorID)
		posWritePart(w, record.BaseEmissionNaet.String())
		posWriteUint64(w, uint64(record.UptimeScoreBps))
		posWriteUint64(w, uint64(record.LatencyScoreBps))
		posWriteUint64(w, uint64(record.CorrectnessScoreBps))
		posWriteUint64(w, uint64(record.TaskCompletionRateBps))
		posWritePart(w, record.RewardNaet.String())
	})
}

func PerformanceRecordFieldNames() []string {
	return []string{
		"epoch_id",
		"operator_address",
		"role",
		"assigned_tasks",
		"completed_tasks",
		"missed_tasks",
		"invalid_tasks",
		"uptime_score",
		"latency_score",
		"correctness_score",
		"task_completion_rate",
		"reward_multiplier",
	}
}

func BuildPerformanceRecord(input PerformanceRecordInput) (PerformanceRecord, error) {
	input.OperatorAddress = strings.TrimSpace(input.OperatorAddress)
	if input.AssignedTasks < input.CompletedTasks {
		return PerformanceRecord{}, errors.New("completed tasks cannot exceed assigned tasks")
	}
	if input.AssignedTasks < input.MissedTasks {
		return PerformanceRecord{}, errors.New("missed tasks cannot exceed assigned tasks")
	}
	if input.AssignedTasks < input.InvalidTasks {
		return PerformanceRecord{}, errors.New("invalid tasks cannot exceed assigned tasks")
	}
	observed, err := checkedAddUint64(input.CompletedTasks, input.MissedTasks, "performance observed task overflow")
	if err != nil {
		return PerformanceRecord{}, err
	}
	observed, err = checkedAddUint64(observed, input.InvalidTasks, "performance observed task overflow")
	if err != nil {
		return PerformanceRecord{}, err
	}
	if observed > input.AssignedTasks {
		return PerformanceRecord{}, errors.New("performance task counts exceed assigned tasks")
	}
	taskCompletion, err := ComputeTaskCompletionRate(TaskCompletionRateInput{
		CompletedAssignedTasks: input.CompletedTasks,
		ExpectedAssignedTasks:  input.AssignedTasks,
	})
	if err != nil {
		return PerformanceRecord{}, err
	}
	multiplier, err := computeRewardMultiplierBps(input.UptimeScoreBps, input.LatencyScoreBps, input.CorrectnessScoreBps, taskCompletion)
	if err != nil {
		return PerformanceRecord{}, err
	}
	record := PerformanceRecord{
		EpochID:               input.EpochID,
		OperatorAddress:       input.OperatorAddress,
		Role:                  input.Role,
		AssignedTasks:         input.AssignedTasks,
		CompletedTasks:        input.CompletedTasks,
		MissedTasks:           input.MissedTasks,
		InvalidTasks:          input.InvalidTasks,
		UptimeScoreBps:        input.UptimeScoreBps,
		LatencyScoreBps:       input.LatencyScoreBps,
		CorrectnessScoreBps:   input.CorrectnessScoreBps,
		TaskCompletionRateBps: taskCompletion,
		RewardMultiplierBps:   multiplier,
	}
	return record, record.Validate()
}

func (r PerformanceRecord) Validate() error {
	if r.EpochID == 0 {
		return errors.New("performance record epoch id is required")
	}
	if err := validatePosToken("performance record operator address", r.OperatorAddress); err != nil {
		return err
	}
	if err := validateValidatorRole(r.Role); err != nil {
		return err
	}
	if r.AssignedTasks < r.CompletedTasks {
		return errors.New("completed tasks cannot exceed assigned tasks")
	}
	if r.AssignedTasks < r.MissedTasks {
		return errors.New("missed tasks cannot exceed assigned tasks")
	}
	if r.AssignedTasks < r.InvalidTasks {
		return errors.New("invalid tasks cannot exceed assigned tasks")
	}
	observed, err := checkedAddUint64(r.CompletedTasks, r.MissedTasks, "performance observed task overflow")
	if err != nil {
		return err
	}
	observed, err = checkedAddUint64(observed, r.InvalidTasks, "performance observed task overflow")
	if err != nil {
		return err
	}
	if observed > r.AssignedTasks {
		return errors.New("performance task counts exceed assigned tasks")
	}
	if err := validatePerformanceRewardBps(r.UptimeScoreBps, r.LatencyScoreBps, r.CorrectnessScoreBps, r.TaskCompletionRateBps); err != nil {
		return err
	}
	if r.RewardMultiplierBps > BasisPoints {
		return fmt.Errorf("reward multiplier must be <= %d bps", BasisPoints)
	}
	expectedMultiplier, err := computeRewardMultiplierBps(r.UptimeScoreBps, r.LatencyScoreBps, r.CorrectnessScoreBps, r.TaskCompletionRateBps)
	if err != nil {
		return err
	}
	if r.RewardMultiplierBps != expectedMultiplier {
		return errors.New("performance record reward multiplier mismatch")
	}
	return nil
}

func ApplyPerformanceDampening(input PerformanceDampeningInput) (PerformanceDampeningResult, error) {
	if err := input.Record.Validate(); err != nil {
		return PerformanceDampeningResult{}, err
	}
	if input.CurrentRewardNaet.IsNil() {
		input.CurrentRewardNaet = sdkmath.ZeroInt()
	}
	if input.CurrentRewardNaet.IsNegative() {
		return PerformanceDampeningResult{}, errors.New("performance dampening current reward cannot be negative")
	}
	if err := validatePerformanceRewardBps(
		input.FutureElectionScoreBps,
		input.DelegationAttractivenessBps,
		input.RoleEligibilityBps,
		input.CollatorAssignmentProbabilityBps,
	); err != nil {
		return PerformanceDampeningResult{}, err
	}
	multiplier := input.Record.RewardMultiplierBps
	result := PerformanceDampeningResult{
		EpochID:                          input.Record.EpochID,
		OperatorAddress:                  input.Record.OperatorAddress,
		Role:                             input.Record.Role,
		RewardMultiplierBps:              multiplier,
		CurrentRewardNaet:                mulIntBps(input.CurrentRewardNaet, multiplier),
		FutureElectionScoreBps:           mulBps(input.FutureElectionScoreBps, multiplier),
		DelegationAttractivenessBps:      mulBps(input.DelegationAttractivenessBps, multiplier),
		RoleEligibilityBps:               mulBps(input.RoleEligibilityBps, multiplier),
		CollatorAssignmentProbabilityBps: mulBps(input.CollatorAssignmentProbabilityBps, multiplier),
	}
	if input.Record.Role != ValidatorRoleCollator {
		result.CollatorAssignmentProbabilityBps = input.CollatorAssignmentProbabilityBps
	}
	return result, nil
}

func reliabilityPenalty(input ReliabilityIndexInput) (uint64, error) {
	penalty := sdkmath.NewIntFromUint64(input.SlashEvents).MulRaw(2_000)
	penalty = penalty.Add(sdkmath.NewIntFromUint64(input.DowntimeEpochs).MulRaw(500))
	penalty = penalty.Add(sdkmath.NewIntFromUint64(input.MissedTasks).MulRaw(100))
	penalty = penalty.Add(sdkmath.NewIntFromUint64(input.RejectedEvidence).MulRaw(250))
	if !penalty.LTE(sdkmath.NewIntFromUint64(uint64(BasisPoints))) {
		return uint64(BasisPoints), nil
	}
	return penalty.Uint64(), nil
}

func SettleWorkloadRewards(input WorkloadRewardInput) (WorkloadRewardSettlement, error) {
	if input.TotalRewardsNaet.IsNegative() {
		return WorkloadRewardSettlement{}, errors.New("workload rewards cannot be negative")
	}
	weights := input.RoleWeights
	if len(weights) == 0 {
		weights = DefaultRoleRewardWeights()
	}
	if err := validateRoleRewardWeights(weights); err != nil {
		return WorkloadRewardSettlement{}, err
	}

	outcomesByRole := make(map[ValidatorRole][]AssignmentOutcome)
	for _, outcome := range input.Outcomes {
		if err := outcome.Validate(); err != nil {
			return WorkloadRewardSettlement{}, err
		}
		outcomesByRole[outcome.Role] = append(outcomesByRole[outcome.Role], outcome)
	}

	rewardByValidator := make(map[string]sdkmath.Int)
	workUnitsByValidator := make(map[string]uint64)
	remainder := sdkmath.ZeroInt()
	completedUnits := uint64(0)

	for _, weight := range weights {
		roleBudget := mulIntBps(input.TotalRewardsNaet, weight.WeightBps)
		roleUnitsByValidator := make(map[string]uint64)
		totalRoleUnits := uint64(0)
		for _, outcome := range outcomesByRole[weight.Role] {
			if !outcome.Completed || outcome.Faulted || outcome.WorkUnits == 0 {
				continue
			}
			roleUnitsByValidator[outcome.ValidatorID] += outcome.WorkUnits
			workUnitsByValidator[outcome.ValidatorID] += outcome.WorkUnits
			totalRoleUnits += outcome.WorkUnits
			completedUnits += outcome.WorkUnits
		}
		if totalRoleUnits == 0 {
			remainder = remainder.Add(roleBudget)
			continue
		}
		validatorIDs := sortedStringKeys(roleUnitsByValidator)
		distributed := sdkmath.ZeroInt()
		for _, validatorID := range validatorIDs {
			reward := roleBudget.MulRaw(int64(roleUnitsByValidator[validatorID])).QuoRaw(int64(totalRoleUnits))
			currentReward, found := rewardByValidator[validatorID]
			if !found {
				currentReward = sdkmath.ZeroInt()
			}
			rewardByValidator[validatorID] = currentReward.Add(reward)
			distributed = distributed.Add(reward)
		}
		remainder = remainder.Add(roleBudget.Sub(distributed))
	}

	validatorIDs := sortedStringKeys(rewardByValidator)
	rewards := make([]ValidatorWorkloadReward, 0, len(validatorIDs))
	for _, validatorID := range validatorIDs {
		rewards = append(rewards, ValidatorWorkloadReward{
			ValidatorID: validatorID,
			RewardNaet:  rewardByValidator[validatorID],
			WorkUnits:   workUnitsByValidator[validatorID],
		})
	}
	settlement := WorkloadRewardSettlement{
		EpochID:        input.EpochID,
		Rewards:        rewards,
		RemainderNaet:  remainder,
		CompletedUnits: completedUnits,
	}
	settlement.RewardRoot = computeWorkloadRewardRoot(settlement)
	return settlement, nil
}

func (o AssignmentOutcome) Validate() error {
	if err := validatePosToken("assignment outcome task id", o.TaskID); err != nil {
		return err
	}
	if err := validateValidatorRole(o.Role); err != nil {
		return err
	}
	if err := validatePosToken("assignment outcome validator id", o.ValidatorID); err != nil {
		return err
	}
	if o.Completed && o.Faulted {
		return errors.New("assignment outcome cannot be both completed and faulted")
	}
	return nil
}

func computeWorkloadRewardRoot(settlement WorkloadRewardSettlement) string {
	return posHashRoot("aetheris-pos-workload-reward-root-v1", func(w posByteWriter) {
		posWriteUint64(w, settlement.EpochID)
		posWriteUint64(w, uint64(len(settlement.Rewards)))
		for _, reward := range settlement.Rewards {
			posWritePart(w, reward.ValidatorID)
			posWritePart(w, reward.RewardNaet.String())
			posWriteUint64(w, reward.WorkUnits)
		}
		posWritePart(w, settlement.RemainderNaet.String())
		posWriteUint64(w, settlement.CompletedUnits)
	})
}

func validatePerformanceRewardBps(values ...uint32) error {
	names := []string{"uptime score", "latency score", "correctness score", "task completion rate"}
	for i, value := range values {
		name := "performance reward component"
		if i < len(names) {
			name = names[i]
		}
		if value > BasisPoints {
			return fmt.Errorf("%s must be <= %d bps", name, BasisPoints)
		}
	}
	return nil
}

func computeRewardMultiplierBps(uptimeScoreBps uint32, latencyScoreBps uint32, correctnessScoreBps uint32, taskCompletionRateBps uint32) (uint32, error) {
	if err := validatePerformanceRewardBps(uptimeScoreBps, latencyScoreBps, correctnessScoreBps, taskCompletionRateBps); err != nil {
		return 0, err
	}
	multiplier := mulBps(uptimeScoreBps, latencyScoreBps)
	multiplier = mulBps(multiplier, correctnessScoreBps)
	multiplier = mulBps(multiplier, taskCompletionRateBps)
	return multiplier, nil
}

func mulBps(value uint32, multiplier uint32) uint32 {
	return uint32((uint64(value) * uint64(multiplier)) / uint64(BasisPoints))
}
