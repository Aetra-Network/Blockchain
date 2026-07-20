package types

import (
	context "context"

	grpc1 "github.com/cosmos/gogoproto/grpc"
	grpc "google.golang.org/grpc"
)

// QueryClient is a hand-written client-side counterpart to QueryServer
// (service.go). x/aez hand-rolls its gRPC service registration instead of
// using protoc-gen-gocosmos output -- see service.go's own doc comment for why
// (no protoc/buf toolchain in this tree) -- and that never got a client-side
// counterpart, the same gap x/contracts/types/queryclient.go closed for its
// module. This is the same fix applied here: a hand-written client so the CLI
// (x/aez/client/cli) can reach the l1.aez.v1.Query service without needing a
// protoc-generated stub.
type QueryClient interface {
	Params(ctx context.Context, in *QueryParamsRequest, opts ...grpc.CallOption) (*QueryParamsResponse, error)
	RoutingTable(ctx context.Context, in *QueryRoutingTableRequest, opts ...grpc.CallOption) (*QueryRoutingTableResponse, error)
	PendingRoutingTable(ctx context.Context, in *QueryPendingRoutingTableRequest, opts ...grpc.CallOption) (*QueryPendingRoutingTableResponse, error)
	Zones(ctx context.Context, in *QueryZonesRequest, opts ...grpc.CallOption) (*QueryZonesResponse, error)
	ZoneOf(ctx context.Context, in *QueryZoneOfRequest, opts ...grpc.CallOption) (*QueryZoneOfResponse, error)
}

type queryClient struct {
	cc grpc1.ClientConn
}

func NewQueryClient(cc grpc1.ClientConn) QueryClient {
	return &queryClient{cc}
}

func (c *queryClient) Params(ctx context.Context, in *QueryParamsRequest, opts ...grpc.CallOption) (*QueryParamsResponse, error) {
	out := new(QueryParamsResponse)
	if err := c.cc.Invoke(ctx, "/l1.aez.v1.Query/Params", in, out, opts...); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *queryClient) RoutingTable(ctx context.Context, in *QueryRoutingTableRequest, opts ...grpc.CallOption) (*QueryRoutingTableResponse, error) {
	out := new(QueryRoutingTableResponse)
	if err := c.cc.Invoke(ctx, "/l1.aez.v1.Query/RoutingTable", in, out, opts...); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *queryClient) PendingRoutingTable(ctx context.Context, in *QueryPendingRoutingTableRequest, opts ...grpc.CallOption) (*QueryPendingRoutingTableResponse, error) {
	out := new(QueryPendingRoutingTableResponse)
	if err := c.cc.Invoke(ctx, "/l1.aez.v1.Query/PendingRoutingTable", in, out, opts...); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *queryClient) Zones(ctx context.Context, in *QueryZonesRequest, opts ...grpc.CallOption) (*QueryZonesResponse, error) {
	out := new(QueryZonesResponse)
	if err := c.cc.Invoke(ctx, "/l1.aez.v1.Query/Zones", in, out, opts...); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *queryClient) ZoneOf(ctx context.Context, in *QueryZoneOfRequest, opts ...grpc.CallOption) (*QueryZoneOfResponse, error) {
	out := new(QueryZoneOfResponse)
	if err := c.cc.Invoke(ctx, "/l1.aez.v1.Query/ZoneOf", in, out, opts...); err != nil {
		return nil, err
	}
	return out, nil
}
