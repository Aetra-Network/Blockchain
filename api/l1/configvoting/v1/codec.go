package configvotingv1

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgSubmitConfigProposal{}, "l1/config-voting/MsgSubmitConfigProposal", nil)
	cdc.RegisterConcrete(&MsgVoteConfigProposal{}, "l1/config-voting/MsgVoteConfigProposal", nil)
	cdc.RegisterConcrete(&MsgExecuteConfigProposal{}, "l1/config-voting/MsgExecuteConfigProposal", nil)
	cdc.RegisterConcrete(&MsgVetoConfigProposal{}, "l1/config-voting/MsgVetoConfigProposal", nil)
}

func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgSubmitConfigProposal{},
		&MsgVoteConfigProposal{},
		&MsgExecuteConfigProposal{},
		&MsgVetoConfigProposal{},
	)
}