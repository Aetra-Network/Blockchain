package keeper

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sovereign-l1/l1/x/aez/types"
)

// decodeJSON unmarshals a stored value, matching getJSON's error wrapping. It is
// used when a raw value comes from an iterator rather than a keyed Get.
func decodeJSON(bz []byte, target any) error {
	if err := json.Unmarshal(bz, target); err != nil {
		return fmt.Errorf("failed to unmarshal aez value: %w", err)
	}
	return nil
}

// putInbox writes a message to the delivery queue and bumps the inbox count. The
// key is height-first (types.InboxKey), so a drain at height H is one bounded
// ascending range scan rather than a full-queue walk (I-22).
func (k Keeper) putInbox(ctx context.Context, msg types.ZoneMessage) error {
	if len(msg.ID) == 0 {
		return fmt.Errorf("%w: inbox message has no id", types.ErrInvalidMessage)
	}
	if err := k.setJSON(ctx, types.InboxKey(msg.DeliverHeight, msg.ID), msg); err != nil {
		return err
	}
	return k.adjustInboxCount(ctx, +1)
}

// deleteInbox removes a delivered/terminal message from the queue and decrements
// the inbox count. It takes the stored key parts (deliver height + id) because
// after re-resolution the message's logical destination may differ from where it
// was filed, but its PHYSICAL key never moves (height-first keying).
func (k Keeper) deleteInbox(ctx context.Context, deliverHeight int64, messageID []byte) error {
	store := k.storeService.OpenKVStore(ctx)
	key := types.InboxKey(deliverHeight, messageID)
	found, err := store.Has(key)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	if err := store.Delete(key); err != nil {
		return err
	}
	return k.adjustInboxCount(ctx, -1)
}

// dueInboxMessage pairs a queued message with the physical key parts needed to
// delete it, so the drain never has to reconstruct them from a possibly-mutated
// message.
type dueInboxMessage struct {
	deliverHeight int64
	messageID     []byte
	msg           types.ZoneMessage
}

// scanDueInbox returns, in ascending (deliver_height, message_id) order, every
// message whose deliver_height <= height -- a single bounded range scan over
// [InboxScanStart, InboxDueEnd(height)). This is the snapshot the drain iterates:
// taken up front because delivery mutates the queue (the pattern proven at
// x/contracts/keeper/keeper.go:2224-2228), and because the range excludes
// deliver_height > height, same-block delivery is structurally impossible (I-12).
func (k Keeper) scanDueInbox(ctx context.Context, height int64) ([]dueInboxMessage, error) {
	store := k.storeService.OpenKVStore(ctx)
	start := types.InboxScanStart()
	end := types.InboxDueEnd(height)
	it, err := store.Iterator(start, end)
	if err != nil {
		return nil, err
	}
	defer it.Close()

	out := make([]dueInboxMessage, 0)
	for ; it.Valid(); it.Next() {
		var msg types.ZoneMessage
		if err := decodeJSON(it.Value(), &msg); err != nil {
			return nil, err
		}
		out = append(out, dueInboxMessage{
			deliverHeight: msg.DeliverHeight,
			messageID:     append([]byte(nil), msg.ID...),
			msg:           msg,
		})
	}
	return out, nil
}

// inboxCount reads the current inbox depth.
func (k Keeper) inboxCount(ctx context.Context) (uint64, error) {
	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get(types.InboxCountKey)
	if err != nil {
		return 0, err
	}
	if len(bz) == 0 {
		return 0, nil
	}
	v, ok := types.DecodeUint64(bz)
	if !ok {
		return 0, fmt.Errorf("aez inbox count is corrupt")
	}
	return v, nil
}

// adjustInboxCount applies a signed delta to the inbox depth, never underflowing.
func (k Keeper) adjustInboxCount(ctx context.Context, delta int64) error {
	count, err := k.inboxCount(ctx)
	if err != nil {
		return err
	}
	switch {
	case delta >= 0:
		count += uint64(delta)
	case uint64(-delta) > count:
		count = 0
	default:
		count -= uint64(-delta)
	}
	store := k.storeService.OpenKVStore(ctx)
	return store.Set(types.InboxCountKey, types.EncodeUint64(count))
}

// hasProcessed reports whether a message id is already terminal. This is the
// exactly-once gate: a marker hit is a deterministic reject, replacing the
// dequeue-by-id "missing from queue" ambiguity of x/contracts (aez.md §4.6).
func (k Keeper) hasProcessed(ctx context.Context, messageID []byte) (bool, error) {
	return k.has(ctx, types.ProcessedKey(messageID))
}

// setProcessed writes the committed exactly-once marker. It is written BEFORE
// effects and committed atomically with them in the block's multistore write, so
// no restart can redeliver a marked message (I-13/I-20).
func (k Keeper) setProcessed(ctx context.Context, marker types.ProcessedMarker) error {
	if err := marker.Validate(); err != nil {
		return err
	}
	return k.setJSON(ctx, types.ProcessedKey(marker.MessageID), marker)
}

// GetProcessedMarker reads a marker (for tests and queries).
func (k Keeper) GetProcessedMarker(ctx context.Context, messageID []byte) (types.ProcessedMarker, bool, error) {
	var marker types.ProcessedMarker
	found, err := k.getJSON(ctx, types.ProcessedKey(messageID), &marker)
	if err != nil {
		return types.ProcessedMarker{}, false, err
	}
	return marker, found, nil
}

// resolveRecipientZone RE-RESOLVES a message's destination against the CURRENT
// routing table -- the re-resolution rule (aez.md:551-556). The id commits to WHO
// (recipient), not WHERE, so if the recipient's bucket moved zones across a
// routing epoch the message follows it to the new zone rather than being
// delivered to a stale one or stranded.
func (k Keeper) resolveRecipientZone(ctx context.Context, msg types.ZoneMessage) (types.ZoneID, error) {
	return k.ZoneOf(ctx, msg.RecipientNS, msg.Recipient)
}
