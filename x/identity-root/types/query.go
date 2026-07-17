package types

import (
	"bytes"
	"compress/gzip"
	"context"

	"github.com/cosmos/gogoproto/grpc"
	gogoproto "github.com/cosmos/gogoproto/proto"
	grpcgo "google.golang.org/grpc"
	proto2 "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// x/identity-root's Query service (ANS Phase A get-methods). Collection
// get-methods (CollectionParams, CollectionBalance, PriceForLabel, Auctions,
// Auction) expose the message-driven collection; dns-item get-methods
// (DomainStatus, NameRecord, ResolveName, ReverseRecord, Subdomains) expose an
// individual name. Query messages carry no signer, so the descriptors are
// field-less (the x/aez query template) -- gogoproto marshals the real struct
// fields off their tags; the descriptor only registers the message names.

type QueryCollectionParamsRequest struct{}
type QueryCollectionParamsResponse struct {
	RootNamespace			string		`protobuf:"bytes,1,opt,name=root_namespace,proto3" json:"root_namespace,omitempty"`
	Enabled				bool		`protobuf:"varint,2,opt,name=enabled,proto3" json:"enabled,omitempty"`
	CollectionFeeNaet		uint64		`protobuf:"varint,3,opt,name=collection_fee_naet,proto3" json:"collection_fee_naet,omitempty"`
	RegistrationPeriodBlocks	uint64		`protobuf:"varint,4,opt,name=registration_period_blocks,proto3" json:"registration_period_blocks,omitempty"`
	RenewalWindowBlocks		uint64		`protobuf:"varint,5,opt,name=renewal_window_blocks,proto3" json:"renewal_window_blocks,omitempty"`
	IssuanceAuctionDurationBlocks	uint64		`protobuf:"varint,6,opt,name=issuance_auction_duration_blocks,proto3" json:"issuance_auction_duration_blocks,omitempty"`
	MinBidRaisePctBps		uint64		`protobuf:"varint,7,opt,name=min_bid_raise_pct_bps,proto3" json:"min_bid_raise_pct_bps,omitempty"`
	SweepIntervalBlocks		uint64		`protobuf:"varint,8,opt,name=sweep_interval_blocks,proto3" json:"sweep_interval_blocks,omitempty"`
	SweepFloorNaet			uint64		`protobuf:"varint,9,opt,name=sweep_floor_naet,proto3" json:"sweep_floor_naet,omitempty"`
	TreasuryModuleName		string		`protobuf:"bytes,10,opt,name=treasury_module_name,proto3" json:"treasury_module_name,omitempty"`
	MinLabelLens			[]uint32	`protobuf:"varint,11,rep,packed,name=min_label_lens,proto3" json:"min_label_lens,omitempty"`
	PricesNaet			[]string	`protobuf:"bytes,12,rep,name=prices_naet,proto3" json:"prices_naet,omitempty"`
}

type QueryCollectionBalanceRequest struct{}
type QueryCollectionBalanceResponse struct {
	BalanceNaet	uint64	`protobuf:"varint,1,opt,name=balance_naet,proto3" json:"balance_naet,omitempty"`
	EscrowedNaet	uint64	`protobuf:"varint,2,opt,name=escrowed_naet,proto3" json:"escrowed_naet,omitempty"`
	RetainedNaet	uint64	`protobuf:"varint,3,opt,name=retained_naet,proto3" json:"retained_naet,omitempty"`
}

type QueryPriceForLabelRequest struct {
	Label string `protobuf:"bytes,1,opt,name=label,proto3" json:"label,omitempty"`
}
type QueryPriceForLabelResponse struct {
	Found		bool	`protobuf:"varint,1,opt,name=found,proto3" json:"found,omitempty"`
	PriceNaet	string	`protobuf:"bytes,2,opt,name=price_naet,proto3" json:"price_naet,omitempty"`
}

// QueryAuction is a flattened, wire-friendly view of an Auction.
type QueryAuction struct {
	Name		string	`protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
	Kind		string	`protobuf:"bytes,2,opt,name=kind,proto3" json:"kind,omitempty"`
	Seller		string	`protobuf:"bytes,3,opt,name=seller,proto3" json:"seller,omitempty"`
	OpenPriceNaet	string	`protobuf:"bytes,4,opt,name=open_price_naet,proto3" json:"open_price_naet,omitempty"`
	HighBidNaet	string	`protobuf:"bytes,5,opt,name=high_bid_naet,proto3" json:"high_bid_naet,omitempty"`
	HighBidder	string	`protobuf:"bytes,6,opt,name=high_bidder,proto3" json:"high_bidder,omitempty"`
	DeadlineHeight	uint64	`protobuf:"varint,7,opt,name=deadline_height,proto3" json:"deadline_height,omitempty"`
	CreatedHeight	uint64	`protobuf:"varint,8,opt,name=created_height,proto3" json:"created_height,omitempty"`
}

// AuctionView flattens an Auction into its query shape.
func AuctionView(a Auction) QueryAuction {
	return QueryAuction{
		Name:		a.Name,
		Kind:		a.Kind,
		Seller:		a.Seller,
		OpenPriceNaet:	a.OpenPriceNaet,
		HighBidNaet:	a.HighBidNaet,
		HighBidder:	a.HighBidder,
		DeadlineHeight:	a.DeadlineHeight,
		CreatedHeight:	a.CreatedHeight,
	}
}

type QueryAuctionsRequest struct{}
type QueryAuctionsResponse struct {
	Auctions []QueryAuction `protobuf:"bytes,1,rep,name=auctions,proto3" json:"auctions"`
}

type QueryAuctionRequest struct {
	Name string `protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
}
type QueryAuctionResponse struct {
	Found	bool		`protobuf:"varint,1,opt,name=found,proto3" json:"found,omitempty"`
	Auction	QueryAuction	`protobuf:"bytes,2,opt,name=auction,proto3" json:"auction"`
}

type QueryDomainStatusRequest struct {
	Name	string	`protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
	Height	uint64	`protobuf:"varint,2,opt,name=height,proto3" json:"height,omitempty"`
}
type QueryDomainStatusResponse struct {
	Found		bool	`protobuf:"varint,1,opt,name=found,proto3" json:"found,omitempty"`
	Active		bool	`protobuf:"varint,2,opt,name=active,proto3" json:"active,omitempty"`
	InAuction	bool	`protobuf:"varint,3,opt,name=in_auction,proto3" json:"in_auction,omitempty"`
	Owner		string	`protobuf:"bytes,4,opt,name=owner,proto3" json:"owner,omitempty"`
	ExpiryHeight	uint64	`protobuf:"varint,5,opt,name=expiry_height,proto3" json:"expiry_height,omitempty"`
}

type QueryNameRecordRequest struct {
	Name string `protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
}
type QueryNameRecordResponse struct {
	Found		bool	`protobuf:"varint,1,opt,name=found,proto3" json:"found,omitempty"`
	Name		string	`protobuf:"bytes,2,opt,name=name,proto3" json:"name,omitempty"`
	Owner		string	`protobuf:"bytes,3,opt,name=owner,proto3" json:"owner,omitempty"`
	ResolverRoot	string	`protobuf:"bytes,4,opt,name=resolver_root,proto3" json:"resolver_root,omitempty"`
	ExpiryHeight	uint64	`protobuf:"varint,5,opt,name=expiry_height,proto3" json:"expiry_height,omitempty"`
	ParentName	string	`protobuf:"bytes,6,opt,name=parent_name,proto3" json:"parent_name,omitempty"`
}

type QueryResolveNameRequest struct {
	Name	string	`protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
	Height	uint64	`protobuf:"varint,2,opt,name=height,proto3" json:"height,omitempty"`
}
type QueryResolveNameResponse struct {
	Found		bool	`protobuf:"varint,1,opt,name=found,proto3" json:"found,omitempty"`
	Active		bool	`protobuf:"varint,2,opt,name=active,proto3" json:"active,omitempty"`
	Name		string	`protobuf:"bytes,3,opt,name=name,proto3" json:"name,omitempty"`
	ResolverRoot	string	`protobuf:"bytes,4,opt,name=resolver_root,proto3" json:"resolver_root,omitempty"`
}

type QueryReverseRecordRequest struct {
	Address string `protobuf:"bytes,1,opt,name=address,proto3" json:"address,omitempty"`
}
type QueryReverseRecordResponse struct {
	Found	bool	`protobuf:"varint,1,opt,name=found,proto3" json:"found,omitempty"`
	Name	string	`protobuf:"bytes,2,opt,name=name,proto3" json:"name,omitempty"`
	Owner	string	`protobuf:"bytes,3,opt,name=owner,proto3" json:"owner,omitempty"`
}

type QuerySubdomainsRequest struct {
	ParentName string `protobuf:"bytes,1,opt,name=parent_name,proto3" json:"parent_name,omitempty"`
}
type QuerySubdomainsResponse struct {
	Names []string `protobuf:"bytes,1,rep,name=names,proto3" json:"names,omitempty"`
}

type QueryServer interface {
	CollectionParams(context.Context, *QueryCollectionParamsRequest) (*QueryCollectionParamsResponse, error)
	CollectionBalance(context.Context, *QueryCollectionBalanceRequest) (*QueryCollectionBalanceResponse, error)
	PriceForLabel(context.Context, *QueryPriceForLabelRequest) (*QueryPriceForLabelResponse, error)
	Auctions(context.Context, *QueryAuctionsRequest) (*QueryAuctionsResponse, error)
	Auction(context.Context, *QueryAuctionRequest) (*QueryAuctionResponse, error)
	DomainStatus(context.Context, *QueryDomainStatusRequest) (*QueryDomainStatusResponse, error)
	NameRecord(context.Context, *QueryNameRecordRequest) (*QueryNameRecordResponse, error)
	ResolveName(context.Context, *QueryResolveNameRequest) (*QueryResolveNameResponse, error)
	ReverseRecord(context.Context, *QueryReverseRecordRequest) (*QueryReverseRecordResponse, error)
	Subdomains(context.Context, *QuerySubdomainsRequest) (*QuerySubdomainsResponse, error)
}

func RegisterQueryServer(s grpc.Server, srv QueryServer) {
	s.RegisterService(&Query_serviceDesc, srv)
}

type queryServiceCall func(context.Context, interface{}, interface{}) (interface{}, error)

var Query_serviceDesc = grpcgo.ServiceDesc{
	ServiceName:	"l1.identityroot.v1.Query",
	HandlerType:	(*QueryServer)(nil),
	Methods: []grpcgo.MethodDesc{
		queryMethodDesc("CollectionParams", queryHandler("CollectionParams", func() interface{} { return new(QueryCollectionParamsRequest) }, func(ctx context.Context, srv interface{}, req interface{}) (interface{}, error) {
			return srv.(QueryServer).CollectionParams(ctx, req.(*QueryCollectionParamsRequest))
		})),
		queryMethodDesc("CollectionBalance", queryHandler("CollectionBalance", func() interface{} { return new(QueryCollectionBalanceRequest) }, func(ctx context.Context, srv interface{}, req interface{}) (interface{}, error) {
			return srv.(QueryServer).CollectionBalance(ctx, req.(*QueryCollectionBalanceRequest))
		})),
		queryMethodDesc("PriceForLabel", queryHandler("PriceForLabel", func() interface{} { return new(QueryPriceForLabelRequest) }, func(ctx context.Context, srv interface{}, req interface{}) (interface{}, error) {
			return srv.(QueryServer).PriceForLabel(ctx, req.(*QueryPriceForLabelRequest))
		})),
		queryMethodDesc("Auctions", queryHandler("Auctions", func() interface{} { return new(QueryAuctionsRequest) }, func(ctx context.Context, srv interface{}, req interface{}) (interface{}, error) {
			return srv.(QueryServer).Auctions(ctx, req.(*QueryAuctionsRequest))
		})),
		queryMethodDesc("Auction", queryHandler("Auction", func() interface{} { return new(QueryAuctionRequest) }, func(ctx context.Context, srv interface{}, req interface{}) (interface{}, error) {
			return srv.(QueryServer).Auction(ctx, req.(*QueryAuctionRequest))
		})),
		queryMethodDesc("DomainStatus", queryHandler("DomainStatus", func() interface{} { return new(QueryDomainStatusRequest) }, func(ctx context.Context, srv interface{}, req interface{}) (interface{}, error) {
			return srv.(QueryServer).DomainStatus(ctx, req.(*QueryDomainStatusRequest))
		})),
		queryMethodDesc("NameRecord", queryHandler("NameRecord", func() interface{} { return new(QueryNameRecordRequest) }, func(ctx context.Context, srv interface{}, req interface{}) (interface{}, error) {
			return srv.(QueryServer).NameRecord(ctx, req.(*QueryNameRecordRequest))
		})),
		queryMethodDesc("ResolveName", queryHandler("ResolveName", func() interface{} { return new(QueryResolveNameRequest) }, func(ctx context.Context, srv interface{}, req interface{}) (interface{}, error) {
			return srv.(QueryServer).ResolveName(ctx, req.(*QueryResolveNameRequest))
		})),
		queryMethodDesc("ReverseRecord", queryHandler("ReverseRecord", func() interface{} { return new(QueryReverseRecordRequest) }, func(ctx context.Context, srv interface{}, req interface{}) (interface{}, error) {
			return srv.(QueryServer).ReverseRecord(ctx, req.(*QueryReverseRecordRequest))
		})),
		queryMethodDesc("Subdomains", queryHandler("Subdomains", func() interface{} { return new(QuerySubdomainsRequest) }, func(ctx context.Context, srv interface{}, req interface{}) (interface{}, error) {
			return srv.(QueryServer).Subdomains(ctx, req.(*QuerySubdomainsRequest))
		})),
	},
	Metadata:	"l1/identityroot/v1/query.proto",
}

func queryMethodDesc(name string, handler grpcgo.MethodHandler) grpcgo.MethodDesc {
	return grpcgo.MethodDesc{MethodName: name, Handler: handler}
}

func queryHandler(method string, newReq func() interface{}, call queryServiceCall) grpcgo.MethodHandler {
	return func(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
		req := newReq()
		if err := dec(req); err != nil {
			return nil, err
		}
		if interceptor == nil {
			return call(ctx, srv, req)
		}
		return interceptor(ctx, req, &grpcgo.UnaryServerInfo{Server: srv, FullMethod: method}, func(ctx context.Context, request interface{}) (interface{}, error) {
			return call(ctx, srv, request)
		})
	}
}

func init() {
	registerQueryTypes()
	gogoproto.RegisterFile("l1/identityroot/v1/query.proto", buildQueryFileDescriptor("l1/identityroot/v1/query.proto", "l1.identityroot.v1", "Query",
		[]string{
			"QueryCollectionParamsRequest", "QueryCollectionParamsResponse",
			"QueryCollectionBalanceRequest", "QueryCollectionBalanceResponse",
			"QueryPriceForLabelRequest", "QueryPriceForLabelResponse",
			"QueryAuction",
			"QueryAuctionsRequest", "QueryAuctionsResponse",
			"QueryAuctionRequest", "QueryAuctionResponse",
			"QueryDomainStatusRequest", "QueryDomainStatusResponse",
			"QueryNameRecordRequest", "QueryNameRecordResponse",
			"QueryResolveNameRequest", "QueryResolveNameResponse",
			"QueryReverseRecordRequest", "QueryReverseRecordResponse",
			"QuerySubdomainsRequest", "QuerySubdomainsResponse",
		},
		[][3]string{
			{"CollectionParams", "QueryCollectionParamsRequest", "QueryCollectionParamsResponse"},
			{"CollectionBalance", "QueryCollectionBalanceRequest", "QueryCollectionBalanceResponse"},
			{"PriceForLabel", "QueryPriceForLabelRequest", "QueryPriceForLabelResponse"},
			{"Auctions", "QueryAuctionsRequest", "QueryAuctionsResponse"},
			{"Auction", "QueryAuctionRequest", "QueryAuctionResponse"},
			{"DomainStatus", "QueryDomainStatusRequest", "QueryDomainStatusResponse"},
			{"NameRecord", "QueryNameRecordRequest", "QueryNameRecordResponse"},
			{"ResolveName", "QueryResolveNameRequest", "QueryResolveNameResponse"},
			{"ReverseRecord", "QueryReverseRecordRequest", "QueryReverseRecordResponse"},
			{"Subdomains", "QuerySubdomainsRequest", "QuerySubdomainsResponse"},
		}))
}

func buildQueryFileDescriptor(path, pkg, service string, messageNames []string, methods [][3]string) []byte {
	messages := make([]*descriptorpb.DescriptorProto, 0, len(messageNames))
	for _, name := range messageNames {
		messages = append(messages, &descriptorpb.DescriptorProto{Name: queryStringPtr(name)})
	}
	md := make([]*descriptorpb.MethodDescriptorProto, 0, len(methods))
	for _, method := range methods {
		md = append(md, &descriptorpb.MethodDescriptorProto{Name: queryStringPtr(method[0]), InputType: queryStringPtr("." + pkg + "." + method[1]), OutputType: queryStringPtr("." + pkg + "." + method[2])})
	}
	svc := &descriptorpb.ServiceDescriptorProto{Name: queryStringPtr(service), Method: md}
	fd := &descriptorpb.FileDescriptorProto{Name: queryStringPtr(path), Package: queryStringPtr(pkg), Syntax: queryStringPtr("proto3"), MessageType: messages, Service: []*descriptorpb.ServiceDescriptorProto{svc}}
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

func queryStringPtr(value string) *string { return &value }

func registerQueryTypes() {
	for _, item := range []struct {
		msg	gogoproto.Message
		name	string
	}{
		{(*QueryCollectionParamsRequest)(nil), "l1.identityroot.v1.QueryCollectionParamsRequest"},
		{(*QueryCollectionParamsResponse)(nil), "l1.identityroot.v1.QueryCollectionParamsResponse"},
		{(*QueryCollectionBalanceRequest)(nil), "l1.identityroot.v1.QueryCollectionBalanceRequest"},
		{(*QueryCollectionBalanceResponse)(nil), "l1.identityroot.v1.QueryCollectionBalanceResponse"},
		{(*QueryPriceForLabelRequest)(nil), "l1.identityroot.v1.QueryPriceForLabelRequest"},
		{(*QueryPriceForLabelResponse)(nil), "l1.identityroot.v1.QueryPriceForLabelResponse"},
		{(*QueryAuction)(nil), "l1.identityroot.v1.QueryAuction"},
		{(*QueryAuctionsRequest)(nil), "l1.identityroot.v1.QueryAuctionsRequest"},
		{(*QueryAuctionsResponse)(nil), "l1.identityroot.v1.QueryAuctionsResponse"},
		{(*QueryAuctionRequest)(nil), "l1.identityroot.v1.QueryAuctionRequest"},
		{(*QueryAuctionResponse)(nil), "l1.identityroot.v1.QueryAuctionResponse"},
		{(*QueryDomainStatusRequest)(nil), "l1.identityroot.v1.QueryDomainStatusRequest"},
		{(*QueryDomainStatusResponse)(nil), "l1.identityroot.v1.QueryDomainStatusResponse"},
		{(*QueryNameRecordRequest)(nil), "l1.identityroot.v1.QueryNameRecordRequest"},
		{(*QueryNameRecordResponse)(nil), "l1.identityroot.v1.QueryNameRecordResponse"},
		{(*QueryResolveNameRequest)(nil), "l1.identityroot.v1.QueryResolveNameRequest"},
		{(*QueryResolveNameResponse)(nil), "l1.identityroot.v1.QueryResolveNameResponse"},
		{(*QueryReverseRecordRequest)(nil), "l1.identityroot.v1.QueryReverseRecordRequest"},
		{(*QueryReverseRecordResponse)(nil), "l1.identityroot.v1.QueryReverseRecordResponse"},
		{(*QuerySubdomainsRequest)(nil), "l1.identityroot.v1.QuerySubdomainsRequest"},
		{(*QuerySubdomainsResponse)(nil), "l1.identityroot.v1.QuerySubdomainsResponse"},
	} {
		gogoproto.RegisterType(item.msg, item.name)
	}
}

func (m *QueryCollectionParamsRequest) Reset()	{ *m = QueryCollectionParamsRequest{} }
func (m *QueryCollectionParamsResponse) Reset()	{ *m = QueryCollectionParamsResponse{} }
func (m *QueryCollectionBalanceRequest) Reset()	{ *m = QueryCollectionBalanceRequest{} }
func (m *QueryCollectionBalanceResponse) Reset()	{ *m = QueryCollectionBalanceResponse{} }
func (m *QueryPriceForLabelRequest) Reset()	{ *m = QueryPriceForLabelRequest{} }
func (m *QueryPriceForLabelResponse) Reset()	{ *m = QueryPriceForLabelResponse{} }
func (m *QueryAuction) Reset()			{ *m = QueryAuction{} }
func (m *QueryAuctionsRequest) Reset()		{ *m = QueryAuctionsRequest{} }
func (m *QueryAuctionsResponse) Reset()		{ *m = QueryAuctionsResponse{} }
func (m *QueryAuctionRequest) Reset()		{ *m = QueryAuctionRequest{} }
func (m *QueryAuctionResponse) Reset()		{ *m = QueryAuctionResponse{} }
func (m *QueryDomainStatusRequest) Reset()	{ *m = QueryDomainStatusRequest{} }
func (m *QueryDomainStatusResponse) Reset()	{ *m = QueryDomainStatusResponse{} }
func (m *QueryNameRecordRequest) Reset()	{ *m = QueryNameRecordRequest{} }
func (m *QueryNameRecordResponse) Reset()	{ *m = QueryNameRecordResponse{} }
func (m *QueryResolveNameRequest) Reset()	{ *m = QueryResolveNameRequest{} }
func (m *QueryResolveNameResponse) Reset()	{ *m = QueryResolveNameResponse{} }
func (m *QueryReverseRecordRequest) Reset()	{ *m = QueryReverseRecordRequest{} }
func (m *QueryReverseRecordResponse) Reset()	{ *m = QueryReverseRecordResponse{} }
func (m *QuerySubdomainsRequest) Reset()	{ *m = QuerySubdomainsRequest{} }
func (m *QuerySubdomainsResponse) Reset()	{ *m = QuerySubdomainsResponse{} }

func (m *QueryCollectionParamsRequest) String() string	{ return gogoproto.CompactTextString(m) }
func (m *QueryCollectionParamsResponse) String() string	{ return gogoproto.CompactTextString(m) }
func (m *QueryCollectionBalanceRequest) String() string	{ return gogoproto.CompactTextString(m) }
func (m *QueryCollectionBalanceResponse) String() string	{ return gogoproto.CompactTextString(m) }
func (m *QueryPriceForLabelRequest) String() string	{ return gogoproto.CompactTextString(m) }
func (m *QueryPriceForLabelResponse) String() string	{ return gogoproto.CompactTextString(m) }
func (m *QueryAuction) String() string			{ return gogoproto.CompactTextString(m) }
func (m *QueryAuctionsRequest) String() string		{ return gogoproto.CompactTextString(m) }
func (m *QueryAuctionsResponse) String() string		{ return gogoproto.CompactTextString(m) }
func (m *QueryAuctionRequest) String() string		{ return gogoproto.CompactTextString(m) }
func (m *QueryAuctionResponse) String() string		{ return gogoproto.CompactTextString(m) }
func (m *QueryDomainStatusRequest) String() string	{ return gogoproto.CompactTextString(m) }
func (m *QueryDomainStatusResponse) String() string	{ return gogoproto.CompactTextString(m) }
func (m *QueryNameRecordRequest) String() string	{ return gogoproto.CompactTextString(m) }
func (m *QueryNameRecordResponse) String() string	{ return gogoproto.CompactTextString(m) }
func (m *QueryResolveNameRequest) String() string	{ return gogoproto.CompactTextString(m) }
func (m *QueryResolveNameResponse) String() string	{ return gogoproto.CompactTextString(m) }
func (m *QueryReverseRecordRequest) String() string	{ return gogoproto.CompactTextString(m) }
func (m *QueryReverseRecordResponse) String() string	{ return gogoproto.CompactTextString(m) }
func (m *QuerySubdomainsRequest) String() string	{ return gogoproto.CompactTextString(m) }
func (m *QuerySubdomainsResponse) String() string	{ return gogoproto.CompactTextString(m) }

func (*QueryCollectionParamsRequest) ProtoMessage()	{}
func (*QueryCollectionParamsResponse) ProtoMessage()	{}
func (*QueryCollectionBalanceRequest) ProtoMessage()	{}
func (*QueryCollectionBalanceResponse) ProtoMessage()	{}
func (*QueryPriceForLabelRequest) ProtoMessage()	{}
func (*QueryPriceForLabelResponse) ProtoMessage()	{}
func (*QueryAuction) ProtoMessage()			{}
func (*QueryAuctionsRequest) ProtoMessage()		{}
func (*QueryAuctionsResponse) ProtoMessage()		{}
func (*QueryAuctionRequest) ProtoMessage()		{}
func (*QueryAuctionResponse) ProtoMessage()		{}
func (*QueryDomainStatusRequest) ProtoMessage()		{}
func (*QueryDomainStatusResponse) ProtoMessage()	{}
func (*QueryNameRecordRequest) ProtoMessage()		{}
func (*QueryNameRecordResponse) ProtoMessage()		{}
func (*QueryResolveNameRequest) ProtoMessage()		{}
func (*QueryResolveNameResponse) ProtoMessage()		{}
func (*QueryReverseRecordRequest) ProtoMessage()	{}
func (*QueryReverseRecordResponse) ProtoMessage()	{}
func (*QuerySubdomainsRequest) ProtoMessage()		{}
func (*QuerySubdomainsResponse) ProtoMessage()		{}
