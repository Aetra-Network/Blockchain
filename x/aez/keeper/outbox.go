package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sovereign-l1/l1/x/aez/types"
)

// EnqueueRequest describes a cross-zone send. Sender and Recipient are given as
// (kind, entity) pairs and classified through CanonicalEntityID -- the SAME
// classification that owns the system-first ordering enforcing I-10, so a caller
// cannot route a module account through the native-account path.
type EnqueueRequest struct {
	SenderKind     types.EntityKind
	Sender         any
	RecipientKind  types.EntityKind
	Recipient      any
	Payload        []byte
	Funds          uint64
	GasLimit       uint64
	DeadlineHeight int64
}

// EnqueueMessage is the source-side of the bus. It resolves the sender's and
// recipient's zones and, ONLY if they differ and the module is enabled, allocates
// a monotonic source sequence, stamps a deterministic id, and writes the message
// to the source outbox (audit log) and the destination inbox scheduled for
// delivery at QueuedHeight+1 (I-12).
//
// INERTNESS (the Phase 4a guarantee). If sender and recipient share a zone --
// which, under the genesis table with all 256 buckets on zone 0, is ALWAYS the
// case for every pair -- this produces NOTHING and returns (produced=false). With
// no message ever produced, the inbox stays empty and the BeginBlock drain is a
// no-op, so single-zone behaviour is bit-identical. The Enabled flag is a second,
// independent guard: a disabled module produces nothing regardless of zones.
//
// It reports the message it wrote and whether it wrote one.
func (k Keeper) EnqueueMessage(ctx context.Context, req EnqueueRequest) (types.ZoneMessage, bool, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return types.ZoneMessage{}, false, err
	}
	// Guard 1 (flag): a disabled x/aez never produces a message (I-23).
	if !params.Prototype.Enabled {
		return types.ZoneMessage{}, false, nil
	}

	senderNS, senderID, err := types.CanonicalEntityID(req.SenderKind, req.Sender)
	if err != nil {
		return types.ZoneMessage{}, false, err
	}
	recipientNS, recipientID, err := types.CanonicalEntityID(req.RecipientKind, req.Recipient)
	if err != nil {
		return types.ZoneMessage{}, false, err
	}
	srcZone, err := k.ZoneOf(ctx, senderNS, senderID)
	if err != nil {
		return types.ZoneMessage{}, false, err
	}
	dstZone, err := k.ZoneOf(ctx, recipientNS, recipientID)
	if err != nil {
		return types.ZoneMessage{}, false, err
	}
	// Guard 2 (structural): same-zone interaction is a local operation, not a
	// cross-zone message. This is the branch that is UNREACHABLE while one zone
	// is populated -- the inertness proof (aez.md §6).
	if srcZone == dstZone {
		return types.ZoneMessage{}, false, nil
	}
	// Phase 4a moves messages, never money. A value leg needs a Core-Zone
	// x/bank escrow x/aez must not hold (I-10); that is Phase 4b.
	if req.Funds != 0 {
		return types.ZoneMessage{}, false, types.ErrValueTransferUnsupported
	}

	height := sdk.UnwrapSDKContext(ctx).BlockHeight()
	seq, err := k.nextSourceSequence(ctx, srcZone, senderID)
	if err != nil {
		return types.ZoneMessage{}, false, err
	}
	msg := types.ZoneMessage{
		SourceZone:        srcZone,
		DestZoneAtEnqueue: dstZone,
		SourceSeq:         seq,
		SenderNS:          senderNS,
		Sender:            senderID,
		RecipientNS:       recipientNS,
		Recipient:         recipientID,
		Payload:           req.Payload,
		Funds:             0,
		GasLimit:          req.GasLimit,
		Kind:              types.MessageKindNormal,
		DeadlineHeight:    req.DeadlineHeight,
		QueuedHeight:      height,
		DeliverHeight:     height + 1,
	}
	msg = msg.WithComputedID()
	if err := k.writeEnqueued(ctx, msg); err != nil {
		return types.ZoneMessage{}, false, err
	}
	return msg, true, nil
}

// writeEnqueued commits a fully-formed message to both the source outbox and the
// destination inbox, enforcing the bounded-queue cap at the inbox (I-21). It is
// shared by EnqueueMessage (NORMAL) and the drain's bounce producer.
func (k Keeper) writeEnqueued(ctx context.Context, msg types.ZoneMessage) error {
	if err := msg.Validate(); err != nil {
		return err
	}
	count, err := k.inboxCount(ctx)
	if err != nil {
		return err
	}
	if count >= types.MaxZoneMessageQueueDepth {
		return fmt.Errorf("%w: depth %d", types.ErrQueueFull, count)
	}
	if err := k.setOutbox(ctx, msg); err != nil {
		return err
	}
	return k.putInbox(ctx, msg)
}

// nextSourceSequence allocates the next monotonic sequence for a (src_zone,
// sender) pair. The counter is stored and keeper-owned: a caller cannot set it,
// which is exactly what forbids the id collision LogicalTime permits in
// x/contracts (aez.md §4.6, Gap C). First message gets seq 1.
func (k Keeper) nextSourceSequence(ctx context.Context, srcZone types.ZoneID, sender []byte) (uint64, error) {
	key := types.OutboxSeqKey(srcZone, types.SenderKey(sender))
	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get(key)
	if err != nil {
		return 0, err
	}
	var last uint64
	if len(bz) != 0 {
		v, ok := types.DecodeUint64(bz)
		if !ok {
			return 0, fmt.Errorf("aez outbox sequence for zone %d is corrupt", uint32(srcZone))
		}
		last = v
	}
	next := last + 1
	if err := store.Set(key, types.EncodeUint64(next)); err != nil {
		return 0, err
	}
	return next, nil
}

// setOutbox writes the source-side audit record for a message.
func (k Keeper) setOutbox(ctx context.Context, msg types.ZoneMessage) error {
	key := types.OutboxKey(msg.SourceZone, types.SenderKey(msg.Sender), msg.SourceSeq)
	return k.setJSON(ctx, key, msg)
}

// pruneOutbox deletes the source-side audit record once a message is terminal,
// keeping the outbox bounded (I-21).
func (k Keeper) pruneOutbox(ctx context.Context, msg types.ZoneMessage) error {
	key := types.OutboxKey(msg.SourceZone, types.SenderKey(msg.Sender), msg.SourceSeq)
	store := k.storeService.OpenKVStore(ctx)
	return store.Delete(key)
}

// GetOutboxMessage reads one source-side audit record (for tests and queries).
func (k Keeper) GetOutboxMessage(ctx context.Context, srcZone types.ZoneID, sender []byte, seq uint64) (types.ZoneMessage, bool, error) {
	key := types.OutboxKey(srcZone, types.SenderKey(sender), seq)
	var msg types.ZoneMessage
	found, err := k.getJSON(ctx, key, &msg)
	if err != nil {
		return types.ZoneMessage{}, false, err
	}
	return msg, found, nil
}
