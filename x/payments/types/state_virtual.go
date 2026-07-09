package types

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	sdkmath "cosmossdk.io/math"
)

func OpenVirtualChannel(state PaymentsState, vc VirtualChannel) (PaymentsState, error) {
	state = state.Export()
	if err := state.Validate(); err != nil {
		return PaymentsState{}, err
	}
	vc = vc.Normalize()
	if _, found := state.VirtualChannelByID(vc.VirtualChannelID); found {
		return PaymentsState{}, errors.New("payments virtual channel already exists")
	}
	capacity, err := parsePositiveInt("payments virtual capacity", vc.Capacity)
	if err != nil {
		return PaymentsState{}, err
	}
	var parentChainID string
	for _, parentID := range vc.ParentChannelIDs {
		channel, found := state.ChannelByID(parentID)
		if !found || channel.Status != ChannelStatusOpen {
			return PaymentsState{}, errors.New("payments virtual channel requires open parents")
		}
		if parentChainID == "" {
			parentChainID = channel.ChainID
		} else if parentChainID != channel.ChainID {
			return PaymentsState{}, errors.New("payments virtual channel parents must share chain id")
		}
		if !containsString(channel.Participants, vc.Endpoints[0]) && !containsString(channel.Participants, vc.Endpoints[1]) {
			return PaymentsState{}, errors.New("payments virtual channel parent path must touch an endpoint")
		}
		if reserved, err := parentReservedCapacity(channel); err != nil {
			return PaymentsState{}, err
		} else if reserved.LT(capacity) {
			return PaymentsState{}, errors.New("payments virtual channel capacity exceeds parent reserved capacity")
		}
		if err := validateVirtualParentTimeout(vc, channel, 0); err != nil {
			return PaymentsState{}, err
		}
	}
	if vc.ChainID == "" {
		vc.ChainID = parentChainID
		vc.AnchorCommitment = ""
		vc.StateHash = ""
	}
	if vc.ChainID != parentChainID {
		return PaymentsState{}, errors.New("payments virtual channel chain id mismatch")
	}
	if vc.AnchorCommitment == "" || vc.StateHash == "" || vc.ParentRouteID == "" || vc.IntermediarySetHash == "" {
		preservedSignatures := vc.Signatures
		vc.Signatures = nil
		built, err := BuildVirtualChannel(vc)
		if err != nil {
			return PaymentsState{}, err
		}
		vc = built
		vc.Signatures = preservedSignatures
	}
	if err := ValidateVirtualChannelActivation(vc); err != nil {
		return PaymentsState{}, err
	}
	chargedState, _, err := ChargePaymentFee(state, PaymentFeeClassVirtualChannelAnchor, feeChannelForVirtual(vc), vc.EndpointA, vc.VirtualChannelID, vc.AnchorFeePaid, vc.ExpiresHeight)
	if err != nil {
		return PaymentsState{}, err
	}
	next := chargedState.Clone()
	next.VirtualChannels = append(next.VirtualChannels, vc)
	sortVirtualChannels(next.VirtualChannels)
	return next, next.Validate()
}

func OpenVirtualChannelWithProof(state PaymentsState, proof VirtualActivationProof) (PaymentsState, error) {
	state = state.Export()
	if err := state.Validate(); err != nil {
		return PaymentsState{}, err
	}
	proof = proof.Normalize()
	vc := proof.VirtualChannel.Normalize()
	if _, found := state.VirtualChannelByID(vc.VirtualChannelID); found {
		return PaymentsState{}, errors.New("payments virtual channel already exists")
	}
	parentChainID, err := validateVirtualParentAccounting(state, vc, proof.RouteTimeoutHeight, proof.AggregatedCapacity, proof.ParentReserves)
	if err != nil {
		return PaymentsState{}, err
	}
	if vc.ChainID == "" {
		vc.ChainID = parentChainID
		vc.AnchorCommitment = ""
		vc.StateHash = ""
	}
	if vc.ChainID != parentChainID {
		return PaymentsState{}, errors.New("payments virtual channel chain id mismatch")
	}
	if vc.AnchorCommitment == "" || vc.StateHash == "" || vc.ParentRouteID == "" || vc.IntermediarySetHash == "" {
		preservedSignatures := vc.Signatures
		vc.Signatures = nil
		built, err := BuildVirtualChannel(vc)
		if err != nil {
			return PaymentsState{}, err
		}
		vc = built
		vc.Signatures = preservedSignatures
	}
	proof.VirtualChannel = vc.Normalize()
	if proof.ProofHash == "" {
		proof.ProofHash = ComputeVirtualActivationProofHash(proof)
	}
	if err := ValidateVirtualActivationProof(proof); err != nil {
		return PaymentsState{}, err
	}
	for _, reserve := range proof.ParentReserves {
		channel, _ := state.ChannelByID(reserve.ParentChannelID)
		parentReserved, err := parentReservedCapacity(channel)
		if err != nil {
			return PaymentsState{}, err
		}
		reserveCapacity, err := virtualReserveAccountingAmount(reserve, proof.AggregatedCapacity)
		if err != nil {
			return PaymentsState{}, err
		}
		if parentReserved.LT(reserveCapacity) {
			return PaymentsState{}, errors.New("payments virtual reserve exceeds parent reserved capacity")
		}
		if !containsString(channel.Participants, reserve.ReservedBy) {
			return PaymentsState{}, errors.New("payments virtual reserve signer must be parent participant")
		}
	}
	proof.VirtualChannel.ParentReserveCommitments = virtualActivationReserveCommitments(proof)
	chargedState, _, err := ChargePaymentFee(state, PaymentFeeClassVirtualChannelAnchor, feeChannelForVirtual(proof.VirtualChannel), proof.VirtualChannel.EndpointA, proof.VirtualChannel.VirtualChannelID, proof.VirtualChannel.AnchorFeePaid, proof.RouteTimeoutHeight)
	if err != nil {
		return PaymentsState{}, err
	}
	next := chargedState.Clone()
	next.VirtualChannels = append(next.VirtualChannels, proof.VirtualChannel)
	sortVirtualChannels(next.VirtualChannels)
	return next, next.Validate()
}

func CloseVirtualChannelWithProof(state PaymentsState, proof VirtualCloseProof, currentHeight uint64) (PaymentsState, VirtualChannel, []VirtualReserveRelease, error) {
	state = state.Export()
	if err := state.Validate(); err != nil {
		return PaymentsState{}, VirtualChannel{}, nil, err
	}
	if currentHeight == 0 {
		return PaymentsState{}, VirtualChannel{}, nil, errors.New("payments virtual close height must be positive")
	}
	index, current, found := state.VirtualChannelIndex(proof.VirtualChannelID)
	if !found {
		return PaymentsState{}, VirtualChannel{}, nil, errors.New("payments virtual channel not found")
	}
	finalState, err := buildVirtualUpdateForCurrent(current, proof.FinalState)
	if err != nil {
		return PaymentsState{}, VirtualChannel{}, nil, err
	}
	proof.FinalState = finalState
	if proof.ProofHash == "" {
		proof.ProofHash = ComputeVirtualCloseProofHash(proof)
	}
	if err := ValidateVirtualCloseProof(proof, current, currentHeight); err != nil {
		return PaymentsState{}, VirtualChannel{}, nil, err
	}
	closed := finalState.Normalize()
	closed.Status = VirtualChannelStatusSettled
	releases, err := virtualReserveReleasesFromClose(proof, current)
	if err != nil {
		return PaymentsState{}, VirtualChannel{}, nil, err
	}
	next := state.Clone()
	next.VirtualChannels = append(next.VirtualChannels[:index], next.VirtualChannels[index+1:]...)
	sortVirtualChannels(next.VirtualChannels)
	return next, closed.Normalize(), releases, next.Validate()
}

func AcceptVirtualChannelUpdate(state PaymentsState, nextVC VirtualChannel, currentHeight uint64) (PaymentsState, error) {
	state = state.Export()
	if err := state.Validate(); err != nil {
		return PaymentsState{}, err
	}
	if currentHeight == 0 {
		return PaymentsState{}, errors.New("payments virtual update height must be positive")
	}
	index, current, found := state.VirtualChannelIndex(nextVC.VirtualChannelID)
	if !found {
		return PaymentsState{}, errors.New("payments virtual channel not found")
	}
	if current.Status != VirtualChannelStatusOpen {
		return PaymentsState{}, errors.New("payments virtual channel is not open")
	}
	if currentHeight >= current.ExpiresHeight {
		return PaymentsState{}, errors.New("payments virtual channel update expired")
	}
	nextVC, err := buildVirtualUpdateForCurrent(current, nextVC)
	if err != nil {
		return PaymentsState{}, err
	}
	if err := validateVirtualEndpointUpdate(current, nextVC); err != nil {
		return PaymentsState{}, err
	}
	next := state.Clone()
	next.VirtualChannels[index] = nextVC
	sortVirtualChannels(next.VirtualChannels)
	return next, next.Validate()
}

func SubmitVirtualChannelDispute(state PaymentsState, proof VirtualChannelDisputeProof, currentHeight uint64) (PaymentsState, error) {
	state = state.Export()
	if err := state.Validate(); err != nil {
		return PaymentsState{}, err
	}
	if currentHeight == 0 {
		return PaymentsState{}, errors.New("payments virtual dispute height must be positive")
	}
	index, current, found := state.VirtualChannelIndex(proof.VirtualChannelID)
	if !found {
		return PaymentsState{}, errors.New("payments virtual channel not found")
	}
	if currentHeight > current.ExpiresHeight {
		return PaymentsState{}, errors.New("payments virtual dispute expired")
	}
	latest, err := buildVirtualUpdateForCurrent(current, proof.LatestState)
	if err != nil {
		return PaymentsState{}, err
	}
	proof.LatestState = latest
	if proof.EvidenceHash == "" {
		proof.EvidenceHash = ComputeVirtualDisputeEvidenceHash(proof)
	}
	if err := ValidateVirtualChannelDisputeProof(proof, current); err != nil {
		return PaymentsState{}, err
	}
	next := state.Clone()
	next.VirtualChannels[index] = latest
	sortVirtualChannels(next.VirtualChannels)
	return next, next.Validate()
}

func CloseVirtualChannel(state PaymentsState, virtualChannelID string, currentHeight uint64) (PaymentsState, VirtualChannel, error) {
	state = state.Export()
	if err := state.Validate(); err != nil {
		return PaymentsState{}, VirtualChannel{}, err
	}
	if currentHeight == 0 {
		return PaymentsState{}, VirtualChannel{}, errors.New("payments virtual close height must be positive")
	}
	virtualChannelID = normalizeHash(virtualChannelID)
	index := -1
	var closed VirtualChannel
	for i, vc := range state.VirtualChannels {
		vc = vc.Normalize()
		if vc.VirtualChannelID == virtualChannelID {
			index = i
			closed = vc
			break
		}
	}
	if index < 0 {
		return PaymentsState{}, VirtualChannel{}, errors.New("payments virtual channel not found")
	}
	closed.Status = VirtualChannelStatusSettled
	next := state.Clone()
	next.VirtualChannels = append(next.VirtualChannels[:index], next.VirtualChannels[index+1:]...)
	sortVirtualChannels(next.VirtualChannels)
	return next, closed.Normalize(), next.Validate()
}

func validateVirtualChannels(channels []ChannelRecord, virtualChannels []VirtualChannel) error {
	channelByID := channelMap(channels)
	seen := make(map[string]struct{}, len(virtualChannels))
	var previous string
	for i, vc := range virtualChannels {
		vc = vc.Normalize()
		if err := vc.Validate(); err != nil {
			return err
		}
		for _, parentID := range vc.ParentChannelIDs {
			if _, found := channelByID[parentID]; !found {
				return errors.New("payments virtual channel references unknown parent")
			}
		}
		if _, found := seen[vc.VirtualChannelID]; found {
			return errors.New("payments duplicate virtual channel")
		}
		seen[vc.VirtualChannelID] = struct{}{}
		if i > 0 && previous >= vc.VirtualChannelID {
			return errors.New("payments virtual channels must be sorted canonically")
		}
		previous = vc.VirtualChannelID
	}
	return nil
}

func parentReservedCapacity(channel ChannelRecord) (sdkmath.Int, error) {
	channel = channel.Normalize()
	reserveA, err := parseNonNegativeInt("payments virtual parent reserve a", channel.LatestState.ReserveA)
	if err != nil {
		return sdkmath.ZeroInt(), err
	}
	reserveB, err := parseNonNegativeInt("payments virtual parent reserve b", channel.LatestState.ReserveB)
	if err != nil {
		return sdkmath.ZeroInt(), err
	}
	return reserveA.Add(reserveB), nil
}

func validateVirtualParentAccounting(state PaymentsState, vc VirtualChannel, routeTimeoutHeight uint64, aggregated bool, reserves []VirtualParentReserve) (string, error) {
	vc = vc.Normalize()
	capacity, err := parsePositiveInt("payments virtual capacity", vc.Capacity)
	if err != nil {
		return "", err
	}
	requiredByParent := map[string]sdkmath.Int{}
	if aggregated {
		for _, reserve := range normalizeVirtualParentReserves(reserves) {
			amount, err := virtualReserveAccountingAmount(reserve, true)
			if err != nil {
				return "", err
			}
			current := requiredByParent[reserve.ParentChannelID]
			if current.IsNil() {
				current = sdkmath.ZeroInt()
			}
			requiredByParent[reserve.ParentChannelID] = current.Add(amount)
		}
	}
	var parentChainID string
	for _, parentID := range vc.ParentChannelIDs {
		channel, found := state.ChannelByID(parentID)
		if !found || channel.Status != ChannelStatusOpen {
			return "", errors.New("payments virtual channel requires open parents")
		}
		if parentChainID == "" {
			parentChainID = channel.ChainID
		} else if parentChainID != channel.ChainID {
			return "", errors.New("payments virtual channel parents must share chain id")
		}
		if !containsString(channel.Participants, vc.Endpoints[0]) && !containsString(channel.Participants, vc.Endpoints[1]) && !sharesAny(channel.Participants, vc.Intermediaries) {
			return "", errors.New("payments virtual channel parent path must touch route participants")
		}
		reserved, err := parentReservedCapacity(channel)
		if err != nil {
			return "", err
		}
		required := capacity
		if aggregated {
			required = requiredByParent[parentID]
			if required.IsNil() || !required.IsPositive() {
				return "", errors.New("payments virtual aggregated reserve missing parent split")
			}
		}
		if reserved.LT(required) {
			return "", errors.New("payments virtual channel capacity exceeds parent reserved capacity")
		}
		if err := validateVirtualParentTimeout(vc, channel, routeTimeoutHeight); err != nil {
			return "", err
		}
	}
	return parentChainID, nil
}

func validateVirtualParentTimeout(vc VirtualChannel, channel ChannelRecord, routeTimeoutHeight uint64) error {
	vc = vc.Normalize()
	channel = channel.Normalize()
	parentSafetyHeight := channel.LatestState.TimeoutHeight
	if parentSafetyHeight == 0 {
		parentSafetyHeight = channel.OpenHeight + channel.CloseDelay + channel.DisputePeriod
	}
	safetyExpiry := vc.ExpiresHeight + channel.CloseDelay + channel.DisputePeriod
	if safetyExpiry < vc.ExpiresHeight || safetyExpiry >= parentSafetyHeight {
		return errors.New("payments virtual channel expiry must be earlier than parent safety timeout")
	}
	if routeTimeoutHeight > 0 {
		if routeTimeoutHeight > parentSafetyHeight {
			return errors.New("payments virtual route timeout exceeds parent safety timeout")
		}
		if safetyExpiry >= routeTimeoutHeight {
			return errors.New("payments virtual channel expiry must be earlier than route timeout")
		}
	}
	return nil
}

func virtualReserveAccountingAmount(reserve VirtualParentReserve, aggregated bool) (sdkmath.Int, error) {
	reserve = reserve.Normalize()
	if aggregated {
		return parsePositiveInt("payments virtual reserve split amount", reserve.SplitAmount)
	}
	return parsePositiveInt("payments virtual reserve capacity", reserve.Capacity)
}

func buildVirtualUpdateForCurrent(current VirtualChannel, nextVC VirtualChannel) (VirtualChannel, error) {
	current = current.Normalize()
	nextVC = nextVC.Normalize()
	nextVC.ChainID = current.ChainID
	nextVC.ParentRouteID = current.ParentRouteID
	nextVC.ParentChannelIDs = append([]string(nil), current.ParentChannelIDs...)
	nextVC.ParentReserveCommitments = append([]string(nil), current.ParentReserveCommitments...)
	nextVC.Endpoints = append([]string(nil), current.Endpoints...)
	nextVC.EndpointA = current.EndpointA
	nextVC.EndpointB = current.EndpointB
	nextVC.Intermediaries = append([]string(nil), current.Intermediaries...)
	nextVC.IntermediarySetHash = current.IntermediarySetHash
	nextVC.Capacity = current.Capacity
	nextVC.RoutingFeeAmount = current.RoutingFeeAmount
	nextVC.ExpiresHeight = current.ExpiresHeight
	nextVC.Status = current.Status
	nextVC.AnchorCommitment = ""
	nextVC.StateHash = ""
	preservedSignatures := nextVC.Signatures
	nextVC.Signatures = nil
	built, err := BuildVirtualChannel(nextVC)
	if err != nil {
		return VirtualChannel{}, err
	}
	built.Signatures = preservedSignatures
	return built.Normalize(), nil
}

func validateVirtualEndpointUpdate(current VirtualChannel, nextVC VirtualChannel) error {
	if nextVC.Normalize().Nonce <= current.Normalize().Nonce {
		return errors.New("payments virtual update nonce must strictly increase")
	}
	return validateVirtualEndpointSignedState(current, nextVC, true)
}

func validateVirtualEndpointSignedState(current VirtualChannel, nextVC VirtualChannel, requireNewer bool) error {
	current = current.Normalize()
	nextVC = nextVC.Normalize()
	if nextVC.VirtualChannelID != current.VirtualChannelID {
		return errors.New("payments virtual update channel mismatch")
	}
	if nextVC.ChainID != current.ChainID || nextVC.ParentRouteID != current.ParentRouteID {
		return errors.New("payments virtual update domain mismatch")
	}
	if strings.Join(nextVC.ParentChannelIDs, "/") != strings.Join(current.ParentChannelIDs, "/") {
		return errors.New("payments virtual update parent channel mismatch")
	}
	if strings.Join(nextVC.ParentReserveCommitments, "/") != strings.Join(current.ParentReserveCommitments, "/") {
		return errors.New("payments virtual update reserve commitment mismatch")
	}
	if strings.Join(nextVC.Endpoints, "/") != strings.Join(current.Endpoints, "/") || strings.Join(nextVC.Intermediaries, "/") != strings.Join(current.Intermediaries, "/") {
		return errors.New("payments virtual update route participant mismatch")
	}
	if nextVC.Capacity != current.Capacity || nextVC.RoutingFeeAmount != current.RoutingFeeAmount || nextVC.ExpiresHeight != current.ExpiresHeight {
		return errors.New("payments virtual update immutable field mismatch")
	}
	if requireNewer && nextVC.Nonce <= current.Nonce {
		return errors.New("payments virtual update nonce must strictly increase")
	}
	if !requireNewer && nextVC.Nonce < current.Nonce {
		return errors.New("payments virtual signed state nonce is stale")
	}
	if err := nextVC.ValidateCore(); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(nextVC.Signatures))
	for _, sig := range nextVC.Signatures {
		sig = sig.Normalize()
		if err := ValidateVirtualChannelSignature(sig, nextVC); err != nil {
			return err
		}
		if !containsString(nextVC.Endpoints, sig.Signer) {
			return errors.New("payments virtual update requires endpoint signatures only")
		}
		seen[sig.Signer] = struct{}{}
	}
	for _, endpoint := range nextVC.Endpoints {
		if _, found := seen[endpoint]; !found {
			return errors.New("payments virtual update missing endpoint signature")
		}
	}
	return nil
}

func virtualReserveReleasesFromClose(proof VirtualCloseProof, current VirtualChannel) ([]VirtualReserveRelease, error) {
	proof = proof.Normalize()
	current = current.Normalize()
	commitments := proof.ParentReserveCommitments
	if len(commitments) == 0 {
		commitments = current.ParentReserveCommitments
	}
	if len(commitments) == 0 {
		for _, parentID := range current.ParentChannelIDs {
			commitments = append(commitments, HashParts("virtual-reserve-release", current.VirtualChannelID, parentID))
		}
	}
	capacity, err := parsePositiveInt("payments virtual capacity", current.Capacity)
	if err != nil {
		return nil, err
	}
	releases := make([]VirtualReserveRelease, 0, len(commitments))
	for i, commitment := range commitments {
		parentID := ""
		if i < len(current.ParentChannelIDs) {
			parentID = current.ParentChannelIDs[i]
		} else {
			parentID = current.ParentChannelIDs[len(current.ParentChannelIDs)-1]
		}
		amount := current.Capacity
		if len(commitments) > len(current.ParentChannelIDs) {
			share := capacity.QuoRaw(int64(len(commitments)))
			amount = share.String()
		}
		release := VirtualReserveRelease{
			SegmentID:         HashParts("virtual-release-segment", current.VirtualChannelID, commitment),
			VirtualChannelID:  current.VirtualChannelID,
			ParentChannelID:   parentID,
			ReserveCommitment: commitment,
			Capacity:          amount,
			BalanceA:          proof.FinalState.BalanceA,
			BalanceB:          proof.FinalState.BalanceB,
			FeeAmount:         current.RoutingFeeAmount,
			ReleaseHeight:     proof.ReleaseHeight,
		}
		release.ReleaseHash = HashParts("virtual-reserve-release", release.SegmentID, release.VirtualChannelID, release.ParentChannelID, release.ReserveCommitment, release.Capacity, release.BalanceA, release.BalanceB, fmt.Sprintf("%020d", release.ReleaseHeight))
		releases = append(releases, release.Normalize())
	}
	sort.SliceStable(releases, func(i, j int) bool {
		return releases[i].SegmentID < releases[j].SegmentID
	})
	return releases, nil
}

func virtualActivationReserveCommitments(proof VirtualActivationProof) []string {
	proof = proof.Normalize()
	out := make([]string, 0, len(proof.ParentReserves))
	for _, reserve := range proof.ParentReserves {
		out = append(out, reserve.ReserveCommitment)
	}
	return normalizeHashSlice(out)
}
