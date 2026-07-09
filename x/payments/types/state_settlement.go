package types

import (
	"errors"

	sdkmath "cosmossdk.io/math"
)

func SubmitClose(state PaymentsState, channelID string, closingState ChannelState, submitter string, currentHeight uint64, settlementFee string) (PaymentsState, error) {
	return SubmitCloseWithRequest(state, ChannelCloseRequest{
		ChannelID:     channelID,
		ClosingState:  closingState,
		CloseReason:   CloseReasonUnilateral,
		Submitter:     submitter,
		CurrentHeight: currentHeight,
		SettlementFee: settlementFee,
	})
}

func SubmitCloseWithRequest(state PaymentsState, req ChannelCloseRequest) (PaymentsState, error) {
	state = state.Export()
	req = req.Normalize()
	if req.CurrentHeight == 0 {
		return PaymentsState{}, errors.New("payments close height must be positive")
	}
	index, channel, found := state.ChannelIndex(req.ChannelID)
	if !found {
		return PaymentsState{}, errors.New("payments channel not found")
	}
	if channel.Status != ChannelStatusOpen {
		return PaymentsState{}, errors.New("payments channel is not open")
	}
	closingState := req.ClosingStateWithSignatures()
	if err := req.ValidateForChannel(channel); err != nil {
		return PaymentsState{}, err
	}
	pending := PendingClose{
		Submitter:          req.Submitter,
		SubmittedHeight:    req.CurrentHeight,
		SettleAfterHeight:  req.CurrentHeight + channel.DisputePeriod,
		CloseReason:        req.CloseReason,
		SettlementFeeDenom: NativeDenom,
		SettlementFee:      req.SettlementFee,
		State:              closingState,
	}
	if err := (SettlementArbitrationInput{
		Operation:     SettlementArbitrationUnilateralClose,
		ChannelID:     channel.ChannelID,
		SignedState:   pending.State,
		CurrentHeight: req.CurrentHeight,
	}).ValidateForChannel(channel); err != nil {
		return PaymentsState{}, err
	}
	if err := pending.ValidateForChannel(channel); err != nil {
		return PaymentsState{}, err
	}
	if pending.State.Nonce < channel.FinalizedNonce {
		return PaymentsState{}, errors.New("payments close state nonce is below finalized nonce")
	}
	if pending.State.Nonce < channel.LatestState.Nonce {
		return PaymentsState{}, errors.New("payments close state nonce is below latest accepted nonce")
	}
	nextChannel := channel
	nextChannel.Status = ChannelStatusPendingClose
	nextChannel.PendingClose = pending
	nextChannel.LatestState = pending.State
	if nextChannel.DisputedNonce < pending.State.Nonce {
		nextChannel.DisputedNonce = pending.State.Nonce
	}
	feeClass := PaymentFeeClassUnilateralClose
	if req.CloseReason == CloseReasonCooperative {
		feeClass = PaymentFeeClassCooperativeClose
	}
	chargedState, _, err := ChargePaymentFee(state, feeClass, channel, req.Submitter, pending.State.StateHash, req.SettlementFee, req.CurrentHeight)
	if err != nil {
		return PaymentsState{}, err
	}
	next := chargedState.Clone()
	nextChannel, err = setChannelFinality(nextChannel, finalityForPendingClose(nextChannel), req.CurrentHeight, &next.Events)
	if err != nil {
		return PaymentsState{}, err
	}
	next.Channels[index] = nextChannel.Normalize()
	next.Edges = filterEdgesForSettledChannel(next.Edges, channel.ChannelID)
	sortChannels(next.Channels)
	return next, next.Validate()
}

func ForcedClose(state PaymentsState, channelID string, submitter string, currentHeight uint64, settlementFee string) (PaymentsState, error) {
	state = state.Export()
	if currentHeight == 0 {
		return PaymentsState{}, errors.New("payments forced close height must be positive")
	}
	index, channel, found := state.ChannelIndex(channelID)
	if !found {
		return PaymentsState{}, errors.New("payments channel not found")
	}
	if channel.Status != ChannelStatusOpen {
		return PaymentsState{}, errors.New("payments channel is not open")
	}
	if !containsString(channel.Participants, submitter) {
		return PaymentsState{}, errors.New("payments forced close submitter must be participant")
	}
	timeoutHeight := channel.LatestState.TimeoutHeight
	if channel.ChannelType == ChannelTypeAsync && channel.LatestState.ExpiryHeight != 0 {
		timeoutHeight = channel.LatestState.ExpiryHeight
	}
	if channel.ChannelType == ChannelTypeUnidirectional && channel.ExpirationHeight != 0 {
		timeoutHeight = channel.ExpirationHeight
	}
	if timeoutHeight == 0 || currentHeight <= timeoutHeight {
		return PaymentsState{}, errors.New("payments forced close timeout has not expired")
	}
	pending := PendingClose{
		Submitter:          submitter,
		SubmittedHeight:    currentHeight,
		SettleAfterHeight:  currentHeight + channel.DisputePeriod,
		CloseReason:        CloseReasonTimeout,
		SettlementFeeDenom: NativeDenom,
		SettlementFee:      settlementFee,
		State:              channel.LatestState.Normalize(),
	}
	if err := pending.ValidateForChannel(channel); err != nil {
		return PaymentsState{}, err
	}
	nextChannel := channel
	nextChannel.Status = ChannelStatusPendingClose
	nextChannel.PendingClose = pending
	if nextChannel.DisputedNonce < pending.State.Nonce {
		nextChannel.DisputedNonce = pending.State.Nonce
	}
	chargedState, _, err := ChargePaymentFee(state, PaymentFeeClassUnilateralClose, channel, submitter, pending.State.StateHash, settlementFee, currentHeight)
	if err != nil {
		return PaymentsState{}, err
	}
	next := chargedState.Clone()
	nextChannel, err = setChannelFinality(nextChannel, finalityForPendingClose(nextChannel), currentHeight, &next.Events)
	if err != nil {
		return PaymentsState{}, err
	}
	next.Channels[index] = nextChannel.Normalize()
	next.Edges = filterEdgesForSettledChannel(next.Edges, channel.ChannelID)
	sortChannels(next.Channels)
	return next, next.Validate()
}

func CooperativeClose(state PaymentsState, channelID string, closingState ChannelState, submitter string, currentHeight uint64, settlementFee string) (PaymentsState, SettlementRecord, error) {
	state = state.Export()
	if currentHeight == 0 {
		return PaymentsState{}, SettlementRecord{}, errors.New("payments cooperative close height must be positive")
	}
	index, channel, found := state.ChannelIndex(channelID)
	if !found {
		return PaymentsState{}, SettlementRecord{}, errors.New("payments channel not found")
	}
	if channel.Status != ChannelStatusOpen {
		return PaymentsState{}, SettlementRecord{}, errors.New("payments channel is not open")
	}
	if !containsString(channel.Participants, submitter) {
		return PaymentsState{}, SettlementRecord{}, errors.New("payments cooperative close submitter must be participant")
	}
	closingState = closingState.Normalize()
	if err := (SettlementArbitrationInput{
		Operation:     SettlementArbitrationCooperativeClose,
		ChannelID:     channel.ChannelID,
		SignedState:   closingState,
		CurrentHeight: currentHeight,
	}).ValidateForChannel(channel); err != nil {
		return PaymentsState{}, SettlementRecord{}, err
	}
	if err := closingState.ValidateForChannel(channel, true); err != nil {
		return PaymentsState{}, SettlementRecord{}, err
	}
	if closingState.Nonce < channel.LatestState.Nonce {
		return PaymentsState{}, SettlementRecord{}, errors.New("payments cooperative close state nonce is below latest accepted nonce")
	}
	finalBalances, err := applySettlementAdjustments(closingState.Balances, nil, nil, settlementFee, submitter)
	if err != nil {
		return PaymentsState{}, SettlementRecord{}, err
	}
	settlement := SettlementRecord{
		ChainID:            channel.ChainID,
		ChannelID:          channel.ChannelID,
		StateHash:          closingState.StateHash,
		Nonce:              closingState.Nonce,
		FinalBalances:      finalBalances,
		SettlementFeeDenom: NativeDenom,
		SettlementFee:      settlementFee,
		SettledHeight:      currentHeight,
	}
	settlement.SettlementHash = ComputeSettlementHash(settlement)
	if err := settlement.ValidateForChannel(channel); err != nil {
		return PaymentsState{}, SettlementRecord{}, err
	}
	nextChannel := channel
	nextChannel.Status = ChannelStatusSettled
	nextChannel.FinalizedNonce = settlement.Nonce
	nextChannel.LatestState = closingState
	nextChannel.PendingClose = PendingClose{}
	chargedState, _, err := ChargePaymentFee(state, PaymentFeeClassCooperativeClose, channel, submitter, closingState.StateHash, settlementFee, currentHeight)
	if err != nil {
		return PaymentsState{}, SettlementRecord{}, err
	}
	next := chargedState.Clone()
	nextChannel.Finality = ChannelFinalitySettled
	next.Events = append(next.Events, ChannelFinalityTransitionEvent(nextChannel, channel.Finality, ChannelFinalitySettled, currentHeight))
	next.Channels[index] = nextChannel.Normalize()
	next.Edges = filterEdgesForSettledChannel(next.Edges, channel.ChannelID)
	next.CustodyLocks = filterCustodyLocksForSettledChannel(next.CustodyLocks, channel.ChannelID)
	next.Settlements = append(next.Settlements, settlement)
	appendSettlementReplayRecords(&next, nextChannel, settlement, nil, currentHeight)
	sortChannels(next.Channels)
	sortSettlements(next.Settlements)
	sortClosedChannelTombstones(next.ClosedChannels)
	return next, settlement, next.Validate()
}

func ReceiverClose(state PaymentsState, channelID string, claim UnidirectionalClaim, receiver string, currentHeight uint64, settlementFee string) (PaymentsState, SettlementRecord, error) {
	state = state.Export()
	if currentHeight == 0 {
		return PaymentsState{}, SettlementRecord{}, errors.New("payments receiver close height must be positive")
	}
	index, channel, found := state.ChannelIndex(channelID)
	if !found {
		return PaymentsState{}, SettlementRecord{}, errors.New("payments channel not found")
	}
	if channel.Status != ChannelStatusOpen {
		return PaymentsState{}, SettlementRecord{}, errors.New("payments channel is not open")
	}
	if channel.ChannelType != ChannelTypeUnidirectional {
		return PaymentsState{}, SettlementRecord{}, errors.New("payments receiver close requires unidirectional channel")
	}
	receiver = normalizeAddress(receiver)
	if receiver != channel.Receiver {
		return PaymentsState{}, SettlementRecord{}, errors.New("payments receiver close submitter must be receiver")
	}
	claim = claim.Normalize()
	if err := (SettlementArbitrationInput{
		Operation:     SettlementArbitrationUnilateralClose,
		ChannelID:     channel.ChannelID,
		Claim:         claim,
		CurrentHeight: currentHeight,
	}).ValidateForChannel(channel); err != nil {
		return PaymentsState{}, SettlementRecord{}, err
	}
	if err := claim.ValidateForChannel(channel); err != nil {
		return PaymentsState{}, SettlementRecord{}, err
	}
	if err := validateUnidirectionalClaimProgress(channel.LatestClaim, claim); err != nil {
		return PaymentsState{}, SettlementRecord{}, err
	}
	if currentHeight > claim.ExpirationHeight+channel.DisputePeriod {
		return PaymentsState{}, SettlementRecord{}, errors.New("payments receiver close claim has expired")
	}
	finalBalances, err := finalBalancesForUnidirectionalClaim(channel, claim, settlementFee, receiver)
	if err != nil {
		return PaymentsState{}, SettlementRecord{}, err
	}
	settlement := SettlementRecord{
		ChainID:            channel.ChainID,
		ChannelID:          channel.ChannelID,
		StateHash:          claim.StateHash,
		Nonce:              claim.Nonce,
		FinalBalances:      finalBalances,
		SettlementFeeDenom: NativeDenom,
		SettlementFee:      settlementFee,
		SettledHeight:      currentHeight,
	}
	settlement.SettlementHash = ComputeSettlementHash(settlement)
	if err := settlement.ValidateForChannel(channel); err != nil {
		return PaymentsState{}, SettlementRecord{}, err
	}
	nextChannel := channel
	nextChannel.Status = ChannelStatusSettled
	nextChannel.FinalizedNonce = settlement.Nonce
	nextChannel.LatestClaim = claim
	nextChannel.PendingClose = PendingClose{}
	chargedState, _, err := ChargePaymentFee(state, PaymentFeeClassUnilateralClose, channel, receiver, claim.StateHash, settlementFee, currentHeight)
	if err != nil {
		return PaymentsState{}, SettlementRecord{}, err
	}
	next := chargedState.Clone()
	nextChannel.Finality = ChannelFinalitySettled
	next.Events = append(next.Events, ChannelFinalityTransitionEvent(nextChannel, channel.Finality, ChannelFinalitySettled, currentHeight))
	next.Channels[index] = nextChannel.Normalize()
	next.Edges = filterEdgesForSettledChannel(next.Edges, channel.ChannelID)
	next.CustodyLocks = filterCustodyLocksForSettledChannel(next.CustodyLocks, channel.ChannelID)
	next.Settlements = append(next.Settlements, settlement)
	appendSettlementReplayRecords(&next, nextChannel, settlement, nil, currentHeight)
	sortChannels(next.Channels)
	sortSettlements(next.Settlements)
	sortClosedChannelTombstones(next.ClosedChannels)
	return next, settlement, next.Validate()
}

func PayerReclaim(state PaymentsState, channelID string, payer string, currentHeight uint64, settlementFee string) (PaymentsState, SettlementRecord, error) {
	state = state.Export()
	if currentHeight == 0 {
		return PaymentsState{}, SettlementRecord{}, errors.New("payments payer reclaim height must be positive")
	}
	index, channel, found := state.ChannelIndex(channelID)
	if !found {
		return PaymentsState{}, SettlementRecord{}, errors.New("payments channel not found")
	}
	if channel.Status != ChannelStatusOpen {
		return PaymentsState{}, SettlementRecord{}, errors.New("payments channel is not open")
	}
	if channel.ChannelType != ChannelTypeUnidirectional {
		return PaymentsState{}, SettlementRecord{}, errors.New("payments payer reclaim requires unidirectional channel")
	}
	payer = normalizeAddress(payer)
	if payer != channel.Payer {
		return PaymentsState{}, SettlementRecord{}, errors.New("payments reclaim submitter must be payer")
	}
	expirationHeight := channel.ExpirationHeight
	claim := channel.LatestClaim.Normalize()
	if !claim.IsZero() {
		expirationHeight = claim.ExpirationHeight
	}
	if currentHeight <= expirationHeight+channel.DisputePeriod {
		return PaymentsState{}, SettlementRecord{}, errors.New("payments reclaim is still in dispute window")
	}
	stateHash := channel.OpeningStateHash
	nonce := channel.LatestState.Nonce
	var finalBalances []Balance
	var err error
	if claim.IsZero() {
		finalBalances, err = applySettlementAdjustments([]Balance{
			{Participant: channel.Payer, Amount: channel.Collateral},
			{Participant: channel.Receiver, Amount: "0"},
		}, nil, nil, settlementFee, payer)
	} else {
		stateHash = claim.StateHash
		nonce = claim.Nonce
		finalBalances, err = finalBalancesForUnidirectionalClaim(channel, claim, settlementFee, payer)
	}
	if err != nil {
		return PaymentsState{}, SettlementRecord{}, err
	}
	settlement := SettlementRecord{
		ChainID:            channel.ChainID,
		ChannelID:          channel.ChannelID,
		StateHash:          stateHash,
		Nonce:              nonce,
		FinalBalances:      finalBalances,
		SettlementFeeDenom: NativeDenom,
		SettlementFee:      settlementFee,
		SettledHeight:      currentHeight,
	}
	settlement.SettlementHash = ComputeSettlementHash(settlement)
	if err := settlement.ValidateForChannel(channel); err != nil {
		return PaymentsState{}, SettlementRecord{}, err
	}
	nextChannel := channel
	chargedState, _, err := ChargePaymentFee(state, PaymentFeeClassUnilateralClose, channel, payer, stateHash, settlementFee, currentHeight)
	if err != nil {
		return PaymentsState{}, SettlementRecord{}, err
	}
	next := chargedState.Clone()
	var transitionErr error
	nextChannel, transitionErr = setChannelFinality(nextChannel, ChannelFinalityExpired, currentHeight, &next.Events)
	if transitionErr != nil {
		return PaymentsState{}, SettlementRecord{}, transitionErr
	}
	nextChannel.Status = ChannelStatusSettled
	nextChannel.FinalizedNonce = settlement.Nonce
	nextChannel.PendingClose = PendingClose{}
	nextChannel.Finality = ChannelFinalitySettled
	next.Events = append(next.Events, ChannelFinalityTransitionEvent(nextChannel, ChannelFinalityExpired, ChannelFinalitySettled, currentHeight))
	next.Channels[index] = nextChannel.Normalize()
	next.Edges = filterEdgesForSettledChannel(next.Edges, channel.ChannelID)
	next.CustodyLocks = filterCustodyLocksForSettledChannel(next.CustodyLocks, channel.ChannelID)
	next.Settlements = append(next.Settlements, settlement)
	appendSettlementReplayRecords(&next, nextChannel, settlement, nil, currentHeight)
	sortChannels(next.Channels)
	sortSettlements(next.Settlements)
	sortClosedChannelTombstones(next.ClosedChannels)
	return next, settlement, next.Validate()
}

func FinalizeSettlement(state PaymentsState, channelID string, currentHeight uint64) (PaymentsState, SettlementRecord, error) {
	return FinalizeSettlementWithRequest(state, FinalSettlementRequest{ChannelID: channelID, CurrentHeight: currentHeight})
}

func FinalizeSettlementWithRequest(state PaymentsState, req FinalSettlementRequest) (PaymentsState, SettlementRecord, error) {
	state = state.Export()
	req = req.Normalize()
	if req.CurrentHeight == 0 {
		return PaymentsState{}, SettlementRecord{}, errors.New("payments settlement height must be positive")
	}
	index, channel, found := state.ChannelIndex(req.ChannelID)
	if !found {
		return PaymentsState{}, SettlementRecord{}, errors.New("payments channel not found")
	}
	if channel.Status != ChannelStatusPendingClose {
		return PaymentsState{}, SettlementRecord{}, errors.New("payments channel is not pending close")
	}
	if req.CurrentHeight < channel.PendingClose.SettleAfterHeight {
		return PaymentsState{}, SettlementRecord{}, errors.New("payments settlement is still in dispute window")
	}
	resolutions := mergeConditionResolutions(channel.PendingClose.ConditionProofs, req.ResolvedConditions)
	if err := (SettlementArbitrationInput{
		Operation:       SettlementArbitrationFinalSettlement,
		ChannelID:       channel.ChannelID,
		SignedState:     channel.PendingClose.State,
		ConditionProofs: resolutions,
		CurrentHeight:   req.CurrentHeight,
	}).ValidateForChannel(channel); err != nil {
		return PaymentsState{}, SettlementRecord{}, err
	}
	if err := rejectReusedConditionClaims(state, channel, resolutions); err != nil {
		return PaymentsState{}, SettlementRecord{}, err
	}
	if err := validateConditionResolutionsForState(channel.PendingClose.State, channel, resolutions, true); err != nil {
		return PaymentsState{}, SettlementRecord{}, err
	}
	baseBalances, err := settlementBalancesWithConditions(channel.PendingClose.State, channel, resolutions)
	if err != nil {
		return PaymentsState{}, SettlementRecord{}, err
	}
	finalBalances, err := applySettlementAdjustments(baseBalances, channel.PendingClose.Penalties, channel.PendingClose.PenaltyAllocations, channel.PendingClose.SettlementFee, channel.PendingClose.Submitter)
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
		SettledHeight:      req.CurrentHeight,
	}
	settlement.SettlementHash = ComputeSettlementHash(settlement)
	if err := settlement.ValidateForChannel(channel); err != nil {
		return PaymentsState{}, SettlementRecord{}, err
	}
	nextChannel := channel
	next := state.Clone()
	nextChannel, err = setChannelFinality(nextChannel, ChannelFinalityFinalizable, req.CurrentHeight, &next.Events)
	if err != nil {
		return PaymentsState{}, SettlementRecord{}, err
	}
	nextChannel.Status = ChannelStatusSettled
	nextChannel.FinalizedNonce = settlement.Nonce
	settledFinality := finalityForSettledChannel(nextChannel)
	nextChannel.Finality = settledFinality
	next.Events = append(next.Events, ChannelFinalityTransitionEvent(nextChannel, ChannelFinalityFinalizable, settledFinality, req.CurrentHeight))
	nextChannel.PendingClose = PendingClose{}
	next.Channels[index] = nextChannel.Normalize()
	next.Edges = filterEdgesForSettledChannel(next.Edges, channel.ChannelID)
	next.CustodyLocks = filterCustodyLocksForSettledChannel(next.CustodyLocks, channel.ChannelID)
	next.Settlements = append(next.Settlements, settlement)
	appendSettlementReplayRecords(&next, nextChannel, settlement, resolutions, req.CurrentHeight)
	sortChannels(next.Channels)
	sortSettlements(next.Settlements)
	sortClosedChannelTombstones(next.ClosedChannels)
	sortConditionClaimRecords(next.ConditionClaims)
	return next, settlement, next.Validate()
}

func AddSettlementBatch(state PaymentsState, batch SettlementBatch) (PaymentsState, error) {
	state = state.Export()
	if err := state.Validate(); err != nil {
		return PaymentsState{}, err
	}
	batch = batch.Normalize()
	if err := batch.Validate(); err != nil {
		return PaymentsState{}, err
	}
	for _, existing := range state.Batches {
		if existing.BatchID == batch.BatchID {
			return PaymentsState{}, errors.New("payments settlement batch already exists")
		}
	}
	for _, op := range batch.Operations {
		channel, found := state.ChannelByID(op.ChannelID)
		if !found {
			return PaymentsState{}, errors.New("payments settlement batch references unknown channel")
		}
		if op.Nonce < channel.FinalizedNonce {
			return PaymentsState{}, errors.New("payments settlement batch operation nonce below finalized nonce")
		}
	}
	next := state.Clone()
	next.Batches = append(next.Batches, batch)
	sortBatches(next.Batches)
	return next, next.Validate()
}

func applySettlementAdjustments(balances []Balance, penalties []Penalty, allocations []PenaltyAllocation, feeText, feePayer string) ([]Balance, error) {
	amounts := make(map[string]sdkmath.Int, len(balances))
	for _, balance := range normalizeBalances(balances) {
		amount, err := parseNonNegativeInt("payments final balance", balance.Amount)
		if err != nil {
			return nil, err
		}
		amounts[balance.Participant] = amount
	}
	for _, penalty := range normalizePenalties(penalties) {
		amount, err := parsePositiveInt("payments penalty amount", penalty.Amount)
		if err != nil {
			return nil, err
		}
		offenderBalance, found := amounts[penalty.Offender]
		if !found || offenderBalance.LT(amount) {
			return nil, errors.New("payments penalty exceeds offender balance")
		}
		amounts[penalty.Offender] = offenderBalance.Sub(amount)
		amounts[penalty.Recipient] = amounts[penalty.Recipient].Add(amount)
	}
	for _, allocation := range normalizePenaltyAllocations(allocations) {
		amount, err := parsePositiveInt("payments penalty allocation amount", allocation.Amount)
		if err != nil {
			return nil, err
		}
		offenderBalance, found := amounts[allocation.Offender]
		if !found || offenderBalance.LT(amount) {
			return nil, errors.New("payments penalty allocation exceeds offender balance")
		}
		amounts[allocation.Offender] = offenderBalance.Sub(amount)
	}
	fee, err := parseNonNegativeInt("payments settlement fee", feeText)
	if err != nil {
		return nil, err
	}
	if fee.IsPositive() {
		balance, found := amounts[feePayer]
		if !found || balance.LT(fee) {
			return nil, errors.New("payments settlement fee exceeds payer balance")
		}
		amounts[feePayer] = balance.Sub(fee)
	}
	out := make([]Balance, 0, len(amounts))
	for participant, amount := range amounts {
		out = append(out, Balance{Participant: participant, Amount: amount.String()})
	}
	return normalizeBalances(out), nil
}

func settlementBalancesWithConditions(state ChannelState, channel ChannelRecord, resolutions []ConditionResolution) ([]Balance, error) {
	state = state.Normalize()
	if len(state.Conditions) == 0 {
		return state.Balances, nil
	}
	amounts := make(map[string]sdkmath.Int, len(state.Balances))
	for _, balance := range normalizeBalances(state.Balances) {
		amount, err := parseNonNegativeInt("payments settlement base balance", balance.Amount)
		if err != nil {
			return nil, err
		}
		amounts[balance.Participant] = amount
	}
	reserveByParticipant := map[string]sdkmath.Int{}
	if state.ChannelType == ChannelTypeBidirectional {
		reserveA, err := parseNonNegativeInt("payments settlement reserve a", state.ReserveA)
		if err != nil {
			return nil, err
		}
		reserveB, err := parseNonNegativeInt("payments settlement reserve b", state.ReserveB)
		if err != nil {
			return nil, err
		}
		reserveByParticipant[state.ParticipantA] = reserveA
		reserveByParticipant[state.ParticipantB] = reserveB
	}
	resolutionByID := make(map[string]ConditionResolution, len(resolutions))
	for _, resolution := range normalizeConditionResolutions(resolutions) {
		resolutionByID[resolution.ConditionID] = resolution
	}
	for _, condition := range state.Conditions {
		condition = condition.Normalize()
		resolution, found := resolutionByID[condition.ConditionID]
		if !found {
			return nil, errors.New("payments condition is unresolved")
		}
		amount, err := parsePositiveInt("payments condition amount", condition.Amount)
		if err != nil {
			return nil, err
		}
		reserve := reserveByParticipant[condition.Payer]
		if reserve.LT(amount) {
			return nil, errors.New("payments condition exceeds reserved balance")
		}
		reserveByParticipant[condition.Payer] = reserve.Sub(amount)
		recipient := resolution.Recipient
		amounts[recipient] = amounts[recipient].Add(amount)
	}
	for participant, reserve := range reserveByParticipant {
		amounts[participant] = amounts[participant].Add(reserve)
	}
	out := make([]Balance, 0, len(amounts))
	for participant, amount := range amounts {
		if !containsString(channel.Participants, participant) {
			return nil, errors.New("payments settlement condition participant must be in channel")
		}
		out = append(out, Balance{Participant: participant, Amount: amount.String()})
	}
	return normalizeBalances(out), nil
}

func appendSettlementReplayRecords(state *PaymentsState, channel ChannelRecord, settlement SettlementRecord, resolutions []ConditionResolution, height uint64) {
	channel = channel.Normalize()
	settlement = settlement.Normalize()
	tombstone := ClosedChannelTombstone{
		ChainID:        channel.ChainID,
		ChannelID:      channel.ChannelID,
		FinalizedNonce: settlement.Nonce,
		StateHash:      settlement.StateHash,
		ClosedHeight:   height,
		ExpiresHeight:  height + DefaultReplayHorizon,
	}.Normalize()
	state.ClosedChannels = upsertClosedChannelTombstone(state.ClosedChannels, tombstone)
	for _, resolution := range normalizeConditionResolutions(resolutions) {
		state.ConditionClaims = append(state.ConditionClaims, ConditionClaimRecord{
			ChainID:        channel.ChainID,
			ChannelID:      channel.ChannelID,
			ConditionID:    resolution.ConditionID,
			EvidenceHash:   resolution.EvidenceHash,
			ResolvedHeight: height,
			ExpiresHeight:  height + DefaultReplayHorizon,
		}.Normalize())
	}
}

func upsertClosedChannelTombstone(tombstones []ClosedChannelTombstone, next ClosedChannelTombstone) []ClosedChannelTombstone {
	out := make([]ClosedChannelTombstone, 0, len(tombstones)+1)
	replaced := false
	for _, tombstone := range tombstones {
		tombstone = tombstone.Normalize()
		if tombstone.ChannelID == next.ChannelID {
			out = append(out, next)
			replaced = true
			continue
		}
		out = append(out, tombstone)
	}
	if !replaced {
		out = append(out, next)
	}
	sortClosedChannelTombstones(out)
	return out
}

func finalBalancesForUnidirectionalClaim(channel ChannelRecord, claim UnidirectionalClaim, settlementFee, feePayer string) ([]Balance, error) {
	collateral, err := parsePositiveInt("payments channel collateral", channel.Collateral)
	if err != nil {
		return nil, err
	}
	claimed, err := parseNonNegativeInt("payments claimed amount", claim.ClaimedAmount)
	if err != nil {
		return nil, err
	}
	if claimed.GT(collateral) {
		return nil, errors.New("payments claimed amount exceeds locked collateral")
	}
	return applySettlementAdjustments([]Balance{
		{Participant: channel.Payer, Amount: collateral.Sub(claimed).String()},
		{Participant: channel.Receiver, Amount: claimed.String()},
	}, nil, nil, settlementFee, feePayer)
}

func filterEdgesForSettledChannel(edges []ChannelEdge, channelID string) []ChannelEdge {
	channelID = normalizeHash(channelID)
	out := make([]ChannelEdge, 0, len(edges))
	for _, edge := range edges {
		if edge.Normalize().ChannelID == channelID {
			continue
		}
		out = append(out, edge)
	}
	return out
}

func filterCustodyLocksForSettledChannel(locks []CustodyLock, channelID string) []CustodyLock {
	channelID = normalizeHash(channelID)
	out := make([]CustodyLock, 0, len(locks))
	for _, lock := range locks {
		if lock.Normalize().ChannelID == channelID {
			continue
		}
		out = append(out, lock)
	}
	return out
}

func latestSettlementHashForChannel(settlements []SettlementRecord, channelID string) string {
	channelID = normalizeHash(channelID)
	var latest SettlementRecord
	for _, settlement := range settlements {
		settlement = settlement.Normalize()
		if settlement.ChannelID != channelID {
			continue
		}
		if settlement.SettledHeight >= latest.SettledHeight {
			latest = settlement
		}
	}
	return latest.SettlementHash
}
