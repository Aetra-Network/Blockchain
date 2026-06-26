package validatorinsurancev1

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgFundValidatorInsurance{}, "l1/validator-insurance/MsgFundValidatorInsurance", nil)
	cdc.RegisterConcrete(&MsgWithdrawValidatorInsurance{}, "l1/validator-insurance/MsgWithdrawValidatorInsurance", nil)
	cdc.RegisterConcrete(&MsgSubmitInsuranceClaim{}, "l1/validator-insurance/MsgSubmitInsuranceClaim", nil)
	cdc.RegisterConcrete(&MsgResolveInsuranceClaim{}, "l1/validator-insurance/MsgResolveInsuranceClaim", nil)
}

func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgFundValidatorInsurance{},
		&MsgWithdrawValidatorInsurance{},
		&MsgSubmitInsuranceClaim{},
		&MsgResolveInsuranceClaim{},
	)
}