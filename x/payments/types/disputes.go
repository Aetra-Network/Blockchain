package types

import (
	"errors"
	"fmt"
	"strings"

	"github.com/sovereign-l1/l1/app/addressing"
)

const (
	ChannelStatusOpen         ChannelStatus = "OPEN"
	ChannelStatusPendingClose ChannelStatus = "PENDING_CLOSE"
	ChannelStatusSettled      ChannelStatus = "SETTLED"
)

const (
	BatchOperationOpen    BatchOperationType = "OPEN"
	BatchOperationClose   BatchOperationType = "CLOSE"
	BatchOperationDispute BatchOperationType = "DISPUTE"
	BatchOperationSettle  BatchOperationType = "SETTLE"
)

type ClosedChannelTombstone struct {
	ChainID        string
	ChannelID      string
	FinalizedNonce uint64
	StateHash      string
	ClosedHeight   uint64
	ExpiresHeight  uint64
}

type ChannelDisputeRequest struct {
	ChannelID             string
	ClosingStateReference string
	NewerState            ChannelState
	FraudProof            FraudProof
	ConditionProofs       []ConditionResolution
	Submitter             string
	CurrentHeight         uint64
	DisputeFeePaid        string
}

type WatchDisputeSubmission struct {
	WatchService          string
	Delegator             string
	ChannelID             string
	ClosingStateReference string
	NewerState            ChannelState
	CurrentHeight         uint64
	EvidenceHash          string
}

type ValidatorPaymentServiceMetadata struct {
	ValidatorAddress string
	ServiceAddress   string
	WatchEndpoint    string
	RoutingEndpoint  string
	PublicKey        string
	MinDelegation    string
	CommissionBps    uint32
	Active           bool
	UpdatedHeight    uint64
	MetadataHash     string
}

type ValidatorWatchRegistration struct {
	ValidatorAddress string
	ServiceAddress   string
	Delegator        string
	MinDelegation    string
	RegisteredHeight uint64
	MetadataHash     string
}

type ValidatorAssistedDisputeSubmission struct {
	ValidatorAddress      string
	ServiceAddress        string
	Delegator             string
	ChannelID             string
	ClosingStateReference string
	NewerState            ChannelState
	CurrentHeight         uint64
	EvidenceHash          string
}

type PendingClose struct {
	Submitter          string
	SubmittedHeight    uint64
	SettleAfterHeight  uint64
	DisputeCount       uint32
	CloseReason        CloseReason
	SettlementFeeDenom string
	SettlementFee      string
	State              ChannelState
	FraudProofs        []FraudProof
	ConditionProofs    []ConditionResolution
	Penalties          []Penalty
	PenaltyAllocations []PenaltyAllocation
}

func (r ChannelDisputeRequest) Normalize() ChannelDisputeRequest {
	r.ChannelID = normalizeHash(r.ChannelID)
	r.ClosingStateReference = normalizeHash(r.ClosingStateReference)
	r.NewerState = r.NewerState.Normalize()
	r.FraudProof = r.FraudProof.Normalize()
	r.ConditionProofs = normalizeConditionResolutions(r.ConditionProofs)
	r.Submitter = strings.TrimSpace(r.Submitter)
	r.DisputeFeePaid = strings.TrimSpace(r.DisputeFeePaid)
	if r.DisputeFeePaid == "" {
		r.DisputeFeePaid = "0"
	}
	return r
}

func (w WatchDisputeSubmission) Normalize() WatchDisputeSubmission {
	w.WatchService = strings.TrimSpace(w.WatchService)
	w.Delegator = strings.TrimSpace(w.Delegator)
	w.ChannelID = normalizeHash(w.ChannelID)
	w.ClosingStateReference = normalizeHash(w.ClosingStateReference)
	w.NewerState = w.NewerState.Normalize()
	w.EvidenceHash = normalizeOptionalHash(w.EvidenceHash)
	return w
}

func (w WatchDisputeSubmission) ValidateForChannel(channel ChannelRecord) error {
	w = w.Normalize()
	channel = channel.Normalize()
	if err := addressing.ValidateUserAddress("payments watch service", w.WatchService); err != nil {
		return err
	}
	if err := addressing.ValidateUserAddress("payments watch delegator", w.Delegator); err != nil {
		return err
	}
	if !containsString(channel.Participants, w.Delegator) {
		return errors.New("payments watch delegator must be channel participant")
	}
	if w.ChannelID != channel.ChannelID {
		return errors.New("payments watch dispute channel mismatch")
	}
	if err := ValidateHash("payments watch dispute closing reference", w.ClosingStateReference); err != nil {
		return err
	}
	if w.CurrentHeight == 0 {
		return errors.New("payments watch dispute height must be positive")
	}
	if w.EvidenceHash != "" {
		if err := ValidateHash("payments watch dispute evidence hash", w.EvidenceHash); err != nil {
			return err
		}
	}
	return w.NewerState.ValidateForChannel(channel, false)
}

func (m ValidatorPaymentServiceMetadata) Normalize() ValidatorPaymentServiceMetadata {
	m.ValidatorAddress = strings.TrimSpace(m.ValidatorAddress)
	m.ServiceAddress = strings.TrimSpace(m.ServiceAddress)
	m.WatchEndpoint = strings.TrimSpace(m.WatchEndpoint)
	m.RoutingEndpoint = strings.TrimSpace(m.RoutingEndpoint)
	m.PublicKey = strings.TrimSpace(m.PublicKey)
	m.MinDelegation = strings.TrimSpace(m.MinDelegation)
	if m.MinDelegation == "" {
		m.MinDelegation = "0"
	}
	m.MetadataHash = normalizeOptionalHash(m.MetadataHash)
	return m
}

func (m ValidatorPaymentServiceMetadata) Validate() error {
	m = m.Normalize()
	if err := addressing.ValidateUserAddress("payments validator service validator", m.ValidatorAddress); err != nil {
		return err
	}
	if err := addressing.ValidateUserAddress("payments validator service address", m.ServiceAddress); err != nil {
		return err
	}
	if m.WatchEndpoint == "" && m.RoutingEndpoint == "" {
		return errors.New("payments validator service requires watch or routing endpoint")
	}
	if err := validateNonNegativeInt("payments validator service minimum delegation", m.MinDelegation); err != nil {
		return err
	}
	if m.CommissionBps > MaxPenaltyRouteBps {
		return errors.New("payments validator service commission exceeds 10000 bps")
	}
	if m.UpdatedHeight == 0 {
		return errors.New("payments validator service update height must be positive")
	}
	expected := ComputeValidatorPaymentServiceMetadataHash(m)
	if m.MetadataHash != "" && m.MetadataHash != expected {
		return errors.New("payments validator service metadata hash mismatch")
	}
	return nil
}

func (r ValidatorWatchRegistration) Normalize() ValidatorWatchRegistration {
	r.ValidatorAddress = strings.TrimSpace(r.ValidatorAddress)
	r.ServiceAddress = strings.TrimSpace(r.ServiceAddress)
	r.Delegator = strings.TrimSpace(r.Delegator)
	r.MinDelegation = strings.TrimSpace(r.MinDelegation)
	if r.MinDelegation == "" {
		r.MinDelegation = "0"
	}
	r.MetadataHash = normalizeOptionalHash(r.MetadataHash)
	return r
}

func (r ValidatorWatchRegistration) Validate(metadata ValidatorPaymentServiceMetadata) error {
	r = r.Normalize()
	metadata = metadata.Normalize()
	if err := metadata.Validate(); err != nil {
		return err
	}
	if !metadata.Active || metadata.WatchEndpoint == "" {
		return errors.New("payments validator watch service is not active")
	}
	if r.ValidatorAddress != metadata.ValidatorAddress || r.ServiceAddress != metadata.ServiceAddress {
		return errors.New("payments validator watch registration service mismatch")
	}
	if err := addressing.ValidateUserAddress("payments validator watch delegator", r.Delegator); err != nil {
		return err
	}
	if r.RegisteredHeight == 0 {
		return errors.New("payments validator watch registration height must be positive")
	}
	if r.RegisteredHeight < metadata.UpdatedHeight {
		return errors.New("payments validator watch registration predates metadata")
	}
	if r.MetadataHash != "" && r.MetadataHash != metadata.MetadataHash {
		return errors.New("payments validator watch registration metadata hash mismatch")
	}
	return validateNonNegativeInt("payments validator watch minimum delegation", r.MinDelegation)
}

func (s ValidatorAssistedDisputeSubmission) Normalize() ValidatorAssistedDisputeSubmission {
	s.ValidatorAddress = strings.TrimSpace(s.ValidatorAddress)
	s.ServiceAddress = strings.TrimSpace(s.ServiceAddress)
	s.Delegator = strings.TrimSpace(s.Delegator)
	s.ChannelID = normalizeHash(s.ChannelID)
	s.ClosingStateReference = normalizeHash(s.ClosingStateReference)
	s.NewerState = s.NewerState.Normalize()
	s.EvidenceHash = normalizeOptionalHash(s.EvidenceHash)
	return s
}

func (s ValidatorAssistedDisputeSubmission) ValidateForChannel(channel ChannelRecord, metadata ValidatorPaymentServiceMetadata) error {
	s = s.Normalize()
	metadata = metadata.Normalize()
	if err := metadata.Validate(); err != nil {
		return err
	}
	if !metadata.Active || metadata.WatchEndpoint == "" {
		return errors.New("payments validator watch service is not active")
	}
	if s.ValidatorAddress != metadata.ValidatorAddress {
		return errors.New("payments validator assisted dispute validator mismatch")
	}
	if s.ServiceAddress == "" {
		s.ServiceAddress = metadata.ServiceAddress
	}
	if s.ServiceAddress != metadata.ServiceAddress {
		return errors.New("payments validator assisted dispute service mismatch")
	}
	return (WatchDisputeSubmission{
		WatchService:          metadata.ServiceAddress,
		Delegator:             s.Delegator,
		ChannelID:             s.ChannelID,
		ClosingStateReference: s.ClosingStateReference,
		NewerState:            s.NewerState,
		CurrentHeight:         s.CurrentHeight,
		EvidenceHash:          s.EvidenceHash,
	}).ValidateForChannel(channel)
}

func (t ClosedChannelTombstone) Normalize() ClosedChannelTombstone {
	t.ChainID = strings.TrimSpace(t.ChainID)
	t.ChannelID = normalizeHash(t.ChannelID)
	t.StateHash = normalizeHash(t.StateHash)
	return t
}

func (t ClosedChannelTombstone) Validate() error {
	t = t.Normalize()
	if strings.TrimSpace(t.ChainID) == "" {
		return errors.New("payments tombstone chain id is required")
	}
	if err := ValidateHash("payments tombstone channel id", t.ChannelID); err != nil {
		return err
	}
	if t.FinalizedNonce == 0 {
		return errors.New("payments tombstone finalized nonce must be positive")
	}
	if err := ValidateHash("payments tombstone state hash", t.StateHash); err != nil {
		return err
	}
	if t.ClosedHeight == 0 {
		return errors.New("payments tombstone closed height must be positive")
	}
	if t.ExpiresHeight <= t.ClosedHeight {
		return errors.New("payments tombstone replay horizon must exceed close height")
	}
	return nil
}

func (p PendingClose) Normalize() PendingClose {
	p.Submitter = strings.TrimSpace(p.Submitter)
	if p.CloseReason == "" {
		p.CloseReason = CloseReasonUnilateral
	}
	p.SettlementFeeDenom = normalizeAssetDenom(p.SettlementFeeDenom)
	p.SettlementFee = strings.TrimSpace(p.SettlementFee)
	p.State = p.State.Normalize()
	p.FraudProofs = normalizeFraudProofs(p.FraudProofs)
	p.ConditionProofs = normalizeConditionResolutions(p.ConditionProofs)
	p.Penalties = normalizePenalties(p.Penalties)
	p.PenaltyAllocations = normalizePenaltyAllocations(p.PenaltyAllocations)
	return p
}

func (p PendingClose) ValidateForChannel(channel ChannelRecord) error {
	p = p.Normalize()
	if err := addressing.ValidateUserAddress("payments pending close submitter", p.Submitter); err != nil {
		return err
	}
	if !containsString(channel.Participants, p.Submitter) {
		return errors.New("payments pending close submitter must be participant")
	}
	if p.SubmittedHeight == 0 {
		return errors.New("payments pending close submitted height must be positive")
	}
	if p.SettleAfterHeight <= p.SubmittedHeight {
		return errors.New("payments pending close settlement height must exceed submitted height")
	}
	if err := validateCloseReason(p.CloseReason); err != nil {
		return err
	}
	if p.SettlementFeeDenom != NativeDenom {
		return fmt.Errorf("payments settlement fee denom must be %s", NativeDenom)
	}
	if err := validateNonNegativeInt("payments settlement fee", p.SettlementFee); err != nil {
		return err
	}
	if err := p.State.ValidateForChannel(channel, false); err != nil {
		return err
	}
	for _, proof := range p.FraudProofs {
		if err := proof.ValidateForChannel(channel); err != nil {
			return err
		}
	}
	if err := validateConditionResolutionsForState(p.State, channel, p.ConditionProofs, false); err != nil {
		return err
	}
	for _, penalty := range p.Penalties {
		if err := penalty.ValidateForChannel(channel); err != nil {
			return err
		}
	}
	for _, allocation := range p.PenaltyAllocations {
		if err := allocation.ValidateForChannel(channel); err != nil {
			return err
		}
	}
	return nil
}
