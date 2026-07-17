package types

import "encoding/binary"

const (
	// ModuleName is the x/aez module name. StoreKey is deliberately equal to
	// it: app/aetra_core_wiring.go:18-25 pairs prototypeModuleNames[i] with
	// prototypeStoreKeys[i] POSITIONALLY, and keeping the two strings equal
	// makes a mis-pairing impossible to express.
	ModuleName	= "aez"
	StoreKey	= ModuleName
)

// Per-entity key layout. There is NO genesis blob key, and there never will be.
//
// x/contracts/keeper/keeper.go:2793-2806 marshals an entire module's state into
// ONE store key. One key is one leaf is one root: it cannot be prefix-split,
// overlaid with a zone prefix, or iterated. That shape is what makes per-zone
// state impossible, and x/aez is the module that must never reintroduce it
// (I-20).
//
// Each prefix below holds exactly one entity per key, so every traversal is a
// byte-ordered range scan -- deterministic without a sort (I-22).
var (
	// ParamsKey holds Params. A single small struct is legitimately one key:
	// it is not a collection, so it cannot grow unboundedly and it can never
	// need a per-zone overlay.
	ParamsKey	= []byte{0x01}

	// RoutingTableCurrentKey holds the current table version (uint64 BE).
	RoutingTableCurrentKey	= []byte{0x02}

	// RoutingTablePendingKey holds the pending table version (uint64 BE),
	// activated at that table's ActivationHeight.
	RoutingTablePendingKey	= []byte{0x03}

	// RoutingTableVersionPrefix holds one RoutingTable per version:
	//   0x04 || version_be8 -> RoutingTable
	RoutingTableVersionPrefix	= []byte{0x04}

	// ZonePrefix holds one Zone descriptor per zone:
	//   0x05 || zone_id_be4 -> Zone
	ZonePrefix	= []byte{0x05}
)

// RoutingTableVersionKey returns the per-entity key for one routing table
// version. Big-endian so lexicographic key order equals numeric version order.
func RoutingTableVersionKey(version uint64) []byte {
	out := make([]byte, 0, len(RoutingTableVersionPrefix)+8)
	out = append(out, RoutingTableVersionPrefix...)
	var scratch [8]byte
	binary.BigEndian.PutUint64(scratch[:], version)
	return append(out, scratch[:]...)
}

// ZoneKey returns the per-entity key for one zone descriptor.
func ZoneKey(zone ZoneID) []byte {
	out := make([]byte, 0, len(ZonePrefix)+4)
	out = append(out, ZonePrefix...)
	var scratch [4]byte
	binary.BigEndian.PutUint32(scratch[:], uint32(zone))
	return append(out, scratch[:]...)
}

// EncodeUint64 encodes a uint64 big-endian.
func EncodeUint64(value uint64) []byte {
	var scratch [8]byte
	binary.BigEndian.PutUint64(scratch[:], value)
	return scratch[:]
}

// DecodeUint64 decodes a big-endian uint64, reporting whether the input was
// exactly 8 bytes.
func DecodeUint64(bz []byte) (uint64, bool) {
	if len(bz) != 8 {
		return 0, false
	}
	return binary.BigEndian.Uint64(bz), true
}
