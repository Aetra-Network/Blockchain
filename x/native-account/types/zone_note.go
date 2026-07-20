package types

import (
	"errors"
)

const (
	MinZoneNoteBytes = 1
	MaxZoneNoteBytes = 512

	// MaxAccountZoneNotes bounds per-recipient retained history (an I-21-style
	// boundedness rule, mirroring x/aez's MaxZoneMessageQueueDepth bounded-queue
	// discipline): the oldest note is evicted on overflow, from both
	// ZoneNotePrefix and ZoneNoteIndexPrefix.
	MaxAccountZoneNotes = 128

	// ZoneNoteGasLimit is the delivery gas budget SendZoneNote enqueues with,
	// well under x/aez/types.MaxGasPerDelivery (1,000,000).
	//
	// This is a flat, conservative ESTIMATE, not a measurement: unlike
	// x/contracts (whose declared gas is tied to real AVM execution cost, per
	// docs/architecture/aez.md), nothing in x/aez's drain budget meters a
	// Deliverer's actual per-call KV/CPU cost. DeliverAddressMessage does one
	// AccountByRaw read plus storeZoneNote's up to ~7 KV ops (2 reads, up to 4
	// writes, an optional 1-read-2-delete eviction) -- real, non-trivial,
	// state-mutating work that x/aez's pre-Phase-3 no-op deliverMessage never
	// had. 50,000 leaves headroom above that real cost while still bounding a
	// same-block burst to a small fraction of TotalMessageGasPerBlock; it is
	// not derived from a per-op gas schedule because this module has none.
	// Real per-op metering is a natural follow-up, not required for Phase 3's
	// send/deliver/isolate guarantees.
	ZoneNoteGasLimit = 50_000
)

var (
	ErrZoneMessagingUnavailable	= errors.New("native account: cross-zone messaging is not wired on this node")
	ErrZoneNoteRecipientReserved	= errors.New("native account: zone note recipient must not be a reserved system address")
	ErrZoneNoteBadLength		= errors.New("native account: zone note payload length out of bounds")
	ErrZoneNoteRecipientUnusable	= errors.New("native account: zone note recipient account cannot receive notes")
)

// ZoneNoteRecord is a delivered cross-zone note (Phase 3): an immutable
// historical fact about one message that crossed AEZ zones and landed on a
// native-account recipient. It is deliberately non-monetary -- Note is an
// opaque byte payload, never an amount -- see x/aez/keeper/outbox.go's
// Funds != 0 rejection and SendAddressMessage's hardcoded Funds: 0, which
// together make this record type structurally incapable of representing
// value even if a future caller tried.
type ZoneNoteRecord struct {
	// MessageID is the AEZ ZoneMessage.ID (32 bytes), tying this record back
	// to the source-side audit trail on x/aez's own bus.
	MessageID	[]byte	`protobuf:"bytes,1,opt,name=message_id,json=messageId,proto3" json:"message_id,omitempty"`
	// SourceZone is the zone the SENDER resolved to at enqueue time
	// (ZoneMessage.SourceZone), never rewritten.
	SourceZone	uint32	`protobuf:"varint,2,opt,name=source_zone,json=sourceZone,proto3" json:"source_zone,omitempty"`
	// SenderRaw is the raw ("ae1...") form of the sending account.
	SenderRaw	string	`protobuf:"bytes,3,opt,name=sender_raw,json=senderRaw,proto3" json:"sender_raw,omitempty"`
	// Note is the opaque, non-monetary payload. MinZoneNoteBytes..MaxZoneNoteBytes.
	Note		[]byte	`protobuf:"bytes,4,opt,name=note,proto3" json:"note,omitempty"`
	QueuedHeight	int64	`protobuf:"varint,5,opt,name=queued_height,json=queuedHeight,proto3" json:"queued_height,omitempty"`
	DeliverHeight	int64	`protobuf:"varint,6,opt,name=deliver_height,json=deliverHeight,proto3" json:"deliver_height,omitempty"`
}

// MsgSendZoneNote sends a non-monetary note from one native-account entity
// to another, crossing AEZ zones if sender and recipient resolve to
// different ones. There is no numeric amount/balance field anywhere on this
// message -- Note is opaque bytes -- so there is no wire-format path from
// this Msg to a coin, independent of any keeper-side guard.
type MsgSendZoneNote struct {
	AccountUser	string	`protobuf:"bytes,1,opt,name=account_user,json=accountUser,proto3" json:"account_user,omitempty"`
	RecipientUser	string	`protobuf:"bytes,2,opt,name=recipient_user,json=recipientUser,proto3" json:"recipient_user,omitempty"`
	// Note's descriptor/tag name is "zone_note", not "note": autocli derives
	// CLI flags from this name, and "--note" collides with the SDK's own
	// reserved memo flag (flags.AddTxFlagsToCmd), which panics the entire
	// tx command tree at construction time. Wire field number (3) and Go
	// field name (Note) are unaffected -- gogoproto's reflection marshal
	// keys off number/wire-type, never off this name string.
	Note		[]byte	`protobuf:"bytes,3,opt,name=zone_note,proto3" json:"zone_note,omitempty"`
}

// MsgSendZoneNoteResponse's SourceZoneResolved/DestZoneResolved distinguish a
// genuine Core-Zone (0) resolution from ZoneOf's silent degrade-to-Core
// fallback (a nil/absent resolver, or a resolution error) -- the same
// Resolved-flag convention AccountZone/zone_resolved already establish
// elsewhere in this module for exactly this "advisory tag with a fallback
// value" shape. Without these, SourceZone=DestZone=0 is ambiguous between
// "both accounts really are in the Core Zone" and "the zone resolver wasn't
// wired", which matters because ZoneMessageSender and ZoneResolver are
// independent With* calls with no coupling enforced anywhere -- production
// (app/keepers.go) always wires both to the same AEZ keeper, but nothing
// in the type system requires that.
type MsgSendZoneNoteResponse struct {
	MessageID		[]byte	`protobuf:"bytes,1,opt,name=message_id,json=messageId,proto3" json:"message_id,omitempty"`
	CrossZone		bool	`protobuf:"varint,2,opt,name=cross_zone,json=crossZone,proto3" json:"cross_zone,omitempty"`
	SourceZone		uint32	`protobuf:"varint,3,opt,name=source_zone,json=sourceZone,proto3" json:"source_zone,omitempty"`
	DestZone		uint32	`protobuf:"varint,4,opt,name=dest_zone,json=destZone,proto3" json:"dest_zone,omitempty"`
	SourceZoneResolved	bool	`protobuf:"varint,5,opt,name=source_zone_resolved,json=sourceZoneResolved,proto3" json:"source_zone_resolved,omitempty"`
	DestZoneResolved	bool	`protobuf:"varint,6,opt,name=dest_zone_resolved,json=destZoneResolved,proto3" json:"dest_zone_resolved,omitempty"`
}
