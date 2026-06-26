package types

import (
	"github.com/cosmos/gogoproto/proto"

	coretypes "github.com/sovereign-l1/l1/x/aetracore/types"
)

func (m *MsgRegisterInterface) Reset()			{ *m = MsgRegisterInterface{} }
func (m *MsgUpdateInterface) Reset()			{ *m = MsgUpdateInterface{} }
func (*MsgRegisterInterface) ProtoMessage()		{}
func (*MsgUpdateInterface) ProtoMessage()		{}
func (m *MsgRegisterInterface) String() string		{ return proto.CompactTextString(m) }
func (m *MsgUpdateInterface) String() string		{ return proto.CompactTextString(m) }

func (m *MsgRegisterServiceResponse) Reset()		{ *m = MsgRegisterServiceResponse{} }
func (m *MsgUpdateServiceResponse) Reset()		{ *m = MsgUpdateServiceResponse{} }
func (m *MsgRenewServiceResponse) Reset()		{ *m = MsgRenewServiceResponse{} }
func (m *MsgDisableServiceResponse) Reset()		{ *m = MsgDisableServiceResponse{} }
func (m *MsgTransferServiceResponse) Reset()		{ *m = MsgTransferServiceResponse{} }
func (m *MsgBindServiceIdentityResponse) Reset()	{ *m = MsgBindServiceIdentityResponse{} }
func (m *MsgUnbindServiceIdentityResponse) Reset()	{ *m = MsgUnbindServiceIdentityResponse{} }
func (m *MsgRegisterProviderResponse) Reset()		{ *m = MsgRegisterProviderResponse{} }
func (m *MsgUpdateProviderResponse) Reset()		{ *m = MsgUpdateProviderResponse{} }
func (m *MsgStakeProviderCollateralResponse) Reset()	{ *m = MsgStakeProviderCollateralResponse{} }
func (m *MsgUnstakeProviderCollateralResponse) Reset()	{ *m = MsgUnstakeProviderCollateralResponse{} }
func (m *MsgAnchorServiceReceiptResponse) Reset()	{ *m = MsgAnchorServiceReceiptResponse{} }
func (m *MsgSubmitServiceDisputeResponse) Reset()	{ *m = MsgSubmitServiceDisputeResponse{} }
func (m *MsgRegisterInterfaceResponse) Reset()		{ *m = MsgRegisterInterfaceResponse{} }
func (m *MsgUpdateInterfaceResponse) Reset()		{ *m = MsgUpdateInterfaceResponse{} }

func (*MsgRegisterServiceResponse) ProtoMessage()	{}
func (*MsgUpdateServiceResponse) ProtoMessage()	{}
func (*MsgRenewServiceResponse) ProtoMessage()	{}
func (*MsgDisableServiceResponse) ProtoMessage()	{}
func (*MsgTransferServiceResponse) ProtoMessage()	{}
func (*MsgBindServiceIdentityResponse) ProtoMessage()	{}
func (*MsgUnbindServiceIdentityResponse) ProtoMessage()	{}
func (*MsgRegisterProviderResponse) ProtoMessage()	{}
func (*MsgUpdateProviderResponse) ProtoMessage()	{}
func (*MsgStakeProviderCollateralResponse) ProtoMessage()	{}
func (*MsgUnstakeProviderCollateralResponse) ProtoMessage()	{}
func (*MsgAnchorServiceReceiptResponse) ProtoMessage()	{}
func (*MsgSubmitServiceDisputeResponse) ProtoMessage()	{}
func (*MsgRegisterInterfaceResponse) ProtoMessage()	{}
func (*MsgUpdateInterfaceResponse) ProtoMessage()	{}

func (m *MsgRegisterServiceResponse) String() string		{ return proto.CompactTextString(m) }
func (m *MsgUpdateServiceResponse) String() string		{ return proto.CompactTextString(m) }
func (m *MsgRenewServiceResponse) String() string		{ return proto.CompactTextString(m) }
func (m *MsgDisableServiceResponse) String() string		{ return proto.CompactTextString(m) }
func (m *MsgTransferServiceResponse) String() string		{ return proto.CompactTextString(m) }
func (m *MsgBindServiceIdentityResponse) String() string	{ return proto.CompactTextString(m) }
func (m *MsgUnbindServiceIdentityResponse) String() string	{ return proto.CompactTextString(m) }
func (m *MsgRegisterProviderResponse) String() string		{ return proto.CompactTextString(m) }
func (m *MsgUpdateProviderResponse) String() string		{ return proto.CompactTextString(m) }
func (m *MsgStakeProviderCollateralResponse) String() string	{ return proto.CompactTextString(m) }
func (m *MsgUnstakeProviderCollateralResponse) String() string	{ return proto.CompactTextString(m) }
func (m *MsgAnchorServiceReceiptResponse) String() string	{ return proto.CompactTextString(m) }
func (m *MsgSubmitServiceDisputeResponse) String() string	{ return proto.CompactTextString(m) }
func (m *MsgRegisterInterfaceResponse) String() string	{ return proto.CompactTextString(m) }
func (m *MsgUpdateInterfaceResponse) String() string		{ return proto.CompactTextString(m) }

func (m *ServiceDisputeRecord) Reset()  { *m = ServiceDisputeRecord{} }
func (*ServiceDisputeRecord) ProtoMessage() {}
func (m *ServiceDisputeRecord) String() string { return proto.CompactTextString(m) }

func (m *GenesisState) Reset()  { *m = GenesisState{} }
func (*GenesisState) ProtoMessage() {}
func (m *GenesisState) String() string { return proto.CompactTextString(m) }

// Ensure coretypes response types also implement proto.Message for codec registration.
var (
	_ proto.Message = (*coretypes.QueryServiceResponse)(nil)
	_ proto.Message = (*coretypes.QueryServicesResponse)(nil)
	_ proto.Message = (*coretypes.QueryProvidersResponse)(nil)
	_ proto.Message = (*coretypes.QueryServiceInterfaceResponse)(nil)
	_ proto.Message = (*coretypes.QueryServicePaymentModelResponse)(nil)
	_ proto.Message = (*coretypes.QueryServiceVerificationModelResponse)(nil)
	_ proto.Message = (*coretypes.QueryServiceReceiptResponse)(nil)
	_ proto.Message = (*coretypes.QueryServiceProofResponse)(nil)
	_ proto.Message = (*coretypes.QueryServiceParamsResponse)(nil)
)