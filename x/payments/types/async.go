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
	ChannelTypeBidirectional  ChannelType = "BIDIRECTIONAL"
	ChannelTypeUnidirectional ChannelType = "UNIDIRECTIONAL"
	ChannelTypeAsync          ChannelType = "ASYNC"
)

type DeltaSignature struct {
	Signer           string
	ChainID          string
	ChannelID        string
	ObjectType       string
	Version          uint32
	Nonce            uint64
	ObjectID         string
	ExpirationHeight uint64
	CommitmentHash   string
	DeltaHash        string
	SignatureHash    string
}

type AsyncFinalizationJob struct {
	JobID           string
	ChannelID       string
	FinalizeHeight  uint64
	EnqueuedHeight  uint64
	Attempts        uint32
	LastRunHeight   uint64
	LastError       string
	Completed       bool
	CompletedHeight uint64
	SettlementHash  string
}

type AsyncPromiseExpiryJob struct {
	JobID             string
	ChannelID         string
	PromiseID         string
	Promise           ConditionalPromise
	Resolver          string
	ExpireAfterHeight uint64
	EnqueuedHeight    uint64
	Attempts          uint32
	LastRunHeight     uint64
	LastError         string
	Completed         bool
	CompletedHeight   uint64
	ResolutionHash    string
}

type AsyncSettlementCompletion struct {
	CompletionID string
	JobID        string
	JobType      string
	ChannelID    string
	ObjectID     string
	ResultHash   string
	Height       uint64
}

type AsyncExecutionResult struct {
	ProcessedFinalizations   uint64
	ProcessedPromiseExpiries uint64
	CompletedJobIDs          []string
	FailedJobIDs             []string
	EmittedCompletionIDs     []string
}

type AsyncPaymentDelta struct {
	UpdateID     string
	ChainID      string
	ChannelID    string
	From         string
	To           string
	Direction    string
	Amount       string
	NonceStart   uint64
	NonceEnd     uint64
	ExpiryHeight uint64
	DeltaHash    string
	Signature    DeltaSignature
}

type AsyncDeltaDisputeProof struct {
	ProofID         string
	ChannelID       string
	CheckpointState ChannelState
	Deltas          []AsyncPaymentDelta
	EvidenceHash    string
}

func BuildAsyncDelta(delta AsyncPaymentDelta) (AsyncPaymentDelta, error) {
	delta = delta.Normalize()
	if err := validateUnsignedAsyncDelta(delta); err != nil {
		return AsyncPaymentDelta{}, err
	}
	delta.DeltaHash = ComputeAsyncDeltaHash(delta)
	return delta, nil
}

func SignatureForAsyncDelta(delta AsyncPaymentDelta, signer string) (DeltaSignature, error) {
	if delta.DeltaHash == "" {
		var err error
		delta, err = BuildAsyncDelta(delta)
		if err != nil {
			return DeltaSignature{}, err
		}
	}
	signer = strings.TrimSpace(signer)
	if err := addressing.ValidateUserAddress("payments async delta signer", signer); err != nil {
		return DeltaSignature{}, err
	}
	return DeltaSignature{
		Signer:           signer,
		ChainID:          delta.ChainID,
		ChannelID:        delta.ChannelID,
		ObjectType:       SignatureObjectDelta,
		Version:          CurrentStateVersion,
		Nonce:            delta.NonceStart,
		ObjectID:         delta.UpdateID,
		ExpirationHeight: delta.ExpiryHeight,
		CommitmentHash:   delta.DeltaHash,
		DeltaHash:        delta.DeltaHash,
		SignatureHash: ComputeSignatureEnvelopeHash(
			signer,
			delta.ChainID,
			delta.ChannelID,
			SignatureObjectDelta,
			CurrentStateVersion,
			delta.NonceStart,
			delta.UpdateID,
			delta.ExpiryHeight,
			delta.DeltaHash,
		),
	}, nil
}

func AsyncDeltaDirection(from, to string) string {
	return strings.TrimSpace(from) + "->" + strings.TrimSpace(to)
}

func BuildAsyncCheckpointState(channel ChannelRecord, deltas []AsyncPaymentDelta, checkpointNonce, currentHeight uint64) (ChannelState, error) {
	channel = channel.Normalize()
	if channel.ChannelType != ChannelTypeAsync {
		return ChannelState{}, errors.New("payments async checkpoint requires async channel")
	}
	if checkpointNonce == 0 {
		return ChannelState{}, errors.New("payments async checkpoint nonce must be positive")
	}
	base := channel.LatestState.Normalize()
	if base.StateHash == "" {
		return ChannelState{}, errors.New("payments async checkpoint requires latest state")
	}
	if checkpointNonce <= base.CheckpointNonce {
		return ChannelState{}, errors.New("payments async checkpoint nonce must increase")
	}
	if currentHeight == 0 {
		return ChannelState{}, errors.New("payments async checkpoint height must be positive")
	}
	if currentHeight > base.ExpiryHeight {
		return ChannelState{}, errors.New("payments async checkpoint is expired")
	}
	normalizedDeltas := normalizeAsyncDeltas(deltas)
	if err := validateAsyncDeltasForCheckpoint(channel, base, normalizedDeltas, checkpointNonce, currentHeight); err != nil {
		return ChannelState{}, err
	}
	nextBalances, err := applyAsyncDeltas(base.Balances, normalizedDeltas)
	if err != nil {
		return ChannelState{}, err
	}
	state, err := BuildState(ChannelState{
		ChainID:              channel.ChainID,
		AppVersion:           CurrentAppVersion,
		ModuleName:           ModuleName,
		ChannelID:            channel.ChannelID,
		ChannelType:          ChannelTypeAsync,
		ParticipantSetHash:   ComputeParticipantSetHash(channel.Participants),
		Denom:                channel.Denom,
		Version:              CurrentStateVersion,
		Epoch:                base.Epoch,
		Nonce:                checkpointNonce,
		Balances:             nextBalances,
		CheckpointNonce:      checkpointNonce,
		CheckpointBalances:   nextBalances,
		AsyncUpdateRoot:      ComputeAsyncDeltaRootForChannel(channel, normalizedDeltas),
		AcceptedUpdateRoot:   ComputeAsyncDeltaRootForChannel(channel, normalizedDeltas),
		SendWindow:           base.SendWindow,
		ReceiveWindow:        base.ReceiveWindow,
		MaxUnackedAmount:     base.MaxUnackedAmount,
		ExpiryHeight:         base.ExpiryHeight,
		TimeoutHeight:        base.TimeoutHeight,
		TimeoutTimestamp:     base.TimeoutTimestamp,
		ChallengePeriod:      base.ChallengePeriod,
		CloseDelay:           base.CloseDelay,
		FeePolicyID:          NativeDenom,
		RequiredSignerBitmap: ComputeRequiredSignerBitmap(channel.Participants, channel.RequiredSigners),
		SignatureScheme:      SignatureSchemeEd25519,
	})
	if err != nil {
		return ChannelState{}, err
	}
	return state, nil
}

func (j AsyncFinalizationJob) Normalize() AsyncFinalizationJob {
	j.JobID = normalizeOptionalHash(j.JobID)
	j.ChannelID = normalizeHash(j.ChannelID)
	j.LastError = strings.TrimSpace(j.LastError)
	j.SettlementHash = normalizeOptionalHash(j.SettlementHash)
	return j
}

func (j AsyncFinalizationJob) Validate() error {
	job := j.Normalize()
	if err := ValidateHash("payments async finalization job id", job.JobID); err != nil {
		return err
	}
	if err := ValidateHash("payments async finalization channel id", job.ChannelID); err != nil {
		return err
	}
	if job.FinalizeHeight == 0 || job.EnqueuedHeight == 0 {
		return errors.New("payments async finalization heights must be positive")
	}
	if job.Completed {
		if job.CompletedHeight == 0 {
			return errors.New("payments async finalization completed height must be positive")
		}
		if err := ValidateHash("payments async finalization settlement hash", job.SettlementHash); err != nil {
			return err
		}
	}
	return nil
}

func (j AsyncPromiseExpiryJob) Normalize() AsyncPromiseExpiryJob {
	j.JobID = normalizeOptionalHash(j.JobID)
	j.ChannelID = normalizeHash(j.ChannelID)
	j.PromiseID = normalizeHash(j.PromiseID)
	j.Promise = j.Promise.Normalize()
	j.Resolver = strings.TrimSpace(j.Resolver)
	j.LastError = strings.TrimSpace(j.LastError)
	j.ResolutionHash = normalizeOptionalHash(j.ResolutionHash)
	return j
}

func (j AsyncPromiseExpiryJob) Validate() error {
	job := j.Normalize()
	if err := ValidateHash("payments async promise expiry job id", job.JobID); err != nil {
		return err
	}
	if err := ValidateHash("payments async promise expiry channel id", job.ChannelID); err != nil {
		return err
	}
	if err := ValidateHash("payments async promise id", job.PromiseID); err != nil {
		return err
	}
	if job.Promise.ChannelID != job.ChannelID || job.Promise.PromiseID != job.PromiseID {
		return errors.New("payments async promise expiry job promise mismatch")
	}
	if err := addressing.ValidateUserAddress("payments async promise resolver", job.Resolver); err != nil {
		return err
	}
	if job.ExpireAfterHeight == 0 || job.EnqueuedHeight == 0 {
		return errors.New("payments async promise expiry heights must be positive")
	}
	if job.Completed {
		if job.CompletedHeight == 0 {
			return errors.New("payments async promise expiry completed height must be positive")
		}
		if err := ValidateHash("payments async promise expiry resolution hash", job.ResolutionHash); err != nil {
			return err
		}
	}
	return nil
}

func (c AsyncSettlementCompletion) Normalize() AsyncSettlementCompletion {
	c.CompletionID = normalizeOptionalHash(c.CompletionID)
	c.JobID = normalizeHash(c.JobID)
	c.JobType = strings.TrimSpace(c.JobType)
	c.ChannelID = normalizeHash(c.ChannelID)
	c.ObjectID = strings.TrimSpace(c.ObjectID)
	c.ResultHash = normalizeHash(c.ResultHash)
	return c
}

func (c AsyncSettlementCompletion) Validate() error {
	completion := c.Normalize()
	if err := ValidateHash("payments async completion id", completion.CompletionID); err != nil {
		return err
	}
	if err := ValidateHash("payments async completion job id", completion.JobID); err != nil {
		return err
	}
	if completion.JobType == "" {
		return errors.New("payments async completion job type is required")
	}
	if err := ValidateHash("payments async completion channel id", completion.ChannelID); err != nil {
		return err
	}
	if err := ValidateHash("payments async completion result hash", completion.ResultHash); err != nil {
		return err
	}
	if completion.Height == 0 {
		return errors.New("payments async completion height must be positive")
	}
	return nil
}

func (s DeltaSignature) Normalize() DeltaSignature {
	s.Signer = strings.TrimSpace(s.Signer)
	s.ChainID = strings.TrimSpace(s.ChainID)
	s.ChannelID = normalizeHash(s.ChannelID)
	s.ObjectType = strings.TrimSpace(s.ObjectType)
	s.ObjectID = strings.TrimSpace(s.ObjectID)
	s.CommitmentHash = normalizeHash(s.CommitmentHash)
	s.DeltaHash = normalizeHash(s.DeltaHash)
	s.SignatureHash = normalizeHash(s.SignatureHash)
	return s
}

func (s DeltaSignature) Validate(expectedDeltaHash string) error {
	s = s.Normalize()
	if err := addressing.ValidateUserAddress("payments async delta signature signer", s.Signer); err != nil {
		return err
	}
	if s.DeltaHash != expectedDeltaHash {
		return errors.New("payments async delta signature hash mismatch")
	}
	if s.ObjectType != SignatureObjectDelta {
		return errors.New("payments async delta signature object type mismatch")
	}
	if s.Version != CurrentStateVersion {
		return errors.New("payments async delta signature version mismatch")
	}
	if s.ObjectID == "" {
		return errors.New("payments async delta signature object id is required")
	}
	if s.CommitmentHash != s.DeltaHash {
		return errors.New("payments async delta signature commitment mismatch")
	}
	if err := ValidateHash("payments async delta signature hash", s.SignatureHash); err != nil {
		return err
	}
	if expected := ComputeSignatureEnvelopeHash(s.Signer, s.ChainID, s.ChannelID, s.ObjectType, s.Version, s.Nonce, s.ObjectID, s.ExpirationHeight, s.CommitmentHash); s.SignatureHash != expected {
		return errors.New("payments async delta signature value mismatch")
	}
	return nil
}

func (d AsyncPaymentDelta) Normalize() AsyncPaymentDelta {
	d.UpdateID = normalizeHash(d.UpdateID)
	d.ChainID = strings.TrimSpace(d.ChainID)
	d.ChannelID = normalizeHash(d.ChannelID)
	d.From = strings.TrimSpace(d.From)
	d.To = strings.TrimSpace(d.To)
	d.Direction = strings.TrimSpace(d.Direction)
	if d.Direction == "" && d.From != "" && d.To != "" {
		d.Direction = AsyncDeltaDirection(d.From, d.To)
	}
	d.Amount = strings.TrimSpace(d.Amount)
	d.DeltaHash = normalizeOptionalHash(d.DeltaHash)
	d.Signature = d.Signature.Normalize()
	return d
}

func (d AsyncPaymentDelta) ValidateForChannel(channel ChannelRecord, currentHeight uint64) error {
	channel = channel.Normalize()
	if err := channel.ValidateCore(); err != nil {
		return err
	}
	if channel.ChannelType != ChannelTypeAsync {
		return errors.New("payments async delta requires async channel")
	}
	delta := d.Normalize()
	if err := validateUnsignedAsyncDelta(delta); err != nil {
		return err
	}
	if delta.ChainID != channel.ChainID {
		return errors.New("payments async delta chain id mismatch")
	}
	if delta.ChannelID != channel.ChannelID {
		return errors.New("payments async delta channel mismatch")
	}
	if !containsString(channel.Participants, delta.From) || !containsString(channel.Participants, delta.To) {
		return errors.New("payments async delta parties must be channel participants")
	}
	if delta.From == delta.To {
		return errors.New("payments async delta parties must differ")
	}
	if delta.Direction != AsyncDeltaDirection(delta.From, delta.To) {
		return errors.New("payments async delta direction mismatch")
	}
	if currentHeight > delta.ExpiryHeight {
		return errors.New("payments async delta is expired")
	}
	if delta.DeltaHash == "" {
		return errors.New("payments async delta hash is required")
	}
	if expected := ComputeAsyncDeltaHash(delta); delta.DeltaHash != expected {
		return errors.New("payments async delta hash mismatch")
	}
	if err := delta.Signature.Validate(delta.DeltaHash); err != nil {
		return err
	}
	if err := validateDeltaSignatureEnvelope(delta.Signature, delta); err != nil {
		return err
	}
	if currentHeight > delta.Signature.ExpirationHeight {
		return errors.New("payments async delta signature is expired")
	}
	if delta.Signature.Signer != delta.From {
		return errors.New("payments async delta signer must be sender")
	}
	return nil
}

func (p AsyncDeltaDisputeProof) Normalize() AsyncDeltaDisputeProof {
	p.ProofID = normalizeHash(p.ProofID)
	p.ChannelID = normalizeHash(p.ChannelID)
	p.CheckpointState = p.CheckpointState.Normalize()
	p.Deltas = normalizeAsyncDeltas(p.Deltas)
	p.EvidenceHash = normalizeHash(p.EvidenceHash)
	return p
}

func (p AsyncDeltaDisputeProof) ValidateForChannel(channel ChannelRecord, currentHeight uint64) error {
	proof := p.Normalize()
	if err := ValidateHash("payments async dispute proof id", proof.ProofID); err != nil {
		return err
	}
	if proof.ChannelID != channel.Normalize().ChannelID {
		return errors.New("payments async dispute proof channel mismatch")
	}
	if err := ValidateHash("payments async dispute evidence hash", proof.EvidenceHash); err != nil {
		return err
	}
	if err := proof.CheckpointState.ValidateForChannel(channel, false); err != nil {
		return err
	}
	reconstructed, err := BuildAsyncCheckpointState(channel, proof.Deltas, proof.CheckpointState.CheckpointNonce, currentHeight)
	if err != nil {
		return err
	}
	if reconstructed.StateHash != proof.CheckpointState.StateHash {
		return errors.New("payments async dispute proof does not reconstruct checkpoint")
	}
	if proof.EvidenceHash != HashParts("async-dispute", proof.CheckpointState.StateHash, ComputeAsyncDeltaRootForChannel(channel, proof.Deltas)) {
		return errors.New("payments async dispute evidence hash mismatch")
	}
	return nil
}

func validateUnsignedAsyncDelta(delta AsyncPaymentDelta) error {
	if err := ValidateHash("payments async delta update id", delta.UpdateID); err != nil {
		return err
	}
	if strings.TrimSpace(delta.ChainID) == "" {
		return errors.New("payments async delta chain id is required")
	}
	if err := ValidateHash("payments async delta channel id", delta.ChannelID); err != nil {
		return err
	}
	if err := addressing.ValidateUserAddress("payments async delta from", delta.From); err != nil {
		return err
	}
	if err := addressing.ValidateUserAddress("payments async delta to", delta.To); err != nil {
		return err
	}
	if delta.From == delta.To {
		return errors.New("payments async delta parties must differ")
	}
	if delta.Direction != AsyncDeltaDirection(delta.From, delta.To) {
		return errors.New("payments async delta direction mismatch")
	}
	if err := validatePositiveInt("payments async delta amount", delta.Amount); err != nil {
		return err
	}
	if delta.NonceStart == 0 || delta.NonceEnd < delta.NonceStart {
		return errors.New("payments async delta nonce range is invalid")
	}
	if delta.ExpiryHeight == 0 {
		return errors.New("payments async delta expiry height must be positive")
	}
	return nil
}

func validateAsyncStateProjection(state ChannelState, channel ChannelRecord) error {
	if state.CheckpointNonce == 0 {
		return errors.New("payments async checkpoint nonce must be positive")
	}
	if state.CheckpointNonce != state.Nonce {
		return errors.New("payments async checkpoint nonce must match state nonce")
	}
	if err := validateBalances(state.CheckpointBalances); err != nil {
		return err
	}
	if !sameBalances(state.Balances, state.CheckpointBalances) {
		return errors.New("payments async checkpoint balances mismatch")
	}
	if err := ValidateHash("payments async update root", state.AsyncUpdateRoot); err != nil {
		return err
	}
	if err := ValidateHash("payments async accepted update root", state.AcceptedUpdateRoot); err != nil {
		return err
	}
	if state.SendWindow == 0 {
		return errors.New("payments async send window must be positive")
	}
	if state.ReceiveWindow == 0 {
		return errors.New("payments async receive window must be positive")
	}
	if err := validatePositiveInt("payments async max unacked amount", state.MaxUnackedAmount); err != nil {
		return err
	}
	if state.ExpiryHeight == 0 {
		return errors.New("payments async expiry height must be positive")
	}
	if channel.LatestState.StateHash != "" && channel.LatestState.ChannelType == ChannelTypeAsync {
		previous := channel.LatestState.Normalize()
		if previous.SendWindow != 0 && state.SendWindow != previous.SendWindow {
			return errors.New("payments async send window cannot change inside checkpoint")
		}
		if previous.ReceiveWindow != 0 && state.ReceiveWindow != previous.ReceiveWindow {
			return errors.New("payments async receive window cannot change inside checkpoint")
		}
		if previous.MaxUnackedAmount != "" && state.MaxUnackedAmount != previous.MaxUnackedAmount {
			return errors.New("payments async max unacked amount cannot change inside checkpoint")
		}
	}
	return nil
}

func validateAsyncOverexposureProof(channel ChannelRecord, asyncProof AsyncDeltaDisputeProof) error {
	channel = channel.Normalize()
	proof := asyncProof.Normalize()
	if channel.ChannelType != ChannelTypeAsync {
		return errors.New("payments async overexposure proof requires async channel")
	}
	if err := ValidateHash("payments async overexposure proof id", proof.ProofID); err != nil {
		return err
	}
	if proof.ChannelID != channel.ChannelID {
		return errors.New("payments async overexposure proof channel mismatch")
	}
	if err := ValidateHash("payments async overexposure evidence hash", proof.EvidenceHash); err != nil {
		return err
	}
	if err := proof.CheckpointState.ValidateForChannel(channel, false); err != nil {
		return err
	}
	if len(proof.Deltas) == 0 {
		return errors.New("payments async overexposure proof requires deltas")
	}
	maxExposure, err := parsePositiveInt("payments async max unacked amount", proof.CheckpointState.MaxUnackedAmount)
	if err != nil {
		return err
	}
	currentHeight := channel.OpenHeight
	if channel.PendingClose.SubmittedHeight != 0 {
		currentHeight = channel.PendingClose.SubmittedHeight
	}
	exposureBySender := make(map[string]sdkmath.Int, len(channel.Participants))
	overexposed := false
	for _, delta := range normalizeAsyncDeltas(proof.Deltas) {
		if err := delta.ValidateForChannel(channel, currentHeight); err != nil {
			return err
		}
		amount, err := parsePositiveInt("payments async delta amount", delta.Amount)
		if err != nil {
			return err
		}
		exposureBySender[delta.From] = exposureBySender[delta.From].Add(amount)
		if exposureBySender[delta.From].GT(maxExposure) {
			overexposed = true
		}
	}
	if !overexposed {
		return errors.New("payments async overexposure proof requires exposure above max")
	}
	if proof.EvidenceHash != HashParts("async-overexposure", proof.CheckpointState.StateHash, ComputeAsyncDeltaRootForChannel(channel, proof.Deltas)) {
		return errors.New("payments async overexposure evidence hash mismatch")
	}
	return nil
}

func validateDeltaSignatureEnvelope(sig DeltaSignature, delta AsyncPaymentDelta) error {
	sig = sig.Normalize()
	delta = delta.Normalize()
	if sig.ChainID != delta.ChainID {
		return errors.New("payments async delta signature chain id mismatch")
	}
	if sig.ChannelID != delta.ChannelID {
		return errors.New("payments async delta signature channel id mismatch")
	}
	if sig.Nonce != delta.NonceStart {
		return errors.New("payments async delta signature nonce mismatch")
	}
	if sig.ExpirationHeight != delta.ExpiryHeight {
		return errors.New("payments async delta signature expiration height mismatch")
	}
	if sig.ObjectID != delta.UpdateID {
		return errors.New("payments async delta signature object id mismatch")
	}
	if sig.CommitmentHash != delta.DeltaHash {
		return errors.New("payments async delta signature commitment mismatch")
	}
	return nil
}

func normalizeAsyncDeltas(deltas []AsyncPaymentDelta) []AsyncPaymentDelta {
	out := make([]AsyncPaymentDelta, len(deltas))
	for i, delta := range deltas {
		out[i] = delta.Normalize()
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].UpdateID != out[j].UpdateID {
			return out[i].UpdateID < out[j].UpdateID
		}
		return out[i].DeltaHash < out[j].DeltaHash
	})
	return out
}

func validateAsyncDeltasForCheckpoint(channel ChannelRecord, base ChannelState, deltas []AsyncPaymentDelta, checkpointNonce, currentHeight uint64) error {
	if len(deltas) == 0 {
		return errors.New("payments async checkpoint requires signed deltas")
	}
	maxExposure, err := parsePositiveInt("payments async max unacked amount", base.MaxUnackedAmount)
	if err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(deltas))
	seenNonce := make(map[string]struct{}, len(deltas))
	exposureBySender := make(map[string]sdkmath.Int, len(channel.Participants))
	for _, delta := range normalizeAsyncDeltas(deltas) {
		if _, found := seen[delta.UpdateID]; found {
			return errors.New("payments duplicate async delta update id")
		}
		seen[delta.UpdateID] = struct{}{}
		if err := delta.ValidateForChannel(channel, currentHeight); err != nil {
			return err
		}
		if delta.NonceEnd-delta.NonceStart+1 > base.SendWindow {
			return errors.New("payments async delta exceeds send window")
		}
		if delta.NonceEnd > checkpointNonce {
			return errors.New("payments async delta nonce exceeds checkpoint")
		}
		if checkpointNonce-delta.NonceEnd > base.ReceiveWindow {
			return errors.New("payments async delta is outside receive window")
		}
		for nonce := delta.NonceStart; nonce <= delta.NonceEnd; nonce++ {
			key := fmt.Sprintf("%s/%d", delta.From, nonce)
			if _, found := seenNonce[key]; found {
				return errors.New("payments duplicate async delta nonce")
			}
			seenNonce[key] = struct{}{}
			if nonce == ^uint64(0) {
				break
			}
		}
		amount, err := parsePositiveInt("payments async delta amount", delta.Amount)
		if err != nil {
			return err
		}
		currentExposure, found := exposureBySender[delta.From]
		if !found {
			currentExposure = sdkmath.ZeroInt()
		}
		exposureBySender[delta.From] = currentExposure.Add(amount)
		if exposureBySender[delta.From].GT(maxExposure) {
			return errors.New("payments async max unacked exposure exceeded")
		}
	}
	return nil
}

func applyAsyncDeltas(baseBalances []Balance, deltas []AsyncPaymentDelta) ([]Balance, error) {
	amounts := make(map[string]sdkmath.Int, len(baseBalances))
	for _, balance := range normalizeBalances(baseBalances) {
		amount, err := parseNonNegativeInt("payments async base balance", balance.Amount)
		if err != nil {
			return nil, err
		}
		amounts[balance.Participant] = amount
	}
	for _, delta := range normalizeAsyncDeltas(deltas) {
		amount, err := parsePositiveInt("payments async delta amount", delta.Amount)
		if err != nil {
			return nil, err
		}
		fromBalance, found := amounts[delta.From]
		if !found {
			return nil, errors.New("payments async delta sender has no balance")
		}
		if fromBalance.LT(amount) {
			return nil, errors.New("payments async delta exceeds sender balance")
		}
		if _, found := amounts[delta.To]; !found {
			return nil, errors.New("payments async delta receiver has no balance")
		}
		amounts[delta.From] = fromBalance.Sub(amount)
		amounts[delta.To] = amounts[delta.To].Add(amount)
	}
	out := make([]Balance, 0, len(amounts))
	for participant, amount := range amounts {
		out = append(out, Balance{Participant: participant, Amount: amount.String()})
	}
	return normalizeBalances(out), nil
}
