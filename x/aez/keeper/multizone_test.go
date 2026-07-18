package keeper

// multizone_test.go activates a SECOND zone and proves the AEZ machinery works
// as multi-zone infrastructure, not merely that it lies dormant under one zone.
// The single-zone dormancy proofs live in bus_test.go
// (TestBusIsInertUnderOneZone); these tests take the complementary side.
//
// It reuses the harness in bus_test.go (busKeeper, installTable, addr, bucketOf,
// twoDistinctBucketAddrs, twoZoneRecipientElsewhere, recorder, enqueueCross,
// busCtx) rather than reinventing it, so a table is always installed through the
// real schedule-then-activate-at-an-epoch-boundary mechanism -- no test-only
// setter can express a table the chain itself could not.

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/app/addressing"
	"github.com/sovereign-l1/l1/x/aez/types"
	"github.com/sovereign-l1/l1/x/internal/kvtest"
)

// --- 1. zone activation -----------------------------------------------------

// TestActivateSecondZoneRemapsSubsetWhileCoreStaysPinned is the zone-activation
// proof. It remaps a SUBSET of the 256 buckets (exactly one) from the Core Zone
// into elastic zone 2, activates the swap at an epoch boundary, and then asserts
// four distinct residency outcomes at once:
//
//   - the remapped entity now HASHES into zone 2 (the table genuinely applies);
//   - an unmoved entity still resolves to Core -- by HASH, so the rest of the
//     table is untouched, not by a blanket pin;
//   - a namespace-pinned entity resolves to Core WITHOUT hashing, so no table
//     could ever move it (the pin short-circuits before the table is read);
//   - a fund-custodying system module account is likewise Core-pinned.
//
// Finally it asserts the governance policy layer refuses to move a still-core
// bucket off Core even with zone 2 already live: the Core Zone is a one-way trap
// (I-9), so its reserved buckets cannot be relocated away.
func TestActivateSecondZoneRemapsSubsetWhileCoreStaysPinned(t *testing.T) {
	k, _ := busKeeper(t)

	sender, recipient := twoDistinctBucketAddrs(t)
	movedBucket := int(bucketOf(t, recipient))

	// Subset remap: ONLY the recipient's bucket moves to zone 2; every other
	// bucket -- including the sender's -- stays on the Core Zone.
	installTable(t, k, 1, 2, 10000, func(bucket int) types.ZoneID {
		if bucket == movedBucket {
			return types.ZoneID(2)
		}
		return types.ZoneIDCore
	})

	at := busCtx(10000)

	// The remapped entity resolves to the new zone, via the hash.
	moved, err := k.ZoneOfEntity(at, types.EntityKindAddress, recipient)
	require.NoError(t, err)
	require.True(t, moved.Hashed, "a remapped entity must reach the bucket hash")
	require.False(t, moved.Pinned)
	require.Equal(t, types.ZoneID(2), moved.Zone, "remapped bucket must resolve to zone 2")
	require.Equal(t, types.BucketID(movedBucket), moved.Bucket)

	// The unmoved entity stays on Core -- by hash, not by pin.
	stay, err := k.ZoneOfEntity(at, types.EntityKindAddress, sender)
	require.NoError(t, err)
	require.True(t, stay.Hashed, "an unmoved account still hashes")
	require.False(t, stay.Pinned)
	require.Equal(t, types.ZoneIDCore, stay.Zone, "an unmoved bucket stays on core")

	// A namespace-pinned name resolves to Core WITHOUT hashing, so no routing
	// table -- not even one that remapped its bucket -- can relocate it.
	pinnedName, err := k.ZoneOfEntity(at, types.EntityKindName, "alice.aet")
	require.NoError(t, err)
	require.Equal(t, types.ZoneIDCore, pinnedName.Zone)
	require.True(t, pinnedName.Pinned)
	require.False(t, pinnedName.Hashed, "a pinned name must never reach the hash")

	// A fund-custodying system module account is Core-pinned too. One entry is
	// enough here; keeper_test.go freezes the whole pin set against a hostile
	// all-buckets-to-zone-4 table.
	entries, err := types.SystemPinSet()
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	res, err := k.ZoneOfEntity(at, types.EntityKindAddress, entries[0].EntityID)
	require.NoError(t, err, entries[0].Label)
	require.Equal(t, types.ZoneIDCore, res.Zone, "%s escaped core", entries[0].Label)
	require.True(t, res.Pinned)
	require.False(t, res.Hashed)

	// Governance cannot move a still-core bucket off Core, even now that zone 2
	// is live: the Core Zone is a one-way trap (I-9). Take the current table and
	// try to relocate the sender's (still-core) bucket to zone 2.
	current, err := k.GetRoutingTable(at)
	require.NoError(t, err)
	move := current.Buckets
	move[int(bucketOf(t, sender))] = types.ZoneID(2)
	moveTable := types.NewRoutingTable(3, 2, 20000, move)
	require.NoError(t, moveTable.Validate())
	require.ErrorIs(t, k.ValidateRoutingTableTransition(at, moveTable), types.ErrCoreZoneImmutable,
		"a reserved core bucket must not be movable off core")
}

// --- 2. cross-zone message, exactly-once (production path) -------------------

// TestCrossZoneExactlyOnceThroughBeginBlocker drives the message through the
// real block-lifecycle entry point -- BeginBlocker (activate + drain with the
// default in-module deliverer) -- rather than calling DrainWith with an injected
// recorder as the existing exactly-once tests do. The observable evidence at
// this path is the committed processed marker, the pruned outbox record, and the
// drained inbox.
//
// It proves: no same-block delivery at H; a committed SUCCESS marker at H+1;
// stability across later empty blocks; and that a re-injected replay is deduped
// by the committed marker even on a FRESH keeper (a restart / state-sync), never
// redelivered. The message id -- a keeper-owned monotonic source sequence, not
// caller-settable content -- is what keys that marker (I-13/I-20).
func TestCrossZoneExactlyOnceThroughBeginBlocker(t *testing.T) {
	k, svc := busKeeper(t)
	msg, sender, _ := enqueueCross(t, k, 21000)

	// H (10000): scheduled for 10001, so BeginBlocker must NOT deliver it now.
	require.NoError(t, k.BeginBlocker(busCtx(10000)))
	_, found, err := k.GetProcessedMarker(busCtx(10000), msg.ID)
	require.NoError(t, err)
	require.False(t, found, "same-block delivery must be impossible through BeginBlocker")

	// H+1 (10001): delivered exactly once by the default in-module deliverer.
	require.NoError(t, k.BeginBlocker(busCtx(10001)))
	m, found, err := k.GetProcessedMarker(busCtx(10001), msg.ID)
	require.NoError(t, err)
	require.True(t, found, "a committed marker must exist after delivery")
	require.Equal(t, types.ReceiptStatusSuccess, m.Status)

	// The source-side audit record is pruned once the message is terminal.
	senderID, err := addressing.NormalizeToAccountIdentity(sender)
	require.NoError(t, err)
	_, found, err = k.GetOutboxMessage(busCtx(10001), msg.SourceZone, senderID, msg.SourceSeq)
	require.NoError(t, err)
	require.False(t, found, "the outbox audit record must be pruned once terminal")

	due, err := k.scanDueInbox(busCtx(10001), 10001)
	require.NoError(t, err)
	require.Empty(t, due, "the inbox must be drained")

	// Later empty blocks: BeginBlocker is a no-op and the marker is stable.
	for h := int64(10002); h <= 10004; h++ {
		require.NoError(t, k.BeginBlocker(busCtx(h)))
	}
	m2, _, err := k.GetProcessedMarker(busCtx(10004), msg.ID)
	require.NoError(t, err)
	require.Equal(t, m, m2, "an already-delivered message must not be re-processed")

	// Re-inject the SAME id and drive a FRESH keeper (restart / state-sync)
	// through BeginBlocker. The committed marker -- not a RAM cache -- dedupes
	// it: no redelivery, and the replayed record is cleaned up.
	require.NoError(t, k.putInbox(busCtx(10005), msg))
	restarted := NewPersistentKeeper(svc)
	require.NoError(t, restarted.BeginBlocker(busCtx(10005)))
	due, err = restarted.scanDueInbox(busCtx(10005), 10005)
	require.NoError(t, err)
	require.Empty(t, due, "a restarted node must consume the replay without redelivering")
	m3, _, err := restarted.GetProcessedMarker(busCtx(10005), msg.ID)
	require.NoError(t, err)
	require.Equal(t, types.ReceiptStatusSuccess, m3.Status, "the marker stays SUCCESS; no second delivery, no bounce")
}

// --- 3. bounce, exactly-once (and the honest value-refund boundary) ---------

// TestCrossZoneBounceIsExactlyOnceAndCarriesNoValue proves the compensation leg:
// a NORMAL message whose recipient rejects BOUNCES back to the sender exactly
// once, the ladder terminates, and a replay of the original is deduped rather
// than producing a second bounce.
//
// It is also honest about the boundary of "refund": Phase 4a moves messages,
// never money. A value-carrying enqueue is rejected outright
// (ErrValueTransferUnsupported) and the produced bounce always carries Funds==0,
// so "the value is refunded" is NOT expressible at this keeper today -- the
// value/refund leg (Phase 4b) is unbuilt. See gapsFound.
func TestCrossZoneBounceIsExactlyOnceAndCarriesNoValue(t *testing.T) {
	k, _ := busKeeper(t)
	msg, sender, recipient := enqueueCross(t, k, 21000)

	// The recipient's executor fails -> the NORMAL message bounces, once. (The
	// injectable deliverer is used so the single delivery attempt is countable;
	// the default in-module deliverer has no observable effect.)
	failRec := &recorder{fail: true}
	require.NoError(t, k.DrainWith(busCtx(10001), failRec.deliver))
	require.Len(t, failRec.calls, 1)

	origMarker, found, err := k.GetProcessedMarker(busCtx(10001), msg.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, types.ReceiptStatusBounced, origMarker.Status)

	due, err := k.scanDueInbox(busCtx(10002), 10002)
	require.NoError(t, err)
	require.Len(t, due, 1, "exactly one compensating bounce is produced")
	bounce := due[0].msg
	require.Equal(t, types.MessageKindBounce, bounce.Kind)
	require.Equal(t, msg.ID, bounce.ParentID)
	require.Equal(t, msg.Recipient, bounce.Sender, "the bounce sender is the original recipient")
	require.Equal(t, msg.Sender, bounce.Recipient, "the bounce returns to the original sender")

	// The bounce carries no value: the refund leg is Phase 4b and unbuilt.
	require.Equal(t, uint64(0), bounce.Funds, "phase 4a bounces carry no value")

	// Re-injecting the ORIGINAL after it is marked BOUNCED must NOT yield a
	// second bounce; its marker dedupes it. Only the genuine bounce delivers.
	require.NoError(t, k.putInbox(busCtx(10002), msg))
	okRec := &recorder{}
	require.NoError(t, k.DrainWith(busCtx(10002), okRec.deliver))
	require.Len(t, okRec.calls, 1, "the replayed original is deduped; only the real bounce delivers")
	require.Equal(t, bounce.ID, okRec.calls[0].id)

	// The ladder terminates -- no compensation loop.
	for h := int64(10003); h <= 10005; h++ {
		remaining, err := k.scanDueInbox(busCtx(h), h)
		require.NoError(t, err)
		require.Empty(t, remaining, "the bounce ladder must terminate")
	}

	// The refund path is genuinely unavailable: a value-carrying cross-zone
	// enqueue (sender in zone 1, recipient in zone 2) is rejected before it can
	// be written, so no "refund the value" message can exist at this keeper.
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

// --- 4. per-zone gas quota with the reserved Core slice ---------------------

// TestPerZoneQuotaReservesCoreWhileElasticIsBounded activates elastic zone 2 and
// then asserts the Phase 6 budget model over the live layout: the Core Zone is
// uncapped (sentinel 0), so it keeps the whole global budget available and can
// never be throttled by an elastic zone; the live elastic zone is BOUNDED by a
// hard per-block cap; and the sum of every elastic cap plus the Core reserve can
// never exceed MaxBlockGas, so elastic work can never eat the Core floor
// (I-18/I-19). Governance cannot raise the elastic caps past that line.
//
// x/aez only EXPOSES this budget (GasQuotaForZone is the narrow accessor
// x/fees consumes structurally); the admission-time throttling of a real
// transaction is enforced by x/fees, which is out of this module's scope.
func TestPerZoneQuotaReservesCoreWhileElasticIsBounded(t *testing.T) {
	k, _ := busKeeper(t)
	_, recipient := twoDistinctBucketAddrs(t)
	installTable(t, k, 1, 2, 10000, twoZoneRecipientElsewhere(t, recipient))
	at := busCtx(10000)

	// Core is uncapped: it keeps its reserved capacity.
	core, err := k.GasQuotaForZone(at, uint32(types.ZoneIDCore))
	require.NoError(t, err)
	require.Equal(t, uint64(0), core, "core zone must read as uncapped")

	// The live elastic zone is bounded.
	elastic, err := k.GasQuotaForZone(at, 2)
	require.NoError(t, err)
	require.Equal(t, uint64(3_000_000), elastic, "elastic zone 2 must be capped")

	// Reserved-Core invariant: elastic caps can never consume the Core floor.
	params, err := k.GetParams(at)
	require.NoError(t, err)
	var elasticSum uint64
	for z := uint32(1); z < types.ZoneCount; z++ {
		cap_, err := params.GasQuota.MaxGasForZone(z)
		require.NoError(t, err)
		elasticSum += cap_
	}
	require.Equal(t, uint64(8_000_000), params.GasQuota.CoreReservedGas(), "core keeps its reserved slice")
	require.LessOrEqual(t, elasticSum+params.GasQuota.CoreReservedGas(), params.GasQuota.MaxBlockGas)

	// Governance cannot raise the elastic caps to eat the Core reserve.
	over := types.DefaultParams()
	over.Prototype.Enabled = true
	for i := range over.GasQuota.Quotas {
		if !over.GasQuota.Quotas[i].ZoneID.IsCore() {
			over.GasQuota.Quotas[i].MaxGas = 5_000_000 // 4*5M + 8M reserve = 28M > 20M
		}
	}
	require.Error(t, k.SetParams(at, over), "an over-budget quota table must be rejected")
}

// --- 5. determinism ---------------------------------------------------------

// distinctSenders returns n native-account addresses that all hash to a bucket
// OTHER than avoid, so a routing table can keep them together in one zone while
// the recipient sits in another. They are DISTINCT senders, so each is allocated
// source sequence 1 independently -- which is what makes the same logical
// message hash to the same id no matter the order the messages are enqueued.
func distinctSenders(t *testing.T, n int, avoid types.BucketID) [][]byte {
	t.Helper()
	out := make([][]byte, 0, n)
	for s := 0x20; s <= 0xff && len(out) < n; s++ {
		a := addr(byte(s))
		if bucketOf(t, a) == avoid {
			continue
		}
		out = append(out, a)
	}
	require.Len(t, out, n, "could not build enough distinct senders")
	return out
}

// TestDrainResultIsIdenticalRegardlessOfEnqueueOrder is the determinism proof.
//
// Two independent chains start from byte-identical genesis and the identical
// routing table; the ONLY difference is the ORDER in which the same set of
// cross-zone messages is enqueued (ascending vs descending). A deterministic bus
// -- sorted (deliver_height, message_id) inbox iteration, keeper-owned
// sequences, id-keyed markers -- must:
//
//  1. deliver in an order that does NOT depend on enqueue order;
//  2. deliver in exactly the canonical id-sorted order;
//  3. commit a BYTE-IDENTICAL store.
//
// (3) is the strong statement: not just equal query answers, but an identical
// committed image, so no map/iteration-order nondeterminism leaked into state.
func TestDrainResultIsIdenticalRegardlessOfEnqueueOrder(t *testing.T) {
	recipient := addr(0x11)
	rb := bucketOf(t, recipient)
	senders := distinctSenders(t, 6, rb)

	assign := func(bucket int) types.ZoneID {
		if bucket == int(rb) {
			return types.ZoneID(2)
		}
		return types.ZoneID(1)
	}

	run := func(order []int) (*kvtest.StoreService, []string) {
		k, svc := busKeeper(t)
		installTable(t, k, 1, 2, 10000, assign)
		for _, i := range order {
			_, produced, err := k.EnqueueMessage(busCtx(10000), EnqueueRequest{
				SenderKind:    types.EntityKindAddress,
				Sender:        senders[i],
				RecipientKind: types.EntityKindAddress,
				Recipient:     recipient,
				Payload:       []byte{byte(i)},
				GasLimit:      21000,
			})
			require.NoError(t, err)
			require.True(t, produced, "each cross-zone pair must produce a message")
		}
		rec := &recorder{}
		require.NoError(t, k.DrainWith(busCtx(10001), rec.deliver))
		delivered := make([]string, 0, len(rec.calls))
		for _, c := range rec.calls {
			delivered = append(delivered, string(c.id))
		}
		return svc, delivered
	}

	svcA, deliveredA := run([]int{0, 1, 2, 3, 4, 5})
	svcB, deliveredB := run([]int{5, 4, 3, 2, 1, 0})

	// (1) delivery order is independent of enqueue order.
	require.Equal(t, deliveredA, deliveredB, "delivery order must not depend on enqueue order")
	require.Len(t, deliveredA, len(senders), "every message delivered exactly once")

	// (2) that order is exactly the canonical id-sorted order.
	sorted := append([]string(nil), deliveredA...)
	sort.Strings(sorted)
	require.Equal(t, sorted, deliveredA, "delivery order is the canonical id order")

	// (3) the full committed store is byte-identical between the two chains.
	require.Equal(t, svcA.RawStore().Snapshot(), svcB.RawStore().Snapshot(),
		"the committed result must be identical regardless of enqueue order")
}

// --- 6. genesis round-trip of in-flight message-bus state -------------------

// TestGenesisRoundTripsInFlightMessageBusState is the regression proof for the
// completeness gap in the original 3-field GenesisState (Params/RoutingTable/
// Zones only): a genesis export taken while a cross-zone message was enqueued
// but not yet drained silently dropped the message -- and, separately, a
// genesis export taken after delivery silently dropped the committed
// exactly-once marker -- with no error, no bounce, no event. GenesisState now
// also carries the outbox, its per-(zone,sender) sequence counters, the
// inbox, and the processed-marker set, so both round-trip exactly.
//
// It drives the whole lifecycle across TWO restores (a chain upgrade while a
// message is in flight, and a second one after it is terminal), because the
// two failure modes -- losing an in-flight message vs. losing a dedupe marker
// -- are independent and each needs its own export/import to exercise.
func TestGenesisRoundTripsInFlightMessageBusState(t *testing.T) {
	k, _ := busKeeper(t)
	msg, sender, _ := enqueueCross(t, k, 21000)
	senderID, err := addressing.NormalizeToAccountIdentity(sender)
	require.NoError(t, err)

	// Export at H (10000): scheduled for 10001, so the message is enqueued
	// (outbox + inbox + its sequence counter) but not yet drained.
	exported, err := k.ExportGenesisState(busCtx(10000))
	require.NoError(t, err)
	require.NoError(t, exported.Validate())
	require.Len(t, exported.PendingOutboxMessages, 1, "the in-flight message must be exported, not dropped")
	require.Equal(t, msg.ID, exported.PendingOutboxMessages[0].ID)
	require.Len(t, exported.PendingInboxMessages, 1)
	require.Equal(t, msg.ID, exported.PendingInboxMessages[0].ID)
	require.Len(t, exported.OutboxSequences, 1)
	require.Empty(t, exported.ProcessedMarkers, "nothing is terminal yet")

	// Init a FRESH keeper/store from that export -- a chain-upgrade genesis
	// while the message is in flight. Before the fix this produced an empty
	// bus with no error, no bounce, and no event: the message was simply gone.
	freshSvc := kvtest.NewStoreService()
	fresh := NewPersistentKeeper(freshSvc)
	require.NoError(t, fresh.InitGenesisState(busCtx(1), exported))

	restoredMsg, found, err := fresh.GetOutboxMessage(busCtx(10000), msg.SourceZone, senderID, msg.SourceSeq)
	require.NoError(t, err)
	require.True(t, found, "the outbox audit record must survive the round-trip")
	require.Equal(t, msg.ID, restoredMsg.ID)

	due, err := fresh.scanDueInbox(busCtx(10000), 10000)
	require.NoError(t, err)
	require.Empty(t, due, "same-block delivery must still be impossible at H on the restored keeper")

	// H+1: the restored keeper's BeginBlocker delivers the message exactly as
	// the original keeper would have.
	require.NoError(t, fresh.BeginBlocker(busCtx(10001)))
	m, found, err := fresh.GetProcessedMarker(busCtx(10001), msg.ID)
	require.NoError(t, err)
	require.True(t, found, "the message must arrive at H+1 on the restored keeper")
	require.Equal(t, types.ReceiptStatusSuccess, m.Status)

	due, err = fresh.scanDueInbox(busCtx(10001), 10001)
	require.NoError(t, err)
	require.Empty(t, due, "the restored inbox must drain")

	_, found, err = fresh.GetOutboxMessage(busCtx(10001), msg.SourceZone, senderID, msg.SourceSeq)
	require.NoError(t, err)
	require.False(t, found, "the outbox record must be pruned once terminal")

	// A replay of the SAME message injected directly against the restored
	// keeper must be deduped by the restored ProcessedMarker set, not
	// redelivered.
	require.NoError(t, fresh.putInbox(busCtx(10002), msg))
	require.NoError(t, fresh.BeginBlocker(busCtx(10002)))
	due, err = fresh.scanDueInbox(busCtx(10002), 10002)
	require.NoError(t, err)
	require.Empty(t, due, "a replayed message against the restored keeper must be consumed, not redelivered")
	m2, _, err := fresh.GetProcessedMarker(busCtx(10002), msg.ID)
	require.NoError(t, err)
	require.Equal(t, m, m2, "the marker must stay the original SUCCESS, not record a second delivery")

	// A SECOND genesis export/import, now that the message is terminal: the
	// dedupe marker must round-trip too, or a second chain upgrade would
	// silently re-open the door to redelivery.
	finalExport, err := fresh.ExportGenesisState(busCtx(10002))
	require.NoError(t, err)
	require.NoError(t, finalExport.Validate())
	require.Empty(t, finalExport.PendingOutboxMessages, "the terminal message must not still be in the outbox")
	require.Empty(t, finalExport.PendingInboxMessages, "the terminal message must not still be in the inbox")
	require.Len(t, finalExport.ProcessedMarkers, 1, "the terminal marker must be exported")
	require.Equal(t, msg.ID, finalExport.ProcessedMarkers[0].MessageID)

	secondSvc := kvtest.NewStoreService()
	second := NewPersistentKeeper(secondSvc)
	require.NoError(t, second.InitGenesisState(busCtx(1), finalExport))
	require.NoError(t, second.putInbox(busCtx(10003), msg))
	require.NoError(t, second.BeginBlocker(busCtx(10003)))
	due, err = second.scanDueInbox(busCtx(10003), 10003)
	require.NoError(t, err)
	require.Empty(t, due, "a second-generation restored keeper must still dedupe the original message")
	m3, found, err := second.GetProcessedMarker(busCtx(10003), msg.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, types.ReceiptStatusSuccess, m3.Status, "no second delivery, no bounce, across two restores")
}
