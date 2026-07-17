package types

import (
	"bytes"
	"compress/gzip"
	"context"

	"github.com/cosmos/gogoproto/grpc"
	gogoproto "github.com/cosmos/gogoproto/proto"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	proto2 "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// x/identity-root's Msg service (ANS Phase A). The descriptors here are
// HAND-WRITTEN, following x/aez/types/tx.go and x/nominator-pool/types/tx.go:
// this tree has no protoc/buf toolchain, so a generated Msg surface could not be
// produced or re-verified. Each message declares its REAL fields (not the
// field-less shape x/config emits) and the service carries the
// cosmos.msg.v1.service option, both mandatory for the x/tx signing context to
// resolve a signer (see the doc comment in x/aez/types/tx.go).
//
// Field NUMBERS must match the protobuf struct tags: the signing context reads
// the signer field straight off the wire by number.

// MsgSendToNameCollection is the message-driven entry point to the .aet
// collection. TOPUP (Opcode=OpcodeTopUp) moves AmountNaet into the collection
// module account. REGISTER (Opcode=OpcodeRegister) parses the label from
// Comment and either opens an issuance auction, refunds (minus a fee) when
// underfunded or when the name is taken, per the collection rules.
type MsgSendToNameCollection struct {
	Sender		string	`protobuf:"bytes,1,opt,name=sender,proto3" json:"sender,omitempty"`
	Opcode		uint32	`protobuf:"varint,2,opt,name=opcode,proto3" json:"opcode,omitempty"`
	Comment		string	`protobuf:"bytes,3,opt,name=comment,proto3" json:"comment,omitempty"`
	AmountNaet	uint64	`protobuf:"varint,4,opt,name=amount_naet,json=amountNaet,proto3" json:"amount_naet,omitempty"`
	Height		uint64	`protobuf:"varint,5,opt,name=height,proto3" json:"height,omitempty"`
}

type MsgSendToNameCollectionResponse struct {
	Outcome		string	`protobuf:"bytes,1,opt,name=outcome,proto3" json:"outcome,omitempty"`
	Name		string	`protobuf:"bytes,2,opt,name=name,proto3" json:"name,omitempty"`
	RefundNaet	uint64	`protobuf:"varint,3,opt,name=refund_naet,json=refundNaet,proto3" json:"refund_naet,omitempty"`
	FeeKeptNaet	uint64	`protobuf:"varint,4,opt,name=fee_kept_naet,json=feeKeptNaet,proto3" json:"fee_kept_naet,omitempty"`
	AuctionOpened	bool	`protobuf:"varint,5,opt,name=auction_opened,json=auctionOpened,proto3" json:"auction_opened,omitempty"`
	DeadlineHeight	uint64	`protobuf:"varint,6,opt,name=deadline_height,json=deadlineHeight,proto3" json:"deadline_height,omitempty"`
}

// MsgPlaceBid escrows AmountNaet as a bid on an open auction for Name. The prior
// high bid is refunded to its bidder; a bid below the standing high plus the
// minimum raise is rejected.
type MsgPlaceBid struct {
	Bidder		string	`protobuf:"bytes,1,opt,name=bidder,proto3" json:"bidder,omitempty"`
	Name		string	`protobuf:"bytes,2,opt,name=name,proto3" json:"name,omitempty"`
	AmountNaet	uint64	`protobuf:"varint,3,opt,name=amount_naet,json=amountNaet,proto3" json:"amount_naet,omitempty"`
	Height		uint64	`protobuf:"varint,4,opt,name=height,proto3" json:"height,omitempty"`
}

type MsgPlaceBidResponse struct {
	Name			string	`protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
	HighBidNaet		uint64	`protobuf:"varint,2,opt,name=high_bid_naet,json=highBidNaet,proto3" json:"high_bid_naet,omitempty"`
	HighBidder		string	`protobuf:"bytes,3,opt,name=high_bidder,json=highBidder,proto3" json:"high_bidder,omitempty"`
	RefundedPreviousNaet	uint64	`protobuf:"varint,4,opt,name=refunded_previous_naet,json=refundedPreviousNaet,proto3" json:"refunded_previous_naet,omitempty"`
	DeadlineHeight		uint64	`protobuf:"varint,5,opt,name=deadline_height,json=deadlineHeight,proto3" json:"deadline_height,omitempty"`
}

// MsgStartAuction lists a domain the caller owns for an owner-listed auction of
// DurationDays (7..365) at a custom start price. No money moves at start.
type MsgStartAuction struct {
	Owner		string	`protobuf:"bytes,1,opt,name=owner,proto3" json:"owner,omitempty"`
	Name		string	`protobuf:"bytes,2,opt,name=name,proto3" json:"name,omitempty"`
	StartPriceNaet	uint64	`protobuf:"varint,3,opt,name=start_price_naet,json=startPriceNaet,proto3" json:"start_price_naet,omitempty"`
	DurationDays	uint32	`protobuf:"varint,4,opt,name=duration_days,json=durationDays,proto3" json:"duration_days,omitempty"`
	Height		uint64	`protobuf:"varint,5,opt,name=height,proto3" json:"height,omitempty"`
}

type MsgStartAuctionResponse struct {
	Name		string	`protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
	DeadlineHeight	uint64	`protobuf:"varint,2,opt,name=deadline_height,json=deadlineHeight,proto3" json:"deadline_height,omitempty"`
}

// MsgUpdatePriceTable replaces the governance-owned price table. The table
// travels as two parallel arrays (label lengths + naet price strings) so the
// hand-written descriptor needs no nested message type.
type MsgUpdatePriceTable struct {
	Authority	string		`protobuf:"bytes,1,opt,name=authority,proto3" json:"authority,omitempty"`
	MinLabelLens	[]uint32	`protobuf:"varint,2,rep,packed,name=min_label_lens,json=minLabelLens,proto3" json:"min_label_lens,omitempty"`
	PricesNaet	[]string	`protobuf:"bytes,3,rep,name=prices_naet,json=pricesNaet,proto3" json:"prices_naet,omitempty"`
}

type MsgUpdatePriceTableResponse struct {
	Tiers uint32 `protobuf:"varint,1,opt,name=tiers,proto3" json:"tiers,omitempty"`
}

// PriceTiersFromMsg reconstructs the []PriceTier from the parallel arrays.
func PriceTiersFromMsg(msg *MsgUpdatePriceTable) []PriceTier {
	if msg == nil {
		return nil
	}
	out := make([]PriceTier, 0, len(msg.MinLabelLens))
	for i := range msg.MinLabelLens {
		price := ""
		if i < len(msg.PricesNaet) {
			price = msg.PricesNaet[i]
		}
		out = append(out, PriceTier{MinLabelLen: msg.MinLabelLens[i], PriceNaet: price})
	}
	return out
}

type MsgServer interface {
	SendToNameCollection(context.Context, *MsgSendToNameCollection) (*MsgSendToNameCollectionResponse, error)
	PlaceBid(context.Context, *MsgPlaceBid) (*MsgPlaceBidResponse, error)
	StartAuction(context.Context, *MsgStartAuction) (*MsgStartAuctionResponse, error)
	UpdatePriceTable(context.Context, *MsgUpdatePriceTable) (*MsgUpdatePriceTableResponse, error)
}

type UnimplementedMsgServer struct{}

func (UnimplementedMsgServer) SendToNameCollection(context.Context, *MsgSendToNameCollection) (*MsgSendToNameCollectionResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method SendToNameCollection not implemented")
}
func (UnimplementedMsgServer) PlaceBid(context.Context, *MsgPlaceBid) (*MsgPlaceBidResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method PlaceBid not implemented")
}
func (UnimplementedMsgServer) StartAuction(context.Context, *MsgStartAuction) (*MsgStartAuctionResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method StartAuction not implemented")
}
func (UnimplementedMsgServer) UpdatePriceTable(context.Context, *MsgUpdatePriceTable) (*MsgUpdatePriceTableResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method UpdatePriceTable not implemented")
}

func RegisterMsgServer(s grpc.Server, srv MsgServer) { s.RegisterService(&Msg_serviceDesc, srv) }

var Msg_serviceDesc = grpcgo.ServiceDesc{
	ServiceName:	"l1.identityroot.v1.Msg",
	HandlerType:	(*MsgServer)(nil),
	Methods: []grpcgo.MethodDesc{
		{MethodName: "SendToNameCollection", Handler: _Msg_SendToNameCollection_Handler},
		{MethodName: "PlaceBid", Handler: _Msg_PlaceBid_Handler},
		{MethodName: "StartAuction", Handler: _Msg_StartAuction_Handler},
		{MethodName: "UpdatePriceTable", Handler: _Msg_UpdatePriceTable_Handler},
	},
	Streams:	[]grpcgo.StreamDesc{},
	Metadata:	"l1/identityroot/v1/tx.proto",
}

func _Msg_SendToNameCollection_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	req := new(MsgSendToNameCollection)
	if err := dec(req); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).SendToNameCollection(ctx, req)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.identityroot.v1.Msg/SendToNameCollection"}
	handler := func(ctx context.Context, request interface{}) (interface{}, error) {
		return srv.(MsgServer).SendToNameCollection(ctx, request.(*MsgSendToNameCollection))
	}
	return interceptor(ctx, req, info, handler)
}

func _Msg_PlaceBid_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	req := new(MsgPlaceBid)
	if err := dec(req); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).PlaceBid(ctx, req)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.identityroot.v1.Msg/PlaceBid"}
	handler := func(ctx context.Context, request interface{}) (interface{}, error) {
		return srv.(MsgServer).PlaceBid(ctx, request.(*MsgPlaceBid))
	}
	return interceptor(ctx, req, info, handler)
}

func _Msg_StartAuction_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	req := new(MsgStartAuction)
	if err := dec(req); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).StartAuction(ctx, req)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.identityroot.v1.Msg/StartAuction"}
	handler := func(ctx context.Context, request interface{}) (interface{}, error) {
		return srv.(MsgServer).StartAuction(ctx, request.(*MsgStartAuction))
	}
	return interceptor(ctx, req, info, handler)
}

func _Msg_UpdatePriceTable_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	req := new(MsgUpdatePriceTable)
	if err := dec(req); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).UpdatePriceTable(ctx, req)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.identityroot.v1.Msg/UpdatePriceTable"}
	handler := func(ctx context.Context, request interface{}) (interface{}, error) {
		return srv.(MsgServer).UpdatePriceTable(ctx, request.(*MsgUpdatePriceTable))
	}
	return interceptor(ctx, req, info, handler)
}

func init() {
	registerTxTypes()
	gogoproto.RegisterFile("l1/identityroot/v1/tx.proto", fileDescriptorIdentityRootTx)
}

var fileDescriptorIdentityRootTx = buildIdentityRootTxFileDescriptor()

func buildIdentityRootTxFileDescriptor() []byte {
	str := descriptorpb.FieldDescriptorProto_TYPE_STRING
	u32 := descriptorpb.FieldDescriptorProto_TYPE_UINT32
	u64 := descriptorpb.FieldDescriptorProto_TYPE_UINT64
	b := descriptorpb.FieldDescriptorProto_TYPE_BOOL

	messages := []*descriptorpb.DescriptorProto{
		{
			Name: txDescriptorString("MsgSendToNameCollection"),
			Field: []*descriptorpb.FieldDescriptorProto{
				txField("sender", 1, str),
				txField("opcode", 2, u32),
				txField("comment", 3, str),
				txField("amount_naet", 4, u64),
				txField("height", 5, u64),
			},
		},
		{
			Name: txDescriptorString("MsgSendToNameCollectionResponse"),
			Field: []*descriptorpb.FieldDescriptorProto{
				txField("outcome", 1, str),
				txField("name", 2, str),
				txField("refund_naet", 3, u64),
				txField("fee_kept_naet", 4, u64),
				txField("auction_opened", 5, b),
				txField("deadline_height", 6, u64),
			},
		},
		{
			Name: txDescriptorString("MsgPlaceBid"),
			Field: []*descriptorpb.FieldDescriptorProto{
				txField("bidder", 1, str),
				txField("name", 2, str),
				txField("amount_naet", 3, u64),
				txField("height", 4, u64),
			},
		},
		{
			Name: txDescriptorString("MsgPlaceBidResponse"),
			Field: []*descriptorpb.FieldDescriptorProto{
				txField("name", 1, str),
				txField("high_bid_naet", 2, u64),
				txField("high_bidder", 3, str),
				txField("refunded_previous_naet", 4, u64),
				txField("deadline_height", 5, u64),
			},
		},
		{
			Name: txDescriptorString("MsgStartAuction"),
			Field: []*descriptorpb.FieldDescriptorProto{
				txField("owner", 1, str),
				txField("name", 2, str),
				txField("start_price_naet", 3, u64),
				txField("duration_days", 4, u32),
				txField("height", 5, u64),
			},
		},
		{
			Name: txDescriptorString("MsgStartAuctionResponse"),
			Field: []*descriptorpb.FieldDescriptorProto{
				txField("name", 1, str),
				txField("deadline_height", 2, u64),
			},
		},
		{
			Name: txDescriptorString("MsgUpdatePriceTable"),
			Field: []*descriptorpb.FieldDescriptorProto{
				txField("authority", 1, str),
				txRepeatedField("min_label_lens", 2, u32),
				txRepeatedField("prices_naet", 3, str),
			},
		},
		{
			Name: txDescriptorString("MsgUpdatePriceTableResponse"),
			Field: []*descriptorpb.FieldDescriptorProto{
				txField("tiers", 1, u32),
			},
		},
	}
	fd := &descriptorpb.FileDescriptorProto{
		Name:		txDescriptorString("l1/identityroot/v1/tx.proto"),
		Package:	txDescriptorString("l1.identityroot.v1"),
		Syntax:		txDescriptorString("proto3"),
		MessageType:	messages,
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: txDescriptorString("Msg"),
			Method: []*descriptorpb.MethodDescriptorProto{
				{
					Name:		txDescriptorString("SendToNameCollection"),
					InputType:	txDescriptorString(".l1.identityroot.v1.MsgSendToNameCollection"),
					OutputType:	txDescriptorString(".l1.identityroot.v1.MsgSendToNameCollectionResponse"),
				},
				{
					Name:		txDescriptorString("PlaceBid"),
					InputType:	txDescriptorString(".l1.identityroot.v1.MsgPlaceBid"),
					OutputType:	txDescriptorString(".l1.identityroot.v1.MsgPlaceBidResponse"),
				},
				{
					Name:		txDescriptorString("StartAuction"),
					InputType:	txDescriptorString(".l1.identityroot.v1.MsgStartAuction"),
					OutputType:	txDescriptorString(".l1.identityroot.v1.MsgStartAuctionResponse"),
				},
				{
					Name:		txDescriptorString("UpdatePriceTable"),
					InputType:	txDescriptorString(".l1.identityroot.v1.MsgUpdatePriceTable"),
					OutputType:	txDescriptorString(".l1.identityroot.v1.MsgUpdatePriceTableResponse"),
				},
			},
			Options: &descriptorpb.ServiceOptions{
				UninterpretedOption: []*descriptorpb.UninterpretedOption{{
					Name: []*descriptorpb.UninterpretedOption_NamePart{{
						NamePart:	txDescriptorString("cosmos.msg.v1.service"),
						IsExtension:	txDescriptorBool(true),
					}},
					IdentifierValue: txDescriptorString("true"),
				}},
			},
		}},
	}
	raw, err := proto2.Marshal(fd)
	if err != nil {
		panic(err)
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		panic(err)
	}
	if err := zw.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func txField(name string, number int32, typ descriptorpb.FieldDescriptorProto_Type) *descriptorpb.FieldDescriptorProto {
	return txDescriptorField(name, number, typ, descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL)
}

func txRepeatedField(name string, number int32, typ descriptorpb.FieldDescriptorProto_Type) *descriptorpb.FieldDescriptorProto {
	return txDescriptorField(name, number, typ, descriptorpb.FieldDescriptorProto_LABEL_REPEATED)
}

func txDescriptorField(name string, number int32, typ descriptorpb.FieldDescriptorProto_Type, label descriptorpb.FieldDescriptorProto_Label) *descriptorpb.FieldDescriptorProto {
	num := number
	fieldType := typ
	fieldLabel := label
	return &descriptorpb.FieldDescriptorProto{
		Name:	txDescriptorString(name),
		Number:	&num,
		Label:	&fieldLabel,
		Type:	&fieldType,
	}
}

func txDescriptorString(value string) *string	{ return &value }
func txDescriptorBool(value bool) *bool		{ return &value }

func registerTxTypes() {
	gogoproto.RegisterType((*MsgSendToNameCollection)(nil), "l1.identityroot.v1.MsgSendToNameCollection")
	gogoproto.RegisterType((*MsgSendToNameCollectionResponse)(nil), "l1.identityroot.v1.MsgSendToNameCollectionResponse")
	gogoproto.RegisterType((*MsgPlaceBid)(nil), "l1.identityroot.v1.MsgPlaceBid")
	gogoproto.RegisterType((*MsgPlaceBidResponse)(nil), "l1.identityroot.v1.MsgPlaceBidResponse")
	gogoproto.RegisterType((*MsgStartAuction)(nil), "l1.identityroot.v1.MsgStartAuction")
	gogoproto.RegisterType((*MsgStartAuctionResponse)(nil), "l1.identityroot.v1.MsgStartAuctionResponse")
	gogoproto.RegisterType((*MsgUpdatePriceTable)(nil), "l1.identityroot.v1.MsgUpdatePriceTable")
	gogoproto.RegisterType((*MsgUpdatePriceTableResponse)(nil), "l1.identityroot.v1.MsgUpdatePriceTableResponse")
}

func (m *MsgSendToNameCollection) Reset()		{ *m = MsgSendToNameCollection{} }
func (m *MsgSendToNameCollectionResponse) Reset()	{ *m = MsgSendToNameCollectionResponse{} }
func (m *MsgPlaceBid) Reset()				{ *m = MsgPlaceBid{} }
func (m *MsgPlaceBidResponse) Reset()			{ *m = MsgPlaceBidResponse{} }
func (m *MsgStartAuction) Reset()			{ *m = MsgStartAuction{} }
func (m *MsgStartAuctionResponse) Reset()		{ *m = MsgStartAuctionResponse{} }
func (m *MsgUpdatePriceTable) Reset()			{ *m = MsgUpdatePriceTable{} }
func (m *MsgUpdatePriceTableResponse) Reset()		{ *m = MsgUpdatePriceTableResponse{} }

func (m *MsgSendToNameCollection) String() string		{ return gogoproto.CompactTextString(m) }
func (m *MsgSendToNameCollectionResponse) String() string	{ return gogoproto.CompactTextString(m) }
func (m *MsgPlaceBid) String() string				{ return gogoproto.CompactTextString(m) }
func (m *MsgPlaceBidResponse) String() string			{ return gogoproto.CompactTextString(m) }
func (m *MsgStartAuction) String() string			{ return gogoproto.CompactTextString(m) }
func (m *MsgStartAuctionResponse) String() string		{ return gogoproto.CompactTextString(m) }
func (m *MsgUpdatePriceTable) String() string			{ return gogoproto.CompactTextString(m) }
func (m *MsgUpdatePriceTableResponse) String() string		{ return gogoproto.CompactTextString(m) }

func (*MsgSendToNameCollection) ProtoMessage()		{}
func (*MsgSendToNameCollectionResponse) ProtoMessage()	{}
func (*MsgPlaceBid) ProtoMessage()			{}
func (*MsgPlaceBidResponse) ProtoMessage()		{}
func (*MsgStartAuction) ProtoMessage()			{}
func (*MsgStartAuctionResponse) ProtoMessage()		{}
func (*MsgUpdatePriceTable) ProtoMessage()		{}
func (*MsgUpdatePriceTableResponse) ProtoMessage()	{}

// Descriptor returns the gzipped file descriptor and this message's index in the
// file's message list. The v0.54.3 tx decoder's RejectUnknownFields walker calls
// Descriptor() on every body message to load its field set; a type without it is
// rejected at decode with "does not have a Descriptor() method" (see
// codec/unknownproto/unknown_fields.go), which is why a wire-encoded
// MsgSendToNameCollection could never decode or route. Indices match the
// messages slice built in buildIdentityRootTxFileDescriptor above.
func (*MsgSendToNameCollection) Descriptor() ([]byte, []int) {
	return fileDescriptorIdentityRootTx, []int{0}
}
func (*MsgSendToNameCollectionResponse) Descriptor() ([]byte, []int) {
	return fileDescriptorIdentityRootTx, []int{1}
}
func (*MsgPlaceBid) Descriptor() ([]byte, []int) {
	return fileDescriptorIdentityRootTx, []int{2}
}
func (*MsgPlaceBidResponse) Descriptor() ([]byte, []int) {
	return fileDescriptorIdentityRootTx, []int{3}
}
func (*MsgStartAuction) Descriptor() ([]byte, []int) {
	return fileDescriptorIdentityRootTx, []int{4}
}
func (*MsgStartAuctionResponse) Descriptor() ([]byte, []int) {
	return fileDescriptorIdentityRootTx, []int{5}
}
func (*MsgUpdatePriceTable) Descriptor() ([]byte, []int) {
	return fileDescriptorIdentityRootTx, []int{6}
}
func (*MsgUpdatePriceTableResponse) Descriptor() ([]byte, []int) {
	return fileDescriptorIdentityRootTx, []int{7}
}
