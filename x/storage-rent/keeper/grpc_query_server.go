package keeper

import (
	"context"
	"errors"

	"github.com/sovereign-l1/l1/x/storage-rent/types"
)

var _ types.QueryServer = queryServer{}

type queryServer struct {
	keeper *Keeper
}

func NewQueryServer(k *Keeper) types.QueryServer {
	return queryServer{keeper: k}
}

func (q queryServer) ContractRent(_ context.Context, req *types.QueryContractRentRequest) (*types.QueryContractRentResponse, error) {
	if req == nil {
		return nil, errors.New("empty contract rent query")
	}
	contract, found, err := q.keeper.ContractRent(req.ContractAddress)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errors.New("contract rent not found")
	}
	return &types.QueryContractRentResponse{Contract: contract}, nil
}

func (q queryServer) RentDebt(_ context.Context, req *types.QueryRentDebtRequest) (*types.QueryRentDebtResponse, error) {
	if req == nil {
		return nil, errors.New("empty rent debt query")
	}
	debt, found, err := q.keeper.RentDebt(req.ContractAddress)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errors.New("contract not found")
	}
	return &types.QueryRentDebtResponse{RentDebt: debt}, nil
}

func (q queryServer) FrozenContracts(_ context.Context, req *types.QueryFrozenContractsRequest) (*types.QueryFrozenContractsResponse, error) {
	if req == nil {
		return nil, errors.New("empty frozen contracts query")
	}
	contracts, err := q.keeper.FrozenContracts()
	if err != nil {
		return nil, err
	}
	return &types.QueryFrozenContractsResponse{Contracts: contracts}, nil
}

func (q queryServer) DeletionQueue(_ context.Context, req *types.QueryDeletionQueueRequest) (*types.QueryDeletionQueueResponse, error) {
	if req == nil {
		return nil, errors.New("empty deletion queue query")
	}
	contracts, err := q.keeper.DeletionQueue()
	if err != nil {
		return nil, err
	}
	return &types.QueryDeletionQueueResponse{Contracts: contracts}, nil
}

func (q queryServer) StorageRentParams(_ context.Context, req *types.QueryStorageRentParamsRequest) (*types.QueryStorageRentParamsResponse, error) {
	if req == nil {
		return nil, errors.New("empty storage rent params query")
	}
	params, err := q.keeper.StorageRentParams()
	if err != nil {
		return nil, err
	}
	return &types.QueryStorageRentParamsResponse{Params: params}, nil
}