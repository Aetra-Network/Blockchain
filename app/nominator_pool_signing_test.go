package app

import (
	"bytes"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/app/addressing"
	nominatorpooltypes "github.com/sovereign-l1/l1/x/nominator-pool/types"
)

// TestNominatorPoolMessagesResolveSignerFromAddressField is the regression guard
// for the nominator-pool signer-resolution fix (Gap 1). Before it, these
// hand-rolled gogo tx types declared no cosmos.msg.v1.signer option and no
// fields, so the x/tx signing context could resolve a signer for none of them
// ("no cosmos.msg.v1.signer option found") and no staking write could ever be
// broadcast. This asserts, through the app's real signing context (the one
// baseapp decodes live txs with), that each user-facing message resolves its
// signer to the caller's plain wallet address, and the authority-gated
// official-pool creation resolves to its governance authority — exactly the
// addresses a normal signature verifies against.
func TestNominatorPoolMessagesResolveSignerFromAddressField(t *testing.T) {
	app := Setup(t, false)

	// A plain wallet address (the "AE..." form of a 20-byte account address, the
	// same kind a bank MsgSend uses as from_address) and a separate authority.
	walletAE, err := addressing.FormatUserFriendly(bytes.Repeat([]byte{0x42}, 20))
	require.NoError(t, err)
	authorityAE, err := addressing.FormatUserFriendly(bytes.Repeat([]byte{0x17}, 20))
	require.NoError(t, err)

	cases := []struct {
		name         string
		msg          sdk.Msg
		signerField  string
	}{
		{
			name:        "MsgDepositToStakingPool -> wallet_address",
			msg:         &nominatorpooltypes.MsgDepositToStakingPool{PoolID: "p", WalletAddress: walletAE, Amount: 10},
			signerField: walletAE,
		},
		{
			name:        "MsgRequestPoolUnbond -> owner_address",
			msg:         &nominatorpooltypes.MsgRequestPoolUnbond{PoolID: "p", OwnerAddress: walletAE, RequestID: "r", Shares: 1},
			signerField: walletAE,
		},
		{
			name:        "MsgClaimPoolRewards -> owner_address",
			msg:         &nominatorpooltypes.MsgClaimPoolRewards{PoolID: "p", OwnerAddress: walletAE},
			signerField: walletAE,
		},
		{
			name:        "MsgCreateOfficialLiquidStakingPool -> authority",
			msg:         &nominatorpooltypes.MsgCreateOfficialLiquidStakingPool{Authority: authorityAE, PoolID: "p", ContractAddressUser: walletAE},
			signerField: authorityAE,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			signers, _, err := app.AppCodec().GetMsgV1Signers(tc.msg)
			require.NoError(t, err, "signer must resolve — the whole point of the fix")
			require.Len(t, signers, 1)
			expected, err := addressing.Parse(tc.signerField)
			require.NoError(t, err)
			require.Equal(t, expected, signers[0], "signer must be the parsed bytes of the message's own address field")
		})
	}
}

// TestNominatorPoolCLIAdvertisesNoUnsignableWithdraw is the D4 regression.
//
// The module's tx CLI listed a "withdraw" command for MsgWithdrawPoolStake. No
// such transaction can exist: the message has no CustomGetSigners entry, so the
// signing context resolves no signer and the tx dies before a block (asserted
// below, so this test states the reason rather than assuming it).
//
// The fix is the removal, not a signer, and the two halves are asserted
// together on purpose. MsgWithdrawPoolStake is authenticated by
// CallerContractUser == pool.ContractAddressUser; a contract address is public,
// so a user-signed withdraw would satisfy that check by simply typing the
// published address, and the pool's only contract gate would be gone. A
// depositor already exits via request-unbond + the EndBlocker's cohort
// settlement, so nothing is lost. See x/nominator-pool/types/cli.go.
//
// Against the pre-fix tree the CLI assertion fails: "withdraw" is present.
func TestNominatorPoolCLIAdvertisesNoUnsignableWithdraw(t *testing.T) {
	app := Setup(t, false)

	walletAE, err := addressing.FormatUserFriendly(bytes.Repeat([]byte{0x42}, 20))
	require.NoError(t, err)
	contractAE, err := addressing.FormatUserFriendly(bytes.Repeat([]byte{0x43}, 20))
	require.NoError(t, err)

	// Why it is not advertised: no signer resolves, through the app's real
	// signing context. If a future change wires one, this assertion fires and
	// whoever wired it has to confront the contract gate above first.
	_, _, err = app.AppCodec().GetMsgV1Signers(&nominatorpooltypes.MsgWithdrawPoolStake{
		CallerContractUser: contractAE,
		PoolID:             "p",
		OwnerAddress:       walletAE,
		RequestID:          "r",
	})
	require.Error(t, err,
		"MsgWithdrawPoolStake must stay unsignable: it is authenticated by a PUBLIC contract address, "+
			"so a user-resolvable signer would defeat its official-contract gate")
	require.ErrorContains(t, err, "no cosmos.msg.v1.signer option found")

	// And so it must not be advertised as a user command.
	var names []string
	for _, cmd := range nominatorpooltypes.NewTxCmd().Commands() {
		names = append(names, cmd.Use)
	}
	require.NotContains(t, names, "withdraw",
		"the tx CLI must not advertise a withdraw command: no such transaction can be signed")
	// The exit that DOES work is still advertised -- this is a removal of a
	// fiction, not a removal of the user's way out of a pool.
	require.Contains(t, names, "request-unbond")
}
