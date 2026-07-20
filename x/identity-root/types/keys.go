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
	// AttachKeyPrefix keys ANS Phase B domain attachments, ONE record per
	// target wallet (the "one domain per wallet" index). The key is
	// 0x06 || target_identity_hex, where target_identity_hex is the hex of the
	// 32-byte v2 account identity the FQDN is attached TO -- the same identity
	// the reputation fee gate derives from a tx sender
	// (addressing.NormalizeToAccountIdentity), so attach-time and fee-time
	// produce the identical key for the same account. Keying by the wallet
	// makes "does this wallet hold a domain?" an O(1) store.Get for the ante
	// fee gate (see keeper.AccountHoldsDomain) and makes the one-per-wallet
	// invariant structural: the wallet identity IS the primary key.
	AttachKeyPrefix		= []byte{0x06}
	// ListingKeyPrefix keys ANS Phase B owner fixed-price sale listings
	// (docs/architecture/ans.md "Owner fixed-price sale"), one record per listed
	// name, keyed by the normalized FQDN -- the same key shape as AuctionKey,
	// since a listing and an auction are mutually exclusive "for sale" states
	// for the same name.
	ListingKeyPrefix	= []byte{0x07}
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

// AttachKey is the per-record key for an Attachment, keyed by the hex of the
// target wallet's 32-byte v2 identity. Keying by the wallet (not the FQDN)
// enforces one domain per wallet and lets the fee gate do an O(1) presence read.
func AttachKey(targetIdentityHex string) []byte {
	return append(append([]byte(nil), AttachKeyPrefix...), []byte(targetIdentityHex)...)
}

// ListingKey is the per-record key for a Listing, keyed by its normalized FQDN.
func ListingKey(name string) []byte {
	return append(append([]byte(nil), ListingKeyPrefix...), []byte(name)...)
}
