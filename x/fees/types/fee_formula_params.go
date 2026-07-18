package types

import (
	"fmt"

	sdkmath "cosmossdk.io/math"
)

// FeeFormulaParams holds the extended fee formula governance parameters that
// supplement the base Params. All values are in naet unless noted.
//
// Formula (Requirement 1.1):
//
//	transfer_fee_naet = max(min_tx_fee_naet, base_transfer_fee_naet)
//	  + gas_used * current_base_fee_per_gas_naet
//	  + tx_size_bytes * byte_fee_naet
//	  + message_count * message_fee_naet
//	  + bounded_congestion_surcharge_naet
//	  + low_reputation_premium_naet
//	  + storage_rent_side_effects_naet
//	  - bounded_reputation_discount_naet
type FeeFormulaParams struct {
	// TargetTransferFeeNaet is the anchor fee for a normal transfer (Requirement 1.2).
	// Default: 500_000_000 naet == 0.5 AET.
	TargetTransferFeeNaet	string	`json:"target_transfer_fee_naet"`

	// BaseFeePerGasNaet is the cost per gas unit in naet.
	BaseFeePerGasNaet	string	`json:"base_fee_per_gas_naet"`

	// ByteFeeNaet is the cost per transaction byte in naet.
	ByteFeeNaet	string	`json:"byte_fee_naet"`

	// MessageFeeNaet is the cost per message in naet.
	MessageFeeNaet	string	`json:"message_fee_naet"`

	// MaxCongestionSurchargeNaet is the upper bound of the congestion surcharge.
	// The actual surcharge is proportional to block utilization above the threshold.
	MaxCongestionSurchargeNaet	string	`json:"max_congestion_surcharge_naet"`

	// LowReputationPremiumCapNaet is the maximum bounded premium added for low-reputation
	// senders (Requirement 1.4). Never blocks a transaction.
	LowReputationPremiumCapNaet	string	`json:"low_reputation_premium_cap_naet"`

	// HighReputationDiscountCapNaet is the maximum bounded discount applied for
	// high-reputation senders (Requirement 1.5). Never zeroes the protocol fee.
	HighReputationDiscountCapNaet	string	`json:"high_reputation_discount_cap_naet"`

	// StorageRentSideEffectsNaet is the default fee budget for transactions that create
	// or increase persistent state (Requirement 6.6). May be overridden per-tx.
	StorageRentSideEffectsNaet	string	`json:"storage_rent_side_effects_naet"`

	// --- ANS Phase B: multiplicative reputation-fee scaling (gated) ---
	// These scale the base transfer anchor for a reputation-GATED sender (a
	// wallet that currently holds a domain, or a validator) between the floor
	// (best reputation) and the ceiling (worst reputation). A non-gated sender
	// is unaffected (1.0x). All three are basis-point / score integers, so the
	// scaling is integer-only and deterministic.
	ReputationFeeFloorBps		uint32	`json:"reputation_fee_floor_bps"`
	ReputationFeeCeilBps		uint32	`json:"reputation_fee_ceil_bps"`
	ReputationFeeReferenceScore	uint32	`json:"reputation_fee_reference_score"`
}

// DefaultFeeFormulaParams returns safe governance defaults for the extended fee formula.
func DefaultFeeFormulaParams() FeeFormulaParams {
	return FeeFormulaParams{
		TargetTransferFeeNaet:		DefaultTargetTransferFeeAmount,
		BaseFeePerGasNaet:		DefaultBaseGasFeePerGas,
		ByteFeeNaet:			DefaultByteFeeNaet,
		MessageFeeNaet:			DefaultMessageFeeNaet,
		MaxCongestionSurchargeNaet:	"1000000000", // up to 1 AET at full congestion
		LowReputationPremiumCapNaet:	DefaultLowReputationPremiumCap,
		HighReputationDiscountCapNaet:	DefaultHighReputationDiscountCap,
		StorageRentSideEffectsNaet:	DefaultStorageRentSideEffectsNaet,
		ReputationFeeFloorBps:		DefaultReputationFeeFloorBps,
		ReputationFeeCeilBps:		DefaultReputationFeeCeilBps,
		ReputationFeeReferenceScore:	DefaultReputationFeeReferenceScore,
	}
}

// Validate checks that all FeeFormulaParams are within acceptable bounds.
func (p FeeFormulaParams) Validate() error {
	if _, err := p.TargetTransferFeeInt(); err != nil {
		return err
	}
	if _, err := validateNonNegativeAmount("base_fee_per_gas_naet", p.BaseFeePerGasNaet); err != nil {
		return err
	}
	if _, err := validateNonNegativeAmount("byte_fee_naet", p.ByteFeeNaet); err != nil {
		return err
	}
	if _, err := validateNonNegativeAmount("message_fee_naet", p.MessageFeeNaet); err != nil {
		return err
	}
	if _, err := validateNonNegativeAmount("max_congestion_surcharge_naet", p.MaxCongestionSurchargeNaet); err != nil {
		return err
	}
	if _, err := validateNonNegativeAmount("low_reputation_premium_cap_naet", p.LowReputationPremiumCapNaet); err != nil {
		return err
	}
	if _, err := validateNonNegativeAmount("high_reputation_discount_cap_naet", p.HighReputationDiscountCapNaet); err != nil {
		return err
	}
	if _, err := validateNonNegativeAmount("storage_rent_side_effects_naet", p.StorageRentSideEffectsNaet); err != nil {
		return err
	}
	// ANS Phase B multiplicative reputation scaling. Validate on defaulted
	// values (0 means "unset -> default", exactly as NormalizeFeeFormulaParams
	// fills them, and every real path normalizes before use) so a zero-valued
	// legacy param set stays valid while a genuinely out-of-range vote is
	// rejected. floor must be positive (never zero the anchor) and <= ceil <=
	// 10000; the reference score must be positive (it is a divisor).
	def := DefaultFeeFormulaParams()
	floorBps := p.ReputationFeeFloorBps
	if floorBps == 0 {
		floorBps = def.ReputationFeeFloorBps
	}
	ceilBps := p.ReputationFeeCeilBps
	if ceilBps == 0 {
		ceilBps = def.ReputationFeeCeilBps
	}
	refScore := p.ReputationFeeReferenceScore
	if refScore == 0 {
		refScore = def.ReputationFeeReferenceScore
	}
	if floorBps > uint32(BasisPoints) {
		return fmt.Errorf("reputation_fee_floor_bps must be in (0, %d], got %d", BasisPoints, p.ReputationFeeFloorBps)
	}
	if ceilBps < floorBps || ceilBps > uint32(BasisPoints) {
		return fmt.Errorf("reputation_fee_ceil_bps must be in [%d, %d], got %d", floorBps, BasisPoints, p.ReputationFeeCeilBps)
	}
	if refScore == 0 {
		return fmt.Errorf("reputation_fee_reference_score must be positive")
	}
	return nil
}

// NormalizeFeeFormulaParams fills zero/empty fields with defaults.
func NormalizeFeeFormulaParams(p FeeFormulaParams) FeeFormulaParams {
	def := DefaultFeeFormulaParams()
	if p.TargetTransferFeeNaet == "" {
		p.TargetTransferFeeNaet = def.TargetTransferFeeNaet
	}
	if p.BaseFeePerGasNaet == "" {
		p.BaseFeePerGasNaet = def.BaseFeePerGasNaet
	}
	if p.ByteFeeNaet == "" {
		p.ByteFeeNaet = def.ByteFeeNaet
	}
	if p.MessageFeeNaet == "" {
		p.MessageFeeNaet = def.MessageFeeNaet
	}
	if p.MaxCongestionSurchargeNaet == "" {
		p.MaxCongestionSurchargeNaet = def.MaxCongestionSurchargeNaet
	}
	if p.LowReputationPremiumCapNaet == "" {
		p.LowReputationPremiumCapNaet = def.LowReputationPremiumCapNaet
	}
	if p.HighReputationDiscountCapNaet == "" {
		p.HighReputationDiscountCapNaet = def.HighReputationDiscountCapNaet
	}
	if p.StorageRentSideEffectsNaet == "" {
		p.StorageRentSideEffectsNaet = def.StorageRentSideEffectsNaet
	}
	if p.ReputationFeeFloorBps == 0 {
		p.ReputationFeeFloorBps = def.ReputationFeeFloorBps
	}
	if p.ReputationFeeCeilBps == 0 {
		p.ReputationFeeCeilBps = def.ReputationFeeCeilBps
	}
	if p.ReputationFeeReferenceScore == 0 {
		p.ReputationFeeReferenceScore = def.ReputationFeeReferenceScore
	}
	return p
}

// TargetTransferFeeInt parses TargetTransferFeeNaet to sdkmath.Int.
func (p FeeFormulaParams) TargetTransferFeeInt() (sdkmath.Int, error) {
	amount, ok := sdkmath.NewIntFromString(p.TargetTransferFeeNaet)
	if !ok || !amount.IsPositive() {
		return sdkmath.Int{}, fmt.Errorf("target_transfer_fee_naet must be a positive integer, got %q", p.TargetTransferFeeNaet)
	}
	return amount, nil
}

// BaseFeePerGasInt parses BaseFeePerGasNaet.
func (p FeeFormulaParams) BaseFeePerGasInt() (sdkmath.Int, error) {
	return parseNonNegativeInt("base_fee_per_gas_naet", p.BaseFeePerGasNaet)
}

// ByteFeeInt parses ByteFeeNaet.
func (p FeeFormulaParams) ByteFeeInt() (sdkmath.Int, error) {
	return parseNonNegativeInt("byte_fee_naet", p.ByteFeeNaet)
}

// MessageFeeInt parses MessageFeeNaet.
func (p FeeFormulaParams) MessageFeeInt() (sdkmath.Int, error) {
	return parseNonNegativeInt("message_fee_naet", p.MessageFeeNaet)
}

// MaxCongestionSurchargeInt parses MaxCongestionSurchargeNaet.
func (p FeeFormulaParams) MaxCongestionSurchargeInt() (sdkmath.Int, error) {
	return parseNonNegativeInt("max_congestion_surcharge_naet", p.MaxCongestionSurchargeNaet)
}

// LowReputationPremiumCapInt parses LowReputationPremiumCapNaet.
func (p FeeFormulaParams) LowReputationPremiumCapInt() (sdkmath.Int, error) {
	return parseNonNegativeInt("low_reputation_premium_cap_naet", p.LowReputationPremiumCapNaet)
}

// HighReputationDiscountCapInt parses HighReputationDiscountCapNaet.
func (p FeeFormulaParams) HighReputationDiscountCapInt() (sdkmath.Int, error) {
	return parseNonNegativeInt("high_reputation_discount_cap_naet", p.HighReputationDiscountCapNaet)
}

// StorageRentSideEffectsInt parses StorageRentSideEffectsNaet.
func (p FeeFormulaParams) StorageRentSideEffectsInt() (sdkmath.Int, error) {
	return parseNonNegativeInt("storage_rent_side_effects_naet", p.StorageRentSideEffectsNaet)
}

// ComputeFullTransferFee calculates the complete deterministic fee for a transaction
// using all formula components from Requirement 1.1.
//
//	transfer_fee_naet = max(min_tx_fee_naet, base_transfer_fee_naet)
//	  + gas_used * current_base_fee_per_gas_naet
//	  + tx_size_bytes * byte_fee_naet
//	  + message_count * message_fee_naet
//	  + bounded_congestion_surcharge_naet
//	  + low_reputation_premium_naet
//	  + storage_rent_side_effects_naet
//	  - bounded_reputation_discount_naet
//
// The result is always clamped to [min_tx_fee_naet, max_fee_amount]: the
// byte/gas/message components are otherwise unbounded, but a required fee
// above the governance hard cap would make an otherwise-legal tx permanently
// unpayable (FINDING-011).
func ComputeFullTransferFee(
	baseParams Params,
	formulaParams FeeFormulaParams,
	gasUsed uint64,
	txSizeBytes uint64,
	messageCount uint64,
	blockUtilizationBps uint32,
	reputationScore uint32,
	reputationFound bool,
	storageRentSideEffectsNaet sdkmath.Int,
) (sdkmath.Int, error) {
	baseParams = NormalizeParams(baseParams)
	formulaParams = NormalizeFeeFormulaParams(formulaParams)

	if err := baseParams.Validate(); err != nil {
		return sdkmath.Int{}, err
	}
	if err := formulaParams.Validate(); err != nil {
		return sdkmath.Int{}, err
	}

	minFee, err := baseParams.MinFeeInt()
	if err != nil {
		return sdkmath.Int{}, err
	}
	baseFee, err := baseParams.BaseFeeInt()
	if err != nil {
		return sdkmath.Int{}, err
	}
	maxFee, err := baseParams.MaxFeeInt()
	if err != nil {
		return sdkmath.Int{}, err
	}
	base := minFee
	if baseFee.GT(minFee) {
		base = baseFee
	}

	gasFeePerGas, err := formulaParams.BaseFeePerGasInt()
	if err != nil {
		return sdkmath.Int{}, err
	}
	gasComponent := gasFeePerGas.MulRaw(int64(gasUsed))

	byteFee, err := formulaParams.ByteFeeInt()
	if err != nil {
		return sdkmath.Int{}, err
	}
	byteComponent := byteFee.MulRaw(int64(txSizeBytes))

	msgFee, err := formulaParams.MessageFeeInt()
	if err != nil {
		return sdkmath.Int{}, err
	}
	msgComponent := msgFee.MulRaw(int64(messageCount))

	maxSurcharge, err := formulaParams.MaxCongestionSurchargeInt()
	if err != nil {
		return sdkmath.Int{}, err
	}
	congestionSurcharge := computeBoundedCongestionSurcharge(maxSurcharge, blockUtilizationBps, baseParams.CongestionThresholdBps)

	// ANS Phase B: reputation scales the base transfer anchor MULTIPLICATIVELY,
	// and only for a reputation-GATED sender (reputationFound == true, set by the
	// wiring adapter for a wallet that currently holds a domain OR is a
	// validator). A non-gated sender keeps the full anchor (1.0x). The
	// cost-recovery components (gas/byte/message/congestion/storage-rent) are
	// NEVER discounted -- they are anti-spam / real-resource charges, not a
	// reputation reward.
	multBps := computeReputationMultiplierBps(
		reputationScore,
		reputationFound,
		formulaParams.ReputationFeeFloorBps,
		formulaParams.ReputationFeeCeilBps,
		formulaParams.ReputationFeeReferenceScore,
	)
	adjustedBase := base.MulRaw(int64(multBps)).QuoRaw(int64(BasisPoints))

	storageRent := storageRentSideEffectsNaet
	if storageRent.IsNil() || storageRent.IsNegative() {
		storageRent = sdkmath.ZeroInt()
	}

	total := adjustedBase.
		Add(gasComponent).
		Add(byteComponent).
		Add(msgComponent).
		Add(congestionSurcharge).
		Add(storageRent)

	if total.LT(minFee) {
		total = minFee
	}
	// The byte/gas/message components above are unbounded in principle (e.g. a
	// large tx linearly inflates byteComponent), but the protocol never admits
	// a fee above the governance hard cap (Params.MaxFeeAmount) -- AdmitTx
	// separately rejects any paid fee > maxFee. Without this clamp, a
	// large-but-envelope-legal tx could have a full-formula requirement that
	// exceeds maxFee, making it permanently unpayable: paying the maximum
	// legal fee would still be "insufficient" against the uncapped
	// requirement. Clamp here -- the single point where the full-formula
	// requirement is computed -- so every caller (not only AdmitTx) gets a
	// requirement that is always <= maxFee and therefore payable
	// (FINDING-011).
	if total.GT(maxFee) {
		total = maxFee
	}

	return total, nil
}

// computeBoundedCongestionSurcharge computes a surcharge proportional to how far
// block utilization exceeds the congestion threshold. This uses only KV-state bps
// (deterministic), never wall-clock or mempool data (Requirement 1.3).
func computeBoundedCongestionSurcharge(maxSurcharge sdkmath.Int, utilizationBps, thresholdBps uint32) sdkmath.Int {
	if utilizationBps <= thresholdBps || maxSurcharge.IsZero() {
		return sdkmath.ZeroInt()
	}
	remainingBps := uint64(BasisPoints) - uint64(thresholdBps)
	if remainingBps == 0 {
		return maxSurcharge
	}
	overBps := uint64(utilizationBps - thresholdBps)

	surcharge := maxSurcharge.MulRaw(int64(overBps)).QuoRaw(int64(remainingBps))
	if surcharge.GT(maxSurcharge) {
		return maxSurcharge
	}
	return surcharge
}

// computeReputationMultiplierBps returns the basis-point factor applied to the
// base transfer anchor for a sender with the given reputation score (ANS Phase
// B). It replaces the additive premium/discount model.
//
//   - A non-GATED sender (found == false: no domain and not a validator) always
//     gets BasisPoints (1.0x) -- a plain wallet's fee is unaffected by whatever
//     reputation record it may happen to have.
//   - A GATED sender is scaled linearly from ceilBps (worst reputation, at
//     score 0) down to floorBps (best reputation, at score >= referenceScore).
//     Higher score -> larger reduction from the ceiling -> lower multiplier.
//
// The result is always in [floorBps, ceilBps] (both <= BasisPoints, floorBps >
// 0 by Validate), so the anchor is scaled down but never zeroed. Integer-only
// (truncating division), so every node computes the identical factor.
func computeReputationMultiplierBps(score uint32, found bool, floorBps, ceilBps, referenceScore uint32) uint32 {
	if !found {
		return uint32(BasisPoints)
	}
	if referenceScore == 0 || ceilBps < floorBps {
		// Defensive: Validate/Normalize guarantee neither, but never divide by
		// zero or underflow the span in the consensus path.
		return ceilBps
	}
	s := score
	if s > referenceScore {
		s = referenceScore
	}
	span := uint64(ceilBps - floorBps)
	reduction := uint32(span * uint64(s) / uint64(referenceScore))
	return ceilBps - reduction
}

// validateNonNegativeAmount validates that a string integer is >= 0.
func validateNonNegativeAmount(name, value string) (sdkmath.Int, error) {
	return parseNonNegativeInt(name, value)
}

func parseNonNegativeInt(name, value string) (sdkmath.Int, error) {
	if value == "" {
		return sdkmath.ZeroInt(), nil
	}
	amount, ok := sdkmath.NewIntFromString(value)
	if !ok {
		return sdkmath.Int{}, fmt.Errorf("%s must be a non-negative integer, got %q", name, value)
	}
	if amount.IsNegative() {
		return sdkmath.Int{}, fmt.Errorf("%s must be non-negative, got %s", name, amount.String())
	}
	return amount, nil
}
