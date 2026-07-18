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
	cdc.RegisterConcrete(&MsgAttachDomain{}, "l1/identityroot/MsgAttachDomain", nil)
	cdc.RegisterConcrete(&MsgDetachDomain{}, "l1/identityroot/MsgDetachDomain", nil)
	cdc.RegisterConcrete(&MsgCreateSubdomain{}, "l1/identityroot/MsgCreateSubdomain", nil)
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
		&MsgAttachDomain{},
		&MsgDetachDomain{},
		&MsgCreateSubdomain{},
	)
	registry.RegisterImplementations(
		(*txtypes.MsgResponse)(nil),
		&MsgSendToNameCollectionResponse{},
		&MsgPlaceBidResponse{},
		&MsgStartAuctionResponse{},
		&MsgUpdatePriceTableResponse{},
		&MsgAttachDomainResponse{},
		&MsgDetachDomainResponse{},
		&MsgCreateSubdomainResponse{},
	)
}
