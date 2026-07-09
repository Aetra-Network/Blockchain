package types

import (
	"errors"
	"fmt"
)

func setChannelFinality(channel ChannelRecord, finality ChannelFinality, height uint64, events *[]PaymentEvent) (ChannelRecord, error) {
	channel = channel.Normalize()
	if height == 0 {
		return ChannelRecord{}, errors.New("payments finality transition height must be positive")
	}
	if !IsChannelFinality(finality) {
		return ChannelRecord{}, fmt.Errorf("unknown payments channel finality %q", finality)
	}
	previous := channel.Finality
	if previous == finality {
		return channel, nil
	}
	channel.Finality = finality
	if err := validateChannelFinalityForStatus(channel); err != nil {
		return ChannelRecord{}, err
	}
	if events != nil {
		*events = append(*events, ChannelFinalityTransitionEvent(channel, previous, finality, height))
	}
	return channel.Normalize(), nil
}

func finalityForPendingClose(channel ChannelRecord) ChannelFinality {
	return DerivedChannelFinality(channel)
}

func finalityForSettledChannel(channel ChannelRecord) ChannelFinality {
	channel = channel.Normalize()
	if len(channel.PendingClose.Penalties) > 0 || len(channel.PendingClose.PenaltyAllocations) > 0 {
		return ChannelFinalityPenalized
	}
	return ChannelFinalitySettled
}

func OpenChannelFromRequest(state PaymentsState, req ChannelOpenRequest) (PaymentsState, PaymentEvent, error) {
	channel, err := BuildChannelFromOpenRequest(req)
	if err != nil {
		return PaymentsState{}, PaymentEvent{}, err
	}
	next, event, err := openChannelRecord(state, channel)
	return next, event, err
}

func OpenChannel(state PaymentsState, channel ChannelRecord) (PaymentsState, error) {
	next, _, err := openChannelRecord(state, channel)
	return next, err
}

func openChannelRecord(state PaymentsState, channel ChannelRecord) (PaymentsState, PaymentEvent, error) {
	state = state.Export()
	if err := state.Validate(); err != nil {
		return PaymentsState{}, PaymentEvent{}, err
	}
	channel = channel.Normalize()
	if channel.Status != ChannelStatusOpen {
		return PaymentsState{}, PaymentEvent{}, errors.New("payments new channel must start open")
	}
	if _, found := state.ChannelByID(channel.ChannelID); found {
		return PaymentsState{}, PaymentEvent{}, errors.New("payments channel already exists")
	}
	if err := (SettlementArbitrationInput{
		Operation: SettlementArbitrationOpen,
		ChannelID: channel.ChannelID,
	}).ValidateForChannel(channel); err != nil {
		return PaymentsState{}, PaymentEvent{}, err
	}
	if err := channel.LatestState.ValidateForChannel(channel, true); err != nil {
		return PaymentsState{}, PaymentEvent{}, err
	}
	if channel.OpeningStateHash == "" {
		channel.OpeningStateHash = channel.LatestState.StateHash
	}
	if channel.OpeningStateHash != channel.LatestState.StateHash {
		return PaymentsState{}, PaymentEvent{}, errors.New("payments opening state hash mismatch")
	}
	channel.FinalizedNonce = 0
	channel.Finality = ChannelFinalityOpen
	if err := channel.Validate(); err != nil {
		return PaymentsState{}, PaymentEvent{}, err
	}
	lock := CustodyLock{ChannelID: channel.ChannelID, Denom: NativeDenom, Amount: channel.Collateral}.Normalize()
	if err := lock.ValidateForChannel(channel); err != nil {
		return PaymentsState{}, PaymentEvent{}, err
	}
	if _, found := state.CustodyLockByChannel(channel.ChannelID); found {
		return PaymentsState{}, PaymentEvent{}, errors.New("payments custody lock already exists")
	}
	chargedState, _, err := ChargePaymentFee(state, PaymentFeeClassChannelOpen, channel, channel.Participants[0], channel.ChannelID, channel.OpeningFeePaid, channel.OpenHeight)
	if err != nil {
		return PaymentsState{}, PaymentEvent{}, err
	}
	event := ChannelOpenEvent(channel)
	next := chargedState.Clone()
	next.Channels = append(next.Channels, channel)
	next.CustodyLocks = append(next.CustodyLocks, lock)
	next.Events = append(next.Events, event)
	next.Events = append(next.Events, ChannelFinalityTransitionEvent(channel, "", ChannelFinalityOpen, channel.OpenHeight))
	sortChannels(next.Channels)
	sortCustodyLocks(next.CustodyLocks)
	return next, event, next.Validate()
}

func AcceptSignedState(state PaymentsState, channelID string, nextState ChannelState, currentHeight uint64) (PaymentsState, error) {
	state = state.Export()
	if currentHeight == 0 {
		return PaymentsState{}, errors.New("payments state update height must be positive")
	}
	index, channel, found := state.ChannelIndex(channelID)
	if !found {
		return PaymentsState{}, errors.New("payments channel not found")
	}
	if channel.Status != ChannelStatusOpen {
		return PaymentsState{}, errors.New("payments channel is not open")
	}
	nextState = nextState.Normalize()
	if err := nextState.ValidateForChannel(channel, true); err != nil {
		return PaymentsState{}, err
	}
	if nextState.Nonce <= channel.LatestState.Nonce {
		return PaymentsState{}, errors.New("payments channel state nonce must strictly increase")
	}
	if err := ValidatePreviousHashContinuity(channel, nextState); err != nil {
		return PaymentsState{}, err
	}
	nextChannel := channel
	nextChannel.LatestState = nextState
	next := state.Clone()
	next.Channels[index] = nextChannel.Normalize()
	sortChannels(next.Channels)
	return next, next.Validate()
}

func AcceptAsyncCheckpoint(state PaymentsState, channelID string, checkpoint ChannelState, deltas []AsyncPaymentDelta, submitter string, currentHeight uint64) (PaymentsState, error) {
	state = state.Export()
	if currentHeight == 0 {
		return PaymentsState{}, errors.New("payments async checkpoint height must be positive")
	}
	index, channel, found := state.ChannelIndex(channelID)
	if !found {
		return PaymentsState{}, errors.New("payments channel not found")
	}
	if channel.Status != ChannelStatusOpen {
		return PaymentsState{}, errors.New("payments channel is not open")
	}
	if channel.ChannelType != ChannelTypeAsync {
		return PaymentsState{}, errors.New("payments checkpoint requires async channel")
	}
	if !containsString(channel.Participants, submitter) {
		return PaymentsState{}, errors.New("payments async checkpoint submitter must be participant")
	}
	checkpoint = checkpoint.Normalize()
	if err := checkpoint.ValidateForChannel(channel, false); err != nil {
		return PaymentsState{}, err
	}
	if checkpoint.CheckpointNonce <= channel.LatestState.CheckpointNonce {
		return PaymentsState{}, errors.New("payments async checkpoint nonce must increase")
	}
	proof := AsyncDeltaDisputeProof{
		ProofID:         HashParts("async-checkpoint-proof", checkpoint.StateHash),
		ChannelID:       channel.ChannelID,
		CheckpointState: checkpoint,
		Deltas:          deltas,
		EvidenceHash:    HashParts("async-dispute", checkpoint.StateHash, ComputeAsyncDeltaRootForChannel(channel, deltas)),
	}
	if err := proof.ValidateForChannel(channel, currentHeight); err != nil {
		return PaymentsState{}, err
	}
	nextChannel := channel
	nextChannel.LatestState = checkpoint
	next := state.Clone()
	next.Channels[index] = nextChannel.Normalize()
	sortChannels(next.Channels)
	return next, next.Validate()
}

func RegisterUpdateCheckpoint(state PaymentsState, req ChannelUpdateRequest) (PaymentsState, ChannelUpdateResult, error) {
	state = state.Export()
	channel, found := state.ChannelByID(req.ChannelID)
	if !found {
		return PaymentsState{}, ChannelUpdateResult{}, errors.New("payments channel not found")
	}
	result, err := ValidateOffchainUpdate(channel, req)
	if err != nil {
		return PaymentsState{}, ChannelUpdateResult{}, err
	}
	if !req.Normalize().RegisterCheckpoint {
		return state, result, nil
	}
	var next PaymentsState
	if channel.ChannelType == ChannelTypeAsync || len(req.Normalize().AsyncDeltas) > 0 {
		next, err = AcceptAsyncCheckpoint(state, channel.ChannelID, req.Normalize().State, req.Normalize().AsyncDeltas, req.Normalize().Submitter, req.Normalize().CurrentHeight)
	} else {
		next, err = AcceptSignedState(state, channel.ChannelID, req.Normalize().State, req.Normalize().CurrentHeight)
	}
	if err != nil {
		return PaymentsState{}, ChannelUpdateResult{}, err
	}
	next, _, err = ChargePaymentFee(next, PaymentFeeClassChannelCheckpoint, channel, req.Normalize().Submitter, req.Normalize().State.StateHash, req.Normalize().CheckpointFeePaid, req.Normalize().CurrentHeight)
	if err != nil {
		return PaymentsState{}, ChannelUpdateResult{}, err
	}
	result.CheckpointRegistered = true
	return next, result, nil
}

func AdvanceChannelFinality(state PaymentsState, channelID string, currentHeight uint64) (PaymentsState, error) {
	state = state.Export()
	if currentHeight == 0 {
		return PaymentsState{}, errors.New("payments finality advance height must be positive")
	}
	index, channel, found := state.ChannelIndex(channelID)
	if !found {
		return PaymentsState{}, errors.New("payments channel not found")
	}
	nextFinality := FinalityAfterPendingClose(channel, currentHeight)
	if nextFinality == channel.Finality {
		return state, nil
	}
	next := state.Clone()
	nextChannel, err := setChannelFinality(channel, nextFinality, currentHeight, &next.Events)
	if err != nil {
		return PaymentsState{}, err
	}
	next.Channels[index] = nextChannel.Normalize()
	sortChannels(next.Channels)
	return next, next.Validate()
}

func ValidateLockedCollateralForFinality(state PaymentsState) error {
	state = state.Export()
	lockByChannel := make(map[string]CustodyLock, len(state.CustodyLocks))
	for _, lock := range state.CustodyLocks {
		lock = lock.Normalize()
		lockByChannel[lock.ChannelID] = lock
	}
	for _, channel := range state.Channels {
		channel = channel.Normalize()
		lock, locked := lockByChannel[channel.ChannelID]
		switch channel.Finality {
		case ChannelFinalitySettled, ChannelFinalityPenalized:
			if channel.Status == ChannelStatusSettled {
				if locked {
					return errors.New("payments settled finality must not retain custody lock")
				}
				continue
			}
		}
		if !locked {
			return errors.New("payments unsettled finality must retain custody lock")
		}
		if err := lock.ValidateForChannel(channel); err != nil {
			return err
		}
	}
	return nil
}

func stateStrongerThan(candidate, current ChannelState) bool {
	candidate = candidate.Normalize()
	current = current.Normalize()
	if candidate.Nonce > current.Nonce {
		return true
	}
	return candidate.ChannelType == ChannelTypeAsync && candidate.CheckpointNonce > current.CheckpointNonce
}
