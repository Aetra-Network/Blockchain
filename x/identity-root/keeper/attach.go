package keeper

import (
	"context"
	"encoding/hex"
	"encoding/json"
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
	next := cloneGenesisUnsorted(k.genesis)
	next.State.Attachments = upsertAttachment(next.State.Attachments, attachment)
	// Incremental validation (FD-02): one-per-wallet and one-per-name
	// uniqueness are already enforced above; only the attachment's own field
	// validity is scoped here.
	if err := validateGlobal(next); err != nil {
		return types.Attachment{}, err
	}
	if err := attachment.Validate(next.IdentityParams); err != nil {
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
	next := cloneGenesisUnsorted(k.genesis)
	next.State.Attachments = removeAttachmentByName(next.State.Attachments, record.Name)
	// Removal-only mutation: cannot newly violate any invariant.
	if err := validateGlobal(next); err != nil {
		return types.Attachment{}, err
	}
	if err := k.persistLocked(next); err != nil {
		return types.Attachment{}, err
	}
	return attachment, nil
}

// DisownAttachment lets the TARGET wallet of an attachment clear it without the
// FQDN owner's cooperation -- the anti-griefing self-detach. AttachDomain lets an
// owner point a name at any allowed wallet without consent, occupying that
// wallet's single one-domain-per-wallet slot; only the owner could DetachDomain,
// so a victim could not clear an unwanted attachment. Here the signer is the
// target itself (see MsgDisownAttachmentSigners), so no owned-name check is
// performed: the target need not own the FQDN, it is disowning an attachment
// aimed at its own account. The target identity is derived with the SAME
// normalization AccountHoldsDomain uses, so the record removed is exactly the one
// the fee gate would have read.
func (k *Keeper) DisownAttachment(msg types.MsgDisownAttachment) (types.Attachment, error) {
	k.lockW()
	defer k.unlockW()
	if err := k.requireEnabled(); err != nil {
		return types.Attachment{}, err
	}
	if msg.Height == 0 {
		return types.Attachment{}, errors.New("identity disown height must be positive")
	}
	if err := types.ValidateUserFacingAEAddress("identity disown target", msg.Target); err != nil {
		return types.Attachment{}, err
	}
	raw, err := addressing.Parse(msg.Target)
	if err != nil {
		return types.Attachment{}, fmt.Errorf("identity disown target: %w", err)
	}
	identity, err := addressing.NormalizeToAccountIdentity(raw)
	if err != nil {
		return types.Attachment{}, fmt.Errorf("identity disown target: %w", err)
	}
	targetIdentityHex := hex.EncodeToString(identity)
	_, attachment, found := attachmentIndexByTarget(k.genesis.State.Attachments, targetIdentityHex)
	if !found {
		return types.Attachment{}, errors.New("identity target wallet holds no attachment to disown")
	}
	next := cloneGenesisUnsorted(k.genesis)
	next.State.Attachments = removeAttachmentByTargetHex(next.State.Attachments, targetIdentityHex)
	// Removal-only mutation: cannot newly violate any invariant.
	if err := validateGlobal(next); err != nil {
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
// It reads the attachment's referenced domain RECORD from the committed store and
// returns true ONLY if that domain still exists AND is ACTIVE at the current
// block height (types.IsActive, the same predicate requireOwnedName uses). This
// closes the passive-expiry hole: an attachment is never cleared when its domain
// lapses (expiry fires no transfer), so a stale len>0 read would have carried the
// fee discount past expiry. The height comes from the ctx's sdk.Context; any
// missing/short-circuit condition degrades to false so the gate never blocks a tx.
func (k Keeper) AccountHoldsDomain(ctx context.Context, addr sdk.AccAddress) (bool, error) {
	if k.storeService == nil || len(addr) == 0 {
		return false, nil
	}
	identity, err := addressing.NormalizeToAccountIdentity(addr.Bytes())
	if err != nil {
		return false, nil
	}
	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get(types.AttachKey(hex.EncodeToString(identity)))
	if err != nil {
		return false, err
	}
	if len(bz) == 0 {
		return false, nil
	}
	var attachment types.Attachment
	if err := json.Unmarshal(bz, &attachment); err != nil {
		// A corrupt index entry degrades to not-gated rather than blocking the tx.
		return false, nil
	}
	recordBz, err := store.Get(types.NameKey(attachment.Fqdn))
	if err != nil {
		return false, err
	}
	if len(recordBz) == 0 {
		// The referenced domain record is gone: not an active holding.
		return false, nil
	}
	var record types.NameRecord
	if err := json.Unmarshal(recordBz, &record); err != nil {
		return false, nil
	}
	return types.IsActive(record, currentBlockHeight(ctx)), nil
}

// currentBlockHeight extracts the deterministic block height from ctx without
// panicking. It mirrors sdk.UnwrapSDKContext's two-branch lookup (a direct
// sdk.Context, or one stashed under SdkContextKey) but returns 0 when neither is
// present -- height 0 makes types.IsActive report false, the safe (un-gated)
// direction, honoring the "any error degrades to false" contract for a caller
// that did not supply a block context.
func currentBlockHeight(ctx context.Context) uint64 {
	if sdkCtx, ok := ctx.(sdk.Context); ok {
		return heightAsUint64(sdkCtx.BlockHeight())
	}
	if v := ctx.Value(sdk.SdkContextKey); v != nil {
		if sdkCtx, ok := v.(sdk.Context); ok {
			return heightAsUint64(sdkCtx.BlockHeight())
		}
	}
	return 0
}

func heightAsUint64(height int64) uint64 {
	if height <= 0 {
		return 0
	}
	return uint64(height)
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

func removeAttachmentByTargetHex(attachments []types.Attachment, targetIdentityHex string) []types.Attachment {
	out := make([]types.Attachment, 0, len(attachments))
	for _, a := range attachments {
		if a.TargetIdentityHex == targetIdentityHex {
			continue
		}
		out = append(out, a)
	}
	return out
}
