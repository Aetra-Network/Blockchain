package types

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	sdkmath "cosmossdk.io/math"
)

const (
	ValidatorRoleValidator          ValidatorRole = "validator"
	ValidatorRoleProposer           ValidatorRole = "proposer"
	ValidatorRoleBlockProducer      ValidatorRole = "block_producer"
	ValidatorRoleVerifier           ValidatorRole = "verifier"
	ValidatorRoleEvidenceReporter   ValidatorRole = "evidence_reporter"
	ValidatorRoleCollator           ValidatorRole = "collator"
	ValidatorRoleDelegationOperator ValidatorRole = "delegation_operator"
	ValidatorRoleFisherman          ValidatorRole = "fisherman"
	ValidatorRoleEvidenceReviewer   ValidatorRole = "evidence_reviewer"
)

const (
	CollatorStatusRegistered = "registered"
	CollatorStatusActive     = "active"
	CollatorStatusSuspended  = "suspended"
	CollatorStatusRetired    = "retired"
)

type CollatorRecord struct {
	CollatorID         string
	OperatorAddress    string
	SupportedWorkloads []WorkloadType
	BondOptional       sdkmath.Int
	Reputation         uint32
	Status             string
	RegisteredEpoch    uint64
}

type CollatorCandidateOutputInput struct {
	EpochID             uint64
	Collator            CollatorRecord
	Task                WorkloadTask
	TaskGroupIDOptional string
	TransactionRoot     string
	StateTransitionRoot string
	ProofBundleRoot     string
}

type CollatorCandidateOutput struct {
	EpochID                       uint64
	CollatorID                    string
	OperatorAddress               string
	TaskID                        string
	TaskGroupIDOptional           string
	WorkloadID                    string
	WorkloadType                  WorkloadType
	TransactionRoot               string
	StateTransitionRoot           string
	ProofBundleRoot               string
	RequiresValidatorVerification bool
	ValidatorSignatures           []string
	Finalized                     bool
	CandidateOutputHash           string
}

type CollatorRegistry struct {
	EpochID      uint64
	Collators    []CollatorRecord
	RegistryRoot string
}

const (
	CollatorVerificationResultValid   = "valid"
	CollatorVerificationResultInvalid = "invalid"
	CollatorVerificationResultAbstain = "abstain"
)

type CollatorOutputVerification struct {
	OutputHash       string
	ValidatorAddress string
	Result           string
	SignatureHash    string
	VerifiedHeight   int64
}

type CollatorOutputVerificationResult struct {
	OutputHash           string
	ValidVotes           uint32
	InvalidVotes         uint32
	AbstainVotes         uint32
	TotalValidators      uint32
	ParticipationBps     uint32
	DecisionThresholdBps uint32
	Accepted             bool
	Rejected             bool
	ValidSignatureHashes []string
	VerificationRoot     string
}

const (
	EvidenceTypeDoubleSignProof              = "double_sign_proof"
	EvidenceTypeInvalidStateTransitionProof  = "invalid_state_transition_proof"
	EvidenceTypeEquivocationProof            = "equivocation_proof"
	EvidenceTypeDowntimeProof                = "downtime_proof"
	EvidenceTypeInvalidTaskExecutionProof    = "invalid_task_execution_proof"
	EvidenceTypeInvalidCollatorOutputProof   = "invalid_collator_output_proof"
	EvidenceTypeInvalidProofAcceptance       = "invalid_proof_acceptance"
	EvidenceTypeFalseCapacityDeclaration     = "false_capacity_declaration"
	EvidenceTypeInvalidEvidenceSubmission    = "invalid_evidence_submission"
	EvidenceStatusSubmitted                  = "submitted"
	EvidenceStatusInVerification             = "in_verification"
	EvidenceStatusAccepted                   = "accepted"
	EvidenceStatusVerified                   = "verified"
	EvidenceStatusRejected                   = "rejected"
	EvidenceStatusExpired                    = "expired"
	EvidenceStatusFinalized                  = "finalized"
	EvidenceStatusSlashed                    = "slashed"
	DefaultEvidenceVerificationQuorumBps     = uint32(6_700)
	DefaultEvidenceFinalityQuorumBps         = uint32(6_700)
	DefaultDoubleSignSlashBps                = uint32(5_000)
	DefaultInvalidStateTransitionSlashBps    = uint32(1_500)
	DefaultEquivocationSlashBps              = uint32(2_000)
	DefaultDowntimeSlashBps                  = uint32(100)
	DefaultInvalidTaskExecutionSlashBps      = uint32(750)
	DefaultInvalidCollatorOutputSlashBps     = uint32(500)
	DefaultInvalidProofAcceptanceSlashBps    = uint32(1_000)
	DefaultFalseCapacityDeclarationSlashBps  = uint32(500)
	DefaultInvalidEvidenceSubmissionSlashBps = uint32(250)
)

func CollatorRecordFieldNames() []string {
	return []string{
		"collator_id",
		"operator_address",
		"supported_workloads",
		"bond_optional",
		"reputation",
		"status",
		"registered_epoch",
	}
}

func CollatorStatusValues() []string {
	return []string{
		CollatorStatusRegistered,
		CollatorStatusActive,
		CollatorStatusSuspended,
		CollatorStatusRetired,
	}
}

func NewCollatorRecord(record CollatorRecord) (CollatorRecord, error) {
	record.CollatorID = strings.TrimSpace(record.CollatorID)
	record.OperatorAddress = strings.TrimSpace(record.OperatorAddress)
	record.Status = strings.TrimSpace(record.Status)
	if record.Status == "" {
		record.Status = CollatorStatusRegistered
	}
	if record.BondOptional.IsNil() {
		record.BondOptional = sdkmath.ZeroInt()
	}
	record.SupportedWorkloads = normalizedWorkloadTypes(record.SupportedWorkloads)
	return record, record.Validate()
}

func (c CollatorRecord) Validate() error {
	if err := validatePosToken("collator id", c.CollatorID); err != nil {
		return err
	}
	if err := validatePosToken("collator operator address", c.OperatorAddress); err != nil {
		return err
	}
	if len(c.SupportedWorkloads) == 0 {
		return errors.New("collator supported workloads are required")
	}
	if err := validateWorkloadTypes(c.SupportedWorkloads); err != nil {
		return err
	}
	if c.BondOptional.IsNil() {
		return errors.New("collator bond optional must be set")
	}
	if c.BondOptional.IsNegative() {
		return errors.New("collator bond optional cannot be negative")
	}
	if c.Reputation > BasisPoints {
		return fmt.Errorf("collator reputation must be <= %d bps", BasisPoints)
	}
	if err := validateCollatorStatus(c.Status); err != nil {
		return err
	}
	if c.RegisteredEpoch == 0 {
		return errors.New("collator registered epoch is required")
	}
	return nil
}

func (c CollatorRecord) SupportsWorkload(workloadType WorkloadType) bool {
	if err := validateWorkloadType(workloadType); err != nil {
		return false
	}
	for _, supported := range c.SupportedWorkloads {
		if supported == workloadType {
			return true
		}
	}
	return false
}

func BuildCollatorCandidateOutput(params Params, input CollatorCandidateOutputInput) (CollatorCandidateOutput, error) {
	if err := params.Validate(); err != nil {
		return CollatorCandidateOutput{}, err
	}
	collator, err := NewCollatorRecord(input.Collator)
	if err != nil {
		return CollatorCandidateOutput{}, err
	}
	if collator.Status == CollatorStatusSuspended || collator.Status == CollatorStatusRetired {
		return CollatorCandidateOutput{}, errors.New("collator is not eligible to build candidate outputs")
	}
	task := normalizeWorkloadTask(params, input.Task)
	if err := task.Validate(params); err != nil {
		return CollatorCandidateOutput{}, err
	}
	if !collator.SupportsWorkload(task.WorkloadType) {
		return CollatorCandidateOutput{}, fmt.Errorf("collator does not support workload %q", task.WorkloadType)
	}
	if input.EpochID == 0 {
		return CollatorCandidateOutput{}, errors.New("collator candidate output epoch id is required")
	}
	output := CollatorCandidateOutput{
		EpochID:                       input.EpochID,
		CollatorID:                    collator.CollatorID,
		OperatorAddress:               collator.OperatorAddress,
		TaskID:                        task.TaskID,
		TaskGroupIDOptional:           strings.TrimSpace(input.TaskGroupIDOptional),
		WorkloadID:                    task.WorkloadID,
		WorkloadType:                  task.WorkloadType,
		TransactionRoot:               input.TransactionRoot,
		StateTransitionRoot:           input.StateTransitionRoot,
		ProofBundleRoot:               input.ProofBundleRoot,
		RequiresValidatorVerification: true,
		ValidatorSignatures:           nil,
		Finalized:                     false,
	}
	output.CandidateOutputHash = ComputeCollatorCandidateOutputHash(output)
	return output, output.Validate()
}

func (o CollatorCandidateOutput) Validate() error {
	if o.EpochID == 0 {
		return errors.New("collator candidate output epoch id is required")
	}
	if err := validatePosToken("collator output collator id", o.CollatorID); err != nil {
		return err
	}
	if err := validatePosToken("collator output operator address", o.OperatorAddress); err != nil {
		return err
	}
	if err := validatePosToken("collator output task id", o.TaskID); err != nil {
		return err
	}
	if o.TaskGroupIDOptional != "" {
		if err := validatePosToken("collator output task group id", o.TaskGroupIDOptional); err != nil {
			return err
		}
	}
	if err := validatePosToken("collator output workload id", o.WorkloadID); err != nil {
		return err
	}
	if err := validateWorkloadType(o.WorkloadType); err != nil {
		return err
	}
	if err := validatePosHash("collator output transaction root", o.TransactionRoot); err != nil {
		return err
	}
	if err := validatePosHash("collator output state transition root", o.StateTransitionRoot); err != nil {
		return err
	}
	if err := validatePosHash("collator output proof bundle root", o.ProofBundleRoot); err != nil {
		return err
	}
	if !o.RequiresValidatorVerification {
		return errors.New("collator output requires validator verification")
	}
	for _, signature := range o.ValidatorSignatures {
		if err := validatePosHash("collator output validator signature", signature); err != nil {
			return err
		}
	}
	if o.Finalized && len(o.ValidatorSignatures) == 0 {
		return errors.New("finalized collator output requires validator signatures")
	}
	if err := validatePosHash("collator output hash", o.CandidateOutputHash); err != nil {
		return err
	}
	if expected := ComputeCollatorCandidateOutputHash(o); expected != o.CandidateOutputHash {
		return errors.New("collator candidate output hash mismatch")
	}
	return nil
}

func ComputeCollatorCandidateOutputHash(output CollatorCandidateOutput) string {
	return posHashRoot("aetheris-pos-collator-output-v1", func(w posByteWriter) {
		posWriteUint64(w, output.EpochID)
		posWritePart(w, output.CollatorID)
		posWritePart(w, output.OperatorAddress)
		posWritePart(w, output.TaskID)
		posWritePart(w, output.TaskGroupIDOptional)
		posWritePart(w, output.WorkloadID)
		posWritePart(w, string(output.WorkloadType))
		posWritePart(w, output.TransactionRoot)
		posWritePart(w, output.StateTransitionRoot)
		posWritePart(w, output.ProofBundleRoot)
		posWriteUint64(w, boolAsUint64(output.RequiresValidatorVerification))
	})
}

func NewCollatorRegistry(epochID uint64, collators []CollatorRecord) (CollatorRegistry, error) {
	records := make([]CollatorRecord, len(collators))
	for i, collator := range collators {
		normalized, err := NewCollatorRecord(collator)
		if err != nil {
			return CollatorRegistry{}, err
		}
		records[i] = normalized
	}
	sort.SliceStable(records, func(i, j int) bool {
		return records[i].CollatorID < records[j].CollatorID
	})
	registry := CollatorRegistry{EpochID: epochID, Collators: records}
	if len(records) == 0 {
		registry.RegistryRoot = PosEmptyRootHash
	} else {
		registry.RegistryRoot = ComputeCollatorRegistryRoot(registry)
	}
	return registry, registry.Validate()
}

func (r CollatorRegistry) Validate() error {
	if r.EpochID == 0 {
		return errors.New("collator registry epoch id is required")
	}
	seen := make(map[string]struct{}, len(r.Collators))
	for _, collator := range r.Collators {
		if err := collator.Validate(); err != nil {
			return err
		}
		if collator.RegisteredEpoch > r.EpochID {
			return errors.New("collator registered epoch cannot exceed registry epoch")
		}
		if _, found := seen[collator.CollatorID]; found {
			return fmt.Errorf("duplicate collator id %q", collator.CollatorID)
		}
		seen[collator.CollatorID] = struct{}{}
	}
	expectedRoot := PosEmptyRootHash
	if len(r.Collators) > 0 {
		expectedRoot = ComputeCollatorRegistryRoot(r)
	}
	if r.RegistryRoot != expectedRoot {
		return errors.New("collator registry root mismatch")
	}
	return nil
}

func (r CollatorRegistry) CollatorByID(collatorID string) (CollatorRecord, bool, error) {
	if err := validatePosToken("collator id", collatorID); err != nil {
		return CollatorRecord{}, false, err
	}
	if err := r.Validate(); err != nil {
		return CollatorRecord{}, false, err
	}
	for _, collator := range r.Collators {
		if collator.CollatorID == collatorID {
			return collator, true, nil
		}
	}
	return CollatorRecord{}, false, nil
}

func (r CollatorRegistry) ActiveCollatorsForWorkload(workloadType WorkloadType) ([]CollatorRecord, error) {
	if err := validateWorkloadType(workloadType); err != nil {
		return nil, err
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	out := make([]CollatorRecord, 0, len(r.Collators))
	for _, collator := range r.Collators {
		if collator.Status == CollatorStatusActive && collator.SupportsWorkload(workloadType) {
			out = append(out, collator)
		}
	}
	return out, nil
}

func ComputeCollatorRegistryRoot(registry CollatorRegistry) string {
	return posHashRoot("aetheris-pos-collator-registry-v1", func(w posByteWriter) {
		posWriteUint64(w, registry.EpochID)
		posWriteUint64(w, uint64(len(registry.Collators)))
		for _, collator := range registry.Collators {
			posWritePart(w, collator.CollatorID)
			posWritePart(w, collator.OperatorAddress)
			posWriteUint64(w, uint64(len(collator.SupportedWorkloads)))
			for _, workloadType := range collator.SupportedWorkloads {
				posWritePart(w, string(workloadType))
			}
			posWritePart(w, collator.BondOptional.String())
			posWriteUint64(w, uint64(collator.Reputation))
			posWritePart(w, collator.Status)
			posWriteUint64(w, collator.RegisteredEpoch)
		}
	})
}

func NewCollatorOutputVerification(output CollatorCandidateOutput, validatorAddress string, result string, signatureHash string, verifiedHeight int64) (CollatorOutputVerification, error) {
	if err := output.Validate(); err != nil {
		return CollatorOutputVerification{}, err
	}
	verification := CollatorOutputVerification{
		OutputHash:       output.CandidateOutputHash,
		ValidatorAddress: strings.TrimSpace(validatorAddress),
		Result:           strings.TrimSpace(result),
		SignatureHash:    strings.TrimSpace(signatureHash),
		VerifiedHeight:   verifiedHeight,
	}
	return verification, verification.Validate()
}

func (v CollatorOutputVerification) Validate() error {
	if err := validatePosHash("collator output verification hash", v.OutputHash); err != nil {
		return err
	}
	if err := validatePosToken("collator output verifier", v.ValidatorAddress); err != nil {
		return err
	}
	if err := validateCollatorVerificationResult(v.Result); err != nil {
		return err
	}
	if err := validatePosHash("collator output verification signature", v.SignatureHash); err != nil {
		return err
	}
	if v.VerifiedHeight < 0 {
		return errors.New("collator output verification height cannot be negative")
	}
	return nil
}

func VerifyCollatorOutputByValidators(output CollatorCandidateOutput, validatorSet []string, votes []CollatorOutputVerification, decisionThresholdBps uint32) (CollatorOutputVerificationResult, error) {
	if err := output.Validate(); err != nil {
		return CollatorOutputVerificationResult{}, err
	}
	if decisionThresholdBps == 0 {
		decisionThresholdBps = DefaultEvidenceVerificationQuorumBps
	}
	if decisionThresholdBps > BasisPoints {
		return CollatorOutputVerificationResult{}, fmt.Errorf("collator verification threshold must be <= %d bps", BasisPoints)
	}
	allowed, err := validatorSetMap("collator verification validator", validatorSet)
	if err != nil {
		return CollatorOutputVerificationResult{}, err
	}
	if len(validatorSet) == 0 {
		return CollatorOutputVerificationResult{}, errors.New("collator verification validator set is required")
	}
	seen := make(map[string]struct{}, len(votes))
	result := CollatorOutputVerificationResult{
		OutputHash:           output.CandidateOutputHash,
		TotalValidators:      uint32(len(validatorSet)),
		DecisionThresholdBps: decisionThresholdBps,
	}
	for _, vote := range votes {
		if err := vote.Validate(); err != nil {
			return CollatorOutputVerificationResult{}, err
		}
		if vote.OutputHash != output.CandidateOutputHash {
			return CollatorOutputVerificationResult{}, errors.New("collator output verification hash mismatch")
		}
		if _, ok := allowed[vote.ValidatorAddress]; !ok {
			return CollatorOutputVerificationResult{}, errors.New("collator output verifier is not in validator set")
		}
		if _, found := seen[vote.ValidatorAddress]; found {
			return CollatorOutputVerificationResult{}, fmt.Errorf("duplicate collator output verification by %q", vote.ValidatorAddress)
		}
		seen[vote.ValidatorAddress] = struct{}{}
		switch vote.Result {
		case CollatorVerificationResultValid:
			result.ValidVotes++
			result.ValidSignatureHashes = append(result.ValidSignatureHashes, vote.SignatureHash)
		case CollatorVerificationResultInvalid:
			result.InvalidVotes++
		case CollatorVerificationResultAbstain:
			result.AbstainVotes++
		}
	}
	result.ParticipationBps = ratioBps(uint64(len(seen)), uint64(len(validatorSet)))
	validBps := ratioBps(uint64(result.ValidVotes), uint64(len(validatorSet)))
	invalidBps := ratioBps(uint64(result.InvalidVotes), uint64(len(validatorSet)))
	result.Accepted = validBps >= decisionThresholdBps
	result.Rejected = invalidBps >= decisionThresholdBps
	if result.Accepted && result.Rejected {
		return CollatorOutputVerificationResult{}, errors.New("collator output verification cannot be both accepted and rejected")
	}
	result.VerificationRoot = ComputeCollatorOutputVerificationRoot(result)
	return result, nil
}

func FinalizeCollatorOutputAfterVerification(output CollatorCandidateOutput, verification CollatorOutputVerificationResult) (CollatorCandidateOutput, error) {
	if err := output.Validate(); err != nil {
		return CollatorCandidateOutput{}, err
	}
	if err := verification.Validate(); err != nil {
		return CollatorCandidateOutput{}, err
	}
	if verification.OutputHash != output.CandidateOutputHash {
		return CollatorCandidateOutput{}, errors.New("collator verification output hash mismatch")
	}
	if !verification.Accepted {
		return CollatorCandidateOutput{}, errors.New("collator output is not accepted by validators")
	}
	out := output
	out.ValidatorSignatures = append([]string(nil), verification.ValidSignatureHashes...)
	out.Finalized = true
	return out, out.Validate()
}

func (r CollatorOutputVerificationResult) Validate() error {
	if err := validatePosHash("collator output verification result hash", r.OutputHash); err != nil {
		return err
	}
	if r.TotalValidators == 0 {
		return errors.New("collator output verification result requires validators")
	}
	if r.ValidVotes+r.InvalidVotes+r.AbstainVotes > r.TotalValidators {
		return errors.New("collator output verification votes exceed validator set")
	}
	if r.ParticipationBps > BasisPoints || r.DecisionThresholdBps > BasisPoints {
		return fmt.Errorf("collator output verification bps must be <= %d", BasisPoints)
	}
	if r.Accepted && r.Rejected {
		return errors.New("collator output verification cannot be both accepted and rejected")
	}
	for _, signature := range r.ValidSignatureHashes {
		if err := validatePosHash("collator output valid signature", signature); err != nil {
			return err
		}
	}
	if err := validatePosHash("collator output verification root", r.VerificationRoot); err != nil {
		return err
	}
	if expected := ComputeCollatorOutputVerificationRoot(r); expected != r.VerificationRoot {
		return errors.New("collator output verification root mismatch")
	}
	return nil
}

func ComputeCollatorOutputVerificationRoot(result CollatorOutputVerificationResult) string {
	return posHashRoot("aetheris-pos-collator-verification-v1", func(w posByteWriter) {
		posWritePart(w, result.OutputHash)
		posWriteUint64(w, uint64(result.ValidVotes))
		posWriteUint64(w, uint64(result.InvalidVotes))
		posWriteUint64(w, uint64(result.AbstainVotes))
		posWriteUint64(w, uint64(result.TotalValidators))
		posWriteUint64(w, uint64(result.ParticipationBps))
		posWriteUint64(w, uint64(result.DecisionThresholdBps))
		posWriteUint64(w, boolAsUint64(result.Accepted))
		posWriteUint64(w, boolAsUint64(result.Rejected))
		posWriteUint64(w, uint64(len(result.ValidSignatureHashes)))
		for _, signature := range result.ValidSignatureHashes {
			posWritePart(w, signature)
		}
	})
}

func BuildInvalidCollatorOutputEvidence(evidenceID string, reporterID string, collator CollatorRecord, output CollatorCandidateOutput, verification CollatorOutputVerificationResult, submittedHeight int64) (StructuredEvidenceRecord, error) {
	if err := collator.Validate(); err != nil {
		return StructuredEvidenceRecord{}, err
	}
	if err := output.Validate(); err != nil {
		return StructuredEvidenceRecord{}, err
	}
	if output.CollatorID != collator.CollatorID {
		return StructuredEvidenceRecord{}, errors.New("invalid collator evidence collator id mismatch")
	}
	if err := verification.Validate(); err != nil {
		return StructuredEvidenceRecord{}, err
	}
	if verification.OutputHash != output.CandidateOutputHash {
		return StructuredEvidenceRecord{}, errors.New("invalid collator evidence output hash mismatch")
	}
	if !verification.Rejected {
		return StructuredEvidenceRecord{}, errors.New("invalid collator evidence requires validator rejection")
	}
	return SubmitStructuredEvidence(StructuredEvidenceRecord{
		EvidenceID:          evidenceID,
		EvidenceType:        EvidenceTypeInvalidCollatorOutputProof,
		ReporterID:          reporterID,
		AccusedValidatorID:  collator.CollatorID,
		SubjectID:           output.CandidateOutputHash,
		EvidenceHash:        ComputeInvalidCollatorOutputEvidenceHash(collator, output, verification),
		EvidenceHeight:      submittedHeight,
		EvidenceEpoch:       output.EpochID,
		SubmittedHeight:     submittedHeight,
		VerificationGroupID: fmt.Sprintf("collator-output/%s", collator.CollatorID),
	})
}

func ComputeInvalidCollatorOutputEvidenceHash(collator CollatorRecord, output CollatorCandidateOutput, verification CollatorOutputVerificationResult) string {
	return posHashRoot("aetheris-pos-invalid-collator-output-evidence-v1", func(w posByteWriter) {
		posWritePart(w, collator.CollatorID)
		posWritePart(w, output.CandidateOutputHash)
		posWritePart(w, verification.VerificationRoot)
		posWritePart(w, collator.BondOptional.String())
	})
}

func ComputeInvalidCollatorOutputPenalty(collator CollatorRecord, evidence StructuredEvidenceRecord) (sdkmath.Int, error) {
	if err := collator.Validate(); err != nil {
		return sdkmath.Int{}, err
	}
	if err := evidence.Validate(); err != nil {
		return sdkmath.Int{}, err
	}
	if evidence.EvidenceType != EvidenceTypeInvalidCollatorOutputProof {
		return sdkmath.Int{}, errors.New("invalid collator output penalty requires invalid collator evidence")
	}
	if evidence.AccusedValidatorID != collator.CollatorID {
		return sdkmath.Int{}, errors.New("invalid collator output evidence accused collator mismatch")
	}
	if evidence.Status != EvidenceStatusAccepted && evidence.Status != EvidenceStatusFinalized && evidence.Status != EvidenceStatusSlashed {
		return sdkmath.Int{}, errors.New("invalid collator output evidence must be accepted before penalty")
	}
	if !collator.BondOptional.IsPositive() {
		return sdkmath.ZeroInt(), nil
	}
	penalty := collator.BondOptional.MulRaw(int64(DefaultInvalidCollatorOutputSlashBps)).QuoRaw(int64(BasisPoints))
	if penalty.GT(collator.BondOptional) {
		return collator.BondOptional, nil
	}
	return penalty, nil
}

func validateCollatorStatus(status string) error {
	switch status {
	case CollatorStatusRegistered, CollatorStatusActive, CollatorStatusSuspended, CollatorStatusRetired:
		return nil
	default:
		return fmt.Errorf("unsupported collator status %q", status)
	}
}

func validateCollatorVerificationResult(result string) error {
	switch result {
	case CollatorVerificationResultValid, CollatorVerificationResultInvalid, CollatorVerificationResultAbstain:
		return nil
	default:
		return fmt.Errorf("unsupported collator verification result %q", result)
	}
}
