package types

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	sdkmath "cosmossdk.io/math"
)

const (
	DefaultDelegationActivationEpochs = uint64(1)
	DefaultEvidenceWindowEpochs       = uint64(4)
	DefaultMinTaskGroupValidators     = uint32(3)
	DefaultMaxTaskGroupValidators     = uint32(21)
	DefaultReporterRewardBps          = uint32(500)
	MaxReporterRewardBps              = uint32(2_000)

	PosHashHexLength = 64
	PosEmptyRootHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	DefaultWorkloadClass = "general"
	maxPosTokenLength    = 128
)

const (
	WorkloadTypeGlobalConsensus      WorkloadType = "global_consensus"
	WorkloadTypeZoneExecution        WorkloadType = "zone_execution"
	WorkloadTypeShardExecution       WorkloadType = "shard_execution"
	WorkloadTypeProofVerification    WorkloadType = "proof_verification"
	WorkloadTypeEvidenceVerification WorkloadType = "evidence_verification"
	WorkloadTypeDataAvailability     WorkloadType = "data_availability"
	WorkloadTypeServiceValidation    WorkloadType = "service_validation"
)

type CapacityFaultEvidence struct {
	ValidatorID       string
	WorkloadID        string
	WorkloadType      WorkloadType
	AssignmentEpoch   uint64
	EvidenceHeight    int64
	UsedForAssignment bool
	Finalized         bool
}

type EvidenceRecord struct {
	EvidenceID          string
	EvidenceType        string
	AccusedValidator    string
	Reporter            string
	EpochID             uint64
	TaskGroupIDOptional string
	ObjectHash          string
	ProofPayloadHash    string
	SubmittedHeight     int64
	Status              string
	VerificationGroupID string
	DecisionHeight      int64
	PenaltyIDOptional   string
}

type EvidenceVerificationGroupInput struct {
	Params               Params
	Epoch                EpochRecord
	ActiveValidators     []ScoredValidator
	Evidence             EvidenceRecord
	MinimumGroupSize     uint32
	DecisionThresholdBps uint32
}

type EvidenceVerificationGroup struct {
	EvidenceID           string
	EpochID              uint64
	VerificationGroupID  string
	Members              []string
	ExcludedValidators   []string
	MinimumGroupSize     uint32
	DecisionThresholdBps uint32
	AssignmentSeed       string
	GroupHash            string
}

type StructuredEvidenceRecord struct {
	EvidenceID           string
	EvidenceType         string
	ReporterID           string
	AccusedValidatorID   string
	SubjectID            string
	EvidenceHash         string
	EvidenceHeight       int64
	EvidenceEpoch        uint64
	SubmittedHeight      int64
	VerificationGroupID  string
	Status               string
	StructuredRecordHash string
}

type EvidenceSlashPolicy struct {
	EvidenceType     string
	Misbehavior      string
	SlashFractionBps uint32
}

type EvidenceVerificationVote struct {
	EvidenceID    string
	ReviewerID    string
	Accepted      bool
	SignatureHash string
	VoteHeight    int64
}

type EvidenceVerificationResult struct {
	EvidenceID        string
	AcceptedVotes     uint32
	RejectedVotes     uint32
	TotalReviewers    uint32
	ParticipationBps  uint32
	QuorumBps         uint32
	Accepted          bool
	Rejected          bool
	Status            string
	VerificationRoot  string
	VerificationGroup string
}

type EvidenceFinalityVote struct {
	EvidenceID     string
	ValidatorID    string
	Approve        bool
	VotingPowerBps uint32
	SignatureHash  string
	FinalityHeight int64
}

type EvidenceFinalityDecision struct {
	EvidenceID        string
	AcceptedPowerBps  uint32
	RejectedPowerBps  uint32
	QuorumBps         uint32
	Finalized         bool
	Accepted          bool
	Status            string
	FinalityVoteRoot  string
	FinalityVoteCount uint32
}

type EvidenceCase struct {
	EvidenceID       string
	ReporterID       string
	ValidatorID      string
	Misbehavior      string
	SlashFractionBps uint32
	EvidenceHeight   int64
	EvidenceEpoch    uint64
	Finalized        bool
}

type EvidenceSettlement struct {
	EvidenceID         string
	ReporterID         string
	Slash              SlashDistribution
	ReporterRewardNaet sdkmath.Int
	BurnNaet           sdkmath.Int
	SettlementHash     string
}

func EvidenceWithinSlashableWindow(params Params, evidenceEpoch uint64, currentEpoch uint64) (bool, error) {
	if err := params.Validate(); err != nil {
		return false, err
	}
	if currentEpoch < evidenceEpoch {
		return false, errors.New("current epoch cannot be before evidence epoch")
	}
	return currentEpoch-evidenceEpoch <= params.EvidenceWindowEpochs, nil
}

func (e CapacityFaultEvidence) Validate() error {
	if err := validatePosToken("capacity evidence validator id", e.ValidatorID); err != nil {
		return err
	}
	if err := validatePosToken("capacity evidence workload id", e.WorkloadID); err != nil {
		return err
	}
	if err := validateWorkloadType(e.WorkloadType); err != nil {
		return err
	}
	if e.AssignmentEpoch == 0 {
		return errors.New("capacity evidence assignment epoch is required")
	}
	if e.EvidenceHeight < 0 {
		return errors.New("capacity evidence height cannot be negative")
	}
	return nil
}

func IsSlashableCapacityFault(evidence CapacityFaultEvidence) (bool, error) {
	if err := evidence.Validate(); err != nil {
		return false, err
	}
	return evidence.Finalized && evidence.UsedForAssignment, nil
}

func NewEvidenceRecord(record EvidenceRecord) (EvidenceRecord, error) {
	record.EvidenceID = strings.TrimSpace(record.EvidenceID)
	record.EvidenceType = strings.TrimSpace(record.EvidenceType)
	record.AccusedValidator = strings.TrimSpace(record.AccusedValidator)
	record.Reporter = strings.TrimSpace(record.Reporter)
	record.TaskGroupIDOptional = strings.TrimSpace(record.TaskGroupIDOptional)
	record.ObjectHash = strings.TrimSpace(record.ObjectHash)
	record.ProofPayloadHash = strings.TrimSpace(record.ProofPayloadHash)
	record.VerificationGroupID = strings.TrimSpace(record.VerificationGroupID)
	record.PenaltyIDOptional = strings.TrimSpace(record.PenaltyIDOptional)
	if record.Status == "" {
		record.Status = EvidenceStatusSubmitted
	}
	return record, record.Validate()
}

func EvidenceRecordFieldNames() []string {
	return []string{
		"evidence_id",
		"evidence_type",
		"accused_validator",
		"reporter",
		"epoch_id",
		"task_group_id_optional",
		"object_hash",
		"proof_payload_hash",
		"submitted_height",
		"status",
		"verification_group_id",
		"decision_height",
		"penalty_id_optional",
	}
}

func EvidenceRecordStatusValues() []string {
	return []string{
		EvidenceStatusSubmitted,
		EvidenceStatusInVerification,
		EvidenceStatusAccepted,
		EvidenceStatusRejected,
		EvidenceStatusExpired,
		EvidenceStatusSlashed,
	}
}

func (e EvidenceRecord) Validate() error {
	if err := validatePosToken("evidence record id", e.EvidenceID); err != nil {
		return err
	}
	if !IsStructuredEvidenceType(e.EvidenceType) {
		return fmt.Errorf("unsupported evidence record type %q", e.EvidenceType)
	}
	if err := validatePosToken("evidence record accused validator", e.AccusedValidator); err != nil {
		return err
	}
	if err := validatePosToken("evidence record reporter", e.Reporter); err != nil {
		return err
	}
	if e.EpochID == 0 {
		return errors.New("evidence record epoch id is required")
	}
	if e.TaskGroupIDOptional != "" {
		if err := validatePosToken("evidence record task group id", e.TaskGroupIDOptional); err != nil {
			return err
		}
	}
	if err := validatePosHash("evidence record object hash", e.ObjectHash); err != nil {
		return err
	}
	if err := validatePosHash("evidence record proof payload hash", e.ProofPayloadHash); err != nil {
		return err
	}
	if e.SubmittedHeight < 0 {
		return errors.New("evidence record submitted height cannot be negative")
	}
	if !isEvidenceRecordStatus(e.Status) {
		return fmt.Errorf("unsupported evidence record status %q", e.Status)
	}
	if e.VerificationGroupID != "" {
		if err := validatePosToken("evidence record verification group id", e.VerificationGroupID); err != nil {
			return err
		}
	}
	if (e.Status == EvidenceStatusInVerification || e.Status == EvidenceStatusAccepted || e.Status == EvidenceStatusRejected || e.Status == EvidenceStatusSlashed) && e.VerificationGroupID == "" {
		return errors.New("evidence record status requires verification group id")
	}
	if e.DecisionHeight < 0 {
		return errors.New("evidence record decision height cannot be negative")
	}
	if e.PenaltyIDOptional != "" {
		if err := validatePosToken("evidence record penalty id", e.PenaltyIDOptional); err != nil {
			return err
		}
	}
	if e.Status == EvidenceStatusSlashed && e.PenaltyIDOptional == "" {
		return errors.New("slashed evidence record requires penalty id")
	}
	if (e.Status == EvidenceStatusAccepted || e.Status == EvidenceStatusRejected || e.Status == EvidenceStatusExpired || e.Status == EvidenceStatusSlashed) && e.DecisionHeight == 0 {
		return errors.New("decided evidence record requires decision height")
	}
	return nil
}

func AssignEvidenceVerificationGroup(record EvidenceRecord, group EvidenceVerificationGroup) (EvidenceRecord, error) {
	if err := record.Validate(); err != nil {
		return EvidenceRecord{}, err
	}
	if err := group.Validate(); err != nil {
		return EvidenceRecord{}, err
	}
	if record.EvidenceID != group.EvidenceID {
		return EvidenceRecord{}, errors.New("evidence verification group record id mismatch")
	}
	if record.EpochID != group.EpochID {
		return EvidenceRecord{}, errors.New("evidence verification group record epoch mismatch")
	}
	next := record
	next.VerificationGroupID = group.VerificationGroupID
	next.Status = EvidenceStatusInVerification
	next.DecisionHeight = 0
	next.PenaltyIDOptional = ""
	return next, next.Validate()
}

func AdvanceEvidenceRecordStatus(record EvidenceRecord, status string, decisionHeight int64, penaltyIDOptional string) (EvidenceRecord, error) {
	if err := record.Validate(); err != nil {
		return EvidenceRecord{}, err
	}
	if !isEvidenceRecordStatus(status) {
		return EvidenceRecord{}, fmt.Errorf("unsupported evidence record status %q", status)
	}
	if !isAllowedEvidenceRecordTransition(record.Status, status) {
		return EvidenceRecord{}, fmt.Errorf("invalid evidence record status transition %s -> %s", record.Status, status)
	}
	next := record
	next.Status = status
	next.DecisionHeight = decisionHeight
	next.PenaltyIDOptional = strings.TrimSpace(penaltyIDOptional)
	return next, next.Validate()
}

func SelectEvidenceVerificationGroup(input EvidenceVerificationGroupInput) (EvidenceVerificationGroup, error) {
	if err := input.Params.Validate(); err != nil {
		return EvidenceVerificationGroup{}, err
	}
	if err := input.Epoch.Validate(); err != nil {
		return EvidenceVerificationGroup{}, err
	}
	if err := input.Evidence.Validate(); err != nil {
		return EvidenceVerificationGroup{}, err
	}
	if input.Evidence.EpochID != input.Epoch.EpochID {
		return EvidenceVerificationGroup{}, errors.New("evidence record epoch does not match verification epoch")
	}
	minimum := input.MinimumGroupSize
	if minimum == 0 {
		minimum = input.Params.MinTaskGroupValidators
	}
	if minimum == 0 {
		return EvidenceVerificationGroup{}, errors.New("evidence verification group minimum size is required")
	}
	threshold := input.DecisionThresholdBps
	if threshold == 0 {
		threshold = DefaultEvidenceVerificationQuorumBps
	}
	if threshold > BasisPoints {
		return EvidenceVerificationGroup{}, fmt.Errorf("evidence decision threshold must be <= %d bps", BasisPoints)
	}
	excluded := evidenceVerificationExclusions(input.Evidence, input.ActiveValidators)
	eligible := make([]string, 0, len(input.ActiveValidators))
	seen := make(map[string]struct{}, len(input.ActiveValidators))
	for _, validator := range input.ActiveValidators {
		validatorID := strings.TrimSpace(validator.ValidatorID)
		if validatorID == "" {
			return EvidenceVerificationGroup{}, errors.New("active validator id is required")
		}
		if _, duplicate := seen[validatorID]; duplicate {
			return EvidenceVerificationGroup{}, fmt.Errorf("duplicate active validator %q", validatorID)
		}
		seen[validatorID] = struct{}{}
		if isExcludedValidator(validatorID, excluded) {
			continue
		}
		eligible = append(eligible, validatorID)
	}
	if uint32(len(eligible)) < minimum {
		return EvidenceVerificationGroup{}, fmt.Errorf("insufficient eligible validators for evidence verification group: need %d got %d", minimum, len(eligible))
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		left := computeEvidenceVerifierSelectionHash(input.Epoch.Seed, input.Evidence.EvidenceID, eligible[i])
		right := computeEvidenceVerifierSelectionHash(input.Epoch.Seed, input.Evidence.EvidenceID, eligible[j])
		if left != right {
			return left < right
		}
		return eligible[i] < eligible[j]
	})
	members := cloneStringSlice(eligible[:minimum])
	sort.Strings(members)
	sort.Strings(excluded)
	group := EvidenceVerificationGroup{
		EvidenceID:           input.Evidence.EvidenceID,
		EpochID:              input.Evidence.EpochID,
		Members:              members,
		ExcludedValidators:   excluded,
		MinimumGroupSize:     minimum,
		DecisionThresholdBps: threshold,
		AssignmentSeed:       computeEvidenceVerificationAssignmentSeed(input.Epoch.Seed, input.Evidence.EvidenceID),
	}
	group.VerificationGroupID = computeEvidenceVerificationGroupID(group)
	group.GroupHash = computeEvidenceVerificationGroupHash(group)
	return group, group.Validate()
}

func (g EvidenceVerificationGroup) Validate() error {
	if err := validatePosToken("evidence verification group evidence id", g.EvidenceID); err != nil {
		return err
	}
	if g.EpochID == 0 {
		return errors.New("evidence verification group epoch id is required")
	}
	if err := validatePosToken("evidence verification group id", g.VerificationGroupID); err != nil {
		return err
	}
	if len(g.Members) == 0 {
		return errors.New("evidence verification group members are required")
	}
	if g.MinimumGroupSize == 0 || uint32(len(g.Members)) < g.MinimumGroupSize {
		return errors.New("evidence verification group minimum size is not met")
	}
	if g.DecisionThresholdBps == 0 || g.DecisionThresholdBps > BasisPoints {
		return fmt.Errorf("evidence verification decision threshold must be within 1..%d bps", BasisPoints)
	}
	if err := validateSortedUniqueTokens("evidence verification group member", g.Members); err != nil {
		return err
	}
	if err := validateSortedUniqueTokens("evidence verification group exclusion", g.ExcludedValidators); err != nil {
		return err
	}
	for _, member := range g.Members {
		if isExcludedValidator(member, g.ExcludedValidators) {
			return fmt.Errorf("evidence verification group member %q is excluded", member)
		}
	}
	if err := validatePosHash("evidence verification group assignment seed", g.AssignmentSeed); err != nil {
		return err
	}
	expectedID := computeEvidenceVerificationGroupID(g)
	if g.VerificationGroupID != expectedID {
		return errors.New("evidence verification group id mismatch")
	}
	expectedHash := computeEvidenceVerificationGroupHash(g)
	if g.GroupHash != expectedHash {
		return errors.New("evidence verification group hash mismatch")
	}
	return nil
}

func StructuredEvidenceTypes() []string {
	return []string{
		EvidenceTypeDoubleSignProof,
		EvidenceTypeInvalidStateTransitionProof,
		EvidenceTypeEquivocationProof,
		EvidenceTypeDowntimeProof,
		EvidenceTypeInvalidTaskExecutionProof,
		EvidenceTypeInvalidCollatorOutputProof,
		EvidenceTypeInvalidProofAcceptance,
		EvidenceTypeFalseCapacityDeclaration,
		EvidenceTypeInvalidEvidenceSubmission,
	}
}

func IsStructuredEvidenceType(evidenceType string) bool {
	switch evidenceType {
	case EvidenceTypeDoubleSignProof,
		EvidenceTypeInvalidStateTransitionProof,
		EvidenceTypeEquivocationProof,
		EvidenceTypeDowntimeProof,
		EvidenceTypeInvalidTaskExecutionProof,
		EvidenceTypeInvalidCollatorOutputProof,
		EvidenceTypeInvalidProofAcceptance,
		EvidenceTypeFalseCapacityDeclaration,
		EvidenceTypeInvalidEvidenceSubmission:
		return true
	default:
		return false
	}
}

func DefaultEvidenceSlashPolicy(evidenceType string) (EvidenceSlashPolicy, error) {
	switch evidenceType {
	case EvidenceTypeDoubleSignProof:
		return EvidenceSlashPolicy{EvidenceType: evidenceType, Misbehavior: MisbehaviorDoubleSign, SlashFractionBps: DefaultDoubleSignSlashBps}, nil
	case EvidenceTypeInvalidStateTransitionProof:
		return EvidenceSlashPolicy{EvidenceType: evidenceType, Misbehavior: MisbehaviorInvalidBlock, SlashFractionBps: DefaultInvalidStateTransitionSlashBps}, nil
	case EvidenceTypeEquivocationProof:
		return EvidenceSlashPolicy{EvidenceType: evidenceType, Misbehavior: MisbehaviorDoubleSign, SlashFractionBps: DefaultEquivocationSlashBps}, nil
	case EvidenceTypeDowntimeProof:
		return EvidenceSlashPolicy{EvidenceType: evidenceType, Misbehavior: MisbehaviorDowntime, SlashFractionBps: DefaultDowntimeSlashBps}, nil
	case EvidenceTypeInvalidTaskExecutionProof:
		return EvidenceSlashPolicy{EvidenceType: evidenceType, Misbehavior: MisbehaviorInvalidBlock, SlashFractionBps: DefaultInvalidTaskExecutionSlashBps}, nil
	case EvidenceTypeInvalidCollatorOutputProof:
		return EvidenceSlashPolicy{EvidenceType: evidenceType, Misbehavior: MisbehaviorInvalidBlock, SlashFractionBps: DefaultInvalidCollatorOutputSlashBps}, nil
	case EvidenceTypeInvalidProofAcceptance:
		return EvidenceSlashPolicy{EvidenceType: evidenceType, Misbehavior: MisbehaviorInvalidBlock, SlashFractionBps: DefaultInvalidProofAcceptanceSlashBps}, nil
	case EvidenceTypeFalseCapacityDeclaration:
		return EvidenceSlashPolicy{EvidenceType: evidenceType, Misbehavior: MisbehaviorInvalidBlock, SlashFractionBps: DefaultFalseCapacityDeclarationSlashBps}, nil
	case EvidenceTypeInvalidEvidenceSubmission:
		return EvidenceSlashPolicy{EvidenceType: evidenceType, Misbehavior: MisbehaviorInvalidBlock, SlashFractionBps: DefaultInvalidEvidenceSubmissionSlashBps}, nil
	default:
		return EvidenceSlashPolicy{}, fmt.Errorf("unsupported structured evidence type %q", evidenceType)
	}
}

func SubmitStructuredEvidence(evidence StructuredEvidenceRecord) (StructuredEvidenceRecord, error) {
	evidence.EvidenceID = strings.TrimSpace(evidence.EvidenceID)
	evidence.EvidenceType = strings.TrimSpace(evidence.EvidenceType)
	evidence.ReporterID = strings.TrimSpace(evidence.ReporterID)
	evidence.AccusedValidatorID = strings.TrimSpace(evidence.AccusedValidatorID)
	evidence.SubjectID = strings.TrimSpace(evidence.SubjectID)
	evidence.EvidenceHash = strings.TrimSpace(evidence.EvidenceHash)
	evidence.VerificationGroupID = strings.TrimSpace(evidence.VerificationGroupID)
	evidence.Status = EvidenceStatusSubmitted
	evidence.StructuredRecordHash = computeStructuredEvidenceHash(evidence)
	return evidence, evidence.Validate()
}

func (e StructuredEvidenceRecord) Validate() error {
	if err := validatePosToken("structured evidence id", e.EvidenceID); err != nil {
		return err
	}
	if !IsStructuredEvidenceType(e.EvidenceType) {
		return fmt.Errorf("unsupported structured evidence type %q", e.EvidenceType)
	}
	if err := validatePosToken("structured evidence reporter id", e.ReporterID); err != nil {
		return err
	}
	if err := validatePosToken("structured evidence accused validator id", e.AccusedValidatorID); err != nil {
		return err
	}
	if err := validatePosToken("structured evidence subject id", e.SubjectID); err != nil {
		return err
	}
	if err := validatePosHash("structured evidence hash", e.EvidenceHash); err != nil {
		return err
	}
	if e.EvidenceHeight < 0 {
		return errors.New("structured evidence height cannot be negative")
	}
	if e.EvidenceEpoch == 0 {
		return errors.New("structured evidence epoch is required")
	}
	if e.SubmittedHeight < 0 {
		return errors.New("structured evidence submitted height cannot be negative")
	}
	if err := validatePosToken("structured evidence verification group id", e.VerificationGroupID); err != nil {
		return err
	}
	if !isEvidenceStatus(e.Status) {
		return fmt.Errorf("unsupported structured evidence status %q", e.Status)
	}
	expectedHash := computeStructuredEvidenceHash(e)
	if e.StructuredRecordHash != expectedHash {
		return errors.New("structured evidence record hash mismatch")
	}
	return nil
}

func VerifyStructuredEvidenceBySubset(evidence StructuredEvidenceRecord, reviewers []string, votes []EvidenceVerificationVote, quorumBps uint32) (EvidenceVerificationResult, error) {
	if quorumBps == 0 {
		quorumBps = DefaultEvidenceVerificationQuorumBps
	}
	if quorumBps > BasisPoints {
		return EvidenceVerificationResult{}, fmt.Errorf("evidence verification quorum must be <= %d bps", BasisPoints)
	}
	if err := evidence.Validate(); err != nil {
		return EvidenceVerificationResult{}, err
	}
	if evidence.Status != EvidenceStatusSubmitted && evidence.Status != EvidenceStatusVerified {
		return EvidenceVerificationResult{}, errors.New("structured evidence must be submitted before subset verification")
	}
	reviewerSet, err := validateEvidenceReviewers(reviewers)
	if err != nil {
		return EvidenceVerificationResult{}, err
	}
	seen := make(map[string]struct{}, len(votes))
	accepted := uint32(0)
	rejected := uint32(0)
	for _, vote := range votes {
		if err := vote.Validate(evidence.EvidenceID); err != nil {
			return EvidenceVerificationResult{}, err
		}
		if _, assigned := reviewerSet[vote.ReviewerID]; !assigned {
			return EvidenceVerificationResult{}, fmt.Errorf("evidence reviewer %q is not assigned to verification subset", vote.ReviewerID)
		}
		if _, found := seen[vote.ReviewerID]; found {
			return EvidenceVerificationResult{}, fmt.Errorf("duplicate evidence verification vote from %q", vote.ReviewerID)
		}
		seen[vote.ReviewerID] = struct{}{}
		if vote.Accepted {
			accepted++
		} else {
			rejected++
		}
	}
	totalReviewers := uint32(len(reviewerSet))
	acceptedBps := ratioBps(uint64(accepted), uint64(totalReviewers))
	rejectedBps := ratioBps(uint64(rejected), uint64(totalReviewers))
	result := EvidenceVerificationResult{
		EvidenceID:        evidence.EvidenceID,
		AcceptedVotes:     accepted,
		RejectedVotes:     rejected,
		TotalReviewers:    totalReviewers,
		ParticipationBps:  ratioBps(uint64(len(votes)), uint64(totalReviewers)),
		QuorumBps:         quorumBps,
		Accepted:          acceptedBps >= quorumBps,
		Rejected:          rejectedBps >= quorumBps,
		Status:            EvidenceStatusSubmitted,
		VerificationGroup: evidence.VerificationGroupID,
	}
	if result.Accepted {
		result.Status = EvidenceStatusVerified
	} else if result.Rejected {
		result.Status = EvidenceStatusRejected
	}
	result.VerificationRoot = computeEvidenceVerificationRoot(evidence.EvidenceID, votes)
	return result, nil
}

func (v EvidenceVerificationVote) Validate(expectedEvidenceID string) error {
	if v.EvidenceID != expectedEvidenceID {
		return errors.New("evidence verification vote id mismatch")
	}
	if err := validatePosToken("evidence verification reviewer id", v.ReviewerID); err != nil {
		return err
	}
	if err := validatePosHash("evidence verification signature hash", v.SignatureHash); err != nil {
		return err
	}
	if v.VoteHeight < 0 {
		return errors.New("evidence verification vote height cannot be negative")
	}
	return nil
}

func FinalizeStructuredEvidence(evidence StructuredEvidenceRecord, verification EvidenceVerificationResult, votes []EvidenceFinalityVote, quorumBps uint32) (EvidenceFinalityDecision, error) {
	if quorumBps == 0 {
		quorumBps = DefaultEvidenceFinalityQuorumBps
	}
	if quorumBps > BasisPoints {
		return EvidenceFinalityDecision{}, fmt.Errorf("evidence finality quorum must be <= %d bps", BasisPoints)
	}
	if err := evidence.Validate(); err != nil {
		return EvidenceFinalityDecision{}, err
	}
	if verification.EvidenceID != evidence.EvidenceID {
		return EvidenceFinalityDecision{}, errors.New("evidence finality verification id mismatch")
	}
	if !verification.Accepted || verification.Status != EvidenceStatusVerified {
		return EvidenceFinalityDecision{}, errors.New("evidence must be verified by consensus subset before finality vote")
	}
	seen := make(map[string]struct{}, len(votes))
	acceptedPower := uint64(0)
	rejectedPower := uint64(0)
	totalPower := uint64(0)
	for _, vote := range votes {
		if err := vote.Validate(evidence.EvidenceID); err != nil {
			return EvidenceFinalityDecision{}, err
		}
		if _, found := seen[vote.ValidatorID]; found {
			return EvidenceFinalityDecision{}, fmt.Errorf("duplicate evidence finality vote from %q", vote.ValidatorID)
		}
		seen[vote.ValidatorID] = struct{}{}
		totalPower += uint64(vote.VotingPowerBps)
		if totalPower > uint64(BasisPoints) {
			return EvidenceFinalityDecision{}, fmt.Errorf("evidence finality voting power must be <= %d bps", BasisPoints)
		}
		if vote.Approve {
			acceptedPower += uint64(vote.VotingPowerBps)
		} else {
			rejectedPower += uint64(vote.VotingPowerBps)
		}
	}
	decision := EvidenceFinalityDecision{
		EvidenceID:        evidence.EvidenceID,
		AcceptedPowerBps:  uint32(acceptedPower),
		RejectedPowerBps:  uint32(rejectedPower),
		QuorumBps:         quorumBps,
		FinalityVoteRoot:  computeEvidenceFinalityVoteRoot(evidence.EvidenceID, votes),
		FinalityVoteCount: uint32(len(votes)),
		Status:            EvidenceStatusVerified,
	}
	if uint32(acceptedPower) >= quorumBps {
		decision.Finalized = true
		decision.Accepted = true
		decision.Status = EvidenceStatusFinalized
	} else if uint32(rejectedPower) >= quorumBps {
		decision.Finalized = true
		decision.Status = EvidenceStatusRejected
	}
	return decision, nil
}

func (v EvidenceFinalityVote) Validate(expectedEvidenceID string) error {
	if v.EvidenceID != expectedEvidenceID {
		return errors.New("evidence finality vote id mismatch")
	}
	if err := validatePosToken("evidence finality validator id", v.ValidatorID); err != nil {
		return err
	}
	if v.VotingPowerBps == 0 || v.VotingPowerBps > BasisPoints {
		return fmt.Errorf("evidence finality voting power must be within 1..%d bps", BasisPoints)
	}
	if err := validatePosHash("evidence finality signature hash", v.SignatureHash); err != nil {
		return err
	}
	if v.FinalityHeight < 0 {
		return errors.New("evidence finality vote height cannot be negative")
	}
	return nil
}

func ExecuteStructuredEvidenceSlashing(params Params, currentEpoch uint64, evidence StructuredEvidenceRecord, decision EvidenceFinalityDecision, selfStake sdkmath.Int, nominations []Nomination) (EvidenceSettlement, error) {
	if decision.EvidenceID != evidence.EvidenceID {
		return EvidenceSettlement{}, errors.New("evidence slashing decision id mismatch")
	}
	if !decision.Finalized || !decision.Accepted || decision.Status != EvidenceStatusFinalized {
		return EvidenceSettlement{}, errors.New("evidence must have accepted finality before slashing")
	}
	if err := evidence.Validate(); err != nil {
		return EvidenceSettlement{}, err
	}
	policy, err := DefaultEvidenceSlashPolicy(evidence.EvidenceType)
	if err != nil {
		return EvidenceSettlement{}, err
	}
	return SettleEvidenceCase(params, currentEpoch, EvidenceCase{
		EvidenceID:       evidence.EvidenceID,
		ReporterID:       evidence.ReporterID,
		ValidatorID:      evidence.AccusedValidatorID,
		Misbehavior:      policy.Misbehavior,
		SlashFractionBps: policy.SlashFractionBps,
		EvidenceHeight:   evidence.EvidenceHeight,
		EvidenceEpoch:    evidence.EvidenceEpoch,
		Finalized:        true,
	}, selfStake, nominations)
}

func SettleEvidenceCase(params Params, currentEpoch uint64, evidence EvidenceCase, selfStake sdkmath.Int, nominations []Nomination) (EvidenceSettlement, error) {
	if err := evidence.Validate(params, currentEpoch); err != nil {
		return EvidenceSettlement{}, err
	}
	slash, err := ComputeSlash(SlashInput{
		ValidatorID:       evidence.ValidatorID,
		Misbehavior:       evidence.Misbehavior,
		SlashFractionBps:  evidence.SlashFractionBps,
		SelfStakeNaet:     selfStake,
		Nominations:       nominations,
		EvidenceHeight:    evidence.EvidenceHeight,
		EvidenceFinalized: true,
	})
	if err != nil {
		return EvidenceSettlement{}, err
	}
	reporterReward := mulIntBps(slash.TotalSlashedNaet, params.ReporterRewardBps)
	settlement := EvidenceSettlement{
		EvidenceID:         evidence.EvidenceID,
		ReporterID:         evidence.ReporterID,
		Slash:              slash,
		ReporterRewardNaet: reporterReward,
		BurnNaet:           slash.TotalSlashedNaet.Sub(reporterReward),
	}
	settlement.SettlementHash = computeEvidenceSettlementHash(settlement)
	return settlement, nil
}

func (e EvidenceCase) Validate(params Params, currentEpoch uint64) error {
	if err := params.Validate(); err != nil {
		return err
	}
	if err := validatePosToken("evidence id", e.EvidenceID); err != nil {
		return err
	}
	if err := validatePosToken("evidence reporter id", e.ReporterID); err != nil {
		return err
	}
	if err := validatePosToken("evidence validator id", e.ValidatorID); err != nil {
		return err
	}
	if !IsSlashableMisbehavior(e.Misbehavior) {
		return fmt.Errorf("unsupported misbehavior %q", e.Misbehavior)
	}
	if e.SlashFractionBps == 0 || e.SlashFractionBps > BasisPoints {
		return fmt.Errorf("slash fraction must be within 1..%d bps", BasisPoints)
	}
	if e.EvidenceHeight < 0 {
		return errors.New("evidence height cannot be negative")
	}
	if !e.Finalized {
		return errors.New("evidence must be finalized before settlement")
	}
	withinWindow, err := EvidenceWithinSlashableWindow(params, e.EvidenceEpoch, currentEpoch)
	if err != nil {
		return err
	}
	if !withinWindow {
		return errors.New("evidence is outside slashable window")
	}
	return nil
}

func computeEvidenceSettlementHash(settlement EvidenceSettlement) string {
	return posHashRoot("aetheris-pos-evidence-settlement-v1", func(w posByteWriter) {
		posWritePart(w, settlement.EvidenceID)
		posWritePart(w, settlement.ReporterID)
		posWritePart(w, settlement.Slash.ValidatorID)
		posWritePart(w, settlement.Slash.Misbehavior)
		posWritePart(w, settlement.Slash.TotalSlashedNaet.String())
		posWritePart(w, settlement.ReporterRewardNaet.String())
		posWritePart(w, settlement.BurnNaet.String())
		posWriteUint64(w, uint64(settlement.Slash.EvidenceHeight))
	})
}

func computeStructuredEvidenceHash(evidence StructuredEvidenceRecord) string {
	return posHashRoot("aetheris-pos-structured-evidence-v1", func(w posByteWriter) {
		posWritePart(w, evidence.EvidenceID)
		posWritePart(w, evidence.EvidenceType)
		posWritePart(w, evidence.ReporterID)
		posWritePart(w, evidence.AccusedValidatorID)
		posWritePart(w, evidence.SubjectID)
		posWritePart(w, evidence.EvidenceHash)
		posWritePart(w, fmt.Sprintf("%d", evidence.EvidenceHeight))
		posWriteUint64(w, evidence.EvidenceEpoch)
		posWritePart(w, fmt.Sprintf("%d", evidence.SubmittedHeight))
		posWritePart(w, evidence.VerificationGroupID)
		posWritePart(w, evidence.Status)
	})
}

func computeEvidenceRecordHash(record EvidenceRecord) string {
	return posHashRoot("aetheris-pos-evidence-record-v1", func(w posByteWriter) {
		posWritePart(w, record.EvidenceID)
		posWritePart(w, record.EvidenceType)
		posWritePart(w, record.AccusedValidator)
		posWritePart(w, record.Reporter)
		posWriteUint64(w, record.EpochID)
		posWritePart(w, record.TaskGroupIDOptional)
		posWritePart(w, record.ObjectHash)
		posWritePart(w, record.ProofPayloadHash)
		posWritePart(w, fmt.Sprintf("%d", record.SubmittedHeight))
		posWritePart(w, record.Status)
		posWritePart(w, record.VerificationGroupID)
		posWritePart(w, fmt.Sprintf("%d", record.DecisionHeight))
		posWritePart(w, record.PenaltyIDOptional)
	})
}

func computeEvidenceVerificationAssignmentSeed(epochSeed string, evidenceID string) string {
	return posHashRoot("aetheris-pos-evidence-verification-seed-v1", func(w posByteWriter) {
		posWritePart(w, epochSeed)
		posWritePart(w, evidenceID)
	})
}

func computeEvidenceVerifierSelectionHash(epochSeed string, evidenceID string, validatorID string) string {
	return posHashRoot("aetheris-pos-evidence-verifier-rank-v1", func(w posByteWriter) {
		posWritePart(w, epochSeed)
		posWritePart(w, evidenceID)
		posWritePart(w, validatorID)
	})
}

func computeEvidenceVerificationGroupID(group EvidenceVerificationGroup) string {
	return posHashRoot("aetheris-pos-evidence-verification-group-id-v1", func(w posByteWriter) {
		posWritePart(w, group.EvidenceID)
		posWriteUint64(w, group.EpochID)
		posWritePart(w, group.AssignmentSeed)
		posWriteUint64(w, uint64(group.MinimumGroupSize))
		posWriteUint64(w, uint64(group.DecisionThresholdBps))
	})
}

func computeEvidenceVerificationGroupHash(group EvidenceVerificationGroup) string {
	return posHashRoot("aetheris-pos-evidence-verification-group-v1", func(w posByteWriter) {
		posWritePart(w, group.EvidenceID)
		posWriteUint64(w, group.EpochID)
		posWritePart(w, group.VerificationGroupID)
		posWriteUint64(w, uint64(group.MinimumGroupSize))
		posWriteUint64(w, uint64(group.DecisionThresholdBps))
		posWritePart(w, group.AssignmentSeed)
		posWriteUint64(w, uint64(len(group.Members)))
		for _, member := range group.Members {
			posWritePart(w, member)
		}
		posWriteUint64(w, uint64(len(group.ExcludedValidators)))
		for _, excluded := range group.ExcludedValidators {
			posWritePart(w, excluded)
		}
	})
}

func computeEvidenceVerificationRoot(evidenceID string, votes []EvidenceVerificationVote) string {
	ordered := make([]EvidenceVerificationVote, len(votes))
	copy(ordered, votes)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].ReviewerID != ordered[j].ReviewerID {
			return ordered[i].ReviewerID < ordered[j].ReviewerID
		}
		return ordered[i].VoteHeight < ordered[j].VoteHeight
	})
	return posHashRoot("aetheris-pos-evidence-verification-root-v1", func(w posByteWriter) {
		posWritePart(w, evidenceID)
		posWriteUint64(w, uint64(len(ordered)))
		for _, vote := range ordered {
			posWritePart(w, vote.ReviewerID)
			posWritePart(w, fmt.Sprintf("%t", vote.Accepted))
			posWritePart(w, vote.SignatureHash)
			posWritePart(w, fmt.Sprintf("%d", vote.VoteHeight))
		}
	})
}

func computeEvidenceFinalityVoteRoot(evidenceID string, votes []EvidenceFinalityVote) string {
	ordered := make([]EvidenceFinalityVote, len(votes))
	copy(ordered, votes)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].ValidatorID != ordered[j].ValidatorID {
			return ordered[i].ValidatorID < ordered[j].ValidatorID
		}
		return ordered[i].FinalityHeight < ordered[j].FinalityHeight
	})
	return posHashRoot("aetheris-pos-evidence-finality-root-v1", func(w posByteWriter) {
		posWritePart(w, evidenceID)
		posWriteUint64(w, uint64(len(ordered)))
		for _, vote := range ordered {
			posWritePart(w, vote.ValidatorID)
			posWritePart(w, fmt.Sprintf("%t", vote.Approve))
			posWriteUint64(w, uint64(vote.VotingPowerBps))
			posWritePart(w, vote.SignatureHash)
			posWritePart(w, fmt.Sprintf("%d", vote.FinalityHeight))
		}
	})
}

func isEvidenceStatus(status string) bool {
	switch status {
	case EvidenceStatusSubmitted,
		EvidenceStatusInVerification,
		EvidenceStatusAccepted,
		EvidenceStatusVerified,
		EvidenceStatusRejected,
		EvidenceStatusExpired,
		EvidenceStatusFinalized,
		EvidenceStatusSlashed:
		return true
	default:
		return false
	}
}

func isEvidenceRecordStatus(status string) bool {
	switch status {
	case EvidenceStatusSubmitted, EvidenceStatusInVerification, EvidenceStatusAccepted, EvidenceStatusRejected, EvidenceStatusExpired, EvidenceStatusSlashed:
		return true
	default:
		return false
	}
}

func isAllowedEvidenceRecordTransition(current string, next string) bool {
	if current == next {
		return true
	}
	switch current {
	case EvidenceStatusSubmitted:
		return next == EvidenceStatusInVerification || next == EvidenceStatusExpired
	case EvidenceStatusInVerification:
		return next == EvidenceStatusAccepted || next == EvidenceStatusRejected || next == EvidenceStatusExpired
	case EvidenceStatusAccepted:
		return next == EvidenceStatusSlashed
	default:
		return false
	}
}

func evidenceVerificationExclusions(evidence EvidenceRecord, validators []ScoredValidator) []string {
	validatorIDs := make(map[string]struct{}, len(validators))
	for _, validator := range validators {
		validatorIDs[validator.ValidatorID] = struct{}{}
	}
	excluded := make([]string, 0, 2)
	if _, found := validatorIDs[evidence.AccusedValidator]; found {
		excluded = append(excluded, evidence.AccusedValidator)
	}
	if _, found := validatorIDs[evidence.Reporter]; found && evidence.Reporter != evidence.AccusedValidator {
		excluded = append(excluded, evidence.Reporter)
	}
	sort.Strings(excluded)
	return excluded
}

func validateEvidenceReviewers(reviewers []string) (map[string]struct{}, error) {
	if len(reviewers) == 0 {
		return nil, errors.New("evidence verification reviewers are required")
	}
	out := make(map[string]struct{}, len(reviewers))
	for _, reviewerID := range reviewers {
		if err := validatePosToken("evidence verification reviewer id", reviewerID); err != nil {
			return nil, err
		}
		if _, found := out[reviewerID]; found {
			return nil, fmt.Errorf("duplicate evidence verification reviewer %q", reviewerID)
		}
		out[reviewerID] = struct{}{}
	}
	return out, nil
}
