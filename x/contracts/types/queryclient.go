package types

import (
	context "context"

	grpc1 "github.com/cosmos/gogoproto/grpc"
	grpc "google.golang.org/grpc"
)

// QueryClient is a hand-written client-side counterpart to GRPCQueryServer
// (service.go). x/contracts hand-rolls its gRPC service registration instead
// of using protoc-gen-gocosmos output (see the comments on
// buildContractsQueryFileDescriptor), and mirroring that file's go_package
// ("github.com/sovereign-l1/l1/x/contracts/types") never got a client-side
// counterpart. A real protoc-generated client for the same l1.contracts.v1.Query
// service DOES exist under api/l1/contracts/v1, but it must NOT be imported
// into the same process as this package: both packages register identical
// proto type names in the global (gogoproto and protobuf-go) registries, and
// combining them corrupts type resolution badly enough to panic
// cosmos-sdk's codec.InterfaceRegistry.RegisterImplementations at app-boot
// (observed: "concrete type *types.MsgStoreCode has already been registered
// under typeURL /, cannot register *types.MsgDeployContract under same
// typeURL"). This client exists so callers (the CLI, the REST gateway) can
// reach the query service without ever importing api/l1/contracts/v1.
type QueryClient interface {
	Params(ctx context.Context, in *QueryParamsRequest, opts ...grpc.CallOption) (*QueryParamsResponse, error)
	Code(ctx context.Context, in *QueryCodeRequest, opts ...grpc.CallOption) (*QueryCodeResponse, error)
	Codes(ctx context.Context, in *QueryCodesRequest, opts ...grpc.CallOption) (*QueryCodesResponse, error)
	Contract(ctx context.Context, in *QueryContractRequest, opts ...grpc.CallOption) (*QueryContractResponse, error)
	Contracts(ctx context.Context, in *QueryContractsRequest, opts ...grpc.CallOption) (*QueryContractsResponse, error)
	ContractStorage(ctx context.Context, in *QueryContractStorageRequest, opts ...grpc.CallOption) (*QueryContractStorageResponse, error)
	ContractReceipts(ctx context.Context, in *QueryContractReceiptsRequest, opts ...grpc.CallOption) (*QueryContractReceiptsResponse, error)
	ContractQueue(ctx context.Context, in *QueryContractQueueRequest, opts ...grpc.CallOption) (*QueryContractQueueResponse, error)
	ContractEvents(ctx context.Context, in *QueryContractEventsRequest, opts ...grpc.CallOption) (*QueryContractEventsResponse, error)
	ContractStateRoot(ctx context.Context, in *QueryContractStateRootRequest, opts ...grpc.CallOption) (*QueryContractStateRootResponse, error)
	SecurityAttestations(ctx context.Context, in *QuerySecurityAttestationsRequest, opts ...grpc.CallOption) (*QuerySecurityAttestationsResponse, error)
	SecurityBadge(ctx context.Context, in *QuerySecurityBadgeRequest, opts ...grpc.CallOption) (*QuerySecurityBadgeResponse, error)
	ContractGet(ctx context.Context, in *QueryContractGetRequest, opts ...grpc.CallOption) (*QueryContractGetResponse, error)
}

type queryClient struct {
	cc grpc1.ClientConn
}

func NewQueryClient(cc grpc1.ClientConn) QueryClient {
	return &queryClient{cc}
}

func (c *queryClient) Params(ctx context.Context, in *QueryParamsRequest, opts ...grpc.CallOption) (*QueryParamsResponse, error) {
	out := new(QueryParamsResponse)
	if err := c.cc.Invoke(ctx, "/l1.contracts.v1.Query/Params", in, out, opts...); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *queryClient) Code(ctx context.Context, in *QueryCodeRequest, opts ...grpc.CallOption) (*QueryCodeResponse, error) {
	out := new(QueryCodeResponse)
	if err := c.cc.Invoke(ctx, "/l1.contracts.v1.Query/Code", in, out, opts...); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *queryClient) Codes(ctx context.Context, in *QueryCodesRequest, opts ...grpc.CallOption) (*QueryCodesResponse, error) {
	out := new(QueryCodesResponse)
	if err := c.cc.Invoke(ctx, "/l1.contracts.v1.Query/Codes", in, out, opts...); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *queryClient) Contract(ctx context.Context, in *QueryContractRequest, opts ...grpc.CallOption) (*QueryContractResponse, error) {
	out := new(QueryContractResponse)
	if err := c.cc.Invoke(ctx, "/l1.contracts.v1.Query/Contract", in, out, opts...); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *queryClient) Contracts(ctx context.Context, in *QueryContractsRequest, opts ...grpc.CallOption) (*QueryContractsResponse, error) {
	out := new(QueryContractsResponse)
	if err := c.cc.Invoke(ctx, "/l1.contracts.v1.Query/Contracts", in, out, opts...); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *queryClient) ContractStorage(ctx context.Context, in *QueryContractStorageRequest, opts ...grpc.CallOption) (*QueryContractStorageResponse, error) {
	out := new(QueryContractStorageResponse)
	if err := c.cc.Invoke(ctx, "/l1.contracts.v1.Query/ContractStorage", in, out, opts...); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *queryClient) ContractReceipts(ctx context.Context, in *QueryContractReceiptsRequest, opts ...grpc.CallOption) (*QueryContractReceiptsResponse, error) {
	out := new(QueryContractReceiptsResponse)
	if err := c.cc.Invoke(ctx, "/l1.contracts.v1.Query/ContractReceipts", in, out, opts...); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *queryClient) ContractQueue(ctx context.Context, in *QueryContractQueueRequest, opts ...grpc.CallOption) (*QueryContractQueueResponse, error) {
	out := new(QueryContractQueueResponse)
	if err := c.cc.Invoke(ctx, "/l1.contracts.v1.Query/ContractQueue", in, out, opts...); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *queryClient) ContractEvents(ctx context.Context, in *QueryContractEventsRequest, opts ...grpc.CallOption) (*QueryContractEventsResponse, error) {
	out := new(QueryContractEventsResponse)
	if err := c.cc.Invoke(ctx, "/l1.contracts.v1.Query/ContractEvents", in, out, opts...); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *queryClient) ContractStateRoot(ctx context.Context, in *QueryContractStateRootRequest, opts ...grpc.CallOption) (*QueryContractStateRootResponse, error) {
	out := new(QueryContractStateRootResponse)
	if err := c.cc.Invoke(ctx, "/l1.contracts.v1.Query/ContractStateRoot", in, out, opts...); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *queryClient) SecurityAttestations(ctx context.Context, in *QuerySecurityAttestationsRequest, opts ...grpc.CallOption) (*QuerySecurityAttestationsResponse, error) {
	out := new(QuerySecurityAttestationsResponse)
	if err := c.cc.Invoke(ctx, "/l1.contracts.v1.Query/SecurityAttestations", in, out, opts...); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *queryClient) SecurityBadge(ctx context.Context, in *QuerySecurityBadgeRequest, opts ...grpc.CallOption) (*QuerySecurityBadgeResponse, error) {
	out := new(QuerySecurityBadgeResponse)
	if err := c.cc.Invoke(ctx, "/l1.contracts.v1.Query/SecurityBadge", in, out, opts...); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *queryClient) ContractGet(ctx context.Context, in *QueryContractGetRequest, opts ...grpc.CallOption) (*QueryContractGetResponse, error) {
	out := new(QueryContractGetResponse)
	if err := c.cc.Invoke(ctx, "/l1.contracts.v1.Query/ContractGet", in, out, opts...); err != nil {
		return nil, err
	}
	return out, nil
}
