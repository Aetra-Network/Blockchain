package types

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/sovereign-l1/l1/app/addressing"
)

type AdaptiveSyncSnapshot struct {
	Key                     string
	Version                 uint64
	Height                  uint64
	Layout                  StoreV2Layout
	ActiveDisputes          []AdaptiveSyncActiveDisputeIndex
	PendingFinalizations    []AdaptiveSyncPendingFinalizationIndex
	WatcherReplayEvents     []AdaptiveSyncWatcherReplayEvent
	SnapshotHash            string
	ConsensusOnly           bool
	RoutingTopologyExcluded bool
}

type AdaptiveSyncActiveDisputeIndex struct {
	Key               string
	ChannelID         string
	PendingStateHash  string
	PendingNonce      uint64
	SubmittedHeight   uint64
	SettleAfterHeight uint64
	DisputeCount      uint32
	Submitter         string
}

type AdaptiveSyncPendingFinalizationIndex struct {
	Key              string
	ChannelID        string
	PendingHeight    uint64
	Finality         ChannelFinality
	PendingStateHash string
	PendingNonce     uint64
}

type AdaptiveSyncWatcherReplayEvent struct {
	Key       string
	Event     PaymentEvent
	EventHash string
}

type AdaptiveSyncRecoveryState struct {
	ActiveChannelIDs          []string
	PendingCloseChannelIDs    []string
	UnresolvedConditionIDs    []string
	VirtualChannelIDs         []string
	SettlementTombstoneIDs    []string
	ActiveDisputeChannelIDs   []string
	PendingFinalizationIDs    []string
	WatcherReplayEventIDs     []string
	RecoveredFromSnapshotHash string
}

func (s AdaptiveSyncSnapshot) Normalize() AdaptiveSyncSnapshot {
	s.Key = strings.TrimSpace(s.Key)
	if s.Version == 0 {
		s.Version = StoreV2MigrationVersion
	}
	s.Layout = s.Layout.Normalize()
	for i := range s.ActiveDisputes {
		s.ActiveDisputes[i] = s.ActiveDisputes[i].Normalize()
	}
	for i := range s.PendingFinalizations {
		s.PendingFinalizations[i] = s.PendingFinalizations[i].Normalize()
	}
	for i := range s.WatcherReplayEvents {
		s.WatcherReplayEvents[i] = s.WatcherReplayEvents[i].Normalize()
	}
	sort.SliceStable(s.ActiveDisputes, func(i, j int) bool { return s.ActiveDisputes[i].Key < s.ActiveDisputes[j].Key })
	sort.SliceStable(s.PendingFinalizations, func(i, j int) bool { return s.PendingFinalizations[i].Key < s.PendingFinalizations[j].Key })
	sort.SliceStable(s.WatcherReplayEvents, func(i, j int) bool { return s.WatcherReplayEvents[i].Key < s.WatcherReplayEvents[j].Key })
	s.SnapshotHash = normalizeOptionalHash(s.SnapshotHash)
	return s
}

func (s AdaptiveSyncSnapshot) Validate() error {
	s = s.Normalize()
	if s.Version != StoreV2MigrationVersion {
		return errors.New("payments adaptive sync snapshot version mismatch")
	}
	if s.Height == 0 {
		return errors.New("payments adaptive sync snapshot height must be positive")
	}
	if s.Key != StoreV2AdaptiveSnapshotKey(s.Height) {
		return errors.New("payments adaptive sync snapshot key mismatch")
	}
	if !s.ConsensusOnly || !s.RoutingTopologyExcluded {
		return errors.New("payments adaptive sync snapshot must exclude routing topology")
	}
	if err := s.Layout.Validate(); err != nil {
		return err
	}
	pendingByChannel := map[string]StoreV2PendingCloseRecord{}
	for _, pending := range s.Layout.PendingCloses {
		pendingByChannel[pending.ChannelID] = pending
	}
	seen := map[string]struct{}{}
	for _, dispute := range s.ActiveDisputes {
		if err := dispute.Validate(); err != nil {
			return err
		}
		if _, found := pendingByChannel[dispute.ChannelID]; !found {
			return errors.New("payments adaptive sync active dispute missing pending close")
		}
		if _, found := seen[dispute.Key]; found {
			return errors.New("payments adaptive sync duplicate active dispute")
		}
		seen[dispute.Key] = struct{}{}
	}
	seen = map[string]struct{}{}
	for _, pending := range s.PendingFinalizations {
		if err := pending.Validate(); err != nil {
			return err
		}
		if _, found := pendingByChannel[pending.ChannelID]; !found {
			return errors.New("payments adaptive sync pending finalization missing pending close")
		}
		if _, found := seen[pending.Key]; found {
			return errors.New("payments adaptive sync duplicate pending finalization")
		}
		seen[pending.Key] = struct{}{}
	}
	seen = map[string]struct{}{}
	for _, event := range s.WatcherReplayEvents {
		if err := event.Validate(); err != nil {
			return err
		}
		if _, found := seen[event.Key]; found {
			return errors.New("payments adaptive sync duplicate watcher replay event")
		}
		seen[event.Key] = struct{}{}
	}
	if s.SnapshotHash == "" {
		return errors.New("payments adaptive sync snapshot hash is required")
	}
	if expected := ComputeAdaptiveSyncSnapshotHash(s); s.SnapshotHash != expected {
		return errors.New("payments adaptive sync snapshot hash mismatch")
	}
	return nil
}

func (i AdaptiveSyncActiveDisputeIndex) Normalize() AdaptiveSyncActiveDisputeIndex {
	i.Key = strings.TrimSpace(i.Key)
	i.ChannelID = normalizeHash(i.ChannelID)
	i.PendingStateHash = normalizeHash(i.PendingStateHash)
	i.Submitter = strings.TrimSpace(i.Submitter)
	return i
}

func (i AdaptiveSyncActiveDisputeIndex) Validate() error {
	i = i.Normalize()
	if i.Key != StoreV2ActiveDisputeKey(i.ChannelID) {
		return errors.New("payments adaptive sync active dispute key mismatch")
	}
	if err := ValidateHash("payments adaptive sync active dispute channel id", i.ChannelID); err != nil {
		return err
	}
	if err := ValidateHash("payments adaptive sync active dispute state hash", i.PendingStateHash); err != nil {
		return err
	}
	if i.PendingNonce == 0 || i.SubmittedHeight == 0 || i.SettleAfterHeight == 0 {
		return errors.New("payments adaptive sync active dispute heights and nonce must be positive")
	}
	if i.DisputeCount == 0 {
		return errors.New("payments adaptive sync active dispute count must be positive")
	}
	return addressing.ValidateUserAddress("payments adaptive sync active dispute submitter", i.Submitter)
}

func (i AdaptiveSyncPendingFinalizationIndex) Normalize() AdaptiveSyncPendingFinalizationIndex {
	i.Key = strings.TrimSpace(i.Key)
	i.ChannelID = normalizeHash(i.ChannelID)
	i.PendingStateHash = normalizeHash(i.PendingStateHash)
	return i
}

func (i AdaptiveSyncPendingFinalizationIndex) Validate() error {
	i = i.Normalize()
	if i.Key != StoreV2PendingFinalizationKey(i.ChannelID) {
		return errors.New("payments adaptive sync pending finalization key mismatch")
	}
	if err := ValidateHash("payments adaptive sync pending finalization channel id", i.ChannelID); err != nil {
		return err
	}
	if !IsChannelFinality(i.Finality) {
		return fmt.Errorf("unknown payments adaptive sync pending finalization finality %q", i.Finality)
	}
	if i.PendingHeight == 0 || i.PendingNonce == 0 {
		return errors.New("payments adaptive sync pending finalization height and nonce must be positive")
	}
	return ValidateHash("payments adaptive sync pending finalization state hash", i.PendingStateHash)
}

func (e AdaptiveSyncWatcherReplayEvent) Normalize() AdaptiveSyncWatcherReplayEvent {
	e.Key = strings.TrimSpace(e.Key)
	e.Event = e.Event.Normalize()
	e.EventHash = normalizeOptionalHash(e.EventHash)
	return e
}

func (e AdaptiveSyncWatcherReplayEvent) Validate() error {
	e = e.Normalize()
	if err := e.Event.Validate(); err != nil {
		return err
	}
	if e.Key != StoreV2WatcherReplayEventKey(e.Event.Height, e.Event.EventID) {
		return errors.New("payments adaptive sync watcher replay event key mismatch")
	}
	if e.EventHash != AdaptiveSyncEventHash(e.Event) {
		return errors.New("payments adaptive sync watcher replay event hash mismatch")
	}
	return nil
}

func AdaptiveSyncEventHash(event PaymentEvent) string {
	event = event.Normalize()
	parts := []string{"adaptive-sync-event", event.EventID, event.EventType, event.ChannelID, fmt.Sprintf("%020d", event.Height)}
	for _, attr := range event.Attributes {
		parts = append(parts, attr.Key, attr.Value)
	}
	return HashParts(parts...)
}

func ComputeAdaptiveSyncSnapshotHash(snapshot AdaptiveSyncSnapshot) string {
	snapshot = snapshot.Normalize()
	parts := []string{"adaptive-sync-snapshot", snapshot.Key, fmt.Sprintf("%020d", snapshot.Height), fmt.Sprintf("%t", snapshot.ConsensusOnly), fmt.Sprintf("%t", snapshot.RoutingTopologyExcluded)}
	for _, record := range snapshot.Layout.Channels {
		parts = append(parts, record.Key, record.LatestStateHash)
	}
	for _, record := range snapshot.Layout.PendingCloses {
		parts = append(parts, record.Key, record.Close.State.StateHash)
	}
	for _, record := range snapshot.Layout.Conditions {
		parts = append(parts, record.Key, fmt.Sprintf("%t", record.Settled))
	}
	for _, record := range snapshot.Layout.VirtualChannels {
		parts = append(parts, record.Key, record.AnchorHash)
	}
	for _, record := range snapshot.Layout.SettlementTombstones {
		parts = append(parts, record.Key, record.Tombstone.StateHash)
	}
	for _, dispute := range snapshot.ActiveDisputes {
		parts = append(parts, dispute.Key, dispute.PendingStateHash, fmt.Sprintf("%020d", dispute.PendingNonce))
	}
	for _, pending := range snapshot.PendingFinalizations {
		parts = append(parts, pending.Key, pending.PendingStateHash, fmt.Sprintf("%020d", pending.PendingHeight))
	}
	for _, event := range snapshot.WatcherReplayEvents {
		parts = append(parts, event.Key, event.EventHash)
	}
	return HashParts(parts...)
}
