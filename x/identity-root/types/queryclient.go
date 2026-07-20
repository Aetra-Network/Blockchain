package types

import (
	context "context"

	grpc1 "github.com/cosmos/gogoproto/grpc"
	grpc "google.golang.org/grpc"
)

// QueryClient is a hand-written client-side counterpart to QueryServer
// (query.go). x/identity-root hand-rolls its gRPC service registration
// instead of using protoc-gen-gocosmos output -- see query.go's own doc
// comment for why (no protoc/buf toolchain in this tree) -- and that never
// got a client-side counterpart, the same gap x/aez/types/queryclient.go and
// x/contracts/types/queryclient.go closed for their modules. This is the same
// fix applied here: a hand-written client so the CLI (x/identity-root/client/cli)
// can reach the l1.identityroot.v1.Query service without needing a
// protoc-generated stub.
type QueryClient interface {
	CollectionParams(ctx context.Context, in *QueryCollectionParamsRequest, opts ...grpc.CallOption) (*QueryCollectionParamsResponse, error)
	CollectionBalance(ctx context.Context, in *QueryCollectionBalanceRequest, opts ...grpc.CallOption) (*QueryCollectionBalanceResponse, error)
	PriceForLabel(ctx context.Context, in *QueryPriceForLabelRequest, opts ...grpc.CallOption) (*QueryPriceForLabelResponse, error)
	Auctions(ctx context.Context, in *QueryAuctionsRequest, opts ...grpc.CallOption) (*QueryAuctionsResponse, error)
	Auction(ctx context.Context, in *QueryAuctionRequest, opts ...grpc.CallOption) (*QueryAuctionResponse, error)
	DomainStatus(ctx context.Context, in *QueryDomainStatusRequest, opts ...grpc.CallOption) (*QueryDomainStatusResponse, error)
	NameRecord(ctx context.Context, in *QueryNameRecordRequest, opts ...grpc.CallOption) (*QueryNameRecordResponse, error)
	ResolveName(ctx context.Context, in *QueryResolveNameRequest, opts ...grpc.CallOption) (*QueryResolveNameResponse, error)
	ReverseRecord(ctx context.Context, in *QueryReverseRecordRequest, opts ...grpc.CallOption) (*QueryReverseRecordResponse, error)
	Subdomains(ctx context.Context, in *QuerySubdomainsRequest, opts ...grpc.CallOption) (*QuerySubdomainsResponse, error)
	NameZone(ctx context.Context, in *QueryNameZoneRequest, opts ...grpc.CallOption) (*QueryNameZoneResponse, error)
	Listing(ctx context.Context, in *QueryListingRequest, opts ...grpc.CallOption) (*QueryListingResponse, error)
}

type queryClient struct {
	cc grpc1.ClientConn
}

func NewQueryClient(cc grpc1.ClientConn) QueryClient {
	return &queryClient{cc}
}

func (c *queryClient) CollectionParams(ctx context.Context, in *QueryCollectionParamsRequest, opts ...grpc.CallOption) (*QueryCollectionParamsResponse, error) {
	out := new(QueryCollectionParamsResponse)
	if err := c.cc.Invoke(ctx, "/l1.identityroot.v1.Query/CollectionParams", in, out, opts...); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *queryClient) CollectionBalance(ctx context.Context, in *QueryCollectionBalanceRequest, opts ...grpc.CallOption) (*QueryCollectionBalanceResponse, error) {
	out := new(QueryCollectionBalanceResponse)
	if err := c.cc.Invoke(ctx, "/l1.identityroot.v1.Query/CollectionBalance", in, out, opts...); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *queryClient) PriceForLabel(ctx context.Context, in *QueryPriceForLabelRequest, opts ...grpc.CallOption) (*QueryPriceForLabelResponse, error) {
	out := new(QueryPriceForLabelResponse)
	if err := c.cc.Invoke(ctx, "/l1.identityroot.v1.Query/PriceForLabel", in, out, opts...); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *queryClient) Auctions(ctx context.Context, in *QueryAuctionsRequest, opts ...grpc.CallOption) (*QueryAuctionsResponse, error) {
	out := new(QueryAuctionsResponse)
	if err := c.cc.Invoke(ctx, "/l1.identityroot.v1.Query/Auctions", in, out, opts...); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *queryClient) Auction(ctx context.Context, in *QueryAuctionRequest, opts ...grpc.CallOption) (*QueryAuctionResponse, error) {
	out := new(QueryAuctionResponse)
	if err := c.cc.Invoke(ctx, "/l1.identityroot.v1.Query/Auction", in, out, opts...); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *queryClient) DomainStatus(ctx context.Context, in *QueryDomainStatusRequest, opts ...grpc.CallOption) (*QueryDomainStatusResponse, error) {
	out := new(QueryDomainStatusResponse)
	if err := c.cc.Invoke(ctx, "/l1.identityroot.v1.Query/DomainStatus", in, out, opts...); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *queryClient) NameRecord(ctx context.Context, in *QueryNameRecordRequest, opts ...grpc.CallOption) (*QueryNameRecordResponse, error) {
	out := new(QueryNameRecordResponse)
	if err := c.cc.Invoke(ctx, "/l1.identityroot.v1.Query/NameRecord", in, out, opts...); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *queryClient) ResolveName(ctx context.Context, in *QueryResolveNameRequest, opts ...grpc.CallOption) (*QueryResolveNameResponse, error) {
	out := new(QueryResolveNameResponse)
	if err := c.cc.Invoke(ctx, "/l1.identityroot.v1.Query/ResolveName", in, out, opts...); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *queryClient) ReverseRecord(ctx context.Context, in *QueryReverseRecordRequest, opts ...grpc.CallOption) (*QueryReverseRecordResponse, error) {
	out := new(QueryReverseRecordResponse)
	if err := c.cc.Invoke(ctx, "/l1.identityroot.v1.Query/ReverseRecord", in, out, opts...); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *queryClient) Subdomains(ctx context.Context, in *QuerySubdomainsRequest, opts ...grpc.CallOption) (*QuerySubdomainsResponse, error) {
	out := new(QuerySubdomainsResponse)
	if err := c.cc.Invoke(ctx, "/l1.identityroot.v1.Query/Subdomains", in, out, opts...); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *queryClient) NameZone(ctx context.Context, in *QueryNameZoneRequest, opts ...grpc.CallOption) (*QueryNameZoneResponse, error) {
	out := new(QueryNameZoneResponse)
	if err := c.cc.Invoke(ctx, "/l1.identityroot.v1.Query/NameZone", in, out, opts...); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *queryClient) Listing(ctx context.Context, in *QueryListingRequest, opts ...grpc.CallOption) (*QueryListingResponse, error) {
	out := new(QueryListingResponse)
	if err := c.cc.Invoke(ctx, "/l1.identityroot.v1.Query/Listing", in, out, opts...); err != nil {
		return nil, err
	}
	return out, nil
}
