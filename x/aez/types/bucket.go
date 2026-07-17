package types

import (
	"crypto/sha256"
	"encoding/binary"
)

const (
	// BucketDomain is the domain separator for the AEZ bucket hash. It is
	// deliberately NOT "aetra-routing-v1" (x/routing/types/routing.go:239):
	// re-domaining guarantees no vector can ever agree across the two
	// schemes while both exist in the tree.
	//
	// Changing this string re-buckets every entity on the chain. It is a
	// consensus constant, frozen by the golden vectors in bucket_test.go.
	BucketDomain	= "aetra-aez-bucket-v1"

	// BucketCount is the number of virtual buckets. It is a CONSTANT and is
	// never derived from a live zone count (I-4).
	//
	// Contrast x/routing/types/routing.go:250, which folds "% activeShards"
	// -- a live count. That construction remaps EVERY entity whenever the
	// count changes. Buckets are permanent; only the bucket->zone routing
	// table moves, and only at a routing-epoch boundary (I-8).
	BucketCount	= 256
)

// BucketID is one of the BucketCount virtual buckets. uint8 makes the domain
// exactly 0..255 unrepresentable-otherwise, so BucketCount and the type width
// cannot silently drift apart.
type BucketID uint8

// ComputeBucket returns the virtual bucket for a canonical entity.
//
//	bucket = BE_uint64(SHA256(BucketDomain || 0x00 || namespace || 0x00 || entityID)[0:8]) % 256
//
// It is a pure function: no clock, no randomness, no map iteration, no I/O and
// no error return (I-3, I-22).
//
// entityID MUST already be canonical identity BYTES. ComputeBucket deliberately
// does NOT canonicalize:
//
//   - Canonicalization is namespace-dependent and, for system entities, must
//     happen BEFORE any normalization (see CanonicalEntityID). Folding it in
//     here would force every call site to re-litigate the system/native-account
//     boundary, which is what enforces I-10 (money never leaves the Core Zone).
//   - Display strings must never reach this hash (I-6). "AE..." and "ae1..."
//     (app/addressing/codec.go:16-35) are two ENCODINGS of one byte string;
//     hashing either renders the same account into a different bucket -- and
//     from Phase 3 on, that is a state fork, not a cosmetic bug. bucket_test.go
//     asserts the divergent values explicitly so the bug is documented rather
//     than merely avoided.
//
// Two deliberate deviations from AssignShard (x/routing/types/routing.go:234-251):
//  1. "% BucketCount", never "% activeShards" (see BucketCount).
//  2. The routing epoch is NOT mixed into the digest. AssignShard mixes it at
//     :245-247, which would remap every entity on every epoch -- a total state
//     migration per epoch (I-5). Buckets are permanent; the TABLE is versioned.
//
// Framing/injectivity: the single 0x00 delimiter is sufficient here because no
// namespace token may contain a NUL byte (enforced by Namespace.Validate) and
// the only variable-length caller-influenced field, entityID, is LAST. Suppose
// ns1||0x00||e1 == ns2||0x00||e2 with |ns1| < |ns2|; the byte at index |ns1| is
// 0x00 on the left but lies inside ns2 on the right -- impossible, since no
// namespace contains NUL. Hence |ns1| == |ns2|, so ns1 == ns2 and e1 == e2.
//
// Note AssignShard gets away with putting a variable-length field in the MIDDLE
// only because its trailing 0x00||epoch[8] is fixed-width, so the total length
// pins the actor length. Do not copy that shape into a construction whose tail
// is variable.
func ComputeBucket(ns Namespace, entityID []byte) BucketID {
	h := sha256.New()
	h.Write([]byte(BucketDomain))
	h.Write([]byte{0})
	h.Write([]byte(ns))
	h.Write([]byte{0})
	h.Write(entityID)
	sum := h.Sum(nil)
	// BucketCount is 256 and 2^64 % 256 == 0, so this fold is exactly sum[7]
	// with zero modulo bias. The 8-byte big-endian fold is kept for shape
	// parity with AssignShard (:249-250) so an auditor diffing the two sees
	// one construction, not two. bucket_test.go freezes the equivalence to
	// sum[7] so nobody "simplifies" it to sum[0] -- which would silently
	// re-bucket the entire chain.
	return BucketID(binary.BigEndian.Uint64(sum[:8]) % BucketCount)
}
