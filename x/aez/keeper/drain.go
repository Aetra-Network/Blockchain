package keeper

import (
	"context"
	"encoding/hex"
	"strconv"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sovereign-l1/l1/x/aez/types"
)

// DeliveryFunc is the seam between the bus and the recipient EXECUTOR. In the
// full system it invokes the destination zone's AVM/native executor; that is a
// cross-module call x/aez must not make in Phase 4a (the recipient isn't zoned
// until Phase 3/5, and money moves only through a Core-Zone escrow x/aez may not
// hold, I-10). So the bus is built against this function TYPE, and Phase 4a wires
// the in-module default (deliverMessage) which has no external effect.
//
// It is passed per-call to DrainWith, NOT stored on the keeper: the keeper stays
// Keeper{storeService} with no field that could carry state across a call (I-20).
// The production path always passes the same default; tests inject a failing or
// panicking function to exercise the bounce and panic-recovery paths.
type DeliveryFunc func(ctx context.Context, msg types.ZoneMessage, dstZone types.ZoneID) error

// Drain is the block-lifecycle entry point. It runs the bus with the default
// in-module deliverer.
func (k Keeper) Drain(ctx context.Context) error {
	return k.DrainWith(ctx, k.deliverMessage)
}

// deliverMessage is the Phase 4a default delivery. There is no recipient executor
// wired yet, so "delivery" means the message reached its (re-resolved)
// destination zone and is acknowledged -- a deterministic success with no
// external effect. When the executor hook lands (Phase 4b/5) the app injects the
// real deliverer through DrainWith; this default and the keeper constructor stay
// handle-free.
func (k Keeper) deliverMessage(_ context.Context, _ types.ZoneMessage, _ types.ZoneID) error {
	return nil
}

// DrainWith delivers every inbox message that is due at the current height,
// bounded by a per-block gas budget, recovering from a delivery panic so one bad
// recipient cannot halt the block (I-15), and bouncing on failure (I-14).
//
// It reuses the proven skeleton of x/contracts' EndBlocker
// (x/contracts/keeper/keeper.go:2205-2257) -- snapshot the queue up front, a
// per-execution gas clamp, break-on-over-budget accounting -- but in BeginBlock
// and WITHOUT the three gaps that skeleton has (aez.md §4.6): it compares
// deliver_height to the block height (the range scan itself excludes anything not
// yet due, so same-block delivery is impossible, I-12), it writes a real
// committed processed marker (I-13), and the id is a monotonic src_seq, not
// content+LogicalTime.
//
// I-23 fast path: if nothing is due, it returns after ONE range scan without
// reading a param, so a disabled/inert chain can never fail a block here.
//
// Phase 6b (docs/architecture/aez-throughput-preservation-design.md): the
// per-block budget is now a PER-ZONE-WEIGHTED split (params.MessageQuota)
// rather than the single flat constant this function used to spend directly.
// The params read only happens when len(due) > 0, preserving the fast path
// above; a genuine store error here propagates like every other GetParams
// call site in this module (GasQuotaForZone, EnqueueMessage), which is an
// existing failure class, not a new one (design doc §5.4b). If the committed
// MessageQuota fails Validate() -- e.g. an old-shape Params blob read after a
// binary upgrade, before anything has populated the new field -- DrainWith
// falls back to drainLegacyGlobalBudget, which reproduces today's exact
// single-global-budget algorithm byte-for-byte (design doc §5.4 Layer 2).
func (k Keeper) DrainWith(ctx context.Context, deliver DeliveryFunc) error {
	height := sdk.UnwrapSDKContext(ctx).BlockHeight()
	due, err := k.scanDueInbox(ctx, height)
	if err != nil {
		return err
	}
	if len(due) == 0 {
		return nil
	}

	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	if params.MessageQuota.Validate() != nil {
		return k.drainLegacyGlobalBudget(ctx, due, height, deliver)
	}
	return k.drainWeighted(ctx, due, height, deliver, params.MessageQuota)
}

// clampDeliveryGas applies the unchanged clamp rule: a zero or over-max
// GasLimit both clamp to MaxGasPerDelivery, so one delivery can never claim
// more than that regardless of what it declares.
func clampDeliveryGas(gasLimit uint64) uint64 {
	if gasLimit == 0 || gasLimit > types.MaxGasPerDelivery {
		return types.MaxGasPerDelivery
	}
	return gasLimit
}

// drainLegacyGlobalBudget reproduces TODAY's pre-Phase-6b algorithm
// byte-for-byte: one shared counter initialized to
// LegacyGlobalMessageGasPerBlock, the unchanged canonical (deliver_height,
// message_id) order (due is already in that order, scanDueInbox), and BREAK
// -- not skip-and-continue -- on the first message whose clamped cost
// exceeds the remaining shared budget. This is the migration-safety
// fallback (design doc §5.4 Layer 2): DrainWith takes this branch whenever
// the committed MessageQuota fails Validate(), which guarantees an upgraded
// chain that has not yet had anything populate the new field keeps today's
// exact behavior, unchanged, indefinitely -- never "everything capped at
// zero," never a failed block.
func (k Keeper) drainLegacyGlobalBudget(ctx context.Context, due []dueInboxMessage, height int64, deliver DeliveryFunc) error {
	budget := types.LegacyGlobalMessageGasPerBlock
	for i := range due {
		gasCost := clampDeliveryGas(due[i].msg.GasLimit)
		if gasCost > budget {
			// Over budget: stop the whole drain (do NOT skip-and-continue).
			// The remaining messages stay queued for a later block; nothing is
			// dropped. Matches x/contracts/keeper/keeper.go:2235-2237.
			break
		}
		budget -= gasCost
		if err := k.processInboxMessage(ctx, due[i], height, deliver); err != nil {
			return err
		}
	}
	return nil
}

// drainWeighted is the Phase 6b per-zone-weighted budget with within-block
// rollover (design doc §1.3/§1.4, as corrected against the adversarial
// review's Finding 1 -- see the doc's revision note). It is a two-pass
// algorithm over the already-materialized due slice: nothing new is read
// from the store, nothing new is nondeterministic, and CometBFT's tx
// ordering is untouched (this is BeginBlocker-internal bookkeeping over
// x/aez's own queue, exactly like today's single-budget loop).
//
// Pass 1 measures, for EVERY zone including Core, how much of its own
// allotment (OwnAllotmentForZone: ReservedGas for Core, MaxGas for an
// elastic zone) is actually used by its own due traffic, and how much is
// left idle. The idle remainder across every zone (Core included) forms one
// shared surplus pool.
//
// Pass 2 spends, in the unchanged canonical order: a message first checks
// the unconditional global backstop (never exceeded, regardless of per-zone
// accounting), then its own zone's own allotment, then -- only if that is
// exhausted -- the shared surplus pool. Core is not special-cased in Pass 2
// at all: it is bounded by its own allotment (ReservedGas) exactly like an
// elastic zone is bounded by MaxGas, and can roll over into the same shared
// surplus exactly like an elastic zone can. This symmetry is what makes an
// elastic zone's own cap unconditionally safe from Core's excess spending
// (closing Finding 1) while simultaneously preserving Core's own floor
// against elastic zones (the property the original, asymmetric draft only
// half-delivered) and preserving inertness (an idle Core-only chain's sole
// zone still reaches the full TotalMessageGasPerBlock, because every
// elastic zone's cap is idle surplus when nothing routes to it).
//
// A message that fails both checks is skipped -- continue, never break --
// so a later message from a different zone still gets its own chance this
// block; nothing is ever dropped, only delayed to a later block.
func (k Keeper) drainWeighted(ctx context.Context, due []dueInboxMessage, height int64, deliver DeliveryFunc, quota types.MessageQuotaParams) error {
	var ownAllotment [types.ZoneCount]uint64
	for z := uint32(0); z < types.ZoneCount; z++ {
		allotment, err := quota.OwnAllotmentForZone(z)
		if err != nil {
			return err
		}
		ownAllotment[z] = allotment
	}

	// Pass 1: measure demand and unused capacity. No mutation, no delivery.
	var demand [types.ZoneCount]uint64
	for i := range due {
		z := uint32(due[i].msg.SourceZone)
		if z >= types.ZoneCount {
			// Structurally unreachable: writeEnqueued validates SourceZone
			// before a message can ever reach the inbox. Defensive only.
			continue
		}
		g := clampDeliveryGas(due[i].msg.GasLimit)
		if demand[z]+g < demand[z] {
			demand[z] = ^uint64(0) // saturate, never wrap (I-22 determinism)
		} else {
			demand[z] += g
		}
	}

	var totalSurplus uint64
	for z := uint32(0); z < types.ZoneCount; z++ {
		used := demand[z]
		if used > ownAllotment[z] {
			used = ownAllotment[z]
		}
		totalSurplus += ownAllotment[z] - used
	}

	// Pass 2: spend, in the unchanged canonical (deliver_height, message_id)
	// order -- due is already in that order (scanDueInbox).
	var spent [types.ZoneCount]uint64
	var totalSpent uint64
	surplusRemaining := totalSurplus

	for i := range due {
		g := clampDeliveryGas(due[i].msg.GasLimit)
		if totalSpent+g > quota.TotalMessageGasPerBlock {
			continue // global backstop: skip, never break -- nothing is dropped
		}
		z := uint32(due[i].msg.SourceZone)
		if z >= types.ZoneCount {
			continue // defensive only, see Pass 1
		}
		switch {
		case spent[z]+g <= ownAllotment[z]:
			spent[z] += g
			totalSpent += g
		case g <= surplusRemaining:
			surplusRemaining -= g
			totalSpent += g
		default:
			continue // own allotment and surplus both exhausted; stays queued
		}
		if err := k.processInboxMessage(ctx, due[i], height, deliver); err != nil {
			return err
		}
	}
	return nil
}

// processInboxMessage runs the exactly-once delivery of one message at block N.
//
// Order (all committed atomically in the block):
//  1. If the id already has a processed marker, it is terminal: delete the inbox
//     record and return (the replay/duplicate gate, I-13).
//  2. Classify the outcome. Re-resolve the destination against the CURRENT table
//     (the re-resolution rule) -> InvalidDestination on failure. Check the
//     deadline -> Expired. Otherwise write a provisional SUCCESS marker BEFORE
//     the effect and call deliver under panic recovery -> ExecutionFailed on
//     error/panic.
//  3. Delete the inbox record and prune the outbox (both terminal transitions).
//  4. On failure of a NORMAL message, produce a compensating BOUNCE and record a
//     BOUNCED marker; if the compensation cannot be produced, or the failing
//     message is itself a BOUNCE/REFUND, record TERMINAL_FAILURE (no re-bounce).
//     On success, record the SUCCESS marker.
func (k Keeper) processInboxMessage(ctx context.Context, due dueInboxMessage, height int64, deliver DeliveryFunc) error {
	msg := due.msg

	// (1) Idempotency / replay: a marked id is terminal. Clean up any lingering
	// inbox record and stop -- never redeliver.
	processed, err := k.hasProcessed(ctx, due.messageID)
	if err != nil {
		return err
	}
	if processed {
		return k.deleteInbox(ctx, due.deliverHeight, due.messageID)
	}

	// (2) Classify the outcome.
	succeeded := false
	reason := types.FailureReasonNone
	dstZone, resolveErr := k.resolveRecipientZone(ctx, msg)
	switch {
	case resolveErr != nil:
		// The recipient no longer resolves against the current table. dstZone
		// is unknown; fall back to the enqueue-time destination for the
		// bounce's source-zone bookkeeping.
		reason = types.FailureReasonInvalidDestination
		dstZone = msg.DestZoneAtEnqueue
	case msg.DeadlineHeight != 0 && height > msg.DeadlineHeight:
		reason = types.FailureReasonExpired
	default:
		// Write the provisional success marker BEFORE any effect, so a partial
		// effect can never be observed without its marker (I-13). It is
		// overwritten below if the effect fails.
		if err := k.setProcessed(ctx, marker(due.messageID, types.ReceiptStatusSuccess, types.FailureReasonNone, height)); err != nil {
			return err
		}
		if derr, panicked := safeDeliver(func() error { return deliver(ctx, msg, dstZone) }); derr != nil || panicked {
			reason = types.FailureReasonExecutionFailed
		} else {
			succeeded = true
		}
	}

	// (3) Terminal transitions for the original message.
	if err := k.deleteInbox(ctx, due.deliverHeight, due.messageID); err != nil {
		return err
	}
	if err := k.pruneOutbox(ctx, msg); err != nil {
		return err
	}

	// (4) Finalize the marker and, on failure, compensate.
	if succeeded {
		if err := k.setProcessed(ctx, marker(due.messageID, types.ReceiptStatusSuccess, types.FailureReasonNone, height)); err != nil {
			return err
		}
		k.emitMessageEvent(ctx, types.EventTypeDeliverZoneMessage, msg, dstZone, types.ReceiptStatusSuccess, types.FailureReasonNone)
		return nil
	}

	// Failure. Only a NORMAL message bounces; a BOUNCE/REFUND that fails is
	// terminal (the kind ladder, I-14). MaxBounceDepth is the redundant cap.
	if msg.Kind == types.MessageKindNormal {
		produced, err := k.enqueueBounce(ctx, msg, height, dstZone)
		if err != nil {
			return err
		}
		if produced {
			if err := k.setProcessed(ctx, marker(due.messageID, types.ReceiptStatusBounced, reason, height)); err != nil {
				return err
			}
			k.emitMessageEvent(ctx, types.EventTypeBounceZoneMessage, msg, dstZone, types.ReceiptStatusBounced, reason)
			return nil
		}
	}

	// No compensation produced (bounce of a bounce, depth exceeded, sender
	// unresolvable, or queue full): terminal, never re-bounced.
	if err := k.setProcessed(ctx, marker(due.messageID, types.ReceiptStatusTerminalFailure, reason, height)); err != nil {
		return err
	}
	k.emitMessageEvent(ctx, types.EventTypeTerminalZoneMessage, msg, dstZone, types.ReceiptStatusTerminalFailure, reason)
	return nil
}

// marker builds a ProcessedMarker, the committed exactly-once record.
func marker(id []byte, status types.ReceiptStatus, reason types.FailureReason, height int64) types.ProcessedMarker {
	return types.ProcessedMarker{MessageID: id, Status: status, Reason: reason, Height: height}
}

// enqueueBounce produces the compensating message that returns a failed NORMAL
// message to its sender, with sender/recipient swapped and Kind=BOUNCE. It is a
// FORWARD-produced new message with a new id -- a saga/compensation, never a
// rollback of the finalized source block (aez.md §5).
//
// It returns produced=false (not an error) when it declines: depth would exceed
// MaxBounceDepth, the sender no longer resolves, or the inbox is full. In those
// cases the caller records TERMINAL_FAILURE. It returns an error only for a real
// store fault. The bounce originates from attemptedZone (where delivery was
// attempted) and its delivery will re-resolve the original sender's CURRENT zone.
func (k Keeper) enqueueBounce(ctx context.Context, original types.ZoneMessage, height int64, attemptedZone types.ZoneID) (bool, error) {
	depth := original.BounceDepth + 1
	if depth > types.MaxBounceDepth {
		return false, nil
	}
	senderZone, err := k.ZoneOf(ctx, original.SenderNS, original.Sender)
	if err != nil {
		// The original sender can no longer be classified; cannot compensate.
		return false, nil
	}
	seq, err := k.nextSourceSequence(ctx, attemptedZone, original.Recipient)
	if err != nil {
		return false, err
	}
	bounce := types.ZoneMessage{
		SourceZone:        attemptedZone,
		DestZoneAtEnqueue: senderZone,
		SourceSeq:         seq,
		SenderNS:          original.RecipientNS,
		Sender:            original.Recipient,
		RecipientNS:       original.SenderNS,
		Recipient:         original.Sender,
		Payload:           original.Payload,
		Funds:             0,
		GasLimit:          original.GasLimit,
		Kind:              types.MessageKindBounce,
		ParentID:          original.ID,
		BounceDepth:       depth,
		DeadlineHeight:    0,
		QueuedHeight:      height,
		DeliverHeight:     height + 1,
	}
	bounce = bounce.WithComputedID()
	if err := k.writeEnqueued(ctx, bounce); err != nil {
		// A full queue (or an over-depth/invalid bounce) is a decline, not a
		// block-halting fault: the caller downgrades to TERMINAL_FAILURE.
		return false, nil
	}
	return true, nil
}

// safeDeliver runs the delivery closure under panic recovery, the I-15 pattern
// (x/contracts/keeper/keeper.go:2296-2320). A panic in one recipient's execution
// becomes a delivery failure (which bounces) rather than a halted block.
func safeDeliver(fn func() error) (err error, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			panicked = true
		}
	}()
	return fn(), false
}

// emitMessageEvent emits one bus event. Like emitRoutingTableEvent it tolerates a
// context with no EventManager (keeper unit tests build sdk.Context directly)
// rather than failing a block over observability.
func (k Keeper) emitMessageEvent(ctx context.Context, eventType string, msg types.ZoneMessage, dstZone types.ZoneID, status types.ReceiptStatus, reason types.FailureReason) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if sdkCtx.EventManager() == nil {
		return
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		eventType,
		sdk.NewAttribute(types.AttributeKeyMessageID, hex.EncodeToString(msg.ID)),
		sdk.NewAttribute(types.AttributeKeySourceZone, strconv.FormatUint(uint64(msg.SourceZone), 10)),
		sdk.NewAttribute(types.AttributeKeyDestZone, strconv.FormatUint(uint64(dstZone), 10)),
		sdk.NewAttribute(types.AttributeKeyStatus, status.String()),
		sdk.NewAttribute(types.AttributeKeyReason, reason.String()),
	))
}
