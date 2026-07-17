package types

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"sort"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/sovereign-l1/l1/app/accounts"
	"github.com/sovereign-l1/l1/app/addressing"
	"github.com/sovereign-l1/l1/x/internal/prototype"
)

// This file imports app/accounts, which itself imports ~12 x/*/types packages.
// That is safe, and the reason is structural rather than incidental: every
// module app/accounts pulls in (bank/staking/gov/mint/fee-collector/
// nominator-pool/...) is permanently pinned to the Core Zone by I-10 ("money
// never leaves the Core Zone"). A Core-pinned module resolves to zone 0 by the
// CorePinned short-circuit and therefore never needs to consult x/aez at all --
// so the reverse edge that would close an import cycle is one the AEZ design
// forbids by construction, not merely one that happens not to exist yet.
//
// (Note app/accounts is NOT a "leaf package": it has a wide x/ fan-in. Only
// app/addressing is genuinely leaf. If a Core-pinned module ever does need a
// zone lookup, that is the signal to split this enumeration into its own
// package rather than to weaken the pin.)

// SystemPinEntry is one reserved system entity that resolves to the Core Zone.
type SystemPinEntry struct {
	// Label identifies the entity for diagnostics and golden-digest output.
	Label	string
	// Layer records which half of the two-layer address model this entry
	// came from (app/accounts/module_accounts.go:38-52).
	Layer	SystemPinLayer
	// EntityID is the address bytes VERBATIM, pre-normalization. See
	// CanonicalEntityID for why normalization must never be applied here.
	EntityID	[]byte
}

// SystemPinLayer names the two disjoint address layers plus the keyless
// prototype/system governance authority.
type SystemPinLayer string

const (
	// SystemPinLayerModuleAccount is a cosmos module account,
	// authtypes.NewModuleAddress(name) -- 20 bytes. These are the addresses
	// that actually custody coins.
	SystemPinLayerModuleAccount	SystemPinLayer	= "module-account"

	// SystemPinLayerCatalog is a reserved catalog ("vanity") address from
	// app/addressing/system_addresses.go -- 32 bytes, classifying
	// system_fixed.
	SystemPinLayerCatalog	SystemPinLayer	= "catalog"

	// SystemPinLayerAuthority is prototype.DefaultAuthority, the keyless
	// sentinel that gates param updates on every prototype and system
	// module. No existing accessor enumerates it; see SystemPinSet.
	SystemPinLayerAuthority	SystemPinLayer	= "authority"
)

// SystemPinSet returns every reserved system entity, in deterministic ascending
// order by (Layer, EntityID).
//
// The set is the UNION of three sources, never a choice among them:
//
//  1. Layer A -- the 23 cosmos module accounts from accounts.GetMaccPerms():
//     authtypes.NewModuleAddress(name), 20 bytes each. These custody the coins.
//  2. Layer B -- the 29 reserved catalog addresses from
//     addressing.AllSystemAddresses(), 32 bytes each.
//  3. prototype.DefaultAuthority -- the keyless governance sentinel.
//
// Layers A and B are byte-disjoint: every reserved system entity has BOTH a
// catalog "vanity" address AND a distinct cosmos module account that its module
// actually reads and writes (app/accounts/module_accounts.go:38-52). Pinning one
// and not the other would strand the other in an elastic zone.
//
// It is derived, never hardcoded, so that adding a module account to
// moduleAccountPermissions cannot silently place a fund-bearing account outside
// the Core Zone. pins_test.go additionally freezes a golden count and digest, so
// that a developer edit to either source list is a loud test failure rather than
// a silent pin-set change.
//
// Deliberately NOT used here:
//   - accounts.BlockedAddresses() -- it removes gov, nominator-pool and AETBurn
//     on purpose. Blocked-ness is a BANK policy, orthogonal to zone residency;
//     those three are among the entities most needing a pin.
//   - accounts.ReservedSystemModuleAccounts() -- only the 12-entry join.
//   - accounts.IsReservedSystemModuleAccountName() -- a wiring-gate predicate
//     over module-account NAMES; false for 11 of the 23.
func SystemPinSet() ([]SystemPinEntry, error) {
	out := make([]SystemPinEntry, 0, 64)

	// Layer A. GetMaccPerms returns a map, so its iteration order is
	// non-deterministic; collect then sort (I-22).
	maccNames := make([]string, 0, 32)
	for name := range accounts.ModuleAccountPermissions() {
		maccNames = append(maccNames, name)
	}
	sort.Strings(maccNames)
	for _, name := range maccNames {
		out = append(out, SystemPinEntry{
			Label:		"module-account/" + name,
			Layer:		SystemPinLayerModuleAccount,
			EntityID:	append([]byte(nil), authtypes.NewModuleAddress(name)...),
		})
	}

	// Layer B. AllSystemAddresses is already a deterministic slice.
	for _, address := range addressing.AllSystemAddresses() {
		bz, err := addressing.Parse(address.Raw)
		if err != nil {
			return nil, fmt.Errorf("invalid reserved system address %s: %w", address.Name, err)
		}
		out = append(out, SystemPinEntry{
			Label:		"catalog/" + address.Name,
			Layer:		SystemPinLayerCatalog,
			EntityID:	bz,
		})
	}

	// The keyless prototype/system governance authority. It is in neither
	// accessor, and it is not the gov module account -- two distinct
	// authorities coexist (SDK modules authorize against gov; prototype and
	// system modules authorize against this sentinel).
	authorityBytes, err := addressing.Parse(prototype.DefaultAuthority)
	if err != nil {
		return nil, fmt.Errorf("invalid prototype default authority: %w", err)
	}
	out = append(out, SystemPinEntry{
		Label:		"authority/prototype-default",
		Layer:		SystemPinLayerAuthority,
		EntityID:	authorityBytes,
	})

	slices.SortFunc(out, func(a, b SystemPinEntry) int {
		if a.Layer != b.Layer {
			if a.Layer < b.Layer {
				return -1
			}
			return 1
		}
		return bytes.Compare(a.EntityID, b.EntityID)
	})
	return out, nil
}

// SystemPinDigest returns a golden SHA-256 over the sorted pin set. It exists so
// that a change to either source list -- both of which are edited by developers,
// not governance -- fails a test loudly instead of silently changing which
// entities are Core-pinned.
func SystemPinDigest() (string, error) {
	entries, err := SystemPinSet()
	if err != nil {
		return "", err
	}
	h := sha256.New()
	h.Write([]byte("aetra-aez-system-pin-set-v1"))
	for _, entry := range entries {
		h.Write([]byte{0})
		h.Write([]byte(entry.Layer))
		h.Write([]byte{0})
		h.Write(entry.EntityID)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// IsSystemEntity reports whether raw address bytes belong to a reserved system
// entity, matching on the bytes VERBATIM with no normalization.
//
// Pre-normalization matching is mandatory, not stylistic. A cosmos module
// account is 20 bytes; NormalizeToAccountIdentity pads it to 32, sees the zero
// prefix, classifies it legacy_padded, and DERIVES A BRAND-NEW v2 identity
// (app/addressing/raw_policy.go:88-97). That identity is not the address x/bank
// keys off and belongs to nobody. So normalizing first would make this lookup
// miss every Layer-A account, drop those accounts into the native-account
// namespace, and hash them into an elastic bucket -- breaking I-10 for exactly
// x/nominator-pool, x/fee-collector and x/mint-authority.
//
// (aez.md:441 states canonical_entity_id is NormalizeToAccountIdentity
// unconditionally. That rule is correct for USER accounts and wrong for module
// accounts; see CanonicalEntityID.)
func IsSystemEntity(raw []byte) (SystemPinEntry, bool, error) {
	if len(raw) == 0 {
		return SystemPinEntry{}, false, nil
	}
	entries, err := SystemPinSet()
	if err != nil {
		return SystemPinEntry{}, false, err
	}
	for _, entry := range entries {
		if bytes.Equal(entry.EntityID, raw) {
			return entry, true, nil
		}
	}
	return SystemPinEntry{}, false, nil
}
