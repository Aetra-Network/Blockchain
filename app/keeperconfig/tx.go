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
