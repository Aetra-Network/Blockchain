package types

import "fmt"

// ProcessedMarker is the committed exactly-once gate. It is the AEZ form of
// x/mesh's ReplayMarker (x/mesh/types/types.go:117-122), lifted almost verbatim.
//
// It is the correction to the single largest gap in the x/contracts queue
// (aez.md §4.6): that queue has NO processed set, so replay protection is
// dequeue-by-id and a replayed message is indistinguishable from one already
// handled -- both are "missing from queue". Here a replay is a MARKER HIT: a
// deterministic reject, keyed by message id ALONE (not (dst_zone, id)), so a
// message that transits zones across a rezone cannot deliver once per zone.
//
// The marker is COMMITTED state, not a RAM dedup cache. That is what makes
// exactly-once survive a restart/state-sync (I-20): a message marked at height h
// is still marked at h+1, so a restarted node does not redeliver it.
type ProcessedMarker struct {
	// MessageID is the id this marker terminates. 32 bytes.
	MessageID []byte
	// Status is the terminal classification.
	Status ReceiptStatus
	// Reason is the failure reason (FailureReasonNone on success).
	Reason FailureReason
	// Height is the block the marker was written.
	Height int64
}

// Validate checks the marker's structural invariants.
func (m ProcessedMarker) Validate() error {
	if len(m.MessageID) == 0 {
		return fmt.Errorf("%w: processed marker requires a message id", ErrInvalidMessage)
	}
	if !m.Status.IsKnown() {
		return fmt.Errorf("%w: unknown processed status %d", ErrInvalidMessage, uint8(m.Status))
	}
	if !m.Reason.IsKnown() {
		return fmt.Errorf("%w: unknown processed reason %d", ErrInvalidMessage, uint8(m.Reason))
	}
	if m.Status == ReceiptStatusSuccess && m.Reason != FailureReasonNone {
		return fmt.Errorf("%w: a successful marker must not carry a failure reason", ErrInvalidMessage)
	}
	if m.Height < 0 {
		return fmt.Errorf("%w: processed marker height must not be negative", ErrInvalidMessage)
	}
	return nil
}
