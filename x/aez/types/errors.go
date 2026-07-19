package types

import "errors"

var (
	// ErrInvalidZone is returned for a zone id outside 0..ZoneCount-1.
	ErrInvalidZone	= errors.New("aez: invalid zone")

	// ErrInvalidNamespace is returned for a namespace outside the closed set
	// or one that violates the NUL-free framing invariant.
	ErrInvalidNamespace	= errors.New("aez: invalid namespace")

	// ErrInvalidRoutingTable is returned when a routing table is not total
	// over all BucketCount buckets, carries an out-of-range zone, or fails
	// its committed hash.
	ErrInvalidRoutingTable	= errors.New("aez: invalid routing table")

	// ErrRoutingTableNotFound is returned when no routing table is stored at
	// the requested version, or no current version is set.
	ErrRoutingTableNotFound	= errors.New("aez: routing table not found")

	// ErrRoutingTableVersion is returned when a table version does not
	// advance monotonically (I-8).
	ErrRoutingTableVersion	= errors.New("aez: routing table version must increase")

	// ErrRoutingEpochBoundary is returned when a table activation is
	// attempted anywhere other than an exact routing-epoch boundary (I-8).
	ErrRoutingEpochBoundary	= errors.New("aez: routing table changes only at a routing-epoch boundary")

	// ErrCoreZoneImmutable is returned when a table would move a Core-pinned
	// bucket out of the Core Zone (I-9).
	ErrCoreZoneImmutable	= errors.New("aez: the core zone never migrates")

	// ErrInvalidEntity is returned when a canonical entity id is empty or
	// cannot be canonicalized.
	ErrInvalidEntity	= errors.New("aez: invalid entity")

	// ErrInvalidGenesis is returned for a genesis state that fails validation.
	ErrInvalidGenesis	= errors.New("aez: invalid genesis")

	// ErrInvalidMessage is returned for a ZoneMessage or marker that fails its
	// structural invariants (Phase 4 message bus).
	ErrInvalidMessage	= errors.New("aez: invalid cross-zone message")

	// ErrQueueFull is returned when the inbox has reached
	// MaxZoneMessageQueueDepth and a new message would exceed the bound (I-21).
	ErrQueueFull	= errors.New("aez: cross-zone message queue is full")

	// ErrValueTransferUnsupported is returned when an enqueue carries non-zero
	// Funds. Phase 4a moves messages, never money: the value leg is Phase 4b
	// (aez.md §4.5/§4.8, I-10).
	ErrValueTransferUnsupported	= errors.New("aez: cross-zone value transfer is not supported in phase 4a")

	// ErrInvalidGasQuota is returned when the Phase 6 per-zone gas quota table
	// is malformed: a capped Core Zone, an elastic zone with a reservation or
	// no cap, a mis-ordered or wrong-length quota set, or a table whose elastic
	// caps plus the Core reserve exceed MaxBlockGas (I-18/I-19).
	ErrInvalidGasQuota	= errors.New("aez: invalid gas quota")

	// ErrInvalidMessageQuota is returned when the Phase 6b per-zone
	// message-bus drain quota (MessageQuotaParams) is malformed: a capped
	// Core Zone, an elastic zone with a reservation or no cap, a mis-ordered
	// or wrong-length quota set, or a table whose elastic caps plus the Core
	// reserve exceed TotalMessageGasPerBlock. Kept distinct from
	// ErrInvalidGasQuota: the two validate different committed tables
	// protecting different resources, and a caller/test may reasonably want
	// to tell them apart.
	ErrInvalidMessageQuota	= errors.New("aez: invalid message quota")
)
