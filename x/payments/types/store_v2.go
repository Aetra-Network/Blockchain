package types

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/sovereign-l1/l1/app/addressing"
)

type StoreV2ChannelRecord struct {
	Key                     string
	Version                 uint64
	ChannelID               string
	Channel                 ChannelRecord
	LatestStateHash         string
	LatestStateNonce        uint64
	PendingCloseKey         string
	ParticipantIndexKeys    []string
	RoutingAdvertisementKey string
}

type StoreV2ChannelStateRecord struct {
	Key              string
	Version          uint64
	ChannelID        string
	Nonce            uint64
	StateHash        string
	FullState        ChannelState
	SubmittedOnChain bool
	CheckpointHeight uint64
}

type StoreV2PendingCloseRecord struct {
	Key       string
	Version   uint64
	ChannelID string
	Close     PendingClose
}

type StoreV2ConditionRecord struct {
	Key           string
	Version       uint64
	ConditionID   string
	ChannelID     string
	Promise       ConditionalPromise
	ExpiresHeight uint64
	Settled       bool
	ClaimEvidence string
}

type StoreV2ParticipantChannelRecord struct {
	Key         string
	Version     uint64
	Participant string
	ChannelID   string
}

type StoreV2SettlementTombstoneRecord struct {
	Key              string
	Version          uint64
	ChannelID        string
	Tombstone        ClosedChannelTombstone
	PruneAfterHeight uint64
}

type StoreV2FeeAccumulatorRecord struct {
	Key          string
	Version      uint64
	BlockOrEpoch string
	Bucket       string
	Amount       string
}

type StoreV2FraudProofRecord struct {
	Key       string
	Version   uint64
	ProofID   string
	ChannelID string
	Proof     FraudProof
}

type StoreV2Layout struct {
	Version              uint64
	Channels             []StoreV2ChannelRecord
	ChannelStates        []StoreV2ChannelStateRecord
	PendingCloses        []StoreV2PendingCloseRecord
	Conditions           []StoreV2ConditionRecord
	VirtualChannels      []StoreV2VirtualChannelRecord
	ParticipantChannels  []StoreV2ParticipantChannelRecord
	SettlementTombstones []StoreV2SettlementTombstoneRecord
	FeeAccumulators      []StoreV2FeeAccumulatorRecord
	FraudProofs          []StoreV2FraudProofRecord
}

type ParticipantChannelPageRequest struct {
	Address string
	Offset  uint64
	Limit   uint64
}

type ParticipantChannelPageResponse struct {
	Entries    []StoreV2ParticipantChannelRecord
	NextOffset uint64
	Total      uint64
}

func (r StoreV2ChannelRecord) Normalize() StoreV2ChannelRecord {
	r.Key = strings.TrimSpace(r.Key)
	if r.Version == 0 {
		r.Version = StoreV2MigrationVersion
	}
	r.ChannelID = normalizeHash(r.ChannelID)
	r.Channel = r.Channel.Normalize()
	r.LatestStateHash = normalizeOptionalHash(r.LatestStateHash)
	r.PendingCloseKey = strings.TrimSpace(r.PendingCloseKey)
	r.ParticipantIndexKeys = normalizeStoreKeySlice(r.ParticipantIndexKeys)
	r.RoutingAdvertisementKey = strings.TrimSpace(r.RoutingAdvertisementKey)
	return r
}

func (r StoreV2ChannelRecord) Validate() error {
	r = r.Normalize()
	if r.Version != StoreV2MigrationVersion {
		return errors.New("payments store v2 channel record version mismatch")
	}
	if r.Key != StoreV2ChannelKey(r.ChannelID) {
		return errors.New("payments store v2 channel key mismatch")
	}
	if r.Channel.ChannelID != r.ChannelID {
		return errors.New("payments store v2 channel id mismatch")
	}
	if err := ValidateHash("payments store v2 latest state hash", r.LatestStateHash); err != nil {
		return err
	}
	if r.LatestStateNonce == 0 {
		return errors.New("payments store v2 latest state nonce must be positive")
	}
	if len(r.Channel.LatestState.Signatures) != 0 {
		return errors.New("payments store v2 active channel record must stay compact")
	}
	for _, key := range r.ParticipantIndexKeys {
		if !strings.HasPrefix(key, paymentKey(StoreV2KeyParticipantChannelsPrefix)+"/") {
			return errors.New("payments store v2 participant index key prefix mismatch")
		}
	}
	return nil
}

func (r StoreV2ChannelStateRecord) Normalize() StoreV2ChannelStateRecord {
	r.Key = strings.TrimSpace(r.Key)
	if r.Version == 0 {
		r.Version = StoreV2MigrationVersion
	}
	r.ChannelID = normalizeHash(r.ChannelID)
	r.StateHash = normalizeOptionalHash(r.StateHash)
	r.FullState = r.FullState.Normalize()
	return r
}

func (r StoreV2ChannelStateRecord) Validate() error {
	r = r.Normalize()
	if r.Version != StoreV2MigrationVersion {
		return errors.New("payments store v2 channel state record version mismatch")
	}
	if r.Key != StoreV2ChannelStateKey(r.ChannelID, r.Nonce) {
		return errors.New("payments store v2 channel state key mismatch")
	}
	if r.Nonce == 0 {
		return errors.New("payments store v2 channel state nonce must be positive")
	}
	if err := ValidateHash("payments store v2 channel state hash", r.StateHash); err != nil {
		return err
	}
	if r.FullState.StateHash != "" && r.FullState.StateHash != r.StateHash {
		return errors.New("payments store v2 channel state hash mismatch")
	}
	if !r.SubmittedOnChain && len(r.FullState.Signatures) != 0 {
		return errors.New("payments store v2 off-chain checkpoint stores hash only")
	}
	return nil
}

func (r StoreV2PendingCloseRecord) Normalize() StoreV2PendingCloseRecord {
	r.Key = strings.TrimSpace(r.Key)
	if r.Version == 0 {
		r.Version = StoreV2MigrationVersion
	}
	r.ChannelID = normalizeHash(r.ChannelID)
	r.Close = r.Close.Normalize()
	return r
}

func (r StoreV2PendingCloseRecord) Validate() error {
	r = r.Normalize()
	if r.Version != StoreV2MigrationVersion {
		return errors.New("payments store v2 pending close record version mismatch")
	}
	if r.Key != StoreV2PendingCloseKey(r.ChannelID) {
		return errors.New("payments store v2 pending close key mismatch")
	}
	if r.Close.State.ChannelID != r.ChannelID {
		return errors.New("payments store v2 pending close channel mismatch")
	}
	return nil
}

func (r StoreV2ConditionRecord) Normalize() StoreV2ConditionRecord {
	r.Key = strings.TrimSpace(r.Key)
	if r.Version == 0 {
		r.Version = StoreV2MigrationVersion
	}
	r.ConditionID = normalizeHash(r.ConditionID)
	r.ChannelID = normalizeHash(r.ChannelID)
	r.Promise = r.Promise.Normalize()
	r.ClaimEvidence = normalizeOptionalHash(r.ClaimEvidence)
	return r
}

func (r StoreV2ConditionRecord) Validate() error {
	r = r.Normalize()
	if r.Version != StoreV2MigrationVersion {
		return errors.New("payments store v2 condition record version mismatch")
	}
	if r.Key != StoreV2ConditionKey(r.ConditionID) {
		return errors.New("payments store v2 condition key mismatch")
	}
	if err := ValidateHash("payments store v2 condition id", r.ConditionID); err != nil {
		return err
	}
	return ValidateHash("payments store v2 condition channel id", r.ChannelID)
}

func (r StoreV2ParticipantChannelRecord) Normalize() StoreV2ParticipantChannelRecord {
	r.Key = strings.TrimSpace(r.Key)
	if r.Version == 0 {
		r.Version = StoreV2MigrationVersion
	}
	r.Participant = strings.TrimSpace(r.Participant)
	r.ChannelID = normalizeHash(r.ChannelID)
	return r
}

func (r StoreV2ParticipantChannelRecord) Validate() error {
	r = r.Normalize()
	if r.Version != StoreV2MigrationVersion {
		return errors.New("payments store v2 participant channel version mismatch")
	}
	if err := addressing.ValidateUserAddress("payments store v2 participant", r.Participant); err != nil {
		return err
	}
	if r.Key != StoreV2ParticipantChannelKey(r.Participant, r.ChannelID) {
		return errors.New("payments store v2 participant channel key mismatch")
	}
	return nil
}

func (r StoreV2SettlementTombstoneRecord) Normalize() StoreV2SettlementTombstoneRecord {
	r.Key = strings.TrimSpace(r.Key)
	if r.Version == 0 {
		r.Version = StoreV2MigrationVersion
	}
	r.ChannelID = normalizeHash(r.ChannelID)
	r.Tombstone = r.Tombstone.Normalize()
	if r.PruneAfterHeight == 0 {
		r.PruneAfterHeight = r.Tombstone.ExpiresHeight
	}
	return r
}

func (r StoreV2SettlementTombstoneRecord) Validate() error {
	r = r.Normalize()
	if r.Version != StoreV2MigrationVersion {
		return errors.New("payments store v2 tombstone version mismatch")
	}
	if r.Key != StoreV2SettlementTombstoneKey(r.ChannelID) {
		return errors.New("payments store v2 tombstone key mismatch")
	}
	if r.Tombstone.ChannelID != r.ChannelID {
		return errors.New("payments store v2 tombstone channel mismatch")
	}
	if r.PruneAfterHeight < r.Tombstone.ClosedHeight {
		return errors.New("payments store v2 tombstone prune height before close")
	}
	return nil
}

func (r StoreV2FeeAccumulatorRecord) Normalize() StoreV2FeeAccumulatorRecord {
	r.Key = strings.TrimSpace(r.Key)
	if r.Version == 0 {
		r.Version = StoreV2MigrationVersion
	}
	r.BlockOrEpoch = strings.TrimSpace(r.BlockOrEpoch)
	r.Bucket = strings.TrimSpace(r.Bucket)
	r.Amount = strings.TrimSpace(r.Amount)
	if r.Amount == "" {
		r.Amount = "0"
	}
	return r
}

func (r StoreV2FeeAccumulatorRecord) Validate() error {
	r = r.Normalize()
	if r.Version != StoreV2MigrationVersion {
		return errors.New("payments store v2 fee accumulator version mismatch")
	}
	if r.Key != StoreV2FeeAccumulatorKey(r.BlockOrEpoch, r.Bucket) {
		return errors.New("payments store v2 fee accumulator key mismatch")
	}
	return validateNonNegativeInt("payments store v2 fee accumulator amount", r.Amount)
}

func (r StoreV2FraudProofRecord) Normalize() StoreV2FraudProofRecord {
	r.Key = strings.TrimSpace(r.Key)
	if r.Version == 0 {
		r.Version = StoreV2MigrationVersion
	}
	r.ProofID = normalizeHash(r.ProofID)
	r.ChannelID = normalizeHash(r.ChannelID)
	r.Proof = r.Proof.Normalize()
	return r
}

func (r StoreV2FraudProofRecord) Validate() error {
	r = r.Normalize()
	if r.Version != StoreV2MigrationVersion {
		return errors.New("payments store v2 fraud proof version mismatch")
	}
	if r.Key != StoreV2FraudProofKey(r.ProofID) {
		return errors.New("payments store v2 fraud proof key mismatch")
	}
	if r.Proof.ProofID != r.ProofID {
		return errors.New("payments store v2 fraud proof id mismatch")
	}
	return ValidateHash("payments store v2 fraud proof channel id", r.ChannelID)
}

func (l StoreV2Layout) Normalize() StoreV2Layout {
	if l.Version == 0 {
		l.Version = StoreV2MigrationVersion
	}
	for i := range l.Channels {
		l.Channels[i] = l.Channels[i].Normalize()
	}
	for i := range l.ChannelStates {
		l.ChannelStates[i] = l.ChannelStates[i].Normalize()
	}
	for i := range l.PendingCloses {
		l.PendingCloses[i] = l.PendingCloses[i].Normalize()
	}
	for i := range l.Conditions {
		l.Conditions[i] = l.Conditions[i].Normalize()
	}
	for i := range l.VirtualChannels {
		l.VirtualChannels[i] = l.VirtualChannels[i].Normalize()
	}
	for i := range l.ParticipantChannels {
		l.ParticipantChannels[i] = l.ParticipantChannels[i].Normalize()
	}
	for i := range l.SettlementTombstones {
		l.SettlementTombstones[i] = l.SettlementTombstones[i].Normalize()
	}
	for i := range l.FeeAccumulators {
		l.FeeAccumulators[i] = l.FeeAccumulators[i].Normalize()
	}
	for i := range l.FraudProofs {
		l.FraudProofs[i] = l.FraudProofs[i].Normalize()
	}
	sortStoreV2Layout(&l)
	return l
}

func (l StoreV2Layout) Validate() error {
	l = l.Normalize()
	if l.Version != StoreV2MigrationVersion {
		return errors.New("payments store v2 layout version mismatch")
	}
	seen := map[string]struct{}{}
	validateKey := func(key string) error {
		if key == "" {
			return errors.New("payments store v2 key is required")
		}
		if _, found := seen[key]; found {
			return fmt.Errorf("payments store v2 duplicate key %s", key)
		}
		seen[key] = struct{}{}
		return nil
	}
	for _, record := range l.Channels {
		if err := validateKey(record.Key); err != nil {
			return err
		}
		if err := record.Validate(); err != nil {
			return err
		}
	}
	for _, record := range l.ChannelStates {
		if err := validateKey(record.Key); err != nil {
			return err
		}
		if err := record.Validate(); err != nil {
			return err
		}
	}
	for _, record := range l.PendingCloses {
		if err := validateKey(record.Key); err != nil {
			return err
		}
		if err := record.Validate(); err != nil {
			return err
		}
	}
	for _, record := range l.Conditions {
		if err := validateKey(record.Key); err != nil {
			return err
		}
		if err := record.Validate(); err != nil {
			return err
		}
	}
	for _, record := range l.VirtualChannels {
		if err := validateKey(record.Key); err != nil {
			return err
		}
		if err := record.Validate(); err != nil {
			return err
		}
	}
	for _, record := range l.ParticipantChannels {
		if err := validateKey(record.Key); err != nil {
			return err
		}
		if err := record.Validate(); err != nil {
			return err
		}
	}
	for _, record := range l.SettlementTombstones {
		if err := validateKey(record.Key); err != nil {
			return err
		}
		if err := record.Validate(); err != nil {
			return err
		}
	}
	for _, record := range l.FeeAccumulators {
		if err := validateKey(record.Key); err != nil {
			return err
		}
		if err := record.Validate(); err != nil {
			return err
		}
	}
	for _, record := range l.FraudProofs {
		if err := validateKey(record.Key); err != nil {
			return err
		}
		if err := record.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func normalizeStoreKeySlice(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := strings.TrimSpace(value)
		if normalized == "" {
			continue
		}
		if _, found := seen[normalized]; found {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	sortStrings(out)
	return out
}

func sortStoreV2Layout(layout *StoreV2Layout) {
	sort.SliceStable(layout.Channels, func(i, j int) bool { return layout.Channels[i].Key < layout.Channels[j].Key })
	sort.SliceStable(layout.ChannelStates, func(i, j int) bool { return layout.ChannelStates[i].Key < layout.ChannelStates[j].Key })
	sort.SliceStable(layout.PendingCloses, func(i, j int) bool { return layout.PendingCloses[i].Key < layout.PendingCloses[j].Key })
	sort.SliceStable(layout.Conditions, func(i, j int) bool { return layout.Conditions[i].Key < layout.Conditions[j].Key })
	sort.SliceStable(layout.VirtualChannels, func(i, j int) bool { return layout.VirtualChannels[i].Key < layout.VirtualChannels[j].Key })
	sort.SliceStable(layout.ParticipantChannels, func(i, j int) bool { return layout.ParticipantChannels[i].Key < layout.ParticipantChannels[j].Key })
	sort.SliceStable(layout.SettlementTombstones, func(i, j int) bool { return layout.SettlementTombstones[i].Key < layout.SettlementTombstones[j].Key })
	sort.SliceStable(layout.FeeAccumulators, func(i, j int) bool { return layout.FeeAccumulators[i].Key < layout.FeeAccumulators[j].Key })
	sort.SliceStable(layout.FraudProofs, func(i, j int) bool { return layout.FraudProofs[i].Key < layout.FraudProofs[j].Key })
}

func compactStoreV2Channel(channel ChannelRecord) ChannelRecord {
	channel = channel.Normalize()
	channel.LatestState = compactStoreV2State(channel.LatestState)
	channel.PendingClose = PendingClose{}
	return channel
}

func compactStoreV2State(state ChannelState) ChannelState {
	state = state.Normalize()
	return ChannelState{
		ChainID:     state.ChainID,
		ChannelID:   state.ChannelID,
		ChannelType: state.ChannelType,
		Denom:       state.Denom,
		Version:     state.Version,
		Nonce:       state.Nonce,
		Epoch:       state.Epoch,
		StateHash:   state.StateHash,
	}
}

func StoreV2RoutingKeyForChannel(channel ChannelRecord) string {
	channel = channel.Normalize()
	if !channel.RoutingAdvertised {
		return ""
	}
	return PaymentRoutingAdvertisementIndexKey(channel.ChannelID)
}

func storeV2ConditionFromPayment(channel ChannelRecord, condition ConditionalPayment, settled bool) StoreV2ConditionRecord {
	channel = channel.Normalize()
	condition = condition.Normalize()
	return StoreV2ConditionRecord{
		Key:           StoreV2ConditionKey(condition.ConditionID),
		Version:       StoreV2MigrationVersion,
		ConditionID:   condition.ConditionID,
		ChannelID:     channel.ChannelID,
		ExpiresHeight: condition.TimeoutHeight,
		Settled:       settled,
	}.Normalize()
}
