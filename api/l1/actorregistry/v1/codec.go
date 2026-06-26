package actorregistryv1

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgRegisterActor{}, "l1/actor-registry/MsgRegisterActor", nil)
	cdc.RegisterConcrete(&MsgUpdateActorCode{}, "l1/actor-registry/MsgUpdateActorCode", nil)
	cdc.RegisterConcrete(&MsgFreezeActor{}, "l1/actor-registry/MsgFreezeActor", nil)
	cdc.RegisterConcrete(&MsgUnfreezeActor{}, "l1/actor-registry/MsgUnfreezeActor", nil)
	cdc.RegisterConcrete(&MsgDeleteActor{}, "l1/actor-registry/MsgDeleteActor", nil)
	cdc.RegisterConcrete(&MsgMigrateActor{}, "l1/actor-registry/MsgMigrateActor", nil)
}

func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgRegisterActor{},
		&MsgUpdateActorCode{},
		&MsgFreezeActor{},
		&MsgUnfreezeActor{},
		&MsgDeleteActor{},
		&MsgMigrateActor{},
	)
}