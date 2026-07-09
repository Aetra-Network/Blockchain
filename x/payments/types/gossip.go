package types

import (
	"errors"
	"fmt"
	"strings"

	"github.com/sovereign-l1/l1/app/addressing"
)

type GossipMessageType string

const (
	GossipChannelAnnouncement GossipMessageType = "ChannelAnnouncement"
	GossipChannelUpdate       GossipMessageType = "ChannelUpdate"
	GossipLiquidityHint       GossipMessageType = "LiquidityHint"
	GossipFeePolicyUpdate     GossipMessageType = "FeePolicyUpdate"
	GossipNodeAnnouncement    GossipMessageType = "NodeAnnouncement"
	GossipRouteFailure        GossipMessageType = "RouteFailure"
	GossipCapacityProbe       GossipMessageType = "CapacityProbe"
)

type GossipSignature struct {
	Signer           string
	ChainID          string
	ObjectType       string
	Version          uint32
	ObjectID         string
	ExpirationHeight uint64
	CommitmentHash   string
	SignatureHash    string
}

type GossipMessage struct {
	MessageID         string
	MessageType       GossipMessageType
	ChainID           string
	ChannelID         string
	NodeID            string
	From              string
	To                string
	Capacity          string
	Liquidity         string
	FeeDenom          string
	FeeAmount         string
	MaxFee            string
	ValidAfterHeight  uint64
	ValidUntilHeight  uint64
	ChannelCommitment string
	FailureCode       string
	ProbeAmount       string
	ReputationDelta   int64
	Sequence          uint64
	Advisory          bool
}

type SignedGossipEnvelope struct {
	Message      GossipMessage
	MessageHash  string
	Signature    GossipSignature
	ReceivedFrom string
	ReceivedAt   uint64
}

type GossipReputation struct {
	NodeID           string
	Score            int64
	InvalidGossip    uint64
	LastUpdateHeight uint64
}

type TopologyStore struct {
	Messages     []SignedGossipEnvelope
	Edges        []ChannelEdge
	Reputation   []GossipReputation
	LastPrunedAt uint64
}

type EdgeRoutingStats struct {
	ChannelID              string
	From                   string
	To                     string
	SuccessRateBps         uint32
	LiquidityUpdatedHeight uint64
	CongestionBps          uint32
	NodeAvailabilityBps    uint32
	FailureCount           uint32
	TimeoutMargin          uint64
	PendingConditionCount  uint32
	AvgResolutionLatency   uint64
	RetryCount             uint32
	ReservePressureBps     uint32
	NodeQueueDelay         uint64
	LastFailureHeight      uint64
	LastUpdatedHeight      uint64
}

type ChannelEdge struct {
	ChannelID            string
	From                 string
	To                   string
	Capacity             string
	FeeDenom             string
	FeeAmount            string
	AdvertisementFeePaid string
	ExpiresHeight        uint64
	Active               bool
}

func (s GossipSignature) Normalize() GossipSignature {
	s.Signer = strings.TrimSpace(s.Signer)
	s.ChainID = strings.TrimSpace(s.ChainID)
	s.ObjectType = strings.TrimSpace(s.ObjectType)
	s.ObjectID = normalizeHash(s.ObjectID)
	s.CommitmentHash = normalizeHash(s.CommitmentHash)
	s.SignatureHash = normalizeHash(s.SignatureHash)
	return s
}

func (s GossipSignature) Validate(message GossipMessage) error {
	sig := s.Normalize()
	message = message.Normalize()
	if err := addressing.ValidateUserAddress("payments gossip signature signer", sig.Signer); err != nil {
		return err
	}
	if sig.ChainID != message.ChainID {
		return errors.New("payments gossip signature chain id mismatch")
	}
	if sig.ObjectType != SignatureObjectGossip {
		return errors.New("payments gossip signature object type mismatch")
	}
	if sig.Version != CurrentStateVersion {
		return errors.New("payments gossip signature version mismatch")
	}
	if sig.ObjectID != message.MessageID || sig.CommitmentHash != message.MessageID {
		return errors.New("payments gossip signature commitment mismatch")
	}
	if sig.ExpirationHeight != message.ValidUntilHeight {
		return errors.New("payments gossip signature expiration mismatch")
	}
	expected := ComputeSignatureEnvelopeHash(sig.Signer, sig.ChainID, "", sig.ObjectType, sig.Version, 0, sig.ObjectID, sig.ExpirationHeight, sig.CommitmentHash)
	if sig.SignatureHash != expected {
		return errors.New("payments gossip signature value mismatch")
	}
	return nil
}

func (m GossipMessage) Normalize() GossipMessage {
	m.MessageID = normalizeOptionalHash(m.MessageID)
	m.ChainID = strings.TrimSpace(m.ChainID)
	m.ChannelID = normalizeOptionalHash(m.ChannelID)
	m.NodeID = strings.TrimSpace(m.NodeID)
	m.From = strings.TrimSpace(m.From)
	m.To = strings.TrimSpace(m.To)
	m.Capacity = strings.TrimSpace(m.Capacity)
	m.Liquidity = strings.TrimSpace(m.Liquidity)
	m.FeeDenom = normalizeAssetDenom(m.FeeDenom)
	m.FeeAmount = strings.TrimSpace(m.FeeAmount)
	m.MaxFee = strings.TrimSpace(m.MaxFee)
	m.ChannelCommitment = normalizeOptionalHash(m.ChannelCommitment)
	m.FailureCode = strings.TrimSpace(m.FailureCode)
	m.ProbeAmount = strings.TrimSpace(m.ProbeAmount)
	if m.NodeID == "" {
		m.NodeID = m.From
	}
	if m.ValidUntilHeight == 0 && m.ValidAfterHeight > 0 {
		m.ValidUntilHeight = m.ValidAfterHeight + DefaultGossipTTL
	}
	return m
}

func (m GossipMessage) ValidateBasic() error {
	message := m.Normalize()
	if !IsGossipMessageType(message.MessageType) {
		return errors.New("payments gossip message type is invalid")
	}
	if strings.TrimSpace(message.ChainID) == "" {
		return errors.New("payments gossip chain id is required")
	}
	if message.ValidAfterHeight == 0 {
		return errors.New("payments gossip valid-after height must be positive")
	}
	if message.ValidUntilHeight <= message.ValidAfterHeight {
		return errors.New("payments gossip validity window must advance")
	}
	if err := addressing.ValidateUserAddress("payments gossip node", message.NodeID); err != nil {
		return err
	}
	switch message.MessageType {
	case GossipNodeAnnouncement:
		return nil
	case GossipRouteFailure:
		if message.FailureCode == "" {
			return errors.New("payments route failure code is required")
		}
		return validateGossipEdgeFields(message, false)
	case GossipCapacityProbe:
		if err := validatePositiveInt("payments capacity probe amount", message.ProbeAmount); err != nil {
			return err
		}
		return validateGossipEdgeFields(message, false)
	case GossipLiquidityHint:
		if err := validateNonNegativeInt("payments liquidity hint amount", message.Liquidity); err != nil {
			return err
		}
		return validateGossipEdgeFields(message, false)
	case GossipFeePolicyUpdate:
		if err := validateNonNegativeInt("payments fee policy max fee", message.MaxFee); err != nil {
			return err
		}
		return validateGossipEdgeFields(message, false)
	case GossipChannelAnnouncement, GossipChannelUpdate:
		return validateGossipEdgeFields(message, true)
	default:
		return errors.New("payments gossip message type is invalid")
	}
}

func (m GossipMessage) ToChannelEdge() (ChannelEdge, bool) {
	message := m.Normalize()
	switch message.MessageType {
	case GossipChannelAnnouncement, GossipChannelUpdate, GossipLiquidityHint, GossipFeePolicyUpdate:
		if message.ChannelID == "" {
			return ChannelEdge{}, false
		}
		capacity := message.Capacity
		if message.MessageType == GossipLiquidityHint && message.Liquidity != "" {
			capacity = message.Liquidity
		}
		if strings.TrimSpace(capacity) == "" {
			return ChannelEdge{}, false
		}
		edge := ChannelEdge{
			ChannelID:     message.ChannelID,
			From:          message.From,
			To:            message.To,
			Capacity:      capacity,
			FeeDenom:      message.FeeDenom,
			FeeAmount:     message.FeeAmount,
			ExpiresHeight: message.ValidUntilHeight,
			Active:        true,
		}.Normalize()
		return edge, true
	default:
		return ChannelEdge{}, false
	}
}

func (e SignedGossipEnvelope) Normalize() SignedGossipEnvelope {
	e.Message = e.Message.Normalize()
	e.MessageHash = normalizeOptionalHash(e.MessageHash)
	e.Signature = e.Signature.Normalize()
	e.ReceivedFrom = strings.TrimSpace(e.ReceivedFrom)
	if e.ReceivedFrom == "" {
		e.ReceivedFrom = e.Signature.Signer
	}
	return e
}

func (e SignedGossipEnvelope) ValidateForState(state PaymentsState, currentHeight uint64) error {
	envelope := e.Normalize()
	if currentHeight == 0 {
		return errors.New("payments gossip validation height must be positive")
	}
	message, err := BuildGossipMessage(envelope.Message)
	if err != nil {
		return err
	}
	if envelope.MessageHash != "" && envelope.MessageHash != message.MessageID {
		return errors.New("payments gossip message hash mismatch")
	}
	envelope.Message = message
	if currentHeight < message.ValidAfterHeight {
		return errors.New("payments gossip message is not yet valid")
	}
	if currentHeight > message.ValidUntilHeight {
		return errors.New("payments gossip message is expired")
	}
	if err := envelope.Signature.Validate(message); err != nil {
		return err
	}
	if envelope.Signature.Signer != message.NodeID {
		return errors.New("payments gossip signer must match advertising node")
	}
	if message.MessageType == GossipChannelAnnouncement {
		if message.ChannelID == "" && message.ChannelCommitment == "" {
			return errors.New("payments channel announcement requires channel id or commitment")
		}
		if message.ChannelID != "" {
			channel, found := state.ChannelByID(message.ChannelID)
			if !found || channel.Status != ChannelStatusOpen {
				return errors.New("payments channel announcement requires open channel")
			}
		}
	}
	return nil
}

func (r GossipReputation) Normalize() GossipReputation {
	r.NodeID = strings.TrimSpace(r.NodeID)
	return r
}

func (r GossipReputation) Validate() error {
	r = r.Normalize()
	if err := addressing.ValidateUserAddress("payments gossip reputation node", r.NodeID); err != nil {
		return err
	}
	if r.LastUpdateHeight == 0 {
		return errors.New("payments gossip reputation update height must be positive")
	}
	return nil
}

func (s TopologyStore) Normalize() TopologyStore {
	for i, envelope := range s.Messages {
		s.Messages[i] = envelope.Normalize()
	}
	for i, edge := range s.Edges {
		s.Edges[i] = edge.Normalize()
	}
	for i, reputation := range s.Reputation {
		s.Reputation[i] = reputation.Normalize()
	}
	sortGossipEnvelopes(s.Messages)
	sortEdges(s.Edges)
	sortGossipReputation(s.Reputation)
	return s
}

func (s TopologyStore) Validate() error {
	store := s.Normalize()
	seenMessages := make(map[string]struct{}, len(store.Messages))
	for _, envelope := range store.Messages {
		id := envelope.Message.MessageID
		if id == "" {
			id = envelope.MessageHash
		}
		if err := ValidateHash("payments gossip store message id", id); err != nil {
			return err
		}
		if _, found := seenMessages[id]; found {
			return errors.New("payments duplicate gossip message")
		}
		seenMessages[id] = struct{}{}
	}
	for _, edge := range store.Edges {
		if err := edge.Validate(); err != nil {
			return err
		}
	}
	seenReputation := make(map[string]struct{}, len(store.Reputation))
	for _, reputation := range store.Reputation {
		if err := reputation.Validate(); err != nil {
			return err
		}
		if _, found := seenReputation[reputation.NodeID]; found {
			return errors.New("payments duplicate gossip reputation")
		}
		seenReputation[reputation.NodeID] = struct{}{}
	}
	return nil
}

func (s EdgeRoutingStats) Normalize() EdgeRoutingStats {
	s.ChannelID = normalizeHash(s.ChannelID)
	s.From = strings.TrimSpace(s.From)
	s.To = strings.TrimSpace(s.To)
	return s
}

func (s EdgeRoutingStats) Validate() error {
	stats := s.Normalize()
	if err := ValidateHash("payments route stats channel id", stats.ChannelID); err != nil {
		return err
	}
	if err := addressing.ValidateUserAddress("payments route stats from", stats.From); err != nil {
		return err
	}
	if err := addressing.ValidateUserAddress("payments route stats to", stats.To); err != nil {
		return err
	}
	if stats.SuccessRateBps > 10_000 || stats.CongestionBps > 10_000 || stats.NodeAvailabilityBps > 10_000 || stats.ReservePressureBps > 10_000 {
		return errors.New("payments route stats bps must be <= 10000")
	}
	return nil
}

func BuildGossipMessage(message GossipMessage) (GossipMessage, error) {
	message = message.Normalize()
	if err := message.ValidateBasic(); err != nil {
		return GossipMessage{}, err
	}
	message.MessageID = ComputeGossipMessageHash(message)
	return message, nil
}

func SignatureForGossip(message GossipMessage, signer string) (GossipSignature, error) {
	message = message.Normalize()
	if message.MessageID == "" {
		var err error
		message, err = BuildGossipMessage(message)
		if err != nil {
			return GossipSignature{}, err
		}
	}
	signer = strings.TrimSpace(signer)
	if err := addressing.ValidateUserAddress("payments gossip signer", signer); err != nil {
		return GossipSignature{}, err
	}
	return GossipSignature{
		Signer:           signer,
		ChainID:          message.ChainID,
		ObjectType:       SignatureObjectGossip,
		Version:          CurrentStateVersion,
		ObjectID:         message.MessageID,
		ExpirationHeight: message.ValidUntilHeight,
		CommitmentHash:   message.MessageID,
		SignatureHash: ComputeSignatureEnvelopeHash(
			signer,
			message.ChainID,
			"",
			SignatureObjectGossip,
			CurrentStateVersion,
			0,
			message.MessageID,
			message.ValidUntilHeight,
			message.MessageID,
		),
	}, nil
}

func (e ChannelEdge) Normalize() ChannelEdge {
	e.ChannelID = normalizeHash(e.ChannelID)
	e.From = strings.TrimSpace(e.From)
	e.To = strings.TrimSpace(e.To)
	e.Capacity = strings.TrimSpace(e.Capacity)
	e.FeeDenom = normalizeAssetDenom(e.FeeDenom)
	e.FeeAmount = strings.TrimSpace(e.FeeAmount)
	e.AdvertisementFeePaid = strings.TrimSpace(e.AdvertisementFeePaid)
	if e.AdvertisementFeePaid == "" {
		e.AdvertisementFeePaid = "0"
	}
	return e
}

func (e ChannelEdge) Validate() error {
	e = e.Normalize()
	if err := ValidateHash("payments routing channel id", e.ChannelID); err != nil {
		return err
	}
	if err := addressing.ValidateUserAddress("payments routing from", e.From); err != nil {
		return err
	}
	if err := addressing.ValidateUserAddress("payments routing to", e.To); err != nil {
		return err
	}
	if e.From == e.To {
		return errors.New("payments routing edge endpoints must differ")
	}
	if err := validatePositiveInt("payments routing capacity", e.Capacity); err != nil {
		return err
	}
	if e.FeeDenom != NativeDenom {
		return fmt.Errorf("payments routing fee denom must be %s", NativeDenom)
	}
	if err := validateNonNegativeInt("payments routing fee", e.FeeAmount); err != nil {
		return err
	}
	return validateNonNegativeInt("payments routing advertisement fee", e.AdvertisementFeePaid)
}

func IsGossipMessageType(messageType GossipMessageType) bool {
	switch messageType {
	case GossipChannelAnnouncement,
		GossipChannelUpdate,
		GossipLiquidityHint,
		GossipFeePolicyUpdate,
		GossipNodeAnnouncement,
		GossipRouteFailure,
		GossipCapacityProbe:
		return true
	default:
		return false
	}
}

func ComputeGossipMessageHash(message GossipMessage) string {
	message = message.Normalize()
	parts := []string{
		"gossip-message",
		string(message.MessageType),
		message.ChainID,
		message.ChannelID,
		message.NodeID,
		message.From,
		message.To,
		message.Capacity,
		message.Liquidity,
		message.FeeDenom,
		message.FeeAmount,
		message.MaxFee,
		fmt.Sprintf("%020d", message.ValidAfterHeight),
		fmt.Sprintf("%020d", message.ValidUntilHeight),
		message.ChannelCommitment,
		message.FailureCode,
		message.ProbeAmount,
		fmt.Sprintf("%d", message.ReputationDelta),
		fmt.Sprintf("%020d", message.Sequence),
		fmt.Sprintf("%t", message.Advisory),
	}
	return HashParts(parts...)
}

func validateGossipEdgeFields(message GossipMessage, requireCapacity bool) error {
	message = message.Normalize()
	if message.ChannelID == "" && message.ChannelCommitment == "" {
		return errors.New("payments gossip edge requires channel id or commitment")
	}
	if message.ChannelID != "" {
		if err := ValidateHash("payments gossip channel id", message.ChannelID); err != nil {
			return err
		}
	}
	if err := addressing.ValidateUserAddress("payments gossip from", message.From); err != nil {
		return err
	}
	if err := addressing.ValidateUserAddress("payments gossip to", message.To); err != nil {
		return err
	}
	if message.From == message.To {
		return errors.New("payments gossip endpoints must differ")
	}
	if requireCapacity {
		if err := validatePositiveInt("payments gossip capacity", message.Capacity); err != nil {
			return err
		}
	} else if strings.TrimSpace(message.Capacity) != "" {
		if err := validateNonNegativeInt("payments gossip capacity", message.Capacity); err != nil {
			return err
		}
	}
	if message.FeeDenom != NativeDenom {
		return fmt.Errorf("payments gossip fee denom must be %s", NativeDenom)
	}
	return validateNonNegativeInt("payments gossip fee", message.FeeAmount)
}
