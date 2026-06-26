package types

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	gogoproto "github.com/cosmos/gogoproto/proto"
	grpc "google.golang.org/grpc"
)

const (
	MsgWithdrawExcessRentTypeURL    = "/l1.storagerent.v1.MsgWithdrawExcessRent"
	MsgFreezeExpiredContractTypeURL = "/l1.storagerent.v1.MsgFreezeExpiredContract"
	MsgDeleteExpiredContractTypeURL  = "/l1.storagerent.v1.MsgDeleteExpiredContract"
	MsgUpdateStorageRentParamsTypeURL = "/l1.storagerent.v1.MsgUpdateStorageRentParams"
)

type MsgWithdrawExcessRentResponse struct{}

func (m *MsgWithdrawExcessRentResponse) Reset()      { *m = MsgWithdrawExcessRentResponse{} }
func (m *MsgWithdrawExcessRentResponse) String() string { return "MsgWithdrawExcessRentResponse" }
func (*MsgWithdrawExcessRentResponse) ProtoMessage()   {}

func (m *MsgWithdrawExcessRentResponse) Marshal() (dAtA []byte, err error)       { return nil, nil }
func (m *MsgWithdrawExcessRentResponse) MarshalTo(dAtA []byte) (int, error)      { return 0, nil }
func (m *MsgWithdrawExcessRentResponse) MarshalToSizedBuffer(dAtA []byte) (int, error) { return len(dAtA), nil }
func (m *MsgWithdrawExcessRentResponse) Size() int                                { return 0 }
func (m *MsgWithdrawExcessRentResponse) Unmarshal(dAtA []byte) error             { return nil }
func (m *MsgWithdrawExcessRentResponse) XXX_Unmarshal(b []byte) error            { return nil }

type MsgFreezeExpiredContractResponse struct{}

func (m *MsgFreezeExpiredContractResponse) Reset()      { *m = MsgFreezeExpiredContractResponse{} }
func (m *MsgFreezeExpiredContractResponse) String() string { return "MsgFreezeExpiredContractResponse" }
func (*MsgFreezeExpiredContractResponse) ProtoMessage()     {}

func (m *MsgFreezeExpiredContractResponse) Marshal() (dAtA []byte, err error)       { return nil, nil }
func (m *MsgFreezeExpiredContractResponse) MarshalTo(dAtA []byte) (int, error)      { return 0, nil }
func (m *MsgFreezeExpiredContractResponse) MarshalToSizedBuffer(dAtA []byte) (int, error) { return len(dAtA), nil }
func (m *MsgFreezeExpiredContractResponse) Size() int                                { return 0 }
func (m *MsgFreezeExpiredContractResponse) Unmarshal(dAtA []byte) error             { return nil }
func (m *MsgFreezeExpiredContractResponse) XXX_Unmarshal(b []byte) error            { return nil }

type MsgDeleteExpiredContractResponse struct{}

func (m *MsgDeleteExpiredContractResponse) Reset()      { *m = MsgDeleteExpiredContractResponse{} }
func (m *MsgDeleteExpiredContractResponse) String() string { return "MsgDeleteExpiredContractResponse" }
func (*MsgDeleteExpiredContractResponse) ProtoMessage()     {}

func (m *MsgDeleteExpiredContractResponse) Marshal() (dAtA []byte, err error)       { return nil, nil }
func (m *MsgDeleteExpiredContractResponse) MarshalTo(dAtA []byte) (int, error)      { return 0, nil }
func (m *MsgDeleteExpiredContractResponse) MarshalToSizedBuffer(dAtA []byte) (int, error) { return len(dAtA), nil }
func (m *MsgDeleteExpiredContractResponse) Size() int                                { return 0 }
func (m *MsgDeleteExpiredContractResponse) Unmarshal(dAtA []byte) error             { return nil }
func (m *MsgDeleteExpiredContractResponse) XXX_Unmarshal(b []byte) error            { return nil }

type MsgUpdateStorageRentParamsResponse struct{}

func (m *MsgUpdateStorageRentParamsResponse) Reset()      { *m = MsgUpdateStorageRentParamsResponse{} }
func (m *MsgUpdateStorageRentParamsResponse) String() string { return "MsgUpdateStorageRentParamsResponse" }
func (*MsgUpdateStorageRentParamsResponse) ProtoMessage()     {}

func (m *MsgUpdateStorageRentParamsResponse) Marshal() (dAtA []byte, err error)       { return nil, nil }
func (m *MsgUpdateStorageRentParamsResponse) MarshalTo(dAtA []byte) (int, error)      { return 0, nil }
func (m *MsgUpdateStorageRentParamsResponse) MarshalToSizedBuffer(dAtA []byte) (int, error) { return len(dAtA), nil }
func (m *MsgUpdateStorageRentParamsResponse) Size() int                                { return 0 }
func (m *MsgUpdateStorageRentParamsResponse) Unmarshal(dAtA []byte) error             { return nil }
func (m *MsgUpdateStorageRentParamsResponse) XXX_Unmarshal(b []byte) error            { return nil }

func (m *MsgWithdrawExcessRent) Reset()            { *m = MsgWithdrawExcessRent{} }
func (m *MsgWithdrawExcessRent) String() string     { return gogoproto.CompactTextString(m) }
func (*MsgWithdrawExcessRent) ProtoMessage()          {}

func (m *MsgWithdrawExcessRent) ValidateBasic() error {
	if m.Authority == "" {
		return fmt.Errorf("authority is required")
	}
	if m.ContractAddress == "" {
		return fmt.Errorf("contract address is required")
	}
	if m.Amount == 0 {
		return fmt.Errorf("amount must be positive")
	}
	if m.Height == 0 {
		return fmt.Errorf("height must be positive")
	}
	return nil
}

func (m *MsgWithdrawExcessRent) GetSigners() []sdk.AccAddress {
	authority, err := sdk.AccAddressFromBech32(m.Authority)
	if err != nil {
		return nil
	}
	return []sdk.AccAddress{authority}
}

func (m *MsgWithdrawExcessRent) XXX_Unmarshal(b []byte) error { return nil }
func (m *MsgWithdrawExcessRent) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	if deterministic {
		return gogoproto.Marshal(m)
	}
	return nil, nil
}

func (m *MsgFreezeExpiredContract) Reset()            { *m = MsgFreezeExpiredContract{} }
func (m *MsgFreezeExpiredContract) String() string     { return gogoproto.CompactTextString(m) }
func (*MsgFreezeExpiredContract) ProtoMessage()          {}

func (m *MsgFreezeExpiredContract) ValidateBasic() error {
	if m.Authority == "" {
		return fmt.Errorf("authority is required")
	}
	if m.ContractAddress == "" {
		return fmt.Errorf("contract address is required")
	}
	if m.Height == 0 {
		return fmt.Errorf("height must be positive")
	}
	return nil
}

func (m *MsgFreezeExpiredContract) GetSigners() []sdk.AccAddress {
	authority, err := sdk.AccAddressFromBech32(m.Authority)
	if err != nil {
		return nil
	}
	return []sdk.AccAddress{authority}
}

func (m *MsgFreezeExpiredContract) XXX_Unmarshal(b []byte) error { return nil }
func (m *MsgFreezeExpiredContract) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	if deterministic {
		return gogoproto.Marshal(m)
	}
	return nil, nil
}

func (m *MsgDeleteExpiredContract) Reset()            { *m = MsgDeleteExpiredContract{} }
func (m *MsgDeleteExpiredContract) String() string     { return gogoproto.CompactTextString(m) }
func (*MsgDeleteExpiredContract) ProtoMessage()          {}

func (m *MsgDeleteExpiredContract) ValidateBasic() error {
	if m.Authority == "" {
		return fmt.Errorf("authority is required")
	}
	if m.ContractAddress == "" {
		return fmt.Errorf("contract address is required")
	}
	if m.Height == 0 {
		return fmt.Errorf("height must be positive")
	}
	return nil
}

func (m *MsgDeleteExpiredContract) GetSigners() []sdk.AccAddress {
	authority, err := sdk.AccAddressFromBech32(m.Authority)
	if err != nil {
		return nil
	}
	return []sdk.AccAddress{authority}
}

func (m *MsgDeleteExpiredContract) XXX_Unmarshal(b []byte) error { return nil }
func (m *MsgDeleteExpiredContract) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	if deterministic {
		return gogoproto.Marshal(m)
	}
	return nil, nil
}

func (m *MsgUpdateStorageRentParams) Reset()            { *m = MsgUpdateStorageRentParams{} }
func (m *MsgUpdateStorageRentParams) String() string     { return gogoproto.CompactTextString(m) }
func (*MsgUpdateStorageRentParams) ProtoMessage()          {}

func (m *MsgUpdateStorageRentParams) ValidateBasic() error {
	if m.Authority == "" {
		return fmt.Errorf("authority is required")
	}
	return m.Params.Validate()
}

func (m *MsgUpdateStorageRentParams) GetSigners() []sdk.AccAddress {
	authority, err := sdk.AccAddressFromBech32(m.Authority)
	if err != nil {
		return nil
	}
	return []sdk.AccAddress{authority}
}

func (m *MsgUpdateStorageRentParams) XXX_Unmarshal(b []byte) error { return nil }
func (m *MsgUpdateStorageRentParams) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	if deterministic {
		return gogoproto.Marshal(m)
	}
	return nil, nil
}

func init() {
	gogoproto.RegisterType((*MsgWithdrawExcessRent)(nil), "l1.storagerent.v1.MsgWithdrawExcessRent")
	gogoproto.RegisterType((*MsgWithdrawExcessRentResponse)(nil), "l1.storagerent.v1.MsgWithdrawExcessRentResponse")
	gogoproto.RegisterType((*MsgFreezeExpiredContract)(nil), "l1.storagerent.v1.MsgFreezeExpiredContract")
	gogoproto.RegisterType((*MsgFreezeExpiredContractResponse)(nil), "l1.storagerent.v1.MsgFreezeExpiredContractResponse")
	gogoproto.RegisterType((*MsgDeleteExpiredContract)(nil), "l1.storagerent.v1.MsgDeleteExpiredContract")
	gogoproto.RegisterType((*MsgDeleteExpiredContractResponse)(nil), "l1.storagerent.v1.MsgDeleteExpiredContractResponse")
	gogoproto.RegisterType((*MsgUpdateStorageRentParams)(nil), "l1.storagerent.v1.MsgUpdateStorageRentParams")
	gogoproto.RegisterType((*MsgUpdateStorageRentParamsResponse)(nil), "l1.storagerent.v1.MsgUpdateStorageRentParamsResponse")
}

func _Msg_WithdrawExcessRent_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(MsgWithdrawExcessRent)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).WithdrawExcessRent(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/l1.storagerent.v1.Msg/WithdrawExcessRent",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MsgServer).WithdrawExcessRent(ctx, req.(*MsgWithdrawExcessRent))
	}
	return interceptor(ctx, in, info, handler)
}

func _Msg_FreezeExpiredContract_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(MsgFreezeExpiredContract)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).FreezeExpiredContract(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/l1.storagerent.v1.Msg/FreezeExpiredContract",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MsgServer).FreezeExpiredContract(ctx, req.(*MsgFreezeExpiredContract))
	}
	return interceptor(ctx, in, info, handler)
}

func _Msg_DeleteExpiredContract_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(MsgDeleteExpiredContract)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).DeleteExpiredContract(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/l1.storagerent.v1.Msg/DeleteExpiredContract",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MsgServer).DeleteExpiredContract(ctx, req.(*MsgDeleteExpiredContract))
	}
	return interceptor(ctx, in, info, handler)
}

func _Msg_UpdateStorageRentParams_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(MsgUpdateStorageRentParams)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).UpdateStorageRentParams(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/l1.storagerent.v1.Msg/UpdateStorageRentParams",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MsgServer).UpdateStorageRentParams(ctx, req.(*MsgUpdateStorageRentParams))
	}
	return interceptor(ctx, in, info, handler)
}