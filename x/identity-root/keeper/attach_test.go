package keeper

import (
	"context"
	"testing"

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
	ctx := context.Background()
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
