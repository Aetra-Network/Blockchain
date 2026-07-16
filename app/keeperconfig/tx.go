package keeperconfig

import (
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	sigtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	txmodule "github.com/cosmos/cosmos-sdk/x/auth/tx/config"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	"github.com/cosmos/cosmos-sdk/x/tx/signing"
	"google.golang.org/protobuf/reflect/protoreflect"

	aetraaddress "github.com/sovereign-l1/l1/app/addressing"
	nativeaccounttypes "github.com/sovereign-l1/l1/x/native-account/types"
	nominatorpooltypes "github.com/sovereign-l1/l1/x/nominator-pool/types"
)

// CustomGetSigners holds signer-resolution overrides for hand-rolled message
// types whose declared cosmos.msg.v1.signer field can't be verified against a
// normal signature -- see native-account/types/signing.go's doc comment for
// MsgActivateAccount's case. Shared by both TxConfig constructions in this
// binary (this file's, used for app.TxConfig()/CLI/query tooling, and
// app.go's early-bootstrap one, which baseapp actually uses to decode every
// live transaction) so they can't silently drift apart.
func CustomGetSigners() map[protoreflect.FullName]signing.GetSignersFunc {
	return map[protoreflect.FullName]signing.GetSignersFunc{
		"l1.nativeaccount.v1.MsgActivateAccount": nativeaccounttypes.MsgActivateAccountSigners,
		// x/nominator-pool's hand-rolled tx types carried no signer option and no
		// fields, so the signing context could not resolve a signer for any of
		// them ("no cosmos.msg.v1.signer option found") -- see
		// nominator-pool/types/signing.go. The three user-facing messages resolve
		// to the caller's plain wallet address; the official-pool creation
		// resolves to the governance authority address.
		"l1.nominatorpool.v1.MsgDepositToStakingPool":         nominatorpooltypes.MsgDepositToStakingPoolSigners,
		"l1.nominatorpool.v1.MsgRequestPoolUnbond":            nominatorpooltypes.MsgRequestPoolUnbondSigners,
		"l1.nominatorpool.v1.MsgClaimPoolRewards":             nominatorpooltypes.MsgClaimPoolRewardsSigners,
		"l1.nominatorpool.v1.MsgCreateOfficialLiquidStakingPool": nominatorpooltypes.MsgCreateOfficialLiquidStakingPoolSigners,
		// #2/SA2-N01: the three plain-pool messages real bank+staking custody
		// depends on had the identical missing-descriptor bug as the four
		// above (live-verified: broadcasting one crashed gogoproto's
		// Unmarshal on every receiving node) -- see the struct doc comments
		// on these types in x/nominator-pool/types/state.go.
		"l1.nominatorpool.v1.MsgCreateNominatorPool":    nominatorpooltypes.MsgCreateNominatorPoolSigners,
		"l1.nominatorpool.v1.MsgDepositToPool":          nominatorpooltypes.MsgDepositToPoolSigners,
		"l1.nominatorpool.v1.MsgRequestPoolWithdrawal":  nominatorpooltypes.MsgRequestPoolWithdrawalSigners,
	}
}

func NewTxConfig(appCodec codec.Codec, bankKeeper bankkeeper.BaseKeeper) client.TxConfig {
	enabledSignModes := append(authtx.DefaultSignModes, sigtypes.SignMode_SIGN_MODE_TEXTUAL)
	txConfig, err := authtx.NewTxConfigWithOptions(
		appCodec,
		authtx.ConfigOptions{
			EnabledSignModes:	enabledSignModes,
			SigningOptions: &signing.Options{
				AddressCodec:		aetraaddress.Codec{},
				ValidatorAddressCodec:	aetraaddress.Codec{},
				CustomGetSigners:	CustomGetSigners(),
			},
			TextualCoinMetadataQueryFn:	txmodule.NewBankKeeperCoinMetadataQueryFn(bankKeeper),
		},
	)
	if err != nil {
		panic(err)
	}
	return txConfig
}
