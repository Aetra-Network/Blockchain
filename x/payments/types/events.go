package types

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

type PaymentEventAttribute struct {
	Key   string
	Value string
}

type PaymentEvent struct {
	EventID    string
	EventType  string
	ChannelID  string
	Height     uint64
	Attributes []PaymentEventAttribute
}

func (e PaymentEventAttribute) Normalize() PaymentEventAttribute {
	e.Key = strings.TrimSpace(e.Key)
	e.Value = strings.TrimSpace(e.Value)
	return e
}

func (e PaymentEvent) Normalize() PaymentEvent {
	e.EventID = normalizeHash(e.EventID)
	e.EventType = strings.TrimSpace(e.EventType)
	e.ChannelID = normalizeHash(e.ChannelID)
	e.Attributes = normalizePaymentEventAttributes(e.Attributes)
	return e
}

func (e PaymentEvent) Validate() error {
	event := e.Normalize()
	if err := ValidateHash("payments event id", event.EventID); err != nil {
		return err
	}
	if event.EventType == "" {
		return errors.New("payments event type is required")
	}
	if err := ValidateHash("payments event channel id", event.ChannelID); err != nil {
		return err
	}
	if event.Height == 0 {
		return errors.New("payments event height must be positive")
	}
	seen := make(map[string]struct{}, len(event.Attributes))
	for _, attr := range event.Attributes {
		if attr.Key == "" {
			return errors.New("payments event attribute key is required")
		}
		if _, found := seen[attr.Key]; found {
			return errors.New("payments duplicate event attribute")
		}
		seen[attr.Key] = struct{}{}
	}
	return nil
}

func ChannelOpenEvent(channel ChannelRecord) PaymentEvent {
	channel = channel.Normalize()
	event := PaymentEvent{
		EventID:   HashParts("channel-open", channel.ChannelID, channel.OpeningStateHash),
		EventType: "channel-open",
		ChannelID: channel.ChannelID,
		Height:    channel.OpenHeight,
		Attributes: []PaymentEventAttribute{
			{Key: "channel_type", Value: string(channel.ChannelType)},
			{Key: "collateral", Value: channel.Collateral},
			{Key: "denom", Value: channel.Denom},
			{Key: "opening_fee", Value: channel.OpeningFeePaid},
			{Key: "routing_advertised", Value: fmt.Sprintf("%t", channel.RoutingAdvertised)},
			{Key: "conditional_payments", Value: fmt.Sprintf("%t", channel.ConditionalPayments)},
		},
	}
	return event.Normalize()
}

func ChannelDisputeEvent(channel ChannelRecord, submitter string, height uint64) PaymentEvent {
	channel = channel.Normalize()
	event := PaymentEvent{
		EventID:   HashParts("channel-dispute", channel.ChannelID, channel.PendingClose.State.StateHash, fmt.Sprintf("%d", height)),
		EventType: "channel-dispute",
		ChannelID: channel.ChannelID,
		Height:    height,
		Attributes: []PaymentEventAttribute{
			{Key: "submitter", Value: strings.TrimSpace(submitter)},
			{Key: "state_hash", Value: channel.PendingClose.State.StateHash},
			{Key: "nonce", Value: fmt.Sprintf("%d", channel.PendingClose.State.Nonce)},
			{Key: "settle_after_height", Value: fmt.Sprintf("%d", channel.PendingClose.SettleAfterHeight)},
		},
	}
	return event.Normalize()
}

func ValidatorAssistedDisputeEvent(metadata ValidatorPaymentServiceMetadata, channel ChannelRecord, delegator string, height uint64) PaymentEvent {
	metadata = metadata.Normalize()
	channel = channel.Normalize()
	event := PaymentEvent{
		EventID:   HashParts("validator-assisted-dispute", metadata.ValidatorAddress, channel.ChannelID, delegator, fmt.Sprintf("%d", height)),
		EventType: "validator-assisted-dispute",
		ChannelID: channel.ChannelID,
		Height:    height,
		Attributes: []PaymentEventAttribute{
			{Key: "validator", Value: metadata.ValidatorAddress},
			{Key: "watch_service", Value: metadata.ServiceAddress},
			{Key: "delegator", Value: strings.TrimSpace(delegator)},
			{Key: "metadata_hash", Value: metadata.MetadataHash},
		},
	}
	return event.Normalize()
}

func ChannelFinalityTransitionEvent(channel ChannelRecord, previous, next ChannelFinality, height uint64) PaymentEvent {
	channel = channel.Normalize()
	attrs := []PaymentEventAttribute{
		{Key: "from_finality", Value: string(previous)},
		{Key: "to_finality", Value: string(next)},
		{Key: "status", Value: string(channel.Status)},
	}
	if pendingHeight, ok := PendingFinalizationHeightForChannel(channel); ok {
		attrs = append(attrs, PaymentEventAttribute{Key: "pending_finalization_height", Value: fmt.Sprintf("%d", pendingHeight)})
	}
	event := PaymentEvent{
		EventID:    HashParts("channel-finality", channel.ChannelID, string(previous), string(next), fmt.Sprintf("%d", height)),
		EventType:  "channel-finality-transition",
		ChannelID:  channel.ChannelID,
		Height:     height,
		Attributes: attrs,
	}
	return event.Normalize()
}

func AsyncSettlementCompletionEvent(completion AsyncSettlementCompletion) PaymentEvent {
	completion = completion.Normalize()
	event := PaymentEvent{
		EventID:   HashParts("async-settlement-completion-event", completion.CompletionID),
		EventType: "async-settlement-completion",
		ChannelID: completion.ChannelID,
		Height:    completion.Height,
		Attributes: []PaymentEventAttribute{
			{Key: "job_id", Value: completion.JobID},
			{Key: "job_type", Value: completion.JobType},
			{Key: "object_id", Value: completion.ObjectID},
			{Key: "result_hash", Value: completion.ResultHash},
		},
	}
	return event.Normalize()
}

func normalizePaymentEventAttributes(attrs []PaymentEventAttribute) []PaymentEventAttribute {
	out := make([]PaymentEventAttribute, len(attrs))
	for i, attr := range attrs {
		out[i] = attr.Normalize()
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Key < out[j].Key
	})
	return out
}
