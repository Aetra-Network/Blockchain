package keeper

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sovereign-l1/l1/app/accounts"
	"github.com/sovereign-l1/l1/app/addressing"
	aeztypes "github.com/sovereign-l1/l1/x/aez/types"
	"github.com/sovereign-l1/l1/x/identity-root/types"
)

// This file is the ANS Phase B attach surface: MsgAttachDomain / MsgDetachDomain
// and the fee-gate reader AccountHoldsDomain.
//
// Attach points an owned FQDN at a target wallet and records it in a per-wallet
// index (one domain per wallet). The target is classified with x/aez
// CanonicalEntityID (system-FIRST, on raw bytes): only a user contract or a
// native_account is allowed. A system entity -- the .aet collection itself, the
// nominator pools, the staking bonded/not-bonded pools, any reserved catalog
// vanity address -- is rejected, as is a dns name string (it fails address
// parsing) and, belt-and-suspenders, anything in IsReservedSystemAddressText or
// the bank BlockedAddresses set. The classifier's system pin set is the
// authoritative guard; the other two are redundant supplements.
//
// The index is keyed by the TARGET wallet's canonical v2 identity, the same
// identity the reputation fee gate derives from a tx sender, so a wallet that a
// domain is attached to reads back as "holds a domain" at ante time.

// AttachDomain records msg.Fqdn (which msg.Owner must own and be active) as
// attached to msg.Target. Rejects a system/pool/staking entity or a dns item,
// and rejects a second attachment for the same target wallet or the same name.
func (k *Keeper) AttachDomain(msg types.MsgAttachDomain) (types.Attachment, error) {
	k.lockW()
	defer k.unlockW()
	if err := k.requireEnabled(); err != nil {
		return types.Attachment{}, err
	}
	if msg.Height == 0 {
		return types.Attachment{}, errors.New("identity attach height must be positive")
	}
	_, record, err := k.requireOwnedName(msg.Fqdn, msg.Owner, msg.Height, true)
	if err != nil {
		return types.Attachment{}, err
	}
	_, canonicalID, err := classifyAttachTarget(msg.Target)
	if err != nil {
		return types.Attachment{}, err
	}
	targetIdentityHex := hex.EncodeToString(canonicalID)
	if _, _, found := attachmentIndexByTarget(k.genesis.State.Attachments, targetIdentityHex); found {
		return types.Attachment{}, errors.New("identity target wallet already holds an attached domain")
	}
	if _, _, found := attachmentIndexByName(k.genesis.State.Attachments, record.Name); found {
		return types.Attachment{}, errors.New("identity name is already attached")
	}
	attachment := types.Attachment{
		Fqdn:			record.Name,
		Target:			strings.TrimSpace(msg.Target),
		TargetIdentityHex:	targetIdentityHex,
		Owner:			msg.Owner,
		CreatedHeight:		msg.Height,
		UpdatedHeight:		msg.Height,
	}.Normalize(k.genesis.IdentityParams)
	next := cloneGenesis(k.genesis)
	next.State.Attachments = upsertAttachment(next.State.Attachments, attachment)
	next.State = next.State.Export()
	if err := next.Validate(); err != nil {
		return types.Attachment{}, err
	}
	if err := k.persistLocked(next); err != nil {
		return types.Attachment{}, err
	}
	return attachment, nil
}

// DetachDomain clears the attachment for an owned FQDN. Active ownership is not
// required so the owner can clean up an attachment on an expired domain.
func (k *Keeper) DetachDomain(msg types.MsgDetachDomain) (types.Attachment, error) {
	k.lockW()
	defer k.unlockW()
	if err := k.requireEnabled(); err != nil {
		return types.Attachment{}, err
	}
	if msg.Height == 0 {
		return types.Attachment{}, errors.New("identity detach height must be positive")
	}
	_, record, err := k.requireOwnedName(msg.Fqdn, msg.Owner, msg.Height, false)
	if err != nil {
		return types.Attachment{}, err
	}
	_, attachment, found := attachmentIndexByName(k.genesis.State.Attachments, record.Name)
	if !found {
		return types.Attachment{}, errors.New("identity name has no attachment to detach")
	}
	next := cloneGenesis(k.genesis)
	next.State.Attachments = removeAttachmentByName(next.State.Attachments, record.Name)
	next.State = next.State.Export()
	if err := next.Validate(); err != nil {
		return types.Attachment{}, err
	}
	if err := k.persistLocked(next); err != nil {
		return types.Attachment{}, err
	}
	return attachment, nil
}

// AccountHoldsDomain reports whether addr currently holds an attached domain. It
// is the reputation fee gate's reader: a value receiver that reads a SINGLE
// committed store key directly (never k.genesis, since the ante path does not
// loadForBlock), takes no lock, and loads no full state -- an O(1) deterministic
// Get so the ante gas stays flat and identical on every node. Any address or
// store error degrades to false (not-gated) so the gate can never block a tx.
func (k Keeper) AccountHoldsDomain(ctx context.Context, addr sdk.AccAddress) (bool, error) {
	if k.storeService == nil || len(addr) == 0 {
		return false, nil
	}
	identity, err := addressing.NormalizeToAccountIdentity(addr.Bytes())
	if err != nil {
		return false, nil
	}
	bz, err := k.storeService.OpenKVStore(ctx).Get(types.AttachKey(hex.EncodeToString(identity)))
	if err != nil {
		return false, err
	}
	return len(bz) > 0, nil
}

// classifyAttachTarget parses and classifies an attach target, returning its
// AEZ namespace and canonical entity id. It rejects everything except a
// user-controlled account/contract: system entities (via CanonicalEntityID's
// system-first pin set), reserved catalog addresses, and bank-blocked accounts.
func classifyAttachTarget(target string) (aeztypes.Namespace, []byte, error) {
	if err := types.ValidateUserFacingAEAddress("identity attachment target", target); err != nil {
		return "", nil, err
	}
	raw, err := addressing.Parse(target)
	if err != nil {
		return "", nil, fmt.Errorf("identity attachment target: %w", err)
	}
	// System FIRST, on raw bytes: only a user contract or native_account passes.
	ns, canonicalID, err := aeztypes.CanonicalEntityID(aeztypes.EntityKindAddress, raw)
	if err != nil {
		return "", nil, fmt.Errorf("identity attachment target classification failed: %w", err)
	}
	if ns == aeztypes.NamespaceSystem {
		return "", nil, errors.New("identity attachment target is a reserved system entity")
	}
	// Belt-and-suspenders. Redundant with the pin set above, but catches the
	// Layer-B catalog vanity forms and bank-blocked pools explicitly.
	if addressing.IsReservedSystemAddressText(target) {
		return "", nil, errors.New("identity attachment target is a reserved system address")
	}
	if accounts.BlockedAddresses()[sdk.AccAddress(raw).String()] {
		return "", nil, errors.New("identity attachment target is a blocked system account")
	}
	return ns, canonicalID, nil
}

func attachmentIndexByTarget(attachments []types.Attachment, targetIdentityHex string) (int, types.Attachment, bool) {
	for i, a := range attachments {
		if a.TargetIdentityHex == targetIdentityHex {
			return i, a, true
		}
	}
	return -1, types.Attachment{}, false
}

func attachmentIndexByName(attachments []types.Attachment, fqdn string) (int, types.Attachment, bool) {
	for i, a := range attachments {
		if a.Fqdn == fqdn {
			return i, a, true
		}
	}
	return -1, types.Attachment{}, false
}

func upsertAttachment(attachments []types.Attachment, attachment types.Attachment) []types.Attachment {
	out := append([]types.Attachment(nil), attachments...)
	if i, _, found := attachmentIndexByTarget(out, attachment.TargetIdentityHex); found {
		out[i] = attachment
	} else {
		out = append(out, attachment)
	}
	types.SortAttachments(out)
	return out
}

func removeAttachmentByName(attachments []types.Attachment, fqdn string) []types.Attachment {
	out := make([]types.Attachment, 0, len(attachments))
	for _, a := range attachments {
		if a.Fqdn == fqdn {
			continue
		}
		out = append(out, a)
	}
	return out
}
