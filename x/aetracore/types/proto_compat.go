package types

import (
	"github.com/cosmos/gogoproto/proto"
)

func (m *MsgRegisterService) Reset()			{ *m = MsgRegisterService{} }
func (m *MsgUpdateService) Reset()			{ *m = MsgUpdateService{} }
func (m *MsgRenewService) Reset()			{ *m = MsgRenewService{} }
func (m *MsgDisableService) Reset()			{ *m = MsgDisableService{} }
func (m *MsgTransferService) Reset()			{ *m = MsgTransferService{} }
func (m *MsgBindServiceIdentity) Reset()		{ *m = MsgBindServiceIdentity{} }
func (m *MsgUnbindServiceIdentity) Reset()		{ *m = MsgUnbindServiceIdentity{} }
func (m *MsgRegisterProvider) Reset()			{ *m = MsgRegisterProvider{} }
func (m *MsgUpdateProvider) Reset()			{ *m = MsgUpdateProvider{} }
func (m *MsgStakeProviderCollateral) Reset()		{ *m = MsgStakeProviderCollateral{} }
func (m *MsgUnstakeProviderCollateral) Reset()		{ *m = MsgUnstakeProviderCollateral{} }
func (m *MsgAnchorServiceReceipt) Reset()		{ *m = MsgAnchorServiceReceipt{} }
func (m *MsgSubmitServiceDispute) Reset()		{ *m = MsgSubmitServiceDispute{} }

func (*MsgRegisterService) ProtoMessage()		{}
func (*MsgUpdateService) ProtoMessage()		{}
func (*MsgRenewService) ProtoMessage()			{}
func (*MsgDisableService) ProtoMessage()		{}
func (*MsgTransferService) ProtoMessage()		{}
func (*MsgBindServiceIdentity) ProtoMessage()		{}
func (*MsgUnbindServiceIdentity) ProtoMessage()		{}
func (*MsgRegisterProvider) ProtoMessage()		{}
func (*MsgUpdateProvider) ProtoMessage()		{}
func (*MsgStakeProviderCollateral) ProtoMessage()	{}
func (*MsgUnstakeProviderCollateral) ProtoMessage()	{}
func (*MsgAnchorServiceReceipt) ProtoMessage()		{}
func (*MsgSubmitServiceDispute) ProtoMessage()		{}

func (m *MsgRegisterService) String() string		{ return proto.CompactTextString(m) }
func (m *MsgUpdateService) String() string		{ return proto.CompactTextString(m) }
func (m *MsgRenewService) String() string		{ return proto.CompactTextString(m) }
func (m *MsgDisableService) String() string		{ return proto.CompactTextString(m) }
func (m *MsgTransferService) String() string		{ return proto.CompactTextString(m) }
func (m *MsgBindServiceIdentity) String() string	{ return proto.CompactTextString(m) }
func (m *MsgUnbindServiceIdentity) String() string	{ return proto.CompactTextString(m) }
func (m *MsgRegisterProvider) String() string		{ return proto.CompactTextString(m) }
func (m *MsgUpdateProvider) String() string		{ return proto.CompactTextString(m) }
func (m *MsgStakeProviderCollateral) String() string	{ return proto.CompactTextString(m) }
func (m *MsgUnstakeProviderCollateral) String() string	{ return proto.CompactTextString(m) }
func (m *MsgAnchorServiceReceipt) String() string	{ return proto.CompactTextString(m) }
func (m *MsgSubmitServiceDispute) String() string	{ return proto.CompactTextString(m) }

func (m *QueryService) Reset()				{ *m = QueryService{} }
func (m *QueryServiceByName) Reset()			{ *m = QueryServiceByName{} }
func (m *QueryServicesByOwner) Reset()			{ *m = QueryServicesByOwner{} }
func (m *QueryServicesByIdentity) Reset()		{ *m = QueryServicesByIdentity{} }
func (m *QueryProvidersByService) Reset()		{ *m = QueryProvidersByService{} }
func (m *QueryServiceInterface) Reset()			{ *m = QueryServiceInterface{} }
func (m *QueryServicePaymentModel) Reset()		{ *m = QueryServicePaymentModel{} }
func (m *QueryServiceVerificationModel) Reset()		{ *m = QueryServiceVerificationModel{} }
func (m *QueryServiceReceipt) Reset()			{ *m = QueryServiceReceipt{} }
func (m *QueryServiceProof) Reset()			{ *m = QueryServiceProof{} }
func (m *QueryServiceParams) Reset()			{ *m = QueryServiceParams{} }

func (*QueryService) ProtoMessage()			{}
func (*QueryServiceByName) ProtoMessage()		{}
func (*QueryServicesByOwner) ProtoMessage()		{}
func (*QueryServicesByIdentity) ProtoMessage()		{}
func (*QueryProvidersByService) ProtoMessage()		{}
func (*QueryServiceInterface) ProtoMessage()		{}
func (*QueryServicePaymentModel) ProtoMessage()		{}
func (*QueryServiceVerificationModel) ProtoMessage()	{}
func (*QueryServiceReceipt) ProtoMessage()		{}
func (*QueryServiceProof) ProtoMessage()		{}
func (*QueryServiceParams) ProtoMessage()		{}

func (m *QueryService) String() string			{ return proto.CompactTextString(m) }
func (m *QueryServiceByName) String() string		{ return proto.CompactTextString(m) }
func (m *QueryServicesByOwner) String() string		{ return proto.CompactTextString(m) }
func (m *QueryServicesByIdentity) String() string	{ return proto.CompactTextString(m) }
func (m *QueryProvidersByService) String() string	{ return proto.CompactTextString(m) }
func (m *QueryServiceInterface) String() string		{ return proto.CompactTextString(m) }
func (m *QueryServicePaymentModel) String() string	{ return proto.CompactTextString(m) }
func (m *QueryServiceVerificationModel) String() string { return proto.CompactTextString(m) }
func (m *QueryServiceReceipt) String() string		{ return proto.CompactTextString(m) }
func (m *QueryServiceProof) String() string		{ return proto.CompactTextString(m) }
func (m *QueryServiceParams) String() string		{ return proto.CompactTextString(m) }

func (m *QueryServiceResponse) Reset()			{ *m = QueryServiceResponse{} }
func (m *QueryServicesResponse) Reset()			{ *m = QueryServicesResponse{} }
func (m *QueryProvidersResponse) Reset()		{ *m = QueryProvidersResponse{} }
func (m *QueryServiceInterfaceResponse) Reset()	{ *m = QueryServiceInterfaceResponse{} }
func (m *QueryServicePaymentModelResponse) Reset()	{ *m = QueryServicePaymentModelResponse{} }
func (m *QueryServiceVerificationModelResponse) Reset()	{ *m = QueryServiceVerificationModelResponse{} }
func (m *QueryServiceReceiptResponse) Reset()		{ *m = QueryServiceReceiptResponse{} }
func (m *QueryServiceProofResponse) Reset()		{ *m = QueryServiceProofResponse{} }
func (m *QueryServiceParamsResponse) Reset()		{ *m = QueryServiceParamsResponse{} }

func (*QueryServiceResponse) ProtoMessage()		{}
func (*QueryServicesResponse) ProtoMessage()		{}
func (*QueryProvidersResponse) ProtoMessage()		{}
func (*QueryServiceInterfaceResponse) ProtoMessage()	{}
func (*QueryServicePaymentModelResponse) ProtoMessage()	{}
func (*QueryServiceVerificationModelResponse) ProtoMessage()	{}
func (*QueryServiceReceiptResponse) ProtoMessage()	{}
func (*QueryServiceProofResponse) ProtoMessage()	{}
func (*QueryServiceParamsResponse) ProtoMessage()	{}

func (m *QueryServiceResponse) String() string		{ return proto.CompactTextString(m) }
func (m *QueryServicesResponse) String() string		{ return proto.CompactTextString(m) }
func (m *QueryProvidersResponse) String() string	{ return proto.CompactTextString(m) }
func (m *QueryServiceInterfaceResponse) String() string	{ return proto.CompactTextString(m) }
func (m *QueryServicePaymentModelResponse) String() string { return proto.CompactTextString(m) }
func (m *QueryServiceVerificationModelResponse) String() string { return proto.CompactTextString(m) }
func (m *QueryServiceReceiptResponse) String() string	{ return proto.CompactTextString(m) }
func (m *QueryServiceProofResponse) String() string	{ return proto.CompactTextString(m) }
func (m *QueryServiceParamsResponse) String() string	{ return proto.CompactTextString(m) }

