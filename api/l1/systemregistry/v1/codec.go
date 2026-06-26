package systemregistryv1

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgRegisterSystemEntity{}, "l1/systemregistry/MsgRegisterSystemEntity", nil)
	cdc.RegisterConcrete(&MsgRegisterSystemEntityResponse{}, "l1/systemregistry/MsgRegisterSystemEntityResponse", nil)
	cdc.RegisterConcrete(&MsgUpdateSystemEntity{}, "l1/systemregistry/MsgUpdateSystemEntity", nil)
	cdc.RegisterConcrete(&MsgUpdateSystemEntityResponse{}, "l1/systemregistry/MsgUpdateSystemEntityResponse", nil)
	cdc.RegisterConcrete(&MsgPauseSystemEntity{}, "l1/systemregistry/MsgPauseSystemEntity", nil)
	cdc.RegisterConcrete(&MsgPauseSystemEntityResponse{}, "l1/systemregistry/MsgPauseSystemEntityResponse", nil)
	cdc.RegisterConcrete(&MsgResumeSystemEntity{}, "l1/systemregistry/MsgResumeSystemEntity", nil)
	cdc.RegisterConcrete(&MsgResumeSystemEntityResponse{}, "l1/systemregistry/MsgResumeSystemEntityResponse", nil)
	cdc.RegisterConcrete(&MsgDeprecateSystemEntity{}, "l1/systemregistry/MsgDeprecateSystemEntity", nil)
	cdc.RegisterConcrete(&MsgDeprecateSystemEntityResponse{}, "l1/systemregistry/MsgDeprecateSystemEntityResponse", nil)
}

func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgRegisterSystemEntity{},
		&MsgRegisterSystemEntityResponse{},
		&MsgUpdateSystemEntity{},
		&MsgUpdateSystemEntityResponse{},
		&MsgPauseSystemEntity{},
		&MsgPauseSystemEntityResponse{},
		&MsgResumeSystemEntity{},
		&MsgResumeSystemEntityResponse{},
		&MsgDeprecateSystemEntity{},
		&MsgDeprecateSystemEntityResponse{},
	)
}