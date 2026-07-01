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

const (
	MsgStoreCodeTypeURL                 = "/l1.contracts.v1.MsgStoreCode"
	MsgDeployContractTypeURL            = "/l1.contracts.v1.MsgDeployContract"
	MsgExecuteExternalTypeURL           = "/l1.contracts.v1.MsgExecuteExternal"
	MsgExecuteInternalTypeURL           = "/l1.contracts.v1.MsgExecuteInternal"
	MsgSendInternalMessageTypeURL       = "/l1.contracts.v1.MsgSendInternalMessage"
	MsgUpdateContractParamsTypeURL      = "/l1.contracts.v1.MsgUpdateContractParams"
	MsgSubmitSecurityAttestationTypeURL = "/l1.contracts.v1.MsgSubmitSecurityAttestation"
	MsgRevokeSecurityAttestationTypeURL = "/l1.contracts.v1.MsgRevokeSecurityAttestation"
)

type GRPCMsgServer interface {
	StoreCode(context.Context, *MsgStoreCode) (*StoreCodeResponse, error)
	DeployContract(context.Context, *MsgDeployContract) (*InstantiateContractResponse, error)
	ExecuteExternal(context.Context, *MsgExecuteExternal) (*ExecuteContractResponse, error)
	ExecuteInternal(context.Context, *MsgExecuteInternal) (*InternalMessage, error)
	SendInternalMessage(context.Context, *MsgSendInternalMessage) (*InternalMessage, error)
	UpdateContractParams(context.Context, *MsgUpdateContractParams) (*MsgUpdateContractParamsResponse, error)
	SubmitSecurityAttestation(context.Context, *MsgSubmitSecurityAttestation) (*MsgSubmitSecurityAttestationResponse, error)
	RevokeSecurityAttestation(context.Context, *MsgRevokeSecurityAttestation) (*MsgRevokeSecurityAttestationResponse, error)
}

type UnimplementedGRPCMsgServer struct{}

func (UnimplementedGRPCMsgServer) StoreCode(context.Context, *MsgStoreCode) (*StoreCodeResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method StoreCode not implemented")
}
func (UnimplementedGRPCMsgServer) DeployContract(context.Context, *MsgDeployContract) (*InstantiateContractResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DeployContract not implemented")
}
func (UnimplementedGRPCMsgServer) ExecuteExternal(context.Context, *MsgExecuteExternal) (*ExecuteContractResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ExecuteExternal not implemented")
}
func (UnimplementedGRPCMsgServer) ExecuteInternal(context.Context, *MsgExecuteInternal) (*InternalMessage, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ExecuteInternal not implemented")
}
func (UnimplementedGRPCMsgServer) SendInternalMessage(context.Context, *MsgSendInternalMessage) (*InternalMessage, error) {
	return nil, status.Errorf(codes.Unimplemented, "method SendInternalMessage not implemented")
}
func (UnimplementedGRPCMsgServer) UpdateContractParams(context.Context, *MsgUpdateContractParams) (*MsgUpdateContractParamsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method UpdateContractParams not implemented")
}
func (UnimplementedGRPCMsgServer) SubmitSecurityAttestation(context.Context, *MsgSubmitSecurityAttestation) (*MsgSubmitSecurityAttestationResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method SubmitSecurityAttestation not implemented")
}
func (UnimplementedGRPCMsgServer) RevokeSecurityAttestation(context.Context, *MsgRevokeSecurityAttestation) (*MsgRevokeSecurityAttestationResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method RevokeSecurityAttestation not implemented")
}

type GRPCQueryServer interface {
	Params(context.Context, *QueryParamsRequest) (*QueryParamsResponse, error)
	Code(context.Context, *QueryCodeRequest) (*QueryCodeResponse, error)
	Codes(context.Context, *QueryCodesRequest) (*QueryCodesResponse, error)
	Contract(context.Context, *QueryContractRequest) (*QueryContractResponse, error)
	Contracts(context.Context, *QueryContractsRequest) (*QueryContractsResponse, error)
	ContractStorage(context.Context, *QueryContractStorageRequest) (*QueryContractStorageResponse, error)
	ContractReceipts(context.Context, *QueryContractReceiptsRequest) (*QueryContractReceiptsResponse, error)
	ContractQueue(context.Context, *QueryContractQueueRequest) (*QueryContractQueueResponse, error)
	ContractEvents(context.Context, *QueryContractEventsRequest) (*QueryContractEventsResponse, error)
	ContractStateRoot(context.Context, *QueryContractStateRootRequest) (*QueryContractStateRootResponse, error)
	SecurityAttestations(context.Context, *QuerySecurityAttestationsRequest) (*QuerySecurityAttestationsResponse, error)
	SecurityBadge(context.Context, *QuerySecurityBadgeRequest) (*QuerySecurityBadgeResponse, error)
}

type UnimplementedGRPCQueryServer struct{}

func (UnimplementedGRPCQueryServer) Params(context.Context, *QueryParamsRequest) (*QueryParamsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Params not implemented")
}
func (UnimplementedGRPCQueryServer) Code(context.Context, *QueryCodeRequest) (*QueryCodeResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Code not implemented")
}
func (UnimplementedGRPCQueryServer) Codes(context.Context, *QueryCodesRequest) (*QueryCodesResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Codes not implemented")
}
func (UnimplementedGRPCQueryServer) Contract(context.Context, *QueryContractRequest) (*QueryContractResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Contract not implemented")
}
func (UnimplementedGRPCQueryServer) Contracts(context.Context, *QueryContractsRequest) (*QueryContractsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Contracts not implemented")
}
func (UnimplementedGRPCQueryServer) ContractStorage(context.Context, *QueryContractStorageRequest) (*QueryContractStorageResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ContractStorage not implemented")
}
func (UnimplementedGRPCQueryServer) ContractReceipts(context.Context, *QueryContractReceiptsRequest) (*QueryContractReceiptsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ContractReceipts not implemented")
}
func (UnimplementedGRPCQueryServer) ContractQueue(context.Context, *QueryContractQueueRequest) (*QueryContractQueueResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ContractQueue not implemented")
}
func (UnimplementedGRPCQueryServer) ContractEvents(context.Context, *QueryContractEventsRequest) (*QueryContractEventsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ContractEvents not implemented")
}
func (UnimplementedGRPCQueryServer) ContractStateRoot(context.Context, *QueryContractStateRootRequest) (*QueryContractStateRootResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ContractStateRoot not implemented")
}
func (UnimplementedGRPCQueryServer) SecurityAttestations(context.Context, *QuerySecurityAttestationsRequest) (*QuerySecurityAttestationsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method SecurityAttestations not implemented")
}
func (UnimplementedGRPCQueryServer) SecurityBadge(context.Context, *QuerySecurityBadgeRequest) (*QuerySecurityBadgeResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method SecurityBadge not implemented")
}

func RegisterMsgServer(s grpc.Server, srv GRPCMsgServer) {
	s.RegisterService(&Msg_serviceDesc, srv)
}

func RegisterQueryServer(s grpc.Server, srv GRPCQueryServer) {
	s.RegisterService(&Query_serviceDesc, srv)
}

var Msg_serviceDesc = grpcgo.ServiceDesc{
	ServiceName: "l1.contracts.v1.Msg",
	HandlerType: (*GRPCMsgServer)(nil),
	Methods: []grpcgo.MethodDesc{
		{MethodName: "StoreCode", Handler: msgHandler(func(s GRPCMsgServer, ctx context.Context, req any) (any, error) {
			return s.StoreCode(ctx, req.(*MsgStoreCode))
		}, newMsgStoreCode)},
		{MethodName: "DeployContract", Handler: msgHandler(func(s GRPCMsgServer, ctx context.Context, req any) (any, error) {
			return s.DeployContract(ctx, req.(*MsgDeployContract))
		}, newMsgDeployContract)},
		{MethodName: "ExecuteExternal", Handler: msgHandler(func(s GRPCMsgServer, ctx context.Context, req any) (any, error) {
			return s.ExecuteExternal(ctx, req.(*MsgExecuteExternal))
		}, newMsgExecuteExternal)},
		{MethodName: "ExecuteInternal", Handler: msgHandler(func(s GRPCMsgServer, ctx context.Context, req any) (any, error) {
			return s.ExecuteInternal(ctx, req.(*MsgExecuteInternal))
		}, newMsgExecuteInternal)},
		{MethodName: "SendInternalMessage", Handler: msgHandler(func(s GRPCMsgServer, ctx context.Context, req any) (any, error) {
			return s.SendInternalMessage(ctx, req.(*MsgSendInternalMessage))
		}, newMsgSendInternalMessage)},
		{MethodName: "UpdateContractParams", Handler: msgHandler(func(s GRPCMsgServer, ctx context.Context, req any) (any, error) {
			return s.UpdateContractParams(ctx, req.(*MsgUpdateContractParams))
		}, newMsgUpdateContractParams)},
		{MethodName: "SubmitSecurityAttestation", Handler: msgHandler(func(s GRPCMsgServer, ctx context.Context, req any) (any, error) {
			return s.SubmitSecurityAttestation(ctx, req.(*MsgSubmitSecurityAttestation))
		}, newMsgSubmitSecurityAttestation)},
		{MethodName: "RevokeSecurityAttestation", Handler: msgHandler(func(s GRPCMsgServer, ctx context.Context, req any) (any, error) {
			return s.RevokeSecurityAttestation(ctx, req.(*MsgRevokeSecurityAttestation))
		}, newMsgRevokeSecurityAttestation)},
	},
	Streams:  []grpcgo.StreamDesc{},
	Metadata: "l1/contracts/v1/tx.proto",
}

var Query_serviceDesc = grpcgo.ServiceDesc{
	ServiceName: "l1.contracts.v1.Query",
	HandlerType: (*GRPCQueryServer)(nil),
	Methods: []grpcgo.MethodDesc{
		{MethodName: "Params", Handler: queryHandler(func(s GRPCQueryServer, ctx context.Context, req any) (any, error) {
			return s.Params(ctx, req.(*QueryParamsRequest))
		}, newQueryParamsRequest)},
		{MethodName: "Code", Handler: queryHandler(func(s GRPCQueryServer, ctx context.Context, req any) (any, error) {
			return s.Code(ctx, req.(*QueryCodeRequest))
		}, newQueryCodeRequest)},
		{MethodName: "Codes", Handler: queryHandler(func(s GRPCQueryServer, ctx context.Context, req any) (any, error) {
			return s.Codes(ctx, req.(*QueryCodesRequest))
		}, newQueryCodesRequest)},
		{MethodName: "Contract", Handler: queryHandler(func(s GRPCQueryServer, ctx context.Context, req any) (any, error) {
			return s.Contract(ctx, req.(*QueryContractRequest))
		}, newQueryContractRequest)},
		{MethodName: "Contracts", Handler: queryHandler(func(s GRPCQueryServer, ctx context.Context, req any) (any, error) {
			return s.Contracts(ctx, req.(*QueryContractsRequest))
		}, newQueryContractsRequest)},
		{MethodName: "ContractStorage", Handler: queryHandler(func(s GRPCQueryServer, ctx context.Context, req any) (any, error) {
			return s.ContractStorage(ctx, req.(*QueryContractStorageRequest))
		}, newQueryContractStorageRequest)},
		{MethodName: "ContractReceipts", Handler: queryHandler(func(s GRPCQueryServer, ctx context.Context, req any) (any, error) {
			return s.ContractReceipts(ctx, req.(*QueryContractReceiptsRequest))
		}, newQueryContractReceiptsRequest)},
		{MethodName: "ContractQueue", Handler: queryHandler(func(s GRPCQueryServer, ctx context.Context, req any) (any, error) {
			return s.ContractQueue(ctx, req.(*QueryContractQueueRequest))
		}, newQueryContractQueueRequest)},
		{MethodName: "ContractEvents", Handler: queryHandler(func(s GRPCQueryServer, ctx context.Context, req any) (any, error) {
			return s.ContractEvents(ctx, req.(*QueryContractEventsRequest))
		}, newQueryContractEventsRequest)},
		{MethodName: "ContractStateRoot", Handler: queryHandler(func(s GRPCQueryServer, ctx context.Context, req any) (any, error) {
			return s.ContractStateRoot(ctx, req.(*QueryContractStateRootRequest))
		}, newQueryContractStateRootRequest)},
		{MethodName: "SecurityAttestations", Handler: queryHandler(func(s GRPCQueryServer, ctx context.Context, req any) (any, error) {
			return s.SecurityAttestations(ctx, req.(*QuerySecurityAttestationsRequest))
		}, newQuerySecurityAttestationsRequest)},
		{MethodName: "SecurityBadge", Handler: queryHandler(func(s GRPCQueryServer, ctx context.Context, req any) (any, error) {
			return s.SecurityBadge(ctx, req.(*QuerySecurityBadgeRequest))
		}, newQuerySecurityBadgeRequest)},
	},
	Streams:  []grpcgo.StreamDesc{},
	Metadata: "l1/contracts/v1/query.proto",
}

type msgInvoker func(GRPCMsgServer, context.Context, any) (any, error)
type queryInvoker func(GRPCQueryServer, context.Context, any) (any, error)
type requestFactory func() any

func msgHandler(invoke msgInvoker, factory requestFactory) grpcgo.MethodHandler {
	return func(srv any, ctx context.Context, dec func(any) error, interceptor grpcgo.UnaryServerInterceptor) (any, error) {
		req := factory()
		if err := dec(req); err != nil {
			return nil, err
		}
		if interceptor == nil {
			return invoke(srv.(GRPCMsgServer), ctx, req)
		}
		info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.contracts.v1.Msg"}
		return interceptor(ctx, req, info, func(ctx context.Context, req any) (any, error) {
			return invoke(srv.(GRPCMsgServer), ctx, req)
		})
	}
}

func queryHandler(invoke queryInvoker, factory requestFactory) grpcgo.MethodHandler {
	return func(srv any, ctx context.Context, dec func(any) error, interceptor grpcgo.UnaryServerInterceptor) (any, error) {
		req := factory()
		if err := dec(req); err != nil {
			return nil, err
		}
		if interceptor == nil {
			return invoke(srv.(GRPCQueryServer), ctx, req)
		}
		info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.contracts.v1.Query"}
		return interceptor(ctx, req, info, func(ctx context.Context, req any) (any, error) {
			return invoke(srv.(GRPCQueryServer), ctx, req)
		})
	}
}

func newMsgStoreCode() any                 { return new(MsgStoreCode) }
func newMsgDeployContract() any            { return new(MsgDeployContract) }
func newMsgExecuteExternal() any           { return new(MsgExecuteExternal) }
func newMsgExecuteInternal() any           { return new(MsgExecuteInternal) }
func newMsgSendInternalMessage() any       { return new(MsgSendInternalMessage) }
func newMsgUpdateContractParams() any      { return new(MsgUpdateContractParams) }
func newMsgSubmitSecurityAttestation() any { return new(MsgSubmitSecurityAttestation) }
func newMsgRevokeSecurityAttestation() any { return new(MsgRevokeSecurityAttestation) }
func newQueryParamsRequest() any           { return new(QueryParamsRequest) }
func newQueryCodeRequest() any             { return new(QueryCodeRequest) }
func newQueryCodesRequest() any            { return new(QueryCodesRequest) }
func newQueryContractRequest() any         { return new(QueryContractRequest) }
func newQueryContractsRequest() any        { return new(QueryContractsRequest) }
func newQueryContractStorageRequest() any  { return new(QueryContractStorageRequest) }
func newQueryContractReceiptsRequest() any { return new(QueryContractReceiptsRequest) }
func newQueryContractQueueRequest() any    { return new(QueryContractQueueRequest) }
func newQueryContractEventsRequest() any   { return new(QueryContractEventsRequest) }
func newQueryContractStateRootRequest() any {
	return new(QueryContractStateRootRequest)
}
func newQuerySecurityAttestationsRequest() any { return new(QuerySecurityAttestationsRequest) }
func newQuerySecurityBadgeRequest() any        { return new(QuerySecurityBadgeRequest) }

func init() {
	gogoproto.RegisterType((*MsgStoreCode)(nil), "l1.contracts.v1.MsgStoreCode")
	gogoproto.RegisterType((*MsgDeployContract)(nil), "l1.contracts.v1.MsgDeployContract")
	gogoproto.RegisterType((*MsgExecuteExternal)(nil), "l1.contracts.v1.MsgExecuteExternal")
	gogoproto.RegisterType((*MsgExecuteInternal)(nil), "l1.contracts.v1.MsgExecuteInternal")
	gogoproto.RegisterType((*MsgSendInternalMessage)(nil), "l1.contracts.v1.MsgSendInternalMessage")
	gogoproto.RegisterType((*MsgUpdateContractParams)(nil), "l1.contracts.v1.MsgUpdateContractParams")
	gogoproto.RegisterType((*MsgSubmitSecurityAttestation)(nil), "l1.contracts.v1.MsgSubmitSecurityAttestation")
	gogoproto.RegisterType((*MsgRevokeSecurityAttestation)(nil), "l1.contracts.v1.MsgRevokeSecurityAttestation")
	gogoproto.RegisterType((*StoreCodeResponse)(nil), "l1.contracts.v1.MsgStoreCodeResponse")
	gogoproto.RegisterType((*InstantiateContractResponse)(nil), "l1.contracts.v1.MsgDeployContractResponse")
	gogoproto.RegisterType((*ExecuteContractResponse)(nil), "l1.contracts.v1.MsgExecuteExternalResponse")
	gogoproto.RegisterType((*InternalMessage)(nil), "l1.contracts.v1.InternalMessage")
	gogoproto.RegisterType((*MsgUpdateContractParamsResponse)(nil), "l1.contracts.v1.MsgUpdateContractParamsResponse")
	gogoproto.RegisterType((*MsgSubmitSecurityAttestationResponse)(nil), "l1.contracts.v1.MsgSubmitSecurityAttestationResponse")
	gogoproto.RegisterType((*MsgRevokeSecurityAttestationResponse)(nil), "l1.contracts.v1.MsgRevokeSecurityAttestationResponse")
	gogoproto.RegisterType((*QueryParamsRequest)(nil), "l1.contracts.v1.QueryParamsRequest")
	gogoproto.RegisterType((*QueryParamsResponse)(nil), "l1.contracts.v1.QueryParamsResponse")
	gogoproto.RegisterType((*QueryCodeRequest)(nil), "l1.contracts.v1.QueryCodeRequest")
	gogoproto.RegisterType((*QueryCodeResponse)(nil), "l1.contracts.v1.QueryCodeResponse")
	gogoproto.RegisterType((*QueryCodesRequest)(nil), "l1.contracts.v1.QueryCodesRequest")
	gogoproto.RegisterType((*QueryCodesResponse)(nil), "l1.contracts.v1.QueryCodesResponse")
	gogoproto.RegisterType((*QueryContractRequest)(nil), "l1.contracts.v1.QueryContractRequest")
	gogoproto.RegisterType((*QueryContractResponse)(nil), "l1.contracts.v1.QueryContractResponse")
	gogoproto.RegisterType((*QueryContractsRequest)(nil), "l1.contracts.v1.QueryContractsRequest")
	gogoproto.RegisterType((*QueryContractsResponse)(nil), "l1.contracts.v1.QueryContractsResponse")
	gogoproto.RegisterType((*QueryContractStorageRequest)(nil), "l1.contracts.v1.QueryContractStorageRequest")
	gogoproto.RegisterType((*QueryContractStorageResponse)(nil), "l1.contracts.v1.QueryContractStorageResponse")
	gogoproto.RegisterType((*QueryContractReceiptsRequest)(nil), "l1.contracts.v1.QueryContractReceiptsRequest")
	gogoproto.RegisterType((*QueryContractReceiptsResponse)(nil), "l1.contracts.v1.QueryContractReceiptsResponse")
	gogoproto.RegisterType((*QueryContractQueueRequest)(nil), "l1.contracts.v1.QueryContractQueueRequest")
	gogoproto.RegisterType((*QueryContractQueueResponse)(nil), "l1.contracts.v1.QueryContractQueueResponse")
	gogoproto.RegisterType((*QueryContractEventsRequest)(nil), "l1.contracts.v1.QueryContractEventsRequest")
	gogoproto.RegisterType((*QueryContractEventsResponse)(nil), "l1.contracts.v1.QueryContractEventsResponse")
	gogoproto.RegisterType((*QueryContractStateRootRequest)(nil), "l1.contracts.v1.QueryContractStateRootRequest")
	gogoproto.RegisterType((*QueryContractStateRootResponse)(nil), "l1.contracts.v1.QueryContractStateRootResponse")
	gogoproto.RegisterType((*QuerySecurityAttestationsRequest)(nil), "l1.contracts.v1.QuerySecurityAttestationsRequest")
	gogoproto.RegisterType((*QuerySecurityAttestationsResponse)(nil), "l1.contracts.v1.QuerySecurityAttestationsResponse")
	gogoproto.RegisterType((*QuerySecurityBadgeRequest)(nil), "l1.contracts.v1.QuerySecurityBadgeRequest")
	gogoproto.RegisterType((*QuerySecurityBadgeResponse)(nil), "l1.contracts.v1.QuerySecurityBadgeResponse")
	gogoproto.RegisterFile("l1/contracts/v1/tx.proto", fileDescriptorContractsTx)
	gogoproto.RegisterFile("l1/contracts/v1/query.proto", fileDescriptorContractsQuery)
}

func (m *MsgStoreCode) Reset()         { *m = MsgStoreCode{} }
func (m *MsgStoreCode) String() string { return gogoproto.CompactTextString(m) }
func (*MsgStoreCode) ProtoMessage()    {}
func (*MsgStoreCode) Descriptor() ([]byte, []int) {
	return fileDescriptorContractsTx, []int{0}
}

func (m *MsgDeployContract) Reset()         { *m = MsgDeployContract{} }
func (m *MsgDeployContract) String() string { return gogoproto.CompactTextString(m) }
func (*MsgDeployContract) ProtoMessage()    {}
func (*MsgDeployContract) Descriptor() ([]byte, []int) {
	return fileDescriptorContractsTx, []int{2}
}

func (m *MsgExecuteExternal) Reset()         { *m = MsgExecuteExternal{} }
func (m *MsgExecuteExternal) String() string { return gogoproto.CompactTextString(m) }
func (*MsgExecuteExternal) ProtoMessage()    {}
func (*MsgExecuteExternal) Descriptor() ([]byte, []int) {
	return fileDescriptorContractsTx, []int{4}
}

func (m *MsgExecuteInternal) Reset()         { *m = MsgExecuteInternal{} }
func (m *MsgExecuteInternal) String() string { return gogoproto.CompactTextString(m) }
func (*MsgExecuteInternal) ProtoMessage()    {}
func (*MsgExecuteInternal) Descriptor() ([]byte, []int) {
	return fileDescriptorContractsTx, []int{6}
}

func (m *MsgSendInternalMessage) Reset()         { *m = MsgSendInternalMessage{} }
func (m *MsgSendInternalMessage) String() string { return gogoproto.CompactTextString(m) }
func (*MsgSendInternalMessage) ProtoMessage()    {}
func (*MsgSendInternalMessage) Descriptor() ([]byte, []int) {
	return fileDescriptorContractsTx, []int{8}
}

func (m *MsgUpdateContractParams) Reset()         { *m = MsgUpdateContractParams{} }
func (m *MsgUpdateContractParams) String() string { return gogoproto.CompactTextString(m) }
func (*MsgUpdateContractParams) ProtoMessage()    {}
func (*MsgUpdateContractParams) Descriptor() ([]byte, []int) {
	return fileDescriptorContractsTx, []int{9}
}

func (m *StoreCodeResponse) Reset()         { *m = StoreCodeResponse{} }
func (m *StoreCodeResponse) String() string { return gogoproto.CompactTextString(m) }
func (*StoreCodeResponse) ProtoMessage()    {}

func (m *InstantiateContractResponse) Reset()         { *m = InstantiateContractResponse{} }
func (m *InstantiateContractResponse) String() string { return gogoproto.CompactTextString(m) }
func (*InstantiateContractResponse) ProtoMessage()    {}

func (m *ExecuteContractResponse) Reset()         { *m = ExecuteContractResponse{} }
func (m *ExecuteContractResponse) String() string { return gogoproto.CompactTextString(m) }
func (*ExecuteContractResponse) ProtoMessage()    {}

func (m *InternalMessage) Reset()         { *m = InternalMessage{} }
func (m *InternalMessage) String() string { return gogoproto.CompactTextString(m) }
func (*InternalMessage) ProtoMessage()    {}
func (*InternalMessage) Descriptor() ([]byte, []int) {
	return fileDescriptorContractsTx, []int{7}
}

func (m *MsgUpdateContractParamsResponse) Reset()         { *m = MsgUpdateContractParamsResponse{} }
func (m *MsgUpdateContractParamsResponse) String() string { return gogoproto.CompactTextString(m) }
func (*MsgUpdateContractParamsResponse) ProtoMessage()    {}

func (m *QueryParamsRequest) Reset()         { *m = QueryParamsRequest{} }
func (m *QueryParamsRequest) String() string { return gogoproto.CompactTextString(m) }
func (*QueryParamsRequest) ProtoMessage()    {}
func (*QueryParamsRequest) Descriptor() ([]byte, []int) {
	return fileDescriptorContractsQuery, []int{0}
}

func (m *QueryParamsResponse) Reset()         { *m = QueryParamsResponse{} }
func (m *QueryParamsResponse) String() string { return gogoproto.CompactTextString(m) }
func (*QueryParamsResponse) ProtoMessage()    {}

func (m *QueryCodeRequest) Reset()         { *m = QueryCodeRequest{} }
func (m *QueryCodeRequest) String() string { return gogoproto.CompactTextString(m) }
func (*QueryCodeRequest) ProtoMessage()    {}
func (*QueryCodeRequest) Descriptor() ([]byte, []int) {
	return fileDescriptorContractsQuery, []int{2}
}

func (m *QueryCodeResponse) Reset()         { *m = QueryCodeResponse{} }
func (m *QueryCodeResponse) String() string { return gogoproto.CompactTextString(m) }
func (*QueryCodeResponse) ProtoMessage()    {}

func (m *QueryCodesRequest) Reset()         { *m = QueryCodesRequest{} }
func (m *QueryCodesRequest) String() string { return gogoproto.CompactTextString(m) }
func (*QueryCodesRequest) ProtoMessage()    {}
func (*QueryCodesRequest) Descriptor() ([]byte, []int) {
	return fileDescriptorContractsQuery, []int{4}
}

func (m *QueryCodesResponse) Reset()         { *m = QueryCodesResponse{} }
func (m *QueryCodesResponse) String() string { return gogoproto.CompactTextString(m) }
func (*QueryCodesResponse) ProtoMessage()    {}

func (m *QueryContractRequest) Reset()         { *m = QueryContractRequest{} }
func (m *QueryContractRequest) String() string { return gogoproto.CompactTextString(m) }
func (*QueryContractRequest) ProtoMessage()    {}
func (*QueryContractRequest) Descriptor() ([]byte, []int) {
	return fileDescriptorContractsQuery, []int{6}
}

func (m *QueryContractResponse) Reset()         { *m = QueryContractResponse{} }
func (m *QueryContractResponse) String() string { return gogoproto.CompactTextString(m) }
func (*QueryContractResponse) ProtoMessage()    {}

func (m *QueryContractsRequest) Reset()         { *m = QueryContractsRequest{} }
func (m *QueryContractsRequest) String() string { return gogoproto.CompactTextString(m) }
func (*QueryContractsRequest) ProtoMessage()    {}
func (*QueryContractsRequest) Descriptor() ([]byte, []int) {
	return fileDescriptorContractsQuery, []int{8}
}

func (m *QueryContractsResponse) Reset()         { *m = QueryContractsResponse{} }
func (m *QueryContractsResponse) String() string { return gogoproto.CompactTextString(m) }
func (*QueryContractsResponse) ProtoMessage()    {}

func (m *QueryContractStorageRequest) Reset()         { *m = QueryContractStorageRequest{} }
func (m *QueryContractStorageRequest) String() string { return gogoproto.CompactTextString(m) }
func (*QueryContractStorageRequest) ProtoMessage()    {}
func (*QueryContractStorageRequest) Descriptor() ([]byte, []int) {
	return fileDescriptorContractsQuery, []int{10}
}

func (m *QueryContractStorageResponse) Reset()         { *m = QueryContractStorageResponse{} }
func (m *QueryContractStorageResponse) String() string { return gogoproto.CompactTextString(m) }
func (*QueryContractStorageResponse) ProtoMessage()    {}

func (m *QueryContractReceiptsRequest) Reset()         { *m = QueryContractReceiptsRequest{} }
func (m *QueryContractReceiptsRequest) String() string { return gogoproto.CompactTextString(m) }
func (*QueryContractReceiptsRequest) ProtoMessage()    {}
func (*QueryContractReceiptsRequest) Descriptor() ([]byte, []int) {
	return fileDescriptorContractsQuery, []int{12}
}

func (m *QueryContractReceiptsResponse) Reset()         { *m = QueryContractReceiptsResponse{} }
func (m *QueryContractReceiptsResponse) String() string { return gogoproto.CompactTextString(m) }
func (*QueryContractReceiptsResponse) ProtoMessage()    {}

func (m *QueryContractQueueRequest) Reset()         { *m = QueryContractQueueRequest{} }
func (m *QueryContractQueueRequest) String() string { return gogoproto.CompactTextString(m) }
func (*QueryContractQueueRequest) ProtoMessage()    {}
func (*QueryContractQueueRequest) Descriptor() ([]byte, []int) {
	return fileDescriptorContractsQuery, []int{14}
}

func (m *QueryContractQueueResponse) Reset()         { *m = QueryContractQueueResponse{} }
func (m *QueryContractQueueResponse) String() string { return gogoproto.CompactTextString(m) }
func (*QueryContractQueueResponse) ProtoMessage()    {}

func (m *QueryContractEventsRequest) Reset()         { *m = QueryContractEventsRequest{} }
func (m *QueryContractEventsRequest) String() string { return gogoproto.CompactTextString(m) }
func (*QueryContractEventsRequest) ProtoMessage()    {}
func (*QueryContractEventsRequest) Descriptor() ([]byte, []int) {
	return fileDescriptorContractsQuery, []int{16}
}

func (m *QueryContractEventsResponse) Reset()         { *m = QueryContractEventsResponse{} }
func (m *QueryContractEventsResponse) String() string { return gogoproto.CompactTextString(m) }
func (*QueryContractEventsResponse) ProtoMessage()    {}

func (m *QueryContractStateRootRequest) Reset()         { *m = QueryContractStateRootRequest{} }
func (m *QueryContractStateRootRequest) String() string { return gogoproto.CompactTextString(m) }
func (*QueryContractStateRootRequest) ProtoMessage()    {}
func (*QueryContractStateRootRequest) Descriptor() ([]byte, []int) {
	return fileDescriptorContractsQuery, []int{18}
}

func (m *QueryContractStateRootResponse) Reset()         { *m = QueryContractStateRootResponse{} }
func (m *QueryContractStateRootResponse) String() string { return gogoproto.CompactTextString(m) }
func (*QueryContractStateRootResponse) ProtoMessage()    {}

func (m *MsgSubmitSecurityAttestation) Reset()         { *m = MsgSubmitSecurityAttestation{} }
func (m *MsgSubmitSecurityAttestation) String() string { return gogoproto.CompactTextString(m) }
func (*MsgSubmitSecurityAttestation) ProtoMessage()    {}
func (*MsgSubmitSecurityAttestation) Descriptor() ([]byte, []int) {
	return fileDescriptorContractsTx, []int{11}
}

func (m *MsgRevokeSecurityAttestation) Reset()         { *m = MsgRevokeSecurityAttestation{} }
func (m *MsgRevokeSecurityAttestation) String() string { return gogoproto.CompactTextString(m) }
func (*MsgRevokeSecurityAttestation) ProtoMessage()    {}
func (*MsgRevokeSecurityAttestation) Descriptor() ([]byte, []int) {
	return fileDescriptorContractsTx, []int{12}
}

func (m *MsgSubmitSecurityAttestationResponse) Reset()         { *m = MsgSubmitSecurityAttestationResponse{} }
func (m *MsgSubmitSecurityAttestationResponse) String() string { return gogoproto.CompactTextString(m) }
func (*MsgSubmitSecurityAttestationResponse) ProtoMessage()    {}

func (m *MsgRevokeSecurityAttestationResponse) Reset()         { *m = MsgRevokeSecurityAttestationResponse{} }
func (m *MsgRevokeSecurityAttestationResponse) String() string { return gogoproto.CompactTextString(m) }
func (*MsgRevokeSecurityAttestationResponse) ProtoMessage()    {}

func (m *QuerySecurityAttestationsRequest) Reset()         { *m = QuerySecurityAttestationsRequest{} }
func (m *QuerySecurityAttestationsRequest) String() string { return gogoproto.CompactTextString(m) }
func (*QuerySecurityAttestationsRequest) ProtoMessage()    {}
func (*QuerySecurityAttestationsRequest) Descriptor() ([]byte, []int) {
	return fileDescriptorContractsQuery, []int{20}
}

func (m *QuerySecurityAttestationsResponse) Reset()         { *m = QuerySecurityAttestationsResponse{} }
func (m *QuerySecurityAttestationsResponse) String() string { return gogoproto.CompactTextString(m) }
func (*QuerySecurityAttestationsResponse) ProtoMessage()    {}

func (m *QuerySecurityBadgeRequest) Reset()         { *m = QuerySecurityBadgeRequest{} }
func (m *QuerySecurityBadgeRequest) String() string { return gogoproto.CompactTextString(m) }
func (*QuerySecurityBadgeRequest) ProtoMessage()    {}
func (*QuerySecurityBadgeRequest) Descriptor() ([]byte, []int) {
	return fileDescriptorContractsQuery, []int{22}
}

func (m *QuerySecurityBadgeResponse) Reset()         { *m = QuerySecurityBadgeResponse{} }
func (m *QuerySecurityBadgeResponse) String() string { return gogoproto.CompactTextString(m) }
func (*QuerySecurityBadgeResponse) ProtoMessage()    {}

var fileDescriptorContractsTx = buildContractsTxFileDescriptor()
var fileDescriptorContractsQuery = buildContractsQueryFileDescriptor()

func buildContractsTxFileDescriptor() []byte {
	fd := &descriptorpb.FileDescriptorProto{
		Name:    descriptorString("l1/contracts/v1/tx.proto"),
		Package: descriptorString("l1.contracts.v1"),
		Syntax:  descriptorString("proto3"),
		Options: &descriptorpb.FileOptions{
			GoPackage: descriptorString("github.com/sovereign-l1/l1/x/contracts/types"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			messageDescriptor("MsgStoreCode"),
			messageDescriptor("MsgStoreCodeResponse"),
			messageDescriptor("MsgDeployContract"),
			messageDescriptor("MsgDeployContractResponse"),
			messageDescriptor("MsgExecuteExternal"),
			messageDescriptor("MsgExecuteExternalResponse"),
			messageDescriptor("MsgExecuteInternal"),
			messageDescriptor("InternalMessage"),
			messageDescriptor("MsgSendInternalMessage"),
			messageDescriptor("MsgUpdateContractParams"),
			messageDescriptor("MsgUpdateContractParamsResponse"),
			messageDescriptor("MsgSubmitSecurityAttestation"),
			messageDescriptor("MsgSubmitSecurityAttestationResponse"),
			messageDescriptor("MsgRevokeSecurityAttestation"),
			messageDescriptor("MsgRevokeSecurityAttestationResponse"),
		},
		Service: []*descriptorpb.ServiceDescriptorProto{
			{
				Name: descriptorString("Msg"),
				Method: []*descriptorpb.MethodDescriptorProto{
					serviceMethodDescriptor("StoreCode", "MsgStoreCode", "MsgStoreCodeResponse"),
					serviceMethodDescriptor("DeployContract", "MsgDeployContract", "MsgDeployContractResponse"),
					serviceMethodDescriptor("ExecuteExternal", "MsgExecuteExternal", "MsgExecuteExternalResponse"),
					serviceMethodDescriptor("ExecuteInternal", "MsgExecuteInternal", "InternalMessage"),
					serviceMethodDescriptor("SendInternalMessage", "MsgSendInternalMessage", "InternalMessage"),
					serviceMethodDescriptor("UpdateContractParams", "MsgUpdateContractParams", "MsgUpdateContractParamsResponse"),
					serviceMethodDescriptor("SubmitSecurityAttestation", "MsgSubmitSecurityAttestation", "MsgSubmitSecurityAttestationResponse"),
					serviceMethodDescriptor("RevokeSecurityAttestation", "MsgRevokeSecurityAttestation", "MsgRevokeSecurityAttestationResponse"),
				},
				Options: &descriptorpb.ServiceOptions{
					UninterpretedOption: []*descriptorpb.UninterpretedOption{{
						Name: []*descriptorpb.UninterpretedOption_NamePart{{
							NamePart:    descriptorString("cosmos.msg.v1.service"),
							IsExtension: descriptorBool(true),
						}},
						IdentifierValue: descriptorString("true"),
					}},
				},
			},
		},
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

func buildContractsQueryFileDescriptor() []byte {
	fd := &descriptorpb.FileDescriptorProto{
		Name:    descriptorString("l1/contracts/v1/query.proto"),
		Package: descriptorString("l1.contracts.v1"),
		Syntax:  descriptorString("proto3"),
		Options: &descriptorpb.FileOptions{
			GoPackage: descriptorString("github.com/sovereign-l1/l1/x/contracts/types"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			messageDescriptor("QueryParamsRequest"),
			messageDescriptor("QueryParamsResponse"),
			messageDescriptor("QueryCodeRequest"),
			messageDescriptor("QueryCodeResponse"),
			messageDescriptor("QueryCodesRequest"),
			messageDescriptor("QueryCodesResponse"),
			messageDescriptor("QueryContractRequest"),
			messageDescriptor("QueryContractResponse"),
			messageDescriptor("QueryContractsRequest"),
			messageDescriptor("QueryContractsResponse"),
			messageDescriptor("QueryContractStorageRequest"),
			messageDescriptor("QueryContractStorageResponse"),
			messageDescriptor("QueryContractReceiptsRequest"),
			messageDescriptor("QueryContractReceiptsResponse"),
			messageDescriptor("QueryContractQueueRequest"),
			messageDescriptor("QueryContractQueueResponse"),
			messageDescriptor("QueryContractEventsRequest"),
			messageDescriptor("QueryContractEventsResponse"),
			messageDescriptor("QueryContractStateRootRequest"),
			messageDescriptor("QueryContractStateRootResponse"),
			messageDescriptor("QuerySecurityAttestationsRequest"),
			messageDescriptor("QuerySecurityAttestationsResponse"),
			messageDescriptor("QuerySecurityBadgeRequest"),
			messageDescriptor("QuerySecurityBadgeResponse"),
		},
		Service: []*descriptorpb.ServiceDescriptorProto{
			{
				Name: descriptorString("Query"),
				Method: []*descriptorpb.MethodDescriptorProto{
					serviceMethodDescriptor("Params", "QueryParamsRequest", "QueryParamsResponse"),
					serviceMethodDescriptor("Code", "QueryCodeRequest", "QueryCodeResponse"),
					serviceMethodDescriptor("Codes", "QueryCodesRequest", "QueryCodesResponse"),
					serviceMethodDescriptor("Contract", "QueryContractRequest", "QueryContractResponse"),
					serviceMethodDescriptor("Contracts", "QueryContractsRequest", "QueryContractsResponse"),
					serviceMethodDescriptor("ContractStorage", "QueryContractStorageRequest", "QueryContractStorageResponse"),
					serviceMethodDescriptor("ContractReceipts", "QueryContractReceiptsRequest", "QueryContractReceiptsResponse"),
					serviceMethodDescriptor("ContractQueue", "QueryContractQueueRequest", "QueryContractQueueResponse"),
					serviceMethodDescriptor("ContractEvents", "QueryContractEventsRequest", "QueryContractEventsResponse"),
					serviceMethodDescriptor("ContractStateRoot", "QueryContractStateRootRequest", "QueryContractStateRootResponse"),
					serviceMethodDescriptor("SecurityAttestations", "QuerySecurityAttestationsRequest", "QuerySecurityAttestationsResponse"),
					serviceMethodDescriptor("SecurityBadge", "QuerySecurityBadgeRequest", "QuerySecurityBadgeResponse"),
				},
			},
		},
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

func messageDescriptor(name string) *descriptorpb.DescriptorProto {
	return &descriptorpb.DescriptorProto{Name: descriptorString(name)}
}

func serviceMethodDescriptor(name, input, output string) *descriptorpb.MethodDescriptorProto {
	return &descriptorpb.MethodDescriptorProto{
		Name:       descriptorString(name),
		InputType:  descriptorString(".l1.contracts.v1." + input),
		OutputType: descriptorString(".l1.contracts.v1." + output),
	}
}

func descriptorString(value string) *string {
	return &value
}

func descriptorBool(value bool) *bool {
	return &value
}
