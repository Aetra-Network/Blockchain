package types

const (
	ModuleName	= "identityroot"
	StoreKey	= ModuleName
)

// Per-record store-key prefixes for the de-blobbed storage layout (see
// x/identity-root/keeper/persistence.go). {0x01} stays the residual blob; each
// prefix below is a distinct first byte so a per-record key can never collide
// with the residual or with another collection -- the same argument
// x/contracts/types/keys.go makes for its layout.
var (
	NameKeyPrefix		= []byte{0x02}
	ResolverKeyPrefix	= []byte{0x03}
	ReverseKeyPrefix	= []byte{0x04}
	AuctionKeyPrefix	= []byte{0x05}
)

// NameKey is the per-record key for a NameRecord, keyed by its normalized FQDN.
func NameKey(name string) []byte {
	return append(append([]byte(nil), NameKeyPrefix...), []byte(name)...)
}

// ResolverKey is the per-record key for a ResolverRecord, keyed by its name.
func ResolverKey(name string) []byte {
	return append(append([]byte(nil), ResolverKeyPrefix...), []byte(name)...)
}

// ReverseKey is the per-record key for a ReverseRecord, keyed by its address.
func ReverseKey(address string) []byte {
	return append(append([]byte(nil), ReverseKeyPrefix...), []byte(address)...)
}

// AuctionKey is the per-record key for an Auction, keyed by its normalized FQDN.
func AuctionKey(name string) []byte {
	return append(append([]byte(nil), AuctionKeyPrefix...), []byte(name)...)
}
