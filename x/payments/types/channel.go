package types

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	sdkmath "cosmossdk.io/math"

	"github.com/sovereign-l1/l1/app/addressing"
)

type ChannelType string

type ChannelStatus string

type ChannelFinality string

func CanonicalStateRequiredFields() []string {
	return []string{
		"chain_id",
		"app_version",
		"module_name",
		"channel_id",
		"channel_type",
		"participant_set_hash",
		"balances",
		"reserves",
		"pending_condition_amounts",
		"accrued_fees",
		"nonce",
		"epoch",
		"previous_state_hash",
		"timeout_height",
		"timeout_timestamp",
		"challenge_period",
		"condition_root",
		"condition_count",
		"required_signer_bitmap",
		"signature_scheme",
		"signature_preimage_hash",
	}
}

type CloseReason string

type Balance struct {
	Participant string
	Amount      string
}

type ChannelOpenRequest struct {
	ChainID                      string
	ChannelID                    string
	Participants                 []string
	InitialBalances              []Balance
	ChannelType                  ChannelType
	Collateral                   string
	CloseDelay                   uint64
	ChallengePeriod              uint64
	FeePolicyID                  string
	OpeningFeeDenom              string
	OpeningFeePaid               string
	RoutingAdvertised            bool
	ConditionalPaymentsSupported bool
	OpenHeight                   uint64
	ExpirationHeight             uint64
	ExpirationTimestamp          int64
}

type ChannelUpdateRequest struct {
	ChannelID            string
	State                ChannelState
	ConditionCommitments []ConditionalPayment
	AsyncDeltas          []AsyncPaymentDelta
	RegisterCheckpoint   bool
	Submitter            string
	CurrentHeight        uint64
	CheckpointFeePaid    string
}

type ChannelUpdateResult struct {
	ChannelID            string
	StateHash            string
	Nonce                uint64
	ValidatedOffChain    bool
	CheckpointRegistered bool
	Liquidity            []Balance
}

type ChannelCloseRequest struct {
	ChannelID     string
	ClosingState  ChannelState
	Signatures    []StateSignature
	CloseReason   CloseReason
	Submitter     string
	CurrentHeight uint64
	SettlementFee string
}

type StateHashDebug struct {
	ChannelID                string
	Status                   ChannelStatus
	LatestNonce              uint64
	LatestStateHash          string
	ComputedLatestStateHash  string
	PendingNonce             uint64
	PendingStateHash         string
	ComputedPendingStateHash string
	FinalizedNonce           uint64
	DisputedNonce            uint64
}

type ChannelState struct {
	ChainID               string
	AppVersion            uint32
	ModuleName            string
	RequiredFields        []string
	ChannelID             string
	ChannelType           ChannelType
	ParticipantSetHash    string
	Denom                 string
	Version               uint32
	ParticipantA          string
	ParticipantB          string
	BalanceA              string
	BalanceB              string
	ReserveA              string
	ReserveB              string
	AccruedFees           string
	Epoch                 uint64
	Nonce                 uint64
	PendingConditionsRoot string
	ConditionRoot         string
	ConditionCount        uint32
	Balances              []Balance
	Conditions            []ConditionalPayment
	PreviousStateHash     string
	StateHash             string
	TimeoutHeight         uint64
	TimeoutTimestamp      int64
	ChallengePeriod       uint64
	CloseDelay            uint64
	FeePolicyID           string
	RequiredSignerBitmap  string
	SignatureScheme       string
	SignaturePreimageHash string
	CheckpointNonce       uint64
	CheckpointBalances    []Balance
	AsyncUpdateRoot       string
	AcceptedUpdateRoot    string
	SendWindow            uint64
	ReceiveWindow         uint64
	MaxUnackedAmount      string
	ExpiryHeight          uint64
	Signatures            []StateSignature
}

type ChannelRecord struct {
	ChainID             string
	ChannelID           string
	ChannelType         ChannelType
	Participants        []string
	RequiredSigners     []string
	Payer               string
	Receiver            string
	ReceiverAckRequired bool
	Denom               string
	Collateral          string
	OpenHeight          uint64
	CloseDelay          uint64
	DisputePeriod       uint64
	ExpirationHeight    uint64
	ExpirationTimestamp int64
	OpeningFeeDenom     string
	OpeningFeePaid      string
	RoutingAdvertised   bool
	ConditionalPayments bool
	CustodyDenom        string
	CustodyAmount       string
	Status              ChannelStatus
	Finality            ChannelFinality
	OpeningStateHash    string
	FinalizedNonce      uint64
	DisputedNonce       uint64
	LatestState         ChannelState
	LatestClaim         UnidirectionalClaim
	PendingClose        PendingClose
}

func BuildState(state ChannelState) (ChannelState, error) {
	state = state.Normalize()
	state.SignaturePreimageHash = ComputeStateSignaturePreimageHash(state)
	if err := validateUnsignedStateShape(state); err != nil {
		return ChannelState{}, err
	}
	state.StateHash = ComputeStateHash(state)
	return state, nil
}

func BuildChannelFromOpenRequest(req ChannelOpenRequest) (ChannelRecord, error) {
	req = req.Normalize()
	if err := req.Validate(); err != nil {
		return ChannelRecord{}, err
	}
	channel := ChannelRecord{
		ChainID:             req.ChainID,
		ChannelID:           req.ChannelID,
		ChannelType:         req.ChannelType,
		Participants:        req.Participants,
		Denom:               NativeDenom,
		Collateral:          req.Collateral,
		OpenHeight:          req.OpenHeight,
		CloseDelay:          req.CloseDelay,
		DisputePeriod:       req.ChallengePeriod,
		ExpirationHeight:    req.ExpirationHeight,
		ExpirationTimestamp: req.ExpirationTimestamp,
		OpeningFeeDenom:     req.OpeningFeeDenom,
		OpeningFeePaid:      req.OpeningFeePaid,
		RoutingAdvertised:   req.RoutingAdvertised,
		ConditionalPayments: req.ConditionalPaymentsSupported,
		CustodyDenom:        NativeDenom,
		CustodyAmount:       req.Collateral,
		Status:              ChannelStatusOpen,
	}
	if req.ChannelType == ChannelTypeUnidirectional && len(req.Participants) == 2 {
		channel.Payer = req.Participants[0]
		channel.Receiver = req.Participants[1]
	}
	state, err := BuildState(openingStateForRequest(req, channel))
	if err != nil {
		return ChannelRecord{}, err
	}
	for _, signer := range channel.Normalize().Participants {
		sig, err := SignatureForState(state, signer)
		if err != nil {
			return ChannelRecord{}, err
		}
		state.Signatures = append(state.Signatures, sig)
	}
	channel.LatestState = state.Normalize()
	channel.OpeningStateHash = channel.LatestState.StateHash
	if err := channel.Validate(); err != nil {
		return ChannelRecord{}, err
	}
	return channel.Normalize(), nil
}

func (r ChannelOpenRequest) Normalize() ChannelOpenRequest {
	r.ChainID = strings.TrimSpace(r.ChainID)
	r.ChannelID = normalizeOptionalHash(r.ChannelID)
	r.Participants = normalizeAddressSet(r.Participants)
	r.InitialBalances = normalizeBalances(r.InitialBalances)
	r.Collateral = strings.TrimSpace(r.Collateral)
	r.FeePolicyID = strings.TrimSpace(r.FeePolicyID)
	if r.FeePolicyID == "" {
		r.FeePolicyID = NativeDenom
	}
	r.OpeningFeeDenom = normalizeAssetDenom(r.OpeningFeeDenom)
	r.OpeningFeePaid = strings.TrimSpace(r.OpeningFeePaid)
	if r.ChannelID == "" {
		parts := append([]string{"open", r.ChainID, string(r.ChannelType), r.Collateral}, r.Participants...)
		r.ChannelID = HashParts(parts...)
	}
	return r
}

func (r ChannelOpenRequest) Validate() error {
	req := r.Normalize()
	if strings.TrimSpace(req.ChainID) == "" {
		return errors.New("payments open chain id is required")
	}
	if err := ValidateHash("payments open channel id", req.ChannelID); err != nil {
		return err
	}
	if !IsChannelType(req.ChannelType) {
		return fmt.Errorf("unknown payments open channel type %q", req.ChannelType)
	}
	if err := validateAddressSet("payments open participant", req.Participants, 2, MaxParticipants); err != nil {
		return err
	}
	if err := validateBalances(req.InitialBalances); err != nil {
		return err
	}
	if err := validateInitialBalances(req.InitialBalances, req.Participants, req.Collateral); err != nil {
		return err
	}
	if err := validatePositiveInt("payments open collateral", req.Collateral); err != nil {
		return err
	}
	if err := validateCloseDelay(req.CloseDelay); err != nil {
		return err
	}
	if err := validateChallengePeriod(req.ChallengePeriod); err != nil {
		return err
	}
	if req.FeePolicyID != NativeDenom {
		return fmt.Errorf("payments open fee policy must be %s", NativeDenom)
	}
	if req.OpeningFeeDenom != NativeDenom {
		return fmt.Errorf("payments opening fee denom must be %s", NativeDenom)
	}
	if err := validateOpeningFeePaid(req.OpeningFeePaid); err != nil {
		return err
	}
	if req.OpenHeight == 0 {
		return errors.New("payments open height must be positive")
	}
	if req.ExpirationTimestamp < 0 {
		return errors.New("payments open expiration timestamp must be non-negative")
	}
	if req.ChannelType == ChannelTypeUnidirectional && req.ExpirationHeight == 0 {
		return errors.New("payments unidirectional open expiration height must be positive")
	}
	if req.ChannelType == ChannelTypeAsync && req.ExpirationHeight == 0 {
		return errors.New("payments async open expiry height must be positive")
	}
	return nil
}

func (r ChannelUpdateRequest) Normalize() ChannelUpdateRequest {
	r.ChannelID = normalizeHash(r.ChannelID)
	r.State = r.State.Normalize()
	r.ConditionCommitments = normalizeConditions(r.ConditionCommitments)
	r.AsyncDeltas = normalizeAsyncDeltas(r.AsyncDeltas)
	r.Submitter = strings.TrimSpace(r.Submitter)
	r.CheckpointFeePaid = strings.TrimSpace(r.CheckpointFeePaid)
	if r.CheckpointFeePaid == "" {
		r.CheckpointFeePaid = "0"
	}
	return r
}

func (r ChannelCloseRequest) Normalize() ChannelCloseRequest {
	r.ChannelID = normalizeHash(r.ChannelID)
	r.ClosingState = r.ClosingState.Normalize()
	r.Signatures = normalizeStateSignatures(r.Signatures)
	r.Submitter = strings.TrimSpace(r.Submitter)
	r.SettlementFee = strings.TrimSpace(r.SettlementFee)
	if r.CloseReason == "" {
		r.CloseReason = CloseReasonUnilateral
	}
	return r
}

func (r ChannelCloseRequest) ClosingStateWithSignatures() ChannelState {
	req := r.Normalize()
	state := req.ClosingState
	if len(req.Signatures) > 0 {
		state.Signatures = req.Signatures
	}
	return state.Normalize()
}

func (r ChannelCloseRequest) ValidateForChannel(channel ChannelRecord) error {
	req := r.Normalize()
	channel = channel.Normalize()
	if req.ChannelID != channel.ChannelID {
		return errors.New("payments close request channel mismatch")
	}
	if err := validateCloseReason(req.CloseReason); err != nil {
		return err
	}
	if err := addressing.ValidateUserAddress("payments close submitter", req.Submitter); err != nil {
		return err
	}
	if !containsString(channel.Participants, req.Submitter) {
		return errors.New("payments close submitter must be participant")
	}
	if req.CurrentHeight == 0 {
		return errors.New("payments close height must be positive")
	}
	if err := validateNonNegativeInt("payments settlement fee", req.SettlementFee); err != nil {
		return err
	}
	return req.ClosingStateWithSignatures().ValidateForChannel(channel, false)
}

func ValidateOffchainUpdate(channel ChannelRecord, req ChannelUpdateRequest) (ChannelUpdateResult, error) {
	channel = channel.Normalize()
	if err := channel.ValidateCore(); err != nil {
		return ChannelUpdateResult{}, err
	}
	if channel.Status != ChannelStatusOpen {
		return ChannelUpdateResult{}, errors.New("payments update requires open channel")
	}
	req = req.Normalize()
	if req.ChannelID != channel.ChannelID {
		return ChannelUpdateResult{}, errors.New("payments update channel mismatch")
	}
	if req.CurrentHeight == 0 {
		return ChannelUpdateResult{}, errors.New("payments update height must be positive")
	}
	if req.Submitter != "" && !containsString(channel.Participants, req.Submitter) {
		return ChannelUpdateResult{}, errors.New("payments update submitter must be participant")
	}
	if req.State.Nonce <= channel.LatestState.Nonce {
		return ChannelUpdateResult{}, errors.New("payments update nonce must increase")
	}
	if err := ValidatePreviousHashContinuity(channel, req.State); err != nil {
		return ChannelUpdateResult{}, err
	}
	if len(req.ConditionCommitments) > 0 {
		if !channel.ConditionalPayments {
			return ChannelUpdateResult{}, errors.New("payments channel does not support conditional payments")
		}
		if err := validateConditions(req.ConditionCommitments); err != nil {
			return ChannelUpdateResult{}, err
		}
		if req.State.PendingConditionsRoot != ComputeConditionsRoot(req.ConditionCommitments) {
			return ChannelUpdateResult{}, errors.New("payments update condition commitment root mismatch")
		}
	}
	if err := req.State.ValidateForChannel(channel, false); err != nil {
		return ChannelUpdateResult{}, err
	}
	if err := validateUpdateExposure(req.State); err != nil {
		return ChannelUpdateResult{}, err
	}
	if len(req.AsyncDeltas) > 0 {
		reconstructed, err := BuildAsyncCheckpointState(channel, req.AsyncDeltas, req.State.CheckpointNonce, req.CurrentHeight)
		if err != nil {
			return ChannelUpdateResult{}, err
		}
		if reconstructed.StateHash != req.State.StateHash {
			return ChannelUpdateResult{}, errors.New("payments async update checkpoint mismatch")
		}
	}
	return ChannelUpdateResult{
		ChannelID:         channel.ChannelID,
		StateHash:         req.State.StateHash,
		Nonce:             req.State.Nonce,
		ValidatedOffChain: true,
		Liquidity:         req.State.Balances,
	}, nil
}

func (s ChannelState) Normalize() ChannelState {
	s.ChainID = strings.TrimSpace(s.ChainID)
	if s.AppVersion == 0 {
		s.AppVersion = CurrentAppVersion
	}
	s.ModuleName = strings.TrimSpace(s.ModuleName)
	if s.ModuleName == "" {
		s.ModuleName = ModuleName
	}
	s.RequiredFields = normalizeRequiredFields(s.RequiredFields)
	if len(s.RequiredFields) == 0 {
		s.RequiredFields = CanonicalStateRequiredFields()
	}
	s.ChannelID = normalizeHash(s.ChannelID)
	s.ParticipantSetHash = normalizeOptionalHash(s.ParticipantSetHash)
	s.Denom = strings.TrimSpace(s.Denom)
	if s.Version == 0 {
		s.Version = CurrentStateVersion
	}
	s.ParticipantA = strings.TrimSpace(s.ParticipantA)
	s.ParticipantB = strings.TrimSpace(s.ParticipantB)
	s.BalanceA = strings.TrimSpace(s.BalanceA)
	s.BalanceB = strings.TrimSpace(s.BalanceB)
	s.ReserveA = strings.TrimSpace(s.ReserveA)
	s.ReserveB = strings.TrimSpace(s.ReserveB)
	s.AccruedFees = strings.TrimSpace(s.AccruedFees)
	s.PreviousStateHash = normalizeOptionalHash(s.PreviousStateHash)
	s.StateHash = normalizeOptionalHash(s.StateHash)
	s.Balances = normalizeBalances(s.Balances)
	s.Conditions = normalizeConditions(s.Conditions)
	s.PendingConditionsRoot = normalizeOptionalHash(s.PendingConditionsRoot)
	s.ConditionRoot = normalizeOptionalHash(s.ConditionRoot)
	if s.ConditionRoot == "" && s.PendingConditionsRoot != "" {
		s.ConditionRoot = s.PendingConditionsRoot
	}
	if s.ConditionRoot == "" {
		s.ConditionRoot = ComputeConditionsRoot(s.Conditions)
	}
	if s.PendingConditionsRoot == "" {
		s.PendingConditionsRoot = s.ConditionRoot
	}
	if s.ConditionCount == 0 && len(s.Conditions) > 0 {
		s.ConditionCount = uint32(len(s.Conditions))
	}
	if s.ChallengePeriod == 0 {
		s.ChallengePeriod = s.CloseDelay
	}
	if s.FeePolicyID == "" {
		s.FeePolicyID = NativeDenom
	}
	s.RequiredSignerBitmap = strings.TrimSpace(s.RequiredSignerBitmap)
	s.SignatureScheme = strings.TrimSpace(s.SignatureScheme)
	if s.SignatureScheme == "" {
		s.SignatureScheme = SignatureSchemeEd25519
	}
	s.SignaturePreimageHash = normalizeOptionalHash(s.SignaturePreimageHash)
	s.CheckpointBalances = normalizeBalances(s.CheckpointBalances)
	s.AsyncUpdateRoot = normalizeOptionalHash(s.AsyncUpdateRoot)
	s.AcceptedUpdateRoot = normalizeOptionalHash(s.AcceptedUpdateRoot)
	s.MaxUnackedAmount = strings.TrimSpace(s.MaxUnackedAmount)
	if s.ChannelType == ChannelTypeAsync {
		if s.CheckpointNonce == 0 {
			s.CheckpointNonce = s.Nonce
		}
		if len(s.CheckpointBalances) == 0 && len(s.Balances) > 0 {
			s.CheckpointBalances = normalizeBalances(s.Balances)
		}
		if len(s.Balances) == 0 && len(s.CheckpointBalances) > 0 {
			s.Balances = normalizeBalances(s.CheckpointBalances)
		}
		if s.AsyncUpdateRoot == "" {
			s.AsyncUpdateRoot = ComputeAsyncDeltaRoot(nil)
		}
		if s.AcceptedUpdateRoot == "" {
			s.AcceptedUpdateRoot = ComputeAsyncDeltaRoot(nil)
		}
	}
	if s.ChannelType == ChannelTypeBidirectional && len(s.Balances) == 2 {
		if s.ParticipantA == "" {
			s.ParticipantA = s.Balances[0].Participant
		}
		if s.ParticipantB == "" {
			s.ParticipantB = s.Balances[1].Participant
		}
		if s.BalanceA == "" {
			s.BalanceA = s.Balances[0].Amount
		}
		if s.BalanceB == "" {
			s.BalanceB = s.Balances[1].Amount
		}
	}
	if s.ReserveA == "" {
		s.ReserveA = "0"
	}
	if s.ReserveB == "" {
		s.ReserveB = "0"
	}
	if s.AccruedFees == "" {
		s.AccruedFees = "0"
	}
	if s.ParticipantSetHash == "" {
		s.ParticipantSetHash = ComputeParticipantSetHash(participantsFromBalances(s.Balances))
	}
	if s.RequiredSignerBitmap == "" {
		participants := participantsFromBalances(s.Balances)
		s.RequiredSignerBitmap = ComputeRequiredSignerBitmap(participants, participants)
	}
	if s.SignaturePreimageHash == "" {
		s.SignaturePreimageHash = ComputeStateSignaturePreimageHash(s)
	}
	s.Signatures = normalizeSignatures(s.Signatures)
	return s
}

func (s ChannelState) ValidateForChannel(channel ChannelRecord, requireAllParticipants bool) error {
	channel = channel.Normalize()
	if err := channel.ValidateCore(); err != nil {
		return err
	}
	state := s.Normalize()
	if err := validateUnsignedStateShape(state); err != nil {
		return err
	}
	if state.ChainID != channel.ChainID {
		return errors.New("payments channel state chain id mismatch")
	}
	if state.ChannelID != channel.ChannelID {
		return errors.New("payments channel state id mismatch")
	}
	if state.ChannelType != channel.ChannelType {
		return errors.New("payments channel state type mismatch")
	}
	if expected := ComputeParticipantSetHash(channel.Participants); state.ParticipantSetHash != expected {
		return errors.New("payments channel state participant set hash mismatch")
	}
	if state.Denom != channel.Denom {
		return errors.New("payments channel state denom mismatch")
	}
	if state.ChallengePeriod != channel.DisputePeriod {
		return errors.New("payments channel state challenge period mismatch")
	}
	if expected := ComputeRequiredSignerBitmap(channel.Participants, channel.RequiredSigners); state.RequiredSignerBitmap != expected {
		return errors.New("payments channel state required signer bitmap mismatch")
	}
	if state.StateHash == "" {
		return errors.New("payments channel state hash is required")
	}
	if expected := ComputeStateHash(state); state.StateHash != expected {
		return errors.New("payments channel state hash mismatch")
	}
	if state.Nonce > 1 && channel.ChannelType != ChannelTypeAsync && state.PreviousStateHash == "" {
		return errors.New("payments channel state previous hash is required")
	}
	if err := validateStateParticipants(state, channel); err != nil {
		return err
	}
	if err := validateCollateralConservation(state, channel); err != nil {
		return err
	}
	required := channel.RequiredSigners
	if requireAllParticipants {
		required = channel.Participants
	}
	return validateSignatureQuorum(state.Signatures, required, state)
}

func ValidatePreviousHashContinuity(channel ChannelRecord, nextState ChannelState) error {
	channel = channel.Normalize()
	nextState = nextState.Normalize()
	if channel.ChannelType == ChannelTypeAsync {
		return nil
	}
	if nextState.Nonce <= 1 {
		return nil
	}
	if nextState.PreviousStateHash != channel.LatestState.StateHash {
		return errors.New("payments channel state previous hash must match latest state")
	}
	return nil
}

func (c ChannelRecord) Normalize() ChannelRecord {
	c.ChainID = strings.TrimSpace(c.ChainID)
	c.ChannelID = normalizeHash(c.ChannelID)
	c.Denom = strings.TrimSpace(c.Denom)
	c.Collateral = strings.TrimSpace(c.Collateral)
	c.OpeningStateHash = normalizeOptionalHash(c.OpeningStateHash)
	c.OpeningFeeDenom = normalizeAssetDenom(c.OpeningFeeDenom)
	c.OpeningFeePaid = strings.TrimSpace(c.OpeningFeePaid)
	c.CustodyDenom = normalizeAssetDenom(c.CustodyDenom)
	c.CustodyAmount = strings.TrimSpace(c.CustodyAmount)
	c.Participants = normalizeAddressSet(c.Participants)
	c.RequiredSigners = normalizeAddressSet(c.RequiredSigners)
	c.Payer = strings.TrimSpace(c.Payer)
	c.Receiver = strings.TrimSpace(c.Receiver)
	if c.ChannelType == ChannelTypeUnidirectional && len(c.Participants) == 2 {
		if c.Payer == "" {
			c.Payer = c.Participants[0]
		}
		if c.Receiver == "" {
			c.Receiver = c.Participants[1]
		}
	}
	if len(c.RequiredSigners) == 0 {
		c.RequiredSigners = append([]string(nil), c.Participants...)
	}
	if c.DisputePeriod == 0 {
		c.DisputePeriod = DefaultDisputePeriod
	}
	if c.CloseDelay == 0 && c.LatestState.CloseDelay != 0 {
		c.CloseDelay = c.LatestState.CloseDelay
	}
	if c.CustodyAmount == "" {
		c.CustodyAmount = c.Collateral
	}
	if c.Status == "" {
		c.Status = ChannelStatusOpen
	}
	c.LatestState = c.LatestState.Normalize()
	c.LatestClaim = c.LatestClaim.Normalize()
	c.PendingClose = c.PendingClose.Normalize()
	if c.Finality == "" {
		c.Finality = DerivedChannelFinality(c)
	}
	return c
}

func DerivedChannelFinality(channel ChannelRecord) ChannelFinality {
	channel.Status = ChannelStatus(strings.TrimSpace(string(channel.Status)))
	channel.PendingClose = channel.PendingClose.Normalize()
	switch channel.Status {
	case ChannelStatusSettled:
		if len(channel.PendingClose.Penalties) > 0 || len(channel.PendingClose.PenaltyAllocations) > 0 {
			return ChannelFinalityPenalized
		}
		return ChannelFinalitySettled
	case ChannelStatusPendingClose:
		if len(channel.PendingClose.Penalties) > 0 || len(channel.PendingClose.PenaltyAllocations) > 0 {
			return ChannelFinalityPenalized
		}
		if len(channel.PendingClose.ConditionProofs) > 0 || len(channel.PendingClose.State.Conditions) > 0 {
			return ChannelFinalityPendingConditionResolution
		}
		if channel.PendingClose.DisputeCount > 0 {
			return ChannelFinalityInDispute
		}
		if channel.PendingClose.CloseReason == CloseReasonTimeout {
			return ChannelFinalityExpired
		}
		return ChannelFinalityPendingClose
	default:
		return ChannelFinalityOpen
	}
}

func FinalityAfterPendingClose(channel ChannelRecord, currentHeight uint64) ChannelFinality {
	channel = channel.Normalize()
	if channel.Status != ChannelStatusPendingClose {
		return channel.Finality
	}
	if channel.Finality == ChannelFinalityPenalized || channel.Finality == ChannelFinalityExpired {
		return channel.Finality
	}
	if currentHeight >= channel.PendingClose.SettleAfterHeight {
		return ChannelFinalityFinalizable
	}
	return channel.Finality
}

func PendingFinalizationHeightForChannel(channel ChannelRecord) (uint64, bool) {
	channel = channel.Normalize()
	if channel.Status != ChannelStatusPendingClose || channel.PendingClose.SettleAfterHeight == 0 {
		return 0, false
	}
	return channel.PendingClose.SettleAfterHeight, true
}

func (c ChannelRecord) ValidateCore() error {
	if strings.TrimSpace(c.ChainID) == "" {
		return errors.New("payments chain id is required")
	}
	if len(c.ChainID) > MaxTokenLength {
		return fmt.Errorf("payments chain id must be <= %d bytes", MaxTokenLength)
	}
	if err := ValidateHash("payments channel id", c.ChannelID); err != nil {
		return err
	}
	if !IsChannelType(c.ChannelType) {
		return fmt.Errorf("unknown payments channel type %q", c.ChannelType)
	}
	if c.Denom != NativeDenom {
		return fmt.Errorf("payments channel collateral denom must be %s", NativeDenom)
	}
	if err := validatePositiveInt("payments channel collateral", c.Collateral); err != nil {
		return err
	}
	if c.OpenHeight == 0 {
		return errors.New("payments channel open height must be positive")
	}
	if err := validateCloseDelay(c.CloseDelay); err != nil {
		return err
	}
	if c.DisputePeriod == 0 {
		return errors.New("payments channel dispute period must be positive")
	}
	if err := validateChallengePeriod(c.DisputePeriod); err != nil {
		return err
	}
	if c.OpeningFeeDenom != NativeDenom {
		return fmt.Errorf("payments opening fee denom must be %s", NativeDenom)
	}
	if err := validateOpeningFeePaid(c.OpeningFeePaid); err != nil {
		return err
	}
	if c.CustodyDenom != NativeDenom {
		return fmt.Errorf("payments custody denom must be %s", NativeDenom)
	}
	if c.CustodyAmount != c.Collateral {
		return errors.New("payments custody amount must match channel collateral")
	}
	if !IsChannelStatus(c.Status) {
		return fmt.Errorf("unknown payments channel status %q", c.Status)
	}
	if !IsChannelFinality(c.Finality) {
		return fmt.Errorf("unknown payments channel finality %q", c.Finality)
	}
	if err := validateChannelFinalityForStatus(c); err != nil {
		return err
	}
	if err := validateAddressSet("payments channel participant", c.Participants, 2, MaxParticipants); err != nil {
		return err
	}
	if err := validateAddressSet("payments channel required signer", c.RequiredSigners, 1, MaxParticipants); err != nil {
		return err
	}
	if c.ChannelType == ChannelTypeUnidirectional {
		if err := validateUnidirectionalChannelCore(c); err != nil {
			return err
		}
	}
	for _, signer := range c.RequiredSigners {
		if !containsString(c.Participants, signer) {
			return errors.New("payments required signer must be a channel participant")
		}
	}
	if c.OpeningStateHash != "" {
		if err := ValidateHash("payments opening state hash", c.OpeningStateHash); err != nil {
			return err
		}
	}
	return nil
}

func (c ChannelRecord) Validate() error {
	channel := c.Normalize()
	if err := channel.ValidateCore(); err != nil {
		return err
	}
	if channel.LatestState.StateHash == "" {
		return errors.New("payments channel latest state is required")
	}
	if err := channel.LatestState.ValidateForChannel(channel, false); err != nil {
		return err
	}
	if channel.LatestState.CloseDelay != channel.CloseDelay {
		return errors.New("payments opening state close delay mismatch")
	}
	if channel.LatestState.FeePolicyID != NativeDenom {
		return fmt.Errorf("payments opening state fee policy must be %s", NativeDenom)
	}
	if channel.OpeningStateHash == "" {
		return errors.New("payments opening state hash is required")
	}
	if channel.ChannelType == ChannelTypeUnidirectional {
		if err := validateUnidirectionalOpeningState(channel); err != nil {
			return err
		}
		if !channel.LatestClaim.IsZero() {
			if err := channel.LatestClaim.ValidateForChannel(channel); err != nil {
				return err
			}
		}
	}
	if channel.FinalizedNonce > channel.LatestState.Nonce {
		if channel.ChannelType != ChannelTypeUnidirectional || channel.LatestClaim.IsZero() || channel.FinalizedNonce > channel.LatestClaim.Nonce {
			return errors.New("payments finalized nonce cannot exceed latest state nonce")
		}
	}
	switch channel.Status {
	case ChannelStatusOpen:
		if channel.PendingClose.State.StateHash != "" {
			return errors.New("payments open channel must not have pending close")
		}
	case ChannelStatusPendingClose:
		if err := channel.PendingClose.ValidateForChannel(channel); err != nil {
			return err
		}
		if channel.DisputedNonce < channel.PendingClose.State.Nonce {
			return errors.New("payments disputed nonce cannot be below pending close nonce")
		}
	case ChannelStatusSettled:
		if channel.PendingClose.State.StateHash != "" {
			return errors.New("payments settled channel must not have pending close")
		}
	}
	return nil
}

func IsChannelType(value ChannelType) bool {
	switch value {
	case ChannelTypeBidirectional, ChannelTypeUnidirectional, ChannelTypeAsync:
		return true
	default:
		return false
	}
}

func IsChannelStatus(value ChannelStatus) bool {
	switch value {
	case ChannelStatusOpen, ChannelStatusPendingClose, ChannelStatusSettled:
		return true
	default:
		return false
	}
}

func IsChannelFinality(value ChannelFinality) bool {
	switch value {
	case ChannelFinalityOpen,
		ChannelFinalityPendingClose,
		ChannelFinalityInDispute,
		ChannelFinalityPendingConditionResolution,
		ChannelFinalityFinalizable,
		ChannelFinalitySettled,
		ChannelFinalityPenalized,
		ChannelFinalityExpired:
		return true
	default:
		return false
	}
}

func IsCloseReason(value CloseReason) bool {
	switch value {
	case CloseReasonUnilateral, CloseReasonCooperative, CloseReasonTimeout, CloseReasonFraud:
		return true
	default:
		return false
	}
}

func validateUnsignedStateShape(state ChannelState) error {
	if strings.TrimSpace(state.ChainID) == "" {
		return errors.New("payments channel state chain id is required")
	}
	if len(state.ChainID) > MaxTokenLength {
		return fmt.Errorf("payments channel state chain id must be <= %d bytes", MaxTokenLength)
	}
	if state.AppVersion != CurrentAppVersion {
		return fmt.Errorf("payments channel state app version must be %d", CurrentAppVersion)
	}
	if state.ModuleName != ModuleName {
		return fmt.Errorf("payments channel state module name must be %s", ModuleName)
	}
	if err := validateRequiredFields(state.RequiredFields); err != nil {
		return err
	}
	if err := ValidateHash("payments channel state channel id", state.ChannelID); err != nil {
		return err
	}
	if !IsChannelType(state.ChannelType) {
		return fmt.Errorf("unknown payments channel state type %q", state.ChannelType)
	}
	if err := ValidateHash("payments channel state participant set hash", state.ParticipantSetHash); err != nil {
		return err
	}
	if state.Denom != NativeDenom {
		return fmt.Errorf("payments channel state denom must be %s", NativeDenom)
	}
	if state.Version != CurrentStateVersion {
		return fmt.Errorf("payments channel state version must be %d", CurrentStateVersion)
	}
	if err := validateNonNegativeInt("payments channel state accrued fees", state.AccruedFees); err != nil {
		return err
	}
	if state.Epoch == 0 {
		return errors.New("payments channel state epoch must be positive")
	}
	if state.Nonce == 0 {
		return errors.New("payments channel state nonce must be positive")
	}
	if state.PreviousStateHash != "" {
		if err := ValidateHash("payments channel state previous hash", state.PreviousStateHash); err != nil {
			return err
		}
	}
	if err := ValidateHash("payments pending conditions root", state.PendingConditionsRoot); err != nil {
		return err
	}
	if err := ValidateHash("payments condition root", state.ConditionRoot); err != nil {
		return err
	}
	if state.ConditionRoot != state.PendingConditionsRoot {
		return errors.New("payments condition root must match pending conditions root")
	}
	if expected := ComputeConditionsRoot(state.Conditions); state.ConditionRoot != expected {
		return errors.New("payments pending conditions root mismatch")
	}
	if state.ConditionCount != uint32(len(state.Conditions)) {
		return errors.New("payments condition count mismatch")
	}
	if state.TimeoutTimestamp < 0 {
		return errors.New("payments channel state timeout timestamp must be non-negative")
	}
	if state.ChallengePeriod == 0 {
		return errors.New("payments channel state challenge period must be positive")
	}
	if err := validateChallengePeriod(state.ChallengePeriod); err != nil {
		return err
	}
	if state.FeePolicyID != NativeDenom {
		return fmt.Errorf("payments channel state fee policy must be %s", NativeDenom)
	}
	if err := validateRequiredSignerBitmap(state.RequiredSignerBitmap); err != nil {
		return err
	}
	if state.SignatureScheme != SignatureSchemeEd25519 {
		return fmt.Errorf("payments channel state signature scheme must be %s", SignatureSchemeEd25519)
	}
	if err := ValidateHash("payments channel state signature preimage hash", state.SignaturePreimageHash); err != nil {
		return err
	}
	if expected := ComputeStateSignaturePreimageHash(state); state.SignaturePreimageHash != expected {
		return errors.New("payments channel state signature preimage hash mismatch")
	}
	if err := validateBalances(state.Balances); err != nil {
		return err
	}
	return validateConditions(state.Conditions)
}

func openingStateForRequest(req ChannelOpenRequest, channel ChannelRecord) ChannelState {
	channel = channel.Normalize()
	state := ChannelState{
		ChainID:              req.ChainID,
		AppVersion:           CurrentAppVersion,
		ModuleName:           ModuleName,
		ChannelID:            req.ChannelID,
		ChannelType:          req.ChannelType,
		ParticipantSetHash:   ComputeParticipantSetHash(channel.Participants),
		Denom:                NativeDenom,
		Version:              CurrentStateVersion,
		Epoch:                1,
		Nonce:                1,
		Balances:             req.InitialBalances,
		TimeoutHeight:        req.OpenHeight + req.ChallengePeriod,
		ChallengePeriod:      req.ChallengePeriod,
		CloseDelay:           req.CloseDelay,
		FeePolicyID:          req.FeePolicyID,
		RequiredSignerBitmap: ComputeRequiredSignerBitmap(channel.Participants, channel.RequiredSigners),
		SignatureScheme:      SignatureSchemeEd25519,
	}
	if req.ChannelType == ChannelTypeUnidirectional {
		state.TimeoutHeight = req.ExpirationHeight
		state.TimeoutTimestamp = req.ExpirationTimestamp
	}
	if req.ChannelType == ChannelTypeAsync {
		state.CheckpointNonce = 1
		state.CheckpointBalances = req.InitialBalances
		state.AsyncUpdateRoot = ComputeAsyncDeltaRootForChannel(channel, nil)
		state.AcceptedUpdateRoot = ComputeAsyncDeltaRootForChannel(channel, nil)
		state.SendWindow = req.CloseDelay
		state.ReceiveWindow = req.ChallengePeriod
		state.MaxUnackedAmount = req.Collateral
		state.ExpiryHeight = req.ExpirationHeight
		state.TimeoutHeight = req.ExpirationHeight
	}
	if channel.ChannelType == ChannelTypeBidirectional && len(req.InitialBalances) == 2 {
		state.ParticipantA = channel.Normalize().Participants[0]
		state.ParticipantB = channel.Normalize().Participants[1]
	}
	return state
}

func validateInitialBalances(balances []Balance, participants []string, collateralText string) error {
	if len(balances) != len(participants) {
		return errors.New("payments initial balances must include every participant")
	}
	for _, balance := range normalizeBalances(balances) {
		if !containsString(participants, balance.Participant) {
			return errors.New("payments initial balance participant must be in channel")
		}
	}
	total, err := sumBalances(balances)
	if err != nil {
		return err
	}
	collateral, err := parsePositiveInt("payments open collateral", collateralText)
	if err != nil {
		return err
	}
	if !total.Equal(collateral) {
		return errors.New("payments initial balances must sum to collateral")
	}
	return nil
}

func validateCloseDelay(closeDelay uint64) error {
	if closeDelay < MinCloseDelay || closeDelay > MaxCloseDelay {
		return fmt.Errorf("payments close delay must be between %d and %d", MinCloseDelay, MaxCloseDelay)
	}
	return nil
}

func validateChallengePeriod(period uint64) error {
	if period < MinChallengePeriod || period > MaxChallengePeriod {
		return fmt.Errorf("payments challenge period must be between %d and %d", MinChallengePeriod, MaxChallengePeriod)
	}
	return nil
}

func validateUpdateExposure(state ChannelState) error {
	state = state.Normalize()
	if state.ChannelType != ChannelTypeBidirectional || len(state.Conditions) == 0 {
		return nil
	}
	conditionTotal, err := sumConditions(state.Conditions)
	if err != nil {
		return err
	}
	reserveA, err := parseNonNegativeInt("payments update reserve a", state.ReserveA)
	if err != nil {
		return err
	}
	reserveB, err := parseNonNegativeInt("payments update reserve b", state.ReserveB)
	if err != nil {
		return err
	}
	if conditionTotal.GT(reserveA.Add(reserveB)) {
		return errors.New("payments update conditions exceed reserve limits")
	}
	return nil
}

func validateCloseReason(reason CloseReason) error {
	if !IsCloseReason(reason) {
		return fmt.Errorf("unknown payments close reason %q", reason)
	}
	return nil
}

func validateChannelFinalityForStatus(channel ChannelRecord) error {
	switch channel.Status {
	case ChannelStatusOpen:
		if channel.Finality != ChannelFinalityOpen && channel.Finality != ChannelFinalityExpired {
			return errors.New("payments open channel finality must be open or expired")
		}
	case ChannelStatusPendingClose:
		switch channel.Finality {
		case ChannelFinalityPendingClose,
			ChannelFinalityInDispute,
			ChannelFinalityPendingConditionResolution,
			ChannelFinalityFinalizable,
			ChannelFinalityPenalized,
			ChannelFinalityExpired:
			return nil
		default:
			return errors.New("payments pending close channel has invalid finality")
		}
	case ChannelStatusSettled:
		if channel.Finality != ChannelFinalitySettled && channel.Finality != ChannelFinalityPenalized {
			return errors.New("payments settled channel finality must be settled or penalized")
		}
	}
	return nil
}

func validateStateParticipants(state ChannelState, channel ChannelRecord) error {
	if channel.ChannelType == ChannelTypeBidirectional {
		if err := validateBidirectionalProjection(state, channel); err != nil {
			return err
		}
	}
	if channel.ChannelType == ChannelTypeAsync {
		if err := validateAsyncStateProjection(state, channel); err != nil {
			return err
		}
	}
	for _, balance := range state.Balances {
		if !containsString(channel.Participants, balance.Participant) {
			return errors.New("payments balance participant must be in channel")
		}
	}
	for _, condition := range state.Conditions {
		if !containsString(channel.Participants, condition.Payer) || !containsString(channel.Participants, condition.Payee) {
			return errors.New("payments condition parties must be in channel")
		}
	}
	return nil
}

func validateBidirectionalProjection(state ChannelState, channel ChannelRecord) error {
	if len(channel.Participants) != 2 {
		return errors.New("payments bidirectional channel requires exactly two participants")
	}
	if len(state.Balances) != 2 {
		return errors.New("payments bidirectional state requires exactly two balances")
	}
	if state.ParticipantA == "" || state.ParticipantB == "" {
		return errors.New("payments bidirectional state participants are required")
	}
	if state.ParticipantA == state.ParticipantB {
		return errors.New("payments bidirectional state participants must differ")
	}
	if state.ParticipantA != channel.Participants[0] || state.ParticipantB != channel.Participants[1] {
		return errors.New("payments bidirectional state participants must match canonical channel order")
	}
	if state.TimeoutHeight == 0 {
		return errors.New("payments bidirectional state timeout height must be positive")
	}
	if state.CloseDelay == 0 {
		return errors.New("payments bidirectional state close delay must be positive")
	}
	balanceByParticipant := map[string]string{}
	for _, balance := range state.Balances {
		balanceByParticipant[balance.Participant] = balance.Amount
	}
	if balanceByParticipant[state.ParticipantA] != state.BalanceA || balanceByParticipant[state.ParticipantB] != state.BalanceB {
		return errors.New("payments bidirectional state balance projection mismatch")
	}
	if err := validateNonNegativeInt("payments bidirectional reserve a", state.ReserveA); err != nil {
		return err
	}
	return validateNonNegativeInt("payments bidirectional reserve b", state.ReserveB)
}

func validateCollateralConservation(state ChannelState, channel ChannelRecord) error {
	collateral, err := parsePositiveInt("payments channel collateral", channel.Collateral)
	if err != nil {
		return err
	}
	if channel.ChannelType == ChannelTypeBidirectional {
		balanceA, err := parseNonNegativeInt("payments bidirectional balance a", state.BalanceA)
		if err != nil {
			return err
		}
		balanceB, err := parseNonNegativeInt("payments bidirectional balance b", state.BalanceB)
		if err != nil {
			return err
		}
		reserveA, err := parseNonNegativeInt("payments bidirectional reserve a", state.ReserveA)
		if err != nil {
			return err
		}
		reserveB, err := parseNonNegativeInt("payments bidirectional reserve b", state.ReserveB)
		if err != nil {
			return err
		}
		total := balanceA.Add(balanceB).Add(reserveA).Add(reserveB)
		if !total.Equal(collateral) {
			return errors.New("payments channel state must conserve collateral")
		}
		return nil
	}
	total, err := sumBalances(state.Balances)
	if err != nil {
		return err
	}
	conditionTotal, err := sumConditions(state.Conditions)
	if err != nil {
		return err
	}
	if !total.Add(conditionTotal).Equal(collateral) {
		return errors.New("payments channel state must conserve collateral")
	}
	return nil
}

func validateBalances(balances []Balance) error {
	if len(balances) == 0 || len(balances) > MaxParticipants {
		return fmt.Errorf("payments balances must be between 1 and %d", MaxParticipants)
	}
	var previous string
	seen := make(map[string]struct{}, len(balances))
	for i, balance := range balances {
		if err := addressing.ValidateUserAddress("payments balance participant", balance.Participant); err != nil {
			return err
		}
		if err := validateNonNegativeInt("payments balance amount", balance.Amount); err != nil {
			return err
		}
		if _, found := seen[balance.Participant]; found {
			return errors.New("payments duplicate balance participant")
		}
		seen[balance.Participant] = struct{}{}
		if i > 0 && previous >= balance.Participant {
			return errors.New("payments balances must be sorted canonically")
		}
		previous = balance.Participant
	}
	return nil
}

func sumBalances(balances []Balance) (sdkmath.Int, error) {
	total := sdkmath.ZeroInt()
	for _, balance := range balances {
		amount, err := parseNonNegativeInt("payments balance amount", balance.Amount)
		if err != nil {
			return sdkmath.Int{}, err
		}
		total = total.Add(amount)
	}
	return total, nil
}

func normalizeBalances(balances []Balance) []Balance {
	out := make([]Balance, len(balances))
	for i, balance := range balances {
		out[i] = Balance{
			Participant: strings.TrimSpace(balance.Participant),
			Amount:      strings.TrimSpace(balance.Amount),
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Participant < out[j].Participant
	})
	return out
}

func participantsFromBalances(balances []Balance) []string {
	participants := make([]string, 0, len(balances))
	for _, balance := range normalizeBalances(balances) {
		if balance.Participant != "" {
			participants = append(participants, balance.Participant)
		}
	}
	return normalizeAddressSet(participants)
}

func sameBalances(left, right []Balance) bool {
	left = normalizeBalances(left)
	right = normalizeBalances(right)
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func channelCounterparty(channel ChannelRecord, offender string) string {
	channel = channel.Normalize()
	offender = strings.TrimSpace(offender)
	for _, participant := range channel.Participants {
		if participant != offender {
			return participant
		}
	}
	return ""
}
