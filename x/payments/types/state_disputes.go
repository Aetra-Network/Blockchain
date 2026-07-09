package types

import (
	"errors"
	"strings"
)

func DisputeClose(state PaymentsState, channelID string, newerState ChannelState, submitter string, currentHeight uint64) (PaymentsState, error) {
	channel, found := state.Export().ChannelByID(channelID)
	if !found {
		return PaymentsState{}, errors.New("payments channel not found")
	}
	return DisputeChannel(state, ChannelDisputeRequest{
		ChannelID:             channelID,
		ClosingStateReference: channel.PendingClose.State.StateHash,
		NewerState:            newerState,
		Submitter:             submitter,
		CurrentHeight:         currentHeight,
	})
}

func DisputeChannel(state PaymentsState, req ChannelDisputeRequest) (PaymentsState, error) {
	state = state.Export()
	req = req.Normalize()
	if req.CurrentHeight == 0 {
		return PaymentsState{}, errors.New("payments dispute height must be positive")
	}
	index, channel, found := state.ChannelIndex(req.ChannelID)
	if !found {
		return PaymentsState{}, errors.New("payments channel not found")
	}
	if channel.Status != ChannelStatusPendingClose {
		return PaymentsState{}, errors.New("payments channel is not pending close")
	}
	if req.ClosingStateReference != channel.PendingClose.State.StateHash {
		return PaymentsState{}, errors.New("payments dispute closing state reference mismatch")
	}
	if req.CurrentHeight > channel.PendingClose.SettleAfterHeight {
		return PaymentsState{}, errors.New("payments dispute window has closed")
	}
	if err := (SettlementArbitrationInput{
		Operation:       SettlementArbitrationDispute,
		ChannelID:       channel.ChannelID,
		SignedState:     req.NewerState,
		ConditionProofs: req.ConditionProofs,
		CurrentHeight:   req.CurrentHeight,
	}).ValidateForChannel(channel); err != nil {
		return PaymentsState{}, err
	}
	if err := req.NewerState.ValidateForChannel(channel, false); err != nil {
		return PaymentsState{}, err
	}
	if !stateStrongerThan(req.NewerState, channel.PendingClose.State) {
		return PaymentsState{}, errors.New("payments dispute state must be newer or stronger")
	}
	if !containsString(channel.Participants, req.Submitter) {
		return PaymentsState{}, errors.New("payments dispute submitter must be participant")
	}
	if err := rejectReusedConditionClaims(state, channel, req.ConditionProofs); err != nil {
		return PaymentsState{}, err
	}
	if err := validateConditionResolutionsForState(req.NewerState, channel, req.ConditionProofs, false); err != nil {
		return PaymentsState{}, err
	}
	nextChannel := channel
	nextChannel.PendingClose.State = req.NewerState
	nextChannel.PendingClose.SubmittedHeight = req.CurrentHeight
	if nextChannel.PendingClose.DisputeCount < MaxDisputeExtensions {
		nextChannel.PendingClose.SettleAfterHeight = req.CurrentHeight + channel.DisputePeriod
		nextChannel.PendingClose.DisputeCount++
	}
	nextChannel.PendingClose.ConditionProofs = mergeConditionResolutions(nextChannel.PendingClose.ConditionProofs, req.ConditionProofs)
	if nextChannel.DisputedNonce < req.NewerState.Nonce {
		nextChannel.DisputedNonce = req.NewerState.Nonce
	}
	if req.FraudProof.ProofID != "" {
		if err := req.FraudProof.ValidateForChannel(channel); err != nil {
			return PaymentsState{}, err
		}
		penalties, allocations, err := BuildFraudPenaltyRouting(channel, req.FraudProof, FraudPenaltyPolicy{})
		if err != nil {
			return PaymentsState{}, err
		}
		nextChannel.PendingClose.FraudProofs = append(nextChannel.PendingClose.FraudProofs, req.FraudProof)
		nextChannel.PendingClose.Penalties = append(nextChannel.PendingClose.Penalties, penalties...)
		nextChannel.PendingClose.PenaltyAllocations = append(nextChannel.PendingClose.PenaltyAllocations, allocations...)
	}
	nextChannel.LatestState = req.NewerState
	chargedState, _, err := ChargePaymentFee(state, PaymentFeeClassDispute, channel, req.Submitter, req.NewerState.StateHash, req.DisputeFeePaid, req.CurrentHeight)
	if err != nil {
		return PaymentsState{}, err
	}
	next := chargedState.Clone()
	nextChannel, err = setChannelFinality(nextChannel, finalityForPendingClose(nextChannel), req.CurrentHeight, &next.Events)
	if err != nil {
		return PaymentsState{}, err
	}
	next.Channels[index] = nextChannel.Normalize()
	next.Events = append(next.Events, ChannelDisputeEvent(nextChannel, req.Submitter, req.CurrentHeight))
	sortChannels(next.Channels)
	return next, next.Validate()
}

func SubmitWatchDispute(state PaymentsState, submission WatchDisputeSubmission) (PaymentsState, error) {
	state = state.Export()
	submission = submission.Normalize()
	channel, found := state.ChannelByID(submission.ChannelID)
	if !found {
		return PaymentsState{}, errors.New("payments channel not found")
	}
	if err := submission.ValidateForChannel(channel); err != nil {
		return PaymentsState{}, err
	}
	return DisputeChannel(state, ChannelDisputeRequest{
		ChannelID:             submission.ChannelID,
		ClosingStateReference: submission.ClosingStateReference,
		NewerState:            submission.NewerState,
		Submitter:             submission.Delegator,
		CurrentHeight:         submission.CurrentHeight,
	})
}

func RegisterValidatorPaymentService(state PaymentsState, metadata ValidatorPaymentServiceMetadata) (PaymentsState, error) {
	state = state.Export()
	metadata = metadata.Normalize()
	metadata.MetadataHash = ComputeValidatorPaymentServiceMetadataHash(metadata)
	if err := metadata.Validate(); err != nil {
		return PaymentsState{}, err
	}
	next := state.Clone()
	replaced := false
	for i, existing := range next.ValidatorPaymentServices {
		if existing.Normalize().ValidatorAddress == metadata.ValidatorAddress {
			next.ValidatorPaymentServices[i] = metadata
			replaced = true
			break
		}
	}
	if !replaced {
		next.ValidatorPaymentServices = append(next.ValidatorPaymentServices, metadata)
	}
	sortValidatorPaymentServices(next.ValidatorPaymentServices)
	return next, next.Validate()
}

func RegisterValidatorWatchService(state PaymentsState, registration ValidatorWatchRegistration) (PaymentsState, error) {
	state = state.Export()
	registration = registration.Normalize()
	metadata, found := state.ValidatorPaymentServiceByValidator(registration.ValidatorAddress)
	if !found {
		return PaymentsState{}, errors.New("payments validator service not found")
	}
	registration.ServiceAddress = metadata.ServiceAddress
	registration.MetadataHash = metadata.MetadataHash
	if registration.MinDelegation == "" || registration.MinDelegation == "0" {
		registration.MinDelegation = metadata.MinDelegation
	}
	if err := registration.Validate(metadata); err != nil {
		return PaymentsState{}, err
	}
	next := state.Clone()
	replaced := false
	for i, existing := range next.ValidatorWatchRegistries {
		existing = existing.Normalize()
		if existing.ValidatorAddress == registration.ValidatorAddress && existing.Delegator == registration.Delegator {
			next.ValidatorWatchRegistries[i] = registration
			replaced = true
			break
		}
	}
	if !replaced {
		next.ValidatorWatchRegistries = append(next.ValidatorWatchRegistries, registration)
	}
	sortValidatorWatchRegistrations(next.ValidatorWatchRegistries)
	return next, next.Validate()
}

func SubmitValidatorAssistedDispute(state PaymentsState, submission ValidatorAssistedDisputeSubmission) (PaymentsState, error) {
	state = state.Export()
	submission = submission.Normalize()
	metadata, found := state.ValidatorPaymentServiceByValidator(submission.ValidatorAddress)
	if !found {
		return PaymentsState{}, errors.New("payments validator service not found")
	}
	channel, found := state.ChannelByID(submission.ChannelID)
	if !found {
		return PaymentsState{}, errors.New("payments channel not found")
	}
	if err := submission.ValidateForChannel(channel, metadata); err != nil {
		return PaymentsState{}, err
	}
	if _, found := state.ValidatorWatchRegistration(submission.ValidatorAddress, submission.Delegator); !found {
		return PaymentsState{}, errors.New("payments validator watch registration not found")
	}
	next, err := SubmitWatchDispute(state, WatchDisputeSubmission{
		WatchService:          metadata.ServiceAddress,
		Delegator:             submission.Delegator,
		ChannelID:             submission.ChannelID,
		ClosingStateReference: submission.ClosingStateReference,
		NewerState:            submission.NewerState,
		CurrentHeight:         submission.CurrentHeight,
		EvidenceHash:          submission.EvidenceHash,
	})
	if err != nil {
		return PaymentsState{}, err
	}
	next.Events = append(next.Events, ValidatorAssistedDisputeEvent(metadata, channel, submission.Delegator, submission.CurrentHeight))
	return next, next.Validate()
}

func SubmitFraudProof(state PaymentsState, channelID string, proof FraudProof, currentHeight uint64) (PaymentsState, error) {
	return SubmitFraudProofWithPolicy(state, channelID, proof, currentHeight, FraudPenaltyPolicy{})
}

func SubmitFraudProofWithPolicy(state PaymentsState, channelID string, proof FraudProof, currentHeight uint64, policy FraudPenaltyPolicy) (PaymentsState, error) {
	state = state.Export()
	if currentHeight == 0 {
		return PaymentsState{}, errors.New("payments fraud proof height must be positive")
	}
	index, channel, found := state.ChannelIndex(channelID)
	if !found {
		return PaymentsState{}, errors.New("payments channel not found")
	}
	if channel.Status != ChannelStatusPendingClose {
		return PaymentsState{}, errors.New("payments fraud proof requires pending close")
	}
	if currentHeight > channel.PendingClose.SettleAfterHeight {
		return PaymentsState{}, errors.New("payments fraud proof window has closed")
	}
	proof = proof.Normalize()
	if err := (SettlementArbitrationInput{
		Operation:     SettlementArbitrationFraudProof,
		ChannelID:     channel.ChannelID,
		FraudProof:    proof,
		CurrentHeight: currentHeight,
	}).ValidateForChannel(channel); err != nil {
		return PaymentsState{}, err
	}
	if err := proof.ValidateForChannel(channel); err != nil {
		return PaymentsState{}, err
	}
	for _, existing := range channel.PendingClose.FraudProofs {
		if existing.ProofID == proof.ProofID {
			return PaymentsState{}, errors.New("payments duplicate fraud proof")
		}
	}
	penalties, allocations, err := BuildFraudPenaltyRouting(channel, proof, policy)
	if err != nil {
		return PaymentsState{}, err
	}
	chargedState, charge, err := ChargePaymentFee(state, PaymentFeeClassFraudProofVerification, channel, proof.SubmittedBy, proof.ProofID, proof.VerificationFeePaid, currentHeight)
	if err != nil {
		return PaymentsState{}, err
	}
	refundedState := chargedState
	if charge.Amount != "0" {
		refundedState, _, err = RefundPaymentFee(chargedState, charge.FeeID, proof.SubmittedBy, "accepted-fraud-proof", currentHeight)
		if err != nil {
			return PaymentsState{}, err
		}
	}
	hookedState, _, err := RecordSecurityReserveAllocationHooks(refundedState, channel.ChannelID, proof, allocations, currentHeight, policy.Normalize().SecurityReserveHook)
	if err != nil {
		return PaymentsState{}, err
	}
	nextChannel := channel
	nextChannel.PendingClose.FraudProofs = append(nextChannel.PendingClose.FraudProofs, proof)
	nextChannel.PendingClose.Penalties = append(nextChannel.PendingClose.Penalties, penalties...)
	nextChannel.PendingClose.PenaltyAllocations = append(nextChannel.PendingClose.PenaltyAllocations, allocations...)
	next := hookedState.Clone()
	nextChannel, err = setChannelFinality(nextChannel, ChannelFinalityPenalized, currentHeight, &next.Events)
	if err != nil {
		return PaymentsState{}, err
	}
	next.Channels[index] = nextChannel.Normalize()
	sortChannels(next.Channels)
	return next, next.Validate()
}

func FraudClose(state PaymentsState, channelID string, currentHeight uint64) (PaymentsState, SettlementRecord, error) {
	state = state.Export()
	if currentHeight == 0 {
		return PaymentsState{}, SettlementRecord{}, errors.New("payments fraud close height must be positive")
	}
	index, channel, found := state.ChannelIndex(channelID)
	if !found {
		return PaymentsState{}, SettlementRecord{}, errors.New("payments channel not found")
	}
	if channel.Status != ChannelStatusPendingClose {
		return PaymentsState{}, SettlementRecord{}, errors.New("payments fraud close requires pending close")
	}
	if len(channel.PendingClose.FraudProofs) == 0 || len(channel.PendingClose.Penalties) == 0 {
		return PaymentsState{}, SettlementRecord{}, errors.New("payments fraud close requires accepted proof")
	}
	if err := (SettlementArbitrationInput{
		Operation:       SettlementArbitrationFinalSettlement,
		ChannelID:       channel.ChannelID,
		SignedState:     channel.PendingClose.State,
		ConditionProofs: channel.PendingClose.ConditionProofs,
		CurrentHeight:   currentHeight,
	}).ValidateForChannel(channel); err != nil {
		return PaymentsState{}, SettlementRecord{}, err
	}
	if err := rejectReusedConditionClaims(state, channel, channel.PendingClose.ConditionProofs); err != nil {
		return PaymentsState{}, SettlementRecord{}, err
	}
	finalBalances, err := applySettlementAdjustments(channel.PendingClose.State.Balances, channel.PendingClose.Penalties, channel.PendingClose.PenaltyAllocations, channel.PendingClose.SettlementFee, channel.PendingClose.Submitter)
	if err != nil {
		return PaymentsState{}, SettlementRecord{}, err
	}
	settlement := SettlementRecord{
		ChainID:            channel.ChainID,
		ChannelID:          channel.ChannelID,
		StateHash:          channel.PendingClose.State.StateHash,
		Nonce:              channel.PendingClose.State.Nonce,
		FinalBalances:      finalBalances,
		SettlementFeeDenom: channel.PendingClose.SettlementFeeDenom,
		SettlementFee:      channel.PendingClose.SettlementFee,
		Penalties:          channel.PendingClose.Penalties,
		PenaltyAllocations: channel.PendingClose.PenaltyAllocations,
		SettledHeight:      currentHeight,
	}
	settlement.SettlementHash = ComputeSettlementHash(settlement)
	if err := settlement.ValidateForChannel(channel); err != nil {
		return PaymentsState{}, SettlementRecord{}, err
	}
	nextChannel := channel
	next := state.Clone()
	nextChannel, err = setChannelFinality(nextChannel, ChannelFinalityFinalizable, currentHeight, &next.Events)
	if err != nil {
		return PaymentsState{}, SettlementRecord{}, err
	}
	nextChannel.Status = ChannelStatusSettled
	nextChannel.FinalizedNonce = settlement.Nonce
	settledFinality := finalityForSettledChannel(nextChannel)
	nextChannel.Finality = settledFinality
	next.Events = append(next.Events, ChannelFinalityTransitionEvent(nextChannel, ChannelFinalityFinalizable, settledFinality, currentHeight))
	nextChannel.PendingClose = PendingClose{}
	next.Channels[index] = nextChannel.Normalize()
	next.Edges = filterEdgesForSettledChannel(next.Edges, channel.ChannelID)
	next.CustodyLocks = filterCustodyLocksForSettledChannel(next.CustodyLocks, channel.ChannelID)
	next.Settlements = append(next.Settlements, settlement)
	appendSettlementReplayRecords(&next, nextChannel, settlement, channel.PendingClose.ConditionProofs, currentHeight)
	sortChannels(next.Channels)
	sortSettlements(next.Settlements)
	sortClosedChannelTombstones(next.ClosedChannels)
	sortConditionClaimRecords(next.ConditionClaims)
	return next, settlement, next.Validate()
}

func validatorWatchRegistrationKey(validatorAddress, delegator string) string {
	return strings.TrimSpace(validatorAddress) + "/" + strings.TrimSpace(delegator)
}
