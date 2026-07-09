package types

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	sdkmath "cosmossdk.io/math"
)

const (
	EpochPhaseDelegation EpochPhase = "delegation"
	EpochPhaseElection   EpochPhase = "election"
	EpochPhaseAssignment EpochPhase = "assignment"
	EpochPhaseActive     EpochPhase = "active"
	EpochPhaseSettlement EpochPhase = "settlement"
	EpochPhaseClosed     EpochPhase = "closed"
)

type DelegationIntent struct {
	NominatorID            string
	ValidatorID            string
	StakeNaet              sdkmath.Int
	RequestedEpoch         uint64
	MaxCommissionBps       uint32
	MinPerformanceScoreBps uint32
}

type DelegationActivation struct {
	ValidatorID   string
	Nominations   []Nomination
	ActivatedAt   uint64
	IntentCount   uint32
	TotalStake    sdkmath.Int
	ActivationKey string
}

type UnbondingRiskWindow struct {
	UnbondingEpochs       uint64
	SlashableWindowEpochs uint64
	TotalRiskEpochs       uint64
}

type UnbondingRiskRecord struct {
	DelegatorID         string
	ValidatorID         string
	AmountNaet          sdkmath.Int
	RequestedEpoch      uint64
	ExitEpoch           uint64
	SlashableUntilEpoch uint64
	RiskHistoryKey      string
}

type RedelegationRiskRecord struct {
	DelegatorID               string
	SourceValidatorID         string
	DestinationValidatorID    string
	AmountNaet                sdkmath.Int
	RequestedEpoch            uint64
	ActivationEpoch           uint64
	SourceSlashableUntilEpoch uint64
	RiskHistoryKey            string
}

type SelfBondChangeRecord struct {
	ValidatorID      string
	PreviousBondNaet sdkmath.Int
	NewBondNaet      sdkmath.Int
	RequestedEpoch   uint64
	ActivationEpoch  uint64
}

type PendingUnbondingSlashExposureInput struct {
	Record        UnbondingRiskRecord
	FaultEpoch    uint64
	EvidenceEpoch uint64
}

const (
	RiskWindowStatusActive  = "active"
	RiskWindowStatusExited  = "exited"
	RiskWindowStatusExpired = "expired"
	RiskWindowStatusSlashed = "slashed"
)

type RiskWindowRecord struct {
	StakeOwner          string
	ValidatorAddress    string
	AmountNaet          sdkmath.Int
	StartEpoch          uint64
	EndEpoch            uint64
	SlashableUntilEpoch uint64
	RiskHistoryRoot     string
	Status              string
}

type SlashExposureQuery struct {
	StakeOwner       string
	ValidatorAddress string
	FaultEpoch       uint64
	EvidenceEpoch    uint64
}

type SlashExposureQueryResult struct {
	StakeOwner       string
	ValidatorAddress string
	FaultEpoch       uint64
	EvidenceEpoch    uint64
	ExposureNaet     sdkmath.Int
	MatchingWindows  []RiskWindowRecord
}

type RejectedDelegationIntent struct {
	Intent DelegationIntent
	Reason string
}

func BuildElectionCandidates(params Params, electionEpoch uint64, candidates []Candidate, intents []DelegationIntent) ([]Candidate, []RejectedDelegationIntent, error) {
	if err := params.Validate(); err != nil {
		return nil, nil, err
	}
	out := make([]Candidate, len(candidates))
	indexByID := make(map[string]int, len(candidates))
	for i, candidate := range candidates {
		cloned := cloneCandidate(candidate)
		if err := cloned.Validate(params); err != nil {
			return nil, nil, err
		}
		if _, found := indexByID[cloned.ValidatorID]; found {
			return nil, nil, fmt.Errorf("duplicate candidate %q", cloned.ValidatorID)
		}
		indexByID[cloned.ValidatorID] = i
		out[i] = cloned
	}
	activations, rejected, err := ActivateDelegationIntents(params, electionEpoch, out, intents)
	if err != nil {
		return nil, nil, err
	}
	for _, activation := range activations {
		idx, found := indexByID[activation.ValidatorID]
		if !found {
			continue
		}
		out[idx].Nominations = mergeNominations(out[idx].Nominations, activation.Nominations)
		out[idx].DelegatedStakeNaet = sumNominations(out[idx].Nominations)
	}
	return out, rejected, nil
}

func DelegationEffectiveElectionEpoch(params Params, requestedEpoch uint64) (uint64, error) {
	if err := params.Validate(); err != nil {
		return 0, err
	}
	return requestedEpoch + params.DelegationActivationEpochs, nil
}

func DelegationAffectsElection(params Params, requestedEpoch uint64, electionEpoch uint64) (bool, error) {
	effectiveEpoch, err := DelegationEffectiveElectionEpoch(params, requestedEpoch)
	if err != nil {
		return false, err
	}
	return electionEpoch >= effectiveEpoch, nil
}

func UnbondingRiskWindowForParams(params Params) (UnbondingRiskWindow, error) {
	if err := params.Validate(); err != nil {
		return UnbondingRiskWindow{}, err
	}
	unbondingEpochs := ceilDivUint64(params.UnbondingSeconds, params.EpochDurationSeconds)
	totalRiskEpochs, err := checkedAddUint64(unbondingEpochs, params.EvidenceWindowEpochs, "unbonding risk window overflow")
	if err != nil {
		return UnbondingRiskWindow{}, err
	}
	return UnbondingRiskWindow{
		UnbondingEpochs:       unbondingEpochs,
		SlashableWindowEpochs: params.EvidenceWindowEpochs,
		TotalRiskEpochs:       totalRiskEpochs,
	}, nil
}

func BeginUnbondingRisk(params Params, delegatorID string, validatorID string, amount sdkmath.Int, requestedEpoch uint64) (UnbondingRiskRecord, error) {
	if requestedEpoch == 0 {
		return UnbondingRiskRecord{}, errors.New("unbonding requested epoch is required")
	}
	if err := validatePosToken("unbonding delegator id", strings.TrimSpace(delegatorID)); err != nil {
		return UnbondingRiskRecord{}, err
	}
	if err := validatePosToken("unbonding validator id", strings.TrimSpace(validatorID)); err != nil {
		return UnbondingRiskRecord{}, err
	}
	if amount.IsNil() || !amount.IsPositive() {
		return UnbondingRiskRecord{}, errors.New("unbonding amount must be positive")
	}
	window, err := UnbondingRiskWindowForParams(params)
	if err != nil {
		return UnbondingRiskRecord{}, err
	}
	exitEpoch, err := checkedAddUint64(requestedEpoch, window.UnbondingEpochs, "unbonding exit epoch overflow")
	if err != nil {
		return UnbondingRiskRecord{}, err
	}
	slashableUntil, err := checkedAddUint64(requestedEpoch, window.TotalRiskEpochs, "unbonding slashable epoch overflow")
	if err != nil {
		return UnbondingRiskRecord{}, err
	}
	record := UnbondingRiskRecord{
		DelegatorID:         strings.TrimSpace(delegatorID),
		ValidatorID:         strings.TrimSpace(validatorID),
		AmountNaet:          amount,
		RequestedEpoch:      requestedEpoch,
		ExitEpoch:           exitEpoch,
		SlashableUntilEpoch: slashableUntil,
	}
	record.RiskHistoryKey = ComputeUnbondingRiskHistoryKey(record)
	return record, record.Validate()
}

func (r UnbondingRiskRecord) Validate() error {
	if err := validatePosToken("unbonding delegator id", r.DelegatorID); err != nil {
		return err
	}
	if err := validatePosToken("unbonding validator id", r.ValidatorID); err != nil {
		return err
	}
	if r.AmountNaet.IsNil() || !r.AmountNaet.IsPositive() {
		return errors.New("unbonding amount must be positive")
	}
	if r.RequestedEpoch == 0 {
		return errors.New("unbonding requested epoch is required")
	}
	if r.ExitEpoch <= r.RequestedEpoch {
		return errors.New("unbonding exit epoch must be after requested epoch")
	}
	if r.SlashableUntilEpoch < r.ExitEpoch {
		return errors.New("unbonding slashable window must cover exit epoch")
	}
	if err := validatePosHash("unbonding risk history key", r.RiskHistoryKey); err != nil {
		return err
	}
	if expected := ComputeUnbondingRiskHistoryKey(r); expected != r.RiskHistoryKey {
		return errors.New("unbonding risk history key mismatch")
	}
	return nil
}

func CreateRedelegationRiskRecord(params Params, delegatorID string, sourceValidatorID string, destinationValidatorID string, amount sdkmath.Int, requestedEpoch uint64) (RedelegationRiskRecord, error) {
	if requestedEpoch == 0 {
		return RedelegationRiskRecord{}, errors.New("redelegation requested epoch is required")
	}
	delegatorID = strings.TrimSpace(delegatorID)
	sourceValidatorID = strings.TrimSpace(sourceValidatorID)
	destinationValidatorID = strings.TrimSpace(destinationValidatorID)
	if err := validatePosToken("redelegation delegator id", delegatorID); err != nil {
		return RedelegationRiskRecord{}, err
	}
	if err := validatePosToken("redelegation source validator id", sourceValidatorID); err != nil {
		return RedelegationRiskRecord{}, err
	}
	if err := validatePosToken("redelegation destination validator id", destinationValidatorID); err != nil {
		return RedelegationRiskRecord{}, err
	}
	if sourceValidatorID == destinationValidatorID {
		return RedelegationRiskRecord{}, errors.New("redelegation destination must differ from source")
	}
	if amount.IsNil() || !amount.IsPositive() {
		return RedelegationRiskRecord{}, errors.New("redelegation amount must be positive")
	}
	if err := params.Validate(); err != nil {
		return RedelegationRiskRecord{}, err
	}
	activationEpoch, err := checkedAddUint64(requestedEpoch, params.DelegationActivationEpochs, "redelegation activation epoch overflow")
	if err != nil {
		return RedelegationRiskRecord{}, err
	}
	window, err := UnbondingRiskWindowForParams(params)
	if err != nil {
		return RedelegationRiskRecord{}, err
	}
	sourceSlashableUntil, err := checkedAddUint64(requestedEpoch, window.TotalRiskEpochs, "redelegation source slashable epoch overflow")
	if err != nil {
		return RedelegationRiskRecord{}, err
	}
	record := RedelegationRiskRecord{
		DelegatorID:               delegatorID,
		SourceValidatorID:         sourceValidatorID,
		DestinationValidatorID:    destinationValidatorID,
		AmountNaet:                amount,
		RequestedEpoch:            requestedEpoch,
		ActivationEpoch:           activationEpoch,
		SourceSlashableUntilEpoch: sourceSlashableUntil,
	}
	record.RiskHistoryKey = ComputeRedelegationRiskHistoryKey(record)
	return record, record.Validate()
}

func (r RedelegationRiskRecord) Validate() error {
	if err := validatePosToken("redelegation delegator id", r.DelegatorID); err != nil {
		return err
	}
	if err := validatePosToken("redelegation source validator id", r.SourceValidatorID); err != nil {
		return err
	}
	if err := validatePosToken("redelegation destination validator id", r.DestinationValidatorID); err != nil {
		return err
	}
	if r.SourceValidatorID == r.DestinationValidatorID {
		return errors.New("redelegation destination must differ from source")
	}
	if r.AmountNaet.IsNil() || !r.AmountNaet.IsPositive() {
		return errors.New("redelegation amount must be positive")
	}
	if r.RequestedEpoch == 0 {
		return errors.New("redelegation requested epoch is required")
	}
	if r.ActivationEpoch <= r.RequestedEpoch {
		return errors.New("redelegation activation epoch must be after requested epoch")
	}
	if r.SourceSlashableUntilEpoch < r.ActivationEpoch {
		return errors.New("redelegation source risk window must cover activation epoch")
	}
	if err := validatePosHash("redelegation risk history key", r.RiskHistoryKey); err != nil {
		return err
	}
	if expected := ComputeRedelegationRiskHistoryKey(r); expected != r.RiskHistoryKey {
		return errors.New("redelegation risk history key mismatch")
	}
	return nil
}

func PlanSelfBondChange(params Params, validatorID string, previousBond sdkmath.Int, newBond sdkmath.Int, requestedEpoch uint64) (SelfBondChangeRecord, error) {
	if err := params.Validate(); err != nil {
		return SelfBondChangeRecord{}, err
	}
	validatorID = strings.TrimSpace(validatorID)
	if err := validatePosToken("self bond validator id", validatorID); err != nil {
		return SelfBondChangeRecord{}, err
	}
	if previousBond.IsNil() || previousBond.IsNegative() || newBond.IsNil() || newBond.IsNegative() {
		return SelfBondChangeRecord{}, errors.New("self bond amounts cannot be nil or negative")
	}
	if requestedEpoch == 0 {
		return SelfBondChangeRecord{}, errors.New("self bond requested epoch is required")
	}
	activationEpoch, err := checkedAddUint64(requestedEpoch, params.DelegationActivationEpochs, "self bond activation epoch overflow")
	if err != nil {
		return SelfBondChangeRecord{}, err
	}
	record := SelfBondChangeRecord{
		ValidatorID:      validatorID,
		PreviousBondNaet: previousBond,
		NewBondNaet:      newBond,
		RequestedEpoch:   requestedEpoch,
		ActivationEpoch:  activationEpoch,
	}
	return record, record.Validate()
}

func (r SelfBondChangeRecord) Validate() error {
	if err := validatePosToken("self bond validator id", r.ValidatorID); err != nil {
		return err
	}
	if r.PreviousBondNaet.IsNil() || r.PreviousBondNaet.IsNegative() || r.NewBondNaet.IsNil() || r.NewBondNaet.IsNegative() {
		return errors.New("self bond amounts cannot be nil or negative")
	}
	if r.RequestedEpoch == 0 {
		return errors.New("self bond requested epoch is required")
	}
	if r.ActivationEpoch <= r.RequestedEpoch {
		return errors.New("self bond activation epoch must be after requested epoch")
	}
	return nil
}

func PendingUnbondingSlashExposure(input PendingUnbondingSlashExposureInput) (sdkmath.Int, error) {
	if err := input.Record.Validate(); err != nil {
		return sdkmath.Int{}, err
	}
	if input.FaultEpoch == 0 || input.EvidenceEpoch == 0 {
		return sdkmath.Int{}, errors.New("fault and evidence epochs are required")
	}
	if input.FaultEpoch >= input.Record.ExitEpoch {
		return sdkmath.ZeroInt(), nil
	}
	if input.EvidenceEpoch > input.Record.SlashableUntilEpoch {
		return sdkmath.ZeroInt(), nil
	}
	return input.Record.AmountNaet, nil
}

func RiskWindowFromUnbonding(record UnbondingRiskRecord, currentEpoch uint64) (RiskWindowRecord, error) {
	if err := record.Validate(); err != nil {
		return RiskWindowRecord{}, err
	}
	window := RiskWindowRecord{
		StakeOwner:          record.DelegatorID,
		ValidatorAddress:    record.ValidatorID,
		AmountNaet:          record.AmountNaet,
		StartEpoch:          record.RequestedEpoch,
		EndEpoch:            record.ExitEpoch,
		SlashableUntilEpoch: record.SlashableUntilEpoch,
		Status:              riskWindowStatus(record.ExitEpoch, record.SlashableUntilEpoch, currentEpoch),
	}
	window.RiskHistoryRoot = ComputeRiskWindowRoot(window)
	return window, window.Validate()
}

func RiskWindowFromRedelegation(record RedelegationRiskRecord, currentEpoch uint64) (RiskWindowRecord, error) {
	if err := record.Validate(); err != nil {
		return RiskWindowRecord{}, err
	}
	window := RiskWindowRecord{
		StakeOwner:          record.DelegatorID,
		ValidatorAddress:    record.SourceValidatorID,
		AmountNaet:          record.AmountNaet,
		StartEpoch:          record.RequestedEpoch,
		EndEpoch:            record.ActivationEpoch,
		SlashableUntilEpoch: record.SourceSlashableUntilEpoch,
		Status:              riskWindowStatus(record.ActivationEpoch, record.SourceSlashableUntilEpoch, currentEpoch),
	}
	window.RiskHistoryRoot = ComputeRiskWindowRoot(window)
	return window, window.Validate()
}

func (r RiskWindowRecord) Validate() error {
	if err := validatePosToken("risk window stake owner", r.StakeOwner); err != nil {
		return err
	}
	if err := validatePosToken("risk window validator address", r.ValidatorAddress); err != nil {
		return err
	}
	if r.AmountNaet.IsNil() || !r.AmountNaet.IsPositive() {
		return errors.New("risk window amount must be positive")
	}
	if r.StartEpoch == 0 {
		return errors.New("risk window start epoch is required")
	}
	if r.EndEpoch <= r.StartEpoch {
		return errors.New("risk window end epoch must be after start epoch")
	}
	if r.SlashableUntilEpoch < r.EndEpoch {
		return errors.New("risk window slashable epoch must cover end epoch")
	}
	if err := validateRiskWindowStatus(r.Status); err != nil {
		return err
	}
	if err := validatePosHash("risk window history root", r.RiskHistoryRoot); err != nil {
		return err
	}
	if expected := ComputeRiskWindowRoot(r); expected != r.RiskHistoryRoot {
		return errors.New("risk window history root mismatch")
	}
	return nil
}

func QuerySlashExposure(windows []RiskWindowRecord, query SlashExposureQuery) (SlashExposureQueryResult, error) {
	query.StakeOwner = strings.TrimSpace(query.StakeOwner)
	query.ValidatorAddress = strings.TrimSpace(query.ValidatorAddress)
	if err := validatePosToken("slash exposure stake owner", query.StakeOwner); err != nil {
		return SlashExposureQueryResult{}, err
	}
	if err := validatePosToken("slash exposure validator address", query.ValidatorAddress); err != nil {
		return SlashExposureQueryResult{}, err
	}
	if query.FaultEpoch == 0 || query.EvidenceEpoch == 0 {
		return SlashExposureQueryResult{}, errors.New("slash exposure fault and evidence epochs are required")
	}
	result := SlashExposureQueryResult{
		StakeOwner:       query.StakeOwner,
		ValidatorAddress: query.ValidatorAddress,
		FaultEpoch:       query.FaultEpoch,
		EvidenceEpoch:    query.EvidenceEpoch,
		ExposureNaet:     sdkmath.ZeroInt(),
		MatchingWindows:  make([]RiskWindowRecord, 0),
	}
	for _, window := range windows {
		if err := window.Validate(); err != nil {
			return SlashExposureQueryResult{}, err
		}
		if window.StakeOwner != query.StakeOwner || window.ValidatorAddress != query.ValidatorAddress {
			continue
		}
		if query.FaultEpoch < window.StartEpoch || query.FaultEpoch >= window.EndEpoch {
			continue
		}
		if query.EvidenceEpoch > window.SlashableUntilEpoch {
			continue
		}
		if window.Status == RiskWindowStatusExpired {
			continue
		}
		result.ExposureNaet = result.ExposureNaet.Add(window.AmountNaet)
		result.MatchingWindows = append(result.MatchingWindows, window)
	}
	return result, nil
}

func ComputeRiskWindowRoot(record RiskWindowRecord) string {
	return posHashRoot("aetheris-pos-risk-window-v1", func(w posByteWriter) {
		posWritePart(w, record.StakeOwner)
		posWritePart(w, record.ValidatorAddress)
		posWritePart(w, record.AmountNaet.String())
		posWriteUint64(w, record.StartEpoch)
		posWriteUint64(w, record.EndEpoch)
		posWriteUint64(w, record.SlashableUntilEpoch)
		posWritePart(w, record.Status)
	})
}

func ComputeUnbondingRiskHistoryKey(record UnbondingRiskRecord) string {
	return posHashRoot("aetheris-pos-unbonding-risk-v1", func(w posByteWriter) {
		posWritePart(w, record.DelegatorID)
		posWritePart(w, record.ValidatorID)
		posWritePart(w, record.AmountNaet.String())
		posWriteUint64(w, record.RequestedEpoch)
		posWriteUint64(w, record.ExitEpoch)
		posWriteUint64(w, record.SlashableUntilEpoch)
	})
}

func ComputeRedelegationRiskHistoryKey(record RedelegationRiskRecord) string {
	return posHashRoot("aetheris-pos-redelegation-risk-v1", func(w posByteWriter) {
		posWritePart(w, record.DelegatorID)
		posWritePart(w, record.SourceValidatorID)
		posWritePart(w, record.DestinationValidatorID)
		posWritePart(w, record.AmountNaet.String())
		posWriteUint64(w, record.RequestedEpoch)
		posWriteUint64(w, record.ActivationEpoch)
		posWriteUint64(w, record.SourceSlashableUntilEpoch)
	})
}

func ActivateDelegationIntents(params Params, electionEpoch uint64, candidates []Candidate, intents []DelegationIntent) ([]DelegationActivation, []RejectedDelegationIntent, error) {
	if err := params.Validate(); err != nil {
		return nil, nil, err
	}
	candidateByID := make(map[string]Candidate, len(candidates))
	for _, candidate := range candidates {
		id := strings.TrimSpace(candidate.ValidatorID)
		if err := validatePosToken("validator id", id); err != nil {
			return nil, nil, err
		}
		if _, found := candidateByID[id]; found {
			return nil, nil, fmt.Errorf("duplicate candidate %q", id)
		}
		candidate.ValidatorID = id
		candidateByID[id] = candidate
	}

	ordered := make([]DelegationIntent, len(intents))
	copy(ordered, intents)
	sort.SliceStable(ordered, func(i, j int) bool {
		return compareDelegationIntents(ordered[i], ordered[j]) < 0
	})

	nominationsByValidator := make(map[string][]Nomination)
	seenNomination := make(map[string]struct{}, len(ordered))
	rejected := make([]RejectedDelegationIntent, 0)
	for _, intent := range ordered {
		if err := intent.Validate(params); err != nil {
			rejected = append(rejected, RejectedDelegationIntent{Intent: intent, Reason: err.Error()})
			continue
		}
		if electionEpoch < intent.RequestedEpoch+params.DelegationActivationEpochs {
			rejected = append(rejected, RejectedDelegationIntent{Intent: intent, Reason: "delegation activation delay has not elapsed"})
			continue
		}
		candidate, found := candidateByID[intent.ValidatorID]
		if !found {
			rejected = append(rejected, RejectedDelegationIntent{Intent: intent, Reason: "validator is not in election market"})
			continue
		}
		if candidate.Jailed || candidate.Tombstoned {
			rejected = append(rejected, RejectedDelegationIntent{Intent: intent, Reason: "validator is not eligible for delegation"})
			continue
		}
		if candidate.CommissionBps > intent.MaxCommissionBps {
			rejected = append(rejected, RejectedDelegationIntent{Intent: intent, Reason: "validator commission exceeds delegation risk profile"})
			continue
		}
		if candidate.PerformanceScoreBps < intent.MinPerformanceScoreBps {
			rejected = append(rejected, RejectedDelegationIntent{Intent: intent, Reason: "validator performance below delegation risk profile"})
			continue
		}
		nominationKey := intent.ValidatorID + "\x00" + intent.NominatorID
		if _, found := seenNomination[nominationKey]; found {
			rejected = append(rejected, RejectedDelegationIntent{Intent: intent, Reason: "duplicate delegation intent for validator"})
			continue
		}
		seenNomination[nominationKey] = struct{}{}
		nominationsByValidator[intent.ValidatorID] = append(nominationsByValidator[intent.ValidatorID], Nomination{
			NominatorID: intent.NominatorID,
			StakeNaet:   intent.StakeNaet,
		})
	}

	validatorIDs := make([]string, 0, len(nominationsByValidator))
	for validatorID := range nominationsByValidator {
		validatorIDs = append(validatorIDs, validatorID)
	}
	sort.Strings(validatorIDs)

	activations := make([]DelegationActivation, 0, len(validatorIDs))
	for _, validatorID := range validatorIDs {
		nominations := sortNominations(nominationsByValidator[validatorID])
		totalStake := sumNominations(nominations)
		activation := DelegationActivation{
			ValidatorID:   validatorID,
			Nominations:   nominations,
			ActivatedAt:   electionEpoch,
			IntentCount:   uint32(len(nominations)),
			TotalStake:    totalStake,
			ActivationKey: computeDelegationActivationKey(electionEpoch, validatorID, nominations),
		}
		activations = append(activations, activation)
	}
	return activations, rejected, nil
}

func (i DelegationIntent) Validate(params Params) error {
	if err := validatePosToken("nominator id", i.NominatorID); err != nil {
		return err
	}
	if err := validatePosToken("validator id", i.ValidatorID); err != nil {
		return err
	}
	if !i.StakeNaet.IsPositive() {
		return errors.New("delegation intent stake must be positive")
	}
	if i.MaxCommissionBps > params.MaxCommissionBps {
		return fmt.Errorf("delegation max commission must be <= %d bps", params.MaxCommissionBps)
	}
	if i.MinPerformanceScoreBps > BasisPoints {
		return fmt.Errorf("delegation minimum performance must be <= %d bps", BasisPoints)
	}
	return nil
}

func validateRiskWindowStatus(status string) error {
	switch status {
	case RiskWindowStatusActive, RiskWindowStatusExited, RiskWindowStatusExpired, RiskWindowStatusSlashed:
		return nil
	default:
		return fmt.Errorf("unsupported risk window status %q", status)
	}
}

func riskWindowStatus(endEpoch uint64, slashableUntilEpoch uint64, currentEpoch uint64) string {
	if currentEpoch > slashableUntilEpoch {
		return RiskWindowStatusExpired
	}
	if currentEpoch >= endEpoch {
		return RiskWindowStatusExited
	}
	return RiskWindowStatusActive
}

func compareDelegationIntents(left, right DelegationIntent) int {
	if left.ValidatorID < right.ValidatorID {
		return -1
	}
	if left.ValidatorID > right.ValidatorID {
		return 1
	}
	if left.NominatorID < right.NominatorID {
		return -1
	}
	if left.NominatorID > right.NominatorID {
		return 1
	}
	if left.RequestedEpoch < right.RequestedEpoch {
		return -1
	}
	if left.RequestedEpoch > right.RequestedEpoch {
		return 1
	}
	return 0
}

func computeDelegationActivationKey(epoch uint64, validatorID string, nominations []Nomination) string {
	return posHashRoot("aetheris-pos-delegation-activation-v1", func(w posByteWriter) {
		posWriteUint64(w, epoch)
		posWritePart(w, validatorID)
		posWriteUint64(w, uint64(len(nominations)))
		for _, nomination := range nominations {
			posWritePart(w, nomination.NominatorID)
			posWritePart(w, nomination.StakeNaet.String())
		}
	})
}

func mergeNominations(existing []Nomination, activated []Nomination) []Nomination {
	byNominator := make(map[string]sdkmath.Int, len(existing)+len(activated))
	for _, nomination := range existing {
		current, found := byNominator[nomination.NominatorID]
		if !found {
			current = sdkmath.ZeroInt()
		}
		byNominator[nomination.NominatorID] = current.Add(nomination.StakeNaet)
	}
	for _, nomination := range activated {
		current, found := byNominator[nomination.NominatorID]
		if !found {
			current = sdkmath.ZeroInt()
		}
		byNominator[nomination.NominatorID] = current.Add(nomination.StakeNaet)
	}
	nominatorIDs := sortedStringKeys(byNominator)
	out := make([]Nomination, 0, len(nominatorIDs))
	for _, nominatorID := range nominatorIDs {
		out = append(out, Nomination{NominatorID: nominatorID, StakeNaet: byNominator[nominatorID]})
	}
	return out
}
