package types_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/aez/types"
)

// baseMessage is a valid NORMAL message fixture for id/validation tests.
func baseMessage() types.ZoneMessage {
	return types.ZoneMessage{
		SourceZone:        types.ZoneID(1),
		DestZoneAtEnqueue: types.ZoneID(2),
		SourceSeq:         1,
		SenderNS:          types.NamespaceNativeAccount,
		Sender:            []byte("sender-identity-bytes"),
		RecipientNS:       types.NamespaceNativeAccount,
		Recipient:         []byte("recipient-identity-bytes"),
		Payload:           []byte("hello"),
		Funds:             0,
		GasLimit:          21000,
		Kind:              types.MessageKindNormal,
		QueuedHeight:      100,
		DeliverHeight:     101,
	}
}

func TestComputeMessageIDIsDeterministicAndFixedWidth(t *testing.T) {
	m := baseMessage()
	id1 := types.ComputeMessageID(m)
	id2 := types.ComputeMessageID(m)
	require.Equal(t, id1, id2)
	require.Len(t, id1, 32)
}

// TestMessageIDDiffersBySequence is the core anti-collision property: two
// byte-identical messages that differ ONLY in SourceSeq must get different ids.
// This is exactly the collision ComputeInternalMessageID permits
// (x/contracts/types/contract_state.go:822-849 hashes content+Height+LogicalTime,
// with LogicalTime caller-overridable) and that AEZ's stored, keeper-only src_seq
// forbids (aez.md §4.6, Gap C).
func TestMessageIDDiffersBySequence(t *testing.T) {
	a := baseMessage()
	b := baseMessage()
	b.SourceSeq = a.SourceSeq + 1
	require.NotEqual(t, types.ComputeMessageID(a), types.ComputeMessageID(b))
}

func TestMessageIDDiffersByEverySemanticField(t *testing.T) {
	base := baseMessage()
	baseID := types.ComputeMessageID(base)

	mutate := func(f func(m *types.ZoneMessage)) []byte {
		m := baseMessage()
		f(&m)
		return types.ComputeMessageID(m)
	}

	require.NotEqual(t, baseID, mutate(func(m *types.ZoneMessage) { m.SourceZone = types.ZoneID(3) }))
	require.NotEqual(t, baseID, mutate(func(m *types.ZoneMessage) {
		m.Kind = types.MessageKindBounce
		m.ParentID = []byte("p")
		m.BounceDepth = 1
	}))
	require.NotEqual(t, baseID, mutate(func(m *types.ZoneMessage) { m.Funds = 1 }))
	require.NotEqual(t, baseID, mutate(func(m *types.ZoneMessage) { m.GasLimit = 22000 }))
	require.NotEqual(t, baseID, mutate(func(m *types.ZoneMessage) { m.DeadlineHeight = 200 }))
	require.NotEqual(t, baseID, mutate(func(m *types.ZoneMessage) { m.QueuedHeight = 101; m.DeliverHeight = 102 }))
	require.NotEqual(t, baseID, mutate(func(m *types.ZoneMessage) { m.Sender = []byte("other-sender") }))
	require.NotEqual(t, baseID, mutate(func(m *types.ZoneMessage) { m.Recipient = []byte("other-recipient") }))
	require.NotEqual(t, baseID, mutate(func(m *types.ZoneMessage) { m.Payload = []byte("world") }))
}

// TestMessageIDExcludesWhereNotWho: the id commits to WHO (recipient) and WHEN
// (queued), never to WHERE (resolved destination) or the delivery SCHEDULE, so it
// is stable across a rezone and a reschedule -- the property the re-resolution
// rule relies on.
func TestMessageIDExcludesWhereNotWho(t *testing.T) {
	base := baseMessage()
	baseID := types.ComputeMessageID(base)

	movedDest := baseMessage()
	movedDest.DestZoneAtEnqueue = types.ZoneID(4)
	require.Equal(t, baseID, types.ComputeMessageID(movedDest))

	rescheduled := baseMessage()
	rescheduled.DeliverHeight = 250 // still >= queued+1, but not in the preimage
	require.Equal(t, baseID, types.ComputeMessageID(rescheduled))
}

// TestMessageIDLengthPrefixIsInjective: because sender/recipient are both
// variable-length, a naive concatenation would let ("ab","c") collide with
// ("a","bc"). The 8-byte length prefix on each field forbids it.
func TestMessageIDLengthPrefixIsInjective(t *testing.T) {
	m1 := baseMessage()
	m1.Sender = []byte("ab")
	m1.Recipient = []byte("c")

	m2 := baseMessage()
	m2.Sender = []byte("a")
	m2.Recipient = []byte("bc")

	require.NotEqual(t, types.ComputeMessageID(m1), types.ComputeMessageID(m2))
}

func TestSenderKeyIsStableAndDistinct(t *testing.T) {
	a := types.SenderKey([]byte("alice"))
	require.Equal(t, a, types.SenderKey([]byte("alice")))
	require.NotEqual(t, a, types.SenderKey([]byte("bob")))
	require.Len(t, a[:], types.SenderKeyLen)
}

func TestMessageValidate(t *testing.T) {
	require.NoError(t, baseMessage().Validate())
	require.NoError(t, baseMessage().WithComputedID().Validate())

	// H+1 violation: deliver at the same height as queued.
	sameHeight := baseMessage()
	sameHeight.DeliverHeight = sameHeight.QueuedHeight
	require.ErrorIs(t, sameHeight.Validate(), types.ErrInvalidMessage)

	// Unknown kind.
	badKind := baseMessage()
	badKind.Kind = types.MessageKind(99)
	require.ErrorIs(t, badKind.Validate(), types.ErrInvalidMessage)

	// Empty sender.
	noSender := baseMessage()
	noSender.Sender = nil
	require.ErrorIs(t, noSender.Validate(), types.ErrInvalidMessage)

	// NORMAL must not have a parent.
	normalWithParent := baseMessage()
	normalWithParent.ParentID = []byte("p")
	require.ErrorIs(t, normalWithParent.Validate(), types.ErrInvalidMessage)

	// BOUNCE requires a parent and a positive depth.
	bounceNoParent := baseMessage()
	bounceNoParent.Kind = types.MessageKindBounce
	require.ErrorIs(t, bounceNoParent.Validate(), types.ErrInvalidMessage)

	// A tampered id is rejected.
	tampered := baseMessage().WithComputedID()
	tampered.ID = bytes.Repeat([]byte{0xaa}, 32)
	require.ErrorIs(t, tampered.Validate(), types.ErrInvalidMessage)
}

func TestProcessedMarkerValidate(t *testing.T) {
	ok := types.ProcessedMarker{MessageID: []byte("id"), Status: types.ReceiptStatusSuccess, Reason: types.FailureReasonNone, Height: 5}
	require.NoError(t, ok.Validate())

	noID := ok
	noID.MessageID = nil
	require.ErrorIs(t, noID.Validate(), types.ErrInvalidMessage)

	// A successful marker must not carry a failure reason.
	inconsistent := ok
	inconsistent.Reason = types.FailureReasonExpired
	require.ErrorIs(t, inconsistent.Validate(), types.ErrInvalidMessage)
}
