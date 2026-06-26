package types

import (
	"context"

	"github.com/cosmos/gogoproto/grpc"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func RegisterMsgServer(server grpc.Server, srv MsgServer) {
	server.RegisterService(&_Msg_serviceDesc, srv)
}

func RegisterQueryServer(server grpc.Server, srv QueryServer) {
	server.RegisterService(&_Query_serviceDesc, srv)
}

var _Msg_serviceDesc = grpcgo.ServiceDesc{
	ServiceName: "l1.services.v1.Msg",
	HandlerType: (*MsgServer)(nil),
	Methods: []grpcgo.MethodDesc{
		{MethodName: "RegisterService", Handler: _Msg_RegisterService_Handler},
		{MethodName: "UpdateService", Handler: _Msg_UpdateService_Handler},
		{MethodName: "RenewService", Handler: _Msg_RenewService_Handler},
		{MethodName: "DisableService", Handler: _Msg_DisableService_Handler},
		{MethodName: "TransferService", Handler: _Msg_TransferService_Handler},
		{MethodName: "BindServiceIdentity", Handler: _Msg_BindServiceIdentity_Handler},
		{MethodName: "UnbindServiceIdentity", Handler: _Msg_UnbindServiceIdentity_Handler},
		{MethodName: "RegisterProvider", Handler: _Msg_RegisterProvider_Handler},
		{MethodName: "UpdateProvider", Handler: _Msg_UpdateProvider_Handler},
		{MethodName: "StakeProviderCollateral", Handler: _Msg_StakeProviderCollateral_Handler},
		{MethodName: "UnstakeProviderCollateral", Handler: _Msg_UnstakeProviderCollateral_Handler},
		{MethodName: "AnchorServiceReceipt", Handler: _Msg_AnchorServiceReceipt_Handler},
		{MethodName: "SubmitServiceDispute", Handler: _Msg_SubmitServiceDispute_Handler},
	},
	Streams:  []grpcgo.StreamDesc{},
	Metadata: "l1/services/v1/tx.proto",
}

var _Query_serviceDesc = grpcgo.ServiceDesc{
	ServiceName: "l1.services.v1.Query",
	HandlerType: (*QueryServer)(nil),
	Methods: []grpcgo.MethodDesc{
		{MethodName: "Service", Handler: _Query_Service_Handler},
		{MethodName: "ServiceByName", Handler: _Query_ServiceByName_Handler},
		{MethodName: "ServicesByOwner", Handler: _Query_ServicesByOwner_Handler},
		{MethodName: "ServicesByIdentity", Handler: _Query_ServicesByIdentity_Handler},
		{MethodName: "ProvidersByService", Handler: _Query_ProvidersByService_Handler},
		{MethodName: "ServiceInterface", Handler: _Query_ServiceInterface_Handler},
		{MethodName: "ServicePaymentModel", Handler: _Query_ServicePaymentModel_Handler},
		{MethodName: "ServiceVerificationModel", Handler: _Query_ServiceVerificationModel_Handler},
		{MethodName: "ServiceReceipt", Handler: _Query_ServiceReceipt_Handler},
		{MethodName: "ServiceProof", Handler: _Query_ServiceProof_Handler},
		{MethodName: "ServiceParams", Handler: _Query_ServiceParams_Handler},
	},
	Streams:  []grpcgo.StreamDesc{},
	Metadata: "l1/services/v1/query.proto",
}

func _Msg_RegisterService_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	in := new(MsgRegisterService)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).RegisterService(ctx, in)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.services.v1.Msg/RegisterService"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MsgServer).RegisterService(ctx, req.(*MsgRegisterService))
	}
	return interceptor(ctx, in, info, handler)
}

func _Msg_UpdateService_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	in := new(MsgUpdateService)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).UpdateService(ctx, in)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.services.v1.Msg/UpdateService"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MsgServer).UpdateService(ctx, req.(*MsgUpdateService))
	}
	return interceptor(ctx, in, info, handler)
}

func _Msg_RenewService_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	in := new(MsgRenewService)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).RenewService(ctx, in)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.services.v1.Msg/RenewService"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MsgServer).RenewService(ctx, req.(*MsgRenewService))
	}
	return interceptor(ctx, in, info, handler)
}

func _Msg_DisableService_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	in := new(MsgDisableService)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).DisableService(ctx, in)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.services.v1.Msg/DisableService"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MsgServer).DisableService(ctx, req.(*MsgDisableService))
	}
	return interceptor(ctx, in, info, handler)
}

func _Msg_TransferService_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	in := new(MsgTransferService)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).TransferService(ctx, in)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.services.v1.Msg/TransferService"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MsgServer).TransferService(ctx, req.(*MsgTransferService))
	}
	return interceptor(ctx, in, info, handler)
}

func _Msg_BindServiceIdentity_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	in := new(MsgBindServiceIdentity)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).BindServiceIdentity(ctx, in)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.services.v1.Msg/BindServiceIdentity"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MsgServer).BindServiceIdentity(ctx, req.(*MsgBindServiceIdentity))
	}
	return interceptor(ctx, in, info, handler)
}

func _Msg_UnbindServiceIdentity_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	in := new(MsgUnbindServiceIdentity)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).UnbindServiceIdentity(ctx, in)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.services.v1.Msg/UnbindServiceIdentity"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MsgServer).UnbindServiceIdentity(ctx, req.(*MsgUnbindServiceIdentity))
	}
	return interceptor(ctx, in, info, handler)
}

func _Msg_RegisterProvider_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	in := new(MsgRegisterProvider)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).RegisterProvider(ctx, in)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.services.v1.Msg/RegisterProvider"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MsgServer).RegisterProvider(ctx, req.(*MsgRegisterProvider))
	}
	return interceptor(ctx, in, info, handler)
}

func _Msg_UpdateProvider_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	in := new(MsgUpdateProvider)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).UpdateProvider(ctx, in)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.services.v1.Msg/UpdateProvider"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MsgServer).UpdateProvider(ctx, req.(*MsgUpdateProvider))
	}
	return interceptor(ctx, in, info, handler)
}

func _Msg_StakeProviderCollateral_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	in := new(MsgStakeProviderCollateral)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).StakeProviderCollateral(ctx, in)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.services.v1.Msg/StakeProviderCollateral"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MsgServer).StakeProviderCollateral(ctx, req.(*MsgStakeProviderCollateral))
	}
	return interceptor(ctx, in, info, handler)
}

func _Msg_UnstakeProviderCollateral_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	in := new(MsgUnstakeProviderCollateral)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).UnstakeProviderCollateral(ctx, in)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.services.v1.Msg/UnstakeProviderCollateral"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MsgServer).UnstakeProviderCollateral(ctx, req.(*MsgUnstakeProviderCollateral))
	}
	return interceptor(ctx, in, info, handler)
}

func _Msg_AnchorServiceReceipt_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	in := new(MsgAnchorServiceReceipt)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).AnchorServiceReceipt(ctx, in)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.services.v1.Msg/AnchorServiceReceipt"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MsgServer).AnchorServiceReceipt(ctx, req.(*MsgAnchorServiceReceipt))
	}
	return interceptor(ctx, in, info, handler)
}

func _Msg_SubmitServiceDispute_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	in := new(MsgSubmitServiceDispute)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).SubmitServiceDispute(ctx, in)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.services.v1.Msg/SubmitServiceDispute"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MsgServer).SubmitServiceDispute(ctx, req.(*MsgSubmitServiceDispute))
	}
	return interceptor(ctx, in, info, handler)
}

func _Query_Service_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	in := new(QueryService)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(QueryServer).Service(ctx, in)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.services.v1.Query/Service"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(QueryServer).Service(ctx, req.(*QueryService))
	}
	return interceptor(ctx, in, info, handler)
}

func _Query_ServiceByName_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	in := new(QueryServiceByName)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(QueryServer).ServiceByName(ctx, in)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.services.v1.Query/ServiceByName"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(QueryServer).ServiceByName(ctx, req.(*QueryServiceByName))
	}
	return interceptor(ctx, in, info, handler)
}

func _Query_ServicesByOwner_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	in := new(QueryServicesByOwner)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(QueryServer).ServicesByOwner(ctx, in)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.services.v1.Query/ServicesByOwner"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(QueryServer).ServicesByOwner(ctx, req.(*QueryServicesByOwner))
	}
	return interceptor(ctx, in, info, handler)
}

func _Query_ServicesByIdentity_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	in := new(QueryServicesByIdentity)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(QueryServer).ServicesByIdentity(ctx, in)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.services.v1.Query/ServicesByIdentity"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(QueryServer).ServicesByIdentity(ctx, req.(*QueryServicesByIdentity))
	}
	return interceptor(ctx, in, info, handler)
}

func _Query_ProvidersByService_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	in := new(QueryProvidersByService)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(QueryServer).ProvidersByService(ctx, in)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.services.v1.Query/ProvidersByService"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(QueryServer).ProvidersByService(ctx, req.(*QueryProvidersByService))
	}
	return interceptor(ctx, in, info, handler)
}

func _Query_ServiceInterface_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	in := new(QueryServiceInterface)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(QueryServer).ServiceInterface(ctx, in)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.services.v1.Query/ServiceInterface"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(QueryServer).ServiceInterface(ctx, req.(*QueryServiceInterface))
	}
	return interceptor(ctx, in, info, handler)
}

func _Query_ServicePaymentModel_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	in := new(QueryServicePaymentModel)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(QueryServer).ServicePaymentModel(ctx, in)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.services.v1.Query/ServicePaymentModel"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(QueryServer).ServicePaymentModel(ctx, req.(*QueryServicePaymentModel))
	}
	return interceptor(ctx, in, info, handler)
}

func _Query_ServiceVerificationModel_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	in := new(QueryServiceVerificationModel)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(QueryServer).ServiceVerificationModel(ctx, in)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.services.v1.Query/ServiceVerificationModel"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(QueryServer).ServiceVerificationModel(ctx, req.(*QueryServiceVerificationModel))
	}
	return interceptor(ctx, in, info, handler)
}

func _Query_ServiceReceipt_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	in := new(QueryServiceReceipt)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(QueryServer).ServiceReceipt(ctx, in)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.services.v1.Query/ServiceReceipt"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(QueryServer).ServiceReceipt(ctx, req.(*QueryServiceReceipt))
	}
	return interceptor(ctx, in, info, handler)
}

func _Query_ServiceProof_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	in := new(QueryServiceProof)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(QueryServer).ServiceProof(ctx, in)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.services.v1.Query/ServiceProof"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(QueryServer).ServiceProof(ctx, req.(*QueryServiceProof))
	}
	return interceptor(ctx, in, info, handler)
}

func _Query_ServiceParams_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	in := new(QueryServiceParams)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(QueryServer).ServiceParams(ctx, in)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.services.v1.Query/ServiceParams"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(QueryServer).ServiceParams(ctx, req.(*QueryServiceParams))
	}
	return interceptor(ctx, in, info, handler)
}

type UnimplementedMsgServer struct{}

func (UnimplementedMsgServer) RegisterService(context.Context, *MsgRegisterService) (*MsgRegisterServiceResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method RegisterService not implemented")
}
func (UnimplementedMsgServer) UpdateService(context.Context, *MsgUpdateService) (*MsgUpdateServiceResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method UpdateService not implemented")
}
func (UnimplementedMsgServer) RegisterInterface(context.Context, *MsgRegisterInterface) (*MsgRegisterInterfaceResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method RegisterInterface not implemented")
}
func (UnimplementedMsgServer) UpdateInterface(context.Context, *MsgUpdateInterface) (*MsgUpdateInterfaceResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method UpdateInterface not implemented")
}
func (UnimplementedMsgServer) RenewService(context.Context, *MsgRenewService) (*MsgRenewServiceResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method RenewService not implemented")
}
func (UnimplementedMsgServer) DisableService(context.Context, *MsgDisableService) (*MsgDisableServiceResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DisableService not implemented")
}
func (UnimplementedMsgServer) TransferService(context.Context, *MsgTransferService) (*MsgTransferServiceResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method TransferService not implemented")
}
func (UnimplementedMsgServer) BindServiceIdentity(context.Context, *MsgBindServiceIdentity) (*MsgBindServiceIdentityResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method BindServiceIdentity not implemented")
}
func (UnimplementedMsgServer) UnbindServiceIdentity(context.Context, *MsgUnbindServiceIdentity) (*MsgUnbindServiceIdentityResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method UnbindServiceIdentity not implemented")
}
func (UnimplementedMsgServer) RegisterProvider(context.Context, *MsgRegisterProvider) (*MsgRegisterProviderResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method RegisterProvider not implemented")
}
func (UnimplementedMsgServer) UpdateProvider(context.Context, *MsgUpdateProvider) (*MsgUpdateProviderResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method UpdateProvider not implemented")
}
func (UnimplementedMsgServer) StakeProviderCollateral(context.Context, *MsgStakeProviderCollateral) (*MsgStakeProviderCollateralResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method StakeProviderCollateral not implemented")
}
func (UnimplementedMsgServer) UnstakeProviderCollateral(context.Context, *MsgUnstakeProviderCollateral) (*MsgUnstakeProviderCollateralResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method UnstakeProviderCollateral not implemented")
}
func (UnimplementedMsgServer) AnchorServiceReceipt(context.Context, *MsgAnchorServiceReceipt) (*MsgAnchorServiceReceiptResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method AnchorServiceReceipt not implemented")
}
func (UnimplementedMsgServer) SubmitServiceDispute(context.Context, *MsgSubmitServiceDispute) (*MsgSubmitServiceDisputeResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method SubmitServiceDispute not implemented")
}

type UnimplementedQueryServer struct{}

func (UnimplementedQueryServer) Service(context.Context, *QueryService) (*QueryServiceResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Service not implemented")
}
func (UnimplementedQueryServer) ServiceByName(context.Context, *QueryServiceByName) (*QueryServiceResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ServiceByName not implemented")
}
func (UnimplementedQueryServer) ServicesByOwner(context.Context, *QueryServicesByOwner) (*QueryServicesResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ServicesByOwner not implemented")
}
func (UnimplementedQueryServer) ServicesByIdentity(context.Context, *QueryServicesByIdentity) (*QueryServicesResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ServicesByIdentity not implemented")
}
func (UnimplementedQueryServer) ProvidersByService(context.Context, *QueryProvidersByService) (*QueryProvidersResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ProvidersByService not implemented")
}
func (UnimplementedQueryServer) ServiceInterface(context.Context, *QueryServiceInterface) (*QueryServiceInterfaceResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ServiceInterface not implemented")
}
func (UnimplementedQueryServer) ServicePaymentModel(context.Context, *QueryServicePaymentModel) (*QueryServicePaymentModelResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ServicePaymentModel not implemented")
}
func (UnimplementedQueryServer) ServiceVerificationModel(context.Context, *QueryServiceVerificationModel) (*QueryServiceVerificationModelResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ServiceVerificationModel not implemented")
}
func (UnimplementedQueryServer) ServiceReceipt(context.Context, *QueryServiceReceipt) (*QueryServiceReceiptResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ServiceReceipt not implemented")
}
func (UnimplementedQueryServer) ServiceProof(context.Context, *QueryServiceProof) (*QueryServiceProofResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ServiceProof not implemented")
}
func (UnimplementedQueryServer) ServiceParams(context.Context, *QueryServiceParams) (*QueryServiceParamsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ServiceParams not implemented")
}