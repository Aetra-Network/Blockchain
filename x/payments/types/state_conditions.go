package types

import (
	"errors"
	"fmt"
	"sort"
)

func RevealPromisePreimage(state PaymentsState, req PreimageRevealRequest) (PaymentsState, []ConditionResolution, error) {
	state = state.Export()
	req = req.Normalize()
	channel, found := state.ChannelByID(req.ChannelID)
	if !found {
		return PaymentsState{}, nil, errors.New("payments channel not found")
	}
	if err := req.ValidateForChannel(channel, state.ConditionClaims); err != nil {
		return PaymentsState{}, nil, err
	}
	preimageHash := HashParts(req.Preimage)
	resolutions := make([]ConditionResolution, 0, len(req.Promises))
	next := state.Clone()
	for _, promise := range normalizeConditionalPromises(req.Promises) {
		evidenceHash := HashParts("promise-preimage", promise.PromiseID, preimageHash)
		resolution := ConditionResolution{
			ConditionID:  promise.PromiseID,
			Resolver:     req.Revealer,
			Recipient:    promise.Destination,
			Amount:       promise.Amount,
			Expired:      false,
			EvidenceHash: evidenceHash,
		}.Normalize()
		resolutions = append(resolutions, resolution)
		next.ConditionClaims = append(next.ConditionClaims, ConditionClaimRecord{
			ChainID:        channel.ChainID,
			ChannelID:      channel.ChannelID,
			ConditionID:    promise.PromiseID,
			EvidenceHash:   evidenceHash,
			PreimageHash:   preimageHash,
			ResolvedHeight: req.CurrentHeight,
			ExpiresHeight:  req.CurrentHeight + DefaultReplayHorizon,
		}.Normalize())
	}
	sortConditionClaimRecords(next.ConditionClaims)
	return next, normalizeConditionResolutions(resolutions), next.Validate()
}

func ExpireConditionalPromises(state PaymentsState, req PromiseExpiryRequest) (PaymentsState, []ConditionResolution, ConditionRootUpdate, error) {
	state = state.Export()
	req = req.Normalize()
	channel, found := state.ChannelByID(req.ChannelID)
	if !found {
		return PaymentsState{}, nil, ConditionRootUpdate{}, errors.New("payments channel not found")
	}
	if err := req.ValidateForChannel(channel, state.ConditionClaims); err != nil {
		return PaymentsState{}, nil, ConditionRootUpdate{}, err
	}
	_, update, err := BuildConditionRootAfterExpiry(channel.LatestState, req.Promises)
	if err != nil {
		return PaymentsState{}, nil, ConditionRootUpdate{}, err
	}
	resolutions := make([]ConditionResolution, 0, len(req.Promises))
	next := state.Clone()
	for _, promise := range normalizeConditionalPromises(req.Promises) {
		evidenceHash := HashParts("promise-expiry", promise.PromiseID, fmt.Sprintf("%020d", req.CurrentHeight))
		resolution := ConditionResolution{
			ConditionID:  promise.PromiseID,
			Resolver:     req.Resolver,
			Recipient:    promise.Source,
			Amount:       promise.Amount,
			Expired:      true,
			EvidenceHash: evidenceHash,
		}.Normalize()
		resolutions = append(resolutions, resolution)
		next.ConditionClaims = append(next.ConditionClaims, ConditionClaimRecord{
			ChainID:        channel.ChainID,
			ChannelID:      channel.ChannelID,
			ConditionID:    promise.PromiseID,
			EvidenceHash:   evidenceHash,
			ResolvedHeight: req.CurrentHeight,
			ExpiresHeight:  req.CurrentHeight + DefaultReplayHorizon,
		}.Normalize())
	}
	sortConditionClaimRecords(next.ConditionClaims)
	return next, normalizeConditionResolutions(resolutions), update, next.Validate()
}

func BatchSettleLinkedPromises(state PaymentsState, req BatchConditionSettlementRequest) (PaymentsState, BatchConditionSettlementResult, error) {
	state = state.Export()
	req = req.Normalize()
	if err := req.ValidateForState(state, state.ConditionClaims); err != nil {
		return PaymentsState{}, BatchConditionSettlementResult{}, err
	}
	proof := req.LinkageProof.Normalize()
	evidenceHash := proof.EvidenceHash
	if evidenceHash == "" {
		evidenceHash = HashParts("batch-condition-settlement", proof.RouteID, string(req.Mode), fmt.Sprintf("%020d", req.CurrentHeight))
	}
	preimageHash := ""
	if req.Mode == ConditionSettlementModePreimage {
		preimageHash = HashParts(req.Preimage)
	}
	updates, err := conditionRootUpdatesForPromises(state, proof.Promises)
	if err != nil {
		return PaymentsState{}, BatchConditionSettlementResult{}, err
	}
	feeChannel, found := state.ChannelByID(proof.Promises[0].ChannelID)
	if !found {
		return PaymentsState{}, BatchConditionSettlementResult{}, errors.New("payments condition fee channel not found")
	}
	chargedState, _, err := ChargePaymentFee(state, PaymentFeeClassConditionalPromiseSettlement, feeChannel, req.Resolver, evidenceHash, req.SettlementFeePaid, req.CurrentHeight)
	if err != nil {
		return PaymentsState{}, BatchConditionSettlementResult{}, err
	}
	next := chargedState.Clone()
	resolutions := make([]ConditionResolution, 0, len(proof.Promises))
	feeClaims := make([]RouteFeeClaim, 0, len(proof.Promises)-1)
	for i, promise := range proof.Promises {
		channel, _ := state.ChannelByID(promise.ChannelID)
		resolution := ConditionResolution{
			ConditionID:  promise.PromiseID,
			Resolver:     req.Resolver,
			Recipient:    promise.Destination,
			Amount:       promise.Amount,
			Expired:      false,
			EvidenceHash: HashParts("batch-condition-resolution", evidenceHash, promise.PromiseID),
		}
		if req.Mode == ConditionSettlementModeExpiry {
			resolution.Recipient = promise.Source
			resolution.Expired = true
		}
		resolution = resolution.Normalize()
		resolutions = append(resolutions, resolution)
		next.ConditionClaims = append(next.ConditionClaims, ConditionClaimRecord{
			ChainID:        channel.ChainID,
			ChannelID:      channel.ChannelID,
			ConditionID:    promise.PromiseID,
			EvidenceHash:   resolution.EvidenceHash,
			PreimageHash:   preimageHash,
			ResolvedHeight: req.CurrentHeight,
			ExpiresHeight:  req.CurrentHeight + DefaultReplayHorizon,
		}.Normalize())
		if req.Mode != ConditionSettlementModePreimage || i == 0 {
			continue
		}
		fee, err := parseNonNegativeInt("payments route fee claim amount", promise.Fee)
		if err != nil {
			return PaymentsState{}, BatchConditionSettlementResult{}, err
		}
		if fee.IsZero() {
			continue
		}
		feeClaims = append(feeClaims, RouteFeeClaim{
			ChannelID:    promise.ChannelID,
			PromiseID:    promise.PromiseID,
			Recipient:    promise.Source,
			Amount:       promise.Fee,
			EvidenceHash: HashParts("route-fee-claim", evidenceHash, promise.PromiseID, promise.Source),
		}.Normalize())
	}
	sortConditionClaimRecords(next.ConditionClaims)
	result := BatchConditionSettlementResult{
		RouteID:              proof.RouteID,
		Resolutions:          resolutions,
		FeeClaims:            feeClaims,
		ConditionRootUpdates: updates,
		EvidenceHash:         evidenceHash,
	}.Normalize()
	if err := result.Validate(); err != nil {
		return PaymentsState{}, BatchConditionSettlementResult{}, err
	}
	return next, result, next.Validate()
}

func rejectReusedConditionClaims(state PaymentsState, channel ChannelRecord, resolutions []ConditionResolution) error {
	channel = channel.Normalize()
	for _, resolution := range normalizeConditionResolutions(resolutions) {
		conditionKey := conditionClaimKey(channel.ChannelID, resolution.ConditionID)
		evidenceKey := conditionEvidenceKey(channel.ChannelID, resolution.EvidenceHash)
		for _, existing := range state.ConditionClaims {
			existing = existing.Normalize()
			if existing.ChainID != channel.ChainID || existing.ChannelID != channel.ChannelID {
				continue
			}
			if conditionClaimKey(existing.ChannelID, existing.ConditionID) == conditionKey {
				return errors.New("payments condition claim has already been used")
			}
			if conditionEvidenceKey(existing.ChannelID, existing.EvidenceHash) == evidenceKey {
				return errors.New("payments condition evidence claim has already been used")
			}
		}
	}
	return nil
}

func conditionRootUpdatesForPromises(state PaymentsState, promises []ConditionalPromise) ([]ConditionRootUpdate, error) {
	grouped := make(map[string][]ConditionalPromise)
	for _, promise := range normalizeConditionalPromises(promises) {
		grouped[promise.ChannelID] = append(grouped[promise.ChannelID], promise)
	}
	channelIDs := make([]string, 0, len(grouped))
	for channelID := range grouped {
		channelIDs = append(channelIDs, channelID)
	}
	sort.Strings(channelIDs)
	updates := make([]ConditionRootUpdate, 0, len(channelIDs))
	for _, channelID := range channelIDs {
		channel, found := state.ChannelByID(channelID)
		if !found {
			return nil, errors.New("payments condition root update channel not found")
		}
		if len(channel.LatestState.Conditions) == 0 {
			continue
		}
		_, update, err := BuildConditionRootAfterExpiry(channel.LatestState, grouped[channelID])
		if err != nil {
			return nil, err
		}
		updates = append(updates, update)
	}
	return normalizeConditionRootUpdates(updates), nil
}

func mergeConditionResolutions(left, right []ConditionResolution) []ConditionResolution {
	byID := make(map[string]ConditionResolution, len(left)+len(right))
	for _, resolution := range normalizeConditionResolutions(left) {
		byID[resolution.ConditionID] = resolution
	}
	for _, resolution := range normalizeConditionResolutions(right) {
		byID[resolution.ConditionID] = resolution
	}
	out := make([]ConditionResolution, 0, len(byID))
	for _, resolution := range byID {
		out = append(out, resolution)
	}
	return normalizeConditionResolutions(out)
}

func conditionClaimKey(channelID, conditionID string) string {
	return normalizeHash(channelID) + "/" + normalizeHash(conditionID)
}

func conditionEvidenceKey(channelID, evidenceHash string) string {
	return normalizeHash(channelID) + "/" + normalizeHash(evidenceHash)
}
