package types

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

type EpochPhase string

type SettlementStatus string

const (
	SettlementStatusPending   SettlementStatus = "pending"
	SettlementStatusFinalized SettlementStatus = "finalized"
)

type EpochPhaseDurations struct {
	DelegationSeconds       uint64
	ElectionSeconds         uint64
	AssignmentSeconds       uint64
	ActiveValidationSeconds uint64
	SettlementSeconds       uint64
}

type EpochSeedSource string

const (
	EpochSeedSourcePreviousSeedValidatorSet EpochSeedSource = "previous_seed_validator_set"
	EpochSeedSourceCometBFTBlockID          EpochSeedSource = "cometbft_block_id"
	EpochSeedSourceExternalBeacon           EpochSeedSource = "external_beacon"
)

type EpochLifecycleStep struct {
	Phase       EpochPhase
	Name        string
	DurationKey string
}

type EpochRecord struct {
	EpochID          uint64
	StartHeight      uint64
	EndHeight        uint64
	Phase            EpochPhase
	Seed             string
	ValidatorSetHash string
	TaskGroupRoot    string
	PerformanceRoot  string
	RewardRoot       string
	SlashRoot        string
	SettlementStatus SettlementStatus
}

type EpochSettlementRoots struct {
	PerformanceRoot string
	RewardRoot      string
	SlashRoot       string
}

func DefaultEpochLifecycle() []EpochLifecycleStep {
	return []EpochLifecycleStep{
		{Phase: EpochPhaseDelegation, Name: "delegation phase", DurationKey: "delegation_phase_duration"},
		{Phase: EpochPhaseElection, Name: "validator election", DurationKey: "election_phase_duration"},
		{Phase: EpochPhaseAssignment, Name: "task group assignment", DurationKey: "assignment_phase_duration"},
		{Phase: EpochPhaseActive, Name: "active validation", DurationKey: "active_validation_duration"},
		{Phase: EpochPhaseSettlement, Name: "settlement + reward + slash finality", DurationKey: "settlement_phase_duration"},
	}
}

func ValidateEpochLifecycle(lifecycle []EpochLifecycleStep) error {
	expected := DefaultEpochLifecycle()
	if len(lifecycle) != len(expected) {
		return errors.New("epoch lifecycle must define every active phase")
	}
	seen := make(map[EpochPhase]struct{}, len(lifecycle))
	for i, step := range lifecycle {
		if step.Phase != expected[i].Phase {
			return fmt.Errorf("epoch lifecycle step %d must be %s", i, expected[i].Phase)
		}
		if _, found := seen[step.Phase]; found {
			return fmt.Errorf("duplicate epoch lifecycle phase %s", step.Phase)
		}
		seen[step.Phase] = struct{}{}
		if strings.TrimSpace(step.Name) != step.Name || step.Name == "" {
			return fmt.Errorf("epoch lifecycle phase %s name is required", step.Phase)
		}
		if strings.TrimSpace(step.DurationKey) != step.DurationKey || step.DurationKey == "" {
			return fmt.Errorf("epoch lifecycle phase %s duration key is required", step.Phase)
		}
	}
	return nil
}

func NextEpochPhase(phase EpochPhase) (EpochPhase, bool, error) {
	switch phase {
	case EpochPhaseDelegation:
		return EpochPhaseElection, false, nil
	case EpochPhaseElection:
		return EpochPhaseAssignment, false, nil
	case EpochPhaseAssignment:
		return EpochPhaseActive, false, nil
	case EpochPhaseActive:
		return EpochPhaseSettlement, false, nil
	case EpochPhaseSettlement:
		return EpochPhaseClosed, true, nil
	case EpochPhaseClosed:
		return EpochPhaseClosed, true, nil
	default:
		return "", false, fmt.Errorf("unsupported epoch phase %q", phase)
	}
}

func ValidateEpochPhaseTransition(from EpochPhase, to EpochPhase) error {
	next, _, err := NextEpochPhase(from)
	if err != nil {
		return err
	}
	if next != to {
		return fmt.Errorf("invalid epoch phase transition from %s to %s", from, to)
	}
	return nil
}

func (s EpochSeedSource) Validate() error {
	switch s {
	case EpochSeedSourcePreviousSeedValidatorSet, EpochSeedSourceCometBFTBlockID, EpochSeedSourceExternalBeacon:
		return nil
	default:
		return fmt.Errorf("unsupported epoch seed source %q", s)
	}
}

func (p Params) EffectiveEpochSeedSource() EpochSeedSource {
	if p.EpochSeedSource == "" {
		return EpochSeedSourcePreviousSeedValidatorSet
	}
	return p.EpochSeedSource
}

func MaxValidatorSetChanges(params Params, activeValidatorCount uint32) (uint32, error) {
	if err := params.Validate(); err != nil {
		return 0, err
	}
	if activeValidatorCount == 0 {
		return 0, errors.New("active validator count must be positive")
	}
	changes := (uint64(activeValidatorCount)*uint64(params.MaxValidatorSetChangeRateBps) + uint64(BasisPoints) - 1) / uint64(BasisPoints)
	if changes == 0 {
		return 1, nil
	}
	if changes > uint64(activeValidatorCount) {
		return activeValidatorCount, nil
	}
	return uint32(changes), nil
}

func EpochRecordFieldNames() []string {
	return []string{
		"epoch_id",
		"start_height",
		"end_height",
		"phase",
		"seed",
		"validator_set_hash",
		"task_group_root",
		"performance_root",
		"reward_root",
		"slash_root",
		"settlement_status",
	}
}

func EpochPhaseValues() []EpochPhase {
	return []EpochPhase{
		EpochPhaseDelegation,
		EpochPhaseElection,
		EpochPhaseAssignment,
		EpochPhaseActive,
		EpochPhaseSettlement,
		EpochPhaseClosed,
	}
}

func ValidateValidatorSetChangeActivation(epoch EpochRecord, activationHeight uint64) error {
	if err := epoch.Validate(); err != nil {
		return err
	}
	if activationHeight != epoch.StartHeight {
		return fmt.Errorf("validator set changes must activate at epoch boundary height %d", epoch.StartHeight)
	}
	return nil
}

func ValidateConsecutiveEpochs(previous EpochRecord, next EpochRecord) error {
	if err := previous.Validate(); err != nil {
		return err
	}
	if err := next.Validate(); err != nil {
		return err
	}
	if next.EpochID != previous.EpochID+1 {
		return errors.New("next epoch id must increment by one")
	}
	if next.StartHeight != previous.EndHeight+1 {
		return errors.New("next epoch must start at previous end height plus one")
	}
	return nil
}

func DefaultEpochPhaseDurations(epochDurationSeconds uint64) EpochPhaseDurations {
	delegation := epochDurationSeconds / 4
	election := epochDurationSeconds / 12
	assignment := epochDurationSeconds / 12
	settlement := epochDurationSeconds / 12
	active := epochDurationSeconds - delegation - election - assignment - settlement
	return EpochPhaseDurations{
		DelegationSeconds:       delegation,
		ElectionSeconds:         election,
		AssignmentSeconds:       assignment,
		ActiveValidationSeconds: active,
		SettlementSeconds:       settlement,
	}
}

func (p Params) EffectivePhaseDurations() EpochPhaseDurations {
	baseDefault := DefaultEpochPhaseDurations(DefaultEpochDurationSeconds)
	if p.PhaseDurations.IsZero() ||
		(p.EpochDurationSeconds != DefaultEpochDurationSeconds && p.PhaseDurations == baseDefault) {
		return DefaultEpochPhaseDurations(p.EpochDurationSeconds)
	}
	return p.PhaseDurations
}

func (d EpochPhaseDurations) IsZero() bool {
	return d.DelegationSeconds == 0 &&
		d.ElectionSeconds == 0 &&
		d.AssignmentSeconds == 0 &&
		d.ActiveValidationSeconds == 0 &&
		d.SettlementSeconds == 0
}

func (d EpochPhaseDurations) TotalSeconds() uint64 {
	return d.DelegationSeconds +
		d.ElectionSeconds +
		d.AssignmentSeconds +
		d.ActiveValidationSeconds +
		d.SettlementSeconds
}

func (d EpochPhaseDurations) Validate(epochDurationSeconds uint64) error {
	if d.DelegationSeconds == 0 {
		return errors.New("delegation phase duration must be positive")
	}
	if d.ElectionSeconds == 0 {
		return errors.New("election phase duration must be positive")
	}
	if d.AssignmentSeconds == 0 {
		return errors.New("assignment phase duration must be positive")
	}
	if d.ActiveValidationSeconds == 0 {
		return errors.New("active validation phase duration must be positive")
	}
	if d.SettlementSeconds == 0 {
		return errors.New("settlement phase duration must be positive")
	}
	if d.TotalSeconds() != epochDurationSeconds {
		return fmt.Errorf("epoch phase durations must sum to %d seconds", epochDurationSeconds)
	}
	return nil
}

func EpochPhaseAt(params Params, epochStartUnixSeconds uint64, nowUnixSeconds uint64) (EpochPhase, error) {
	if err := params.Validate(); err != nil {
		return "", err
	}
	if nowUnixSeconds < epochStartUnixSeconds {
		return "", errors.New("epoch phase time cannot be before epoch start")
	}
	elapsed := nowUnixSeconds - epochStartUnixSeconds
	if elapsed >= params.EpochDurationSeconds {
		return EpochPhaseClosed, nil
	}
	durations := params.EffectivePhaseDurations()
	if elapsed < durations.DelegationSeconds {
		return EpochPhaseDelegation, nil
	}
	elapsed -= durations.DelegationSeconds
	if elapsed < durations.ElectionSeconds {
		return EpochPhaseElection, nil
	}
	elapsed -= durations.ElectionSeconds
	if elapsed < durations.AssignmentSeconds {
		return EpochPhaseAssignment, nil
	}
	elapsed -= durations.AssignmentSeconds
	if elapsed < durations.ActiveValidationSeconds {
		return EpochPhaseActive, nil
	}
	return EpochPhaseSettlement, nil
}

func NewEpochRecord(params Params, epochID uint64, startHeight uint64, endHeight uint64, phase EpochPhase, previousSeed string, validators []ScoredValidator) (EpochRecord, error) {
	if err := params.Validate(); err != nil {
		return EpochRecord{}, err
	}
	if startHeight == 0 || endHeight < startHeight {
		return EpochRecord{}, errors.New("epoch heights must be positive and ordered")
	}
	if err := validateEpochPhase(phase); err != nil {
		return EpochRecord{}, err
	}
	validatorSetHash, err := ComputeValidatorSetHash(validators)
	if err != nil {
		return EpochRecord{}, err
	}
	seed, err := DeriveEpochSeedWithSource(params.EffectiveEpochSeedSource(), epochID, startHeight, previousSeed, validatorSetHash)
	if err != nil {
		return EpochRecord{}, err
	}
	record := EpochRecord{
		EpochID:          epochID,
		StartHeight:      startHeight,
		EndHeight:        endHeight,
		Phase:            phase,
		Seed:             seed,
		ValidatorSetHash: validatorSetHash,
		TaskGroupRoot:    PosEmptyRootHash,
		PerformanceRoot:  PosEmptyRootHash,
		RewardRoot:       PosEmptyRootHash,
		SlashRoot:        PosEmptyRootHash,
		SettlementStatus: SettlementStatusPending,
	}
	if err := record.Validate(); err != nil {
		return EpochRecord{}, err
	}
	return record, nil
}

func CloseEpochRecord(record EpochRecord, performanceRoot string, rewardRoot string, slashRoot string) (EpochRecord, error) {
	if err := record.Validate(); err != nil {
		return EpochRecord{}, err
	}
	if record.Phase != EpochPhaseSettlement {
		return EpochRecord{}, errors.New("epoch must be in settlement phase before closing")
	}
	if err := validatePosHash("performance root", performanceRoot); err != nil {
		return EpochRecord{}, err
	}
	if err := validatePosHash("reward root", rewardRoot); err != nil {
		return EpochRecord{}, err
	}
	if err := validatePosHash("slash root", slashRoot); err != nil {
		return EpochRecord{}, err
	}
	record.Phase = EpochPhaseClosed
	record.PerformanceRoot = performanceRoot
	record.RewardRoot = rewardRoot
	record.SlashRoot = slashRoot
	record.SettlementStatus = SettlementStatusFinalized
	return record, record.Validate()
}

func (r EpochRecord) Validate() error {
	if r.StartHeight == 0 || r.EndHeight < r.StartHeight {
		return errors.New("epoch heights must be positive and ordered")
	}
	if err := validateEpochPhase(r.Phase); err != nil {
		return err
	}
	if err := validatePosHash("epoch seed", r.Seed); err != nil {
		return err
	}
	if err := validatePosHash("validator set hash", r.ValidatorSetHash); err != nil {
		return err
	}
	if err := validatePosHash("task group root", r.TaskGroupRoot); err != nil {
		return err
	}
	if err := validatePosHash("performance root", r.PerformanceRoot); err != nil {
		return err
	}
	if err := validatePosHash("reward root", r.RewardRoot); err != nil {
		return err
	}
	if err := validatePosHash("slash root", r.SlashRoot); err != nil {
		return err
	}
	switch r.SettlementStatus {
	case SettlementStatusPending, SettlementStatusFinalized:
	default:
		return errors.New("unsupported settlement status")
	}
	if r.Phase == EpochPhaseClosed && r.SettlementStatus != SettlementStatusFinalized {
		return errors.New("closed epoch must have finalized settlement")
	}
	if r.SettlementStatus == SettlementStatusFinalized && r.Phase != EpochPhaseClosed {
		return errors.New("finalized settlement must close the epoch")
	}
	return nil
}

func ComputeValidatorSetHash(validators []ScoredValidator) (string, error) {
	ordered := cloneScoredValidators(validators)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].ValidatorID < ordered[j].ValidatorID
	})
	seen := make(map[string]struct{}, len(ordered))
	for _, validator := range ordered {
		if err := validatePosToken("validator id", validator.ValidatorID); err != nil {
			return "", err
		}
		if validator.VotingPowerNaet.IsNegative() {
			return "", errors.New("validator voting power cannot be negative")
		}
		if validator.Score.IsNegative() {
			return "", errors.New("validator score cannot be negative")
		}
		if _, found := seen[validator.ValidatorID]; found {
			return "", fmt.Errorf("duplicate validator %q", validator.ValidatorID)
		}
		seen[validator.ValidatorID] = struct{}{}
		if err := validateValidatorRoles(validator.Roles); err != nil {
			return "", err
		}
	}
	return posHashRoot("aetheris-pos-validator-set-v1", func(w posByteWriter) {
		posWriteUint64(w, uint64(len(ordered)))
		for _, validator := range ordered {
			posWritePart(w, validator.ValidatorID)
			posWritePart(w, validator.VotingPowerNaet.String())
			posWritePart(w, validator.Score.String())
			for _, role := range normalizedRoles(validator.Roles, AllValidatorRoles()) {
				posWritePart(w, string(role))
			}
		}
	}), nil
}

func DeriveEpochSeed(epochID uint64, startHeight uint64, previousSeed string, validatorSetHash string) (string, error) {
	return DeriveEpochSeedWithSource(EpochSeedSourcePreviousSeedValidatorSet, epochID, startHeight, previousSeed, validatorSetHash)
}

func DeriveEpochSeedWithSource(source EpochSeedSource, epochID uint64, startHeight uint64, previousSeed string, validatorSetHash string) (string, error) {
	if err := source.Validate(); err != nil {
		return "", err
	}
	if startHeight == 0 {
		return "", errors.New("epoch seed start height must be positive")
	}
	if previousSeed == "" {
		previousSeed = PosEmptyRootHash
	}
	if err := validatePosHash("previous epoch seed", previousSeed); err != nil {
		return "", err
	}
	if err := validatePosHash("validator set hash", validatorSetHash); err != nil {
		return "", err
	}
	return posHashRoot("aetheris-pos-epoch-seed-v1", func(w posByteWriter) {
		posWritePart(w, string(source))
		posWriteUint64(w, epochID)
		posWriteUint64(w, startHeight)
		posWritePart(w, previousSeed)
		posWritePart(w, validatorSetHash)
	}), nil
}

func validateEpochPhase(phase EpochPhase) error {
	switch phase {
	case EpochPhaseDelegation, EpochPhaseElection, EpochPhaseAssignment, EpochPhaseActive, EpochPhaseSettlement, EpochPhaseClosed:
		return nil
	default:
		return fmt.Errorf("unsupported epoch phase %q", phase)
	}
}
