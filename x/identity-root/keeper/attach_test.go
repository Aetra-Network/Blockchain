package keeper

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/app/addressing"
	"github.com/sovereign-l1/l1/x/identity-root/types"
	"github.com/sovereign-l1/l1/x/internal/kvtest"
	"github.com/sovereign-l1/l1/x/internal/prototype"
)

// targetUser is a plain user account (classifies as native_account) -- an
// allowed attach target.
var targetUser = mustAE("33")

func systemAddrUF(t *testing.T, name string) string {
	t.Helper()
	addr, found := addressing.SystemAddressByName(name)
	require.True(t, found, "system address %q must exist", name)
	return addr.UserFriendly
}

func moduleAddrUF(t *testing.T, moduleName string) string {
	t.Helper()
	uf, err := addressing.FormatUserFriendly(authtypes.NewModuleAddress(moduleName))
	require.NoError(t, err)
	return uf
}

// blockCtx builds an sdk.Context carrying only a block height -- enough for the
// expiry-aware fee gate (AccountHoldsDomain reads the height off it). The kvtest
// store service ignores the context, so no multistore is needed.
func blockCtx(height int64) sdk.Context {
	return sdk.Context{}.WithBlockHeight(height)
}

// TestAttachDomainAllowsNativeAccount is the happy path: an owned, active FQDN
// attaches to a plain user account (a native_account -- a user contract would
// classify the same way and is equally allowed).
func TestAttachDomainAllowsNativeAccount(t *testing.T) {
	k := setupKeeper(t)
	_, err := k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "alice", Height: 10})
	require.NoError(t, err)

	att, err := k.AttachDomain(types.MsgAttachDomain{Owner: ownerA, Fqdn: "alice", Target: targetUser, Height: 11})
	require.NoError(t, err)
	require.Equal(t, "alice.aet", att.Fqdn)
	require.Equal(t, targetUser, att.Target)
	require.Len(t, att.TargetIdentityHex, 64, "target identity must be 32-byte hex")
}

// TestAttachDomainRejectsForbiddenTargets proves the classifier guard: a system
// catalog entity (the .aet collection itself), the nominator pools, a
// bonded-staking module account, and a dns name string are all rejected. These
// are the negatives that must fail; the allow case above is the positive.
func TestAttachDomainRejectsForbiddenTargets(t *testing.T) {
	cases := []struct {
		name	string
		target	string
	}{
		{"collection catalog (AETIdentityRoot)", systemAddrUF(t, "AETIdentityRoot")},
		{"nominator pool", systemAddrUF(t, "AETNominatorPool")},
		{"single nominator pool", systemAddrUF(t, "AETSingleNominatorPool")},
		{"staking bonded pool", moduleAddrUF(t, stakingtypes.BondedPoolName)},
		{"staking not-bonded pool", moduleAddrUF(t, stakingtypes.NotBondedPoolName)},
		{"dns name string", "daniil.aet"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			k := setupKeeper(t)
			_, err := k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "alice", Height: 10})
			require.NoError(t, err)

			_, err = k.AttachDomain(types.MsgAttachDomain{Owner: ownerA, Fqdn: "alice", Target: tc.target, Height: 11})
			require.Error(t, err, "attach to %s must be rejected", tc.name)
		})
	}
}

// TestAttachDomainRequiresOwnedActiveName rejects an attach by a non-owner and
// for an unregistered name.
func TestAttachDomainRequiresOwnedActiveName(t *testing.T) {
	k := setupKeeper(t)
	_, err := k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "alice", Height: 10})
	require.NoError(t, err)

	_, err = k.AttachDomain(types.MsgAttachDomain{Owner: ownerB, Fqdn: "alice", Target: targetUser, Height: 11})
	require.ErrorContains(t, err, "requires owner")

	_, err = k.AttachDomain(types.MsgAttachDomain{Owner: ownerA, Fqdn: "ghost", Target: targetUser, Height: 11})
	require.ErrorContains(t, err, "not found")
}

// TestAttachDomainRejectsSecondAttachForSameWallet is the one-domain-per-wallet
// invariant: a second attach whose Target is a wallet already holding a domain
// is rejected.
func TestAttachDomainRejectsSecondAttachForSameWallet(t *testing.T) {
	k := setupKeeper(t)
	_, err := k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "alice", Height: 10})
	require.NoError(t, err)
	_, err = k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "bob", Height: 10})
	require.NoError(t, err)

	_, err = k.AttachDomain(types.MsgAttachDomain{Owner: ownerA, Fqdn: "alice", Target: targetUser, Height: 11})
	require.NoError(t, err)

	_, err = k.AttachDomain(types.MsgAttachDomain{Owner: ownerA, Fqdn: "bob", Target: targetUser, Height: 12})
	require.ErrorContains(t, err, "already holds")
}

// TestAttachDomainRejectsSecondAttachForSameName rejects attaching the same
// name twice (to different wallets).
func TestAttachDomainRejectsSecondAttachForSameName(t *testing.T) {
	k := setupKeeper(t)
	_, err := k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "alice", Height: 10})
	require.NoError(t, err)

	_, err = k.AttachDomain(types.MsgAttachDomain{Owner: ownerA, Fqdn: "alice", Target: targetUser, Height: 11})
	require.NoError(t, err)

	_, err = k.AttachDomain(types.MsgAttachDomain{Owner: ownerA, Fqdn: "alice", Target: mustAE("44"), Height: 12})
	require.ErrorContains(t, err, "already attached")
}

// TestDetachDomainClearsAttachment proves detach frees the wallet: after detach,
// the same wallet can hold a different name's attachment.
func TestDetachDomainClearsAttachment(t *testing.T) {
	k := setupKeeper(t)
	_, err := k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "alice", Height: 10})
	require.NoError(t, err)
	_, err = k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "bob", Height: 10})
	require.NoError(t, err)

	_, err = k.AttachDomain(types.MsgAttachDomain{Owner: ownerA, Fqdn: "alice", Target: targetUser, Height: 11})
	require.NoError(t, err)

	// Same wallet is blocked while alice is attached.
	_, err = k.AttachDomain(types.MsgAttachDomain{Owner: ownerA, Fqdn: "bob", Target: targetUser, Height: 12})
	require.Error(t, err)

	detached, err := k.DetachDomain(types.MsgDetachDomain{Owner: ownerA, Fqdn: "alice", Height: 13})
	require.NoError(t, err)
	require.Equal(t, "alice.aet", detached.Fqdn)

	// After detach the wallet is free again.
	_, err = k.AttachDomain(types.MsgAttachDomain{Owner: ownerA, Fqdn: "bob", Target: targetUser, Height: 14})
	require.NoError(t, err)
}

// TestDetachDomainRejectsMissingAttachment rejects a detach for a name with no
// attachment.
func TestDetachDomainRejectsMissingAttachment(t *testing.T) {
	k := setupKeeper(t)
	_, err := k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "alice", Height: 10})
	require.NoError(t, err)

	_, err = k.DetachDomain(types.MsgDetachDomain{Owner: ownerA, Fqdn: "alice", Height: 11})
	require.ErrorContains(t, err, "no attachment")
}

// TestAccountHoldsDomainReadsCommittedStore is the fee-gate reader over a REAL
// store: a wallet reads back as holding a domain only after an attach commits,
// and reads back as not holding it after a detach commits. This is the exact
// O(1) presence read the ante fee gate performs.
func TestAccountHoldsDomainReadsCommittedStore(t *testing.T) {
	svc := kvtest.NewStoreService()
	kv := NewPersistentKeeper(svc)
	k := &kv
	gs := DefaultGenesis()
	gs.Params = prototype.TestnetParams()
	gs.IdentityParams.RegistrationPeriod = 100
	// The fee gate is expiry-aware, so it needs a block height. Registration at
	// msg-height 10 with period 100 yields ExpiryHeight 110; height 50 is active.
	ctx := blockCtx(50)
	require.NoError(t, k.InitGenesisState(ctx, gs))
	require.NoError(t, k.loadForBlock(ctx))

	_, err := k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "alice", Height: 10})
	require.NoError(t, err)

	targetAddr := accAddr(t, targetUser)

	holds, err := k.AccountHoldsDomain(ctx, targetAddr)
	require.NoError(t, err)
	require.False(t, holds, "wallet holds no domain before attach")

	_, err = k.AttachDomain(types.MsgAttachDomain{Owner: ownerA, Fqdn: "alice", Target: targetUser, Height: 11})
	require.NoError(t, err)

	holds, err = k.AccountHoldsDomain(ctx, targetAddr)
	require.NoError(t, err)
	require.True(t, holds, "wallet must read back as holding a domain after a committed attach")

	// A different wallet is unaffected.
	otherHolds, err := k.AccountHoldsDomain(ctx, accAddr(t, ownerB))
	require.NoError(t, err)
	require.False(t, otherHolds, "an unrelated wallet must not read as holding a domain")

	_, err = k.DetachDomain(types.MsgDetachDomain{Owner: ownerA, Fqdn: "alice", Height: 12})
	require.NoError(t, err)

	holds, err = k.AccountHoldsDomain(ctx, targetAddr)
	require.NoError(t, err)
	require.False(t, holds, "detach must clear the fee-gate index in the committed store")
}

// TestDisownAttachmentClearsGriefAttach is FIX A: an FQDN owner grief-attaches to
// a victim wallet (occupying its one-domain slot without consent); the victim,
// who does NOT own the FQDN, self-disowns the attachment aimed at its own wallet,
// and the fee gate reads false afterward. The victim never touches the domain.
func TestDisownAttachmentClearsGriefAttach(t *testing.T) {
	svc := kvtest.NewStoreService()
	kv := NewPersistentKeeper(svc)
	k := &kv
	gs := DefaultGenesis()
	gs.Params = prototype.TestnetParams()
	gs.IdentityParams.RegistrationPeriod = 100
	ctx := blockCtx(50)
	require.NoError(t, k.InitGenesisState(ctx, gs))
	require.NoError(t, k.loadForBlock(ctx))

	// ownerA owns alice.aet and grief-attaches it to the victim wallet.
	_, err := k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "alice", Height: 10})
	require.NoError(t, err)
	_, err = k.AttachDomain(types.MsgAttachDomain{Owner: ownerA, Fqdn: "alice", Target: targetUser, Height: 11})
	require.NoError(t, err)

	victimAddr := accAddr(t, targetUser)
	holds, err := k.AccountHoldsDomain(ctx, victimAddr)
	require.NoError(t, err)
	require.True(t, holds, "the grief attach must occupy the victim's one-domain slot")

	// The victim self-disowns -- no owned-name check, the signer is the target.
	disowned, err := k.DisownAttachment(types.MsgDisownAttachment{Target: targetUser, Height: 12})
	require.NoError(t, err)
	require.Equal(t, "alice.aet", disowned.Fqdn)

	holds, err = k.AccountHoldsDomain(ctx, victimAddr)
	require.NoError(t, err)
	require.False(t, holds, "after the victim disowns, the fee-gate index must be clear")

	// The name is freed too: ownerA can attach alice.aet somewhere else again.
	_, err = k.AttachDomain(types.MsgAttachDomain{Owner: ownerA, Fqdn: "alice", Target: mustAE("44"), Height: 13})
	require.NoError(t, err)
}

// TestDisownAttachmentByNonTargetFails proves only the wallet an attachment
// points AT can disown it: a different wallet's disown finds no attachment under
// its own identity and errors, so it cannot clear someone else's slot. (The
// signer resolver binds the signature to the target field, so on the wire a
// non-target could not even author this message for the victim's identity.)
func TestDisownAttachmentByNonTargetFails(t *testing.T) {
	svc := kvtest.NewStoreService()
	kv := NewPersistentKeeper(svc)
	k := &kv
	gs := DefaultGenesis()
	gs.Params = prototype.TestnetParams()
	gs.IdentityParams.RegistrationPeriod = 100
	ctx := blockCtx(50)
	require.NoError(t, k.InitGenesisState(ctx, gs))
	require.NoError(t, k.loadForBlock(ctx))

	_, err := k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "alice", Height: 10})
	require.NoError(t, err)
	_, err = k.AttachDomain(types.MsgAttachDomain{Owner: ownerA, Fqdn: "alice", Target: targetUser, Height: 11})
	require.NoError(t, err)

	// A wallet that is NOT the attachment target has no attachment under its own
	// identity, so its disown errors and the victim's slot stays occupied.
	_, err = k.DisownAttachment(types.MsgDisownAttachment{Target: mustAE("55"), Height: 12})
	require.ErrorContains(t, err, "no attachment")

	holds, err := k.AccountHoldsDomain(ctx, accAddr(t, targetUser))
	require.NoError(t, err)
	require.True(t, holds, "a non-target disown must not clear the victim's attachment")

	// The victim itself can still disown.
	_, err = k.DisownAttachment(types.MsgDisownAttachment{Target: targetUser, Height: 13})
	require.NoError(t, err)
}

// TestAccountHoldsDomainExpiryAware is FIX B: the fee gate reads the LIVE domain
// record and returns false once the referenced domain lapses by passive expiry
// (no detach, no transfer event) -- and true again after a renewal. Same
// persistent-store harness as TestAccountHoldsDomainReadsCommittedStore.
func TestAccountHoldsDomainExpiryAware(t *testing.T) {
	svc := kvtest.NewStoreService()
	kv := NewPersistentKeeper(svc)
	k := &kv
	gs := DefaultGenesis()
	gs.Params = prototype.TestnetParams()
	gs.IdentityParams.RegistrationPeriod = 100
	gs.IdentityParams.RenewalWindowBlocks = 100
	// Register at height 10 -> ExpiryHeight 110. The block context supplies the
	// height the gate compares against.
	require.NoError(t, k.InitGenesisState(blockCtx(50), gs))
	require.NoError(t, k.loadForBlock(blockCtx(50)))

	_, err := k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "alice", Height: 10})
	require.NoError(t, err)
	_, err = k.AttachDomain(types.MsgAttachDomain{Owner: ownerA, Fqdn: "alice", Target: targetUser, Height: 11})
	require.NoError(t, err)

	targetAddr := accAddr(t, targetUser)

	// Active domain (height 50 < expiry 110): gate true.
	holds, err := k.AccountHoldsDomain(blockCtx(50), targetAddr)
	require.NoError(t, err)
	require.True(t, holds, "an attached, active domain must gate true")

	// Height advanced past ExpiryHeight 110: the attachment is untouched, but the
	// domain has passively expired, so the gate must read false.
	holds, err = k.AccountHoldsDomain(blockCtx(150), targetAddr)
	require.NoError(t, err)
	require.False(t, holds, "a lapsed domain must un-gate even though the attachment was never cleared")

	// Renew inside the window (before expiry 110): ExpiryHeight -> 210.
	require.NoError(t, k.loadForBlock(blockCtx(105)))
	_, err = k.RenewName(types.MsgRenewName{Owner: ownerA, Name: "alice", Height: 105})
	require.NoError(t, err)

	// Same height 150 now reads active again (150 < new expiry 210): gate true.
	holds, err = k.AccountHoldsDomain(blockCtx(150), targetAddr)
	require.NoError(t, err)
	require.True(t, holds, "a renewed domain must gate true again at a height it previously failed")
}

// TestTransferNameClearsAttachment is the regression for the audit finding that a
// domain SALE carried the reputation fee discount to the seller. The attachment
// is exactly what the ante fee gate (AccountHoldsDomain) reads, so a transfer
// must clear it: the old target loses the discount and the buyer must re-attach.
func TestTransferNameClearsAttachment(t *testing.T) {
	svc := kvtest.NewStoreService()
	kv := NewPersistentKeeper(svc)
	k := &kv
	gs := DefaultGenesis()
	gs.Params = prototype.TestnetParams()
	gs.IdentityParams.RegistrationPeriod = 100
	ctx := blockCtx(50)
	require.NoError(t, k.InitGenesisState(ctx, gs))
	require.NoError(t, k.loadForBlock(ctx))

	_, err := k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "alice", Height: 10})
	require.NoError(t, err)
	_, err = k.AttachDomain(types.MsgAttachDomain{Owner: ownerA, Fqdn: "alice", Target: targetUser, Height: 11})
	require.NoError(t, err)

	targetAddr := accAddr(t, targetUser)
	holds, err := k.AccountHoldsDomain(ctx, targetAddr)
	require.NoError(t, err)
	require.True(t, holds, "target holds the attached domain before the sale")

	// Sell alice.aet to a new owner.
	_, err = k.TransferName(types.MsgTransferName{Owner: ownerA, Name: "alice", NewOwner: ownerB, Height: 12})
	require.NoError(t, err)

	// The reputation discount must NOT survive the sale: the attachment (and its
	// AttachKey store entry) is gone, so the old target no longer reads as holding.
	holds, err = k.AccountHoldsDomain(ctx, targetAddr)
	require.NoError(t, err)
	require.False(t, holds, "reputation discount must not carry to the seller after a domain sale")
}
