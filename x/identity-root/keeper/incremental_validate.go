package keeper

import (
	"errors"
	"fmt"

	"github.com/sovereign-l1/l1/x/identity-root/types"
)

// This file is FD-02's fix: it replaces the per-mutation full O(state)
// GenesisState.Validate() (which re-Exports and re-checks every one of the
// module's collections on EVERY write) with a set of O(touched) checks that
// reproduce the EXACT accept/reject decision of the full validator for every
// invariant reachable from a single message. See the investigation notes for
// the full case-by-case justification; the summary is:
//
//   - params.Validate() and the two collection-size caps are O(1) and stay on
//     every write (validateGlobal).
//   - Per-record field validity (record.Validate / resolver.Validate / ...)
//     is checked ONLY for the entity the handler just touched, not the whole
//     collection -- every type's Validate method already self-normalizes and
//     is O(1) in the size of the state.
//   - Name uniqueness, resolver/reverse/binding name-references, and
//     reverse/binding owner-matches are already enforced incrementally by the
//     handlers' own upsert/index guards (see collection_test.go /
//     keeper_test.go); they are NOT re-checked here because the full
//     validator's version of them is redundant, not additive.
//   - Three cross-record invariants are ONLY caught by the full validator and
//     are NOT otherwise enforced by any handler: (1) a reserved name must not
//     end up owned by a non-authority (reachable via ReserveName over an
//     already-registered name, CreateSubdomain of a reserved FQDN, TransferName
//     to a non-authority, and an auction grant), (2) an owner_only child's
//     owner must track its parent's owner (reachable via TransferName of
//     either side), (3) the two size caps. These are replicated exactly below
//     (checkReservedOwnership, transferPreservesSubdomainOwnershipPolicy,
//     validateGlobal) so an operation that the OLD full-Validate path used to
//     reject still gets rejected -- flipping any of these from reject to
//     accept would commit different bytes than the pre-upgrade binary for the
//     same message sequence (an AppHash split).
//
// Full IdentityRootState.Validate() (which Exports and walks every collection)
// remains the ONLY validator at genesis/import/migration boundaries
// (GenesisState.Validate, InitGenesis, InitGenesisState, ExportGenesisState,
// Migrate1to2/Migrate2to3State, module.ValidateGenesis), where arbitrary
// external state must be checked exhaustively. It is deliberately left alone.

// validateGlobal runs the O(1) checks every mutation must still pass:
// prototype.Params.Validate() and IdentityRootParams.Validate() (the
// "params.Validate() -- O(1)" invariant), plus the three collection-size caps
// (len(Records) <= MaxRecords, len(ReservedNames) <= MaxReservedNames,
// len(Auctions) <= MaxAuctions) that the old code ONLY caught via a full
// Validate. len() is O(1) on a Go slice, so this never scans a collection.
// The Auctions cap is also enforced EARLY, at both open-auction call sites
// (registerViaCollectionLocked, StartAuction in collection.go) -- this check
// is the backstop, not the primary guard, matching the Records/ReservedNames
// caps' existing pattern.
func validateGlobal(gs GenesisState) error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}
	if err := gs.IdentityParams.Validate(); err != nil {
		return err
	}
	if uint32(len(gs.State.Records)) > gs.IdentityParams.MaxRecords {
		return errors.New("identity root record count exceeds limit")
	}
	if uint32(len(gs.State.ReservedNames)) > gs.IdentityParams.MaxReservedNames {
		return errors.New("identity root reserved name count exceeds limit")
	}
	if uint32(len(gs.State.Auctions)) > gs.IdentityParams.MaxAuctions {
		return errors.New("identity root auction count exceeds limit")
	}
	return nil
}

// checkReservedOwnership reproduces state.go's "reserved identity name cannot
// be owned by normal user" cross-check (the reserved x record-owner pairing),
// scoped to a single name: an O(1) reserved-set membership test plus an O(1)
// record lookup. It is called from whichever side of the pairing a handler
// just changed -- ReserveName (the reserved side), RegisterName/CreateSubdomain
// /an auction grant (the record side, though RegisterName/CreateSubdomain
// already reject this before ever building `next`), and TransferName (the
// record side, where the owner change is the only way this can newly break).
func checkReservedOwnership(gs GenesisState, name string) error {
	if !isReserved(gs.State.ReservedNames, name) {
		return nil
	}
	_, record, found := recordIndex(gs.State.Records, name)
	if !found {
		return nil
	}
	if isRootAuthority(gs.State.RootAuthorities, record.Owner) {
		return nil
	}
	return fmt.Errorf("reserved identity name %q cannot be owned by normal user", record.Name)
}

// transferPreservesSubdomainOwnershipPolicy reproduces state.go's
// "identity subdomain %q must follow parent ownership policy" cross-check for
// the two directions a single TransferName of `name` can violate it (`gs` must
// already carry the record's NEW owner):
//
//   - the CHILD side: if `name` is itself a child of an owner_only parent, the
//     new owner must still equal the parent's owner (O(1): one parent lookup).
//   - the PARENT side: if `name` is itself an owner_only parent, every child
//     record naming it as ParentName must still match its (new) owner
//     (O(children of `name`), never O(total records)).
func transferPreservesSubdomainOwnershipPolicy(gs GenesisState, name string) error {
	records := gs.State.Records
	_, record, found := recordIndex(records, name)
	if !found {
		return nil
	}
	if record.ParentName != "" {
		if _, parent, found := recordIndex(records, record.ParentName); found {
			if parent.SubdomainPolicy == types.SubdomainPolicyOwnerOnly && record.Owner != parent.Owner {
				return fmt.Errorf("identity subdomain %q must follow parent ownership policy", record.Name)
			}
		}
	}
	if record.SubdomainPolicy == types.SubdomainPolicyOwnerOnly {
		for _, child := range records {
			if child.ParentName == name && child.Owner != record.Owner {
				return fmt.Errorf("identity subdomain %q must follow parent ownership policy", child.Name)
			}
		}
	}
	return nil
}

// requireParentPolicySatisfied reproduces state.go's cross-record parent
// invariant (state.go:374-385) for a single about-to-be-created record:
// every record with a non-empty ParentName must reference an EXISTING parent
// record, a Disabled parent rejects any child outright, and an OwnerOnly
// parent requires the child's owner to equal the parent's owner. The full
// validator walks every record's ParentName unconditionally; RegisterName can
// mint an arbitrary-depth dotted name via types.ParentName (e.g.
// "a.b.aet") without ever going through CreateSubdomain's requireOwnedName
// (which enforces a DIFFERENT, stronger, msg-level rule: the CALLER must own
// the parent). This check restores exactly the old accept/reject decision --
// parent-must-exist-and-satisfy-policy -- and nothing more: RegisterName does
// not require the caller to own the parent.
func requireParentPolicySatisfied(records []types.NameRecord, parentName, childName, childOwner string) error {
	if parentName == "" {
		return nil
	}
	_, parent, found := recordIndex(records, parentName)
	if !found {
		return fmt.Errorf("identity subdomain %q references missing parent", childName)
	}
	if parent.SubdomainPolicy == types.SubdomainPolicyDisabled {
		return fmt.Errorf("identity subdomain %q is disabled by parent policy", childName)
	}
	if parent.SubdomainPolicy == types.SubdomainPolicyOwnerOnly && childOwner != parent.Owner {
		return fmt.Errorf("identity subdomain %q must follow parent ownership policy", childName)
	}
	return nil
}

// validateGrantedName is the EndBlocker-side counterpart of
// checkReservedOwnership + per-record Validate for a name an auction just
// granted (or re-granted) to its winner. A no-op grant that leaves an invalid
// record here is exactly the kind of previously-halting condition (full
// Validate rejected it, which propagated as an EndBlocker error = deterministic
// chain halt) that must keep halting identically after this change.
func validateGrantedName(gs GenesisState, name string) error {
	_, record, found := recordIndex(gs.State.Records, name)
	if !found {
		return fmt.Errorf("identity auction grant for %q left no record", name)
	}
	if err := record.Validate(gs.IdentityParams); err != nil {
		return err
	}
	if err := checkReservedOwnership(gs, name); err != nil {
		return err
	}
	// A grant re-owns the record exactly like TransferName does, so it can
	// break the same owner_only-parent<->child invariant (e.g. an
	// owner-listed auction on a domain that has owner_only children): the
	// investigation's per-handler table only calls this out for TransferName,
	// but grantAuctionName performs the identical Owner mutation and the old
	// full Validate walked every record regardless of which handler produced
	// it, so it must be checked here too.
	return transferPreservesSubdomainOwnershipPolicy(gs, name)
}
