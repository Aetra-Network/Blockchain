package keeper

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/contracts/types"
)

// TestStoreCodeHashOnlyResubmissionByDifferentWalletCannotWipeOrHijack is the
// exact scenario from the confirmed finding: an owner stores real, verified
// bytecode + manifest for a CodeHash, then a COMPLETELY DIFFERENT active
// wallet resubmits a hash-only MsgStoreCode (same CodeHash, no Bytecode, no
// ManifestBytes, same CodeBytes so it passes the size-bounds check). Before
// the fix, upsertCode fully replaced the CodeRecord by CodeID with no
// merge/ownership logic, so this silently blanked the stored Bytecode and
// ManifestBytes to empty and reassigned Owner to the attacker. After the
// fix, the resubmission must succeed harmlessly (it changes nothing it is
// allowed to change) and the existing Bytecode, ManifestBytes and Owner must
// all be completely unchanged.
func TestStoreCodeHashOnlyResubmissionByDifferentWalletCannotWipeOrHijack(t *testing.T) {
	owner := aeAddress("11")
	attacker := aeAddress("22")
	k := NewKeeperWithAccountStatus(testAccountStatus{owner: accountStatusActive, attacker: accountStatusActive})

	manifest := testInterfaceManifest("wallet-lock")
	hash, err := avm.InterfaceHash(manifest)
	require.NoError(t, err)
	module := minimalAVMModuleWithQueryGetter()
	module.MetadataHash = hash
	bytecode, err := avm.EncodeModule(module)
	require.NoError(t, err)
	manifestBytes, err := json.Marshal(manifest)
	require.NoError(t, err)

	stored, err := k.StoreCode(types.MsgStoreCode{Authority: owner, Bytecode: bytecode, ManifestBytes: manifestBytes})
	require.NoError(t, err)

	before, found, err := k.Code(types.QueryCodeRequest{CodeID: stored.CodeID})
	require.NoError(t, err)
	require.True(t, found)
	require.NotEmpty(t, before.Bytecode)
	require.NotEmpty(t, before.ManifestBytes)
	require.Equal(t, owner, before.Owner)

	// The attack: a different wallet re-submits hash-only, matching CodeBytes
	// so it clears the size-bounds check, but with no Bytecode/ManifestBytes
	// of its own.
	resp, err := k.StoreCode(types.MsgStoreCode{Authority: attacker, CodeHash: stored.CodeID, CodeBytes: before.CodeBytes})
	require.NoError(t, err, "a harmless hash-only re-reference must still succeed")
	require.Equal(t, stored.CodeID, resp.CodeID)

	after, found, err := k.Code(types.QueryCodeRequest{CodeID: stored.CodeID})
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, before.Bytecode, after.Bytecode, "existing bytecode must survive a hash-only resubmission by a non-owner")
	require.Equal(t, before.ManifestBytes, after.ManifestBytes, "existing manifest must survive a hash-only resubmission by a non-owner")
	require.Equal(t, owner, after.Owner, "owner must never be silently reassigned by a resubmission")
	require.Equal(t, before.CodeBytes, after.CodeBytes)
}

// TestStoreCodeResubmissionByDifferentWalletAddingManifestIsRejected covers
// the other half of the same hole: a different wallet's resubmission that
// WOULD actually change stored content -- here, attaching a manifest to a
// code that so far has none -- must be rejected rather than silently
// applied, since only the original owner may change what a CodeHash's
// record contains. (A manifest is hash-bound to the module's own
// MetadataHash baked into the bytecode, so for a FIXED CodeHash/bytecode
// there is exactly one manifest content that can ever verify; going from "no
// manifest" to "the verifying manifest" is therefore the only realistic
// shape a genuine same-hash content change takes.)
func TestStoreCodeResubmissionByDifferentWalletAddingManifestIsRejected(t *testing.T) {
	owner := aeAddress("11")
	attacker := aeAddress("22")
	k := NewKeeperWithAccountStatus(testAccountStatus{owner: accountStatusActive, attacker: accountStatusActive})

	manifest := testInterfaceManifest("wallet-lock")
	hash, err := avm.InterfaceHash(manifest)
	require.NoError(t, err)
	module := minimalAVMModuleWithQueryGetter()
	module.MetadataHash = hash
	bytecode, err := avm.EncodeModule(module)
	require.NoError(t, err)
	manifestBytes, err := json.Marshal(manifest)
	require.NoError(t, err)

	// Owner stores bytecode only, no manifest yet.
	stored, err := k.StoreCode(types.MsgStoreCode{Authority: owner, Bytecode: bytecode})
	require.NoError(t, err)

	// A different wallet re-submits the identical bytecode (so, necessarily,
	// the identical CodeHash) together with the manifest that verifies
	// against it, attempting to attach a manifest the owner never published.
	_, err = k.StoreCode(types.MsgStoreCode{Authority: attacker, Bytecode: bytecode, ManifestBytes: manifestBytes})
	require.ErrorContains(t, err, types.ErrUnauthorized)

	after, found, err := k.Code(types.QueryCodeRequest{CodeID: stored.CodeID})
	require.NoError(t, err)
	require.True(t, found)
	require.Empty(t, after.ManifestBytes, "a non-owner must not be able to attach a manifest to someone else's code")
	require.Equal(t, owner, after.Owner)
}

// TestStoreCodeOwnerCanLegitimatelyUpdateManifestForExistingCode confirms the
// fix does not break the legitimate use case: the TRUE owner of an existing
// bytecode-only CodeHash later publishing a manifest for their own code (the
// same "add a manifest to already-stored bytecode" content change rejected
// for a non-owner above) must still succeed.
func TestStoreCodeOwnerCanLegitimatelyUpdateManifestForExistingCode(t *testing.T) {
	owner := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{owner: accountStatusActive})

	manifest := testInterfaceManifest("wallet-lock")
	hash, err := avm.InterfaceHash(manifest)
	require.NoError(t, err)
	module := minimalAVMModuleWithQueryGetter()
	module.MetadataHash = hash
	bytecode, err := avm.EncodeModule(module)
	require.NoError(t, err)
	manifestBytes, err := json.Marshal(manifest)
	require.NoError(t, err)

	stored, err := k.StoreCode(types.MsgStoreCode{Authority: owner, Bytecode: bytecode})
	require.NoError(t, err)

	empty, found, err := k.Code(types.QueryCodeRequest{CodeID: stored.CodeID})
	require.NoError(t, err)
	require.True(t, found)
	require.Empty(t, empty.ManifestBytes)

	resp, err := k.StoreCode(types.MsgStoreCode{Authority: owner, Bytecode: bytecode, ManifestBytes: manifestBytes})
	require.NoError(t, err, "the true owner must still be able to publish/update their own code's manifest")
	require.Equal(t, stored.CodeID, resp.CodeID)

	after, found, err := k.Code(types.QueryCodeRequest{CodeID: stored.CodeID})
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, manifestBytes, after.ManifestBytes)
	require.Equal(t, owner, after.Owner)
}

// TestStoreCodeHashOnlyFirstRegistrationStillWorks pins that a hash-only
// StoreCode for a BRAND NEW CodeHash (no existing record at all) keeps
// working exactly as before -- the merge-preserve/ownership logic only
// engages when a record already exists.
func TestStoreCodeHashOnlyFirstRegistrationStillWorks(t *testing.T) {
	wallet := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{wallet: accountStatusActive})

	resp, err := k.StoreCode(types.MsgStoreCode{Authority: wallet, CodeHash: sha256Hex("brand-new"), CodeBytes: 128})
	require.NoError(t, err)

	record, found, err := k.Code(types.QueryCodeRequest{CodeID: resp.CodeID})
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, wallet, record.Owner)
	require.Empty(t, record.Bytecode)
	require.Empty(t, record.ManifestBytes)
}
