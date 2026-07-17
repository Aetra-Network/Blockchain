package types

import (
	"fmt"
	"slices"
	"strings"
)

// Namespace is the closed set of entity kinds AEZ can bucket. It is the first
// field of the bucket preimage and therefore a consensus constant: adding,
// removing, or renaming a token re-buckets every entity under it.
//
// Every token MUST be ASCII and MUST NOT contain a NUL byte -- the single-0x00
// framing in ComputeBucket is injective only under that invariant. Validate
// enforces it, and namespace_test.go asserts it for the whole set.
type Namespace string

const (
	// NamespaceNativeAccount covers ordinary user accounts held by
	// x/native-account. canonical_entity_id is the 32-byte v2 identity from
	// addressing.NormalizeToAccountIdentity -- the identity activation
	// records under.
	NamespaceNativeAccount	Namespace	= "native-account"

	// NamespaceContract covers Aetralis contracts. canonical_entity_id is
	// the canonical address bytes recovered with addressing.Parse from the
	// address contracttypes.DeriveContractAddress returns. A contract
	// address is ALREADY a v2 identity; do not additionally normalize it.
	NamespaceContract	Namespace	= "contract"

	// NamespaceName covers the native DNS registry (x/identity-root).
	// Core-pinned: the Core Zone owns the name registry (aez.md §4.10, I-9).
	NamespaceName	Namespace	= "name"

	// NamespaceSystem covers reserved system entities: both layers of the
	// two-layer address model (app/accounts/module_accounts.go:38-52) --
	// reserved catalog "vanity" addresses AND the distinct cosmos module
	// accounts that actually custody coins. Core-pinned: money never leaves
	// the Core Zone (I-10).
	NamespaceSystem	Namespace	= "system"
)

// allNamespaces is the closed set, in ascending byte order so any ordered use
// (hashing, export, iteration) is deterministic without a later sort (I-22).
var allNamespaces = []Namespace{
	NamespaceContract,
	NamespaceName,
	NamespaceNativeAccount,
	NamespaceSystem,
}

// AllNamespaces returns every namespace in deterministic ascending order.
func AllNamespaces() []Namespace {
	return slices.Clone(allNamespaces)
}

// IsKnown reports whether ns is a member of the closed set.
func (ns Namespace) IsKnown() bool {
	return slices.Contains(allNamespaces, ns)
}

// Validate rejects unknown namespaces and re-checks the NUL-free invariant that
// ComputeBucket's framing depends on. The NUL check is not redundant with the
// closed-set check: it is the load-bearing assumption behind bucket injectivity,
// so it is asserted rather than assumed.
func (ns Namespace) Validate() error {
	if !ns.IsKnown() {
		return fmt.Errorf("%w: unknown namespace %q", ErrInvalidNamespace, string(ns))
	}
	if strings.ContainsRune(string(ns), 0) {
		return fmt.Errorf("%w: namespace %q contains a NUL byte", ErrInvalidNamespace, string(ns))
	}
	return nil
}

// CorePinned reports whether every entity in the namespace resolves to the Core
// Zone unconditionally, bypassing the bucket hash and the routing table
// entirely.
//
// This is what makes "the Core Zone never migrates" (I-9) structural rather than
// conventional: a pinned entity never reaches the table, so NO table version --
// including a hand-crafted malicious one -- can express a Core-Zone move.
// pins_test.go proves this against an adversarial all-buckets-to-zone-4 table
// and asserts the hash was never entered.
//
// CorePinned is a NAMESPACE-level predicate, not an entity list. For
// NamespaceName that is exactly right: the Core Zone owns the whole registry.
// The same is true of x/staking's validator set (I-2) -- validator operator
// accounts are ordinary user-derived addresses that appear in no catalog, so
// only namespace-level pinning can express "the Core Zone owns the validator
// set". Entity-level pinning could not.
func CorePinned(ns Namespace) bool {
	switch ns {
	case NamespaceName, NamespaceSystem:
		return true
	default:
		return false
	}
}
