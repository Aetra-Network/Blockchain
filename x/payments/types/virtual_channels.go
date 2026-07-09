package types

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	sdkmath "cosmossdk.io/math"

	"github.com/sovereign-l1/l1/app/addressing"
)

type VirtualChannelStatus string

const (
	VirtualChannelStatusOpen    VirtualChannelStatus = "OPEN"
	VirtualChannelStatusSettled VirtualChannelStatus = "SETTLED"
)

type VirtualCloseMode string

const (
	VirtualCloseModeCooperative      VirtualCloseMode = "COOPERATIVE_ENDPOINT"
	VirtualCloseModeExpired          VirtualCloseMode = "EXPIRED"
	VirtualCloseModeIntermediaryRisk VirtualCloseMode = "INTERMEDIARY_RISK"
	VirtualCloseModeDisputed         VirtualCloseMode = "DISPUTED"
)

const (
	PaymentFeeClassChannelOpen                  PaymentFeeClass = "CHANNEL_OPEN"
	PaymentFeeClassChannelCheckpoint            PaymentFeeClass = "CHANNEL_CHECKPOINT"
	PaymentFeeClassCooperativeClose             PaymentFeeClass = "COOPERATIVE_CLOSE"
	PaymentFeeClassUnilateralClose              PaymentFeeClass = "UNILATERAL_CLOSE"
	PaymentFeeClassDispute                      PaymentFeeClass = "DISPUTE"
	PaymentFeeClassFraudProofVerification       PaymentFeeClass = "FRAUD_PROOF_VERIFICATION"
	PaymentFeeClassConditionalPromiseSettlement PaymentFeeClass = "CONDITIONAL_PROMISE_SETTLEMENT"
	PaymentFeeClassVirtualChannelAnchor         PaymentFeeClass = "VIRTUAL_CHANNEL_ANCHOR"
	PaymentFeeClassRoutingAdvertisement         PaymentFeeClass = "ROUTING_ADVERTISEMENT"
)

type StoreV2VirtualChannelRecord struct {
	Key              string
	Version          uint64
	VirtualChannelID string
	Channel          VirtualChannel
	AnchorHash       string
}

type VirtualChannel struct {
	VirtualChannelID         string
	ChainID                  string
	Nonce                    uint64
	ParentRouteID            string
	ParentChannelIDs         []string
	ParentReserveCommitments []string
	Endpoints                []string
	EndpointA                string
	EndpointB                string
	Intermediaries           []string
	IntermediarySetHash      string
	Capacity                 string
	BalanceA                 string
	BalanceB                 string
	RoutingFeeAmount         string
	AnchorFeePaid            string
	ExpiresHeight            uint64
	Status                   VirtualChannelStatus
	AnchorCommitment         string
	ConditionRoot            string
	StateHash                string
	Signatures               []StateSignature
}

type VirtualReservationSignature struct {
	Signer           string
	ChainID          string
	VirtualChannelID string
	ParentRouteID    string
	ParentChannelID  string
	ObjectType       string
	Version          uint32
	Capacity         string
	SplitAmount      string
	FeeAmount        string
	ExpirationHeight uint64
	CommitmentHash   string
	SignatureHash    string
}

type VirtualParentReserve struct {
	SegmentID         string
	ParentChannelID   string
	ReservedBy        string
	Capacity          string
	SplitAmount       string
	FeeAmount         string
	ReserveCommitment string
	Signature         VirtualReservationSignature
}

type VirtualActivationProof struct {
	VirtualChannel     VirtualChannel
	ParentReserves     []VirtualParentReserve
	RouteTimeoutHeight uint64
	AggregatedCapacity bool
	ProofHash          string
}

type VirtualReserveRelease struct {
	SegmentID         string
	VirtualChannelID  string
	ParentChannelID   string
	ReserveCommitment string
	Capacity          string
	BalanceA          string
	BalanceB          string
	FeeAmount         string
	ReleaseHeight     uint64
	ReleaseHash       string
}

type VirtualCloseProof struct {
	VirtualChannelID         string
	ParentRouteID            string
	CloseMode                VirtualCloseMode
	FinalState               VirtualChannel
	ParentReserveCommitments []string
	SubmittedBy              string
	CloseHeight              uint64
	ReleaseHeight            uint64
	ProofHash                string
}

type VirtualChannelDisputeProof struct {
	VirtualChannelID         string
	ParentRouteID            string
	LatestState              VirtualChannel
	ParentReserveCommitments []string
	SubmittedBy              string
	EvidenceHash             string
}

type VirtualReserveSegment struct {
	SegmentID         string
	VirtualChannelID  string
	ParentChannelID   string
	ReserveCommitment string
	Capacity          string
	BalanceA          string
	BalanceB          string
	FeeAmount         string
	SegmentHash       string
}

type VirtualSegmentSettlementProof struct {
	SegmentID         string
	VirtualChannelID  string
	ParentChannelID   string
	FinalStateHash    string
	ReserveCommitment string
	BalanceA          string
	BalanceB          string
	SettlementHash    string
}

type VirtualPartialActivationFailure struct {
	VirtualChannelID  string
	FailedSegmentID   string
	Reason            string
	RefundCommitments []string
	FailureHash       string
}

func BuildVirtualChannel(vc VirtualChannel) (VirtualChannel, error) {
	vc = vc.Normalize()
	if vc.ParentRouteID == "" {
		parts := append([]string{"virtual-parent-route", vc.VirtualChannelID}, vc.ParentChannelIDs...)
		vc.ParentRouteID = HashParts(parts...)
	}
	if vc.IntermediarySetHash == "" {
		vc.IntermediarySetHash = ComputeParticipantSetHash(vc.Intermediaries)
	}
	vc.AnchorCommitment = ""
	vc.StateHash = ""
	vc.AnchorCommitment = ComputeVirtualChannelAnchor(vc)
	vc.StateHash = ComputeVirtualChannelStateHash(vc)
	if err := vc.ValidateCore(); err != nil {
		return VirtualChannel{}, err
	}
	return vc.Normalize(), nil
}

func SignatureForVirtualChannel(vc VirtualChannel, signer string) (StateSignature, error) {
	vc = vc.Normalize()
	if vc.StateHash == "" {
		var err error
		vc, err = BuildVirtualChannel(vc)
		if err != nil {
			return StateSignature{}, err
		}
	}
	signer = strings.TrimSpace(signer)
	if err := addressing.ValidateUserAddress("payments virtual channel signer", signer); err != nil {
		return StateSignature{}, err
	}
	return StateSignature{
		Signer:           signer,
		ChainID:          vc.ChainID,
		ChannelID:        vc.VirtualChannelID,
		ObjectType:       SignatureObjectVirtual,
		Version:          CurrentStateVersion,
		Nonce:            vc.Nonce,
		ObjectID:         vc.StateHash,
		ExpirationHeight: vc.ExpiresHeight,
		CommitmentHash:   vc.StateHash,
		StateHash:        vc.StateHash,
		SignatureHash: ComputeSignatureEnvelopeHash(
			signer,
			vc.ChainID,
			vc.VirtualChannelID,
			SignatureObjectVirtual,
			CurrentStateVersion,
			vc.Nonce,
			vc.StateHash,
			vc.ExpiresHeight,
			vc.StateHash,
		),
	}, nil
}

func ValidateVirtualChannelSignature(sig StateSignature, vc VirtualChannel) error {
	sig = sig.Normalize()
	vc = vc.Normalize()
	if err := addressing.ValidateUserAddress("payments virtual channel signature signer", sig.Signer); err != nil {
		return err
	}
	if sig.ChainID != vc.ChainID {
		return errors.New("payments virtual channel signature chain id mismatch")
	}
	if sig.ChannelID != vc.VirtualChannelID {
		return errors.New("payments virtual channel signature channel id mismatch")
	}
	if sig.ObjectType != SignatureObjectVirtual {
		return errors.New("payments virtual channel signature object type mismatch")
	}
	if sig.Version != CurrentStateVersion {
		return errors.New("payments virtual channel signature version mismatch")
	}
	if sig.Nonce != vc.Nonce {
		return errors.New("payments virtual channel signature nonce mismatch")
	}
	if sig.ObjectID != vc.StateHash || sig.CommitmentHash != vc.StateHash || sig.StateHash != vc.StateHash {
		return errors.New("payments virtual channel signature commitment mismatch")
	}
	if sig.ExpirationHeight != vc.ExpiresHeight {
		return errors.New("payments virtual channel signature expiration mismatch")
	}
	if err := ValidateHash("payments virtual channel signature hash", sig.SignatureHash); err != nil {
		return err
	}
	expected := ComputeSignatureEnvelopeHash(sig.Signer, sig.ChainID, sig.ChannelID, sig.ObjectType, sig.Version, sig.Nonce, sig.ObjectID, sig.ExpirationHeight, sig.CommitmentHash)
	if sig.SignatureHash != expected {
		return errors.New("payments virtual channel signature hash mismatch")
	}
	return nil
}

func BuildVirtualParentReserve(vc VirtualChannel, reserve VirtualParentReserve, signer string) (VirtualParentReserve, error) {
	vc = vc.Normalize()
	reserve = reserve.Normalize()
	signer = strings.TrimSpace(signer)
	if reserve.ParentChannelID == "" {
		return VirtualParentReserve{}, errors.New("payments virtual reserve parent channel id is required")
	}
	if reserve.Capacity == "" {
		reserve.Capacity = vc.Capacity
	}
	if reserve.SplitAmount == "" {
		reserve.SplitAmount = reserve.Capacity
	}
	if reserve.FeeAmount == "" {
		reserve.FeeAmount = "0"
	}
	if reserve.SegmentID == "" {
		reserve.SegmentID = HashParts("virtual-reserve-segment", vc.VirtualChannelID, reserve.ParentChannelID, reserve.ReservedBy, reserve.SplitAmount)
	}
	if signer != "" {
		reserve.ReservedBy = signer
	}
	if reserve.ReservedBy == "" {
		return VirtualParentReserve{}, errors.New("payments virtual reserve signer is required")
	}
	reserve.ReserveCommitment = ComputeVirtualReserveCommitment(vc, reserve)
	sig, err := SignatureForVirtualReservation(vc, reserve, reserve.ReservedBy)
	if err != nil {
		return VirtualParentReserve{}, err
	}
	reserve.Signature = sig
	return reserve.Normalize(), nil
}

func ComputeVirtualReserveCommitment(vc VirtualChannel, reserve VirtualParentReserve) string {
	vc = vc.Normalize()
	reserve = reserve.Normalize()
	return HashParts(
		"virtual-reserve-commitment",
		vc.ChainID,
		vc.VirtualChannelID,
		vc.ParentRouteID,
		reserve.SegmentID,
		reserve.ParentChannelID,
		reserve.ReservedBy,
		reserve.Capacity,
		reserve.SplitAmount,
		reserve.FeeAmount,
		fmt.Sprintf("%020d", vc.ExpiresHeight),
	)
}

func SignatureForVirtualReservation(vc VirtualChannel, reserve VirtualParentReserve, signer string) (VirtualReservationSignature, error) {
	vc = vc.Normalize()
	reserve = reserve.Normalize()
	signer = strings.TrimSpace(signer)
	if err := addressing.ValidateUserAddress("payments virtual reservation signer", signer); err != nil {
		return VirtualReservationSignature{}, err
	}
	if reserve.ReserveCommitment == "" {
		reserve.ReservedBy = signer
		reserve.ReserveCommitment = ComputeVirtualReserveCommitment(vc, reserve)
	}
	return VirtualReservationSignature{
		Signer:           signer,
		ChainID:          vc.ChainID,
		VirtualChannelID: vc.VirtualChannelID,
		ParentRouteID:    vc.ParentRouteID,
		ParentChannelID:  reserve.ParentChannelID,
		ObjectType:       SignatureObjectVirtualReserve,
		Version:          CurrentStateVersion,
		Capacity:         reserve.Capacity,
		SplitAmount:      reserve.SplitAmount,
		FeeAmount:        reserve.FeeAmount,
		ExpirationHeight: vc.ExpiresHeight,
		CommitmentHash:   reserve.ReserveCommitment,
		SignatureHash: ComputeSignatureEnvelopeHash(
			signer,
			vc.ChainID,
			vc.VirtualChannelID,
			SignatureObjectVirtualReserve,
			CurrentStateVersion,
			vc.Nonce,
			reserve.ReserveCommitment,
			vc.ExpiresHeight,
			reserve.ReserveCommitment,
		),
	}, nil
}

func ValidateVirtualReservationSignature(sig VirtualReservationSignature, vc VirtualChannel, reserve VirtualParentReserve) error {
	sig = sig.Normalize()
	vc = vc.Normalize()
	reserve = reserve.Normalize()
	if err := addressing.ValidateUserAddress("payments virtual reservation signature signer", sig.Signer); err != nil {
		return err
	}
	if sig.ChainID != vc.ChainID {
		return errors.New("payments virtual reservation signature chain id mismatch")
	}
	if sig.VirtualChannelID != vc.VirtualChannelID {
		return errors.New("payments virtual reservation signature channel id mismatch")
	}
	if sig.ParentRouteID != vc.ParentRouteID {
		return errors.New("payments virtual reservation signature route id mismatch")
	}
	if sig.ParentChannelID != reserve.ParentChannelID {
		return errors.New("payments virtual reservation signature parent channel mismatch")
	}
	if sig.ObjectType != SignatureObjectVirtualReserve {
		return errors.New("payments virtual reservation signature object type mismatch")
	}
	if sig.Version != CurrentStateVersion {
		return errors.New("payments virtual reservation signature version mismatch")
	}
	if sig.Capacity != reserve.Capacity || sig.FeeAmount != reserve.FeeAmount {
		return errors.New("payments virtual reservation signature amount mismatch")
	}
	if sig.SplitAmount != reserve.SplitAmount {
		return errors.New("payments virtual reservation signature split amount mismatch")
	}
	if sig.ExpirationHeight != vc.ExpiresHeight {
		return errors.New("payments virtual reservation signature expiration mismatch")
	}
	if sig.CommitmentHash != reserve.ReserveCommitment {
		return errors.New("payments virtual reservation signature commitment mismatch")
	}
	expectedCommitment := ComputeVirtualReserveCommitment(vc, reserve)
	if reserve.ReserveCommitment != expectedCommitment {
		return errors.New("payments virtual reserve commitment mismatch")
	}
	expected := ComputeSignatureEnvelopeHash(sig.Signer, sig.ChainID, sig.VirtualChannelID, sig.ObjectType, sig.Version, vc.Nonce, sig.CommitmentHash, sig.ExpirationHeight, sig.CommitmentHash)
	if sig.SignatureHash != expected {
		return errors.New("payments virtual reservation signature hash mismatch")
	}
	return ValidateHash("payments virtual reservation signature hash", sig.SignatureHash)
}

func BuildVirtualActivationProof(vc VirtualChannel, reserves []VirtualParentReserve, routeTimeoutHeight uint64) (VirtualActivationProof, error) {
	vc = vc.Normalize()
	if vc.StateHash == "" || vc.AnchorCommitment == "" || vc.ParentRouteID == "" || vc.IntermediarySetHash == "" {
		built, err := BuildVirtualChannel(vc)
		if err != nil {
			return VirtualActivationProof{}, err
		}
		built.Signatures = vc.Signatures
		vc = built.Normalize()
	}
	proof := VirtualActivationProof{
		VirtualChannel:     vc,
		ParentReserves:     normalizeVirtualParentReserves(reserves),
		RouteTimeoutHeight: routeTimeoutHeight,
	}
	proof.ProofHash = ComputeVirtualActivationProofHash(proof)
	return proof.Normalize(), nil
}

func ComputeVirtualActivationProofHash(proof VirtualActivationProof) string {
	proof = proof.Normalize()
	parts := []string{
		"virtual-activation-proof",
		proof.VirtualChannel.ChainID,
		proof.VirtualChannel.VirtualChannelID,
		proof.VirtualChannel.StateHash,
		proof.VirtualChannel.ParentRouteID,
		fmt.Sprintf("%020d", proof.RouteTimeoutHeight),
		fmt.Sprintf("%t", proof.AggregatedCapacity),
	}
	for _, reserve := range proof.ParentReserves {
		parts = append(parts, reserve.SegmentID, reserve.ParentChannelID, reserve.ReservedBy, reserve.SplitAmount, reserve.ReserveCommitment)
	}
	return HashParts(parts...)
}

func ValidateVirtualActivationProof(proof VirtualActivationProof) error {
	proof = proof.Normalize()
	vc := proof.VirtualChannel
	if err := ValidateVirtualChannelActivation(vc); err != nil {
		return err
	}
	if proof.RouteTimeoutHeight == 0 {
		return errors.New("payments virtual activation proof route timeout is required")
	}
	if proof.RouteTimeoutHeight <= vc.ExpiresHeight {
		return errors.New("payments virtual activation proof route timeout must exceed virtual expiry")
	}
	if !proof.AggregatedCapacity && len(proof.ParentReserves) != len(vc.ParentChannelIDs) {
		return errors.New("payments virtual activation proof requires one reserve per parent")
	}
	if proof.AggregatedCapacity {
		if err := ValidateVirtualReserveSegments(vc, VirtualReserveSegmentsFromProof(proof)); err != nil {
			return err
		}
	}
	parentSet := make(map[string]struct{}, len(vc.ParentChannelIDs))
	for _, parentID := range vc.ParentChannelIDs {
		parentSet[parentID] = struct{}{}
	}
	seenParents := make(map[string]struct{}, len(proof.ParentReserves))
	coveredParents := make(map[string]struct{}, len(proof.ParentReserves))
	for _, reserve := range proof.ParentReserves {
		if _, found := parentSet[reserve.ParentChannelID]; !found {
			return errors.New("payments virtual activation proof reserve references unknown parent")
		}
		if !proof.AggregatedCapacity {
			if _, found := seenParents[reserve.ParentChannelID]; found {
				return errors.New("payments virtual activation proof duplicate parent reserve")
			}
			seenParents[reserve.ParentChannelID] = struct{}{}
		}
		if _, found := seenParents[reserve.SegmentID]; found {
			return errors.New("payments virtual activation proof duplicate parent reserve")
		}
		seenParents[reserve.SegmentID] = struct{}{}
		coveredParents[reserve.ParentChannelID] = struct{}{}
		if !containsString(vc.Intermediaries, reserve.ReservedBy) && !containsString(vc.Endpoints, reserve.ReservedBy) {
			return errors.New("payments virtual reserve signer must be route participant")
		}
		reserved, err := parsePositiveInt("payments virtual reserve capacity", reserve.Capacity)
		if err != nil {
			return err
		}
		capacity, err := parsePositiveInt("payments virtual capacity", vc.Capacity)
		if err != nil {
			return err
		}
		if reserved.LT(capacity) {
			if !proof.AggregatedCapacity {
				return errors.New("payments virtual reserve capacity below virtual capacity")
			}
			split, err := parsePositiveInt("payments virtual reserve split amount", reserve.SplitAmount)
			if err != nil {
				return err
			}
			if reserved.LT(split) {
				return errors.New("payments virtual reserve capacity below split amount")
			}
		}
		if err := ValidateVirtualReservationSignature(reserve.Signature, vc, reserve); err != nil {
			return err
		}
	}
	for parentID := range parentSet {
		if _, found := coveredParents[parentID]; !found {
			return errors.New("payments virtual activation proof missing parent reserve")
		}
	}
	expected := ComputeVirtualActivationProofHash(proof)
	if proof.ProofHash != expected {
		return errors.New("payments virtual activation proof hash mismatch")
	}
	return nil
}

func BuildVirtualCloseProof(final VirtualChannel, mode VirtualCloseMode, commitments []string, submittedBy string, closeHeight uint64) (VirtualCloseProof, error) {
	final = final.Normalize()
	if final.StateHash == "" || final.AnchorCommitment == "" {
		built, err := BuildVirtualChannel(final)
		if err != nil {
			return VirtualCloseProof{}, err
		}
		built.Signatures = final.Signatures
		final = built.Normalize()
	}
	proof := VirtualCloseProof{
		VirtualChannelID:         final.VirtualChannelID,
		ParentRouteID:            final.ParentRouteID,
		CloseMode:                mode,
		FinalState:               final,
		ParentReserveCommitments: normalizeHashSlice(commitments),
		SubmittedBy:              strings.TrimSpace(submittedBy),
		CloseHeight:              closeHeight,
		ReleaseHeight:            VirtualCloseReleaseHeight(mode, closeHeight, final.ExpiresHeight),
	}
	proof.ProofHash = ComputeVirtualCloseProofHash(proof)
	return proof.Normalize(), nil
}

func VirtualCloseReleaseHeight(mode VirtualCloseMode, closeHeight, expiresHeight uint64) uint64 {
	switch mode {
	case VirtualCloseModeCooperative, VirtualCloseModeExpired:
		return closeHeight
	case VirtualCloseModeIntermediaryRisk, VirtualCloseModeDisputed:
		return closeHeight + DefaultDisputePeriod
	default:
		return 0
	}
}

func ComputeVirtualCloseProofHash(proof VirtualCloseProof) string {
	proof = proof.Normalize()
	parts := []string{
		"virtual-close-proof",
		proof.VirtualChannelID,
		proof.ParentRouteID,
		string(proof.CloseMode),
		proof.FinalState.StateHash,
		proof.SubmittedBy,
		fmt.Sprintf("%020d", proof.CloseHeight),
		fmt.Sprintf("%020d", proof.ReleaseHeight),
	}
	parts = append(parts, proof.ParentReserveCommitments...)
	return HashParts(parts...)
}

func ValidateVirtualCloseProof(proof VirtualCloseProof, current VirtualChannel, currentHeight uint64) error {
	proof = proof.Normalize()
	current = current.Normalize()
	if currentHeight == 0 || proof.CloseHeight != currentHeight {
		return errors.New("payments virtual close proof height mismatch")
	}
	if !IsVirtualCloseMode(proof.CloseMode) {
		return fmt.Errorf("unknown payments virtual close mode %q", proof.CloseMode)
	}
	if proof.VirtualChannelID != current.VirtualChannelID || proof.FinalState.VirtualChannelID != current.VirtualChannelID {
		return errors.New("payments virtual close proof channel mismatch")
	}
	if proof.ParentRouteID != current.ParentRouteID || proof.FinalState.ParentRouteID != current.ParentRouteID {
		return errors.New("payments virtual close proof route mismatch")
	}
	if !containsString(current.Endpoints, proof.SubmittedBy) && !containsString(current.Intermediaries, proof.SubmittedBy) {
		return errors.New("payments virtual close submitter must be route participant")
	}
	if len(current.ParentReserveCommitments) > 0 && strings.Join(proof.ParentReserveCommitments, "/") != strings.Join(current.ParentReserveCommitments, "/") {
		return errors.New("payments virtual close reserve commitment mismatch")
	}
	if proof.ReleaseHeight != VirtualCloseReleaseHeight(proof.CloseMode, proof.CloseHeight, current.ExpiresHeight) {
		return errors.New("payments virtual close release height mismatch")
	}
	switch proof.CloseMode {
	case VirtualCloseModeCooperative:
		if proof.FinalState.Nonce < current.Nonce {
			return errors.New("payments virtual cooperative close state is stale")
		}
		if err := validateVirtualEndpointSignedState(current, proof.FinalState, false); err != nil {
			return err
		}
	case VirtualCloseModeExpired:
		if currentHeight < current.ExpiresHeight+DefaultDisputePeriod {
			return errors.New("payments virtual expired close before finalization")
		}
		if proof.FinalState.Nonce < current.Nonce {
			return errors.New("payments virtual expired close state is stale")
		}
		if err := validateVirtualEndpointSignedState(current, proof.FinalState, false); err != nil {
			return err
		}
	case VirtualCloseModeIntermediaryRisk:
		if !containsString(current.Intermediaries, proof.SubmittedBy) {
			return errors.New("payments virtual intermediary-risk close requires intermediary submitter")
		}
		if proof.FinalState.Nonce < current.Nonce {
			return errors.New("payments virtual intermediary-risk close state is stale")
		}
		if err := validateVirtualEndpointSignedState(current, proof.FinalState, false); err != nil {
			return err
		}
	case VirtualCloseModeDisputed:
		if proof.FinalState.Nonce < current.Nonce {
			return errors.New("payments virtual disputed close state is stale")
		}
		if err := validateVirtualEndpointSignedState(current, proof.FinalState, false); err != nil {
			return err
		}
	}
	expected := ComputeVirtualCloseProofHash(proof)
	if proof.ProofHash != expected {
		return errors.New("payments virtual close proof hash mismatch")
	}
	return nil
}

func VirtualReserveSegmentsFromProof(proof VirtualActivationProof) []VirtualReserveSegment {
	proof = proof.Normalize()
	out := make([]VirtualReserveSegment, 0, len(proof.ParentReserves))
	for _, reserve := range proof.ParentReserves {
		segment := VirtualReserveSegment{
			SegmentID:         reserve.SegmentID,
			VirtualChannelID:  proof.VirtualChannel.VirtualChannelID,
			ParentChannelID:   reserve.ParentChannelID,
			ReserveCommitment: reserve.ReserveCommitment,
			Capacity:          reserve.SplitAmount,
			BalanceA:          reserve.SplitAmount,
			BalanceB:          "0",
			FeeAmount:         reserve.FeeAmount,
		}
		segment.SegmentHash = ComputeVirtualReserveSegmentHash(segment)
		out = append(out, segment.Normalize())
	}
	return normalizeVirtualReserveSegments(out)
}

func ComputeVirtualReserveSegmentHash(segment VirtualReserveSegment) string {
	segment = segment.Normalize()
	return HashParts("virtual-reserve-segment", segment.SegmentID, segment.VirtualChannelID, segment.ParentChannelID, segment.ReserveCommitment, segment.Capacity, segment.BalanceA, segment.BalanceB, segment.FeeAmount)
}

func ValidateVirtualReserveSegments(vc VirtualChannel, segments []VirtualReserveSegment) error {
	vc = vc.Normalize()
	segments = normalizeVirtualReserveSegments(segments)
	if len(segments) == 0 {
		return errors.New("payments virtual reserve segments are required")
	}
	total := sdkmath.ZeroInt()
	seen := make(map[string]struct{}, len(segments))
	for _, segment := range segments {
		if err := segment.ValidateForVirtualChannel(vc); err != nil {
			return err
		}
		if _, found := seen[segment.SegmentID]; found {
			return errors.New("payments virtual reserve segments must be unique")
		}
		seen[segment.SegmentID] = struct{}{}
		capacity, err := parsePositiveInt("payments virtual reserve segment capacity", segment.Capacity)
		if err != nil {
			return err
		}
		total = total.Add(capacity)
	}
	capacity, err := parsePositiveInt("payments virtual capacity", vc.Capacity)
	if err != nil {
		return err
	}
	if !total.Equal(capacity) {
		return errors.New("payments virtual reserve segment split amount must equal capacity")
	}
	return nil
}

func BuildVirtualSegmentSettlementProofs(vc VirtualChannel, segments []VirtualReserveSegment) ([]VirtualSegmentSettlementProof, error) {
	vc = vc.Normalize()
	if err := ValidateVirtualReserveSegments(vc, segments); err != nil {
		return nil, err
	}
	segments = normalizeVirtualReserveSegments(segments)
	out := make([]VirtualSegmentSettlementProof, 0, len(segments))
	for _, segment := range segments {
		proof := VirtualSegmentSettlementProof{
			SegmentID:         segment.SegmentID,
			VirtualChannelID:  vc.VirtualChannelID,
			ParentChannelID:   segment.ParentChannelID,
			FinalStateHash:    vc.StateHash,
			ReserveCommitment: segment.ReserveCommitment,
			BalanceA:          segment.BalanceA,
			BalanceB:          segment.BalanceB,
		}
		proof.SettlementHash = ComputeVirtualSegmentSettlementHash(proof)
		out = append(out, proof.Normalize())
	}
	return normalizeVirtualSegmentSettlementProofs(out), nil
}

func ComputeVirtualSegmentSettlementHash(proof VirtualSegmentSettlementProof) string {
	proof = proof.Normalize()
	return HashParts("virtual-segment-settlement", proof.SegmentID, proof.VirtualChannelID, proof.ParentChannelID, proof.FinalStateHash, proof.ReserveCommitment, proof.BalanceA, proof.BalanceB)
}

func BuildVirtualPartialActivationFailure(vc VirtualChannel, failedSegmentID, reason string, refundCommitments []string) (VirtualPartialActivationFailure, error) {
	vc = vc.Normalize()
	failure := VirtualPartialActivationFailure{
		VirtualChannelID:  vc.VirtualChannelID,
		FailedSegmentID:   normalizeHash(failedSegmentID),
		Reason:            strings.TrimSpace(reason),
		RefundCommitments: normalizeHashSlice(refundCommitments),
	}
	failure.FailureHash = ComputeVirtualPartialActivationFailureHash(failure)
	if err := failure.ValidateForVirtualChannel(vc); err != nil {
		return VirtualPartialActivationFailure{}, err
	}
	return failure.Normalize(), nil
}

func ComputeVirtualPartialActivationFailureHash(failure VirtualPartialActivationFailure) string {
	failure = failure.Normalize()
	parts := []string{"virtual-partial-activation-failure", failure.VirtualChannelID, failure.FailedSegmentID, failure.Reason}
	parts = append(parts, failure.RefundCommitments...)
	return HashParts(parts...)
}

func BuildVirtualChannelDisputeProof(latest VirtualChannel, commitments []string, submittedBy string) (VirtualChannelDisputeProof, error) {
	latest = latest.Normalize()
	if latest.StateHash == "" || latest.AnchorCommitment == "" {
		built, err := BuildVirtualChannel(latest)
		if err != nil {
			return VirtualChannelDisputeProof{}, err
		}
		built.Signatures = latest.Signatures
		latest = built.Normalize()
	}
	proof := VirtualChannelDisputeProof{
		VirtualChannelID:         latest.VirtualChannelID,
		ParentRouteID:            latest.ParentRouteID,
		LatestState:              latest,
		ParentReserveCommitments: normalizeHashSlice(commitments),
		SubmittedBy:              strings.TrimSpace(submittedBy),
	}
	proof.EvidenceHash = ComputeVirtualDisputeEvidenceHash(proof)
	return proof.Normalize(), nil
}

func ComputeVirtualDisputeEvidenceHash(proof VirtualChannelDisputeProof) string {
	proof = proof.Normalize()
	parts := []string{
		"virtual-dispute-proof",
		proof.VirtualChannelID,
		proof.ParentRouteID,
		proof.LatestState.StateHash,
		proof.SubmittedBy,
	}
	parts = append(parts, proof.ParentReserveCommitments...)
	return HashParts(parts...)
}

func ValidateVirtualChannelDisputeProof(proof VirtualChannelDisputeProof, current VirtualChannel) error {
	proof = proof.Normalize()
	current = current.Normalize()
	if proof.VirtualChannelID != current.VirtualChannelID || proof.LatestState.VirtualChannelID != current.VirtualChannelID {
		return errors.New("payments virtual dispute proof channel mismatch")
	}
	if proof.ParentRouteID != current.ParentRouteID || proof.LatestState.ParentRouteID != current.ParentRouteID {
		return errors.New("payments virtual dispute proof route mismatch")
	}
	if !containsString(current.Endpoints, proof.SubmittedBy) && !containsString(current.Intermediaries, proof.SubmittedBy) {
		return errors.New("payments virtual dispute submitter must be route participant")
	}
	if proof.LatestState.Nonce <= current.Nonce {
		return errors.New("payments virtual dispute state nonce must be newer")
	}
	if len(proof.ParentReserveCommitments) != len(current.ParentChannelIDs) {
		return errors.New("payments virtual dispute proof requires parent reserve commitments")
	}
	if len(current.ParentReserveCommitments) > 0 && strings.Join(proof.ParentReserveCommitments, "/") != strings.Join(current.ParentReserveCommitments, "/") {
		return errors.New("payments virtual dispute proof reserve commitment mismatch")
	}
	if err := validateVirtualEndpointUpdate(current, proof.LatestState); err != nil {
		return err
	}
	expected := ComputeVirtualDisputeEvidenceHash(proof)
	if proof.EvidenceHash != expected {
		return errors.New("payments virtual dispute proof evidence hash mismatch")
	}
	return nil
}

func (v VirtualChannel) Normalize() VirtualChannel {
	v.VirtualChannelID = normalizeHash(v.VirtualChannelID)
	v.ChainID = strings.TrimSpace(v.ChainID)
	v.ParentRouteID = normalizeOptionalHash(v.ParentRouteID)
	for i := range v.ParentChannelIDs {
		v.ParentChannelIDs[i] = normalizeHash(v.ParentChannelIDs[i])
	}
	v.ParentReserveCommitments = normalizeHashSlice(v.ParentReserveCommitments)
	v.Endpoints = normalizeAddressSet(v.Endpoints)
	v.EndpointA = strings.TrimSpace(v.EndpointA)
	v.EndpointB = strings.TrimSpace(v.EndpointB)
	if len(v.Endpoints) == 2 {
		if v.EndpointA == "" {
			v.EndpointA = v.Endpoints[0]
		}
		if v.EndpointB == "" {
			v.EndpointB = v.Endpoints[1]
		}
	}
	if v.EndpointA != "" && v.EndpointB != "" {
		v.Endpoints = normalizeAddressSet([]string{v.EndpointA, v.EndpointB})
		v.EndpointA = v.Endpoints[0]
		v.EndpointB = v.Endpoints[1]
	}
	v.Intermediaries = normalizeAddressSet(v.Intermediaries)
	v.IntermediarySetHash = normalizeOptionalHash(v.IntermediarySetHash)
	v.Capacity = strings.TrimSpace(v.Capacity)
	v.BalanceA = strings.TrimSpace(v.BalanceA)
	v.BalanceB = strings.TrimSpace(v.BalanceB)
	v.RoutingFeeAmount = strings.TrimSpace(v.RoutingFeeAmount)
	v.AnchorFeePaid = strings.TrimSpace(v.AnchorFeePaid)
	if v.Nonce == 0 {
		v.Nonce = 1
	}
	if v.BalanceA == "" {
		v.BalanceA = v.Capacity
	}
	if v.BalanceB == "" {
		v.BalanceB = "0"
	}
	if v.RoutingFeeAmount == "" {
		v.RoutingFeeAmount = "0"
	}
	if v.AnchorFeePaid == "" {
		v.AnchorFeePaid = "0"
	}
	v.AnchorCommitment = normalizeOptionalHash(v.AnchorCommitment)
	v.ConditionRoot = normalizeOptionalHash(v.ConditionRoot)
	v.StateHash = normalizeOptionalHash(v.StateHash)
	v.Signatures = normalizeSignatures(v.Signatures)
	if v.Status == "" {
		v.Status = VirtualChannelStatusOpen
	}
	return v
}

func (v VirtualChannel) Validate() error {
	return v.ValidateCore()
}

func (v VirtualChannel) ValidateCore() error {
	vc := v.Normalize()
	if err := ValidateHash("payments virtual channel id", vc.VirtualChannelID); err != nil {
		return err
	}
	if strings.TrimSpace(vc.ChainID) == "" {
		return errors.New("payments virtual channel chain id is required")
	}
	if vc.Nonce == 0 {
		return errors.New("payments virtual channel nonce must be positive")
	}
	if vc.ParentRouteID != "" {
		if err := ValidateHash("payments virtual parent route id", vc.ParentRouteID); err != nil {
			return err
		}
	}
	if len(vc.ParentChannelIDs) == 0 || len(vc.ParentChannelIDs) > MaxParentChannels {
		return fmt.Errorf("payments virtual parent channels must be between 1 and %d", MaxParentChannels)
	}
	seen := make(map[string]struct{}, len(vc.ParentChannelIDs))
	for _, id := range vc.ParentChannelIDs {
		if err := ValidateHash("payments virtual parent channel id", id); err != nil {
			return err
		}
		if _, found := seen[id]; found {
			return errors.New("payments virtual parent channels must be unique")
		}
		seen[id] = struct{}{}
	}
	if len(vc.ParentReserveCommitments) > 0 {
		if len(vc.ParentReserveCommitments) != len(vc.ParentChannelIDs) {
			return errors.New("payments virtual parent reserve commitments must match parent channels")
		}
		seenCommitments := make(map[string]struct{}, len(vc.ParentReserveCommitments))
		for _, commitment := range vc.ParentReserveCommitments {
			if err := ValidateHash("payments virtual parent reserve commitment", commitment); err != nil {
				return err
			}
			if _, found := seenCommitments[commitment]; found {
				return errors.New("payments virtual parent reserve commitments must be unique")
			}
			seenCommitments[commitment] = struct{}{}
		}
	}
	if err := validateAddressSet("payments virtual endpoint", vc.Endpoints, 2, 2); err != nil {
		return err
	}
	if vc.EndpointA != vc.Endpoints[0] || vc.EndpointB != vc.Endpoints[1] {
		return errors.New("payments virtual endpoints must match endpoint fields")
	}
	if len(vc.Intermediaries) > MaxParticipants {
		return fmt.Errorf("payments virtual intermediaries must be <= %d", MaxParticipants)
	}
	if vc.IntermediarySetHash == "" {
		return errors.New("payments virtual intermediary set hash is required")
	}
	if expected := ComputeParticipantSetHash(vc.Intermediaries); vc.IntermediarySetHash != expected {
		return errors.New("payments virtual intermediary set hash mismatch")
	}
	if err := validatePositiveInt("payments virtual capacity", vc.Capacity); err != nil {
		return err
	}
	if err := validateNonNegativeInt("payments virtual balance a", vc.BalanceA); err != nil {
		return err
	}
	if err := validateNonNegativeInt("payments virtual balance b", vc.BalanceB); err != nil {
		return err
	}
	if err := validateNonNegativeInt("payments virtual routing fee", vc.RoutingFeeAmount); err != nil {
		return err
	}
	if err := validateNonNegativeInt("payments virtual anchor fee", vc.AnchorFeePaid); err != nil {
		return err
	}
	capacity, err := parsePositiveInt("payments virtual capacity", vc.Capacity)
	if err != nil {
		return err
	}
	balanceA, err := parseNonNegativeInt("payments virtual balance a", vc.BalanceA)
	if err != nil {
		return err
	}
	balanceB, err := parseNonNegativeInt("payments virtual balance b", vc.BalanceB)
	if err != nil {
		return err
	}
	if !balanceA.Add(balanceB).Equal(capacity) {
		return errors.New("payments virtual balances must equal capacity")
	}
	if vc.ExpiresHeight == 0 {
		return errors.New("payments virtual channel expiry height must be positive")
	}
	if !IsVirtualChannelStatus(vc.Status) {
		return fmt.Errorf("unknown payments virtual channel status %q", vc.Status)
	}
	if vc.AnchorCommitment == "" {
		return errors.New("payments virtual channel anchor is required")
	}
	if expected := ComputeVirtualChannelAnchor(vc); vc.AnchorCommitment != expected {
		return errors.New("payments virtual channel anchor mismatch")
	}
	if vc.ConditionRoot != "" {
		if err := ValidateHash("payments virtual condition root", vc.ConditionRoot); err != nil {
			return err
		}
	}
	if err := ValidateHash("payments virtual channel state hash", vc.StateHash); err != nil {
		return err
	}
	if expected := ComputeVirtualChannelStateHash(vc); vc.StateHash != expected {
		return errors.New("payments virtual channel state hash mismatch")
	}
	if err := validateVirtualChannelSignatures(vc); err != nil {
		return err
	}
	return nil
}

func validateVirtualChannelSignatures(vc VirtualChannel) error {
	vc = vc.Normalize()
	if len(vc.Signatures) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(vc.Signatures))
	for _, sig := range vc.Signatures {
		if err := ValidateVirtualChannelSignature(sig, vc); err != nil {
			return err
		}
		if _, found := seen[sig.Normalize().Signer]; found {
			return errors.New("payments duplicate virtual channel signature")
		}
		seen[sig.Normalize().Signer] = struct{}{}
	}
	return nil
}

func ValidateVirtualChannelActivation(vc VirtualChannel) error {
	vc = vc.Normalize()
	if err := vc.ValidateCore(); err != nil {
		return err
	}
	required := normalizeAddressSet(append(append([]string{}, vc.Endpoints...), vc.Intermediaries...))
	if len(required) == 0 {
		return errors.New("payments virtual channel activation requires signers")
	}
	seen := make(map[string]struct{}, len(vc.Signatures))
	for _, sig := range vc.Signatures {
		sig = sig.Normalize()
		if err := ValidateVirtualChannelSignature(sig, vc); err != nil {
			return err
		}
		seen[sig.Signer] = struct{}{}
	}
	for _, signer := range required {
		if _, found := seen[signer]; !found {
			return errors.New("payments virtual channel missing required signature")
		}
	}
	return nil
}

func (s VirtualReservationSignature) Normalize() VirtualReservationSignature {
	s.Signer = strings.TrimSpace(s.Signer)
	s.ChainID = strings.TrimSpace(s.ChainID)
	s.VirtualChannelID = normalizeHash(s.VirtualChannelID)
	s.ParentRouteID = normalizeHash(s.ParentRouteID)
	s.ParentChannelID = normalizeHash(s.ParentChannelID)
	s.Capacity = strings.TrimSpace(s.Capacity)
	s.SplitAmount = strings.TrimSpace(s.SplitAmount)
	s.FeeAmount = strings.TrimSpace(s.FeeAmount)
	s.CommitmentHash = normalizeHash(s.CommitmentHash)
	s.SignatureHash = normalizeHash(s.SignatureHash)
	return s
}

func (r VirtualParentReserve) Normalize() VirtualParentReserve {
	r.SegmentID = normalizeOptionalHash(r.SegmentID)
	r.ParentChannelID = normalizeHash(r.ParentChannelID)
	r.ReservedBy = strings.TrimSpace(r.ReservedBy)
	r.Capacity = strings.TrimSpace(r.Capacity)
	if r.SplitAmount == "" {
		r.SplitAmount = r.Capacity
	}
	r.SplitAmount = strings.TrimSpace(r.SplitAmount)
	if r.FeeAmount == "" {
		r.FeeAmount = "0"
	}
	r.FeeAmount = strings.TrimSpace(r.FeeAmount)
	r.ReserveCommitment = normalizeOptionalHash(r.ReserveCommitment)
	r.Signature = r.Signature.Normalize()
	return r
}

func (p VirtualActivationProof) Normalize() VirtualActivationProof {
	p.VirtualChannel = p.VirtualChannel.Normalize()
	p.ParentReserves = normalizeVirtualParentReserves(p.ParentReserves)
	p.ProofHash = normalizeOptionalHash(p.ProofHash)
	return p
}

func (p VirtualChannelDisputeProof) Normalize() VirtualChannelDisputeProof {
	p.VirtualChannelID = normalizeHash(p.VirtualChannelID)
	p.ParentRouteID = normalizeHash(p.ParentRouteID)
	p.LatestState = p.LatestState.Normalize()
	p.ParentReserveCommitments = normalizeHashSlice(p.ParentReserveCommitments)
	p.SubmittedBy = strings.TrimSpace(p.SubmittedBy)
	p.EvidenceHash = normalizeOptionalHash(p.EvidenceHash)
	return p
}

func (r VirtualReserveRelease) Normalize() VirtualReserveRelease {
	r.SegmentID = normalizeOptionalHash(r.SegmentID)
	r.VirtualChannelID = normalizeHash(r.VirtualChannelID)
	r.ParentChannelID = normalizeHash(r.ParentChannelID)
	r.ReserveCommitment = normalizeHash(r.ReserveCommitment)
	r.Capacity = strings.TrimSpace(r.Capacity)
	r.BalanceA = strings.TrimSpace(r.BalanceA)
	r.BalanceB = strings.TrimSpace(r.BalanceB)
	r.FeeAmount = strings.TrimSpace(r.FeeAmount)
	r.ReleaseHash = normalizeOptionalHash(r.ReleaseHash)
	return r
}

func (p VirtualCloseProof) Normalize() VirtualCloseProof {
	p.VirtualChannelID = normalizeHash(p.VirtualChannelID)
	p.ParentRouteID = normalizeHash(p.ParentRouteID)
	p.FinalState = p.FinalState.Normalize()
	p.ParentReserveCommitments = normalizeHashSlice(p.ParentReserveCommitments)
	p.SubmittedBy = strings.TrimSpace(p.SubmittedBy)
	p.ProofHash = normalizeOptionalHash(p.ProofHash)
	return p
}

func (s VirtualReserveSegment) Normalize() VirtualReserveSegment {
	s.SegmentID = normalizeHash(s.SegmentID)
	s.VirtualChannelID = normalizeHash(s.VirtualChannelID)
	s.ParentChannelID = normalizeHash(s.ParentChannelID)
	s.ReserveCommitment = normalizeHash(s.ReserveCommitment)
	s.Capacity = strings.TrimSpace(s.Capacity)
	s.BalanceA = strings.TrimSpace(s.BalanceA)
	s.BalanceB = strings.TrimSpace(s.BalanceB)
	s.FeeAmount = strings.TrimSpace(s.FeeAmount)
	s.SegmentHash = normalizeOptionalHash(s.SegmentHash)
	return s
}

func (s VirtualReserveSegment) ValidateForVirtualChannel(vc VirtualChannel) error {
	s = s.Normalize()
	vc = vc.Normalize()
	if err := ValidateHash("payments virtual reserve segment id", s.SegmentID); err != nil {
		return err
	}
	if s.VirtualChannelID != vc.VirtualChannelID {
		return errors.New("payments virtual reserve segment channel mismatch")
	}
	if !containsString(vc.ParentChannelIDs, s.ParentChannelID) {
		return errors.New("payments virtual reserve segment references unknown parent")
	}
	if err := ValidateHash("payments virtual reserve segment commitment", s.ReserveCommitment); err != nil {
		return err
	}
	capacity, err := parsePositiveInt("payments virtual reserve segment capacity", s.Capacity)
	if err != nil {
		return err
	}
	balanceA, err := parseNonNegativeInt("payments virtual reserve segment balance a", s.BalanceA)
	if err != nil {
		return err
	}
	balanceB, err := parseNonNegativeInt("payments virtual reserve segment balance b", s.BalanceB)
	if err != nil {
		return err
	}
	if !balanceA.Add(balanceB).Equal(capacity) {
		return errors.New("payments virtual reserve segment balances must equal capacity")
	}
	if err := validateNonNegativeInt("payments virtual reserve segment fee", s.FeeAmount); err != nil {
		return err
	}
	if expected := ComputeVirtualReserveSegmentHash(s); s.SegmentHash != expected {
		return errors.New("payments virtual reserve segment hash mismatch")
	}
	return nil
}

func (p VirtualSegmentSettlementProof) Normalize() VirtualSegmentSettlementProof {
	p.SegmentID = normalizeHash(p.SegmentID)
	p.VirtualChannelID = normalizeHash(p.VirtualChannelID)
	p.ParentChannelID = normalizeHash(p.ParentChannelID)
	p.FinalStateHash = normalizeHash(p.FinalStateHash)
	p.ReserveCommitment = normalizeHash(p.ReserveCommitment)
	p.BalanceA = strings.TrimSpace(p.BalanceA)
	p.BalanceB = strings.TrimSpace(p.BalanceB)
	p.SettlementHash = normalizeOptionalHash(p.SettlementHash)
	return p
}

func (p VirtualSegmentSettlementProof) ValidateForSegment(segment VirtualReserveSegment, vc VirtualChannel) error {
	p = p.Normalize()
	segment = segment.Normalize()
	vc = vc.Normalize()
	if p.SegmentID != segment.SegmentID || p.VirtualChannelID != vc.VirtualChannelID || p.ParentChannelID != segment.ParentChannelID {
		return errors.New("payments virtual segment settlement proof domain mismatch")
	}
	if p.FinalStateHash != vc.StateHash || p.ReserveCommitment != segment.ReserveCommitment {
		return errors.New("payments virtual segment settlement proof commitment mismatch")
	}
	if p.BalanceA != segment.BalanceA || p.BalanceB != segment.BalanceB {
		return errors.New("payments virtual segment settlement proof balance mismatch")
	}
	if expected := ComputeVirtualSegmentSettlementHash(p); p.SettlementHash != expected {
		return errors.New("payments virtual segment settlement proof hash mismatch")
	}
	return nil
}

func (f VirtualPartialActivationFailure) Normalize() VirtualPartialActivationFailure {
	f.VirtualChannelID = normalizeHash(f.VirtualChannelID)
	f.FailedSegmentID = normalizeHash(f.FailedSegmentID)
	f.Reason = strings.TrimSpace(f.Reason)
	f.RefundCommitments = normalizeHashSlice(f.RefundCommitments)
	f.FailureHash = normalizeOptionalHash(f.FailureHash)
	return f
}

func (f VirtualPartialActivationFailure) ValidateForVirtualChannel(vc VirtualChannel) error {
	f = f.Normalize()
	vc = vc.Normalize()
	if f.VirtualChannelID != vc.VirtualChannelID {
		return errors.New("payments virtual partial activation failure channel mismatch")
	}
	if err := ValidateHash("payments virtual failed segment id", f.FailedSegmentID); err != nil {
		return err
	}
	if f.Reason == "" {
		return errors.New("payments virtual partial activation failure reason is required")
	}
	if len(f.RefundCommitments) == 0 {
		return errors.New("payments virtual partial activation failure refund commitments are required")
	}
	if expected := ComputeVirtualPartialActivationFailureHash(f); f.FailureHash != expected {
		return errors.New("payments virtual partial activation failure hash mismatch")
	}
	return nil
}

func (r StoreV2VirtualChannelRecord) Normalize() StoreV2VirtualChannelRecord {
	r.Key = strings.TrimSpace(r.Key)
	if r.Version == 0 {
		r.Version = StoreV2MigrationVersion
	}
	r.VirtualChannelID = normalizeHash(r.VirtualChannelID)
	r.Channel = r.Channel.Normalize()
	r.AnchorHash = normalizeOptionalHash(r.AnchorHash)
	return r
}

func (r StoreV2VirtualChannelRecord) Validate() error {
	r = r.Normalize()
	if r.Version != StoreV2MigrationVersion {
		return errors.New("payments store v2 virtual channel record version mismatch")
	}
	if r.Key != StoreV2VirtualChannelKey(r.VirtualChannelID) {
		return errors.New("payments store v2 virtual channel key mismatch")
	}
	if r.Channel.VirtualChannelID != r.VirtualChannelID {
		return errors.New("payments store v2 virtual channel id mismatch")
	}
	return ValidateHash("payments store v2 virtual channel anchor", r.AnchorHash)
}

func IsVirtualChannelStatus(value VirtualChannelStatus) bool {
	switch value {
	case VirtualChannelStatusOpen, VirtualChannelStatusSettled:
		return true
	default:
		return false
	}
}

func IsVirtualCloseMode(value VirtualCloseMode) bool {
	switch value {
	case VirtualCloseModeCooperative,
		VirtualCloseModeExpired,
		VirtualCloseModeIntermediaryRisk,
		VirtualCloseModeDisputed:
		return true
	default:
		return false
	}
}

func normalizeVirtualParentReserves(reserves []VirtualParentReserve) []VirtualParentReserve {
	out := make([]VirtualParentReserve, len(reserves))
	for i, reserve := range reserves {
		out[i] = reserve.Normalize()
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SegmentID != out[j].SegmentID {
			return out[i].SegmentID < out[j].SegmentID
		}
		if out[i].ParentChannelID != out[j].ParentChannelID {
			return out[i].ParentChannelID < out[j].ParentChannelID
		}
		return out[i].ReservedBy < out[j].ReservedBy
	})
	return out
}

func normalizeVirtualReserveSegments(segments []VirtualReserveSegment) []VirtualReserveSegment {
	out := make([]VirtualReserveSegment, len(segments))
	for i, segment := range segments {
		out[i] = segment.Normalize()
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].SegmentID < out[j].SegmentID
	})
	return out
}

func normalizeVirtualSegmentSettlementProofs(proofs []VirtualSegmentSettlementProof) []VirtualSegmentSettlementProof {
	out := make([]VirtualSegmentSettlementProof, len(proofs))
	for i, proof := range proofs {
		out[i] = proof.Normalize()
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].SegmentID < out[j].SegmentID
	})
	return out
}
