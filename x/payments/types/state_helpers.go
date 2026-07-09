package types

import (
	"fmt"
	"sort"
)

func sharesAny(left, right []string) bool {
	for _, value := range left {
		if containsString(right, value) {
			return true
		}
	}
	return false
}

func uint32Max(left, right uint32) uint32 {
	if left > right {
		return left
	}
	return right
}

func uint32Min(left, right uint32) uint32 {
	if left < right {
		return left
	}
	return right
}

func sortRouteQueue(queue []routeSearchPath) {
	sort.SliceStable(queue, func(i, j int) bool {
		if !queue[i].cost.Equal(queue[j].cost) {
			return queue[i].cost.LT(queue[j].cost)
		}
		return routePathKey(queue[i].edges) < routePathKey(queue[j].edges)
	})
}

func channelMap(channels []ChannelRecord) map[string]ChannelRecord {
	out := make(map[string]ChannelRecord, len(channels))
	for _, channel := range channels {
		channel = channel.Normalize()
		out[channel.ChannelID] = channel
	}
	return out
}

func sortChannels(channels []ChannelRecord) {
	sort.SliceStable(channels, func(i, j int) bool {
		return channels[i].Normalize().ChannelID < channels[j].Normalize().ChannelID
	})
}

func sortEdges(edges []ChannelEdge) {
	sort.SliceStable(edges, func(i, j int) bool {
		return edgeKey(edges[i].Normalize()) < edgeKey(edges[j].Normalize())
	})
}

func sortGossipEnvelopes(envelopes []SignedGossipEnvelope) {
	sort.SliceStable(envelopes, func(i, j int) bool {
		left := envelopes[i].Normalize()
		right := envelopes[j].Normalize()
		if left.Message.MessageID == right.Message.MessageID {
			return left.ReceivedAt < right.ReceivedAt
		}
		return left.Message.MessageID < right.Message.MessageID
	})
}

func sortGossipReputation(reputation []GossipReputation) {
	sort.SliceStable(reputation, func(i, j int) bool {
		return reputation[i].Normalize().NodeID < reputation[j].Normalize().NodeID
	})
}

func sortForwardingReplayRecords(records []ForwardingPacketReplayRecord) {
	sort.SliceStable(records, func(i, j int) bool {
		left := records[i].Normalize()
		right := records[j].Normalize()
		if left.RouteID == right.RouteID {
			return left.PacketID < right.PacketID
		}
		return left.RouteID < right.RouteID
	})
}

func normalizeForwardingReplayRecords(records []ForwardingPacketReplayRecord) []ForwardingPacketReplayRecord {
	out := make([]ForwardingPacketReplayRecord, len(records))
	for i, record := range records {
		out[i] = record.Normalize()
	}
	sortForwardingReplayRecords(out)
	return out
}

func normalizeGossipReputation(reputation []GossipReputation) []GossipReputation {
	out := make([]GossipReputation, len(reputation))
	for i, record := range reputation {
		out[i] = record.Normalize()
	}
	sortGossipReputation(out)
	return out
}

func sortVirtualChannels(channels []VirtualChannel) {
	sort.SliceStable(channels, func(i, j int) bool {
		return channels[i].Normalize().VirtualChannelID < channels[j].Normalize().VirtualChannelID
	})
}

func sortSettlements(settlements []SettlementRecord) {
	sort.SliceStable(settlements, func(i, j int) bool {
		return settlements[i].Normalize().ChannelID < settlements[j].Normalize().ChannelID
	})
}

func sortBatches(batches []SettlementBatch) {
	sort.SliceStable(batches, func(i, j int) bool {
		return batches[i].Normalize().BatchID < batches[j].Normalize().BatchID
	})
}

func sortCustodyLocks(locks []CustodyLock) {
	sort.SliceStable(locks, func(i, j int) bool {
		return locks[i].Normalize().ChannelID < locks[j].Normalize().ChannelID
	})
}

func sortClosedChannelTombstones(tombstones []ClosedChannelTombstone) {
	sort.SliceStable(tombstones, func(i, j int) bool {
		return tombstones[i].Normalize().ChannelID < tombstones[j].Normalize().ChannelID
	})
}

func sortConditionClaimRecords(claims []ConditionClaimRecord) {
	sort.SliceStable(claims, func(i, j int) bool {
		left := claims[i].Normalize()
		right := claims[j].Normalize()
		return conditionClaimKey(left.ChannelID, left.ConditionID)+"/"+left.EvidenceHash < conditionClaimKey(right.ChannelID, right.ConditionID)+"/"+right.EvidenceHash
	})
}

func sortValidatorPaymentServices(services []ValidatorPaymentServiceMetadata) {
	sort.SliceStable(services, func(i, j int) bool {
		return services[i].Normalize().ValidatorAddress < services[j].Normalize().ValidatorAddress
	})
}

func sortValidatorWatchRegistrations(registrations []ValidatorWatchRegistration) {
	sort.SliceStable(registrations, func(i, j int) bool {
		left := registrations[i].Normalize()
		right := registrations[j].Normalize()
		return validatorWatchRegistrationKey(left.ValidatorAddress, left.Delegator) < validatorWatchRegistrationKey(right.ValidatorAddress, right.Delegator)
	})
}

func sortPaymentFeeMultipliers(multipliers []PaymentFeeMultiplier) {
	sort.SliceStable(multipliers, func(i, j int) bool {
		return string(multipliers[i].Normalize().FeeClass) < string(multipliers[j].Normalize().FeeClass)
	})
}

func sortPaymentFeeCharges(charges []PaymentFeeCharge) {
	sort.SliceStable(charges, func(i, j int) bool {
		return charges[i].Normalize().FeeID < charges[j].Normalize().FeeID
	})
}

func sortPaymentFeeRefunds(refunds []PaymentFeeRefund) {
	sort.SliceStable(refunds, func(i, j int) bool {
		return refunds[i].Normalize().RefundID < refunds[j].Normalize().RefundID
	})
}

func sortSecurityReserveAllocationHooks(hooks []SecurityReserveAllocationHook) {
	sort.SliceStable(hooks, func(i, j int) bool {
		return hooks[i].Normalize().HookID < hooks[j].Normalize().HookID
	})
}

func sortSettlementInclusionLatencies(records []SettlementInclusionLatency) {
	sort.SliceStable(records, func(i, j int) bool {
		return records[i].Normalize().RecordID < records[j].Normalize().RecordID
	})
}

func sortAsyncFinalizationJobs(jobs []AsyncFinalizationJob) {
	sort.SliceStable(jobs, func(i, j int) bool {
		return jobs[i].Normalize().JobID < jobs[j].Normalize().JobID
	})
}

func sortAsyncPromiseExpiryJobs(jobs []AsyncPromiseExpiryJob) {
	sort.SliceStable(jobs, func(i, j int) bool {
		return jobs[i].Normalize().JobID < jobs[j].Normalize().JobID
	})
}

func sortAsyncSettlementCompletions(completions []AsyncSettlementCompletion) {
	sort.SliceStable(completions, func(i, j int) bool {
		return completions[i].Normalize().CompletionID < completions[j].Normalize().CompletionID
	})
}

func edgeKey(edge ChannelEdge) string {
	return fmt.Sprintf("%s/%s/%s", edge.ChannelID, edge.From, edge.To)
}
