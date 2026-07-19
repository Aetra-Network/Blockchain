package keeper

import (
	"context"
	"fmt"
	"testing"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sovereign-l1/l1/app/addressing"
	"github.com/sovereign-l1/l1/x/aez/types"
	"github.com/sovereign-l1/l1/x/internal/kvtest"
)

// --- harness ---------------------------------------------------------------

func busCtx(height int64) context.Context {
	return sdk.NewContext(nil, cmtproto.Header{Height: height}, false, log.NewNopLogger())
}

// busKeeper builds an ENABLED keeper over an in-memory store. Enabling is
// required for the enqueue path; the single-zone genesis still makes it inert
// (TestBusIsInertUnderOneZone proves that even enabled, one zone produces
// nothing).
func busKeeper(t *testing.T) (Keeper, *kvtest.StoreService) {
	t.Helper()
	svc := kvtest.NewStoreService()
	k := NewPersistentKeeper(svc)
	ctx := busCtx(1)
	require.NoError(t, k.InitGenesisState(ctx, types.DefaultGenesis()))
	params := types.DefaultParams()
	params.Prototype.Enabled = true
	params.Prototype.TestnetProfile = true
	require.NoError(t, k.SetParams(ctx, params))
	return k, svc
}

func addr(seed byte) []byte {
	b := make([]byte, 20)
	for i := range b {
		b[i] = seed
	}
	return b
}

func bucketOf(t *testing.T, raw []byte) types.BucketID {
	t.Helper()
	id, err := addressing.NormalizeToAccountIdentity(raw)
	require.NoError(t, err)
	return types.ComputeBucket(types.NamespaceNativeAccount, id)
}

// twoDistinctBucketAddrs returns two native-account addresses whose buckets
// differ, so a routing table can place them in different zones.
func twoDistinctBucketAddrs(t *testing.T) (sender, recipient []byte) {
	t.Helper()
	sender = addr(0x11)
	sb := bucketOf(t, sender)
	for s := 0x12; s <= 0xff; s++ {
		recipient = addr(byte(s))
		if bucketOf(t, recipient) != sb {
			return sender, recipient
		}
	}
	t.Fatal("could not find two addresses with distinct buckets")
	return nil, nil
}

// installTable installs a routing table via the REAL mechanism (schedule +
// activate at an epoch boundary), so no test can express a table the chain could
// not. assign maps each of the 256 buckets to a zone.
func installTable(t *testing.T, k Keeper, scheduleAt int64, version uint64, activation int64, assign func(bucket int) types.ZoneID) {
	t.Helper()
	var buckets [types.BucketCount]types.ZoneID
	for i := range buckets {
		buckets[i] = assign(i)
	}
	tbl := types.NewRoutingTable(version, 1, activation, buckets)
	require.NoError(t, tbl.Validate())
	require.NoError(t, k.SetPendingRoutingTable(busCtx(scheduleAt), tbl))
	activated, err := k.MaybeActivatePendingRoutingTable(busCtx(activation))
	require.NoError(t, err)
	require.True(t, activated)
}

// twoZoneRecipientElsewhere returns an assign that puts the recipient bucket in
// zone 2 and everything else (including the sender) in zone 1.
func twoZoneRecipientElsewhere(t *testing.T, recipient []byte) func(int) types.ZoneID {
	rb := int(bucketOf(t, recipient))
	return func(bucket int) types.ZoneID {
		if bucket == rb {
			return types.ZoneID(2)
		}
		return types.ZoneID(1)
	}
}

// recorder is an injectable DeliveryFunc that records calls and can be told to
// fail or panic.
type recorder struct {
	calls    []recordedCall
	fail     bool
	panicNow bool
}

type recordedCall struct {
	id  []byte
	dst types.ZoneID
}

func (r *recorder) deliver(_ context.Context, msg types.ZoneMessage, dst types.ZoneID) error {
	r.calls = append(r.calls, recordedCall{id: append([]byte(nil), msg.ID...), dst: dst})
	if r.panicNow {
		panic("injected delivery panic")
	}
	if r.fail {
		return fmt.Errorf("injected delivery failure")
	}
	return nil
}

func prefixEnd(prefix []byte) []byte {
	end := append([]byte(nil), prefix...)
	for i := len(end) - 1; i >= 0; i-- {
		end[i]++
		if end[i] != 0 {
			return end[:i+1]
		}
	}
	return nil
}

// enqueueCross installs a two-zone table (sender->1, recipient->2), then enqueues
// a NORMAL message at height 10000. It returns the enqueued message.
func enqueueCross(t *testing.T, k Keeper, gasLimit uint64) (types.ZoneMessage, []byte, []byte) {
	t.Helper()
	sender, recipient := twoDistinctBucketAddrs(t)
	installTable(t, k, 1, 2, 10000, twoZoneRecipientElsewhere(t, recipient))

	msg, produced, err := k.EnqueueMessage(busCtx(10000), EnqueueRequest{
		SenderKind:    types.EntityKindAddress,
		Sender:        sender,
		RecipientKind: types.EntityKindAddress,
		Recipient:     recipient,
		Payload:       []byte("payload"),
		GasLimit:      gasLimit,
	})
	require.NoError(t, err)
	require.True(t, produced, "cross-zone enqueue must produce a message")
	return msg, sender, recipient
}

// --- inertness -------------------------------------------------------------

// TestBusIsInertUnderOneZone is THE inertness proof: with all 256 buckets on
// zone 0, every entity resolves to the same zone, so no ZoneMessage is ever
// produced and the drain is a no-op. The store is byte-identical before and
// after -- the "bit-identical single-zone behaviour" guarantee.
func TestBusIsInertUnderOneZone(t *testing.T) {
	k, svc := busKeeper(t)

	before := svc.RawStore().Snapshot()

	// A cross-zone send between arbitrary entities: both resolve to zone 0.
	sender, recipient := twoDistinctBucketAddrs(t)
	msg, produced, err := k.EnqueueMessage(busCtx(1), EnqueueRequest{
		SenderKind:    types.EntityKindAddress,
		Sender:        sender,
		RecipientKind: types.EntityKindAddress,
		Recipient:     recipient,
		Payload:       []byte("x"),
	})
	require.NoError(t, err)
	require.False(t, produced, "single zone must never produce a message")
	require.Empty(t, msg.ID)

	// Run the real BeginBlocker (activate + drain) across several blocks.
	for h := int64(2); h <= 8; h++ {
		require.NoError(t, k.BeginBlocker(busCtx(h)))
	}

	after := svc.RawStore().Snapshot()
	require.Equal(t, before, after, "single-zone bus must not touch state")

	for _, prefix := range [][]byte{types.OutboxPrefix, types.OutboxSeqPrefix, types.InboxPrefix, types.ProcessedPrefix} {
		it, err := svc.RawStore().Iterator(prefix, prefixEnd(prefix))
		require.NoError(t, err)
		require.False(t, it.Valid(), "bus prefix %x must be empty under one zone", prefix)
		require.NoError(t, it.Close())
	}
}

// TestEnqueueSameElasticZoneProducesNothing: inertness is a per-pair property,
// not just a genesis one. Two entities in the SAME elastic zone still produce
// nothing.
func TestEnqueueSameElasticZoneProducesNothing(t *testing.T) {
	k, _ := busKeeper(t)
	sender, recipient := twoDistinctBucketAddrs(t)
	// Map EVERY bucket to zone 1, so sender and recipient share a zone.
	installTable(t, k, 1, 2, 10000, func(int) types.ZoneID { return types.ZoneID(1) })

	_, produced, err := k.EnqueueMessage(busCtx(10000), EnqueueRequest{
		SenderKind:    types.EntityKindAddress,
		Sender:        sender,
		RecipientKind: types.EntityKindAddress,
		Recipient:     recipient,
	})
	require.NoError(t, err)
	require.False(t, produced, "same-zone pair must not produce a message")
}

// TestDisabledModuleProducesNothing: the Enabled flag is an independent guard.
func TestDisabledModuleProducesNothing(t *testing.T) {
	svc := kvtest.NewStoreService()
	k := NewPersistentKeeper(svc)
	ctx := busCtx(1)
	require.NoError(t, k.InitGenesisState(ctx, types.DefaultGenesis())) // Enabled=false

	sender, recipient := twoDistinctBucketAddrs(t)
	_, produced, err := k.EnqueueMessage(ctx, EnqueueRequest{
		SenderKind:    types.EntityKindAddress,
		Sender:        sender,
		RecipientKind: types.EntityKindAddress,
		Recipient:     recipient,
	})
	require.NoError(t, err)
	require.False(t, produced)
}

// --- enqueue ---------------------------------------------------------------

func TestEnqueueCrossZoneWritesOutboxAndInbox(t *testing.T) {
	k, _ := busKeeper(t)
	msg, sender, recipient := enqueueCross(t, k, 21000)

	require.Equal(t, types.ZoneID(1), msg.SourceZone)
	require.Equal(t, types.ZoneID(2), msg.DestZoneAtEnqueue)
	require.Equal(t, uint64(1), msg.SourceSeq)
	require.Equal(t, int64(10000), msg.QueuedHeight)
	require.Equal(t, int64(10001), msg.DeliverHeight, "H+1 minimum (I-12)")
	require.Len(t, msg.ID, 32)
	require.Equal(t, msg.ID, types.ComputeMessageID(msg))

	// Outbox audit record present under (src_zone, sender, seq).
	senderID, err := addressing.NormalizeToAccountIdentity(sender)
	require.NoError(t, err)
	out, found, err := k.GetOutboxMessage(busCtx(10000), types.ZoneID(1), senderID, 1)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, msg.ID, out.ID)

	// Inbox record present, scheduled for 10001.
	due, err := k.scanDueInbox(busCtx(10001), 10001)
	require.NoError(t, err)
	require.Len(t, due, 1)
	require.Equal(t, msg.ID, due[0].msg.ID)

	// Second enqueue of the same pair gets seq 2 and a different id.
	msg2, produced, err := k.EnqueueMessage(busCtx(10000), EnqueueRequest{
		SenderKind:    types.EntityKindAddress,
		Sender:        sender,
		RecipientKind: types.EntityKindAddress,
		Recipient:     recipient,
		Payload:       []byte("payload"),
	})
	require.NoError(t, err)
	require.True(t, produced)
	require.Equal(t, uint64(2), msg2.SourceSeq)
	require.NotEqual(t, msg.ID, msg2.ID, "monotonic src_seq gives distinct ids")
}

func TestEnqueueRejectsValueTransfer(t *testing.T) {
	k, _ := busKeeper(t)
	sender, recipient := twoDistinctBucketAddrs(t)
	installTable(t, k, 1, 2, 10000, twoZoneRecipientElsewhere(t, recipient))

	_, produced, err := k.EnqueueMessage(busCtx(10000), EnqueueRequest{
		SenderKind:    types.EntityKindAddress,
		Sender:        sender,
		RecipientKind: types.EntityKindAddress,
		Recipient:     recipient,
		Funds:         1,
	})
	require.ErrorIs(t, err, types.ErrValueTransferUnsupported)
	require.False(t, produced)
}

// --- H+1 delivery ----------------------------------------------------------

// TestDeliveredAtHPlus1NotH is the regression test for the same-block-delivery
// gap at x/contracts/keeper/keeper.go:2216-2244.
func TestDeliveredAtHPlus1NotH(t *testing.T) {
	k, _ := busKeeper(t)
	msg, _, _ := enqueueCross(t, k, 21000)

	// Drain at H (10000): the message is scheduled for 10001 and MUST NOT be
	// delivered this block.
	rec := &recorder{}
	require.NoError(t, k.DrainWith(busCtx(10000), rec.deliver))
	require.Empty(t, rec.calls, "same-block delivery must be impossible")
	_, found, err := k.GetProcessedMarker(busCtx(10000), msg.ID)
	require.NoError(t, err)
	require.False(t, found, "no marker before delivery")

	// Drain at H+1 (10001): delivered exactly once.
	require.NoError(t, k.DrainWith(busCtx(10001), rec.deliver))
	require.Len(t, rec.calls, 1)
	require.Equal(t, msg.ID, rec.calls[0].id)
	require.Equal(t, types.ZoneID(2), rec.calls[0].dst)

	marker, found, err := k.GetProcessedMarker(busCtx(10001), msg.ID)
	require.NoError(t, err)
	require.True(t, found, "committed marker after delivery")
	require.Equal(t, types.ReceiptStatusSuccess, marker.Status)

	// Inbox drained, outbox pruned.
	due, err := k.scanDueInbox(busCtx(10001), 10001)
	require.NoError(t, err)
	require.Empty(t, due)
}

// --- exactly once / replay -------------------------------------------------

// TestReplayRejectedByCommittedMarker: re-injecting a delivered message must hit
// the processed marker and NOT redeliver -- a deterministic reject, not the
// "missing from queue" ambiguity of x/contracts.
func TestReplayRejectedByCommittedMarker(t *testing.T) {
	k, _ := busKeeper(t)
	msg, _, _ := enqueueCross(t, k, 21000)

	rec := &recorder{}
	require.NoError(t, k.DrainWith(busCtx(10001), rec.deliver))
	require.Len(t, rec.calls, 1)

	// Re-inject the SAME message and drain again in a later block.
	require.NoError(t, k.putInbox(busCtx(10002), msg))
	require.NoError(t, k.DrainWith(busCtx(10002), rec.deliver))
	require.Len(t, rec.calls, 1, "a replayed message must not be delivered a second time")

	// The re-injected record is cleaned up.
	due, err := k.scanDueInbox(busCtx(10002), 10002)
	require.NoError(t, err)
	require.Empty(t, due)
}

// TestReplayRejectedAfterRestart proves the marker is COMMITTED state, not a RAM
// dedup cache: a FRESH keeper over the same store still rejects the replay. This
// is the F-17 property for the exactly-once gate (I-13/I-20).
func TestReplayRejectedAfterRestart(t *testing.T) {
	k, svc := busKeeper(t)
	msg, _, _ := enqueueCross(t, k, 21000)

	rec := &recorder{}
	require.NoError(t, k.DrainWith(busCtx(10001), rec.deliver))
	require.Len(t, rec.calls, 1)

	// Re-inject, then "restart" by constructing a new keeper over the same store.
	require.NoError(t, k.putInbox(busCtx(10002), msg))
	restarted := NewPersistentKeeper(svc)

	rec2 := &recorder{}
	require.NoError(t, restarted.DrainWith(busCtx(10002), rec2.deliver))
	require.Empty(t, rec2.calls, "a restarted node must read the committed marker and not redeliver")
}

// --- rezone re-resolution --------------------------------------------------

// TestRezonedMessageDeliveredToNewZoneExactlyOnce is the re-resolution rule under
// load (aez.md §8.4): a message enqueued to a bucket that then moves zones across
// an epoch boundary is delivered exactly once to the entity's NEW zone.
func TestRezonedMessageDeliveredToNewZoneExactlyOnce(t *testing.T) {
	k, _ := busKeeper(t)
	sender, recipient := twoDistinctBucketAddrs(t)
	rb := int(bucketOf(t, recipient))

	// T1 (active at 10000): recipient bucket -> zone 2, everything else -> zone 1.
	installTable(t, k, 1, 2, 10000, func(bucket int) types.ZoneID {
		if bucket == rb {
			return types.ZoneID(2)
		}
		return types.ZoneID(1)
	})

	msg, produced, err := k.EnqueueMessage(busCtx(10000), EnqueueRequest{
		SenderKind:    types.EntityKindAddress,
		Sender:        sender,
		RecipientKind: types.EntityKindAddress,
		Recipient:     recipient,
		Payload:       []byte("payload"),
	})
	require.NoError(t, err)
	require.True(t, produced)
	require.Equal(t, types.ZoneID(2), msg.DestZoneAtEnqueue)

	// T2 (active at 20000): the recipient bucket MOVES to zone 3.
	installTable(t, k, 10001, 3, 20000, func(bucket int) types.ZoneID {
		if bucket == rb {
			return types.ZoneID(3)
		}
		return types.ZoneID(1)
	})

	// Deliver after the rezone: it must land in zone 3 (the NEW zone), once.
	rec := &recorder{}
	require.NoError(t, k.DrainWith(busCtx(20001), rec.deliver))
	require.Len(t, rec.calls, 1)
	require.Equal(t, msg.ID, rec.calls[0].id)
	require.Equal(t, types.ZoneID(3), rec.calls[0].dst, "message must follow the recipient to its new zone")

	// Not redelivered on a subsequent block.
	require.NoError(t, k.DrainWith(busCtx(20002), rec.deliver))
	require.Len(t, rec.calls, 1)
}

// --- panic / bounce --------------------------------------------------------

// TestPanicDeliveryBouncedNotHalted: a panicking delivery must not halt the
// block; the message bounces back to the sender instead.
func TestPanicDeliveryBouncedNotHalted(t *testing.T) {
	k, _ := busKeeper(t)
	msg, _, _ := enqueueCross(t, k, 21000)

	// Drain at H+1 with a panicking deliverer.
	panicRec := &recorder{panicNow: true}
	require.NoError(t, k.DrainWith(busCtx(10001), panicRec.deliver), "one panicking delivery must not halt the block")
	require.Len(t, panicRec.calls, 1)

	// The original is marked BOUNCED, not delivered.
	marker, found, err := k.GetProcessedMarker(busCtx(10001), msg.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, types.ReceiptStatusBounced, marker.Status)
	require.Equal(t, types.FailureReasonExecutionFailed, marker.Reason)

	// A BOUNCE message now sits in the inbox for 10002, addressed back to the
	// sender, lineage pointing at the original.
	due, err := k.scanDueInbox(busCtx(10002), 10002)
	require.NoError(t, err)
	require.Len(t, due, 1)
	bounce := due[0].msg
	require.Equal(t, types.MessageKindBounce, bounce.Kind)
	require.Equal(t, msg.ID, bounce.ParentID)
	require.Equal(t, uint32(1), bounce.BounceDepth)
	require.Equal(t, msg.Recipient, bounce.Sender, "bounce sender is the original recipient")
	require.Equal(t, msg.Sender, bounce.Recipient, "bounce returns to the original sender")

	// Deliver the bounce successfully: it lands in the original sender's zone (1).
	okRec := &recorder{}
	require.NoError(t, k.DrainWith(busCtx(10002), okRec.deliver))
	require.Len(t, okRec.calls, 1)
	require.Equal(t, types.ZoneID(1), okRec.calls[0].dst)
	bounceMarker, found, err := k.GetProcessedMarker(busCtx(10002), bounce.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, types.ReceiptStatusSuccess, bounceMarker.Status)
}

// TestBounceOfBounceTerminatesNotLoops: a BOUNCE that itself fails is NOT
// re-bounced -- the kind ladder ends in TERMINAL_FAILURE (I-14).
func TestBounceOfBounceTerminatesNotLoops(t *testing.T) {
	k, _ := busKeeper(t)
	msg, _, _ := enqueueCross(t, k, 21000)

	// NORMAL fails -> BOUNCE produced; the original is marked BOUNCED.
	failRec := &recorder{fail: true}
	require.NoError(t, k.DrainWith(busCtx(10001), failRec.deliver))
	origMarker, found, err := k.GetProcessedMarker(busCtx(10001), msg.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, types.ReceiptStatusBounced, origMarker.Status)

	due, err := k.scanDueInbox(busCtx(10002), 10002)
	require.NoError(t, err)
	require.Len(t, due, 1)
	bounce := due[0].msg
	require.Equal(t, types.MessageKindBounce, bounce.Kind)

	// BOUNCE also fails -> TERMINAL_FAILURE, and NO new message is produced.
	require.NoError(t, k.DrainWith(busCtx(10002), failRec.deliver))
	bounceMarker, found, err := k.GetProcessedMarker(busCtx(10002), bounce.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, types.ReceiptStatusTerminalFailure, bounceMarker.Status)

	// Nothing new was enqueued: the ladder terminated.
	for h := int64(10003); h <= 10005; h++ {
		remaining, err := k.scanDueInbox(busCtx(h), h)
		require.NoError(t, err)
		require.Empty(t, remaining, "a bounce of a bounce must not loop")
	}
}

// TestExpiredMessageBounces: a message past its deadline fails with Expired and
// bounces (an in-module failure mode needing no external hook).
func TestExpiredMessageBounces(t *testing.T) {
	k, _ := busKeeper(t)
	sender, recipient := twoDistinctBucketAddrs(t)
	installTable(t, k, 1, 2, 10000, twoZoneRecipientElsewhere(t, recipient))

	msg, produced, err := k.EnqueueMessage(busCtx(10000), EnqueueRequest{
		SenderKind:     types.EntityKindAddress,
		Sender:         sender,
		RecipientKind:  types.EntityKindAddress,
		Recipient:      recipient,
		Payload:        []byte("payload"),
		DeadlineHeight: 10001,
	})
	require.NoError(t, err)
	require.True(t, produced)

	// Deliver at 10005, well past the deadline: Expired -> BOUNCED.
	rec := &recorder{}
	require.NoError(t, k.DrainWith(busCtx(10005), rec.deliver))
	require.Empty(t, rec.calls, "an expired message is not handed to the deliverer")
	marker, found, err := k.GetProcessedMarker(busCtx(10005), msg.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, types.ReceiptStatusBounced, marker.Status)
	require.Equal(t, types.FailureReasonExpired, marker.Reason)
}

// --- bounded queue ---------------------------------------------------------

// TestQueueBoundEnforced: enqueue past MaxZoneMessageQueueDepth is a
// deterministic reject, not unbounded growth (I-21). The count is seeded to the
// cap so the real check fires without materializing 65k messages.
func TestQueueBoundEnforced(t *testing.T) {
	k, svc := busKeeper(t)
	sender, recipient := twoDistinctBucketAddrs(t)
	installTable(t, k, 1, 2, 10000, twoZoneRecipientElsewhere(t, recipient))

	// Seed the inbox count to the cap through the raw store.
	require.NoError(t, svc.RawStore().Set(types.InboxCountKey, types.EncodeUint64(types.MaxZoneMessageQueueDepth)))

	_, produced, err := k.EnqueueMessage(busCtx(10000), EnqueueRequest{
		SenderKind:    types.EntityKindAddress,
		Sender:        sender,
		RecipientKind: types.EntityKindAddress,
		Recipient:     recipient,
		Payload:       []byte("payload"),
	})
	require.ErrorIs(t, err, types.ErrQueueFull)
	require.False(t, produced)
}

// --- gas budget ------------------------------------------------------------

// --- Phase 6b: per-zone-weighted message-bus drain budget -------------------
//
// docs/architecture/aez-throughput-preservation-design.md is the full design
// (adversarial-review history included). These tests exercise drainWeighted
// (drain.go) against a routing table that actually splits buckets across
// zones -- fourElasticZonesRoundRobin below -- through the SAME real
// installTable/schedule-then-activate mechanism every other table in this
// file uses, so nothing here expresses a table the chain itself could not
// produce.

// fourElasticZonesRoundRobin is a TEST FIXTURE ONLY (design doc §2), never a
// production genesis proposal: GenesisRoutingTable() (routing_table.go) still
// maps every one of the 256 buckets to Core, and nothing in this file changes
// that default. It assigns bucket b to elastic zone (b%4)+1, spreading
// buckets roughly evenly across the four elastic zones while routing NO
// bucket to Core -- Core stays reachable in these tests only through the pin
// short-circuit (system accounts, names), exactly as in production, never
// through the table. Used only by the tests below.
func fourElasticZonesRoundRobin(bucket int) types.ZoneID {
	return types.ZoneID(bucket%4 + 1)
}

// addrsForZone returns n distinct addresses whose bucket hashes to zone z
// under assign. Used only by the Phase 6b tests below to populate specific
// zones under fourElasticZonesRoundRobin.
func addrsForZone(t *testing.T, assign func(bucket int) types.ZoneID, z types.ZoneID, n int) [][]byte {
	t.Helper()
	out := make([][]byte, 0, n)
	for s := 0x10; s <= 0xff && len(out) < n; s++ {
		a := addr(byte(s))
		if assign(int(bucketOf(t, a))) == z {
			out = append(out, a)
		}
	}
	require.Len(t, out, n, "could not find %d addresses hashing to zone %d", n, z)
	return out
}

// idSet builds a set of message ids (as strings) from a slice of ZoneMessage.
func idSet(msgs ...types.ZoneMessage) map[string]bool {
	out := make(map[string]bool, len(msgs))
	for _, m := range msgs {
		out[string(m.ID)] = true
	}
	return out
}

// countIn reports how many of ids are present in delivered.
func countIn(ids map[string]bool, delivered map[string]bool) int {
	n := 0
	for id := range ids {
		if delivered[id] {
			n++
		}
	}
	return n
}

// TestDrainWeightedPreservesZoneBThroughputUnderZoneAFlood is the
// acceptance-criteria load test (design doc §3): zone A floods the bus with
// 10x its own elastic cap's worth of cross-zone traffic while zone B sends
// exactly one normal, fair-share message. Under the OLD single-global-budget
// algorithm, whether B's message is among the 8 that admit this block depends
// entirely on where B's content-addressed message id happens to sort among
// A's ten -- pure luck. Under the per-zone-weighted budget, B's message
// admits via its OWN allotment, a check that runs before any zone's rollover
// spend can matter, so it is guaranteed regardless of sort order.
//
// Zones 3, 4, and Core are idle throughout (the fixture never routes a
// bucket to Core), so their own allotments (1,000,000 + 1,000,000 + 4,000,000
// = 6,000,000) become the measured rollover surplus zone A's flood draws on.
// Numbers below are worked out mechanically from the two-pass algorithm
// (design doc §3.4) and hold regardless of canonical message-id sort order --
// only WHICH of A's specific messages land in which bucket varies with sort
// order, never the counts.
func TestDrainWeightedPreservesZoneBThroughputUnderZoneAFlood(t *testing.T) {
	k, _ := busKeeper(t)
	installTable(t, k, 1, 2, 10000, fourElasticZonesRoundRobin)

	zone2Addrs := addrsForZone(t, fourElasticZonesRoundRobin, types.ZoneID(2), 2)
	floodRecipient := zone2Addrs[0]
	fairSender := zone2Addrs[1]
	floodSenders := addrsForZone(t, fourElasticZonesRoundRobin, types.ZoneID(1), 10)
	fairRecipient := addrsForZone(t, fourElasticZonesRoundRobin, types.ZoneID(3), 1)[0]

	ctx := busCtx(10000)
	floodMsgs := make([]types.ZoneMessage, 0, 10)
	for i, s := range floodSenders {
		msg, produced, err := k.EnqueueMessage(ctx, EnqueueRequest{
			SenderKind:    types.EntityKindAddress,
			Sender:        s,
			RecipientKind: types.EntityKindAddress,
			Recipient:     floodRecipient,
			Payload:       []byte{byte(i)},
			GasLimit:      types.MaxGasPerDelivery,
		})
		require.NoError(t, err)
		require.True(t, produced)
		require.Equal(t, types.ZoneID(1), msg.SourceZone, "flood sender must resolve to zone 1")
		floodMsgs = append(floodMsgs, msg)
	}

	fairMsg, produced, err := k.EnqueueMessage(ctx, EnqueueRequest{
		SenderKind:    types.EntityKindAddress,
		Sender:        fairSender,
		RecipientKind: types.EntityKindAddress,
		Recipient:     fairRecipient,
		Payload:       []byte("fair-share"),
		GasLimit:      types.MaxGasPerDelivery,
	})
	require.NoError(t, err)
	require.True(t, produced)
	require.Equal(t, types.ZoneID(2), fairMsg.SourceZone, "fair-share sender must resolve to zone 2")

	floodIDs := idSet(floodMsgs...)

	// H+1: zone 2's own allotment (1,000,000) admits B's single message
	// regardless of its id's sort position; zone 1 gets its own allotment (1
	// message) plus rollover from zones 3, 4, and Core (idle: 1M+1M+4M=6M),
	// admitting 7 of its 10 messages. Total admitted: 8 -- exactly what the
	// legacy single-global-budget algorithm would ALSO admit in aggregate;
	// the new mechanism does not cost throughput here, it only guarantees
	// WHICH 8 include B.
	rec := &recorder{}
	require.NoError(t, k.DrainWith(busCtx(10001), rec.deliver))
	require.Len(t, rec.calls, 8, "8,000,000 / 1,000,000 per delivery = 8 admissions this block")

	delivered := map[string]bool{}
	for _, c := range rec.calls {
		delivered[string(c.id)] = true
	}
	require.True(t, delivered[string(fairMsg.ID)],
		"zone B's fair-share message must deliver in the same block it becomes due, independent of message-id sort order")
	require.Equal(t, 7, countIn(floodIDs, delivered), "zone A's own allotment (1) + measured rollover (6) = 7 of its 10 messages")

	// H+2: everyone else is idle again (zones 2/3/4 and Core), so the full
	// budget (own 1,000,000 + rollover 7,000,000 = 8,000,000) is available to
	// zone A's remaining traffic -- comfortably more than the 3,000,000 still
	// queued. Nothing was ever dropped; the whole 11-message set delivers
	// across exactly 2 blocks.
	require.NoError(t, k.DrainWith(busCtx(10002), rec.deliver))
	require.Len(t, rec.calls, 11, "all 11 messages deliver across exactly 2 blocks; nothing is ever dropped")

	seen := map[string]bool{}
	for _, c := range rec.calls {
		require.False(t, seen[string(c.id)], "no message delivered twice")
		seen[string(c.id)] = true
	}
	require.True(t, seen[string(fairMsg.ID)])
	for id := range floodIDs {
		require.True(t, seen[id], "every flood message must eventually deliver, none dropped")
	}
}

// TestDrainWeightedGuaranteesEachFloodingZoneItsOwnCapEvenSimultaneously is
// the "quota exhaustion isolated per zone" proof required independent of the
// single-flood scenario above: TWO elastic zones flood the bus in the SAME
// block, each well past its own 1,000,000 cap. The two-pass algorithm
// guarantees each zone's own allotment regardless of the other zone's flood
// or of canonical message-id interleaving order -- Pass 1 measures every
// zone's own-cap usage from real demand before Pass 2 ever runs, so one
// zone's excess can only ever compete for the *measured leftover* surplus,
// never for another zone's own guaranteed slice (design doc §1.4, the
// Finding-1 fix).
func TestDrainWeightedGuaranteesEachFloodingZoneItsOwnCapEvenSimultaneously(t *testing.T) {
	k, _ := busKeeper(t)
	installTable(t, k, 1, 2, 10000, fourElasticZonesRoundRobin)

	zone1Senders := addrsForZone(t, fourElasticZonesRoundRobin, types.ZoneID(1), 5)
	zone3Senders := addrsForZone(t, fourElasticZonesRoundRobin, types.ZoneID(3), 5)
	zone2Recipient := addrsForZone(t, fourElasticZonesRoundRobin, types.ZoneID(2), 1)[0]

	ctx := busCtx(10000)
	enqueueFlood := func(senders [][]byte) []types.ZoneMessage {
		out := make([]types.ZoneMessage, 0, len(senders))
		for i, s := range senders {
			msg, produced, err := k.EnqueueMessage(ctx, EnqueueRequest{
				SenderKind:    types.EntityKindAddress,
				Sender:        s,
				RecipientKind: types.EntityKindAddress,
				Recipient:     zone2Recipient,
				Payload:       []byte{byte(i)},
				GasLimit:      types.MaxGasPerDelivery,
			})
			require.NoError(t, err)
			require.True(t, produced)
			out = append(out, msg)
		}
		return out
	}
	zone1Msgs := enqueueFlood(zone1Senders)
	zone3Msgs := enqueueFlood(zone3Senders)
	for _, m := range zone1Msgs {
		require.Equal(t, types.ZoneID(1), m.SourceZone)
	}
	for _, m := range zone3Msgs {
		require.Equal(t, types.ZoneID(3), m.SourceZone)
	}

	rec := &recorder{}
	require.NoError(t, k.DrainWith(busCtx(10001), rec.deliver))

	// Global backstop: 8,000,000 / 1,000,000 = 8 admissions this block, no
	// matter how the two floods interleave in canonical id order.
	require.Len(t, rec.calls, 8, "global backstop caps this block at 8 admissions regardless of interleaving")

	delivered := map[string]bool{}
	for _, c := range rec.calls {
		delivered[string(c.id)] = true
	}
	zone1IDs, zone3IDs := idSet(zone1Msgs...), idSet(zone3Msgs...)
	require.GreaterOrEqual(t, countIn(zone1IDs, delivered), 1, "zone 1's own allotment must survive zone 3's simultaneous flood")
	require.GreaterOrEqual(t, countIn(zone3IDs, delivered), 1, "zone 3's own allotment must survive zone 1's simultaneous flood")

	// Nothing is ever dropped: every remaining message drains by the next
	// block, once both floods go idle and the full budget is theirs again.
	require.NoError(t, k.DrainWith(busCtx(10002), rec.deliver))
	require.Len(t, rec.calls, 10, "all 10 flood messages deliver across at most 2 blocks; nothing dropped")
	seen := map[string]bool{}
	for _, c := range rec.calls {
		require.False(t, seen[string(c.id)], "no message delivered twice")
		seen[string(c.id)] = true
	}
	for id := range zone1IDs {
		require.True(t, seen[id])
	}
	for id := range zone3IDs {
		require.True(t, seen[id])
	}
}

// TestDrainWeightedCoreIdleReserveRollsOverToSingleBusyElasticZone isolates
// the Finding-1 fix specifically (design doc's revision note): with every
// OTHER zone -- including Core -- completely idle, a single busy elastic
// zone must reach the FULL legacy 8,000,000 budget (its own 1,000,000 cap
// plus 7,000,000 of rollover: 1,000,000 each from the two other idle elastic
// zones plus Core's full 4,000,000 reserved floor). Before the fix, Core was
// excluded from the rollover pool in both directions, so this same scenario
// would have topped out at 4,000,000 (own cap + only the two idle elastic
// zones' surplus) -- a real throughput REGRESSION in the single-busy-zone
// case the original draft did not deliver on. 8, not 4, is the proof Core's
// idle floor is genuinely part of the shared surplus pool.
func TestDrainWeightedCoreIdleReserveRollsOverToSingleBusyElasticZone(t *testing.T) {
	k, _ := busKeeper(t)
	installTable(t, k, 1, 2, 10000, fourElasticZonesRoundRobin)

	recipient := addrsForZone(t, fourElasticZonesRoundRobin, types.ZoneID(2), 1)[0]
	senders := addrsForZone(t, fourElasticZonesRoundRobin, types.ZoneID(1), 9)

	ctx := busCtx(10000)
	msgs := make([]types.ZoneMessage, 0, 9)
	for i, s := range senders {
		msg, produced, err := k.EnqueueMessage(ctx, EnqueueRequest{
			SenderKind:    types.EntityKindAddress,
			Sender:        s,
			RecipientKind: types.EntityKindAddress,
			Recipient:     recipient,
			Payload:       []byte{byte(i)},
			GasLimit:      types.MaxGasPerDelivery,
		})
		require.NoError(t, err)
		require.True(t, produced)
		msgs = append(msgs, msg)
	}

	rec := &recorder{}
	require.NoError(t, k.DrainWith(busCtx(10001), rec.deliver))
	require.Len(t, rec.calls, 8,
		"own cap (1) + rollover from zones 2/3/4 (3,000,000) + Core's idle reserve (4,000,000) = 8, not 4")

	require.NoError(t, k.DrainWith(busCtx(10002), rec.deliver))
	require.Len(t, rec.calls, 9, "the 9th message, held over one block, must still deliver -- nothing dropped")
}

// TestDrainBudgetStopsAndResumes: the per-block budget bounds delivery; anything
// over budget stays queued and drains on a later block (nothing is dropped).
func TestDrainBudgetStopsAndResumes(t *testing.T) {
	k, _ := busKeeper(t)
	sender, recipient := twoDistinctBucketAddrs(t)
	installTable(t, k, 1, 2, 10000, twoZoneRecipientElsewhere(t, recipient))

	// Each message clamps to MaxGasPerDelivery (1,000,000). Budget is 8,000,000,
	// so exactly 8 of 9 deliver this block; the 9th (whichever sorts last by id)
	// stays queued and drains next block. Nothing is dropped.
	const perMsg = types.MaxGasPerDelivery
	enqueued := map[string]bool{}
	for i := 0; i < 9; i++ {
		msg, produced, err := k.EnqueueMessage(busCtx(10000), EnqueueRequest{
			SenderKind:    types.EntityKindAddress,
			Sender:        sender,
			RecipientKind: types.EntityKindAddress,
			Recipient:     recipient,
			Payload:       []byte{byte(i)},
			GasLimit:      perMsg,
		})
		require.NoError(t, err)
		require.True(t, produced)
		enqueued[string(msg.ID)] = true
	}

	rec := &recorder{}
	require.NoError(t, k.DrainWith(busCtx(10001), rec.deliver))
	require.Len(t, rec.calls, 8, "budget 8,000,000 / 1,000,000 per delivery = 8 this block")

	// The 9th remains queued and drains next block; the full set delivers once.
	require.NoError(t, k.DrainWith(busCtx(10002), rec.deliver))
	require.Len(t, rec.calls, 9)
	delivered := map[string]bool{}
	for _, c := range rec.calls {
		require.False(t, delivered[string(c.id)], "no message delivered twice")
		delivered[string(c.id)] = true
	}
	require.Equal(t, enqueued, delivered, "every enqueued message delivered exactly once")
}
