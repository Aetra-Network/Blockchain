package types

import (
	"errors"
	"strings"

	sdkmath "cosmossdk.io/math"

	"github.com/sovereign-l1/l1/app/addressing"
)

type ClaimSignature struct {
	Signer           string
	ChainID          string
	ChannelID        string
	ObjectType       string
	Version          uint32
	Nonce            uint64
	ObjectID         string
	ExpirationHeight uint64
	CommitmentHash   string
	ClaimHash        string
	SignatureHash    string
}

type UnidirectionalClaim struct {
	ChainID             string
	ChannelID           string
	Payer               string
	Receiver            string
	LockedAmount        string
	ClaimedAmount       string
	Nonce               uint64
	ExpirationHeight    uint64
	ExpirationTimestamp int64
	StateHash           string
	PayerSignature      ClaimSignature
	ReceiverAckOptional ClaimSignature
}

type StreamingPaymentFrame struct {
	ChannelID           string
	StreamID            string
	Payer               string
	Receiver            string
	PreviousClaimed     string
	RatePerBlock        string
	StartHeight         uint64
	CurrentHeight       uint64
	Nonce               uint64
	ExpirationHeight    uint64
	ExpirationTimestamp int64
}

func BuildUnidirectionalClaim(claim UnidirectionalClaim) (UnidirectionalClaim, error) {
	claim = claim.Normalize()
	if err := validateUnsignedUnidirectionalClaim(claim); err != nil {
		return UnidirectionalClaim{}, err
	}
	claim.StateHash = ComputeUnidirectionalClaimHash(claim)
	return claim, nil
}

func SignatureForClaim(claim UnidirectionalClaim, signer string) (ClaimSignature, error) {
	if claim.StateHash == "" {
		var err error
		claim, err = BuildUnidirectionalClaim(claim)
		if err != nil {
			return ClaimSignature{}, err
		}
	}
	signer = strings.TrimSpace(signer)
	if err := addressing.ValidateUserAddress("payments claim signer", signer); err != nil {
		return ClaimSignature{}, err
	}
	return ClaimSignature{
		Signer:           signer,
		ChainID:          claim.ChainID,
		ChannelID:        claim.ChannelID,
		ObjectType:       SignatureObjectClaim,
		Version:          CurrentStateVersion,
		Nonce:            claim.Nonce,
		ObjectID:         claim.StateHash,
		ExpirationHeight: claim.ExpirationHeight,
		CommitmentHash:   claim.StateHash,
		ClaimHash:        claim.StateHash,
		SignatureHash: ComputeSignatureEnvelopeHash(
			signer,
			claim.ChainID,
			claim.ChannelID,
			SignatureObjectClaim,
			CurrentStateVersion,
			claim.Nonce,
			claim.StateHash,
			claim.ExpirationHeight,
			claim.StateHash,
		),
	}, nil
}

func StreamingClaimForChannel(channel ChannelRecord, frame StreamingPaymentFrame) (UnidirectionalClaim, error) {
	channel = channel.Normalize()
	frame = frame.Normalize()
	if channel.ChannelType != ChannelTypeUnidirectional {
		return UnidirectionalClaim{}, errors.New("payments streaming claim requires unidirectional channel")
	}
	if frame.ChannelID != channel.ChannelID {
		return UnidirectionalClaim{}, errors.New("payments streaming claim channel mismatch")
	}
	if frame.Payer != channel.Payer || frame.Receiver != channel.Receiver {
		return UnidirectionalClaim{}, errors.New("payments streaming claim parties mismatch")
	}
	if frame.CurrentHeight < frame.StartHeight {
		return UnidirectionalClaim{}, errors.New("payments streaming current height must be >= start height")
	}
	previous, err := parseNonNegativeInt("payments streaming previous claimed", frame.PreviousClaimed)
	if err != nil {
		return UnidirectionalClaim{}, err
	}
	rate, err := parseNonNegativeInt("payments streaming rate", frame.RatePerBlock)
	if err != nil {
		return UnidirectionalClaim{}, err
	}
	if frame.CurrentHeight-frame.StartHeight > uint64(^uint(0)>>1) {
		return UnidirectionalClaim{}, errors.New("payments streaming elapsed height is too large")
	}
	elapsed := sdkmath.NewInt(int64(frame.CurrentHeight - frame.StartHeight))
	claimed := previous.Add(rate.Mul(elapsed))
	collateral, err := parsePositiveInt("payments channel collateral", channel.Collateral)
	if err != nil {
		return UnidirectionalClaim{}, err
	}
	if claimed.GT(collateral) {
		claimed = collateral
	}
	return BuildUnidirectionalClaim(UnidirectionalClaim{
		ChainID:             channel.ChainID,
		ChannelID:           channel.ChannelID,
		Payer:               channel.Payer,
		Receiver:            channel.Receiver,
		LockedAmount:        channel.Collateral,
		ClaimedAmount:       claimed.String(),
		Nonce:               frame.Nonce,
		ExpirationHeight:    frame.ExpirationHeight,
		ExpirationTimestamp: frame.ExpirationTimestamp,
	})
}

func (s ClaimSignature) Normalize() ClaimSignature {
	s.Signer = strings.TrimSpace(s.Signer)
	s.ChainID = strings.TrimSpace(s.ChainID)
	s.ChannelID = normalizeHash(s.ChannelID)
	s.ObjectType = strings.TrimSpace(s.ObjectType)
	s.ObjectID = strings.TrimSpace(s.ObjectID)
	s.CommitmentHash = normalizeHash(s.CommitmentHash)
	s.ClaimHash = normalizeHash(s.ClaimHash)
	s.SignatureHash = normalizeHash(s.SignatureHash)
	return s
}

func (s ClaimSignature) Validate(expectedClaimHash string) error {
	s = s.Normalize()
	if err := addressing.ValidateUserAddress("payments claim signature signer", s.Signer); err != nil {
		return err
	}
	if s.ClaimHash != expectedClaimHash {
		return errors.New("payments claim signature hash mismatch")
	}
	if s.ObjectType != SignatureObjectClaim {
		return errors.New("payments claim signature object type mismatch")
	}
	if s.Version != CurrentStateVersion {
		return errors.New("payments claim signature version mismatch")
	}
	if s.ObjectID != s.ClaimHash {
		return errors.New("payments claim signature object id mismatch")
	}
	if s.CommitmentHash != s.ClaimHash {
		return errors.New("payments claim signature commitment mismatch")
	}
	if err := ValidateHash("payments claim signature hash", s.SignatureHash); err != nil {
		return err
	}
	if expected := ComputeSignatureEnvelopeHash(s.Signer, s.ChainID, s.ChannelID, s.ObjectType, s.Version, s.Nonce, s.ObjectID, s.ExpirationHeight, s.CommitmentHash); s.SignatureHash != expected {
		return errors.New("payments claim signature value mismatch")
	}
	return nil
}

func (c UnidirectionalClaim) Normalize() UnidirectionalClaim {
	c.ChainID = strings.TrimSpace(c.ChainID)
	c.ChannelID = normalizeHash(c.ChannelID)
	c.Payer = strings.TrimSpace(c.Payer)
	c.Receiver = strings.TrimSpace(c.Receiver)
	c.LockedAmount = strings.TrimSpace(c.LockedAmount)
	c.ClaimedAmount = strings.TrimSpace(c.ClaimedAmount)
	c.StateHash = normalizeOptionalHash(c.StateHash)
	c.PayerSignature = c.PayerSignature.Normalize()
	c.ReceiverAckOptional = c.ReceiverAckOptional.Normalize()
	return c
}

func (c UnidirectionalClaim) IsZero() bool {
	c = c.Normalize()
	return c.ChannelID == "" && c.StateHash == ""
}

func (c UnidirectionalClaim) ValidateForChannel(channel ChannelRecord) error {
	channel = channel.Normalize()
	if err := channel.ValidateCore(); err != nil {
		return err
	}
	if channel.ChannelType != ChannelTypeUnidirectional {
		return errors.New("payments claim requires unidirectional channel")
	}
	claim := c.Normalize()
	if err := validateUnsignedUnidirectionalClaim(claim); err != nil {
		return err
	}
	if claim.ChainID != channel.ChainID {
		return errors.New("payments claim chain id mismatch")
	}
	if claim.ChannelID != channel.ChannelID {
		return errors.New("payments claim channel mismatch")
	}
	if claim.Payer != channel.Payer || claim.Receiver != channel.Receiver {
		return errors.New("payments claim parties mismatch")
	}
	locked, err := parsePositiveInt("payments claim locked amount", claim.LockedAmount)
	if err != nil {
		return err
	}
	collateral, err := parsePositiveInt("payments channel collateral", channel.Collateral)
	if err != nil {
		return err
	}
	if !locked.Equal(collateral) {
		return errors.New("payments claim locked amount mismatch")
	}
	claimed, err := parseNonNegativeInt("payments claimed amount", claim.ClaimedAmount)
	if err != nil {
		return err
	}
	if claimed.GT(collateral) {
		return errors.New("payments claimed amount exceeds locked collateral")
	}
	if claim.StateHash == "" {
		return errors.New("payments claim state hash is required")
	}
	if expected := ComputeUnidirectionalClaimHash(claim); claim.StateHash != expected {
		return errors.New("payments claim state hash mismatch")
	}
	if err := claim.PayerSignature.Validate(claim.StateHash); err != nil {
		return err
	}
	if err := validateClaimSignatureEnvelope(claim.PayerSignature, claim); err != nil {
		return err
	}
	if claim.PayerSignature.Signer != channel.Payer {
		return errors.New("payments claim payer signature is required")
	}
	if claim.ReceiverAckOptional.SignatureHash == "" {
		if channel.ReceiverAckRequired {
			return errors.New("payments receiver acknowledgement is required")
		}
		return nil
	}
	if err := claim.ReceiverAckOptional.Validate(claim.StateHash); err != nil {
		return err
	}
	if err := validateClaimSignatureEnvelope(claim.ReceiverAckOptional, claim); err != nil {
		return err
	}
	if claim.ReceiverAckOptional.Signer != channel.Receiver {
		return errors.New("payments receiver acknowledgement signer mismatch")
	}
	return nil
}

func (f StreamingPaymentFrame) Normalize() StreamingPaymentFrame {
	f.ChannelID = normalizeHash(f.ChannelID)
	f.StreamID = normalizeHash(f.StreamID)
	f.Payer = strings.TrimSpace(f.Payer)
	f.Receiver = strings.TrimSpace(f.Receiver)
	f.PreviousClaimed = strings.TrimSpace(f.PreviousClaimed)
	f.RatePerBlock = strings.TrimSpace(f.RatePerBlock)
	return f
}

func validateUnsignedUnidirectionalClaim(claim UnidirectionalClaim) error {
	if strings.TrimSpace(claim.ChainID) == "" {
		return errors.New("payments claim chain id is required")
	}
	if err := ValidateHash("payments claim channel id", claim.ChannelID); err != nil {
		return err
	}
	if err := addressing.ValidateUserAddress("payments claim payer", claim.Payer); err != nil {
		return err
	}
	if err := addressing.ValidateUserAddress("payments claim receiver", claim.Receiver); err != nil {
		return err
	}
	if claim.Payer == claim.Receiver {
		return errors.New("payments claim parties must differ")
	}
	if err := validatePositiveInt("payments claim locked amount", claim.LockedAmount); err != nil {
		return err
	}
	if err := validateNonNegativeInt("payments claim claimed amount", claim.ClaimedAmount); err != nil {
		return err
	}
	if claim.Nonce == 0 {
		return errors.New("payments claim nonce must be positive")
	}
	if claim.ExpirationHeight == 0 {
		return errors.New("payments claim expiration height must be positive")
	}
	if claim.ExpirationTimestamp < 0 {
		return errors.New("payments claim expiration timestamp must be non-negative")
	}
	return nil
}

func validateUnidirectionalChannelCore(channel ChannelRecord) error {
	if len(channel.Participants) != 2 {
		return errors.New("payments unidirectional channel requires exactly two participants")
	}
	if err := addressing.ValidateUserAddress("payments unidirectional payer", channel.Payer); err != nil {
		return err
	}
	if err := addressing.ValidateUserAddress("payments unidirectional receiver", channel.Receiver); err != nil {
		return err
	}
	if channel.Payer == channel.Receiver {
		return errors.New("payments unidirectional parties must differ")
	}
	if !containsString(channel.Participants, channel.Payer) || !containsString(channel.Participants, channel.Receiver) {
		return errors.New("payments unidirectional parties must be channel participants")
	}
	if channel.ExpirationHeight == 0 {
		return errors.New("payments unidirectional expiration height must be positive")
	}
	if channel.ExpirationTimestamp < 0 {
		return errors.New("payments unidirectional expiration timestamp must be non-negative")
	}
	return nil
}

func validateUnidirectionalOpeningState(channel ChannelRecord) error {
	if channel.LatestState.TimeoutHeight != channel.ExpirationHeight {
		return errors.New("payments unidirectional opening state expiration height mismatch")
	}
	if channel.LatestState.TimeoutTimestamp != channel.ExpirationTimestamp {
		return errors.New("payments unidirectional opening state expiration timestamp mismatch")
	}
	balanceByParticipant := map[string]string{}
	for _, balance := range channel.LatestState.Balances {
		balanceByParticipant[balance.Participant] = balance.Amount
	}
	payerBalance, err := parseNonNegativeInt("payments unidirectional payer opening balance", balanceByParticipant[channel.Payer])
	if err != nil {
		return err
	}
	collateral, err := parsePositiveInt("payments channel collateral", channel.Collateral)
	if err != nil {
		return err
	}
	if !payerBalance.Equal(collateral) {
		return errors.New("payments unidirectional payer must lock full collateral on open")
	}
	receiverBalance, err := parseNonNegativeInt("payments unidirectional receiver opening balance", balanceByParticipant[channel.Receiver])
	if err != nil {
		return err
	}
	if !receiverBalance.IsZero() {
		return errors.New("payments unidirectional receiver opening balance must be zero")
	}
	return nil
}

func validateUnidirectionalClaimProgress(previous, next UnidirectionalClaim) error {
	if previous.IsZero() {
		return nil
	}
	previous = previous.Normalize()
	next = next.Normalize()
	if next.Nonce <= previous.Nonce {
		return errors.New("payments claim nonce must strictly increase")
	}
	previousClaimed, err := parseNonNegativeInt("payments previous claimed amount", previous.ClaimedAmount)
	if err != nil {
		return err
	}
	nextClaimed, err := parseNonNegativeInt("payments claimed amount", next.ClaimedAmount)
	if err != nil {
		return err
	}
	if nextClaimed.LT(previousClaimed) {
		return errors.New("payments claimed amount must not decrease")
	}
	return nil
}

func validateClaimSignatureEnvelope(sig ClaimSignature, claim UnidirectionalClaim) error {
	sig = sig.Normalize()
	claim = claim.Normalize()
	if sig.ChainID != claim.ChainID {
		return errors.New("payments claim signature chain id mismatch")
	}
	if sig.ChannelID != claim.ChannelID {
		return errors.New("payments claim signature channel id mismatch")
	}
	if sig.Nonce != claim.Nonce {
		return errors.New("payments claim signature nonce mismatch")
	}
	if sig.ExpirationHeight != claim.ExpirationHeight {
		return errors.New("payments claim signature expiration height mismatch")
	}
	if sig.ObjectID != claim.StateHash {
		return errors.New("payments claim signature object id mismatch")
	}
	if sig.CommitmentHash != claim.StateHash {
		return errors.New("payments claim signature commitment mismatch")
	}
	return nil
}
