package types

import (
	"errors"
)

func BuildAdaptiveSyncSnapshot(state PaymentsState, height uint64) (AdaptiveSyncSnapshot, error) {
	if height == 0 {
		return AdaptiveSyncSnapshot{}, errors.New("payments adaptive sync snapshot height must be positive")
	}
	state = state.Export()
	if err := state.Validate(); err != nil {
		return AdaptiveSyncSnapshot{}, err
	}
	layout, err := BuildStoreV2Layout(state)
	if err != nil {
		return AdaptiveSyncSnapshot{}, err
	}
	snapshot := AdaptiveSyncSnapshot{
		Key:                     StoreV2AdaptiveSnapshotKey(height),
		Version:                 StoreV2MigrationVersion,
		Height:                  height,
		Layout:                  layout,
		ConsensusOnly:           true,
		RoutingTopologyExcluded: true,
	}
	for _, channel := range state.Channels {
		channel = channel.Normalize()
		if channel.Status == ChannelStatusPendingClose && channel.PendingClose.State.StateHash != "" {
			if channel.PendingClose.DisputeCount > 0 || channel.Finality == ChannelFinalityInDispute {
				snapshot.ActiveDisputes = append(snapshot.ActiveDisputes, AdaptiveSyncActiveDisputeIndex{
					Key:               StoreV2ActiveDisputeKey(channel.ChannelID),
					ChannelID:         channel.ChannelID,
					PendingStateHash:  channel.PendingClose.State.StateHash,
					PendingNonce:      channel.PendingClose.State.Nonce,
					SubmittedHeight:   channel.PendingClose.SubmittedHeight,
					SettleAfterHeight: channel.PendingClose.SettleAfterHeight,
					DisputeCount:      channel.PendingClose.DisputeCount,
					Submitter:         channel.PendingClose.Submitter,
				}.Normalize())
			}
			if pendingHeight, ok := PendingFinalizationHeightForChannel(channel); ok {
				snapshot.PendingFinalizations = append(snapshot.PendingFinalizations, AdaptiveSyncPendingFinalizationIndex{
					Key:              StoreV2PendingFinalizationKey(channel.ChannelID),
					ChannelID:        channel.ChannelID,
					PendingHeight:    pendingHeight,
					Finality:         channel.Finality,
					PendingStateHash: channel.PendingClose.State.StateHash,
					PendingNonce:     channel.PendingClose.State.Nonce,
				}.Normalize())
			}
		}
	}
	for _, event := range state.Events {
		event = event.Normalize()
		snapshot.WatcherReplayEvents = append(snapshot.WatcherReplayEvents, AdaptiveSyncWatcherReplayEvent{
			Key:       StoreV2WatcherReplayEventKey(event.Height, event.EventID),
			Event:     event,
			EventHash: AdaptiveSyncEventHash(event),
		}.Normalize())
	}
	snapshot = snapshot.Normalize()
	snapshot.SnapshotHash = ComputeAdaptiveSyncSnapshotHash(snapshot)
	if err := snapshot.Validate(); err != nil {
		return AdaptiveSyncSnapshot{}, err
	}
	return snapshot, nil
}

func RecoverAdaptiveSyncSafety(snapshot AdaptiveSyncSnapshot) (AdaptiveSyncRecoveryState, error) {
	snapshot = snapshot.Normalize()
	if err := snapshot.Validate(); err != nil {
		return AdaptiveSyncRecoveryState{}, err
	}
	recovered := AdaptiveSyncRecoveryState{RecoveredFromSnapshotHash: snapshot.SnapshotHash}
	for _, channel := range snapshot.Layout.Channels {
		recovered.ActiveChannelIDs = append(recovered.ActiveChannelIDs, channel.ChannelID)
		if channel.PendingCloseKey != "" {
			recovered.PendingCloseChannelIDs = append(recovered.PendingCloseChannelIDs, channel.ChannelID)
		}
	}
	for _, condition := range snapshot.Layout.Conditions {
		if !condition.Settled {
			recovered.UnresolvedConditionIDs = append(recovered.UnresolvedConditionIDs, condition.ConditionID)
		}
	}
	for _, vc := range snapshot.Layout.VirtualChannels {
		recovered.VirtualChannelIDs = append(recovered.VirtualChannelIDs, vc.VirtualChannelID)
	}
	for _, tombstone := range snapshot.Layout.SettlementTombstones {
		recovered.SettlementTombstoneIDs = append(recovered.SettlementTombstoneIDs, tombstone.ChannelID)
	}
	for _, dispute := range snapshot.ActiveDisputes {
		recovered.ActiveDisputeChannelIDs = append(recovered.ActiveDisputeChannelIDs, dispute.ChannelID)
	}
	for _, pending := range snapshot.PendingFinalizations {
		recovered.PendingFinalizationIDs = append(recovered.PendingFinalizationIDs, pending.ChannelID)
	}
	for _, event := range snapshot.WatcherReplayEvents {
		recovered.WatcherReplayEventIDs = append(recovered.WatcherReplayEventIDs, event.Event.EventID)
	}
	sortStrings(recovered.ActiveChannelIDs)
	sortStrings(recovered.PendingCloseChannelIDs)
	sortStrings(recovered.UnresolvedConditionIDs)
	sortStrings(recovered.VirtualChannelIDs)
	sortStrings(recovered.SettlementTombstoneIDs)
	sortStrings(recovered.ActiveDisputeChannelIDs)
	sortStrings(recovered.PendingFinalizationIDs)
	sortStrings(recovered.WatcherReplayEventIDs)
	return recovered, nil
}
