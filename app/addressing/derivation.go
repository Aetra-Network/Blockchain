package addressing

import (
	"errors"
	"fmt"
	"strings"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
)

type AddressRole string

const (
	AddressRoleAccount	AddressRole	= "account"
	AddressRoleValidator	AddressRole	= "validator"
	AddressRoleConsensus	AddressRole	= "consensus"
)

type AddressPair struct {
	Role	AddressRole
	User	string
	Raw	string
}

const pubKeyAddressV2Domain = "aetra-pubkey-address-v1"

func DeriveAccountAddress(pubKey cryptotypes.PubKey) (AddressPair, error) {
	return deriveAddressPair(AddressRoleAccount, pubKey)
}

func DeriveValidatorAddress(pubKey cryptotypes.PubKey) (AddressPair, error) {
	return deriveAddressPair(AddressRoleValidator, pubKey)
}

func DeriveConsensusAddress(pubKey cryptotypes.PubKey) (AddressPair, error) {
	return deriveAddressPair(AddressRoleConsensus, pubKey)
}

// NormalizeToAccountIdentity maps a plain account-address seed to the bytes of
// that account's canonical "v2 identity" — the identity DeriveAccountAddress
// derives from a pubkey (it runs the same NormalizeV2RawAddress domain), and the
// one native-account records activation under. Server-side code that only holds
// the plain address string, not the pubkey, uses this to reach the exact identity
// the account was activated under.
//
// It is idempotent for an address that is already a v2 (or reserved-system)
// identity: NormalizeV2RawAddress returns those classes unchanged. So callers may
// pass either the plain address or the already-derived identity and get the
// identity back either way.
func NormalizeToAccountIdentity(seed []byte) ([]byte, error) {
	return NormalizeV2RawAddress(pubKeyAddressV2Domain, seed)
}

func PairFromUserAddress(role AddressRole, userAddress string) (AddressPair, error) {
	userAddress = strings.TrimSpace(userAddress)
	if !strings.HasPrefix(userAddress, UserFriendlyPrefix) {
		return AddressPair{}, fmt.Errorf("%s address must use AE user-facing address format", role)
	}
	bz, err := Parse(userAddress)
	if err != nil {
		return AddressPair{}, err
	}
	return addressPairFromBytes(role, bz)
}

func PairFromRawAddress(role AddressRole, rawAddress string) (AddressPair, error) {
	rawAddress = strings.TrimSpace(rawAddress)
	if !strings.HasPrefix(rawAddress, Bech32HRP+"1") {
		return AddressPair{}, fmt.Errorf("%s raw address must use ae1 bech32 address format", role)
	}
	bz, err := Parse(rawAddress)
	if err != nil {
		return AddressPair{}, err
	}
	return addressPairFromBytes(role, bz)
}

func (p AddressPair) Validate() error {
	if !isAddressRole(p.Role) {
		return fmt.Errorf("unsupported address role %q", p.Role)
	}
	fromUser, err := PairFromUserAddress(p.Role, p.User)
	if err != nil {
		return err
	}
	fromRaw, err := PairFromRawAddress(p.Role, p.Raw)
	if err != nil {
		return err
	}
	if fromUser.Raw != fromRaw.Raw || fromUser.User != fromRaw.User {
		return fmt.Errorf("%s AE and raw addresses must represent the same account", p.Role)
	}
	return nil
}

func deriveAddressPair(role AddressRole, pubKey cryptotypes.PubKey) (AddressPair, error) {
	if pubKey == nil {
		return AddressPair{}, errors.New("public key is required")
	}
	v2, err := NormalizeV2RawAddress(pubKeyAddressV2Domain, []byte(pubKey.Address()))
	if err != nil {
		return AddressPair{}, err
	}
	return addressPairFromBytes(role, v2)
}

func addressPairFromBytes(role AddressRole, bz []byte) (AddressPair, error) {
	if !isAddressRole(role) {
		return AddressPair{}, fmt.Errorf("unsupported address role %q", role)
	}
	user, err := FormatUserFriendly(bz)
	if err != nil {
		return AddressPair{}, err
	}
	return AddressPair{
		Role:	role,
		User:	user,
		Raw:	Format(bz),
	}, nil
}

func isAddressRole(role AddressRole) bool {
	return role == AddressRoleAccount || role == AddressRoleValidator || role == AddressRoleConsensus
}
