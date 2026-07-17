package types

import (
	"fmt"
	"strings"

	"github.com/sovereign-l1/l1/app/addressing"
)

// EntityKind is what the CALLER knows about an entity before classification. It
// is not the namespace: an address the caller believes is an account may in fact
// be a reserved system entity, and CanonicalEntityID is what decides.
type EntityKind string

const (
	// EntityKindAddress is any account-shaped address, in either encoding
	// ("AE..." user-friendly or "ae1..." bech32) or as raw bytes. It may
	// resolve to NamespaceSystem or NamespaceNativeAccount.
	EntityKindAddress	EntityKind	= "address"

	// EntityKindContract is an Aetralis contract address. Always resolves to
	// NamespaceContract.
	EntityKindContract	EntityKind	= "contract"

	// EntityKindName is an already-normalized FQDN from the native registry
	// (identityroottypes.NormalizeName). Always resolves to NamespaceName.
	EntityKindName	EntityKind	= "name"
)

// CanonicalEntityID resolves a caller-supplied entity to its (namespace,
// canonical_entity_id) pair. It is the ONLY place classification and
// normalization happen; ComputeBucket stays a dumb pure function over bytes.
//
// The ORDER of resolution is the invariant, not an implementation detail:
//
//	system  ->  contract/name  ->  native-account
//
// System is resolved FIRST, on raw bytes, before any normalization. If a module
// account fell through to native-account it would be normalized into a phantom
// v2 identity, hashed, and placed in an elastic bucket -- and I-10 ("money never
// leaves the Core Zone") would break structurally for precisely the modules that
// must be pinned forever. See IsSystemEntity for the mechanism.
//
// Because CorePinned(system) and CorePinned(name) are both true, the bucket for
// those entities never reaches a routing decision -- but the CLASSIFICATION is
// what actually enforces the pin, so it is frozen by golden vectors all the same.
func CanonicalEntityID(kind EntityKind, entity any) (Namespace, []byte, error) {
	switch kind {
	case EntityKindName:
		name, ok := entity.(string)
		if !ok {
			return "", nil, fmt.Errorf("%w: name entity must be a string, got %T", ErrInvalidEntity, entity)
		}
		name = strings.TrimSpace(name)
		if name == "" {
			return "", nil, fmt.Errorf("%w: name must not be empty", ErrInvalidEntity)
		}
		// The one namespace whose entity id legitimately IS a string --
		// but a NORMALIZED CANONICAL string (lowercased, root-qualified,
		// single-valued via identityroottypes.NormalizeName), not a
		// display encoding of some other underlying bytes. Callers must
		// normalize before calling; ComputeBucket hashes these bytes
		// as-is. This is the deliberate exception to I-6.
		return NamespaceName, []byte(name), nil

	case EntityKindAddress, EntityKindContract:
		raw, err := entityAddressBytes(entity)
		if err != nil {
			return "", nil, err
		}
		// System FIRST, on raw bytes, pre-normalization.
		if _, found, err := IsSystemEntity(raw); err != nil {
			return "", nil, err
		} else if found {
			return NamespaceSystem, raw, nil
		}
		if kind == EntityKindContract {
			// A contract address is already a v2 identity. Do NOT
			// additionally normalize: normalization is a no-op on it
			// only by luck of classification.
			return NamespaceContract, raw, nil
		}
		identity, err := addressing.NormalizeToAccountIdentity(raw)
		if err != nil {
			return "", nil, fmt.Errorf("%w: %s", ErrInvalidEntity, err)
		}
		return NamespaceNativeAccount, identity, nil

	default:
		return "", nil, fmt.Errorf("%w: unsupported entity kind %q", ErrInvalidEntity, string(kind))
	}
}

// entityAddressBytes accepts raw bytes or either display encoding and returns
// canonical address bytes.
//
// Strings are resolved through addressing.Parse, which accepts BOTH the "AE..."
// user-friendly form and the "ae1..." bech32 form and returns identical bytes
// for the same account. That is the whole point: the two are ENCODINGS of one
// byte string, and a wallet submitting one form must never land the account in a
// different bucket from a CLI submitting the other (I-6). The raw string is
// never hashed.
func entityAddressBytes(entity any) ([]byte, error) {
	switch value := entity.(type) {
	case []byte:
		if len(value) == 0 {
			return nil, fmt.Errorf("%w: address bytes must not be empty", ErrInvalidEntity)
		}
		return append([]byte(nil), value...), nil
	case string:
		text := strings.TrimSpace(value)
		if text == "" {
			return nil, fmt.Errorf("%w: address must not be empty", ErrInvalidEntity)
		}
		raw, err := addressing.Parse(text)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrInvalidEntity, err)
		}
		return raw, nil
	default:
		return nil, fmt.Errorf("%w: address entity must be []byte or string, got %T", ErrInvalidEntity, entity)
	}
}
