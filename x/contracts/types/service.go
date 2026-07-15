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

	msgv1 "cosmossdk.io/api/cosmos/msg/v1"
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
	MsgTopUpContractTypeURL             = "/l1.contracts.v1.MsgTopUpContract"
	MsgPayContractStorageDebtTypeURL    = "/l1.contracts.v1.MsgPayContractStorageDebt"
	MsgUnfreezeContractTypeURL          = "/l1.contracts.v1.MsgUnfreezeContract"
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
	TopUpContract(context.Context, *MsgTopUpContract) (*MsgTopUpContractResponse, error)
	PayContractStorageDebt(context.Context, *MsgPayContractStorageDebt) (*MsgPayContractStorageDebtResponse, error)
	UnfreezeContract(context.Context, *MsgUnfreezeContract) (*MsgUnfreezeContractResponse, error)
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
func (UnimplementedGRPCMsgServer) TopUpContract(context.Context, *MsgTopUpContract) (*MsgTopUpContractResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method TopUpContract not implemented")
}
func (UnimplementedGRPCMsgServer) PayContractStorageDebt(context.Context, *MsgPayContractStorageDebt) (*MsgPayContractStorageDebtResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method PayContractStorageDebt not implemented")
}
func (UnimplementedGRPCMsgServer) UnfreezeContract(context.Context, *MsgUnfreezeContract) (*MsgUnfreezeContractResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method UnfreezeContract not implemented")
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
	ContractGet(context.Context, *QueryContractGetRequest) (*QueryContractGetResponse, error)
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
func (UnimplementedGRPCQueryServer) ContractGet(context.Context, *QueryContractGetRequest) (*QueryContractGetResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ContractGet not implemented")
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
		{MethodName: "TopUpContract", Handler: msgHandler(func(s GRPCMsgServer, ctx context.Context, req any) (any, error) {
			return s.TopUpContract(ctx, req.(*MsgTopUpContract))
		}, newMsgTopUpContract)},
		{MethodName: "PayContractStorageDebt", Handler: msgHandler(func(s GRPCMsgServer, ctx context.Context, req any) (any, error) {
			return s.PayContractStorageDebt(ctx, req.(*MsgPayContractStorageDebt))
		}, newMsgPayContractStorageDebt)},
		{MethodName: "UnfreezeContract", Handler: msgHandler(func(s GRPCMsgServer, ctx context.Context, req any) (any, error) {
			return s.UnfreezeContract(ctx, req.(*MsgUnfreezeContract))
		}, newMsgUnfreezeContract)},
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
		{MethodName: "ContractGet", Handler: queryHandler(func(s GRPCQueryServer, ctx context.Context, req any) (any, error) {
			return s.ContractGet(ctx, req.(*QueryContractGetRequest))
		}, newQueryContractGetRequest)},
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
func newMsgTopUpContract() any             { return new(MsgTopUpContract) }
func newMsgPayContractStorageDebt() any    { return new(MsgPayContractStorageDebt) }
func newMsgUnfreezeContract() any          { return new(MsgUnfreezeContract) }
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
func newQueryContractGetRequest() any          { return new(QueryContractGetRequest) }

func init() {
	gogoproto.RegisterType((*MsgStoreCode)(nil), "l1.contracts.v1.MsgStoreCode")
	gogoproto.RegisterType((*MsgDeployContract)(nil), "l1.contracts.v1.MsgDeployContract")
	gogoproto.RegisterType((*MsgExecuteExternal)(nil), "l1.contracts.v1.MsgExecuteExternal")
	gogoproto.RegisterType((*MsgExecuteInternal)(nil), "l1.contracts.v1.MsgExecuteInternal")
	gogoproto.RegisterType((*MsgSendInternalMessage)(nil), "l1.contracts.v1.MsgSendInternalMessage")
	gogoproto.RegisterType((*MsgUpdateContractParams)(nil), "l1.contracts.v1.MsgUpdateContractParams")
	gogoproto.RegisterType((*MsgSubmitSecurityAttestation)(nil), "l1.contracts.v1.MsgSubmitSecurityAttestation")
	gogoproto.RegisterType((*MsgRevokeSecurityAttestation)(nil), "l1.contracts.v1.MsgRevokeSecurityAttestation")
	gogoproto.RegisterType((*MsgTopUpContract)(nil), "l1.contracts.v1.MsgTopUpContract")
	gogoproto.RegisterType((*MsgPayContractStorageDebt)(nil), "l1.contracts.v1.MsgPayContractStorageDebt")
	gogoproto.RegisterType((*MsgUnfreezeContract)(nil), "l1.contracts.v1.MsgUnfreezeContract")
	gogoproto.RegisterType((*StoreCodeResponse)(nil), "l1.contracts.v1.MsgStoreCodeResponse")
	gogoproto.RegisterType((*InstantiateContractResponse)(nil), "l1.contracts.v1.MsgDeployContractResponse")
	gogoproto.RegisterType((*ExecuteContractResponse)(nil), "l1.contracts.v1.MsgExecuteExternalResponse")
	gogoproto.RegisterType((*InternalMessage)(nil), "l1.contracts.v1.InternalMessage")
	gogoproto.RegisterType((*MsgUpdateContractParamsResponse)(nil), "l1.contracts.v1.MsgUpdateContractParamsResponse")
	gogoproto.RegisterType((*MsgSubmitSecurityAttestationResponse)(nil), "l1.contracts.v1.MsgSubmitSecurityAttestationResponse")
	gogoproto.RegisterType((*MsgRevokeSecurityAttestationResponse)(nil), "l1.contracts.v1.MsgRevokeSecurityAttestationResponse")
	gogoproto.RegisterType((*MsgTopUpContractResponse)(nil), "l1.contracts.v1.MsgTopUpContractResponse")
	gogoproto.RegisterType((*MsgPayContractStorageDebtResponse)(nil), "l1.contracts.v1.MsgPayContractStorageDebtResponse")
	gogoproto.RegisterType((*MsgUnfreezeContractResponse)(nil), "l1.contracts.v1.MsgUnfreezeContractResponse")
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
	gogoproto.RegisterType((*GetMethodArg)(nil), "l1.contracts.v1.GetMethodArg")
	gogoproto.RegisterType((*QueryContractGetRequest)(nil), "l1.contracts.v1.QueryContractGetRequest")
	gogoproto.RegisterType((*QueryContractGetResponse)(nil), "l1.contracts.v1.QueryContractGetResponse")
	gogoproto.RegisterFile("l1/contracts/v1/tx.proto", fileDescriptorContractsTx)
	gogoproto.RegisterFile("l1/contracts/v1/query.proto", fileDescriptorContractsQuery)
}

func (m *MsgStoreCode) Reset()         { *m = MsgStoreCode{} }
func (m *MsgStoreCode) String() string { return gogoproto.CompactTextString(m) }
func (*MsgStoreCode) ProtoMessage()    {}

// XXX_MessageName pins this type's proto message name so gogoproto.MessageName
// resolves it correctly regardless of package init order. Without it, if
// api/l1/contracts/v1 (which registers the same "l1.contracts.v1.*" names for
// its own, unrelated generated types) happens to run its init() first, this
// type's entry in gogoproto's global revProtoTypes map is silently never set
// (see cosmos/gogoproto/proto.RegisterType's duplicate-name no-op), and
// RegisterInterfaces below panics with "already registered under typeURL /".
func (*MsgStoreCode) XXX_MessageName() string { return "l1.contracts.v1.MsgStoreCode" }

func (*MsgStoreCode) Descriptor() ([]byte, []int) {
	return fileDescriptorContractsTx, []int{0}
}

func (m *MsgDeployContract) Reset()                { *m = MsgDeployContract{} }
func (m *MsgDeployContract) String() string        { return gogoproto.CompactTextString(m) }
func (*MsgDeployContract) ProtoMessage()           {}
func (*MsgDeployContract) XXX_MessageName() string { return "l1.contracts.v1.MsgDeployContract" }
func (*MsgDeployContract) Descriptor() ([]byte, []int) {
	return fileDescriptorContractsTx, []int{2}
}

func (m *MsgExecuteExternal) Reset()                { *m = MsgExecuteExternal{} }
func (m *MsgExecuteExternal) String() string        { return gogoproto.CompactTextString(m) }
func (*MsgExecuteExternal) ProtoMessage()           {}
func (*MsgExecuteExternal) XXX_MessageName() string { return "l1.contracts.v1.MsgExecuteExternal" }
func (*MsgExecuteExternal) Descriptor() ([]byte, []int) {
	return fileDescriptorContractsTx, []int{4}
}

func (m *MsgExecuteInternal) Reset()                { *m = MsgExecuteInternal{} }
func (m *MsgExecuteInternal) String() string        { return gogoproto.CompactTextString(m) }
func (*MsgExecuteInternal) ProtoMessage()           {}
func (*MsgExecuteInternal) XXX_MessageName() string { return "l1.contracts.v1.MsgExecuteInternal" }
func (*MsgExecuteInternal) Descriptor() ([]byte, []int) {
	return fileDescriptorContractsTx, []int{6}
}

func (m *MsgSendInternalMessage) Reset()         { *m = MsgSendInternalMessage{} }
func (m *MsgSendInternalMessage) String() string { return gogoproto.CompactTextString(m) }
func (*MsgSendInternalMessage) ProtoMessage()    {}
func (*MsgSendInternalMessage) XXX_MessageName() string {
	return "l1.contracts.v1.MsgSendInternalMessage"
}
func (*MsgSendInternalMessage) Descriptor() ([]byte, []int) {
	return fileDescriptorContractsTx, []int{8}
}

func (m *MsgUpdateContractParams) Reset()         { *m = MsgUpdateContractParams{} }
func (m *MsgUpdateContractParams) String() string { return gogoproto.CompactTextString(m) }
func (*MsgUpdateContractParams) ProtoMessage()    {}
func (*MsgUpdateContractParams) XXX_MessageName() string {
	return "l1.contracts.v1.MsgUpdateContractParams"
}
func (*MsgUpdateContractParams) Descriptor() ([]byte, []int) {
	return fileDescriptorContractsTx, []int{9}
}

func (m *StoreCodeResponse) Reset()                { *m = StoreCodeResponse{} }
func (m *StoreCodeResponse) String() string        { return gogoproto.CompactTextString(m) }
func (*StoreCodeResponse) ProtoMessage()           {}
func (*StoreCodeResponse) XXX_MessageName() string { return "l1.contracts.v1.MsgStoreCodeResponse" }

func (m *InstantiateContractResponse) Reset()         { *m = InstantiateContractResponse{} }
func (m *InstantiateContractResponse) String() string { return gogoproto.CompactTextString(m) }
func (*InstantiateContractResponse) ProtoMessage()    {}
func (*InstantiateContractResponse) XXX_MessageName() string {
	return "l1.contracts.v1.MsgDeployContractResponse"
}

func (m *ExecuteContractResponse) Reset()         { *m = ExecuteContractResponse{} }
func (m *ExecuteContractResponse) String() string { return gogoproto.CompactTextString(m) }
func (*ExecuteContractResponse) ProtoMessage()    {}
func (*ExecuteContractResponse) XXX_MessageName() string {
	return "l1.contracts.v1.MsgExecuteExternalResponse"
}

func (m *InternalMessage) Reset()                { *m = InternalMessage{} }
func (m *InternalMessage) String() string        { return gogoproto.CompactTextString(m) }
func (*InternalMessage) ProtoMessage()           {}
func (*InternalMessage) XXX_MessageName() string { return "l1.contracts.v1.InternalMessage" }
func (*InternalMessage) Descriptor() ([]byte, []int) {
	return fileDescriptorContractsTx, []int{7}
}

func (m *MsgUpdateContractParamsResponse) Reset()         { *m = MsgUpdateContractParamsResponse{} }
func (m *MsgUpdateContractParamsResponse) String() string { return gogoproto.CompactTextString(m) }
func (*MsgUpdateContractParamsResponse) ProtoMessage()    {}
func (*MsgUpdateContractParamsResponse) XXX_MessageName() string {
	return "l1.contracts.v1.MsgUpdateContractParamsResponse"
}

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

func (m *GetMethodArg) Reset()         { *m = GetMethodArg{} }
func (m *GetMethodArg) String() string { return gogoproto.CompactTextString(m) }
func (*GetMethodArg) ProtoMessage()    {}

func (m *QueryContractGetRequest) Reset()         { *m = QueryContractGetRequest{} }
func (m *QueryContractGetRequest) String() string { return gogoproto.CompactTextString(m) }
func (*QueryContractGetRequest) ProtoMessage()    {}

func (m *QueryContractGetResponse) Reset()         { *m = QueryContractGetResponse{} }
func (m *QueryContractGetResponse) String() string { return gogoproto.CompactTextString(m) }
func (*QueryContractGetResponse) ProtoMessage()    {}

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
func (*MsgSubmitSecurityAttestation) XXX_MessageName() string {
	return "l1.contracts.v1.MsgSubmitSecurityAttestation"
}
func (*MsgSubmitSecurityAttestation) Descriptor() ([]byte, []int) {
	return fileDescriptorContractsTx, []int{11}
}

func (m *MsgRevokeSecurityAttestation) Reset()         { *m = MsgRevokeSecurityAttestation{} }
func (m *MsgRevokeSecurityAttestation) String() string { return gogoproto.CompactTextString(m) }
func (*MsgRevokeSecurityAttestation) ProtoMessage()    {}
func (*MsgRevokeSecurityAttestation) XXX_MessageName() string {
	return "l1.contracts.v1.MsgRevokeSecurityAttestation"
}
func (*MsgRevokeSecurityAttestation) Descriptor() ([]byte, []int) {
	return fileDescriptorContractsTx, []int{13}
}

func (m *MsgSubmitSecurityAttestationResponse) Reset()         { *m = MsgSubmitSecurityAttestationResponse{} }
func (m *MsgSubmitSecurityAttestationResponse) String() string { return gogoproto.CompactTextString(m) }
func (*MsgSubmitSecurityAttestationResponse) ProtoMessage()    {}
func (*MsgSubmitSecurityAttestationResponse) XXX_MessageName() string {
	return "l1.contracts.v1.MsgSubmitSecurityAttestationResponse"
}

func (m *MsgRevokeSecurityAttestationResponse) Reset()         { *m = MsgRevokeSecurityAttestationResponse{} }
func (m *MsgRevokeSecurityAttestationResponse) String() string { return gogoproto.CompactTextString(m) }
func (*MsgRevokeSecurityAttestationResponse) ProtoMessage()    {}
func (*MsgRevokeSecurityAttestationResponse) XXX_MessageName() string {
	return "l1.contracts.v1.MsgRevokeSecurityAttestationResponse"
}

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

func (m *MsgTopUpContract) Reset()                { *m = MsgTopUpContract{} }
func (m *MsgTopUpContract) String() string        { return gogoproto.CompactTextString(m) }
func (*MsgTopUpContract) ProtoMessage()           {}
func (*MsgTopUpContract) XXX_MessageName() string { return "l1.contracts.v1.MsgTopUpContract" }
func (*MsgTopUpContract) Descriptor() ([]byte, []int) {
	return fileDescriptorContractsTx, []int{21}
}

func (m *MsgTopUpContractResponse) Reset()         { *m = MsgTopUpContractResponse{} }
func (m *MsgTopUpContractResponse) String() string { return gogoproto.CompactTextString(m) }
func (*MsgTopUpContractResponse) ProtoMessage()    {}
func (*MsgTopUpContractResponse) XXX_MessageName() string {
	return "l1.contracts.v1.MsgTopUpContractResponse"
}

func (m *MsgPayContractStorageDebt) Reset()         { *m = MsgPayContractStorageDebt{} }
func (m *MsgPayContractStorageDebt) String() string { return gogoproto.CompactTextString(m) }
func (*MsgPayContractStorageDebt) ProtoMessage()    {}
func (*MsgPayContractStorageDebt) XXX_MessageName() string {
	return "l1.contracts.v1.MsgPayContractStorageDebt"
}
func (*MsgPayContractStorageDebt) Descriptor() ([]byte, []int) {
	return fileDescriptorContractsTx, []int{23}
}

func (m *MsgPayContractStorageDebtResponse) Reset()         { *m = MsgPayContractStorageDebtResponse{} }
func (m *MsgPayContractStorageDebtResponse) String() string { return gogoproto.CompactTextString(m) }
func (*MsgPayContractStorageDebtResponse) ProtoMessage()    {}
func (*MsgPayContractStorageDebtResponse) XXX_MessageName() string {
	return "l1.contracts.v1.MsgPayContractStorageDebtResponse"
}

func (m *MsgUnfreezeContract) Reset()                { *m = MsgUnfreezeContract{} }
func (m *MsgUnfreezeContract) String() string        { return gogoproto.CompactTextString(m) }
func (*MsgUnfreezeContract) ProtoMessage()           {}
func (*MsgUnfreezeContract) XXX_MessageName() string { return "l1.contracts.v1.MsgUnfreezeContract" }
func (*MsgUnfreezeContract) Descriptor() ([]byte, []int) {
	return fileDescriptorContractsTx, []int{25}
}

func (m *MsgUnfreezeContractResponse) Reset()         { *m = MsgUnfreezeContractResponse{} }
func (m *MsgUnfreezeContractResponse) String() string { return gogoproto.CompactTextString(m) }
func (*MsgUnfreezeContractResponse) ProtoMessage()    {}
func (*MsgUnfreezeContractResponse) XXX_MessageName() string {
	return "l1.contracts.v1.MsgUnfreezeContractResponse"
}

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
			withSigner(messageDescriptorFields("MsgStoreCode",
				stringField("authority", 1),
				bytesField("bytecode", 2),
				stringField("code_hash", 3),
				uint64Field("code_bytes", 4),
			), "authority"),
			messageDescriptorFields("MsgStoreCodeResponse",
				stringField("code_id", 1),
				stringField("state_root", 2),
			),
			withSigner(messageDescriptorFields("MsgDeployContract",
				stringField("creator", 1),
				stringField("code_id", 2),
				stringField("salt", 3),
				bytesField("init_payload", 4),
				uint64Field("initial_balance", 5),
				stringField("admin", 6),
				bytesField("metadata", 7),
				stringField("avm_chain_id", 8),
				stringField("avm_namespace", 9),
				messageField("state_init", 10, ".l1.contracts.v1.StateInit"),
				boolField("upgradeable", 11),
				boolField("system_owned", 12),
				uint64Field("schema_version", 13),
				uint64Field("height", 14),
			), "creator"),
			messageDescriptorFields("MsgDeployContractResponse",
				stringField("contract_address_user", 1),
				stringField("contract_address_raw", 2),
				stringField("owner", 3),
				stringField("admin", 4),
				uint64Field("balance", 5),
				repeatedMessageField("events", 6, ".l1.contracts.v1.ContractEvent"),
			),
			withSigner(messageDescriptorFields("MsgExecuteExternal",
				stringField("sender", 1),
				stringField("contract_address", 2),
				bytesField("payload", 3),
				uint64Field("funds", 4),
				uint64Field("gas_limit", 5),
				bytesField("metadata", 6),
				stringField("avm_chain_id", 7),
				stringField("avm_namespace", 8),
				messageField("state_init", 9, ".l1.contracts.v1.StateInit"),
				uint64Field("height", 10),
				uint32Field("opcode", 11),
			), "sender"),
			messageDescriptorFields("MsgExecuteExternalResponse",
				stringField("contract_address_user", 1),
				stringField("owner", 2),
				uint64Field("balance", 3),
				repeatedMessageField("events", 4, ".l1.contracts.v1.ContractEvent"),
			),
			messageDescriptorFields("MsgExecuteInternal",
				messageField("message", 1, ".l1.contracts.v1.InternalMessage"),
				uint64Field("height", 2),
			),
			messageDescriptorFields("InternalMessage",
				stringField("source_contract_user", 1),
				stringField("destination_account", 2),
				uint64Field("funds", 3),
				uint32Field("opcode", 4),
				uint64Field("query_id", 5),
				bytesField("body", 6),
				messageField("state_init", 7, ".l1.contracts.v1.StateInit"),
				boolField("bounce", 8),
				uint64Field("deadline", 9),
				uint64Field("gas_limit", 10),
				uint64Field("logical_time", 11),
				stringField("message_id", 12),
				boolField("refunded", 13),
				uint64Field("height", 14),
				uint32Field("mode", 15),
				stringField("comment", 16),
			),
			messageDescriptorFields("MsgSendInternalMessage",
				messageField("message", 1, ".l1.contracts.v1.InternalMessage"),
				uint64Field("height", 2),
			),
			withSigner(messageDescriptorFields("MsgUpdateContractParams",
				stringField("authority", 1),
				messageField("params", 2, ".l1.contracts.v1.Params"),
			), "authority"),
			messageDescriptorFields("MsgUpdateContractParamsResponse",
				stringField("state_root", 1),
			),
			withSigner(messageDescriptorFields("MsgSubmitSecurityAttestation",
				stringField("authority", 1),
				messageField("attestation", 2, ".l1.contracts.v1.ContractSecurityAttestation"),
			), "authority"),
			messageDescriptorFields("MsgSubmitSecurityAttestationResponse",
				messageField("attestation", 1, ".l1.contracts.v1.ContractSecurityAttestation"),
				stringField("state_root", 2),
			),
			withSigner(messageDescriptorFields("MsgRevokeSecurityAttestation",
				stringField("authority", 1),
				stringField("attestation_id", 2),
				stringField("revoked_reason", 3),
				uint64Field("height", 4),
			), "authority"),
			messageDescriptorFields("MsgRevokeSecurityAttestationResponse",
				messageField("attestation", 1, ".l1.contracts.v1.ContractSecurityAttestation"),
				stringField("state_root", 2),
			),
			messageDescriptorFields("StateInit",
				uint32Field("abi_version", 1),
				stringField("code_id", 2),
				stringField("code_hash", 3),
				bytesField("init_data", 4),
				stringField("salt", 5),
				bytesField("salt_bytes", 6),
				stringField("owner", 7),
				repeatedMessageField("libraries", 8, ".l1.contracts.v1.CodeDependency"),
				stringField("initial_storage_root", 9),
				uint64Field("initial_balance_naet", 10),
				repeatedStringField("capabilities", 11),
			),
			messageDescriptorFields("CodeDependency",
				stringField("code_id", 1),
				stringField("code_hash", 2),
			),
			messageDescriptorFields("ContractEvent",
				stringField("type", 1),
				stringField("actor", 2),
				stringField("contract", 3),
				uint64Field("amount", 4),
				stringField("internal_raw", 5),
			),
			// Params, SecurityGraphEdge, and ContractSecurityAttestation live in
			// this (tx) file because tx messages reference them; the query file
			// imports them via its Dependency on tx.proto. Defining them in both
			// files would be a duplicate-registration error, and defining them
			// only in query.proto would leave the tx messages unresolvable since
			// proto imports cannot be circular.
			messageDescriptorFields("Params",
				stringField("authority", 1),
				boolField("enabled", 2),
				uint64Field("max_code_bytes", 3),
				uint64Field("max_contract_storage_bytes", 4),
				uint64Field("max_gas_per_execution", 5),
				uint64Field("storage_rent_per_byte_block", 6),
				uint64Field("max_init_data_bytes", 7),
				uint64Field("max_state_init_salt_bytes", 8),
				uint32Field("max_state_init_dependencies", 9),
				uint64Field("max_internal_message_gas_per_block", 10),
			),
			messageDescriptorFields("SecurityGraphEdge",
				stringField("from", 1),
				stringField("to", 2),
				stringField("relation", 3),
			),
			messageDescriptorFields("ContractSecurityAttestation",
				stringField("attestation_id", 1),
				stringField("contract_address_user", 2),
				stringField("contract_address_raw", 3),
				stringField("source", 4),
				stringField("source_url", 5),
				stringField("commit_hash", 6),
				stringField("code_hash", 7),
				stringField("evidence_hash", 8),
				uint64Field("checked_height", 9),
				uint64Field("updated_height", 10),
				uint32Field("risk_score_bps", 11),
				repeatedStringField("categories", 12),
				repeatedStringField("flags", 13),
				repeatedStringField("related_addresses", 14),
				repeatedMessageField("graph_edges", 15, ".l1.contracts.v1.SecurityGraphEdge"),
				stringField("status", 16),
				stringField("revoked_reason", 17),
				stringField("signed_by", 18),
			),
			withSigner(messageDescriptorFields("MsgTopUpContract",
				stringField("sender", 1),
				stringField("contract_address", 2),
				uint64Field("amount", 3),
				uint64Field("height", 4),
			), "sender"),
			// MsgTopUpContractResponse wraps Contract, which is defined in
			// query.proto; tx.proto cannot import query.proto back (query.proto
			// already depends on tx.proto, and proto imports cannot be
			// circular -- see the Dependency comment in
			// buildContractsQueryFileDescriptor), so the field is left
			// undeclared here. This descriptor is registry metadata only: Msg
			// wire encoding for hand-written types in this package uses the Go
			// struct's protobuf tags via gogoproto's reflection-based Marshal
			// fallback, not this descriptor (see query_marshal.go's doc comment
			// for why only the query-side types need genuine hand-rolled
			// Marshal/Unmarshal methods).
			messageDescriptor("MsgTopUpContractResponse"),
			withSigner(messageDescriptorFields("MsgPayContractStorageDebt",
				stringField("sender", 1),
				stringField("contract_address", 2),
				uint64Field("amount", 3),
				uint64Field("height", 4),
			), "sender"),
			messageDescriptor("MsgPayContractStorageDebtResponse"),
			withSigner(messageDescriptorFields("MsgUnfreezeContract",
				stringField("sender", 1),
				stringField("contract_address", 2),
				uint64Field("height", 3),
			), "sender"),
			messageDescriptor("MsgUnfreezeContractResponse"),
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
					serviceMethodDescriptor("TopUpContract", "MsgTopUpContract", "MsgTopUpContractResponse"),
					serviceMethodDescriptor("PayContractStorageDebt", "MsgPayContractStorageDebt", "MsgPayContractStorageDebtResponse"),
					serviceMethodDescriptor("UnfreezeContract", "MsgUnfreezeContract", "MsgUnfreezeContractResponse"),
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
		// Query messages reference StateInit, InternalMessage, Params, and
		// ContractSecurityAttestation, which are defined in tx.proto (they are
		// also referenced by tx messages, and proto imports cannot be
		// circular). tx.proto is registered first in init() below.
		Dependency: []string{"l1/contracts/v1/tx.proto"},
		Options: &descriptorpb.FileOptions{
			GoPackage: descriptorString("github.com/sovereign-l1/l1/x/contracts/types"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			messageDescriptorFields("PageRequest",
				uint32Field("limit", 1),
			),
			messageDescriptor("QueryParamsRequest"),
			messageDescriptorFields("QueryParamsResponse",
				messageField("params", 1, ".l1.contracts.v1.Params"),
			),
			messageDescriptorFields("QueryCodeRequest",
				stringField("code_id", 1),
			),
			messageDescriptorFields("QueryCodeResponse",
				messageField("code", 1, ".l1.contracts.v1.CodeRecord"),
				boolField("found", 2),
			),
			messageDescriptorFields("CodeRecord",
				stringField("code_id", 1),
				stringField("code_hash", 2),
				uint64Field("code_bytes", 3),
				bytesField("bytecode", 4),
				stringField("owner", 5),
			),
			messageDescriptorFields("QueryCodesRequest",
				messageField("pagination", 1, ".l1.contracts.v1.PageRequest"),
			),
			messageDescriptorFields("QueryCodesResponse",
				repeatedMessageField("codes", 1, ".l1.contracts.v1.CodeRecord"),
			),
			messageDescriptorFields("QueryContractRequest",
				stringField("contract_address", 1),
				stringField("chain_id", 2),
				stringField("namespace", 3),
				stringField("deployer", 4),
				messageField("state_init", 5, ".l1.contracts.v1.StateInit"),
			),
			messageDescriptorFields("QueryContractResponse",
				stringField("contract_address", 1),
				stringField("state_root", 2),
				boolField("found", 3),
				boolField("virtual", 4),
				messageField("contract", 5, ".l1.contracts.v1.Contract"),
				stringField("status", 6),
			),
			messageDescriptorFields("Contract",
				stringField("address_user", 1),
				stringField("address_raw", 2),
				stringField("code_id", 3),
				stringField("code_hash", 4),
				stringField("state_init_hash", 5),
				messageField("state_init", 6, ".l1.contracts.v1.StateInit"),
				stringField("creator", 7),
				stringField("owner", 8),
				stringField("admin", 9),
				boolField("upgradeable", 10),
				boolField("upgrades_disabled", 11),
				boolField("system_owned", 12),
				uint64Field("storage_schema_version", 13),
				bytesField("init_msg", 14),
				bytesField("data", 15),
				uint64Field("balance", 16),
				stringField("state_root", 17),
				stringField("status", 18),
				uint64Field("storage_bytes", 19),
				uint64Field("last_storage_charge_height", 20),
				uint64Field("storage_rent_debt", 21),
				uint64Field("logical_time", 22),
				uint64Field("created_height", 23),
				uint64Field("updated_height", 24),
			),
			messageDescriptorFields("QueryContractsRequest",
				messageField("pagination", 1, ".l1.contracts.v1.PageRequest"),
			),
			messageDescriptorFields("QueryContractsResponse",
				repeatedMessageField("contracts", 1, ".l1.contracts.v1.Contract"),
			),
			messageDescriptorFields("QueryContractStorageRequest",
				stringField("contract_address", 1),
				bytesField("key_prefix", 2),
				messageField("pagination", 3, ".l1.contracts.v1.PageRequest"),
			),
			messageDescriptorFields("QueryContractStorageResponse",
				repeatedMessageField("entries", 1, ".l1.contracts.v1.ContractStorageEntry"),
			),
			messageDescriptorFields("ContractStorageEntry",
				stringField("contract_address", 1),
				bytesField("key", 2),
				bytesField("value", 3),
			),
			messageDescriptorFields("QueryContractReceiptsRequest",
				stringField("contract_address", 1),
				messageField("pagination", 2, ".l1.contracts.v1.PageRequest"),
			),
			messageDescriptorFields("QueryContractReceiptsResponse",
				repeatedMessageField("receipts", 1, ".l1.contracts.v1.ContractReceipt"),
			),
			messageDescriptorFields("ContractReceipt",
				stringField("receipt_id", 1),
				stringField("contract_address", 2),
				stringField("actor", 3),
				stringField("operation", 4),
				uint32Field("exit_code", 5),
				uint64Field("amount", 6),
				uint64Field("gas_used", 7),
				uint64Field("logical_time", 8),
				uint64Field("height", 9),
			),
			messageDescriptorFields("QueryContractQueueRequest",
				stringField("contract_address", 1),
				messageField("pagination", 2, ".l1.contracts.v1.PageRequest"),
			),
			messageDescriptorFields("QueryContractQueueResponse",
				repeatedMessageField("messages", 1, ".l1.contracts.v1.InternalMessage"),
			),
			messageDescriptorFields("QueryContractEventsRequest",
				stringField("contract_address", 1),
				messageField("pagination", 2, ".l1.contracts.v1.PageRequest"),
			),
			messageDescriptor("QueryContractEventsResponse"),
			messageDescriptorFields("QueryContractStateRootRequest",
				stringField("contract_address", 1),
			),
			messageDescriptorFields("QueryContractStateRootResponse",
				stringField("state_root", 1),
			),
			messageDescriptorFields("QuerySecurityAttestationsRequest",
				stringField("contract_address", 1),
				boolField("include_revoked", 2),
				messageField("pagination", 3, ".l1.contracts.v1.PageRequest"),
			),
			messageDescriptorFields("QuerySecurityAttestationsResponse",
				repeatedMessageField("attestations", 1, ".l1.contracts.v1.ContractSecurityAttestation"),
			),
			messageDescriptorFields("QuerySecurityBadgeRequest",
				stringField("contract_address", 1),
			),
			messageDescriptorFields("QuerySecurityBadgeResponse",
				messageField("badge", 1, ".l1.contracts.v1.ContractSecurityBadge"),
				boolField("found", 2),
			),
			messageDescriptorFields("GetMethodArg",
				stringField("type", 1),
				stringField("value", 2),
			),
			messageDescriptorFields("QueryContractGetRequest",
				stringField("contract_address", 1),
				stringField("method", 2),
				repeatedMessageField("args", 3, ".l1.contracts.v1.GetMethodArg"),
				uint64Field("gas_limit", 4),
			),
			messageDescriptorFields("QueryContractGetResponse",
				boolField("success", 1),
				uint32Field("exit_code", 2),
				uint64Field("gas_used", 3),
				stringField("result", 4),
				stringField("result_type", 5),
				stringField("method", 6),
				uint32Field("selector", 7),
				stringField("error", 8),
			),
			messageDescriptorFields("ContractSecurityBadge",
				stringField("contract_address", 1),
				stringField("badge", 2),
				boolField("verified", 3),
				uint32Field("risk_score_bps", 4),
				repeatedStringField("categories", 5),
				repeatedStringField("flags", 6),
				repeatedStringField("related_addresses", 7),
				repeatedMessageField("graph_edges", 8, ".l1.contracts.v1.SecurityGraphEdge"),
				uint32Field("attestation_count", 9),
				uint32Field("active_attestation_count", 10),
				uint32Field("revoked_attestation_count", 11),
				uint64Field("latest_updated_height", 12),
				repeatedStringField("attestation_ids", 13),
			),
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
					serviceMethodDescriptor("ContractGet", "QueryContractGetRequest", "QueryContractGetResponse"),
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

func messageDescriptorFields(name string, fields ...*descriptorpb.FieldDescriptorProto) *descriptorpb.DescriptorProto {
	return &descriptorpb.DescriptorProto{Name: descriptorString(name), Field: fields}
}

// withSigner marks msg with the cosmos.msg.v1.signer option naming
// fieldName as the account whose signature authorizes this message, matching
// the semantics used by proto-generated cosmos-sdk Msg types. Set via the
// real protobuf-go extension API (not UninterpretedOption) so the SDK's
// x/tx/signing.Context can resolve it via a plain descriptor lookup.
func withSigner(msg *descriptorpb.DescriptorProto, fieldName string) *descriptorpb.DescriptorProto {
	opts := &descriptorpb.MessageOptions{}
	proto2.SetExtension(opts, msgv1.E_Signer, []string{fieldName})
	msg.Options = opts
	return msg
}

func descriptorLabel(label descriptorpb.FieldDescriptorProto_Label) *descriptorpb.FieldDescriptorProto_Label {
	return &label
}

func descriptorFieldType(kind descriptorpb.FieldDescriptorProto_Type) *descriptorpb.FieldDescriptorProto_Type {
	return &kind
}

func descriptorInt32(value int32) *int32 {
	return &value
}

func scalarField(name string, number int32, kind descriptorpb.FieldDescriptorProto_Type, repeated bool) *descriptorpb.FieldDescriptorProto {
	label := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
	if repeated {
		label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED
	}
	return &descriptorpb.FieldDescriptorProto{
		Name:     descriptorString(name),
		Number:   descriptorInt32(number),
		Label:    descriptorLabel(label),
		Type:     descriptorFieldType(kind),
		JsonName: descriptorString(protoJSONName(name)),
	}
}

func stringField(name string, number int32) *descriptorpb.FieldDescriptorProto {
	return scalarField(name, number, descriptorpb.FieldDescriptorProto_TYPE_STRING, false)
}

func repeatedStringField(name string, number int32) *descriptorpb.FieldDescriptorProto {
	return scalarField(name, number, descriptorpb.FieldDescriptorProto_TYPE_STRING, true)
}

func bytesField(name string, number int32) *descriptorpb.FieldDescriptorProto {
	return scalarField(name, number, descriptorpb.FieldDescriptorProto_TYPE_BYTES, false)
}

func boolField(name string, number int32) *descriptorpb.FieldDescriptorProto {
	return scalarField(name, number, descriptorpb.FieldDescriptorProto_TYPE_BOOL, false)
}

func uint32Field(name string, number int32) *descriptorpb.FieldDescriptorProto {
	return scalarField(name, number, descriptorpb.FieldDescriptorProto_TYPE_UINT32, false)
}

func uint64Field(name string, number int32) *descriptorpb.FieldDescriptorProto {
	return scalarField(name, number, descriptorpb.FieldDescriptorProto_TYPE_UINT64, false)
}

func messageField(name string, number int32, typeName string) *descriptorpb.FieldDescriptorProto {
	f := scalarField(name, number, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, false)
	f.TypeName = descriptorString(typeName)
	return f
}

func repeatedMessageField(name string, number int32, typeName string) *descriptorpb.FieldDescriptorProto {
	f := scalarField(name, number, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, true)
	f.TypeName = descriptorString(typeName)
	return f
}

// protoJSONName lower-camel-cases a snake_case proto field name, matching
// protoc's default jsonName derivation (e.g. "code_hash" -> "codeHash").
func protoJSONName(name string) string {
	var buf bytes.Buffer
	upperNext := false
	for _, r := range name {
		if r == '_' {
			upperNext = true
			continue
		}
		if upperNext && r >= 'a' && r <= 'z' {
			r -= 'a' - 'A'
		}
		upperNext = false
		buf.WriteRune(r)
	}
	return buf.String()
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
