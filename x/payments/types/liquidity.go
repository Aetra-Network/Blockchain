package types

import (
	"errors"
	"fmt"
	"strings"

	"github.com/sovereign-l1/l1/app/addressing"
)

const (
	RouteFailureCapacity        RouteFailureClass = "CAPACITY"
	RouteFailureTimeout         RouteFailureClass = "TIMEOUT"
	RouteFailureCongestion      RouteFailureClass = "CONGESTION"
	RouteFailureLiquidityStale  RouteFailureClass = "LIQUIDITY_STALE"
	RouteFailureNodeUnavailable RouteFailureClass = "NODE_UNAVAILABLE"
	RouteFailurePolicyRejected  RouteFailureClass = "POLICY_REJECTED"
	RouteFailureUnknown         RouteFailureClass = "UNKNOWN"
)

type LiquidityAdvertisement struct {
	AdvertisementID     string
	ChannelID           string
	Advertiser          string
	Counterparty        string
	Capacity            string
	FeeDenom            string
	BaseFee             string
	ReservationFee      string
	VirtualSetupFee     string
	ReliabilityBps      uint32
	ValidUntilHeight    uint64
	DepositAmount       string
	BackedByReservation bool
	AdvertisementHash   string
}

type LiquidityReservationSignature struct {
	Signer           string
	ChainID          string
	ChannelID        string
	ObjectType       string
	Version          uint32
	Nonce            uint64
	ObjectID         string
	ExpirationHeight uint64
	CommitmentHash   string
	SignatureHash    string
}

type SignedLiquidityReservation struct {
	ReservationID    string
	AdvertisementID  string
	ChainID          string
	ChannelID        string
	Reserver         string
	Counterparty     string
	Capacity         string
	FeeAmount        string
	ExpirationHeight uint64
	Nonce            uint64
	CommitmentHash   string
	Signature        LiquidityReservationSignature
}

func BuildLiquidityAdvertisement(ad LiquidityAdvertisement, requiredDeposit string) (LiquidityAdvertisement, error) {
	ad = ad.Normalize()
	if ad.AdvertisementID == "" {
		ad.AdvertisementID = HashParts("liquidity-advertisement", ad.ChannelID, ad.Advertiser, ad.Counterparty, ad.Capacity, fmt.Sprintf("%020d", ad.ValidUntilHeight))
	}
	ad.AdvertisementHash = ""
	ad.AdvertisementHash = ComputeLiquidityAdvertisementHash(ad)
	if err := ad.Validate(requiredDeposit); err != nil {
		return LiquidityAdvertisement{}, err
	}
	return ad.Normalize(), nil
}

func ComputeLiquidityAdvertisementHash(ad LiquidityAdvertisement) string {
	ad = ad.Normalize()
	return HashParts(
		"liquidity-advertisement",
		ad.AdvertisementID,
		ad.ChannelID,
		ad.Advertiser,
		ad.Counterparty,
		ad.Capacity,
		ad.FeeDenom,
		ad.BaseFee,
		ad.ReservationFee,
		ad.VirtualSetupFee,
		fmt.Sprintf("%010d", ad.ReliabilityBps),
		fmt.Sprintf("%020d", ad.ValidUntilHeight),
		ad.DepositAmount,
		fmt.Sprintf("%t", ad.BackedByReservation),
	)
}

func (ad LiquidityAdvertisement) Normalize() LiquidityAdvertisement {
	ad.AdvertisementID = normalizeOptionalHash(ad.AdvertisementID)
	ad.ChannelID = normalizeHash(ad.ChannelID)
	ad.Advertiser = strings.TrimSpace(ad.Advertiser)
	ad.Counterparty = strings.TrimSpace(ad.Counterparty)
	ad.Capacity = strings.TrimSpace(ad.Capacity)
	ad.FeeDenom = normalizeAssetDenom(ad.FeeDenom)
	ad.BaseFee = strings.TrimSpace(ad.BaseFee)
	ad.ReservationFee = strings.TrimSpace(ad.ReservationFee)
	ad.VirtualSetupFee = strings.TrimSpace(ad.VirtualSetupFee)
	ad.DepositAmount = strings.TrimSpace(ad.DepositAmount)
	for _, field := range []*string{&ad.BaseFee, &ad.ReservationFee, &ad.VirtualSetupFee, &ad.DepositAmount} {
		if *field == "" {
			*field = "0"
		}
	}
	ad.AdvertisementHash = normalizeOptionalHash(ad.AdvertisementHash)
	return ad
}

func (ad LiquidityAdvertisement) Validate(requiredDeposit string) error {
	ad = ad.Normalize()
	if err := ValidateHash("payments liquidity advertisement id", ad.AdvertisementID); err != nil {
		return err
	}
	if err := ValidateHash("payments liquidity advertisement channel id", ad.ChannelID); err != nil {
		return err
	}
	if err := addressing.ValidateUserAddress("payments liquidity advertiser", ad.Advertiser); err != nil {
		return err
	}
	if err := addressing.ValidateUserAddress("payments liquidity counterparty", ad.Counterparty); err != nil {
		return err
	}
	if ad.Advertiser == ad.Counterparty {
		return errors.New("payments liquidity advertisement parties must differ")
	}
	if err := validatePositiveInt("payments liquidity advertised capacity", ad.Capacity); err != nil {
		return err
	}
	if ad.FeeDenom != NativeDenom {
		return fmt.Errorf("payments liquidity advertisement fee denom must be %s", NativeDenom)
	}
	for _, item := range []struct {
		name   string
		amount string
	}{
		{"payments liquidity base fee", ad.BaseFee},
		{"payments liquidity reservation fee", ad.ReservationFee},
		{"payments liquidity virtual setup fee", ad.VirtualSetupFee},
		{"payments liquidity advertisement deposit", ad.DepositAmount},
	} {
		if err := validateNonNegativeInt(item.name, item.amount); err != nil {
			return err
		}
	}
	if ad.ReliabilityBps > MaxPenaltyRouteBps {
		return errors.New("payments liquidity reliability bps exceeds 10000")
	}
	if ad.ValidUntilHeight == 0 {
		return errors.New("payments liquidity advertisement validity height must be positive")
	}
	requiredDeposit = strings.TrimSpace(requiredDeposit)
	if requiredDeposit != "" {
		required, err := parseNonNegativeInt("payments liquidity required deposit", requiredDeposit)
		if err != nil {
			return err
		}
		deposit, err := parseNonNegativeInt("payments liquidity advertisement deposit", ad.DepositAmount)
		if err != nil {
			return err
		}
		if deposit.LT(required) {
			return errors.New("payments liquidity advertisement deposit below required")
		}
	}
	if ad.AdvertisementHash == "" {
		return errors.New("payments liquidity advertisement hash is required")
	}
	if expected := ComputeLiquidityAdvertisementHash(ad); ad.AdvertisementHash != expected {
		return errors.New("payments liquidity advertisement hash mismatch")
	}
	return nil
}

func LiquidityAvailabilityScore(ad LiquidityAdvertisement, stats EdgeRoutingStats) (int64, error) {
	ad = ad.Normalize()
	if err := ad.Validate("0"); err != nil {
		return 0, err
	}
	capacity, err := parsePositiveInt("payments liquidity score capacity", ad.Capacity)
	if err != nil {
		return 0, err
	}
	score := capacity.QuoRaw(10).Int64()
	score += int64(ad.ReliabilityBps) / 100
	if ad.BackedByReservation {
		score += 100
	}
	stats = stats.Normalize()
	score += int64(stats.SuccessRateBps) / 200
	score -= int64(stats.FailureCount) * 25
	score -= int64(stats.CongestionBps) / 200
	score -= int64(stats.PendingConditionCount) * 5
	return score, nil
}

func ApplyFalseLiquidityAdvertisementPenalty(store TopologyStore, ad LiquidityAdvertisement, currentHeight uint64) (TopologyStore, string, error) {
	ad = ad.Normalize()
	if err := ad.Validate("0"); err != nil {
		return TopologyStore{}, "", err
	}
	forfeited, err := parseNonNegativeInt("payments false liquidity deposit", ad.DepositAmount)
	if err != nil {
		return TopologyStore{}, "", err
	}
	next := PenalizeInvalidGossip(store, ad.Advertiser, currentHeight)
	return next, forfeited.String(), nil
}

func BuildSignedLiquidityReservation(reservation SignedLiquidityReservation, signer string) (SignedLiquidityReservation, error) {
	reservation = reservation.Normalize()
	if reservation.ReservationID == "" {
		reservation.ReservationID = HashParts("liquidity-reservation", reservation.AdvertisementID, reservation.ChannelID, reservation.Reserver, fmt.Sprintf("%020d", reservation.Nonce))
	}
	reservation.CommitmentHash = ""
	reservation.Signature = LiquidityReservationSignature{}
	reservation.CommitmentHash = ComputeLiquidityReservationHash(reservation)
	signature, err := SignatureForLiquidityReservation(reservation, signer)
	if err != nil {
		return SignedLiquidityReservation{}, err
	}
	reservation.Signature = signature
	if err := reservation.Validate(); err != nil {
		return SignedLiquidityReservation{}, err
	}
	return reservation.Normalize(), nil
}

func ComputeLiquidityReservationHash(reservation SignedLiquidityReservation) string {
	reservation = reservation.Normalize()
	return HashParts(
		"liquidity-reservation",
		reservation.ReservationID,
		reservation.AdvertisementID,
		reservation.ChainID,
		reservation.ChannelID,
		reservation.Reserver,
		reservation.Counterparty,
		reservation.Capacity,
		reservation.FeeAmount,
		fmt.Sprintf("%020d", reservation.ExpirationHeight),
		fmt.Sprintf("%020d", reservation.Nonce),
	)
}

func SignatureForLiquidityReservation(reservation SignedLiquidityReservation, signer string) (LiquidityReservationSignature, error) {
	reservation = reservation.Normalize()
	signer = strings.TrimSpace(signer)
	if err := addressing.ValidateUserAddress("payments liquidity reservation signer", signer); err != nil {
		return LiquidityReservationSignature{}, err
	}
	if reservation.CommitmentHash == "" {
		reservation.CommitmentHash = ComputeLiquidityReservationHash(reservation)
	}
	return LiquidityReservationSignature{
		Signer:           signer,
		ChainID:          reservation.ChainID,
		ChannelID:        reservation.ChannelID,
		ObjectType:       SignatureObjectLiquidity,
		Version:          CurrentStateVersion,
		Nonce:            reservation.Nonce,
		ObjectID:         reservation.ReservationID,
		ExpirationHeight: reservation.ExpirationHeight,
		CommitmentHash:   reservation.CommitmentHash,
		SignatureHash: ComputeSignatureEnvelopeHash(
			signer,
			reservation.ChainID,
			reservation.ChannelID,
			SignatureObjectLiquidity,
			CurrentStateVersion,
			reservation.Nonce,
			reservation.ReservationID,
			reservation.ExpirationHeight,
			reservation.CommitmentHash,
		),
	}, nil
}

func (r SignedLiquidityReservation) Normalize() SignedLiquidityReservation {
	r.ReservationID = normalizeOptionalHash(r.ReservationID)
	r.AdvertisementID = normalizeHash(r.AdvertisementID)
	r.ChainID = strings.TrimSpace(r.ChainID)
	r.ChannelID = normalizeHash(r.ChannelID)
	r.Reserver = strings.TrimSpace(r.Reserver)
	r.Counterparty = strings.TrimSpace(r.Counterparty)
	r.Capacity = strings.TrimSpace(r.Capacity)
	r.FeeAmount = strings.TrimSpace(r.FeeAmount)
	if r.FeeAmount == "" {
		r.FeeAmount = "0"
	}
	r.CommitmentHash = normalizeOptionalHash(r.CommitmentHash)
	r.Signature = r.Signature.Normalize()
	return r
}

func (r SignedLiquidityReservation) Validate() error {
	reservation := r.Normalize()
	if err := ValidateHash("payments liquidity reservation id", reservation.ReservationID); err != nil {
		return err
	}
	if err := ValidateHash("payments liquidity reservation advertisement id", reservation.AdvertisementID); err != nil {
		return err
	}
	if reservation.ChainID == "" {
		return errors.New("payments liquidity reservation chain id is required")
	}
	if err := ValidateHash("payments liquidity reservation channel id", reservation.ChannelID); err != nil {
		return err
	}
	if err := addressing.ValidateUserAddress("payments liquidity reserver", reservation.Reserver); err != nil {
		return err
	}
	if err := addressing.ValidateUserAddress("payments liquidity reservation counterparty", reservation.Counterparty); err != nil {
		return err
	}
	if reservation.Reserver == reservation.Counterparty {
		return errors.New("payments liquidity reservation parties must differ")
	}
	if err := validatePositiveInt("payments liquidity reservation capacity", reservation.Capacity); err != nil {
		return err
	}
	if err := validateNonNegativeInt("payments liquidity reservation fee", reservation.FeeAmount); err != nil {
		return err
	}
	if reservation.ExpirationHeight == 0 || reservation.Nonce == 0 {
		return errors.New("payments liquidity reservation expiration and nonce must be positive")
	}
	if reservation.CommitmentHash == "" {
		return errors.New("payments liquidity reservation commitment is required")
	}
	if expected := ComputeLiquidityReservationHash(reservation); reservation.CommitmentHash != expected {
		return errors.New("payments liquidity reservation commitment mismatch")
	}
	return reservation.Signature.Validate(reservation)
}

func (s LiquidityReservationSignature) Normalize() LiquidityReservationSignature {
	s.Signer = strings.TrimSpace(s.Signer)
	s.ChainID = strings.TrimSpace(s.ChainID)
	s.ChannelID = normalizeHash(s.ChannelID)
	s.ObjectType = strings.TrimSpace(s.ObjectType)
	s.ObjectID = normalizeOptionalHash(s.ObjectID)
	s.CommitmentHash = normalizeOptionalHash(s.CommitmentHash)
	s.SignatureHash = normalizeOptionalHash(s.SignatureHash)
	return s
}

func (s LiquidityReservationSignature) Validate(reservation SignedLiquidityReservation) error {
	sig := s.Normalize()
	reservation = reservation.Normalize()
	if err := addressing.ValidateUserAddress("payments liquidity reservation signature signer", sig.Signer); err != nil {
		return err
	}
	if sig.Signer != reservation.Reserver {
		return errors.New("payments liquidity reservation signer mismatch")
	}
	if sig.ChainID != reservation.ChainID || sig.ChannelID != reservation.ChannelID {
		return errors.New("payments liquidity reservation signature domain mismatch")
	}
	if sig.ObjectType != SignatureObjectLiquidity || sig.Version != CurrentStateVersion {
		return errors.New("payments liquidity reservation signature object mismatch")
	}
	if sig.Nonce != reservation.Nonce || sig.ObjectID != reservation.ReservationID || sig.ExpirationHeight != reservation.ExpirationHeight || sig.CommitmentHash != reservation.CommitmentHash {
		return errors.New("payments liquidity reservation signature commitment mismatch")
	}
	if err := ValidateHash("payments liquidity reservation signature hash", sig.SignatureHash); err != nil {
		return err
	}
	expected := ComputeSignatureEnvelopeHash(sig.Signer, sig.ChainID, sig.ChannelID, sig.ObjectType, sig.Version, sig.Nonce, sig.ObjectID, sig.ExpirationHeight, sig.CommitmentHash)
	if sig.SignatureHash != expected {
		return errors.New("payments liquidity reservation signature value mismatch")
	}
	return nil
}
