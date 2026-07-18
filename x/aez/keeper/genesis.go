package keeper

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/sovereign-l1/l1/x/aez/types"
)

// DefaultGenesis returns the module's default genesis state.
func DefaultGenesis() types.GenesisState {
	return types.DefaultGenesis()
}

// InitGenesisState writes genesis into the store as PER-ENTITY keys: params
// under its own key, one key per zone descriptor, the routing table under its
// own version key plus a current pointer, and -- the Phase 4 message bus --
// one write per pending outbox message, outbox sequence counter, pending
// inbox message, and processed marker, each through the SAME low-level
// writer the runtime bus uses (setOutbox/putInbox/setProcessed), so a
// restored keeper's stores are indistinguishable from one that reached the
// same state by running the bus live.
//
// Nothing is retained in RAM. There is no assignGenesis step, because there is
// no k.genesis field to assign to (I-20).
func (k Keeper) InitGenesisState(ctx context.Context, gs types.GenesisState) error {
	if err := gs.Validate(); err != nil {
		return err
	}
	if err := k.SetParams(ctx, gs.Params); err != nil {
		return err
	}
	for _, zone := range gs.Zones {
		if err := k.SetZone(ctx, zone); err != nil {
			return err
		}
	}
	if err := k.InitRoutingTable(ctx, gs.RoutingTable); err != nil {
		return err
	}
	for _, msg := range gs.PendingOutboxMessages {
		if err := k.setOutbox(ctx, msg); err != nil {
			return err
		}
	}
	for _, rec := range gs.OutboxSequences {
		if err := k.initOutboxSequence(ctx, rec); err != nil {
			return err
		}
	}
	for _, msg := range gs.PendingInboxMessages {
		if err := k.putInbox(ctx, msg); err != nil {
			return err
		}
	}
	for _, marker := range gs.ProcessedMarkers {
		if err := k.setProcessed(ctx, marker); err != nil {
			return err
		}
	}
	return nil
}

// initOutboxSequence writes one exported per-(zone, hashed sender) counter
// directly under its OutboxSeqKey. It is genesis-only: the runtime path
// (nextSourceSequence) always reads-then-increments, but a genesis restore is
// installing an already-allocated counter value verbatim, not allocating a new
// one.
func (k Keeper) initOutboxSequence(ctx context.Context, rec types.OutboxSeqRecord) error {
	if err := rec.Validate(); err != nil {
		return err
	}
	var senderKey [types.SenderKeyLen]byte
	copy(senderKey[:], rec.SenderKey)
	key := types.OutboxSeqKey(rec.SourceZone, senderKey)
	store := k.storeService.OpenKVStore(ctx)
	return store.Set(key, types.EncodeUint64(rec.Seq))
}

// ExportGenesisState reads genesis back OUT OF THE STORE.
//
// Every value is read from committed state, never from a cached struct and never
// gated on a reflect.DeepEqual comparison against DefaultGenesis(). An export
// that reads RAM exports whatever the process happened to be holding, which is
// how a restarted node and a running node produce different exports from the
// same committed state.
//
// The four message-bus collections are read by a full-prefix scan each (the
// same Iterator pattern scanDueInbox uses, generalized from a height-bounded
// range to the whole prefix), so an export mid-flight -- a message enqueued
// but not yet drained -- carries it forward instead of silently dropping it.
func (k Keeper) ExportGenesisState(ctx context.Context) (types.GenesisState, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return types.GenesisState{}, err
	}
	zones, err := k.GetAllZones(ctx)
	if err != nil {
		return types.GenesisState{}, err
	}
	table, err := k.GetRoutingTable(ctx)
	if err != nil {
		return types.GenesisState{}, fmt.Errorf("failed to export aez routing table: %w", err)
	}
	outboxMessages, err := k.exportOutboxMessages(ctx)
	if err != nil {
		return types.GenesisState{}, fmt.Errorf("failed to export aez outbox: %w", err)
	}
	outboxSequences, err := k.exportOutboxSequences(ctx)
	if err != nil {
		return types.GenesisState{}, fmt.Errorf("failed to export aez outbox sequences: %w", err)
	}
	inboxMessages, err := k.exportInboxMessages(ctx)
	if err != nil {
		return types.GenesisState{}, fmt.Errorf("failed to export aez inbox: %w", err)
	}
	processedMarkers, err := k.exportProcessedMarkers(ctx)
	if err != nil {
		return types.GenesisState{}, fmt.Errorf("failed to export aez processed markers: %w", err)
	}

	gs := types.GenesisState{
		Params:			params,
		RoutingTable:		table,
		Zones:			zones,
		PendingOutboxMessages:	outboxMessages,
		OutboxSequences:	outboxSequences,
		PendingInboxMessages:	inboxMessages,
		ProcessedMarkers:	processedMarkers,
	}
	gs.SortCollections()
	return gs, nil
}

// prefixRangeEnd returns the exclusive upper bound of the whole key range
// starting with prefix: prefix with its last byte incremented, carrying into
// preceding bytes on overflow (nil if prefix is all 0xff, meaning "no upper
// bound" -- unreachable for the single-byte prefixes x/aez uses today).
func prefixRangeEnd(prefix []byte) []byte {
	end := append([]byte(nil), prefix...)
	for i := len(end) - 1; i >= 0; i-- {
		end[i]++
		if end[i] != 0 {
			return end[:i+1]
		}
	}
	return nil
}

// exportOutboxMessages reads every source-side audit record (OutboxPrefix) in
// ascending key order.
func (k Keeper) exportOutboxMessages(ctx context.Context) ([]types.ZoneMessage, error) {
	store := k.storeService.OpenKVStore(ctx)
	it, err := store.Iterator(types.OutboxPrefix, prefixRangeEnd(types.OutboxPrefix))
	if err != nil {
		return nil, err
	}
	defer it.Close()

	// var, not make(..., 0): an empty scan must stay nil so an untouched
	// (Phase 4-inert) genesis is byte-identical to DefaultGenesis's zero
	// value, not merely equal-length-zero to it.
	var out []types.ZoneMessage
	for ; it.Valid(); it.Next() {
		var msg types.ZoneMessage
		if err := decodeJSON(it.Value(), &msg); err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
	return out, nil
}

// exportOutboxSequences reads every per-(zone, hashed sender) counter
// (OutboxSeqPrefix) in ascending key order. Unlike the outbox/inbox/processed
// scans, the zone and sender key are not present in the stored VALUE (a bare
// uint64), so they are decoded back out of the KEY itself.
func (k Keeper) exportOutboxSequences(ctx context.Context) ([]types.OutboxSeqRecord, error) {
	store := k.storeService.OpenKVStore(ctx)
	it, err := store.Iterator(types.OutboxSeqPrefix, prefixRangeEnd(types.OutboxSeqPrefix))
	if err != nil {
		return nil, err
	}
	defer it.Close()

	const wantKeyLen = 1 + 4 + types.SenderKeyLen // prefix || zone_be4 || sender_key_32
	var out []types.OutboxSeqRecord
	for ; it.Valid(); it.Next() {
		key := it.Key()
		if len(key) != wantKeyLen {
			return nil, fmt.Errorf("aez outbox sequence key has unexpected length %d, want %d", len(key), wantKeyLen)
		}
		zone := types.ZoneID(binary.BigEndian.Uint32(key[1:5]))
		senderKey := append([]byte(nil), key[5:]...)
		seq, ok := types.DecodeUint64(it.Value())
		if !ok {
			return nil, fmt.Errorf("aez outbox sequence value for zone %d is corrupt", uint32(zone))
		}
		out = append(out, types.OutboxSeqRecord{SourceZone: zone, SenderKey: senderKey, Seq: seq})
	}
	return out, nil
}

// exportInboxMessages reads the FULL delivery queue (InboxPrefix), not the
// height-bounded due subset scanDueInbox reads for a live drain: genesis must
// carry forward every message still queued, whatever its deliver height.
func (k Keeper) exportInboxMessages(ctx context.Context) ([]types.ZoneMessage, error) {
	store := k.storeService.OpenKVStore(ctx)
	it, err := store.Iterator(types.InboxScanStart(), prefixRangeEnd(types.InboxPrefix))
	if err != nil {
		return nil, err
	}
	defer it.Close()

	var out []types.ZoneMessage
	for ; it.Valid(); it.Next() {
		var msg types.ZoneMessage
		if err := decodeJSON(it.Value(), &msg); err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
	return out, nil
}

// exportProcessedMarkers reads every committed exactly-once marker
// (ProcessedPrefix) in ascending key (message id) order.
func (k Keeper) exportProcessedMarkers(ctx context.Context) ([]types.ProcessedMarker, error) {
	store := k.storeService.OpenKVStore(ctx)
	it, err := store.Iterator(types.ProcessedPrefix, prefixRangeEnd(types.ProcessedPrefix))
	if err != nil {
		return nil, err
	}
	defer it.Close()

	var out []types.ProcessedMarker
	for ; it.Valid(); it.Next() {
		var marker types.ProcessedMarker
		if err := decodeJSON(it.Value(), &marker); err != nil {
			return nil, err
		}
		out = append(out, marker)
	}
	return out, nil
}
