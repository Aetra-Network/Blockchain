package schedulerv1

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgUpdateParams{}, "l1/scheduler/MsgUpdateParams", nil)
	cdc.RegisterConcrete(&MsgRegisterScheduledJob{}, "l1/scheduler/MsgRegisterScheduledJob", nil)
	cdc.RegisterConcrete(&MsgPauseScheduledJob{}, "l1/scheduler/MsgPauseScheduledJob", nil)
	cdc.RegisterConcrete(&MsgResumeScheduledJob{}, "l1/scheduler/MsgResumeScheduledJob", nil)
	cdc.RegisterConcrete(&MsgCancelScheduledJob{}, "l1/scheduler/MsgCancelScheduledJob", nil)
	cdc.RegisterConcrete(&MsgExecuteDueJobs{}, "l1/scheduler/MsgExecuteDueJobs", nil)
}

func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgUpdateParams{},
		&MsgRegisterScheduledJob{},
		&MsgPauseScheduledJob{},
		&MsgResumeScheduledJob{},
		&MsgCancelScheduledJob{},
		&MsgExecuteDueJobs{},
	)
}