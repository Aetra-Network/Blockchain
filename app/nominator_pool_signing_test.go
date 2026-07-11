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
