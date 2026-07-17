package types

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// RoutingTableDomain is the domain separator for the canonical routing-table
// hash. Distinct from BucketDomain so a table hash can never collide with a
// bucket preimage.
const RoutingTableDomain = "aetra-aez-routing-table-v1"

// RoutingTable is the versioned bucket->zone map.
//
// Buckets is a fixed-size [BucketCount]ZoneID array, not a map or a slice. That
// is deliberate and load-bearing:
//
//   - Totality (I-7: every one of the 256 buckets maps to exactly one zone) is
//     enforced by the TYPE, not by a runtime length check. A map would permit a
//     missing bucket; a slice would permit a wrong length.
//   - Iteration over an array is index-ordered, so hashing and export are
//     deterministic with no sort and no map-iteration hazard (I-22).
type RoutingTable struct {
	// Version increases monotonically. It is the identity of the table.
	Version	uint64
	// Epoch is the routing epoch this table belongs to.
	Epoch	uint64
	// ActivationHeight is the block height at which this table becomes
	// current. It must be an exact routing-epoch boundary (I-8).
	ActivationHeight	int64
	// Buckets maps every bucket to its zone.
	Buckets	[BucketCount]ZoneID
	// TableHash is the committed canonical hash of the fields above. It is
	// recomputed and compared on every read, so a table cannot be tampered
	// with in the store without detection.
	TableHash	[]byte
}

// NewRoutingTable builds a table over the given bucket assignments and fills in
// its canonical hash.
func NewRoutingTable(version, epoch uint64, activationHeight int64, buckets [BucketCount]ZoneID) RoutingTable {
	table := RoutingTable{
		Version:		version,
		Epoch:			epoch,
		ActivationHeight:	activationHeight,
		Buckets:		buckets,
	}
	table.TableHash = table.ComputeTableHash()
	return table
}

// GenesisRoutingTable returns the Phase 1 table: ALL BucketCount buckets map to
// the Core Zone.
//
// This is what makes Phase 1 purely additive -- with every bucket on zone 0, no
// entity can resolve anywhere else, so execution semantics are unchanged
// regardless of whether a caller consults the table. (Per aez.md:466-472 the
// AppHash still moves at the genesis/upgrade boundary because a new store and
// new genesis bytes exist; "bit-identical" is about execution semantics, not the
// literal root.)
func GenesisRoutingTable() RoutingTable {
	var buckets [BucketCount]ZoneID
	for i := range buckets {
		buckets[i] = ZoneIDCore
	}
	return NewRoutingTable(1, 0, 0, buckets)
}

// ComputeTableHash returns the canonical hash of the table's semantic fields.
//
// Every field is written with a fixed width and a domain separator leads, so the
// digest is stable across Go struct field reordering and cannot be confused with
// a bucket preimage. TableHash itself is excluded (it is the output).
func (t RoutingTable) ComputeTableHash() []byte {
	h := sha256.New()
	h.Write([]byte(RoutingTableDomain))
	h.Write([]byte{0})
	var scratch [8]byte
	binary.BigEndian.PutUint64(scratch[:], t.Version)
	h.Write(scratch[:])
	binary.BigEndian.PutUint64(scratch[:], t.Epoch)
	h.Write(scratch[:])
	binary.BigEndian.PutUint64(scratch[:], uint64(t.ActivationHeight))
	h.Write(scratch[:])
	var zone [4]byte
	for i := 0; i < BucketCount; i++ {
		binary.BigEndian.PutUint32(zone[:], uint32(t.Buckets[i]))
		h.Write(zone[:])
	}
	return h.Sum(nil)
}

// Validate enforces I-7 (totality over all 256 buckets, every zone in range),
// version sanity, and the committed-hash match.
func (t RoutingTable) Validate() error {
	if t.Version == 0 {
		return fmt.Errorf("%w: version must be positive", ErrInvalidRoutingTable)
	}
	if t.ActivationHeight < 0 {
		return fmt.Errorf("%w: activation height must not be negative", ErrInvalidRoutingTable)
	}
	// Totality is guaranteed by the array type; the reachable failure is an
	// out-of-range zone id.
	for i := 0; i < BucketCount; i++ {
		if err := t.Buckets[i].Validate(); err != nil {
			return fmt.Errorf("%w: bucket %d: %s", ErrInvalidRoutingTable, i, err)
		}
	}
	if len(t.TableHash) == 0 {
		return fmt.Errorf("%w: table hash is required", ErrInvalidRoutingTable)
	}
	want := t.ComputeTableHash()
	if len(t.TableHash) != len(want) {
		return fmt.Errorf("%w: table hash length %d, want %d", ErrInvalidRoutingTable, len(t.TableHash), len(want))
	}
	for i := range want {
		if t.TableHash[i] != want[i] {
			return fmt.Errorf("%w: table hash does not match contents", ErrInvalidRoutingTable)
		}
	}
	return nil
}

// ZoneForBucket returns the zone the table assigns to a bucket. Total by
// construction: BucketID is a uint8 and Buckets has exactly 256 entries, so
// every possible BucketID indexes a real entry and this cannot panic.
func (t RoutingTable) ZoneForBucket(bucket BucketID) ZoneID {
	return t.Buckets[bucket]
}

// IsRoutingEpochBoundary reports whether height is an exact routing-epoch
// boundary for the given epoch length. Height 0 (genesis) is always a boundary.
func IsRoutingEpochBoundary(height int64, epochLength uint64) bool {
	if epochLength == 0 {
		return false
	}
	if height < 0 {
		return false
	}
	return uint64(height)%epochLength == 0
}

// RoutingEpochForHeight returns the routing epoch containing height.
func RoutingEpochForHeight(height int64, epochLength uint64) uint64 {
	if epochLength == 0 || height <= 0 {
		return 0
	}
	return uint64(height) / epochLength
}
