package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
)

func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgRegisterService{}, "l1/services/MsgRegisterService", nil)
	cdc.RegisterConcrete(&MsgUpdateService{}, "l1/services/MsgUpdateService", nil)
	cdc.RegisterConcrete(&MsgRenewService{}, "l1/services/MsgRenewService", nil)
	cdc.RegisterConcrete(&MsgDisableService{}, "l1/services/MsgDisableService", nil)
	cdc.RegisterConcrete(&MsgTransferService{}, "l1/services/MsgTransferService", nil)
	cdc.RegisterConcrete(&MsgBindServiceIdentity{}, "l1/services/MsgBindServiceIdentity", nil)
	cdc.RegisterConcrete(&MsgUnbindServiceIdentity{}, "l1/services/MsgUnbindServiceIdentity", nil)
	cdc.RegisterConcrete(&MsgRegisterProvider{}, "l1/services/MsgRegisterProvider", nil)
	cdc.RegisterConcrete(&MsgUpdateProvider{}, "l1/services/MsgUpdateProvider", nil)
	cdc.RegisterConcrete(&MsgStakeProviderCollateral{}, "l1/services/MsgStakeProviderCollateral", nil)
	cdc.RegisterConcrete(&MsgUnstakeProviderCollateral{}, "l1/services/MsgUnstakeProviderCollateral", nil)
	cdc.RegisterConcrete(&MsgAnchorServiceReceipt{}, "l1/services/MsgAnchorServiceReceipt", nil)
	cdc.RegisterConcrete(&MsgSubmitServiceDispute{}, "l1/services/MsgSubmitServiceDispute", nil)
}

func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgRegisterService{},
		&MsgUpdateService{},
		&MsgRenewService{},
		&MsgDisableService{},
		&MsgTransferService{},
		&MsgBindServiceIdentity{},
		&MsgUnbindServiceIdentity{},
		&MsgRegisterProvider{},
		&MsgUpdateProvider{},
		&MsgStakeProviderCollateral{},
		&MsgUnstakeProviderCollateral{},
		&MsgAnchorServiceReceipt{},
		&MsgSubmitServiceDispute{},
	)
	registry.RegisterImplementations(
		(*txtypes.MsgResponse)(nil),
		&MsgRegisterServiceResponse{},
		&MsgUpdateServiceResponse{},
		&MsgRenewServiceResponse{},
		&MsgDisableServiceResponse{},
		&MsgTransferServiceResponse{},
		&MsgBindServiceIdentityResponse{},
		&MsgUnbindServiceIdentityResponse{},
		&MsgRegisterProviderResponse{},
		&MsgUpdateProviderResponse{},
		&MsgStakeProviderCollateralResponse{},
		&MsgUnstakeProviderCollateralResponse{},
		&MsgAnchorServiceReceiptResponse{},
		&MsgSubmitServiceDisputeResponse{},
	)
}