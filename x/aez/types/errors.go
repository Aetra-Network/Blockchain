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
)
