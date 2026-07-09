package types

import (
	"errors"
	"fmt"
	"strings"

	"github.com/sovereign-l1/l1/app/addressing"
)

type PaymentFeeClass string

type PaymentFeeSchedule struct {
	Denom                           string
	ChannelOpenFee                  string
	ChannelOpenPerParticipantFee    string
	ChannelCheckpointFee            string
	CooperativeCloseFee             string
	UnilateralCloseFee              string
	DisputeFee                      string
	FraudProofVerificationFee       string
	ConditionalPromiseSettlementFee string
	VirtualChannelAnchorFee         string
	RoutingAdvertisementFee         string
	RoutingAdvertisementDeposit     string
	ConditionalCapabilitySurcharge  string
	VirtualChannelAnchorSurcharge   string
	StorageByteFee                  string
	StorageFeeEnabled               bool
	OpenFeeMin                      string
	OpenFeeMax                      string
	StorageRentPerBlock             string
	RenewalPeriod                   uint64
	BaseMultiplierBps               uint32
	MaxMultiplierBps                uint32
}

type PaymentFeeMultiplier struct {
	FeeClass      PaymentFeeClass
	MultiplierBps uint32
	CongestionBps uint32
	UpdatedHeight uint64
}

type PaymentFeeCharge struct {
	FeeID          string
	FeeClass       PaymentFeeClass
	ChannelID      string
	ObjectID       string
	Payer          string
	Denom          string
	Amount         string
	RequiredAmount string
	StorageBytes   uint64
	MultiplierBps  uint32
	Height         uint64
	Refunded       bool
}

type PaymentFeeRefund struct {
	RefundID  string
	FeeID     string
	Recipient string
	Denom     string
	Amount    string
	Reason    string
	Height    uint64
}

type SecurityReserveAllocationHook struct {
	HookID     string
	ChannelID  string
	ProofID    string
	Offender   string
	Denom      string
	Amount     string
	Height     uint64
	Route      PenaltyRoute
	Allocation string
}

type SettlementInclusionLatency struct {
	RecordID        string
	OperationID     string
	ChannelID       string
	Operation       SettlementArbitrationOperation
	SubmittedHeight uint64
	IncludedHeight  uint64
	LatencyBlocks   uint64
	SLOThreshold    uint64
	Breached        bool
}

type SettlementGasCostSchedule struct {
	OpenGas                 uint64
	CooperativeCloseGas     uint64
	UnilateralCloseGas      uint64
	DisputeGas              uint64
	FraudProofGas           uint64
	ConditionResolutionGas  uint64
	PenaltyRoutingGas       uint64
	FinalSettlementGas      uint64
	ReplayProtectionGas     uint64
	PerSignatureGas         uint64
	PerConditionGas         uint64
	PerFraudProofGas        uint64
	PerPenaltyAllocationGas uint64
	PerStateByteGas         uint64
}

type SettlementGasEstimate struct {
	Operation            SettlementArbitrationOperation
	BaseGas              uint64
	SignatureGas         uint64
	ConditionGas         uint64
	FraudProofGas        uint64
	PenaltyAllocationGas uint64
	StateByteGas         uint64
	TotalGas             uint64
}

type ChannelOpenFeeFormula struct {
	Denom                  string
	BaseFee                string
	ParticipantFee         string
	ParticipantCount       uint64
	StorageByteFee         string
	StorageBytes           uint64
	StorageFee             string
	ConditionalSurcharge   string
	VirtualAnchorSurcharge string
	RoutingDeposit         string
	RentReserve            string
	MultiplierBps          uint32
	MinFee                 string
	MaxFee                 string
	TotalFee               string
}

func DefaultPaymentFeeSchedule() PaymentFeeSchedule {
	return PaymentFeeSchedule{
		Denom:                           NativeDenom,
		ChannelOpenFee:                  DefaultOpeningFee,
		ChannelOpenPerParticipantFee:    "0",
		ChannelCheckpointFee:            "0",
		CooperativeCloseFee:             "0",
		UnilateralCloseFee:              "0",
		DisputeFee:                      "0",
		FraudProofVerificationFee:       "0",
		ConditionalPromiseSettlementFee: "0",
		VirtualChannelAnchorFee:         "0",
		RoutingAdvertisementFee:         "0",
		RoutingAdvertisementDeposit:     "0",
		ConditionalCapabilitySurcharge:  "0",
		VirtualChannelAnchorSurcharge:   "0",
		StorageByteFee:                  "0",
		OpenFeeMin:                      DefaultOpeningFee,
		OpenFeeMax:                      "0",
		StorageRentPerBlock:             "0",
		BaseMultiplierBps:               10_000,
		MaxMultiplierBps:                100_000,
	}
}

func (s PaymentFeeSchedule) Normalize() PaymentFeeSchedule {
	defaults := DefaultPaymentFeeSchedule()
	s.Denom = normalizeAssetDenom(s.Denom)
	if s.Denom == "" {
		s.Denom = defaults.Denom
	}
	fields := []*string{
		&s.ChannelOpenFee,
		&s.ChannelOpenPerParticipantFee,
		&s.ChannelCheckpointFee,
		&s.CooperativeCloseFee,
		&s.UnilateralCloseFee,
		&s.DisputeFee,
		&s.FraudProofVerificationFee,
		&s.ConditionalPromiseSettlementFee,
		&s.VirtualChannelAnchorFee,
		&s.RoutingAdvertisementFee,
		&s.RoutingAdvertisementDeposit,
		&s.ConditionalCapabilitySurcharge,
		&s.VirtualChannelAnchorSurcharge,
		&s.StorageByteFee,
		&s.OpenFeeMin,
		&s.OpenFeeMax,
		&s.StorageRentPerBlock,
	}
	for _, field := range fields {
		*field = strings.TrimSpace(*field)
		if *field == "" {
			*field = "0"
		}
	}
	if s.ChannelOpenFee == "0" {
		s.ChannelOpenFee = defaults.ChannelOpenFee
	}
	if s.OpenFeeMin == "0" {
		s.OpenFeeMin = defaults.OpenFeeMin
	}
	if s.BaseMultiplierBps == 0 {
		s.BaseMultiplierBps = defaults.BaseMultiplierBps
	}
	if s.MaxMultiplierBps == 0 {
		s.MaxMultiplierBps = defaults.MaxMultiplierBps
	}
	return s
}

func (s PaymentFeeSchedule) Validate() error {
	s = s.Normalize()
	if s.Denom != NativeDenom {
		return fmt.Errorf("payments fee schedule denom must be %s", NativeDenom)
	}
	for _, item := range []struct {
		name   string
		amount string
	}{
		{"payments channel open fee", s.ChannelOpenFee},
		{"payments channel open per participant fee", s.ChannelOpenPerParticipantFee},
		{"payments checkpoint fee", s.ChannelCheckpointFee},
		{"payments cooperative close fee", s.CooperativeCloseFee},
		{"payments unilateral close fee", s.UnilateralCloseFee},
		{"payments dispute fee", s.DisputeFee},
		{"payments fraud proof verification fee", s.FraudProofVerificationFee},
		{"payments conditional promise settlement fee", s.ConditionalPromiseSettlementFee},
		{"payments virtual channel anchor fee", s.VirtualChannelAnchorFee},
		{"payments routing advertisement fee", s.RoutingAdvertisementFee},
		{"payments routing advertisement deposit", s.RoutingAdvertisementDeposit},
		{"payments conditional capability surcharge", s.ConditionalCapabilitySurcharge},
		{"payments virtual channel anchor surcharge", s.VirtualChannelAnchorSurcharge},
		{"payments storage byte fee", s.StorageByteFee},
		{"payments open fee minimum", s.OpenFeeMin},
		{"payments open fee maximum", s.OpenFeeMax},
		{"payments storage rent per block", s.StorageRentPerBlock},
	} {
		if err := validateNonNegativeInt(item.name, item.amount); err != nil {
			return err
		}
	}
	if s.OpenFeeMax != "0" {
		minFee, err := parseNonNegativeInt("payments open fee minimum", s.OpenFeeMin)
		if err != nil {
			return err
		}
		maxFee, err := parseNonNegativeInt("payments open fee maximum", s.OpenFeeMax)
		if err != nil {
			return err
		}
		if maxFee.LT(minFee) {
			return errors.New("payments open fee maximum cannot be below minimum")
		}
	}
	if s.BaseMultiplierBps == 0 || s.BaseMultiplierBps > s.MaxMultiplierBps {
		return errors.New("payments fee multiplier bounds are invalid")
	}
	return nil
}

func (m PaymentFeeMultiplier) Normalize() PaymentFeeMultiplier {
	return m
}

func (m PaymentFeeMultiplier) Validate() error {
	if !IsPaymentFeeClass(m.FeeClass) {
		return fmt.Errorf("unknown payments fee class %q", m.FeeClass)
	}
	if m.MultiplierBps == 0 {
		return errors.New("payments fee multiplier must be positive")
	}
	if m.UpdatedHeight == 0 {
		return errors.New("payments fee multiplier height must be positive")
	}
	return nil
}

func (c PaymentFeeCharge) Normalize() PaymentFeeCharge {
	c.FeeID = normalizeOptionalHash(c.FeeID)
	c.ChannelID = normalizeOptionalHash(c.ChannelID)
	c.ObjectID = strings.TrimSpace(c.ObjectID)
	c.Payer = strings.TrimSpace(c.Payer)
	c.Denom = normalizeAssetDenom(c.Denom)
	c.Amount = strings.TrimSpace(c.Amount)
	c.RequiredAmount = strings.TrimSpace(c.RequiredAmount)
	return c
}

func (c PaymentFeeCharge) Validate() error {
	c = c.Normalize()
	if err := ValidateHash("payments fee id", c.FeeID); err != nil {
		return err
	}
	if !IsPaymentFeeClass(c.FeeClass) {
		return fmt.Errorf("unknown payments fee class %q", c.FeeClass)
	}
	if c.ChannelID != "" {
		if err := ValidateHash("payments fee channel id", c.ChannelID); err != nil {
			return err
		}
	}
	if err := addressing.ValidateUserAddress("payments fee payer", c.Payer); err != nil {
		return err
	}
	if c.Denom != NativeDenom {
		return fmt.Errorf("payments fee denom must be %s", NativeDenom)
	}
	if err := validateNonNegativeInt("payments fee amount", c.Amount); err != nil {
		return err
	}
	if err := validateNonNegativeInt("payments required fee amount", c.RequiredAmount); err != nil {
		return err
	}
	paid, err := parseNonNegativeInt("payments fee amount", c.Amount)
	if err != nil {
		return err
	}
	required, err := parseNonNegativeInt("payments required fee amount", c.RequiredAmount)
	if err != nil {
		return err
	}
	if paid.LT(required) {
		return errors.New("payments fee charge is below required amount")
	}
	if c.MultiplierBps == 0 {
		return errors.New("payments fee charge multiplier must be positive")
	}
	if c.Height == 0 {
		return errors.New("payments fee charge height must be positive")
	}
	return nil
}

func (r PaymentFeeRefund) Normalize() PaymentFeeRefund {
	r.RefundID = normalizeOptionalHash(r.RefundID)
	r.FeeID = normalizeOptionalHash(r.FeeID)
	r.Recipient = strings.TrimSpace(r.Recipient)
	r.Denom = normalizeAssetDenom(r.Denom)
	r.Amount = strings.TrimSpace(r.Amount)
	r.Reason = strings.TrimSpace(r.Reason)
	return r
}

func (r PaymentFeeRefund) Validate() error {
	r = r.Normalize()
	if err := ValidateHash("payments fee refund id", r.RefundID); err != nil {
		return err
	}
	if err := ValidateHash("payments refunded fee id", r.FeeID); err != nil {
		return err
	}
	if err := addressing.ValidateUserAddress("payments fee refund recipient", r.Recipient); err != nil {
		return err
	}
	if r.Denom != NativeDenom {
		return fmt.Errorf("payments fee refund denom must be %s", NativeDenom)
	}
	if err := validatePositiveInt("payments fee refund amount", r.Amount); err != nil {
		return err
	}
	if r.Reason == "" {
		return errors.New("payments fee refund reason is required")
	}
	if r.Height == 0 {
		return errors.New("payments fee refund height must be positive")
	}
	return nil
}

func DefaultSettlementGasCostSchedule() SettlementGasCostSchedule {
	return SettlementGasCostSchedule{
		OpenGas:                 30_000,
		CooperativeCloseGas:     22_000,
		UnilateralCloseGas:      35_000,
		DisputeGas:              45_000,
		FraudProofGas:           60_000,
		ConditionResolutionGas:  40_000,
		PenaltyRoutingGas:       20_000,
		FinalSettlementGas:      50_000,
		ReplayProtectionGas:     10_000,
		PerSignatureGas:         2_000,
		PerConditionGas:         3_000,
		PerFraudProofGas:        8_000,
		PerPenaltyAllocationGas: 1_500,
		PerStateByteGas:         8,
	}
}

func (s SettlementGasCostSchedule) Normalize() SettlementGasCostSchedule {
	defaults := DefaultSettlementGasCostSchedule()
	if s.OpenGas == 0 {
		s.OpenGas = defaults.OpenGas
	}
	if s.CooperativeCloseGas == 0 {
		s.CooperativeCloseGas = defaults.CooperativeCloseGas
	}
	if s.UnilateralCloseGas == 0 {
		s.UnilateralCloseGas = defaults.UnilateralCloseGas
	}
	if s.DisputeGas == 0 {
		s.DisputeGas = defaults.DisputeGas
	}
	if s.FraudProofGas == 0 {
		s.FraudProofGas = defaults.FraudProofGas
	}
	if s.ConditionResolutionGas == 0 {
		s.ConditionResolutionGas = defaults.ConditionResolutionGas
	}
	if s.PenaltyRoutingGas == 0 {
		s.PenaltyRoutingGas = defaults.PenaltyRoutingGas
	}
	if s.FinalSettlementGas == 0 {
		s.FinalSettlementGas = defaults.FinalSettlementGas
	}
	if s.ReplayProtectionGas == 0 {
		s.ReplayProtectionGas = defaults.ReplayProtectionGas
	}
	if s.PerSignatureGas == 0 {
		s.PerSignatureGas = defaults.PerSignatureGas
	}
	if s.PerConditionGas == 0 {
		s.PerConditionGas = defaults.PerConditionGas
	}
	if s.PerFraudProofGas == 0 {
		s.PerFraudProofGas = defaults.PerFraudProofGas
	}
	if s.PerPenaltyAllocationGas == 0 {
		s.PerPenaltyAllocationGas = defaults.PerPenaltyAllocationGas
	}
	if s.PerStateByteGas == 0 {
		s.PerStateByteGas = defaults.PerStateByteGas
	}
	return s
}

func (s SettlementGasCostSchedule) Validate() error {
	schedule := s.Normalize()
	values := []uint64{
		schedule.OpenGas,
		schedule.CooperativeCloseGas,
		schedule.UnilateralCloseGas,
		schedule.DisputeGas,
		schedule.FraudProofGas,
		schedule.ConditionResolutionGas,
		schedule.PenaltyRoutingGas,
		schedule.FinalSettlementGas,
		schedule.ReplayProtectionGas,
		schedule.PerSignatureGas,
		schedule.PerConditionGas,
		schedule.PerFraudProofGas,
		schedule.PerPenaltyAllocationGas,
		schedule.PerStateByteGas,
	}
	for _, value := range values {
		if value == 0 {
			return errors.New("payments settlement gas cost schedule must be positive")
		}
	}
	return nil
}

func (h SecurityReserveAllocationHook) Normalize() SecurityReserveAllocationHook {
	h.HookID = normalizeOptionalHash(h.HookID)
	h.ChannelID = normalizeHash(h.ChannelID)
	h.ProofID = normalizeHash(h.ProofID)
	h.Offender = strings.TrimSpace(h.Offender)
	h.Denom = normalizeAssetDenom(h.Denom)
	h.Amount = strings.TrimSpace(h.Amount)
	h.Allocation = normalizeOptionalHash(h.Allocation)
	if h.Route == "" {
		h.Route = PenaltyRouteSecurityReserve
	}
	return h
}

func (h SecurityReserveAllocationHook) ValidateForChannel(channel ChannelRecord) error {
	hook := h.Normalize()
	channel = channel.Normalize()
	if err := ValidateHash("payments security reserve hook id", hook.HookID); err != nil {
		return err
	}
	if hook.ChannelID != channel.ChannelID {
		return errors.New("payments security reserve hook channel mismatch")
	}
	if err := ValidateHash("payments security reserve proof id", hook.ProofID); err != nil {
		return err
	}
	if !containsString(channel.Participants, hook.Offender) {
		return errors.New("payments security reserve hook offender must be channel participant")
	}
	if hook.Denom != NativeDenom {
		return fmt.Errorf("payments security reserve hook denom must be %s", NativeDenom)
	}
	if hook.Route != PenaltyRouteSecurityReserve {
		return errors.New("payments security reserve hook route mismatch")
	}
	if err := validatePositiveInt("payments security reserve hook amount", hook.Amount); err != nil {
		return err
	}
	if hook.Height == 0 {
		return errors.New("payments security reserve hook height must be positive")
	}
	if err := ValidateHash("payments security reserve allocation commitment", hook.Allocation); err != nil {
		return err
	}
	return nil
}

func (l SettlementInclusionLatency) Normalize() SettlementInclusionLatency {
	l.RecordID = normalizeOptionalHash(l.RecordID)
	l.OperationID = normalizeOptionalHash(l.OperationID)
	l.ChannelID = normalizeHash(l.ChannelID)
	if l.IncludedHeight >= l.SubmittedHeight && l.SubmittedHeight != 0 {
		l.LatencyBlocks = l.IncludedHeight - l.SubmittedHeight
	}
	if l.SLOThreshold > 0 {
		l.Breached = l.LatencyBlocks > l.SLOThreshold
	}
	return l
}

func (l SettlementInclusionLatency) Validate(channels []ChannelRecord) error {
	record := l.Normalize()
	if err := ValidateHash("payments settlement inclusion latency id", record.RecordID); err != nil {
		return err
	}
	if err := ValidateHash("payments settlement inclusion operation id", record.OperationID); err != nil {
		return err
	}
	if err := ValidateHash("payments settlement inclusion channel id", record.ChannelID); err != nil {
		return err
	}
	if !IsSettlementArbitrationOperation(record.Operation) {
		return fmt.Errorf("unknown payments settlement inclusion operation %q", record.Operation)
	}
	if record.SubmittedHeight == 0 || record.IncludedHeight == 0 || record.IncludedHeight < record.SubmittedHeight {
		return errors.New("payments settlement inclusion heights are invalid")
	}
	if record.LatencyBlocks != record.IncludedHeight-record.SubmittedHeight {
		return errors.New("payments settlement inclusion latency mismatch")
	}
	if record.SLOThreshold == 0 {
		return errors.New("payments settlement inclusion SLO threshold must be positive")
	}
	if record.Breached != (record.LatencyBlocks > record.SLOThreshold) {
		return errors.New("payments settlement inclusion breach marker mismatch")
	}
	if _, found := channelMap(channels)[record.ChannelID]; !found {
		return errors.New("payments settlement inclusion channel not found")
	}
	return nil
}

func IsPaymentFeeClass(value PaymentFeeClass) bool {
	switch value {
	case PaymentFeeClassChannelOpen,
		PaymentFeeClassChannelCheckpoint,
		PaymentFeeClassCooperativeClose,
		PaymentFeeClassUnilateralClose,
		PaymentFeeClassDispute,
		PaymentFeeClassFraudProofVerification,
		PaymentFeeClassConditionalPromiseSettlement,
		PaymentFeeClassVirtualChannelAnchor,
		PaymentFeeClassRoutingAdvertisement:
		return true
	default:
		return false
	}
}

func validateOpeningFeePaid(feePaid string) error {
	paid, err := parseNonNegativeInt("payments opening fee paid", feePaid)
	if err != nil {
		return err
	}
	required, err := parsePositiveInt("payments opening fee required", DefaultOpeningFee)
	if err != nil {
		return err
	}
	if paid.LT(required) {
		return errors.New("payments opening fee is not paid")
	}
	return nil
}
