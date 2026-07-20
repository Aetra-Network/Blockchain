package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
)

func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgSendToNameCollection{}, "l1/identityroot/MsgSendToNameCollection", nil)
	cdc.RegisterConcrete(&MsgPlaceBid{}, "l1/identityroot/MsgPlaceBid", nil)
	cdc.RegisterConcrete(&MsgStartAuction{}, "l1/identityroot/MsgStartAuction", nil)
	cdc.RegisterConcrete(&MsgUpdatePriceTable{}, "l1/identityroot/MsgUpdatePriceTable", nil)
	cdc.RegisterConcrete(&MsgListForSale{}, "l1/identityroot/MsgListForSale", nil)
	cdc.RegisterConcrete(&MsgDelistName{}, "l1/identityroot/MsgDelistName", nil)
	cdc.RegisterConcrete(&MsgBuyListedName{}, "l1/identityroot/MsgBuyListedName", nil)
	cdc.RegisterConcrete(&MsgAttachDomain{}, "l1/identityroot/MsgAttachDomain", nil)
	cdc.RegisterConcrete(&MsgDetachDomain{}, "l1/identityroot/MsgDetachDomain", nil)
	cdc.RegisterConcrete(&MsgDisownAttachment{}, "l1/identityroot/MsgDisownAttachment", nil)
	cdc.RegisterConcrete(&MsgCreateSubdomain{}, "l1/identityroot/MsgCreateSubdomain", nil)
	cdc.RegisterConcrete(&MsgRenewName{}, "l1/identityroot/MsgRenewName", nil)
	cdc.RegisterConcrete(&MsgTransferName{}, "l1/identityroot/MsgTransferName", nil)
	cdc.RegisterConcrete(&MsgSetResolver{}, "l1/identityroot/MsgSetResolver", nil)
	cdc.RegisterConcrete(&MsgSetReverseRecord{}, "l1/identityroot/MsgSetReverseRecord", nil)
	cdc.RegisterConcrete(&MsgReserveName{}, "l1/identityroot/MsgReserveName", nil)
	cdc.RegisterConcrete(&MsgReleaseReservedName{}, "l1/identityroot/MsgReleaseReservedName", nil)
}

// RegisterInterfaces registers the Msg service descriptor and the Phase-A Msg
// types. RegisterMsgServiceDesc walks the tx.go file descriptor and REQUIRES
// the cosmos.msg.v1.service option -- see buildIdentityRootTxFileDescriptor.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	msgservice.RegisterMsgServiceDesc(registry, &Msg_serviceDesc)
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgSendToNameCollection{},
		&MsgPlaceBid{},
		&MsgStartAuction{},
		&MsgUpdatePriceTable{},
		&MsgListForSale{},
		&MsgDelistName{},
		&MsgBuyListedName{},
		&MsgAttachDomain{},
		&MsgDetachDomain{},
		&MsgDisownAttachment{},
		&MsgCreateSubdomain{},
		&MsgRenewName{},
		&MsgTransferName{},
		&MsgSetResolver{},
		&MsgSetReverseRecord{},
		&MsgReserveName{},
		&MsgReleaseReservedName{},
	)
	registry.RegisterImplementations(
		(*txtypes.MsgResponse)(nil),
		&MsgSendToNameCollectionResponse{},
		&MsgPlaceBidResponse{},
		&MsgStartAuctionResponse{},
		&MsgUpdatePriceTableResponse{},
		&MsgListForSaleResponse{},
		&MsgDelistNameResponse{},
		&MsgBuyListedNameResponse{},
		&MsgAttachDomainResponse{},
		&MsgDetachDomainResponse{},
		&MsgDisownAttachmentResponse{},
		&MsgCreateSubdomainResponse{},
		&MsgRenewNameResponse{},
		&MsgTransferNameResponse{},
		&MsgSetResolverResponse{},
		&MsgSetReverseRecordResponse{},
		&MsgReserveNameResponse{},
		&MsgReleaseReservedNameResponse{},
	)
}
