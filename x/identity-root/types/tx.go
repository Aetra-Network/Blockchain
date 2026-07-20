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

// MsgAttachDomain (ANS Phase B) attaches an owned FQDN to a Target wallet. The
// caller (Owner) must own the FQDN; the Target is classified with x/aez
// CanonicalEntityID and only a user contract or a native_account is allowed --
// system/pool/staking entities and dns items are rejected. One domain per
// wallet: a second attach for the same Target is rejected.
type MsgAttachDomain struct {
	Owner	string	`protobuf:"bytes,1,opt,name=owner,proto3" json:"owner,omitempty"`
	Fqdn	string	`protobuf:"bytes,2,opt,name=fqdn,proto3" json:"fqdn,omitempty"`
	Target	string	`protobuf:"bytes,3,opt,name=target,proto3" json:"target,omitempty"`
	Height	uint64	`protobuf:"varint,4,opt,name=height,proto3" json:"height,omitempty"`
}

type MsgAttachDomainResponse struct {
	Fqdn	string	`protobuf:"bytes,1,opt,name=fqdn,proto3" json:"fqdn,omitempty"`
	Target	string	`protobuf:"bytes,2,opt,name=target,proto3" json:"target,omitempty"`
}

// MsgDetachDomain (ANS Phase B) clears the attachment for an owned FQDN,
// freeing the target wallet from the one-domain-per-wallet index.
type MsgDetachDomain struct {
	Owner	string	`protobuf:"bytes,1,opt,name=owner,proto3" json:"owner,omitempty"`
	Fqdn	string	`protobuf:"bytes,2,opt,name=fqdn,proto3" json:"fqdn,omitempty"`
	Height	uint64	`protobuf:"varint,3,opt,name=height,proto3" json:"height,omitempty"`
}

type MsgDetachDomainResponse struct {
	Fqdn	string	`protobuf:"bytes,1,opt,name=fqdn,proto3" json:"fqdn,omitempty"`
}

// MsgDisownAttachment (ANS Phase B, anti-griefing) lets the TARGET wallet of an
// attachment clear it without the FQDN owner's cooperation. AttachDomain lets an
// FQDN owner point a name at ANY allowed wallet without that wallet's consent,
// occupying its single one-domain-per-wallet slot; only the FQDN owner can
// DetachDomain. This message closes that grief: the signer is the Target itself,
// authorizing removal of the attachment that points at its OWN wallet. No
// owned-name check -- the target need not own the FQDN.
type MsgDisownAttachment struct {
	Target	string	`protobuf:"bytes,1,opt,name=target,proto3" json:"target,omitempty"`
	Height	uint64	`protobuf:"varint,2,opt,name=height,proto3" json:"height,omitempty"`
}

type MsgDisownAttachmentResponse struct {
	Fqdn	string	`protobuf:"bytes,1,opt,name=fqdn,proto3" json:"fqdn,omitempty"`
	Target	string	`protobuf:"bytes,2,opt,name=target,proto3" json:"target,omitempty"`
}

// MsgListForSale (ANS Phase B) lists a domain the caller owns for a fixed-price
// sale (docs/architecture/ans.md "Owner fixed-price sale"): the owner sets any
// PriceNaet they want; MsgBuyListedName by anyone paying it triggers an atomic
// name-for-payment swap, reusing the same ownership-transfer mechanics as
// TransferName plus the collection's bank-custody pattern for the payment. No
// money moves at listing time.
type MsgListForSale struct {
	Owner		string	`protobuf:"bytes,1,opt,name=owner,proto3" json:"owner,omitempty"`
	Name		string	`protobuf:"bytes,2,opt,name=name,proto3" json:"name,omitempty"`
	PriceNaet	uint64	`protobuf:"varint,3,opt,name=price_naet,json=priceNaet,proto3" json:"price_naet,omitempty"`
	Height		uint64	`protobuf:"varint,4,opt,name=height,proto3" json:"height,omitempty"`
}

type MsgListForSaleResponse struct {
	Name		string	`protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
	PriceNaet	uint64	`protobuf:"varint,2,opt,name=price_naet,json=priceNaet,proto3" json:"price_naet,omitempty"`
}

// MsgDelistName (ANS Phase B) clears an owner's fixed-price listing without a
// sale -- the explicit delist path alongside the implicit clear-on-transfer (see
// keeper.TransferName / keeper.grantAuctionName).
type MsgDelistName struct {
	Owner	string	`protobuf:"bytes,1,opt,name=owner,proto3" json:"owner,omitempty"`
	Name	string	`protobuf:"bytes,2,opt,name=name,proto3" json:"name,omitempty"`
	Height	uint64	`protobuf:"varint,3,opt,name=height,proto3" json:"height,omitempty"`
}

type MsgDelistNameResponse struct {
	Name string `protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
}

// MsgBuyListedName (ANS Phase B) is the buyer side of MsgListForSale: paying the
// LIVE listing price atomically transfers the NameRecord to Buyer and the price
// from Buyer to the current owner, resetting the term like any other purchase (a
// gift/TransferName does not). Buyer pays exactly the listing's current price --
// read from the on-chain Listing by the keeper at execution time, not supplied
// by the caller -- so there is no over/under-payment amount to carry on the wire.
type MsgBuyListedName struct {
	Buyer	string	`protobuf:"bytes,1,opt,name=buyer,proto3" json:"buyer,omitempty"`
	Name	string	`protobuf:"bytes,2,opt,name=name,proto3" json:"name,omitempty"`
	Height	uint64	`protobuf:"varint,3,opt,name=height,proto3" json:"height,omitempty"`
}

type MsgBuyListedNameResponse struct {
	Name		string	`protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
	Owner		string	`protobuf:"bytes,2,opt,name=owner,proto3" json:"owner,omitempty"`
	PriceNaet	uint64	`protobuf:"varint,3,opt,name=price_naet,json=priceNaet,proto3" json:"price_naet,omitempty"`
}

// MsgCreateSubdomainResponse is the wire response for MsgCreateSubdomain (whose
// request struct lives in state.go, promoted to the wire with protobuf tags).
type MsgCreateSubdomainResponse struct {
	Name		string	`protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
	ExpiryHeight	uint64	`protobuf:"varint,2,opt,name=expiry_height,json=expiryHeight,proto3" json:"expiry_height,omitempty"`
}

// MsgRenewNameResponse is the wire response for MsgRenewName (request struct
// lives in state.go).
type MsgRenewNameResponse struct {
	Name		string	`protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
	ExpiryHeight	uint64	`protobuf:"varint,2,opt,name=expiry_height,json=expiryHeight,proto3" json:"expiry_height,omitempty"`
}

// MsgTransferNameResponse is the wire response for MsgTransferName (request
// struct lives in state.go).
type MsgTransferNameResponse struct {
	Name	string	`protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
	Owner	string	`protobuf:"bytes,2,opt,name=owner,proto3" json:"owner,omitempty"`
}

// MsgSetResolverResponse is the wire response for MsgSetResolver (request
// struct lives in state.go).
type MsgSetResolverResponse struct {
	Name		string	`protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
	ResolverRoot	string	`protobuf:"bytes,2,opt,name=resolver_root,json=resolverRoot,proto3" json:"resolver_root,omitempty"`
}

// MsgSetReverseRecordResponse is the wire response for MsgSetReverseRecord
// (request struct lives in state.go).
type MsgSetReverseRecordResponse struct {
	Address	string	`protobuf:"bytes,1,opt,name=address,proto3" json:"address,omitempty"`
	Name	string	`protobuf:"bytes,2,opt,name=name,proto3" json:"name,omitempty"`
}

// MsgReserveNameResponse is the wire response for MsgReserveName (request
// struct lives in state.go).
type MsgReserveNameResponse struct {
	Name		string	`protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
	Authority	string	`protobuf:"bytes,2,opt,name=authority,proto3" json:"authority,omitempty"`
}

// MsgReleaseReservedNameResponse is the wire response for
// MsgReleaseReservedName (request struct lives in state.go).
type MsgReleaseReservedNameResponse struct {
	Name string `protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
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
	ListForSale(context.Context, *MsgListForSale) (*MsgListForSaleResponse, error)
	DelistName(context.Context, *MsgDelistName) (*MsgDelistNameResponse, error)
	BuyListedName(context.Context, *MsgBuyListedName) (*MsgBuyListedNameResponse, error)
	AttachDomain(context.Context, *MsgAttachDomain) (*MsgAttachDomainResponse, error)
	DetachDomain(context.Context, *MsgDetachDomain) (*MsgDetachDomainResponse, error)
	DisownAttachment(context.Context, *MsgDisownAttachment) (*MsgDisownAttachmentResponse, error)
	CreateSubdomain(context.Context, *MsgCreateSubdomain) (*MsgCreateSubdomainResponse, error)
	RenewName(context.Context, *MsgRenewName) (*MsgRenewNameResponse, error)
	TransferName(context.Context, *MsgTransferName) (*MsgTransferNameResponse, error)
	SetResolver(context.Context, *MsgSetResolver) (*MsgSetResolverResponse, error)
	SetReverseRecord(context.Context, *MsgSetReverseRecord) (*MsgSetReverseRecordResponse, error)
	ReserveName(context.Context, *MsgReserveName) (*MsgReserveNameResponse, error)
	ReleaseReservedName(context.Context, *MsgReleaseReservedName) (*MsgReleaseReservedNameResponse, error)
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
func (UnimplementedMsgServer) ListForSale(context.Context, *MsgListForSale) (*MsgListForSaleResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListForSale not implemented")
}
func (UnimplementedMsgServer) DelistName(context.Context, *MsgDelistName) (*MsgDelistNameResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DelistName not implemented")
}
func (UnimplementedMsgServer) BuyListedName(context.Context, *MsgBuyListedName) (*MsgBuyListedNameResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method BuyListedName not implemented")
}
func (UnimplementedMsgServer) AttachDomain(context.Context, *MsgAttachDomain) (*MsgAttachDomainResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method AttachDomain not implemented")
}
func (UnimplementedMsgServer) DetachDomain(context.Context, *MsgDetachDomain) (*MsgDetachDomainResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DetachDomain not implemented")
}
func (UnimplementedMsgServer) DisownAttachment(context.Context, *MsgDisownAttachment) (*MsgDisownAttachmentResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DisownAttachment not implemented")
}
func (UnimplementedMsgServer) CreateSubdomain(context.Context, *MsgCreateSubdomain) (*MsgCreateSubdomainResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CreateSubdomain not implemented")
}
func (UnimplementedMsgServer) RenewName(context.Context, *MsgRenewName) (*MsgRenewNameResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method RenewName not implemented")
}
func (UnimplementedMsgServer) TransferName(context.Context, *MsgTransferName) (*MsgTransferNameResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method TransferName not implemented")
}
func (UnimplementedMsgServer) SetResolver(context.Context, *MsgSetResolver) (*MsgSetResolverResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method SetResolver not implemented")
}
func (UnimplementedMsgServer) SetReverseRecord(context.Context, *MsgSetReverseRecord) (*MsgSetReverseRecordResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method SetReverseRecord not implemented")
}
func (UnimplementedMsgServer) ReserveName(context.Context, *MsgReserveName) (*MsgReserveNameResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ReserveName not implemented")
}
func (UnimplementedMsgServer) ReleaseReservedName(context.Context, *MsgReleaseReservedName) (*MsgReleaseReservedNameResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ReleaseReservedName not implemented")
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
		{MethodName: "ListForSale", Handler: _Msg_ListForSale_Handler},
		{MethodName: "DelistName", Handler: _Msg_DelistName_Handler},
		{MethodName: "BuyListedName", Handler: _Msg_BuyListedName_Handler},
		{MethodName: "AttachDomain", Handler: _Msg_AttachDomain_Handler},
		{MethodName: "DetachDomain", Handler: _Msg_DetachDomain_Handler},
		{MethodName: "DisownAttachment", Handler: _Msg_DisownAttachment_Handler},
		{MethodName: "CreateSubdomain", Handler: _Msg_CreateSubdomain_Handler},
		{MethodName: "RenewName", Handler: _Msg_RenewName_Handler},
		{MethodName: "TransferName", Handler: _Msg_TransferName_Handler},
		{MethodName: "SetResolver", Handler: _Msg_SetResolver_Handler},
		{MethodName: "SetReverseRecord", Handler: _Msg_SetReverseRecord_Handler},
		{MethodName: "ReserveName", Handler: _Msg_ReserveName_Handler},
		{MethodName: "ReleaseReservedName", Handler: _Msg_ReleaseReservedName_Handler},
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

func _Msg_ListForSale_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	req := new(MsgListForSale)
	if err := dec(req); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).ListForSale(ctx, req)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.identityroot.v1.Msg/ListForSale"}
	handler := func(ctx context.Context, request interface{}) (interface{}, error) {
		return srv.(MsgServer).ListForSale(ctx, request.(*MsgListForSale))
	}
	return interceptor(ctx, req, info, handler)
}

func _Msg_DelistName_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	req := new(MsgDelistName)
	if err := dec(req); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).DelistName(ctx, req)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.identityroot.v1.Msg/DelistName"}
	handler := func(ctx context.Context, request interface{}) (interface{}, error) {
		return srv.(MsgServer).DelistName(ctx, request.(*MsgDelistName))
	}
	return interceptor(ctx, req, info, handler)
}

func _Msg_BuyListedName_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	req := new(MsgBuyListedName)
	if err := dec(req); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).BuyListedName(ctx, req)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.identityroot.v1.Msg/BuyListedName"}
	handler := func(ctx context.Context, request interface{}) (interface{}, error) {
		return srv.(MsgServer).BuyListedName(ctx, request.(*MsgBuyListedName))
	}
	return interceptor(ctx, req, info, handler)
}

func _Msg_AttachDomain_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	req := new(MsgAttachDomain)
	if err := dec(req); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).AttachDomain(ctx, req)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.identityroot.v1.Msg/AttachDomain"}
	handler := func(ctx context.Context, request interface{}) (interface{}, error) {
		return srv.(MsgServer).AttachDomain(ctx, request.(*MsgAttachDomain))
	}
	return interceptor(ctx, req, info, handler)
}

func _Msg_DetachDomain_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	req := new(MsgDetachDomain)
	if err := dec(req); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).DetachDomain(ctx, req)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.identityroot.v1.Msg/DetachDomain"}
	handler := func(ctx context.Context, request interface{}) (interface{}, error) {
		return srv.(MsgServer).DetachDomain(ctx, request.(*MsgDetachDomain))
	}
	return interceptor(ctx, req, info, handler)
}

func _Msg_DisownAttachment_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	req := new(MsgDisownAttachment)
	if err := dec(req); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).DisownAttachment(ctx, req)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.identityroot.v1.Msg/DisownAttachment"}
	handler := func(ctx context.Context, request interface{}) (interface{}, error) {
		return srv.(MsgServer).DisownAttachment(ctx, request.(*MsgDisownAttachment))
	}
	return interceptor(ctx, req, info, handler)
}

func _Msg_CreateSubdomain_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	req := new(MsgCreateSubdomain)
	if err := dec(req); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).CreateSubdomain(ctx, req)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.identityroot.v1.Msg/CreateSubdomain"}
	handler := func(ctx context.Context, request interface{}) (interface{}, error) {
		return srv.(MsgServer).CreateSubdomain(ctx, request.(*MsgCreateSubdomain))
	}
	return interceptor(ctx, req, info, handler)
}

func _Msg_RenewName_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	req := new(MsgRenewName)
	if err := dec(req); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).RenewName(ctx, req)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.identityroot.v1.Msg/RenewName"}
	handler := func(ctx context.Context, request interface{}) (interface{}, error) {
		return srv.(MsgServer).RenewName(ctx, request.(*MsgRenewName))
	}
	return interceptor(ctx, req, info, handler)
}

func _Msg_TransferName_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	req := new(MsgTransferName)
	if err := dec(req); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).TransferName(ctx, req)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.identityroot.v1.Msg/TransferName"}
	handler := func(ctx context.Context, request interface{}) (interface{}, error) {
		return srv.(MsgServer).TransferName(ctx, request.(*MsgTransferName))
	}
	return interceptor(ctx, req, info, handler)
}

func _Msg_SetResolver_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	req := new(MsgSetResolver)
	if err := dec(req); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).SetResolver(ctx, req)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.identityroot.v1.Msg/SetResolver"}
	handler := func(ctx context.Context, request interface{}) (interface{}, error) {
		return srv.(MsgServer).SetResolver(ctx, request.(*MsgSetResolver))
	}
	return interceptor(ctx, req, info, handler)
}

func _Msg_SetReverseRecord_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	req := new(MsgSetReverseRecord)
	if err := dec(req); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).SetReverseRecord(ctx, req)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.identityroot.v1.Msg/SetReverseRecord"}
	handler := func(ctx context.Context, request interface{}) (interface{}, error) {
		return srv.(MsgServer).SetReverseRecord(ctx, request.(*MsgSetReverseRecord))
	}
	return interceptor(ctx, req, info, handler)
}

func _Msg_ReserveName_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	req := new(MsgReserveName)
	if err := dec(req); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).ReserveName(ctx, req)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.identityroot.v1.Msg/ReserveName"}
	handler := func(ctx context.Context, request interface{}) (interface{}, error) {
		return srv.(MsgServer).ReserveName(ctx, request.(*MsgReserveName))
	}
	return interceptor(ctx, req, info, handler)
}

func _Msg_ReleaseReservedName_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	req := new(MsgReleaseReservedName)
	if err := dec(req); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).ReleaseReservedName(ctx, req)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.identityroot.v1.Msg/ReleaseReservedName"}
	handler := func(ctx context.Context, request interface{}) (interface{}, error) {
		return srv.(MsgServer).ReleaseReservedName(ctx, request.(*MsgReleaseReservedName))
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
		{
			Name: txDescriptorString("MsgAttachDomain"),
			Field: []*descriptorpb.FieldDescriptorProto{
				txField("owner", 1, str),
				txField("fqdn", 2, str),
				txField("target", 3, str),
				txField("height", 4, u64),
			},
		},
		{
			Name: txDescriptorString("MsgAttachDomainResponse"),
			Field: []*descriptorpb.FieldDescriptorProto{
				txField("fqdn", 1, str),
				txField("target", 2, str),
			},
		},
		{
			Name: txDescriptorString("MsgDetachDomain"),
			Field: []*descriptorpb.FieldDescriptorProto{
				txField("owner", 1, str),
				txField("fqdn", 2, str),
				txField("height", 3, u64),
			},
		},
		{
			Name: txDescriptorString("MsgDetachDomainResponse"),
			Field: []*descriptorpb.FieldDescriptorProto{
				txField("fqdn", 1, str),
			},
		},
		{
			Name: txDescriptorString("MsgCreateSubdomain"),
			Field: []*descriptorpb.FieldDescriptorProto{
				txField("owner", 1, str),
				txField("parent_name", 2, str),
				txField("label", 3, str),
				txField("subdomain_owner", 4, str),
				txField("height", 5, u64),
				txField("resolver_root", 6, str),
				txField("subdomain_policy", 7, str),
			},
		},
		{
			Name: txDescriptorString("MsgCreateSubdomainResponse"),
			Field: []*descriptorpb.FieldDescriptorProto{
				txField("name", 1, str),
				txField("expiry_height", 2, u64),
			},
		},
		{
			Name: txDescriptorString("MsgDisownAttachment"),
			Field: []*descriptorpb.FieldDescriptorProto{
				txField("target", 1, str),
				txField("height", 2, u64),
			},
		},
		{
			Name: txDescriptorString("MsgDisownAttachmentResponse"),
			Field: []*descriptorpb.FieldDescriptorProto{
				txField("fqdn", 1, str),
				txField("target", 2, str),
			},
		},
		{
			Name: txDescriptorString("MsgRenewName"),
			Field: []*descriptorpb.FieldDescriptorProto{
				txField("owner", 1, str),
				txField("name", 2, str),
				txField("height", 3, u64),
			},
		},
		{
			Name: txDescriptorString("MsgRenewNameResponse"),
			Field: []*descriptorpb.FieldDescriptorProto{
				txField("name", 1, str),
				txField("expiry_height", 2, u64),
			},
		},
		{
			Name: txDescriptorString("MsgTransferName"),
			Field: []*descriptorpb.FieldDescriptorProto{
				txField("owner", 1, str),
				txField("name", 2, str),
				txField("new_owner", 3, str),
				txField("height", 4, u64),
			},
		},
		{
			Name: txDescriptorString("MsgTransferNameResponse"),
			Field: []*descriptorpb.FieldDescriptorProto{
				txField("name", 1, str),
				txField("owner", 2, str),
			},
		},
		{
			Name: txDescriptorString("MsgSetResolver"),
			Field: []*descriptorpb.FieldDescriptorProto{
				txField("owner", 1, str),
				txField("name", 2, str),
				txField("resolver_root", 3, str),
				txField("height", 4, u64),
			},
		},
		{
			Name: txDescriptorString("MsgSetResolverResponse"),
			Field: []*descriptorpb.FieldDescriptorProto{
				txField("name", 1, str),
				txField("resolver_root", 2, str),
			},
		},
		{
			Name: txDescriptorString("MsgSetReverseRecord"),
			Field: []*descriptorpb.FieldDescriptorProto{
				txField("owner", 1, str),
				txField("address", 2, str),
				txField("name", 3, str),
				txField("height", 4, u64),
			},
		},
		{
			Name: txDescriptorString("MsgSetReverseRecordResponse"),
			Field: []*descriptorpb.FieldDescriptorProto{
				txField("address", 1, str),
				txField("name", 2, str),
			},
		},
		{
			Name: txDescriptorString("MsgReserveName"),
			Field: []*descriptorpb.FieldDescriptorProto{
				txField("authority", 1, str),
				txField("name", 2, str),
				txField("reason", 3, str),
			},
		},
		{
			Name: txDescriptorString("MsgReserveNameResponse"),
			Field: []*descriptorpb.FieldDescriptorProto{
				txField("name", 1, str),
				txField("authority", 2, str),
			},
		},
		{
			Name: txDescriptorString("MsgReleaseReservedName"),
			Field: []*descriptorpb.FieldDescriptorProto{
				txField("authority", 1, str),
				txField("name", 2, str),
			},
		},
		{
			Name: txDescriptorString("MsgReleaseReservedNameResponse"),
			Field: []*descriptorpb.FieldDescriptorProto{
				txField("name", 1, str),
			},
		},
		{
			Name: txDescriptorString("MsgListForSale"),
			Field: []*descriptorpb.FieldDescriptorProto{
				txField("owner", 1, str),
				txField("name", 2, str),
				txField("price_naet", 3, u64),
				txField("height", 4, u64),
			},
		},
		{
			Name: txDescriptorString("MsgListForSaleResponse"),
			Field: []*descriptorpb.FieldDescriptorProto{
				txField("name", 1, str),
				txField("price_naet", 2, u64),
			},
		},
		{
			Name: txDescriptorString("MsgDelistName"),
			Field: []*descriptorpb.FieldDescriptorProto{
				txField("owner", 1, str),
				txField("name", 2, str),
				txField("height", 3, u64),
			},
		},
		{
			Name: txDescriptorString("MsgDelistNameResponse"),
			Field: []*descriptorpb.FieldDescriptorProto{
				txField("name", 1, str),
			},
		},
		{
			Name: txDescriptorString("MsgBuyListedName"),
			Field: []*descriptorpb.FieldDescriptorProto{
				txField("buyer", 1, str),
				txField("name", 2, str),
				txField("height", 3, u64),
			},
		},
		{
			Name: txDescriptorString("MsgBuyListedNameResponse"),
			Field: []*descriptorpb.FieldDescriptorProto{
				txField("name", 1, str),
				txField("owner", 2, str),
				txField("price_naet", 3, u64),
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
				{
					Name:		txDescriptorString("AttachDomain"),
					InputType:	txDescriptorString(".l1.identityroot.v1.MsgAttachDomain"),
					OutputType:	txDescriptorString(".l1.identityroot.v1.MsgAttachDomainResponse"),
				},
				{
					Name:		txDescriptorString("DetachDomain"),
					InputType:	txDescriptorString(".l1.identityroot.v1.MsgDetachDomain"),
					OutputType:	txDescriptorString(".l1.identityroot.v1.MsgDetachDomainResponse"),
				},
				{
					Name:		txDescriptorString("DisownAttachment"),
					InputType:	txDescriptorString(".l1.identityroot.v1.MsgDisownAttachment"),
					OutputType:	txDescriptorString(".l1.identityroot.v1.MsgDisownAttachmentResponse"),
				},
				{
					Name:		txDescriptorString("CreateSubdomain"),
					InputType:	txDescriptorString(".l1.identityroot.v1.MsgCreateSubdomain"),
					OutputType:	txDescriptorString(".l1.identityroot.v1.MsgCreateSubdomainResponse"),
				},
				{
					Name:		txDescriptorString("RenewName"),
					InputType:	txDescriptorString(".l1.identityroot.v1.MsgRenewName"),
					OutputType:	txDescriptorString(".l1.identityroot.v1.MsgRenewNameResponse"),
				},
				{
					Name:		txDescriptorString("TransferName"),
					InputType:	txDescriptorString(".l1.identityroot.v1.MsgTransferName"),
					OutputType:	txDescriptorString(".l1.identityroot.v1.MsgTransferNameResponse"),
				},
				{
					Name:		txDescriptorString("SetResolver"),
					InputType:	txDescriptorString(".l1.identityroot.v1.MsgSetResolver"),
					OutputType:	txDescriptorString(".l1.identityroot.v1.MsgSetResolverResponse"),
				},
				{
					Name:		txDescriptorString("SetReverseRecord"),
					InputType:	txDescriptorString(".l1.identityroot.v1.MsgSetReverseRecord"),
					OutputType:	txDescriptorString(".l1.identityroot.v1.MsgSetReverseRecordResponse"),
				},
				{
					Name:		txDescriptorString("ReserveName"),
					InputType:	txDescriptorString(".l1.identityroot.v1.MsgReserveName"),
					OutputType:	txDescriptorString(".l1.identityroot.v1.MsgReserveNameResponse"),
				},
				{
					Name:		txDescriptorString("ReleaseReservedName"),
					InputType:	txDescriptorString(".l1.identityroot.v1.MsgReleaseReservedName"),
					OutputType:	txDescriptorString(".l1.identityroot.v1.MsgReleaseReservedNameResponse"),
				},
				{
					Name:		txDescriptorString("ListForSale"),
					InputType:	txDescriptorString(".l1.identityroot.v1.MsgListForSale"),
					OutputType:	txDescriptorString(".l1.identityroot.v1.MsgListForSaleResponse"),
				},
				{
					Name:		txDescriptorString("DelistName"),
					InputType:	txDescriptorString(".l1.identityroot.v1.MsgDelistName"),
					OutputType:	txDescriptorString(".l1.identityroot.v1.MsgDelistNameResponse"),
				},
				{
					Name:		txDescriptorString("BuyListedName"),
					InputType:	txDescriptorString(".l1.identityroot.v1.MsgBuyListedName"),
					OutputType:	txDescriptorString(".l1.identityroot.v1.MsgBuyListedNameResponse"),
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
	gogoproto.RegisterType((*MsgAttachDomain)(nil), "l1.identityroot.v1.MsgAttachDomain")
	gogoproto.RegisterType((*MsgAttachDomainResponse)(nil), "l1.identityroot.v1.MsgAttachDomainResponse")
	gogoproto.RegisterType((*MsgDetachDomain)(nil), "l1.identityroot.v1.MsgDetachDomain")
	gogoproto.RegisterType((*MsgDetachDomainResponse)(nil), "l1.identityroot.v1.MsgDetachDomainResponse")
	gogoproto.RegisterType((*MsgDisownAttachment)(nil), "l1.identityroot.v1.MsgDisownAttachment")
	gogoproto.RegisterType((*MsgDisownAttachmentResponse)(nil), "l1.identityroot.v1.MsgDisownAttachmentResponse")
	gogoproto.RegisterType((*MsgCreateSubdomain)(nil), "l1.identityroot.v1.MsgCreateSubdomain")
	gogoproto.RegisterType((*MsgCreateSubdomainResponse)(nil), "l1.identityroot.v1.MsgCreateSubdomainResponse")
	gogoproto.RegisterType((*MsgRenewName)(nil), "l1.identityroot.v1.MsgRenewName")
	gogoproto.RegisterType((*MsgRenewNameResponse)(nil), "l1.identityroot.v1.MsgRenewNameResponse")
	gogoproto.RegisterType((*MsgTransferName)(nil), "l1.identityroot.v1.MsgTransferName")
	gogoproto.RegisterType((*MsgTransferNameResponse)(nil), "l1.identityroot.v1.MsgTransferNameResponse")
	gogoproto.RegisterType((*MsgSetResolver)(nil), "l1.identityroot.v1.MsgSetResolver")
	gogoproto.RegisterType((*MsgSetResolverResponse)(nil), "l1.identityroot.v1.MsgSetResolverResponse")
	gogoproto.RegisterType((*MsgSetReverseRecord)(nil), "l1.identityroot.v1.MsgSetReverseRecord")
	gogoproto.RegisterType((*MsgSetReverseRecordResponse)(nil), "l1.identityroot.v1.MsgSetReverseRecordResponse")
	gogoproto.RegisterType((*MsgReserveName)(nil), "l1.identityroot.v1.MsgReserveName")
	gogoproto.RegisterType((*MsgReserveNameResponse)(nil), "l1.identityroot.v1.MsgReserveNameResponse")
	gogoproto.RegisterType((*MsgReleaseReservedName)(nil), "l1.identityroot.v1.MsgReleaseReservedName")
	gogoproto.RegisterType((*MsgReleaseReservedNameResponse)(nil), "l1.identityroot.v1.MsgReleaseReservedNameResponse")
	gogoproto.RegisterType((*MsgListForSale)(nil), "l1.identityroot.v1.MsgListForSale")
	gogoproto.RegisterType((*MsgListForSaleResponse)(nil), "l1.identityroot.v1.MsgListForSaleResponse")
	gogoproto.RegisterType((*MsgDelistName)(nil), "l1.identityroot.v1.MsgDelistName")
	gogoproto.RegisterType((*MsgDelistNameResponse)(nil), "l1.identityroot.v1.MsgDelistNameResponse")
	gogoproto.RegisterType((*MsgBuyListedName)(nil), "l1.identityroot.v1.MsgBuyListedName")
	gogoproto.RegisterType((*MsgBuyListedNameResponse)(nil), "l1.identityroot.v1.MsgBuyListedNameResponse")
}

func (m *MsgSendToNameCollection) Reset()		{ *m = MsgSendToNameCollection{} }
func (m *MsgSendToNameCollectionResponse) Reset()	{ *m = MsgSendToNameCollectionResponse{} }
func (m *MsgPlaceBid) Reset()				{ *m = MsgPlaceBid{} }
func (m *MsgPlaceBidResponse) Reset()			{ *m = MsgPlaceBidResponse{} }
func (m *MsgStartAuction) Reset()			{ *m = MsgStartAuction{} }
func (m *MsgStartAuctionResponse) Reset()		{ *m = MsgStartAuctionResponse{} }
func (m *MsgUpdatePriceTable) Reset()			{ *m = MsgUpdatePriceTable{} }
func (m *MsgUpdatePriceTableResponse) Reset()		{ *m = MsgUpdatePriceTableResponse{} }
func (m *MsgAttachDomain) Reset()			{ *m = MsgAttachDomain{} }
func (m *MsgAttachDomainResponse) Reset()		{ *m = MsgAttachDomainResponse{} }
func (m *MsgDetachDomain) Reset()			{ *m = MsgDetachDomain{} }
func (m *MsgDetachDomainResponse) Reset()		{ *m = MsgDetachDomainResponse{} }
func (m *MsgDisownAttachment) Reset()			{ *m = MsgDisownAttachment{} }
func (m *MsgDisownAttachmentResponse) Reset()		{ *m = MsgDisownAttachmentResponse{} }
func (m *MsgCreateSubdomain) Reset()			{ *m = MsgCreateSubdomain{} }
func (m *MsgCreateSubdomainResponse) Reset()		{ *m = MsgCreateSubdomainResponse{} }
func (m *MsgRenewName) Reset()				{ *m = MsgRenewName{} }
func (m *MsgRenewNameResponse) Reset()			{ *m = MsgRenewNameResponse{} }
func (m *MsgTransferName) Reset()			{ *m = MsgTransferName{} }
func (m *MsgTransferNameResponse) Reset()		{ *m = MsgTransferNameResponse{} }
func (m *MsgSetResolver) Reset()			{ *m = MsgSetResolver{} }
func (m *MsgSetResolverResponse) Reset()		{ *m = MsgSetResolverResponse{} }
func (m *MsgSetReverseRecord) Reset()			{ *m = MsgSetReverseRecord{} }
func (m *MsgSetReverseRecordResponse) Reset()		{ *m = MsgSetReverseRecordResponse{} }
func (m *MsgReserveName) Reset()			{ *m = MsgReserveName{} }
func (m *MsgReserveNameResponse) Reset()		{ *m = MsgReserveNameResponse{} }
func (m *MsgReleaseReservedName) Reset()		{ *m = MsgReleaseReservedName{} }
func (m *MsgReleaseReservedNameResponse) Reset()	{ *m = MsgReleaseReservedNameResponse{} }
func (m *MsgListForSale) Reset()			{ *m = MsgListForSale{} }
func (m *MsgListForSaleResponse) Reset()		{ *m = MsgListForSaleResponse{} }
func (m *MsgDelistName) Reset()			{ *m = MsgDelistName{} }
func (m *MsgDelistNameResponse) Reset()		{ *m = MsgDelistNameResponse{} }
func (m *MsgBuyListedName) Reset()			{ *m = MsgBuyListedName{} }
func (m *MsgBuyListedNameResponse) Reset()		{ *m = MsgBuyListedNameResponse{} }

func (m *MsgSendToNameCollection) String() string		{ return gogoproto.CompactTextString(m) }
func (m *MsgSendToNameCollectionResponse) String() string	{ return gogoproto.CompactTextString(m) }
func (m *MsgPlaceBid) String() string				{ return gogoproto.CompactTextString(m) }
func (m *MsgPlaceBidResponse) String() string			{ return gogoproto.CompactTextString(m) }
func (m *MsgStartAuction) String() string			{ return gogoproto.CompactTextString(m) }
func (m *MsgStartAuctionResponse) String() string		{ return gogoproto.CompactTextString(m) }
func (m *MsgUpdatePriceTable) String() string			{ return gogoproto.CompactTextString(m) }
func (m *MsgUpdatePriceTableResponse) String() string		{ return gogoproto.CompactTextString(m) }
func (m *MsgAttachDomain) String() string			{ return gogoproto.CompactTextString(m) }
func (m *MsgAttachDomainResponse) String() string		{ return gogoproto.CompactTextString(m) }
func (m *MsgDetachDomain) String() string			{ return gogoproto.CompactTextString(m) }
func (m *MsgDetachDomainResponse) String() string		{ return gogoproto.CompactTextString(m) }
func (m *MsgDisownAttachment) String() string			{ return gogoproto.CompactTextString(m) }
func (m *MsgDisownAttachmentResponse) String() string		{ return gogoproto.CompactTextString(m) }
func (m *MsgCreateSubdomain) String() string			{ return gogoproto.CompactTextString(m) }
func (m *MsgCreateSubdomainResponse) String() string		{ return gogoproto.CompactTextString(m) }
func (m *MsgRenewName) String() string				{ return gogoproto.CompactTextString(m) }
func (m *MsgRenewNameResponse) String() string			{ return gogoproto.CompactTextString(m) }
func (m *MsgTransferName) String() string			{ return gogoproto.CompactTextString(m) }
func (m *MsgTransferNameResponse) String() string		{ return gogoproto.CompactTextString(m) }
func (m *MsgSetResolver) String() string			{ return gogoproto.CompactTextString(m) }
func (m *MsgSetResolverResponse) String() string		{ return gogoproto.CompactTextString(m) }
func (m *MsgSetReverseRecord) String() string			{ return gogoproto.CompactTextString(m) }
func (m *MsgSetReverseRecordResponse) String() string		{ return gogoproto.CompactTextString(m) }
func (m *MsgReserveName) String() string			{ return gogoproto.CompactTextString(m) }
func (m *MsgReserveNameResponse) String() string		{ return gogoproto.CompactTextString(m) }
func (m *MsgReleaseReservedName) String() string		{ return gogoproto.CompactTextString(m) }
func (m *MsgReleaseReservedNameResponse) String() string	{ return gogoproto.CompactTextString(m) }
func (m *MsgListForSale) String() string			{ return gogoproto.CompactTextString(m) }
func (m *MsgListForSaleResponse) String() string		{ return gogoproto.CompactTextString(m) }
func (m *MsgDelistName) String() string			{ return gogoproto.CompactTextString(m) }
func (m *MsgDelistNameResponse) String() string		{ return gogoproto.CompactTextString(m) }
func (m *MsgBuyListedName) String() string			{ return gogoproto.CompactTextString(m) }
func (m *MsgBuyListedNameResponse) String() string		{ return gogoproto.CompactTextString(m) }

func (*MsgSendToNameCollection) ProtoMessage()		{}
func (*MsgSendToNameCollectionResponse) ProtoMessage()	{}
func (*MsgPlaceBid) ProtoMessage()			{}
func (*MsgPlaceBidResponse) ProtoMessage()		{}
func (*MsgStartAuction) ProtoMessage()			{}
func (*MsgStartAuctionResponse) ProtoMessage()		{}
func (*MsgUpdatePriceTable) ProtoMessage()		{}
func (*MsgUpdatePriceTableResponse) ProtoMessage()	{}
func (*MsgAttachDomain) ProtoMessage()			{}
func (*MsgAttachDomainResponse) ProtoMessage()		{}
func (*MsgDetachDomain) ProtoMessage()			{}
func (*MsgDetachDomainResponse) ProtoMessage()		{}
func (*MsgDisownAttachment) ProtoMessage()		{}
func (*MsgDisownAttachmentResponse) ProtoMessage()	{}
func (*MsgCreateSubdomain) ProtoMessage()		{}
func (*MsgCreateSubdomainResponse) ProtoMessage()	{}
func (*MsgRenewName) ProtoMessage()			{}
func (*MsgRenewNameResponse) ProtoMessage()		{}
func (*MsgTransferName) ProtoMessage()			{}
func (*MsgTransferNameResponse) ProtoMessage()		{}
func (*MsgSetResolver) ProtoMessage()			{}
func (*MsgSetResolverResponse) ProtoMessage()		{}
func (*MsgSetReverseRecord) ProtoMessage()		{}
func (*MsgSetReverseRecordResponse) ProtoMessage()	{}
func (*MsgReserveName) ProtoMessage()			{}
func (*MsgReserveNameResponse) ProtoMessage()		{}
func (*MsgReleaseReservedName) ProtoMessage()		{}
func (*MsgReleaseReservedNameResponse) ProtoMessage()	{}
func (*MsgListForSale) ProtoMessage()			{}
func (*MsgListForSaleResponse) ProtoMessage()		{}
func (*MsgDelistName) ProtoMessage()			{}
func (*MsgDelistNameResponse) ProtoMessage()		{}
func (*MsgBuyListedName) ProtoMessage()		{}
func (*MsgBuyListedNameResponse) ProtoMessage()	{}

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
func (*MsgAttachDomain) Descriptor() ([]byte, []int) {
	return fileDescriptorIdentityRootTx, []int{8}
}
func (*MsgAttachDomainResponse) Descriptor() ([]byte, []int) {
	return fileDescriptorIdentityRootTx, []int{9}
}
func (*MsgDetachDomain) Descriptor() ([]byte, []int) {
	return fileDescriptorIdentityRootTx, []int{10}
}
func (*MsgDetachDomainResponse) Descriptor() ([]byte, []int) {
	return fileDescriptorIdentityRootTx, []int{11}
}
func (*MsgCreateSubdomain) Descriptor() ([]byte, []int) {
	return fileDescriptorIdentityRootTx, []int{12}
}
func (*MsgCreateSubdomainResponse) Descriptor() ([]byte, []int) {
	return fileDescriptorIdentityRootTx, []int{13}
}
func (*MsgDisownAttachment) Descriptor() ([]byte, []int) {
	return fileDescriptorIdentityRootTx, []int{14}
}
func (*MsgDisownAttachmentResponse) Descriptor() ([]byte, []int) {
	return fileDescriptorIdentityRootTx, []int{15}
}
func (*MsgRenewName) Descriptor() ([]byte, []int) {
	return fileDescriptorIdentityRootTx, []int{16}
}
func (*MsgRenewNameResponse) Descriptor() ([]byte, []int) {
	return fileDescriptorIdentityRootTx, []int{17}
}
func (*MsgTransferName) Descriptor() ([]byte, []int) {
	return fileDescriptorIdentityRootTx, []int{18}
}
func (*MsgTransferNameResponse) Descriptor() ([]byte, []int) {
	return fileDescriptorIdentityRootTx, []int{19}
}
func (*MsgSetResolver) Descriptor() ([]byte, []int) {
	return fileDescriptorIdentityRootTx, []int{20}
}
func (*MsgSetResolverResponse) Descriptor() ([]byte, []int) {
	return fileDescriptorIdentityRootTx, []int{21}
}
func (*MsgSetReverseRecord) Descriptor() ([]byte, []int) {
	return fileDescriptorIdentityRootTx, []int{22}
}
func (*MsgSetReverseRecordResponse) Descriptor() ([]byte, []int) {
	return fileDescriptorIdentityRootTx, []int{23}
}
func (*MsgReserveName) Descriptor() ([]byte, []int) {
	return fileDescriptorIdentityRootTx, []int{24}
}
func (*MsgReserveNameResponse) Descriptor() ([]byte, []int) {
	return fileDescriptorIdentityRootTx, []int{25}
}
func (*MsgReleaseReservedName) Descriptor() ([]byte, []int) {
	return fileDescriptorIdentityRootTx, []int{26}
}
func (*MsgReleaseReservedNameResponse) Descriptor() ([]byte, []int) {
	return fileDescriptorIdentityRootTx, []int{27}
}
func (*MsgListForSale) Descriptor() ([]byte, []int) {
	return fileDescriptorIdentityRootTx, []int{28}
}
func (*MsgListForSaleResponse) Descriptor() ([]byte, []int) {
	return fileDescriptorIdentityRootTx, []int{29}
}
func (*MsgDelistName) Descriptor() ([]byte, []int) {
	return fileDescriptorIdentityRootTx, []int{30}
}
func (*MsgDelistNameResponse) Descriptor() ([]byte, []int) {
	return fileDescriptorIdentityRootTx, []int{31}
}
func (*MsgBuyListedName) Descriptor() ([]byte, []int) {
	return fileDescriptorIdentityRootTx, []int{32}
}
func (*MsgBuyListedNameResponse) Descriptor() ([]byte, []int) {
	return fileDescriptorIdentityRootTx, []int{33}
}
