package configv1

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgSubmitConfigChange{}, "l1/config/MsgSubmitConfigChange", nil)
	cdc.RegisterConcrete(&MsgSubmitConfigChangeResponse{}, "l1/config/MsgSubmitConfigChangeResponse", nil)
	cdc.RegisterConcrete(&MsgApproveConfigChange{}, "l1/config/MsgApproveConfigChange", nil)
	cdc.RegisterConcrete(&MsgApproveConfigChangeResponse{}, "l1/config/MsgApproveConfigChangeResponse", nil)
	cdc.RegisterConcrete(&MsgRejectConfigChange{}, "l1/config/MsgRejectConfigChange", nil)
	cdc.RegisterConcrete(&MsgRejectConfigChangeResponse{}, "l1/config/MsgRejectConfigChangeResponse", nil)
	cdc.RegisterConcrete(&MsgExecuteConfigChange{}, "l1/config/MsgExecuteConfigChange", nil)
	cdc.RegisterConcrete(&MsgExecuteConfigChangeResponse{}, "l1/config/MsgExecuteConfigChangeResponse", nil)
	cdc.RegisterConcrete(&MsgCancelConfigChange{}, "l1/config/MsgCancelConfigChange", nil)
	cdc.RegisterConcrete(&MsgCancelConfigChangeResponse{}, "l1/config/MsgCancelConfigChangeResponse", nil)
}

func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgSubmitConfigChange{},
		&MsgSubmitConfigChangeResponse{},
		&MsgApproveConfigChange{},
		&MsgApproveConfigChangeResponse{},
		&MsgRejectConfigChange{},
		&MsgRejectConfigChangeResponse{},
		&MsgExecuteConfigChange{},
		&MsgExecuteConfigChangeResponse{},
		&MsgCancelConfigChange{},
		&MsgCancelConfigChangeResponse{},
	)
}