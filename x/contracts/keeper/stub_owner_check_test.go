package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/contracts/types"
)

// TestExecuteContractStubPathRequiresOwnerForNonExecutableCode is the
// regression guard for the owner-check half of FINDING-004: even after
// StoreCode gained the real avm.DecodeModule + Verify gate, a code record
// with NO Bytecode at all (only CodeHash/CodeBytes -- the deliberately
// still-supported lightweight registration flow used by storeContractCode
// elsewhere in this package) remains executable=false forever, since
// loadAVMModule's `len(code.Bytecode) == 0` early return never even calls
// DecodeModule. executeContract's non-executable stub branch writes the raw
// external payload straight into Contract.Data with no access control
// beyond the contract's lifecycle status -- so before this fix, ANY active
// wallet (not just the contract's owner) could overwrite that public data
// blob. This proves the stub path is still reachable post-fix, and that it
// now requires the caller to be the contract's owner.
func TestExecuteContractStubPathRequiresOwnerForNonExecutableCode(t *testing.T) {
	owner := aeAddress("11")
	attacker := aeAddress("22")
	k := NewKeeperWithAccountStatus(testAccountStatus{owner: accountStatusActive, attacker: accountStatusActive})

	codeHash := sha256Hex("stub-owner-check-code")
	_, err := k.StoreCode(types.MsgStoreCode{Authority: owner, CodeHash: codeHash, CodeBytes: 128})
	require.NoError(t, err)

	deployed, err := k.InstantiateContract(types.MsgInstantiateContract{
		Creator: owner,
		CodeID:  codeHash,
		InitMsg: []byte("init"),
		Funds:   1_000_000,
		Admin:   owner,
		Salt:    "stub-owner-check",
		Height:  10,
	})
	require.NoError(t, err)
	require.Equal(t, owner, deployed.Owner, "test assumption: the deployer is the contract owner")

	// The attacker (an unrelated but active wallet) must be rejected, and
	// must not be able to mutate the contract's data.
	_, err = k.ExecuteContract(types.MsgExecuteContract{
		Sender:          attacker,
		ContractAddress: deployed.ContractAddressUser,
		Msg:             []byte("attacker-write"),
		Height:          11,
	})
	require.ErrorContains(t, err, types.ErrUnauthorized)
	afterAttack, err := k.Contract(types.QueryContractRequest{ContractAddress: deployed.ContractAddressUser})
	require.NoError(t, err)
	require.Equal(t, []byte("init"), afterAttack.Contract.Data, "a rejected write must not mutate the non-executable contract's data (still the InitMsg from instantiate)")

	// The owner itself can still write to its own non-executable contract --
	// this is the pre-existing, intended stub behavior, not itself a bug.
	_, err = k.ExecuteContract(types.MsgExecuteContract{
		Sender:          owner,
		ContractAddress: deployed.ContractAddressUser,
		Msg:             []byte("owner-write"),
		Height:          12,
	})
	require.NoError(t, err)
	afterOwner, err := k.Contract(types.QueryContractRequest{ContractAddress: deployed.ContractAddressUser})
	require.NoError(t, err)
	require.Equal(t, []byte("owner-write"), afterOwner.Contract.Data)
}
