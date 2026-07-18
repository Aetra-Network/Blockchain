package types

import (
	"bytes"
	"fmt"
	"sort"
)

// GenesisState is the x/aez genesis.
//
// It carries the FULL routing table explicitly rather than a "core-only" flag.
// Genesis is the one place the whole table is legitimately one value (there is
// exactly one table per version, and versions are per-entity keys in the store),
// and shipping all 256 assignments makes the Phase 1 promise -- every bucket on
// zone 0 -- auditable by reading the exported genesis rather than by trusting a
// constructor.
//
// The four message-bus fields below round-trip the Phase 4 machinery's
// authoritative state: the destination delivery queue (InboxPrefix), the
// source-side audit log (OutboxPrefix), the per-(zone,sender) monotonic
// sequence counters (OutboxSeqPrefix), and the committed exactly-once dedupe
// markers (ProcessedPrefix). Before this, a genesis export while a cross-zone
// message was in flight silently dropped it (and its dedupe marker/sequence
// counter) with no error -- invisible under one zone (the queues are always
// empty, aez.md §6's inertness proof), but a genuine completeness gap the
// moment multi-zone is activated. Every one of these is empty at genesis
// today, so DefaultGenesis and every existing export are unaffected.
type GenesisState struct {
	Params		Params
	RoutingTable	RoutingTable
	Zones		[]Zone

	// PendingOutboxMessages is the full content of the source-side audit log
	// (OutboxPrefix) at export time: one entry per emitted, not-yet-terminal
	// cross-zone message. Each carries its own (SourceZone, Sender, SourceSeq),
	// which is exactly what OutboxKey needs to reconstruct its storage key, so
	// no side-channel key material is needed alongside the message.
	PendingOutboxMessages []ZoneMessage

	// OutboxSequences is the full content of the per-(zone,sender) monotonic
	// counter set (OutboxSeqPrefix). SenderKey is the already-hashed 32-byte
	// key (SenderKey()'s output, never the raw sender identity) because that
	// is the only form the counter's OWN store key ever held -- the counter
	// can outlive every outbox message that ever used it, so it cannot be
	// reconstructed from PendingOutboxMessages.
	OutboxSequences []OutboxSeqRecord

	// PendingInboxMessages is the full content of the delivery queue
	// (InboxPrefix): every message scheduled but not yet drained. Each
	// carries its own (DeliverHeight, ID), which is exactly what InboxKey
	// needs to reconstruct its storage key.
	PendingInboxMessages []ZoneMessage

	// ProcessedMarkers is the full content of the committed exactly-once
	// dedupe set (ProcessedPrefix). Losing even one of these across a genesis
	// round-trip would let a message that was already terminal be redelivered
	// on the restored keeper.
	ProcessedMarkers []ProcessedMarker
}

// OutboxSeqRecord is one exported entry of the per-(source zone, hashed
// sender) monotonic sequence counter (OutboxSeqPrefix).
type OutboxSeqRecord struct {
	// SourceZone is the zone half of the counter's key.
	SourceZone ZoneID
	// SenderKey is the already-hashed 32-byte sender key (SenderKeyLen), the
	// exact form OutboxSeqKey stores under -- never the raw sender identity.
	SenderKey []byte
	// Seq is the last-used sequence number for this (zone, sender) pair.
	Seq uint64
}

// Validate checks the record's structural invariants.
func (r OutboxSeqRecord) Validate() error {
	if err := r.SourceZone.Validate(); err != nil {
		return fmt.Errorf("%w: outbox sequence source zone: %s", ErrInvalidGenesis, err)
	}
	if len(r.SenderKey) != SenderKeyLen {
		return fmt.Errorf("%w: outbox sequence sender key must be %d bytes, got %d", ErrInvalidGenesis, SenderKeyLen, len(r.SenderKey))
	}
	return nil
}

// senderKeyArray copies a validated 32-byte slice into the fixed-size array
// the key-builder functions (OutboxSeqKey) require.
func senderKeyArray(b []byte) [SenderKeyLen]byte {
	var out [SenderKeyLen]byte
	copy(out[:], b)
	return out
}

// DefaultGenesis returns the Phase 1 genesis: prototype-disabled params, all 256
// buckets on the Core Zone, and one descriptor per zone. The message-bus
// collections are empty: Phase 4 never runs under the single-zone genesis
// table (aez.md §6's inertness proof).
func DefaultGenesis() GenesisState {
	zones := make([]Zone, 0, ZoneCount)
	for _, id := range AllZoneIDs() {
		zones = append(zones, NewZone(id))
	}
	return GenesisState{
		Params:		DefaultParams(),
		RoutingTable:	GenesisRoutingTable(),
		Zones:		zones,
	}
}

// Validate checks params, the routing table, and the zone descriptor set.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidGenesis, err)
	}
	if err := gs.RoutingTable.Validate(); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidGenesis, err)
	}
	if uint32(len(gs.Zones)) != ZoneCount {
		return fmt.Errorf("%w: genesis must declare exactly %d zones, got %d", ErrInvalidGenesis, ZoneCount, len(gs.Zones))
	}
	seen := make(map[ZoneID]bool, len(gs.Zones))
	for _, zone := range gs.Zones {
		if err := zone.Validate(); err != nil {
			return fmt.Errorf("%w: %s", ErrInvalidGenesis, err)
		}
		if seen[zone.ID] {
			return fmt.Errorf("%w: duplicate zone %d", ErrInvalidGenesis, uint32(zone.ID))
		}
		seen[zone.ID] = true
	}
	for _, id := range AllZoneIDs() {
		if !seen[id] {
			return fmt.Errorf("%w: genesis is missing zone %d", ErrInvalidGenesis, uint32(id))
		}
	}
	if err := validateOutboxMessages(gs.PendingOutboxMessages); err != nil {
		return err
	}
	if err := validateOutboxSequences(gs.OutboxSequences); err != nil {
		return err
	}
	if err := validateInboxMessages(gs.PendingInboxMessages); err != nil {
		return err
	}
	if err := validateProcessedMarkers(gs.ProcessedMarkers); err != nil {
		return err
	}
	return nil
}

// validateOutboxMessages checks each pending outbox message's own structural
// invariants, requires a stamped id (a genuine emission always has one), and
// rejects a duplicate (SourceZone, Sender, SourceSeq) -- the exact tuple
// OutboxKey keys on, so a duplicate here is two entries that would collide
// into the same store key on Init.
func validateOutboxMessages(msgs []ZoneMessage) error {
	seen := make(map[string]bool, len(msgs))
	for i, m := range msgs {
		if err := m.Validate(); err != nil {
			return fmt.Errorf("%w: pending outbox message %d: %s", ErrInvalidGenesis, i, err)
		}
		if len(m.ID) == 0 {
			return fmt.Errorf("%w: pending outbox message %d has no id", ErrInvalidGenesis, i)
		}
		key := string(OutboxKey(m.SourceZone, SenderKey(m.Sender), m.SourceSeq))
		if seen[key] {
			return fmt.Errorf("%w: duplicate pending outbox message for zone %d seq %d", ErrInvalidGenesis, uint32(m.SourceZone), m.SourceSeq)
		}
		seen[key] = true
	}
	return nil
}

// validateInboxMessages mirrors validateOutboxMessages for the delivery
// queue: duplicate detection keys on (DeliverHeight, ID), the InboxKey tuple.
func validateInboxMessages(msgs []ZoneMessage) error {
	seen := make(map[string]bool, len(msgs))
	for i, m := range msgs {
		if err := m.Validate(); err != nil {
			return fmt.Errorf("%w: pending inbox message %d: %s", ErrInvalidGenesis, i, err)
		}
		if len(m.ID) == 0 {
			return fmt.Errorf("%w: pending inbox message %d has no id", ErrInvalidGenesis, i)
		}
		key := string(InboxKey(m.DeliverHeight, m.ID))
		if seen[key] {
			return fmt.Errorf("%w: duplicate pending inbox message at height %d", ErrInvalidGenesis, m.DeliverHeight)
		}
		seen[key] = true
	}
	return nil
}

// validateOutboxSequences rejects a malformed record or a duplicate
// (SourceZone, SenderKey) pair -- a fresh keeper can hold only one counter
// per pair.
func validateOutboxSequences(recs []OutboxSeqRecord) error {
	seen := make(map[string]bool, len(recs))
	for i, r := range recs {
		if err := r.Validate(); err != nil {
			return fmt.Errorf("%w: outbox sequence %d: %s", ErrInvalidGenesis, i, err)
		}
		key := string(OutboxSeqKey(r.SourceZone, senderKeyArray(r.SenderKey)))
		if seen[key] {
			return fmt.Errorf("%w: duplicate outbox sequence for zone %d", ErrInvalidGenesis, uint32(r.SourceZone))
		}
		seen[key] = true
	}
	return nil
}

// validateProcessedMarkers rejects a malformed marker or a duplicate message
// id -- ProcessedKey is keyed by id alone, so two markers for the same id
// would collide into one store key on Init.
func validateProcessedMarkers(markers []ProcessedMarker) error {
	seen := make(map[string]bool, len(markers))
	for i, m := range markers {
		if err := m.Validate(); err != nil {
			return fmt.Errorf("%w: processed marker %d: %s", ErrInvalidGenesis, i, err)
		}
		key := string(ProcessedKey(m.MessageID))
		if seen[key] {
			return fmt.Errorf("%w: duplicate processed marker", ErrInvalidGenesis)
		}
		seen[key] = true
	}
	return nil
}

// SortCollections orders each of the four message-bus collections into their
// canonical on-disk key order (I-22). ExportGenesisState calls this so a
// caller never has to depend on scan/collection construction happening to
// preserve that order already.
func (gs *GenesisState) SortCollections() {
	sort.Slice(gs.PendingOutboxMessages, func(i, j int) bool {
		a, b := gs.PendingOutboxMessages[i], gs.PendingOutboxMessages[j]
		return bytes.Compare(
			OutboxKey(a.SourceZone, SenderKey(a.Sender), a.SourceSeq),
			OutboxKey(b.SourceZone, SenderKey(b.Sender), b.SourceSeq),
		) < 0
	})
	sort.Slice(gs.OutboxSequences, func(i, j int) bool {
		a, b := gs.OutboxSequences[i], gs.OutboxSequences[j]
		return bytes.Compare(
			OutboxSeqKey(a.SourceZone, senderKeyArray(a.SenderKey)),
			OutboxSeqKey(b.SourceZone, senderKeyArray(b.SenderKey)),
		) < 0
	})
	sort.Slice(gs.PendingInboxMessages, func(i, j int) bool {
		a, b := gs.PendingInboxMessages[i], gs.PendingInboxMessages[j]
		return bytes.Compare(InboxKey(a.DeliverHeight, a.ID), InboxKey(b.DeliverHeight, b.ID)) < 0
	})
	sort.Slice(gs.ProcessedMarkers, func(i, j int) bool {
		return bytes.Compare(
			ProcessedKey(gs.ProcessedMarkers[i].MessageID),
			ProcessedKey(gs.ProcessedMarkers[j].MessageID),
		) < 0
	})
}

// IsCoreOnly reports whether every one of the BucketCount buckets maps to the
// Core Zone -- i.e. whether this genesis is the purely-additive Phase 1 shape.
//
// This is deliberately NOT enforced by Validate. "All buckets on zone 0" is a
// property of the genesis x/aez SHIPS (DefaultGenesis, asserted by
// genesis_test.go), not a structural requirement of a well-formed genesis: a
// table with elastic assignments is perfectly valid and is exactly what later
// phases will ship. Conflating the two would make Validate reject a legitimate
// future genesis. What protects the Core Zone is the CorePinned short-circuit,
// which no table version can express its way around (I-9), not a genesis check.
func (gs GenesisState) IsCoreOnly() bool {
	for i := 0; i < BucketCount; i++ {
		if !gs.RoutingTable.Buckets[i].IsCore() {
			return false
		}
	}
	return true
}
