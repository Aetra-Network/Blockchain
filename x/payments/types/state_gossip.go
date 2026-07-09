package types

import (
	"errors"
	"strings"
)

func RegisterRoutingEdge(state PaymentsState, edge ChannelEdge) (PaymentsState, error) {
	state = state.Export()
	if err := state.Validate(); err != nil {
		return PaymentsState{}, err
	}
	edge = edge.Normalize()
	if err := edge.Validate(); err != nil {
		return PaymentsState{}, err
	}
	channel, found := state.ChannelByID(edge.ChannelID)
	if !found || channel.Status != ChannelStatusOpen {
		return PaymentsState{}, errors.New("payments routing edge requires open channel")
	}
	if !containsString(channel.Participants, edge.From) || !containsString(channel.Participants, edge.To) {
		return PaymentsState{}, errors.New("payments routing edge endpoints must be channel participants")
	}
	if _, found := state.EdgeByKey(edge.ChannelID, edge.From, edge.To); found {
		return PaymentsState{}, errors.New("payments routing edge already exists")
	}
	chargedState, _, err := ChargePaymentFee(state, PaymentFeeClassRoutingAdvertisement, channel, edge.From, edgeKey(edge), edge.AdvertisementFeePaid, channel.OpenHeight)
	if err != nil {
		return PaymentsState{}, err
	}
	next := chargedState.Clone()
	next.Edges = append(next.Edges, edge)
	sortEdges(next.Edges)
	return next, next.Validate()
}

func ApplyGossipEnvelope(store TopologyStore, state PaymentsState, envelope SignedGossipEnvelope, currentHeight uint64) (TopologyStore, error) {
	store = store.Normalize()
	state = state.Export()
	envelope = envelope.Normalize()
	if err := envelope.ValidateForState(state, currentHeight); err != nil {
		return PenalizeInvalidGossip(store, gossipPenaltyNode(envelope), currentHeight), err
	}
	message, err := BuildGossipMessage(envelope.Message)
	if err != nil {
		return PenalizeInvalidGossip(store, gossipPenaltyNode(envelope), currentHeight), err
	}
	envelope.Message = message
	envelope.MessageHash = message.MessageID
	if envelope.ReceivedAt == 0 {
		envelope.ReceivedAt = currentHeight
	}
	next := store
	next.Messages = upsertGossipEnvelope(next.Messages, envelope)
	if edge, ok := message.ToChannelEdge(); ok {
		next.Edges = upsertTopologyEdge(next.Edges, edge)
	}
	next.Reputation = addGossipReputation(next.Reputation, message.NodeID, message.ReputationDelta, false, currentHeight)
	return next.Normalize(), next.Validate()
}

func PenalizeInvalidGossip(store TopologyStore, nodeID string, currentHeight uint64) TopologyStore {
	store = store.Normalize()
	nodeID = strings.TrimSpace(nodeID)
	if currentHeight == 0 || nodeID == "" {
		return store
	}
	store.Reputation = addGossipReputation(store.Reputation, nodeID, -InvalidGossipPenalty, true, currentHeight)
	return store.Normalize()
}

func PruneTopologyStore(store TopologyStore, currentHeight uint64) (TopologyStore, error) {
	if currentHeight == 0 {
		return TopologyStore{}, errors.New("payments topology prune height must be positive")
	}
	store = store.Normalize()
	next := TopologyStore{
		Messages:     make([]SignedGossipEnvelope, 0, len(store.Messages)),
		Edges:        make([]ChannelEdge, 0, len(store.Edges)),
		Reputation:   append([]GossipReputation(nil), store.Reputation...),
		LastPrunedAt: currentHeight,
	}
	for _, envelope := range store.Messages {
		envelope = envelope.Normalize()
		if envelope.Message.ValidUntilHeight >= currentHeight {
			next.Messages = append(next.Messages, envelope)
		}
	}
	for _, edge := range store.Edges {
		edge = edge.Normalize()
		if edge.ExpiresHeight == 0 || edge.ExpiresHeight >= currentHeight {
			next.Edges = append(next.Edges, edge)
		}
	}
	next = next.Normalize()
	return next, next.Validate()
}

func RoutingScoreForEdge(store TopologyStore, edge ChannelEdge) int64 {
	store = store.Normalize()
	edge = edge.Normalize()
	score := int64(0)
	for _, reputation := range store.Reputation {
		reputation = reputation.Normalize()
		if reputation.NodeID == edge.From {
			score += reputation.Score
		}
	}
	return score
}

func gossipPenaltyNode(envelope SignedGossipEnvelope) string {
	envelope = envelope.Normalize()
	if envelope.Message.NodeID != "" {
		return envelope.Message.NodeID
	}
	if envelope.Signature.Signer != "" {
		return envelope.Signature.Signer
	}
	return envelope.ReceivedFrom
}

func upsertGossipEnvelope(envelopes []SignedGossipEnvelope, next SignedGossipEnvelope) []SignedGossipEnvelope {
	next = next.Normalize()
	messageID := next.Message.MessageID
	out := make([]SignedGossipEnvelope, 0, len(envelopes)+1)
	replaced := false
	for _, envelope := range envelopes {
		envelope = envelope.Normalize()
		if envelope.Message.MessageID == messageID {
			out = append(out, next)
			replaced = true
			continue
		}
		out = append(out, envelope)
	}
	if !replaced {
		out = append(out, next)
	}
	sortGossipEnvelopes(out)
	return out
}

func upsertTopologyEdge(edges []ChannelEdge, next ChannelEdge) []ChannelEdge {
	next = next.Normalize()
	nextKey := edgeKey(next)
	out := make([]ChannelEdge, 0, len(edges)+1)
	replaced := false
	for _, edge := range edges {
		edge = edge.Normalize()
		if edgeKey(edge) == nextKey {
			out = append(out, next)
			replaced = true
			continue
		}
		out = append(out, edge)
	}
	if !replaced {
		out = append(out, next)
	}
	sortEdges(out)
	return out
}

func addGossipReputation(reputation []GossipReputation, nodeID string, delta int64, invalid bool, height uint64) []GossipReputation {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" || height == 0 {
		return normalizeGossipReputation(reputation)
	}
	out := make([]GossipReputation, 0, len(reputation)+1)
	replaced := false
	for _, record := range reputation {
		record = record.Normalize()
		if record.NodeID == nodeID {
			record.Score += delta
			record.LastUpdateHeight = height
			if invalid {
				record.InvalidGossip++
			}
			out = append(out, record)
			replaced = true
			continue
		}
		out = append(out, record)
	}
	if !replaced {
		record := GossipReputation{NodeID: nodeID, Score: delta, LastUpdateHeight: height}
		if invalid {
			record.InvalidGossip = 1
		}
		out = append(out, record.Normalize())
	}
	return normalizeGossipReputation(out)
}
