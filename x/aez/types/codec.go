package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
)

func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgUpdateRoutingTable{}, "l1/aez/MsgUpdateRoutingTable", nil)
}

// RegisterInterfaces registers the Msg service descriptor and the module's one
// Msg type.
//
// RegisterMsgServiceDesc walks the file descriptor tx.go registered with
// gogoproto and REQUIRES the cosmos.msg.v1.service option to be present -- that
// is the consumer the UninterpretedOption in buildAEZTxFileDescriptor exists
// for.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	msgservice.RegisterMsgServiceDesc(registry, &Msg_serviceDesc)
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgUpdateRoutingTable{},
	)
	registry.RegisterImplementations(
		(*txtypes.MsgResponse)(nil),
		&MsgUpdateRoutingTableResponse{},
	)
}
