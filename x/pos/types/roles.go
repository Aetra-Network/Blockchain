package types

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	sdkmath "cosmossdk.io/math"
)

type ValidatorRole string

const (
	RoleStatusEligible  = "eligible"
	RoleStatusAssigned  = "assigned"
	RoleStatusSuspended = "suspended"
	RoleStatusInactive  = "inactive"
)

type RoleRecord struct {
	ValidatorAddress  string
	Role              ValidatorRole
	EpochID           uint64
	Status            string
	EligibilityScore  uint32
	Capacity          ValidatorCapacity
	AssignedTaskCount uint32
	PerformanceScore  uint32
}

type RoleRule struct {
	Role                  ValidatorRole
	Description           string
	RequiresValidator     bool
	RequiresMinimumStake  bool
	RequiresDeposit       bool
	RequiresAuthorization bool
	RequiresFeeDisclosure bool
	RequiresRiskPolicy    bool
	CanFinalize           bool
	RewardWeightBps       uint32
	MinimumPerformanceBps uint32
	MinimumEligibilityBps uint32
}

type RoleRegistry struct {
	Rules []RoleRule
}

type RoleEligibilityInput struct {
	Params                       Params
	Role                         ValidatorRole
	ActorAddress                 string
	Candidate                    Candidate
	DepositNaet                  sdkmath.Int
	DelegationOperatorAuthorized bool
	FeesDisclosed                bool
	RiskPolicyDisclosed          bool
}

type RolePerformanceMetrics struct {
	ValidatorAddress string
	Role             ValidatorRole
	EpochID          uint64
	AssignedTasks    uint32
	CompletedTasks   uint32
	FaultedTasks     uint32
	MissedTasks      uint32
	PerformanceScore uint32
}

type RoleRewardInput struct {
	EpochID          uint64
	TotalRewardsNaet sdkmath.Int
	Records          []RoleRecord
	Weights          []RoleRewardWeight
}

type RoleRewardWeight struct {
	Role      ValidatorRole
	WeightBps uint32
}

func DefaultRoleRewardWeights() []RoleRewardWeight {
	return []RoleRewardWeight{
		{Role: ValidatorRoleBlockProducer, WeightBps: 3_500},
		{Role: ValidatorRoleVerifier, WeightBps: 3_500},
		{Role: ValidatorRoleCollator, WeightBps: 1_500},
		{Role: ValidatorRoleEvidenceReviewer, WeightBps: 1_500},
	}
}

func ValidatorSupportsRole(candidate Candidate, role ValidatorRole) bool {
	if err := validateValidatorRole(role); err != nil {
		return false
	}
	if len(candidate.Roles) == 0 {
		return true
	}
	for _, candidateRole := range candidate.Roles {
		if candidateRole == role {
			return true
		}
	}
	return false
}

func ValidatorRoleValues() []ValidatorRole {
	return []ValidatorRole{
		ValidatorRoleValidator,
		ValidatorRoleProposer,
		ValidatorRoleVerifier,
		ValidatorRoleEvidenceReporter,
		ValidatorRoleDelegationOperator,
		ValidatorRoleCollator,
		ValidatorRoleFisherman,
	}
}

func RoleRecordFieldNames() []string {
	return []string{
		"validator_address",
		"role",
		"epoch_id",
		"status",
		"eligibility_score",
		"capacity",
		"assigned_task_count",
		"performance_score",
	}
}

func RoleStatusValues() []string {
	return []string{
		RoleStatusEligible,
		RoleStatusAssigned,
		RoleStatusSuspended,
		RoleStatusInactive,
	}
}

func NewRoleRecord(record RoleRecord) (RoleRecord, error) {
	record.ValidatorAddress = strings.TrimSpace(record.ValidatorAddress)
	record.Status = strings.TrimSpace(record.Status)
	if record.Status == "" {
		record.Status = RoleStatusEligible
	}
	return record, record.Validate()
}

func (r RoleRecord) Validate() error {
	if err := validatePosToken("role record validator address", r.ValidatorAddress); err != nil {
		return err
	}
	if err := validateValidatorRole(r.Role); err != nil {
		return err
	}
	if r.EpochID == 0 {
		return errors.New("role record epoch id is required")
	}
	if err := validateRoleStatus(r.Status); err != nil {
		return err
	}
	if r.EligibilityScore > BasisPoints {
		return fmt.Errorf("role record eligibility score must be <= %d bps", BasisPoints)
	}
	if r.PerformanceScore > BasisPoints {
		return fmt.Errorf("role record performance score must be <= %d bps", BasisPoints)
	}
	if err := r.Capacity.Validate(); err != nil {
		return err
	}
	if r.Capacity.MaxTaskGroups > 0 && r.AssignedTaskCount > r.Capacity.MaxTaskGroups {
		return errors.New("role record assigned task count exceeds capacity")
	}
	if r.Status == RoleStatusAssigned && r.AssignedTaskCount == 0 {
		return errors.New("assigned role record requires assigned task count")
	}
	return nil
}

func ValidateRoleRecords(records []RoleRecord) error {
	seen := make(map[string]struct{}, len(records))
	for _, record := range records {
		if err := record.Validate(); err != nil {
			return err
		}
		key := fmt.Sprintf("%s|%s|%d", record.ValidatorAddress, record.Role, record.EpochID)
		if _, found := seen[key]; found {
			return fmt.Errorf("duplicate role record %s", key)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func DefaultRoleRegistry() RoleRegistry {
	return RoleRegistry{Rules: []RoleRule{
		{Role: ValidatorRoleValidator, Description: "participates in consensus security", RequiresValidator: true, RequiresMinimumStake: true, RewardWeightBps: 2_000, MinimumPerformanceBps: 8_000, MinimumEligibilityBps: 8_000, CanFinalize: true},
		{Role: ValidatorRoleProposer, Description: "produces canonical block or task output for slot", RequiresValidator: true, RequiresMinimumStake: true, RewardWeightBps: 1_500, MinimumPerformanceBps: 8_500, MinimumEligibilityBps: 8_500},
		{Role: ValidatorRoleVerifier, Description: "re-executes and signs verification receipts", RequiresValidator: true, RequiresMinimumStake: true, RewardWeightBps: 2_000, MinimumPerformanceBps: 8_500, MinimumEligibilityBps: 8_500},
		{Role: ValidatorRoleEvidenceReporter, Description: "detects and submits faults", RequiresDeposit: true, RewardWeightBps: 1_000, MinimumPerformanceBps: 7_000, MinimumEligibilityBps: 7_000},
		{Role: ValidatorRoleDelegationOperator, Description: "manages delegated capital strategy where authorized", RequiresAuthorization: true, RequiresFeeDisclosure: true, RequiresRiskPolicy: true, RewardWeightBps: 1_000, MinimumPerformanceBps: 8_000, MinimumEligibilityBps: 8_000},
		{Role: ValidatorRoleCollator, Description: "assembles transactions, state transitions, and proof bundles", RequiresValidator: true, RewardWeightBps: 1_000, MinimumPerformanceBps: 8_000, MinimumEligibilityBps: 8_000},
		{Role: ValidatorRoleFisherman, Description: "external fault detector submitting fraud proofs with deposit", RequiresDeposit: true, RewardWeightBps: 500, MinimumPerformanceBps: 6_000, MinimumEligibilityBps: 6_000},
	}}
}

func (r RoleRegistry) Validate() error {
	seen := make(map[ValidatorRole]struct{}, len(r.Rules))
	for _, rule := range r.Rules {
		if err := rule.Validate(); err != nil {
			return err
		}
		if _, found := seen[rule.Role]; found {
			return fmt.Errorf("duplicate role registry rule %q", rule.Role)
		}
		seen[rule.Role] = struct{}{}
	}
	return nil
}

func (r RoleRegistry) Rule(role ValidatorRole) (RoleRule, bool, error) {
	if err := validateValidatorRole(role); err != nil {
		return RoleRule{}, false, err
	}
	if err := r.Validate(); err != nil {
		return RoleRule{}, false, err
	}
	for _, rule := range r.Rules {
		if rule.Role == role {
			return rule, true, nil
		}
	}
	return RoleRule{}, false, nil
}

func (r RoleRule) Validate() error {
	if err := validateValidatorRole(r.Role); err != nil {
		return err
	}
	if strings.TrimSpace(r.Description) == "" {
		return errors.New("role registry description is required")
	}
	if r.RewardWeightBps > BasisPoints {
		return fmt.Errorf("role reward weight must be <= %d bps", BasisPoints)
	}
	if r.MinimumPerformanceBps > BasisPoints || r.MinimumEligibilityBps > BasisPoints {
		return fmt.Errorf("role minimum scores must be <= %d bps", BasisPoints)
	}
	if r.Role == ValidatorRoleCollator && r.CanFinalize {
		return errors.New("collator role cannot finalize without validator verification")
	}
	return nil
}

func CheckRoleEligibility(registry RoleRegistry, input RoleEligibilityInput) (RoleRecord, error) {
	input.ActorAddress = strings.TrimSpace(input.ActorAddress)
	if err := input.Params.Validate(); err != nil {
		return RoleRecord{}, err
	}
	rule, found, err := registry.Rule(input.Role)
	if err != nil {
		return RoleRecord{}, err
	}
	if !found {
		return RoleRecord{}, fmt.Errorf("missing role registry rule %q", input.Role)
	}
	if rule.RequiresValidator {
		if err := input.Candidate.Validate(input.Params); err != nil {
			return RoleRecord{}, err
		}
		if !ValidatorSupportsRole(input.Candidate, input.Role) && !legacyRoleAliasSupports(input.Candidate, input.Role) {
			return RoleRecord{}, fmt.Errorf("candidate does not support role %q", input.Role)
		}
		if rule.RequiresMinimumStake && input.Candidate.SelfStakeNaet.Add(input.Candidate.DelegatedStakeNaet).LT(input.Params.MinStakeNaet) {
			return RoleRecord{}, errors.New("role requires minimum validator stake")
		}
		input.ActorAddress = input.Candidate.ValidatorID
	}
	if input.ActorAddress == "" {
		input.ActorAddress = strings.TrimSpace(input.Candidate.ValidatorID)
	}
	if input.ActorAddress == "" {
		return RoleRecord{}, errors.New("role eligibility actor address is required")
	}
	if rule.RequiresDeposit && (input.DepositNaet.IsNil() || !input.DepositNaet.IsPositive()) {
		return RoleRecord{}, errors.New("role requires evidence or fraud-proof deposit")
	}
	if rule.RequiresAuthorization && !input.DelegationOperatorAuthorized {
		return RoleRecord{}, errors.New("delegation operator role requires authorization")
	}
	if rule.RequiresFeeDisclosure && !input.FeesDisclosed {
		return RoleRecord{}, errors.New("delegation operator role requires fee disclosure")
	}
	if rule.RequiresRiskPolicy && !input.RiskPolicyDisclosed {
		return RoleRecord{}, errors.New("delegation operator role requires risk policy disclosure")
	}
	performance := normalizeOptionalFactorBps(input.Candidate.PerformanceScoreBps)
	if !rule.RequiresValidator && performance == BasisPoints && input.Candidate.ValidatorID == "" {
		performance = rule.MinimumPerformanceBps
	}
	if performance < rule.MinimumPerformanceBps {
		return RoleRecord{}, errors.New("role performance below requirement")
	}
	uptime := normalizeOptionalFactorBps(input.Candidate.UptimeFactorBps)
	if !rule.RequiresValidator && input.Candidate.ValidatorID == "" {
		uptime = BasisPoints
	}
	eligibility := uint32((uint64(performance) + uint64(uptime)) / 2)
	record, err := NewRoleRecord(RoleRecord{
		ValidatorAddress: input.ActorAddress,
		Role:             input.Role,
		EpochID:          1,
		Status:           RoleStatusEligible,
		EligibilityScore: eligibility,
		Capacity:         input.Candidate.Capacity,
		PerformanceScore: performance,
	})
	if err != nil {
		return RoleRecord{}, err
	}
	if record.EligibilityScore < rule.MinimumEligibilityBps {
		return RoleRecord{}, errors.New("role eligibility score below requirement")
	}
	return record, nil
}

func ComputeRolePerformanceMetrics(record RoleRecord, completedTasks uint32, faultedTasks uint32, missedTasks uint32) (RolePerformanceMetrics, error) {
	if err := record.Validate(); err != nil {
		return RolePerformanceMetrics{}, err
	}
	if completedTasks+faultedTasks+missedTasks > record.AssignedTaskCount {
		return RolePerformanceMetrics{}, errors.New("role performance task counts exceed assigned tasks")
	}
	score := uint32(BasisPoints)
	if record.AssignedTaskCount > 0 {
		completedBps := ratioBps(uint64(completedTasks), uint64(record.AssignedTaskCount))
		faultPenalty := roleMinBps(uint64(faultedTasks) * 2_500)
		missedPenalty := roleMinBps(uint64(missedTasks) * 1_000)
		if uint64(faultPenalty)+uint64(missedPenalty) >= uint64(completedBps) {
			score = 0
		} else {
			score = completedBps - faultPenalty - missedPenalty
		}
	}
	return RolePerformanceMetrics{
		ValidatorAddress: record.ValidatorAddress,
		Role:             record.Role,
		EpochID:          record.EpochID,
		AssignedTasks:    record.AssignedTaskCount,
		CompletedTasks:   completedTasks,
		FaultedTasks:     faultedTasks,
		MissedTasks:      missedTasks,
		PerformanceScore: score,
	}, nil
}

func SettleRoleRewards(input RoleRewardInput) (WorkloadRewardSettlement, error) {
	outcomes := make([]AssignmentOutcome, 0, len(input.Records))
	for _, record := range input.Records {
		if err := record.Validate(); err != nil {
			return WorkloadRewardSettlement{}, err
		}
		outcomes = append(outcomes, AssignmentOutcome{
			TaskID:      fmt.Sprintf("role/%s/%d", record.Role, record.EpochID),
			Role:        record.Role,
			ValidatorID: record.ValidatorAddress,
			Completed:   record.Status == RoleStatusAssigned || record.Status == RoleStatusEligible,
			Faulted:     record.Status == RoleStatusSuspended,
			WorkUnits:   uint64(record.PerformanceScore) * uint64(roleMaxUint32(record.AssignedTaskCount, 1)),
		})
	}
	return SettleWorkloadRewards(WorkloadRewardInput{
		EpochID:          input.EpochID,
		TotalRewardsNaet: input.TotalRewardsNaet,
		RoleWeights:      input.Weights,
		Outcomes:         outcomes,
	})
}

func SuspendRoleOnFault(records []RoleRecord, validatorAddress string, role ValidatorRole, epochID uint64) ([]RoleRecord, error) {
	if err := validatePosToken("role suspension validator address", validatorAddress); err != nil {
		return nil, err
	}
	if err := validateValidatorRole(role); err != nil {
		return nil, err
	}
	out := make([]RoleRecord, len(records))
	copy(out, records)
	found := false
	for i, record := range out {
		if record.ValidatorAddress == validatorAddress && record.Role == role && record.EpochID == epochID {
			record.Status = RoleStatusSuspended
			record.AssignedTaskCount = 0
			out[i] = record
			found = true
			break
		}
	}
	if !found {
		return nil, errors.New("role record not found for suspension")
	}
	return out, ValidateRoleRecords(out)
}

func legacyRoleAliasSupports(candidate Candidate, role ValidatorRole) bool {
	if role == ValidatorRoleProposer {
		return ValidatorSupportsRole(candidate, ValidatorRoleBlockProducer)
	}
	if role == ValidatorRoleEvidenceReporter {
		return ValidatorSupportsRole(candidate, ValidatorRoleEvidenceReviewer)
	}
	return false
}

func roleMinBps(value uint64) uint32 {
	if value >= uint64(BasisPoints) {
		return BasisPoints
	}
	return uint32(value)
}

func roleMaxUint32(left uint32, right uint32) uint32 {
	if left >= right {
		return left
	}
	return right
}

func AllValidatorRoles() []ValidatorRole {
	return []ValidatorRole{
		ValidatorRoleValidator,
		ValidatorRoleProposer,
		ValidatorRoleBlockProducer,
		ValidatorRoleVerifier,
		ValidatorRoleEvidenceReporter,
		ValidatorRoleDelegationOperator,
		ValidatorRoleCollator,
		ValidatorRoleFisherman,
		ValidatorRoleEvidenceReviewer,
	}
}

func DefaultTaskRoles() []ValidatorRole {
	return []ValidatorRole{ValidatorRoleBlockProducer, ValidatorRoleVerifier}
}

func validateValidatorRole(role ValidatorRole) error {
	switch role {
	case ValidatorRoleValidator,
		ValidatorRoleProposer,
		ValidatorRoleBlockProducer,
		ValidatorRoleVerifier,
		ValidatorRoleEvidenceReporter,
		ValidatorRoleDelegationOperator,
		ValidatorRoleCollator,
		ValidatorRoleFisherman,
		ValidatorRoleEvidenceReviewer:
		return nil
	default:
		return fmt.Errorf("unsupported validator role %q", role)
	}
}

func validateRoleStatus(status string) error {
	switch status {
	case RoleStatusEligible, RoleStatusAssigned, RoleStatusSuspended, RoleStatusInactive:
		return nil
	default:
		return fmt.Errorf("unsupported role status %q", status)
	}
}

func validateValidatorRoles(roles []ValidatorRole) error {
	seen := make(map[ValidatorRole]struct{}, len(roles))
	for _, role := range roles {
		if err := validateValidatorRole(role); err != nil {
			return err
		}
		if _, found := seen[role]; found {
			return fmt.Errorf("duplicate validator role %q", role)
		}
		seen[role] = struct{}{}
	}
	return nil
}

func normalizedRoles(roles []ValidatorRole, defaults []ValidatorRole) []ValidatorRole {
	if len(roles) == 0 {
		roles = defaults
	}
	out := make([]ValidatorRole, len(roles))
	copy(out, roles)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i] < out[j]
	})
	return out
}

func validateRoleRewardWeights(weights []RoleRewardWeight) error {
	if len(weights) == 0 {
		return errors.New("role reward weights are required")
	}
	total := uint64(0)
	seen := make(map[ValidatorRole]struct{}, len(weights))
	for _, weight := range weights {
		if err := validateValidatorRole(weight.Role); err != nil {
			return err
		}
		if _, found := seen[weight.Role]; found {
			return fmt.Errorf("duplicate role reward weight %q", weight.Role)
		}
		seen[weight.Role] = struct{}{}
		total += uint64(weight.WeightBps)
	}
	if total != uint64(BasisPoints) {
		return fmt.Errorf("role reward weights must sum to %d bps", BasisPoints)
	}
	return nil
}
