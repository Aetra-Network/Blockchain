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

type QueryContractRentRequest struct {
	ContractAddress string `protobuf:"bytes,1,opt,name=contract_address,json=contractAddress,proto3" json:"contract_address,omitempty"`
}

type QueryContractRentResponse struct {
	Contract ContractRentRecord `protobuf:"bytes,1,opt,name=contract,proto3" json:"contract"`
}

type QueryRentDebtRequest struct {
	ContractAddress string `protobuf:"bytes,1,opt,name=contract_address,json=contractAddress,proto3" json:"contract_address,omitempty"`
}

type QueryRentDebtResponse struct {
	RentDebt uint64 `protobuf:"varint,1,opt,name=rent_debt,json=rentDebt,proto3" json:"rent_debt,omitempty"`
}

type QueryFrozenContractsRequest struct{}

type QueryFrozenContractsResponse struct {
	Contracts []ContractRentRecord `protobuf:"bytes,1,rep,name=contracts,proto3" json:"contracts"`
}

type QueryDeletionQueueRequest struct{}

type QueryDeletionQueueResponse struct {
	Contracts []ContractRentRecord `protobuf:"bytes,1,rep,name=contracts,proto3" json:"contracts"`
}

type QueryStorageRentParamsRequest struct{}

type QueryStorageRentParamsResponse struct {
	Params StorageRentParams `protobuf:"bytes,1,opt,name=params,proto3" json:"params"`
}

type QueryServer interface {
	ContractRent(context.Context, *QueryContractRentRequest) (*QueryContractRentResponse, error)
	RentDebt(context.Context, *QueryRentDebtRequest) (*QueryRentDebtResponse, error)
	FrozenContracts(context.Context, *QueryFrozenContractsRequest) (*QueryFrozenContractsResponse, error)
	DeletionQueue(context.Context, *QueryDeletionQueueRequest) (*QueryDeletionQueueResponse, error)
	StorageRentParams(context.Context, *QueryStorageRentParamsRequest) (*QueryStorageRentParamsResponse, error)
}

type UnimplementedQueryServer struct{}

func (UnimplementedQueryServer) ContractRent(context.Context, *QueryContractRentRequest) (*QueryContractRentResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ContractRent not implemented")
}
func (UnimplementedQueryServer) RentDebt(context.Context, *QueryRentDebtRequest) (*QueryRentDebtResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method RentDebt not implemented")
}
func (UnimplementedQueryServer) FrozenContracts(context.Context, *QueryFrozenContractsRequest) (*QueryFrozenContractsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method FrozenContracts not implemented")
}
func (UnimplementedQueryServer) DeletionQueue(context.Context, *QueryDeletionQueueRequest) (*QueryDeletionQueueResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DeletionQueue not implemented")
}
func (UnimplementedQueryServer) StorageRentParams(context.Context, *QueryStorageRentParamsRequest) (*QueryStorageRentParamsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method StorageRentParams not implemented")
}

func RegisterQueryServer(s grpc.Server, srv QueryServer) {
	s.RegisterService(&Query_serviceDesc, srv)
}

var Query_serviceDesc = grpcgo.ServiceDesc{
	ServiceName: "l1.storagerent.v1.Query",
	HandlerType: (*QueryServer)(nil),
	Methods: []grpcgo.MethodDesc{
		{MethodName: "ContractRent", Handler: _Query_ContractRent_Handler},
		{MethodName: "RentDebt", Handler: _Query_RentDebt_Handler},
		{MethodName: "FrozenContracts", Handler: _Query_FrozenContracts_Handler},
		{MethodName: "DeletionQueue", Handler: _Query_DeletionQueue_Handler},
		{MethodName: "StorageRentParams", Handler: _Query_StorageRentParams_Handler},
	},
	Streams:  []grpcgo.StreamDesc{},
	Metadata: "l1/storagerent/v1/query.proto",
}

func _Query_ContractRent_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	return queryHandler(ctx, srv, dec, interceptor, "ContractRent", new(QueryContractRentRequest), func(ctx context.Context, srv QueryServer, req interface{}) (interface{}, error) {
		return srv.ContractRent(ctx, req.(*QueryContractRentRequest))
	})
}

func _Query_RentDebt_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	return queryHandler(ctx, srv, dec, interceptor, "RentDebt", new(QueryRentDebtRequest), func(ctx context.Context, srv QueryServer, req interface{}) (interface{}, error) {
		return srv.RentDebt(ctx, req.(*QueryRentDebtRequest))
	})
}

func _Query_FrozenContracts_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	return queryHandler(ctx, srv, dec, interceptor, "FrozenContracts", new(QueryFrozenContractsRequest), func(ctx context.Context, srv QueryServer, req interface{}) (interface{}, error) {
		return srv.FrozenContracts(ctx, req.(*QueryFrozenContractsRequest))
	})
}

func _Query_DeletionQueue_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	return queryHandler(ctx, srv, dec, interceptor, "DeletionQueue", new(QueryDeletionQueueRequest), func(ctx context.Context, srv QueryServer, req interface{}) (interface{}, error) {
		return srv.DeletionQueue(ctx, req.(*QueryDeletionQueueRequest))
	})
}

func _Query_StorageRentParams_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	return queryHandler(ctx, srv, dec, interceptor, "StorageRentParams", new(QueryStorageRentParamsRequest), func(ctx context.Context, srv QueryServer, req interface{}) (interface{}, error) {
		return srv.StorageRentParams(ctx, req.(*QueryStorageRentParamsRequest))
	})
}

func queryHandler(ctx context.Context, srv interface{}, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor, method string, req interface{}, call func(context.Context, QueryServer, interface{}) (interface{}, error)) (interface{}, error) {
	if err := dec(req); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return call(ctx, srv.(QueryServer), req)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.storagerent.v1.Query/" + method}
	handler := func(ctx context.Context, request interface{}) (interface{}, error) {
		return call(ctx, srv.(QueryServer), request)
	}
	return interceptor(ctx, req, info, handler)
}

func (m *QueryContractRentRequest) Reset()          { *m = QueryContractRentRequest{} }
func (m *QueryContractRentRequest) String() string   { return gogoproto.CompactTextString(m) }
func (*QueryContractRentRequest) ProtoMessage()       {}

func (m *QueryContractRentResponse) Reset()          { *m = QueryContractRentResponse{} }
func (m *QueryContractRentResponse) String() string   { return gogoproto.CompactTextString(m) }
func (*QueryContractRentResponse) ProtoMessage()      {}

func (m *QueryRentDebtRequest) Reset()          { *m = QueryRentDebtRequest{} }
func (m *QueryRentDebtRequest) String() string   { return gogoproto.CompactTextString(m) }
func (*QueryRentDebtRequest) ProtoMessage()       {}

func (m *QueryRentDebtResponse) Reset()          { *m = QueryRentDebtResponse{} }
func (m *QueryRentDebtResponse) String() string   { return gogoproto.CompactTextString(m) }
func (*QueryRentDebtResponse) ProtoMessage()       {}

func (m *QueryFrozenContractsRequest) Reset()          { *m = QueryFrozenContractsRequest{} }
func (m *QueryFrozenContractsRequest) String() string   { return gogoproto.CompactTextString(m) }
func (*QueryFrozenContractsRequest) ProtoMessage()       {}

func (m *QueryFrozenContractsResponse) Reset()          { *m = QueryFrozenContractsResponse{} }
func (m *QueryFrozenContractsResponse) String() string   { return gogoproto.CompactTextString(m) }
func (*QueryFrozenContractsResponse) ProtoMessage()      {}

func (m *QueryDeletionQueueRequest) Reset()          { *m = QueryDeletionQueueRequest{} }
func (m *QueryDeletionQueueRequest) String() string   { return gogoproto.CompactTextString(m) }
func (*QueryDeletionQueueRequest) ProtoMessage()       {}

func (m *QueryDeletionQueueResponse) Reset()          { *m = QueryDeletionQueueResponse{} }
func (m *QueryDeletionQueueResponse) String() string   { return gogoproto.CompactTextString(m) }
func (*QueryDeletionQueueResponse) ProtoMessage()       {}

func (m *QueryStorageRentParamsRequest) Reset()          { *m = QueryStorageRentParamsRequest{} }
func (m *QueryStorageRentParamsRequest) String() string   { return gogoproto.CompactTextString(m) }
func (*QueryStorageRentParamsRequest) ProtoMessage()       {}

func (m *QueryStorageRentParamsResponse) Reset()          { *m = QueryStorageRentParamsResponse{} }
func (m *QueryStorageRentParamsResponse) String() string   { return gogoproto.CompactTextString(m) }
func (*QueryStorageRentParamsResponse) ProtoMessage()       {}

func init() {
	registerQueryTypes()
	gogoproto.RegisterFile("l1/storagerent/v1/query.proto", buildStorageRentQueryFileDescriptor())
}

func registerQueryTypes() {
	gogoproto.RegisterType((*QueryContractRentRequest)(nil), "l1.storagerent.v1.QueryContractRentRequest")
	gogoproto.RegisterType((*QueryContractRentResponse)(nil), "l1.storagerent.v1.QueryContractRentResponse")
	gogoproto.RegisterType((*QueryRentDebtRequest)(nil), "l1.storagerent.v1.QueryRentDebtRequest")
	gogoproto.RegisterType((*QueryRentDebtResponse)(nil), "l1.storagerent.v1.QueryRentDebtResponse")
	gogoproto.RegisterType((*QueryFrozenContractsRequest)(nil), "l1.storagerent.v1.QueryFrozenContractsRequest")
	gogoproto.RegisterType((*QueryFrozenContractsResponse)(nil), "l1.storagerent.v1.QueryFrozenContractsResponse")
	gogoproto.RegisterType((*QueryDeletionQueueRequest)(nil), "l1.storagerent.v1.QueryDeletionQueueRequest")
	gogoproto.RegisterType((*QueryDeletionQueueResponse)(nil), "l1.storagerent.v1.QueryDeletionQueueResponse")
	gogoproto.RegisterType((*QueryStorageRentParamsRequest)(nil), "l1.storagerent.v1.QueryStorageRentParamsRequest")
	gogoproto.RegisterType((*QueryStorageRentParamsResponse)(nil), "l1.storagerent.v1.QueryStorageRentParamsResponse")
}

func buildStorageRentQueryFileDescriptor() []byte {
	queryMessageNames := []string{
		"QueryContractRentRequest",
		"QueryContractRentResponse",
		"QueryRentDebtRequest",
		"QueryRentDebtResponse",
		"QueryFrozenContractsRequest",
		"QueryFrozenContractsResponse",
		"QueryDeletionQueueRequest",
		"QueryDeletionQueueResponse",
		"QueryStorageRentParamsRequest",
		"QueryStorageRentParamsResponse",
	}
	messages := make([]*descriptorpb.DescriptorProto, 0, len(queryMessageNames))
	for _, name := range queryMessageNames {
		messages = append(messages, &descriptorpb.DescriptorProto{Name: descriptorString(name)})
	}
	methods := []*descriptorpb.MethodDescriptorProto{
		queryDescriptorMethod("ContractRent", "QueryContractRentRequest", "QueryContractRentResponse"),
		queryDescriptorMethod("RentDebt", "QueryRentDebtRequest", "QueryRentDebtResponse"),
		queryDescriptorMethod("FrozenContracts", "QueryFrozenContractsRequest", "QueryFrozenContractsResponse"),
		queryDescriptorMethod("DeletionQueue", "QueryDeletionQueueRequest", "QueryDeletionQueueResponse"),
		queryDescriptorMethod("StorageRentParams", "QueryStorageRentParamsRequest", "QueryStorageRentParamsResponse"),
	}
	fd := &descriptorpb.FileDescriptorProto{
		Name:       descriptorString("l1/storagerent/v1/query.proto"),
		Package:    descriptorString("l1.storagerent.v1"),
		Syntax:     descriptorString("proto3"),
		MessageType: messages,
		Service:    []*descriptorpb.ServiceDescriptorProto{{Name: descriptorString("Query"), Method: methods}},
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

func queryDescriptorMethod(name, input, output string) *descriptorpb.MethodDescriptorProto {
	return &descriptorpb.MethodDescriptorProto{
		Name:       descriptorString(name),
		InputType:  descriptorString(".l1.storagerent.v1." + input),
		OutputType: descriptorString(".l1.storagerent.v1." + output),
	}
}