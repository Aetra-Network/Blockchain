package types

import "fmt"

// ReceiptStatus and FailureReason are lifted verbatim from
// x/mesh/types/types.go:28-44 (dead prose; shape only) and retyped as small
// enums. They classify the terminal outcome of a delivery attempt.
type ReceiptStatus uint8

const (
	// ReceiptStatusSuccess: the message reached its destination zone.
	ReceiptStatusSuccess ReceiptStatus = 0
	// ReceiptStatusBounced: delivery failed and a BOUNCE was produced.
	ReceiptStatusBounced ReceiptStatus = 1
	// ReceiptStatusRefunded: delivery failed and value was refunded (latent in
	// Phase 4a -- no value moves; see message.go MessageKindRefund).
	ReceiptStatusRefunded ReceiptStatus = 2
	// ReceiptStatusTerminalFailure: a compensating message itself failed. It is
	// NOT bounced again -- the kind ladder terminates here (I-14).
	ReceiptStatusTerminalFailure ReceiptStatus = 3
)

func (s ReceiptStatus) IsKnown() bool {
	switch s {
	case ReceiptStatusSuccess, ReceiptStatusBounced, ReceiptStatusRefunded, ReceiptStatusTerminalFailure:
		return true
	default:
		return false
	}
}

func (s ReceiptStatus) String() string {
	switch s {
	case ReceiptStatusSuccess:
		return "SUCCESS"
	case ReceiptStatusBounced:
		return "BOUNCED"
	case ReceiptStatusRefunded:
		return "REFUNDED"
	case ReceiptStatusTerminalFailure:
		return "TERMINAL_FAILURE"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", uint8(s))
	}
}

// FailureReason names why a delivery failed. FailureReasonNone is the successful
// case.
type FailureReason uint8

const (
	FailureReasonNone               FailureReason = 0
	FailureReasonInvalidDestination FailureReason = 1
	FailureReasonExpired            FailureReason = 2
	FailureReasonExecutionFailed    FailureReason = 3
)

func (r FailureReason) IsKnown() bool {
	switch r {
	case FailureReasonNone, FailureReasonInvalidDestination, FailureReasonExpired, FailureReasonExecutionFailed:
		return true
	default:
		return false
	}
}

func (r FailureReason) String() string {
	switch r {
	case FailureReasonNone:
		return "NONE"
	case FailureReasonInvalidDestination:
		return "INVALID_DESTINATION"
	case FailureReasonExpired:
		return "EXPIRED"
	case FailureReasonExecutionFailed:
		return "EXECUTION_FAILED"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", uint8(r))
	}
}

// ZoneReceipt is the delivery outcome record. It reuses MeshReceipt's shape
// (x/mesh/types/types.go:102-115) with the two shard fields dropped. It is
// produced by the drain for observability; Phase 4a does not persist a receipt
// set (the committed exactly-once gate is ProcessedMarker), so ZoneReceipt is an
// event/return shape, not stored state.
type ZoneReceipt struct {
	MessageID  []byte
	SourceZone ZoneID
	DestZone   ZoneID
	Status     ReceiptStatus
	Reason     FailureReason
	Height     int64
}
