package types

import "encoding/binary"

const (
	// ModuleName is the x/aez module name. StoreKey is deliberately equal to
	// it: app/aetra_core_wiring.go:18-25 pairs prototypeModuleNames[i] with
	// prototypeStoreKeys[i] POSITIONALLY, and keeping the two strings equal
	// makes a mis-pairing impossible to express.
	ModuleName	= "aez"
	StoreKey	= ModuleName
)

// Per-entity key layout. There is NO genesis blob key, and there never will be.
//
// x/contracts/keeper/keeper.go:2793-2806 marshals an entire module's state into
// ONE store key. One key is one leaf is one root: it cannot be prefix-split,
// overlaid with a zone prefix, or iterated. That shape is what makes per-zone
// state impossible, and x/aez is the module that must never reintroduce it
// (I-20).
//
// Each prefix below holds exactly one entity per key, so every traversal is a
// byte-ordered range scan -- deterministic without a sort (I-22).
var (
	// ParamsKey holds Params. A single small struct is legitimately one key:
	// it is not a collection, so it cannot grow unboundedly and it can never
	// need a per-zone overlay.
	ParamsKey	= []byte{0x01}

	// RoutingTableCurrentKey holds the current table version (uint64 BE).
	RoutingTableCurrentKey	= []byte{0x02}

	// RoutingTablePendingKey holds the pending table version (uint64 BE),
	// activated at that table's ActivationHeight.
	RoutingTablePendingKey	= []byte{0x03}

	// RoutingTableVersionPrefix holds one RoutingTable per version:
	//   0x04 || version_be8 -> RoutingTable
	RoutingTableVersionPrefix	= []byte{0x04}

	// ZonePrefix holds one Zone descriptor per zone:
	//   0x05 || zone_id_be4 -> Zone
	ZonePrefix	= []byte{0x05}

	// --- Phase 4 message bus. Four new prefixes, all inside the ALREADY-mounted
	// aez store: no new store key, no MountKVStores change. Every prefix is
	// empty at genesis (written only by a runtime branch that never runs under
	// one zone), so the exported genesis is byte-identical to Phase 2's. ---

	// OutboxPrefix holds one ZoneMessage per source emission (a source-side
	// audit log, pruned on terminal state, I-21):
	//   0x06 || src_zone_be4 || sender_key_32 || src_seq_be8 -> ZoneMessage
	OutboxPrefix	= []byte{0x06}

	// OutboxSeqPrefix holds the per-(src_zone, sender) monotonic counter. This
	// is the non-caller-settable sequence that makes message ids collision-free
	// (aez.md §4.6, Gap C):
	//   0x07 || src_zone_be4 || sender_key_32 -> uint64 BE (last used seq)
	OutboxSeqPrefix	= []byte{0x07}

	// InboxPrefix holds the authoritative delivery queue, keyed HEIGHT-FIRST so
	// "everything deliverable at height H" is one bounded ascending range scan
	// and nothing durable is filed under a dst_zone the recipient can leave
	// across a rezone (the re-resolution rule):
	//   0x08 || deliver_height_be8 || message_id_32 -> ZoneMessage
	InboxPrefix	= []byte{0x08}

	// ProcessedPrefix holds the committed exactly-once marker set, keyed by
	// message id ALONE (not (dst_zone, id)) so a rezoned message cannot deliver
	// once per zone:
	//   0x09 || message_id_32 -> ProcessedMarker
	ProcessedPrefix	= []byte{0x09}

	// InboxCountKey holds the current inbox depth as a uint64 BE. It is a single
	// small counter (legitimately one key, like ParamsKey) that makes the
	// bounded-queue check (I-21) O(1) instead of an O(n) scan on every enqueue.
	// Incremented on inbox write, decremented on inbox delete; never negative.
	InboxCountKey	= []byte{0x0a}
)

// SenderKeyLen is the fixed width of a hashed sender key (SenderKey).
const SenderKeyLen = 32

// OutboxSeqKey is the counter key for one (src_zone, sender) pair.
func OutboxSeqKey(srcZone ZoneID, senderKey [SenderKeyLen]byte) []byte {
	out := make([]byte, 0, len(OutboxSeqPrefix)+4+SenderKeyLen)
	out = append(out, OutboxSeqPrefix...)
	var z [4]byte
	binary.BigEndian.PutUint32(z[:], uint32(srcZone))
	out = append(out, z[:]...)
	return append(out, senderKey[:]...)
}

// OutboxKey is the audit-log key for one emitted message.
func OutboxKey(srcZone ZoneID, senderKey [SenderKeyLen]byte, seq uint64) []byte {
	out := make([]byte, 0, len(OutboxPrefix)+4+SenderKeyLen+8)
	out = append(out, OutboxPrefix...)
	var z [4]byte
	binary.BigEndian.PutUint32(z[:], uint32(srcZone))
	out = append(out, z[:]...)
	out = append(out, senderKey[:]...)
	var s [8]byte
	binary.BigEndian.PutUint64(s[:], seq)
	return append(out, s[:]...)
}

// InboxKey is the delivery-queue key for one message. deliver_height leads so
// key order equals delivery order.
func InboxKey(deliverHeight int64, messageID []byte) []byte {
	out := make([]byte, 0, len(InboxPrefix)+8+len(messageID))
	out = append(out, InboxPrefix...)
	var h [8]byte
	binary.BigEndian.PutUint64(h[:], uint64(deliverHeight))
	out = append(out, h[:]...)
	return append(out, messageID...)
}

// InboxDueEnd returns the exclusive upper bound of the inbox range that is due
// at height H: every key with deliver_height <= H. It is 0x08 || be8(H+1), which
// excludes deliver_height == H+1 and above (I-12: nothing produced this block or
// later can be delivered this block).
func InboxDueEnd(height int64) []byte {
	out := make([]byte, 0, len(InboxPrefix)+8)
	out = append(out, InboxPrefix...)
	var h [8]byte
	binary.BigEndian.PutUint64(h[:], uint64(height)+1)
	return append(out, h[:]...)
}

// InboxScanStart returns the inclusive lower bound of the whole inbox range.
func InboxScanStart() []byte {
	out := make([]byte, 0, len(InboxPrefix)+8)
	out = append(out, InboxPrefix...)
	var h [8]byte
	// be8(0) -- the earliest possible deliver height.
	out = append(out, h[:]...)
	return out
}

// ProcessedKey is the exactly-once marker key for a message id.
func ProcessedKey(messageID []byte) []byte {
	out := make([]byte, 0, len(ProcessedPrefix)+len(messageID))
	out = append(out, ProcessedPrefix...)
	return append(out, messageID...)
}

// RoutingTableVersionKey returns the per-entity key for one routing table
// version. Big-endian so lexicographic key order equals numeric version order.
func RoutingTableVersionKey(version uint64) []byte {
	out := make([]byte, 0, len(RoutingTableVersionPrefix)+8)
	out = append(out, RoutingTableVersionPrefix...)
	var scratch [8]byte
	binary.BigEndian.PutUint64(scratch[:], version)
	return append(out, scratch[:]...)
}

// ZoneKey returns the per-entity key for one zone descriptor.
func ZoneKey(zone ZoneID) []byte {
	out := make([]byte, 0, len(ZonePrefix)+4)
	out = append(out, ZonePrefix...)
	var scratch [4]byte
	binary.BigEndian.PutUint32(scratch[:], uint32(zone))
	return append(out, scratch[:]...)
}

// EncodeUint64 encodes a uint64 big-endian.
func EncodeUint64(value uint64) []byte {
	var scratch [8]byte
	binary.BigEndian.PutUint64(scratch[:], value)
	return scratch[:]
}

// DecodeUint64 decodes a big-endian uint64, reporting whether the input was
// exactly 8 bytes.
func DecodeUint64(bz []byte) (uint64, bool) {
	if len(bz) != 8 {
		return 0, false
	}
	return binary.BigEndian.Uint64(bz), true
}
