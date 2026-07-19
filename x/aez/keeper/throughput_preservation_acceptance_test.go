package keeper

// throughput_preservation_acceptance_test.go is the missing "before" half of
// the Phase 6b acceptance-criteria load test
// (docs/architecture/aez-throughput-preservation-design.md §3): a genuine
// before/after contrast of the SAME 11-message, zone-A-flood-vs-zone-B-
// fair-share workload, run through TWO code paths that are both live in this
// binary today -- drainLegacyGlobalBudget (the pre-Phase-6b single-global-
// budget algorithm, reached via the migration-safety fallback) and
// drainWeighted (the new per-zone-weighted budget) -- rather than asserting
// only the "after" state (which bus_test.go's
// TestDrainWeightedPreservesZoneBThroughputUnderZoneAFlood already does) and
// leaving the old failure mode undemonstrated.
//
// Design choice: the "before" state is produced by forcing DrainWith's real,
// shipped drainLegacyGlobalBudget fallback branch (writeOldShapeParams,
// reused verbatim from message_quota_migration_test.go), NOT by hand-
// reverting drain.go or hand-maintaining a second copy of the old algorithm.
// This is the same choice the design doc makes in §3.3 and for the same
// reason: drainLegacyGlobalBudget IS today's exact pre-Phase-6b algorithm,
// byte-for-byte (single shared 8,000,000 counter, canonical id order, BREAK
// on first over-budget message) -- so exercising it via the fallback trigger
// is a genuine "old behaviour" measurement, not a re-implementation that
// could silently drift from what actually shipped.
//
// Both the flood and the fair-share message's ids are computed PURELY (via
// types.ComputeMessageID, no store) before either keeper is built, so the
// grind search that finds a fair-share payload excluding zone B from the
// legacy algorithm's first-8-by-ascending-id admission window costs nothing
// beyond hashing, and the SAME precomputed ids are then asserted to match
// what each of the two independent, freshly-built keepers actually enqueues
// -- binding the pure computation to real keeper behaviour twice.

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/app/addressing"
	"github.com/sovereign-l1/l1/x/aez/types"
)

// zoneMessageIDShape builds the fields ComputeMessageID actually hashes for a
// message that would be the FIRST message ever sent from senderRaw at
// srcZone (so SourceSeq is deterministically 1, matching what a fresh
// keeper's nextSourceSequence allocates for a never-before-seen (zone,
// sender) pair). dstZone is recorded for clarity only -- DestZoneAtEnqueue is
// deliberately excluded from the id preimage (message.go's ComputeMessageID
// doc comment), so it cannot affect the computed id.
func zoneMessageIDShape(t *testing.T, senderRaw, recipientRaw []byte, srcZone, dstZone types.ZoneID, payload []byte, gasLimit uint64, queuedHeight int64) types.ZoneMessage {
	t.Helper()
	senderID, err := addressing.NormalizeToAccountIdentity(senderRaw)
	require.NoError(t, err)
	recipientID, err := addressing.NormalizeToAccountIdentity(recipientRaw)
	require.NoError(t, err)
	return types.ZoneMessage{
		SourceZone:        srcZone,
		DestZoneAtEnqueue: dstZone,
		SourceSeq:         1,
		SenderNS:          types.NamespaceNativeAccount,
		Sender:            senderID,
		RecipientNS:       types.NamespaceNativeAccount,
		Recipient:         recipientID,
		Payload:           payload,
		GasLimit:          gasLimit,
		Kind:              types.MessageKindNormal,
		QueuedHeight:      queuedHeight,
		DeliverHeight:     queuedHeight + 1,
	}
}

// TestDrainOldGlobalBudgetStarvesZoneBWhileNewWeightedBudgetPreservesIt is the
// full before/after acceptance-criteria proof (design doc §3): the IDENTICAL
// enqueued workload -- zone A flooding with 10 cross-zone messages (10x a
// single elastic zone's default cap) against zone B's single fair-share
// message, same fourElasticZonesRoundRobin fixture table, same height -- is
// run twice, through two independent fresh keepers/stores:
//
//   - BEFORE: params.MessageQuota is corrupted to the Go zero value (via the
//     real old-shape-params mechanism, writeOldShapeParams), forcing DrainWith
//     down its drainLegacyGlobalBudget branch -- today's actual pre-Phase-6b
//     single-global-budget algorithm.
//   - AFTER: params.MessageQuota stays the real DefaultMessageQuotaParams(),
//     so DrainWith takes drainWeighted -- the new per-zone-weighted budget.
//
// Zone B's fair-share message's payload is ground (brute-force search over a
// counter, per design doc §3.3's idiom) so its content-addressed id
// PROVABLY sorts outside the legacy algorithm's first-8-by-ascending-id
// admission window for this exact 11-message set -- making "B is starved
// under the old mechanism" a deterministic, reproducible fact rather than a
// matter of CI-run luck. The new mechanism's guarantee (B always admits via
// its own zone allotment, independent of id sort order) is a property of the
// algorithm itself (§3.4's worked algebra), so it holds for this exact same
// ground id too -- which is the point: identical adversarial input, opposite
// outcome, depending only on which of the two shipped code paths runs it.
func TestDrainOldGlobalBudgetStarvesZoneBWhileNewWeightedBudgetPreservesIt(t *testing.T) {
	const height = int64(10000)

	// --- Fixture addresses (pure; no keeper needed for the search itself).
	zone2Addrs := addrsForZone(t, fourElasticZonesRoundRobin, types.ZoneID(2), 2)
	floodRecipient := zone2Addrs[0]
	fairSender := zone2Addrs[1]
	floodSenders := addrsForZone(t, fourElasticZonesRoundRobin, types.ZoneID(1), 10)
	fairRecipient := addrsForZone(t, fourElasticZonesRoundRobin, types.ZoneID(3), 1)[0]

	// --- Pure precomputation of the 10 flood ids (no store touched).
	floodPayloads := make([][]byte, 10)
	floodIDs := make([][]byte, 10)
	for i, s := range floodSenders {
		payload := []byte{byte(i)}
		shape := zoneMessageIDShape(t, s, floodRecipient, types.ZoneID(1), types.ZoneID(2), payload, types.MaxGasPerDelivery, height)
		floodPayloads[i] = payload
		floodIDs[i] = types.ComputeMessageID(shape)
	}

	// --- Grind zone B's payload until its id sorts OUTSIDE the legacy
	// algorithm's first-8-by-ascending-id admission window: at least 8 of
	// the 10 flood ids must be strictly less than it (so B's rank among the
	// 11 is >= 9, and the legacy algorithm's break-on-first-over-budget
	// stops before ever reaching B).
	var fairPayload, fairID []byte
	grindAttempts := 0
	for i := 0; ; i++ {
		require.Less(t, i, 100000, "grind search failed to find a B-excluding payload within budget")
		grindAttempts++
		candidate := []byte(fmt.Sprintf("fair-share-grind-%d", i))
		shape := zoneMessageIDShape(t, fairSender, fairRecipient, types.ZoneID(2), types.ZoneID(3), candidate, types.MaxGasPerDelivery, height)
		id := types.ComputeMessageID(shape)
		lessCount := 0
		for _, fid := range floodIDs {
			if bytes.Compare(fid, id) < 0 {
				lessCount++
			}
		}
		if lessCount >= 8 {
			fairPayload, fairID = candidate, id
			break
		}
	}
	t.Logf("grind: found a zone-B payload excluding it from the legacy first-8 after %d attempt(s) (payload=%q)", grindAttempts, fairPayload)

	// runScenario enqueues the IDENTICAL 11-message workload into a fresh
	// keeper/store, optionally corrupts MessageQuota to force the legacy
	// fallback, drains at H+1, and reports measured numbers.
	runScenario := func(t *testing.T, forceLegacy bool) (h1Calls int, fairAdmittedH1 bool, floodAdmittedH1 int, h1GasSpent uint64, h2TotalCalls int) {
		t.Helper()
		k, _ := busKeeper(t)
		installTable(t, k, 1, 2, height, fourElasticZonesRoundRobin)
		ctx := busCtx(height)

		gotFloodIDs := make([][]byte, 0, 10)
		for i, s := range floodSenders {
			msg, produced, err := k.EnqueueMessage(ctx, EnqueueRequest{
				SenderKind:    types.EntityKindAddress,
				Sender:        s,
				RecipientKind: types.EntityKindAddress,
				Recipient:     floodRecipient,
				Payload:       floodPayloads[i],
				GasLimit:      types.MaxGasPerDelivery,
			})
			require.NoError(t, err)
			require.True(t, produced)
			require.Equal(t, floodIDs[i], msg.ID, "real enqueue must match the pure precomputed id")
			require.Equal(t, types.ZoneID(1), msg.SourceZone)
			gotFloodIDs = append(gotFloodIDs, msg.ID)
		}

		fairMsg, produced, err := k.EnqueueMessage(ctx, EnqueueRequest{
			SenderKind:    types.EntityKindAddress,
			Sender:        fairSender,
			RecipientKind: types.EntityKindAddress,
			Recipient:     fairRecipient,
			Payload:       fairPayload,
			GasLimit:      types.MaxGasPerDelivery,
		})
		require.NoError(t, err)
		require.True(t, produced)
		require.Equal(t, fairID, fairMsg.ID, "real enqueue must match the pure precomputed id")
		require.Equal(t, types.ZoneID(2), fairMsg.SourceZone)

		if forceLegacy {
			params, err := k.GetParams(ctx)
			require.NoError(t, err)
			writeOldShapeParams(t, k, ctx, params)
			got, err := k.GetParams(ctx)
			require.NoError(t, err)
			require.Error(t, got.MessageQuota.Validate(), "test setup: the corrupted MessageQuota must fail Validate to force DrainWith's legacy fallback branch")
		}

		rec := &recorder{}
		require.NoError(t, k.DrainWith(busCtx(height+1), rec.deliver))

		delivered := map[string]bool{}
		for _, c := range rec.calls {
			delivered[string(c.id)] = true
		}
		fairAdmitted := delivered[string(fairMsg.ID)]
		floodCount := 0
		var gasSpent uint64
		for _, id := range gotFloodIDs {
			if delivered[string(id)] {
				floodCount++
				gasSpent += types.MaxGasPerDelivery
			}
		}
		if fairAdmitted {
			gasSpent += types.MaxGasPerDelivery
		}
		h1Calls = len(rec.calls)

		// Recovery: nothing is ever dropped, in EITHER mechanism -- drain
		// again one block later and confirm the full 11-message set
		// eventually delivers exactly once each.
		require.NoError(t, k.DrainWith(busCtx(height+2), rec.deliver))
		seen := map[string]bool{}
		for _, c := range rec.calls {
			require.False(t, seen[string(c.id)], "no message delivered twice")
			seen[string(c.id)] = true
		}
		require.True(t, seen[string(fairMsg.ID)], "zone B's message must eventually deliver, never dropped")
		for _, id := range gotFloodIDs {
			require.True(t, seen[string(id)], "every zone A flood message must eventually deliver, never dropped")
		}

		return h1Calls, fairAdmitted, floodCount, gasSpent, len(rec.calls)
	}

	beforeH1Calls, beforeFairAdmitted, beforeFloodCount, beforeGas, beforeTotal := runScenario(t, true)
	afterH1Calls, afterFairAdmitted, afterFloodCount, afterGas, afterTotal := runScenario(t, false)

	t.Logf("BEFORE (legacy single-global-budget fallback, drainLegacyGlobalBudget): H+1 admitted=%d/11, gas spent=%d/%d, zone B admitted=%v, zone A admitted=%d/10, total delivered across 2 blocks=%d/11",
		beforeH1Calls, beforeGas, types.LegacyGlobalMessageGasPerBlock, beforeFairAdmitted, beforeFloodCount, beforeTotal)
	t.Logf("AFTER  (new per-zone-weighted budget, drainWeighted):          H+1 admitted=%d/11, gas spent=%d/%d, zone B admitted=%v, zone A admitted=%d/10, total delivered across 2 blocks=%d/11",
		afterH1Calls, afterGas, types.DefaultMessageQuotaParams().TotalMessageGasPerBlock, afterFairAdmitted, afterFloodCount, afterTotal)

	// --- THE ACCEPTANCE CRITERION ---
	//
	// Same 11-message workload, same routing table, same zone-B message
	// (byte-identical id in both runs) -- the OLD mechanism excludes it from
	// this block's admissions (this exact grind was constructed to make that
	// deterministic, not probabilistic), while the NEW mechanism admits it
	// regardless.
	require.False(t, beforeFairAdmitted, "BEFORE: the old single-global-budget algorithm must exclude zone B's ground-to-fail message from this block's admissions")
	require.True(t, afterFairAdmitted, "AFTER: the new per-zone-weighted budget must admit zone B's message this block regardless of its id's sort position")

	// Both mechanisms admit the SAME aggregate count this block (8 of 11) --
	// the new mechanism does not cost throughput here, it only fixes WHICH
	// 8 are guaranteed to include zone B (design doc §3.5).
	require.Equal(t, 8, beforeH1Calls, "legacy: 8,000,000 / 1,000,000 per delivery = 8 admissions, canonical id order, break on first over-budget message")
	require.Equal(t, 8, afterH1Calls, "weighted: same aggregate throughput (8) this block as the legacy mechanism")

	require.Equal(t, 8, beforeFloodCount, "legacy: with B excluded by the grind, all 8 admissions this block are zone A's flood messages")
	require.Equal(t, 7, afterFloodCount, "weighted: zone A's own allotment (1) + measured rollover from idle zones 3/4/Core (6) = 7 of its 10 messages")

	require.Equal(t, uint64(8_000_000), beforeGas, "legacy: 8 deliveries x 1,000,000 gas = 8,000,000 spent this block")
	require.Equal(t, uint64(8_000_000), afterGas, "weighted: 8 deliveries x 1,000,000 gas = 8,000,000 spent this block (same aggregate gas, different membership)")

	require.Equal(t, 11, beforeTotal, "legacy fallback: nothing ever dropped, full set delivers across 2 blocks")
	require.Equal(t, 11, afterTotal, "weighted: nothing ever dropped, full set delivers across 2 blocks")
}
