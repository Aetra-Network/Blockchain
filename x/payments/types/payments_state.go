package types

import (
	"errors"
	"strings"
)

type PaymentsState struct {
	Channels                 []ChannelRecord
	Edges                    []ChannelEdge
	VirtualChannels          []VirtualChannel
	Settlements              []SettlementRecord
	Batches                  []SettlementBatch
	CustodyLocks             []CustodyLock
	ClosedChannels           []ClosedChannelTombstone
	ConditionClaims          []ConditionClaimRecord
	ValidatorPaymentServices []ValidatorPaymentServiceMetadata
	ValidatorWatchRegistries []ValidatorWatchRegistration
	FeeSchedule              PaymentFeeSchedule
	FeeMultipliers           []PaymentFeeMultiplier
	FeeCharges               []PaymentFeeCharge
	FeeRefunds               []PaymentFeeRefund
	SecurityReserveHooks     []SecurityReserveAllocationHook
	InclusionLatencies       []SettlementInclusionLatency
	AsyncFinalizationQueue   []AsyncFinalizationJob
	AsyncPromiseExpiryQueue  []AsyncPromiseExpiryJob
	AsyncCompletions         []AsyncSettlementCompletion
	Events                   []PaymentEvent
}

func EmptyState() PaymentsState {
	return PaymentsState{
		Channels:                 []ChannelRecord{},
		Edges:                    []ChannelEdge{},
		VirtualChannels:          []VirtualChannel{},
		Settlements:              []SettlementRecord{},
		Batches:                  []SettlementBatch{},
		CustodyLocks:             []CustodyLock{},
		ClosedChannels:           []ClosedChannelTombstone{},
		ConditionClaims:          []ConditionClaimRecord{},
		ValidatorPaymentServices: []ValidatorPaymentServiceMetadata{},
		ValidatorWatchRegistries: []ValidatorWatchRegistration{},
		FeeSchedule:              DefaultPaymentFeeSchedule(),
		FeeMultipliers:           []PaymentFeeMultiplier{},
		FeeCharges:               []PaymentFeeCharge{},
		FeeRefunds:               []PaymentFeeRefund{},
		SecurityReserveHooks:     []SecurityReserveAllocationHook{},
		InclusionLatencies:       []SettlementInclusionLatency{},
		AsyncFinalizationQueue:   []AsyncFinalizationJob{},
		AsyncPromiseExpiryQueue:  []AsyncPromiseExpiryJob{},
		AsyncCompletions:         []AsyncSettlementCompletion{},
		Events:                   []PaymentEvent{},
	}
}

func ImportState(state PaymentsState) (PaymentsState, error) {
	state = state.Export()
	if err := state.Validate(); err != nil {
		return PaymentsState{}, err
	}
	return state, nil
}

func (s PaymentsState) Export() PaymentsState {
	out := s.Clone()
	sortChannels(out.Channels)
	sortEdges(out.Edges)
	sortVirtualChannels(out.VirtualChannels)
	sortSettlements(out.Settlements)
	sortBatches(out.Batches)
	sortCustodyLocks(out.CustodyLocks)
	sortClosedChannelTombstones(out.ClosedChannels)
	sortConditionClaimRecords(out.ConditionClaims)
	sortValidatorPaymentServices(out.ValidatorPaymentServices)
	sortValidatorWatchRegistrations(out.ValidatorWatchRegistries)
	sortPaymentFeeMultipliers(out.FeeMultipliers)
	sortPaymentFeeCharges(out.FeeCharges)
	sortPaymentFeeRefunds(out.FeeRefunds)
	sortSecurityReserveAllocationHooks(out.SecurityReserveHooks)
	sortSettlementInclusionLatencies(out.InclusionLatencies)
	sortAsyncFinalizationJobs(out.AsyncFinalizationQueue)
	sortAsyncPromiseExpiryJobs(out.AsyncPromiseExpiryQueue)
	sortAsyncSettlementCompletions(out.AsyncCompletions)
	return out
}

func (s PaymentsState) Clone() PaymentsState {
	out := PaymentsState{
		Channels:                 make([]ChannelRecord, len(s.Channels)),
		Edges:                    make([]ChannelEdge, len(s.Edges)),
		VirtualChannels:          make([]VirtualChannel, len(s.VirtualChannels)),
		Settlements:              make([]SettlementRecord, len(s.Settlements)),
		Batches:                  make([]SettlementBatch, len(s.Batches)),
		CustodyLocks:             make([]CustodyLock, len(s.CustodyLocks)),
		ClosedChannels:           make([]ClosedChannelTombstone, len(s.ClosedChannels)),
		ConditionClaims:          make([]ConditionClaimRecord, len(s.ConditionClaims)),
		ValidatorPaymentServices: make([]ValidatorPaymentServiceMetadata, len(s.ValidatorPaymentServices)),
		ValidatorWatchRegistries: make([]ValidatorWatchRegistration, len(s.ValidatorWatchRegistries)),
		FeeSchedule:              s.FeeSchedule.Normalize(),
		FeeMultipliers:           make([]PaymentFeeMultiplier, len(s.FeeMultipliers)),
		FeeCharges:               make([]PaymentFeeCharge, len(s.FeeCharges)),
		FeeRefunds:               make([]PaymentFeeRefund, len(s.FeeRefunds)),
		SecurityReserveHooks:     make([]SecurityReserveAllocationHook, len(s.SecurityReserveHooks)),
		InclusionLatencies:       make([]SettlementInclusionLatency, len(s.InclusionLatencies)),
		AsyncFinalizationQueue:   make([]AsyncFinalizationJob, len(s.AsyncFinalizationQueue)),
		AsyncPromiseExpiryQueue:  make([]AsyncPromiseExpiryJob, len(s.AsyncPromiseExpiryQueue)),
		AsyncCompletions:         make([]AsyncSettlementCompletion, len(s.AsyncCompletions)),
		Events:                   make([]PaymentEvent, len(s.Events)),
	}
	for i, channel := range s.Channels {
		out.Channels[i] = channel.Normalize()
	}
	for i, edge := range s.Edges {
		out.Edges[i] = edge.Normalize()
	}
	for i, vc := range s.VirtualChannels {
		out.VirtualChannels[i] = vc.Normalize()
	}
	for i, settlement := range s.Settlements {
		out.Settlements[i] = settlement.Normalize()
	}
	for i, batch := range s.Batches {
		out.Batches[i] = batch.Normalize()
	}
	for i, lock := range s.CustodyLocks {
		out.CustodyLocks[i] = lock.Normalize()
	}
	for i, tombstone := range s.ClosedChannels {
		out.ClosedChannels[i] = tombstone.Normalize()
	}
	for i, claim := range s.ConditionClaims {
		out.ConditionClaims[i] = claim.Normalize()
	}
	for i, metadata := range s.ValidatorPaymentServices {
		out.ValidatorPaymentServices[i] = metadata.Normalize()
	}
	for i, registration := range s.ValidatorWatchRegistries {
		out.ValidatorWatchRegistries[i] = registration.Normalize()
	}
	for i, multiplier := range s.FeeMultipliers {
		out.FeeMultipliers[i] = multiplier.Normalize()
	}
	for i, charge := range s.FeeCharges {
		out.FeeCharges[i] = charge.Normalize()
	}
	for i, refund := range s.FeeRefunds {
		out.FeeRefunds[i] = refund.Normalize()
	}
	for i, hook := range s.SecurityReserveHooks {
		out.SecurityReserveHooks[i] = hook.Normalize()
	}
	for i, latency := range s.InclusionLatencies {
		out.InclusionLatencies[i] = latency.Normalize()
	}
	for i, job := range s.AsyncFinalizationQueue {
		out.AsyncFinalizationQueue[i] = job.Normalize()
	}
	for i, job := range s.AsyncPromiseExpiryQueue {
		out.AsyncPromiseExpiryQueue[i] = job.Normalize()
	}
	for i, completion := range s.AsyncCompletions {
		out.AsyncCompletions[i] = completion.Normalize()
	}
	for i, event := range s.Events {
		out.Events[i] = event.Normalize()
	}
	return out
}

func (s PaymentsState) Validate() error {
	if err := validateChannels(s.Channels); err != nil {
		return err
	}
	if err := validateEdges(s.Channels, s.Edges); err != nil {
		return err
	}
	if err := validateVirtualChannels(s.Channels, s.VirtualChannels); err != nil {
		return err
	}
	if err := validateSettlements(s.Channels, s.Settlements); err != nil {
		return err
	}
	if err := validateBatches(s.Channels, s.Batches); err != nil {
		return err
	}
	if err := validateCustodyLocks(s.Channels, s.CustodyLocks); err != nil {
		return err
	}
	if err := ValidateLockedCollateralForFinality(s); err != nil {
		return err
	}
	if err := validateClosedChannelTombstones(s.Channels, s.ClosedChannels); err != nil {
		return err
	}
	if err := validateConditionClaimRecords(s.Channels, s.ConditionClaims); err != nil {
		return err
	}
	if err := validateValidatorPaymentServices(s.ValidatorPaymentServices); err != nil {
		return err
	}
	if err := validateValidatorWatchRegistrations(s.ValidatorPaymentServices, s.ValidatorWatchRegistries); err != nil {
		return err
	}
	if err := s.FeeSchedule.Normalize().Validate(); err != nil {
		return err
	}
	if err := validatePaymentFeeMultipliers(s.FeeSchedule.Normalize(), s.FeeMultipliers); err != nil {
		return err
	}
	if err := validatePaymentFeeCharges(s.FeeCharges); err != nil {
		return err
	}
	if err := validatePaymentFeeRefunds(s.FeeCharges, s.FeeRefunds); err != nil {
		return err
	}
	if err := validateSecurityReserveAllocationHooks(s.Channels, s.SecurityReserveHooks); err != nil {
		return err
	}
	if err := validateSettlementInclusionLatencies(s.Channels, s.InclusionLatencies); err != nil {
		return err
	}
	if err := validateAsyncFinalizationJobs(s.Channels, s.AsyncFinalizationQueue); err != nil {
		return err
	}
	if err := validateAsyncPromiseExpiryJobs(s.Channels, s.AsyncPromiseExpiryQueue); err != nil {
		return err
	}
	if err := validateAsyncSettlementCompletions(s.Channels, s.AsyncFinalizationQueue, s.AsyncPromiseExpiryQueue, s.AsyncCompletions); err != nil {
		return err
	}
	return validatePaymentEvents(s.Channels, s.Events)
}

func (s PaymentsState) ChannelByID(channelID string) (ChannelRecord, bool) {
	_, channel, found := s.ChannelIndex(channelID)
	return channel, found
}

func (s PaymentsState) ChannelIndex(channelID string) (int, ChannelRecord, bool) {
	needle := normalizeHash(channelID)
	for i, channel := range s.Channels {
		channel = channel.Normalize()
		if channel.ChannelID == needle {
			return i, channel, true
		}
	}
	return 0, ChannelRecord{}, false
}

func (s PaymentsState) ValidatorPaymentServiceByValidator(validatorAddress string) (ValidatorPaymentServiceMetadata, bool) {
	validatorAddress = strings.TrimSpace(validatorAddress)
	for _, metadata := range s.ValidatorPaymentServices {
		metadata = metadata.Normalize()
		if metadata.ValidatorAddress == validatorAddress {
			return metadata, true
		}
	}
	return ValidatorPaymentServiceMetadata{}, false
}

func (s PaymentsState) ValidatorWatchRegistration(validatorAddress, delegator string) (ValidatorWatchRegistration, bool) {
	validatorAddress = strings.TrimSpace(validatorAddress)
	delegator = strings.TrimSpace(delegator)
	for _, registration := range s.ValidatorWatchRegistries {
		registration = registration.Normalize()
		if registration.ValidatorAddress == validatorAddress && registration.Delegator == delegator {
			return registration, true
		}
	}
	return ValidatorWatchRegistration{}, false
}

func (s PaymentsState) EdgeByKey(channelID, from, to string) (ChannelEdge, bool) {
	channelID = normalizeHash(channelID)
	for _, edge := range s.Edges {
		edge = edge.Normalize()
		if edge.ChannelID == channelID && edge.From == from && edge.To == to {
			return edge, true
		}
	}
	return ChannelEdge{}, false
}

func (s PaymentsState) VirtualChannelByID(id string) (VirtualChannel, bool) {
	_, vc, found := s.VirtualChannelIndex(id)
	return vc, found
}

func (s PaymentsState) VirtualChannelIndex(id string) (int, VirtualChannel, bool) {
	needle := normalizeHash(id)
	for i, vc := range s.VirtualChannels {
		vc = vc.Normalize()
		if vc.VirtualChannelID == needle {
			return i, vc, true
		}
	}
	return 0, VirtualChannel{}, false
}

func (s PaymentsState) StateHashDebug(channelID string) (StateHashDebug, error) {
	channel, found := s.Export().ChannelByID(channelID)
	if !found {
		return StateHashDebug{}, errors.New("payments channel not found")
	}
	debug := StateHashDebug{
		ChannelID:               channel.ChannelID,
		Status:                  channel.Status,
		LatestNonce:             channel.LatestState.Nonce,
		LatestStateHash:         channel.LatestState.StateHash,
		ComputedLatestStateHash: ComputeStateHash(channel.LatestState),
		FinalizedNonce:          channel.FinalizedNonce,
		DisputedNonce:           channel.DisputedNonce,
	}
	if channel.PendingClose.State.StateHash != "" {
		debug.PendingNonce = channel.PendingClose.State.Nonce
		debug.PendingStateHash = channel.PendingClose.State.StateHash
		debug.ComputedPendingStateHash = ComputeStateHash(channel.PendingClose.State)
	}
	return debug, nil
}

func (s PaymentsState) CustodyLockByChannel(channelID string) (CustodyLock, bool) {
	needle := normalizeHash(channelID)
	for _, lock := range s.CustodyLocks {
		lock = lock.Normalize()
		if lock.ChannelID == needle {
			return lock, true
		}
	}
	return CustodyLock{}, false
}

func (s PaymentsState) PendingFinalizationHeight(channelID string) (uint64, bool, error) {
	state := s.Export()
	channel, found := state.ChannelByID(channelID)
	if !found {
		return 0, false, errors.New("payments channel not found")
	}
	height, ok := PendingFinalizationHeightForChannel(channel)
	return height, ok, nil
}
