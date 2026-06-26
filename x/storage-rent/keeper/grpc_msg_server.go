package keeper

import (
	"context"

	"github.com/sovereign-l1/l1/x/storage-rent/types"
)

type msgServer struct {
	k *Keeper
}

func NewMsgServer(k *Keeper) types.MsgServer {
	return &msgServer{k: k}
}

var _ types.MsgServer = &msgServer{}

func (m *msgServer) PayStorageRent(ctx context.Context, req *types.MsgPayStorageRent) (*types.MsgPayStorageRentResponse, error) {
	_, _, err := m.k.PayStorageRent(ctx, *req)
	if err != nil {
		return nil, err
	}
	return &types.MsgPayStorageRentResponse{}, nil
}

func (m *msgServer) UnfreezeContract(ctx context.Context, req *types.MsgUnfreezeContract) (*types.MsgUnfreezeContractResponse, error) {
	_, _, err := m.k.UnfreezeContract(ctx, *req)
	if err != nil {
		return nil, err
	}
	return &types.MsgUnfreezeContractResponse{}, nil
}

func (m *msgServer) WithdrawExcessRent(_ context.Context, req *types.MsgWithdrawExcessRent) (*types.MsgWithdrawExcessRentResponse, error) {
	_, err := m.k.WithdrawExcessRent(*req)
	if err != nil {
		return nil, err
	}
	return &types.MsgWithdrawExcessRentResponse{}, nil
}

func (m *msgServer) FreezeExpiredContract(_ context.Context, req *types.MsgFreezeExpiredContract) (*types.MsgFreezeExpiredContractResponse, error) {
	_, err := m.k.FreezeExpiredContract(*req)
	if err != nil {
		return nil, err
	}
	return &types.MsgFreezeExpiredContractResponse{}, nil
}

func (m *msgServer) DeleteExpiredContract(_ context.Context, req *types.MsgDeleteExpiredContract) (*types.MsgDeleteExpiredContractResponse, error) {
	_, err := m.k.DeleteExpiredContract(*req)
	if err != nil {
		return nil, err
	}
	return &types.MsgDeleteExpiredContractResponse{}, nil
}

func (m *msgServer) UpdateStorageRentParams(_ context.Context, req *types.MsgUpdateStorageRentParams) (*types.MsgUpdateStorageRentParamsResponse, error) {
	err := m.k.UpdateStorageRentParams(*req)
	if err != nil {
		return nil, err
	}
	return &types.MsgUpdateStorageRentParamsResponse{}, nil
}
