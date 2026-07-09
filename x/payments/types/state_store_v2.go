package types

import (
	"errors"
	"strings"

	"github.com/sovereign-l1/l1/app/addressing"
)

func BuildStoreV2Layout(state PaymentsState) (StoreV2Layout, error) {
	state = state.Export()
	if err := state.Validate(); err != nil {
		return StoreV2Layout{}, err
	}
	layout := StoreV2Layout{Version: StoreV2MigrationVersion}
	seenConditionKeys := map[string]struct{}{}
	appendCondition := func(record StoreV2ConditionRecord) {
		record = record.Normalize()
		if _, found := seenConditionKeys[record.Key]; found {
			return
		}
		seenConditionKeys[record.Key] = struct{}{}
		layout.Conditions = append(layout.Conditions, record)
	}
	for _, channel := range state.Channels {
		channel = channel.Normalize()
		compact := compactStoreV2Channel(channel)
		participantKeys := make([]string, 0, len(channel.Participants))
		for _, participant := range channel.Participants {
			index := StoreV2ParticipantChannelRecord{
				Key:         StoreV2ParticipantChannelKey(participant, channel.ChannelID),
				Version:     StoreV2MigrationVersion,
				Participant: participant,
				ChannelID:   channel.ChannelID,
			}.Normalize()
			layout.ParticipantChannels = append(layout.ParticipantChannels, index)
			participantKeys = append(participantKeys, index.Key)
		}
		sortStrings(participantKeys)
		record := StoreV2ChannelRecord{
			Key:                     StoreV2ChannelKey(channel.ChannelID),
			Version:                 StoreV2MigrationVersion,
			ChannelID:               channel.ChannelID,
			Channel:                 compact,
			LatestStateHash:         channel.LatestState.StateHash,
			LatestStateNonce:        channel.LatestState.Nonce,
			ParticipantIndexKeys:    participantKeys,
			RoutingAdvertisementKey: StoreV2RoutingKeyForChannel(channel),
		}
		layout.Channels = append(layout.Channels, record.Normalize())
		addLatestHashCheckpoint := true
		for _, condition := range channel.LatestState.Conditions {
			appendCondition(storeV2ConditionFromPayment(channel, condition, false))
		}
		if channel.PendingClose.State.StateHash != "" {
			if channel.PendingClose.State.Nonce == channel.LatestState.Nonce {
				addLatestHashCheckpoint = false
			}
			pending := StoreV2PendingCloseRecord{
				Key:       StoreV2PendingCloseKey(channel.ChannelID),
				Version:   StoreV2MigrationVersion,
				ChannelID: channel.ChannelID,
				Close:     channel.PendingClose,
			}.Normalize()
			layout.PendingCloses = append(layout.PendingCloses, pending)
			layout.Channels[len(layout.Channels)-1].PendingCloseKey = pending.Key
			layout.ChannelStates = append(layout.ChannelStates, StoreV2ChannelStateRecord{
				Key:              StoreV2ChannelStateKey(channel.ChannelID, channel.PendingClose.State.Nonce),
				Version:          StoreV2MigrationVersion,
				ChannelID:        channel.ChannelID,
				Nonce:            channel.PendingClose.State.Nonce,
				StateHash:        channel.PendingClose.State.StateHash,
				FullState:        channel.PendingClose.State,
				SubmittedOnChain: true,
				CheckpointHeight: channel.PendingClose.SubmittedHeight,
			}.Normalize())
			for _, condition := range channel.PendingClose.State.Conditions {
				appendCondition(storeV2ConditionFromPayment(channel, condition, false))
			}
			for _, proof := range channel.PendingClose.FraudProofs {
				layout.FraudProofs = append(layout.FraudProofs, StoreV2FraudProofRecord{
					Key:       StoreV2FraudProofKey(proof.ProofID),
					Version:   StoreV2MigrationVersion,
					ProofID:   proof.ProofID,
					ChannelID: channel.ChannelID,
					Proof:     proof,
				}.Normalize())
			}
		}
		if addLatestHashCheckpoint {
			layout.ChannelStates = append(layout.ChannelStates, StoreV2ChannelStateRecord{
				Key:              StoreV2ChannelStateKey(channel.ChannelID, channel.LatestState.Nonce),
				Version:          StoreV2MigrationVersion,
				ChannelID:        channel.ChannelID,
				Nonce:            channel.LatestState.Nonce,
				StateHash:        channel.LatestState.StateHash,
				FullState:        compactStoreV2State(channel.LatestState),
				SubmittedOnChain: false,
				CheckpointHeight: channel.OpenHeight,
			}.Normalize())
		}
	}
	for _, vc := range state.VirtualChannels {
		vc = vc.Normalize()
		layout.VirtualChannels = append(layout.VirtualChannels, StoreV2VirtualChannelRecord{
			Key:              StoreV2VirtualChannelKey(vc.VirtualChannelID),
			Version:          StoreV2MigrationVersion,
			VirtualChannelID: vc.VirtualChannelID,
			Channel:          vc,
			AnchorHash:       vc.AnchorCommitment,
		}.Normalize())
	}
	for _, tombstone := range state.ClosedChannels {
		tombstone = tombstone.Normalize()
		layout.SettlementTombstones = append(layout.SettlementTombstones, StoreV2SettlementTombstoneRecord{
			Key:              StoreV2SettlementTombstoneKey(tombstone.ChannelID),
			Version:          StoreV2MigrationVersion,
			ChannelID:        tombstone.ChannelID,
			Tombstone:        tombstone,
			PruneAfterHeight: tombstone.ExpiresHeight,
		}.Normalize())
	}
	layout = layout.Normalize()
	if err := layout.Validate(); err != nil {
		return StoreV2Layout{}, err
	}
	return layout, nil
}

func PruneStoreV2Layout(layout StoreV2Layout, currentHeight uint64) (StoreV2Layout, error) {
	if currentHeight == 0 {
		return StoreV2Layout{}, errors.New("payments store v2 prune height must be positive")
	}
	layout = layout.Normalize()
	pruned := layout
	pruned.SettlementTombstones = pruned.SettlementTombstones[:0]
	for _, tombstone := range layout.SettlementTombstones {
		tombstone = tombstone.Normalize()
		if tombstone.PruneAfterHeight == 0 || tombstone.PruneAfterHeight >= currentHeight {
			pruned.SettlementTombstones = append(pruned.SettlementTombstones, tombstone)
		}
	}
	pruned.Conditions = pruned.Conditions[:0]
	for _, condition := range layout.Conditions {
		condition = condition.Normalize()
		if !condition.Settled && (condition.ExpiresHeight == 0 || condition.ExpiresHeight >= currentHeight) {
			pruned.Conditions = append(pruned.Conditions, condition)
		}
	}
	pruned = pruned.Normalize()
	return pruned, pruned.Validate()
}

func QueryStoreV2ParticipantChannels(layout StoreV2Layout, req ParticipantChannelPageRequest) (ParticipantChannelPageResponse, error) {
	layout = layout.Normalize()
	address := strings.TrimSpace(req.Address)
	if err := addressing.ValidateUserAddress("payments store v2 participant query address", address); err != nil {
		return ParticipantChannelPageResponse{}, err
	}
	limit := req.Limit
	if limit == 0 {
		limit = 50
	}
	matches := []StoreV2ParticipantChannelRecord{}
	for _, entry := range layout.ParticipantChannels {
		entry = entry.Normalize()
		if entry.Participant == address {
			matches = append(matches, entry)
		}
	}
	total := uint64(len(matches))
	if req.Offset >= total {
		return ParticipantChannelPageResponse{Entries: []StoreV2ParticipantChannelRecord{}, Total: total}, nil
	}
	end := req.Offset + limit
	if end > total {
		end = total
	}
	next := uint64(0)
	if end < total {
		next = end
	}
	return ParticipantChannelPageResponse{
		Entries:    append([]StoreV2ParticipantChannelRecord(nil), matches[req.Offset:end]...),
		NextOffset: next,
		Total:      total,
	}, nil
}
