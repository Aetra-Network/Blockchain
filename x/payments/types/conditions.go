package types

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	sdkmath "cosmossdk.io/math"

	"github.com/sovereign-l1/l1/app/addressing"
)

const (
	ChannelFinalityOpen                       ChannelFinality = "OPEN"
	ChannelFinalityPendingClose               ChannelFinality = "PENDING_CLOSE"
	ChannelFinalityInDispute                  ChannelFinality = "IN_DISPUTE"
	ChannelFinalityPendingConditionResolution ChannelFinality = "PENDING_CONDITION_RESOLUTION"
	ChannelFinalityFinalizable                ChannelFinality = "FINALIZABLE"
	ChannelFinalitySettled                    ChannelFinality = "SETTLED"
	ChannelFinalityPenalized                  ChannelFinality = "PENALIZED"
	ChannelFinalityExpired                    ChannelFinality = "EXPIRED"
)

type ConditionType string

const (
	ConditionTypeHashLock ConditionType = "HASH_LOCK"
	ConditionTypeTimeLock ConditionType = "TIME_LOCK"
)

type ConditionSettlementMode string

const (
	ConditionSettlementModePreimage ConditionSettlementMode = "PREIMAGE"
	ConditionSettlementModeExpiry   ConditionSettlementMode = "EXPIRY"
)

type ConditionalPayment struct {
	ConditionID   string
	ConditionType ConditionType
	Payer         string
	Payee         string
	Amount        string
	HashLock      string
	TimeoutHeight uint64
	NonceStart    uint64
	NonceEnd      uint64
}

type ConditionalPromise struct {
	PromiseID                 string
	ChannelID                 string
	Source                    string
	Destination               string
	Amount                    string
	Fee                       string
	HashLock                  string
	TimeoutHeight             uint64
	TimeoutTimestamp          int64
	ConditionType             ConditionType
	RouteIDOptional           string
	PreviousPromiseIDOptional string
	NextPromiseIDOptional     string
	Nonce                     uint64
	PromiseHash               string
	Signature                 PromiseSignature
}

type PromiseSignature struct {
	Signer           string
	ChainID          string
	ChannelID        string
	ObjectType       string
	Version          uint32
	Nonce            uint64
	ObjectID         string
	ExpirationHeight uint64
	CommitmentHash   string
	PromiseHash      string
	SignatureHash    string
}

type ConditionResolution struct {
	ConditionID  string
	Resolver     string
	Recipient    string
	Amount       string
	Expired      bool
	EvidenceHash string
}

type ConditionClaimRecord struct {
	ChainID        string
	ChannelID      string
	ConditionID    string
	EvidenceHash   string
	PreimageHash   string
	ResolvedHeight uint64
	ExpiresHeight  uint64
}

type PreimageRevealRequest struct {
	ChannelID     string
	Promises      []ConditionalPromise
	Preimage      string
	Revealer      string
	CurrentHeight uint64
}

type PromiseExpiryRequest struct {
	ChannelID     string
	Promises      []ConditionalPromise
	Resolver      string
	CurrentHeight uint64
}

type ConditionLinkageProof struct {
	RouteID                    string
	Promises                   []ConditionalPromise
	Sender                     string
	Receiver                   string
	Amount                     string
	TotalFees                  string
	HashLock                   string
	TimeoutMargin              uint64
	PartialDispute             bool
	OffchainResolvedPromiseIDs []string
	EvidenceHash               string
}

type BatchConditionSettlementRequest struct {
	LinkageProof      ConditionLinkageProof
	Mode              ConditionSettlementMode
	Preimage          string
	Resolver          string
	CurrentHeight     uint64
	SettlementFeePaid string
}

type BatchConditionSettlementResult struct {
	RouteID              string
	Resolutions          []ConditionResolution
	FeeClaims            []RouteFeeClaim
	ConditionRootUpdates []ConditionRootUpdate
	EvidenceHash         string
}

type ConditionRootUpdate struct {
	ChannelID      string
	Nonce          uint64
	ConditionRoot  string
	ConditionCount uint32
	Conditions     []ConditionalPayment
}

func BuildConditionalPromise(promise ConditionalPromise) (ConditionalPromise, error) {
	promise = promise.Normalize()
	if err := promise.ValidateBasic(); err != nil {
		return ConditionalPromise{}, err
	}
	promise.PromiseHash = ComputeConditionalTransferPromiseHash(promise)
	return promise, nil
}

func SignatureForPromise(channel ChannelRecord, promise ConditionalPromise, signer string) (PromiseSignature, error) {
	channel = channel.Normalize()
	if promise.PromiseHash == "" {
		var err error
		promise, err = BuildConditionalPromise(promise)
		if err != nil {
			return PromiseSignature{}, err
		}
	}
	signer = strings.TrimSpace(signer)
	if err := addressing.ValidateUserAddress("payments promise signer", signer); err != nil {
		return PromiseSignature{}, err
	}
	return PromiseSignature{
		Signer:           signer,
		ChainID:          channel.ChainID,
		ChannelID:        promise.ChannelID,
		ObjectType:       SignatureObjectPromise,
		Version:          CurrentStateVersion,
		Nonce:            promise.Nonce,
		ObjectID:         promise.PromiseHash,
		ExpirationHeight: promise.TimeoutHeight,
		CommitmentHash:   promise.PromiseHash,
		PromiseHash:      promise.PromiseHash,
		SignatureHash: ComputeSignatureEnvelopeHash(
			signer,
			channel.ChainID,
			promise.ChannelID,
			SignatureObjectPromise,
			CurrentStateVersion,
			promise.Nonce,
			promise.PromiseHash,
			promise.TimeoutHeight,
			promise.PromiseHash,
		),
	}, nil
}

func (p ConditionalPromise) Normalize() ConditionalPromise {
	p.PromiseID = normalizeHash(p.PromiseID)
	p.ChannelID = normalizeHash(p.ChannelID)
	p.Source = strings.TrimSpace(p.Source)
	p.Destination = strings.TrimSpace(p.Destination)
	p.Amount = strings.TrimSpace(p.Amount)
	p.Fee = strings.TrimSpace(p.Fee)
	p.HashLock = normalizeOptionalHash(p.HashLock)
	p.RouteIDOptional = normalizeOptionalHash(p.RouteIDOptional)
	p.PreviousPromiseIDOptional = normalizeOptionalHash(p.PreviousPromiseIDOptional)
	p.NextPromiseIDOptional = normalizeOptionalHash(p.NextPromiseIDOptional)
	p.PromiseHash = normalizeOptionalHash(p.PromiseHash)
	p.Signature = p.Signature.Normalize()
	return p
}

func (p ConditionalPromise) ValidateBasic() error {
	promise := p.Normalize()
	if err := ValidateHash("payments promise id", promise.PromiseID); err != nil {
		return err
	}
	if err := ValidateHash("payments promise channel id", promise.ChannelID); err != nil {
		return err
	}
	if err := addressing.ValidateUserAddress("payments promise source", promise.Source); err != nil {
		return err
	}
	if err := addressing.ValidateUserAddress("payments promise destination", promise.Destination); err != nil {
		return err
	}
	if promise.Source == promise.Destination {
		return errors.New("payments promise parties must differ")
	}
	if err := validatePositiveInt("payments promise amount", promise.Amount); err != nil {
		return err
	}
	if err := validateNonNegativeInt("payments promise fee", promise.Fee); err != nil {
		return err
	}
	if promise.TimeoutHeight == 0 {
		return errors.New("payments promise timeout height must be positive")
	}
	if promise.TimeoutTimestamp < 0 {
		return errors.New("payments promise timeout timestamp must be non-negative")
	}
	if promise.Nonce == 0 {
		return errors.New("payments promise nonce must be positive")
	}
	if !IsConditionType(promise.ConditionType) {
		return fmt.Errorf("unknown payments promise condition type %q", promise.ConditionType)
	}
	if promise.ConditionType == ConditionTypeHashLock {
		if err := ValidateHash("payments promise hash lock", promise.HashLock); err != nil {
			return err
		}
	} else if promise.HashLock != "" {
		return errors.New("payments time-lock promise must not include hash lock")
	}
	return nil
}

func (p ConditionalPromise) ValidateForChannel(channel ChannelRecord) error {
	promise := p.Normalize()
	channel = channel.Normalize()
	if err := promise.ValidateBasic(); err != nil {
		return err
	}
	if promise.ChannelID != channel.ChannelID {
		return errors.New("payments promise channel mismatch")
	}
	if !containsString(channel.Participants, promise.Source) || !containsString(channel.Participants, promise.Destination) {
		return errors.New("payments promise parties must be channel participants")
	}
	if !channel.ConditionalPayments {
		return errors.New("payments channel does not support conditional promises")
	}
	if err := validatePromiseTimeoutWindow(channel, promise); err != nil {
		return err
	}
	if promise.PromiseHash == "" {
		return errors.New("payments promise hash is required")
	}
	if expected := ComputeConditionalTransferPromiseHash(promise); promise.PromiseHash != expected {
		return errors.New("payments promise hash mismatch")
	}
	if err := promise.Signature.Validate(promise.PromiseHash); err != nil {
		return err
	}
	return validatePromiseSignatureEnvelope(channel, promise.Signature, promise)
}

func (p ConditionalPromise) ToConditionalPayment() ConditionalPayment {
	promise := p.Normalize()
	return ConditionalPayment{
		ConditionID:   promise.PromiseID,
		ConditionType: promise.ConditionType,
		Payer:         promise.Source,
		Payee:         promise.Destination,
		Amount:        promise.Amount,
		HashLock:      promise.HashLock,
		TimeoutHeight: promise.TimeoutHeight,
		NonceStart:    promise.Nonce,
		NonceEnd:      promise.Nonce,
	}.Normalize()
}

func (s PromiseSignature) Normalize() PromiseSignature {
	s.Signer = strings.TrimSpace(s.Signer)
	s.ChainID = strings.TrimSpace(s.ChainID)
	s.ChannelID = normalizeHash(s.ChannelID)
	s.ObjectType = strings.TrimSpace(s.ObjectType)
	s.ObjectID = normalizeOptionalHash(s.ObjectID)
	s.CommitmentHash = normalizeOptionalHash(s.CommitmentHash)
	s.PromiseHash = normalizeOptionalHash(s.PromiseHash)
	s.SignatureHash = normalizeOptionalHash(s.SignatureHash)
	return s
}

func (s PromiseSignature) Validate(expectedPromiseHash string) error {
	s = s.Normalize()
	if err := addressing.ValidateUserAddress("payments promise signature signer", s.Signer); err != nil {
		return err
	}
	if s.PromiseHash != expectedPromiseHash {
		return errors.New("payments promise signature hash mismatch")
	}
	if s.ObjectType != SignatureObjectPromise {
		return errors.New("payments promise signature object type mismatch")
	}
	if s.ObjectID != s.PromiseHash {
		return errors.New("payments promise signature object id mismatch")
	}
	if s.CommitmentHash != s.PromiseHash {
		return errors.New("payments promise signature commitment mismatch")
	}
	if err := ValidateHash("payments promise signature hash", s.SignatureHash); err != nil {
		return err
	}
	if expected := ComputeSignatureEnvelopeHash(s.Signer, s.ChainID, s.ChannelID, s.ObjectType, s.Version, s.Nonce, s.ObjectID, s.ExpirationHeight, s.CommitmentHash); s.SignatureHash != expected {
		return errors.New("payments promise signature hash mismatch")
	}
	return nil
}

func (r ConditionResolution) Normalize() ConditionResolution {
	r.ConditionID = normalizeHash(r.ConditionID)
	r.Resolver = strings.TrimSpace(r.Resolver)
	r.Recipient = strings.TrimSpace(r.Recipient)
	r.Amount = strings.TrimSpace(r.Amount)
	r.EvidenceHash = normalizeHash(r.EvidenceHash)
	return r
}

func (r ConditionResolution) ValidateForCondition(condition ConditionalPayment, channel ChannelRecord) error {
	resolution := r.Normalize()
	condition = condition.Normalize()
	if resolution.ConditionID != condition.ConditionID {
		return errors.New("payments condition resolution id mismatch")
	}
	if err := addressing.ValidateUserAddress("payments condition resolver", resolution.Resolver); err != nil {
		return err
	}
	if !containsString(channel.Participants, resolution.Resolver) {
		return errors.New("payments condition resolver must be participant")
	}
	if err := addressing.ValidateUserAddress("payments condition resolution recipient", resolution.Recipient); err != nil {
		return err
	}
	if resolution.Recipient != condition.Payer && resolution.Recipient != condition.Payee {
		return errors.New("payments condition resolution recipient must be condition party")
	}
	if resolution.Expired && resolution.Recipient != condition.Payer {
		return errors.New("payments expired condition must return to payer")
	}
	if !resolution.Expired && resolution.Recipient != condition.Payee {
		return errors.New("payments resolved condition must pay payee")
	}
	amount, err := parsePositiveInt("payments condition resolution amount", resolution.Amount)
	if err != nil {
		return err
	}
	conditionAmount, err := parsePositiveInt("payments condition amount", condition.Amount)
	if err != nil {
		return err
	}
	if !amount.Equal(conditionAmount) {
		return errors.New("payments condition resolution amount mismatch")
	}
	return ValidateHash("payments condition resolution evidence hash", resolution.EvidenceHash)
}

func (r ConditionClaimRecord) Normalize() ConditionClaimRecord {
	r.ChainID = strings.TrimSpace(r.ChainID)
	r.ChannelID = normalizeHash(r.ChannelID)
	r.ConditionID = normalizeHash(r.ConditionID)
	r.EvidenceHash = normalizeHash(r.EvidenceHash)
	r.PreimageHash = normalizeOptionalHash(r.PreimageHash)
	return r
}

func (r ConditionClaimRecord) Validate() error {
	r = r.Normalize()
	if strings.TrimSpace(r.ChainID) == "" {
		return errors.New("payments condition claim chain id is required")
	}
	if err := ValidateHash("payments condition claim channel id", r.ChannelID); err != nil {
		return err
	}
	if err := ValidateHash("payments condition claim condition id", r.ConditionID); err != nil {
		return err
	}
	if err := ValidateHash("payments condition claim evidence hash", r.EvidenceHash); err != nil {
		return err
	}
	if r.PreimageHash != "" {
		if err := ValidateHash("payments condition claim preimage hash", r.PreimageHash); err != nil {
			return err
		}
	}
	if r.ResolvedHeight == 0 {
		return errors.New("payments condition claim resolved height must be positive")
	}
	if r.ExpiresHeight <= r.ResolvedHeight {
		return errors.New("payments condition claim replay horizon must exceed resolution height")
	}
	return nil
}

func (r PreimageRevealRequest) Normalize() PreimageRevealRequest {
	r.ChannelID = normalizeHash(r.ChannelID)
	r.Promises = normalizeConditionalPromises(r.Promises)
	r.Preimage = strings.TrimSpace(r.Preimage)
	r.Revealer = strings.TrimSpace(r.Revealer)
	return r
}

func (r PreimageRevealRequest) ValidateForChannel(channel ChannelRecord, settledClaims []ConditionClaimRecord) error {
	req := r.Normalize()
	channel = channel.Normalize()
	if req.ChannelID != channel.ChannelID {
		return errors.New("payments preimage reveal channel mismatch")
	}
	if err := addressing.ValidateUserAddress("payments preimage revealer", req.Revealer); err != nil {
		return err
	}
	if !containsString(channel.Participants, req.Revealer) {
		return errors.New("payments preimage revealer must be channel participant")
	}
	if req.CurrentHeight == 0 {
		return errors.New("payments preimage reveal height must be positive")
	}
	if req.Preimage == "" {
		return errors.New("payments preimage reveal preimage is required")
	}
	if len(req.Promises) == 0 {
		return errors.New("payments preimage reveal requires promises")
	}
	var hashLock string
	seen := make(map[string]struct{}, len(req.Promises))
	for _, promise := range req.Promises {
		if err := promise.ValidateForChannel(channel); err != nil {
			return err
		}
		if _, found := seen[promise.PromiseID]; found {
			return errors.New("payments duplicate promise id")
		}
		seen[promise.PromiseID] = struct{}{}
		if promise.ConditionType != ConditionTypeHashLock {
			return errors.New("payments preimage reveal requires hash-lock promises")
		}
		if req.CurrentHeight > promise.TimeoutHeight {
			return errors.New("payments preimage reveal promise has timed out")
		}
		if err := VerifyPromisePreimage(promise, req.Preimage); err != nil {
			return err
		}
		if promiseWasSettled(channel, promise.PromiseID, settledClaims) {
			return errors.New("payments promise has already been settled")
		}
		if hashLock == "" {
			hashLock = promise.HashLock
		} else if promise.HashLock != hashLock {
			return errors.New("payments linked promises must use compatible hash locks")
		}
	}
	return nil
}

func (r PromiseExpiryRequest) Normalize() PromiseExpiryRequest {
	r.ChannelID = normalizeHash(r.ChannelID)
	r.Promises = normalizeConditionalPromises(r.Promises)
	r.Resolver = strings.TrimSpace(r.Resolver)
	return r
}

func (r PromiseExpiryRequest) ValidateForChannel(channel ChannelRecord, settledClaims []ConditionClaimRecord) error {
	req := r.Normalize()
	channel = channel.Normalize()
	if req.ChannelID != channel.ChannelID {
		return errors.New("payments promise expiry channel mismatch")
	}
	if err := addressing.ValidateUserAddress("payments promise expiry resolver", req.Resolver); err != nil {
		return err
	}
	if !containsString(channel.Participants, req.Resolver) {
		return errors.New("payments promise expiry resolver must be channel participant")
	}
	if req.CurrentHeight == 0 {
		return errors.New("payments promise expiry height must be positive")
	}
	if len(req.Promises) == 0 {
		return errors.New("payments promise expiry requires promises")
	}
	seen := make(map[string]struct{}, len(req.Promises))
	for _, promise := range req.Promises {
		if err := promise.ValidateForChannel(channel); err != nil {
			return err
		}
		if _, found := seen[promise.PromiseID]; found {
			return errors.New("payments duplicate promise id")
		}
		seen[promise.PromiseID] = struct{}{}
		if req.CurrentHeight <= promise.TimeoutHeight {
			return errors.New("payments promise has not expired")
		}
		if promiseWasSettled(channel, promise.PromiseID, settledClaims) {
			return errors.New("payments promise has already been settled")
		}
	}
	return nil
}

func (p ConditionLinkageProof) Normalize() ConditionLinkageProof {
	p.RouteID = normalizeHash(p.RouteID)
	p.Promises = normalizePromiseRoute(p.Promises)
	p.Sender = strings.TrimSpace(p.Sender)
	p.Receiver = strings.TrimSpace(p.Receiver)
	p.Amount = strings.TrimSpace(p.Amount)
	p.TotalFees = strings.TrimSpace(p.TotalFees)
	p.HashLock = normalizeHash(p.HashLock)
	p.EvidenceHash = normalizeOptionalHash(p.EvidenceHash)
	for i, id := range p.OffchainResolvedPromiseIDs {
		p.OffchainResolvedPromiseIDs[i] = normalizeHash(id)
	}
	sort.Strings(p.OffchainResolvedPromiseIDs)
	return p
}

func (p ConditionLinkageProof) ValidateForState(state PaymentsState, settledClaims []ConditionClaimRecord) error {
	proof := p.Normalize()
	state = state.Export()
	if err := ValidateHash("payments condition linkage route id", proof.RouteID); err != nil {
		return err
	}
	if err := addressing.ValidateUserAddress("payments condition linkage sender", proof.Sender); err != nil {
		return err
	}
	if err := addressing.ValidateUserAddress("payments condition linkage receiver", proof.Receiver); err != nil {
		return err
	}
	if proof.Sender == proof.Receiver {
		return errors.New("payments condition linkage endpoints must differ")
	}
	if err := validatePositiveInt("payments condition linkage amount", proof.Amount); err != nil {
		return err
	}
	if err := validateNonNegativeInt("payments condition linkage total fees", proof.TotalFees); err != nil {
		return err
	}
	if err := ValidateHash("payments condition linkage hash lock", proof.HashLock); err != nil {
		return err
	}
	if proof.EvidenceHash != "" {
		if err := ValidateHash("payments condition linkage evidence hash", proof.EvidenceHash); err != nil {
			return err
		}
	}
	if len(proof.Promises) == 0 {
		return errors.New("payments condition linkage requires promises")
	}
	if len(proof.Promises) < 2 && !proof.PartialDispute {
		return errors.New("payments condition linkage requires at least two promises")
	}
	if proof.TimeoutMargin == 0 {
		proof.TimeoutMargin = DefaultTimeoutMargin
	}
	seen := make(map[string]struct{}, len(proof.Promises)+len(proof.OffchainResolvedPromiseIDs))
	for _, id := range proof.OffchainResolvedPromiseIDs {
		if err := ValidateHash("payments offchain resolved promise id", id); err != nil {
			return err
		}
		if _, found := seen[id]; found {
			return errors.New("payments duplicate offchain resolved promise id")
		}
		seen[id] = struct{}{}
	}
	if !proof.PartialDispute && len(proof.OffchainResolvedPromiseIDs) > 0 {
		return errors.New("payments offchain resolved promises require partial dispute proof")
	}
	if proof.PartialDispute && len(proof.OffchainResolvedPromiseIDs) == 0 {
		return errors.New("payments partial dispute requires offchain resolved promise ids")
	}
	channels := make([]ChannelRecord, len(proof.Promises))
	for i, promise := range proof.Promises {
		if _, found := seen[promise.PromiseID]; found {
			return errors.New("payments duplicate linked promise id")
		}
		seen[promise.PromiseID] = struct{}{}
		channel, found := state.ChannelByID(promise.ChannelID)
		if !found {
			return errors.New("payments linked promise channel not found")
		}
		channel = channel.Normalize()
		if channel.Status != ChannelStatusOpen {
			return errors.New("payments linked promise channel must be open")
		}
		if !channel.ConditionalPayments {
			return errors.New("payments linked promise channel must support conditions")
		}
		if err := promise.ValidateForChannel(channel); err != nil {
			return err
		}
		if promise.ConditionType != ConditionTypeHashLock {
			return errors.New("payments linked promise must be hash-lock")
		}
		if promise.HashLock != proof.HashLock {
			return errors.New("payments linked promises must share hash lock")
		}
		if promise.RouteIDOptional != "" && promise.RouteIDOptional != proof.RouteID {
			return errors.New("payments linked promise route id mismatch")
		}
		if promiseWasSettled(channel, promise.PromiseID, settledClaims) {
			return errors.New("payments linked promise has already been settled")
		}
		channels[i] = channel
	}
	if proof.Promises[0].Source != proof.Sender {
		return errors.New("payments linked route sender mismatch")
	}
	if proof.Promises[len(proof.Promises)-1].Destination != proof.Receiver {
		return errors.New("payments linked route receiver mismatch")
	}
	if err := validateLinkedPromiseConservation(proof.Promises, proof.Amount, proof.TotalFees); err != nil {
		return err
	}
	for i := 0; i < len(proof.Promises)-1; i++ {
		upstream := proof.Promises[i]
		downstream := proof.Promises[i+1]
		if upstream.Destination != downstream.Source {
			return errors.New("payments linked promise hop mismatch")
		}
		if upstream.NextPromiseIDOptional != "" && upstream.NextPromiseIDOptional != downstream.PromiseID {
			return errors.New("payments linked promise next id mismatch")
		}
		if downstream.PreviousPromiseIDOptional != "" && downstream.PreviousPromiseIDOptional != upstream.PromiseID {
			return errors.New("payments linked promise previous id mismatch")
		}
		if err := ValidateCrossChannelPromiseTimeoutOrdering(channels[i], channels[i+1], upstream, downstream, proof.TimeoutMargin); err != nil {
			return err
		}
	}
	return nil
}

func (r BatchConditionSettlementRequest) Normalize() BatchConditionSettlementRequest {
	r.LinkageProof = r.LinkageProof.Normalize()
	r.Preimage = strings.TrimSpace(r.Preimage)
	r.Resolver = strings.TrimSpace(r.Resolver)
	r.SettlementFeePaid = strings.TrimSpace(r.SettlementFeePaid)
	if r.SettlementFeePaid == "" {
		r.SettlementFeePaid = "0"
	}
	return r
}

func (r BatchConditionSettlementRequest) ValidateForState(state PaymentsState, settledClaims []ConditionClaimRecord) error {
	req := r.Normalize()
	if req.Mode != ConditionSettlementModePreimage && req.Mode != ConditionSettlementModeExpiry {
		return errors.New("payments batch condition settlement mode is invalid")
	}
	if err := addressing.ValidateUserAddress("payments batch condition resolver", req.Resolver); err != nil {
		return err
	}
	if req.CurrentHeight == 0 {
		return errors.New("payments batch condition settlement height must be positive")
	}
	if err := req.LinkageProof.ValidateForState(state, settledClaims); err != nil {
		return err
	}
	proof := req.LinkageProof.Normalize()
	if req.Mode == ConditionSettlementModePreimage && req.Resolver != proof.Receiver {
		return errors.New("payments batch preimage resolver must be route receiver")
	}
	for _, promise := range req.LinkageProof.Normalize().Promises {
		if req.Mode == ConditionSettlementModePreimage {
			if req.CurrentHeight > promise.TimeoutHeight {
				return errors.New("payments batch preimage promise has timed out")
			}
			if err := VerifyPromisePreimage(promise, req.Preimage); err != nil {
				return err
			}
			continue
		}
		if req.CurrentHeight <= promise.TimeoutHeight {
			return errors.New("payments batch expiry promise has not expired")
		}
	}
	return nil
}

func (r BatchConditionSettlementResult) Normalize() BatchConditionSettlementResult {
	r.RouteID = normalizeHash(r.RouteID)
	r.Resolutions = normalizeConditionResolutions(r.Resolutions)
	r.ConditionRootUpdates = normalizeConditionRootUpdates(r.ConditionRootUpdates)
	for i, claim := range r.FeeClaims {
		r.FeeClaims[i] = claim.Normalize()
	}
	sort.SliceStable(r.FeeClaims, func(i, j int) bool {
		if r.FeeClaims[i].ChannelID == r.FeeClaims[j].ChannelID {
			return r.FeeClaims[i].PromiseID < r.FeeClaims[j].PromiseID
		}
		return r.FeeClaims[i].ChannelID < r.FeeClaims[j].ChannelID
	})
	r.EvidenceHash = normalizeHash(r.EvidenceHash)
	return r
}

func (r BatchConditionSettlementResult) Validate() error {
	result := r.Normalize()
	if err := ValidateHash("payments batch condition result route id", result.RouteID); err != nil {
		return err
	}
	if len(result.Resolutions) == 0 {
		return errors.New("payments batch condition result requires resolutions")
	}
	for _, claim := range result.FeeClaims {
		if err := claim.Validate(); err != nil {
			return err
		}
	}
	return ValidateHash("payments batch condition result evidence hash", result.EvidenceHash)
}

func validateConditionResolutionsForState(state ChannelState, channel ChannelRecord, resolutions []ConditionResolution, requireAll bool) error {
	state = state.Normalize()
	if len(state.Conditions) == 0 {
		if len(resolutions) > 0 {
			return errors.New("payments condition proofs supplied for state without conditions")
		}
		return nil
	}
	conditionByID := make(map[string]ConditionalPayment, len(state.Conditions))
	for _, condition := range state.Conditions {
		condition = condition.Normalize()
		conditionByID[condition.ConditionID] = condition
	}
	seen := make(map[string]struct{}, len(resolutions))
	for _, resolution := range normalizeConditionResolutions(resolutions) {
		condition, found := conditionByID[resolution.ConditionID]
		if !found {
			return errors.New("payments condition proof references unknown condition")
		}
		if _, found := seen[resolution.ConditionID]; found {
			return errors.New("payments duplicate condition proof")
		}
		if err := resolution.ValidateForCondition(condition, channel); err != nil {
			return err
		}
		seen[resolution.ConditionID] = struct{}{}
	}
	if requireAll && len(seen) != len(conditionByID) {
		return errors.New("payments all conditions must be resolved or expired")
	}
	return nil
}

func validateConditions(conditions []ConditionalPayment) error {
	if len(conditions) > MaxConditionsPerState {
		return fmt.Errorf("payments conditions must be <= %d", MaxConditionsPerState)
	}
	var previous string
	seen := make(map[string]struct{}, len(conditions))
	for i, condition := range conditions {
		if err := condition.Validate(); err != nil {
			return err
		}
		if _, found := seen[condition.ConditionID]; found {
			return errors.New("payments duplicate condition id")
		}
		seen[condition.ConditionID] = struct{}{}
		if i > 0 && previous >= condition.ConditionID {
			return errors.New("payments conditions must be sorted canonically")
		}
		previous = condition.ConditionID
	}
	return nil
}

func ValidateConditionalPromisesForChannel(channel ChannelRecord, promises []ConditionalPromise, settledClaims []ConditionClaimRecord) error {
	channel = channel.Normalize()
	if len(promises) > MaxConditionsPerState {
		return fmt.Errorf("payments promises must be <= %d", MaxConditionsPerState)
	}
	seen := make(map[string]struct{}, len(promises))
	reservedBySource := make(map[string]sdkmath.Int, len(channel.Participants))
	for _, promise := range normalizeConditionalPromises(promises) {
		if err := promise.ValidateForChannel(channel); err != nil {
			return err
		}
		if _, found := seen[promise.PromiseID]; found {
			return errors.New("payments duplicate promise id")
		}
		seen[promise.PromiseID] = struct{}{}
		if promiseWasSettled(channel, promise.PromiseID, settledClaims) {
			return errors.New("payments promise has already been settled")
		}
		amount, err := parsePositiveInt("payments promise amount", promise.Amount)
		if err != nil {
			return err
		}
		fee, err := parseNonNegativeInt("payments promise fee", promise.Fee)
		if err != nil {
			return err
		}
		current, found := reservedBySource[promise.Source]
		if !found {
			current = sdkmath.ZeroInt()
		}
		reservedBySource[promise.Source] = current.Add(amount).Add(fee)
	}
	for source, reserved := range reservedBySource {
		available, err := availablePromiseReserve(channel, source)
		if err != nil {
			return err
		}
		if reserved.GT(available) {
			return errors.New("payments promises exceed available reserve")
		}
	}
	return nil
}

func VerifyPromisePreimage(promise ConditionalPromise, preimage string) error {
	promise = promise.Normalize()
	preimage = strings.TrimSpace(preimage)
	if promise.ConditionType != ConditionTypeHashLock {
		return errors.New("payments preimage verification requires hash-lock promise")
	}
	if preimage == "" {
		return errors.New("payments preimage is required")
	}
	if HashParts(preimage) != promise.HashLock {
		return errors.New("payments preimage does not satisfy hash lock")
	}
	return nil
}

func ValidatePromiseTimeoutOrdering(channel ChannelRecord, upstream, downstream ConditionalPromise, margin uint64) error {
	channel = channel.Normalize()
	upstream = upstream.Normalize()
	downstream = downstream.Normalize()
	if margin == 0 {
		margin = DefaultTimeoutMargin
	}
	minMargin := channel.CloseDelay + channel.DisputePeriod
	if margin < minMargin {
		return errors.New("payments timeout margin must cover dispute and settlement latency")
	}
	if upstream.ChannelID != channel.ChannelID || downstream.ChannelID != channel.ChannelID {
		return errors.New("payments timeout ordering channel mismatch")
	}
	if upstream.HashLock != downstream.HashLock {
		return errors.New("payments timeout ordering requires compatible hash locks")
	}
	if downstream.TimeoutHeight+margin < downstream.TimeoutHeight || downstream.TimeoutHeight+margin > upstream.TimeoutHeight {
		return errors.New("payments downstream timeout must expire before upstream by margin")
	}
	if upstream.PreviousPromiseIDOptional != "" && upstream.PreviousPromiseIDOptional != downstream.PromiseID {
		return errors.New("payments upstream previous promise link mismatch")
	}
	if downstream.NextPromiseIDOptional != "" && downstream.NextPromiseIDOptional != upstream.PromiseID {
		return errors.New("payments downstream next promise link mismatch")
	}
	return nil
}

func ValidateCrossChannelPromiseTimeoutOrdering(upstreamChannel, downstreamChannel ChannelRecord, upstream, downstream ConditionalPromise, margin uint64) error {
	upstreamChannel = upstreamChannel.Normalize()
	downstreamChannel = downstreamChannel.Normalize()
	upstream = upstream.Normalize()
	downstream = downstream.Normalize()
	if margin == 0 {
		margin = DefaultTimeoutMargin
	}
	upstreamLatency := upstreamChannel.CloseDelay + upstreamChannel.DisputePeriod
	downstreamLatency := downstreamChannel.CloseDelay + downstreamChannel.DisputePeriod
	minMargin := upstreamLatency
	if downstreamLatency > minMargin {
		minMargin = downstreamLatency
	}
	if margin < minMargin {
		return errors.New("payments cross-channel timeout margin must cover dispute and settlement latency")
	}
	if upstream.ChannelID != upstreamChannel.ChannelID || downstream.ChannelID != downstreamChannel.ChannelID {
		return errors.New("payments cross-channel timeout ordering channel mismatch")
	}
	if upstream.HashLock != downstream.HashLock {
		return errors.New("payments cross-channel timeout ordering requires compatible hash locks")
	}
	if downstream.TimeoutHeight+margin < downstream.TimeoutHeight || downstream.TimeoutHeight+margin > upstream.TimeoutHeight {
		return errors.New("payments downstream timeout must expire before upstream by margin")
	}
	return nil
}

func ValidatePromiseTimeoutChain(channel ChannelRecord, promises []ConditionalPromise, margin uint64) error {
	byID := make(map[string]ConditionalPromise, len(promises))
	for _, promise := range normalizeConditionalPromises(promises) {
		byID[promise.PromiseID] = promise
	}
	for _, downstream := range byID {
		if downstream.NextPromiseIDOptional == "" {
			continue
		}
		upstream, found := byID[downstream.NextPromiseIDOptional]
		if !found {
			return errors.New("payments timeout chain references unknown upstream promise")
		}
		if err := ValidatePromiseTimeoutOrdering(channel, upstream, downstream, margin); err != nil {
			return err
		}
	}
	return nil
}

func validateLinkedPromiseConservation(promises []ConditionalPromise, amount, totalFees string) error {
	if len(promises) == 0 {
		return errors.New("payments linked promise conservation requires promises")
	}
	finalAmount, err := parsePositiveInt("payments linked promise final amount", amount)
	if err != nil {
		return err
	}
	expectedFees, err := parseNonNegativeInt("payments linked promise total fees", totalFees)
	if err != nil {
		return err
	}
	accumulatedFees := sdkmath.ZeroInt()
	for i := 1; i < len(promises); i++ {
		fee, err := parseNonNegativeInt("payments linked promise hop fee", promises[i].Fee)
		if err != nil {
			return err
		}
		accumulatedFees = accumulatedFees.Add(fee)
		incoming, err := parsePositiveInt("payments linked promise incoming amount", promises[i-1].Amount)
		if err != nil {
			return err
		}
		outgoing, err := parsePositiveInt("payments linked promise outgoing amount", promises[i].Amount)
		if err != nil {
			return err
		}
		if !incoming.Equal(outgoing.Add(fee)) {
			return errors.New("payments linked promise amount conservation failed")
		}
	}
	if !accumulatedFees.Equal(expectedFees) {
		return errors.New("payments linked promise total fee mismatch")
	}
	lastAmount, err := parsePositiveInt("payments linked promise receiver amount", promises[len(promises)-1].Amount)
	if err != nil {
		return err
	}
	if !lastAmount.Equal(finalAmount) {
		return errors.New("payments linked promise final amount mismatch")
	}
	firstAmount, err := parsePositiveInt("payments linked promise sender amount", promises[0].Amount)
	if err != nil {
		return err
	}
	if !firstAmount.Equal(finalAmount.Add(expectedFees)) {
		return errors.New("payments linked promise route total mismatch")
	}
	finalFee, err := parseNonNegativeInt("payments linked final promise fee", promises[len(promises)-1].Fee)
	if err != nil {
		return err
	}
	if len(promises) > 1 && finalFee.IsZero() && !expectedFees.IsZero() {
		return errors.New("payments linked final hop fee must pay forwarding intermediary")
	}
	return nil
}

func BuildConditionRootUpdateFromPromises(channel ChannelRecord, base ChannelState, promises []ConditionalPromise, settledClaims []ConditionClaimRecord) (ChannelState, ConditionRootUpdate, error) {
	channel = channel.Normalize()
	base = base.Normalize()
	if err := base.ValidateForChannel(channel, false); err != nil {
		return ChannelState{}, ConditionRootUpdate{}, err
	}
	if err := ValidateConditionalPromisesForChannel(channel, promises, settledClaims); err != nil {
		return ChannelState{}, ConditionRootUpdate{}, err
	}
	conditions := make([]ConditionalPayment, 0, len(promises))
	for _, promise := range normalizeConditionalPromises(promises) {
		conditions = append(conditions, promise.ToConditionalPayment())
	}
	if err := validateConditions(conditions); err != nil {
		return ChannelState{}, ConditionRootUpdate{}, err
	}
	next := base
	next.Conditions = conditions
	next.ConditionRoot = ComputeConditionsRoot(conditions)
	next.PendingConditionsRoot = next.ConditionRoot
	next.ConditionCount = uint32(len(conditions))
	next.SignaturePreimageHash = ComputeStateSignaturePreimageHash(next)
	next.StateHash = ComputeStateHash(next)
	return next.Normalize(), ConditionRootUpdate{
		ChannelID:      channel.ChannelID,
		Nonce:          next.Nonce,
		ConditionRoot:  next.ConditionRoot,
		ConditionCount: next.ConditionCount,
		Conditions:     next.Conditions,
	}, nil
}

func BuildConditionRootAfterExpiry(base ChannelState, expired []ConditionalPromise) (ChannelState, ConditionRootUpdate, error) {
	base = base.Normalize()
	expiredByID := make(map[string]struct{}, len(expired))
	for _, promise := range normalizeConditionalPromises(expired) {
		expiredByID[promise.PromiseID] = struct{}{}
	}
	conditions := make([]ConditionalPayment, 0, len(base.Conditions))
	for _, condition := range normalizeConditions(base.Conditions) {
		if _, found := expiredByID[condition.ConditionID]; found {
			continue
		}
		conditions = append(conditions, condition)
	}
	if len(conditions) == len(base.Conditions) {
		return ChannelState{}, ConditionRootUpdate{}, errors.New("payments expiry did not remove any condition")
	}
	next := base
	next.Conditions = conditions
	next.ConditionRoot = ComputeConditionsRoot(conditions)
	next.PendingConditionsRoot = next.ConditionRoot
	next.ConditionCount = uint32(len(conditions))
	next.SignaturePreimageHash = ComputeStateSignaturePreimageHash(next)
	next.StateHash = ComputeStateHash(next)
	return next.Normalize(), ConditionRootUpdate{
		ChannelID:      next.ChannelID,
		Nonce:          next.Nonce,
		ConditionRoot:  next.ConditionRoot,
		ConditionCount: next.ConditionCount,
		Conditions:     next.Conditions,
	}, nil
}

func validatePromiseTimeoutWindow(channel ChannelRecord, promise ConditionalPromise) error {
	if promise.TimeoutHeight <= channel.OpenHeight {
		return errors.New("payments promise timeout must be after channel open height")
	}
	maxHeight := channel.LatestState.TimeoutHeight
	if maxHeight == 0 {
		maxHeight = channel.OpenHeight + channel.CloseDelay + channel.DisputePeriod
	}
	if promise.TimeoutHeight > maxHeight {
		return errors.New("payments promise timeout exceeds channel timeout")
	}
	if promise.TimeoutHeight+channel.DisputePeriod < promise.TimeoutHeight || promise.TimeoutHeight+channel.DisputePeriod > maxHeight {
		return errors.New("payments promise timeout must fit dispute window")
	}
	return nil
}

func validatePromiseSignatureEnvelope(channel ChannelRecord, sig PromiseSignature, promise ConditionalPromise) error {
	sig = sig.Normalize()
	promise = promise.Normalize()
	if sig.ChainID != channel.ChainID {
		return errors.New("payments promise signature chain id mismatch")
	}
	if sig.ChannelID != promise.ChannelID {
		return errors.New("payments promise signature channel id mismatch")
	}
	if sig.Version != CurrentStateVersion {
		return errors.New("payments promise signature version mismatch")
	}
	if sig.Nonce != promise.Nonce {
		return errors.New("payments promise signature nonce mismatch")
	}
	if sig.ExpirationHeight != promise.TimeoutHeight {
		return errors.New("payments promise signature expiration height mismatch")
	}
	if sig.Signer != promise.Source {
		return errors.New("payments promise signature signer must be source")
	}
	return nil
}

func availablePromiseReserve(channel ChannelRecord, source string) (sdkmath.Int, error) {
	state := channel.Normalize().LatestState.Normalize()
	if channel.ChannelType == ChannelTypeBidirectional {
		if source == state.ParticipantA {
			return parseNonNegativeInt("payments promise reserve a", state.ReserveA)
		}
		if source == state.ParticipantB {
			return parseNonNegativeInt("payments promise reserve b", state.ReserveB)
		}
	}
	return sdkmath.Int{}, errors.New("payments promise source reserve not found")
}

func promiseWasSettled(channel ChannelRecord, promiseID string, settledClaims []ConditionClaimRecord) bool {
	channel = channel.Normalize()
	promiseID = normalizeHash(promiseID)
	for _, claim := range settledClaims {
		claim = claim.Normalize()
		if claim.ChannelID == channel.ChannelID && claim.ConditionID == promiseID {
			return true
		}
	}
	return false
}

func (c ConditionalPayment) Normalize() ConditionalPayment {
	c.ConditionID = normalizeHash(c.ConditionID)
	c.Payer = strings.TrimSpace(c.Payer)
	c.Payee = strings.TrimSpace(c.Payee)
	c.Amount = strings.TrimSpace(c.Amount)
	c.HashLock = normalizeOptionalHash(c.HashLock)
	return c
}

func (c ConditionalPayment) Validate() error {
	condition := c.Normalize()
	if err := ValidateHash("payments condition id", condition.ConditionID); err != nil {
		return err
	}
	if !IsConditionType(condition.ConditionType) {
		return fmt.Errorf("unknown payments condition type %q", condition.ConditionType)
	}
	if err := addressing.ValidateUserAddress("payments condition payer", condition.Payer); err != nil {
		return err
	}
	if err := addressing.ValidateUserAddress("payments condition payee", condition.Payee); err != nil {
		return err
	}
	if condition.Payer == condition.Payee {
		return errors.New("payments condition parties must differ")
	}
	if err := validatePositiveInt("payments condition amount", condition.Amount); err != nil {
		return err
	}
	if condition.TimeoutHeight == 0 {
		return errors.New("payments condition timeout height must be positive")
	}
	if condition.NonceStart == 0 || condition.NonceEnd < condition.NonceStart {
		return errors.New("payments condition nonce range is invalid")
	}
	if condition.ConditionType == ConditionTypeHashLock {
		return ValidateHash("payments condition hash lock", condition.HashLock)
	}
	if condition.HashLock != "" {
		return errors.New("payments time-lock condition must not include hash lock")
	}
	return nil
}

func IsConditionType(value ConditionType) bool {
	switch value {
	case ConditionTypeHashLock, ConditionTypeTimeLock:
		return true
	default:
		return false
	}
}

func sumConditions(conditions []ConditionalPayment) (sdkmath.Int, error) {
	total := sdkmath.ZeroInt()
	for _, condition := range conditions {
		amount, err := parsePositiveInt("payments condition amount", condition.Amount)
		if err != nil {
			return sdkmath.Int{}, err
		}
		total = total.Add(amount)
	}
	return total, nil
}

func normalizeConditions(conditions []ConditionalPayment) []ConditionalPayment {
	out := make([]ConditionalPayment, len(conditions))
	for i, condition := range conditions {
		out[i] = condition.Normalize()
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].ConditionID < out[j].ConditionID
	})
	return out
}

func normalizeConditionalPromises(promises []ConditionalPromise) []ConditionalPromise {
	out := make([]ConditionalPromise, len(promises))
	for i, promise := range promises {
		out[i] = promise.Normalize()
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].PromiseID < out[j].PromiseID
	})
	return out
}

func normalizePromiseRoute(promises []ConditionalPromise) []ConditionalPromise {
	out := make([]ConditionalPromise, len(promises))
	for i, promise := range promises {
		out[i] = promise.Normalize()
	}
	return out
}

func normalizeConditionResolutions(resolutions []ConditionResolution) []ConditionResolution {
	out := make([]ConditionResolution, len(resolutions))
	for i, resolution := range resolutions {
		out[i] = resolution.Normalize()
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].ConditionID < out[j].ConditionID
	})
	return out
}

func normalizeConditionRootUpdates(updates []ConditionRootUpdate) []ConditionRootUpdate {
	out := make([]ConditionRootUpdate, len(updates))
	for i, update := range updates {
		update.ChannelID = normalizeHash(update.ChannelID)
		update.ConditionRoot = normalizeHash(update.ConditionRoot)
		update.Conditions = normalizeConditions(update.Conditions)
		out[i] = update
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].ChannelID < out[j].ChannelID
	})
	return out
}
