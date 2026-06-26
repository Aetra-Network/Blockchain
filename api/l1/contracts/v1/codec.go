package contractsv1

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgStoreCode{}, "l1/contracts/MsgStoreCode", nil)
	cdc.RegisterConcrete(&MsgStoreCodeResponse{}, "l1/contracts/MsgStoreCodeResponse", nil)
	cdc.RegisterConcrete(&MsgDeployContract{}, "l1/contracts/MsgDeployContract", nil)
	cdc.RegisterConcrete(&MsgDeployContractResponse{}, "l1/contracts/MsgDeployContractResponse", nil)
	cdc.RegisterConcrete(&MsgExecuteExternal{}, "l1/contracts/MsgExecuteExternal", nil)
	cdc.RegisterConcrete(&MsgExecuteExternalResponse{}, "l1/contracts/MsgExecuteExternalResponse", nil)
	cdc.RegisterConcrete(&MsgExecuteInternal{}, "l1/contracts/MsgExecuteInternal", nil)
	cdc.RegisterConcrete(&MsgExecuteInternalResponse{}, "l1/contracts/MsgExecuteInternalResponse", nil)
	cdc.RegisterConcrete(&MsgSendInternalMessage{}, "l1/contracts/MsgSendInternalMessage", nil)
	cdc.RegisterConcrete(&MsgSendInternalMessageResponse{}, "l1/contracts/MsgSendInternalMessageResponse", nil)
	cdc.RegisterConcrete(&MsgUpdateContractParams{}, "l1/contracts/MsgUpdateContractParams", nil)
	cdc.RegisterConcrete(&MsgUpdateContractParamsResponse{}, "l1/contracts/MsgUpdateContractParamsResponse", nil)
}

func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgStoreCode{},
		&MsgStoreCodeResponse{},
		&MsgDeployContract{},
		&MsgDeployContractResponse{},
		&MsgExecuteExternal{},
		&MsgExecuteExternalResponse{},
		&MsgExecuteInternal{},
		&MsgExecuteInternalResponse{},
		&MsgSendInternalMessage{},
		&MsgSendInternalMessageResponse{},
		&MsgUpdateContractParams{},
		&MsgUpdateContractParamsResponse{},
	)
}