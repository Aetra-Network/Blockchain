package types

import (
	"errors"
	"fmt"
	"strings"

	sdkmath "cosmossdk.io/math"

	"github.com/sovereign-l1/l1/app/addressing"
)

func ConfigurePaymentFeeSchedule(state PaymentsState, schedule PaymentFeeSchedule) (PaymentsState, error) {
	state = state.Export()
	schedule = schedule.Normalize()
	if err := schedule.Validate(); err != nil {
		return PaymentsState{}, err
	}
	next := state.Clone()
	next.FeeSchedule = schedule
	return next, next.Validate()
}

func SetPaymentFeeMultiplier(state PaymentsState, multiplier PaymentFeeMultiplier) (PaymentsState, error) {
	state = state.Export()
	multiplier = multiplier.Normalize()
	if multiplier.UpdatedHeight == 0 {
		return PaymentsState{}, errors.New("payments fee multiplier height must be positive")
	}
	if err := multiplier.Validate(); err != nil {
		return PaymentsState{}, err
	}
	if multiplier.MultiplierBps > state.FeeSchedule.Normalize().MaxMultiplierBps {
		return PaymentsState{}, errors.New("payments fee multiplier exceeds schedule maximum")
	}
	next := state.Clone()
	replaced := false
	for i, existing := range next.FeeMultipliers {
		if existing.Normalize().FeeClass == multiplier.FeeClass {
			next.FeeMultipliers[i] = multiplier
			replaced = true
			break
		}
	}
	if !replaced {
		next.FeeMultipliers = append(next.FeeMultipliers, multiplier)
	}
	sortPaymentFeeMultipliers(next.FeeMultipliers)
	return next, next.Validate()
}

func RequiredPaymentFee(state PaymentsState, feeClass PaymentFeeClass, channel ChannelRecord) (string, uint64, uint32, error) {
	state = state.Export()
	schedule := state.FeeSchedule.Normalize()
	if err := schedule.Validate(); err != nil {
		return "", 0, 0, err
	}
	if feeClass == PaymentFeeClassChannelOpen {
		formula, err := ComputeChannelOpenFeeFormula(state, channel)
		if err != nil {
			return "", 0, 0, err
		}
		return formula.TotalFee, formula.StorageBytes, formula.MultiplierBps, nil
	}
	baseText, err := paymentFeeBaseAmount(schedule, feeClass)
	if err != nil {
		return "", 0, 0, err
	}
	base, err := parseNonNegativeInt("payments base fee", baseText)
	if err != nil {
		return "", 0, 0, err
	}
	storageBytes := paymentStorageFootprint(feeClass, channel)
	if schedule.StorageFeeEnabled && storageBytes > 0 {
		byteFee, err := parseNonNegativeInt("payments storage byte fee", schedule.StorageByteFee)
		if err != nil {
			return "", 0, 0, err
		}
		base = base.Add(byteFee.Mul(sdkmath.NewIntFromUint64(storageBytes)))
	}
	multiplier := feeMultiplierForClass(state, feeClass, schedule)
	required := base.Mul(sdkmath.NewInt(int64(multiplier)))
	denom := sdkmath.NewInt(10_000)
	if !required.IsZero() {
		required = required.Add(denom.Sub(sdkmath.OneInt())).Quo(denom)
	}
	return required.String(), storageBytes, multiplier, nil
}

func ComputeChannelOpenFeeFormula(state PaymentsState, channel ChannelRecord) (ChannelOpenFeeFormula, error) {
	state = state.Export()
	schedule := state.FeeSchedule.Normalize()
	if err := schedule.Validate(); err != nil {
		return ChannelOpenFeeFormula{}, err
	}
	channel = channel.Normalize()
	base, err := parseNonNegativeInt("payments channel open base fee", schedule.ChannelOpenFee)
	if err != nil {
		return ChannelOpenFeeFormula{}, err
	}
	perParticipant, err := parseNonNegativeInt("payments channel open per participant fee", schedule.ChannelOpenPerParticipantFee)
	if err != nil {
		return ChannelOpenFeeFormula{}, err
	}
	participantCount := uint64(len(channel.Participants))
	participantFee := perParticipant.Mul(sdkmath.NewIntFromUint64(participantCount))
	storageBytes := EstimateChannelOpenStorageFootprint(channel)
	byteFee, err := parseNonNegativeInt("payments channel open storage byte fee", schedule.StorageByteFee)
	if err != nil {
		return ChannelOpenFeeFormula{}, err
	}
	storageFee := sdkmath.ZeroInt()
	if schedule.StorageFeeEnabled {
		storageFee = byteFee.Mul(sdkmath.NewIntFromUint64(storageBytes))
	}
	conditionalSurcharge, err := parseNonNegativeInt("payments conditional capability surcharge", schedule.ConditionalCapabilitySurcharge)
	if err != nil {
		return ChannelOpenFeeFormula{}, err
	}
	if !channel.ConditionalPayments {
		conditionalSurcharge = sdkmath.ZeroInt()
	}
	if _, err := parseNonNegativeInt("payments virtual channel anchor surcharge", schedule.VirtualChannelAnchorSurcharge); err != nil {
		return ChannelOpenFeeFormula{}, err
	}
	virtualSurcharge := sdkmath.ZeroInt()
	routingDeposit, err := parseNonNegativeInt("payments routing advertisement deposit", schedule.RoutingAdvertisementDeposit)
	if err != nil {
		return ChannelOpenFeeFormula{}, err
	}
	if !channel.RoutingAdvertised {
		routingDeposit = sdkmath.ZeroInt()
	}
	rentReserve := sdkmath.ZeroInt()
	if schedule.RenewalPeriod > 0 {
		rentPerBlock, err := parseNonNegativeInt("payments storage rent per block", schedule.StorageRentPerBlock)
		if err != nil {
			return ChannelOpenFeeFormula{}, err
		}
		rentReserve = rentPerBlock.Mul(sdkmath.NewIntFromUint64(schedule.RenewalPeriod))
	}
	subtotal := base.Add(participantFee).Add(storageFee).Add(conditionalSurcharge).Add(virtualSurcharge).Add(routingDeposit).Add(rentReserve)
	minFee, err := parseNonNegativeInt("payments open fee minimum", schedule.OpenFeeMin)
	if err != nil {
		return ChannelOpenFeeFormula{}, err
	}
	if subtotal.LT(minFee) {
		subtotal = minFee
	}
	maxFee, err := parseNonNegativeInt("payments open fee maximum", schedule.OpenFeeMax)
	if err != nil {
		return ChannelOpenFeeFormula{}, err
	}
	if !maxFee.IsZero() && subtotal.GT(maxFee) {
		subtotal = maxFee
	}
	multiplier := feeMultiplierForClass(state, PaymentFeeClassChannelOpen, schedule)
	total := subtotal.Mul(sdkmath.NewInt(int64(multiplier)))
	denom := sdkmath.NewInt(10_000)
	if !total.IsZero() {
		total = total.Add(denom.Sub(sdkmath.OneInt())).Quo(denom)
	}
	return ChannelOpenFeeFormula{
		Denom:                  NativeDenom,
		BaseFee:                base.String(),
		ParticipantFee:         participantFee.String(),
		ParticipantCount:       participantCount,
		StorageByteFee:         byteFee.String(),
		StorageBytes:           storageBytes,
		StorageFee:             storageFee.String(),
		ConditionalSurcharge:   conditionalSurcharge.String(),
		VirtualAnchorSurcharge: virtualSurcharge.String(),
		RoutingDeposit:         routingDeposit.String(),
		RentReserve:            rentReserve.String(),
		MultiplierBps:          multiplier,
		MinFee:                 schedule.OpenFeeMin,
		MaxFee:                 schedule.OpenFeeMax,
		TotalFee:               total.String(),
	}, nil
}

func ChargePaymentFee(state PaymentsState, feeClass PaymentFeeClass, channel ChannelRecord, payer, objectID, amountPaid string, height uint64) (PaymentsState, PaymentFeeCharge, error) {
	state = state.Export()
	if height == 0 {
		return PaymentsState{}, PaymentFeeCharge{}, errors.New("payments fee charge height must be positive")
	}
	if err := addressing.ValidateUserAddress("payments fee payer", payer); err != nil {
		return PaymentsState{}, PaymentFeeCharge{}, err
	}
	amountPaid = strings.TrimSpace(amountPaid)
	if amountPaid == "" {
		amountPaid = "0"
	}
	if err := validateNonNegativeInt("payments fee paid", amountPaid); err != nil {
		return PaymentsState{}, PaymentFeeCharge{}, err
	}
	required, storageBytes, multiplier, err := RequiredPaymentFee(state, feeClass, channel)
	if err != nil {
		return PaymentsState{}, PaymentFeeCharge{}, err
	}
	paid, err := parseNonNegativeInt("payments fee paid", amountPaid)
	if err != nil {
		return PaymentsState{}, PaymentFeeCharge{}, err
	}
	requiredAmount, err := parseNonNegativeInt("payments required fee", required)
	if err != nil {
		return PaymentsState{}, PaymentFeeCharge{}, err
	}
	if paid.LT(requiredAmount) {
		return PaymentsState{}, PaymentFeeCharge{}, fmt.Errorf("payments %s fee below required amount %s", feeClass, required)
	}
	channelID := normalizeOptionalHash(channel.ChannelID)
	objectID = strings.TrimSpace(objectID)
	charge := PaymentFeeCharge{
		FeeID:          HashParts("payment-fee", string(feeClass), channelID, objectID, payer, amountPaid, fmt.Sprintf("%020d", height)),
		FeeClass:       feeClass,
		ChannelID:      channelID,
		ObjectID:       objectID,
		Payer:          strings.TrimSpace(payer),
		Denom:          NativeDenom,
		Amount:         amountPaid,
		RequiredAmount: required,
		StorageBytes:   storageBytes,
		MultiplierBps:  multiplier,
		Height:         height,
	}.Normalize()
	if err := charge.Validate(); err != nil {
		return PaymentsState{}, PaymentFeeCharge{}, err
	}
	next := state.Clone()
	next.FeeCharges = append(next.FeeCharges, charge)
	sortPaymentFeeCharges(next.FeeCharges)
	return next, charge, next.Validate()
}

func RefundPaymentFee(state PaymentsState, feeID, recipient, reason string, height uint64) (PaymentsState, PaymentFeeRefund, error) {
	state = state.Export()
	feeID = normalizeHash(feeID)
	index := -1
	var charge PaymentFeeCharge
	for i, existing := range state.FeeCharges {
		existing = existing.Normalize()
		if existing.FeeID == feeID {
			index = i
			charge = existing
			break
		}
	}
	if index < 0 {
		return PaymentsState{}, PaymentFeeRefund{}, errors.New("payments fee charge not found")
	}
	if charge.Refunded {
		return PaymentsState{}, PaymentFeeRefund{}, errors.New("payments fee already refunded")
	}
	if charge.Amount == "0" {
		return state, PaymentFeeRefund{}, nil
	}
	refund := PaymentFeeRefund{
		RefundID:  HashParts("payment-fee-refund", feeID, recipient, reason, fmt.Sprintf("%020d", height)),
		FeeID:     feeID,
		Recipient: recipient,
		Denom:     NativeDenom,
		Amount:    charge.Amount,
		Reason:    reason,
		Height:    height,
	}.Normalize()
	if err := refund.Validate(); err != nil {
		return PaymentsState{}, PaymentFeeRefund{}, err
	}
	next := state.Clone()
	next.FeeCharges[index].Refunded = true
	next.FeeRefunds = append(next.FeeRefunds, refund)
	sortPaymentFeeCharges(next.FeeCharges)
	sortPaymentFeeRefunds(next.FeeRefunds)
	return next, refund, next.Validate()
}

func EstimateSettlementMessageGas(input SettlementArbitrationInput, schedule SettlementGasCostSchedule) (SettlementGasEstimate, error) {
	input = input.Normalize()
	schedule = schedule.Normalize()
	if err := schedule.Validate(); err != nil {
		return SettlementGasEstimate{}, err
	}
	if !IsSettlementArbitrationOperation(input.Operation) {
		return SettlementGasEstimate{}, fmt.Errorf("unknown payments settlement gas operation %q", input.Operation)
	}
	base, err := settlementBaseGasForOperation(input.Operation, schedule)
	if err != nil {
		return SettlementGasEstimate{}, err
	}
	signatureCount := uint64(len(input.SignedState.Normalize().Signatures))
	if !input.Claim.IsZero() {
		signatureCount++
		if input.Claim.ReceiverAckOptional.SignatureHash != "" {
			signatureCount++
		}
	}
	conditionCount := uint64(len(input.ConditionProofs))
	fraudProofCount := uint64(0)
	if input.FraudProof.Normalize().ProofID != "" {
		fraudProofCount = 1
	}
	penaltyAllocationCount := uint64(0)
	if input.Operation == SettlementArbitrationPenaltyRouting && input.FraudProof.Normalize().ProofID != "" {
		penaltyAllocationCount = 1
	}
	stateBytes := estimateSettlementStateBytes(input)
	estimate := SettlementGasEstimate{
		Operation:            input.Operation,
		BaseGas:              base,
		SignatureGas:         signatureCount * schedule.PerSignatureGas,
		ConditionGas:         conditionCount * schedule.PerConditionGas,
		FraudProofGas:        fraudProofCount * schedule.PerFraudProofGas,
		PenaltyAllocationGas: penaltyAllocationCount * schedule.PerPenaltyAllocationGas,
		StateByteGas:         stateBytes * schedule.PerStateByteGas,
	}
	estimate.TotalGas = estimate.BaseGas + estimate.SignatureGas + estimate.ConditionGas + estimate.FraudProofGas + estimate.PenaltyAllocationGas + estimate.StateByteGas
	return estimate, nil
}

func RecordSecurityReserveAllocationHooks(state PaymentsState, channelID string, proof FraudProof, allocations []PenaltyAllocation, height uint64, enabled bool) (PaymentsState, []SecurityReserveAllocationHook, error) {
	state = state.Export()
	if !enabled {
		return state, nil, nil
	}
	channel, found := state.ChannelByID(channelID)
	if !found {
		return PaymentsState{}, nil, errors.New("payments security reserve hook channel not found")
	}
	hooks, err := BuildSecurityReserveAllocationHooks(channel, proof, allocations, height)
	if err != nil {
		return PaymentsState{}, nil, err
	}
	if len(hooks) == 0 {
		return state, nil, nil
	}
	next := state.Clone()
	next.SecurityReserveHooks = append(next.SecurityReserveHooks, hooks...)
	sortSecurityReserveAllocationHooks(next.SecurityReserveHooks)
	return next, hooks, next.Validate()
}

func BuildSecurityReserveAllocationHooks(channel ChannelRecord, proof FraudProof, allocations []PenaltyAllocation, height uint64) ([]SecurityReserveAllocationHook, error) {
	channel = channel.Normalize()
	proof = proof.Normalize()
	if height == 0 {
		return nil, errors.New("payments security reserve hook height must be positive")
	}
	out := []SecurityReserveAllocationHook{}
	for _, allocation := range normalizePenaltyAllocations(allocations) {
		if allocation.Route != PenaltyRouteSecurityReserve {
			continue
		}
		commitment := HashParts("security-reserve-allocation", channel.ChannelID, proof.ProofID, allocation.Offender, allocation.Amount, fmt.Sprintf("%020d", height))
		hook := SecurityReserveAllocationHook{
			HookID:     HashParts("security-reserve-hook", commitment),
			ChannelID:  channel.ChannelID,
			ProofID:    proof.ProofID,
			Offender:   allocation.Offender,
			Denom:      NativeDenom,
			Amount:     allocation.Amount,
			Height:     height,
			Route:      PenaltyRouteSecurityReserve,
			Allocation: commitment,
		}.Normalize()
		if err := hook.ValidateForChannel(channel); err != nil {
			return nil, err
		}
		out = append(out, hook)
	}
	sortSecurityReserveAllocationHooks(out)
	return out, nil
}

func RecordSettlementInclusionLatency(state PaymentsState, operationID, channelID string, operation SettlementArbitrationOperation, submittedHeight, includedHeight, sloThreshold uint64) (PaymentsState, SettlementInclusionLatency, error) {
	state = state.Export()
	channelID = normalizeHash(channelID)
	if _, found := state.ChannelByID(channelID); !found {
		return PaymentsState{}, SettlementInclusionLatency{}, errors.New("payments inclusion latency channel not found")
	}
	record := SettlementInclusionLatency{
		RecordID:        HashParts("settlement-inclusion-latency", operationID, channelID, string(operation), fmt.Sprintf("%020d", submittedHeight), fmt.Sprintf("%020d", includedHeight)),
		OperationID:     operationID,
		ChannelID:       channelID,
		Operation:       operation,
		SubmittedHeight: submittedHeight,
		IncludedHeight:  includedHeight,
		SLOThreshold:    sloThreshold,
	}.Normalize()
	if err := record.Validate(state.Channels); err != nil {
		return PaymentsState{}, SettlementInclusionLatency{}, err
	}
	next := state.Clone()
	next.InclusionLatencies = append(next.InclusionLatencies, record)
	sortSettlementInclusionLatencies(next.InclusionLatencies)
	return next, record, next.Validate()
}

func settlementBaseGasForOperation(operation SettlementArbitrationOperation, schedule SettlementGasCostSchedule) (uint64, error) {
	switch operation {
	case SettlementArbitrationOpen, SettlementArbitrationCollateralCustody:
		return schedule.OpenGas, nil
	case SettlementArbitrationCooperativeClose:
		return schedule.CooperativeCloseGas, nil
	case SettlementArbitrationUnilateralClose:
		return schedule.UnilateralCloseGas, nil
	case SettlementArbitrationDispute:
		return schedule.DisputeGas, nil
	case SettlementArbitrationFraudProof:
		return schedule.FraudProofGas, nil
	case SettlementArbitrationConditionResolution:
		return schedule.ConditionResolutionGas, nil
	case SettlementArbitrationPenaltyRouting:
		return schedule.PenaltyRoutingGas, nil
	case SettlementArbitrationFinalSettlement:
		return schedule.FinalSettlementGas, nil
	case SettlementArbitrationReplayProtection:
		return schedule.ReplayProtectionGas, nil
	default:
		return 0, fmt.Errorf("unknown payments settlement gas operation %q", operation)
	}
}

func estimateSettlementStateBytes(input SettlementArbitrationInput) uint64 {
	input = input.Normalize()
	state := input.SignedState.Normalize()
	size := uint64(len(input.ChannelID) + len(state.StateHash) + len(state.PreviousStateHash) + len(state.ConditionRoot) + len(state.ParticipantSetHash))
	size += uint64(len(state.Balances) * 80)
	size += uint64(len(state.Signatures) * 128)
	size += uint64(len(input.ConditionProofs) * 96)
	proof := input.FraudProof.Normalize()
	if proof.ProofID != "" {
		size += uint64(len(proof.ProofID) + len(proof.EvidenceHash) + len(proof.PenaltyAmount) + 256)
	}
	if !input.Claim.IsZero() {
		claim := input.Claim.Normalize()
		size += uint64(len(claim.StateHash) + len(claim.ClaimedAmount) + 128)
	}
	if size == 0 {
		return 1
	}
	return size
}

func paymentFeeBaseAmount(schedule PaymentFeeSchedule, feeClass PaymentFeeClass) (string, error) {
	switch feeClass {
	case PaymentFeeClassChannelOpen:
		return schedule.ChannelOpenFee, nil
	case PaymentFeeClassChannelCheckpoint:
		return schedule.ChannelCheckpointFee, nil
	case PaymentFeeClassCooperativeClose:
		return schedule.CooperativeCloseFee, nil
	case PaymentFeeClassUnilateralClose:
		return schedule.UnilateralCloseFee, nil
	case PaymentFeeClassDispute:
		return schedule.DisputeFee, nil
	case PaymentFeeClassFraudProofVerification:
		return schedule.FraudProofVerificationFee, nil
	case PaymentFeeClassConditionalPromiseSettlement:
		return schedule.ConditionalPromiseSettlementFee, nil
	case PaymentFeeClassVirtualChannelAnchor:
		return schedule.VirtualChannelAnchorFee, nil
	case PaymentFeeClassRoutingAdvertisement:
		return schedule.RoutingAdvertisementFee, nil
	default:
		return "", fmt.Errorf("unknown payments fee class %q", feeClass)
	}
}

func paymentStorageFootprint(feeClass PaymentFeeClass, channel ChannelRecord) uint64 {
	channel = channel.Normalize()
	switch feeClass {
	case PaymentFeeClassChannelOpen, PaymentFeeClassChannelCheckpoint, PaymentFeeClassUnilateralClose, PaymentFeeClassDispute:
		return EstimateChannelOpenStorageFootprint(channel)
	case PaymentFeeClassConditionalPromiseSettlement:
		return uint64(len(channel.ChannelID) + len(channel.LatestState.Conditions)*128)
	case PaymentFeeClassVirtualChannelAnchor:
		return uint64(len(channel.ChannelID) + len(channel.Participants)*48 + 128)
	case PaymentFeeClassRoutingAdvertisement:
		return uint64(len(channel.ChannelID) + len(channel.Participants)*48 + 64)
	default:
		return 0
	}
}

func EstimateChannelOpenStorageFootprint(channel ChannelRecord) uint64 {
	channel = channel.Normalize()
	footprint := uint64(128)
	footprint += uint64(len(channel.ChannelID) + len(channel.ChainID) + len(channel.Denom) + len(channel.Collateral))
	footprint += uint64(len(channel.Participants) * 48)
	footprint += uint64(len(channel.RequiredSigners) * 48)
	footprint += uint64(len(channel.OpeningStateHash) + len(channel.LatestState.StateHash) + len(channel.LatestState.ParticipantSetHash))
	footprint += uint64(len(channel.LatestState.Balances) * 64)
	footprint += uint64(len(channel.LatestState.ReserveA) + len(channel.LatestState.ReserveB))
	footprint += uint64(len(channel.LatestState.Conditions) * 160)
	if channel.ConditionalPayments {
		footprint += 64
	}
	if channel.RoutingAdvertised {
		footprint += 96
	}
	return footprint
}

func feeMultiplierForClass(state PaymentsState, feeClass PaymentFeeClass, schedule PaymentFeeSchedule) uint32 {
	multiplier := schedule.Normalize().BaseMultiplierBps
	for _, configured := range state.FeeMultipliers {
		configured = configured.Normalize()
		if configured.FeeClass == feeClass {
			multiplier = configured.MultiplierBps
			break
		}
	}
	return multiplier
}

func feeChannelForVirtual(vc VirtualChannel) ChannelRecord {
	vc = vc.Normalize()
	return ChannelRecord{
		ChainID:      vc.ChainID,
		ChannelID:    vc.VirtualChannelID,
		Participants: vc.Endpoints,
		LatestState:  ChannelState{StateHash: vc.StateHash},
	}
}
