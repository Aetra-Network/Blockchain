package validatorregistryv1

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgRegisterValidator{}, "l1/validator-registry/MsgRegisterValidator", nil)
	cdc.RegisterConcrete(&MsgUpdateValidatorMetadata{}, "l1/validator-registry/MsgUpdateValidatorMetadata", nil)
	cdc.RegisterConcrete(&MsgRotateConsensusKey{}, "l1/validator-registry/MsgRotateConsensusKey", nil)
	cdc.RegisterConcrete(&MsgUpdateWithdrawalAddress{}, "l1/validator-registry/MsgUpdateWithdrawalAddress", nil)
	cdc.RegisterConcrete(&MsgUpdateTreasuryAddress{}, "l1/validator-registry/MsgUpdateTreasuryAddress", nil)
	cdc.RegisterConcrete(&MsgRetireValidator{}, "l1/validator-registry/MsgRetireValidator", nil)
	cdc.RegisterConcrete(&MsgSetValidatorCapabilities{}, "l1/validator-registry/MsgSetValidatorCapabilities", nil)
}

func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgRegisterValidator{},
		&MsgUpdateValidatorMetadata{},
		&MsgRotateConsensusKey{},
		&MsgUpdateWithdrawalAddress{},
		&MsgUpdateTreasuryAddress{},
		&MsgRetireValidator{},
		&MsgSetValidatorCapabilities{},
	)
}