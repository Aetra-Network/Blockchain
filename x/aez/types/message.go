package types

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// MessageDomain is the domain separator for the canonical cross-zone message id.
// It sits alongside BucketDomain and RoutingTableDomain (bucket.go,
// routing_table.go) so a message id can never collide with a bucket preimage or
// a table hash. Changing it changes every message id; it is a consensus constant.
const MessageDomain = "aetra-aez-zone-message-v1"

// SenderKeyDomain domain-separates the fixed-width sender key derived from a
// variable-length sender identity. The key gives the per-(zone,sender) sequence
// counter and the outbox a uniform 32-byte component without a length-prefix
// delimiter hazard; the raw sender still enters the message id (ComputeMessageID)
// so nothing is lost.
const SenderKeyDomain = "aetra-aez-sender-key-v1"

// Bus bounds. These are CONSTANTS, not params, on purpose. x/aez reads no param
// on the per-block drain path (drain.go), so there is no governance-raised value
// a restarted node could hold stale -- the F-17 class the whole module is
// structurally immune to (I-20). A bound that lived in Params would put the drain
// behind a param read and reintroduce that read's failure surface for no benefit
// while the bus is inert.
const (
	// MaxZoneMessageQueueDepth caps the inbox. It mirrors
	// x/contracts/types/api.go:26 (MaxInternalMessageQueueDepth = 65536): the
	// bound is enforced at ENQUEUE, so the queue can only shrink at drain
	// (I-21).
	MaxZoneMessageQueueDepth = 65536

	// MaxBounceDepth caps the bounce/refund lineage. The kind ladder
	// (NORMAL -> BOUNCE -> terminal, drain.go) already forbids a bounce of a
	// bounce, so depth never exceeds 1 in practice; this is the explicit
	// belt-and-suspenders guard the invariant table (I-14) names.
	MaxBounceDepth = 8

	// ZoneMessageGasPerBlock is the per-block drain budget. It is transient
	// accounting recomputed every block and never enters committed state, so it
	// is a constant here rather than committed Params. Phase 6 replaces this
	// single global budget with a per-zone budget plus a Core reservation
	// (aez.md Phase 6); that is deliberately NOT built here.
	ZoneMessageGasPerBlock = uint64(8_000_000)

	// MaxGasPerDelivery clamps a single delivery's charge. It mirrors
	// x/fees MaxTxGas (1,000,000) so one message can never claim the whole
	// block budget.
	MaxGasPerDelivery = uint64(1_000_000)
)

// MessageKind is the closed set of cross-zone message kinds. It is lifted from
// x/mesh/types/types.go:20-26 (which is dead prose; only the shape is reused) and
// retyped into x/aez.
type MessageKind uint8

const (
	// MessageKindNormal is a first-class cross-zone message.
	MessageKindNormal MessageKind = 0

	// MessageKindBounce is the compensating message produced when a NORMAL
	// delivery fails: same lineage, sender/recipient swapped, returned to the
	// original sender. A BOUNCE that itself fails does not bounce again (I-14).
	MessageKindBounce MessageKind = 1

	// MessageKindRefund returns value to a source on failure. It is defined for
	// completeness and for the id ladder, but is LATENT in Phase 4a: x/aez
	// carries no custody and Funds is always 0 here, so nothing produces a
	// refund until the value leg (Phase 4b) lands. See drain.go.
	MessageKindRefund MessageKind = 2
)

// IsKnown reports whether the kind is a member of the closed set.
func (k MessageKind) IsKnown() bool {
	switch k {
	case MessageKindNormal, MessageKindBounce, MessageKindRefund:
		return true
	default:
		return false
	}
}

func (k MessageKind) String() string {
	switch k {
	case MessageKindNormal:
		return "NORMAL"
	case MessageKindBounce:
		return "BOUNCE"
	case MessageKindRefund:
		return "REFUND"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", uint8(k))
	}
}

// ZoneMessage is the cross-zone envelope. It reuses the field shapes of
// x/mesh/types/types.go:76-94 (MeshMessage) retyped to AEZ's numeric ZoneID, with
// the shard/finality/proof/asset fields dropped (there are no shards, one
// consensus, one AppHash, and x/aez moves messages, never money -- I-1/I-10) and
// a stored, non-caller-settable SourceSeq replacing the caller-overridable
// LogicalTime that let x/contracts collide two identical messages (aez.md §4.6).
//
// Sender/Recipient are canonical identity BYTES, never display strings (I-6);
// SenderNS/RecipientNS carry the namespace so delivery can RE-RESOLVE the
// recipient's zone against the then-current table (the re-resolution rule,
// aez.md:551-556) rather than trusting a destination pinned at enqueue.
type ZoneMessage struct {
	// ID is the deterministic message id (ComputeMessageID). 32 bytes.
	ID []byte

	// SourceZone is the zone the sender lived in at enqueue -- an immutable
	// historical fact that enters the id.
	SourceZone ZoneID

	// DestZoneAtEnqueue is the destination resolved at enqueue. It is stored
	// for observability ONLY; delivery re-resolves and does not trust it.
	DestZoneAtEnqueue ZoneID

	// SourceSeq is the per-(SourceZone, Sender) monotonic counter. It is
	// allocated by the keeper and is NOT caller-settable -- the field that
	// structurally forbids the id collision ComputeInternalMessageID permits.
	SourceSeq uint64

	// SenderNS/Sender identify the sender. Sender is canonical bytes.
	SenderNS Namespace
	Sender   []byte

	// RecipientNS/Recipient identify the recipient. Recipient is canonical
	// bytes; RecipientNS drives re-resolution at delivery.
	RecipientNS Namespace
	Recipient   []byte

	// Payload is the raw message body. Its hash (not the raw bytes) enters the
	// id, so the id is stable while the body is delivered verbatim.
	Payload []byte

	// Funds is the value carried. Phase 4a asserts it is 0 at enqueue; the
	// field and its fixed-width id slot are reserved for the value leg (4b).
	Funds uint64

	// GasLimit bounds the delivery's charge against the per-block budget.
	GasLimit uint64

	// Kind is NORMAL / BOUNCE / REFUND.
	Kind MessageKind

	// ParentID is the id of the message this one compensates for; "" (nil) for
	// a NORMAL message. Bounce/refund lineage for MaxBounceDepth (I-14).
	ParentID []byte

	// BounceDepth is 0 for a NORMAL message and parent+1 for a compensating
	// one. Enqueue rejects a depth past MaxBounceDepth (I-14).
	BounceDepth uint32

	// DeadlineHeight, when > 0, is the height past which delivery is Expired.
	DeadlineHeight int64

	// QueuedHeight is the block the message was enqueued at.
	QueuedHeight int64

	// DeliverHeight is the earliest block delivery may occur. Enqueue stamps it
	// >= QueuedHeight+1 (I-12); the drain re-checks it.
	DeliverHeight int64
}

// SenderKey returns the fixed 32-byte key derived from a canonical sender
// identity. Distinct senders map to distinct keys with overwhelming probability;
// the raw sender still enters the message id, so the key is a keying convenience,
// not an identity.
func SenderKey(sender []byte) [32]byte {
	h := sha256.New()
	h.Write([]byte(SenderKeyDomain))
	h.Write([]byte{0})
	h.Write(sender)
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// PayloadHash returns SHA256(payload). A nil payload hashes to the digest of the
// empty string, which is well defined and stable.
func PayloadHash(payload []byte) [32]byte {
	return sha256.Sum256(payload)
}

// ComputeMessageID is the deterministic message id.
//
//	id = SHA256(
//	    MessageDomain || 0x00
//	  || src_zone_be4 || src_seq_be8 || kind_be1
//	  || funds_be16 || gas_limit_be8 || deadline_be8 || queued_be8
//	  || len(sender)_be8 || sender
//	  || len(recipient)_be8 || recipient
//	  || sha256(payload)
//	  || len(parent_id)_be8 || parent_id )
//
// Every variable-length field is length-prefixed with an 8-byte big-endian
// length, because sender/recipient/payload/parent are all variable and
// caller-influenced -- the single-0x00 framing that is injective for ComputeBucket
// (its last field is variable) is NOT injective here (bucket.go:60-70 warns of
// exactly this). funds is a fixed 16-byte slot (a left-padded uint64) reserved so
// the value leg does not have to re-lay the preimage.
//
// dst_zone and deliver_height are deliberately EXCLUDED: the id commits to WHO
// (recipient), not WHERE (ZoneOf(recipient)), so it is stable across a rezone
// (the re-resolution rule). src_seq -- stored, monotonic, keeper-only -- is what
// makes two byte-identical messages from one sender hash to DIFFERENT ids, the
// collision ComputeInternalMessageID permits (aez.md §4.6).
func ComputeMessageID(m ZoneMessage) []byte {
	h := sha256.New()
	h.Write([]byte(MessageDomain))
	h.Write([]byte{0})

	var b8 [8]byte
	var b4 [4]byte

	binary.BigEndian.PutUint32(b4[:], uint32(m.SourceZone))
	h.Write(b4[:])
	binary.BigEndian.PutUint64(b8[:], m.SourceSeq)
	h.Write(b8[:])
	h.Write([]byte{byte(m.Kind)})

	// funds as a 16-byte big-endian slot (8 zero bytes then the uint64).
	var funds16 [16]byte
	binary.BigEndian.PutUint64(funds16[8:], m.Funds)
	h.Write(funds16[:])

	binary.BigEndian.PutUint64(b8[:], m.GasLimit)
	h.Write(b8[:])
	binary.BigEndian.PutUint64(b8[:], uint64(m.DeadlineHeight))
	h.Write(b8[:])
	binary.BigEndian.PutUint64(b8[:], uint64(m.QueuedHeight))
	h.Write(b8[:])

	writeLenPrefixed(h, m.Sender)
	writeLenPrefixed(h, m.Recipient)

	ph := PayloadHash(m.Payload)
	h.Write(ph[:])

	writeLenPrefixed(h, m.ParentID)

	return h.Sum(nil)
}

// writeLenPrefixed writes an 8-byte big-endian length then the bytes, so a
// variable-length field cannot be confused with its neighbour.
func writeLenPrefixed(h interface{ Write([]byte) (int, error) }, b []byte) {
	var l8 [8]byte
	binary.BigEndian.PutUint64(l8[:], uint64(len(b)))
	h.Write(l8[:])
	h.Write(b)
}

// WithComputedID returns a copy of the message with ID set to its canonical
// hash. It is the only sanctioned way to stamp an id.
func (m ZoneMessage) WithComputedID() ZoneMessage {
	m.ID = ComputeMessageID(m)
	return m
}

// Validate checks the envelope's structural invariants. It does NOT enforce the
// Phase 4a Funds==0 rule -- that is an enqueue-time policy (outbox.go), kept out
// of Validate so a future value-carrying message stays a well-formed envelope.
func (m ZoneMessage) Validate() error {
	if !m.Kind.IsKnown() {
		return fmt.Errorf("%w: unknown message kind %d", ErrInvalidMessage, uint8(m.Kind))
	}
	if err := m.SourceZone.Validate(); err != nil {
		return fmt.Errorf("%w: source zone: %s", ErrInvalidMessage, err)
	}
	if err := m.DestZoneAtEnqueue.Validate(); err != nil {
		return fmt.Errorf("%w: dest zone: %s", ErrInvalidMessage, err)
	}
	if err := m.SenderNS.Validate(); err != nil {
		return fmt.Errorf("%w: sender namespace: %s", ErrInvalidMessage, err)
	}
	if err := m.RecipientNS.Validate(); err != nil {
		return fmt.Errorf("%w: recipient namespace: %s", ErrInvalidMessage, err)
	}
	if len(m.Sender) == 0 {
		return fmt.Errorf("%w: sender is required", ErrInvalidMessage)
	}
	if len(m.Recipient) == 0 {
		return fmt.Errorf("%w: recipient is required", ErrInvalidMessage)
	}
	if m.QueuedHeight < 0 {
		return fmt.Errorf("%w: queued height must not be negative", ErrInvalidMessage)
	}
	if m.DeliverHeight < m.QueuedHeight+1 {
		return fmt.Errorf("%w: deliver height %d must be at least queued height %d + 1 (I-12)", ErrInvalidMessage, m.DeliverHeight, m.QueuedHeight)
	}
	if m.DeadlineHeight != 0 && m.DeadlineHeight < m.DeliverHeight {
		return fmt.Errorf("%w: deadline height %d precedes deliver height %d", ErrInvalidMessage, m.DeadlineHeight, m.DeliverHeight)
	}
	if m.BounceDepth > MaxBounceDepth {
		return fmt.Errorf("%w: bounce depth %d exceeds max %d", ErrInvalidMessage, m.BounceDepth, MaxBounceDepth)
	}
	if m.Kind == MessageKindNormal {
		if len(m.ParentID) != 0 {
			return fmt.Errorf("%w: a normal message must not have a parent id", ErrInvalidMessage)
		}
		if m.BounceDepth != 0 {
			return fmt.Errorf("%w: a normal message must have bounce depth 0", ErrInvalidMessage)
		}
	} else {
		if len(m.ParentID) == 0 {
			return fmt.Errorf("%w: a %s message requires a parent id", ErrInvalidMessage, m.Kind)
		}
		if m.BounceDepth == 0 {
			return fmt.Errorf("%w: a %s message requires a positive bounce depth", ErrInvalidMessage, m.Kind)
		}
	}
	if len(m.ID) != 0 {
		want := ComputeMessageID(m)
		if len(m.ID) != len(want) {
			return fmt.Errorf("%w: id length %d, want %d", ErrInvalidMessage, len(m.ID), len(want))
		}
		for i := range want {
			if m.ID[i] != want[i] {
				return fmt.Errorf("%w: id does not match contents", ErrInvalidMessage)
			}
		}
	}
	return nil
}
